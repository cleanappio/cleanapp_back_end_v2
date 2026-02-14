package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"email-service/config"
	"email-service/email"
	"email-service/models"

	"github.com/apex/log"
	_ "github.com/go-sql-driver/mysql"
	geojson "github.com/paulmach/go.geojson"
)

// EmailService handles the email sending logic
type EmailService struct {
	db     *sql.DB
	config *config.Config
	email  *email.EmailSender
}

const (
	retryReasonDailyLimit            = "daily_limit"
	retryReasonAllThrottled          = "all_throttled"
	retryReasonNoValidEmails         = "no_valid_emails"
	retryReasonAggregateFailure      = "aggregate_send_failed"
	retryReasonThrottleError         = "throttle_check_error"
	retryReasonAwaitContactDiscovery = "await_contact_discovery"
)

// isValidEmail checks if a string is a valid email address
func (s *EmailService) isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}

	// Updated regex to prevent consecutive dots and ensure proper email format
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(email) {
		return false
	}

	// Filter obvious placeholders
	lower := strings.ToLower(email)
	placeholders := []string{
		"test@", "example@", "sample@", "demo@",
		"noreply@", "no-reply@", "donotreply@",
		"admin@localhost", "user@localhost",
		"@example.com", "@test.com", "@localhost",
	}
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			return false
		}
	}

	return true
}

// isEmailOptedOut checks if an email address has opted out from receiving emails
func (s *EmailService) isEmailOptedOut(ctx context.Context, email string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM opted_out_emails 
		WHERE email = ?
	`, email).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check if email %s is opted out: %w", email, err)
	}

	return count > 0, nil
}

// isFirstTimeRecipient checks if this is the first email being sent to this recipient
func (s *EmailService) isFirstTimeRecipient(ctx context.Context, email string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM email_recipient_history 
		WHERE email = ?
	`, email).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check email history for %s: %w", email, err)
	}

	return count == 0, nil
}

// recordEmailSent records that an email was sent to a recipient
func (s *EmailService) recordEmailSent(ctx context.Context, email string) error {
	// Use INSERT ... ON DUPLICATE KEY UPDATE to handle both new and existing recipients
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO email_recipient_history (email, first_email_sent_at, last_email_sent_at, email_count) 
		VALUES (?, NOW(), NOW(), 1)
		ON DUPLICATE KEY UPDATE 
			last_email_sent_at = NOW(),
			email_count = email_count + 1
	`, email)

	if err != nil {
		return fmt.Errorf("failed to record email sent to %s: %w", email, err)
	}

	return nil
}

// getThrottleDecision checks whether this brand+email pair should be throttled and returns
// the earliest re-attempt time when throttled.
func (s *EmailService) getThrottleDecision(ctx context.Context, brandName, email string) (bool, *time.Time, error) {
	throttleDays := s.config.ThrottleDays
	if throttleDays <= 0 {
		throttleDays = 7 // Default fallback
	}

	var lastSentAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT last_sent_at FROM brand_email_throttle 
		WHERE brand_name = ? AND email = ?
	`, brandName, email).Scan(&lastSentAt)

	if err == sql.ErrNoRows {
		return false, nil, nil // Never sent to this pair, don't throttle
	}
	if err != nil {
		return false, nil, fmt.Errorf("failed to check throttle for brand %s, email %s: %w", brandName, email, err)
	}

	if lastSentAt.Valid {
		throttlePeriod := time.Duration(throttleDays) * 24 * time.Hour
		nextAttempt := lastSentAt.Time.Add(throttlePeriod)
		if time.Now().Before(nextAttempt) {
			return true, &nextAttempt, nil // Recently sent, throttle this email
		}
	}
	return false, nil, nil
}

// shouldThrottleEmail checks if we should throttle sending email to this brand+email pair.
func (s *EmailService) shouldThrottleEmail(ctx context.Context, brandName, email string) (bool, error) {
	throttled, _, err := s.getThrottleDecision(ctx, brandName, email)
	return throttled, err
}

// recordBrandEmailSent records that an email was sent for a specific brand
func (s *EmailService) recordBrandEmailSent(ctx context.Context, brandName, email string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO brand_email_throttle (brand_name, email, last_sent_at, email_count)
		VALUES (?, ?, NOW(), 1)
		ON DUPLICATE KEY UPDATE 
			last_sent_at = NOW(),
			email_count = email_count + 1
	`, brandName, email)

	if err != nil {
		return fmt.Errorf("failed to record brand email sent for %s to %s: %w", brandName, email, err)
	}
	return nil
}

// getDailyEmailCount returns the number of emails sent today for a specific brand
func (s *EmailService) getDailyEmailCount(ctx context.Context, brandName string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM brand_email_throttle 
		WHERE brand_name = ? 
		AND DATE(last_sent_at) = CURDATE()
	`, brandName).Scan(&count)

	if err != nil {
		return 0, fmt.Errorf("failed to get daily email count for brand %s: %w", brandName, err)
	}
	return count, nil
}

// NewEmailService creates a new email service
func NewEmailService(cfg *config.Config) (*EmailService, error) {
	// Connect to database
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection with exponential backoff retry
	var waitInterval time.Duration = 1 * time.Second
	for {
		if err := db.Ping(); err == nil {
			break // Connection successful
		}
		log.WithError(err).Warnf("Database connection failed, retrying in %v", waitInterval)
		time.Sleep(waitInterval)
		waitInterval *= 2 // Exponential backoff: 1s, 2s, 4s, 8s, ...
	}

	// Verify and create required tables
	if err := verifyAndCreateTables(db); err != nil {
		return nil, fmt.Errorf("failed to verify/create tables: %w", err)
	}

	// Create email sender
	emailSender := email.NewEmailSender(cfg)

	return &EmailService{
		db:     db,
		config: cfg,
		email:  emailSender,
	}, nil
}

// Close closes the database connection
func (s *EmailService) Close() error {
	return s.db.Close()
}

// ProcessReports polls for new reports and sends emails
func (s *EmailService) ProcessReports() error {
	ctx := context.Background()
	start := time.Now()
	log.Info("Polling cycle started: fetching unprocessed reports")

	// Get reports that haven't been processed for email sending
	reports, err := s.getUnprocessedReports(ctx)
	if err != nil {
		return fmt.Errorf("failed to get unprocessed reports: %w", err)
	}

	log.Infof("Found %d unprocessed reports (in %s)", len(reports), time.Since(start))

	reportsStart := time.Now()
	for _, report := range reports {
		if err := s.processReport(ctx, report); err != nil {
			log.Errorf("Failed to process report %d: %v", report.Seq, err)
			continue
		}
	}
	log.Infof("Finished processing %d reports (took %s)", len(reports), time.Since(reportsStart))
	return nil
}

