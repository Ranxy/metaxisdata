# PR-1 review — outbound notification delivery (webhook channel + outbox + dispatcher)

- **Branch**: `feat/dq-outbound-notifications`
- **Base**: `6f43286` (main) · **Head**: `5271d3a` (amended PR-1 commit)
- **In scope**: `6655123..5271d3a` — 55 files, +10,959 / −296 (proto store + v1, migration, store, component, dispatcher runner,
  API handlers, server/metrics/maintenance wiring, Settings page + api/i18n/glossary/route/layout, docs).
  `6655123` (`.agents/reviews/data-quality-competitive-gap-analysis.md`) is a docs-only commit outside this review.
- **Method**: read the governing docs first (`AGENTS.md`, `backend/AGENTS.md`, `proto/AGENTS.md`, `frontend/AGENTS.md`,
  `backend/migrator/AGENTS.md`, `.agents/docs/notifications.md` outbound section), then the full diff. Ran every read-only gate
  (results below). Two claims were checked against a real PostgreSQL 16 container (disposable, `docker`, removed afterwards) and
  one against a Go `net/http` repro; the results are quoted in the findings. Generated code was regenerated and compared
  (`cd proto && buf generate` produced **no** diff, so `backend/generated-go/`, `frontend/src/types/proto-es/` and
  `proto/gen/grpc-doc/` are in sync with the proto change).
- **Not modified**: no source file was touched. This document is the only added file, left uncommitted.

## Verdict

**fix blockers first** — one blocker (channel resource IDs are generated in an alphabet that the server's own name parser
rejects, so ~99.8% of created channels cannot be tested/edited/deleted/fetched afterwards), plus four majors. The
architecture (event-level outbox + dispatcher, encrypted write-only credential, per-channel subscription filter) is sound and
matches the agreed scope; nothing here argues for a redesign.

---

## What I verified as correct (so it does not need re-checking)

**Event semantics**
- One `Send` / `SendToWorkspaceAdmins` call = one event. The event id is generated once per call, after the inbox write, and
  only when at least one inbox row was actually written (`service.go:242-330`): dedupe-suppressed writes (`created == nil`),
  and a `SendToWorkspaceAdmins` whose writes all failed, enqueue nothing. Unit tests pin the multi-admin case (2 admins → 2 inbox
  rows, 1 delivery per channel, shared event id), the dedupe-suppressed case, per-call id freshness, subscription
  filtering (type set, severity floor, disabled), no-detail messages, and best-effort outbox failure.
- `(event_id, channel_id)` is a real uniqueness guarantee: verified in PostgreSQL that the exact `INSERT … SELECT * FROM UNNEST(…)`
  statement accepts `pq.Array`-style literals for `text[]/int2[]/int4[]/timestamptz[]/jsonb[]` and that a second execution of the
  same `(event_id, channel_id)` inserts `0` rows (`ON CONFLICT DO NOTHING`). `event_id` is opaque text, never validated, so the
  generator alphabet problem below does not affect it.
- All three producers go through the same two entry points (schema sync `operation.go`, OpenLineage `openlineage_handler.go`,
  data quality `qualityrunner/notify.go`), and each logical event makes exactly one call — no producer calls both `Send` and
  `SendToWorkspaceAdmins` for one event, so a channel is not duplicated.
- Enqueue is synchronous but bounded by the existing `writeTimeout = 10s` detached context, and its error is only logged; a
  broken channel never fails the producer. Same-event-twice is absorbed by the ordinary dedupe key/index (outbox is a
  consequence of a written inbox row, not a second identity).

**SQL / lease semantics** (`backend/store/notification_delivery.go`)
- Claim is a CTE with `FOR UPDATE SKIP LOCKED`, `state = ANY(int2[])`, `next_attempt_at <= NOW()`, `ORDER BY next_attempt_at, id`,
  `LIMIT $2`, then `UPDATE … RETURNING`, incrementing `attempts` and setting the lease (`next_attempt_at = NOW() + lease`) on the
  same row. SENDING rows with an expired lease are reclaimed by the same predicate, which is the documented crash recovery.
