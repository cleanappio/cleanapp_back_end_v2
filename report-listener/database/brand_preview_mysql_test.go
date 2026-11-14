package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"report-listener/models"
)

// Runs inside the guarded local MySQL fixture already required by evidence CI.
// This exercises the actual indexed candidate SQL and canonical projections.
func testPublicBrandPreviewMySQL(t *testing.T, db *sql.DB) {
	t.Helper()
	d := &Database{db: db}
	type previewResult struct {
		found   bool
		ids     []string
		preview models.PublicBrandPreview
	}
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(seq int, brand, kind string) {
		t.Helper()
		exec(`INSERT INTO reports(seq,public_id,ts,latitude,longitude,image) VALUES(?,?,'2026-10-16',0,0,'')`, seq, fmt.Sprintf("rpt_%022d", seq))
		exec(`INSERT INTO report_analysis VALUES(?,'en',?, 'Preview Fixture',TRUE,'digital','2026-10-16','English issue','AI summary',0.5)`, seq, brand)
		if kind != "unknown" {
			exec(`INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,evidence_version) VALUES(?,?,'Original observation',1)`, seq, kind)
		}
	}
	read := func(brand, kind, lang string, n int) previewResult {
		t.Helper()
		preview, err := d.GetPublicBrandPreview(context.Background(), EvidenceFilter{BrandName: brand, Kind: kind, Language: lang, Limit: n})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(preview.Reports))
		for i, report := range preview.Reports {
			ids[i] = report.PublicID
			if report.CanonicalPath != "/digital/report/"+report.PublicID || report.ReportKind != preview.EvidenceKind && preview.EvidenceKind != "all" {
				t.Fatalf("incorrect public report identity/kind: %+v", report)
			}
		}
		return previewResult{preview.BrandFound, ids, preview}
	}
	for seq := 4001; seq <= 4008; seq++ {
		seed(seq, "preview-fixture", "firsthand")
	}
	exec(`INSERT INTO report_analysis VALUES(4008,'fr','preview-fixture','Apercu',TRUE,'digital','2026-10-16','French issue','Resume',0.5)`)
	for _, row := range []struct {
		seq   int
		guard string
	}{
		{4010, `INSERT INTO reports_owners VALUES(4010,'private-owner',FALSE)`},
		{4011, `INSERT INTO report_status VALUES(4011,'resolved')`},
		{4012, `INSERT INTO report_raw VALUES(4012,'sensor','id','fetcher','quarantine')`},
		{4013, `UPDATE report_analysis SET is_valid=FALSE WHERE seq=4013`},
		{4014, `UPDATE report_analysis SET classification='physical' WHERE seq=4014`},
		{4015, `UPDATE reports SET public_id='legacy_sequence' WHERE seq=4015`},
	} {
		seed(row.seq, "preview-fixture", "firsthand")
		exec(row.guard)
	}
	got := read("preview-fixture", "firsthand", "fr", 999)
	if !got.found || len(got.ids) != 6 || got.ids[0] != fmt.Sprintf("rpt_%022d", 4008) || got.ids[5] != fmt.Sprintf("rpt_%022d", 4003) || got.preview.Reports[0].Title != "French issue" {
		t.Fatalf("bounded/latest/visibility/language query failed: %+v", got)
	}
	if one := read("preview-fixture", "firsthand", "en", 1); len(one.ids) != 1 || one.preview.Reports[0].Title != "English issue" {
		t.Fatal("requested limit or language was ignored")
	}
	seed(4101, "preview-private", "firsthand")
	exec(`INSERT INTO reports_owners VALUES(4101,'private-owner',FALSE)`)
	if read("preview-private", "all", "en", 6).found || read("preview-absent", "firsthand", "en", 6).found {
		t.Fatal("private-only or absent brand became public")
	}
	seed(4201, "preview-coverage", "coverage")
	if coverage := read("preview-coverage", "firsthand", "en", 6); !coverage.found || len(coverage.ids) != 0 {
		t.Fatal("coverage-only presence was confused with absence")
	}
	if coverage := read("preview-coverage", "coverage", "en", 6); !coverage.found || len(coverage.ids) != 1 {
		t.Fatal("coverage evidence filter failed")
	}
	seed(4301, "preview-unknown", "unknown")
	if unknown := read("preview-unknown", "unknown", "en", 6); !unknown.found || len(unknown.ids) != 1 || unknown.preview.Reports[0].SourcePublishedAt != nil {
		t.Fatal("missing enrichment or provenance was fabricated")
	}
	seed(4401, "preview-restricted", "firsthand")
	exec(`INSERT INTO external_ingest_index(seq,source,external_id,source_url) VALUES(4401,'github_issue','preview-private','https://github.com/org/repo/issues/1')`)
	exec(`INSERT INTO wire_submissions_raw(report_seq,report_json,provenance_json,submitted_at,actor_kind,channel) VALUES(4401,'{"privacy":{"requires_redaction":true}}','{}','2026-10-16','human','web')`)
	private := read("preview-restricted", "firsthand", "en", 6).preview.Reports[0]
	if private.Title != "Reported issue" || private.Summary != "" || private.Excerpt != nil || private.SourceURL != nil {
		t.Fatalf("actual source privacy flag escaped redaction: %+v", private)
	}
	description := "private-reader-description"
	detail := &models.ReportWithAnalysis{Report: models.Report{Seq: 4401, Image: []byte("private-reader-image"), Description: &description}, Analysis: []models.ReportAnalysis{{Title: "private-reader-title", Summary: "private-reader-summary", Description: "private-reader-analysis-description"}}}
	if err := d.AttachPublicEvidence(context.Background(), []*models.ReportWithAnalysis{detail}); err != nil {
		t.Fatal(err)
	}
	if detail.Analysis[0].Title != "Reported issue" || detail.Analysis[0].Summary != "" || detail.Analysis[0].Description != "" || detail.Report.Image != nil || detail.Report.Description != nil || detail.Report.SourceURL != nil {
		t.Fatal("public detail path did not apply actual Wire privacy flag")
	}
	// Public canonical pages may remain available, but neither public image
	// reader may bypass a retained Wire privacy flag (including an older row).
	exec(`UPDATE reports SET image='restricted-original-image' WHERE seq=4401`)
	for _, privacy := range []string{`{"privacy":{"requires_redaction":true}}`, `{"privacy":{"contains_pii":true}}`} {
		exec(`UPDATE wire_submissions_raw SET report_json=? WHERE report_seq=4401`, privacy)
		exec(`INSERT INTO wire_submissions_raw(report_seq,report_json,provenance_json,submitted_at,actor_kind,channel) VALUES(4401,'{"privacy":{"contains_pii":false,"requires_redaction":false}}','{}','2026-10-17','human','web')`)
		publicID := fmt.Sprintf("rpt_%022d", 4401)
		if image, err := d.GetImageByPublicID(context.Background(), publicID); err == nil || len(image) != 0 {
			t.Fatal("direct image reader bypassed Wire privacy")
		}
		if image, err := d.GetPublicReportMediaByPublicID(context.Background(), publicID, 1024); err == nil || len(image) != 0 {
			t.Fatal("bounded media reader bypassed Wire privacy")
		}
		if seq, err := d.PublicReportSequence(context.Background(), publicID); err != nil || seq != 4401 {
			t.Fatal("media-only privacy restriction incorrectly hid the public report")
		}
	}
	exec(`UPDATE wire_submissions_raw SET report_json='{"privacy":{"contains_pii":false,"requires_redaction":false}}' WHERE report_seq=4401`)
	if image, err := d.GetImageByPublicID(context.Background(), fmt.Sprintf("rpt_%022d", 4401)); err != nil || string(image) != "restricted-original-image" {
		t.Fatal("explicitly unrestricted public Wire image was hidden")
	}
	if image, err := d.GetPublicReportMediaByPublicID(context.Background(), fmt.Sprintf("rpt_%022d", 4401), 1024); err != nil || string(image) != "restricted-original-image" {
		t.Fatal("explicitly unrestricted public Wire preview was hidden")
	}
	seed(4402, "preview-boundary", "firsthand")
	boundary := func(limit int) string { return strings.Repeat("x", limit-14) + " user@example.test" }
	exec(`UPDATE report_analysis SET title=?,summary=?,brand_display_name=? WHERE seq=4402`, boundary(240), boundary(700), boundary(120))
	exec(`UPDATE report_evidence SET evidence_excerpt=? WHERE seq=4402`, boundary(500))
	redacted := read("preview-boundary", "firsthand", "en", 6).preview
	if redacted.BrandDisplay != "preview-boundary" || redacted.Reports[0].Title != "Reported issue" || redacted.Reports[0].Summary != "" || redacted.Reports[0].Excerpt != nil {
		t.Fatal("SQL truncation concealed boundary-crossing sensitive text")
	}
	seed(4501, "claude", "firsthand")
	exec(`INSERT INTO report_analysis VALUES(4501,'en','claudecode','Claude Code',TRUE,'digital','2026-10-16','Alias issue','AI summary',0.5)`)
	if aliases := read("anthropic", "firsthand", "en", 6); !aliases.found || len(aliases.ids) != 1 {
		t.Fatal("company alias union duplicated or omitted a public report")
	}
	// Exercise the actual preview pipeline, including one batched canonical
	// provenance read and a privacy flag discovered after display hydration.
	for _, seq := range []int{4451, 4452} {
		seed(seq, "preview-provenance", "firsthand")
		exec(`INSERT INTO report_raw VALUES(?,'web',?,'preview-canonical','public')`, seq, fmt.Sprint("preview-source-", seq))
	}
	const privateSentinel = "PRIVATE_PREVIEW_DERIVATIVE_SENTINEL"
	exec(`UPDATE report_analysis SET title=?,summary=?,brand_display_name=? WHERE seq=4452`, privateSentinel, privateSentinel, privateSentinel)
	publicBody := `{"description":"PUBLIC_ORIGINAL_NOT_SERIALIZED https://x.com/account/access https://x.com/fixture/status/1874425336427446275","digital_context":{"platform":"twitter","product_or_site":"x.com / Cloudflare","source_url":"https://x.com/fixture/status/1874425336427446275","metadata":{"reporter_context":"relayed","source_published_at":"2025-01-01T14:00:00+02:00"}}}`
	client := `{"cleanapp_client":{"transport":"mcp","name":"grok","name_source":"caller_provided"}}`
	for _, row := range []struct {
		seq               int
		fetcher, body, at string
	}{
		{4451, "preview-canonical", publicBody, "2026-11-14 12:00:00"},
		{4452, "preview-canonical", `{"privacy":{"requires_redaction":true},"description":"` + privateSentinel + `","digital_context":{"product_or_site":"` + privateSentinel + `"}}`, "2026-11-14 12:00:00"},
		{4452, "preview-other-fetcher", `{"privacy":{"requires_redaction":false}}`, "2026-11-14 13:00:00"},
	} {
		exec(`INSERT INTO wire_submissions_raw(fetcher_id,report_seq,report_json,provenance_json,extensions_json,observed_at,submitted_at,actor_kind,channel) VALUES(?,?,?,'{"generation_method":"human_assisted"}',?,'2026-11-13',?,'machine','wire')`, row.fetcher, row.seq, row.body, client, row.at)
	}
	actual := read("preview-provenance", "firsthand", "en", 6).preview
	if len(actual.Reports) != 2 || actual.BrandDisplay != "preview-provenance" {
		t.Fatalf("late private display escaped the bounded preview: %+v", actual)
	}
	public, privateContext := actual.Reports[1], actual.Reports[0]
	if stringValue(public.SubmissionTransport) != "mcp" || stringValue(public.SubmissionClient) != "grok" || !public.SubmissionClientClaimed || stringValue(public.ReporterContext) != "relayed" || stringValue(public.ReportedProductOrSite) != "x.com / Cloudflare" || stringValue(public.SourcePlatform) != "twitter" || len(public.ReferencedURLs) != 2 || len(public.SourceReferences) != 1 || public.SourceReferenceCount == nil || *public.SourceReferenceCount != 1 || public.ObservedAtBasis != "provided" || public.SourcePublishedAtBasis != "provided" {
		t.Fatalf("actual preview dropped retained context: %+v", public)
	}
	if privateContext.Title != "Reported issue" || privateContext.Summary != "" || privateContext.SubmissionClient != nil || privateContext.ReporterContext != nil || privateContext.ReportedProductOrSite != nil || privateContext.SourcePlatform != nil || len(privateContext.ReferencedURLs) != 0 || len(privateContext.SourceReferences) != 0 || privateContext.SourceReferenceCount != nil {
		t.Fatalf("late canonical restriction leaked preview context: %+v", privateContext)
	}
	encoded, err := json.Marshal(actual)
	if err != nil || strings.Contains(string(encoded), privateSentinel) || strings.Contains(string(encoded), "PUBLIC_ORIGINAL_NOT_SERIALIZED") {
		t.Fatalf("actual preview leaked original or derivative text: %v %s", err, encoded)
	}
	var publicJSON map[string]json.RawMessage
	data, _ := json.Marshal(public)
	if err := json.Unmarshal(data, &publicJSON); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"submission_transport", "submission_client", "submission_client_claimed", "reporter_context", "reported_product_or_site", "referenced_urls", "source_references", "source_reference_count", "observed_at_basis", "source_published_at_basis", "source_platform"} {
		if _, exists := publicJSON[field]; !exists {
			t.Fatalf("preview JSON omitted %s", field)
		}
	}
	// EXPLAIN is executed on real SQL too; require the installed brand access
	// index to remain in the plan without locking optimizer-specific row counts.
	f, _ := NormalizeBrandPreviewFilter(EvidenceFilter{BrandName: "preview-coverage", Limit: 6})
	query, args := brandPreviewCandidatesQuery(f, 6)
	var plan string
	if err := db.QueryRow("EXPLAIN FORMAT=JSON "+query, args...).Scan(&plan); err != nil || !strings.Contains(plan, evidenceBrandCoreIndex) {
		t.Fatalf("candidate query lost its bounded brand index: %v", err)
	}
}
