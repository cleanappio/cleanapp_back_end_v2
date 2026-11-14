package reportoutcome

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Generated locally from a fixture timestamp/sequence; never a retained source
// identifier. Deliberately select a Snowflake that also passes the Luhn check.
func fixtureLuhnSnowflake(t *testing.T, created time.Time) string {
	t.Helper()
	const epoch int64 = 1288834974657
	base := uint64(created.UnixMilli()-epoch) << 22
	for sequence := uint64(0); sequence < 100; sequence++ {
		id := strconv.FormatUint(base|sequence, 10)
		if len(id) == 19 && paymentNumber(id) {
			return id
		}
	}
	t.Fatal("fixture did not produce a 19-digit Luhn-valid Snowflake")
	return ""
}

func fixtureStatusURL(t *testing.T) string {
	return "https://x.com/fixture_user/status/" + fixtureLuhnSnowflake(t, time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))
}

func TestPrivacyAllowsOnlyPublicStatusIDPathDigits(t *testing.T) {
	link := fixtureStatusURL(t)
	for _, text := range []string{
		link,
		"The reported error is described here (" + link + ").",
		"See " + link + "), then retry Save.",
		link + "/",
		link + "?s=20&t=ordinary-reference",
		link + "#context",
		strings.Replace(link, "https://x.com", "HTTPS://X.COM", 1),
		strings.Replace(link, "x.com", "www.x.com", 1),
		strings.Replace(link, "x.com", "twitter.com", 1),
		strings.Replace(link, "x.com", "www.twitter.com", 1),
		strings.Replace(link, "fixture_user/status", "i/web/status", 1),
		strings.Replace(link, "fixture_user", "card_number", 1),
	} {
		if reason := PrivacyHold(nil, 0, false, false, text); reason != "" {
			t.Fatalf("public status namespace held: %s", reason)
		}
	}
	// The same observation appears in immutable Wire JSON and retained report
	// description. Both must pass, rather than fixing only a dedicated URL field.
	description := "The upload failed; source (" + link + ")."
	raw, _ := json.Marshal(map[string]any{"description": description, "digital_context": map[string]any{"url": link}})
	if reason := PrivacyHold(raw, 0, false, false, description); reason != "" {
		t.Fatalf("retained description still held: %s", reason)
	}
}

func TestPrivacyDoesNotExemptStandaloneOrUnknownNamespaceIDs(t *testing.T) {
	link := fixtureStatusURL(t)
	id := link[strings.LastIndex(link, "/")+1:]
	for _, text := range []string{
		id,
		link + " " + id,
		id + " " + link,
		link + "?reference=" + id,
		strings.Replace(link, "x.com", "example.invalid", 1),
		strings.Replace(link, "x.com", "x.com.example.invalid", 1),
		strings.Replace(link, "x.com", "subdomain.x.com", 1),
		strings.Replace(link, "x.com", "x.com.", 1),
		strings.Replace(link, "https:", "http:", 1),
		strings.Replace(link, "x.com", "x.com:443", 1),
		strings.Replace(link, "fixture_user", "sixteen_charname", 1) + "/photo/1",
		strings.Replace(link, "fixture_user", "%66ixture_user", 1),
		strings.Replace(link, "/status/", "/messages/", 1),
		"https://x.com/fixture_user/status/42?post=" + id,
	} {
		if reason := PrivacyHold(nil, 0, false, false, text); reason != "payment_information_detected" {
			t.Fatalf("numeric value outside approved path was not held: %q", reason)
		}
	}
}

func TestPrivacyKeepsPANsSensitiveBesideOrInsideStatusLinks(t *testing.T) {
	link := fixtureStatusURL(t)
	for _, pan := range []string{
		"4111111111111111", "5555555555554444", "378282246310005",
		"6011111111111117", "4000000000000000006",
	} {
		if !paymentNumber(pan) {
			t.Fatal("invalid test PAN fixture")
		}
		for _, text := range []string{
			pan, link + " " + pan, pan + " " + link,
			"https://x.com/fixture_user/status/" + pan,
			link + "?reference=" + pan,
		} {
			if reason := PrivacyHold(nil, 0, false, false, text); reason != "payment_information_detected" {
				t.Fatalf("PAN was cleared by a source link: %q", reason)
			}
		}
	}
}

