package main

import (
	"flag"
	"log"

	"cleanapp/pipelines/server"
)

func main() {
	flag.Parse()
	log.Println("Hello!")
	server.StartService()
	log.Println("Bye!")
}
