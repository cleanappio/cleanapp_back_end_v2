use anyhow::Result;
use serde::Deserialize;

#[derive(Clone, Debug, Deserialize)]
pub struct Config {
    pub db_host: String,
    pub db_port: String,
    pub db_user: String,
    pub db_password: String,
    pub db_name: String,
    pub http_port: u16,
}

impl Config {
    pub fn from_env() -> Result<Self> {
        let db_host = std::env::var("DB_HOST").unwrap_or_else(|_| "127.0.0.1".into());
        let db_port = std::env::var("DB_PORT").unwrap_or_else(|_| "3306".into());
        let db_user = std::env::var("DB_USER").unwrap_or_else(|_| "server".into());
        let db_password = std::env::var("DB_PASSWORD").unwrap_or_default();
        let db_name = std::env::var("DB_NAME").unwrap_or_else(|_| "cleanapp".into());
        let http_port = std::env::var("HTTP_PORT").ok().and_then(|s| s.parse().ok()).unwrap_or(9084);
        Ok(Self { db_host, db_port, db_user, db_password, db_name, http_port })
    }
}


