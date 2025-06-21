package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"encoding/json"
	"io"

	"customer-service/models"
	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v82"
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

// ProcessPayment processes a payment webhook from Stripe with idempotency
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

	// Check if we've already processed this event (idempotency check)
	if processed, err := h.service.IsWebhookEventProcessed(c.Request.Context(), event.ID); err != nil {
		log.Printf("Error checking webhook event: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to check event"})
		return
	} else if processed {
		log.Printf("Webhook event %s already processed, skipping", event.ID)
		c.JSON(http.StatusOK, models.MessageResponse{Message: "event already processed"})
		return
	}

	// Mark event as being processed
	if err := h.service.MarkWebhookEventProcessing(c.Request.Context(), event.ID, string(event.Type), payload); err != nil {
		log.Printf("Error marking webhook event: %v", err)
		// Continue processing anyway, but log the error
	}

	// Process the event
	var processingError error
	switch event.Type {
	case "payment_intent.succeeded":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
			processingError = fmt.Errorf("failed to parse payment intent: %w", err)
		} else {
			processingError = h.service.ProcessSuccessfulPayment(c.Request.Context(), &paymentIntent)
		}

	case "payment_intent.payment_failed":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
			processingError = fmt.Errorf("failed to parse payment intent: %w", err)
		} else {
			processingError = h.service.ProcessFailedPayment(c.Request.Context(), &paymentIntent)
		}

	case "invoice.payment_succeeded":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			processingError = fmt.Errorf("failed to parse invoice: %w", err)
		} else {
			processingError = h.service.ProcessInvoicePayment(c.Request.Context(), &invoice)
		}

	case "invoice.payment_failed":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			processingError = fmt.Errorf("failed to parse invoice: %w", err)
		} else {
			processingError = h.service.ProcessFailedInvoicePayment(c.Request.Context(), &invoice)
		}

	case "customer.subscription.created":
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			processingError = fmt.Errorf("failed to parse subscription: %w", err)
		} else {
			log.Printf("Subscription created: %s", subscription.ID)
			processingError = h.service.SyncSubscriptionFromStripe(c.Request.Context(), &subscription)
		}

	case "customer.subscription.updated":
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			processingError = fmt.Errorf("failed to parse subscription: %w", err)
		} else {
			processingError = h.service.UpdateSubscriptionStatus(c.Request.Context(), &subscription)
		}

	case "customer.subscription.deleted":
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			processingError = fmt.Errorf("failed to parse subscription: %w", err)
		} else {
			processingError = h.service.HandleSubscriptionDeletion(c.Request.Context(), &subscription)
		}

	case "customer.subscription.trial_will_end":
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			processingError = fmt.Errorf("failed to parse subscription: %w", err)
		} else {
			// Handle trial ending notification
			processingError = h.service.HandleTrialWillEnd(c.Request.Context(), &subscription)
		}

	case "charge.refunded":
		var charge stripe.Charge
		if err := json.Unmarshal(event.Data.Raw, &charge); err != nil {
			processingError = fmt.Errorf("failed to parse charge: %w", err)
		} else {
			processingError = h.service.ProcessRefund(c.Request.Context(), &charge)
		}

	case "payment_method.attached":
		var paymentMethod stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &paymentMethod); err != nil {
			processingError = fmt.Errorf("failed to parse payment method: %w", err)
		} else {
			processingError = h.service.SyncPaymentMethodFromStripe(c.Request.Context(), &paymentMethod)
		}

	case "payment_method.detached":
		var paymentMethod stripe.PaymentMethod
		if err := json.Unmarshal(event.Data.Raw, &paymentMethod); err != nil {
			processingError = fmt.Errorf("failed to parse payment method: %w", err)
		} else {
			processingError = h.service.HandlePaymentMethodDetached(c.Request.Context(), &paymentMethod)
		}

	default:
		log.Printf("Unhandled webhook event type: %s", event.Type)
		// Mark as skipped
		h.service.MarkWebhookEventSkipped(c.Request.Context(), event.ID)
		c.JSON(http.StatusOK, models.MessageResponse{Message: "event type not handled"})
		return
	}

	// Update webhook event status
	if processingError != nil {
		log.Printf("Error processing webhook event %s: %v", event.ID, processingError)
		h.service.MarkWebhookEventFailed(c.Request.Context(), event.ID, processingError.Error())
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to process event"})
		return
	}

	// Mark as successfully processed
	if err := h.service.MarkWebhookEventProcessed(c.Request.Context(), event.ID); err != nil {
		log.Printf("Error marking webhook event as processed: %v", err)
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "webhook processed"})
}
