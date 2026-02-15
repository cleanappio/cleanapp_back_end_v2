package email

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"time"

	"github.com/fogleman/gg"
	geojson "github.com/paulmach/go.geojson"
)

type ViewPort struct {
	LatMin float64 `json:"latmin"`
	LonMin float64 `json:"lonmin"`
	LatMax float64 `json:"latmax"`
	LonMax float64 `json:"lonmax"`
}

const tileSize = 256
const maxTiles = 16

var osmTileHTTPClient = &http.Client{
	Timeout: 8 * time.Second,
}

// GeneratePolygonImg draws an OSM image for a given polygon feature and draws a given polygon on it.
// If feature is nil, it generates a 1km map centered on the report coordinates.
func GeneratePolygonImg(feature *geojson.Feature, reportLat, reportLon float64) ([]byte, error) {
	var zoom int
	var xMin, xMax, yMin, yMax int
	var err error

	if feature == nil {
		// Generate a 1km bounding box centered on the report coordinates
		zoom, xMin, xMax, yMin, yMax, err = generate1kmBoundingBox(reportLat, reportLon)
		if err != nil {
			return nil, err
		}
	} else {
		// Use the existing logic for polygon features
		zoom, xMin, xMax, yMin, yMax, err = boundToZoomXY(feature)
		if err != nil {
			return nil, err
		}
	}

	return generate(xMin, xMax, yMin, yMax, zoom, feature, reportLat, reportLon)
}

// generate1kmBoundingBox generates a 1km bounding box centered on the given coordinates
// and returns the appropriate zoom level and tile coordinates
func generate1kmBoundingBox(centerLat, centerLon float64) (zoom, xMin, xMax, yMin, yMax int, err error) {
	// Calculate 1km in degrees more accurately
	// 1 degree of latitude ≈ 111.32 km (constant)
	// 1 degree of longitude ≈ 111.32 * cos(latitude) km (varies by latitude)

	latDegrees := 1.0 / 111.32                                       // 1km in latitude degrees (constant)
	lonDegrees := 1.0 / (111.32 * math.Cos(centerLat*math.Pi/180.0)) // 1km in longitude degrees

	// Use the larger of the two to ensure we cover at least 1km in both directions
	kmInDegrees := math.Max(latDegrees, lonDegrees)

	// Create a bounding box that covers at least 1km in both directions
	bbox := &ViewPort{
		LatMin: centerLat - kmInDegrees/2,
		LatMax: centerLat + kmInDegrees/2,
		LonMin: centerLon - kmInDegrees/2,
		LonMax: centerLon + kmInDegrees/2,
	}

	// Find appropriate zoom level that fits within maxTiles
	zoom = 19
	for z := zoom; z > 0; z-- {
		xMin, yMax = latLngToTile(bbox.LatMin, bbox.LonMin, z)
		xMax, yMin = latLngToTile(bbox.LatMax, bbox.LonMax, z)
		tiles := (xMax - xMin + 1) * (yMax - yMin + 1)
		if tiles <= maxTiles {
			zoom = z
			break
		}
	}

	return zoom, xMin, xMax, yMin, yMax, nil
}

// generate generates an image for a given bound and a feature
// It fetches all tiles within the bound and draws the feature and the report point on them
func generate(xMin, xMax, yMin, yMax, zoom int, feature *geojson.Feature, reportLat, reportLon float64) ([]byte, error) {
	// Get all four tiles numbers.
	tiles := getAllTiles(xMin, xMax, yMin, yMax)
	imgs := make([][]image.Image, yMax-yMin+1)
	for i, col := range tiles {
		imgs[i] = make([]image.Image, xMax-xMin+1)
		for j, tile := range col {
			img, err := fetchTile(tile[0], tile[1], zoom)
			if err != nil {
				return nil, err
			}
			imgs[i][j] = img
		}
	}

	// Create a new image to draw on
	bounds := image.Rectangle{
		Min: image.Point{
			X: 0, Y: 0,
		},
		Max: image.Point{
			X: tileSize * len(tiles[0]),
			Y: tileSize * len(tiles),
		},
	}
	dst := image.NewRGBA(bounds)

	dc := gg.NewContextForRGBA(dst)
	dc.SetLineWidth(3)

	// Draw image
	for j, col := range imgs {
		for i, img := range col {
			ic := getImageCoords(i, j)
			dc.DrawImage(img, ic.X, ic.Y)
		}
	}

	// Draw polygon
	if feature != nil {
		if feature.Geometry.IsPolygon() {
			drawPolygon(dc, convertPoly(feature.Geometry.Polygon, tiles, zoom))
		} else if feature.Geometry.IsMultiPolygon() {
			for _, poly := range feature.Geometry.MultiPolygon {
				drawPolygon(dc, convertPoly(poly, tiles, zoom))
			}
		}
	}

	// Draw a report point
	dc.SetLineWidth(2)
	ptX, ptY := convertPoint(reportLat, reportLon, tiles, zoom)
	dc.SetRGBA255(255, 0, 0, 200)
	dc.NewSubPath()
	dc.DrawCircle(ptX, ptY, 15)
	dc.ClosePath()
	dc.FillPreserve()
	dc.SetRGBA255(233, 0, 0, 255)
	dc.Stroke()

	// Save image with polygon
	out := []byte{}
	writer := bytes.NewBuffer(out)
	if err := png.Encode(writer, dst); err != nil {
		return nil, err
	}
	return writer.Bytes(), nil
}

