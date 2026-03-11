package geojsonx

import (
	"encoding/json"
	"fmt"
	"math"
)

type Bounds struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

func NewBounds(west, south, east, north float64) Bounds {
	return Bounds{West: west, South: south, East: east, North: north}
}

func (b Bounds) Valid() bool {
	return !math.IsNaN(b.West) && !math.IsNaN(b.South) && !math.IsNaN(b.East) && !math.IsNaN(b.North) &&
		b.East >= b.West && b.North >= b.South
}

func (b Bounds) Expand(delta float64) Bounds {
	if delta <= 0 {
		return b
	}
	return Bounds{
		West:  b.West - delta,
		South: b.South - delta,
		East:  b.East + delta,
		North: b.North + delta,
	}
}

func (b Bounds) Center() (float64, float64) {
	return (b.South + b.North) / 2, (b.West + b.East) / 2
}

func (b Bounds) Intersects(other Bounds) bool {
	if !b.Valid() || !other.Valid() {
		return false
	}
	return b.West <= other.East && b.East >= other.West && b.South <= other.North && b.North >= other.South
}

func (b Bounds) Area() float64 {
	if !b.Valid() {
		return 0
	}
	return math.Max(0, b.East-b.West) * math.Max(0, b.North-b.South)
}

func (b Bounds) IntersectionRatio(other Bounds) float64 {
	if !b.Intersects(other) {
		return 0
	}
	inter := Bounds{
		West:  math.Max(b.West, other.West),
		South: math.Max(b.South, other.South),
		East:  math.Min(b.East, other.East),
		North: math.Min(b.North, other.North),
	}
	interArea := inter.Area()
	if interArea <= 0 {
		return 0
	}
	unionArea := b.Area() + other.Area() - interArea
	if unionArea <= 0 {
		return 0
	}
	return interArea / unionArea
}

func (b Bounds) ToJSONArray() ([]byte, error) {
	return json.Marshal([]float64{b.West, b.South, b.East, b.North})
}

func ParseBoundsJSON(raw string) (*Bounds, error) {
	if raw == "" {
		return nil, nil
	}
	var arr []float64
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) == 4 {
		bounds := Bounds{West: arr[0], South: arr[1], East: arr[2], North: arr[3]}
		if !bounds.Valid() {
			return nil, fmt.Errorf("invalid bounds json")
		}
		return &bounds, nil
	}
	var bounds Bounds
	if err := json.Unmarshal([]byte(raw), &bounds); err != nil {
		return nil, err
	}
	if !bounds.Valid() {
		return nil, fmt.Errorf("invalid bounds json")
	}
	return &bounds, nil
}

func BoundsFromJSON(raw string) (*Bounds, error) {
	if raw == "" {
		return nil, nil
	}
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return BoundsFromValue(value)
}

func BoundsFromValue(value interface{}) (*Bounds, error) {
	bounds := &Bounds{
		West:  math.Inf(1),
		South: math.Inf(1),
		East:  math.Inf(-1),
		North: math.Inf(-1),
	}
	if !walkValue(value, bounds) {
		return nil, fmt.Errorf("unable to derive bounds from geometry")
	}
	if !bounds.Valid() {
		return nil, fmt.Errorf("derived invalid bounds from geometry")
	}
	return bounds, nil
}

