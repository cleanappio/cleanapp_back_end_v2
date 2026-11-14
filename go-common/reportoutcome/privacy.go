package reportoutcome

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	emailPattern  = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)+`)
	secretPattern = regexp.MustCompile(`(?i)(?:\bBearer\s+[a-z0-9._~+/=-]{12,}|\b(?:sk-[a-z0-9_-]{16,}|gh[pousr]_[a-z0-9]{20,}|AKIA[A-Z0-9]{16})\b|\b(?:password|api[_ -]?key|access[_ -]?token|secret)\s*[:=]\s*[^\s,;]{4,})`)
	phonePattern  = regexp.MustCompile(`(?:\+[1-9][0-9 ()-]{7,}[0-9]|\([0-9]{3}\)[ -]?[0-9]{3}[ -]?[0-9]{4}|\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b)`)
	cardPattern   = regexp.MustCompile(`\b[0-9][0-9 -]{11,25}[0-9]\b`)
	urlPattern    = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
)

// PrivacyHold checks the complete text projection, not only a generated
// summary. It never clears declared restrictions and never claims to sanitize
// images. No provider calls, URLs or images are fetched here.
func PrivacyHold(reportJSON []byte, imageBytes int64, declaredPII, declaredRedaction bool, text ...string) string {
	if declaredPII {
		return "declared_personal_information"
	}
	if declaredRedaction {
		return "redaction_required"
	}
	if imageBytes > 0 {
		return "image_privacy_review_required"
	}
	if len(reportJSON) > 65536 {
		return "public_projection_too_large"
	}
	var report any
	if len(reportJSON) > 0 && json.Unmarshal(reportJSON, &report) != nil {
		return "invalid_public_projection"
	}
	budget := 65536
	var inspect func(any) string
	inspect = func(value any) string {
		switch item := value.(type) {
		case string:
			budget -= len(item)
			if budget < 0 {
				return "public_projection_too_large"
			}
			return sensitiveText(item)
		case map[string]any:
			keys := make([]string, 0, len(item))
			for key := range item {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child := item[key]
				if reason := inspect(key); reason != "" {
					return reason
				}
				if explicitSensitiveKey(key, child) {
					return "sensitive_credentials_detected"
				}
				if key == "evidence_bundle" {
					if items, ok := child.([]any); ok {
						for _, entry := range items {
							media, _ := entry.(map[string]any)
							kind, _ := media["type"].(string)
							mime, _ := media["mime_type"].(string)
							textLink := kind == "url" || kind == "link" || kind == "text" || kind == "citation" || kind == "source" || kind == "quote"
							if !textLink || (mime != "" && !strings.HasPrefix(mime, "text/") && mime != "application/json") {
								return "media_privacy_review_required"
							}
							if strings.Contains(strings.ToLower(kind), "image") || strings.Contains(strings.ToLower(kind), "photo") ||
								strings.Contains(strings.ToLower(kind), "video") || strings.Contains(strings.ToLower(kind), "audio") ||
								strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/") || strings.HasPrefix(mime, "audio/") {
								return "media_privacy_review_required"
							}
						}
					}
				}
				if reason := inspect(child); reason != "" {
					return reason
				}
			}
		case []any:
			for _, child := range item {
				if reason := inspect(child); reason != "" {
					return reason
				}
			}
		}
		return ""
	}
	for _, item := range text {
		if reason := inspect(item); reason != "" {
			return reason
		}
	}
	return inspect(report)
}

func explicitSensitiveKey(key string, value any) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key))
	switch normalized {
	case "password", "apikey", "accesstoken", "refreshtoken", "authorization", "privatekey", "clientsecret", "secret", "token", "trackingtoken", "drafttoken", "receipttoken":
	default:
		return false
	}
	return value != nil && value != false && value != ""
}

func sensitiveText(text string) string {
	if !utf8.ValidString(text) {
		return "invalid_public_projection"
	}
	if secretPattern.MatchString(text) {
		return "sensitive_credentials_detected"
	}
	if emailPattern.MatchString(text) || phonePattern.MatchString(text) {
		return "personal_contact_detected"
	}
	for _, candidate := range cardPattern.FindAllString(text, -1) {
		if paymentNumber(candidate) {
			return "payment_information_detected"
		}
	}
	for _, raw := range urlPattern.FindAllString(text, -1) {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User != nil {
			return "sensitive_url_detected"
		}
		if address := net.ParseIP(parsed.Hostname()); address != nil &&
			(address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast()) {
			return "private_network_url_detected"
		}
		parameters := parsed.Query()
		if fragment, err := url.ParseQuery(strings.TrimPrefix(parsed.Fragment, "?")); err == nil {
			for key, values := range fragment {
				parameters[key] = values
			}
		}
		for key := range parameters {
			switch strings.ToLower(key) {
			case "token", "key", "api_key", "apikey", "password", "secret", "access_token", "authorization", "signature", "track", "tracking_token", "draft_token", "receipt_token":
				return "sensitive_url_detected"
			}
		}
	}
	return ""
}

func paymentNumber(value string) bool {
	var digits []int
	for _, char := range value {
		if char >= '0' && char <= '9' {
			digits = append(digits, int(char-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for index := len(digits) - 1; index >= 0; index-- {
		digit := digits[index]
		if (len(digits)-1-index)%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}
