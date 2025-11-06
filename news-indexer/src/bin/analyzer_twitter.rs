use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::{json, Value as JsonValue};
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[path = "../indexer_twitter_schema.rs"]
mod indexer_twitter_schema;

#[derive(Parser, Debug, Clone)]
struct Args {
    #[arg(long, default_value = "config.toml")] config_path: String,
    #[arg(long, env = "DB_URL")] db_url: Option<String>,
    #[arg(long, env = "GEMINI_API_KEY")] gemini_api_key: Option<String>,
    #[arg(long, env = "GEMINI_MODEL", default_value = "gemini-1.5-flash")] gemini_model: String,
    #[arg(long, env = "ANALYZER_BATCH_SIZE", default_value_t = 10)] batch_size: usize,
    #[arg(long, env = "ANALYZER_INTERVAL_SECS", default_value_t = 300)] interval_secs: u64,
    #[arg(long, env = "ANALYZER_ONLY_WITH_IMAGES", default_value_t = false)] only_with_images: bool,
}

#[derive(Deserialize, Clone, Debug)]
struct Config { general: Option<GeneralConfig> }
#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig { db_url: String }

const PROMPT: &str = r#"
You are classifying a tweet's relevance to CleanApp reports. Consider the tweet text and up to four images.
Return ONLY a strict JSON object with the following fields:
{
  "is_relevant": boolean,
  "relevance": number,
  "classification": "physical" | "digital" | "unknown",
  "litter_probability": number,
  "hazard_probability": number,
  "digital_bug_probability": number,
  "severity_level": number,
  "brand_display_name": string,
  "brand_name": string,
  "summary": string,
  "language": string,
  "inferred_contact_emails": []
}

