# Responses Processing Implementation

## Overview

This implementation adds responses processing to the report-processor service. When a report is marked as resolved, a corresponding response entry is automatically created with a "verified" status.

## Database Changes

### New Table: `responses`

The `responses` table has an identical structure to the `reports` table, plus additional `status` and `report_seq` fields:

```sql
CREATE TABLE IF NOT EXISTS responses(
  seq INT NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP default current_timestamp,
  id VARCHAR(255) NOT NULL,
  team INT NOT NULL,
  latitude FLOAT NOT NULL,
  longitude FLOAT NOT NULL,
  x FLOAT,
  y FLOAT,
  image LONGBLOB NOT NULL,
  action_id VARCHAR(32),
  description VARCHAR(255),
  status ENUM('resolved', 'verified') NOT NULL DEFAULT 'resolved',
  report_seq INT NOT NULL, -- Reference to the resolved report
  PRIMARY KEY (seq),
  INDEX id_index (id),
  INDEX action_idx (action_id),
  INDEX latitude_index (latitude),
  INDEX longitude_index (longitude),
  INDEX status_index (status),
  INDEX report_seq_index (report_seq),
  FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
);
```

## API Endpoints

### Public Endpoints

#### GET `/api/v3/responses/get?seq=123`
Gets a specific response by sequence number.

**Response:**
```json
{
  "success": true,
  "data": {
    "seq": 123,
    "id": "user123",
    "team": 0,
    "latitude": 40.7128,
    "longitude": -74.0060,
    "x": 0.5,
    "y": 0.5,
    "image": "...",
    "action_id": null,
    "status": "verified",
    "report_seq": 456
  }
}
```

#### GET `/api/v3/responses/by_status?status=verified`
Gets all responses with a specific status.

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "seq": 123,
      "id": "user123",
      "team": 0,
      "latitude": 40.7128,
      "longitude": -74.0060,
      "x": 0.5,
      "y": 0.5,
      "image": "...",
      "action_id": null,
      "status": "verified",
      "report_seq": 456
    }
  ],
  "count": 1
}
```

## Automatic Response Creation

The system automatically creates responses when reports are resolved through the `/api/v3/match_report` endpoint:

### Report Matching Resolution
When the `/api/v3/match_report` endpoint processes reports and determines that a report should be resolved (based on image similarity and litter removal detection), the system automatically:
1. Marks the report as resolved in the `report_status` table
2. Creates a corresponding entry in the `responses` table with status "verified" using the match request data
3. Increments the user's `kitns_daily` field by 1

The response is created directly from the match request data, including:
- User ID, coordinates, and image from the match request
- Team is set to 0 (UNKNOWN) since it's not available in the match request
- Action ID is set to null since it's not available in the match request
- Report sequence number (report_seq) is set to the sequence of the resolved report

**Transaction Safety**: Both the response creation and the `kitns_daily` increment are performed within a single database transaction, ensuring that either both operations succeed or both fail together. This maintains data consistency and prevents partial updates.

This ensures that every report resolved through the matching process has a corresponding verified response entry with the actual data from the resolution request, a reference to the original report, and the user is properly rewarded with an incremented daily kitn count.

**Note**: Manual report resolution via `/api/v3/reports/mark_resolved` only marks the report as resolved but does not create a response entry. Responses are only created through the automated matching process.

## Database Service Methods

### New Methods Added

- `EnsureResponsesTable(ctx context.Context) error` - Creates the responses table if it doesn't exist
- `CreateResponseFromReport(ctx context.Context, reportSeq int, status string) (*models.Response, error)` - Creates a response from a report
- `CreateResponseFromMatchRequest(ctx context.Context, req models.MatchReportRequest, reportSeq int, status string) (*models.Response, error)` - Creates a response from match request data and increments user's kitn_daily
- `GetResponse(ctx context.Context, seq int) (*models.Response, error)` - Gets a response by sequence
- `GetResponsesByStatus(ctx context.Context, status string) ([]models.Response, error)` - Gets responses by status

### Modified Methods

- `MarkReportResolved(ctx context.Context, seq int) error` - Marks a report as resolved (no longer creates responses automatically)

## Models

### New Models Added

- `Response` - Represents a response entry

## Testing

Tests have been added to verify:
- Responses table creation
- Response creation from non-existent reports (should fail)
- Basic functionality of the new database methods

## Usage Example

1. A report is submitted and stored in the `reports` table
2. When the report is processed through `/api/v3/match_report` and determined to be resolved, the system automatically:
   - Marks the report as resolved
   - Creates a verified response entry using the match request data (user ID, coordinates, image, etc.)
   - Increments the user's `kitns_daily` field by 1
3. Reports can also be manually marked as resolved using `/api/v3/reports/mark_resolved` (but this does not create a response or increment kitn_daily)
4. You can query responses using the new endpoints to track resolution status

## Status Values

- `resolved`: Status for responses created manually (not used in current implementation)
- `verified`: Status when a response is created automatically from a resolved report through the matching process

The system ensures that when a report is resolved through the matching process, a verified response is created, providing a complete audit trail of the automated resolution process.
