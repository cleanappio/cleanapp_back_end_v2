-- group: coverage
-- title: Brands with first-hand evidence and nothing ever sent
-- kind: count
-- window: all time
-- better: lower
-- definition: Brands with at least @backlog_min eligible first-hand reports (5 unless set otherwise) of which none has ever been sent to anyone. The size of the "no door" problem.
SELECT COUNT(*)
FROM (
  SELECT a.brand_name
  FROM ({{eligible_firsthand}}) c
  JOIN report_analysis a ON a.seq = c.seq AND a.language = 'en'
  WHERE a.brand_name <> ''
  GROUP BY a.brand_name
  HAVING COUNT(DISTINCT c.seq) >= @backlog_min
     AND SUM(EXISTS (SELECT 1 FROM report_email_deliveries d WHERE d.seq = c.seq)) = 0
) unsent
