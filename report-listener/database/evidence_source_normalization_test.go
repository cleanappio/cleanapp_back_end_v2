package database

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type compactSourcePayloadCount int

func (expected compactSourcePayloadCount) Match(value driver.Value) bool {
	bytes, ok := value.([]byte)
	if !ok {
		return false
	}
	var inputs []json.RawMessage
	return json.Unmarshal(bytes, &inputs) == nil && len(inputs) == int(expected)
}

func normalizedSourceMockRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"seq", "platform", "url", "published", "observed", "ingested", "missing", "actor", "fetcher", "restricted", "identity", "external", "channel", "generation", "account", "ingested_null"})
}

func TestEvidenceSourceNormalizationEmptyAndBoundedBatchesShareCallerTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	empty, err := normalizeEvidenceSources(context.Background(), tx, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty source scope was not an empty result")
	}
	inputs := make([]evidenceSourceNormalizationRow, 1001)
	first, last := normalizedSourceMockRows(), normalizedSourceMockRows()
	for i := range inputs {
		inputs[i].Seq = i + 1
		rows := first
		if i == 1000 {
			rows = last
		}
		rows.AddRow(i+1, "", "", nil, nil, nil, false, "", "", false, "source", false, "", "", "", true)
	}
	mock.ExpectQuery(`WITH source_rows AS`).WithArgs(compactSourcePayloadCount(1000)).WillReturnRows(first)
	mock.ExpectQuery(`WITH source_rows AS`).WithArgs(compactSourcePayloadCount(1)).WillReturnRows(last)
	got, err := normalizeEvidenceSources(context.Background(), tx, inputs)
	if err != nil || len(got) != 1001 || !got[1].IngestedAt.IsZero() || got[1].PublishedAt != nil || got[1].ObservedAt != nil {
		t.Fatalf("batched metadata or SQL NULL state changed: rows=%d err=%v", len(got), err)
	}
	// The helper cannot commit/rollback the surrounding snapshot transaction.
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceSourceNormalizationRejectsDuplicateIncompleteAndInvalidInput(t *testing.T) {
	for _, mode := range []string{"duplicate", "incomplete", "invalid_json"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			inputs := []evidenceSourceNormalizationRow{{Seq: 1}, {Seq: 2}}
			if mode == "invalid_json" {
				inputs[0].WireReport = json.RawMessage(`{invalid}`)
			} else {
				rows := normalizedSourceMockRows().AddRow(1, "", "", nil, nil, nil, false, "", "", false, "source", false, "", "", "", true)
				if mode == "duplicate" {
					rows.AddRow(1, "", "", nil, nil, nil, false, "", "", false, "source", false, "", "", "", true)
				}
				mock.ExpectQuery(`WITH source_rows AS`).WillReturnRows(rows)
			}
			if _, err := normalizeEvidenceSources(context.Background(), tx, inputs); err == nil {
				t.Fatal("invalid source scope was silently accepted")
			}
			mock.ExpectRollback()
			tx.Rollback()
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEvidenceSourceNormalizationSQLHasNoLegacyTableReadsAndRetainsSourceRules(t *testing.T) {
	query := evidenceSourceNormalizationSQL()
	for _, forbidden := range []string{"FROM report_analysis", "JOIN report_analysis", "FROM reports ", "JOIN reports ", "FROM wire_submissions_raw", "JOIN wire_submissions_raw", "ROW_NUMBER", "ALTER TABLE", "UPDATE "} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("compact normalization unexpectedly reads/writes legacy state: %s", forbidden)
		}
	}
	for _, required := range []string{"JSON_TABLE", "wire_report JSON", "DATETIME(6)", "utf8mb4_bin", "SHA1(CONCAT('reddit:'", "JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.source_published_at') IS NULL", "observation_time_supplied", "source_account_key", "NO_MERGE(candidates)", "NO_MERGE(provenance)", "NO_MERGE(normalized)", "ts IS NULL"} {
		if !strings.Contains(query, required) {
			t.Fatalf("missing exact source normalization rule: %s", required)
		}
	}
}

func TestEvidenceSourceNormalizationPreservesNullableOutputDates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	published := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`WITH source_rows AS`).WillReturnRows(normalizedSourceMockRows().AddRow(1, "reddit_archive", "", published, nil, nil, true, "", "", false, "source:archive", true, "", "", "", true))
	output, err := normalizeEvidenceSources(context.Background(), tx, []evidenceSourceNormalizationRow{{Seq: 1}})
	if err != nil || output[1].PublishedAt == nil || !output[1].PublishedAt.Equal(published) || output[1].ObservedAt != nil || !output[1].IngestedAt.IsZero() || !output[1].IngestedAtNull || !output[1].IngestionTimeMissing {
		t.Fatal("source publication became ingestion or incident time")
	}
	mock.ExpectRollback()
	tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceSourceNormalizationPayloadPreservesNullAndNonNullZeroWithoutMutatingInput(t *testing.T) {
	zeroDate := "0000-00-00 00:00:00.000000"
	input := evidenceSourceNormalizationRow{Seq: 1, ReportTS: nil, EiiPublished: &zeroDate, WireReport: json.RawMessage(`{"digital_context":{"metadata":{"source_published_at":null,"author_username":9007199254740993}}}`)}
	payload, err := evidenceSourceNormalizationPayload([]evidenceSourceNormalizationRow{input})
	if err != nil {
		t.Fatal(err)
	}
	var decoded []evidenceSourceNormalizationRow
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[0].ReportTS != nil || decoded[0].EiiPublished == nil || *decoded[0].EiiPublished != "0001-01-01 00:00:00.000000" || *input.EiiPublished != zeroDate || !strings.Contains(string(decoded[0].WireReport), `"source_published_at":null`) || !strings.Contains(string(decoded[0].WireReport), "9007199254740993") {
		t.Fatal("payload encoding merged null/zero or changed raw JSON/source input")
	}
}
