package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"path"
	"strings"
	"time"

	"report-listener/database"

	"github.com/gin-gonic/gin"
)

const (
	digitalShareSourceKind       = "digital_share"
	maxDigitalShareImageCount    = 6
	maxDigitalShareImageBytes    = 16 * 1024 * 1024
	maxDigitalShareTotalBytes    = 64 * 1024 * 1024
	maxDigitalShareMultipartForm = 96 << 20
)

type digitalShareSubmissionRequest struct {
	SourceURL          string `json:"source_url" form:"source_url"`
	SharedText         string `json:"shared_text" form:"shared_text"`
	Platform           string `json:"platform" form:"platform"`
	SourceApp          string `json:"source_app" form:"source_app"`
	CaptureMode        string `json:"capture_mode" form:"capture_mode"`
	ClientCreatedAt    string `json:"client_created_at" form:"client_created_at"`
	ClientSubmissionID string `json:"client_submission_id" form:"client_submission_id"`
	ReporterID         string `json:"reporter_id" form:"reporter_id"`
	DeviceID           string `json:"device_id" form:"device_id"`
	AppVersion         string `json:"app_version" form:"app_version"`
}

type digitalShareSubmissionResponse struct {
	OK            bool   `json:"ok"`
	ReceiptID     string `json:"receipt_id"`
	SubmissionID  string `json:"submission_id"`
	SourceID      string `json:"source_id"`
	ReportID      int    `json:"report_id,omitempty"`
	PublicID      string `json:"public_id,omitempty"`
	Status        string `json:"status"`
	Lane          string `json:"lane"`
	Queued        bool   `json:"queued"`
	SharedPayload string `json:"shared_payload_type,omitempty"`
}

type normalizedDigitalSharePayload struct {
	SourceURL            string
	SharedText           string
	Platform             string
	SourceApp            string
	CaptureMode          string
	ClientCreatedAt      string
	ClientSubmissionID   string
	ReporterID           string
	DeviceID             string
	AppVersion           string
	Images               []digitalShareImageAttachment
	SharedPayloadType    string
	NormalizedSourceHash string
}

type digitalShareImageAttachment struct {
	Bytes     []byte
	MimeType  string
	Filename  string
	SHA256Hex string
}

