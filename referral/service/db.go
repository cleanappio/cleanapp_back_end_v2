package service

import (
	"database/sql"
	"log"
)

type referralDB struct {
	db *sql.DB
}

func (db *referralDB) ReadReferral(key string) (string, error) {
	log.Printf("Read: retrieving the referral code for the device %s\n", key)

	rows, err := db.db.Query(`SELECT refvalue
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

func (db *referralDB) WriteReferral(key, value string) error {
	log.Printf("Write: Trying to save the referral from device %s with value %s\n", key, value)

	existing, err := db.ReadReferral(key)
	if err != nil {
		return err
	}

	// If the referral already exists then just return without inserting
	if existing != "" {
		return nil
	}

	_, err = db.db.Exec(`INSERT
	  INTO referrals (refkey, refvalue)
	  VALUES (?, ?)`,
		key, value)

	return err
}
