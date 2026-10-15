package contacts

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReportExtractionRetainsSuggestionsWithoutVerifyingOrUpdatingContacts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewContactService(db)
	mock.ExpectQuery(`SELECT .* FROM brand_contacts`).WithArgs("otherbrand").WillReturnRows(sqlmock.NewRows([]string{
		"id", "brand_name", "product_name", "contact_name", "contact_title", "contact_level", "email", "email_verified", "twitter_handle", "linkedin_url", "github_handle", "source", "created_at",
	}))
	suggestionQuery := regexp.QuoteMeta(`INSERT INTO brand_contacts
		(brand_name,contact_name,contact_title,contact_level,email,email_verified,twitter_handle,source)
		VALUES (?,'User Reported Contact','Reported via App','ic',?,FALSE,?,'manual')
		ON DUPLICATE KEY UPDATE id=id`)
	mock.ExpectExec(suggestionQuery).WithArgs("otherbrand", "candidate@example.org", "").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(suggestionQuery).WithArgs("otherbrand", "", "@candidate").WillReturnResult(sqlmock.NewResult(2, 1))
	if err := service.ProcessReportDescription("otherbrand", "Quoted contact candidate@example.org and @candidate"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
