package server

import (
	"bytes"
	"cleanapp-common/appenv"
	"cleanapp-common/httpx"
	"cleanapp/backend/server/api"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type humanIngestProxyRequest struct {
	Version    string  `json:"version"`
	Channel    string  `json:"channel"`
	ReporterID string  `json:"reporter_id,omitempty"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Image      []byte  `json:"image,omitempty"`
	ActionID   string  `json:"action_id,omitempty"`
	Annotation string  `json:"annotation,omitempty"`
}

type humanIngestProxyResponse struct {
	ReceiptID string `json:"receipt_id"`
	ReportID  int    `json:"report_id"`
	PublicID  string `json:"public_id,omitempty"`
	Status    string `json:"status"`
	Lane      string `json:"lane"`
}

func humanIngestSubmitURL() string {
	base := strings.TrimRight(appenv.String("HUMAN_INGEST_BASE_URL", "https://live.cleanapp.io"), "/")
	return base + "/api/v1/human-reports/submit"
}

func proxyLegacyReportToHumanIngest(c *gin.Context, report *api.ReportArgs) (*humanIngestProxyResponse, int, string) {
	payload := humanIngestProxyRequest{
		Version:    "2.0",
		Channel:    "legacy_v2",
		ReporterID: strings.TrimSpace(report.Id),
		Latitude:   report.Latitude,
		Longitude:  report.Longitude,
		X:          report.X,
		Y:          report.Y,
		Image:      report.Image,
		ActionID:   strings.TrimSpace(report.ActionId),
		Annotation: strings.TrimSpace(report.Annotation),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, http.StatusInternalServerError, "failed to marshal human ingest payload"
	}

	client := httpx.NewClient(20 * time.Second)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, humanIngestSubmitURL(), bytes.NewReader(body))
	if err != nil {
		return nil, http.StatusInternalServerError, "failed to create human ingest request"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", c.GetHeader("X-Request-Id"))
	req.Header.Set("User-Agent", c.GetHeader("User-Agent"))

	resp, err := client.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, "failed to submit report to canonical ingest"
	}
	defer resp.Body.Close()

	var humanResp humanIngestProxyResponse
	if err := json.NewDecoder(resp.Body).Decode(&humanResp); err != nil {
		if resp.StatusCode >= 500 {
			return nil, http.StatusBadGateway, "canonical ingest returned an invalid response"
		}
		return nil, http.StatusBadRequest, "canonical ingest rejected the report"
	}
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, humanResp.Status
	}
	return &humanResp, http.StatusOK, ""
}

func Report(c *gin.Context) {
	var report = &api.ReportArgs{}

	if err := c.BindJSON(report); err != nil {
		log.Errorf("Failed to get the argument in /report call: %w", err)
		return
	}

	if report.Version != "2.0" {
		log.Errorf("Bad version in /report, expected: 2.0, got: %v", report.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.")
		return
	}

	humanResp, statusCode, statusText := proxyLegacyReportToHumanIngest(c, report)
	if humanResp == nil {
		if statusText == "" {
			statusText = "Failed to save the report."
		}
		c.String(statusCode, statusText)
		return
	}

	c.Header("X-CleanApp-Legacy-Route", "deprecated")
	c.JSON(http.StatusOK, api.ReportResponse{Seq: humanResp.ReportID})
}
