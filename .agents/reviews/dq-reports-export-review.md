# P0-2 PR1 (streamed CSV exports + streaming audit) — adversarial review

Reviewer: Phase B review of the uncommitted PR1 diff in
`/home/ran/gocode/metaxisdata/.agents/worktrees/feat-dq-reports-exports` (branch `feat/dq-reports-exports`).
Base: `HEAD` (= `1bbaddf`). Reviewed `git diff HEAD -- <paths>` plus the untracked
`data_quality_export_test.go`, `component/audit/stream.go`/`stream_test.go`, `ExportCsvButton.vue`/test and the export
integration test.

## What was exercised

- Read: the three export handlers and the shared streaming writer, the store `WorstStatuses` addition, the streaming audit
  interceptor + `StreamRecorder`, both unit-test files, the export integration test, and the frontend CSV/api/timeout/button/page
  diffs.
- `go build ./backend/...` — pass. `go test ./backend/api/v1/ -run '…export/audit/stream/target…'` — pass.
  `go test ./backend/component/audit ./backend/store` — pass.
- `gofmt -l` on every PR1 Go file — clean.
- `golangci-lint run ./backend/component/audit/... ./backend/api/v1/...` — no PR1 findings (only the two pre-existing
  `multiline-if-init` issues that local v2.14 reports on untouched files).
- `go vet -tags integration ./backend/test/integration/runner/` — pass (the suite itself was run by the Lead with Docker; not
  re-run here).
- Frontend: `pnpm --dir frontend test run` for `csv`, `apiTimeout`, `ExportCsvButton` and the three list pages — 6 files /
  64 tests pass. `pnpm run type-check` and `pnpm run i18n` (missing/unused/sort/glossary) pass.

## Blockers

None. The export contract is implemented correctly at the levels this PR controls (paging, chunk framing, row counts, filter
parity, audit capture) and the existing gates are green.

## Majors

**E-1 — Export streams have no server-side lifetime bound, and the client cannot cancel; a hung stream can occupy all four
slots indefinitely.**
`DeadlineInterceptor.WrapStreamingHandler` deliberately gives streams no deadline (`backend/api/v1/deadline_interceptor.go:70-76`)
and `frontend/src/utils/apiTimeout.ts` deliberately gives `Export*` no client deadline. An export is then only bounded by the
client keeping the connection open. The process has four export slots total (`backend/api/v1/data_quality_service.go:108-146`,
non-blocking `ResourceExhausted`); a page query against the metadata DB that stalls (no statement timeout is set on export reads —
the store queries run on the request context) keeps holding its slot. Four such streams (or four clients that open an export and
walk away without closing the tab) disable the whole export/report feature until the process restarts, because the slot helper
never queues and never reaps.
Additionally `ExportCsvButton.vue`/the pages pass no `AbortSignal`, so a user has no cancel affordance — only closing the tab
aborts the fetch.
**Fix:** wrap the handler context in a bounded deadline (e.g. `context.WithTimeout(ctx, 15*time.Minute)`; the cap is ~100 pages,
so a normal export is far under it) inside `writeQualityCSVExport`/the three handlers, map the expiry to `DeadlineExceeded`, and
add a WARN log with the procedure; alternatively/additionally add a per-slot lease reaper in the service. On the frontend, create
an `AbortController` per export and abort it on unmount (and expose a cancel path if the UX allows), which also frees the server
slot via the request context. Add an integration/unit test for the expiry path.

