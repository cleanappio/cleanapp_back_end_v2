use serde::{Deserialize, Serialize};
use std::env;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
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
        
        let routing_key = env::var("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY")
            .map_err(|_| ConfigError::MissingEnvVar("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY".to_string()))?;

        Ok(Config {
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
        format!("amqp://{}:{}@{}:{}", 
                self.amqp_user, 
                self.amqp_password, 
                self.amqp_host, 
                self.amqp_port)
    }

    pub fn validate(&self) -> Result<(), ConfigError> {
        if self.amqp_host.is_empty() {
            return Err(ConfigError::InvalidEnvVar("AMQP_HOST".to_string(), "cannot be empty".to_string()));
        }
        
        if self.amqp_user.is_empty() {
            return Err(ConfigError::InvalidEnvVar("AMQP_USER".to_string(), "cannot be empty".to_string()));
        }
        
        if self.amqp_password.is_empty() {
            return Err(ConfigError::InvalidEnvVar("AMQP_PASSWORD".to_string(), "cannot be empty".to_string()));
        }
        
        if self.exchange.is_empty() {
            return Err(ConfigError::InvalidEnvVar("RABBITMQ_EXCHANGE".to_string(), "cannot be empty".to_string()));
        }
        
        if self.queue_name.is_empty() {
            return Err(ConfigError::InvalidEnvVar("RABBITMQ_RENDERER_QUEUE_NAME".to_string(), "cannot be empty".to_string()));
        }
        
        if self.routing_key.is_empty() {
            return Err(ConfigError::InvalidEnvVar("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY".to_string(), "cannot be empty".to_string()));
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
    
    CONFIG.set(config)
        .map_err(|_| ConfigError::InvalidEnvVar("CONFIG".to_string(), "Config already initialized".to_string()))?;
    
    Ok(())
}

pub fn get_config() -> &'static Config {
    CONFIG.get().expect("Config not initialized. Call init_config() first.")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_amqp_url_formatting() {
        let config = Config {
            amqp_host: "localhost".to_string(),
            amqp_port: 5672,
            amqp_user: "guest".to_string(),
            amqp_password: "guest".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
        };

        assert_eq!(config.amqp_url(), "amqp://guest:guest@localhost:5672");
    }

    #[test]
    fn test_config_validation() {
        let valid_config = Config {
            amqp_host: "localhost".to_string(),
            amqp_port: 5672,
            amqp_user: "guest".to_string(),
            amqp_password: "guest".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
        };

        assert!(valid_config.validate().is_ok());

        let invalid_config = Config {
            amqp_host: "".to_string(),
            amqp_port: 5672,
            amqp_user: "guest".to_string(),
            amqp_password: "guest".to_string(),
            exchange: "test_exchange".to_string(),
            queue_name: "test_queue".to_string(),
            routing_key: "test_routing_key".to_string(),
        };

        assert!(invalid_config.validate().is_err());
    }
}
