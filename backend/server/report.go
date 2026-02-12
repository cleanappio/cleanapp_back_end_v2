package server

import (
	"cleanapp/common/disburse"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/backend/stxn"

	"github.com/apex/log"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

func Report(c *gin.Context) {
	var report = &api.ReportArgs{}

	// Get the arguments.
	if err := c.BindJSON(report); err != nil {
		log.Errorf("Failed to get the argument in /report call: %w", err)
		return
	}

	if report.Version != "2.0" {
		log.Errorf("Bad version in /report, expected: 2.0, got: %v", report.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Error connecting to DB: %w", err)
		return
	}

	// Add report to the database.
	savedReport, err := db.SaveReport(dbc, report)
	if err != nil {
		log.Errorf("Failed to write report with %w", err)
		c.String(http.StatusInternalServerError, "Failed to save the report.") // 500
		return
	}

	// Publish report to RabbitMQ for analysis
	publishReport(savedReport)

	c.JSON(http.StatusOK, api.ReportResponse{Seq: savedReport.Seq})

	go stxn.SendReport(ethcommon.HexToAddress(report.Id), disburse.ToWei(1.0))
}

// publishReportToAnalysis publishes a report to RabbitMQ for analysis
func publishReport(report *api.Report) {
	// Check if publisher is initialized
	if rabbitmqPublisher == nil {
		log.Errorf("RabbitMQ publisher not initialized, cannot publish report %d", report.Seq)
		return
	}

	// Create the report data structure for the analysis service
	newReport := struct {
		Seq         int     `json:"seq"`
		Timestamp   string  `json:"timestamp"`
		ID          string  `json:"id"`
		Team        int     `json:"team"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		X           float64 `json:"x"`
		Y           float64 `json:"y"`
		ActionID    string  `json:"action_id"`
		Description string  `json:"description"`
	}{
		Seq:         report.Seq,
		Timestamp:   report.Timestamp,
		ID:          report.ID,
		Team:        report.Team,
		Latitude:    report.Latitude,
		Longitude:   report.Longitude,
		X:           report.X,
		Y:           report.Y,
		ActionID:    report.ActionID,
		Description: report.Description,
	}

	// Publish the report to RabbitMQ
	err := rabbitmqPublisher.Publish(newReport)
	if err != nil {
		log.Errorf("Failed to publish report %d to RabbitMQ: %v", report.Seq, err)
		return
	}

	log.Infof("Successfully published report %d to RabbitMQ for analysis", report.Seq)
}
