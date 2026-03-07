use anyhow::{Context, Result};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::{Pool, Row};
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[path = "../indexer_bluesky_schema.rs"]
mod indexer_bluesky_schema;

#[derive(Deserialize, Clone, Debug)]
struct Config {
    general: Option<GeneralConfig>,
    submit: Option<SubmitConfig>,
}

#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig {
    db_url: String,
}

#[derive(Deserialize, Clone, Debug)]
struct SubmitConfig {
    endpoint_url: Option<String>,
    token: Option<String>,
    protocol: Option<String>,
}

#[derive(Parser, Debug, Clone)]
struct Args {
    #[arg(long, default_value = "config.toml")]
    config_path: String,
    #[arg(long, env = "DB_URL")]
    db_url: Option<String>,
    #[arg(long, env = "SUBMIT_ENDPOINT_URL")]
    endpoint_url: Option<String>,
    #[arg(long, env = "SUBMIT_TOKEN")]
    token: Option<String>,
    #[arg(long, env = "SUBMIT_PROTOCOL", default_value = "auto")]
    protocol: String,
    #[arg(long, env = "SUBMIT_BATCH_SIZE", default_value_t = 100)]
    batch_size: usize,
    #[arg(long, env = "SUBMIT_INTERVAL_SECS", default_value_t = 300)]
    interval_secs: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum SubmitProtocol {
    Auto,
    Legacy,
    Wire,
}

impl SubmitProtocol {
    fn parse(raw: &str) -> Result<Self> {
        match raw.trim().to_ascii_lowercase().as_str() {
            "" | "auto" => Ok(Self::Auto),
            "legacy" => Ok(Self::Legacy),
            "wire" => Ok(Self::Wire),
            other => anyhow::bail!("unsupported submit protocol: {}", other),
        }
    }

    fn resolve(self, token: &str) -> Self {
        match self {
            Self::Auto => {
                if token.starts_with("cleanapp_fk_live_") || token.starts_with("cleanapp_fk_test_")
                {
                    Self::Wire
                } else {
                    Self::Legacy
                }
            }
            other => other,
        }
    }
}

#[derive(Debug, Clone)]
struct BlueskyPreparedItem {
    uri: String,
    author_handle: String,
    text: String,
    severity: f64,
    relevance: f64,
    classification: String,
    created_iso: String,
    image_base64: Option<String>,
    summary: String,
    report_title: String,
    report_description: String,
    brand_display_name: String,
    brand_name: String,
    inferred_contact_emails: serde_json::Value,
    url: String,
}

#[derive(Debug, Deserialize, Serialize)]
struct WireBatchResponse {
    items: Vec<WireReceipt>,
    submitted: usize,
    accepted: usize,
    rejected: usize,
    duplicates: usize,
}

#[derive(Debug, Deserialize, Serialize)]
struct WireReceipt {
    receipt_id: String,
    submission_id: String,
    source_id: String,
    received_at: String,
    status: String,
    lane: String,
    #[serde(default)]
    report_id: Option<i64>,
    #[serde(default)]
    idempotency_replay: bool,
}

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
        .or_else(|| {
            cfg.as_ref()
                .and_then(|c| c.general.as_ref().map(|g| g.db_url.clone()))
        })
        .context("db_url must be provided via --db-url or DB_URL")?;

    let endpoint_url = args
        .endpoint_url
        .clone()
        .or_else(|| {
            cfg.as_ref()
                .and_then(|c| c.submit.as_ref().and_then(|s| s.endpoint_url.clone()))
        })
        .context("endpoint_url must be provided via SUBMIT_ENDPOINT_URL")?;

    let token = args
        .token
        .clone()
        .or_else(|| {
            cfg.as_ref()
                .and_then(|c| c.submit.as_ref().and_then(|s| s.token.clone()))
        })
        .context("token must be provided via SUBMIT_TOKEN")?;

    let configured_protocol = args.protocol.clone();
    let protocol = SubmitProtocol::parse(
        cfg.as_ref()
            .and_then(|c| c.submit.as_ref().and_then(|s| s.protocol.as_deref()))
            .unwrap_or(&configured_protocol),
    )?
    .resolve(&token);

    let batch_size = args.batch_size.min(500).max(1);

