package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"
)

// Message represents a received RabbitMQ message
type Message struct {
	Body        []byte
	RoutingKey  string
	Exchange    string
	ContentType string
	Timestamp   time.Time
	DeliveryTag uint64
}

// CallbackFunc represents a callback function for processing messages
type CallbackFunc func(msg *Message) error

// Subscriber represents a RabbitMQ subscriber instance
type Subscriber struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
	queue    string
}

// NewSubscriber creates a new RabbitMQ subscriber instance
func NewSubscriber(amqpURL, exchangeName, queueName string) (*Subscriber, error) {
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

	// Declare exchange with specified parameters (same as publisher)
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

	// Declare queue with non-exclusive, durable settings
	queue, err := channel.QueueDeclare(
		queueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		channel.Close()
		conn.Close()
		return nil, fmt.Errorf("context timeout while creating subscriber: %w", ctx.Err())
	default:
	}

	subscriber := &Subscriber{
		conn:     conn,
		channel:  channel,
		exchange: exchangeName,
		queue:    queue.Name,
	}

	return subscriber, nil
}

// Start begins consuming messages from the queue with the specified routing key callbacks
func (s *Subscriber) Start(routingKeyCallbacks map[string]CallbackFunc) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create bindings for each routing key
	for routingKey := range routingKeyCallbacks {
		err := s.channel.QueueBind(
			s.queue,    // queue name
			routingKey, // routing key
			s.exchange, // exchange
			false,      // no-wait
			nil,        // arguments
		)
		if err != nil {
			return fmt.Errorf("failed to bind queue %s to exchange %s with routing key %s: %w",
				s.queue, s.exchange, routingKey, err)
		}
	}

	// Start consuming messages
	msgs, err := s.channel.Consume(
		s.queue, // queue
		"",      // consumer
		false,   // auto-ack (set to false for manual ack)
		false,   // exclusive
		false,   // no-local
		false,   // no-wait
		nil,     // args
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return fmt.Errorf("context timeout while starting subscriber: %w", ctx.Err())
	default:
	}

	// Process messages in goroutines
	go func() {
		for delivery := range msgs {
			// Create message wrapper
			msg := &Message{
				Body:        delivery.Body,
				RoutingKey:  delivery.RoutingKey,
				Exchange:    delivery.Exchange,
				ContentType: delivery.ContentType,
				Timestamp:   delivery.Timestamp,
				DeliveryTag: delivery.DeliveryTag,
			}

			// Find callback for this routing key
			callback, exists := routingKeyCallbacks[delivery.RoutingKey]
			if !exists {
				log.Printf("No callback found for routing key: %s", delivery.RoutingKey)
				// Reject message if no callback found
				delivery.Nack(false, false)
				continue
			}

			// Process message in goroutine
			go func(delivery amqp.Delivery, msg *Message, callback CallbackFunc) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Panic in message callback for routing key %s: %v", msg.RoutingKey, r)
						// Reject message on panic
						delivery.Nack(false, false)
					}
				}()

				// Call the callback function
				err := callback(msg)
				if err != nil {
					log.Printf("Error processing message for routing key %s: %v", msg.RoutingKey, err)
					// Reject message on error
					delivery.Nack(false, false)
					return
				}

				// Acknowledge message after successful processing
				err = delivery.Ack(false)
				if err != nil {
					log.Printf("Failed to acknowledge message for routing key %s: %v", msg.RoutingKey, err)
				}
			}(delivery, msg, callback)
		}
	}()

	return nil
}

// UnmarshalTo unmarshals the message body into the provided interface
func (m *Message) UnmarshalTo(v interface{}) error {
	return json.Unmarshal(m.Body, v)
}

// Close closes the subscriber connection and channel
func (s *Subscriber) Close() error {
	var err error

	if s.channel != nil {
		if channelErr := s.channel.Close(); channelErr != nil {
			log.Printf("Failed to close channel: %v", channelErr)
			err = channelErr
		}
	}

	if s.conn != nil {
		if connErr := s.conn.Close(); connErr != nil {
			log.Printf("Failed to close connection: %v", connErr)
			if err == nil {
				err = connErr
			}
		}
	}

	return err
}

// IsConnected checks if the subscriber is still connected
func (s *Subscriber) IsConnected() bool {
	if s.conn == nil || s.channel == nil {
		return false
	}

	// Check if connection is still alive
	select {
	case <-s.conn.NotifyClose(make(chan *amqp.Error)):
		return false
	default:
		return true
	}
}

// GetExchange returns the exchange name
func (s *Subscriber) GetExchange() string {
	return s.exchange
}

// GetQueue returns the queue name
func (s *Subscriber) GetQueue() string {
	return s.queue
}
