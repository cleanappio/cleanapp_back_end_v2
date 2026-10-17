# Evidence first: implementation and release

The implemented frontend leads with original observations and their supporting
evidence. The public backend separates experience kind, acquisition provenance,
entity relevance, source/publication/observation/ingestion dates, and independent
validation. Related cases are labeled issue families; source reports/accounts are
never presented as affected-user totals.

## Implemented scope

- Shared original resolver and source-only Gemini/OpenAI classifier, literal
  excerpt checks, genuine original-image support, and privacy-aware presentation.
- Explicit additive migrations create only the per-sequence `report_evidence` table. The legacy FULLTEXT analysis table and its indexes/history remain intact. Evidence is shared across translations; version-zero retries cannot overwrite completed enrichment.
- Normal analysis, translated rows, recent pre-analyzed imports, native human
  submissions, digital shares, and bounded historical evidence backfill.
- Strict `kind=firsthand|coverage|unknown|all`, signal/recent/recurrence sorting,
  exact company/product scope, canonical source deduplication, related family
  entries, matching snapshot counts, bounded source samples, and pagination.
- Public discovery, lite/full brand responses, and canonical physical/digital
  detail evidence. The Rust brand endpoint delegates to the canonical Go reader.
- Production brand pages with original evidence before chat, source disclosures,
  truthful missing-data states, subscriber access, and evidence inspection on
  existing area-dashboard report modals. No separate property briefing route
  exists; general maps/nearby selection defaults retain their existing behavior.
- Internal X/Bluesky/GitHub submitters retain original source snapshots separately
  from summaries. Native and imported inputs leave unsupplied incident time
  unknown; publication/capture timestamps retain their own meaning.

## Release order

1. Pass local affected tests and the new MySQL 8 CI fixture. The fixture uses a
   dedicated loopback database ending `_evidence_test`, refuses existing tables,
   and never uses application or production credentials.
2. Run explicit backend migrations from the exact release source with the
   established target environment. From the backend repository:

   ```bash
   ./scripts/db/run_go_service_migrations.sh report-analyze-pipeline report-listener
   ```

3. Release the analysis pipeline, Go listener, and Rust listener together using
   the established source/digest deployment process. Rust by-brand depends on the
   internal Go listener. `BRAND_BRIEFING_API_URL` defaults to
   `http://cleanapp_report_listener:8080`; use a matching private/loopback address
   outside Compose. Its bounded timeout/error behavior never substitutes the old
   mixed feed.
4. Release the modified X/GitHub and Bluesky submitter binaries/images using their
   existing service build process. Preserve relevance and routing policies.
5. Configure the evidence-backfill command with the established `DB_*` and LLM
   environment. Inspect originals and proposed labels before applying a cohort:

   ```bash
   cd report-analysis-backfill
   go run ./cmd/evidence-backfill --dry-run --brand grok --limit 50
   go run ./cmd/evidence-backfill --dry-run --brands grok,anthropic,claude,claudecode,openai,chatgpt,codex,whisper --limit 200
   ```

   These commands are read-only against the database but call the configured
   model. Output `label_counts` are not classifier precision. Apply only the
   reviewed cohort with `--apply`; record `--through-seq` and disjoint shard bounds.
   Advance `--after-seq` only after a batch has zero failures; retry failed batches
   using their original bounds so transient failures remain eligible. `--force` revisits successfully enriched rows when better evidence
   becomes available. Raw sources and existing summaries/routing stay intact.

6. Release the frontend after backend/schema readiness and the initial reviewed
   backfill. The frontend repository automatically deploys on merges to main, so
   keep its PR unmerged until this ordering is satisfied. Sparse firsthand views
   stay honest and link to unclassified/all sources.

## Operational semantics

- The automatic worker covers recent ingestion within the newest 50,000 sequences through the existing primary key, with batches capped at 50. Wire/index ingestion time also qualifies old publication timestamps. Older reports require
  explicit bounded backfill. Transient resolver/provider errors keep version zero
  for retry; successfully assessed unknown records can be revisited explicitly.
- Evidence adds a source-only provider request where originals exist. The
  process-wide `EVIDENCE_REQUESTS_PER_MINUTE` budget defaults to 20 and covers both
  normal analysis evidence and the recent-import worker. Separate backfill
  processes have their own budget; allocate combined provider quota deliberately.
- Case association does not prove an identical defect, independent corroboration,
  or affected-user prevalence. Missing account information is null rather than
  zero. Source samples are bounded and individually privacy-filtered; families remain visible when every sample is withheld. Dates on the representative source remain
  distinct from family activity dates.
- Upstream/resolution states remain unknown when the stored evidence does not
  establish them. Nothing infers a fix from source closure.
- Existing active/public visibility, ownership, quarantine, routing, and intake
  rules are preserved. Coverage and unknown reports remain accessible in explicit
  views; physical and digital share the enrichment model.
- The projected reader keeps the inactive-status guard as `NOT EXISTS` and the
  private-owner guard as a correlated `COUNT(*)=0`. The latter retains per-report
  indexed owner lookups instead of materializing unrelated private owners. Its
  `JOIN_INDEX(ro idx_is_public)` hint remains optional; an absent index does not
  change publication semantics or require a schema migration.

## Validation record

Affected Go modules (`go-common`, `report-analyze-pipeline`, `report-listener`,
`report-analysis-backfill`) passed `go test ./...` and `go vet ./...`. Changed Rust
submitter tests and the seven Rust briefing proxy tests passed. Twelve frontend
evidence/inventory tests, TypeScript checking, and the production build passed with loopback
API environment configuration. Unrelated existing lint warnings remain.

Local CUA checks covered desktop 1280px and mobile 320px, evidence before chat,
source disclosures, kind/sort selection, token navigation, and empty/error states.
Screenshot records use a read-only local fixture with a historical public GitHub
example and explicitly synthetic coverage/unknown fixtures. Their counts are not
production statistics.

The subsequent release checks used exact MySQL 8.0.44 fixtures for CTE/window/JSON behavior, source privacy, migration idempotence without legacy schema changes, monotonic evidence writes, and bounded recent-worker eligibility. Production-shaped provider samples were reviewed before backfill writes. Final production outcomes are recorded in the release artifact after deployment.

## Retained older paths

The old Rust `db::fetch_reports_by_brand` SQL helper is now unused by its handler
and remains in place. Legacy Go brand query/count helpers and count-cache code
remain for existing callers/compatibility; the new briefing uses its own matching
snapshot selection. Shared frontend components removed from digital brand use
are retained for their other surfaces. No unrelated cleanup was performed.
