package stripe

import (
	"fmt"
	"log"
	"customer-service/config"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"

	"github.com/stripe/stripe-go/v82/paymentmethod"
	"github.com/stripe/stripe-go/v82/subscription"
	"github.com/stripe/stripe-go/v82/webhook"
)

// Client wraps Stripe API operations
type Client struct {
	config *config.Config
}

// NewClient creates a new Stripe client
func NewClient(cfg *config.Config) *Client {
	// Validate that we have a Stripe key
	if cfg.StripeSecretKey == "" {
		log.Fatal("STRIPE_SECRET_KEY is not set in environment variables")
	}
	
	// Set the Stripe API key globally
	stripe.Key = cfg.StripeSecretKey
	
	// Log that Stripe is configured (but don't log the actual key)
	log.Printf("Stripe client initialized with key starting with: %s...", cfg.StripeSecretKey[:7])
	
	return &Client{config: cfg}
}

// CreateCustomer creates a customer in Stripe
func (c *Client) CreateCustomer(email, name, customerID string) (*stripe.Customer, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
		Metadata: map[string]string{
			"customer_id": customerID,
		},
	}
	
	result, err := customer.New(params)
	if err != nil {
		// Log more details about the error
		log.Printf("Failed to create Stripe customer: %v", err)
		return nil, fmt.Errorf("failed to create Stripe customer: %w", err)
	}
	
	return result, nil
}

// AttachPaymentMethod attaches a payment method to a customer
func (c *Client) AttachPaymentMethod(paymentMethodID, customerID string) (*stripe.PaymentMethod, error) {
	log.Printf("Attaching payment method %s to customer %s", paymentMethodID, customerID)
	params := &stripe.PaymentMethodAttachParams{
		Customer: stripe.String(customerID),
	}
	
	result, err := paymentmethod.Attach(paymentMethodID, params)
	if err != nil {
		log.Printf("Failed to attach payment method %s to customer %s: %v", paymentMethodID, customerID, err)
		return nil, fmt.Errorf("failed to attach payment method: %w", err)
	}
	
	return result, nil
}

// DetachPaymentMethod detaches a payment method
func (c *Client) DetachPaymentMethod(paymentMethodID string) (*stripe.PaymentMethod, error) {
	params := &stripe.PaymentMethodDetachParams{}
	
	result, err := paymentmethod.Detach(paymentMethodID, params)
	if err != nil {
		log.Printf("Failed to detach payment method %s: %v", paymentMethodID, err)
		return nil, fmt.Errorf("failed to detach payment method: %w", err)
	}
	
	return result, nil
}

// SetDefaultPaymentMethod sets the default payment method for a customer
func (c *Client) SetDefaultPaymentMethod(customerID, paymentMethodID string) error {
	log.Printf("Setting default payment method %s for customer %s", paymentMethodID, customerID)
	params := &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{
			DefaultPaymentMethod: stripe.String(paymentMethodID),
		},
	}
	
	_, err := customer.Update(customerID, params)
	if err != nil {
		log.Printf("Failed to set default payment method for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to set default payment method: %w", err)
	}
	
	return nil
}

// CreateSubscription creates a subscription in Stripe
func (c *Client) CreateSubscription(customerID, planType, billingCycle, paymentMethodID string) (*stripe.Subscription, error) {
	priceKey := fmt.Sprintf("%s_%s", planType, billingCycle)
	priceID := c.config.StripePrices[priceKey]
	
	if priceID == "" {
		return nil, fmt.Errorf("price not configured for %s", priceKey)
	}
	
	log.Printf("Creating subscription for customer %s with price %s", customerID, priceID)

	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{
				Price: stripe.String(priceID),
			},
		},
		PaymentBehavior: stripe.String("error_if_incomplete"),
		Expand:          []*string{
			stripe.String("latest_invoice"),
			stripe.String("latest_invoice.payment_intent"),
		},
		PaymentSettings: &stripe.SubscriptionPaymentSettingsParams{
			PaymentMethodOptions: nil,
			SaveDefaultPaymentMethod: stripe.String("on_subscription"),
		},
		DefaultPaymentMethod: &paymentMethodID,
		Metadata: map[string]string{
			"plan_type":     planType,
			"billing_cycle": billingCycle,
		},
	}
	
	resultSubscr, err := subscription.New(params)
	if err != nil {
		log.Printf("Failed to create subscription: %v", err)
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}
	
	return resultSubscr, nil
}

