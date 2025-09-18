use anyhow::{Context, Result};
use clap::Parser;
use log::{error, info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::Value;
use std::time::{Duration as StdDuration, SystemTime, UNIX_EPOCH};
use tokio::time::sleep;

#[derive(Deserialize)]
struct Config {
    general: Option<GeneralConfig>,
    github: Option<GithubConfig>,
}

#[derive(Deserialize)]
struct GeneralConfig {
    db_url: String,
}

#[derive(Deserialize)]
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

    /// Per-page page size (max 100)
    #[arg(long, default_value_t = 100)]
    per_page: u32,

    /// Max pages to fetch (GitHub returns 1000 results max per search)
    #[arg(long, default_value_t = 10)]
    max_pages: u32,

    /// Hard cap on the number of GitHub search requests to make (safety)
    #[arg(long, default_value_t = 120)]
    max_queries: u32,
}

fn mask_token(tok: &str) -> String {
    if tok.len() <= 8 { return "(short token)".to_string(); }
    format!("{}...{}", &tok[..4], &tok[tok.len()-4..])
}

fn fmt_dt(s: &str) -> String { chrono::DateTime::parse_from_rfc3339(s).map(|dt| dt.format("%Y-%m-%d %H:%M:%S").to_string()).unwrap_or_default() }

