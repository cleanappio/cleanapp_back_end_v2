package main

import (
	"flag"

	"cleanapp/referral/server"
)

func main() {
	flag.Parse()
	server.StartServer()
}