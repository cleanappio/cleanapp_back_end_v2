package server

import (
	"cleanapp/common"
	"cleanapp/pipelines/disburse"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type DisburseArgs struct {
	Version string `json:"version"` // Must be "2.0"
}

func DisburseTokens(c *gin.Context) {
	internalToken := strings.TrimSpace(os.Getenv("INTERNAL_ADMIN_TOKEN"))
	if internalToken == "" {
		log.Error("INTERNAL_ADMIN_TOKEN is not configured for /tokens_disburse")
		c.String(http.StatusServiceUnavailable, "Internal admin token not configured.")
		return
	}

	gotToken := strings.TrimSpace(c.GetHeader("X-Internal-Admin-Token"))
	if gotToken == "" || subtle.ConstantTimeCompare([]byte(gotToken), []byte(internalToken)) != 1 {
		log.Warn("Unauthorized /tokens_disburse call")
		c.String(http.StatusUnauthorized, "Unauthorized.")
		return
	}

	var args DisburseArgs

	if err := c.BindJSON(&args); err != nil {
		log.Errorf("Failed to get the argument in /tokens_disburse call: %w", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /tokens_disburse, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	db, err := common.DBConnect()
	if err != nil {
		log.Errorf("Error connecting to the database, %w", err)
		c.String(http.StatusInternalServerError, "Database connection error.") // 500
		return
	}
	defer db.Close()

	d, err := disburse.NewDailyDisburser(db)
	if err != nil {
		log.Errorf("Disburser creation failed, %w", err)
		c.String(http.StatusInternalServerError, "Disburser creation error.") // 500
		return
	}
	err = d.Disburse()
	if err != nil {
		log.Errorf("Disburse failed, %w", err)
		c.String(http.StatusInternalServerError, "Token disbursement failure.") // 500
		return
	}
	log.Infof("Tokens disburse finished successfully.")

	c.Status(http.StatusOK)
}
