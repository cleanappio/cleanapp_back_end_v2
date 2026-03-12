package handlers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"report-listener/geojsonx"
	"report-listener/middleware"
	"report-listener/models"
)

var clusterStopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "that": {}, "this": {}, "into": {}, "onto": {}, "over": {},
	"near": {}, "under": {}, "school": {}, "campus": {}, "building": {}, "facility": {}, "report": {}, "issue": {},
	"incident": {}, "hazard": {}, "physical": {}, "digital": {}, "brick": {}, "wall": {}, "facade": {}, "on": {}, "at": {},
}

type clusterNode struct {
	report    models.ReportWithAnalysis
	analysis  *models.ReportAnalysis
	tokenSet  map[string]struct{}
	titleText string
}

type clusterEdge struct {
	score     float64
	rationale []string
}

const (
	clusterAnalyzeTargetDiscoveryTimeout = 4 * time.Second
	clusterAnalyzeCaseMatchTimeout       = 1500 * time.Millisecond
	caseTargetRefreshTimeout             = 5 * time.Second
)

func (h *Handlers) AnalyzeCluster(c *gin.Context) {
	var req models.ReportsByGeometryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	geometryJSON, err := normalizeGeometryPayload(req.Geometry)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	n := req.N
	if n <= 0 {
		n = 250
	}
	classification := req.Classification
	if classification == "" {
		classification = "physical"
	}

	reports, err := h.db.GetReportsByGeometry(c.Request.Context(), string(geometryJSON), classification, n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to analyze cluster scope"})
		return
	}

	suggestedTargets := h.bestEffortClusterSuggestedTargets(c.Request.Context(), string(geometryJSON), reports, 8)
	response := analyzeClusterReports(reports, classification, "geometry", 0, json.RawMessage(geometryJSON), suggestedTargets)
	response.CandidateCases = h.bestEffortClusterCaseMatches(
		c.Request.Context(),
		classification,
		string(geometryJSON),
		reports,
		response.Hypotheses,
	)
	c.JSON(http.StatusOK, response)
}

func (h *Handlers) AnalyzeClusterFromReport(c *gin.Context) {
	var req models.ClusterFromReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.Seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "seq is required"})
		return
	}

	seed, err := h.db.GetReportBySeq(c.Request.Context(), req.Seq)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "seed report not found"})
		return
	}

	radiusKm := req.RadiusKm
	if radiusKm <= 0 {
		radiusKm = 0.25
	}
	n := req.N
	if n <= 0 {
		n = 250
	}
	reports, err := h.db.GetReportsByLatLng(c.Request.Context(), seed.Report.Latitude, seed.Report.Longitude, radiusKm, n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to analyze nearby reports"})
		return
	}

	classification := req.Classification
	if classification == "" {
		classification = preferredClassification(seed)
		if classification == "" {
			classification = "physical"
		}
	}
	filtered := reports[:0]
	for _, report := range reports {
		if preferredClassification(&report) == classification {
			filtered = append(filtered, report)
		}
	}
	reports = filtered

	suggestedTargets := h.bestEffortClusterSuggestedTargets(c.Request.Context(), "", reports, 8)
	response := analyzeClusterReports(reports, classification, "seed_report", req.Seq, nil, suggestedTargets)
	response.CandidateCases = h.bestEffortClusterCaseMatches(
		c.Request.Context(),
		classification,
		"",
		reports,
		response.Hypotheses,
	)
	c.JSON(http.StatusOK, response)
}

func (h *Handlers) bestEffortClusterSuggestedTargets(
	parent context.Context,
	geometryJSON string,
	reports []models.ReportWithAnalysis,
	limit int,
) []models.CaseEscalationTarget {
	ctx, cancel := context.WithTimeout(parent, clusterAnalyzeTargetDiscoveryTimeout)
	defer cancel()

	targets, err := h.suggestEscalationTargets(ctx, geometryJSON, reports, limit)
	if err != nil {
		log.Printf("warn: cluster target derivation failed: %v", err)
		return nil
	}
	return targets
}

func (h *Handlers) bestEffortClusterCaseMatches(
	parent context.Context,
	classification string,
	geometryJSON string,
	reports []models.ReportWithAnalysis,
	hypotheses []models.ClusterIncidentHypothesis,
) []models.CaseMatchCandidate {
	ctx, cancel := context.WithTimeout(parent, clusterAnalyzeCaseMatchTimeout)
	defer cancel()

	candidates, err := h.findCaseMatchCandidates(
		ctx,
		classification,
		geometryJSON,
		reports,
		extractHypothesisMatchTexts(hypotheses),
	)
	if err != nil {
		log.Printf("warn: cluster nearby-case lookup failed: %v", err)
		return nil
	}
	return candidates
}

