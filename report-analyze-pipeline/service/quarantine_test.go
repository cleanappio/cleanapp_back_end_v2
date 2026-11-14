package service

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/rabbitmq"
	"report-analyze-pipeline/services"
)

type quarantineAnalysisStore struct {
	*analysisStore
	visibility    string
	visibilityErr error
	lookups       int
}

func (d *quarantineAnalysisStore) ReportVisibility(int) (string, error) {
	d.lookups++
	return d.visibility, d.visibilityErr
}

func expectMachineOriginal(mock sqlmock.Sqlmock, seq int) {
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(seq).
		WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("I hit an error on submit.", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(seq).
		WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(seq).
		WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow("openai_machine_fixture", "assistant"))
	mock.ExpectQuery(`SELECT submission_id FROM wire_submissions_raw`).WithArgs(seq).
		WillReturnRows(sqlmock.NewRows([]string{"submission_id"}).AddRow("machine_submission"))
	mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE submission_id = \? AND report_seq = \?`).
		WithArgs("machine_submission", seq).
		WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(
			`{"domain":"digital","description":"I hit an error on submit.","digital_context":{"metadata":{"original_text":"I hit an error on submit."}}}`,
			"machine", "wire"))
}

func TestMachineAnalysisPersistsBeforeFailClosedVisibilityCheck(t *testing.T) {
	lookupErr := errors.New("visibility lookup unavailable")
	for _, tc := range []struct {
		name          string
		visibility    string
		visibilityErr error
		wantEvents    int
	}{
		{name: "quarantine remains private after analysis", visibility: "shadow"},
		{name: "lookup failure retries without publication", visibility: "shadow", visibilityErr: lookupErr},
		{name: "public machine report still publishes", visibility: "public", wantEvents: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			const seq = 84
			store := &quarantineAnalysisStore{
				analysisStore: &analysisStore{sqlDB: db, report: &database.Report{Seq: seq, Description: "I hit an error on submit."}},
				visibility:    tc.visibility, visibilityErr: tc.visibilityErr,
			}
			client := &analysisClient{}
			publisher := &recordingPublisher{}
			// Both global background jobs are disabled. A machine report must
			// still traverse normal per-report analysis without human-web promotion.
			svc := &Service{config: &config.Config{EvidenceWorkerEnabled: false, EnrichExternalDigitalEnabled: false}, db: store,
				llmClient: client, brandService: services.NewBrandService(), publisher: publisher}
			expectMachineOriginal(mock, seq)
			err = svc.AnalyzeReport(store.report)
			if !errors.Is(err, tc.visibilityErr) {
				t.Fatalf("analysis error = %v, want %v", err, tc.visibilityErr)
			}
			var permanent *rabbitmq.PermanentError
			if errors.As(err, &permanent) {
				t.Fatal("visibility failure became permanent instead of retryable")
			}
			if len(store.analyses) != 1 || len(store.evidenceWrites) != 1 || client.calls != 1 || store.lookups != 1 {
				t.Fatalf("analysis was not retained before visibility check: analyses=%d evidence=%d classifications=%d lookups=%d",
					len(store.analyses), len(store.evidenceWrites), client.calls, store.lookups)
			}
			if store.analyses[0].Seq != seq || store.analyses[0].Language != "en" {
				t.Fatal("stored analysis lost its report identity or language")
			}
			if len(publisher.events) != tc.wantEvents || store.publishedMarkers != tc.wantEvents {
				t.Fatalf("publication mismatch: events=%d markers=%d want=%d", len(publisher.events), store.publishedMarkers, tc.wantEvents)
			}
			if tc.visibilityErr != nil {
				// A transient DB error must not silently consume the queued report.
				// Recovery rechecks its private visibility and still emits no event.
				store.visibilityErr = nil
				expectMachineOriginal(mock, seq)
				if err := svc.AnalyzeReport(store.report); err != nil {
					t.Fatalf("private report did not recover on retry: %v", err)
				}
				if len(store.analyses) != 2 || store.analyses[1].Seq != seq || store.lookups != 2 {
					t.Fatal("retry did not retain analysis for the same report and recheck visibility")
				}
				if len(publisher.events) != 0 || store.publishedMarkers != 0 || store.visibility != "shadow" {
					t.Fatal("recovered quarantine report was published or promoted")
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
