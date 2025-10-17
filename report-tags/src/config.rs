use std::env;

#[derive(Debug, Clone)]
pub struct Config {
    pub db_host: String,
    pub db_port: u16,
    pub db_user: String,
    pub db_password: String,
    pub db_name: String,
    pub port: u16,
    pub redis_url: Option<String>,
    pub rust_log: String,
    pub max_tag_follows: u32,
}

impl Config {
    pub fn load() -> Self {
        Self {
            db_host: env::var("DB_HOST").unwrap_or_else(|_| "localhost".to_string()),
            db_port: env::var("DB_PORT")
                .unwrap_or_else(|_| "3306".to_string())
                .parse()
                .unwrap_or(3306),
            db_user: env::var("DB_USER").unwrap_or_else(|_| "server".to_string()),
            db_password: env::var("DB_PASSWORD").unwrap_or_else(|_| "secret_app".to_string()),
            db_name: env::var("DB_NAME").unwrap_or_else(|_| "cleanapp".to_string()),
            port: env::var("PORT")
                .unwrap_or_else(|_| "8083".to_string())
                .parse()
                .unwrap_or(8083),
            redis_url: env::var("REDIS_URL").ok(),
            rust_log: env::var("RUST_LOG").unwrap_or_else(|_| "info".to_string()),
            max_tag_follows: env::var("MAX_TAG_FOLLOWS")
                .unwrap_or_else(|_| "200".to_string())
                .parse()
                .unwrap_or(200),
        }
    }
}
