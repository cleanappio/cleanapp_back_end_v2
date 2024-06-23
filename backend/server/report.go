package server

import (
	"cleanapp/common"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func Report(c *gin.Context) {
	var report api.ReportArgs

	// Get the arguments.
	if err := c.BindJSON(&report); err != nil {
		log.Errorf("Failed to get the argument in /report call: %w", err)
		return
	}

	if report.Version != "2.0" {
		log.Errorf("Bad version in /report, expected: 2.0, got: %v", report.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("Error connecting to DB: %w", err)
		return
	}
	defer dbc.Close()

	// Add report to the database.
	err = db.SaveReport(dbc, report)
	if err != nil {
		log.Errorf("Failed to write report with %w", err)
		c.String(http.StatusInternalServerError, "Failed to save the report.") // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
