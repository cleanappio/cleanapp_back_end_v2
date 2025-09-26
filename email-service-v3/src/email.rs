use anyhow::{Context, Result};
use regex::Regex;
use reqwest::header::{HeaderMap, HeaderValue, AUTHORIZATION, CONTENT_TYPE};

pub async fn send_sendgrid_email(
    api_key: &str,
    from_name: &str,
    from_email: &str,
    to_email: &str,
    subject: &str,
    html_content: &str,
    plain_content: &str,
    bcc_email: Option<&str>,
) -> Result<()> {
    let (processed_html, attachments) = extract_inline_data_images(html_content);

    let mut headers = HeaderMap::new();
    headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
    headers.insert(
        AUTHORIZATION,
        HeaderValue::from_str(&format!("Bearer {}", api_key))?,
    );

    let mut payload = serde_json::json!({
        "personalizations": [{
            "to": [{"email": to_email}],
            "subject": subject
        }],
        "from": {"email": from_email, "name": from_name},
        "content": [
            {"type": "text/plain", "value": plain_content},
            {"type": "text/html", "value": processed_html}
        ]
    });

    if let Some(bcc) = bcc_email {
        if let Some(personalizations) = payload.get_mut("personalizations").and_then(|v| v.as_array_mut()) {
            if let Some(first) = personalizations.get_mut(0) {
                first["bcc"] = serde_json::json!([{ "email": bcc }]);
            }
        }
    }

    if !attachments.is_empty() {
        let atts: Vec<serde_json::Value> = attachments
            .into_iter()
            .map(|a| serde_json::json!({
                "content": a.base64_content,
                "type": a.mime,
                "filename": a.filename,
                "disposition": "inline",
                "content_id": a.cid
            }))
            .collect();
        payload["attachments"] = serde_json::Value::Array(atts);
    }

    let client = reqwest::Client::new();
    let res = client
        .post("https://api.sendgrid.com/v3/mail/send")
        .headers(headers)
        .body(payload.to_string())
        .send()
        .await
        .context("sendgrid request failed")?;

    let status = res.status();
    let body = res.text().await.unwrap_or_default();
    if !status.is_success() {
        anyhow::bail!("sendgrid error: status={} body={}", status, truncate(&body));
    }
    Ok(())
}

fn truncate(s: &str) -> String {
    const MAX: usize = 512;
    if s.len() > MAX { format!("{}...", &s[..MAX]) } else { s.to_string() }
}

struct InlineAttachment {
    cid: String,
    filename: String,
    mime: String,
    base64_content: String,
}

fn extract_inline_data_images(html: &str) -> (String, Vec<InlineAttachment>) {
    // Match only the data URI inside attributes and replace with cid:...
    // Regex that captures: 1) mime type, 2) base64 payload; stops at quote
    let re = Regex::new(r#"(?i)data:([a-z0-9.+\-]+/[a-z0-9.+\-]+);base64,([^"']+)"#).unwrap();
    let mut attachments: Vec<InlineAttachment> = Vec::new();
    let mut idx: usize = 0;
    let processed = re
        .replace_all(html, |caps: &regex::Captures| {
            idx += 1;
            let mime = caps.get(1).map(|m| m.as_str()).unwrap_or("image/jpeg").to_string();
            let b64 = caps.get(2).map(|m| m.as_str()).unwrap_or("").to_string();
            let cid = format!("img{}", idx);
            let ext = mime_extension(&mime);
            let filename = format!("inline-{}.{}", idx, ext);
            attachments.push(InlineAttachment { cid: cid.clone(), filename, mime: mime.clone(), base64_content: b64 });
            format!("cid:{}", cid)
        })
        .into_owned();
    (processed, attachments)
}

fn mime_extension(mime: &str) -> String {
    if let Some(rest) = mime.strip_prefix("image/") {
        match rest {
            "jpeg" => "jpg".to_string(),
            "jpg" | "png" | "gif" | "webp" | "bmp" => rest.to_string(),
            // common case where servers report svg+xml
            "svg+xml" => "svg".to_string(),
            _ => "img".to_string(),
        }
    } else {
        "img".to_string()
    }
}


