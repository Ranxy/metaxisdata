# P0-2 foundation review — `bd7c87e` (protocol surface) + `932f7e8` (settings tests)

Reviewer: adversarial review of the shared foundation for P0-2 (CSV exports, per-target HTML report, weekly digest).
Repo: `/home/ran/gocode/metaxisdata/.agents/worktrees/feat-dq-reports-exports`, branch `feat/dq-reports-exports`.

## Scope and method

- Read: root `AGENTS.md`, `backend/AGENTS.md`, `proto/AGENTS.md`, `backend/migrator/AGENTS.md`,
  `.agents/docs/data-quality.md`, `.agents/docs/notifications.md`, `docs/security-posture.md`,
  `.agents/reviews/data-quality-competitive-gap-analysis.md` (§P0-2, §5), and `.agents/docs/rate-limiting.md`-relevant code.
- Reviewed the two commits only (`git show bd7c87e`, `git show 932f7e8`), plus the pre-existing code the new contract depends on
  (list RPCs, notification fan-out/render/dispatch, audit interceptor and `component/audit`, store unmarshaling, migrator).
- Note: the worktree carries **uncommitted in-flight PR work** (export/report/digest/frontend, incl.
  `backend/component/audit/stream.go`, `backend/component/notification/data_quality_digest.go`,
  `backend/component/qualityreport/`, `backend/runner/qualitydigestrunner/`, modified `data_quality_export.go`).
  Per the task, Phase A did **not** review that work; it is only used to confirm which cross-PR dependencies exist.
- Verified in an isolated export of `HEAD` (`/tmp/dq-foundation`, `git archive`):
  - `go build ./backend/...` — pass.
  - `go test ./backend/api/v1/ ./backend/component/audit/ ./backend/component/notification/... ./backend/store/ ./backend/component/dataquality/ ./backend/migrator/` — pass.
  - `buf lint` + `buf format -d` — clean. `gofmt` — clean.
  - `golangci-lint run` (local v2.14.0) — **3 new findings, all in the new stub file** (see F-7); base commit has 3 unrelated
    pre-existing revive findings under v2.14, so the stub findings are attributable to this commit.
  - Ad-hoc probe of `audit.MarshalAuditMessage(ExportQualityReportResponse)` and of protojson unknown-enum behavior (F-1, V-4).

## Verified good (no action)

- **V-1 Proto field numbers / oneof.** The store `NotificationDeliveryEvent` oneof arm `data_quality_digest = 7`
  (`proto/store/store/notification.proto:385`) is correct: field 6 is `audience_user_ids` (`:392`, outside the oneof), so 7 is the
  next free number and no reserved slot was skipped. `NOTIFICATION_TYPE_DATA_QUALITY_DIGEST = 4` is the next free value in both
  the store (`:24`) and v1 (`proto/v1/v1/notification_service.proto:229`) enums; additions are wire- and JSON-compatible.
- **V-2 Export filter parity.** Field-by-field the three export requests mirror the list requests, with only `page_size`/
  `page_token` removed and `max_rows` added: executions `state/outcome/trigger/rule/target_guid/instance/initiator/start_time/
  end_time/run` (`data_quality_service.proto:2743-2779` vs `:1760-1804`); issues
  `status/severity/rule/target_guid/owner/start_time/end_time` (`:2781-2809` vs `:1819-1848`); targets
  `instance/database/schema/table/search/worst_status/has_open_issue/has_pending_draft/order_by` (`:2811-2843` vs `:2412-2444`).
  Time-window inclusivity wording matches the lists (executions `end_time` exclusive, issues `end_time` inclusive).
- **V-3 Migration/LATEST.** `0035##quality_digest_send.sql` and the `LATEST.sql` block (`:1380-1392`) are byte-identical DDL,
  idempotent (`IF NOT EXISTS`), one incremental for the current `0.1` line; the migrator derives the latest version from the
  file tree (no constant to bump). No workspace column is correct: no `quality_*` table has one (verified 0 occurrences), matching
  the single-tenant schema. `TEXT COLLATE "C"` matches the other quality tables.
- **V-4 Stored-JSONB compatibility.** `common.ProtojsonUnmarshaler` is `DiscardUnknown: true`; an unknown enum *name* in a stored
  `notification_delivery.payload` (e.g. after a rollback) unmarshals to `UNSPECIFIED` without error (tested), and an unknown
  oneof arm is discarded. Rollback therefore does not crash the dispatcher; it degrades to a fallback title. Adding
  `weekly_digest` to the stored `DataQualitySetting` JSONB is likewise rollback-safe.
