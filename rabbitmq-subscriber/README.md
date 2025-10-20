# RabbitMQ Subscriber - Standalone Application

A standalone Rust application for consuming messages from RabbitMQ using AMQP 0.9.1 protocol. This application uses the `rabbitmq` library from `rustlib/rabbitmq`.

## Features

- **Non-exclusive Queues**: Creates durable, non-exclusive queues
- **Routing Key Bindings**: Supports multiple routing key bindings per queue
- **Callback-based Processing**: Uses callback functions for message processing
- **Explicit Acknowledgment**: Requires explicit ack after message processing
- **Async Processing**: Each message is processed asynchronously
- **Error Recovery**: Automatic message rejection on processing errors

## Prerequisites

- Rust 1.70+ installed
- RabbitMQ server running (default: `amqp://guest:guest@localhost:5672`)

## Installation

1. Clone or download this application
2. Navigate to the application directory:
   ```bash
   cd rabbitmq-subscriber
   ```

3. Build the application:
   ```bash
   cargo build --release
   ```

## Usage

### Basic Usage

Run the subscriber application:

```bash
cargo run
```

This will:
1. Connect to RabbitMQ at `amqp://guest:guest@localhost:5672`
2. Create an exchange named `example_exchange`
3. Create a queue named `example_queue`
4. Bind the queue to routing keys: `example.routing.key`, `custom.routing.key`, `error.routing.key`
5. Start consuming messages and processing them with appropriate callbacks
6. Keep running until Ctrl+C is pressed

### Customizing the Application

You can modify the `main.rs` file to:

1. **Change connection URL**:
   ```rust
   let amqp_url = "amqp://username:password@host:port/vhost";
   ```

2. **Change exchange and queue names**:
   ```rust
   let mut subscriber = Subscriber::new(amqp_url, "my_exchange", "my_queue").await?;
   ```

3. **Add custom routing key callbacks**:
   ```rust
   callbacks.insert(
       "user.created".to_string(),
       Arc::new(handle_user_created) as CallbackFunc,
   );
   ```

4. **Implement custom message handlers**:
   ```rust
   fn handle_user_created(msg: &Message) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
       let user: UserMessage = msg.unmarshal_to()?;
       println!("User created: {:?}", user);
       // Your processing logic here
       Ok(())
   }
   ```

## Message Processing

The application processes messages using callback functions:

- **`handle_example_message`**: Processes messages with `example.routing.key`
- **`handle_custom_message`**: Processes messages with `custom.routing.key`
- **`handle_error_message`**: Processes messages with `error.routing.key` (simulates errors)

### Message Structure

Messages are automatically deserialized from JSON. The example uses:

```rust
struct ExampleMessage {
    id: i32,
    message: String,
    timestamp: DateTime<Utc>,
}
```

## Queue Configuration

The application automatically creates queues with the following parameters:

- **Durable**: `true`
- **Exclusive**: `false`
- **Auto-delete**: `false`
- **No-wait**: `false`

## Exchange Configuration

The application automatically declares exchanges with the following parameters:

- **Type**: `direct`
- **Durable**: `true`
- **Auto-deleted**: `false`
- **Internal**: `false`
- **No-wait**: `false`

## Error Handling

The application provides comprehensive error handling:

- **Connection failures**: Detailed error messages for connection issues
- **Channel creation failures**: Handles channel creation errors
- **Exchange/Queue declaration failures**: Handles declaration errors
- **Message processing errors**: Automatically rejects messages on processing errors
- **Callback not found**: Rejects messages when no callback is registered for a routing key

## Message Acknowledgment

- **Success**: Messages are acknowledged (`ACK`) after successful processing
- **Error**: Messages are rejected (`NACK`) on processing errors
- **No Callback**: Messages are rejected when no callback is found for the routing key

## Building for Production

```bash
cargo build --release
```

The binary will be available at `target/release/rabbitmq-subscriber`.

## Running the Binary

```bash
./target/release/rabbitmq-subscriber
```

## Logging

The application uses `env_logger` for logging. You can control log levels using environment variables:

```bash
RUST_LOG=debug cargo run
RUST_LOG=info cargo run
RUST_LOG=warn cargo run
RUST_LOG=error cargo run
```

## Testing with Publisher

To test the subscriber, you can use the companion `rabbitmq-publisher` application:

1. Start the subscriber:
   ```bash
   cargo run
   ```

2. In another terminal, run the publisher:
   ```bash
   cd ../rabbitmq-publisher
   cargo run
   ```

The subscriber should receive and process the messages published by the publisher.

## Graceful Shutdown

The application handles graceful shutdown:

- Press `Ctrl+C` to stop the application
- The application will finish processing current messages
- Connections are properly closed
- Resources are cleaned up

## Dependencies

- `rabbitmq` - RabbitMQ library from `rustlib/rabbitmq`
- `tokio` - Async runtime
- `serde` - Serialization framework
- `chrono` - Date and time handling
- `env_logger` - Logging

## License

This application is part of the CleanApp project.
