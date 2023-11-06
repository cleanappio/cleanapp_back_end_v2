package main

import (
	"flag"
	"log"

	"cleanapp/backend/impl"
)

func main() {
	flag.Parse()
	log.Println("Hello!")
	backend.StartService()
	log.Println("Bye!")
}
