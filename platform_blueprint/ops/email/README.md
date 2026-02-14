# Email Ops (Prod VM)

This folder contains **secrets-safe** VM helpers to make the email pipeline less of a black box.

## Quick Stats (Last 24h)

```bash
HOST=deployer@34.122.15.16 ./platform_blueprint/ops/email/stats_prod_vm.sh
```

What it tells you:
- how many brand-recipient sends happened recently (derived from `brand_email_throttle`)
- how many reports were marked processed recently (`sent_reports_emails`)
- current retry queue size and top retry reasons (`email_report_retry`)

## Trace One Report (Seq) End-to-End

```bash
HOST=deployer@34.122.15.16 ./platform_blueprint/ops/email/trace_seq_prod_vm.sh <seq>
```

It prints (DB + logs):
- `reports` + `report_analysis` summary
- retry state (`email_report_retry`)
- processed marker (`sent_reports_emails`)
- physical discovery state (`physical_contact_lookup_state` + candidates)
- brand send correlation (brand context + throttle rows around `processed_at`)

Notes:
- Output **redacts email addresses** from logs.
- The most reliable “did we send?” signal right now is:
  - `sent_reports_emails` has the seq, and
  - `brand_email_throttle.last_sent_at` matches the processed timestamp window for the report’s brand.

