package services

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"custom-area-dashboard/config"
	"custom-area-dashboard/models"

	_ "github.com/go-sql-driver/mysql"
)

// DatabaseService manages database connections and queries
type DatabaseService struct {
	db           *sql.DB
	cfg          *config.Config
}

// NewDatabaseService creates a new database service
func NewDatabaseService(cfg *config.Config) (*DatabaseService, error) {
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

	return &DatabaseService{db: db, cfg: cfg}, nil
}

// Close closes the database connection
func (s *DatabaseService) Close() error {
	return s.db.Close()
}

// GetReportsByCustomArea gets the last n reports with analysis that are contained within the custom area defined in config
func (s *DatabaseService) GetReportsByCustomArea(n int) ([]models.ReportWithAnalysis, error) {
	// Fetch the geometry for the cfg.CustomAreaID
	geometryQuery := `
		SELECT ST_AsText(geom)
		FROM area_index
		WHERE area_id = ?
	`

	var geometry string
	err := s.db.QueryRow(geometryQuery, s.cfg.CustomAreaID).Scan(&geometry)
	if err != nil {
		return nil, fmt.Errorf("failed to query geometry: %w", err)
	}

	// Get all reports within the area using area_index table
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

	reportRows, err := s.db.Query(reportsQuery, geometry, n)
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

// GetReportsAggregatedData returns aggregated reports data for all areas of the configured sub admin level
func (s *DatabaseService) GetReportsAggregatedData() ([]models.AreaAggrData, error) {
	if len(s.cfg.CustomAreaSubIDs) == 0 {
		return []models.AreaAggrData{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(s.cfg.CustomAreaSubIDs))
	args := make([]interface{}, len(s.cfg.CustomAreaSubIDs))
	for i, areaID := range s.cfg.CustomAreaSubIDs {
		placeholders[i] = "?"
		args[i] = areaID
	}

	// Get geometries for area IDs
	geometriesQuery := fmt.Sprintf(`
		SELECT a.id, a.name, ST_AsText(ai.geom)
		FROM areas a
		JOIN area_index ai ON a.id = ai.area_id
		WHERE a.id IN (%s)
	`, strings.Join(placeholders, ","))
	
	rows, err := s.db.Query(geometriesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query geometries: %w", err)
	}
	defer rows.Close()
	
	var geometries []string
	var areasData []models.AreaAggrData
	for rows.Next() {
		var areaData models.AreaAggrData
		var geometry string
		err := rows.Scan(&areaData.AreaID, &areaData.Name, &geometry)
		if err != nil {
			return nil, fmt.Errorf("failed to scan geometry: %w", err)
		}
		areasData = append(areasData, areaData)
		geometries = append(geometries, geometry)
	}

	// Collect all data and calculate statistics
	var reportCounts []int
	var totalCount int
	var maxCount int
	for i, geometry := range geometries {
		// Get aggregated data for all areas using area_index table
		query := `
			SELECT 
				COUNT(CASE WHEN rs.status IS NULL OR rs.status = 'active' THEN r.seq END) as reports_count,
				COALESCE(AVG(CASE WHEN rs.status IS NULL OR rs.status = 'active' THEN ra.severity_level END), 0.0) as mean_severity,
				COALESCE(AVG(CASE WHEN rs.status IS NULL OR rs.status = 'active' THEN ra.litter_probability END), 0.0) as mean_litter_probability,
				COALESCE(AVG(CASE WHEN rs.status IS NULL OR rs.status = 'active' THEN ra.hazard_probability END), 0.0) as mean_hazard_probability
			FROM reports r
			LEFT JOIN reports_geometry rg ON r.seq = rg.seq
			LEFT JOIN report_analysis ra ON r.seq = ra.seq AND ra.language = 'en'
			LEFT JOIN report_status rs ON r.seq = rs.seq
			WHERE ST_Within(rg.geom, ST_GeomFromText(?, 4326))
		`

		err = s.db.QueryRow(query, geometry).Scan(
			&areasData[i].ReportsCount,
			&areasData[i].MeanSeverity,
			&areasData[i].MeanLitterProbability,
			&areasData[i].MeanHazardProbability,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to query aggregated data: %w", err)
		}

		reportCounts = append(reportCounts, areasData[i].ReportsCount)
		totalCount += areasData[i].ReportsCount
		if areasData[i].ReportsCount > maxCount {
			maxCount = areasData[i].ReportsCount
		}
	}

	// Calculate mean from the collected counts
	var meanCount float64
	if len(reportCounts) > 0 {
		meanCount = float64(totalCount) / float64(len(reportCounts))
	}

	// Set mean and max values for all areas
	for i := range areasData {
		areasData[i].ReportsMean = meanCount
		areasData[i].ReportsMax = maxCount
	}

	return areasData, nil
}

// GetAreasByIds fetches areas from the database by their IDs
func (s *DatabaseService) GetAreasByIds(areaIDs []int64) ([]models.CustomArea, error) {
	if len(areaIDs) == 0 {
		return []models.CustomArea{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(areaIDs))
	args := make([]any, len(areaIDs))
	for i, areaID := range areaIDs {
		placeholders[i] = "?"
		args[i] = areaID
	}

	// Query to get areas by IDs
	query := fmt.Sprintf(`
		SELECT id, name, area_json
		FROM areas
		WHERE id IN (%s)
		ORDER BY id
	`, strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query areas by IDs: %w", err)
	}
	defer rows.Close()

	var areas []models.CustomArea
	for rows.Next() {
		var area models.CustomArea
		var id int64
		var areaJson string

		err := rows.Scan(&id, &area.Name, &areaJson)
		if err != nil {
			log.Printf("Warning: failed to scan area: %v", err)
			continue
		}

		area.AreaID = id
		area.Area = []byte(areaJson)

		areas = append(areas, area)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating areas: %w", err)
	}

	return areas, nil
}

// getEnvOrDefault gets an environment variable or returns a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
