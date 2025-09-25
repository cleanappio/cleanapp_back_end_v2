use anyhow::Result;
use email_service_v3::{config::Config, db, models::{Brand, BrandEmail}};
use mysql as my;
use my::prelude::*;
use std::collections::{BTreeMap, BTreeSet};

#[tokio::main]
async fn main() -> Result<()> {
    dotenvy::dotenv().ok();
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .with_target(false)
        .compact()
        .init();

    let cfg = Config::from_env()?;
    tracing::info!("DB URI: {}", cfg.mysql_masked_url());

    let pool = db::connect_pool(&cfg)?;
    let mut conn = pool.get_conn()?;
    db::init_schema(&mut conn)?;

    // Query report_analysis for brand_name, brand_display_name and inferred_contact_emails
    let rows: Vec<(Option<String>, Option<String>, Option<String>)> = conn.exec(
        r#"SELECT brand_name, brand_display_name, inferred_contact_emails FROM report_analysis 
           WHERE classification = 'digital' 
             AND brand_name IS NOT NULL 
             AND inferred_contact_emails IS NOT NULL"#,
        (),
    )?;

    let mut brand_to_emails: BTreeMap<String, BTreeSet<String>> = BTreeMap::new();
    let mut brand_to_display: BTreeMap<String, String> = BTreeMap::new();
    for (brand_opt, display_opt, emails_opt) in rows {
        let brand = match brand_opt { Some(b) if !b.trim().is_empty() => b.trim().to_string(), _ => continue };
        if !brand_to_display.contains_key(&brand) {
            let display = display_opt.unwrap_or_default().trim().to_string();
            let display = if display.is_empty() { prettify_brand_name(&brand) } else { display };
            brand_to_display.insert(brand.clone(), display);
        }
        let emails = emails_opt.unwrap_or_default();
        for part in emails.split(',') {
            let email = part.trim().to_string();
            if email.is_empty() { continue; }
            brand_to_emails.entry(brand.clone()).or_default().insert(email);
        }
    }

    tracing::info!("Found {} brands in report_analysis for backfill", brand_to_emails.len());

    for (brand_name, emails) in brand_to_emails {
        let display = brand_to_display.get(&brand_name).cloned().unwrap_or_else(|| prettify_brand_name(&brand_name));
        db::upsert_brand(&mut conn, &Brand { brand_name: brand_name.clone(), brand_display_name: display })?;
        for email in emails {
            db::upsert_brand_email(&mut conn, &BrandEmail { email_address: email, brand_name: brand_name.clone() })?;
        }
    }

    tracing::info!("Backfill complete");
    Ok(())
}

fn prettify_brand_name(brand: &str) -> String {
    let replaced = brand.replace(['_', '-'], " ");
    replaced
        .split_whitespace()
        .map(|w| {
            let mut ch = w.chars();
            match ch.next() {
                Some(first) => format!("{}{}", first.to_uppercase(), ch.as_str().to_lowercase()),
                None => String::new(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

