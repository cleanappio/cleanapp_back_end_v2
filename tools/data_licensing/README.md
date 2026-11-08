# Evidence-first dataset reconciliation and review-set selection

This is the durable home of the algorithm used to prepare CleanApp's November 8,
2026 licensing review set. The `review500.json` profile preserves its category
quotas, metadata gates, scores, diversity penalties and staged cap relaxations.
It is configuration, not a claim that these settings fit every future buyer.

The tool is offline, model-independent and Python-standard-library only. It
reads JSONL exports and saved evidence. It has no database, network, model,
upload, delete or production-visibility operation. Selection creates a **local
owner-review proposal**, never permission to license or distribute data.

## Entry points

Run from the repository root with Python 3.10 or later:

```sh
python3 tools/data_licensing/sorter.py adapt \
  --input /data/vetted_pool.jsonl --metadata /data/current_metadata.jsonl \
  --dimensions /data/saved_dimensions.jsonl --holds /data/current_holds.jsonl \
  --output /data/normalized.jsonl

python3 tools/data_licensing/sorter.py reconcile \
  --input /data/normalized.jsonl --output /data/dispositions.jsonl

python3 tools/data_licensing/sorter.py select \
  --input /data/normalized.jsonl --config tools/data_licensing/review500.json \
  --output /data/selected.jsonl

python3 -m unittest discover -s tools/data_licensing/tests -v
```

Paths above are examples supplied by the caller. Do not commit private exports,
images, tokens, identity values or local review notes. Existing output files are
never overwritten. A failed run does not publish a partial output.

`adapt_saved_review_pool()` accepts the original three export shapes: pool rows
with `saved_quality_proof`/`saved_visible_review_proof`, current `kind=report` and
`kind=analysis` rows, and saved dimension rows with `old_width`/`old_height`.
The adapter omits local image paths. It pins the metadata and complete hold
snapshot hashes and attaches holds by report ID or exact original hash. It does
not turn an unverified database claim into a fresh byte-hash verification.

Other datasets can use their own adapter to emit the normalized contract below.
Streaming `reconcile` can cover an entire database export without materializing
all rows. Selection is a greedy O(eligible candidates × selected items) pass:
materialize a bounded, qualified projection before selecting a large review set.
There is no assertion that this selector is a production database scan service.

## Required normalized evidence contract

Each candidate is one report/media-original binding. `seq` is a unique positive
integer; a report with multiple media needs an adapter with explicit media-item
identity rather than silently duplicating the report sequence.

| Fields | Meaning |
| --- | --- |
| `original_sha256`, `current_original_sha256` | Expected retained-original bytes and verified current binding; both must agree. |
| `source_snapshot_sha256` | Hash of the current source export used for this decision. |
| `hold_snapshot_sha256`, `holds_complete: true` | Provenance and explicit completeness declaration for current restrictions. Missing is a hold, not a clean record. |
| `quality_evidence` | Exact `original_sha256`, status `eligible`, and `artifact_sha256` of the saved quality evidence. |
| `visible_review` | Exact `original_sha256`, status `clear_for_private_analysis`, and saved-review `artifact_sha256`. |
| `visibility`, `active_actions`, `shared_holds` | Current source-state gates; public visibility alone establishes no rights. |
| `width`, `height`, `image_bytes`, `mime_type`, `header_hex` | Saved or current metadata, not recomputed image analysis. Width/height are actual image pixels, never vector dimensions. |
| `latitude`, `longitude`, `source_group` | Retained coordinates and pseudonymous source grouping for selection diversity; not verified geography or independent people. |
| `stratum`, `base_score` | Adapter-provided selection context. Text-derived strata remain text-derived; never call them human-verified labels. |
| `dhash` | Optional **already saved** 16-hex perceptual hash. No new image hashing/decoding is performed to create one. |
| `annotations`, `report_description` | Existing model/source text with provider and language roles preserved. |
| `holds`, `supersessions` | Typed saved events and narrow, explicit later review decisions, described below. |

Quality and visible-review metadata are trusted adapter inputs, not cryptographic
proof that the review was correct or human. The caller must validate artifact
hashes, source versions, current original bytes and authority. Historical private
analysis eligibility, especially a contact-sheet review, does not certify full
resolution privacy, content accuracy, authorship, rights or training clearance.

## Typed holds and supersession

`disposition()` keeps privacy, unresolved details, manual-quality candidates,
quality/relevance, caption hints, alias and curator restrictions separate. It
never emits `global_junk_confirmed: true` or privacy/rights clearance. Unknown
hold kinds remain held. A SHA-only image hold follows exact-media aliases; a
report-only hold remains report-only. Never zip flat ID and hash arrays into
invented pairs. A new original hash does not inherit an old image's quality proof.