func (h *Handlers) SubmitDigitalShare(c *gin.Context) {
	req, images, err := parseDigitalShareSubmissionRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	payload, parseErr := normalizeDigitalSharePayload(req, images)
	if parseErr != nil {
		log.Printf("digital share: parse failed: %v", parseErr)
		c.JSON(http.StatusBadRequest, gin.H{
			"ok":    false,
			"error": parseErr.Error(),
		})
		return
	}

	log.Printf(
		"digital share: submit started platform=%s source_app=%s payload=%s source_url_present=%t image_count=%d",
		payload.Platform,
		payload.SourceApp,
		payload.SharedPayloadType,
		payload.SourceURL != "",
		len(payload.Images),
	)

	auth := h.humanReportAuthContext("share")
	submission := buildDigitalShareWireSubmission(payload, h.cfg.HumanIngestReportSourcePrefix)
	receipt, statusCode := h.processCleanAppWireSubmissionInternalWithHook(
		c.Request.Context(),
		auth,
		submission,
		c.ClientIP(),
		c.GetHeader("User-Agent"),
		c.GetHeader("X-Request-Id"),
		"/api/v3/reports/digital-share",
		func(ctx context.Context, reportSeq int) error {
			return h.db.UpsertDigitalShareMetadata(
				ctx,
				reportSeq,
				payload.SourceURL,
				payload.SourceApp,
				payload.Platform,
				payload.CaptureMode,
				payload.ClientCreatedAt,
				payload.ClientSubmissionID,
				payload.NormalizedSourceHash,
				payload.SharedText,
				toDatabaseDigitalShareAttachments(payload.Images),
			)
		},
	)

	if receipt.ReportID > 0 && strings.TrimSpace(payload.DeviceID) != "" {
		if err := h.db.LinkReportToPushInstall(c.Request.Context(), receipt.ReportID, payload.DeviceID); err != nil {
			log.Printf("warn: failed to link digital share report %d to push install %s: %v", receipt.ReportID, payload.DeviceID, err)
		}
	}

	if receipt.ReportID > 0 && !receipt.IdempotencyReplay && strings.TrimSpace(payload.ReporterID) != "" {
		if err := h.db.IncrementReporterDailyKITNs(c.Request.Context(), payload.ReporterID); err != nil {
			log.Printf("warn: failed to increment KITNs for digital share %d (%s): %v", receipt.ReportID, payload.ReporterID, err)
		}
	}

	publicID := ""
	if receipt.ReportID > 0 {
		if pid, err := h.db.GetReportPublicIDBySeq(c.Request.Context(), receipt.ReportID); err == nil {
			publicID = pid
		} else {
			log.Printf("warn: failed to look up public_id for digital share %d: %v", receipt.ReportID, err)
		}
	}

	if len(receipt.Errors) > 0 && statusCode >= 400 {
		log.Printf("digital share: submit failed status=%d errors=%v", statusCode, receipt.Errors)
		c.JSON(statusCode, gin.H{
			"ok":                 false,
			"receipt_id":         receipt.ReceiptID,
			"submission_id":      receipt.SubmissionID,
			"source_id":          receipt.SourceID,
			"status":             receipt.Status,
			"lane":               receipt.Lane,
			"report_id":          receipt.ReportID,
			"public_id":          publicID,
			"errors":             receipt.Errors,
			"shared_payload":     payload.SharedPayloadType,
			"idempotency_replay": receipt.IdempotencyReplay,
		})
		return
	}

	log.Printf(
		"digital share: submit succeeded report_id=%d public_id=%s status=%s lane=%s replay=%t",
		receipt.ReportID,
		publicID,
		receipt.Status,
		receipt.Lane,
		receipt.IdempotencyReplay,
	)

	c.JSON(http.StatusOK, digitalShareSubmissionResponse{
		OK:            true,
		ReceiptID:     receipt.ReceiptID,
		SubmissionID:  receipt.SubmissionID,
		SourceID:      receipt.SourceID,
		ReportID:      receipt.ReportID,
		PublicID:      publicID,
		Status:        receipt.Status,
		Lane:          receipt.Lane,
		Queued:        len(receipt.Errors) == 0,
		SharedPayload: payload.SharedPayloadType,
	})
}

func parseDigitalShareSubmissionRequest(c *gin.Context) (digitalShareSubmissionRequest, []digitalShareImageAttachment, error) {
	var req digitalShareSubmissionRequest
	var images []digitalShareImageAttachment

	contentType := c.ContentType()
	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"):
		if err := c.Request.ParseMultipartForm(maxDigitalShareMultipartForm); err != nil {
			return req, nil, fmt.Errorf("invalid multipart payload")
		}
		req.SourceURL = strings.TrimSpace(c.PostForm("source_url"))
		req.SharedText = strings.TrimSpace(c.PostForm("shared_text"))
		req.Platform = strings.TrimSpace(c.PostForm("platform"))
		req.SourceApp = strings.TrimSpace(c.PostForm("source_app"))
		req.CaptureMode = strings.TrimSpace(c.PostForm("capture_mode"))
		req.ClientCreatedAt = strings.TrimSpace(c.PostForm("client_created_at"))
		req.ClientSubmissionID = strings.TrimSpace(c.PostForm("client_submission_id"))
		req.ReporterID = strings.TrimSpace(c.PostForm("reporter_id"))
		req.DeviceID = strings.TrimSpace(c.PostForm("device_id"))
		req.AppVersion = strings.TrimSpace(c.PostForm("app_version"))

		var fileErr error
		images, fileErr = readDigitalShareMultipartFiles(c)
		if fileErr != nil {
			return req, nil, fileErr
		}
	case strings.HasPrefix(contentType, "application/json"), contentType == "":
		if err := c.ShouldBindJSON(&req); err != nil {
			return req, nil, fmt.Errorf("invalid json payload")
		}
	default:
		return req, nil, fmt.Errorf("unsupported content type")
	}

	return req, images, nil
}

