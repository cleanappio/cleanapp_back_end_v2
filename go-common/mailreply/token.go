// Package mailreply turns replies to CleanApp notification emails into
// recorded outcomes. It has two halves, both pure functions with no I/O:
//
//   - token.go: a signed reply address per notification, so an inbound message
//     can be tied to the exact notification it answers without trusting the
//     sender.
//   - classify.go: decide what an inbound message is (bounce, automatic
//     acknowledgement, out-of-office, unsubscribe request or human reply).
//
// See docs/architecture/notification-outcomes.md for how these are used.
package mailreply

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

// Reply addresses use Gmail plus-addressing, so they need no mailbox or DNS
// change: info+r.<id>.<mac>@cleanapp.io is delivered to info@cleanapp.io.
const tagPrefix = "r."

var (
	idPattern = regexp.MustCompile(`^[a-z0-9]{8,32}$`)
	b32       = base32.StdEncoding.WithPadding(base32.NoPadding)
)

func sign(secret []byte, id string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("cleanapp-reply-v1\x00" + id))
	return strings.ToLower(b32.EncodeToString(mac.Sum(nil)[:10]))
}

// Address returns the Reply-To address for one notification. base is the
// mailbox that receives replies (for example "info@cleanapp.io"); id is the
// notification's opaque identifier: lower-case letters and digits, 8 to 32
// characters, with no meaning to outsiders. The secret must be at least 16
// bytes and must never be logged.
func Address(base, id string, secret []byte) (string, error) {
	if len(secret) < 16 {
		return "", fmt.Errorf("reply secret must be at least 16 bytes")
	}
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("notification id must be 8-32 lower-case letters or digits")
	}
	parsed, err := mail.ParseAddress(base)
	if err != nil {
		return "", fmt.Errorf("invalid reply mailbox: %w", err)
	}
	at := strings.LastIndexByte(parsed.Address, '@')
	local, domain := parsed.Address[:at], parsed.Address[at+1:]
	if strings.ContainsRune(local, '+') {
		return "", fmt.Errorf("reply mailbox must not already contain a plus tag")
	}
	return local + "+" + tagPrefix + id + "." + sign(secret, id) + "@" + domain, nil
}

// ParseAddress returns the notification id carried by a reply address, and
// whether its signature is valid. An unsigned, altered or foreign address
// returns ok=false: callers must then treat the message as unmatched rather
// than guess which notification it answers.
func ParseAddress(address string, secret []byte) (id string, ok bool) {
	if len(secret) < 16 {
		return "", false
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return "", false
	}
	addr := strings.ToLower(parsed.Address)
	at := strings.LastIndexByte(addr, '@')
	plus := strings.IndexByte(addr[:at], '+')
	if plus < 0 {
		return "", false
	}
	tag := addr[plus+1 : at]
	if !strings.HasPrefix(tag, tagPrefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(tag, tagPrefix), ".")
	if len(parts) != 2 || !idPattern.MatchString(parts[0]) {
		return "", false
	}
	if !hmac.Equal([]byte(sign(secret, parts[0])), []byte(parts[1])) {
		return "", false
	}
	return parts[0], true
}

// FindID looks for a valid reply address in the places a mail system records
// the envelope recipient. Bounces and some ticket systems rewrite To, so the
// delivery headers are checked as well. The first valid signature wins.
func FindID(header mail.Header, secret []byte) (string, bool) {
	for _, key := range []string{"Delivered-To", "X-Original-To", "To", "Cc", "X-Failed-Recipients"} {
		for _, value := range header[textprotoKey(key)] {
			list, err := mail.ParseAddressList(value)
			if err != nil {
				if id, ok := ParseAddress(strings.TrimSpace(value), secret); ok {
					return id, true
				}
				continue
			}
			for _, entry := range list {
				if id, ok := ParseAddress(entry.Address, secret); ok {
					return id, true
				}
			}
		}
	}
	return "", false
}

// mail.Header keys are in canonical MIME form.
func textprotoKey(key string) string {
	parts := strings.Split(key, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "-")
}
