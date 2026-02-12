package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
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
	IntentGenericInScope    IntelligenceIntent = "generic_in_scope"
	IntentOutOfScope        IntelligenceIntent = "oos"
)

type IntelligenceQueryRequest struct {
	OrgID            string  `json:"org_id"`
	Question         string  `json:"question"`
	SessionID        string  `json:"session_id"`
	UserID           *string `json:"user_id"`
	SubscriptionTier string  `json:"subscription_tier"`
}

type IntelligenceEvidenceItem struct {
	Seq       int    `json:"seq"`
	Title     string `json:"title"`
	Permalink string `json:"permalink"`
}

type IntelligenceQueryResponse struct {
	Answer           string                     `json:"answer"`
	ReportsAnalyzed  int                        `json:"reports_analyzed"`
	PaywallTriggered bool                       `json:"paywall_triggered"`
	EvidenceCount    int                        `json:"evidence_count,omitempty"`
	Evidence         []IntelligenceEvidenceItem `json:"evidence,omitempty"`
	SuggestedPrompts []string                   `json:"suggested_prompts,omitempty"`
}

var (
	exportPromptRegex = regexp.MustCompile(`(?i)(show all reports|export|csv|json|pdf|download|full database|full dataset|dump|list all|raw report text|exhaustive list)`)
	linkRegex         = regexp.MustCompile(`https?://[^\s)]+`)
	oosGlobalRegex    = regexp.MustCompile(`(?i)\b(eth|btc|bitcoin|ethereum|doge|crypto|price|weather|sports|nba|nfl|mlb|nhl|epl|stock|stocks|nasdaq|dow|gold price|president|prime minister|celebrity|movie times|lottery|horoscope)\b`)
	issueTermsRegex   = regexp.MustCompile(`(?i)\b(bug|bugs|error|errors|issue|issues|incident|incidents|outage|outages|login|password|security|spam|phishing|abuse|crash|latency|downtime|complain|complaints|ux|ui|fix|priority|risk|risks|report|reports|alert|alerts|feedback|support|billing|feature|trend|trends|fehler|problem|probleme|ausfall|sicherheit|risiko|risiken|bericht|berichte|beschwerde|beschwerden|problema|problemas|incidencia|incidencias|seguridad|riesgo|riesgos|incidentes|rapport|rapports|sécurité|risque|risques|panne|pannes|problème|problèmes|ошибк|проблем|сбо|инцидент|безопасност|отчет|отчёт|жалоб|риск|риски|otchet|otchety|otchetov)\b`)
)

var promptSuggestions = []string{
	"What do users complain about most?",
	"What should we fix first?",
	"Are there any security risks?",
}

const freeTierPaywallMessage = `You’ve reached the free intelligence limit.

Upgrade to access:
• Unlimited questions
• Full report details
• Raw data & exports
• Trend analysis and alerts

[Upgrade to Pro]`

