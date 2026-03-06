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
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
}

func DBConnect(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", mysqlAddress(cfg))
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil, err
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	deadline := time.Now().Add(30 * time.Second)
	waitInterval := time.Second
	for {
		if err := db.Ping(); err == nil {
			log.Println("Established db connection.")
			return db, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database ping timeout")
		}
		time.Sleep(waitInterval)
		if waitInterval < 4*time.Second {
			waitInterval *= 2
		}
	}
}
