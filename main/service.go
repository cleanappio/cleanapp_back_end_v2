package main

import (
	"flag"
	"log"

	"cleanapp/be"
)

func main() {
	flag.Parse()
	log.Println("Hello!")
	be.StartService()
	log.Println("Bye!")
}
