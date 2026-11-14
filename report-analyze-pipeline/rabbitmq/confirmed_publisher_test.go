package rabbitmq

import (
	"context"
	"errors"
	"testing"

	"github.com/streadway/amqp"
)

type confirmedChannelFixture struct {
	mode      string
	confirms  chan amqp.Confirmation
	returns   chan amqp.Return
	closed    chan *amqp.Error
	message   amqp.Publishing
	mandatory bool
	published bool
	wasClosed bool
}

func (f *confirmedChannelFixture) Confirm(bool) error {
	if f.mode == "confirm setup failure" {
		return errors.New("fixture setup unavailable")
	}
	return nil
}
func (f *confirmedChannelFixture) NotifyPublish(ch chan amqp.Confirmation) chan amqp.Confirmation {
	f.confirms = ch
	return ch
}
func (f *confirmedChannelFixture) NotifyReturn(ch chan amqp.Return) chan amqp.Return {
	f.returns = ch
	return ch
}
func (f *confirmedChannelFixture) NotifyClose(ch chan *amqp.Error) chan *amqp.Error {
	f.closed = ch
	return ch
}
func (f *confirmedChannelFixture) Close() error { f.wasClosed = true; return nil }
func (f *confirmedChannelFixture) Publish(_, _ string, mandatory, _ bool, message amqp.Publishing) error {
	f.published, f.mandatory, f.message = true, mandatory, message
	switch f.mode {
	case "accepted":
		f.confirms <- amqp.Confirmation{DeliveryTag: 1, Ack: true}
	case "returned and acknowledged":
		f.returns <- amqp.Return{ReplyCode: 312}
		f.confirms <- amqp.Confirmation{DeliveryTag: 1, Ack: true}
	case "nack":
		f.confirms <- amqp.Confirmation{DeliveryTag: 1, Ack: false}
	case "connection closed":
		close(f.closed)
	case "publish failure":
		return errors.New("fixture socket failed")
	}
	return nil
}

func TestOutcomePublicationRequiresRoutedBrokerConfirmation(t *testing.T) {
	for _, mode := range []string{"accepted", "returned and acknowledged", "nack", "connection closed", "publish failure", "confirm setup failure", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			fixture := &confirmedChannelFixture{mode: mode}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "timeout" {
				cancel()
			}
			message := amqp.Publishing{MessageId: "report-analysed:42:agent-outcome-v1", DeliveryMode: amqp.Persistent}
			err := publishConfirmed(ctx, fixture, "cleanapp-exchange", "report.analysed", message)
			if (err == nil) != (mode == "accepted") {
				t.Fatalf("publication %s returned %v", mode, err)
			}
			if !fixture.wasClosed {
				t.Fatal("dedicated confirmation channel was not closed")
			}
			if mode == "confirm setup failure" || mode == "timeout" {
				if fixture.published {
					t.Fatal("published after cancellation or without broker confirmations")
				}
			} else if !fixture.mandatory || fixture.message.MessageId != message.MessageId || fixture.message.DeliveryMode != amqp.Persistent {
				t.Fatal("lost mandatory routing, stable event identity, or persistence")
			}
		})
	}
}
