use anyhow::{Context, Result};
use mysql_async as my;
use mysql_async::params;
use mysql_async::prelude::Queryable;
use serde::{Deserialize, Serialize};
use std::time::Duration;
use tokio::{signal, time::sleep};
use tracing::{error, info, warn};

#[derive(Clone, Debug)]
struct Config {
    db_host: String,
    db_port: String,
    db_user: String,
    db_password: String,
    db_name: String,
    openai_api_key: String,
    openai_model: String,
    loop_delay_ms: u64,
    batch_limit: u64,
}

impl Config {
    fn from_env() -> Self {
        let get = |k: &str, d: &str| std::env::var(k).unwrap_or_else(|_| d.to_string());

        Self {
            db_host: get("DB_HOST", "localhost"),
            db_port: get("DB_PORT", "3306"),
            db_user: get("DB_USER", "server"),
            db_password: get("DB_PASSWORD", "secret_app"),
            db_name: get("DB_NAME", "cleanapp"),
            openai_api_key: get("OPENAI_API_KEY", ""),
            openai_model: get("OPENAI_MODEL", "gpt-4o"),
            loop_delay_ms: get("LOOP_DELAY_MS", "10000").parse().unwrap_or(10000),
            batch_limit: get("BATCH_LIMIT", "10").parse().unwrap_or(10),
        }
    }

    fn mysql_url(&self) -> String {
        format!(
            "{}:{}@tcp({}:{})/{}?parseTime=true&multiStatements=true",
            self.db_user, self.db_password, self.db_host, self.db_port, self.db_name
        )
    }
}

#[derive(Debug, Deserialize, Serialize)]
struct ReportAnalysisRow {
    seq: i64,
    brand_display_name: String,
}

#[derive(Debug, Deserialize, Serialize)]
struct OpenAIResponseChoice {
    message: OpenAIMessage,
}

#[derive(Debug, Deserialize, Serialize)]
struct OpenAIMessage {
    content: String,
}

#[derive(Debug, Deserialize, Serialize)]
struct OpenAIChatRequest<'a> {
    model: &'a str,
    messages: Vec<OpenAIChatMessage<'a>>,
    temperature: f32,
}

#[derive(Debug, Deserialize, Serialize)]
struct OpenAIChatMessage<'a> {
    role: &'a str,
    content: String,
}

#[derive(Debug, Deserialize, Serialize)]
struct OpenAIChatResponse {
    choices: Vec<OpenAIResponseChoice>,
}

async fn fetch_support_emails(brand: &str, cfg: &Config) -> Result<Option<String>> {
    if cfg.openai_api_key.is_empty() {
        warn!("OPENAI_API_KEY is empty; skipping LLM lookup");
        return Ok(None);
    }

    let prompt = format!(
        "Given the brand/app name '{}', provide a short, comma-separated list (1-3) of plausible official support contact emails for notifying about software issues. Prefer vendor domains. Return ONLY the emails, comma-separated, no extra text.",
        brand
    );

    let req_body = OpenAIChatRequest {
        model: &cfg.openai_model,
        messages: vec![
            OpenAIChatMessage {
                role: "system",
                content: "You extract support contact emails.".to_string(),
            },
            OpenAIChatMessage {
                role: "user",
                content: prompt,
            },
        ],
        temperature: 0.2,
    };

    let client = reqwest::Client::new();
    let resp = client
        .post("https://api.openai.com/v1/chat/completions")
        .bearer_auth(&cfg.openai_api_key)
        .json(&req_body)
        .send()
        .await
        .context("openai request failed")?;

    if !resp.status().is_success() {
        warn!("OpenAI non-success status: {}", resp.status());
        return Ok(None);
    }

    let data: OpenAIChatResponse = resp.json().await.context("openai json decode")?;
    let content = data
        .choices
        .first()
        .map(|c| c.message.content.trim().to_string())
        .unwrap_or_default();

    let cleaned = content
        .split(',')
        .map(|s| s.trim())
        .filter(|s| s.contains('@'))
        .collect::<Vec<_>>()
        .join(",");

    if cleaned.is_empty() {
        Ok(None)
    } else {
        Ok(Some(cleaned))
    }
}

async fn run_once(pool: &my::Pool, cfg: &Config) -> Result<usize> {
    let mut conn = pool.get_conn().await?;
    // Find candidate analyses: valid digital reports with empty inferred_contact_emails
    let select_sql = r#"
        SELECT seq, brand_display_name
        FROM report_analysis
        WHERE is_valid = TRUE
          AND classification = 'digital'
          AND (inferred_contact_emails IS NULL OR inferred_contact_emails = '' )
        ORDER BY updated_at ASC
        LIMIT :limit
    "#;

    let rows: Vec<(i64, Option<String>)> = conn
        .exec(select_sql, params! { "limit" => cfg.batch_limit })
        .await?;

    let mut processed = 0usize;
    for (seq, brand_opt) in rows.into_iter() {
        let brand = brand_opt.unwrap_or_default();
        if brand.is_empty() {
            continue;
        }

        match fetch_support_emails(&brand, cfg).await? {
            Some(emails) => {
                let update_sql = r#"
                    UPDATE report_analysis
                    SET inferred_contact_emails = :emails
                    WHERE seq = :seq AND language = 'en'
                "#;
                conn.exec_drop(update_sql, params! { "emails" => emails, "seq" => seq })
                    .await?;
                processed += 1;
                info!(
                    "Updated inferred_contact_emails for seq={} ({})",
                    seq, brand
                );
            }
            None => {
                info!("No emails inferred for seq={} ({})", seq, brand);
            }
        }
    }

    Ok(processed)
}

#[tokio::main]
async fn main() -> Result<()> {
    dotenvy::dotenv().ok();
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .with_target(false)
        .compact()
        .init();

    let cfg = Config::from_env();

    let url = cfg.mysql_url();
    let opts = my::Opts::from_url(&url).context("invalid MySQL URL")?;
    let pool = my::Pool::new(opts);

    info!(
        "email-fettcher starting; delay={}ms, limit={}",
        cfg.loop_delay_ms, cfg.batch_limit
    );

    loop {
        tokio::select! {
            _ = signal::ctrl_c() => {
                info!("Shutdown signal received");
                break;
            }
            _ = sleep(Duration::from_millis(cfg.loop_delay_ms)) => {
                match run_once(&pool, &cfg).await {
                    Ok(n) => info!("Batch processed: {} rows", n),
                    Err(e) => error!("Batch error: {:#}", e),
                }
            }
        }
    }

    pool.disconnect().await?;
    Ok(())
}
