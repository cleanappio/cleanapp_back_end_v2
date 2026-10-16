use lapin::{
    Channel, Connection, ConnectionProperties, Consumer, ExchangeKind,
    options::*,
    types::{AMQPValue, FieldTable},
};
use serde::de::DeserializeOwned;
use std::{collections::HashMap, sync::Arc, time::Duration};
use thiserror::Error;
use tokio::{
    sync::watch,
    task::JoinHandle,
    time::{sleep, timeout},
};

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
    let Some(h) = headers.as_ref() else {
        return 0;
    };
    // FieldTable is a thin wrapper around a map; access the inner map for lookups.
    let Some(v) = h.inner().get(RETRY_COUNT_HEADER) else {
        return 0;
    };
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
    #[error("Subscriber lifecycle failed: {0}")]
    Lifecycle(String),
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
    amqp_url: String,
    channel: Channel,
    exchange: String,
    queue: String,
    connection: Option<Connection>,
    shutdown: watch::Sender<bool>,
    task: Option<JoinHandle<Result<(), SubscriberError>>>,
}

impl Subscriber {
    async fn connect_channel(
        amqp_url: &str,
        exchange_name: &str,
        queue_name: &str,
    ) -> Result<(Connection, Channel, String), SubscriberError> {
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

        Ok((connection, channel, queue.name().to_string()))
    }

    /// Creates a new RabbitMQ subscriber instance
    pub async fn new(
        amqp_url: &str,
        exchange_name: &str,
        queue_name: &str,
    ) -> Result<Self, SubscriberError> {
        // Retry initial connection with backoff.
        //
        // In CI and during deployments, RabbitMQ can become "healthy" slightly before it accepts
        // TCP connections on 5672. Without retry, services can crashloop on a transient refusal.
        let mut backoff = Duration::from_millis(250);
        let (connection, channel, queue) = loop {
            match Self::connect_channel(amqp_url, exchange_name, queue_name).await {
                Ok(v) => break v,
                Err(e) => {
                    log::warn!(
                        "rabbitmq: initial connect failed; exchange={} queue={} err={} retry_in_ms={}",
                        exchange_name,
                        queue_name,
                        e,
                        backoff.as_millis()
                    );
                    sleep(backoff).await;
                    backoff = std::cmp::min(backoff.saturating_mul(2), Duration::from_secs(10));
                }
            }
        };
        let (shutdown, _) = watch::channel(false);
        Ok(Subscriber {
            amqp_url: amqp_url.to_string(),
            channel,
            exchange: exchange_name.to_string(),
            queue,
            connection: Some(connection),
            shutdown,
            task: None,
        })
    }