- **V-5 Settings contract.** Zero means default is respected: absent/empty digest resolves to disabled Monday 09:00 UTC
  (`settings.go:226-243`); an enabled schedule requires weekday/time/zone and a disabled one may carry partial *valid* values
  (`settings.go:264-295`); stored garbage is resolved leniently rather than failing the runner (`settings.go:240-262`).
  v1 and store `DayOfWeek` have identical numbering (both ISO, `MONDAY = 1`), so the numeric casts are currently correct.
  The `DataQualitySetting` JSONB column comment stays accurate (same message name, new field only).
- **V-6 REST/gateway.** The three exports intentionally have no `google.api.http` (gateway cannot proxy server streams) and are
  reached over Connect like `ExplainSQL`; the report's `GET /v1/{parent=workspaces/*}/qualityTargets:exportReport` does not
  collide with `qualityTargets` (list), `qualityTargets:get`, or `qualityTargets:getSchedule`; `buf lint`, generated gateway,
  OpenAPI, Go + proto-es output are all committed consistently.
- **V-7 Permissions.** Exports use the matching `*.list` permission and the report uses `rules.list`; read-only members already
  hold all three, and every datum the report adds (unresolved issues, latest results, failure-sample summary) is already
  readable at `issues.list` / `executions.list`, so the report grants no new read scope.
- **V-8 Report language/trend fields.** `trend_days` capped at the 90-day execution retention is consistent with the
  maintenance window; `language` matches the existing DQ/ExplainSQL convention.

## Findings

### Blocker

**F-1 — The report's HTML response is copied into the permanent audit ledger.**
`proto/v1/v1/data_quality_service.proto:386-391` marks `ExportQualityReport` `audit = true`, and its response carries the whole
document as `string content = 2` (`:2871`). The unary audit path marshals the **response** into `audit_log.response`
(`backend/api/v1/audit.go:115-131` at HEAD; `MarshalAuditMessage` at `backend/component/audit/audit.go:73`, per-field bound
8 KiB at `:36`, per-payload bound 256 KiB at `:43`). Measured: a report response with 20 KB of HTML produced an audit payload
whose `content` field was 8,206 bytes.
Consequence: every report call writes ~8 KiB of report HTML into a row that is **never pruned**
(`docs/security-posture.md:21`), and the report explicitly contains a "capped sample summary" — i.e. data derived from failed-row
samples. That violates the stated audit invariant "审计行只含请求元数据，不含 LLM 正文/样本"
(`.agents/docs/data-quality.md:271`) and the ledger's redaction discipline (the LLM-debug bodies are redacted for exactly this
reason, `audit.go:240-260`). It is also a visible UI problem: admins read `audit_log.response` back.
Fix (report PR, choose one):
1. add a method-scoped response-field suppression in `marshalAuditMessage`/`createAuditLog` for
   `DataQualityService.ExportQualityReport` that replaces `content` with `[REDACTED]` (preferred: no proto change, no effect on
   other methods), and add a unit test asserting the stored response contains no `content`; or
2. rename the response field to a name on `IsSensitiveAuditField` (e.g. `html`) and add it to the redaction list, keeping the
   generic list honest with a test; or
3. return the report over a streaming RPC, whose audit path (after the recorder lands) stores only a handler-supplied summary.
Whichever is chosen, pin it with a test: `AuditResponseContainsNoReportHTML`.

### Major

**F-2 — Digest fan-out contradicts the documented "empty event_types = every type" filter.**
The foundation adds `NOTIFICATION_TYPE_DATA_QUALITY_DIGEST` but leaves the channel/filter contract saying empty means all:
`proto/store/store/notification.proto:292` and `:411`, `proto/v1/v1/notification_service.proto:526` and `:768`. The runtime filter
implements that literally (`backend/component/notification/outbound_queue.go:77-96`), routing has the same wildcard
(`outbound_routing.go:97-110`), and routes are additive (`routed_only` channels are added after `subscribes`). The new enum's own
comment ("only the outbound channels subscribed to this type receive it", store `:22-24`, v1 `:226-229`) is not enforceable by
the existing fan-out.
If the digest PR reuses `Send`/`enqueueOutbound` (or any helper that shares `subscribes`), **every existing channel created before
this release (empty `event_types`) starts receiving weekly digests**, which is exactly what the plan forbids.
Requirements the digest PR must enforce (and test):
1. Digest delivery is **explicit opt-in**: only channels whose `event_types` literally contains
   `NOTIFICATION_TYPE_DATA_QUALITY_DIGEST` (never empty-means-all). Do not go through `Send`/`enqueueOutbound`; the dedicated
   enqueue must use its own predicate.
