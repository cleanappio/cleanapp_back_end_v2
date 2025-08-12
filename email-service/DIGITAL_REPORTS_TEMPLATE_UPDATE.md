# Digital Reports Email Template Update

## Overview
This update modifies the email sending template to remove metrics gauges for digital reports while maintaining them for physical reports. Digital reports now show a notice explaining that physical metrics are not applicable.

## Key Changes Made

### 1. Model Updates
- **File**: `models/report.go`
- **Change**: Added `Classification string` field to `ReportAnalysis` struct

### 2. Service Updates
- **File**: `service/email_service.go`
- **Changes**:
  - Updated `getReportAnalysis()` to fetch the classification field
  - Added logging to show report classification type
  - Enhanced logging to include report type in success messages

### 3. Email Template Updates
- **File**: `email/email_sender.go`
- **Changes**:
  - Added `getMetricsSection()` method to conditionally render content
  - Modified HTML template to show different content based on report type
  - Updated plain text template to handle digital vs physical reports
  - Added CSS styling for digital notice

## Template Behavior

### For Physical Reports (classification = "physical"):
- Shows the full metrics gauge with:
  - Litter Probability gauge (0-100%)
  - Hazard Probability gauge (0-100%)
  - Severity Level gauge (0-10 scale)
- Displays "Type: Physical Issue" in report details
- Includes probability scores in plain text version
- **No analysis summary section**

### For Digital Reports (classification = "digital"):
- **Removes** the metrics gauge section
- Shows a digital notice explaining metrics are not applicable
- Displays "Type: Digital Issue" in report details
- Plain text version explains that physical metrics don't apply
- **No analysis summary section**

## HTML Template Changes

The HTML template now conditionally renders content:

```html
<!-- Report Details Section -->
<div class="analysis-section">
    <h3>Report Details</h3>
    <p><strong>Title:</strong> %s</p>
    <p><strong>Description:</strong> %s</p>
    <p><strong>Type:</strong> %s</p>  <!-- New field -->
</div>

<!-- Conditional Metrics Section -->
%s  <!-- This is replaced by getMetricsSection() -->

<!-- For Physical Reports: Shows gauge-grid -->
<!-- For Digital Reports: Shows digital-notice -->
```

## CSS Styling

Added new CSS class for digital notices:
```css
.digital-notice { 
    background-color: #fff3cd; 
    padding: 15px; 
    border-radius: 5px; 
    margin: 15px 0; 
    border-left: 4px solid #ffc107; 
}
```

## Plain Text Template Changes

The plain text version now shows different content:

### Physical Reports:
```
REPORT ANALYSIS:
Title: [Title]
Description: [Description]
Type: Physical Issue

PROBABILITY SCORES:
- Litter Probability: XX.X%
- Hazard Probability: XX.X%
- Severity Level: X.X (0-10 scale)

This email contains:
- The report image
- A map showing the location
- AI analysis results
```

### Digital Reports:
```
REPORT ANALYSIS:
Title: [Title]
Description: [Description]
Type: Digital Issue

This email contains:
- The report image
- A map showing the location
- AI analysis results

Note: This is a digital issue report. Physical metrics (litter/hazard probability) are not applicable.
```

## Database Requirements

The `report_analysis` table must include the `classification` field:
```sql
ALTER TABLE report_analysis ADD COLUMN classification ENUM('physical', 'digital') DEFAULT 'physical';
```

## Benefits

1. **Appropriate Content**: Digital reports no longer show irrelevant physical metrics
2. **Clear Communication**: Users understand why certain metrics are not shown
3. **Consistent Experience**: Both report types get appropriate, relevant information
4. **Professional Appearance**: Digital reports look clean without unnecessary gauges

## Example Output

### Physical Report Email:
- Full metrics gauge with litter, hazard, and severity
- "Type: Physical Issue" displayed
- All probability scores shown

### Digital Report Email:
- No metrics gauge
- Digital notice explaining the report type
- "Type: Digital Issue" displayed
- Clean, focused content

## Testing

The changes can be tested by:
1. Creating reports with different classifications
2. Verifying email templates render correctly for each type
3. Checking that digital reports don't show metrics gauges
4. Ensuring physical reports maintain full functionality

## Backward Compatibility

- Existing physical reports continue to work as before
- New digital reports get appropriate treatment
- No breaking changes to existing functionality
