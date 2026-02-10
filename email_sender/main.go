package main

import (
	"cleanapp/email_sender/db"
	"cleanapp/email_sender/email"
	"cleanapp/email_sender/logic"
	"cleanapp/email_sender/utils"
	"flag"
	"log"
)

func main() {

	err := utils.LoadEnvFile(".env")
	if err != nil {
		log.Printf("warning: failed to load env file: %v", err)
	}

	flag.Parse()

	//db := mysql.Connect(*dbUser, *dbPassword, *dbHost, *dbPort, *dbName)
	//defer db.Close()

	dbConn := db.Connect()
	defer dbConn.Close()

	emailer := &email.Mailer{}

	log.Println(" Starting email dispatcher...")
	logic.CheckAndSendReports(dbConn, emailer)

	log.Println(" Service complete")

}
