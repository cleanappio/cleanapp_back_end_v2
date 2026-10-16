# Report listener v4

The Rust listener continues serving brand summaries, physical/digital map points,
and report details from MySQL. Only `/api/v4/reports/by-brand` delegates to the
canonical Go `/api/v3/reports/by-brand` briefing so both API versions apply the
same original-evidence classification, source deduplication, issue-family
association, ranking, product scope and count policy. JSON is passed through to
preserve additive evidence and recurrence metadata.

## Brand briefing configuration

- `BRAND_BRIEFING_API_URL`: internal base URL of the Go report listener. Default
  `http://cleanapp_report_listener:8080` matches the production Compose service.
  For local host testing, use `http://127.0.0.1:9081`.
- `BRAND_BRIEFING_TIMEOUT_SECONDS`: total request/body timeout; default 8 seconds,
  bounded to 1–30 seconds. Responses are bounded to 64 MiB.

The base URL must use HTTP(S), identify a loopback/private address, a single-label
internal service name or an `.internal` hostname, and contain no credentials,
path, query or fragment. The canonical request path is fixed. Redirects and
ambient HTTP proxies are disabled. Authorization headers are forwarded only to
this configured internal endpoint and are never logged by the client.

Accepted queries are `brand_name`, `n`, `lang`, `kind` (or `content_kind`), `sort`,
`offset`, `product` and `full_data`. Defaults are `kind=firsthand`, `sort=signal`,
`lang=en`, `n=25`, `offset=0` and `full_data=true`; full reports preserve the Rust
endpoint's existing default. Explicit `full_data=false` requests the canonical
lightweight shape. The Go service validates filters and applies its page-size
cap. Missing/invalid parameters return 400. Authentication failures preserve 401
or 403. Unavailable, malformed or oversized canonical responses return 503;
there is no fallback to the old mixed SQL feed.

The deployment therefore requires the Go report listener for brand briefings.
Other Rust endpoints do not acquire this dependency. The old
`db::fetch_reports_by_brand` helper is retained, now unused, pending an explicit
cleanup decision.

## Verification

`cargo test` includes a loopback fake canonical service that checks default and
explicit queries, full/lightweight selection, authorization forwarding,
additive metadata/count preservation, canonical 400 and unavailable 503, plus
unsafe URL rejection, request timeouts and redirect/malformed-response rejection. Existing detail security tests remain unchanged.

## Graceful shutdown

Persistent SIGTERM and SIGINT listeners are registered before configuration and database startup. A signal stays pending across startup, then stops accepting new HTTP connections and lets admitted responses complete through Axum graceful shutdown. A normal drain returns success instead of exiting with the previous unconditional code 2. Routes and publication/privacy checks are unchanged. There is no forced request cancellation or promised fixed drain duration; an old running artifact still needs its own verified retirement procedure.

`cargo test --manifest-path report-listener-v4/Cargo.toml --locked shutdown` runs real TERM/INT child-process fixtures and an HTTP regression that holds an admitted response open, requests graceful shutdown, and requires that response to complete before the server returns. These fixtures use no service database or production endpoint.
