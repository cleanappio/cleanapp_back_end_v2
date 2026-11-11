package mailreply

import (
	"net/mail"
	"regexp"
	"strings"
)

// Kind is what an inbound message is. It maps onto notify outcome types in
// docs/architecture/notification-outcomes.md.
type Kind string

const (
	// KindBounce: the notification was not delivered.
	KindBounce Kind = "bounce"
	// KindAutoAck: an automatic acknowledgement, usually a help-desk ticket.
	KindAutoAck Kind = "auto_ack"
	// KindOutOfOffice: an automatic absence notice. Not an acknowledgement.
	KindOutOfOffice Kind = "out_of_office"
	// KindUnsubscribe: the recipient asked not to be contacted again.
	KindUnsubscribe Kind = "unsubscribe"
	// KindHumanReply: none of the above; treated as a person answering.
	KindHumanReply Kind = "human_reply"
)

// Result is the classification of one inbound message. Every field is derived
// from the message itself; nothing is inferred about whether the underlying
// problem was fixed.
type Result struct {
	Kind Kind
	// Permanent is set for bounces: true for a 5.x.x failure (do not retry
	// this address), false for a 4.x.x or unknown failure.
	Permanent bool
	// StatusCode is the RFC 3463 code found in a bounce, such as "5.1.1".
	StatusCode string
	// TicketRef is the recipient's own reference, when an acknowledgement
	// states one, such as "#48213". Empty when none is recognizable.
	TicketRef string
	// Reason says which rule decided, for logs and review. It contains no
	// message content.
	Reason string
}

var (
	statusCode   = regexp.MustCompile(`(?i)\b(?:status:\s*|#)?([45])\.(\d{1,3})\.(\d{1,3})\b`)
	smtpCode     = regexp.MustCompile(`\b(5[0-5]\d|4[0-5]\d)[ -]`)
	bounceSubj   = regexp.MustCompile(`(?i)(undeliver|delivery status notification|delivery (has )?failed|mail delivery (failed|subsystem)|returned mail|failure notice|message not delivered|delivery incomplete)`)
	oooSubj      = regexp.MustCompile(`(?i)(out of (the )?office|automatic reply|auto(matic)?[- ]?reply|autoreply|on (annual |parental )?leave|vacation|away from)`)
	oooStrong    = regexp.MustCompile(`(?i)(out of (the )?office|on (annual |parental )?leave|vacation|away from|i am currently away|will return|back on)`)
	ackBody      = regexp.MustCompile(`(?i)(we('| ha)ve received your (request|message|email|inquiry|enquiry|report)|thank(s| you) for (contacting|reaching out|your (message|email|request))|your (request|ticket|case) (has been|was) (received|created|logged|opened)|a (ticket|case) has been (created|opened))`)
	ticketRef    = regexp.MustCompile(`(?i)(?:\[|\()?(?:ticket|request|case|incident|reference|ref|id)\s*(?:number|no\.?|id)?\s*[:#]?\s*#?([A-Z]{0,4}-?\d{4,12})\b`)
	bracketRef   = regexp.MustCompile(`\[#?([A-Z]{0,4}-?\d{4,12})\]`)
	unsubSubject = regexp.MustCompile(`(?i)^\s*(re:\s*)*unsubscribe\b`)
)

// Classify decides what an inbound message is. header is the message header;
// text is its plain-text body (or the readable part of it), which may be empty.
//
// Order matters: a bounce can carry auto-reply headers, and an out-of-office
// can thank the sender, so the more specific and more consequential kinds are
// tested first.
func Classify(header mail.Header, text string) Result {
	subject := header.Get("Subject")
	from := strings.ToLower(header.Get("From"))
	contentType := strings.ToLower(header.Get("Content-Type"))
	auto := strings.ToLower(header.Get("Auto-Submitted"))
	precedence := strings.ToLower(header.Get("Precedence"))

	isDSN := strings.Contains(contentType, "multipart/report") && strings.Contains(contentType, "delivery-status")
	fromDaemon := strings.Contains(from, "mailer-daemon") || strings.Contains(from, "postmaster@") || strings.Contains(from, "mail delivery subsystem")
	if isDSN || header.Get("X-Failed-Recipients") != "" || (fromDaemon && bounceSubj.MatchString(subject)) {
		result := Result{Kind: KindBounce, Reason: "delivery status notification"}
		if m := statusCode.FindStringSubmatch(text); m != nil {
			result.StatusCode = m[1] + "." + m[2] + "." + m[3]
			result.Permanent = m[1] == "5"
		} else if m := smtpCode.FindStringSubmatch(text); m != nil {
			result.Permanent = m[1][0] == '5'
		}
		return result
	}

	if unsubSubject.MatchString(subject) {
		return Result{Kind: KindUnsubscribe, Reason: "unsubscribe subject"}
	}

	automatic := (auto != "" && auto != "no") || header.Get("X-Autoreply") != "" || header.Get("X-Autorespond") != "" ||
		header.Get("X-Auto-Response-Suppress") != "" || precedence == "auto_reply" || precedence == "bulk" || precedence == "junk"

	if oooStrong.MatchString(subject) || (automatic && oooStrong.MatchString(firstLines(text, 12))) {
		return Result{Kind: KindOutOfOffice, Reason: "absence notice"}
	}

	ref := ""
	if m := bracketRef.FindStringSubmatch(subject); m != nil {
		ref = m[1]
	} else if m := ticketRef.FindStringSubmatch(subject); m != nil {
		ref = m[1]
	} else if m := ticketRef.FindStringSubmatch(firstLines(text, 30)); m != nil {
		ref = m[1]
	}
	if automatic || ackBody.MatchString(firstLines(text, 30)) {
		if automatic && ref == "" && !ackBody.MatchString(firstLines(text, 30)) && oooSubj.MatchString(subject) {
			return Result{Kind: KindOutOfOffice, Reason: "automatic reply without acknowledgement"}
		}
		return Result{Kind: KindAutoAck, TicketRef: ref, Reason: "automatic acknowledgement"}
	}
	return Result{Kind: KindHumanReply, TicketRef: ref, Reason: "no automatic markers"}
}

func firstLines(text string, n int) string {
	lines := strings.SplitN(text, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
