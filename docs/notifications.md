# Notifications

Every event the platform reports has two destinations: an **in-app inbox**
message for the people concerned, and, when an administrator configures one, an
**outbound channel** that receives the same event outside the product. The inbox
is always on. The outbound side is opt-in and starts empty.

## The inbox

The bell at the bottom of the sidebar collects the messages the deployment sends
to you: the outcome of a schema sync you asked for, OpenLineage ingestion
failures, and data quality issues — opened, recovered, or a check that could not
run. Opening the inbox clears its badge; individual messages are marked read when
you click them. Messages are kept forever and are personal to their recipient.

## Outbound channels

Settings → **Notifications** is the administrator surface.

A channel is one destination and the rules for what it receives:

- **Type** — the protocol and the destination:
  - **Webhook** — a generic HTTP `POST` with a stable JSON body (described below).
  - **DingTalk**, **WeCom**, **Feishu** — group robots. The message is a markdown
    card (DingTalk, WeCom) or text (Feishu); the robot key is part of the URL.
  - **Email** — sent through the workspace SMTP server; recipients are the fixed
    addresses and/or the people the event concerns.
- **Endpoint URL** — where an HTTP delivery is posted (webhook and the three chat
  kinds). It is stored encrypted and is never returned by a read; leave the field
  empty while editing to keep the stored one.
- **Recipients** (email only) — the fixed addresses that always receive the
  message, and an option to also notify the audience the in-app message
  addressed: the rule's owner, the initiator of a sync operation, or the
  workspace administrators for an event that concerns nobody in particular. At
  least one of the two is required. An email channel fails while no SMTP server
  is configured.
- **Subscription** — which event types (data quality, schema sync, OpenLineage;
  nothing selected means all) and the **minimum severity**. A channel below the
  floor receives nothing; the inbox is unaffected.
- **Message language** — the outbound text is rendered in this language
  (`zh-CN` or `en-US`).
- **Signing** — webhook, DingTalk and Feishu can sign their requests; WeCom's
  robot key is the whole credential. When on, the webhook request carries
  `X-Metaxisdata-Signature: sha256=<hex>` computed with your signing secret over
  the raw body, plus `X-Metaxisdata-Event` and `X-Metaxisdata-Delivery` headers.
  DingTalk and Feishu verify a timestamp and signature (below).

Delivery is asynchronous: the platform queues the event, posts it, and retries a
failure with backoff (1m, 5m, 15m, 1h, 6h). After six attempts the delivery is
**dead**; fix the cause and press **Retry**, which grants a fresh six-attempt
budget. An event addressed to five
administrators still reaches a channel once.

### The webhook body

```json
{
  "eventId": "k3j9x0a2b7c1",
  "deliveryId": 42,
  "eventType": "DATA_QUALITY",
  "severity": "ERROR",
  "title": "Data quality: orders id not null failed on prod/app/public/orders",
  "text": "Status: PASS → FAIL · Severity: ERROR · Observed: 12 rows · Failed rows: 12/1000",
  "link": "https://metaxis.example.com/data-quality/executions/workspaces/ws-1/qualityExecutions/981",
  "occurredAt": "2026-10-31T09:12:44Z"
}
```

`link` is present only when the workspace has an **external URL** configured
(Settings → General); the message then points at the issue, execution or
instance page behind the event. The data-quality links carry the whole resource
name, because those detail pages read the name from the path. Redirects are not
followed: a 3xx answer is a failed attempt (a 301/302 would turn the POST into a
GET without the body, and a 307/308 would re-send the body and signature to a
host the endpoint chose). The body is a fixed shape: fields are added, never
renamed or removed.

### Verifying the signature

Recompute HMAC-SHA256 over the raw request body with the channel's signing
secret and compare it with the header. The endpoint URL and the signing secret
are written once and never shown again; rotate them by editing the channel and
filling the fields with new values.

### Chat robots

The three chat kinds speak their vendor's group-robot API:

- **DingTalk** — posts a markdown card to the robot's `.../robot/send`
  endpoint. With signing on, the request carries `timestamp` and `sign` query
  parameters, where `sign = base64(HMAC-SHA256(key=<secret>, data="<timestamp>\n<secret>"))`.
  A robot that is restricted by keyword or IP address does not need signing.
