package be

import (
	"flag"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
)

const (
	EndPointUser          = "/update_or_create_user"
	EndPointPrivacyAndTOC = "/update_privacy_and_toc"
	EndPointReport        = "/report"
	EndPointGetMap        = "/get_map"
)

var (
	serverPort = flag.Int("port", 8080, "The port used by the service.")
)

func StartService() {
	log.Println("Starting the service...")
	router := gin.Default()
	router.POST(EndPointUser, UpdateUser)
	router.POST(EndPointPrivacyAndTOC, UpdatePrivacyAndTOC)
	router.POST(EndPointReport, Report)
	router.POST(EndPointGetMap, GetMap)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Println("Finished the service. Should not ever being seen.")
}
