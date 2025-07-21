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
	Type           string           `json:"type"`
	ContractEmails []*ContactEmail  `json:"contact_emails"`
	Coordinates    *geojson.Feature `json:"coordinates"`
	CreatedAt      string           `json:"created_at"`
	UpdatedAt      string           `json:"updated_at"`
}

type CreateAreaRequest struct {
	Area *Area `json:"area"`
}

type UpdateConsentRequest struct {
	ContactEmail *ContactEmail `json:"contact_email"`
}

type DeleteAreaRequest struct {
	AreaId uint64 `json:"area_id"`
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
