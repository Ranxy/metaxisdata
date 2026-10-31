# PR-2 review — outbound email + DingTalk / WeCom / Feishu chat robots

- **Branch**: `feat/dq-outbound-notifications`
- **Commit reviewed**: `2bf9666 feat(notification): deliver outbound events to email and chat robots`
- **Parent**: `17f4ed6` (PR-1 review fixes; already reviewed) · main is still at `6f43286`, so **PR-1 has never been merged or released**
- **Range**: `17f4ed6..2bf9666` — 61 files, +7,904 / −586 (proto store + v1, store email setting, outbound adapters, queue/dispatcher, API, frontend, docs, tests)
- **Method**: read the governing docs first (`AGENTS.md`, `backend/AGENTS.md`, `proto/AGENTS.md`, `frontend/AGENTS.md`, `backend/migrator/AGENTS.md`, `.agents/docs/notifications.md`), then the full diff; ran all gates below plus one throw-away Go program against `common.ProtojsonUnmarshaler` to confirm the pre-release upgrade behaviour of the renamed secret.
- **No source or generated file was modified.** The working tree is *not* clean at the end of the review: `.agents/docs/notifications.md` carries an **uncommitted** one-line addendum (mtime 18:47:41, not mine) that documents the PR-1 → PR-2 secret rename and the re-enter-the-endpoint recovery. I reviewed it; it is accurate. Everything else below is about the committed state `2bf9666`.

## Verdict

**approve with fixes** — **0 blockers, 0 majors, 10 minors, 12 nits.**

Nothing here is a correctness or security blocker: the SMTP transport is built on the standard library with TLS verification on, redirects are refused for every HTTP kind, the write-only credential contract extends cleanly to the SMTP password, audience resolution is done at send time with deleted accounts skipped, the kind/config/secret update matrix is coherent, and the tests actually pin behaviour rather than just happy paths. The minors are: one behavioural rough edge for email (a single rejected recipient cancels the whole delivery), one MIME conformance gap, the pre-release-only secret rename, a lint gate that cannot see new findings, a handful of docs/coverage gaps, and the usual nits.

---

## Verified as correct (no need to re-check)

**Upgrade / compatibility (scrutiny 1)**
- PR-1 is confirmed unreleased: `git merge-base --is-ancestor 5271d3a main` is false and `main` is still `6f43286`. The `NotificationChannelSecret` rename (`webhook_url`/`webhook_signing_secret` → `endpoint`/`signing_secret`) is therefore an acceptable pre-release change, and field numbers/types are unchanged so the wire format is still compatible.
- The additive changes are genuinely additive: the only lines removed from `proto/v1` + `proto/store` are the two renamed secret fields and comments; the five kinds, five config oneof arms, `audience_user_ids`, `SettingName.EMAIL`, store/v1 `EmailSetting` are all new field numbers. I verified the generated trees are in sync: `cd proto && buf generate` left `git status` unchanged (only the concurrent doc edit described above), so `backend/generated-go/`, `frontend/src/types/proto-es/` and `proto/gen/grpc-doc/` match the proto.
- No migration is needed and none was added: `setting` is a text `name` / polymorphic `value` table (`LATEST.sql:72-84`), `notification_channel.payload`/`secret` and `notification_delivery.payload` are JSONB/TEXT. `TestMigrateSchemaLATESTMatchesTheIncrementChain` passes against real PostgreSQL.
- Old-value behaviour of the rename (checked with `common.ProtojsonUnmarshaler`, `DiscardUnknown: true`): `{"webhookUrl":…,"webhookSigningSecret":…}` unmarshals **without error** into `endpoint=""`, `signing_secret=""`; a read-modify-write persists `{}`. Consequence and recovery are the minor #1 below.

