package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"report-listener/database"

	"github.com/gin-gonic/gin"
)

type IntelligenceIntent string

const (
	IntentComplaintsSummary IntelligenceIntent = "complaints_summary"
	IntentFixFirst          IntelligenceIntent = "fix_first"
	IntentSecurityRisks     IntelligenceIntent = "security_risks"
	IntentTrends            IntelligenceIntent = "trends"
	IntentSampleReports     IntelligenceIntent = "sample_reports"
	IntentReportsLastWeek   IntelligenceIntent = "reports_last_week"
	IntentGenericInScope    IntelligenceIntent = "generic_in_scope"
	IntentOutOfScope        IntelligenceIntent = "oos"
)

type IntelligenceResponseMode string

const (
	ResponseModeFull        IntelligenceResponseMode = "full"
	ResponseModePartialFree IntelligenceResponseMode = "partial_free"
	ResponseModeBlocked     IntelligenceResponseMode = "blocked"
)

type IntelligenceQueryRequest struct {
	OrgID            string  `json:"org_id"`
	Question         string  `json:"question"`
	SessionID        string  `json:"session_id"`
	UserID           *string `json:"user_id"`
	SubscriptionTier string  `json:"subscription_tier"`
	QualityMode      string  `json:"quality_mode"`
}

type IntelligenceEvidenceItem struct {
	Seq       int    `json:"seq"`
	Title     string `json:"title"`
	Permalink string `json:"permalink"`
}

type IntelligenceCategoryAggregate struct {
	Name     string  `json:"name"`
	Count    int     `json:"count"`
	SharePct float64 `json:"share_pct"`
}

type IntelligenceSeverityAggregate struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type IntelligenceAggregates struct {
	TotalReports       int                             `json:"total_reports"`
	ReportsThisMonth   int                             `json:"reports_this_month"`
	ReportsLast30Days  int                             `json:"reports_last_30_days"`
	ReportsLast7Days   int                             `json:"reports_last_7_days"`
	ReportsPrev7Days   int                             `json:"reports_prev_7_days"`
	GrowthLast7VsPrev7 float64                         `json:"growth_last_7_vs_prev_7"`
	TopCategories      []IntelligenceCategoryAggregate `json:"top_categories,omitempty"`
	SeverityBreakdown  []IntelligenceSeverityAggregate `json:"severity_breakdown,omitempty"`
}

type IntelligenceExample struct {
	ID        int     `json:"id"`
	CreatedAt string  `json:"created_at"`
	Channel   string  `json:"channel"`
	Severity  float64 `json:"severity"`
	Category  string  `json:"category"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet"`
	URL       string  `json:"url,omitempty"`
}

type IntelligenceCitation struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type IntelligenceResponseData struct {
	Aggregates *IntelligenceAggregates `json:"aggregates,omitempty"`
	Examples   []IntelligenceExample   `json:"examples,omitempty"`
	Citations  []IntelligenceCitation  `json:"citations,omitempty"`
}

type IntelligenceUpsell struct {
	Text string `json:"text"`
	CTA  string `json:"cta"`
}

type IntelligenceQueryResponse struct {
	Mode           IntelligenceResponseMode `json:"mode"`
	AnswerMarkdown string                   `json:"answer_markdown"`
	Data           IntelligenceResponseData `json:"data"`
	Upsell         *IntelligenceUpsell      `json:"upsell,omitempty"`

	// Legacy fields for backward compatibility.
	Answer           string                     `json:"answer"`
	ReportsAnalyzed  int                        `json:"reports_analyzed"`
	PaywallTriggered bool                       `json:"paywall_triggered"`
	EvidenceCount    int                        `json:"evidence_count,omitempty"`
	Evidence         []IntelligenceEvidenceItem `json:"evidence,omitempty"`
	SuggestedPrompts []string                   `json:"suggested_prompts,omitempty"`
}

type intelligenceComputedResult struct {
	Mode             IntelligenceResponseMode
	AnswerMarkdown   string
	Data             IntelligenceResponseData
	Upsell           *IntelligenceUpsell
	SuggestedPrompts []string
	PaywallTriggered bool
}

type entitlementOptions struct {
	ExportRequested bool
}

var (
	exportPromptRegex = regexp.MustCompile(`(?i)(show all reports|export|csv|json|pdf|download|full database|full dataset|dump|list all|raw report text|exhaustive list)`)
	oosGlobalRegex    = regexp.MustCompile(`(?i)\b(eth|btc|bitcoin|ethereum|doge|crypto|price|weather|sports|nba|nfl|mlb|nhl|epl|stock|stocks|nasdaq|dow|gold price|president|prime minister|celebrity|movie times|lottery|horoscope)\b`)
	issueTermsRegex   = regexp.MustCompile(`(?i)\b(bug|bugs|error|errors|issue|issues|incident|incidents|outage|outages|login|password|security|spam|phishing|abuse|crash|latency|downtime|complain|complaints|ux|ui|fix|priority|risk|risks|report|reports|alert|alerts|feedback|support|billing|feature|trend|trends|sample|examples|dataset|fehler|problem|probleme|ausfall|sicherheit|risiko|risiken|bericht|berichte|beschwerde|beschwerden|problema|problemas|incidencia|incidencias|seguridad|riesgo|riesgos|incidentes|rapport|rapports|sécurité|risque|risques|panne|pannes|problème|problèmes|ошибк|проблем|сбо|инцидент|безопасност|отчет|отчёт|жалоб|риск|риски|otchet|otchety|otchetov)\b`)
	emailRegex        = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	phoneRegex        = regexp.MustCompile(`\+?[0-9][0-9\-\s]{7,}[0-9]`)
	idRegex           = regexp.MustCompile(`\b[0-9]{9,}\b`)
)