// getUnprocessedReports gets reports that have been analyzed but haven't been sent emails for
func (s *EmailService) getUnprocessedReports(ctx context.Context) ([]models.Report, error) {
	qStart := time.Now()
	query := `
        SELECT r.seq, r.id, r.latitude, r.longitude, r.image, r.ts
        FROM reports r
        INNER JOIN report_analysis ra ON r.seq = ra.seq
        LEFT JOIN sent_reports_emails sre ON r.seq = sre.seq
		LEFT JOIN email_report_retry erry ON r.seq = erry.seq
        WHERE sre.seq IS NULL
        AND ra.language = 'en'
		AND (erry.seq IS NULL OR erry.next_attempt_at <= NOW())
        ORDER BY r.seq DESC
        LIMIT 500
    `

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Errorf("getUnprocessedReports query error (in %s): %v", time.Since(qStart), err)
		return nil, err
	}
	defer rows.Close()

	var reports []models.Report
	for rows.Next() {
		var report models.Report
		if err := rows.Scan(&report.Seq, &report.ID, &report.Latitude, &report.Longitude, &report.Image, &report.Timestamp); err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}

	log.Infof("getUnprocessedReports returned %d rows (in %s)", len(reports), time.Since(qStart))
	return reports, nil
}

// getUnprocessedBrandlessPhysicalReports returns physical reports that have no brand_name set.
// These are not picked up by aggregate brand notifications and would otherwise be dropped.
func (s *EmailService) getUnprocessedBrandlessPhysicalReports(ctx context.Context, limit int) ([]models.Report, error) {
	if limit <= 0 {
		limit = 5
	}
	qStart := time.Now()
	// Two-step lookup to avoid expensive joins against large blobs (reports.image) during candidate selection.
	//
	// Step 1: pick seqs from report_analysis (fast: ordered by PK desc + small limit).
	// Step 2: fetch report rows for those seqs (PK lookup).
	var seqs []int64
	seqRows, err := s.db.QueryContext(ctx, `
		SELECT ra.seq
		FROM report_analysis ra
		LEFT JOIN sent_reports_emails sre ON sre.seq = ra.seq
		LEFT JOIN email_report_retry erry ON erry.seq = ra.seq
		WHERE sre.seq IS NULL
		  AND ra.is_valid = TRUE
		  AND ra.classification = 'physical'
		  AND ra.language = 'en'
		  AND (ra.brand_name IS NULL OR ra.brand_name = '')
		  AND (erry.seq IS NULL OR erry.next_attempt_at <= NOW())
		ORDER BY ra.seq DESC
		LIMIT ?
	`, limit)
	if err != nil {
		log.Errorf("getUnprocessedBrandlessPhysicalReports step1 query error (in %s): %v", time.Since(qStart), err)
		return nil, err
	}
	for seqRows.Next() {
		var seq int64
		if err := seqRows.Scan(&seq); err != nil {
			seqRows.Close()
			return nil, err
		}
		seqs = append(seqs, seq)
	}
	seqRows.Close()

	if len(seqs) == 0 {
		log.Infof("getUnprocessedBrandlessPhysicalReports returned 0 rows (in %s)", time.Since(qStart))
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(seqs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(seqs))
	for _, s := range seqs {
		args = append(args, s)
	}

	query := fmt.Sprintf(`
		SELECT seq, id, latitude, longitude, image, ts
		FROM reports
		WHERE seq IN (%s)
		ORDER BY seq DESC
	`, placeholders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Errorf("getUnprocessedBrandlessPhysicalReports step2 query error (in %s): %v", time.Since(qStart), err)
		return nil, err
	}
	defer rows.Close()

	var reports []models.Report
	for rows.Next() {
		var report models.Report
		if err := rows.Scan(&report.Seq, &report.ID, &report.Latitude, &report.Longitude, &report.Image, &report.Timestamp); err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}

	log.Infof("getUnprocessedBrandlessPhysicalReports returned %d rows (in %s)", len(reports), time.Since(qStart))
	return reports, nil
}

// ProcessBrandlessPhysicalReports processes physical reports with empty brand_name.
// Bounded by `limit` to avoid bursty catch-up sends.
func (s *EmailService) ProcessBrandlessPhysicalReports(limit int) error {
	ctx := context.Background()
	start := time.Now()
	log.Info("Brandless physical cycle started: fetching unprocessed physical reports with empty brand_name")

	reports, err := s.getUnprocessedBrandlessPhysicalReports(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to get brandless physical reports: %w", err)
	}

	log.Infof("Found %d brandless physical reports (in %s)", len(reports), time.Since(start))

	processed := 0
	for _, report := range reports {
		// Avoid futile discovery loops for missing coordinates when there are no inferred emails.
		if report.Latitude == 0 && report.Longitude == 0 {
			analysis, aerr := s.getReportAnalysis(ctx, report.Seq)
			if aerr != nil {
				log.Warnf("Brandless physical: failed to load analysis for seq %d: %v", report.Seq, aerr)
				continue
			}
			if strings.TrimSpace(analysis.InferredContactEmails) == "" {
				log.Infof("Brandless physical: seq %d has (0,0) coordinates and no inferred contacts; marking processed", report.Seq)
				if err := s.markReportAsProcessed(ctx, report.Seq); err != nil {
					log.Warnf("Brandless physical: failed to mark seq %d processed: %v", report.Seq, err)
					continue
				}
				processed++
				continue
			}
		}

		if s.config.DryRun {
			log.Infof("🔒 DRY RUN: Would process brandless physical seq=%d; marking processed without sending", report.Seq)
			if err := s.markReportAsProcessed(ctx, report.Seq); err != nil {
				log.Warnf("Brandless physical: failed to mark seq %d processed in dry-run: %v", report.Seq, err)
				continue
			}
			processed++
			continue
		}

		if err := s.processReport(ctx, report); err != nil {
			log.Errorf("Brandless physical: failed to process report %d: %v", report.Seq, err)
			continue
		}
		processed++
	}

	log.Infof("Brandless physical cycle complete: %d/%d processed (took %s)", processed, len(reports), time.Since(start))
	return nil
}

// getUnprocessedReportsByBrand gets unprocessed reports grouped by brand for aggregate notifications
func (s *EmailService) getUnprocessedReportsByBrand(ctx context.Context) ([]models.BrandReportSummary, error) {
	qStart := time.Now()

	// Query to get brand summaries with new report counts and seqs
	query := `
		SELECT 
			ra.brand_name,
			COALESCE(ra.brand_display_name, ra.brand_name) as brand_display_name,
			COUNT(*) as new_count,
			(SELECT COUNT(*) FROM report_analysis ra2 WHERE ra2.brand_name = ra.brand_name AND ra2.language = 'en') as total_count,
			GROUP_CONCAT(r.seq ORDER BY r.seq DESC) as seqs,
			MAX(r.seq) as latest_seq,
			MAX(ra.classification) as classification,
			MAX(ra.inferred_contact_emails) as inferred_contact_emails
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN sent_reports_emails sre ON r.seq = sre.seq
		LEFT JOIN email_report_retry erry ON r.seq = erry.seq
		WHERE sre.seq IS NULL
		  AND ra.brand_name != ''
		  AND ra.language = 'en'
		  AND (erry.seq IS NULL OR erry.next_attempt_at <= NOW())
		GROUP BY ra.brand_name, ra.brand_display_name
		ORDER BY new_count DESC
		LIMIT 100
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Errorf("getUnprocessedReportsByBrand query error (in %s): %v", time.Since(qStart), err)
		return nil, err
	}
	defer rows.Close()

	var summaries []models.BrandReportSummary
	for rows.Next() {
		var summary models.BrandReportSummary
		var seqsStr string
		var inferredEmails sql.NullString

		if err := rows.Scan(
			&summary.BrandName,
			&summary.BrandDisplayName,
			&summary.NewReportCount,
			&summary.TotalReportCount,
			&seqsStr,
			&summary.LatestReportSeq,
			&summary.Classification,
			&inferredEmails,
		); err != nil {
			return nil, err
		}

		// Parse the comma-separated seqs
		if seqsStr != "" {
			seqParts := strings.Split(seqsStr, ",")
			for _, seqPart := range seqParts {
				if seq, err := strconv.ParseInt(strings.TrimSpace(seqPart), 10, 64); err == nil {
					summary.ReportSeqs = append(summary.ReportSeqs, seq)
				}
			}
		}

		summary.InferredContactEmails = inferredEmails.String
		summaries = append(summaries, summary)
	}

	log.Infof("getUnprocessedReportsByBrand returned %d brands (in %s)", len(summaries), time.Since(qStart))
	return summaries, nil
}

// ProcessBrandNotifications processes reports grouped by brand, sending ONE aggregate email per brand
func (s *EmailService) ProcessBrandNotifications() error {
	ctx := context.Background()
	start := time.Now()

	// Log if dry-run mode is enabled
	if s.config.DryRun {
		log.Info("🔒 DRY RUN MODE ENABLED - emails will be logged but not sent")
	}

	log.Info("Aggregate notification cycle started: fetching brands with new reports")

	// Get brands with unprocessed reports
	brandSummaries, err := s.getUnprocessedReportsByBrand(ctx)
	if err != nil {
		return fmt.Errorf("failed to get brand summaries: %w", err)
	}

	log.Infof("Found %d brands with new reports (in %s)", len(brandSummaries), time.Since(start))

	var processedBrands, skippedBrands, emailsSent, dailyLimitHits, scheduledRetries int

	for _, summary := range brandSummaries {
		// SAFETY CHECK: Daily email limit per brand
		dailyCount, err := s.getDailyEmailCount(ctx, summary.BrandName)
		if err != nil {
			log.Warnf("Failed to get daily email count for %s: %v", summary.BrandName, err)
		} else if dailyCount >= s.config.MaxDailyEmailsPerBrand {
			log.Warnf("⚠️ DAILY LIMIT REACHED for brand %s (%d/%d), skipping",
				summary.BrandName, dailyCount, s.config.MaxDailyEmailsPerBrand)
			nextAttempt := nextDailyRetryWindow(time.Now())
			scheduledRetries += s.scheduleReportsRetry(ctx, summary.ReportSeqs, retryReasonDailyLimit, nextAttempt)
			dailyLimitHits++
			skippedBrands++
			continue
		}

		// Get contact emails for this brand
		emails := strings.Split(strings.TrimSpace(summary.InferredContactEmails), ",")
		var cleanEmails []string
		throttledCount := 0
		throttleCheckErrors := 0
		earliestRetry := time.Time{}
		hasRetryTime := false

		for _, email := range emails {
			cleanEmail := strings.TrimSpace(email)
			if cleanEmail == "" || !s.isValidEmail(cleanEmail) {
				continue
			}

			// Check opt-out
			optedOut, err := s.isEmailOptedOut(ctx, cleanEmail)
			if err != nil {
				log.Warnf("Failed to check opt-out for %s: %v", cleanEmail, err)
				continue
			}
			if optedOut {
				log.Infof("Skipping opted-out email: %s", cleanEmail)
				continue
			}

			// Check per-brand throttle
			throttled, nextAttempt, err := s.getThrottleDecision(ctx, summary.BrandName, cleanEmail)
			if err != nil {
				log.Warnf("Failed to check throttle for %s/%s: %v", summary.BrandName, cleanEmail, err)
				throttleCheckErrors++
				continue
			}
			if throttled {
				throttledCount++
				if nextAttempt != nil && (!hasRetryTime || nextAttempt.Before(earliestRetry)) {
					earliestRetry = *nextAttempt
					hasRetryTime = true
				}
				log.Infof("Throttling email to %s for brand %s (already sent recently)", cleanEmail, summary.BrandName)
				continue
			}

			cleanEmails = append(cleanEmails, cleanEmail)
		}

		if len(cleanEmails) == 0 {
			reason := retryReasonNoValidEmails
			nextAttempt := time.Now().Add(6 * time.Hour)
			if throttledCount > 0 {
				reason = retryReasonAllThrottled
				if hasRetryTime {
					nextAttempt = earliestRetry
				}
			} else if throttleCheckErrors > 0 {
				reason = retryReasonThrottleError
				nextAttempt = time.Now().Add(30 * time.Minute)
			}

			log.Infof("Brand %s: no sendable emails (throttled=%d, throttle_check_errors=%d); scheduling retry for %d reports at %s (reason=%s)",
				summary.BrandName, throttledCount, throttleCheckErrors, len(summary.ReportSeqs), nextAttempt.Format(time.RFC3339), reason)
			scheduledRetries += s.scheduleReportsRetry(ctx, summary.ReportSeqs, reason, nextAttempt)
			skippedBrands++
			continue
		}

		// DRY RUN MODE: Log what would be sent but don't actually send
		if s.config.DryRun {
			log.Infof("🔒 DRY RUN: Would send to %d recipients for brand %s (%d new, %d total reports)",
				len(cleanEmails), summary.BrandName, summary.NewReportCount, summary.TotalReportCount)
			log.Infof("🔒 DRY RUN: Recipients: %v", cleanEmails)
			for _, seq := range summary.ReportSeqs {
				if err := s.markReportAsProcessed(ctx, seq); err != nil {
					log.Warnf("Failed to mark report %d as processed in dry-run: %v", seq, err)
				}
			}
			processedBrands++
			emailsSent += len(cleanEmails)
			continue
		}

		// Send ONE aggregate notification for this brand
		log.Infof("Brand %s: sending aggregate notification (%d new, %d total) to %d recipients",
			summary.BrandName, summary.NewReportCount, summary.TotalReportCount, len(cleanEmails))

		if err := s.sendAggregateNotification(ctx, &summary, cleanEmails); err != nil {
			log.Errorf("Failed to send aggregate notification for brand %s: %v", summary.BrandName, err)
			nextAttempt := time.Now().Add(30 * time.Minute)
			scheduledRetries += s.scheduleReportsRetry(ctx, summary.ReportSeqs, retryReasonAggregateFailure, nextAttempt)
			continue
		}

		// Record brand+email throttle entries (AFTER successful send)
		for _, emailAddr := range cleanEmails {
			if err := s.recordBrandEmailSent(ctx, summary.BrandName, emailAddr); err != nil {
				log.Warnf("Failed to record brand email sent for %s to %s: %v", summary.BrandName, emailAddr, err)
			}
			if err := s.recordEmailSent(ctx, emailAddr); err != nil {
				log.Warnf("Failed to record email sent to %s: %v", emailAddr, err)
			}
		}

		for _, seq := range summary.ReportSeqs {
			if err := s.markReportAsProcessed(ctx, seq); err != nil {
				log.Warnf("Failed to mark report %d as processed after successful send: %v", seq, err)
			}
		}

		processedBrands++
		emailsSent += len(cleanEmails)
	}

	log.Infof("Aggregate notification cycle complete: %d brands processed, %d skipped (%d daily limit hits), %d emails sent, %d reports scheduled for retry (took %s)",
		processedBrands, skippedBrands, dailyLimitHits, emailsSent, scheduledRetries, time.Since(start))
	return nil
}

// sendAggregateNotification sends one aggregate email for a brand
func (s *EmailService) sendAggregateNotification(ctx context.Context, summary *models.BrandReportSummary, emails []string) error {
	// Build aggregate notification and send via email sender
	return s.email.SendAggregateEmail(emails, summary, s.config.OptOutURL)
}

// processReport processes a single report and sends emails if needed
func (s *EmailService) processReport(ctx context.Context, report models.Report) error {
	// Get analysis data for this report
	analysis, err := s.getReportAnalysis(ctx, report.Seq)
	if err != nil {
		return fmt.Errorf("failed to get analysis for report %d: %w", report.Seq, err)
	}

	// Check if we have inferred contact emails
	inferredStr := strings.TrimSpace(analysis.InferredContactEmails)
	if inferredStr != "" && (analysis.Classification == "digital" || s.config.UseInferredEmailsForPhysical) {
		// Split the comma-separated emails and send to each
		emails := strings.Split(inferredStr, ",")
		var cleanEmails []string

		// Clean up each email (remove whitespace) and validate
		for _, email := range emails {
			cleanEmail := strings.TrimSpace(email)
			if cleanEmail != "" && s.isValidEmail(cleanEmail) {
				cleanEmails = append(cleanEmails, cleanEmail)
			} else if cleanEmail != "" {
				log.Warnf("Report %d: Invalid email address found in inferred contacts: %s", report.Seq, cleanEmail)
			}
		}

		if len(cleanEmails) > 0 {
			log.Infof("Report %d: Using %d valid inferred contact emails (priority over area emails): %v", report.Seq, len(cleanEmails), cleanEmails)

			// Send emails to inferred contacts (no area context needed)
			if err := s.sendEmailsToInferredContacts(ctx, report, analysis, cleanEmails); err != nil {
				log.Errorf("Failed to send emails to inferred contacts for report %d: %v", report.Seq, err)
			} else {
				log.Infof("Successfully sent emails to inferred contacts for report %d (%s report)", report.Seq, analysis.Classification)
			}

			// Mark report as processed and return
			return s.markReportAsProcessed(ctx, report.Seq)
		} else {
			log.Infof("Report %d: No valid inferred contact emails found after validation", report.Seq)
		}
	} else if inferredStr != "" && analysis.Classification != "digital" && !s.config.UseInferredEmailsForPhysical {
		log.Infof("Report %d: Inferred contact emails present but physical inferred-email sending disabled; falling back to area-based logic", report.Seq)
	} else {
		log.Infof("Report %d: No inferred contact emails present (classification=%s); falling back to area-based logic", report.Seq, analysis.Classification)
	}

	// Fall back to area-based email logic if no inferred emails
	log.Infof("Report %d: Falling back to area-based email logic (%s report)", report.Seq, analysis.Classification)

	// Find areas that contain this report point
	features, emails, err := s.findAreasForReport(ctx, report)
	if err != nil {
		return fmt.Errorf("failed to find areas for report: %w", err)
	}

	// If no areas found, mark as processed and return
	if len(emails) == 0 {
		// Physical reports may get inferred_contact_emails populated asynchronously (OSM/web enrichment, email-fetcher, etc).
		// If we mark processed immediately, we lose the chance to send when those contacts arrive.
		if analysis.Classification != "digital" {
			if report.Latitude == 0 && report.Longitude == 0 {
				log.Infof("Report %d: No areas found and location is (0,0); cannot do location-based contact discovery. Marking processed.", report.Seq)
				return s.markReportAsProcessed(ctx, report.Seq)
			}
			deferred, derr := s.deferReportForContactDiscovery(ctx, report.Seq)
			if derr != nil {
				return derr
			}
			if deferred {
				return nil
			}
		}

		log.Infof("Report %d: No areas found, marking as processed", report.Seq)
		return s.markReportAsProcessed(ctx, report.Seq)
	}

	log.Infof("Report %d: Found %d areas with emails, sending area-based emails", report.Seq, len(emails))

	// Send emails for each area
	for areaID, emailAddrs := range emails {
		if err := s.sendEmailsForArea(ctx, report, analysis, features[areaID], emailAddrs); err != nil {
			log.Errorf("Failed to send emails for area %d: %v", areaID, err)
			// Continue with other areas
		}
	}

	// Mark report as processed
	return s.markReportAsProcessed(ctx, report.Seq)
}

func (s *EmailService) getReportRetryCount(ctx context.Context, seq int64) (int, error) {
	var retryCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT retry_count
		FROM email_report_retry
		WHERE seq = ?
		LIMIT 1
	`, seq).Scan(&retryCount)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if retryCount < 0 {
		return 0, nil
	}
	return retryCount, nil
}

