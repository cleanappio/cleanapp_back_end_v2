package be

import (
	"crypto/md5"
	"flag"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
)

const (
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
)

var (
	serverPort = flag.Int("port", 8080, "The port used by the service.")
)

type BaseArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
}

type TeamColor int

const (
	Unknown = 0
	Blue    = 1
	Green   = 2
)

// This function is internal to the BE and anywhere else the DB field should be used.
func userIdToTeam(id string) TeamColor {
	if id == "" {
		log.Printf("Empty user ID %q, this must not happen.", id)
		return 1
	}
	md5 := md5.Sum([]byte(id))
	return TeamColor(md5[len(md5)-1]%2 + 1)
}

func StartService() {
	log.Println("Starting the service...")
	router := gin.Default()
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

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Println("Finished the service. Should not ever being seen.")
}
