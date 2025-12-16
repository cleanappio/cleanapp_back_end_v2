package handlers

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"report-listener/database"
	"report-listener/models"
	"report-listener/rabbitmq"
	brandutil "report-listener/utils"
	ws "report-listener/websocket"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
)

const (
	// MaxReportsLimit is the maximum number of reports that can be requested in a single query
	MaxReportsLimit = 100000
)

// Handlers contains all HTTP handlers
type Handlers struct {
	hub               *ws.Hub
	db                *database.Database
	rabbitmqPublisher *rabbitmq.Publisher
	rabbitmqReplier   *rabbitmq.Publisher
}

// NewHandlers creates a new handlers instance
func NewHandlers(hub *ws.Hub, db *database.Database, pub *rabbitmq.Publisher, replyPub *rabbitmq.Publisher) *Handlers {
	return &Handlers{
		hub:               hub,
		db:                db,
		rabbitmqPublisher: pub,
		rabbitmqReplier:   replyPub,
	}
}

// DB exposes the underlying database handle for wiring
func (h *Handlers) Db() *database.Database {
	return h.db
}

// BulkIngestRequest is the payload schema for bulk ingest
type BulkIngestRequest struct {
	Source string `json:"source"`
	Items  []struct {
		ExternalID  string                 `json:"external_id"`
		Title       string                 `json:"title"`
		Content     string                 `json:"content"`
		URL         string                 `json:"url"`
		CreatedAt   string                 `json:"created_at"`
		UpdatedAt   string                 `json:"updated_at"`
		Score       float64                `json:"score"`
		Tags        []string               `json:"tags"`
		Metadata    map[string]interface{} `json:"metadata"`
		SkipAI      *bool                  `json:"skip_ai"`
		ImageBase64 string                 `json:"image_base64,omitempty"`
	} `json:"items"`
}

// BulkIngestResponse contains per-batch stats
type BulkIngestResponse struct {
	Inserted int         `json:"inserted"`
	Updated  int         `json:"updated"`
	Skipped  int         `json:"skipped"`
	Errors   []BulkError `json:"errors"`
}

// TwitterReplyEvent is sent to RabbitMQ to trigger X reply for twitter-sourced reports
type TwitterReplyEvent struct {
	Seq            int    `json:"seq"`
	TweetID        string `json:"tweet_id"`
	Classification string `json:"classification"`
}

type BulkError struct {
	Index  int    `json:"i"`
	Reason string `json:"reason"`
}

