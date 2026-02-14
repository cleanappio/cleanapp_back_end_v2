use anyhow::{Context, Result};
use mysql_async as my;
use mysql_async::params;
use mysql_async::prelude::Queryable;
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use std::io::{self, Write};
use std::time::Duration;
use std::time::{SystemTime, UNIX_EPOCH};
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
    physical_batch_limit: u64,
    physical_max_contacts: usize,
    enable_digital_email_fetcher: bool,
    enable_physical_email_fetcher: bool,
    seq_range: Option<(i64, i64)>,
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
            physical_batch_limit: get("PHYSICAL_BATCH_LIMIT", "25").parse().unwrap_or(25),
            physical_max_contacts: get("PHYSICAL_MAX_CONTACTS", "5").parse().unwrap_or(5),
            enable_digital_email_fetcher: parse_bool_env("ENABLE_DIGITAL_EMAIL_FETCHER", true),
            enable_physical_email_fetcher: parse_bool_env("ENABLE_PHYSICAL_EMAIL_FETCHER", true),
            seq_range: parse_seq_range(std::env::var("SEQ_RANGE").ok().as_deref()),
        }
    }

    fn mysql_masked_url(&self) -> String {
        format!(
            "mysql://{}:{}@{}:{}/{}",
            self.db_user,
            mask_secret(&self.db_password, 2, 2),
            self.db_host,
            self.db_port,
            self.db_name
        )
    }

    fn build_mysql_opts(&self) -> my::Opts {
        let port: u16 = self.db_port.parse().unwrap_or(3306);
        let builder = my::OptsBuilder::default()
            .ip_or_hostname(self.db_host.clone())
            .tcp_port(port)
            .user(Some(self.db_user.clone()))
            .pass(Some(self.db_password.clone()))
            .db_name(Some(self.db_name.clone()));
        my::Opts::from(builder)
    }
}

fn parse_bool_env(key: &str, default: bool) -> bool {
    match std::env::var(key) {
        Ok(v) => matches!(
            v.trim().to_lowercase().as_str(),
            "1" | "true" | "yes" | "on"
        ),
        Err(_) => default,
    }
}

fn parse_seq_range(val: Option<&str>) -> Option<(i64, i64)> {
    let raw = val?.trim();
    if raw.is_empty() {
        return None;
    }
    let parts: Vec<&str> = raw.split('-').collect();
    if parts.len() != 2 {
        return None;
    }
    let start = parts[0].trim().parse::<i64>().ok()?;
    let end = parts[1].trim().parse::<i64>().ok()?;
    if start > end {
        return None;
    }
    Some((start, end))
}

fn mask_secret(value: &str, front: usize, back: usize) -> String {
    if value.is_empty() {
        return "".to_string();
    }
    if value.len() <= front + back {
        return "***".to_string();
    }
    format!("{}...{}", &value[..front], &value[value.len() - back..])
}

#[derive(Debug, Clone)]
struct PhysicalCandidateRow {
    seq: i64,
    latitude: f64,
    longitude: f64,
}

#[derive(Debug, Clone)]
struct PhysicalEmailCandidate {
    email: String,
    area_id: Option<i64>,
}

#[derive(Debug, Default)]
struct RunStats {
    digital_candidates: usize,
    digital_updated: usize,
    physical_candidates: usize,
    physical_resolved: usize,
    physical_no_match: usize,
    physical_errors: usize,
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

    let normalized = normalize_email_candidates(vec![content], 3);
    if normalized.is_empty() {
        Ok(None)
    } else {
        Ok(Some(normalized.join(",")))
    }
}

fn now_unix() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

fn next_attempt_unix(after_secs: i64) -> i64 {
    now_unix() + after_secs.max(0)
}

