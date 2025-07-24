package services

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"custom-area-dashboard/models"

	_ "github.com/go-sql-driver/mysql"
)

// DatabaseService manages database connections and queries
type DatabaseService struct {
	db           *sql.DB
	areasService *AreasService
	wktConverter *WKTConverter
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

	return &DatabaseService{db: db, areasService: areasService, wktConverter: NewWKTConverter()}, nil
}

// Close closes the database connection
func (s *DatabaseService) Close() error {
	return s.db.Close()
}

// GetReportsByCustomArea gets the last n reports with analysis that are contained within a given custom area
func (s *DatabaseService) GetReportsByCustomArea(osmID int64, n int) ([]models.ReportWithAnalysis, error) {
	// Find the custom area by OSM ID
	var targetArea *models.CustomArea
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
		return nil, fmt.Errorf("custom area with OSM ID %d not found", osmID)
	}

	// Convert the area geometry to WKT format for spatial query
	// The area.Area contains the raw GeoJSON geometry, we need to convert it to WKT
	areaWKT, err := s.wktConverter.ConvertGeoJSONToWKT(targetArea.Area)
	if err != nil {
		return nil, fmt.Errorf("failed to convert area geometry to WKT: %w", err)
	}

	// First, get all reports within the area
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id
		FROM reports r
		JOIN reports_geometry rg ON r.seq = rg.seq
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE ST_Within(rg.geom, ST_GeomFromText(?, 4326))
		AND (rs.status IS NULL OR rs.status = 'active')
		ORDER BY r.ts DESC
		LIMIT ?
	`

	reportRows, err := s.db.Query(reportsQuery, areaWKT, n)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences and reports
	var reportSeqs []int
	var reports []models.ReportData
	for reportRows.Next() {
		var report models.ReportData
		var timestamp time.Time
		var x, y sql.NullFloat64
		var actionID sql.NullString

		err := reportRows.Scan(
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
		reportSeqs = append(reportSeqs, report.Seq)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	if len(reports) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))
	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}

	// Then, get all analyses for these reports
	analysesQuery := fmt.Sprintf(`
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description,
			ra.litter_probability, ra.hazard_probability, 
			ra.severity_level, ra.summary, ra.language, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq DESC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := s.db.Query(analysesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	// Group analyses by report sequence
	analysesBySeq := make(map[int][]models.ReportAnalysis)
	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		var analysisCreatedAt time.Time
		var analysisImage sql.NullString // Handle nullable analysis_image field

		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysisCreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}

		// Convert timestamp to string
		analysis.CreatedAt = analysisCreatedAt.Format(time.RFC3339)

		analysesBySeq[analysis.Seq] = append(analysesBySeq[analysis.Seq], analysis)
	}

	if err = analysisRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analyses: %w", err)
	}

	// Combine reports with their analyses
	var result []models.ReportWithAnalysis
	for _, report := range reports {
		analyses := analysesBySeq[report.Seq]
		if len(analyses) == 0 {
			// Skip reports without analyses
			continue
		}

		result = append(result, models.ReportWithAnalysis{
			Report:   report,
			Analysis: analyses,
		})
	}

	return result, nil
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

	// Calculate mean and max reports count across all areas
	// First, get all report counts for each area
	var reportCounts []int
	var totalCount int
	var maxCount int
	for _, area := range areas {
		areaWKT, err := s.wktConverter.ConvertGeoJSONToWKT(area.Area)
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
		totalCount += count
		if count > maxCount {
			maxCount = count
		}
	}

	// Calculate mean from the collected counts
	var meanCount float64
	if len(reportCounts) > 0 {
		meanCount = float64(totalCount) / float64(len(reportCounts))
	}

	// Get aggregated data for each area
	var areasData []models.AreaAggrData
	for _, area := range areas {
		areaWKT, err := s.wktConverter.ConvertGeoJSONToWKT(area.Area)
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
			LEFT JOIN report_status rs ON r.seq = rs.seq
			WHERE ST_Within(rg.geom, ST_GeomFromText(?, 4326)) AND ra.language = 'en'
			AND (rs.status IS NULL OR rs.status = 'active')
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
		areaData.ReportsMean = meanCount
		areaData.ReportsMax = maxCount

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