// UpdateSubscription updates a subscription in Stripe
func (c *Client) UpdateSubscription(subscriptionID, planType, billingCycle string) (*stripe.Subscription, error) {
	// First get the current subscription
	sub, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		log.Printf("Failed to get subscription %s: %v", subscriptionID, err)
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	priceKey := fmt.Sprintf("%s_%s", planType, billingCycle)
	newPriceID := c.config.StripePrices[priceKey]
	
	if newPriceID == "" {
		return nil, fmt.Errorf("price not configured for %s", priceKey)
	}

	log.Printf("Updating subscription %s to price %s", subscriptionID, newPriceID)

	// Update the subscription item with the new price
	params := &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{
			{
				ID:    stripe.String(sub.Items.Data[0].ID),
				Price: stripe.String(newPriceID),
			},
		},
		ProrationBehavior: stripe.String("create_prorations"),
		Metadata: map[string]string{
			"plan_type":     planType,
			"billing_cycle": billingCycle,
		},
	}

	result, err := subscription.Update(subscriptionID, params)
	if err != nil {
		log.Printf("Failed to update subscription %s: %v", subscriptionID, err)
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}
	
	return result, nil
}

// CancelSubscription cancels a subscription in Stripe
func (c *Client) CancelSubscription(subscriptionID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionCancelParams{
		InvoiceNow: stripe.Bool(true),
		Prorate:    stripe.Bool(true),
	}
	
	result, err := subscription.Cancel(subscriptionID, params)
	if err != nil {
		log.Printf("Failed to cancel subscription %s: %v", subscriptionID, err)
		return nil, fmt.Errorf("failed to cancel subscription: %w", err)
	}
	
	return result, nil
}

// ReactivateSubscription reactivates a canceled subscription
func (c *Client) ReactivateSubscription(subscriptionID string) (*stripe.Subscription, error) {
	// For a canceled subscription that hasn't ended yet, we can update it
	params := &stripe.SubscriptionParams{
		CancelAtPeriodEnd: stripe.Bool(false),
	}
	
	result, err := subscription.Update(subscriptionID, params)
	if err != nil {
		log.Printf("Failed to reactivate subscription %s: %v", subscriptionID, err)
		return nil, fmt.Errorf("failed to reactivate subscription: %w", err)
	}
	
	return result, nil
}

// GetPaymentMethod retrieves payment method details
func (c *Client) GetPaymentMethod(paymentMethodID string) (*stripe.PaymentMethod, error) {
	result, err := paymentmethod.Get(paymentMethodID, nil)
	if err != nil {
		log.Printf("Failed to get payment method %s: %v", paymentMethodID, err)
		return nil, fmt.Errorf("failed to get payment method: %w", err)
	}
	
	return result, nil
}

// ConstructWebhookEvent constructs a webhook event from the request
func (c *Client) ConstructWebhookEvent(payload []byte, header string) (stripe.Event, error) {
	if c.config.StripeWebhookSecret == "" {
		return stripe.Event{}, fmt.Errorf("webhook secret not configured")
	}
	
	event, err := webhook.ConstructEvent(payload, header, c.config.StripeWebhookSecret)
	if err != nil {
		log.Printf("Failed to construct webhook event: %v", err)
		return stripe.Event{}, fmt.Errorf("failed to construct webhook event: %w", err)
	}
	
	return event, nil
}

// GetSubscription retrieves a subscription from Stripe
func (c *Client) GetSubscription(subscriptionID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionParams{
		Expand: []*string{stripe.String("latest_invoice")},
	}
	
	result, err := subscription.Get(subscriptionID, params)
	if err != nil {
		log.Printf("Failed to get subscription %s: %v", subscriptionID, err)
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}
	
	return result, nil
}
