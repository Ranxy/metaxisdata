# Data-quality end-to-end fixture (MySQL + PostgreSQL)

A reproducible fixture for Metaxisdata's **Data Quality** subsystem, in the same
shape as [test/e2e_etl](../e2e_etl/README.md): deterministic seed SQL, an install
script that resets and deploys it, a driver that runs the workload, and a SQL file
that prints the evidence.

What it proves, against a **running server** and two **real engines**:

- the 20 rule types compile and execute for real, with the exact six-state
  verdicts, failed/total rows, observed values, samples and executed SQL;
- problems and notifications: FAIL opens one issue and one in-app message,
  repairing the data resolves it `AUTO_RECOVERED`, `ERROR(NO_READ_ONLY_SOURCE)`
  opens no issue and pages the administrators;
- the outbound webhook path: a failing check is delivered to a real HTTP endpoint
  (and not to a channel that only subscribes to the weekly digest);
- the scheduled tick: a due target is planned and executed by the runner, with
  the schedule advancing afterwards;
- the single-target HTML report, and its redacted audit row;
- the read models: target directory, target workspace, rule history, workspace
  overview, coverage and score;
- PostgreSQL-only semantics: `percent` sampling, `PERCENTILE`, and a
  cross-database reconciliation that needs a second statement.

The machine gate is the driver's exit code; `verify_quality.sql` is the
human-readable evidence. This is deliberately *not* a CI test: like e2e_etl it
needs a deployed server and the local containers, and it is the acceptance
walkthrough a release is checked with. The hermetic counterpart is
`make test-integration-data-quality` (real server + testcontainers) in
`backend/test/integration`.

## Prerequisites

The same local environment `test/e2e_etl` documents:

- a Metaxisdata **server** on `http://localhost:8083` using the local metadata
  PostgreSQL (`metaxisdata`, user `dev`/`dev`), started for example with

  ```bash
  PG_URL='postgres://dev:dev@localhost:5432/metaxisdata?sslmode=disable' go run ./backend/bin/server/main.go --debug
  ```

- the **mysql8-db-1** container (127.0.0.1:4417, root password `rootadmin`) and
  the **pg16-db-1** container (127.0.0.1:5432) — the ones the ETL fixture uses;
- instances **mysql-dev-1** and **test-pg-1** already registered (as
  `test/e2e_etl` does). Both need a READ_ONLY data source: `install.sh` creates
  or updates the MySQL one for the fixture's SELECT-only user and creates the
  PostgreSQL one only when missing, so a pre-existing PostgreSQL source must be
  able to read `e2e_dq` / `e2e_dq_ref` (the `dev` login the ETL fixture leaves
  can);
- a workspace **administrator** e-mail and password (rules create, settings and
  instance data sources are administrator operations);
- `python3` (3.10+), `psql`, `docker` and `curl` on the machine that runs the
  scripts.

The server and the driver must share the loopback interface: S10 starts a local
HTTP receiver and configures the webhook endpoint as `http://127.0.0.1:<port>`,
so a server in a container or on another host cannot reach it (skip that section
or run the server on the host in that case).

The fixture assumes the workspace's default DQ settings: failure samples stored
(the sample assertions need them) and the default schedule interval (S2 asserts a
valid schedule, not one specific hour). The webhook probe channels set
`bypass_quiet_hours` so a quiet-hours window cannot delay S10.

No Airflow and no DataX are needed. The fixture creates one extra instance it
owns, `dq-noread`, used only to prove the "missing READ_ONLY source" path without
touching the instances the ETL fixture uses.

## Layout

| Path | What it is |
| --- | --- |
| `install.sh` | reset + deploy: recreate the databases, ensure data sources, full sync |
| `run_quality.sh` | `install.sh` (unless `--no-install`) then the driver |
| `tools/dq_driver.py` | single-file Python 3 driver: ConnectRPC over JSON, assertions, exit code |
| `mysql/10_schema.sql`, `mysql/20_seed.sql` | the deterministic defect dataset |
| `mysql/30_row_count_change_growth.sql`, `40_row_count_change_revert.sql` | grow/shrink the row count around the baseline rule |
| `mysql/50_repair.sql` | fix every seeded defect |
| `mysql/60_schema_drift_alter.sql`, `61_schema_drift_revert.sql` | add/remove a column behind a `schema_drift` rule |
| `mysql/70_orphan_drop.sql` | drop the table behind an enabled rule |
| `mysql/80_segment_lose_region.sql`, `81_segment_restore_region.sql` | lose/restore a segment |
| `pg/10_schema.sql`, `20_seed.sql` | the PostgreSQL fixture |
| `pg/30_ref_schema.sql`, `40_ref_seed.sql` | the reference database for cross-database reconciliation |
| `rules/mysql.json`, `rules/postgres.json` | the rule suite and the expected verdict matrix |
| `verify_quality.sql` | 10 sections of `psql` evidence; section 10 must be all zeros |
| `README.md` | this file |

## Run

