use anyhow::{Context, Result};
use clap::Parser;
use log::{error, info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::Value;
use std::collections::VecDeque;
use std::time::{Duration as StdDuration, SystemTime, UNIX_EPOCH};
use tokio::time::sleep;

#[derive(Deserialize, Clone, Debug)]
struct Config {
    general: Option<GeneralConfig>,
    github: Option<GithubConfig>,
}

#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig {
    db_url: String,
}

#[derive(Deserialize, Clone, Debug)]
struct GithubConfig {
    token: Option<String>,
    user_agent: Option<String>,
}

#[derive(Parser, Debug, Clone)]
struct Args {
    /// Shared config path (to reuse DB URL, token, etc.)
    #[arg(long, default_value = "config.toml")]
    config_path: String,

    /// Override DB URL if not in config
    #[arg(long)]
    db_url: Option<String>,

    /// GitHub token for higher rate limits (optional)
    #[arg(long)]
    github_token: Option<String>,

    /// User-Agent header (GitHub requires some UA)
    #[arg(long, default_value = "cleanapp-news-indexer/0.1")]
    user_agent: String,

    /// Number of repos per search query batch (controls URL length)
    #[arg(long, default_value_t = 25)]
    repos_per_batch: usize,

    /// Per-page size for GitHub search (max 100)
    #[arg(long, default_value_t = 100)]
    per_page: u32,

    /// Max pages to fetch per batch (each page up to per_page issues)
    #[arg(long, default_value_t = 5)]
    max_pages: u32,

    /// Hard cap on the number of GitHub search requests to make (safety)
    #[arg(long, default_value_t = 2000)]
    max_queries: u32,

    /// Re-index only repos not fetched since this timestamp (RFC3339), else all
    #[arg(long)]
    since: Option<String>,

    /// Limit total repos to process (for testing)
    #[arg(long, default_value_t = 0)]
    limit_repos: u64,

    /// Created since filter for issues (RFC3339 date like 2024-01-01)
    #[arg(long, default_value = "2024-01-01")]
    issues_created_since: String,

    /// Max issues per repo to retain (approx; enforced by sorting and filtering post-insert)
    #[arg(long, default_value_t = 50)]
    max_issues_per_repo: u32,

    /// Skip repos fetched within the last N days
    #[arg(long, default_value_t = 90)]
    skip_recent_days: i64,
}

fn mask_token(tok: &str) -> String {
    if tok.len() <= 8 { return "(short token)".to_string(); }
    format!("{}...{}", &tok[..4], &tok[tok.len()-4..])
}

fn truncate_utf8_boundary(s: &mut String, max_bytes: usize) {
    if s.len() <= max_bytes { return; }
    let mut idx = max_bytes;
    while idx > 0 && !s.is_char_boundary(idx) { idx -= 1; }
    s.truncate(idx);
}

fn truncate_chars(s: &str, max_chars: usize) -> String {
    let mut it = s.chars();
    let mut out = String::with_capacity(std::cmp::min(s.len(), max_chars));
    for _ in 0..max_chars {
        if let Some(ch) = it.next() { out.push(ch); } else { break; }
    }
    if it.next().is_some() { out } else { s.to_string() }
}

