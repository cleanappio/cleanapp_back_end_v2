package database

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func archiveTestString(value string) *string { return &value }

func archiveTestInput(seq int) evidenceSourceNormalizationRow {
	original := "t1_original"
	digest := sha1.Sum([]byte("reddit:" + original))
	return evidenceSourceNormalizationRow{Seq: seq, ReportID: "reddit_" + original, FetcherID: "archive-import",
		EiiSource: archiveTestString("reddit_archive"), EiiID: archiveTestString(hex.EncodeToString(digest[:])[:32]),
		ReportTS: archiveTestString("2024-04-06 12:34:56.123456"), EiiCreated: archiveTestString("2026-09-14 12:34:56.000000")}
}

func TestArchiveFastPathPreservesPublicationIngestionAndNullPresence(t *testing.T) {
	input := archiveTestInput(1)
	record, ok := normalizeTrustedArchiveSource(input)
	if !ok || record.SourceKey != "source:reddit_archive:archive-import:"+*input.EiiID || !record.ExternalSource || record.PrivacyRestricted || record.SourceAccountKey != "" || record.SourceURL != "" || record.ObservedAt != nil {
		t.Fatal("archive identity/provenance changed")
	}
	if record.PublishedAt == nil || record.PublishedAt.Year() != 2024 || record.IngestedAt.Year() != 2026 || record.IngestionTimeMissing || record.IngestedAtNull {
		t.Fatal("archive publication was confused with ingestion")
	}
	input.EiiPublished = archiveTestString("2025-01-02 00:00:00.000000")
	input.EiiCreated = nil
	record, ok = normalizeTrustedArchiveSource(input)
	if !ok || record.PublishedAt.Year() != 2025 || record.IngestedAt.Year() != 2024 || !record.IngestionTimeMissing || record.IngestedAtNull {
		t.Fatal("explicit publication or missing-ingestion fallback changed")
	}
	input.ReportTS = nil
	input.EiiPublished = nil
	record, ok = normalizeTrustedArchiveSource(input)
	if !ok || record.PublishedAt != nil || !record.IngestedAtNull || !record.IngestionTimeMissing {
		t.Fatal("NULL dates became known dates")
	}
	input.ReportTS = archiveTestString("0000-00-00 00:00:00.000000")
	input.EiiCreated = archiveTestString("0000-00-00 00:00:00")
	record, ok = normalizeTrustedArchiveSource(input)
	if !ok || record.PublishedAt == nil || !record.PublishedAt.IsZero() || !record.IngestedAt.IsZero() || record.IngestedAtNull || record.IngestionTimeMissing {
		t.Fatal("stored zero dates lost their non-NULL presence")
	}
}

func TestArchiveFastPathRejectsUnsupportedShapes(t *testing.T) {
	changes := map[string]func(*evidenceSourceNormalizationRow){
		"wrong_source": func(v *evidenceSourceNormalizationRow) { v.EiiSource = archiveTestString("reddit_dump") },
		"case_variant": func(v *evidenceSourceNormalizationRow) { v.EiiSource = archiveTestString("Reddit_archive") },
		"wrong_hash":   func(v *evidenceSourceNormalizationRow) { v.EiiID = archiveTestString(strings.Repeat("0", 32)) },
		"padded_hash":  func(v *evidenceSourceNormalizationRow) { v.EiiID = archiveTestString(*v.EiiID + " ") },
		"url":          func(v *evidenceSourceNormalizationRow) { v.EiiURL = archiveTestString("https://reddit.com/source") },
		"wire_null":    func(v *evidenceSourceNormalizationRow) { v.WireReport = json.RawMessage("null") },
		"wire_privacy": func(v *evidenceSourceNormalizationRow) {
			v.WireReport = json.RawMessage(`{"privacy":{"requires_redaction":true}}`)
		},
		"actor":        func(v *evidenceSourceNormalizationRow) { v.WireActor = "human" },
		"channel":      func(v *evidenceSourceNormalizationRow) { v.WireChannel = "wire" },
		"observed":     func(v *evidenceSourceNormalizationRow) { v.WireObserved = v.ReportTS },
		"submitted":    func(v *evidenceSourceNormalizationRow) { v.WireSubmitted = v.EiiCreated },
		"provenance":   func(v *evidenceSourceNormalizationRow) { v.WireProvenance = json.RawMessage(`{}`) },
		"extensions":   func(v *evidenceSourceNormalizationRow) { v.WireExtensions = json.RawMessage(`{}`) },
		"width":        func(v *evidenceSourceNormalizationRow) { v.FetcherID = strings.Repeat("x", 65) },
		"raw_width":    func(v *evidenceSourceNormalizationRow) { v.RawSourceID = archiveTestString(strings.Repeat("x", 256)) },
		"invalid_utf8": func(v *evidenceSourceNormalizationRow) { v.FetcherID = string([]byte{0xff}) },
		"partial_zero": func(v *evidenceSourceNormalizationRow) {
			v.EiiCreated = archiveTestString("2026-00-00 00:00:00.000000")
		},
		"rfc_date":  func(v *evidenceSourceNormalizationRow) { v.EiiCreated = archiveTestString("2026-09-14T12:34:56Z") },
		"sql_width": func(v *evidenceSourceNormalizationRow) { v.Seq = 2147483648 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			input := archiveTestInput(1)
			change(&input)
			if _, ok := normalizeTrustedArchiveSource(input); ok {
				t.Fatal("unsupported shape bypassed the SQL oracle")
			}
		})
	}
	input := archiveTestInput(1)
	input.FetcherID = strings.Repeat("é", 64)
	if _, ok := normalizeTrustedArchiveSource(input); !ok {
		t.Fatal("valid VARCHAR character width was incorrectly treated as bytes")
	}
}

