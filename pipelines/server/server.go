package server

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os/signal"
	"syscall"

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
	// Install listeners before serving so Docker's PID1 signal is never handled
	// by the default immediate process exit while a request is active.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
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
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("Failed to start pipelines service: %v", err)
	}
	if err := serveUntilShutdown(ctx, srv, listener); err != nil {
		log.Fatalf("Pipelines service failed: %v", err)
	}
	log.Info("Pipelines requests drained; service stopped")
}
