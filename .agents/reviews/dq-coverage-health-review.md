# P0-3 (coverage, quality score, recent pass rate) — adversarial review

Reviewer: independent adversarial review of commit `04b18b7`
(`feat(data-quality): add coverage, quality score and recent pass rate`) in
`/home/ran/gocode/metaxisdata/.agents/worktrees/feat-dq-coverage-health`
(branch `feat-dq-coverage-health`). Base: `88aa197`. Reviewed the whole 39-file
diff `git diff 88aa197..HEAD`, the P0-3 section of
`.agents/reviews/data-quality-competitive-gap-analysis.md`, `backend/AGENTS.md`,
`proto/AGENTS.md`, `frontend/AGENTS.md`, both data-quality docs, the existing
DQ reviews (for format) and the accepted product decisions listed on the shared
task. Existing decisions are not re-litigated; where the code or docs contradict
them, that is reported as a defect.

## What was exercised

- `git diff 88aa197..HEAD` — all 39 files read. `git status --porcelain` was clean
  before the review; the only change this review introduces is this document.
- `go build ./...` — pass.
- `go test ./backend/component/dataquality/... ./backend/store/...
  ./backend/api/v1/... ./backend/component/qualityreport/...` — pass.
- `go test -tags=integration ./backend/test/integration/runner -run TestDataQuality
  -count=1` — pass in 43 s against real PostgreSQL + MySQL (testcontainers),
  including the new `assertJourneyTargetHealth` / `assertJourneyDeletedInstanceCoverage`
  assertions and the coverage/score assertions in `assertJourneyOverview`.
- `gofmt -l backend/` — clean.
- `golangci-lint run --new-from-rev=88aa197 --allow-parallel-runners ./backend/...`
  — 0 issues.
- `cd proto && buf generate` then `git status --porcelain` — no diff: the committed
  generated Go/frontend/grpc-doc output is current. `buf format -d` and `buf lint`
  — clean.
- `pnpm --dir frontend type-check` — pass.
- `pnpm --dir frontend test run` — 158 files / 1611 tests pass (the new overview,
  targets, target-workspace and `utils/dataQualityOverview` tests included).
- `pnpm --dir frontend biome:check` — clean (no fixes); `pnpm --dir frontend i18n`
  — all checks pass (missing/unused keys, sort, placeholder parity, glossary
  107 terms / 94 rules).
- Read the four `ruleTypeSpecs` consumers in
  `backend/component/dataquality/{rule_type_spec,score,evaluate,executor}.go`,
  the coverage/recent-execution SQL in `backend/store/quality_{overview,execution}.go`,
  the three assembly sites in `backend/api/v1/data_quality_{overview,target,report}.go`,
  the report renderer, the proto, both docs and the new tests.
- Not run: `pnpm audit:ui` (accepted: no live app/CDP in this environment; UI
  budgets unchanged).

## Blockers

None. The feature is correct on its accepted definitions for the state a normal
workspace is in: all 18 rule types are classified from the type catalog, the
weighted math and unset behaviour are right, the workspace/dimension/owner
aggregations and the per-target score/report agree with each other, the coverage
denominator/guard is the documented one, and the whole path is exercised
end-to-end against a real server. The two Majors below are an honesty bug in one
promised invariant and a scalability defect on the directory read path; neither
corrupts data or breaks an existing feature.

## Majors

