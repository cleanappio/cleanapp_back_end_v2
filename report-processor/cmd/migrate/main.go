package main

import (
	"context"
	"log"
	"time"

	"report_processor/config"
	"report_processor/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := database.RunMigrations(ctx, db.GetDB()); err != nil {
		log.Fatal(err)
	}

	log.Println("report-processor migrations applied successfully")
}
