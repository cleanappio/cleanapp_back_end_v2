package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"customer-service/models"
	"customer-service/utils"
	"customer-service/utils/encryption"
	"customer-service/utils/stripe"
	"github.com/golang-jwt/jwt/v5"
	stripelib "github.com/stripe/stripe-go/v82"
	"golang.org/x/crypto/bcrypt"
)

// CustomerService handles all customer-related database operations
type CustomerService struct {
	db           *sql.DB
	encryptor    *encryption.Encryptor
	jwtSecret    []byte
	stripeClient *stripe.Client // Add this field
}

// NewCustomerService creates a new customer service instance
func NewCustomerService(db *sql.DB, encryptor *encryption.Encryptor, jwtSecret string, stripeClient *stripe.Client) *CustomerService {
	return &CustomerService{
		db:           db,
		encryptor:    encryptor,
		jwtSecret:    []byte(jwtSecret),
		stripeClient: stripeClient,
	}
}

// CreateCustomer creates a new customer with all related data
func (s *CustomerService) CreateCustomer(ctx context.Context, req models.CreateCustomerRequest) (*models.Customer, error) {
	// Generate Ethereum-like address for ID
	id, err := utils.GenerateEthereumAddress()
	if err != nil {
		return nil, err
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Encrypt sensitive data
	emailEncrypted, err := s.encryptor.Encrypt(req.Email)
	if err != nil {
		return nil, err
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Insert customer
	if err := s.insertCustomer(ctx, tx, id, req.Name, emailEncrypted); err != nil {
		return nil, err
	}

	// Insert login method
	if err := s.insertLoginMethod(ctx, tx, id, "email", string(passwordHash)); err != nil {
		return nil, err
	}

	// Insert customer areas
	if err := s.insertCustomerAreas(ctx, tx, id, req.AreaIDs); err != nil {
		return nil, err
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &models.Customer{
		ID:        id,
		Name:      req.Name,
		Email:     req.Email,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// CreateSubscription creates a new subscription for a customer
func (s *CustomerService) CreateSubscription(ctx context.Context, customerID string, req models.CreateSubscriptionRequest) (*models.Subscription, error) {
	// Verify customer exists
	customer, err := s.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, errors.New("customer not found")
	}

	// Check if customer already has an active subscription
	var activeCount int
	err = s.db.QueryRowContext(ctx, 
		"SELECT COUNT(*) FROM subscriptions WHERE customer_id = ? AND status = 'active'", 
		customerID).Scan(&activeCount)
	if err != nil {
		return nil, err
	}
	if activeCount > 0 {
		return nil, errors.New("customer already has an active subscription")
	}

	// Get or create Stripe customer ID
	stripeCustomerID, err := s.ensureStripeCustomer(ctx, customer)
	if err != nil {
		return nil, fmt.Errorf("failed to create Stripe customer: %w", err)
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Attach payment method to customer in Stripe
	pm, err := s.stripeClient.AttachPaymentMethod(req.StripePaymentMethodID, stripeCustomerID)
	if err != nil {
		return nil, fmt.Errorf("failed to attach payment method: %w", err)
	}

	// Set as default payment method
	if err := s.stripeClient.SetDefaultPaymentMethod(stripeCustomerID, pm.ID); err != nil {
		return nil, fmt.Errorf("failed to set default payment method: %w", err)
	}

	// Create subscription in Stripe
	stripeSub, err := s.stripeClient.CreateSubscription(stripeCustomerID, req.PlanType, req.BillingCycle, req.StripePaymentMethodID)
	if err != nil {
		return nil, fmt.Errorf("failed to create Stripe subscription: %w", err)
	}

	// Insert subscription in database
	_, err = tx.ExecContext(ctx,
		`INSERT INTO subscriptions (customer_id, plan_type, billing_cycle, status, 
		 stripe_subscription_id, stripe_price_id, start_date, next_billing_date) 
		 VALUES (?, ?, ?, ?, ?, ?, FROM_UNIXTIME(?), FROM_UNIXTIME(?))`,
		customerID, req.PlanType, req.BillingCycle, stripeSub.Status,
		stripeSub.ID, stripeSub.Items.Data[0].Price.ID,
		stripeSub.Items.Data[0].CurrentPeriodStart, stripeSub.Items.Data[0].CurrentPeriodEnd)
	if err != nil {
		return nil, err
	}

	// Store payment method in database
	if err := s.storePaymentMethodFromStripe(ctx, tx, customerID, stripeCustomerID, pm); err != nil {
		return nil, err
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	// Return subscription details
	return &models.Subscription{
		ID:              0, // Will be set by auto-increment
		CustomerID:      customerID,
		PlanType:        req.PlanType,
		BillingCycle:    req.BillingCycle,
		Status:          string(stripeSub.Status),
		StartDate:       time.Unix(stripeSub.Items.Data[0].CurrentPeriodStart, 0),
		NextBillingDate: time.Unix(stripeSub.Items.Data[0].CurrentPeriodEnd, 0),
	}, nil
}

// GetSubscription retrieves the customer's active subscription
func (s *CustomerService) GetSubscription(ctx context.Context, customerID string) (*models.Subscription, error) {
	subscription := &models.Subscription{}
	
	err := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, plan_type, billing_cycle, status, start_date, next_billing_date 
		 FROM subscriptions 
		 WHERE customer_id = ? AND status = 'active' 
		 ORDER BY created_at DESC 
		 LIMIT 1`,
		customerID).Scan(
			&subscription.ID,
			&subscription.CustomerID,
			&subscription.PlanType,
			&subscription.BillingCycle,
			&subscription.Status,
			&subscription.StartDate,
			&subscription.NextBillingDate)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("no active subscription found")
		}
		return nil, err
	}
	
	return subscription, nil
}

// UpdateSubscription updates an existing subscription
func (s *CustomerService) UpdateSubscription(ctx context.Context, customerID string, req models.UpdateSubscriptionRequest) error {
	// Get current subscription
	var stripeSubID sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT stripe_subscription_id FROM subscriptions WHERE customer_id = ? AND status = 'active'",
		customerID).Scan(&stripeSubID)
	if err != nil {
		if err == sql.ErrNoRows {
			return errors.New("no active subscription found")
		}
		return err
	}

	// Update subscription in Stripe
	stripeSub, err := s.stripeClient.UpdateSubscription(stripeSubID.String, req.PlanType, req.BillingCycle)
	if err != nil {
		return fmt.Errorf("failed to update Stripe subscription: %w", err)
	}

	// Update subscription in database
	_, err = s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET plan_type = ?, billing_cycle = ?, next_billing_date = FROM_UNIXTIME(?), 
		     stripe_price_id = ?, updated_at = NOW() 
		 WHERE customer_id = ? AND status = 'active'`,
		req.PlanType, req.BillingCycle, stripeSub.Items.Data[0].CurrentPeriodEnd,
		stripeSub.Items.Data[0].Price.ID, customerID)
	
	return err
}

// CancelSubscription cancels the customer's active subscription
func (s *CustomerService) CancelSubscription(ctx context.Context, customerID string) error {
	// Get subscription
	var stripeSubID sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT stripe_subscription_id FROM subscriptions WHERE customer_id = ? AND status = 'active'",
		customerID).Scan(&stripeSubID)
	if err != nil {
		if err == sql.ErrNoRows {
			return errors.New("no active subscription found")
		}
		return err
	}

	// Cancel subscription in Stripe
	stripeSub, err := s.stripeClient.CancelSubscription(stripeSubID.String)
	if err != nil {
		return fmt.Errorf("failed to cancel Stripe subscription: %w", err)
	}

	// Update subscription in database
	log.Printf("Updating subscription status to %s for customer %s", stripeSub.Status, customerID)
	_, err = s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET status = ?, updated_at = NOW() 
		 WHERE customer_id = ? AND status = 'active'`,
		stripeSub.Status, customerID)
	
	return err
}

// UpdateCustomer updates customer information
func (s *CustomerService) UpdateCustomer(ctx context.Context, customerID string, req models.UpdateCustomerRequest) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update customer name if provided
	if req.Name != nil {
		if err := s.updateCustomerName(ctx, tx, customerID, *req.Name); err != nil {
			return err
		}
	}

	// Update email if provided
	if req.Email != nil {
		if err := s.updateCustomerEmail(ctx, tx, customerID, *req.Email); err != nil {
			return err
		}
	}

	// Update areas if provided
	if len(req.AreaIDs) > 0 {
		if err := s.updateCustomerAreas(ctx, tx, customerID, req.AreaIDs); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteCustomer deletes a customer and all related data
func (s *CustomerService) DeleteCustomer(ctx context.Context, customerID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM customers WHERE id = ?",
		customerID)
	return err
}

// GetCustomer retrieves customer information
func (s *CustomerService) GetCustomer(ctx context.Context, customerID string) (*models.Customer, error) {
	var emailEncrypted string
	customer := &models.Customer{}

	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, email_encrypted, created_at, updated_at FROM customers WHERE id = ?",
		customerID).Scan(&customer.ID, &customer.Name, &emailEncrypted, &customer.CreatedAt, &customer.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("customer not found")
		}
		return nil, err
	}

	email, err := s.encryptor.Decrypt(emailEncrypted)
	if err != nil {
		return nil, err
	}
	customer.Email = email

	return customer, nil
}

// GetPaymentMethods retrieves all payment methods for a customer
func (s *CustomerService) GetPaymentMethods(ctx context.Context, customerID string) ([]models.PaymentMethod, error) {
	query := `
		SELECT id, customer_id, stripe_payment_method_id, stripe_customer_id, 
		       last_four, brand, exp_month, exp_year, cardholder_name, is_default
		FROM payment_methods
		WHERE customer_id = ?
		ORDER BY created_at DESC
	`
	
	rows, err := s.db.QueryContext(ctx, query, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var methods []models.PaymentMethod
	for rows.Next() {
		var method models.PaymentMethod
		var cardholderName sql.NullString
		err := rows.Scan(
			&method.ID,
			&method.CustomerID,
			&method.StripePaymentMethodID,
			&method.StripeCustomerID,
			&method.LastFour,
			&method.Brand,
			&method.ExpMonth,
			&method.ExpYear,
			&cardholderName,
			&method.IsDefault,
		)
		if err != nil {
			return nil, err
		}
		methods = append(methods, method)
	}

	return methods, rows.Err()
}

// AddPaymentMethod adds a new payment method for a customer
func (s *CustomerService) AddPaymentMethod(ctx context.Context, customerID string, stripePaymentMethodID string, isDefault bool) error {
	// Get customer to ensure they exist and get Stripe customer ID
	customer, err := s.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}

	// Ensure customer has a Stripe customer ID
	stripeCustomerID, err := s.ensureStripeCustomer(ctx, customer)
	if err != nil {
		return err
	}

	// Attach payment method to Stripe customer
	pm, err := s.stripeClient.AttachPaymentMethod(stripePaymentMethodID, stripeCustomerID)
	if err != nil {
		return fmt.Errorf("failed to attach payment method: %w", err)
	}

	// Set as default if requested
	if isDefault {
		if err := s.stripeClient.SetDefaultPaymentMethod(stripeCustomerID, pm.ID); err != nil {
			return fmt.Errorf("failed to set default payment method: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// If setting as default, unset other defaults
	if isDefault {
		_, err = tx.ExecContext(ctx,
			"UPDATE payment_methods SET is_default = FALSE WHERE customer_id = ?",
			customerID)
		if err != nil {
			return err
		}
	}

	// Store payment method in database
	if err := s.storePaymentMethodFromStripe(ctx, tx, customerID, stripeCustomerID, pm); err != nil {
		return err
	}

	return tx.Commit()
}

// UpdatePaymentMethod updates a payment method (mainly for setting default)
func (s *CustomerService) UpdatePaymentMethod(ctx context.Context, customerID string, paymentMethodID int, isDefault bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Verify payment method belongs to customer
	var exists bool
	err = tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM payment_methods WHERE id = ? AND customer_id = ?)",
		paymentMethodID, customerID).Scan(&exists)
	if err != nil || !exists {
		return errors.New("payment method not found")
	}

	if isDefault {
		// Unset other defaults
		_, err = tx.ExecContext(ctx,
			"UPDATE payment_methods SET is_default = FALSE WHERE customer_id = ?",
			customerID)
		if err != nil {
			return err
		}
	}

	// Update the payment method
	_, err = tx.ExecContext(ctx,
		"UPDATE payment_methods SET is_default = ? WHERE id = ?",
		isDefault, paymentMethodID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeletePaymentMethod removes a payment method
func (s *CustomerService) DeletePaymentMethod(ctx context.Context, customerID string, paymentMethodID int) error {
	// Get the Stripe payment method ID
	var stripePaymentMethodID string
	err := s.db.QueryRowContext(ctx,
		"SELECT stripe_payment_method_id FROM payment_methods WHERE id = ? AND customer_id = ?",
		paymentMethodID, customerID).Scan(&stripePaymentMethodID)
	if err != nil {
		if err == sql.ErrNoRows {
			return errors.New("payment method not found")
		}
		return err
	}

	// Detach from Stripe
	if _, err := s.stripeClient.DetachPaymentMethod(stripePaymentMethodID); err != nil {
		return fmt.Errorf("failed to detach payment method from Stripe: %w", err)
	}

	// Delete from database
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM payment_methods WHERE id = ? AND customer_id = ?",
		paymentMethodID, customerID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return errors.New("payment method not found")
	}

	return nil
}

// GetBillingHistory retrieves billing history for a customer
func (s *CustomerService) GetBillingHistory(ctx context.Context, customerID string, limit, offset int) ([]models.BillingHistory, error) {
	query := `
		SELECT id, customer_id, subscription_id, amount, currency, status, payment_date
		FROM billing_history
		WHERE customer_id = ?
		ORDER BY payment_date DESC
		LIMIT ? OFFSET ?
	`
	
	rows, err := s.db.QueryContext(ctx, query, customerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []models.BillingHistory
	for rows.Next() {
		var record models.BillingHistory
		err := rows.Scan(
			&record.ID,
			&record.CustomerID,
			&record.SubscriptionID,
			&record.Amount,
			&record.Currency,
			&record.Status,
			&record.PaymentDate,
		)
		if err != nil {
			return nil, err
		}
		history = append(history, record)
	}

	return history, rows.Err()
}

// Login authenticates a customer and returns a JWT token
func (s *CustomerService) Login(ctx context.Context, req models.LoginRequest) (string, error) {
	var customerID string
	var err error

	// Email/password login
	customerID, err = s.authenticateWithPassword(ctx, req.Email, req.Password)
	if err != nil {
		return "", err
	}

	return s.generateToken(ctx, customerID)
}

// ValidateToken validates a JWT token and returns the customer ID
func (s *CustomerService) ValidateToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return s.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return "", errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid token claims")
	}

	// Check if it's an access token (not refresh)
	tokenType, _ := claims["type"].(string)
	if tokenType == "refresh" {
		return "", errors.New("cannot use refresh token for authentication")
	}

	customerID, ok := claims["customer_id"].(string)
	if !ok {
		return "", errors.New("invalid customer id in token")
	}

	// Verify token in database
	if err := s.verifyTokenInDB(customerID, tokenString); err != nil {
		return "", err
	}

	return customerID, nil
}

// Helper methods for database operations

func (s *CustomerService) insertCustomer(ctx context.Context, tx *sql.Tx, id, name, emailEncrypted string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO customers (id, name, email_encrypted) VALUES (?, ?, ?)",
		id, name, emailEncrypted)
	return err
}

func (s *CustomerService) insertLoginMethod(ctx context.Context, tx *sql.Tx, customerID, methodType, passwordHash string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO login_methods (customer_id, method_type, password_hash) VALUES (?, ?, ?)",
		customerID, methodType, passwordHash)
	return err
}

func (s *CustomerService) insertOAuthLoginMethod(ctx context.Context, tx *sql.Tx, customerID, methodType, oauthID string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO login_methods (customer_id, method_type, oauth_id) VALUES (?, ?, ?)",
		customerID, methodType, oauthID)
	return err
}

func (s *CustomerService) insertCustomerAreas(ctx context.Context, tx *sql.Tx, customerID string, areaIDs []int) error {
	for _, areaID := range areaIDs {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO customer_areas (customer_id, area_id) VALUES (?, ?)",
			customerID, areaID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *CustomerService) insertSubscription(ctx context.Context, tx *sql.Tx, customerID, planType, billingCycle string) error {
	startDate := time.Now()
	var nextBillingDate time.Time

	if billingCycle == models.BillingMonthly {
		nextBillingDate = startDate.AddDate(0, 1, 0)
	} else {
		nextBillingDate = startDate.AddDate(1, 0, 0)
	}

	// Use FROM_UNIXTIME for date fields to ensure timezone consistency
	_, err := tx.ExecContext(ctx,
		"INSERT INTO subscriptions (customer_id, plan_type, billing_cycle, start_date, next_billing_date) VALUES (?, ?, ?, FROM_UNIXTIME(?), FROM_UNIXTIME(?))",
		customerID, planType, billingCycle, startDate.Unix(), nextBillingDate.Unix())
	return err
}

func (s *CustomerService) insertSubscriptionWithID(ctx context.Context, tx *sql.Tx, customerID, planType, billingCycle string) (int64, error) {
	startDate := time.Now()
	var nextBillingDate time.Time

	if billingCycle == models.BillingMonthly {
		nextBillingDate = startDate.AddDate(0, 1, 0)
	} else {
		nextBillingDate = startDate.AddDate(1, 0, 0)
	}

	// Use FROM_UNIXTIME for date fields to ensure timezone consistency
	result, err := tx.ExecContext(ctx,
		"INSERT INTO subscriptions (customer_id, plan_type, billing_cycle, start_date, next_billing_date) VALUES (?, ?, ?, FROM_UNIXTIME(?), FROM_UNIXTIME(?))",
		customerID, planType, billingCycle, startDate.Unix(), nextBillingDate.Unix())
	if err != nil {
		return 0, err
	}
	
	return result.LastInsertId()
}

func (s *CustomerService) insertPaymentMethod(ctx context.Context, tx *sql.Tx, customerID string, req models.CreateCustomerRequest) error {
	// This method is no longer used since payment is now part of subscription creation
	return errors.New("deprecated: use attachStripePaymentMethod")
}

func (s *CustomerService) attachStripePaymentMethod(ctx context.Context, tx *sql.Tx, customerID, stripeCustomerID, stripePaymentMethodID string) error {
	// Check if payment method already exists
	var exists bool
	err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM payment_methods WHERE stripe_payment_method_id = ?)",
		stripePaymentMethodID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		// Payment method already attached, nothing to do
		return nil
	}

	// In production, you would:
	// 1. Call Stripe API to attach payment method to customer
	// 2. Retrieve payment method details from Stripe
	// For now, we'll use placeholder data
	_, err = tx.ExecContext(ctx,
		`INSERT INTO payment_methods 
		(customer_id, stripe_payment_method_id, stripe_customer_id, last_four, brand, exp_month, exp_year, is_default) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		customerID, stripePaymentMethodID, stripeCustomerID, "4242", "visa", 12, 2025, true)
	return err
}

func (s *CustomerService) ensureStripeCustomer(ctx context.Context, customer *models.Customer) (string, error) {
	// Check if customer already has a Stripe customer ID
	var stripeCustomerID sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT stripe_customer_id FROM customers WHERE id = ?",
		customer.ID).Scan(&stripeCustomerID)
	if err != nil {
		return "", err
	}

	if stripeCustomerID.Valid && stripeCustomerID.String != "" {
		return stripeCustomerID.String, nil
	}

	// Create customer in Stripe
	stripeCustomer, err := s.stripeClient.CreateCustomer(customer.Email, customer.Name, customer.ID)
	if err != nil {
		return "", fmt.Errorf("failed to create Stripe customer: %w", err)
	}
	
	// Update customer with Stripe customer ID
	_, err = s.db.ExecContext(ctx,
		"UPDATE customers SET stripe_customer_id = ? WHERE id = ?",
		stripeCustomer.ID, customer.ID)
	if err != nil {
		return "", err
	}

	return stripeCustomer.ID, nil
}

func (s *CustomerService) updateCustomerName(ctx context.Context, tx *sql.Tx, customerID, name string) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE customers SET name = ? WHERE id = ?",
		name, customerID)
	return err
}

