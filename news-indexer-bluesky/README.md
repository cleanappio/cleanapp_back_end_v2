# Bluesky issue ingestion

The search collector polls fourteen physical and digital issue queries with an
equal page budget. Each poll starts at the newest results, requests a rolling
48-hour window and independently checks the author's `createdAt` timestamp.
Search pagination cursors are never carried into the next poll. Individual
query failures do not prevent the remaining queries from running.

The Jetstream collector uses the same eligibility policy. Public-space issues
require an attached photo; quoted photos and external link previews do not count.
The collector downloads the author's images, including `recordWithMedia` embeds,
and stores their bytes and MIME types before making the post available to the
analyzer. Examples include waste, potholes, accessibility obstructions, damaged
infrastructure, leaks, flooding and fallen trees.

Digital issues require a software, website or product context and a malfunction
or concrete friction signal. They can be text-only. The analyzer confirms an
actionable current bug, broken link, accessibility failure or UX problem and
rejects general commentary, promotion and vague negativity. Physical issues
do not need a company brand to be relevant.

Emergency-alert syndication, status updates and explicitly resolved incidents
are excluded. The submitter also applies the policy to the existing analyzed
backlog, and excludes posts older than 48 hours. It reserves half its batch for
each domain so a busy domain cannot consume all capacity. Policy-rejected
backlog items retain their source evidence but become ineligible for submission.
Existing published reports are not deleted by this rollout.

Wire evidence retains attached photos, original Bluesky links and timestamps.
Large images use the original image URL to stay within the receiver's 2 MiB
request limit. Coordinates are used only when explicitly supplied in recognized
source map links; the collector never substitutes guessed coordinates or `(0,0)`.
Photos alone do not establish a geographic location.

The watchdog checks only eligible fresh work. Historical queues and text-only
physical alerts must not trigger repeated submitter restarts.

## Validation and deployment

`cargo test --bins` covers diverse photo issues, text-only digital problems,
real emergency-alert fixtures, stale/future timestamps, image embed variants,
physical payload photo/location preservation and encoded request body limits.

`Dockerfile.diverse` builds and tests all four workers in one release image.
`cloudbuild.diverse.yaml` takes `_IMAGE` as its immutable deployment tag. Workers
must retain their existing entrypoints/commands, configuration mount, credentials
and network settings when using this shared image.
