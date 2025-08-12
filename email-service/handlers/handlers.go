package handlers

import (
	"fmt"
	"net/http"
	"time"

	"email-service/service"

	"github.com/gin-gonic/gin"
)

// OptOutRequest represents the request body for opting out an email
type OptOutRequest struct {
	Email string `json:"email" binding:"required"`
}

// OptOutResponse represents the response for opt-out requests
type OptOutResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// EmailServiceHandler handles HTTP requests for the email service
type EmailServiceHandler struct {
	emailService *service.EmailService
}

// NewEmailServiceHandler creates a new handler instance
func NewEmailServiceHandler(emailService *service.EmailService) *EmailServiceHandler {
	return &EmailServiceHandler{
		emailService: emailService,
	}
}

// HandleOptOut handles POST requests to /api/v3/optout
func (h *EmailServiceHandler) HandleOptOut(c *gin.Context) {
	var req OptOutRequest

	// Parse and validate request body using Gin's binding
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body: " + err.Error(),
		})
		return
	}

	// Validate email
	if req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Email is required",
		})
		return
	}

	// Add email to opted out table
	err := h.emailService.AddOptedOutEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to opt out email: %v", err),
		})
		return
	}

	// Return success response
	response := OptOutResponse{
		Success: true,
		Message: fmt.Sprintf("Email %s has been opted out successfully", req.Email),
	}

	c.JSON(http.StatusOK, response)
}

// HandleOptOutLink handles GET requests to /opt-out with email parameter
func (h *EmailServiceHandler) HandleOptOutLink(c *gin.Context) {
	email := c.Query("email")

	if email == "" {
		c.HTML(http.StatusBadRequest, "optout_error.html", gin.H{
			"error": "Email parameter is required",
		})
		return
	}

	// Add email to opted out table
	err := h.emailService.AddOptedOutEmail(email)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "optout_error.html", gin.H{
			"error": fmt.Sprintf("Failed to opt out email: %v", err),
		})
		return
	}

	// Show success page
	c.HTML(http.StatusOK, "optout_success.html", gin.H{
		"email":   email,
		"message": fmt.Sprintf("Email %s has been opted out successfully", email),
	})
}

// HandleHealth handles GET requests to /health
func (h *EmailServiceHandler) HandleHealth(c *gin.Context) {
	response := gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "email-service",
	}

	c.JSON(http.StatusOK, response)
}