Rules:
- If not relevant, set is_relevant=false and probabilities near 0.0.
- brand_name is a normalized lowercase version of brand_display_name.
- summary <= 300 chars.
"#;

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();
    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args
        .db_url
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.general.as_ref().map(|g| g.db_url.clone())))
        .context("db_url must be provided via --db-url or config.general.db_url")?;
    let gemini_key = args
        .gemini_api_key
        .clone()
        .context("gemini api key must be provided via --gemini-api-key or GEMINI_API_KEY")?;

    info!(
        "analyzer_twitter start model={} batch_size={} interval={}s only_with_images={}",
        args.gemini_model, args.batch_size, args.interval_secs, args.only_with_images
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    indexer_twitter_schema::ensure_twitter_tables(&pool).await?;

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

async fn run_once(pool: &Pool, client: &reqwest::Client, gemini_key: &str, args: &Args) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    let mut rows: Vec<(i64, String, String, String, String)> = if args.only_with_images {
        conn.exec(
            r#"SELECT t.tweet_id,
                       COALESCE(t.text,''),
                       COALESCE(t.username,''),
                       COALESCE(t.lang,''),
                       COALESCE(t.url,'')
                FROM indexer_twitter_tweet t
                LEFT JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                WHERE a.tweet_id IS NULL
                  AND EXISTS (SELECT 1 FROM indexer_twitter_media m WHERE m.tweet_id = t.tweet_id AND m.type = 'photo')
                ORDER BY t.created_at ASC
                LIMIT ?"#,
            (args.batch_size as u64,),
        )
        .await?
    } else {
        conn.exec(
            r#"SELECT t.tweet_id,
                       COALESCE(t.text,''),
                       COALESCE(t.username,''),
                       COALESCE(t.lang,''),
                       COALESCE(t.url,'')
                FROM indexer_twitter_tweet t
                LEFT JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                WHERE a.tweet_id IS NULL
                ORDER BY t.created_at ASC
                LIMIT ?"#,
            (args.batch_size as u64,),
        )
        .await?
    };

    if rows.is_empty() {
        info!("analyzer: nothing to analyze");
        return Ok(());
    }

    for (tweet_id, text, username, lang, url) in rows.into_iter() {
        // Load up to 4 images
        let media_hashes: Vec<Vec<u8>> = conn
            .exec(
                r#"SELECT sha256 FROM indexer_twitter_media
                   WHERE tweet_id = ? AND type = 'photo' AND sha256 IS NOT NULL
                   ORDER BY position ASC
                   LIMIT 4"#,
                (tweet_id,),
            )
            .await?;
        let mut images_base64: Vec<(String, String)> = Vec::new(); // (mime, data)
        for sha in media_hashes.iter() {
            let row: Option<(Option<String>, Vec<u8>)> = conn
                .exec_first(
                    r#"SELECT mime, data FROM indexer_media_blob WHERE sha256 = ?"#,
                    (sha.clone(),),
                )
                .await?;
            if let Some((mime_opt, data)) = row {
                let mime = mime_opt.unwrap_or_else(|| "image/jpeg".to_string());
                use base64::engine::general_purpose::STANDARD;
                use base64::Engine;
                let b64 = STANDARD.encode(&data);
                images_base64.push((mime, b64));
            }
        }

        let req_body = build_gemini_request(&text, &username, &lang, &url, &images_base64);
        let endpoint = format!(
            "https://generativelanguage.googleapis.com/v1beta/models/{}:generateContent?key={}",
            args.gemini_model, gemini_key
        );
        let mut is_relevant = false;
        let mut relevance = 0.0;
        let mut classification = "unknown".to_string();
        let mut litter_probability = 0.0;
        let mut hazard_probability = 0.0;
        let mut digital_bug_probability = 0.0;
        let mut severity_level = 0.0;
        let mut brand_display_name = String::new();
        let mut brand_name = String::new();
        let mut summary = String::new();
        let mut language = if lang.is_empty() { "en".to_string() } else { lang.clone() };
        let mut inferred_contact_emails = JsonValue::Array(vec![]);
        let mut raw_llm: JsonValue = JsonValue::Null;
        let mut err_text: Option<String> = None;

        match client.post(&endpoint).json(&req_body).send().await {
            Ok(resp) => {
                if !resp.status().is_success() {
                    let st = resp.status();
                    let body = resp.text().await.unwrap_or_default();
                    warn!("gemini http {}: {}", st, body);
                    err_text = Some(format!("http {}", st));
                } else {
                    let v: JsonValue = resp.json().await.unwrap_or(JsonValue::Null);
                    raw_llm = v.clone();
                    if let Some(text_out) = extract_gemini_text(&v) {
                        match serde_json::from_str::<JsonValue>(&text_out) {
                            Ok(obj) => {
                                is_relevant = obj.get("is_relevant").and_then(|x| x.as_bool()).unwrap_or(false);
                                relevance = obj.get("relevance").and_then(|x| x.as_f64()).unwrap_or(0.0);
                                classification = obj.get("classification").and_then(|x| x.as_str()).unwrap_or("unknown").to_string();
                                litter_probability = obj.get("litter_probability").and_then(|x| x.as_f64()).unwrap_or(0.0);
                                hazard_probability = obj.get("hazard_probability").and_then(|x| x.as_f64()).unwrap_or(0.0);
                                digital_bug_probability = obj.get("digital_bug_probability").and_then(|x| x.as_f64()).unwrap_or(0.0);
                                severity_level = obj.get("severity_level").and_then(|x| x.as_f64()).unwrap_or(0.0);
                                brand_display_name = obj.get("brand_display_name").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                brand_name = obj.get("brand_name").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                summary = obj.get("summary").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                if let Some(l) = obj.get("language").and_then(|x| x.as_str()) { language = l.to_string(); }
                                if let Some(emails) = obj.get("inferred_contact_emails").cloned() { inferred_contact_emails = emails; }
                            }
                            Err(e) => {
                                warn!("gemini parse json failed: {}", e);
                                err_text = Some("invalid_json".to_string());
                            }
                        }
                    } else {
                        err_text = Some("no_text_candidate".to_string());
                    }
                }
            }
            Err(e) => {
                warn!("gemini request failed: {}", e);
                err_text = Some("request_failed".to_string());
            }
        }

        // Insert analysis
        conn.exec_drop(
            r#"INSERT INTO indexer_twitter_analysis (
                    tweet_id, is_relevant, relevance, classification, litter_probability,
                    hazard_probability, digital_bug_probability, severity_level, brand_name,
                    brand_display_name, summary, language, inferred_contact_emails, raw_llm, analyzed_at, error
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), ?)
               ON DUPLICATE KEY UPDATE
                    is_relevant=VALUES(is_relevant), relevance=VALUES(relevance), classification=VALUES(classification),
                    litter_probability=VALUES(litter_probability), hazard_probability=VALUES(hazard_probability),
                    digital_bug_probability=VALUES(digital_bug_probability), severity_level=VALUES(severity_level),
                    brand_name=VALUES(brand_name), brand_display_name=VALUES(brand_display_name), summary=VALUES(summary),
                    language=VALUES(language), inferred_contact_emails=VALUES(inferred_contact_emails), raw_llm=VALUES(raw_llm),
                    error=VALUES(error)"#,
            mysql_async::params::Params::Positional(vec![
                tweet_id.into(),
                is_relevant.into(),
                relevance.into(),
                classification.into(),
                litter_probability.into(),
                hazard_probability.into(),
                digital_bug_probability.into(),
                severity_level.into(),
                brand_name.into(),
                brand_display_name.into(),
                summary.into(),
                language.into(),
                serde_json::to_string(&inferred_contact_emails).unwrap_or("[]".into()).into(),
                serde_json::to_string(&raw_llm).unwrap_or("null".into()).into(),
                err_text.into(),
            ]),
        )
        .await?;

        // politeness delay
        sleep(StdDuration::from_millis(150)).await;
    }

    Ok(())
}

fn build_gemini_request(
    text: &str,
    username: &str,
    lang: &str,
    url: &str,
    images: &Vec<(String, String)>,
) -> JsonValue {
    let mut parts = vec![json!({ "text": PROMPT.to_string() })];
    let context = format!("Tweet by @{} (lang={}):\n{}\nURL: {}", username, lang, text, url);
    parts.push(json!({ "text": context }));
    for (mime, b64) in images.iter() {
        parts.push(json!({ "inline_data": { "mime_type": mime, "data": b64 } }));
    }
    json!({ "contents": [{ "parts": parts }] })
}

fn extract_gemini_text(v: &JsonValue) -> Option<String> {
    // candidates[0].content.parts[*].text
    let cands = v.get("candidates")?.as_array()?;
    let first = cands.get(0)?;
    let content = first.get("content")?;
    let parts = content.get("parts")?.as_array()?;
    for p in parts {
        if let Some(t) = p.get("text").and_then(|x| x.as_str()) {
            return Some(t.to_string());
        }
    }
    None
}


