package server

import (
	"net/http"

	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func GetTeams(c *gin.Context) {
	var ba api.BaseArgs

	// Get the arguments.
	if err := c.BindJSON(&ba); err != nil {
		log.Errorf("Failed to get the argument in %q call: %w", EndPointGetTeams, err)
		return
	}

	if ba.Version != "2.0" {
		log.Errorf("Bad version in %s, expected: 2.0, got: %v", EndPointGetTeams, ba.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Error connecting to DB: %w", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	// Get teams stats.
	r, err := getTeamsCached(dbc)
	if err != nil {
		log.Errorf("Failed to report stats for user with %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	r.Base = ba

	c.IndentedJSON(http.StatusOK, r) // 200
}
