# Notifications — Reference

> Maintenance reference for `backend/component/notification/`, the notification
> service handlers, the notification store, the dispatcher and digest runners, and the
> frontend notification family. Wire shapes live in
> [notification_service.proto](../../proto/v1/v1/notification_service.proto) and
> [notification.proto](../../proto/store/store/notification.proto); operator behavior is
> [docs/notifications.md](../../docs/notifications.md).

## What it is

Two delivery surfaces over one event. The **inbox** writes one row per recipient and the
browser renders it from structured data; **outbound channels** fan the same event out to
administrator-configured destinations, rendered and posted by a background dispatcher. The
inbox is always on, and outbound delivery never blocks or rolls back an inbox write.

| Inbox source | Trigger | Recipients | Detail |
| --- | --- | --- | --- |
| Schema sync | A user-requested sync, the first sync after instance creation, or a failed periodic sync | Initiator; a background failure goes to the administrators | `SchemaSyncDetail` |
| OpenLineage ingestion | A refused request, or a namespace matching no instance | Administrators: API-key auth, no initiating user | `OpenLineageDetail` |
| Data quality | Issue opened or recovered, execution failure, storm summary, watched target's schema change | Rule owner (administrators when none); failures and storms to administrators only | `DataQualityDetail` |
| Quality agent | A session parked on a code that needs a person, a patrol report with failures or new issues, or drafts awaiting review | Agent owner, or administrators when none is set | `QualityAgentAttentionDetail` (`NOTIFICATION_TYPE_DATA_QUALITY_AGENT`) |

A row stores `type + severity + structured detail`, never prose: the frontend renders it
through i18n, so one row reads in either language and an older client degrades an unknown
type to a generic line. The table is **personal data** — reads are scoped by
`recipient_id`, not by workspace. **Two counts, not one:** `unread` (`read_at IS NULL`)
changes only when a message is clicked or all are marked read; `unseen` is the subset
created after `principal.notification_seen_at` and drives the bell badge. Opening the
inbox only advances the watermark, so the badge clears while every message stays unread.

## Decisions — inbox

| Decision | Why |
| --- | --- |
| Recipient is the operation's initiator; only a background failure goes to the administrators | Needs an operation entity and role-to-user resolution |
| A message center only: no sync-job table and no progress display | Aggregated state lives in the syncer process |
| One message per sync operation, and a database's result within it is **its first result** | Retry does not rewrite the message; a transient failure therefore appears in it |
| Permanent retention, no TTL | Capacity is a partitioning question, not cleanup |
| A suppression window defaults to one hour (`NAMESPACE_UNMAPPED`: 24 hours) | One constant, adjustable only together with the database bucket |
| The first full sync after instance creation notifies, and OpenLineage 4xx refusals and unmapped namespaces both notify administrators | The message points cover every refusal branch plus the resolver |
| Administrators cannot compose messages by hand | Every body renders from i18n; no free text in a permanent row |
| The badge counts messages written after the inbox was last opened, not unread messages | The user asks "anything new", not "how many unclicked" |
| Opening the inbox only advances the watermark, never writes `read_at` | Only the mark-read RPCs write read state |
| Messages arriving while the inbox is open do not light the badge and advance the watermark | They are already on screen |
| The watermark is stamped by the server's `NOW()` | A client cannot claim it saw a message before a chosen time |

## Data model

| Column or index | Shape | Role |
| --- | --- | --- |
| `recipient_id` | `INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE` | Personal-data scope key |
| `read_at` | `TIMESTAMPTZ`, `NULL` = unread | Read state is a column, not a JSONB field |
| `dedupe_key` | `TEXT NOT NULL DEFAULT ''`, `<event>:<target>:<bucket>` | Empty = not deduplicated |
| `payload` | `JSONB` | `protojson` of `Notification`; ids, timestamps, recipient and dedupe key are not inside |
| `idx_notification_recipient_created_at` | `(recipient_id, created_at DESC, id DESC)` | Inbox listing |
| `idx_notification_recipient_unread` | `(recipient_id) WHERE read_at IS NULL` | Badge count touches only this index |
| `idx_notification_dedupe` | UNIQUE `(recipient_id, dedupe_key) WHERE dedupe_key <> ''` | Cross-replica, cross-restart deduplication |