2. Routing must not add digest to channels: a rule with empty `event_types`/owners/groups is a wildcard at enqueue
   (`outbound_routing.go:97`) and would otherwise select digest events for `routed_only` channels. Skip routing for digest events,
   or document and test that routes cannot select them (`validateNotificationRouteMatch` currently rejects type 4 outright,
   `backend/api/v1/notification_route_service.go:176-185` — keep that and say why, or add type 4 deliberately).
3. Update the two proto comments above so the contract reads "empty means every type **except** the digest, which must be named
   explicitly", in both store and v1, and mirror it in `.agents/docs/notifications.md` (O-2).
4. Add a unit test with an empty-`event_types` channel, an enabled channel subscribed to DQ only, and a wildcard route; assert
   only the explicit digest subscriber receives it.

**F-3 — EMAIL channels with no fixed addresses become instant-DEAD deliveries for every digest.**
Digest events have no audience (`NotificationDeliveryEvent.audience_user_ids = 6`, store `:386-392`, and the digest is
workspace-level), while an EMAIL channel resolves recipients as fixed addresses ∪ audience and **immediately DEADs** when the
union is empty (`backend/component/notification/outbound/recipients.go:26-60`,
`backend/runner/notificationdispatcher/notificationdispatcher.go:246-252`, decision O-13). A common configuration — an EMAIL
channel with `include_audience = true` and no fixed addresses, used for per-owner schema-sync/DQ alerts — would either be
silently excluded (if the digest PR filters on fixed addresses) or produce one permanently-DEAD row per week per channel that an
admin cannot fix except by adding an address to a channel that intentionally has none.
Requirement: decide and enforce at enqueue time — (a) set the digest event's audience to the workspace administrators when the
channel uses `include_audience` (digest is workspace-level, so admins are the natural audience; this also makes the "who receives"
story consistent with `SendToWorkspaceAdmins`), or (b) skip EMAIL channels whose resolved fixed-address list is empty and record
that in `channel_count`/metrics rather than queueing a doomed delivery. Document the choice in `docs/notifications.md`; add a
dispatcher/enqueue test.

**F-4 — CSV exports silently truncate at `max_rows` and the client cannot tell.**
`ExportQualityDataChunk` (`data_quality_service.proto:2730-2739`) carries `bytes data` and cumulative `int64 row_count` only.
When the row cap is hit, the last chunk is indistinguishable from an export that happened to contain exactly `max_rows` rows, and
the CSV itself contains no marker. For an artifact used in weekly reports/acceptance/audit, a silently incomplete export is a
correctness bug. The proto comment ("stops at max_rows") documents the behaviour but not that it is detectable.
Fix (export PR, before release — proto is new so changing it is free): add `bool truncated = 3` (set on the final chunk when the
cap was hit) or a final summary field, and surface it in the client ("export truncated at 100,000 rows"); test the boundary
(0 rows, exactly cap, cap+1). Minimum alternative if the field is rejected: have the handler append a clearly labelled final
pseudo-row — not recommended, it corrupts machine-readable output.

**F-5 — `update_mask = "data_quality"` wipes `weekly_digest` for any client predating the field.**
`applyWorkspaceProfileUpdate` replaces the whole `DataQualitySetting` for that mask
(`backend/api/v1/setting_service.go:139-160`); `normalizeDataQualitySetting` stores `WeeklyDigest: nil` when the request omits it
(`:204-228`). So a save from a stale SPA tab (or an older API client / script) that round-trips the DQ limits without knowing
about `weekly_digest` silently disables a configured digest schedule — the same failure class "whole-message mask" creates for
every new field, but here it silently stops an alerting feature. The settings page is served from the same binary, but open tabs
and non-SPA clients exist.
Fix (settings/digest PR, pick one): (a) accept `data_quality.weekly_digest` (and `data_quality.weekly_digest.*`) as granular
mask paths like quiet hours does (`setting_service.go:703-780`), and merge rather than replace when the message is absent;
(b) keep the whole-message semantics but explicitly preserve a stored digest when the update's `weekly_digest` is absent and the
mask is `data_quality` (document that clearing requires `weekly_digest` to be present-and-disabled); or (c) at minimum document
the behaviour in `.agents/docs/data-quality.md` §4.1 and add a test pinning it. Recommend (a) or (b); (c) alone leaves a silent
alerting loss.

