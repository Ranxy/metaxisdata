# P0-2 PR3 (weekly digest + outbound delivery claim fix) — adversarial review

Reviewer: Phase B review of the uncommitted PR3 diff in
`/home/ran/gocode/metaxisdata/.agents/worktrees/feat-dq-reports-exports` (branch `feat/dq-reports-exports`).
Base: `HEAD` (= `331db79`). Reviewed the untracked runner/store/notification files plus
`git diff HEAD -- backend/server/server.go backend/store/notification_delivery.go backend/store/notification_delivery_test.go`,
the render digest arm/tests, and the digest integration test.

## What was exercised

- `go build ./backend/...` — pass.
- `go test ./backend/runner/qualitydigestrunner/ ./backend/store/ ./backend/component/notification/...` — pass.
- `gofmt -l` on every PR3 Go file — clean.
- `golangci-lint run ./backend/runner/qualitydigestrunner/... ./backend/component/notification/... ./backend/store/... ./backend/server/...`
  — no PR3 findings (only three pre-existing `multiline-if-init` issues in untouched store files under local v2.14).
- `go vet -tags integration ./backend/test/integration/runner/` — pass (the Docker suite itself was run by the Lead).
- Read `.agents/docs/data-quality.md` D-73 §3.9 and `.agents/docs/notifications.md` O-2/O-4/O-13 against the implementation.

## Blockers

None. The schedule computation, one-period-per-week guard, opt-in fan-out, deterministic event identity, "no inbox row"
property, EMAIL skip, render fallbacks and the outbound claim fix are all correct and tested. The claim fix is correct and
complete (details under "Verified good").

## Majors

**G-1 — The digest's period label is rendered in UTC, so it names the wrong days for time zones east of UTC+9.**
`digestPeriodLabel` formats `period_start`/`period_end` with `.UTC()` (`backend/component/notification/outbound/render.go:117-125`
area) because the detail record does not carry the schedule's time zone. The runner computes the period in the configured
location, so for an Australia/Sydney, Pacific/Auckland or any UTC+10..+14 schedule at a typical morning hour (e.g. Monday 09:00
+11 = Sunday 22:00 UTC) the title/period line becomes `2026-10-19 ~ 2026-10-26` while the digest actually covers
Monday–Sunday local. The dates are the headline of a management artifact, and the rest of the document (counts) is correct,
which makes the discrepancy hard for a reader to notice.
**Fix (digest PR):** carry the schedule's IANA zone in the detail — add `string timezone` (or `string period_label`) to
`storepb.DataQualityDigestDetail`, fill it from `dataquality.Settings.Digest.Timezone` in `digestDetail`, and format the label in
that zone; while there, render an unambiguous window (`2026-10-20 01:00 → 2026-10-27 01:00 UTC` or "week ending 2026-10-27")
instead of two bare dates separated by `~`, which reads as an inclusive 8-day range. Add a render test with a UTC+11 zone.
(Proto is pre-release, so the field addition is free. If the team prefers no proto change, store preformatted local date strings
on the detail instead.)

## Minors

