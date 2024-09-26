package server

import (
	"flag"

	"cleanapp/common"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/backend/util"
	"cleanapp/common/disburse"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

var (
	ethNetworkUrlMain   = flag.String("eth_network_url_main", "", "Ethereum network address for main chain.")
	privateKeyMain      = flag.String("eth_private_key_main", "", "The private key for connecting to the smart contract for main chain.")
	contractAddressMain = flag.String("contract_address_main", "", "The contract address in HEX for main chain.")

	ethNetworkUrlShadow   = flag.String("eth_network_url_shadow", "", "Ethereum network address for shadow chain.")
	privateKeyShadow      = flag.String("eth_private_key_shadow", "", "The private key for connecting to the smart contract for shadow chain.")
	contractAddressShadow = flag.String("contract_address_shadow", "", "The contract address in HEX for shadow chain.")
)

func CreateOrUpdateUser(c *gin.Context) {
	var user api.UserArgs

	// Get the arguments.
	if err := c.BindJSON(&user); err != nil {
		log.Errorf("Failed to get the argument in /user call: %w", err)
		return
	}

	if user.Version != "2.0" {
		log.Errorf("Bad version in /update_or_create_user, expected: 2.0, got: %v", user.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("Error connecting to DB: %v", err)
		return
	}
	defer dbc.Close()

	mainDisburser, err := disburse.NewDisburser(*ethNetworkUrlMain, *privateKeyMain, *contractAddressMain)
	if err != nil {
		log.Errorf("Error creating main tokens disburser: %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	disbursers := []*disburse.Disburser{mainDisburser}

	if *ethNetworkUrlShadow != "" {
		shadowDisburser, err := disburse.NewDisburser(*ethNetworkUrlShadow, *privateKeyShadow, *contractAddressShadow)
		if err != nil {
			log.Errorf("Error creating main tokens disburser: %w", err)
			c.Status(http.StatusInternalServerError) // 500
			return
		}
		disbursers = append(disbursers, shadowDisburser)
	}

	resp, err := db.CreateOrUpdateUser(dbc, &user, util.UserIdToTeam, disbursers)
	if err != nil {
		if resp != nil && resp.DupAvatar {
			// Printing error and returning success, the duplicate info is in response
			log.Errorf("%w", err)
		} else {
			log.Errorf("Failed to update user with %w", err)
			c.Status(http.StatusInternalServerError) // 500
			return
		}
	}

	if user.Referral != "" {
		// TODO: Make the call async after the db connection is handled by the db controller
		db.CleanupReferral(dbc, user.Referral)
	}
	c.IndentedJSON(http.StatusOK, resp) // 200
}