**F-6 — `quality_digest_send` records a period before the deliveries exist, so the migration's no-duplicate claim is not
guaranteed.**
The DDL comment (`backend/migrator/migration/0.1/0035##quality_digest_send.sql:1-4`) states a second tick/restart/catch-up
"cannot queue the same week's digest twice". The schema only guarantees **one period row**; it cannot guarantee one enqueue,
because the row insert and the `notification_delivery` inserts are separate writes. If the runner records the period after a
successful enqueue (as `backend/runner/qualitydigestrunner/qualitydigestrunner.go` describes in the working tree), a crash
between the delivery insert and the period insert re-enqueues the week with a new random `event_id` → duplicate digest; if it
records first and then crashes, the week is skipped. Either way the comment overpromises.
Fix (digest PR): make "record the period + create the deliveries" one DB transaction (or insert the period row first with
`ON CONFLICT DO NOTHING` and treat conflict as "already sent to some channels", then create deliveries for the returned row), and
either way reword the migration comment to state the actual at-most-once/at-least-once choice. Add a store test that a second
record attempt is a no-op (`store/quality_digest_test.go`-style guard already exists for the SQL shape; add the failure-ordering
case). If the period is recorded first, also decide what `channel_count = 0` means for a week where enqueue failed (currently
it means "no channel subscribed").

**F-7 — New stubs fail the lint gate.**
`backend/api/v1/data_quality_export.go:16,22,28` (at `bd7c87e`): all three stub methods have an unused receiver; `revive`
`unused-receiver` flags them (reproduced with golangci-lint v2.14.0; CI pins v2.13.1 — verify, but the fix is free). The stubs are
replaced by the export PR, so this is only a gate problem if the foundation lands as its own PR.
Fix: `func (*DataQualityService) Export...` in the stubs, or fold the foundation into the export PR (the working tree already
uses the receiver). Also confirm `golangci-lint run --allow-parallel-runners` is clean on the final branch tip.

**F-8 — Bulk reads are now audited, reversing a documented policy, and the consequence is not recorded.**
All four new RPCs are `audit = true` (`data_quality_service.proto:134/176/367/388`). Before this change the rule was "the
ConnectRPC read baseline is not audited" (`docs/security-posture.md:21`) and "只读列表/详情…不审计"
(`.agents/docs/data-quality.md:271`). Two consequences must be made deliberate in docs (the PRs must land them together, one owner):
1. Every export/report call writes a permanent ledger row (bounded by the 8 KiB/256 KiB payload caps, but kept forever), so a
   member can add rows simply by looping a cheap call. The audited-read policy exists to avoid exactly this; auditing a *bulk*
   read is defensible, but must be listed as a deliberate deviation in `docs/security-posture.md` with the rationale (bulk quality
   evidence) and its bound (principal budget).
2. Audited methods consume the **shared** deployment ledger budget: `(principal, procedure)` 3000/min and 30,000/min global
   (`backend/api/v1/principal_throttle_interceptor.go:39-140`, `docs/security-posture.md:27-29`). A member looping exports can
   therefore spend the deployment-wide budget and get other principals' audited calls refused for the window. This is an accepted
   property for audited writes, but it is newly reachable through a *read*; note it in the same doc entry.
   Consider a dedicated lower rate/concurrency limit for exports (see F-9) instead of relying on the shared counter alone.

**F-9 — No bound on concurrent exports; an export holds a metadata-DB connection for its whole stream.**
Each export is one stream: one row cursor over the metadata DB for up to 100,000 rows, potentially for seconds. Nothing in the
contract or the throttle limits concurrency — the principal budget (3000/min, and it is checked at stream start only) allows a
single member to open hundreds of streams that each hold a store connection until drained. That contends with every API read and
runner on the shared pool (`backend/store/db_connection.go`, pool sized from the server connection budget). The three exports are
member-level permissions, so any read-only member can do this.
Fix (export PR): add a process-wide (or per-principal) in-flight semaphore for exports/reports — e.g. 1–2 concurrent exports per
principal and ~8 per process, refusing with `ResourceExhausted` — and document it in `.agents/docs/data-quality.md` §3.6,
alongside the existing "队列满 → ResourceExhausted" precedent. Also check `ctx` cancellation per chunk and close the cursor on
client disconnect.

