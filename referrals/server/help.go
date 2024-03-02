package server

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Help(c *gin.Context) {
	log.Print("Call to /help")

	c.String(http.StatusOK, `
	CleanApp Referrals Redeem:
	Cleanapp referrals redeem server, version 2.0, 2024.
	`)
}
