# Prometheus 指标（Metrics）— Reference

> 状态：**已实现**（2026-10，v1 = P0 66 条）。基线 `af4876d`。
> 本文是实现后的维护参考：指标契约、标签规则、埋点不变量与已知限制。
> P1/P2 的命名已冻结但尚未实现，见 §6 与 §9。
>
> 关联文档：[runner.md](runner.md)、[schemasync.md](schemasync.md)、[lineage-analyzer.md](lineage-analyzer.md)、
> [data-quality.md](data-quality.md)、[openlineage-lineage.md](openlineage-lineage.md)、
> [backend-open-items.md](backend-open-items.md)（D8）、[security-open-items.md](security-open-items.md)（M25）、
> [docs/deploy.md](../../docs/deploy.md)、[docs/security-posture.md](../../docs/security-posture.md)。

---

## 0. 摘要（先看这五条）

1. **新增 `backend/metrics` 包**：按域组织 instrument，显式注入到 runner/API（传 `nil` 即关闭），
   用**自定义 registry**，不碰 `prometheus.DefaultRegisterer`。
2. **指标走独立监听端口**：新增 `--metrics-addr` / `METAXISDATA_METRICS_ADDR`，默认关闭；
   主端口删除 `/metrics` 与 `--debug` 的绑定，`--debug` 只留 pprof。删除废弃的 `echo-contrib` prometheus。
3. **对外命名统一 `metaxisdata_*`**：`metaxisdata_http_*`、`metaxisdata_rpc_*`、
   `metaxisdata_schemasync_*`、`metaxisdata_lineage_*`、`metaxisdata_openlineage_*`、
   `metaxisdata_quality_*`、`metaxisdata_query_*`、`metaxisdata_llm_*`、`metaxisdata_maintenance_*`、
   `metaxisdata_lineagevalidation_*`、`metaxisdata_instance_connection_*`、`metaxisdata_runner_*`、
   `metaxisdata_db_pool_*`、`metaxisdata_build_info`。
4. **只允许有界枚举标签**（engine/status/result/reason/…），不加实例、库、表、GUID；
   全部指标合计目标 < 3,000 活跃 series。
5. **已按 4 个独立范围落地**：基础设施 → 元数据采集 → 血缘/OpenLineage → 数据质量/LLM；
   v1 只做 P0（66 条，按目录行计），P1/P2 命名已冻结、后续补实现。本方案即 D8 所要求的「重建遥测设施」的决定；
   实现后 D8 已关闭、M25 已收窄为 pprof。

---

## 1. 目标与范围

### 1.1 目标

给 Metaxisdata 补一层**拉取式（pull）Prometheus 指标**，覆盖用户点名的四类后台业务（元数据采集、血缘分析、
数据质量，以及 LLM/目标库查询），加上所有部署都需要的底座（HTTP/API、Go 运行时、进程、数据库连接池）。
指标要能回答运维的三个问题：

1. **现在是否正常**：各 runner 是否还活着、任务在成功还是失败、队列是否积压、连接预算是否打满。
2. **慢在哪里**：目标库往返、元数据事务、血缘解析、LLM 提供商、调度层，各占多少时间。
3. **失败为什么**：用有界的失败原因/错误码标签区分可行动的故障类别，而不是只有一行 `slog.Warn`。

### 1.2 已确认的决策（评审结论）

| 决策点 | 结论 |
| --- | --- |
| 指标端点 | **独立监听端口**，默认关闭；与 API 端口隔离，**不再依赖 `--debug`** |
| 首版覆盖范围 | 基础（HTTP/API + Go 运行时 + 进程 + 连接池）、元数据采集、血缘分析 + OpenLineage、数据质量、LLM、维护任务 |
| 标签粒度 | **仅聚合枚举维度**（engine/status/result/reason 等），不加实例、库、表、GUID |
| 交付物 | 指标 + 运维/维护文档；**不**提交 Grafana 面板或告警规则 |
| 旧 `api_*` 系列 | **直接改名**为 `metaxisdata_http_*` / `metaxisdata_rpc_*`，不保留兼容路径 |
| ExplainSQL 的 token | v1 只保证 DQ 路径的 token；ExplainSQL 先只上报 `llm_usage_missing_total`，`IncludeUsage` 待验证网关后再开 |
| 连接 governor 热路径 | v1 只上 `in_use` / `waiters` / `try_rejected`（零热路径成本）；`wait_seconds` 直方图放 P1 |
| LLM `provider` 标签 | **保留**（值域由运营配置的 profile 名限定，数量很少） |

### 1.3 非目标（本方案明确不做）

- 不引入 OpenTelemetry / tracing / pushgateway：单进程自托管部署，pull 模型足够
  （`docs/deploy.md` 的「Run one server process」一节说明了单副本约束）。
- 不做多副本指标聚合。指标是**进程本地**的；部署本身要求单副本
  （`docs/deploy.md`、[runner.md](runner.md) 的「State kept in the process」）。
- 不把业务身份写进标签：实例名、数据库名、schema/table/column、meta GUID、规则 ID、SQL 文本、
  用户/邮箱/IP、OpenLineage 的 namespace/job/dataset 名，一律不进标签（基数无界且敏感）。
- 不恢复被删除的自上报遥测（`backend/metric/`、`backend/plugin/metric/`，见 D8）。那是另一套东西；
  本方案是全新的、基于 `prometheus/client_golang` 的拉取式 instrument 包。

---

## 2. 改造前的现状（背景）

### 2.1 今天已经有一个 `/metrics`，但只是半成品

- `backend/server/echo_routes.go:95` 用**已废弃**的 `echo-contrib/prometheus` 建了
  `NewPrometheus("api", nil)`，`:99` 的 `p.Use(e)` 同时注册中间件和 `GET /metrics`。
- 暴露 4 个系列，标签全部是 `code,method,host,url`：
  `api_requests_total`、`api_request_duration_seconds`、`api_request_size_bytes`、`api_response_size_bytes`。
- 注册在**进程级默认 registry**（`prometheus.DefaultRegisterer/DefaultGatherer`），所以任何用
  `promauto`/`prometheus.MustRegister` 的包级 instrument 都会自动出现在 `/metrics`。
- `backend/server/echo_routes.go:94` + `:129-141` 的 `metricsGateMiddleware` 让 `/metrics` 在
  `profile.RuntimeDebug` 关闭时返回 404。
- 两个已存在的基数炸弹：`url` 标签被仓库改成**原始请求路径**（`:96-98`，含 SPA 的 `/*` catch-all 和任意 404 探测），
  `host` 标签是调用方可控的 `Request.Host`。这也解释了为什么它只能藏在 debug 后面。
- `echo-contrib` 是被标了 3 次 deprecated 的 API（模块源码 `prometheus.go:144,179,305`），
  而 `go.mod:19` 里这个模块只被这一处使用。

### 2.2 `/metrics` 之外没有任何指标

- `backend/metric/`、`backend/plugin/metric/` 已删除，D8（[backend-open-items.md](backend-open-items.md)）
  记录「同步失败只有 `slog.Warn`，恢复计数需先决定是否重建遥测设施」。
- [runner.md](runner.md)「Known accepted gaps」：**No runner metrics**。
- [data-quality.md](data-quality.md) 也把「连接等待/饱和度不可观测」记为已知缺口。
- `prometheus/client_golang` 目前只是 **indirect** 依赖（`go.mod:98`，经 echo-contrib 引入）。

### 2.3 两个必须正视的约束

1. **`RuntimeDebug` 不是指标开关，也不能变成指标开关。** 同一个开关还控制日志级别
   （`backend/api/v1/setting_service.go:367-381`）和 panic 时是否把堆栈返回给调用方
   （`backend/server/grpc_routes.go:134-137`），并且可以在运行时通过 API 打开。
   为了抓指标而打开 debug 日志和堆栈回传，是不可接受的副作用。这正是 security-open-items 的 M25 所指。
2. **单副本、进程内状态。** runner 的队列、gate、重试退避、连接租约全是内存态
   （[runner.md](runner.md)）。因此指标是「本进程视角」，重启后计数器归零是预期行为，
   dashboard 不能用 `increase()` 跨重启做长期账。

---

## 3. 总体设计

### 3.1 新包 `backend/metrics`：显式注入、自定义 registry

**位置**：新增顶层包 `backend/metrics/`（叶子包，只依赖 `client_golang` 与 `generated-go/store` 的枚举，
不依赖任何业务包，避免 import cycle，也允许 `plugin/*` 直接使用）。

