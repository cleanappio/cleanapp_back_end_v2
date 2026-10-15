package database

// publicReportCurrentVisibilitySQL applies the canonical public report policy
// through indexed sequence lookups. The outer report table must use alias r.
// This also revalidates previously issued discovery tokens when an image is read.
const publicReportCurrentVisibilitySQL = `
 (SELECT COUNT(*) FROM report_raw rr WHERE rr.report_seq = r.seq AND rr.visibility <> 'public') = 0
 AND (SELECT COUNT(*) FROM report_status rs WHERE rs.seq = r.seq AND rs.status <> 'active') = 0
 AND (SELECT COUNT(*) FROM reports_owners ro WHERE ro.seq = r.seq AND ro.owner IS NOT NULL AND ro.owner <> '' AND ro.is_public = FALSE) = 0`