func (s *CustomerService) updateCustomerEmail(ctx context.Context, tx *sql.Tx, customerID, email string) error {
	emailEncrypted, err := s.encryptor.Encrypt(email)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE customers SET email_encrypted = ? WHERE id = ?",
		emailEncrypted, customerID)
	return err
}

func (s *CustomerService) updateCustomerAreas(ctx context.Context, tx *sql.Tx, customerID string, areaIDs []int) error {
	// Delete existing areas
	_, err := tx.ExecContext(ctx,
		"DELETE FROM customer_areas WHERE customer_id = ?",
		customerID)
	if err != nil {
		return err
	}

	// Insert new areas
	return s.insertCustomerAreas(ctx, tx, customerID, areaIDs)
}

func (s *CustomerService) authenticateWithPassword(ctx context.Context, email, password string) (string, error) {
	var customerID string
	var passwordHash string

	// First, find the customer by decrypting emails
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, email_encrypted FROM customers")
	if err != nil {
		return "", errors.New("authentication failed")
	}
	defer rows.Close()

	var foundCustomerID string
	for rows.Next() {
		var id, encEmail string
		if err := rows.Scan(&id, &encEmail); err != nil {
			continue
		}
		
		decryptedEmail, err := s.encryptor.Decrypt(encEmail)
		if err != nil {
			continue
		}
		
		if decryptedEmail == email {
			foundCustomerID = id
			break
		}
	}

	if foundCustomerID == "" {
		log.Println("Customer not found for email:", email)
		return "", errors.New("invalid credentials")
	}

	// Now get the password hash for this customer
	err = s.db.QueryRowContext(ctx,
		"SELECT customer_id, password_hash FROM login_methods WHERE customer_id = ? AND method_type = 'email'",
		foundCustomerID).Scan(&customerID, &passwordHash)
	if err != nil {
		return "", errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		log.Println("Password mismatch for customer:", customerID)
		return "", errors.New("invalid credentials")
	}

	return customerID, nil
}

