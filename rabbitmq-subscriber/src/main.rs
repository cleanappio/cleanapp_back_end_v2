use chrono::{DateTime, Utc};
use rabbitmq::{CallbackFunc, Message, Subscriber};
use serde::{Deserialize, Serialize};
use std::{collections::HashMap, sync::Arc};

#[derive(Serialize, Deserialize, Debug)]
struct ExampleMessage {
    id: i32,
    message: String,
    timestamp: DateTime<Utc>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Initialize logging
    env_logger::init();

    // RabbitMQ connection URL
    let amqp_url = "amqp://guest:guest@localhost:5672";

    // Create a new subscriber
    let mut subscriber = Subscriber::new(amqp_url, "example_exchange", "example_queue").await?;

    // Define callback functions for different routing keys
    let mut callbacks: HashMap<String, CallbackFunc> = HashMap::new();
    
    callbacks.insert(
        "example.routing.key".to_string(),
        Arc::new(handle_example_message) as CallbackFunc,
    );
    
    callbacks.insert(
        "custom.routing.key".to_string(),
        Arc::new(handle_custom_message) as CallbackFunc,
    );
    
    callbacks.insert(
        "error.routing.key".to_string(),
        Arc::new(handle_error_message) as CallbackFunc,
    );

    // Start consuming messages
    subscriber.start(callbacks).await?;

    println!("Subscriber started successfully!");
    println!(
        "Listening on exchange: {}, queue: {}",
        subscriber.get_exchange(),
        subscriber.get_queue()
    );
    println!("Press Ctrl+C to stop...");

    // Keep the program running
    tokio::signal::ctrl_c().await?;
    println!("Shutting down...");

    // Close the subscriber
    subscriber.close().await?;

    Ok(())
}

// handle_example_message processes messages with "example.routing.key" routing key
fn handle_example_message(msg: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let example_msg: ExampleMessage = msg.unmarshal_to()?;

    println!(
        "Received example message: ID={}, Message={}, Time={}",
        example_msg.id,
        example_msg.message,
        example_msg.timestamp.format("%Y-%m-%dT%H:%M:%S%.3fZ")
    );

    Ok(())
}

// handle_custom_message processes messages with "custom.routing.key" routing key
fn handle_custom_message(msg: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let example_msg: ExampleMessage = msg.unmarshal_to()?;

    println!(
        "Received custom message: ID={}, Message={}, Time={}",
        example_msg.id,
        example_msg.message,
        example_msg.timestamp.format("%Y-%m-%dT%H:%M:%S%.3fZ")
    );

    Ok(())
}

// handle_error_message simulates an error scenario
fn handle_error_message(msg: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    println!("Received error message: {}", String::from_utf8_lossy(&msg.body));

    // Simulate an error - this will cause the message to be rejected
    Err("simulated processing error".into())
}