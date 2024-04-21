package server

import (
	"cleanapp/pipelines/disburse"
	"cleanapp/common"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type DisbusrseArgs struct {
	Version string  `json:"version"` // Must be "2.0"
}

func DisburseTokens(c *gin.Context) {
	var args RedeemArgs

	if err := c.BindJSON(&args); err != nil {
		log.Errorf("Failed to get the argument in /get_stats call: %w", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /update_or_create_user, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	db, err := common.DBConnect()
	if err != nil {
		log.Errorf("Error connecting to the database, %w", err)
		return
	}
	defer db.Close()

	d, err := disburse.NewDisburser(db)
	if err != nil {
		log.Errorf("Disburser creation failed, %w", err)
	}
	succeeded, failed, err := d.Disburse()
	if err != nil {
		log.Errorf("Disburse failed, %w", err)
		return
	}
	log.Infof("Tokens disburse finished, %d succeeded, %d failed.", succeeded, failed)

	c.Status(http.StatusOK)
}
