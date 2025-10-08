use anyhow::Result;
use email_service_v3::{config::Config, db, email::send_sendgrid_email};
use mysql as my;
use tokio::{signal, time::{sleep, Duration}};

#[tokio::main]
async fn main() -> Result<()> {
    dotenvy::dotenv().ok();
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .with_target(false)
        .compact()
        .init();

    let cfg = Config::from_env()?;
    if !cfg.enable_email_v3 {
        tracing::warn!("ENABLE_EMAIL_V3 is disabled; service will exit without starting");
        return Ok(());
    }
    tracing::info!("email-service-v3 starting; DB={}, poll={:?}, test_brands={:?}", cfg.mysql_masked_url(), cfg.poll_interval, cfg.test_brands);

    let pool = db::connect_pool(&cfg)?;
    let mut conn = pool.get_conn()?;
    db::init_schema(&mut conn)?;
    drop(conn);

    if cfg.test_brands.is_some() {
        if let Err(e) = run_once(&pool, &cfg).await { tracing::error!("Batch error: {:#}", e); }
        return Ok(());
    } else {
        loop {
            tokio::select! {
                _ = signal::ctrl_c() => {
                    tracing::info!("Shutdown signal received");
                    break;
                }
                _ = sleep(cfg.poll_interval) => {
                    if let Err(e) = run_once(&pool, &cfg).await { tracing::error!("Batch error: {:#}", e); }
                }
            }
        }
    }

    Ok(())
}

async fn run_once(pool: &my::Pool, cfg: &Config) -> Result<()> {
    let mut conn = pool.get_conn()?;
    let period_days = (cfg.notification_period.as_secs() / 86400) as i64;
    let to_send = if let Some(ref brands) = cfg.test_brands {
        db::pick_due_notifications_for_brands(&mut conn, period_days, brands)?
    } else {
        db::pick_due_notifications(&mut conn, period_days)?
    };
    tracing::info!("Due notifications: {}", to_send.len());

    for (email, brand, brand_display_name) in to_send {
        // Skip opted-out recipients
        if db::is_email_opted_out(&mut conn, &email)? {
            tracing::info!("Skipping opted-out email: {} (brand {})", email, brand);
            continue;
        }

        let url = format!("{}/{}", cfg.digital_base_url.trim_end_matches('/'), brand);
        let html = match fetch_until_ready(&url, Duration::from_secs(30), Duration::from_millis
            (1500)).await {
            Ok(h) => h,
            Err(e) => {
                tracing::warn!("Skipping brand {} ({}): content not ready within timeout: {:#}", brand, email, e);
                continue;
            }
        };
        let subject = "CleanApp Reports Summary";
        let plain = format!(
            "A new {} report has been analyzed and requires your attention.\nSee: {}",
            brand_display_name, url
        );
        let unsub_link = format!("{}?email={}", cfg.opt_out_url, email);
        let plain = format!(
            "{}\n\nIf you received this in error, please ribe here: {}unsubsc",
            plain, unsub_link
        );
        match send_sendgrid_email(
            &cfg.sendgrid_api_key,
            &cfg.sendgrid_from_name,
            &cfg.sendgrid_from_email,
            &email,
            subject,
            &format!(
                "<p>A new {} report has been analyzed and requires your attention.</p><p><a href=\"{}\">Open live dashboard</a></p>{}<div style=\"margin-top:24px;font-size:12px;color:#666\">If you received this in error, please <a href=\"{}\">unsubscribe here</a>.</div>",
                brand_display_name,
                url,
                html,
                unsub_link
            ),
            &plain,
            Some(&cfg.bcc_email_address),
        ).await {
            Ok(_) => {
                tracing::info!("Email sent to {} for brand {}", email, brand);
                db::record_notification(&mut conn, &email, &brand)?;
            }
            Err(e) => tracing::warn!("Send email failed for {} {}: {:#}", email, brand, e),
        }
    }

    Ok(())
}

async fn fetch_once(url: &str) -> Result<String> {
    let client = reqwest::Client::new();
    let res = client.get(url).send().await?;
    let status = res.status();
    let body = res.text().await.unwrap_or_default();
    if !status.is_success() {
        anyhow::bail!("fetch {} failed: {}", url, status);
    }
    Ok(body)
}

fn looks_loading(html: &str) -> bool {
    let lower = html.to_lowercase();
    let loading = lower.contains("loading") || lower.contains("skeleton");
    let has_recent = lower.contains("recent reports");
    let has_items = lower.contains("<article") || lower.contains("data-report") || lower.contains("class=\"report");
    (loading && has_recent) && !has_items
}

async fn fetch_until_ready(url: &str, max_wait: Duration, interval: Duration) -> Result<String> {
    let start = std::time::Instant::now();
    loop {
        let html = fetch_once(url).await?;
        if !looks_loading(&html) {
            return Ok(html);
        }
        if start.elapsed() >= max_wait {
            anyhow::bail!("content still loading after {:?}", max_wait);
        }
        sleep(interval).await;
    }
}

