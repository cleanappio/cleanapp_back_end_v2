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
	mysqlPassword = flag.String("mysql_password", "secret", "MySQL password.")
	mysqlHost     = flag.String("mysql_host", "localhost", "MySQL host.")
	mysqlPort     = flag.String("mysql_port", "3306", "MySQL port.")
	mysqlDb       = flag.String("mysql_db", "cleanapp", "MySQL database to use.")
)

func mysqlAddress() string {
	db := fmt.Sprintf("server:%s@tcp(%s:%s)/%s", *mysqlPassword, *mysqlHost, *mysqlPort, *mysqlDb)
	return db
}

func validateResult(r sql.Result, e error, checkRowsAffected bool) error {
	if e != nil {
		log.Printf("Query failed: %v", e)
		return e
	}
	rows, err := r.RowsAffected()
	if err != nil {
		log.Printf("Failed to get status of db op: %s", err)
		return err
	}
	if checkRowsAffected && rows != 1 {
		m := fmt.Sprintf("Expected to affect 1 row, affected %d", rows)
		log.Print(m)
		return fmt.Errorf(m)
	}
	return nil
}

func updateUser(db *sql.DB, u *UserArgs, teamGen func(string) TeamColor) (*UserResp, error) {
	log.Printf("Write: Trying to create or update user %s / %s", u.Id, u.Avatar)
	rows, err := db.Query("SELECT id FROM users WHERE avatar = ?", u.Avatar)
	if err != nil {
		log.Printf("Couldn't get user with avatar %s", u.Avatar)
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != u.Id {
			return &UserResp{
					DupAvatar: true,
				}, fmt.Errorf("duplicated avatar %s for the user %s: avatar already exists for the user %s",
					u.Avatar,
					u.Id,
					id)
		}
	}

	team := teamGen(u.Id)

	result, err := db.Exec(`INSERT INTO users (id, avatar, referral, team) VALUES (?, ?, ?, ?)
	                        ON DUPLICATE KEY UPDATE avatar=?, referral=?, team=?`,
		u.Id, u.Avatar, u.Referral, team, u.Avatar, u.Referral, team)

	err = validateResult(result, err, false)
	if err != nil {
		return nil, err
	}
	return &UserResp{
		Team: team,
	}, nil
}

func updatePrivacyAndTOC(db *sql.DB, args *PrivacyAndTOCArgs) error {
	log.Printf("Writing privacy and TOC %v", args)

	if args.Privacy != "" && args.AgreeTOC != "" {
		result, err := db.Exec(`UPDATE users
			SET privacy = ?, agree_toc = ?
			WHERE id = ?`, args.Privacy, args.AgreeTOC, args.Id)
		return validateResult(result, err, false)
	} else if args.Privacy != "" {
		result, err := db.Exec(`UPDATE users
			SET privacy = ?
			WHERE id = ?`, args.Privacy, args.Id)
		return validateResult(result, err, false)
	} else if args.AgreeTOC != "" {
		result, err := db.Exec(`UPDATE users
			SET agree_toc = ?
			WHERE id = ?`, args.AgreeTOC, args.Id)
		return validateResult(result, err, false)
	}
	return fmt.Errorf("either privacy or agree_toc should be specified")
}

