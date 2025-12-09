package osm

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"
)

const (
	// CacheGridSize is the grid size in meters for coordinate rounding (100m)
	CacheGridSize = 100.0
	// CacheTTL is how long cached results are valid
	CacheTTL = 365 * 24 * time.Hour // 1 year
)

// CachedLocationService wraps the OSM client with database caching
type CachedLocationService struct {
	client *Client
	db     *sql.DB
}

// NewCachedLocationService creates a new cached location service
func NewCachedLocationService(db *sql.DB) *CachedLocationService {
	return &CachedLocationService{
		client: NewClient(),
		db:     db,
	}
}

// Client returns the underlying OSM client for direct access
func (s *CachedLocationService) Client() *Client {
	return s.client
}

// CreateCacheTable creates the OSM location cache table if it doesn't exist
func (s *CachedLocationService) CreateCacheTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS osm_location_cache (
			id INT AUTO_INCREMENT PRIMARY KEY,
			lat_grid DOUBLE NOT NULL,
			lon_grid DOUBLE NOT NULL,
			location_context JSON NOT NULL,
			inferred_emails TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL,
			UNIQUE KEY idx_lat_lon (lat_grid, lon_grid),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create osm_location_cache table: %w", err)
	}
	log.Println("osm_location_cache table verified/created")
	return nil
}

// roundToGrid rounds a coordinate to the cache grid size
// This allows caching results for nearby coordinates
func roundToGrid(coord float64) float64 {
	// Convert degrees to approximate meters (at equator, 1 degree ≈ 111,320 meters)
	// For simplicity, we use a fixed conversion factor
	metersPerDegree := 111320.0
	gridDegrees := CacheGridSize / metersPerDegree
	return math.Round(coord/gridDegrees) * gridDegrees
}

// GetLocationContext retrieves location context, using cache if available
func (s *CachedLocationService) GetLocationContext(lat, lon float64) (*LocationContext, error) {
	latGrid := roundToGrid(lat)
	lonGrid := roundToGrid(lon)

	// Try to get from cache first
	ctx, err := s.getFromCache(latGrid, lonGrid)
	if err == nil && ctx != nil {
		log.Printf("OSM cache hit for (%.6f, %.6f) -> grid (%.6f, %.6f)", lat, lon, latGrid, lonGrid)
		return ctx, nil
	}

	// Cache miss - fetch from OSM
	log.Printf("OSM cache miss for (%.6f, %.6f), fetching from Nominatim", lat, lon)
	ctx, err = s.client.ReverseGeocode(lat, lon)
	if err != nil {
		return nil, err
	}

	// Store in cache
	if err := s.saveToCache(latGrid, lonGrid, ctx); err != nil {
		log.Printf("Warning: failed to cache OSM result: %v", err)
		// Don't fail the request, just log the warning
	}

	return ctx, nil
}

// getFromCache retrieves a cached location context
func (s *CachedLocationService) getFromCache(latGrid, lonGrid float64) (*LocationContext, error) {
	var contextJSON string
	err := s.db.QueryRow(`
		SELECT location_context 
		FROM osm_location_cache 
		WHERE lat_grid = ? AND lon_grid = ? AND expires_at > NOW()
	`, latGrid, lonGrid).Scan(&contextJSON)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query cache: %w", err)
	}

	var ctx LocationContext
	if err := json.Unmarshal([]byte(contextJSON), &ctx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached context: %w", err)
	}

	return &ctx, nil
}

// saveToCache stores a location context in the cache
func (s *CachedLocationService) saveToCache(latGrid, lonGrid float64, ctx *LocationContext) error {
	contextJSON, err := json.Marshal(ctx)
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	expiresAt := time.Now().Add(CacheTTL)

	_, err = s.db.Exec(`
		INSERT INTO osm_location_cache (lat_grid, lon_grid, location_context, expires_at)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE 
			location_context = VALUES(location_context),
			expires_at = VALUES(expires_at),
			created_at = NOW()
	`, latGrid, lonGrid, string(contextJSON), expiresAt)

	if err != nil {
		return fmt.Errorf("failed to save to cache: %w", err)
	}

	return nil
}

// SaveInferredEmails updates the cache with inferred emails for a location
func (s *CachedLocationService) SaveInferredEmails(lat, lon float64, emails string) error {
	latGrid := roundToGrid(lat)
	lonGrid := roundToGrid(lon)

	_, err := s.db.Exec(`
		UPDATE osm_location_cache 
		SET inferred_emails = ?
		WHERE lat_grid = ? AND lon_grid = ?
	`, emails, latGrid, lonGrid)

	if err != nil {
		return fmt.Errorf("failed to save inferred emails: %w", err)
	}

	return nil
}

// GetCachedInferredEmails retrieves cached inferred emails for a location
func (s *CachedLocationService) GetCachedInferredEmails(lat, lon float64) (string, error) {
	latGrid := roundToGrid(lat)
	lonGrid := roundToGrid(lon)

	var emails sql.NullString
	err := s.db.QueryRow(`
		SELECT inferred_emails 
		FROM osm_location_cache 
		WHERE lat_grid = ? AND lon_grid = ? AND expires_at > NOW()
	`, latGrid, lonGrid).Scan(&emails)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to query cached emails: %w", err)
	}

	return emails.String, nil
}

// CleanExpiredCache removes expired cache entries
func (s *CachedLocationService) CleanExpiredCache() (int64, error) {
	result, err := s.db.Exec("DELETE FROM osm_location_cache WHERE expires_at < NOW()")
	if err != nil {
		return 0, fmt.Errorf("failed to clean expired cache: %w", err)
	}

	rows, _ := result.RowsAffected()
	return rows, nil
}
