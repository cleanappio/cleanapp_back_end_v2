// Package inbox reads replies to notification emails and records what came
// back: a bounce, an automatic acknowledgement, an absence notice, an
// unsubscribe request or a reply. It is phase 1b of
// docs/architecture/notification-outcomes.md.
//
// Rules this package keeps:
//   - An inbound message is untrusted input. It is tied to a notification only
//     by a signed reply address or an exact Message-ID reference. Its content
//     never chooses a recipient and is never stored.
//   - It only reads the mailbox. What has been handled is kept in the
//     database (inbox_messages), not in mailbox labels.
//   - It fails closed. A message that cannot be parsed or matched records no
//     outcome.
package inbox

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
)

// Limits on how much of a message is read. A reply can quote a long thread
// or carry attachments; classification needs only the start of the text.
const (
	maxTextBytes = 64 * 1024
	maxPartBytes = 256 * 1024
	maxMIMEDepth = 4
)

// Message is the part of an inbound email that classification and matching use.
type Message struct {
	Header mail.Header
	// Text is the readable text: the plain-text body and, for a bounce, the
	// delivery-status and returned-header parts.
	Text string
	// References are the Message-IDs this message answers or quotes, without
	// angle brackets: In-Reply-To and References, then any Message-ID quoted
	// in the body (a bounce returns the original headers there).
	References []string
}

var messageIDPattern = regexp.MustCompile(`<([^<>\s@]{1,200}@[^<>\s@]{1,200})>`)
var quotedMessageID = regexp.MustCompile(`(?im)^message-id:\s*<([^<>\s@]{1,200}@[^<>\s@]{1,200})>`)

// Parse reads a raw RFC 5322 message.
func Parse(raw []byte) (*Message, error) {
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var text strings.Builder
	collectText(&text, parsed.Header.Get("Content-Type"), parsed.Header.Get("Content-Transfer-Encoding"), parsed.Body, 0)
	message := &Message{Header: parsed.Header, Text: text.String()}

	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			message.References = append(message.References, id)
		}
	}
	for _, key := range []string{"In-Reply-To", "References"} {
		for _, match := range messageIDPattern.FindAllStringSubmatch(parsed.Header.Get(key), -1) {
			add(match[1])
		}
	}
	for _, match := range quotedMessageID.FindAllStringSubmatch(message.Text, 8) {
		add(match[1])
	}
	return message, nil
}

// collectText appends the readable parts of a MIME body to out, up to
// maxTextBytes. Unknown or binary parts are skipped.
func collectText(out *strings.Builder, contentType, encoding string, body io.Reader, depth int) {
	if out.Len() >= maxTextBytes || depth > maxMIMEDepth {
		return
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain" // RFC 2045: a missing or broken type is plain text
	}
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextRawPart()
			if err != nil {
				return
			}
			collectText(out, part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part, depth+1)
		}
	case mediaType == "message/rfc822":
		// A returned original message: only its headers matter (Message-ID).
		inner, err := mail.ReadMessage(io.LimitReader(body, maxPartBytes))
		if err != nil {
			return
		}
		appendText(out, "Message-ID: "+inner.Header.Get("Message-Id")+"\n")
	case mediaType == "text/plain", mediaType == "message/delivery-status", mediaType == "text/rfc822-headers":
		limited := io.LimitReader(body, maxPartBytes)
		switch strings.ToLower(strings.TrimSpace(encoding)) {
		case "base64":
			limited = base64.NewDecoder(base64.StdEncoding, newlineStripper{limited})
		case "quoted-printable":
			limited = quotedprintable.NewReader(limited)
		}
		data, _ := io.ReadAll(limited) // a truncated part is still usable text
		appendText(out, string(data)+"\n")
	}
}

func appendText(out *strings.Builder, text string) {
	if room := maxTextBytes - out.Len(); room > 0 {
		if len(text) > room {
			text = text[:room]
		}
		out.WriteString(strings.ToValidUTF8(text, ""))
	}
}

// newlineStripper removes line breaks, which the base64 decoder rejects.
type newlineStripper struct{ r io.Reader }

func (s newlineStripper) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	kept := 0
	for _, b := range p[:n] {
		if b != '\r' && b != '\n' {
			p[kept] = b
			kept++
		}
	}
	return kept, err
}

// senderDomain is the domain of the From address, lower case, or "".
func senderDomain(header mail.Header) string {
	address, err := mail.ParseAddress(header.Get("From"))
	if err != nil {
		return ""
	}
	return domainOf(address.Address)
}

func domainOf(address string) string {
	at := strings.LastIndexByte(address, '@')
	if at < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(address[at+1:]))
}
