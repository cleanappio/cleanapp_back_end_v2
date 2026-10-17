package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestProjectedEvidenceCompleteCohortsAcrossAllocationBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 1023, 1024, 1025, 3089} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows := sqlmock.NewRows(projectedTestColumns)
			wantMissing := []int{}
			for seq := 1; seq <= count; seq++ {
				row := projectedTestRow(seq)
				if seq%37 == 0 {
					row[12] = false
					wantMissing = append(wantMissing, seq)
				}
				if seq%97 == 0 {
					row[2], row[3], row[5] = nil, nil, nil
					row[21], row[22], row[23], row[24] = nil, nil, nil, nil
				}
				rows.AddRow(row...)
			}
			mock.ExpectBegin()
			mock.ExpectQuery("WITH preferred").WillReturnRows(rows).RowsWillBeClosed()
			mock.ExpectRollback()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
			all, selections, bySeq, missing, err := loadProjectedEvidenceBase(context.Background(), tx, f)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != count || len(selections) != count || len(bySeq) != count || !reflect.DeepEqual(missing, wantMissing) {
				t.Fatal("stream lost reports, selected analyses, or exact fallback identities")
			}
			for i, record := range all {
				seq := i + 1
				if record.Seq != seq || bySeq[seq] != i || record.SourceKey != fmt.Sprintf("url:example.test/report/%d", seq) || record.Brand != "claude" || record.Classification != "digital" || record.Kind != "firsthand" || record.PublicID != "" || record.SourceURL != "" {
					t.Fatalf("report identity or retained row changed at %d", seq)
				}
				null := seq%97 == 0
				if record.SeverityNull != null || selections[i].languageNull != null || selections[i].created.Valid == null {
					t.Fatalf("nullable selected analysis changed at %d", seq)
				}
				if !null && (record.Severity != 0.7 || record.Language != "fr" || selections[i].created.String != "2026-10-15 00:00:00.000000") {
					t.Fatalf("selected analysis tuple ownership changed at %d", seq)
				}
				if seq%37 != 0 && (record.PublishedAt == nil || record.PublishedAt.Nanosecond() != 123456000 || !record.IngestedAtNull) {
					t.Fatalf("retained date ownership or NULL state changed at %d", seq)
				}
			}
			_ = tx.Rollback()
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProjectedEvidenceLateStreamFailureNeverReturnsCompletedChunks(t *testing.T) {
	for _, mode := range []string{"stream_error", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows := sqlmock.NewRows(projectedTestColumns)
			for seq := 1; seq <= 1025; seq++ {
				rows.AddRow(projectedTestRow(seq)...)
			}
			if mode == "stream_error" {
				rows.RowError(1024, errors.New("late stream failure"))
			} else {
				inconsistent := projectedTestRow(1)
				inconsistent[18] = "different-source"
				rows.AddRow(inconsistent...)
			}
			mock.ExpectBegin()
			mock.ExpectQuery("WITH preferred").WillReturnRows(rows).RowsWillBeClosed()
			mock.ExpectRollback()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
			all, selections, bySeq, missing, err := loadProjectedEvidenceBase(context.Background(), tx, f)
			if err == nil || all != nil || selections != nil || bySeq != nil || missing != nil {
				t.Fatal("partial or duplicated cohort escaped as a complete result")
			}
			_ = tx.Rollback()
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProjectedEvidencePreferredAnalysisReplacesEarlierChunksWithoutDuplicateMisses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows := sqlmock.NewRows(projectedTestColumns)
	for seq := 1; seq <= 1025; seq++ {
		row := projectedTestRow(seq)
		if seq == 1025 {
			row[12] = false
		}
		rows.AddRow(row...)
	}
	newer := time.Date(2026, 10, 16, 0, 0, 0, 123456000, time.UTC)
	for _, seq := range []int{1, 1025} {
		row := projectedTestRow(seq)
		row[2], row[5], row[24] = 0.95, "2026-10-16 00:00:00.123456", newer
		if seq == 1025 {
			row[12] = false
		}
		rows.AddRow(row...)
	}
	// A later losing analysis must not undo the winning record in chunk0.
	rows.AddRow(projectedTestRow(1)...)
	mock.ExpectBegin()
	mock.ExpectQuery("WITH preferred").WillReturnRows(rows).RowsWillBeClosed()
	mock.ExpectRollback()
	tx, _ := db.Begin()
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Language: "fr", Kind: "all"})
	all, selections, bySeq, missing, err := loadProjectedEvidenceBase(context.Background(), tx, f)
	if err != nil || len(all) != 1025 || len(selections) != 1025 || len(bySeq) != 1025 || !reflect.DeepEqual(missing, []int{1025}) {
		t.Fatalf("streamed analyses changed the full report or dirty fallback scope: %v", err)
	}
	for _, seq := range []int{1, 1025} {
		i := bySeq[seq]
		if all[i].Seq != seq || all[i].SourceKey != fmt.Sprintf("url:example.test/report/%d", seq) || all[i].Severity != 0.95 || selections[i].created.String != "2026-10-16 00:00:00.123456" || !selections[i].createdTime.Valid || !selections[i].createdTime.Time.Equal(newer) {
			t.Fatalf("preferred tuple replacement missed or corrupted chunk for report%d", seq)
		}
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceProjectionDateGrammarAndCalendar(t *testing.T) {
	for _, literal := range []string{
		"1000-01-01 00:00:00", "2000-02-29 23:59:59", "9999-12-31 23:59:59.999999",
		"2026-10-15 12:30:45.0", "2026-10-15 12:30:45.12", "2026-10-15 12:30:45.123", "2026-10-15 12:30:45.1234", "2026-10-15 12:30:45.12345",
	} {
		got, ok := sourceProjectionDate(sql.NullString{String: literal, Valid: true})
		want, err := time.ParseInLocation("2006-01-02 15:04:05.999999", literal, time.UTC)
		if err != nil || !ok || got == nil || !got.Equal(want) {
			t.Fatalf("valid SQL literal changed: %q", literal)
		}
	}
	for _, literal := range []string{
		"0000-00-00 00:00:00", "0000-00-00 00:00:00.0", "0000-00-00 00:00:00.00", "0000-00-00 00:00:00.000",
		"0000-00-00 00:00:00.0000", "0000-00-00 00:00:00.00000", "0000-00-00 00:00:00.000000",
	} {
		got, ok := sourceProjectionDate(sql.NullString{String: literal, Valid: true})
		if !ok || got == nil || !got.IsZero() {
			t.Fatalf("SQL zero date lost its non-NULL state: %q", literal)
		}
	}
	for _, literal := range []string{
		"", "2026-10-15 12:30:45.", "2026-10-15 12:30:45,1", "2026-10-15 12:30:45.1234567",
		"2026-10-15T12:30:45", "2026-10-15 12:30:45Z", "2026-10-15 12:30:45.1 ", "２０２６-10-15 12:30:45",
		"0999-12-31 23:59:59", "0000-01-01 00:00:00", "0000-00-00 00:00:00.000001",
		"2026-00-01 00:00:00", "2026-01-00 00:00:00", "2026-02-29 00:00:00", "1900-02-29 00:00:00",
		"2026-04-31 00:00:00", "2026-10-15 24:00:00", "2026-10-15 12:60:00", "2026-10-15 12:30:60",
	} {
		if _, ok := sourceProjectionDate(sql.NullString{String: literal, Valid: true}); ok {
			t.Fatalf("malformed cache date must trigger exact fallback: %q", literal)
		}
	}
}

var projectedAllocationBenchmarkRecords []evidenceRecord
var projectedAllocationBenchmarkDate *time.Time
var projectedAllocationBenchmarkSelections []evidenceAnalysisSelection

func TestProjectedEvidenceSelectionChunksKeepCompleteOwnedTuples(t *testing.T) {
	for _, count := range []int{0, 1, 1023, 1024, 1025, 3089} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var accumulator projectedEvidenceSelectionAccumulator
			want := make([]evidenceAnalysisSelection, 0, count)
			for i := 0; i < count; i++ {
				value := evidenceAnalysisSelection{
					created: sql.NullString{String: fmt.Sprint("literal_", i), Valid: i%3 != 0}, languageNull: i%5 == 0,
					requested: sql.NullInt64{Int64: int64(i % 2), Valid: i%5 != 0}, english: sql.NullInt64{Int64: int64(i % 3), Valid: i%5 != 0},
					languageWeight: []byte(fmt.Sprint("native_weight_", i)), createdTime: sql.NullTime{Time: time.Unix(int64(i), 123456000), Valid: i%7 != 0}, sourceClean: i%11 != 0,
				}
				want = append(want, value)
				accumulator.add(value)
			}
			for _, index := range []int{0, 1023, 1024, 2048} {
				if index >= count {
					continue
				}
				replacement := want[index]
				replacement.created.String = fmt.Sprint("replacement_", index)
				replacement.languageWeight = []byte(fmt.Sprint("chosen_weight_", index))
				*accumulator.selection(index) = replacement
				want[index] = replacement
			}
			if accumulator.length != count || !reflect.DeepEqual(accumulator.selections(), want) {
				t.Fatal("chunk selection retention/replacement changed nullable or owned comparison fields")
			}
		})
	}
}