// BulkIngest handles POST /api/v3/reports/bulk_ingest
func (h *Handlers) BulkIngest(c *gin.Context) {
	start := time.Now()

	var req BulkIngestRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.Source == "" || len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source and items required"})
		return
	}

	fetcherID := c.GetString("fetcher_id")
	if fetcherID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if len(req.Items) > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items limit 1000"})
		return
	}

	// Determine if we should skip side-effects (RabbitMQ publishing) for throughput
	// This is opt-in via metadata, not hardcoded by source name
	fastPath := false
	skipAIReview := false
	for _, it := range req.Items {
		if it.Metadata != nil {
			if v, ok := it.Metadata["skip_side_effects"].(bool); ok && v {
				fastPath = true
			}
			if v, ok := it.Metadata["bulk_mode"].(bool); ok && v {
				fastPath = true
				skipAIReview = true
			}
			if v, ok := it.Metadata["needs_ai_review"].(bool); ok && v {
				skipAIReview = true
			}
		}
	}

	log.Printf("bulk_ingest: starting source=%s items=%d fastPath=%t skipAIReview=%t", req.Source, len(req.Items), fastPath, skipAIReview)

	resp := BulkIngestResponse{}

	seenExt := make(map[string]bool)
	var uniqueExt []string
	for i, it := range req.Items {
		if strings.TrimSpace(it.ExternalID) == "" {
			resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "missing external_id"})
			resp.Skipped++
			continue
		}
		if seenExt[it.ExternalID] {
			resp.Skipped++
			continue
		}
		seenExt[it.ExternalID] = true
		uniqueExt = append(uniqueExt, it.ExternalID)
	}

	existing := make(map[string]int)
	const lookupChunk = 200
	for i := 0; i < len(uniqueExt); i += lookupChunk {
		end := i + lookupChunk
		if end > len(uniqueExt) {
			end = len(uniqueExt)
		}
		chunk := uniqueExt[i:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]interface{}, 0, len(chunk)+1)
		args = append(args, req.Source)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := h.db.DB().QueryContext(c.Request.Context(),
			fmt.Sprintf("SELECT external_id, seq FROM external_ingest_index WHERE source = ? AND external_id IN (%s)", placeholders),
			args...,
		)
		if err != nil {
			log.Printf("bulk ingest lookup failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		for rows.Next() {
			var ext string
			var seq int
			if err := rows.Scan(&ext, &seq); err == nil {
				existing[ext] = seq
			}
		}
		rows.Close()
	}

	type preparedItem struct {
		idx            int
		seq            int
		ext            string
		team           int
		lat            float64
		lon            float64
		img            []byte
		title          string
		description    string
		url            string
		brandName      string
		brandDisplay   string
		lp             float64
		hp             float64
		dbp            float64
		severity       float64
		classification string
		lang           string
		summary        string
		inferredEmails interface{}
		needsAIReview  bool
	}

	var newItems []preparedItem
	seenNew := make(map[string]bool)
	for i, it := range req.Items {
		if strings.TrimSpace(it.ExternalID) == "" {
			continue
		}
		if _, ok := existing[it.ExternalID]; ok {
			resp.Skipped++
			continue
		}
		if seenNew[it.ExternalID] {
			resp.Skipped++
			continue
		}
		seenNew[it.ExternalID] = true

		var imgBytes []byte
		if strings.TrimSpace(it.ImageBase64) != "" {
			if b, err := base64.StdEncoding.DecodeString(it.ImageBase64); err == nil {
				imgBytes = b
			}
		}

		lat := 0.0
		lon := 0.0
		if it.Metadata != nil {
			if v, ok := it.Metadata["latitude"].(float64); ok {
				lat = v
			}
			if v, ok := it.Metadata["longitude"].(float64); ok {
				lon = v
			}
		}

		team := 1 + rand.Intn(2)
		title := truncateRunes(stripNonBMP(it.Title), 500)
		description := truncateRunes(stripNonBMP(it.Content), 16000)
		trimmedDesc := trimmedDescriptionForSummary(description)
		url := truncateRunes(stripNonBMP(it.URL), 500)
		brandDisplay := extractBrandDisplay(it.Title, it.Metadata)
		brandName := brandutil.NormalizeBrandName(brandDisplay)
		if strings.EqualFold(req.Source, "github_issue") {
			brandName = normalizeGithubBrandName(brandDisplay)
		}
		severity := clampSeverity(it.Score)
		lp := 0.0
		hp := 0.0
		dbp := 1.0
		if it.Metadata != nil {
			if v, ok := it.Metadata["litter_probability"].(float64); ok {
				lp = v
			}
			if v, ok := it.Metadata["hazard_probability"].(float64); ok {
				hp = v
			}
			if v, ok := it.Metadata["digital_bug_probability"].(float64); ok {
				dbp = v
			} else if v, ok := it.Metadata["classification"].(string); ok && v == "physical" {
				dbp = 0.0
			}
			if v, ok := it.Metadata["severity_level"].(float64); ok {
				severity = clampSeverity(v)
			}
		}
		classification := "digital"
		if v, ok := it.Metadata["classification"].(string); ok {
			if vv := strings.ToLower(strings.TrimSpace(v)); vv == "physical" || vv == "digital" {
				classification = vv
			}
		}
		lang := "en"
		if v, ok := it.Metadata["language"].(string); ok && v != "" {
			lang = v
		}
		summary := title + " : " + trimmedDesc + " : " + safeURL(it.URL)
		if s, ok := it.Metadata["summary"].(string); ok && s != "" {
			summary = s + " : " + safeURL(it.URL)
		}

		var inferred interface{}
		if it.Metadata != nil {
			if emails, ok := it.Metadata["inferred_contact_emails"]; ok {
				if arr, ok := emails.([]interface{}); ok {
					parts := make([]string, 0, len(arr))
					for _, e := range arr {
						if s, ok := e.(string); ok {
							parts = append(parts, s)
						}
					}
					if len(parts) > 0 {
						inferred = strings.Join(parts, ",")
					}
				} else if s, ok := emails.(string); ok {
					inferred = s
				}
			}
		}

		newItems = append(newItems, preparedItem{
			idx:            i,
			ext:            it.ExternalID,
			team:           team,
			lat:            lat,
			lon:            lon,
			img:            imgBytes,
			title:          title,
			description:    description,
			url:            url,
			brandName:      brandName,
			brandDisplay:   brandDisplay,
			lp:             lp,
			hp:             hp,
			dbp:            dbp,
			severity:       severity,
			classification: classification,
			lang:           lang,
			summary:        truncate(summary, 8192),
			inferredEmails: inferred,
			needsAIReview:  skipAIReview,
		})
	}

	if len(newItems) == 0 {
		log.Printf("bulk_ingest source=%s total=%d inserted=0 skipped=%d duration=%s", req.Source, len(req.Items), resp.Skipped, time.Since(start))
		c.JSON(http.StatusOK, resp)
		return
	}

	tx, err := h.db.DB().BeginTx(c.Request.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "begin tx failed"})
		return
	}

	const insertChunk = 200
	reporterID := "fetcher:" + fetcherID
	for i := 0; i < len(newItems); i += insertChunk {
		end := i + insertChunk
		if end > len(newItems) {
			end = len(newItems)
		}
		chunk := newItems[i:end]
		vals := make([]string, 0, len(chunk))
		args := make([]interface{}, 0, len(chunk)*9)
		for _, it := range chunk {
			vals = append(vals, "(?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args,
				reporterID,
				it.team,
				"",
				it.lat,
				it.lon,
				0.0,
				0.0,
				it.img,
				truncateRunes(stripNonBMP(it.title), 255),
			)
		}
		res, err := tx.ExecContext(c.Request.Context(),
			fmt.Sprintf("INSERT INTO reports (id, team, action_id, latitude, longitude, x, y, image, description) VALUES %s", strings.Join(vals, ",")),
			args...,
		)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "insert reports failed"})
			return
		}
		firstID, _ := res.LastInsertId()
		rows, _ := res.RowsAffected()
		for j := 0; j < int(rows); j++ {
			newItems[i+j].seq = int(firstID) + j
		}
	}

	var analysisVals []string
	var analysisArgs []interface{}
	for _, it := range newItems {
		analysisVals = append(analysisVals, "(?, ?, '', NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?, ?)")
		analysisArgs = append(analysisArgs,
			it.seq,
			req.Source,
			it.title,
			it.description,
			it.brandName,
			it.brandDisplay,
			it.lp,
			it.hp,
			it.dbp,
			it.severity,
			it.summary,
			it.lang,
			it.classification,
			nullable(it.inferredEmails),
			it.needsAIReview,
		)
	}
	if len(analysisVals) > 0 {
		if _, err := tx.ExecContext(c.Request.Context(),
			fmt.Sprintf("INSERT INTO report_analysis (seq, source, analysis_text, analysis_image, title, description, brand_name, brand_display_name, litter_probability, hazard_probability, digital_bug_probability, severity_level, summary, language, is_valid, classification, inferred_contact_emails, needs_ai_review) VALUES %s", strings.Join(analysisVals, ",")),
			analysisArgs...,
		); err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "insert analysis failed"})
			return
		}
	}

	var detailVals []string
	var detailArgs []interface{}
	for _, it := range newItems {
		company, product := splitOwnerRepo(it.brandDisplay)
		detailVals = append(detailVals, "(?, ?, ?, ?)")
		detailArgs = append(detailArgs, it.seq, nullable(company), nullable(product), it.url)
	}
	if len(detailVals) > 0 {
		if _, err := tx.ExecContext(c.Request.Context(),
			fmt.Sprintf("INSERT INTO report_details (seq, company_name, product_name, url) VALUES %s ON DUPLICATE KEY UPDATE company_name=VALUES(company_name), product_name=VALUES(product_name), url=VALUES(url)", strings.Join(detailVals, ",")),
			detailArgs...,
		); err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "insert details failed"})
			return
		}
	}

	var mapVals []string
	var mapArgs []interface{}
	for _, it := range newItems {
		mapVals = append(mapVals, "(?, ?, ?)")
		mapArgs = append(mapArgs, req.Source, it.ext, it.seq)
	}
	if len(mapVals) > 0 {
		if _, err := tx.ExecContext(c.Request.Context(),
			fmt.Sprintf("INSERT INTO external_ingest_index (source, external_id, seq) VALUES %s ON DUPLICATE KEY UPDATE seq = VALUES(seq), updated_at = NOW()", strings.Join(mapVals, ",")),
			mapArgs...,
		); err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert index failed"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}

	resp.Inserted = len(newItems)
	log.Printf("bulk_ingest source=%s total=%d inserted=%d skipped=%d duration=%s", req.Source, len(req.Items), resp.Inserted, resp.Skipped, time.Since(start))

	if !fastPath && h.rabbitmqPublisher != nil {
		for _, it := range newItems {
			publishForRenderer(c, h, it.seq)
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) ListenReports(c *gin.Context) {
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection to WebSocket: %v", err)
		return
	}

	// Create a new client
	client := ws.NewClient(h.hub, conn)

	// Register the client with the hub
	h.hub.Register <- client

	// Start the client's read and write pumps in goroutines
	go client.WritePump()
	go client.ReadPump()

	log.Printf("WebSocket connection established")
}

// HealthCheck returns the service health status
func (h *Handlers) HealthCheck(c *gin.Context) {
	connectedClients, lastBroadcastSeq := h.hub.GetStats()

	response := models.HealthResponse{
		Status:           "healthy",
		Service:          "report-listener",
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		ConnectedClients: connectedClients,
		LastBroadcastSeq: lastBroadcastSeq,
	}

	c.JSON(http.StatusOK, response)
}

// GetLastNAnalyzedReports returns the last N analyzed reports
func (h *Handlers) GetLastNAnalyzedReports(c *gin.Context) {
	// Get the limit parameter from query string, default to 10 if not provided
	limitStr := c.DefaultQuery("n", "10")
	classification := c.DefaultQuery("classification", "physical")

	limit := 10 // default value
	if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
		limit = parsedLimit
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum number of reports to prevent abuse
	if limit > MaxReportsLimit {
		limit = MaxReportsLimit
	}

	// Get the full_data parameter from query string, default to true if not provided
	fullDataStr := c.DefaultQuery("full_data", "true")
	fullData := true // default value
	if parsedFullData, err := strconv.ParseBool(fullDataStr); err == nil {
		fullData = parsedFullData
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'full_data' parameter. Must be 'true' or 'false'."})
		return
	}

	// Apply a stricter cap when full_data is requested to protect DB and payload size
	if fullData && limit > 50000 {
		limit = 50000
	}

	// Get the reports from the database
	reportsInterface, err := h.db.GetLastNAnalyzedReports(c.Request.Context(), limit, classification, fullData)
	if err != nil {
		log.Printf("Failed to get last N analyzed reports: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Handle different return types based on full_data parameter
	if fullData {
		// Type assertion to get the reports with analysis
		reportsWithAnalysis, ok := reportsInterface.([]models.ReportWithAnalysis)
		if !ok {
			log.Printf("Failed to type assert reports to []models.ReportWithAnalysis")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
			return
		}

		// Create the response in the same format as WebSocket broadcasts
		response := models.ReportBatch{
			Reports: reportsWithAnalysis,
			Count:   len(reportsWithAnalysis),
			FromSeq: 0,
			ToSeq:   0,
		}

		// Set FromSeq and ToSeq if there are reports
		if len(reportsWithAnalysis) > 0 {
			response.FromSeq = reportsWithAnalysis[0].Report.Seq
			response.ToSeq = reportsWithAnalysis[len(reportsWithAnalysis)-1].Report.Seq
		}

		c.JSON(http.StatusOK, response)
	} else {
		// Type assertion to get reports with minimal analysis
		reportsWithMinimalAnalysis, ok := reportsInterface.([]models.ReportWithMinimalAnalysis)
		if !ok {
			log.Printf("Failed to type assert reports to []models.ReportWithMinimalAnalysis")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
			return
		}

		// Create a custom response structure for minimal analysis to maintain consistency
		// but with the minimal data structure
		response := gin.H{
			"reports":  reportsWithMinimalAnalysis,
			"count":    len(reportsWithMinimalAnalysis),
			"from_seq": 0,
			"to_seq":   0,
		}

		// Set FromSeq and ToSeq if there are reports
		if len(reportsWithMinimalAnalysis) > 0 {
			response["from_seq"] = reportsWithMinimalAnalysis[0].Report.Seq
			response["to_seq"] = reportsWithMinimalAnalysis[len(reportsWithMinimalAnalysis)-1].Report.Seq
		}

		c.JSON(http.StatusOK, response)
	}
}

// GetSearchReports searches reports using FULLTEXT search
func (h *Handlers) GetSearchReports(c *gin.Context) {
	// Get the search query parameter (required)
	searchQuery := c.Query("q")
	if strings.TrimSpace(searchQuery) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or empty 'q' parameter. Search query is required."})
		return
	}

	// Get the classification parameter (optional - empty string if not provided)
	classification := c.Query("classification")

	// Get the full_data parameter from query string, default to true if not provided
	fullDataStr := c.DefaultQuery("full_data", "true")
	fullData := true // default value
	if parsedFullData, err := strconv.ParseBool(fullDataStr); err == nil {
		fullData = parsedFullData
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'full_data' parameter. Must be 'true' or 'false'."})
		return
	}

	// Transform search query: replace "-" with "+" and add "+" before each word for boolean mode
	// First, replace any minus signs with plus signs
	searchQuery = strings.ReplaceAll(searchQuery, "-", "+")

	words := strings.Fields(strings.TrimSpace(searchQuery))
	transformedWords := make([]string, 0, len(words))
	for _, word := range words {
		if word != "" {
			// Add "+" prefix if the word doesn't already start with "+"
			if !strings.HasPrefix(word, "+") {
				transformedWords = append(transformedWords, "+"+word)
			} else {
				transformedWords = append(transformedWords, word)
			}
		}
	}
	transformedQuery := strings.Join(transformedWords, " ")

	// Ensure we have a valid transformed query
	if transformedQuery == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'q' parameter. Search query must contain at least one non-empty word."})
		return
	}

	// Get the reports from the database
	reportsInterface, err := h.db.SearchReports(c.Request.Context(), transformedQuery, classification, fullData)
	if err != nil {
		log.Printf("Failed to search reports: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Handle different return types based on full_data parameter
	if fullData {
		// Type assertion to get the reports with analysis
		reportsWithAnalysis, ok := reportsInterface.([]models.ReportWithAnalysis)
		if !ok {
			log.Printf("Failed to type assert reports to []models.ReportWithAnalysis")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
			return
		}

		// Create the response in the same format as WebSocket broadcasts
		response := models.ReportBatch{
			Reports: reportsWithAnalysis,
			Count:   len(reportsWithAnalysis),
			FromSeq: 0,
			ToSeq:   0,
		}

		// Set FromSeq and ToSeq if there are reports
		if len(reportsWithAnalysis) > 0 {
			response.FromSeq = reportsWithAnalysis[0].Report.Seq
			response.ToSeq = reportsWithAnalysis[len(reportsWithAnalysis)-1].Report.Seq
		}

		c.JSON(http.StatusOK, response)
	} else {
		// Type assertion to get reports with minimal analysis
		reportsWithMinimalAnalysis, ok := reportsInterface.([]models.ReportWithMinimalAnalysis)
		if !ok {
			log.Printf("Failed to type assert reports to []models.ReportWithMinimalAnalysis")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
			return
		}

		// Create a custom response structure for minimal analysis to maintain consistency
		// but with the minimal data structure
		response := gin.H{
			"reports":  reportsWithMinimalAnalysis,
			"count":    len(reportsWithMinimalAnalysis),
			"from_seq": 0,
			"to_seq":   0,
		}

		// Set FromSeq and ToSeq if there are reports
		if len(reportsWithMinimalAnalysis) > 0 {
			response["from_seq"] = reportsWithMinimalAnalysis[0].Report.Seq
			response["to_seq"] = reportsWithMinimalAnalysis[len(reportsWithMinimalAnalysis)-1].Report.Seq
		}

		c.JSON(http.StatusOK, response)
	}
}

// GetReportBySeq returns a specific report by sequence ID
func (h *Handlers) GetReportBySeq(c *gin.Context) {
	// Get the seq parameter from query string
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'seq' parameter"})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'seq' parameter. Must be a positive integer."})
		return
	}

	// Get the report from the database
	reportWithAnalysis, err := h.db.GetReportBySeq(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get report by seq %d: %v", seq, err)
		if err.Error() == fmt.Sprintf("report with seq %d not found", seq) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve report"})
		return
	}

	c.JSON(http.StatusOK, reportWithAnalysis)
}

