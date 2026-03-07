use cleanapp_rust_common::envx;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    pub server_port: u16,
    pub db_host: String,
    pub db_port: String,
    pub db_user: String,
    pub db_password: String,
    pub db_name: String,
    pub amqp_host: String,
    pub amqp_port: u16,
    pub amqp_user: String,
    pub amqp_password: String,
    pub exchange: String,
    pub queue_name: String,
    pub routing_key: String,
    pub allowed_origins: Vec<String>,
}

impl Config {
    pub fn from_env() -> Result<Self, ConfigError> {
        let server_port = envx::parse("SERVER_PORT", "8080");
        let db_host = envx::string("DB_HOST", "127.0.0.1");
        let db_port = envx::string("DB_PORT", "3306");
        let db_user = envx::string("DB_USER", "server");
        let db_password = envx::optional("DB_PASSWORD")
            .ok_or_else(|| ConfigError::MissingEnvVar("DB_PASSWORD".to_string()))?;
        let db_name = envx::string("DB_NAME", "cleanapp");
        let amqp_host = envx::string("AMQP_HOST", "127.0.0.1");
        let amqp_port = envx::parse("AMQP_PORT", "5672");
        let amqp_user = envx::string("AMQP_USER", "cleanapp");
        let amqp_password = envx::optional("AMQP_PASSWORD")
            .ok_or_else(|| ConfigError::MissingEnvVar("AMQP_PASSWORD".to_string()))?;
        let exchange = envx::string("RABBITMQ_EXCHANGE", "cleanapp");
        let queue_name = envx::string("RABBITMQ_RENDERER_QUEUE_NAME", "report-renderer");
        let routing_key = envx::string("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY", "report.analysed");
        let allowed_origins = envx::list(
            "ALLOWED_ORIGINS",
            "https://cleanapp.io,https://www.cleanapp.io,https://api.cleanapp.io,https://live.cleanapp.io,http://localhost:3000,http://127.0.0.1:3000,http://localhost:5173,http://127.0.0.1:5173",
        );

        Ok(Config {
            server_port,
            db_host,
            db_port,
            db_user,
            db_password,
            db_name,
            amqp_host,
            amqp_port,
            amqp_user,
            amqp_password,
            exchange,
            queue_name,
            routing_key,
            allowed_origins,
        })
    }

    pub fn amqp_url(&self) -> String {
        format!(
            "amqp://{}:{}@{}:{}",
            self.amqp_user, self.amqp_password, self.amqp_host, self.amqp_port
        )
    }

    pub fn validate(&self) -> Result<(), ConfigError> {
        if self.server_port == 0 {
            return Err(ConfigError::InvalidEnvVar(
                "SERVER_PORT".to_string(),
                "must be a valid port number".to_string(),
            ));
        }

        if self.db_host.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "DB_HOST".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.db_port.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "DB_PORT".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.db_user.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "DB_USER".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.db_password.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "DB_PASSWORD".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.db_name.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "DB_NAME".to_string(),
                "cannot be empty".to_string(),
            ));
        }
        if self.amqp_host.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "AMQP_HOST".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.amqp_user.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "AMQP_USER".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.amqp_password.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "AMQP_PASSWORD".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.exchange.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "RABBITMQ_EXCHANGE".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.queue_name.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "RABBITMQ_RENDERER_QUEUE_NAME".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.routing_key.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "RABBITMQ_ANALYSED_REPORT_ROUTING_KEY".to_string(),
                "cannot be empty".to_string(),
            ));
        }

        if self.allowed_origins.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "ALLOWED_ORIGINS".to_string(),
                "must contain at least one origin".to_string(),
            ));
        }

        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    #[error("Missing environment variable: {0}")]
    MissingEnvVar(String),

    #[error("Invalid environment variable {0}: {1}")]
    InvalidEnvVar(String, String),
}

use std::sync::OnceLock;

// Global config instance using OnceLock for thread safety
static CONFIG: OnceLock<Config> = OnceLock::new();

pub fn init_config() -> Result<(), ConfigError> {
    let config = Config::from_env()?;
    config.validate()?;

    CONFIG.set(config).map_err(|_| {
        ConfigError::InvalidEnvVar(
            "CONFIG".to_string(),
            "Config already initialized".to_string(),
        )
    })?;

    Ok(())
}

pub fn get_config() -> &'static Config {
    CONFIG
        .get()
        .expect("Config not initialized. Call init_config() first.")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_amqp_url_formatting() {
        let config = Config {
            server_port: 3000,
            amqp_host: "localhost".to_string(),
            amqp_port: 5672,
            amqp_user: "test-user".to_string(),
            amqp_password: "test-password".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
            db_host: "localhost".to_string(),
            db_port: "3306".to_string(),
            db_user: "root".to_string(),
            db_password: "test-password".to_string(),
            db_name: "test_db".to_string(),
            allowed_origins: vec!["http://localhost:3000".to_string()],
        };

        assert_eq!(
            config.amqp_url(),
            "amqp://test-user:test-password@localhost:5672"
        );
    }

    #[test]
    fn test_config_validation() {
        let valid_config = Config {
            server_port: 3000,
            amqp_host: "localhost".to_string(),
            amqp_port: 5672,
            amqp_user: "test-user".to_string(),
            amqp_password: "test-password".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
            db_host: "localhost".to_string(),
            db_port: "3306".to_string(),
            db_user: "root".to_string(),
            db_password: "test-password".to_string(),
            db_name: "test_db".to_string(),
            allowed_origins: vec!["http://localhost:3000".to_string()],
        };

        assert!(valid_config.validate().is_ok());

        let invalid_config = Config {
            server_port: 0,
            amqp_host: "".to_string(),
            amqp_port: 5672,
            amqp_user: "test-user".to_string(),
            amqp_password: "test-password".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
            db_host: "".to_string(),
            db_port: "".to_string(),
            db_user: "".to_string(),
            db_password: "".to_string(),
            db_name: "".to_string(),
            allowed_origins: vec![],
        };

        assert!(invalid_config.validate().is_err());
    }
}