- I doubted that the partial index `idx_notification_delivery_due (next_attempt_at) WHERE state IN (1,2)` could be used with a
  parameterized `$1::int2[]`, and checked with `EXPLAIN (ANALYZE, BUFFERS)` on 20k rows: PostgreSQL **does** use it
  (`Index Scan using idx_notification_delivery_due … Filter: (state = ANY ('{1,2}'::smallint[]))`, plus an incremental sort for the
  `id` tie-break). The driver's unnamed prepared statement plans with the bound values, so this is a custom plan each time. No
  finding.
- `attempts` is incremented by the claim, so `RETURNING attempts` already carries the post-increment value; the dispatcher's
  budget math (`delivery.Attempts >= 6` → DEAD, else `retryBackoff[attempts-1]`) therefore yields attempts 1..6 → backoff
  1m/5m/15m/1h/6h → DEAD on the 6th failure, exactly as documented and as the unit tests assert. `attempts`/`state` are `INT4`
  in SQL and scanned into `int16`/`int32`; `database/sql` converts in range, and the int2/int4 casts on the write side match the
  columns (verified by the insert above).
- `FinishNotificationDelivery` makes SENT/DEAD/PENDING explicit, stamps `sent_at` only for SENT, and clears it otherwise;
  `RequeueNotificationDelivery` is a single conditional `UPDATE … WHERE state = DEAD RETURNING`, so it is idempotent and cannot
  duplicate a post; `PruneNotificationDeliveries` deletes only SENT/DEAD older than the cutoff (queued rows are never pruned);
  the listing applies a limit clamp and offset guard.

