# Incentives and buyers

**In short.** Three groups need a reason to take part: the person who
reports, the party that must act, and the party with a stake in the answer.
Rewards for the reporter were built first and most. The party that must act
has been offered a dashboard and a subscription. The party with a stake, named
as the payer in 2018 and in every statement of the idea since, has no product
at all. Nothing in the system makes ignoring a notice cost anything.

Last reviewed: 2026-11-17.

## The reporter

| Mechanism | Since | Rule | Status |
| --- | --- | --- | --- |
| Referral | 2023-10-25 `d7028298`; paid 2024-02-25 `656528eb` | Each referrer up the chain gets a tenth per level. | live |
| Teams | 2023-11-22 `cd8928f0` | Green against blue, by report count. | live |
| Leaderboard | 2023-11-29 `d2c18432` | Sum of earned and paid tokens. | live |
| KITN token | 2023-12-24 `72fac8a9` | One per report, one on sign-up, one per photo match. | live |
| On-chain payout | 2024-04-20 `b079cd93` | A daily job sends balances to wallets. | live |
| Sort game | 2026-04-04 `0a75cfe9` | One KITN per vote on a report's urgency. | live |
| Push when something happens | 2026-03-15 `7925c866` | | live |
| Outcome receipt, free | decided 2026-11-11 | The reporter sees what happened to their report without paying. | built, not deployed |

About the token:

- 10,000,000,000 KITN are minted by the contract in `cleanapp-token`.
- The explorer link the app gives a reporter points at
  `sepolia.basescan.org`, a test network (`backend/server/get_blockchain_link.go`).
  The `cleanapp-token` repository deploys to Base Sepolia.
- `[inference]` As far as the repositories show, the token has no market
  price. Whether a main-network deployment exists is not recorded here.
- A token balance counts activity. It does not depend on whether the report
  was true, new, or led to anything.

`[2018 wiki]` The original aim was wider: rewards for "a) reporting
waste/hazards; b) performing verification/analytics; c) incentivizing
effective response(s)". Only (a) and a small part of (b) were built.

`[inference]` The reward the documents care most about is not a token.
`WHY.md`: "Users report once, see nothing happen, and stop." The reward is
that something happened and the reporter was told. That depends on outcomes
(page 03).

## The party that must act

| Offer | Since | Status |
| --- | --- | --- |
| A dashboard for a customer's area or brand | 2025-07 | live: Montenegro, Red Bull, New York, DevConnect, Edge City |
| Ownership, and the option of keeping reports private | 2025-08-19 | live |
| Plans: Free; Pro $99 a month; Enterprise $499 a month for up to five brands or places | 2025-06-14 `82eaa94e`; prices in `pricing.tsx` | live |
| Brand intelligence chat, limited free turns, "Pro unlocks…" | 2026-02-12 `839df3d4` | live |
| Email with an "Estimated Liability" box | 2025-12-09 | live on one path only (page 05) |
| "Cost Impact Analysis", "Predictive Risk Assessment", "Risk and hotspot forecasting" | | named on screen; not built |

Who pays, and how much has been paid, is not in the repositories.

`[inference]` These surfaces can help a team that already wants useful
feedback. The operating question is whether CleanApp lowers the recipient's
cost of investigating and acting. A compact issue with before evidence, a
specific ask, and a route into the team's existing workflow can save time.
A verified repair record and fewer duplicate follow-ups can give the team
a reason to share an update.

## The party with a stake

`[2018 wiki]` "To access CleanApp Analytics, interested parties pay a small
subscription fee, commensurate with their intended use."

`THEORY.md` §4 lists them: regulators and auditors; researchers and
watchdogs; competitors and market participants; insurers and risk analysts;
journalists and civil society; internal teams whose performance is
implicated. "The same cluster can support multiple valid value propositions."

`[founder]` Customers are both responsible parties and interested parties.

What exists for them:

- Notify plans have a second and a third wave, "Authorities and oversight"
  and "Other stakeholders" (page 03). An operator can select eligible targets
  through case escalation. Routine automated delivery and observed buyer
  use remain unverified.
- `authority_directory_rules` holds seeded rules for public bodies.
- No page, feed, export, alert or price is aimed at an insurer, a regulator,
  a journalist or an investor.

`[inference]` The value to an interested party depends on the decision it can
make with this evidence. An insurer could use a verified timeline in a risk
review. A regulator could use a scoped record to prioritize an inspection.
Buyers and concrete uses need validation before they drive product design.

## The shape of the problem

`[inference]`

1. A responsible team may act because the evidence saves investigation time,
   because the defect imposes ongoing costs, or because someone with oversight
   can act on the record.
2. A party may repair quietly and still prefer silence. Offer a low-friction
   reply, an existing ticket reference, and visible credit for an independently
   verified change. Keep reply and correction access free.
3. Public visibility can influence priorities if the facts are reliable and
   a relevant audience has reason to use them. Private and official channels
   can deliver evidence into a workflow with real authority. The applicable
   legal effect needs its own analysis.
4. Paid integration, analysis, and partner commitments are business
   hypotheses. A party's payment should have no bearing on the public evidence
   or the ability to reply. [Notice and remediation](12-notice-and-remediation.md)

## Questions to reason through

1. Which interested party would pay first, for which class of defect?
2. What would a responsible party pay for in triage, workflow integration,
   or evidence that its fixes hold up?
3. How should independently submitted observations and authorized
   enterprise-private intake coexist?
4. What should a reporter earn, and should it depend on the outcome?
5. Does the token still have a role, and if so, what does it reward?