func BuildAggregateGeometryJSON(rawGeometries []string) (string, string, error) {
	features := make([]map[string]interface{}, 0, len(rawGeometries))
	var aggregate *Bounds
	for _, raw := range rawGeometries {
		if raw == "" {
			continue
		}
		var value interface{}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return "", "", err
		}
		bounds, err := BoundsFromValue(value)
		if err == nil && bounds != nil {
			if aggregate == nil {
				copyBounds := *bounds
				aggregate = &copyBounds
			} else {
				aggregate.West = math.Min(aggregate.West, bounds.West)
				aggregate.South = math.Min(aggregate.South, bounds.South)
				aggregate.East = math.Max(aggregate.East, bounds.East)
				aggregate.North = math.Max(aggregate.North, bounds.North)
			}
		}
		switch item := value.(type) {
		case map[string]interface{}:
			typ, _ := item["type"].(string)
			switch typ {
			case "Feature":
				features = append(features, item)
			case "FeatureCollection":
				nested, _ := item["features"].([]interface{})
				for _, rawFeature := range nested {
					if feature, ok := rawFeature.(map[string]interface{}); ok {
						features = append(features, feature)
					}
				}
			default:
				features = append(features, map[string]interface{}{
					"type":       "Feature",
					"properties": map[string]interface{}{},
					"geometry":   item,
				})
			}
		}
	}

	if len(features) == 0 {
		return "", "", nil
	}
	aggregateValue := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}
	aggregateJSONBytes, err := json.Marshal(aggregateValue)
	if err != nil {
		return "", "", err
	}

	var bboxJSON string
	if aggregate != nil && aggregate.Valid() {
		boundsBytes, err := aggregate.ToJSONArray()
		if err != nil {
			return "", "", err
		}
		bboxJSON = string(boundsBytes)
	}
	return string(aggregateJSONBytes), bboxJSON, nil
}

func walkValue(value interface{}, bounds *Bounds) bool {
	switch typed := value.(type) {
	case map[string]interface{}:
		typ, _ := typed["type"].(string)
		switch typ {
		case "Feature":
			return walkValue(typed["geometry"], bounds)
		case "FeatureCollection":
			found := false
			items, _ := typed["features"].([]interface{})
			for _, item := range items {
				found = walkValue(item, bounds) || found
			}
			return found
		case "GeometryCollection":
			found := false
			items, _ := typed["geometries"].([]interface{})
			for _, item := range items {
				found = walkValue(item, bounds) || found
			}
			return found
		case "Point":
			coords, _ := typed["coordinates"].([]interface{})
			return walkCoordinates(coords, bounds)
		case "MultiPoint", "LineString":
			items, _ := typed["coordinates"].([]interface{})
			found := false
			for _, item := range items {
				coords, _ := item.([]interface{})
				found = walkCoordinates(coords, bounds) || found
			}
			return found
		case "MultiLineString", "Polygon":
			items, _ := typed["coordinates"].([]interface{})
			found := false
			for _, ring := range items {
				points, _ := ring.([]interface{})
				for _, point := range points {
					coords, _ := point.([]interface{})
					found = walkCoordinates(coords, bounds) || found
				}
			}
			return found
		case "MultiPolygon":
			items, _ := typed["coordinates"].([]interface{})
			found := false
			for _, polygon := range items {
				rings, _ := polygon.([]interface{})
				for _, ring := range rings {
					points, _ := ring.([]interface{})
					for _, point := range points {
						coords, _ := point.([]interface{})
						found = walkCoordinates(coords, bounds) || found
					}
				}
			}
			return found
		}
	case []interface{}:
		found := false
		for _, item := range typed {
			found = walkValue(item, bounds) || found
		}
		return found
	}
	return false
}

func walkCoordinates(coords []interface{}, bounds *Bounds) bool {
	if len(coords) < 2 {
		return false
	}
	lng, okLng := toFloat(coords[0])
	lat, okLat := toFloat(coords[1])
	if !okLng || !okLat {
		return false
	}
	if math.IsInf(bounds.West, 1) {
		bounds.West = lng
		bounds.South = lat
		bounds.East = lng
		bounds.North = lat
		return true
	}
	bounds.West = math.Min(bounds.West, lng)
	bounds.South = math.Min(bounds.South, lat)
	bounds.East = math.Max(bounds.East, lng)
	bounds.North = math.Max(bounds.North, lat)
	return true
}

func toFloat(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		return v, err == nil
	default:
		return 0, false
	}
}
