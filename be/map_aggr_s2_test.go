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
		{lon: 4.1, lat: 5.7},
		{lon: 5.6, lat: 7.3},
		{lon: 7.5, lat: 8.3},
		{lon: 7.7, lat: 8.1},
		{lon: 7.9, lat: 8.9},
		{lon: 10.7, lat: 9.1},
		{lon: 3.7, lat: 5.1},
	}

	for _, v := range vals {
		a.AddPoint(v.lat, v.lon)
	}

	r := a.ToArray()
	e := map[string]bool{
		"{9.13346520192252 6.407735769346235 2}": true,
		"{9.073922316826462 9.189905212974633 1}": true,
		"{6.394177107399892 3.7435089912669084 3}": true,
		"{6.368035609920834 6.407735769346235 1}": true,
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
