package api

const (
	UserEndpoint          = "/update_or_create_user"
	ReportEndpoint        = "/report"
	GetMapEndpoint        = "/get_map"
	ReadReferralEndpoint  = "/readreferral"
	WriteReferralEndpoint = "/writereferral"
)

type ViewPort struct {
	LatTop    float64 `json:"lattop"`
	LonLeft   float64 `json:"lonleft"`
	LatBottom float64 `json:"latbottom"`
	LonRight  float64 `json:"lonright"`
}

type MapArgs struct {
	Version string   `json:"version"` // Must be "2.0"
	Id      string   `json:"id"`      // public key.
	VPort   ViewPort `json:"vport"`
}

type ReferralQuery struct {
	RefKey string `form:"refkey"`
}

type ReferralResult struct {
	RefValue string `json:"refvalue"`
}

type ReferralData struct {
	RefKey   string `json:"refkey"`
	RefValue string `json:"refvalue"`
}

type ReportArgs struct {
	Version  string  `json:"version"` // Must be "2.0"
	Id       string  `json:"id"`      // public key.
	Latitude float64 `json:"latitude"`
	Longitue float64 `json:"longitude"`
	X        int32   `json:"x"`
	Y        int32   `json:"y"`
	Image    []byte  `json:"image"`
}

type UserArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
	Avatar  string `json:"avatar"`
}