**结构**（每个域一个文件，一个 `Metrics` 聚合）：

```go
// backend/metrics/metrics.go
type Metrics struct {
    HTTP        httpMetrics
    RPC         rpcMetrics
    Schemasync  schemasyncMetrics
    Lineage     lineageMetrics
    OpenLineage openLineageMetrics
    Quality     qualityMetrics
    LLM         llmMetrics
    Maintenance maintenanceMetrics
    Runner      runnerMetrics
    DB          dbPoolMetrics
}

// New 构建全部 instrument，但不注册；测试每个用例拿到独立计数。
func New() *Metrics

// Register 把全部 instrument 显式注册到给定 registry。
func (m *Metrics) Register(reg prometheus.Registerer) error
```

**关键选择：注入而不是包级全局变量。**

- 仓库已经为「包级状态」付过代价：lineage engines 的注册曾由 `init()` 填充，导致测试和 server 对
  「注册了什么」看法不一致（`backend/server/server.go:111-115` 的注释）。指标沿用显式注入。
- 测试隔离是刚需：直方图无法重置，包级全局会让不同用例的观测互相污染；注入的 `metrics.New()`
  让每个用例拿到干净 instrument，可直接用 `testutil` 断言。
- 传 `nil` 表示「关闭指标」（记录方法都是 nil-receiver 安全的），所以既有测试里的
  `&Syncer{...}`、`&Analyzer{}` 这类零值构造**不需要改**——这是把改动面压到最小的关键。

**记录方法而不是暴露 `*prometheus.CounterVec` 字段。** 业务代码不 import `client_golang`，
只调 `m.ObserveDatabaseSync(engine, outcome, d)` 这类方法；标签映射（proto 枚举 → 小写标签值）、
nil 检查、单位换算都收敛在 `backend/metrics` 内，便于用一个「标签白名单」测试守住基数。

**自定义 registry，不用 `DefaultRegisterer`。** 现在测试为了绕开全局注册表要加互斥 + `sync.Once`
（`backend/server/echo_routes_test.go:19-40`）；同进程第二个 server 也无法重复注册。
`NewServer` 里新建 `prometheus.NewRegistry()`，显式注册：

- `collectors.NewGoCollector()`、`collectors.NewProcessCollector(...)`：保留今天已有的 `go_*`/`process_*`；
- 自研 `metaxisdata_db_pool_*` collector（见 §4.1）：刻意**不用** `collectors.NewDBStatsCollector` 的
  `go_sql_*` 名字，因为它和 Go 运行时指标混在前缀里，语义也不自洽；
- `metaxisdata_build_info{version,git_commit}`；
- `m.Register(reg)` 的业务 instrument。

**为什么不用 `echoprometheus`**：它虽然就在已 pin 的 `echo-contrib v0.17.4` 里（无需新模块），
但标签名固定为 `code,method,host,url`、无法注入自定义 registry 断言、且保留 `host` 标签与 404 原始路径问题。
自己写 HTTP 中间件约 40 行，换来 `method/route/status` 这套想要的标签和可测试性，同时可以整个删掉
`echo-contrib` 依赖。

### 3.2 指标端点：独立监听端口

**配置面**：

- 新增 flag：`--metrics-addr`（string，默认 `""` = 不启动指标监听）。
- 新增 Profile 字段：`config.Profile.MetricsAddr string`（`backend/config/profile.go`）。
- 容器环境变量：`METAXISDATA_METRICS_ADDR`，在 `scripts/docker/metaxisdata-entrypoint.sh` 里映射为
  `--metrics-addr`（沿用「显式 flag 覆盖 env」的既有优先级）。
- 推荐值写进文档：`127.0.0.1:9090`（本机 scrape）；`0.0.0.0:9090`/`:9090` 允许，但文档必须写明
  「只在可信网络边界内暴露，本端点不做鉴权」。

**实现**：

- 独立 `http.Server` + 独立 `http.ServeMux`，只挂 `GET /metrics` → `promhttp.HandlerFor(registry, ...)`。
- 生命周期跟随主 server：`NewServer` 构建 registry；`Run` 里 `net.Listen` 并 `go Serve`
  （bind 失败直接返回错误，和主端口一致）；`Shutdown` 里先于 store 关闭、幂等关闭。
- 主端口**移除** `/metrics` 路由与 `metricsGateMiddleware`；`--debug` 只保留 pprof。
  `api_*` 四个系列随旧中间件一起消失（改名见 §7）。
- 指标监听不经过主 Echo 中间件：安全头不适用，但 promhttp 已设置 `Content-Type: text/plain; version=...`，
  且响应不含 HTML，无 XSS 面；文档补一句即可。

**为什么这样切**：

- 抓取是运维/监控系统行为，不应该要求打开会改变应用行为的 debug 开关（M25）。
- 端点默认关闭 + 独立端口 = 默认不可达；开启后是否可达由网络边界决定，符合「loopback / 内网」
  的安全建议，同时避免在 API 端口上新增匿名聚合端点。
- 不做 Bearer token：pull 场景下 Prometheus 的 `authorization` 配置和密钥轮换成本对自托管产品不划算。
  若未来要跨公网抓取，再作为独立决策叠加。

### 3.3 命名、标签与单位规范

**命名**（对外契约，一旦发布不轻易改）：

- 统一 namespace **`metaxisdata`**，每个域一个 subsystem：
  `metaxisdata_http_*`、`metaxisdata_rpc_*`、`metaxisdata_schemasync_*`、`metaxisdata_lineage_*`、
  `metaxisdata_openlineage_*`、`metaxisdata_quality_*`、`metaxisdata_query_*`、`metaxisdata_llm_*`、
  `metaxisdata_maintenance_*`、`metaxisdata_lineagevalidation_*`、`metaxisdata_instance_connection_*`、
  `metaxisdata_runner_*`、`metaxisdata_db_pool_*`、`metaxisdata_build_info`。
- 后缀：counter 用 `_total`；时长用 `_seconds`（基础单位秒）；大小用 `_bytes`；gauge 无后缀或带单位后缀。

**标签**：

- 只允许**有界枚举**：`engine`、`meta_type`、`object_type`、`status`、`outcome`、`result`、`reason`、
  `stage`、`kind`、`call`、`mode`、`priority`、`sweep`、`item`、`service`、`method`、`code`、`route`、
  `feature`、`provider`、`purpose`、`rule_type`、`error_code`、`event_type`、`failure_kind`、`queue`、`state`。
- 枚举值一律**小写 snake_case**，由 `backend/metrics` 里的显式 `switch` 从 proto 枚举映射
  （不用 `String()`，它返回大写下划线形式；linter 的 `exhaustive` 会强制列出所有分支）。
- **禁止**：任何 GUID/ID、实例/库/表/列名、规则标题、SQL 文本、错误消息、用户名/邮箱/IP、
  OpenLineage producer 文本。历史教训就是现在的 `url`/`host` 标签。
- 直方图上不放 `status` 之类会成倍乘桶的标签（`status` 放 counter 上即可）。
  `provider` 是唯一非硬编码枚举（运营配置的 LLM profile **title**；资源名里带生成 id，不作为标签值），
  由配置量而非数据量约束；**已确认保留**（数量很少，且「哪个提供商在变慢」是可行动信号）。

**基数预算**：全部域合计目标 **< 3,000 个活跃 series**（含 bucket 的 `_bucket` 展开）。
按域的估算见附录 A。守住它的手段是一个**标签名白名单测试**：gather registry 后遍历所有指标，
断言每个 label name ∈ 白名单，且断言指标名集合等于黄金清单（防止漏注册/误注册）。

**HTTP `route` 标签**：用 Echo 的路由模板 `c.Path()`（`/metaxisdata.v1.InstanceService/*`、
`/v1/*`、`/api/v1/lineage*` 等，总数 < 60），空路径（未匹配 404）折叠为 `unmatched`，
SPA catch-all `/*` 折叠为 `spa`。这修掉了今天的原始路径基数问题。

### 3.4 一个全局陷阱：REST gateway 的两次 HTTP

`/v1/*` 由 grpc-gateway 通过**回环连接自己**（`backend/server/grpc_routes.go:240-256`，路由 `:397`），
所以一次 REST 调用在 HTTP 层看起来是「外层 `/v1/*` + 内层 ConnectRPC 请求」两次。
处理方式：HTTP 中间件跳过 gateway 内部回环请求（复用 `backend/component/audit/audit.go` 的
`GatewayPeerHeader` + `GatewayProofHeader` 校验，导出一个只读判定函数），
于是：

