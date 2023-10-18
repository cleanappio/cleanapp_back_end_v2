package be

import (
	"log"

	"github.com/gin-gonic/gin"
)

const (
	EndPointReport = "/report"
)

func StartService() {
	log.Println("Starting the service...")
	router := gin.Default()
	router.POST(EndPointReport, Report)

	router.Run("localhost:8080")
	log.Println("Finished the service. Should not ever being seen.")
}
