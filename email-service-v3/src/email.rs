use anyhow::{Context, Result};
use reqwest::header::{HeaderMap, HeaderValue, AUTHORIZATION, CONTENT_TYPE};

pub async fn send_sendgrid_email(
    api_key: &str,
    from_name: &str,
    from_email: &str,
    to_email: &str,
    subject: &str,
    html_content: &str,
    plain_content: &str,
) -> Result<()> {
    let mut headers = HeaderMap::new();
    headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
    headers.insert(
        AUTHORIZATION,
        HeaderValue::from_str(&format!("Bearer {}", api_key))?,
    );

    let payload = serde_json::json!({
        "personalizations": [{
            "to": [{"email": to_email}],
            "subject": subject
        }],
        "from": {"email": from_email, "name": from_name},
        "content": [
            {"type": "text/plain", "value": plain_content},
            {"type": "text/html", "value": html_content}
        ]
    });

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


