package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// This independent reference starts at the normalized-core contract. It proves
// MySQL window, PADSPACE, NULL and collation behavior without sharing the Go
// aggregation implementation or changing production/source-loader queries.
func TestEvidenceGroupingMySQLParityFixture(t *testing.T) {
	dsn := os.Getenv("GROUPING_TEST_DSN")
	if dsn == "" {
		t.Skip("set GROUPING_TEST_DSN for the dedicated local grouping_parity_evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "grouping_parity_evidence_test" {
		t.Fatal("fixture only permits dedicated local grouping_parity_evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// SET SESSION below and the inserts must share one fixture connection.
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE grouping_inputs (seq INT PRIMARY KEY,source_key TEXT COLLATE utf8mb4_bin,account_key VARCHAR(512) COLLATE utf8mb4_bin NULL,kind VARCHAR(16),version INT,excerpt_present BOOL,impact_present BOOL,media BOOL,restricted BOOL,relationship VARCHAR(16),severity DOUBLE,observed_at DATETIME NULL,published_at DATETIME NULL,ingested_at DATETIME NULL) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	defer db.Exec(`DROP TABLE grouping_inputs`)
	exec(`CREATE TABLE grouping_cases (seq INT,case_id VARCHAR(64),PRIMARY KEY (case_id,seq)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	defer db.Exec(`DROP TABLE grouping_cases`)
	inputs := []evidenceRecord{groupingRecord(1, "source:same", "coverage"), groupingRecord(2, "source:same ", "firsthand"), groupingRecord(3, "source:other", "coverage"), groupingRecord(4, "source:unknown", "firsthand"), groupingRecord(5, "source:singleton", "firsthand")}
	inputs[1].Version, inputs[1].Impact = 2, "impact"
	inputs[1].ObservedAt, inputs[1].PublishedAt, inputs[1].IngestedAt = groupingDate(2), groupingDate(10), *groupingDate(11)
	inputs[2].PublishedAt, inputs[2].Severity = groupingDate(3), 0.9
	inputs[3].Version = 0
	inputs[4].HasOriginalMedia, inputs[4].Excerpt, inputs[4].Relationship = true, "", "dependency"
	inputs[4].IngestedAt = *groupingDate(12)
	caseIDs := map[int]string{1: "ä", 2: "b", 3: "ä"}
	for seq := 10; seq <= 19; seq++ {
		r := groupingRecord(seq, fmt.Sprintf("source:samples:%d", seq), "firsthand")
		r.Excerpt, r.SourceAccountKey = "", "platform:alice"
		if seq%2 == 0 {
			r.SourceAccountKey += " "
		}
		r.PrivacyRestricted = seq >= 13
		inputs = append(inputs, r)
		caseIDs[seq] = "case_samples"
	}
	for seq := 20; seq <= 21; seq++ {
		r := groupingRecord(seq, fmt.Sprintf("source:private:%d", seq), "firsthand")
		r.PrivacyRestricted = true
		inputs = append(inputs, r)
		caseIDs[seq] = "case_private"
	}
	inputs = append(inputs, groupingRecord(22, "source:Path", "firsthand"), groupingRecord(23, "source:path", "firsthand"), groupingRecord(24, "source:pad-one", "firsthand"), groupingRecord(25, "source:pad-two", "firsthand"))
	caseIDs[24], caseIDs[25] = "case_pad", "case_pad "
	zero := time.Time{}
	for seq := 30; seq <= 36; seq++ {
		r := groupingRecord(seq, fmt.Sprintf("source:zero-or-null:%d", seq), "firsthand")
		r.IngestedAt = zero
		r.IngestedAtNull = seq != 30
		switch seq {
		case 31:
			r.PublishedAt = &zero
		case 32, 33:
			r.ObservedAt, r.PublishedAt = &zero, &zero
		}
		inputs = append(inputs, r)
	}
	caseIDs[33], caseIDs[34] = "case_known_zero", "case_known_zero"
	caseIDs[35], caseIDs[36] = "case_all_null", "case_all_null"
	var priorMode string
	if err := db.QueryRowContext(ctx, `SELECT @@SESSION.sql_mode`).Scan(&priorMode); err != nil {
		t.Fatal(err)
	}
	exec(`SET SESSION sql_mode=''`)
	for _, r := range inputs {
		var account any
		if r.SourceAccountKey != "" {
			account = r.SourceAccountKey
		}
		var ingested any
		if !r.IngestedAtNull {
			ingested = groupingSQLFixtureDate(&r.IngestedAt)
		}
		exec(`INSERT INTO grouping_inputs VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.Seq, r.SourceKey, account, r.Kind, r.Version, r.Excerpt != "", r.Impact != "", r.HasOriginalMedia, r.PrivacyRestricted, r.Relationship, r.Severity, groupingSQLFixtureDate(r.ObservedAt), groupingSQLFixtureDate(r.PublishedAt), ingested)
		if caseID, linked := caseIDs[r.Seq]; linked {
			exec(`INSERT INTO grouping_cases VALUES (?,?)`, r.Seq, caseID)
		}
	}
	exec(`SET SESSION sql_mode=?`, priorMode)
	refs := map[int]evidenceCaseReference{}
	caseRows, err := db.QueryContext(ctx, `SELECT seq,MIN(case_id) AS case_id FROM grouping_cases GROUP BY seq ORDER BY case_id`)
	if err != nil {
		t.Fatal(err)
	}
	for order := 0; caseRows.Next(); order++ {
		var seq int
		var id string
		if err := caseRows.Scan(&seq, &id); err != nil {
			t.Fatal(err)
		}
		refs[seq] = evidenceCaseReference{ID: id, Order: order}
	}
	if err := caseRows.Err(); err != nil {
		t.Fatal(err)
	}
	caseRows.Close()
	for _, kind := range []string{"all", "firsthand", "coverage", "unknown"} {
		for _, sortBy := range []string{"signal", "recent", "recurrence"} {
			for _, offset := range []int{0, 2, 100} {
				f := EvidenceFilter{Kind: kind, Sort: sortBy, Limit: 3, Offset: offset}
				if offset == 0 {
					// Include the ancient zero/NULL groups for every kind/sort,
					// while keeping a bounded middle page and empty-offset checks.
					f.Limit = 50
				}
				actual := groupEvidenceRecords(inputs, refs, f)
				predicate := "1=1"
				args := []any{}
				if kind != "all" {
					predicate = "report_kind=?"
					args = append(args, kind)
				}
				order := "evidence_strength DESC,entity_relevance DESC,supporting_report_count DESC,severity DESC,last_signal_at DESC,seq DESC"
				if sortBy == "recent" {
					order = "last_signal_at DESC,seq DESC"
				} else if sortBy == "recurrence" {
					order = "supporting_report_count DESC,evidence_strength DESC,last_signal_at DESC,seq DESC"
				}
				query := fmt.Sprintf(groupingSQLReference, predicate, order, "p."+strings.ReplaceAll(order, ",", ",p."))
				args = append(args, f.Limit, f.Offset)
				rows, err := db.QueryContext(ctx, query, args...)
				if err != nil {
					t.Fatal(err)
				}
				var expectedSeqs []int
				for rows.Next() {
					var all, first, coverage, unknown, total, high, medium, seq, imports, support, duplicates int
					var sourceKey, groupKey, rowKind string
					var caseID sql.NullString
					var accounts sql.NullInt64
					var observed, published sql.NullTime
					var samplesJSON []byte
					if err := rows.Scan(&all, &first, &coverage, &unknown, &total, &high, &medium, &seq, &sourceKey, &groupKey, &rowKind, &caseID, &imports, &support, &accounts, &duplicates, &observed, &published, &samplesJSON); err != nil {
						t.Fatal(err)
					}
					if actual.KindCounts.All != all || actual.KindCounts.Firsthand != first || actual.KindCounts.Coverage != coverage || actual.KindCounts.Unknown != unknown || actual.Total != total || actual.High != high || actual.Medium != medium {
						t.Fatalf("counts differ for%s/%s/offset%d", kind, sortBy, offset)
					}
					if seq == 0 {
						continue
					}
					index := len(expectedSeqs)
					expectedSeqs = append(expectedSeqs, seq)
					if index >= len(actual.Records) {
						t.Fatal("Go returned fewer page rows than SQL")
					}
					r := actual.Records[index]
					if r.Seq != seq || r.SourceKey != sourceKey || r.GroupKey != groupKey || r.Kind != rowKind || stringValue(r.CaseID) != caseID.String || r.ImportCount != imports || r.SupportingCount != support || r.FamilyDuplicates != duplicates || (r.SourceAccountCount != nil) != accounts.Valid || (accounts.Valid && int(accounts.Int64) != *r.SourceAccountCount) || !groupingNullableTimeEqual(r.LatestObservedAt, observed) || !groupingNullableTimeEqual(r.LatestPublishedAt, published) {
						t.Fatalf("record differs for%s/%s/offset%d/row%d", kind, sortBy, offset, index)
					}
					var sampleSeqs []int
					if err := json.Unmarshal(samplesJSON, &sampleSeqs); err != nil {
						t.Fatal(err)
					}
					gotSamples := make([]int, 0, len(actual.SupportingSamples[r.Seq]))
					for _, sample := range actual.SupportingSamples[r.Seq] {
						gotSamples = append(gotSamples, sample.Seq)
					}
					sort.Ints(sampleSeqs)
					sort.Ints(gotSamples)
					if !reflect.DeepEqual(sampleSeqs, gotSamples) {
						t.Fatalf("source sample eligibility differs for%s/%s/offset%d/row%d", kind, sortBy, offset, index)
					}
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				rows.Close()
				if len(expectedSeqs) != len(actual.Records) || actual.HasMore != (offset+len(actual.Records) < actual.Total) {
					t.Fatalf("page length/HasMore differs for%s/%s/offset%d", kind, sortBy, offset)
				}
			}
		}
	}
}

func groupingSQLFixtureDate(value *time.Time) any {
	if value == nil {
		return nil
	}
	if value.IsZero() {
		return "0000-00-00 00:00:00"
	}
	return *value
}

func groupingNullableTimeEqual(actual *time.Time, expected sql.NullTime) bool {
	return (actual == nil && !expected.Valid) || (actual != nil && expected.Valid && actual.Equal(expected.Time))
}

const groupingSQLReference = `WITH normalized AS (
 SELECT i.*,CASE WHEN version>0 AND kind IN ('firsthand','coverage') THEN kind ELSE 'unknown' END AS report_kind,
 CASE WHEN version>0 AND (excerpt_present OR media) AND NOT restricted THEN 2 ELSE 0 END+CASE WHEN version>0 AND impact_present AND NOT restricted THEN 1 ELSE 0 END AS evidence_strength,
 CASE WHEN version<1 THEN 0 WHEN relationship='affected' THEN 2 WHEN relationship='dependency' THEN 1 ELSE 0 END AS entity_relevance FROM grouping_inputs i
), representatives AS (
 SELECT n.*,ROW_NUMBER() OVER(PARTITION BY source_key ORDER BY version DESC,evidence_strength DESC,entity_relevance DESC,seq DESC) AS duplicate_rank,
 FIRST_VALUE(seq) OVER(PARTITION BY source_key ORDER BY version DESC,evidence_strength DESC,entity_relevance DESC,seq DESC) AS representative_seq,COUNT(*) OVER(PARTITION BY source_key) AS import_count FROM normalized n
), entries AS (SELECT * FROM representatives WHERE duplicate_rank=1), case_links AS (
 SELECT n.representative_seq AS seq,MIN(c.case_id) AS case_id FROM grouping_cases c JOIN representatives n ON n.seq=c.seq GROUP BY n.representative_seq
), tagged AS (
 SELECT e.*,c.case_id,CASE WHEN c.case_id IS NULL THEN source_key ELSE CONCAT('family:',c.case_id) END COLLATE utf8mb4_bin AS group_key FROM entries e LEFT JOIN case_links c ON c.seq=e.seq
), filtered AS (
 SELECT t.*,ROW_NUMBER() OVER(PARTITION BY group_key ORDER BY evidence_strength DESC,entity_relevance DESC,severity DESC,COALESCE(observed_at,published_at,ingested_at) DESC,seq DESC) AS family_rank,
 FIRST_VALUE(seq) OVER(PARTITION BY group_key ORDER BY evidence_strength DESC,entity_relevance DESC,severity DESC,COALESCE(observed_at,published_at,ingested_at) DESC,seq DESC) AS family_representative_seq FROM tagged t WHERE %s
), stats AS (
 SELECT family_representative_seq,COUNT(*) AS supporting_report_count,CASE WHEN COUNT(account_key)=COUNT(*) THEN COUNT(DISTINCT account_key) ELSE NULL END AS source_account_count,
 SUM(import_count)-COUNT(*) AS duplicates,MAX(COALESCE(observed_at,published_at,ingested_at)) AS last_signal_at,MAX(observed_at) AS latest_observed,MAX(published_at) AS latest_published FROM filtered GROUP BY family_representative_seq
), samples AS (
 SELECT family_representative_seq,JSON_ARRAYAGG(seq) AS sample_seqs FROM filtered WHERE family_rank<=8 AND NOT restricted GROUP BY family_representative_seq
), briefing AS (
 SELECT f.*,s.supporting_report_count,s.source_account_count,s.duplicates,s.last_signal_at,s.latest_observed,s.latest_published,COALESCE(a.sample_seqs,JSON_ARRAY()) AS sample_seqs FROM filtered f JOIN stats s ON s.family_representative_seq=f.family_representative_seq LEFT JOIN samples a ON a.family_representative_seq=f.family_representative_seq WHERE family_rank=1
), scope_counts AS (
 SELECT COUNT(DISTINCT group_key) AS all_count,COUNT(DISTINCT CASE WHEN report_kind='firsthand' THEN group_key END) AS first_count,COUNT(DISTINCT CASE WHEN report_kind='coverage' THEN group_key END) AS coverage_count,COUNT(DISTINCT CASE WHEN report_kind='unknown' THEN group_key END) AS unknown_count FROM tagged
), page_counts AS (SELECT COUNT(*) AS total_count,COALESCE(SUM(severity>=0.7),0) AS high_count,COALESCE(SUM(severity>=0.4 AND severity<0.7),0) AS medium_count FROM briefing), paged AS (SELECT * FROM briefing ORDER BY %s LIMIT ? OFFSET ?)
SELECT s.all_count,s.first_count,s.coverage_count,s.unknown_count,c.total_count,c.high_count,c.medium_count,COALESCE(p.seq,0),COALESCE(p.source_key,''),COALESCE(p.group_key,''),COALESCE(p.report_kind,''),p.case_id,COALESCE(p.import_count,0),COALESCE(p.supporting_report_count,0),p.source_account_count,COALESCE(p.duplicates,0),p.latest_observed,p.latest_published,COALESCE(p.sample_seqs,JSON_ARRAY()) FROM scope_counts s CROSS JOIN page_counts c LEFT JOIN paged p ON TRUE ORDER BY %s`