var promptSuggestions = []string{
	"What do users complain about most?",
	"What should we fix first?",
	"Are there any security risks?",
}

const freeTierUpsellText = "Pro unlocks deeper drill-down, unredacted detail, exports, and unlimited intelligence queries."

func (h *Handlers) QueryIntelligence(c *gin.Context) {
	var req IntelligenceQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json payload"})
		return
	}

	orgID := strings.ToLower(strings.TrimSpace(req.OrgID))
	question := strings.TrimSpace(req.Question)
	sessionID := strings.TrimSpace(req.SessionID)
	tier := strings.ToLower(strings.TrimSpace(req.SubscriptionTier))
	if tier == "" {
		tier = "anonymous"
	}
	qualityMode := normalizeQualityMode(req.QualityMode)
	signedIn := req.UserID != nil && strings.TrimSpace(*req.UserID) != ""
	isPro := tier == "pro"

	if orgID == "" || question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id and question are required"})
		return
	}
	if !isPro && !signedIn && sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required for non-pro usage"})
		return
	}

	totalCount, highCount, mediumCount := h.getIntelligenceCounts(c.Request.Context(), orgID)
	if !isPro && !signedIn {
		allowed, turnsUsed, usageErr := h.db.GetAndIncrementIntelligenceUsage(
			c.Request.Context(),
			sessionID,
			h.cfg.IntelligenceFreeTierMaxTurn,
			24*time.Hour,
		)
		if usageErr != nil {
			log.Printf("intelligence usage enforcement failed: %v", usageErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enforce usage policy"})
			return
		}
		if !allowed {
			blocked := intelligenceComputedResult{
				Mode:           ResponseModeBlocked,
				AnswerMarkdown: fmt.Sprintf("You’ve reached the free intelligence limit for this session (%d turns).", turnsUsed),
				Upsell: &IntelligenceUpsell{
					Text: freeTierUpsellText,
					CTA:  "Upgrade to Pro",
				},
				PaywallTriggered: true,
			}
			resp := h.toAPIResponse(blocked, totalCount)
			h.logIntelligenceMetrics(orgID, tier, qualityMode, IntentGenericInScope, resp)
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	intent := classifyIntelligenceIntent(orgID, question)
	if intent == IntentOutOfScope {
		out := intelligenceComputedResult{
			Mode: ResponseModeBlocked,
			AnswerMarkdown: fmt.Sprintf(
				"I’m Live CleanApp Intelligence for %s. I answer questions about %s issues and reports (bugs, outages, UX, security). I can’t answer that here.",
				orgID,
				orgID,
			),
			SuggestedPrompts: cloneSuggestedPrompts(),
		}
		resp := h.toAPIResponse(out, totalCount)
		h.logIntelligenceMetrics(orgID, tier, qualityMode, intent, resp)
		c.JSON(http.StatusOK, resp)
		return
	}

	excludedIDs := make([]int, 0)
	if sessionID != "" {
		stateCtx, cancel := context.WithTimeout(c.Request.Context(), 800*time.Millisecond)
		excluded, err := h.db.GetLastReportIDsForSession(stateCtx, sessionID)
		cancel()
		if err != nil {
			log.Printf("intelligence session state read failed org=%s: %v", orgID, err)
		} else {
			excludedIDs = excluded
		}
	}

	intelCtx, priorities, err := h.loadIntentContext(c.Request.Context(), orgID, question, intent, excludedIDs)
	if err != nil {
		log.Printf("intelligence context failed for org=%s intent=%s: %v", orgID, intent, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load intelligence context"})
		return
	}

	if totalCount > 0 {
		intelCtx.ReportsAnalyzed = totalCount
	}
	if highCount > 0 {
		intelCtx.HighPriorityCount = highCount
	}
	if mediumCount > 0 {
		intelCtx.MediumPriorityCount = mediumCount
	}

	baseURL := h.cfg.IntelligenceBaseURL
	computed := h.buildComputedResult(intent, intelCtx, question, baseURL, priorities)

	if h.geminiClient != nil && h.geminiClient.Enabled() && isPro {
		computed.AnswerMarkdown = h.maybeRefineAnswerWithGemini(c.Request.Context(), computed.AnswerMarkdown, computed.Data, question, intent, tier, qualityMode)
	}

	computed = applyEntitlements(tier, computed, entitlementOptions{ExportRequested: exportPromptRegex.MatchString(question)})
	computed = h.ensureConcreteNonBlockedAnswer(computed, intelCtx)

	resp := h.toAPIResponse(computed, intelCtx.ReportsAnalyzed)

	if sessionID != "" && len(resp.Evidence) > 0 {
		used := make([]int, 0, len(resp.Evidence))
		for _, item := range resp.Evidence {
			if item.Seq > 0 {
				used = append(used, item.Seq)
			}
		}
		stateCtx, cancel := context.WithTimeout(c.Request.Context(), 900*time.Millisecond)
		saveErr := h.db.SaveLastReportIDsForSession(stateCtx, sessionID, used, 24*time.Hour)
		cancel()
		if saveErr != nil {
			log.Printf("intelligence session state write failed org=%s: %v", orgID, saveErr)
		}
	}

	h.logIntelligenceMetrics(orgID, tier, qualityMode, intent, resp)
	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) ensureConcreteNonBlockedAnswer(result intelligenceComputedResult, ctx *database.IntelligenceContext) intelligenceComputedResult {
	if result.Mode == ResponseModeBlocked {
		return result
	}
	if hasConcreteAnswer(result.AnswerMarkdown, result.Data.Examples) {
		return result
	}

	result.AnswerMarkdown = fmt.Sprintf(
		"We analyzed %d reports for %s. Last 30 days: %d reports; last 7 days: %d (vs %d previous week, %.1f%%).",
		ctx.ReportsAnalyzed,
		ctx.OrgID,
		ctx.ReportsLast30Days,
		ctx.ReportsLast7Days,
		ctx.ReportsPrev7Days,
		ctx.GrowthLast7VsPrev7,
	)
	return result
}

func (h *Handlers) maybeRefineAnswerWithGemini(
	ctx context.Context,
	baseAnswer string,
	data IntelligenceResponseData,
	question string,
	intent IntelligenceIntent,
	tier string,
	qualityMode string,
) string {
	if strings.TrimSpace(baseAnswer) == "" {
		return baseAnswer
	}

	systemPrompt := `You are rewriting a CleanApp Intelligence answer.
- Keep all numbers, facts, and report references accurate.
- Do not invent claims or links.
- Keep it concise and executive-grade.
- Do not append any upsell or CTA lines.
- Keep language aligned with the user's language.`

	var b strings.Builder
	b.WriteString("Intent: ")
	b.WriteString(string(intent))
	b.WriteString("\nTier: ")
	b.WriteString(tier)
	b.WriteString("\nQuestion: ")
	b.WriteString(question)
	b.WriteString("\n\nCurrent answer:\n")
	b.WriteString(baseAnswer)
	if data.Aggregates != nil {
		b.WriteString("\n\nGrounding aggregates:\n")
		b.WriteString(fmt.Sprintf("total=%d, last30=%d, last7=%d, prev7=%d, growth=%.1f%%\n",
			data.Aggregates.TotalReports,
			data.Aggregates.ReportsLast30Days,
			data.Aggregates.ReportsLast7Days,
			data.Aggregates.ReportsPrev7Days,
			data.Aggregates.GrowthLast7VsPrev7,
		))
	}
	if len(data.Examples) > 0 {
		b.WriteString("Examples (do not invent beyond these):\n")
		for _, ex := range data.Examples {
			b.WriteString(fmt.Sprintf("- Report #%d | %s | sev=%.2f | %s\n", ex.ID, ex.Title, ex.Severity, ex.URL))
		}
	}

	queryCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	refined, err := h.geminiClient.GenerateAnswerWithQuality(queryCtx, systemPrompt, b.String(), qualityMode)
	if err != nil {
		return baseAnswer
	}
	refined = strings.TrimSpace(refined)
	if !hasConcreteAnswer(refined, data.Examples) {
		return baseAnswer
	}
	if strings.Contains(strings.ToLower(refined), "substantial corpus") {
		return baseAnswer
	}
	return refined
}

func (h *Handlers) toAPIResponse(result intelligenceComputedResult, reportsAnalyzed int) IntelligenceQueryResponse {
	answer := strings.TrimSpace(result.AnswerMarkdown)
	if answer == "" {
		answer = "I couldn’t analyze that right now."
	}

	evidence := make([]IntelligenceEvidenceItem, 0, len(result.Data.Examples))
	for _, ex := range result.Data.Examples {
		if ex.ID <= 0 {
			continue
		}
		evidence = append(evidence, IntelligenceEvidenceItem{
			Seq:       ex.ID,
			Title:     strings.TrimSpace(ex.Title),
			Permalink: strings.TrimSpace(ex.URL),
		})
	}

	return IntelligenceQueryResponse{
		Mode:             result.Mode,
		AnswerMarkdown:   answer,
		Data:             result.Data,
		Upsell:           result.Upsell,
		Answer:           answer,
		ReportsAnalyzed:  reportsAnalyzed,
		PaywallTriggered: result.PaywallTriggered,
		EvidenceCount:    len(evidence),
		Evidence:         evidence,
		SuggestedPrompts: result.SuggestedPrompts,
	}
}

func (h *Handlers) buildComputedResult(
	intent IntelligenceIntent,
	ctx *database.IntelligenceContext,
	question string,
	baseURL string,
	priorities []database.FixPriority,
) intelligenceComputedResult {
	agg := buildAggregatesPayload(ctx)
	examples := h.buildExamplesForIntent(intent, ctx, baseURL, priorities, 8)
	citations := buildCitations(ctx, examples)

	result := intelligenceComputedResult{
		Mode: ResponseModeFull,
		Data: IntelligenceResponseData{
			Aggregates: agg,
			Examples:   examples,
			Citations:  citations,
		},
	}

	switch intent {
	case IntentComplaintsSummary:
		result.AnswerMarkdown = buildComplaintsAnswer(ctx, agg, examples)
	case IntentFixFirst:
		result.AnswerMarkdown = buildFixFirstAnswer(ctx, priorities, examples)
	case IntentSecurityRisks:
		result.AnswerMarkdown = buildSecurityAnswer(ctx, agg, examples)
	case IntentTrends:
		result.AnswerMarkdown = buildTrendsAnswer(ctx, agg, examples)
	case IntentSampleReports:
		result.AnswerMarkdown = buildSampleReportsAnswer(ctx, agg, examples)
	case IntentReportsLastWeek:
		result.AnswerMarkdown = buildLastWeekAnswer(ctx, agg, examples)
	default:
		result.AnswerMarkdown = buildGenericInScopeAnswer(ctx, question, agg, examples)
	}

	return result
}

func buildAggregatesPayload(ctx *database.IntelligenceContext) *IntelligenceAggregates {
	if ctx == nil {
		return nil
	}

	topCategories := make([]IntelligenceCategoryAggregate, 0, len(ctx.TopClassifications))
	for _, item := range ctx.TopClassifications {
		if strings.TrimSpace(item.Name) == "" || item.Count <= 0 {
			continue
		}
		share := 0.0
		if ctx.ReportsAnalyzed > 0 {
			share = (float64(item.Count) / float64(ctx.ReportsAnalyzed)) * 100.0
		}
		topCategories = append(topCategories, IntelligenceCategoryAggregate{
			Name:     item.Name,
			Count:    item.Count,
			SharePct: share,
		})
	}

	severity := []IntelligenceSeverityAggregate{
		{Name: "critical", Count: ctx.SeverityDistribution.Critical},
		{Name: "high", Count: ctx.SeverityDistribution.High},
		{Name: "medium", Count: ctx.SeverityDistribution.Medium},
		{Name: "low", Count: ctx.SeverityDistribution.Low},
	}

	return &IntelligenceAggregates{
		TotalReports:       ctx.ReportsAnalyzed,
		ReportsThisMonth:   ctx.ReportsThisMonth,
		ReportsLast30Days:  ctx.ReportsLast30Days,
		ReportsLast7Days:   ctx.ReportsLast7Days,
		ReportsPrev7Days:   ctx.ReportsPrev7Days,
		GrowthLast7VsPrev7: ctx.GrowthLast7VsPrev7,
		TopCategories:      topCategories,
		SeverityBreakdown:  severity,
	}
}

func (h *Handlers) buildExamplesForIntent(
	intent IntelligenceIntent,
	ctx *database.IntelligenceContext,
	baseURL string,
	priorities []database.FixPriority,
	max int,
) []IntelligenceExample {
	if max <= 0 {
		max = 5
	}

	pick := make([]database.ReportSnippet, 0, max)
	switch intent {
	case IntentFixFirst:
		for _, p := range priorities {
			pick = append(pick, p.Reports...)
		}
		if len(pick) == 0 {
			pick = append(pick, collectEvidenceReports(ctx)...)
		}
	case IntentSecurityRisks:
		for _, r := range collectEvidenceReports(ctx) {
			if isSecurityReportSnippet(r) {
				pick = append(pick, r)
			}
		}
		if len(pick) == 0 {
			pick = append(pick, collectEvidenceReports(ctx)...)
		}
	case IntentTrends, IntentReportsLastWeek:
		pick = append(pick, collectEvidenceReports(ctx)...)
		sort.SliceStable(pick, func(i, j int) bool { return pick[i].UpdatedAt.After(pick[j].UpdatedAt) })
	default:
		pick = append(pick, collectEvidenceReports(ctx)...)
	}

	preferredLanguage := preferredEvidenceLanguageForOrg(ctx.OrgID)
	preferredBySeq := make(map[int]database.ReportSnippet, max)
	resolveSnippet := func(r database.ReportSnippet) database.ReportSnippet {
		if h.db == nil || preferredLanguage == "" || r.Seq <= 0 {
			return r
		}
		if cached, ok := preferredBySeq[r.Seq]; ok {
			return cached
		}
		qctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		defer cancel()
		preferred, err := h.db.GetPreferredReportSnippetBySeq(qctx, ctx.OrgID, r.Seq, preferredLanguage)
		if err != nil {
			log.Printf("intelligence_preferred_language_lookup_failed org=%s seq=%d err=%v", ctx.OrgID, r.Seq, err)
			preferredBySeq[r.Seq] = r
			return r
		}
		if preferred == nil {
			preferredBySeq[r.Seq] = r
			return r
		}
		preferredBySeq[r.Seq] = *preferred
		return *preferred
	}

	examples := make([]IntelligenceExample, 0, max)
	seen := make(map[int]struct{}, max)
	for _, r := range pick {
		if len(examples) >= max {
			break
		}
		if r.Seq <= 0 {
			continue
		}
		if _, ok := seen[r.Seq]; ok {
			continue
		}
		seen[r.Seq] = struct{}{}
		examples = append(examples, reportSnippetToExample(resolveSnippet(r), baseURL, ctx.OrgID))
	}

	// Safety fallback: always provide representative examples for answer quality.
	if len(examples) == 0 {
		fallbackCtx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
		defer cancel()

		fallbackRows, err := h.db.GetFallbackReportSnippets(fallbackCtx, ctx.OrgID, max)
		if err != nil {
			log.Printf("intelligence_examples_fallback_failed org=%s err=%v", ctx.OrgID, err)
			return examples
		}
		for _, r := range fallbackRows {
			if len(examples) >= max {
				break
			}
			if r.Seq <= 0 {
				continue
			}
			if _, ok := seen[r.Seq]; ok {
				continue
			}
			seen[r.Seq] = struct{}{}
			examples = append(examples, reportSnippetToExample(resolveSnippet(r), baseURL, ctx.OrgID))
		}
	}
	return examples
}

func preferredEvidenceLanguageForOrg(orgID string) string {
	org := strings.ToLower(strings.TrimSpace(orgID))
	if org == "" {
		return ""
	}
	// Keep native-language rendering for Montenegro-specific dashboard context.
	if strings.Contains(org, "montenegro") {
		return ""
	}
	return "en"
}

func reportSnippetToExample(r database.ReportSnippet, baseURL, orgID string) IntelligenceExample {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = fmt.Sprintf("Report #%d", r.Seq)
	}
	snippet := strings.TrimSpace(r.Summary)
	if snippet == "" {
		snippet = "No summary available."
	}
	channel := strings.TrimSpace(r.Classification)
	if channel == "" {
		channel = "unknown"
	}
	return IntelligenceExample{
		ID:        r.Seq,
		CreatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
		Channel:   channel,
		Severity:  r.SeverityLevel,
		Category:  channel,
		Title:     truncateText(title, 140),
		Snippet:   truncateText(snippet, 220),
		URL:       buildReportPermalink(baseURL, orgID, r.Seq),
	}
}

func buildCitations(ctx *database.IntelligenceContext, examples []IntelligenceExample) []IntelligenceCitation {
	citations := make([]IntelligenceCitation, 0, 8)
	citations = append(citations,
		IntelligenceCitation{Label: "Total reports", Value: strconv.Itoa(ctx.ReportsAnalyzed)},
		IntelligenceCitation{Label: "Last 7 days", Value: strconv.Itoa(ctx.ReportsLast7Days)},
		IntelligenceCitation{Label: "Previous 7 days", Value: strconv.Itoa(ctx.ReportsPrev7Days)},
	)
	if len(ctx.TopClassifications) > 0 {
		citations = append(citations, IntelligenceCitation{
			Label: "Top category",
			Value: fmt.Sprintf("%s (%d)", ctx.TopClassifications[0].Name, ctx.TopClassifications[0].Count),
		})
	}
	for _, ex := range examples {
		if ex.ID <= 0 {
			continue
		}
		citations = append(citations, IntelligenceCitation{
			Label: fmt.Sprintf("Report #%d", ex.ID),
			Value: ex.URL,
		})
		if len(citations) >= 8 {
			break
		}
	}
	return citations
}

func buildComplaintsAnswer(ctx *database.IntelligenceContext, agg *IntelligenceAggregates, examples []IntelligenceExample) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s has %d valid reports in CleanApp. In the last 30 days, %d reports were logged; last 7 days were %d versus %d in the prior week (%+.1f%%).\n\n", ctx.OrgID, agg.TotalReports, agg.ReportsLast30Days, agg.ReportsLast7Days, agg.ReportsPrev7Days, agg.GrowthLast7VsPrev7))
	if len(agg.TopCategories) > 0 {
		b.WriteString("Top issue categories right now:\n")
		for i, c := range agg.TopCategories {
			if i >= 3 {
				break
			}
			b.WriteString(fmt.Sprintf("- %s: %d reports (%.1f%% of total)\n", c.Name, c.Count, c.SharePct))
		}
	}
	b.WriteString(fmt.Sprintf("Severity mix: critical %d, high %d, medium %d, low %d.\n", ctx.SeverityDistribution.Critical, ctx.SeverityDistribution.High, ctx.SeverityDistribution.Medium, ctx.SeverityDistribution.Low))
	appendExampleBullets(&b, examples, 3)
	return strings.TrimSpace(b.String())
}

func buildFixFirstAnswer(ctx *database.IntelligenceContext, priorities []database.FixPriority, examples []IntelligenceExample) string {
	var b strings.Builder
	if len(priorities) == 0 {
		b.WriteString(fmt.Sprintf("%s has %d valid reports. The highest-value next step is to prioritize the top recurring issue cluster and reduce recurrence over the next 14 days.\n", ctx.OrgID, ctx.ReportsAnalyzed))
		appendExampleBullets(&b, examples, 3)
		return strings.TrimSpace(b.String())
	}

	b.WriteString("Here is the highest-impact fix sequence right now:\n")
	for i, p := range priorities {
		if i >= 3 {
			break
		}
		line := fmt.Sprintf("- Priority %d: %s (score %.2f; freq %d; avg severity %.2f; recent7 %d)", i+1, p.Issue, p.Score, p.Frequency, p.AvgSeverity, p.Recent7Days)
		if len(p.Reports) > 0 {
			line += fmt.Sprintf(" (Report #%d)", p.Reports[0].Seq)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nQuick win (48h): patch the top-severity recurring issue and publish a status update tied to linked reports.\n")
	b.WriteString("Big rock (2-4w): remove root-cause recurrence for Priority 1 and track week-over-week incident reduction.\n")
	appendExampleBullets(&b, examples, 3)
	return strings.TrimSpace(b.String())
}

func buildSecurityAnswer(ctx *database.IntelligenceContext, agg *IntelligenceAggregates, examples []IntelligenceExample) string {
	securityExamples := make([]IntelligenceExample, 0, len(examples))
	for _, ex := range examples {
		if isSecurityText(ex.Title + " " + ex.Snippet) {
			securityExamples = append(securityExamples, ex)
		}
	}
	if len(securityExamples) == 0 {
		securityExamples = examples
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Security posture snapshot for %s: high+critical signals total %d (%d critical, %d high). Last 30 days: %d reports; week-over-week trend: %d vs %d (%+.1f%%).\n",
		ctx.OrgID,
		ctx.SeverityDistribution.Critical+ctx.SeverityDistribution.High,
		ctx.SeverityDistribution.Critical,
		ctx.SeverityDistribution.High,
		agg.ReportsLast30Days,
		agg.ReportsLast7Days,
		agg.ReportsPrev7Days,
		agg.GrowthLast7VsPrev7,
	))
	appendExampleBullets(&b, securityExamples, 4)
	return strings.TrimSpace(b.String())
}

func buildTrendsAnswer(ctx *database.IntelligenceContext, agg *IntelligenceAggregates, examples []IntelligenceExample) string {
	var b strings.Builder
	delta := agg.ReportsLast7Days - agg.ReportsPrev7Days
	b.WriteString(fmt.Sprintf("Trend view: %d reports in the last 7 days versus %d in the previous 7-day window (%+.1f%%, delta %d). Last 30 days total: %d reports.\n",
		agg.ReportsLast7Days,
		agg.ReportsPrev7Days,
		agg.GrowthLast7VsPrev7,
		delta,
		agg.ReportsLast30Days,
	))
	if len(agg.TopCategories) > 0 {
		b.WriteString("The largest current driver is ")
		b.WriteString(fmt.Sprintf("%s (%d reports, %.1f%% share).\n", agg.TopCategories[0].Name, agg.TopCategories[0].Count, agg.TopCategories[0].SharePct))
	}
	appendExampleBullets(&b, examples, 3)
	return strings.TrimSpace(b.String())
}

func buildSampleReportsAnswer(ctx *database.IntelligenceContext, agg *IntelligenceAggregates, examples []IntelligenceExample) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Here is a representative sample from %s: %d total valid reports in dataset, %d reports in the last 30 days.\n", ctx.OrgID, agg.TotalReports, agg.ReportsLast30Days))
	if len(examples) == 0 {
		b.WriteString("No representative examples were available right now.")
		return b.String()
	}
	appendExampleBullets(&b, examples, 5)
	return strings.TrimSpace(b.String())
}

func buildLastWeekAnswer(ctx *database.IntelligenceContext, agg *IntelligenceAggregates, examples []IntelligenceExample) string {
	var b strings.Builder
	if agg.ReportsLast7Days > 0 {
		b.WriteString(fmt.Sprintf("%s has %d reports in the last 7 days (%d in the previous 7-day window, %+.1f%%).\n", ctx.OrgID, agg.ReportsLast7Days, agg.ReportsPrev7Days, agg.GrowthLast7VsPrev7))
	} else {
		b.WriteString(fmt.Sprintf("%s has 0 reports in the last 7 days.", ctx.OrgID))
		if ctx.LastReportAt != nil {
			b.WriteString(fmt.Sprintf(" Last report was on %s UTC.", ctx.LastReportAt.UTC().Format("2006-01-02")))
		}
		b.WriteString("\n")
	}
	appendExampleBullets(&b, examples, 3)
	return strings.TrimSpace(b.String())
}

func buildGenericInScopeAnswer(ctx *database.IntelligenceContext, question string, agg *IntelligenceAggregates, examples []IntelligenceExample) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("For %s, we currently have %d valid reports. Last 30 days: %d. Last 7 days: %d vs %d in the prior week (%+.1f%%).\n",
		ctx.OrgID,
		agg.TotalReports,
		agg.ReportsLast30Days,
		agg.ReportsLast7Days,
		agg.ReportsPrev7Days,
		agg.GrowthLast7VsPrev7,
	))
	if len(agg.TopCategories) > 0 {
		b.WriteString("Most frequent categories now: ")
		for i, c := range agg.TopCategories {
			if i >= 3 {
				break
			}
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("%s (%d)", c.Name, c.Count))
		}
		b.WriteString(".\n")
	}
	appendExampleBullets(&b, examples, 3)
	_ = question
	return strings.TrimSpace(b.String())
}

