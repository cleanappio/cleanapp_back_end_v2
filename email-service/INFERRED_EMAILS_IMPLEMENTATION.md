# Inferred Contact Emails Implementation

## Overview
This implementation adds support for using `inferred_contact_emails` from the `report_analysis` table as recipients for sending report emails. When this field contains valid email addresses, they take priority over area-based email logic.

## Key Changes Made

### 1. Model Updates
- **File**: `models/report.go`
- **Change**: Added `InferredContactEmails string` field to `ReportAnalysis` struct

### 2. Service Logic Updates
- **File**: `service/email_service.go`
- **Changes**:
  - Added `isValidEmail()` method for email validation
  - Updated `getReportAnalysis()` to fetch the new field
  - Modified `processReport()` to prioritize inferred emails over area emails
  - Added `sendEmailsToInferredContacts()` method for sending emails without area context

### 3. Email Sender Updates
- **File**: `email/email_sender.go`
- **Changes**:
  - Modified `sendOneEmail()` and `sendOneEmailWithAnalysis()` to handle nil map images
  - Added conditional logic to only attach map images when they exist

### 4. Testing
- **File**: `service/email_service_test.go`
- **Changes**:
  - Added comprehensive email validation tests
  - Added tests for email processing logic

## Priority Logic

The email service now follows this priority order:

1. **Inferred Contact Emails** (Highest Priority)
   - Check if `inferred_contact_emails` field is not empty
   - Split comma-separated emails and validate each
   - Send emails to valid addresses
   - Skip area-based email logic entirely

2. **Area-Based Emails** (Fallback)
   - Only used when no valid inferred emails exist
   - Uses existing area detection and contact email logic

## Email Processing Flow

### For Inferred Emails:
```
1. Parse comma-separated email string
2. Clean whitespace from each email
3. Validate each email address
4. Send emails to valid addresses
5. Mark report as processed
6. Skip area-based logic
```

### For Area-Based Emails (Fallback):
```
1. Detect areas containing report location
2. Find contact emails for those areas
3. Send emails with area context and map images
4. Mark report as processed
```

## Email Validation

The service validates email addresses using a regex pattern:
- Must have local part and domain
- Supports common email characters (+, -, _, .)
- Rejects emails with spaces or invalid formats
- Logs warnings for invalid emails found

## Logging

Enhanced logging provides clear visibility into the decision-making process:
- Shows when inferred emails are used vs. area emails
- Reports the number of valid emails found
- Logs warnings for invalid email addresses
- Tracks successful email sending

## Benefits

1. **Priority System**: Inferred emails take precedence over area emails
2. **Validation**: Ensures only valid email addresses receive emails
3. **Fallback**: Maintains existing area-based functionality when needed
4. **Flexibility**: Can handle reports with or without inferred contacts
5. **Logging**: Clear visibility into email routing decisions

## Example Usage

### With Inferred Emails:
```
Report 123: Using 2 valid inferred contact emails (priority over area emails): [contact@brand.com, support@company.org]
Report 123: Successfully sent emails to inferred contacts
```

### Without Inferred Emails:
```
Report 124: No valid inferred contact emails found after validation
Report 124: Falling back to area-based email logic
Report 124: Found 3 areas with emails, sending area-based emails
```

## Database Requirements

The `report_analysis` table must include the `inferred_contact_emails` field:
```sql
ALTER TABLE report_analysis ADD COLUMN inferred_contact_emails TEXT;
```

## Testing

Run the tests to verify functionality:
```bash
cd email-service
go test ./service -v
```

The tests cover:
- Email validation logic
- Email processing and cleaning
- Edge cases (empty strings, invalid formats)
