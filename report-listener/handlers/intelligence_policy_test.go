package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"report-listener/database"
)

func fixtureIntelligenceContext() *database.IntelligenceContext {
	now := time.Date(2026, 2, 12, 12, 0, 0, 0, time.UTC)
	return &database.IntelligenceContext{
		OrgID:              "apple",
		ReportsAnalyzed:    66148,
		ReportsThisMonth:   421,
		ReportsLast30Days:  1903,
		ReportsLast7Days:   320,
		ReportsPrev7Days:   245,
		GrowthLast7VsPrev7: 30.6,
		LastReportAt:       &now,
		SeverityDistribution: database.SeverityDistribution{
			Critical: 12,
			High:     210,
			Medium:   1010,
			Low:      671,
		},
		TopClassifications: []database.NamedCount{
			{Name: "digital", Count: 44120},
			{Name: "physical", Count: 22028},
		},
		TopIssues: []database.NamedCount{
			{Name: "Apple - Security", Count: 2900},
			{Name: "Apple - Feature Request", Count: 2400},
		},
		EvidencePack: []database.ReportSnippet{
			{Seq: 1144052, Title: "Apple auth bypass", Summary: "Users report suspicious login bypass attempts and phishing pages.", Classification: "digital", SeverityLevel: 0.92, UpdatedAt: now.Add(-2 * time.Hour)},
			{Seq: 1143042, Title: "Billing retry loop", Summary: "Recurring billing retry loop after failed card validation.", Classification: "digital", SeverityLevel: 0.74, UpdatedAt: now.Add(-12 * time.Hour)},
			{Seq: 1141442, Title: "Store listing mismatch", Summary: "Product copy mismatch causes trust issues in storefront descriptions.", Classification: "physical", SeverityLevel: 0.66, UpdatedAt: now.Add(-24 * time.Hour)},
			{Seq: 1130751, Title: "Password reset abuse", Summary: "Potential abuse in password reset endpoint with repeated requests.", Classification: "digital", SeverityLevel: 0.89, UpdatedAt: now.Add(-36 * time.Hour)},
			{Seq: 1120847, Title: "Fraudulent domain clone", Summary: "Typosquat domain impersonating Apple support and harvesting credentials.", Classification: "digital", SeverityLevel: 0.95, UpdatedAt: now.Add(-48 * time.Hour)},
		},
		MatchedReports: []database.ReportSnippet{
			{Seq: 1144052, Title: "Apple auth bypass", Summary: "Users report suspicious login bypass attempts and phishing pages.", Classification: "digital", SeverityLevel: 0.92, UpdatedAt: now.Add(-2 * time.Hour)},
		},
	}
}

func fixturePriorities() []database.FixPriority {
	ctx := fixtureIntelligenceContext()
	return []database.FixPriority{
		{
			Issue:       "Apple - Security",
			Frequency:   2900,
			AvgSeverity: 0.88,
			Recent7Days: 101,
			Score:       257752.0,
			Reports: []database.ReportSnippet{
				ctx.EvidencePack[0],
				ctx.EvidencePack[4],
			},
		},
		{
			Issue:       "Apple - Feature Request",
			Frequency:   2400,
			AvgSeverity: 0.57,
			Recent7Days: 64,
			Score:       101376.0,
			Reports: []database.ReportSnippet{
				ctx.EvidencePack[1],
			},
		},
	}
}

func TestCommonIntentAnswersAreConcrete(t *testing.T) {
	h := &Handlers{}
	ctx := fixtureIntelligenceContext()
	priorities := fixturePriorities()

	prompts := []string{
		"what are the top issues reported?",
		"what problems are increasing fastest?",
		"are there any security risks?",
		"give me sample reports",
		"how many reports last week",
	}

	for _, prompt := range prompts {
		intent := classifyIntelligenceIntent("apple", prompt)
		computed := h.buildComputedResult(intent, ctx, prompt, "https://cleanapp.io", priorities)
		computed = applyEntitlements("free", computed, entitlementOptions{})

		if computed.Mode == ResponseModeBlocked {
			t.Fatalf("prompt %q unexpectedly blocked", prompt)
		}
		if !hasConcreteAnswer(computed.AnswerMarkdown, computed.Data.Examples) {
			t.Fatalf("prompt %q produced non-concrete answer: %q", prompt, computed.AnswerMarkdown)
		}
		if strings.Contains(strings.ToLower(computed.AnswerMarkdown), "substantial corpus") {
			t.Fatalf("prompt %q returned banned boilerplate", prompt)
		}
		if computed.Upsell == nil {
			t.Fatalf("prompt %q expected single upsell in free mode", prompt)
		}
		upgradeMentions := strings.Count(strings.ToLower(computed.AnswerMarkdown), "upgrade to pro") + strings.Count(strings.ToLower(computed.Upsell.Text), "upgrade to pro")
		if upgradeMentions > 1 {
			t.Fatalf("prompt %q repeated CTA too many times", prompt)
		}
	}
}

func TestApplyEntitlementsKeepsExamplesAndCapsToFive(t *testing.T) {
	h := &Handlers{}
	ctx := fixtureIntelligenceContext()
	priorities := fixturePriorities()

	computed := h.buildComputedResult(IntentSampleReports, ctx, "give me sample reports", "https://cleanapp.io", priorities)
	computed.Data.Examples = append(computed.Data.Examples,
		IntelligenceExample{ID: 999001, Title: "Extra", Snippet: "s1"},
		IntelligenceExample{ID: 999002, Title: "Extra2", Snippet: "s2"},
	)
	computed = applyEntitlements("free", computed, entitlementOptions{ExportRequested: true})

	if computed.Mode != ResponseModePartialFree {
		t.Fatalf("expected partial_free, got %s", computed.Mode)
	}
	if len(computed.Data.Examples) == 0 {
		t.Fatalf("expected examples for free response")
	}
	if len(computed.Data.Examples) > 5 {
		t.Fatalf("expected examples <=5, got %d", len(computed.Data.Examples))
	}
	if computed.Upsell == nil || !strings.Contains(strings.ToLower(computed.Upsell.Text), "pro") {
		t.Fatalf("expected single upsell text in free mode")
	}
}

func TestResponseSchemaContainsModeAnswerDataAndNoBoilerplate(t *testing.T) {
	h := &Handlers{}
	ctx := fixtureIntelligenceContext()
	priorities := fixturePriorities()

	computed := h.buildComputedResult(IntentComplaintsSummary, ctx, "what are top complaints", "https://cleanapp.io", priorities)
	computed = applyEntitlements("pro", computed, entitlementOptions{})
	resp := h.toAPIResponse(computed, ctx.ReportsAnalyzed)

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if decoded["mode"] == nil || decoded["answer_markdown"] == nil || decoded["data"] == nil {
		t.Fatalf("response missing required schema fields: %v", decoded)
	}
	if strings.Contains(strings.ToLower(resp.AnswerMarkdown), "substantial corpus") {
		t.Fatalf("response included banned boilerplate")
	}
	if !hasConcreteAnswer(resp.AnswerMarkdown, resp.Data.Examples) {
		t.Fatalf("response failed concrete-answer requirement")
	}
}
