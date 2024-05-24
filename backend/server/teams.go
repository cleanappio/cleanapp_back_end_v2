package server

import (
	"log"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/gin-gonic/gin"
)

func GetTeams(c *gin.Context) {
	log.Print("Call to " + EndPointGetTeams)
	var ba api.BaseArgs

	// Troubleshooting code:
	// b, _ := c.GetRawData()
	// log.Printf("Got %s", string(b))

	// Get the arguments.
	if err := c.BindJSON(&ba); err != nil {
		log.Printf("Failed to get the argument in %q call: %v", EndPointGetTeams, err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if ba.Version != "2.0" {
		log.Printf("Bad version in %s, expected: 2.0, got: %v", EndPointGetTeams, ba.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Get teams stats.
	log.Printf("%s got %v", EndPointGetTeams, ba)
	r, err := db.GetTeams()
	if err != nil {
		log.Printf("Failed to report stats for user with %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	r.Base = ba

	c.IndentedJSON(http.StatusOK, r) // 200
}
