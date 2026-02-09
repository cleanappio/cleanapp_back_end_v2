use cleanapp_rustlib::rabbitmq::subscriber::{Callback, Message, Subscriber, SubscriberError};
use crate::config::Config;
use crate::services::tag_service;
use sqlx::MySqlPool;
use std::sync::Arc;
use log;
use serde::{Deserialize, Serialize};
use std::future::Future;

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct ReportWithTagsMessage {
    pub seq: i32,
    #[serde(default)]
    pub tags: Option<Vec<String>>,
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
        let callback: Arc<dyn Callback + Send + Sync + 'static> = Arc::new(ReportTagsCallback { pool });

        let mut callbacks: std::collections::HashMap<String, Arc<dyn Callback + Send + Sync + 'static>> =
            std::collections::HashMap::new();
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

fn block_on<F: Future>(fut: F) -> F::Output {
    tokio::task::block_in_place(|| tokio::runtime::Handle::current().block_on(fut))
}

impl Callback for ReportTagsCallback {
    fn on_message(&self, message: &Message) -> Result<(), Box<dyn std::error::Error>> {
        // Try to deserialize the message - handle both formats:
        // 1. Messages with tags field (explicit tags)
        // 2. Messages without tags field (raw reports - skip tag processing)
        let report_msg: ReportWithTagsMessage = match message.unmarshal_to() {
            Ok(msg) => msg,
            Err(e) => {
                // Log the error but don't fail - some messages might not have tags
                // This happens when report.raw messages don't include a tags field
                log::debug!("Failed to deserialize report message (may not have tags field): {}. Skipping tag processing.", e);
                // Return Ok to acknowledge the message even though we can't process it
                return Ok(());
            }
        };

        // Skip processing if no tags provided
        let tags = match report_msg.tags {
            Some(tags) if !tags.is_empty() => tags,
            _ => {
                log::debug!(
                    "Report {} has no tags, skipping tag processing",
                    report_msg.seq
                );
                return Ok(());
            }
        };

        log::info!(
            "Received report message with tags: seq={}, tags={:?}",
            report_msg.seq,
            tags
        );

        // Process inline: ack/nack decision depends on *this* returning success/failure.
        let pool = Arc::clone(&self.pool);
        let report_seq = report_msg.seq;
        let tags_vec = tags;

        let res = block_on(async move {
            tag_service::add_tags_to_report(&pool, report_seq, tags_vec).await
        });

        match res {
            Ok(added_tags) => {
                log::info!(
                    "Successfully processed tags for report {}: {:?}",
                    report_seq,
                    added_tags
                );
                Ok(())
            }
            Err(e) => {
                log::error!("Failed to process tags for report {}: {}", report_seq, e);
                Err(Box::new(e))
            }
        }
    }
}
