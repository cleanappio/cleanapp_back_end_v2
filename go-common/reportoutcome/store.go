package reportoutcome

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type stored struct {
	handling              Handling
	consent               int
	outcome               Outcome
	fingerprint           string
	publicationAuthorized bool
	event                 string
	attempts              int
	nextAttempt           sql.NullTime
}

type source struct {
	input                                      Input
	publicID, description, visibility, fetcher string
	reportJSON                                 []byte
	imageBytes                                 int64
	applicable                                 bool
}

func readStored(ctx context.Context, q queryer, seq int, lock bool) (*stored, error) {
	statement := `SELECT consent_version,publication_mode,notices_mode,outcome_json,
	 COALESCE(analysis_fingerprint,''),publication_authorized,public_event_status,public_event_attempts,public_event_next_attempt_at
	 FROM report_outcomes WHERE report_seq=?`
	if lock {
		statement += ` FOR UPDATE`
	}
	var value stored
	var raw []byte
	err := q.QueryRowContext(ctx, statement, seq).Scan(&value.consent, &value.handling.Publication,
		&value.handling.Notices, &raw, &value.fingerprint, &value.publicationAuthorized, &value.event, &value.attempts, &value.nextAttempt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &value.outcome); err != nil {
		return nil, fmt.Errorf("invalid stored report outcome")
	}
	return &value, nil
}

