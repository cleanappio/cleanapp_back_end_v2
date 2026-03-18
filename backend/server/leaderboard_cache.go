package server

import (
	"database/sql"
	"sync"
	"time"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
)

const (
	leaderboardCacheTTL         = 2 * time.Minute
	leaderboardWarmupDelay      = 5 * time.Second
	leaderboardRefreshInterval  = 2 * time.Minute
	defaultLeaderboardTopScores = 7
)

type teamsCacheState struct {
	mu         sync.RWMutex
	value      api.TeamsResponse
	lastLoaded time.Time
	loaded     bool
	refreshing bool
}

type topScoresSnapshot struct {
	ID    string
	Title string
	Kitn  float64
}

type topScoresCacheState struct {
	mu         sync.RWMutex
	value      []topScoresSnapshot
	lastLoaded time.Time
	loaded     bool
	refreshing bool
}

var (
	leaderboardTeamsCache     = &teamsCacheState{}
	leaderboardTopScoresCache = &topScoresCacheState{}
	leaderboardSelfRankCache  = &selfRankCacheState{entries: map[string]selfRankSnapshot{}}
)

type selfRankSnapshot struct {
	record     api.TopScoresRecord
	lastLoaded time.Time
}

type selfRankCacheState struct {
	mu      sync.RWMutex
	entries map[string]selfRankSnapshot
}

func StartLeaderboardCacheUpdater() {
	log.Info("Starting leaderboard cache updater...")

	go func() {
		time.Sleep(leaderboardWarmupDelay)
		refreshLeaderboardCaches()

		ticker := time.NewTicker(leaderboardRefreshInterval)
		defer ticker.Stop()

		for range ticker.C {
			refreshLeaderboardCaches()
		}
	}()
}

func refreshLeaderboardCaches() {
	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("leaderboard cache: failed to connect to DB: %v", err)
		return
	}

	if _, err := getTeamsCached(dbc); err != nil {
		log.Errorf("leaderboard cache: failed to refresh teams: %v", err)
	}
	if _, err := getTopScoresCached(dbc, defaultLeaderboardTopScores); err != nil {
		log.Errorf("leaderboard cache: failed to refresh top scores: %v", err)
	}
}

func getTeamsCached(dbc *sql.DB) (api.TeamsResponse, error) {
	leaderboardTeamsCache.mu.RLock()
	if leaderboardTeamsCache.loaded && time.Since(leaderboardTeamsCache.lastLoaded) < leaderboardCacheTTL {
		value := leaderboardTeamsCache.value
		leaderboardTeamsCache.mu.RUnlock()
		return value, nil
	}
	hasLoaded := leaderboardTeamsCache.loaded
	stale := leaderboardTeamsCache.value
	refreshing := leaderboardTeamsCache.refreshing
	leaderboardTeamsCache.mu.RUnlock()

	if hasLoaded {
		if !refreshing {
			go refreshTeamsCacheAsync()
		}
		return stale, nil
	}

	return refreshTeamsCache(dbc)
}

func refreshTeamsCacheAsync() {
	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("leaderboard cache: async teams DB connect failed: %v", err)
		return
	}
	if _, err := refreshTeamsCache(dbc); err != nil {
		log.Errorf("leaderboard cache: async teams refresh failed: %v", err)
	}
}

func refreshTeamsCache(dbc *sql.DB) (api.TeamsResponse, error) {
	leaderboardTeamsCache.mu.Lock()
	if leaderboardTeamsCache.refreshing {
		value := leaderboardTeamsCache.value
		loaded := leaderboardTeamsCache.loaded
		leaderboardTeamsCache.mu.Unlock()
		if loaded {
			return value, nil
		}
		return db.GetTeamsWithDB(dbc)
	}
	leaderboardTeamsCache.refreshing = true
	leaderboardTeamsCache.mu.Unlock()

	defer func() {
		leaderboardTeamsCache.mu.Lock()
		leaderboardTeamsCache.refreshing = false
		leaderboardTeamsCache.mu.Unlock()
	}()

	value, err := db.GetTeamsWithDB(dbc)
	if err != nil {
		return api.TeamsResponse{}, err
	}

	leaderboardTeamsCache.mu.Lock()
	leaderboardTeamsCache.value = value
	leaderboardTeamsCache.lastLoaded = time.Now()
	leaderboardTeamsCache.loaded = true
	leaderboardTeamsCache.mu.Unlock()
	return value, nil
}

func getTopScoresCached(dbc *sql.DB, topCount int) ([]topScoresSnapshot, error) {
	if topCount <= 0 {
		topCount = defaultLeaderboardTopScores
	}

	leaderboardTopScoresCache.mu.RLock()
	if leaderboardTopScoresCache.loaded &&
		len(leaderboardTopScoresCache.value) >= topCount &&
		time.Since(leaderboardTopScoresCache.lastLoaded) < leaderboardCacheTTL {
		snapshot := append([]topScoresSnapshot(nil), leaderboardTopScoresCache.value[:topCount]...)
		leaderboardTopScoresCache.mu.RUnlock()
		return snapshot, nil
	}
	hasLoaded := leaderboardTopScoresCache.loaded
	var stale []topScoresSnapshot
	if leaderboardTopScoresCache.loaded {
		count := min(topCount, len(leaderboardTopScoresCache.value))
		stale = append([]topScoresSnapshot(nil), leaderboardTopScoresCache.value[:count]...)
	}
	refreshing := leaderboardTopScoresCache.refreshing
	leaderboardTopScoresCache.mu.RUnlock()

	if hasLoaded && len(stale) > 0 {
		if !refreshing {
			go refreshTopScoresCacheAsync(topCount)
		}
		return stale, nil
	}

	return refreshTopScoresCache(dbc, topCount)
}

