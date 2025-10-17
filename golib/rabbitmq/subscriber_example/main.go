package main

import (
	"fmt"
	"log"
	"time"

	"cleanapp/golib/rabbitmq"
)

// Example message structure (same as publisher example)
type ExampleMessage struct {
	ID        int       `json:"id"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	// RabbitMQ connection URL
	amqpURL := "amqp://guest:guest@localhost:5672/"

	// Create a new subscriber
	subscriber, err := rabbitmq.NewSubscriber(amqpURL, "example_exchange", "example_queue")
	if err != nil {
		log.Fatalf("Failed to create subscriber: %v", err)
	}
	defer subscriber.Close()

	// Define callback functions for different routing keys
	callbacks := map[string]rabbitmq.CallbackFunc{
		"example.routing.key": handleExampleMessage,
		"custom.routing.key":  handleCustomMessage,
		"error.routing.key":   handleErrorMessage,
	}

	// Start consuming messages
	err = subscriber.Start(callbacks)
	if err != nil {
		log.Fatalf("Failed to start subscriber: %v", err)
	}

	fmt.Println("Subscriber started successfully!")
	fmt.Printf("Listening on exchange: %s, queue: %s\n",
		subscriber.GetExchange(), subscriber.GetQueue())
	fmt.Println("Press Ctrl+C to stop...")

	// Keep the program running
	select {}
}

// handleExampleMessage processes messages with "example.routing.key" routing key
func handleExampleMessage(msg *rabbitmq.Message) error {
	var exampleMsg ExampleMessage
	err := msg.UnmarshalTo(&exampleMsg)
	if err != nil {
		return fmt.Errorf("failed to unmarshal example message: %w", err)
	}

	fmt.Printf("Received example message: ID=%d, Message=%s, Time=%s\n",
		exampleMsg.ID, exampleMsg.Message, exampleMsg.Timestamp.Format(time.RFC3339))

	return nil
}

// handleCustomMessage processes messages with "custom.routing.key" routing key
func handleCustomMessage(msg *rabbitmq.Message) error {
	var exampleMsg ExampleMessage
	err := msg.UnmarshalTo(&exampleMsg)
	if err != nil {
		return fmt.Errorf("failed to unmarshal custom message: %w", err)
	}

	fmt.Printf("Received custom message: ID=%d, Message=%s, Time=%s\n",
		exampleMsg.ID, exampleMsg.Message, exampleMsg.Timestamp.Format(time.RFC3339))

	return nil
}

// handleErrorMessage simulates an error scenario
func handleErrorMessage(msg *rabbitmq.Message) error {
	fmt.Printf("Received error message: %s\n", string(msg.Body))

	// Simulate an error - this will cause the message to be rejected
	return fmt.Errorf("simulated processing error")
}
