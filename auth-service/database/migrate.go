package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "auth-service", []migrator.Step{
		{ID: "0001_client_auth", Description: "create client_auth table", Up: createClientAuthTable},
		{ID: "0002_login_methods", Description: "create login_methods table", Up: createLoginMethodsTable},
		{ID: "0003_auth_tokens", Description: "create auth_tokens table", Up: createAuthTokensTable},
		{ID: "0004_password_reset_tokens", Description: "create password_reset_tokens table", Up: createPasswordResetTokensTable},
	})
}

func createClientAuthTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS client_auth (
			id VARCHAR(256) PRIMARY KEY,
			name VARCHAR(256) NOT NULL,
			email_encrypted TEXT NOT NULL,
			sync_version INT DEFAULT 1,
			last_sync_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_email_encrypted (email_encrypted(255)),
			INDEX idx_sync_version (sync_version)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create client_auth table: %w", err)
	}
	return nil
}

func createLoginMethodsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS login_methods (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id VARCHAR(256) NOT NULL,
			method_type ENUM('email', 'google', 'apple', 'facebook') NOT NULL,
			password_hash VARCHAR(256),
			oauth_id VARCHAR(256),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user_method (user_id, method_type),
			INDEX idx_oauth (method_type, oauth_id),
			CONSTRAINT login_methods_ibfk_1 FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE,
			CONSTRAINT unique_user_method UNIQUE (user_id, method_type)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create login_methods table: %w", err)
	}
	return nil
}

func createAuthTokensTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS auth_tokens (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id VARCHAR(256) NOT NULL,
			token_hash VARCHAR(256) NOT NULL,
			token_type ENUM('access', 'refresh') DEFAULT 'access',
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user_token_type (user_id, token_type),
			CONSTRAINT auth_tokens_ibfk_1 FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create auth_tokens table: %w", err)
	}
	return nil
}

func createPasswordResetTokensTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id VARCHAR(256) NOT NULL,
			token_hash VARCHAR(256) NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			used_at TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_token_hash (token_hash),
			CONSTRAINT password_reset_tokens_ibfk_1 FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create password_reset_tokens table: %w", err)
	}
	return nil
}
