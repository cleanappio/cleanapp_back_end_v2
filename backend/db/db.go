package db

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"cleanapp/backend/server/api"
	"cleanapp/backend/util"
	"cleanapp/common"
	"cleanapp/common/disburse"

	"github.com/apex/log"
	ethcommon "github.com/ethereum/go-ethereum/common"
	_ "github.com/go-sql-driver/mysql"
)

func UpdateUser(db *sql.DB, u *api.UserArgs, teamGen func(string) util.TeamColor, disbursers []*disburse.Disburser) (*api.UserResp, error) {
	{
		avRows, err := db.Query("SELECT id FROM users WHERE avatar = ?", u.Avatar)
		if err != nil {
			log.Errorf("Error getting user with avatar %s, %w", u.Avatar, err)
			return nil, err
		}
		defer avRows.Close()

		if avRows.Next() {
			// Check for duplication.
			var id string
			if err := avRows.Scan(&id); err != nil {
				return nil, err
			}
			if id != u.Id {
				return &api.UserResp{
						DupAvatar: true,
					}, fmt.Errorf("duplicated avatar %s for the user %s: avatar already exists for the user %s",
						u.Avatar,
						u.Id,
						id)
			}
		}
	}
	var initialKitn = 0
	{
		idRows, err := db.Query("SELECT id FROM users WHERE id = ?", u.Id)
		if err != nil {
			log.Errorf("Error getting user with id %s, %w", u.Id, err)
			return nil, err
		}
		defer idRows.Close()

		if !idRows.Next() {
			initialKitn = 1
			// No existing user yet, it's a user creation. Sending 1 KITN to the user.
			for _, disburser := range disbursers {
				disburser.DisburseBatch(map[ethcommon.Address]*big.Int{
					ethcommon.HexToAddress(u.Id): disburse.ToWei(float32(initialKitn)),
				})
			}
		}
	}
	team := teamGen(u.Id)

	result, err := db.Exec(`INSERT INTO users (id, avatar, referral, team, kitns_disbursed) VALUES (?, ?, ?, ?, ?)
	                        ON DUPLICATE KEY UPDATE avatar=?, referral=?, team=?`,
		u.Id, u.Avatar, u.Referral, team, initialKitn, u.Avatar, u.Referral, team)

	common.LogResult("updateUser", result, err)

	if err != nil {
		return nil, err
	}
	// Save a copy of counters in a shadow table.
	result, err = db.Exec(`INSERT INTO users_shadow (id, avatar, referral, team, kitns_disbursed) VALUES (?, ?, ?, ?)
	         ON DUPLICATE KEY UPDATE avatar=?, referral=?, team=?`,
		u.Id, u.Avatar, u.Referral, team, initialKitn, u.Avatar, u.Referral, team)
	common.LogResult("update shadow user", result, err)
	return &api.UserResp{
		Team: team,
	}, nil
}

func UpdatePrivacyAndTOC(db *sql.DB, args *api.PrivacyAndTOCArgs) error {
	if args.Privacy != "" && args.AgreeTOC != "" {
		result, err := db.Exec(`UPDATE users
			SET privacy = ?, agree_toc = ?
			WHERE id = ?`, args.Privacy, args.AgreeTOC, args.Id)
		common.LogResult("updatePrivacyAndTOC", result, err)
		return err
	} else if args.Privacy != "" {
		result, err := db.Exec(`UPDATE users
			SET privacy = ?
			WHERE id = ?`, args.Privacy, args.Id)
		common.LogResult("updatePrivacyAndTOC", result, err)
		return err
	} else if args.AgreeTOC != "" {
		result, err := db.Exec(`UPDATE users
			SET agree_toc = ?
			WHERE id = ?`, args.AgreeTOC, args.Id)
		common.LogResult("updatePrivacyAndTOC", result, err)
		return err
	}
	return fmt.Errorf("either privacy or agree_toc should be specified")
}

