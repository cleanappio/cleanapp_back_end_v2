package backend

import (
	"cleanapp/api"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func (h *handler) ReadReferral(c *gin.Context) {
	refQuery := &api.ReferralQuery{}
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

	c.JSON(http.StatusOK, api.ReferralResult{
		RefValue: refValue,
	})
}

func (h *handler) WriteReferral(c *gin.Context) {
	refData := &api.ReferralData{}
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
