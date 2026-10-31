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

- **Type** — the protocol. This release ships **Webhook**: an HTTP `POST` with a
  stable JSON body.
- **Endpoint URL** — where the delivery is posted. It is stored encrypted and is
  never returned by a read; leave the field empty while editing to keep the
  stored one.
- **Subscription** — which event types (data quality, schema sync, OpenLineage;
  nothing selected means all) and the **minimum severity**. A channel below the
  floor receives nothing; the inbox is unaffected.
- **Message language** — the outbound text is rendered in this language
  (`zh-CN` or `en-US`).
- **Signing** — when on, the request carries
  `X-Metaxisdata-Signature: sha256=<hex>` computed with your signing secret over
  the raw body, plus `X-Metaxisdata-Event` and `X-Metaxisdata-Delivery` headers.

Delivery is asynchronous: the platform queues the event, posts it, and retries a
failure with backoff (1m, 5m, 15m, 1h, 6h). After six attempts the delivery is
**dead**; fix the cause and press **Retry**. An event addressed to five
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
  "link": "https://metaxis.example.com/data-quality/executions/981",
  "occurredAt": "2026-10-31T09:12:44Z"
}
```

`link` is present only when the workspace has an **external URL** configured
(Settings → General); the message then points at the issue, execution or
instance page behind the event. The body is a fixed shape: fields are added,
never renamed or removed.

### Verifying the signature

Recompute HMAC-SHA256 over the raw request body with the channel's signing
secret and compare it with the header. The endpoint URL and the signing secret
are written once and never shown again; rotate them by editing the channel and
filling the fields with new values.

## Deliveries

The **Deliveries** card on the same page lists the most recent attempts: the
queued time, channel, event, state, how many attempts were made, and the last
error. States are *pending* (waiting, or waiting for a retry), *sending*,
*sent*, and *dead* (out of attempts). Deliveries are kept 30 days; a channel's
queued deliveries are removed with it.

## Permissions and safety

Reading channels and deliveries needs `metaxisdata.settings.get`; changing a
channel, testing it and retrying a delivery need `metaxisdata.settings.update`.
Every change, test send and retry is recorded in the audit log, with the endpoint
credential redacted.

The endpoint URL is treated as a credential: it is encrypted at rest with the
deployment data key, never returned by an API read, redacted from audit rows, and
replaced with `<endpoint>` in recorded delivery errors. Because the destination
is chosen by a workspace administrator, the deployment allows requests to any
`http`/`https` address the administrator enters — the same trust boundary as an
instance's database connection; see
[security-posture.md](security-posture.md).

Delivery is single-replica, like the rest of the background work: run one server
process or accept that a queued delivery is claimed by whichever replica polls
it. A server restart does not lose a queued delivery; an attempt interrupted
mid-request is retried after its lease expires.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| No outbound message arrived, but the inbox has one | The channel is disabled, its event types exclude the event, its minimum severity is above the event's, or no channel is configured at all |
| A delivery shows `dead` | The last error names the cause (DNS, TLS, timeout, HTTP status, endpoint body). Fix the endpoint, then **Retry** |
| Test send fails but real deliveries work | The test message is sent through the same adapter; a 4xx from the endpoint usually means the endpoint rejects the test's `link` or its body shape |
| The message has no link | Configure **external URL** in Settings → General |
| The endpoint receives a duplicate | Retries are at-least-once: a POST that timed out after the endpoint accepted it is retried. Deduplicate on `eventId`/`deliveryId` |