**F-10 — Report size is unbounded by contract.**
`ExportQualityReportResponse.content` is a single unary string (`:2865-2872`); the server buffers the whole HTML, the Connect
response has no configured send cap (only request caps exist, `backend/server/grpc_routes.go:43-53,173`), and the report includes
up to 100 rules, 90 days of trend, and rule history for a target. A pathological target (100 rules × 90 days at a 15-minute
interval) could produce a multi-megabyte document rendered per request, on top of the audit copy from F-1.
Requirement (report PR): enforce a hard size/row cap in the renderer (e.g. truncate the trend to a fixed bucket count, cap listed
rules/failed checks/samples and state the omission in the document), return `InvalidArgument`/`ResourceExhausted` above the cap
rather than an OOM, and add a test for the cap. Document the expected ceiling in the RPC comment and `.agents/docs/data-quality.md`.

**F-11 — `owner_email` in the digest detail violates the outbox "no addresses" contract and leaks addresses to external
destinations.**
`proto/store/store/notification.proto:246-254` stores `owner_email` in `DataQualityDigestOwner`, which becomes part of
`notification_delivery.payload` — a workspace-level outbox row rendered to webhooks/IM robots/email. Decision O-4 says the outbox
"不存收件人的姓名/地址 … 邮箱地址在发送时才解析" precisely so a changed/deleted account resolves at send time
(`.agents/docs/notifications.md:308`). The digest payload now carries a snapshot address that (a) goes stale, (b) is rendered into
third-party webhook/chat requests, and (c) is copied into audit/delivery diagnostics. The comment "so the message is readable
without another read" does not outweigh the documented invariant.
Fix (digest PR): drop `owner_email` (render `owner` + a display name resolved at send time like the email recipient path does), or
replace with a non-address display field. If the address is genuinely required to identify an owner for readers, store the user
id/resource only and resolve in the dispatcher; update O-4 if a deliberate exception is wanted.

**F-12 — Docs are not updated with the new surface.**
Foundation adds 4 RPCs, 1 notification type, 1 stored setting field, 1 table — and touches no documentation. Repo rules require
the maintenance reference to describe a subsystem-scale feature and the RPC count/invariants to stay true
(`.agents/docs/data-quality.md:263` says "共 42 个 RPC" — now 46; §3.6 says reads are not audited — now four are; §4.1/§4.3 need
`weekly_digest` and `quality_digest_send`). `docs/security-posture.md` needs the audited-read deviation (F-8). Assign **one** doc
owner across the three PRs so they do not conflict; do not leave it to the last PR.

### Minor

**F-13 — No guard test keeps the export filters in parity with the list filters.**
The export requests copy the list filters by hand (`:2743-2843`). A future filter added to `ListQualityExecutions`/
`ListQualityIssues`/`ListQualityTargets` will silently not be exportable. Add a protoreflect test in `backend/api/v1` (or
`proto`-adjacent) that compares field names/types/kinds of each List request minus `page_size`/`page_token` with the matching
export request minus `max_rows`, and fails on drift. Cheap and pins V-2.

**F-14 — `max_rows` contract leaves negative values and ordering undefined.**
The three `max_rows` comments (`:2776`, `:2806`, `:2840`) only say zero = default 100000 and >max is coerced. A negative value is
undefined and, if passed to `LIMIT`, is a server error. State "negative is `InvalidArgument`" (or "coerced to the default") in the
proto and enforce it in the handler. Likewise document the row order of the executions/issues exports (list order: newest first);
the targets export states `order_by`, the other two do not.

**F-15 — Digest verdict tally is internally ambiguous.**
`DataQualityDigestDetail` (`store/notification.proto:211-245`) says `execution_count` is "the verdict tally of the period's
completed executions" and then lists `passed/warned/failed/errored`; the six-state model also has `SKIPPED` and
`NOT_EVALUATED` (`data-quality.md` §2.2). A reader cannot reconcile `execution_count` with the four tallies, and a renderer will
choose differently than the next reader. Define it precisely: either `execution_count = passed+warned+failed+errored` (exclude
skipped/not-evaluated and say so), or make it the total and add `skipped_count`/`not_evaluated_count`. Also decide whether
`new/recovered_issue_count` covers muted issues (the ledger semantics in §3.4 are subtle).

**F-16 — Digest detail carries no target identity for deep links.**
`DataQualityDigestTarget` (`store/notification.proto:257-265`) has only `target_path`, so the rendered digest cannot link to the
target workbench; `outbound.link()` (`backend/component/notification/outbound/render.go:179-206`) has a default branch, so the
digest currently links to the deployment root. Add `target_guid`/`meta_type` (they are cheap ids, consistent with O-4's "ids not
addresses"), and add an explicit digest arm to `link()` pointing at the DQ issues/target page. (The digest PR must add the render
arm regardless; `Render`'s detail switch at `render.go:16-36` has a default that would otherwise title the message
"DATA_QUALITY_DIGEST".)

