package database

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReportOutcomeTimelineIsOrderedCollapsedAndAllowlisted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t0 := time.Date(2026, 11, 11, 14, 17, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"outcome_type", "recorded_at", "ticket"}).
		AddRow("sent", t0, "").                                              // covered by summary events
		AddRow("acknowledged", t0.Add(time.Minute), "48213").                // ticket shown
		AddRow("viewed", t0.Add(time.Hour), "").                             //
		AddRow("viewed", t0.Add(3*time.Hour), "").                           // collapses into the first view
		AddRow("misrouted", t0.Add(4*time.Hour), "").                        // internal: not published
		AddRow("replied", t0.Add(5*time.Hour), "<script>alert(1)</script>"). // hostile "ticket" dropped
		AddRow("Bounced ", t0.Add(6*time.Hour), "this-is-far-too-long-to-be-a-ticket-reference")
	mock.ExpectQuery(regexp.QuoteMeta(reportOutcomeTimelineSQL)).WithArgs("rpt_abc").WillReturnRows(rows)
	timeline, err := (&Database{db: db}).ReportOutcomeTimeline(context.Background(), "rpt_abc")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind, ticket string
		count        int
	}{{"acknowledged", "48213", 1}, {"viewed", "", 2}, {"replied", "", 1}, {"bounced", "", 1}}
	if len(timeline) != len(want) {
		t.Fatalf("timeline=%+v", timeline)
	}
	for i, w := range want {
		if timeline[i].Type != w.kind || timeline[i].TicketRef != w.ticket || timeline[i].Count != w.count {
			t.Fatalf("event %d = %+v, want %+v", i, timeline[i], w)
		}
	}
	if !timeline[1].At.Equal(t0.Add(time.Hour)) {
		t.Fatalf("collapsed event must keep its first time: %v", timeline[1].At)
	}
}

func TestReportOutcomeTimelineEmptyAndFailing(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(reportOutcomeTimelineSQL)).WithArgs("rpt_none").WillReturnRows(sqlmock.NewRows([]string{"outcome_type", "recorded_at", "ticket"}))
	timeline, err := (&Database{db: db}).ReportOutcomeTimeline(context.Background(), "rpt_none")
	if err != nil || timeline == nil || len(timeline) != 0 {
		t.Fatalf("empty timeline must be an empty list, not null: %v %v", timeline, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(reportOutcomeTimelineSQL)).WillReturnError(errors.New("db down"))
	if _, err := (&Database{db: db}).ReportOutcomeTimeline(context.Background(), "rpt_x"); err == nil {
		t.Fatal("query failure was hidden")
	}
}
