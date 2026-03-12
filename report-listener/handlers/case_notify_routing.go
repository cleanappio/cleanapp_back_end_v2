package handlers

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"report-listener/models"
)

type caseRoutingProfile struct {
	Structural         bool
	Severe             bool
	Urgent             bool
	ImmediateDanger    bool
	SensitiveOccupancy bool
	HazardMode         string
}

func (h *Handlers) syncCaseContactStrategy(ctx context.Context, detail *models.CaseDetail) error {
	if detail == nil || strings.TrimSpace(detail.Case.CaseID) == "" {
		return nil
	}
	if len(detail.EscalationTargets) == 0 {
		return nil
	}

	routedTargets := routeCaseEscalationTargets(detail)
	storedTargets, err := h.db.UpsertCaseEscalationTargets(ctx, detail.Case.CaseID, routedTargets)
	if err != nil {
		return err
	}
	storedTargets = filterVisibleCaseTargets(storedTargets)

	observations := buildCaseContactObservations(storedTargets)
	storedObservations, err := h.db.ReplaceCaseContactObservations(ctx, detail.Case.CaseID, observations)
	if err != nil {
		return err
	}

	notifyPlan := buildCaseNotifyPlan(detail, storedTargets, storedObservations)
	storedPlan, err := h.db.ReplaceCaseNotifyPlan(ctx, detail.Case.CaseID, notifyPlan)
	if err != nil {
		return err
	}

	detail.EscalationTargets = storedTargets
	detail.ContactObservations = storedObservations
	detail.NotifyPlan = storedPlan
	return nil
}

func routeCaseEscalationTargets(detail *models.CaseDetail) []models.CaseEscalationTarget {
	profile := buildCaseRoutingProfile(detail)
	return routeTargetsWithProfile(profile, detail.EscalationTargets)
}

func routeReportEscalationTargets(report *models.ReportWithAnalysis, targets []models.CaseEscalationTarget) []models.CaseEscalationTarget {
	profile := buildReportRoutingProfile(report)
	return routeTargetsWithProfile(profile, targets)
}

func routeTargetsWithProfile(profile caseRoutingProfile, source []models.CaseEscalationTarget) []models.CaseEscalationTarget {
	targets := make([]models.CaseEscalationTarget, 0, len(source))
	for _, target := range source {
		routed := target
		routed.DecisionScope = emptyStringDefault(routed.DecisionScope, decisionScopeForRoleType(routed.RoleType))
		routed.AttributionClass = emptyStringDefault(routed.AttributionClass, attributionClassForTarget(routed))
		routed.ActionabilityScore = scoreCaseEscalationTarget(profile, routed)
		routed.NotifyTier = notifyTierForTarget(profile, routed)
		routed.SendEligibility = sendEligibilityForTarget(profile, routed)
		routed.ReasonSelected = buildCaseTargetReason(profile, routed)
		targets = append(targets, routed)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].NotifyTier != targets[j].NotifyTier {
			return targets[i].NotifyTier < targets[j].NotifyTier
		}
		if targets[i].ActionabilityScore != targets[j].ActionabilityScore {
			return targets[i].ActionabilityScore > targets[j].ActionabilityScore
		}
		if targets[i].ConfidenceScore != targets[j].ConfidenceScore {
			return targets[i].ConfidenceScore > targets[j].ConfidenceScore
		}
		return strings.Compare(targets[i].DisplayName, targets[j].DisplayName) < 0
	})
	return targets
}

func buildCaseContactObservations(targets []models.CaseEscalationTarget) []models.CaseContactObservation {
	return buildContactObservations("", targets)
}

func buildReportContactObservations(reportSeq int, targets []models.CaseEscalationTarget) []models.CaseContactObservation {
	return buildContactObservations(fmt.Sprintf("%d", reportSeq), targets)
}