**F-17 — v1↔store `DayOfWeek` casts rely on silent numeric equivalence.**
`setting_service.go:207` and `:272` cast between `v1pb.DayOfWeek` and `storepb.DayOfWeek`. The values happen to match today, but
this is an unguarded invariant across two files that may evolve independently (the helper `dataquality.DigestDayOfWeek` already
exists for the reverse direction but is only used in `resolveDataQualitySetting:313`). Use explicit mapping in both directions
(or add a comment naming the invariant), and add a test asserting the enums stay identical.

**F-18 — Digest default zone is UTC, so "enable with defaults" fires at 17:00 in Asia/Shanghai.**
`DefaultDigestTimezone = "UTC"` (`settings.go:61-63`) only affects the *displayed resolved* schedule, but the settings page loads
the resolved value, so an admin who enables without touching the fields saves `09:00 UTC`. Quiet hours, by contrast, requires an
explicit IANA zone. Either make the UI require an explicit zone when enabling, or have the API refuse `enabled=true` without an
explicit zone (currently defaults silently fill it) — at minimum surface the zone prominently in the UI. The current default is
defensible only if the UI makes it obvious.

**F-19 — Test gaps in `932f7e8`/`bd7c87e`.**
The new tests cover normalize validation, normalize→store conversion, resolve defaults and `DigestScheduleFromStore` leniency
well. Missing and worth adding: (a) `SettingsFromProfile` wiring with an explicitly configured digest (regression if the
`settings.Digest = DigestScheduleFromStore(...)` line is lost); (b) a disabled digest carrying partial *valid* values
(e.g. day only) is accepted (the leniency the validator promises); (c) a JSONB round-trip
(`protojson.Marshal`/`Unmarshal` of the stored `DataQualitySetting` with the digest) to pin the stored shape; (d) the update-mask
behaviour from F-5, whichever semantics are chosen.

**F-20 — Audit resource resolution is empty for exports/report.**
`audit.ResolveResource` (`backend/component/audit/audit.go:279`) looks for `name`/`user.name`/`email`; the new requests name
`parent` + `target_guid`/filters, and the report response has `file_name` but no `name`. Audit rows for these calls therefore get
`resource = user` (the fallback in `createAuditLog`, `audit.go:130-137`), so an operator cannot search the ledger by target. Add
`target_guid`/`targetGuid` (or `target.path`) as a candidate for these methods, or set `Resource` explicitly in the handler via
the recorder; pin with a test.

### Nits

- **N-1** `quality_digest_send.sent_at` (`0035:11`) is written when the digest is *queued*, not sent; the table comment says
  "queued". Rename to `queued_at` or document precisely what the timestamp means. Also consider `period_key` as the primary key
  (drop the unused `BIGSERIAL id`), and record that 52 rows/year needs no retention policy (unlike the other DQ tables).
- **N-2** `ExportQualityDataChunk` is a generic name but its doc is CSV-specific (BOM, header row). Either rename to
  `ExportCSVChunk` or phrase the contract as "the first slice starts with the file header (for CSV: UTF-8 BOM + header row)".
  Also state that an empty result still sends exactly one chunk with BOM+header and `row_count = 0`.
- **N-3** v1 `QualityDigestSetting.time` comment (`proto/v1/v1/setting_service.proto:221`) does not say it is required while
  enabled, unlike day/timezone. Add it. Store `QualityDigestSetting` fields carry no comments at all
  (`proto/store/store/setting.proto:146-153`) — the other stored messages document their fields.
- **N-4** store `NotificationDeliveryEvent.audience_user_ids` comment (`:386-392`) still implies every event has an audience;
  state that a digest event has none (and what the email channel does then, per F-3).
- **N-5** The export streams' audit rows will show `Severity = ERROR` when a user cancels a large download
  (`audit.MapSeverity` maps `context.Canceled`/`DeadlineExceeded`-shaped Connect errors to ERROR). Map client cancellation to a
  non-error severity or document the noise; large downloads make cancellation common.
- **N-6** `ExportQualityReport` is audited while `GetQualityTarget` (same data, plus history) is not; note the reason in the RPC
  comment (`:380-385` explains "bulk read of quality evidence" — extend it to say the same data remains unaudited elsewhere, so
  the audit is about the artifact, not access control).

## What the three PRs must enforce (integration checklist)

