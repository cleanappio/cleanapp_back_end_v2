use anyhow::{Context, Result, anyhow};
use async_compression::tokio::bufread::{GzipDecoder, XzDecoder, ZstdDecoder};
use chrono::{DateTime, TimeZone, Utc};
use clap::{Parser, ValueEnum};
use log::{info, warn};
use reqwest::StatusCode;
use serde::Deserialize;
use serde_json::{Value as JsonValue, json};
use std::collections::HashSet;
use std::path::PathBuf;
use std::sync::{
    Arc,
    atomic::{AtomicUsize, Ordering},
};
use std::time::{Duration, Instant};
use tokio::fs;
use tokio::io::{self, AsyncBufRead, AsyncBufReadExt, BufReader};
use tokio::sync::Semaphore;
use tokio_util::io::StreamReader;
use urlencoding::encode;
use futures_util::TryStreamExt;

#[derive(Parser, Debug, Clone)]
#[command(author, version, about = "Stream Reddit dumps into CleanApp bulk_ingest", long_about = None)]
struct Args {
    /// Input file paths or URLs (supports .gz, .zst, .xz, or plain NDJSON)
    #[arg(long = "inputs", required = true)]
    inputs: Vec<String>,

    /// CleanApp backend URL (env: CLEANAPP_BACKEND_URL)
    #[arg(long = "backend-url", env = "CLEANAPP_BACKEND_URL")]
    backend_url: Option<String>,

    /// Fetcher token (env: CLEANAPP_FETCHER_TOKEN)
    #[arg(long = "fetcher-token", env = "CLEANAPP_FETCHER_TOKEN")]
    fetcher_token: Option<String>,

    /// Source label for bulk_ingest (defaults to reddit_dump)
    #[arg(long = "source", default_value = "reddit_dump")]
    source: String,

    /// Maximum items to ingest (for testing)
    #[arg(long = "max-items")]
    max_items: Option<usize>,

    /// Maximum number of concurrent input streams
    #[arg(long = "concurrency", default_value_t = 8)]
    concurrency: usize,

    /// Batch size (capped at 1000 to respect backend limit)
    #[arg(long = "batch-size", default_value_t = 1000)]
    batch_size: usize,

    /// Which record types to ingest
    #[arg(long = "mode", value_enum, default_value_t = Mode::Both)]
    mode: Mode,

    /// Optional subreddit allowlist file (one subreddit per line)
    #[arg(long = "subreddit-allowlist")]
    subreddit_allowlist: Option<PathBuf>,

    /// Optional keyword file (case-insensitive, one keyword per line)
    #[arg(long = "keyword-file")]
    keyword_file: Option<PathBuf>,

    /// Dry run - print first N converted items instead of submitting
    #[arg(long = "dry-run")]
    dry_run: bool,

    /// Optional bearer token for accessing private GCS objects
    #[arg(long = "gcs-token", env = "GCS_BEARER_TOKEN")]
    gcs_token: Option<String>,

    /// Maximum requests per second (0 = unlimited). Helps prevent overwhelming the backend.
    #[arg(long = "rps", default_value_t = 10)]
    requests_per_second: usize,

    /// Only include records created after this date (UTC, YYYY-MM-DD format)
    #[arg(long = "after")]
    after: Option<String>,

    /// Only include records created before this date (UTC, YYYY-MM-DD format)
    #[arg(long = "before")]
    before: Option<String>,
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, ValueEnum)]
enum Mode {
    Comments,
    Submissions,
    Both,
}

#[derive(Debug, Deserialize)]
struct RedditRecord {
    id: Option<String>,
    name: Option<String>,
    body: Option<String>,
    selftext: Option<String>,
    title: Option<String>,
    permalink: Option<String>,
    created_utc: Option<f64>,
    score: Option<f64>,
    subreddit: Option<String>,
    author: Option<String>,
    link_id: Option<String>,
    parent_id: Option<String>,
    num_comments: Option<i64>,
}

#[derive(Debug, Clone)]
struct BulkItem {
    external_id: String,
    title: String,
    content: String,
    url: String,
    created_at: String,
    score: f64,
    metadata: JsonValue,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    if args.inputs.is_empty() {
        return Err(anyhow!("at least one input is required"));
    }

