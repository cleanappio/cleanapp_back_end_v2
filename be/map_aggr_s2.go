package be

import (
	"github.com/golang/geo/r1"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
)

type aggrUnit struct{
	cnt int64
	origCell s2.CellID
}

type mapAggregatorS2 struct {
	level int
	aggrs map[s2.CellID]*aggrUnit
}

const (
	expectedCells = 160
	minLevel = 6
	maxLevel = 16
)

func cellBaseLevel(vp *ViewPort, center *Point) int {
	minLL := s2.LatLngFromDegrees(vp.LatMin, vp.LonMin)
	maxLL := s2.LatLngFromDegrees(vp.LatMax, vp.LonMax)

	rect := s2.Rect{
		Lat: r1.Interval{
			Lo: minLL.Lat.Radians(),
			Hi: maxLL.Lat.Radians()},
		Lng: s1.Interval{
			Lo: minLL.Lng.Radians(),
			Hi: maxLL.Lng.Radians()},
	}

	vpArea := rect.Area()

	centerLL := s2.CellIDFromLatLng(s2.LatLngFromDegrees(center.Lat, center.Lon))

	for lv := maxLevel; lv >= minLevel; lv-- {
		cc := s2.CellFromCellID(centerLL.Parent(lv))
		if (vpArea / cc.ApproxArea() < expectedCells) {
			return lv
		}
	}
	return minLevel  // Large enough level
}

func NewMapAggregatorS2(vp *ViewPort, center *Point) mapAggregatorS2 {
	lv := cellBaseLevel(vp, center)
	return mapAggregatorS2{
		level: lv,
		aggrs: make(map[s2.CellID]*aggrUnit),
	}
}

func (a *mapAggregatorS2) AddPoint(lat, lon float64) {
	pc := s2.CellIDFromLatLng(s2.LatLngFromDegrees(lat, lon))
	parent := pc.Parent(a.level)
	if _, ok := a.aggrs[parent]; !ok {
		a.aggrs[parent] = &aggrUnit{}
	}
	a.aggrs[parent].cnt += 1
	a.aggrs[parent].origCell = pc
}

func (a *mapAggregatorS2) ToArray() []MapResult {
	r := make([]MapResult, 0, len(a.aggrs))
	for c, unit := range a.aggrs {
		ll := c.LatLng()
		if unit.cnt == 1 {
			ll = unit.origCell.LatLng()
		}
		r = append(r, MapResult{
			Latitude: ll.Lat.Degrees(),
			Longitude: ll.Lng.Degrees(),
			Count: unit.cnt,
		})
	}
	return r
}
