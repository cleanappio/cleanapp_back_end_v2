package models

import (
	geojson "github.com/paulmach/go.geojson"
)

type ContactEmail struct {
	Email         string `json:"email"`
	ConsentReport bool   `json:"consent_report"`
}

type Area struct {
	Id             uint64           `json:"id"`
	Name           string           `json:"name"`
	Description    string           `json:"description"`
	IsCustom       bool             `json:"is_custom"`
	ContactName    string           `json:"contact_name"`
	ContractEmails []*ContactEmail  `json:"contact_emails"`
	Coordinates    *geojson.Feature `json:"coordinates"`
	CreatedAt      string           `json:"created_at"`
	UpdatedAt      string           `json:"updated_at"`
}

type CreateAreaRequest struct {
	Version string `json:"version"` // Must be "2.0"
	Area    *Area  `json:"area"`
}

type UpdateConsentRequest struct {
	Version      string        `json:"version"` // Must be "2.0"
	ContactEmail *ContactEmail `json:"contact_email"`
}

type AreasResponse struct {
	Areas []Area `json:"areas"`
}

type AreasCountResponse struct {
	Count uint64 `json:"count"`
}

type ViewPort struct {
	LatMin float64 `json:"latmin"`
	LonMin float64 `json:"lonmin"`
	LatMax float64 `json:"latmax"`
	LonMax float64 `json:"lonmax"`
}