**G-1 — Column coverage can exceed 100% after a column disappears, but the docs
and user docs promise it cannot.**
`backend/api/v1/data_quality_overview.go:189-192` counts a covered column for
every enabled rule whose target still has a registry row, using
`dataquality.ConfigColumn` on the rule config and never comparing it with the
target's *current* registry columns. The denominator
(`backend/store/quality_overview.go:59-72`) is the target's current column arrays,
and `ListExistingMetaRegistryTargets` (`quality_overview.go:84-89`) only proves the
target row exists. A rule's column is validated when the rule is saved
(`backend/component/dataquality/validate.go`), but schema sync rewrites the
registry column list when a column is dropped or renamed and the rule stays
ENABLED (its later executions become ERROR; nothing archives it, and
`target_orphan_reasons` only checks the object, not its columns). Example: a table
with five columns gets five column checks, three columns are dropped, the registry
now has two columns — `covered_column_count` stays 5, `total_column_count` is 2,
and the instance (and a small workspace) reports 250% coverage. The same
arithmetic feeds `coverage_by_instance`/`coverage_by_environment`
(`data_quality_overview.go:246-247`), so the breakdown shows it too.
This directly contradicts `.agents/docs/data-quality.md:109` (D-75
"覆盖率不会超过 100%"), `:334` ("因此分子不会超过分母"), `docs/data-quality.md:226`
("coverage can never exceed 100%"), and the maintenance doc's §3.10 bullet.
The target guard itself is correct; the missing step is column membership.
**Fix:** make the numerator honest rather than merely guarded. The existence read
already joins `meta_registry_resource`; extend it (or add a batch read) to return
the target's current column-name set (the union of the four metadata arrays, as
the denominator already computes them), and only count `(target_guid, column)`
when the column is in that set. Cheaper interim: clamp each target's covered
columns to its registry column count and reword the three docs; dropping the
≤100% claim without fixing the number leaves a misleading metric, so prefer the
intersection. While the guard is being touched, also add
`r.object_type = ANY($coverageObjectTypes)` to `qualityTargetExistenceQuery` so the
numerator and denominator predicates are literally the same set (latent today only
because rule targets are restricted to the four types by
`dataquality.metadataColumns`).

**G-2 — The directory's recent-executions read scans every completed execution per
target and materialises full sample payloads, to compute two status counts.**
`backend/store/quality_execution.go:788-807` ranks with
`ROW_NUMBER() … PARTITION BY e.target_guid, e.target_type` in a CTE and applies
`rn <= $5` in the outer query. A `WindowAgg` cannot stop after the twentieth row,
so every matching completed execution of every enabled rule on the page is read
before 20 are kept; on a mature target that is years of executions. The projection
is the full `qualityExecutionColumns` (`quality_execution.go:19`), including
`sample`, `executed_sql`, `thresholds` and `target_path`, and
`scanQualityExecution` (`quality_execution.go:238-252`) JSON-unmarshals three of
them for every row — while the only consumer, `qualityRecentPassRate`
(`backend/api/v1/data_quality_target.go:570-590`), reads `Status` alone. The
directory allows a page size of 1000 (`qualityListMaximum`,
`backend/api/v1/data_quality_service.go:45-46`), so one page load can fetch and
unmarshal up to 20k execution rows carrying failure samples. This is the one new
query on the hottest DQ page and the first thing that degrades at scale; the same
aggregate is recomputed when only the trend window changes, because the page calls
the whole `GetQualityOverview` for it (`DataQualityOverviewPage.test.ts:514`).
**Fix:** return counts, not rows. Keep the keys join and use a per-key
`LATERAL (SELECT e.status FROM quality_execution e JOIN quality_rule r … WHERE
e.target_guid = k.guid AND e.target_type = k.object_type AND e.state = $completed
ORDER BY e.started_at DESC, e.id DESC LIMIT $20)` — the existing
`idx_quality_execution_target_time` makes it 20 index rows per target — then
aggregate `COUNT(*) FILTER (WHERE status = PASS)` / judged in SQL, or at minimum
project only `target_guid, target_type, status`. Add a store/integration case with
25 executions on one target asserting the newest 20 are counted.

## Minors

**g-1 — The coverage numerator's guard and the denominator are two separate
statements, so a concurrent registry delete can still push table coverage past
100%.**
`backend/api/v1/data_quality_overview.go:66-74` runs
`ListExistingMetaRegistryTargets` first and `ListQualityCoverage` afterwards. The
registry rows are deleted by schema sync while a user can be reading the overview;
a row deleted between the two statements is absent from `total_table_count` but
still present in `monitored_table_count`/`judged_table_count` (and the instance
rows), which is the "can never exceed 100%" invariant again, plus
`docs/data-quality.md:278` claims the numbers "always match the executions you can
open". The window is milliseconds and today's integration suite cannot see it, but
the promise is absolute.
**Fix:** make the two reads one snapshot — run the new overview reads in a
read-only `REPEATABLE READ` transaction (the same recommendation as the digest
snapshot in `dq-reports-digest-review.md` g-4), or derive both numerator and
denominator from a single statement (rules joined to registry), or at least read
coverage first and existence second so a race can only under-count.

