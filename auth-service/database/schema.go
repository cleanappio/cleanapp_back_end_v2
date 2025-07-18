package database

import (
	"database/sql"
	"fmt"
	"log"
)

// Schema contains the database schema for authentication
const Schema = `
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;

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
);

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
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(256) NOT NULL,
    token_hash VARCHAR(256) NOT NULL,
    token_type ENUM('access', 'refresh') DEFAULT 'access',
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_token_type (user_id, token_type),
	CONSTRAINT auth_tokens_ibfk_1 FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE
);
`

// InitializeSchema creates the database schema and runs migrations
func InitializeSchema(db *sql.DB) error {
	// Create tables
	if _, err := db.Exec(Schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	log.Println("Database schema initialized successfully")
	return nil
}
