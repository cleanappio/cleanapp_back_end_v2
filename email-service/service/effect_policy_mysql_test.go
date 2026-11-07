package service

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"cleanapp-common/effectpolicy"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

// This fixture is deliberately confined to a disposable local MySQL database.
// The parent task runs it on AWS; it never permits a production database DSN.
func TestHistoricalEffectPolicyMySQL(t *testing.T) {
	dsn := os.Getenv("EFFECT_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("set EFFECT_POLICY_TEST_DSN to an isolated AWS fixture")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_effect_policy_test") {
		t.Fatal("fixture only permits a local dedicated _effect_policy_test database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, query := range []string{"DROP TABLE IF EXISTS report_side_effect_policy", "DROP TABLE IF EXISTS reports", "CREATE TABLE reports(seq INT PRIMARY KEY)", "INSERT INTO reports VALUES(1),(2),(3),(4)"} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := effectpolicy.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO report_side_effect_policy(report_seq,source_namespace,reason) VALUES(2,'dataunion-0.1.0','historical import without effects')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO report_side_effect_policy(report_seq,source_namespace,reason,policy_version,suppress_email,suppress_notifications,suppress_rewards,suppress_routing,suppress_analysis) VALUES(3,'future','unknown semantics',2,FALSE,FALSE,FALSE,FALSE,FALSE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO report_side_effect_policy(report_seq,source_namespace,reason,suppress_email,suppress_notifications,suppress_rewards,suppress_routing,suppress_analysis) VALUES(4,'malformed','non-boolean flags',2,2,2,2,2)`); err != nil {
		t.Fatal(err)
	}
	for _, effect := range []effectpolicy.Effect{effectpolicy.Email, effectpolicy.Notifications, effectpolicy.Rewards, effectpolicy.Routing, effectpolicy.Analysis} {
		rows, err := db.QueryContext(ctx, `SELECT r.seq FROM reports r WHERE `+effectpolicy.AllowSQL("r", "seq", effect)+` ORDER BY r.seq`)
		if err != nil {
			t.Fatal(err)
		}
		var seqs []int
		for rows.Next() {
			var seq int
			if err := rows.Scan(&seq); err != nil {
				t.Fatal(err)
			}
			seqs = append(seqs, seq)
		}
		rows.Close()
		if len(seqs) != 1 || seqs[0] != 1 {
			t.Fatalf("effect %s allowed reports %v", effect, seqs)
		}
		blocked, err := effectpolicy.Suppressed(ctx, db, 2, effect)
		if err != nil || !blocked {
			t.Fatalf("historical effect %s blocked=%v err=%v", effect, blocked, err)
		}
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE report_side_effect_policy"); err != nil {
		t.Fatal(err)
	}
	blocked, err := effectpolicy.Suppressed(ctx, db, 1, effectpolicy.Email)
	if !blocked || err == nil {
		t.Fatal("missing policy schema authorized email")
	}
}