**SMTP security (scrutiny 2)**
- TLS: implicit TLS uses `tls.DialWithDialer` with `ServerName: host, MinVersion: TLS1.2`; STARTTLS is explicit (`client.StartTLS` with the same config, so it fails closed if the server does not advertise it — no silent downgrade). Certificate verification is on in both paths; the connection deadline is set on the socket (`conn.SetDeadline`) so `net/smtp`'s missing context support cannot hold a worker past the attempt budget.
- Auth: PLAIN goes through `smtp.PlainAuth` (which itself refuses plaintext non-localhost), LOGIN has a small `smtp.Auth` implementation whose `Next` matches the base64-decoded `Username:`/`Password:` challenges, CRAM-MD5 uses `smtp.CRAMMD5Auth`. `NewClient` sets `tls=true` for a `*tls.Conn`, so PLAIN over implicit TLS is accepted by the library. The API additionally rejects any authentication over `EMAIL_SMTP_ENCRYPTION_NONE` (`setting_service.go` `validateEmailSetting`), which is tested.
- Password handling: the plaintext never enters the stored payload (`mergeEmailSettingUpdate` returns it out-of-band and `emailSMTPConfigFromV1` does not copy it — asserted by `TestMergeEmailSettingUpdateKeepsThePasswordOutOfThePayload`); the store seals it with `credentialCipher()` (same `v1:` AES-256-GCM path as channel endpoints); `GetEmailSetting` strips it and reports `HasPassword`; the API converts to `password_set` and never copies the password; an unnamed/empty password keeps the stored ciphertext (store re-reads it rather than trusting the request); the audit interceptor redacts it through the existing `password` marker, and `endpoint` was added to `IsSensitiveAuditField` (with the field-scoped test updated).
- Header injection: `from_name` and `Subject` are stripped of CR/LF before encoding; `From` is parsed and round-tripped; every recipient — fixed or audience — goes through `mail.ParseAddress` with a CR/LF reject in `recipients.go`/`NormalizeEmailAddresses`, and user email addresses are validated at account creation. The body is quoted-printable with a `text/plain; charset=utf-8` part, so non-ASCII text survives without 8BITMIME.

**IM protocols (scrutiny 3)**
- DingTalk: `sign = base64(HMAC-SHA256(key=secret, data=timestamp+"\n"+secret))` with a millisecond timestamp, appended as URL-encoded query parameters and recomputed per attempt — matches the vendor spec and is pinned by a test.
- Feishu: `sign = base64(HMAC-SHA256(key=timestamp+"\n"+secret, data=""))` with a second-resolution timestamp in the request body — matches the vendor spec; both the `{"code","msg"}` and legacy `{"StatusCode","StatusMessage"}` answer shapes are checked.
- WeCom: markdown with the robot key in the endpoint; content truncated to the 4096-byte limit with a UTF-8-safe helper.
- All three treat a non-2xx status **and** a non-zero `errcode`/`code` as a failed attempt (a 200 with a business error is not a delivery), which is the failure mode that would otherwise mark rejected messages as sent.
- Redirects are refused for every HTTP kind (`CheckRedirect` → `ErrUseLastResponse`) and a 3xx is a failure; transport errors are sanitized against both the raw and the parsed/normalized endpoint, and the `last_error`/test-send answer never contains the robot token. Both are covered by tests.

**Email recipient semantics (scrutiny 4)**
- Audience identity: `Send` stores `[recipient_id]`, `SendToWorkspaceAdmins` stores every administrator, and `deliveryEvent` writes them to `NotificationDeliveryEvent.audience_user_ids`; the payload holds ids, not addresses (security-posture updated accordingly; the admin delivery API exposes no payload/audience).
- Resolution happens in the dispatcher at send time through `GetUserByID` (which uses `ShowDeleted: true`); a deleted account yields an empty address and is skipped, a changed address resolves to the new one (user-update paths invalidate `userIDCache`), a resolver **error** aborts the attempt so it is retried rather than silently dropping a person, and a result that resolves to nobody is DEAD immediately with the reason and the dead counter incremented.
- Fixed addresses are normalized/validated at write time and re-validated at send time; recipients are de-duplicated case-insensitively, deduped against each other, and bounded at 50; an unconfigured SMTP setting is a retry (configuration the administrator can close), while adapter-construction/config errors are DEAD.
- Privacy: the delivery payload carries no address, only ids, and delivery rows keep the 30-day retention.

**Channel update semantics (scrutiny 5)**
- Kind is immutable (an update-mask `kind` path is `InvalidArgument`; the stored kind is cloned), the config oneof must match the kind on create, on a masked `config` update and on read conversion, an empty mask is a full replace of the mutable fields and still requires a matching config, a masked update is validated *after* the merge, and unknown mask paths are rejected.
- The secret keeps the write-once contract: an omitted or empty part keeps the stored half, so rotating only the endpoint or only the signing secret works; EMAIL channels take no credential (a supplied one is rejected), and a masked update that omits `display_name` is no longer rejected now that validation runs after the merge (the PR-1 fix holds).