func (s *EmailService) deferReportForContactDiscovery(ctx context.Context, seq int64) (bool, error) {
	maxRetries := s.config.ContactDiscoveryMaxRetries
	if maxRetries <= 0 {
		return false, nil
	}

	retryCount, err := s.getReportRetryCount(ctx, seq)
	if err != nil {
		return false, fmt.Errorf("failed to get retry_count for seq %d: %w", seq, err)
	}

	if retryCount >= maxRetries {
		log.Infof("Report %d: No recipients found; contact-discovery retries exhausted (%d/%d), will mark as processed",
			seq, retryCount, maxRetries)
		return false, nil
	}

	baseMinutes := s.config.ContactDiscoveryRetryMinutes
	if baseMinutes <= 0 {
		baseMinutes = 30
	}
	base := time.Duration(baseMinutes) * time.Minute

	// Exponential backoff: base * 2^retryCount, capped to 24h.
	backoff := base * time.Duration(1<<retryCount)
	if backoff > 24*time.Hour {
		backoff = 24 * time.Hour
	}
	nextAttempt := time.Now().Add(backoff)

	log.Infof("Report %d: No recipients found yet; deferring processing for contact discovery (retry %d/%d, next in %s at %s)",
		seq, retryCount+1, maxRetries, backoff, nextAttempt.Format(time.RFC3339))

	if err := s.scheduleReportRetry(ctx, seq, retryReasonAwaitContactDiscovery, nextAttempt); err != nil {
		return false, err
	}
	return true, nil
}

