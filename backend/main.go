package main

import (
	"flag"

	"cleanapp/backend/server"
	"github.com/apex/log"
)

func main() {
	flag.Parse()
	log.Info("Hello!")
	server.StartService()
	log.Info("Bye!")
}
