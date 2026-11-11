package mailreply

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

// Link tokens make "viewed" a recorded fact. Each report link in a
// notification email carries a token naming the notification and the report:
//
//	https://www.cleanapp.io/r/<notification id>.<report seq, base 36>.<mac>
//
// The token says nothing about the reporter and grants nothing: following it
// only redirects to the public report page. The signature uses the same
// secret as reply addresses under a different label, so a reply signature
// can never be replayed as a link signature or the reverse.

func signLink(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("cleanapp-link-v1\x00" + payload))
	return strings.ToLower(b32.EncodeToString(mac.Sum(nil)[:10]))
}

// LinkToken returns the token for one report inside one notification.
func LinkToken(id string, seq int64, secret []byte) (string, error) {
	if len(secret) < 16 {
		return "", fmt.Errorf("link secret must be at least 16 bytes")
	}
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("notification id must be 8-32 lower-case letters or digits")
	}
	if seq <= 0 {
		return "", fmt.Errorf("report seq must be positive")
	}
	payload := id + "." + strconv.FormatInt(seq, 36)
	return payload + "." + signLink(secret, payload), nil
}

// ParseLinkToken verifies a token and returns the notification ID and report
// seq. ok is false for anything malformed, unsigned or signed with another
// secret; callers must then record nothing.
func ParseLinkToken(token string, secret []byte) (id string, seq int64, ok bool) {
	if len(secret) < 16 || len(token) > 96 {
		return "", 0, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || !idPattern.MatchString(parts[0]) {
		return "", 0, false
	}
	seq, err := strconv.ParseInt(parts[1], 36, 64)
	// Reject non-canonical spellings ("0a", "+a", "A") so one view has one token.
	if err != nil || seq <= 0 || strconv.FormatInt(seq, 36) != parts[1] {
		return "", 0, false
	}
	if !hmac.Equal([]byte(signLink(secret, parts[0]+"."+parts[1])), []byte(parts[2])) {
		return "", 0, false
	}
	return parts[0], seq, true
}