func buildContactObservations(ownerID string, targets []models.CaseEscalationTarget) []models.CaseContactObservation {
	observations := make([]models.CaseContactObservation, 0, len(targets))
	for _, target := range targets {
		channelType := strings.TrimSpace(caseTargetChannel(target))
		channelValue := strings.TrimSpace(target.Email)
		switch channelType {
		case "phone":
			channelValue = strings.TrimSpace(target.Phone)
		case "social":
			channelValue = strings.TrimSpace(target.SocialHandle)
		case "website":
			channelValue = strings.TrimSpace(target.Website)
			if channelValue == "" {
				channelValue = strings.TrimSpace(target.ContactURL)
			}
		}
		observations = append(observations, models.CaseContactObservation{
			CaseID:           ownerID,
			RoleType:         target.RoleType,
			DecisionScope:    target.DecisionScope,
			OrganizationName: target.Organization,
			PersonName:       target.DisplayName,
			ChannelType:      channelType,
			ChannelValue:     channelValue,
			Email:            target.Email,
			Phone:            target.Phone,
			Website:          target.Website,
			ContactURL:       target.ContactURL,
			SocialPlatform:   target.SocialPlatform,
			SocialHandle:     target.SocialHandle,
			SourceURL:        target.SourceURL,
			EvidenceText:     target.EvidenceText,
			Verification:     target.Verification,
			AttributionClass: target.AttributionClass,
			ConfidenceScore:  target.ConfidenceScore,
			TargetSource:     target.TargetSource,
		})
	}
	return observations
}

func buildCaseNotifyPlan(detail *models.CaseDetail, targets []models.CaseEscalationTarget, observations []models.CaseContactObservation) *models.CaseNotifyPlan {
	if detail == nil {
		return nil
	}
	profile := buildCaseRoutingProfile(detail)
	return buildNotifyPlan(detail.Case.CaseID, profile, targets, observations)
}

func buildReportNotifyPlan(report *models.ReportWithAnalysis, targets []models.CaseEscalationTarget, observations []models.CaseContactObservation) *models.CaseNotifyPlan {
	if report == nil {
		return nil
	}
	profile := buildReportRoutingProfile(report)
	return buildNotifyPlan("", profile, targets, observations)
}

func buildNotifyPlan(caseID string, profile caseRoutingProfile, targets []models.CaseEscalationTarget, observations []models.CaseContactObservation) *models.CaseNotifyPlan {
	observationIDs := make(map[string]int64, len(observations))
	for _, observation := range observations {
		if key := observationKeyForTargetObservation(observation); key != "" {
			observationIDs[key] = observation.ID
		}
	}

	items := make([]models.CaseNotifyPlanItem, 0, len(targets))
	perWaveRank := make(map[int]int)
	selectedCountByScope := make(map[string]int)
	for _, target := range targets {
		wave := notifyTierForTarget(profile, target)
		perWaveRank[wave]++
		selected := shouldSelectTargetForNotifyPlan(profile, target, selectedCountByScope)
		if selected {
			selectedCountByScope[target.DecisionScope]++
		}
		observationID := observationIDs[observationKeyForTarget(target)]
		var observationPtr *int64
		if observationID > 0 {
			observationPtr = &observationID
		}
		var targetIDPtr *int64
		if target.ID > 0 {
			targetID := target.ID
			targetIDPtr = &targetID
		}
		items = append(items, models.CaseNotifyPlanItem{
			TargetID:           targetIDPtr,
			ObservationID:      observationPtr,
			WaveNumber:         wave,
			PriorityRank:       perWaveRank[wave],
			RoleType:           target.RoleType,
			DecisionScope:      target.DecisionScope,
			ActionabilityScore: target.ActionabilityScore,
			SendEligibility:    target.SendEligibility,
			ReasonSelected:     target.ReasonSelected,
			Selected:           selected,
		})
	}
	return &models.CaseNotifyPlan{
		CaseID:      caseID,
		HazardMode:  profile.HazardMode,
		Status:      "active",
		Summary:     buildCaseNotifyPlanSummary(profile, items),
		Items:       items,
		PlanVersion: 1,
	}
}

