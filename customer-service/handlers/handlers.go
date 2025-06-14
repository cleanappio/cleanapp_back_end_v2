package handlers

import (
	"net/http"

	"customer-service/database"
	"customer-service/models"
	"github.com/gin-gonic/gin"
)

// Handlers contains all HTTP handlers
type Handlers struct {
	service *database.CustomerService
}

// NewHandlers creates a new handlers instance
func NewHandlers(service *database.CustomerService) *Handlers {
	return &Handlers{
		service: service,
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

	token, err := h.service.Login(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.TokenResponse{Token: token})
}

// HealthCheck returns the service health status
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "customer-service",
	})
}