// IntakeRecordTx is called in the same transaction as the new report. A replay
// can never upgrade the permissions originally recorded for that report.
func IntakeRecordTx(ctx context.Context, tx *sql.Tx, seq int, handling *Handling) error {
	normalized, err := NormalizeHandling(handling)
	if err != nil {
		return err
	}
	version := 0
	if handling != nil {
		version = 1
	}
	initial := Review(Input{Handling: normalized, ConsentVersion: version}, Analysis{}, time.Now().UTC())
	raw, err := json.Marshal(initial)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO report_outcomes
	 (report_seq,consent_version,publication_mode,notices_mode,outcome_json)
	 VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE report_seq=report_seq`, seq, version, normalized.Publication, normalized.Notices, raw)
	if err != nil {
		return err
	}
	existing, err := readStored(ctx, tx, seq, true)
	if err != nil {
		return err
	}
	if existing == nil || existing.consent != version || existing.handling != normalized {
		return fmt.Errorf("report handling authorization is immutable")
	}
	return nil
}

func readSource(ctx context.Context, q queryer, seq int, value *stored) (source, error) {
	var result source
	var active, ownerPrivate bool
	err := q.QueryRowContext(ctx, `SELECT COALESCE(r.public_id,''),COALESCE(r.description,''),
	 COALESCE(OCTET_LENGTH(r.image),0),COALESCE(rr.visibility,'shadow'),COALESCE(rr.fetcher_id,''),
	 NOT EXISTS(SELECT 1 FROM report_status s WHERE s.seq=r.seq AND s.status IS NOT NULL AND s.status<>'active'),
	 EXISTS(SELECT 1 FROM reports_owners o WHERE o.seq=r.seq AND o.owner IS NOT NULL AND o.owner<>'' AND o.is_public=FALSE)
	 FROM reports r LEFT JOIN report_raw rr ON rr.report_seq=r.seq WHERE r.seq=?`, seq).
		Scan(&result.publicID, &result.description, &result.imageBytes, &result.visibility, &result.fetcher, &active, &ownerPrivate)
	if err != nil {
		return result, err
	}
	var submission, actor, channel, agentID string
	err = q.QueryRowContext(ctx, `SELECT submission_id,actor_kind,channel,agent_id FROM wire_submissions_raw
	 WHERE report_seq=? AND fetcher_id=? ORDER BY submitted_at DESC,submission_id DESC LIMIT 1`, seq, result.fetcher).
		Scan(&submission, &actor, &channel, &agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if actor != "machine" || (channel != "wire" && channel != "public_agent") {
		return result, nil
	}
	var agentJSON, provenanceJSON []byte
	err = q.QueryRowContext(ctx, `SELECT agent_json,provenance_json,
	 CASE WHEN OCTET_LENGTH(report_json)<=65536 THEN report_json ELSE NULL END
	 FROM wire_submissions_raw WHERE submission_id=? AND report_seq=?`, submission, seq).
		Scan(&agentJSON, &provenanceJSON, &result.reportJSON)
	if err != nil {
		return result, err
	}
	var agent struct {
		Type string `json:"agent_type"`
	}
	var provenance struct {
		Generation string `json:"generation_method"`
	}
	if json.Unmarshal(agentJSON, &agent) != nil || json.Unmarshal(provenanceJSON, &provenance) != nil {
		return result, fmt.Errorf("invalid retained agent provenance")
	}
	if agentID == "report-processor" || agent.Type == "internal_service" ||
		provenance.Generation == "report_processor_match" || provenance.Generation == "legacy_v3_bulk_ingest" ||
		strings.HasPrefix(agentID, "legacy-bulk-ingest-") {
		return result, nil
	}
	if value == nil && result.visibility == "public" {
		return result, nil
	}
	result.applicable = true
	result.input.Handling = Handling{Publication: "private", Notices: "none"}
	if value != nil {
		result.input.Handling = value.handling
		result.input.ConsentVersion = value.consent
	}
	var report struct {
		Description    string            `json:"description"`
		DigitalContext map[string]any    `json:"digital_context"`
		Evidence       []json.RawMessage `json:"evidence_bundle"`
		Location       json.RawMessage   `json:"location"`
		Target         struct {
			Name string `json:"name"`
		} `json:"target_entity"`
		Privacy struct {
			PII       bool `json:"contains_pii"`
			Redaction bool `json:"requires_redaction"`
		} `json:"privacy"`
	}
	if len(result.reportJSON) == 0 || json.Unmarshal(result.reportJSON, &report) != nil {
		result.input.PrivacyHoldReason = "invalid_public_projection"
	}
	result.input.HasOriginal = strings.TrimSpace(report.Description) != "" || strings.TrimSpace(result.description) != "" || result.imageBytes > 0
	result.input.HasSourceContext = retainedContext(result.reportJSON)
	var wireRestricted, projectionRestricted bool
	if err = q.QueryRowContext(ctx, `SELECT
	 EXISTS(SELECT 1 FROM wire_evidence_projection WHERE report_seq=? AND (contains_pii=TRUE OR requires_redaction=TRUE)),
	 EXISTS(SELECT 1 FROM report_evidence_source_projection WHERE seq=? AND privacy_restricted=TRUE)`, seq, seq).Scan(&wireRestricted, &projectionRestricted); err != nil {
		return result, err
	}
	if result.input.PrivacyHoldReason == "" {
		result.input.PrivacyHoldReason = PrivacyHold(result.reportJSON, result.imageBytes, report.Privacy.PII, report.Privacy.Redaction, result.description)
	}
	if result.input.PrivacyHoldReason == "" && (wireRestricted || projectionRestricted) {
		result.input.PrivacyHoldReason = "source_privacy_restricted"
	}
	if result.input.PrivacyHoldReason == "" {
		hold, err := publicAnalysisPrivacy(ctx, q, seq)
		if err != nil {
			return result, err
		}
		result.input.PrivacyHoldReason = hold
	}
	if !active {
		result.input.PolicyHoldReason = "report_inactive"
	}
	if ownerPrivate {
		result.input.PolicyHoldReason = "owner_private"
	}
	if result.publicID == "" {
		result.input.PolicyHoldReason = "public_identifier_unavailable"
	}
	var namespace, reason string
	var version int
	var email, notifications, rewards, routing, analysis bool
	err = q.QueryRowContext(ctx, `SELECT source_namespace,reason,policy_version,suppress_email,
	 suppress_notifications,suppress_rewards,suppress_routing,suppress_analysis
	 FROM report_side_effect_policy WHERE report_seq=?`, seq).
		Scan(&namespace, &reason, &version, &email, &notifications, &rewards, &routing, &analysis)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if errors.Is(err, sql.ErrNoRows) && result.input.ConsentVersion > 0 {
		result.input.PolicyHoldReason = "explicit_effect_policy_missing"
	}
	if err == nil {
		result.input.AnalysisSuppressed = version != 1 || analysis
		known := (namespace == "public-agent" && reason == "Anonymous agent intake is private pending review") ||
			(namespace == "agent-outcome" && reason == "Explicit agent handling permissions")
		noticesBlocked := result.input.Handling.Notices != "automatic"
		expected := version == 1 && !analysis && rewards && email == noticesBlocked && notifications == noticesBlocked && routing == noticesBlocked
		if !known || !expected {
			result.input.PolicyHoldReason = "explicit_effect_policy_hold"
		}
	}
	return result, nil
}

func (s *Store) Input(ctx context.Context, seq int) (Input, bool, error) {
	value, err := readStored(ctx, s.db, seq, false)
	if err != nil {
		return Input{}, false, err
	}
	source, err := readSource(ctx, s.db, seq, value)
	return source.input, source.applicable, err
}

func analysisHash(input Input, analysis Analysis) string {
	raw, _ := json.Marshal(struct {
		Input    Input
		Analysis Analysis
	}{input, analysis})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) Complete(ctx context.Context, seq int, analysis Analysis) (Record, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	value, err := readStored(ctx, tx, seq, true)
	if err != nil {
		return Record{}, err
	}
	source, err := readSource(ctx, tx, seq, value)
	if err != nil {
		return Record{}, err
	}
	if !source.applicable {
		return Record{}, nil
	}
	if analysis.Available {
		persisted, err := storedAnalysis(ctx, tx, seq)
		if err != nil {
			return Record{}, err
		}
		if !persisted.Available {
			return Record{}, fmt.Errorf("analysis must be persisted before completion")
		}
		analysis = persisted
	}
	if value == nil {
		if err := IntakeRecordTx(ctx, tx, seq, nil); err != nil {
			return Record{}, err
		}
		value, err = readStored(ctx, tx, seq, true)
		if err != nil {
			return Record{}, err
		}
	}
	if source.input.PrivacyHoldReason == "" {
		source.input.PrivacyHoldReason = PrivacyHold(nil, 0, false, false, analysis.Title, analysis.Description, analysis.Summary, analysis.BrandName)
	}
	contextKey, observation, tokens := observationKeys(source, analysis)
	if analysis.Available && analysis.IsValid {
		source.input.Candidates, err = matchCandidates(ctx, tx, seq, contextKey, observation, tokens)
		if err != nil {
			return Record{}, err
		}
	}
	fingerprint := analysisHash(source.input, analysis)
	outcome := Review(source.input, analysis, time.Now().UTC())
	if fingerprint == value.fingerprint && value.outcome.Decision != nil {
		outcome.Decision.DecidedAt = value.outcome.Decision.DecidedAt
		outcome.Processing.CompletedAt = value.outcome.Processing.CompletedAt
	}
	authorized := outcome.Publication.Status == "pending" && source.input.ConsentVersion == 1
	event := "none"
	if authorized {
		event = "pending"
		outcome.Publication.Status = "public"
		if value.event == "published" {
			event = "published"
			if source.input.Handling.Notices != "automatic" {
				outcome.Final = true
				outcome.NextAction = NextAction{Type: "none"}
			}
		}
		if value.event == "failed" {
			event = "failed"
			outcome.Final = true
			outcome.NextAction = NextAction{Type: "none"}
			outcome.Publication.ReasonCode = "publication_event_failed"
			for index := range outcome.Delivery {
				if source.input.Handling.Notices == "automatic" {
					outcome.Delivery[index].Status = "failed"
					outcome.Delivery[index].ReasonCode = "publication_event_failed"
				}
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE report_raw SET visibility='public',trust_level='unverified',
		 promoted_to_public_at=COALESCE(promoted_to_public_at,NOW()) WHERE report_seq=?`, seq); err != nil {
			return Record{}, err
		}
	} else if value.publicationAuthorized {
		if _, err = tx.ExecContext(ctx, `UPDATE report_raw SET visibility='shadow',trust_level='unverified'
		 WHERE report_seq=?`, seq); err != nil {
			return Record{}, err
		}
	}
	if outcome.Publication.Status == "public" {
		outcome.Publication.CanonicalPath = canonicalPath(analysis.Classification, source.publicID)
	}
	raw, err := json.Marshal(outcome)
	if err != nil {
		return Record{}, err
	}
	tokenJSON, _ := json.Marshal(tokens)
	_, err = tx.ExecContext(ctx, `UPDATE report_outcomes SET policy_version=?,processing_status=?,decision_state=?,reason_code=?,
	 outcome_json=?,analysis_fingerprint=?,publication_status=?,publication_authorized=?,public_event_status=?,
	 context_key=NULLIF(?,''),observation_fingerprint=NULLIF(?,''),observation_tokens=?,match_reconciled=?,reconcile_pending=?,
	 next_reconcile_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND) WHERE report_seq=?`,
		PolicyVersion, outcome.Processing.Status, string(outcome.Decision.State), outcome.Decision.ReasonCode,
		raw, fingerprint, outcome.Publication.Status, authorized, event, contextKey, observation, tokenJSON, value.event == "published",
		outcome.Processing.Status == "pending_analysis" || event == "pending", seq)
	if err != nil {
		return Record{}, err
	}
	if err = tx.Commit(); err != nil {
		return Record{}, err
	}
	due := !value.nextAttempt.Valid || !time.Now().Before(value.nextAttempt.Time)
	return Record{Outcome: outcome, Applicable: true, PublishPending: event == "pending" && due && value.attempts < 5, ServerAuthorizedPublication: authorized}, nil
}

