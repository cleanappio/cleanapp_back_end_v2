package server

import (
	"flag"
	"fmt"

	"cleanapp/common/version"
	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHelp            = "/help"
	EndPointReferralsRedeem = "/referrals_redeem"
	EndPointTokensDisburse  = "/tokens_disburse"
	EndPointVersion         = "/version"
)

var (
	serverPort = flag.Int("port", 8090, "The port used by the service.")
)

func StartService() {
	log.Info("Starting the service...")
	router := gin.Default()
	router.GET(EndPointVersion, func(c *gin.Context) {
		c.JSON(200, version.Get("cleanapp-pipelines"))
	})
	router.GET(EndPointHelp, Help)
	router.POST(EndPointReferralsRedeem, ReferralsRedeem)
	router.POST(EndPointTokensDisburse, DisburseTokens)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Warn("Finished the service. Should not ever being seen.")
}
