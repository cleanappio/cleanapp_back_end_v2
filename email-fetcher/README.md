email-fetcher
===============

Rust daemon that fills `report_analysis.inferred_contact_emails` for:

- **Digital reports**: asks the LLM for plausible vendor support emails based on `brand_display_name`
- **Physical reports**: resolves location-aware contacts using `area_index` + `contact_emails`

Physical pass state is tracked in `physical_contact_lookup_state` so unmatched/error rows are retried on backoff instead of hot-looping.

Environment
-----------
- DB_HOST (default: localhost)
- DB_PORT (default: 3306)
- DB_USER (default: server)
- DB_PASSWORD (required)
- DB_NAME (default: cleanapp)
- OPENAI_API_KEY (required for inference)
- OPENAI_MODEL (default: gpt-4o)
- LOOP_DELAY_MS (default: 10000)
- BATCH_LIMIT (default: 10)
- PHYSICAL_BATCH_LIMIT (default: 25)
- PHYSICAL_MAX_CONTACTS (default: 5)
- SEQ_RANGE (optional: e.g. `29590-29600` to limit affected seq)
- ENABLE_DIGITAL_EMAIL_FETCHER (default: true)
- ENABLE_PHYSICAL_EMAIL_FETCHER (default: true)

Run
---
```bash
SEQ_RANGE=29590-29600 cargo run -q
```

Docker
------
Build an image similar to other services. A minimal Dockerfile will be added later to align with pipeline conventions.

### Graceful shutdown

The entrypoint replaces its shell with the Rust process so Docker's default
SIGTERM reaches the application. SIGTERM and SIGINT are registered before
database startup and retained during each batch. A signal stops the next batch;
the current batch finishes and the MySQL pool disconnects before exit. An active
network or database request can extend that drain, so a stop timeout is not a
guarantee that the process has finished. Operators should verify the exit state
and avoid escalating to a forced kill while work is draining.

The local signal tests use only freshly spawned fixture processes, with no
database, API or email work:

```sh
cargo test --manifest-path email-fetcher/Cargo.toml --locked shutdown::tests
```