func (h *Handlers) MatchCluster(c *gin.Context) {
	var req models.MatchClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	classification := strings.TrimSpace(req.Classification)
	if classification == "" {
		classification = "physical"
	}

	var geometryJSON string
	if req.Geometry != nil {
		geometryBlob, err := normalizeGeometryPayload(req.Geometry)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		geometryJSON = string(geometryBlob)
	}

	reports := make([]models.ReportWithAnalysis, 0)
	if len(req.ReportSeqs) > 0 {
		loaded, err := h.db.GetReportsBySeqs(c.Request.Context(), req.ReportSeqs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load reports for matching"})
			return
		}
		reports = loaded
	} else if geometryJSON != "" {
		n := req.N
		if n <= 0 {
			n = 250
		}
		loaded, err := h.db.GetReportsByGeometry(c.Request.Context(), geometryJSON, classification, n)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load reports for matching"})
			return
		}
		reports = loaded
	}

	candidates, err := h.findCaseMatchCandidates(
		c.Request.Context(),
		classification,
		geometryJSON,
		reports,
		[]string{req.Title, req.Summary},
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to match cluster to cases"})
		return
	}

	c.JSON(http.StatusOK, models.MatchClusterResponse{
		Classification: classification,
		CandidateCases: candidates,
	})
}

func (h *Handlers) GetCasesByReportSeq(c *gin.Context) {
	seqParam := strings.TrimSpace(c.Query("seq"))
	if seqParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "seq is required"})
		return
	}

	seq, err := strconv.Atoi(seqParam)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "seq must be a positive integer"})
		return
	}

	items, err := h.db.GetCasesByReportSeq(c.Request.Context(), seq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load cases for report"})
		return
	}

	c.JSON(http.StatusOK, models.ReportCasesResponse{
		Seq:   seq,
		Cases: items,
	})
}

