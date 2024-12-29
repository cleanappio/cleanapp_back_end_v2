package server

import (
	"flag"
	"fmt"
	"time"

	"github.com/apex/log"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHelp              = "/help"
	EndPointUser              = "/update_or_create_user"
	EndPointReport            = "/report"
	EndPointReadReport        = "/read_report"
	EndPointGetMap            = "/get_map"
	EndPointGetStats          = "/get_stats"
	EndPointGetTeams          = "/get_teams"
	EndPointGetTopScores      = "/get_top_scores"
	EndPointPrivacyAndTOC     = "/update_privacy_and_toc"
	EndPointReadReferral      = "/read_referral"
	EndPointWriteReferral     = "/write_referral"
	EndPointGenerateReferral  = "/generate_referral"
	EndPointGetBlockChainLink = "/get_blockchain_link"
	EndPointGetActions        = "/get_actions"
	EndPointGetAction         = "/get_action"
	EndPointCreateAction      = "/create_action"
	EndPointUpdateAction      = "/update_action"
	EndPointDeleteAction      = "/delete_action"
	EndPointUpdateUserAction  = "/update_user_action"
)

var (
	serverPort = flag.Int("port", 8080, "The port used by the service.")
)

func StartService() {
	log.Info("Starting the service...")
	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.GET(EndPointHelp, Help)
	router.POST(EndPointUser, CreateOrUpdateUser)
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
	router.POST(EndPointCreateAction, CreateAction)
	router.POST(EndPointUpdateAction, UpdateAction)
	router.POST(EndPointDeleteAction, DeleteAction)
	router.GET(EndPointGetActions, GetActions)
	router.GET(EndPointGetAction, GetAction)
	router.POST(EndPointUpdateUserAction, UpdateUserAction)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Info("Finished the service. Should not ever being seen.")
}
