use anyhow::Result;
use cleanapp_rust_common::envx;
use serde::Deserialize;

#[derive(Clone, Debug, Deserialize)]
pub struct Config {
    pub db_host: String,
    pub db_port: String,
    pub db_user: String,
    pub db_password: String,
    pub db_name: String,
    pub http_port: u16,
    pub allowed_origins: Vec<String>,
    pub public_detail_rate_limit_rps: f64,
    pub public_detail_rate_limit_burst: usize,
    pub public_detail_abuse_window_seconds: u64,
    pub public_detail_abuse_max_hits: usize,
    pub public_detail_abuse_max_misses: usize,
}

impl Config {
    pub fn from_env() -> Result<Self> {
        let db_password = envx::optional("DB_PASSWORD")
            .ok_or_else(|| anyhow::anyhow!("DB_PASSWORD is required"))?;
        let db_host = envx::string("DB_HOST", "127.0.0.1");
        let db_port = envx::string("DB_PORT", "3306");
        let db_user = envx::string("DB_USER", "server");
        let db_name = envx::string("DB_NAME", "cleanapp");
        let http_port = envx::parse("HTTP_PORT", "9084");
        let allowed_origins = envx::list(
            "ALLOWED_ORIGINS",
            "https://cleanapp.io,https://www.cleanapp.io,https://api.cleanapp.io,https://live.cleanapp.io,http://localhost:3000,http://127.0.0.1:3000,http://localhost:5173,http://127.0.0.1:5173",
        );
        let public_detail_rate_limit_rps = envx::parse("PUBLIC_DETAIL_RATE_LIMIT_RPS", "1.5");
        let public_detail_rate_limit_burst = envx::parse("PUBLIC_DETAIL_RATE_LIMIT_BURST", "8");
        let public_detail_abuse_window_seconds =
            envx::parse("PUBLIC_DETAIL_ABUSE_WINDOW_SECONDS", "600");
        let public_detail_abuse_max_hits = envx::parse("PUBLIC_DETAIL_ABUSE_MAX_HITS", "60");
        let public_detail_abuse_max_misses = envx::parse("PUBLIC_DETAIL_ABUSE_MAX_MISSES", "12");
        Ok(Self {
            db_host,
            db_port,
            db_user,
            db_password,
            db_name,
            http_port,
            allowed_origins,
            public_detail_rate_limit_rps,
            public_detail_rate_limit_burst,
            public_detail_abuse_window_seconds,
            public_detail_abuse_max_hits,
            public_detail_abuse_max_misses,
        })
    }
}