fn fmt_dt(s: &str) -> String {
    chrono::DateTime::parse_from_rfc3339(s)
        .map(|dt| dt.format("%Y-%m-%d %H:%M:%S").to_string())
        .unwrap_or_default()
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    // Load config (best-effort)
    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args.db_url.clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.general.as_ref().map(|g| g.db_url.clone())))
        .context("db_url must be provided via --db-url or config.general.db_url")?;

    let token = args.github_token.clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.github.as_ref().and_then(|g| g.token.clone())));
    let user_agent = cfg.as_ref().and_then(|c| c.github.as_ref().and_then(|g| g.user_agent.clone()))
        .unwrap_or_else(|| args.user_agent.clone());

    let per_page = args.per_page.min(100);
    let max_pages = args.max_pages.max(1);
    let max_queries = args.max_queries.max(1);
    let repos_per_batch = args.repos_per_batch.max(1);

    info!(
        "github issues index: start repos_per_batch={} per_page={} max_pages={} max_queries={} token={} since={:?} issues_created_since={}",
        repos_per_batch,
        per_page,
        max_pages,
        max_queries,
        token.as_ref().map(|t| mask_token(t)).unwrap_or("(none)".to_string()),
        args.since,
        args.issues_created_since,
    );

    // Prepare DB
    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    let mut conn = pool.get_conn().await?;
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_github_issue (
            issue_id BIGINT NOT NULL,
            repo_id BIGINT NOT NULL,
            repo_full_name VARCHAR(255) NOT NULL,
            title VARCHAR(512),
            url VARCHAR(255),
            body TEXT,
            comments INT,
            reactions_plus_one INT,
            created_at DATETIME,
            updated_at DATETIME,
            state VARCHAR(32),
            is_pull_request BOOL DEFAULT FALSE,
            PRIMARY KEY (issue_id),
            INDEX idx_repo (repo_id),
            INDEX idx_repo_created (repo_id, created_at),
            INDEX idx_reactions (reactions_plus_one)
        )
    "#).await?;
    if let Err(e) = conn.query_drop("ALTER TABLE indexer_github_issue MODIFY COLUMN title VARCHAR(512)").await {
        warn!("alter table title->VARCHAR(512) skipped: {}", e);
    }
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_github_issues_fetch_state (
            repo_id BIGINT PRIMARY KEY,
            repo_full_name VARCHAR(255) NOT NULL,
            last_success TIMESTAMP NULL,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        )
    "#).await?;
    drop(conn);

    // Read repos list
    let mut conn = pool.get_conn().await?;
    let total_repos: u64 = conn.exec_first("SELECT COUNT(*) FROM indexer_github_repos", ()).await?.unwrap_or(0);

    // Select only repos not fetched in the last N days (or never fetched)
    let repo_rows: Vec<(i64, String)> = if args.limit_repos == 0 {
        conn.exec_map(
            r#"
            SELECT r.repo_id, r.full_name
            FROM indexer_github_repos r
            LEFT JOIN indexer_github_issues_fetch_state s ON s.repo_id = r.repo_id
            WHERE s.last_success IS NULL OR s.last_success < NOW() - INTERVAL ? DAY
            ORDER BY r.stargazers_count DESC
            "#,
            (args.skip_recent_days,),
            |(id, name)| (id, name),
        ).await?
    } else {
        conn.exec_map(
            r#"
            SELECT r.repo_id, r.full_name
            FROM indexer_github_repos r
            LEFT JOIN indexer_github_issues_fetch_state s ON s.repo_id = r.repo_id
            WHERE s.last_success IS NULL OR s.last_success < NOW() - INTERVAL ? DAY
            ORDER BY r.stargazers_count DESC
            LIMIT ?
            "#,
            (args.skip_recent_days, args.limit_repos),
            |(id, name)| (id, name),
        ).await?
    };
    drop(conn);

    info!("github issues: loaded repos {} of total {}", repo_rows.len(), total_repos);

    // HTTP client
    let mut headers = reqwest::header::HeaderMap::new();
    headers.insert(reqwest::header::USER_AGENT, user_agent.parse().unwrap());
    if let Some(tok) = &token {
        headers.insert(reqwest::header::AUTHORIZATION, format!("Bearer {}", tok).parse().unwrap());
    }
    let client = reqwest::Client::builder()
        .default_headers(headers)
        .timeout(StdDuration::from_secs(30))
        .build()?;

    let mut queries_used = 0u32;

    // Build batches of repos
    let mut queue: VecDeque<(i64, String)> = VecDeque::from(repo_rows);
    let mut batch_index: u64 = 0;
    while !queue.is_empty() {
        let mut batch: Vec<(i64, String)> = Vec::with_capacity(repos_per_batch);
        for _ in 0..repos_per_batch {
            if let Some(x) = queue.pop_front() { batch.push(x); } else { break; }
        }
        batch_index += 1;
        let batch_repos_count = batch.len();

        // Construct search query
        let mut q = String::new();
        for (_, full) in &batch {
            if !q.is_empty() { q.push(' '); }
            q.push_str(&format!("repo:{}", full));
        }
        // filters: issues only, open, created since, bug-ish terms
        let terms = "(label:bug OR bug OR crash OR error OR \"not working\")";
        let created = &args.issues_created_since;
        let qualifiers = format!("is:issue state:open created:>={}", created);
        let full_query = format!("{} {} {}", q, qualifiers, terms);
        // URL encode q parameter minimal: spaces -> +; but we will let reqwest encode via query param

        info!(
            "batch {}: repos={} query_parts_len={} per_page={} max_pages={}",
            batch_index, batch_repos_count, full_query.len(), per_page, max_pages
        );

        let mut total_items_in_batch = 0usize;
        for page in 1..=max_pages {
            if queries_used >= max_queries { warn!("max_queries reached: {}", queries_used); break; }
            let url = "https://api.github.com/search/issues";
            let req = client.get(url).query(&[
                ("q", full_query.as_str()),
                ("sort", "reactions-+1"),
                ("order", "desc"),
                ("per_page", &per_page.to_string()),
                ("page", &page.to_string()),
            ]);

            info!("batch {}: requesting page {} for {} repos", batch_index, page, batch_repos_count);
            let resp = req.send().await?;
            queries_used += 1;

            let rl_rem = resp.headers().get("X-RateLimit-Remaining").and_then(|v| v.to_str().ok()).unwrap_or("?");
            let rl_lim = resp.headers().get("X-RateLimit-Limit").and_then(|v| v.to_str().ok()).unwrap_or("?");
            let rl_reset = resp.headers().get("X-RateLimit-Reset").and_then(|v| v.to_str().ok());
            info!("rate-limit: {}/{} reset={:?}", rl_rem, rl_lim, rl_reset);

            if resp.status() == reqwest::StatusCode::FORBIDDEN {
                warn!("rate limited on batch {} page {}", batch_index, page);
                if let Some(ts) = rl_reset.and_then(|s| s.parse::<u64>().ok()) {
                    let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs();
                    if ts > now { let wait = ts - now + 1; warn!("sleeping {}s until reset", wait); sleep(StdDuration::from_secs(wait)).await; }
                } else { sleep(StdDuration::from_secs(60)).await; }
                continue;
            }
            if !resp.status().is_success() { warn!("batch {} page {} http {}", batch_index, page, resp.status()); break; }

            let body = resp.text().await.unwrap_or_default();
            let v: Value = match serde_json::from_str(&body) { Ok(v) => v, Err(e) => { error!("json parse error on batch {} page {}: {}", batch_index, page, e); break; } };
            let items = v["items"].as_array().cloned().unwrap_or_default();
            if items.is_empty() { info!("batch {} page {}: items 0", batch_index, page); break; }

            total_items_in_batch += items.len();

            // Write to DB
            let mut conn = pool.get_conn().await?;
            let before_cnt: i64 = conn.exec_first("SELECT COUNT(*) FROM indexer_github_issue", ()).await?.unwrap_or(0);

            let params_iter = items.iter().filter_map(|it| {
                // skip PRs
                if it["pull_request"].is_object() { return None; }
                let issue_id = it["id"].as_i64().unwrap_or(0);
                let title = truncate_chars(it["title"].as_str().unwrap_or(""), 255);
                let url = it["html_url"].as_str().unwrap_or("").to_string();
                let mut body = it["body"].as_str().unwrap_or("").to_string();
                if body.len() > 16384 { truncate_utf8_boundary(&mut body, 16384); }
                let comments = it["comments"].as_i64().unwrap_or(0) as i32;
                let reactions = it["reactions"]["+1"].as_i64().unwrap_or(0) as i32;
                let created_at = fmt_dt(it["created_at"].as_str().unwrap_or(""));
                let updated_at = fmt_dt(it["updated_at"].as_str().unwrap_or(""));
                let state = it["state"].as_str().unwrap_or("").to_string();

                // derive repo id/name from repository_url or from item["repository_url"] and lookup in batch
                // GitHub search/issues includes repository_url like https://api.github.com/repos/OWNER/REPO
                let repo_url = it["repository_url"].as_str().unwrap_or("");
                let repo_full_name = repo_url.strip_prefix("https://api.github.com/repos/").unwrap_or("");
                let repo_id = batch.iter().find(|(_, full)| full == &repo_full_name).map(|(id, _)| *id).unwrap_or(0);

                Some(params!{
                    "issue_id" => issue_id,
                    "repo_id" => repo_id,
                    "repo_full_name" => repo_full_name.to_string(),
                    "title" => title,
                    "url" => url,
                    "body" => body,
                    "comments" => comments,
                    "+1" => reactions, // placeholder key; we'll bind properly in SQL string
                    "reactions_plus_one" => reactions,
                    "created_at" => created_at,
                    "updated_at" => updated_at,
                    "state" => state,
                    "is_pull_request" => false,
                })
            });

            conn.exec_batch(
                r#"INSERT INTO indexer_github_issue
                      (issue_id, repo_id, repo_full_name, title, url, body, comments, reactions_plus_one, created_at, updated_at, state, is_pull_request)
                   VALUES
                      (:issue_id, :repo_id, :repo_full_name, :title, :url, :body, :comments, :reactions_plus_one, :created_at, :updated_at, :state, :is_pull_request)
                   ON DUPLICATE KEY UPDATE
                      repo_id=VALUES(repo_id),
                      repo_full_name=VALUES(repo_full_name),
                      title=VALUES(title),
                      url=VALUES(url),
                      body=VALUES(body),
                      comments=VALUES(comments),
                      reactions_plus_one=VALUES(reactions_plus_one),
                      created_at=VALUES(created_at),
                      updated_at=VALUES(updated_at),
                      state=VALUES(state),
                      is_pull_request=VALUES(is_pull_request)
                "#,
                params_iter
            ).await?;

            let after_cnt: i64 = conn.exec_first("SELECT COUNT(*) FROM indexer_github_issue", ()).await?.unwrap_or(before_cnt);
            let inserted = (after_cnt - before_cnt).max(0);
            info!("batch {} page {}: inserted(new_rows) {}", batch_index, page, inserted);
            drop(conn);

            sleep(StdDuration::from_millis(500)).await;
        }

        // Mark fetch_state for repos in this batch
        let mut conn = pool.get_conn().await?;
        let params_iter = batch.iter().map(|(repo_id, full)| {
            params!{
                "repo_id" => repo_id,
                "repo_full_name" => full,
            }
        });
        conn.exec_batch(
            r#"INSERT INTO indexer_github_issues_fetch_state (repo_id, repo_full_name, last_success)
               VALUES (:repo_id, :repo_full_name, NOW())
               ON DUPLICATE KEY UPDATE
                 repo_full_name=VALUES(repo_full_name),
                 last_success=VALUES(last_success)
            "#,
            params_iter
        ).await?;
        info!("batch {} done: repos={} total_items_seen={} (queries_used={})",
            batch_index, batch_repos_count, total_items_in_batch, queries_used);

        // Throttle between batches to be nice
        sleep(StdDuration::from_millis(750)).await;
    }

    info!("github issues index finished (queries_used={})", queries_used);
    Ok(())
}