fn is_reasonable_email(value: &str) -> bool {
    let v = value.trim();
    if v.len() < 6 || v.contains(' ') {
        return false;
    }
    // Filter out placeholder tokens that sometimes appear in AI outputs.
    // Personal mailboxes are allowed; we're only excluding obviously non-real addresses.
    if v.contains('<') || v.contains('>') || v.contains('{') || v.contains('}') {
        return false;
    }
    let lower = v.to_lowercase();
    // Filter obvious placeholders. Personal mailboxes are allowed.
    let placeholders = [
        "test@",
        "example@",
        "sample@",
        "demo@",
        "noreply@",
        "no-reply@",
        "donotreply@",
        "admin@localhost",
        "user@localhost",
        "@example.com",
        "@test.com",
        "@localhost",
    ];
    for p in placeholders {
        if lower.contains(p) {
            return false;
        }
    }
    let mut parts = v.split('@');
    let local = parts.next().unwrap_or_default();
    let domain = parts.next().unwrap_or_default();
    if local.is_empty() || domain.is_empty() || parts.next().is_some() {
        return false;
    }
    // Domains should not contain underscores; placeholders often do (e.g. "establishment_domain.com").
    // Allow underscores in the local-part; reject only in the domain.
    if domain.contains('_') {
        return false;
    }
    domain.contains('.') && !domain.starts_with('.') && !domain.ends_with('.')
}

fn normalize_email_candidates(
    raw_values: impl IntoIterator<Item = String>,
    max_contacts: usize,
) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();

    for raw in raw_values {
        for token in raw.split(',') {
            let email = token.trim().to_lowercase();
            if !is_reasonable_email(&email) {
                continue;
            }
            if seen.insert(email.clone()) {
                out.push(email);
                if out.len() >= max_contacts {
                    return out;
                }
            }
        }
    }
    out
}

fn normalize_physical_email_candidates(
    raw_values: impl IntoIterator<Item = (i64, String)>,
    max_contacts: usize,
) -> Vec<PhysicalEmailCandidate> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();

    for (area_id, raw) in raw_values {
        for token in raw.split(',') {
            let email = token.trim().to_lowercase();
            if !is_reasonable_email(&email) {
                continue;
            }
            if seen.insert(email.clone()) {
                out.push(PhysicalEmailCandidate {
                    email,
                    area_id: Some(area_id),
                });
                if out.len() >= max_contacts {
                    return out;
                }
            }
        }
    }
    out
}

async fn column_exists(conn: &mut my::Conn, table: &str, column: &str) -> Result<bool> {
    let count: Option<u64> = conn
        .exec_first(
            r#"
            SELECT COUNT(*)
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = DATABASE()
              AND TABLE_NAME = :table
              AND COLUMN_NAME = :col
            "#,
            params! {"table" => table, "col" => column},
        )
        .await?;
    Ok(count.unwrap_or(0) > 0)
}

async fn ensure_column(conn: &mut my::Conn, table: &str, column: &str, ddl: &str) -> Result<()> {
    if column_exists(conn, table, column).await? {
        return Ok(());
    }
    info!("DB migrate: adding column {}.{}", table, column);
    let sql = format!("ALTER TABLE `{}` ADD COLUMN {}", table, ddl);
    conn.query_drop(sql).await?;
    Ok(())
}

async fn ensure_physical_lookup_state_columns(conn: &mut my::Conn) -> Result<()> {
    // These are additive and safe: used for auditability and multi-worker safety.
    ensure_column(
        conn,
        "physical_contact_lookup_state",
        "claimed_at",
        "`claimed_at` TIMESTAMP NULL DEFAULT NULL",
    )
    .await?;
    ensure_column(
        conn,
        "physical_contact_lookup_state",
        "claimed_by",
        "`claimed_by` VARCHAR(128) NULL DEFAULT NULL",
    )
    .await?;
    ensure_column(
        conn,
        "physical_contact_lookup_state",
        "selected_emails",
        "`selected_emails` TEXT NULL",
    )
    .await?;
    ensure_column(
        conn,
        "physical_contact_lookup_state",
        "selected_by_version",
        "`selected_by_version` VARCHAR(64) NULL DEFAULT NULL",
    )
    .await?;
    ensure_column(
        conn,
        "physical_contact_lookup_state",
        "selected_reason",
        "`selected_reason` VARCHAR(255) NULL DEFAULT NULL",
    )
    .await?;
    Ok(())
}

async fn ensure_physical_candidates_table(pool: &my::Pool) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    conn.query_drop(
        r#"
        CREATE TABLE IF NOT EXISTS physical_contact_candidates (
            seq BIGINT NOT NULL,
            email VARCHAR(320) NOT NULL,
            area_id BIGINT NULL,
            source_type VARCHAR(32) NOT NULL,
            source_ref VARCHAR(255) NULL,
            confidence DECIMAL(4,3) NOT NULL DEFAULT 0.000,
            evidence_url TEXT NULL,
            evidence_hash CHAR(64) NULL,
            evidence_excerpt VARCHAR(255) NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (seq, email),
            INDEX idx_pcc_seq (seq),
            INDEX idx_pcc_area (area_id),
            INDEX idx_pcc_updated (updated_at)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
        "#,
    )
    .await?;
    Ok(())
}