    info!(
        "submitter_bluesky: start endpoint={} protocol={:?} batch_size={} interval={}s",
        endpoint_url, protocol, batch_size, args.interval_secs
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    indexer_bluesky_schema::ensure_bluesky_tables(&pool).await?;

    let client = reqwest::Client::builder()
        .timeout(StdDuration::from_secs(60))
        .build()?;

    loop {
        if let Err(e) = run_once(&pool, &client, &endpoint_url, &token, protocol, batch_size).await
        {
            warn!("run_once error: {e}");
        }
        sleep(StdDuration::from_secs(args.interval_secs)).await;
    }
}

async fn run_once(
    pool: &Pool,
    client: &reqwest::Client,
    endpoint_url: &str,
    token: &str,
    protocol: SubmitProtocol,
    batch_size: usize,
) -> Result<()> {
    let mut conn = pool.get_conn().await?;

    // Fetch analyzed posts not yet submitted
    // Legacy mode uses external_ingest_index exclusion.
    // Wire mode adds a local receipt ledger keyed by Bluesky URI so retries are idempotent
    // even though Wire itself does not populate external_ingest_index.
    let rows: Vec<Row> = conn.exec(
        r#"SELECT p.uri, COALESCE(p.author_handle,''), COALESCE(p.text,''),
                  COALESCE(a.severity_level, 0.0), COALESCE(a.relevance, 0.0),
                  COALESCE(a.classification, 'digital'),
                  DATE_FORMAT(p.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                  (SELECT data FROM indexer_media_blob b WHERE b.sha256 = 
                   (SELECT m.sha256 FROM indexer_bluesky_media m WHERE m.post_uri=p.uri ORDER BY position ASC LIMIT 1) LIMIT 1),
                  COALESCE(a.summary, ''), COALESCE(a.report_title, ''),
                  COALESCE(a.report_description, ''), COALESCE(a.brand_display_name, ''),
                  COALESCE(a.brand_name, ''), COALESCE(a.inferred_contact_emails, '[]')
           FROM indexer_bluesky_post p
           JOIN indexer_bluesky_analysis a ON a.uri = p.uri
           LEFT JOIN external_ingest_index ei 
             ON ei.source = 'bluesky' AND ei.external_id COLLATE utf8mb4_unicode_ci = p.uri
           LEFT JOIN indexer_bluesky_wire_submission ws
             ON ws.uri COLLATE utf8mb4_unicode_ci = p.uri
           WHERE a.is_relevant = TRUE
             AND ei.seq IS NULL
             AND (? = 'legacy' OR ws.uri IS NULL)
           ORDER BY p.created_at ASC, p.uri ASC
           LIMIT ?"#,
        (protocol_name(protocol), batch_size as u64),
    )
    .await?;

    if rows.is_empty() {
        info!("submitter: no posts to submit");
        return Ok(());
    }

    info!("submitter: building payload for {} posts", rows.len());

    // Build payload
    let mut items: Vec<BlueskyPreparedItem> = Vec::with_capacity(rows.len());
    for row in rows.iter() {
        let uri: String = row.get::<String, _>(0).unwrap_or_default();
        let author_handle: String = row.get::<String, _>(1).unwrap_or_default();
        let text: String = row.get::<String, _>(2).unwrap_or_default();
        let severity: f64 = row.get::<Option<f64>, _>(3).unwrap_or(None).unwrap_or(0.0);
        let relevance: f64 = row.get::<Option<f64>, _>(4).unwrap_or(None).unwrap_or(0.0);
        let classification: String = row
            .get::<Option<String>, _>(5)
            .unwrap_or(None)
            .unwrap_or_else(|| "digital".to_string());
        let created_iso: String = row
            .get::<Option<String>, _>(6)
            .unwrap_or(None)
            .unwrap_or_default();
        let img_opt: Option<Vec<u8>> = row.get::<Option<Vec<u8>>, _>(7).unwrap_or(None);
        let summary: String = row
            .get::<Option<String>, _>(8)
            .unwrap_or(None)
            .unwrap_or_default();
        let report_title: String = row
            .get::<Option<String>, _>(9)
            .unwrap_or(None)
            .unwrap_or_default();
        let report_description: String = row
            .get::<Option<String>, _>(10)
            .unwrap_or(None)
            .unwrap_or_default();
        let brand_display_name: String = row
            .get::<Option<String>, _>(11)
            .unwrap_or(None)
            .unwrap_or_default();
        let brand_name: String = row
            .get::<Option<String>, _>(12)
            .unwrap_or(None)
            .unwrap_or_default();
        let inferred_contact_emails: String = row
            .get::<Option<String>, _>(13)
            .unwrap_or(None)
            .unwrap_or_else(|| "[]".to_string());

        // Build web URL from AT URI
        // at://did:plc:xxx/app.bsky.feed.post/yyy -> https://bsky.app/profile/handle/post/yyy
        // Note: Bluesky URLs work with either handle or DID
        let post_id = uri.rsplit('/').next().unwrap_or("");
        // Use author_handle if available, otherwise extract DID from URI
        let profile_id = if !author_handle.is_empty() {
            author_handle.clone()
        } else {
            // Extract DID from AT URI: at://did:plc:xxx/app.bsky.feed.post/yyy
            uri.strip_prefix("at://")
                .and_then(|s| s.split('/').next())
                .unwrap_or("")
                .to_string()
        };
        let url = format!("https://bsky.app/profile/{}/post/{}", profile_id, post_id);

        let title = if !report_title.is_empty() {
            truncate_chars(&report_title, 120)
        } else {
            truncate_chars(&text, 120)
        };

        let mut content = if !report_description.is_empty() {
            report_description
        } else {
            text.clone()
        };
        content = format!("{} : {}", content, url);

        let image_base64 = img_opt.as_ref().map(|b| STANDARD.encode(b));

        items.push(BlueskyPreparedItem {
            uri,
            author_handle,
            text: truncate_chars(&content, 4000),
            severity,
            relevance,
            classification,
            created_iso,
            image_base64,
            summary,
            report_title: title,
            report_description: truncate_chars(&content, 4000),
            brand_display_name,
            brand_name,
            inferred_contact_emails: serde_json::from_str::<serde_json::Value>(
                &inferred_contact_emails,
            )
            .unwrap_or(json!([])),
            url,
        });
    }

    match protocol {
        SubmitProtocol::Legacy => {
            submit_legacy(client, endpoint_url, token, &items).await?;
        }
        SubmitProtocol::Wire => {
            submit_wire(pool, client, endpoint_url, token, &items).await?;
        }
        SubmitProtocol::Auto => unreachable!("auto should have been resolved before run_once"),
    }

    Ok(())
}

async fn submit_legacy(
    client: &reqwest::Client,
    endpoint_url: &str,
    token: &str,
    items: &[BlueskyPreparedItem],
) -> Result<()> {
    let payload = json!({
        "source": "bluesky",
        "items": items.iter().map(|it| {
            json!({
                "external_id": it.uri,
                "title": it.report_title,
                "content": it.text,
                "url": it.url,
                "created_at": it.created_iso,
                "updated_at": it.created_iso,
                "score": normalize_score(it.severity, it.relevance),
                "metadata": {
                    "author_handle": it.author_handle,
                    "classification": it.classification,
                    "relevance": it.relevance,
                    "severity_level": it.severity,
                    "summary": it.summary,
                    "brand_display_name": it.brand_display_name,
                    "brand_name": it.brand_name,
                    "inferred_contact_emails": it.inferred_contact_emails
                },
                "tags": ["bluesky"],
                "skip_ai": true,
                "image_base64": it.image_base64
            })
        }).collect::<Vec<_>>(),
    });

    let resp = client
        .post(format!(
            "{}/api/v3/reports/bulk_ingest",
            endpoint_url.trim_end_matches('/')
        ))
        .bearer_auth(token)
        .json(&payload)
        .send()
        .await;

    match resp {
        Ok(r) => {
            if !r.status().is_success() {
                let status = r.status();
                let text = r.text().await.unwrap_or_default();
                warn!("legacy submit failed http {}: {}", status, text);
                return Ok(());
            }
            let v: serde_json::Value = r.json().await.unwrap_or_else(|_| json!({}));
            let inserted = v.get("inserted").and_then(|x| x.as_u64()).unwrap_or(0);
            let updated = v.get("updated").and_then(|x| x.as_u64()).unwrap_or(0);
            info!(
                "legacy submitted batch: rows={} inserted={} updated={}",
                items.len(),
                inserted,
                updated
            );
        }
        Err(e) => {
            warn!("legacy submit http error: {}", e);
        }
    }

    Ok(())
}

async fn submit_wire(
    pool: &Pool,
    client: &reqwest::Client,
    endpoint_url: &str,
    token: &str,
    items: &[BlueskyPreparedItem],
) -> Result<()> {
    let payload = json!({
        "items": items.iter().map(bluesky_item_to_wire_submission).collect::<Vec<_>>(),
    });

    let resp = client
        .post(format!(
            "{}/api/v1/agent-reports:batchSubmit",
            endpoint_url.trim_end_matches('/')
        ))
        .bearer_auth(token)
        .json(&payload)
        .send()
        .await;

    match resp {
        Ok(r) => {
            if !r.status().is_success() {
                let status = r.status();
                let text = r.text().await.unwrap_or_default();
                warn!("wire submit failed http {}: {}", status, text);
                return Ok(());
            }
            let wire: WireBatchResponse = r.json().await.context("parse wire batch response")?;
            persist_wire_receipts(pool, &wire).await?;
            info!(
                "wire submitted batch: rows={} submitted={} accepted={} duplicates={} rejected={}",
                items.len(),
                wire.submitted,
                wire.accepted,
                wire.duplicates,
                wire.rejected
            );
        }
        Err(e) => {
            warn!("wire submit http error: {}", e);
        }
    }

    Ok(())
}

async fn persist_wire_receipts(pool: &Pool, wire: &WireBatchResponse) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    let stmt = conn
        .prep(
            r#"INSERT INTO indexer_bluesky_wire_submission
               (uri, source_id, receipt_id, report_id, status, lane, idempotency_replay, response_json)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?)
               ON DUPLICATE KEY UPDATE
                   source_id = VALUES(source_id),
                   receipt_id = VALUES(receipt_id),
                   report_id = VALUES(report_id),
                   status = VALUES(status),
                   lane = VALUES(lane),
                   idempotency_replay = VALUES(idempotency_replay),
                   response_json = VALUES(response_json),
                   updated_at = CURRENT_TIMESTAMP"#,
        )
        .await?;

