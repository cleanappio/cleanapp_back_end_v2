// Package effectpolicy separates historical publication from permission to run
// outbound effects. An absent row preserves ordinary report behavior; a failed
// policy read never authorizes an effect.
package effectpolicy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
)

type Effect string

const (
	Email         Effect = "email"
	Notifications Effect = "notifications"
	Rewards       Effect = "rewards"
	Routing       Effect = "routing"
	Analysis      Effect = "analysis"
	Version              = 1
)

const CreateTableSQL = `CREATE TABLE IF NOT EXISTS report_side_effect_policy (
 report_seq INT NOT NULL PRIMARY KEY,
 source_namespace VARCHAR(64) NOT NULL,
 reason VARCHAR(255) NOT NULL,
 policy_version INT NOT NULL DEFAULT 1,
 suppress_email BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_notifications BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_rewards BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_routing BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_analysis BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY idx_report_effect_policy_source (source_namespace, report_seq),
 CONSTRAINT fk_report_effect_policy_report FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func column(effect Effect) (string, bool) {
	switch effect {
	case Email:
		return "suppress_email", true
	case Notifications:
		return "suppress_notifications", true
	case Rewards:
		return "suppress_rewards", true
	case Routing:
		return "suppress_routing", true
	case Analysis:
		return "suppress_analysis", true
	default:
		return "", false
	}
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// AllowSQL is a correlated indexed predicate. Its identifiers are fixed by code,
// never request input. Unknown effects or identifiers yield FALSE. Unknown
// stored policy versions and NULL flags are suppressed instead of guessed.
func AllowSQL(alias, seqColumn string, effect Effect) string {
	flag, ok := column(effect)
	if !ok || !identifier.MatchString(alias) || !identifier.MatchString(seqColumn) {
		return "FALSE"
	}
	return `NOT EXISTS (SELECT 1 FROM report_side_effect_policy effect_policy WHERE effect_policy.report_seq=` + alias + `.` + seqColumn + ` AND (COALESCE(effect_policy.policy_version,0)<>1 OR COALESCE(effect_policy.` + flag + `,TRUE)<>FALSE))`
}

type QueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Suppressed(ctx context.Context, q QueryRower, seq int64, effect Effect) (bool, error) {
	flag, ok := column(effect)
	if !ok || seq <= 0 {
		return true, fmt.Errorf("invalid report effect policy lookup")
	}
	var version int
	var blocked bool
	err := q.QueryRowContext(ctx, `SELECT policy_version, `+flag+` FROM report_side_effect_policy WHERE report_seq=?`, seq).Scan(&version, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("read report side-effect policy: %w", err)
	}
	if version != Version {
		return true, fmt.Errorf("unsupported report side-effect policy version")
	}
	return blocked, nil
}

// CaseSuppressed also covers the anchor report if a legacy case does not yet
// have a case_reports link. A policy error prevents the case transport call.
func CaseSuppressed(ctx context.Context, q QueryRower, caseID string, effect Effect) (bool, error) {
	flag, ok := column(effect)
	if !ok || caseID == "" {
		return true, fmt.Errorf("invalid case effect policy lookup")
	}
	var blocked bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM report_side_effect_policy p WHERE
 (p.report_seq IN (SELECT seq FROM case_reports WHERE case_id=?)
  OR p.report_seq=(SELECT anchor_report_seq FROM cases WHERE case_id=?))
 AND (COALESCE(p.policy_version,0)<>1 OR COALESCE(p.`+flag+`,TRUE)<>FALSE))`, caseID, caseID).Scan(&blocked)
	if err != nil {
		return true, fmt.Errorf("read case side-effect policy: %w", err)
	}
	return blocked, nil
}

// Migrate is an explicit additive release step. Runtime reads never create or
// repair schema. No report rows, original history, or balances are modified.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, CreateTableSQL); err != nil {
		return fmt.Errorf("create report side-effect policy: %w", err)
	}
	return Verify(ctx, db)
}

func Verify(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT report_seq,source_namespace,reason,policy_version,suppress_email,suppress_notifications,suppress_rewards,suppress_routing,suppress_analysis,created_at FROM report_side_effect_policy WHERE FALSE`)
	if err != nil {
		return fmt.Errorf("verify report side-effect policy columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var primary, primaryColumns, foreignKey int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(column_name='report_seq' AND seq_in_index=1 AND non_unique=0),0) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='report_side_effect_policy' AND index_name='PRIMARY'`).Scan(&primaryColumns, &primary); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.key_column_usage WHERE table_schema=DATABASE() AND table_name='report_side_effect_policy' AND column_name='report_seq' AND referenced_table_schema=DATABASE() AND referenced_table_name='reports' AND referenced_column_name='seq'`).Scan(&foreignKey); err != nil {
		return err
	}
	if primary != 1 || primaryColumns != 1 || foreignKey != 1 {
		return fmt.Errorf("report side-effect policy must retain report primary/foreign keys")
	}
	return nil
}
