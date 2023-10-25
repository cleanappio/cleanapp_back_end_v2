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
		log.Errorf("error in db connecting, %v", err)
		return nil, err
	}

	return &ReferralHandler{
		db: &referralDB{db: sqldb},
	}, nil
}

func (h *ReferralHandler) ReadReferral(c *gin.Context) {
	refQuery := &referralQuery{}
	if err := c.BindQuery(refQuery); err != nil {
		log.Errorf("error in query binding, %v", err)
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	refValue, err := h.db.ReadReferral(refQuery.RefKey)
	if err != nil {
		log.Errorf("error in referral reading, %v", err)
		c.Status(http.StatusInternalServerError)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, referralResult{
		RefValue: refValue,
	})
}

func (h *ReferralHandler) WriteReferral(c *gin.Context) {
	refData := &referralData{}
	if err := c.BindJSON(refData); err != nil {
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	if err := h.db.WriteReferral(refData.RefKey, refData.RefValue); err != nil {
		c.Status(http.StatusInternalServerError)
		c.Error(err)
		return
	}

	c.Status(http.StatusOK)
}