async fn ensure_physical_lookup_state_table(pool: &my::Pool) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    conn.query_drop(
        r#"
        CREATE TABLE IF NOT EXISTS physical_contact_lookup_state (
            seq BIGINT PRIMARY KEY,
            status VARCHAR(32) NOT NULL DEFAULT 'pending',
            attempt_count INT NOT NULL DEFAULT 0,
            next_attempt_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            last_error TEXT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            INDEX idx_physical_lookup_next (next_attempt_at),
            INDEX idx_physical_lookup_status (status),
            INDEX idx_physical_lookup_updated (updated_at)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
        "#,
    )
    .await?;
    // Additive migrations (safe in prod): extra columns used for auditability / multi-worker safety.
    ensure_physical_lookup_state_columns(&mut conn).await?;
    Ok(())
}

async fn run_digital_once(conn: &mut my::Conn, cfg: &Config) -> Result<(usize, usize)> {
    // Find candidate analyses: valid digital reports with empty inferred_contact_emails
    let rows: Vec<(i64, Option<String>)> = if let Some((start, end)) = cfg.seq_range {
        let select_sql = r#"
            SELECT seq, brand_display_name
            FROM report_analysis
            WHERE is_valid = TRUE
              AND classification = 'digital'
              AND language = 'en'
              AND seq BETWEEN :start AND :end
              AND (inferred_contact_emails IS NULL OR inferred_contact_emails = '' )
            ORDER BY updated_at ASC
            LIMIT :limit
        "#;
        conn.exec(
            select_sql,
            params! { "start" => start, "end" => end, "limit" => cfg.batch_limit },
        )
        .await?
    } else {
        let select_sql = r#"
            SELECT seq, brand_display_name
            FROM report_analysis
            WHERE is_valid = TRUE
              AND classification = 'digital'
              AND language = 'en'
              AND (inferred_contact_emails IS NULL OR inferred_contact_emails = '' )
            ORDER BY updated_at ASC
            LIMIT :limit
        "#;
        conn.exec(select_sql, params! { "limit" => cfg.batch_limit })
            .await?
    };

    let total = rows.len();
    if total == 0 {
        info!("Digital pass: no candidate rows found");
        return Ok((0, 0));
    }
    info!("Digital pass: fetched {} candidate rows", total);

    let mut processed = 0usize;
    for (idx, (seq, brand_opt)) in rows.into_iter().enumerate() {
        let brand = brand_opt.unwrap_or_default();
        if brand.is_empty() {
            info!(
                "Digital pass: skipping seq={} {}/{} due to empty brand_display_name",
                seq,
                idx + 1,
                total
            );
            continue;
        }

        info!(
            "Digital pass: processing {}/{} seq={} brand='{}'",
            idx + 1,
            total,
            seq,
            brand
        );

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
                    "Digital pass: updated inferred_contact_emails for seq={} ({})",
                    seq, brand
                );
            }
            None => {
                info!(
                    "Digital pass: no emails inferred for seq={} ({})",
                    seq, brand
                );
            }
        }
    }

    Ok((total, processed))
}