// findAreasForReport finds areas that contain the report point and their associated emails
func (s *EmailService) findAreasForReport(ctx context.Context, report models.Report) (map[uint64]*geojson.Feature, map[uint64][]string, error) {
	// Convert point to WKT format
	// IMPORTANT: MySQL follows the SRID axis order for geographic SRS. For EPSG:4326 the axis order
	// is latitude, then longitude. So we intentionally build `POINT(lat lon)` here.
	ptWKT := fmt.Sprintf("POINT(%g %g)", report.Latitude, report.Longitude)

	// Find areas that contain this point
	qStart := time.Now()
	rows, err := s.db.QueryContext(ctx,
		"SELECT area_id FROM area_index WHERE MBRWithin(ST_GeomFromText(?, 4326), geom)",
		ptWKT)
	if err != nil {
		log.Errorf("area_index query error for report %d (in %s): %v", report.Seq, time.Since(qStart), err)
		return nil, nil, err
	}
	defer rows.Close()

	areaMap := make(map[uint64]bool)
	for rows.Next() {
		var areaID uint64
		if err := rows.Scan(&areaID); err != nil {
			return nil, nil, err
		}
		areaMap[areaID] = true
	}

	log.Infof("Report %d: area_index returned %d area ids (in %s)", report.Seq, len(areaMap), time.Since(qStart))
	if len(areaMap) == 0 {
		log.Infof("Report %d: No areas found, marking as processed", report.Seq)
		return nil, nil, nil
	}

	log.Infof("Report %d: Found %d areas", report.Seq, len(areaMap))

	// Get area features
	areaFeatures, err := s.getAreaFeatures(ctx, areaMap)
	if err != nil {
		return nil, nil, err
	}

	// Get emails for areas
	areaEmails, err := s.getAreaEmails(ctx, areaMap)
	if err != nil {
		return nil, nil, err
	}

	return areaFeatures, areaEmails, nil
}

