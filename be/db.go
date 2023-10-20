package be

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

/*
TODO: Open connection once and then resuse it on every call. Soemthing like that:
	var (
		db *sql.DB
	)

	func init() {
		db, err := sql.Open("mysql", "server:dev_pass@tcp(mysql)/cleanapp")
		if err != nil {
			log.Printf("Failed to connect to the database: %v", err)
			// TODO: Panic?
		}
		db.SetConnMaxLifetime(time.Minute * 3)
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(10)
		log.Println("Established db connection.")
	}
*/

func saveReport(r ReportArgs) error {
	log.Printf("Write: Trying to save report from user %s to db located at %f,%f", r.Id, r.Lattitude, r.Longitue)
	db, err := sql.Open("mysql", "server:dev_pass@tcp(mysql)/cleanapp")
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return err
	}
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	log.Println("Established db connection.")

	result, err := db.Exec(`INSERT
	  INTO reports (id, lattitude, longitude, x, y, image)
	  VALUES (?, ?, ?, ?, ?, ?)`,
	  r.Id, r.Lattitude, r.Longitue, r.X, r.Y, r.Image)
	if err != nil {
		log.Fatal(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		log.Fatal(err)
	}
	if rows != 1 {
		log.Fatalf("expected to affect 1 row, affected %d", rows)
	}

	log.Printf("Write: code %v", err)
	return err
}
