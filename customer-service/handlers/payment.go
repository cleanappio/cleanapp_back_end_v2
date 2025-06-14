package handlers

import (
	"net/http"

	"github.com/cleanapp/customer-service/models"
	"github.com/gin-gonic/gin"
)

// GetPaymentMethods retrieves customer's payment methods
func (h *Handlers) GetPaymentMethods(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	// TODO: Implement GetPaymentMethods in service layer
	c.JSON(http.StatusOK, gin.H{
		"message": "payment methods endpoint - to be implemented",
		"customer_id": customerID,
	})
}

// AddPaymentMethod adds a new payment method
func (h *Handlers) AddPaymentMethod(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req struct {
		CardNumber string `json:"card_number" binding:"required"`
		CardHolder string `json:"card_holder" binding:"required"`
		Expiry     string `json:"expiry" binding:"required"`
		CVV        string `json:"cvv" binding:"required,len=3"`
		IsDefault  bool   `json:"is_default"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// TODO: Implement AddPaymentMethod in service layer
	c.JSON(http.StatusCreated, models.MessageResponse{Message: "payment method added successfully"})
}

// UpdatePaymentMethod updates an existing payment method
func (h *Handlers) UpdatePaymentMethod(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	paymentMethodID := c.Param("id")
	if paymentMethodID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "payment method id required"})
		return
	}

	var req struct {
		IsDefault bool `json:"is_default"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// TODO: Implement UpdatePaymentMethod in service layer
	c.JSON(http.StatusOK, models.MessageResponse{Message: "payment method updated successfully"})
}

// DeletePaymentMethod removes a payment method
func (h *Handlers) DeletePaymentMethod(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	paymentMethodID := c.Param("id")
	if paymentMethodID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "payment method id required"})
		return
	}

	// TODO: Implement DeletePaymentMethod in service layer
	c.JSON(http.StatusOK, models.MessageResponse{Message: "payment method deleted successfully"})
}

// ProcessPayment processes a payment webhook (from payment gateway)
func (h *Handlers) ProcessPayment(c *gin.Context) {
	// This would typically be called by your payment processor (Stripe, etc.)
	// and would include webhook signature verification

	var req struct {
		CustomerID     string  `json:"customer_id"`
		Amount         float64 `json:"amount"`
		Currency       string  `json:"currency"`
		PaymentMethod  string  `json:"payment_method"`
		WebhookSecret  string  `json:"webhook_secret"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// TODO: Verify webhook signature
	// TODO: Process payment in service layer

	c.JSON(http.StatusOK, models.MessageResponse{Message: "payment processed"})
}
