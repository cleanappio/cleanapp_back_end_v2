use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::{json, Value as JsonValue};
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[path = "../indexer_bluesky_schema.rs"]
mod indexer_bluesky_schema;

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
    #[arg(long, env = "ANALYZER_BATCH_SIZE", default_value_t = 10)]
    batch_size: usize,
    #[arg(long, env = "ANALYZER_INTERVAL_SECS", default_value_t = 300)]
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
You are classifying a Bluesky social media post's relevance to CleanApp reports for digital issues (app bugs, UX problems, feature requests).
Consider the post text and any images.
Return ONLY a strict JSON object with the following fields:
{
  "is_relevant": boolean,
  "relevance": number,
  "classification": "physical" | "digital" | "unknown",
  "litter_probability": number,
  "hazard_probability": number,
  "digital_bug_probability": number,
  "severity_level": number,  // MUST be in the range [0.0, 1.0]
  "latitude": number | null,
  "longitude": number | null,
  "report_title": string,      // A short, human-friendly report title (<= 120 chars)
  "report_description": string,// A concise description suitable for a report body (<= 1000 chars)
  "brand_display_name": string,
  "brand_name": string,
  "summary": string,
  "language": string,
  "inferred_contact_emails": []
}

Rules:
- For app complaints/bugs, set classification="digital" and digital_bug_probability high.
- If not relevant to app issues, set is_relevant=false and probabilities near 0.0.
- brand_name is a normalized lowercase version of brand_display_name.
- summary <= 300 chars.
- report_title <= 120 chars; report_description <= 1000 chars.
"#;

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    if args.interval_secs == 0 {
        info!("analyzer_bluesky disabled by option: ANALYZER_INTERVAL_SECS=0; exiting");
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
        "analyzer_bluesky start model={} batch_size={} interval={}s",
        args.gemini_model, args.batch_size, args.interval_secs
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    indexer_bluesky_schema::ensure_bluesky_tables(&pool).await?;

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

    // Fetch unanalyzed posts
    let rows: Vec<(String, String, String, String)> = conn
        .exec(
            r#"SELECT p.uri, COALESCE(p.text,''), COALESCE(p.author_handle,''), COALESCE(p.lang,'')
               FROM indexer_bluesky_post p
               LEFT JOIN indexer_bluesky_analysis a ON a.uri = p.uri
               WHERE a.uri IS NULL OR a.error IS NOT NULL
               ORDER BY p.created_at ASC
               LIMIT ?"#,
            (args.batch_size as u64,),
        )
        .await?;

    if rows.is_empty() {
        info!("analyzer: nothing to analyze");
        return Ok(());
    }

    info!("analyzer: processing {} posts", rows.len());

    for (uri, text, author_handle, lang) in rows {
        // Load images for this post
        let media_hashes: Vec<Vec<u8>> = conn
            .exec(
                r#"SELECT sha256 FROM indexer_bluesky_media
                   WHERE post_uri = ? AND sha256 IS NOT NULL
                   ORDER BY position ASC
                   LIMIT 4"#,
                (uri.clone(),),
            )
            .await?;

        let mut images_base64: Vec<(String, String)> = Vec::new();
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

        // Build Gemini request
        let req_body = build_gemini_request(&text, &author_handle, &lang, &images_base64);

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

        let mut is_relevant = false;
        let mut relevance = 0.0;
        let mut classification = "digital".to_string();
        let mut digital_bug_probability = 0.0;
        let mut severity_level = 0.0;
        let mut brand_display_name = String::new();
        let mut brand_name = String::new();
        let mut summary = String::new();
        let mut report_title = String::new();
        let mut report_description = String::new();
        let mut language = if lang.is_empty() { "en".to_string() } else { lang.clone() };
        let mut raw_llm: JsonValue = JsonValue::Null;
        let mut err_text: Option<String> = None;

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
                        err_text = Some(format!("http {}", st));
                        break;
                    }

                    let v: JsonValue = resp.json().await.unwrap_or(JsonValue::Null);
                    raw_llm = v.clone();

                    if let Some(text_out) = extract_gemini_text(&v) {
                        match serde_json::from_str::<JsonValue>(&text_out) {
                            Ok(obj) => {
                                is_relevant = obj.get("is_relevant").and_then(|x| x.as_bool()).unwrap_or(false);
                                relevance = obj.get("relevance").and_then(|x| x.as_f64()).unwrap_or(0.0);
                                classification = obj.get("classification").and_then(|x| x.as_str()).unwrap_or("digital").to_lowercase();
                                if classification != "physical" && classification != "digital" && classification != "unknown" {
                                    classification = "digital".to_string();
                                }
                                digital_bug_probability = obj.get("digital_bug_probability").and_then(|x| x.as_f64()).unwrap_or(0.0);
                                severity_level = obj.get("severity_level").and_then(|x| x.as_f64()).unwrap_or(0.0).clamp(0.0, 1.0);
                                brand_display_name = obj.get("brand_display_name").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                brand_name = obj.get("brand_name").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                summary = obj.get("summary").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                report_title = obj.get("report_title").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                report_description = obj.get("report_description").and_then(|x| x.as_str()).unwrap_or("").to_string();
                                if let Some(l) = obj.get("language").and_then(|x| x.as_str()) {
                                    // Truncate to 10 chars to fit VARCHAR(10)
                                    language = l.chars().take(10).collect();
                                }
                                err_text = None;
                            }
                            Err(e) => {
                                warn!("gemini parse json failed: {}", e);
                                err_text = Some("invalid_json".to_string());
                            }
                        }
                    } else {
                        err_text = Some("no_text_candidate".to_string());
                    }
                    break;
                }
                Err(e) => {
                    warn!("gemini request failed: {}", e);
                    err_text = Some("request_failed".to_string());
                    break;
                }
            }
        }

        // Insert analysis
        conn.exec_drop(
            r#"INSERT INTO indexer_bluesky_analysis (
                    uri, is_relevant, relevance, classification,
                    digital_bug_probability, severity_level,
                    report_title, report_description, brand_name, brand_display_name,
                    summary, language, raw_llm, error
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
               ON DUPLICATE KEY UPDATE
                    is_relevant=VALUES(is_relevant), relevance=VALUES(relevance),
                    classification=VALUES(classification), digital_bug_probability=VALUES(digital_bug_probability),
                    severity_level=VALUES(severity_level), report_title=VALUES(report_title),
                    report_description=VALUES(report_description), brand_name=VALUES(brand_name),
                    brand_display_name=VALUES(brand_display_name), summary=VALUES(summary),
                    language=VALUES(language), raw_llm=VALUES(raw_llm), error=VALUES(error)"#,
            mysql_async::params::Params::Positional(vec![
                uri.into(),
                is_relevant.into(),
                relevance.into(),
                classification.into(),
                digital_bug_probability.into(),
                severity_level.into(),
                report_title.into(),
                report_description.into(),
                brand_name.into(),
                brand_display_name.into(),
                summary.into(),
                language.into(),
                serde_json::to_string(&raw_llm).unwrap_or("null".into()).into(),
                err_text.into(),
            ]),
        )
        .await?;

        // Rate limiting
        sleep(StdDuration::from_millis(150)).await;
    }

    Ok(())
}

fn build_gemini_request(
    text: &str,
    author_handle: &str,
    lang: &str,
    images: &[(String, String)],
) -> JsonValue {
    let mut parts = vec![json!({ "text": PROMPT.to_string() })];
    let context = format!(
        "Bluesky post by @{} (lang={}):\n{}",
        author_handle, lang, text
    );
    parts.push(json!({ "text": context }));

    for (mime, b64) in images.iter() {
        parts.push(json!({ "inline_data": { "mime_type": mime, "data": b64 } }));
    }

    json!({
        "generationConfig": { "response_mime_type": "application/json" },
        "contents": [{ "role": "user", "parts": parts }]
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
