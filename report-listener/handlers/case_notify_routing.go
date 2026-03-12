package handlers

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"report-listener/models"
)

type caseRoutingProfile struct {
	Classification     string
	DefectClass        string
	DefectMode         string
	ExposureMode       string
	SeverityBand       string
	UrgencyBand        string
	JurisdictionKey    string
	Structural         bool
	Severe             bool
	Urgent             bool
	ImmediateDanger    bool
	SensitiveOccupancy bool
	AssetClass         string
	HazardMode         string
}

func (h *Handlers) syncCaseContactStrategy(ctx context.Context, detail *models.CaseDetail) error {
	if detail == nil || strings.TrimSpace(detail.Case.CaseID) == "" {
		return nil
	}
	if len(detail.EscalationTargets) == 0 {
		return nil
	}

	profile := buildCaseRoutingProfile(detail)
	routedTargets := h.routeTargetsForSubject(ctx, profile, detail.EscalationTargets)
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

	storedProfile, err := h.db.UpsertSubjectRoutingProfile(ctx, subjectRoutingProfileModel("case", detail.Case.CaseID, profile))
	if err != nil {
		return err
	}

	executionTasks, err := h.db.ReplaceNotifyExecutionTasks(ctx, "case", detail.Case.CaseID, buildNotifyExecutionTasks("case", detail.Case.CaseID, storedTargets, storedPlan))
	if err != nil {
		return err
	}

	notifyOutcomes, err := h.db.ListNotifyOutcomes(ctx, "case", detail.Case.CaseID)
	if err != nil {
		return err
	}

	detail.EscalationTargets = storedTargets
	detail.ContactObservations = storedObservations
	detail.NotifyPlan = storedPlan
	detail.RoutingProfile = storedProfile
	detail.ExecutionTasks = executionTasks
	detail.NotifyOutcomes = notifyOutcomes
	return nil
}

func routeCaseEscalationTargets(detail *models.CaseDetail) []models.CaseEscalationTarget {
	if detail == nil {
		return nil
	}
	return routeTargetsWithProfile(buildCaseRoutingProfile(detail), detail.EscalationTargets)
}

func routeReportEscalationTargets(report *models.ReportWithAnalysis, targets []models.CaseEscalationTarget) []models.CaseEscalationTarget {
	return routeTargetsWithProfile(buildReportRoutingProfile(report), targets)
}

