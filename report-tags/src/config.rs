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
    pub amqp_host: String,
    pub amqp_port: u16,
    pub amqp_user: String,
    pub amqp_password: String,
    pub rabbitmq_exchange: String,
    pub rabbitmq_queue: String,
    pub rabbitmq_raw_report_routing_key: String,
    pub rabbitmq_tag_event_routing_key: String,
}

impl Config {
    pub fn load() -> Self {
        let config = Self {
            db_host: env::var("DB_HOST").unwrap_or_else(|_| "localhost".to_string()),
            db_port: env::var("DB_PORT")
                .unwrap_or_else(|_| "3306".to_string())
                .parse()
                .unwrap_or(3306),
            db_user: env::var("DB_USER").unwrap_or_else(|_| "server".to_string()),
            db_password: env::var("DB_PASSWORD").unwrap_or_else(|_| "secret_app".to_string()),
            db_name: env::var("DB_NAME").unwrap_or_else(|_| "cleanapp".to_string()),
            port: env::var("PORT")
                .unwrap_or_else(|_| "8080".to_string())
                .parse()
                .unwrap_or(8080),
            redis_url: env::var("REDIS_URL").ok(),
            rust_log: env::var("RUST_LOG").unwrap_or_else(|_| "info".to_string()),
            max_tag_follows: env::var("MAX_TAG_FOLLOWS")
                .unwrap_or_else(|_| "200".to_string())
                .parse()
                .unwrap_or(200),
            amqp_host: env::var("AMQP_HOST").unwrap_or_else(|_| "localhost".to_string()),
            amqp_port: env::var("AMQP_PORT")
                .unwrap_or_else(|_| "5672".to_string())
                .parse()
                .unwrap_or(5672),
            amqp_user: env::var("AMQP_USER").unwrap_or_else(|_| "guest".to_string()),
            amqp_password: env::var("AMQP_PASSWORD").unwrap_or_else(|_| "guest".to_string()),
            rabbitmq_exchange: env::var("RABBITMQ_EXCHANGE")
                .unwrap_or_else(|_| "cleanapp".to_string()),
            rabbitmq_queue: env::var("RABBITMQ_QUEUE")
                .unwrap_or_else(|_| "report-tags".to_string()),
            rabbitmq_raw_report_routing_key: env::var("RABBITMQ_RAW_REPORT_ROUTING_KEY")
                .unwrap_or_else(|_| "report.raw".to_string()),
            rabbitmq_tag_event_routing_key: env::var("RABBITMQ_TAG_EVENT_ROUTING_KEY")
                .unwrap_or_else(|_| "tag.added".to_string()),
        };

        // Validate configuration
        if config.db_host.is_empty() {
            panic!("DB_HOST environment variable is required");
        }
        if config.db_user.is_empty() {
            panic!("DB_USER environment variable is required");
        }
        if config.db_password.is_empty() {
            panic!("DB_PASSWORD environment variable is required");
        }
        if config.db_name.is_empty() {
            panic!("DB_NAME environment variable is required");
        }
        if config.port == 0 {
            panic!("PORT environment variable must be a valid port number");
        }

        config
    }

    pub fn amqp_url(&self) -> String {
        format!(
            "amqp://{}:{}@{}:{}",
            self.amqp_user, self.amqp_password, self.amqp_host, self.amqp_port
        )
    }
}
