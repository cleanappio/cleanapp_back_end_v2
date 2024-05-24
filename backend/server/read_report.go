package server

import (
	"cleanapp/common"
	"log"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/gin-gonic/gin"
)

func ReadReport(c *gin.Context) {
	log.Print("Call to /read_report")
	args := &api.ReadReportArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Printf("Failed to get the argument in /read_report call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if args.Version != "2.0" {
		log.Printf("Bad version in /read_report, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	log.Printf("/update_privacy_and_toc got %v", args)

	dbc, err := common.DBConnect()
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer dbc.Close()

	result, err := db.ReadReport(dbc, args)
	if err != nil {
		log.Printf("Referral writing, %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, result)
}
