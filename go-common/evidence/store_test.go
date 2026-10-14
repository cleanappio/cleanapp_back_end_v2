package evidence

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
)

func TestSaveNormalizesBoundsAndUpsertsOneReportSequence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fields := Fields{ReportKind: " FIRSTHAND ", EvidenceExcerpt: strings.Repeat("界", 605), ReportedImpact: " " + strings.Repeat("影", 510) + " ", ClassificationReason: " " + strings.Repeat("理", 510) + " ", EntityRelationship: " DEPENDENCY "}
	mock.ExpectExec(`INSERT INTO report_evidence.*ON DUPLICATE KEY UPDATE`).WithArgs(42, "firsthand", strings.Repeat("界", 600), strings.Repeat("影", 500), strings.Repeat("理", 500), "dependency", 1).WillReturnResult(sqlmock.NewResult(42, 1))
	if err := Save(context.Background(), db, 42, fields, Version); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFinalizedUnknownAndFailureVersionRemainDistinct(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, version := range []int{0, 1} {
		mock.ExpectExec(`INSERT INTO report_evidence`).WithArgs(7, "unknown", "", "", "No original", "unknown", version).WillReturnResult(sqlmock.NewResult(7, 1))
		if err := Save(context.Background(), db, 7, Fields{ReportKind: "invented", ClassificationReason: " No original ", EntityRelationship: "owner"}, version); err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFailureCannotDowngradeCompletedVersion(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_, query string) error {
		for _, field := range []string{"report_kind", "evidence_excerpt", "reported_impact", "classification_reason", "entity_relationship"} {
			gate := field + " = IF(VALUES(evidence_version) >= evidence_version, VALUES(" + field + "), " + field + ")"
			if !strings.Contains(query, gate) {
				return errors.New("evidence field can overwrite a newer version")
			}
		}
		assignment := "evidence_version = GREATEST(evidence_version, VALUES(evidence_version))"
		if !strings.HasSuffix(strings.TrimSpace(query), assignment) {
			return errors.New("version must advance last, never downgrade")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("monotonic evidence upsert").WithArgs(9, "unknown", "", "", "Retryable failure", "unknown", 0).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := Save(context.Background(), db, 9, Unknown("Retryable failure"), 0); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveValidatesIdentityAndVersionBeforeSQL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, input := range []struct{ seq, version int }{{0, 1}, {-1, 1}, {1, -1}} {
		if err := Save(context.Background(), db, input.seq, Unknown("Reason"), input.version); err == nil {
			t.Fatal("invalid storage identity/version accepted")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSavePropagatesWriteFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	failure := errors.New("evidence table unavailable")
	mock.ExpectExec(`INSERT INTO report_evidence`).WillReturnError(failure)
	if err := Save(context.Background(), db, 1, Unknown("Reason"), 1); !errors.Is(err, failure) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
