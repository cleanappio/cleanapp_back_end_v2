package be

import (
	"fmt"
	"testing"
)

func TestAggregator(t *testing.T) {
	a := NewAggregator(4.0, 5.0, 10.0, 5.0, 10, 5)

	type val struct {
		lon float64
		lat float64
	}
	vals := []val{
		val{lon: 4.5, lat: 5.3},
		val{lon: 4.1, lat: 5.7},
		val{lon: 5.6, lat: 7.3},
		val{lon: 7.5, lat: 8.3},
		val{lon: 7.7, lat: 8.1},
		val{lon: 7.9, lat: 8.9},
		val{lon: 10.7, lat: 9.1},
		val{lon: 3.7, lat: 5.1},
	}

	for _, v := range vals {
		a.AddPoint(v.lat, v.lon)
	} 

	r := a.ToArray()
	e := map[string]bool{"{5.5 5.5 2}": true, "{7.5 5.5 1}": true, "{8.5 7.5 3}": true}
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
