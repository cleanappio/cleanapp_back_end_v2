use anyhow::Result;
use clap::Parser;
use log::{info, error};
use mysql_async::prelude::*;
use mysql_async::Pool;
use std::collections::VecDeque;
use std::time::Duration as StdDuration;

#[derive(Parser, Debug, Clone)]
struct Args {
    /// MySQL connection string, e.g. mysql://user:pass@host:port/db
    #[arg(long)]
    db_url: String,

    /// Country code (e.g., us)
    #[arg(long, default_value = "us")]
    country: String,

    /// Root genre id; 36 is iOS Apps root
    #[arg(long, default_value = "36")]
    root_id: String,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    let client = reqwest::Client::builder()
        .user_agent("news-indexer-genres/0.1 (+https://cleanapp.io)")
        .timeout(StdDuration::from_secs(20))
        .build()?;

    let url = format!(
        "https://itunes.apple.com/WebObjects/MZStoreServices.woa/ws/genres?cc={}&id={}",
        args.country, args.root_id
    );
    let resp = client.get(&url).send().await?;
    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        error!("genres fetch failed: {} body_head={}", status, &body.chars().take(200).collect::<String>());
        return Ok(());
    }
    let body = resp.text().await.unwrap_or_default();
    let json: serde_json::Value = match serde_json::from_str(&body) {
        Ok(v) => v,
        Err(e) => { error!("genres parse failed: {} body_head={}", e, &body.chars().take(200).collect::<String>()); return Ok(()); }
    };

    // Flatten tree: collect (id, name, parent_id, path)
    let mut records: Vec<(String, String, Option<String>, String)> = Vec::new();

    fn enqueue_children(queue: &mut VecDeque<(String, serde_json::Value, Option<String>, String)>, id: String, node: serde_json::Value, parent: Option<String>, path: String) {
        queue.push_back((id, node, parent, path));
    }

    let mut queue: VecDeque<(String, serde_json::Value, Option<String>, String)> = VecDeque::new();
    for (id, node) in json.as_object().unwrap_or(&serde_json::Map::new()).iter() {
        let name = node["name"].as_str().unwrap_or("");
        let path = format!("{}:{}", id, name);
        enqueue_children(&mut queue, id.clone(), node.clone(), None, path);
    }

    while let Some((id, node, parent, path)) = queue.pop_front() {
        let name = node["name"].as_str().unwrap_or("").to_string();
        records.push((id.clone(), name.clone(), parent.clone(), path.clone()));
        if let Some(subs) = node["subgenres"].as_object() {
            for (cid, cnode) in subs.iter() {
                let cname = cnode["name"].as_str().unwrap_or("");
                let cpath = format!("{} > {}:{}", path, cid, cname);
                enqueue_children(&mut queue, cid.clone(), cnode.clone(), Some(id.clone()), cpath);
            }
        }
    }

    info!("genres discovered: {}", records.len());

    // Upsert into DB
    let pool = Pool::new(mysql_async::Opts::from_url(&args.db_url)?);
    let mut conn = pool.get_conn().await?;

    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_appstore_genres (
            genre_id VARCHAR(16) PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            parent_id VARCHAR(16),
            path TEXT,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            INDEX parent_idx (parent_id)
        )
    "#).await?;

    for chunk in records.chunks(500) {
        let params: Vec<_> = chunk.iter().collect();
        conn.exec_batch(
            r#"INSERT INTO indexer_appstore_genres (genre_id, name, parent_id, path)
               VALUES (:gid, :name, :pid, :path)
               ON DUPLICATE KEY UPDATE
                 name=VALUES(name),
                 parent_id=VALUES(parent_id),
                 path=VALUES(path),
                 updated_at=CURRENT_TIMESTAMP"#,
            params.iter().map(|(gid, name, pid, path)| params!{
                "gid" => gid,
                "name" => name,
                "pid" => pid,
                "path" => path,
            })
        ).await?;
    }

    info!("upserted {} genres into indexer_appstore_genres", records.len());

    Ok(())
}
