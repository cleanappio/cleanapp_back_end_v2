use std::{net::IpAddr, time::Duration};

use anyhow::{anyhow, Result};
use axum::http::{HeaderValue, StatusCode};
use reqwest::{redirect::Policy, Client, Url};
use serde::Deserialize;
use serde_json::{json, Value};

const MAX_RESPONSE_BYTES: usize = 64 * 1024 * 1024;
type BriefingError = (StatusCode, axum::Json<Value>);

#[derive(Clone)]
pub struct BrandBriefingClient {
    client: Client,
    endpoint: Url,
}

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
pub struct ReportsByBrandParams {
    pub brand_name: String,
    pub n: Option<String>,
    pub lang: Option<String>,
    pub kind: Option<String>,
    pub content_kind: Option<String>,
    pub sort: Option<String>,
    pub offset: Option<String>,
    pub product: Option<String>,
    pub full_data: Option<String>,
}

fn error(status: StatusCode, message: &str) -> BriefingError {
    (status, axum::Json(json!({ "error": message })))
}

impl BrandBriefingClient {
    pub fn new(base: &str, timeout: Duration) -> Result<Self> {
        let mut endpoint = Url::parse(base)
            .map_err(|_| anyhow!("BRAND_BRIEFING_API_URL must be an internal HTTP(S) base URL"))?;
        let host = endpoint.host_str().unwrap_or("");
        let internal_host = host == "localhost"
            || match host
                .trim_start_matches('[')
                .trim_end_matches(']')
                .parse::<IpAddr>()
            {
                Ok(IpAddr::V4(ip)) => ip.is_loopback() || ip.is_private() || ip.is_link_local(),
                Ok(IpAddr::V6(ip)) => {
                    ip.is_loopback() || ip.is_unique_local() || ip.is_unicast_link_local()
                }
                Err(_) => {
                    !host.is_empty()
                        && !host.contains(':')
                        && (!host.contains('.') || host.ends_with(".internal"))
                }
            };
        if !matches!(endpoint.scheme(), "http" | "https")
            || !internal_host
            || !endpoint.username().is_empty()
            || endpoint.password().is_some()
            || endpoint.query().is_some()
            || endpoint.fragment().is_some()
            || !matches!(endpoint.path(), "" | "/")
        {
            return Err(anyhow!("BRAND_BRIEFING_API_URL must be an internal HTTP(S) base URL without credentials, path, query or fragment"));
        }
        endpoint.set_path("/api/v3/reports/by-brand");
        let client = Client::builder()
            .redirect(Policy::none())
            .no_proxy()
            .connect_timeout(timeout.min(Duration::from_secs(3)))
            .timeout(timeout)
            .build()
            .map_err(|_| anyhow!("Failed to configure canonical brand briefing client"))?;
        Ok(Self { client, endpoint })
    }