func SaveReport(db *sql.DB, r api.ReportArgs) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Errorf("Error creating transaction: %w", err)
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT
	  INTO reports (id, team, latitude, longitude, x, y, image)
	  VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Id, util.UserIdToTeam(r.Id), r.Latitude, r.Longitue, r.X, r.Y, r.Image)
	common.LogResult("saveReport", result, err)
	if err != nil {
		log.Errorf("Error inserting report: %w", err)
		return err
	}

	result, err = tx.ExecContext(ctx, `UPDATE users SET kitns_daily = kitns_daily + 1 WHERE id = ?`, r.Id)
	common.LogResult("saveReport", result, err)
	if err != nil {
		log.Errorf("Error update kitns: %w\n", err)
		return err
	}
	// Save a copy of counters in a shadow table.
	tx.ExecContext(ctx, `UPDATE users_shadow SET kitns_daily = kitns_daily + 1 WHERE id = ?`, r.Id)
	return tx.Commit()
}

func GetMap(userId string, m api.ViewPort, retention time.Duration) ([]api.MapResult, error) {
	db, err := common.DBConnect()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Extend the selection rectangle
	latSize := m.LatMax - m.LatMin
	lonSize := m.LonMax - m.LonMin
	m.LatMin -= latSize / 2
	m.LatMax += latSize / 2
	m.LonMin -= lonSize / 2
	m.LonMax += lonSize / 2

	// TODO: Handle 180 meridian inside.
	// Exmaples of rectangles:
	// Zurich 47.3677679,8.5554069 => 47.3602948,8.5766434 top > bottom, left<right
	// Memphis, TN 35.5293051,-90.4510656 => 34.770288,-89.4742701 top > bottom, left < right
	// Madagascar -14.489877, 44.066256 => -26.459353, 52.375980 top > bottom, left < right
	rows, err := db.Query(`
	  SELECT seq, latitude, longitude, team, id
	  FROM reports
	  WHERE latitude > ? AND longitude > ?
	  	AND latitude <= ? AND longitude <= ?
			AND TIMESTAMPDIFF(HOUR, ts, NOW()) <= ?
	`, m.LatMin, m.LonMin, m.LatMax, m.LonMax, retention.Hours())
	if err != nil {
		log.Errorf("Could not retrieve reports: %w", err)
		return nil, err
	}
	defer rows.Close()

	r := make([]api.MapResult, 0, 100)

	for rows.Next() {
		var (
			lat  float64
			lon  float64
			seq  int64
			team util.TeamColor
			id   string
		)
		if err := rows.Scan(&seq, &lat, &lon, &team, &id); err != nil {
			log.Errorf("Cannot scan a row: %w", err)
			continue
		}
		r = append(r, api.MapResult{Latitude: lat, Longitude: lon, Count: 1, ReportID: seq, Team: team, Own: id == userId})
	}
	return r, nil
}

func GetStats(db *sql.DB, id string) (*api.StatsResponse, error) {
	rows, err := db.Query(`
	   SELECT kitns_daily, kitns_disbursed, kitns_ref_daily, kitns_ref_disbursed
	   FROM users
	   WHERE id = ?
	 `, id)
	if err != nil {
		log.Errorf("Could not retrieve number of kittens for user %q: %w", id, err)
		return nil, err
	}
	defer rows.Close()

	kitnsDaily := 0
	kitnsDisbursed := 0
	kitnsRefDaily := 0.0
	kitnsRefDisbursed := 0.0
	err = nil
	if rows.Next() {
		if err := rows.Scan(&kitnsDaily, &kitnsDisbursed, &kitnsRefDaily, &kitnsRefDisbursed); err != nil {
			log.Errorf("Cannot count number of kittens for user %q with error %w", id, err)
		}
	} else {
		log.Errorf("Zero rows counting kittens for user %q, returning 0.", id)
		err = fmt.Errorf("zero rows counting kittens for user %q, returning 0", id)
	}

	return &api.StatsResponse{
		Version:           "2.0",
		Id:                id,
		KitnsDaily:        kitnsDaily,
		KitnsDisbursed:    kitnsDisbursed,
		KitnsRefDaily:     kitnsRefDaily,
		KitnsRefDisbusded: kitnsRefDisbursed,
	}, err
}

