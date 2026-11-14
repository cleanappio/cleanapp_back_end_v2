package reportoutcome

import "time"

func Review(input Input, analysis Analysis, now time.Time) Outcome {
	result := Outcome{Processing: Processing{Status: "pending_analysis"},
		Decision:    &Decision{State: Pending, ReasonCode: "pending_analysis", Message: "Analysis is pending.", PolicyVersion: PolicyVersion},
		Publication: Publication{Status: "private", ReasonCode: "private_intake"},
		Delivery:    []Delivery{{Channel: "email", Status: "not_requested"}, {Channel: "blockscan", Status: "not_requested"}},
		NextAction:  NextAction{Type: "wait"}}
	if input.AnalysisSuppressed {
		result.Processing.Status = "not_required"
		result.Decision = &Decision{State: Held, ReasonCode: "analysis_policy_hold", Message: "This record is retained under an explicit analysis restriction.", PolicyVersion: PolicyVersion, DecidedAt: &now}
		result.Publication = Publication{Status: "held", ReasonCode: "analysis_policy_hold"}
		result.NextAction = NextAction{Type: "none"}
		result.Final = true
		return result
	}
	if !analysis.Available && input.ConsentVersion > 0 && input.PrivacyHoldReason != "" &&
		(input.Handling.Publication == "automatic" || input.Handling.Notices == "automatic") {
		result.Processing.Status = "not_required"
		result.Decision = &Decision{State: Held, ReasonCode: input.PrivacyHoldReason, Message: "This report is retained privately. Prepare a safe text version as a new report with a new source ID, retaining this receipt. Changing the original payload on retry is not supported.", PolicyVersion: PolicyVersion, DecidedAt: &now}
		result.Publication = Publication{Status: "held", ReasonCode: input.PrivacyHoldReason}
		result.NextAction = NextAction{Type: "review_privacy"}
		result.Final = true
		for index := range result.Delivery {
			if input.Handling.Notices == "automatic" {
				result.Delivery[index].Status, result.Delivery[index].ReasonCode = "held", input.PrivacyHoldReason
			}
		}
		return result
	}
	if !analysis.Available {
		return result
	}
	result.Processing = Processing{Status: "analyzed", CompletedAt: &now}
	if analysis.CompletedAt != nil {
		result.Processing.CompletedAt = analysis.CompletedAt
	}
	result.Decision = &Decision{State: Accepted, ReasonCode: "report_accepted", Message: "Analysis accepted this report as an observed problem. This does not establish independent verification or resolution.", PolicyVersion: PolicyVersion, DecidedAt: &now}
	result.NextAction = NextAction{Type: "none"}
	result.Final = true
	switch {
	case !analysis.IsValid:
		result.Decision.State, result.Decision.ReasonCode, result.Decision.Message = Rejected, "not_reportable_problem", "Analysis did not identify a reportable problem in this submission."
	case !input.HasOriginal:
		result.Decision.State, result.Decision.ReasonCode, result.Decision.Message = NeedsInformation, "original_observation_needed", "Prepare a new report containing the original observation or evidence, using a new source ID and retaining this receipt."
		result.NextAction = NextAction{Type: "provide_information", Fields: []string{"original_observation"}}
	case input.ConsentVersion > 0 && (input.Handling.Publication == "automatic" || input.Handling.Notices == "automatic") && !input.HasSourceContext:
		result.Decision.State, result.Decision.ReasonCode, result.Decision.Message = NeedsInformation, "affected_context_needed", "Prepare a new report identifying the affected product, URL, place, or source context. Use a new source ID and retain this receipt."
		result.NextAction = NextAction{Type: "provide_information", Fields: []string{"affected_product_url_or_place"}}
	case analysis.EvidenceVersion < 1:
		result.Decision.State, result.Decision.ReasonCode, result.Decision.Message = Held, "evidence_classification_unavailable", "The report is retained, but evidence classification is unavailable. Prepare a new report with clearer original source context, using a new source ID and retaining this receipt."
		result.NextAction = NextAction{Type: "provide_information", Fields: []string{"original_evidence_context"}}
	case analysis.ReportKind != "firsthand" && analysis.ReportKind != "coverage":
		result.Decision.State, result.Decision.ReasonCode, result.Decision.Message = NeedsInformation, "observation_context_needed", "Prepare a new report describing the observed problem and source context. Use a new source ID and retain this receipt."
		result.NextAction = NextAction{Type: "provide_information", Fields: []string{"observed_problem", "observation_context"}}
	case len(input.Candidates) > 0:
		result.Decision.State, result.Decision.ReasonCode, result.Decision.Message = ClusterCandidate, "related_issue_evidence", "This report is retained as additional evidence for a related issue. It keeps its own record and attribution."
		result.Relationship = &Relationship{Type: "cluster_candidate", Candidates: input.Candidates}
	case analysis.ReportKind == "coverage" && !input.HasSourceContext:
		result.Decision.State, result.Decision.ReasonCode, result.Decision.Message = NeedsInformation, "source_context_needed", "Prepare a new report with a clear source reference or observation context. Use a new source ID and retain this receipt."
		result.NextAction = NextAction{Type: "provide_information", Fields: []string{"observation_or_related_issue"}}
	}
	accepted := result.Decision.State == Accepted || result.Decision.State == ClusterCandidate
	if !accepted {
		result.Publication.ReasonCode = result.Decision.ReasonCode
		return result
	}
	if input.ConsentVersion > 0 && input.Handling.Publication == "automatic" {
		reason := input.PolicyHoldReason
		if reason == "" {
			reason = input.PrivacyHoldReason
		}
		if reason != "" {
			result.Publication = Publication{Status: "held", ReasonCode: reason}
			if input.PrivacyHoldReason != "" {
				result.NextAction = NextAction{Type: "review_privacy"}
			}
		} else {
			result.Publication = Publication{Status: "pending"}
			result.NextAction = NextAction{Type: "wait"}
			result.Final = false
		}
	}
	if input.ConsentVersion > 0 && input.Handling.Notices == "automatic" {
		for index := range result.Delivery {
			result.Delivery[index].Status = "held"
			result.Delivery[index].ReasonCode = "publication_required"
			if result.Publication.Status == "pending" {
				result.Delivery[index].Status, result.Delivery[index].ReasonCode = "pending", "publication_pending"
			} else if result.Publication.ReasonCode != "" {
				result.Delivery[index].ReasonCode = result.Publication.ReasonCode
			}
		}
	}
	return result
}
