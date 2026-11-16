// Package sf311import owns the additive case and media ledgers used by the
// trusted SolveSF importer. It adds no public intake API or processing hooks.
package sf311import

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

// Schema is also checked against the native writer's isolated MySQL fixture.
//
//go:embed schema.sql
var Schema string

func Migrate(ctx context.Context, db *sql.DB) error {
	for _, statement := range strings.Split(Schema, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create SF311 source ledger: %w", err)
		}
	}
	return nil
}
