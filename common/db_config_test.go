package common

import (
	"strings"
	"testing"
)

func TestDatabasePasswordEnvironmentAndLegacyCLI(t *testing.T) {
	old := *mysqlPassword
	defer func() { *mysqlPassword = old }()
	t.Setenv("MYSQL_APP_PASSWORD", "environment-password")
	t.Setenv("DB_PASSWORD", "secondary-password")
	*mysqlPassword = ""
	dsn, err := mysqlAddress()
	if err != nil || !strings.Contains(dsn, "environment-password@") {
		t.Fatal("existing environment password fallback unavailable")
	}
	*mysqlPassword = "explicit-password"
	dsn, err = mysqlAddress()
	if err != nil || !strings.Contains(dsn, "explicit-password@") {
		t.Fatal("legacy explicit password flag precedence changed")
	}
}
