package be

import (
	"database/sql"
	"flag"
	"fmt"
	"log"

	"cleanapp/common"

	_ "github.com/go-sql-driver/mysql"
)

var (
	// mysqlAddress = flag.String("mysql_address", "server:dev_pass@tcp(localhost:33060)/cleanapp", "MySQL address string")
	mysqlAddress = flag.String("mysql_address", "server:dev_pass@tcp(cleanupdb:3306)/cleanapp", "MySQL address string")
)

func validateResult(r sql.Result, e error) error {
	if e != nil {
		log.Printf("Query failed: %v", e)
		return e
	}
	rows, err := r.RowsAffected()
	if err != nil {
		log.Printf("Failed to get status of db op: %s", err)
		return err
	}
	if rows != 1 {
		m := fmt.Sprintf("Expected to affect 1 row, affected %d", rows)
		log.Print(m)
		return fmt.Errorf(m)
	}
	return nil
}

func updateUser(u UserArgs) error {
	log.Printf("Write: Trying to create or update user %s / %s", u.Id, u.Avatar)
	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		return err
	}

	result, err := db.Exec(`INSERT INTO users (id, avatar) VALUES (?, ?)
	                        ON DUPLICATE KEY UPDATE avatar=?`,
		u.Id, u.Avatar, u.Avatar)

	return validateResult(result, err)
}

func saveReport(r ReportArgs) error {
	log.Printf("Write: Trying to save report from user %s to db located at %f,%f", r.Id, r.Latitude, r.Longitue)
	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		return err
	}

	result, err := db.Exec(`INSERT
	  INTO reports (id, latitude, longitude, x, y, image)
	  VALUES (?, ?, ?, ?, ?, ?)`,
		r.Id, r.Latitude, r.Longitue, r.X, r.Y, r.Image)

	return validateResult(result, err)
}
