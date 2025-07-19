package utils

import (
	"areas-service/config"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func mysqlAddress(cfg *config.Config) string {
	db := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
	return db
}

func DBConnect() (*sql.DB, error) {
	cfg := config.Load()

	db, err := sql.Open("mysql", mysqlAddress(cfg))
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil, err
	}
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	log.Println("Established db connection.")
	return db, err
}