func (s *CustomerService) authenticateWithOAuth(ctx context.Context, provider, token string) (string, error) {
	// In production, validate token with respective OAuth provider
	// and get the OAuth ID from the provider
	// This is a simplified implementation
	var customerID string
	err := s.db.QueryRowContext(ctx,
		"SELECT customer_id FROM login_methods WHERE method_type = ? AND oauth_id = ?",
		provider, token).Scan(&customerID)
	if err != nil {
		return "", errors.New("invalid oauth token")
	}
	return customerID, nil
}

// generateToken generates a JWT token for a customer (legacy method for backward compatibility)
func (s *CustomerService) generateToken(ctx context.Context, customerID string) (string, error) {
	// Calculate expiry time once
	now := time.Now()
	expiry := now.Add(24 * time.Hour)

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"customer_id": customerID,
		"exp":         expiry.Unix(),
		"iat":         now.Unix(),
	})

	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	// Store token hash using FROM_UNIXTIME for consistent timezone handling
	tokenHash := utils.HashToken(tokenString)
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO auth_tokens (customer_id, token_hash, expires_at) VALUES (?, ?, FROM_UNIXTIME(?))",
		customerID, tokenHash, expiry.Unix())
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// verifyTokenInDB checks if a token exists and is not expired
func (s *CustomerService) verifyTokenInDB(customerID, tokenString string) error {
	tokenHash := utils.HashToken(tokenString)
	var exists bool

	// Use UTC_TIMESTAMP() for comparison to ensure timezone consistency
	err := s.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM auth_tokens WHERE customer_id = ? AND token_hash = ? AND (token_type = 'access' OR token_type IS NULL) AND expires_at > NOW())",
		customerID, tokenHash).Scan(&exists)
	if err != nil || !exists {
		return errors.New("token not found or expired")
	}
	return nil
}

