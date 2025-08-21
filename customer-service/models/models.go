package models

import "time"

// Constants for subscription plans
const (
	PlanBase      = "base"
	PlanAdvanced  = "advanced"
	PlanExclusive = "exclusive"

	BillingMonthly = "monthly"
	BillingAnnual  = "annual"
)

// Customer represents a CleanApp customer (subscription-focused)
// Auth data (name, email) is managed by the auth-service
type Customer struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Subscription represents a customer's subscription plan
type Subscription struct {
	ID              int       `json:"id"`
	CustomerID      string    `json:"customer_id"`
	PlanType        string    `json:"plan_type"`
	BillingCycle    string    `json:"billing_cycle"`
	Status          string    `json:"status"`
	StartDate       time.Time `json:"start_date"`
	NextBillingDate time.Time `json:"next_billing_date"`
}

// PaymentMethod represents a customer's payment method stored in Stripe
type PaymentMethod struct {
	ID                    int    `json:"id"`
	CustomerID            string `json:"customer_id"`
	StripePaymentMethodID string `json:"stripe_payment_method_id"`
	StripeCustomerID      string `json:"stripe_customer_id"`
	LastFour              string `json:"last_four"`
	Brand                 string `json:"brand"` // visa, mastercard, amex, etc.
	ExpMonth              int    `json:"exp_month"`
	ExpYear               int    `json:"exp_year"`
	IsDefault             bool   `json:"is_default"`
}

// BillingHistory represents a billing transaction
type BillingHistory struct {
	ID             int       `json:"id"`
	CustomerID     string    `json:"customer_id"`
	SubscriptionID int       `json:"subscription_id"`
	Amount         float64   `json:"amount"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	PaymentDate    time.Time `json:"payment_date"`
}

// Area represents a service area
type Area struct {
	ID          int         `json:"id"`
	Name        string      `json:"name"`
	Coordinates interface{} `json:"coordinates,omitempty"`
}

// CreateCustomerRequest represents the request to create a new customer
// Note: name, email, and password are handled by auth-service
// This request is for creating a customer record for subscription purposes
type CreateCustomerRequest struct {
	Areas []CustomerArea `json:"areas" binding:"required,min=1"`
}

// UpdateCustomerRequest represents the request to update customer information
// Note: name and email are handled by auth-service
type UpdateCustomerRequest struct {
	Areas []CustomerArea `json:"areas,omitempty"`
}

// AddCustomerBrandsRequest represents the request to add brands to a customer
type AddCustomerBrandsRequest struct {
	Brands []CustomerBrand `json:"brands"`
}

// RemoveCustomerBrandsRequest represents the request to remove brands from a customer
type RemoveCustomerBrandsRequest struct {
	Brands []CustomerBrand `json:"brands"`
}

// UpdateCustomerBrandsRequest represents the request to update all brands for a customer
type UpdateCustomerBrandsRequest struct {
	Brands []CustomerBrand `json:"brands"`
}

// CustomerBrand represents a customer's brand association with public/private status
type CustomerBrand struct {
	CustomerID string `json:"customer_id"`
	BrandName  string `json:"brand_name"`
	IsPublic   bool   `json:"is_public"`
}

// CustomerBrandsResponse represents the response for customer brands
type CustomerBrandsResponse struct {
	CustomerID string          `json:"customer_id"`
	Brands     []CustomerBrand `json:"brands"`
}

// CreateSubscriptionRequest represents the request to create a subscription
type CreateSubscriptionRequest struct {
	PlanType              string `json:"plan_type" binding:"required,oneof=base advanced exclusive"`
	BillingCycle          string `json:"billing_cycle" binding:"required,oneof=monthly annual"`
	StripePaymentMethodID string `json:"stripe_payment_method_id" binding:"required"`
}

// UpdateSubscriptionRequest represents the request to update a subscription
type UpdateSubscriptionRequest struct {
	PlanType     string `json:"plan_type" binding:"required,oneof=base advanced exclusive"`
	BillingCycle string `json:"billing_cycle" binding:"required,oneof=monthly annual"`
}

// MessageResponse represents a simple message response
type MessageResponse struct {
	Message string `json:"message"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// AddPaymentMethodRequest represents the request to add a new payment method via Stripe
type AddPaymentMethodRequest struct {
	StripePaymentMethodID string `json:"stripe_payment_method_id" binding:"required"`
	IsDefault             bool   `json:"is_default"`
}

// StripeWebhookRequest represents a Stripe webhook payload
type StripeWebhookRequest struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

type Price struct {
	Product  string `json:"product" binding:"required, oneof=base advanced exclusive"`
	Period   string `json:"period" binding:"required, oneof=monthly annual"` // monthly or annual
	Amount   int64  `json:"amount" binding:"required,min=0"`                 // in cents
	Currency string `json:"currency" binding:"required"`
}

type PricesResponse struct {
	Prices []Price `json:"prices"`
}

// UserProfile represents user profile data from auth-service
type UserProfile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// CustomerArea represents a customer's area association
type CustomerArea struct {
	CustomerID string    `json:"customer_id"`
	AreaID     int       `json:"area_id"`
	IsPublic   bool      `json:"is_public"`
	CreatedAt  time.Time `json:"created_at"`
}

// AddCustomerAreasRequest represents the request to add areas to a customer
type AddCustomerAreasRequest struct {
	Areas []CustomerArea `json:"areas"`
}

// UpdateCustomerAreasRequest represents the request to replace all areas for a customer
type UpdateCustomerAreasRequest struct {
	Areas []CustomerArea `json:"areas"`
}

// DeleteCustomerAreasRequest represents the request to remove areas from a customer
type DeleteCustomerAreasRequest struct {
	Areas []CustomerArea `json:"areas"`
}

// CustomerAreasResponse represents the response for customer areas
type CustomerAreasResponse struct {
	Areas []CustomerArea `json:"areas"`
	Count int            `json:"count"`
}
