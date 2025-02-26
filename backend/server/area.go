package server

import (
	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/common"
	"fmt"
	"net/http"
	"strconv"

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
		log.Errorf("Bad version in /create_or_update_area, expected: 2.0, got: %v", args.Version)
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
	latMinStr, hasLatMin := c.GetQuery("sw_lat")
	lonMinStr, hasLonMin := c.GetQuery("sw_lon")
	latMaxStr, hasLatMax := c.GetQuery("ne_lat")
	lonMaxStr, hasLonMax := c.GetQuery("ne_lon")

	var latMin, lonMin, latMax, lonMax float64
	var err error
	var vp *api.ViewPort
	if hasLatMin && hasLatMax && hasLonMin && hasLonMax {
		if latMin, err = strconv.ParseFloat(latMinStr, 64); err != nil {
			log.Errorf("Error in parsing sw_lat param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing sw_lat: %v", err))
			return
		}
		if lonMin, err = strconv.ParseFloat(lonMinStr, 64); err != nil {
			log.Errorf("Error in parsing sw_lon param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing sw_lon: %v", err))
			return
		}
		if latMax, err = strconv.ParseFloat(latMaxStr, 64); err != nil {
			log.Errorf("Error in parsing ne_lat param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing ne_lat: %v", err))
			return
		}
		if lonMax, err = strconv.ParseFloat(lonMaxStr, 64); err != nil {
			log.Errorf("Error in parsing ne_lon param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing ne_lon: %v", err))
			return
		}
		vp = &api.ViewPort{
			LatMin: latMin,
			LonMin: lonMin,
			LatMax: latMax,
			LonMax: lonMax,
		}
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}
	defer dbc.Close()

	areaIds, err := db.GetAreaIdsForViewport(dbc, vp)
	if err != nil {
		log.Errorf("Error getting area IDs for viewport %v: %w", vp, err)
		c.String(http.StatusInternalServerError, fmt.Sprintf("Getting area IDs: %v", err))
		return
	}

	res, err := db.GetAreas(dbc, areaIds)
	if err != nil {
		log.Errorf("Error getting areas: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprintf("Getting areas: %v", err))
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
		return
	}

	c.Status(http.StatusOK)
}

func GetAreasCount(c *gin.Context) {
	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}
	defer dbc.Close()

	cnt, err := db.GetAreasCount(dbc)
	if err != nil {
		log.Errorf("Error getting areas.count: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}

	c.IndentedJSON(http.StatusOK, &api.AreasCountResponse{
		Count: cnt,
	})
}
