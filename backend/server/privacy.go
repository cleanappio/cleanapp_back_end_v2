package server

import (
	"cleanapp/common"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func UpdatePrivacyAndTOC(c *gin.Context) {
	var args api.PrivacyAndTOCArgs

	if err := c.BindJSON(&args); err != nil {
		log.Errorf("Failed to get arguments: %w", err)
		c.String(http.StatusBadRequest, "Could not read JSON input.") // 400
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /update_or_create_user, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}
	defer dbc.Close()

	err = db.UpdatePrivacyAndTOC(dbc, &args)
	if err != nil {
		log.Errorf("Failed to update privacy and TOC %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