**Export PR**
- `ExportQualityDataChunk.truncated` (F-4) and negative `max_rows` handling (F-14).
- Streaming audit recorder (`backend/component/audit/stream.go`, `audit.go` wrapping `Receive`) in the **same** PR as the audited
  RPCs, with a handler-supplied result summary (`row_count`, format, `max_rows`, `truncated`) — never the CSV bytes. At the
  foundation commit the streaming audit path writes no request/resource at all (`backend/api/v1/audit.go:62-91` at HEAD), so a
  release that includes the annotations without the recorder produces ledger rows that do not say what was exported.
- Concurrency/in-flight cap (F-9); per-chunk `ctx` check and cursor close; header-only chunk for empty results.
- Filter-parity guard test (F-13); order documented (F-14).
- Audit policy/security-posture/doc updates shared with the report PR (F-8, F-12); do not edit the same doc sections from two PRs
  without coordinating.

**Report PR**
- Audit response redaction (F-1) — blocker; test it.
- Size cap and truncation-in-document (F-10); `trend_days` validation (negative/0/>90), language validation/fallback
  (`zh-CN`/`en-US`, empty = en-US, unknown = `InvalidArgument` or documented fallback).
- HTML escaping: every interpolated value (target path, rule titles, error messages, sample values) must be HTML-escaped, and the
  document must load nothing external (no CDN fonts/scripts; inline SVG chart). The report is opened via `file://` by recipients,
  so a stored-XSS payload in a rule title must not execute. Add a renderer test with `<script>` in a title/sample value.
- Audit resource resolution (F-20); `.agents/docs` + `docs/` updates including the report's monitoring-basis section.

**Digest PR**
- Explicit opt-in fan-out + route exclusion (F-2); empty `event_types` must not receive it; update proto comments and
  `notifications.md` O-2.
- EMAIL audience/fixed-address policy (F-3).
- Atomic "record period + enqueue deliveries" (F-6) and correct migration comment.
- Drop `owner_email` or resolve at send time (F-11); add `target_guid` + `link()` arm (F-16).
- Render templates for the new detail in `component/notification/outbound/render.go` (both languages, en-US default) and the
  dispatcher path; the current `Render`/`link` switches have defaults that would otherwise produce a `DATA_QUALITY_DIGEST` title
  and root link.
- Digest runner wiring (7th/8th runner), startup behavior, schedule resolution using `dataquality.Settings.Digest`, per-week
  idempotency; tests for DST/timezone boundary, disabled workspace, no subscribed channel (`channel_count = 0`), and a channel
  subscribed to all *other* types.
- Settings/docs: `weekly_digest` mask/wipe behaviour (F-5), UTC-default UX (F-18), §4.1/§4.3 docs.

**Frontend PR**
- The generated v1 enum now has `DATA_QUALITY_DIGEST = 4`; the channel editor's static event-type list and `eventTypeLabel`
  (`frontend/src/pages/settings/NotificationSettingsPage.vue:1380-1400,2370-2380`) must add it as a **separate opt-in** control
  (not part of "all types"), with a hint that EMAIL channels need a fixed address (F-3) and that empty selection excludes it
  (F-2). Delivery list labels also need it.
- Streaming CSV client: concatenate raw bytes (do not `TextDecoder` each chunk independently — see the chunk contract), strip/keep
  BOM, handle mid-stream error as "no file", and surface truncation (F-4).
- No in-app notification copy for digest; keep `lib/notificationText.ts` fallback behaviour (a digest must never appear in the
  inbox; if it somehow does, the existing `default` branch is the intended degradation).

## Verdict

The foundation is a sound structural base: the proto additions are backward-compatible, the oneof/enum numbering is correct, the
export filters genuinely mirror the list RPCs, the migration follows the two-file rule and is consistent with `LATEST.sql`, the
settings resolve/validate split matches the repo's "zero means default" contract, and the tree builds, formats, lints (modulo the
three stub receivers) and passes the existing Go suites.

It is **not ready to build the feature PRs on without four contract decisions**: (1) the report must not copy its HTML into the
permanent audit ledger (F-1); (2) the digest must be explicitly opt-in and must not contradict the documented empty-`event_types`
wildcard (F-2), including the EMAIL no-recipient trap (F-3) and the `owner_email` payload leak (F-11); (3) exports need a
truncation signal (F-4); (4) the whole-message `data_quality` mask must not silently wipe a configured digest (F-5). The
`quality_digest_send` comment should be corrected so the at-most-once guarantee is real (F-6), the stub lint findings should be
fixed or absorbed (F-7), and the audited-read policy deviation plus all new surface must land in
`docs/security-posture.md` / `.agents/docs/` in the same stack (F-8, F-12). With those addressed, I see no blocker to the
three-PR split; without them the digest PR in particular can ship a fan-out that spams every pre-existing channel or produces
dead deliveries, and the report PR would put report data into a ledger that is never pruned.

