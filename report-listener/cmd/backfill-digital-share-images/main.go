package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"report-listener/config"
	"report-listener/database"
	"report-listener/handlers"
	"report-listener/rabbitmq"
)

type candidateReport struct {
	Seq         int
	Timestamp   time.Time
	Latitude    float64
	Longitude   float64
	Description string
	SourceURL   string
}

func main() {
	var (
		sinceHours = flag.Int("since-hours", 96, "only consider share reports created within the last N hours")
		limit      = flag.Int("limit", 250, "maximum number of candidate reports to scan")
		dryRun     = flag.Bool("dry-run", false, "discover candidates and fetch images without writing DB changes or republishing analysis")
		republish  = flag.Bool("republish", true, "republish updated reports to report.raw so analysis refreshes with the new image")
	)
	flag.Parse()

	if *sinceHours <= 0 {
		log.Fatal("since-hours must be > 0")
	}
	if *limit <= 0 {
		log.Fatal("limit must be > 0")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()

	var publisher *rabbitmq.Publisher
	if *republish && !*dryRun {
		publisher, err = rabbitmq.NewPublisher(cfg.AMQPURL(), cfg.RabbitExchange, cfg.RabbitRawReportRoutingKey)
		if err != nil {
			log.Fatalf("connect rabbitmq publisher: %v", err)
		}
		defer publisher.Close()
	}

	ctx := context.Background()
	candidates, err := loadCandidates(ctx, db.DB(), *sinceHours, *limit)
	if err != nil {
		log.Fatalf("load candidates: %v", err)
	}

	log.Printf("digital share image backfill: found %d candidate reports in the last %d hours", len(candidates), *sinceHours)
	if len(candidates) == 0 {
		return
	}

	var (
		updatedCount      int
		republishedCount  int
		noImageCount      int
		fetchFailureCount int
		writeFailureCount int
	)

	for _, candidate := range candidates {
		log.Printf("backfill: seq=%d source_url=%s", candidate.Seq, candidate.SourceURL)
		attachments, err := handlers.FetchRemoteDigitalShareAttachments(ctx, candidate.SourceURL, 6, 64*1024*1024)
		if err != nil {
			fetchFailureCount++
			log.Printf("backfill: seq=%d fetch failed: %v", candidate.Seq, err)
			continue
		}
		if len(attachments) == 0 {
			noImageCount++
			log.Printf("backfill: seq=%d no remote images found", candidate.Seq)
			continue
		}

		if *dryRun {
			updatedCount++
			log.Printf("backfill dry-run: seq=%d would write %d images and refresh analysis", candidate.Seq, len(attachments))
			continue
		}

		if err := persistBackfilledImages(ctx, db.DB(), candidate.Seq, attachments); err != nil {
			writeFailureCount++
			log.Printf("backfill: seq=%d write failed: %v", candidate.Seq, err)
			continue
		}
		updatedCount++

		if publisher != nil {
			msg := map[string]any{
				"seq":         candidate.Seq,
				"description": candidate.Description,
				"latitude":    candidate.Latitude,
				"longitude":   candidate.Longitude,
			}
			if err := publisher.PublishWithRoutingKey(cfg.RabbitRawReportRoutingKey, msg); err != nil {
				writeFailureCount++
				log.Printf("backfill: seq=%d republish failed: %v", candidate.Seq, err)
				continue
			}
			republishedCount++
		}
	}

	log.Printf(
		"digital share image backfill complete: candidates=%d updated=%d republished=%d no_image=%d fetch_failures=%d write_failures=%d dry_run=%t",
		len(candidates),
		updatedCount,
		republishedCount,
		noImageCount,
		fetchFailureCount,
		writeFailureCount,
		*dryRun,
	)

	if writeFailureCount > 0 {
		log.Fatal("digital share image backfill completed with write failures")
	}
}

func loadCandidates(ctx context.Context, db *sql.DB, sinceHours int, limit int) ([]candidateReport, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			r.seq,
			r.ts,
			r.latitude,
			r.longitude,
			COALESCE(r.description, '') AS description,
			COALESCE(dsr.source_url, '') AS source_url
		FROM reports r
		INNER JOIN digital_share_reports dsr ON dsr.report_seq = r.seq
		LEFT JOIN digital_share_attachments dsa ON dsa.report_seq = r.seq
		WHERE dsr.source_url IS NOT NULL
		  AND dsr.source_url <> ''
		  AND r.ts >= (UTC_TIMESTAMP() - INTERVAL ? HOUR)
		  AND OCTET_LENGTH(COALESCE(r.image, '')) = 0
		GROUP BY r.seq, r.ts, r.latitude, r.longitude, r.description, dsr.source_url
		HAVING COUNT(dsa.report_seq) = 0
		ORDER BY r.seq ASC
		LIMIT ?
	`, sinceHours, limit)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]candidateReport, 0, limit)
	for rows.Next() {
		var item candidateReport
		if err := rows.Scan(
			&item.Seq,
			&item.Timestamp,
			&item.Latitude,
			&item.Longitude,
			&item.Description,
			&item.SourceURL,
		); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		item.Description = strings.TrimSpace(item.Description)
		item.SourceURL = strings.TrimSpace(item.SourceURL)
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidates: %w", err)
	}
	return candidates, nil
}

func persistBackfilledImages(ctx context.Context, db *sql.DB, seq int, attachments []database.DigitalShareAttachment) error {
	if seq <= 0 || len(attachments) == 0 || len(attachments[0].Bytes) == 0 {
		return fmt.Errorf("invalid backfill payload for seq %d", seq)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE reports
		 SET image = ?
		 WHERE seq = ?
		   AND OCTET_LENGTH(COALESCE(image, '')) = 0`,
		attachments[0].Bytes,
		seq,
	)
	if err != nil {
		return fmt.Errorf("update reports.image: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for reports.image: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("report %d was not eligible for image backfill", seq)
	}

	for idx, attachment := range attachments {
		if len(attachment.Bytes) == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO digital_share_attachments (
				report_seq, ordinal, filename, mime_type, sha256, image
			) VALUES (?, ?, ?, ?, ?, ?)`,
			seq,
			idx,
			nullIfBlank(attachment.Filename),
			nullIfBlank(attachment.MIMEType),
			nullIfBlank(attachment.SHA256),
			attachment.Bytes,
		); err != nil {
			return fmt.Errorf("insert attachment[%d]: %w", idx, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func nullIfBlank(raw string) any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.TrimSpace(raw)
}
