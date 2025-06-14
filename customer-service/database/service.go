package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"customer-service/models"
	"customer-service/utils"
	"customer-service/utils/encryption"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// CustomerService handles all customer-related database operations
type CustomerService struct {
	db        *sql.DB
	encryptor *encryption.Encryptor
	jwtSecret []byte
}

// NewCustomerService creates a new customer service instance
func NewCustomerService(db *sql.DB, encryptor *encryption.Encryptor, jwtSecret string) *CustomerService {
	return &CustomerService{
		db:        db,
		encryptor: encryptor,
		jwtSecret: []byte(jwtSecret),
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

	// Insert subscription (Stripe subscription would be created here in production)
	subscriptionID, err := s.insertSubscriptionWithID(ctx, tx, customerID, req.PlanType, req.BillingCycle)
	if err != nil {
		return nil, err
	}

	// Attach payment method to customer if not already attached
	if err := s.attachStripePaymentMethod(ctx, tx, customerID, stripeCustomerID, req.StripePaymentMethodID); err != nil {
		return nil, err
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	// Return subscription details
	startDate := time.Now()
	var nextBillingDate time.Time
	if req.BillingCycle == models.BillingMonthly {
		nextBillingDate = startDate.AddDate(0, 1, 0)
	} else {
		nextBillingDate = startDate.AddDate(1, 0, 0)
	}

	return &models.Subscription{
		ID:              int(subscriptionID),
		CustomerID:      customerID,
		PlanType:        req.PlanType,
		BillingCycle:    req.BillingCycle,
		Status:          "active",
		StartDate:       startDate,
		NextBillingDate: nextBillingDate,
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
	currentSub, err := s.GetSubscription(ctx, customerID)
	if err != nil {
		return err
	}

	// Calculate new next billing date if billing cycle changed
	var nextBillingDate time.Time
	if req.BillingCycle != currentSub.BillingCycle {
		if req.BillingCycle == models.BillingMonthly {
			nextBillingDate = time.Now().AddDate(0, 1, 0)
		} else {
			nextBillingDate = time.Now().AddDate(1, 0, 0)
		}
	} else {
		nextBillingDate = currentSub.NextBillingDate
	}

	// Update subscription
	_, err = s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET plan_type = ?, billing_cycle = ?, next_billing_date = ?, updated_at = NOW() 
		 WHERE customer_id = ? AND status = 'active'`,
		req.PlanType, req.BillingCycle, nextBillingDate, customerID)
	
	return err
}

// CancelSubscription cancels the customer's active subscription
func (s *CustomerService) CancelSubscription(ctx context.Context, customerID string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions 
		 SET status = 'cancelled', updated_at = NOW() 
		 WHERE customer_id = ? AND status = 'active'`,
		customerID)
	
	if err != nil {
		return err
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if rowsAffected == 0 {
		return errors.New("no active subscription found")
	}
	
	return nil
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
		ORDER BY is_default DESC, created_at DESC
	`
	
	rows, err := s.db.QueryContext(ctx, query, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var methods []models.PaymentMethod
	for rows.Next() {
		var method models.PaymentMethod
		err := rows.Scan(
			&method.ID,
			&method.CustomerID,
			&method.StripePaymentMethodID,
			&method.StripeCustomerID,
			&method.LastFour,
			&method.Brand,
			&method.ExpMonth,
			&method.ExpYear,
			&method.CardholderName,
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

	// In production, you would:
	// 1. Attach the payment method to the Stripe customer
	// 2. Retrieve payment method details from Stripe
	// For now, we'll simulate with placeholder data
	paymentMethodDetails := &models.PaymentMethod{
		CustomerID:            customerID,
		StripePaymentMethodID: stripePaymentMethodID,
		StripeCustomerID:      stripeCustomerID,
		LastFour:              "4242", // Would come from Stripe
		Brand:                 "visa", // Would come from Stripe
		ExpMonth:              12,     // Would come from Stripe
		ExpYear:               2025,   // Would come from Stripe
		CardholderName:        "",     // Would come from Stripe
		IsDefault:             isDefault,
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

	// Insert new payment method
	_, err = tx.ExecContext(ctx,
		`INSERT INTO payment_methods 
		(customer_id, stripe_payment_method_id, stripe_customer_id, last_four, brand, exp_month, exp_year, cardholder_name, is_default) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		paymentMethodDetails.CustomerID,
		paymentMethodDetails.StripePaymentMethodID,
		paymentMethodDetails.StripeCustomerID,
		paymentMethodDetails.LastFour,
		paymentMethodDetails.Brand,
		paymentMethodDetails.ExpMonth,
		paymentMethodDetails.ExpYear,
		paymentMethodDetails.CardholderName,
		paymentMethodDetails.IsDefault,
	)
	if err != nil {
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
	// In production, you would also detach from Stripe
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

	if req.Provider == "" {
		// Email/password login
		customerID, err = s.authenticateWithPassword(ctx, req.Email, req.Password)
		if err != nil {
			return "", err
		}
	} else {
		// OAuth login
		customerID, err = s.authenticateWithOAuth(ctx, req.Provider, req.Token)
		if err != nil {
			return "", err
		}
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

	_, err := tx.ExecContext(ctx,
		"INSERT INTO subscriptions (customer_id, plan_type, billing_cycle, start_date, next_billing_date) VALUES (?, ?, ?, ?, ?)",
		customerID, planType, billingCycle, startDate, nextBillingDate)
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

	result, err := tx.ExecContext(ctx,
		"INSERT INTO subscriptions (customer_id, plan_type, billing_cycle, start_date, next_billing_date) VALUES (?, ?, ?, ?, ?)",
		customerID, planType, billingCycle, startDate, nextBillingDate)
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

	// In production, you would create a Stripe customer here
	// For now, we'll generate a placeholder ID
	newStripeCustomerID := fmt.Sprintf("cus_%s", utils.GenerateRandomID(14))
	
	// Update customer with Stripe customer ID
	_, err = s.db.ExecContext(ctx,
		"UPDATE customers SET stripe_customer_id = ? WHERE id = ?",
		newStripeCustomerID, customer.ID)
	if err != nil {
		return "", err
	}

	return newStripeCustomerID, nil
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

func (s *CustomerService) generateToken(ctx context.Context, customerID string) (string, error) {
	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"customer_id": customerID,
		"exp":         time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	// Store token hash for validation
	tokenHash := utils.HashToken(tokenString)
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO auth_tokens (customer_id, token_hash, expires_at) VALUES (?, ?, ?)",
		customerID, tokenHash, time.Now().Add(24*time.Hour))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func (s *CustomerService) verifyTokenInDB(customerID, tokenString string) error {
	tokenHash := utils.HashToken(tokenString)
	var exists bool

	err := s.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM auth_tokens WHERE customer_id = ? AND token_hash = ? AND expires_at > NOW())",
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
