package be

import (
	"github.com/golang/geo/r1"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
)

type aggrUnit struct {
	cnt     int64
	origRes []MapResult
}

type mapAggregatorS2 struct {
	level int
	aggrs map[s2.CellID]*aggrUnit
}

const (
	expectedCells = 16
	minLevel      = 2
	maxLevel      = 18
	levelStep     = 2
	minRepToAggr  = 10
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
		// Finding the s2cell level with which the viewport area can be
		// approx. covered by a number expectedCells s2cells.
		if vpArea/cc.ApproxArea() < expectedCells {
			// Applying s2cells with a step levelStep
			alignedLv := lv / levelStep * levelStep
			if alignedLv == lv {
				return alignedLv
			}
			return alignedLv + levelStep
		}
	}
	return minLevel
}

func NewMapAggregatorS2(vp *ViewPort, center *Point) mapAggregatorS2 {
	lv := cellBaseLevel(vp, center)
	return mapAggregatorS2{
		level: lv,
		aggrs: make(map[s2.CellID]*aggrUnit),
	}
}

func (a *mapAggregatorS2) AddPoint(mapRes MapResult) {
	pc := s2.CellIDFromLatLng(s2.LatLngFromDegrees(mapRes.Latitude, mapRes.Longitude))
	parent := pc.Parent(a.level)
	if _, ok := a.aggrs[parent]; !ok {
		a.aggrs[parent] = &aggrUnit{}
	}
	a.aggrs[parent].cnt += 1

	// Seeing how many cells are aggregated in the parent cell.
	// If <= minRepToAggr then add the report to origin report results.
	// Otherwise clear report results which is a signal to use aggregated
	// result.
	if a.aggrs[parent].cnt < minRepToAggr {
		a.aggrs[parent].origRes = append(a.aggrs[parent].origRes, mapRes)
	} else {
		a.aggrs[parent].origRes = nil
	}
}

func (a *mapAggregatorS2) ToArray() []MapResult {
	r := make([]MapResult, 0, len(a.aggrs))
	for c, unit := range a.aggrs {
		ll := c.LatLng()
		if unit.origRes != nil {
			r = append(r, unit.origRes...)
		} else {
			r = append(r, MapResult{
				Latitude:  ll.Lat.Degrees(),
				Longitude: ll.Lng.Degrees(),
				Count:     unit.cnt,
			})
		}
	}
	return r
}