**E-2 — Spreadsheet formula injection is undecided for a file explicitly built for spreadsheets.**
The exports write a UTF-8 BOM so Excel detects the encoding (`data_quality_export.go:41-43, 390-401`) and carry text that is
user- or external-system-controlled: rule titles, target paths/columns synced from monitored databases, error messages, issue
resolution notes. `appendCSVRecord` (`:460-471`) only relies on `encoding/csv` quoting, which does not stop a cell beginning
`=`, `+`, `-`, `@`, TAB or CR from being evaluated as a formula when the file is opened in Excel/Sheets/LibreOffice. A table
named `=HYPERLINK(...)` in a monitored database can therefore become an active formula in an administrator's download.
The repository already has this exposure in the client-side audit-log CSV (`frontend/src/utils/csv.ts` `csvEscape`), so this may
be an accepted posture — but this PR creates the first **server-side** bulk export and should make the decision explicit.
**Fix (choose one):** neutralize formula-leading cells for the free-text/identity columns only (prefix `'` and document that
machine consumers should strip it; do **not** touch numeric columns such as `observed_value`, where `-` is legitimate), with a
unit test; or add an accepted-risk entry to `docs/security-posture.md` (and §3.9 of `.agents/docs/data-quality.md`) stating that
downloaded CSVs are data for spreadsheet use and must not be opened from untrusted deployments. Either way it should not be left
implicit.

## Minors

**e-1 — A user-cancelled export writes an ERROR-severity audit row.**
`audit.MapSeverity` maps every non-`*connect.Error` to `ERROR` (`backend/component/audit/audit.go:339-353`); a client disconnect
usually surfaces from `stream.Send` as a context/EOF error, and `audit_test` does not pin cancellation. A large download that a
user closes therefore adds an error row to the permanent ledger per attempt. Map `context.Canceled`/`io.EOF`-shaped errors to
`WARNING` (or `INFO` with a `status.message` of "client cancelled") and add a test; keep real failures as `ERROR`.

**e-2 — The audit `resource` column is the actor for every export.**
The recorded request has `parent` + filters but no `name` (`ResolveResource`, `audit.go:279-292`), so the fallback at
`backend/api/v1/audit.go:97-100` stores the user as `resource`. The row is still diagnosable from its request payload, but an
audit search by resource cannot find exports. Add `targetGuid`/`rule` fallbacks for the export procedures (or set `Resource`
explicitly from the recorder) and pin the expected resource in the integration test. (Carry-over of foundation F-20; the report
RPC has the same shape.)

**e-3 — The targets export re-resolves registry state per page, including an N+1 database lookup.**
`enrichQualityTargetsForExport` reuses `targetOrphanReasons`, whose per-page body loops over distinct `(instance, database)` pairs
and calls `store.GetDatabase` once per pair (`backend/api/v1/data_quality_service.go:1531-1541`). One page of 1000 targets can
issue up to ~1000 such queries, and a full 100k-row export repeats that for every page (~100 pages). The list RPC has the same
per-page cost, but the export multiplies it. Batch the database lookups (`FindDatabaseMessage` by names) and/or cache resolutions
by `MetaGUIDKey` across pages in the export; no correctness change.

**e-4 — Issue-filter resolution duplicates the ListQualityIssues logic.**
`ExportQualityIssues` re-implements rule and owner resolution (`data_quality_export.go:103-128`) that `ListQualityIssues` already
performs (`data_quality_service.go:1684-1706`). The execution export avoids this by projecting the request onto the list request;
do the same by extracting a `resolveQualityIssueFilters(ctx, find, rule, owner)` helper, so a change to owner semantics (e.g.
group-owned rules) cannot drift between the page and the file. Related nits: `qualityIssueListRequestFromExport` still sets
`rule`/`owner` even though `applyQualityIssueListFilters` ignores them, and `qualityTargetListRequestFromExport` sets
`worst_status`/`has_open_issue`/`has_pending_draft`/`order_by` even though `applyQualityTargetListFilters` ignores them — harmless
today, misleading to the next reader.

