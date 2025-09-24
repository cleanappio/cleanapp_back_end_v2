email-fetcher
===============

Small Rust daemon that fills in report_analysis.inferred_contact_emails for valid digital reports by asking OpenAI for plausible vendor support emails based on brand_display_name.

Environment
-----------
- DB_HOST (default: localhost)
- DB_PORT (default: 3306)
- DB_USER (default: server)
- DB_PASSWORD (default: secret_app)
- DB_NAME (default: cleanapp)
- OPENAI_API_KEY (required for inference)
- OPENAI_MODEL (default: gpt-4o)
- LOOP_DELAY_MS (default: 10000)
- BATCH_LIMIT (default: 10)
- SEQ_RANGE (optional: e.g. `29590-29600` to limit affected seq)

Run
---
```bash
SEQ_RANGE=29590-29600 cargo run -q
```

Docker
------
Build an image similar to other services. A minimal Dockerfile will be added later to align with pipeline conventions.