// GetLastNReportsByID returns the last N reports for a given report ID
func (h *Handlers) GetLastNReportsByID(c *gin.Context) {
	// Get the id parameter from query string
	reportID := c.Query("id")
	if reportID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'id' parameter"})
		return
	}

	// Get the reports from the database
	reports, err := h.db.GetLastNReportsByID(c.Request.Context(), reportID)
	if err != nil {
		log.Printf("Failed to get last N reports by ID %s: %v", reportID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

// GetReportsByLatLng returns reports within a specified radius around given coordinates
func (h *Handlers) GetReportsByLatLng(c *gin.Context) {
	// Get the latitude parameter from query string
	latStr := c.Query("latitude")
	if latStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'latitude' parameter"})
		return
	}

	// Get the longitude parameter from query string
	lngStr := c.Query("longitude")
	if lngStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'longitude' parameter"})
		return
	}

	// Get the radius_km parameter from query string, default to 10 if not provided
	radiusKmStr := c.DefaultQuery("radius_km", "1.0")

	// Get the n parameter from query string, default to 10 if not provided
	nStr := c.DefaultQuery("n", "10")

	n := 10 // default value
	if parsedN, err := strconv.Atoi(nStr); err == nil && parsedN > 0 {
		n = parsedN
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Parse latitude
	latitude, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'latitude' parameter. Must be a valid number."})
		return
	}

	// Parse longitude
	longitude, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'longitude' parameter. Must be a valid number."})
		return
	}

	// Parse radius_km
	radiusKm := 1.0 // default value
	if parsedRadius, err := strconv.ParseFloat(radiusKmStr, 32); err == nil && parsedRadius > 0 {
		radiusKm = parsedRadius
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'radius_km' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum radius to prevent abuse (e.g., 100 km)
	if radiusKm > 100 {
		radiusKm = 100
	}

	// Get the reports from the database
	reports, err := h.db.GetReportsByLatLng(c.Request.Context(), latitude, longitude, radiusKm, n)
	if err != nil {
		log.Printf("Failed to get reports by lat/lng (%.6f, %.6f, radius: %fkm): %v", latitude, longitude, radiusKm, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

// GetReportsByLatLng returns reports within a specified radius around given coordinates
func (h *Handlers) GetReportsByLatLngLite(c *gin.Context) {
	// Get the latitude parameter from query string
	latStr := c.Query("latitude")
	if latStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'latitude' parameter"})
		return
	}

	// Get the longitude parameter from query string
	lngStr := c.Query("longitude")
	if lngStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'longitude' parameter"})
		return
	}

	// Get the radius_km parameter from query string, default to 10 if not provided
	radiusKmStr := c.DefaultQuery("radius_km", "1.0")

	// Get the n parameter from query string, default to 10 if not provided
	nStr := c.DefaultQuery("n", "10")

	n := 10 // default value
	if parsedN, err := strconv.Atoi(nStr); err == nil && parsedN > 0 {
		n = parsedN
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Parse latitude
	latitude, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'latitude' parameter. Must be a valid number."})
		return
	}

	// Parse longitude
	longitude, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'longitude' parameter. Must be a valid number."})
		return
	}

	// Parse radius_km
	radiusKm := 1.0 // default value
	if parsedRadius, err := strconv.ParseFloat(radiusKmStr, 32); err == nil && parsedRadius > 0 {
		radiusKm = parsedRadius
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'radius_km' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum radius to prevent abuse (e.g., 100 km)
	if radiusKm > 100 {
		radiusKm = 100
	}

	// Get the reports from the database
	reports, err := h.db.GetReportsByLatLngLite(c.Request.Context(), latitude, longitude, radiusKm, n)
	if err != nil {
		log.Printf("Failed to get reports by lat/lng (%.6f, %.6f, radius: %fkm): %v", latitude, longitude, radiusKm, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handlers) GetReportsByBrand(c *gin.Context) {
	// Get the brand name parameter from query string
	brandName := c.Query("brand_name")
	if brandName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'brand_name' parameter"})
		return
	}

	// Get the limit parameter from query string, default to 10 if not provided
	limitStr := c.DefaultQuery("n", "10")

	limit := 10 // default value
	if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
		limit = parsedLimit
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum number of reports to prevent abuse
	if limit > MaxReportsLimit {
		limit = MaxReportsLimit
	}

	// Get the reports from the database
	reports, err := h.db.GetReportsByBrandName(c.Request.Context(), brandName, limit)
	if err != nil {
		log.Printf("Failed to get reports by brand '%s': %v", brandName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

// GetImageBySeq returns a base64 encoded image for a specific report by sequence number
func (h *Handlers) GetImageBySeq(c *gin.Context) {
	// Get the seq parameter from query string
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing seq parameter"})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seq parameter. Must be a positive integer."})
		return
	}

	// Get the image from the database
	imageData, err := h.db.GetImageBySeq(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get image for report seq %d: %v", seq, err)
		if err.Error() == fmt.Sprintf("report with seq %d not found", seq) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve image"})
		}
		return
	}

	// Encode image as base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// Return the base64 encoded image
	c.JSON(http.StatusOK, gin.H{
		"image": base64Image,
	})
}

// GetRawImageBySeq returns the raw image data for a specific report by sequence number
func (h *Handlers) GetRawImageBySeq(c *gin.Context) {
	// Get the seq parameter from query string
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing seq parameter"})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seq parameter. Must be a positive integer."})
		return
	}

	// Get the image from the database
	imageData, err := h.db.GetImageBySeq(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get image for report seq %d: %v", seq, err)
		if err.Error() == fmt.Sprintf("report with seq %d not found", seq) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve image"})
		}
		return
	}

	// Detect image type from the data
	contentType := "image/jpeg" // default
	if len(imageData) > 4 {
		// Check for common image format signatures
		if imageData[0] == 0x89 && imageData[1] == 0x50 && imageData[2] == 0x4E && imageData[3] == 0x47 {
			contentType = "image/png"
		} else if imageData[0] == 0x47 && imageData[1] == 0x49 && imageData[2] == 0x46 {
			contentType = "image/gif"
		} else if imageData[0] == 0x42 && imageData[1] == 0x4D {
			contentType = "image/bmp"
		} else if imageData[0] == 0xFF && imageData[1] == 0xD8 {
			contentType = "image/jpeg"
		}
	}

	// Set the appropriate Content-Type header for the image
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.Itoa(len(imageData)))

	// Return the raw image data
	c.Data(http.StatusOK, contentType, imageData)
}

