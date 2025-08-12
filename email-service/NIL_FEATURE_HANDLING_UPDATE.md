# Nil Feature Handling Update

## Overview
This update modifies the `GeneratePolygonImg` function to handle cases where the feature parameter is nil. When no polygon feature is provided, the function now generates a 1km map centered on the report coordinates instead of failing.

## Changes Made

### 1. Function Signature and Logic Update
- **File**: `email/draw_image.go`
- **Function**: `GeneratePolygonImg`
- **Changes**:
  - Added nil feature handling logic
  - Conditional execution based on feature availability
  - Fallback to 1km bounding box when feature is nil

### 2. New Function: `generate1kmBoundingBox`
- **Purpose**: Generates a 1km bounding box centered on report coordinates
- **Input**: `centerLat`, `centerLon` (report coordinates)
- **Output**: `zoom`, `xMin`, `xMax`, `yMin`, `yMax` (tile coordinates)
- **Features**:
  - Accurate 1km calculation considering latitude variations
  - Automatic zoom level selection within `maxTiles` constraint
  - Proper tile coordinate calculation

### 3. Polygon Drawing Safety
- **File**: `email/draw_image.go`
- **Function**: `generate`
- **Changes**:
  - Added nil check before attempting to draw polygons
  - Prevents runtime errors when feature is nil
  - Maintains functionality for valid features

## Technical Details

### 1km Bounding Box Calculation

The function calculates the appropriate degrees to represent 1km:

```go
// 1 degree of latitude ≈ 111.32 km (constant)
latDegrees := 1.0 / 111.32

// 1 degree of longitude ≈ 111.32 * cos(latitude) km (varies by latitude)
lonDegrees := 1.0 / (111.32 * math.Cos(centerLat*math.Pi/180.0))

// Use the larger to ensure we cover at least 1km in both directions
kmInDegrees := math.Max(latDegrees, lonDegrees)
```

### Zoom Level Selection

The function automatically selects the appropriate zoom level:

```go
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
```

## Use Cases

### 1. With Polygon Feature (Existing Behavior)
```go
// When a valid polygon feature is provided
feature := &geojson.Feature{...}
image, err := GeneratePolygonImg(feature, reportLat, reportLon)
// Generates map with polygon overlay
```

### 2. Without Polygon Feature (New Behavior)
```go
// When no polygon feature is available
image, err := GeneratePolygonImg(nil, reportLat, reportLon)
// Generates 1km map centered on report coordinates
```

## Benefits

1. **Error Prevention**: Eliminates crashes when feature is nil
2. **Fallback Support**: Provides useful map output even without polygon data
3. **Flexible Usage**: Supports both polygon and non-polygon scenarios
4. **Accurate Mapping**: 1km bounding box ensures appropriate map coverage
5. **Performance Optimization**: Automatic zoom level selection within tile limits

## Map Output

### With Feature:
- Shows polygon boundaries
- Includes report point marker
- Covers polygon area with appropriate zoom

### Without Feature:
- Shows 1km area around report point
- Includes report point marker
- No polygon overlay
- Appropriate zoom level for 1km coverage

## Example Scenarios

### Scenario 1: Area-based Reports
```go
// Report is associated with a specific area
areaFeature := getAreaFeature(reportID)
image, err := GeneratePolygonImg(areaFeature, report.Lat, report.Lon)
// Shows area boundaries + report point
```

### Scenario 2: Standalone Reports
```go
// Report has no associated area
image, err := GeneratePolygonImg(nil, report.Lat, report.Lon)
// Shows 1km map centered on report point
```

### Scenario 3: Digital Reports
```go
// Digital reports may not have geographic areas
image, err := GeneratePolygonImg(nil, report.Lat, report.Lon)
// Shows 1km map for context
```

## Backward Compatibility

- **No breaking changes**: Existing code continues to work
- **Enhanced functionality**: New nil handling capability
- **Same output format**: Both scenarios produce valid PNG images
- **Consistent API**: Function signature remains unchanged

## Testing

The changes can be verified by:

1. **With Feature**: Test existing polygon functionality
2. **Without Feature**: Test new nil handling
3. **Edge Cases**: Test various coordinate combinations
4. **Performance**: Verify zoom level selection efficiency
5. **Output Quality**: Check map coverage and resolution

## Summary

The `GeneratePolygonImg` function now gracefully handles nil features by:
- Generating appropriate 1km bounding boxes
- Calculating optimal zoom levels
- Preventing runtime errors
- Maintaining consistent output quality
- Supporting both polygon and non-polygon use cases

This enhancement makes the email service more robust and flexible while maintaining all existing functionality.
