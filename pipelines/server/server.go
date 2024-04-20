package server

import (
	"flag"
	"fmt"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHelp             = "/help"
	EndPointReferralsRedeem  = "/referrals_redeem"
)

var (
	serverPort = flag.Int("port", 8090, "The port used by the service.")
)

func StartService() {
	log.Info("Starting the service...")
	router := gin.Default()
	router.GET(EndPointHelp, Help)
	router.POST(EndPointReferralsRedeem, ReferralsRedeem)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Warn("Finished the service. Should not ever being seen.")
}