async fn fetch_physical_candidates(
    conn: &mut my::Conn,
    cfg: &Config,
) -> Result<Vec<PhysicalCandidateRow>> {
    // Prefer the email-service retry queue (await_contact_discovery) so we only do work
    // when the sender is explicitly waiting for location-based contacts.
    //
    // Fall back to a broader scan if there are no retry-queue candidates (useful in dev
    // or if retry scheduling is temporarily disabled).
    let rows: Vec<(i64, f64, f64)> = if let Some((start, end)) = cfg.seq_range {
        conn.exec(
            r#"
            SELECT ra.seq, r.latitude, r.longitude
            FROM email_report_retry er
            INNER JOIN report_analysis ra ON ra.seq = er.seq
            INNER JOIN reports r ON r.seq = er.seq
            LEFT JOIN sent_reports_emails sre ON sre.seq = ra.seq
            LEFT JOIN physical_contact_lookup_state pls ON pls.seq = ra.seq
            WHERE er.reason = 'await_contact_discovery'
              AND sre.seq IS NULL
              AND ra.is_valid = TRUE
              AND ra.classification = 'physical'
              AND ra.language = 'en'
              AND ra.seq BETWEEN :start AND :end
              AND r.latitude BETWEEN -90 AND 90
              AND r.longitude BETWEEN -180 AND 180
              AND NOT (r.latitude = 0 AND r.longitude = 0)
              AND (
                    pls.seq IS NULL
                 OR pls.next_attempt_at <= NOW()
                 -- If the sender is blocked by placeholder inferred emails (e.g. "<org.com>" or invalid domains),
                 -- allow the fetcher to clean them up immediately rather than waiting for backoff.
                 OR ra.inferred_contact_emails LIKE '%<%'
                 OR ra.inferred_contact_emails LIKE '%>%'
                 OR ra.inferred_contact_emails LIKE '%{%'
                 OR ra.inferred_contact_emails LIKE '%}%'
                 OR ra.inferred_contact_emails LIKE '%@%_%'
              )
              AND (pls.status IS NULL OR pls.status != 'resolved')
            ORDER BY er.next_attempt_at ASC, ra.seq DESC
            LIMIT :limit
            "#,
            params! {"start" => start, "end" => end, "limit" => cfg.physical_batch_limit},
        )
        .await?
    } else {
        conn.exec(
            r#"
            SELECT ra.seq, r.latitude, r.longitude
            FROM email_report_retry er
            INNER JOIN report_analysis ra ON ra.seq = er.seq
            INNER JOIN reports r ON r.seq = er.seq
            LEFT JOIN sent_reports_emails sre ON sre.seq = ra.seq
            LEFT JOIN physical_contact_lookup_state pls ON pls.seq = ra.seq
            WHERE er.reason = 'await_contact_discovery'
              AND sre.seq IS NULL
              AND ra.is_valid = TRUE
              AND ra.classification = 'physical'
              AND ra.language = 'en'
              AND r.latitude BETWEEN -90 AND 90
              AND r.longitude BETWEEN -180 AND 180
              AND NOT (r.latitude = 0 AND r.longitude = 0)
              AND (
                    pls.seq IS NULL
                 OR pls.next_attempt_at <= NOW()
                 -- If the sender is blocked by placeholder inferred emails (e.g. "<org.com>" or invalid domains),
                 -- allow the fetcher to clean them up immediately rather than waiting for backoff.
                 OR ra.inferred_contact_emails LIKE '%<%'
                 OR ra.inferred_contact_emails LIKE '%>%'
                 OR ra.inferred_contact_emails LIKE '%{%'
                 OR ra.inferred_contact_emails LIKE '%}%'
                 OR ra.inferred_contact_emails LIKE '%@%_%'
              )
              AND (pls.status IS NULL OR pls.status != 'resolved')
            ORDER BY er.next_attempt_at ASC, ra.seq DESC
            LIMIT :limit
            "#,
            params! {"limit" => cfg.physical_batch_limit},
        )
        .await?
    };

    if !rows.is_empty() {
        return Ok(rows
            .into_iter()
            .map(|(seq, latitude, longitude)| PhysicalCandidateRow {
                seq,
                latitude,
                longitude,
            })
            .collect());
    }

    // Fallback scan path.
    let scan_rows: Vec<(i64, f64, f64)> = if let Some((start, end)) = cfg.seq_range {
        conn.exec(
            r#"
            SELECT ra.seq, r.latitude, r.longitude
            FROM report_analysis ra
            INNER JOIN reports r ON r.seq = ra.seq
            LEFT JOIN sent_reports_emails sre ON sre.seq = ra.seq
            LEFT JOIN physical_contact_lookup_state pls ON pls.seq = ra.seq
            WHERE ra.is_valid = TRUE
              AND sre.seq IS NULL
              AND ra.classification = 'physical'
              AND ra.language = 'en'
              AND ra.seq BETWEEN :start AND :end
              AND (ra.inferred_contact_emails IS NULL OR ra.inferred_contact_emails = '')
              AND r.latitude BETWEEN -90 AND 90
              AND r.longitude BETWEEN -180 AND 180
              AND NOT (r.latitude = 0 AND r.longitude = 0)
              AND (pls.seq IS NULL OR pls.next_attempt_at <= NOW())
              AND (pls.status IS NULL OR pls.status != 'resolved')
            ORDER BY ra.seq DESC
            LIMIT :limit
            "#,
            params! {"start" => start, "end" => end, "limit" => cfg.physical_batch_limit},
        )
        .await?
    } else {
        conn.exec(
            r#"
            SELECT ra.seq, r.latitude, r.longitude
            FROM report_analysis ra
            INNER JOIN reports r ON r.seq = ra.seq
            LEFT JOIN sent_reports_emails sre ON sre.seq = ra.seq
            LEFT JOIN physical_contact_lookup_state pls ON pls.seq = ra.seq
            WHERE ra.is_valid = TRUE
              AND sre.seq IS NULL
              AND ra.classification = 'physical'
              AND ra.language = 'en'
              AND (ra.inferred_contact_emails IS NULL OR ra.inferred_contact_emails = '')
              AND r.latitude BETWEEN -90 AND 90
              AND r.longitude BETWEEN -180 AND 180
              AND NOT (r.latitude = 0 AND r.longitude = 0)
              AND (pls.seq IS NULL OR pls.next_attempt_at <= NOW())
              AND (pls.status IS NULL OR pls.status != 'resolved')
            ORDER BY ra.seq DESC
            LIMIT :limit
            "#,
            params! {"limit" => cfg.physical_batch_limit},
        )
        .await?
    };

    Ok(scan_rows
        .into_iter()
        .map(|(seq, latitude, longitude)| PhysicalCandidateRow {
            seq,
            latitude,
            longitude,
        })
        .collect())
}

