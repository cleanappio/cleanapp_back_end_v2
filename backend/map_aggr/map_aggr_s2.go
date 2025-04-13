package map_aggr

import (
	"cleanapp/backend/server/api"

	"github.com/golang/geo/r1"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
)

type aggrUnit struct {
	cnt         int64
	containment [4]bool // 4 elements, one per child cell
	pin         s2.Point
	origRes     []*api.MapResult
}

type mapAggregatorS2 struct {
	level  int
	points map[s2.CellID][]*api.MapResult
	aggrs  map[s2.CellID]*aggrUnit
}

const (
	expectedCells             = 16
	minLevel                  = 2
	maxLevel                  = 18
	minRepToAggr              = 10
	weightDiffThreshold       = 8
)

func CellBaseLevel(vp *api.ViewPort, center *api.Point) int {
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
			return lv
		}
	}
	return minLevel
}

func NewMapAggregatorS2(vp *api.ViewPort, center *api.Point) mapAggregatorS2 {
	return mapAggregatorS2{
		level:  CellBaseLevel(vp, center),
		points: make(map[s2.CellID][]*api.MapResult),
		aggrs:  make(map[s2.CellID]*aggrUnit),
	}
}

func (a *mapAggregatorS2) AddPoint(mapRes api.MapResult) {
	pc := s2.CellIDFromLatLng(s2.LatLngFromDegrees(mapRes.Latitude, mapRes.Longitude))
	parent := pc.Parent(maxLevel)
	if a.points[parent] == nil {
		a.points[parent] = make([]*api.MapResult, 0)
	}
	a.points[parent] = append(a.points[parent], &mapRes)
}

func (a *mapAggregatorS2) ToArray() []api.MapResult {
	a.aggregate()
	r := make([]api.MapResult, 0, len(a.aggrs))
	for _, unit := range a.aggrs {
		ll := s2.LatLngFromPoint(unit.pin)
		if unit.cnt <= minRepToAggr {
			for _, res := range unit.origRes {
				r = append(r, *res)
			}
		} else {
			r = append(r, api.MapResult{
				Latitude:  ll.Lat.Degrees(),
				Longitude: ll.Lng.Degrees(),
				Count:     unit.cnt,
			})
		}
	}
	return r
}

func (a *mapAggregatorS2) computeCentroid(pCell s2.CellID, chAggrs []*aggrUnit) s2.Point {
	fChPins := make([]s2.Point, 0)
	maxWeight := int64(0)
	for _, aggr := range chAggrs {
		if maxWeight < aggr.cnt {
			maxWeight = aggr.cnt
		}
	}
	for _, aggr := range chAggrs {
		if maxWeight/aggr.cnt < weightDiffThreshold {
			fChPins = append(fChPins, aggr.pin)
		}
	}
	switch len(fChPins) {
	case 1:
		return fChPins[0]
	case 2:
		return s2.PlanarCentroid(fChPins[0], fChPins[0], fChPins[1])
	case 3:
		return s2.PlanarCentroid(fChPins[0], fChPins[1], fChPins[2])
	case 4:
		return s2.PointFromLatLng(pCell.LatLng())
	}
	return s2.PointFromLatLng(pCell.LatLng())
}

func (a *mapAggregatorS2) aggrStep(level int) {
	if level < a.level {
		return
	}
	// Aggregate existing aggregation units on one S2 cell level up
	nextAggrs := make(map[s2.CellID]*aggrUnit)
	for cell, unit := range a.aggrs {
		p := cell.Parent(level)
		eu, ok := nextAggrs[p]
		if !ok {
			// First cell on the level, copy aggregation from the previous level
			// but w/o containment info
			nextAggrs[p] = &aggrUnit{
				cnt:         unit.cnt,
				containment: [4]bool{},
				origRes:     unit.origRes,
			}
		} else {
			// Sum the existing aggregation with the current one
			nextAggrs[p] = &aggrUnit{
				cnt:         eu.cnt + unit.cnt,
				containment: eu.containment,
			}
			if eu.cnt+unit.cnt <= minRepToAggr {
				nextAggrs[p].origRes = append(eu.origRes, unit.origRes...)
			}
		}
		// Mark the containment of the child aggregation
		// The unit is an aggregation on level+1, so it's a child for the nextAggs[p]
		// which is an aggregation on level
		nextAggrs[p].containment[cell.ChildPosition(level+1)] = true
	}
	// Computing a pin point position for the new aggregation.
	// It's to be a centroid of children aggregations pins
	for pCell, pUnit := range nextAggrs {
		chAggrs := make([]*aggrUnit, 0)
		for i, v := range pUnit.containment {
			if v {
				chCell := pCell.Children()[i]
				if chAggr, ok := a.aggrs[chCell]; ok {
					chAggrs = append(chAggrs, chAggr)
				}
			}
		}
		pUnit.pin = a.computeCentroid(pCell, chAggrs)
	}
	// Replace the aggregations with newly computed ones
	a.aggrs = nextAggrs
	// Call the next level recursively
	a.aggrStep(level - 1)
}

func (a *mapAggregatorS2) aggregate() {
	for cell, pts := range a.points {
		a.aggrs[cell] = &aggrUnit{
			cnt:         int64(len(pts)),
			containment: [4]bool{true, true, true, true},
			pin:         s2.PointFromLatLng(cell.LatLng()),
		}
		if len(pts) <= minRepToAggr {
			a.aggrs[cell].origRes = pts
		}
	}
	a.aggrStep(maxLevel - 1)
}
