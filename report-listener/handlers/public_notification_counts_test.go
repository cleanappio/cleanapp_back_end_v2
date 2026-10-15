package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"report-listener/models"
)

func TestPublicCardsUseOneBulkCountForEachOpenedRepresentative(t *testing.T) {
	rows := []models.ReportWithMinimalAnalysis{
		{Report: models.Report{Seq: 7, PublicID: "canonical-7"}, Analysis: []models.MinimalAnalysis{{Classification: "digital", Title: "First finding"}}, Recurrence: &models.PublicRecurrence{SupportingReportCount: 200}},
		{Report: models.Report{Seq: 11, PublicID: "canonical-11"}, Analysis: []models.MinimalAnalysis{{Classification: "digital", Title: "Second finding"}}},
	}
	four, zero := 4, 0
	calls := 0
	cards, err := discoveryCardsWithNotificationCounts(context.Background(), rows, "en", func(classification, publicID string) (string, error) {
		return "sealed-" + publicID, nil
	}, func(ctx context.Context, seqs []int) (map[int]models.PublicNotificationCount, error) {
		calls++
		if !reflect.DeepEqual(seqs, []int{7, 11}) {
			t.Fatalf("counts must use canonical representatives, not supporting family: %v", seqs)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("optional count lookup lacks its bounded deadline")
		}
		return map[int]models.PublicNotificationCount{7: {State: "sent", EmailCount: &four}, 11: {State: "none_recorded", EmailCount: &zero}}, nil
	})
	if err != nil || calls != 1 || len(cards) != 2 || cards[0].DiscoveryToken != "sealed-canonical-7" || *cards[0].NotificationCount.EmailCount != 4 || *cards[1].NotificationCount.EmailCount != 0 {
		t.Fatalf("representative count projection: %+v calls=%d err=%v", cards, calls, err)
	}
	encoded, err := json.Marshal(cards)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"notification_count":{"state":"sent","email_count":4}`) || !strings.Contains(string(encoded), `"state":"none_recorded","email_count":0`) {
		t.Fatalf("known count JSON: %s", encoded)
	}
	for _, forbidden := range []string{"recipient", "provider", "delivery_source", "@", "seq\""} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public card disclosed %q: %s", forbidden, encoded)
		}
	}
}

func TestPublicCardCountFailureKeepsReportsAndDoesNotInventZero(t *testing.T) {
	rows := []models.ReportWithMinimalAnalysis{{Report: models.Report{Seq: 7, PublicID: "canonical-7"}, Analysis: []models.MinimalAnalysis{{Classification: "digital", Title: "Visible finding"}}}}
	cards, err := discoveryCardsWithNotificationCounts(context.Background(), rows, "en", func(string, string) (string, error) { return "sealed", nil },
		func(context.Context, []int) (map[int]models.PublicNotificationCount, error) {
			return nil, errors.New("ledger unavailable")
		})
	if err != nil || len(cards) != 1 || cards[0].Title != "Visible finding" || cards[0].NotificationCount.State != "unavailable" || cards[0].NotificationCount.EmailCount != nil {
		t.Fatalf("optional counter failure broke the report page: %+v err=%v", cards, err)
	}
	encoded, _ := json.Marshal(cards[0].NotificationCount)
	if string(encoded) != `{"state":"unavailable"}` {
		t.Fatalf("unavailable response fabricated a count: %s", encoded)
	}
}

func TestPublicCardCountDeadlineKeepsTheReportUsable(t *testing.T) {
	rows := []models.ReportWithMinimalAnalysis{{Report: models.Report{Seq: 7, PublicID: "canonical-7"}, Analysis: []models.MinimalAnalysis{{Classification: "digital", Title: "Visible finding"}}}}
	for _, lookupError := range []error{context.DeadlineExceeded, context.Canceled} {
		cards, err := discoveryCardsWithNotificationCounts(context.Background(), rows, "en", func(string, string) (string, error) { return "sealed", nil },
			func(context.Context, []int) (map[int]models.PublicNotificationCount, error) { return nil, lookupError })
		if err != nil || len(cards) != 1 || cards[0].Title != "Visible finding" || cards[0].NotificationCount.State != "unavailable" || cards[0].NotificationCount.EmailCount != nil {
			t.Fatalf("optional counter deadline/cancellation broke page: %+v error=%v", cards, err)
		}
	}
}
