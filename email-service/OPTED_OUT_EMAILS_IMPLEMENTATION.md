# Opted-Out Emails Implementation

## Overview
This implementation adds support for an `opted_out_emails` table that allows users to opt out of receiving emails. The system now checks this table before sending any emails and skips opted-out addresses.

## Changes Made

### 1. Database Table Creation
- **File**: `service/email_service.go`
- **Function**: `verifyAndCreateTables`
- **Changes**:
  - Added `opted_out_emails` table creation logic
  - Table includes indexed email field for fast lookups
  - Automatic table creation on service startup

### 2. Opt-Out Checking Function
- **File**: `service/email_service.go`
- **Function**: `isEmailOptedOut`
- **Purpose**: Checks if an email address has opted out
- **Implementation**: Database query with COUNT(*) for efficiency

### 3. Email Filtering in Inferred Contacts
- **File**: `service/email_service.go`
- **Function**: `sendEmailsToInferredContacts`
- **Changes**:
  - Added opted-out email filtering
  - Logs skipped emails for transparency
  - Only sends to valid, non-opted-out addresses
  - **Generates 1km map image** centered on report coordinates
  - Provides geographic context even without polygon features

### 4. Email Filtering in Area Contacts
- **File**: `service/email_service.go`
- **Function**: `sendEmailsForArea`
- **Changes**:
  - Added opted-out email filtering
  - Logs skipped emails for transparency
  - Only sends to valid, non-opted-out addresses

## Database Schema

### opted_out_emails Table
```sql
CREATE TABLE opted_out_emails (
    id INT AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    opted_out_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_email (email),
    INDEX idx_opted_out_at (opted_out_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
```

### Table Structure
- **id**: Auto-incrementing primary key
- **email**: Email address (unique, indexed)
- **opted_out_at**: Timestamp when opt-out was recorded
- **Indexes**: 
  - Primary key on `id`
  - Unique index on `email` for fast lookups
  - Index on `opted_out_at` for potential future features

## Email Filtering Process

### 1. Email Validation
```go
// Check if email is opted out
optedOut, err := s.isEmailOptedOut(ctx, email)
if err != nil {
    log.Warnf("Failed to check if email %s is opted out: %v, skipping", email, err)
    continue
}

if optedOut {
    log.Infof("Skipping opted-out email: %s", email)
    continue
}
```

### 2. Filtering Results
- **Before filtering**: Original email list
- **After filtering**: Only non-opted-out emails
- **Logging**: Shows how many emails were filtered out

### 3. Skip Conditions
- **All emails opted out**: No emails sent, logged appropriately
- **Some emails opted out**: Only valid emails receive messages
- **No emails opted out**: All emails sent (normal operation)

### 4. Map Image Generation
- **Inferred contacts**: 1km map centered on report coordinates (no polygon overlay)
- **Area contacts**: Map with polygon boundaries and report point marker
- **Fallback handling**: If map generation fails, emails sent without map images

## Use Cases

### 1. User Opt-Out
```sql
-- User opts out of emails
INSERT INTO opted_out_emails (email) VALUES ('user@example.com');
```

### 2. Email Processing
```go
// System automatically skips opted-out emails
emails := []string{"user1@example.com", "user2@example.com", "opted@example.com"}
// Only user1@example.com and user2@example.com receive emails
```

### 3. Opt-Out Management
```sql
-- Check who has opted out
SELECT email, opted_out_at FROM opted_out_emails;

-- Remove opt-out (user wants emails again)
DELETE FROM opted_out_emails WHERE email = 'user@example.com';
```

## Benefits

1. **User Control**: Users can opt out of unwanted emails
2. **Compliance**: Helps meet email marketing regulations
3. **Efficiency**: Prevents sending emails to users who don't want them
4. **Transparency**: Clear logging of skipped emails
5. **Performance**: Indexed email field for fast lookups

## Logging and Monitoring

### Log Messages
- **Opt-out detected**: `"Skipping opted-out email: user@example.com"`
- **Filtering results**: `"Sending emails to 2 valid contacts (filtered from 3 total)"`
- **All opted out**: `"All emails for report 123 are opted out, no emails sent"`

### Monitoring Points
- Number of opted-out emails per report
- Total emails filtered vs. sent
- Opt-out rate trends

## Performance Considerations

### Database Queries
- **Efficient lookups**: Indexed email field
- **Single query per email**: COUNT(*) with WHERE clause
- **Minimal overhead**: Only one additional query per email address

### Caching Opportunities
- **Future enhancement**: Could cache opted-out emails in memory
- **Batch processing**: Could batch opt-out checks for multiple emails

## Security and Privacy

### Data Protection
- **Email addresses**: Stored securely in database
- **Opt-out history**: Timestamp tracking for audit purposes
- **Unique constraint**: Prevents duplicate opt-outs

### Compliance Features
- **Opt-out respect**: System never sends to opted-out addresses
- **Audit trail**: Timestamp of when opt-out was recorded
- **Easy removal**: Simple DELETE to restore email access

## Future Enhancements

### 1. Bulk Operations
```sql
-- Bulk opt-out (future feature)
INSERT INTO opted_out_emails (email) VALUES 
('user1@example.com'), ('user2@example.com'), ('user3@example.com');
```

### 2. Opt-Out Categories
```sql
-- Category-specific opt-outs (future feature)
CREATE TABLE opted_out_categories (
    email VARCHAR(255),
    category VARCHAR(50),
    opted_out_at TIMESTAMP
);
```

### 3. Opt-Out Analytics
```sql
-- Opt-out rate analysis (future feature)
SELECT 
    DATE(opted_out_at) as date,
    COUNT(*) as opt_outs
FROM opted_out_emails 
GROUP BY DATE(opted_out_at);
```

## Testing

### Test Scenarios
1. **No opt-outs**: Verify all emails are sent
2. **Some opt-outs**: Verify only non-opted-out emails receive messages
3. **All opt-outs**: Verify no emails are sent
4. **Invalid emails**: Verify error handling for database issues

### Test Data
```sql
-- Test opt-out entries
INSERT INTO opted_out_emails (email) VALUES ('test1@example.com');
INSERT INTO opted_out_emails (email) VALUES ('test2@example.com');
```

## Summary

The opted-out emails functionality provides:
- **User control** over email preferences
- **Efficient filtering** of email recipients
- **Comprehensive logging** for transparency
- **Performance optimization** through database indexing
- **Easy management** of opt-out preferences

This enhancement makes the email service more user-friendly and compliant while maintaining performance and reliability.
