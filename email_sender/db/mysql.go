package db

import (
	"cleanapp/email_sender/models"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

var (
	DBUser     = flag.String("db_user", "", "Database username")
	DBPassword = flag.String("db_password", "", "Database password")
	DBHost     = flag.String("db_host", "", "Database host")
	DBPort     = flag.String("db_port", "", "Database port")
	DBName     = flag.String("db_name", "", "Database name")
)

/*func Connect() *sql.DB {
	dsn := "username:password@tcp(localhost:3306)/yourdb"


	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf(" DB connection failed: %v", err)
	}
	return db
}
*/

func Connect() *sql.DB {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf(" DB connection failed: %v", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatalf(" DB ping failed: %v", err)
	}

	log.Println(" Connected to MySQL!")

	_, err = db.Exec("SET SESSION sort_buffer_size = 1024 * 1024 * 10") // 10MB
	if err != nil {
		log.Fatal("Failed to set sort_buffer_size:", err)
	}

	return db
}

func GetAreasWith20Reports(db *sql.DB) []int {
	rows, err := db.Query(`
		SELECT area_id
		FROM report_to_area
		GROUP BY area_id
		HAVING COUNT(*) >= 20
	`)
	if err != nil {
		log.Fatalf(" Failed to query areas: %v", err)
	}
	defer rows.Close()

	var areaIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			areaIDs = append(areaIDs, id)
		}
	}
	return areaIDs
}

func GetReportsForArea(areaID int, db *sql.DB) []models.Report {
	rows, err := db.Query(`
		SELECT r.seq, r.latitude, r.longitude, r.ts, r.team, r.action_id
		FROM reports r
		JOIN report_to_area rta ON r.seq = rta.report_seq
		WHERE rta.area_id = ?
	`, areaID)
	if err != nil {
		log.Printf(" Failed to get reports for area %d: %v", areaID, err)
		return nil
	}
	defer rows.Close()

	reports := []models.Report{}
	for rows.Next() {
		r := models.Report{}
		if err := rows.Scan(&r.Seq, &r.Latitude, &r.Longitude, &r.TS, &r.Team, &r.ActionID); err == nil {
			reports = append(reports, r)
		}
	}
	return reports
}

func GetContactEmail(areaID int, db *sql.DB) models.Contact {
	row := db.QueryRow("SELECT area_id, email, consent_report FROM contact_emails WHERE area_id = ?", areaID)
	var c models.Contact
	err := row.Scan(&c.AreaID, &c.Email, &c.ConsentReport)
	if err != nil {
		log.Printf(" Failed to get contact email for area %d: %v", areaID, err)
	}
	return c
}

func AlreadySent(areaID int, db *sql.DB) bool {
	row := db.QueryRow("SELECT 1 FROM report_packages_sent WHERE area_id = ?", areaID)
	var exists int
	return row.Scan(&exists) == nil
}

func MarkAsSent(areaID int, db *sql.DB) {
	_, err := db.Exec("INSERT INTO report_packages_sent (area_id) VALUES (?)", areaID)
	if err != nil {
		log.Printf(" Failed to mark area %d as sent: %v", areaID, err)
	}
}