func (h *Handlers) CreateCase(c *gin.Context) {
	var req models.CreateCaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title is required"})
		return
	}

	userID := middleware.GetUserIDFromContext(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var geometryJSON string
	if req.Geometry != nil {
		geometryBlob, err := normalizeGeometryPayload(req.Geometry)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		geometryJSON = string(geometryBlob)
	}

	var (
		anchorReport *models.ReportWithAnalysis
		err          error
	)
	if req.AnchorReportSeq > 0 {
		anchorReport, err = h.db.GetReportBySeq(c.Request.Context(), req.AnchorReportSeq)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "anchor report not found"})
			return
		}
	}

	clusterIDs := make([]string, 0, 1)
	if req.ClusterStats != nil || req.ClusterAnalysis != nil || req.ClusterSummary != "" || geometryJSON != "" {
		savedCluster := &models.SavedCluster{
			SourceType:      emptyDefault(req.ClusterSourceType, "geometry"),
			Classification:  emptyDefault(req.Classification, "physical"),
			GeometryJSON:    geometryJSON,
			SeedReportSeq:   intPtr(req.AnchorReportSeq),
			ReportCount:     len(req.ReportSeqs),
			Summary:         req.ClusterSummary,
			CreatedByUserID: userID,
		}
		if statsJSON, err := json.Marshal(req.ClusterStats); err == nil && string(statsJSON) != "null" {
			savedCluster.StatsJSON = string(statsJSON)
		}
		if analysisJSON, err := json.Marshal(req.ClusterAnalysis); err == nil && string(analysisJSON) != "null" {
			savedCluster.AnalysisJSON = string(analysisJSON)
		}
		if err := h.db.CreateSavedCluster(c.Request.Context(), savedCluster); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist cluster snapshot"})
			return
		}
		clusterIDs = append(clusterIDs, savedCluster.ClusterID)
	}

	targets := make([]models.CaseEscalationTarget, 0, len(req.EscalationTargets))
	for _, target := range req.EscalationTargets {
		targets = append(targets, models.CaseEscalationTarget{
			RoleType:        target.RoleType,
			Organization:    target.Organization,
			DisplayName:     target.DisplayName,
			Channel:         target.Channel,
			Email:           target.Email,
			Phone:           target.Phone,
			Website:         target.Website,
			ContactURL:      target.ContactURL,
			SocialPlatform:  target.SocialPlatform,
			SocialHandle:    target.SocialHandle,
			TargetSource:    target.TargetSource,
			ConfidenceScore: target.ConfidenceScore,
			Rationale:       target.Rationale,
		})
	}
	if len(targets) == 0 {
		autoTargetReports := make([]models.ReportWithAnalysis, 0, len(req.ReportSeqs)+1)
		if len(req.ReportSeqs) > 0 {
			autoTargetReports, err = h.db.GetReportsBySeqs(c.Request.Context(), req.ReportSeqs)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load reports for escalation target discovery"})
				return
			}
		}
		if len(autoTargetReports) == 0 && anchorReport != nil {
			autoTargetReports = append(autoTargetReports, *anchorReport)
		}
		autoTargets, err := h.suggestEscalationTargets(c.Request.Context(), geometryJSON, autoTargetReports, 8)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to suggest escalation targets"})
			return
		}
		targets = autoTargets
	}

	reportsForMatching := make([]models.ReportWithAnalysis, 0, len(req.ReportSeqs)+1)
	if len(req.ReportSeqs) > 0 {
		reportsForMatching, err = h.db.GetReportsBySeqs(c.Request.Context(), req.ReportSeqs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load reports for case matching"})
			return
		}
	}
	if len(reportsForMatching) == 0 && anchorReport != nil {
		reportsForMatching = append(reportsForMatching, *anchorReport)
	}

	matchTexts := []string{req.Title, req.Summary, req.ClusterSummary}
	candidateCases := make([]models.CaseMatchCandidate, 0)
	if existingCaseID := strings.TrimSpace(req.ExistingCaseID); existingCaseID == "" || !req.ForceNewCase {
		candidateCases, err = h.findCaseMatchCandidates(
			c.Request.Context(),
			emptyDefault(req.Classification, "physical"),
			geometryJSON,
			reportsForMatching,
			matchTexts,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load matching cases"})
			return
		}
	}

	targetCaseID := strings.TrimSpace(req.ExistingCaseID)
	matchScore := 0.0
	matchReason := ""
	if targetCaseID == "" && !req.ForceNewCase {
		if preferred := preferredCaseMatchCandidate(candidateCases); preferred != nil {
			targetCaseID = preferred.CaseID
			matchScore = preferred.MatchScore
			matchReason = strings.Join(preferred.MatchReasons, "; ")
		}
	} else if targetCaseID != "" {
		matchScore = 1
		matchReason = "case selected explicitly"
		for _, candidate := range candidateCases {
			if candidate.CaseID == targetCaseID {
				matchScore = candidate.MatchScore
				matchReason = strings.Join(candidate.MatchReasons, "; ")
				break
			}
		}
	}

	caseRecord := &models.Case{
		Slug:             ensureUniqueSlug(strings.TrimSpace(slugify(req.Title))),
		Title:            strings.TrimSpace(req.Title),
		Type:             emptyDefault(req.Type, "incident"),
		Status:           emptyDefault(req.Status, "open"),
		Classification:   emptyDefault(req.Classification, "physical"),
		Summary:          req.Summary,
		UncertaintyNotes: req.UncertaintyNotes,
		GeometryJSON:     geometryJSON,
		CreatedByUserID:  userID,
	}
	if anchorReport != nil {
		caseRecord.AnchorReportSeq = &anchorReport.Report.Seq
		caseRecord.AnchorLat = &anchorReport.Report.Latitude
		caseRecord.AnchorLng = &anchorReport.Report.Longitude
		severity := preferredSeverity(anchorReport)
		caseRecord.SeverityScore = severity
		caseRecord.UrgencyScore = severity
		caseRecord.ConfidenceScore = 0.8
		caseRecord.ExposureScore = 0.6
		caseRecord.CriticalityScore = severity
		caseRecord.TrendScore = 0.4
		ts := anchorReport.Report.Timestamp
		caseRecord.FirstSeenAt = &ts
		caseRecord.LastSeenAt = &ts
	}
	if geometryJSON != "" {
		caseRecord.AggregateGeometryJSON, caseRecord.AggregateBBoxJSON, _ = geojsonx.BuildAggregateGeometryJSON([]string{geometryJSON})
	}
	caseRecord.ClusterCount = len(clusterIDs)
	caseRecord.LinkedReportCount = len(req.ReportSeqs)
	if len(clusterIDs) > 0 {
		now := time.Now().UTC()
		caseRecord.LastClusterAt = &now
	}

	auditPayload := gin.H{
		"report_seqs":     req.ReportSeqs,
		"anchor_seq":      req.AnchorReportSeq,
		"cluster_ids":     clusterIDs,
		"target_count":    len(targets),
		"classification":  caseRecord.Classification,
		"existing_case":   targetCaseID,
		"candidate_cases": candidateCases,
	}

	if targetCaseID != "" {
		clusterLinks := make([]models.CaseClusterLink, 0, len(clusterIDs))
		for _, clusterID := range clusterIDs {
			clusterLinks = append(clusterLinks, models.CaseClusterLink{
				CaseID:      targetCaseID,
				ClusterID:   clusterID,
				MatchScore:  matchScore,
				MatchReason: matchReason,
			})
		}
		if err := h.db.AttachClusterToCase(c.Request.Context(), targetCaseID, req.ReportSeqs, clusterLinks, targets, userID, auditPayload); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to attach cluster to case"})
			return
		}
		detail, err := h.db.GetCaseDetail(c.Request.Context(), targetCaseID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cluster attached but failed to load case"})
			return
		}
		if err := h.syncCaseContactStrategy(c.Request.Context(), detail); err != nil {
			log.Printf("warn: failed to sync case contact strategy for %s: %v", targetCaseID, err)
		}
		c.JSON(http.StatusOK, detail)
		return
	}
	if err := h.db.CreateCase(c.Request.Context(), caseRecord, req.ReportSeqs, clusterIDs, targets, auditPayload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create case"})
		return
	}

	detail, err := h.db.GetCaseDetail(c.Request.Context(), caseRecord.CaseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "case created but failed to load detail"})
		return
	}
	if err := h.syncCaseContactStrategy(c.Request.Context(), detail); err != nil {
		log.Printf("warn: failed to sync case contact strategy for %s: %v", caseRecord.CaseID, err)
	}
	c.JSON(http.StatusCreated, detail)
}

