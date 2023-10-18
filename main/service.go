package main

import (
	"log"

	"cleanapp/be"
)

func main() {
	log.Println("Hello!")
	be.StartService()
	log.Println("Bye!")
}
