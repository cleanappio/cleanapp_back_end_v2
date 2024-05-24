package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func Help(c *gin.Context) {
	c.String(http.StatusOK, `
	CleanApp API:
	cleanapp.io API server, version 2.0, 2023.
	`)
}
