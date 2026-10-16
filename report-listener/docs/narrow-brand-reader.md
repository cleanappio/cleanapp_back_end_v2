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

The source winner window carries only the report sequence, native-collation EII
source/external identities, and Wire submission identity. It keeps the original
SQL source/ID/descending-submission-time order. Only Wire submission time is read
for that selection, using its projection or a missing/stale-header PK fallback;
full report/source metadata, typed nodes and publication guards join after the
winning identity. Guards depend only on report sequence, so moving them after
selection cannot change the visible report universe or its full-source overview.

Only thirteen scalar rank/count fields cross source deduplication, case tagging,
family selection and pagination. Public identity, coordinates, source metadata,
and selected-analysis metadata rejoin the same normalized snapshot by the chosen
sequence after paging. The normalized source-rank window leaves exactly one row
per sequence, so these bounded detail joins cannot multiply selected reports.

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
