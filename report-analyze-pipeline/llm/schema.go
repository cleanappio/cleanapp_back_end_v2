package llm

// AnalysisSchema preserves the field names consumed by parser.ParseAnalysis,
// including the historical digital_bug_probabilty spelling.
func AnalysisSchema() map[string]any {
	props := map[string]any{}
	for _, name := range []string{"title", "description", "legal_risk_estimate"} {
		props[name] = map[string]any{"type": "string"}
	}
	for _, name := range []string{"brand_name", "location", "responsible_party"} {
		props[name] = map[string]any{"type": []string{"string", "null"}}
	}
	for _, name := range []string{"litter_probability", "hazard_probability", "digital_bug_probabilty", "severity_level"} {
		props[name] = map[string]any{"type": "number", "description": "Between 0 and 1 inclusive."}
	}
	props["classification"] = map[string]any{"type": "string", "enum": []string{"physical", "digital"}}
	props["is_valid"] = map[string]any{"type": "boolean"}
	for _, name := range []string{"inferred_contact_emails", "suggested_remediation"} {
		props[name] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	user := map[string]any{}
	userKeys := []string{"name", "email", "company", "role", "company_size"}
	for _, name := range userKeys {
		user[name] = map[string]any{"type": []string{"string", "null"}}
	}
	props["user_info"] = map[string]any{"type": "object", "properties": user, "required": userKeys, "additionalProperties": false}
	return map[string]any{"type": "object", "properties": props, "required": []string{"title", "description", "classification", "user_info", "location", "brand_name", "litter_probability", "hazard_probability", "digital_bug_probabilty", "severity_level", "legal_risk_estimate", "is_valid", "responsible_party", "inferred_contact_emails", "suggested_remediation"}, "additionalProperties": false}
}
