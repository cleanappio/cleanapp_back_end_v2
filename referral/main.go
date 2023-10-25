package main

import (
	"flag"
	"fmt"
	"runtime"

	"cleanapp/referral/server"
)

func main() {
	fmt.Printf("%s/%s\n", runtime.GOOS, runtime.GOARCH)
	flag.Parse()
	server.StartServer()
}