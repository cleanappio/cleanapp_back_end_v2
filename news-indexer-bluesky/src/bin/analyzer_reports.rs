//! Report Analyzer Service
//!
//! This service processes reports that have `needs_ai_review = TRUE` in the `report_analysis` table.
//! It sends the title and description to Gemini AI to extract:
//! - Proper brand name and display name
//! - Distilled summary (gist)
//! - Cleaned report title and description
//!
//! After processing, it updates the report_analysis table and sets needs_ai_review = FALSE.

use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::{json, Value as JsonValue};
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[derive(Parser, Debug, Clone)]
struct Args {
    #[arg(long, default_value = "config.toml")]
    config_path: String,
    #[arg(long, env = "DB_URL")]
    db_url: Option<String>,
    #[arg(long, env = "GEMINI_API_KEY")]
    gemini_api_key: Option<String>,
    #[arg(long, env = "GEMINI_MODEL", default_value = "gemini-flash-latest")]
    gemini_model: String,
    #[arg(long, env = "ANALYZER_BATCH_SIZE", default_value_t = 50)]
    batch_size: usize,
    #[arg(long, env = "ANALYZER_INTERVAL_SECS", default_value_t = 60)]
    interval_secs: u64,
}

#[derive(Deserialize, Clone, Debug)]
struct Config {
    general: Option<GeneralConfig>,
}

#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig {
    db_url: String,
}

const PROMPT: &str = r#"
You are analyzing a user-submitted report for CleanApp's brand sentiment platform.
CleanApp crowdsources feedback about SPECIFIC brands and forwards it to those brands.

CRITICAL: We need SPECIFIC, IDENTIFIABLE brand names - not vague categories.

Given the report title and description, return ONLY a strict JSON object:
{
  "brand_display_name": string,  // MUST be a specific brand (e.g., "Uber", "Discord", "Steam", "Delta Airlines")
  "brand_name": string,          // Normalized lowercase version (e.g., "uber", "discord", "steam")
  "summary": string,             // A distilled 1-2 sentence gist of the issue (<= 300 chars)
  "report_title": string,        // A clean, concise title (<= 120 chars)
  "report_description": string,  // A clear description of the issue (<= 1000 chars)
  "classification": "digital" | "physical",
  "severity_level": number,      // 0.0 to 1.0 (1.0 = critical)
  "digital_bug_probability": number,  // 0.0 to 1.0
  "language": string             // ISO language code (e.g., "en", "es", "fr")
}

BRAND EXTRACTION RULES:
1. Extract the ACTUAL company/brand name mentioned or implied
2. "MY STEAM ACCOUNT won't download..." → brand = "Steam"
3. "Uber driver was rude..." → brand = "Uber"  
4. "The Disney+ app keeps crashing" → brand = "Disney+"
5. Look for product names, app names, service names, company names

DO NOT USE vague categories like:
- "Unknown Service Platform" ❌
- "Delivery Service App" ❌
- "Operating System/Hardware" ❌
- "Airline Industry" ❌
- "Unknown Ride Share App" ❌

Instead, identify the SPECIFIC brand:
- If discussing a rideshare, is it Uber, Lyft, Bolt, or Grab?
- If discussing delivery, is it DoorDash, UberEats, Grubhub, or Instacart?
- If discussing an airline, is it Delta, United, Southwest, or American?

If you truly cannot identify a specific brand after careful analysis, use "Unidentified" 
(but this should be rare - most complaints mention a brand explicitly or implicitly).
"#;

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    if args.interval_secs == 0 {
        info!("analyzer_reports disabled by option: ANALYZER_INTERVAL_SECS=0; exiting");
        return Ok(());
    }

    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args
        .db_url
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.general.as_ref().map(|g| g.db_url.clone())))
        .context("db_url must be provided via --db-url or DB_URL")?;

    let gemini_key = args
        .gemini_api_key
        .clone()
        .context("gemini api key must be provided via GEMINI_API_KEY")?;

    info!(
        "analyzer_reports start model={} batch_size={} interval={}s",
        args.gemini_model, args.batch_size, args.interval_secs
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);

    let client = reqwest::Client::builder()
        .timeout(StdDuration::from_secs(60))
        .build()?;

    loop {
        if let Err(e) = run_once(&pool, &client, &gemini_key, &args).await {
            warn!("run_once error: {e}");
        }
        sleep(StdDuration::from_secs(args.interval_secs)).await;
    }
}

