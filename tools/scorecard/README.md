# Scorecard

Measures how far a first-hand report travels: received, sent to someone, seen
by a person at the responsible party, answered, fixed. What each number means,
why these numbers, and how to use them is in
[`docs/wiki/11-scorecard.md`](../../docs/wiki/11-scorecard.md).

- `metrics/<id>.sql`: one metric per file. A header says what it measures. The
  body is a single `SELECT`.
- `metrics/_eligible_firsthand.sql`: the one shared definition of which
  reports count. Metrics include it as `{{eligible_firsthand}}`.
- `scorecard.py`: runs each metric through the `mysql` client and prints or
  records the result. Python 3.8 or later, standard library only.

## Run it

Give it a `mysql` client command that selects the database and reads SQL on
standard input. Prefer an account that can only read. The tool cannot write
with any account (see below), so this is a second lock, not the only one.

```bash
python3 tools/scorecard/scorecard.py run --mysql 'mysql --defaults-extra-file=/path/readonly.cnf cleanapp'
```

On the production host the database container keeps a read-only account,
`importer` (`db/init.sql` grants it `SELECT` only; `db/set_passwords.sh` sets
its password from `MYSQL_READER_PASSWORD`). Let the container supply the
password from its own environment:

```bash
python3 tools/scorecard/scorecard.py run \
  --mysql "docker exec -i cleanapp_db sh -c 'MYSQL_PWD=\"\$MYSQL_READER_PASSWORD\" exec mysql -uimporter cleanapp'"
```

Never put a password in the command. It would be visible to other processes
and kept in shell history.

To record a measurement, add `--ledger docs/wiki/scorecard-ledger.csv` and
commit the change. `--note` stores a short remark with the rows, for example
the release that was live. `--as-of 2026-11-17T00:00:00` measures as of a
fixed UTC time instead of now.

Other commands:

```bash
python3 tools/scorecard/scorecard.py catalogue   # the metric list as a Markdown table
python3 tools/scorecard/scorecard.py script      # the exact SQL a run would execute
```

## What it does to the database

Nothing. Each metric runs in its own session, which is set to UTC, limited to
60 seconds per statement, and opened as a read-only transaction. A metric file
that contains anything other than one `SELECT` is refused before it is sent.

The heaviest queries read every first-hand evidence row and its report. That
is tens of thousands of rows today. If a metric starts to report
`unavailable: timed out`, it needs a tighter bound; nothing else is affected.

## Reading the output

```
[chain]
  chain_1_received   412
  chain_2_notified   3.2 per 100 (13 of 412)
  chain_3_seen       unavailable: table notification_messages missing
```

- A count is a number. A share is shown per hundred with its two counts. A
  median is shown with how many items it was taken over.
- `none to measure (0 of 0)` means the question does not apply yet. It is not
  a zero.
- `unavailable: …` means the metric could not be measured, with a short fixed
  reason. Text from the database server is never copied into the output,
  because the ledger is public.
- The run exits with status 2, and writes nothing to the ledger, if no metric
  at all could be measured.

Time windows, relative to the moment of measurement:

| Name | Span | Used for |
| --- | --- | --- |
| last 30 days | the 30 days before now | volumes |
| matured | from 37 days ago to 7 days ago | anything that needs time for an answer to arrive |

## Add or change a metric

1. Add `metrics/<id>.sql` with the six header lines (`group`, `title`, `kind`,
   `window`, `better`, `definition`) and one `SELECT`. `kind` is `count` (one
   number), `ratio` (numerator, denominator) or `median` (median, how many).
2. Add rows to `testdata/rows.sql` that exercise it and its expected value to
   `EXPECTED` in `test_scorecard.py`.
3. Add it to the table in `docs/wiki/11-scorecard.md` (`catalogue` prints the
   row). A test fails if the wiki does not list every metric.

Changing what an existing metric counts breaks comparison with every earlier
ledger row. Add a new metric with a new name instead, and say in the wiki that
the old one is retired.

## Test it

```bash
cd tools/scorecard
python3 -m unittest -v
SCORECARD_TEST_MYSQL='mysql -uroot --socket=/path/mysql.sock' python3 -m unittest -v
```

The second form also runs every metric on a MySQL 8 server. It builds a
throwaway database from the table definitions in this repository (`db/init.sql`,
the Go migration strings, the EPC schema), loads `testdata/rows.sql` and checks
each value. It proves that a metric cannot write, and that a missing table
costs only the metrics that need it. CI runs it in the `evidence_mysql` job.