// CreateOAuthCustomer creates a customer with OAuth provider
// TODO: Implement this method for OAuth registration
func (s *CustomerService) CreateOAuthCustomer(ctx context.Context, name, email, provider, oauthID string, areaIDs []int) (*models.Customer, error) {
	// This would:
	// 1. Create customer with encrypted email
	// 2. Create login_method with oauth_id
	// 3. No password needed
	return nil, errors.New("oauth registration not implemented")
}

// GenerateTokenPair generates both access and refresh tokens
func (s *CustomerService) GenerateTokenPair(ctx context.Context, customerID string) (string, string, error) {
	// Calculate expiration times once to ensure consistency
	now := time.Now()
	log.Println("Current time for token generation:", now, "In UNIX:", now.Unix())
	accessExpiry := now.Add(1 * time.Hour)
	refreshExpiry := now.Add(30 * 24 * time.Hour)

	// Generate access token (1 hour expiry)
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"customer_id": customerID,
		"type":        "access",
		"exp":         accessExpiry.Unix(),
		"iat":         now.Unix(),
	})

	accessTokenString, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	// Generate refresh token (30 days expiry)
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"customer_id": customerID,
		"type":        "refresh",
		"exp":         refreshExpiry.Unix(),
		"iat":         now.Unix(),
	})

	refreshTokenString, err := refreshToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	// Store both tokens with the same expiry times
	if err := s.storeTokens(ctx, customerID, accessTokenString, refreshTokenString, accessExpiry, refreshExpiry); err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

// ValidateRefreshToken validates a refresh token and returns the customer ID
func (s *CustomerService) ValidateRefreshToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return s.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return "", errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid token claims")
	}

	// Check token type
	tokenType, ok := claims["type"].(string)
	if !ok || tokenType != "refresh" {
		return "", errors.New("not a refresh token")
	}

	customerID, ok := claims["customer_id"].(string)
	if !ok {
		return "", errors.New("invalid customer id in token")
	}

	// Verify token in database
	if err := s.verifyRefreshTokenInDB(customerID, tokenString); err != nil {
		return "", err
	}

	return customerID, nil
}

