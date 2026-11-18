# EPC and public notice

**In short.** The idea of the Ethereum Plus Code is that every brand and every
place has a public address which exists before anyone claims it, and that
CleanApp posts its notices on a board anyone can read. Publication can be
verified independently of recipient participation. Three parts are built:
an address for each brand, a durable queue and dispatch through a chat
service. The live `/epc` page is titled "CleanApp Public Noticeboard" and
shows sample rows. The proposed board would publish an issue-specific notice
with evidence, dates and a correction history. [Notice and remediation](12-notice-and-remediation.md)

Last reviewed 2026-11-18. The [live page](https://www.cleanapp.io/epc) loaded
a [browser bundle](https://www.cleanapp.io/_next/static/chunks/pages/epc-db00250a1c44ecec.js)
containing hard-coded counts for real brands and generated EPC identifiers.

## The idea, in the project's words

> "**Ethereum Plus Codes (EPCs)** are like Google Plus Codes, but better.
> Every brand and place with CleanApp Reports automatically gets an EPC, a
> public address where anyone can send messages. Want to message the Eiffel
> Tower or Red Bull? With EPCs on CleanApp, you can."
> — `cleanapp-frontend/src/pages/epc.tsx`

> "You want an address for each Ethereum Place Code (EPC), potentially with
> some smart contract functions down the line, and you dont want to
> administrate a private key for each EPC."
> — `epc-pusher/OVERVIEW.md`

`[founder]` EPCs were introduced to give public notice, which is what
CleanApp has always tried to be: a public bulletin board.

`[2018 wiki]` The earlier statement of the same aim: a shared ledger is wanted
because it is "more secure against censorship and regulatory capture".

`[inference]` Put together, the idea has four properties, and they are what
separate it from every channel on page 03:

1. **The address exists first.** Nobody has to sign up, publish a contact or
   agree to be reachable.
2. **Publication has its own receipt.** A reader can inspect the issue and
   see when it was posted. Transport acceptance, accountable-role reach and
   legal effect each need separate evidence.
3. **Anyone can check it.** That a notice was posted, about what and when,
   does not rest on CleanApp's word.
4. **The addressee can claim the address later** and answer in the same
   place.

## What is built

| Part | How | Where | Status |
| --- | --- | --- | --- |
| A key per subject | `cleanapp/<brand>` today. A different prefix per campaign, for example `gplus/<shortcode>` for places. | `epc_campaigns`, `epc_contracts`; `OVERVIEW.md` | brands live; places plan |
| An address per key | CREATE3 gives an address that depends only on the key and the factory, so it can be known before any contract exists. The worker asks the factory contract at `0xDc41…FAA`, in a local EVM, for the address of a key. | `epc-pusher/src/epc.ts` | live |
| A notice per public report | "You have a new CleanApp Report. Please see `https://cleanapp.io/digital/<brand>`. This is your new Ethereum Plus Code. Please check in periodically for updates and future CleanApp reports." | [Report template](../../epc-pusher/lib/templates/report_notification.tpl) | live |
| A durable queue | One row per notice: campaign, key, address, event key (`prod:report:<seq>`), rendered text, status (held, pending, sending, sent, error, uncertain), the provider's message ID, attempts, times. | `epc_outbox`; patch `20261113_epc_cumulative_dispatch.sql` | live |
| Delivery | Blockscan Chat, a messaging service addressed by Ethereum address. Sends are paced: 1.2 seconds apart in the recommended settings, never under 1 second. | `epc-pusher/src/worker.ts`, `notifyReports.ts` | live, pilot scope |
| Guards | Only reports that are public, active, have a public ID and no private owner. Suppressed imports are skipped. Rechecked before sending. | `epc-pusher/README.md` | live |
| Honest failure states | A timeout or a crash after the request leaves the notice "uncertain". It is never re-sent automatically, because the provider offers no way to make a send repeat-safe. | same | live |

"Pilot scope": Blockscan currently accepts messages to addresses CleanApp has
written to before and rejects new ones. With `EPC_EXISTING_CHATS_ONLY=true`
the worker sends only to those and holds the rest for later
(`ebbbd02e`, 2026-11-13). How many addresses that is, and how many notices
have been sent, is in the production database and was not read for this page.

## History

| Date | Commit | What |
| --- | --- | --- |
| 2025-09-15 | `3b15671e` | EPC backend imported. |
| 2025-10-07 | `572a4cec`, `01181ccb` | "Working EPC Pipeline (ready to go)". Sending switched on in the development environment from report 29,604 (`setup/setup.sh`). Production left off. |
| 2025-10 to 2026-10 | | No production sending. The notices of this period went from the development system `[inference]` from `setup/setup.sh` and the `legacy-dev` label. |
| 2026-11-07 | `8931f82a` | Imported archives are barred from producing notices. |
| 2026-11-13 | `47f3313e` | Production: durable queue, import of the earlier notices under the label `legacy-dev`, registration as an AWS role. |
| 2026-11-13 | `ebbbd02e` | Existing chats keep sending while new recipients are held. |

## What is not built

| Missing | Why it matters | Evidence |
| --- | --- | --- |
| The contracts | The worker derives an address. Contract deployment and control of the resulting address are separate steps. | [EPC worker documentation](../../epc-pusher/README.md) |
| Claiming | A brand or a place cannot take over its address, read as its owner, or reply. | no code |
| Place codes | "Every brand and place": only brands have keys. | `gplus/` appears only as an example |
| A record others can verify | The private outbox stores dispatch state and the chat provider's receipt. Public publication of the exact issue needs a separate record. | `epc_outbox` |
| The board | `/epc` generates rows from 24 hard-coded brand names and sample counts, with codes of the form `EPC-000001`. | [Browser bundle observed 2026-11-18](https://www.cleanapp.io/_next/static/chunks/pages/epc-db00250a1c44ecec.js) |

## Public surfaces and evidence

| Thing | What a reader can establish |
| --- | --- |
| The address for a brand | Yes, in principle: anyone can compute it from the key and the factory. Nowhere is it shown. |
| The brand page the notice points to | Yes, and it is real. |
| EPC chat dispatch | Provider status and timestamps reside in the private outbox. |
| The text of the notice | Not on any chain. Whether a third party can read a Blockscan Chat message addressed to someone else is `[not verified]`. |
| Reading as the addressee | `[not verified]` Determine who can sign in to the derived address and read an actual message. |

The [case studies](https://www.cleanapp.io/clustersamples) already demonstrate
an evidence-backed public record. The Google Ads example exposes the dated
transport receipts it can support and the limits of the underlying evidence.

**First correction.** Label `/epc` clearly as a preview or remove the
sample rows. A public noticeboard derives its credibility from accurate
issue-level evidence and a visible correction history.

## What would make the notice public

1. **Publish a reviewed notice.** Show the issue reference, date and version,
   a safe evidence summary, public links, and corrections or responses cleared
   for publication. The private outbox can supply dispatch evidence for a
   separate delivery status. Keep private recipients and provider identifiers
   in the operating record.
2. **Show the address** on relevant public pages when it can be verified,
   alongside the dated notices actually published there.
3. **Make the record tamper-evident.** For example, publish a daily hash of
   approved public notice versions to a public chain. With a reproducible
   proof, a reader could verify that a specific version was committed by a
   given date. Keep notice content and restricted evidence off chain.
4. **Deploy the factory's contracts** so that an address can hold state:
   notices, their dates, and later a reply.
5. **Claiming.** A way for the named party to prove it is the brand or the
   owner of the place, take the address, and answer there.
6. **Places.** A key per place, so that a pothole and a building entrance
   have an address in the same way a brand does.

## Questions to reason through

1. **What makes a notice count?** In law there are regimes where a notice
   that meets a stated form has a defined effect, and regimes where what
   matters is whether the party "knew or should have known". Which of these
   is CleanApp aiming at, for which kinds of defect, in which countries? This
   needs a lawyer's answer, not an engineer's.
2. **Who is the board for?** The named party, who may never look? Or the
   people who will ask the named party why it did nothing: an insurer, a
   regulator, a journalist, a court, a customer?
3. **One notice per report, or per issue?** Today it is per report, and every
   notice is the same sentence with a link to the brand page.
4. **What goes on the board when the party answers "no"?**
5. **How should private intake coexist with the board?** Matching ownership
   rows can suppress public visibility. Keep publication decisions independent
   of subscription status. [Ownership filter](03-notice-and-routing.md#private-ownership-and-public-visibility)
6. **Why Ethereum, stated as a requirement?** The 2018 answer was resistance
   to censorship. If the requirement is "CleanApp cannot later alter or
   remove the record, and neither can the party named", several designs meet
   it. The choice should follow from the requirement.
7. **The name.** The page says Ethereum Plus Code. `OVERVIEW.md` says Ethereum
   Place Code. Which is it?
