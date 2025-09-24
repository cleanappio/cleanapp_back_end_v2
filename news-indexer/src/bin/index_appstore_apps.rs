use anyhow::{Result};
use clap::Parser;
use log::{info, error};
use mysql_async::prelude::*;
use mysql_async::{Pool};
use std::collections::{HashMap, HashSet};
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[derive(Parser, Debug, Clone)]
struct Args {
    /// MySQL connection string, e.g. mysql://user:pass@host:port/db
    #[arg(long)]
    db_url: String,

    /// Country code (e.g., us)
    #[arg(long, default_value = "us")]
    country: String,

    /// Comma-separated list of Apple genre IDs (used if --from-db-genres is false)
    #[arg(long, default_value = "6018,6000,6022,6017,6016,6015,6023,6014,6013,6012,6020,6011,6010,6009,6021,6019,6008,6007,6006,6005,6004,6003,6002")]
    genres: String,

    /// Limit per genre (Apple RSS caps at 200)
    #[arg(long, default_value = "200")]
    limit: u32,

    /// If set, load genres from indexer_appstore_genres table
    #[arg(long, default_value_t = false)]
    from_db_genres: bool,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();

    let args = Args::parse();

    let limit = args.limit.min(200);

    // Determine genres source
    let genres: Vec<String> = if args.from_db_genres {
        let pool = Pool::new(mysql_async::Opts::from_url(&args.db_url)?);
        let mut conn = pool.get_conn().await?;
        let rows: Vec<(String,)> = conn.exec("SELECT genre_id FROM indexer_appstore_genres", ()).await?;
        let list: Vec<String> = rows.into_iter().map(|(id,)| id).collect();
        info!("loaded {} genres from DB", list.len());
        list
    } else {
        args.genres.split(',').map(|s| s.trim().to_string()).filter(|s| !s.is_empty()).collect()
    };

    let client = reqwest::Client::builder()
        .user_agent("news-indexer-indexer/0.1 (+https://cleanapp.io)")
        .timeout(StdDuration::from_secs(20))
        .build()?;

    let mut app_to_genres: HashMap<String, HashSet<String>> = HashMap::new();
    let mut app_to_name: HashMap<String, String> = HashMap::new();

    for genre in genres {
        let url = format!(
            "https://itunes.apple.com/{}/rss/topfreeapplications/limit={}/genre={}/json",
            args.country, limit, genre
        );
        let resp = client.get(&url).send().await;
        match resp {
            Ok(r) => {
                if !r.status().is_success() {
                    let status = r.status();
                    let body = r.text().await.unwrap_or_default();
                    error!("fetch failed for genre {}: {} body_head={}", genre, status, &body.chars().take(200).collect::<String>());
                    continue;
                }
                let body = r.text().await.unwrap_or_default();
                let parsed: serde_json::Value = match serde_json::from_str(&body) {
                    Ok(v) => v,
                    Err(e) => { error!("parse failed for genre {}: {} body_head={}", genre, e, &body.chars().take(200).collect::<String>()); continue; }
                };
                let entries = parsed["feed"]["entry"].as_array().cloned().unwrap_or_default();
                let mut count = 0usize;
                for entry in entries {
                    let app_id = entry["id"]["attributes"]["im:id"].as_str().unwrap_or("").to_string();
                    let name = entry["im:name"]["label"].as_str().unwrap_or("").to_string();
                    if app_id.is_empty() || name.is_empty() { continue; }
                    app_to_name.entry(app_id.clone()).or_insert(name);
                    app_to_genres.entry(app_id).or_default().insert(genre.clone());
                    count += 1;
                }
                info!("genre {}: fetched {} apps", genre, count);
            }
            Err(e) => {
                error!("http error for genre {}: {}", genre, e);
            }
        }
        sleep(StdDuration::from_millis(150)).await; // be polite
    }

    info!("unique apps collected: {}", app_to_name.len());

    // Connect to DB and upsert
    let pool = Pool::new(mysql_async::Opts::from_url(&args.db_url)?);
    let mut conn = pool.get_conn().await?;

    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_appstore_apps (
            app_id VARCHAR(32) NOT NULL,
            name VARCHAR(255) NOT NULL,
            genres TEXT NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (app_id),
            INDEX name_idx (name)
        )
    "#).await?;

    // Prepare batch upsert
    let mut values: Vec<(String, String, String)> = Vec::with_capacity(app_to_name.len());
    for (app_id, name) in &app_to_name {
        let genres_set = app_to_genres.get(app_id).cloned().unwrap_or_default();
        let mut genres_vec: Vec<String> = genres_set.into_iter().collect();
        genres_vec.sort();
        let genres_joined = genres_vec.join(",");
        values.push((app_id.clone(), name.clone(), genres_joined));
    }

    // Chunk inserts to avoid packet size issues
    for chunk in values.chunks(500) {
        let params: Vec<_> = chunk.iter().map(|(id, name, genres)| (id, name, genres)).collect();
        conn.exec_batch(
            r#"INSERT INTO indexer_appstore_apps (app_id, name, genres)
               VALUES (:app_id, :name, :genres)
               ON DUPLICATE KEY UPDATE name=VALUES(name), genres=VALUES(genres), updated_at=CURRENT_TIMESTAMP"#,
            params.iter().map(|(id, name, genres)| params! {
                "app_id" => id,
                "name" => name,
                "genres" => genres,
            })
        ).await?;
    }

    info!("upserted {} apps into indexer_appstore_apps", app_to_name.len());

    Ok(())
}
