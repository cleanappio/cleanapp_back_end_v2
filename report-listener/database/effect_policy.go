package database

import (
	"context"

	"cleanapp-common/effectpolicy"
)

func (d *Database) SideEffectSuppressed(ctx context.Context, seq int, effect effectpolicy.Effect) (bool, error) {
	return effectpolicy.Suppressed(ctx, d.db, int64(seq), effect)
}

func (d *Database) CaseSideEffectSuppressed(ctx context.Context, caseID string, effect effectpolicy.Effect) (bool, error) {
	return effectpolicy.CaseSuppressed(ctx, d.db, caseID, effect)
}
