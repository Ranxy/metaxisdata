# P0-4 (asset trust signals) — adversarial review

Reviewer: independent adversarial review of branch `feat-dq-asset-trust-signals`
(commits `dc72b71` proto + generated, `be61977` read/aggregation, `d3e868c` MCP,
`7c92aa2` frontend, `75cbb0c` docs) in
`/home/ran/gocode/metaxisdata/.agents/worktrees/feat-dq-asset-trust-signals`.
Base: `6cff8b4`. Reviewed the whole 65-file diff `git diff 6cff8b4..HEAD`, the
P0-4 row of `.agents/reviews/data-quality-competitive-gap-analysis.md` §5, D-77
and §3.11 of `.agents/docs/data-quality.md`, `docs/data-quality.md`, the MCP
docs and guide, the P0-3 review (for format), the nested `AGENTS.md` files for
backend/proto/frontend, and the accepted decisions listed on the shared task.
Existing decisions are not re-litigated; where the code or the docs contradict
them, that is reported as a defect.

## What was exercised

- `git diff 6cff8b4..HEAD` — all 65 files read. `git status --porcelain` was
  clean before the review; the only change this review introduces is this
  document (temporary probes removed, see below).
- `go build ./...` — pass.
- `go test ./backend/store ./backend/api/v1 ./backend/mcp` — pass.
- `gofmt -l backend/` — clean.
- `golangci-lint run --new-from-rev=6cff8b4 --allow-parallel-runners ./backend/...`
  — 0 issues.
- `cd proto && buf format -d && buf lint && buf generate`, then
  `git status --porcelain` — format/lint clean, generated Go/frontend/grpc-doc
  output is current (no diff after regeneration).
- `pnpm --dir frontend type-check` — pass.
- `pnpm --dir frontend test run` — 164 files / 1678 tests pass.
- `pnpm --dir frontend biome:check` — clean; `pnpm --dir frontend i18n` — all
  checks pass (110 glossary terms / 94 rules).
- `make test-integration-data-quality` — pass in 33.7 s against real
  PostgreSQL 16 + MySQL 8.4 (testcontainers), including the new
  `assertJourneyTargetSelection` and the coverage assertions.
- Read the backend paths end to end: `ListQualityTargets` (directory + exact
  selection), `assembleQualityTargetPage`, `qualityTargetColumnCoverage`,
  `GetQualityTarget`/`loadQualityTargetWorkspace`, `targetWhere`'s selection
  predicate, `ListMetaRegistryTargetColumns`, `ConfigColumn`, the MCP
  `get_quality_signal` path and its wiring, plus the whole frontend addition
  (utils/composables/components/pages/i18n) and all new tests.
- Temporary integration probe (deleted after use): a real MySQL fixture table
  driven through column-drop → pause-all → column-rename → table-drop, with the
  coverage pair compared across the exact selection, the directory read and
  `GetQualityTarget`, plus a VIEW target (freshness on a view column) to cover
  the `viewMetadata` column branch. Every stage agreed; see "Verified good".
- Temporary MCP probe (deleted after use): a summary for an all-paused target
  whose last run passed, marshalled through the real tool.
- Temporary PostgreSQL probe (container deleted after use): production schema
  from `backend/migrator/migration/LATEST.sql`, 1,000 rules across 250 targets
  and 180,000 retained executions, `EXPLAIN (ANALYZE, BUFFERS)` of the
  selection query with 20 keys, and of a scratch rewrite that pushes the key
  predicate into the `live` CTE (product code untouched). Details in M-1.
- Not run: `pnpm audit:ui` (no live app/CDP in this environment; no UI budget
  change in the diff).

## Blockers

None. The feature is correct on every accepted selection, coverage, three-state
and read-only decision: I could not make coverage over-count, make an
unmonitored object render as a verdict, make the selection behave like a page,
make a failed read look like "not monitored", make the three surfaces disagree,
or make MCP answer absence/paused with an error. M-1 is a scalability defect on
the new hot read path, not a correctness one.