    /// Starts consuming messages from the queue with the specified routing key callbacks
    pub async fn start(
        &mut self,
        routing_key_callbacks: HashMap<String, Arc<dyn Callback + Send + Sync>>,
    ) -> Result<(), SubscriberError> {
        if self.task.is_some() || *self.shutdown.borrow() {
            return Err(SubscriberError::Lifecycle(
                "already started or stopping".into(),
            ));
        }
        let amqp_url = self.amqp_url.clone();
        let exchange = self.exchange.clone();
        let connect_queue = self.queue.clone();
        let callbacks = Arc::new(routing_key_callbacks);
        let mut channel = self.channel.clone();
        let mut queue_name = self.queue.clone();
        let mut connection = self.connection.take().ok_or_else(|| {
            SubscriberError::Lifecycle("connection already owned by consumer".into())
        })?;
        let mut shutdown = self.shutdown.subscribe();
        self.task = Some(tokio::spawn(async move {
            let mut backoff = Duration::from_secs(1);
            let result = loop {
                let res = Subscriber::run_once(
                    &channel,
                    &exchange,
                    &queue_name,
                    callbacks.clone(),
                    rabbitmq_concurrency(),
                    shutdown.clone(),
                )
                .await;
                if *shutdown.borrow() {
                    break res;
                }
                if let Err(error) = res {
                    log::error!(
                        "rabbitmq: consumer ended exchange={} queue={} err={}",
                        exchange,
                        queue_name,
                        error
                    );
                }
                tokio::select! {
                    biased;
                    _ = stop_requested(&mut shutdown) => break Ok(()),
                    _ = sleep(backoff) => {}
                }
                backoff = std::cmp::min(backoff.saturating_mul(2), Duration::from_secs(30));
                let next = tokio::select! {
                    biased;
                    _ = stop_requested(&mut shutdown) => break Ok(()),
                    result = Subscriber::connect_channel(&amqp_url, &exchange, &connect_queue) => result,
                };
                match next {
                    Ok((next_connection, ch, q)) => {
                        if connection.status().connected() {
                            let _ = connection.close(200, "reconnecting drained consumer").await;
                        }
                        connection = next_connection;
                        channel = ch;
                        queue_name = q;
                        backoff = Duration::from_secs(1);
                        log::info!(
                            "rabbitmq: reconnected exchange={} queue={}",
                            exchange,
                            queue_name
                        );
                    }
                    Err(error) => log::error!(
                        "rabbitmq: reconnect failed exchange={} queue={} err={}",
                        exchange,
                        connect_queue,
                        error
                    ),
                }
            };
            // run_once has joined every admitted callback + ack/nack future.
            // Buffered, unstarted deliveries are requeued by channel closure.
            let closed = if connection.status().connected() {
                connection
                    .close(200, "consumer callbacks drained")
                    .await
                    .map_err(|e| SubscriberError::ConnectionFailed(e.to_string()))
            } else {
                Ok(())
            };
            result.and(closed)
        }));
        Ok(())
    }

    /// Stop intake without aborting the admitted callback/ack futures.
    pub fn request_stop(&self) {
        self.shutdown.send_replace(true);
    }

    async fn run_once(
        channel: &Channel,
        exchange: &str,
        queue: &str,
        callbacks: Arc<HashMap<String, Arc<dyn Callback + Send + Sync>>>,
        workers: usize,
        mut shutdown: watch::Receiver<bool>,
    ) -> Result<(), SubscriberError> {
        let setup = async {
            // Create bindings for each routing key (idempotent).
            for routing_key in callbacks.keys() {
                channel
                    .queue_bind(
                        queue,
                        exchange,
                        routing_key,
                        QueueBindOptions::default(),
                        FieldTable::default(),
                    )
                    .await
                    .map_err(|e| {
                        SubscriberError::QueueBindFailed(format!(
                            "Failed to bind queue {} to exchange {} with routing key {}: {}",
                            queue, exchange, routing_key, e
                        ))
                    })?;
            }

            // Constrain in-flight deliveries to match our processing concurrency.
            channel
                .basic_qos(
                    u16::try_from(workers).unwrap_or(u16::MAX),
                    BasicQosOptions::default(),
                )
                .await
                .map_err(|e| SubscriberError::ChannelFailed(format!("failed to set QoS: {}", e)))?;

            let consumer = channel
                .basic_consume(
                    queue,
                    "",
                    BasicConsumeOptions {
                        no_ack: false,
                        exclusive: false,
                        no_local: false,
                        nowait: false,
                    },
                    FieldTable::default(),
                )
                .await
                .map_err(|e| SubscriberError::ConsumerRegistrationFailed(e.to_string()))?;

            Ok::<_, SubscriberError>(consumer)
        };
        let consumer = tokio::select! {
            biased;
            _ = stop_requested(&mut shutdown) => return Ok(()),
            consumer = setup => consumer?,
        };
        Self::process_messages(
            consumer,
            callbacks,
            channel.clone(),
            queue.to_string(),
            workers,
            shutdown,
        )
        .await
    }