func digitalShareMultipartFileHeaders(c *gin.Context, keys ...string) []*multipart.FileHeader {
	if c.Request.MultipartForm == nil {
		return nil
	}
	var out []*multipart.FileHeader
	seen := map[string]struct{}{}
	for _, key := range keys {
		for _, fileHeader := range c.Request.MultipartForm.File[key] {
			if fileHeader == nil {
				continue
			}
			sig := fmt.Sprintf("%p", fileHeader)
			if _, ok := seen[sig]; ok {
				continue
			}
			seen[sig] = struct{}{}
			out = append(out, fileHeader)
		}
	}
	return out
}

func readDigitalShareMultipartFiles(c *gin.Context) ([]digitalShareImageAttachment, error) {
	fileHeaders := digitalShareMultipartFileHeaders(c, "attachments", "attachments[]", "attachment", "images", "image", "file")
	if len(fileHeaders) == 0 {
		return nil, nil
	}
	if len(fileHeaders) > maxDigitalShareImageCount {
		return nil, fmt.Errorf("too many attachments")
	}

	totalBytes := 0
	images := make([]digitalShareImageAttachment, 0, len(fileHeaders))
	for _, fileHeader := range fileHeaders {
		image, err := readDigitalShareMultipartFile(fileHeader)
		if err != nil {
			return nil, err
		}
		totalBytes += len(image.Bytes)
		if totalBytes > maxDigitalShareTotalBytes {
			return nil, fmt.Errorf("attachments too large")
		}
		images = append(images, image)
	}
	return images, nil
}

func readDigitalShareMultipartFile(fileHeader *multipart.FileHeader) (digitalShareImageAttachment, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return digitalShareImageAttachment{}, fmt.Errorf("failed to open attachment")
	}
	defer file.Close()

	limited := io.LimitReader(file, maxDigitalShareImageBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return digitalShareImageAttachment{}, fmt.Errorf("failed to read attachment")
	}
	if len(data) > maxDigitalShareImageBytes {
		return digitalShareImageAttachment{}, fmt.Errorf("attachment too large")
	}

	mimeType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	sum := sha256.Sum256(data)
	return digitalShareImageAttachment{
		Bytes:     data,
		MimeType:  clampStr(mimeType, 128),
		Filename:  clampStr(strings.TrimSpace(fileHeader.Filename), 255),
		SHA256Hex: hex.EncodeToString(sum[:]),
	}, nil
}

func normalizeDigitalSharePayload(
	req digitalShareSubmissionRequest,
	images []digitalShareImageAttachment,
) (normalizedDigitalSharePayload, error) {
	payload := normalizedDigitalSharePayload{
		SourceURL:          normalizeSharedURL(req.SourceURL),
		SharedText:         strings.TrimSpace(req.SharedText),
		Platform:           normalizeSharePlatform(req.Platform),
		SourceApp:          clampStr(strings.TrimSpace(req.SourceApp), 255),
		CaptureMode:        normalizeShareCaptureMode(req.CaptureMode),
		ClientCreatedAt:    normalizeClientCreatedAt(req.ClientCreatedAt),
		ClientSubmissionID: clampStr(strings.TrimSpace(req.ClientSubmissionID), 255),
		ReporterID:         clampStr(strings.TrimSpace(req.ReporterID), 128),
		DeviceID:           clampStr(strings.TrimSpace(req.DeviceID), 128),
		AppVersion:         clampStr(strings.TrimSpace(req.AppVersion), 64),
		Images:             normalizeDigitalShareAttachments(images),
	}

	if payload.SharedText != "" {
		if inferred := normalizeSharedURL(payload.SharedText); inferred != "" && payload.SourceURL == "" {
			payload.SourceURL = inferred
			payload.SharedText = ""
		}
	}
	if payload.SourceApp == "" {
		payload.SourceApp = inferShareSourceApp(payload.SourceURL)
	}

	switch {
	case payload.SourceURL != "" && payload.SharedText != "" && len(payload.Images) > 0:
		payload.SharedPayloadType = "url+text+image"
	case payload.SourceURL != "" && len(payload.Images) > 0:
		payload.SharedPayloadType = "url+image"
	case payload.SharedText != "" && len(payload.Images) > 0:
		payload.SharedPayloadType = "text+image"
	case payload.SourceURL != "":
		payload.SharedPayloadType = "url"
	case payload.SharedText != "":
		payload.SharedPayloadType = "text"
	case len(payload.Images) > 0:
		payload.SharedPayloadType = "image"
	default:
		return normalizedDigitalSharePayload{}, fmt.Errorf("no usable share payload")
	}

	payload.NormalizedSourceHash = digitalShareDedupKey(payload)
	return payload, nil
}

