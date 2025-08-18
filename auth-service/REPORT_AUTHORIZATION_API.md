# Report Authorization API

## Overview

The Report Authorization API allows authenticated users to check if they are authorized to view specific reports based on brand ownership and area restrictions.

## Endpoint

```
POST /api/v3/reports/authorization
```

**Authentication Required**: Yes (JWT token in Authorization header)

## Request

### Headers
```
Authorization: Bearer <jwt_token>
Content-Type: application/json
```

### Body
```json
{
  "report_seqs": [123, 456, 789]
}
```

**Parameters:**
- `report_seqs` (array of integers, required): List of report sequence numbers to check authorization for

## Response

### Success Response (200 OK)
```json
{
  "authorizations": [
    {
      "report_seq": 123,
      "authorized": true,
      "reason": "Location not within any restricted area"
    },
    {
      "report_seq": 456,
      "authorized": true,
      "reason": "Brand belongs to user"
    },
    {
      "report_seq": 789,
      "authorized": false,
      "reason": "Brand belongs to another user"
    }
  ]
}
```

### Error Responses

#### 400 Bad Request
```json
{
  "error": "Invalid request format"
}
```

#### 401 Unauthorized
```json
{
  "error": "unauthorized"
}
```

#### 500 Internal Server Error
```json
{
  "error": "failed to check report authorization"
}
```

## Authorization Logic

The API implements the following authorization rules:

### Brand-based Authorization
- If a report contains a brand name that belongs to the logged-in user → **AUTHORIZED**
- If a report contains a brand name that belongs to another user → **DENIED**
- If a report contains a brand name that doesn't belong to any user → **AUTHORIZED**

### Area-based Authorization
- If a report location falls within an area that belongs to the logged-in user → **AUTHORIZED**
- If a report location falls within an area that belongs to another user → **DENIED**
- If a report location doesn't fall within any restricted area → **AUTHORIZED**

### Priority
- Brand restrictions take precedence over area restrictions
- If a report is denied due to brand restrictions, area checks are not performed
- If a report is authorized due to brand ownership, area checks are still performed

## Implementation Details

### Database Tables Used
- `reports`: Contains report information including location (latitude/longitude) and description
- `report_analysis`: Contains analyzed report data including normalized brand names
- `customer_brands`: Maps customer IDs to brand names
- `customer_areas`: Maps customer IDs to area IDs
- `areas`: Contains area information
- `area_index`: Contains spatial geometry data for areas

### Brand Detection
The API extracts brand information from the `report_analysis` table, which contains pre-analyzed report data with normalized brand names. This provides accurate brand identification without relying on text parsing.

### Spatial Queries
The API uses MySQL spatial functions to determine if a report location falls within user areas:
```sql
SELECT DISTINCT ca.customer_id 
FROM customer_areas ca
JOIN areas a ON ca.area_id = a.id
JOIN area_index ai ON a.id = ai.area_id
WHERE ST_Contains(ai.geom, ST_Point(?, ?))
```



## Example Usage

### cURL
```bash
curl -X POST http://localhost:8080/api/v3/reports/authorization \
  -H "Authorization: Bearer <your_jwt_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "report_seqs": [123, 456, 789]
  }'
```

### JavaScript
```javascript
const response = await fetch('/api/v3/reports/authorization', {
  method: 'POST',
  headers: {
    'Authorization': 'Bearer ' + token,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    report_seqs: [123, 456, 789]
  })
});

const result = await response.json();
console.log(result.authorizations);
```

## Notes

- The API processes multiple reports in a single request for efficiency
- Authorization checks are performed in parallel where possible
- All database queries use parameterized statements to prevent SQL injection
- Comprehensive logging is implemented for debugging and monitoring
- The API follows the existing error handling and response patterns of the auth service