// InvalidateToken removes a token from the database
func (s *CustomerService) InvalidateToken(ctx context.Context, customerID, tokenString string) error {
	tokenHash := utils.HashToken(tokenString)
	
	// Delete the token
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM auth_tokens WHERE customer_id = ? AND token_hash = ?",
		customerID, tokenHash)
	if err != nil {
		return err
	}
	
	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if rowsAffected == 0 {
		// Token not found, but we don't treat this as an error for logout
		return nil
	}
	
	return nil
}

// ValidateOAuthToken validates OAuth token with provider
func (s *CustomerService) ValidateOAuthToken(ctx context.Context, provider, idToken, accessToken string) (*models.OAuthUserInfo, error) {
	// TODO: Implement actual OAuth validation with providers
	// This is a placeholder implementation
	
	switch provider {
	case "google":
		// Validate with Google OAuth2 API
		return s.validateGoogleToken(idToken)
	case "facebook":
		// Validate with Facebook Graph API
		return s.validateFacebookToken(accessToken)
	case "apple":
		// Validate with Apple Sign In
		return s.validateAppleToken(idToken)
	default:
		return nil, errors.New("unsupported provider")
	}
}

// GetOrCreateOAuthCustomer gets existing customer or creates new one from OAuth
func (s *CustomerService) GetOrCreateOAuthCustomer(ctx context.Context, provider string, userInfo *models.OAuthUserInfo) (*models.Customer, bool, error) {
	// Check if customer exists with this OAuth ID
	var customerID string
	err := s.db.QueryRowContext(ctx,
		"SELECT customer_id FROM login_methods WHERE method_type = ? AND oauth_id = ?",
		provider, userInfo.ID).Scan(&customerID)
	
	if err == nil {
		// Customer exists, return it
		customer, err := s.GetCustomer(ctx, customerID)
		return customer, false, err
	}

	// Customer doesn't exist, create new one
	id, err := utils.GenerateEthereumAddress()
	if err != nil {
		return nil, false, err
	}

	// Encrypt email
	emailEncrypted, err := s.encryptor.Encrypt(userInfo.Email)
	if err != nil {
		return nil, false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	// Insert customer
	if err := s.insertCustomer(ctx, tx, id, userInfo.Name, emailEncrypted); err != nil {
		return nil, false, err
	}

	// Insert OAuth login method
	if err := s.insertOAuthLoginMethod(ctx, tx, id, provider, userInfo.ID); err != nil {
		return nil, false, err
	}

	// Add default area (ID: 1)
	if err := s.insertCustomerAreas(ctx, tx, id, []int{1}); err != nil {
		return nil, false, err
	}

	if err = tx.Commit(); err != nil {
		return nil, false, err
	}

	return &models.Customer{
		ID:        id,
		Name:      userInfo.Name,
		Email:     userInfo.Email,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, true, nil
}

// GenerateOAuthURL generates OAuth provider URL
func (s *CustomerService) GenerateOAuthURL(provider string) (string, string, error) {
	// TODO: Implement actual OAuth URL generation
	// This requires OAuth client configuration
	
	state := utils.GenerateRandomID(32)
	
	switch provider {
	case "google":
		// Generate Google OAuth URL
		return "https://accounts.google.com/o/oauth2/v2/auth?client_id=YOUR_CLIENT_ID&redirect_uri=YOUR_REDIRECT_URI&response_type=code&scope=openid%20email%20profile&state=" + state, state, nil
	case "facebook":
		// Generate Facebook OAuth URL
		return "https://www.facebook.com/v12.0/dialog/oauth?client_id=YOUR_APP_ID&redirect_uri=YOUR_REDIRECT_URI&state=" + state, state, nil
	case "apple":
		// Generate Apple OAuth URL
		return "https://appleid.apple.com/auth/authorize?client_id=YOUR_CLIENT_ID&redirect_uri=YOUR_REDIRECT_URI&response_type=code&scope=name%20email&state=" + state, state, nil
	default:
		return "", "", errors.New("unsupported provider")
	}
}

// ReactivateSubscription reactivates a canceled subscription
func (s *CustomerService) ReactivateSubscription(ctx context.Context, customerID string) (*models.Subscription, error) {
	// Check for canceled subscription
	var subscription models.Subscription
	err := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, plan_type, billing_cycle, status, start_date, next_billing_date 
		 FROM subscriptions 
		 WHERE customer_id = ? AND status = 'canceled' 
		 ORDER BY updated_at DESC 
		 LIMIT 1`,
		customerID).Scan(
			&subscription.ID,
			&subscription.CustomerID,
			&subscription.PlanType,
			&subscription.BillingCycle,
			&subscription.Status,
			&subscription.StartDate,
			&subscription.NextBillingDate)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("no canceled subscription found")
		}
		return nil, err
	}

	// Calculate new billing date
	nextBillingDate := time.Now()
	if subscription.BillingCycle == models.BillingMonthly {
		nextBillingDate = nextBillingDate.AddDate(0, 1, 0)
	} else {
		nextBillingDate = nextBillingDate.AddDate(1, 0, 0)
	}

	// Reactivate subscription using FROM_UNIXTIME for timezone consistency
	_, err = s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET status = 'active', next_billing_date = FROM_UNIXTIME(?), updated_at = UTC_TIMESTAMP() 
		 WHERE id = ?`,
		nextBillingDate.Unix(), subscription.ID)
	
	if err != nil {
		return nil, err
	}

	subscription.Status = "active"
	subscription.NextBillingDate = nextBillingDate
	
	return &subscription, nil
}

// GetBillingRecord retrieves a specific billing record
func (s *CustomerService) GetBillingRecord(ctx context.Context, customerID, billingID string) (*models.BillingHistory, error) {
	var record models.BillingHistory
	err := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, subscription_id, amount, currency, status, payment_date
		 FROM billing_history
		 WHERE id = ? AND customer_id = ?`,
		billingID, customerID).Scan(
			&record.ID,
			&record.CustomerID,
			&record.SubscriptionID,
			&record.Amount,
			&record.Currency,
			&record.Status,
			&record.PaymentDate)
	
	if err != nil {
		return nil, err
	}
	
	return &record, nil
}

// GenerateInvoice generates or retrieves an invoice PDF
func (s *CustomerService) GenerateInvoice(ctx context.Context, billing *models.BillingHistory) ([]byte, string, error) {
	// TODO: Implement actual PDF generation or Stripe invoice retrieval
	// For now, return a placeholder
	
	// In production, you would:
	// 1. Check if invoice exists in Stripe
	// 2. Download from Stripe or generate PDF
	// 3. Cache the result
	
	placeholderPDF := []byte("PDF content would go here")
	return placeholderPDF, "application/pdf", nil
}

// GetAreas retrieves all service areas
func (s *CustomerService) GetAreas(ctx context.Context) ([]models.Area, error) {
	// TODO: This should come from a proper areas table
	// For now, return mock data
	
	areas := []models.Area{
		{ID: 1, Name: "Downtown", Coordinates: map[string]float64{"lat": 40.7128, "lng": -74.0060}},
		{ID: 2, Name: "Midtown", Coordinates: map[string]float64{"lat": 40.7549, "lng": -73.9840}},
		{ID: 3, Name: "Uptown", Coordinates: map[string]float64{"lat": 40.7812, "lng": -73.9665}},
		{ID: 4, Name: "Brooklyn", Coordinates: map[string]float64{"lat": 40.6782, "lng": -73.9442}},
		{ID: 5, Name: "Queens", Coordinates: map[string]float64{"lat": 40.7282, "lng": -73.7949}},
	}
	
	return areas, nil
}

// Helper methods for OAuth validation (placeholders)

func (s *CustomerService) validateGoogleToken(idToken string) (*models.OAuthUserInfo, error) {
	// TODO: Implement Google token validation
	// Use Google's OAuth2 API to validate the ID token
	return nil, errors.New("google oauth not implemented")
}

func (s *CustomerService) validateFacebookToken(accessToken string) (*models.OAuthUserInfo, error) {
	// TODO: Implement Facebook token validation
	// Use Facebook Graph API to validate the access token
	return nil, errors.New("facebook oauth not implemented")
}

func (s *CustomerService) validateAppleToken(idToken string) (*models.OAuthUserInfo, error) {
	// TODO: Implement Apple token validation
	// Validate Apple's JWT token
	return nil, errors.New("apple oauth not implemented")
}

// storeTokens stores access and refresh tokens with their expiration times using Unix timestamps
func (s *CustomerService) storeTokens(ctx context.Context, customerID, accessToken, refreshToken string, accessExpiry, refreshExpiry time.Time) error {
	accessHash := utils.HashToken(accessToken)
	refreshHash := utils.HashToken(refreshToken)
	
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Store access token using FROM_UNIXTIME for consistent timezone handling
	_, err = tx.ExecContext(ctx,
		"INSERT INTO auth_tokens (customer_id, token_hash, token_type, expires_at) VALUES (?, ?, 'access', FROM_UNIXTIME(?))",
		customerID, accessHash, accessExpiry.Unix())
	if err != nil {
		return err
	}

	// Store refresh token using FROM_UNIXTIME for consistent timezone handling
	_, err = tx.ExecContext(ctx,
		"INSERT INTO auth_tokens (customer_id, token_hash, token_type, expires_at) VALUES (?, ?, 'refresh', FROM_UNIXTIME(?))",
		customerID, refreshHash, refreshExpiry.Unix())
	if err != nil {
		return err
	}

	return tx.Commit()
}

// verifyRefreshTokenInDB checks if a refresh token exists and is not expired
func (s *CustomerService) verifyRefreshTokenInDB(customerID, tokenString string) error {
	tokenHash := utils.HashToken(tokenString)
	var exists bool

	// Use UTC_TIMESTAMP() for comparison to ensure timezone consistency
	err := s.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM auth_tokens WHERE customer_id = ? AND token_hash = ? AND token_type = 'refresh' AND FROM_UNIXTIME(expires_at) > NOW())",
		customerID, tokenHash).Scan(&exists)
	if err != nil || !exists {
		return errors.New("refresh token not found or expired")
	}
	return nil
}

// AuthenticateCustomer authenticates a customer and returns their ID
func (s *CustomerService) AuthenticateCustomer(ctx context.Context, req models.LoginRequest) (string, error) {
	// OAuth login
	if req.Provider != "" {
		return s.authenticateWithOAuth(ctx, req.Provider, req.Token)
	}
	
	// Email/password login
	return s.authenticateWithPassword(ctx, req.Email, req.Password)
}

// ProcessSuccessfulPayment processes a successful payment from Stripe
func (s *CustomerService) ProcessSuccessfulPayment(ctx context.Context, paymentIntent *stripelib.PaymentIntent) error {
	// Extract customer ID from metadata or invoice
	customerID := paymentIntent.Metadata["customer_id"]
	if customerID == "" {
		// Get invoice and extract customer
		// This would require expanding the invoice in the webhook
		return nil // For now, skip if no customer ID
	}

	// Record payment in billing history
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO billing_history 
		(customer_id, subscription_id, stripe_payment_intent_id, amount, currency, status, payment_date) 
		SELECT c.id, s.id, ?, ?, ?, 'completed', NOW()
		FROM customers c
		JOIN subscriptions s ON s.customer_id = c.id
		WHERE c.id = ? AND s.status = 'active'
		LIMIT 1`,
		paymentIntent.ID, float64(paymentIntent.Amount)/100, paymentIntent.Currency, customerID)
	
	return err
}

