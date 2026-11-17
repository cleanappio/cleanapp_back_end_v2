package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"cleanapp-common/mailreply"
	"email-service/email"
	"email-service/models"

	"github.com/apex/log"
)

// Reply tracking, phase 1 of docs/architecture/notification-outcomes.md.
//
// Each company notification gets an opaque random ID. The ID is signed into
// the Reply-To address (info+r.<id>.<signature>@cleanapp.io), and after the
// transport accepts the email the ID is stored with the reports the email
// carried. A later reply, auto-acknowledgement or bounce that arrives at that
// address can then be matched to the exact send and to its reports.
//
// Nothing here may block or fail a notification: if tracking cannot be set
// up or recorded, the email is sent or kept exactly as before and the only
// loss is that replies to it cannot be matched.

// notificationMessageTablesSQL is run by RunMigrations. Both statements are
// idempotent. The tables are append-only: rows are written once per accepted
// send and never updated.
var notificationMessageTablesSQL = []string{
	`CREATE TABLE IF NOT EXISTS notification_messages (
		notification_id CHAR(16) NOT NULL PRIMARY KEY,
		provider_message_id VARCHAR(255) NOT NULL DEFAULT '',
		recipient_email VARCHAR(320) NOT NULL,
		company_key VARCHAR(64) NULL,
		thread_key VARCHAR(64) NULL,
		sent_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_notification_provider_message (provider_message_id),
		INDEX idx_notification_recipient_sent (recipient_email(191), sent_at),
		INDEX idx_notification_thread (thread_key, sent_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS notification_message_reports (
		notification_id CHAR(16) NOT NULL,
		seq INT NOT NULL,
		PRIMARY KEY (notification_id, seq),
		INDEX idx_notification_report_seq (seq)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
}

// prepareReplyTracking sets summary.ReplyTo to a signed address for a new
// notification ID and returns the ID. It returns "" and leaves the summary
// untouched when reply tracking is off or the address cannot be built.
func (s *EmailService) prepareReplyTracking(summary *models.BrandReportSummary) string {
	if !s.config.ReplyTrackingEnabled {
		return ""
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		log.Warnf("Reply tracking skipped: %v", err)
		return ""
	}
	id := hex.EncodeToString(raw)
	address, err := mailreply.Address(s.config.ReplyMailbox, id, []byte(s.config.ReplyTokenSecret))
	if err != nil {
		log.Warnf("Reply tracking skipped: %v", err)
		return ""
	}
	summary.ReplyTo = address
	return id
}

// prepareViewLinks replaces each report link with a signed /r/<token> link
// (phase 2). It does nothing without a notification ID, so it can never be
// active without reply tracking. A report whose token cannot be built keeps
// its plain public link.
func (s *EmailService) prepareViewLinks(summary *models.BrandReportSummary, id string) {
	if id == "" || !s.config.ViewTrackingEnabled || !strings.HasPrefix(s.config.ViewLinkBaseURL, "https://") {
		return
	}
	if !s.viewLinksResolve(context.Background()) {
		return
	}
	links := make(map[int64]string, len(summary.ReportSeqs))
	for _, seq := range summary.ReportSeqs {
		token, err := mailreply.LinkToken(id, seq, []byte(s.config.ReplyTokenSecret))
		if err != nil {
			log.Warnf("View link skipped for one report: %v", err)
			continue
		}
		links[seq] = s.config.ViewLinkBaseURL + token
	}
	summary.ReportLinks = links
}

// recordNotificationMessage stores which reports an accepted email carried.
// Failures are logged with enough detail to rebuild the row by hand and are
// otherwise ignored: the delivery ledger, not this table, is the record that
// the email was sent.
func (s *EmailService) recordNotificationMessage(ctx context.Context, id, companyKey, recipient string, result email.CustomEmailSendResult, seqs []int64) {
	if id == "" || result.Status != "sent" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO notification_messages (notification_id, provider_message_id, recipient_email, company_key, sent_at) VALUES (?, ?, ?, ?, ?)`,
		id, result.ProviderMessageID, recipient, companyKey, result.SentAt); err != nil {
		log.Errorf("notification_message_recovery id=%s company=%s provider_message_id=%s seqs=%v error=%v", id, companyKey, result.ProviderMessageID, seqs, err)
		return
	}
	for _, seq := range seqs {
		if _, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO notification_message_reports (notification_id, seq) VALUES (?, ?)`, id, seq); err != nil {
			log.Errorf("notification_message_recovery id=%s seq=%d error=%v", id, seq, err)
		}
	}
}

// viewLinkCheck remembers whether report-listener has been seen to resolve a
// link signed by this service.
type viewLinkCheck struct {
	mu        sync.Mutex
	resolved  bool
	lastTried time.Time
}

const viewLinkCheckInterval = 10 * time.Minute

// viewLinksResolve reports whether tracked links may be used. A tracked link
// that report-listener cannot verify sends the recipient to the home page
// instead of the report, so nothing is assumed: this asks report-listener to
// resolve a link for a report an earlier notification carried, and tracked
// links stay off until one resolves to a report page. Until then, and with
// no earlier notification to test with, emails keep their plain links. It
// re-tries at most every viewLinkCheckInterval and, once true, stays true.
//
// The request is not recorded as a view: report-listener ignores this
// client's user agent.
func (s *EmailService) viewLinksResolve(ctx context.Context) bool {
	s.viewLinks.mu.Lock()
	defer s.viewLinks.mu.Unlock()
	if s.viewLinks.resolved {
		return true
	}
	if !s.viewLinks.lastTried.IsZero() && time.Since(s.viewLinks.lastTried) < viewLinkCheckInterval {
		return false
	}
	s.viewLinks.lastTried = time.Now()
	if s.config.ReportListenerURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.notification_id, mr.seq
		FROM notification_messages m
		JOIN notification_message_reports mr ON mr.notification_id = m.notification_id
		ORDER BY m.sent_at DESC LIMIT 5`)
	if err != nil {
		log.Warnf("View link check skipped: %v", err)
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			return false
		}
		token, err := mailreply.LinkToken(id, seq, []byte(s.config.ReplyTokenSecret))
		if err != nil {
			continue
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.ReportListenerURL+"/api/v3/r/"+token, nil)
		if err != nil {
			return false
		}
		response, err := companyReaderClient.Do(request) // does not follow redirects
		if err != nil {
			log.Warnf("View link check failed: %v", err)
			return false
		}
		response.Body.Close()
		// A verified link for a public report redirects to its report page; an
		// unverified one, or a report no longer public, goes to the home page.
		if response.StatusCode == http.StatusFound && strings.Contains(response.Header.Get("Location"), "/report/") {
			s.viewLinks.resolved = true
			log.Infof("View links verified against report-listener; tracked links are on")
			return true
		}
	}
	return false
}