**EmailSetting API (scrutiny 6)**
- `GetEmailSetting` returns an empty message when unset (no 404) and reports only `password_set`; `UpdateEmailSetting` requires a mask (same convention as `UpdateWorkspaceProfileSetting`), supports `from`, `from_name`, `smtp` and the `smtp.*` sub-paths, rejects unknown paths, and validates from (parse + exact round-trip), from_name (CR/LF and 100 runes), host (length and disallowed characters), port range, encryption/auth enumerations, username+stored-password presence for every auth mechanism, and refuses auth over unencrypted transport.
- Clearing both `from` and `smtp.host` is the explicit off switch and deletes the row rather than persisting a half-configured server; the handler re-reads and returns the stored state; `UpdateEmailSetting` is audited, `SendTestEmail` is deliberately not (and its proto/comment/docs now say so consistently); `TestNotificationChannel` lost its old audit annotation in the same spirit.

**Queue / dispatcher (all three kinds plus email)**
- The event-level audience is plumbed without changing event identity (`(event_id, channel_id)` unchanged, one delivery per event per channel); the email path reads the SMTP setting once per attempt, resolves recipients before posting, and records SENT/DEAD/PENDING using the existing budget/backoff/metrics; `RequeueNotificationDelivery` resetting `attempts` (PR-1 fix) still holds and the maintenance docs were updated.

**Frontend**
- The dialog switches fields per kind (endpoint/signing for webhook+DingTalk+Feishu, endpoint only for WeCom, recipients+audience for email), kind is fixed when editing, changing kind resets kind-specific fields, the email flow sends `kind` + the matching config and **no secret**, the update mask still names `config`, and editing starts with blank write-only fields so the stored endpoint/signing secret/password are kept.
- The SMTP card loads the setting (with STARTTLS/PLAIN/587 defaults for an unset workspace), always sends the whole `smtp` message so an empty password keeps the stored one, reloads after save, gates save/test on `settings.update` and shows a read-only notice without `settings.get`, and clears the password field after load/save. `buildUpdateEmailSettingRequest` is unit-tested for the "never name `smtp.password`" contract.
- i18n: all new keys exist in both locales, the glossary gained DingTalk/WeCom/Feishu/SMTP and `.agents/docs/i18n-glossary.md` was regenerated; `pnpm i18n` passes.

---

## Minors

