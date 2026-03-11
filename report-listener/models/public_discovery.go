package models

type PublicDiscoveryCard struct {
	DiscoveryToken string   `json:"discovery_token"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Classification string   `json:"classification"`
	SeverityLevel  float64  `json:"severity_level"`
	Timestamp      string   `json:"timestamp"`
	BrandName      string   `json:"brand_name,omitempty"`
	BrandDisplay   string   `json:"brand_display_name,omitempty"`
	Latitude       *float64 `json:"latitude,omitempty"`
	Longitude      *float64 `json:"longitude,omitempty"`
}

type PublicDiscoveryBatch struct {
	Items               []PublicDiscoveryCard `json:"items"`
	Count               int                   `json:"count"`
	TotalCount          int                   `json:"total_count,omitempty"`
	HighPriorityCount   int                   `json:"high_priority_count,omitempty"`
	MediumPriorityCount int                   `json:"medium_priority_count,omitempty"`
}

type PublicPhysicalPoint struct {
	Kind           string  `json:"kind"`
	Classification string  `json:"classification"`
	MarkerToken    string  `json:"marker_token,omitempty"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	SeverityLevel  float64 `json:"severity_level"`
	Count          int     `json:"count,omitempty"`
}

type PublicBrandSummary struct {
	Classification string `json:"classification"`
	DiscoveryToken string `json:"discovery_token"`
	BrandName      string `json:"brand_name"`
	BrandDisplay   string `json:"brand_display_name"`
	Total          int    `json:"total"`
}

type PublicDiscoveryResolveResponse struct {
	Kind           string `json:"kind"`
	Classification string `json:"classification"`
	PublicID       string `json:"public_id,omitempty"`
	BrandName      string `json:"brand_name,omitempty"`
	CanonicalPath  string `json:"canonical_path"`
}
