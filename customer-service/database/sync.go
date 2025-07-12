package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"customer-service/models"
	"customer-service/utils/encryption"
)

// SyncService handles synchronization between customer-service and auth-service
type SyncService struct {
	customerDB *sql.DB
	encryptor  *encryption.Encryptor
	authURL    string
	httpClient *http.Client
}

// AuthData represents the auth data structure from auth-service
type AuthData struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	SyncVersion int       `json:"sync_version"`
	LastSyncAt  time.Time `json:"last_sync_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewSyncService creates a new synchronization service
func NewSyncService(customerDB *sql.DB, encryptor *encryption.Encryptor, authURL string) *SyncService {
	return &SyncService{
		customerDB: customerDB,
		encryptor:  encryptor,
		authURL:    authURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SyncFromAuthService fetches auth data and syncs it to customers table
func (s *SyncService) SyncFromAuthService(ctx context.Context) error {
	// Fetch auth data from auth-service
	authRecords, err := s.fetchAuthFromAuthService(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch auth data: %w", err)
	}

	// Sync each auth record
	for _, auth := range authRecords {
		if err := s.syncAuthToCustomer(ctx, auth); err != nil {
			log.Printf("Failed to sync auth %s: %v", auth.ID, err)
			continue
		}
	}

	return nil
}

// SyncToAuthService syncs customers data to auth-service
func (s *SyncService) SyncToAuthService(ctx context.Context) error {
	// Get all customer records that need syncing
	rows, err := s.customerDB.QueryContext(ctx, `
		SELECT id, name, email_encrypted, sync_version, last_sync_at, created_at, updated_at 
		FROM customers 
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return fmt.Errorf("failed to query customers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var customer models.Customer
		var emailEncrypted string
		var syncVersion int
		var lastSyncAt time.Time

		if err := rows.Scan(
			&customer.ID, &customer.Name, &emailEncrypted, &syncVersion, &lastSyncAt,
			&customer.CreatedAt, &customer.UpdatedAt,
		); err != nil {
			log.Printf("Failed to scan customer row: %v", err)
			continue
		}

		// Decrypt email
		email, err := s.encryptor.Decrypt(emailEncrypted)
		if err != nil {
			log.Printf("Failed to decrypt email for %s: %v", customer.ID, err)
			continue
		}
		customer.Email = email

		// Sync to auth-service
		if err := s.syncCustomerToAuth(ctx, customer); err != nil {
			log.Printf("Failed to sync customer %s to auth: %v", customer.ID, err)
			continue
		}
	}

	return nil
}

// fetchAuthFromAuthService fetches auth data from auth-service
func (s *SyncService) fetchAuthFromAuthService(ctx context.Context) ([]AuthData, error) {
	url := fmt.Sprintf("%s/api/v3/auth/sync", s.authURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch auth data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth service returned status %d", resp.StatusCode)
	}

	var authRecords []AuthData
	if err := json.NewDecoder(resp.Body).Decode(&authRecords); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return authRecords, nil
}

// syncAuthToCustomer syncs an auth record to the customers table
func (s *SyncService) syncAuthToCustomer(ctx context.Context, auth AuthData) error {
	// Encrypt email
	emailEncrypted, err := s.encryptor.Encrypt(auth.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}

	// Check if customer record exists
	var existingSyncVersion int
	err = s.customerDB.QueryRowContext(ctx,
		"SELECT sync_version FROM customers WHERE id = ?",
		auth.ID).Scan(&existingSyncVersion)

	if err == sql.ErrNoRows {
		// Insert new record
		_, err = s.customerDB.ExecContext(ctx, `
			INSERT INTO customers (id, name, email_encrypted, sync_version, last_sync_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, auth.ID, auth.Name, emailEncrypted, auth.SyncVersion, auth.LastSyncAt, auth.CreatedAt, auth.UpdatedAt)
	} else if err == nil && auth.SyncVersion > existingSyncVersion {
		// Update existing record if auth version is newer
		_, err = s.customerDB.ExecContext(ctx, `
			UPDATE customers 
			SET name = ?, email_encrypted = ?, sync_version = ?, last_sync_at = ?, updated_at = ?
			WHERE id = ?
		`, auth.Name, emailEncrypted, auth.SyncVersion, auth.LastSyncAt, auth.UpdatedAt, auth.ID)
	}

	if err != nil {
		return fmt.Errorf("failed to sync auth to customer: %w", err)
	}

	return nil
}

// syncCustomerToAuth syncs a customer record to the auth-service
func (s *SyncService) syncCustomerToAuth(ctx context.Context, customer models.Customer) error {
	// Prepare auth data
	authData := AuthData{
		ID:          customer.ID,
		Name:        customer.Name,
		Email:       customer.Email,
		SyncVersion: 1, // Will be incremented by auth-service
		LastSyncAt:  time.Now(),
		CreatedAt:   customer.CreatedAt,
		UpdatedAt:   customer.UpdatedAt,
	}

	// Send to auth-service
	url := fmt.Sprintf("%s/api/v3/auth/sync", s.authURL)

	jsonData, err := json.Marshal(authData)
	if err != nil {
		return fmt.Errorf("failed to marshal auth data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to sync to auth service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth service returned status %d", resp.StatusCode)
	}

	// Update sync version in customer-service
	_, err = s.customerDB.ExecContext(ctx, `
		UPDATE customers 
		SET sync_version = sync_version + 1, last_sync_at = NOW()
		WHERE id = ?
	`, customer.ID)

	if err != nil {
		return fmt.Errorf("failed to update sync version: %w", err)
	}

	return nil
}

// GetUnsyncedRecords returns records that need synchronization
func (s *SyncService) GetUnsyncedRecords(ctx context.Context) ([]models.Customer, error) {
	rows, err := s.customerDB.QueryContext(ctx, `
		SELECT id, name, email_encrypted, created_at, updated_at
		FROM customers 
		WHERE last_sync_at < updated_at OR last_sync_at IS NULL
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query unsynced records: %w", err)
	}
	defer rows.Close()

	var records []models.Customer
	for rows.Next() {
		var customer models.Customer
		var emailEncrypted string

		if err := rows.Scan(&customer.ID, &customer.Name, &emailEncrypted, &customer.CreatedAt, &customer.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}

		// Decrypt email
		email, err := s.encryptor.Decrypt(emailEncrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		customer.Email = email

		records = append(records, customer)
	}

	return records, nil
}
