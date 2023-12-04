package be

import (
	"cleanapp/common"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type ReferralQuery struct {
	RefKey string `form:"refkey"` // A key in format <IPAddress>:<screenwidth>:<screenheight>
}

type ReferralResult struct {
	RefValue string `json:"refvalue"` // A referral code, example: aSvd3B6fEhJ
}

type ReferralData struct {
	RefKey   string `json:"refkey"`   // A key in format <IPAddress>:<screenwidth>:<screenheight>
	RefValue string `json:"refvalue"` // A referral code, example: aSvd3B6fEhJ
}

type GenRefRequest struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
}

type GenRefResponse struct {
	RefValue string `json:"refvalue"` // A referral code, example: aSvd3B6fEhJ
}

func ReadReferral(c *gin.Context) {
	refQuery := &ReferralQuery{}
	if err := c.BindJSON(refQuery); err != nil {
		log.Errorf("JSON binding, %w", err)
		c.Error(err)
		c.Status(http.StatusBadRequest)
		return
	}

	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		log.Errorf("%v", err)
		return
	}
	defer db.Close()

	refValue, err := readReferral(db, refQuery.RefKey)
	if err != nil {
		log.Errorf("referral reading, %v", err)
		c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, ReferralResult{
		RefValue: refValue,
	})
}

func WriteReferral(c *gin.Context) {
	refData := &ReferralData{}
	if err := c.BindJSON(refData); err != nil {
		log.Errorf("JSON binding, %w", err)
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		log.Errorf("%v", err)
		return
	}
	defer db.Close()

	if err := writeReferral(db, refData.RefKey, refData.RefValue); err != nil {
		c.Error(err)
		log.Errorf("referral writing, %w", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}

func GenerateReferral(c *gin.Context) {
	req := &GenRefRequest{}
	if err := c.BindJSON(req); err != nil {
		log.Errorf("JSON binding, %w", err)
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	if req.Version != "2.0" {
		log.Errorf("Bad version in /report, expected: 2.0, got: %v", req.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		log.Errorf("%v", err)
		return
	}
	defer db.Close()

	ref, err := generateReferral(db, req, randRefGen)
	if err != nil {
		log.Errorf("referral generating, %w", err)
		c.Status(http.StatusInternalServerError)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, ref)
}
