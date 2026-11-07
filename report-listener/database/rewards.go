package database

import (
	"cleanapp-common/effectpolicy"
	"context"
	"fmt"
	"strings"
)

// IncrementReporterDailyKITNs preserves the legacy mobile reward behavior for
// wallet-backed human submissions that now enter through the canonical ingest path.
func (d *Database) IncrementReporterDailyKITNs(ctx context.Context, reporterID string, reportSeq int) error {
	reporterID = strings.TrimSpace(reporterID)
	if reporterID == "" {
		return nil
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reward tx: %w", err)
	}
	defer tx.Rollback()
	if blocked, err := effectpolicy.Suppressed(ctx, tx, int64(reportSeq), effectpolicy.Rewards); err != nil {
		return err
	} else if blocked {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET kitns_daily = kitns_daily + 1
		WHERE id = ?
	`, reporterID); err != nil {
		return fmt.Errorf("increment users.kitns_daily: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE users_shadow
		SET kitns_daily = kitns_daily + 1
		WHERE id = ?
	`, reporterID); err != nil {
		return fmt.Errorf("increment users_shadow.kitns_daily: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reward tx: %w", err)
	}
	return nil
}
