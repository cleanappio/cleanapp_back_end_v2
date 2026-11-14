package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// Invoked only by the dedicated, loopback-only MySQL fixture. This checks real
// JSON/CTE semantics, canonical-fetcher identity and an immutable retained body.
func testPublicSubmissionProvenanceMySQL(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO reports(seq,public_id,ts,id,latitude,longitude,image) VALUES(4701,'rpt_0000000000000000004701','2026-11-14 20:06:21','provenance-fixture-1',0,0,''),(4702,'rpt_0000000000000000004702','2026-11-14 20:06:21','provenance-fixture-2',0,0,''),(4703,'rpt_0000000000000000004703','2026-11-14 20:06:21','provenance-fixture-3',0,0,'')`,
		`INSERT INTO report_raw VALUES(4701,'web','provenance-fixture-1','provenance-canonical','public'),(4702,'web','provenance-fixture-2','provenance-canonical','quarantine'),(4703,'web','provenance-fixture-3','provenance-canonical','public')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	retained := `{"description":"The affected page is https://x.com/account/access.","digital_context":{"product_or_site":"x.com / Cloudflare","metadata":{"reporter_context":"relayed","source_published_at":"2026-11-12T10:00:00+02:00"}}}`
	provenance := `{"generation_method":"human_assisted","upstream_sources":[{"kind":"url","value":"https://example.com/original"}]}`
	client := `{"cleanapp_client":{"transport":"mcp","name":"grok","name_source":"caller_provided"}}`
	insert := `INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id,report_json,provenance_json,extensions_json,submitted_at) VALUES(?,?,?,?,?,?,?)`
	for _, row := range []struct {
		id, fetcher, body, at string
		seq                   int
	}{
		{"provenance-a", "provenance-canonical", `{"description":"Older retained revision"}`, "2026-11-14 20:06:11", 4701},
		{"provenance-z", "provenance-canonical", retained, "2026-11-14 20:06:11", 4701},
		{"provenance-wrong", "other-fetcher", `{"digital_context":{"product_or_site":"WRONG CANONICAL SOURCE"}}`, "2026-11-14 21:00:00", 4701},
		{"provenance-private", "provenance-canonical", retained, "2026-11-14 20:06:11", 4702},
		{"provenance-flagged", "provenance-canonical", `{"privacy":{"requires_redaction":true},"digital_context":{"product_or_site":"WITHHELD"}}`, "2026-11-14 20:06:11", 4703},
	} {
		if _, err := db.Exec(insert, row.id, row.seq, row.fetcher, row.body, provenance, client, row.at); err != nil {
			t.Fatal(err)
		}
	}
	var before string
	if err := db.QueryRow(`SELECT report_json FROM wire_submissions_raw WHERE submission_id='provenance-z'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	records := []evidenceRecord{}
	for _, seq := range []int{4701, 4702, 4703} {
		records = append(records, evidenceRecord{Seq: seq, ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted"})
	}
	if err := hydratePublicSubmissionProvenance(context.Background(), db, records); err != nil {
		t.Fatal(err)
	}
	ev := records[0].publicEvidence()
	if stringValue(ev.ReportedProductOrSite) != "x.com / Cloudflare" || stringValue(ev.SubmissionTransport) != "mcp" || stringValue(ev.SubmissionClient) != "grok" || !ev.SubmissionClientClaimed || len(ev.ReferencedURLs) != 1 || ev.SourceReferenceCount == nil || *ev.SourceReferenceCount != 1 || ev.SourcePublishedAt == nil || ev.SourcePublishedAt.Format("2006-01-02T15:04:05Z07:00") != "2026-11-12T08:00:00Z" {
		t.Fatalf("canonical retained context was not projected: %+v", ev)
	}
	for _, r := range records[1:] {
		if r.SubmissionProvenance != nil {
			t.Fatalf("nonpublic or explicitly restricted context was projected: seq %d", r.Seq)
		}
	}
	var after string
	if err := db.QueryRow(`SELECT report_json FROM wire_submissions_raw WHERE submission_id='provenance-z'`).Scan(&after); err != nil || before != after || !json.Valid([]byte(after)) {
		t.Fatalf("retained original changed: %v", err)
	}
}
