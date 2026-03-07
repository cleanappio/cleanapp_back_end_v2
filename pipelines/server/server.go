package server

import (
	"flag"
	"fmt"
	"net/http"

	"cleanapp-common/edge"
	"cleanapp-common/serverx"
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
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.GET(EndPointVersion, func(c *gin.Context) {
		c.JSON(200, version.Get("cleanapp-pipelines"))
	})
	router.GET(EndPointHelp, Help)
	router.POST(EndPointReferralsRedeem, ReferralsRedeem)
	router.POST(EndPointTokensDisburse, DisburseTokens)

	srv := serverx.New(fmt.Sprintf(":%d", *serverPort), router)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start pipelines service: %v", err)
	}
	log.Warn("Finished the service. Should not ever being seen.")
}