func BenchmarkProjectedEvidenceSelectionAccumulation(b *testing.B) {
	for _, count := range []int{12, 53173, 61041} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			for _, strategy := range []string{"growing", "chunked"} {
				b.Run(strategy, func(b *testing.B) {
					b.ReportAllocs()
					b.ReportMetric(float64(unsafe.Sizeof(evidenceAnalysisSelection{})), "tuple_B")
					for n := 0; n < b.N; n++ {
						if strategy == "growing" {
							selections := make([]evidenceAnalysisSelection, 0, 1024)
							for i := 0; i < count; i++ {
								selections = append(selections, evidenceAnalysisSelection{created: sql.NullString{String: "literal", Valid: true}})
							}
							projectedAllocationBenchmarkSelections = selections
						} else {
							var selections projectedEvidenceSelectionAccumulator
							for i := 0; i < count; i++ {
								selections.add(evidenceAnalysisSelection{created: sql.NullString{String: "literal", Valid: true}})
							}
							projectedAllocationBenchmarkSelections = selections.selections()
						}
					}
				})
			}
		})
	}
}

func BenchmarkProjectedEvidenceAccumulation(b *testing.B) {
	for _, count := range []int{12, 53173, 61042} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			for _, strategy := range []string{"growing", "chunked"} {
				b.Run(strategy, func(b *testing.B) {
					b.ReportAllocs()
					for n := 0; n < b.N; n++ {
						if strategy == "growing" {
							// Reference the former capacity1024 append behavior.
							records := make([]evidenceRecord, 0, 1024)
							for seq := 0; seq < count; seq++ {
								records = append(records, evidenceRecord{Seq: seq})
							}
							projectedAllocationBenchmarkRecords = records
						} else {
							var records projectedEvidenceAccumulator
							for seq := 0; seq < count; seq++ {
								records.add(evidenceRecord{Seq: seq})
							}
							projectedAllocationBenchmarkRecords = records.records()
						}
					}
				})
			}
		})
	}
}

func BenchmarkSourceProjectionDate(b *testing.B) {
	value := sql.NullString{String: "2026-10-15 12:30:45.123456", Valid: true}
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		projectedAllocationBenchmarkDate, _ = sourceProjectionDate(value)
	}
}
