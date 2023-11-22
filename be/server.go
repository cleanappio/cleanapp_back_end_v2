package be

import (
	"flag"
	"fmt"
	"log"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	EndPointUser     = "/update_or_create_user"
	EndPointReport   = "/report"
	EndPointGetMap   = "/get_map"
	EndPointGetStats = "/get_stats"
	EndPointGetTeams = "/get_teams"
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
	t, e := strconv.ParseInt(id[len(id)-1:], 16, 64)
	if e != nil {
		log.Printf("Bad user ID %s, err %v", id, e)
		return 1
	}
	return TeamColor(t%2 + 1)
}

func StartService() {
	log.Println("Starting the service...")
	router := gin.Default()
	router.POST(EndPointUser, UpdateUser)
	router.POST(EndPointReport, Report)
	router.POST(EndPointGetMap, GetMap)
	router.POST(EndPointGetStats, GetStats)
	router.POST(EndPointGetTeams, GetTeams)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Println("Finished the service. Should not ever being seen.")
}
