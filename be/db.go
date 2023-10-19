package be

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func saveReport(r ReportArgs) error {
	log.Printf("Write: Trying to save report from user %s to db located at %f,%f", r.Id, r.Lattitude, r.Longitue)
	db, err := sql.Open("mysql", "server:dev_pass@tcp(mysql)/cleanapp")
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return err
	}
	// See "Important settings" section.
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	// TODO: Finish actually writing to the database.

	log.Printf("Write: code %v", err)
	return err
}
