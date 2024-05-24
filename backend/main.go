package main

import (
	"flag"
	"log"

	"cleanapp/backend/server"
)

func main() {
	flag.Parse()
	log.Println("Hello!")
	server.StartService()
	log.Println("Bye!")
}