// ProcessInvoicePayment processes an invoice payment from Stripe
func (s *CustomerService) ProcessInvoicePayment(ctx context.Context, invoice *stripelib.Invoice) error {
	var subscr *stripelib.Subscription
	if invoice.Parent != nil && invoice.Parent.Type == stripelib.InvoiceParentTypeSubscriptionDetails {
		subscr = invoice.Parent.SubscriptionDetails.Subscription
	}

	if subscr == nil {
		return nil // Not a subscription invoice
	}

	// Retrieve full subscription details
	subscr, err := s.stripeClient.GetSubscription(subscr.ID)
	if err != nil {
		return fmt.Errorf("failed to retrieve subscription: %w", err)
	}

	// Get customer ID from Stripe customer ID
	var customerID string
	err = s.db.QueryRowContext(ctx,
		"SELECT id FROM customers WHERE stripe_customer_id = ?",
		invoice.Customer.ID).Scan(&customerID)
	if err != nil {
		return err
	}

	// Get subscription ID
	var subscriptionID int
	err = s.db.QueryRowContext(ctx,
		"SELECT id FROM subscriptions WHERE stripe_subscription_id = ?",
		subscr.ID).Scan(&subscriptionID)
	if err != nil {
		return err
	}

	// Record payment in billing history
	var pID string
	if len(invoice.Payments.Data) > 0 {
		pID = invoice.Payments.Data[0].ID
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO billing_history 
		(customer_id, subscription_id, stripe_payment_intent_id, stripe_invoice_id, 
		 amount, currency, status, payment_date) 
		VALUES (?, ?, ?, ?, ?, ?, 'completed', FROM_UNIXTIME(?))`,
		customerID, subscriptionID, pID, invoice.ID,
		float64(invoice.AmountPaid)/100, invoice.Currency, invoice.StatusTransitions.PaidAt)
	
	// Update next billing date
	if err == nil {
		_, err = s.db.ExecContext(ctx,
			"UPDATE subscriptions SET next_billing_date = FROM_UNIXTIME(?) WHERE id = ?",
			subscr.Items.Data[0].CurrentPeriodEnd, subscriptionID)
	}
	
	return err
}

// UpdateSubscriptionStatus updates subscription status from Stripe webhook
func (s *CustomerService) UpdateSubscriptionStatus(ctx context.Context, subscription *stripelib.Subscription) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET status = ?, next_billing_date = FROM_UNIXTIME(?), updated_at = NOW() 
		 WHERE stripe_subscription_id = ?`,
		subscription.Status, subscription.Items.Data[0].CurrentPeriodEnd, subscription.ID)
	return err
}