| # | Location | Failure scenario / why it matters | Fix |
| --- | --- | --- | --- |
| m1 | `proto/store/store/notification.proto:283-295`, `backend/store/notification_channel.go:232` (encrypt), `backend/common/utils.go:18` (`DiscardUnknown: true`) | **Pre-release-only upgrade break, silent:** a channel created on a PR-1 build stores `{"webhookUrl":…}`; after this commit it decrypts successfully but parses to an **empty** `endpoint` (verified). Deliveries then dead-letter `"the webhook channel has no endpoint"`, while `secret_set` is still true so the UI gives no hint; saving an unrelated edit fails with "a channel endpoint is required" until the endpoint is retyped. Acceptable because PR-1 was never released, but the failure is silent. | Either accept the old keys once on read (`if endpoint == "" { endpoint = legacy.GetWebhookUrl() }`) or make the UI/API surface "credential unreadable, re-enter the endpoint" instead of `secret_set: true`. At minimum keep the uncommitted doc addendum (already accurate) and mention it in release notes. |
| m2 | `backend/component/notification/outbound/email.go:181-186` | **One rejected recipient cancels the whole delivery.** The RCPT loop returns on the first failure, so with fixed `oncall@…` plus audience, one bad address (a typo domain, a relay that rejects an unroutable domain, a deactivated mailbox on a strict relay) means *no one* receives that notification; the attempt is retried six times over ~7h and then DEAD, and every later event fails the same way. | Continue past a rejected recipient: collect accepted ones and the rejected addresses, deliver if at least one was accepted, and record the skipped addresses in `last_error`/the delivery row; only fail when none is accepted. (Sending one transaction per recipient would also solve it, and would fix m3.) |
| m3 | `backend/component/notification/outbound/email.go:247` | **All recipients are in one `To:` header.** An external fixed address (on-call list, shared mailbox) learns the internal rule owner's / administrators' email addresses, and vice versa, because every notification exposes the full audience. | Put the fixed addresses and the audience in `Bcc:` with `To:` set to the From address (or a role address), or send one message per recipient. |
| m4 | `backend/component/notification/outbound/email.go:248` | **Subject is a single unfolded RFC 2047 encoded-word.** `mime.QEncoding.Encode` does not split or fold; a non-ASCII data-quality title is encoded at ~9 characters per CJK character, so any title longer than a few dozen Chinese characters violates the 75-character encoded-word limit, and a very long title (rule titles have no length cap) can push the `Subject:` line toward RFC 5322's 998-octet limit — receivers may garble it or reject the message. | Truncate the subject (e.g. 120 runes plus `…`) before encoding, and/or fold into several encoded words; bound the subject the same way `render.go` bounds error text. |
| m5 | `backend/component/notification/outbound/email.go:198` | **A failed `QUIT` after `DATA` is recorded as a failed attempt.** The server has already accepted the message, so the retry sends a duplicate email; the docs promise at-least-once but this duplicate is avoidable. | Treat a `Quit` error as success (ignore it, or log at debug): the transaction is complete once the DATA terminator is accepted. |
| m6 | `golangci-lint` configuration vs. new code: `backend/api/v1/setting_service.go:474`, `backend/store/email_setting.go:98` | **The lint gate cannot see new findings.** The repo has 181 pre-existing `revive/multiline-if-init` findings; golangci-lint's default `max-same-issues: 3` reports only three of them, and which three varies per run (I saw `auth/authenticator.go`, `plugin/db/pg/sync.go`, then `migrator/migrator.go`). Uncapped (`--max-same-issues=0 --max-issues-per-linter=0`), the branch adds **7** findings in changed files, **2 of them new in PR-2**: the multi-line `if _, err := receiver.Post(..., outbound.Message{…})` in `SendTestEmail` and the multi-line `if _, err := s.UpsertSetting(..., &SetSettingMessage{…})` in `SetEmailSetting`. The default run therefore "passes" the same rule everywhere else while hiding the new violations. | Hoist the composite literal to a variable before the `if` in both places, and raise the cap in CI (or add a baseline) so new findings cannot hide behind the existing debt. |
| m7 | `backend/api/v1/notification_outbound_service.go:566` (`convertToV1NotificationChannelConfig`), `backend/api/v1/setting_service.go:415-486` (handlers), `backend/store/email_setting.go` (password keep/clear); `backend/runner/notificationdispatcher/notificationdispatcher_test.go:312` (fake SMTP) | **Coverage gaps in the riskiest new code.** (a) The read converter for DingTalk/WeCom/Feishu/email is untested (only the webhook case exists) — a wrong case mapping would show the wrong config in the UI; (b) `GetEmailSetting`/`UpdateEmailSetting`/`SendTestEmail` handlers are untested, in particular the clear-by-empty delete path and `password_set` propagation (only the pure merge/convert/validate helpers are); (c) the store's password keep-vs-replace read (`password == nil` branch) is untested; (d) the SMTP transaction is only exercised on the plain `NONE`/`NONE` path — the custom LOGIN state machine, STARTTLS, RCPT-failure and QUIT branches never run in tests. | Add the four-kind converter table test; handler tests with a fake store for get/update(clear)/password_set; a loopback test with a self-signed certificate for STARTTLS and a fake server that advertises `AUTH LOGIN`; a test for the RCPT-failure behaviour once m2 is fixed. |
| m8 | `docs/notifications.md:118-121` (and the email section generally) | **Operator docs are slightly off on four points.** (a) "capped at 50 recipients" reads as truncation, but the code *fails* the delivery once >50 (retried, then DEAD); (b) the page never says how to turn email delivery off (clear both From and host); (c) "the delivery record shows an empty endpoint response" — there is no response column, only an empty `last_error`, so the sentence is confusing; (d) a **channel** Test on an EMAIL channel mails the fixed recipients (only the SMTP-card "Send test email" goes to the caller), which the docs do not say. | Say "a delivery with more than 50 recipients fails", document the From+host clear as the off switch, drop or reword the "empty endpoint response" sentence, and add one line about what a channel Test sends for email. |
| m9 | `backend/migrator/migration/LATEST.sql:73-76` | The `setting` DDL comment enumerates the `SettingName` values (`AUTH_SECRET, BRANDING_LOGO, WORKSPACE_ID, WORKSPACE_PROFILE, PASSWORD_RESTRICTION, ENVIRONMENT, ENCRYPTION_KEY`) and its `value` comment describes the payloads; PR-2 adds `EMAIL` and the `EmailSetting` payload but the schema comment now under-describes the column. No DDL change is needed, but the comment is the schema-level documentation of the enum. | Add `EMAIL` (and its payload) to the comment, and confirm `TestMigrateSchemaLATESTMatchesTheIncrementChain` still passes (it compares structure; comments may not be compared, but check). |
| m10 | `.agents/docs/README.md:15`, `.agents/docs/metrics.md:485` | Two stale reference lines remain from the PR-1 fix: the index row still says "The **six** background runners" while `runner.md` now documents seven (the PR-1 fix updated the page but not the index row), and the delivery-duration metric is still described as "单次 **HTTP** 投递耗时" although `ObserveNotificationDeliveryDuration` now times SMTP attempts too. | Update both lines ("seven runners"; "one outbound delivery attempt, HTTP or SMTP"). |

