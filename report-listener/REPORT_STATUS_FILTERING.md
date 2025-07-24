# Report Status Filtering in Report-Listener

## Overview

The report-listener service has been updated to filter reports based on their status in the `report_status` table. This ensures that only non-resolved reports are fetched and broadcasted to clients.

## Changes Made

### 1. Updated Database Queries

All report fetching functions now use a `LEFT JOIN` with the `report_status` table and filter for reports that are either:
- Not in the `report_status` table (no status entry)
- Have status = 'active'

Reports with status = 'resolved' are excluded from all queries.

### 2. Modified Functions

The following functions have been updated:

#### `GetReportsSince(ctx context.Context, sinceSeq int)`
- **Purpose**: Fetches reports with analysis since a given sequence number
- **Filter**: `WHERE r.seq > ? AND (rs.status IS NULL OR rs.status = 'active')`
- **Use Case**: Used for real-time broadcasting of new reports

#### `GetLastNAnalyzedReports(ctx context.Context, limit int)`
- **Purpose**: Fetches the last N analyzed reports
- **Filter**: `WHERE (rs.status IS NULL OR rs.status = 'active')`
- **Use Case**: Used for initial data loading and API endpoints

#### `GetReportBySeq(ctx context.Context, seq int)`
- **Purpose**: Fetches a single report by sequence ID
- **Filter**: `WHERE r.seq = ? AND (rs.status IS NULL OR rs.status = 'active')`
- **Use Case**: Used for individual report retrieval

#### `GetLastNReportsByID(ctx context.Context, reportID string, limit int)`
- **Purpose**: Fetches the last N reports for a specific user ID
- **Filter**: `WHERE r.id = ? AND (rs.status IS NULL OR rs.status = 'active')`
- **Use Case**: Used for user-specific report history

### 3. SQL Query Structure

All queries now follow this pattern:

```sql
SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude
FROM reports r
INNER JOIN report_analysis ra ON r.seq = ra.seq
LEFT JOIN report_status rs ON r.seq = rs.seq
WHERE [original_conditions] 
  AND (rs.status IS NULL OR rs.status = 'active')
ORDER BY [ordering]
```

### 4. Error Message Updates

- Updated error messages to reflect that reports might not be found due to being resolved
- Example: `"report with seq %d not found or is resolved"`

## Benefits

1. **Automatic Filtering**: Reports marked as resolved are automatically excluded from all queries
2. **Real-time Updates**: When a report is marked as resolved via the report-processor service, it immediately stops appearing in the report-listener
3. **Consistent Behavior**: All endpoints and WebSocket broadcasts respect the resolved status
4. **Performance**: Filtering happens at the database level, reducing data transfer

## Integration with Report-Processor

This change works in conjunction with the `report-processor` service:

1. **Report-Processor**: Marks reports as resolved via `/api/v3/reports/mark_resolved`
2. **Report-Listener**: Automatically excludes resolved reports from all queries
3. **Real-time Effect**: Changes are immediately reflected in WebSocket broadcasts

## Testing

Added test functions to verify the filtering logic:

- `TestReportFilteringWithStatus`: Tests `GetLastNAnalyzedReports`
- `TestGetReportsSince`: Tests `GetReportsSince`

These tests verify that:
- Only non-resolved reports are returned
- Reports with no status entry are included
- Reports with status = 'active' are included
- Reports with status = 'resolved' are excluded

## Database Schema Dependency

This implementation assumes the existence of the `report_status` table:

```sql
CREATE TABLE report_status (
    seq INT NOT NULL,
    status ENUM('active', 'resolved') NOT NULL DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (seq),
    FOREIGN KEY (seq) REFERENCES reports(seq) ON DELETE CASCADE
);
```

## Backward Compatibility

- Reports without status entries continue to work normally
- Existing functionality is preserved for non-resolved reports
- No breaking changes to API responses or WebSocket messages
- Only the filtering behavior has changed

## Example Scenarios

### Scenario 1: New Report
1. Report is created in `reports` table
2. Analysis is added to `report_analysis` table
3. No entry in `report_status` table
4. **Result**: Report appears in report-listener

### Scenario 2: Active Report
1. Report exists in `reports` table
2. Analysis exists in `report_analysis` table
3. Entry in `report_status` with status = 'active'
4. **Result**: Report appears in report-listener

### Scenario 3: Resolved Report
1. Report exists in `reports` table
2. Analysis exists in `report_analysis` table
3. Entry in `report_status` with status = 'resolved'
4. **Result**: Report is excluded from report-listener

### Scenario 4: Report Marked as Resolved
1. User calls report-processor `/api/v3/reports/mark_resolved` with seq = 123
2. Report status is updated to 'resolved'
3. **Result**: Report immediately disappears from report-listener broadcasts 