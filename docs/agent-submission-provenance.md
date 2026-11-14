# Public agent-submission provenance

The public Go evidence reader adds context from the retained canonical Wire
submission for selected page/detail records. This is a read-time repair: no
migration, backfill, model request, external URL fetch or original-payload update
is required. It leaves consent, publication, routing and outcomes unchanged.

## Separate acquisition, original sources and affected pages

- A machine Wire submission with explicit `human_assisted` generation is an
  `agent_submission`, labeled “Agent-assisted submission.” A linked original X
  post does not change the submission channel into a crawler import.
- `submission_transport` uses the explicit `extensions.cleanapp_client.transport`
  (`mcp` or `web`) where recorded. Otherwise the known generic intake channel may
  be shown. Old agent names/tags do not establish which client was used.
- `submission_client` is emitted only with the explicit `name_source` marker
  `caller_provided`; `submission_client_claimed` remains true. It is not verified
  platform identity. `reporter_context` is likewise a claim and never promotes
  the source-only evidence classification to firsthand.
- `reported_product_or_site` preserves explicit affected-product context.
  `referenced_urls` contains up to 32 safe literal links in the retained
  description, suitable for “Links in submitted report.” An affected account
  access page is not an original social post.
- `source_references` contains declared original-source URLs and recognized
  literal public-post URL shapes, with `kind` `declared_source` or `linked_post`.
  A URL shape is not proof that a post exists or is publicly accessible.
  `source_reference_count` counts distinct references, including X/Twitter URL
  aliases once; it never counts affected people or independent corroboration.
  The count is absent when retained context is unavailable, truncated or
  references are withheld. A complete retained description with no original
  references has count zero. The list is bounded even when the count is larger.

## Dates and unresolved states

`ingested_at` remains the server/report receipt time. A prepared draft's frozen
Wire `submitted_at` is not relabeled as receipt time. Missing observation time
stays absent (`observed_at_basis=not_recorded`); a supplied observation uses basis
`provided`.

Source publication time retains source metadata when available. Explicit
RFC3339 source dates, including offsets, use `source_published_at_basis=provided`
when consistent with the existing source timestamp. A single unambiguous X post
reference may supply a Snowflake-derived timestamp with the explicit basis
`derived_x_status_id`; it is neither an observation date nor independent source
verification. Missing or conflicting context does not fabricate dates. Source
status, resolution and validation are unchanged without independent evidence.

## Read boundaries and validation

The extra read is limited to the selected public page (at most 50 reports) or
detail batch (at most 100), with a three-second SQL deadline. It joins the
canonical `report_raw.fetcher_id`, then selects the latest Wire submission by
`submitted_at DESC, submission_id DESC`. The private JSON projection is capped
at 64 KiB; description extraction is capped at 16 KiB. Raw bodies and agent
identity are never serialized in public evidence. Public visibility, active
status, ownership and explicit privacy flags still gate context. The shared
deterministic privacy check and URL guards exclude sensitive text, credentials,
private hosts and private chat links before canonicalization removes anything.

Before release, run the affected listener unit suites. The existing dedicated
loopback-only `TestEvidenceMySQLFixture` also contains
`PublicSubmissionProvenance`, checking real JSON/CTE semantics, source identity,
tie-breaking, restricted rows and unchanged original bytes. It refuses a
nonlocal database or a database name without the `_evidence_test` suffix.

For a bounded operational dry-run, compare the selected public API JSON before
and after deploying only the reader against retained rows; inspect the added
fields and ensure the original received time and existing outcome are identical.
Use the known public report ID, without exposing retained JSON or credentials.
Unsupported old client names and unsupplied observation dates must remain absent.
No raw-data “repair” UPDATE is appropriate: new explicit context belongs in a new
submission, while supported existing context is projected additively.