    let backend_url = args
        .backend_url
        .clone()
        .context("--backend-url or CLEANAPP_BACKEND_URL is required")?;
    let fetcher_token = args
        .fetcher_token
        .clone()
        .context("--fetcher-token or CLEANAPP_FETCHER_TOKEN is required")?;

    let batch_size = args.batch_size.clamp(1, 1000);
    let max_items = args.max_items.unwrap_or(usize::MAX);

    let allowlist = Arc::new(load_filter(&args.subreddit_allowlist).await?);
    let keywords = Arc::new(load_filter(&args.keyword_file).await?);

    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(60))
        .build()?;

    let remaining = Arc::new(AtomicUsize::new(max_items));
    let semaphore = Arc::new(Semaphore::new(args.concurrency.max(1)));

    let inputs = args.inputs.clone();
    let rps = args.requests_per_second;
    let mut tasks = Vec::with_capacity(inputs.len());
    for input in inputs {
        let args = args.clone();
        let client = client.clone();
        let allowlist = allowlist.clone();
        let keywords = keywords.clone();
        let remaining = remaining.clone();
        let semaphore = semaphore.clone();
        let backend_url = backend_url.clone();
        let fetcher_token = fetcher_token.clone();

        tasks.push(tokio::spawn(async move {
            let permit = semaphore.acquire().await.expect("semaphore poisoned");
            let res = process_input(
                &input,
                &args,
                &client,
                &allowlist,
                &keywords,
                &remaining,
                batch_size,
                &backend_url,
                &fetcher_token,
                rps,
            )
            .await;
            drop(permit);
            res.map_err(|e| anyhow!("{}: {e}", input))
        }));
    }

    let mut total_converted = 0usize;
    for task in tasks {
        match task.await? {
            Ok(count) => total_converted += count,
            Err(e) => return Err(e),
        }
    }

    info!("completed ingestion; converted {total_converted} items");
    Ok(())
}

async fn load_filter(path: &Option<PathBuf>) -> Result<HashSet<String>> {
    if let Some(path) = path {
        let data = fs::read_to_string(path).await?;
        let set = data
            .lines()
            .filter_map(|l| {
                let trimmed = l.trim();
                if trimmed.is_empty() || trimmed.starts_with('#') {
                    None
                } else {
                    Some(trimmed.to_ascii_lowercase())
                }
            })
            .collect();
        Ok(set)
    } else {
        Ok(HashSet::new())
    }
}

