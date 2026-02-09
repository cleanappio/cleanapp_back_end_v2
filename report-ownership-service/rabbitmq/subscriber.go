package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
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

// PermanentError marks a message processing failure as non-retriable.
// The subscriber will Nack with requeue=false (dead-letter if configured).
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	if e == nil || e.Err == nil {
		return "permanent error"
	}
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Permanent wraps err as a PermanentError (non-retriable).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

func isPermanent(err error) bool {
	var perr *PermanentError
	return errors.As(err, &perr)
}

const (
	defaultConcurrency = 20
	envConcurrency     = "RABBITMQ_CONCURRENCY"
)

func rabbitMQConcurrency() int {
	if v := os.Getenv(envConcurrency); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("rabbitmq: invalid %s=%q, using default=%d", envConcurrency, v, defaultConcurrency)
	}
	return defaultConcurrency
}

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

	workers := rabbitMQConcurrency()
	if err := s.channel.Qos(
		workers, // prefetch count
		0,       // prefetch size
		false,   // global
	); err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

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

	jobs := make(chan amqp.Delivery, workers)

	// Worker pool: bounded concurrency, ack/nack is done *after* processing completes.
	for i := 0; i < workers; i++ {
		workerID := i + 1
		go func() {
			for delivery := range jobs {
				startedAt := time.Now()
				log.Printf(
					"rabbitmq worker_start worker_id=%d exchange=%s queue=%s routing_key=%s delivery_tag=%d redelivered=%t",
					workerID, delivery.Exchange, s.queue, delivery.RoutingKey, delivery.DeliveryTag, delivery.Redelivered,
				)

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
					nackErr := delivery.Nack(false, false) // permanent: no handler
					log.Printf(
						"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=nack requeue=false err=%q nack_err=%v",
						workerID, delivery.RoutingKey, delivery.DeliveryTag, time.Since(startedAt).Milliseconds(),
						"no callback for routing key", nackErr,
					)
					continue
				}

				var callbackErr error
				requeue := false
				panicVal := any(nil)

				func() {
					defer func() {
						if r := recover(); r != nil {
							panicVal = r
						}
					}()
					callbackErr = callback(msg)
				}()

				action := "ack"
				var ackErr error
				var nackErr error
				if panicVal != nil {
					action = "nack"
					requeue = false // treat panics as permanent
					nackErr = delivery.Nack(false, requeue)
				} else if callbackErr != nil {
					action = "nack"
					requeue = !isPermanent(callbackErr)
					nackErr = delivery.Nack(false, requeue)
				} else {
					ackErr = delivery.Ack(false)
				}

				durationMs := time.Since(startedAt).Milliseconds()
				if panicVal != nil {
					log.Printf(
						"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s requeue=%t panic=%v nack_err=%v",
						workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, requeue, panicVal, nackErr,
					)
					continue
				}

				if callbackErr != nil {
					log.Printf(
						"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s requeue=%t err=%v nack_err=%v",
						workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, requeue, callbackErr, nackErr,
					)
					continue
				}

				log.Printf(
					"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s ack_err=%v",
					workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, ackErr,
				)
			}
		}()
	}

	// Dispatcher: read deliveries and hand them to workers (no per-message goroutine).
	go func() {
		for delivery := range msgs {
			jobs <- delivery
		}
		close(jobs)
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
