package server

import (
	"net/http"

	"cleanapp/backend/db"
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

	// Get teams stats.
	r, err := db.GetTeams()
	if err != nil {
		log.Errorf("Failed to report stats for user with %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	r.Base = ba

	c.IndentedJSON(http.StatusOK, r) // 200
}
