package rabbitmq

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/streadway/amqp"
)

// These tests use only uniquely named fixture queues/exchanges. Run them against
// a disposable broker, never the production vhost:
// CLEANAPP_TEST_AMQP_URL=amqp://... go test ./rabbitmq -run Integration
func integrationSubscriber(t *testing.T) (*Subscriber, *amqp.Channel, string) {
	t.Helper()
	url := os.Getenv("CLEANAPP_TEST_AMQP_URL")
	if url == "" {
		t.Skip("CLEANAPP_TEST_AMQP_URL is unset; requires a disposable RabbitMQ broker")
	}
	t.Setenv(envConcurrency, "1")
	name := fmt.Sprintf("cleanapp-ci-%d", time.Now().UnixNano())
	s, err := NewSubscriber(url, name+"-exchange", name+"-queue", 1)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		_, _ = ch.QueueDelete(s.queue, false, false, false)
		_ = ch.ExchangeDelete(s.exchange, false, false)
		_ = conn.Close()
	})
	return s, ch, "fixture"
}

func integrationPublish(t *testing.T, s *Subscriber, ch *amqp.Channel, key, body string) {
	t.Helper()
	if err := ch.Publish(s.exchange, key, false, false, amqp.Publishing{
		ContentType: "text/plain", DeliveryMode: amqp.Persistent, Body: []byte(body),
	}); err != nil {
		t.Fatal(err)
	}
}

func integrationAwait(t *testing.T, pred func() bool, reason string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", reason)
}

func integrationCloseOnlyChannel(t *testing.T, s *Subscriber) *amqp.Connection {
	t.Helper()
	s.opMu.Lock()
	defer s.opMu.Unlock()
	oldConn := s.conn
	// A passive declaration of an absent exchange makes RabbitMQ close this
	// channel with 404 while the AMQP connection remains open (the incident).
	err := s.channel.ExchangeDeclarePassive("absent-ci-exchange", "direct", true, false, false, false, nil)
	if err == nil {
		t.Fatal("expected a broker channel exception")
	}
	if oldConn.IsClosed() {
		t.Fatal("test must close the channel without closing its connection")
	}
	return oldConn
}

func TestIntegrationReconnectAfterChannelOnlyClosure(t *testing.T) {
	for _, beforeStart := range []bool{false, true} {
		t.Run(fmt.Sprintf("before_start=%t", beforeStart), func(t *testing.T) {
			s, ch, key := integrationSubscriber(t)
			var processed atomic.Int32
			if beforeStart {
				integrationCloseOnlyChannel(t, s)
			}
			if err := s.Start(map[string]CallbackFunc{key: func(*Message) error {
				processed.Add(1)
				return nil
			}}); err != nil {
				t.Fatal(err)
			}
			integrationAwait(t, func() bool {
				q, err := ch.QueueInspect(s.queue)
				return err == nil && q.Consumers == 1
			}, "one live consumer")
			if !beforeStart {
				integrationCloseOnlyChannel(t, s)
			}
			integrationPublish(t, s, ch, key, "recovery")
			integrationAwait(t, func() bool { return processed.Load() == 1 }, "queued fixture after channel recovery")
			integrationAwait(t, func() bool {
				q, err := ch.QueueInspect(s.queue)
				return err == nil && q.Messages == 0 && q.Consumers == 1 && s.IsConnected()
			}, "recovered consumer and empty queue")
		})
	}
}

func TestIntegrationMissingRetryExchangePreservesDeliveryAndRecovers(t *testing.T) {
	s, ch, key := integrationSubscriber(t)
	var processed atomic.Int32
	if err := s.Start(map[string]CallbackFunc{key: func(*Message) error {
		if processed.Add(1) == 1 {
			return errors.New("fixture provider timeout")
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	integrationAwait(t, func() bool {
		q, err := ch.QueueInspect(s.queue)
		return err == nil && q.Consumers == 1
	}, "consumer")
	integrationPublish(t, s, ch, key, "retry-recovery")
	integrationAwait(t, func() bool { return processed.Load() >= 2 }, "preserved delivery after missing retry exchange")
	integrationAwait(t, func() bool {
		q, err := ch.QueueInspect(s.queue)
		return err == nil && q.Messages == 0 && q.Consumers == 1
	}, "drained recovered queue")
	if got := processed.Load(); got != 2 {
		t.Fatalf("processed=%d, expected original attempt and one redelivery", got)
	}
}

func TestIntegrationOldDeliveryCannotPublishRetryThroughNewChannel(t *testing.T) {
	s, ch, key := integrationSubscriber(t)
	retryExchange := rabbitMQRetryExchange(s.queue)
	retryQueue := s.queue + "-fixture-retry"
	if err := ch.ExchangeDeclare(retryExchange, "direct", true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ch.QueueDeclare(retryQueue, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(retryQueue, key, retryExchange, false, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ch.QueueDelete(retryQueue, false, false, false)
		_ = ch.ExchangeDelete(retryExchange, false, false)
	})
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var processed atomic.Int32
	if err := s.Start(map[string]CallbackFunc{key: func(*Message) error {
		if processed.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			return errors.New("fixture timeout on old delivery")
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	integrationAwait(t, func() bool {
		q, err := ch.QueueInspect(s.queue)
		return err == nil && q.Consumers == 1
	}, "consumer")
	integrationPublish(t, s, ch, key, "in-flight")
	select {
	case <-firstStarted:
	case <-time.After(12 * time.Second):
		t.Fatal("first fixture did not start")
	}
	oldConn := integrationCloseOnlyChannel(t, s)
	integrationAwait(t, func() bool {
		s.opMu.Lock()
		defer s.opMu.Unlock()
		return s.conn != oldConn && s.connected.Load()
	}, "new AMQP connection")
	integrationPublish(t, s, ch, key, "next-delivery")
	close(releaseFirst)
	integrationAwait(t, func() bool { return processed.Load() >= 3 }, "redelivery plus next fixture")
	q, err := ch.QueueInspect(retryQueue)
	if err != nil {
		t.Fatal(err)
	}
	if q.Messages != 0 {
		t.Fatalf("old delivery created %d additional retries through new channel", q.Messages)
	}
}
