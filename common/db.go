package common

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

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

func DBConnect() (*sql.DB, error) {
	db, err := sql.Open("mysql", mysqlAddress())
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil, err
	}
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	log.Println("Established db connection.")
	return db, err
}