    async fn process_messages(
        consumer: Consumer,
        callbacks: Arc<HashMap<String, Arc<dyn Callback + Send + Sync>>>,
        channel: Channel,
        queue_name: String,
        workers: usize,
        shutdown: watch::Receiver<bool>,
    ) -> Result<(), SubscriberError> {
        let retry_prefix = std::env::var(ENV_RETRY_EXCHANGE_PREFIX)
            .unwrap_or_else(|_| DEFAULT_RETRY_EXCHANGE_PREFIX.to_string());
        let retry_exchange = retry_exchange_for_queue(&retry_prefix, &queue_name);
        let max_retries = rabbitmq_max_retries();

        let consumer_tag = consumer.tag().to_string();
        let cancel_channel = channel.clone();
        consume_and_drain(consumer, workers, shutdown, move |delivery| {
                let callbacks = callbacks.clone();
                let channel = channel.clone();
                let queue_name = queue_name.clone();
                let retry_exchange = retry_exchange.clone();

                async move {
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
                        callback_err_str = Some(
                            SubscriberError::NoCallbackFound(routing_key.clone()).to_string(),
                        );
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
                        return Ok::<(), lapin::Error>(());
                    }

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
                                callback_err_str
                                    .clone()
                                    .unwrap_or_else(|| "error".to_string()),
                                nack_err
                            );
                            return Ok(());
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
                        return Ok(());
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
                        return Ok(());
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

                    Ok::<(), lapin::Error>(())
                }
            }, move || async move {
                cancel_channel.basic_cancel(&consumer_tag, BasicCancelOptions::default()).await
            })
            .await
            .map_err(|e| SubscriberError::ChannelFailed(format!("consumer drain error: {}", e)))?;

        Ok(())
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
    /// Cancel intake and JOIN the actual spawned consumer and all admitted
    /// callback/ack work before closing the connection. No timeout/abort.
    pub async fn close(mut self) -> Result<(), SubscriberError> {
        self.request_stop();
        if let Some(task) = self.task.take() {
            task.await
                .map_err(|e| SubscriberError::Lifecycle(e.to_string()))?
        } else if let Some(connection) = self.connection.take() {
            if connection.status().connected() {
                connection
                    .close(200, "subscriber closed before intake")
                    .await
                    .map_err(|e| SubscriberError::ConnectionFailed(e.to_string()))
            } else {
                Ok(())
            }
        } else {
            Ok(())
        }
    }
}

async fn stop_requested(stop: &mut watch::Receiver<bool>) {
    loop {
        if *stop.borrow_and_update() {
            return;
        }
        if stop.changed().await.is_err() {
            return;
        }
    }
}

/// Keep admitted work futures alive on shutdown AND on stream/callback errors.
/// Cancelling a `try_for_each_concurrent` future would silently drop them.
async fn consume_and_drain<S, T, E, F, Work, Cancel, Cancelled>(
    mut stream: S,
    workers: usize,
    mut stop: watch::Receiver<bool>,
    mut work: F,
    cancel: Cancel,
) -> Result<(), E>
where
    S: futures_util::Stream<Item = Result<T, E>> + Unpin,
    F: FnMut(T) -> Work,
    Work: std::future::Future<Output = Result<(), E>>,
    Cancel: FnOnce() -> Cancelled,
    Cancelled: std::future::Future<Output = Result<(), E>>,
{
    use futures_util::{StreamExt, stream::FuturesUnordered};
    let mut admitted = FuturesUnordered::new();
    let mut result = Ok(());
    let mut cancel = Some(cancel);
    loop {
        tokio::select! {
            biased;
            _ = stop_requested(&mut stop) => {
                result = cancel.take().expect("cancel used once")().await;
                break;
            }
            completed = admitted.next(), if !admitted.is_empty() => {
                if let Some(Err(error)) = completed { result = Err(error); break; }
            }
            item = stream.next(), if admitted.len() < workers.max(1) => {
                match item {
                    Some(Ok(item)) => admitted.push(work(item)),
                    Some(Err(error)) => { result = Err(error); break; }
                    None => break,
                }
            }
        }
    }
    while let Some(completed) = admitted.next().await {
        if result.is_ok() {
            result = completed;
        }
    }
    result
}

