package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cleanapp-common/mailreply"
)

// Notification is an email this system sent, as recorded by email-service in
// notification_messages.
type Notification struct {
	ID        string
	Recipient string
	// PublicIDs are the public IDs of the reports the email carried.
	PublicIDs []string
}

// Record is what one inbound message amounts to.
type Record struct {
	MailboxMessageID string
	// Result is the notify outcome type, or ResultUnmatched or
	// ResultUnparseable when no outcome is recorded.
	Result string
	// Notification is nil when the message was not matched.
	Notification *Notification
	// Evidence is stored as notify_outcomes.evidence_json. It never contains
	// message text or a sender address.
	Evidence string
	// PermanentBounce: the recipient address does not accept mail.
	PermanentBounce bool
	// OptOut: the recipient asked not to be contacted again.
	OptOut     bool
	ReceivedAt time.Time
}

const (
	ResultUnmatched   = "unmatched"
	ResultUnparseable = "unparseable"
)

// Store is the database side of the worker.
type Store interface {
	// InboxMessageHandled reports whether a mailbox message was already recorded.
	InboxMessageHandled(ctx context.Context, mailboxMessageID string) (bool, error)
	// NotificationByID returns nil when the ID is unknown.
	NotificationByID(ctx context.Context, id string) (*Notification, error)
	// NotificationByMessageID finds a notification by the Message-ID the mail
	// relay gave it. It returns nil when none of the IDs is known.
	NotificationByMessageID(ctx context.Context, messageIDs []string) (*Notification, error)
	// RecordInboxMessage stores the record once; a repeat is not an error.
	RecordInboxMessage(ctx context.Context, record Record) error
}

// outcomeTypes maps what a message is onto notify_outcomes.outcome_type.
var outcomeTypes = map[mailreply.Kind]string{
	mailreply.KindBounce:      "bounced",
	mailreply.KindAutoAck:     "acknowledged",
	mailreply.KindOutOfOffice: "out_of_office",
	mailreply.KindUnsubscribe: "unsubscribed",
	mailreply.KindHumanReply:  "replied",
}

// Handle classifies one raw message and records the result. It returns the
// result recorded, or "" when the message had been handled before.
func Handle(ctx context.Context, store Store, secret []byte, mailboxMessageID string, raw []byte, receivedAt time.Time) (string, error) {
	handled, err := store.InboxMessageHandled(ctx, mailboxMessageID)
	if err != nil {
		return "", err
	}
	if handled {
		return "", nil
	}
	record := Record{MailboxMessageID: mailboxMessageID, Result: ResultUnparseable, ReceivedAt: receivedAt}
	message, err := Parse(raw)
	if err != nil {
		return record.Result, store.RecordInboxMessage(ctx, record)
	}

	// A signed reply address proves which notification is answered. A bounce
	// goes to the envelope sender instead, so it is matched by the original
	// Message-ID it returns.
	var notification *Notification
	if id, ok := mailreply.FindID(message.Header, secret); ok {
		notification, err = store.NotificationByID(ctx, id)
	} else if len(message.References) > 0 {
		notification, err = store.NotificationByMessageID(ctx, message.References)
	}
	if err != nil {
		return "", err
	}
	if notification == nil || len(notification.PublicIDs) == 0 {
		record.Result = ResultUnmatched
		return record.Result, store.RecordInboxMessage(ctx, record)
	}

	result := mailreply.Classify(message.Header, message.Text)
	record.Notification = notification
	record.Result = outcomeTypes[result.Kind]
	record.PermanentBounce = result.Kind == mailreply.KindBounce && result.Permanent
	record.OptOut = result.Kind == mailreply.KindUnsubscribe
	evidence := map[string]any{
		"kind":               string(result.Kind),
		"notification_id":    notification.ID,
		"mailbox_message_id": mailboxMessageID,
		// A reply address is a capability: anyone holding it can answer. Record
		// whether the answer came from the recipient's own domain, so a reply
		// is not presented as the organization's when it is not.
		"from_recipient_domain": senderDomain(message.Header) != "" && senderDomain(message.Header) == domainOf(notification.Recipient),
	}
	if result.Kind == mailreply.KindBounce {
		evidence["permanent"] = result.Permanent
		if result.StatusCode != "" {
			evidence["status_code"] = result.StatusCode
		}
	}
	if result.TicketRef != "" {
		evidence["ticket_ref"] = result.TicketRef
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return "", fmt.Errorf("encode inbox evidence: %w", err)
	}
	record.Evidence = string(encoded)
	return record.Result, store.RecordInboxMessage(ctx, record)
}

// Mailbox is read-only access to one mailbox.
type Mailbox interface {
	// List returns the IDs of messages received since the given time.
	List(ctx context.Context, since time.Time) ([]string, error)
	// Raw returns one message in RFC 5322 form and when it was received.
	Raw(ctx context.Context, id string) ([]byte, time.Time, error)
}

// Poll handles every message received since the given time that has not been
// handled before, and returns how many of each result it recorded. One bad
// message does not stop the rest; the first error is returned at the end.
func Poll(ctx context.Context, mailbox Mailbox, store Store, secret []byte, since time.Time) (map[string]int, error) {
	ids, err := mailbox.List(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("list mailbox: %w", err)
	}
	counts := map[string]int{}
	var firstErr error
	for _, id := range ids {
		if ctx.Err() != nil {
			return counts, ctx.Err()
		}
		result, err := handleOne(ctx, mailbox, store, secret, id)
		if err != nil {
			if firstErr == nil {
				// The message ID is safe to log; message content is not.
				firstErr = fmt.Errorf("mailbox message %s: %w", strings.TrimSpace(id), err)
			}
			continue
		}
		if result != "" {
			counts[result]++
		}
	}
	return counts, firstErr
}

func handleOne(ctx context.Context, mailbox Mailbox, store Store, secret []byte, id string) (string, error) {
	// Check before fetching: most messages in the window were handled already.
	handled, err := store.InboxMessageHandled(ctx, id)
	if err != nil || handled {
		return "", err
	}
	raw, receivedAt, err := mailbox.Raw(ctx, id)
	if err != nil {
		return "", err
	}
	return Handle(ctx, store, secret, id, raw, receivedAt)
}
