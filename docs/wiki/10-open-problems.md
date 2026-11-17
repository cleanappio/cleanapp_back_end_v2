# Open problems and working hypotheses

**In short.** The system collects and describes well. It does not yet group
reports into issues, value them, post a notice that is public, or learn what
happened. This page lists where the code and the idea disagree, the
hypotheses I would test and what would prove each wrong, what can be measured
this month without building much, and the questions only the founder can
answer.

Everything on this page is `[inference]` unless it cites another page. It is
here to be argued with.

## A. Where the code and the idea disagree

| # | The idea or a document says | The system does | Kind | Page |
| --- | --- | --- | --- | --- |
| 1 | Clusters are the unit of meaning. | Groups by hand. Sends by clock. Four grouping stores that do not feed each other. | deep | 06 |
| 2 | Independent reports converge. | `independence` is always "unassessed". | deep | 07 |
| 3 | Routing is to many parties, in waves. | One wave, one address, three companies. | deep | 03 |
| 4 | "If CleanApp ever becomes 'just another inbox,' it has failed." | The main live channel is email to support inboxes. | deep | 03 |
| 5 | CleanApp is a public noticeboard. | `/epc` shows invented rows about real companies. | quick, and urgent because it is public | 04 |
| 6 | The record is public. | A paying owner can make reports about itself private. | needs a decision | 03 |
| 7 | The data is liability data. | An unsourced dollar table, one sentence per report, absent from brand emails. | deep | 05 |
| 8 | "Never invent … billing amounts." | The other prompt in the same file supplies dollar amounts. | needs a decision | 05 |
| 9 | "Legal Risk Factor". | Hazard probability under another name. | quick | 05 |
| 10 | Severity is one number. | Written as 0–1, read in one place as 0–10. | quick | 05 |
| 11 | The system learns who acts. | Seven outcomes ever. No word for "no". | in progress, plus a design gap | 03, 07 |
| 12 | Reports are "clustered with similar reports, and automatically routed … via email, social notifications, DMs" (`ARCHITECTURE.md`). | Email and EPC chat. | document out of date | 03 |
| 13 | Ignoring a signal becomes costly. | Nothing raises the cost of silence. | deep | 09 |
| 14 | Reporters are rewarded. | A token on a test network, as far as the repositories show. | needs a decision | 09 |
| 15 | Ethereum Plus Code. | `OVERVIEW.md` says Ethereum Place Code. | naming | 04 |

## B. Working hypotheses

Each one says what would show it to be wrong.

**H1. Post first, deliver second.** The notice should be a public posting
about an issue. Email and every other private channel should deliver a
pointer to that posting, and each delivery and each answer should be logged
on it.
*Wrong if:* once replies are tracked, private notices are acknowledged at a
useful rate on their own; or counsel judges that public posting about named
companies carries more risk than it is worth.

**H2. The unit is the issue.** Notice, value and outcome belong to an issue,
not to a report and not to a brand.
*Wrong if:* two people judging the same pairs of reports often disagree about
whether they are the same issue. Then the unit has to be looser.

**H3. Fewer and better.** A hundred issues with clean evidence do more than a
million records.
*Wrong if:* the people who receive notices respond to volume more than to
evidence.

**H4. Facts before dollars.** The number of distinct sources, how long the
issue has existed, whether it came back, and how long the party has taken to
answer can all be measured now, and they are what liability turns on. A
dollar figure comes later, from a source the recipient accepts.
*Wrong if:* a buyer says it cannot act without a dollar figure and names a
source it would accept.

**H5. Interested parties create the incentive.** A responsible party acts
when people it answers to can see the notice. Those people are also the
first customers.
*Wrong if:* shown real issue files, no insurer, regulator, journalist or
investor wants them.

**H6. The answer belongs on the record.** "Intended behavior", a fix, and
dated silence are each worth more on the posting than any summary.
*Wrong if:* parties refuse to answer because the answer would be public. Then
record that an answer was given and keep its text private.

**H7. Nothing can be calibrated until a few issues go all the way round.**
Every threshold in page 06 and every score in page 05 is a guess because no
outcome has been recorded. A handful of issues carried by hand from report to
recorded answer would give the first ground truth.
*Wrong if:* the reply tracking merged on 2026-11-17 produces enough outcomes
by itself.

Which number tests which hypothesis (metrics are defined in page 11):

| Hypothesis | Number | Reads as support when |
| --- | --- | --- |
| H1 | `notices_seen`, `chain_3_seen` | private notices alone stay near zero |
| H2 | `same_issue_agreement` | two people agree on most pairs |
| H3 | `signal_share_of_sends` against `notices_seen` | the seen share rises as the first-hand share of sends rises |
| H4 | none yet | a buyer acts on counts and dates alone |
| H5 | none yet | an interested party asks for an issue file |
| H6 | `chain_4_answered` | answers arrive when they are recorded and shown |
| H7 | `fixed_total` | it stays at zero until issues are carried by hand |

## C. What can be measured this month

None of these needs a large build. Each replaces an assumption with a number.
Rows 1 and 4 are now queries in the scorecard (page 11) and need only a run
against production.

| # | Measurement | What it settles |
| --- | --- | --- |
| 1 | Once reply and view tracking are live: of notices sent, the share that bounce, auto-reply, get a human reply, or are opened, within five working days. | H1, H7 |
| 2 | Two hundred pairs of reports about one brand, judged "same issue or not" by two people. Compare their answers with each other, and with every matcher in page 06. | H2; the first calibration set |
| 3 | For the hundred most-reported brands: is there any published way for a third party to report a fault? With the address and the date checked. | page 03; the size of the "no door" problem |
| 4 | EPC census: addresses, notices by status, notices held. Has any EPC chat ever been opened? What does the chat provider show to a third party? | page 04 |
| 5 | First-hand accounts per thousand records, by intake source. | page 08; which sources to restart |
| 6 | How many reports photo comparison has marked resolved, with fifty checked by eye. | page 07; the one automatic fix signal |
| 7 | The share of first-hand records that are repeats, on a larger sample than Anthropic's 120. | page 06 |

## D. The smallest complete loop

A proposal to reason about, not a plan.

Take one issue that is already documented: the Google Ads install counts.
Give it one page that holds the evidence, the number of distinct sources, the
dates, every notice sent, and the answer that came back, including "intended
behavior". Post it on the board at its EPC address. Send the pointer to the
responsible party and to chosen interested parties. Record what each does.

Most of what this needs exists. Three things do not: a place for the party's
answer (page 07), a board that shows real postings (page 04), and delivery to
an interested party (page 09). Building those three for one issue would test
H1, H2, H5 and H6 together, and the result would be real either way.

## E. Questions only the founder can answer

1. What was tried between 2013 and 2018, and between 2019 and 2022? With
   whom? What failed, and why?
2. The patent: what became of it, and does it still matter?
3. Is there a current white paper or CleanApp Standard?
4. EPC: what has the chat provider said about new recipients? Has any brand
   ever opened its EPC chat or asked to claim an address?
5. Google Ads: the disclosure's reference, its date, and the exact reply.
6. Who has paid CleanApp, for what, and what did they say they valued?
7. Which interested parties have asked for this data without being asked?
8. Has a lawyer given a view on posting about named companies, and on what a
   posted notice achieves?
9. Should an owner be able to make reports about itself private?
10. Is the token meant to carry value?
