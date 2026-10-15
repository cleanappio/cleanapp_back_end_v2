package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

// withCompanyNotificationLock serializes cooperating workers from selection
// through accepted-receipt persistence. MySQL named locks belong to a physical
// connection, so the lease stays on one dedicated Conn until explicit release.
// The callback uses the normal pool and therefore needs another free connection.
// This does not promise exactly-once SMTP delivery across a process/DB crash.
// Read-only notification plans deliberately do not call this wrapper.
func (s *EmailService) withCompanyNotificationLock(ctx context.Context, companyKey string, work func() error) (ran bool, err error) {
	route, known := ResolveCompanyRoute(companyKey)
	if !known {
		return false, fmt.Errorf("cannot lock an unknown notification company")
	}
	if work == nil {
		return false, fmt.Errorf("company notification work is required")
	}
	lockName := "cleanapp:company-notification:" + route.CompanyKey
	acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	conn, err := s.db.Conn(acquireCtx)
	if err != nil {
		cancel()
		return false, fmt.Errorf("get company notification lock connection: %w", err)
	}
	var acquired sql.NullInt64
	err = conn.QueryRowContext(acquireCtx, `SELECT GET_LOCK(?,0)`, lockName).Scan(&acquired)
	cancel()
	if err != nil {
		// The server may have acquired the lock before a response/scan failed.
		// A plain Conn.Close would return that physical connection to the pool.
		discardCompanyLockConnection(conn)
		return false, fmt.Errorf("acquire company notification lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		if acquired.Valid && acquired.Int64 == 0 {
			conn.Close()
			return false, nil // Another worker owns this company; do not wait.
		}
		discardCompanyLockConnection(conn)
		return false, fmt.Errorf("company notification lock was not acquired")
	}
	defer func() {
		// A canceled work context must not strand a connection-scoped lock.
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer releaseCancel()
		var released sql.NullInt64
		releaseErr := conn.QueryRowContext(releaseCtx, `SELECT RELEASE_LOCK(?)`, lockName).Scan(&released)
		if releaseErr != nil || !released.Valid || released.Int64 != 1 {
			discardCompanyLockConnection(conn)
			if releaseErr == nil {
				releaseErr = fmt.Errorf("company notification lock release was not confirmed")
			}
			err = errors.Join(err, fmt.Errorf("release company notification lock: %w", releaseErr))
			return
		}
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close company notification lock connection: %w", closeErr))
		}
	}()
	return true, work()
}

func discardCompanyLockConnection(conn *sql.Conn) {
	// database/sql discards the physical connection on driver.ErrBadConn,
	// releasing a possible server-side lock instead of reusing its session.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}