**g-2 — The target-row tooltip names the wrong window size.**
`frontend/src/pages/data-quality/DataQualityTargetsPage.vue:324-328` passes
`summary.recentExecutionCount` — the number of *judged* executions — into
`dataQuality.targets.scoreLineHint`, whose copy says "the last {count} completed
executions" (`frontend/src/locales/en-US.json:1347`; zh-CN `:1347`). The window is
the last 20 completed executions; when the window contains ERROR/SKIPPED/
NOT_EVALUATED, the judged count is smaller and the tooltip understates it (e.g.
"last 15 completed executions" when 20 were read). The API field is documented as
"how many were judged" (proto `QualityTargetSummary.recent_execution_count`), so
the server is fine; the copy is wrong.
**Fix:** pass the constant `20` (or the actual window constant) and, if the judged
count should stay visible, phrase it "of the last 20 completed executions, {count}
were judged". Keep the hint absent when `recentExecutionCount === 0`.

**g-3 — Retargeting a rule (supported by the update mask) attributes its old
executions to the old target's pass rate.**
`backend/store/quality_execution.go:797-799` joins `quality_rule` only on
`id`/state, partitions by the *execution's* `target_guid, target_type`, and the
window is therefore the target's history of all currently enabled rules. Rules can
be moved (`backend/api/v1/data_quality_convert.go:560-573` accepts `update_mask:
"target"` and sets `TargetMoved`; `syncQualityTargetScheduleAfterRuleUpdate`
reconciles both schedules). After moving a rule A→B, its pre-move executions still
carry target A, so A's `recent_pass_rate` keeps counting a check that no longer
watches A until those rows age out, while A's score (built from A's current rules)
already dropped it. The accepted decision ("last 20 COMPLETED executions after
filtering to currently enabled non-archived rules") does not cover retargeting.
**Fix:** add `AND e.target_guid = r.target_guid AND e.target_type = r.target_type`
to the join, so a moved rule's history leaves the old target's window (the
executions remain visible in the Executions list). Also decide and document
whether the score's `ListLatestQualityExecutions` should require the latest
execution's target to equal the rule's current target; today a moved rule's first
score for B can be an execution that ran against A, which the current docs do not
mention.

**g-4 — The two riskiest new SQL paths have no behavioural test.**
`backend/store/quality_execution_test.go:500-514` only string-matches
`qualityExecutionRecentByTargetsStatement`, and the integration journey seeds 17
executions (below the 20 limit) on TABLE targets only. Nothing exercises
(a) the `rn <= 20` cap, (b) the `viewMetadata` / `externalTableMetadata` /
`materializedViewMetadata` column arrays in `qualityCoverageQuery`, or
(c) the docs' "instance/environment rows add up to the workspace totals" identity
(the integration only asserts each row is `<=` the totals). These are exactly the
paths a future refactor can break silently.
**Fix:** add one integration case with 25 completed executions on one target
(assert `recent_execution_count == 20` and the pass share of the newest 20), one
VIEW (or external table) target in the journey seed whose columns are asserted
`>= 1` and consistent with `target_columns`, and assert the per-instance sum equals
`monitored_table_count` / `total_table_count`. Add store unit cases for the empty
`keys` no-op and the `limitPerTarget <= 0` guard
(`backend/store/quality_execution.go:818-826`).

**g-5 — The overview now fails outright when the cosmetic environment-title read
fails.**
`backend/api/v1/data_quality_overview.go:74-78` returns the `ListEnvironments`
error, aborting the whole overview. Environment titles are presentation only: the
breakdown row already degrades to its key when the title is empty
(`assembleQualityScoreOverview`, `:157-159`, `coverageLabel` in
`DataQualityOverviewPage.vue`). The RPC previously had no settings dependency, so a
settings/profile read blip now blanks the page.
**Fix:** treat the failure as an empty title map (log at most once) and still return
the coverage and score.

**g-6 — The new report assertion does not prove the score card renders.**
`backend/api/v1/data_quality_report_test.go:195` asserts the document contains
`"100.0"`, but the same fixture already renders `"100.0%"` for the rule pass rate
(`report_test.go` `rulePassRateText`), so the assertion passes even if the score
card is deleted or regressed. The `qualityreport` unit test's `"85.0"` assertion is
unambiguous and does the real work.
**Fix:** assert the label/value pair (find `Quality score` and its card value) or
use a score that cannot collide with another number in the document (e.g. 82.5),
or assert through `newReportView` like `TestRenderQualityScoreUnset`.

**g-7 — The new journey health assertions duplicate the fixture table as magic
numbers.**
`backend/test/integration/runner/data_quality_journey_service_test.go:152` passes
`17, 10.0/17.0` by hand. `createJourneyRules`
(`data_quality_journey_service_test.go:270-505`) already returns every spec with
`targetGUID` and `firstRun`, and the same test derives other expectations from
that slice. Adding one rule or changing one seeded verdict today fails P2 with a
confusing mismatch instead of a clear "the fixture changed" signal.
**Fix:** compute `wantScored` and `wantPassRate` from `rules` filtered to
`targetGUID == source.orders` (`count` and `count(firstRun == PASS)`), as
`assertJourneyOverview` already derives its bounds from `len(rules)`.

**g-8 — The overview's coverage denominator is an unbounded registry scan on every
load.**
`backend/store/quality_overview.go:59-72` scans every TABLE / EXTERNAL_TABLE /
VIEW / MATERIALIZED_VIEW registry row under live instances and evaluates four
`jsonb_array_length` expressions per row, with no LIMIT and no cache; the overview
has no per-RPC deadline (Connect). A metadata registry of a few million objects
makes the overview page seconds long, and the page re-runs the whole aggregate
each time the 7/30-day trend toggle is used. The proto comment's "a handful of
queries" is true of the count and silent about the cost.
**Fix (triaged):** acceptable for the current scale if documented as a known cost
in `.agents/docs/data-quality.md` §7 #25; when it bites, cache the coverage totals
for ~1 minute or move the denominator to a scheduled roll-up. Do not add a new
table in this PR.

## Verified good (no action)

- **Type classification (all 18).** `ScoreModeFor`
  (`backend/component/dataquality/score.go:24-33`) derives the row-level set from
  `ruleTypeSpecs[].CountsTotal` plus the explicit `NULL_PERCENT` case, and the
  contract test (`score_test.go:15-51`) pins twelve row types, six verdict types
  and the two unknown-type paths. I walked every compiler/observation pair: the
  eleven `CountsTotal` types plus `null_percent` really do carry
  `failed_rows`/`total_rows` (including `schema_drift`, whose total is the
  expected-column count), and the six verdict types have no denominator. No type
  is missing or misclassified.
- **Score math and optionality.** `CheckScore` clamps, falls back to the verdict
  when `total_rows <= 0`, excludes ERROR/SKIPPED/NOT_EVALUATED/UNSPECIFIED from both
  sides, and `CheckWeight`/`ScoreAggregate.Score` produce
  `100 × Σ(weight×score)/Σweight` with an unset result (nil, not 0/100) when
  nothing is judged. Weights WARNING 1 / ERROR 2 / CRITICAL 3 match the accepted
  decision, and an unspecified severity counts as a warning. Unit tests pin the
  arithmetic and the unset case; the API tests pin a hand-computed
  `100*5.6/6`/row-level example.
- **Coverage denominator and guard as accepted.** The denominator is the live-
  instance registry of the four object types, columns from the object metadata
  arrays (verified against `buildColumnMetadataResources`, which writes COLUMN
  rows only for TABLE — the doc's reason for not using `ListMetadata(COLUMN)` is
  correct); the numerator excludes rules whose target row is gone or whose
  instance is soft-deleted, while `enabled_rule_count` and the target directory
  still show them (`assembleQualityScoreOverview:171-178`,
  `ListExistingMetaRegistryTargets`); per-target score/report use the target's own
  enabled checks. The integration journey's dropped-target and soft-deleted-
  instance scenarios pass against real PostgreSQL.
- **Aggregation/assembly consistency.** The target directory, `GetQualityTarget`
  and the report all use the same `qualityTargetScores`/`qualityRecentPassRate`
  helpers, and the report's `unscored = enabled − scored` is non-negative because
  the score helper re-filters to ENABLED. The integration test asserts the
  directory row and the workspace header return identical score, scored count,
  pass rate and execution count. The per-target score deliberately includes
  orphaned targets' own checks, as accepted.
- **Proto/API.** Field numbers 13–23 on `QualityOverview`, 16–19 on
  `QualityTargetSummary`, and the three new breakdown messages are additive with
  no reserved-number collision; `optional double` is used correctly for the
  "unset is not 0/100" fields; comments state the unset and exclusion semantics.
  No RPC signature, annotation, permission or audit flag changed (reads stay
  unaudited, as accepted); no `proto/store` change and no JSONB/store change
  sneaked in. `buf generate` reproduces the committed output exactly.
- **Removed `CountMonitoredTables`.** No Go/TS reference remains; the overview's
  new denominator replaces it. The maintenance reference and the user doc were
  updated (the only stale mention is the historical proposal doc, n-3).
- **Recent pass rate semantics as accepted.** The SQL filters rules to
  ENABLED and not deleted *before* the window (`quality_execution.go:797-799`), so
  paused/archived history neither enters the score nor occupies one of the 20
  slots; ERROR/SKIPPED/NOT_EVALUATED are excluded from the ratio and judged count;
  WARN is in the denominator only. The `limitPerTarget <= 0` and empty-key guards
  are correct.
- **Frontend.** The overview's coverage and score cards show `-` for an undefined
  denominator/score and `0.0%` for a real zero; the four lenses are local (no
  second read), instance/environment sort by title-or-key, dimensions by enum
  order, owners alphabetically with the unassigned bucket last, and the breakdown
  switch is a proper `role="group"` with `aria-pressed`. The target directory row
  and the workspace header both render the score and pass rate with a dash for
  "not judged". The `utils/dataQualityOverview` helpers are pure and unit
  tested (percent/score/pass-rate formatting, null denominators, NaN, ordering),
  and the new page tests assert concrete DOM text and the no-refetch behaviour
  rather than snapshots. The mobile score line is the `md:hidden` mirror of the
  desktop cell, not a duplicate.
- **i18n/glossary.** All four gates pass; coverage/quality score/pass rate are in
  `glossary.json` with forbidden variants declared, the generated glossary table is
  in sync, and both locales carry the same keys and placeholders. The Chinese copy
  uses the accepted renderings (覆盖率/质量分/通过率).
- **Docs truth (apart from G-1 and the cross-read approximation in g-1).**
  D-75/D-76/§3.10/I-44/#22/#25, the overview
  paragraph and the user doc's "Coverage and quality score" section describe the
  implemented formulas, exclusions, unset behaviour, guard, report card and known
  limitations; I found no other claim the code does not honour. The removed
  "monitored tables" wording and non-goal list were updated.

## Nits

**n-1 — `judged_table_count` has no consumer.** The proto field 16
(`proto/v1/v1/data_quality_service.proto:2401`) is assembled and documented
(`.agents/docs/data-quality.md:330`) but the overview page never renders it (only
the test fixture sets it). Either show it (e.g. "N judged" in the coverage hint) or
defer the field to P0-4, which is where asset-page trust signals land.

**n-2 — Two proto comments omit the orphan-target exclusion.**
`QualityDimensionScore.rule_count` (`proto/v1/v1/data_quality_service.proto:2455-2456`)
says "Enabled rules of this dimension" and `QualityOwnerScore.rule_count`
(`:2468`) has no comment, but both counts exclude enabled rules whose target has no
coverage row, as the maintenance reference says. Add "that still resolve to a
registry row" to both comments so the API contract matches the code without
reading D-75.

**n-3 — Stale reference to the removed store method in the historical proposal
doc.** `.agents/docs/data-quality-target-centric.md:47` still says
"`CountMonitoredTables` 只能给全工作区一个数（`store/quality_overview.go:16-54`）" —
the function is gone and the line range is stale. One-line edit, or mark the doc
as a historical proposal in its status line.

**n-4 — `statCoverageHint` is not plural-aware.** `frontend/src/locales/en-US.json:879`
renders "1 / 1 tables" and "1 columns"; every other count hint in this area uses
pipe plurals. Add `table | tables` / `column | columns` (zh-CN is unaffected).

## Verdict

The P0-3 feature is well built: the score is a small, auditable pure function
derived from the single rule-type catalog and pinned by contract tests; the
coverage guard follows the accepted decision; directory/workspace/report agree; and
the whole path is proven against a real server. **No blocker.** G-1 (column
coverage can exceed 100% after schema drift, contradicting the maintenance
reference and the user-facing promise) and G-2 (the directory's recent-executions
window scans unbounded history and ships sample payloads to compute two counts)
should be fixed before release; the rest are precision, test, copy and robustness
items that can be triaged. With G-1 fixed (or the invariant reworded and the
numerator intersected)
and G-2 replaced by a bounded per-target read, I would approve this commit.
