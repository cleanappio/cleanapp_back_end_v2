package main

import (
	"context"
	"log"
	"time"

	"customer-service/config"
	"customer-service/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := database.OpenDB(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := database.RunMigrations(ctx, db); err != nil {
		log.Fatal(err)
	}

	log.Println("customer-service migrations applied successfully")
}
