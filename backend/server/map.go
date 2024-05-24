package server

import (
	"log"
	"net/http"
	"time"

	"cleanapp/backend/db"
	"cleanapp/backend/map_aggr"
	"cleanapp/backend/server/api"

	"github.com/gin-gonic/gin"
)

const retention = 24 * 30 * time.Hour // For initial run it's one month. To be reduced after we get more users.

func GetMap(c *gin.Context) {
	log.Print("Call to /get_map")
	var ma api.MapArgs

	// Get the arguments.
	if err := c.BindJSON(&ma); err != nil {
		log.Printf("Failed to get the argument in /get_map call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	log.Printf("/get_map got %v", ma)

	if ma.Version != "2.0" {
		log.Printf("Bad version in /update_or_create_user, expected: 2.0, got: %v", ma.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	r, err := db.GetMap(ma.Id, ma.VPort, retention)
	if err != nil {
		log.Printf("Failed to update user with %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	a := map_aggr.NewMapAggregatorS2(&ma.VPort, &ma.Center)
	for _, p := range r {
		a.AddPoint(p)
	}
	c.IndentedJSON(http.StatusOK, a.ToArray()) // 200
}
