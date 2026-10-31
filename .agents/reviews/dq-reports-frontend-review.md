# P0-2 frontend review — exports, report download and digest settings

Reviewer: Phase B frontend review of the client half of P0-2 in
`/home/ran/gocode/metaxisdata/.agents/worktrees/feat-dq-reports-exports` at the final branch tip (`6390138`).
This is the fourth P0-2 review; it closes the frontend checklist that the three per-PR reviews covered only from the backend
side.

## Scope

- `frontend/src/utils/csv.ts` + `csv.test.ts` — the streaming CSV assembler / download helper.
- `frontend/src/utils/apiTimeout.ts` + test — the deadline exemption for `Export*`.
- `frontend/src/api/data-quality.ts` + test — `exportQualityExecutions/Issues/Targets/Report`.
- `frontend/src/components/data-quality/ExportCsvButton.vue` + test.
- `frontend/src/pages/data-quality/{DataQualityExecutionsPage,DataQualityIssuesPage,DataQualityTargetsPage}.vue` + tests —
  filters handed to the export.
- `frontend/src/pages/data-quality/DataQualityTargetPage.vue` + test — report download and gating.
- `frontend/src/pages/settings/DataQualitySettingsPage.vue` + test — weekly-digest card (setting shape only; the backend
  contract is reviewed in the digest review).
- `frontend/src/locales/{en-US,zh-CN}.json`, `frontend/glossary.json` — copy and terminology.

## Gates re-run at HEAD

- `pnpm run biome:check` — 526 files, no fixes.
- `pnpm run lint` — clean.
- `pnpm run i18n` — missing/unused keys, cross-locale consistency, placeholder parity, sorted locales and the glossary rule all
  pass (104 terms / 90 rules).
- `pnpm run type-check` — pass.
- `pnpm run test run` — 158 files / **1598 tests pass** (32.5 s).
- Backend-facing integration evidence: `go test -tags integration ./backend/test/integration/runner/ -run
  'TestDataQuality(Export|Report|TargetAssembly)|TestQualityDigest'` passes (29.9 s, real PostgreSQL + MySQL + a local webhook),
  so the client contract exercised below is the one the server actually serves.

## Verified against the behavior checklist

**Stream handling / bytes / BOM**
- `collectCsvStream` concatenates the chunk `data` byte views verbatim into `BlobPart[]`; it never decodes a slice, so a slice that
  ends in the middle of a UTF-8 sequence is harmless (`csv.ts:76-105`).
- The client adds **no** BOM and **no** separator: the server writes the BOM + header into the first slice and the helper only
  appends (`csv.ts:68-75`), so there is no double BOM and no inserted newline.
- Cheap framing guards (e-8 partial, accepted scope): a stream that delivered zero bytes and a `row_count` that went backwards
  both raise a user-visible export error instead of saving an empty/misordered file (`csv.ts:79-96`; tests
  "rejects a stream that delivered no bytes at all" / "whose row count goes backwards").
- `ApiTimeout` gives every `Export*` method no client deadline (`apiTimeout.ts` `LONG_RUNNING_METHOD` adds `Export`), matching
  the server's own 15-minute bound and the 30-minute stream semantics; the report (unary) is exempt too. Tested.

**Truncation warning**
- `CsvDownload` carries the last slice's cumulative `rowCount` and the OR of the `truncated` flags; the button toasts
  `dataQuality.exportCsvTruncated` ("Exported the first {count} rows; narrow the filters for the rest.") instead of claiming
  success (`ExportCsvButton.vue:66-80`), while a complete export toasts the row count. Both paths tested at the button level and
  in `csv.test.ts`.

**Abort / cancel (E-1 frontend half)**
- The button owns an `AbortController`; `exporter` takes the signal, and `onBeforeUnmount` aborts the in-flight stream
  (`ExportCsvButton.vue:46-86`). A cancelled request is swallowed (`isAborted(error)` check), so navigating away neither toasts an
  error nor writes a file, and the server-side request context is cancelled with it (which releases the export slot).
- The button is disabled while exporting, ignores re-entrant clicks, and renders a spinner + "Exporting…" label.
- Failure path toasts the localized fallback (`dataQuality.exportCsvFailed`), re-enables the button and deletes no partial file
  (the blob is built only after the stream ends).

**Applied filters, not the visible page**
- Executions directory, flat view: state/outcome/trigger from the applied selects, plus the URL-carried `rule`/`target`/`run` chips
  — exactly the list request's filter set (page tests "exports the flat executions under the URL's filters, not one page").
- Executions directory, target view: server `search` + `order`, matching that view's list call.
- Issues directory: applied status/severity.
- Targets directory: `appliedSearch` (the debounced/applied term, not the live input — the e-7 fix), the `failing` toggle sent as
  its own `failingOnly` flag (server-side FAIL-or-ERROR), `hasOpenIssue`, `hasPendingDraft`, `orderBy`. Test asserts the request
  payload exactly, including "the export never sets worstStatus" for failing-only.
- Result: the downloaded file is the whole server selection for the filters on screen, not the current page.

