package main

import (
	"database/sql"
	"email-service/config"
	"email-service/service"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	cfg, err := config.Load()
	if err != nil { log.Fatal(err) }
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := sql.Open("mysql", dsn)
	if err != nil { log.Fatal(err) }
	defer db.Close()
	if err := service.RunMigrations(db); err != nil { log.Fatal(err) }
	log.Printf("email-service migrations applied successfully")
}
