# Evidence first: implementation and release

The production frontend now leads with original observations and their supporting
evidence. The public backend separates experience kind, acquisition provenance,
entity relevance, source/publication/observation/ingestion dates, and independent
validation. Related cases are labeled issue families; source reports/accounts are
never presented as affected-user totals.

## Implemented scope

- Shared original resolver and source-only Gemini/OpenAI classifier, literal
  excerpt checks, genuine original-image support, and privacy-aware presentation.
- Explicit additive migrations for evidence columns and relevant indexes.
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
   reviewed cohort with `--apply`; use returned `last_seq` with `--after-seq` to
   resume. `--force` revisits successfully enriched rows when better evidence
   becomes available. Raw sources and existing summaries/routing stay intact.

6. Release the frontend after backend/schema readiness and the initial reviewed
   backfill. The frontend repository automatically deploys on merges to main, so
   keep its PR unmerged until this ordering is satisfied. Sparse firsthand views
   stay honest and link to unclassified/all sources.

## Operational semantics

- The automatic worker covers the last day of ingestion. Older reports require
  explicit bounded backfill. Transient resolver/provider errors keep version zero
  for retry; successfully assessed unknown records can be revisited explicitly.
- Evidence adds a source-only provider request where originals exist. The
  process-wide `EVIDENCE_REQUESTS_PER_MINUTE` budget defaults to20 and covers both
  normal analysis evidence and the recent-import worker. Separate backfill
  processes have their own budget; allocate combined provider quota deliberately.
- Case association does not prove an identical defect, independent corroboration,
  or affected-user prevalence. Missing account information is null rather than
  zero. Source samples are bounded; dates on the representative source remain
  distinct from family activity dates.
- Upstream/resolution states remain unknown when the stored evidence does not
  establish them. Nothing infers a fix from source closure.
- Existing active/public visibility, ownership, quarantine, routing, and intake
  rules are preserved. Coverage and unknown reports remain accessible in explicit
  views; physical and digital share the enrichment model.

## Validation record

Affected Go modules (`go-common`, `report-analyze-pipeline`, `report-listener`,
`report-analysis-backfill`) passed `go test ./...` and `go vet ./...`. Changed Rust
submitter tests and the seven Rust briefing proxy tests passed. Seven frontend
evidence tests, TypeScript checking, and the production build passed with loopback
API environment configuration. Unrelated existing lint warnings remain.

Local CUA checks covered desktop1280px and mobile320px, evidence before chat,
source disclosures, kind/sort selection, token navigation, and empty/error states.
Screenshot records use a read-only local fixture with a historical public GitHub
example and explicitly synthetic coverage/unknown fixtures. Their counts are not
production statistics.

No local MySQL server was available; real CTE/window/JSON execution is validated by
the new dedicated CI job before release. No production migration, backfill write,
model call, merge, or deployment was performed during implementation.

## Retained older paths

The old Rust `db::fetch_reports_by_brand` SQL helper is now unused by its handler
and remains in place. Legacy Go brand query/count helpers and count-cache code
remain for existing callers/compatibility; the new briefing uses its own matching
snapshot selection. Shared frontend components removed from digital brand use
are retained for their other surfaces. No unrelated cleanup was performed.
