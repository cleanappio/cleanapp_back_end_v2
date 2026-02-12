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
}

var exportPromptRegex = regexp.MustCompile(`(?i)(show all reports|export|csv|json|pdf|download|full database|full dataset|dump|list all|raw report text|exhaustive list)`)
var linkRegex = regexp.MustCompile(`https?://[^\s)]+`)

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

	intelCtx, err := h.loadIntelligenceContext(c.Request.Context(), orgID, question)
	if err != nil {
		log.Printf("intelligence context failed for org=%s: %v", orgID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load intelligence context"})
		return
	}

	baseURL := h.cfg.IntelligenceBaseURL
	evidenceItems := buildEvidenceItems(intelCtx, baseURL, 6)

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
				ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
				PaywallTriggered: true,
				EvidenceCount:    len(evidenceItems),
				Evidence:         evidenceItems,
			})
			return
		}
	}

	answer := buildExecutiveSummary(intelCtx, question, baseURL)

	if tier != "pro" && exportPromptRegex.MatchString(question) {
		answer = "I can provide a summary of key findings.\n\nFull report access and exports are available with a Pro subscription.\n\n" + buildExecutiveSummary(intelCtx, question, baseURL)
		answer = enforceExecutiveFormat(answer, intelCtx, question, baseURL)
		answer = sanitizeFreeTierAnswer(answer)
		answer = ensureUpgradeNudge(answer)
		c.JSON(http.StatusOK, IntelligenceQueryResponse{
			Answer:           answer,
			ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
			PaywallTriggered: false,
			EvidenceCount:    len(evidenceItems),
			Evidence:         evidenceItems,
		})
		return
	}

	if h.geminiClient != nil && h.geminiClient.Enabled() {
		systemPrompt := buildSystemPrompt(tier)
		userPrompt := buildUserPrompt(intelCtx, question, baseURL)

		queryCtx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()

		generated, genErr := h.geminiClient.GenerateAnswer(queryCtx, systemPrompt, userPrompt)
		if genErr != nil {
			log.Printf("intelligence gemini generation failed org=%s tier=%s: %v", orgID, tier, genErr)
		} else if strings.TrimSpace(generated) != "" {
			answer = strings.TrimSpace(generated)
		}
	}

	answer = enforceExecutiveFormat(answer, intelCtx, question, baseURL)
	if tier != "pro" {
		answer = sanitizeFreeTierAnswer(answer)
		answer = ensureUpgradeNudge(answer)
	}

	c.JSON(http.StatusOK, IntelligenceQueryResponse{
		Answer:           answer,
		ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
		PaywallTriggered: false,
		EvidenceCount:    len(evidenceItems),
		Evidence:         evidenceItems,
	})
}

func (h *Handlers) loadIntelligenceContext(ctx context.Context, orgID, question string) (*database.IntelligenceContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	return h.db.GetIntelligenceContext(ctx, orgID, question)
}