#[allow(clippy::too_many_arguments)]
async fn upsert_physical_lookup_state(
    conn: &mut my::Conn,
    seq: i64,
    status: &str,
    next_attempt_unix: i64,
    last_error: Option<&str>,
    selected_emails: Option<&str>,
    selected_by_version: Option<&str>,
    selected_reason: Option<&str>,
) -> Result<()> {
    conn.exec_drop(
        r#"
        INSERT INTO physical_contact_lookup_state (
            seq, status, attempt_count, next_attempt_at, last_error,
            selected_emails, selected_by_version, selected_reason,
            created_at, updated_at
        )
        VALUES (
            :seq, :status, 1, FROM_UNIXTIME(:next_unix), :last_error,
            :selected_emails, :selected_by_version, :selected_reason,
            NOW(), NOW()
        )
        ON DUPLICATE KEY UPDATE
            status = VALUES(status),
            next_attempt_at = VALUES(next_attempt_at),
            last_error = VALUES(last_error),
            selected_emails = VALUES(selected_emails),
            selected_by_version = VALUES(selected_by_version),
            selected_reason = VALUES(selected_reason),
            attempt_count = attempt_count + 1,
            updated_at = NOW()
        "#,
        params! {
            "seq" => seq,
            "status" => status,
            "next_unix" => next_attempt_unix,
            "last_error" => last_error,
            "selected_emails" => selected_emails,
            "selected_by_version" => selected_by_version,
            "selected_reason" => selected_reason,
        },
    )
    .await?;
    Ok(())
}

async fn upsert_physical_candidate(
    conn: &mut my::Conn,
    seq: i64,
    candidate: &PhysicalEmailCandidate,
    source_type: &str,
    source_ref: Option<&str>,
    confidence: f32,
) -> Result<()> {
    conn.exec_drop(
        r#"
        INSERT INTO physical_contact_candidates (
            seq, email, area_id, source_type, source_ref, confidence, created_at, updated_at
        )
        VALUES (
            :seq, :email, :area_id, :source_type, :source_ref, :confidence, NOW(), NOW()
        )
        ON DUPLICATE KEY UPDATE
            area_id = VALUES(area_id),
            source_type = VALUES(source_type),
            source_ref = VALUES(source_ref),
            confidence = GREATEST(confidence, VALUES(confidence)),
            updated_at = NOW()
        "#,
        params! {
            "seq" => seq,
            "email" => &candidate.email,
            "area_id" => candidate.area_id,
            "source_type" => source_type,
            "source_ref" => source_ref,
            "confidence" => confidence,
        },
    )
    .await?;
    Ok(())
}

