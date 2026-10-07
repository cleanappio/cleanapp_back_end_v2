package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// ReportAnalysisLock serializes analysis of a sequence across analyzer processes.
// MySQL named locks belong to sessions, so this connection must stay pinned until
// analysis and publication finish, and release must use the same connection.
type ReportAnalysisLock struct {
	conn *sql.Conn
	name string
}

// LockReportAnalysis acquires a per-report lock, then checks the publication marker
// while holding it. Already published queue replays can be acknowledged without
// overwriting analysis or sending another downstream notification event.
func (d *Database) LockReportAnalysis(ctx context.Context, seq int) (*ReportAnalysisLock, bool, error) {
	if seq <= 0 {
		return nil, false, fmt.Errorf("invalid report sequence %d", seq)
	}
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("get analysis lock connection: %w", err)
	}
	lock := &ReportAnalysisLock{conn: conn, name: fmt.Sprintf("cleanapp:report-analysis:%d", seq)}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 30)`, lock.name).Scan(&acquired); err != nil {
		// A cancelled query may have acquired its session lock just before the
		// cancellation reached the client. Discarding the connection releases it.
		lock.discardConnection()
		return nil, false, fmt.Errorf("acquire report %d analysis lock: %w", seq, err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		return nil, false, fmt.Errorf("report %d analysis already in progress; lock unavailable", seq)
	}

	var published sql.NullTime
	err = conn.QueryRowContext(ctx, `SELECT analysed_published_at FROM report_raw WHERE report_seq = ?`, seq).Scan(&published)
	if err != nil && err != sql.ErrNoRows {
		_ = lock.Release()
		return nil, false, fmt.Errorf("read report %d publication marker: %w", seq, err)
	}
	return lock, published.Valid, nil
}

// Release releases the named lock before returning its connection to the pool.
func (l *ReportAnalysisLock) Release() error {
	if l.conn == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var released sql.NullInt64
	err := l.conn.QueryRowContext(ctx, `SELECT RELEASE_LOCK(?)`, l.name).Scan(&released)
	if err != nil || !released.Valid || released.Int64 != 1 {
		l.discardConnection()
		if err != nil {
			return fmt.Errorf("release analysis lock: %w", err)
		}
		return fmt.Errorf("release analysis lock %s: session did not own the lock", l.name)
	}
	err = l.conn.Close()
	l.conn = nil
	return err
}

func (l *ReportAnalysisLock) discardConnection() {
	_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = l.conn.Close()
	l.conn = nil
}
