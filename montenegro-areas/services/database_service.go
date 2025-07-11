package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"montenegro-areas/models"

	_ "github.com/go-sql-driver/mysql"
)

// DatabaseService manages database connections and queries
type DatabaseService struct {
	db           *sql.DB
	areasService *AreasService
}

// NewDatabaseService creates a new database service
func NewDatabaseService(areasService *AreasService) (*DatabaseService, error) {
	// Get database connection details from environment variables
	dbUser := getEnvOrDefault("DB_USER", "server")
	dbPassword := getEnvOrDefault("DB_PASSWORD", "secret_app")
	dbHost := getEnvOrDefault("DB_HOST", "localhost")
	dbPort := getEnvOrDefault("DB_PORT", "3306")
	dbName := getEnvOrDefault("DB_NAME", "cleanapp")

	// Create DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		dbUser, dbPassword, dbHost, dbPort, dbName)

	// Open database connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	log.Printf("Database connection established to %s:%s/%s", dbHost, dbPort, dbName)

	return &DatabaseService{db: db, areasService: areasService}, nil
}

// Close closes the database connection
func (s *DatabaseService) Close() error {
	return s.db.Close()
}

// GetReportsByMontenegroArea gets the last n reports that are contained within a given MontenegroArea
func (s *DatabaseService) GetReportsByMontenegroArea(osmID int64, n int) ([]models.ReportData, error) {
	// Find the MontenegroArea by OSM ID
	var targetArea *models.MontenegroArea
	for _, areas := range s.areasService.areas {
		for _, area := range areas {
			if area.OSMID == osmID {
				targetArea = &area
				break
			}
		}
		if targetArea != nil {
			break
		}
	}

	if targetArea == nil {
		return nil, fmt.Errorf("MontenegroArea with OSM ID %d not found", osmID)
	}

	// Convert the area geometry to WKT format for spatial query
	// The area.Area contains the raw GeoJSON geometry, we need to convert it to WKT
	areaWKT, err := s.convertGeoJSONToWKT(targetArea.Area)
	if err != nil {
		return nil, fmt.Errorf("failed to convert area geometry to WKT: %w", err)
	}

	// Query to get reports within the area using spatial functions
	query := `
		SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id
		FROM reports r
		JOIN reports_geometry rg ON r.seq = rg.seq
		WHERE ST_Within(rg.geom, ST_GeomFromText(?, 4326))
		ORDER BY r.ts DESC
		LIMIT ?
	`

	rows, err := s.db.Query(query, areaWKT, n)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer rows.Close()

	var reports []models.ReportData
	for rows.Next() {
		var report models.ReportData
		var timestamp time.Time
		var x, y sql.NullFloat64
		var actionID sql.NullString

		err := rows.Scan(
			&report.Seq,
			&timestamp,
			&report.ID,
			&report.Team,
			&report.Latitude,
			&report.Longitude,
			&x,
			&y,
			&actionID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}

		// Convert timestamp to string
		report.Timestamp = timestamp.Format(time.RFC3339)

		// Handle nullable fields
		if x.Valid {
			report.X = &x.Float64
		}
		if y.Valid {
			report.Y = &y.Float64
		}
		if actionID.Valid {
			report.ActionID = &actionID.String
		}

		reports = append(reports, report)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	return reports, nil
}

// GetReportsAggregatedData returns aggregated reports data for all areas of admin level 6
func (s *DatabaseService) GetReportsAggregatedData() ([]models.AreaAggrData, error) {
	// Get all areas of admin level 6
	areas, err := s.areasService.GetAreasByAdminLevel(6)
	if err != nil {
		return nil, fmt.Errorf("failed to get areas for admin level 6: %w", err)
	}

	if len(areas) == 0 {
		return []models.AreaAggrData{}, nil
	}

	// Calculate median reports count across all areas
	// First, get all report counts for each area
	var reportCounts []int
	for _, area := range areas {
		areaWKT, err := s.convertGeoJSONToWKT(area.Area)
		if err != nil {
			log.Printf("Warning: failed to convert area geometry for OSM ID %d: %v", area.OSMID, err)
			continue
		}

		query := `SELECT COUNT(r.seq) as reports_count
			FROM reports r
			JOIN reports_geometry rg ON r.seq = rg.seq
			WHERE ST_Within(rg.geom, ST_GeomFromText(?, 4326))`

		var count int
		err = s.db.QueryRow(query, areaWKT).Scan(&count)
		if err != nil {
			log.Printf("Warning: failed to get report count for OSM ID %d: %v", area.OSMID, err)
			count = 0
		}
		reportCounts = append(reportCounts, count)
	}

	// Calculate median from the collected counts
	var medianCount float64
	if len(reportCounts) > 0 {
		// Sort the counts to find median
		sort.Ints(reportCounts)
		if len(reportCounts)%2 == 0 {
			// Even number of elements, take average of middle two
			mid := len(reportCounts) / 2
			medianCount = float64(reportCounts[mid-1]+reportCounts[mid]) / 2.0
		} else {
			// Odd number of elements, take middle element
			mid := len(reportCounts) / 2
			medianCount = float64(reportCounts[mid])
		}
	}

	// Get aggregated data for each area
	var areasData []models.AreaAggrData
	for _, area := range areas {
		areaWKT, err := s.convertGeoJSONToWKT(area.Area)
		if err != nil {
			log.Printf("Warning: failed to convert area geometry for OSM ID %d: %v", area.OSMID, err)
			continue
		}

		// Query to get aggregated data for this area
		query := `
			SELECT 
				COUNT(r.seq) as reports_count,
				COALESCE(AVG(ra.severity_level), 0.0) as mean_severity,
				COALESCE(AVG(ra.litter_probability), 0.0) as mean_litter_probability,
				COALESCE(AVG(ra.hazard_probability), 0.0) as mean_hazard_probability
			FROM reports r
			JOIN reports_geometry rg ON r.seq = rg.seq
			LEFT JOIN report_analysis ra ON r.seq = ra.seq
			WHERE ST_Within(rg.geom, ST_GeomFromText(?, 4326))
		`

		var areaData models.AreaAggrData
		err = s.db.QueryRow(query, areaWKT).Scan(
			&areaData.ReportsCount,
			&areaData.MeanSeverity,
			&areaData.MeanLitterProbability,
			&areaData.MeanHazardProbability,
		)
		if err != nil {
			log.Printf("Warning: failed to get aggregated data for OSM ID %d: %v", area.OSMID, err)
			// Set default values for this area
			areaData.ReportsCount = 0
			areaData.MeanSeverity = 0.0
			areaData.MeanLitterProbability = 0.0
			areaData.MeanHazardProbability = 0.0
		}

		// Set the area metadata
		areaData.OSMID = area.OSMID
		areaData.Name = area.Name
		areaData.ReportsMedian = medianCount

		areasData = append(areasData, areaData)
	}

	return areasData, nil
}

// getEnvOrDefault gets an environment variable or returns a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// convertGeoJSONToWKT converts a GeoJSON geometry to WKT format
func (s *DatabaseService) convertGeoJSONToWKT(geometry json.RawMessage) (string, error) {
	// Parse the GeoJSON geometry
	var geom models.Geometry
	if err := json.Unmarshal(geometry, &geom); err != nil {
		return "", fmt.Errorf("failed to unmarshal geometry: %w", err)
	}

	// Convert based on geometry type
	switch geom.Type {
	case "Polygon":
		return s.convertPolygonToWKT(geom.Coordinates)
	case "MultiPolygon":
		return s.convertMultiPolygonToWKT(geom.Coordinates)
	default:
		return "", fmt.Errorf("unsupported geometry type: %s", geom.Type)
	}
}

// convertPolygonToWKT converts a GeoJSON polygon to WKT format
func (s *DatabaseService) convertPolygonToWKT(coordinates json.RawMessage) (string, error) {
	var coords [][][]float64
	if err := json.Unmarshal(coordinates, &coords); err != nil {
		return "", fmt.Errorf("failed to unmarshal polygon coordinates: %w", err)
	}

	if len(coords) == 0 {
		return "", fmt.Errorf("empty polygon coordinates")
	}

	// Convert to WKT format
	var rings []string
	for _, ring := range coords {
		var points []string
		for _, point := range ring {
			if len(point) >= 2 {
				// WKT format: longitude latitude (note the order)
				points = append(points, fmt.Sprintf("%g %g", point[0], point[1]))
			}
		}
		rings = append(rings, fmt.Sprintf("(%s)", strings.Join(points, ",")))
	}

	return fmt.Sprintf("POLYGON(%s)", strings.Join(rings, ",")), nil
}

// convertMultiPolygonToWKT converts a GeoJSON multi-polygon to WKT format
func (s *DatabaseService) convertMultiPolygonToWKT(coordinates json.RawMessage) (string, error) {
	var coords [][][][]float64
	if err := json.Unmarshal(coordinates, &coords); err != nil {
		return "", fmt.Errorf("failed to unmarshal multi-polygon coordinates: %w", err)
	}

	if len(coords) == 0 {
		return "", fmt.Errorf("empty multi-polygon coordinates")
	}

	// Convert each polygon
	var polygons []string
	for _, polygon := range coords {
		// Convert polygon to JSON and then to WKT
		polygonJSON, err := json.Marshal(polygon)
		if err != nil {
			return "", fmt.Errorf("failed to marshal polygon: %w", err)
		}
		polygonWKT, err := s.convertPolygonToWKT(polygonJSON)
		if err != nil {
			return "", fmt.Errorf("failed to convert polygon: %w", err)
		}
		// Remove the POLYGON wrapper and add to the list
		polygonWKT = strings.TrimPrefix(polygonWKT, "POLYGON(")
		polygonWKT = strings.TrimSuffix(polygonWKT, ")")
		polygons = append(polygons, fmt.Sprintf("(%s)", polygonWKT))
	}

	return fmt.Sprintf("MULTIPOLYGON(%s)", strings.Join(polygons, ",")), nil
}
