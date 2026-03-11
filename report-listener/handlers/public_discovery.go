package handlers

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"report-listener/database"
	"report-listener/models"
	"report-listener/publicdiscovery"

	"github.com/gin-gonic/gin"
)

const (
	publicDiscoveryDefaultLimit = 12
	publicDiscoveryMaxLimit     = 50
	publicPointsDefaultLimit    = 4000
	publicPointsMaxLimit        = 8000
)

func canonicalReportPath(classification, publicID string) string {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return ""
	}
	tab := "physical"
	if classification == "digital" {
		tab = "digital"
	}
	return fmt.Sprintf("/%s/report/%s", tab, url.PathEscape(publicID))
}

func canonicalBrandPath(brandName string) string {
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		return ""
	}
	return fmt.Sprintf("/digital/%s", url.PathEscape(brandName))
}

func (h *Handlers) sealDiscoveryReportToken(classification, publicID string) (string, error) {
	return h.discoveryCodec.Seal(publicdiscovery.Payload{
		Kind:           publicdiscovery.KindReport,
		Classification: classification,
		PublicID:       publicID,
		ExpiresAtUnix:  time.Now().Add(h.cfg.PublicDiscoveryTokenTTL).Unix(),
	})
}

func (h *Handlers) sealDiscoveryBrandToken(classification, brandName string) (string, error) {
	return h.discoveryCodec.Seal(publicdiscovery.Payload{
		Kind:           publicdiscovery.KindBrand,
		Classification: classification,
		BrandName:      brandName,
		ExpiresAtUnix:  time.Now().Add(h.cfg.PublicDiscoveryTokenTTL).Unix(),
	})
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func parseOptionalInt(raw string, def int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func parseOptionalFloat(raw string, def float64) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func toPublicDiscoveryCards(
	reports []models.ReportWithMinimalAnalysis,
	language string,
	tokenForReport func(classification, publicID string) (string, error),
) ([]models.PublicDiscoveryCard, error) {
	items := make([]models.PublicDiscoveryCard, 0, len(reports))
	for _, report := range reports {
		if strings.TrimSpace(report.Report.PublicID) == "" {
			continue
		}
		analysis := database.PreferredMinimalAnalysis(report.Analysis, language)
		classification := analysis.Classification
		if classification == "" {
			classification = "physical"
		}
		token, err := tokenForReport(classification, report.Report.PublicID)
		if err != nil {
			return nil, err
		}

		item := models.PublicDiscoveryCard{
			DiscoveryToken: token,
			Title:          analysis.Title,
			Summary:        analysis.Summary,
			Classification: classification,
			SeverityLevel:  analysis.SeverityLevel,
			Timestamp:      report.Report.Timestamp.Format("2006-01-02 15:04:05"),
			BrandName:      analysis.BrandName,
			BrandDisplay:   analysis.BrandDisplayName,
		}
		if classification == "physical" {
			lat := report.Report.Latitude
			lon := report.Report.Longitude
			item.Latitude = &lat
			item.Longitude = &lon
		}
		items = append(items, item)
	}
	return items, nil
}

func (h *Handlers) publicDiscoveryBatchFromInterface(
	reportsInterface interface{},
	language string,
) (models.PublicDiscoveryBatch, error) {
	reports, ok := reportsInterface.([]models.ReportWithMinimalAnalysis)
	if !ok {
		if fullReports, ok := reportsInterface.([]models.ReportWithAnalysis); ok && len(fullReports) == 0 {
			return models.PublicDiscoveryBatch{Items: []models.PublicDiscoveryCard{}, Count: 0}, nil
		}
		return models.PublicDiscoveryBatch{}, fmt.Errorf("invalid public discovery payload")
	}
	items, err := toPublicDiscoveryCards(reports, language, h.sealDiscoveryReportToken)
	if err != nil {
		return models.PublicDiscoveryBatch{}, err
	}
	return models.PublicDiscoveryBatch{
		Items: items,
		Count: len(items),
	}, nil
}

type pointCluster struct {
	latSum         float64
	lonSum         float64
	maxSeverity    float64
	count          int
	classification string
}

func clusterCellSize(zoom float64) float64 {
	switch {
	case zoom < 3:
		return 10.0
	case zoom < 5:
		return 4.0
	case zoom < 7:
		return 1.5
	default:
		return 0
	}
}

func clusterPublicPoints(points []database.PublicPointRecord, zoom float64) []models.PublicPhysicalPoint {
	cellSize := clusterCellSize(zoom)
	if cellSize <= 0 {
		return nil
	}

	buckets := make(map[string]*pointCluster)
	for _, point := range points {
		row := int(math.Floor((point.Latitude + 90.0) / cellSize))
		col := int(math.Floor((point.Longitude + 180.0) / cellSize))
		key := fmt.Sprintf("%d:%d", row, col)
		bucket := buckets[key]
		if bucket == nil {
			bucket = &pointCluster{classification: "physical"}
			buckets[key] = bucket
		}
		bucket.latSum += point.Latitude
		bucket.lonSum += point.Longitude
		if point.SeverityLevel > bucket.maxSeverity {
			bucket.maxSeverity = point.SeverityLevel
		}
		bucket.count++
	}

	out := make([]models.PublicPhysicalPoint, 0, len(buckets))
	for _, bucket := range buckets {
		if bucket.count == 0 {
			continue
		}
		out = append(out, models.PublicPhysicalPoint{
			Kind:           "cluster",
			Classification: bucket.classification,
			Latitude:       bucket.latSum / float64(bucket.count),
			Longitude:      bucket.lonSum / float64(bucket.count),
			SeverityLevel:  bucket.maxSeverity,
			Count:          bucket.count,
		})
	}
	return out
}

func (h *Handlers) GetPublicDiscoveryLast(c *gin.Context) {
	limit, err := parseOptionalInt(c.Query("n"), publicDiscoveryDefaultLimit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter"})
		return
	}
	limit = clampInt(limit, 1, publicDiscoveryMaxLimit)

	classification := c.DefaultQuery("classification", "physical")
	language := c.DefaultQuery("lang", "en")

	reportsInterface, err := h.db.GetLastNAnalyzedReports(c.Request.Context(), limit, classification, false)
	if err != nil {
		log.Printf("Failed to get public discovery last reports: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	batch, err := h.publicDiscoveryBatchFromInterface(reportsInterface, language)
	if err != nil {
		log.Printf("Failed to build public discovery last batch: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}
	c.JSON(http.StatusOK, batch)
}

func (h *Handlers) GetPublicDiscoverySearch(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'q' parameter"})
		return
	}

	classification := strings.TrimSpace(c.Query("classification"))
	language := c.DefaultQuery("lang", "en")
	limit, err := parseOptionalInt(c.Query("n"), publicDiscoveryDefaultLimit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter"})
		return
	}
	limit = clampInt(limit, 1, publicDiscoveryMaxLimit)

	transformedQuery := strings.Join(strings.Fields(strings.ReplaceAll(query, "-", "+")), " ")
	words := strings.Fields(transformedQuery)
	for i, word := range words {
		if !strings.HasPrefix(word, "+") {
			words[i] = "+" + word
		}
	}
	transformedQuery = strings.Join(words, " ")
	if transformedQuery == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'q' parameter"})
		return
	}

	reportsInterface, err := h.db.SearchReports(c.Request.Context(), transformedQuery, classification, false, limit)
	if err != nil {
		log.Printf("Failed to get public discovery search reports: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	batch, err := h.publicDiscoveryBatchFromInterface(reportsInterface, language)
	if err != nil {
		log.Printf("Failed to build public discovery search batch: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}
	c.JSON(http.StatusOK, batch)
}

func (h *Handlers) GetPublicDiscoveryByBrand(c *gin.Context) {
	brandName := strings.TrimSpace(c.Query("brand_name"))
	if brandName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'brand_name' parameter"})
		return
	}

	language := c.DefaultQuery("lang", "en")
	limit, err := parseOptionalInt(c.Query("n"), publicDiscoveryDefaultLimit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter"})
		return
	}
	limit = clampInt(limit, 1, publicDiscoveryMaxLimit)

	reportsLite, err := h.db.GetReportsByBrandNameLite(c.Request.Context(), brandName, limit)
	if err != nil {
		log.Printf("Failed to get public discovery by-brand '%s': %v", brandName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	items, err := toPublicDiscoveryCards(reportsLite, language, h.sealDiscoveryReportToken)
	if err != nil {
		log.Printf("Failed to build public discovery by-brand '%s': %v", brandName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	totalCount := len(items)
	highPriorityCount := 0
	mediumPriorityCount := 0
	if cached, ok, fresh := h.getBrandCountsCached(brandName); ok && fresh {
		totalCount = cached.Total
		highPriorityCount = cached.High
		mediumPriorityCount = cached.Medium
	} else {
		countCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		total, high, medium, countErr := h.db.GetBrandPriorityCountsByBrandName(countCtx, brandName)
		cancel()
		if countErr == nil {
			totalCount = total
			highPriorityCount = high
			mediumPriorityCount = medium
			h.setBrandCountsCached(brandName, total, high, medium)
		}
	}

	c.JSON(http.StatusOK, models.PublicDiscoveryBatch{
		Items:               items,
		Count:               len(items),
		TotalCount:          totalCount,
		HighPriorityCount:   highPriorityCount,
		MediumPriorityCount: mediumPriorityCount,
	})
}

func (h *Handlers) GetPublicDiscoveryByLatLng(c *gin.Context) {
	latitude, err := strconv.ParseFloat(strings.TrimSpace(c.Query("latitude")), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'latitude' parameter"})
		return
	}
	longitude, err := strconv.ParseFloat(strings.TrimSpace(c.Query("longitude")), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'longitude' parameter"})
		return
	}
	radiusKm, err := parseOptionalFloat(c.Query("radius_km"), 0.5)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'radius_km' parameter"})
		return
	}
	limit, err := parseOptionalInt(c.Query("n"), publicDiscoveryDefaultLimit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter"})
		return
	}
	limit = clampInt(limit, 1, publicDiscoveryMaxLimit)
	language := c.DefaultQuery("lang", "en")

	reportsLite, err := h.db.GetReportsByLatLngLite(c.Request.Context(), latitude, longitude, radiusKm, limit)
	if err != nil {
		log.Printf("Failed to get public discovery by-latlng: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	items, err := toPublicDiscoveryCards(reportsLite, language, h.sealDiscoveryReportToken)
	if err != nil {
		log.Printf("Failed to build public discovery by-latlng batch: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}
	c.JSON(http.StatusOK, models.PublicDiscoveryBatch{
		Items: items,
		Count: len(items),
	})
}

func (h *Handlers) GetPublicDiscoveryBrandSummaries(c *gin.Context) {
	classification := c.DefaultQuery("classification", "digital")
	language := c.DefaultQuery("lang", "en")
	limit, err := parseOptionalInt(c.Query("n"), 500)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter"})
		return
	}
	limit = clampInt(limit, 1, 1000)

	items, err := h.db.GetPublicBrandSummaries(c.Request.Context(), classification, language, limit)
	if err != nil {
		log.Printf("Failed to get public brand summaries: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve brand summaries"})
		return
	}

	out := make([]models.PublicBrandSummary, 0, len(items))
	for _, item := range items {
		token, err := h.sealDiscoveryBrandToken(item.Classification, item.BrandName)
		if err != nil {
			log.Printf("Failed to seal public brand token for '%s': %v", item.BrandName, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve brand summaries"})
			return
		}
		out = append(out, models.PublicBrandSummary{
			Classification: item.Classification,
			DiscoveryToken: token,
			BrandName:      item.BrandName,
			BrandDisplay:   item.BrandDisplay,
			Total:          item.Total,
		})
	}
	c.JSON(http.StatusOK, out)
}

func parseBBox(raw string) (lonMin, latMin, lonMax, latMax float64, err error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("bbox must have 4 comma-separated values")
	}
	values := make([]float64, 4)
	for i, part := range parts {
		value, parseErr := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if parseErr != nil {
			return 0, 0, 0, 0, parseErr
		}
		values[i] = value
	}
	return values[0], values[1], values[2], values[3], nil
}

func (h *Handlers) GetPublicDiscoveryPhysicalPoints(c *gin.Context) {
	bbox := c.Query("bbox")
	if strings.TrimSpace(bbox) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'bbox' parameter"})
		return
	}

	lonMin, latMin, lonMax, latMax, err := parseBBox(bbox)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'bbox' parameter"})
		return
	}

	zoom, err := parseOptionalFloat(c.Query("zoom"), 2.5)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'zoom' parameter"})
		return
	}
	limit, err := parseOptionalInt(c.Query("limit"), publicPointsDefaultLimit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'limit' parameter"})
		return
	}
	limit = clampInt(limit, 100, publicPointsMaxLimit)

	points, err := h.db.GetPublicPhysicalPointsByBBox(c.Request.Context(), latMin, latMax, lonMin, lonMax, limit)
	if err != nil {
		log.Printf("Failed to get public physical points: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve map points"})
		return
	}

	if clusters := clusterPublicPoints(points, zoom); len(clusters) > 0 {
		c.JSON(http.StatusOK, clusters)
		return
	}

	out := make([]models.PublicPhysicalPoint, 0, len(points))
	for _, point := range points {
		if strings.TrimSpace(point.PublicID) == "" {
			continue
		}
		token, err := h.sealDiscoveryReportToken("physical", point.PublicID)
		if err != nil {
			log.Printf("Failed to seal public point token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve map points"})
			return
		}
		out = append(out, models.PublicPhysicalPoint{
			Kind:           "point",
			Classification: "physical",
			MarkerToken:    token,
			Latitude:       point.Latitude,
			Longitude:      point.Longitude,
			SeverityLevel:  point.SeverityLevel,
		})
	}

	c.JSON(http.StatusOK, out)
}

func (h *Handlers) ResolvePublicDiscoveryToken(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'token' parameter"})
		return
	}

	payload, err := h.discoveryCodec.Open(token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired token"})
		return
	}

	switch payload.Kind {
	case publicdiscovery.KindReport:
		canonicalPath := canonicalReportPath(payload.Classification, payload.PublicID)
		if canonicalPath == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid report token"})
			return
		}
		c.JSON(http.StatusOK, models.PublicDiscoveryResolveResponse{
			Kind:           string(payload.Kind),
			Classification: payload.Classification,
			PublicID:       payload.PublicID,
			CanonicalPath:  canonicalPath,
		})
	case publicdiscovery.KindBrand:
		canonicalPath := canonicalBrandPath(payload.BrandName)
		if canonicalPath == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid brand token"})
			return
		}
		c.JSON(http.StatusOK, models.PublicDiscoveryResolveResponse{
			Kind:           string(payload.Kind),
			Classification: payload.Classification,
			BrandName:      payload.BrandName,
			CanonicalPath:  canonicalPath,
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported token kind"})
	}
}