Read state is a column because the badge count is a hot path: `payload->>'readTime' IS
NULL` cannot use a partial index, while `read_at` plus the partial index makes the count
one index probe. `principal.notification_seen_at` is the per-user watermark (`NULL` = never
opened), not a per-message column, which would update a batch of rows on every open. Both
counts come from one statement (`COUNT(*)` plus `COUNT(*) FILTER (created_at > watermark)`),
so `unseen <= unread` is constructed; two statements would take two snapshots.

Outbound storage: `notification_channel` (payload JSONB plus a write-only `secret` TEXT
with a `v1:` encrypted `NotificationChannelSecret`), `notification_delivery` (`event_id`,
cascading `channel_id`, state, attempts, `next_attempt_at` as both retry time and SENDING
lease, `last_error`, payload, `quiet_deferred`, UNIQUE `(event_id, channel_id)`), and
`notification_route` (payload JSONB). A new channel kind, config field or audience rule is
a proto increment needing no migration; a new table does.

## Write path

`notification.Service` is the only inbox writer and the start of the push stream:
`Send` (one recipient), `SendToWorkspaceAdmins` (resolve `roles/workspaceAdmin` with group
expansion, one cloned row each; none means a log line), `SendToRecipients` (explicit set),
`create` (fill the workspace name, insert, publish only when a row was written), `publish`
(under `publishMu`, read both counts once and hand the event to subscribers, so delivery
order matches snapshot order; a failed count read still delivers with
`CountsKnown = false`), the quality-agent helpers that build a `DATA_QUALITY_AGENT`
escalation, and `DedupeKey` (empty key for a non-positive window).

- **Best effort by contract.** A failed write is logged and never fails the sync or
  ingestion path; callers discard the error. The send methods detach from request
  cancellation (`context.WithoutCancel`) but stay bounded to `writeTimeout = 10s`.
- **Two suppression layers.** The process gate (`Service.claim`, a `recent` map bounded by
  `gateLimit = 1<<16`, `gateRetention = 24h`) absorbs most repeats cheaply; the unique
  index covers replicas and restarts. An empty key bypasses both; a failed insert
  releases the gate so the window can retry.
- **Payload bounds live in the constructors**: `MaxFailureEntries = 100`,
  `MaxErrorBytes = 2<<10` (rune-truncated), so no caller grows a permanent row.
- **Windows**: sync failure `1h`; OpenLineage refusal or failure `1h` (subject = the
  namespace, or `key/<id>`); unmapped namespace `24h`.

## Sync operation aggregation

One sync operation = the instance metadata step plus every database it queued; the
aggregator is process-local with `operationTimeout = 2h`.

- `StartOperation` → `RecordInstanceResult` → `EnqueueDatabases` (registers pending
  databases and seals the set); the checker's `completeDatabase` fans a database result
  out to every operation containing it. A discarded queue item (instance gone) must be
  reported too, or the operation waits forever.
- A deadline plus the checker's 10-second tick finalizes timed-out operations with missing
  databases as `UNFINISHED`; that is the only guard against a message never written and an
  entry never freed.
- A database background failure notifies on retry abandon; an instance failure notifies
  in `trySyncAll` with the hourly bucket, because a stuck `last_sync_time` retries every 15
  minutes. Background success does not notify.
- `CreateInstance`, `SyncInstance` and `BatchSyncInstances` register per-instance
  operations; a single-database `SyncDatabase` bypasses the aggregator; with no database
  queued, `FinishOperation` writes immediately.

## OpenLineage ingestion failures

Every refusal branch funnels through `reportIngestionFailure`, which notifies and then
writes the response; the status selects the severity.

| Branch | HTTP | Kind | Severity | Dedupe subject |
| --- | --- | --- | --- | --- |
| Parse failure, per-event limit | 400/413 | `INVALID_EVENT` | WARNING | `key/<id>` |
| Transport or batch limit | 413 | `LIMIT_EXCEEDED` | WARNING | `key/<id>` |
| Scope mismatch | 403 | `SCOPE_MISMATCH` | WARNING | The event's namespace |
| Persist or process error (including a partial batch) | 500 | `PERSIST_FAILED` / `PROCESS_FAILED` | ERROR | The event's namespace |
| No matching instance, stored as external | — | `NAMESPACE_UNMAPPED` | WARNING | The namespace, 24-hour bucket |

