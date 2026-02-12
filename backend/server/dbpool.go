package server

import (
	"database/sql"
	"sync"

	"cleanapp/common"
)

var (
	serverDBOnce sync.Once
	serverDB     *sql.DB
	serverDBErr  error
)

func getServerDB() (*sql.DB, error) {
	serverDBOnce.Do(func() {
		serverDB, serverDBErr = common.DBConnect()
	})
	return serverDB, serverDBErr
}

func closeServerDB() {
	if serverDB != nil {
		_ = serverDB.Close()
	}
}
