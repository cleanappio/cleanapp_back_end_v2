package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	"cleanapp-common/events"
	"cleanapp-common/evidence"
	"github.com/DATA-DOG/go-sqlmock"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/services"
)

type evidenceWrite struct {
	seq, version, normalCount int
	fields                    evidence.Fields
}

type analysisStore struct {
	*database.Database
	sqlDB            *sql.DB
	report           *database.Report
	mu               sync.Mutex
	analyses         []*database.ReportAnalysis
	evidenceWrites   []evidenceWrite
	evidenceVersion  int
	evidenceSaveErr  error
	publishedMarkers int
}

func (d *analysisStore) GetDB() *sql.DB { return d.sqlDB }
func (d *analysisStore) LockReportAnalysis(context.Context, int) (*database.ReportAnalysisLock, bool, error) {
	return &database.ReportAnalysisLock{}, false, nil
}
func (d *analysisStore) GetReportBySeq(int) (*database.Report, error) { return d.report, nil }
func (d *analysisStore) GetReportImage(int) ([]byte, error)           { return nil, nil }
func (d *analysisStore) SaveAnalysis(a *database.ReportAnalysis) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.analyses = append(d.analyses, a)
	return nil
}
func (d *analysisStore) SaveEvidence(_ context.Context, seq int, fields evidence.Fields, version int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.evidenceWrites = append(d.evidenceWrites, evidenceWrite{seq: seq, version: version, fields: fields, normalCount: len(d.analyses)})
	if d.evidenceSaveErr != nil {
		return d.evidenceSaveErr
	}
	d.evidenceVersion = version
	return nil
}
func (d *analysisStore) GetPendingEvidenceReports(context.Context, int) ([]database.PendingEvidence, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.evidenceVersion >= evidence.Version {
		return nil, nil
	}
	return []database.PendingEvidence{{Seq: d.report.Seq}}, nil
}
func (d *analysisStore) ReportVisibility(int) (string, error) { return "public", nil }
func (d *analysisStore) MarkAnalysedPublished(int)            { d.publishedMarkers++ }

type recordingPublisher struct{ events []events.ReportAnalysed }

func (p *recordingPublisher) Publish(v any) error {
	p.events = append(p.events, v.(events.ReportAnalysed))
	return nil
}
func (*recordingPublisher) Close() error { return nil }

type analysisClient struct {
	evidenceErr error
	calls       int
}

func (*analysisClient) SourceName() string { return "Test" }
func (*analysisClient) AnalyzeImage([]byte, string) (string, error) {
	return `{"title":"Form submit failed","description":"The form shows an error on submit.","classification":"digital","is_valid":true,"brand_name":""}`, nil
}
func (c *analysisClient) TranslateAnalysis(s, _ string) (string, error) { return s, nil }
func (c *analysisClient) AnalyzeEvidence(_, _ string) (evidence.Fields, error) {
	c.calls++
	if c.evidenceErr != nil {
		return evidence.Fields{}, c.evidenceErr
	}
	return evidence.Fields{ReportKind: "firsthand", EvidenceExcerpt: "I hit an error on submit.", EntityRelationship: "affected", ClassificationReason: "Original author describes an encountered form failure."}, nil
}

func expectNativeOriginal(mock sqlmock.Sqlmock, seq int) {
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("I hit an error on submit.", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow("human_mobile_test", "sensor"))
	mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(`{"problem_type":"citizen_submission","description":"I hit an error on submit."}`, "human", "mobile"))
}