// fetchTile fetches one tile from OSM for given tile indices.
func fetchTile(x, y, zoom int) (image.Image, error) {
	tileURL := fmt.Sprintf("https://tile.openstreetmap.org/%d/%d/%d.png", zoom, x, y)

	req, err := http.NewRequest("GET", tileURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "CleanApp/2.0")

	resp, err := osmTileHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch tile: %s", resp.Status)
	}

	img, err := png.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return img, nil
}

func getImageCoords(i, j int) (imgPt image.Point) {
	imgPt = image.Point{
		X: i * tileSize,
		Y: j * tileSize,
	}
	return
}

func boundToZoomXY(feature *geojson.Feature) (zoom, xMin, xMax, yMin, yMax int, err error) {
	polyBound, err := computeBoundingBox(feature)
	if err != nil {
		return
	}
	zoom = 19
	for z := zoom; z > 0; z-- {
		xMin, yMax = latLngToTile(polyBound.LatMin, polyBound.LonMin, z)
		xMax, yMin = latLngToTile(polyBound.LatMax, polyBound.LonMax, z)
		tiles := (xMax - xMin + 1) * (yMax - yMin + 1)
		if tiles <= maxTiles {
			zoom = z
			break
		}
	}
	return
}

// latLngToTile converts latitude/longitude to OSM tile indices
func latLngToTile(lat, lon float64, zoom int) (x, y int) {
	n := math.Pow(2, float64(zoom))
	x = int((lon + 180.0) / 360.0 * n)
	y = int((1.0 - math.Log(math.Tan(lat*math.Pi/180.0)+1.0/math.Cos(lat*math.Pi/180.0))/math.Pi) / 2.0 * n)
	return
}

// computeBoundingBox computes a bounding box for a given area, either polygon or multiPolygon
func computeBoundingBox(feature *geojson.Feature) (*ViewPort, error) {
	if feature.Geometry.IsPolygon() {
		return computeBoundingBoxPolygon(feature.Geometry.Polygon), nil
	} else if feature.Geometry.IsMultiPolygon() {
		return computeBoundingBoxMultiPolygon(feature.Geometry.MultiPolygon), nil
	}
	return nil, fmt.Errorf("unsupported geometry type %v", feature.Geometry.Type)
}

// computeBoundingBoxPolygon computes a lat/lng bounding box for a polygon
func computeBoundingBoxPolygon(coords [][][]float64) *ViewPort {
	// Initialize bounding box with extreme values
	minLon, minLat := 180.0, 90.0
	maxLon, maxLat := -180.0, -90.0

	// Iterate through all coordinates
	for _, ring := range coords {
		for _, point := range ring {
			lon, lat := point[0], point[1]

			if lon < minLon {
				minLon = lon
			}
			if lon > maxLon {
				maxLon = lon
			}
			if lat < minLat {
				minLat = lat
			}
			if lat > maxLat {
				maxLat = lat
			}
		}
	}

	return &ViewPort{
		LonMin: minLon,
		LatMin: minLat,
		LonMax: maxLon,
		LatMax: maxLat,
	}
}

// computeBoundingBoxMultiPolygon computes a lat/lng bounding box for a multiPolygon.
func computeBoundingBoxMultiPolygon(coords [][][][]float64) *ViewPort {
	// Initialize bounding box with extreme values
	minLon, minLat := 180.0, 90.0
	maxLon, maxLat := -180.0, -90.0

	// Iterate through each polygon in the MultiPolygon
	for _, polygon := range coords {
		bbox := computeBoundingBoxPolygon(polygon)
		if bbox.LonMin < minLon {
			minLon = bbox.LonMin
		}
		if bbox.LonMax > maxLon {
			maxLon = bbox.LonMax
		}
		if bbox.LatMin < minLat {
			minLat = bbox.LatMin
		}
		if bbox.LatMax > maxLat {
			maxLat = bbox.LatMax
		}
	}

	return &ViewPort{
		LonMin: minLon,
		LatMin: minLat,
		LonMax: maxLon,
		LatMax: maxLat,
	}
}

