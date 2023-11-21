package be

import (
	"flag"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
)

const (
	EndPointUser   = "/update_or_create_user"
	EndPointReport = "/report"
	EndPointGetMap = "/get_map"
	EndPointGetStats = "/get_stats"
)

var (
	serverPort = flag.Int("port", 8080, "The port used by the service.")
)

func StartService() {
	log.Println("Starting the service...")
	router := gin.Default()
	router.POST(EndPointUser, UpdateUser)
	router.POST(EndPointReport, Report)
	router.POST(EndPointGetMap, GetMap)
	router.POST(EndPointGetStats, GetStats)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Println("Finished the service. Should not ever being seen.")
}
