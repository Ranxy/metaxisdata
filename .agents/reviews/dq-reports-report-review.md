# P0-2 PR2 (report + frontend report download) — adversarial review

Reviewer: Phase B review of the uncommitted PR2 diff in
`/home/ran/gocode/metaxisdata/.agents/worktrees/feat-dq-reports-exports` (branch `feat/dq-reports-exports`).
Base: `HEAD` (= `1bbaddf`). Reviewed `git diff HEAD -- <paths>` plus the untracked `backend/component/qualityreport/`,
`quality_target_trend.go`, `data_quality_report_test.go`, and the report integration test.

## What was exercised

- Read the handler (`data_quality_report.go`), the extracted assembly (`data_quality_target.go` diff), the renderer
  (`qualityreport/report.go`, `labels.go`, `report.html.tmpl`), the per-target trend store method, both unit test files, the
  report integration test, and the frontend API/page/i18n/locale diff.
- `go build ./backend/...` — pass.
- `go test ./backend/component/qualityreport/ ./backend/store/ ./backend/api/v1/` — pass.
- `go vet -tags integration ./backend/test/integration/runner/` — pass (integration suite not executed; no Docker here).
- `gofmt -l` on every PR2 Go file — clean. `buf` untouched.
- `golangci-lint run ./backend/component/qualityreport/... ./backend/store/... ./backend/api/v1/...` — one **new** finding (C-1).
- Frontend: `pnpm --dir frontend test run src/pages/data-quality/DataQualityTargetPage.test.ts` — 51/51 pass;
  `pnpm run type-check` — pass; `pnpm run i18n` (missing/unused keys, sort, glossary) — pass.

## Blockers

**C-1 — `loadQualityTargetWorkspace` has an unused parameter; the lint gate fails.**
`backend/api/v1/data_quality_target.go:252` declares `workspaceID` but never uses it in the body (the handler separately
validates `parent` via `s.workspaceID`). `revive` (`unused-parameter`, enabled via `enable-all-rules: true`) flags it:
```
backend/api/v1/data_quality_target.go:252:78: unused-parameter: parameter 'workspaceID' seems to be unused, consider removing or renaming it as _
```
This is a CI lint failure introduced by PR2. **Fix:** drop the parameter from `loadQualityTargetWorkspace` and both call sites
(`data_quality_report.go:54`, `data_quality_target.go:366`); the handlers already resolve the workspace. (Renaming to `_` also
silences it but leaves dead signature weight.)

## Majors

**M-1 — The report fetches and copies far more failed-sample data than it can render, with no concurrency bound.**
- `data_quality_report.go:28-31` fetches **50** failed executions; `:289-320` (`qualityReportFailedChecks`) copies every sample
  row and cell of every one of them into the report model. Only after that does the renderer cap to 20 checks × 5 rows
  (`qualityreport/report.go:50-53, 724-745`). The comment's justification ("the extra rows let the report say how many it
  omitted without a second count query") is not what the code does — the omitted count comes from the trend tally
  (`report.FailedCheckTotal = report.Counts.Fail`, `:210`), so 30 of the 50 fetched executions are pure waste.
- Each execution row carries its stored failure sample; the engine caps one execution's result/sample at 1 MiB
  (`.agents/docs/data-quality.md` §3.3), and the execution rows also carry `executed_sql` (≤8 KiB) and messages. A worst-case
  report therefore reads up to tens of MiB into memory before the renderer throws almost all of it away.
- Unlike the streaming exports, the report RPC has **no in-flight bound**: `ExportQualityReport` is unary and does not acquire
  the `exportSlots` semaphore (`data_quality_service.go:108-146`). It is audited, so a principal may call it up to 3000/min
  (`backend/api/v1/principal_throttle_interceptor.go:39-140`), and nothing limits concurrent renders. Several concurrent large
  reports are a plausible memory/CPU spike, and the 2 MiB output cap does not bound the input.
**Fix (report-impl):**
1. Fetch `qualityreport.maxFailedChecks` (20) failed executions, not 50 — export the constant from the renderer or add a
   `MaxFailedChecks` const in `api/v1`; update the stale comment.
2. Trim each check's samples in the handler to `maxSampleRows + 1` (or expose `MaxSampleRows`), so the model never carries more
   than the renderer can use.
3. Acquire the same export/report slot semaphore around the report call (reuse `acquireExportSlot`, or give reports their own
   smaller cap) and release it in a `defer`.