async fn process_input(
    input: &str,
    args: &Args,
    client: &reqwest::Client,
    allowlist: &HashSet<String>,
    keywords: &HashSet<String>,
    remaining: &AtomicUsize,
    batch_size: usize,
    backend_url: &str,
    fetcher_token: &str,
    rps: usize,
) -> Result<usize> {
    let reader = open_reader(input, client, args.gcs_token.as_deref()).await?;
    let mut lines = reader.lines();
    let mut buffer: Vec<BulkItem> = Vec::with_capacity(batch_size);
    let mut printed = 0usize;
    let mut converted = 0usize;
    let mut last_submit = Instant::now();
    let min_interval = if rps > 0 {
        Duration::from_millis((1000 / rps) as u64)
    } else {
        Duration::ZERO
    };

    let endpoint = format!(
        "{}/api/v3/reports/bulk_ingest",
        backend_url.trim_end_matches('/')
    );

    while let Some(line) = lines.next_line().await? {
        if line.trim().is_empty() {
            continue;
        }

        let record: RedditRecord = match serde_json::from_str(&line) {
            Ok(v) => v,
            Err(e) => {
                warn!("skipping malformed line: {e}");
                continue;
            }
        };

        // Date filtering based on created_utc
        if let Some(created_utc) = record.created_utc {
            if let Some(ref after_str) = args.after {
                if let Ok(after_date) = chrono::NaiveDate::parse_from_str(after_str, "%Y-%m-%d") {
                    let after_ts = after_date.and_hms_opt(0, 0, 0).unwrap().and_utc().timestamp() as f64;
                    if created_utc < after_ts {
                        continue;
                    }
                }
            }
            if let Some(ref before_str) = args.before {
                if let Ok(before_date) = chrono::NaiveDate::parse_from_str(before_str, "%Y-%m-%d") {
                    let before_ts = before_date.and_hms_opt(0, 0, 0).unwrap().and_utc().timestamp() as f64;
                    if created_utc >= before_ts {
                        continue;
                    }
                }
            }
        }

        if let Some(item) = convert_record(&record, args.mode)? {
            if !allowlist.is_empty() {
                let subreddit = record
                    .subreddit
                    .as_deref()
                    .unwrap_or_default()
                    .to_ascii_lowercase();
                if !allowlist.contains(&subreddit) {
                    continue;
                }
            }

            if !keywords.is_empty() {
                let haystack = format!(
                    "{}\n{}",
                    item.title.to_ascii_lowercase(),
                    item.content.to_ascii_lowercase()
                );
                if !keywords.iter().any(|kw| haystack.contains(kw)) {
                    continue;
                }
            }

            if remaining
                .fetch_update(Ordering::SeqCst, Ordering::SeqCst, |v| {
                    if v == 0 { None } else { Some(v - 1) }
                })
                .is_err()
            {
                break;
            }
            converted += 1;

            if args.dry_run {
                if printed < args.max_items.unwrap_or(usize::MAX) {
                    println!(
                        "{}",
                        serde_json::to_string_pretty(&json!({
                            "external_id": item.external_id,
                            "title": item.title,
                            "content": item.content,
                            "url": item.url,
                            "created_at": item.created_at,
                            "score": item.score,
                            "metadata": item.metadata,
                        }))?
                    );
                    printed += 1;
                }
            } else {
                buffer.push(item);
                if buffer.len() >= batch_size {
                    // Rate limiting: ensure minimum interval between requests
                    if min_interval > Duration::ZERO {
                        let elapsed = last_submit.elapsed();
                        if elapsed < min_interval {
                            tokio::time::sleep(min_interval - elapsed).await;
                        }
                    }
                    submit_batch(&endpoint, fetcher_token, &args.source, &buffer, client).await?;
                    last_submit = Instant::now();
                    buffer.clear();
                }
            }
        }
    }

    if !args.dry_run && !buffer.is_empty() {
        submit_batch(&endpoint, fetcher_token, &args.source, &buffer, client).await?;
    }

    Ok(converted)
}

fn convert_record(record: &RedditRecord, mode: Mode) -> Result<Option<BulkItem>> {
    let is_comment = record.body.is_some() || record.parent_id.is_some();
    let is_submission = record.title.is_some() || record.selftext.is_some();

    match mode {
        Mode::Comments if !is_comment => return Ok(None),
        Mode::Submissions if !is_submission => return Ok(None),
        _ => {}
    }

    if is_comment {
        build_comment_item(record).map(Some)
    } else if is_submission {
        build_submission_item(record).map(Some)
    } else {
        Ok(None)
    }
}

fn build_comment_item(record: &RedditRecord) -> Result<BulkItem> {
    let external_id = record
        .name
        .clone()
        .or_else(|| record.id.as_ref().map(|id| format!("t1_{}", id)))
        .ok_or_else(|| anyhow!("comment missing id"))?;

    let created_at = format_timestamp(record.created_utc)?;
    let url = format!(
        "https://reddit.com{}",
        record.permalink.as_deref().unwrap_or("")
    );
    let metadata = json!({
        "subreddit": record.subreddit.clone().unwrap_or_default(),
        "author": record.author.clone().unwrap_or_default(),
        "link_id": record.link_id.clone().unwrap_or_default(),
        "parent_id": record.parent_id.clone().unwrap_or_default(),
        "kind": "comment",
    });

    Ok(BulkItem {
        external_id,
        title: "Reddit comment".to_string(),
        content: sanitize_for_mysql(&record.body.clone().unwrap_or_default()),
        url,
        created_at,
        score: record.score.unwrap_or(0.0),
        metadata,
    })
}