fn truncate_utf8_boundary(s: &mut String, max_bytes: usize) {
    if s.len() <= max_bytes { return; }
    let mut idx = max_bytes;
    while idx > 0 && !s.is_char_boundary(idx) { idx -= 1; }
    s.truncate(idx);
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

    info!("github index: start per_page={} max_pages={} max_queries={} token={}", per_page, max_pages, max_queries, token.as_ref().map(|t| mask_token(t)).unwrap_or("(none)".to_string()));

    // Prepare DB
    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    let mut conn = pool.get_conn().await?;
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_github_repos (
            repo_id BIGINT PRIMARY KEY,
            full_name VARCHAR(255) NOT NULL,
            html_url VARCHAR(255) NOT NULL,
            description TEXT,
            stargazers_count INT,
            forks_count INT,
            open_issues_count INT,
            language VARCHAR(128),
            created_at DATETIME,
            updated_at DATETIME,
            pushed_at DATETIME,
            last_indexed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            INDEX idx_full_name (full_name),
            INDEX idx_stars (stargazers_count)
        )
    "#).await?;
    // Ensure description is TEXT in case the table pre-existed with VARCHAR
    if let Err(e) = conn.query_drop("ALTER TABLE indexer_github_repos MODIFY COLUMN description TEXT").await {
        warn!("alter table description->TEXT skipped: {}", e);
    }
    drop(conn);

    // Determine starting floor from DB
    let mut conn2 = pool.get_conn().await?;
    let mut floor: i64 = conn2.exec_first("SELECT COALESCE(MIN(stargazers_count), 2000000000) FROM indexer_github_repos", ()).await?.unwrap_or(2_000_000_000);
    drop(conn2);

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

    'windows: loop {
        if queries_used >= max_queries { warn!("max_queries reached: {}", queries_used); break; }
        info!("window: starting with floor stars<{} (queries_used={})", floor, queries_used);
        let mut window_min_stars: Option<i64> = None;
        let mut window_new_rows: i64 = 0;

        for page in 1..=max_pages {
            if queries_used >= max_queries { break 'windows; }
            let q = if floor >= 2_000_000_000 {
                // initial full-range query
                "stars:%3E1".to_string()
            } else {
                // single qualifier to ensure GitHub applies the filter
                format!("stars:%3C{}", floor)
            };
            let url = format!("https://api.github.com/search/repositories?q={}&sort=stars&order=desc&per_page={}&page={}", q, per_page, page);
            info!("github index: requesting page {} {} (floor={})", page, url, floor);
            let resp = client.get(&url).send().await?;
            queries_used += 1;

            let rl_rem = resp.headers().get("X-RateLimit-Remaining").and_then(|v| v.to_str().ok()).unwrap_or("?");
            let rl_lim = resp.headers().get("X-RateLimit-Limit").and_then(|v| v.to_str().ok()).unwrap_or("?");
            let rl_reset = resp.headers().get("X-RateLimit-Reset").and_then(|v| v.to_str().ok());
            info!("rate-limit: {}/{} reset={:?}", rl_rem, rl_lim, rl_reset);

            if resp.status() == reqwest::StatusCode::FORBIDDEN {
                warn!("rate limited on page {}", page);
                if let Some(ts) = rl_reset.and_then(|s| s.parse::<u64>().ok()) {
                    let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs();
                    if ts > now { let wait = ts - now + 1; warn!("sleeping {}s until reset", wait); sleep(StdDuration::from_secs(wait)).await; }
                } else { sleep(StdDuration::from_secs(60)).await; }
                continue;
            }
            if !resp.status().is_success() { warn!("page {} http {}", page, resp.status()); break; }

            let body = resp.text().await.unwrap_or_default();
            let v: Value = match serde_json::from_str(&body) { Ok(v) => v, Err(e) => { error!("json parse error on page {}: {}", page, e); break; } };
            let items = v["items"].as_array().cloned().unwrap_or_default();
            if items.is_empty() { info!("page {}: items 0", page); break; }

            let first_stars = items.first().and_then(|r| r["stargazers_count"].as_i64()).unwrap_or(-1);
            let last_stars  = items.last().and_then(|r| r["stargazers_count"].as_i64()).unwrap_or(-1);
            info!("page {}: items {} first_stars={} last_stars={}", page, items.len(), first_stars, last_stars);

            let mut conn = pool.get_conn().await?;
            let before_cnt: i64 = conn.exec_first("SELECT COUNT(*) FROM indexer_github_repos", ()).await?.unwrap_or(0);

            // Batch params
            let params_iter = items.iter().map(|repo| {
                let repo_id = repo["id"].as_i64().unwrap_or(0);
                let full_name = repo["full_name"].as_str().unwrap_or("").to_string();
                let html_url = repo["html_url"].as_str().unwrap_or("").to_string();
                let mut description = repo["description"].as_str().unwrap_or("").to_string();
                if description.len() > 4096 { truncate_utf8_boundary(&mut description, 4096); }
                let stargazers = repo["stargazers_count"].as_i64().unwrap_or(0) as i32;
                let forks = repo["forks_count"].as_i64().unwrap_or(0) as i32;
                let open_issues = repo["open_issues_count"].as_i64().unwrap_or(0) as i32;
                let language = repo["language"].as_str().unwrap_or("").to_string();
                let created_at = fmt_dt(repo["created_at"].as_str().unwrap_or(""));
                let updated_at = fmt_dt(repo["updated_at"].as_str().unwrap_or(""));
                let pushed_at = fmt_dt(repo["pushed_at"].as_str().unwrap_or(""));

                if window_min_stars.map_or(true, |m| (stargazers as i64) < m) { window_min_stars = Some(stargazers as i64); }

                params!{
                    "repo_id" => repo_id,
                    "full_name" => full_name,
                    "html_url" => html_url,
                    "description" => description,
                    "stars" => stargazers,
                    "forks" => forks,
                    "issues" => open_issues,
                    "language" => language,
                    "created_at" => created_at,
                    "updated_at" => updated_at,
                    "pushed_at" => pushed_at,
                }
            });

            conn.exec_batch(
                r#"INSERT INTO indexer_github_repos
                      (repo_id, full_name, html_url, description, stargazers_count, forks_count, open_issues_count, language, created_at, updated_at, pushed_at)
                   VALUES
                      (:repo_id, :full_name, :html_url, :description, :stars, :forks, :issues, :language, :created_at, :updated_at, :pushed_at)
                   ON DUPLICATE KEY UPDATE
                      full_name=VALUES(full_name),
                      html_url=VALUES(html_url),
                      description=VALUES(description),
                      stargazers_count=VALUES(stargazers_count),
                      forks_count=VALUES(forks_count),
                      open_issues_count=VALUES(open_issues_count),
                      language=VALUES(language),
                      created_at=VALUES(created_at),
                      updated_at=VALUES(updated_at),
                      pushed_at=VALUES(pushed_at)
                "#,
                params_iter
            ).await?;

            let after_cnt: i64 = conn.exec_first("SELECT COUNT(*) FROM indexer_github_repos", ()).await?.unwrap_or(before_cnt);
            let inserted = (after_cnt - before_cnt).max(0);
            window_new_rows += inserted;
            info!("page {}: inserted(new_rows) {}", page, inserted);
            sleep(StdDuration::from_millis(500)).await;
        }

        let next_floor = window_min_stars.map(|m| m - 1).unwrap_or(floor - 1);
        info!("window done: floor={} inserted(new_rows)={} window_min_stars={:?} -> next_floor={}", floor, window_new_rows, window_min_stars, next_floor);
        floor = next_floor;
        if floor <= 1 { break; }
    }

    let mut cfinal = pool.get_conn().await?;
    let table_cnt: i64 = cfinal.exec_first("SELECT COUNT(*) FROM indexer_github_repos", ()).await?.unwrap_or(0);
    info!("github index finished: table_count={} (queries_used={})", table_cnt, queries_used);
    Ok(())
}