    for receipt in &wire.items {
        let response_json = serde_json::to_string(receipt)?;
        conn.exec_drop(
            &stmt,
            (
                &receipt.source_id,
                &receipt.source_id,
                &receipt.receipt_id,
                receipt.report_id,
                &receipt.status,
                &receipt.lane,
                receipt.idempotency_replay,
                response_json,
            ),
        )
        .await?;
    }

    Ok(())
}

fn bluesky_item_to_wire_submission(it: &BlueskyPreparedItem) -> serde_json::Value {
    let mut evidence = vec![json!({
        "evidence_id": format!("ev_{}", stable_slug(&it.uri)),
        "type": "url",
        "uri": it.url,
        "captured_at": it.created_iso,
    })];
    if let Some(image_base64) = &it.image_base64 {
        evidence.push(json!({
            "evidence_id": format!("ev_img_{}", stable_slug(&it.uri)),
            "type": "image",
            "mime_type": "image/jpeg",
            "captured_at": it.created_iso,
            "uri": format!("data:image/jpeg;base64,{}", image_base64),
        }));
    }

    json!({
        "schema_version": "cleanapp-wire.v1",
        "source_id": it.uri,
        "submitted_at": utc_now_iso(),
        "observed_at": it.created_iso,
        "agent": {
            "agent_id": "news-indexer-bluesky",
            "agent_name": "CleanApp Bluesky Submitter",
            "agent_type": "fetcher",
            "operator_type": "internal",
            "auth_method": "api_key",
            "software_version": env!("CARGO_PKG_VERSION"),
            "execution_mode": "batch"
        },
        "provenance": {
            "generation_method": "bluesky_submitter",
            "upstream_sources": [
                {"kind": "bluesky_uri", "value": it.uri},
                {"kind": "web_url", "value": it.url}
            ],
            "chain_of_custody": ["index_bluesky", "analyzer_bluesky", "submitter_bluesky"]
        },
        "report": {
            "domain": "digital",
            "problem_type": "social_media_report",
            "problem_subtype": it.classification,
            "title": it.report_title,
            "description": it.report_description,
            "language": "en",
            "severity": severity_bucket(it.severity),
            "confidence": normalize_score(it.severity, it.relevance),
            "target_entity": {
                "target_type": "brand",
                "name": if !it.brand_display_name.is_empty() { &it.brand_display_name } else { &it.brand_name }
            },
            "digital_context": {
                "platform": "bluesky",
                "url": it.url,
                "author_handle": it.author_handle,
                "classification": it.classification,
                "summary": it.summary,
                "brand_name": it.brand_name,
                "brand_display_name": it.brand_display_name,
                "inferred_contact_emails": it.inferred_contact_emails
            },
            "evidence_bundle": evidence,
            "tags": ["bluesky", it.classification],
        },
        "delivery": {
            "requested_lane": "auto"
        }
    })
}

fn protocol_name(protocol: SubmitProtocol) -> &'static str {
    match protocol {
        SubmitProtocol::Auto => "auto",
        SubmitProtocol::Legacy => "legacy",
        SubmitProtocol::Wire => "wire",
    }
}

fn severity_bucket(severity: f64) -> &'static str {
    match severity {
        s if s >= 0.85 => "critical",
        s if s >= 0.65 => "high",
        s if s >= 0.35 => "medium",
        _ => "low",
    }
}

fn utc_now_iso() -> String {
    chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true)
}

fn stable_slug(input: &str) -> String {
    input
        .chars()
        .map(|c| if c.is_ascii_alphanumeric() { c } else { '_' })
        .collect()
}

fn normalize_score(severity: f64, relevance: f64) -> f64 {
    let mut s = if severity > 0.0 {
        severity
    } else {
        0.7 + 0.3 * relevance.clamp(0.0, 1.0)
    };
    s = s.clamp(0.7, 1.0);
    s
}

fn truncate_chars(s: &str, max_chars: usize) -> String {
    if s.chars().count() <= max_chars {
        return s.to_string();
    }
    s.chars().take(max_chars).collect()
}