// Helper functions for bulk_ingest

// truncateRunes truncates a string to a maximum number of runes
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return s
}

// stripNonBMP removes non-BMP (4-byte UTF-8) characters that can cause MySQL issues
func stripNonBMP(s string) string {
	var result strings.Builder
	for _, r := range s {
		if r <= 0xFFFF {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// truncate truncates a string to a maximum number of bytes
func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes]
}

// trimmedDescriptionForSummary returns a shorter version of the description for summary
func trimmedDescriptionForSummary(desc string) string {
	const maxLen = 500
	if len(desc) <= maxLen {
		return desc
	}
	return desc[:maxLen] + "..."
}

// extractBrandDisplay extracts brand display name from title or metadata
func extractBrandDisplay(title string, metadata map[string]interface{}) string {
	if metadata != nil {
		if brand, ok := metadata["brand"].(string); ok && brand != "" {
			return brand
		}
		if brand, ok := metadata["brand_name"].(string); ok && brand != "" {
			return brand
		}
		if brand, ok := metadata["brand_display_name"].(string); ok && brand != "" {
			return brand
		}
	}
	// Extract from title (first word or GitHub owner/repo format)
	parts := strings.Fields(title)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// clampSeverity clamps severity to valid range 0-10
func clampSeverity(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 10 {
		return 10
	}
	return score
}

// normalizeGithubBrandName normalizes GitHub owner/repo to brand name
func normalizeGithubBrandName(brandDisplay string) string {
	// For GitHub, use owner/repo as brand name directly
	return strings.ToLower(strings.TrimSpace(brandDisplay))
}

// safeURL returns the URL or empty string if too long
func safeURL(url string) string {
	if len(url) > 500 {
		return url[:500]
	}
	return url
}

// nullable returns nil for empty strings, otherwise the value
func nullable(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	return v
}

// splitOwnerRepo splits a GitHub owner/repo string into company and product
func splitOwnerRepo(brandDisplay string) (string, string) {
	parts := strings.SplitN(brandDisplay, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return brandDisplay, ""
}

// publishForRenderer publishes a report to RabbitMQ for rendering
func publishForRenderer(c *gin.Context, h *Handlers, seq int) {
	if h.rabbitmqPublisher == nil {
		return
	}
	// Best effort publish - don't block on errors
	go func() {
		if err := h.rabbitmqPublisher.Publish(c.Request.Context(), seq); err != nil {
			log.Printf("warn: failed to publish seq %d for rendering: %v", seq, err)
		}
	}()
}

