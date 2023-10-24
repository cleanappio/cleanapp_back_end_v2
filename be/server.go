package be

import (
	"log"

	"github.com/gin-gonic/gin"
)

const (
	EndPointUser   = "/update_or_create_user"
	EndPointReport = "/report"
)

func StartService() {
	log.Println("Starting the service...")
	router := gin.Default()
	router.POST(EndPointUser, UpdateUser)
	router.POST(EndPointReport, Report)

	router.Run(":8080")
	//router.Run("localhost:8080")
	log.Println("Finished the service. Should not ever being seen.")
}