```bash
cd test/e2e_quality
export METAXISDATA_EMAIL=admin@example.com METAXISDATA_PASSWORD=…   # never committed
./install.sh          # reset the fixtures and full-sync; prints the resolved GUIDs
./run_quality.sh      # install + run + assert (a few minutes)
PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_quality.sql
```

`run_quality.sh --no-install` runs the assertions against the state the last
`install.sh` created; that is useful while changing the driver but not
repeatable, because the run itself repairs the data. **Re-running the full
`./run_quality.sh` is safe and is the acceptance criterion**: it resets data,
archives the previous run's rules, closes leftover issues and re-creates the
suite, so the notification/issue counts are about this run only.

The driver prints `ok`/`FAIL` per expectation, keeps going through the remaining
sections, and exits non-zero when any expectation failed. Run it in a throwaway
workspace: a real deployment pages whoever subscribes to DATA_QUALITY events
(in-app, webhook, e-mail), and the fixture's repair notifications are no
exception. `DQ_EXECUTION_TIMEOUT` (default 90 s) bounds each execution poll
and `DQ_REGISTRY_TIMEOUT` (default 240 s) bounds the registry wait after a sync.

### Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `METAXISDATA_URL` | `http://localhost:8083` | the server |
| `METAXISDATA_EMAIL`, `METAXISDATA_PASSWORD` | required | a workspace administrator (never committed) |
| `MYSQL_CONTAINER` | `mysql8-db-1` | the MySQL container used to seed/mutate data |
| `MYSQL_ROOT_PASSWORD` | `rootadmin` | its root password (the ETL fixture documents the same default) |
| `PG_CONTAINER` | `pg16-db-1` | the PostgreSQL container, used only to `CREATE DATABASE` |
| `PG_HOST`/`PG_PORT`/`PG_USER`/`PG_PASSWORD` | `localhost`/`5432`/`dev`/`dev` | the PostgreSQL fixture and the metadata DB |
| `DQ_MYSQL_INSTANCE`/`DQ_PG_INSTANCE`/`DQ_NOREAD_INSTANCE` | `mysql-dev-1`/`test-pg-1`/`dq-noread` | the registered instance IDs |
| `DQ_MYSQL_HOST`/`DQ_MYSQL_PORT` | `127.0.0.1`/`4417` | the address the server's READ_ONLY source uses |
| `DQ_METADATA_DB` | `metaxisdata` | the metadata database the S9 seam updates |
| `DQ_PG_READER_USER`/`DQ_PG_READER_PASSWORD` | `dev`/`dev` | credentials used when `install.sh` has to create the PostgreSQL READ_ONLY source |
| `DQ_EXECUTION_TIMEOUT` | `90` | seconds to wait for one execution to finish |
| `DQ_REGISTRY_TIMEOUT` | `240` | seconds to wait for the registry after a sync |

## Scenarios

| Section | What it does | What it asserts |
| --- | --- | --- |
| S0 | create the suite | every rule is accepted; the fixture owns exactly its 22 MySQL rules |
| S1 | save-time gates (no execution) | mismatched config arm, range on a view, unknown GUID, stale revision (`aborted`), PERCENTILE on MySQL, range on `jsonb` are all rejected; archiving keeps history readable |
| S2 | verdict matrix on `dq_orders` + helper tables | the exact matrix below, plus each run creating one batch for its single target, one issue and one `ISSUE_OPENED` per FAIL, failure/passing evidence, a valid target schedule |
| S3 | `row_count_change` | first run records the baseline; +50 % fails a 20 % bound; an unchanged count passes against the new baseline |
| S4 | `segment_check` | first run stores the observed segments; losing one FAILs with one disappeared segment; restoring it recovers |
| S5 | repair | every repaired rule passes, every issue is `RESOLVED(AUTO_RECOVERED)`, the overview/directory/workspace/trend read models agree |
| S6 | `schema_drift` | passes, an added column FAILs with one drift item, `target_schema_changed` lights up, `Acknowledge` clears it, reverting the DDL recovers |
| S7 | orphan | after the table is dropped and the registry is refreshed the run is `SKIPPED(ORPHANED)`, not `ERROR`, and opens no issue |
| S8 | missing READ_ONLY source | creating a query rule is `failed_precondition`; running one is `ERROR(NO_READ_ONLY_SOURCE)`; no issue; `EXECUTION_FAILED` reaches the administrators; restoring the source passes |
| S9 | scheduled tick | the target's schedule starts at 1 h; with the one metadata-DB seam (`next_run_at` moved into the past) the runner executes it as `SCHEDULED` with no initiator, and the schedule advances |
| S10 | outbound webhook | a real local HTTP endpoint receives the FAIL event; a channel subscribed only to the digest receives nothing |
| S11 | report | the HTML is self-contained, names the target, has no script tag; the audit row exists with the body redacted |
| S12 | PostgreSQL | the sampled range is `sampled` with a non-empty sample and records `"`+`$` SQL; `PERCENTILE` passes; a cross-database reconciliation records its reference statement; repair recovers |

### The verdict matrix

Every row is a rule in `rules/mysql.json` unless marked PG. `failed/total` is the
evidence the driver asserts; where a row lists a value the driver checks that
field, and where it shows a verdict sequence the sequence is asserted by the
scenario function (the counts behind PASS/`observed` are shown for the reviewer,
not all of them are re-asserted).