async fn lookup_physical_contact_emails(
    conn: &mut my::Conn,
    latitude: f64,
    longitude: f64,
    max_contacts: usize,
) -> Result<(usize, Vec<PhysicalEmailCandidate>)> {
    // IMPORTANT: MySQL follows the SRID axis order for geographic SRS. For EPSG:4326 the axis order
    // is latitude, then longitude. So we intentionally build `POINT(lat lon)` here.
    let pt_wkt = format!("POINT({} {})", latitude, longitude);

    let area_count = conn
        .exec_first::<u64, _, _>(
            r#"
            SELECT COUNT(DISTINCT area_id)
            FROM area_index
            WHERE MBRWithin(ST_GeomFromText(:pt, 4326), geom)
            "#,
            params! {"pt" => pt_wkt.clone()},
        )
        .await?
        .unwrap_or(0) as usize;

    if area_count == 0 {
        return Ok((0, vec![]));
    }

    let raw_rows: Vec<(i64, Option<String>)> = conn
        .exec(
            r#"
            SELECT DISTINCT ai.area_id, ce.email
            FROM area_index ai
            INNER JOIN contact_emails ce ON ce.area_id = ai.area_id
            WHERE MBRWithin(ST_GeomFromText(:pt, 4326), ai.geom)
              AND ce.consent_report = TRUE
            LIMIT :limit
            "#,
            params! {
                "pt" => pt_wkt,
                "limit" => ((max_contacts.max(1) * 4) as u64),
            },
        )
        .await?;

    let candidates = normalize_physical_email_candidates(
        raw_rows
            .into_iter()
            .filter_map(|(area_id, email_opt)| email_opt.map(|e| (area_id, e))),
        max_contacts.max(1),
    );
    Ok((area_count, candidates))
}