`NAMESPACE_UNMAPPED` hangs on the resolver's once-per-namespace-per-process gate; the
resolver takes an optional `UnmatchedNamespaceReporter`, and the processor wired for
maintenance and lineage revalidation is built without one. **Deliberately not
notified:** `401` (anonymous probing and misconfiguration look alike, and the audit
ledger holds it), `429` (the response says the producer is over budget), and "valid event
but no lineage" (a data-quality problem). Display uses a masked key.

## Public API

`NotificationService` has 20 RPCs; the field list and HTTP annotations are in the proto,
which is the source of truth.

- The inbox half carries no permission annotation, no `allow_without_credential` and no
  audit annotation: login is required, the scope is the caller, and reading one's inbox is
  not an administrative action for the ledger. A permission would lock a custom role out
  of its own messages. The channel, delivery and routing half uses `settings.get` for reads
  and `settings.update` for changes; writes are audited, a test send is not.
- `GetUnreadNotificationCount` returns both counts from one statement (its name is kept
  for older clients); `BatchMarkNotificationsRead` is idempotent and bounded by
  `maxNotificationsPerBatch = 1000`; `DeleteNotification` returns `NotFound` for someone
  else's message without distinguishing it. There is no CEL filter, no merged-counts RPC
  (a rename would 404 older frontends), and no REST route for the stream, because
  grpc-gateway cannot proxy server streaming.

## Push stream

A ConnectRPC **server-streaming RPC**, not SSE or `EventSource`: it reuses the
`ExplainSQL` precedent, so the interceptors apply and the frontend stays on the ConnectRPC
client. An event carries the message plus both counts, so the bell updates on arrival. The
stream is process-local on a single replica; there is no polling, only a refresh on
reconnect and one when the tab becomes visible. A connection lives at most 30 minutes and
the server ends it, because a stream authenticates once at open and ending it bounds logout
and revocation; a 25-second `KeepAlive` heartbeat also notices a dead peer. Backpressure
drops the subscription, never a message, and read-state or watermark changes are not
pushed.

`hub.go` is the fan-out keyed by `recipientID`: `publish` never blocks the writer and
closes a subscriber whose buffer is full; the buffer is 16; at most 8 subscriptions per
recipient, past which the new connection is refused with `ResourceExhausted`; `close` ends
everything and refuses new subscriptions; every send and close happens under the hub
lock. The handler authenticates the caller, returns `Unimplemented` when no subscriptions
are configured (before any store read), resolves the workspace once, subscribes, sets
`X-Accel-Buffering: no` before the first `Send`, then loops on the heartbeat and lifetime.
A lifetime end and a client disconnect both return `nil`.

`Server.Shutdown` closes the notifier before Echo, or every stop waits out the graceful
period for streams that never end; the nil guard covers a hand-built `Server{}` in a
lifecycle test.

## Frontend

`api/notification.ts` holds the inbox, count and read calls, the subscription wrapper and
the channel/delivery/route CRUD; `store/modules/notification.ts` holds the Pinia state and
the reconnect loop; `lib/notificationText.ts` renders type + detail into keys, params and
an href (with a coverage gate); `NotificationBell.vue` owns the badge (99+, recent
`RECENT_NOTIFICATION_LIMIT = 8` messages) and the stream lifecycle; `NotificationsPage.vue`
and `settings/NotificationSettingsPage.vue` are the two surfaces.

- The badge is driven by `unseenCount` and the page actions by `unreadCount`; swapping them
  either makes the badge impossible to clear or grays out "mark all read" while unread
  messages exist. `openInbox()`/`closeInbox()` are a counter because the dropdown and the
  page can be open together; a stuck count leaves the badge dark. Opening clears the badge
  optimistically and a failed `MarkNotificationsSeen` is swallowed, because the next
  refresh restores it. A message arriving while open must be prepended by `applyArrival`,
  or the watermark silences a message the user never saw.
- A `generation` counter, not a boolean, guards the reconnect loop: unmount and mount in
  one tick would leak a stream per layout switch until the 8-subscription cap refuses every
  new one. Every (re)connect refreshes once — the fallback for lost events and restarts —
  and with the inbox open it re-sends `MarkNotificationsSeen` if the server reports newer
  messages. `Unimplemented` is the only error not retried; two tickets keep a stale
  response from overwriting a newer one.
