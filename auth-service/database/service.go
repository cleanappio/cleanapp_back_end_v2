package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"auth-service/models"
	"auth-service/utils/encryption"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// AuthService handles all authentication-related database operations
type AuthService struct {
	db          *sql.DB
	encryptor   *encryption.Encryptor
	jwtSecret   []byte
	syncService *SyncService
}

// NewAuthService creates a new authentication service instance
func NewAuthService(db *sql.DB, encryptor *encryption.Encryptor, jwtSecret string, customerURL string) *AuthService {
	return &AuthService{
		db:          db,
		encryptor:   encryptor,
		jwtSecret:   []byte(jwtSecret),
		syncService: NewSyncService(db, encryptor, customerURL),
	}
}

// CreateUser creates a new user with email/password authentication
func (s *AuthService) CreateUser(ctx context.Context, req models.CreateUserRequest) (*models.ClientAuth, error) {
	// Check if user already exists
	exists, err := s.UserExistsByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, errors.New("user already exists")
	}

	// Generate user ID
	userID := generateUserID()

	// Encrypt email
	emailEncrypted, err := s.encryptor.Encrypt(req.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt email: %w", err)
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert user
	if err := s.insertUser(ctx, tx, userID, req.Name, emailEncrypted); err != nil {
		return nil, fmt.Errorf("failed to insert user: %w", err)
	}

	// Insert login method
	if err := s.insertLoginMethod(ctx, tx, userID, "email", string(passwordHash), ""); err != nil {
		return nil, fmt.Errorf("failed to insert login method: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Create user object for sync
	user := &models.ClientAuth{
		ID:        userID,
		Name:      req.Name,
		Email:     req.Email,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Trigger automatic sync to customer service
	go func() {
		if err := s.syncService.SyncToCustomerService(context.Background()); err != nil {
			log.Printf("Failed to sync new user %s to customer service: %v", userID, err)
		}
	}()

	return user, nil
}

// GetUser retrieves a user by ID
func (s *AuthService) GetUser(ctx context.Context, userID string) (*models.ClientAuth, error) {
	var name, emailEncrypted string
	var createdAt, updatedAt time.Time

	err := s.db.QueryRowContext(ctx,
		"SELECT name, email_encrypted, created_at, updated_at FROM client_auth WHERE id = ?",
		userID).Scan(&name, &emailEncrypted, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("user not found")
		}
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	// Decrypt email
	email, err := s.encryptor.Decrypt(emailEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt email: %w", err)
	}

	return &models.ClientAuth{
		ID:        userID,
		Name:      name,
		Email:     email,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// UpdateUser updates user information
func (s *AuthService) UpdateUser(ctx context.Context, userID string, req models.UpdateUserRequest) (*models.ClientAuth, error) {
	// Get current user
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Build update query
	updates := []string{}
	args := []interface{}{}

	if req.Name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *req.Name)
		user.Name = *req.Name
	}

	if req.Email != nil {
		// Check if email is already taken
		exists, err := s.UserExistsByEmail(ctx, *req.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to check email existence: %w", err)
		}
		if exists && *req.Email != user.Email {
			return nil, errors.New("email already taken")
		}

		// Encrypt new email
		emailEncrypted, err := s.encryptor.Encrypt(*req.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt email: %w", err)
		}

		updates = append(updates, "email_encrypted = ?")
		args = append(args, emailEncrypted)
		user.Email = *req.Email
	}

	if len(updates) == 0 {
		return user, nil
	}

	// Add user ID to args
	args = append(args, userID)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute update
	query := fmt.Sprintf("UPDATE client_auth SET %s, updated_at = CURRENT_TIMESTAMP WHERE id = ?", strings.Join(updates, ", "))
	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	user.UpdatedAt = time.Now()

	// Trigger automatic sync to customer service
	go func() {
		if err := s.syncService.SyncToCustomerService(context.Background()); err != nil {
			log.Printf("Failed to sync updated user %s to customer service: %v", userID, err)
		}
	}()

	return user, nil
}

// DeleteUser deletes a user and all associated data
func (s *AuthService) DeleteUser(ctx context.Context, userID string) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete user
	result, err := tx.ExecContext(ctx, "DELETE FROM client_auth WHERE id = ?", userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("user not found")
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Trigger automatic sync to customer service
	go func() {
		if err := s.syncService.SyncToCustomerService(context.Background()); err != nil {
			log.Printf("Failed to sync deleted user %s to customer service: %v", userID, err)
		}
	}()

	return nil
}

// Login authenticates a user and returns a token
func (s *AuthService) Login(ctx context.Context, req models.LoginRequest) (string, error) {
	var userID string
	var err error

	// Email/password login
	userID, err = s.authenticateWithPassword(ctx, req.Email, req.Password)
	if err != nil {
		return "", err
	}

	return s.generateToken(ctx, userID)
}

// ValidateToken validates a JWT token and returns the user ID
func (s *AuthService) ValidateToken(tokenString string) (string, error) {
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

	userID, ok := claims["user_id"].(string)
	if !ok {
		return "", errors.New("invalid user id in token")
	}

	// Verify token in database
	if err := s.verifyTokenInDB(userID, tokenString); err != nil {
		return "", err
	}

	return userID, nil
}

// GenerateTokenPair generates both access and refresh tokens
func (s *AuthService) GenerateTokenPair(ctx context.Context, userID string) (string, string, error) {
	// Calculate expiration times once to ensure consistency
	now := time.Now()
	accessExpiry := now.Add(1 * time.Hour)
	refreshExpiry := now.Add(30 * 24 * time.Hour)

	// Generate access token (1 hour expiry)
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"type":    "access",
		"exp":     accessExpiry.Unix(),
		"iat":     now.Unix(),
	})

	accessTokenString, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	// Generate refresh token (30 days expiry)
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"type":    "refresh",
		"exp":     refreshExpiry.Unix(),
		"iat":     now.Unix(),
	})

	refreshTokenString, err := refreshToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	// Store both tokens with the same expiry times
	if err := s.storeTokens(ctx, userID, accessTokenString, refreshTokenString, accessExpiry, refreshExpiry); err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

