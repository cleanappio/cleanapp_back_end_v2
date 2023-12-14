package be

import (
	"fmt"
	"testing"
)

func TestMapAggregatorS2(t *testing.T) {
	a := NewMapAggregatorS2(&ViewPort{
		LatMin: 4.0,
		LonMin: 5.0,
		LatMax: 9.0,
		LonMax: 10.0,
	}, &Point{
		Lat: 6.5,
		Lon: 7.5,
	})
	fmt.Printf("%v", a)

	type val struct {
		lon float64
		lat float64
	}
	vals := []val{
		{lon: 4.5, lat: 5.3},
		{lon: 4.51, lat: 5.31},
		{lon: 4.52, lat: 5.32},
		{lon: 4.53, lat: 5.33},
		{lon: 4.54, lat: 5.34},
		{lon: 4.55, lat: 5.35},
		{lon: 4.56, lat: 5.36},
		{lon: 4.57, lat: 5.37},
		{lon: 4.58, lat: 5.38},
		{lon: 4.59, lat: 5.39},
		{lon: 4.6, lat: 5.4},
		{lon: 4.61, lat: 5.41},
		{lon: 4.1, lat: 5.7},
		{lon: 5.6, lat: 7.3},
		{lon: 7.5, lat: 8.3},
		{lon: 7.7, lat: 8.1},
		{lon: 7.9, lat: 8.9},
		{lon: 10.7, lat: 9.1},
		{lon: 3.7, lat: 5.1},
	}

	for i, v := range vals {
		a.AddPoint(MapResult{Latitude: v.lat, Longitude: v.lon, Count: 1, ReportID: int64(i), Team: 1})
	}

	r := a.ToArray()
	e := map[string]bool{
    "{5.1 3.7 1 18 1}": true,
    "{5.7132243245354655 4.397635665480262 13 0 0}": true,
    "{7.3 5.6 1 13 1}": true,
    "{8.3 7.5 1 14 1}": true,
    "{8.1 7.7 1 15 1}": true,
    "{8.9 7.9 1 16 1}": true,
    "{9.1 10.7 1 17 1}": true,
	}
	fmt.Printf("%v", r)
	if len(r) != len(e) {
		t.Errorf("Result length %d is different from the expected %d", len(r), len(e))
	}
	for _, v := range r {
		s := fmt.Sprintf("%v", v)
		if _, ok := e[s]; !ok {
			t.Errorf("The result %q  is not expected.", s)
		}
	}
}