- Deep links point at the instance, OpenLineage events, namespace mapping or agent page.
  The **61** `notifications.*` keys exist in both locales; the `no-unused-keys`
  exemptions are the shared list that `frontend/eslint.config.mjs` imports from
  `frontend/scripts/check-vue-i18n.mjs` (`eslintUnusedKeyIgnores`), so that script's
  reference patterns must change with this family's property names.

## Invariants

| Rule | Breaking consequence |
| --- | --- |
| Every query is scoped by `recipient_id` | One user can read, mark or delete another's messages |
| Read state is `read_at` plus the unread partial index | The badge count degrades to a `payload->>` scan |
| Opening the inbox only advances the watermark, never writes `read_at` | Unclicked messages become read and "mark all read" loses meaning |
| The watermark is stamped by the server's `NOW()` | A client clock could permanently exclude a message from "new" |
| Both counts come from one statement, and the badge uses `unseen_count` only | The badge can exceed the unread count, or return to "click every message to clear it" |
| `openInbox()` and `closeInbox()` are paired | A stuck counter leaves the badge dark until a reload |
| Deduplication keeps the unique index plus `ON CONFLICT DO NOTHING` | The process gate alone lets duplicates through after a restart or on a second replica |
| A notification write stays best-effort; `publish` stays non-blocking and drops an overflowing subscription | A full table or a slow client fails the sync or blocks the runner goroutine |
| Subscription methods carry no permission annotation; `SubscribeNotifications` carries no audit annotation | A custom role cannot see its own messages, or every connection writes to the permanent ledger |
| `X-Accel-Buffering: no` precedes the first `Send` | A proxy buffers the stream and the bell never updates |
| `Shutdown` closes the notifier before Echo | Every stop waits out the graceful period and logs an error |
| The frontend uses `generation`, not a boolean, and refreshes on every (re)connect | A layout switch leaks streams, or lost events and restarts leave the inbox drifted |
| One operation reports a database's first result; `notification` rows are never pruned | Retries send two noisy messages, or the record of what the system told a user is lost |

## Failure modes

| Situation | Behavior |
| --- | --- |
| The count query fails | Logged; the event still delivers with `CountsKnown = false` and the frontend asks again |
| The unique index suppresses the insert | No row, no error, nothing published |
| A write fails | The suppression key is released; the caller ignores the error and continues |
| A subscriber buffer fills | The hub closes that stream; the client reconnects and refreshes; the message remains |
| One recipient exceeds 8 subscriptions, or the 30-minute bound is reached | The new connection gets `ResourceExhausted`; the stream ends normally and the client reconnects and re-authenticates |
| The session expires or is revoked on an open stream | The next `Receive` is refused with `Unauthenticated` |
| A server-streaming call hits the 401 replay path | The session interceptor's replay fails for server streaming (connect-web builds a one-shot iterable, so the replay throws `missing request message`, `Unknown`). Renewal did happen; the reconnect loop restores the stream and the caller sees one `Unknown`. `ExplainSQL` is affected the same way; pre-existing |
| More than one replica, or another tab marks read | A message written elsewhere is not pushed and another tab's read is not echoed; the reconnecting or visibility refresh covers both |
| Application and database clocks disagree | The watermark uses the database's `NOW()` and `created_at` the application clock; a slow application clock can mark a just-written message as seen. Invisible with one host and NTP; recorded in a code comment |

## Open items

1. Cache the single streaming input message in `frontend/src/api/session.ts` so the 401
   replay works for server streaming; it touches the shared session path and affects
   `ExplainSQL` too.
2. Cross-replica delivery via PostgreSQL `LISTEN/NOTIFY`, with reconnect and dedup; and
   push read-state and watermark changes for exactly consistent multi-tab badges, which
   first needs an echo story.
3. Desktop notifications (a new branch in `applyArrival`), and a decision whether the
   "valid event but no lineage" warning should notify.
4. Re-check token revocation on each heartbeat to tighten the 30-minute bound. Letting
   administrators message a user needs a free-text detail branch, a send RPC and an audit
   decision.
5. Harden the operation aggregator for multiple replicas or frequent restarts with a
   hidden pending row plus a startup sweep. Tune the suppression window only by moving the
   process gate and the database bucket together, and unify the watermark and application
   clocks before multiple replicas.

## Outbound delivery

