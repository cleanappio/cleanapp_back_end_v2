package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

// Runs only inside TestEvidenceMySQLFixture's guarded disposable database.
// The executable SQL oracle, including its binary PADSPACE public-ID check,
// remains authoritative when the parent and projection collations differ.
func testNarrowEvidenceMalformedPublicIDsMySQL(t *testing.T, db *sql.DB) {
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`ALTER TABLE reports MODIFY COLUMN public_id VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`)
	values := []any{"rpt_narrow_6400", "", nil, " ", "   ", "rpt_narrow_6405 ", "rpt_narrow_6406"}
	for i, value := range values {
		seq := 6400 + i
		exec(`INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(?,?,?,'2026-10-15',0,0,'')`, seq, fmt.Sprint("narrow_", seq), value)
		exec(`INSERT INTO report_analysis VALUES(?,'en','narrowidentity','Identity',TRUE,'digital','2026-10-15','Original title','Original summary',0.5)`, seq)
	}
	exec(`UPDATE report_metadata_projection SET public_id=NULL WHERE seq=6406`)
	d := &Database{db: db}
	for _, mode := range []string{"warm", "mixed_parent_fallback"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "mixed_parent_fallback" {
				exec(`DELETE FROM report_metadata_projection WHERE seq BETWEEN 6400 AND 6405`)
			}
			f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "narrowidentity", Kind: "all", Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			got, err := d.getEvidenceBrandPageNarrowSQL(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			want, err := d.getEvidenceBrandPageSQL(context.Background(), f)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("mixed-collation selected-ID oracle parity differs: %v", err)
			}
			ids := map[int]string{}
			for _, report := range got.Reports {
				ids[report.Report.Seq] = report.Report.PublicID
			}
			if !reflect.DeepEqual(ids, map[int]string{6400: "rpt_narrow_6400", 6405: "rpt_narrow_6405 "}) || got.Total != 2 || got.Overview.TotalReports != 2 {
				t.Fatalf("malformed/authoritative-null identity entered counts, or valid ID was rewritten: %+v", got)
			}
		})
	}
}

func testNarrowEvidenceSlimRanksMySQL(t *testing.T, db *sql.DB) {
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for seq := 6500; seq <= 6513; seq++ {
		exec(`INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(?,?,?,'2026-10-15',12.25,-41.5,'')`, seq, fmt.Sprint("slim_", seq), fmt.Sprint("rpt_slim_", seq))
		severity := 0.95 - float64(seq-6502)*0.05
		if seq < 6502 {
			severity = 0.1
		}
		exec(`INSERT INTO report_analysis VALUES(?,'en','narrowslim','Slim Display',TRUE,'digital','2026-10-15',?, ?, ?)`, seq, fmt.Sprint("Title ", seq), fmt.Sprint("Summary ", seq), severity)
		kind, version, excerpt, impact, relationship := "firsthand", 1, "", "", "unknown"
		if seq == 6501 {
			kind, version = "coverage", 2
		}
		if seq == 6500 || seq == 6502 {
			excerpt, impact, relationship = fmt.Sprint("Observation ", seq), "Blocked", "affected"
		}
		exec(`INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES(?,?,?,?,?,?,?)`, seq, kind, excerpt, impact, fmt.Sprint("Reason ", seq), relationship, version)
		url := fmt.Sprintf("https://example.test/slim/%d", seq)
		if seq < 6502 {
			url = "https://example.test/slim/shared"
		}
		exec(`INSERT INTO external_ingest_index(seq,source,external_id,source_url,source_timestamp,created_at) VALUES(?,'custom',?,?,'2026-10-10','2026-10-15')`, seq, fmt.Sprint("slim_", seq), url)
		// A losing import carries the case link onto its coverage winner; kind
		// filtering must happen only after that source winner and case mapping.
		if seq != 6501 {
			exec(`INSERT INTO case_reports VALUES(?,'narrow_slim_family')`, seq)
		}
	}
	// The second-ranked observation remains in the family count but is omitted
	// from the existing first-eight sample slice, leaving seven visible samples.
	exec(`INSERT INTO wire_submissions_raw(submission_id,report_seq,report_json,provenance_json,observed_at,submitted_at,extensions_json) VALUES('slim_private',6503,JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE)),JSON_OBJECT(),NULL,'2026-10-15',JSON_OBJECT())`)
	d := &Database{db: db}
	for _, kind := range []string{"all", "firsthand", "coverage", "unknown"} {
		for _, sortBy := range []string{"signal", "recent", "recurrence"} {
			t.Run(kind+"/"+sortBy, func(t *testing.T) {
				f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "narrowslim", Kind: kind, Sort: sortBy, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				got, err := d.getEvidenceBrandPageNarrowSQL(context.Background(), f)
				if err != nil {
					t.Fatal(err)
				}
				want, err := d.getEvidenceBrandPageSQL(context.Background(), f)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("slim-rank public fields/counts/order differ from SQL oracle: %v\ngot: %+v\nwant: %+v", err, got, want)
				}
				if got.Overview.TotalReports != 14 || got.KindCounts.All != 1 || got.KindCounts.Firsthand != 1 || got.KindCounts.Coverage != 1 || got.KindCounts.Unknown != 0 {
					t.Fatalf("source dedup/case grouping/facets/overview changed: %+v", got)
				}
				if kind == "unknown" {
					if got.Total != 0 || len(got.Reports) != 0 {
						t.Fatal("source loser reappeared after kind filtering")
					}
					return
				}
				if got.Total != 1 || len(got.Reports) != 1 || got.Reports[0].Recurrence.CaseID == nil || *got.Reports[0].Recurrence.CaseID != "narrow_slim_family" {
					t.Fatal("case link on source import was lost during late detail join")
				}
				if kind == "coverage" {
					if got.Reports[0].Report.Seq != 6501 || got.Reports[0].Recurrence.DuplicateImportCount != 1 {
						t.Fatal("source winner selection/import count changed")
					}
					return
				}
				report := got.Reports[0]
				if report.Report.Seq != 6502 || report.Analysis[0].Title != "Title 6502" || report.Analysis[0].Summary != "Summary 6502" || report.Analysis[0].BrandDisplayName != "Slim Display" || report.Evidence.Excerpt == nil || *report.Evidence.Excerpt != "Observation 6502" {
					t.Fatal("rank core failed to recover selected public display/evidence fields")
				}
				if len(report.Recurrence.SupportingSources) != 7 {
					t.Fatalf("sample rank/first-eight privacy slice changed: %+v", report.Recurrence.SupportingSources)
				}
				for i, seq := range []int{6502, 6504, 6505, 6506, 6507, 6508, 6509} {
					if report.Recurrence.SupportingSources[i].Title != fmt.Sprint("Title ", seq) {
						t.Fatalf("sample rank order changed at %d: %+v", i, report.Recurrence.SupportingSources)
					}
				}
			})
		}
	}
}
