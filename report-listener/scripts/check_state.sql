-- Check the current state of the report-listener service
SELECT 
    service_name,
    last_processed_seq,
    created_at,
    updated_at,
    TIMESTAMPDIFF(SECOND, updated_at, NOW()) as seconds_since_last_update
FROM service_state 
WHERE service_name = 'report-listener';

-- Check the latest report in the database
SELECT 
    MAX(seq) as latest_report_seq,
    COUNT(*) as total_reports,
    MAX(ts) as latest_report_time
FROM reports;

-- Check for any missed reports (if service_state exists)
SELECT 
    s.last_processed_seq as last_processed,
    r.max_seq as latest_available,
    (r.max_seq - s.last_processed_seq) as missed_reports
FROM service_state s
CROSS JOIN (
    SELECT COALESCE(MAX(seq), 0) as max_seq FROM reports
) r
WHERE s.service_name = 'report-listener'; 