func appendExampleBullets(b *strings.Builder, examples []IntelligenceExample, max int) {
	if len(examples) == 0 {
		return
	}
	if max <= 0 {
		max = 3
	}
	b.WriteString("\nRepresentative evidence:\n")
	for i, ex := range examples {
		if i >= max {
			break
		}
		b.WriteString(fmt.Sprintf("- %s (%s, severity %.2f, %s) (Report #%d)\n", ex.Title, ex.Channel, ex.Severity, formatShortDate(ex.CreatedAt), ex.ID))
	}
}

func formatShortDate(raw string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return "recent"
	}
	return t.UTC().Format("2006-01-02")
}

func applyEntitlements(plan string, result intelligenceComputedResult, opts entitlementOptions) intelligenceComputedResult {
	plan = strings.ToLower(strings.TrimSpace(plan))
	if result.Mode == ResponseModeBlocked {
		result.AnswerMarkdown = stripUpgradeLines(result.AnswerMarkdown)
		if result.Upsell != nil {
			result.Upsell.Text = stripDuplicateUpgradeMentions(result.Upsell.Text)
		}
		return result
	}

	if plan == "pro" {
		result.Mode = ResponseModeFull
		result.AnswerMarkdown = stripUpgradeLines(result.AnswerMarkdown)
		result.Upsell = nil
		return result
	}

	result.Mode = ResponseModePartialFree
	result.AnswerMarkdown = stripUpgradeLines(result.AnswerMarkdown)

	if len(result.Data.Examples) > 5 {
		result.Data.Examples = result.Data.Examples[:5]
	}
	for i := range result.Data.Examples {
		result.Data.Examples[i].Snippet = redactPII(result.Data.Examples[i].Snippet)
		result.Data.Examples[i].Title = redactPII(result.Data.Examples[i].Title)
	}

	upsellText := freeTierUpsellText
	if opts.ExportRequested {
		upsellText = "Raw exports and exhaustive listings are Pro-only. This preview includes representative findings you can act on now."
	}
	result.Upsell = &IntelligenceUpsell{Text: upsellText, CTA: "Upgrade to Pro"}
	result.Upsell.Text = stripDuplicateUpgradeMentions(result.Upsell.Text)

	return result
}