**g-1 — The execution tally's point-in-time boundary is asymmetric (started-at axis, current state).**
`qualityDigestExecutionCountsQuery` selects `state = COMPLETED AND started_at >= since AND started_at < until`
(`backend/store/quality_digest.go:121-126`). An execution that starts just before `until` but is still RUNNING when the digest
aggregates (the runner's first pass is at the scheduled instant/startup, before any settle delay) is excluded from this digest;
the next digest starts at `until`, so it is excluded there too and never counted. Conversely, in a catch-up run hours later the
same predicate counts executions that completed after the period ended, as long as they started inside it. This is a deliberate
consequence of the started-at axis (matching the overview), but the boundary behaviour is not documented.
**Fix (cheap):** delay aggregation of a period by a short settle window (e.g. only handle a period whose `until` is ≥ 10–15
minutes old — the runner's own batch timeout is 15 min), or state in D-73 that executions crossing the period boundary are
attributed to the period they started in and may be missing when still in flight at aggregation time. Either is fine; doing
neither leaves a silent discrepancy.

**g-2 — Coverage counts are "as of aggregation", but the proto comments say "at the end of the period".**
`qualityDigestTargetRuleCountsQuery` counts enabled, non-deleted rules with no time predicate
(`backend/store/quality_digest.go:109-112`); `DataQualityDigestDetail.target_count`/`rule_count` are documented as "in the
period"/"at the end of the period" (`proto/store/store/notification.proto`: "How many targets had at least one enabled check in
the period", "…at the end of the period"). For a timely digest the difference is minutes; for a 23-hour catch-up it can be a
day of rule edits. There is no history to reconstruct the period-end state, so reword the proto comments to "when the digest was
aggregated" (and note the same for the open-issue state, which *is* point-in-time by predicate and is accurately documented).

**g-3 — Persistent aggregation/enqueue failures retry (and WARN) every minute for the whole catch-up window.**
The stale-period path warns once and remembers the period (`handledPeriodKey`), but a failing `QualityDigestSnapshot`,
`EnqueueDataQualityDigest` or `TryRecordQualityDigestSend` leaves the period unhandled, so `pass` retries on every 1-minute tick
for up to 24 hours: ~1440 aggregation attempts + WARN lines per failed week. That is the right retry policy, but it should back
off (e.g. remember the last attempt time and retry at 5–10 minute intervals, resetting on success) or the warn should be
rate-limited. Not a correctness issue.

**g-4 — The snapshot is five independent queries outside a transaction.**
`QualityDigestSnapshot` (`quality_digest.go:218-246`) runs coverage, executions, issue movement/open state, owner and target
queries independently. The issue queries are point-in-time-consistent via the `until` predicate, but the coverage query and the
execution tally read live state, so a concurrent rule delete/create or a just-completed execution can make
`target_count`/`rule_count` disagree with the execution tally's target set inside one document. Weekly, low impact; either wrap
the five reads in a read-only repeatable-read transaction or document the approximation.

**g-5 — Capped owner/target sections do not say that they are capped.**
Both queries cap at 5 (`qualityDigestOwnerLimit`/`qualityDigestTargetLimit`); the renderer prints exactly the rows it got ("Top
owners:", "Top failing targets:") with no "+N more" marker, and the totals above do not break down by owner/target, so a reader
cannot tell whether five named owners are the whole list. The proto comments say "Capped; the totals above carry the whole
picture" — for owners the totals do not actually carry the omitted owners. Add an omitted count to the detail (the store can
return `COUNT(*)` for owners cheaply) and render "(+N more)", or phrase the labels as "Top owners (at most 5)". Low impact.

**g-6 — `owner_name` is not rendered as a fallback identity for the unassigned bucket's `failing_rule_count`.**
`FailingRuleCount` is aggregated and stored on every owner but the renderer only prints `open_issue_count`
(`render.go` `digestOwnerLine`); the field is dead in the current output. Either render it ("N rules") or drop it from the detail
until a consumer exists — proto/store fields are cheaper to add later than to remove, so rendering it (or documenting that it is
reserved for the future report) is the safer choice.

**g-7 — `EnqueueDataQualityDigest` does not validate `periodKey`.**
The store methods validate a blank period key, and the runner always supplies one, but `EnqueueDataQualityDigest` itself accepts
`""` and would create `event_id = "dqdigest:"`. Add the same `strings.TrimSpace(periodKey) != ""` guard (Invalid argument) so the
internal API cannot produce a collision-prone event identity; add a unit case next to `TestEnqueueDataQualityDigestRequiresADetail`.

**g-8 — EMAIL skip treats a non-empty but all-blank address list as "has fixed addresses".**
`digestEmailWithoutAddresses` only checks `len(GetAddresses()) == 0`
(`backend/component/notification/data_quality_digest.go:100-108`). Channel validation should prevent blank addresses, but the
defensive check for the digest (which has no audience to fall back to) should count only non-blank trimmed addresses — otherwise a
misconfigured channel still gets a doomed DEAD delivery every week instead of being skipped. Cheap fix in the same predicate.

**g-9 — Test gaps in the digest suite.**
Unit coverage is strong: schedule slot/walk-back, catch-up boundaries (exact 24 h, ±1 min), the "weekday later in the week" case,
UTC/UTC+14/unknown-zone fallbacks, DST spring-forward/fall-back, weekly period-key advance (incl. the early-Sunday case), pass
orchestration (enqueue→record, recorded skip, zero channels, enqueue failure retry, snapshot/read/record failures, settings
failure), panic containment, severity table, detail mapping, and the warn-once stale test. The integration test proves the whole
path against a real store + dispatcher (opt-in channel receives, other DQ-type channel does not, event id = `dqdigest:<week>`,
severity INFO on a clean week, no link without `external_url`, period start/end, no inbox rows). Missing and worth adding:
1. a second `pass` after a successful send asserts **no** second delivery / no second enqueue (idempotency across ticks with the
   real store, not just the fake),
2. the **EMAIL without fixed addresses** skip and the **routed_only** skip end to end (currently unit-only),
3. a digest period with actual counts (seed one PASS/FAIL execution + one issue) to prove the store SQL end to end — the current
   integration digest is a clean workspace, so `execution_count`/owner/target aggregates are never exercised against PostgreSQL,
4. the catch-up skip path's WARN (unit-tested only).

## Verified good (no action)

- **Outbound delivery claim fix (the actual bug):** `buildClaimNotificationDeliveriesQuery` now returns
  `notificationDeliveryColumnsForClaim`, i.e. the same columns in the same order qualified with the UPDATE target alias `d`
  (`backend/store/notification_delivery.go:24-31, 393`). That removes the PostgreSQL ambiguity between `notification_delivery.id`
  and the `due` CTE's `id` (SQLSTATE 42702) which made every claim fail. Checked: (a) `scanNotificationDelivery`'s positional
  order is unchanged and the new test compares the qualified list with `qualifyColumnList(notificationDeliveryColumns, "d.")`;
  (b) no other store statement has the same ambiguity — every other `RETURNING` belongs to a single-table INSERT/UPDATE (the only
  other UPDATE-with-RETURNING, `RequeueNotificationDelivery`, has no `FROM`); (c) the `SET`/`WHERE` clauses name the target alias
  where needed (`attempts = d.attempts + 1`, `WHERE d.id = due.id`); (d) `CreateNotificationDeliveries` already uses
  `ON CONFLICT (event_id, channel_id) DO NOTHING`, so the runner's documented crash-window retry is genuinely harmless.
- **Schedule:** `mostRecentDigest` walks back at most 8 local days, builds the candidate in the schedule zone, skips candidates
  after `now`, resolves DST-nonexistent wall times to a real instant on the same day and the fall-back ambiguity deterministically,
  and `dueAt` enforces the inclusive 24-hour catch-up window; `time.LoadLocation` failure falls back to UTC (the settings resolver
  already keeps only valid zones). All of this is unit-tested, including UTC+14 and unknown-zone cases.
- **Period identity/idempotency:** key = ISO week of the scheduled instant in its own zone (`isoWeekKey`); it advances weekly, the
  24-hour stale period is skipped with one WARN and remembered; recording is `INSERT … ON CONFLICT (period_key) DO NOTHING`, the
  enqueue uses `event_id = "dqdigest:" + periodKey` so a retry after a partial success hits the delivery unique index instead of
  duplicating, and the runner enqueues first / records second (a crash between the two re-runs the same period harmlessly). The
  migration comment and code comments now state exactly that guarantee (F-6 closed).
- **Opt-in fan-out:** `subscribesDigest` requires the digest type to be named explicitly (empty `event_types` does not select
  it), skips disabled and `routed_only` channels, applies the severity floor, and never consults routing (`ListNotificationRoutes`
  is not called), so a wildcard route or an unreadable routing table cannot leak the digest; an EMAIL channel with no fixed
  address is skipped because the digest has no audience. All pinned by the opt-in unit test and by the "other DQ channel receives
  nothing" integration case. The proto comments in `store/notification.proto` and `notification_service.proto` now document the
  exception (F-2 closed).
- **No inbox rows:** the digest path writes only `notification_delivery` rows (no `Send`/`SendToWorkspaceAdmins` call); the
  integration test asserts `notification` is empty and the delivery list has exactly one row for the subscribed channel.
- **Snapshot semantics:** COMPLETED only; started-at axis matching the overview/trend; `execution_count` is defined and tested as
  the sum of PASS/WARN/FAIL/ERROR (SKIPPED/NOT_EVALUATED deliberately absent, per the updated proto/docs); new issues by
  `first_seen_at` in `[since,until)`, recovered by `resolved_at` in the window; open state is point-in-time at `until`
  (`first_seen_at < until AND (resolved_at IS NULL OR resolved_at >= until)`), same predicate in the owner/target breakdowns;
  owner cap 5 ordered by open issues with the unassigned bucket kept last; target cap 5 ordered by failed then errored then GUID,
  with the newest execution path snapshot and per-target open-issue join. Store tests pin each predicate's shape and the ranking
  helper; the snapshot validates zero/inverted periods before touching the DB.
- **Render:** both languages, period/title, execution/problem/coverage/owner/target lines, empty owner-name fallback to the
  resource id, unassigned label, no owners/targets → sections omitted, no `external_url` → empty link, digest links to
  `/data-quality` (workspace-level), no contact details anywhere (`owner_name` is a display name; F-11 closed), and the outbound
  payload carries no sample values (only counts/paths/display names).
- **Lifecycle:** runner is in the shared `runners` list (so it gets `runner.Go`'s panic boundary, metrics and the shutdown
  `runnerCtx` cancel), `guardPass` contains a panicking pass, and `Run` passes once at startup then on a 1-minute tick (cheap:
  after the period is handled the probe is skipped entirely).
- **Docs:** D-73 / §3.9 and the notifications reference describe the digest schedule, opt-in, idempotency, 5/5 caps and the
  email/no-audience rule; the migration comment matches the implemented guarantee.

## Verdict

This is a high-quality PR: the schedule/idempotency core is correct and unusually well tested (DST, catch-up boundary, retries,
panic, warn-once), the opt-in fan-out cannot leak the digest to legacy channels or routes, the payload carries no contact details
or sample values, and the outbound claim fix is correct, complete and regression-tested (the column-order invariant is pinned).
It also fixes the pre-existing bug that made outbound delivery completely non-functional, which unblocks PR1's audit/export
integration assertions. **No blocker.**

G-1 (UTC period label misnames the covered days for UTC+10..+14 schedules) is the only user-visible correctness issue and should
be fixed before release (cheap: carry the schedule zone or a preformatted period label in the detail). The remaining items are
documentation/precision/robustness (g-1 boundary asymmetry, g-2 "at aggregation" wording, g-3 retry backoff, g-4 non-transactional
snapshot, g-5 cap marker, g-7 period-key guard, g-8 blank-address defense) and test additions (g-9), all of which can be
triaged without blocking. With G-1 fixed (or explicitly deferred) I would approve PR3.