#[cfg(test)]
mod shutdown_tests {
    use super::consume_and_drain;
    use std::sync::{Arc, Mutex};
    use tokio::sync::{mpsc, oneshot, watch};
    use tokio_stream::wrappers::ReceiverStream;
    #[tokio::test]
    async fn cancel_joins_callback_then_ack_even_when_cancel_fails() {
        for cancel_error in [false, true] {
            let (stop, stopped) = watch::channel(false);
            let (input, stream) = mpsc::channel(4);
            let (began, started) = oneshot::channel();
            let (finish_work, work_done) = oneshot::channel();
            let (work_ended, work_ended_rx) = oneshot::channel();
            let (finish_ack, ack_done) = oneshot::channel();
            let (cancelled, cancel_seen) = oneshot::channel();
            let unit = Arc::new(Mutex::new(Some((began, work_done, work_ended, ack_done))));
            let task = tokio::spawn(consume_and_drain(
                ReceiverStream::new(stream),
                2,
                stopped,
                move |id| {
                    assert_eq!(id, 1, "shutdown admitted the buffered next delivery");
                    let (began, work, work_ended, ack) = unit.lock().unwrap().take().unwrap();
                    async move {
                        began.send(()).unwrap();
                        work.await.unwrap();
                        work_ended.send(()).unwrap();
                        ack.await.unwrap();
                        Ok::<_, &'static str>(())
                    }
                },
                move || async move {
                    cancelled.send(()).unwrap();
                    if cancel_error { Err("cancel") } else { Ok(()) }
                },
            ));
            input.send(Ok(1)).await.unwrap();
            started.await.unwrap();
            stop.send_replace(true);
            let _ = input.send(Ok(2)).await;
            cancel_seen.await.unwrap();
            assert!(!task.is_finished());
            finish_work.send(()).unwrap();
            work_ended_rx.await.unwrap();
            assert!(!task.is_finished(), "pending acknowledgment was dropped");
            finish_ack.send(()).unwrap();
            assert_eq!(
                task.await.unwrap(),
                if cancel_error { Err("cancel") } else { Ok(()) }
            );
        }
    }
    #[tokio::test]
    async fn stream_error_joins_admitted_callback_and_ack() {
        let (_stop, stopped) = watch::channel(false);
        let (input, stream) = mpsc::channel(2);
        let (began, started) = oneshot::channel();
        let (release, completed) = oneshot::channel();
        let unit = Arc::new(Mutex::new(Some((began, completed))));
        let task = tokio::spawn(consume_and_drain(
            ReceiverStream::new(stream),
            2,
            stopped,
            move |_| {
                let (began, completed) = unit.lock().unwrap().take().unwrap();
                async move {
                    began.send(()).unwrap();
                    completed.await.unwrap();
                    Ok::<_, &'static str>(())
                }
            },
            || async { Ok(()) },
        ));
        input.send(Ok(1)).await.unwrap();
        started.await.unwrap();
        input.send(Err("stream")).await.unwrap();
        tokio::task::yield_now().await;
        assert!(!task.is_finished());
        release.send(()).unwrap();
        assert_eq!(task.await.unwrap(), Err("stream"));
    }
    #[tokio::test]
    async fn pending_shutdown_admits_nothing() {
        let (stop, stopped) = watch::channel(false);
        stop.send_replace(true);
        let result = consume_and_drain(
            futures_util::stream::iter([Ok::<_, &'static str>(1)]),
            1,
            stopped,
            |_| async {
                panic!("pending shutdown started callback");
                #[allow(unreachable_code)]
                Ok(())
            },
            || async { Ok(()) },
        )
        .await;
        assert_eq!(result, Ok(()));
    }
}
