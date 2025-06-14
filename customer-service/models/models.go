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
	MethodID   string    `json:"method_id,omitempty"`
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

// PaymentMethod represents a customer's payment method
type PaymentMethod struct {
	ID         int    `json:"id"`
	CustomerID string `json:"customer_id"`
	LastFour   string `json:"last_four"`
	CardHolder string `json:"card_holder"`
	Expiry     string `json:"expiry"`
	IsDefault  bool   `json:"is_default"`
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

// CreateCustomerRequest represents the request to create a new customer
type CreateCustomerRequest struct {
	Name         string   `json:"name" binding:"required,max=256"`
	Email        string   `json:"email" binding:"required,email"`
	Password     string   `json:"password" binding:"required,min=8"`
	AreaIDs      []int    `json:"area_ids" binding:"required,min=1"`
	PlanType     string   `json:"plan_type" binding:"required,oneof=base advanced exclusive"`
	BillingCycle string   `json:"billing_cycle" binding:"required,oneof=monthly annual"`
	CardNumber   string   `json:"card_number" binding:"required"`
	CardHolder   string   `json:"card_holder" binding:"required"`
	Expiry       string   `json:"expiry" binding:"required"`
	CVV          string   `json:"cvv" binding:"required,len=3"`
}

// UpdateCustomerRequest represents the request to update customer information
type UpdateCustomerRequest struct {
	Name    *string `json:"name,omitempty" binding:"omitempty,max=256"`
	Email   *string `json:"email,omitempty" binding:"omitempty,email"`
	AreaIDs []int   `json:"area_ids,omitempty"`
}

// LoginRequest represents the authentication request
type LoginRequest struct {
	Email    string `json:"email" binding:"required_without=Provider"`
	Password string `json:"password" binding:"required_without=Provider"`
	Provider string `json:"provider" binding:"required_without=Email,oneof=google apple facebook"`
	Token    string `json:"token" binding:"required_with=Provider"`
}

// TokenResponse represents the authentication response
type TokenResponse struct {
	Token string `json:"token"`
}

// MessageResponse represents a simple message response
type MessageResponse struct {
	Message string `json:"message"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}