func normalizeDigitalShareAttachments(images []digitalShareImageAttachment) []digitalShareImageAttachment {
	if len(images) == 0 {
		return nil
	}
	capHint := len(images)
	if capHint > maxDigitalShareImageCount {
		capHint = maxDigitalShareImageCount
	}
	normalized := make([]digitalShareImageAttachment, 0, capHint)
	for _, image := range images {
		if len(image.Bytes) == 0 {
			continue
		}
		mimeType := clampStr(strings.TrimSpace(image.MimeType), 128)
		if mimeType == "" {
			mimeType = http.DetectContentType(image.Bytes)
		}
		filename := clampStr(strings.TrimSpace(image.Filename), 255)
		if filename == "" {
			filename = fmt.Sprintf("shared-image-%d", len(normalized)+1)
		}
		sha := strings.TrimSpace(image.SHA256Hex)
		if sha == "" {
			sum := sha256.Sum256(image.Bytes)
			sha = hex.EncodeToString(sum[:])
		}
		normalized = append(normalized, digitalShareImageAttachment{
			Bytes:     image.Bytes,
			MimeType:  mimeType,
			Filename:  filename,
			SHA256Hex: sha,
		})
		if len(normalized) == maxDigitalShareImageCount {
			break
		}
	}
	return normalized
}

func normalizeSharePlatform(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ios", "android":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "unknown"
	}
}

func normalizeShareCaptureMode(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "share_extension", "android_share_intent":
		return strings.TrimSpace(strings.ToLower(raw))
	default:
		return "unknown_share_target"
	}
}

