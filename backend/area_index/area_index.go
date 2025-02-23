package area_index

import (
	"cleanapp/backend/server/api"
	"fmt"
	"strings"
)

func PointToWKT(longitude, latitude float64) string {
	return fmt.Sprintf("POINT(%g %g)", latitude, longitude)
}

func ViewPortToWKT(vp *api.ViewPort) string {
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

func AreaToWKT(area *api.Area) (string, error) {
	if area.Coordinates.Geometry.IsPolygon() {
		return PolygonToWKT(area), nil
	} else if area.Coordinates.Geometry.IsMultiPolygon() {
		return MultiPolygonToWKT(area), nil
	} else {
		return "", fmt.Errorf("unsupported geometry type: %s", area.Coordinates.Geometry.Type)
	}
}

func PolygonToWKT(area *api.Area) string {
	return fmt.Sprintf("POLYGON(%s)", innerWKT(area.Coordinates.Geometry.Polygon))
}

func MultiPolygonToWKT(area *api.Area) string {
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
