package database

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
)

func TestHumanWebIntakeRequiresAdapterProvenanceAndPreservesUnknownLocation(t *testing.T) {
	for _, tc := range []struct {
		name, fetcher, actor, channel, ext string
		want                               bool
	}{
		{"canonical", "human-web", "human", "web", `{"web_intake_pending_analysis":true}`, true},
		{"machine claims human", "external-fetcher", "human", "web", `{"web_intake_pending_analysis":true}`, false},
		{"wrong actor", "human-web", "machine", "web", `{"web_intake_pending_analysis":true}`, false},
		{"wrong channel", "human-web", "human", "wire", `{"web_intake_pending_analysis":true}`, false},
		{"old web no marker", "human-web", "human", "web", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			fullText := strings.Repeat("original ", 100)
			mock.ExpectQuery("SELECT rr.fetcher_id").WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"fetcher_id", "submission_id", "actor_kind", "channel"}).AddRow(tc.fetcher, "sub_test", tc.actor, tc.channel))
			if tc.fetcher == "human-web" && tc.actor == "human" && tc.channel == "web" {
				mock.ExpectQuery("SELECT report_json, extensions_json").WithArgs("sub_test", 7).WillReturnRows(sqlmock.NewRows([]string{"report_json", "extensions_json"}).AddRow(`{"problem_type":"citizen_submission","description":"`+fullText+`"}`, tc.ext))
			}
			got, active, err := (&Database{db: db}).HumanWebIntake(context.Background(), 7)
			if err != nil || active != tc.want {
				t.Fatalf("active=%v err=%v", active, err)
			}
			if active && (got.Text != fullText || got.HasLocation) {
				t.Fatal("original truncated or missing location invented")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHumanWebIntakeRetriesIfWirePersistenceHasNotCaughtUpToQueue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT rr.fetcher_id").WithArgs(8).WillReturnRows(sqlmock.NewRows([]string{"fetcher_id", "submission_id", "actor_kind", "channel"}).AddRow("human-web", nil, nil, nil))
	_, active, err := (&Database{db: db}).HumanWebIntake(context.Background(), 8)
	if err == nil || active {
		t.Fatal("unpersisted original was treated as complete")
	}
}