func refreshTopScoresCacheAsync(topCount int) {
	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("leaderboard cache: async top scores DB connect failed: %v", err)
		return
	}
	if _, err := refreshTopScoresCache(dbc, topCount); err != nil {
		log.Errorf("leaderboard cache: async top scores refresh failed: %v", err)
	}
}

func refreshTopScoresCache(dbc *sql.DB, topCount int) ([]topScoresSnapshot, error) {
	leaderboardTopScoresCache.mu.Lock()
	if leaderboardTopScoresCache.refreshing {
		var stale []topScoresSnapshot
		if leaderboardTopScoresCache.loaded {
			count := min(topCount, len(leaderboardTopScoresCache.value))
			stale = append([]topScoresSnapshot(nil), leaderboardTopScoresCache.value[:count]...)
		}
		leaderboardTopScoresCache.mu.Unlock()
		if len(stale) > 0 {
			return stale, nil
		}
		return queryTopScoresSnapshot(dbc, topCount)
	}
	leaderboardTopScoresCache.refreshing = true
	leaderboardTopScoresCache.mu.Unlock()

	defer func() {
		leaderboardTopScoresCache.mu.Lock()
		leaderboardTopScoresCache.refreshing = false
		leaderboardTopScoresCache.mu.Unlock()
	}()

	snapshot, err := queryTopScoresSnapshot(dbc, topCount)
	if err != nil {
		return nil, err
	}

	leaderboardTopScoresCache.mu.Lock()
	leaderboardTopScoresCache.value = append([]topScoresSnapshot(nil), snapshot...)
	leaderboardTopScoresCache.lastLoaded = time.Now()
	leaderboardTopScoresCache.loaded = true
	leaderboardTopScoresCache.mu.Unlock()

	return snapshot, nil
}

func queryTopScoresSnapshot(dbc *sql.DB, topCount int) ([]topScoresSnapshot, error) {
	rows, err := dbc.Query(`
		SELECT id, avatar, kitns_daily + kitns_disbursed + kitns_ref_daily + kitns_ref_disbursed AS cnt
		FROM users
		ORDER BY cnt DESC
		LIMIT ?`, topCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshot := make([]topScoresSnapshot, 0, topCount)
	for rows.Next() {
		var item topScoresSnapshot
		if err := rows.Scan(&item.ID, &item.Title, &item.Kitn); err != nil {
			return nil, err
		}
		snapshot = append(snapshot, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func buildTopScoresResponse(dbc *sql.DB, snapshot []topScoresSnapshot, userID string, topCount int) *api.TopScoresResponse {
	resp := &api.TopScoresResponse{Records: make([]api.TopScoresRecord, 0, len(snapshot))}
	hasYou := false
	for idx, item := range snapshot {
		isYou := item.ID == userID
		if isYou {
			hasYou = true
		}
		resp.Records = append(resp.Records, api.TopScoresRecord{
			Place: idx + 1,
			Title: item.Title,
			Kitn:  item.Kitn,
			IsYou: isYou,
		})
	}
	if !hasYou && userID != "" {
		if you, err := getSelfRankCached(dbc, userID, topCount); err == nil {
			resp.Records = append(resp.Records, you)
		} else {
			log.Warnf("leaderboard cache: failed to load self rank for %s: %v", userID, err)
		}
	}
	return resp
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getSelfRankCached(dbc *sql.DB, userID string, topCount int) (api.TopScoresRecord, error) {
	leaderboardSelfRankCache.mu.RLock()
	if entry, ok := leaderboardSelfRankCache.entries[userID]; ok && time.Since(entry.lastLoaded) < leaderboardCacheTTL {
		record := entry.record
		leaderboardSelfRankCache.mu.RUnlock()
		return record, nil
	}
	leaderboardSelfRankCache.mu.RUnlock()

	record, err := querySelfRank(dbc, userID, topCount)
	if err != nil {
		return api.TopScoresRecord{}, err
	}

	leaderboardSelfRankCache.mu.Lock()
	leaderboardSelfRankCache.entries[userID] = selfRankSnapshot{
		record:     record,
		lastLoaded: time.Now(),
	}
	leaderboardSelfRankCache.mu.Unlock()
	return record, nil
}

func querySelfRank(dbc *sql.DB, userID string, topCount int) (api.TopScoresRecord, error) {
	var (
		id     string
		avatar string
		cnt    float64
	)
	err := dbc.QueryRow(`
		SELECT id, avatar, kitns_daily + kitns_disbursed + kitns_ref_daily + kitns_ref_disbursed AS cnt
		FROM users
		WHERE id = ?`, userID).Scan(&id, &avatar, &cnt)
	if err != nil {
		return api.TopScoresRecord{}, err
	}

	var aheadCount int
	if err := dbc.QueryRow(`
		SELECT count(*) AS c
		FROM users
		WHERE kitns_daily + kitns_disbursed + kitns_ref_daily + kitns_ref_disbursed > ?
	`, cnt).Scan(&aheadCount); err != nil {
		return api.TopScoresRecord{}, err
	}

	place := aheadCount + 1
	if aheadCount < topCount {
		place = topCount + 1
	}

	return api.TopScoresRecord{
		Place: place,
		Title: avatar,
		Kitn:  cnt,
		IsYou: true,
	}, nil
}