func buildCaseRoutingProfile(detail *models.CaseDetail) caseRoutingProfile {
	textParts := []string{
		detail.Case.Title,
		detail.Case.Summary,
		detail.Case.UncertaintyNotes,
	}
	maxReportSeverity := 0.0
	for _, report := range detail.LinkedReports {
		if report.SeverityLevel > maxReportSeverity {
			maxReportSeverity = report.SeverityLevel
		}
		textParts = append(textParts, report.Title, report.Summary)
	}
	joined := strings.ToLower(strings.Join(textParts, " "))
	profile := caseRoutingProfile{
		Structural:         containsAny(joined, []string{"structural", "crack", "collapse", "column", "beam", "facade", "brick", "concrete", "wall"}),
		Severe:             detail.Case.SeverityScore >= 0.8 || maxReportSeverity >= 0.8,
		Urgent:             detail.Case.UrgencyScore >= 0.7,
		ImmediateDanger:    containsAny(joined, []string{"falling", "imminent", "collapse", "separating", "detached", "support column", "support beam", "exposed", "children", "occupants"}),
		SensitiveOccupancy: containsAny(joined, []string{"school", "hospital", "station", "metro", "airport", "playground", "terminal", "mall", "daycare", "nursery"}),
		HazardMode:         "standard",
	}
	switch {
	case profile.ImmediateDanger && profile.Severe:
		profile.HazardMode = "emergency"
	case profile.Severe || profile.Urgent:
		profile.HazardMode = "urgent"
	}
	return profile
}

func buildReportRoutingProfile(report *models.ReportWithAnalysis) caseRoutingProfile {
	if report == nil {
		return caseRoutingProfile{HazardMode: "standard"}
	}
	textParts := []string{}
	maxSeverity := 0.0
	for _, analysis := range report.Analysis {
		textParts = append(textParts, analysis.Title, analysis.Summary, analysis.Description, analysis.AnalysisText)
		if analysis.SeverityLevel > maxSeverity {
			maxSeverity = analysis.SeverityLevel
		}
	}
	if report.Report.Description != nil {
		textParts = append(textParts, *report.Report.Description)
	}
	if report.Report.SourceURL != nil {
		textParts = append(textParts, *report.Report.SourceURL)
	}
	joined := strings.ToLower(strings.Join(textParts, " "))
	profile := caseRoutingProfile{
		Structural:         containsAny(joined, []string{"structural", "crack", "collapse", "column", "beam", "facade", "brick", "concrete", "wall", "support"}),
		Severe:             maxSeverity >= 0.8,
		Urgent:             maxSeverity >= 0.7 || containsAny(joined, []string{"urgent", "danger", "hazard", "critical"}),
		ImmediateDanger:    containsAny(joined, []string{"falling", "imminent", "collapse", "separating", "detached", "support column", "support beam", "load-bearing", "exposed"}),
		SensitiveOccupancy: containsAny(joined, []string{"school", "hospital", "station", "metro", "airport", "playground", "terminal", "mall", "daycare", "nursery", "children", "commuter"}),
		HazardMode:         "standard",
	}
	switch {
	case profile.ImmediateDanger && profile.Severe:
		profile.HazardMode = "emergency"
	case profile.Severe || profile.Urgent:
		profile.HazardMode = "urgent"
	}
	return profile
}

func decisionScopeForRoleType(roleType string) string {
	switch strings.ToLower(strings.TrimSpace(roleType)) {
	case "operator", "operator_admin", "site_leadership", "facility_manager":
		return "site_ops"
	case "owner", "property_owner", "landlord":
		return "asset_owner"
	case "building_authority", "public_safety", "fire_authority":
		return "regulator"
	case "architect", "engineer", "contractor":
		return "project_party"
	default:
		return "other"
	}
}

func attributionClassForTarget(target models.CaseEscalationTarget) string {
	verification := strings.ToLower(strings.TrimSpace(target.Verification))
	source := strings.ToLower(strings.TrimSpace(target.TargetSource))
	switch {
	case strings.HasPrefix(verification, "official") || strings.HasPrefix(source, "area_contact") || strings.HasPrefix(source, "mapped_area"):
		return "official_direct"
	case verification == "directory_listing" || verification == "mapped_area_contact" || strings.HasPrefix(source, "google_places") || strings.Contains(source, "registry"):
		return "official_registry"
	default:
		return "heuristic"
	}
}

