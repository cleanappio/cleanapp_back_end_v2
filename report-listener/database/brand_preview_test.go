package database

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBrandPreviewUsesExactBoundedIndexedScopesAndPublicPolicy(t *testing.T) {
	f, err := NormalizeBrandPreviewFilter(EvidenceFilter{BrandName: "  OPENAI ", Limit: 999})
	if err != nil || f.Kind != "firsthand" || f.Language != "en" || f.Limit != 6 {
		t.Fatalf("wrong preview defaults: %+v %v", f, err)
	}
	query, args := brandPreviewCandidatesQuery(f, f.Limit)
	if len(args) != len(f.Brands)*3+1 || strings.Count(query, "ORDER BY ra.seq DESC LIMIT ?") != len(f.Brands) {
		t.Fatal("aliases are not independently bounded before merging")
	}
	for _, required := range []string{"FORCE INDEX (" + evidenceBrandCoreIndex + ")", "ra.brand_name=?", "ra.is_valid=TRUE", "ra.classification='digital'", "public_id REGEXP", publicReportCurrentVisibilitySQL, "ev.evidence_version>0 AND ev.report_kind=?"} {
		if !strings.Contains(query, required) {
			t.Fatalf("missing exact scope/visibility guard: %s", required)
		}
	}
	for _, forbidden := range []string{" LIKE ", "GROUP BY", "OFFSET", "r.image", "latitude", "longitude", "case_reports", "notification", "contact"} {
		if strings.Contains(query, forbidden) || strings.Contains(brandPreviewMetadataQuery, forbidden) {
			t.Fatalf("preview invoked unbounded or private projection: %s", forbidden)
		}
	}
	malicious, _ := NormalizeBrandPreviewFilter(EvidenceFilter{BrandName: "not-a-real-brand' OR 1=1 --", Limit: 1})
	query, args = brandPreviewCandidatesQuery(malicious, 1)
	if strings.Contains(query, malicious.BrandName) || args[0] != malicious.BrandName {
		t.Fatal("brand values must remain bound parameters")
	}
	for _, kind := range []string{"coverage", "unknown", "all"} {
		kindFilter := f
		kindFilter.Kind = kind
		q, _ := brandPreviewCandidatesQuery(kindFilter, 1)
		if kind == "unknown" && !strings.Contains(q, "NOT EXISTS (SELECT 1 FROM report_evidence") {
			t.Fatal("missing evidence must stay explicitly unknown")
		}
		if kind == "all" && strings.Contains(q, "FROM report_evidence ev") {
			t.Fatal("presence check must not depend on evidence classification")
		}
	}
}

func TestBrandPreviewProjectionIsBoundedRedactedAndDoesNotInventProvenance(t *testing.T) {
	date := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	r := evidenceRecord{PublicID: "rpt_aUl-fseGsxzbWPAXbgKxGw", Title: strings.Repeat("界", 300), Summary: "Customer could not finish checkout.",
		Kind: "firsthand", Version: 1, SourceURL: "https://github.com/example/product/issues/12?utm_source=test#fragment", PublishedAt: &date,
		Excerpt: "Checkout never finished.", IngestedAt: date, ActorKind: "machine", FetcherID: "private-fetcher", SourceAccountKey: "private-actor", SourceKey: "private-source"}
	preview := publicBrandPreviewReport(r)
	if len([]rune(preview.Title)) != 240 || preview.ReportKind != "firsthand" || preview.Excerpt == nil || *preview.Excerpt != r.Excerpt || preview.ObservedAt != nil {
		t.Fatalf("original/bounds/date facts changed: %+v", preview)
	}
	if preview.SourceURL == nil || *preview.SourceURL != "https://github.com/example/product/issues/12" || preview.IntakeMethod != "imported" || preview.IntakeChannel != nil {
		t.Fatalf("source provenance changed: %+v", preview)
	}
	encoded, _ := json.Marshal(preview)
	for _, forbidden := range []string{"private-fetcher", "private-actor", "private-source", "seq", "latitude", "longitude", "image", "discovery_token", "count", "actor", "fetcher", "severity", "contact"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private or unsupported preview field exposed: %s", forbidden)
		}
	}
	r.Title, r.Summary, r.Excerpt = "Email person@example.test", "password=veryprivatevalue", "token=veryprivatevalue"
	preview = publicBrandPreviewReport(r)
	if preview.Title != "Reported issue" || preview.Summary != "" || preview.Excerpt != nil {
		t.Fatal("sensitive text escaped preview redaction")
	}
	r.Title, r.Summary, r.Excerpt, r.PrivacyRestricted = "Original title", "Original summary", "Original excerpt", true
	preview = publicBrandPreviewReport(r)
	if preview.Title != "Reported issue" || preview.Summary != "" || preview.SourceURL != nil || preview.Excerpt != nil {
		t.Fatal("privacy-restricted source material exposed")
	}
	r.Version, r.Kind = 0, "firsthand"
	if publicBrandPreviewReport(r).ReportKind != "unknown" {
		t.Fatal("unenriched source was labeled firsthand")
	}
	for _, raw := range []string{"javascript:alert(1)", "http://127.0.0.1/private", "https://user:password@example.test/", "https://example.test/?token=private"} {
		r.SourceURL, r.PrivacyRestricted = raw, false
		if publicBrandPreviewReport(r).SourceURL != nil {
			t.Fatalf("unsafe source URL accepted: %s", raw)
		}
	}
}

func TestBrandPreviewPresenceIsIndependentOfFirsthandFilter(t *testing.T) {
	for _, found := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		f, _ := NormalizeBrandPreviewFilter(EvidenceFilter{BrandName: "rarebrand", Limit: 6})
		filtered, filteredArgs := brandPreviewCandidatesQuery(f, 6)
		presence := f
		presence.Kind = "all"
		anyKind, anyArgs := brandPreviewCandidatesQuery(presence, 1)
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(filtered)).WithArgs(filteredArgs[0], filteredArgs[1], filteredArgs[2], filteredArgs[3]).WillReturnRows(sqlmock.NewRows([]string{"seq"}))
		rows := sqlmock.NewRows([]string{"seq"})
		if found {
			rows.AddRow(1234567) // Not limited by a global recent/top-500 sample.
		}
		mock.ExpectQuery(regexp.QuoteMeta(anyKind)).WithArgs(anyArgs[0], anyArgs[1], anyArgs[2]).WillReturnRows(rows)
		mock.ExpectCommit()
		preview, err := (&Database{db: db}).GetPublicBrandPreview(context.Background(), f)
		if err != nil || preview.BrandFound != found || len(preview.Reports) != 0 || preview.EvidenceKind != "firsthand" {
			t.Fatalf("empty filter confused with absence: %+v %v", preview, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestBrandPreviewUnavailableCannotBecomeAnEmptySuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT DISTINCT preview_candidates.seq").WillReturnError(errors.New("query timeout"))
	mock.ExpectRollback()
	preview, err := (&Database{db: db}).GetPublicBrandPreview(context.Background(), EvidenceFilter{BrandName: "rarebrand", Limit: 6})
	if err == nil || preview.BrandFound || len(preview.Reports) != 0 {
		t.Fatal("query failure returned apparently valid empty discovery")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