fn build_submission_item(record: &RedditRecord) -> Result<BulkItem> {
    let external_id = record
        .name
        .clone()
        .or_else(|| record.id.as_ref().map(|id| format!("t3_{}", id)))
        .ok_or_else(|| anyhow!("submission missing id"))?;

    let created_at = format_timestamp(record.created_utc)?;
    let url = format!(
        "https://reddit.com{}",
        record.permalink.as_deref().unwrap_or("")
    );
    let metadata = json!({
        "subreddit": record.subreddit.clone().unwrap_or_default(),
        "author": record.author.clone().unwrap_or_default(),
        "num_comments": record.num_comments.unwrap_or(0),
        "kind": "submission",
    });

    Ok(BulkItem {
        external_id,
        title: sanitize_for_mysql(&record
            .title
            .clone()
            .unwrap_or_else(|| "Reddit submission".to_string())),
        content: sanitize_for_mysql(&record.selftext.clone().unwrap_or_default()),
        url,
        created_at,
        score: record.score.unwrap_or(0.0),
        metadata,
    })
}

fn format_timestamp(ts: Option<f64>) -> Result<String> {
    let ts = ts.ok_or_else(|| anyhow!("missing created_utc"))?;
    let secs = ts.trunc() as i64;
    let nanos = ((ts.fract() * 1e9).round() as u32).min(999_999_999);
    let dt: DateTime<Utc> = Utc
        .timestamp_opt(secs, nanos)
        .single()
        .ok_or_else(|| anyhow!("invalid timestamp"))?;
    Ok(dt.to_rfc3339())
}

/// Sanitize string for MySQL by removing 4-byte UTF-8 characters (emojis, etc.)
/// that may cause issues even with utf8mb4 if there are malformed sequences
fn sanitize_for_mysql(s: &str) -> String {
    s.chars()
        .filter(|c| {
            // Keep only BMP characters (3-byte UTF-8 max) and valid ASCII
            // This removes emojis and other supplementary plane characters
            *c <= '\u{FFFF}' && !c.is_control()
        })
        .collect()
}

enum InputSource {
    Local(String),
    Remote { url: String, token: Option<String> },
}

fn resolve_input(input: &str, gcs_token: Option<&str>) -> Result<InputSource> {
    if let Some(path) = input.strip_prefix("gs://") {
        let mut parts = path.splitn(2, '/');
        let bucket = parts
            .next()
            .filter(|b| !b.is_empty())
            .ok_or_else(|| anyhow!("gcs input missing bucket"))?;
        let object = parts
            .next()
            .filter(|o| !o.is_empty())
            .ok_or_else(|| anyhow!("gcs input missing object path"))?;

        let encoded = encode(object);
        let url =
            format!("https://storage.googleapis.com/storage/v1/b/{bucket}/o/{encoded}?alt=media");
        return Ok(InputSource::Remote {
            url,
            token: gcs_token.map(String::from),
        });
    }

    if input.starts_with("http://") || input.starts_with("https://") {
        Ok(InputSource::Remote {
            url: input.to_string(),
            token: None,
        })
    } else {
        Ok(InputSource::Local(input.to_string()))
    }
}

async fn open_reader(
    input: &str,
    client: &reqwest::Client,
    gcs_token: Option<&str>,
) -> Result<Box<dyn AsyncBufRead + Unpin + Send>> {
    match resolve_input(input, gcs_token)? {
        InputSource::Remote { url, token } => {
            let mut request = client.get(url);
            if let Some(token) = token {
                request = request.bearer_auth(token);
            }
            let resp = request.send().await?.error_for_status()?;
            let stream = resp
                .bytes_stream()
                .map_err(|e| io::Error::new(io::ErrorKind::Other, e));
            let reader = StreamReader::new(stream);
            Ok(wrap_decoder(reader, input))
        }
        InputSource::Local(path) => {
            let file = fs::File::open(path).await?;
            let reader = BufReader::new(file);
            Ok(wrap_decoder(reader, input))
        }
    }
}

fn wrap_decoder<R>(reader: R, input: &str) -> Box<dyn AsyncBufRead + Unpin + Send>
where
    R: AsyncBufRead + Unpin + Send + 'static,
{
    if input.ends_with(".gz") {
        Box::new(BufReader::new(GzipDecoder::new(reader)))
    } else if input.ends_with(".zst") || input.ends_with(".zstd") {
        Box::new(BufReader::new(ZstdDecoder::new(reader)))
    } else if input.ends_with(".xz") {
        Box::new(BufReader::new(XzDecoder::new(reader)))
    } else {
        Box::new(BufReader::new(reader))
    }
}

