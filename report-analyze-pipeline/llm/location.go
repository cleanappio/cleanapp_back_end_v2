package llm

import "fmt"

type LocationContext struct {
	PrimaryName  string `json:"primary_name"`
	ParentOrg    string `json:"parent_org"`
	Operator     string `json:"operator"`
	Domain       string `json:"domain"`
	ContactEmail string `json:"contact_email"`
	LocationType string `json:"location_type"`
	City         string `json:"city"`
	State        string `json:"state"`
	Country      string `json:"country"`
}

type LocationAnalyzer interface {
	AnalyzeImageWithLocation([]byte, string, *LocationContext) (string, error)
}

func LocationPrompt(locCtx *LocationContext) string {
	locationContextStr := ""
	if locCtx != nil && (locCtx.PrimaryName != "" || locCtx.ParentOrg != "" || locCtx.Domain != "") {
		locationContextStr = "\n\nLOCATION_CONTEXT (use this for responsible-party attribution and any explicitly provided contacts):\n"
		if locCtx.PrimaryName != "" {
			locationContextStr += fmt.Sprintf("- Primary Location: %s\n", locCtx.PrimaryName)
		}
		if locCtx.ParentOrg != "" {
			locationContextStr += fmt.Sprintf("- Parent Organization: %s\n", locCtx.ParentOrg)
		}
		if locCtx.Operator != "" {
			locationContextStr += fmt.Sprintf("- Operator: %s\n", locCtx.Operator)
		}
		if locCtx.Domain != "" {
			locationContextStr += fmt.Sprintf("- Domain: %s\n", locCtx.Domain)
		}
		if locCtx.ContactEmail != "" {
			locationContextStr += fmt.Sprintf("- Known Contact Email: %s\n", locCtx.ContactEmail)
		}
		if locCtx.LocationType != "" {
			locationContextStr += fmt.Sprintf("- Location Type: %s\n", locCtx.LocationType)
		}
		if locCtx.City != "" {
			locationContextStr += fmt.Sprintf("- City: %s\n", locCtx.City)
		}
		if locCtx.State != "" {
			locationContextStr += fmt.Sprintf("- State: %s\n", locCtx.State)
		}
		if locCtx.Country != "" {
			locationContextStr += fmt.Sprintf("- Country: %s\n", locCtx.Country)
		}
	}

	return locationContextStr
}
