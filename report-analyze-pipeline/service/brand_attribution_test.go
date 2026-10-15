package service

import (
	"encoding/json"
	"strings"
	"testing"

	"cleanapp-common/evidence"
	"github.com/DATA-DOG/go-sqlmock"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/contacts"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/services"
)

type subjectAnalysisClient struct {
	*analysisClient
	brand, original, evidenceTarget, input string
}

func (c *subjectAnalysisClient) AnalyzeImage(_ []byte, input string) (string, error) {
	c.input = input
	b, err := json.Marshal(map[string]any{
		"title": "CleanApp brand clustering problem", "description": c.original,
		"classification": "digital", "is_valid": true, "brand_name": c.brand,
	})
	return string(b), err
}

func (c *subjectAnalysisClient) AnalyzeEvidence(_ string, target string) (evidence.Fields, error) {
	c.evidenceTarget = target
	return evidence.Fields{ReportKind: "firsthand", EvidenceExcerpt: c.original, EntityRelationship: "affected", ClassificationReason: "The author describes their own difficulty clustering properties."}, nil
}

func TestComplaintSubjectSurvivesAnalysisPersistenceTranslationAndRouting(t *testing.T) {
	for _, tc := range []struct {
		name, modelBrand, original, wantBrand, wantDisplay string
	}{
		{"explicit subject with examples", "CleanApp", "On CleanApp itself, I can't easily cluster multiple properties or sites into one brand cluster, eg clustering Grok and Grokipedia and XAI and X and SpaceX", "cleanapp", "CleanApp"},
		{"unknown subject with examples", "", "Can't easily cluster multiple properties or sites into one brand cluster, eg clustering Grok and Grokipedia and XAI and X and SpaceX", "", ""},
		{"screenshot reports an incorrect brand label", "CleanApp", "problem with CleanApp brand attribution logic. It's not ascribing to CleanApp itself. Screenshot shows the incorrect Grok and xAI attribution.", "cleanapp", "CleanApp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			const seq = 1345010
			store := &webAnalysisStore{analysisStore: &analysisStore{sqlDB: db, report: &database.Report{Seq: seq, Description: tc.original}}, intake: database.HumanWebIntake{Text: tc.original}, visibility: "shadow"}
			client := &subjectAnalysisClient{analysisClient: &analysisClient{}, brand: tc.modelBrand, original: tc.original}
			publisher := &recordingPublisher{}
			svc := &Service{config: &config.Config{TranslationLanguages: map[string]string{"de": "German"}}, db: store, llmClient: client, brandService: services.NewBrandService(), contactService: contacts.NewContactService(db), publisher: publisher}
			mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow(tc.original, nil))
			mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
			mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow("web_subject_regression", "text"))
			mock.ExpectQuery(`SELECT submission_id FROM wire_submissions_raw`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"submission_id"}).AddRow("web_subject_regression"))
			originalJSON, _ := json.Marshal(map[string]string{"problem_type": "citizen_submission", "description": tc.original})
			mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE submission_id = \? AND report_seq = \?`).WithArgs("web_subject_regression", seq).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(originalJSON, "human", "web"))
			if tc.wantBrand != "" {
				// No Grok/xAI contact lookup is allowed for this CleanApp complaint.
				mock.ExpectQuery(`SELECT .* FROM brand_contacts`).WithArgs("cleanapp").WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			if err := svc.AnalyzeReport(store.report); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(client.input, tc.original) || client.evidenceTarget != tc.wantBrand {
				t.Fatalf("original or evidence target changed: target=%q want=%q", client.evidenceTarget, tc.wantBrand)
			}
			if len(store.analyses) != 2 || len(publisher.events) != 1 || len(publisher.events[0].Analysis) != 2 {
				t.Fatal("expected one canonical analysis, one translation and one publication")
			}
			for _, a := range store.analyses {
				if a.BrandName != tc.wantBrand || a.BrandDisplayName != tc.wantDisplay || a.InferredContactEmails != "" {
					t.Fatalf("analysis persisted an incidental brand or recipient: %+v", a)
				}
			}
			for _, a := range publisher.events[0].Analysis {
				if a.BrandName != tc.wantBrand || a.BrandDisplayName != tc.wantDisplay {
					t.Fatal("downstream event used an incidental brand")
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
