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

func updatePrivacyAndTOC(db *sql.DB, args *PrivacyAndTOCArgs) error {
	log.Printf("Writing privacy and TOC %v", args)

	if args.Privacy != "" && args.AgreeTOC != "" {
		result, err := db.Exec(`UPDATE users
			SET privacy = ?, agree_toc = ?
			WHERE id = ?`, args.Privacy, args.AgreeTOC, args.Id)
		return validateResult(result, err)
	} else if args.Privacy != "" {
		result, err := db.Exec(`UPDATE users
			SET privacy = ?
			WHERE id = ?`, args.Privacy, args.Id)
		return validateResult(result, err)
	} else if args.AgreeTOC != "" {
		result, err := db.Exec(`UPDATE users
			SET agree_toc = ?
			WHERE id = ?`, args.AgreeTOC, args.Id)
		return validateResult(result, err)
	}
	return fmt.Errorf("either privacy or agree_toc should be specified")
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

func getMap(m ViewPort) ([]MapResult, error) {
	log.Printf("Write: Trying to map/coordinates from db in %f,%f:%f,%f", m.LatTop, m.LonLeft, m.LatBottom, m.LonRight)
	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		return nil, err
	}
	log.Printf("%f:%f to %f:%f", m.LatTop, m.LonLeft, m.LatBottom, m.LonRight)
	//latw := m.LatW / steps
	//lonw := m.LonW / steps

	// TODO: Limit the time scope, say, last  week. Or make it a parameter.
	rows, err := db.Query(`
	  SELECT latitude, longitude
	  FROM reports
	  WHERE latitude > ? AND longitude > ?
	  	AND latitude <= ? AND longitude <= ?
	`, m.LatTop, m.LonLeft, m.LatBottom, m.LonRight)
	if err != nil {
		log.Printf("Could not retrieve reports: %v", err)
		return nil, err
	}
	defer rows.Close()

	r := make([]MapResult, 0, 100)

	for rows.Next() {
		var (
			lat float64
			lon float64
		)
		if err := rows.Scan(&lat, &lon); err != nil {
			log.Printf("Cannot scan a row with error %v", err)
			continue
		}
		log.Printf("%f:%f", lat, lon)
		r = append(r, MapResult{Latitude: lat, Longitude: lon, Count: 1})
	}
	return r, nil
}
