package handlers

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cleanapp-common/mailreply"

	"github.com/gin-gonic/gin"
)

// Tracked report links, phase 2 of
// docs/architecture/notification-outcomes.md.
//
// GET /r/<token> verifies the token, records that the recipient followed a
// link in a notification email, and redirects to the public report page.
//
// Rules this handler keeps:
//   - It always redirects. Any failure (bad token, unknown notification,
//     private report, database error) goes to the home page and records
//     nothing, so a link in an email is never a dead end and never an oracle
//     for which reports or notifications exist.
//   - It sets no cookie and stores no IP address or user agent.
//   - The redirect target is built from a public report ID read from the
//     database, never from the request, so it cannot be an open redirect.

const notificationLinkFallback = "https://www.cleanapp.io/"

// Mail gateways and chat apps fetch links to scan or preview them. Those
// fetches are not a person reading the report.
var notificationLinkScannerAgents = []string{
	"bot", "crawler", "spider", "preview", "scanner", "proofpoint", "mimecast", "barracuda",
	"safelinks", "urldefense", "slackbot", "facebookexternalhit", "whatsapp", "telegram",
	"curl", "wget", "python-requests", "go-http-client", "headless",
}

func notificationLinkLikelyHuman(method, userAgent string) bool {
	if method != http.MethodGet {
		return false
	}
	agent := strings.ToLower(strings.TrimSpace(userAgent))
	if agent == "" {
		return false
	}
	for _, marker := range notificationLinkScannerAgents {
		if strings.Contains(agent, marker) {
			return false
		}
	}
	return true
}

func (h *Handlers) FollowNotificationLink(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex")
	destination := notificationLinkFallback

	id, seq, ok := mailreply.ParseLinkToken(c.Param("token"), []byte(h.cfg.ReplyTokenSecret))
	if ok {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		target, err := h.db.RecordNotificationView(ctx, id, seq, notificationLinkLikelyHuman(c.Request.Method, c.Request.UserAgent()), time.Now().UTC())
		if err != nil {
			// The token is not logged: it is a capability.
			log.Printf("warn: notification link: %v", err)
		}
		if target != nil {
			domain := "physical"
			if target.Digital {
				domain = "digital"
			}
			destination = "https://www.cleanapp.io/" + domain + "/report/" + url.PathEscape(target.PublicID)
		}
	}
	c.Redirect(http.StatusFound, destination)
}
