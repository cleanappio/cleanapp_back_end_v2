package server

import (
	"cleanapp/common"
	"cleanapp/pipelines/disburse"
	"flag"
	"net/http"

	"github.com/apex/log"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
)

var (
	ethNetworkUrl   = flag.String("eth_network_url", "", "Ethereum network address.")
	privateKey      = flag.String("eth_private_key", "", "The private key for connecting to the smart contract.")
	contractAddress = flag.String("contract_address", "", "The contract address in HEX")
)

type DisbusrseArgs struct {
	Version string `json:"version"` // Must be "2.0"
}

func DisburseTokens(c *gin.Context) {
	var args RedeemArgs

	if err := c.BindJSON(&args); err != nil {
		log.Errorf("Failed to get the argument in /get_stats call: %w", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /update_or_create_user, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	db, err := common.DBConnect()
	if err != nil {
		log.Errorf("Error connecting to the database, %w", err)
		return
	}
	defer db.Close()

	client, err := ethclient.Dial(*ethNetworkUrl)
	if err != nil {
		log.Errorf("Error creating the Ethereum client, %w", err)
	}

	d, err := disburse.NewDisburser(db, client, *privateKey, *contractAddress)
	if err != nil {
		log.Errorf("Disburser creation failed, %w", err)
	}
	err = d.Disburse()
	if err != nil {
		log.Errorf("Disburse failed, %w", err)
		return
	}
	log.Infof("Tokens disburse finished successfully.")

	c.Status(http.StatusOK)
}
