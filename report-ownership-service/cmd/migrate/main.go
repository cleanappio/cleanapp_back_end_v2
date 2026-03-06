package main

import (
	"database/sql"
	"fmt"
	"log"
	"report-ownership-service/config"
	"report-ownership-service/database"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	cfg, err := config.Load()
	if err != nil { log.Fatal(err) }
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := sql.Open("mysql", dsn)
	if err != nil { log.Fatal(err) }
	defer db.Close()
	if err := database.InitializeSchema(db); err != nil { log.Fatal(err) }
	log.Printf("report-ownership-service migrations applied successfully")
}
