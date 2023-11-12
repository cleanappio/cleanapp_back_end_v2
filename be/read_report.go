package be

import (
	"cleanapp/common"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ReadReportArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Seq     int    `json:"seq"`     // Report ID.
}

type ReadReportResponse struct {
	Id     string `json:"id"`
	Avatar string `json:"avatar"`
	Image  []byte `json:"image"`
}

func ReadReport(c *gin.Context) {
	log.Print("Call to /read_report")
	var args *ReadReportArgs

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

	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		log.Printf("%v", err)
		return
	}

	result, err := readReport(db, args)
	if err != nil {
		log.Printf("Referral writing, %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, result)
}
