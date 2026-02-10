package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/streadway/amqp"
)

// Publisher represents a RabbitMQ publisher instance
type Publisher struct {
	mu         sync.Mutex
	amqpURL    string
	conn       *amqp.Connection
	channel    *amqp.Channel
	exchange   string
	routingKey string
}

// NewPublisher creates a new RabbitMQ publisher instance
func NewPublisher(amqpURL, exchangeName, routingKey string) (*Publisher, error) {
	// Create connection with timeout context (best-effort; streadway doesn't accept ctx)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := &Publisher{
		amqpURL:    amqpURL,
		exchange:   exchangeName,
		routingKey: routingKey,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.connectLocked(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// Publish sends a JSON message to the exchange with the configured routing key
func (p *Publisher) Publish(message interface{}) error {
	return p.PublishWithRoutingKey(p.routingKey, message)
}

// PublishWithRoutingKey sends a JSON message to the exchange with a custom routing key
func (p *Publisher) PublishWithRoutingKey(routingKey string, message interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message to JSON: %w", err)
	}

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	}

	return p.publish(ctx, routingKey, publishing)
}

// Close closes the publisher connection and channel
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

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

func (p *Publisher) connectLocked(ctx context.Context) error {
	conn, err := amqp.Dial(p.amqpURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	if err := ch.ExchangeDeclare(
		p.exchange,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	select {
	case <-ctx.Done():
		ch.Close()
		conn.Close()
		return fmt.Errorf("context timeout while creating publisher: %w", ctx.Err())
	default:
	}

	p.conn = conn
	p.channel = ch
	return nil
}

func (p *Publisher) closeLocked() {
	if p.channel != nil {
		_ = p.channel.Close()
		p.channel = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}

func isConnClosedErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, amqp.ErrClosed) {
		return true
	}
	if strings.Contains(err.Error(), "channel/connection is not open") {
		return true
	}
	return false
}

func (p *Publisher) publish(ctx context.Context, routingKey string, publishing amqp.Publishing) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn == nil || p.conn.IsClosed() || p.channel == nil {
		p.closeLocked()
		if err := p.connectLocked(ctx); err != nil {
			return err
		}
	}

	err := p.channel.Publish(p.exchange, routingKey, false, false, publishing)
	if err != nil && isConnClosedErr(err) {
		p.closeLocked()
		if connErr := p.connectLocked(ctx); connErr != nil {
			return fmt.Errorf("failed to publish message: %w (reconnect failed: %v)", err, connErr)
		}
		err = p.channel.Publish(p.exchange, routingKey, false, false, publishing)
	}
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("context timeout while publishing message: %w", ctx.Err())
	default:
	}
	return nil
}

// IsConnected indicates whether the publisher currently has an open connection/channel.
func (p *Publisher) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn != nil && !p.conn.IsClosed() && p.channel != nil
}
