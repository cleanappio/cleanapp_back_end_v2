package common

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var (
	mysqlPassword = flag.String("mysql_password", "secret", "MySQL password.")
	mysqlHost     = flag.String("mysql_host", "localhost", "MySQL host.")
	mysqlPort     = flag.String("mysql_port", "3306", "MySQL port.")
	mysqlDb       = flag.String("mysql_db", "cleanapp", "MySQL database to use.")
)

func mysqlAddress() string {
	db := fmt.Sprintf("server:%s@tcp(%s:%s)/%s", *mysqlPassword, *mysqlHost, *mysqlPort, *mysqlDb)
	return db
}

func DBConnect() (*sql.DB, error) {
	db, err := sql.Open("mysql", mysqlAddress())
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil, err
	}

	maxOpen := envInt([]string{"CLEANAPP_SERVICE_DB_MAX_OPEN_CONNS", "DB_MAX_OPEN_CONNS"}, 25)
	maxIdle := envInt([]string{"CLEANAPP_SERVICE_DB_MAX_IDLE_CONNS", "DB_MAX_IDLE_CONNS"}, 10)
	connMaxLifetimeMin := envInt([]string{"CLEANAPP_SERVICE_DB_CONN_MAX_LIFETIME_MIN", "DB_CONN_MAX_LIFETIME_MIN"}, 5)
	pingMaxWaitSec := envInt([]string{"CLEANAPP_SERVICE_DB_PING_MAX_WAIT_SEC", "DB_PING_MAX_WAIT_SEC"}, 60)

	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		db.SetMaxIdleConns(maxIdle)
	}
	if connMaxLifetimeMin > 0 {
		db.SetConnMaxLifetime(time.Duration(connMaxLifetimeMin) * time.Minute)
	}

	deadline := time.Now().Add(time.Duration(pingMaxWaitSec) * time.Second)
	waitInterval := time.Second
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr := db.PingContext(ctx)
		cancel()
		if pingErr == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database ping timeout after %ds: %w", pingMaxWaitSec, pingErr)
		}
		log.Printf("Database connection failed, retrying in %v: %v", waitInterval, pingErr)
		time.Sleep(waitInterval)
		waitInterval *= 2
		if waitInterval > 30*time.Second {
			waitInterval = 30 * time.Second
		}
	}

	log.Printf("Established db connection pool: open=%d idle=%d max_lifetime_min=%d", maxOpen, maxIdle, connMaxLifetimeMin)
	return db, err
}

func envInt(keys []string, defaultValue int) int {
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
	return defaultValue
}
