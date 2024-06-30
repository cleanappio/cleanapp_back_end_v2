package server

import (
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/common"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func ReadReferral(c *gin.Context) {
	refQuery := &api.ReferralQuery{}
	if err := c.BindJSON(refQuery); err != nil {
		log.Errorf("JSON binding, %v", err)
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("%v", err)
		return
	}
	defer dbc.Close()

	refValue, err := db.ReadReferral(dbc, refQuery.RefKey)
	if err != nil {
		log.Errorf("referral reading, %v", err)
		c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, api.ReferralResult{
		RefValue: refValue,
	})
}

func WriteReferral(c *gin.Context) {
	refData := &api.ReferralData{}
	if err := c.BindJSON(refData); err != nil {
		log.Errorf("JSON binding, %v", err)
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("%v", err)
		return
	}
	defer dbc.Close()

	if err := db.WriteReferral(dbc, refData.RefKey, refData.RefValue); err != nil {
		c.Error(err)
		log.Errorf("referral writing, %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}

func GenerateReferral(c *gin.Context) {
	req := &api.GenRefRequest{}
	if err := c.BindJSON(req); err != nil {
		log.Errorf("JSON binding, %v", err)
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	if req.Version != "2.0" {
		log.Errorf("Bad version in /report, expected: 2.0, got: %v", req.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("%v", err)
		return
	}
	defer dbc.Close()

	ref, err := db.GenerateReferral(dbc, req, randRefGen)
	if err != nil {
		log.Errorf("referral generating, %v", err)
		c.Status(http.StatusInternalServerError)
		c.Error(err)
		return
	}

	c.IndentedJSON(http.StatusOK, ref)
}
