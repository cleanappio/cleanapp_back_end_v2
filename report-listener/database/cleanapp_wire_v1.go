package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type WireSubmissionRaw struct {
	SubmissionID      string
	ReceiptID         string
	FetcherID         string
	KeyID             sql.NullString
	SourceID          string
	SchemaVersion     string
	SubmittedAt       time.Time
	ObservedAt        sql.NullTime
	AgentID           string
	Lane              string
	MaterialHash      string
	SubmissionQuality float64
	ReportSeq         sql.NullInt64
	AgentJSON         []byte
	ProvenanceJSON    []byte
	ReportJSON        []byte
	DedupeJSON        []byte
	DeliveryJSON      []byte
	ExtensionsJSON    []byte
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type WireReceipt struct {
	ReceiptID         string
	SubmissionID      string
	FetcherID         string
	SourceID          string
	ReportSeq         sql.NullInt64
	Status            string
	Lane              string
	IdempotencyReplay bool
	RejectionCode     sql.NullString
	WarningsJSON      []byte
	NextCheckAfter    sql.NullTime
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type WireReputationProfile struct {
	FetcherID          string
	PrecisionScore     float64
	NoveltyScore       float64
	EvidenceScore      float64
	RoutingScore       float64
	CorroborationScore float64
	LatencyScore       float64
	ResolutionScore    float64
	PolicyScore        float64
	DedupePenalty      float64
	AbusePenalty       float64
	ReputationScore    float64
	SampleSize         int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (d *Database) GetWireSubmissionByFetcherAndSource(ctx context.Context, fetcherID, sourceID string) (*WireSubmissionRaw, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT submission_id, receipt_id, fetcher_id, key_id, source_id, schema_version,
		       submitted_at, observed_at, agent_id, lane, material_hash, submission_quality,
		       report_seq, agent_json, provenance_json, report_json, dedupe_json, delivery_json, extensions_json,
		       created_at, updated_at
		FROM wire_submissions_raw
		WHERE fetcher_id = ? AND source_id = ?
	`, fetcherID, sourceID)

	var s WireSubmissionRaw
	if err := row.Scan(
		&s.SubmissionID, &s.ReceiptID, &s.FetcherID, &s.KeyID, &s.SourceID, &s.SchemaVersion,
		&s.SubmittedAt, &s.ObservedAt, &s.AgentID, &s.Lane, &s.MaterialHash, &s.SubmissionQuality,
		&s.ReportSeq, &s.AgentJSON, &s.ProvenanceJSON, &s.ReportJSON, &s.DedupeJSON, &s.DeliveryJSON, &s.ExtensionsJSON,
		&s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get wire submission: %w", err)
	}
	return &s, nil
}

func (d *Database) InsertWireSubmissionAndReceipt(ctx context.Context, submission *WireSubmissionRaw, receipt *WireReceipt) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin wire tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO wire_submissions_raw (
			submission_id, receipt_id, fetcher_id, key_id, source_id, schema_version,
			submitted_at, observed_at, agent_id, lane, material_hash, submission_quality,
			report_seq, agent_json, provenance_json, report_json, dedupe_json, delivery_json, extensions_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		submission.SubmissionID,
		submission.ReceiptID,
		submission.FetcherID,
		nullStr(submission.KeyID),
		submission.SourceID,
		submission.SchemaVersion,
		submission.SubmittedAt,
		nullTime(submission.ObservedAt),
		submission.AgentID,
		submission.Lane,
		submission.MaterialHash,
		submission.SubmissionQuality,
		nullInt64(submission.ReportSeq),
		nullJSON(submission.AgentJSON),
		nullJSON(submission.ProvenanceJSON),
		nullJSON(submission.ReportJSON),
		nullJSON(submission.DedupeJSON),
		nullJSON(submission.DeliveryJSON),
		nullJSON(submission.ExtensionsJSON),
	)
	if err != nil {
		return fmt.Errorf("insert wire submission: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO wire_submission_receipts (
			receipt_id, submission_id, fetcher_id, source_id, report_seq, status, lane,
			idempotency_replay, rejection_code, warnings_json, next_check_after
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		receipt.ReceiptID,
		receipt.SubmissionID,
		receipt.FetcherID,
		receipt.SourceID,
		nullInt64(receipt.ReportSeq),
		receipt.Status,
		receipt.Lane,
		receipt.IdempotencyReplay,
		nullStr(receipt.RejectionCode),
		nullJSON(receipt.WarningsJSON),
		nullTime(receipt.NextCheckAfter),
	)
	if err != nil {
		return fmt.Errorf("insert wire receipt: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit wire tx: %w", err)
	}
	return nil
}

func (d *Database) GetWireReceipt(ctx context.Context, fetcherID, receiptID string) (*WireReceipt, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT receipt_id, submission_id, fetcher_id, source_id, report_seq, status, lane,
		       idempotency_replay, rejection_code, warnings_json, next_check_after, created_at, updated_at
		FROM wire_submission_receipts
		WHERE fetcher_id = ? AND receipt_id = ?
	`, fetcherID, receiptID)
	var r WireReceipt
	if err := row.Scan(
		&r.ReceiptID, &r.SubmissionID, &r.FetcherID, &r.SourceID, &r.ReportSeq, &r.Status, &r.Lane,
		&r.IdempotencyReplay, &r.RejectionCode, &r.WarningsJSON, &r.NextCheckAfter, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get wire receipt: %w", err)
	}
	return &r, nil
}

func (d *Database) GetLatestWireReceiptBySource(ctx context.Context, fetcherID, sourceID string) (*WireReceipt, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT receipt_id, submission_id, fetcher_id, source_id, report_seq, status, lane,
		       idempotency_replay, rejection_code, warnings_json, next_check_after, created_at, updated_at
		FROM wire_submission_receipts
		WHERE fetcher_id = ? AND source_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, fetcherID, sourceID)
	var r WireReceipt
	if err := row.Scan(
		&r.ReceiptID, &r.SubmissionID, &r.FetcherID, &r.SourceID, &r.ReportSeq, &r.Status, &r.Lane,
		&r.IdempotencyReplay, &r.RejectionCode, &r.WarningsJSON, &r.NextCheckAfter, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get latest wire receipt by source: %w", err)
	}
	return &r, nil
}

func (d *Database) EnsureWireReputationProfile(ctx context.Context, fetcherID string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO wire_agent_reputation_metrics (
			fetcher_id
		) VALUES (?)
		ON DUPLICATE KEY UPDATE fetcher_id = fetcher_id
	`, fetcherID)
	if err != nil {
		return fmt.Errorf("ensure wire reputation profile: %w", err)
	}
	return nil
}

func (d *Database) GetWireReputationProfile(ctx context.Context, fetcherID string) (*WireReputationProfile, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT fetcher_id, precision_score, novelty_score, evidence_score, routing_score,
		       corroboration_score, latency_score, resolution_score, policy_score,
		       dedupe_penalty, abuse_penalty, reputation_score, sample_size, created_at, updated_at
		FROM wire_agent_reputation_metrics
		WHERE fetcher_id = ?
	`, fetcherID)
	var p WireReputationProfile
	if err := row.Scan(
		&p.FetcherID, &p.PrecisionScore, &p.NoveltyScore, &p.EvidenceScore, &p.RoutingScore,
		&p.CorroborationScore, &p.LatencyScore, &p.ResolutionScore, &p.PolicyScore,
		&p.DedupePenalty, &p.AbusePenalty, &p.ReputationScore, &p.SampleSize, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get wire reputation profile: %w", err)
	}
	return &p, nil
}

func (d *Database) IncrementWireReputationSample(ctx context.Context, fetcherID string) error {
	if err := d.EnsureWireReputationProfile(ctx, fetcherID); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE wire_agent_reputation_metrics
		SET sample_size = sample_size + 1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE fetcher_id = ?
	`, fetcherID)
	if err != nil {
		return fmt.Errorf("increment wire reputation sample: %w", err)
	}
	return nil
}

func (d *Database) MarshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func nullStr(v sql.NullString) interface{} {
	if !v.Valid || v.String == "" {
		return nil
	}
	return v.String
}

func nullTime(v sql.NullTime) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Time
}

func nullInt64(v sql.NullInt64) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullJSON(v []byte) interface{} {
	if len(v) == 0 {
		return nil
	}
	return v
}
