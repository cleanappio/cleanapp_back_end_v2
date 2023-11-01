package be

import (
	"log"
)

type MapResult struct {
	Latitude float64
	Longitude float64
	Count int64
}

type Aggregator struct {
	latTop, lonLeft float64
	latStep, lonStep float64
	latCnt, lonCnt int
	v map[int]*MapResult
}

func NewAggregator(lat, lon, latw, lonw float64, latc, lonc int) Aggregator {
	return Aggregator {
		latTop: lat,
		lonLeft: lon,
		latStep: latw / float64(latc),
		lonStep: lonw / float64(lonc),
		latCnt: latc,
		lonCnt: lonc,
		v: make(map[int]*MapResult),
	}
}

func (a Aggregator) AddPoint(lat, lon float64) {
	latX := int((lat - a.latTop) / a.latStep)
	lonX := int((lon - a.lonLeft) / a.lonStep)
	if latX < 0 || lonX < 0 || latX >= a.latCnt || lonX >= a.lonCnt {
		log.Printf("%f:%f results in  %d:%d index outside of the box", lat, lon, latX, lonX)
		return
	}
	x := latX * a.lonCnt + lonX
	v, ok := a.v[x]
	if ok {
		v.Count += 1
		log.Printf("%d:%d->%d %d", latX, lonX, x, v.Count)
		return
	}
	a.v[x] = &MapResult{
		Latitude: a.latTop + a.latStep *(0.5+float64(latX)),
		Longitude: a.lonLeft + a.lonStep*(0.5+float64(lonX)),
		Count: 1,
	}
	log.Printf("%d:%d->%d 1", latX, lonX, x)
}

func (a Aggregator) ToArray() []MapResult {
	r := make([]MapResult, 0, len(a.v))
	for _, v := range a.v {
		r = append(r, *v)
	}
	return r
 }