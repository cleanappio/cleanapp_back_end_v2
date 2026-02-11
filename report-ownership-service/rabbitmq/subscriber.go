package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
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

	defaultMaxRetries = 10
	envMaxRetries     = "RABBITMQ_MAX_RETRIES"

	defaultRetryExchangePrefix = "cleanapp-retry."
	envRetryExchangePrefix     = "RABBITMQ_RETRY_EXCHANGE_PREFIX"
	retryCountHeaderKey        = "x-cleanapp-retry-count"
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

func rabbitMQMaxRetries() int {
	if v := os.Getenv(envMaxRetries); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
		log.Printf("rabbitmq: invalid %s=%q, using default=%d", envMaxRetries, v, defaultMaxRetries)
	}
	return defaultMaxRetries
}

func rabbitMQRetryExchange(queue string) string {
	prefix := os.Getenv(envRetryExchangePrefix)
	if prefix == "" {
		prefix = defaultRetryExchangePrefix
	}
	return prefix + queue
}

func retryCountFromHeaders(headers amqp.Table) int {
	if headers == nil {
		return 0
	}
	v, ok := headers[retryCountHeaderKey]
	if !ok || v == nil {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	switch t := v.(type) {
	case int:
		if t < 0 {
			return 0
		}
		return t
	case int32:
		if t < 0 {
			return 0
		}
		return int(t)
	case int64:
		if t < 0 {
			return 0
		}
		if t > int64(maxInt) {
			return maxInt
		}
		return int(t)
	case uint32:
		if t > uint32(maxInt) {
			return maxInt
		}
		return int(t)
	case uint64:
		if t > uint64(maxInt) {
			return maxInt
		}
		return int(t)
	case string:
		if n, err := strconv.Atoi(t); err == nil && n >= 0 {
			return n
		}
		return 0
	default:
		return 0
	}
}

func withRetryCountHeader(headers amqp.Table, next int) amqp.Table {
	out := amqp.Table{}
	for k, v := range headers {
		out[k] = v
	}
	if next < 0 {
		next = 0
	}
	out[retryCountHeaderKey] = int32(next)
	return out
}

// Subscriber represents a RabbitMQ subscriber instance
type Subscriber struct {
	amqpURL  string
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
	queue    string
	opMu     sync.Mutex

	startOnce sync.Once
	closeOnce sync.Once
	done      chan struct{}
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
		amqpURL:  amqpURL,
		conn:     conn,
		channel:  channel,
		exchange: exchangeName,
		queue:    queue.Name,
		done:     make(chan struct{}),
	}

	return subscriber, nil
}

