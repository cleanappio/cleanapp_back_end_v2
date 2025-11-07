use cleanapp_rustlib::rabbitmq::subscriber::{Callback, Message, Subscriber, SubscriberError};
use crate::config::Config;
use crate::services::tag_service;
use sqlx::MySqlPool;
use std::sync::Arc;
use log;
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct ReportWithTagsMessage {
    pub seq: i32,
    pub tags: Vec<String>,
}

pub struct ReportTagsSubscriber {
    subscriber: Subscriber,
}

impl ReportTagsSubscriber {
    pub async fn new(config: &Config) -> Result<Self, SubscriberError> {
        let amqp_url = config.amqp_url();
        let exchange = &config.rabbitmq_exchange;
        let queue = &config.rabbitmq_queue;

        log::info!(
            "Initializing RabbitMQ subscriber: exchange={}, queue={}",
            exchange,
            queue
        );

        let subscriber = Subscriber::new(&amqp_url, exchange, queue).await?;

        Ok(Self { subscriber })
    }

    pub async fn start(
        &mut self,
        pool: MySqlPool,
        routing_key: &str,
    ) -> Result<(), SubscriberError> {
        log::info!("Starting RabbitMQ subscriber for routing key: {}", routing_key);

        let pool = Arc::new(pool);
        let callback: Arc<dyn Callback> = Arc::new(ReportTagsCallback { pool });

        let mut callbacks: std::collections::HashMap<String, Arc<dyn Callback>> = std::collections::HashMap::new();
        callbacks.insert(routing_key.to_string(), callback);

        self.subscriber.start(callbacks).await?;

        log::info!("RabbitMQ subscriber started successfully");
        Ok(())
    }

    pub async fn close(self) -> Result<(), SubscriberError> {
        self.subscriber.close().await?;
        log::info!("RabbitMQ subscriber closed");
        Ok(())
    }
}

struct ReportTagsCallback {
    pool: Arc<MySqlPool>,
}

impl Callback for ReportTagsCallback {
    fn on_message(&self, message: &Message) -> Result<(), Box<dyn std::error::Error>> {
        // Deserialize the message
        let report_msg: ReportWithTagsMessage = match message.unmarshal_to() {
            Ok(msg) => msg,
            Err(e) => {
                log::error!("Failed to deserialize report message: {}", e);
                return Err(Box::new(e));
            }
        };

        log::info!(
            "Received report message: seq={}, tags={:?}",
            report_msg.seq,
            report_msg.tags
        );

        // Process tags asynchronously
        let pool = Arc::clone(&self.pool);
        let report_seq = report_msg.seq;
        let tags = report_msg.tags.clone();

        tokio::spawn(async move {
            match tag_service::add_tags_to_report(&pool, report_seq, tags).await {
                Ok(added_tags) => {
                    log::info!(
                        "Successfully processed tags for report {}: {:?}",
                        report_seq,
                        added_tags
                    );
                }
                Err(e) => {
                    log::error!(
                        "Failed to process tags for report {}: {}",
                        report_seq,
                        e
                    );
                }
            }
        });

        Ok(())
    }
}

