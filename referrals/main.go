package main

import (
	"cleanapp/common"
	"cleanapp/referrals/redeem"
	"flag"
	"fmt"

	"github.com/apex/log"
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

func main() {
	flag.Parse()

	db, err := common.DBConnect(mysqlAddress())
	if err != nil {
		log.Errorf("Cannot connect to the database: %w", err)
		return
	}
	redeem.Redeem(db)
}