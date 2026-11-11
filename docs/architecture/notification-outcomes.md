# Notification outcomes: from "sent" to "someone acted"

Status: **plan. Phase 0 (a library) is in the PR that adds this document; nothing is wired into production.**
Owner of this document: whoever last shipped a phase. Update the status table at
the bottom in the same PR as the code.

This is the working plan for closing the second half of the CleanApp loop: after
a report is routed, find out what happened to it, tell the reporter, and use
what happened to route better next time. It follows the 2026-03-13 decision
"Outcome learning is part of the product, not a reporting afterthought"
(`docs/decisions/decision-log.md`).

Read this first if you are about to touch `email-service`, the notify outcome
tables, the report receipt page, or anything that says a notification was
"delivered".

## 1. Why this exists

On 11 November 2026 a web report about a product bug was traced end to end. It
was analyzed, published and emailed to the product owner's support address
within two minutes. After that the system knew nothing: not whether the mail
arrived, whether anyone read it, or whether anyone answered. The reporter could
see even less. A report is only worth what happens after it is filed, so this
is the part of the product that decides whether the loop is real.

## 2. What exists today (measured, not assumed)

Measured on the production database on 2026-11-11. Re-measure before relying on
these numbers.

| Thing | Where | State |
| --- | --- | --- |
| Outgoing mail | `email-service`, `go-common/mailtransport` | Google Workspace SMTP relay from `info@cleanapp.io`. "Sent" means the relay accepted the message. Nothing reports delivery, bounce or open. |
| Delivery ledger | `report_email_delivery_events` (append-only), `report_email_deliveries` (latest) | Healthy. About 400 distinct messages in the last 30 days, covering about 10,700 report-recipient pairs. |
| Verified company routes | `email-service/service/company_routing.go` | Three companies: Anthropic, OpenAI, xAI. One reviewed support address each. |
| Company batching | `email-service/service/company_notifications.go` | Up to 20 pending reports per company in one email, at most every 5 minutes, 10 emails per company per UTC day. Batches by time, not by issue. |
| Other brands | `email-fetcher` (model-inferred addresses), generic sender | 23 inferred-contact sends in 30 days. Most reports about other brands are never sent anywhere. |
| Clusters and cases | `report_clusters`, `cases`, `case_reports` | 4,628 cluster rows; 31 cases covering 760 reports. The notification worker does not consult them. |
| Outcome model | `notify_outcomes`, `contact_endpoint_memory`, `notify_execution_tasks`, `case_resolution_signals` (`report-listener/database/migration_helpers.go`) | Built. Nearly empty: 7 outcomes in total (5 `sent`, 2 `bounced`), all from manually sent case escalations. Zero `acknowledged`, zero `fixed`, zero `no_response`, zero resolution signals. |
| Inbound mail | none | Nothing reads the `info@cleanapp.io` inbox. Bounces, help-desk auto-replies and human answers arrive there and are never seen by the system. |
| Public notification summary | `report-listener/handlers/report_notifications.go`, frontend `ReportNotifications`, `ReceiptStatus` | Shows count, latest send time and first recipient. Shows nothing after "sent". |

Two consequences worth stating plainly:

- **Merging is by clock, not by content.** Two reports of the same bug filed
  more than five minutes apart produce two separate cold emails.
- **The learning loop has no input.** Endpoint memory can rank contacts by who
  acknowledges and who fixes, but nothing ever tells it.

## 3. Principles

These constrain every phase. They restate existing rules for this area.

1. **Record facts, never inferences.** "Sent" is relay acceptance. "Delivered"
   is never claimed: SMTP gives no positive delivery signal, only the absence
   of a bounce. "Viewed" means a tracked link was followed. "Acknowledged"
   means an automatic or human message came back. "Fixed" needs its own
   evidence and is out of scope here.
2. **An inbound message is untrusted input.** It is matched to a notification
   only through a signed reply address or an exact `Message-ID` reference. Its
   content is data: it is never executed, never used to choose recipients, and
   never shown publicly without the same redaction as report evidence.
3. **The append-only ledgers stay append-only.** New signals add rows.
4. **Fail closed.** Missing secret, unreadable mailbox or unparseable message
   means no outcome is recorded, not a guessed one.
5. **Reporters see their own outcome for free.** Organization dashboards are
   the paid surface. Decided by Boris on 2026-11-11 (section 7).