func (h *Handlers) UpsertCaseFromCluster(c *gin.Context) {
	h.CreateCase(c)
}

func (h *Handlers) MergeCases(c *gin.Context) {
	var req models.MergeCasesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.TargetCaseID) == "" || len(req.SourceCaseIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_case_id and source_case_ids are required"})
		return
	}
	userID := middleware.GetUserIDFromContext(c)
	if err := h.db.MergeCases(c.Request.Context(), req.TargetCaseID, req.SourceCaseIDs, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to merge cases"})
		return
	}
	detail, err := h.db.GetCaseDetail(c.Request.Context(), req.TargetCaseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cases merged but failed to load target case"})
		return
	}
	if err := h.syncCaseContactStrategy(c.Request.Context(), detail); err != nil {
		log.Printf("warn: failed to sync case contact strategy for %s: %v", req.TargetCaseID, err)
	}
	c.JSON(http.StatusOK, detail)
}

func (h *Handlers) GetCase(c *gin.Context) {
	detail, err := h.db.GetCaseDetail(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}
	if queryBoolParam(c, "refresh_targets") {
		if enriched, err := h.enrichCaseEscalationTargets(c.Request.Context(), detail); err != nil {
			log.Printf("warn: case escalation target enrichment failed for %s: %v", c.Param("case_id"), err)
		} else if len(enriched) > 0 {
			detail.EscalationTargets = enriched
		}
	}
	if detail.NotifyPlan == nil || len(detail.ContactObservations) == 0 || queryBoolParam(c, "refresh_targets") {
		if err := h.syncCaseContactStrategy(c.Request.Context(), detail); err != nil {
			log.Printf("warn: failed to sync case contact strategy for %s: %v", c.Param("case_id"), err)
		}
	}
	c.JSON(http.StatusOK, detail)
}

func (h *Handlers) AddReportsToCase(c *gin.Context) {
	var req models.AddReportsToCaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if len(req.ReportSeqs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "report_seqs is required"})
		return
	}
	userID := middleware.GetUserIDFromContext(c)
	if err := h.db.AddReportsToCase(c.Request.Context(), c.Param("case_id"), req.ReportSeqs, req.LinkReason, req.Confidence, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to attach reports"})
		return
	}
	detail, err := h.db.GetCaseDetail(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reports attached but failed to reload case"})
		return
	}
	if err := h.syncCaseContactStrategy(c.Request.Context(), detail); err != nil {
		log.Printf("warn: failed to sync case contact strategy for %s: %v", c.Param("case_id"), err)
	}
	c.JSON(http.StatusOK, detail)
}

func (h *Handlers) UpdateCaseStatus(c *gin.Context) {
	var req models.UpdateCaseStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status is required"})
		return
	}
	actorUserID := middleware.GetUserIDFromContext(c)
	if req.ActorUserID != "" {
		actorUserID = req.ActorUserID
	}
	payload := req.Payload
	if payload == nil {
		payload = gin.H{"status": req.Status, "summary": req.Summary}
	}
	if err := h.db.UpdateCaseStatus(c.Request.Context(), c.Param("case_id"), req.Status, req.Summary, actorUserID, payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update case status"})
		return
	}
	detail, err := h.db.GetCaseDetail(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "status updated but failed to reload case"})
		return
	}
	if err := h.syncCaseContactStrategy(c.Request.Context(), detail); err != nil {
		log.Printf("warn: failed to sync case contact strategy for %s: %v", c.Param("case_id"), err)
	}
	c.JSON(http.StatusOK, detail)
}

