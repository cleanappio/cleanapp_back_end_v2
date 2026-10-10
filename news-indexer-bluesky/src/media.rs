use anyhow::Result;
use mysql_async::prelude::*;
use serde_json::Value;
use sha2::{Digest, Sha256};

#[path = "selection.rs"]
mod selection;

pub async fn store_images(
    client: &reqwest::Client,
    conn: &mut mysql_async::Conn,
    uri: &str,
    did: &str,
    embed: &Value,
) -> Result<usize> {
    let mut stored = 0;
    for (position, url) in selection::image_urls(embed, did).iter().enumerate() {
        let response = match client.get(url).send().await {
            Ok(r) if r.status().is_success() => r,
            _ => continue,
        };
        if response.content_length().is_some_and(|n| n > 5_000_000) {
            continue;
        }
        let mime = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|s| s.to_str().ok())
            .unwrap_or("")
            .split(';')
            .next()
            .unwrap_or("")
            .to_owned();
        if !mime.starts_with("image/") {
            continue;
        }
        let bytes = response.bytes().await?;
        if bytes.is_empty() || bytes.len() > 5_000_000 {
            continue;
        }
        let digest = Sha256::digest(&bytes).to_vec();
        conn.exec_drop("INSERT INTO indexer_media_blob (sha256,mime,data) VALUES (?,?,?) ON DUPLICATE KEY UPDATE mime=COALESCE(mime,VALUES(mime))", (&digest,&mime,bytes.as_ref())).await?;
        conn.exec_drop("INSERT INTO indexer_bluesky_media (post_uri,position,sha256,url) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE sha256=VALUES(sha256),url=VALUES(url)", (uri,position as u32,&digest,url)).await?;
        stored += 1;
    }
    Ok(stored)
}