**e-5 — Test gaps in the export suite.**
Unit coverage is good (BOM/header/cumulative rows, split-record counting, short page, ceiling truncation incl. exact-cap vs cap+1,
CSV field formatting, filter-parity reflection, slot mapping, store `WorstStatuses`, stream recorder). The integration test covers
executions + targets + failing-only + invalid combination + truncation + the audit row; it does **not** cover:
1. the **issues** export end to end (filters, rule/owner empty-match, header-only output),
2. an **empty selection** (header-only document, `row_count = 0`, `truncated = false`),
3. a **cancelled/failed** stream's audit row (result summary recorded, severity),
4. a **member** token (permission gate) rather than the admin token,
5. a **time-window** boundary (`start_time` inclusive / `end_time` exclusive) through the export path — inherited by projection but
   not proven end to end.
Add at least (1), (2) and (4); they are cheap with the existing harness.

**e-6 — The filter-parity guard compares names only.**
`TestQualityExportFiltersMirrorTheListFilters` (`data_quality_export_test.go:455-509`) checks that every list field name exists on
the export and vice versa, but not that the type/kind matches. A future change from e.g. `int32` to `int64` on one side would
pass. Compare `Kind()` (and `Message().FullName()` for message fields) too.

**e-7 — Frontend has no abort/cancel and the targets page can export the un-applied search text.**
`ExportCsvButton.vue` owns no `AbortController`, and the three pages pass no signal, so navigating away leaves the server stream
open until the transport closes (E-1). On `DataQualityTargetsPage.vue`, the export reads `search.value` (the live input) while the
table's server query is debounced — a click immediately after typing exports a filter the visible table has not applied yet. Use
the applied/debounced value for export, and abort the stream in `onBeforeUnmount`.

**e-8 — The client does not validate the framing it rebuilds.**
`collectCsvStream` (`frontend/src/utils/csv.ts:66-88`) concatenates slices verbatim (correct) and trusts the last `rowCount`; it
never checks that the first bytes are the BOM, that `rowCount` is monotonic, or that the assembled file is non-trivial. If the
server contract regresses, the user gets a silently broken download. Add cheap assertions (BOM prefix on the first slice,
non-decreasing `rowCount`, non-empty document, optional absolute byte cap) and surface the existing generic export error.

**e-9 — The public proto comments do not state the snapshot/partial-file semantics.**
D-73 in `.agents/docs/data-quality.md` now documents "not a snapshot" and the 100k/1k-page contract, but the RPC comments
(`proto/v1/v1/data_quality_service.proto:126-137, 169-180, 360-371`) say only "stops at max_rows". Add: rows written while the
export pages may fall on either side of a page boundary; a stream error means the file is incomplete and must be discarded (the
frontend already discards it); the ceiling's `truncated` flag is on the **final** slice only.

## Nits

- **n-1** `qualityExportEnum(target.Type.String(), "")` works because of the empty-prefix special case; add a one-line comment or
  a `qualityExportBareEnum` helper so the next reader does not "fix" the empty prefix.
- **n-2** The execution CSV column `truncated` (the execution's sample truncation) and the chunk field `truncated` (row-ceiling
  cut) share a word with different meanings; document the column in `qualityExecutionExportHeader`'s comment.
- **n-3** The targets CSV omits `pending_label_count` although `QualityTargetSummary` carries it; choose deliberately and say so
  in the header comment.
- **n-4** The ceiling check issues one extra one-row list query when the cap is reached exactly; that is correct and cheap, but
  say so in `streamQualityCSV`'s comment so it is not removed as redundant.
- **n-5** `ExportQualityTargets` calls `validateQualityTargetExportFilters` before resolving the workspace; a contradictory filter
  from an invalid parent returns `InvalidArgument` rather than the workspace error. Fine, but if error precedence matters (it can
  leak filter semantics to a caller who cannot address the workspace) validate after `workspaceID`. Low priority because the
  permission/ACL interceptor runs first.

## Verified good (no action)

- **Filter parity:** the executions export projects its request onto `ListQualityExecutionsRequest` and reuses
  `applyQualityExecutionListFilters`, so every filter, the resource-name resolution and the time-window inclusivity are inherited
  from the list (not re-implemented). Issues status/severity/target-guid/time filters and targets instance-path/search filters map
  identically to their list handlers. `failing_only` is export-only, documented, rejected together with `worst_status` before any
  chunk is written, and the unit reflection test pins the list↔export field sets in both directions.
