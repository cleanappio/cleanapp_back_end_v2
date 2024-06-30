package server

import (
	"flag"
	"fmt"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHelp             = "/help"
	EndPointUser             = "/update_or_create_user"
	EndPointReport           = "/report"
	EndPointReadReport       = "/read_report"
	EndPointGetMap           = "/get_map"
	EndPointGetStats         = "/get_stats"
	EndPointGetTeams         = "/get_teams"
	EndPointGetTopScores     = "/get_top_scores"
	EndPointPrivacyAndTOC    = "/update_privacy_and_toc"
	EndPointReadReferral     = "/read_referral"
	EndPointWriteReferral    = "/write_referral"
	EndPointGenerateReferral = "/generate_referral"
	EndPointGetBlockChainLink = "/get_blockchain_link"
)

var (
	serverPort = flag.Int("port", 8080, "The port used by the service.")
)

func StartService() {
	log.Info("Starting the service...")
	router := gin.Default()
	router.GET(EndPointHelp, Help)
	router.POST(EndPointUser, UpdateUser)
	router.POST(EndPointPrivacyAndTOC, UpdatePrivacyAndTOC)
	router.POST(EndPointReport, Report)
	router.POST(EndPointReadReport, ReadReport)
	router.POST(EndPointGetMap, GetMap)
	router.POST(EndPointGetStats, GetStats)
	router.POST(EndPointGetTeams, GetTeams)
	router.POST(EndPointGetTopScores, GetTopScores)
	router.POST(EndPointReadReferral, ReadReferral)
	router.POST(EndPointWriteReferral, WriteReferral)
	router.POST(EndPointGenerateReferral, GenerateReferral)
	router.POST(EndPointGetBlockChainLink, GetBlockchainLink)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Info("Finished the service. Should not ever being seen.")
}