func (s *Store) PublicationPending(ctx context.Context, seq int) (bool, error) {
	value, err := readStored(ctx, s.db, seq, false)
	if err != nil || value == nil {
		return false, err
	}
	source, err := readSource(ctx, s.db, seq, value)
	if err != nil {
		return false, err
	}
	return source.applicable && value.publicationAuthorized && value.event == "pending" && value.attempts < 5 &&
		(!value.nextAttempt.Valid || !time.Now().Before(value.nextAttempt.Time)) && source.visibility == "public" &&
		source.input.PrivacyHoldReason == "" && source.input.PolicyHoldReason == "" && !source.input.AnalysisSuppressed, nil
}

func (s *Store) MarkPublished(ctx context.Context, seq int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	value, err := readStored(ctx, tx, seq, true)
	if err != nil {
		return err
	}
	if value == nil || !value.publicationAuthorized {
		return fmt.Errorf("report publication was not authorized")
	}
	source, err := readSource(ctx, tx, seq, value)
	if err != nil {
		return err
	}
	if source.visibility != "public" || source.input.PolicyHoldReason != "" || source.input.PrivacyHoldReason != "" || source.input.AnalysisSuppressed {
		return fmt.Errorf("report publication authorization changed")
	}
	value.outcome.Publication.Status = "public"
	analysis, err := storedAnalysis(ctx, tx, seq)
	if err != nil {
		return err
	}
	path := canonicalPath(analysis.Classification, source.publicID)
	if !analysis.Available || path == "" {
		return fmt.Errorf("published report classification is unavailable")
	}
	value.outcome.Publication.CanonicalPath = path
	value.outcome.Final = value.handling.Notices != "automatic"
	if value.outcome.Final {
		value.outcome.NextAction = NextAction{Type: "none"}
	}
	raw, err := json.Marshal(value.outcome)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE report_outcomes SET outcome_json=?,publication_status='public',
	 public_event_status='published',public_event_attempts=public_event_attempts+1,public_event_error_code=NULL,
	 public_event_next_attempt_at=NULL,match_reconciled=FALSE,reconcile_pending=TRUE,
	 next_reconcile_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND) WHERE report_seq=?`, raw, seq); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE report_raw SET analysed_published_at=COALESCE(analysed_published_at,NOW()) WHERE report_seq=?`, seq); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkPublicationFailed(ctx context.Context, seq int, reasonCode string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	value, err := readStored(ctx, tx, seq, true)
	if err != nil {
		return err
	}
	if value == nil || value.event != "pending" {
		return nil
	}
	attempts := value.attempts + 1
	state := "pending"
	pending := true
	if attempts >= 5 {
		state = "failed"
		pending = false
		value.outcome.Final = true
		value.outcome.NextAction = NextAction{Type: "none"}
		value.outcome.Publication.ReasonCode = "publication_event_failed"
		for index := range value.outcome.Delivery {
			if value.handling.Notices == "automatic" {
				value.outcome.Delivery[index].Status = "failed"
				value.outcome.Delivery[index].ReasonCode = "publication_event_failed"
			}
		}
	}
	raw, err := json.Marshal(value.outcome)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE report_outcomes SET public_event_attempts=?,public_event_status=?,
	 public_event_error_code='publication_enqueue_failed',public_event_next_attempt_at=?,outcome_json=?,reconcile_pending=?,
	 next_reconcile_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND) WHERE report_seq=?`,
		attempts, state, time.Now().UTC().Add(publicationDelay(attempts)), raw, pending, seq)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Pending(ctx context.Context, limit int) ([]int, error) {
	if limit < 1 {
		limit = 10
	}
	if limit > 10 {
		limit = 10
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT report_seq FROM report_outcomes
	 WHERE reconcile_pending=TRUE AND next_reconcile_at<=UTC_TIMESTAMP(6)
	 ORDER BY next_reconcile_at,report_seq LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return nil, err
	}
	var candidates []int
	for rows.Next() {
		var seq int
		if err = rows.Scan(&seq); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, seq)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	result := []int{}
	for _, seq := range candidates {
		if len(result) >= limit {
			break
		}
		if _, err = tx.ExecContext(ctx, `UPDATE report_outcomes SET next_reconcile_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND) WHERE report_seq=?`, seq); err != nil {
			return nil, err
		}
		var ready bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_analysis WHERE seq=? AND language='en')`, seq).Scan(&ready); err != nil {
			return nil, err
		}
		if ready {
			result = append(result, seq)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) Get(ctx context.Context, seq int) (*Outcome, error) {
	value, err := readStored(ctx, s.db, seq, false)
	if err != nil {
		return nil, err
	}
	source, err := readSource(ctx, s.db, seq, value)
	if err != nil {
		return nil, err
	}
	if !source.applicable {
		return nil, sql.ErrNoRows
	}
	var outcome Outcome
	if value == nil {
		analysis, err := storedAnalysis(ctx, s.db, seq)
		if err != nil {
			return nil, err
		}
		outcome = Review(source.input, analysis, time.Now().UTC())
	} else {
		outcome = value.outcome
		if outcome.Processing.Status == "pending_analysis" {
			analysis, err := storedAnalysis(ctx, s.db, seq)
			if err != nil {
				return nil, err
			}
			// English analysis can precede evidence/translation persistence and
			// durable Complete. A reader must not invent a terminal decision in
			// that window, or agents would stop polling before the real outcome.
			if analysis.Available {
				outcome.Processing = Processing{Status: "analyzed", CompletedAt: analysis.CompletedAt}
				outcome.Decision = &Decision{State: Pending, ReasonCode: "decision_pending", Message: "Analysis is saved; the automated decision is pending.", PolicyVersion: PolicyVersion}
				outcome.Final = false
				outcome.NextAction = NextAction{Type: "wait"}
			}
		}
	}
	if outcome.Publication.Status == "public" && (source.visibility != "public" || source.input.PrivacyHoldReason != "" || source.input.PolicyHoldReason != "") {
		outcome.Publication = Publication{Status: "held", ReasonCode: "publication_restricted"}
	}
	if outcome.Relationship != nil {
		outcome.Relationship.Candidates, err = visibleCandidates(ctx, s.db, outcome.Relationship.Candidates)
		if err != nil {
			return nil, err
		}
	}
	if err := s.delivery(ctx, seq, &outcome, value != nil && value.event == "pending"); err != nil {
		return nil, err
	}
	return &outcome, nil
}

