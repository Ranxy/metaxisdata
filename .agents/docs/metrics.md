# Prometheus metrics — Reference

> Maintenance reference for `backend/metrics/` and the dedicated scrape endpoint.
> Operator-facing setup and the recommended bind address:
> [docs/deploy.md](../../docs/deploy.md#prometheus-metrics). The accepted decision to
> serve the endpoint anonymously:
> [docs/security-posture.md](../../docs/security-posture.md).
> Related references: [runner.md](runner.md), [schemasync.md](schemasync.md),
> [lineage-analyzer.md](lineage-analyzer.md), [data-quality.md](data-quality.md),
> [openlineage-lineage.md](openlineage-lineage.md),
> [instance-connections.md](instance-connections.md),
> [backend-open-items.md](backend-open-items.md),
> [security-open-items.md](security-open-items.md).

**The authoritative list of instruments is the code, not this file.** `backend/metrics/`
builds them, and `backend/metrics/metrics_test.go` pins every metric name and its exact
label set. Adding or renaming an instrument without updating that test fails the package,
which is the guard against an accidental unbounded label. Do not add a hand-maintained
catalogue here; it drifts from the code.

## Decisions

| Decision | Choice and why |
| --- | --- |
| Endpoint | A dedicated listener, off by default. Never tied to `--debug`: that switch also raises the log level and returns panic stacks to callers, which a scrape must not cause. |
| Registry | One private `prometheus.NewRegistry()` per server. Not `prometheus.DefaultRegisterer`: a second server in the same process must register cleanly, and the endpoint should carry only what this server registered. |
| Injection | One `*metrics.Metrics` built by `metrics.New()`, passed to subsystems like any other dependency. Not package globals: tests need isolated counters, and a zero-value constructor must keep working. |
| Recording API | Typed recorder methods such as `ObserveDatabaseSync`, never exported `*CounterVec` fields. The label vocabulary, proto-to-label mapping, nil checks and unit conversion stay inside the package. |
| Names | Namespace `metaxisdata`, one subsystem per instrument family. The older `api_*` series were renamed rather than kept as aliases; they were only reachable behind the debug switch and no dashboard or alert referenced them. |
| Pull only | No OpenTelemetry, tracing agent or pushgateway. A single process serves a single deployment. |
| Aggregation | None. Instruments are process-local and a deployment runs one server replica, so a restart resets every counter. |
| Business identity | Never a label; see the MUST rules below. |
| Deliverables | Instruments plus this reference and the operator section. No committed Grafana dashboards or alert rules. |
| LLM provider | The one label whose values come from configuration rather than a hardcoded enum, because "which provider is slow" is actionable and the count is bounded by the administrators' profile list. |

## Endpoint contract

- **Configuration.** `--metrics-addr` (`config.Profile.MetricsAddr`) with the container
  equivalent `METAXISDATA_METRICS_ADDR`; an empty value disables the listener. The
  documented recommendation is `127.0.0.1:9090`.
- **Registry contents.** A private registry holding the standard `go_*` and `process_*`
  collectors, `metaxisdata_build_info`, the `metaxisdata_db_pool_*` collector over
  `Store.DBStats`, and every domain instrument registered by `Metrics.Register`.
- **Handler.** `Server.metricsHandler` serves exactly one route: `GET`/`HEAD /metrics`,
  answered by `promhttp.HandlerFor` over the private registry. Any other method gets
  `405` with an `Allow` header; any other path gets `404`. The port is not a second API.
- **Lifecycle.** `Server.startMetricsServer` binds and serves the listener alongside the
  API listener; a bind failure fails startup and closes the API listener it had already
  bound. `Server.Shutdown` closes the metrics listener before the store, on its own short
  budget, because it holds no durable state.
- **Timeouts.** The listener sets a read-header, write and idle timeout. The write budget
  bounds one scrape of a large registry.
- **Anonymous.** No authentication, no TLS, no bearer token: for a pull endpoint inside a
  private deployment, a token would add rotation cost without a matching threat. Access
  control is the network boundary, so bind it to loopback or an internal interface.
- **Invariant: the general API path never exports a metric.** The API port serves no
  `/metrics` route; a request there falls through to the SPA catch-all, and `--debug`
  only enables pprof and logging. There is one metrics endpoint and it is the dedicated
  listener.
- **No self-measurement.** The API-port HTTP middleware skips `/metrics` as well as
  `/healthz` and `/debug/pprof/*`, so a scrape never creates traffic series.

## Naming and label rules

These are MUST rules for every instrument, new or changed.

1. **Name shape.** `metaxisdata_<subsystem>_<name>`, with the subsystem one of `http`,
   `rpc`, `runner`, `schemasync`, `lineage`, `lineagevalidation`, `openlineage`,
   `quality`, `query`, `llm`, `maintenance`, `instance_connection`, `db_pool`,
   `notification`, `agent`. `metaxisdata_build_info` is the only instrument without a
   subsystem.
2. **Suffixes.** Counters end in `_total`; durations are `_seconds`; sizes are `_bytes`;
   gauges carry no suffix or a unit suffix. Cumulative durations read from
   `database/sql` keep its `_seconds_total` shape.
3. **Label values are bounded enums**, rendered by an explicit `switch` in
   `backend/metrics/labels.go` from the proto enum. Use lowercase `snake_case`; never
   `enum.String()`, which yields the upper-case proto spelling. An unknown value
   collapses to a fixed fallback such as `unknown`, `other` or `unspecified` so stored
   data cannot grow the label set.
4. **Label names are limited to the golden table's vocabulary**: `method`, `route`,
   `status`, `service`, `code`, `runner`, `outcome`, `engine`, `stage`, `reason`, `call`,
   `object_type`, `meta_type`, `result`, `kind`, `failure_kind`, `trigger`, `rule_type`,
   `error_code`, `feature`, `provider`, `purpose`, `product`, `tool`, `queue`, `priority`,
   `item`, `state`, `attempt`.
5. **Never a label:** any GUID or resource name, instance/database/schema/table/column
   name, rule title, SQL text, error message, user name/email/IP, or OpenLineage
   producer/namespace/job/dataset name. The former middleware's `url` (raw request path)
   and `host` (caller-controlled) labels are the cautionary example: both were
   cardinality bombs.
6. **No bucket multipliers.** A histogram must not carry a label that multiplies its
   buckets, such as `status` or `reason`; put those on a counter instead.
7. **`provider` is the only configuration-derived value.** It is the configured LLM
   profile title, bounded by what an administrator can create. A generated resource id is
   never a label value.
8. **HTTP `route` is the Echo route template**, never the raw path. An unmatched request
   (empty template) is `unmatched`; the SPA catch-all is `spa`. The method label is a
   lowercase standard method or `other`, because `net/http` accepts arbitrary method
   tokens.
9. **Any new instrument or label must update `TestRegisteredInstruments`.** That table is
   the allowlist: it fails on an unlisted name, an unlisted label, or a changed label set
   on an existing instrument.
10. **State that has no cheap mutation hook is computed at scrape time.** Queue depths
    and lease counts use `funcGauge` (`backend/metrics/scrape.go`) over a snapshot instead
    of a second counter that can drift; `sync.Map` iteration is acceptable because the
    object count is in the thousands.

## Cardinality budget

The budget is a MUST, not a guideline: **the whole process should stay under 3,000 active
series, counted with `_bucket` expansion.** It is not enforced by a test; it is held by
rules 3 through 7 above, which bound every dimension, and by the golden table, which makes
each new dimension a deliberate change.

- The large dimensions are all bounded by code, not by data: route templates (generated
  from the registered API surface), RPC methods (generated from the proto set), engines,
  metadata object types, analysis results, rule types and quality statuses, query
  purposes, and the configured LLM profiles. Real label combinations are far sparser
  than the product of their values.
- Histogram buckets dominate the count. Adding a label to a histogram is the most
  expensive change a proposal can make.
- A signal that needs a per-instance, per-table or per-user dimension is not a metric.
  Read it from the logs, the audit ledger, or the store instead.
- A domain's series estimate can be reasoned about as `label cardinality x bucket count`
  per family; if a proposal cannot be bounded that way, the label is wrong.

## Invariants

1. **Gate reuse records the owner only.** The schema-sync per-resource gate lets waiters
   reuse the owner's result; recording on entry and exit of the shared function counts
   one sync twice and reports the wait as sync time.
2. **A shared function is instrumented once.** API handlers and background runners call
   the same implementation; instrument that implementation, never the handler again.
3. **Success is counted after commit.** Metadata diffs and DDL rows are written inside a
   transaction; counting between `BeginTx` and `Commit` would include rolled-back work.
   A DDL fetch failure happens before the transaction and is counted as it occurs.
4. **Failure reasons use only what existing branches can prove.** A driver error has no
   typed transient/permanent classification, so `reason` stops at values such as
   `connection_limit`, `hidden`, `not_found`, `timeout`, `canceled` and `other`. Do not
   invent `transient`, `driver` or `store`.
5. **Lineage analysis failures are recorded inside the execution unit.** The queue drain
   sees only a non-nil error, while "definition unreadable", "cleared", "unsupported
   engine" and "analysis failed" all return nil; recording only in the drain would report
   zero failures forever. Count in each terminal branch of the analyzer, and let the
   drain add timeout, stale and panic.
6. **A delivery attempt is not an inbox row.** Notifications are deduplicated per
   recipient and window, so one trigger can write zero inbox rows while one event fans
   out several outbound deliveries.
7. **The REST gateway's loopback hop is excluded from HTTP counts.** `/v1/*` is proxied
   through a loopback connection to the Connect handler, so one REST call would otherwise
   appear as two HTTP requests. The HTTP middleware skips requests identified as gateway
   forwards.
8. **HTTP counts and RPC counts are different layers; do not add them.** HTTP counts
   client requests (REST once, direct Connect once); RPC counts API calls (both once).
9. **The connection governor's wait is not on the hot path.** Opening a target database
   goes through the governor every time, so its per-acquire histogram was deliberately
   left out. Saturation is read from the scrape-time gauges
   (`metaxisdata_instance_connection_in_use`, `_waiters`,
   `_try_acquire_rejected_total`) instead.
10. **Recorders are nil-safe.** A `nil *Metrics` records nothing and never panics, which
    is what lets tests and constructors omit it.
11. **No business identity in labels.** When a dashboard needs to drill into an instance
    or table, use logs, audit or a store query; do not open a metric dimension for it.

## Limits and the scrape contract

| Limit | Behavior |
| --- | --- |
| Endpoint disabled by default | No listener exists until `--metrics-addr` is set; an unset address is a valid deployment. |
| Anonymous and plaintext | Anyone who can reach the bound address may scrape; anyone who can read the network may read it. Bind loopback or internal, and do not expose it through a public ingress. |
| Read-only | Only `GET`/`HEAD /metrics`; other methods are refused with `405`. |
| Single path | One route on one listener; there is no second endpoint, no health probe and no pprof there. |
| Process-local, single replica | Counters reset on restart and are not aggregated across replicas. Use rates within one process lifetime; do not reconcile long-term totals with `increase()` across restarts. |
| Scrape cost | Every scrape gathers the registry and iterates the scrape-time snapshots. Keep instrument and label cardinality bounded; the write timeout bounds one slow scrape. |
| No scrape integration test | The listener and handler are covered by unit tests (`metrics_endpoint_test.go`, `http_metrics_test.go`); the integration harness starts the real binary without `--metrics-addr`, so there is no end-to-end scrape suite. |
| Endpoint contents | Only what the private registry holds: the standard `go_*`/`process_*` collectors plus this process's instruments. Nothing registered on the deprecated default registry leaks in. |

## Instrument families by subsystem

The families below are orientation, not a catalogue; names, labels and values live in
`backend/metrics/` and its test. The package registers 71 named `metaxisdata_*`
instruments, four of them scrape-time gauges, plus the standard Go and process collectors.

| Subsystem | Families | Answers |
| --- | --- | --- |
| `http` | requests, request duration | API traffic and latency by route template and status |
| `rpc` | requests, request duration | ConnectRPC calls by service, method and client-visible code |
| `runner` | up, panics | Whether a background runner is alive; a panic kills it and does not restart |
| `schemasync` | scans, instance/database syncs, durations, failures, driver calls, meta resources written/deleted, retries, connection deferrals | Sync health, where time goes, and whether the budget is the bottleneck |
| `lineage` | analysis objects by terminal result, analyze duration, in-flight, queue depth, retry exhaustion, edges written | Analyzer throughput and failure classes |
| `lineagevalidation` | triggers, passes, pass duration, edges | Background revalidation cadence and backlog |
| `openlineage` | ingest failures by kind, events by result, process duration, edges | Whether producers deliver usable lineage, and why a request was refused |
| `quality` | due scan, executions created/completed/deferred/abandoned/admitted, in progress, manual queue, rule executions/duration/errors | Scheduler backlog, execution lifecycle and rule outcomes |
| `query` | read-only target queries by purpose, duration | Target-database latency separated from scheduling latency |
| `llm` | calls, duration, tokens, usage missing, parse failures, quota rejections | Model spend, latency and provider-side failure |
| `maintenance` | passes, pass duration, deleted rows, item failures | Retention and cleanup health |
| `instance_connection` | in use, waiters by priority, try-acquire rejections | Whether the per-instance connection budget is saturated |
| `db_pool` | connections by state, max open, waits, wait duration | Metadata-database pool saturation |
| `notification` | delivery attempts by outcome, delivery duration | Outbound delivery success, including quiet-hours deferrals |
| `agent` | tool calls, duration, result bytes | Model-facing tool executions on the built-in products and MCP |
| build | `metaxisdata_build_info` | Which release and commit the process runs |
| standard | `go_*`, `process_*` | Runtime and process health |

## Where things live

- **Package**: `backend/metrics/` — `metrics.go` (the `Metrics` aggregate, `New`,
  `Register`), `labels.go` (proto enum to label value), `scrape.go` (the scrape-time
  gauge), `build.go`, `db.go`, and one file per domain. A nil `*Metrics` disables
  recording.
- **Endpoint**: `Server.startMetricsServer`, `Server.metricsHandler` and the listener
  lifecycle in `backend/server/server.go`; the address comes from
  `config.Profile.MetricsAddr`, set by the `--metrics-addr` flag in
  `backend/bin/server/cmd/root.go` and by `METAXISDATA_METRICS_ADDR` in the container
  entrypoint.
- **HTTP middleware**: `backend/server/http_metrics.go` (skip rules, route-template
  labels, panic recorded as 500).
- **ConnectRPC interceptor**: `backend/api/v1/metrics_interceptor.go`, installed first
  (outermost) in the interceptor chain, so it observes the codes auth, throttle and ACL
  refuse with as well as the mapped code that `ErrorMapping` returns from the innermost
  layer.
- **Pool stats**: `Store.DBStats` in `backend/store/store.go`, consumed by
  `metrics.NewDBPoolCollector`.
- **Tests**: `backend/metrics/metrics_test.go` pins the golden names and label sets,
  the scrape-time gauges, nil safety, enum mappings, the recorder end to end, and that
  every built instrument is registered. `backend/server/metrics_endpoint_test.go` pins
  the private-registry handler, method refusal, listener start/scrape/shutdown and bind
  failure; `backend/server/http_metrics_test.go` pins the skips, the gateway hop and the
  route folding; `backend/api/v1/metrics_interceptor_test.go` pins procedure splitting and
  code mapping. Domain tests live beside each subsystem that records.
- **Docs**: the operator section in `docs/deploy.md`; the anonymous-endpoint decision in
  `docs/security-posture.md`; the package's row in [backend/AGENTS.md](../../backend/AGENTS.md).
- **Gate**: `gofmt`, `golangci-lint run --allow-parallel-runners`, and
  `go test ./backend/metrics/... ./backend/server/... ./backend/api/v1/...`.
