package server

import (
	"cleanapp/common"
	"log"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/gin-gonic/gin"
)

func Report(c *gin.Context) {
	log.Print("Call to /report")
	var report api.ReportArgs

	// Get the arguments.
	if err := c.BindJSON(&report); err != nil {
		log.Printf("Failed to get the argument in /report call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if report.Version != "2.0" {
		log.Printf("Bad version in /report, expected: 2.0, got: %v", report.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer dbc.Close()

	// Add report to the database.
	logReport(report)
	err = db.SaveReport(dbc, report)
	if err != nil {
		log.Printf("Failed to write report with %v", err)
		c.String(http.StatusInternalServerError, "Failed to save the report.") // 500
		return
	}
	c.Status(http.StatusOK) // 200
}

func logReport(r api.ReportArgs) {
	r.Image = nil
	log.Printf("/report got %v", r)
}
