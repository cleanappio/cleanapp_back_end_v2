package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/streadway/amqp"
)

// PublishConfirmedWithID is the outcome publisher's durable broker boundary.
// A stable per-report message ID survives a retry after an uncertain ACK/DB
// marker window. Recipient delivery is still proved only by transport records.
// Legacy publication keeps its existing channel and behavior.
func (p *Publisher) PublishConfirmedWithID(message any, eventID string) error {
	if eventID == "" {
		return fmt.Errorf("confirmed publication requires an event identity")
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal confirmed analysed event: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil || p.conn.IsClosed() {
		p.closeLocked()
		if err := p.connectLocked(ctx); err != nil {
			return err
		}
	}
	ch, err := p.conn.Channel()
	if err != nil {
		return fmt.Errorf("open confirmed analysis channel: %w", err)
	}
	return publishConfirmed(ctx, ch, p.exchange, p.routingKey, amqp.Publishing{
		ContentType: "application/json", Body: body, DeliveryMode: amqp.Persistent,
		MessageId: eventID, Timestamp: time.Now(),
	})
}

type confirmedPublishChannel interface {
	Confirm(bool) error
	NotifyPublish(chan amqp.Confirmation) chan amqp.Confirmation
	NotifyReturn(chan amqp.Return) chan amqp.Return
	NotifyClose(chan *amqp.Error) chan *amqp.Error
	Publish(string, string, bool, bool, amqp.Publishing) error
	Close() error
}

func publishConfirmed(ctx context.Context, ch confirmedPublishChannel, exchange, routingKey string, message amqp.Publishing) error {
	defer ch.Close()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("analysed-event publication cancelled: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		return fmt.Errorf("enable analysed-event confirmations: %w", err)
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))
	closed := ch.NotifyClose(make(chan *amqp.Error, 1))
	if err := ch.Publish(exchange, routingKey, true, false, message); err != nil {
		return fmt.Errorf("publish confirmed analysed event: %w", err)
	}
	select {
	case _, ok := <-returns:
		if !ok {
			return fmt.Errorf("analysed-event return channel closed before confirmation")
		}
		return fmt.Errorf("analysed event was returned as unroutable")
	case confirm, ok := <-confirms:
		if !ok || !confirm.Ack {
			return fmt.Errorf("analysed event was not acknowledged by the broker")
		}
		// RabbitMQ sends basic.return before basic.ack for an unroutable
		// mandatory message. Both notifications may already be buffered here.
		select {
		case <-returns:
			return fmt.Errorf("analysed event was returned as unroutable")
		default:
		}
		return nil
	case <-closed:
		return fmt.Errorf("analysis channel closed before broker confirmation")
	case <-ctx.Done():
		return fmt.Errorf("analysed-event confirmation timed out: %w", ctx.Err())
	}
}
