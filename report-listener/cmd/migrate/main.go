package main

import (
	"context"
	"log"
	"os"
	"time"

	"report-listener/config"
	"report-listener/database"
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

	timeout := 30 * time.Minute
	if raw := os.Getenv("MIGRATION_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("invalid MIGRATION_TIMEOUT %q: %v", raw, err)
		}
		timeout = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := database.RunMigrations(ctx, db.DB()); err != nil {
		log.Fatal(err)
	}

	log.Println("report-listener migrations applied successfully")
}
