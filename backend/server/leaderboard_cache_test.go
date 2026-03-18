package server

import (
	"testing"
	"time"

	"cleanapp/backend/server/api"

	"github.com/DATA-DOG/go-sqlmock"
)

func resetLeaderboardSelfRankCache() {
	leaderboardSelfRankCache.mu.Lock()
	leaderboardSelfRankCache.entries = map[string]selfRankSnapshot{}
	leaderboardSelfRankCache.mu.Unlock()
}

func TestBuildTopScoresResponseAppendsSelfRankWhenMissingFromTopSlice(t *testing.T) {
	resetLeaderboardSelfRankCache()

	dbc, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer dbc.Close()

	userID := "0xme"
	topCount := 7
	snapshot := []topScoresSnapshot{
		{ID: "0x1", Title: "one", Kitn: 100},
		{ID: "0x2", Title: "two", Kitn: 90},
	}

	mock.ExpectQuery("SELECT id, avatar, kitns_daily").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "avatar", "cnt"}).
			AddRow(userID, "My Avatar", 42.0))
	mock.ExpectQuery("SELECT count\\(\\*\\) AS c").
		WithArgs(42.0).
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(12))

	resp := buildTopScoresResponse(dbc, snapshot, userID, topCount)
	if len(resp.Records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(resp.Records))
	}

	you := resp.Records[2]
	if !you.IsYou {
		t.Fatalf("expected appended record to be marked as self")
	}
	if you.Title != "My Avatar" {
		t.Fatalf("expected self title My Avatar, got %q", you.Title)
	}
	if you.Place != 13 {
		t.Fatalf("expected place 13, got %d", you.Place)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildTopScoresResponseKeepsExistingSelfRow(t *testing.T) {
	resetLeaderboardSelfRankCache()

	dbc, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer dbc.Close()

	userID := "0xme"
	resp := buildTopScoresResponse(dbc, []topScoresSnapshot{
		{ID: userID, Title: "Me", Kitn: 120},
		{ID: "0x2", Title: "two", Kitn: 110},
	}, userID, 7)

	if len(resp.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(resp.Records))
	}
	if !resp.Records[0].IsYou {
		t.Fatalf("expected first record to remain self")
	}
}

func TestGetSelfRankCachedUsesCacheWithinTTL(t *testing.T) {
	resetLeaderboardSelfRankCache()

	dbc, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer dbc.Close()

	userID := "0xme"
	record := api.TopScoresRecord{
		Place: 9,
		Title: "Cached Me",
		Kitn:  77,
		IsYou: true,
	}
	leaderboardSelfRankCache.mu.Lock()
	leaderboardSelfRankCache.entries[userID] = selfRankSnapshot{
		record:     record,
		lastLoaded: time.Now(),
	}
	leaderboardSelfRankCache.mu.Unlock()

	got, err := getSelfRankCached(dbc, userID, 7)
	if err != nil {
		t.Fatalf("getSelfRankCached: %v", err)
	}
	if got != record {
		t.Fatalf("expected cached record %+v, got %+v", record, got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql calls: %v", err)
	}
}