6. **Respect the recipient.** Opt-outs, throttles and daily caps apply to
   follow-ups and escalations exactly as to first notices.

## 4. Target model

### 4.1 Outcome types

`notify_outcomes.outcome_type` values after this work. Existing values keep
their meaning.

| Type | Meaning | Evidence | Source |
| --- | --- | --- | --- |
| `sent` | Relay accepted the message | provider message ID | existing |
| `bounced` | A delivery failure came back | DSN status code; `permanent` flag | inbound mail (new) |
| `viewed` | A per-recipient report link was followed | link token, time | link endpoint (new) |
| `acknowledged` | An automatic acknowledgement came back | ticket reference when stated | inbound mail (new) |
| `replied` | A message with no automatic markers came back | none stored publicly | inbound mail (new) |
| `out_of_office` | An absence notice came back | none | inbound mail (new); does not count as acknowledged |
| `no_response` | No bounce, view or reply within the waiting period | the period used | escalation worker (new) |
| `misrouted`, `fixed` | Unchanged | unchanged | existing manual path |

### 4.2 Tying an inbound message to a notification

Each outgoing notification gets an opaque **notification ID** and a
**Reply-To** address that carries it, signed:

```
Reply-To: info+r.<notification id>.<signature>@cleanapp.io
```

Gmail plus-addressing delivers this to `info@cleanapp.io` with no mailbox or
DNS change. The signature is an HMAC over the ID under a server secret
(`REPLY_TOKEN_SECRET`), so nobody can forge a reply "from" another
notification. `go-common/mailreply` implements this:

- `mailreply.Address(base, id, secret)` builds the address.
- `mailreply.ParseAddress` and `mailreply.FindID` recover and verify the ID
  from `Delivered-To`, `X-Original-To`, `To`, `Cc` or `X-Failed-Recipients`.
- `mailreply.Classify(header, text)` returns bounce, auto-acknowledgement,
  out-of-office, unsubscribe or human reply, with the bounce status code and
  the ticket reference when present.

The package is pure functions with table-driven tests
(`go-common/mailreply/mailreply_test.go`). It performs no I/O.

**Bounces do not use Reply-To.** A delivery failure is returned to the
envelope sender (`info@cleanapp.io`), not to the Reply-To address. It still
lands in the same mailbox, and it is matched by the original `Message-ID`
that the bounce quotes, which is why `notification_messages` stores
`provider_message_id`. Replies, auto-acknowledgements and out-of-office
messages do use Reply-To and are matched by the signed address.

A new table maps the ID to what was sent:

```
notification_messages
  notification_id   CHAR(16) PRIMARY KEY     -- opaque, random
  provider_message_id VARCHAR(255)           -- RFC Message-ID, for In-Reply-To matching
  recipient_email   VARCHAR(320)
  company_key       VARCHAR(64) NULL
  thread_key        VARCHAR(64) NULL         -- see 4.4
  sent_at           TIMESTAMP
notification_message_reports
  notification_id, seq                        -- which reports the message carried
```

### 4.3 Reading the inbox

A small worker (new service or a mode of `email-service`, decided in phase 1)
reads `info@cleanapp.io`, and for each new message:

1. Finds a valid notification ID (`mailreply.FindID`), or falls back to an exact
   `In-Reply-To` / `References` match against `provider_message_id`.
   No match: record the mailbox message ID as `unmatched` and stop.
2. Classifies it (`mailreply.Classify`).
3. Writes one `notify_outcomes` row per report the notification carried, and
   updates `contact_endpoint_memory` through the existing
   `recordNotifyOutcomeAndMemory` path.
4. For `unsubscribe`, calls the existing opt-out path.
5. Records the mailbox message ID in `inbox_messages` so it is handled once.
   Processing is idempotent on that ID. The worker never writes to the
   mailbox (no labels, no read marks), so it needs read-only access.

Mailbox access is the one thing this needs from a human: see section 7.

### 4.4 Merging by issue

Before the company worker builds an email, it groups pending reports by case
(`case_reports`), falling back to cluster (`report_clusters`). For each group:

- No earlier notification for that case and recipient: send as today.
- An earlier one exists: send a **follow-up in the same thread**
  (`In-Reply-To` and `References` set to the first message's ID, same subject)
  that states the running count ("3rd report of this issue") and links the new
  reports. `thread_key` records the case so later reports find the thread.

Follow-ups count toward the daily cap. A follow-up is never sent to an address
that has bounced permanently or opted out.

### 4.5 Viewed

Report links in notification emails carry a per-recipient token:

```
https://www.cleanapp.io/r/<link token>
```

The endpoint records a `viewed` outcome and redirects to the public report
page. It records at most one view per notification and report per hour,
ignores obvious link scanners (requests within two seconds of send, or with a
known scanner user agent), and never sets a cookie or stores an IP address.
It always redirects: an invalid token, an unknown notification or a report
that is no longer public goes to the home page and records nothing.

The token is `<notification id>.<report seq in base 36>.<signature>`, built
by `mailreply.LinkToken` and verified by `mailreply.ParseLinkToken`. It uses
the reply secret under a different label, so one signature cannot stand in
for the other. No tracking pixel is used: support desks
strip them and they overstate attention.

### 4.6 Escalation on silence

A worker looks at notifications older than the waiting period (default 5
working days, configurable per route) with no `bounced`, `viewed`,
`acknowledged` or `replied` outcome. It records `no_response` and, when the
route has another contact, plans the next one through the existing notify plan
waves. Permanent bounces skip the wait. Each route has a hard limit on
escalation steps.

### 4.7 More verified routes

`company_routing.go` grows from three companies to the brands with the most
firsthand reports. Each route keeps its cited source URL and review date. A
route may be an email address, a web form or an issue tracker; non-email
routes are recorded as planned tasks (`notify_execution_tasks`) until an
executor exists for that channel. Model-inferred addresses stay a fallback and
are never promoted to "verified" automatically.

### 4.8 What the reporter and the public see

`GET /api/v1/reports/notifications` (public summary) gains a `timeline`: the
ordered outcome types with times, and for acknowledgements the ticket
reference. It never includes message bodies or sender addresses. The receipt
page's status list (`cleanapp-frontend`, `ReceiptStatus`) and the report page
render it:

```
Received → Analyzed and published → Sent to Anthropic support
→ Acknowledged (ticket 48213) → Viewed → Replied
```

and, when relevant, "Bounced" or "No response after 5 working days; trying a
second route".

## 5. Phases

Each phase is its own PR and its own deploy, behind a flag that defaults off.

| # | Phase | Ships | Flag | Done when |
| --- | --- | --- | --- | --- |
| 0 | Reply library | `go-common/mailreply` and this document | none (not wired) | tests pass in CI |
| 1 | Reply addresses and inbox reading | `notification_messages` tables, `Reply-To` on company notifications, inbox worker | `REPLY_TRACKING_ENABLED` | a real auto-reply to a test notification appears as `acknowledged` in `notify_outcomes` |
| 2 | Viewed links | `/r/<token>` endpoint, tokenized links in the company template | `VIEW_TRACKING_ENABLED` | following a link in a test email records exactly one `viewed` |
| 3 | Timeline in the public summary and receipt | API `timeline`, frontend status list and report page | none | the receipt for a test report shows acknowledgement with its ticket reference |
| 4 | Merge by issue | grouping by case, threaded follow-ups | `ISSUE_THREADING_ENABLED` | two reports of one case produce one thread, not two cold emails |
| 5 | Escalation on silence | `no_response` worker, next-route planning | `ESCALATION_ENABLED` | a seeded stale notification gets `no_response` and a planned second route, and respects caps |
| 6 | Route coverage | reviewed routes for the top brands | data change | each new route has a source URL and review date and passes the routing tests |

Order matters: 3 and 5 need the data that 1 and 2 produce. 4 and 6 are
independent and can run in parallel with 1.

## 6. Privacy, safety and abuse

- Inbound bodies are not stored in `notify_outcomes`. `evidence_json` holds the
  classification, the status code, the ticket reference and the mailbox
  message ID only.
- A reply address is a capability: anyone who has it can make a notification
  look "replied to". That is why outcomes are labelled by what was received,
  not by who sent it, and why `replied` is never shown as "the company
  answered" without the sender domain matching the recipient's.
- Link tokens are single-purpose and reveal nothing about the reporter.
- The inbox worker needs read-only access to one mailbox. It must not have
  send or modify permission; sending stays with the relay.
- The secret for reply and link tokens lives with the other runtime secrets and
  is never logged. Rotating it invalidates old reply addresses, so rotation
  keeps the previous secret valid for 90 days.

## 7. Decisions needed from a human

1. **Mailbox access. Open: needs a Workspace super admin.** The inbox worker
   needs to read `info@cleanapp.io`. Options: a Google Workspace service
   account with domain-wide delegation limited to the Gmail read-only scope
   for that one user (recommended), or an app password over IMAP. Exact steps
   are in section 10. An agent must not do this: it creates a credential and
   changes domain security settings, so a human does it and stores the key
   with the other runtime secrets.
2. **Paywall. Decided 2026-11-11 (Boris): yes, free.** A reporter sees the
   outcome of their own report without paying. Phase 3 puts the timeline on
   the receipt and removes the "For subscribers" gate for the reporter's own
   report. Organization dashboards stay paid.
3. **Waiting period** before `no_response`: default 5 working days.
4. **Which brands** get verified routes first. Default: top 25 by firsthand
   report count over the last 90 days.

## 8. How to verify each phase safely

Production is the only live environment. Use `EMAIL_DRY_RUN=true` and
`./main --notification-plan` (see `email-service/README.md`) to inspect what
would be sent. Test reply handling against a notification addressed to a
mailbox you control, never against a real company address. Synthetic reports
must not be routed to companies.

## 9. Status

| Phase | State | PR | Deployed |
| --- | --- | --- | --- |
| 0 Reply library | done | #196 | not applicable (library only) |
| 1a Reply addresses and `notification_messages` | code merged, flag off | this PR | see deploy log; `REPLY_TRACKING_ENABLED` unset |
| 1b Inbox worker | blocked on mailbox access (section 10) | | |
| 2 Viewed links | code merged, flag off | PR after #197, plus the site forward in `cleanapp-frontend` | `VIEW_TRACKING_ENABLED` unset |
| 3 Timeline | code merged, no flag (empty until outcomes exist) | PR after phase 2, plus `ReceiptStatus` in `cleanapp-frontend` | see deploy log |
| 4 Merge by issue | not started | | |
| 5 Escalation on silence | not started (needs outcomes from 1b and 2) | | |
| 6 Route coverage | not started | | |

### What 1a changed

- `go-common/mailtransport`: `Message.ReplyTo`, written as a `Reply-To`
  header, validated like every other address (bare ASCII address, no control
  characters). It is never an envelope recipient.
- `email-service/service/reply_tracking.go`: `prepareReplyTracking` creates
  the notification ID and signed address; `recordNotificationMessage` writes
  `notification_messages` and `notification_message_reports` after the
  transport accepts the email. Both are called from `processCompanyPlan` only
  (the verified company path). Inferred-contact and individual sends are
  unchanged.
- `email-service/service/email_service.go`: `RunMigrations` creates the two
  tables (`CREATE TABLE IF NOT EXISTS`, safe to re-run).
- Config (`email-service/config/config.go`):

  | Variable | Default | Meaning |
  | --- | --- | --- |
  | `REPLY_TRACKING_ENABLED` | `false` | Adds the signed Reply-To and records each accepted send. |
  | `REPLY_TOKEN_SECRET` | none | HMAC secret, at least 16 bytes. Never logged. |
  | `REPLY_MAILBOX` | `info@cleanapp.io` | Mailbox the plus-address is built on. |

  If the flag is on and the secret is missing or too short, the service logs
  one error, keeps tracking off and sends normally. Tracking can never stop
  or fail a notification.

### Turning 1a on

1. Generate a secret (`openssl rand -hex 32`) and store it as
   `REPLY_TOKEN_SECRET` with the other email-service secrets. Do not commit
   it and do not print it.
2. Set `REPLY_TRACKING_ENABLED=true` and release through the AWS deploy
   workflow.
3. Check: the next company notification has a `Reply-To` of the form
   `info+r.<16 hex>.<16 chars>@cleanapp.io`, and
   `SELECT COUNT(*) FROM notification_messages` goes up by one per email.

Turning it on before the inbox worker exists is harmless and useful: replies
still arrive in `info@cleanapp.io` exactly as today, and the mapping needed
to match them later starts accumulating.

### What 2 changed

- `go-common/mailreply/link.go`: `LinkToken` and `ParseLinkToken`.
- `email-service`: `prepareViewLinks` (in `service/reply_tracking.go`) fills
  `BrandReportSummary.ReportLinks`; the company template uses a tracked link
  wherever it would have used the public report link. Config:

  | Variable | Default | Meaning |
  | --- | --- | --- |
  | `VIEW_TRACKING_ENABLED` | `false` | Use tracked links in company notifications. Needs reply tracking on. |
  | `VIEW_LINK_BASE_URL` | `https://www.cleanapp.io/r/` | Prefix of a tracked link. Must be HTTPS. |

- `report-listener`: `GET /r/:token` and `GET /api/v3/r/:token`
  (`handlers/notification_links.go`), backed by
  `database.RecordNotificationView`. It reads `REPLY_TOKEN_SECRET`; without
  it every link redirects to the home page and nothing is recorded.
- A `viewed` row in `notify_outcomes` looks like: `subject_kind='report'`,
  `subject_ref=<public id>`, `endpoint_key=<recipient address>`,
  `source_type='notification_link'`, `source_ref=<notification id>`.

### Turning 2 on

Order matters, because a tracked link that nothing answers is a broken link
in a company's inbox:

1. `cleanapp-frontend` forwards `www.cleanapp.io/r/<token>` to the
   report-listener endpoint, and that is deployed.
2. `report-listener` is deployed with `REPLY_TOKEN_SECRET` set to the same
   value as `email-service`.
3. Check by hand: `https://www.cleanapp.io/r/x` redirects to the home page.
4. Only then set `VIEW_TRACKING_ENABLED=true` on `email-service`.

### What 3 changed

- `report-listener/database/report_outcome_timeline.go`:
  `ReportOutcomeTimeline(publicID)` reads `notify_outcomes` for the report
  and returns the public outcome types in time order. Repeats collapse into
  the first occurrence with a `count`. The published types are an allowlist
  (`publicOutcomeTypes`); anything else stays internal.
- The notification summary (`GET /api/v3/reports/notifications/by-public-id`
  and the summary embedded in the public report) gains
  `timeline: [{type, at, count, ticket_ref?}]`. It is always a list. Only the
  ticket reference is read from `evidence_json`, and only if it looks like
  one (24 characters at most; letters, digits, `-`, `#`).
- `cleanapp-frontend`: `ReceiptStatus` lists the timeline under "Sent to";
  labels live in `src/lib/report-notifications.ts` (`outcomeLabel`).
- No flag: with no outcome rows the timeline is empty and nothing changes on
  screen. It fills as phases 1b and 2 start recording.
- The per-recipient log (`/notifications/full/...`) is unchanged and still
  requires a subscription. The reporter's own outcome is in the free summary.

## 10. Mailbox access: steps for a Workspace super admin

Recommended option, about ten minutes. The result is a JSON key that can
read mail, and nothing else, through the one scope below.

1. Google Cloud console, project of your choice (or a new one named
   `cleanapp-inbox`): enable the **Gmail API**.
2. IAM and Admin, Service Accounts: create `cleanapp-inbox-reader`. Grant it
   no project roles. Create a JSON key and download it.
3. Copy the service account's **Unique ID** (a number, the OAuth client ID).
4. Google Admin console, Security, Access and data control, API controls,
   Manage domain-wide delegation, Add new. Client ID: the number from step 3.
   Scope, exactly one:
   `https://www.googleapis.com/auth/gmail.readonly`
   (Do not add `gmail.modify`, `gmail.send` or `https://mail.google.com/`:
   each of those can send or change mail. Read-only is enough because the
   worker keeps its "already processed" state in the database, not in Gmail
   labels.)
5. Put the JSON key on the production host as a mounted secret file and set
   `INBOX_CREDENTIALS_FILE` to its path and `INBOX_USER=info@cleanapp.io`.
   Never paste the key into chat, a PR or a log.

Domain-wide delegation lets the service account act as any user it names
within those scopes; the worker names only `INBOX_USER`. If that is not
acceptable, use the alternative: turn on 2-Step Verification for
`info@cleanapp.io`, create an **app password**, and provide it as
`INBOX_IMAP_PASSWORD_FILE`. That credential is broader (full IMAP access to
the mailbox) but limited to one user.
