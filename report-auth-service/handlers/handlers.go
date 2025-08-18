package handlers

import (
	"log"
	"net/http"

	"report-auth-service/database"
	"report-auth-service/models"

	"github.com/gin-gonic/gin"
)

// Handlers handles HTTP requests for the report authorization service
type Handlers struct {
	service *database.ReportAuthService
}

// NewHandlers creates a new handlers instance
func NewHandlers(service *database.ReportAuthService) *Handlers {
	return &Handlers{
		service: service,
	}
}

// CheckReportAuthorization checks if a user is authorized to view specific reports
func (h *Handlers) CheckReportAuthorization(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID := c.GetString("user_id")
	if userID == "" {
		log.Printf("ERROR: User ID not found in context")
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.ReportAuthorizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("ERROR: Invalid JSON in CheckReportAuthorization request for user %s: %v", userID, err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	log.Printf("INFO: Checking report authorization for user %s with %d reports", userID, len(req.ReportSeqs))

	authorizations, err := h.service.CheckReportAuthorization(c.Request.Context(), userID, req.ReportSeqs)
	if err != nil {
		log.Printf("ERROR: Failed to check report authorization for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to check report authorization"})
		return
	}

	response := models.ReportAuthorizationResponse{
		Authorizations: authorizations,
	}

	log.Printf("INFO: Successfully checked report authorization for user %s with %d reports", userID, len(req.ReportSeqs))
	c.JSON(http.StatusOK, response)
}

// HealthCheck returns the service health status
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, models.MessageResponse{
		Message: "Report authorization service is healthy",
	})
}

// RootHealthCheck returns the service health status (root level)
func (h *Handlers) RootHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "report-auth-service",
		"version": "1.0.0",
	})
}
