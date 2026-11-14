package reportoutcome

import (
	"context"
	"database/sql"
	_ "embed"
)

// SchemaSQL is executed only by explicit administrative migration entrypoints.
// Runtime readers and workers never create or alter tables.
//
//go:embed schema.sql
var SchemaSQL string

func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, SchemaSQL)
	return err
}
