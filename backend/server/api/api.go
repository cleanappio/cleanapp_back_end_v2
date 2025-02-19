package api

import (
	"cleanapp/backend/util"

	geojson "github.com/paulmach/go.geojson"
)

type ActionModifyArgs struct {
	Version string       `json:"version"` // Must be 2.0
	Record  ActionRecord `json:"record"`
}

type ActionModifyResponse struct {
	Record ActionRecord `json:"record"`
}

type ActionRecord struct {
	Id             string `json:"id"`
	Name           string `json:"name"`
	IsActive       bool   `json:"is_active"`
	ExpirationDate string `json:"expiration_date"`
}

type ActionsResponse struct {
	Records []ActionRecord `json:"records"`
}

type BaseArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
}

type BlockchainLinkResponse struct {
	BlockchainLink string `json:"blockchain_link"`
}

type MapArgs struct {
	Version string   `json:"version"` // Must be "2.0"
	Id      string   `json:"id"`      // public key.
	VPort   ViewPort `json:"vport"`
	Center  Point    `json:"center"`
}

type MapResult struct {
	Latitude  float64        `json:"latitude"`
	Longitude float64        `json:"longitude"`
	Count     int64          `json:"count"`
	ReportID  int64          `json:"report_id"` // Ignored if Count > 1
	Team      util.TeamColor `json:"team"`      // Ignored if Count > 1
	Own       bool           `json:"own"`
}

type ViewPort struct {
	LatMin float64 `json:"latmin"`
	LonMin float64 `json:"lonmin"`
	LatMax float64 `json:"latmax"`
	LonMax float64 `json:"lonmax"`
}

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type TeamsResponse struct {
	Base  BaseArgs `json:"base"`
	Blue  int      `json:"blue"`
	Green int      `json:"green"`
}

type PrivacyAndTOCArgs struct {
	Version  string `json:"version"` // Must be "2.0"
	Id       string `json:"id"`      // public key.
	Privacy  string `json:"privacy"`
	AgreeTOC string `json:"agree_toc"`
}

type ReadReportArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // User public address
	Seq     int    `json:"seq"`     // Report ID.
}

type ReadReportResponse struct {
	Id       string `json:"id"`
	Avatar   string `json:"avatar"`
	Own      bool   `json:"own"`
	ActionId string `json:"action_id"`
	Image    []byte `json:"image"`
}

type ReferralQuery struct {
	RefKey string `form:"refkey"` // A key in format <IPAddress>:<screenwidth>:<screenheight>
}

type ReferralResult struct {
	RefValue string `json:"refvalue"` // A referral code, example: aSvd3B6fEhJ
}

type ReferralData struct {
	RefKey   string `json:"refkey"`   // A key in format <IPAddress>:<screenwidth>:<screenheight>
	RefValue string `json:"refvalue"` // A referral code, example: aSvd3B6fEhJ
}

type GenRefRequest struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
}

type GenRefResponse struct {
	RefValue string `json:"refvalue"` // A referral code, example: aSvd3B6fEhJ
}

type ReportArgs struct {
	Version  string  `json:"version"` // Must be "2.0"
	Id       string  `json:"id"`      // public key.
	Latitude float64 `json:"latitude"`
	Longitue float64 `json:"longitude"`
	X        float64 `json:"x"` // 0.0..1.0
	Y        float64 `json:"y"` // 0.0..1.0
	Image    []byte  `json:"image"`
	ActionId string  `json:"action_id"`
}

type StatsArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
}

type StatsResponse struct {
	Version           string  `json:"version"` // Must be "2.0"
	Id                string  `json:"id"`      // public key.
	KitnsDaily        int     `json:"kitns_daily"`
	KitnsDisbursed    int     `json:"kitns_disbursed"`
	KitnsRefDaily     float64 `json:"kitns_ref_daily"`
	KitnsRefDisbusded float64 `json:"kitns_ref_disbursed"`
}

type TopScoresRecord struct {
	Place int     `json:"place"`
	Title string  `json:"title"`
	Kitn  float64 `json:"kitn"`
	IsYou bool    `json:"is_you"`
}

type TopScoresResponse struct {
	Records []TopScoresRecord `json:"records"`
}

type UserActionArgs struct {
	Version  string `json:"version"` // Must be "2.0"
	Id       string `json:"id"`      // public key.
	ActionId string `json:"action_id"`
}

type UserArgs struct {
	Version  string `json:"version"` // Must be "2.0"
	Id       string `json:"id"`      // public key.
	Avatar   string `json:"avatar"`
	Referral string `json:"referral"`
}

type UserResp struct {
	Team      util.TeamColor `json:"team"` // Blue or Green
	DupAvatar bool           `json:"dup_avatar"`
}

// Responsibility Areas API

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
