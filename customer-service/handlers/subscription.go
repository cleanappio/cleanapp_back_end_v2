package handlers

import (
	"net/http"

	"customer-service/models"
	"github.com/gin-gonic/gin"
)

// GetSubscription retrieves the customer's current subscription
func (h *Handlers) GetSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	// TODO: Implement GetSubscription in service layer
	c.JSON(http.StatusOK, gin.H{
		"message": "subscription endpoint - to be implemented",
		"customer_id": customerID,
	})
}

// UpdateSubscription updates the customer's subscription plan
func (h *Handlers) UpdateSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req struct {
		PlanType     string `json:"plan_type" binding:"required,oneof=base advanced exclusive"`
		BillingCycle string `json:"billing_cycle" binding:"required,oneof=monthly annual"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// TODO: Implement UpdateSubscription in service layer
	c.JSON(http.StatusOK, models.MessageResponse{Message: "subscription updated successfully"})
}

// CancelSubscription cancels the customer's subscription
func (h *Handlers) CancelSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	// TODO: Implement CancelSubscription in service layer
	c.JSON(http.StatusOK, models.MessageResponse{Message: "subscription cancelled successfully"})
}

// GetBillingHistory retrieves the customer's billing history
func (h *Handlers) GetBillingHistory(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	// Pagination parameters
	page := c.DefaultQuery("page", "1")
	limit := c.DefaultQuery("limit", "10")

	// TODO: Implement GetBillingHistory in service layer
	c.JSON(http.StatusOK, gin.H{
		"message": "billing history endpoint - to be implemented",
		"customer_id": customerID,
		"page": page,
		"limit": limit,
	})
}
