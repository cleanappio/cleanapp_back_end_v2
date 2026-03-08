package server

import (
	"errors"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func ReadReportEmailStatus(c *gin.Context) {
	args := &api.ReadReportArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /read_report_email_status call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /read_report_email_status, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.")
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}

	result, err := db.ReadReportEmailStatus(dbc, args)
	if err != nil {
		if errors.Is(err, db.ErrReportNotOwner) {
			c.Status(http.StatusForbidden)
			return
		}
		if errors.Is(err, db.ErrReportNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		log.Errorf("Failed to read report email status: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, result)
}
