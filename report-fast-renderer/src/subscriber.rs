use cleanapp_rustlib::rabbitmq::subscriber::{
  Subscriber,
  SubscriberError,
  Callback,
};
use crate::config::get_config;
use tracing::info;
use std::{collections::HashMap, sync::Arc};

pub struct FastRendererSubscriber {
    subscriber: Subscriber,
}

impl FastRendererSubscriber {
    pub async fn new() -> Result<Self, SubscriberError> {
        let config = get_config();
        let amqp_url = config.amqp_url();
        let exchange_name = &config.exchange;
        let queue_name = &config.queue_name;
        
        info!("Initializing FastRendererSubscriber with exchange: {}, queue: {}", exchange_name, queue_name);
        
        let subscriber = Subscriber::new(&amqp_url, exchange_name, queue_name).await?;
        Ok(Self { 
            subscriber: subscriber
        })
    }

    pub async fn start_listening(&mut self, callback: Arc<dyn Callback + Send + Sync + 'static>) -> Result<(), SubscriberError> {
        info!("Starting FastRendererSubscriber listener...");
        
        // Create routing key callbacks
        let config = get_config();
        let mut routing_key_callbacks: HashMap<String, Arc<dyn Callback + Send + Sync + 'static>> = HashMap::new();

        // Add callback for the analysed report routing key
        routing_key_callbacks.insert(config.routing_key.clone(), callback);

        // Start the subscriber
        self.subscriber.start(routing_key_callbacks).await?;
        
        Ok(())
    }
}
