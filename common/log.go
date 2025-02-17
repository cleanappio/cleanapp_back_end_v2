package common

import (
	"database/sql"

	"github.com/apex/log"
)

func LogResult(msgPrefix string, r sql.Result, e error, e1 bool) {
	if e != nil {
		log.Errorf("Query failed: %w", e)
		return
	}
	rows, err := r.RowsAffected()
	if err != nil {
		log.Errorf("Failed to get status of db op: %w", err)
		return
	}
	if e1 && rows != 1 {
		log.Warnf("%s: Expected to affect 1 row, affected %d", msgPrefix, rows)
	}
}