// HandleSubscriptionDeletion handles subscription deletion from Stripe
func (s *CustomerService) HandleSubscriptionDeletion(ctx context.Context, subscription *stripelib.Subscription) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET status = 'canceled', updated_at = NOW() 
		 WHERE stripe_subscription_id = ?`,
		subscription.ID)
	return err
}

func (s *CustomerService) storePaymentMethodFromStripe(ctx context.Context, tx *sql.Tx, customerID, stripeCustomerID string, pm *stripelib.PaymentMethod) error {
	// Check if payment method already exists
	var exists bool
	err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM payment_methods WHERE stripe_payment_method_id = ?)",
		pm.ID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Extract card details
	var lastFour, brand string
	var expMonth, expYear int64
	var cardholderName string
	
	if pm.Card != nil {
		lastFour = pm.Card.Last4
		brand = string(pm.Card.Brand)
		expMonth = pm.Card.ExpMonth
		expYear = pm.Card.ExpYear
		if pm.BillingDetails != nil && pm.BillingDetails.Name != "" {
			cardholderName = pm.BillingDetails.Name
		}
	}

	// Insert payment method
	_, err = tx.ExecContext(ctx,
		`INSERT INTO payment_methods 
		(customer_id, stripe_payment_method_id, stripe_customer_id, last_four, brand, 
		 exp_month, exp_year, cardholder_name, is_default) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		customerID, pm.ID, stripeCustomerID, lastFour, brand, 
		expMonth, expYear, cardholderName, true)
	return err
}

// IsWebhookEventProcessed checks if a webhook event has already been processed
func (s *CustomerService) IsWebhookEventProcessed(ctx context.Context, eventID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM webhook_events WHERE id = ?)",
		eventID).Scan(&exists)
	return exists, err
}

// MarkWebhookEventProcessing marks a webhook event as being processed
func (s *CustomerService) MarkWebhookEventProcessing(ctx context.Context, eventID, eventType string, rawData []byte) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO webhook_events (id, type, raw_data, status) VALUES (?, ?, ?, 'processed') ON DUPLICATE KEY UPDATE id = id",
		eventID, eventType, rawData)
	return err
}

// MarkWebhookEventProcessed marks a webhook event as successfully processed
func (s *CustomerService) MarkWebhookEventProcessed(ctx context.Context, eventID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE webhook_events SET status = 'processed', processed_at = NOW() WHERE id = ?",
		eventID)
	return err
}

// MarkWebhookEventFailed marks a webhook event as failed
func (s *CustomerService) MarkWebhookEventFailed(ctx context.Context, eventID, errorMessage string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE webhook_events SET status = 'failed', error_message = ?, processed_at = NOW() WHERE id = ?",
		errorMessage, eventID)
	return err
}

// MarkWebhookEventSkipped marks a webhook event as skipped
func (s *CustomerService) MarkWebhookEventSkipped(ctx context.Context, eventID string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO webhook_events (id, type, status) VALUES (?, 'unknown', 'skipped') ON DUPLICATE KEY UPDATE status = 'skipped'",
		eventID)
	return err
}

// SyncSubscriptionFromStripe syncs subscription data from Stripe
func (s *CustomerService) SyncSubscriptionFromStripe(ctx context.Context, stripeSub *stripelib.Subscription) error {
	// Get customer ID from Stripe customer
	var customerID string
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM customers WHERE stripe_customer_id = ?",
		stripeSub.Customer.ID).Scan(&customerID)
	if err != nil {
		return fmt.Errorf("customer not found for stripe_customer_id: %s", stripeSub.Customer.ID)
	}

	// Extract plan details from metadata or items
	planType := stripeSub.Metadata["plan_type"]
	billingCycle := stripeSub.Metadata["billing_cycle"]
	
	// Update or insert subscription
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO subscriptions 
		(customer_id, stripe_subscription_id, stripe_price_id, plan_type, billing_cycle, 
		 status, start_date, next_billing_date, trial_end, cancel_at_period_end) 
		VALUES (?, ?, ?, ?, ?, ?, FROM_UNIXTIME(?), FROM_UNIXTIME(?), ?, ?)
		ON DUPLICATE KEY UPDATE 
		status = VALUES(status),
		next_billing_date = VALUES(next_billing_date),
		trial_end = VALUES(trial_end),
		cancel_at_period_end = VALUES(cancel_at_period_end),
		updated_at = NOW()`,
		customerID, stripeSub.ID, stripeSub.Items.Data[0].Price.ID, planType, billingCycle,
		stripeSub.Status, stripeSub.Items.Data[0].CurrentPeriodStart, stripeSub.Items.Data[0].CurrentPeriodEnd,
		nilableTimestamp(stripeSub.TrialEnd), stripeSub.CancelAtPeriodEnd)
	
	// Log sync operation
	s.logStripeSync(ctx, "subscription", customerID, stripeSub.ID, "sync", err)
	
	return err
}

// ProcessFailedPayment processes a failed payment from Stripe
func (s *CustomerService) ProcessFailedPayment(ctx context.Context, paymentIntent *stripelib.PaymentIntent) error {
	// Extract customer ID from metadata or get from Stripe customer
	customerID := paymentIntent.Metadata["customer_id"]
	if customerID == "" && paymentIntent.Customer != nil {
		// Get customer ID from our database using Stripe customer ID
		err := s.db.QueryRowContext(ctx,
			"SELECT id FROM customers WHERE stripe_customer_id = ?",
			paymentIntent.Customer.ID).Scan(&customerID)
		if err != nil {
			log.Printf("Failed to find customer for stripe_customer_id: %s", paymentIntent.Customer.ID)
			return nil // Don't fail the webhook
		}
	}

	// Get failure reason
	failureReason := ""
	if paymentIntent.LastPaymentError != nil {
		failureReason = paymentIntent.LastPaymentError.Msg
	}

	// Record failed payment in billing history
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO billing_history 
		(customer_id, subscription_id, stripe_payment_intent_id, amount, currency, status, failure_reason, payment_date) 
		SELECT c.id, s.id, ?, ?, ?, 'failed', ?, NOW()
		FROM customers c
		LEFT JOIN subscriptions s ON s.customer_id = c.id AND s.status = 'active'
		WHERE c.id = ?
		LIMIT 1`,
		paymentIntent.ID, float64(paymentIntent.Amount)/100, paymentIntent.Currency, failureReason, customerID)
	
	return err
}