The outbound surface extends the inbox. Its operator-facing configuration and behavior live
in [docs/notifications.md](../../docs/notifications.md); this section records the decisions
behind them. One `Send`, `SendToWorkspaceAdmins` or `SendToRecipients` call is one event: N
inbox rows plus 0..M deliveries selected by channel subscription. Inbox and deliveries stay
decoupled. The quiet-hours schedule is also the data-quality maintenance window: checks and
inbox writes continue, and only outbound delivery is deferred, except for
`bypass_quiet_hours` channels (see [data-quality.md](data-quality.md)).

A `notification_channel` is a workspace-level destination, kind one of `WEBHOOK`,
`DINGTALK`, `WECOM`, `FEISHU`, `EMAIL`. HTTP kinds keep the endpoint URL and signing secret
encrypted in `secret` and never return them; EMAIL has no credential of its own and sends
through the workspace SMTP setting. A `notification_delivery` is one event × channel,
state PENDING/SENDING/SENT/DEAD, with `next_attempt_at` as both retry time and SENDING
lease. `event_id` is random per Send call and shared by the event's inbox rows; UNIQUE
`(event_id, channel_id)` gives one delivery per event per channel.

### Decisions

| Decision | Why |
| --- | --- |
| A channel is workspace-level; credentials are write-only and `v1:` AES-256-GCM encrypted, and channel management reuses `settings.get`/`settings.update` with writes audited and test sends not | Outbound capability belongs to the workspace and the permission catalog does not grow; a test changes nothing |
| Subscription filtering is per channel: `event_types` (empty = every type that existed when it was configured) and `min_severity` | An empty list must never silently select a later periodic type |
| Asynchronous outbox plus a dispatcher (2-second tick, 20 claims, 4 concurrent posts, 5-minute lease); backoff 1m/5m/15m/1h/6h, then DEAD; manual retry resets attempts; EMAIL and HTTP kinds share the path | A producer never fails on a channel problem; an expired lease is reclaimable, and there is no second delivery path |
| Render at send time (channel language, empty = `en-US`); the row stores a structured event and audience user ids, never names or addresses | A template fix applies to queued deliveries; addresses resolve at send time |
| The webhook body is stable JSON (`eventId`, `deliveryId`, `eventType`, `severity`, `title`, `text`, `link`, `occurredAt`); event and delivery headers are always sent, and `sign=true` adds an HMAC-SHA256 signature header | Destinations can script against it; secrets stay out of logs and errors |
| Deep links come from `WorkspaceProfileSetting.external_url`; with none, no link | A link must open in a browser |
| Deliveries are kept 30 days and queued rows never pruned; redirects are never followed, and a 3xx is a failed attempt | Long enough to explain a missed alert, bounded in size; a default client would turn a redirect into a bodyless GET or re-POST the signature elsewhere |
| Outbound SSRF risk is carried by administrators-only configuration, audit and documentation; an IM host allowlist can be added later | The same accepted decision as instance credentials reaching any database |
| DingTalk, WeCom and Feishu are separate kinds with their own payload and signing; all check HTTP 2xx and the response `errcode`/`code` | "HTTP 200 plus a business error" differs per vendor; trusting the status records a refusal as delivered |
| Email uses the workspace SMTP setting; the password is encrypted, write-only and audit-redacted; authentication requires TLS; recipients are the fixed addresses plus optionally the event audience, resolved at send time, deduplicated case-insensitively, capped at 50; empty resolution is immediate DEAD while missing SMTP retries | One deployment has one sending server and a password must never cross plaintext; email has no signed-in-user layer, so "who should receive this" must come from the event |
| Quiet hours are a workspace setting: weekly windows plus one-off maintenance windows of at most 30 days, which a channel may bypass; a delivery inside a window is held (returned to PENDING at window end, attempt decremented, `quiet_deferred`) and never dropped; a read, parse or DST-gap failure fails open | One on-call rhythm per deployment; on-call must page through maintenance; alerts are not lost or charged to the retry budget |
| Routing rules are a separate `notification_route` table matching event types, minimum severity, owners and IAM groups, and are additive; matching happens at enqueue time and fails open to the subscription fan-out; a routed channel still passes its own filter, and a `routed_only` channel with no matching rule is skipped | Team channels opt out while normal channels are unaffected; the event decides its channels when written, and a failure prefers a duplicate to a lost alert |
| "Escalate to a second channel after N hours" is not implemented | Not every deployment wants automatic escalation; a default would page twice |
| A quality-agent escalation is a per-owner inbox message, not a broadcast, and its detail carries resource names and a bounded title/question | The owner is the person who can decide; no prose in the row keeps both languages and forward compatibility |
| The weekly digest is delivery-only: no inbox row, no audience, deterministic `event_id = dqdigest:<ISO week>`, only explicitly subscribed enabled channels, `routed_only` and address-less EMAIL channels skipped | A new periodic message must be opt-in; a digest has no audience for routing or e-mail, and the deterministic id makes a retried tick or restart a no-op |

