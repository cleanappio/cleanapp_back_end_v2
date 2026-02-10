use lapin::{
    options::*,
    types::{AMQPValue, FieldTable},
    Channel, Connection, ConnectionProperties, Consumer, ExchangeKind,
};
use serde::de::DeserializeOwned;
use std::{collections::HashMap, sync::Arc, time::Duration};
use thiserror::Error;
use tokio::time::timeout;

const DEFAULT_CONCURRENCY: usize = 20;
const ENV_CONCURRENCY: &str = "RABBITMQ_CONCURRENCY";

const DEFAULT_MAX_RETRIES: u32 = 10;
const ENV_MAX_RETRIES: &str = "RABBITMQ_MAX_RETRIES";

const DEFAULT_RETRY_EXCHANGE_PREFIX: &str = "cleanapp-retry.";
const ENV_RETRY_EXCHANGE_PREFIX: &str = "RABBITMQ_RETRY_EXCHANGE_PREFIX";

const RETRY_COUNT_HEADER: &str = "x-cleanapp-retry-count";

fn rabbitmq_concurrency() -> usize {
    let v = std::env::var(ENV_CONCURRENCY).ok();
    let Some(v) = v else {
        return DEFAULT_CONCURRENCY;
    };
    match v.parse::<usize>() {
        Ok(n) if n > 0 => n,
        _ => {
            log::warn!(
                "rabbitmq: invalid {}={:?}, using default={}",
                ENV_CONCURRENCY,
                v,
                DEFAULT_CONCURRENCY
            );
            DEFAULT_CONCURRENCY
        }
    }
}

fn rabbitmq_max_retries() -> u32 {
    let v = std::env::var(ENV_MAX_RETRIES).ok();
    let Some(v) = v else {
        return DEFAULT_MAX_RETRIES;
    };
    match v.parse::<u32>() {
        Ok(n) => n,
        _ => {
            log::warn!(
                "rabbitmq: invalid {}={:?}, using default={}",
                ENV_MAX_RETRIES,
                v,
                DEFAULT_MAX_RETRIES
            );
            DEFAULT_MAX_RETRIES
        }
    }
}

fn retry_exchange_for_queue(prefix: &str, queue: &str) -> String {
    format!("{}{}", prefix, queue)
}

fn retry_count_from_headers(headers: &Option<FieldTable>) -> u32 {
    let Some(h) = headers.as_ref() else { return 0; };
    // FieldTable is a thin wrapper around a map; access the inner map for lookups.
    let Some(v) = h.inner().get(RETRY_COUNT_HEADER) else { return 0; };
    match v {
        AMQPValue::LongUInt(n) => *n,
        AMQPValue::LongInt(n) => (*n).try_into().unwrap_or(0),
        AMQPValue::LongLongInt(n) => (*n).try_into().unwrap_or(0),
        _ => 0,
    }
}

fn with_retry_count(mut props: lapin::BasicProperties, next: u32) -> lapin::BasicProperties {
    let mut headers = props
        .headers()
        .as_ref()
        .cloned()
        .unwrap_or_else(FieldTable::default);
    headers.insert(RETRY_COUNT_HEADER.into(), AMQPValue::LongUInt(next));
    props = props.with_headers(headers);
    props
}

#[derive(Debug)]
pub struct PermanentError {
    err: Box<dyn std::error::Error + Send + Sync>,
}

impl PermanentError {
    pub fn new(err: Box<dyn std::error::Error + Send + Sync>) -> Self {
        Self { err }
    }
}

impl std::fmt::Display for PermanentError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.err)
    }
}

impl std::error::Error for PermanentError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        Some(&*self.err)
    }
}

/// Wrap an error as a permanent (non-retriable) error.
///
/// Subscriber will `Nack(requeue=false)`, which will dead-letter if the queue has a DLX configured.
pub fn permanent<E>(err: E) -> Box<dyn std::error::Error>
where
    E: std::error::Error + Send + Sync + 'static,
{
    Box::new(PermanentError::new(Box::new(err)))
}