**M-2 — "Showing N of M failed checks" can contradict the list under a race.**
`report.FailedCheckTotal` is set from the trend tally (`data_quality_report.go:210`), which is queried *before* the failed
executions (`:59` vs `:66`). A FAIL execution that completes in between is in the list but not in the tally, so
`qualityreport/report.go:720-729` renders e.g. "Showing 21 of 20 failed checks". The reverse (a check in the tally that the
newest-N list misses) is correct and intended.
**Fix:** make the total and the list come from one observation — either set
`report.FailedCheckTotal = max(report.Counts.Fail, int32(len(failedExecutions)))`, or count on the failed-execution query and
drop the trend-derived total. Add a unit test for the mismatch case.

## Minors

**m-1 — Download-name format contradicts the proto's documented example.**
`qualityReportFileName` formats `20060102T150405Z` (`data_quality_report.go:110-114`) → `quality-report-20261031T120000Z.html`;
the proto comment promises `quality-report-2026-10-31T120000Z.html` (`proto/v1/v1/data_quality_service.proto:2867-2872`), and the
unit/integration tests pin both shapes (`data_quality_report_test.go:52-57`, integration `^quality-report-\d{8}T\d{6}Z\.html$`).
Pick one. My preference: change the Go format to `2006-01-02T150405Z` (matches the doc, still filename-safe) and update the two
tests.

**m-2 — The report embeds the caller's e-mail address.**
`qualityReportGeneratedBy` (`data_quality_report.go:119-134`) returns `"Name <email>"` and the template prints it
(`report.html.tmpl:120`). The document is explicitly forwardable to third parties; the rest of DQ's outward-facing output
deliberately avoids contact details (outbox O-4, and F-11 renamed `owner_email` to `owner_name` for exactly this). Use the
display name only (fall back to the principal resource id) or record the address only in the audit row.

**m-3 — The report widens the read scope of `rules.list` to failure samples.**
The RPC is gated by `metaxisdata.dataQuality.rules.list` (`proto:388-389`), but it renders per-execution failure samples, error
messages and observed values, which the UI otherwise exposes under `executions.get/list`. GetQualityTarget already returns last
results under `rules.list`, but not sample rows. Members hold both permissions, so this only affects custom roles.
**Fix:** either document the implied read scope in the RPC comment and `.agents/docs/data-quality.md` §3.6 (the permission is
the target's read, and it includes its evidence), or gate the sample section behind the executions permission resolved from the
caller's policy — the former is cheaper and matches the "aggregate reads use rules.list" precedent.

**m-4 — Size trimming makes the empty-section text lie.**
When the 2 MiB shrink loop drops rules/issues/checks, `RulesEmpty`/`IssuesEmpty`/`SamplesEmpty` render "This target has no
rules." / "No open issues." / "No check failed in the window." (`qualityreport/report.go:324-327, 438-443`,
`report.html.tmpl:175-176, 217-218, 249-251`) while the `TrimNote` banner says content was omitted. Add a distinct
"not shown because of the size cap" label (the view already knows `limits.trimmed` and the original counts) or always keep at
least one row per section before giving up.

**m-5 — Audit `resource` column is the actor for report calls.**
`ResolveResource` looks for `name`/`user.name`/`email` (`backend/component/audit/audit.go:279`); the report request has
`parent` + `target_guid`, so the row's `resource` falls back to the user (`backend/api/v1/audit.go:97-99` area). The request
payload now carries the target GUID, so the row is still diagnosable, but audit search by target misses it. Add `targetGuid` as a
`ResolveResource` candidate (report/export only) or set `Resource` in the handler via the recorder. (Carry-over of foundation
F-20; not fixed by the report PR.)

**m-6 — No regression test pins that `GetQualityTarget` is unchanged by the extraction.**
I diffed the extracted assembly line by line: the success path is behaviour-identical (same store calls, same filters, same
defaults; only the schedule/run-summary/drafts ordering moved, with no user-visible effect), and all existing `api/v1` tests
pass. But `data_quality_target_test.go` only covers helpers, so a future edit could change the RPC unnoticed. Add one
store-fake/integration assertion set for GetQualityTarget (rules/latest/history/open issues/target columns/orphaned) or an
integration case that compares the report's data with the GetQualityTarget response for the same target.