## Majors

### M-1 — The exact selection still runs the directory's whole-workspace aggregation on every metadata page, and the docs claim the only extra cost is one registry read.

`backend/store/quality_target.go:256-270` renders `TargetKeys` as
`(t.target_guid, t.target_type) IN (SELECT * FROM unnest(...))` over the final
`targets` CTE. `live` (`:300-304`) selects every non-archived rule in the
workspace, `targets` (`:305-317`) groups all of them, and `latest`
(`:318-326`) reads the newest non-zero-status execution of **every** live rule
(`DISTINCT ON (e.rule_id)` over all executions); `results` then aggregates that
whole set. The key predicate is applied after the aggregations, and the planner
does not push it into the `live`/`latest` scans — so a selection of 20 objects
costs the same order of work as rendering the whole DQ directory, on *every*
metadata browse/list/detail load (P0-4 adds the caller; previously only the DQ
targets page paid it).

Probe (fresh PostgreSQL, production DDL, 1,000 rules = the workspace cap
`backend/api/v1/data_quality_service.go:40`, 250 targets, 180,000 retained
executions = 90-day retention `backend/runner/maintenance/maintenance.go:55`):

- Selection with 20 keys: `Index Scan using idx_quality_execution_rule_time on
  quality_execution ... rows=180000`, `Incremental Sort ... rows=180000`,
  `Execution Time: 201.5 ms`, `Buffers: shared hit=181,044`; it returns 20 rows.
- Scratch rewrite adding the same key predicate to the `live` CTE:
  `Execution Time: 65.7 ms`, `Buffers: shared hit=4,539`. Filtering the
  execution read to the selected rules as well (easy with the existing
  `idx_quality_execution_target_time` / `idx_quality_execution_rule_time`)
  makes it O(page) instead of O(retained history).

