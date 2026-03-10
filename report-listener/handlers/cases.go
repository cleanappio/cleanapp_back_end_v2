package handlers

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
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

	suggestedTargets, err := h.db.SuggestEscalationTargetsByGeometry(c.Request.Context(), string(geometryJSON), reportSeqs(reports), 8)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to derive escalation targets"})
		return
	}

	response := analyzeClusterReports(reports, classification, "geometry", 0, json.RawMessage(geometryJSON), suggestedTargets)
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

	suggestedTargets, err := h.db.SuggestEscalationTargetsByGeometry(c.Request.Context(), "", reportSeqs(reports), 8)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to derive escalation targets"})
		return
	}

	response := analyzeClusterReports(reports, classification, "seed_report", req.Seq, nil, suggestedTargets)
	c.JSON(http.StatusOK, response)
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
			Email:           target.Email,
			Phone:           target.Phone,
			TargetSource:    target.TargetSource,
			ConfidenceScore: target.ConfidenceScore,
			Rationale:       target.Rationale,
		})
	}
	if len(targets) == 0 {
		autoTargets, err := h.db.SuggestEscalationTargetsByGeometry(c.Request.Context(), geometryJSON, req.ReportSeqs, 8)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to suggest escalation targets"})
			return
		}
		targets = autoTargets
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

	auditPayload := gin.H{
		"report_seqs":    req.ReportSeqs,
		"anchor_seq":     req.AnchorReportSeq,
		"cluster_ids":    clusterIDs,
		"target_count":   len(targets),
		"classification": caseRecord.Classification,
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
	c.JSON(http.StatusCreated, detail)
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
