package database

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// loadFreshEvidenceSourceRecords reads only source-normalizer inputs for the
// supplied existing parents. It does not select analysis, inspect publication,
// lock rows, or publish cache generations. A refresher must lock its generation
// rows before its first consistent read; a reader uses its own RR snapshot.
// Deleted/missing parents are omitted, never stamped as a clean empty source.
func loadFreshEvidenceSourceRecords(ctx context.Context, tx *sql.Tx, ids []int) (map[int]evidenceRecord, error) {
	unique := make(map[int]bool, len(ids))
	for _, seq := range ids {
		if seq <= 0 || unique[seq] {
			return nil, fmt.Errorf("fresh source report IDs must be positive and unique")
		}
		unique[seq] = true
	}
	if len(ids) == 0 {
		return map[int]evidenceRecord{}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT ids.seq,
 CASE WHEN m.seq IS NOT NULL THEN m.id ELSE r.id END,
 DATE_FORMAT(CASE WHEN m.seq IS NOT NULL THEN m.ts ELSE r.ts END,'%Y-%m-%d %H:%i:%s.%f'),
 rr.source_type,rr.source_id,COALESCE(rr.fetcher_id,'')
 FROM `+evidenceIDsTable+` JOIN reports r ON r.seq=ids.seq
 LEFT JOIN report_metadata_projection m ON m.seq=ids.seq
 LEFT JOIN report_raw rr ON rr.report_seq=ids.seq`, evidenceIDsJSON(ids))
	if err != nil {
		return nil, fmt.Errorf("read fresh source report metadata: %w", err)
	}
	sources := make(map[int]evidenceSourceNormalizationRow, len(ids))
	existing := make([]int, 0, len(ids))
	for rows.Next() {
		var s evidenceSourceNormalizationRow
		var id, ts, rawType, rawID sql.NullString
		if err := rows.Scan(&s.Seq, &id, &ts, &rawType, &rawID, &s.FetcherID); err != nil {
			rows.Close()
			return nil, err
		}
		if !unique[s.Seq] {
			rows.Close()
			return nil, fmt.Errorf("fresh source metadata is outside requested scope")
		}
		if _, duplicate := sources[s.Seq]; duplicate {
			rows.Close()
			return nil, fmt.Errorf("fresh source metadata has duplicate report")
		}
		s.ReportID, s.ReportTS = id.String, nullableEvidenceString(ts)
		s.RawSourceType, s.RawSourceID = nullableEvidenceString(rawType), nullableEvidenceString(rawID)
		sources[s.Seq] = s
		existing = append(existing, s.Seq)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		return map[int]evidenceRecord{}, nil
	}
	sort.Ints(existing)
	if err := loadEvidenceIndexSources(ctx, tx, existing, sources); err != nil {
		return nil, err
	}
	// The established Wire selector orders duplicates by raw submitted_at DESC.
	// Only after choosing an ID do matching projection fields become authoritative.
	if err := loadEvidenceWireSources(ctx, tx, existing, sources); err != nil {
		return nil, err
	}
	inputs := make([]evidenceSourceNormalizationRow, 0, len(existing))
	for _, seq := range existing {
		inputs = append(inputs, sources[seq])
	}
	normalized, err := normalizeEvidenceSources(ctx, tx, inputs)
	if err != nil {
		return nil, err
	}
	if len(normalized) != len(existing) {
		return nil, fmt.Errorf("fresh source normalization is incomplete")
	}
	for _, seq := range existing {
		if record, ok := normalized[seq]; !ok || record.Seq != seq {
			return nil, fmt.Errorf("fresh source normalization has missing report")
		}
	}
	return normalized, nil
}
