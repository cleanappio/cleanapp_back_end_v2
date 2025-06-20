package handlers

import (
	"log"
	"net/http"
	"strconv"
	"encoding/json"
	"io"

	"customer-service/models"
	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v76"
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
	// Read the request body
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "failed to read request body"})
		return
	}

	// Get the Stripe signature header
	signatureHeader := c.GetHeader("Stripe-Signature")
	if signatureHeader == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "missing stripe signature"})
		return
	}

	// Construct the event
	event, err := h.stripeClient.ConstructWebhookEvent(payload, signatureHeader)
	if err != nil {
		log.Printf("Webhook signature verification failed: %v", err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid signature"})
		return
	}

	// Handle the event
	switch event.Type {
	case "payment_intent.succeeded":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
			log.Printf("Error parsing payment intent: %v", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "failed to parse event"})
			return
		}
		
		// Process successful payment
		if err := h.service.ProcessSuccessfulPayment(c.Request.Context(), &paymentIntent); err != nil {
			log.Printf("Error processing successful payment: %v", err)
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to process payment"})
			return
		}

	case "payment_intent.payment_failed":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
			log.Printf("Error parsing payment intent: %v", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "failed to parse event"})
			return
		}
		
		// Process failed payment
		if err := h.service.ProcessFailedPayment(c.Request.Context(), &paymentIntent); err != nil {
			log.Printf("Error processing failed payment: %v", err)
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to process payment failure"})
			return
		}

	case "invoice.payment_succeeded":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			log.Printf("Error parsing invoice: %v", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "failed to parse event"})
			return
		}
		
		// Process invoice payment
		if err := h.service.ProcessInvoicePayment(c.Request.Context(), &invoice); err != nil {
			log.Printf("Error processing invoice payment: %v", err)
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to process invoice"})
			return
		}

	case "customer.subscription.created":
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			log.Printf("Error parsing subscription: %v", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "failed to parse event"})
			return
		}
		
		// Handle subscription creation
		log.Printf("Subscription created: %s", subscription.ID)

	case "customer.subscription.updated":
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			log.Printf("Error parsing subscription: %v", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "failed to parse event"})
			return
		}
		
		// Update subscription status
		if err := h.service.UpdateSubscriptionStatus(c.Request.Context(), &subscription); err != nil {
			log.Printf("Error updating subscription status: %v", err)
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to update subscription"})
			return
		}

	case "customer.subscription.deleted":
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			log.Printf("Error parsing subscription: %v", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "failed to parse event"})
			return
		}
		
		// Handle subscription deletion
		if err := h.service.HandleSubscriptionDeletion(c.Request.Context(), &subscription); err != nil {
			log.Printf("Error handling subscription deletion: %v", err)
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to handle deletion"})
			return
		}

	default:
		log.Printf("Unhandled webhook event type: %s", event.Type)
	}

	// Return 200 OK to acknowledge receipt of the event
	c.JSON(http.StatusOK, models.MessageResponse{Message: "webhook processed"})
}
