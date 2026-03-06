package database

import (
	"customer-service/config"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func OpenDB(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/cleanapp?parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	applyDBPoolSettings(db)

	maxWaitSec := envInt([]string{"CUSTOMER_SERVICE_DB_PING_MAX_WAIT_SEC", "DB_PING_MAX_WAIT_SEC"}, 60)
	deadline := time.Now().Add(time.Duration(maxWaitSec) * time.Second)
	waitInterval := time.Second
	for {
		pingErr := db.Ping()
		if pingErr == nil {
			return db, nil
		}
		if time.Now().After(deadline) {
			return nil, pingErr
		}
		time.Sleep(waitInterval)
		waitInterval *= 2
		if waitInterval > 30*time.Second {
			waitInterval = 30 * time.Second
		}
	}
}

func applyDBPoolSettings(db *sql.DB) {
	maxOpen := envInt([]string{"CUSTOMER_SERVICE_DB_MAX_OPEN_CONNS", "DB_MAX_OPEN_CONNS"}, 20)
	maxIdle := envInt([]string{"CUSTOMER_SERVICE_DB_MAX_IDLE_CONNS", "DB_MAX_IDLE_CONNS"}, 10)
	maxLifetimeMin := envInt([]string{"CUSTOMER_SERVICE_DB_CONN_MAX_LIFETIME_MIN", "DB_CONN_MAX_LIFETIME_MIN"}, 5)

	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		db.SetMaxIdleConns(maxIdle)
	}
	if maxLifetimeMin > 0 {
		db.SetConnMaxLifetime(time.Duration(maxLifetimeMin) * time.Minute)
	}
}

func envInt(keys []string, def int) int {
	for _, key := range keys {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			return n
		}
	}
	return def
}
