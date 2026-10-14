package database

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// normalizeEvidenceSources skips JSON/regular-expression evaluation only for
// the authoritative archive shape that has no Wire metadata or source URL.
// Every other input retains the executable MySQL normalization oracle.
func normalizeEvidenceSources(ctx context.Context, tx *sql.Tx, inputs []evidenceSourceNormalizationRow) (map[int]evidenceRecord, error) {
	output := make(map[int]evidenceRecord, len(inputs))
	seen := make(map[int]bool, len(inputs))
	fallback := make([]evidenceSourceNormalizationRow, 0)
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[input.Seq] {
			return nil, fmt.Errorf("duplicate compact source sequence")
		}
		seen[input.Seq] = true
		if record, ok := normalizeTrustedArchiveSource(input); ok {
			output[input.Seq] = record
		} else {
			fallback = append(fallback, input)
		}
	}
	other, err := normalizeEvidenceSourcesSQL(ctx, tx, fallback)
	if err != nil {
		return nil, err
	}
	for seq, record := range other {
		output[seq] = record
	}
	return output, nil
}

func normalizeTrustedArchiveSource(input evidenceSourceNormalizationRow) (evidenceRecord, bool) {
	if input.Seq <= 0 || input.Seq > 2147483647 || input.EiiSource == nil || *input.EiiSource != "reddit_archive" || input.EiiID == nil ||
		(input.EiiURL != nil && *input.EiiURL != "") || input.WireActor != "" || input.WireChannel != "" || input.WireObserved != nil || input.WireSubmitted != nil ||
		len(input.WireReport) != 0 || len(input.WireProvenance) != 0 || len(input.WireExtensions) != 0 {
		return evidenceRecord{}, false
	}
	if !archiveSourceField(input.ReportID, 255) || !archiveSourceField(input.FetcherID, 64) || !archiveSourceOptionalField(input.RawSourceType, 64) || !archiveSourceOptionalField(input.RawSourceID, 255) {
		return evidenceRecord{}, false
	}
	if !strings.HasPrefix(input.ReportID, "reddit_t1_") && !strings.HasPrefix(input.ReportID, "reddit_t3_") {
		return evidenceRecord{}, false
	}
	original := input.ReportID[len("reddit_"):]
	if len(original) <= 3 {
		return evidenceRecord{}, false
	}
	for _, c := range original[3:] {
		if c < 'a' || c > 'z' {
			if c < '0' || c > '9' {
				return evidenceRecord{}, false
			}
		}
	}
	digest := sha1.Sum([]byte("reddit:" + original))
	identity := hex.EncodeToString(digest[:])[:32]
	if *input.EiiID != identity {
		return evidenceRecord{}, false
	}
	reportTS, ok := archiveSourceDate(input.ReportTS)
	if !ok {
		return evidenceRecord{}, false
	}
	published, ok := archiveSourceDate(input.EiiPublished)
	if !ok {
		return evidenceRecord{}, false
	}
	created, ok := archiveSourceDate(input.EiiCreated)
	if !ok {
		return evidenceRecord{}, false
	}
	if published == nil {
		published = reportTS
	}
	ingested := created
	if ingested == nil {
		ingested = reportTS
	}
	record := evidenceRecord{Seq: input.Seq, SourcePlatform: "reddit_archive", SourceKey: "source:reddit_archive:" + input.FetcherID + ":" + identity,
		ExternalSource: true, FetcherID: input.FetcherID, PublishedAt: published, IngestionTimeMissing: created == nil, IngestedAtNull: ingested == nil}
	if ingested != nil {
		record.IngestedAt = *ingested
	}
	return record, true
}

func archiveSourceField(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit
}

func archiveSourceOptionalField(value *string, limit int) bool {
	return value == nil || archiveSourceField(*value, limit)
}

func archiveSourceDate(value *string) (*time.Time, bool) {
	if value == nil {
		return nil, true
	}
	if *value == "0000-00-00 00:00:00.000000" || *value == "0000-00-00 00:00:00" {
		zero := time.Time{}
		return &zero, true
	}
	const layout = "2006-01-02 15:04:05.000000"
	if len(*value) != len(layout) {
		return nil, false
	}
	parsed, err := time.ParseInLocation(layout, *value, time.UTC)
	if err != nil || parsed.Year() < 1000 || parsed.Format(layout) != *value {
		return nil, false
	}
	return &parsed, true
}
