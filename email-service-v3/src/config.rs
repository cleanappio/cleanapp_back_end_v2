use anyhow::{Context, Result};
use cleanapp_rust_common::envx;
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
    pub bcc_email_address: String,
    pub enable_email_v3: bool,
}

impl Config {
    pub fn from_env() -> Result<Self> {
        dotenvy::dotenv().ok();
        let db_host = envx::string("DB_HOST", "localhost");
        let db_port = envx::string("DB_PORT", "3306");
        let db_user = envx::string("DB_USER", "server");
        let db_password = envx::required("DB_PASSWORD");
        let db_name = envx::string("DB_NAME", "cleanapp");

        let sendgrid_api_key = envx::string("SENDGRID_API_KEY", "");
        let sendgrid_from_name = envx::string("SENDGRID_FROM_NAME", "CleanApp");
        let sendgrid_from_email = envx::string("SENDGRID_FROM_EMAIL", "info@cleanapp.io");

        let poll_interval = humantime::parse_duration(&envx::string("POLL_INTERVAL", "10s"))?;
        let http_port: u16 = envx::string("HTTP_PORT", "8080")
            .parse()
            .context("HTTP_PORT parse")?;
        let opt_out_url = envx::string("OPT_OUT_URL", "https://cleanapp.io/api/optout");

        let notification_period =
            humantime::parse_duration(&envx::string("NOTIFICATION_PERIOD", "90d"))?;
        let digital_base_url = envx::string("DIGITAL_BASE_URL", "https://cleanapp.io/api/email");
        let env_name = envx::string("ENV", "prod");
        let test_brands_raw = envx::string("TEST_BRANDS", "");
        let test_brands = {
            let v: Vec<String> = test_brands_raw
                .split(',')
                .map(|s| s.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect();
            if v.is_empty() {
                None
            } else {
                Some(v)
            }
        };
        let bcc_email_address = envx::string("BCC_EMAIL_ADDRESS", "cleanapp@stxn.io");
        let enable_email_v3 = matches!(
            envx::string("ENABLE_EMAIL_V3", "true")
                .to_lowercase()
                .as_str(),
            "1" | "true" | "yes" | "on"
        );

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
            bcc_email_address,
            enable_email_v3,
        })
    }

    pub fn mysql_masked_url(&self) -> String {
        format!(
            "mysql://{}:***@{}:{}/{}",
            self.db_user, self.db_host, self.db_port, self.db_name
        )
    }
}