func scoreCaseEscalationTarget(profile caseRoutingProfile, target models.CaseEscalationTarget) float64 {
	score := target.ConfidenceScore * 0.32
	switch target.DecisionScope {
	case "site_ops":
		score += 0.42
	case "asset_owner":
		score += 0.34
	case "regulator":
		score += 0.36
	case "project_party":
		score += 0.2
	default:
		score += 0.12
	}

	switch target.AttributionClass {
	case "official_direct":
		score += 0.18
	case "official_registry":
		score += 0.1
	default:
		score -= 0.04
	}

	switch caseTargetChannel(target) {
	case "email":
		score += 0.06
	case "phone":
		score += 0.09
	case "website":
		score -= 0.12
	case "social":
		score -= 0.08
	}

	if profile.Structural && target.DecisionScope == "regulator" {
		score += 0.12
	}
	if profile.Structural && target.DecisionScope == "project_party" {
		score += 0.07
	}
	if profile.ImmediateDanger && target.RoleType == "public_safety" {
		score += 0.18
	}
	if profile.ImmediateDanger && target.RoleType == "fire_authority" {
		score += 0.16
	}
	if profile.SensitiveOccupancy && target.DecisionScope == "site_ops" {
		score += 0.08
	}
	if !profile.Structural && target.DecisionScope == "project_party" {
		score -= 0.18
	}
	return math.Max(0, math.Min(1, score))
}

func notifyTierForTarget(profile caseRoutingProfile, target models.CaseEscalationTarget) int {
	switch target.DecisionScope {
	case "site_ops", "asset_owner":
		return 1
	case "regulator":
		if profile.HazardMode == "emergency" {
			return 1
		}
		return 2
	case "project_party":
		return 3
	default:
		return 4
	}
}

func sendEligibilityForTarget(profile caseRoutingProfile, target models.CaseEscalationTarget) string {
	hasDirectChannel := strings.TrimSpace(target.Email) != "" || strings.TrimSpace(target.Phone) != ""
	switch target.DecisionScope {
	case "site_ops":
		if hasDirectChannel && target.AttributionClass != "heuristic" {
			return "auto"
		}
		if hasDirectChannel {
			return "review"
		}
		return "hold"
	case "asset_owner":
		if hasDirectChannel && target.AttributionClass == "official_direct" {
			return "auto"
		}
		if hasDirectChannel {
			return "review"
		}
		return "hold"
	case "regulator":
		if hasDirectChannel && (profile.Severe || profile.Urgent || profile.ImmediateDanger) && target.AttributionClass != "heuristic" {
			return "auto"
		}
		if hasDirectChannel && target.AttributionClass != "heuristic" {
			return "review"
		}
		return "hold"
	case "project_party":
		if !profile.Structural {
			return "hold"
		}
		if hasDirectChannel && target.AttributionClass != "heuristic" {
			return "review"
		}
		return "hold"
	default:
		if hasDirectChannel && target.AttributionClass == "official_direct" {
			return "review"
		}
		return "hold"
	}
}

func buildCaseTargetReason(profile caseRoutingProfile, target models.CaseEscalationTarget) string {
	scopeLabel := map[string]string{
		"site_ops":      "can act directly at the site",
		"asset_owner":   "appears accountable for the asset",
		"regulator":     "has oversight or enforcement responsibility",
		"project_party": "is connected to the design or construction chain",
	}[target.DecisionScope]
	if scopeLabel == "" {
		scopeLabel = "is plausibly relevant to this case"
	}
	reasons := []string{scopeLabel}
	switch target.AttributionClass {
	case "official_direct":
		reasons = append(reasons, "sourced from an official contact surface")
	case "official_registry":
		reasons = append(reasons, "sourced from a directory or registry-style official page")
	default:
		reasons = append(reasons, "sourced from heuristic discovery")
	}
	if profile.Structural && target.DecisionScope == "regulator" {
		reasons = append(reasons, "structural risk warrants authority oversight")
	}
	if profile.Structural && target.DecisionScope == "project_party" {
		reasons = append(reasons, "structural risk makes project-chain stakeholders relevant")
	}
	if profile.ImmediateDanger && target.RoleType == "public_safety" {
		reasons = append(reasons, "immediate danger cues warrant public-safety awareness")
	}
	return strings.Join(reasons, "; ")
}

