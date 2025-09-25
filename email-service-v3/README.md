# email-service-v3

Rust reimplementation of email service with brand-aggregated notifications.

Binaries:
- email-service-v3-backfill: populate brands and brand_emails from report_analysis.
- email-service-v3: polling service that sends summary emails per brand using SendGrid.

Environment (reused where possible):
- DB_HOST (default: localhost)
- DB_PORT (default: 3306)
- DB_USER (default: server)
- DB_PASSWORD (default: secret)
- DB_NAME (default: cleanapp)
- SENDGRID_API_KEY (required)
- SENDGRID_FROM_NAME (default: CleanApp)
- SENDGRID_FROM_EMAIL (default: info@cleanapp.io)
- POLL_INTERVAL (default: 10s)
- HTTP_PORT (default: 8080)
- OPT_OUT_URL (default: http://localhost:8080/opt-out)
- NOTIFICATION_PERIOD (default: 90d)
- DIGITAL_BASE_URL (default: https://www.cleanapp.io/digital)
- ENV (default: prod)

Backfill:

```
cargo run -p email-service-v3 --bin email-service-v3-backfill
```

Service:

```
RUST_LOG=info cargo run -p email-service-v3 --bin email-service-v3
```


