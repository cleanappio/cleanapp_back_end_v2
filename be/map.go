package be

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ViewPort struct {
	LatTop    float64 `json:"lattop"`
	LonLeft   float64 `json:"lonleft"`
	LatBottom float64 `json:"latbottom"`
	LonRight  float64 `json:"lonright"`
}

type S2Cell string

type MapArgs struct {
	Version string   `json:"version"` // Must be "2.0"
	Id      string   `json:"id"`      // public key.
	VPort   ViewPort `json:"vport"`
	S2Cells []S2Cell `json:"s2cells"` // Nullable, not implemented yet.
}

func GetMap(c *gin.Context) {
	log.Print("Call to /get_map")
	var ma MapArgs

	// Troubleshooting code:
	// b, _ := c.GetRawData()
	// log.Printf("Got %s", string(b))

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
	a := NewMapAggregator(vp.LatTop, vp.LonLeft, vp.LatBottom, vp.LonRight, 10, 10)
	for _, p := range r {
		a.AddPoint(p.Latitude, p.Longitude)
		fmt.Printf("%v", p)

	}
	c.IndentedJSON(http.StatusOK, a.ToArray()) // 200
}