func TestPrivacyPaymentContextCannotUseTheSourceIDException(t *testing.T) {
	link := fixtureStatusURL(t)
	for _, prefix := range []string{"Card number: ", "PAN is ", "My credit card: ", "Credit card number (PAN): ", "Primary account number = ", "card_number "} {
		if reason := PrivacyHold(nil, 0, false, false, prefix+link); reason != "payment_information_detected" {
			t.Fatalf("explicit payment label cleared: %q", reason)
		}
	}
	if reason := PrivacyHold(nil, 0, false, false, link+" is my card number."); reason != "payment_information_detected" {
		t.Fatalf("payment label after a URL cleared: %q", reason)
	}
	for _, key := range []string{"pan", "card_number", "credit-card-number", "primary_account_number"} {
		for _, value := range []any{link, []any{link}, map[string]any{"value": link}} {
			raw, _ := json.Marshal(map[string]any{"digital_context": map[string]any{key: value}})
			if reason := PrivacyHold(raw, 0, false, false); reason != "payment_information_detected" {
				t.Fatalf("explicit nested payment field cleared: %q", reason)
			}
		}
	}
}

func TestPrivacyStatusLinksRetainURLAndDeclaredRestrictions(t *testing.T) {
	link := fixtureStatusURL(t)
	for _, text := range []string{
		link + "?token=fixture-private-capability",
		link + "#tracking_token=fixture-private-capability",
		link + "?draft_token=fixture-private-capability#context",
		link + "?token=%ZZ",
		link + "?next=" + url.QueryEscape("https://example.invalid/?access_token=fixture-private-capability"),
		link + "#next=" + url.QueryEscape("https://example.invalid/#track=fixture-private-capability"),
		link + "?next=" + url.QueryEscape("http://127.0.0.1/private"),
		link + "?next=" + url.QueryEscape("http://localhost/private"),
		link + " http://[fe80::1%25lo0]/private",
		strings.Replace(link, "x.com", "user:fixture-password@x.com", 1),
		strings.Replace(link, "x.com", "x.com@evil.invalid", 1),
		strings.Replace(link, "x.com", "127.0.0.1", 1),
		strings.Replace(link, "x.com", "[::1]", 1),
		link + " fixture@example.invalid",
		link + " password=fixture-private-value",
	} {
		// Exercise nested retained context, not just a top-level URL parameter.
		raw, _ := json.Marshal(map[string]any{"digital_context": map[string]any{"source": text}})
		if reason := PrivacyHold(raw, 0, false, false); reason == "" {
			t.Fatal("source URL bypassed a privacy restriction")
		}
	}
	for _, tc := range []struct {
		pii, redaction bool
		image          int64
		reason         string
	}{
		{true, false, 0, "declared_personal_information"},
		{false, true, 0, "redaction_required"},
		{false, false, 1, "image_privacy_review_required"},
	} {
		if got := PrivacyHold(nil, tc.image, tc.pii, tc.redaction, link); got != tc.reason {
			t.Fatalf("declared restriction changed: %q", got)
		}
	}
}

func TestPublicStatusSnowflakeBoundsUseAnExplicitClock(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for _, delta := range []time.Duration{-24 * time.Hour, 4 * time.Minute, 6 * time.Minute} {
		id := fixtureLuhnSnowflake(t, now.Add(delta))
		raw := "https://x.com/fixture_user/status/" + id
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		start, end, ok := publicXStatusID(parsed, now)
		if ok != (delta <= 5*time.Minute) {
			t.Fatalf("unexpected Snowflake clock-skew decision for %s", delta)
		}
		if ok && raw[start:end] != id {
			t.Fatal("exception offsets extend beyond the ID path segment")
		}
	}
	for _, id := range []string{"0", "0180000000000000000", "18446744073709551615", "9223372036854775807", "4000000000000000006"} {
		parsed, _ := url.Parse("https://x.com/fixture_user/status/" + id)
		if _, _, ok := publicXStatusID(parsed, now); ok {
			t.Fatal("noncanonical, out-of-range or future ID was exempted")
		}
	}
}

func TestPublicStatusIDExceptionStillRequiresAnalysisAndReview(t *testing.T) {
	input := automaticInput()
	input.PrivacyHoldReason = PrivacyHold(nil, 0, false, false, "Observed an upload failure ("+fixtureStatusURL(t)+").")
	outcome := Review(input, Analysis{}, time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	if input.PrivacyHoldReason != "" || outcome.Decision.State != Pending || outcome.Processing.Status != "pending_analysis" || outcome.Final || outcome.Publication.Status != "private" {
		t.Fatal("recognizing an ID must only unblock normal analysis, never authorize publication")
	}
}
