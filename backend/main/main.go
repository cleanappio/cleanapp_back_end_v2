package main

import (
	"flag"
	"log"

	"cleanapp/backend"
)

func main() {
	flag.Parse()
	log.Println("Hello!")
	backend.StartService()
	log.Println("Bye!")
}