func analyzeClusterReports(
	reports []models.ReportWithAnalysis,
	classification string,
	scopeType string,
	anchorSeq int,
	geometry interface{},
	suggestedTargets []models.CaseEscalationTarget,
) models.ClusterAnalysisResponse {
	nodes := make([]clusterNode, 0, len(reports))
	breakdown := map[string]int{}
	var (
		totalSeverity float64
		maxSeverity   float64
		highCount     int
		mediumCount   int
		firstSeen     *models.Report
		lastSeen      *models.Report
	)

	for _, report := range reports {
		analysis := preferredAnalysis(&report)
		if analysis == nil {
			continue
		}
		nodes = append(nodes, clusterNode{
			report:    report,
			analysis:  analysis,
			tokenSet:  tokenizeForCluster(analysis.Title + " " + analysis.Summary + " " + analysis.Description),
			titleText: firstNonEmpty(analysis.Title, analysis.Summary, analysis.Description, report.Report.ID),
		})
		breakdown[firstNonEmpty(analysis.Classification, classification, "physical")]++
		totalSeverity += analysis.SeverityLevel
		if analysis.SeverityLevel > maxSeverity {
			maxSeverity = analysis.SeverityLevel
		}
		if analysis.SeverityLevel >= 0.8 {
			highCount++
		} else if analysis.SeverityLevel >= 0.5 {
			mediumCount++
		}
		if firstSeen == nil || report.Report.Timestamp.Before(firstSeen.Timestamp) {
			r := report.Report
			firstSeen = &r
		}
		if lastSeen == nil || report.Report.Timestamp.After(lastSeen.Timestamp) {
			r := report.Report
			lastSeen = &r
		}
	}

	stats := models.ClusterStats{
		Classification:          classification,
		ReportCount:             len(reports),
		SeverityMax:             maxSeverity,
		HighPriorityCount:       highCount,
		MediumPriorityCount:     mediumCount,
		ClassificationBreakdown: breakdown,
	}
	if len(nodes) > 0 {
		stats.SeverityAverage = totalSeverity / float64(len(nodes))
	}
	if firstSeen != nil {
		stats.FirstSeenAt = &firstSeen.Timestamp
	}
	if lastSeen != nil {
		stats.LastSeenAt = &lastSeen.Timestamp
	}

	hypotheses := buildHypotheses(nodes, classification)
	return models.ClusterAnalysisResponse{
		ScopeType:        scopeType,
		Classification:   classification,
		AnchorReportSeq:  anchorSeq,
		Geometry:         geometry,
		Reports:          reports,
		Stats:            stats,
		Hypotheses:       hypotheses,
		SuggestedTargets: suggestedTargets,
	}
}

func (h *Handlers) enrichCaseEscalationTargets(ctx context.Context, detail *models.CaseDetail) ([]models.CaseEscalationTarget, error) {
	if h.contactDiscoverer == nil || detail == nil {
		return nil, nil
	}
	if len(detail.LinkedReports) == 0 {
		return filterVisibleCaseTargets(detail.EscalationTargets), nil
	}

	seqs := make([]int, 0, len(detail.LinkedReports))
	for _, report := range detail.LinkedReports {
		if report.Seq > 0 {
			seqs = append(seqs, report.Seq)
		}
	}
	if len(seqs) == 0 {
		return filterVisibleCaseTargets(detail.EscalationTargets), nil
	}

	reports, err := h.db.GetReportsBySeqs(ctx, seqs)
	if err != nil {
		return nil, err
	}
	enrichCtx, cancel := context.WithTimeout(ctx, caseTargetRefreshTimeout)
	defer cancel()
	enriched := h.contactDiscoverer.EnrichTargets(enrichCtx, reports, detail.EscalationTargets, 16)
	stored, err := h.db.UpsertCaseEscalationTargets(ctx, detail.Case.CaseID, enriched)
	if err != nil {
		return filterVisibleCaseTargets(enriched), nil
	}
	return filterVisibleCaseTargets(stored), nil
}

