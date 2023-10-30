package be

import (
	"database/sql"
	"flag"
	"fmt"
	"log"

	"cleanapp/common"

	_ "github.com/go-sql-driver/mysql"
)

const (
	steps = 10 // Steps in geo coordinates withoing a geo port. It seems not practical to have more that 10x10 spots on such a small screen.
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

func makeCoord(v float64, base, step float64) int64 {
	r := (v-step)/step
	if r  < 0 || r > steps {
		log.Printf("%v is outside of 10*%v from %v", v, step, base)
		return 0.0
	}
	return int64(r)
}

func getMap(m MapArgs) ([]*MapResult, error) {
	log.Printf("Write: Trying to map/coordinates from user %s from db around %f,%f", m.Id, m.Latitude, m.Longitue)
	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		return nil, err
	}
	late := m.Latitude + m.Length
	lone := m.Longitue + m.Width
	latw := m.Length / steps
	lonw := m.Width / steps

	// TODO: Limit the time scope, say, last  week. Or make it a parameter.
	rows, err := db.Query(`
	  SELECT latitude, longitude
	  FROM reports
	  WHERE latitude > ? AND longitude > ?
	  	AND latitude < ? AND longitude < ?
	`, m.Latitude, m.Longitue, late, lone)
	if err != nil {
		log.Printf("Could not retrieve reports: %v", err)
		return nil, err
	}
	defer rows.Close()

	// TODO: Do not return more than 100, the client may break,
	// and the user will not percive so many results.
	r := make(map[int64]*MapResult)

	for rows.Next() {
		var (
			lat   float64
			lon   float64
		)
		if err := rows.Scan(&lat, &lon); err != nil {
			log.Printf("Cannot scan a row with error %v",  err) 
			continue
		}
		lt := makeCoord(lat, m.Latitude, latw)
		ln := makeCoord(lon, m.Longitue, lonw)
		ndx := lt*1000 + ln
		if _, ok := r[ndx]; !ok {
			r[ndx] = &MapResult{
				Latitude: lat + float64(lt)/5,
				Longitude: lon + float64(ln)/5,
				Count: 0,
			}
		}
		r[ndx].Count += 1
	}
	
	log.Printf("%d lines found", len(r))
	var rr []*MapResult
	for _, ri := range r {
		rr = append(rr, ri)
	}

	return rr, nil
}
