package inbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cleanapp-common/mailreply"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

const (
	testNotificationID = "0123456789abcdef"
	testProviderID     = "CAKx7=abc123@mail.gmail.com"
)

type fakeStore struct {
	notification *Notification
	handled      map[string]bool
	records      []Record
	failRecord   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		notification: &Notification{ID: testNotificationID, Recipient: "Support@Example.com", PublicIDs: []string{"rpt_a", "rpt_b"}},
		handled:      map[string]bool{},
	}
}

func (f *fakeStore) InboxMessageHandled(_ context.Context, id string) (bool, error) {
	return f.handled[id], nil
}
func (f *fakeStore) NotificationByID(_ context.Context, id string) (*Notification, error) {
	if id == testNotificationID {
		return f.notification, nil
	}
	return nil, nil
}
func (f *fakeStore) NotificationByMessageID(_ context.Context, ids []string) (*Notification, error) {
	for _, id := range ids {
		if id == testProviderID {
			return f.notification, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) RecordInboxMessage(_ context.Context, record Record) error {
	if f.failRecord != nil {
		return f.failRecord
	}
	f.handled[record.MailboxMessageID] = true
	f.records = append(f.records, record)
	return nil
}

func replyAddress(t *testing.T) string {
	t.Helper()
	address, err := mailreply.Address("info@cleanapp.io", testNotificationID, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func crlf(lines ...string) []byte { return []byte(strings.Join(lines, "\r\n")) }

func handle(t *testing.T, store *fakeStore, id string, raw []byte) (string, Record) {
	t.Helper()
	result, err := Handle(context.Background(), store, testSecret, id, raw, time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.records) == 0 {
		return result, Record{}
	}
	return result, store.records[len(store.records)-1]
}

func evidence(t *testing.T, record Record) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(record.Evidence), &decoded); err != nil {
		t.Fatalf("evidence is not JSON: %v (%q)", err, record.Evidence)
	}
	return decoded
}

func TestHelpDeskAcknowledgementIsRecordedWithItsTicket(t *testing.T) {
	store := newFakeStore()
	raw := crlf(
		"From: Example Support <support@example.com>",
		"To: "+replyAddress(t),
		"Subject: [#48213] We received your request",
		"Auto-Submitted: auto-replied",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Thank you for contacting Example. A ticket has been created. SECRET-BODY-TEXT",
	)
	result, record := handle(t, store, "m1", raw)
	if result != "acknowledged" || record.Notification == nil || record.OptOut || record.PermanentBounce {
		t.Fatalf("result=%q record=%+v", result, record)
	}
	got := evidence(t, record)
	if got["ticket_ref"] != "48213" || got["kind"] != "auto_ack" || got["from_recipient_domain"] != true ||
		got["notification_id"] != testNotificationID || got["mailbox_message_id"] != "m1" {
		t.Fatalf("evidence=%v", got)
	}
	if strings.Contains(record.Evidence, "SECRET-BODY-TEXT") || strings.Contains(strings.ToLower(record.Evidence), "support@example.com") {
		t.Fatalf("evidence must not hold message text or the sender address: %s", record.Evidence)
	}
}

func TestBounceIsMatchedByTheReturnedMessageID(t *testing.T) {
	store := newFakeStore()
	raw := crlf(
		"From: Mail Delivery Subsystem <mailer-daemon@googlemail.com>",
		"To: info@cleanapp.io",
		"Subject: Delivery Status Notification (Failure)",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="b1"`,
		"",
		"--b1",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Address not found. Your message wasn't delivered.",
		"--b1",
		"Content-Type: message/delivery-status",
		"",
		"Final-Recipient: rfc822; support@example.com",
		"Action: failed",
		"Status: 5.1.1",
		"--b1",
		"Content-Type: message/rfc822",
		"",
		"From: info@cleanapp.io",
		"To: support@example.com",
		"Message-ID: <"+testProviderID+">",
		"Subject: Reports about Example",
		"",
		"original body",
		"--b1--",
	)
	result, record := handle(t, store, "m2", raw)
	if result != "bounced" || !record.PermanentBounce {
		t.Fatalf("result=%q permanent=%v", result, record.PermanentBounce)
	}
	got := evidence(t, record)
	if got["status_code"] != "5.1.1" || got["permanent"] != true || got["from_recipient_domain"] != false {
		t.Fatalf("evidence=%v", got)
	}
}

func TestTemporaryBounceIsNotPermanent(t *testing.T) {
	store := newFakeStore()
	raw := crlf(
		"From: postmaster@example.net",
		"To: info@cleanapp.io",
		"Subject: Undeliverable: Reports about Example",
		"X-Failed-Recipients: support@example.com",
		"In-Reply-To: <"+testProviderID+">",
		"",
		"Status: 4.2.2 mailbox full",
	)
	result, record := handle(t, store, "m3", raw)
	if result != "bounced" || record.PermanentBounce {
		t.Fatalf("result=%q permanent=%v", result, record.PermanentBounce)
	}
}

func TestAbsenceNoticeIsNotAnAcknowledgement(t *testing.T) {
	store := newFakeStore()
	raw := crlf(
		"From: someone@example.com",
		"To: "+replyAddress(t),
		"Subject: Automatic reply: Out of office",
		"Auto-Submitted: auto-replied",
		"",
		"I am currently away and will return on Monday.",
	)
	if result, _ := handle(t, store, "m4", raw); result != "out_of_office" {
		t.Fatalf("result=%q", result)
	}
}

func TestReplyRecordsWhetherItCameFromTheRecipientsDomain(t *testing.T) {
	for _, tc := range []struct {
		from string
		want bool
	}{{"Jo <jo@example.com>", true}, {"Jo <jo@elsewhere.org>", false}, {"not an address", false}} {
		store := newFakeStore()
		// Base64 with line breaks, as mail clients send it.
		body := base64.StdEncoding.EncodeToString([]byte("Thanks, we are looking into this now and will follow up."))
		raw := crlf(
			"From: "+tc.from,
			"Delivered-To: "+replyAddress(t),
			"To: someone-else@cleanapp.io",
			"Subject: Re: Reports about Example",
			"Content-Type: text/plain; charset=utf-8",
			"Content-Transfer-Encoding: base64",
			"",
			body[:20],
			body[20:],
		)
		result, record := handle(t, store, "m5", raw)
		if result != "replied" {
			t.Fatalf("from=%q result=%q", tc.from, result)
		}
		if got := evidence(t, record)["from_recipient_domain"]; got != tc.want {
			t.Fatalf("from=%q from_recipient_domain=%v want %v", tc.from, got, tc.want)
		}
	}
}

func TestUnsubscribeOptsOutTheNotifiedAddress(t *testing.T) {
	store := newFakeStore()
	raw := crlf("From: support@example.com", "To: "+replyAddress(t), "Subject: Re: UNSUBSCRIBE", "", "please stop")
	result, record := handle(t, store, "m6", raw)
	if result != "unsubscribed" || !record.OptOut {
		t.Fatalf("result=%q optout=%v", result, record.OptOut)
	}
	// The address opted out is the one notified, never one named in the message.
	if record.Notification.Recipient != "Support@Example.com" {
		t.Fatalf("recipient=%q", record.Notification.Recipient)
	}
}

func TestForgedUnknownAndUnrelatedMessagesRecordNoOutcome(t *testing.T) {
	forged := strings.Replace(replyAddress(t), testNotificationID, "fedcba9876543210", 1)
	for name, raw := range map[string][]byte{
		"forged signature":   crlf("From: a@example.com", "To: "+forged, "Subject: Re: hello", "", "hi"),
		"no token or thread": crlf("From: a@example.com", "To: info@cleanapp.io", "Subject: Partnership", "", "hi"),
		"unknown thread":     crlf("From: a@example.com", "To: info@cleanapp.io", "In-Reply-To: <other@mail.example>", "Subject: Re: x", "", "hi"),
	} {
		store := newFakeStore()
		result, record := handle(t, store, "m7", raw)
		if result != ResultUnmatched || record.Notification != nil || record.Evidence != "" || record.OptOut {
			t.Fatalf("%s: result=%q record=%+v", name, result, record)
		}
	}
	store := newFakeStore()
	if result, record := handle(t, store, "m8", []byte("this is not an email")); result != ResultUnparseable || record.Notification != nil {
		t.Fatalf("result=%q record=%+v", result, record)
	}
	// A notification that carried no public report has nothing to attach to.
	store = newFakeStore()
	store.notification.PublicIDs = nil
	if result, _ := handle(t, store, "m9", crlf("From: a@example.com", "To: "+replyAddress(t), "Subject: Re: x", "", "hi")); result != ResultUnmatched {
		t.Fatalf("result=%q", result)
	}
}

func TestAMessageIsHandledOnce(t *testing.T) {
	store := newFakeStore()
	raw := crlf("From: a@example.com", "To: "+replyAddress(t), "Subject: Re: x", "", "hi")
	handle(t, store, "m10", raw)
	result, err := Handle(context.Background(), store, testSecret, "m10", raw, time.Now())
	if err != nil || result != "" || len(store.records) != 1 {
		t.Fatalf("result=%q err=%v records=%d", result, err, len(store.records))
	}
}

func TestParseBoundsWhatItReads(t *testing.T) {
	long := strings.Repeat("x", 3*maxTextBytes)
	message, err := Parse(crlf("From: a@example.com", "Subject: big", "", long))
	if err != nil || len(message.Text) > maxTextBytes {
		t.Fatalf("err=%v len=%d", err, len(message.Text))
	}
	// Attachments and HTML are skipped; nested multipart text is read.
	raw := crlf(
		"From: a@example.com", "Subject: nested", `Content-Type: multipart/mixed; boundary="o"`, "",
		"--o", `Content-Type: multipart/alternative; boundary="i"`, "",
		"--i", "Content-Type: text/plain", "Content-Transfer-Encoding: quoted-printable", "", "plain=20part",
		"--i", "Content-Type: text/html", "", "<b>html part</b>", "--i--",
		"--o", "Content-Type: application/pdf", "Content-Transfer-Encoding: base64", "", "QUJD", "--o--",
	)
	message, err = Parse(raw)
	if err != nil || !strings.Contains(message.Text, "plain part") || strings.Contains(message.Text, "html part") || strings.Contains(message.Text, "QUJD") {
		t.Fatalf("err=%v text=%q", err, message.Text)
	}
}

type fakeMailbox struct {
	ids     []string
	raw     map[string][]byte
	fetched []string
	listErr error
}

func (f *fakeMailbox) List(context.Context, time.Time) ([]string, error) { return f.ids, f.listErr }
func (f *fakeMailbox) Raw(_ context.Context, id string) ([]byte, time.Time, error) {
	f.fetched = append(f.fetched, id)
	raw, ok := f.raw[id]
	if !ok {
		return nil, time.Time{}, errors.New("fetch failed")
	}
	return raw, time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC), nil
}

func TestPollSkipsHandledMessagesAndSurvivesOneBadMessage(t *testing.T) {
	store := newFakeStore()
	store.handled["old"] = true
	reply := crlf("From: a@example.com", "To: "+replyAddress(t), "Subject: Re: x", "", "hi")
	mailbox := &fakeMailbox{ids: []string{"old", "broken", "new", "other"}, raw: map[string][]byte{
		"new":   reply,
		"other": crlf("From: a@example.com", "To: info@cleanapp.io", "Subject: hello", "", "hi"),
	}}
	counts, err := Poll(context.Background(), mailbox, store, testSecret, time.Now().Add(-time.Hour))
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("the failed message must be reported: %v", err)
	}
	if counts["replied"] != 1 || counts[ResultUnmatched] != 1 || len(counts) != 2 {
		t.Fatalf("counts=%v", counts)
	}
	if strings.Join(mailbox.fetched, ",") != "broken,new,other" {
		t.Fatalf("handled messages must not be fetched again: %v", mailbox.fetched)
	}
	if !store.records[0].ReceivedAt.Equal(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("outcome time must be the receive time: %v", store.records[0].ReceivedAt)
	}
	if _, err := Poll(context.Background(), &fakeMailbox{listErr: errors.New("denied")}, store, testSecret, time.Now()); err == nil {
		t.Fatal("a mailbox that cannot be listed is an error")
	}
}

func TestGmailReaderListsPagesAndDecodesRawMessages(t *testing.T) {
	raw := crlf("From: a@example.com", "Subject: hi", "", "body")
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("the reader must only read: %s %s", r.Method, r.URL.Path)
		}
		switch {
		case r.URL.Path == "/messages" && r.URL.Query().Get("pageToken") == "":
			queries = append(queries, r.URL.Query().Get("q"))
			w.Write([]byte(`{"messages":[{"id":"a"},{"id":"b"}],"nextPageToken":"p2"}`))
		case r.URL.Path == "/messages":
			w.Write([]byte(`{"messages":[{"id":"c"}]}`))
		case r.URL.Path == "/messages/a" && r.URL.Query().Get("format") == "raw":
			json.NewEncoder(w).Encode(map[string]string{"raw": base64.URLEncoding.EncodeToString(raw), "internalDate": "1794744000000"})
		default:
			http.Error(w, `{"error":"secret detail"}`, http.StatusForbidden)
		}
	}))
	defer server.Close()
	gmail := &Gmail{client: server.Client(), baseURL: server.URL}

	since := time.Unix(1794700000, 0)
	ids, err := gmail.List(context.Background(), since)
	if err != nil || strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if len(queries) != 1 || queries[0] != "-in:sent -in:drafts -in:chats after:1794700000" {
		t.Fatalf("query=%q", queries)
	}
	got, receivedAt, err := gmail.Raw(context.Background(), "a")
	if err != nil || string(got) != string(raw) || !receivedAt.Equal(time.UnixMilli(1794744000000)) {
		t.Fatalf("raw=%q at=%v err=%v", got, receivedAt, err)
	}
	if _, _, err := gmail.Raw(context.Background(), "missing"); err == nil || strings.Contains(err.Error(), "secret detail") {
		t.Fatalf("an HTTP error must be reported without the response body: %v", err)
	}
}

func TestGmailReaderAsksOnlyForReadAccess(t *testing.T) {
	if gmailReadOnlyScope != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Fatalf("scope=%q", gmailReadOnlyScope)
	}
	if _, err := NewGmail(context.Background(), "/nonexistent/key.json", "info@cleanapp.io"); err == nil {
		t.Fatal("missing credentials must fail")
	}
}
