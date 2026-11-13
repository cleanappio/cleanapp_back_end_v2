package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"time"
)

// EnsurePublicAgentActor provisions one server-owned actor, never an account
// per IP. The legacy credential has no known preimage and legacy auth is off.
func (d *Database) EnsurePublicAgentActor(ctx context.Context, actorID string) error {
	hash := make([]byte, 32)
	if _, err := rand.Read(hash); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO fetchers
		(fetcher_id,name,token_hash,scopes,active,owner_type,status,tier,reputation_score,daily_cap_items,per_minute_cap_items,default_visibility,default_trust_level,routing_enabled,rewards_enabled,last_seen_at)
		VALUES (?,'Public anonymous agent intake',?,NULL,FALSE,'public_agent','active',0,0,0,0,'shadow','unverified',FALSE,FALSE,NOW())
		ON DUPLICATE KEY UPDATE active=FALSE,tier=0,default_visibility='shadow',default_trust_level='unverified',routing_enabled=FALSE,rewards_enabled=FALSE,last_seen_at=NOW()`, actorID, hash)
	return err
}

// TryPublicAgentIntentLock serializes one intent across listener replicas. Keep
// the dedicated connection until release: MySQL named locks are connection-owned.
func (d *Database) TryPublicAgentIntentLock(ctx context.Context, name string) (func(), bool, error) {
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?,0)`, name).Scan(&acquired); err != nil {
		conn.Close()
		return nil, false, err
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		conn.Close()
		return nil, false, nil
	}
	release := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(releaseCtx, `SELECT RELEASE_LOCK(?)`, name).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			// Discard a connection whose lock ownership cannot be confirmed.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
	return release, true, nil
}
