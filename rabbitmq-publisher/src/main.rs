use chrono::{DateTime, Utc};
use rabbitmq::Publisher;
use serde::{Deserialize, Serialize};

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

    // Create a new publisher
    let publisher = Publisher::new(amqp_url, "example_exchange", "example.routing.key").await?;

    // Example message
    let message = ExampleMessage {
        id: 1,
        message: "Hello from RabbitMQ Publisher!".to_string(),
        timestamp: Utc::now(),
    };

    // Publish the message
    publisher.publish(&message).await?;
    println!("Message published successfully!");

    // Example of publishing with a custom routing key
    let custom_message = ExampleMessage {
        id: 2,
        message: "Custom routing key message".to_string(),
        timestamp: Utc::now(),
    };

    publisher.publish_with_routing_key("custom.routing.key", &custom_message).await?;
    println!("Custom message published successfully!");

    // Check connection status
    if publisher.is_connected() {
        println!("Publisher is still connected");
    } else {
        println!("Publisher connection is lost");
    }

    // Close the publisher
    publisher.close().await?;

    Ok(())
}