fn is_permanent(err: &(dyn std::error::Error + 'static)) -> bool {
    err.is::<PermanentError>()
}

#[derive(Error, Debug)]
pub enum SubscriberError {
    #[error("Failed to connect to RabbitMQ: {0}")]
    ConnectionFailed(String),
    #[error("Failed to open channel: {0}")]
    ChannelFailed(String),
    #[error("Failed to declare exchange: {0}")]
    ExchangeDeclarationFailed(String),
    #[error("Failed to declare queue: {0}")]
    QueueDeclarationFailed(String),
    #[error("Failed to bind queue: {0}")]
    QueueBindFailed(String),
    #[error("Failed to register consumer: {0}")]
    ConsumerRegistrationFailed(String),
    #[error("Context timeout: {0}")]
    Timeout(String),
    #[error("No callback found for routing key: {0}")]
    NoCallbackFound(String),
}

/// Message represents a received RabbitMQ message
#[derive(Debug, Clone)]
pub struct Message {
    pub body: Vec<u8>,
    pub routing_key: String,
    pub exchange: String,
    pub content_type: Option<String>,
    pub timestamp: Option<u64>,
    pub delivery_tag: u64,
}

impl Message {
    /// Unmarshals the message body into the provided type
    pub fn unmarshal_to<T: DeserializeOwned>(&self) -> Result<T, serde_json::Error> {
        serde_json::from_slice(&self.body)
    }
}

pub trait Callback {
    fn on_message(&self, message: &Message) -> Result<(), Box<dyn std::error::Error>>;
}

/// Subscriber represents a RabbitMQ subscriber instance
pub struct Subscriber {
    channel: Channel,
    exchange: String,
    queue: String,
}

impl Subscriber {
    /// Creates a new RabbitMQ subscriber instance
    pub async fn new(
        amqp_url: &str,
        exchange_name: &str,
        queue_name: &str,
    ) -> Result<Self, SubscriberError> {
        // Create connection with timeout
        let connection = timeout(
            Duration::from_secs(60),
            Connection::connect(amqp_url, ConnectionProperties::default()),
        )
        .await
        .map_err(|_| SubscriberError::Timeout("Connection timeout".to_string()))?
        .map_err(|e| SubscriberError::ConnectionFailed(e.to_string()))?;

        // Create channel
        let channel = connection
            .create_channel()
            .await
            .map_err(|e| SubscriberError::ChannelFailed(e.to_string()))?;

        // Declare exchange with specified parameters (same as publisher)
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
            .map_err(|e| SubscriberError::ExchangeDeclarationFailed(e.to_string()))?;

        // Declare queue with non-exclusive, durable settings
        let queue = channel
            .queue_declare(
                queue_name,
                QueueDeclareOptions {
                    durable: true,
                    exclusive: false,
                    auto_delete: false,
                    nowait: false,
                    passive: false,
                },
                FieldTable::default(),
            )
            .await
            .map_err(|e| SubscriberError::QueueDeclarationFailed(e.to_string()))?;

        Ok(Subscriber {
            channel,
            exchange: exchange_name.to_string(),
            queue: queue.name().to_string(),
        })
    }

    /// Starts consuming messages from the queue with the specified routing key callbacks
    pub async fn start(
        &mut self,
        routing_key_callbacks: HashMap<String, Arc<dyn Callback + Send + Sync>>,
    ) -> Result<(), SubscriberError> {
        // Create bindings for each routing key
        for routing_key in routing_key_callbacks.keys() {
            self.channel
                .queue_bind(
                    &self.queue,
                    &self.exchange,
                    routing_key,
                    QueueBindOptions::default(),
                    FieldTable::default(),
                )
                .await
                .map_err(|e| {
                    SubscriberError::QueueBindFailed(format!(
                        "Failed to bind queue {} to exchange {} with routing key {}: {}",
                        self.queue, self.exchange, routing_key, e
                    ))
                })?;
        }

        let workers = rabbitmq_concurrency();
        // Constrain in-flight deliveries to match our processing concurrency.
        if let Err(e) = self
            .channel
            .basic_qos(
                u16::try_from(workers).unwrap_or(u16::MAX),
                BasicQosOptions::default(),
            )
            .await
        {
            return Err(SubscriberError::ChannelFailed(format!(
                "failed to set QoS: {}",
                e
            )));
        }

        // Start consuming messages
        let consumer = self
            .channel
            .basic_consume(
                &self.queue,
                "",
                BasicConsumeOptions {
                    no_ack: false, // Manual ack
                    exclusive: false,
                    no_local: false,
                    nowait: false,
                },
                FieldTable::default(),
            )
            .await
            .map_err(|e| SubscriberError::ConsumerRegistrationFailed(e.to_string()))?;

        // Process messages (bounded concurrency; ack/nack after processing).
        self.process_messages(consumer, routing_key_callbacks, workers)
            .await;

        Ok(())
    }

    /// Processes incoming messages
    async fn process_messages(
        &self,
        consumer: Consumer,
        routing_key_callbacks: HashMap<String, Arc<dyn Callback + Send + Sync>>,
        workers: usize,
    ) {
        let callbacks = Arc::new(routing_key_callbacks);
        let channel = self.channel.clone();
        let queue_name = self.queue.clone();
        let retry_prefix =
            std::env::var(ENV_RETRY_EXCHANGE_PREFIX).unwrap_or_else(|_| DEFAULT_RETRY_EXCHANGE_PREFIX.to_string());
        let retry_exchange = retry_exchange_for_queue(&retry_prefix, &queue_name);
        let max_retries = rabbitmq_max_retries();

        tokio::spawn(async move {
            use futures_util::stream::StreamExt;

            let mut message_stream = consumer;

            // Process deliveries concurrently with a fixed cap.
            message_stream
                .for_each_concurrent(workers, |delivery_res| {
                    let callbacks = callbacks.clone();
                    let channel = channel.clone();
                    let queue_name = queue_name.clone();
                    let retry_exchange = retry_exchange.clone();

                    async move {
                        let delivery = match delivery_res {
                            Ok(d) => d,
                            Err(e) => {
                                log::error!("rabbitmq: delivery error: {}", e);
                                return;
                            }
                        };

                        let started_at = std::time::Instant::now();
                        let routing_key = delivery.routing_key.clone().to_string();
                        let exchange = delivery.exchange.clone().to_string();
                        let delivery_tag = delivery.delivery_tag;
                        let redelivered = delivery.redelivered;

                        log::info!(
                            "rabbitmq worker_start exchange={} queue={} routing_key={} delivery_tag={} redelivered={}",
                            exchange,
                            queue_name,
                            routing_key,
                            delivery_tag,
                            redelivered
                        );

                        let message = Message {
                            body: delivery.data.clone(),
                            routing_key: routing_key.clone(),
                            exchange: exchange.clone(),
                            content_type: delivery
                                .properties
                                .content_type()
                                .as_ref()
                                .map(|s| s.to_string()),
                            timestamp: delivery.properties.timestamp().as_ref().copied(),
                            delivery_tag,
                        };

                        let mut action = "ack";
                        let mut requeue = false;
                        let mut retry_to_exchange = false;
                        let retry_count = retry_count_from_headers(delivery.properties.headers());
                        // Keep errors as strings so this worker future stays `Send` across awaits.
                        let mut callback_err_str: Option<String> = None;
                        let mut panic_val: Option<String> = None;

                        if let Some(callback) = callbacks.get(&routing_key) {
                            let res = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                                callback.on_message(&message)
                            }));
                            match res {
                                Ok(Ok(())) => {}
                                Ok(Err(e)) => {
                                    action = "nack";
                                    requeue = !is_permanent(&*e);
                                    retry_to_exchange = requeue;
                                    callback_err_str = Some(e.to_string());
                                }
                                Err(p) => {
                                    action = "nack";
                                    requeue = false;
                                    let s = if let Some(s) = p.downcast_ref::<&str>() {
                                        s.to_string()
                                    } else if let Some(s) = p.downcast_ref::<String>() {
                                        s.clone()
                                    } else {
                                        "panic".to_string()
                                    };
                                    panic_val = Some(s);
                                }
                            }
                        } else {
                            action = "nack";
                            requeue = false; // no handler -> permanent
                            callback_err_str = Some(SubscriberError::NoCallbackFound(routing_key.clone()).to_string());
                        }

                        let duration_ms = started_at.elapsed().as_millis();
                        if action == "ack" {
                            let ack_err = channel
                                .basic_ack(delivery_tag, BasicAckOptions::default())
                                .await
                                .err();
                            log::info!(
                                "rabbitmq worker_finish routing_key={} delivery_tag={} duration_ms={} action=ack ack_err={:?}",
                                routing_key,
                                delivery_tag,
                                duration_ms,
                                ack_err
                            );
                        } else {
                            // Transient error: move message to per-queue retry exchange (delayed via <queue>.retry TTL),
                            // then ack the original delivery to prevent tight requeue loops.
                            if retry_to_exchange {
                                if retry_count >= max_retries {
                                    // Retry budget exhausted -> send to DLQ via Nack(requeue=false).
                                    let nack_err = channel
                                        .basic_nack(
                                            delivery_tag,
                                            BasicNackOptions {
                                                multiple: false,
                                                requeue: false,
                                            },
                                        )
                                        .await
                                        .err();
                                    log::error!(
                                        "rabbitmq worker_finish routing_key={} delivery_tag={} duration_ms={} action=nack requeue=false retries_exhausted=true retry_count={} max_retries={} err={} nack_err={:?}",
                                        routing_key,
                                        delivery_tag,
                                        duration_ms,
                                        retry_count,
                                        max_retries,
                                        callback_err_str.clone().unwrap_or_else(|| "error".to_string()),
                                        nack_err
                                    );
                                    return;
                                }

                                let next_retry = retry_count.saturating_add(1);
                                let props = with_retry_count(delivery.properties.clone(), next_retry);

                                let publish_err = channel
                                    .basic_publish(
                                        &retry_exchange,
                                        &routing_key,
                                        BasicPublishOptions::default(),
                                        &delivery.data,
                                        props,
                                    )
                                    .await
                                    .err();

                                if publish_err.is_none() {
                                    let ack_err = channel
                                        .basic_ack(delivery_tag, BasicAckOptions::default())
                                        .await
                                        .err();
                                    log::error!(
                                        "rabbitmq worker_finish routing_key={} delivery_tag={} duration_ms={} action=retry retry_exchange={} retry_count_next={} max_retries={} ack_err={:?}",
                                        routing_key,
                                        delivery_tag,
                                        duration_ms,
                                        retry_exchange,
                                        next_retry,
                                        max_retries,
                                        ack_err
                                    );
                                } else {
                                    // Fallback: if retry exchange isn't configured yet, requeue the original.
                                    let nack_err = channel
                                        .basic_nack(
                                            delivery_tag,
                                            BasicNackOptions {
                                                multiple: false,
                                                requeue: true,
                                            },
                                        )
                                        .await
                                        .err();
                                    log::error!(
                                        "rabbitmq worker_finish routing_key={} delivery_tag={} duration_ms={} action=nack requeue=true retry_exchange={} retry_count={} max_retries={} publish_err={:?} nack_err={:?}",
                                        routing_key,
                                        delivery_tag,
                                        duration_ms,
                                        retry_exchange,
                                        retry_count,
                                        max_retries,
                                        publish_err,
                                        nack_err
                                    );
                                }
                                return;
                            }

                            let nack_err = channel
                                .basic_nack(
                                    delivery_tag,
                                    BasicNackOptions {
                                        multiple: false,
                                        requeue,
                                    },
                                )
                                .await
                                .err();

                            if let Some(pv) = panic_val {
                                log::error!(
                                    "rabbitmq worker_finish routing_key={} delivery_tag={} duration_ms={} action=nack requeue={} panic={} nack_err={:?}",
                                    routing_key,
                                    delivery_tag,
                                    duration_ms,
                                    requeue,
                                    pv,
                                    nack_err
                                );
                                return;
                            }

                            if let Some(e) = callback_err_str {
                                log::error!(
                                    "rabbitmq worker_finish routing_key={} delivery_tag={} duration_ms={} action=nack requeue={} err={} nack_err={:?}",
                                    routing_key,
                                    delivery_tag,
                                    duration_ms,
                                    requeue,
                                    e,
                                    nack_err
                                );
                            } else {
                                log::error!(
                                    "rabbitmq worker_finish routing_key={} delivery_tag={} duration_ms={} action=nack requeue={} nack_err={:?}",
                                    routing_key,
                                    delivery_tag,
                                    duration_ms,
                                    requeue,
                                    nack_err
                                );
                            }
                        }
                    }
                })
                .await;
        });
    }

    /// Checks if the subscriber is still connected
    pub fn is_connected(&self) -> bool {
        // For now, we'll assume connection is always active
        // In a real implementation, you might want to track connection state
        true
    }

    /// Returns the exchange name
    pub fn get_exchange(&self) -> &str {
        &self.exchange
    }

    /// Returns the queue name
    pub fn get_queue(&self) -> &str {
        &self.queue
    }
}

impl Drop for Subscriber {
    fn drop(&mut self) {
        // Note: In Rust, we can't easily implement async Drop
        // The connection and channel will be closed when they go out of scope
        // For explicit cleanup, users should call close() method
    }
}

impl Subscriber {
    /// Closes the subscriber connection and channel
    pub async fn close(self) -> Result<(), SubscriberError> {
        // Channel will be closed when dropped
        // Connection will be closed when dropped
        Ok(())
    }
}