// ProcessFailedInvoicePayment processes a failed invoice payment
func (s *CustomerService) ProcessFailedInvoicePayment(ctx context.Context, invoice *stripelib.Invoice) error {
	var subscr *stripelib.Subscription
	if invoice.Parent != nil && invoice.Parent.Type == stripelib.InvoiceParentTypeSubscriptionDetails {
		subscr = invoice.Parent.SubscriptionDetails.Subscription
	}

	if subscr == nil {
		return nil // Not a subscription invoice
	}

	// Get customer ID from Stripe customer ID
	var customerID string
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM customers WHERE stripe_customer_id = ?",
		invoice.Customer.ID).Scan(&customerID)
	if err != nil {
		return err
	}

	// Update subscription status if needed
	_, err = s.db.ExecContext(ctx,
		`UPDATE subscriptions 
			SET status = 'past_due', updated_at = NOW() 
			WHERE stripe_subscription_id = ?`,
		subscr)
	if err != nil {
		log.Printf("Failed to update subscription status: %v", err)
	}

	// Record failed payment
	failureReason := "Invoice payment failed"
	if invoice.LastFinalizationError != nil {
		failureReason = invoice.LastFinalizationError.Msg
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO billing_history 
		(customer_id, subscription_id, stripe_invoice_id, amount, currency, status, failure_reason, payment_date) 
		SELECT ?, s.id, ?, ?, ?, 'failed', ?, NOW()
		FROM subscriptions s
		WHERE s.customer_id = ? AND s.stripe_subscription_id = ?
		LIMIT 1`,
		customerID, invoice.ID, float64(invoice.AmountDue)/100, invoice.Currency, 
		failureReason, customerID, subscr.ID)
	
	return err
}

// HandleTrialWillEnd handles trial ending notifications
func (s *CustomerService) HandleTrialWillEnd(ctx context.Context, subscription *stripelib.Subscription) error {
	// Update trial_end in our database
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET trial_end = FROM_UNIXTIME(?), updated_at = NOW() 
		 WHERE stripe_subscription_id = ?`,
		subscription.TrialEnd, subscription.ID)
	
	// TODO: Send email notification to customer about trial ending
	
	return err
}

// SyncPaymentMethodFromStripe syncs a payment method from Stripe
func (s *CustomerService) SyncPaymentMethodFromStripe(ctx context.Context, pm *stripelib.PaymentMethod) error {
	// Get customer ID from Stripe customer ID
	var customerID string
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM customers WHERE stripe_customer_id = ?",
		pm.Customer.ID).Scan(&customerID)
	if err != nil {
		return fmt.Errorf("customer not found for stripe_customer_id: %s", pm.Customer.ID)
	}

	// Extract card details
	var lastFour, brand string
	var expMonth, expYear int64
	var cardholderName string
	
	if pm.Card != nil {
		lastFour = pm.Card.Last4
		brand = string(pm.Card.Brand)
		expMonth = pm.Card.ExpMonth
		expYear = pm.Card.ExpYear
		if pm.BillingDetails != nil && pm.BillingDetails.Name != "" {
			cardholderName = pm.BillingDetails.Name
		}
	}

	// Insert or update payment method
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO payment_methods 
		(customer_id, stripe_payment_method_id, stripe_customer_id, last_four, brand, 
		 exp_month, exp_year, cardholder_name, is_default) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, FALSE)
		ON DUPLICATE KEY UPDATE 
		last_four = VALUES(last_four),
		brand = VALUES(brand),
		exp_month = VALUES(exp_month),
		exp_year = VALUES(exp_year),
		cardholder_name = VALUES(cardholder_name)`,
		customerID, pm.ID, pm.Customer.ID, lastFour, brand, 
		expMonth, expYear, cardholderName)
	
	// Log sync operation
	s.logStripeSync(ctx, "payment_method", customerID, pm.ID, "sync", err)
	
	return err
}

// HandlePaymentMethodDetached handles payment method detachment
func (s *CustomerService) HandlePaymentMethodDetached(ctx context.Context, pm *stripelib.PaymentMethod) error {
	// Delete the payment method from our database
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM payment_methods WHERE stripe_payment_method_id = ?",
		pm.ID)
	
	if err != nil {
		return err
	}
	
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Deleted payment method %s from database", pm.ID)
	}
	
	return nil
}

// ProcessRefund processes a refund from Stripe
func (s *CustomerService) ProcessRefund(ctx context.Context, charge *stripelib.Charge) error {
	// Calculate total refunded amount
	var refundAmount int64
	for _, refund := range charge.Refunds.Data {
		refundAmount += refund.Amount
	}
	
	// Determine refund status
	status := "partially_refunded"
	if refundAmount >= charge.Amount {
		status = "refunded"
	}
	
	// Update billing history with refund information
	result, err := s.db.ExecContext(ctx,
		`UPDATE billing_history 
		 SET refund_amount = ?, status = ?
		 WHERE stripe_charge_id = ? OR stripe_payment_intent_id = ?`,
		float64(refundAmount)/100, status, charge.ID, charge.PaymentIntent.ID)
	
	if err != nil {
		return err
	}
	
	rowsAffected, _ := result.RowsAffected()
	log.Printf("Updated %d billing records with refund amount %.2f", rowsAffected, float64(refundAmount)/100)
	
	return nil
}

// Helper functions to add to database/service.go

// logStripeSync logs Stripe sync operations for debugging
func (s *CustomerService) logStripeSync(ctx context.Context, entityType, entityID, stripeID, action string, err error) {
	status := "success"
	var errorMsg *string
	if err != nil {
		status = "failed"
		errStr := err.Error()
		errorMsg = &errStr
	}
	
	_, logErr := s.db.ExecContext(ctx,
		`INSERT INTO stripe_sync_log 
		(entity_type, entity_id, stripe_id, action, status, error_message) 
		VALUES (?, ?, ?, ?, ?, ?)`,
		entityType, entityID, stripeID, action, status, errorMsg)
	
	if logErr != nil {
		log.Printf("Failed to log stripe sync operation: %v", logErr)
	}
}

// nilableTimestamp converts unix timestamp to *time.Time
func nilableTimestamp(unixTime int64) *time.Time {
	if unixTime == 0 {
		return nil
	}
	t := time.Unix(unixTime, 0)
	return &t
}

// formatNullableTime formats a nullable timestamp for MySQL
func formatNullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Unix()
}