# Narrow SQL brand reader candidate

Large scopes currently stream every visible report through analysis, metadata,
source hydration, normalization and Go grouping even when only a handful of
firsthand records appear. This candidate computes exact counts and the bounded
page in MySQL, then fetches display text only for that page and its supporting
samples. It keeps the existing eight-second request deadline.

`getEvidenceBrandPageNarrowSQL` is an explicit candidate. This branch does not
switch the public route or add an automatic retry/fallback to a different reader.
Choose one serving strategy only after parity CI and source-bound runtime proof.

The untouched executable SQL reference supplies identity/date expressions,
visibility, preferred analysis, source deduplication, case mapping, kind filters,
facets, ordering and counts. Checked query adaptations replace full-cohort
display fields with empty values/presence bits and use the established scalar
source projections with guarded original-data fallback. Contract drift returns
an error rather than silently reverting to a wide query.

Preferred analysis ranking carries only its six covered nullable analysis fields
and the unchanged row number. Per-report evidence is keyed by sequence and does
not affect either analysis or source selection, so it joins after both choices.
Empty display placeholders and the exact SQL-collated evidence-presence flags
are also introduced after selection, avoiding cells in the analysis temporary.

The one selected analysis tuple travels through scalar report metadata, avoiding
a second preferred-analysis lookup. Each report then uses independent indexed
`LEFT JOIN LATERAL` lookups for its first native-collation EII source/external-ID
pair and latest Wire submission. Each identity lookup alone has `LIMIT 1`; the
cohort remains complete. Wire ordering reads its projection timestamp or a
missing/stale-header PK fallback using `CASE`, preserving an authoritative NULL.
Both the header and fallback must match the selected report sequence. Exact
timestamp or collation ties retain the oracle's unspecified choice; there is no
new tie-breaker, and every detail/privacy field binds to the chosen submission.

The compact selected identity/analysis/report-metadata scope is materialized
once with `NO_MERGE(r)` in its candidate consumer so late consumers do not rerun
the lateral lookups. Source URLs, typed JSON nodes, source keys and display data
stay outside that materialization. Full chosen source nodes and publication
guards join afterward; latest-private Wire submissions are chosen before any
privacy handling, suppressing older public data. Sequence-only publication
guards preserve the visible report universe and full-source overview.

Only thirteen scalar rank/count fields cross source deduplication, case tagging,
family selection and pagination. Public identity, coordinates, source metadata,
and selected-analysis metadata rejoin the same normalized snapshot by the chosen
sequence after paging. Analysis rank1 and the independent identity choices leave
exactly one row per sequence, so these bounded detail joins cannot multiply
selected reports.

Page/sample private metadata retains the exact selected analysis's nullable
language, creation literal, brand, classification and severity. The existing
bounded display hydrator uses those values to avoid mixing tied analyses. It
does not call the full-brand analysis loader. Supporting sample JSON is built
after paging, only for selected families, with the existing eight-sample and
privacy rules. Private selection metadata is removed by public serialization.
Samples carry their existing family rank privately so a different physical join
order cannot change the supporting-source order or expand the first-eight slice.

The original binary selected-public-ID predicate is authoritative: NULL, empty
and all-ASCII-space identities stay excluded under mixed parent/projection
collations, and an authoritative projection NULL never falls back to a parent
identity. Well-formed public IDs retain their original bytes.

Focused tests check bounded late hydration and failure without partial exact
counts. The guarded dual-MySQL fixture compares every public field/count to the
unchanged reference across its existing kind/sort/offset matrix, cold/mixed
report/EII/Wire projections, stale Wire links, and explicit malformed IDs under
mixed collations. Passing local tests with no fixture DSN does not establish
MySQL correctness or production performance; those checks remain required.
An additional slim-rank fixture checks a coverage source winner before kind
filtering, a case link carried by its losing import, exact late display fields,
and a family larger than eight with a private second-ranked observation.

The identity-first fixture compares native accent ordering of source identities,
latest Wire data/privacy, EII/Wire cross products, chosen French analysis, and
owner/status/shadow/malformed-ID exclusions across warm, mixed/stale and cold
projections against the executable oracle.

Both structural changes still need runtime proof. In the measured 393e758
candidate, even a normalized-prefix COUNT reached the unchanged eight-second
server cap with two disk temporary tables. Neither local correctness checks nor
removing those disk temporaries establishes that the entire request fits its
deadline.

Raw overview counts reuse all compact representative rows before source deduplication filtering. This preserves losing-import counts and ingestion-time windows while avoiding a separate full normalization consumer.

The compact thirteen-field `rank_input` is explicitly materialized for the
source windows with a scoped `NO_MERGE(rank_input)` hint. This computes source
identity once per input row instead of inlining the normalization expression
into several window sort keys. Import counts use the same ordered keys as the
winner windows and an explicit unbounded-preceding through unbounded-following
frame, preserving full-partition counts while permitting a shared sort. This
does not alter the separate upstream source-selection work or establish runtime
acceptance; the actual plan and complete eight-second request still need proof.