func filterVisibleCaseTargets(targets []models.CaseEscalationTarget) []models.CaseEscalationTarget {
	normalized := make([]models.CaseEscalationTarget, 0, len(targets))
	seen := make(map[string]struct{})
	for _, target := range targets {
		cleaned, ok := normalizeCaseEscalationTarget(target)
		if !ok {
			continue
		}
		key := caseEscalationTargetKey(cleaned)
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		normalized = append(normalized, cleaned)
	}
	targets = normalized

	hasPreferred := false
	for _, target := range targets {
		if strings.EqualFold(strings.TrimSpace(target.TargetSource), "inferred_contact") {
			continue
		}
		if strings.TrimSpace(target.Email) != "" ||
			strings.TrimSpace(target.Phone) != "" ||
			strings.TrimSpace(target.Website) != "" ||
			strings.TrimSpace(target.ContactURL) != "" {
			hasPreferred = true
			break
		}
	}
	if !hasPreferred {
		return targets
	}

	filtered := make([]models.CaseEscalationTarget, 0, len(targets))
	for _, target := range targets {
		if strings.EqualFold(strings.TrimSpace(target.TargetSource), "inferred_contact") {
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered
}

func extractHypothesisMatchTexts(hypotheses []models.ClusterIncidentHypothesis) []string {
	texts := make([]string, 0, len(hypotheses))
	for _, hypothesis := range hypotheses {
		if title := strings.TrimSpace(hypothesis.Title); title != "" {
			texts = append(texts, title)
		}
	}
	return texts
}

func queryBoolParam(c *gin.Context, key string) bool {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return false
	}
	parsed, err := strconv.ParseBool(raw)
	if err == nil {
		return parsed
	}
	switch strings.ToLower(raw) {
	case "1", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func preferredCaseMatchCandidate(candidates []models.CaseMatchCandidate) *models.CaseMatchCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if candidates[0].MatchScore < 0.58 {
		return nil
	}
	return &candidates[0]
}

func (h *Handlers) findCaseMatchCandidates(
	ctx context.Context,
	classification string,
	geometryJSON string,
	reports []models.ReportWithAnalysis,
	matchTexts []string,
) ([]models.CaseMatchCandidate, error) {
	clusterBounds := clusterBoundsForMatching(geometryJSON, reports)
	candidates, err := h.db.ListOpenCasesForMatching(ctx, classification, clusterBounds, 12)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	clusterSeqSet := make(map[int]struct{}, len(reports))
	clusterTextParts := make([]string, 0, len(matchTexts)+len(reports))
	clusterTextParts = append(clusterTextParts, matchTexts...)
	for _, report := range reports {
		clusterSeqSet[report.Report.Seq] = struct{}{}
		if analysis := preferredAnalysis(&report); analysis != nil {
			clusterTextParts = append(clusterTextParts, firstNonEmpty(analysis.Title, analysis.Summary, analysis.Description))
		}
	}
	clusterTokens := tokenizeForCluster(strings.Join(clusterTextParts, " "))
	clusterLat, clusterLng, clusterCenterOK := clusterCenterForMatching(clusterBounds, reports)

	matched := make([]models.CaseMatchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		score := 0.12
		reasons := make([]string, 0, 4)
		sharedCount := sharedCaseReports(clusterSeqSet, candidate.LinkedReportSeqs)
		candidate.SharedReportCount = sharedCount
		if sharedCount > 0 {
			denominator := maxInt(len(clusterSeqSet), len(candidate.LinkedReportSeqs))
			if denominator <= 0 {
				denominator = sharedCount
			}
			sharedRatio := float64(sharedCount) / float64(denominator)
			score += 0.56 * sharedRatio
			reasons = append(reasons, fmt.Sprintf("%d shared reports", sharedCount))
		}

		candidateBounds := boundsForCaseCandidate(candidate)
		if clusterBounds != nil && candidateBounds != nil {
			overlap := clusterBounds.IntersectionRatio(*candidateBounds)
			if overlap > 0 {
				score += math.Min(0.22, overlap*0.35)
				reasons = append(reasons, "overlapping area")
			}
		}

		if clusterCenterOK {
			if candidateLat, candidateLng, ok := caseCandidateCenter(candidate, candidateBounds); ok {
				distance := caseMatchDistanceMeters(clusterLat, clusterLng, candidateLat, candidateLng)
				switch {
				case distance <= 60:
					score += 0.18
					reasons = append(reasons, "same location")
				case distance <= 150:
					score += 0.12
					reasons = append(reasons, "nearby location")
				case distance <= 300:
					score += 0.06
				}
			}
		}

		titleSimilarity := tokenSetSimilarity(clusterTokens, tokenizeForCluster(candidate.Title+" "+candidate.Summary))
		if titleSimilarity > 0 {
			score += 0.16 * titleSimilarity
			if titleSimilarity >= 0.3 {
				reasons = append(reasons, "shared incident language")
			}
		}

		if candidate.ClusterCount > 1 {
			score += math.Min(0.05, float64(candidate.ClusterCount)*0.01)
		}

		if score < 0.35 && sharedCount == 0 {
			continue
		}
		candidate.MatchScore = math.Min(1, score)
		candidate.MatchReasons = dedupeStrings(reasons)
		matched = append(matched, candidate)
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].MatchScore == matched[j].MatchScore {
			return matched[i].UpdatedAt.After(matched[j].UpdatedAt)
		}
		return matched[i].MatchScore > matched[j].MatchScore
	})
	return matched, nil
}

