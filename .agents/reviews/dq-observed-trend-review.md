# P1-1 (observed trend + history threshold suggestion) and §6.1 baseline repair — adversarial review

Reviewer: an independent adversarial review ran from a fresh context, read-only, on
branch `feat-dq-observed-trend` (head `ca377fb`, base `6ecf8a2`) in
`.agents/worktrees/feat-dq-observed-trend`. The whole diff was read
(`git diff 6ecf8a2..HEAD`: 54 files, +8386/−997; 9 of them are buf output that was
regenerated and checked for idempotency, leaving 45 hand-written files, +5451/−85),
together with the P1-1 row and §6.1 of
[`data-quality-competitive-gap-analysis.md`](data-quality-competitive-gap-analysis.md),
D-80/D-81/D-82/I-47/I-48, §3.6/§3.12/§6/§7/§8 of
[`.agents/docs/data-quality.md`](../docs/data-quality.md), `docs/data-quality.md`,
the i18n glossary, and the nested `AGENTS.md` files. Existing decisions (①②③ +
§6.1 only; no rolling-median baseline; no aggregate/`row_count_change` suggestion;
no CEL sampling change) were not re-litigated; where code or docs contradicted a
decision, that is reported as a defect.

## What was exercised

- `git diff 6ecf8a2..HEAD` read in full; `git status --porcelain` clean before and
  after (the frontend gates run with `--fix` wrote nothing).
- `go build ./...`; `go test ./backend/... -count=1` (all packages pass);
  `gofmt -l backend/` clean;
  `golangci-lint run --allow-parallel-runners --max-issues-per-linter 0` reports 3
  `revive multiline-if-init` issues, all in `backend/plugin/db/starrocks/sync.go`,
  which this branch does not touch (release-bump drift, not a PR finding).
- `buf format -d proto`, `buf lint proto` clean; `cd proto && buf generate` then
  `git status --porcelain` empty (generated code is in sync).
- Frontend: `pnpm biome:check`, `pnpm lint`, `pnpm i18n` (112 terms / 95 rules),
  `pnpm type-check`, `pnpm test run` — 169 files / 1751 tests pass.
- `go test -tags integration -run '^TestDataQualityJourneyMySQLRealServerIntegration$'
  -count=1 -timeout 40m ./backend/test/integration/runner` against real PostgreSQL +
  MySQL (Docker): pass in 35.5 s, including `assertJourneyTrend` and
  `assertJourneyDefaultOnEmptyBaseline`.
- External probes with `go test -overlay` (test file under `/tmp`, never in the
  repository): suggestion→`ValidateConfig` for every row-count shape × four
  histories (min-only, max-only, min+max, all-warn; data 0/1/1e6); a type change
  away and back (A→B→A); a PERCENT point with `total_rows=0`; a flat series
  (warn==fail); `cross_table` tolerance subtraction; COUNT→PERCENT metric change.
- Not run: live-browser UI audit (`pnpm audit:ui`; no CDP endpoint here) — no
  layout budget was changed.

## Findings and closure

Every finding was closed in `22bcb0b`; the post-fix verification below was run by the implementing side.

