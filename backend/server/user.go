package server

import (
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/backend/util"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

// publishUserEvent publishes a user event to RabbitMQ using the shared publisher
func publishUserEvent(routingKey string, userData interface{}) {
	if rabbitmqPublisher == nil {
		log.Errorf("RabbitMQ publisher not initialized, cannot publish user event: %s", routingKey)
		return
	}

	err := rabbitmqPublisher.PublishWithRoutingKey(routingKey, userData)
	if err != nil {
		log.Errorf("Failed to publish user event %s: %v", routingKey, err)
		return
	}

	log.Infof("Successfully published user event: %s", routingKey)
}

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

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Error connecting to DB: %v", err)
		return
	}

	resp, err := db.CreateOrUpdateUser(dbc, &user, util.UserIdToTeam, nil)
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
	// Publish user add/edit event for successful user creation/update
	if err == nil {
		publishUserEvent(userRoutingKey, user)
	}

	if user.Referral != "" {
		// TODO: Make the call async after the db connection is handled by the db controller
		db.CleanupReferral(dbc, user.Referral)
	}

	c.IndentedJSON(http.StatusOK, resp) // 200
}