## Nits

| # | Location | Note |
| --- | --- | --- |
| n1 | `backend/component/notification/outbound/channel_test.go:135` | `_ = context.Background()` with an otherwise-unused `context` import ("keeps the signature honest") — `NewReceiver` takes no context. Remove the import and the line. |
| n2 | `backend/component/notification/outbound/chat.go:33-49`, `dingtalk.go:33`, `wecom.go:29`, `feishu.go:37` | A fresh `http.Client` (and transport) per attempt, unlike the webhook receiver which holds one; no connection reuse across retries. Hoist the client into the receiver. |
| n3 | `backend/component/notification/outbound/channel.go:50` | A WeCom channel accepts and stores an unused signing secret (`validateEndpointChannel(secret, false)` neither requires nor rejects it). Either reject a credential the kind cannot use (as EMAIL does) or say in the proto/doc that it is ignored. |
| n4 | `backend/api/v1/notification_outbound_service.go:431-478` (`mergeNotificationChannelUpdate`) | A full update that names a *different* kind is silently ignored when its config matches the stored kind (the kind comes from the cloned payload). Return `InvalidArgument` for a kind change even without a mask, as the masked path already does. |
| n5 | `backend/component/notification/outbound/email.go:36-48` vs `backend/api/v1/setting_service.go` `validateEmailSetting` | `NewEmail` re-implements most of the settings validation but omits the "authentication requires transport encryption" rule; a setting written by any future path would let the custom LOGIN mechanism send a password in the clear. Extract the rule and call it from both. |
| n6 | `backend/api/v1/setting_service.go` (`validateEmailSetting`, host check) | `strings.ContainsAny(host, " \t\r\n/:")` rejects IPv6 literal hosts (`[::1]:25` style is impossible to configure). Allow bracketed literals if that matters, or document host names only. |
| n7 | `backend/component/notification/outbound/email.go:240-266` | No `Message-ID` (and no `Auto-Submitted: auto-generated`); a machine alert looks like a personal mail to some filters, and Message-ID is what receivers dedupe on. |
| n8 | `backend/component/notification/outbound/dingtalk.go:63`, `feishu.go:33` | Robot timestamps come from the app clock; both vendors tolerate only ~1 h skew. The inbox reference documents the app/DB clock split, but the robot-signing consequence is not documented. |
| n9 | `backend/api/v1/setting_service.go:452-486` (`SendTestEmail`) | No rate limit or cooldown: a `settings.update` holder can loop test emails through the workspace SMTP. Consider a small in-process cooldown, or state that it is deliberately unbounded. |
| n10 | `frontend/src/pages/settings/NotificationSettingsPage.vue` (SMTP card) | "Send test email" uses the **stored** setting, not the dirty form: a user who types a new host and clicks it tests the old server. Disable it while the form is dirty, or add "save first" to the hint. |
| n11 | `frontend/src/pages/settings/NotificationSettingsPage.vue:698` | `canGetEmailSetting` duplicates the route's `settings.get` meta (the page is unreachable without it). Harmless, but it is dead logic to maintain. |
| n12 | `backend/component/notification/outbound/email.go:184` | The rejected address is echoed into `last_error` ("SMTP RCPT TO %s failed"), which every `settings.get` holder can read and which is kept 30 days. It is PII (not a credential); consider omitting or masking the address. |