async fn run_physical_once(
    conn: &mut my::Conn,
    cfg: &Config,
) -> Result<(usize, usize, usize, usize)> {
    let candidates = fetch_physical_candidates(conn, cfg).await?;
    let total = candidates.len();
    if total == 0 {
        info!("Physical pass: no candidate rows found");
        return Ok((0, 0, 0, 0));
    }
    info!("Physical pass: fetched {} candidate rows", total);

    let mut resolved = 0usize;
    let mut no_match = 0usize;
    let mut errors = 0usize;

    for (idx, row) in candidates.into_iter().enumerate() {
        // Pull the current inferred_contact_emails so we can:
        // - merge/dedupe with new candidates, and
        // - clear placeholder-only values (e.g. "<organization.com>") to unblock future passes.
        let existing_raw: Option<String> = conn
            .exec_first(
                r#"
                SELECT inferred_contact_emails
                FROM report_analysis
                WHERE seq = :seq
                  AND language = 'en'
                LIMIT 1
                "#,
                params! {"seq" => row.seq},
            )
            .await?;
        let existing_raw = existing_raw.unwrap_or_default();
        let existing_valid =
            normalize_email_candidates([existing_raw.clone()], cfg.physical_max_contacts.max(1));

        info!(
            "Physical pass: processing {}/{} seq={} lat={} lng={}",
            idx + 1,
            total,
            row.seq,
            row.latitude,
            row.longitude
        );

        match lookup_physical_contact_emails(
            conn,
            row.latitude,
            row.longitude,
            cfg.physical_max_contacts,
        )
        .await
        {
            Ok((area_count, candidates)) => {
                if !candidates.is_empty() {
                    // Persist provenance for auditability (safe: UPSERT by (seq,email)).
                    for c in &candidates {
                        let source_ref = c
                            .area_id
                            .map(|id| format!("area_id={}", id))
                            .unwrap_or_else(|| "area_id=unknown".to_string());
                        if let Err(e) = upsert_physical_candidate(
                            conn,
                            row.seq,
                            c,
                            "area_index",
                            Some(source_ref.as_str()),
                            0.95,
                        )
                        .await
                        {
                            warn!(
                                "Physical pass: failed to upsert candidate for seq={} email={} err={:#}",
                                row.seq, c.email, e
                            );
                        }
                    }

                    let new_emails = candidates
                        .iter()
                        .map(|c| c.email.clone())
                        .collect::<Vec<_>>();
                    // Merge: existing (filtered) + new candidates; cap to max_contacts.
                    let merged = normalize_email_candidates(
                        [existing_valid.join(","), new_emails.join(",")],
                        cfg.physical_max_contacts.max(1),
                    );
                    let email_csv = merged.join(",");
                    conn.exec_drop(
                        r#"
                        UPDATE report_analysis
                        SET inferred_contact_emails = :emails
                        WHERE seq = :seq
                        "#,
                        params! {
                            "emails" => email_csv.as_str(),
                            "seq" => row.seq,
                        },
                    )
                    .await?;

                    // If we successfully resolved contacts, ask the sender to retry immediately.
                    conn.exec_drop(
                        r#"
                        UPDATE email_report_retry
                        SET next_attempt_at = NOW(), updated_at = NOW()
                        WHERE seq = :seq AND reason = 'await_contact_discovery'
                        "#,
                        params! {"seq" => row.seq},
                    )
                    .await?;

                    let selected_reason = format!("area_index match (area_hits={})", area_count);
                    upsert_physical_lookup_state(
                        conn,
                        row.seq,
                        "resolved",
                        next_attempt_unix(7 * 24 * 3600),
                        None,
                        Some(email_csv.as_str()),
                        Some("v1_area_index"),
                        Some(selected_reason.as_str()),
                    )
                    .await?;
                    resolved += 1;
                    info!(
                        "Physical pass: seq={} resolved with {} contact(s), area_hits={}",
                        row.seq,
                        candidates.len(),
                        area_count
                    );
                } else {
                    // If the existing inferred_contact_emails contains only placeholders/invalid addresses,
                    // clear it so other discovery paths can run without being blocked.
                    if existing_valid.is_empty() && !existing_raw.trim().is_empty() {
                        conn.exec_drop(
                            r#"
                            UPDATE report_analysis
                            SET inferred_contact_emails = ''
                            WHERE seq = :seq AND language = 'en'
                            "#,
                            params! {"seq" => row.seq},
                        )
                        .await?;
                    }
                    let (status, retry_delay) = if area_count == 0 {
                        ("no_area_match", 6 * 3600)
                    } else {
                        ("no_contact_email", 4 * 3600)
                    };
                    let selected_reason = format!("area_index no match (area_hits={})", area_count);
                    upsert_physical_lookup_state(
                        conn,
                        row.seq,
                        status,
                        next_attempt_unix(retry_delay),
                        None,
                        None,
                        Some("v1_area_index"),
                        Some(selected_reason.as_str()),
                    )
                    .await?;
                    // Back off sender retries too; otherwise the email-service will churn the same rows
                    // faster than contact discovery can improve.
                    conn.exec_drop(
                        r#"
                        UPDATE email_report_retry
                        SET next_attempt_at = DATE_ADD(NOW(), INTERVAL :delay SECOND), updated_at = NOW()
                        WHERE seq = :seq AND reason = 'await_contact_discovery'
                        "#,
                        params! {"seq" => row.seq, "delay" => retry_delay},
                    )
                    .await?;
                    no_match += 1;
                    info!(
                        "Physical pass: seq={} no contacts found (status={}, area_hits={})",
                        row.seq, status, area_count
                    );
                }
            }
            Err(err) => {
                let err_msg = format!("{:#}", err);
                if let Err(state_err) = upsert_physical_lookup_state(
                    conn,
                    row.seq,
                    "error",
                    next_attempt_unix(30 * 60),
                    Some(&err_msg),
                    None,
                    Some("v1_area_index"),
                    Some("lookup error"),
                )
                .await
                {
                    warn!(
                        "Physical pass: failed to update lookup state after error for seq={} err={:#}",
                        row.seq, state_err
                    );
                }
                errors += 1;
                warn!("Physical pass: seq={} lookup error: {}", row.seq, err_msg);
            }
        }
    }

    Ok((total, resolved, no_match, errors))
}

async fn run_once(pool: &my::Pool, cfg: &Config) -> Result<RunStats> {
    let mut conn = pool.get_conn().await?;
    let mut stats = RunStats::default();

    if cfg.enable_physical_email_fetcher {
        let (total, resolved, no_match, errors) = run_physical_once(&mut conn, cfg).await?;
        stats.physical_candidates = total;
        stats.physical_resolved = resolved;
        stats.physical_no_match = no_match;
        stats.physical_errors = errors;
    }

    // Run digital after physical: digital can be slow (LLM calls) and should not starve
    // location-based contact discovery.
    if cfg.enable_digital_email_fetcher {
        let (total, updated) = run_digital_once(&mut conn, cfg).await?;
        stats.digital_candidates = total;
        stats.digital_updated = updated;
    }

    Ok(stats)
}