func buildCaseNotifyPlanSummary(profile caseRoutingProfile, items []models.CaseNotifyPlanItem) string {
	selected := 0
	authorities := 0
	for _, item := range items {
		if item.Selected {
			selected++
		}
		if item.DecisionScope == "regulator" {
			authorities++
		}
	}
	switch profile.HazardMode {
	case "emergency":
		return fmt.Sprintf("Immediate-response plan prioritizes %d direct operators/owners first, with %d authority targets ready in the next wave.", selected, authorities)
	case "urgent":
		return fmt.Sprintf("Urgent notify plan focuses on %d direct operators/owners now and keeps %d authority stakeholders queued for escalation.", selected, authorities)
	default:
		return fmt.Sprintf("Notify plan recommends %d primary contacts now, with %d authority or oversight stakeholders retained for escalation if needed.", selected, authorities)
	}
}

func shouldSelectTargetForNotifyPlan(profile caseRoutingProfile, target models.CaseEscalationTarget, selectedCountByScope map[string]int) bool {
	if target.SendEligibility != "auto" {
		return false
	}
	scope := target.DecisionScope
	limit := 1
	switch scope {
	case "site_ops":
		limit = 3
	case "asset_owner":
		limit = 2
	case "regulator":
		if profile.HazardMode == "emergency" {
			limit = 2
		} else {
			limit = 1
		}
	default:
		limit = 1
	}
	return selectedCountByScope[scope] < limit
}

func caseTargetChannel(target models.CaseEscalationTarget) string {
	return strings.ToLower(strings.TrimSpace(emptyStringDefault(target.Channel, caseEscalationTargetFallbackChannel(target))))
}

func caseEscalationTargetFallbackChannel(target models.CaseEscalationTarget) string {
	switch {
	case strings.TrimSpace(target.Email) != "":
		return "email"
	case strings.TrimSpace(target.Phone) != "":
		return "phone"
	case strings.TrimSpace(target.SocialHandle) != "":
		return "social"
	case strings.TrimSpace(target.Website) != "" || strings.TrimSpace(target.ContactURL) != "":
		return "website"
	default:
		return ""
	}
}

func observationKeyForTarget(target models.CaseEscalationTarget) string {
	observation := models.CaseContactObservation{
		RoleType:         target.RoleType,
		DecisionScope:    target.DecisionScope,
		OrganizationName: target.Organization,
		PersonName:       target.DisplayName,
		ChannelType:      caseTargetChannel(target),
		Email:            target.Email,
		Phone:            target.Phone,
		Website:          target.Website,
		ContactURL:       target.ContactURL,
		SocialPlatform:   target.SocialPlatform,
		SocialHandle:     target.SocialHandle,
	}
	return observationKeyForTargetObservation(observation)
}

func observationKeyForTargetObservation(observation models.CaseContactObservation) string {
	channelType := strings.ToLower(strings.TrimSpace(observation.ChannelType))
	channelValue := strings.ToLower(strings.TrimSpace(observation.ChannelValue))
	if channelValue == "" {
		switch channelType {
		case "email":
			channelValue = strings.ToLower(strings.TrimSpace(observation.Email))
		case "phone":
			channelValue = strings.ToLower(strings.TrimSpace(observation.Phone))
		case "website":
			channelValue = strings.ToLower(strings.TrimSpace(observation.Website))
			if channelValue == "" {
				channelValue = strings.ToLower(strings.TrimSpace(observation.ContactURL))
			}
		case "social":
			channelValue = strings.ToLower(strings.TrimSpace(observation.SocialHandle))
		}
	}
	if channelType != "" && channelValue != "" {
		return channelType + ":" + channelValue
	}
	return strings.ToLower(strings.TrimSpace(observation.OrganizationName)) + ":" +
		strings.ToLower(strings.TrimSpace(observation.PersonName))
}
