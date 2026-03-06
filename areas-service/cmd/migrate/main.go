package main

import (
	"areas-service/config"
	"areas-service/database"
	"areas-service/utils"
	"context"
	"log"
	"time"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := utils.DBConnect(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.RunMigrations(ctx, db); err != nil {
		log.Fatal(err)
	}
	log.Printf("areas-service migrations applied successfully")
}