func buildHypotheses(nodes []clusterNode, classification string) []models.ClusterIncidentHypothesis {
	if len(nodes) == 0 {
		return nil
	}
	parent := make([]int, len(nodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	edges := make(map[[2]int]clusterEdge)
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			score, rationale := similarityScore(nodes[i], nodes[j], classification)
			if score >= 0.55 {
				union(i, j)
				edges[[2]int{i, j}] = clusterEdge{score: score, rationale: rationale}
			}
		}
	}

	groups := map[int][]int{}
	for i := range nodes {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	hypotheses := make([]models.ClusterIncidentHypothesis, 0, len(groups))
	for _, indexes := range groups {
		sort.Slice(indexes, func(i, j int) bool {
			return preferredSeverity(&nodes[indexes[i]].report) > preferredSeverity(&nodes[indexes[j]].report)
		})
		rep := nodes[indexes[0]]
		reportSeqs := make([]int, 0, len(indexes))
		rationaleSet := map[string]struct{}{}
		var maxSeverity float64
		for _, idx := range indexes {
			reportSeqs = append(reportSeqs, nodes[idx].report.Report.Seq)
			sev := preferredSeverity(&nodes[idx].report)
			if sev > maxSeverity {
				maxSeverity = sev
			}
		}
		for i := 0; i < len(indexes); i++ {
			for j := i + 1; j < len(indexes); j++ {
				key := [2]int{min(indexes[i], indexes[j]), max(indexes[i], indexes[j])}
				if edge, ok := edges[key]; ok {
					for _, reason := range edge.rationale {
						rationaleSet[reason] = struct{}{}
					}
				}
			}
		}
		rationale := make([]string, 0, len(rationaleSet))
		for reason := range rationaleSet {
			rationale = append(rationale, reason)
		}
		sort.Strings(rationale)
		title := rep.titleText
		if title == "" {
			title = inferHypothesisTitle(indexes, nodes)
		}
		hashInput := fmt.Sprintf("%s:%v", title, reportSeqs)
		sum := sha1.Sum([]byte(hashInput))
		hypotheses = append(hypotheses, models.ClusterIncidentHypothesis{
			HypothesisID:            "hyp_" + hex.EncodeToString(sum[:6]),
			Title:                   title,
			Classification:          firstNonEmpty(rep.analysis.Classification, classification, "physical"),
			RepresentativeReportSeq: rep.report.Report.Seq,
			ReportSeqs:              reportSeqs,
			ReportCount:             len(indexes),
			Confidence:              confidenceFromCluster(indexes, maxSeverity),
			SeverityScore:           maxSeverity,
			UrgencyScore:            urgencyFromCluster(indexes, nodes),
			Rationale:               rationale,
		})
	}

	sort.Slice(hypotheses, func(i, j int) bool {
		if hypotheses[i].UrgencyScore == hypotheses[j].UrgencyScore {
			return hypotheses[i].SeverityScore > hypotheses[j].SeverityScore
		}
		return hypotheses[i].UrgencyScore > hypotheses[j].UrgencyScore
	})
	return hypotheses
}

func similarityScore(a, b clusterNode, classification string) (float64, []string) {
	score := 0.0
	var rationale []string
	if firstNonEmpty(a.analysis.Classification, classification, "physical") == firstNonEmpty(b.analysis.Classification, classification, "physical") {
		score += 0.2
		rationale = append(rationale, "same classification")
	}
	sevDelta := math.Abs(a.analysis.SeverityLevel - b.analysis.SeverityLevel)
	if sevDelta <= 0.2 {
		score += 0.1
		rationale = append(rationale, "similar severity")
	}
	jaccard := tokenJaccard(a.tokenSet, b.tokenSet)
	if jaccard > 0.15 {
		score += 0.4
		rationale = append(rationale, "shared incident language")
	}
	if haversineKm(a.report.Report.Latitude, a.report.Report.Longitude, b.report.Report.Latitude, b.report.Report.Longitude) < 0.1 {
		score += 0.2
		rationale = append(rationale, "same physical location")
	}
	if a.analysis.BrandDisplayName != "" && strings.EqualFold(a.analysis.BrandDisplayName, b.analysis.BrandDisplayName) {
		score += 0.1
		rationale = append(rationale, "same organization")
	}
	return score, rationale
}

func preferredAnalysis(report *models.ReportWithAnalysis) *models.ReportAnalysis {
	if report == nil || len(report.Analysis) == 0 {
		return nil
	}
	for i := range report.Analysis {
		if strings.EqualFold(report.Analysis[i].Language, "en") {
			return &report.Analysis[i]
		}
	}
	return &report.Analysis[0]
}

func preferredClassification(report *models.ReportWithAnalysis) string {
	analysis := preferredAnalysis(report)
	if analysis == nil {
		return ""
	}
	return analysis.Classification
}

func preferredSeverity(report *models.ReportWithAnalysis) float64 {
	analysis := preferredAnalysis(report)
	if analysis == nil {
		return 0
	}
	return analysis.SeverityLevel
}

func reportSeqs(reports []models.ReportWithAnalysis) []int {
	out := make([]int, 0, len(reports))
	for _, report := range reports {
		out = append(out, report.Report.Seq)
	}
	return out
}

func tokenizeForCluster(input string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		if _, skip := clusterStopWords[field]; skip {
			continue
		}
		out[field] = struct{}{}
	}
	return out
}

func tokenJaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersect := 0
	union := len(a)
	for token := range b {
		if _, ok := a[token]; ok {
			intersect++
		} else {
			union++
		}
	}
	return float64(intersect) / float64(union)
}

func urgencyFromCluster(indexes []int, nodes []clusterNode) float64 {
	var maxSeverity float64
	var latestTs int64
	for _, idx := range indexes {
		sev := preferredSeverity(&nodes[idx].report)
		if sev > maxSeverity {
			maxSeverity = sev
		}
		ts := nodes[idx].report.Report.Timestamp.Unix()
		if ts > latestTs {
			latestTs = ts
		}
	}
	recencyBoost := 0.0
	if latestTs > 0 && timeSinceHours(latestTs) <= 168 {
		recencyBoost = 0.1
	}
	sizeBoost := math.Min(float64(len(indexes))*0.08, 0.3)
	return math.Min(maxSeverity+sizeBoost+recencyBoost, 1.0)
}

