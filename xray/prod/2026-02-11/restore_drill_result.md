# Restore Drill Result (Prod)

- Date (UTC): 2026-02-11
- Source backup object: `gs://cleanapp_mysql_backup_prod/current/cleanapp_all.sql.gz`
- Source metadata: `gs://cleanapp_mysql_backup_prod/current/cleanapp_all.metadata.json`
- Scratch container: `cleanapp_db_restore_drill_20260211T084437Z`

## Row Count Validation

From metadata (`row_counts`):
- `reports`: `1,132,758`
- `report_analysis`: `1,299,843`

From restored scratch DB:
- `reports`: `1,132,621`
- `report_analysis`: `1,299,706`

Delta:
- `reports`: `137` (`0.012094%`)
- `report_analysis`: `137` (`0.010540%`)

Interpretation:
- Restored counts are within a tight tolerance (< `0.2%`) and consistent with an online backup drift profile on a write-active system.

## Cleanup

- Removed stale drill artifacts after validation:
  - container `cleanapp_db_restore_drill_20260211T084437Z`
  - volume `eko_mysql_restore_drill_20260211T084437Z`
  - temp env file `/tmp/cleanapp_db_restore_drill_20260211T084437Z.env`
