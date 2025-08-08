# Testing Guide for Report Listener

## Overview

This guide explains how to test the report-listener service locally, including the new dynamic `full_data` parameter functionality.

## Prerequisites

1. Go 1.19+ installed
2. MySQL database running with the cleanapp schema
3. `jq` installed for JSON formatting (optional but recommended)

## Running Tests

### 1. Unit Tests

```bash
cd report-listener
go test ./database -v
```

This will run the database tests including:

- `TestReportFilteringWithStatus`: Tests both `full_data=true` and `full_data=false` scenarios
- `TestGetReportsSince`: Tests the GetReportsSince function

### 2. Running the Service Locally

First, make sure your database is running and accessible. Then:

```bash
cd report-listener
go run main.go
```

The service will start on `localhost:8080` by default.

### 3. API Testing

#### Using the Test Script

```bash
chmod +x test_api.sh
./test_api.sh
```

#### Manual Testing with curl

**Get reports with analysis (default behavior):**

```bash
curl "http://localhost:8080/api/reports/last?n=5"
```

**Get reports with simplified analysis:**

```bash
curl "http://localhost:8080/api/reports/last?n=5&full_data=false&classification=physical"
```

**Get reports with digital classification:**

```bash
curl "http://localhost:8080/api/reports/last?n=5&full_data=false&classification=digital"
```

**Custom limit:**

```bash
curl "http://localhost:8080/api/reports/last?n=10&full_data=true&classification=physical"
```

**Error handling - invalid parameters:**

```bash
curl "http://localhost:8080/api/reports/last?n=invalid&classification=physical"
curl "http://localhost:8080/api/reports/last?n=5&full_data=invalid&classification=physical"
```

## API Endpoints

### GET /api/reports/last

Returns the last N analyzed reports.

**Query Parameters:**

- `n` (optional): Number of reports to return (default: 10, max: 50000)
- `full_data` (optional): Whether to include analysis data (default: true)
- `classification` (optional): Type of reports to return (default: "physical", options: "physical", "digital")

**Response Formats:**

When `full_data=true`:

```json
{
  "reports": [
    {
      "report": {
        "seq": 123,
        "timestamp": "2024-01-01T12:00:00Z",
        "id": "report_id",
        "latitude": 40.7128,
        "longitude": -74.006
      },
      "analysis": [
        {
          "seq": 123,
          "source": "ai_analysis",
          "title": "Litter Detected",
          "description": "Plastic bottle found",
          "litter_probability": 0.95,
          "hazard_probability": 0.1
        }
      ]
    }
  ],
  "count": 1,
  "from_seq": 123,
  "to_seq": 123
}
```

When `full_data=false`:

```json
{
  "reports": [
    {
      "report": {
        "seq": 123,
        "timestamp": "2024-01-01T12:00:00Z",
        "id": "report_id",
        "latitude": 40.7128,
        "longitude": -74.006
      },
      "analysis": [
        {
          "severity_level": 0.8,
          "classification": "physical"
        }
      ]
    }
  ],
  "count": 1,
  "from_seq": 123,
  "to_seq": 123
}
```

## Performance Considerations

- Use `full_data=false` when you only need basic report information (faster response)
- Use `full_data=true` when you need detailed analysis data
- The `n` parameter is limited to 50,000 to prevent abuse
- Reports are filtered to only include non-resolved reports (status = 'active' or no status)

## Troubleshooting

1. **Database connection issues**: Check your database configuration in `config/config.go`
2. **No reports returned**: Ensure your database has reports with analysis data
3. **Permission errors**: Make sure the database user has proper permissions
4. **Port conflicts**: Change the port in `main.go` if 8080 is already in use
