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

	"auth-service/models"
	"auth-service/utils/encryption"
)

// SyncService handles synchronization between auth-service and customer-service
type SyncService struct {
	authDB      *sql.DB
	encryptor   *encryption.Encryptor
	customerURL string
	httpClient  *http.Client
}

// CustomerData represents the customer data structure from customer-service
type CustomerData struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	SyncVersion int       `json:"sync_version"`
	LastSyncAt  time.Time `json:"last_sync_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewSyncService creates a new synchronization service
func NewSyncService(authDB *sql.DB, encryptor *encryption.Encryptor, customerURL string) *SyncService {
	return &SyncService{
		authDB:      authDB,
		encryptor:   encryptor,
		customerURL: customerURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SyncFromCustomerService fetches customer data and syncs it to client_auth table
func (s *SyncService) SyncFromCustomerService(ctx context.Context) error {
	// Fetch customers from customer-service
	customers, err := s.fetchCustomersFromCustomerService(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch customers: %w", err)
	}

	// Sync each customer
	for _, customer := range customers {
		if err := s.syncCustomerToAuth(ctx, customer); err != nil {
			log.Printf("Failed to sync customer %s: %v", customer.ID, err)
			continue
		}
	}

	return nil
}

// SyncToCustomerService syncs client_auth data to customer-service
func (s *SyncService) SyncToCustomerService(ctx context.Context) error {
	// Get all client_auth records that need syncing
	rows, err := s.authDB.QueryContext(ctx, `
		SELECT id, name, email_encrypted, sync_version, last_sync_at, created_at, updated_at 
		FROM client_auth 
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return fmt.Errorf("failed to query client_auth: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var auth models.ClientAuth
		var emailEncrypted string
		var syncVersion int
		var lastSyncAt time.Time

		if err := rows.Scan(
			&auth.ID, &auth.Name, &emailEncrypted, &syncVersion, &lastSyncAt,
			&auth.CreatedAt, &auth.UpdatedAt,
		); err != nil {
			log.Printf("Failed to scan client_auth row: %v", err)
			continue
		}

		// Decrypt email
		email, err := s.encryptor.Decrypt(emailEncrypted)
		if err != nil {
			log.Printf("Failed to decrypt email for %s: %v", auth.ID, err)
			continue
		}
		auth.Email = email

		// Sync to customer-service
		if err := s.syncAuthToCustomer(ctx, auth); err != nil {
			log.Printf("Failed to sync auth %s to customer: %v", auth.ID, err)
			continue
		}
	}

	return nil
}

// fetchCustomersFromCustomerService fetches customer data from customer-service
func (s *SyncService) fetchCustomersFromCustomerService(ctx context.Context) ([]CustomerData, error) {
	url := fmt.Sprintf("%s/api/v3/customers/sync", s.customerURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch customers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("customer service returned status %d", resp.StatusCode)
	}

	var customers []CustomerData
	if err := json.NewDecoder(resp.Body).Decode(&customers); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return customers, nil
}

// syncCustomerToAuth syncs a customer record to the client_auth table
func (s *SyncService) syncCustomerToAuth(ctx context.Context, customer CustomerData) error {
	// Encrypt email
	emailEncrypted, err := s.encryptor.Encrypt(customer.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}

	// Check if client_auth record exists
	var existingSyncVersion int
	err = s.authDB.QueryRowContext(ctx,
		"SELECT sync_version FROM client_auth WHERE id = ?",
		customer.ID).Scan(&existingSyncVersion)

	if err == sql.ErrNoRows {
		// Insert new record
		_, err = s.authDB.ExecContext(ctx, `
			INSERT INTO client_auth (id, name, email_encrypted, sync_version, last_sync_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, customer.ID, customer.Name, emailEncrypted, customer.SyncVersion, customer.LastSyncAt, customer.CreatedAt, customer.UpdatedAt)
	} else if err == nil && customer.SyncVersion > existingSyncVersion {
		// Update existing record if customer version is newer
		_, err = s.authDB.ExecContext(ctx, `
			UPDATE client_auth 
			SET name = ?, email_encrypted = ?, sync_version = ?, last_sync_at = ?, updated_at = ?
			WHERE id = ?
		`, customer.Name, emailEncrypted, customer.SyncVersion, customer.LastSyncAt, customer.UpdatedAt, customer.ID)
	}

	if err != nil {
		return fmt.Errorf("failed to sync customer to auth: %w", err)
	}

	return nil
}

// syncAuthToCustomer syncs a client_auth record to the customer-service
func (s *SyncService) syncAuthToCustomer(ctx context.Context, auth models.ClientAuth) error {
	// Prepare customer data
	customerData := CustomerData{
		ID:          auth.ID,
		Name:        auth.Name,
		Email:       auth.Email,
		SyncVersion: 1, // Will be incremented by customer-service
		LastSyncAt:  time.Now(),
		CreatedAt:   auth.CreatedAt,
		UpdatedAt:   auth.UpdatedAt,
	}

	// Send to customer-service
	url := fmt.Sprintf("%s/api/v3/customers/sync", s.customerURL)

	jsonData, err := json.Marshal(customerData)
	if err != nil {
		return fmt.Errorf("failed to marshal customer data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to sync to customer service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("customer service returned status %d", resp.StatusCode)
	}

	// Update sync version in auth-service
	_, err = s.authDB.ExecContext(ctx, `
		UPDATE client_auth 
		SET sync_version = sync_version + 1, last_sync_at = NOW()
		WHERE id = ?
	`, auth.ID)

	if err != nil {
		return fmt.Errorf("failed to update sync version: %w", err)
	}

	return nil
}

// GetUnsyncedRecords returns records that need synchronization
func (s *SyncService) GetUnsyncedRecords(ctx context.Context) ([]models.ClientAuth, error) {
	rows, err := s.authDB.QueryContext(ctx, `
		SELECT id, name, email_encrypted, created_at, updated_at
		FROM client_auth 
		WHERE last_sync_at < updated_at OR last_sync_at IS NULL
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query unsynced records: %w", err)
	}
	defer rows.Close()

	var records []models.ClientAuth
	for rows.Next() {
		var auth models.ClientAuth
		var emailEncrypted string

		if err := rows.Scan(&auth.ID, &auth.Name, &emailEncrypted, &auth.CreatedAt, &auth.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}

		// Decrypt email
		email, err := s.encryptor.Decrypt(emailEncrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		auth.Email = email

		records = append(records, auth)
	}

	return records, nil
}
