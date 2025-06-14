package database

import (
	"context"
	"database/sql"
	"errors"
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
	if err := s.insertLoginMethod(ctx, tx, id, "email", req.Email, string(passwordHash)); err != nil {
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
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM customers WHERE id = ?)", customerID).Scan(&exists)
	if err != nil || !exists {
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

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Insert subscription
	subscriptionID, err := s.insertSubscriptionWithID(ctx, tx, customerID, req.PlanType, req.BillingCycle)
	if err != nil {
		return nil, err
	}

	// Insert payment method
	if err := s.insertPaymentMethodFromRequest(ctx, tx, customerID, req); err != nil {
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

func (s *CustomerService) insertLoginMethod(ctx context.Context, tx *sql.Tx, customerID, methodType, methodID, passwordHash string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO login_methods (customer_id, method_type, method_id, password_hash) VALUES (?, ?, ?, ?)",
		customerID, methodType, methodID, passwordHash)
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
	return errors.New("deprecated: use insertPaymentMethodFromRequest")
}

func (s *CustomerService) insertPaymentMethodFromRequest(ctx context.Context, tx *sql.Tx, customerID string, req models.CreateSubscriptionRequest) error {
	// Encrypt payment data
	cardNumberEncrypted, err := s.encryptor.Encrypt(req.CardNumber)
	if err != nil {
		return err
	}

	cardHolderEncrypted, err := s.encryptor.Encrypt(req.CardHolder)
	if err != nil {
		return err
	}

	expiryEncrypted, err := s.encryptor.Encrypt(req.Expiry)
	if err != nil {
		return err
	}

	cvvEncrypted, err := s.encryptor.Encrypt(req.CVV)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO payment_methods (customer_id, card_number_encrypted, card_holder_encrypted, expiry_encrypted, cvv_encrypted, is_default) VALUES (?, ?, ?, ?, ?, ?)",
		customerID, cardNumberEncrypted, cardHolderEncrypted, expiryEncrypted, cvvEncrypted, true)
	return err
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
	if err != nil {
		return err
	}

	// Update login method
	_, err = tx.ExecContext(ctx,
		"UPDATE login_methods SET method_id = ? WHERE customer_id = ? AND method_type = 'email'",
		email, customerID)
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

	err := s.db.QueryRowContext(ctx,
		"SELECT customer_id, password_hash FROM login_methods WHERE method_type = 'email' AND method_id = ?",
		email).Scan(&customerID, &passwordHash)
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
	// This is a simplified implementation
	var customerID string
	err := s.db.QueryRowContext(ctx,
		"SELECT customer_id FROM login_methods WHERE method_type = ? AND method_id = ?",
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
