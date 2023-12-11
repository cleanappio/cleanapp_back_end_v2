package be

import (
	"log"
)

type MapAggregator struct {
	vp               ViewPort
	latStep, lonStep float64 // May be negative west of Grinwich and in Southern hemisphere.
	latCnt, lonCnt   int
	v                map[int]*MapResult
}

func NewMapAggregator(vp *ViewPort, latCnt, lonCnt int) MapAggregator {
	return MapAggregator{
		vp:      *vp,
		latStep: (vp.LatMax - vp.LatMin) / float64(latCnt),
		lonStep: (vp.LonMax - vp.LonMin) / float64(lonCnt),
		latCnt:  latCnt,
		lonCnt:  lonCnt,
		v:       make(map[int]*MapResult),
	}
}

func (a MapAggregator) AddPoint(lat, lon float64) {
	vp := &a.vp
	latX := int((lat - vp.LatMin) / a.latStep)
	lonX := int((lon - vp.LonMin) / a.lonStep)
	if latX < 0 || lonX < 0 || latX >= a.latCnt || lonX >= a.lonCnt {
		log.Printf("%f:%f results in  %d:%d index outside of the box", lat, lon, latX, lonX)
		return
	}
	x := latX*a.lonCnt + lonX
	v, ok := a.v[x]
	if ok {
		v.Count += 1
		// Second+ times only mid-quadrant:
		v.Latitude = vp.LatMin + a.latStep*(0.5+float64(latX))
		v.Longitude = vp.LonMin + a.lonStep*(0.5+float64(lonX))
		return
	}
	// First time only the exact coordinates.
	a.v[x] = &MapResult{
		Latitude:  lat,
		Longitude: lon,
		Count:     1,
	}
}

func (a MapAggregator) ToArray() []MapResult {
	r := make([]MapResult, 0, len(a.v))
	for _, v := range a.v {
		r = append(r, *v)
	}
	return r
}
