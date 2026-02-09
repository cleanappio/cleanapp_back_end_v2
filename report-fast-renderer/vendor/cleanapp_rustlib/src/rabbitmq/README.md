# RabbitMQ Publisher & Subscriber Library (Rust)

A Rust library for publishing and consuming messages from RabbitMQ using AMQP 0.9.1 protocol. This library provides the same functionality as the Go version in `golib/rabbitmq`.

## Features

### Publisher Features
- **Direct Exchange Support**: Declares and uses direct-type exchanges
- **JSON Message Publishing**: Automatically serializes Rust structs to JSON
- **Timeout Context**: Uses 60-second timeout for all operations
- **Persistent Messages**: Messages are marked as persistent for durability
- **Connection Management**: Proper connection and channel lifecycle management
- **Error Handling**: Comprehensive error handling with descriptive messages

### Subscriber Features
- **Non-exclusive Queues**: Creates durable, non-exclusive queues
- **Routing Key Bindings**: Supports multiple routing key bindings per queue
- **Callback-based Processing**: Uses callback functions for message processing
- **Explicit Acknowledgment**: Requires explicit ack after message processing
- **Async Processing**: Each message is processed asynchronously
- **Error Recovery**: Automatic message rejection on processing errors

## Installation

Add this to your `Cargo.toml`:

```toml
[dependencies]
rabbitmq = { path = "rustlib/rabbitmq" }
tokio = { version = "1.0", features = ["full"] }
serde = { version = "1.0", features = ["derive"] }
chrono = { version = "0.4", features = ["serde"] }
env_logger = "0.10"
```

## Usage

### Basic Publisher Usage

```rust
use chrono::{DateTime, Utc};
use rabbitmq::Publisher;
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize)]
struct Message {
    id: i32,
    content: String,
    timestamp: DateTime<Utc>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Create publisher
    let publisher = Publisher::new(
        "amqp://guest:guest@localhost:5672/", // AMQP URL
        "my_exchange",                        // Exchange name
        "my.routing.key",                     // Default routing key
    ).await?;

    // Publish message
    let message = Message {
        id: 1,
        content: "Hello World".to_string(),
        timestamp: Utc::now(),
    };
    
    publisher.publish(&message).await?;
    
    // Close publisher
    publisher.close().await?;
    
    Ok(())
}
```

### Publishing with Custom Routing Key

```rust
// Publish with a different routing key
publisher.publish_with_routing_key("custom.key", &message).await?;
```

### Connection Management

```rust
// Check if publisher is still connected
if publisher.is_connected() {
    // Publisher is healthy
}

// Get exchange and routing key info
let exchange = publisher.get_exchange();
let routing_key = publisher.get_routing_key();
```

## Subscriber Usage

### Basic Subscriber Usage

```rust
use rabbitmq::{CallbackFunc, Message, Subscriber};
use std::{collections::HashMap, sync::Arc};

#[derive(Serialize, Deserialize)]
struct Message {
    id: i32,
    content: String,
    timestamp: DateTime<Utc>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Create subscriber
    let mut subscriber = Subscriber::new(
        "amqp://guest:guest@localhost:5672/", // AMQP URL
        "my_exchange",                        // Exchange name
        "my_queue",                          // Queue name
    ).await?;

    // Define callbacks for different routing keys
    let mut callbacks: HashMap<String, CallbackFunc> = HashMap::new();
    
    callbacks.insert(
        "user.created".to_string(),
        Arc::new(handle_user_created) as CallbackFunc,
    );
    
    callbacks.insert(
        "user.updated".to_string(),
        Arc::new(handle_user_updated) as CallbackFunc,
    );
    
    callbacks.insert(
        "user.deleted".to_string(),
        Arc::new(handle_user_deleted) as CallbackFunc,
    );

    // Start consuming messages
    subscriber.start(callbacks).await?;

    // Keep running
    tokio::signal::ctrl_c().await?;
    
    // Close subscriber
    subscriber.close().await?;
    
    Ok(())
}

fn handle_user_created(msg: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let user: Message = msg.unmarshal_to()?;
    println!("User created: {:?}", user);
    Ok(())
}

fn handle_user_updated(msg: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let user: Message = msg.unmarshal_to()?;
    println!("User updated: {:?}", user);
    Ok(())
}

fn handle_user_deleted(msg: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let user: Message = msg.unmarshal_to()?;
    println!("User deleted: {:?}", user);
    Ok(())
}
```

### Subscriber Features

```rust
// Check if subscriber is still connected
if subscriber.is_connected() {
    // Subscriber is healthy
}

// Get exchange and queue info
let exchange = subscriber.get_exchange();
let queue = subscriber.get_queue();
```

## Running Examples

### Publisher Example

```bash
cd rustlib/rabbitmq
cargo run --example publisher_example
```

### Subscriber Example

```bash
cd rustlib/rabbitmq
cargo run --example subscriber_example
```

## Exchange Configuration

The library automatically declares exchanges with the following parameters:

- **Type**: `direct`
- **Durable**: `true`
- **Auto-deleted**: `false`
- **Internal**: `false`
- **No-wait**: `false`

## Message Format

All messages are automatically:
- Serialized to JSON format
- Set with `Content-Type: application/json`
- Marked as persistent (`DeliveryMode: 2`)
- Timestamped with the current time

## Error Handling

The library provides detailed error messages for common failure scenarios:

- Connection failures
- Channel creation failures
- Exchange declaration failures
- Message publishing failures
- Context timeouts

## Dependencies

- `lapin` - AMQP 0.9.1 client library
- `tokio` - Async runtime
- `serde` - Serialization framework
- `chrono` - Date and time handling
- `thiserror` - Error handling utilities

## Comparison with Go Version

This Rust implementation provides the same functionality as the Go version with the following differences:

1. **Async/Await**: Uses Rust's async/await instead of goroutines
2. **Type Safety**: Leverages Rust's type system for compile-time safety
3. **Memory Safety**: No garbage collector, deterministic memory management
4. **Error Handling**: Uses Result types instead of Go's error handling
5. **Ownership**: Uses Rust's ownership system for resource management

## License

This library is part of the CleanApp project.
