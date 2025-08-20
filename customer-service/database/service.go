package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"customer-service/models"
	"customer-service/utils"
	"customer-service/utils/stripe"

	stripego "github.com/stripe/stripe-go/v82"
)

// CustomerService handles all customer-related database operations
type CustomerService struct {
	db             *sql.DB
	stripeClient   *stripe.Client
	authServiceURL string
}

// NewCustomerService creates a new customer service instance
func NewCustomerService(db *sql.DB, stripeClient *stripe.Client, authServiceURL string) *CustomerService {
	return &CustomerService{
		db:             db,
		stripeClient:   stripeClient,
		authServiceURL: authServiceURL,
	}
}

// CreateCustomer creates a new customer record for subscription purposes
// Note: Auth data (name, email) is managed by the auth-service
func (s *CustomerService) CreateCustomer(ctx context.Context, customerID string, req models.CreateCustomerRequest) (*models.Customer, error) {
	log.Printf("INFO: Creating customer %s with %d areas", customerID, len(req.AreaIDs))

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("ERROR: Failed to begin transaction for customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert customer (no name/email - managed by auth-service)
	if err := s.insertCustomer(ctx, tx, customerID); err != nil {
		log.Printf("ERROR: Failed to insert customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to insert customer: %w", err)
	}

	// Insert customer areas
	for _, areaID := range req.AreaIDs {
		if err := s.insertCustomerArea(ctx, tx, customerID, areaID, req.IsPublic); err != nil {
			log.Printf("ERROR: Failed to insert customer area for customer %s, area %d: %v", customerID, areaID, err)
			return nil, fmt.Errorf("failed to insert customer area: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: Failed to commit transaction for customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("INFO: Customer %s created successfully", customerID)

	// Create customer object
	customer := &models.Customer{
		ID:        customerID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return customer, nil
}

// GetCustomer retrieves a customer by ID, creating one if it doesn't exist
func (s *CustomerService) GetCustomer(ctx context.Context, customerID string) (*models.Customer, error) {
	log.Printf("DEBUG: Getting customer %s", customerID)

	var createdAt, updatedAt time.Time

	err := s.db.QueryRowContext(ctx,
		"SELECT created_at, updated_at FROM customers WHERE id = ?",
		customerID).Scan(&createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("INFO: Customer not found, creating new customer: %s", customerID)
			// Customer doesn't exist, create one
			return s.ensureCustomerExists(ctx, customerID)
		}
		log.Printf("ERROR: Failed to query customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to query customer: %w", err)
	}

	return &models.Customer{
		ID:        customerID,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// UpdateCustomer updates customer information
// Note: name and email are managed by auth-service
func (s *CustomerService) UpdateCustomer(ctx context.Context, customerID string, req models.UpdateCustomerRequest) error {
	// Check if customer exists
	_, err := s.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}

	// For now, only area updates are supported
	// Name and email updates should be done through auth-service
	if len(req.AreaIDs) == 0 {
		return nil
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Remove existing areas
	_, err = tx.ExecContext(ctx, "DELETE FROM customer_areas WHERE customer_id = ?", customerID)
	if err != nil {
		return fmt.Errorf("failed to remove existing areas: %w", err)
	}

	// Insert new areas
	for _, areaID := range req.AreaIDs {
		if err := s.insertCustomerArea(ctx, tx, customerID, areaID, req.IsPublic); err != nil {
			return fmt.Errorf("failed to insert customer area: %w", err)
		}
	}

	// Update customer timestamp
	_, err = tx.ExecContext(ctx, "UPDATE customers SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", customerID)
	if err != nil {
		return fmt.Errorf("failed to update customer timestamp: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// DeleteCustomer deletes a customer and all associated data
func (s *CustomerService) DeleteCustomer(ctx context.Context, customerID string) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete customer
	result, err := tx.ExecContext(ctx, "DELETE FROM customers WHERE id = ?", customerID)
	if err != nil {
		return fmt.Errorf("failed to delete customer: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("customer not found")
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// CustomerExists checks if a customer exists by ID
func (s *CustomerService) CustomerExists(ctx context.Context, customerID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM customers WHERE id = ?)",
		customerID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to query customer existence: %w", err)
	}

	return exists, nil
}

// CreateSubscription creates a new subscription for a customer
func (s *CustomerService) CreateSubscription(ctx context.Context, customerID string, req models.CreateSubscriptionRequest) (*models.Subscription, error) {
	log.Printf("INFO: Creating subscription for customer %s, plan: %s, billing: %s", customerID, req.PlanType, req.BillingCycle)

	// Check if customer already has an active subscription
	existing, err := s.GetSubscription(ctx, customerID)
	if err == nil && existing != nil {
		log.Printf("WARNING: Customer %s already has an active subscription", customerID)
		return nil, errors.New("customer already has an active subscription")
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("ERROR: Failed to begin transaction for subscription creation for customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure customer exists first
	_, err = s.ensureCustomerExists(ctx, customerID)
	if err != nil {
		log.Printf("ERROR: Failed to ensure customer exists for subscription creation %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to ensure customer exists: %w", err)
	}

	// Get or create customer's Stripe customer ID
	var stripeCustomerID sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT stripe_customer_id FROM customers WHERE id = ?", customerID).Scan(&stripeCustomerID)
	if err != nil {
		log.Printf("ERROR: Failed to get customer Stripe ID for subscription creation %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to get customer stripe ID: %w", err)
	}

	// If stripe_customer_id is NULL, we need to create a Stripe customer
	if !stripeCustomerID.Valid || stripeCustomerID.String == "" {
		log.Printf("INFO: Creating Stripe customer for subscription creation %s", customerID)

		// Get user profile from auth-service to create Stripe customer
		userProfile, err := s.getUserProfileFromAuthService(ctx, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to get user profile for subscription creation %s: %v", customerID, err)
			return nil, fmt.Errorf("failed to get user profile: %w", err)
		}

		// Create Stripe customer
		stripeCustomer, err := s.stripeClient.CreateCustomer(userProfile.Email, userProfile.Name, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to create Stripe customer for subscription creation %s: %v", customerID, err)
			return nil, fmt.Errorf("failed to create stripe customer: %w", err)
		}

		// Update customer record with Stripe customer ID
		_, err = tx.ExecContext(ctx, "UPDATE customers SET stripe_customer_id = ? WHERE id = ?", stripeCustomer.ID, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to update customer with Stripe ID for subscription creation %s: %v", customerID, err)
			return nil, fmt.Errorf("failed to update customer with stripe ID: %w", err)
		}

		stripeCustomerID.String = stripeCustomer.ID
		stripeCustomerID.Valid = true
		log.Printf("INFO: Successfully created Stripe customer %s for subscription creation %s", stripeCustomerID.String, customerID)
	}

	// Create subscription in Stripe
	stripeSubscription, err := s.stripeClient.CreateSubscription(stripeCustomerID.String, req.PlanType, req.BillingCycle, req.StripePaymentMethodID)
	if err != nil {
		return nil, fmt.Errorf("failed to create stripe subscription: %w", err)
	}

	// Insert subscription into database
	subscriptionID, err := s.insertSubscription(ctx, customerID, req.PlanType, req.BillingCycle, stripeSubscription.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to insert subscription: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Calculate next billing date based on billing cycle
	var nextBillingDate time.Time
	switch req.BillingCycle {
	case "monthly":
		nextBillingDate = time.Now().AddDate(0, 1, 0)
	case "annual", "yearly":
		nextBillingDate = time.Now().AddDate(1, 0, 0)
	default:
		// Default to monthly if billing cycle is not recognized
		nextBillingDate = time.Now().AddDate(0, 1, 0)
	}

	return &models.Subscription{
		ID:              int(subscriptionID),
		CustomerID:      customerID,
		PlanType:        req.PlanType,
		BillingCycle:    req.BillingCycle,
		Status:          "active",
		StartDate:       time.Now(),
		NextBillingDate: nextBillingDate,
	}, nil
}

// GetSubscription retrieves the customer's current subscription
func (s *CustomerService) GetSubscription(ctx context.Context, customerID string) (*models.Subscription, error) {
	log.Printf("DEBUG: Getting subscription for customer %s", customerID)

	var subscription models.Subscription
	err := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, plan_type, billing_cycle, status, start_date, next_billing_date 
		 FROM subscriptions WHERE customer_id = ? AND status = 'active'`,
		customerID).Scan(&subscription.ID, &subscription.CustomerID, &subscription.PlanType,
		&subscription.BillingCycle, &subscription.Status, &subscription.StartDate, &subscription.NextBillingDate)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("WARNING: No active subscription found for customer %s", customerID)
			return nil, errors.New("no active subscription found")
		}
		log.Printf("ERROR: Failed to query subscription for customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to query subscription: %w", err)
	}

	log.Printf("DEBUG: Found subscription %d for customer %s", subscription.ID, customerID)
	return &subscription, nil
}

// UpdateSubscription updates the customer's subscription
func (s *CustomerService) UpdateSubscription(ctx context.Context, customerID string, req models.UpdateSubscriptionRequest) error {
	// Get current subscription
	subscription, err := s.GetSubscription(ctx, customerID)
	if err != nil {
		return err
	}

	// Get subscription's Stripe ID
	var stripeSubscriptionID sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT stripe_subscription_id FROM subscriptions WHERE id = ?", subscription.ID).Scan(&stripeSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to get subscription stripe ID: %w", err)
	}

	if !stripeSubscriptionID.Valid || stripeSubscriptionID.String == "" {
		return fmt.Errorf("subscription does not have a valid Stripe ID")
	}

	// Update subscription in Stripe
	_, err = s.stripeClient.UpdateSubscription(stripeSubscriptionID.String, req.PlanType, req.BillingCycle)
	if err != nil {
		return fmt.Errorf("failed to update stripe subscription: %w", err)
	}

	// Calculate next billing date based on new billing cycle
	var nextBillingDate string
	switch req.BillingCycle {
	case "monthly":
		nextBillingDate = "DATE_ADD(CURRENT_DATE, INTERVAL 1 MONTH)"
	case "annual", "yearly":
		nextBillingDate = "DATE_ADD(CURRENT_DATE, INTERVAL 1 YEAR)"
	default:
		// Default to monthly if billing cycle is not recognized
		nextBillingDate = "DATE_ADD(CURRENT_DATE, INTERVAL 1 MONTH)"
	}

	// Update subscription in database
	_, err = s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE subscriptions SET plan_type = ?, billing_cycle = ?, next_billing_date = %s, updated_at = CURRENT_TIMESTAMP WHERE id = ?", nextBillingDate),
		req.PlanType, req.BillingCycle, subscription.ID)
	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	return nil
}

// CancelSubscription cancels the customer's subscription
func (s *CustomerService) CancelSubscription(ctx context.Context, customerID string) error {
	// Get current subscription
	subscription, err := s.GetSubscription(ctx, customerID)
	if err != nil {
		return err
	}

	// Get subscription's Stripe ID
	var stripeSubscriptionID sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT stripe_subscription_id FROM subscriptions WHERE id = ?", subscription.ID).Scan(&stripeSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to get subscription stripe ID: %w", err)
	}

	if !stripeSubscriptionID.Valid || stripeSubscriptionID.String == "" {
		return fmt.Errorf("subscription does not have a valid Stripe ID")
	}

	// Cancel subscription in Stripe
	_, err = s.stripeClient.CancelSubscription(stripeSubscriptionID.String)
	if err != nil {
		return fmt.Errorf("failed to cancel stripe subscription: %w", err)
	}

	// Update subscription status in database
	_, err = s.db.ExecContext(ctx,
		"UPDATE subscriptions SET status = 'canceled', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		subscription.ID)
	if err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	return nil
}

// ReactivateSubscription reactivates a canceled subscription
func (s *CustomerService) ReactivateSubscription(ctx context.Context, customerID string) (*models.Subscription, error) {
	// Get canceled subscription
	var subscription models.Subscription
	err := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, plan_type, billing_cycle, status, start_date, next_billing_date 
		 FROM subscriptions WHERE customer_id = ? AND status = 'canceled'`,
		customerID).Scan(&subscription.ID, &subscription.CustomerID, &subscription.PlanType,
		&subscription.BillingCycle, &subscription.Status, &subscription.StartDate, &subscription.NextBillingDate)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("no canceled subscription found")
		}
		return nil, fmt.Errorf("failed to query subscription: %w", err)
	}

	// Get subscription's Stripe ID
	var stripeSubscriptionID sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT stripe_subscription_id FROM subscriptions WHERE id = ?", subscription.ID).Scan(&stripeSubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription stripe ID: %w", err)
	}

	if !stripeSubscriptionID.Valid || stripeSubscriptionID.String == "" {
		return nil, fmt.Errorf("subscription does not have a valid Stripe ID")
	}

	// Reactivate subscription in Stripe
	_, err = s.stripeClient.ReactivateSubscription(stripeSubscriptionID.String)
	if err != nil {
		return nil, fmt.Errorf("failed to reactivate stripe subscription: %w", err)
	}

	// Update subscription status in database
	_, err = s.db.ExecContext(ctx,
		"UPDATE subscriptions SET status = 'active', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		subscription.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update subscription status: %w", err)
	}

	subscription.Status = "active"
	return &subscription, nil
}

// GetPaymentMethods retrieves customer's payment methods
func (s *CustomerService) GetPaymentMethods(ctx context.Context, customerID string) ([]models.PaymentMethod, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, customer_id, stripe_payment_method_id, stripe_customer_id, last_four, brand, exp_month, exp_year, is_default
		 FROM payment_methods WHERE customer_id = ? ORDER BY is_default DESC, created_at DESC`,
		customerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query payment methods: %w", err)
	}
	defer rows.Close()

	var methods []models.PaymentMethod
	for rows.Next() {
		var method models.PaymentMethod
		err := rows.Scan(&method.ID, &method.CustomerID, &method.StripePaymentMethodID,
			&method.StripeCustomerID, &method.LastFour, &method.Brand, &method.ExpMonth,
			&method.ExpYear, &method.IsDefault)
		if err != nil {
			return nil, fmt.Errorf("failed to scan payment method: %w", err)
		}
		methods = append(methods, method)
	}

	return methods, nil
}

// AddPaymentMethod adds a new payment method
func (s *CustomerService) AddPaymentMethod(ctx context.Context, customerID, stripePaymentMethodID string, isDefault bool) error {
	log.Printf("INFO: Adding payment method for customer %s, isDefault: %t", customerID, isDefault)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("ERROR: Failed to begin transaction for payment method addition for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure customer exists first
	_, err = s.ensureCustomerExists(ctx, customerID)
	if err != nil {
		log.Printf("ERROR: Failed to ensure customer exists for payment method addition %s: %v", customerID, err)
		return fmt.Errorf("failed to ensure customer exists: %w", err)
	}

	// Get or create customer's Stripe customer ID
	var stripeCustomerID sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT stripe_customer_id FROM customers WHERE id = ?", customerID).Scan(&stripeCustomerID)
	if err != nil {
		log.Printf("ERROR: Failed to get customer Stripe ID for payment method addition %s: %v", customerID, err)
		return fmt.Errorf("failed to get customer stripe ID: %w", err)
	}

	// If stripe_customer_id is NULL, we need to create a Stripe customer
	if !stripeCustomerID.Valid || stripeCustomerID.String == "" {
		log.Printf("INFO: Creating Stripe customer for payment method addition %s", customerID)

		// Get user profile from auth-service to create Stripe customer
		userProfile, err := s.getUserProfileFromAuthService(ctx, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to get user profile for payment method addition %s: %v", customerID, err)
			return fmt.Errorf("failed to get user profile: %w", err)
		}

		// Create Stripe customer
		stripeCustomer, err := s.stripeClient.CreateCustomer(userProfile.Email, userProfile.Name, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to create Stripe customer for payment method addition %s: %v", customerID, err)
			return fmt.Errorf("failed to create stripe customer: %w", err)
		}

		// Update customer record with Stripe customer ID
		_, err = tx.ExecContext(ctx, "UPDATE customers SET stripe_customer_id = ? WHERE id = ?", stripeCustomer.ID, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to update customer with Stripe ID for payment method addition %s: %v", customerID, err)
			return fmt.Errorf("failed to update customer with stripe ID: %w", err)
		}

		stripeCustomerID.String = stripeCustomer.ID
		stripeCustomerID.Valid = true
		log.Printf("INFO: Successfully created Stripe customer %s for payment method addition %s", stripeCustomerID.String, customerID)
	}

	// Get payment method details from Stripe
	paymentMethod, err := s.stripeClient.GetPaymentMethod(stripePaymentMethodID)
	if err != nil {
		log.Printf("ERROR: Failed to get payment method from Stripe for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to get payment method from stripe: %w", err)
	}

	// Attach payment method to customer in Stripe
	_, err = s.stripeClient.AttachPaymentMethod(stripePaymentMethodID, stripeCustomerID.String)
	if err != nil {
		log.Printf("ERROR: Failed to attach payment method to Stripe for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to attach payment method: %w", err)
	}

	// Set as default if requested
	if isDefault {
		err = s.stripeClient.SetDefaultPaymentMethod(stripeCustomerID.String, stripePaymentMethodID)
		if err != nil {
			log.Printf("ERROR: Failed to set default payment method in Stripe for customer %s: %v", customerID, err)
			return fmt.Errorf("failed to set default payment method: %w", err)
		}
	}

	// Insert payment method into database
	_, err = tx.ExecContext(ctx,
		`INSERT INTO payment_methods (customer_id, stripe_payment_method_id, stripe_customer_id, last_four, brand, exp_month, exp_year, is_default)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		customerID, stripePaymentMethodID, stripeCustomerID.String, paymentMethod.Card.Last4,
		paymentMethod.Card.Brand, paymentMethod.Card.ExpMonth, paymentMethod.Card.ExpYear, isDefault)
	if err != nil {
		log.Printf("ERROR: Failed to insert payment method into database for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to insert payment method: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: Failed to commit transaction for payment method addition for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("INFO: Successfully added payment method for customer %s", customerID)
	return nil
}

// UpdatePaymentMethod updates an existing payment method
func (s *CustomerService) UpdatePaymentMethod(ctx context.Context, customerID, paymentMethodID string, isDefault bool) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure customer exists first
	_, err = s.ensureCustomerExists(ctx, customerID)
	if err != nil {
		log.Printf("ERROR: Failed to ensure customer exists for payment method update %s: %v", customerID, err)
		return fmt.Errorf("failed to ensure customer exists: %w", err)
	}

	// Get or create customer's Stripe customer ID
	var stripeCustomerID sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT stripe_customer_id FROM customers WHERE id = ?", customerID).Scan(&stripeCustomerID)
	if err != nil {
		log.Printf("ERROR: Failed to get customer Stripe ID for payment method update %s: %v", customerID, err)
		return fmt.Errorf("failed to get customer stripe ID: %w", err)
	}

	// If stripe_customer_id is NULL, we need to create a Stripe customer
	if !stripeCustomerID.Valid || stripeCustomerID.String == "" {
		log.Printf("INFO: Creating Stripe customer for payment method update %s", customerID)

		// Get user profile from auth-service to create Stripe customer
		userProfile, err := s.getUserProfileFromAuthService(ctx, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to get user profile for payment method update %s: %v", customerID, err)
			return fmt.Errorf("failed to get user profile: %w", err)
		}

		// Create Stripe customer
		stripeCustomer, err := s.stripeClient.CreateCustomer(userProfile.Email, userProfile.Name, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to create Stripe customer for payment method update %s: %v", customerID, err)
			return fmt.Errorf("failed to create stripe customer: %w", err)
		}

		// Update customer record with Stripe customer ID
		_, err = tx.ExecContext(ctx, "UPDATE customers SET stripe_customer_id = ? WHERE id = ?", stripeCustomer.ID, customerID)
		if err != nil {
			log.Printf("ERROR: Failed to update customer with Stripe ID for payment method update %s: %v", customerID, err)
			return fmt.Errorf("failed to update customer with stripe ID: %w", err)
		}

		stripeCustomerID.String = stripeCustomer.ID
		stripeCustomerID.Valid = true
		log.Printf("INFO: Successfully created Stripe customer %s for payment method update %s", stripeCustomerID.String, customerID)
	}

	// Get payment method's Stripe ID
	var stripePaymentMethodID string
	err = s.db.QueryRowContext(ctx, "SELECT stripe_payment_method_id FROM payment_methods WHERE id = ? AND customer_id = ?",
		paymentMethodID, customerID).Scan(&stripePaymentMethodID)
	if err != nil {
		return fmt.Errorf("failed to get payment method: %w", err)
	}

	// Set as default if requested
	if isDefault {
		err = s.stripeClient.SetDefaultPaymentMethod(stripeCustomerID.String, stripePaymentMethodID)
		if err != nil {
			return fmt.Errorf("failed to set default payment method: %w", err)
		}
	}

	// Update payment method in database
	_, err = tx.ExecContext(ctx,
		"UPDATE payment_methods SET is_default = ? WHERE id = ? AND customer_id = ?",
		isDefault, paymentMethodID, customerID)
	if err != nil {
		return fmt.Errorf("failed to update payment method: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// DeletePaymentMethod deletes a payment method
func (s *CustomerService) DeletePaymentMethod(ctx context.Context, customerID, paymentMethodID string) error {
	// Get payment method's Stripe ID
	var stripePaymentMethodID string
	err := s.db.QueryRowContext(ctx, "SELECT stripe_payment_method_id FROM payment_methods WHERE id = ? AND customer_id = ?",
		paymentMethodID, customerID).Scan(&stripePaymentMethodID)
	if err != nil {
		return fmt.Errorf("failed to get payment method: %w", err)
	}

	// Detach payment method from Stripe
	_, err = s.stripeClient.DetachPaymentMethod(stripePaymentMethodID)
	if err != nil {
		return fmt.Errorf("failed to detach payment method: %w", err)
	}

	// Delete payment method from database
	_, err = s.db.ExecContext(ctx, "DELETE FROM payment_methods WHERE id = ? AND customer_id = ?", paymentMethodID, customerID)
	if err != nil {
		return fmt.Errorf("failed to delete payment method: %w", err)
	}

	return nil
}

// GetBillingHistory retrieves customer's billing history
func (s *CustomerService) GetBillingHistory(ctx context.Context, customerID string, limit, offset int) ([]models.BillingHistory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, customer_id, subscription_id, amount, currency, status, payment_date
		 FROM billing_history WHERE customer_id = ? ORDER BY payment_date DESC LIMIT ? OFFSET ?`,
		customerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query billing history: %w", err)
	}
	defer rows.Close()

	var history []models.BillingHistory
	for rows.Next() {
		var record models.BillingHistory
		err := rows.Scan(&record.ID, &record.CustomerID, &record.SubscriptionID,
			&record.Amount, &record.Currency, &record.Status, &record.PaymentDate)
		if err != nil {
			return nil, fmt.Errorf("failed to scan billing record: %w", err)
		}
		history = append(history, record)
	}

	return history, nil
}

// GetBillingRecord retrieves a specific billing record
func (s *CustomerService) GetBillingRecord(ctx context.Context, customerID, billingID string) (*models.BillingHistory, error) {
	var record models.BillingHistory
	err := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, subscription_id, amount, currency, status, payment_date
		 FROM billing_history WHERE id = ? AND customer_id = ?`,
		billingID, customerID).Scan(&record.ID, &record.CustomerID, &record.SubscriptionID,
		&record.Amount, &record.Currency, &record.Status, &record.PaymentDate)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("billing record not found")
		}
		return nil, fmt.Errorf("failed to query billing record: %w", err)
	}

	return &record, nil
}

// GenerateInvoice generates an invoice PDF (placeholder implementation)
func (s *CustomerService) GenerateInvoice(ctx context.Context, billing *models.BillingHistory) ([]byte, string, error) {
	// This is a placeholder implementation
	// In a real implementation, you would generate a PDF invoice
	invoiceData := []byte("Invoice PDF data would be generated here")
	return invoiceData, "application/pdf", nil
}

// GetAreas returns available service areas
func (s *CustomerService) GetAreas(ctx context.Context) ([]models.Area, error) {
	// This is a placeholder implementation
	// In a real implementation, you would query the areas table
	areas := []models.Area{
		{ID: 1, Name: "Downtown"},
		{ID: 2, Name: "Uptown"},
		{ID: 3, Name: "Midtown"},
	}
	return areas, nil
}

// GetCustomerBrands retrieves all brands for a customer
func (s *CustomerService) GetCustomerBrands(ctx context.Context, customerID string) ([]models.CustomerBrand, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT customer_id, brand_name, is_public FROM customer_brands WHERE customer_id = ? ORDER BY brand_name", customerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query customer brands: %w", err)
	}
	defer rows.Close()

	var brands []models.CustomerBrand
	for rows.Next() {
		var brand models.CustomerBrand
		if err := rows.Scan(&brand.CustomerID, &brand.BrandName, &brand.IsPublic); err != nil {
			return nil, fmt.Errorf("failed to scan brand: %w", err)
		}
		// Apply brand display name normalization
		brand.BrandName = utils.GetBrandDisplayName(brand.BrandName)
		brands = append(brands, brand)
	}

	return brands, nil
}

// AddCustomerBrands adds brands to a customer's brand list
func (s *CustomerService) AddCustomerBrands(ctx context.Context, customerID string, brandNames []string, isPublic bool) error {
	if len(brandNames) == 0 {
		return nil
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert new brands
	for _, brandName := range brandNames {
		normalized := utils.NormalizeBrandName(brandName)
		if err := s.insertCustomerBrand(ctx, tx, customerID, normalized, isPublic); err != nil {
			return fmt.Errorf("failed to insert customer brand: %w", err)
		}
	}

	// Update customer timestamp
	_, err = tx.ExecContext(ctx, "UPDATE customers SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", customerID)
	if err != nil {
		return fmt.Errorf("failed to update customer timestamp: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// RemoveCustomerBrands removes brands from a customer's brand list
func (s *CustomerService) RemoveCustomerBrands(ctx context.Context, customerID string, brandNames []string) error {
	if len(brandNames) == 0 {
		return nil
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Remove specified brands
	for _, brandName := range brandNames {
		normalized := utils.NormalizeBrandName(brandName)
		_, err = tx.ExecContext(ctx, "DELETE FROM customer_brands WHERE customer_id = ? AND brand_name = ?", customerID, normalized)
		if err != nil {
			return fmt.Errorf("failed to remove customer brand: %w", err)
		}
	}

	// Update customer timestamp
	_, err = tx.ExecContext(ctx, "UPDATE customers SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", customerID)
	if err != nil {
		return fmt.Errorf("failed to update customer timestamp: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateCustomerBrands replaces all brands for a customer with the new list
func (s *CustomerService) UpdateCustomerBrands(ctx context.Context, customerID string, brandNames []string, isPublic bool) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Remove existing brands
	_, err = tx.ExecContext(ctx, "DELETE FROM customer_brands WHERE customer_id = ?", customerID)
	if err != nil {
		return fmt.Errorf("failed to remove existing brands: %w", err)
	}

	// Insert new brands
	for _, brandName := range brandNames {
		normalized := utils.NormalizeBrandName(brandName)
		if err := s.insertCustomerBrand(ctx, tx, customerID, normalized, isPublic); err != nil {
			return fmt.Errorf("failed to insert customer brand: %w", err)
		}
	}

	// Update customer timestamp
	_, err = tx.ExecContext(ctx, "UPDATE customers SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", customerID)
	if err != nil {
		return fmt.Errorf("failed to update customer timestamp: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Webhook event processing methods (placeholder implementations)
func (s *CustomerService) IsWebhookEventProcessed(ctx context.Context, eventID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM webhook_events WHERE id = ?)", eventID).Scan(&exists)
	return exists, err
}

func (s *CustomerService) MarkWebhookEventProcessing(ctx context.Context, eventID, eventType string, payload []byte) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO webhook_events (id, type, raw_data, status) VALUES (?, ?, ?, 'processed')",
		eventID, eventType, payload)
	return err
}

func (s *CustomerService) MarkWebhookEventProcessed(ctx context.Context, eventID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE webhook_events SET status = 'processed' WHERE id = ?", eventID)
	return err
}

func (s *CustomerService) MarkWebhookEventSkipped(ctx context.Context, eventID string) {
	s.db.ExecContext(ctx, "UPDATE webhook_events SET status = 'skipped' WHERE id = ?", eventID)
}

func (s *CustomerService) MarkWebhookEventFailed(ctx context.Context, eventID, errorMessage string) {
	s.db.ExecContext(ctx, "UPDATE webhook_events SET status = 'failed', error_message = ? WHERE id = ?", errorMessage, eventID)
}

// Stripe webhook processing methods (placeholder implementations)
func (s *CustomerService) ProcessSuccessfulPayment(ctx context.Context, paymentIntent *stripego.PaymentIntent) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) ProcessFailedPayment(ctx context.Context, paymentIntent *stripego.PaymentIntent) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) ProcessInvoicePayment(ctx context.Context, invoice *stripego.Invoice) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) ProcessFailedInvoicePayment(ctx context.Context, invoice *stripego.Invoice) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) SyncSubscriptionFromStripe(ctx context.Context, subscription *stripego.Subscription) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) UpdateSubscriptionStatus(ctx context.Context, subscription *stripego.Subscription) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) HandleSubscriptionDeletion(ctx context.Context, subscription *stripego.Subscription) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) HandleTrialWillEnd(ctx context.Context, subscription *stripego.Subscription) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) ProcessRefund(ctx context.Context, charge *stripego.Charge) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) SyncPaymentMethodFromStripe(ctx context.Context, paymentMethod *stripego.PaymentMethod) error {
	// Placeholder implementation
	return nil
}

func (s *CustomerService) HandlePaymentMethodDetached(ctx context.Context, paymentMethod *stripego.PaymentMethod) error {
	// Placeholder implementation
	return nil
}

// Helper methods

// ensureCustomerExists ensures a customer exists in the database, creating one if needed
func (s *CustomerService) ensureCustomerExists(ctx context.Context, customerID string) (*models.Customer, error) {
	log.Printf("INFO: Ensuring customer exists: %s", customerID)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("ERROR: Failed to begin transaction for customer creation %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Check if customer was created by another request while we were waiting
	var createdAt, updatedAt time.Time
	err = tx.QueryRowContext(ctx,
		"SELECT created_at, updated_at FROM customers WHERE id = ?",
		customerID).Scan(&createdAt, &updatedAt)
	if err == nil {
		// Customer already exists, commit and return
		if err := tx.Commit(); err != nil {
			log.Printf("ERROR: Failed to commit transaction for existing customer %s: %v", customerID, err)
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
		log.Printf("INFO: Customer %s already exists", customerID)
		return &models.Customer{
			ID:        customerID,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}, nil
	}

	if err != sql.ErrNoRows {
		log.Printf("ERROR: Failed to check customer existence %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to check customer existence: %w", err)
	}

	// Customer doesn't exist, create one
	if err := s.insertCustomer(ctx, tx, customerID); err != nil {
		log.Printf("ERROR: Failed to insert customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to insert customer: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: Failed to commit transaction for customer creation %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("INFO: Successfully created customer %s", customerID)

	// Return the newly created customer
	return &models.Customer{
		ID:        customerID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// getUserProfileFromAuthService fetches user profile data from auth-service
func (s *CustomerService) getUserProfileFromAuthService(ctx context.Context, userID string) (*models.UserProfile, error) {
	log.Printf("DEBUG: Fetching user profile from auth-service for user %s", userID)

	url := fmt.Sprintf("%s/api/v3/users/%s", s.authServiceURL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("ERROR: Failed to create request to auth-service for user %s: %v", userID, err)
		return nil, fmt.Errorf("failed to create request to auth-service: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to call auth-service for user %s: %v", userID, err)
		return nil, fmt.Errorf("failed to call auth-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("ERROR: Auth-service returned status %d for user %s", resp.StatusCode, userID)
		return nil, fmt.Errorf("auth-service returned status %d", resp.StatusCode)
	}

	var profile models.UserProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		log.Printf("ERROR: Failed to decode user profile for user %s: %v", userID, err)
		return nil, fmt.Errorf("failed to decode user profile: %w", err)
	}

	log.Printf("DEBUG: Successfully fetched user profile for user %s", userID)
	return &profile, nil
}

func (s *CustomerService) insertCustomer(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO customers (id) VALUES (?)",
		id)
	return err
}

func (s *CustomerService) insertCustomerArea(ctx context.Context, tx *sql.Tx, customerID string, areaID int, isPublic bool) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO customer_areas (customer_id, area_id, is_public) VALUES (?, ?, ?)",
		customerID, areaID, isPublic)
	return err
}

// GetCustomerAreas retrieves all areas for a customer
func (s *CustomerService) GetCustomerAreas(ctx context.Context, customerID string) ([]models.CustomerArea, error) {
	log.Printf("DEBUG: Getting areas for customer %s", customerID)

	rows, err := s.db.QueryContext(ctx,
		"SELECT customer_id, area_id, is_public, created_at FROM customer_areas WHERE customer_id = ? ORDER BY created_at",
		customerID)
	if err != nil {
		log.Printf("ERROR: Failed to query customer areas for customer %s: %v", customerID, err)
		return nil, fmt.Errorf("failed to query customer areas: %w", err)
	}
	defer rows.Close()

	var areas []models.CustomerArea
	for rows.Next() {
		var area models.CustomerArea
		if err := rows.Scan(&area.CustomerID, &area.AreaID, &area.IsPublic, &area.CreatedAt); err != nil {
			log.Printf("ERROR: Failed to scan customer area for customer %s: %v", customerID, err)
			return nil, fmt.Errorf("failed to scan customer area: %w", err)
		}
		areas = append(areas, area)
	}

	log.Printf("DEBUG: Found %d areas for customer %s", len(areas), customerID)
	return areas, nil
}

// AddCustomerAreas adds areas to a customer
func (s *CustomerService) AddCustomerAreas(ctx context.Context, customerID string, areaIDs []int, isPublic bool) error {
	log.Printf("INFO: Adding %d areas to customer %s with is_public=%t", len(areaIDs), customerID, isPublic)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("ERROR: Failed to begin transaction for adding areas to customer %s: %v", customerID, err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure customer exists
	if err := s.ensureCustomerExistsInTx(ctx, tx, customerID); err != nil {
		log.Printf("ERROR: Failed to ensure customer exists %s: %v", customerID, err)
		return fmt.Errorf("failed to ensure customer exists: %w", err)
	}

	// Add each area
	for _, areaID := range areaIDs {
		if err := s.insertCustomerAreaIfNotExists(ctx, tx, customerID, areaID, isPublic); err != nil {
			log.Printf("ERROR: Failed to add area %d to customer %s: %v", areaID, customerID, err)
			return fmt.Errorf("failed to add area %d: %w", areaID, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: Failed to commit transaction for adding areas to customer %s: %v", customerID, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("INFO: Successfully added %d areas to customer %s", len(areaIDs), customerID)
	return nil
}

// UpdateCustomerAreas replaces all areas for a customer
func (s *CustomerService) UpdateCustomerAreas(ctx context.Context, customerID string, areaIDs []int, isPublic bool) error {
	log.Printf("INFO: Updating areas for customer %s with %d areas and is_public=%t", customerID, len(areaIDs), isPublic)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("ERROR: Failed to begin transaction for updating areas for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure customer exists
	if err := s.ensureCustomerExistsInTx(ctx, tx, customerID); err != nil {
		log.Printf("ERROR: Failed to ensure customer exists %s: %v", customerID, err)
		return fmt.Errorf("failed to ensure customer exists: %w", err)
	}

	// Delete all existing areas for the customer
	if err := s.deleteAllCustomerAreas(ctx, tx, customerID); err != nil {
		log.Printf("ERROR: Failed to delete existing areas for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to delete existing areas: %w", err)
	}

	// Add new areas
	for _, areaID := range areaIDs {
		if err := s.insertCustomerArea(ctx, tx, customerID, areaID, isPublic); err != nil {
			log.Printf("ERROR: Failed to add area %d to customer %s: %v", areaID, customerID, err)
			return fmt.Errorf("failed to add area %d: %w", areaID, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: Failed to commit transaction for updating areas for customer %s: %v", customerID, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("INFO: Successfully updated areas for customer %s", customerID)
	return nil
}

// DeleteCustomerAreas removes specific areas from a customer
func (s *CustomerService) DeleteCustomerAreas(ctx context.Context, customerID string, areaIDs []int) error {
	log.Printf("INFO: Deleting %d areas from customer %s", len(areaIDs), customerID)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("ERROR: Failed to begin transaction for deleting areas from customer %s: %v", customerID, err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure customer exists
	if err := s.ensureCustomerExistsInTx(ctx, tx, customerID); err != nil {
		log.Printf("ERROR: Failed to ensure customer exists %s: %v", customerID, err)
		return fmt.Errorf("failed to ensure customer exists: %w", err)
	}

	// Delete each area
	for _, areaID := range areaIDs {
		if err := s.deleteCustomerArea(ctx, tx, customerID, areaID); err != nil {
			log.Printf("ERROR: Failed to delete area %d from customer %s: %v", areaID, customerID, err)
			return fmt.Errorf("failed to delete area %d: %w", areaID, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: Failed to commit transaction for deleting areas from customer %s: %v", customerID, err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("INFO: Successfully deleted %d areas from customer %s", len(areaIDs), customerID)
	return nil
}

// Helper methods for customer areas operations

func (s *CustomerService) insertCustomerAreaIfNotExists(ctx context.Context, tx *sql.Tx, customerID string, areaID int, isPublic bool) error {
	// Check if the area already exists for this customer
	var exists int
	err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM customer_areas WHERE customer_id = ? AND area_id = ?",
		customerID, areaID).Scan(&exists)

	if err == sql.ErrNoRows {
		// Area doesn't exist, insert it
		return s.insertCustomerArea(ctx, tx, customerID, areaID, isPublic)
	} else if err != nil {
		return err
	}

	// Area already exists, skip insertion
	log.Printf("DEBUG: Area %d already exists for customer %s, skipping", areaID, customerID)
	return nil
}

func (s *CustomerService) deleteAllCustomerAreas(ctx context.Context, tx *sql.Tx, customerID string) error {
	_, err := tx.ExecContext(ctx,
		"DELETE FROM customer_areas WHERE customer_id = ?",
		customerID)
	return err
}

func (s *CustomerService) deleteCustomerArea(ctx context.Context, tx *sql.Tx, customerID string, areaID int) error {
	result, err := tx.ExecContext(ctx,
		"DELETE FROM customer_areas WHERE customer_id = ? AND area_id = ?",
		customerID, areaID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		log.Printf("DEBUG: Area %d not found for customer %s, skipping deletion", areaID, customerID)
	}

	return nil
}

func (s *CustomerService) ensureCustomerExistsInTx(ctx context.Context, tx *sql.Tx, customerID string) error {
	var exists int
	err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM customers WHERE id = ?",
		customerID).Scan(&exists)

	if err == sql.ErrNoRows {
		// Customer doesn't exist, create it
		return s.insertCustomer(ctx, tx, customerID)
	}

	return err
}

// insertCustomerBrand inserts a brand for a customer (brandName should already be normalized)
func (s *CustomerService) insertCustomerBrand(ctx context.Context, tx *sql.Tx, customerID string, brandName string, isPublic bool) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO customer_brands (customer_id, brand_name, is_public) VALUES (?, ?, ?)",
		customerID, brandName, isPublic)
	return err
}

func (s *CustomerService) insertSubscription(ctx context.Context, customerID, planType, billingCycle, stripeSubscriptionID string) (int64, error) {
	// Determine the billing interval based on billing cycle
	var interval string
	switch billingCycle {
	case "monthly":
		interval = "1 MONTH"
	case "annual", "yearly":
		interval = "1 YEAR"
	default:
		// Default to monthly if billing cycle is not recognized
		interval = "1 MONTH"
	}

	result, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO subscriptions (customer_id, plan_type, billing_cycle, stripe_subscription_id, start_date, next_billing_date)
		 VALUES (?, ?, ?, ?, CURRENT_DATE, DATE_ADD(CURRENT_DATE, INTERVAL %s))`, interval),
		customerID, planType, billingCycle, stripeSubscriptionID)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Utility functions

func generateCustomerID() string {
	return fmt.Sprintf("customer_%d", time.Now().UnixNano())
}

func isValidEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}
