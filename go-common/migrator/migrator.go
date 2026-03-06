package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

const tableName = "go_schema_migrations"

type Step struct {
	ID          string
	Description string
	Up          func(context.Context, *sql.DB) error
}

func Run(ctx context.Context, db *sql.DB, service string, steps []Step) error {
	service = strings.TrimSpace(service)
	if service == "" {
		return fmt.Errorf("service name is required")
	}
	if err := ensureTable(ctx, db); err != nil {
		return err
	}

	sorted := append([]Step(nil), steps...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	applied, err := appliedSteps(ctx, db, service)
	if err != nil {
		return err
	}

	for _, step := range sorted {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("migration step id is required")
		}
		if step.Up == nil {
			return fmt.Errorf("migration step %s has nil Up", step.ID)
		}
		if _, ok := applied[step.ID]; ok {
			continue
		}
		if err := step.Up(ctx, db); err != nil {
			return fmt.Errorf("migration %s failed: %w", step.ID, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO `+tableName+` (service_name, step_id, description)
			VALUES (?, ?, ?)
		`, service, step.ID, step.Description); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", step.ID, err)
		}
	}

	return nil
}

func ensureTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+tableName+` (
			service_name VARCHAR(128) NOT NULL,
			step_id VARCHAR(255) NOT NULL,
			description TEXT,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (service_name, step_id),
			INDEX idx_go_schema_migrations_applied_at (applied_at)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to ensure %s table: %w", tableName, err)
	}
	return nil
}

func appliedSteps(ctx context.Context, db *sql.DB, service string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT step_id
		FROM `+tableName+`
		WHERE service_name = ?
	`, service)
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	out := make(map[string]struct{})
	for rows.Next() {
		var stepID string
		if err := rows.Scan(&stepID); err != nil {
			return nil, fmt.Errorf("failed to scan applied migration: %w", err)
		}
		out[stepID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate applied migrations: %w", err)
	}
	return out, nil
}
