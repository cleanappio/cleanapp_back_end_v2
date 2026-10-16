package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The covering source indexes determine singleton choices without any header,
// timestamp, JSON or global sort. Only ambiguous groups need native SQL order.
// This compact DISTINCT scope is shared by both UNION branches. Publication is
// deliberately evaluated later, by the complete fused report read.
func hybridEvidenceSourceIdentityQuery(f EvidenceFilter) (string, []interface{}) {
	marks := make([]string, len(f.Brands))
	args := make([]interface{}, len(f.Brands))
	for i, brand := range f.Brands {
		marks[i], args[i] = "?", brand
	}
	query := `WITH identity_scope AS (
 SELECT DISTINCT seq FROM report_analysis FORCE INDEX (` + evidenceBrandCoreIndex + `)
 WHERE brand_name IN (` + strings.Join(marks, ",") + `) AND is_valid=TRUE AND classification='digital' AND seq IS NOT NULL
 ) SELECT /*+ MAX_EXECUTION_TIME(8000) */ 1 AS source_kind,e.seq,e.source,e.external_id,NULL AS wire_id
 FROM identity_scope p STRAIGHT_JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=p.seq
 UNION ALL SELECT 2,w.report_seq,NULL,NULL,w.submission_id
 FROM identity_scope p STRAIGHT_JOIN wire_submissions_raw w ON w.report_seq=p.seq`
	return query, args
}

type hybridEvidenceSourceChoice struct {
	Seq        int     `json:"seq"`
	Source     *string `json:"source"`
	ExternalID *string `json:"external_id"`
	WireID     *string `json:"wire_submission_id"`
	eiiCount   int
	wireCount  int
}

// Bound the one prepared-statement parameter without truncating the report
// universe. Larger cohorts fail closed rather than sending an oversized packet
// or reporting partial exact counts. The server may impose a smaller packet
// limit; that query failure is propagated by the same transaction.
const hybridEvidenceSourcePayloadLimit = 8 << 20

func encodeHybridEvidenceSourceChoices(selected []*hybridEvidenceSourceChoice) (string, error) {
	// Fixed-position tuples avoid repeating long field names for every report.
	// Pointers still encode absent identities as JSON null, never empty strings.
	tuples := make([][4]interface{}, len(selected))
	for i, choice := range selected {
		tuples[i] = [4]interface{}{choice.Seq, choice.Source, choice.ExternalID, choice.WireID}
	}
	encoded, err := json.Marshal(tuples)
	if err != nil {
		return "", err
	}
	if len(encoded) > hybridEvidenceSourcePayloadLimit {
		return "", fmt.Errorf("hybrid source identity parameter exceeds its byte budget")
	}
	return string(encoded), nil
}

// JSON_TABLE column collations match the established projection hydration
// contracts. These values are identities already selected on native columns;
// JSON collation never participates in choosing a winner.
const hybridEvidenceChosenSourcesCTE = `chosen_source_ids AS (
 SELECT * FROM JSON_TABLE(?, '$[*]' COLUMNS(
 seq INT PATH '$[0]',
 source VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$[1]',
 external_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$[2]',
 wire_submission_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$[3]')) selected
 ), source_winners AS (
 SELECT /*+ NO_MERGE(chosen) */ r.*,chosen.source,chosen.external_id,chosen.wire_submission_id
 FROM metadata r LEFT JOIN chosen_source_ids chosen ON chosen.seq=r.seq
 )`

