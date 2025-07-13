package logic

import (
	"cleanapp/email_sender/email"
	"database/sql"
	"fmt"
	"log"
)

type Report struct {
	Seq   int
	Lat   float64
	Lon   float64
	Image []byte
}

type Contact struct {
	AreaID int
	Email  string
}

/* func CheckAndSendReports(db *sql.DB, mailer *email.Mailer) {
	query := `
	  SELECT ai.area_id, c.email, r.seq, r.latitude, r.longitude, r.image
	  FROM reports r
	  JOIN area_index ai ON ST_Within(ST_SRID(POINT(r.longitude, r.latitude), 4326), ai.geom)
	  JOIN contact_emails c ON ai.area_id = c.area_id
	  WHERE r.report_sent = false
	  ORDER BY ai.area_id
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("Failed to query reports: %v", err)
	}
	defer rows.Close()

	type AreaData struct {
		Reports []Report
		Emails  map[string]bool
	}

	areas := map[int]*AreaData{}

	for rows.Next() {
		var areaID int
		var emailAddr string
		var r Report
		if err := rows.Scan(&areaID, &emailAddr, &r.Seq, &r.Lat, &r.Lon, &r.Image); err != nil {
			log.Printf("Skipping bad row: %v", err)
			continue
		}

		if _, ok := areas[areaID]; !ok {
			areas[areaID] = &AreaData{Reports: []Report{}, Emails: map[string]bool{}}
		}
		areas[areaID].Reports = append(areas[areaID].Reports, r)
		areas[areaID].Emails[emailAddr] = true
	}

	for areaID, data := range areas {
		if len(data.Reports) < 20 {
			continue
		}

		recipients := []string{}
		for e := range data.Emails {
			recipients = append(recipients, e)
		}

		if len(data.Reports) > 0 {
			log.Printf(" Sending %d reports to area %d", len(data.Reports), areaID)
			// dummy map image
			mapImage := []byte("dummy-map")
			mailer.SendEmails(recipients, data.Reports[0].Image, mapImage)

			markAsSent(db, data.Reports)
		}
	}
}
*/

func CheckAndSendReports(db *sql.DB, mailer *email.Mailer) {
	log.Println(" Starting report scan...")

	query := `
	  SELECT ai.area_id, c.email, r.seq, r.latitude, r.longitude, r.image
      FROM reports_copy r
      JOIN area_index ai ON MBRWithin(r.geom, ai.geom)
      JOIN contact_emails c ON ai.area_id = c.area_id
      WHERE r.report_sent IS NULL OR r.report_sent = 0
      ORDER BY ai.area_id
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf(" Failed to query reports: %v", err)
	}
	defer rows.Close()

	log.Println(" Collecting report data...")

	type AreaData struct {
		Reports []Report
		Emails  map[string]bool
	}

	areas := map[int]*AreaData{}
	countRows := 0
	for rows.Next() {
		var areaID int
		var emailAddr string
		var r Report
		if err := rows.Scan(&areaID, &emailAddr, &r.Seq, &r.Lat, &r.Lon, &r.Image); err != nil {
			log.Printf(" Skipping bad row: %v", err)
			continue
		}

		countRows++
		if _, ok := areas[areaID]; !ok {
			areas[areaID] = &AreaData{Reports: []Report{}, Emails: map[string]bool{}}
		}
		areas[areaID].Reports = append(areas[areaID].Reports, r)
		areas[areaID].Emails[emailAddr] = true
	}

	log.Printf(" Loaded %d rows into %d area groups\n", countRows, len(areas))

	for areaID, data := range areas {
		log.Printf(" Area %d: %d reports, %d recipients", areaID, len(data.Reports), len(data.Emails))

		if len(data.Reports) < 20 {
			log.Printf(" Skipping area %d — not enough reports (%d < 20)", areaID, len(data.Reports))
			continue
		}

		recipients := []string{}
		for e := range data.Emails {
			recipients = append(recipients, e)
		}

		log.Printf(" Sending email to area %d with %d reports to %d recipients", areaID, len(data.Reports), len(recipients))

		mapImage := []byte("dummy-map")
		mailer.SendEmails(recipients, data.Reports[0].Image, mapImage)

		markAsSent(db, data.Reports)

		log.Printf(" Reports marked as sent for area %d\n", areaID)
	}

}

func markAsSent(db *sql.DB, reports []Report) {
	for _, r := range reports {
		_, err := db.Exec("UPDATE reports SET report_sent = true WHERE seq = ?", r.Seq)
		if err != nil {
			fmt.Printf("Failed to mark report %d as sent: %v\n", r.Seq, err)
		}
	}
}
