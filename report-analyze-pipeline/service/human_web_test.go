package service

import (
	"cleanapp-common/evidence"
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/services"
	"strings"
	"testing"
)

func TestWebPublicationRequiresValidFirsthandRetainedHumanOriginal(t *testing.T) {
	for _, tc := range []struct {
		name, kind, source, text string
		valid, hasImage, want    bool
	}{
		{"firsthand text", "firsthand", "human_submission", "The checkout button failed when I clicked it.", true, false, true},
		{"firsthand image", "firsthand", "human_submission", "", true, true, true},
		{"invalid or spam", "firsthand", "human_submission", "An observed problem", false, false, false},
		{"unknown", "unknown", "human_submission", "A generic request", true, false, false},
		{"coverage", "coverage", "human_submission", "Somebody said a thing", true, false, false},
		{"import claims firsthand", "firsthand", "twitter", "I cannot pay", true, false, false},
		{"missing original", "firsthand", "human_submission", "", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &database.ReportAnalysis{IsValid: tc.valid, Fields: evidence.Fields{ReportKind: tc.kind}}
			o := evidence.Original{SourceType: tc.source, Text: tc.text}
			if tc.hasImage {
				o.Image = []byte("retained original image")
			}
			if got := canPublishHumanWebAnalysis(a, o); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

type webAnalysisStore struct {
	*analysisStore
	intake     database.HumanWebIntake
	visibility string
	promotions int
	promoteErr error
}

func (d *webAnalysisStore) HumanWebIntake(context.Context, int) (database.HumanWebIntake, bool, error) {
	return d.intake, true, nil
}
func (d *webAnalysisStore) PublishAnalyzedHumanWebIntake(context.Context, int) error {
	if d.promoteErr != nil {
		return d.promoteErr
	}
	d.promotions++
	d.visibility = "public"
	return nil
}
func (d *webAnalysisStore) ReportVisibility(int) (string, error) { return d.visibility, nil }

type webAnalysisClient struct {
	*analysisClient
	valid       bool
	kind, input string
}

type retryWebPublisher struct {
	*recordingPublisher
	fail bool
}

func (p *retryWebPublisher) Publish(event any) error {
	if p.fail {
		return errors.New("broker unavailable")
	}
	return p.recordingPublisher.Publish(event)
}

func (c *webAnalysisClient) AnalyzeImage(_ []byte, input string) (string, error) {
	c.input = input
	b, _ := json.Marshal(map[string]any{"title": "A blocked doorway", "description": "The reporter observed a blocked door.", "classification": "physical", "is_valid": c.valid, "brand_name": ""})
	return string(b), nil
}
func (c *webAnalysisClient) AnalyzeEvidence(_, _ string) (evidence.Fields, error) {
	return evidence.Fields{ReportKind: c.kind, EvidenceExcerpt: "I hit an error on submit.", ClassificationReason: "The original describes a specific observed problem."}, nil
}

func TestWebTextTraversesExistingAnalyzerAndOnlyValidFirsthandPublishes(t *testing.T) {
	for _, tc := range []struct {
		name, kind                                        string
		valid, failPromotion, failPublication, wantPublic bool
	}{
		{"observed valid problem", "firsthand", true, false, false, true},
		{"invalid content", "firsthand", false, false, false, false},
		{"ambiguous content", "unknown", true, false, false, false},
		{"coverage content", "coverage", true, false, false, false},
		{"failed promotion retries", "firsthand", true, true, false, false},
		{"failed publication retries", "firsthand", true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			originalText := strings.Repeat("Retained original context. ", 20) + "I hit an error on submit."
			store := &webAnalysisStore{analysisStore: &analysisStore{sqlDB: db, report: &database.Report{Seq: 83, Description: "truncated legacy projection"}}, intake: database.HumanWebIntake{Text: originalText}, visibility: "shadow"}
			if tc.failPromotion {
				store.promoteErr = errors.New("database unavailable")
			}
			client := &webAnalysisClient{analysisClient: &analysisClient{}, valid: tc.valid, kind: tc.kind}
			publisher := &retryWebPublisher{recordingPublisher: &recordingPublisher{}, fail: tc.failPublication}
			svc := &Service{config: &config.Config{}, db: store, llmClient: client, brandService: services.NewBrandService(), publisher: publisher}
			expectOriginal := func() {
				mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(83).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("truncated legacy projection", nil))
				mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(83).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
				mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(83).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow("web_original", "text"))
				mock.ExpectQuery(`SELECT submission_id FROM wire_submissions_raw`).WithArgs(83).WillReturnRows(sqlmock.NewRows([]string{"submission_id"}).AddRow("web_submission"))
				reportJSON, _ := json.Marshal(map[string]string{"problem_type": "citizen_submission", "description": originalText})
				mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE submission_id = \? AND report_seq = \?`).WithArgs("web_submission", 83).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(reportJSON, "human", "web"))
			}
			expectOriginal()
			err = svc.AnalyzeReport(store.report)
			if (err != nil) != (tc.failPromotion || tc.failPublication) {
				t.Fatalf("unexpected analysis result: %v", err)
			}
			if len(store.analyses) != 1 || len(store.evidenceWrites) != 1 {
				t.Fatal("original not analyzed/persisted through existing pipeline")
			}
			if !strings.Contains(client.input, originalText) || !strings.Contains(client.input, `"location_supplied":false`) {
				t.Fatal("full original or unknown location lost")
			}
			if (len(publisher.events) > 0) != tc.wantPublic || (store.promotions > 0) != (tc.wantPublic || tc.failPublication) {
				t.Fatalf("promotion/publication mismatch: promoted=%d events=%d", store.promotions, len(publisher.events))
			}
			if tc.failPublication {
				if store.publishedMarkers != 0 {
					t.Fatal("failed publish marked complete")
				}
				publisher.fail = false
				expectOriginal()
				if err := svc.AnalyzeReport(store.report); err != nil {
					t.Fatal(err)
				}
				if len(publisher.events) != 1 || store.publishedMarkers != 1 {
					t.Fatal("retained queue retry did not publish successfully")
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
