package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"
)

// Publisher represents a RabbitMQ publisher instance
type Publisher struct {
	conn       *amqp.Connection
	channel    *amqp.Channel
	exchange   string
	routingKey string
}

// NewPublisher creates a new RabbitMQ publisher instance
func NewPublisher(amqpURL, exchangeName, routingKey string) (*Publisher, error) {
	// Create connection with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Connect to RabbitMQ
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create channel
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Declare exchange with specified parameters
	err = channel.ExchangeDeclare(
		exchangeName, // name
		"direct",     // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		channel.Close()
		conn.Close()
		return nil, fmt.Errorf("context timeout while creating publisher: %w", ctx.Err())
	default:
	}

	publisher := &Publisher{
		conn:       conn,
		channel:    channel,
		exchange:   exchangeName,
		routingKey: routingKey,
	}

	return publisher, nil
}

// Publish sends a JSON message to the exchange with the configured routing key
func (p *Publisher) Publish(message interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Marshal message to JSON
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message to JSON: %w", err)
	}

	// Create publishing message
	publishing := amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent, // Make message persistent
		Timestamp:    time.Now(),
	}

	// Publish message
	err = p.channel.Publish(
		p.exchange,   // exchange
		p.routingKey, // routing key
		false,        // mandatory
		false,        // immediate
		publishing,   // message
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return fmt.Errorf("context timeout while publishing message: %w", ctx.Err())
	default:
	}

	return nil
}

// PublishWithRoutingKey sends a JSON message to the exchange with a custom routing key
func (p *Publisher) PublishWithRoutingKey(routingKey string, message interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Marshal message to JSON
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message to JSON: %w", err)
	}

	// Create publishing message
	publishing := amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent, // Make message persistent
		Timestamp:    time.Now(),
	}

	// Publish message
	err = p.channel.Publish(
		p.exchange, // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		publishing, // message
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return fmt.Errorf("context timeout while publishing message: %w", ctx.Err())
	default:
	}

	return nil
}

// Close closes the publisher connection and channel
func (p *Publisher) Close() error {
	var err error

	if p.channel != nil {
		if channelErr := p.channel.Close(); channelErr != nil {
			log.Printf("Failed to close channel: %v", channelErr)
			err = channelErr
		}
	}

	if p.conn != nil {
		if connErr := p.conn.Close(); connErr != nil {
			log.Printf("Failed to close connection: %v", connErr)
			if err == nil {
				err = connErr
			}
		}
	}

	return err
}

// IsConnected checks if the publisher is still connected
func (p *Publisher) IsConnected() bool {
	if p.conn == nil || p.channel == nil {
		return false
	}

	// Check if connection is still alive
	select {
	case <-p.conn.NotifyClose(make(chan *amqp.Error)):
		return false
	default:
		return true
	}
}

// GetExchange returns the exchange name
func (p *Publisher) GetExchange() string {
	return p.exchange
}

// GetRoutingKey returns the default routing key
func (p *Publisher) GetRoutingKey() string {
	return p.routingKey
}
