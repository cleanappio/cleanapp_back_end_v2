package mailreply

import (
	"net/mail"
	"strings"
	"testing"
)

var secret = []byte("0123456789abcdef0123456789abcdef")

func TestAddressRoundTrip(t *testing.T) {
	addr, err := Address("info@cleanapp.io", "n7f3k2m9q4x1", secret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(addr, "info+r.n7f3k2m9q4x1.") || !strings.HasSuffix(addr, "@cleanapp.io") {
		t.Fatalf("unexpected address %q", addr)
	}
	id, ok := ParseAddress("Support <"+strings.ToUpper(addr)+">", secret)
	if !ok || id != "n7f3k2m9q4x1" {
		t.Fatalf("round trip failed: %q %v", id, ok)
	}
}

func TestParseAddressRejectsForgedAndForeign(t *testing.T) {
	addr, _ := Address("info@cleanapp.io", "n7f3k2m9q4x1", secret)
	other, _ := Address("info@cleanapp.io", "zzzzzzzz0000", secret)
	forged := strings.Replace(addr, "n7f3k2m9q4x1", "zzzzzzzz0000", 1) // id swapped, signature kept
	for _, bad := range []string{"info@cleanapp.io", "info+news@cleanapp.io", forged, "info+r.short.abc@cleanapp.io", "not an address"} {
		if _, ok := ParseAddress(bad, secret); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, ok := ParseAddress(addr, []byte("another-secret-of-16+bytes")); ok {
		t.Error("accepted signature under a different secret")
	}
	if _, ok := ParseAddress(other, secret); !ok {
		t.Error("rejected a valid second address")
	}
}

func TestAddressValidation(t *testing.T) {
	if _, err := Address("info@cleanapp.io", "n7f3k2m9q4x1", []byte("short")); err == nil {
		t.Error("accepted a short secret")
	}
	if _, err := Address("info@cleanapp.io", "Has Space", secret); err == nil {
		t.Error("accepted an invalid id")
	}
	if _, err := Address("info+x@cleanapp.io", "n7f3k2m9q4x1", secret); err == nil {
		t.Error("accepted a base mailbox that already has a tag")
	}
}

func TestFindIDChecksDeliveryHeaders(t *testing.T) {
	addr, _ := Address("info@cleanapp.io", "n7f3k2m9q4x1", secret)
	h := mail.Header{"To": {"someone@example.com"}, "Delivered-To": {addr}}
	if id, ok := FindID(h, secret); !ok || id != "n7f3k2m9q4x1" {
		t.Fatalf("did not find id in Delivered-To: %q %v", id, ok)
	}
	if _, ok := FindID(mail.Header{"To": {"info@cleanapp.io"}}, secret); ok {
		t.Error("found an id where none exists")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		header mail.Header
		text   string
		kind   Kind
		check  func(Result) bool
	}{
		{"hard bounce DSN", mail.Header{"From": {"Mail Delivery Subsystem <mailer-daemon@googlemail.com>"}, "Subject": {"Delivery Status Notification (Failure)"}, "Content-Type": {`multipart/report; report-type=delivery-status; boundary="x"`}},
			"Address not found\nStatus: 5.1.1\n550 5.1.1 The email account that you tried to reach does not exist.", KindBounce,
			func(r Result) bool { return r.Permanent && r.StatusCode == "5.1.1" }},
		{"soft bounce", mail.Header{"From": {"postmaster@example.com"}, "Subject": {"Undeliverable: CleanApp report"}},
			"Status: 4.2.2\nMailbox full", KindBounce, func(r Result) bool { return !r.Permanent && r.StatusCode == "4.2.2" }},
		{"bounce with auto headers still a bounce", mail.Header{"From": {"MAILER-DAEMON@mx.example.com"}, "Subject": {"Mail delivery failed"}, "Auto-Submitted": {"auto-replied"}},
			"550 mailbox unavailable", KindBounce, func(r Result) bool { return r.Permanent }},
		{"help desk ticket", mail.Header{"From": {"Support <support@example.com>"}, "Subject": {"[#48213] We received your request"}, "Auto-Submitted": {"auto-generated"}},
			"Thank you for contacting us. Your request has been received.", KindAutoAck, func(r Result) bool { return r.TicketRef == "48213" }},
		{"ack in body without headers", mail.Header{"From": {"help@example.com"}, "Subject": {"Re: CleanApp report"}},
			"Hello,\nWe have received your request. Ticket number: ABC-204455\n", KindAutoAck, func(r Result) bool { return r.TicketRef == "ABC-204455" }},
		{"out of office", mail.Header{"From": {"pat@example.com"}, "Subject": {"Automatic reply: CleanApp report"}, "Auto-Submitted": {"auto-replied"}},
			"I am out of the office until Monday.", KindOutOfOffice, nil},
		{"out of office that says thanks", mail.Header{"From": {"pat@example.com"}, "Subject": {"Out of office"}, "Auto-Submitted": {"auto-replied"}},
			"Thank you for your email. I will return on 3 March.", KindOutOfOffice, nil},
		{"unsubscribe", mail.Header{"From": {"pat@example.com"}, "Subject": {"RE: UNSUBSCRIBE"}}, "", KindUnsubscribe, nil},
		{"human reply", mail.Header{"From": {"Pat Lee <pat@example.com>"}, "Subject": {"Re: 2 new reports about Example"}},
			"Hi, thanks for flagging this. We shipped a fix this morning.\n", KindHumanReply, func(r Result) bool { return r.TicketRef == "" }},
		{"human reply quoting a year is not a ticket", mail.Header{"From": {"pat@example.com"}, "Subject": {"Re: report"}},
			"We fixed it in the 2026 release.", KindHumanReply, func(r Result) bool { return r.TicketRef == "" }},
	}
	for _, c := range cases {
		got := Classify(c.header, c.text)
		if got.Kind != c.kind || (c.check != nil && !c.check(got)) {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}
