# RabbitMQ Publisher & Subscriber Library

A Go library for publishing and consuming messages from RabbitMQ using AMQP 0.9.1 protocol.

## Features

### Publisher Features
- **Direct Exchange Support**: Declares and uses direct-type exchanges
- **JSON Message Publishing**: Automatically marshals Go structs to JSON
- **Timeout Context**: Uses 60-second timeout for all operations
- **Persistent Messages**: Messages are marked as persistent for durability
- **Connection Management**: Proper connection and channel lifecycle management
- **Error Handling**: Comprehensive error handling with descriptive messages

### Subscriber Features
- **Non-exclusive Queues**: Creates durable, non-exclusive queues
- **Routing Key Bindings**: Supports multiple routing key bindings per queue
- **Callback-based Processing**: Uses callback functions for message processing
- **Explicit Acknowledgment**: Requires explicit ack after message processing
- **Goroutine-based Processing**: Each message is processed in its own goroutine
- **Error Recovery**: Automatic message rejection on processing errors

## Installation

```bash
go get github.com/cleanapp/golib/rabbitmq
```

## Usage

### Basic Usage

```go
package main

import (
    "log"
    "time"
    
    "github.com/cleanapp/golib/rabbitmq"
)

type Message struct {
    ID        int       `json:"id"`
    Content   string    `json:"content"`
    Timestamp time.Time `json:"timestamp"`
}

func main() {
    // Create publisher
    publisher, err := rabbitmq.NewPublisher(
        "amqp://guest:guest@localhost:5672/", // AMQP URL
        "my_exchange",                        // Exchange name
        "my.routing.key",                     // Default routing key
    )
    if err != nil {
        log.Fatal(err)
    }
    defer publisher.Close()

    // Publish message
    message := Message{
        ID:        1,
        Content:   "Hello World",
        Timestamp: time.Now(),
    }
    
    err = publisher.Publish(message)
    if err != nil {
        log.Fatal(err)
    }
}
```

### Publishing with Custom Routing Key

```go
// Publish with a different routing key
err = publisher.PublishWithRoutingKey("custom.key", message)
```

### Connection Management

```go
// Check if publisher is still connected
if publisher.IsConnected() {
    // Publisher is healthy
}

// Get exchange and routing key info
exchange := publisher.GetExchange()
routingKey := publisher.GetRoutingKey()
```

## Subscriber Usage

### Basic Subscriber Usage

```go
package main

import (
    "log"
    "time"
    
    "github.com/cleanapp/golib/rabbitmq"
)

type Message struct {
    ID        int       `json:"id"`
    Content   string    `json:"content"`
    Timestamp time.Time `json:"timestamp"`
}

func main() {
    // Create subscriber
    subscriber, err := rabbitmq.NewSubscriber(
        "amqp://guest:guest@localhost:5672/", // AMQP URL
        "my_exchange",                        // Exchange name
        "my_queue",                          // Queue name
    )
    if err != nil {
        log.Fatal(err)
    }
    defer subscriber.Close()

    // Define callbacks for different routing keys
    callbacks := map[string]rabbitmq.CallbackFunc{
        "user.created": handleUserCreated,
        "user.updated": handleUserUpdated,
        "user.deleted": handleUserDeleted,
    }

    // Start consuming messages
    err = subscriber.Start(callbacks)
    if err != nil {
        log.Fatal(err)
    }

    // Keep running
    select {}
}

func handleUserCreated(msg *rabbitmq.Message) error {
    var user Message
    err := msg.UnmarshalTo(&user)
    if err != nil {
        return err
    }
    
    log.Printf("User created: %+v", user)
    return nil
}

func handleUserUpdated(msg *rabbitmq.Message) error {
    var user Message
    err := msg.UnmarshalTo(&user)
    if err != nil {
        return err
    }
    
    log.Printf("User updated: %+v", user)
    return nil
}

func handleUserDeleted(msg *rabbitmq.Message) error {
    var user Message
    err := msg.UnmarshalTo(&user)
    if err != nil {
        return err
    }
    
    log.Printf("User deleted: %+v", user)
    return nil
}
```

### Subscriber Features

```go
// Check if subscriber is still connected
if subscriber.IsConnected() {
    // Subscriber is healthy
}

// Get exchange and queue info
exchange := subscriber.GetExchange()
queue := subscriber.GetQueue()
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
- Marshaled to JSON format
- Set with `Content-Type: application/json`
- Marked as persistent (`DeliveryMode: amqp.Persistent`)
- Timestamped with the current time

## Error Handling

The library provides detailed error messages for common failure scenarios:

- Connection failures
- Channel creation failures
- Exchange declaration failures
- Message publishing failures
- Context timeouts

## Dependencies

- `github.com/streadway/amqp` - AMQP 0.9.1 client library

## License

This library is part of the CleanApp project.
