package server

import (
	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/common"
	"fmt"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func CreateOrUpdateArea(c *gin.Context) {
	args := &api.CreateAreaRequest{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /create_area call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /create_area, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Put the area into the DB
	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}
	defer dbc.Close()

	err = db.CreateOrUpdateArea(dbc, args)
	if err != nil {
		log.Errorf("Error creating or updating area: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}
	
	c.Status(http.StatusOK)
}

func GetAreas(c *gin.Context) {
	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}
	defer dbc.Close()

	res, err := db.GetAreas(dbc, nil)
	if err != nil {
		log.Errorf("Error getting areas: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}

	c.IndentedJSON(http.StatusOK, res)
}

func UpdateConsent(c *gin.Context) {
	args := &api.UpdateConsentRequest{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /update_consent call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /update_consent, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}
	defer dbc.Close()

	if err = db.UpdateConsent(dbc, args); err != nil {
		log.Errorf("Error updating email consent: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprint(err))
	}

	c.Status(http.StatusOK)
}