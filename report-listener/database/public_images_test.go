package database

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPublicImageReadersRequirePrivacyCheckAndFailClosed(t *testing.T) {
	for _, preview := range []bool{false, true} {
		for _, unavailable := range []bool{false, true} {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			d := &Database{db: db}
			query := mock.ExpectQuery(`SELECT.*NOT EXISTS.*FROM wire_submissions_raw media_wire.*media_wire.report_seq = r.seq.*privacy.contains_pii.*privacy.requires_redaction`)
			if preview {
				query.WithArgs(1024, "rpt_example")
			} else {
				query.WithArgs("rpt_example")
			}
			if unavailable {
				query.WillReturnError(errors.New("privacy metadata unavailable"))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"image"}))
			}
			var image []byte
			if preview {
				image, err = d.GetPublicReportMediaByPublicID(context.Background(), "rpt_example", 1024)
			} else {
				image, err = d.GetImageByPublicID(context.Background(), "rpt_example")
			}
			if err == nil || len(image) != 0 {
				t.Fatal("public reader returned media without an affirmative privacy check")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			db.Close()
		}
	}
}