#[tokio::main]
async fn main() -> Result<()> {
    dotenvy::dotenv().ok();
    // Emit early message before logger init so it shows even if we exit immediately
    let enabled_raw = std::env::var("ENABLE_EMAIL_FETCHER").unwrap_or_else(|_| "".to_string());
    eprintln!(
        "email-fetcher init | ENABLE_EMAIL_FETCHER='{}'",
        enabled_raw
    );
    let _ = io::stderr().flush();
    println!(
        "email-fetcher init stdout | ENABLE_EMAIL_FETCHER='{}'",
        enabled_raw
    );
    let _ = io::stdout().flush();

    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .with_target(false)
        .compact()
        .init();

    // Feature toggle: allow deploys to ship disabled and exit gracefully
    let enabled = if enabled_raw.is_empty() {
        "false".to_string()
    } else {
        enabled_raw
    };
    if !matches!(enabled.to_lowercase().as_str(), "1" | "true" | "yes" | "on") {
        // Print explicitly to stderr and flush to ensure visibility in fast-exit containers
        eprintln!("WARN: ENABLE_EMAIL_FETCHER is disabled; exiting without starting");
        let _ = io::stderr().flush();
        println!("WARN: ENABLE_EMAIL_FETCHER is disabled; exiting without starting");
        let _ = io::stdout().flush();
        // Give logging collectors a moment to capture lines from a fast-exit container
        std::thread::sleep(Duration::from_millis(300));
        warn!("ENABLE_EMAIL_FETCHER is disabled; exiting without starting");
        return Ok(());
    }

    println!("email-fetcher: logger initialized and feature enabled");
    let _ = io::stdout().flush();

    let cfg = Config::from_env();
    println!(
        "email-fetcher: config loaded: db={} openai_model={} delay={}ms digital_limit={} physical_limit={} digital_enabled={} physical_enabled={}",
        cfg.mysql_masked_url(),
        cfg.openai_model,
        cfg.loop_delay_ms,
        cfg.batch_limit,
        cfg.physical_batch_limit,
        cfg.enable_digital_email_fetcher,
        cfg.enable_physical_email_fetcher
    );
    let _ = io::stdout().flush();

    let masked_url = cfg.mysql_masked_url();
    let openai_key_masked = mask_secret(&cfg.openai_api_key, 4, 4);
    info!("DB URI: {}", masked_url);
    info!(
        "OpenAI model: {}, key: {}",
        cfg.openai_model, openai_key_masked
    );
    info!(
        "Feature flags: digital_email_fetcher={} physical_email_fetcher={} (physical_max_contacts={})",
        cfg.enable_digital_email_fetcher,
        cfg.enable_physical_email_fetcher,
        cfg.physical_max_contacts
    );

    let opts = cfg.build_mysql_opts();
    let pool = my::Pool::new(opts);
    ensure_physical_lookup_state_table(&pool)
        .await
        .context("ensuring physical lookup state table")?;
    ensure_physical_candidates_table(&pool)
        .await
        .context("ensuring physical candidates table")?;

    println!(
        "email-fetcher: mysql pool created, entering loop with delay={}ms",
        cfg.loop_delay_ms
    );
    let _ = io::stdout().flush();

    info!(
        "email-fetcher starting; delay={}ms digital_limit={} physical_limit={}",
        cfg.loop_delay_ms, cfg.batch_limit, cfg.physical_batch_limit
    );

    loop {
        tokio::select! {
            _ = signal::ctrl_c() => {
                info!("Shutdown signal received");
                break;
            }
            _ = sleep(Duration::from_millis(cfg.loop_delay_ms)) => {
                match run_once(&pool, &cfg).await {
                    Ok(stats) => info!(
                        "Batch summary: digital candidates={} updated={} | physical candidates={} resolved={} no_match={} errors={}",
                        stats.digital_candidates,
                        stats.digital_updated,
                        stats.physical_candidates,
                        stats.physical_resolved,
                        stats.physical_no_match,
                        stats.physical_errors
                    ),
                    Err(e) => error!("Batch error: {:#}", e),
                }
            }
        }
    }

    println!("email-fetcher: disconnecting pool");
    let _ = io::stdout().flush();
    pool.disconnect().await?;
    Ok(())
}
