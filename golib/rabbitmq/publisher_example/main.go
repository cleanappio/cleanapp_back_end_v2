package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cleanappio/cleanapp_back_end_v2/cleanapp/golib/rabbitmq"
)

// Example message structure
type ExampleMessage struct {
	ID        int       `json:"id"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	// RabbitMQ connection URL
	amqpURL := "amqp://guest:guest@localhost:5672/"

	// Create a new publisher
	publisher, err := rabbitmq.NewPublisher(amqpURL, "example_exchange", "example.routing.key")
	if err != nil {
		log.Fatalf("Failed to create publisher: %v", err)
	}
	defer publisher.Close()

	// Example message
	message := ExampleMessage{
		ID:        1,
		Message:   "Hello from RabbitMQ Publisher!",
		Timestamp: time.Now(),
	}

	// Publish the message
	err = publisher.Publish(message)
	if err != nil {
		log.Fatalf("Failed to publish message: %v", err)
	}

	fmt.Println("Message published successfully!")

	// Example of publishing with a custom routing key
	customMessage := ExampleMessage{
		ID:        2,
		Message:   "Custom routing key message",
		Timestamp: time.Now(),
	}

	err = publisher.PublishWithRoutingKey("custom.routing.key", customMessage)
	if err != nil {
		log.Fatalf("Failed to publish custom message: %v", err)
	}

	fmt.Println("Custom message published successfully!")

	// Check connection status
	if publisher.IsConnected() {
		fmt.Println("Publisher is still connected")
	} else {
		fmt.Println("Publisher connection is lost")
	}
}
