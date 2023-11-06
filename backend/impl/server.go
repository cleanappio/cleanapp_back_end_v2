package backend

import (
	"cleanapp/api"
	"flag"
	"fmt"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

var (
	mysqlAddress = flag.String("mysql_address", "server:dev_pass@tcp(cleanupdb:3306)/cleanapp", "MySQL address string")
	serverPort   = flag.Int("port", 8080, "The port used by the service.")
)

type handler struct {
	sDB *sqlDB
}

func newHandler() (*handler, error) {
	db, err := dbConnect(*mysqlAddress)
	if err != nil {
		return nil, err
	}

	return &handler{
		sDB: &sqlDB{db: db},
	}, nil
}

func StartService() {
	log.Info("Starting the service...")
	router := gin.Default()

	handler, err := newHandler()
	if err != nil {
		log.Errorf("http handler creation: %w", err)
		return
	}

	router.POST(api.UserEndpoint, handler.updateUser)
	router.POST(api.ReportEndpoint, handler.report)
	router.POST(api.GetMapEndpoint, handler.getMap)
	router.GET(api.ReadReferralEndpoint, handler.readReferral)
	router.POST(api.WriteReferralEndpoint, handler.writeReferral)

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Info("Finished the service. Should not ever being seen.")
}
