package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"customer-service/models"
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
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert customer (no name/email - managed by auth-service)
	if err := s.insertCustomer(ctx, tx, customerID); err != nil {
		return nil, fmt.Errorf("failed to insert customer: %w", err)
	}

	// Insert customer areas
	for _, areaID := range req.AreaIDs {
		if err := s.insertCustomerArea(ctx, tx, customerID, areaID); err != nil {
			return nil, fmt.Errorf("failed to insert customer area: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Create customer object
	customer := &models.Customer{
		ID:        customerID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return customer, nil
}

// GetCustomer retrieves a customer by ID
func (s *CustomerService) GetCustomer(ctx context.Context, customerID string) (*models.Customer, error) {
	var createdAt, updatedAt time.Time

	err := s.db.QueryRowContext(ctx,
		"SELECT created_at, updated_at FROM customers WHERE id = ?",
		customerID).Scan(&createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("customer not found")
		}
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
		if err := s.insertCustomerArea(ctx, tx, customerID, areaID); err != nil {
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
	// Check if customer already has an active subscription
	existing, err := s.GetSubscription(ctx, customerID)
	if err == nil && existing != nil {
		return nil, errors.New("customer already has an active subscription")
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get or create customer's Stripe customer ID
	var stripeCustomerID string
	err = tx.QueryRowContext(ctx, "SELECT stripe_customer_id FROM customers WHERE id = ?", customerID).Scan(&stripeCustomerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("customer not found: %w", err)
		}
		// If stripe_customer_id is NULL, we need to create a Stripe customer
		if stripeCustomerID == "" {
			// Get user profile from auth-service to create Stripe customer
			userProfile, err := s.getUserProfileFromAuthService(ctx, customerID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user profile: %w", err)
			}

			// Create Stripe customer
			stripeCustomer, err := s.stripeClient.CreateCustomer(userProfile.Email, userProfile.Name, customerID)
			if err != nil {
				return nil, fmt.Errorf("failed to create stripe customer: %w", err)
			}

			// Update customer record with Stripe customer ID
			_, err = tx.ExecContext(ctx, "UPDATE customers SET stripe_customer_id = ? WHERE id = ?", stripeCustomer.ID, customerID)
			if err != nil {
				return nil, fmt.Errorf("failed to update customer with stripe ID: %w", err)
			}

			stripeCustomerID = stripeCustomer.ID
		} else {
			return nil, fmt.Errorf("failed to get customer stripe ID: %w", err)
		}
	}

	// Create subscription in Stripe
	stripeSubscription, err := s.stripeClient.CreateSubscription(stripeCustomerID, req.PlanType, req.BillingCycle, req.StripePaymentMethodID)
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

	return &models.Subscription{
		ID:              int(subscriptionID),
		CustomerID:      customerID,
		PlanType:        req.PlanType,
		BillingCycle:    req.BillingCycle,
		Status:          "active",
		StartDate:       time.Now(),
		NextBillingDate: time.Now().AddDate(0, 1, 0), // Default to 1 month
	}, nil
}

// GetSubscription retrieves the customer's current subscription
func (s *CustomerService) GetSubscription(ctx context.Context, customerID string) (*models.Subscription, error) {
	var subscription models.Subscription
	err := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, plan_type, billing_cycle, status, start_date, next_billing_date 
		 FROM subscriptions WHERE customer_id = ? AND status = 'active'`,
		customerID).Scan(&subscription.ID, &subscription.CustomerID, &subscription.PlanType,
		&subscription.BillingCycle, &subscription.Status, &subscription.StartDate, &subscription.NextBillingDate)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("no active subscription found")
		}
		return nil, fmt.Errorf("failed to query subscription: %w", err)
	}

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
	var stripeSubscriptionID string
	err = s.db.QueryRowContext(ctx, "SELECT stripe_subscription_id FROM subscriptions WHERE id = ?", subscription.ID).Scan(&stripeSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to get subscription stripe ID: %w", err)
	}

	// Update subscription in Stripe
	_, err = s.stripeClient.UpdateSubscription(stripeSubscriptionID, req.PlanType, req.BillingCycle)
	if err != nil {
		return fmt.Errorf("failed to update stripe subscription: %w", err)
	}

	// Update subscription in database
	_, err = s.db.ExecContext(ctx,
		"UPDATE subscriptions SET plan_type = ?, billing_cycle = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
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
	var stripeSubscriptionID string
	err = s.db.QueryRowContext(ctx, "SELECT stripe_subscription_id FROM subscriptions WHERE id = ?", subscription.ID).Scan(&stripeSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to get subscription stripe ID: %w", err)
	}

	// Cancel subscription in Stripe
	_, err = s.stripeClient.CancelSubscription(stripeSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to cancel stripe subscription: %w", err)
	}

	// Update subscription status in database
	_, err = s.db.ExecContext(ctx,
		"UPDATE subscriptions SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
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
		 FROM subscriptions WHERE customer_id = ? AND status = 'cancelled'`,
		customerID).Scan(&subscription.ID, &subscription.CustomerID, &subscription.PlanType,
		&subscription.BillingCycle, &subscription.Status, &subscription.StartDate, &subscription.NextBillingDate)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("no canceled subscription found")
		}
		return nil, fmt.Errorf("failed to query subscription: %w", err)
	}

	// Get subscription's Stripe ID
	var stripeSubscriptionID string
	err = s.db.QueryRowContext(ctx, "SELECT stripe_subscription_id FROM subscriptions WHERE id = ?", subscription.ID).Scan(&stripeSubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription stripe ID: %w", err)
	}

	// Reactivate subscription in Stripe
	_, err = s.stripeClient.ReactivateSubscription(stripeSubscriptionID)
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
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get or create customer's Stripe customer ID
	var stripeCustomerID string
	err = tx.QueryRowContext(ctx, "SELECT stripe_customer_id FROM customers WHERE id = ?", customerID).Scan(&stripeCustomerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("customer not found: %w", err)
		}
		// If stripe_customer_id is NULL, we need to create a Stripe customer
		if stripeCustomerID == "" {
			// Get user profile from auth-service to create Stripe customer
			userProfile, err := s.getUserProfileFromAuthService(ctx, customerID)
			if err != nil {
				return fmt.Errorf("failed to get user profile: %w", err)
			}

			// Create Stripe customer
			stripeCustomer, err := s.stripeClient.CreateCustomer(userProfile.Email, userProfile.Name, customerID)
			if err != nil {
				return fmt.Errorf("failed to create stripe customer: %w", err)
			}

			// Update customer record with Stripe customer ID
			_, err = tx.ExecContext(ctx, "UPDATE customers SET stripe_customer_id = ? WHERE id = ?", stripeCustomer.ID, customerID)
			if err != nil {
				return fmt.Errorf("failed to update customer with stripe ID: %w", err)
			}

			stripeCustomerID = stripeCustomer.ID
		} else {
			return fmt.Errorf("failed to get customer stripe ID: %w", err)
		}
	}

	// Get payment method details from Stripe
	paymentMethod, err := s.stripeClient.GetPaymentMethod(stripePaymentMethodID)
	if err != nil {
		return fmt.Errorf("failed to get payment method from stripe: %w", err)
	}

	// Attach payment method to customer in Stripe
	_, err = s.stripeClient.AttachPaymentMethod(stripePaymentMethodID, stripeCustomerID)
	if err != nil {
		return fmt.Errorf("failed to attach payment method: %w", err)
	}

	// Set as default if requested
	if isDefault {
		err = s.stripeClient.SetDefaultPaymentMethod(stripeCustomerID, stripePaymentMethodID)
		if err != nil {
			return fmt.Errorf("failed to set default payment method: %w", err)
		}
	}

	// Insert payment method into database
	_, err = tx.ExecContext(ctx,
		`INSERT INTO payment_methods (customer_id, stripe_payment_method_id, stripe_customer_id, last_four, brand, exp_month, exp_year, is_default)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		customerID, stripePaymentMethodID, stripeCustomerID, paymentMethod.Card.Last4,
		paymentMethod.Card.Brand, paymentMethod.Card.ExpMonth, paymentMethod.Card.ExpYear, isDefault)
	if err != nil {
		return fmt.Errorf("failed to insert payment method: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

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

	// Get or create customer's Stripe customer ID
	var stripeCustomerID string
	err = tx.QueryRowContext(ctx, "SELECT stripe_customer_id FROM customers WHERE id = ?", customerID).Scan(&stripeCustomerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("customer not found: %w", err)
		}
		// If stripe_customer_id is NULL, we need to create a Stripe customer
		if stripeCustomerID == "" {
			// Get user profile from auth-service to create Stripe customer
			userProfile, err := s.getUserProfileFromAuthService(ctx, customerID)
			if err != nil {
				return fmt.Errorf("failed to get user profile: %w", err)
			}

			// Create Stripe customer
			stripeCustomer, err := s.stripeClient.CreateCustomer(userProfile.Email, userProfile.Name, customerID)
			if err != nil {
				return fmt.Errorf("failed to create stripe customer: %w", err)
			}

			// Update customer record with Stripe customer ID
			_, err = tx.ExecContext(ctx, "UPDATE customers SET stripe_customer_id = ? WHERE id = ?", stripeCustomer.ID, customerID)
			if err != nil {
				return fmt.Errorf("failed to update customer with stripe ID: %w", err)
			}

			stripeCustomerID = stripeCustomer.ID
		} else {
			return fmt.Errorf("failed to get customer stripe ID: %w", err)
		}
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
		err = s.stripeClient.SetDefaultPaymentMethod(stripeCustomerID, stripePaymentMethodID)
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

// getUserProfileFromAuthService fetches user profile data from auth-service
func (s *CustomerService) getUserProfileFromAuthService(ctx context.Context, userID string) (*models.UserProfile, error) {
	url := fmt.Sprintf("%s/api/v3/users/%s", s.authServiceURL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request to auth-service: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call auth-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth-service returned status %d", resp.StatusCode)
	}

	var profile models.UserProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("failed to decode user profile: %w", err)
	}

	return &profile, nil
}

func (s *CustomerService) insertCustomer(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO customers (id) VALUES (?)",
		id)
	return err
}

func (s *CustomerService) insertCustomerArea(ctx context.Context, tx *sql.Tx, customerID string, areaID int) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO customer_areas (customer_id, area_id) VALUES (?, ?)",
		customerID, areaID)
	return err
}

func (s *CustomerService) insertSubscription(ctx context.Context, customerID, planType, billingCycle, stripeSubscriptionID string) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO subscriptions (customer_id, plan_type, billing_cycle, stripe_subscription_id, start_date, next_billing_date)
		 VALUES (?, ?, ?, ?, CURRENT_DATE, DATE_ADD(CURRENT_DATE, INTERVAL 1 MONTH))`,
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
