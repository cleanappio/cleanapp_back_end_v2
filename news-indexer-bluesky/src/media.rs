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
        if !declared_size_allowed(response.content_length()) {
            continue;
        }
        let Some(mime) = image_mime(
            response
                .headers()
                .get(reqwest::header::CONTENT_TYPE)
                .and_then(|s| s.to_str().ok()),
        ) else {
            continue;
        };
        let mime = mime.to_owned();
        let bytes = response.bytes().await?;
        if !body_size_allowed(bytes.len()) {
            continue;
        }
        let digest = Sha256::digest(&bytes).to_vec();
        conn.exec_drop("INSERT INTO indexer_media_blob (sha256,mime,data) VALUES (?,?,?) ON DUPLICATE KEY UPDATE mime=COALESCE(mime,VALUES(mime))", (&digest,&mime,bytes.as_ref())).await?;
        conn.exec_drop("INSERT INTO indexer_bluesky_media (post_uri,position,sha256,url) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE sha256=VALUES(sha256),url=VALUES(url)", (uri,position as u32,&digest,url)).await?;
        stored += 1;
    }
    Ok(stored)
}

fn declared_size_allowed(size: Option<u64>) -> bool {
    !size.is_some_and(|n| n > 5_000_000)
}

fn image_mime(content_type: Option<&str>) -> Option<&str> {
    let mime = content_type.unwrap_or("").split(';').next().unwrap_or("");
    mime.starts_with("image/").then_some(mime)
}

fn body_size_allowed(size: usize) -> bool {
    size > 0 && size <= 5_000_000
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn media_requires_image_mime_and_enforces_both_declared_and_actual_limits() {
        for mime in [
            None,
            Some(""),
            Some("text/html"),
            Some("application/octet-stream"),
            Some("IMAGE/jpeg"),
        ] {
            assert_eq!(image_mime(mime), None);
        }
        assert_eq!(
            image_mime(Some("image/jpeg; charset=binary")),
            Some("image/jpeg")
        );
        assert_eq!(image_mime(Some("image/png")), Some("image/png"));
        assert!(declared_size_allowed(None));
        assert!(declared_size_allowed(Some(5_000_000)));
        assert!(!declared_size_allowed(Some(5_000_001)));
        assert!(!body_size_allowed(0));
        assert!(body_size_allowed(5_000_000));
        assert!(!body_size_allowed(5_000_001));
    }
}
