package server

import (
	"flag"
	"fmt"

	"cleanapp/referral/service"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	readReferralEndpoint = "readreferral"
	writeReferralEndpoint = "writereferral"
)

var (
	serverPort = flag.Int("port", 8081, "The port used by the service.")
)

func StartServer() {
	log.Info("Starting the server...")
	router := gin.Default()
	handler, err := service.NewHandler()
	if err != nil {
		log.Errorf("referral handler creation: %w", err)
		return
	}
	router.GET(readReferralEndpoint, handler.ReadReferral)
	router.POST(writeReferralEndpoint, handler.WriteReferral)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Info("Finished the server. Should not ever being seen.")
}
