package be

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Help(c *gin.Context) {
	log.Print("Call to /help")

	c.HTML(http.StatusOK, `
	<h1>CleanApp API</h1>
	<p>cleanapp.io API server, version 2.0, 2023</p>
	`)
}
