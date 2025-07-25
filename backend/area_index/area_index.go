package area_index

import (
	"fmt"
	"strings"

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

type CreateAreaResponse struct {
	AreaId  uint64 `json:"area_id"`
	Message string `json:"message"`
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

func PointToWKT(longitude, latitude float64) string {
	return fmt.Sprintf("POINT(%g %g)", latitude, longitude)
}

func ViewPortToWKT(vp *ViewPort) string {
	poly := [][][]float64{
		{
			{vp.LonMin, vp.LatMin},
			{vp.LonMax, vp.LatMin},
			{vp.LonMax, vp.LatMax},
			{vp.LonMin, vp.LatMax},
			{vp.LonMin, vp.LatMin},
		},
	}
	return fmt.Sprintf("POLYGON(%s)", innerWKT(poly))
}

func AreaToWKT(area *Area) (string, error) {
	if area.Coordinates.Geometry.IsPolygon() {
		return PolygonToWKT(area), nil
	} else if area.Coordinates.Geometry.IsMultiPolygon() {
		return MultiPolygonToWKT(area), nil
	} else {
		return "", fmt.Errorf("unsupported geometry type: %s", area.Coordinates.Geometry.Type)
	}
}

func PolygonToWKT(area *Area) string {
	return fmt.Sprintf("POLYGON(%s)", innerWKT(area.Coordinates.Geometry.Polygon))
}

func MultiPolygonToWKT(area *Area) string {
	wktPolys := make([]string, len(area.Coordinates.Geometry.MultiPolygon))
	for i, poly := range area.Coordinates.Geometry.MultiPolygon {
		wktPolys[i] = fmt.Sprintf("(%s)", innerWKT(poly))
	}
	return fmt.Sprintf("MULTIPOLYGON(%s)", strings.Join(wktPolys, ","))
}

func innerWKT(poly [][][]float64) string {
	wktList := make([][]string, len(poly))
	for i, loop := range poly {
		wktList[i] = make([]string, len(loop))
		for j, point := range loop {
			wktList[i][j] = fmt.Sprintf("%g %g", point[1], point[0])
		}
	}
	wktLoops := make([]string, len(wktList))
	for i, wktPairs := range wktList {
		wktLoops[i] = fmt.Sprintf("(%s)", strings.Join(wktPairs, ","))
	}
	return strings.Join(wktLoops, ",")
}
