use crate::config::Config;
use crate::rabbitmq::messages::TagAddedEvent;
use cleanapp_rustlib::rabbitmq::publisher::Publisher as RustLibPublisher;
use chrono::Utc;
use anyhow::Result;
use log;

pub struct TagEventPublisher {
    publisher: RustLibPublisher,
    routing_key: String,
}

impl TagEventPublisher {
    pub async fn new(config: &Config) -> Result<Self> {
        let amqp_url = config.amqp_url();
        let exchange = &config.rabbitmq_exchange;
        let routing_key = &config.rabbitmq_tag_event_routing_key;

        log::info!(
            "Initializing RabbitMQ publisher: exchange={}, routing_key={}",
            exchange,
            routing_key
        );

        let publisher = RustLibPublisher::new(&amqp_url, exchange, routing_key).await?;

        Ok(Self {
            publisher,
            routing_key: routing_key.clone(),
        })
    }

    pub async fn publish_tag_added(&self, report_seq: i32, tags: Vec<String>) -> Result<()> {
        let event = TagAddedEvent {
            report_seq,
            tags,
            timestamp: Utc::now(),
        };

        self.publisher.publish(&event).await?;
        log::debug!("Published TagAddedEvent for report_seq: {}", report_seq);
        Ok(())
    }

    pub async fn close(self) -> Result<()> {
        self.publisher.close().await?;
        log::info!("RabbitMQ publisher closed");
        Ok(())
    }

    pub fn is_connected(&self) -> bool {
        self.publisher.is_connected()
    }
}

