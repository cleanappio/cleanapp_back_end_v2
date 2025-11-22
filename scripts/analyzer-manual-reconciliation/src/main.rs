use std::path::PathBuf;
use std::time::Duration;

use anyhow::{anyhow, Context, Result};
use clap::Parser;
use futures::{stream, StreamExt};
use rand::{thread_rng, Rng};
use reqwest::StatusCode;
use serde::{Deserialize, Serialize};
use tokio::time::sleep;
use tracing::{error, info, warn};
use tracing_subscriber::EnvFilter;
use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64_STD;
use hex::FromHex;

#[derive(Parser, Debug, Clone)]
#[command(name = "analyzer-manual-reconciliation", about = "Re-import reports for analysis from a CSV export")]
struct Cli {
	/// Path to CSV with reports to re-import
	#[arg(long, default_value = "cleanapp_reports.csv")]
	csv: PathBuf,

	/// Base URL for the API (no trailing slash)
	#[arg(long, default_value = "http://api.cleanapp.io:8080")]
	api_url: String,

	/// Number of concurrent HTTP requests
	#[arg(long, default_value_t = 2)]
	concurrency: usize,

	/// Maximum number of retries for a failed request
	#[arg(long, default_value_t = 3)]
	max_retries: usize,

	/// Initial backoff for retries (e.g. 500ms, 2s)
	#[arg(long, default_value = "500ms")]
	initial_backoff: humantime::Duration,

	/// Optional fixed delay between submissions (e.g. 100ms, 1s)
	#[arg(long)]
	inter_request_delay: Option<humantime::Duration>,

	/// Skip records if the image field cannot be validated/decoded
	#[arg(long, default_value_t = true)]
	skip_on_image_error: bool,

	/// If set, only validate and print what would be sent
	#[arg(long, default_value_t = false)]
	dry_run: bool,
}

#[derive(Debug, Deserialize)]
struct CsvReport {
	// Unused by the API, but may be present in the export
	#[serde(default)]
	seq: Option<i64>,
	#[serde(default)]
	ts: Option<String>,
	#[serde(default)]
	team: Option<i32>,

	// Required for re-submission
	id: String,
	latitude: f64,
	longitude: f64,
	x: f64,
	y: f64,

	// Optional fields
	#[serde(default)]
	image: Option<String>, // base64 string if present
	#[serde(default)]
	action_id: Option<String>,
	#[serde(default)]
	description: Option<String>,
}

#[derive(Debug, Serialize)]
struct ReportPayload<'a> {
	version: &'a str, // must be "2.0"
	id: &'a str,
	latitude: f64,
	longitude: f64,
	x: f64,
	y: f64,
	#[serde(skip_serializing_if = "Option::is_none")]
	image: Option<&'a str>, // base64 string
	action_id: &'a str,
	annotation: &'a str,
}

#[derive(Debug, Deserialize)]
struct ReportResponse {
	seq: i64,
}

#[tokio::main]
async fn main() -> Result<()> {
	// Logging
	tracing_subscriber::fmt()
		.with_env_filter(EnvFilter::from_default_env().add_directive("info".parse()?))
		.with_target(false)
		.compact()
		.init();

	let cli = Cli::parse();

	info!("CSV: {}", cli.csv.display());
	info!("API: {}", cli.api_url);
	info!("Concurrency: {}", cli.concurrency);
	if cli.dry_run {
		warn!("Running in DRY RUN mode - no reports will be submitted");
	}

	let client = reqwest::Client::builder()
		.user_agent("analyzer-manual-reconciliation/0.1")
		.timeout(Duration::from_secs(30))
		.build()?;

	let mut rdr = csv::ReaderBuilder::new()
		.has_headers(true)
		.from_path(&cli.csv)
		.with_context(|| format!("failed to open CSV: {}", cli.csv.display()))?;

	let mut records = Vec::new();
	for result in rdr.deserialize() {
		let rec: CsvReport = result.with_context(|| "failed to parse CSV row")?;
		records.push(rec);
	}

	info!("Loaded {} records from CSV", records.len());

	let inter_delay: Option<Duration> = cli.inter_request_delay.map(|d| d.into());
	let initial_backoff: Duration = cli.initial_backoff.into();

	let successes = tokio::sync::Mutex::new(0usize);
	let skipped = tokio::sync::Mutex::new(0usize);
	let failures = tokio::sync::Mutex::new(0usize);

	stream::iter(records.into_iter().enumerate())
		.map(|(idx, rec)| {
			let client = &client;
			let api_url = cli.api_url.clone();
			let dry_run = cli.dry_run;
			let max_retries = cli.max_retries;
			let initial_backoff = initial_backoff;
			let skip_on_image_error = cli.skip_on_image_error;
			async move {
				match process_record(
					client,
					&api_url,
					rec,
					dry_run,
					max_retries,
					initial_backoff,
					skip_on_image_error,
					idx + 1,
				)
				.await
				{
					Ok(res) => Ok(res),
					Err(e) => Err(e),
				}
			}
		})
		.buffer_unordered(cli.concurrency)
		.for_each(|res| {
			let successes = &successes;
			let skipped = &skipped;
			let failures = &failures;
			async move {
				match res {
					Ok(ProcessResult::Submitted(_seq)) => {
						*successes.lock().await += 1;
					}
					Ok(ProcessResult::Skipped) => {
						*skipped.lock().await += 1;
					}
					Ok(ProcessResult::DryRun) => {
						*successes.lock().await += 1; // treat as success in dry-run
					}
					Err(err) => {
						error!("{err:#}");
						*failures.lock().await += 1;
					}
				}
			}
		})
		.await;

	if let Some(d) = inter_delay {
		// Small final delay to let logs flush when inter-request delays were used
		sleep(d.min(Duration::from_millis(250))).await;
	}

	let ok = *successes.lock().await;
	let sk = *skipped.lock().await;
	let fail = *failures.lock().await;
	info!("Done. Success: {ok}, Skipped: {sk}, Failed: {fail}");
	if fail > 0 {
		return Err(anyhow!("some records failed: {fail}"));
	}
	Ok(())
}

