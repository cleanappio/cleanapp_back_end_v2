package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"customer-service/database"
	"customer-service/models"
	"customer-service/utils/stripe"

	"github.com/gin-gonic/gin"
)

// Handlers contains all HTTP handlers
type Handlers struct {
	service      *database.CustomerService
	stripeClient *stripe.Client // Add this field
}

// NewHandlers creates a new handlers instance
func NewHandlers(service *database.CustomerService, stripeClient *stripe.Client) *Handlers {
	return &Handlers{
		service:      service,
		stripeClient: stripeClient,
	}
}

// CreateCustomer handles customer registration
func (h *Handlers) CreateCustomer(c *gin.Context) {
	var req models.CreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	customer, err := h.service.CreateCustomer(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to create customer"})
		return
	}

	c.JSON(http.StatusCreated, customer)
}

// UpdateCustomer handles customer information updates
func (h *Handlers) UpdateCustomer(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	if err := h.service.UpdateCustomer(c.Request.Context(), customerID, req); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to update customer"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "customer updated successfully"})
}

// DeleteCustomer handles customer account deletion
func (h *Handlers) DeleteCustomer(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	if err := h.service.DeleteCustomer(c.Request.Context(), customerID); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to delete customer"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "customer deleted successfully"})
}

// GetCustomer retrieves customer information
func (h *Handlers) GetCustomer(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	customer, err := h.service.GetCustomer(c.Request.Context(), customerID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "customer not found"})
		return
	}

	c.JSON(http.StatusOK, customer)
}

// Login handles customer authentication
func (h *Handlers) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Authenticate and get customer ID
	customerID, err := h.service.AuthenticateCustomer(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Generate token pair
	token, refreshToken, err := h.service.GenerateTokenPair(c.Request.Context(), customerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate tokens"})
		return
	}

	c.JSON(http.StatusOK, models.TokenResponse{
		Token:        token,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600, // 1 hour
	})
}

// HealthCheck returns the service health status
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "customer-service",
	})
}

// RefreshToken handles token refresh
func (h *Handlers) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Validate refresh token and get customer ID
	customerID, err := h.service.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "invalid refresh token"})
		return
	}

	// Generate new access token
	token, refreshToken, err := h.service.GenerateTokenPair(c.Request.Context(), customerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, models.TokenResponse{
		Token:        token,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600, // 1 hour
	})
}

// Logout handles customer logout
func (h *Handlers) Logout(c *gin.Context) {
	customerID := c.GetString("customer_id")
	token := c.GetString("token")

	if customerID == "" || token == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	// Invalidate the token
	if err := h.service.InvalidateToken(c.Request.Context(), customerID, token); err != nil {
		// Log error but still return success to client
		c.JSON(http.StatusOK, models.MessageResponse{Message: "logged out successfully"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "logged out successfully"})
}

// OAuthLogin handles OAuth authentication
func (h *Handlers) OAuthLogin(c *gin.Context) {
	var req models.OAuthLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Validate OAuth token with provider
	userInfo, err := h.service.ValidateOAuthToken(c.Request.Context(), req.Provider, req.IDToken, req.AccessToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "invalid oauth token"})
		return
	}

	// Check if customer exists or create new one
	customer, isNew, err := h.service.GetOrCreateOAuthCustomer(c.Request.Context(), req.Provider, userInfo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to process oauth login"})
		return
	}

	// Generate tokens
	token, refreshToken, err := h.service.GenerateTokenPair(c.Request.Context(), customer.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate token"})
		return
	}

	response := models.TokenResponse{
		Token:        token,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
	}

	// Add customer info for new registrations
	if isNew {
		response.Customer = customer
	}

	c.JSON(http.StatusOK, response)
}

// GetOAuthURL returns the OAuth provider URL for authentication
func (h *Handlers) GetOAuthURL(c *gin.Context) {
	provider := c.Param("provider")
	
	// Validate provider
	if provider != "google" && provider != "facebook" && provider != "apple" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid provider"})
		return
	}

	// Generate OAuth URL based on provider
	url, state, err := h.service.GenerateOAuthURL(provider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate oauth url"})
		return
	}

	c.JSON(http.StatusOK, models.OAuthURLResponse{
		URL:   url,
		State: state,
	})
}

// ReactivateSubscription reactivates a cancelled subscription
func (h *Handlers) ReactivateSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	subscription, err := h.service.ReactivateSubscription(c.Request.Context(), customerID)
	if err != nil {
		if err.Error() == "no cancelled subscription found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to reactivate subscription"})
		return
	}

	c.JSON(http.StatusOK, subscription)
}

// DownloadInvoice returns a billing invoice
func (h *Handlers) DownloadInvoice(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	billingID := c.Param("id")
	
	// Get billing record and verify ownership
	billing, err := h.service.GetBillingRecord(c.Request.Context(), customerID, billingID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "invoice not found"})
		return
	}

	// Generate or retrieve invoice PDF
	invoiceData, contentType, err := h.service.GenerateInvoice(c.Request.Context(), billing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate invoice"})
		return
	}

	// Set headers for file download
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "attachment; filename=invoice-"+billingID+".pdf")
	c.Header("Content-Length", fmt.Sprint(len(invoiceData)))
	
	c.Data(http.StatusOK, contentType, invoiceData)
}

// GetAreas returns available service areas
func (h *Handlers) GetAreas(c *gin.Context) {
	areas, err := h.service.GetAreas(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to get areas"})
		return
	}

	c.JSON(http.StatusOK, areas)
}

// RootHealthCheck returns service health at root level
func (h *Handlers) RootHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}