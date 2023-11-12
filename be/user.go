package be

import (
	"cleanapp/common"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type UserArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
	Avatar  string `json:"avatar"`
}

type UserResp struct {
	Team TeamColor `json:"team"` // Blue or Green
}

type PrivacyAndTOCArgs struct {
	Version  string `json:"version"` // Must be "2.0"
	Id       string `json:"id"`      // public key.
	Privacy  string `json:"privacy"`
	AgreeTOC string `json:"agree_toc"`
}

func UpdateUser(c *gin.Context) {
	log.Print("Call to /update_or_create_user")
	var user UserArgs

	/* Troubleshooting code:
	b, _ := c.GetRawData()
	log.Printf("Got %s", string(b))
	*/

	// Get the arguments.
	if err := c.BindJSON(&user); err != nil {
		log.Printf("Failed to get the argument in /user call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if user.Version != "2.0" {
		log.Printf("Bad version in /update_or_create_user, expected: 2.0, got: %v", user.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	log.Printf("/update_or_create_user got %v", user)
	err := updateUser(user)
	if err != nil {
		log.Printf("Failed to update user with %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	c.IndentedJSON(http.StatusOK, UserResp{Team: userIdToTeam(user.Id)}) // 200
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

	err = updatePrivacyAndTOC(db, &args)
	if err != nil {
		log.Printf("Failed to update privacy and TOC %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
