package be

import (
	"fmt"
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

type MapArgs struct {
	Version string   `json:"version"` // Must be "2.0"
	Id      string   `json:"id"`      // public key.
	VPort   ViewPort `json:"vport"`
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
	log.Printf("/get_map got %v", ma)
	r, err := getMap(ma.VPort)
	if err != nil {
		log.Printf("Failed to update user with %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	vp := &ma.VPort
	a := NewMapAggregator(vp, 10, 10)
	for _, p := range r {
		a.AddPoint(p.Latitude, p.Longitude)
		fmt.Printf("%v", p)

	}
	c.IndentedJSON(http.StatusOK, a.ToArray()) // 200
}
