package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

var exportPromptRegex = regexp.MustCompile(`(?i)(show all reports|export|csv|json|pdf|download|full database|full dataset|dump|list all)`)

const freeTierPaywallMessage = `You’ve reached the free intelligence limit.

Upgrade to access:
• Unlimited questions
• Full report details
• Raw data & exports
• Trend analysis and alerts

[Upgrade to Pro]`

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

	intelCtx, err := h.loadIntelligenceContext(c.Request.Context(), orgID)
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

	if tier != "pro" && exportPromptRegex.MatchString(question) {
		c.JSON(http.StatusOK, IntelligenceQueryResponse{
			Answer: fmt.Sprintf(
				"I can provide a summary of key findings.\n\nFull report access and exports are available with a Pro subscription.\n\n%s",
				buildQuickSummary(intelCtx),
			),
			ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
			PaywallTriggered: false,
		})
		return
	}

	answer := buildQuickSummary(intelCtx)
	if h.geminiClient != nil && h.geminiClient.Enabled() {
		systemPrompt := buildSystemPrompt(tier)
		userPrompt := buildUserPrompt(intelCtx, question)

		queryCtx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()

		generated, genErr := h.geminiClient.GenerateAnswer(queryCtx, systemPrompt, userPrompt)
		if genErr != nil {
			log.Printf("intelligence gemini generation failed org=%s tier=%s: %v", orgID, tier, genErr)
		} else if strings.TrimSpace(generated) != "" {
			answer = strings.TrimSpace(generated)
		}
	}

	if tier != "pro" {
		answer = sanitizeFreeTierAnswer(answer)
	}

	c.JSON(http.StatusOK, IntelligenceQueryResponse{
		Answer:           answer,
		ReportsAnalyzed:  intelCtx.ReportsAnalyzed,
		PaywallTriggered: false,
	})
}

func (h *Handlers) loadIntelligenceContext(ctx context.Context, orgID string) (*database.IntelligenceContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return h.db.GetIntelligenceContext(ctx, orgID)
}

func buildSystemPrompt(tier string) string {
	base := `You are CleanApp Intelligence.

You are an operations intelligence assistant, not customer support.
Keep responses concise, structured, and actionable.`

	if strings.ToLower(strings.TrimSpace(tier)) == "pro" {
		return base + `

For Pro users:
- Provide detailed analysis.
- Still avoid dumping extremely large raw datasets unless explicitly requested.`
	}

	return base + `

For free-tier users:
- Provide high-level summaries only.
- Never provide full report lists.
- Never output raw datasets.
- Never provide exportable formats (PDF, CSV, JSON).
- If the user asks for full data, exports, or exhaustive lists, respond with a summary and mention that full access requires a Pro upgrade.`
}

func buildUserPrompt(ctx *database.IntelligenceContext, question string) string {
	var b strings.Builder
	b.WriteString("Organization: ")
	b.WriteString(ctx.OrgID)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Reports analyzed: %d\n", ctx.ReportsAnalyzed))
	b.WriteString(fmt.Sprintf("Reports this month: %d\n", ctx.ReportsThisMonth))
	b.WriteString(fmt.Sprintf("High priority reports: %d\n", ctx.HighPriorityCount))
	b.WriteString(fmt.Sprintf("Medium priority reports: %d\n", ctx.MediumPriorityCount))

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

	if len(ctx.RecentSummaries) > 0 {
		b.WriteString("Recent report summaries:\n")
		for _, summary := range ctx.RecentSummaries {
			b.WriteString("- ")
			b.WriteString(summary)
			b.WriteString("\n")
		}
	}

	b.WriteString("\nUser question:\n")
	b.WriteString(question)
	return b.String()
}

func buildQuickSummary(ctx *database.IntelligenceContext) string {
	var topIssue string
	if len(ctx.TopIssues) > 0 {
		topIssue = fmt.Sprintf("Top recurring issue: %s (%d reports).", ctx.TopIssues[0].Name, ctx.TopIssues[0].Count)
	}
	if topIssue == "" {
		topIssue = "Top recurring issue patterns are available with active tracking."
	}

	return fmt.Sprintf(
		"We analyzed %d reports.\n\nThis month: %d reports.\nHigh priority: %d.\nMedium priority: %d.\n\n%s",
		ctx.ReportsAnalyzed,
		ctx.ReportsThisMonth,
		ctx.HighPriorityCount,
		ctx.MediumPriorityCount,
		topIssue,
	)
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
		strings.Contains(lower, ".pdf") {
		return "I can provide a summary of key findings.\n\nFull report access and exports are available with a Pro subscription."
	}
	return out
}