- **WeCom** — posts a markdown message to the robot's `.../webhook/send`
  endpoint; the key is the `key` query parameter, so the whole endpoint is the
  credential and there is nothing else to sign.
- **Feishu** — posts a text message to the custom bot's `.../hook/<token>`
  endpoint. With signing on, the body carries `timestamp` and `sign`, where
  `sign = base64(HMAC-SHA256(key="<timestamp>\n<secret>", data=""))`.

The robot answer's protocol code is part of delivery: an `errcode`/`code` that is
not zero is a failed attempt, even when the HTTP status is 200. WeCom truncates a
long message to its 4096-byte markdown limit.

### Email

Email delivery needs one workspace-wide SMTP server, configured on the same
page under **Email delivery (SMTP)**: the From address and optional name, host,
port, encryption (none, STARTTLS, or implicit TLS), authentication (none, PLAIN,
LOGIN, or CRAM-MD5), user name and password. Authentication cannot be combined
with unencrypted transport. The password is write-only: it is stored encrypted,
never returned by a read, and redacted from the audit log; leave the field empty
while editing to keep the stored one. **Send test email** sends a message to your
own address and tells you whether the SMTP transaction completed.

An email channel lists fixed recipients and/or follows the event's audience. The
audience is resolved when the delivery runs, so an owner who changes their
address receives it at the new one and a deleted account is skipped. One SMTP
transaction carries the fixed addresses and the audience together, capped at 50
recipients. Email has no application-level answer: a completed SMTP transaction
is the receipt, and the delivery record shows an empty endpoint response.

## Deliveries

The **Deliveries** card on the same page lists the most recent attempts: the
queued time, channel, event, state, how many attempts were made, when the next
attempt is due, and the last error. States are *pending* (waiting, or waiting for a retry), *sending*,
*sent*, and *dead* (out of attempts). Deliveries are kept 30 days; a channel's
queued deliveries are removed with it.

## Permissions and safety

Reading channels and deliveries needs `metaxisdata.settings.get`; changing a
channel, changing the SMTP setting, testing either, and retrying a delivery need
`metaxisdata.settings.update`. Channel changes, SMTP changes and delivery retries
are recorded in the audit log, with the endpoint credential and the SMTP password
redacted. A test send (channel or email) is not audited: it queues nothing and
its response is third-party text that does not belong in the permanent ledger.

The endpoint URL is treated as a credential for every HTTP kind: it is encrypted
at rest with the deployment data key, never returned by an API read, redacted
from audit rows, and replaced with `<endpoint>` in recorded delivery errors. The
SMTP password is encrypted the same way. Because the destination is chosen by a
workspace administrator, the deployment allows requests to any `http`/`https`
address the administrator enters — the same trust boundary as an instance's
database connection; see [security-posture.md](security-posture.md). Redirects
are not followed for any HTTP kind: a 3xx answer is a failed attempt.

Delivery is single-replica, like the rest of the background work: run one server
process or accept that a queued delivery is claimed by whichever replica polls
it. A server restart does not lose a queued delivery; an attempt interrupted
mid-request is retried after its lease expires.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| No outbound message arrived, but the inbox has one | The channel is disabled, its event types exclude the event, its minimum severity is above the event's, or no channel is configured at all |
| A delivery shows `dead` | The last error names the cause (DNS, TLS, timeout, HTTP status, endpoint body). Fix the endpoint, then **Retry** |
| A delivery says "email delivery is not configured" | Save the **Email delivery (SMTP)** card first; an email channel queues while the server is unset and fails once its retry budget is spent |
| Test send fails but real deliveries work | The test message is sent through the same adapter; a 4xx from the endpoint usually means the endpoint rejects the test's `link` or its body shape |
| Email test fails with "SMTP authentication failed" or "unencrypted connection" | Choose STARTTLS or SSL/TLS; authentication is not allowed over an unencrypted connection. Check the user name and re-enter the password if it was rotated |
| Email test succeeds but an owner did not receive the email | The channel lists fixed recipients only, the owner's account has no address or was deleted, or the message is in spam. Add a fixed team address as the fallback |
| The message has no link | Configure **external URL** in Settings → General |
| The endpoint receives a duplicate | Retries are at-least-once: a POST that timed out after the endpoint accepted it is retried. Deduplicate on `eventId`/`deliveryId` |