// getAreaFeatures gets the GeoJSON features for the given areas
func (s *EmailService) getAreaFeatures(ctx context.Context, areaMap map[uint64]bool) (map[uint64]*geojson.Feature, error) {
	if len(areaMap) == 0 {
		return nil, nil
	}

	areaIDs := make([]any, 0, len(areaMap))
	for areaID := range areaMap {
		areaIDs = append(areaIDs, areaID)
	}

	placeholders := strings.Repeat("?,", len(areaIDs))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

	query := fmt.Sprintf("SELECT id, area_json FROM areas WHERE id IN (%s)", placeholders)
	qStart := time.Now()
	rows, err := s.db.QueryContext(ctx, query, areaIDs...)
	if err != nil {
		log.Errorf("getAreaFeatures query error (in %s): %v", time.Since(qStart), err)
		return nil, err
	}
	defer rows.Close()

	features := make(map[uint64]*geojson.Feature)
	for rows.Next() {
		var areaID uint64
		var areaJSON string
		if err := rows.Scan(&areaID, &areaJSON); err != nil {
			return nil, err
		}

		feature := &geojson.Feature{}
		if err := json.Unmarshal([]byte(areaJSON), feature); err != nil {
			return nil, err
		}
		features[areaID] = feature
	}

	log.Infof("getAreaFeatures fetched %d features (in %s)", len(features), time.Since(qStart))
	return features, nil
}

