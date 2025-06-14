package handlers

import (
	"net/http"

	"customer-service/models"
	"github.com/gin-gonic/gin"
)

// CreateSubscription creates a new subscription for the customer
func (h *Handlers) CreateSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.CreateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	subscription, err := h.service.CreateSubscription(c.Request.Context(), customerID, req)
	if err != nil {
		if err.Error() == "customer already has an active subscription" {
			c.JSON(http.StatusConflict, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to create subscription"})
		return
	}

	c.JSON(http.StatusCreated, subscription)
}

// GetSubscription retrieves the customer's current subscription
func (h *Handlers) GetSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	subscription, err := h.service.GetSubscription(c.Request.Context(), customerID)
	if err != nil {
		if err.Error() == "no active subscription found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to get subscription"})
		return
	}

	c.JSON(http.StatusOK, subscription)
}

// UpdateSubscription updates the customer's subscription plan
func (h *Handlers) UpdateSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.UpdateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	if err := h.service.UpdateSubscription(c.Request.Context(), customerID, req); err != nil {
		if err.Error() == "no active subscription found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to update subscription"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "subscription updated successfully"})
}

// CancelSubscription cancels the customer's subscription
func (h *Handlers) CancelSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	if err := h.service.CancelSubscription(c.Request.Context(), customerID); err != nil {
		if err.Error() == "no active subscription found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to cancel subscription"})
		return
	}

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
