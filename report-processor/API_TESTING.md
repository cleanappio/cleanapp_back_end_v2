# Report Processor API Testing

This directory contains Python scripts for testing the `/api/v3/match_report` endpoint.

## Prerequisites

- Python 3.6+
- `requests` library: `pip install requests`
- Report processor service running
- Valid image files (JPG, PNG)

## Scripts

### 1. `test_match_report_api.py` - Command Line Testing

A comprehensive command-line tool for testing the API with various parameters.

#### Usage

```bash
# Basic usage
python test_match_report_api.py <image_path> <latitude> <longitude> [service_url]

# Examples
python test_match_report_api.py image.jpg 40.7128 -74.0060
python test_match_report_api.py photo.png 51.5074 -0.1278 http://localhost:8080
python test_match_report_api.py test.jpg 37.7749 -122.4194 https://api.example.com
```

#### Features

- ✅ Validates image file existence and format
- ✅ Validates coordinate ranges (latitude: -90 to 90, longitude: -180 to 180)
- ✅ Converts images to byte arrays automatically
- ✅ Displays detailed results including similarity scores and resolution status
- ✅ Highlights automatically resolved reports
- ✅ Comprehensive error handling and user feedback
- ✅ Configurable service URL

#### Output Example

```
============================================================
🧪 Testing /api/v3/match_report endpoint
============================================================
Reading image from: test_image.jpg
Sending request to: http://localhost:8080/api/v3/match_report
Location: 40.7128, -74.0060
Image size: 245760 bytes

Response Status: 200
✅ Request successful!
Message: Report matching completed. 2 reports resolved out of 5 compared.

Found 5 reports to compare:

  Report 1:
    Sequence: 123
    Similarity: 0.850
    Resolved: True
    🎉 This report was automatically resolved!

  Report 2:
    Sequence: 124
    Similarity: 0.420
    Resolved: False

  Report 3:
    Sequence: 125
    Similarity: 0.920
    Resolved: True
    🎉 This report was automatically resolved!

============================================================
✅ Test completed successfully!
============================================================
```

### 2. `example_usage.py` - Programmatic Integration

A Python class demonstrating how to integrate the API into your application.

#### Features

- ✅ `CleanAppReportMatcher` class for easy integration
- ✅ Helper methods for filtering results
- ✅ Batch processing examples
- ✅ Error handling and timeout management
- ✅ Configurable service URL

#### Usage

```python
from example_usage import CleanAppReportMatcher

# Initialize client
matcher = CleanAppReportMatcher("http://localhost:8080")

# Match a single report
result = matcher.match_report("image.jpg", 40.7128, -74.0060)

# Get only resolved reports
resolved = matcher.get_resolved_reports(result)

# Get high similarity reports
high_sim = matcher.get_high_similarity_reports(result, threshold=0.7)
```

## API Endpoint Details

### Request Format

```json
{
  "version": "2.0",
  "id": "string",
  "latitude": 40.7128,
  "longitude": -74.0060,
  "x": 0.5,
  "y": 0.5,
  "image": [byte_array]
}
```

**Note**: The `image` field contains the raw image bytes as an array of integers (0-255), not a base64 encoded string. For example, a small image might be represented as `[255, 216, 255, 224, 0, 16, 74, 70, 73, 70, ...]` where each number represents one byte of the image file.

### Response Format

```json
{
  "success": true,
  "message": "Report matching completed. X reports resolved out of Y compared.",
  "results": [
    {
      "report_seq": 123,
      "similarity": 0.850,
      "resolved": true
    }
  ]
}
```

## Configuration

### Environment Variables

The report processor service uses these environment variables:

```bash
# Required for image comparison
OPENAI_API_KEY=your_openai_api_key_here
OPENAI_MODEL=gpt-4o

# Optional configuration
REPORTS_RADIUS_METERS=10.0
PORT=8080
DB_HOST=localhost
DB_PORT=3306
DB_USER=server
DB_PASSWORD=secret_app
DB_NAME=cleanapp
```

### Service Startup

```bash
# Start the service
cd report-processor
go run main.go

# Or with environment variables
OPENAI_API_KEY=your_key REPORTS_RADIUS_METERS=20.0 go run main.go
```

## Testing Workflow

1. **Start the service** with proper configuration
2. **Prepare test images** (JPG/PNG format)
3. **Run the test script** with your image and coordinates
4. **Check the results** for similarity scores and resolution status
5. **Verify database updates** - resolved reports should be marked in `report_status` table

## Troubleshooting

### Common Issues

1. **Connection Error**: Make sure the service is running on the specified URL
2. **Image Format Error**: Use JPG or PNG files
3. **Coordinate Error**: Ensure latitude (-90 to 90) and longitude (-180 to 180) are valid
4. **OpenAI Error**: Check that `OPENAI_API_KEY` is set correctly
5. **Database Error**: Verify database connection and table existence

### Debug Mode

Enable detailed logging by setting the log level:

```bash
LOG_LEVEL=debug go run main.go
```

### Health Check

Test if the service is running:

```bash
curl http://localhost:8080/health
```

Expected response:
```json
{
  "status": "healthy",
  "service": "report-processor",
  "time": "2024-01-01T12:00:00Z"
}
```

## Performance Notes

- Image comparison uses OpenAI's vision API, which may take 5-15 seconds per comparison
- The service processes reports in sequence, so response time scales with the number of nearby reports
- Large images are sent as byte arrays, which increases payload size
- Consider image compression for better performance with large files
