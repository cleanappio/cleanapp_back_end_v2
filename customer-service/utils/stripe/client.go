package stripe

import (
	"fmt"
	"customer-service/config"
	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/paymentmethod"
	"github.com/stripe/stripe-go/v76/subscription"
	"github.com/stripe/stripe-go/v76/webhook"
)

// Client wraps Stripe API operations
type Client struct {
	config *config.Config
}

// NewClient creates a new Stripe client
func NewClient(cfg *config.Config) *Client {
	stripe.Key = cfg.StripeSecretKey
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
	return customer.New(params)
}

// AttachPaymentMethod attaches a payment method to a customer
func (c *Client) AttachPaymentMethod(paymentMethodID, customerID string) (*stripe.PaymentMethod, error) {
	params := &stripe.PaymentMethodAttachParams{
		Customer: stripe.String(customerID),
	}
	return paymentmethod.Attach(paymentMethodID, params)
}

// DetachPaymentMethod detaches a payment method
func (c *Client) DetachPaymentMethod(paymentMethodID string) (*stripe.PaymentMethod, error) {
	params := &stripe.PaymentMethodDetachParams{}
	return paymentmethod.Detach(paymentMethodID, params)
}

// SetDefaultPaymentMethod sets the default payment method for a customer
func (c *Client) SetDefaultPaymentMethod(customerID, paymentMethodID string) error {
	params := &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{
			DefaultPaymentMethod: stripe.String(paymentMethodID),
		},
	}
	_, err := customer.Update(customerID, params)
	return err
}

// CreateSubscription creates a subscription in Stripe
func (c *Client) CreateSubscription(customerID string, planType, billingCycle string) (*stripe.Subscription, error) {
	priceKey := fmt.Sprintf("%s_%s", planType, billingCycle)
	priceID := c.config.StripePrices[priceKey]
	
	if priceID == "" {
		return nil, fmt.Errorf("price not configured for %s", priceKey)
	}

	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{
				Price: stripe.String(priceID),
			},
		},
		PaymentBehavior: stripe.String("default_incomplete"),
		Expand:          []*string{stripe.String("latest_invoice.payment_intent")},
		Metadata: map[string]string{
			"plan_type":     planType,
			"billing_cycle": billingCycle,
		},
	}
	
	return subscription.New(params)
}

// UpdateSubscription updates a subscription in Stripe
func (c *Client) UpdateSubscription(subscriptionID, planType, billingCycle string) (*stripe.Subscription, error) {
	// First get the current subscription
	sub, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		return nil, err
	}

	priceKey := fmt.Sprintf("%s_%s", planType, billingCycle)
	newPriceID := c.config.StripePrices[priceKey]
	
	if newPriceID == "" {
		return nil, fmt.Errorf("price not configured for %s", priceKey)
	}

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

	return subscription.Update(subscriptionID, params)
}

// CancelSubscription cancels a subscription in Stripe
func (c *Client) CancelSubscription(subscriptionID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionCancelParams{
		InvoiceNow: stripe.Bool(true),
		Prorate:    stripe.Bool(true),
	}
	return subscription.Cancel(subscriptionID, params)
}

// ReactivateSubscription reactivates a cancelled subscription
func (c *Client) ReactivateSubscription(subscriptionID string) (*stripe.Subscription, error) {
	// For a cancelled subscription that hasn't ended yet, we can update it
	params := &stripe.SubscriptionParams{
		CancelAtPeriodEnd: stripe.Bool(false),
	}
	return subscription.Update(subscriptionID, params)
}

// GetPaymentMethod retrieves payment method details
func (c *Client) GetPaymentMethod(paymentMethodID string) (*stripe.PaymentMethod, error) {
	return paymentmethod.Get(paymentMethodID, nil)
}

// ConstructWebhookEvent constructs a webhook event from the request
func (c *Client) ConstructWebhookEvent(payload []byte, header string) (stripe.Event, error) {
	return webhook.ConstructEvent(payload, header, c.config.StripeWebhookSecret)
}

// GetSubscription retrieves a subscription from Stripe
func (c *Client) GetSubscription(subscriptionID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionParams{
		Expand: []*string{stripe.String("latest_invoice")},
	}
	return subscription.Get(subscriptionID, params)
}