// drqwPolygon draws the polygon inside a given image.
func drawPolygon(dc *gg.Context, poly [][][]float64) {
	for _, loop := range poly {
		dc.SetRGBA255(219, 33, 213, 100)
		dc.NewSubPath()
		dc.MoveTo(loop[0][0], loop[0][1])
		for _, point := range loop[1:] {
			dc.LineTo(point[0], point[1])
		}
		dc.ClosePath()
		dc.FillPreserve()
		dc.SetRGBA255(219, 33, 213, 255)
		dc.Stroke()
	}
}

// getTileBBox computes the bounding box for a tile given its x, y, and z.
func getTileBBox(x, y, zoom int) *ViewPort {
	vp := &ViewPort{
		LonMin: tile2lon(x, zoom),
		LonMax: tile2lon(x+1, zoom),
		LatMax: tile2lat(y, zoom),
		LatMin: tile2lat(y+1, zoom),
	}
	return vp
}

// convertPoly converts the polygon from the lat/lngs to the image coordinates.
func convertPoly(srcPoly [][][]float64, tiles [][][]int, zoom int) (ret [][][]float64) {
	bbMin := getTileBBox(tiles[len(tiles)-1][0][0], tiles[len(tiles)-1][0][1], zoom)
	bbMax := getTileBBox(tiles[0][len(tiles[0])-1][0], tiles[0][len(tiles[0])-1][1], zoom)
	bbXSize := bbMax.LonMax - bbMin.LonMin
	bbYSize := bbMax.LatMax - bbMin.LatMin

	ret = make([][][]float64, len(srcPoly))
	for i, loop := range srcPoly {
		ret[i] = make([][]float64, len(loop))
		for j, pt := range loop {
			ret[i][j] = make([]float64, 2)
			ret[i][j][0] = (1 - (bbMax.LonMax-pt[0])/bbXSize) * tileSize * float64(len(tiles[0]))
			ret[i][j][1] = (bbMax.LatMax - pt[1]) / bbYSize * tileSize * float64(len(tiles))
		}
	}
	return
}

// convertPoint converts point lat/lng to the image coordinates.
func convertPoint(ptLat, ptLon float64, tiles [][][]int, zoom int) (retX, retY float64) {
	bbMin := getTileBBox(tiles[len(tiles)-1][0][0], tiles[len(tiles)-1][0][1], zoom)
	bbMax := getTileBBox(tiles[0][len(tiles[0])-1][0], tiles[0][len(tiles[0])-1][1], zoom)
	bbXSize := bbMax.LonMax - bbMin.LonMin
	bbYSize := bbMax.LatMax - bbMin.LatMin

	retX = (1 - (bbMax.LonMax-ptLon)/bbXSize) * tileSize * float64(len(tiles[0]))
	retY = (bbMax.LatMax - ptLat) / bbYSize * tileSize * float64(len(tiles))
	return
}

// getAllTiles gets a matrix of tiles in OSM tile indices within a given bound.
func getAllTiles(xMin, xMax, yMin, yMax int) (tiles [][][]int) {
	tiles = make([][][]int, yMax-yMin+1)
	for tileY := yMin; tileY <= yMax; tileY++ {
		tiles[tileY-yMin] = make([][]int, xMax-xMin+1)
		for tileX := xMin; tileX <= xMax; tileX++ {
			tiles[tileY-yMin][tileX-xMin] = make([]int, 2)
			tiles[tileY-yMin][tileX-xMin][0] = tileX
			tiles[tileY-yMin][tileX-xMin][1] = tileY
		}
	}
	return
}

// tile2lon converts a tile x coordinate at zoom level z to longitude.
func tile2lon(x, z int) float64 {
	n := math.Exp2(float64(z)) // 2^z
	return float64(x)/n*360.0 - 180.0
}

// tile2lat converts a tile y coordinate at zoom level z to latitude.
func tile2lat(y, z int) float64 {
	n := math.Exp2(float64(z))
	latRad := math.Atan(math.Sinh(math.Pi * (1 - 2*float64(y)/n)))
	return latRad * 180 / math.Pi
}