**Report download**
- `exportQualityReport` sends `targetGuid`, `effectiveMetaType`, `trendDays` (default 0 = server's 30 days) and the current UI
  `locale` (`en-US`/`zh-CN`); test pins the exact request.
- The button is gated on the target actually being loaded (`canExportReport`: not loading, no load error, a known `targetGuid`, and
  not already running) — the earlier m-9 "empty GUID → InvalidArgument toast" gap is closed.
- Response handling: `downloadBlob` with `text/html;charset=utf-8`, the server's `fileName` (fallback `quality-report-<local
  timestamp>.html` if it were ever empty), success toast; failure toasts `dataQuality.workspace.exportReportFailed` and downloads
  nothing. Double-click guarded, spinner while rendering.
- The file is downloaded, not opened in a tab, so it remains a forwardable artifact; the report's own self-containment is a
  server-side concern already verified in the report review.

**i18n / copy**
- Every new string is in both locales: `dataQuality.exportCsv*` (6), `dataQuality.workspace.exportReport*`/`reportDownloaded` (4),
  `dataQualitySettings.digest*` (10), `dataQualitySettings.validation.digest*` (5), `notificationSettings.eventDataQualityDigest`,
  and the reworded `eventTypesHint` that documents the digest opt-in exception. The i18n gate (missing/unused keys, cross-locale
  parity, placeholder parity, sorted keys, glossary) passes.
- The digest card is off by default, explains the opt-in delivery and the out-of-band nature, and validates enabled ⇒
  weekday + time + IANA zone on the client while the server re-validates. The event-type hint tells administrators that "nothing
  selected" still does not select the digest.
- `glossary.json` remains consistent (the i18n gate checks 104 terms / 90 rules).

## Findings

**No blockers or majors.**

**f-1 (minor, verification gap) — `audit:ui` (layout budget gate) could not be run in this environment.**
`frontend/scripts/audit-ui-layout.mjs` needs a built SPA + a CDP browser + a logged-in session cookie. The new DQ actions are
buttons in `PageHeader #actions` on exactly the routes with budgets — `/data-quality/targets` 260, `/data-quality/executions`
200, `/data-quality/issues` 240 (`scripts/audit-ui-layout.mjs`) — and the header action row can wrap when several buttons (run all,
export report, edit/new check) are present at the audited viewport. The budgets may therefore have moved by a few pixels even
though the table itself did not change. **This must be run in CI (or locally with the dev server + a browser) before merge; it is
the one gate not verified for this branch.** If it fails, the fix is a budget bump or putting the export/report actions behind the
existing overflow, not a behaviour change.

**f-2 (minor, docs/implementation mismatch) — the downloaded report has no in-file "sensitive: forwarding forwards sample rows"
notice.**
The operator guide tells users to treat the report as sensitive, and `docs/security-posture.md` / `.agents/docs/data-quality.md`
state that the document "carries a visible 'forwarding this file forwards those rows' warning", but the renderer footer only says it
is self-contained (`qualityreport/labels.go` `FooterNote`, en/zh), and the download UI has no additional note. This is the last
open item from the report review's m-3 family: either add one footer sentence to both label catalogs (e.g. "It contains sampled
values from the monitored data; forwarding the file forwards those values." / the zh equivalent) — one line, no proto change,
update the footer assertion — or reword the docs to stop claiming it. Recommend adding the footer line before release.

**f-3 (nit) — `downloadBlob` revokes the object URL synchronously right after `click()`** (`csv.ts`).
This is the shared helper (also used by the audit-log export), and it works in current Chromium/Firefox; Safari and some download
managers can race a synchronous revoke against the download start. Deferring the revoke (`setTimeout(..., 0)` or on the next
task) is the usual fix. Not introduced by this feature; noted so the helper is not "fixed" twice.

**f-4 (nit) — no explicit cancel affordance.** Cancellation happens by navigating away (AbortController on unmount), which is the
accepted UX for a background download; a visible "Cancel" on the button's spinner state would be nicer but is not required and
was explicitly left out to keep the action row simple.

**f-5 (nit) — targets directory: the visible list is itself capped at the first 1000 rows** (pre-existing `useAsync` page size),
while the export covers the whole selection. That is the intended difference ("export everything the filters select"), but the
toast/file can contain rows the reader never saw; the export hint says exactly that. No change requested.

## Verdict

The frontend matches the agreed contract and the checklist: the CSV stream is assembled byte-for-byte (no double BOM, no
mid-UTF-8 decoding), capped files warn instead of lying, the in-flight request is cancelled on navigation without an error toast,
the file name/row count/error states are all handled, each directory exports the **applied** filter set (URL chips included, and
the failing-only toggle is sent as its own server flag), the report download is gated on a loaded target and uses the interface
language, and every new string exists in both locales with the i18n/glossary gates green. 1598 client tests, type-check, lint,
biome and i18n all pass, and the backend integration subset that serves these clients passes against real PostgreSQL/MySQL.

The only material residual is **f-1**: the layout-budget audit (`audit:ui`) could not run here and must gate the merge in CI,
given that new buttons were added to the header action rows of three budgeted routes. **f-2** (report footer sensitivity note vs
docs) is a one-line decision the docs/feature owner should settle in the handover commit. Neither blocks the code.
