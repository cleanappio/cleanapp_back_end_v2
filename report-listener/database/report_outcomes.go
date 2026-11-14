package database

import (
	"cleanapp-common/reportoutcome"
	"context"
	"database/sql"
	"errors"
)

func (d *Database) ReportOutcome(ctx context.Context, seq int) (*reportoutcome.Outcome, error) {
	outcome, err := reportoutcome.NewStore(d.db).Get(ctx, seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return outcome, err
}
