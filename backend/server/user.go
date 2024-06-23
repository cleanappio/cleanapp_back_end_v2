package server

import (
	"cleanapp/common"
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/backend/util"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func UpdateUser(c *gin.Context) {
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

	resp, err := db.UpdateUser(dbc, &user, util.UserIdToTeam)
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
