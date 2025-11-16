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

	// Simple limits
	if len(req.Items) > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items limit 1000"})
		return
	}

	resp := BulkIngestResponse{}

	for i, it := range req.Items {
		if it.ExternalID == "" {
			resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "missing external_id"})
			resp.Skipped++
			continue
		}

		// Idempotency: check mapping
		seq, exists, err := h.db.GetSeqByExternal(c.Request.Context(), req.Source, it.ExternalID)
		if err != nil {
			resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "db error"})
			continue
		}

		// Decode optional base64 image (best-effort)
		var imgBytes []byte
		if strings.TrimSpace(it.ImageBase64) != "" {
			if b, err := base64.StdEncoding.DecodeString(it.ImageBase64); err == nil {
				imgBytes = b
			} else {
				// ignore invalid image, proceed without image
				imgBytes = []byte{}
			}
		} else {
			imgBytes = []byte{}
		}

		if !exists {
			// Insert report + analysis + mapping in one transaction to avoid orphan rows
			tx, err := h.db.DB().BeginTx(c.Request.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "begin tx failed"})
				continue
			}
			reporterID := "fetcher:" + fetcherID
			// Insert report
			// randomize team between 1 and 2
			team := 1 + rand.Intn(2)
			// Optional coordinates from metadata
			lat := 0.0
			if it.Metadata != nil {
				if v, ok := it.Metadata["latitude"].(float64); ok {
					lat = v
				}
			}
			lon := 0.0
			if it.Metadata != nil {
				if v, ok := it.Metadata["longitude"].(float64); ok {
					lon = v
				}
			}

			res, err := tx.ExecContext(c.Request.Context(),
				`INSERT INTO reports (id, team, action_id, latitude, longitude, x, y, image, description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				reporterID, team, "", lat, lon, 0.0, 0.0, imgBytes, truncateRunes(stripNonBMP(it.Title), 255),
			)
			if err != nil {
				tx.Rollback()
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("insert report failed: %v", err), 256)})
				continue
			}
			lastID, err := res.LastInsertId()
			if err != nil {
				tx.Rollback()
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("get last insert id failed: %v", err), 256)})
				continue
			}
			seq = int(lastID)

			// Prepare analysis fields per spec
			title := truncateRunes(stripNonBMP(it.Title), 500)
			description := truncateRunes(stripNonBMP(it.Content), 16000)
			// Optional tags processing: insert into tags/report_tags
			if len(it.Tags) > 0 {
				for _, raw := range it.Tags {
					tagDisp := strings.TrimSpace(strings.TrimPrefix(raw, "#"))
					if tagDisp == "" {
						continue
					}
					tagCanon := strings.ToLower(tagDisp)
					// Upsert tag and get id
					resTag, err := tx.ExecContext(c.Request.Context(),
						`INSERT INTO tags (canonical_name, display_name, usage_count) VALUES (?, ?, 0)
                         ON DUPLICATE KEY UPDATE display_name=VALUES(display_name), id=LAST_INSERT_ID(id)`,
						tagCanon, tagDisp,
					)
					if err != nil {
						log.Printf("tag upsert failed: %v", err)
						continue
					}
					tagID, _ := resTag.LastInsertId()
					if tagID == 0 {
						// Fallback: read id by canonical
						row := tx.QueryRowContext(c.Request.Context(), `SELECT id FROM tags WHERE canonical_name = ?`, tagCanon)
						_ = row.Scan(&tagID)
					}
					if tagID > 0 {
						_, _ = tx.ExecContext(c.Request.Context(),
							`INSERT IGNORE INTO report_tags (report_seq, tag_id) VALUES (?, ?)`,
							seq, tagID,
						)
						_, _ = tx.ExecContext(c.Request.Context(),
							`UPDATE tags SET usage_count = usage_count + 1, last_used_at = NOW() WHERE id = ?`,
							tagID,
						)
					}
				}
			}
			// Brand fields:
			// - For twitter: keep empty unless explicitly provided in metadata
			// - For github_issue: derive brand from owner/repo
			// - Otherwise: derive using generic extractor
			var brandDisplay string
			var brandName string
			if strings.EqualFold(req.Source, "twitter") {
				if it.Metadata != nil {
					if v, ok := it.Metadata["brand_display_name"].(string); ok && strings.TrimSpace(v) != "" {
						brandDisplay = v
					}
					if v, ok := it.Metadata["brand_name"].(string); ok && strings.TrimSpace(v) != "" {
						brandName = v
					}
				}
			} else if strings.EqualFold(req.Source, "github_issue") {
				brandDisplay = extractBrandDisplay(it.Title, it.Metadata)
				brandName = normalizeGithubBrandName(brandDisplay)
			} else {
				brandDisplay = extractBrandDisplay(it.Title, it.Metadata)
				brandName = brandutil.NormalizeBrandName(brandDisplay)
			}
			severity := clampSeverity(it.Score)
			// summary composed below; no standalone sumSafe needed
			// Compose trimmed description for summary
			trimmedDesc := trimmedDescriptionForSummary(description)
			// Insert analysis (truncate to avoid oversized multi-byte issues)
			// Extract optional analysis fields from metadata if provided by submitter
			lp := 0.0
			if v, ok := it.Metadata["litter_probability"].(float64); ok {
				lp = v
			}
			hp := 0.0
			if v, ok := it.Metadata["hazard_probability"].(float64); ok {
				hp = v
			}
			dbp := 1.0
			if v, ok := it.Metadata["digital_bug_probability"].(float64); ok {
				dbp = v
			} else if v, ok := it.Metadata["classification"].(string); ok && v == "physical" {
				dbp = 0.0
			}
			// severity from Score (already clamped), but allow override via metadata.severity_level
			if v, ok := it.Metadata["severity_level"].(float64); ok {
				severity = clampSeverity(v)
			}
			// classification override (normalize to allowed enum values)
			classification := "digital"
			if v, ok := it.Metadata["classification"].(string); ok && v != "" {
				vv := strings.ToLower(strings.TrimSpace(v))
				if vv == "physical" || vv == "digital" {
					classification = vv
				}
			}
			// language optional
			lang := "en"
			if v, ok := it.Metadata["language"].(string); ok && v != "" {
				lang = v
			}
			// summary: prefer metadata.summary, fall back to composed summary
			metaSummary, _ := it.Metadata["summary"].(string)
			if metaSummary == "" {
				metaSummary = title + " : " + trimmedDesc + " : " + safeURL(it.URL)
			} else {
				metaSummary = truncate(trimmedDesc, 0) // noop to keep truncate function referenced
				metaSummary = ""                       // reset; maintain below explicit string build
			}
			effectiveSummary := it.Metadata["summary"]
			if s, ok := effectiveSummary.(string); ok && s != "" {
				// indexer summary + tweet URL
				metaSummary = s + " : " + safeURL(it.URL)
			} else {
				metaSummary = title + " : " + trimmedDesc + " : " + safeURL(it.URL)
			}

			_, err = tx.ExecContext(c.Request.Context(), `
                INSERT INTO report_analysis (
                    seq, source, analysis_text, analysis_image, title, description,
                    brand_name, brand_display_name, litter_probability, hazard_probability,
                    digital_bug_probability, severity_level, summary, language, is_valid, classification
                ) VALUES (?, ?, '', NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?)
            `, seq, req.Source, title, description, brandName, brandDisplay, lp, hp, dbp, severity, truncate(metaSummary, 8192), lang, classification)
			if err != nil {
				tx.Rollback()
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("insert analysis failed: %v", err), 256)})
				continue
			}

			// Insert report_details (company_name, product_name, url)
			company, product := splitOwnerRepo(brandDisplay)
			_, err = tx.ExecContext(c.Request.Context(), `
                INSERT INTO report_details (seq, company_name, product_name, url) VALUES (?, ?, ?, ?)
                ON DUPLICATE KEY UPDATE company_name=VALUES(company_name), product_name=VALUES(product_name), url=VALUES(url)
            `, seq, nullable(company), nullable(product), truncateRunes(stripNonBMP(it.URL), 500))
			if err != nil {
				tx.Rollback()
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("insert details failed: %v", err), 256)})
				continue
			}

			// Insert mapping
			_, err = tx.ExecContext(c.Request.Context(), `
                INSERT INTO external_ingest_index (source, external_id, seq) VALUES (?, ?, ?)
                ON DUPLICATE KEY UPDATE seq = VALUES(seq), updated_at = NOW()`,
				req.Source, it.ExternalID, seq,
			)
			if err != nil {
				tx.Rollback()
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("upsert mapping failed: %v", err), 256)})
				continue
			}
			if err := tx.Commit(); err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("commit failed: %v", err), 256)})
				continue
			}
			resp.Inserted++

			// Publish event for twitter replier on new twitter-sourced reports
			if h.rabbitmqReplier != nil && strings.EqualFold(req.Source, "twitter") {
				tweetID := it.ExternalID
				if tweetID != "" {
					classStr := ""
					if it.Metadata != nil {
						if v, ok := it.Metadata["classification"]; ok {
							if s, ok2 := v.(string); ok2 {
								classStr = strings.ToLower(s)
							}
						}
					}
					if classStr == "" {
						for _, t := range it.Tags {
							ts := strings.ToLower(t)
							if ts == "physical" || ts == "digital" {
								classStr = ts
								break
							}
						}
					}
					if classStr == "physical" || classStr == "digital" {
						_ = h.rabbitmqReplier.Publish(TwitterReplyEvent{
							Seq:            int(seq),
							TweetID:        tweetID,
							Classification: classStr,
						})
					}
				}
			}

			// Publish to RabbitMQ for fast renderer
			if h.rabbitmqPublisher != nil {
				publishForRenderer(c, h, seq)
			}
		} else {
			// Optional update of analysis if changed
			title := truncateRunes(stripNonBMP(it.Title), 500)
			description := truncateRunes(stripNonBMP(it.Content), 16000)
			// Optional tags processing on update
			if len(it.Tags) > 0 {
				for _, raw := range it.Tags {
					tagDisp := strings.TrimSpace(strings.TrimPrefix(raw, "#"))
					if tagDisp == "" {
						continue
					}
					tagCanon := strings.ToLower(tagDisp)
					// Upsert tag and get id
					resTag, err := h.db.DB().ExecContext(c.Request.Context(),
						`INSERT INTO tags (canonical_name, display_name, usage_count) VALUES (?, ?, 0)
                         ON DUPLICATE KEY UPDATE display_name=VALUES(display_name), id=LAST_INSERT_ID(id)`,
						tagCanon, tagDisp,
					)
					if err == nil {
						tagID, _ := resTag.LastInsertId()
						if tagID == 0 {
							row := h.db.DB().QueryRowContext(c.Request.Context(), `SELECT id FROM tags WHERE canonical_name = ?`, tagCanon)
							_ = row.Scan(&tagID)
						}
						if tagID > 0 {
							_, _ = h.db.DB().ExecContext(c.Request.Context(),
								`INSERT IGNORE INTO report_tags (report_seq, tag_id) VALUES (?, ?)`,
								seq, tagID,
							)
							_, _ = h.db.DB().ExecContext(c.Request.Context(),
								`UPDATE tags SET usage_count = usage_count + 1, last_used_at = NOW() WHERE id = ?`,
								tagID,
							)
						}
					}
				}
			}
			// Brand fields (same policy as insert)
			var brandDisplay string
			var brandName string
			if strings.EqualFold(req.Source, "twitter") {
				if it.Metadata != nil {
					if v, ok := it.Metadata["brand_display_name"].(string); ok && strings.TrimSpace(v) != "" {
						brandDisplay = v
					}
					if v, ok := it.Metadata["brand_name"].(string); ok && strings.TrimSpace(v) != "" {
						brandName = v
					}
				}
			} else if strings.EqualFold(req.Source, "github_issue") {
				brandDisplay = extractBrandDisplay(it.Title, it.Metadata)
				brandName = normalizeGithubBrandName(brandDisplay)
			} else {
				brandDisplay = extractBrandDisplay(it.Title, it.Metadata)
				brandName = brandutil.NormalizeBrandName(brandDisplay)
			}
			severity := clampSeverity(it.Score)
			// summary composed below; no standalone sumSafe needed

			// Compose trimmed description for summary
			trimmedDesc := trimmedDescriptionForSummary(description)
			// Extract optional analysis fields from metadata for update
			lp := 0.0
			if v, ok := it.Metadata["litter_probability"].(float64); ok {
				lp = v
			}
			hp := 0.0
			if v, ok := it.Metadata["hazard_probability"].(float64); ok {
				hp = v
			}
			dbp := 1.0
			if v, ok := it.Metadata["digital_bug_probability"].(float64); ok {
				dbp = v
			} else if v, ok := it.Metadata["classification"].(string); ok && v == "physical" {
				dbp = 0.0
			}
			if v, ok := it.Metadata["severity_level"].(float64); ok {
				severity = clampSeverity(v)
			}
			classification := ""
			if v, ok := it.Metadata["classification"].(string); ok {
				vv := strings.ToLower(strings.TrimSpace(v))
				if vv == "physical" || vv == "digital" {
					classification = vv
				} else {
					classification = ""
				}
			}
			lang := ""
			if v, ok := it.Metadata["language"].(string); ok {
				lang = v
			}
			effSummary := title + " : " + trimmedDesc + " : " + safeURL(it.URL)
			if s, ok := it.Metadata["summary"].(string); ok && s != "" {
				effSummary = s + " : " + safeURL(it.URL)
			}

			// Update analysis fields
			_, err = h.db.DB().ExecContext(c.Request.Context(), `
                UPDATE report_analysis SET title = ?, description = ?, brand_name = ?, brand_display_name = ?,
                    litter_probability = IFNULL(?, litter_probability),
                    hazard_probability = IFNULL(?, hazard_probability),
                    digital_bug_probability = IFNULL(?, digital_bug_probability),
                    severity_level = ?, summary = ?, 
                    classification = COALESCE(NULLIF(?, ''), classification),
                    language = COALESCE(NULLIF(?, ''), language),
                    updated_at = NOW()
                WHERE seq = ?
            `, title, description, brandName, brandDisplay, lp, hp, dbp, severity, truncate(effSummary, 8192),
				// classification conditional (empty string => keep existing)
				classification,
				// language conditional (empty string => keep existing)
				lang,
				seq)
			if err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("update analysis failed: %v", err), 256)})
				continue
			}

			// Update report image if provided
			if len(imgBytes) > 0 {
				if _, err := h.db.DB().ExecContext(c.Request.Context(), `UPDATE reports SET image = ?, ts = ts WHERE seq = ?`, imgBytes, seq); err != nil {
					resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("update image failed: %v", err), 256)})
					continue
				}
			}

			// Upsert report_details as well on update path
			company, product := splitOwnerRepo(brandDisplay)
			_, err = h.db.DB().ExecContext(c.Request.Context(), `
                INSERT INTO report_details (seq, company_name, product_name, url) VALUES (?, ?, ?, ?)
                ON DUPLICATE KEY UPDATE company_name=VALUES(company_name), product_name=VALUES(product_name), url=VALUES(url)
            `, seq, nullable(company), nullable(product), truncateRunes(stripNonBMP(it.URL), 500))
			if err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: truncate(fmt.Sprintf("update details failed: %v", err), 256)})
				continue
			}
			resp.Updated++

			// Publish to RabbitMQ for fast renderer on update as well
			if h.rabbitmqPublisher != nil {
				publishForRenderer(c, h, seq)
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// publishForRenderer publishes a sanitized ReportWithAnalysis to RabbitMQ for the fast renderer
func publishForRenderer(c *gin.Context, h *Handlers, seq int) {
	if h.rabbitmqPublisher == nil {
		return
	}
	rwa, err := h.db.GetReportBySeq(c.Request.Context(), seq)
	if err != nil {
		log.Printf("warn: cannot fetch report %d for rabbitmq publish: %v", seq, err)
		return
	}
	// Strip images from the event to match renderer expectations
	rwa.Report.Image = nil
	for i := range rwa.Analysis {
		rwa.Analysis[i].AnalysisImage = nil
	}
	if err := h.rabbitmqPublisher.Publish(rwa); err != nil {
		log.Printf("warn: rabbitmq publish failed for seq=%d: %v", seq, err)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// naive byte trim; DB columns are VARCHAR/TEXT; keep simple here
	return s[:max]
}

// truncateRunes returns a string limited by rune count, preventing mid-rune cuts
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == max {
			return s[:i]
		}
		count++
	}
	return s
}

// stripNonBMP removes characters outside the Basic Multilingual Plane (e.g., 4-byte emojis)
// to avoid issues on misconfigured MySQL setups even after utf8mb4 attempts.
func stripNonBMP(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r <= 0xFFFF { // keep BMP only
			b.WriteRune(r)
		}
	}
	return b.String()
}

func summary(title, desc string) string {
	if title == "" {
		return truncate(desc, 256)
	}
	if desc == "" {
		return title
	}
	return title + ": " + truncate(desc, 256)
}

func extractBrandDisplay(title string, metadata map[string]interface{}) string {
	if metadata != nil {
		if v, ok := metadata["app_name"].(string); ok && v != "" {
			return v
		}
		if v, ok := metadata["repo_full_name"].(string); ok && v != "" {
			return v
		}
	}
	return title
}

// normalizeGithubBrandName replaces owner/repo slash with hyphen and normalizes
func normalizeGithubBrandName(display string) string {
	// Preserve a single hyphen between owner and repo by normalizing parts separately
	parts := strings.Split(display, "/")
	if len(parts) >= 2 {
		owner := brandutil.NormalizeBrandName(parts[0])
		repo := brandutil.NormalizeBrandName(parts[1])
		if owner != "" && repo != "" {
			return owner + "-" + repo
		}
	}
	// Fallback: replace slash with hyphen, then normalize; hyphen may be removed by normalizer
	safe := strings.ReplaceAll(display, "/", "-")
	return brandutil.NormalizeBrandName(safe)
}

func clampSeverity(score float64) float64 {
	// Accept pre-normalized [0.0..1.0], clamp to [0.7..1.0]
	if score < 0.7 {
		return 0.7
	}
	if score > 1.0 {
		return 1.0
	}
	return score
}

// splitOwnerRepo extracts owner and repo from a display like "owner/repo"
func splitOwnerRepo(display string) (string, string) {
	if strings.Contains(display, "/") {
		parts := strings.SplitN(display, "/", 2)
		owner := parts[0]
		repo := parts[1]
		return owner, repo
	}
	return "", display
}

// safeURL normalizes and truncates URL for summary/details
func safeURL(u string) string {
	u = strings.TrimSpace(u)
	if len(u) > 500 {
		u = u[:500]
	}
	return u
}

// nullable returns nil for empty strings to allow DB NULL
func nullable(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// trimmedDescriptionForSummary: 1) take first 256 runes; 2) remove newlines; 3) append "..."
func trimmedDescriptionForSummary(desc string) string {
	// limit to 256 runes
	short := truncateRunes(desc, 256)
	// remove CR/LF
	single := strings.ReplaceAll(strings.ReplaceAll(short, "\r", " "), "\n", " ")
	// collapse multiple spaces
	single = strings.Join(strings.Fields(single), " ")
	// add ellipsis
	if single == "" {
		return "..."
	}
	if len(single) > 3 && strings.HasSuffix(single, "...") {
		return single
	}
	return single + "..."
}

// WebSocket upgrader
var upgrader = gorilla.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for now
		// In production, you should implement proper origin checking
		return true
	},
}

// ListenReports handles WebSocket connections for report listening
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
