package backend

type MapResult struct {
	Latitude  float64
	Longitude float64
	Count     int64
}

type MapAggregator struct {
	vp               ViewPort
	latStep, lonStep float64 // May be negative west of Grinwich and in Southern hemisphere.
	latCnt, lonCnt   int
	v                map[int]*MapResult
}

func NewMapAggregator(latTop, lonLeft, latBottom, lonRight float64, latCnt, lonCnt int) MapAggregator {
	return MapAggregator{
		vp: ViewPort{
			LatTop:    latTop,
			LonLeft:   lonLeft,
			LatBottom: latBottom,
			LonRight:  lonRight,
		},
		latStep: (latBottom - latTop) / float64(latCnt),
		lonStep: (lonRight - lonLeft) / float64(lonCnt),
		latCnt:  latCnt,
		lonCnt:  lonCnt,
		v:       make(map[int]*MapResult),
	}
}

func (a MapAggregator) AddPoint(lat, lon float64) {
	vp := &a.vp
	latX := int((lat - vp.LatTop) / a.latStep)
	lonX := int((lon - vp.LonLeft) / a.lonStep)
	if latX < 0 || lonX < 0 || latX >= a.latCnt || lonX >= a.lonCnt {
		return
	}
	x := latX*a.lonCnt + lonX
	v, ok := a.v[x]
	if ok {
		v.Count += 1
		return
	}
	a.v[x] = &MapResult{
		Latitude:  vp.LatTop + a.latStep*(0.5+float64(latX)),
		Longitude: vp.LonLeft + a.lonStep*(0.5+float64(lonX)),
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