enum ProcessResult {
	Submitted(i64),
	Skipped,
	DryRun,
}

async fn process_record(
	client: &reqwest::Client,
	api_url: &str,
	rec: CsvReport,
	dry_run: bool,
	max_retries: usize,
	initial_backoff: Duration,
	skip_on_image_error: bool,
	ordinal: usize,
) -> Result<ProcessResult> {
	// Validate id
	if rec.id.trim().is_empty() {
		warn!("[{ordinal}] missing id - skipping record");
		return Ok(ProcessResult::Skipped);
	}

	let action_id = rec.action_id.as_deref().unwrap_or_default();
	let annotation = rec.description.as_deref().unwrap_or_default();

	// Normalize image: handle hex or base64; strip data URL, remove whitespace, validate and re-encode to base64
	let mut image_owned: Option<String> = None;
	if let Some(raw) = rec.image.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
		match normalize_image_data(raw) {
			Ok((encoded, decoded_preview_len)) => {
				image_owned = Some(encoded);
				// Optionally log preview len
				info!("[{ordinal}] image ok ({} bytes decoded)", decoded_preview_len);
			}
			Err(e) => {
				if skip_on_image_error {
					warn!("[{ordinal}] skipping record due to image error: {e}");
					return Ok(ProcessResult::Skipped);
				} else {
					warn!("[{ordinal}] proceeding without image (may fail on server): {e}");
				}
			}
		}
	} else if skip_on_image_error {
		warn!("[{ordinal}] missing image - skipping record");
		return Ok(ProcessResult::Skipped);
	}

	let payload = ReportPayload {
		version: "2.0",
		id: &rec.id,
		latitude: rec.latitude,
		longitude: rec.longitude,
		x: rec.x,
		y: rec.y,
		image: image_owned.as_deref(),
		action_id,
		annotation,
	};

	if dry_run {
		info!(
			"[{ordinal}] would submit id={} lat={}, lon={} x={}, y={} action_id='{}' annotation_len={} image={}",
			rec.id,
			rec.latitude,
			rec.longitude,
			rec.x,
			rec.y,
			action_id,
			annotation.len(),
			image_owned.as_deref().map(|_| "yes").unwrap_or("no")
		);
		return Ok(ProcessResult::DryRun);
	}

	let url = format!("{}/report", api_url.trim_end_matches('/'));
	let mut attempt = 0usize;
	let mut backoff = initial_backoff;
	loop {
		attempt += 1;
		let resp = client.post(&url).json(&payload).send().await;
		match resp {
			Ok(r) => {
				if r.status().is_success() {
					let rr: ReportResponse = r.json().await.context("invalid JSON response")?;
					info!(
						"[{ordinal}] submitted id={} -> seq={}",
						rec.id,
						rr.seq
					);
					return Ok(ProcessResult::Submitted(rr.seq));
				}
				if should_retry_status(r.status()) && attempt <= max_retries {
					let jitter_ms = thread_rng().gen_range(0..(backoff.as_millis() as u64 / 4 + 1));
					let delay = backoff + Duration::from_millis(jitter_ms);
					warn!(
						"[{ordinal}] server status {} - retrying in {:?} (attempt {}/{})",
						r.status(),
						delay,
						attempt,
						max_retries
					);
					sleep(delay).await;
					backoff = backoff.saturating_mul(2).min(Duration::from_secs(8));
					continue;
				} else {
					let status = r.status();
					let body = r.text().await.unwrap_or_else(|_| "<body read failed>".to_string());
					return Err(anyhow!("[{ordinal}] server error: status={status}, body={body}"));
				}
			}
			Err(err) => {
				if attempt <= max_retries {
					let jitter_ms = thread_rng().gen_range(0..(backoff.as_millis() as u64 / 4 + 1));
					let delay = backoff + Duration::from_millis(jitter_ms);
					warn!(
						"[{ordinal}] request error: {} - retrying in {:?} (attempt {}/{})",
						err, delay, attempt, max_retries
					);
					sleep(delay).await;
					backoff = backoff.saturating_mul(2).min(Duration::from_secs(8));
					continue;
				}
				return Err(anyhow!("[{ordinal}] request failed after retries: {err}"));
			}
		}
	}
}