- **Paging:** page size 1000, ceiling 100k, `max_rows <= 0` → default, `> cap` → cap; the loop stops on a short page or the ceiling,
  never splits a record, and the ceiling is enforced by the loop (the test deliberately returns more rows than requested). The
  exact-cap vs cap+1 distinction is made with a one-row probe; only the final slice carries `truncated`; a page that is cut short
  by the ceiling is flagged. Order clauses in the store all have stable tiebreakers (`started_at,id` / `first_seen_at,id` /
  object+guid), so OFFSET paging inside one export is deterministic for a stable table.
- **Chunk contract:** BOM+header first, slices ≤256 KiB, the writer keeps a tail so the final slice exists even when everything
  has flushed, `row_count` counts only fully contained rows and is cumulative/monotonic and ends at the export's row count; a
  long record split across slices is counted exactly once (unit-tested). `encoding/csv` handles embedded quotes, commas, newlines
  and multi-byte UTF-8; a chunk may split a UTF-8 sequence and the client concatenates bytes (never decodes).
- **Store change:** `WorstStatuses` is additive; the slice replaces the single-status predicate; the single-status predicate and
  its argument shape are unchanged (test), ranks come from the same `QualityTargetStatusRank` used by the directory, and the
  `pq.Array([]{FAIL,ERROR})` predicate is exercised by the integration failing-only case.
- **Streaming audit:** unary path untouched (diff); `recordingStreamingHandlerConn` only overrides `Receive`, so request frames
  are cloned into the recorder and response frames/LLM content are never captured; the handler reports `exportedRows`/`maxRows` via
  `RecordStreamResult` even when the stream ends in error; `MarshalAuditResult` applies the same redaction/field/payload bounds as a
  unary response; the integration test proves the real server writes a stream row with the request filters and the row summary, and
  `ExplainQualityExecution` now gets a request payload (name/language/force only — no LLM content) without any behaviour change.
- **No source values:** none of the three CSV schemas carry failure samples, sample values or executed SQL; only execution
  aggregates, issue metadata, and target aggregates.
- **Frontend:** slices are concatenated as bytes with no client BOM or separator, `truncated` produces a warning (not a success),
  row count is localized via `formatRowCount`, failures toast and re-enable the button, the button is disabled while streaming,
  the three pages export the **applied** filters (executions: state/outcome/trigger + URL rule/target/run chips; issues:
  status/severity; targets: search/toggles/order with `failing` sent as its own flag), and `Export*` is exempted from the client
  deadline. i18n keys exist in both locales and the glossary/i18n gates pass.
- **Docs:** `.agents/docs/data-quality.md` D-73 + §3.6/§3.9 already record the export/report decisions, the audited-read
  deviation, the 4-export bound and the "not a snapshot" caveat.

## Verdict

This is a solid, well-tested PR. The export wire contract (paging, chunk framing, row counting, truncation signalling, CSV
escaping) is correct and pinned by unit tests that exercise the awkward boundaries, the filter surface genuinely cannot drift
from the list RPCs for executions, and the streaming audit change is minimal, well-bounded and proven against the real server
(the unary audit path and `ExplainQualityExecution` are unaffected). No correctness blocker.

Two decisions gate merge quality: bound the export stream's lifetime (E-1 — otherwise four stuck streams can disable the feature
process-wide and the UI has no cancel), and settle the spreadsheet-formula posture explicitly (E-2 — sanitize text columns or
record the accepted risk). The minors are test coverage (issues export, empty export, member token, cancel audit), audit resource
and cancel severity, the target-export enrichment amplification, a duplicated issue-filter resolver, and frontend abort/applied
filter polish. With E-1/E-2 addressed and e-1/e-5/e-7 folded in, I would approve PR1.