### Failure modes

| Situation | Behavior |
| --- | --- |
| The channel is deleted | Its queued deliveries cascade with it |
| A credential is missing or cannot be decrypted | A missing credential is a configuration error and immediate DEAD; an undecryptable one retries until the key is fixed |
| The channel list cannot be read | Claimed deliveries go back to PENDING without spending an attempt |
| 5xx, timeout or network error | Backoff retry; the URL is replaced with `<endpoint>` in `last_error` |
| 4xx or 3xx | Retried until the budget is spent (3xx never followed); after a fix a DEAD delivery can be retried by hand with a reset attempt count |
| An IM endpoint returns 200 with a non-zero vendor code | Treated as a failure, with the vendor text in `last_error` |
| SMTP is not configured, or an EMAIL channel resolves no recipient | Missing SMTP retries and self-heals once configured; no recipient is immediate DEAD and a fixed address allows a manual retry |
| A delivery lands in a quiet window | Returned to PENDING at window end with the attempt decremented, metric `deferred`, adapter not called |
| Quiet hours are saved or disabled | `ReleaseQuietDeferredDeliveries` marks held rows due; a still-active window re-defers them next tick |
| The inbox write succeeded but the outbox write failed | Logged only; the message remains and the event is not delivered (best effort) |
| The dispatcher crashes mid-POST | The lease expires and the next tick reclaims the row, counting an attempt |
| A test send is pressed | The adapter is called directly; nothing queued, no delivery, no audit |
| Digest enqueue fails, or a crash splits enqueue and period recording | Retried within 24 hours; the deterministic event id makes the retry a no-op, and a recorded-but-unqueued period is skipped |

## Where things live

- **Migrations**: `backend/migrator/migration/0.1/0017##notification.sql`,
  `0018##notification_seen.sql`, `0033##notification_outbound.sql` and
  `0034##notification_route.sql`, mirrored in `LATEST.sql` and pinned by the migrator's
  consistency test.
- **Component** `backend/component/notification/`: `service.go` (write path and
  suppression), `hub.go`, `data_quality_agent.go`, `data_quality_digest.go`,
  `outbound_queue.go` (fan-out and subscription filter), `outbound_routing.go`, and
  `outbound/` (receiver factory, validation, render, webhook, chat kinds, email,
  recipients, quiet-hours schedule). **Store**: `backend/store/notification.go`,
  `notification_channel.go`, `notification_delivery.go`, `notification_route.go`,
  `email_setting.go`, `quiet_hours.go`, `quality_digest.go`; two SQL shapes are invariants
  — the claim's `RETURNING` must qualify with `d.` (the `FROM due` subquery also has an
  `id`, so unqualified fails with 42702) and the completion statement must cast its reused
  parameter explicitly (inferred twice it fails with 42P08 and deliveries stay SENDING
  until the lease reclaims them).
- **API and wiring**: the three `backend/api/v1/notification*_service.go` files,
  `backend/server/grpc_routes.go` and `server.go`. **Runners**:
  `backend/runner/notificationdispatcher/`, `backend/runner/qualitydigestrunner/`, the
  quality session and patrol runners, and `backend/runner/schemasync/operation.go`.
  **Ingestion notifications**: `backend/api/v1/openlineage_handler.go` and
  `backend/plugin/openlineage/resolver.go`. The frontend files are named in the section
  above; the bell mounts at the bottom of `AppSidebar`, `/notifications` is reached through
  its "view all", and there is no server-side switch.
- **Tests**: component tests for hub, service, digest and the outbound queue; store tests
  for recipient scope and claim SQL; dispatcher tests with a fake SMTP server; digest
  idempotency; `backend/common/resource_name_test.go`; the notifier-first lifecycle test;
  the integration suite in `backend/test/integration/runner/`; and the frontend tests
  beside each file. **Gates**: `go test` on those packages, the integration suites,
  `pnpm --dir frontend test run`, and the buf gates for a proto change.
