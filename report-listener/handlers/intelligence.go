package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
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

type IntelligenceQueryResponse struct {
	Answer           string `json:"answer"`
	ReportsAnalyzed  int    `json:"reports_analyzed"`
	PaywallTriggered bool   `json:"paywall_triggered"`
}

var exportPromptRegex = regexp.MustCompile(`(?i)(show all reports|export|csv|json|pdf|download|full database|full dataset|dump|list all|raw report text)`)

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
			})
			return
		}
	}

	baseURL := h.cfg.IntelligenceBaseURL
	answer := buildExecutiveSummary(intelCtx, baseURL)

	if tier != "pro" && exportPromptRegex.MatchString(question) {
		answer = fmt.Sprintf(
			"I can provide a summary of key findings.\n\nFull report access and exports are available with a Pro subscription.\n\n%s",
			buildExecutiveSummary(intelCtx, baseURL),
		)
		answer = ensureEvidenceLinks(answer, intelCtx, baseURL, 2)
		answer = sanitizeFreeTierAnswer(answer)
		answer = ensureUpgradeNudge(answer)
		c.JSON(http.StatusOK, IntelligenceQueryResponse{
			Answer:           answer,
			ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
			PaywallTriggered: false,
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

	answer = ensureEvidenceLinks(answer, intelCtx, baseURL, 3)
	if tier != "pro" {
		answer = sanitizeFreeTierAnswer(answer)
		answer = ensureUpgradeNudge(answer)
	}

	c.JSON(http.StatusOK, IntelligenceQueryResponse{
		Answer:           answer,
		ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
		PaywallTriggered: false,
	})
}

func (h *Handlers) loadIntelligenceContext(ctx context.Context, orgID, question string) (*database.IntelligenceContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 2200*time.Millisecond)
	defer cancel()
	return h.db.GetIntelligenceContext(ctx, orgID, question)
}

func buildSystemPrompt(tier string) string {
	base := `You are CleanApp Intelligence, an operations intelligence analyst for CEOs/CTOs.

Output quality requirements:
- prioritize qualitative insight and decision context, not just counts.
- be concise and executive-ready.
- reference specific evidence from provided report snippets.
- include concrete report permalinks from the provided evidence when available.

Response structure:
1) Executive brief (2-3 sentences)
2) What leadership should know (3 bullets)
3) Recommended actions (3 numbered items)
4) Evidence reports (2-3 bullets with title + permalink)`

	if strings.ToLower(strings.TrimSpace(tier)) == "pro" {
		return base + `

For Pro users:
- Provide detailed analysis and clear tradeoffs.
- Avoid dumping huge raw data; summarize intelligently unless explicitly requested.`
	}

	return base + `

For free-tier users:
- Provide high-level summaries only.
- Never provide full report lists.
- Never output raw datasets.
- Never provide exportable formats (PDF, CSV, JSON).
- If asked for full data/exports/exhaustive lists, provide summary and mention Pro upgrade.`
}

func buildUserPrompt(ctx *database.IntelligenceContext, question, baseURL string) string {
	var b strings.Builder
	b.WriteString("Organization: ")
	b.WriteString(ctx.OrgID)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Reports analyzed: %d\n", ctx.ReportsAnalyzed))
	b.WriteString(fmt.Sprintf("Reports this month: %d\n", ctx.ReportsThisMonth))
	b.WriteString(fmt.Sprintf("Reports last 30 days: %d\n", ctx.ReportsLast30Days))
	b.WriteString(fmt.Sprintf("High priority reports: %d\n", ctx.HighPriorityCount))
	b.WriteString(fmt.Sprintf("Medium priority reports: %d\n", ctx.MediumPriorityCount))
	b.WriteString(fmt.Sprintf("Last 7 days: %d\n", ctx.ReportsLast7Days))
	b.WriteString(fmt.Sprintf("Previous 7 days: %d\n", ctx.ReportsPrev7Days))
	b.WriteString(fmt.Sprintf("7d trend vs prior week: %.1f%%\n", ctx.GrowthLast7VsPrev7))

	if len(ctx.TopClassifications) > 0 {
		b.WriteString("Top classifications:\n")
		for _, item := range ctx.TopClassifications {
			b.WriteString(fmt.Sprintf("- %s: %d\n", item.Name, item.Count))
		}
	}

	if len(ctx.TopIssues) > 0 {
		b.WriteString("Top recurring issues:\n")
		for _, item := range ctx.TopIssues {
			b.WriteString(fmt.Sprintf("- %s (%d)\n", item.Name, item.Count))
		}
	}

	evidence := collectEvidenceReports(ctx)
	if len(evidence) > 0 {
		b.WriteString("Evidence report snippets (use these links in your Evidence section):\n")
		for _, item := range evidence {
			b.WriteString(fmt.Sprintf("- seq=%d title=%q class=%q severity=%.2f updated=%s\n", item.Seq, item.Title, item.Classification, item.SeverityLevel, item.UpdatedAt.Format(time.RFC3339)))
			b.WriteString(fmt.Sprintf("  summary=%q\n", truncateText(item.Summary, 260)))
			b.WriteString(fmt.Sprintf("  permalink=%s\n", buildReportPermalink(baseURL, ctx.OrgID, item.Seq)))
		}
	}

	if len(ctx.RecentSummaries) > 0 {
		b.WriteString("Recent report summaries:\n")
		for _, summary := range ctx.RecentSummaries {
			b.WriteString("- ")
			b.WriteString(truncateText(summary, 220))
			b.WriteString("\n")
		}
	}

	if len(ctx.Keywords) > 0 {
		b.WriteString("Question keywords inferred: ")
		b.WriteString(strings.Join(ctx.Keywords, ", "))
		b.WriteString("\n")
	}

	b.WriteString("\nUser question:\n")
	b.WriteString(question)
	return b.String()
}

func buildExecutiveSummary(ctx *database.IntelligenceContext, baseURL string) string {
	var b strings.Builder

	b.WriteString("Executive brief\n")
	b.WriteString(fmt.Sprintf("- CleanApp analyzed %d reports for %s. High-priority signals: %d, medium-priority: %d.\n", ctx.ReportsAnalyzed, ctx.OrgID, ctx.HighPriorityCount, ctx.MediumPriorityCount))
	if ctx.ReportsLast30Days > 0 {
		b.WriteString(fmt.Sprintf("- Activity in the last 30 days: %d reports. Last 7 days: %d vs %d in the prior week (%.1f%% trend).\n", ctx.ReportsLast30Days, ctx.ReportsLast7Days, ctx.ReportsPrev7Days, ctx.GrowthLast7VsPrev7))
	} else {
		b.WriteString("- No significant new report volume in the last 30 days; main value now is risk memory and recurring-pattern management.\n")
	}

	b.WriteString("\nWhat leadership should know\n")
	if len(ctx.TopIssues) > 0 {
		top := ctx.TopIssues[0]
		b.WriteString(fmt.Sprintf("- Most repeated issue pattern: %s (%d reports), indicating a persistent quality/reliability theme rather than isolated incidents.\n", top.Name, top.Count))
	}
	if len(ctx.TopClassifications) > 0 {
		primary := ctx.TopClassifications[0]
		b.WriteString(fmt.Sprintf("- Dominant classification: %s (%d reports), which should drive prioritization and owner assignment.\n", primary.Name, primary.Count))
	}
	if samples := collectEvidenceReports(ctx); len(samples) > 0 {
		for i, s := range samples {
			if i >= 2 {
				break
			}
			b.WriteString(fmt.Sprintf("- Example signal: %s (%s). %s\n", s.Title, s.Classification, truncateText(s.Summary, 140)))
		}
	}

	b.WriteString("\nRecommended actions\n")
	b.WriteString("1. Assign an owner to the top recurring issue and set a 14-day remediation target with measurable outcomes.\n")
	b.WriteString("2. Triage high-priority items first and publish a public-facing status update for trust and transparency.\n")
	b.WriteString("3. Review evidence reports below with engineering/product leadership and convert repeated themes into a fix roadmap.\n")

	b.WriteString("\nEvidence reports\n")
	evidence := collectEvidenceReports(ctx)
	if len(evidence) == 0 {
		b.WriteString("- No specific evidence links available right now.\n")
	} else {
		for i, item := range evidence {
			if i >= 3 {
				break
			}
			b.WriteString(fmt.Sprintf("- %s — %s\n", item.Title, buildReportPermalink(baseURL, ctx.OrgID, item.Seq)))
		}
	}

	return strings.TrimSpace(b.String())
}

func collectEvidenceReports(ctx *database.IntelligenceContext) []database.ReportSnippet {
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

func ensureEvidenceLinks(answer string, ctx *database.IntelligenceContext, baseURL string, maxLinks int) string {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return trimmed
	}
	if strings.Contains(trimmed, "http://") || strings.Contains(trimmed, "https://") {
		return trimmed
	}

	evidence := collectEvidenceReports(ctx)
	if len(evidence) == 0 {
		return trimmed
	}
	if maxLinks <= 0 {
		maxLinks = 2
	}

	var b strings.Builder
	b.WriteString(trimmed)
	b.WriteString("\n\nEvidence reports\n")
	for i, item := range evidence {
		if i >= maxLinks {
			break
		}
		b.WriteString(fmt.Sprintf("- %s — %s\n", item.Title, buildReportPermalink(baseURL, ctx.OrgID, item.Seq)))
	}
	return strings.TrimSpace(b.String())
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
