use cleanapp_rust_common::envx;

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
            db_host: envx::string("DB_HOST", "localhost"),
            db_port: envx::parse("DB_PORT", "3306"),
            db_user: envx::string("DB_USER", "server"),
            db_password: envx::required("DB_PASSWORD"),
            db_name: envx::string("DB_NAME", "cleanapp"),
            port: envx::parse("PORT", "8080"),
            redis_url: envx::optional("REDIS_URL"),
            rust_log: envx::string("RUST_LOG", "info"),
            max_tag_follows: envx::parse("MAX_TAG_FOLLOWS", "200"),
            amqp_host: envx::string("AMQP_HOST", "localhost"),
            amqp_port: envx::parse("AMQP_PORT", "5672"),
            amqp_user: envx::string("AMQP_USER", "cleanapp"),
            amqp_password: envx::required("AMQP_PASSWORD"),
            rabbitmq_exchange: envx::string("RABBITMQ_EXCHANGE", "cleanapp"),
            rabbitmq_queue: envx::string("RABBITMQ_QUEUE", "report-tags"),
            rabbitmq_raw_report_routing_key: envx::string(
                "RABBITMQ_RAW_REPORT_ROUTING_KEY",
                "report.raw",
            ),
            rabbitmq_tag_event_routing_key: envx::string(
                "RABBITMQ_TAG_EVENT_ROUTING_KEY",
                "tag.added",
            ),
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
