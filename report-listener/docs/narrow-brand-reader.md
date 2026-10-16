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

Page/sample private metadata retains the exact selected analysis's nullable
language, creation literal, brand, classification and severity. The existing
bounded display hydrator uses those values to avoid mixing tied analyses. It
does not call the full-brand analysis loader. Supporting sample JSON is built
after paging, only for selected families, with the existing eight-sample and
privacy rules. Private selection metadata is removed by public serialization.

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