func saveReport(r ReportArgs) error {
	log.Printf("Write: Trying to save report from user %s to db located at %f,%f", r.Id, r.Latitude, r.Longitue)
	db, err := common.DBConnect(mysqlAddress())
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := db.Exec(`INSERT
	  INTO reports (id, team, latitude, longitude, x, y, image)
	  VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Id, userIdToTeam(r.Id), r.Latitude, r.Longitue, r.X, r.Y, r.Image)

	return validateResult(result, err, true)
}

func getMap(m ViewPort) ([]MapResult, error) {
	log.Printf("Write: Trying to map/coordinates from db in %f,%f:%f,%f", m.LatMin, m.LonMin, m.LatMax, m.LonMax)
	db, err := common.DBConnect(mysqlAddress())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// TODO: Limit the time scope, say, last  week. Or make it a parameter.
	// TODO: Handle 180 meridian inside.
	// Exmaples of rectangles:
	// Zurich 47.3677679,8.5554069 => 47.3602948,8.5766434 top > bottom, left<right
	// Memphis, TN 35.5293051,-90.4510656 => 34.770288,-89.4742701 top > bottom, left < right
	// Madagascra -14.489877, 44.066256 => -26.459353, 52.375980 top > bottom, left < right
	rows, err := db.Query(`
	  SELECT seq, latitude, longitude, team
	  FROM reports
	  WHERE latitude > ? AND longitude > ?
	  	AND latitude <= ? AND longitude <= ?
	`, m.LatMin, m.LonMin, m.LatMax, m.LonMax)
	if err != nil {
		log.Printf("Could not retrieve reports: %v", err)
		return nil, err
	}
	defer rows.Close()

	r := make([]MapResult, 0, 100)

	for rows.Next() {
		var (
			lat  float64
			lon  float64
			seq  int64
			team TeamColor
		)
		if err := rows.Scan(&seq, &lat, &lon, &team); err != nil {
			log.Printf("Cannot scan a row: %v", err)
			continue
		}
		r = append(r, MapResult{Latitude: lat, Longitude: lon, Count: 1, ReportID: seq, Team: team})
	}
	return r, nil
}

func getStats(id string) (StatsResponse, error) {
	log.Printf("Write: Trying to get stats for user %s", id)
	db, err := common.DBConnect(mysqlAddress())
	if err != nil {
		return StatsResponse{}, err
	}
	defer db.Close()

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

	return StatsResponse{
		Version: "2.0",
		Id:      id,
		Kittens: cnt,
	}, err
}

func getTeams() (TeamsResponse, error) {
	log.Printf("Write: Trying to get teams results")
	db, err := common.DBConnect(mysqlAddress())
	if err != nil {
		return TeamsResponse{}, err
	}
	defer db.Close()

	rows, err := db.Query(`
	   SELECT
	     SUM(IF(Team=1,1,0)) AS Blue,
	     SUM(IF(Team=2,1,0)) AS Green
	   FROM reports
	 `) // TODO: Limit the timeline.
	if err != nil {
		log.Printf("Could not calculate teams stats: %v", err)
		return TeamsResponse{}, err
	}
	defer rows.Close()

	blue, green := 0, 0
	err = nil
	if rows.Next() {
		if err := rows.Scan(&blue, &green); err != nil {
			log.Printf("Cannot count team stats with error %v", err)
		}
	} else {
		log.Printf("Zero rows counting team stats, returning 0s.")
		err = fmt.Errorf("zero rows counting team stats returning 0s")
	}

	return TeamsResponse{
		Blue:  blue,
		Green: green,
	}, err
}

func getTopScores(db *sql.DB, args *BaseArgs, topCount int) (*TopScoresResponse, error) {
	rows, err := db.Query(`
		SELECT u.id, u.avatar, count(*) AS cnt
		FROM reports r JOIN users u ON r.id = u.id
		GROUP BY u.id
		ORDER BY cnt DESC
		LIMIT ?`, topCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := &TopScoresResponse{
		Records: []TopScoresRecord{},
	}
	i := 1
	hasYou := false
	for rows.Next() {
		var id, avatar string
		var cnt int

		if err := rows.Scan(&id, &avatar, &cnt); err != nil {
			return nil, err
		}
		ret.Records = append(ret.Records, TopScoresRecord{
			Place: i,
			Title: avatar,
			Kitn:  cnt,
			IsYou: id == args.Id,
		})
		i += 1
		if id == args.Id {
			hasYou = true
		}
	}

	// If the list contains the user, we are done, no need to fetch user's stats.
	if hasYou {
		return ret, nil
	}

	rows, err = db.Query(`
		SELECT u.id, u.avatar, count(*) AS cnt
		FROM reports r RIGHT OUTER JOIN users u ON r.id = u.id
		WHERE u.id = ?
		GROUP BY u.id`, args.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var id, avatar string
		var cnt int
		if err := rows.Scan(&id, &avatar, &cnt); err != nil {
			return nil, err
		}
		you := TopScoresRecord{
			Title: avatar,
			Kitn:  cnt,
			IsYou: true,
		}
		newRows, err := db.Query(`
			SELECT count(*) AS c
			FROM(
				SELECT id, count(*) AS cnt
				FROM reports r
				GROUP BY id
				HAVING cnt > ?
			) AS t
		`, cnt)
		if err != nil {
			return nil, err
		}
		if newRows.Next() {
			var yourCnt int
			if err := newRows.Scan(&yourCnt); err != nil {
				return nil, err
			}
			you.Place = yourCnt + 1
			if yourCnt < topCount {
				you.Place = topCount + 1
			}
		}
		ret.Records = append(ret.Records, you)
	}
	return ret, nil
}

func readReport(db *sql.DB, args *ReadReportArgs) (*ReadReportResponse, error) {
	log.Printf("Read: Getting the report %d\n", args.Seq)

	rows, err := db.Query(`SELECT
		r.id, r.image, u.avatar, u.privacy
		FROM reports AS r
		JOIN users AS u
		ON r.id = u.id
		WHERE r.seq = ?`,
		args.Seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	const shareData = "sharing_data_live"

	var (
		id      string
		image   []byte
		avatar  string
		privacy string
	)

	// Take only the first row. Ignore others as duplicates are not expected.
	if !rows.Next() {
		return nil, fmt.Errorf("Report %d wasn't found", args.Seq)
	}

	if err := rows.Scan(&id, &image, &avatar, &privacy); err != nil {
		return nil, err
	}

	ret := &ReadReportResponse{
		Id:    id,
		Image: image,
	}

	if privacy == shareData {
		ret.Avatar = avatar
	}

	return ret, nil
}

func readReferral(db *sql.DB, key string) (string, error) {
	log.Printf("Read: retrieving the referral code for the device %s\n", key)

	rows, err := db.Query(`SELECT refvalue
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

func writeReferral(db *sql.DB, key, value string) error {
	log.Printf("Write: Trying to save the referral from device %s with value %s\n", key, value)

	existing, err := readReferral(db, key)
	if err != nil {
		return err
	}

	// If the referral already exists then just return without inserting
	if existing != "" {
		return nil
	}

	_, err = db.Exec(`INSERT
	  INTO referrals (refkey, refvalue)
	  VALUES (?, ?)`,
		key, value)

	return err
}

func generateReferral(db *sql.DB, req *GenRefRequest, codeGen func() string) (*GenRefResponse, error) {
	log.Printf("Generate and store referral code for the user %s", req.Id)

	rows, err := db.Query(`SELECT referral
		FROM users_refcodes
		WHERE id = ?`,
		req.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refCode string
	// Take only the first row. Ignore others as duplicates are not expected.
	if rows.Next() {
		if err := rows.Scan(&refCode); err != nil {
			return nil, err
		}
		return &GenRefResponse{
			RefValue: refCode,
		}, nil
	}

	refCode = codeGen()

	if _, err := db.Exec(`INSERT
		INTO users_refcodes (id, referral)
		VALUES (?, ?)`,
		req.Id, refCode); err != nil {
		return nil, err
	}

	return &GenRefResponse{
		RefValue: refCode,
	}, nil
}

func cleanupReferral(db *sql.DB, ref string) error {
	log.Printf("Cleaning up referral %s\n", ref)

	if _, err := db.Exec(`DELETE
		FROM referrals
		WHERE refvalue = ?`, ref); err != nil {
		log.Printf("Error cleaning up referral, %v\n", err)
		return err
	}
	return nil
}