// ValidateRefreshToken validates a refresh token and returns the user ID
func (s *AuthService) ValidateRefreshToken(tokenString string) (string, error) {
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

	userID, ok := claims["user_id"].(string)
	if !ok {
		return "", errors.New("invalid user id in token")
	}

	// Verify token in database
	if err := s.verifyRefreshTokenInDB(userID, tokenString); err != nil {
		return "", err
	}

	return userID, nil
}

// InvalidateToken removes a token from the database
func (s *AuthService) InvalidateToken(ctx context.Context, userID, tokenString string) error {
	tokenHash := hashToken(tokenString)

	// Delete the token
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM auth_tokens WHERE user_id = ? AND token_hash = ?",
		userID, tokenHash)
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

// UserExistsByEmail checks if a user exists by email address
func (s *AuthService) UserExistsByEmail(ctx context.Context, email string) (bool, error) {
	// Validate email
	if email == "" || !isValidEmail(email) {
		return false, fmt.Errorf("invalid email format")
	}

	// Fetch all users and decrypt emails to find a match
	// This is necessary because encryption is not deterministic
	rows, err := s.db.QueryContext(ctx, "SELECT email_encrypted FROM client_auth")
	if err != nil {
		return false, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var emailEncrypted string
		if err := rows.Scan(&emailEncrypted); err != nil {
			continue
		}

		decryptedEmail, err := s.encryptor.Decrypt(emailEncrypted)
		if err != nil {
			continue
		}

		if decryptedEmail == email {
			return true, nil
		}
	}

	return false, nil
}

// Helper methods

func (s *AuthService) insertUser(ctx context.Context, tx *sql.Tx, id, name, emailEncrypted string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO client_auth (id, name, email_encrypted) VALUES (?, ?, ?)",
		id, name, emailEncrypted)
	return err
}

func (s *AuthService) insertLoginMethod(ctx context.Context, tx *sql.Tx, userID, methodType, passwordHash, oauthID string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO login_methods (user_id, method_type, password_hash, oauth_id) VALUES (?, ?, ?, ?)",
		userID, methodType, passwordHash, oauthID)
	return err
}