func storedAnalysis(ctx context.Context, q queryer, seq int) (Analysis, error) {
	var result Analysis
	err := q.QueryRowContext(ctx, `SELECT a.source,a.is_valid,a.classification,COALESCE(a.title,''),COALESCE(a.description,''),
	 COALESCE(a.summary,''),COALESCE(a.brand_name,''),COALESCE(e.report_kind,'unknown'),
	 COALESCE(e.entity_relationship,'unknown'),COALESCE(e.evidence_version,0),a.created_at
	 FROM report_analysis a LEFT JOIN report_evidence e ON e.seq=a.seq
	 WHERE a.seq=? AND a.language='en' ORDER BY a.updated_at DESC,a.source LIMIT 1`, seq).
		Scan(&result.Source, &result.IsValid, &result.Classification, &result.Title, &result.Description, &result.Summary,
			&result.BrandName, &result.ReportKind, &result.EntityRelationship, &result.EvidenceVersion, &result.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	result.Available = err == nil
	return result, err
}

func canonicalPath(classification, publicID string) string {
	if !strings.HasPrefix(publicID, "rpt_") || strings.ContainsAny(publicID, "/?# ") {
		return ""
	}
	if classification != "physical" && classification != "digital" {
		return ""
	}
	return "/" + classification + "/report/" + publicID
}
