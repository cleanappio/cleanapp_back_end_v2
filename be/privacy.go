package be

import (
	"cleanapp/common"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type PrivacyAndTOCArgs struct {
	Version  string `json:"version"` // Must be "2.0"
	Id       string `json:"id"`      // public key.
	Privacy  string `json:"privacy"`
	AgreeTOC string `json:"agree_toc"`
}

func UpdatePrivacyAndTOC(c *gin.Context) {
	log.Print("Call to /update_privacy_and_toc")
	var args PrivacyAndTOCArgs

	if err := c.BindJSON(&args); err != nil {
		log.Printf("Failed to get arguments: %v", err)
		c.String(http.StatusBadRequest, "Could not read JSON input.") // 400
		return
	}

	if args.Version != "2.0" {
		log.Printf("Bad version in /update_or_create_user, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	log.Printf("/update_privacy_and_toc got %v", args)

	db, err := common.DBConnect(*mysqlAddress)
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer db.Close()

	err = updatePrivacyAndTOC(db, &args)
	if err != nil {
		log.Printf("Failed to update privacy and TOC %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