func TestMainAnalysisPersistsAndRoutesDespiteEvidenceSaveFailure(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := &analysisStore{sqlDB: sqlDB, report: &database.Report{Seq: 71, Description: "I hit an error on submit."}, evidenceSaveErr: errors.New("PRIVATE_EVIDENCE_VALUE should never be logged")}
	client := &analysisClient{}
	publisher := &recordingPublisher{}
	svc := &Service{config: &config.Config{TranslationLanguages: map[string]string{"de": "German", "fr": "French"}}, db: store, llmClient: client, brandService: services.NewBrandService(), publisher: publisher}
	var logs bytes.Buffer
	prior := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prior) })
	expectNativeOriginal(mock, 71)
	if err := svc.AnalyzeReport(store.report); err != nil {
		t.Fatalf("evidence failure requeued normal analysis: %v", err)
	}
	if len(store.analyses) != 3 || len(store.evidenceWrites) != 1 || client.calls != 1 {
		t.Fatalf("normal/evidence work duplicated: normal=%d evidence=%d calls=%d", len(store.analyses), len(store.evidenceWrites), client.calls)
	}
	if write := store.evidenceWrites[0]; write.normalCount != 1 || write.seq != 71 || write.version != evidence.Version {
		t.Fatalf("evidence not saved once after English commit: %#v", write)
	}
	if len(publisher.events) != 1 || len(publisher.events[0].Analysis) != 3 || store.publishedMarkers != 1 {
		t.Fatal("evidence failure prevented normal routing")
	}
	for _, a := range store.analyses {
		if a.ReportKind != "firsthand" || a.EvidenceExcerpt != "I hit an error on submit." {
			t.Fatalf("translation altered original evidence: %#v", a)
		}
	}
	if strings.Contains(logs.String(), "PRIVATE_EVIDENCE_VALUE") {
		t.Fatal("evidence storage error leaked extracted data")
	}
	if pending, err := store.GetPendingEvidenceReports(context.Background(), 10); err != nil || len(pending) != 1 {
		t.Fatal("failed evidence storage no longer retryable")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMainTransientEvidenceFailureRetriesWithoutReanalysisOrNewEvent(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := &analysisStore{sqlDB: sqlDB, report: &database.Report{Seq: 72, Description: "I hit an error on submit."}}
	client := &analysisClient{evidenceErr: errors.New("temporary model outage")}
	publisher := &recordingPublisher{}
	svc := &Service{config: &config.Config{TranslationLanguages: map[string]string{"de": "German"}, EvidenceWorkerBatchSize: 10}, db: store, llmClient: client, brandService: services.NewBrandService(), publisher: publisher}
	expectNativeOriginal(mock, 72)
	if err := svc.AnalyzeReport(store.report); err != nil {
		t.Fatal(err)
	}
	if len(store.evidenceWrites) != 1 || store.evidenceWrites[0].version != 0 || store.evidenceVersion != 0 {
		t.Fatal("transient classification was marked complete")
	}
	client.evidenceErr = nil
	expectNativeOriginal(mock, 72)
	svc.processEvidenceBatch()
	svc.processEvidenceBatch() // Completed evidence no longer requires a model call.
	if len(store.analyses) != 2 || len(publisher.events) != 1 || store.publishedMarkers != 1 {
		t.Fatal("evidence retry reran normal analysis or routing")
	}
	if len(store.evidenceWrites) != 2 || store.evidenceVersion != evidence.Version || client.calls != 2 {
		t.Fatalf("failed evidence did not retry once: writes=%d version=%d calls=%d", len(store.evidenceWrites), store.evidenceVersion, client.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceWorkerRetriesResolverAndModelFailuresWithoutEvents(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := &analysisStore{sqlDB: sqlDB, report: &database.Report{Seq: 73}}
	client := &analysisClient{evidenceErr: errors.New("temporary model outage")}
	publisher := &recordingPublisher{}
	svc := &Service{config: &config.Config{EvidenceWorkerBatchSize: 10}, db: store, llmClient: client, publisher: publisher}
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(73).WillReturnError(errors.New("temporary original lookup outage"))
	svc.processEvidenceBatch()
	expectNativeOriginal(mock, 73)
	svc.processEvidenceBatch()
	if len(store.evidenceWrites) != 0 || len(store.analyses) != 0 || len(publisher.events) != 0 {
		t.Fatal("failed worker enrichment changed stored analysis or routing")
	}
	client.evidenceErr = nil
	expectNativeOriginal(mock, 73)
	svc.processEvidenceBatch()
	if len(store.evidenceWrites) != 1 || store.evidenceVersion != evidence.Version || len(store.analyses) != 0 || len(publisher.events) != 0 {
		t.Fatal("worker retry did not remain evidence-only")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
