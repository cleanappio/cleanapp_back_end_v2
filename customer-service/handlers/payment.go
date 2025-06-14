package handlers

import (
	"net/http"
	"strconv"

	"customer-service/models"
	"github.com/gin-gonic/gin"
)

// GetPaymentMethods retrieves customer's payment methods
func (h *Handlers) GetPaymentMethods(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	methods, err := h.service.GetPaymentMethods(c.Request.Context(), customerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to get payment methods"})
		return
	}

	c.JSON(http.StatusOK, methods)
}

// AddPaymentMethod adds a new payment method
func (h *Handlers) AddPaymentMethod(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.AddPaymentMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	if err := h.service.AddPaymentMethod(c.Request.Context(), customerID, req.StripePaymentMethodID, req.IsDefault); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to add payment method"})
		return
	}

	c.JSON(http.StatusCreated, models.MessageResponse{Message: "payment method added successfully"})
}

// UpdatePaymentMethod updates an existing payment method
func (h *Handlers) UpdatePaymentMethod(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	paymentMethodID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid payment method id"})
		return
	}

	var req struct {
		IsDefault bool `json:"is_default"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	if err := h.service.UpdatePaymentMethod(c.Request.Context(), customerID, paymentMethodID, req.IsDefault); err != nil {
		if err.Error() == "payment method not found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to update payment method"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "payment method updated successfully"})
}

// DeletePaymentMethod removes a payment method
func (h *Handlers) DeletePaymentMethod(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	paymentMethodID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid payment method id"})
		return
	}

	if err := h.service.DeletePaymentMethod(c.Request.Context(), customerID, paymentMethodID); err != nil {
		if err.Error() == "payment method not found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to delete payment method"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "payment method deleted successfully"})
}

// ProcessPayment processes a payment webhook from Stripe
func (h *Handlers) ProcessPayment(c *gin.Context) {
	// In production, you would:
	// 1. Verify the webhook signature using the Stripe webhook secret
	// 2. Parse the Stripe event
	// 3. Handle different event types (payment_intent.succeeded, payment_intent.failed, etc.)
	// 4. Update your database accordingly

	var req models.StripeWebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// TODO: Implement Stripe webhook handling
	// For now, just acknowledge receipt
	c.JSON(http.StatusOK, models.MessageResponse{Message: "webhook received"})
}
