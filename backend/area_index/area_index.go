package area_index

import (
	"cleanapp/backend/server/api"

	"github.com/golang/geo/s2"
)

func AreaToIndex(area *api.Area) []s2.CellID {
	loops := []*s2.Loop{}
	for _, loop := range area.Coordinates.Geometry.Polygon {
		pts := []s2.Point{}
		for _, point := range loop {
			ll := s2.LatLngFromDegrees(point[1], point[0])
			pts = append(pts, s2.PointFromLatLng(ll))
		}
		loops = append(loops, s2.LoopFromPoints(pts))
	}
	p := s2.PolygonFromLoops(loops)

	rc := s2.NewRegionCoverer()
	rc.MaxCells = 32
	rc.MaxLevel = 22
	rc.MinLevel = 12

	u := rc.Covering(p)
	return u
}
