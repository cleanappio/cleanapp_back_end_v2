package backend

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func dbConnect(mysqlAddress string) (*sql.DB, error) {
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

type sqlDB struct {
	db *sql.DB
}

func (s *sqlDB) updateUser(u UserArgs) error {
	log.Printf("Write: Trying to create or update user %s / %s", u.Id, u.Avatar)

	result, err := s.db.Exec(`INSERT INTO users (id, avatar) VALUES (?, ?)
	                        ON DUPLICATE KEY UPDATE avatar=?`,
		u.Id, u.Avatar, u.Avatar)

	return validateResult(result, err)
}

func (s *sqlDB) saveReport(r ReportArgs) error {
	log.Printf("Write: Trying to save report from user %s to db located at %f,%f", r.Id, r.Latitude, r.Longitue)

	result, err := s.db.Exec(`INSERT
	  INTO reports (id, latitude, longitude, x, y, image)
	  VALUES (?, ?, ?, ?, ?, ?)`,
		r.Id, r.Latitude, r.Longitue, r.X, r.Y, r.Image)

	return validateResult(result, err)
}

func (s *sqlDB) getMap(m ViewPort) ([]MapResult, error) {
	log.Printf("Write: Trying to map/coordinates from db in %f,%f:%f,%f", m.LatTop, m.LonLeft, m.LatBottom, m.LonRight)

	// TODO: Limit the time scope, say, last  week. Or make it a parameter.
	rows, err := s.db.Query(`
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

func (s *sqlDB) readReferral(key string) (string, error) {
	log.Printf("Read: retrieving the referral code for the device %s\n", key)

	rows, err := s.db.Query(`SELECT refvalue
		FROM referrals
		WHERE refkey = ?`,
		key)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var value string
	// Take only the first row. Ignore others as duplicates are not expected.
	if !rows.Next() {
		return "", nil
	}
	if err := rows.Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func (s *sqlDB) writeReferral(key, value string) error {
	log.Printf("Write: Trying to save the referral from device %s with value %s\n", key, value)

	existing, err := s.readReferral(key)
	if err != nil {
		return err
	}

	// If the referral already exists then just return without inserting
	if existing != "" {
		return nil
	}

	_, err = s.db.Exec(`INSERT
	  INTO referrals (refkey, refvalue)
	  VALUES (?, ?)`,
		key, value)

	return err
}
