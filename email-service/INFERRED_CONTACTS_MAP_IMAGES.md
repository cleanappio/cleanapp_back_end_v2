# Inferred Contacts Map Image Generation

## Overview
This update ensures that emails sent to inferred contacts also include map images, providing geographic context even when no polygon features are available. The system now generates 1km maps centered on report coordinates for all email recipients.

## Changes Made

### 1. Enhanced Inferred Contacts Email Function
- **File**: `service/email_service.go`
- **Function**: `sendEmailsToInferredContacts`
- **Changes**:
  - Added map image generation using `email.GeneratePolygonImg(nil, lat, lon)`
  - Generates 1km map centered on report coordinates
  - Graceful fallback if map generation fails
  - Maintains email functionality even without map images

### 2. Map Image Generation Logic
- **Nil Feature Handling**: Uses the enhanced `GeneratePolygonImg` function
- **1km Bounding Box**: Automatically calculates appropriate zoom level and tile coverage
- **Centered on Report**: Map is centered on the report's latitude/longitude coordinates
- **No Polygon Overlay**: Clean map without area boundaries

## Map Image Behavior

### For Inferred Contacts:
```
1. Generate 1km bounding box centered on report coordinates
2. Calculate optimal zoom level (within maxTiles constraint)
3. Fetch OSM tiles for the bounding box
4. Create map image with report point marker
5. Attach map image to email
```

### For Area Contacts (Existing):
```
1. Use polygon feature to determine bounding box
2. Calculate appropriate zoom level for polygon coverage
3. Fetch OSM tiles for the polygon area
4. Create map image with polygon overlay and report point
5. Attach map image to email
```

## Technical Implementation

### Map Generation Call:
```go
// Generate map image for inferred contacts (1km map centered on report coordinates)
mapImg, err := email.GeneratePolygonImg(nil, report.Latitude, report.Longitude)
if err != nil {
    log.Warnf("Failed to generate map image for report %d: %v, sending email without map", report.Seq, err)
    // Continue without map image
}

// Send emails with analysis data and map image
return s.email.SendEmailsWithAnalysis(validEmails, report.Image, mapImg, analysis)
```

### Error Handling:
- **Map generation fails**: Email sent without map image
- **Logging**: Warning message with error details
- **Graceful degradation**: Service continues to function

## Benefits

### 1. **Consistent User Experience**
- All email recipients now receive map images
- Geographic context provided regardless of contact type
- Uniform email format across different scenarios

### 2. **Enhanced Information**
- Users can see where the report was filed
- 1km context provides neighborhood-level detail
- No need for external mapping tools

### 3. **Professional Appearance**
- Emails look complete and informative
- Consistent with area-based email format
- Better user engagement

### 4. **Geographic Context**
- Helps users understand report location
- Useful for reports without specific area associations
- Supports both urban and rural report locations

## Map Image Specifications

### Coverage Area:
- **Size**: 1km × 1km (approximate)
- **Center**: Report coordinates (latitude/longitude)
- **Zoom Level**: Automatically optimized for tile count constraints

### Image Content:
- **Base Map**: OpenStreetMap tiles
- **Report Point**: Red marker at report location
- **No Polygons**: Clean map without area boundaries
- **Format**: PNG image with transparency support

### Performance Considerations:
- **Tile Count**: Limited to `maxTiles` (16) for performance
- **Zoom Optimization**: Automatic selection of appropriate zoom level
- **Caching**: OSM tiles fetched on-demand

## Use Cases

### 1. **Standalone Reports**
- Reports not associated with specific areas
- Individual user submissions
- General location-based issues

### 2. **Digital Reports**
- Digital issues that need geographic context
- Reports without physical area boundaries
- General location awareness

### 3. **Fallback Scenarios**
- When area data is unavailable
- System errors in area detection
- New report types without area associations

## Example Scenarios

### Scenario 1: Inferred Contact Email
```
Report: Litter found in city park
Coordinates: 40.7128°N, -74.0060°W
Map: 1km area around Central Park
Recipients: contact@city.gov, support@cleanup.org
```

### Scenario 2: Area Contact Email
```
Report: Hazard on Main Street
Coordinates: 40.7589°N, -73.9851°W
Map: Downtown district boundaries + report point
Recipients: downtown@city.gov, safety@mainstreet.org
```

## Backward Compatibility

- **No breaking changes**: Existing functionality preserved
- **Enhanced functionality**: Inferred contacts now get map images
- **Same email format**: Consistent structure across all email types
- **Fallback support**: Emails work even if map generation fails

## Testing

### Test Scenarios:
1. **Inferred contacts with map**: Verify 1km map generation
2. **Area contacts with polygon**: Verify existing polygon functionality
3. **Map generation failure**: Verify graceful fallback
4. **Coordinate variations**: Test different latitude/longitude combinations

### Verification Points:
- Map images are generated for inferred contacts
- 1km coverage area is appropriate
- Report point is correctly positioned
- Email attachments include map images
- Error handling works correctly

## Summary

The inferred contacts map image generation provides:
- **Consistent experience** across all email types
- **Geographic context** for all report emails
- **Professional appearance** with complete information
- **Robust error handling** for reliable operation
- **Performance optimization** through automatic zoom selection

This enhancement ensures that all email recipients receive comprehensive, informative emails with geographic context, improving user experience and engagement while maintaining system reliability.
