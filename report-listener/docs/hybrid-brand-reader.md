# Fused source-input hybrid candidate

`getEvidenceBrandPageHybrid` stays off the public route until dual-MySQL parity
and an actual complete request under the existing eight-second deadline pass.
It is a single chosen reader candidate, with no timeout retry through another
reader, approximate count, early cohort limit or cache.

The query constructor reuses the narrow reader's checked source-winner scope and
the candidate's exact metadata/source joins and publication predicates. It retains
only requested-language and brand-scope parameters. Kind, sort, offset, limit and
overview clock never restrict the input universe. One statement streams 46
scalar/presence columns for every visible report, including the selected Wire
presence check and fifteen typed projected JSON nodes. SQL selects the preferred
analysis and native-collation source identities, and applies publication guards.
It does not infer dates or compute canonical URLs/source keys for the whole
cohort. Evidence text remains presence flags until bounded display hydration.

The hybrid reuses `normalizeEvidenceSources`: its established guarded archive
fast path and bounded MySQL fallback remain the only identity/date normalizers.
Stored report ID is distinct from public ID, and nullable dates remain raw
`DATE_FORMAT` strings until that normalizer. Only actual selected Wire rows build
compact documents, using the same extracted fifteen-node assembly as the existing
bulk reader. Absent Wire rows retain nil documents; present all-NULL projections
retain present documents, preserving fast-path eligibility and privacy semantics.
Source fields merge explicitly without replacing selected analysis/evidence data.

This one input consumer does not retain the narrow reader's shared-scope
materialization. The original narrow reader is unchanged. The fused statement
keeps the eight-second server cap and statement
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

Any incomplete or duplicate input stream, normalization failure, deadline, case/hydration error or
commit failure returns an error with no apparently exact partial page. The
focused tests exercise scope parameters, bounded exact nullable hydration,
full-scope overview/facets, NULL ingestion, sample precision, native archive
eligibility, typed Wire nodes, and failure paths.
The guarded MySQL fixture adds full hybrid/reference parity to the existing
kind/sort/offset matrix, cold/warm/mixed/stale projections, malformed public IDs,
native source winners, and large/private family samples. Local tests without a
fixture DSN cannot establish MySQL correctness or production performance.
