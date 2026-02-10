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
	"sync/atomic"
	"time"

	"report-analyze-pipeline/metrics"

	"github.com/streadway/amqp"
)

// Message represents a received RabbitMQ message.
type Message struct {
	Body        []byte
	RoutingKey  string
	Exchange    string
	ContentType string
	Timestamp   time.Time
	DeliveryTag uint64
}

// CallbackFunc processes a message. Return:
// - nil on success (will Ack)
// - Permanent(err) for permanent failure (will Nack requeue=false; DLQ if configured)
// - any other error for transient failure (will retry/requeue)
type CallbackFunc func(msg *Message) error

// PermanentError marks a message processing failure as non-retriable.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string {
	if e == nil || e.Err == nil {
		return "permanent error"
	}
	return e.Err.Error()
}
func (e *PermanentError) Unwrap() error { return e.Err }

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
		if int64(t) > int64(maxInt) {
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

// Subscriber is a RabbitMQ subscriber instance.
type Subscriber struct {
	amqpURL  string
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
	queue    string
	prefetch int

	// opMu serializes amqp operations on s.channel since amqp.Channel is not safe for concurrent use.
	opMu sync.Mutex

	startOnce sync.Once
	done      chan struct{}

	// Observability signals (best-effort).
	connected      atomic.Bool
	lastConnectNs  atomic.Int64
	lastDeliveryNs atomic.Int64
	lastError      atomic.Value // string
}

// NewSubscriber creates a new RabbitMQ subscriber instance.
func NewSubscriber(amqpURL, exchangeName, queueName string, prefetchCount int) (*Subscriber, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s := &Subscriber{
		amqpURL:  amqpURL,
		exchange: exchangeName,
		queue:    queueName,
		prefetch: prefetchCount,
		done:     make(chan struct{}),
	}

	// Establish initial connection so callers fail fast if RabbitMQ is unreachable.
	s.opMu.Lock()
	err := s.reconnectLocked(ctx)
	s.opMu.Unlock()
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Subscriber) setLastError(err error) {
	if err == nil {
		s.lastError.Store("")
		return
	}
	s.lastError.Store(err.Error())
}

// reconnectLocked tears down any existing channel/connection and recreates them.
// Caller must hold s.opMu.
func (s *Subscriber) reconnectLocked(ctx context.Context) error {
	// Close existing resources (ignore errors).
	if s.channel != nil {
		_ = s.channel.Close()
		s.channel = nil
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}

	conn, err := amqp.Dial(s.amqpURL)
	if err != nil {
		s.connected.Store(false)
		metrics.RabbitMQConnected.Set(0)
		s.setLastError(err)
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		s.connected.Store(false)
		metrics.RabbitMQConnected.Set(0)
		s.setLastError(err)
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
		s.connected.Store(false)
		metrics.RabbitMQConnected.Set(0)
		s.setLastError(err)
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
		s.connected.Store(false)
		metrics.RabbitMQConnected.Set(0)
		s.setLastError(err)
		return fmt.Errorf("failed to declare queue: %w", err)
	}
	s.queue = q.Name

	select {
	case <-ctx.Done():
		_ = ch.Close()
		_ = conn.Close()
		s.connected.Store(false)
		metrics.RabbitMQConnected.Set(0)
		return fmt.Errorf("context timeout while connecting subscriber: %w", ctx.Err())
	default:
	}

	s.conn = conn
	s.channel = ch
	s.connected.Store(true)
	metrics.RabbitMQConnected.Set(1)

	now := time.Now().UnixNano()
	s.lastConnectNs.Store(now)
	metrics.RabbitMQLastConnectSeconds.Set(float64(time.Unix(0, now).Unix()))

	s.setLastError(nil)
	return nil
}

// Start begins consuming messages and dispatching them to the routing key callbacks.
func (s *Subscriber) Start(routingKeyCallbacks map[string]CallbackFunc) error {
	var startErr error
	s.startOnce.Do(func() {
		workers := rabbitMQConcurrency()
		if s.prefetch > 0 && workers > s.prefetch {
			workers = s.prefetch
		}

		jobs := make(chan amqp.Delivery, workers)
		maxRetries := rabbitMQMaxRetries()

		// Worker pool: bounded concurrency, ack/nack is done *after* processing completes.
		for i := 0; i < workers; i++ {
			workerID := i + 1
			go func() {
				for delivery := range jobs {
					func() {
						startedAt := time.Now()
						s.lastDeliveryNs.Store(startedAt.UnixNano())
						metrics.RabbitMQLastDeliverySeconds.Set(float64(startedAt.Unix()))

						metrics.WorkerInFlight.Inc()
						defer metrics.WorkerInFlight.Dec()

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

						callback, exists := routingKeyCallbacks[delivery.RoutingKey]
						if !exists {
							s.opMu.Lock()
							nackErr := delivery.Nack(false, false)
							s.opMu.Unlock()
							if nackErr != nil {
								metrics.NackErrorTotal.Inc()
							}
							metrics.ProcessedTotal.WithLabelValues("permanent_error").Inc()
							metrics.ProcessingDurationSeconds.WithLabelValues("permanent_error").Observe(time.Since(startedAt).Seconds())
							log.Printf(
								"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=nack requeue=false err=%q nack_err=%v",
								workerID, delivery.RoutingKey, delivery.DeliveryTag, time.Since(startedAt).Milliseconds(),
								"no callback for routing key", nackErr,
							)
							return
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
						retryExchange := rabbitMQRetryExchange(s.queue)

						if panicVal != nil {
						action = "nack"
						requeue = false // treat panics as permanent
						s.opMu.Lock()
						nackErr = delivery.Nack(false, requeue)
						s.opMu.Unlock()
						if nackErr != nil {
							metrics.NackErrorTotal.Inc()
						}
						} else if callbackErr != nil {
						requeue = !isPermanent(callbackErr)
						if requeue {
							attempts := retryCountFromHeaders(delivery.Headers)
							if attempts >= maxRetries {
								action = "nack"
								requeue = false
								s.opMu.Lock()
								nackErr = delivery.Nack(false, requeue)
								s.opMu.Unlock()
								if nackErr != nil {
									metrics.NackErrorTotal.Inc()
								}
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
								// Publish to retry exchange then Ack original to avoid tight retry loops.
								publishErr = s.channel.Publish(retryExchange, delivery.RoutingKey, false, false, pub)
								if publishErr == nil {
									ackErr = delivery.Ack(false)
									if ackErr != nil {
										metrics.AckErrorTotal.Inc()
									}
								} else {
									metrics.RetryPublishErrorTotal.Inc()
									action = "nack"
									requeue = true
									nackErr = delivery.Nack(false, requeue)
									if nackErr != nil {
										metrics.NackErrorTotal.Inc()
									}
								}
								s.opMu.Unlock()
							}
						} else {
							action = "nack"
							s.opMu.Lock()
							nackErr = delivery.Nack(false, requeue)
							s.opMu.Unlock()
							if nackErr != nil {
								metrics.NackErrorTotal.Inc()
							}
						}
						} else {
						s.opMu.Lock()
						ackErr = delivery.Ack(false)
						s.opMu.Unlock()
						if ackErr != nil {
							metrics.AckErrorTotal.Inc()
						}
					}

						durationMs := time.Since(startedAt).Milliseconds()
						if panicVal != nil {
						metrics.ProcessedTotal.WithLabelValues("panic").Inc()
						metrics.ProcessingDurationSeconds.WithLabelValues("panic").Observe(time.Since(startedAt).Seconds())
						log.Printf(
							"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s requeue=%t panic=%v nack_err=%v",
							workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, requeue, panicVal, nackErr,
						)
							return
						}

						if callbackErr != nil {
						if isPermanent(callbackErr) {
							metrics.ProcessedTotal.WithLabelValues("permanent_error").Inc()
							metrics.ProcessingDurationSeconds.WithLabelValues("permanent_error").Observe(time.Since(startedAt).Seconds())
						} else {
							metrics.ProcessedTotal.WithLabelValues("transient_error").Inc()
							metrics.ProcessingDurationSeconds.WithLabelValues("transient_error").Observe(time.Since(startedAt).Seconds())
						}
						log.Printf(
							"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s requeue=%t err=%v retry_exchange=%s publish_err=%v ack_err=%v nack_err=%v",
							workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, requeue, callbackErr,
							retryExchange, publishErr, ackErr, nackErr,
						)
							return
						}

						metrics.ProcessedTotal.WithLabelValues("success").Inc()
						metrics.ProcessingDurationSeconds.WithLabelValues("success").Observe(time.Since(startedAt).Seconds())
						log.Printf(
							"rabbitmq worker_finish worker_id=%d routing_key=%s delivery_tag=%d duration_ms=%d action=%s retry_exchange=%s publish_err=%v ack_err=%v",
							workerID, delivery.RoutingKey, delivery.DeliveryTag, durationMs, action, retryExchange, publishErr, ackErr,
						)
					}()
				}
			}()
		}

		// Consume loop: if the broker restarts, the consumer channel closes; we reconnect and resume.
		go func() {
			backoff := 1 * time.Second
			for {
				select {
				case <-s.done:
					close(jobs)
					return
				default:
				}

				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				s.opMu.Lock()
				if s.conn == nil || s.conn.IsClosed() || s.channel == nil {
					if err := s.reconnectLocked(ctx); err != nil {
						s.opMu.Unlock()
						cancel()
						log.Printf("rabbitmq reconnect failed queue=%s exchange=%s err=%v", s.queue, s.exchange, err)
						time.Sleep(backoff)
						if backoff < 30*time.Second {
							backoff *= 2
						}
						continue
					}
				}

				// Re-apply QoS and bindings on each (re)connect.
				workersPrefetch := workers
				if err := s.channel.Qos(workersPrefetch, 0, false); err != nil {
					s.connected.Store(false)
					metrics.RabbitMQConnected.Set(0)
					s.setLastError(err)
					s.opMu.Unlock()
					cancel()
					log.Printf("rabbitmq qos failed queue=%s err=%v", s.queue, err)
					time.Sleep(backoff)
					if backoff < 30*time.Second {
						backoff *= 2
					}
					continue
				}

				for routingKey := range routingKeyCallbacks {
					if err := s.channel.QueueBind(s.queue, routingKey, s.exchange, false, nil); err != nil {
						s.connected.Store(false)
						metrics.RabbitMQConnected.Set(0)
						s.setLastError(err)
						s.opMu.Unlock()
						cancel()
						log.Printf("rabbitmq bind failed queue=%s exchange=%s routing_key=%s err=%v", s.queue, s.exchange, routingKey, err)
						time.Sleep(backoff)
						if backoff < 30*time.Second {
							backoff *= 2
						}
						continue
					}
				}

				msgs, err := s.channel.Consume(s.queue, "", false, false, false, false, nil)
				s.opMu.Unlock()
				cancel()
				if err != nil {
					s.connected.Store(false)
					metrics.RabbitMQConnected.Set(0)
					s.setLastError(err)
					log.Printf("rabbitmq consume failed queue=%s err=%v", s.queue, err)
					time.Sleep(backoff)
					if backoff < 30*time.Second {
						backoff *= 2
					}
					continue
				}

				log.Printf("rabbitmq consuming exchange=%s queue=%s workers=%d prefetch=%d", s.exchange, s.queue, workers, workers)
				backoff = 1 * time.Second

				for {
					select {
					case <-s.done:
						close(jobs)
						return
					case delivery, ok := <-msgs:
						if !ok {
							s.connected.Store(false)
							metrics.RabbitMQConnected.Set(0)
							log.Printf("rabbitmq delivery channel closed queue=%s exchange=%s; reconnecting", s.queue, s.exchange)
							time.Sleep(backoff)
							if backoff < 30*time.Second {
								backoff *= 2
							}
							goto Reconnect
						}
						jobs <- delivery
					}
				}

			Reconnect:
				continue
			}
		}()
	})
	return startErr
}

// UnmarshalTo unmarshals the message body into the provided interface.
func (m *Message) UnmarshalTo(v any) error {
	return json.Unmarshal(m.Body, v)
}

// Close closes the subscriber connection and channel.
func (s *Subscriber) Close() error {
	select {
	case <-s.done:
		// already closed
	default:
		close(s.done)
	}

	var err error
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

	s.connected.Store(false)
	metrics.RabbitMQConnected.Set(0)
	return err
}

// IsConnected indicates if the subscriber is currently connected (best-effort).
func (s *Subscriber) IsConnected() bool {
	if s.conn == nil || s.channel == nil {
		return false
	}
	if s.conn.IsClosed() {
		return false
	}
	return s.connected.Load()
}

// LastConnectAt returns the last time we successfully (re)connected.
func (s *Subscriber) LastConnectAt() time.Time {
	ns := s.lastConnectNs.Load()
	if ns <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// LastDeliveryAt returns the last time we observed a delivery.
func (s *Subscriber) LastDeliveryAt() time.Time {
	ns := s.lastDeliveryNs.Load()
	if ns <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// LastError returns the last connection/consumption error string (best-effort).
func (s *Subscriber) LastError() string {
	v := s.lastError.Load()
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GetExchange returns the exchange name.
func (s *Subscriber) GetExchange() string { return s.exchange }

// GetQueue returns the queue name.
func (s *Subscriber) GetQueue() string { return s.queue }
