package evidencebackfill

import (
	"cleanapp-common/evidence"
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
)

func TestBackfillIncludesNonEnglishReportsAndGroupsTranslationsBySequence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// No en-only predicate: a brand report with only und/ja/etc. analyses remains
	// eligible. GROUP BY seq means translations do not inflate label counts.
	mock.ExpectQuery(`SELECT ra.seq, MAX\(ra.brand_name\).*brand_name IN \(\?,\?\).*GROUP BY ra.seq ORDER BY ra.seq ASC LIMIT`).WithArgs(0, "grok", "claude", 50).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand", "title"}).AddRow(4, "grok", "Cannot submit the form").AddRow(5, "claude", "Context history lost"))
	got, err := (DatabaseStore{DB: db}).Candidates(context.Background(), Options{Brands: []string{"grok", "claude"}, Limit: 50})
	if err != nil || len(got) != 2 || got[1].Seq != 5 || got[1].Title != "Context history lost" {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillBoundsSnapshotAndPartitionsByReportSequence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT ra.seq, MAX\(ra.brand_name\).*ra.seq > \? AND ra.seq <= \? AND MOD\(ra.seq, \?\) = \? AND ra.brand_name IN \(\?\).*GROUP BY ra.seq ORDER BY ra.seq ASC LIMIT`).WithArgs(10, 100, 8, 5, "grok", 50).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand", "title"}).AddRow(21, "grok", "One original source"))
	got, err := (DatabaseStore{DB: db}).Candidates(context.Background(), Options{Brands: []string{"grok"}, Limit: 50, AfterSeq: 10, ThroughSeq: 100, ShardCount: 8, ShardIndex: 5})
	if err != nil || len(got) != 1 || got[0].Seq != 21 {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillReadsNewEvidenceVersionWithoutLegacyAnalysisColumns(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "retryable only", true: "force review"}[force], func(t *testing.T) {
			matcher := sqlmock.QueryMatcherFunc(func(_, query string) error {
				if !strings.Contains(query, "LEFT JOIN report_evidence ev ON ev.seq = ra.seq") || strings.Contains(query, "ra.evidence_version") {
					return errors.New("backfill still depends on altering legacy analysis")
				}
				versionFilter := "COALESCE(ev.evidence_version,0) < 1"
				if strings.Contains(query, versionFilter) == force {
					return errors.New("force/retryable evidence version filter is incorrect")
				}
				if !strings.Contains(query, "GROUP BY ra.seq") || strings.Contains(query, "WHERE ra.language") {
					return errors.New("translations are not grouped as one report")
				}
				return nil
			})
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("new per-report evidence candidate query").WithArgs(0, "claude", 10).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand", "title"}).AddRow(5, "claude", "Only translated source"))
			got, err := (DatabaseStore{DB: db}).Candidates(context.Background(), Options{Brands: []string{"claude"}, Limit: 10, Force: force})
			if err != nil || len(got) != 1 || got[0].Seq != 5 {
				t.Fatalf("got %#v err=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBackfillSavesOnePerSequenceEvidenceRowAndNeverRewritesAnalyses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fields := evidence.Fields{ReportKind: "coverage", EvidenceExcerpt: "Original quote", ReportedImpact: "", ClassificationReason: "Relayed incident", EntityRelationship: "mentioned"}
	mock.ExpectExec(`INSERT INTO report_evidence.*ON DUPLICATE KEY UPDATE`).WithArgs(12, "coverage", "Original quote", "", "Relayed incident", "mentioned", evidence.Version).WillReturnResult(sqlmock.NewResult(12, 1))
	if err := (DatabaseStore{DB: db}).Save(context.Background(), 12, fields); err != nil {
		t.Fatal(err)
	}
	// There is no per-language loop or UPDATE report_analysis/report_raw query.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