fn is_supported_image(bytes: &[u8]) -> bool {
	// JPEG
	if bytes.len() >= 3 && bytes[0] == 0xFF && bytes[1] == 0xD8 && bytes[2] == 0xFF {
		return true;
	}
	// PNG
	if bytes.len() >= 8 && &bytes[..8] == b"\x89PNG\r\n\x1a\n" {
		return true;
	}
	// GIF
	if bytes.len() >= 6 && (&bytes[..6] == b"GIF87a" || &bytes[..6] == b"GIF89a") {
		return true;
	}
	false
}

fn normalize_image_data(raw: &str) -> Result<(String, usize)> {
	// Strip data URL prefix
	let data = if raw.starts_with("data:") {
		raw.splitn(2, ',').nth(1).ok_or_else(|| anyhow!("invalid data URL image field"))?
	} else {
		raw
	};
	// Remove whitespace
	let compact: String = data.chars().filter(|c| !c.is_whitespace()).collect();

	// Try HEX (e.g., "0xFFD8FF..." or just hex digits)
	if looks_like_hex(&compact) {
		let no_prefix = compact.strip_prefix("0x").or_else(|| compact.strip_prefix("0X")).unwrap_or(&compact);
		let bytes = Vec::from_hex(no_prefix).context("invalid hex image data")?;
		if !is_supported_image(&bytes) {
			return Err(anyhow!("decoded image is not a supported format (expect JPEG/PNG/GIF)"));
		}
		let reenc = BASE64_STD.encode(&bytes);
		return Ok((reenc, bytes.len()));
	}
	// Try standard base64 first (pad to multiple of 4)
	let padded_std = pad_base64(&compact);
	if let Ok(decoded) = BASE64_STD.decode(padded_std.as_bytes()) {
		if !is_supported_image(&decoded) {
			return Err(anyhow!("decoded image is not a supported format (expect JPEG/PNG/GIF)"));
		}
		let reenc = BASE64_STD.encode(&decoded);
		return Ok((reenc, decoded.len()));
	}
	// Try URL-safe after mapping to standard alphabet
	let mapped = compact.replace('-', "+").replace('_', "/");
	let padded_mapped = pad_base64(&mapped);
	let decoded = BASE64_STD
		.decode(padded_mapped.as_bytes())
		.context("invalid base64 (even after URL-safe mapping)")?;
	if !is_supported_image(&decoded) {
		return Err(anyhow!("decoded image is not a supported format (expect JPEG/PNG/GIF)"));
	}
	let reenc = BASE64_STD.encode(&decoded);
	Ok((reenc, decoded.len()))
}

fn looks_like_hex(s: &str) -> bool {
	let s = s.trim();
	let s = s.strip_prefix("0x").or_else(|| s.strip_prefix("0X")).unwrap_or(s);
	// Consider hex if all chars are 0-9a-fA-F and length even and length > 2
	if s.len() < 2 || s.len() % 2 != 0 {
		return false;
	}
	s.chars().all(|c| c.is_ascii_hexdigit())
}

fn pad_base64(s: &str) -> String {
	let rem = s.len() % 4;
	if rem == 0 {
		s.to_string()
	} else {
		let pad = 4 - rem;
		let mut out = String::with_capacity(s.len() + pad);
		out.push_str(s);
		for _ in 0..pad {
			out.push('=');
		}
		out
	}
}

fn should_retry_status(status: StatusCode) -> bool {
	match status {
		StatusCode::REQUEST_TIMEOUT
		| StatusCode::TOO_MANY_REQUESTS
		| StatusCode::BAD_GATEWAY
		| StatusCode::SERVICE_UNAVAILABLE
		| StatusCode::GATEWAY_TIMEOUT => true,
		_ if status.is_server_error() => true,
		_ => false,
	}
}


