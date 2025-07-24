package database

import (
	"context"
	"testing"

	"report_processor/config"
)

func TestEnsureReportStatusTable(t *testing.T) {
	// This test requires a running MySQL database
	// Skip if not available
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     "3306",
		DBUser:     "server",
		DBPassword: "secret_app",
		DBName:     "cleanapp",
	}

	db, err := NewDatabase(cfg)
	if err != nil {
		t.Skipf("Skipping test - database not available: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()
	err = db.EnsureReportStatusTable(ctx)
	if err != nil {
		t.Errorf("Failed to ensure report_status table: %v", err)
	}
}

func TestMarkReportResolved(t *testing.T) {
	// This test requires a running MySQL database with reports table
	// Skip if not available
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     "3306",
		DBUser:     "server",
		DBPassword: "secret_app",
		DBName:     "cleanapp",
	}

	db, err := NewDatabase(cfg)
	if err != nil {
		t.Skipf("Skipping test - database not available: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()

	// Ensure table exists
	err = db.EnsureReportStatusTable(ctx)
	if err != nil {
		t.Skipf("Skipping test - cannot ensure table: %v", err)
		return
	}

	// Test with a non-existent report (should fail)
	err = db.MarkReportResolved(ctx, 999999)
	if err == nil {
		t.Error("Expected error when marking non-existent report as resolved")
	}
}