func routeTargetsWithProfile(profile caseRoutingProfile, source []models.CaseEscalationTarget) []models.CaseEscalationTarget {
	targets := make([]models.CaseEscalationTarget, 0, len(source))
	for _, target := range source {
		routed := target
		routed.EndpointKey = emptyStringDefault(routed.EndpointKey, endpointKeyForTarget(routed))
		routed.OrganizationKey = emptyStringDefault(routed.OrganizationKey, organizationKeyForTarget(routed))
		routed.DecisionScope = emptyStringDefault(routed.DecisionScope, decisionScopeForRoleType(routed.RoleType))
		routed.AttributionClass = emptyStringDefault(routed.AttributionClass, attributionClassForTarget(routed))
		routed.SiteMatchScore = scoreSiteMatch(profile, routed)
		routed.SourceQualityScore = scoreSourceQuality(routed)
		routed.RoleFitScore = scoreRoleFit(profile, routed)
		routed.ChannelQualityScore = scoreChannelQuality(routed)
		if routed.OutcomeMemoryScore == 0 {
			routed.OutcomeMemoryScore = defaultOutcomeMemoryScore(routed)
		}
		routed.ActionabilityScore = scoreCaseEscalationTarget(profile, routed)
		routed.NotifyTier = notifyTierForTarget(profile, routed)
		routed.SendEligibility = sendEligibilityForTarget(profile, routed)
		routed.ExecutionMode = executionModeForTarget(profile, routed)
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

func (h *Handlers) routeTargetsForSubject(ctx context.Context, profile caseRoutingProfile, source []models.CaseEscalationTarget) []models.CaseEscalationTarget {
	enriched := make([]models.CaseEscalationTarget, 0, len(source))
	for _, target := range source {
		routed := target
		routed.EndpointKey = emptyStringDefault(routed.EndpointKey, endpointKeyForTarget(routed))
		routed.OrganizationKey = emptyStringDefault(routed.OrganizationKey, organizationKeyForTarget(routed))
		if memory, err := h.db.GetContactEndpointMemory(ctx, routed.EndpointKey); err == nil && memory != nil {
			routed.OutcomeMemoryScore = scoreOutcomeMemory(memory, routed.RoleType, profile.AssetClass)
			if memory.CooldownUntil != nil {
				routed.CooldownUntil = memory.CooldownUntil
			}
		} else if routed.OutcomeMemoryScore == 0 {
			routed.OutcomeMemoryScore = defaultOutcomeMemoryScore(routed)
		}
		enriched = append(enriched, routed)
	}
	return routeTargetsWithProfile(profile, enriched)
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
		Classification:     detail.Case.Classification,
		DefectClass:        inferDefectClass(detail.Case.Classification, joined),
		DefectMode:         inferDefectMode(detail.Case.Classification, joined),
		ExposureMode:       inferExposureMode(joined),
		SeverityBand:       scoreBand(detail.Case.SeverityScore, maxReportSeverity),
		UrgencyBand:        urgencyBand(detail.Case.UrgencyScore, joined),
		JurisdictionKey:    inferJurisdictionKeyFromCase(detail),
		Structural:         containsAny(joined, []string{"structural", "crack", "collapse", "column", "beam", "facade", "brick", "concrete", "wall"}),
		Severe:             detail.Case.SeverityScore >= 0.8 || maxReportSeverity >= 0.8,
		Urgent:             detail.Case.UrgencyScore >= 0.7,
		ImmediateDanger:    containsAny(joined, []string{"falling", "imminent", "collapse", "separating", "detached", "support column", "support beam", "exposed", "children", "occupants"}),
		SensitiveOccupancy: containsAny(joined, []string{"school", "hospital", "station", "metro", "airport", "playground", "terminal", "mall", "daycare", "nursery"}),
		AssetClass:         inferAssetClassFromText(joined),
		HazardMode:         "standard",
	}
	switch {
	case profile.ImmediateDanger && profile.Severe:
		profile.HazardMode = "emergency"
	case profile.Severe || profile.Urgent:
		profile.HazardMode = "urgent"
	}
	if profile.DefectMode == "" {
		profile.DefectMode = profile.HazardMode
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
		Classification:     firstNonEmpty(reportPrimaryClassification(report), "physical"),
		DefectClass:        inferDefectClass(firstNonEmpty(reportPrimaryClassification(report), "physical"), joined),
		DefectMode:         inferDefectMode(firstNonEmpty(reportPrimaryClassification(report), "physical"), joined),
		ExposureMode:       inferExposureMode(joined),
		SeverityBand:       scoreBand(maxSeverity, maxSeverity),
		UrgencyBand:        urgencyBand(maxSeverity, joined),
		JurisdictionKey:    inferJurisdictionKeyFromReport(report),
		Structural:         containsAny(joined, []string{"structural", "crack", "collapse", "column", "beam", "facade", "brick", "concrete", "wall", "support"}),
		Severe:             maxSeverity >= 0.8,
		Urgent:             maxSeverity >= 0.7 || containsAny(joined, []string{"urgent", "danger", "hazard", "critical"}),
		ImmediateDanger:    containsAny(joined, []string{"falling", "imminent", "collapse", "separating", "detached", "support column", "support beam", "load-bearing", "exposed"}),
		SensitiveOccupancy: containsAny(joined, []string{"school", "hospital", "station", "metro", "airport", "playground", "terminal", "mall", "daycare", "nursery", "children", "commuter"}),
		AssetClass:         inferAssetClassFromText(joined),
		HazardMode:         "standard",
	}
	switch {
	case profile.ImmediateDanger && profile.Severe:
		profile.HazardMode = "emergency"
	case profile.Severe || profile.Urgent:
		profile.HazardMode = "urgent"
	}
	if profile.DefectMode == "" {
		profile.DefectMode = profile.HazardMode
	}
	return profile
}

func decisionScopeForRoleType(roleType string) string {
	switch strings.ToLower(strings.TrimSpace(roleType)) {
	case "operator", "operator_admin", "site_leadership", "facility_manager", "transit_authority", "public_works", "traffic_authority", "infrastructure_authority", "support", "engineering", "product_owner", "trust_safety", "security":
		return "site_ops"
	case "owner", "property_owner", "landlord":
		return "asset_owner"
	case "building_authority", "public_safety", "fire_authority", "transit_safety":
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
	score := 0.30*target.SourceQualityScore +
		0.25*target.RoleFitScore +
		0.20*target.SiteMatchScore +
		0.15*target.ChannelQualityScore +
		0.10*target.OutcomeMemoryScore
	score += target.ConfidenceScore * 0.08
	if profile.ImmediateDanger && target.DecisionScope == "regulator" {
		score += 0.08
	}
	if profile.DefectClass == "digital_security" && (target.RoleType == "security" || target.RoleType == "trust_safety") {
		score += 0.08
	}
	if profile.DefectClass == "digital_product_bug" && (target.RoleType == "support" || target.RoleType == "engineering" || target.RoleType == "product_owner") {
		score += 0.07
	}
	if profile.DefectClass == "digital_accessibility" && (target.RoleType == "support" || target.RoleType == "product_owner") {
		score += 0.06
	}
	if profile.Structural && target.DecisionScope == "project_party" {
		score += 0.04
	}
	if !profile.Structural && target.DecisionScope == "project_party" {
		score -= 0.14
	}
	if target.CooldownUntil != nil && target.CooldownUntil.After(time.Now().UTC()) {
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
		if profile.DefectClass == "digital_security" && (target.RoleType == "security" || target.RoleType == "trust_safety") {
			return 1
		}
		return 4
	}
}

func sendEligibilityForTarget(profile caseRoutingProfile, target models.CaseEscalationTarget) string {
	hasDirectChannel := strings.TrimSpace(target.Email) != "" || strings.TrimSpace(target.Phone) != "" || strings.TrimSpace(target.ContactURL) != ""
	if target.CooldownUntil != nil && target.CooldownUntil.After(time.Now().UTC()) {
		return "hold"
	}
	switch target.DecisionScope {
	case "site_ops":
		if hasDirectChannel && target.AttributionClass != "heuristic" && target.ActionabilityScore >= 0.72 {
			return "auto"
		}
		if hasDirectChannel {
			return "review"
		}
		return "hold"
	case "asset_owner":
		if hasDirectChannel && target.AttributionClass == "official_direct" && target.ActionabilityScore >= 0.74 {
			return "auto"
		}
		if hasDirectChannel {
			return "review"
		}
		return "hold"
	case "regulator":
		if hasDirectChannel && (profile.Severe || profile.Urgent || profile.ImmediateDanger) && target.AttributionClass != "heuristic" && target.ActionabilityScore >= 0.75 {
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
		if hasDirectChannel && target.AttributionClass == "official_direct" && target.ActionabilityScore >= 0.68 {
			return "review"
		}
		return "hold"
	}
}

func executionModeForTarget(profile caseRoutingProfile, target models.CaseEscalationTarget) string {
	switch caseTargetChannel(target) {
	case "email":
		if target.SendEligibility == "auto" {
			return "auto"
		}
		return "review"
	case "phone":
		return "task"
	case "website":
		if strings.TrimSpace(target.ContactURL) != "" {
			return "task"
		}
		return "hold"
	case "social":
		return "review"
	default:
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
	if strings.HasPrefix(profile.DefectClass, "digital_") && target.RoleType == "support" {
		reasons = append(reasons, "support channel is the fastest credible path for a digital defect")
	}
	if profile.DefectClass == "digital_security" && target.RoleType == "security" {
		reasons = append(reasons, "security-sensitive defects should reach security response directly")
	}
	if target.ExecutionMode == "task" {
		reasons = append(reasons, "best handled as a structured follow-up task rather than an immediate email blast")
	}
	if profile.Structural && target.DecisionScope == "project_party" {
		reasons = append(reasons, "structural risk makes project-chain stakeholders relevant")
	}
	if profile.AssetClass == "transit_station" && target.RoleType == "transit_authority" {
		reasons = append(reasons, "transit infrastructure hazards are best routed to the station operator")
	}
	if (profile.AssetClass == "roadway" || profile.AssetClass == "bridge") && (target.RoleType == "public_works" || target.RoleType == "traffic_authority" || target.RoleType == "infrastructure_authority") {
		reasons = append(reasons, "road and bridge hazards should reach the infrastructure operator quickly")
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
		if strings.HasPrefix(profile.DefectClass, "digital_") {
			return fmt.Sprintf("Notify plan recommends %d primary product/operator contacts now, with %d authority or oversight stakeholders retained for escalation if the issue persists.", selected, authorities)
		}
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

func endpointKeyForTarget(target models.CaseEscalationTarget) string {
	return caseEscalationTargetKey(target)
}

func organizationKeyForTarget(target models.CaseEscalationTarget) string {
	return strings.ToLower(strings.TrimSpace(firstNonEmpty(target.Organization, target.DisplayName)))
}

func scoreSourceQuality(target models.CaseEscalationTarget) float64 {
	switch target.AttributionClass {
	case "official_direct":
		return 1.0
	case "official_registry":
		return 0.85
	case "verified_directory":
		return 0.7
	case "legacy_inferred":
		return 0.25
	default:
		return 0.45
	}
}

func scoreChannelQuality(target models.CaseEscalationTarget) float64 {
	switch caseTargetChannel(target) {
	case "email":
		if target.AttributionClass == "official_direct" {
			return 1.0
		}
		return 0.9
	case "website":
		if strings.TrimSpace(target.ContactURL) != "" {
			return 0.8
		}
		return 0.5
	case "phone":
		return 0.75
	case "social":
		return 0.55
	default:
		return 0.3
	}
}

func scoreSiteMatch(profile caseRoutingProfile, target models.CaseEscalationTarget) float64 {
	score := 0.2
	corpus := strings.ToLower(strings.Join([]string{
		target.Organization,
		target.DisplayName,
		target.Website,
		target.ContactURL,
		target.SourceURL,
		target.EvidenceText,
	}, " "))
	jurisdictionTokens := significantOrganizationTokens(profile.JurisdictionKey)
	if tokenMatchCount(corpus, jurisdictionTokens) > 0 {
		score += 0.15
	}
	assetTokens := significantOrganizationTokens(profile.AssetClass)
	if tokenMatchCount(corpus, assetTokens) > 0 {
		score += 0.1
	}
	if target.AttributionClass == "official_direct" {
		score += 0.2
	}
	if profile.AssetClass != "" && strings.Contains(corpus, strings.ReplaceAll(profile.AssetClass, "_", " ")) {
		score += 0.15
	}
	return math.Max(0.2, math.Min(1, score))
}

func scoreRoleFit(profile caseRoutingProfile, target models.CaseEscalationTarget) float64 {
	roleType := strings.ToLower(strings.TrimSpace(target.RoleType))
	if profile.AssetClass == "transit_station" {
		switch roleType {
		case "transit_authority":
			return 1.0
		case "transit_safety":
			return 0.94
		case "public_safety", "fire_authority":
			return 0.84
		case "building_authority":
			return 0.66
		}
	}
	if profile.AssetClass == "roadway" || profile.AssetClass == "bridge" {
		switch roleType {
		case "public_works", "traffic_authority", "infrastructure_authority":
			return 0.97
		case "public_safety":
			return 0.84
		}
	}
	if profile.AssetClass == "school" {
		switch roleType {
		case "facility_manager", "operator":
			return 0.98
		case "building_authority":
			return 0.9
		case "public_safety", "fire_authority":
			return 0.82
		}
	}
	switch profile.DefectClass {
	case "digital_security":
		switch roleType {
		case "security":
			return 1.0
		case "trust_safety":
			return 0.88
		case "support":
			return 0.7
		case "engineering":
			return 0.8
		}
	case "digital_product_bug":
		switch roleType {
		case "support":
			return 0.95
		case "engineering":
			return 0.9
		case "product_owner":
			return 0.82
		case "operator":
			return 0.78
		}
	case "digital_accessibility":
		switch roleType {
		case "support":
			return 0.9
		case "product_owner":
			return 0.88
		case "engineering":
			return 0.8
		}
	default:
		switch target.DecisionScope {
		case "site_ops":
			return 0.92
		case "asset_owner":
			return 0.8
		case "regulator":
			if profile.Severe || profile.Urgent || profile.ImmediateDanger {
				return 0.9
			}
			return 0.72
		case "project_party":
			if profile.Structural {
				return 0.7
			}
			return 0.4
		}
	}
	return 0.55
}

func defaultOutcomeMemoryScore(target models.CaseEscalationTarget) float64 {
	if strings.EqualFold(strings.TrimSpace(target.TargetSource), "inferred_contact") {
		return 0.35
	}
	return 0.6
}

func inferDefectClass(classification, corpus string) string {
	classification = strings.ToLower(strings.TrimSpace(classification))
	switch {
	case classification == "digital" && containsAny(corpus, []string{"vulnerability", "security", "xss", "sql injection", "credential", "takeover", "data leak", "phishing", "fraud"}):
		return "digital_security"
	case classification == "digital" && containsAny(corpus, []string{"accessibility", "screen reader", "contrast", "wcag", "keyboard navigation", "aria"}):
		return "digital_accessibility"
	case classification == "digital":
		return "digital_product_bug"
	case containsAny(corpus, []string{"structural", "collapse", "crack", "beam", "column", "facade", "brick", "load-bearing", "support"}):
		return "physical_structural"
	case containsAny(corpus, []string{"waste", "litter", "spill", "sanitation", "garbage", "odor", "sewage"}):
		return "physical_sanitation"
	case containsAny(corpus, []string{"accessible", "accessibility", "wheelchair", "trip hazard", "blocked exit"}):
		return "physical_accessibility"
	case containsAny(corpus, []string{"service outage", "downtime", "broken kiosk", "payment terminal", "escalator", "elevator"}):
		return "operational_service"
	default:
		return "physical_safety"
	}
}

func inferDefectMode(classification, corpus string) string {
	classification = strings.ToLower(strings.TrimSpace(classification))
	if classification == "digital" && containsAny(corpus, []string{"fraud", "scam", "impersonation", "fake"}) {
		return "abuse"
	}
	if containsAny(corpus, []string{"imminent", "falling", "collapse", "urgent", "critical"}) {
		return "emergency"
	}
	return "standard"
}

func inferExposureMode(corpus string) string {
	switch {
	case containsAny(corpus, []string{"children", "commuter", "passenger", "public", "occupants", "users"}):
		return "public_exposure"
	case containsAny(corpus, []string{"customer", "user", "account", "checkout"}):
		return "user_exposure"
	default:
		return "localized"
	}
}

func scoreBand(primary, secondary float64) string {
	score := math.Max(primary, secondary)
	switch {
	case score >= 0.9:
		return "critical"
	case score >= 0.75:
		return "high"
	case score >= 0.45:
		return "medium"
	default:
		return "low"
	}
}

func urgencyBand(score float64, corpus string) string {
	switch {
	case score >= 0.85 || containsAny(corpus, []string{"immediate", "urgent", "critical", "emergency"}):
		return "immediate"
	case score >= 0.65:
		return "high"
	case score >= 0.35:
		return "medium"
	default:
		return "low"
	}
}

func inferJurisdictionKeyFromCase(detail *models.CaseDetail) string {
	if detail == nil {
		return ""
	}
	for _, target := range detail.EscalationTargets {
		if strings.TrimSpace(target.OrganizationKey) != "" {
			return target.OrganizationKey
		}
	}
	return ""
}

func inferJurisdictionKeyFromReport(report *models.ReportWithAnalysis) string {
	if report == nil {
		return ""
	}
	for _, target := range report.EscalationTargets {
		if strings.TrimSpace(target.OrganizationKey) != "" {
			return target.OrganizationKey
		}
	}
	return ""
}

func reportPrimaryClassification(report *models.ReportWithAnalysis) string {
	if report == nil {
		return ""
	}
	for _, analysis := range report.Analysis {
		if strings.TrimSpace(analysis.Classification) != "" {
			return analysis.Classification
		}
	}
	return ""
}

func subjectRoutingProfileModel(subjectKind, subjectRef string, profile caseRoutingProfile) models.SubjectRoutingProfile {
	contextJSON := fmt.Sprintf(
		`{"structural":%t,"severe":%t,"urgent":%t,"immediate_danger":%t,"sensitive_occupancy":%t,"hazard_mode":"%s"}`,
		profile.Structural,
		profile.Severe,
		profile.Urgent,
		profile.ImmediateDanger,
		profile.SensitiveOccupancy,
		profile.HazardMode,
	)
	return models.SubjectRoutingProfile{
		SubjectKind:     subjectKind,
		SubjectRef:      subjectRef,
		Classification:  emptyStringDefault(profile.Classification, "physical"),
		DefectClass:     emptyStringDefault(profile.DefectClass, "general_defect"),
		DefectMode:      emptyStringDefault(profile.DefectMode, "standard"),
		AssetClass:      emptyStringDefault(profile.AssetClass, "general_site"),
		JurisdictionKey: profile.JurisdictionKey,
		ExposureMode:    emptyStringDefault(profile.ExposureMode, "localized"),
		SeverityBand:    emptyStringDefault(profile.SeverityBand, "medium"),
		UrgencyBand:     emptyStringDefault(profile.UrgencyBand, "medium"),
		ContextJSON:     contextJSON,
	}
}

func buildNotifyExecutionTasks(subjectKind, subjectRef string, targets []models.CaseEscalationTarget, plan *models.CaseNotifyPlan) []models.NotifyExecutionTask {
	if subjectKind == "" || subjectRef == "" || plan == nil {
		return nil
	}
	targetsByID := make(map[int64]models.CaseEscalationTarget, len(targets))
	for _, target := range targets {
		targetsByID[target.ID] = target
	}
	tasks := make([]models.NotifyExecutionTask, 0)
	for _, item := range plan.Items {
		if !item.Selected || item.TargetID == nil {
			continue
		}
		target, ok := targetsByID[*item.TargetID]
		if !ok {
			continue
		}
		executionMode := emptyStringDefault(target.ExecutionMode, "review")
		if executionMode == "auto" || executionMode == "hold" {
			continue
		}
		payloadJSON := fmt.Sprintf(
			`{"target_id":%d,"organization":"%s","display_name":"%s","reason":"%s","channel":"%s"}`,
			target.ID,
			escapeJSONString(target.Organization),
			escapeJSONString(target.DisplayName),
			escapeJSONString(target.ReasonSelected),
			escapeJSONString(caseTargetChannel(target)),
		)
		tasks = append(tasks, models.NotifyExecutionTask{
			SubjectKind:   subjectKind,
			SubjectRef:    subjectRef,
			TargetID:      item.TargetID,
			WaveNumber:    item.WaveNumber,
			RoleType:      target.RoleType,
			ChannelType:   caseTargetChannel(target),
			ExecutionMode: executionMode,
			TaskStatus:    "pending",
			Summary:       buildExecutionTaskSummary(target),
			PayloadJSON:   payloadJSON,
		})
	}
	return tasks
}

func buildExecutionTaskSummary(target models.CaseEscalationTarget) string {
	label := firstNonEmpty(strings.TrimSpace(target.DisplayName), strings.TrimSpace(target.Organization), "stakeholder")
	switch caseTargetChannel(target) {
	case "phone":
		return fmt.Sprintf("Call %s", label)
	case "website":
		if strings.TrimSpace(target.ContactURL) != "" {
			return fmt.Sprintf("Submit contact form for %s", label)
		}
		return fmt.Sprintf("Review website contact path for %s", label)
	case "social":
		return fmt.Sprintf("Review social escalation path for %s", label)
	default:
		return fmt.Sprintf("Review outreach path for %s", label)
	}
}

func scoreOutcomeMemory(memory *models.ContactEndpointMemory, roleType, assetClass string) float64 {
	if memory == nil {
		return 0.6
	}
	score := 0.58
	score += math.Min(0.18, float64(memory.SuccessCount)*0.03)
	score += math.Min(0.16, float64(memory.AckCount)*0.04)
	score += math.Min(0.14, float64(memory.FixCount)*0.05)
	score -= math.Min(0.24, float64(memory.BounceCount)*0.08)
	score -= math.Min(0.18, float64(memory.MisrouteCount)*0.07)
	score -= math.Min(0.14, float64(memory.NoResponseCount)*0.04)
	if strings.EqualFold(strings.TrimSpace(memory.PreferredForRoleType), strings.TrimSpace(roleType)) {
		score += 0.08
	}
	if strings.EqualFold(strings.TrimSpace(memory.PreferredForAssetClass), strings.TrimSpace(assetClass)) {
		score += 0.06
	}
	if memory.CooldownUntil != nil && memory.CooldownUntil.After(time.Now().UTC()) {
		score -= 0.12
	}
	return math.Max(0.05, math.Min(1, score))
}

func reportSubjectRef(report *models.ReportWithAnalysis) string {
	if report == nil {
		return ""
	}
	if strings.TrimSpace(report.Report.PublicID) != "" {
		return strings.TrimSpace(report.Report.PublicID)
	}
	if report.Report.Seq > 0 {
		return fmt.Sprintf("%d", report.Report.Seq)
	}
	return ""
}

func escapeJSONString(value string) string {
	replacer := strings.NewReplacer(
		`\\`, `\\\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return replacer.Replace(value)
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
