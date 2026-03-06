package main

import (
	"areas-service/config"
	"areas-service/database"
	"areas-service/utils"
	"log"
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
	if err := database.InitSchema(db); err != nil {
		log.Fatal(err)
	}
	log.Printf("areas-service migrations applied successfully")
}