func loadHybridEvidenceSourceChoices(ctx context.Context, tx *sql.Tx, f EvidenceFilter) (string, error) {
	query, args := hybridEvidenceSourceIdentityQuery(f)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("read covered hybrid source identities: %w", err)
	}
	choices := map[int]*hybridEvidenceSourceChoice{}
	for rows.Next() {
		var kind, seq int
		var source, id, wire sql.NullString
		if err := rows.Scan(&kind, &seq, &source, &id, &wire); err != nil {
			rows.Close()
			return "", err
		}
		choice := choices[seq]
		if choice == nil {
			choice = &hybridEvidenceSourceChoice{Seq: seq}
			choices[seq] = choice
		}
		switch {
		case kind == 1 && source.Valid && id.Valid && !wire.Valid:
			choice.eiiCount++
			if choice.eiiCount == 1 {
				choice.Source, choice.ExternalID = nullableEvidenceString(source), nullableEvidenceString(id)
			}
		case kind == 2 && !source.Valid && !id.Valid && wire.Valid:
			choice.wireCount++
			if choice.wireCount == 1 {
				choice.WireID = nullableEvidenceString(wire)
			}
		default:
			rows.Close()
			return "", fmt.Errorf("hybrid source identity is incomplete")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	eiiDuplicates, wireDuplicates := []int{}, []int{}
	ids := make([]int, 0, len(choices))
	for seq, choice := range choices {
		ids = append(ids, seq)
		if choice.eiiCount > 1 {
			eiiDuplicates = append(eiiDuplicates, seq)
		}
		if choice.wireCount > 1 {
			wireDuplicates = append(wireDuplicates, seq)
		}
	}
	sort.Ints(ids)
	sort.Ints(eiiDuplicates)
	sort.Ints(wireDuplicates)
	if err := resolveHybridEvidenceDuplicateEII(ctx, tx, choices, eiiDuplicates); err != nil {
		return "", err
	}
	if err := resolveHybridEvidenceDuplicateWire(ctx, tx, choices, wireDuplicates); err != nil {
		return "", err
	}
	selected := make([]*hybridEvidenceSourceChoice, 0, len(ids))
	for _, seq := range ids {
		selected = append(selected, choices[seq])
	}
	return encodeHybridEvidenceSourceChoices(selected)
}

func resolveHybridEvidenceDuplicateEII(ctx context.Context, tx *sql.Tx, choices map[int]*hybridEvidenceSourceChoice, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	// The first tuple in each group uses the original columns' actual SQL
	// collation, including non-ASCII ordering and trailing-space equivalence.
	rows, err := tx.QueryContext(ctx, `SELECT /*+ MAX_EXECUTION_TIME(8000) */ e.seq,e.source,e.external_id
 FROM `+evidenceIDsTable+` STRAIGHT_JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=ids.seq
 ORDER BY ids.seq,e.source,e.external_id`, evidenceIDsJSON(ids))
	if err != nil {
		return fmt.Errorf("choose duplicate hybrid external identities: %w", err)
	}
	defer rows.Close()
	seen := map[int]int{}
	for rows.Next() {
		var seq int
		var source, id string
		if err := rows.Scan(&seq, &source, &id); err != nil {
			return err
		}
		choice := choices[seq]
		if choice == nil || choice.eiiCount <= 1 {
			return fmt.Errorf("duplicate hybrid external identity is outside its scope")
		}
		seen[seq]++
		if seen[seq] == 1 {
			choice.Source, choice.ExternalID = &source, &id
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, seq := range ids {
		if seen[seq] != choices[seq].eiiCount {
			return fmt.Errorf("duplicate hybrid external identities are incomplete")
		}
	}
	return nil
}

func resolveHybridEvidenceDuplicateWire(ctx context.Context, tx *sql.Tx, choices map[int]*hybridEvidenceSourceChoice, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	// A matching projection's NULL timestamp remains authoritative. A stale
	// report link cannot match; raw fallback is bound to that same submission
	// and report. Select latest before privacy, with no new timestamp tie rule.
	rows, err := tx.QueryContext(ctx, `SELECT /*+ MAX_EXECUTION_TIME(8000) */ w.report_seq,w.submission_id
 FROM `+evidenceIDsTable+` STRAIGHT_JOIN wire_submissions_raw w ON w.report_seq=ids.seq
 LEFT JOIN wire_evidence_projection h ON h.submission_id=w.submission_id AND h.report_seq=w.report_seq
 LEFT JOIN wire_submissions_raw fallback ON fallback.submission_id=CASE WHEN h.submission_id IS NULL THEN w.submission_id ELSE NULL END AND fallback.report_seq=w.report_seq
 ORDER BY ids.seq,CASE WHEN h.submission_id IS NOT NULL THEN h.submitted_at ELSE fallback.submitted_at END DESC`, evidenceIDsJSON(ids))
	if err != nil {
		return fmt.Errorf("choose duplicate hybrid Wire identities: %w", err)
	}
	defer rows.Close()
	seen := map[int]int{}
	for rows.Next() {
		var seq int
		var id string
		if err := rows.Scan(&seq, &id); err != nil {
			return err
		}
		choice := choices[seq]
		if choice == nil || choice.wireCount <= 1 {
			return fmt.Errorf("duplicate hybrid Wire identity is outside its scope")
		}
		seen[seq]++
		if seen[seq] == 1 {
			choice.WireID = &id
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, seq := range ids {
		if seen[seq] != choices[seq].wireCount {
			return fmt.Errorf("duplicate hybrid Wire identities are incomplete")
		}
	}
	return nil
}
