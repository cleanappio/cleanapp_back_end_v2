use anyhow::{Context, Result};
use std::time::Duration;

#[derive(Clone, Debug)]
pub struct Config {
    // Database
    pub db_host: String,
    pub db_port: String,
    pub db_user: String,
    pub db_password: String,
    pub db_name: String,

    // SendGrid
    pub sendgrid_api_key: String,
    pub sendgrid_from_name: String,
    pub sendgrid_from_email: String,

    // Service
    pub poll_interval: Duration,
    pub http_port: u16,
    pub opt_out_url: String,

    // V3 extras
    pub notification_period: Duration,
    pub digital_base_url: String,
    pub env: String,
    pub test_brands: Option<Vec<String>>,
}

impl Config {
    pub fn from_env() -> Result<Self> {
        dotenvy::dotenv().ok();
        let db_host = env("DB_HOST", "localhost");
        let db_port = env("DB_PORT", "3306");
        let db_user = env("DB_USER", "server");
        let db_password = env("DB_PASSWORD", "secret");
        let db_name = env("DB_NAME", "cleanapp");

        let sendgrid_api_key = env("SENDGRID_API_KEY", "");
        let sendgrid_from_name = env("SENDGRID_FROM_NAME", "CleanApp");
        let sendgrid_from_email = env("SENDGRID_FROM_EMAIL", "info@cleanapp.io");

        let poll_interval = humantime::parse_duration(&env("POLL_INTERVAL", "10s"))?;
        let http_port: u16 = env("HTTP_PORT", "8080").parse().context("HTTP_PORT parse")?;
        let opt_out_url = env("OPT_OUT_URL", "http://localhost:8080/opt-out");

        let notification_period = humantime::parse_duration(&env("NOTIFICATION_PERIOD", "90d"))?;
        let digital_base_url = env("DIGITAL_BASE_URL", "https://cleanapp.io/api/email");
        let env_name = env("ENV", "prod");
        let test_brands_raw = env("TEST_BRANDS", "");
        let test_brands = {
            let v: Vec<String> = test_brands_raw
                .split(',')
                .map(|s| s.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect();
            if v.is_empty() { None } else { Some(v) }
        };

        Ok(Self {
            db_host,
            db_port,
            db_user,
            db_password,
            db_name,
            sendgrid_api_key,
            sendgrid_from_name,
            sendgrid_from_email,
            poll_interval,
            http_port,
            opt_out_url,
            notification_period,
            digital_base_url,
            env: env_name,
            test_brands,
        })
    }

    pub fn mysql_masked_url(&self) -> String {
        format!(
            "mysql://{}:***@{}:{}/{}",
            self.db_user, self.db_host, self.db_port, self.db_name
        )
    }
}

fn env(key: &str, default: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| default.to_string())
}