func (s *Subscriber) closeLocked() {
	if s.channel != nil {
		_ = s.channel.Close()
		s.channel = nil
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *Subscriber) reconnectLocked() error {
	s.closeLocked()

	conn, err := amqp.Dial(s.amqpURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	if err := ch.ExchangeDeclare(
		s.exchange,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	q, err := ch.QueueDeclare(
		s.queue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare queue: %w", err)
	}
	s.queue = q.Name

	s.conn = conn
	s.channel = ch
	return nil
}

type consumeSession struct {
	msgs      <-chan amqp.Delivery
	connClose <-chan *amqp.Error
	chClose   <-chan *amqp.Error
}

func (s *Subscriber) startConsumeSessionLocked(
	routingKeyCallbacks map[string]CallbackFunc,
	workers int,
) (*consumeSession, error) {
	if s.conn == nil || s.conn.IsClosed() || s.channel == nil {
		if err := s.reconnectLocked(); err != nil {
			return nil, err
		}
	}

	if err := s.channel.Qos(workers, 0, false); err != nil {
		s.closeLocked()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	// Create bindings for each routing key. Bindings are idempotent, so re-binding is safe.
	for routingKey := range routingKeyCallbacks {
		if err := s.channel.QueueBind(s.queue, routingKey, s.exchange, false, nil); err != nil {
			s.closeLocked()
			return nil, fmt.Errorf(
				"failed to bind queue %s to exchange %s with routing key %s: %w",
				s.queue, s.exchange, routingKey, err,
			)
		}
	}

	msgs, err := s.channel.Consume(
		s.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		s.closeLocked()
		return nil, fmt.Errorf("failed to register consumer: %w", err)
	}

	// Buffer=1 ensures the notify channels deliver a single close event without blocking.
	connClose := s.conn.NotifyClose(make(chan *amqp.Error, 1))
	chClose := s.channel.NotifyClose(make(chan *amqp.Error, 1))
	return &consumeSession{msgs: msgs, connClose: connClose, chClose: chClose}, nil
}

// Start begins consuming messages from the queue with the specified routing key callbacks
func (s *Subscriber) Start(routingKeyCallbacks map[string]CallbackFunc) error {
	var startErr error
	s.startOnce.Do(func() {
		workers := rabbitMQConcurrency()
		jobs := make(chan amqp.Delivery, workers)
		maxRetries := rabbitMQMaxRetries()
		retryExchange := rabbitMQRetryExchange(s.queue)

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
						var nackErr error
						s.opMu.Lock()
						nackErr = delivery.Nack(false, false) // permanent: no handler
						s.opMu.Unlock()
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
					var publishErr error
					if panicVal != nil {
						action = "nack"
						requeue = false // treat panics as permanent
						s.opMu.Lock()
						nackErr = delivery.Nack(false, requeue)
						s.opMu.Unlock()
					} else if callbackErr != nil {
						requeue = !isPermanent(callbackErr)
						if requeue {
							// Transient error: publish to per-queue retry exchange, then ack original.
							attempts := retryCountFromHeaders(delivery.Headers)
							if attempts >= maxRetries {
								action = "nack"
								requeue = false
								s.opMu.Lock()
								nackErr = delivery.Nack(false, requeue)
								s.opMu.Unlock()
							} else {
								action = "retry"
								next := attempts + 1
								pub := amqp.Publishing{
									Headers:      withRetryCountHeader(delivery.Headers, next),
									ContentType:  delivery.ContentType,
									Body:         delivery.Body,
									DeliveryMode: delivery.DeliveryMode,
									Timestamp:    delivery.Timestamp,
								}
								s.opMu.Lock()
								publishErr = s.channel.Publish(retryExchange, delivery.RoutingKey, false, false, pub)
								if publishErr == nil {
									ackErr = delivery.Ack(false)
								} else {
									// Fallback if retry exchange isn't set up: requeue original.
									action = "nack"
									requeue = true
									nackErr = delivery.Nack(false, requeue)
								}
								s.opMu.Unlock()
							}
						} else {
							action = "nack"
							s.opMu.Lock()
							nackErr = delivery.Nack(false, requeue)
							s.opMu.Unlock()
						}
					} else {
						s.opMu.Lock()
						ackErr = delivery.Ack(false)
						s.opMu.Unlock()
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
							"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s requeue=%t err=%v retry_exchange=%s publish_err=%v ack_err=%v nack_err=%v",
							workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, requeue, callbackErr,
							retryExchange, publishErr, ackErr, nackErr,
						)
						continue
					}

					log.Printf(
						"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s retry_exchange=%s publish_err=%v ack_err=%v",
						workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, retryExchange, publishErr, ackErr,
					)
				}
			}()
		}

		// Initial session: fail fast if we can't consume at startup.
		s.opMu.Lock()
		initialSession, err := s.startConsumeSessionLocked(routingKeyCallbacks, workers)
		s.opMu.Unlock()
		if err != nil {
			close(jobs)
			startErr = err
			return
		}

		// Consume loop: if the broker restarts, the consumer channel closes; we reconnect and resume.
		go func() {
			backoff := 1 * time.Second
			session := initialSession
			for {
				select {
				case <-s.done:
					close(jobs)
					return
				default:
				}

				if session == nil {
					s.opMu.Lock()
					next, err := s.startConsumeSessionLocked(routingKeyCallbacks, workers)
					s.opMu.Unlock()
					if err != nil {
						log.Printf("rabbitmq consume_start_failed queue=%s exchange=%s err=%v", s.queue, s.exchange, err)
						time.Sleep(backoff)
						if backoff < 30*time.Second {
							backoff *= 2
						}
						continue
					}
					session = next
					backoff = 1 * time.Second
				}

				for {
					select {
					case <-s.done:
						close(jobs)
						return
					case cerr := <-session.connClose:
						if cerr != nil {
							log.Printf("rabbitmq conn_closed queue=%s err=%v", s.queue, cerr)
						} else {
							log.Printf("rabbitmq conn_closed queue=%s", s.queue)
						}
						goto reconnect
					case cerr := <-session.chClose:
						if cerr != nil {
							log.Printf("rabbitmq channel_closed queue=%s err=%v", s.queue, cerr)
						} else {
							log.Printf("rabbitmq channel_closed queue=%s", s.queue)
						}
						goto reconnect
					case delivery, ok := <-session.msgs:
						if !ok {
							log.Printf("rabbitmq deliveries_closed queue=%s", s.queue)
							goto reconnect
						}
						select {
						case jobs <- delivery:
						case <-s.done:
							close(jobs)
							return
						}
					}
				}

			reconnect:
				s.opMu.Lock()
				s.closeLocked()
				s.opMu.Unlock()
				session = nil
				time.Sleep(backoff)
				if backoff < 30*time.Second {
					backoff *= 2
				}
			}
		}()
	})

	return startErr
}

// UnmarshalTo unmarshals the message body into the provided interface
func (m *Message) UnmarshalTo(v interface{}) error {
	return json.Unmarshal(m.Body, v)
}

// Close closes the subscriber connection and channel
func (s *Subscriber) Close() error {
	var err error

	s.closeOnce.Do(func() { close(s.done) })

	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.channel != nil {
		if channelErr := s.channel.Close(); channelErr != nil {
			log.Printf("Failed to close channel: %v", channelErr)
			err = channelErr
		}
		s.channel = nil
	}

	if s.conn != nil {
		if connErr := s.conn.Close(); connErr != nil {
			log.Printf("Failed to close connection: %v", connErr)
			if err == nil {
				err = connErr
			}
		}
		s.conn = nil
	}

	return err
}

// IsConnected checks if the subscriber is still connected
func (s *Subscriber) IsConnected() bool {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.conn == nil || s.channel == nil {
		return false
	}

	return !s.conn.IsClosed()
}

// GetExchange returns the exchange name
func (s *Subscriber) GetExchange() string {
	return s.exchange
}

// GetQueue returns the queue name
func (s *Subscriber) GetQueue() string {
	return s.queue
}