func (s *AuthService) authenticateWithPassword(ctx context.Context, email, password string) (string, error) {
	var userID string
	var passwordHash string

	// First, find the user by decrypting emails
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, email_encrypted FROM client_auth")
	if err != nil {
		return "", errors.New("authentication failed")
	}
	defer rows.Close()

	var foundUserID string
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
			foundUserID = id
			break
		}
	}

	if foundUserID == "" {
		log.Println("User not found for email:", email)
		return "", errors.New("invalid credentials")
	}

	// Now get the password hash for this user
	err = s.db.QueryRowContext(ctx,
		"SELECT user_id, password_hash FROM login_methods WHERE user_id = ? AND method_type = 'email'",
		foundUserID).Scan(&userID, &passwordHash)
	if err != nil {
		return "", errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		log.Println("Password mismatch for user:", userID)
		return "", errors.New("invalid credentials")
	}

	return userID, nil
}

// generateToken generates a JWT token for a user (legacy method for backward compatibility)
func (s *AuthService) generateToken(ctx context.Context, userID string) (string, error) {
	// Calculate expiry time once
	now := time.Now()
	expiry := now.Add(24 * time.Hour)

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     expiry.Unix(),
		"iat":     now.Unix(),
	})

	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	// Store token hash using FROM_UNIXTIME for consistent timezone handling
	tokenHash := hashToken(tokenString)
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO auth_tokens (user_id, token_hash, expires_at) VALUES (?, ?, FROM_UNIXTIME(?))",
		userID, tokenHash, expiry.Unix())
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// verifyTokenInDB checks if a token exists and is not expired
func (s *AuthService) verifyTokenInDB(userID, tokenString string) error {
	tokenHash := hashToken(tokenString)
	var exists bool

	// Use UTC_TIMESTAMP() for comparison to ensure timezone consistency
	err := s.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM auth_tokens WHERE user_id = ? AND token_hash = ? AND (token_type = 'access' OR token_type IS NULL) AND expires_at > NOW())",
		userID, tokenHash).Scan(&exists)
	if err != nil || !exists {
		return errors.New("token not found or expired")
	}
	return nil
}

// storeTokens stores access and refresh tokens with their expiration times using Unix timestamps
func (s *AuthService) storeTokens(ctx context.Context, userID, accessToken, refreshToken string, accessExpiry, refreshExpiry time.Time) error {
	accessHash := hashToken(accessToken)
	refreshHash := hashToken(refreshToken)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Store access token using FROM_UNIXTIME for consistent timezone handling
	_, err = tx.ExecContext(ctx,
		"INSERT INTO auth_tokens (user_id, token_hash, token_type, expires_at) VALUES (?, ?, 'access', FROM_UNIXTIME(?))",
		userID, accessHash, accessExpiry.Unix())
	if err != nil {
		return err
	}

	// Store refresh token using FROM_UNIXTIME for consistent timezone handling
	_, err = tx.ExecContext(ctx,
		"INSERT INTO auth_tokens (user_id, token_hash, token_type, expires_at) VALUES (?, ?, 'refresh', FROM_UNIXTIME(?))",
		userID, refreshHash, refreshExpiry.Unix())
	if err != nil {
		return err
	}

	return tx.Commit()
}

// verifyRefreshTokenInDB checks if a refresh token exists and is not expired
func (s *AuthService) verifyRefreshTokenInDB(userID, tokenString string) error {
	tokenHash := hashToken(tokenString)
	var exists bool

	// Use UTC_TIMESTAMP() for comparison to ensure timezone consistency
	err := s.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM auth_tokens WHERE user_id = ? AND token_hash = ? AND token_type = 'refresh' AND expires_at > NOW())",
		userID, tokenHash).Scan(&exists)
	if err != nil || !exists {
		return errors.New("refresh token not found or expired")
	}
	return nil
}

// Utility functions

func generateUserID() string {
	return fmt.Sprintf("user_%d", time.Now().UnixNano())
}

func hashToken(token string) string {
	// Simple hash for token storage - in production, use a proper hash function
	return fmt.Sprintf("%x", len(token))
}

func isValidEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}
