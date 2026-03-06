package main

import (
	"context"
	"log"
	"time"

	"gdpr-process-service/config"
	"gdpr-process-service/database"
	"gdpr-process-service/utils"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := utils.DBConnect(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := database.RunMigrations(ctx, db); err != nil {
		log.Fatal(err)
	}

	log.Println("gdpr-process-service migrations applied successfully")
}
