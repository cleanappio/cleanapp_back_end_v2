package be

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ReportArgs struct {
	Version  string  `json:"version"` // Must be "2.0"
	Id       string  `json:"id"`      // public key.
	Latitude float64 `json:"latitude"`
	Longitue float64 `json:"longitude"`
	X        int32   `json:"x"`
	Y        int32   `json:"y"`
	Image    []byte  `json:"image"`
}

func Report(c *gin.Context) {
	log.Print("Call to /report")
	var report ReportArgs

	/* Troubleshooting code:
	b, _ := c.GetRawData()
	log.Printf("Got %s", string(b))
	*/

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

	// Add report to the database.
	log.Printf("/report got %v", report)
	err := saveReport(report)
	if err != nil {
		log.Printf("Failed to write report with %v", err)
		c.String(http.StatusInternalServerError, "Failed to save the report.") // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
