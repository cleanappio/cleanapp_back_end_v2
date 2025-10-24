use cleanapp_rustlib::rabbitmq::subscriber::{
  Subscriber,
  SubscriberError,
  CallbackFunc,
  Message,
};
use crate::config::get_config;
use tracing::{info, debug};
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

    pub async fn start_listening(&mut self) -> Result<(), SubscriberError> {
        info!("Starting FastRendererSubscriber listener...");
        
        // Create routing key callbacks
        let config = get_config();
        let mut routing_key_callbacks: HashMap<String, CallbackFunc> = HashMap::new();
        
        // Add callback for the analysed report routing key
        let callback: CallbackFunc = Arc::new(default_message_handler);
        routing_key_callbacks.insert(config.routing_key.clone(), callback);
        
        // Start the subscriber
        self.subscriber.start(routing_key_callbacks).await?;
        
        Ok(())
    }
}

// Default callback for handling messages
pub fn default_message_handler(message: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    debug!("Received message: {:?}", message);
    
    // TODO: Implement actual message processing logic
    // This is where you would:
    // 1. Parse the message content
    // 2. Extract report data
    // 3. Process/render the report
    // 4. Send response back or store results
    
    info!("Message processed successfully");
    Ok(())
}
