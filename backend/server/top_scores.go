package server

import (
	"cleanapp/common"
	"log"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/gin-gonic/gin"
)

func GetTopScores(c *gin.Context) {
	log.Print("Call to " + EndPointGetTopScores)

	var ba api.BaseArgs

	if err := c.BindJSON(&ba); err != nil {
		log.Printf("Failed to get the argument in %q call: %v", EndPointGetTopScores, err)
		c.String(http.StatusBadRequest, "Could not read JSON input.")
		return
	}

	if ba.Version != "2.0" {
		log.Printf("Bad version in %s, expected: 2.0, got: %v", EndPointGetTopScores, ba.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer dbc.Close()

	r, err := db.GetTopScores(dbc, &ba, 7)
	if err != nil {
		log.Printf("Failed to get top scores %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, r)
}
