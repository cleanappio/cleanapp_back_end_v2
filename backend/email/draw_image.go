package email

import (
	"bytes"
	"cleanapp/backend/server/api"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"

	"github.com/fogleman/gg"
	geojson "github.com/paulmach/go.geojson"
)

const tileSize = 256
const maxTiles = 16

// GeneratePolygonImg drwas an OSM image for a given polygon feature and draws a given polygon on it.
func GeneratePolygonImg(feature *geojson.Feature, reportLat, reportLon float64) ([]byte, error) {
	zoom, xMin, xMax, yMin, yMax, err := boundToZoomXY(feature)
	if err != nil {
		return nil, err
	}

	return generate(xMin, xMax, yMin, yMax, zoom, feature, reportLat, reportLon)
}

// fetchTile downloads the OSM tile and saves it as a PNG file with a polygon drawn on it
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
	for j, col := range imgs {
		for i, img := range col {
			ic := getImageCoords(i, j)
			draw.Draw(dst, image.Rectangle{Min: ic, Max: image.Point{X: ic.X + tileSize, Y: ic.Y + tileSize}}, img, image.Point{}, draw.Src)
		}
	}

	// Draw polygon
	dc := gg.NewContextForRGBA(dst)
	dc.SetColor(color.RGBA{219, 33, 213, 255})
	dc.SetLineWidth(3)
	dc.SetFillStyle(gg.NewSolidPattern(color.RGBA{36, 222, 42, 20}))

	if feature.Geometry.IsPolygon() {
		drawPolygon(dc, convertPoly(feature.Geometry.Polygon, tiles, zoom))
	} else if feature.Geometry.IsMultiPolygon() {
		for _, poly := range feature.Geometry.MultiPolygon {
			drawPolygon(dc, convertPoly(poly, tiles, zoom))
		}
	}

	// Draw a report point
	dc.SetColor(color.RGBA{214, 0, 0, 255})
	dc.SetLineWidth(2)
	dc.SetFillStyle(gg.NewSolidPattern(color.RGBA{255, 0, 0, 255}))
	ptX, ptY := convertPoint(reportLat, reportLon, tiles, zoom)
	dc.NewSubPath()
	dc.DrawCircle(ptX, ptY, 15)
	dc.ClosePath()
	dc.FillPreserve()
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

	client := &http.Client{}
	req, err := http.NewRequest("GET", tileURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "CleanApp/2.0")

	resp, err := client.Do(req)
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
func computeBoundingBox(feature *geojson.Feature) (*api.ViewPort, error) {
	if feature.Geometry.IsPolygon() {
		return computeBoundingBoxPolygon(feature.Geometry.Polygon), nil
	} else if feature.Geometry.IsMultiPolygon() {
		return computeBoundingBoxMultiPolygon(feature.Geometry.MultiPolygon), nil
	}
	return nil, fmt.Errorf("unsupported geometry type %v", feature.Geometry.Type)
}

// computeBoundingBoxPolygon computes a lat/lng bounding box for a polygon
func computeBoundingBoxPolygon(coords [][][]float64) *api.ViewPort {
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

	return &api.ViewPort{
		LonMin: minLon,
		LatMin: minLat,
		LonMax: maxLon,
		LatMax: maxLat,
	}
}

// computeBoundingBoxMultiPolygon computes a lat/lng bounding box for a multiPolygon.
func computeBoundingBoxMultiPolygon(coords [][][][]float64) *api.ViewPort {
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

	return &api.ViewPort{
		LonMin: minLon,
		LatMin: minLat,
		LonMax: maxLon,
		LatMax: maxLat,
	}
}

// drqwPolygon draws the polygon inside a given image.
func drawPolygon(dc *gg.Context, poly [][][]float64) {
	for _, loop := range poly {
		dc.NewSubPath()
		dc.MoveTo(loop[0][0], loop[0][1])
		for _, point := range loop[1:] {
			dc.LineTo(point[0], point[1])
		}
		dc.ClosePath()
		dc.FillPreserve()
		dc.Stroke()
	}
}

// getTileBBox computes the bounding box for a tile given its x, y, and z.
func getTileBBox(x, y, zoom int) *api.ViewPort {
	vp := &api.ViewPort{
		LonMin: tile2lon(x, zoom),
		LonMax: tile2lon(x+1, zoom),
		LatMax: tile2lat(y, zoom),
		LatMin: tile2lat(y+1, zoom),
	}
	return vp
}

// convertPoly converts the polygon from the lat/lngs to the image coordinates.
func convertPoly(srcPoly [][][]float64, tiles [][][]int, zoom int) (ret [][][]float64) {
	bbMin := getTileBBox(tiles[len(tiles) - 1][0][0], tiles[len(tiles) - 1][0][1], zoom)
	bbMax := getTileBBox(tiles[0][len(tiles[0]) - 1][0], tiles[0][len(tiles[0]) - 1][1], zoom)
	bbXSize := bbMax.LonMax - bbMin.LonMin
	bbYSize := bbMax.LatMax - bbMin.LatMin

	ret = make([][][]float64, len(srcPoly))
	for i, loop := range srcPoly {
		ret[i] = make([][]float64, len(loop))
		for j, pt := range loop {
			ret[i][j] = make([]float64, 2)
			ret[i][j][0] = (1-(bbMax.LonMax-pt[0])/bbXSize)*tileSize*float64(len(tiles[0]))
			ret[i][j][1] = (bbMax.LatMax-pt[1])/bbYSize*tileSize*float64(len(tiles))
		}
	}
	return
}

// convertPoint converts point lat/lng to the image coordinates.
func convertPoint(ptLat, ptLon float64, tiles [][][]int, zoom int) (retX, retY float64) {
	bbMin := getTileBBox(tiles[len(tiles) - 1][0][0], tiles[len(tiles) - 1][0][1], zoom)
	bbMax := getTileBBox(tiles[0][len(tiles[0]) - 1][0], tiles[0][len(tiles[0]) - 1][1], zoom)
	bbXSize := bbMax.LonMax - bbMin.LonMin
	bbYSize := bbMax.LatMax - bbMin.LatMin

	retX = (1-(bbMax.LonMax-ptLon)/bbXSize)*tileSize*float64(len(tiles[0]))
	retY = (bbMax.LatMax-ptLat)/bbYSize*tileSize*float64(len(tiles))
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
