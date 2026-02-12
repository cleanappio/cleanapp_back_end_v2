package server

import (
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func GetTopScores(c *gin.Context) {

	var ba api.BaseArgs

	if err := c.BindJSON(&ba); err != nil {
		log.Errorf("Failed to get the argument in %q call: %w", EndPointGetTopScores, err)
		return
	}

	if ba.Version != "2.0" {
		log.Errorf("Bad version in %s, expected: 2.0, got: %v", EndPointGetTopScores, ba.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Error connecting to DB: %w", err)
		return
	}

	r, err := db.GetTopScores(dbc, &ba, 7)
	if err != nil {
		log.Errorf("Failed to get top scores %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, r)
}
