package server

import (
	"cleanapp/common"
	"log"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/backend/util"

	"github.com/gin-gonic/gin"
)

func UpdateUser(c *gin.Context) {
	log.Print("Call to /update_or_create_user")
	var user api.UserArgs

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

	dbc, err := common.DBConnect()
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer dbc.Close()

	resp, err := db.UpdateUser(dbc, &user, util.UserIdToTeam)
	if err != nil {
		if resp != nil && resp.DupAvatar {
			// Printing error and returning success, the duplicate info is in response
			log.Printf("%v", err)
		} else {
			log.Printf("Failed to update user with %v", err)
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
