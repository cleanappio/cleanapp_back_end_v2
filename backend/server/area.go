package server

import (
	"cleanapp/backend/server/api"
	"fmt"
	"net/http"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
	geojson "github.com/paulmach/go.geojson"
)

func CreateArea(c *gin.Context) {
	args := &api.CreateAreaRequest{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /create_action call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /create_action, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	log.Info(fmt.Sprintln(args))

	c.Status(http.StatusOK)
}

func GetAreas(c *gin.Context) {
	res := &api.Area{
		Id:            42,
		Name:          "Test Area",
		Description:   "This is a test area",
		ContactName:   "Test",
		ContractEmail: "test@testmail.com",
		Coordinates: &geojson.Feature{
			ID:   "way/217462115",
			Type: "Feature",
			Geometry: &geojson.Geometry{
				Type: "Polygon",
				Polygon: [][][]float64{
					{
						{175.4527356, -41.2149372},
						{175.4525347, -41.2147901},
						{175.4523615, -41.2146843},
						{175.450895, -41.2154755},
						{175.4517529, -41.2163586},
						{175.4522375, -41.2160895},
						{175.4519603, -41.2162412},
						{175.4522983, -41.2160563},
						{175.4524341, -41.2159776},
						{175.4518662, -41.2154027},
						{175.4527356, -41.2149372},
					},
				},
			},
			Properties: map[string]interface{} {
				"@id": "way/217462115",
				"name": "Martinborough Top 10 Holiday Park",
				"email": "camp@martinboroughholidaypark.com",
				"phone": "+64 6 306 8946",
			},
		},
		CreatedAt: "2025-02-11T13:06:32.724Z",
		UpdatedAt: "2025-02-11T13:06:32.763Z",
	}

	c.IndentedJSON(http.StatusOK, []*api.Area {res})
}