func GetTeams() (api.TeamsResponse, error) {
	db, err := common.DBConnect()
	if err != nil {
		return api.TeamsResponse{}, err
	}
	defer db.Close()

	rows, err := db.Query(`
	   SELECT
	     SUM(IF(Team=1,1,0)) AS Blue,
	     SUM(IF(Team=2,1,0)) AS Green
	   FROM reports
	 `) // TODO: Limit the timeline.
	if err != nil {
		log.Errorf("Could not calculate teams stats: %w", err)
		return api.TeamsResponse{}, err
	}
	defer rows.Close()

	blue, green := 0, 0
	err = nil
	if rows.Next() {
		if err := rows.Scan(&blue, &green); err != nil {
			log.Errorf("Cannot count team stats with error %w", err)
		}
	} else {
		log.Error("Zero rows counting team stats, returning 0s.")
		err = fmt.Errorf("zero rows counting team stats returning 0s")
	}

	return api.TeamsResponse{
		Blue:  blue,
		Green: green,
	}, err
}

func GetTopScores(db *sql.DB, args *api.BaseArgs, topCount int) (*api.TopScoresResponse, error) {
	rows, err := db.Query(`
		SELECT id, avatar, kitns_daily + kitns_disbursed + kitns_ref_daily + kitns_ref_disbursed AS cnt
		FROM users
		ORDER BY cnt DESC
		LIMIT ?`, topCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := &api.TopScoresResponse{
		Records: []api.TopScoresRecord{},
	}
	i := 1
	hasYou := false
	for rows.Next() {
		var id, avatar string
		var cnt float64

		if err := rows.Scan(&id, &avatar, &cnt); err != nil {
			return nil, err
		}
		ret.Records = append(ret.Records, api.TopScoresRecord{
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
		SELECT id, avatar, kitns_daily + kitns_disbursed + kitns_ref_daily + kitns_ref_disbursed AS cnt
		FROM users
		WHERE id = ?`, args.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var id, avatar string
		var cnt float64
		if err := rows.Scan(&id, &avatar, &cnt); err != nil {
			return nil, err
		}
		you := api.TopScoresRecord{
			Title: avatar,
			Kitn:  cnt,
			IsYou: true,
		}
		newRows, err := db.Query(`
			SELECT count(*) AS c
			FROM users
			WHERE kitns_daily + kitns_disbursed + kitns_ref_daily + kitns_ref_disbursed > ?
		`, cnt)
		if err != nil {
			return nil, err
		}
		defer newRows.Close()
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

func ReadReport(db *sql.DB, args *api.ReadReportArgs) (*api.ReadReportResponse, error) {
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

	const shareData = "share_data_live"

	var (
		id      string
		image   []byte
		avatar  string
		privacy string
	)

	// Take only the first row. Ignore others as duplicates are not expected.
	if !rows.Next() {
		return nil, fmt.Errorf("report %d wasn't found", args.Seq)
	}

	if err := rows.Scan(&id, &image, &avatar, &privacy); err != nil {
		return nil, err
	}

	ret := &api.ReadReportResponse{
		Id:    id,
		Image: image,
	}

	if privacy == shareData || id == args.Id {
		ret.Avatar = avatar
	}

	if id == args.Id {
		ret.Own = true
	}

	return ret, nil
}

func ReadReferral(db *sql.DB, key string) (string, error) {
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

func WriteReferral(db *sql.DB, key, value string) error {
	existing, err := ReadReferral(db, key)
	if err != nil {
		return err
	}

	// If the referral already exists then just return without inserting
	if existing != "" {
		return nil
	}

	result, err := db.Exec(`INSERT
	  INTO referrals (refkey, refvalue)
	  VALUES (?, ?)`,
		key, value)

	common.LogResult("writeReferral", result, err)

	return err
}

func GenerateReferral(db *sql.DB, req *api.GenRefRequest, codeGen func() string) (*api.GenRefResponse, error) {
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
		return &api.GenRefResponse{
			RefValue: refCode,
		}, nil
	}

	refCode = codeGen()

	result, err := db.Exec(`INSERT
		INTO users_refcodes (id, referral)
		VALUES (?, ?)`,
		req.Id, refCode)
	common.LogResult("generteReferral", result, err)

	if err != nil {
		return nil, err
	}

	return &api.GenRefResponse{
		RefValue: refCode,
	}, nil
}

func CleanupReferral(db *sql.DB, ref string) error {
	result, err := db.Exec(`DELETE
		FROM referrals
		WHERE refvalue = ?`, ref)
	common.LogResult("cleanupReferral", result, err)

	if err != nil {
		log.Errorf("Error cleaning up referral, %w", err)
		return err
	}
	return nil
}
