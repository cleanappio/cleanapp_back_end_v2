package backend

import (
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type referralQuery struct {
	RefKey string `form:"refkey"`
}

type referralResult struct {
	RefValue string `json:"refvalue"`
}

type referralData struct {
	RefKey   string `json:"refkey"`
	RefValue string `json:"refvalue"`
}

func (h *handler) readReferral(c *gin.Context) {
	refQuery := &referralQuery{}
	if err := c.BindQuery(refQuery); err != nil {
		log.Errorf("query binding, %w", err)
		c.Error(err)
		c.Status(http.StatusBadRequest)
		return
	}

	refValue, err := h.sDB.readReferral(refQuery.RefKey)
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

func (h *handler) writeReferral(c *gin.Context) {
	refData := &referralData{}
	if err := c.BindJSON(refData); err != nil {
		log.Errorf("JSON binding, %w", err)
		c.Status(http.StatusBadRequest)
		c.Error(err)
		return
	}

	if err := h.sDB.writeReferral(refData.RefKey, refData.RefValue); err != nil {
		c.Error(err)
		log.Errorf("referral writing, %w", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}
