use serde::{Deserialize, Serialize};
use std::env;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    pub server_port: String,
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
}

impl Config {
    pub fn from_env() -> Result<Self, ConfigError> {
        let server_port = env::var("SERVER_PORT")
            .map_err(|_| ConfigError::MissingEnvVar("SERVER_PORT".to_string()))?;

        let db_host =
            env::var("DB_HOST").map_err(|_| ConfigError::MissingEnvVar("DB_HOST".to_string()))?;

        let db_port =
            env::var("DB_PORT").map_err(|_| ConfigError::MissingEnvVar("DB_PORT".to_string()))?;

        let db_user =
            env::var("DB_USER").map_err(|_| ConfigError::MissingEnvVar("DB_USER".to_string()))?;
        let db_password = env::var("DB_PASSWORD")
            .map_err(|_| ConfigError::MissingEnvVar("DB_PASSWORD".to_string()))?;

        let db_name =
            env::var("DB_NAME").map_err(|_| ConfigError::MissingEnvVar("DB_NAME".to_string()))?;

        let amqp_host = env::var("AMQP_HOST")
            .map_err(|_| ConfigError::MissingEnvVar("AMQP_HOST".to_string()))?;

        let amqp_port = env::var("AMQP_PORT")
            .map_err(|_| ConfigError::MissingEnvVar("AMQP_PORT".to_string()))?
            .parse::<u16>()
            .map_err(|e| ConfigError::InvalidEnvVar("AMQP_PORT".to_string(), e.to_string()))?;

        let amqp_user = env::var("AMQP_USER")
            .map_err(|_| ConfigError::MissingEnvVar("AMQP_USER".to_string()))?;

        let amqp_password = env::var("AMQP_PASSWORD")
            .map_err(|_| ConfigError::MissingEnvVar("AMQP_PASSWORD".to_string()))?;

        let exchange = env::var("RABBITMQ_EXCHANGE")
            .map_err(|_| ConfigError::MissingEnvVar("RABBITMQ_EXCHANGE".to_string()))?;

        let queue_name = env::var("RABBITMQ_RENDERER_QUEUE_NAME")
            .map_err(|_| ConfigError::MissingEnvVar("RABBITMQ_RENDERER_QUEUE_NAME".to_string()))?;

        let routing_key = env::var("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY").map_err(|_| {
            ConfigError::MissingEnvVar("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY".to_string())
        })?;

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
        })
    }

    pub fn amqp_url(&self) -> String {
        format!(
            "amqp://{}:{}@{}:{}",
            self.amqp_user, self.amqp_password, self.amqp_host, self.amqp_port
        )
    }

    pub fn validate(&self) -> Result<(), ConfigError> {
        if self.server_port.is_empty() {
            return Err(ConfigError::InvalidEnvVar(
                "SERVER_PORT".to_string(),
                "cannot be empty".to_string(),
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
            server_port: "3000".to_string(),
            amqp_host: "localhost".to_string(),
            amqp_port: 5672,
            amqp_user: "guest".to_string(),
            amqp_password: "guest".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
            db_host: "localhost".to_string(),
            db_port: "3306".to_string(),
            db_user: "root".to_string(),
            db_password: "password".to_string(),
            db_name: "test_db".to_string(),
        };

        assert_eq!(config.amqp_url(), "amqp://guest:guest@localhost:5672");
    }

    #[test]
    fn test_config_validation() {
        let valid_config = Config {
            server_port: "3000".to_string(),
            amqp_host: "localhost".to_string(),
            amqp_port: 5672,
            amqp_user: "guest".to_string(),
            amqp_password: "guest".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
            db_host: "localhost".to_string(),
            db_port: "3306".to_string(),
            db_user: "root".to_string(),
            db_password: "password".to_string(),
            db_name: "test_db".to_string(),
        };

        assert!(valid_config.validate().is_ok());

        let invalid_config = Config {
            server_port: "".to_string(),
            amqp_host: "".to_string(),
            amqp_port: 5672,
            amqp_user: "guest".to_string(),
            amqp_password: "guest".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
            db_host: "".to_string(),
            db_port: "".to_string(),
            db_user: "".to_string(),
            db_password: "".to_string(),
            db_name: "".to_string(),
        };

        assert!(invalid_config.validate().is_err());
    }
}
