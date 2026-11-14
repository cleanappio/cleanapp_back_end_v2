package reportoutcome

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	emailPattern  = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)+`)
	secretPattern = regexp.MustCompile(`(?i)(?:\bBearer\s+[a-z0-9._~+/=-]{12,}|\b(?:sk-[a-z0-9_-]{16,}|gh[pousr]_[a-z0-9]{20,}|AKIA[A-Z0-9]{16})\b|\b(?:password|api[_ -]?key|access[_ -]?token|secret)\s*[:=]\s*[^\s,;]{4,})`)
	phonePattern  = regexp.MustCompile(`(?:\+[1-9][0-9 ()-]{7,}[0-9]|\([0-9]{3}\)[ -]?[0-9]{3}[ -]?[0-9]{4}|\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b)`)
	cardPattern   = regexp.MustCompile(`\b[0-9][0-9 -]{11,25}[0-9]\b`)
	urlPattern    = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
	xStatusPath   = regexp.MustCompile(`^/(?:[A-Za-z0-9_]{1,15}/status|i/web/status)/([1-9][0-9]{18})/?$`)
	panContext    = regexp.MustCompile(`(?i)\b(?:pan|(?:credit|debit|payment|bank)[ _-]+card|card[ _-]*(?:number|no)|primary[ _-]+account[ _-]+number)\b(?:\s*\(PAN\))?\s*(?:[:=#]|\bis\b|\s+https?://)`)
	panAfterURL   = regexp.MustCompile(`(?i)^is\s+(?:(?:my|the|a)\s+)?(?:pan|(?:credit|debit|payment|bank)[ _-]+card(?:[ _-]+number)?|card[ _-]*(?:number|no)|primary[ _-]+account[ _-]+number)\b`)
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
	now := time.Now().UTC()
	var inspect func(any, bool) string
	inspect = func(value any, paymentContext bool) string {
		switch item := value.(type) {
		case string:
			budget -= len(item)
			if budget < 0 {
				return "public_projection_too_large"
			}
			return sensitiveText(item, now, paymentContext)
		case map[string]any:
			keys := make([]string, 0, len(item))
			for key := range item {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child := item[key]
				if reason := inspect(key, paymentContext); reason != "" {
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
				if reason := inspect(child, paymentContext || paymentNumberKey(key)); reason != "" {
					return reason
				}
			}
		case []any:
			for _, child := range item {
				if reason := inspect(child, paymentContext); reason != "" {
					return reason
				}
			}
		}
		return ""
	}
	for _, item := range text {
		if reason := inspect(item, false); reason != "" {
			return reason
		}
	}
	return inspect(report, false)
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

func paymentNumberKey(key string) bool {
	switch strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key)) {
	case "pan", "cardnumber", "creditcardnumber", "debitcardnumber", "paymentcardnumber", "primaryaccountnumber":
		return true
	}
	return false
}

func sensitiveText(text string, now time.Time, paymentContext bool) string {
	if !utf8.ValidString(text) {
		return "invalid_public_projection"
	}
	if secretPattern.MatchString(text) {
		return "sensitive_credentials_detected"
	}
	if emailPattern.MatchString(text) || phonePattern.MatchString(text) {
		return "personal_contact_detected"
	}
	var paymentText []byte
	for _, span := range urlPattern.FindAllStringIndex(text, -1) {
		// A prose link can end in ").". Ignore closing prose punctuation for
		// parsing; retained text is unchanged and URL privacy checks still run.
		raw := strings.TrimRight(text[span[0]:span[1]], ".,;:!)]}")
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User != nil {
			return "sensitive_url_detected"
		}
		if reason := sensitiveURL(parsed, 0); reason != "" {
			return reason
		}
		if !paymentContext && !panContext.MatchString(text) && !panAfterURL.MatchString(strings.TrimLeft(text[span[1]:], " \r\n\t.,;:!)]}")) {
			if start, end, ok := publicXStatusID(parsed, now); ok {
				if paymentText == nil {
					paymentText = []byte(text)
				}
				// Mask only this exact path segment. Other numbers, including
				// the same ID outside the URL or PANs in its query, still scan.
				for i := span[0] + start; i < span[0]+end; i++ {
					paymentText[i] = 'x'
				}
			}
		}
	}
	if paymentText != nil {
		text = string(paymentText)
	}
	for _, candidate := range cardPattern.FindAllString(text, -1) {
		if paymentNumber(candidate) {
			return "payment_information_detected"
		}
	}
	return ""
}

// Validate all URL components before considering an ID exception. Decoded nested
// URL parameters cannot hide credentials behind an otherwise recognized host.
func sensitiveURL(parsed *url.URL, depth int) string {
	if parsed.User != nil {
		return "sensitive_url_detected"
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "private_network_url_detected"
	}
	// IPv6 zone identifiers do not make a private/link-local address public.
	addressHost, _, _ := strings.Cut(host, "%")
	if address := net.ParseIP(addressHost); address != nil &&
		(address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast()) {
		return "private_network_url_detected"
	}
	for _, query := range []string{parsed.RawQuery, strings.TrimPrefix(parsed.Fragment, "?")} {
		parameters, err := url.ParseQuery(query)
		if err != nil {
			return "sensitive_url_detected"
		}
		for key := range parameters {
			switch strings.ToLower(key) {
			case "token", "key", "api_key", "apikey", "password", "secret", "access_token", "authorization", "signature", "track", "tracking_token", "draft_token", "receipt_token":
				return "sensitive_url_detected"
			}
		}
		for _, values := range parameters {
			for _, value := range values {
				for _, raw := range urlPattern.FindAllString(value, -1) {
					if depth >= 2 {
						return "sensitive_url_detected"
					}
					nested, err := url.Parse(strings.TrimRight(raw, ".,;:!)]}"))
					if err != nil {
						return "sensitive_url_detected"
					}
					if reason := sensitiveURL(nested, depth+1); reason != "" {
						return reason
					}
				}
			}
		}
	}
	return ""
}

// Recognize only the observed modern 19-digit public status-ID namespace, not
// arbitrary numbers on trusted hosts. This does not verify a post's existence,
// visibility, author or contents. Keep shorter IDs conservative to preserve the
// existing 13–18-digit PAN checks. Layout: twitter-archive/snowflake IdWorker.
func publicXStatusID(parsed *url.URL, now time.Time) (start, end int, ok bool) {
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Port() != "" || parsed.Opaque != "" || parsed.EscapedPath() != parsed.Path {
		return 0, 0, false
	}
	switch strings.ToLower(parsed.Host) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
	default:
		return 0, 0, false
	}
	match := xStatusPath.FindStringSubmatchIndex(parsed.Path)
	if match == nil {
		return 0, 0, false
	}
	id, err := strconv.ParseUint(parsed.Path[match[2]:match[3]], 10, 63)
	if err != nil {
		return 0, 0, false
	}
	const twitterEpochMillis int64 = 1288834974657
	created := time.UnixMilli(int64(id>>22) + twitterEpochMillis)
	if created.After(now.Add(5 * time.Minute)) {
		return 0, 0, false
	}
	prefix := len(parsed.Scheme) + len("://") + len(parsed.Host)
	return prefix + match[2], prefix + match[3], true
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