| Rule type | Fixture | Verdict | failed/total |
| --- | --- | --- | --- |
| `row_count` | `dq_orders` = 10 rows | PASS | 0/10 |
| `row_count_change` | +50 % against a 20 % bound | PASS → PASS → FAIL → PASS | — |
| `null_percent` | 2 NULL e-mails, warn 1 / max 3 | WARN | 2/10 |
| `unique` | `external_ref` `R-4` twice | FAIL | 1/10 |
| `unique_combination` | `(customer_id, product_code)` duplicate | FAIL | 1/10 |
| `accepted_values` | `status = 'cancelled'` | FAIL | 1/10 |
| `range` | `amount = -5.00`, bounds 0..1000 | FAIL | 1/10 |
| `regex` | `product_code = 'AB'` | FAIL | 1/10 |
| `string_length` | `product_code = 'AB'`, min 3 | FAIL | 1/10 |
| `referential_integrity` | `customer_id = 999` has no parent | FAIL | 1/10 |
| `cross_column` | `shipped_quantity 7 > quantity 6` | FAIL | 1/10 |
| `custom_sql` | `amount < 0` returns one row | FAIL | observed 1 |
| `cel_expression` | `amount >= 0.0` fails on -5 | FAIL | 1/10 |
| `schema_drift` | one column added after the snapshot | PASS → FAIL → PASS | 1/2 drift items |
| `group_check` | bucket `a` has 3 rows, max 2 | FAIL | 1/3 groups |
| `aggregate` | `AVG(amount)` between 0 and 100 | PASS | observed 50.5 |
| `cross_table` | `dq_orders` vs `dq_orders_rollup` counts | PASS | observed 0 |
| `standard_code` | one invalid USCC | FAIL | 1/10 |
| `segment_check` | `east` disappears, then returns | NOT_EVALUATED → FAIL → PASS | 1/3 expected |
| `freshness` (`dq_heartbeat`) | every timestamp is from 2020 | FAIL | — |
| `aggregate` (`dq_empty`, six-state) | SUM over an empty table | NOT_EVALUATED | — |
| `row_count` (`dq_orphan_target`) | table dropped under the rule | SKIPPED(ORPHANED) | — |

PostgreSQL: `percent`-sampled `range` on `dq_big` (all 5000 rows out of bounds →
FAIL, then repaired to PASS), `aggregate PERCENTILE` (PASS, 30), `null_percent`
(FAIL 2/5, then repaired), cross-database `cross_table` (PASS, one statement per
side). The driver also asserts the two engine gates above (PERCENTILE refused on
MySQL, range refused on `jsonb`).

## Evidence (`verify_quality.sql`)

The SQL hardcodes the fixture instance IDs and database names; overriding
`DQ_MYSQL_INSTANCE`/`DQ_PG_INSTANCE` makes the evidence empty (the gates then
look green). Keep the defaults when reading the evidence.

Sections 1-9 print the current state: rules per target, latest verdict per rule,
the type × status matrix, sample evidence, issues and resolution reasons,
notifications by kind, dispatch batches, target schedules, and the audit rows of
the fixture's writes (the report body must read `[REDACTED]`).

Section 10 is the gate. Every `must_be_zero` must be 0:

| Gate | Meaning |
| --- | --- |
| latest verdict is FAIL | after S5/S6/S12 every fixture check ends PASS/WARN/NOT_EVALUATED |
| latest verdict is ERROR | a check that could not run must not be left behind |
| open issue on a live fixture rule | every opened issue was recovered |
| storm summary instead of per-rule messages | the batches stay under the 10-FAIL storm threshold |
| abandoned dispatch batch | no batch was lost |
| enabled checks but no next run | every live target has a schedule |
| rule with more than one unresolved issue | the per-rule partial unique index holds |
| report audit row with an unredacted body | report bodies never enter the permanent ledger |
| fewer than 20 distinct rule types | the matrix covers all 20 types |

## Seams and limitations

Two deliberate seams, both documented because a reviewer should know them:

1. the fixture edits the **target database directly** (MySQL/PostgreSQL) to create
   and repair defects — that is what makes the verdicts exact;
2. exactly one metadata-DB write moves `quality_target_schedule.next_run_at` into
   the past (S9). No API can make a target due, and the minimum interval is 15
   minutes; the Go integration journey uses the same seam.

Not covered, on purpose:

- the LLM paths (rule drafts, failure explanations, labels) and the agent
  session / auto-enrollment surfaces — they need a model provider and are not
  deterministic;
- CSV streaming exports (Connect server-streaming framing); the unary HTML report
  is covered instead;
- MSSQL / StarRocks / Doris. They are the one real-engine gap the existing
  integration tests also have; extending this fixture with an engine directory and
  a rule file is the intended next step (the `e2e_etl` containers are already
  registered).

The driver talks to the server over the same ConnectRPC JSON endpoint the SPA
uses (`POST /metaxisdata.v1.<Service>/<Method>`), and reads `quality_*` tables
only from `verify_quality.sql`.
