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


func getStats(id string) (StatsResponse, error) {
	log.Printf("Write: Trying to get stats for user %s", id)
	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		return StatsResponse{}, err
	}

	rows, err := db.Query(`
	   SELECT COUNT(*)
	   FROM reports
	   WHERE id = ?
	 `, id)
	if err != nil {
	 	log.Printf("Could not retrieve number of kittens for user %q: %v", id, err)
	 	return StatsResponse{}, err
	}
	defer rows.Close()

	cnt := 0
	err = nil
	if rows.Next() {
		if err := rows.Scan(&cnt); err != nil {
			log.Printf("Cannot count number of kittens for user %q with error %v", id, err)
		}
	} else {
		log.Printf("Zero rows counting kittens for user %q, returning 0.", id)
		err = fmt.Errorf("zero rows counting kittens for user %q, returning 0", id)
	}

	return StatsResponse {
		Version: "2.0",
		Id: id,
		Kittens: cnt,
	}, err
}
