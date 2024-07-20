package map_aggr

import (
	"fmt"
	"testing"

	"cleanapp/backend/server/api"
)

func TestMapAggregatorS2(t *testing.T) {
	type val struct {
		lat float64
		lon float64
	}

	testCases := []struct {
		name      string
		latMin    float64
		lonMin    float64
		latMax    float64
		lonMax    float64
		latCenter float64
		lonCenter float64

		vals []val

		e map[string]bool
	}{
		{
			name:      "Large Viewport",
			latMin:    42.691869916020075,
			lonMin:    -4.318880552071925,
			latMax:    52.80861391899353,
			lonMax:    11.800429267075046,
			latCenter: 47.7502419175,
			lonCenter: 3.7407743575,

			vals: []val{
				{47.31462939002329, 8.541340828180283},
				{47.31462939002329, 8.541340828180283},
				{47.31462939002329, 8.541340828180283},
				{47.31462939002329, 8.541340828180283},
				{47.33001916923687, 8.526018592128164},
				{47.33001916923687, 8.526018592128164},
				{47.33001916923687, 8.526018592128164},
				{47.32553912731774, 8.541040883060727},
				{47.342540664005575, 8.524205901684924},
				{47.33262304063603, 8.5200006810743},
				{47.3162507337501, 8.5439348359329},
				{47.31736001922385, 8.517462177871218},
				{47.38400103557999, 8.493601108716156},
				{47.39907725236555, 8.612192557531866},
				{48.95821274837425, -0.5711499548796795},
			},

			e: map[string]bool{
				"{47.35315615503948 8.536694425531673 14 0 0 false}":  true,
				"{48.95821274837425 -0.5711499548796795 1 14 1 true}": true,
			},
		}, {
			name:      "Small Viewport",
			latMin:    47.00155041602738,
			lonMin:    7.875126253510233,
			latMax:    47.73257160018401,
			lonMax:    8.979175225820796,
			latCenter: 47.3670610081,
			lonCenter: 8.42715073967,

			vals: []val{
				{47.31462939002329, 8.541340828180283},
				{47.31462939002329, 8.541340828180283},
				{47.31462939002329, 8.541340828180283},
				{47.31462939002329, 8.541340828180283},
				{47.33001916923687, 8.526018592128164},
				{47.33001916923687, 8.526018592128164},
				{47.33001916923687, 8.526018592128164},
				{47.32553912731774, 8.541040883060727},
				{47.342540664005575, 8.524205901684924},
				{47.33262304063603, 8.5200006810743},
				{47.3162507337501, 8.5439348359329},
				{47.31736001922385, 8.517462177871218},
				{47.38400103557999, 8.493601108716156},
				{47.39907725236555, 8.612192557531866},
			},

			e: map[string]bool{
				"{47.39907725236555 8.612192557531866 1 13 1 true}": true,
				"{47.32553912731774 8.541040883060727 1 7 1 true}":  true,
				"{47.3162507337501 8.5439348359329 1 10 1 true}":    true,
				"{47.31462939002329 8.541340828180283 1 0 1 true}":  true,
				"{47.31462939002329 8.541340828180283 1 1 1 true}":  true,
				"{47.31462939002329 8.541340828180283 1 2 1 true}":  true,
				"{47.31462939002329 8.541340828180283 1 3 1 true}":  true,
				"{47.38400103557999 8.493601108716156 1 12 1 true}": true,
				"{47.342540664005575 8.524205901684924 1 8 1 true}": true,
				"{47.33262304063603 8.5200006810743 1 9 1 true}":    true,
				"{47.33001916923687 8.526018592128164 1 4 1 true}":  true,
				"{47.33001916923687 8.526018592128164 1 5 1 true}":  true,
				"{47.33001916923687 8.526018592128164 1 6 1 true}":  true,
				"{47.31736001922385 8.517462177871218 1 11 1 true}": true,
			},
		},
	}

	for _, testCase := range testCases {
		a := NewMapAggregatorS2(&api.ViewPort{
			LatMin: testCase.latMin,
			LonMin: testCase.lonMin,
			LatMax: testCase.latMax,
			LonMax: testCase.lonMax,
		}, &api.Point{
			Lat: testCase.latCenter,
			Lon: testCase.lonCenter,
		})

		for i, v := range testCase.vals {
			a.AddPoint(api.MapResult{Latitude: v.lat, Longitude: v.lon, Count: 1, ReportID: int64(i), Team: 1, Own: true})
		}
		r := a.ToArray()

		if len(r) != len(testCase.e) {
			t.Errorf("%s: Result length %d is different from the expected %d", testCase.name, len(r), len(testCase.e))
		}
		for _, v := range r {
			s := fmt.Sprintf("%v", v)
			if _, ok := testCase.e[s]; !ok {
				t.Errorf("%s: The result %q  is not expected.", testCase.name, s)
			}
		}
	}
}
