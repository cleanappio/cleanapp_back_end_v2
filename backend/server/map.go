package server

import (
	"net/http"
	"time"

	"cleanapp/backend/db"
	"cleanapp/backend/map_aggr"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	aggregationLevelThreshold = 14
	retention = 24 * 365 * time.Hour // For initial run it's one year. To be reduced after we get more users.
)

func GetMap(c *gin.Context) {
	var ma api.MapArgs

	// Get the arguments.
	if err := c.BindJSON(&ma); err != nil {
		log.Errorf("Failed to get the argument in /get_map call: %w", err)
		return
	}

	if ma.Version != "2.0" {
		log.Errorf("Bad version in /update_or_create_user, expected: 2.0, got: %v", ma.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	r, err := db.GetMap(ma.Id, ma.VPort, retention)
	if err != nil {
		log.Errorf("Failed to update user with %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	level := map_aggr.CellBaseLevel(&ma.VPort, &ma.Center)

	if level > aggregationLevelThreshold {
		log.Infof("The level is %d, no aggregation needed, returning raw data")
		c.IndentedJSON(http.StatusOK, r) // 200
	} else {
		a := map_aggr.NewMapAggregatorS2(&ma.VPort, &ma.Center)
		for _, p := range r {
			a.AddPoint(p)
		}
		c.IndentedJSON(http.StatusOK, a.ToArray()) // 200
		}
}