func buildSystemPrompt(tier string) string {
	base := `You are CleanApp Intelligence, an executive intelligence analyst for CEOs/CTOs.

Hard constraints:
- Ground every concrete claim in the provided evidence pack.
- Do not invent incidents, counts, trends, or links.
- If evidence is insufficient, say so explicitly.
- Keep output crisp, strategic, and action-oriented.

Required response format (exact section headings):
1) Executive takeaway
2) What’s changing
3) Qualitative context (user voice)
4) Recommended actions
5) Evidence links

Evidence rules:
- Include 3-6 report permalinks when available.
- Prioritize: most recent relevant, highest severity relevant, representative recurring issue.
- Avoid near-duplicate incidents.`

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

func buildUserPrompt(ctx *database.IntelligenceContext, question, baseURL string) string {
	var b strings.Builder
	b.WriteString("Organization: ")
	b.WriteString(ctx.OrgID)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Reports analyzed: %d\n", ctx.ReportsAnalyzed))
	b.WriteString(fmt.Sprintf("Reports this month: %d\n", ctx.ReportsThisMonth))
	b.WriteString(fmt.Sprintf("Reports last 30 days: %d\n", ctx.ReportsLast30Days))
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

func buildExecutiveSummary(ctx *database.IntelligenceContext, question, baseURL string) string {
	evidence := collectEvidenceReports(ctx)
	links := buildEvidenceItems(ctx, baseURL, 6)
	minLinks := 3
	if len(links) < minLinks {
		minLinks = len(links)
	}

	var b strings.Builder
	b.WriteString("1) Executive takeaway\n")
	if len(ctx.TopIssues) > 0 {
		top := ctx.TopIssues[0]
		b.WriteString(fmt.Sprintf("%s has recurring pressure around \"%s\" (%d reports), with %d high-priority signals requiring active ownership.\n", ctx.OrgID, top.Name, top.Count, ctx.HighPriorityCount))
	} else {
		b.WriteString(fmt.Sprintf("%s has %d analyzed reports with visible recurring issue patterns that warrant focused execution.\n", ctx.OrgID, ctx.ReportsAnalyzed))
	}

	b.WriteString("\n2) What’s changing\n")
	if ctx.ReportsLast30Days > 0 {
		b.WriteString(fmt.Sprintf("Last 7 days: %d reports vs %d in the prior 7 days (%.1f%%). Last 30 days total: %d.\n", ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7, ctx.ReportsLast30Days))
	} else {
		b.WriteString("Recent volume is muted; current risk is dominated by unresolved recurring issues rather than net-new spikes.\n")
	}
	if len(ctx.TopClassifications) > 0 {
		b.WriteString(fmt.Sprintf("Dominant category: %s (%d reports).\n", ctx.TopClassifications[0].Name, ctx.TopClassifications[0].Count))
	}

	b.WriteString("\n3) Qualitative context (user voice)\n")
	if len(evidence) == 0 {
		b.WriteString("- No recent qualitative snippets are available for this brand right now.\n")
	} else {
		snippetCount := 3
		if len(evidence) < snippetCount {
			snippetCount = len(evidence)
		}
		for i := 0; i < snippetCount; i++ {
			item := evidence[i]
			b.WriteString(fmt.Sprintf("- \"%s\" (%s, severity %.2f)\n", truncateText(item.Summary, 160), item.Classification, item.SeverityLevel))
		}
	}

	b.WriteString("\n4) Recommended actions\n")
	b.WriteString("1. Assign a single accountable owner for the top recurring issue and set a 14-day remediation target.\n")
	b.WriteString("2. Prioritize high-severity incidents first, then bundle medium-severity repeats into one execution stream.\n")
	b.WriteString("3. Review evidence links below in weekly leadership triage and convert recurring patterns into roadmap commitments.\n")

	b.WriteString("\n5) Evidence links\n")
	if len(links) == 0 {
		b.WriteString("- No report links available.\n")
	} else {
		for i, item := range links {
			if i >= 6 {
				break
			}
			b.WriteString(fmt.Sprintf("- %s — %s\n", item.Title, item.Permalink))
		}
		if minLinks > 0 {
			b.WriteString(fmt.Sprintf("\nEvidence: %d reports\n", len(links)))
		}
	}

	_ = question
	return strings.TrimSpace(b.String())
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

func buildEvidenceItems(ctx *database.IntelligenceContext, baseURL string, max int) []IntelligenceEvidenceItem {
	if max <= 0 {
		max = 6
	}
	reports := collectEvidenceReports(ctx)
	if len(reports) == 0 {
		return nil
	}

	items := make([]IntelligenceEvidenceItem, 0, max)
	seen := make(map[int]struct{}, max)
	for _, r := range reports {
		if len(items) >= max {
			break
		}
		if _, exists := seen[r.Seq]; exists {
			continue
		}
		seen[r.Seq] = struct{}{}
		items = append(items, IntelligenceEvidenceItem{
			Seq:       r.Seq,
			Title:     r.Title,
			Permalink: buildReportPermalink(baseURL, ctx.OrgID, r.Seq),
		})
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].Seq > items[j].Seq })
	return items
}

func buildReportPermalink(baseURL, orgID string, seq int) string {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://cleanapp.io"
	}
	org := url.PathEscape(strings.ToLower(strings.TrimSpace(orgID)))
	return fmt.Sprintf("%s/digital/%s/report/%d", strings.TrimRight(baseURL, "/"), org, seq)
}

func enforceExecutiveFormat(answer string, ctx *database.IntelligenceContext, question, baseURL string) string {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return buildExecutiveSummary(ctx, question, baseURL)
	}

	requiredHeadings := []string{
		"1) executive takeaway",
		"2) what’s changing",
		"3) qualitative context",
		"4) recommended actions",
		"5) evidence links",
	}
	lower := strings.ToLower(trimmed)
	for _, heading := range requiredHeadings {
		if !strings.Contains(lower, heading) {
			return buildExecutiveSummary(ctx, question, baseURL)
		}
	}

	requiredLinks := 3
	evidenceAvailable := len(buildEvidenceItems(ctx, baseURL, 6))
	if evidenceAvailable < requiredLinks {
		requiredLinks = evidenceAvailable
	}
	if countHTTPLinks(trimmed) < requiredLinks {
		trimmed = appendEvidenceLinksSection(trimmed, ctx, baseURL, 6)
	}

	return strings.TrimSpace(trimmed)
}

func appendEvidenceLinksSection(answer string, ctx *database.IntelligenceContext, baseURL string, maxLinks int) string {
	items := buildEvidenceItems(ctx, baseURL, maxLinks)
	if len(items) == 0 {
		return answer
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(answer))
	if !strings.Contains(strings.ToLower(answer), "evidence links") {
		b.WriteString("\n\n5) Evidence links\n")
	} else {
		b.WriteString("\n")
	}
	for i, item := range items {
		if i >= maxLinks {
			break
		}
		b.WriteString(fmt.Sprintf("- %s — %s\n", item.Title, item.Permalink))
	}
	b.WriteString(fmt.Sprintf("\nEvidence: %d reports\n", len(items)))
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
