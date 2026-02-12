package server

import (
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func GetStats(c *gin.Context) {
	var sa api.StatsArgs

	// Get the arguments.
	if err := c.BindJSON(&sa); err != nil {
		log.Errorf("Failed to get the argument in /get_stats call: %w", err)
		return
	}

	if sa.Version != "2.0" {
		log.Errorf("Bad version in /update_or_create_user, expected: 2.0, got: %v", sa.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Error connecting to DB: %w", err)
		return
	}

	r, err := db.GetStats(dbc, sa.Id)
	if err != nil {
		log.Errorf("Failed to update user with %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, r) // 200
}