---

## Test / coverage assessment (scrutiny 7)

Genuinely behaviour-pinning, not just happy paths:
- IM: payload shape per vendor, both signature formulas recomputed independently in the test, non-zero `errcode`/`code` treated as failure, legacy Feishu answer shape, endpoint sanitization for all three, redirect refusal asserted by counting target hits.
- Email: address normalization (dedupe, display-name reject, CR-LF reject, 21st address), audience resolution/skip/dedupe/resolver-error/50-cap, MIME header assertions including a header-injection attempt in the from name, `NewEmail` completeness matrix, `Post` without recipients.
- Dispatcher: a **loopback fake SMTP server** drives the real `net/smtp` transaction and asserts the delivered `To:`/`From:`/`Subject:`; unconfigured SMTP → PENDING with the reason; audience resolving to nobody → DEAD with the reason; plus the PR-1 webhook/redirect/backoff/lease tests.
- API: payload → store conversion for the email config, config/kind mismatch rejection, full-replace/mask/kind-immutability, secret conversion; EmailSetting merge/keep-rotate/clear/validation matrix and converter.
- Frontend: create-email-channel sends kind+config and **no secret**, refuses no-recipient channels, DingTalk shows robot signing fields and not the webhook HMAC copy, SMTP load/save sends a typed password and an **empty** password on the second save (store keeps it), permission gating, test email; `setting.ts` request-builder tests pin the mask and "never name `smtp.password`".

Remaining gaps (m7): new-kind read converter, the three email handlers, the SMTP store keep/clear branch, and the SMTP transaction's security-relevant branches (STARTTLS certificate handling, custom LOGIN state machine, RCPT failure, QUIT). A loopback TLS server with a self-signed cert is cheap and would close the most valuable part of the last gap without a real external SMTP dependency. No handler-level test covers the "clear both From and host deletes the setting" path, which is the only way an operator turns email off.

---

## Gates actually run (from the branch worktree)

| Gate | Result |
| --- | --- |
| `go build ./...` | **pass** |
| `gofmt -l backend/` | **pass** (no files) |
| `go test ./...` | **pass** (all packages; no failures) |
| `go test -tags=integration ./backend/migrator/ -run TestMigrateSchemaLATESTMatchesTheIncrementChain` | **pass** (real PostgreSQL via testcontainers, 5.7 s) |
| `golangci-lint run --allow-parallel-runners` | **exit 1** with 3 `revive/multiline-if-init` findings in files this branch does not touch (which three varies per run). Uncapped (`--max-same-issues=0 --max-issues-per-linter=0`): 181 findings repo-wide, 7 in branch-changed files, **2 new in PR-2** — see m6. |
| `cd proto && buf lint` | **pass** |
| `cd proto && buf generate` + `git status` | **no generated drift** (proto-es, generated-go, grpc-doc all in sync) |
| `frontend`: `pnpm -s biome:ci` / `lint:ci` / `i18n` / `type-check` | **pass** (exit 0 each; i18n checks missing/unused/consistency/placeholder/parity, key sorter and the glossary, including the four new terms) |
| `frontend`: `pnpm -s test run` | **pass** (133 files / 1,335 tests) |

Out-of-band: one throw-away Go program using the repo's `common.ProtojsonUnmarshaler` and generated store types to confirm the pre-release secret-rename behaviour quoted in m1. No database, Docker container or external SMTP server was needed.

---

## Suggested fix order

1. **m2 + m3 + m5** in one email-transport change: deliver to accepted recipients, put recipients in `Bcc`, ignore a post-DATA `Quit` failure — these three are the only minors with a real delivery impact.
2. **m1**: decide the pre-release migration story (small read fallback, or the doc-only note that is already in the working tree) and make `secret_set`/the UI not claim a usable credential.
3. **m6** (two literal hoists + raise the CI cap / add a baseline) so the lint gate can see new findings again.
4. **m7** tests for the new-kind converter, the email handlers and the SMTP security branches; **m4** subject folding/truncation; **m8/m9/m10** doc and DDL-comment corrections.
5. Nits in any order; n2 and n3 are the only ones touching code behaviour.
