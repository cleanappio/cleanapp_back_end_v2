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

// Customer represents a CleanApp customer
type Customer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LoginMethod represents an authentication method for a customer
type LoginMethod struct {
	ID         int       `json:"id"`
	CustomerID string    `json:"customer_id"`
	MethodType string    `json:"method_type"`
	OAuthID    string    `json:"oauth_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
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
	CardholderName        string `json:"cardholder_name"`
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
type CreateCustomerRequest struct {
	Name         string   `json:"name" binding:"required,max=256"`
	Email        string   `json:"email" binding:"required,email"`
	Password     string   `json:"password" binding:"required,min=8"`
	AreaIDs      []int    `json:"area_ids" binding:"required,min=1"`
}

// UpdateCustomerRequest represents the request to update customer information
type UpdateCustomerRequest struct {
	Name    *string `json:"name,omitempty" binding:"omitempty,max=256"`
	Email   *string `json:"email,omitempty" binding:"omitempty,email"`
	AreaIDs []int   `json:"area_ids,omitempty"`
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

// LoginRequest represents the authentication request
// For email/password login: provide email and password
// For OAuth login: provide provider and token
type LoginRequest struct {
	Email    string `json:"email" binding:"required_without=Provider"`
	Password string `json:"password" binding:"required_without=Provider"`
	Provider string `json:"provider" binding:"required_without=Email,omitempty,oneof=google apple facebook"`
	Token    string `json:"token" binding:"required_with=Provider"` // OAuth ID from provider
}

// TokenResponse represents the authentication response
type TokenResponse struct {
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresIn    int       `json:"expires_in,omitempty"`
	Customer     *Customer `json:"customer,omitempty"` // Only for new OAuth registrations
}

// OAuthLoginRequest represents an OAuth authentication request
type OAuthLoginRequest struct {
	Provider        string                 `json:"provider" binding:"required,oneof=google facebook apple"`
	IDToken         string                 `json:"id_token,omitempty"`
	AccessToken     string                 `json:"access_token,omitempty"`
	AuthorizationCode string               `json:"authorization_code,omitempty"`
	UserInfo        map[string]interface{} `json:"user_info,omitempty"`
}

// OAuthURLResponse represents the OAuth URL response
type OAuthURLResponse struct {
	URL   string `json:"url"`
	State string `json:"state,omitempty"`
}

// OAuthUserInfo represents user information from OAuth provider
type OAuthUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
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