---

## Addendum — fix commit `1bbaddf` ("close the foundation review gaps"), re-verified

Verified in an isolated `git archive 1bbaddf` copy: `go build ./backend/...` passes; `go test` for
`backend/component/audit`, `backend/api/v1`, `backend/component/dataquality`, `backend/store`,
`backend/component/notification` passes; `buf lint`/`buf format -d` clean. The six requested fixes were checked against the
committed contract plus the (still uncommitted) implementations they depend on; they are **closed**, with the residual notes
below (no re-reporting).

| Finding | Status | Evidence / residual |
| --- | --- | --- |
| F-1 report HTML in ledger | **closed** | `ExportQualityReportResponse.html_content = 2` (`data_quality_service.proto:2877-2886`); `IsSensitiveAuditField` covers `htmlcontent`/`html_content` (`audit.go:250-258`) — protojson's `htmlContent` normalizes to the former; `TestMarshalAuditMessageRedactsReportHTML` pins redaction and that `fileName` survives. Residual: the report handler must populate `HtmlContent` (the in-flight `data_quality_report.go:82-85` still assigns the removed `Content` field, so the worktree deliberately does not compile until report-impl updates it — verify in that PR). |
| F-2 digest opt-in vs empty = all | **closed** | Store comments now say empty = every type *except* the opt-in digest (`notification.proto:291-295`) and that routes never select it (`:411-413`); same in v1 (`notification_service.proto:526-530, 767-772`). The implementation enforces explicit membership, skips `routed_only` channels, and does not consult routes (`component/notification/data_quality_digest.go`). Residual: `docs/notifications.md` O-2 and the frontend opt-in control (lead/frontend pass). |
| F-4 silent export truncation | **closed** | `bool truncated = 3` (`data_quality_service.proto:2739-2745`); the implementation probes one extra row at the ceiling so an exact-cap selection is not flagged, keeps a byte tail pending so the last slice carries the flag (`data_quality_export.go` writer), and the max_rows comment now covers negative values. Nit: the field comment's parenthetical ("and therefore on every later slice is meaningless") is hard to parse; restate as "set only on the final slice". |
| F-5 digest wiped by whole-message mask | **closed** | `applyWorkspaceProfileUpdate` preserves the stored digest when an older client omits the nested message, replaces it when the message is present (including explicit `enabled=false`), and still clears it when the whole `data_quality` message is absent (`setting_service.go:141-152`, test `TestApplyWorkspaceProfileUpdateKeepsOmittedWeeklyDigest`). Residual: state the omit-preserves / explicit-clears contract in the `weekly_digest` proto comment and data-quality.md §4.1 (docs pass). |
| F-6 digest period comment overpromise | **closed** | Both migration copies now describe "one handled period", non-transactional enqueue, and the deterministic event id. Verified the implementation matches: `event_id = "dqdigest:" + periodKey` with the delivery `(event_id, channel_id)` unique index absorbing retries, and the runner enqueues first, leaves the period unrecorded on enqueue failure, records only after success. Behavior note to decide in the digest PR (not a foundation defect): `catchUpWindow = 24h` means a digest missed by more than a day (weekend outage/restart) is silently skipped with no log line; consider a WARN when a due period is dropped or document the accepted skip. |
| F-11 `owner_email` in outbox payload | **closed** | Renamed `owner_name = 2` with a comment stating it is a display name and the payload carries no contact details (`notification.proto:246-255`); the aggregation reads `principal.name`, not `principal.email` (`store/quality_digest.go` owner query). Residual: renderer must tolerate an empty name (deleted account) — check in digest PR. |

Bonus verified while re-checking: the F-9 export slot semaphore is implemented on `DataQualityService`
(`data_quality_service.go:108-146`, cap 4, `ResourceExhausted`, nil disables it for direct-construct tests) and the in-flight
handlers acquire/release it; F-14's max_rows wording is fixed. One nit for the export PR: on `ctx.Done()` the slot helper returns
`ctx.Err()` raw rather than a `connect.CodeCanceled`/`DeadlineExceeded` error, which the error-mapping interceptor will map to a
generic code — return a mapped error if the surrounding handlers rely on the interceptor mapping.

Phase B remains as before: the Lead messages per PR; each review will re-run build/tests on the final diff and focus on the
behavior/test gaps listed in the checklist above.