const freeTierUpgradeNudge = "\n\nUpgrade to Pro for unlimited intelligence queries, full report detail, and exports."

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

	if orgID == "" || question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id and question are required"})
		return
	}
	if tier != "pro" && sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required for non-pro usage"})
		return
	}

	totalCount, highCount, mediumCount := h.getIntelligenceCounts(c.Request.Context(), orgID)

	if tier != "pro" {
		allowed, _, usageErr := h.db.GetAndIncrementIntelligenceUsage(
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
			c.JSON(http.StatusOK, IntelligenceQueryResponse{
				Answer:           freeTierPaywallMessage,
				ReportsAnalyzed:  totalCount,
				PaywallTriggered: true,
			})
			return
		}
	}

	intent := classifyIntelligenceIntent(orgID, question)
	if intent == IntentOutOfScope {
		answer := fmt.Sprintf("I’m CleanApp Intelligence for %s. I answer questions about %s issues/reports (bugs, outages, UX, security). I can’t answer that here.", orgID, orgID)
		c.JSON(http.StatusOK, IntelligenceQueryResponse{
			Answer:           answer,
			ReportsAnalyzed:  totalCount,
			PaywallTriggered: false,
			SuggestedPrompts: cloneSuggestedPrompts(),
		})
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
	answer := h.buildIntentFallbackAnswer(intent, intelCtx, question, baseURL, priorities)

	if tier != "pro" && exportPromptRegex.MatchString(question) {
		answer = "I can provide a summary of key findings.\n\nFull report access and exports are available with a Pro subscription.\n\n" + answer
	}

	if h.geminiClient != nil && h.geminiClient.Enabled() {
		systemPrompt := buildSystemPrompt(tier, intent)
		userPrompt := buildUserPrompt(intelCtx, question, baseURL, intent, priorities)

		queryCtx, cancel := context.WithTimeout(c.Request.Context(), 18*time.Second)
		generated, genErr := h.geminiClient.GenerateAnswer(queryCtx, systemPrompt, userPrompt)
		cancel()
		if genErr != nil {
			log.Printf("intelligence gemini generation failed org=%s intent=%s tier=%s: %v", orgID, intent, tier, genErr)
		} else if strings.TrimSpace(generated) != "" {
			answer = strings.TrimSpace(generated)
		}
	}

	answer = h.enforceIntentFormat(intent, answer, intelCtx, question, baseURL, priorities)
	if tier != "pro" {
		answer = sanitizeFreeTierAnswer(answer)
		answer = ensureUpgradeNudge(answer)
	}

	evidenceItems := h.buildIntentEvidenceItems(intent, intelCtx, baseURL, priorities)
	if len(evidenceItems) > 6 {
		evidenceItems = evidenceItems[:6]
	}

	if sessionID != "" && len(evidenceItems) > 0 {
		used := make([]int, 0, len(evidenceItems))
		for _, item := range evidenceItems {
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

	c.JSON(http.StatusOK, IntelligenceQueryResponse{
		Answer:           answer,
		ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
		PaywallTriggered: false,
		EvidenceCount:    len(evidenceItems),
		Evidence:         evidenceItems,
	})
}

func (h *Handlers) getIntelligenceCounts(ctx context.Context, orgID string) (int, int, int) {
	if cached, ok, fresh := h.getBrandCountsCached(orgID); ok && fresh {
		return cached.Total, cached.High, cached.Medium
	}
	countCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	total, high, medium, err := h.db.GetBrandPriorityCountsByBrandName(countCtx, orgID)
	if err != nil {
		if cached, ok, _ := h.getBrandCountsCached(orgID); ok {
			return cached.Total, cached.High, cached.Medium
		}
		return 0, 0, 0
	}
	h.setBrandCountsCached(orgID, total, high, medium)
	return total, high, medium
}

func (h *Handlers) loadIntentContext(ctx context.Context, orgID, question string, intent IntelligenceIntent, excludeIDs []int) (*database.IntelligenceContext, []database.FixPriority, error) {
	ctxRead, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Intent-driven prompts like "what should we fix first?" are broad executive asks,
	// not keyword searches; avoid over-filtering evidence/context by the literal prompt.
	contextQuestion := question
	priorityQuestion := question
	switch intent {
	case IntentFixFirst, IntentComplaintsSummary, IntentSecurityRisks, IntentTrends:
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

	switch {
	case strings.Contains(q, "fix first"),
		strings.Contains(q, "what should we fix"),
		strings.Contains(q, "prioritize"),
		strings.Contains(q, "priority plan"):
		return IntentFixFirst
	case strings.Contains(q, "complain"),
		strings.Contains(q, "users complain"),
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

	// Be permissive by default: only classify as OOS when the ask is clearly
	// global/trivia and has no product/report scope signals.
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

func buildSystemPrompt(tier string, intent IntelligenceIntent) string {
	base := `You are CleanApp Intelligence, an executive intelligence analyst for CEOs/CTOs.

Hard constraints:
- Ground every concrete claim in the provided evidence pack.
- Do not invent incidents, counts, trends, or links.
- If evidence is insufficient, say so explicitly.
- Keep output crisp, strategic, and action-oriented.
- Respond in the same language as the user's question when possible.
- Write in natural narrative form for executive readers, not a rigid template.
- Prefer short paragraphs plus selective bullets where they improve clarity.
- Include inline citations in prose/bullets using this style: (Report #12345).

Evidence rules:
- Include 3-6 report permalinks when available.
- Prioritize: most recent relevant, highest severity relevant, representative recurring issue.
- Avoid near-duplicate incidents.`

	if intent == IntentFixFirst {
		base += `

For this question intent:
- Provide a ranked Top 3 fix plan.
- For each priority include: why now (severity×frequency×recency), success metric, and 1-2 inline citations.
- Include one "Quick win (48h)" and one "Big rock (2-4w)".`
	}

	if strings.EqualFold(strings.TrimSpace(tier), "pro") {
		return base + `

For Pro users:
- You may provide deeper analysis and tradeoffs.
- Still avoid dumping massive raw data.`
	}

	return base + `

For free-tier users:
- Provide summary-level insights only.
- Never provide full report dumps or exhaustive lists.
- Never output export formats (PDF/CSV/JSON).
- If asked for full data/exports, provide summary and mention Pro upgrade.`
}

func buildUserPrompt(ctx *database.IntelligenceContext, question, baseURL string, intent IntelligenceIntent, priorities []database.FixPriority) string {
	var b strings.Builder
	b.WriteString("Intent: ")
	b.WriteString(string(intent))
	b.WriteString("\n")
	b.WriteString("Organization: ")
	b.WriteString(ctx.OrgID)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Page dataset total (all-time valid): %d\n", ctx.ReportsAnalyzed))
	b.WriteString(fmt.Sprintf("Analysis window last 30 days: %d\n", ctx.ReportsLast30Days))
	b.WriteString(fmt.Sprintf("This month: %d\n", ctx.ReportsThisMonth))
	b.WriteString(fmt.Sprintf("Last 7 days: %d | Previous 7 days: %d | Growth: %.1f%%\n", ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7))
	b.WriteString(fmt.Sprintf("Priority counts: high=%d medium=%d\n", ctx.HighPriorityCount, ctx.MediumPriorityCount))
	b.WriteString(fmt.Sprintf("Severity distribution: critical=%d high=%d medium=%d low=%d\n", ctx.SeverityDistribution.Critical, ctx.SeverityDistribution.High, ctx.SeverityDistribution.Medium, ctx.SeverityDistribution.Low))

	if len(ctx.TopClassifications) > 0 {
		b.WriteString("Top categories:\n")
		for _, item := range ctx.TopClassifications {
			b.WriteString(fmt.Sprintf("- %s: %d\n", item.Name, item.Count))
		}
	}
	if len(ctx.TopEntities) > 0 {
		b.WriteString("Top entities:\n")
		for _, item := range ctx.TopEntities {
			b.WriteString(fmt.Sprintf("- %s: %d\n", item.Name, item.Count))
		}
	}
	if len(ctx.TopTags) > 0 {
		b.WriteString("Top tags:\n")
		for _, item := range ctx.TopTags {
			b.WriteString(fmt.Sprintf("- %s: %d\n", item.Name, item.Count))
		}
	}
	if len(ctx.Keywords) > 0 {
		b.WriteString("Question keywords inferred: ")
		b.WriteString(strings.Join(ctx.Keywords, ", "))
		b.WriteString("\n")
	}

	if len(priorities) > 0 {
		b.WriteString("Fix priorities (pre-ranked):\n")
		for i, p := range priorities {
			b.WriteString(fmt.Sprintf("%d) %s | freq=%d avg_severity=%.2f recent7=%d score=%.2f\n", i+1, p.Issue, p.Frequency, p.AvgSeverity, p.Recent7Days, p.Score))
			for _, r := range p.Reports {
				b.WriteString(fmt.Sprintf("   - seq=%d title=%q class=%q sev=%.2f updated=%s\n", r.Seq, r.Title, r.Classification, r.SeverityLevel, r.UpdatedAt.Format(time.RFC3339)))
				b.WriteString(fmt.Sprintf("     snippet=%q\n", truncateText(r.Summary, 220)))
				b.WriteString(fmt.Sprintf("     permalink=%s\n", buildReportPermalink(baseURL, ctx.OrgID, r.Seq)))
			}
		}
	}

	evidence := collectEvidenceReports(ctx)
	if len(evidence) > 0 {
		b.WriteString("Evidence pack (ground your answer on these reports):\n")
		for _, item := range evidence {
			b.WriteString(fmt.Sprintf("- seq=%d title=%q class=%q severity=%.2f updated=%s\n", item.Seq, item.Title, item.Classification, item.SeverityLevel, item.UpdatedAt.Format(time.RFC3339)))
			b.WriteString(fmt.Sprintf("  snippet=%q\n", truncateText(item.Summary, 220)))
			b.WriteString(fmt.Sprintf("  permalink=%s\n", buildReportPermalink(baseURL, ctx.OrgID, item.Seq)))
		}
	}

	b.WriteString("\nUser question:\n")
	b.WriteString(question)
	return b.String()
}

func (h *Handlers) buildIntentFallbackAnswer(intent IntelligenceIntent, ctx *database.IntelligenceContext, question, baseURL string, priorities []database.FixPriority) string {
	switch intent {
	case IntentFixFirst:
		return buildFixFirstSummary(ctx, baseURL, priorities)
	case IntentComplaintsSummary:
		return buildComplaintsSummary(ctx, baseURL)
	case IntentSecurityRisks:
		return buildSecuritySummary(ctx, baseURL)
	case IntentTrends:
		return buildTrendsSummary(ctx, baseURL)
	default:
		return buildExecutiveSummary(ctx, question, baseURL)
	}
}

func buildExecutiveSummary(ctx *database.IntelligenceContext, question, baseURL string) string {
	evidence := collectEvidenceReports(ctx)
	links := buildEvidenceItemsFromReports(ctx.OrgID, baseURL, evidence, 6)

	var b strings.Builder
	b.WriteString("Executive takeaway: ")
	if len(ctx.TopIssues) > 0 {
		top := ctx.TopIssues[0]
		b.WriteString(fmt.Sprintf("%s has recurring pressure around \"%s\" (%d reports), with %d high-priority signals requiring active ownership.\n", ctx.OrgID, top.Name, top.Count, ctx.HighPriorityCount))
	} else {
		b.WriteString(fmt.Sprintf("%s has %d analyzed reports with recurring issue patterns that require focused execution.\n", ctx.OrgID, ctx.ReportsAnalyzed))
	}

	b.WriteString("\n\nWhat’s changing:\n")
	b.WriteString(fmt.Sprintf("Page dataset total (all-time valid): %d reports. Analysis window (last 30 days): %d reports.\n", ctx.ReportsAnalyzed, ctx.ReportsLast30Days))
	b.WriteString(fmt.Sprintf("Last 7 days: %d vs previous 7 days: %d (%.1f%%).\n", ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7))
	if len(ctx.TopClassifications) > 0 {
		b.WriteString(fmt.Sprintf("Dominant category: %s (%d).\n", ctx.TopClassifications[0].Name, ctx.TopClassifications[0].Count))
	}

	b.WriteString("\nQualitative context (user voice):\n")
	appendUserVoice(&b, evidence)

	b.WriteString("\nRecommended actions:\n")
	b.WriteString("1. Assign one accountable owner for the top recurring issue and set a 14-day remediation target.\n")
	b.WriteString("2. Prioritize high-severity incidents first, then bundle medium-severity repeats into one execution stream.\n")
	b.WriteString("3. Review the linked incidents in weekly leadership triage and convert recurring patterns into roadmap commitments.\n")

	b.WriteString("\nSupporting evidence:\n")
	appendEvidenceLinks(&b, links)

	_ = question
	return strings.TrimSpace(b.String())
}

func buildComplaintsSummary(ctx *database.IntelligenceContext, baseURL string) string {
	evidence := collectEvidenceReports(ctx)
	links := buildEvidenceItemsFromReports(ctx.OrgID, baseURL, evidence, 6)

	var b strings.Builder
	b.WriteString("Executive takeaway: ")
	if len(ctx.TopIssues) > 0 {
		b.WriteString(fmt.Sprintf("User complaints cluster around \"%s\" and adjacent recurring themes.\n", ctx.TopIssues[0].Name))
	} else {
		b.WriteString("User complaints are concentrated in a small set of recurring issue themes.\n")
	}

	b.WriteString("\n\nWhat’s changing:\n")
	b.WriteString(fmt.Sprintf("Page dataset total: %d reports. Last 30 days: %d reports. Last 7 vs previous 7: %d vs %d (%.1f%%).\n", ctx.ReportsAnalyzed, ctx.ReportsLast30Days, ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7))
	if len(ctx.TopClassifications) > 0 {
		b.WriteString("Top complaint buckets: ")
		for i, item := range ctx.TopClassifications {
			if i >= 3 {
				break
			}
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("%s (%d)", item.Name, item.Count))
		}
		b.WriteString(".\n")
	}

	b.WriteString("\nQualitative context (user voice):\n")
	appendUserVoice(&b, evidence)

	b.WriteString("\nRecommended actions:\n")
	b.WriteString("1. Convert top complaint theme into a visible 2-week fix initiative with clear owner.\n")
	b.WriteString("2. Publish a short customer-facing update for the highest-frequency complaint category.\n")
	b.WriteString("3. Track complaint recurrence weekly and stop new regressions before adding new feature scope.\n")

	b.WriteString("\nSupporting evidence:\n")
	appendEvidenceLinks(&b, links)
	return strings.TrimSpace(b.String())
}

func buildSecuritySummary(ctx *database.IntelligenceContext, baseURL string) string {
	evidence := collectEvidenceReports(ctx)
	links := buildEvidenceItemsFromReports(ctx.OrgID, baseURL, evidence, 6)

	var b strings.Builder
	b.WriteString("Executive takeaway: ")
	b.WriteString(fmt.Sprintf("Security-relevant signals are present with %d high-priority reports in the current dataset.\n", ctx.HighPriorityCount))

	b.WriteString("\n\nWhat’s changing:\n")
	b.WriteString(fmt.Sprintf("Page dataset total: %d reports. Last 30 days: %d. Last 7 vs previous 7: %d vs %d (%.1f%%).\n", ctx.ReportsAnalyzed, ctx.ReportsLast30Days, ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7))
	b.WriteString(fmt.Sprintf("Severity mix: critical=%d high=%d medium=%d low=%d.\n", ctx.SeverityDistribution.Critical, ctx.SeverityDistribution.High, ctx.SeverityDistribution.Medium, ctx.SeverityDistribution.Low))

	b.WriteString("\nQualitative context (user voice):\n")
	appendUserVoice(&b, evidence)

	b.WriteString("\nRecommended actions:\n")
	b.WriteString("1. Triage highest-severity security incidents first and assign incident owners with 24h updates.\n")
	b.WriteString("2. Add preventive controls for recurring abuse/phishing vectors and measure recurrence weekly.\n")
	b.WriteString("3. Publish internal security notes tying each mitigation to linked incident evidence.\n")

	b.WriteString("\nSupporting evidence:\n")
	appendEvidenceLinks(&b, links)
	return strings.TrimSpace(b.String())
}

func buildTrendsSummary(ctx *database.IntelligenceContext, baseURL string) string {
	evidence := collectEvidenceReports(ctx)
	links := buildEvidenceItemsFromReports(ctx.OrgID, baseURL, evidence, 6)

	var b strings.Builder
	b.WriteString("Executive takeaway: ")
	if ctx.GrowthLast7VsPrev7 > 0 {
		b.WriteString(fmt.Sprintf("Issue velocity is increasing (+%.1f%% week-over-week), indicating growing operational pressure.\n", ctx.GrowthLast7VsPrev7))
	} else {
		b.WriteString(fmt.Sprintf("Issue velocity is stable-to-down (%+.1f%% week-over-week), but recurring themes still persist.\n", ctx.GrowthLast7VsPrev7))
	}

	b.WriteString("\n\nWhat’s changing:\n")
	b.WriteString(fmt.Sprintf("Page dataset total: %d reports. Analysis window (last 30 days): %d reports.\n", ctx.ReportsAnalyzed, ctx.ReportsLast30Days))
	b.WriteString(fmt.Sprintf("Last 7 days: %d vs previous 7 days: %d (%.1f%%).\n", ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7))
	if len(ctx.TopIssues) > 0 {
		b.WriteString(fmt.Sprintf("Top recurring trend driver: %s (%d reports).\n", ctx.TopIssues[0].Name, ctx.TopIssues[0].Count))
	}

	b.WriteString("\nQualitative context (user voice):\n")
	appendUserVoice(&b, evidence)

	b.WriteString("\nRecommended actions:\n")
	b.WriteString("1. Put trend-driving issue into weekly leadership KPI review with explicit reduction targets.\n")
	b.WriteString("2. Assign a prevention owner for each recurring trend driver and review recurrence after each release.\n")
	b.WriteString("3. Treat rising-week trend spikes as release gates for impacted areas.\n")

	b.WriteString("\nSupporting evidence:\n")
	appendEvidenceLinks(&b, links)
	return strings.TrimSpace(b.String())
}

func buildFixFirstSummary(ctx *database.IntelligenceContext, baseURL string, priorities []database.FixPriority) string {
	var b strings.Builder
	b.WriteString("Executive takeaway: ")
	if len(priorities) > 0 {
		b.WriteString(fmt.Sprintf("The fastest path to risk reduction is a ranked Top 3 fix plan led by \"%s\" and two adjacent recurring issues.\n", priorities[0].Issue))
	} else {
		b.WriteString("The fastest path to risk reduction is to focus on the top recurring issue clusters and execute a ranked fix plan.\n")
	}

	b.WriteString("\n\nWhat’s changing:\n")
	b.WriteString(fmt.Sprintf("Page dataset total (all-time valid): %d reports. Analysis window (last 30 days): %d reports.\n", ctx.ReportsAnalyzed, ctx.ReportsLast30Days))
	b.WriteString(fmt.Sprintf("Last 7 days: %d vs previous 7 days: %d (%.1f%%).\n", ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7))
	b.WriteString(fmt.Sprintf("Severity mix: critical=%d high=%d medium=%d low=%d.\n", ctx.SeverityDistribution.Critical, ctx.SeverityDistribution.High, ctx.SeverityDistribution.Medium, ctx.SeverityDistribution.Low))

	b.WriteString("\nQualitative context (user voice):\n")
	voice := flattenPriorityReports(priorities)
	if len(voice) == 0 {
		voice = collectEvidenceReports(ctx)
	}
	appendUserVoice(&b, voice)

	b.WriteString("\nRecommended actions:\n")
	b.WriteString("Top 3 Fix Plan\n")
	if len(priorities) == 0 {
		b.WriteString("1. Prioritize the highest-frequency issue theme and assign a single owner this week.\n")
		b.WriteString("2. Triage high-severity repeats first, then batch medium-severity recurrences.\n")
		b.WriteString("3. Track weekly recurrence and stop regressions before additional scope.\n")
	} else {
		for i, p := range priorities {
			if i >= 3 {
				break
			}
			b.WriteString(fmt.Sprintf("\nPriority %d — %s\n", i+1, p.Issue))
			b.WriteString(fmt.Sprintf("Why now: severity×frequency×recency score %.2f (avg severity %.2f × frequency %d × recent7 %d).\n", p.Score, p.AvgSeverity, p.Frequency, p.Recent7Days))
			metricTarget := 25 + (i * 5)
			b.WriteString(fmt.Sprintf("Success metric: reduce new '%s' reports by %d%% over the next 14 days; keep high-severity additions near zero.\n", p.Issue, metricTarget))
			if len(p.Reports) > 0 {
				links := buildEvidenceItemsFromReports(ctx.OrgID, baseURL, p.Reports, 2)
				if len(links) > 0 {
					b.WriteString("Evidence:\n")
					for _, item := range links {
						b.WriteString(fmt.Sprintf("- %s\n", item.Permalink))
					}
				}
			}
		}
	}

	b.WriteString("\nQuick win (48h): patch the top-severity recurring defect and publish a visible status update tied to the linked incidents.\n")
	b.WriteString("Big rock (2-4w): remove root-cause recurrence for the #1 priority issue via owner-led remediation and regression guardrails.\n")

	b.WriteString("\nSupporting evidence:\n")
	links := hqUniqueEvidenceForFixFirst(ctx, baseURL, priorities)
	appendEvidenceLinks(&b, links)

	return strings.TrimSpace(b.String())
}

func appendUserVoice(b *strings.Builder, evidence []database.ReportSnippet) {
	if len(evidence) == 0 {
		b.WriteString("- No recent qualitative snippets are available for this brand right now.\n")
		return
	}
	shown := 0
	seen := make(map[string]struct{}, 3)
	for _, item := range evidence {
		if shown >= 3 {
			break
		}
		snippet := strings.TrimSpace(item.Summary)
		if snippet == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(item.Title + "|" + snippet))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		b.WriteString(fmt.Sprintf("- \"%s\" (%s, severity %.2f)\n", truncateText(snippet, 170), item.Classification, item.SeverityLevel))
		shown++
	}
	if shown == 0 {
		b.WriteString("- No recent qualitative snippets are available for this brand right now.\n")
	}
}

func appendEvidenceLinks(b *strings.Builder, links []IntelligenceEvidenceItem) {
	if len(links) == 0 {
		b.WriteString("- No report links available.\n")
		return
	}
	max := len(links)
	if max > 6 {
		max = 6
	}
	for i := 0; i < max; i++ {
		b.WriteString(fmt.Sprintf("- Report #%d: %s — %s\n", links[i].Seq, links[i].Title, links[i].Permalink))
	}
	b.WriteString(fmt.Sprintf("\nEvidence: %d reports\n", max))
}

func flattenPriorityReports(priorities []database.FixPriority) []database.ReportSnippet {
	out := make([]database.ReportSnippet, 0, 6)
	for _, p := range priorities {
		out = append(out, p.Reports...)
	}
	return out
}

func hqUniqueEvidenceForFixFirst(ctx *database.IntelligenceContext, baseURL string, priorities []database.FixPriority) []IntelligenceEvidenceItem {
	reports := flattenPriorityReports(priorities)
	if len(reports) == 0 {
		reports = collectEvidenceReports(ctx)
	}
	items := buildEvidenceItemsFromReports(ctx.OrgID, baseURL, reports, 6)
	if len(items) > 6 {
		return items[:6]
	}
	return items
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

func buildEvidenceItemsFromReports(orgID, baseURL string, reports []database.ReportSnippet, max int) []IntelligenceEvidenceItem {
	if max <= 0 {
		max = 6
	}
	if len(reports) == 0 {
		return nil
	}

	items := make([]IntelligenceEvidenceItem, 0, max)
	seen := make(map[int]struct{}, max)
	for _, r := range reports {
		if len(items) >= max {
			break
		}
		if r.Seq <= 0 {
			continue
		}
		if _, exists := seen[r.Seq]; exists {
			continue
		}
		seen[r.Seq] = struct{}{}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = fmt.Sprintf("Report #%d", r.Seq)
		}
		items = append(items, IntelligenceEvidenceItem{
			Seq:       r.Seq,
			Title:     title,
			Permalink: buildReportPermalink(baseURL, orgID, r.Seq),
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Seq > items[j].Seq })
	return items
}

func (h *Handlers) buildIntentEvidenceItems(intent IntelligenceIntent, ctx *database.IntelligenceContext, baseURL string, priorities []database.FixPriority) []IntelligenceEvidenceItem {
	switch intent {
	case IntentFixFirst:
		return hqUniqueEvidenceForFixFirst(ctx, baseURL, priorities)
	default:
		return buildEvidenceItemsFromReports(ctx.OrgID, baseURL, collectEvidenceReports(ctx), 6)
	}
}

func buildReportPermalink(baseURL, orgID string, seq int) string {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://cleanapp.io"
	}
	org := url.PathEscape(strings.ToLower(strings.TrimSpace(orgID)))
	return fmt.Sprintf("%s/digital/%s/report/%d", strings.TrimRight(baseURL, "/"), org, seq)
}

func (h *Handlers) enforceIntentFormat(intent IntelligenceIntent, answer string, ctx *database.IntelligenceContext, question, baseURL string, priorities []database.FixPriority) string {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return h.buildIntentFallbackAnswer(intent, ctx, question, baseURL, priorities)
	}

	if intent == IntentFixFirst {
		lower := strings.ToLower(trimmed)
		if !strings.Contains(lower, "top 3") && !strings.Contains(lower, "priority") {
			return h.buildIntentFallbackAnswer(intent, ctx, question, baseURL, priorities)
		}
	}

	requiredLinks := 3
	evidenceAvailable := len(h.buildIntentEvidenceItems(intent, ctx, baseURL, priorities))
	if evidenceAvailable < requiredLinks {
		requiredLinks = evidenceAvailable
	}
	if countHTTPLinks(trimmed) < requiredLinks {
		trimmed = h.appendEvidenceLinksSection(intent, trimmed, ctx, baseURL, priorities, 6)
	}

	return strings.TrimSpace(trimmed)
}

func (h *Handlers) appendEvidenceLinksSection(intent IntelligenceIntent, answer string, ctx *database.IntelligenceContext, baseURL string, priorities []database.FixPriority, maxLinks int) string {
	items := h.buildIntentEvidenceItems(intent, ctx, baseURL, priorities)
	if len(items) == 0 {
		return answer
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(answer))
	if !strings.Contains(strings.ToLower(answer), "evidence") {
		b.WriteString("\n\nSupporting evidence:\n")
	} else {
		b.WriteString("\n")
	}
	for i, item := range items {
		if i >= maxLinks {
			break
		}
		b.WriteString(fmt.Sprintf("- Report #%d: %s — %s\n", item.Seq, item.Title, item.Permalink))
	}
	b.WriteString(fmt.Sprintf("\nEvidence: %d reports\n", minInt(len(items), maxLinks)))
	return strings.TrimSpace(b.String())
}

func countHTTPLinks(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return len(linkRegex.FindAllString(text, -1))
}

func sanitizeFreeTierAnswer(answer string) string {
	out := strings.TrimSpace(answer)
	if out == "" {
		return out
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "```") ||
		strings.Contains(lower, "\"reports\"") ||
		strings.Contains(lower, ".csv") ||
		strings.Contains(lower, ".pdf") ||
		strings.Contains(lower, "{\"") {
		return "I can provide a summary of key findings.\n\nFull report access and exports are available with a Pro subscription."
	}
	return out
}

func ensureUpgradeNudge(answer string) string {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "upgrade to pro") || strings.Contains(lower, "pro subscription") {
		return trimmed
	}
	return trimmed + freeTierUpgradeNudge
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