    pub async fn fetch(
        &self,
        params: &ReportsByBrandParams,
        authorization: Option<HeaderValue>,
    ) -> std::result::Result<Value, BriefingError> {
        let kind = params
            .kind
            .as_deref()
            .filter(|value| !value.is_empty())
            .or(params
                .content_kind
                .as_deref()
                .filter(|value| !value.is_empty()))
            .unwrap_or("firsthand");
        let query = [
            ("brand_name", params.brand_name.as_str()),
            ("kind", kind),
            ("sort", params.sort.as_deref().unwrap_or("signal")),
            ("lang", params.lang.as_deref().unwrap_or("en")),
            ("n", params.n.as_deref().unwrap_or("25")),
            ("offset", params.offset.as_deref().unwrap_or("0")),
            ("product", params.product.as_deref().unwrap_or("")),
            // Rust historically returned full reports. Preserve this default.
            ("full_data", params.full_data.as_deref().unwrap_or("true")),
        ];
        let mut request = self.client.get(self.endpoint.clone()).query(&query);
        if let Some(header) = authorization {
            request = request.header(reqwest::header::AUTHORIZATION, header);
        }
        let unavailable = || {
            error(
                StatusCode::SERVICE_UNAVAILABLE,
                "Evidence briefing temporarily unavailable",
            )
        };
        let mut response = request.send().await.map_err(|_| unavailable())?;
        match response.status() {
            StatusCode::BAD_REQUEST => {
                return Err(error(
                    StatusCode::BAD_REQUEST,
                    "Invalid brand briefing parameters",
                ))
            }
            StatusCode::UNAUTHORIZED => {
                return Err(error(StatusCode::UNAUTHORIZED, "Authentication required"))
            }
            StatusCode::FORBIDDEN => return Err(error(StatusCode::FORBIDDEN, "Access denied")),
            status if !status.is_success() => return Err(unavailable()),
            _ => {}
        }
        if response
            .content_length()
            .is_some_and(|length| length > MAX_RESPONSE_BYTES as u64)
        {
            return Err(unavailable());
        }
        let mut body = Vec::new();
        while let Some(chunk) = response.chunk().await.map_err(|_| unavailable())? {
            if body.len().saturating_add(chunk.len()) > MAX_RESPONSE_BYTES {
                return Err(unavailable());
            }
            body.extend_from_slice(&chunk);
        }
        // Preserve additive metadata; a narrowed Rust DTO must not discard it.
        let value: Value = serde_json::from_slice(&body).map_err(|_| unavailable())?;
        if !value.is_object() || !value.get("reports").is_some_and(Value::is_array) {
            return Err(unavailable());
        }
        Ok(value)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::{
        extract::Query, http::HeaderMap, response::IntoResponse, routing::get, Json, Router,
    };
    use serde_json::json;
    use std::collections::HashMap;

    fn params() -> ReportsByBrandParams {
        ReportsByBrandParams {
            brand_name: "anthropic".into(),
            n: None,
            lang: None,
            kind: None,
            content_kind: None,
            sort: None,
            offset: None,
            product: None,
            full_data: None,
        }
    }

    async fn fake_api() -> (String, tokio::task::JoinHandle<()>) {
        let app = Router::new().route("/api/v3/reports/by-brand", get(|Query(query): Query<HashMap<String,String>>, headers: HeaderMap| async move {
            if query.get("kind").map(String::as_str) == Some("invalid") {
                return (StatusCode::BAD_REQUEST, Json(json!({"error":"invalid kind"})));
            }
            (StatusCode::OK, Json(json!({"reports":[{"evidence":{"report_kind":"firsthand","excerpt":"Original source"},"recurrence":{"supporting_report_count":1}}],"kind_counts":{"firsthand":1,"coverage":2,"unknown":3,"all":6},"counts_exact":true,"scope":{"brand_name":"anthropic"},"query":query,"authorization":headers.get("authorization").and_then(|v|v.to_str().ok())})))
        }));
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let handle = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        (url, handle)
    }

    #[tokio::test]
    async fn defaults_preserve_full_reports_and_additive_metadata() {
        let (url, task) = fake_api().await;
        let client = BrandBriefingClient::new(&url, Duration::from_secs(2)).unwrap();
        let value = client.fetch(&params(), None).await.unwrap();
        assert_eq!(value["query"]["kind"], "firsthand");
        assert_eq!(value["query"]["sort"], "signal");
        assert_eq!(value["query"]["full_data"], "true");
        assert_eq!(value["query"]["n"], "25");
        assert_eq!(
            value["reports"][0]["evidence"]["excerpt"],
            "Original source"
        );
        assert_eq!(value["kind_counts"]["all"], 6);
        assert_eq!(value["counts_exact"], true);
        task.abort();
    }

    #[tokio::test]
    async fn forwards_scoped_query_and_authorization_only_to_fixed_endpoint() {
        let (url, task) = fake_api().await;
        let client = BrandBriefingClient::new(&url, Duration::from_secs(2)).unwrap();
        let mut query = params();
        query.kind = Some("coverage".into());
        query.sort = Some("recurrence".into());
        query.product = Some("claude-code".into());
        query.lang = Some("me".into());
        query.n = Some("7".into());
        query.offset = Some("14".into());
        query.full_data = Some("false".into());
        let value = client
            .fetch(&query, Some(HeaderValue::from_static("Bearer local-test")))
            .await
            .unwrap();
        assert_eq!(
            value["query"],
            json!({"brand_name":"anthropic","kind":"coverage","sort":"recurrence","product":"claude-code","lang":"me","n":"7","offset":"14","full_data":"false"})
        );
        assert_eq!(value["authorization"], "Bearer local-test");
        task.abort();
    }

    #[tokio::test]
    async fn canonical_invalid_filters_remain_bad_requests() {
        let (url, task) = fake_api().await;
        let client = BrandBriefingClient::new(&url, Duration::from_secs(2)).unwrap();
        let mut query = params();
        query.kind = Some("invalid".into());
        assert_eq!(
            client.fetch(&query, None).await.unwrap_err().0,
            StatusCode::BAD_REQUEST
        );
        task.abort();
    }

    #[tokio::test]
    async fn unavailable_canonical_service_never_falls_back_to_mixed_sql_feed() {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        drop(listener);
        let client = BrandBriefingClient::new(&url, Duration::from_millis(200)).unwrap();
        assert_eq!(
            client.fetch(&params(), None).await.unwrap_err().0,
            StatusCode::SERVICE_UNAVAILABLE
        );
    }

    #[tokio::test]
    async fn bounded_timeout_returns_service_unavailable() {
        let app = Router::new().route(
            "/api/v3/reports/by-brand",
            get(|| async { std::future::pending::<Json<Value>>().await }),
        );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let task = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        let client = BrandBriefingClient::new(&url, Duration::from_millis(50)).unwrap();
        assert_eq!(
            client.fetch(&params(), None).await.unwrap_err().0,
            StatusCode::SERVICE_UNAVAILABLE
        );
        task.abort();
    }

    #[tokio::test]
    async fn redirects_are_not_followed_and_wrong_json_shapes_are_unavailable() {
        let app = Router::new().route(
            "/api/v3/reports/by-brand",
            get(|Query(query): Query<HashMap<String, String>>| async move {
                if query.get("kind").map(String::as_str) == Some("redirect") {
                    return (
                        StatusCode::TEMPORARY_REDIRECT,
                        [("location", "http://127.0.0.1:1")],
                        Json(json!({})),
                    )
                        .into_response();
                }
                Json(json!({"error":"upstream shape without reports"})).into_response()
            }),
        );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let task = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        let client = BrandBriefingClient::new(&url, Duration::from_secs(2)).unwrap();
        for kind in ["redirect", "malformed"] {
            let mut query = params();
            query.kind = Some(kind.into());
            assert_eq!(
                client
                    .fetch(&query, Some(HeaderValue::from_static("Bearer local-test")))
                    .await
                    .unwrap_err()
                    .0,
                StatusCode::SERVICE_UNAVAILABLE
            );
        }
        task.abort();
    }

    #[test]
    fn rejects_public_credentials_and_non_base_urls() {
        for url in [
            "https://cleanapp.io",
            "http://user:secret@localhost:9081",
            "http://localhost:9081/arbitrary",
            "http://localhost:9081?token=secret",
            "http://localhost:9081#fragment",
            "file:///tmp/service",
            "http://8.8.8.8:9081",
            "http://[2001:4860:4860::8888]:9081",
        ] {
            assert!(
                BrandBriefingClient::new(url, Duration::from_secs(2)).is_err(),
                "must reject unsafe base URL"
            );
        }
        assert!(BrandBriefingClient::new(
            "http://cleanapp_report_listener:8080",
            Duration::from_secs(2)
        )
        .is_ok());
        assert!(BrandBriefingClient::new("http://[::1]:9081", Duration::from_secs(2)).is_ok());
    }
}
