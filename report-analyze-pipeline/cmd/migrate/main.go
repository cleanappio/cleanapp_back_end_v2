package main

import (
	"context"
	"log"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := database.RunMigrations(ctx, db.GetDB()); err != nil {
		log.Fatal(err)
	}

	log.Println("report-analyze-pipeline migrations applied successfully")
}
