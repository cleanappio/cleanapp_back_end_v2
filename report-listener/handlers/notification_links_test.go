package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"report-listener/config"

	"github.com/gin-gonic/gin"
)

func TestNotificationLinkLikelyHuman(t *testing.T) {
	for agent, want := range map[string]bool{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15": true,
		"": false,
		"Mozilla/5.0 (compatible; Googlebot/2.1)":      false,
		"Slackbot-LinkExpanding 1.0":                   false,
		"Mozilla/5.0 Proofpoint URL Defense":           false,
		"curl/8.4.0":                                   false,
		"Mozilla/5.0 HeadlessChrome/120.0":             false,
		"Mozilla/5.0 (Windows NT 10.0) Mimecast agent": false,
	} {
		if got := notificationLinkLikelyHuman(http.MethodGet, agent); got != want {
			t.Errorf("%q: got %v", agent, got)
		}
	}
	if notificationLinkLikelyHuman(http.MethodHead, "Mozilla/5.0 Safari") {
		t.Error("HEAD request counted as a person")
	}
}

// An invalid or unverifiable token must redirect to the home page without
// touching the database (h.db is nil here: any database call would panic).
func TestFollowNotificationLinkInvalidTokenRedirectsHomeWithoutDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, secret := range map[string]string{"no secret configured": "", "secret configured": strings.Repeat("k", 32)} {
		t.Run(name, func(t *testing.T) {
			h := &Handlers{cfg: &config.Config{ReplyTokenSecret: secret}}
			router := gin.New()
			router.GET("/r/:token", h.FollowNotificationLink)
			for _, token := range []string{"x", "0123456789abcdef.a.aaaaaaaaaaaaaaaa", "evil.example.com"} {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/r/"+token, nil)
				req.Header.Set("User-Agent", "Mozilla/5.0 Safari")
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != notificationLinkFallback {
					t.Fatalf("token %q: status=%d location=%q", token, rec.Code, rec.Header().Get("Location"))
				}
				if rec.Header().Get("Set-Cookie") != "" || rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("token %q: unexpected headers %v", token, rec.Header())
				}
			}
		})
	}
}
