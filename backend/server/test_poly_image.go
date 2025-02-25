package server

import (
	"cleanapp/backend/email"
	"cleanapp/backend/server/api"
	"fmt"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func DrawTestPolyImage(c *gin.Context) {
	args := &api.TestPolyImageRequest{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /test_poly_image call: %w", err)
		c.String(http.StatusBadRequest, fmt.Sprintf("%v", err))
		return
	}

	img, err := email.GeneratePolygonImg(&args.Feature, args.ReportLat, args.ReportLon)
	if err != nil {
		log.Errorf("Error generating image: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprintf("Error generating image: %v", err))
		return
	}

	c.Data(http.StatusOK, "image/png", img)
}