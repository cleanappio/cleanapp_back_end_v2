package backend

import (
	"cleanapp/api"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func (h *handler) GetMap(c *gin.Context) {
	log.Info("Call to /get_map")
	var ma api.MapArgs

	// Troubleshooting code:
	// b, _ := c.GetRawData()
	// log.Printf("Got %s", string(b))

	// Get the arguments.
	if err := c.BindJSON(&ma); err != nil {
		log.Errorf("Failed to get the argument in /get_map call: %w", err)
		c.String(http.StatusInternalServerError, "Could not read JSON input.") // 500
		return
	}

	if ma.Version != "2.0" {
		log.Errorf("Bad version in /update_or_create_user, expected: 2.0, got: %w", ma.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	// Add user to the database.
	log.Infof("/get_map got %v", ma)
	r, err := h.sDB.getMap(ma.VPort)
	if err != nil {
		log.Errorf("Failed to update user with %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	vp := &ma.VPort
	a := NewMapAggregator(vp.LatTop, vp.LonLeft, vp.LatBottom, vp.LonRight, 10, 10)
	for _, p := range r {
		a.AddPoint(p.Latitude, p.Longitude)

	}
	c.IndentedJSON(http.StatusOK, a.ToArray()) // 200
	//c.Status(http.StatusOK) // 200
}