func normalizeClientCreatedAt(raw string) string {
	if ts := parseRFC3339(strings.TrimSpace(raw)); ts != nil {
		return ts.UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func normalizeSharedURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		if !looksLikeURL(raw) {
			return ""
		}
		raw = "https://" + raw
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	host = strings.TrimPrefix(host, "www.")
	switch host {
	case "twitter.com":
		host = "x.com"
	}
	parsed.Scheme = "https"
	parsed.Host = host
	parsed.Fragment = ""
	if strings.HasSuffix(parsed.Path, "/") && parsed.Path != "/" {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	return parsed.String()
}

func inferShareSourceApp(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return ""
	}
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	host = strings.TrimPrefix(host, "www.")
	switch host {
	case "twitter.com":
		host = "x.com"
	}
	return host
}

func looksLikeURL(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || strings.ContainsAny(raw, " \n\t") {
		return false
	}
	return strings.Contains(raw, ".") || strings.HasPrefix(raw, "x.com/") || strings.HasPrefix(raw, "twitter.com/")
}

func digitalShareDedupKey(payload normalizedDigitalSharePayload) string {
	scope := strings.TrimSpace(firstNonEmptyShare(payload.ReporterID, payload.DeviceID, "anonymous"))
	switch {
	case payload.SourceURL != "":
		sum := sha256.Sum256([]byte(payload.SourceURL))
		return "url:" + scope + ":" + hex.EncodeToString(sum[:12])
	case len(payload.Images) > 0:
		return "img:" + scope + ":" + payload.Images[0].SHA256Hex[:24]
	default:
		sum := sha256.Sum256([]byte(payload.SharedText))
		return "txt:" + scope + ":" + hex.EncodeToString(sum[:12])
	}
}

func toDatabaseDigitalShareAttachments(images []digitalShareImageAttachment) []database.DigitalShareAttachment {
	if len(images) == 0 {
		return nil
	}
	out := make([]database.DigitalShareAttachment, 0, len(images))
	for idx, image := range images {
		out = append(out, database.DigitalShareAttachment{
			Ordinal:  idx,
			Filename: image.Filename,
			MIMEType: image.MimeType,
			SHA256:   image.SHA256Hex,
			Bytes:    image.Bytes,
		})
	}
	return out
}

func buildDigitalShareWireSubmission(payload normalizedDigitalSharePayload, sourcePrefix string) cleanAppWireSubmission {
	submittedAt := time.Now().UTC().Format(time.RFC3339)
	sourceID := fmt.Sprintf("%s:share:%s", strings.TrimSpace(sourcePrefix), payload.NormalizedSourceHash)
	if strings.TrimSpace(sourcePrefix) == "" {
		sourceID = "share:" + payload.NormalizedSourceHash
	}

	title, description := digitalShareTitleAndDescription(payload)
	sub := cleanAppWireSubmission{
		SchemaVersion: cleanAppWireSchemaV1,
		SourceID:      sourceID,
		SubmittedAt:   submittedAt,
		ObservedAt:    payload.ClientCreatedAt,
		Extensions: map[string]any{
			"share_source_app":           payload.SourceApp,
			"share_capture_mode":         payload.CaptureMode,
			"share_platform":             payload.Platform,
			"share_client_created_at":    payload.ClientCreatedAt,
			"share_payload_type":         payload.SharedPayloadType,
			"share_client_submission_id": payload.ClientSubmissionID,
			"device_id":                  payload.DeviceID,
			"app_version":                payload.AppVersion,
		},
	}

	sub.Agent.AgentID = firstNonEmptyShare(payload.ReporterID, payload.DeviceID, "share-user")
	sub.Agent.AgentName = "share-target"
	sub.Agent.AgentType = "user"
	sub.Agent.OperatorType = "individual"
	sub.Agent.AuthMethod = "device_session"
	sub.Agent.SoftwareVersion = payload.AppVersion
	sub.Agent.ExecutionMode = "share_target"

	sub.Provenance.GenerationMethod = "shared_submission"
	sub.Provenance.ChainOfCustody = []string{payload.CaptureMode, "digital_share_endpoint", "wire_ingest"}
	sub.Provenance.HumanInLoop = true
	if payload.SourceURL != "" {
		sub.Provenance.UpstreamSources = append(sub.Provenance.UpstreamSources, struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}{Kind: "url", Value: payload.SourceURL})
	}
	if payload.SourceApp != "" {
		sub.Provenance.UpstreamSources = append(sub.Provenance.UpstreamSources, struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}{Kind: "source_app", Value: payload.SourceApp})
	}

	sub.Report.Domain = "digital"
	sub.Report.ProblemType = "shared_content_report"
	sub.Report.Title = title
	sub.Report.Description = description
	sub.Report.Language = "und"
	sub.Report.Confidence = 0.85
	sub.Report.TargetEntity.TargetType = "platform"
	sub.Report.TargetEntity.Name = firstNonEmptyShare(payload.SourceApp, inferShareSourceApp(payload.SourceURL))
	sub.Report.DigitalContext = map[string]any{
		"source_url":           payload.SourceURL,
		"shared_text":          payload.SharedText,
		"source_app":           payload.SourceApp,
		"platform":             payload.Platform,
		"capture_mode":         payload.CaptureMode,
		"client_created_at":    payload.ClientCreatedAt,
		"client_submission_id": payload.ClientSubmissionID,
	}

	if payload.SourceURL != "" {
		sub.Report.EvidenceBundle = append(sub.Report.EvidenceBundle, struct {
			EvidenceID string `json:"evidence_id,omitempty"`
			Type       string `json:"type"`
			URI        string `json:"uri,omitempty"`
			SHA256     string `json:"sha256,omitempty"`
			MIMEType   string `json:"mime_type,omitempty"`
			CapturedAt string `json:"captured_at,omitempty"`
		}{
			EvidenceID: "shared-url",
			Type:       "url",
			URI:        payload.SourceURL,
			MIMEType:   "text/uri-list",
			CapturedAt: payload.ClientCreatedAt,
		})
	}

	if payload.SharedText != "" {
		sum := sha256.Sum256([]byte(payload.SharedText))
		sub.Report.EvidenceBundle = append(sub.Report.EvidenceBundle, struct {
			EvidenceID string `json:"evidence_id,omitempty"`
			Type       string `json:"type"`
			URI        string `json:"uri,omitempty"`
			SHA256     string `json:"sha256,omitempty"`
			MIMEType   string `json:"mime_type,omitempty"`
			CapturedAt string `json:"captured_at,omitempty"`
		}{
			EvidenceID: "shared-text",
			Type:       "text",
			SHA256:     hex.EncodeToString(sum[:]),
			MIMEType:   "text/plain",
			CapturedAt: payload.ClientCreatedAt,
		})
	}

	for idx, image := range payload.Images {
		sub.Report.EvidenceBundle = append(sub.Report.EvidenceBundle, struct {
			EvidenceID string `json:"evidence_id,omitempty"`
			Type       string `json:"type"`
			URI        string `json:"uri,omitempty"`
			SHA256     string `json:"sha256,omitempty"`
			MIMEType   string `json:"mime_type,omitempty"`
			CapturedAt string `json:"captured_at,omitempty"`
		}{
			EvidenceID: fmt.Sprintf("shared-image-%d", idx+1),
			Type:       "inline_image",
			SHA256:     image.SHA256Hex,
			MIMEType:   image.MimeType,
			CapturedAt: payload.ClientCreatedAt,
		})
		if idx == 0 {
			sub.Extensions["image_base64"] = base64.StdEncoding.EncodeToString(image.Bytes)
			sub.Extensions["image_filename"] = image.Filename
			sub.Extensions["image_mime_type"] = image.MimeType
		}
	}
	if len(payload.Images) > 0 {
		sub.Extensions["share_image_count"] = len(payload.Images)
	}

	sub.Delivery.RequestedLane = wireLaneHumanAuto
	return sub
}

