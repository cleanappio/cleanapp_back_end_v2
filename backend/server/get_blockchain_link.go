package server

import (
	"fmt"
	"net/http"

	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	SEPOLIA_BASE_URL = "https://sepolia.basescan.org/address/%s#tokentxns"
)

func GetBlockchainLink(c *gin.Context) {
	args := &api.BaseArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /get_blockchain_url call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /get_blockchain_url, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	response := &api.BlockchainLinkResponse{
		BlockchainLink: fmt.Sprintf(SEPOLIA_BASE_URL, args.Id),
	}

	c.IndentedJSON(http.StatusOK, response)
}