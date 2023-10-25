package service

import (
	"flag"
	"net/http"

	"cleanapp/common"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

var (
	mysqlAddress = flag.String("mysql_address", "server:dev_pass@tcp(127.0.0.1:3306)/cleanapp", "MySQL address string")
)

type referralQuery struct {
	RefKey string `form:"refkey"`
}

type referralResult struct {
	RefValue string `json:"refvalue"`
}

type referralData struct {
	RefKey string `json:"refkey"`
	RefValue string `json:"refvalue"`
}

type ReferralHandler struct {
	db *referralDB
}

func NewHandler() (*ReferralHandler, error) {
	sqldb, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		return nil, err
	}

	return &ReferralHandler{
		db: &referralDB{db: sqldb},
	}, nil
}

func (h *ReferralHandler) ReadReferral(c *gin.Context) {
	refQuery := &referralQuery{}
	if err := c.BindQuery(refQuery); err != nil {
		log.Errorf("query binding, %w", err)
		c.Error(err)
		c.Status(http.StatusBadRequest)
		return
	}

	refValue, err := h.db.ReadReferral(refQuery.RefKey)
	if err != nil {
		log.Errorf("referral reading, %v", err)
		c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, referralResult{
		RefValue: refValue,
	})
}

func (h *ReferralHandler) WriteReferral(c *gin.Context) {
	refData := &referralData{}
	if err := c.BindJSON(refData); err != nil {
		log.Errorf("JSON binding, %w", err)
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	if err := h.db.WriteReferral(refData.RefKey, refData.RefValue); err != nil {
		c.Error(err)
		log.Errorf("referral writing, %w", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}