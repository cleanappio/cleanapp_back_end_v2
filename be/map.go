package be

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ViewPort struct {
	LatMin float64 `json:"latmin"`
	LonMin float64 `json:"lonmin"`
	LatMax float64 `json:"latmax"`
	LonMax float64 `json:"lonmax"`
}

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type MapArgs struct {
	Version string   `json:"version"` // Must be "2.0"
	Id      string   `json:"id"`      // public key.
	VPort   ViewPort `json:"vport"`
	Center  Point    `json:"center"`
}

type MapResult struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Count     int64     `json:"count"`
	ReportID  int64     `json:"report_id"` // Ignored if Count > 1
	Team      TeamColor `json:"team"`      // Ignored if Count > 1
}

func GetMap(c *gin.Context) {
	log.Print("Call to /get_map")
	var ma MapArgs

	// Get the arguments.
	if err := c.BindJSON(&ma); err != nil {
		log.Printf("Failed to get the argument in /get_map call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if ma.Version != "2.0" {
		log.Printf("Bad version in /update_or_create_user, expected: 2.0, got: %v", ma.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	r, err := getMap(ma.VPort)
	if err != nil {
		log.Printf("Failed to update user with %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	// a := NewMapAggregator(&ma.VPort, 10, 10)
	a := NewMapAggregatorS2(&ma.VPort, &ma.Center)
	for _, p := range r {
		a.AddPoint(p)
	}
	c.IndentedJSON(http.StatusOK, a.ToArray()) // 200
}
