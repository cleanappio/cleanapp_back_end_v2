package be

import (
	"cleanapp/common"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type StatsArgs struct {
	Version string `json:"version"` // Must be "2.0"
	Id      string `json:"id"`      // public key.
}

type StatsResponse struct {
	Version           string  `json:"version"` // Must be "2.0"
	Id                string  `json:"id"`      // public key.
	KitnsDaily        int     `json:"kitns_daily"`
	KitnsDisbursed    int     `json:"kitns_disbursed"`
	KitnsRefDaily     float64 `json:"kitns_ref_daily"`
	KitnsRefDisbusded float64 `json:"kitns_ref_disbursed"`
}

func GetStats(c *gin.Context) {
	log.Print("Call to /get_stats")
	var sa StatsArgs

	// Get the arguments.
	if err := c.BindJSON(&sa); err != nil {
		log.Printf("Failed to get the argument in /get_stats call: %v", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if sa.Version != "2.0" {
		log.Printf("Bad version in /update_or_create_user, expected: 2.0, got: %v", sa.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	db, err := common.DBConnect()
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer db.Close()

	// Add user to the database.
	log.Printf("/get_stats got %v", sa)
	r, err := getStats(db, sa.Id)
	if err != nil {
		log.Printf("Failed to update user with %v", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, r) // 200
}