**m-7 — The 2 MiB shrink loop re-renders the whole document per step.**
`qualityreport/report.go:231-297` halves one budget per iteration (cell text 5 steps, rows 4, checks up to 20, rules up to 100,
issues up to 200). For a pathological report each step is a full template execution of a multi-MiB string. It converges, but the
cost is O(steps × document size) CPU in a request. Shrink multiplicatively (drop checks/rules/issues by halves) and/or cap the
iteration count, then rely on the failure message. Not a behaviour bug.

**m-8 — Integration coverage gaps.**
`backend/test/integration/runner/data_quality_report_service_test.go` is a good end-to-end (both languages, window clamp,
self-containment, orphaned target, no-rules → error) but does not cover: a **non-admin member** token (permission), the
**audit row** (assert `response.htmlContent` is redacted / absent from `audit_log`), the **2 MiB/cap path** (needs a seeded
large execution), a **resolvable target with no rules** (decide whether that should be an empty report or `NotFound`), and
**determinism** (same request twice with a fixed clock is not possible through the API; renderer test covers it). Add at least
the member-token and audit-row cases — they are cheap with the harness.

**m-9 — Frontend: the download button is enabled before the target identity exists.**
`DataQualityTargetPage.vue:69-74` renders the report button unconditionally; `exportReport()` (`:1304-1332`) sends
`targetGuid.value`, which is empty on an error/loading shell, producing a server `InvalidArgument` toast instead of a disabled
button. Disable it while the target is not loaded (same guard as the run-all button's `summary.ruleCount === 0`), and add the
"button disabled while `reportRunning`" assertion to `DataQualityTargetPage.test.ts` (currently only success/failure paths are
tested).

## Nits

- **N-1** `formatTime` always renders UTC without labelling it (`qualityreport/report.go:850-857`); the window line shows bare
  dates. State UTC once in the scope block (the trend is UTC-bucketed by contract) so a forwarded report is unambiguous.
- **N-2** The engine is printed as the raw enum (`"MYSQL"`, `data_quality_report.go:240`) while everything else is localized.
  Either add engine labels to the catalog or accept and document the raw token.
- **N-3** With a resolvable target that has no rules, the report renders a mostly-empty document ("This target has no rules.");
  decide if a report should exist for an unmonitored target (a `NotFound`/`Precondition` would be clearer than a blank artifact).
- **N-4** `qualityreport/report.go:74-82` says a template edit is a "compile-time failure"; `template.Must` panics at process
  init, not compile time. Reword.
- **N-5** `formatObserved`'s `TIMESTAMP` branch (`labels.go:559-560`) renders `time.Unix(int64(value), 0)`; no rule type sets
  `OBSERVED_KIND_TIMESTAMP` today (only `format.go` maps it), so it is dead code — fine, but note it if the branch is meant as a
  contract.
- **N-6** `report.html.tmpl` has no `meta name="color-scheme"`/dark-mode handling; cosmetic only for a printed/forwarded file.

## Integration notes (not findings against PR2 code)

- The frontend report path imports `downloadBlob`/`fileTimestamp` from `@/utils/csv.ts`, which PR1 adds. PR2 must land **after**
  (or with) PR1; if PR2 lands first the frontend does not compile.
- `frontend/src/locales/*.json` in this worktree also carries PR1/PR3 keys (CSV, digest settings). Do not split that file by
  hunks without coordinating; the i18n gate passed for the combined state but each PR alone must also pass it.
- The report renderer owns its own bilingual catalog (`qualityreport/labels.go`), duplicating terminology that also lives in the
  frontend locales. `.agents/docs/data-quality.md` (or a short comment in `labels.go`) should point future editors at the second
  catalog; the two can drift silently today.

## Verdict

The report is well designed and unusually well tested for its size: one assembled workspace, a pure mapper, a pure renderer with
an embedded `html/template` (auto-escaping is the XSS defense and is tested), hard 20/5/2 MiB caps with an explicit trim banner,
deterministic output, self-containment asserted by marker scanning, both languages, orphaned-target rendering, and a real-server
integration test. The `GetQualityTarget` extraction is behaviour-preserving (verified by line-by-line diff + existing tests).

**No logic blocker.** Two majors gate merge quality: the lint failure (C-1, trivial) and the unbounded sample fetch/copy plus
missing report concurrency bound (M-1, real memory risk because the report is a cheap audited read). M-2 is a one-line
correctness/UX race. The minors are documentation/consistency/test coverage and can be triaged. With C-1, M-1, M-2 fixed and
m-1/m-4 addressed, I would approve PR2.