async fn run_once(
    pool: &Pool,
    client: &reqwest::Client,
    gemini_key: &str,
    args: &Args,
) -> Result<()> {
    let mut conn = pool.get_conn().await?;

    // Fetch reports that need AI review
    let rows: Vec<(i64, String, String, String)> = conn
        .exec(
            r#"SELECT seq, COALESCE(title,''), COALESCE(description,''), COALESCE(source,'')
               FROM report_analysis
               WHERE needs_ai_review = TRUE
               ORDER BY seq DESC
               LIMIT ?"#,
            (args.batch_size as u64,),
        )
        .await?;

    if rows.is_empty() {
        info!("analyzer_reports: nothing to analyze");
        return Ok(());
    }

    info!("analyzer_reports: processing {} reports", rows.len());

    for (seq, title, description, source) in rows {
        // Build Gemini request
        let req_body = build_gemini_request(&title, &description, &source);

        // Try API endpoints
        let endpoints = vec![
            format!(
                "https://generativelanguage.googleapis.com/v1beta/models/{}:generateContent?key={}",
                args.gemini_model, gemini_key
            ),
            format!(
                "https://generativelanguage.googleapis.com/v1/models/{}:generateContent?key={}",
                args.gemini_model, gemini_key
            ),
        ];

        let mut brand_display_name = String::new();
        let mut brand_name = String::new();
        let mut summary = String::new();
        let mut report_title = String::new();
        let mut report_description = String::new();
        let mut classification = "digital".to_string();
        let mut severity_level = 0.5;
        let mut digital_bug_probability = 0.5;
        let mut language = "en".to_string();
        let mut success = false;

        for ep in endpoints.iter() {
            match client.post(ep).json(&req_body).send().await {
                Ok(resp) => {
                    if !resp.status().is_success() {
                        let st = resp.status();
                        let body = resp.text().await.unwrap_or_default();
                        if st.as_u16() == 404 {
                            continue;
                        }
                        warn!("gemini http {}: {}", st, body);
                        break;
                    }

                    let v: JsonValue = resp.json().await.unwrap_or(JsonValue::Null);

                    if let Some(text_out) = extract_gemini_text(&v) {
                        match serde_json::from_str::<JsonValue>(&text_out) {
                            Ok(obj) => {
                                brand_display_name = obj.get("brand_display_name")
                                    .and_then(|x| x.as_str())
                                    .unwrap_or("")
                                    .to_string();
                                brand_name = obj.get("brand_name")
                                    .and_then(|x| x.as_str())
                                    .unwrap_or("")
                                    .to_string();
                                summary = obj.get("summary")
                                    .and_then(|x| x.as_str())
                                    .unwrap_or("")
                                    .chars()
                                    .take(300)
                                    .collect();
                                report_title = obj.get("report_title")
                                    .and_then(|x| x.as_str())
                                    .unwrap_or("")
                                    .chars()
                                    .take(120)
                                    .collect();
                                report_description = obj.get("report_description")
                                    .and_then(|x| x.as_str())
                                    .unwrap_or("")
                                    .chars()
                                    .take(1000)
                                    .collect();
                                classification = obj.get("classification")
                                    .and_then(|x| x.as_str())
                                    .unwrap_or("digital")
                                    .to_lowercase();
                                // ENUM only allows 'physical' or 'digital' - default to 'digital'
                                if classification != "physical" {
                                    classification = "digital".to_string();
                                }
                                severity_level = obj.get("severity_level")
                                    .and_then(|x| x.as_f64())
                                    .unwrap_or(0.5)
                                    .clamp(0.0, 1.0);
                                digital_bug_probability = obj.get("digital_bug_probability")
                                    .and_then(|x| x.as_f64())
                                    .unwrap_or(0.5);
                                if let Some(l) = obj.get("language").and_then(|x| x.as_str()) {
                                    language = l.chars().take(10).collect();
                                }
                                success = true;
                            }
                            Err(e) => {
                                warn!("gemini parse json failed for seq {}: {}", seq, e);
                            }
                        }
                    }
                    break;
                }
                Err(e) => {
                    warn!("gemini request failed for seq {}: {}", seq, e);
                    break;
                }
            }
        }

        if success {
            // Update report_analysis with AI results
            conn.exec_drop(
                r#"UPDATE report_analysis SET
                    brand_name = ?,
                    brand_display_name = ?,
                    summary = ?,
                    title = ?,
                    description = ?,
                    classification = ?,
                    severity_level = ?,
                    digital_bug_probability = ?,
                    language = ?,
                    needs_ai_review = FALSE
                WHERE seq = ?"#,
                (
                    &brand_name,
                    &brand_display_name,
                    &summary,
                    &report_title,
                    &report_description,
                    &classification,
                    severity_level,
                    digital_bug_probability,
                    &language,
                    seq,
                ),
            )
            .await?;
            info!(
                "analyzer_reports: updated seq={} brand={} summary_len={}",
                seq, brand_display_name, summary.len()
            );
        } else {
            // Mark as processed anyway to avoid infinite retries, but keep original content
            conn.exec_drop(
                r#"UPDATE report_analysis SET needs_ai_review = FALSE WHERE seq = ?"#,
                (seq,),
            )
            .await?;
            warn!("analyzer_reports: failed AI for seq={}, marked as processed", seq);
        }

        // Rate limiting to avoid hitting API limits
        sleep(StdDuration::from_millis(200)).await;
    }

    Ok(())
}

fn build_gemini_request(title: &str, description: &str, source: &str) -> JsonValue {
    let context = format!(
        "Report from source '{}'\n\nTitle: {}\n\nDescription: {}",
        source, title, description
    );

    json!({
        "generationConfig": { "response_mime_type": "application/json" },
        "contents": [{
            "role": "user",
            "parts": [
                { "text": PROMPT.to_string() },
                { "text": context }
            ]
        }]
    })
}

fn extract_gemini_text(v: &JsonValue) -> Option<String> {
    let cands = v.get("candidates")?.as_array()?;
    let first = cands.first()?;
    let content = first.get("content")?;
    let parts = content.get("parts")?.as_array()?;
    for p in parts {
        if let Some(t) = p.get("text").and_then(|x| x.as_str()) {
            return Some(t.to_string());
        }
    }
    None
}
