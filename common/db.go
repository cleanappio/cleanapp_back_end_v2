package common

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func DBConnect(mysqlAddress string) (*sql.DB, error) {
	db, err := sql.Open("mysql", mysqlAddress)
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