- `metaxisdata_http_requests_total` = 客户端 HTTP 请求数（REST 计一次、直连 Connect 计一次）；
- `metaxisdata_rpc_requests_total` = API 调用数（REST 与直连各一次）。
两者不在同一层，**不要相加**；文档必须写清楚。

---

## 4. 指标目录

约定：**P0** = 第一批实现（每个域的核心健康信号）；**P1** = 紧随其后、便宜且有用；
**P2** = 需要小改造或价值依赖具体运维需求，先不实现。
所有行号基于 `af4876d`，实现时会漂移。

### 4.1 基础：HTTP / ConnectRPC / 运行时 / 连接池 / runner 生命周期

**HTTP（自研 Echo 中间件，`backend/server/http_metrics.go`）**

| 指标 | 类型 | 标签 | 埋点 | 语义 / 注意 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_http_requests_total` | counter | `method,route,status` | 中间件退出 | 每个完成的 HTTP 请求一次；`status` 取 `c.Response().Status`，出错时回退 Echo error 码；`method` 只取小写标准方法（get/post/…）+ `other`，**原始 token 不进标签**（net/http 接受任意方法串，404/405 也会经过中间件）；跳过 `/healthz`、`/debug/pprof/*` 与 gateway 内部回环；被 recover 中间件捕获的 panic 记为 500 | P0 |
| `metaxisdata_http_request_duration_seconds` | histogram | `method,route` | 同上 | 端到端；流式请求在流结束时观测一次 | P0 |
| `metaxisdata_http_request_size_bytes` | histogram | `method,route` | 中间件入口 | `ContentLength` + header 近似（与旧实现同口径） | P1 |
| `metaxisdata_http_response_size_bytes` | histogram | `method,route` | 中间件退出 | `c.Response().Size` | P1 |
| `metaxisdata_http_in_flight_requests` | gauge | — | 中间件入口/退出 | 并发度；计数器推不出来的信号 | P1 |

> 中间件要**尽量靠前**安装（紧随 `recoverMiddleware`），这样 CSRF、body limit 等中间件拒绝的请求也能被计入；
> 同时跳过 `/healthz`（镜像每 30s 探活）、`/debug/pprof/*` 和 gateway 内部回环请求，避免无意义序列。

**ConnectRPC（`backend/api/v1/metrics_interceptor.go`，放在拦截器链**最外层**）**

> 必须最外层：`grpc_routes.go:141-156` 的链里 Auth/Throttle/ACL 会拒绝请求，`ErrorMapping` 在最内层。
> 放最外层才能观测到「被限流/被拒绝」以及客户端实际看到的 code。

| 指标 | 类型 | 标签 | 埋点 | 语义 / 注意 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_rpc_requests_total` | counter | `service,method,code` | `WrapUnary` / `WrapStreamingHandler` | 每次调用一次；`code` 用 `connect.CodeOf(err).String()`，成功为 `ok`；服务/方法集由 20 个 service、141 个 RPC 限定 | P0 |
| `metaxisdata_rpc_request_duration_seconds` | histogram | `service,method` | unary | 墙钟；不含 stream | P0 |
| `metaxisdata_rpc_stream_duration_seconds` | histogram | `service,method` | streaming handler | 3 个 server-streaming RPC（ExplainSQL、SubscribeNotifications、ExplainQualityExecution）结束时观测；通知流天然长（30 min 级），桶要覆盖 | P1 |
| `metaxisdata_rpc_request_size_bytes` / `_response_size_bytes` | histogram | `service,method` | unary | `proto.Size(req.Any()/resp.Any())`，precedent 见 `backend/api/v1/audit.go:39,117` | P2 |
| `metaxisdata_rpc_in_flight_requests` | gauge | `service` | both wrap | 区分「真忙」与「长连接流」 | P1 |

**运行时 / 构建**

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `go_*`、`process_*` | 标准 collector | — | registry 构建时注册 | 保留今天默认 registry 已有的系列，不丢失 | P0 |
| `metaxisdata_build_info` | gauge=1 | `version,git_commit` | registry 构建时 | 部署身份；来自 `backend/common/version` | P0 |
| `metaxisdata_db_pool_connections` | gauge | `state=in_use\|idle` | 自研 collector，读 `Store.DBStats()` | 元数据库连接池饱和度 | P0 |
| `metaxisdata_db_pool_max_open_connections` | gauge | — | 同上 | 池上限（启动时按 `max_connections - superuser_reserved` 推出，operator 目前看不到） | P0 |
| `metaxisdata_db_pool_wait_total` / `_wait_duration_seconds_total` | counter | — | 同上 | 连接等待次数/时长；`wait_duration` 增长 = 池太小或慢查询 | P0 |
| `metaxisdata_db_pool_closed_total` | counter | `reason=max_idle\|max_idle_time\|max_lifetime` | 同上 | 连接回收原因 | P1 |

> 已按此实现窄访问器 `func (s *Store) DBStats() sql.DBStats`（旁边是 `GetDB()`，
> `backend/store/store.go`），避免把 `*sql.DB` 泄漏到 metrics 包。

**Runner 生命周期（`backend/runner/runner.go`）**

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_runner_up` | gauge | `runner` | `runner.Go` 启动置 1，defer 置 0 | **一个 panic 会把 runner 打死且不重启**（`:41-47` 只记日志），这是唯一能在进程内看到「runner 死了」的信号 | P0 |
| `metaxisdata_runner_panics_total` | counter | `runner` | `runner.Go` 的 recover 分支 | 被 panic 边界吞掉的次数 | P0 |

> `runner.Go` 需要多一个 `*metrics.Metrics` 参数（`runner_test.go` 不直接调用它，改动面很小）。

### 4.2 元数据采集：schemasync / 连接预算 / maintenance / lineagevalidation

**schemasync（`backend/runner/schemasync/`）**

最关键的实现约束：`SyncInstance`（`instance_sync.go`）和 `SyncDatabaseSchema`（`database_sync.go`）
的 per-resource gate 会让**非 owner 等待并复用 owner 的结果**。所以 outcome/耗时只能在 `owner == true`
的分支记录；API 触发和后台触发走的是同一个函数（`instance_service.go` 的 `SyncInstance`/`BatchSyncInstances`、
`operation.go` 的 `SyncDatabaseForUser`），**只在共享函数里埋一次**，不要在 API handler 再埋。

| 指标 | 类型 | 标签 | 埋点 | 语义 / 注意 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_schemasync_scans_total` | counter | `outcome=success\|failure` | `trySyncAll` 的返回值（`queue.go`） | 每轮 15 min 扫描一次；`trySyncAll` 现在返回 `ok bool`（store 读取失败或 panic 为 false；单个实例同步失败不算扫描失败） | P0 |
| `metaxisdata_schemasync_scan_duration_seconds` | histogram | — | `trySyncAllOnce` 返回后（`queue.go`） | 一轮实例扫描的墙钟，**包含本轮对到期实例的枚举/同步**（`trySyncAll` 内同步调用 `SyncInstance`）；单库 schema 同步由 checker 并发入队，不在这里 | P0 |
| `metaxisdata_schemasync_instance_syncs_total` | counter | `engine,outcome` | owner-only defer（`instance_sync.go` 的 `SyncInstance`） | 每次真实的实例枚举 | P0 |
| `metaxisdata_schemasync_instance_sync_duration_seconds` | histogram | `engine` | 同上 | 含目标库往返 | P0 |
| `metaxisdata_schemasync_database_syncs_total` | counter | `engine,outcome` | owner-only defer（`database_sync.go` 的 `SyncDatabaseSchema`） | 每次真实的库同步；checker 10s 与 API 共用 | P0 |
| `metaxisdata_schemasync_database_sync_duration_seconds` | histogram | `engine` | 同上 | 从拿到 gate 之后算，排除等 owner 的时间；含连接租约、driver、元数据事务、DDL 抓取 | P0 |
| `metaxisdata_schemasync_failures_total` | counter | `stage=instance\|database`,`reason` | 上述 defer | reason 只能用已有分支：`connection_limit`、`hidden`、`not_found`、`timeout`、`canceled`、`other`。**不要臆造 transient/permanent**（driver error 无类型） | P0 |
| `metaxisdata_schemasync_driver_call_duration_seconds` | histogram | `engine,call=sync_instance\|sync_db_schema` | driver 调用处（`SyncInstance`/`SyncDatabaseSchema` 里的 `ObserveDriverCall`） | 把「目标库慢」和「元数据库慢」拆开 | P0 |
| `metaxisdata_schemasync_queue_depth` | gauge | `state=queued\|retrying` | scrape-time `GaugeFunc`（Range 两个 `sync.Map`） | 队列没有 Len，用 Range 近似；含等待退避的项 | P1 |
| `metaxisdata_schemasync_in_flight` | gauge | `kind=instance\|database` | gate 的 `count()`（需新访问器） | 正在跑的真实同步数（owner） | P1 |
| `metaxisdata_schemasync_meta_resources_written_total` | counter | `object_type` | **commit 之后**（`database_sync.go` 的 `SyncDatabaseSchema`） | 创建+更新合并计数（`diff()` 不区分 created/updated） | P0 |
| `metaxisdata_schemasync_meta_resources_deleted_total` | counter | `object_type` | `logSchemaSyncDeletion`（`metadata_batch.go`，已有按类型计数） | 破坏性同步的告警面；保持「只记日志」的既有决策不变 | P0 |
| `metaxisdata_schemasync_ddl_refresh_total` | counter | `mode=full\|rotating\|changed` | commit 后（`database_sync.go` 的 `SyncDatabaseSchema`） | DDL 刷新节奏 | P1 |
| `metaxisdata_schemasync_ddl_objects_total` | counter | `result=upserted\|deleted\|unchanged` | commit 后（`database_sync.go` 的 `SyncDatabaseSchema`） | DDL 行写入；事务内不要埋 | P1 |
| `metaxisdata_schemasync_ddl_fetch_failures_total` | counter | — | `prepareObjectDefinitions`（`object_definition.go`） | 抓取失败在事务之前发生，即使回滚也应计数 | P1 |
| `metaxisdata_schemasync_statistics_refresh_total` | counter | `outcome=refreshed\|read_failed\|write_failed` | 统计刷新分支（`database_sync.go` 的 `SyncDatabaseSchema`） | best-effort 通道，失败不等于同步失败 | P1 |
| `metaxisdata_schemasync_retries_total` | counter | `attempt=1\|2\|3` | `scheduleRetry`（`queue.go`） | 退避重排 | P0 |
| `metaxisdata_schemasync_retries_exhausted_total` | counter | `engine` | `scheduleRetry` 返回 true 的调用方（`syncer.go` 的 checker） | 「持续失败的库」告警信号 | P0 |
| `metaxisdata_schemasync_connection_limit_deferrals_total` | counter | `kind=database\|instance` | checker/scan 的 TryAcquire 失败分支（`queue.go` 的 `trySyncAll`、`syncer.go` 的 checker） | **背压不是失败**：预算打满时跳过本轮 | P0 |
| `metaxisdata_schemasync_database_skips_total` | counter | `reason=hidden\|instance_gone` | checker 的丢弃分支（`syncer.go`：实例消失、`ErrDatabaseHidden`）、`instance_sync.go` 的 `forgetQueuedDatabase` 调用点 | 排队库被无同步地丢弃 | P1 |
| `metaxisdata_schemasync_database_hides_total` | counter | `reason=driver_not_found\|snapshot_missing` | `markDatabaseDeleted`（`database_sync.go`）、`SyncInstance`（`instance_sync.go`） | 清单收缩信号 | P1 |
| `metaxisdata_schemasync_panics_total` | counter | `kind=database_sync\|instance_scan` | 终端 recover（`queue.go` 的 `trySyncAll`、`syncer.go` 的 checker；`SyncInstance`/`SyncDatabaseSchema` 的 recover 只释放 gate 后 re-panic，不重复计数） | 被吞掉的 panic | P1 |
| `metaxisdata_schemasync_operations_total` | counter | `outcome=succeeded\|partial\|failed` | `deliverOperation`（`operation.go`，每次 operation 恰好一次） | **用户可见的同步请求**，不是同步工作量；`abandoned` 暂不提供（`instanceErr` 只有字符串，无类型原因） | P1 |
| `metaxisdata_schemasync_operation_databases_total` | counter | `state=succeeded\|failed\|unfinished` | 同上，`detail` 里已有三个计数 | 一次库结果会扇出给所有等待它的 operation（`operation.go` 的 `completeDatabase`），计数单位是「报告」不是「同步」 | P2 |
| `metaxisdata_schemasync_operation_expired_total` | counter | — | `expireOperations`（`operation.go`） | 2h 超时的兜底信号 | P2 |

**实例连接预算（`backend/component/state/connection.go`）**

`State` 已暴露 scrape 用的 `ConnectionInUse`/`ConnectionWaiters`（`connection.go`），`server.go` 用 GaugeFunc 接线；governor 在内部维护 holders/waiters。

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_instance_connection_in_use` | gauge | — | `State.ConnectionInUse`（scrape 时 GaugeFunc，`server.go` 接线） | 全实例租约总数（聚合，不加实例标签） | P0 |
| `metaxisdata_instance_connection_waiters` | gauge | `priority=background\|manual\|interactive` | `State.ConnectionWaiters`（scrape 时 GaugeFunc，按 priority 拆分） | 「预算是不是瓶颈」的直接信号 | P0 |
| `metaxisdata_instance_connection_try_acquire_rejected_total` | counter | — | `connectionGovernor.tryAcquire` 的拒绝分支（`connection.go`） | 零基数、纯加法；目前是静默分支 | P0 |
| `metaxisdata_instance_connection_wait_seconds` | histogram | `priority,outcome=granted\|timeout\|canceled` | `acquire`（`connection.go`），只观测真正排队的请求 | **热路径**（每次开 driver 都会走）。已确认 v1 不做，放 P1；v1 先用下面三条零成本指标看饱和度 | P1 |

**maintenance（`backend/runner/maintenance/maintenance.go`）**

`passSummary`（`maintenance.go`）已经把所有计数、失败项、`retentionSkipped` 聚在一起，日志在 `Run` 的 pass 汇总处（`maintenance.go`）。
建议**只在 pass 级埋点**（不要同时埋 per-item，否则双计）。

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_maintenance_passes_total` | counter | `outcome=ok\|partial` | `Run` 的 pass 汇总日志（`maintenance.go`） | 每轮 6h 维护 | P0 |
| `metaxisdata_maintenance_pass_duration_seconds` | histogram | — | `Run` 的 pass 汇总日志（`maintenance.go`，`started` 已有） | 1h pass 预算 | P0 |
| `metaxisdata_maintenance_deleted_rows_total` | counter | `item`（12 个清理项） | 同上，`summary.counts` | 每轮清理量；注意两条 lineage 计数是「边」不是行 | P0 |
| `metaxisdata_maintenance_item_failures_total` | counter | `item` | 同上，`summary.failed` | 单项失败但 pass 继续 | P0 |
| `metaxisdata_maintenance_revalidation_edges_total` | counter | `kind,result` | `revalidateIngestedLineage`（`maintenance.go`） | 维护侧 revalidation 的吞吐 | P1 |
| `metaxisdata_maintenance_revalidation_backlog_total` | counter | `kind` | 同上（`truncated`） | 一轮没排干 backlog | P1 |
| `metaxisdata_maintenance_openlineage_retention_skipped_total` | counter | `reason=no_window\|setting_unavailable` | `pruneOpenLineageRuns`（`maintenance.go`） | 保留策略未配置时今天只打日志 | P2 |

**lineagevalidation（`backend/runner/lineagevalidation/validation.go`）**

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_lineagevalidation_triggers_total` | counter | `result=queued\|coalesced` | `Trigger`（`validation.go`） | `queued`=信号进入 channel（可能在 settle 窗口被 `discardPendingTrigger` 折叠，**不保证多跑一次 pass**）；`coalesced`=channel 已满被丢弃 | P0 |
| `metaxisdata_lineagevalidation_passes_total` | counter | `outcome=ok\|error\|backlog_remaining` | `runOnce`（`validation.go`） | `backlogRemaining` 是函数内局部值，需在函数内埋点 | P0 |
| `metaxisdata_lineagevalidation_pass_duration_seconds` | histogram | `outcome` | 同上 | | P0 |
| `metaxisdata_lineagevalidation_edges_total` | counter | `result=revalidated\|still_unknown` | 同上，复用返回值 | | P0 |
| `metaxisdata_lineagevalidation_trigger_pending` | gauge | — | `GaugeFunc` 读 `Trigger` 的 buffered channel 长度（`validation.go`） | 「欠了一次 pass」 | P2 |

### 4.3 血缘：analyzer / OpenLineage 摄取 / revalidation

**lineage analyzer（`backend/runner/lineageanalyzer/{analyzer,queue,analyze,build_sql,persist,classify}.go`）**

最重要的实现约束：`drainAndAnalyze` **只能看到非 nil error**，而 `analyzeObject` 对
「解析失败清空血缘」、「空定义清空」、「定义不可读保留旧边」、「引擎无分析器跳过」、「成功」全部返回 `nil`。
**如果只在 drain 的 err 分支计数，会永远显示零失败、系统看起来永远健康。**
失败原因必须在 `analyzeObject` 的各个终态分支内埋（约 11 处），drain 只补 timeout/stale/panic。

| 指标 | 类型 | 标签 | 埋点 | 语义 / 注意 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_lineage_analysis_objects_total` | counter | `meta_type,engine,result` | `analyzeObject`/drain 的每个终态分支 | `result` ∈ `stored, engine_unsupported, definition_unavailable, definition_cleared, analysis_failed, interrupted, store_error, gone, stale, timeout, panic`。`stored` 包含「产出 0 条边」 | P0 |
| `metaxisdata_lineage_analyze_duration_seconds` | histogram | `engine,meta_type` | 包住 `lineage.Analyze`（`analyze.go` 的 `analyzeObject`） | 纯解析耗时，不含 store 往返；上层用 `analysis_objects_total` 看规模 | P0 |
| `metaxisdata_lineage_analysis_queue_depth` | gauge | `queue=event\|scan` | scrape-time Range `analyzeMap`/`scanMap` | `sync.Map` 无 Len；含退避等待项，depth ≠ 可立即执行量 | P0 |
| `metaxisdata_lineage_analysis_in_flight` | gauge | — | 工作函数（`analyze.go` 的 `drainAndAnalyze`）inc / defer dec | 并发上限 10（`MaxGoroutines`） | P0 |
| `metaxisdata_lineage_edges_written_total` | counter | `meta_type,kind=column\|table` | 写成功后（`analyze.go` 的 `analyzeObject`） | **提交给 replace 的边数**（store 是删+重插，且 `ON CONFLICT DO NOTHING` 会丢重复，`RowsAffected` 被丢弃），精确插入数需要改 store，标为「上界」 | P0 |
| `metaxisdata_lineage_analysis_retries_total` | counter | `attempt=1\|2\|3` | `scheduleRetry`（`queue.go`） | 30s/2m/5m 退避 | P1 |
| `metaxisdata_lineage_analysis_retries_exhausted_total` | counter | `meta_type` | 放弃分支（`queue.go` 的 `scheduleRetry` 内） | 「持续失败、等下轮全量扫描」告警 | P0 |
| `metaxisdata_lineage_analysis_batch_size` | histogram | — | `queue.go` 的 `takeAnalyzeBatch` 返回值 | 10s tick 取多少；上限 1000/tick = 100 obj/s 天花板 | P1 |
| `metaxisdata_lineage_scan_duration_seconds` | histogram | `outcome=completed\|deadline` | `queueAllBounded`（`analyzer.go`） | 每小时全量扫描，5 min 预算 | P1 |
| `metaxisdata_lineage_scan_objects_total` | counter | `meta_type,action=queued\|skipped` | `analyzer.go` 的 `queueAll` | `skipped` = 指纹/版本命中，不做重算 | P1 |
| `metaxisdata_lineage_scan_errors_total` | counter | `meta_type,stage` | `analyzer.go` 的 `queueAll` | 全量扫描的列表读取失败 | P1 |
| `metaxisdata_lineage_analysis_gaps_total` | counter | `category,meta_type` | `analyze.go` 的 `analyzeObject`（`*UnsupportedStatementError` 在手） | 部分分析（有边但有未建模 gap），与「完全没血缘」区分 | P1 |

> 不要 hook 在 `lineage.Analyze`（`backend/plugin/lineage/lineage.go`）：它有三个调用方
> （runner、OpenLineage SQL facet、`AnalyzeSQL` RPC），混在一起会污染后台分析的数字。

**OpenLineage 摄取（同步、请求 goroutine 内完成，60s 超时）**

| 指标 | 类型 | 标签 | 埋点 | 语义 / 注意 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_openlineage_ingest_failures_total` | counter | `failure_kind,route=event\|batch` | `reportIngestionFailure` + 两个 401 分支（`auth`）+ 限流 deny（`rate_limited`） | 被拒绝的请求按原因分类；`failure_kind` 是 9 个有界值：storepb 枚举的小写形式（`invalid_event`/`limit_exceeded`/`scope_mismatch`/`persist_failed`/`process_failed`/`namespace_unmapped`/`unspecified`）加 `auth`、`rate_limited` | P0 |
| `metaxisdata_openlineage_ingest_events_total` | counter | `route,result=accepted\|rejected,reason=parse\|limits\|scope\|persist\|process\|none` | 单事件与批次各自的终态分支 | 事件级（一次 1000 条的批次半成功会按事件计数）；accepted 事件的 `reason=none`（不接受空标签值） | P0 |
| `metaxisdata_openlineage_ingest_process_duration_seconds` | histogram | `route` | `handleIngestion`（单事件）/ `processBatchEvents`（批内每事件），`openlineage_handler.go` | 血缘推导（校验 + 可选 SQL 分析 + 写边），整个请求的延迟由 HTTP 指标覆盖 | P0 |
| `metaxisdata_openlineage_edges_total` | counter | `kind=column\|table` | `recordLineageEdges`（`processor.go`）写成功后 | 「Airflow 有没有给我们列级血缘」 | P0 |
| `metaxisdata_openlineage_runs_persisted_total` | counter | `event_type`（归一化） | `handleIngestion` 的 `UpsertOpenLineageRun`、`processBatchEvents` 的 `UpsertOpenLineageRuns` | run 行写入量（重投会更新同一 run，计写入不重复） | P1 |
| `metaxisdata_openlineage_edges_degraded_total` | counter | — | `ProcessRunEvent`（`processor.go`，来自 `report.degradedEdges`） | 生产者列声明与 registry 冲突被清空，保留表级依赖 | P1 |
| `metaxisdata_openlineage_edges_unknown_relation_total` | counter | — | 同上 | registry 还不认识的端点，交给 revalidation | P1 |
| `metaxisdata_openlineage_events_no_lineage_total` | counter | `reason=non_complete\|sql_facet_without_datasets\|no_edges` | `ProcessRunEvent`（`processor.go`） | COMPLETE 但没产出；`sql_facet_without_datasets` 是文档里的 Airflow 配置错误 | P2 |
| `metaxisdata_openlineage_table_level_fallback_total` | counter | — | `sqlFacetLineage`（`processor.go`）的 SQL 无法分析分支 | SQL facet 无法分析、退化为表级边 | P1 |
| `metaxisdata_openlineage_dataset_resolutions_total` | counter | `caller=ingest\|preview,outcome=manual_mapping\|auto_match\|external` | `ResolveDataset`/`ResolveDatasetPreview`（`resolver.go`） | 同一个 resolver 也服务读页面，**必须带 caller** 否则摄取数字被页面访问放大 | P1 |
| `metaxisdata_openlineage_external_dataset_resolutions_total` | counter | `dataset_type` | `ResolveDataset`（`resolver.go`） | 落到外部数据集的比例 | P2 |
| `metaxisdata_openlineage_ingest_batch_size` | histogram | — | `processBatchEvents`（`openlineage_handler.go`） | 批次行为 vs 1000 上限 | P2 |

**revalidation（两个 runner 调用同一对 Processor 方法）**

`RevalidateUnresolvedLineage` / `RevalidateContradictedColumnClaims` 同时被 maintenance（6h）和
post-sync runner 调用。**计数必须埋在这两个 Processor 方法里并带 `sweep` 标签**，
不能埋在两个 runner 里（否则双计）。

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_openlineage_revalidation_batches_total` | counter | `sweep=unresolved_endpoint\|contradicted_column_claim` | `RevalidateUnresolvedLineage`/`RevalidateContradictedColumnClaims`（`lineage_revalidation.go`） | 每次一批（500 条上限） | P1 |
| `metaxisdata_openlineage_revalidation_duration_seconds` | histogram | `sweep` | 同上 | 读+校验+重写一批的成本 | P1 |
| `metaxisdata_openlineage_revalidation_edges_total` | counter | `sweep,result=read\|revalidated\|degraded` | 同上 | 每轮工作量，**不是去重后的边数** | P1 |
| `metaxisdata_openlineage_revalidation_still_unknown_total` | counter | `sweep` | 同样的两个 `Revalidate*` 方法（`lineage_revalidation.go`） | 端点仍然未知的 run 数（backlog 增长信号） | P1 |
| `metaxisdata_openlineage_revalidation_truncated_total` | counter | `sweep` | 同样的两个 `Revalidate*` 方法（`lineage_revalidation.go`） | 一批没排干 | P1 |

### 4.4 数据质量：调度 / 规则执行 / 问题台账 / 目标库查询 / 画像

**调度与执行（`backend/runner/qualityrunner/{qualityrunner,types,contract,schedule,plan,admission,execute,execution_result,issue,notify,format}.go`、`backend/api/v1/data_quality_service.go`）**

`quality_run` 是内部派发批次（连接准入、一个 tick 的分组、风暴聚合、保留期级联），
**没有任何指标以它为单位**；对外指标一律按执行（`quality_execution`）计。

| 指标 | 类型 | 标签 | 埋点 | 语义 / 注意 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_quality_due_scan_duration_seconds` | histogram | — | `scanDue` 的规划段（settings/quota/`FindDueQualityTargets`/`planDueBatches`），**不要包住 `wp.Wait()`** | 调度层成本；包住执行会退化成批次耗时的重复 | P0 |
| `metaxisdata_quality_due_rules` | histogram | — | `FindDueQualityTargets` 之后 | 每次扫描到期规则数；p95 顶到 200 说明积压排不干 | P0 |
| `metaxisdata_quality_executions_created_total` | counter | `trigger=scheduled\|manual` | scheduled 在 `PlanQualityExecutions` 成功后逐行计（`startScheduledBatch`）；manual 在 API 规划成功后逐行计（`planManualQualityExecutions`） | 规划的执行行数（含之后被放弃的） | P0 |
| `metaxisdata_quality_executions_completed_total` | counter | `outcome`（6 个 QualityStatus） | `FinishQualityExecution` 成功后（`executeBatch`） | 正常收尾的执行及其判定；`UNSPECIFIED` 不会出现（ABANDONED 不计） | P0 |
| `metaxisdata_quality_execution_admissions_total` | counter | `trigger,result=admitted\|deferred` | scheduled `startScheduledBatch`、manual `executeManualBatch` 的 `admitBatch` 之后 | 连接预算准入结果（按批次一次） | P0 |
| `metaxisdata_quality_executions_deferred_total` | counter | `reason=admission_timeout\|instance_read_error` | scheduled 的 `startScheduledBatch` | 定时执行因实例预算/实例读取失败没被规划；目标保持 due 等下轮，同实例本轮剩余目标也跳过（manual 不 deferred，拿不到即放弃） | P0 |
| `metaxisdata_quality_executions_abandoned_total` | counter | `reason=admission_timeout\|instance_read_error\|stale_sweep\|persist_failed\|incomplete\|plan_failed\|queue_full\|mark_running_failed` | startup sweep、manual 准入失败/实例读取失败、`abandonBatch`、规划失败、API 队列满、批次超时/未全部落终态（`incomplete`）、批次无法进入 RUNNING 即被关闭（`mark_running_failed`） | 「执行在悄悄消失」的告警面（按执行行计） | P0 |
| `metaxisdata_quality_executions_in_progress` | gauge | — | `MarkQualityExecutionRunning` 成功后 inc，终态写入后 dec | 正在执行的规则（两个实例各自串行，名义上限 2） | P0 |
| `metaxisdata_quality_manual_queue_depth` | gauge | — | `GaugeFunc` 读 `len(r.manual)`（`chan`，容量 512 个批次） | 手动批次积压 | P0 |
| `metaxisdata_quality_manual_enqueue_rejected_total` | counter | — | `EnqueueQualityExecutions` 的 default 分支 | 队列满 → API 返回 ResourceExhausted | P0 |
| `metaxisdata_quality_rule_executions_total` | counter | `rule_type,status` | `executeBatch` 的 per-execution 循环，在终态写入成功后 | 一次执行一条；状态是六态判定（含 SKIPPED/NOT_EVALUATED） | P0 |
| `metaxisdata_quality_rule_execution_duration_seconds` | histogram | `rule_type` | 同上，用 `result.DurationMS`（从 runner 拾起到写完判定） | 单规则端到端（建连+聚合+采样+判定），不含准入等待；目标库耗时看 `query_readonly_*` | P0 |
| `metaxisdata_quality_rule_errors_total` | counter | `rule_type,error_code` | 同上，`result.ErrorCode != ""` | error code 是已有稳定枚举（`CONNECTION/AUTH/TIMEOUT/...`）；`QUOTA` 是历史值，出现即回归 | P0 |
| `metaxisdata_quality_rule_skips_total` | counter | `reason=inflight\|retired\|orphaned\|lease_unavailable` | **（未实现）**`executeOne` 的四个 SKIPPED 返回点 | 其中两类 `error_code` 为空，无法从错误计数推出 | P1 |
| `metaxisdata_quality_rule_panics_total` | counter | — | **（未实现）**`executeOneSafe` recover | 把 panic 和普通 EXECUTION 失败区分开 | P1 |
| `metaxisdata_quality_storms_total` | counter | — | **（未实现）**`isQualityStorm` | 一次内部批次里 >10 条 FAIL | P2 |
| `metaxisdata_quality_issues_opened_total` | counter | `severity` | **（未实现）**新开问题插入成功后 | 新开问题（首次 FAIL） | P1 |
| `metaxisdata_quality_issues_touched_total` | counter | — | **（未实现）**`TouchQualityIssue` 成功后 | 既有问题再次命中 | P1 |
| `metaxisdata_quality_issues_resolved_total` | counter | — | **（未实现）**`ResolveQualityIssue` 成功后 | 自动恢复关闭 | P1 |
| `metaxisdata_quality_notifications_total` | counter | `kind,result=ok\|error` | **（未实现）**`sendDataQualityNotification` | **投递尝试**，不是收件箱行数（下游有去重） | P2 |

**目标库只读查询（`backend/plugin/db/query.go:69-112`）**

产品里所有目标库只读 SQL 都走 `RunReadOnlyQuery`，这是最省的单一埋点。`QueryOptions` 已有
`Purpose` 字段（6 个调用点分别标记聚合/采样/CEL 采样/画像聚合/画像采样/dry-run）；
`Options.Metrics` 把记录器带进这个包，避免再穿透构造链。

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_query_readonly_total` | counter | `purpose,outcome=success\|failure` | `RunReadOnlyQuery` | 每次目标库只读语句；`purpose` 为空时折叠为 `unspecified` | P0 |
| `metaxisdata_query_readonly_duration_seconds` | histogram | `purpose` | 同上（从 begin 到 commit） | 「目标库慢」还是「调度慢」的分界线 | P0 |
| `metaxisdata_query_readonly_rows` | histogram | `purpose` | 扫描结束后 | 聚合类恒为 1；采样/画像是关注点 | P1 |
| `metaxisdata_query_readonly_truncated_total` | counter | `purpose` | `result.Truncated` | 行/字节上限截断 | P2 |
| `metaxisdata_quality_profile_total` / `_duration_seconds` | counter/histogram | `outcome` / — | **（未实现）**画像请求（两次目标库查询） | 画像请求量与耗时 | P1 |
| `metaxisdata_quality_profile_columns` | histogram | `engine` | **（未实现）**单次画像列数（上限 200） | 列数分布 | P2 |

> 不要用 `acquireQualityConnection` 的 `action string`（人类文案 `"the dry run"`）当标签；
> 用 typed purpose 或在两个调用点映射。

### 4.5 出站通知投递

`backend/runner/notificationdispatcher` 是唯一埋点处：它认领 outbox 行、渲染并 POST。

| 指标 | 类型 | 标签 | 埋点 | 语义 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_notification_deliveries_attempted_total` | counter | `outcome=sent\|failed\|dead` | 每次认领的投递结束（`attempt`/`finishFailure`） | 一次事件 × 渠道的投递尝试结果；`dead` 含配置错误与 6 次耗尽 | P0 |
| `metaxisdata_notification_delivery_duration_seconds` | histogram | — | `receiver.Post` 前后 | 单次 HTTP 投递耗时（不含认领/退避） | P0 |

队列深度 gauge 尚未实现（需要 `COUNT(*) WHERE state IN (1,2)`），是 P1。

### 4.6 LLM

**推荐在 agent loop 内部统一埋点**，而不是在每个调用点各埋一遍：给 `llm.AgentConfig`
（`backend/component/llm/event.go` 的 `AgentConfig`）加 `Feature string` 和 `Metrics *metrics.Metrics`，
`RunAgentLoop`（`backend/component/llm/agent.go`）在唯一出口处记录**调用数、耗时、token 与提供商/传输层错误**
（空响应、stream 错误、达到最大轮数）。调用方（DQ 的三个分析 + ExplainSQL）只需填 `Feature`。
注意：**JSON 结构解析失败发生在调用方**（DQ 的严格解析，模型答了但格式不合格），不在 agent loop 里，
所以 `llm_parse_failures_total` 仍埋在 DQ 的解析点。

**token 的 v1 范围（已确认）**：DQ 路径已经请求 `IncludeUsage`，v1 就保证这部分 token；
ExplainSQL 的 `AgentConfig` **暂不打开** `IncludeUsage`（部分 OpenAI 兼容网关会拒绝 `stream_options`，
拒绝整个请求比缺 usage 更糟），先由 `llm_usage_missing_total{feature="explain_sql"}` 暴露缺失；
等确认部署侧网关支持后，再单独开一个 PR 打开并补齐 token。

| 指标 | 类型 | 标签 | 埋点 | 语义 / 注意 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| `metaxisdata_llm_calls_total` | counter | `feature,provider,outcome=success\|error\|empty_response\|canceled` | `RunAgentLoop` 出口 | 一次完成请求（L3 列分析按批次计，1 个 RPC 可 1–3 次） | P0 |
| `metaxisdata_llm_call_duration_seconds` | histogram | `feature,provider` | 同上 | 提供商往返；无总超时（header 30s / idle 60s / 最多 6 轮），桶要覆盖到 300s | P0 |
| `metaxisdata_llm_tokens_total` | counter | `feature,provider,kind=prompt\|completion` | 消费 `AgentEventUsage`/`AgentEventAgentEnd.Usage` | 提供商上报才有；**不是账单**。v1 只保证 DQ 路径（已开 `IncludeUsage`）；ExplainSQL 的 `IncludeUsage` 待验证网关后再开（已确认） | P0 |
| `metaxisdata_llm_usage_missing_total` | counter | `feature` | 无 usage 事件时 | 提供商没报 usage → token 面板会低估；也是网关不支持 `stream_options` 的信号。ExplainSQL 的 token 在 v1 不保证，**这条是它的补偿信号**，因此列为 P0 | P0 |
| `metaxisdata_llm_parse_failures_total` | counter | `feature` | DQ 三个严格 JSON 解析失败点（`data_quality_llm_analysis.go`、`data_quality_label_analysis.go`） | 模型答了但结构不合格，不缓存不落库 | P0 |
| `metaxisdata_llm_cache_events_total` | counter | `feature=explain_sql\|dq_explain_result,event=hit\|miss\|write_error` | ExplainSQL 缓存命中/未命中/写失败（`explain_sql_service.go`）；DQ L2（`data_quality_llm_analysis.go`） | 缓存规避了多少模型调用 | P1 |
| `metaxisdata_llm_quota_rejections_total` | counter | `kind=analysis\|column_analysis` | `LLMQuota.quotaExhaustedLocked`（`backend/component/dataquality/quota.go`） | **仅日配额拒绝**（ResourceExhausted）；共享并发信号量是排队等待，不是拒绝，因此没有对应计数 | P0 |
| `metaxisdata_llm_quota_used` | gauge | `kind` | `LLMQuota.Begin` 成功后（`backend/component/dataquality/quota.go`），UTC 日切重置 | 进程本地当日用量 | P1 |
| `metaxisdata_llm_analyses_in_flight` | gauge | — | `GaugeFunc` 读 semaphore 长度 | 并发上限 2，共享，**不要加 kind 标签** | P1 |
| `metaxisdata_llm_concurrency_wait_seconds` | histogram | `kind` | 配额通过到拿到 semaphore（`LLMQuota.Begin`，`backend/component/dataquality/quota.go`） | 「提供商慢」还是「我们自己的并发闸门」 | P2 |
| `metaxisdata_llm_debug_log_dropped_total` | counter | `reason=queue_full\|shutdown` | `debugLogWriter.enqueue`（`backend/component/llm/debug.go`） | 调试日志是 best-effort，丢弃要可见 | P2 |

---

## 5. 跨域实现约束（不变量）

这些是「照抄会出错」的地方，实现时逐条对照：

1. **gate 复用只记 owner。** `SyncInstance` / `SyncDatabaseSchema` 的等待者会复用 owner 结果；
   在函数入口/出口埋点会把一次同步记成两次，并把等待时间记成同步耗时。
2. **共享函数埋一次。** API handler 与后台 runner 调用同一实现；只在共享函数埋，绝不在 handler 再埋。
3. **提交后才计成功。** 元数据差异、DDL 行写入都在事务内；在 `BeginTx` 与 `Commit` 之间埋点会把回滚的工作算进去。
   例外：DDL 抓取失败发生在事务前，照计。
4. **失败原因只用已有分支能证明的分类。** driver error 没有 transient/permanent 类型；
   不要发明 `transient`、`driver`、`store` 这类标签值。
5. **analyzer 的失败必须埋在执行单元内部。** 见 §4.3（drain 看不到确定性失败）。
6. **通知尝试 ≠ 投递行数。** 通知服务按 `(recipient, dedupe_key)` 去重。
7. **队列深度用 scrape-time collector，不要维护原子计数器。** `sync.Map` 会在 overwrite 时丢代际，
   原子计数容易漂移；每次 scrape Range 一遍是可接受的（对象数在千级）。
8. **热路径谨慎。** 只有连接 governor 的 `acquire` 是每条 driver 打开都会走的热路径；
   其余埋点都在「一次同步/一次 run/一次 pass」粒度。
9. **REST gateway 双层。** HTTP 计数跳过内部回环，否则一次 REST 调用计两次。
10. **业务身份不进标签。** 需要按实例/表下钻时，用日志、审计或 store 查询，不要开指标口子。

---

## 6. 实现状态

v1（P0 66 条，按目录行计）已经实现，按四个可独立评审的范围落地：

| 范围 | 状态 | 主要落点 |
| --- | --- | --- |
| 基础设施 | 已实现 | `backend/metrics/`（全部 P0 instrument、proto 枚举→标签映射、nil 安全的记录方法、黄金指标名/标签白名单/nil 安全测试）；独立 `--metrics-addr` 监听器与私有 registry（`backend/server/server.go`）；HTTP 中间件（`backend/server/http_metrics.go`）；ConnectRPC 拦截器（`backend/api/v1/metrics_interceptor.go`）；`runner.Go` 的 up/panics；`Store.DBStats` 与自研 `metaxisdata_db_pool_*` collector；删除废弃的 echo-contrib 中间件 |
| 元数据采集 | 已实现 | `schemasync`（owner-only gate、事务提交后按类型计数、扫描/实例/库/驱动耗时、重试与耗尽、连接预算背压、失败原因）；`maintenance` pass 级汇总（lineage 边数不计入 deleted_rows）；`lineagevalidation` 触发/pass/边；governor 的非阻塞租约拒绝 |
| 血缘 / OpenLineage | 已实现 | `lineageanalyzer` 的每个终态分支（interrupted 与 timeout 用 sentinel 去重）、解析耗时、in-flight、重试耗尽、列/表边；队列深度 scrape collector；OpenLineage 请求失败（含 401 与限流拒绝）、事件级接受/拒绝、处理耗时、摄取边 |
| 数据质量 / LLM / 目标库 | 已实现 | `qualityrunner` 调度、执行规划与生命周期、准入/延后/放弃、规则漏斗（每次执行一次）；`db.RunReadOnlyQuery` 单一埋点（`QueryOptions.Purpose`）；`RunAgentLoop` 统一记录 calls/duration/tokens/usage_missing；DQ 解析失败与 LLM 配额拒绝 |

测试与门禁：`backend/metrics` 有黄金指标名 + 标签白名单 + scrape collector + nil 安全测试；各域新增 hermetic 测试；`go build ./...`、`go vet -tags integration ./...`、`go test ./...` 通过。P1/P2 指标（例如 schemasync 队列 in-flight、DDL 明细、scan 指纹跳过、issue 计数、LLM cache 事件、连接等待时长直方图）尚未实现，命名与语义已在 §4 预留。

## 7. 兼容性与文档变更

### 7.1 破坏性变化（仅一处）

- `api_requests_total`、`api_request_duration_seconds`、`api_request_size_bytes`、
  `api_response_size_bytes` 随废弃中间件一起消失，改名为
  `metaxisdata_http_*` / `metaxisdata_rpc_*`。**已确认直接改名，不保留兼容路径**
  （仓库内没有任何 dashboard/alert 引用这些名字，已 grep；且它们原本只在 debug 开关下可达）。
  release note 里写明即可。
- `--debug` 不再暴露 `/metrics`（只保留 pprof）。`docs/deploy.md`（环境变量表与 flags 段）和
  `docs/deploy_zh.md` 镜像修改；`setting_service.go` 与 `setting_service.proto` 里「runtime debug 暴露
  pprof 和 metrics」的注释要删掉 metrics。

### 7.2 文档与记录

- 新增运维说明：`docs/deploy.md` 在「Health and build metadata」之后新增「Prometheus metrics」小节，
  写清 `METAXISDATA_METRICS_ADDR`、推荐绑定地址、指标名分组与 scrape 方式。
- `docs/security-posture.md` 新增一条已接受决策：指标默认关闭、独立监听、**匿名无鉴权**、
  只应绑定在可信网络边界；`--debug` 只影响 pprof。
- `.agents/docs/security-open-items.md` 的 M25 收窄为「pprof 仍只有 RuntimeDebug 一个闸门」。
- `.agents/docs/backend-open-items.md`：D8 关闭（本方案即「重建遥测设施」的决定），并清掉过时的文档残留行。
- `.agents/docs/runner.md`、`data-quality.md` 中「no metrics」的已知缺口条目更新。
- `backend/AGENTS.md` 包归属表加 `backend/metrics/`。
- `.agents/docs/README.md` 加本文件一行。
- 实现完成后，本文件改写为「已实现」维护参考。

### 7.3 配置面汇总

| 位置 | 变化 |
| --- | --- |
| `backend/bin/server/cmd/root.go` | 新 flag `--metrics-addr`（默认 `""`） |
| `backend/config/profile.go` | `MetricsAddr string` |
| `scripts/docker/metaxisdata-entrypoint.sh` | `METAXISDATA_METRICS_ADDR` → `--metrics-addr` |
| `docs/deploy.md` / `docs/deploy_zh.md` | env 表 + flags 段 + metrics 小节 |
| `docker-compose.yml` | 可选：注释示例（本次不做资产） |

---

## 8. 测试与验证

- `backend/metrics/metrics_test.go`：`TestRegisteredInstruments`（指标名 + 标签集合的黄金表）、`TestScrapeCollectors`、`TestNilMetricsIsNoop`、`TestLabelMappings`、`TestRecordersEmitExpectedLabels`（记录器端到端的标签与数值）。
- `backend/server/http_metrics_test.go`：跳过的路由、gateway 回环、route 模板折叠、被 recover 捕获的 panic 记为 500；`backend/server/metrics_endpoint_test.go`：私有 registry 的 `/metrics` 响应、非 GET/HEAD 返回 405、监听器启动/抓取/关闭、不可用地址启动失败；`backend/server/openlineage_ingestion_test.go`：限流拒绝的 `failure_kind=rate_limited` 计数。
- `backend/api/v1/metrics_interceptor_test.go`：procedure 拆分与 Connect code 映射。
- 各域 hermetic 测试：`schemasync`（失败原因、按类型聚合、重试）、`lineageanalyzer`（终态映射、interrupted/timeout 去重、边计数）、`openlineage`（failure kind、route、边拆分、单事件解析失败计数）、`qualityrunner`（规则漏斗标签）、`component/llm`（一次调用、usage_missing、empty response）、`plugin/db`（purpose 传递）。
- 门禁：`gofmt`、`golangci-lint run --allow-parallel-runners`（本机 v2.14 会报 3 条既有的 `multiline-if-init`，均在本次未触碰的文件）、`go build ./...`、`go vet -tags integration ./...`、`go test ./...`。
- **未做端到端 scrape 集成测试**：指标端点是可选项，集成 harness 启动真实二进制时尚未传 `--metrics-addr`；监听器与处理器已由单元测试覆盖。

## 9. 已知限制与后续增量

1. **P1/P2 尚未实现。** §4 每张表的「优先级」列即实现状态；命名已冻结，后续实现不需要改契约。
2. **连接等待时长直方图**（唯一的热路径埋点）在 P1；v1 用 `in_use` / `waiters` / `try_acquire_rejected` 观察饱和度。
3. **ExplainSQL 的 token 不在 v1 范围内。** 它不请求 `IncludeUsage`（部分 OpenAI 兼容网关会拒绝 `stream_options`），缺失由 `metaxisdata_llm_usage_missing_total{feature="explain_sql"}` 暴露；确认部署侧网关后再单独打开。
4. **`metaxisdata_lineage_edges_written_total` 是上界。** store 的 delete+reinsert 与 `ON CONFLICT DO NOTHING` 使精确插入数不可得，未改 store 签名。
5. **失败原因粒度有限。** 驱动错误没有 typed 分类，`reason` 只到 `connection_limit` / `hidden` / `not_found` / `timeout` / `canceled` / `other`；不要臆造 transient/permanent。
6. **drain 级 outcome 的 `engine` 为 `unknown`。** 对象级 engine 在 drain 处不可得（`stale` / `timeout` / `panic`）；`interrupted` 在 `analyzeObject` 内记录，带真实 engine。
7. **指标是进程本地、单副本语义。** 重启归零；不要用 `increase()` 跨重启做长期账。
8. **HTTP 与 RPC 计数不可相加。** REST gateway 的内部回环已从 HTTP 计数中排除：HTTP 指标是客户端 HTTP 请求，RPC 指标是 API 调用。
9. **指标端点匿名。** 无鉴权，只应绑定在可信接口/网络边界内；见 `docs/security-posture.md`。
10. **日执行配额没有专用 gauge。** 用 `increase(metaxisdata_quality_executions_created_total[1d])`
    对照工作区设置 `daily_execution_limit` 观察饱和度；配额触顶只跳过定时 due 扫描，手动运行不受影响。

## 附录 A — 基数估算（全部标签组合都被观测到的上限）

| 域 | 估算 series（含 `_bucket` 展开前） |
| --- | --- |
| HTTP/RPC | ≈ 60 route × 5 method × 15 status（实际稀疏）+ 141 RPC × 18 code（实际稀疏）→ 活跃通常 < 500 |
| schemasync | engine(9) × outcome(2) × 若干 + object_type(10) × 3 + 12 项维护 → < 200 |
| lineage | meta_type(4) × engine(9) × result(11) ≈ 396 + 直方图 ≈ 500 |
| openlineage | failure_kind(9) × route(2) + 事件/边/解析 < 200 |
| quality | rule_type(19) × status(7) ≈ 133 + error_code ≈ 247 + 直方图 ≈ 400 |
| query | purpose(6) × outcome(2) + 直方图 < 150 |
| llm | feature(4) × provider(配置数) × outcome(4) + 直方图 < 200 |
| 运行时 | go/process/db_pool ≈ 40 |
| **合计** | **约 2,000–3,000，目标 < 3,000** |

## 附录 B — 建议的直方图桶

| 指标族 | 桶 |
| --- | --- |
| HTTP 请求时长 | `.005 .01 .025 .05 .1 .25 .5 1 2.5 5 10 30 60 300 1800 3600`（含长连接流） |
| RPC unary 时长 | `.001 .005 .01 .025 .05 .1 .25 .5 1 2.5 5 10 30 60 120 300 600 1200`（覆盖 20 min 长 unary 上限） |
| RPC stream 时长 | `1 5 15 30 60 120 300 600 1200 1800 3600` |
| schemasync 扫描/同步 | `1 5 15 30 60 120 300 600 900` |
| DDL/统计刷新 | `0.01 .05 .1 .5 1 5 10 30 60 300` |
| lineage 解析 | `.01 .05 .1 .25 .5 1 2.5 5 10 30 60 120` |
| OpenLineage 处理 | `.005 .01 .05 .1 .25 .5 1 2.5 5 10 30 60` |
| revalidation 批 | `.05 .1 .5 1 2.5 5 10 30 60 120 300` |
| quality 单规则 | `.01 .05 .1 .25 .5 1 2.5 5 10 30 60 120 300 600` |
| 目标库只读查询 | `.001 .005 .01 .05 .1 .25 .5 1 2.5 5 10 30 60 120` |
| LLM 调用 | `.25 .5 1 2.5 5 10 20 30 60 120 300` |
| 连接等待 | `.001 .005 .01 .05 .1 .5 1 5 10 30 60` |
| DB 连接等待时长 | 用 counter，不用直方图 |
