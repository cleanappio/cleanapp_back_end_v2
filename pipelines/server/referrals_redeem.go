package server

import (
	"cleanapp/common"
	"cleanapp/pipelines/redeem"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type RedeemArgs struct {
	Version string `json:"version"` // Must be "2.0"
}

func ReferralsRedeem(c *gin.Context) {
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

	succeeded, failed, err := redeem.Redeem(db)
	if err != nil {
		log.Errorf("Redeem failed, %w", err)
		return
	}
	log.Infof("Redeem finished, %d users redeemed successfully, %d users redeem failed.", succeeded, failed)

	c.Status(http.StatusOK)
}