func digitalShareTitleAndDescription(payload normalizedDigitalSharePayload) (string, string) {
	title := ""
	if payload.SharedText != "" {
		title = firstMeaningfulShareLine(payload.SharedText)
	}
	if title == "" && payload.SourceURL != "" {
		if parsed, err := neturl.Parse(payload.SourceURL); err == nil {
			host := strings.TrimPrefix(parsed.Hostname(), "www.")
			lastPath := strings.TrimSpace(path.Base(parsed.Path))
			switch {
			case host != "" && lastPath != "" && lastPath != "/":
				title = fmt.Sprintf("Shared post/page from %s: %s", host, lastPath)
			case host != "":
				title = fmt.Sprintf("Shared post/page from %s", host)
			}
		}
	}
	if title == "" && len(payload.Images) > 1 {
		title = "Shared screenshot set report"
	}
	if title == "" && len(payload.Images) == 1 {
		title = "Shared screenshot report"
	}
	if title == "" {
		title = "Shared digital report"
	}
	title = clampStr(strings.ReplaceAll(title, "\n", " "), 255)

	var parts []string
	if payload.SourceURL != "" {
		parts = append(parts, "Shared URL: "+payload.SourceURL)
	}
	if payload.SharedText != "" {
		parts = append(parts, "Shared text: "+payload.SharedText)
	}
	if payload.SourceApp != "" {
		parts = append(parts, "Source app: "+payload.SourceApp)
	}
	if len(parts) == 0 {
		parts = append(parts, title)
	}
	description := strings.Join(parts, "\n\n")
	return title, clampStr(description, 8192)
}

func firstMeaningfulShareLine(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return line
	}
	return raw
}

func firstNonEmptyShare(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
