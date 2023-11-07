package backend

import (
	"cleanapp/api"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func (h *handler) Report(c *gin.Context) {
	log.Info("Call to /report")
	var report api.ReportArgs

	/* Troubleshooting code:
	b, _ := c.GetRawData()
	log.Printf("Got %s", string(b))
	*/

	// Get the arguments.
	if err := c.BindJSON(&report); err != nil {
		log.Infof("Failed to get the argument in /report call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if report.Version != "2.0" {
		log.Infof("Bad version in /report, expected: 2.0, got: %v", report.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add report to the database.
	log.Infof("/report got %v", report)
	err := h.sDB.saveReport(report)
	if err != nil {
		log.Infof("Failed to write report with %v", err)
		c.String(http.StatusInternalServerError, "Failed to save the report.") // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