An explicit-zero-urgency spam vote is a **review candidate**. Positive-urgency
mobile votes encoded as spam are ambiguous, not binary spam decisions. No vote
means unknown, and translations are not independent votes or affected people.

Only `kind=caption_hint` can be retired here. A supersession must name its
`supersedes_id`, match its kind and exact `reason_code`, bind to the current
original and source-snapshot hashes, identify an `explicit_saved_review` or
`explicit_owner_review`, pin that artifact hash, and use decision
`retire_specific_hint`. Its timezone-aware `reviewed_at` must be later than the
hold's timezone-aware `observed_at`. A general generated caption, a missing
timestamp or an older annotation cannot retire a newer finding. Retiring one
black/blank inference does not clear an independent SHA-only, privacy or other
restriction. Privacy/rights release requires its own reviewed workflow.

`annotation_consensus()` preserves the narrow saved-text rules in `text_rules.py`.
It nominates review only when distinct saved English variants support one narrow
quality category without contradictory useful text. It treats original reporter
purpose, curator notes, digital-failure context and disagreement as review needs.
Duplicate rows/translations do not create independence. A black bag, blurred
background, illegible sign or person incidental to a useful issue must not become
a whole-image junk verdict. Saved pixel decisions and explicit useful-dark
overrides remain their own evidence; speculative AI narrative is not pixel truth.

## The preserved 500-image selection profile

Default quotas are 140 infrastructure, 105 litter, 85 dumping/construction,
60 accessibility, 55 bins/collection, 35 water/utilities and 20 other physical
observations. These are a **curated diversity showcase**, not a random or
statistically representative corpus sample. Physical-only selection is a profile
choice, not a restriction on CleanApp's physical/digital architecture.

The default profile requires a 600-pixel short edge, 800-pixel long edge and
50,000 original bytes; a JPEG header; current English ChatGPT/Gemini descriptions;
saved visible/quality acceptance; no active hold; valid retained coordinates; and
no configured privacy/quality text cue or digital classification in this profile.
Text cue exclusion is conservative sample screening, not a fresh image verdict.
Countries are never guessed from captions or coordinates.

Each round selects the least-filled quota ratio, then maximizes:

```text
base_score + min(12, 3 * log2(max(1, bytes / 50000)))
           + min(10, short_edge / 250)
           - 9 * source_group_count
           - 12 * one_degree_cell_count
           - 18 * 0.02_degree_cell_count
```

Ties prefer the lower report sequence. Configurable cap stages are `(20,30,6)`,
`(25,40,8)`, `(40,60,10)` and `(80,100,15)` for source/coarse/fine grouping. Saved
dHash distance ≤4 within the same coarse cell blocks near-duplicates. Exact raw
SHA duplicates are excluded globally. Deficits can be redistributed to feasible
strata with the configured spare margin; every change is reported. An exhausted
pool fails rather than looping forever. Config input is not mutated.

All thresholds, providers, patterns, category quotas, grids, scores, penalties,
cap stages and saved-dHash distance are in `review500.json`. Changing the profile
requires a new provenance/version note and relevant tests. A filter miss, model
confidence or an attractive caption is not a privacy or quality certificate.

## Original-byte handling and owner review

The optional Python API `freeze_originals(selected, local_paths, output_dir)`
hashes all selected local originals before copying, refuses different existing
destination bytes or symlinks, copies bytes unchanged and verifies every copy.
It creates no thumbnails, crops, masks, synthetic data or new pixel metrics and
does not edit/delete originals. Its JPEG filenames reflect this profile only.
Byte identity proves unchanged bytes, not scene truth, privacy or ownership.

Keep original reporter text, imported text, legacy unknown-authorship descriptions,
generated analyses, tags and curator annotations distinguishable. A missing
annotation remains missing. Capture, report and import times remain separate;
stored coordinates and camera metadata remain attributed source claims.

Every selected image remains pending owner review. Keep review state outside
immutable originals and selection provenance; bind decisions to dataset/item/hash
and preserve them on reload. The local monitor and any external upload are
separate applications, not capabilities of this tool. Do not commit this chat's
monitor tokens, local paths or private sample to the repository.

## Validation and provenance

Synthetic tests cover stale binding, source/version and reason-specific
supersession, privacy preservation, ambiguous urgency, translation/consensus
limits, useful-scene false positives, deterministic diversity, raw/perceptual
deduplication, finite quota exhaustion, atomic output and unchanged-byte copying.
The extracted selector was also checked locally against the private November 8
metadata: all 500 original selections reproduced in the same order, without
opening images or making model calls. Private fixtures stay outside this repo.
`provenance.json` pins the extracted algorithm sources and profile identity.