func TestEvidenceArchiveNormalizationMySQLFixture(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set guarded local EVIDENCE_TEST_DSN")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_evidence_test") {
		t.Fatal("fixture only permits dedicated local databases ending _evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	inputs := []evidenceSourceNormalizationRow{}
	mutations := []func(*evidenceSourceNormalizationRow){
		func(v *evidenceSourceNormalizationRow) {},
		func(v *evidenceSourceNormalizationRow) { v.EiiURL = archiveTestString("") },
		func(v *evidenceSourceNormalizationRow) {
			v.EiiPublished = archiveTestString("2025-01-02 03:04:05.678901")
		},
		func(v *evidenceSourceNormalizationRow) { v.EiiCreated = nil },
		func(v *evidenceSourceNormalizationRow) { v.EiiCreated = nil; v.ReportTS = nil },
		func(v *evidenceSourceNormalizationRow) { v.ReportTS = archiveTestString("0000-00-00 00:00:00.000000") },
		func(v *evidenceSourceNormalizationRow) { v.EiiCreated = archiveTestString("0000-00-00 00:00:00") },
		func(v *evidenceSourceNormalizationRow) {
			v.EiiPublished = archiveTestString("0000-00-00 00:00:00.000000")
		},
		func(v *evidenceSourceNormalizationRow) { v.FetcherID = strings.Repeat("é", 64) },
		func(v *evidenceSourceNormalizationRow) { v.EiiID = archiveTestString(strings.ToUpper(*v.EiiID)) },
		func(v *evidenceSourceNormalizationRow) { v.EiiSource = archiveTestString("Reddit_archive") },
		func(v *evidenceSourceNormalizationRow) {
			v.WireReport = json.RawMessage(`{"privacy":{"requires_redaction":true}}`)
		},
		func(v *evidenceSourceNormalizationRow) {
			v.EiiURL = archiveTestString("https://reddit.com/original?tracking=one")
		},
		func(v *evidenceSourceNormalizationRow) { v.FetcherID = strings.Repeat("x", 65) },
		func(v *evidenceSourceNormalizationRow) {
			v.EiiCreated = archiveTestString("2026-00-00 00:00:00.000000")
		},
	}
	for i, change := range mutations {
		input := archiveTestInput(i + 1)
		change(&input)
		inputs = append(inputs, input)
	}
	got, err := normalizeEvidenceSources(ctx, tx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	want, err := normalizeEvidenceSourcesSQL(ctx, tx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		if !reflect.DeepEqual(got[input.Seq], want[input.Seq]) {
			t.Fatalf("archive/fallback result differs from unchanged SQL oracle for case %d", input.Seq)
		}
	}
}

var archiveBenchmarkRecords map[int]evidenceRecord

func BenchmarkEvidenceArchiveBulk15000(b *testing.B) {
	inputs := make([]evidenceSourceNormalizationRow, 15000)
	for i := range inputs {
		inputs[i] = archiveTestInput(i + 1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		records, err := normalizeEvidenceSources(context.Background(), nil, inputs)
		if err != nil || len(records) != len(inputs) || records[1].IngestedAt.Year() != 2026 {
			b.Fatal("archive bulk failed")
		}
		archiveBenchmarkRecords = records
	}
}
