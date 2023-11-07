package backend

import (
	"cleanapp/api"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func (h *handler) UpdateUser(c *gin.Context) {
	log.Info("Call to /update_or_create_user")
	var user api.UserArgs

	/* Troubleshooting code:
	b, _ := c.GetRawData()
	log.Printf("Got %s", string(b))
	*/

	// Get the arguments.
	if err := c.BindJSON(&user); err != nil {
		log.Errorf("Failed to get the argument in /user call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if user.Version != "2.0" {
		log.Infof("Bad version in /update_or_create_user, expected: 2.0, got: %v", user.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	log.Infof("/update_or_create_user got %v", user)
	err := h.sDB.updateUser(user)
	if err != nil {
		log.Errorf("Failed to update user with %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