// getAreaEmails gets the email addresses for the given areas
func (s *EmailService) getAreaEmails(ctx context.Context, areaMap map[uint64]bool) (map[uint64][]string, error) {
	if len(areaMap) == 0 {
		return nil, nil
	}

	areaIDs := make([]any, 0, len(areaMap))
	for areaID := range areaMap {
		areaIDs = append(areaIDs, areaID)
	}

	placeholders := strings.Repeat("?,", len(areaIDs))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

	query := fmt.Sprintf("SELECT area_id, email FROM contact_emails WHERE area_id IN (%s) AND consent_report = true", placeholders)
	qStart := time.Now()
	rows, err := s.db.QueryContext(ctx, query, areaIDs...)
	if err != nil {
		log.Errorf("getAreaEmails query error (in %s): %v", time.Since(qStart), err)
		return nil, err
	}
	defer rows.Close()

	areaEmails := make(map[uint64][]string)
	for rows.Next() {
		var areaID uint64
		var email string
		if err := rows.Scan(&areaID, &email); err != nil {
			return nil, err
		}

		if areaEmails[areaID] == nil {
			areaEmails[areaID] = []string{}
		}
		areaEmails[areaID] = append(areaEmails[areaID], email)
	}

	log.Infof("getAreaEmails fetched %d area->emails rows (in %s)", len(areaEmails), time.Since(qStart))
	return areaEmails, nil
}

// sendEmailsToInferredContacts sends emails to inferred contact emails without area context
func (s *EmailService) sendEmailsToInferredContacts(ctx context.Context, report models.Report, analysis *models.ReportAnalysis, emails []string) error {
	if len(emails) == 0 {
		return nil
	}

	brandName := analysis.BrandName
	if brandName == "" {
		brandName = "__missing_brand__"
	}

	// Filter out opted-out emails AND throttled emails
	var validEmails []string
	var throttledCount int
	for _, email := range emails {
		// Check opt-out first
		optedOut, err := s.isEmailOptedOut(ctx, email)
		if err != nil {
			log.Warnf("Failed to check if email %s is opted out: %v, skipping", email, err)
			continue
		}

		if optedOut {
			log.Infof("Skipping opted-out email: %s", email)
			continue
		}

		// Check per-brand throttle
		throttled, err := s.shouldThrottleEmail(ctx, brandName, email)
		if err != nil {
			log.Warnf("Failed to check throttle for brand %s, email %s: %v, proceeding anyway", brandName, email, err)
			// On error, proceed with sending (fail-open for throttle)
		} else if throttled {
			log.Infof("Throttling email to %s for brand %s (already sent recently)", email, brandName)
			throttledCount++
			continue
		}

		validEmails = append(validEmails, email)
	}

	if len(validEmails) == 0 {
		if throttledCount > 0 {
			log.Infof("All %d emails for report %d (brand: %s) are throttled or opted out, no emails sent", len(emails), report.Seq, brandName)
		} else {
			log.Infof("All emails for report %d are opted out, no emails sent", report.Seq)
		}
		return nil
	}

	log.Infof("Sending emails to %d valid inferred contacts for report %d (filtered from %d total, %d throttled)", len(validEmails), report.Seq, len(emails), throttledCount)

	// Generate map image only for physical reports (digital reports don't need location context)
	var mapImg []byte
	if analysis.Classification != "digital" {
		var err error
		mapImg, err = email.GeneratePolygonImg(nil, report.Latitude, report.Longitude)
		if err != nil {
			log.Warnf("Failed to generate map image for report %d: %v, sending email without map", report.Seq, err)
			// Continue without map image
		}
	} else {
		log.Infof("Report %d is digital, skipping map generation", report.Seq)
	}

	// Send emails with analysis data and map image
	err := s.email.SendEmailsWithAnalysis(validEmails, report.Image, mapImg, analysis)
	if err != nil {
		return err
	}

	// Record that emails were sent to these recipients (for both general history and brand throttling)
	for _, emailAddr := range validEmails {
		// Record general email history
		if recordErr := s.recordEmailSent(ctx, emailAddr); recordErr != nil {
			log.Warnf("Failed to record email sent to %s: %v", emailAddr, recordErr)
			// Continue - don't fail the whole operation for history tracking
		}

		// Record brand-specific throttle (this is critical for preventing spam)
		if recordErr := s.recordBrandEmailSent(ctx, brandName, emailAddr); recordErr != nil {
			log.Warnf("Failed to record brand email sent for %s to %s: %v", brandName, emailAddr, recordErr)
			// Continue - don't fail the whole operation
		}
	}

	return nil
}

// sendEmailsForArea sends emails for a specific area
func (s *EmailService) sendEmailsForArea(ctx context.Context, report models.Report, analysis *models.ReportAnalysis, feature *geojson.Feature, emails []string) error {
	if len(emails) == 0 {
		return nil
	}

	// Filter out opted-out emails
	var validEmails []string
	for _, email := range emails {
		optedOut, err := s.isEmailOptedOut(ctx, email)
		if err != nil {
			log.Warnf("Failed to check if email %s is opted out: %v, skipping", email, err)
			continue
		}

		if optedOut {
			log.Infof("Skipping opted-out email: %s", email)
			continue
		}

		validEmails = append(validEmails, email)
	}

	if len(validEmails) == 0 {
		log.Infof("All emails for area are opted out, no emails sent")
		return nil
	}

	log.Infof("Sending emails to %d valid contacts for area (filtered from %d total)", len(validEmails), len(emails))

	// Generate polygon image only for physical reports (digital reports don't need location)
	var polyImg []byte
	if analysis.Classification != "digital" {
		var err error
		polyImg, err = email.GeneratePolygonImg(feature, report.Latitude, report.Longitude)
		if err != nil {
			log.Warnf("Failed to generate polygon image: %v, sending email without map", err)
			// Continue without map image
		}
	} else {
		log.Infof("Report %d is digital, skipping polygon image generation", report.Seq)
	}

	// Send emails with analysis data
	err := s.email.SendEmailsWithAnalysis(validEmails, report.Image, polyImg, analysis)
	if err != nil {
		return err
	}

	// Record that emails were sent to these recipients
	for _, emailAddr := range validEmails {
		if recordErr := s.recordEmailSent(ctx, emailAddr); recordErr != nil {
			log.Warnf("Failed to record email sent to %s: %v", emailAddr, recordErr)
			// Continue - don't fail the whole operation for history tracking
		}
	}

	return nil
}