func stripUpgradeLines(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(l, "upgrade to pro") || strings.Contains(l, "pro unlock") || strings.Contains(l, "full report detail") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func stripDuplicateUpgradeMentions(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	if strings.Count(lower, "upgrade to pro") <= 1 {
		return trimmed
	}
	first := strings.Index(lower, "upgrade to pro")
	if first < 0 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:first+len("Upgrade to Pro")])
}

func hasConcreteAnswer(answer string, examples []IntelligenceExample) bool {
	if len(examples) > 0 {
		return true
	}
	if strings.TrimSpace(answer) == "" {
		return false
	}
	if strings.Contains(strings.ToLower(answer), "substantial corpus") {
		return false
	}
	for _, r := range answer {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func redactPII(s string) string {
	out := strings.TrimSpace(s)
	if out == "" {
		return out
	}
	out = emailRegex.ReplaceAllString(out, "[redacted-email]")
	out = phoneRegex.ReplaceAllString(out, "[redacted-phone]")
	out = idRegex.ReplaceAllString(out, "[redacted-id]")
	return truncateText(out, 220)
}

func isSecurityReportSnippet(item database.ReportSnippet) bool {
	return isSecurityText(item.Title + " " + item.Summary)
}

func isSecurityText(text string) bool {
	lower := strings.ToLower(text)
	securityTerms := []string{"security", "phishing", "spam", "fraud", "abuse", "vuln", "malware", "attack", "credential"}
	for _, term := range securityTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func (h *Handlers) logIntelligenceMetrics(orgID, tier, quality string, intent IntelligenceIntent, resp IntelligenceQueryResponse) {
	hasAgg := resp.Data.Aggregates != nil
	exampleCount := len(resp.Data.Examples)
	nonAnswer := resp.Mode != ResponseModeBlocked && !hasConcreteAnswer(resp.AnswerMarkdown, resp.Data.Examples)
	log.Printf(
		"intelligence_metrics org=%s tier=%s quality=%s intent=%s mode=%s aggregates=%t examples=%d non_answer=%t",
		orgID,
		tier,
		quality,
		intent,
		resp.Mode,
		hasAgg,
		exampleCount,
		nonAnswer,
	)
}

func (h *Handlers) getIntelligenceCounts(ctx context.Context, orgID string) (int, int, int) {
	if cached, ok, fresh := h.getBrandCountsCached(orgID); ok && fresh {
		return cached.Total, cached.High, cached.Medium
	}
	countCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	total, high, medium, err := h.db.GetBrandPriorityCountsByBrandName(countCtx, orgID)
	if err != nil {
		// Best-effort fallback to total-only count so UI/chat can still show real scale.
		totalOnlyCtx, cancelTotal := context.WithTimeout(ctx, 8*time.Second)
		totalOnly, countErr := h.db.GetReportsCountByBrandName(totalOnlyCtx, orgID)
		cancelTotal()
		if countErr == nil && totalOnly > 0 {
			if cached, ok, _ := h.getBrandCountsCached(orgID); ok {
				return totalOnly, cached.High, cached.Medium
			}
			return totalOnly, 0, 0
		}
		if cached, ok, _ := h.getBrandCountsCached(orgID); ok {
			return cached.Total, cached.High, cached.Medium
		}
		return 0, 0, 0
	}
	h.setBrandCountsCached(orgID, total, high, medium)
	return total, high, medium
}

func (h *Handlers) loadIntentContext(ctx context.Context, orgID, question string, intent IntelligenceIntent, excludeIDs []int) (*database.IntelligenceContext, []database.FixPriority, error) {
	ctxRead, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Broad executive prompts should not be keyword-overfiltered.
	contextQuestion := question
	priorityQuestion := question
	switch intent {
	case IntentFixFirst, IntentComplaintsSummary, IntentSecurityRisks, IntentTrends, IntentSampleReports, IntentReportsLastWeek:
		contextQuestion = ""
		priorityQuestion = ""
	}

	intelCtx, err := h.db.GetIntelligenceContextWithOptions(ctxRead, orgID, contextQuestion, database.IntelligenceContextOptions{
		Intent:           string(intent),
		ExcludeReportIDs: excludeIDs,
	})
	if err != nil {
		return nil, nil, err
	}

	priorities := make([]database.FixPriority, 0)
	if intent == IntentFixFirst {
		prioCtx, cancelPrio := context.WithTimeout(ctx, 2200*time.Millisecond)
		defer cancelPrio()
		rows, prioErr := h.db.GetFixPriorities(prioCtx, orgID, priorityQuestion, excludeIDs, 3)
		if prioErr != nil {
			log.Printf("fix priority query failed org=%s: %v", orgID, prioErr)
		} else {
			priorities = rows
		}
	}

	return intelCtx, priorities, nil
}

func classifyIntelligenceIntent(orgID, question string) IntelligenceIntent {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return IntentOutOfScope
	}

	if isOutOfScopeQuery(orgID, q) {
		return IntentOutOfScope
	}

	sampleTerms := []string{"sample", "examples", "what i'm buying", "what im buying", "show me reports", "some reports", "representative"}
	for _, term := range sampleTerms {
		if strings.Contains(q, term) {
			return IntentSampleReports
		}
	}

	if strings.Contains(q, "last week") || strings.Contains(q, "past week") || strings.Contains(q, "last 7 days") || strings.Contains(q, "how many reports") {
		return IntentReportsLastWeek
	}

	switch {
	case strings.Contains(q, "fix first"),
		strings.Contains(q, "what should we fix"),
		strings.Contains(q, "prioritize"),
		strings.Contains(q, "priority plan"):
		return IntentFixFirst
	case strings.Contains(q, "top issues"),
		strings.Contains(q, "biggest issues"),
		strings.Contains(q, "top complaints"),
		strings.Contains(q, "complain"),
		strings.Contains(q, "complaints"):
		return IntentComplaintsSummary
	case strings.Contains(q, "security"),
		strings.Contains(q, "phishing"),
		strings.Contains(q, "spam"),
		strings.Contains(q, "fraud"),
		strings.Contains(q, "abuse"):
		return IntentSecurityRisks
	case strings.Contains(q, "trend"),
		strings.Contains(q, "increasing"),
		strings.Contains(q, "decreasing"),
		strings.Contains(q, "changing"),
		strings.Contains(q, "spike"),
		strings.Contains(q, "week over week"),
		strings.Contains(q, "month over month"):
		return IntentTrends
	default:
		return IntentGenericInScope
	}
}

func isOutOfScopeQuery(orgID, lowerQuestion string) bool {
	hasIssueSignal := issueTermsRegex.MatchString(lowerQuestion)
	hasOrgSignal := false
	hasCleanAppSignal := strings.Contains(lowerQuestion, "cleanapp") || strings.Contains(lowerQuestion, "clean ai") || strings.Contains(lowerQuestion, "cleanai")
	org := strings.ToLower(strings.TrimSpace(orgID))
	if org != "" && strings.Contains(lowerQuestion, org) {
		hasOrgSignal = true
	} else {
		for _, token := range strings.FieldsFunc(org, func(r rune) bool {
			return (r < '0' || r > '9') && (r < 'a' || r > 'z')
		}) {
			if len(token) < 4 {
				continue
			}
			if strings.Contains(lowerQuestion, token) {
				hasOrgSignal = true
				break
			}
		}
	}

	hasScopeSignal := hasIssueSignal || hasOrgSignal || hasCleanAppSignal
	if oosGlobalRegex.MatchString(lowerQuestion) && !hasScopeSignal {
		return true
	}

	return false
}

func cloneSuggestedPrompts() []string {
	out := make([]string, len(promptSuggestions))
	copy(out, promptSuggestions)
	return out
}

func normalizeQualityMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fast":
		return "fast"
	case "deep":
		return "deep"
	default:
		return "deep"
	}
}

func collectEvidenceReports(ctx *database.IntelligenceContext) []database.ReportSnippet {
	if len(ctx.EvidencePack) > 0 {
		return ctx.EvidencePack
	}
	if len(ctx.MatchedReports) > 0 {
		return ctx.MatchedReports
	}
	return ctx.RepresentativeReports
}

func buildReportPermalink(baseURL, orgID string, seq int) string {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://cleanapp.io"
	}
	org := url.PathEscape(strings.ToLower(strings.TrimSpace(orgID)))
	return fmt.Sprintf("%s/digital/%s/report/%d", strings.TrimRight(baseURL, "/"), org, seq)
}

func truncateText(s string, max int) string {
	text := strings.TrimSpace(s)
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}
