package server

import (
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/common"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func GetValidReportsCount(c *gin.Context) {
	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("Error connecting to DB: %w", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer dbc.Close()

	total, physical, digital, err := db.GetValidReportsCounts(dbc)
	if err != nil {
		log.Errorf("Failed to get valid reports counts: %w", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total_reports":          total,
		"total_physical_reports": physical,
		"total_digital_reports":  digital,
	})
}
