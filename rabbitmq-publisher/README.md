# RabbitMQ Publisher - Standalone Application

A standalone Rust application for publishing messages to RabbitMQ using AMQP 0.9.1 protocol. This application uses the `rabbitmq` library from `rustlib/rabbitmq`.

## Features

- **Direct Exchange Support**: Declares and uses direct-type exchanges
- **JSON Message Publishing**: Automatically serializes Rust structs to JSON
- **Timeout Context**: Uses 60-second timeout for all operations
- **Persistent Messages**: Messages are marked as persistent for durability
- **Connection Management**: Proper connection and channel lifecycle management
- **Error Handling**: Comprehensive error handling with descriptive messages

## Prerequisites

- Rust 1.70+ installed
- RabbitMQ server running (default: `amqp://guest:guest@localhost:5672`)

## Installation

1. Clone or download this application
2. Navigate to the application directory:
   ```bash
   cd rabbitmq-publisher
   ```

3. Build the application:
   ```bash
   cargo build --release
   ```

## Usage

### Basic Usage

Run the publisher application:

```bash
cargo run
```

This will:
1. Connect to RabbitMQ at `amqp://guest:guest@localhost:5672`
2. Create an exchange named `example_exchange`
3. Publish a sample message with routing key `example.routing.key`
4. Publish another message with routing key `custom.routing.key`
5. Display connection status
6. Close the connection

### Customizing the Application

You can modify the `main.rs` file to:

1. **Change connection URL**:
   ```rust
   let amqp_url = "amqp://username:password@host:port/vhost";
   ```

2. **Change exchange name**:
   ```rust
   let publisher = Publisher::new(amqp_url, "my_exchange", "my.routing.key").await?;
   ```

3. **Publish custom messages**:
   ```rust
   let custom_message = MyStruct {
       field1: "value1",
       field2: 42,
       timestamp: Utc::now(),
   };
   publisher.publish(&custom_message).await?;
   ```

4. **Use different routing keys**:
   ```rust
   publisher.publish_with_routing_key("different.key", &message).await?;
   ```

## Message Format

All messages are automatically:
- Serialized to JSON format
- Set with `Content-Type: application/json`
- Marked as persistent (`DeliveryMode: 2`)
- Timestamped with the current time

## Exchange Configuration

The application automatically declares exchanges with the following parameters:

- **Type**: `direct`
- **Durable**: `true`
- **Auto-deleted**: `false`
- **Internal**: `false`
- **No-wait**: `false`

## Error Handling

The application provides detailed error messages for common failure scenarios:

- Connection failures
- Channel creation failures
- Exchange declaration failures
- Message publishing failures
- Context timeouts

## Dependencies

- `rabbitmq` - RabbitMQ library from `rustlib/rabbitmq`
- `tokio` - Async runtime
- `serde` - Serialization framework
- `chrono` - Date and time handling
- `env_logger` - Logging

## Building for Production

```bash
cargo build --release
```

The binary will be available at `target/release/rabbitmq-publisher`.

## Running the Binary

```bash
./target/release/rabbitmq-publisher
```

## Logging

The application uses `env_logger` for logging. You can control log levels using environment variables:

```bash
RUST_LOG=debug cargo run
RUST_LOG=info cargo run
RUST_LOG=warn cargo run
RUST_LOG=error cargo run
```

## License

This application is part of the CleanApp project.
