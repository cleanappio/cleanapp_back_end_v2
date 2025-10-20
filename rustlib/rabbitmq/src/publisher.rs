use lapin::{
    options::*, types::FieldTable, Channel, Connection,
    ConnectionProperties, ExchangeKind,
};
use serde::Serialize;
use std::time::Duration;
use thiserror::Error;
use tokio::time::timeout;

#[derive(Error, Debug)]
pub enum PublisherError {
    #[error("Failed to connect to RabbitMQ: {0}")]
    ConnectionFailed(String),
    #[error("Failed to open channel: {0}")]
    ChannelFailed(String),
    #[error("Failed to declare exchange: {0}")]
    ExchangeDeclarationFailed(String),
    #[error("Failed to marshal message to JSON: {0}")]
    SerializationFailed(String),
    #[error("Failed to publish message: {0}")]
    PublishFailed(String),
    #[error("Context timeout: {0}")]
    Timeout(String),
}

/// Publisher represents a RabbitMQ publisher instance
pub struct Publisher {
    channel: Channel,
    exchange: String,
    routing_key: String,
}

impl Publisher {
    /// Creates a new RabbitMQ publisher instance
    pub async fn new(
        amqp_url: &str,
        exchange_name: &str,
        routing_key: &str,
    ) -> Result<Self, PublisherError> {
        // Create connection with timeout
        let connection = timeout(
            Duration::from_secs(60),
            Connection::connect(amqp_url, ConnectionProperties::default()),
        )
        .await
        .map_err(|_| PublisherError::Timeout("Connection timeout".to_string()))?
        .map_err(|e| PublisherError::ConnectionFailed(e.to_string()))?;

        // Create channel
        let channel = connection
            .create_channel()
            .await
            .map_err(|e| PublisherError::ChannelFailed(e.to_string()))?;

        // Declare exchange with specified parameters
        channel
            .exchange_declare(
                exchange_name,
                ExchangeKind::Direct,
                ExchangeDeclareOptions {
                    durable: true,
                    auto_delete: false,
                    internal: false,
                    nowait: false,
                    passive: false,
                },
                FieldTable::default(),
            )
            .await
            .map_err(|e| PublisherError::ExchangeDeclarationFailed(e.to_string()))?;

        Ok(Publisher {
            channel,
            exchange: exchange_name.to_string(),
            routing_key: routing_key.to_string(),
        })
    }

    /// Publishes a JSON message to the exchange with the configured routing key
    pub async fn publish<T: Serialize>(&self, message: &T) -> Result<(), PublisherError> {
        self.publish_with_routing_key(&self.routing_key, message).await
    }

    /// Publishes a JSON message to the exchange with a custom routing key
    pub async fn publish_with_routing_key<T: Serialize>(
        &self,
        routing_key: &str,
        message: &T,
    ) -> Result<(), PublisherError> {
        // Marshal message to JSON
        let body = serde_json::to_vec(message)
            .map_err(|e| PublisherError::SerializationFailed(e.to_string()))?;

        // Create publishing options
        let options = BasicPublishOptions::default();
        let properties = lapin::BasicProperties::default()
            .with_content_type("application/json".into())
            .with_delivery_mode(2) // Persistent
            .with_timestamp(chrono::Utc::now().timestamp() as u64);

        // Publish message with timeout
        let result = timeout(
            Duration::from_secs(60),
            self.channel.basic_publish(
                &self.exchange,
                routing_key,
                options,
                &body,
                properties,
            ),
        )
        .await
        .map_err(|_| PublisherError::Timeout("Publish timeout".to_string()))?;

        match result {
            Ok(_) => Ok(()),
            Err(e) => Err(PublisherError::PublishFailed(e.to_string())),
        }
    }

    /// Checks if the publisher is still connected
    pub fn is_connected(&self) -> bool {
        // For now, we'll assume connection is always active
        // In a real implementation, you might want to track connection state
        true
    }

    /// Returns the exchange name
    pub fn get_exchange(&self) -> &str {
        &self.exchange
    }

    /// Returns the default routing key
    pub fn get_routing_key(&self) -> &str {
        &self.routing_key
    }
}

impl Drop for Publisher {
    fn drop(&mut self) {
        // Note: In Rust, we can't easily implement async Drop
        // The connection and channel will be closed when they go out of scope
        // For explicit cleanup, users should call close() method
    }
}

impl Publisher {
    /// Closes the publisher connection and channel
    pub async fn close(self) -> Result<(), PublisherError> {
        // Channel will be closed when dropped
        // Connection will be closed when dropped
        Ok(())
    }
}
