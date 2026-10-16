# Fused normalized-read hybrid candidate

`getEvidenceBrandPageHybrid` stays off the public route until dual-MySQL parity
and an actual complete request under the existing eight-second deadline pass.
It is a single chosen reader candidate, with no timeout retry through another
reader, approximate count, early cohort limit or cache.

The query constructor reuses the narrow reader's checked authoritative SQL only
through `normalized`, cutting at the unique `rank_input` boundary. It retains
only requested-language and brand-scope parameters. Kind, sort, offset, limit and
overview clock never restrict the normalized universe. One statement streams
29 scalar/presence columns for every visible report: SQL still selects the
preferred analysis, native-collation source identities and exact typed Wire
nodes, computes identities/times, and applies all publication/privacy guards.
Evidence text remains presence flags until bounded display hydration.

The hybrid has one normalization consumer, so it checked-removes the narrow
reader's shared-scope `NO_MERGE(r)` materialization. The original narrow reader is
unchanged. The fused statement keeps the eight-second server cap and statement
64MiB temporary-table ceiling, within one eight-second read-only repeatable-read
transaction covering cases, grouping and final display/image reads.

The existing `evidenceOverview` runs on the full visible report slice, followed
by SQL-collated case references on every visible import and unchanged
`groupEvidenceRecords`. This preserves source deduplication before case/kind
filtering, exact facets/counts, severity/date ordering, and first-eight samples
before private samples are removed. Latest-private Wire selection cannot expose
older public fields.

One record slice holds the cohort. A sequence-to-index map and a small parallel
nullable-selection slice retain chosen analysis metadata without another full
record map. Only selected page/sample IDs become `evidenceAnalysisCore` values
for the existing bounded display hydrator. NULL severity, language, creation
literal and ingestion time retain their separate meanings. Public epoch fallback
for NULL ingestion occurs after grouping; zero non-NULL timestamps remain
distinct. Supporting source dates match the oracle's whole-second JSON format,
while the page's own source date keeps its precision.

Any incomplete or duplicate normalized stream, deadline, case/hydration error or
commit failure returns an error with no apparently exact partial page. The
focused tests exercise scope parameters, bounded exact nullable hydration,
full-scope overview/facets, NULL ingestion, sample precision and failure paths.
The guarded MySQL fixture adds full hybrid/reference parity to the existing
kind/sort/offset matrix, cold/warm/mixed/stale projections, malformed public IDs,
native source winners, and large/private family samples. Local tests without a
fixture DSN cannot establish MySQL correctness or production performance.

The normalized stream uses the existing compact normalizer's candidates/provenance/normalized materialization boundaries. This stores scalar URL/JSON results once instead of expanding their expressions repeatedly into canonical source keys. The single chosen-metadata scope remains mergeable; the three barriers contain only source metadata and display-presence fields.