| # | Severity | Finding (file:line) | Proposed fix | Status |
| --- | --- | --- | --- | --- |
| 1 | major | The baseline predicate is documented as "COMPLETED **and** `observed_value` non-null **and** `total_rows > 0`" (`.agents/docs/data-quality.md:116` D-82, `:203` §3.6, `:618` §7 #6). A `NOT_EVALUATED` row has `observed_value = NULL` **by construction** (`backend/runner/qualityrunner/execution_result.go:205-221`), so under the documented rule no `NOT_EVALUATED` row could ever be a baseline — the whole §6.1 repair is the `total_rows` fallback that the docs' predicate excludes. The shipped code requires only `total_rows > 0` and prefers `observed_value` when present (`backend/runner/qualityrunner/plan.go:164-183`), which is what D-82's rationale and §7 #20 describe. | Reword the three places to "most recent COMPLETED execution with `total_rows > 0`, using `observed_value` when it stored one and `total_rows` otherwise (any verdict)". §7 #20 (`:632`) already has the right wording; make the other three match it. | Fixed in `22bcb0b` |
| 2 | major | Applying a suggestion leaves a **stale warning bound** when the suggestion omits `warn` (the documented `warn == fail ⇒ no warning zone` case, `backend/component/dataquality/trend.go:347-351`): `applyThresholdSuggestion`'s `setSingleBounds` only writes `warnThreshold` when it is present and otherwise keeps the form's old value (`frontend/src/utils/dataQualityForm.ts:1387-1390`; same pattern for `NULL_PERCENT` and `FRESHNESS`, lines 1399-1415). Concrete case: a unique check saved with `fail=10, warn=5`, whose five recent judged runs all measure 0 failing rows, suggests `fail=0, warn omitted`; the form becomes `fail=0, warn=5` and Save is rejected by `validateCountThresholds` ("warning threshold 5 exceeds failure threshold 0", `backend/component/dataquality/validate.go:982-983`); freshness has the same strict rule (`:900-907`) and the client `validateRuleForm` does not check warn/fail order. | The suggestion is authoritative for the fields it covers: clear the warn field (`""`) when `thresholds.warnValue`/`warnValueUpper` is absent, instead of keeping the old value. Add a frontend test for "flat history + pre-existing warn bound". | Fixed in `22bcb0b` |
| 3 | minor | `history_restarted` does not mean "the drawn history starts at a type change" when the rule type changed away and back: the loop `continue`s past the incompatible revision and keeps older same-type points (`backend/component/dataquality/trend.go:238-245`). Overlay probe: revisions A(rev1), B(rev2), A(rev3) → `points=[exec1, exec3]`, `HistoryRestarted=true`, so the line connects across the excluded run. This contradicts D-80 "类型变更前的历史不可比" plus the proto comment (`proto/v1/v1/data_quality_service.proto:1922-1925`), `docs/data-quality.md:234-236` ("The series stops at a rule-type change"), the i18n copy (`frontend/src/locales/en-US.json:1413`, `zh-CN.json:1413`), and §3.12/§7 #29. §7 #29's "the band hides when the most recent judged execution is not the current revision" is likewise imprecise — the band uses the newest *drawn* point (`trend.go:276-291`). | Preferred: `break` at the first known incompatible revision so the series really is truncated at the type change (one-line change; keeps every doc true). Otherwise reword all five places to "executions judged by another check type are left out" and fix §7 #29's band sentence. | Fixed in `22bcb0b` |
| 4 | minor | D-80 claims "规则改 filter/阈值不截断序列（点仍可比）" (`.agents/docs/data-quality.md:114`; also §3.12 `:407`, §7 #29 `:641`). Threshold edits keep the measurement comparable, but a **filter edit changes the row population and the `failed_rows`/`total_rows` denominator**, so pre- and post-filter points are not the same quantity — and the p95/p99 suggestion mixes both regimes. The implementation follows the "do not truncate" decision; only the comparability claim is wrong and the user doc is silent about it. | Keep the behaviour, drop the "点仍可比" claim and record the filter caveat (trend points and suggestion sample may span a filter change) in D-80/§7 #29 and the operator doc; or extend the revision comparison to the filter so filter edits restart the series. | Fixed in `22bcb0b` |
| 5 | nit | The maintenance reference names the new store method `ListQualityRuleRevisionsByNumber` (`.agents/docs/data-quality.md:401` §3.12, `:654` §8), while the shipped method is `ListQualityRuleRevisionsByRevision` (`backend/store/quality_rule.go:958`) and the SQL const/test still say "ByNumbers". | Pick one name; the docs are the easiest to fix. | Fixed in `22bcb0b` |
| 6 | nit | The `baseline_value` proto comment says a first, baseline-less observation "is then NOT_EVALUATED, driven by `on_empty`" (`proto/v1/v1/data_quality_service.proto:1287-1290`). A **newly created** check now defaults `on_empty=PASS` (D-82), so its first run is `PASS` with `baseline_value` unset; only a rule whose `on_empty` is NOT_EVALUATED gets that verdict. | Say "the verdict is then whatever `on_empty` decides (`NOT_EVALUATED` by default)". | Fixed in `22bcb0b` |
| 7 | nit | The chart's legacy-snapshot fallback (comparison `UNSPECIFIED`) maps `fail_value`/`warn_value` to **lower** bounds, while its own comment says "An upper pair is the shape a count rule writes, so it is the safe read" (`frontend/src/utils/dataQualityTrend.ts:168-175`; its test pins the lower-bound reading, `dataQualityTrend.test.ts:122-143`). If ever reached, a count rule's upper bound would be shaded as a failing region *below* it. `QualityThresholds.comparison` shipped with the data model (`6f0aace`), so no stored snapshot should be comparison-less. | Read the legacy pair as upper bounds (match the comment) or drop the dead branch; fix the test with it. | Fixed in `22bcb0b` |
| 8 | nit | In the integration journey, the pre-existing doc comment for `assertJourneyMissingReadOnlySource` was left above the new `assertJourneyDefaultOnEmptyBaseline`, so the new helper carries the wrong doc comment and the original function lost its comment (`backend/test/integration/runner/data_quality_journey_service_test.go:1196-1204` and `:1237-1240`). | Delete the three stale lines at `:1196-1198`. | Fixed in `22bcb0b` |
| 9 | nit | Two doc/precision gaps: D-80 says each point carries "六态" (`.agents/docs/data-quality.md:114`), but only `PASS`/`WARN`/`FAIL` are ever points; and the operator paragraph says suggestions exist "for a count or freshness check" (`docs/data-quality.md:87-96`) although a `cross_table` check (NUMBER unit) also gets the p95/p99 suggestion. | Say "three verdicts" and mention the table-to-table difference explicitly, or say "for the checks whose value bounds the failure count (and freshness lag)". | Fixed in `22bcb0b` |

## Post-fix verification

All nine findings were fixed in `22bcb0b` on top of `ca377fb`:

- **1, 4, 5, 6, 9 (docs/proto words):** the baseline predicate now states `total_rows > 0`
  with `observed_value` preferred and `total_rows` as the fallback; the trend section
  says the series truncates at the first incompatible revision and records the filter
  caveat; the store method is named `ListQualityRuleRevisionsByRevision`; the
  `baseline_value` comment says the first verdict follows `on_empty`; the suggestion
  paragraph covers the table-to-table difference and the operator page warns about a
  filter change.
- **2 (stale warning bound):** `applyThresholdSuggestion` now clears the warning field
  when the suggestion omits it, for the shared pair, `null_percent` (count and percent),
  freshness and each row-count side; new tests cover a flat history with an existing
  `warn=5`.
- **3 (type change away and back):** `BuildTrend` `break`s at the first revision with
  another rule type, so A→B→A draws only the newest A run; a unit test pins it.
- **7 (legacy band):** a comparison-less snapshot's lone bound is read as the upper
  bound, and the legacy test pins that; the range pair keeps both sides.
- **8 (integration comment):** the stale doc comment above the new helper was removed.

Re-run by the implementing side, on the post-fix tree:

- `gofmt -l backend/` empty; `go build ./...`; `go test ./backend/... -count=1` all pass;
  `go test ./backend/component/dataquality ./backend/api/v1 ./backend/store
  ./backend/runner/qualityrunner` 4 packages pass.
- `buf format -d`/`buf lint` clean; `cd proto && buf generate` idempotent (the regenerated
  comment-only change is in the fix commit).
- `golangci-lint run --allow-parallel-runners --max-issues-per-linter 0`: 173 repo-wide
  `revive multiline-if-init` issues (the same count as `main`, lint-version drift), **zero
  on changed lines**.
- Frontend `pnpm biome:check` (one formatting fix), `pnpm lint`, `pnpm i18n`
  (112 terms / 95 rules), `pnpm type-check`, `pnpm test run`: 169 files / 1755 tests pass.
- Docker integration journey
  `go test -tags integration -run '^TestDataQualityJourneyMySQLRealServerIntegration$'
  ./backend/test/integration/runner`: pass in 36.3 s (post-fix tree).

## Verified good

- **Suggestion families match the enum's percent capability.** A probe crossed
  `rule_type_spec.go`'s `Percent` flag with its `Trend` shape: exactly the ten
  `Percent: true` types use `trendPercentCapableShape`, and every other numeric
  type has its own unit shape — so a PERCENT history can never be plotted as a raw
  count (and vice versa). `schema_drift` has no shape, enforced by the spec test.
- **Unit normalization.** PERCENT is `failed_rows/total_rows·100` and is never
  multiplied again (frontend `formatTrendValue` test); `total_rows=0` PERCENT points
  are dropped (probe); `null_percent` switches count/pct on its own config, not the
  shared metric; `cross_table` subtracts the **execution revision's** tolerance
  (probe: 20−4=16, 20−10=10, inside tolerance=0); `row_count_change` plots the row
  count and refuses a band; `freshness` plots seconds. The `aggregate` and CEL units are
  right for the stored observation.
- **Suggestion arithmetic.** ≥5 kept points, linear-interpolation p95/p99, round up
  (whole rows/seconds, 2 decimals for percent/number), `warn == fail` omits warn
  (probe: flat 5 → `fail=5`, `warn=nil`), row count only fills configured bounds
  (`floor(min×0.9)`, `ceil(max×1.1)`, warn at the observed extremes; probe:
  suggestion→config passes `ValidateConfig` for 16 config/history combinations),
  `exact_rows` is `UNSUPPORTED_CONFIG`, `aggregate`/`row_count_change`/`schema_drift`
  have no suggestion (and `row_count_change` reports `UNIT_NOT_COMPARABLE`).
- **Band and window rules.** Band only from the newest drawn point at the rule's
  current revision; `EDITED_SINCE_LAST_RUN`/`NO_JUDGED_EXECUTION`/
  `UNIT_NOT_COMPARABLE` are each reachable and mapped; empty history is not an
  error; points are oldest first; unknown revisions are kept; limit defaults to 30
  and clamps to 90 (unit-tested).
- **§6.1 baseline repair works end to end.** A `NOT_EVALUATED` execution stores
  `observed_value=NULL` with `total_rows` set (`execution_result.go:205-221`); the
  picker prefers `observed_value` but falls back to `total_rows`, skips 0-row rows,
  applies the unchanged ≤7d/≤2×interval window and the new current-target predicate,
  and records value+time on the execution for any status. The integration journey
  proves it: a default-`NOT_EVALUATED` check is `NOT_EVALUATED` on its first run with
  `total_rows=10` and no baseline, and is judged on the second run with
  `baseline_value=10`.
- **The two `nil`-target callers are unchanged.** `ListRecentQualityExecutions(rule,
  nil, n)` renders exactly the previous statement (no target predicate), so the L2
  explanation (`data_quality_llm_analysis.go:362`) and the issue-migration previous
  status (`issue.go:145`) keep their behaviour; only the row_count_change baseline
  passes a target.
- **Store and migration plumbing.** `qualityExecutionColumns`/`scanQualityExecution`
  are 31 columns in the same order (pinned by a scan stub), `FinishQualityExecution`
  binds `$24/$25` to `baseline_value`/`baseline_time` with matching args, migration
  `0036` is idempotent (`ADD COLUMN IF NOT EXISTS`) and identical to the two columns
  appended in `LATEST.sql`; the new reads use the existing
  `idx_quality_execution_rule_time` index and add no table/index; `pq.Array([]int32)`
  is a supported lib/pq type for `revision = ANY($2::int[])`.
- **Read-only surface.** `GetQualityRuleTrend` is annotated with
  `metaxisdata.dataQuality.executions.list`, has no audit option, writes nothing,
  touches no target database, needs no connection budget, and costs two indexed reads
  (executions + revisions). The proto comments match the implementation for the
  limit clamp, point order, unit enum and band rule.
- **Frontend wiring.** The drawer reads the trend in parallel with the recent-execution
  table and a failed trend read leaves the rest of the sheet intact with its own retry
  (component test); the chart's y domain always includes the band (test), width is the
  measured container width with a 1:1 viewBox, points are keyboard-focusable and the
  series is mirrored as an `sr-only` table; sampled points are hollow and labelled;
  `unitChanged` catches a type change, the shared metric and `null_percent`'s own
  metric; applying a suggestion only fills the form (test asserts no update call);
  `on_empty=PASS` is applied to a new count-change check only and an edit keeps the
  saved behaviour (test); the baseline is shown in the drawer and the execution detail
  with a "no baseline yet" fallback.
- **Docs whose claims were verified:** §7 #20's "已修复" is real (integration test);
  the operator page's row-count-change/trend/troubleshooting paragraphs match behaviour;
  the glossary's `trend`/`baseline` rows are what the generated table and `pnpm i18n`
  expect (no forbidden `走势` in the locales); the RPC count in §3.6 is 48 as shipped.

## Residual risks (documented, not blocking)

- The baseline read is scoped by target but **not by rule revision/type**. A rule
  changed from another type to `row_count_change` on the same target can still pick
  an older execution of the previous type as its denominator; because
  `observed_value` is preferred over `total_rows`, a previously judged `unique` run
  would contribute its *failed-row* count as the row-count denominator (pre-existing
  exposure, but the new "any verdict" rule widens the eligible set). Scoping the
  baseline to revisions whose type is `ROW_COUNT_CHANGE` would close it.
- `baseline_time` is the baseline execution's `started_at`, not the exact moment its
  `COUNT(*)` ran; it is an approximation inside one execution.
- The trend query trusts the writer invariant that non-judged verdicts have a NULL
  `observed_value` (it filters only `observed_value IS NOT NULL`). A future writer or
  a pre-release development row that stored a value for `ERROR`/`NOT_EVALUATED` would
  be plotted with a status the chart has no colour for.
- The incremental `0036` upgrade path was not exercised: the integration harness
  migrates a fresh database from `LATEST.sql`, so only the fresh-install DDL is
  covered (the incremental is a trivial idempotent `ALTER`).
- `baselineCandidateLimit = 10`: a table that records ten consecutive zero-row
  executions and then refills has no baseline left even though an older usable one
  exists inside the seven-day window.
- Suggestions (and the band's drawn points) span filter/definition edits by design;
  see finding 4 for the documentation caveat.
- The chart's 1:1 width depends on `ResizeObserver`/`clientWidth`; the jsdom tests
  cover geometry but not the measured-width path (the fallback 640 is used in tests).

## Verdict

`fix-then-ship`. Two majors: the D-82/§3.6/§7 #6 baseline description contradicts the
shipped `total_rows` fallback (a documentation edit), and "use suggested thresholds"
can produce a form the server rejects when the suggestion legitimately omits the warn
bound (a small, well-scoped frontend fix plus a test). The two minors and five nits
are documentation/precision or dead-branch issues. Nothing in the trend read, the
baseline fix, the migration, the permission model or the chart is wrong in a way that
blocks the feature, and every quality gate plus the real-server journey passes.