func confidenceFromCluster(indexes []int, maxSeverity float64) float64 {
	return math.Min(0.45+math.Min(float64(len(indexes))*0.08, 0.3)+maxSeverity*0.2, 0.98)
}

func inferHypothesisTitle(indexes []int, nodes []clusterNode) string {
	freq := map[string]int{}
	for _, idx := range indexes {
		for token := range nodes[idx].tokenSet {
			freq[token]++
		}
	}
	type tokenCount struct {
		Token string
		Count int
	}
	var ranked []tokenCount
	for token, count := range freq {
		ranked = append(ranked, tokenCount{Token: token, Count: count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Count == ranked[j].Count {
			return ranked[i].Token < ranked[j].Token
		}
		return ranked[i].Count > ranked[j].Count
	})
	parts := make([]string, 0, 3)
	for _, token := range ranked {
		parts = append(parts, strings.Title(token.Token))
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "Clustered Incident"
	}
	return strings.Join(parts, " ")
}

func timeSinceHours(unixTs int64) float64 {
	return math.Abs(float64(time.Now().Unix()-unixTs)) / 3600.0
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371.0
	dLat := degreesToRadians(lat2 - lat1)
	dLon := degreesToRadians(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(degreesToRadians(lat1))*math.Cos(degreesToRadians(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadius * c
}

func degreesToRadians(v float64) float64 { return v * math.Pi / 180.0 }

func slugify(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var b strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func ensureUniqueSlug(base string) string {
	if base == "" {
		base = "case"
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s-%d", base, time.Now().UnixNano())))
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(sum[:4]))
}

func clusterBoundsForMatching(geometryJSON string, reports []models.ReportWithAnalysis) *geojsonx.Bounds {
	if strings.TrimSpace(geometryJSON) != "" {
		if bounds, err := geojsonx.BoundsFromJSON(geometryJSON); err == nil && bounds != nil && bounds.Valid() {
			return bounds
		}
	}
	var bounds *geojsonx.Bounds
	for _, report := range reports {
		lat := report.Report.Latitude
		lng := report.Report.Longitude
		if lat == 0 && lng == 0 {
			continue
		}
		if bounds == nil {
			copyBounds := geojsonx.NewBounds(lng, lat, lng, lat)
			bounds = &copyBounds
			continue
		}
		bounds.West = math.Min(bounds.West, lng)
		bounds.South = math.Min(bounds.South, lat)
		bounds.East = math.Max(bounds.East, lng)
		bounds.North = math.Max(bounds.North, lat)
	}
	return bounds
}

func clusterCenterForMatching(bounds *geojsonx.Bounds, reports []models.ReportWithAnalysis) (float64, float64, bool) {
	if bounds != nil && bounds.Valid() {
		lat, lng := bounds.Center()
		return lat, lng, true
	}
	var totalLat float64
	var totalLng float64
	var count int
	for _, report := range reports {
		if report.Report.Latitude == 0 && report.Report.Longitude == 0 {
			continue
		}
		totalLat += report.Report.Latitude
		totalLng += report.Report.Longitude
		count++
	}
	if count == 0 {
		return 0, 0, false
	}
	return totalLat / float64(count), totalLng / float64(count), true
}

func boundsForCaseCandidate(candidate models.CaseMatchCandidate) *geojsonx.Bounds {
	if bounds, err := geojsonx.ParseBoundsJSON(candidate.AggregateBBoxJSON); err == nil && bounds != nil && bounds.Valid() {
		return bounds
	}
	if bounds, err := geojsonx.BoundsFromJSON(candidate.AggregateGeometryJSON); err == nil && bounds != nil && bounds.Valid() {
		return bounds
	}
	if bounds, err := geojsonx.BoundsFromJSON(candidate.GeometryJSON); err == nil && bounds != nil && bounds.Valid() {
		return bounds
	}
	return nil
}

func caseCandidateCenter(candidate models.CaseMatchCandidate, bounds *geojsonx.Bounds) (float64, float64, bool) {
	if candidate.AnchorLat != nil && candidate.AnchorLng != nil {
		return *candidate.AnchorLat, *candidate.AnchorLng, true
	}
	if bounds == nil || !bounds.Valid() {
		return 0, 0, false
	}
	lat, lng := bounds.Center()
	return lat, lng, true
}

func sharedCaseReports(clusterSeqSet map[int]struct{}, caseSeqs []int) int {
	if len(clusterSeqSet) == 0 || len(caseSeqs) == 0 {
		return 0
	}
	count := 0
	for _, seq := range caseSeqs {
		if _, ok := clusterSeqSet[seq]; ok {
			count++
		}
	}
	return count
}

func tokenSetSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for token := range a {
		if _, ok := b[token]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(a) + len(b) - intersection
	if union <= 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func caseMatchDistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	return haversineKm(lat1, lon1, lat2, lon2) * 1000
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func intPtr(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