// getReportAnalysis gets the analysis data for a specific report
func (s *EmailService) getReportAnalysis(ctx context.Context, seq int64) (*models.ReportAnalysis, error) {
	qStart := time.Now()
	query := `
		SELECT seq, source, title, description, 
		brand_name, brand_display_name,
		litter_probability, hazard_probability,
		severity_level, inferred_contact_emails, classification, legal_risk_estimate
		FROM report_analysis
		WHERE seq = ? AND language = 'en'
		LIMIT 1
	`

	var contact_emails sql.NullString
	var brandName sql.NullString
	var brandDisplayName sql.NullString
	var legalRiskEstimate sql.NullString
	var analysis models.ReportAnalysis
	err := s.db.QueryRowContext(ctx, query, seq).Scan(
		&analysis.Seq,
		&analysis.Source,
		&analysis.Title,
		&analysis.Description,
		&brandName,
		&brandDisplayName,
		&analysis.LitterProbability,
		&analysis.HazardProbability,
		&analysis.SeverityLevel,
		&contact_emails,
		&analysis.Classification,
		&legalRiskEstimate,
	)
	if err != nil {
		log.Errorf("getReportAnalysis error for seq %d (in %s): %v", seq, time.Since(qStart), err)
		return nil, fmt.Errorf("failed to get analysis for seq %d: %w", seq, err)
	}

	analysis.InferredContactEmails = contact_emails.String
	analysis.BrandName = brandName.String
	analysis.BrandDisplayName = brandDisplayName.String
	analysis.LegalRiskEstimate = legalRiskEstimate.String

	// Count total reports for this brand (for personalized email messaging)
	if analysis.BrandName != "" {
		countQuery := `
			SELECT COUNT(DISTINCT seq) 
			FROM report_analysis 
			WHERE brand_name = ? AND language = 'en'
		`
		var count int
		if err := s.db.QueryRowContext(ctx, countQuery, analysis.BrandName).Scan(&count); err != nil {
			log.Warnf("Failed to count reports for brand %s: %v", analysis.BrandName, err)
			// Continue without count - not critical
		} else {
			analysis.BrandReportCount = count
			log.Infof("Brand %s has %d total reports", analysis.BrandName, count)
		}
	}

	log.Infof("getReportAnalysis loaded seq %d (in %s)", seq, time.Since(qStart))
	return &analysis, nil
}

// markReportAsProcessed marks a report as processed for email sending
func (s *EmailService) markReportAsProcessed(ctx context.Context, seq int64) error {
	start := time.Now()
	_, err := s.db.ExecContext(ctx, "INSERT INTO sent_reports_emails (seq) VALUES (?)", seq)
	if err != nil {
		log.Errorf("markReportAsProcessed error for seq %d (in %s): %v", seq, time.Since(start), err)
		return err
	}
	if err := s.clearReportRetry(ctx, seq); err != nil {
		log.Warnf("Failed to clear retry state for seq %d: %v", seq, err)
	}
	log.Infof("Report %d marked as processed (in %s)", seq, time.Since(start))
	return nil
}

func (s *EmailService) clearReportRetry(ctx context.Context, seq int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM email_report_retry WHERE seq = ?", seq)
	return err
}