async fn submit_batch(
    endpoint: &str,
    token: &str,
    source: &str,
    items: &[BulkItem],
    client: &reqwest::Client,
) -> Result<()> {
    let payload = json!({
        "source": source,
        "items": items.iter().map(|it| {
            // Merge our flags with existing metadata
            let mut meta = it.metadata.clone();
            if let Some(obj) = meta.as_object_mut() {
                obj.insert("needs_ai_review".to_string(), json!(true));
                obj.insert("bulk_mode".to_string(), json!(true));
            }
            json!({
                "external_id": it.external_id,
                "title": it.title,
                "content": it.content,
                "url": it.url,
                "created_at": it.created_at,
                "updated_at": it.created_at,
                "score": it.score,
                "tags": [],
                "metadata": meta,
            })
        }).collect::<Vec<_>>()
    });

    let mut attempt = 0u32;
    let mut delay = Duration::from_secs(1);
    loop {
        let start = Instant::now();
        let resp = client
            .post(endpoint)
            .bearer_auth(token)
            .json(&payload)
            .send()
            .await;

        match resp {
            Ok(r) if r.status().is_success() => {
                let status = r.status();
                let elapsed = start.elapsed();
                match r.json::<BulkIngestResponse>().await {
                    Ok(stats) => {
                        info!(
                            "batch submitted status={} total={} inserted={} skipped={} duration={}ms",
                            status,
                            items.len(),
                            stats.inserted + stats.updated,
                            stats.skipped,
                            elapsed.as_millis()
                        );
                    }
                    Err(e) => {
                        warn!(
                            "batch submitted status={} total={} but failed to parse stats: {e}",
                            status,
                            items.len()
                        );
                    }
                }
                return Ok(());
            }
            Ok(r) => {
                let status = r.status();
                if should_retry_status(status) && attempt < 5 {
                    warn!(
                        "batch failed with status {}. retrying in {:?}",
                        status,
                        delay
                    );
                } else {
                    let body = r.text().await.unwrap_or_default();
                    return Err(anyhow!("batch failed status {} body {}", status, body));
                }
            }
            Err(e) => {
                if attempt >= 5 {
                    return Err(anyhow!("batch send error: {e}"));
                }
                warn!("batch send error: {e}; retrying in {:?}", delay);
            }
        }

        tokio::time::sleep(delay).await;
        attempt += 1;
        delay = (delay * 2).min(Duration::from_secs(30));
    }
}

fn should_retry_status(status: StatusCode) -> bool {
    status.is_server_error() || status == StatusCode::TOO_MANY_REQUESTS
}

#[derive(Debug, Deserialize)]
struct BulkIngestResponse {
    inserted: i64,
    updated: i64,
    skipped: i64,
    errors: Option<Vec<BulkError>>,
}

#[derive(Debug, Deserialize)]
struct BulkError {
    i: usize,
    reason: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_comment_into_item() {
        let json_line = r#"{
            "id": "abcd",
            "body": "Hello world",
            "permalink": "/r/rust/comments/abcd/example",
            "created_utc": 1700000000,
            "score": 5,
            "subreddit": "rust",
            "author": "carol",
            "link_id": "t3_parent",
            "parent_id": "t1_parent"
        }"#;

        let record: RedditRecord = serde_json::from_str(json_line).unwrap();
        let item = convert_record(&record, Mode::Both).unwrap().unwrap();
        assert_eq!(item.external_id, "t1_abcd");
        assert_eq!(item.title, "Reddit comment");
        assert!(item.url.contains("reddit.com"));
        assert_eq!(item.metadata["kind"], "comment");
    }

    #[test]
    fn resolve_gcs_input_to_signed_endpoint() {
        let source = resolve_input("gs://bucket/path/to/object.zst", Some("token"))
            .expect("gcs input should parse");
        match source {
            InputSource::Remote { url, token } => {
                assert_eq!(
                    url,
                    "https://storage.googleapis.com/storage/v1/b/bucket/o/path%2Fto%2Fobject.zst?alt=media"
                );
                assert_eq!(token.as_deref(), Some("token"));
            }
            _ => panic!("expected remote gcs source"),
        }
    }
}
