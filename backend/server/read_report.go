package server

import (
	"cleanapp/common"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func ReadReport(c *gin.Context) {
	args := &api.ReadReportArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /read_report call: %w", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /read_report, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}
	defer dbc.Close()

	result, err := db.ReadReport(dbc, args)
	if err != nil {
		log.Errorf("Referral writing, %w", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, result)
}
