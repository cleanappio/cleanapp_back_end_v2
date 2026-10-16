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