func (s *EmailService) scheduleReportRetry(ctx context.Context, seq int64, reason string, nextAttempt time.Time) error {
	// `no_valid_emails` can churn forever for brands without any discoverable contacts.
	// We cap retries and apply exponential backoff (up to a max) at the DB level so the
	// aggregate loop doesn't hot-spin.
	baseHours := s.config.NoValidEmailsBaseHours
	if baseHours <= 0 {
		baseHours = 6
	}
	maxBackoffHours := s.config.NoValidEmailsMaxBackoffHours
	if maxBackoffHours <= 0 {
		maxBackoffHours = 168 // 7 days
	}
	maxRetries := s.config.NoValidEmailsMaxRetries
	if maxRetries <= 0 {
		maxRetries = 10
	}

	// Cap the exponent so baseHours*2^expCap >= maxBackoffHours (and avoid POW blowups).
	expCap := 0
	for expCap < 30 {
		if baseHours*(1<<expCap) >= maxBackoffHours {
			break
		}
		expCap++
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO email_report_retry (seq, reason, next_attempt_at, retry_count, created_at, updated_at)
		VALUES (?, ?, ?, 1, NOW(), NOW())
		ON DUPLICATE KEY UPDATE
			reason = VALUES(reason),
			next_attempt_at = CASE
				WHEN VALUES(reason) = ? THEN DATE_ADD(NOW(), INTERVAL LEAST(? * POW(2, LEAST(retry_count, ?)), ?) HOUR)
				ELSE VALUES(next_attempt_at)
			END,
			retry_count = CASE
				WHEN VALUES(reason) = ? AND retry_count >= ? THEN retry_count
				ELSE retry_count + 1
			END,
			updated_at = NOW()
	`, seq, reason, nextAttempt,
		retryReasonNoValidEmails, baseHours, expCap, maxBackoffHours,
		retryReasonNoValidEmails, maxRetries,
	)
	if err != nil {
		return fmt.Errorf("failed to schedule retry for seq %d (%s): %w", seq, reason, err)
	}
	return nil
}

func (s *EmailService) scheduleReportsRetry(ctx context.Context, seqs []int64, reason string, nextAttempt time.Time) int {
	scheduled := 0
	for _, seq := range seqs {
		if err := s.scheduleReportRetry(ctx, seq, reason, nextAttempt); err != nil {
			log.Warnf("Failed to schedule retry for seq %d: %v", seq, err)
			continue
		}
		scheduled++
	}
	return scheduled
}

func nextDailyRetryWindow(now time.Time) time.Time {
	n := now
	if n.IsZero() {
		n = time.Now()
	}
	y, m, d := n.Date()
	loc := n.Location()
	return time.Date(y, m, d+1, 0, 5, 0, 0, loc)
}

// verifyAndCreateTables ensures all required tables exist with proper structure
func verifyAndCreateTables(db *sql.DB) error {
	ctx := context.Background()

	// Check if sent_reports_emails table exists
	var tableExists int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() 
		AND table_name = 'sent_reports_emails'
	`).Scan(&tableExists)

	if err != nil {
		return fmt.Errorf("failed to check if sent_reports_emails table exists: %w", err)
	}

	if tableExists == 0 {
		log.Info("Creating sent_reports_emails table...")

		// Create the table with proper indexing
		createTableSQL := `
			CREATE TABLE sent_reports_emails (
				seq INT PRIMARY KEY,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				INDEX idx_created_at (created_at),
				INDEX idx_seq (seq)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`

		_, err = db.ExecContext(ctx, createTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create sent_reports_emails table: %w", err)
		}

		log.Info("sent_reports_emails table created successfully")
	} else {
		log.Info("sent_reports_emails table already exists")

		// Verify that the seq index exists
		var indexExists int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) 
			FROM information_schema.statistics 
			WHERE table_schema = DATABASE() 
			AND table_name = 'sent_reports_emails' 
			AND index_name = 'PRIMARY'
		`).Scan(&indexExists)

		if err != nil {
			return fmt.Errorf("failed to check seq index: %w", err)
		}

		if indexExists == 0 {
			log.Warn("seq index missing on sent_reports_emails table, this may cause performance issues")
		} else {
			log.Info("seq index verified on sent_reports_emails table")
		}
	}

	// Check if opted_out_emails table exists
	var optedOutTableExists int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() 
		AND table_name = 'opted_out_emails'
	`).Scan(&optedOutTableExists)

	if err != nil {
		return fmt.Errorf("failed to check if opted_out_emails table exists: %w", err)
	}

	if optedOutTableExists == 0 {
		log.Info("Creating opted_out_emails table...")

		// Create the opted_out_emails table with proper indexing
		createOptedOutTableSQL := `
			CREATE TABLE opted_out_emails (
				id INT AUTO_INCREMENT PRIMARY KEY,
				email VARCHAR(255) NOT NULL UNIQUE,
				opted_out_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				INDEX idx_email (email),
				INDEX idx_opted_out_at (opted_out_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`

		_, err = db.ExecContext(ctx, createOptedOutTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create opted_out_emails table: %w", err)
		}

		log.Info("opted_out_emails table created successfully")
	} else {
		log.Info("opted_out_emails table already exists")
	}

	// Check if email_recipient_history table exists
	var recipientHistoryTableExists int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() 
		AND table_name = 'email_recipient_history'
	`).Scan(&recipientHistoryTableExists)

	if err != nil {
		return fmt.Errorf("failed to check if email_recipient_history table exists: %w", err)
	}

	if recipientHistoryTableExists == 0 {
		log.Info("Creating email_recipient_history table...")

		// Create the email_recipient_history table for tracking first-time vs returning recipients
		createRecipientHistoryTableSQL := `
			CREATE TABLE email_recipient_history (
				id INT AUTO_INCREMENT PRIMARY KEY,
				email VARCHAR(255) NOT NULL UNIQUE,
				first_email_sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				last_email_sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
				email_count INT DEFAULT 1,
				INDEX idx_email_history_email (email),
				INDEX idx_email_history_first_sent (first_email_sent_at),
				INDEX idx_email_history_last_sent (last_email_sent_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`

		_, err = db.ExecContext(ctx, createRecipientHistoryTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create email_recipient_history table: %w", err)
		}

		log.Info("email_recipient_history table created successfully")
	} else {
		log.Info("email_recipient_history table already exists")
	}

	// Check if brand_email_throttle table exists (for per-brand rate limiting)
	var throttleTableExists int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() 
		AND table_name = 'brand_email_throttle'
	`).Scan(&throttleTableExists)

	if err != nil {
		return fmt.Errorf("failed to check if brand_email_throttle table exists: %w", err)
	}

	if throttleTableExists == 0 {
		log.Info("Creating brand_email_throttle table...")

		// Create the brand_email_throttle table for per-brand email rate limiting
		createThrottleTableSQL := `
			CREATE TABLE brand_email_throttle (
				brand_name VARCHAR(255) NOT NULL,
				email VARCHAR(255) NOT NULL,
				last_sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				email_count INT DEFAULT 1,
				PRIMARY KEY (brand_name, email),
				INDEX idx_throttle_brand (brand_name),
				INDEX idx_throttle_email (email),
				INDEX idx_throttle_last_sent (last_sent_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`

		_, err = db.ExecContext(ctx, createThrottleTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create brand_email_throttle table: %w", err)
		}

		log.Info("brand_email_throttle table created successfully")
	} else {
		log.Info("brand_email_throttle table already exists")
	}

	// Check if email_report_retry table exists (retry state for unsent reports)
	var retryTableExists int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		AND table_name = 'email_report_retry'
	`).Scan(&retryTableExists)
	if err != nil {
		return fmt.Errorf("failed to check if email_report_retry table exists: %w", err)
	}

	if retryTableExists == 0 {
		log.Info("Creating email_report_retry table...")
		createRetryTableSQL := `
			CREATE TABLE email_report_retry (
				seq INT PRIMARY KEY,
				reason VARCHAR(64) NOT NULL,
				next_attempt_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				retry_count INT NOT NULL DEFAULT 1,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
				INDEX idx_retry_next_attempt (next_attempt_at),
				INDEX idx_retry_reason (reason),
				INDEX idx_retry_updated_at (updated_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`
		_, err = db.ExecContext(ctx, createRetryTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create email_report_retry table: %w", err)
		}
		log.Info("email_report_retry table created successfully")
	} else {
		log.Info("email_report_retry table already exists")
	}

	// Verify that required tables exist
	requiredTables := []string{"reports", "area_index", "areas", "contact_emails", "report_analysis"}
	for _, tableName := range requiredTables {
		var exists int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) 
			FROM information_schema.tables 
			WHERE table_schema = DATABASE() 
			AND table_name = ?
		`, tableName).Scan(&exists)

		if err != nil {
			return fmt.Errorf("failed to check if %s table exists: %w", tableName, err)
		}

		if exists == 0 {
			return fmt.Errorf("required table %s does not exist", tableName)
		}
	}

	log.Info("All required tables verified")
	return nil
}

// AddOptedOutEmail adds an email to the opted_out_emails table
func (s *EmailService) AddOptedOutEmail(email string) error {
	ctx := context.Background()

	// Check if email already exists
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM opted_out_emails WHERE email = ?
	`, email).Scan(&count)

	if err != nil {
		return fmt.Errorf("failed to check if email %s already exists: %w", email, err)
	}

	if count > 0 {
		return fmt.Errorf("email %s is already opted out", email)
	}

	// Insert new opted out email
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO opted_out_emails (email) VALUES (?)
	`, email)

	if err != nil {
		return fmt.Errorf("failed to add email %s to opted out list: %w", email, err)
	}

	log.Infof("Email %s has been opted out successfully", email)
	return nil
}