The cost scales linearly with retained executions: a 1,000-rule workspace on an
hourly cadence inside the 90-day window is ~2.1 M execution rows, a 15-minute
cadence ~8.6 M. This is exactly the failure mode P0-3 G-2 called out, moved onto
a much hotter page. The maintenance reference understates it:
`.agents/docs/data-quality.md:388` (§3.11) and `:611` (§7 #26) say the only
additional read per page is one registry column list and are silent about the
aggregation.

**Fix:** when `find.TargetKeys` is non-empty, bind the key set into the `live`
CTE as well (and, to bound `latest`, into the execution read — e.g.
`e.rule_id = ANY($selectedRuleIDs)` plus the existing rule-time index, or
`(e.target_guid, e.target_type) IN ($keys)` with
`idx_quality_execution_target_time`), so `targets`/`latest`/`results` are
computed for the page only; keep the outer predicate as a safety net. Bound
`issues`/`drafts` likewise or justify them as small. Add a regression that fails
when the selection plan reaches executions of unselected rules — e.g. a store
test asserting the query text carries the key predicate in `live`, plus an
integration/EXPLAIN case for the plan shape — and update §3.11/§7 #26 with the
real cost.

## Minors

**m-1 — The "one assembly path" claim is only half true: `GetQualityTarget`
assembles its own summary; only the coverage helper is shared.**
`.agents/docs/data-quality.md:383` says the directory, the asset badge and
`GetQualityTarget`'s workspace header "都走 `assembleQualityTargetPage`", and
D-77 (`:111`) says the three "共用同一个装配函数". In code,
`assembleQualityTargetPage` (`backend/api/v1/data_quality_target.go:179`) fills
the directory page and the exact selection only; `GetQualityTarget`
(`:519-594`) rebuilds the summary from `loadQualityTargetWorkspace` +
`qualityTargetScores` + `qualityRecentPassRate`, and shares only
`qualityTargetColumnCoverage` (`:582`). I verified the numbers agree today (the
journey asserts score/pass rate/worst status/coverage between the directory read
and `GetQualityTarget`, and my drift probe compared all three surfaces at every
state), so this is not a wrong-number bug; the claimed structural guarantee is
what does not exist, so a future edit to one path can drift silently.
**Fix:** either route Get through the shared fill (build a
`store.QualityTargetMessage` for the target and call `assembleQualityTargetPage`),
or reword D-77/§3.11 to "directory + selection share `assembleQualityTargetPage`;
the workspace header shares the coverage helper and is pinned to agree by the
journey", and extend the journey agreement assertion to a paused and an orphaned
target (today it pins one monitored TABLE).

**m-2 — §3.11 and the ListQualityTargets bullet say a key is absent when the
target has "no enabled rule", which contradicts the "no non-archived rule"
definition and the code.**
`.agents/docs/data-quality.md:272` and `:369` describe absence as "无启用规则"
/ "没有启用规则". The store excludes only ARCHIVED (`backend/store/quality_target.go:300-304`),
D-77 (`:111`) says "没有任何非归档规则的目标缺席", and a paused-only target is
returned with `enabled_rule_count = 0` (verified: after pausing every check my
probe still received the row, enabled=0, 0/2 coverage). As written, a reader
would expect an all-paused target to be absent/“not monitored”.
**Fix:** change both lines to "没有非归档规则" / "no non-archived rule".

**m-3 — MCP reports a stale health verdict for an all-paused target.**
`backend/mcp/tool.go:648-663` always maps `summary.worst_status` when the row is
present. The aggregate includes paused checks' last verdicts, so an all-paused
target that last passed answers
`{"monitored":true,"enabledRuleCount":0,"worstStatus":"QUALITY_STATUS_PASS",...}`
(confirmed with a temporary tool probe). The UI deliberately suppresses that
verdict (`frontend/src/utils/dataQualitySignal.ts:65-88` maps all-paused to a
neutral "All checks paused", and D-77/§3.11 call paused "not a health verdict"),
and the MCP docs (`docs/mcp.md:96-102`, `backend/mcp/guide.md:92-104`) explain
`enabledRuleCount: 0` but never tell the consumer to ignore `worstStatus`. A
model reading it will report the table as healthy. **Fix:** omit `worstStatus`
when the summary is all-paused (the tool already omits it for UNSPECIFIED,
`:712`), or add an explicit `allPaused: true` field and document it; add an MCP
test for the all-paused answer (today `tool_test.go` covers monitored,
unmonitored and never-judged only).

**m-4 — The user docs say the list's "not monitored" mark links to the
workspace; it does not.**
`docs/data-quality.md:315-316`: "A table with no check is marked 'not monitored'
and links to the target workspace to add one." The list cell intentionally has
no link (`frontend/src/components/data-quality/QualitySignalCell.vue:38-39`,
because a link inside a clickable row swallows the row click); only the asset
badge (`QualitySignalBadge.vue:22-31`) and the DQ-tab strip
(`MetadataQualitySection.vue:70-88`) carry the `add=1` CTA. **Fix:** reword to
name the surfaces (the asset header badge and the Data Quality tab offer "Start
monitoring"; the browser column is text-only).

**m-5 — The user docs compare the target score with a number the Overview page
does not show.**
`docs/data-quality.md:324`: "The score is the same number as on Data Quality →
Overview". `GetQualityOverview` returns the workspace and dimension/owner scores
(`backend/api/v1/data_quality_overview.go:170-237`) and never a per-target score;
the asset page shows `QualityTargetSummary.quality_score`
(`backend/api/v1/data_quality_target.go:587-588`). The formula is shared, the
aggregation is not. **Fix:** "uses the same weighted formula as the scores on
Overview; the target directory and the workspace header show the same target
score".

**m-6 — The maintenance reference claims every unmonitored/paused surface offers
a CTA; the list cell does not.**
`.agents/docs/data-quality.md:292` and `:386` say the neutral states "give a
CTA" (unmonitored with `add=1`, paused opens the workspace). The list column has
no link by design (same comment as m-4); the CTA lives in the badge and the DQ
tab. **Fix:** name the surfaces, and keep the list column documented as
text-only.

**m-7 — The new page-level read wiring and two coverage branches have no
end-to-end test.**
(a) `frontend/src/pages/MetadataBrowserPage.vue:944-995` is new logic: which
targets a screen asks about (`leaf` vs `activeGroup.items`), the two
`useQualitySignals` instances, the search-dropdown open gating, and the
`qualitySignals`/`qualityState` pass-through. The components are unit-tested
with synthetic maps, but a wrong meta type in
`qualitySignalFor(currentGuid, MetaType.TABLE)` (or a mixed-up prop) would render
a monitored object as "Not monitored" with every gate green; there is no
`MetadataBrowserPage.test.ts` today. (b) Target-level coverage for
MATERIALIZED_VIEW / EXTERNAL_TABLE reads the other two metadata column arrays
(`backend/store/quality_overview.go:88-107`), but the P0-4 integration assertion
only covers a TABLE (`backend/test/integration/runner/data_quality_journey_service_test.go:960-969`);
my probe added a VIEW (2 columns, 1 covered — passed), leaving the MV/external
branches unexercised. **Fix:** add a page test that mocks `listQualityTargets`
for a leaf and for a list page and asserts the request keys and rendered states,
and add one MV or external-table registry fixture (or a store-level case) for
the column branch.

## Nits

- **n-1 — "Last run Never run".** `frontend/src/composables/useQualitySignalText.ts:124`
  renders `dataQuality.signal.lastRun` with `lastRunText()`; for a monitored but
  never-run target the tooltip reads "… · Last run Never run". Omit the clause
  when `lastRunTime` is unset (the status word already says "Never run").
- **n-2 — Guide enum names do not match the payload.**
  `backend/mcp/guide.md:95` lists `PASS`, `WARN`, `FAIL`, `ERROR`, `SKIPPED`,
  `NOT_EVALUATED`, but the tool emits `QUALITY_STATUS_WARN` etc.
  (`backend/mcp/tool.go:712-717`). Either spell the full names in the guide or
  strip the `QUALITY_STATUS_` prefix to match the guide/CLI-friendly contract;
  the test currently pins the prefixed form (`backend/mcp/tool_test.go:708`).
- **n-3 — 0/0 is always explained as "the target no longer resolves".**
  `frontend/src/pages/data-quality/DataQualityTargetsPage.vue:678-684` picks
  `dataQuality.targets.coverageUnresolved` whenever `qualitySignalCoverage`
  returns null, but a resolvable object with an empty registry column list also
  reports 0/0 (`backend/store/quality_overview.go:146-154` keeps the key with an
  empty set; `qualityTargetColumnCoverage` at
  `backend/api/v1/data_quality_target.go:278` returns 0/0). Base the copy on
  `summary.orphaned`, or say "no current column list".
- **n-4 — The 200-key cap is enforced before deduplication**
  (`backend/api/v1/data_quality_target.go:120-124`), so 201 copies of one key
  are `InvalidArgument` even though they collapse to one target. Harmless for
  the browser (it chunks distinct keys), and consistent with the proto wording;
  noted only so the ordering is a conscious choice.

## Verified good (no action)

- **Gates green.** build, unit tests, gofmt, golangci-lint (0 issues), buf
  format/lint/generate with a clean status afterwards, frontend type-check,
  biome, i18n, 1678 frontend tests, and the real-server DQ journey integration
  test all pass.
- **Selection is an exact, unpaged batch read.** Unit tests pin validation
  (missing GUID, UNSPECIFIED meta type, non-targetable type, >200 keys, exactly
  200 accepted, duplicates collapse, request order restored) and the store test
  pins the `$14/$15` unnest predicate. The journey proves it end-to-end:
  `page_size=1` still returns both monitored targets, `next_page_token` is
  empty, an unmonitored table is absent, a duplicate key collapses, request
  order is preserved, and an invalid key is `InvalidArgument`. I additionally
  confirmed a paused-only target stays present with `enabled_rule_count = 0`
  (it is not treated as absent), and that other directory filters combine with
  the selection as documented.
- **Coverage cannot over-count.** One `ListMetaRegistryTargetColumns` read feeds
  both numerator and denominator (`backend/api/v1/data_quality_target.go:246-268`),
  `qualityTargetColumnCoverage` intersects (`:278-286`), and the pair is 0/0
  when the target does not resolve. My drift probe (real MySQL + registry sync)
  confirmed all of the following on the selection, directory and workspace
  paths, all agreeing: 2/3 initially (a table-level rule covers no column);
  1/2 after dropping the checked column; 0/2 after pausing the remaining
  column rule (paused covers nothing); 0/2 after renaming it; 0/0 with
  `orphaned=true` after dropping the table. A VIEW target reports 2 total
  columns from `viewMetadata`, 1 covered — the view branch of the column query
  works. The journey's 10/6 assertion additionally covers the multi-column rule
  types (unique_combination, cross_column, group_check, cel) contributing
  nothing.
- **The three UI states stay distinct and none of the first two is a verdict.**
  Absent → "Not monitored" (neutral dot, no second line); `enabledRuleCount = 0`
  → "All checks paused" (neutral, hint, no score/pass-rate facts); monitored but
  `worstStatus = UNSPECIFIED` → "Never run" (neutral). PASS/WARN/FAIL/ERROR map
  to success/warning/destructive/danger. Component tests assert no
  `.bg-success` in the unmonitored/paused/never-run cells, the badge, and the
  directory row; the directory's paused badge is neutral and its score/pass-rate
  line is hidden.
- **A failed or loading read is never "not monitored".** The composable keeps a
  `denied/loading/ready/failed` state (`frontend/src/composables/useQualitySignals.ts:130-210`);
  cells and badges render nothing unless they have an answer or the read is
  ready; a superseding read is aborted and cannot overwrite the screen; a failed
  read leaves the cells blank (unit tests cover all of these, including the
  late-failure case).
- **Permission and read-only surface.** The column and badge are hidden and no
  request is made without `metaxisdata.dataQuality.rules.list`
  (`useQualitySignals.ts:92-135`; list props are `undefined` in
  `MetadataBrowserPage.vue:289-293`). The diff adds no table, migration, write,
  audit row or rate-limit entry; the export filter-parity gate was updated to
  classify `targets` as `list_only` (`backend/api/v1/data_quality_export_test.go:457-500`).
  The MCP tool reuses the existing per-call audit and the RPC's permission.
- **MCP absence is an answer, not a failure.** `monitored: false` for absence
  and for object types no rule can watch (no RPC call at all), bare GUID without
  `meta_type` is `InvalidArgument`, judged fields are omitted until judged, and
  the selection request is one key with no page size/token. Verified by the new
  tests and by a tool probe.
- **The Home card does not count paused targets as monitored.**
  `useDashboard` calls `GetQualityOverview({trendDays: 1})` only with the
  permission, and `monitored_table_count` is built from enabled rules only
  (`backend/api/v1/data_quality_overview.go:170-237`); a failure lands in the
  page's failed-section list instead of blanking the page.
- **Numbers and copy match the docs where it matters.** 200-key cap, 20/50 page
  sizes, `trend_days=1`, 10 MCP tools, the D-77 formula, `0/0 → "-"`, the
  permission string, and the new i18n/glossary terms (110 terms / 94 rules) are
  consistent across code, proto comments and both docs trees; the generated
  proto/frontend/grpc-doc output is current.
- **Tests that exist are meaningful.** They cover the pure helpers (tone,
  coverage, keys, wording, tooltip, facts), the composable state machine
  (batching, supersede, prune, failure, permission), all three signal components,
  list/layout forwarding, the directory row, the DQ tab strip, the asset badge,
  the API request shape, the dashboard, and the MCP tool's monitored/unmonitored/
  never-judged/validation paths.

## Verdict

**Approve for merge once M-1 is fixed or explicitly accepted as debt, and m-3 is
decided.** The product behavior is correct on every accepted definition: the
selection is a real exact batch read, the coverage pair is honest in every drift
state and identical across the three surfaces, the three neutral states are
distinct and never green, a failed read never impersonates "not monitored",
permissions/read-only hold, and the MCP absence/paused contract is honored. The
one substantive problem is that the new metadata-page read is not bounded by its
selection and the reference understates the cost; the fix is local to
`ListQualityTargets` and testable. m-1/m-2/m-4/m-5/m-6 are documentation accuracy,
m-3 is an MCP honesty gap worth fixing while the surface is new, m-7 is test
coverage, and the nits are copy.

## Post-fix verification (75cbb0c..3b7378e)

Reviewed the five commits on top of the reviewed tree: `7a3af9b`
(`fix(data-quality): bound the trust-signal read to the selected targets`),
`69030a1` (`fix(mcp): stop reporting a paused target's last verdict as its
health`), `b5aeedc` (`test(frontend): cover the metadata page's trust-signal
wiring`), `f5770db` (`docs(data-quality): correct the trust-signal cost and
surface claims`) and `3b7378e` (this review). Read the whole
`git diff 75cbb0c..HEAD` (20 files) and the new tests. No product code changed
as part of this verification; the temporary probes below were removed and the
worktree contains only this document's edit.

### What was re-exercised

- Gates: `go build ./...`; `go test ./backend/store ./backend/api/v1
  ./backend/mcp`; `gofmt -l backend/` clean; `golangci-lint run
  --new-from-rev=6cff8b4 --allow-parallel-runners ./backend/...` — 0 issues;
  `buf format -d`, `buf lint`, `buf generate` then `git status --porcelain`
  clean; `pnpm type-check`; `pnpm test run` — 165 files / 1683 tests pass on an
  idle machine (see r-1); `biome:check` clean; `i18n` all checks pass (110
  terms / 94 rules); `make test-integration-data-quality` — PASS in 36.1 s
  against real PostgreSQL 16 + MySQL 8.4.
- **M-1 EXPLAIN probe, same fixture as the review**: production schema from
  `LATEST.sql`, 1,000 rules (the workspace cap) over 250 targets, 180,000
  retained executions (90-day retention), a 20-key selection. I rendered the
  *actual* new statement through `qualityTargetsStatement` into literal-argument
  SQL (temporary store test, removed) and reconstructed the pre-fix statement
  for the same fixture, then ran `EXPLAIN (ANALYZE, BUFFERS)`:

  | Query | Execution time | Execution buffers | Plan |
  | --- | --- | --- | --- |
  | selection, pre-fix | 182.0 ms | 181,044 shared hits | index scan `rows=180000` over every retained execution, then incremental sort |
  | selection, post-fix | **3.62 ms** | **438 shared hits** (max) | hash semi-join of the 20 keys into 1,000 live rules, then per-rule `JOIN LATERAL (… LIMIT 1)`: `Index Scan idx_quality_execution_rule_time … loops=80, rows≈2`; 20 rows returned |
  | directory, pre-fix | 178.2 ms | 181,044 shared hits | scan over every retained execution |
  | directory, post-fix | **13.0 ms** | **5,042 shared hits** | 1,000 per-rule index probes; no execution-history scan |

  The original baseline was 201.5 ms; the same-fixture pre-fix rerun is
  182.0 ms, so the comparison is conservative. The plan confirms the selection
  is pushed into `live` (hash semi join), `issues`, `drafts` and the outer
  safety net, and that the execution read is one bounded probe per selected
  rule rather than a scan of the workspace history. The directory path keeps
  its semantics and gets faster as a side effect.
- **Coverage/state drift probe, re-run** (temporary integration test, removed):
  the exact selection, the directory read and `GetQualityTarget` still agree at
  every stage on a real MySQL fixture — 2/3 initially (the table-level rule
  covers no column), 1/2 after `DROP COLUMN`, 0/2 after pausing the remaining
  column check, the all-paused target still returned with
  `enabled_rule_count = 0` and the directory agreeing, 2 total / 1 covered for
  a VIEW target, and 0/0 with `orphaned=true` after `DROP TABLE`. No
  behavioral regression from the query rewrite.
- **MV/external column arms** of `qualityTargetColumnQuery` (the per-page read
  P0-4 calls) exercised directly against the production schema with synthesized
  `MATERIALIZED_VIEW`, `EXTERNAL_TABLE`, `VIEW` and `TABLE` registry rows: each
  returns its own metadata array's columns, and a missing key is absent (0/0).
  The automated gap remains (r-3).
- Re-read the changed docs, proto comments, MCP guide and both data-quality
  docs against the code, and read the new store/MCP/page tests.

### Per-finding status

| Finding | Status | Evidence |
| --- | --- | --- |
| **M-1** unbounded selection read | **fixed** | `qualityTargetsStatement` (`backend/store/quality_target.go:334`) pushes the key pair into `live` (`:383`), `issues`, `drafts` and the outer safety net (`:357-370`), and `latest` is a per-rule `LATERAL … LIMIT 1` (`:401`). `TestQualityTargetsStatementPushesSelection` (`backend/store/quality_target_test.go:214`) pins the four predicates, the LATERAL/LIMIT and the `$14/$15 → $16/$17` layout; probe above: 182.0 → 3.62 ms, 181,044 → 438 buffer hits. §3.11 (`:388`) and §7 #26 (`:611`) now describe the real cost. |
| **m-1** "one assembly path" docs | **fixed**, one stale row left (r-2) | D-77 (`:111`) and §3.11 (`:383`) now say directory + selection share `assembleQualityTargetPage` while `GetQualityTarget` reuses the coverage/score/pass-rate helpers and is pinned by the journey — matches the code. |
| **m-2** absent = "no enabled rule" | **partially fixed** (r-2) | Proto comment (`proto/v1/v1/data_quality_service.proto:2551-2555`) and the §3.6 bullet (`.agents/docs/data-quality.md:272`) fixed; `:369` still says "缺席就表示该目标没有启用规则". |
| **m-3** MCP paused verdict | **fixed** | `backend/mcp/tool.go:581-585, 657-676` adds `allPaused` and omits `worstStatus` when `enabledRuleCount <= 0`; `TestGetQualitySignalReportsAllPaused`; `docs/mcp.md:96-102` and `backend/mcp/guide.md:88-96` updated. A paused target whose last run passed now answers `allPaused: true`, `enabledRuleCount: 0` and no verdict. |
| **m-4** list "not monitored" link | **fixed** | `docs/data-quality.md:315-319` rewrites the bullet: text-only list cell, Start-monitoring CTA on the badge and tab. |
| **m-5** score vs Overview | **fixed** | `docs/data-quality.md:324-327` now compares the target score with Targets/workspace header and only the formula with Overview. |
| **m-6** CTA claim | **fixed** | `.agents/docs/data-quality.md:292` and `:384` name the surfaces and keep the list cell text-only. |
| **m-7(a)** page wiring test | **fixed**, new timing risk r-1 | `frontend/src/pages/MetadataBrowserPage.test.ts` mounts a TABLE leaf, a VIEW leaf, a list level and a no-permission page and asserts the `guid#metaType` selection keys, the rendered verdict, the unmonitored row and the no-request case. |
| **m-7(b)** MV/external coverage test | **residual accepted** (r-3) | SQL arms directly verified (above); no MV-capable engine in the harness and the query is the shared P0-3 workspace-coverage read, so deferring is acceptable. |
| **n-1** "Last run Never run" | **fixed** | `frontend/src/composables/useQualitySignalText.ts:126-131` omits the clause when `lastRunTime` is unset. |
| **n-2** enum names in guide | **fixed** | `backend/mcp/guide.md:95-99` names the full `QUALITY_STATUS_*` values. |
| **n-3** 0/0 tooltip | **fixed** | `frontend/src/pages/data-quality/DataQualityTargetsPage.vue:679-693` splits orphan vs empty column list with a new key, and the page test covers both. |
| **n-4** cap before dedupe | **rejected (conscious)** | The cap applies to the request list and is documented in the proto comment; acceptable. |

### Residual findings

**r-1 (Minor, test reliability) — the new leaf page test is timing-sensitive
under load.**
`frontend/src/pages/MetadataBrowserPage.test.ts:190` waits for the badge inside
a `defineAsyncComponent` (`TableMetadataDetail` is `defineAsyncComponent`,
`frontend/src/pages/MetadataBrowserPage.vue:371`) using `vi.waitFor`'s default
1 s timeout. It passes isolated (3/3, ~0.7-0.9 s) and the whole suite passes on
an idle machine, but two consecutive `pnpm test run` invocations failed exactly
this assertion while the DQ integration containers and the PostgreSQL probe were
running in parallel (1 failed / 1683 on both). This is not a product defect, but
on a loaded CI worker it will intermittently red the frontend gate. **Fix:** pass
an explicit timeout — `await vi.waitFor(..., { timeout: 5000, interval: 50 })`
— or await the page's load state before mounting.

**r-2 (Minor, doc accuracy) — two stale claims the m-1/m-2 fixes missed.**
`.agents/docs/data-quality.md:624` still calls
`assembleQualityTargetPage`/`qualityTargetColumnCoverage` "目录行、精确选择与
工作台页头的唯一装配路径", contradicting the corrected D-77 (`:111`) and §3.11
(`:383`): the workbench header has its own assembly and shares only the helpers.
`.agents/docs/data-quality.md:369` still says a missing selection key means
"该目标没有启用规则"; a paused-only target is returned with
`enabled_rule_count = 0` (reconfirmed by the drift probe), so it must say
"没有任何非归档规则" and name the all-paused returned case, as D-77 (`:111`),
`:272` and the proto comment now do. Both are one-line wording edits.

**r-3 (residual, accepted) — no automated MV/external-table coverage test.**
`backend/store/quality_overview.go:88-107` reads the MV/external column arrays,
but the integration journey still asserts only a TABLE (plus the VIEW probe I
ran by hand). The SQL arms are verified and the query is unchanged from the
P0-3 shared workspace coverage (whose g-4 note listed the same gap), so
deferring is acceptable; fold it into the engine-matrix follow-up when a
MV-capable engine enters CI.

No other regression found: the selection contract (unpaged, request order,
dedupe, 200 cap, absent = not monitored, paused present with
`enabled_rule_count = 0`), the coverage invariants (paused/dropped/renamed
column excluded, 0/0 for an unresolved target, one registry read, three
surfaces agreeing), the three neutral UI states, the permission/no-request
behavior, and the MCP absence/unwatchable/all-paused answers all still hold, and
the generated proto/frontend/grpc-doc output is current.

### Verdict

**M-1 is fixed and verified**: the metadata-page selection now costs its own
targets — 182.0 → 3.62 ms and 181,044 → 438 execution buffer hits on the
1,000-rule / 180k-execution fixture, with the plan bounded to 80 per-rule
probes for 20 keys and no workspace execution scan; the directory read also
improves (178 → 13 ms) instead of regressing. m-1/m-3/m-4/m-5/m-6/n-1/n-2/n-3
are fixed, m-7(a) is covered and n-4 is a conscious choice; the drift probe and
the journey show no behavioral regression. Only two one-line documentation
corrections (r-2) and a test-timeout hardening (r-1) remain, neither touching
product behavior. **Recommendation: approve the branch for merge once r-1 and
r-2 are applied; m-7(b)/n-4 are accepted residuals.**