**Migration**
- `0033##notification_outbound.sql` is appended after `0032`, is `IF NOT EXISTS`-idempotent, and is byte-for-byte equal to the
  added block in `LATEST.sql` (same DDL, indexes, comments) — the two files agree, and column comments name the
  `proto/store` messages they hold (JSONB contract). `go test -tags=integration ./backend/migrator/ -run
  TestMigrateSchemaLATESTMatchesTheIncrementChain` **passes** (5.7s, real PostgreSQL via testcontainers), so chain/LATEST parity
  is pinned. FK `channel_id … ON DELETE CASCADE`, unique keys on `resource_id` and `(event_id, channel_id)`, and both indexes
  are exactly what the queries need (apart from the listing shape, minor #4). Prune retention (30 days, terminal rows only)
  matches `.agents/docs/notifications.md` O-8.

**Dispatcher** (`backend/runner/notificationdispatcher/`)
- Tick 2s with `runner.Jitter`, ≤20 claims, ≤4 concurrent posts, 5m lease, 15s attempt budget over a 10s HTTP timeout; channel
  list and `external_url` are read once per batch; nil metrics are safe (`metrics/notification.go` guards `m == nil`, and the
  golden metric-name test is updated and passes); the two new instruments are registered through `Metrics.New`/`collectors`.
- Shutdown: the loop returns on `ctx.Done()`; in-flight attempts are cancelled, their finish writes fail and are logged, and the
  lease reclaims them later — at-least-once is documented and no row is stranded by a normal shutdown.
- Rendering happens at send time from the stored structured event, in the channel's language; `occurredAt` is the queue time;
  the body/headers are stable and the HMAC is computed over the exact bytes sent.

**Security**
- The credential never reaches a read path: `notificationChannelColumns` returns `(secret <> '') AS has_secret` and not the
  column; the API conversion only sets `secret_set`; the only decryption call sites are the dispatcher and the test-send path.
  Encryption goes through `store.credentialCipher()` (same `v1:` AES-256-GCM path as instance credentials, installed at startup
  by `server/init.go`), ciphertext is never echoed in errors, and a nil/empty secret stores `''` so an update can distinguish
  "keep" from "replace" (`CASE WHEN $3 = '' THEN secret ELSE $3`).
- Partial rotation is handled on both sides: the API merges the *fields* of an update secret (URL and signing secret independently)
  with the decrypted stored value before validating and storing (`notification_outbound_service.go:115-142`), so rotating only the
  URL or only the signing secret works; the UI's empty-field-keeps-stored rule matches it.
- Audit redaction: `IsSensitiveAuditField` lowercases and the new markers `webhookurl`/`webhook_url`/`webhookendpoint`/
  `webhook_endpoint` are matched case-insensitively, and `SanitizeAuditValue` recurses into nested maps, so a nested
  `secret.webhookUrl` **is** redacted (`webhookSigningSecret` was already caught by the `secret` marker). Existing audit tests
  pass.
- URL validation (non-empty, ≤2048 chars, `http`/`https`, host present) and transport-error sanitization (`<endpoint>`) exist;
  the SSRF posture doc is honest about administrator-chosen destinations (one inaccuracy about redirects — see major #2).

**API / AIP**
- Permissions are exactly the agreed reuse: `settings.get` on list/get/list-deliveries, `settings.update` on
  create/update/delete/test/retry, and `audit=true` on all mutations **and** on `Test`/`Retry`; the ACL and audit coverage tests
  (which walk the proto registry) pass. Resource patterns/name parsing for deliveries (numeric row id) and channels, the
  NotFound/InvalidArgument (parse) /FailedPrecondition (test send) mapping, and "test send queues nothing" are correct.
- Generated Connect/gateway code includes all 8 new methods and the REST routes, and regeneration is a no-op.

**Runner / server / maintenance wiring**
- The dispatcher is registered as the 7th runner with a unique name in the single `runners` slice (WaitGroup count stays in
  sync), `maintenance` gained `PruneNotificationDeliveries` in its store interface and prune list, and the runner/metrics tests
  (`backend/runner/maintenance`, `backend/metrics`) pass.

**Frontend**
- `biome ci`, `eslint --max-warnings=0`, `i18n` (missing/unused/consistency/placeholder/parity + sorter + 97-term glossary) and
  `vue-tsc --build` all pass; `pnpm test run` is 132 files / 1320 tests green. Route (`/settings/notifications`, permission
  `settings.get`), Settings nav entry, layout-audit registration, and both locale files (65 keys each, balanced) are present.
  The dialog's create/edit/secret-keep state machine is coherent: editing starts with empty endpoint/signing-secret fields,
  `buildSecret()` returns `undefined` when both are empty, and the update mask names every mutable field.
- Docs `docs/notifications.md` (new), `.agents/docs/notifications.md` outbound section, `docs/security-posture.md` bullet,
  `docs/data-quality.md`, `.agents/docs/data-quality.md` D-09 and the root `AGENTS.md` Data Quality bullet were all updated, and
  the old "no email or webhook" claims in the operator/agent docs are gone (the one remaining "仅站内信" row in
  `.agents/docs/notifications.md` decisions #5 is explicitly marked as superseded by the outbound section).

---

## Findings

### Blocker

| # | Severity | Location | Why it matters | Fix |
| --- | --- | --- | --- | --- |
| B1 | **blocker** | `backend/store/notification_channel.go:87` (+ `:17`) generating the id; `backend/common/resource_name.go:13,335-345` parsing it | Channel resource IDs are generated by `common.RandomString(12)`, whose alphabet is `[0-9a-zA-Z]` (`common/utils.go:46`), but `GetNotificationChannelID` rejects anything that does not match `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$` — no uppercase, first char must be a lowercase letter. I measured it with the real functions: **1,996 of 2,000 generated channel names (99.80%) are rejected**. Create and List still work (they never parse the id), but every later operation goes through `GetNotificationChannelID` — `Get`, `Update`, `Delete`, `Test`, and `ListNotificationDeliveries`' `channel` filter — so after creating a channel the Settings page's Test/Edit/Delete all fail with `InvalidArgument: invalid notification channel id in …`, and the only cleanup is SQL. The feature's management surface is effectively unusable. No test exercises create→parse, which is why the suite is green. | Generate a valid id, e.g. add `common.RandomResourceID(n)` that draws from `[a-z0-9]` and forces a leading `a-z` (or `strings.ToLower` **plus** a leading letter), and use it here; add a round-trip test `store.Create → GetNotificationChannelID(name)` (and an API create→get test). Sibling code already shows the constraint was known: `api/v1/instance_convert.go:140-144` lowercases generated data-source ids and validates client-supplied ones. |

### Major

| # | Severity | Location | Why it matters | Fix |
| --- | --- | --- | --- | --- |
| M1 | **major** | `backend/runner/notificationdispatcher/notificationdispatcher.go:112-119` with `:144-147` | A **transient** failure of the per-batch `ListNotificationChannels` is only logged; `byID` stays empty, so every claimed delivery of that tick is finished as `DEAD ("the notification channel no longer exists")` — up to 20 deliveries per tick permanently killed by one DB hiccup, bypassing the retry budget, while the real channel exists. Because the FK is `ON DELETE CASCADE`, a genuinely missing channel cannot produce a row, so this branch is in practice only reachable through this bug (the unit test `TestDispatchDeadWhenChannelGone` encodes the "empty list = gone" conflation). | On a channel-list error, abort the batch: return the claimed rows to `PENDING` at `NOW()` (or at least `continue` without finishing) and retry next tick. Only mark a delivery DEAD from `channel == nil` when the list read *succeeded*. Distinguish "list failed" from "channel absent" in the fake/test. |
| M2 | **major** | `backend/component/notification/outbound/webhook.go:59,92` (default `http.Client`, no `CheckRedirect`) | The default client follows redirects. I reproduced Go's behavior: on **301/302/303** the POST is re-issued as a **GET** with the body dropped (the signature header is still forwarded), so a destination that redirects `http→https` (very common) answers 200, the receiver returns success, and the delivery is recorded `SENT` although the notification body was never delivered — silent loss that no retry can fix. On **307/308** the same body *and* the `X-Metaxisdata-Signature` header are re-POSTed to a host chosen by the endpoint, not by the administrator, so the accepted SSRF surface ("destinations an administrator enters") silently widens, and a body+signature pair leaks to a third host. | Construct the client with `CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }` and treat any 3xx as a failed attempt (recorded error, normal retry). Document that redirects are not followed. |
| M3 | **major** | `backend/component/notification/outbound/render.go:184-197` (`lastSegment`) vs `frontend/src/router/index.ts:338,363` and `frontend/src/pages/data-quality/DataQualityIssueDetailPage.vue:534-538`, `.../DataQualityExecutionDetailPage.vue:543-548` | Outbound deep links for data-quality events are malformed: the renderer emits `…/data-quality/issues/{lastSegment}` / `…/data-quality/executions/{lastSegment}`, e.g. `…/executions/981`, but those routes are `issues/:issueId+` / `executions/:executionId+` and the pages join the segments back into the **whole resource name** to call `GetQualityIssue`/`GetQualityExecution`. `Get("981")` is `InvalidArgument`, so the link lands on an error page. Issue-opened/recovered and execution-failed — two of the three in-scope event types — are affected; only the schema-sync (`instances/{id}`) and OpenLineage list links are correct. The docs example (`docs/notifications.md`, `"link": "…/data-quality/executions/981"`) encodes the broken shape. The in-app link builder already does it right: `frontend/src/lib/notificationText.ts:129-134` appends the whole resource name. | Build the path with the full resource name (same as `qualityResourceHref`) instead of `lastSegment`, and add render tests that set `issue`/`execution` to real names and assert the full-name path; fix the docs example. |
| M4 | **major** (coverage) | `backend/api/v1/notification_outbound_service.go` (no test file), `backend/test/integration/` (nothing added), `backend/store/notification_delivery.go` (no guard test) | None of the 8 outbound RPCs is covered by a handler test, there is no store guard/round-trip test for channel names, ids, lease/claim shape, `ON CONFLICT`, requeue or prune, and there is no integration scenario (channel CRUD → test send against an httptest endpoint → delivery listing/retry). B1 and M1 are exactly the class of bug such a test would have caught: a single `create → get/update/delete` round-trip fails today. | Add: an API test for create/get/update(mask+secret-keep)/delete/test/retry with a fake store; a store guard test pinning the claim/unique/requeue SQL shape (the `backend/store/meta_resource_test.go` pattern); one integration case (channel CRUD + test send + delivery listing) under `backend/test/integration/runner/`. |

### Minor

| # | Severity | Location | Why it matters | Fix |
| --- | --- | --- | --- | --- |
| 1 | minor | `backend/runner/notificationdispatcher/notificationdispatcher.go:206-211` `truncateError` | Cuts at 1024 **bytes**, unlike the rune-safe `truncate` next door (`outbound/render.go:323`) and `common.TruncateUTF8Bytes` used by audit. A webhook error embeds up to 1000 **runes** of the endpoint's body; a CJK body can exceed 1024 bytes, and cutting mid-rune produces invalid UTF-8, which PostgreSQL rejects on insert. `FinishNotificationDelivery` then fails, the row stays `SENDING` with a 5-minute lease, is reclaimed, fails to finish again — a delivery stuck in `SENDING` that re-POSTs every 5 minutes forever, is never DEAD, and (state `SENDING`) is never pruned. | Use `common.TruncateUTF8Bytes(value, maxErrorBytes)` (and append the same truncation suffix), or truncate the response body by runes before composing the error. |
| 2 | minor | `backend/api/v1/notification_outbound_service.go:108` + `:377-413`, `:112` | A masked update that omits fields is rejected before the mask is applied: `storeNotificationChannelPayload` requires a display name (and a secret is required on create) *before* `applyNotificationChannelUpdateMask` merges the stored payload, so `update_mask=["enabled"]` with a partially populated channel returns `InvalidArgument`, against AIP-134. Also a `kind` change is silently overwritten (`payload.Kind = existing.Kind`) and unknown mask paths are silently ignored (the comment admits it) instead of `InvalidArgument`. | Apply the mask first (or validate only the masked fields), return `InvalidArgument` for a kind change and for unknown mask paths, and keep the kind-immutability documented as a rejected request rather than a silent no-op. |
| 3 | minor | `backend/store/notification_delivery.go:280-302` + `notificationdispatcher.go:181-192` | `RequeueNotificationDelivery` sets `PENDING` but does not reset `attempts`; the claim increments it to 7, and on the next failure `Attempts >= maxAttempts` immediately DEADs it. A manual Retry therefore grants exactly one attempt with no backoff, which the UI ("fix the cause and press Retry") and docs do not say. | Reset `attempts = 0` (and keep `last_error` cleared) on requeue, or document the single-attempt semantics explicitly in the proto/docs. |
| 4 | minor (perf) | `backend/store/notification_delivery.go:250-257` | `WHERE ($1::int8 = 0 OR channel_id = $1) AND ($2::int2 = 0 OR state = $2)` defeats index usage, and no index supports `ORDER BY created_at DESC` without a channel prefix. Verified with `EXPLAIN (ANALYZE)` on 20k rows: `Seq Scan on notification_delivery … Sort (top-N) … Buffers: shared hit=228` even when filtering by channel. The table keeps 30 days of deliveries, so every Settings page view scans it. | Build the WHERE clause from the present filters (`$1=0` branches are a classic index-killer), and add `CREATE INDEX … ON notification_delivery(created_at DESC, id DESC)` for the unfiltered admin listing. |
| 5 | minor | `backend/api/v1/notification_outbound_service.go:176-223`; `proto/v1/v1/notification_service.proto:129-130`; `docs/notifications.md` "Every change, test send and retry is recorded in the audit log" | The handler/proto comment says a test send "writes nothing to the ledger", but the RPC carries `(metaxisdata.v1.audit) = true`, and the audit interceptor stores the **response** too (`api/v1/audit.go:122`), i.e. up to 1000 chars of arbitrary third-party response body go into the permanent ledger under a non-redacted `response` key. Docs say audited; the code comments say not. | Pick one: keep the audit row but exclude/redact the response body (drop `Response` for this method or add a marker), or remove the audit annotation; then make the proto/handler comments match. |
| 6 | minor | `backend/component/notification/outbound/webhook.go:111-114` `sanitize` | `strings.ReplaceAll(message, w.url, "<endpoint>")` only matches the raw stored string. I reproduced with `net/http`: for `HTTPS://host/…` the transport error quotes the **normalized** (`https://…`) URL, so the exact replace misses and the endpoint URL — which the accepted threat model treats as a credential, since chat URLs carry `access_token` — lands in `last_error`, which the API returns to every `settings.get` holder. | Sanitize both `raw` and `parsed.String()` (or unwrap `*url.Error` and keep only `err.Err` plus the host), and add a test with a case-normalized/percent-encoded URL. |
| 7 | minor | `frontend/src/pages/settings/NotificationSettingsPage.vue:88-100,137-163,297,525,820` | (a) Test is the only row action not gated by `canUpdate`, so a `settings.get`-only member sees it and gets `PermissionDenied`; (b) the list separator `"、"` is hardcoded, so the English UI renders `Data quality、Schema sync`; (c) the endpoint `FormField` is marked `required` while on edit empty means "keep the stored one" (the description says so — the marker contradicts it); (d) the delivery filter offers all/pending/dead/sent but not `SENDING`, although the state exists and stuck leases are exactly what an operator wants to filter. | Add `:disabled="!canUpdate"`, use a translated separator (`t('common.listSeparator')` or a `joinI18n`-style helper), mark the field required only when creating, and add a `Sending` filter option. |
| 8 | minor (docs) | `.agents/docs/notifications.md:319-326` failure-mode table; `:309` (O-5); root `AGENTS.md:27`; `.agents/docs/runner.md:5` + table; `.agents/docs/README.md:15`; `.agents/docs/metrics.md` catalogue | Several claims are now wrong or stale: (a) "channel deleted → queued deliveries marked DEAD (`channel no longer exists`)" — the FK cascade deletes them, as `docs/notifications.md` and the proto Delete comment correctly say; pick one truth (if cascade is intended, delete the DEAD row from the failure-mode table and note the cascade); (b) "missing credential → retried as a normal failure, DEAD after 6" — a missing URL DEADs immediately in `attempt` via `NewReceiver`, only a decrypt failure retries; (c) O-5 says `X-Metaxisdata-Event`/`X-Metaxisdata-Delivery` are attached when signing, but they are always sent; (d) the runner reference still says "The six runners" and does not list `notificationdispatcher`, and the metrics catalogue lacks `metaxisdata_notification_deliveries_attempted_total` / `…_delivery_duration_seconds`; the root `AGENTS.md` Settings bullet does not mention the new Settings → Notifications page. | Update the outbound failure-mode table to match the code (cascade, immediate DEAD on receiver construction), fix O-5, add the 7th runner + the two metrics to the references, and list the page under Settings. |
| 9 | minor | `proto/v1/v1/notification_service.proto:21-26` (service comment), `:130`, `:545`, `:608-609`; `backend/api/v1/notification_outbound_service.go:173-175` | Stale proto contract text introduced/left by this PR: the `NotificationService` comment still says "No method here carries a `(metaxisdata.v1.permission)` annotation", but eight new methods do; `NotificationDelivery.next_attempt_time` says "Unset when the delivery is terminal" while `convertToV1NotificationDelivery` always sets it (and the store always writes it); `TestNotificationChannel`'s "Nothing is written to the ledger" contradicts its own audit annotation; `ListNotificationDeliveries.page_size/page_token` carry no documented default/max (AIP-158). | Reword the service comment (inbox methods unannotated, outbound methods settings-gated), only set `NextAttemptTime` for non-terminal states (or fix the comment), fix the test-send comment, and document `page_size` default 50 / max 1000. |
| 10 | minor | `backend/store/notification_delivery.go:82-95`, `backend/component/notification/outbound_queue.go:31-64` | The enqueue insert is one multi-row `UNNEST` under a `channel_id` FK; a channel deleted between the channel read and the insert makes the **whole** insert fail, so the event produces no deliveries for any of the healthy channels either (only a log line). Additionally the event id is generated before the channel filter, so it is wasted when nothing subscribes. | Filter first, then generate; and make the write tolerant of a vanished channel (e.g. `INSERT … SELECT` from a `VALUES`/`UNNEST` list joined to `notification_channel`, which drops rows for deleted channels — or retry once without the failed channel). |

### Nits

| # | Location | Note |
| --- | --- | --- |
| 1 | `migration/0.1/0033##notification_outbound.sql` | `state INT4 NOT NULL` has no `CHECK (state IN (1,2,3,4))`; states come from Go enums, so a CHECK would document the invariant at the same cost. |
| 2 | `backend/store/notification_delivery.go:18-19,25,71,113` | `event_key` is written and scanned but never read or exposed; either surface it in the admin delivery resource (it is the dedupe key an operator would want) or drop the column/field. |
| 3 | `backend/component/notification/outbound/outbound.go:67-72` | `NormalizeLanguage` only matches the literal prefixes `zh`/`ZH`; `zH-CN` falls back to `en-US` and `zh-Hant` renders Simplified. Use `language.MatchStrings`/prefix-normalize case-insensitively (a BCP-47 tag is case-insensitive). |
| 4 | `backend/runner/notificationdispatcher/notificationdispatcher.go:191` | `retryBackoff[min(int(delivery.Attempts)-1, len-1)]` would index `-1` if a future call site ever passes `Attempts == 0`; a cheap `max(1, …)` clamp keeps it total. |
| 5 | `backend/runner/notificationdispatcher/notificationdispatcher.go:144-157` | `NotificationDeliveryDead` is not counted when the channel is missing or `NewReceiver` rejects the config, and `deliveries_attempted_total` help text says "attempts by outcome" — the metric undercounts dead deliveries that never reached a POST. |
| 6 | `backend/component/notification/outbound/render.go:205-210` | `lastSegment` computes the split index on `strings.TrimRight(resource, "/")` but slices the original string, so a name with a trailing slash returns `id/` (moot once M3 moves the link builder off `lastSegment`). |
| 7 | `proto/v1/v1/notification_service.proto:602,619` | `ListNotificationDeliveriesRequest.channel` and the `Get/Delete/Update` request `name` fields lack `(google.api.resource_reference)`; `ListNotificationChannels` has no pagination (documented as intentional — acceptable, but then the field-level comments should say so). |
| 8 | `frontend/src/pages/settings/NotificationSettingsPage.vue:170-260` | The deliveries table never shows `sent_time` or `next_attempt_time` (both in the proto) — "queued", state, attempts and error are there, but an operator diagnosing a `PENDING`/`SENDING` row cannot see when the next attempt is due. |

---

## Test and coverage gaps (ranked by what they would have caught)

1. **No channel-id round trip (would have caught blocker B1).** No test creates a channel through the store and parses its
   returned name, and no API test covers `CreateNotificationChannel` → `Get/Update/Delete/Test`. One test would have failed
   ~99.8% of the time.
2. **No dispatcher test for a channel-list read failure (would have caught M1).** `fakeDispatcherStore.ListNotificationChannels`
   cannot inject an error, and `TestDispatchDeadWhenChannelGone` asserts the conflated "empty list ⇒ DEAD" behavior as correct.
3. **No test for redirects (would have caught M2).** The webhook tests use direct `httptest` handlers; a 301/307 case would show
   GET-downgrade / cross-host re-POST.
4. **No test asserting a rendered link for a data-quality event with `issue`/`execution` set (would have caught M3).**
   `render_test.go` only exercises the no-issue/no-execution fallback `…/data-quality/executions`.
5. **No store guard test for the outbox SQL.** The claim CTE, `ON CONFLICT`, the conditional requeue, prune and the list filters
   are only exercised by a real database; per `backend/AGENTS.md` a query whose shape is the invariant deserves a guard test
   (the repo's `meta_resource_test.go` pattern), and there is no integration case for outbound delivery at all.
6. **No test for the update-mask merge** (partial mask, kind change, unknown path, secret-keep/independent rotation) or for the
   audit-redaction of `secret.webhookUrl` specifically (the generic redaction tests pass, but nothing pins the new markers).
7. **No frontend test for the new page**, while three sibling settings pages (`DataQualitySettingsPage`, `LLMProviderManagementPage`,
   `ConnectedAppsSettingsPage`) and the inbox page have one. The risky parts are the create/edit/secret-keep state machine and the
   update mask; a mount test with mocked `@/api/notification` would cover them cheaply. This matters more than usual because the
   API has no tests either, so nothing else pins the same behavior.
8. Untested but low value now: error truncation at the byte boundary (item 1), `NormalizeLanguage` case handling, and
   channel-disabled-at-delivery-time behavior.

---

## Gates (real results, run from the worktree)

| Gate | Result |
| --- | --- |
| `go build ./...` | **pass** |
| `gofmt -l backend/` | **pass** (no files) |
| `golangci-lint run --allow-parallel-runners` | **3 findings, all pre-existing in files this branch does not touch** (`backend/api/auth/authenticator.go:72`, `backend/plugin/db/pg/sync.go:379,1188`, revive `multiline-if-init`); no new findings. `git diff --name-only` confirms none of them are in the diff. |
| `go test ./backend/component/notification/... ./backend/runner/notificationdispatcher/... ./backend/runner/maintenance/... ./backend/store/... ./backend/metrics/... ./backend/api/v1/... ./backend/common/...` | **pass** |
| `go test -tags=integration ./backend/migrator/ -run TestMigrateSchemaLATESTMatchesTheIncrementChain` | **pass** (real PostgreSQL; Docker available) |
| `cd proto && buf lint` | **pass** |
| `cd proto && buf generate` then `git status` | **clean** — generated Go/proto-es/grpc-doc output is in sync with the proto change |
| `frontend`: `pnpm -s biome:ci` / `lint:ci` / `i18n` / `type-check` | **pass** (exit 0 each; i18n reports 97 glossary terms, locale sorter normalized) |
| `frontend`: `pnpm test run` | **pass** (132 files / 1320 tests) |

Out-of-band checks: disposable PostgreSQL 16 container (created/removed by me) for the `UNNEST … jsonb[]` insert, the
`ON CONFLICT` duplicate no-op, the claim `EXPLAIN`, and the listing `EXPLAIN`; a Go `net/http` repro for redirect semantics and
transport-error URL quoting; a throw-away module for the channel-id rejection measurement. No destructive command was run
against anything outside the disposable container.

---

## Suggested fix order

1. **B1** — one-line generator fix plus a round-trip test; without it nothing else on the admin surface can be exercised.
2. **M1** — abort the batch on a channel-read failure (small, prevents silent loss of queued notifications).
3. **M2** — stop following redirects (small, prevents both false `SENT` and cross-host re-POST of a signed body).
4. **M3** — build deep links from whole resource names, fix the docs example, add the render assertions.
5. **M4** — API test file + store guard test + one integration scenario, which then locks all of the above.
6. Minors in the listed order; docs consistency (minor 8/9) can ride along with the fix commit.
