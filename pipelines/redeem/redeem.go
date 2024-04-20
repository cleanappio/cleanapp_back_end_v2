package redeem

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/apex/log"
)

func Redeem(db *sql.DB) (int, int, error) {
	// Get users who joined with referral codes
	rows, err := db.Query(`
	  SELECT id, referral, kitns_daily + kitns_disbursed - kitns_ref_redeemed AS kitns_to_refer
	  FROM users
	  WHERE referral != ''
		AND kitns_daily + kitns_disbursed - kitns_ref_redeemed > 0
	`)
	if err != nil {
		log.Errorf("Error reading users referred by others, %w", err)
		return 0, 0, err
	}
	defer rows.Close()

	successRows := 0
	failRows := 0
	for rows.Next() {
		var (
			id           string
			referral     string
			kitnsToRefer int
		)
		if err := rows.Scan(&id, &referral, &kitnsToRefer); err != nil {
			log.Errorf("Cannot scan a row: %w", err)
			failRows += 1
			continue
		}
		log.Infof("Redeeming %d kitns to referrers of the user %s", kitnsToRefer, id)
		if err := redeemOneUser(db, id, referral, kitnsToRefer); err != nil {
			log.Errorf("Error while redeeming the user %s: %w", id, err)
			failRows += 1
			continue
		}
		successRows += 1
	}

	return successRows, failRows, nil
}

func redeemOneUser(db *sql.DB, id, referral string, kitnsToRefer int) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Errorf("Error creating transaction: %w", err)
		return err
	}
	defer tx.Rollback()

	if err := redeemStep(db, referral, kitnsToRefer, 1); err != nil {
		return err
	}
	// Update processed tokens after successful awarding
	res, err := db.Exec(`
		UPDATE users
		SET kitns_ref_redeemed = kitns_ref_redeemed + ?
		WHERE id = ?
	`, kitnsToRefer, id)
	logResult(fmt.Sprintf("Update %d redeemed kitns for %s", kitnsToRefer, id), res, err)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func redeemStep(db *sql.DB, referral string, kitnsToRefer int, refLevel int) error {
	// Get the user that should get awards
	rows, err := db.Query(`
		SELECT id
		FROM users_refcodes
		WHERE referral = ?
	`, referral)
	if err != nil {
		log.Errorf("Error reading referrer, %w", err)
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil
	}

	var nextId string
	if err = rows.Scan(&nextId); err != nil {
		log.Errorf("Can't scan a row: %w", err)
		return err
	}

	// Award the user
	awarded := float32(kitnsToRefer) * getRefCoeffForLevel(refLevel)
	log.Infof("Awarding %f kitns to the user %s", awarded, nextId)
	res, err := db.Exec(`
		UPDATE users
		SET kitns_ref_daily = kitns_ref_daily + ?
		WHERE id = ?
	`, awarded, nextId)
	logResult(fmt.Sprintf("Award %f referral kitns for %s", awarded, nextId), res, err)
	if err != nil {
		return err
	}

	// Check for the next awarded user in the referral chain
	rows, err = db.Query(`
		SELECT referral
		FROM users
		WHERE id = ?
	`, nextId)

	if err != nil {
		log.Errorf("Error reading next referral, %w", err)
		return err
	}

	if !rows.Next() {
		return nil
	}

	var nextRef string
	if err = rows.Scan(&nextRef); err != nil {
		log.Errorf("Can't scan a row: %w", err)
		return err
	}

	// Stop qwqrding if the referral chain is ended.
	if nextRef == "" {
		return nil
	}

	// Awards the next referrer if found
	return redeemStep(db, nextRef, kitnsToRefer, refLevel+1)
}

func getRefCoeffForLevel(level int) float32 {
	return 0.1 / float32(level)
}

func logResult(msgPrefix string, r sql.Result, e error) {
	if e != nil {
		log.Errorf("Query failed: %w", e)
		return
	}
	rows, err := r.RowsAffected()
	if err != nil {
		log.Errorf("Failed to get status of db op: %w", err)
		return
	}
	if rows != 1 {
		log.Warnf("%s: Expected to affect 1 row, affected %d", msgPrefix, rows)
	}
}
