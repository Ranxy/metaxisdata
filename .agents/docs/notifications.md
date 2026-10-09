# 站内信与通知推送 — Reference

> Status: **implemented**（站内信 `4fb0e00`/`32a6bd2`，服务端推送 `a00e3e0`）。本文件是 `backend/component/notification/`、`backend/api/v1/notification_service.go`、`backend/runner/schemasync/operation.go` 与前端 `notification*` 这一族的维护参考。
> Related: [proto/v1/v1/notification_service.proto](proto/v1/v1/notification_service.proto)、[proto/store/store/notification.proto](proto/store/store/notification.proto)、[docs/security-posture.md:31](docs/security-posture.md#L31)。
> 行形状与公开面的字段清单以 proto 为准；本文件只记契约、不变量与耦合。

## What it is

按**用户**投递的站内信，外加一条常驻的服务端推送流（ConnectRPC server streaming）。消息只有两个来源：

| 来源 | 触发 | 收件人 | detail |
| --- | --- | --- | --- |
| schema 同步操作 | 人工同步 / 建实例后首次同步 / 周期后台同步失败 | 发起人；后台失败给工作区管理员 | `SchemaSyncDetail` |
| OpenLineage 摄取 | 4xx 拒收、5xx 失败、namespace 未映射 | 工作区管理员（摄取用 API key，无发起用户） | `OpenLineageDetail` |

消息正文**不落文本**：服务端只存 `type + severity + 结构化 detail`，前端用 i18n 渲染（`en-US` / `zh-CN`）。这是双语与 proto3 前向兼容（老前端遇新类型降级为通用文案）的前提。

`notification` 表是**个人数据**：与其余所有表不同，读路径按 `recipient_id` 作用域而不是 workspace，因此表本身没有 workspace 列（[0017##notification.sql:1-8](backend/migrator/migration/0.1/0017%23%23notification.sql#L1-L8)）。

**两个数不是一回事**：`unread`（未读，`read_at IS NULL`）是消息自身的状态，只有点开某一条或"全部已读"才变；`unseen`（未查看）是未读里、`created_at > principal.notification_seen_at` 的那些，是铃铛红点的数字。打开通知（铃铛下拉或 `/notifications`）只把 `notification_seen_at` 推到当前时间，红点清零而消息仍未读——用户点了哪条，哪条才变已读。

## Decisions

站内信侧（写入口径）：

| # | 决定 | 理由 / 影响 |
| --- | --- | --- |
| 1 | 收件人 = **操作发起人**；只有后台/周期同步失败发给**工作区管理员** | 需要"操作"实体与角色→用户解析 |
| 2 | 只做消息中心，不建同步任务表、不展示进度 | 聚合状态在 syncer 进程内，不是新表 |
| 3 | 粒度 = **一次同步操作一条** | 引入 operation 聚合器 |
| 4 | 首批事件：schema 同步 + OpenLineage 摄取异常 | 两类 detail |
| 5 | 仅站内信（铃铛 + 列表），不做邮件 / webhook | 无出站投递器 |
| 6 | **永久保留**，不接 TTL 清理 | 无 TTL，表持续增长，量级话题是分区/归档 |
| 7 | 后台通知抑制窗口默认 **1 小时桶**（`NAMESPACE_UNMAPPED` 为 24 小时） | 常量，可调；两层抑制必须一起调 |
| 8 | 建实例后的首次全量同步**要发**消息（发起人 = 创建者） | `CreateInstance` 也登记操作 |
| 9 | OpenLineage 的 **4xx 拒收**与 **namespace 未映射**都通知管理员 | 通知点覆盖全部拒收分支 + resolver |
| 10 | 管理员手动发消息**不做** | 正文仍全部走 i18n，不引入自由文本 |
| 11 | 一个库在"这次操作"里的结果 = **它的第一次结果** | 退避重试（1m/5m/15m）不改写消息、不补发；代价是瞬时失败也出现在消息里 |
| 12 | **红点数字 = 上次打开通知之后写入的未读消息数**（`principal.notification_seen_at` 水位），不是未读数 | 用户看的是"有没有新的"，不是"还有几条没点"；未读态留在列表里由用户自己清 |
| 13 | 打开通知**只推水位、不改读态** | 打开下拉/页面就清红点，消息仍需用户点开才变已读；`read_at` 只有 `BatchMarkNotificationsRead` / `MarkAllNotificationsRead` 能写 |
| 14 | 下拉/页面**打开期间**到达的消息不点亮红点，并顺带推进水位 | 它此刻正出现在用户看的列表里；否则刚看完列表又被提示"有新消息" |
| 15 | 水位由**服务端 `NOW()`** 打戳 | 与 `read_at` 同约定：客户端不能自报"我在某条消息之前就看过" |

推送侧：

| # | 决定 | 理由 / 影响 |
| --- | --- | --- |
| 1 | 用 ConnectRPC **server-streaming RPC**，不用字面 SSE/`EventSource` | 复用 `ExplainSQL` 先例；auth/throttle/error-mapping 拦截器免费；前端保持 100% 收敛在 ConnectRPC client |
| 2 | 事件**带消息本身 + 两个最新计数**（`unread_count` + `unseen_count`） | 铃铛收到即更新，零额外往返；服务端每条消息多两次计数查询（都走同一个 partial index） |
| 3 | **单副本 / 进程内 hub** | 与"设备登录状态进程内""操作聚合器进程内"同取向；跨副本投递不做 |
| 4 | 覆盖铃铛 + `/notifications` **仅第一页**自动刷新 | 不打断分页阅读 |
| 5 | 去掉 30s 轮询，只在"重连成功"与"标签页重新可见"时各刷新一次 | 不再有周期性请求 |
| 6 | 连接时长上界 **30 分钟**，到点服务端主动收尾 | 流只在连接时鉴权一次，收尾逼客户端重连 = 重新鉴权，logout/撤销才有上界 |
| 7 | 心跳 **25s** 常驻显式 `KeepAlive` 空消息 | Connect protocol 无注释帧；同时是"对端已死"的写失败探测点；小于 nginx 默认 `proxy_read_timeout` 60s |
| 8 | 背压 = **丢订阅、不丢消息** | 订阅者缓冲满就关它的流，消息已在库里，客户端重连刷新 |
| 9 | **已读状态变化不推送** | 另一标签页标已读后角标要等刷新/重连/重新可见；推送它需要再定"谁改的、多标签页回声" |
| 10 | **水位的推进也不推送** | 与 9 同一取舍：标签页 A 清掉红点后，标签页 B 要等刷新/重连/重新可见（切过去就会触发可见性刷新） |

## 数据模型

表与索引（[LATEST.sql:678-705](backend/migrator/migration/LATEST.sql#L678-L705)，与 [0017##notification.sql](backend/migrator/migration/0.1/0017%23%23notification.sql) 同列同索引，只是注释长短不一）：

| 列 / 索引 | 形状 | 作用 |
| --- | --- | --- |
| `recipient_id` | `INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE` | 个人数据的作用域键 |
| `read_at` | `TIMESTAMPTZ`，`NULL` = 未读 | **读态是列，不是 JSONB 字段** |
| `dedupe_key` | `TEXT NOT NULL DEFAULT ''`，格式 `<event>:<target>:<bucket>` | 空串 = 不去重（发给发起人的个人消息） |
| `payload` | `JSONB NOT NULL DEFAULT '{}'` | `protojson` 的 `proto/store Notification`；`id`/`createTime`/`recipientId`/`readTime`/`dedupeKey` **不在** payload 里 |
| `idx_notification_recipient_created_at` | `(recipient_id, created_at DESC, id DESC)` | 收件箱列表 |
| `idx_notification_recipient_unread` | `(recipient_id) WHERE read_at IS NULL` | 未读计数只探索引 |
| `idx_notification_dedupe` | UNIQUE `(recipient_id, dedupe_key) WHERE dedupe_key <> ''` | 跨副本、跨重启的最终去重 |

**读态为什么是列**：未读计数是角标热路径，`payload->>'readTime' IS NULL` 走不了 partial index；`read_at` + 部分索引让计数成为一次索引探针。

水位在 `principal` 上（[0018##notification_seen.sql](backend/migrator/migration/0.1/0018%23%23notification_seen.sql)、[LATEST.sql:31-35](backend/migrator/migration/LATEST.sql#L31-L35)）：

| 列 | 形状 | 作用 |
| --- | --- | --- |
| `principal.notification_seen_at` | `TIMESTAMPTZ`，`NULL` = 从没打开过通知 | 红点的水位；`NULL` 时读作"所有未读都是新的"（SQL 里用 `COALESCE(..., '-infinity')` 表达）。**不在 `notification` 表上**：它是"我上次看收件箱是什么时候"，一行一个用户，而给每条消息记一个"看过"标志会让每次打开都 UPDATE 一整批行 |

两个数由**同一条语句**返回（[notification.go:151](backend/store/notification.go#L151)）：`COUNT(*)` 与 `COUNT(*) FILTER (created_at > 水位)`，`WHERE` 由 `notificationScope(recipientID, true)` 拼出，所以 `unseen ≤ unread` 是构造出来的，不是"通常成立"。分两条语句读会拿到两个快照，中间写入的那条能让红点数超过未读数。语句里 join `principal` 只为取水位，`recipient_id` 仍是第一个谓词，未读 partial index 照旧是入口。

## 写入路径

唯一写入口是 `notification.Service`（[service.go:83-95](backend/component/notification/service.go#L83-L95)），它同时是推送的起点：

| 方法 | 契约 |
| --- | --- |
| `Send(ctx, n)` | 投给一个收件人；`recipient_id` 必须 > 0 |
| `SendToWorkspaceAdmins(ctx, n)` | 解析 `roles/workspaceAdmin`（含组展开，跳过 `allUsers` 与 system bot）后逐条克隆投递，每人一行、各自读态；无管理员只记日志 |
| `create(ctx, n)` | 补齐 workspace 名 → 写库 → 若真写出新行则 `publish`；被 dedupe 抑制（`CreateNotification` 返回 `(nil, nil)`）时**不发布** |
| `publish(ctx, n)` | 在 `publishMu` 内"读一次计数（两个数） + 交给订阅者"，保证投递顺序与快照顺序一致；计数读不到仍投递消息本身（`Event.CountsKnown = false`，前端自己去问） |
| `Subscribe(recipientID) / Close()` | 订阅/停机收尾（见下节） |
| `DedupeKey(event, target, now, window)` | 生成 `<event>:<target>:<bucket>`；`window <= 0` 返回空串（不去重） |

关键实现事实：

- **best-effort 契约**：写通知失败只记日志，绝不影响同步/摄取路径（与 audit 拦截器同约定）。调用方显式忽略返回错误。
- **写入脱离请求取消但有界**：`Send` / `SendToWorkspaceAdmins` 用 `context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)`，`writeTimeout = 10s`（[service.go:65-68](backend/component/notification/service.go#L65-L68)）。
- **两层抑制**：进程内闸门 `Service.claim`（`recent` map，`gateLimit = 1<<16`、`gateRetention = 24h`，[service.go:415-445](backend/component/notification/service.go#L415-L445)）先挡掉绝大多数重复，省下"读 IAM policy + 展开组"的成本；数据库唯一索引是第二层，负责跨副本/重启。`dedupe_key` 为空时两层都不生效。写库失败会 `release`，让本窗口内可以重试。
- **载荷边界在构造函数/消息构造函数里**：`MaxFailureEntries = 100`、`MaxErrorBytes = 2<<10`（按 rune 截断），调用方传什么都不会撑大那条永久保留的行（[service.go:57-63](backend/component/notification/service.go#L57-L63)、[:172-219](backend/component/notification/service.go#L172-L219)）。
- **窗口常量**（[service.go:37-46](backend/component/notification/service.go#L37-L46)）：`BackgroundFailureWindow = 1h`、`UnmappedNamespaceWindow = 24h`；键见下表。

| 事件类 | 键 | 窗口 |
| --- | --- | --- |
| 后台同步失败（实例/库） | `schema-sync.instance:<instance>:<h>` / `schema-sync.database:<db>:<h>` | 1 小时 |
| OpenLineage 拒收 / 服务端失败 | `openlineage:<主体>:<kind>:<h>`，主体 = 事件的 namespace，解析不出来时 `key/<api key id>` | 1 小时 |
| OpenLineage namespace 未映射 | `openlineage.namespace-unmapped:<namespace>:<d>` | 24 小时 |

## 同步操作聚合

一个用户可见的同步操作 = 实例元数据步骤 + 它入队的所有库；进程内聚合，`operationTimeout = 2h`（[operation.go:21](backend/runner/schemasync/operation.go#L21)、[:31-66](backend/runner/schemasync/operation.go#L31-L66)）。

- 登记入口：`StartOperation` → `RecordInstanceResult` → `EnqueueDatabases`（登记 pending 并**封口**）→ checker 里 `completeDatabase` 把结果**扇出**给所有包含该库的 op（两个并发操作共享一个库是可能的）。
- checker 丢弃"实例已不存在"的队列项时也要回报，否则 op 永远等不到结果。
- **超时兜底**：op 带 deadline，checker 的 10s tick 顺带 finalize 超时 op，未出结果的库记 `UNFINISHED`。这是"永远不写消息 / 内存条目永不释放"的唯一保险。
- 库级后台失败在 `scheduleRetry` 的**放弃**分支通知管理员（"重试 N 次仍失败"）；实例级失败在 `trySyncAll` 里通知，用小时桶，因为 `last_sync_time` 不前进会每 15 分钟重试。后台**成功不通知**（没有人在等，只会变噪声）。
- 调用方改造：`CreateInstance`、`SyncInstance`、`BatchSyncInstances`（每实例一个 op）登记 op；**单库同步 `SyncDatabase` 不走聚合器**（请求内同步完成，直接投递）。
- 无库入队（没有新库、或实例阶段就失败）时 `FinishOperation` 立即写消息。

## OpenLineage 摄取异常 → 管理员

摄取是 API key 认证的机器流量，异常一律给管理员。所有拒绝分支收成一个 `reportIngestionFailure`（[openlineage_handler.go:93-143](backend/api/v1/openlineage_handler.go#L93-L143)），4xx 与 5xx 共用，severity 由 status 决定。

| 分支 | HTTP | kind | severity | 去重主体 |
| --- | --- | --- | --- | --- |
| 事件/批次解析失败、超单事件限制 | 400/413 | `INVALID_EVENT` | WARNING | `key/<id>`（拿不到 namespace） |
| 请求体 > 8MiB、批量 > 1000 条 | 413 | `LIMIT_EXCEEDED` | WARNING | `key/<id>` |
| `eventWithinScope` 不符 | 403 | `SCOPE_MISMATCH` | WARNING | 事件的 namespace |
| `UpsertOpenLineageRun(s)` / `ProcessRunEvent` 报错（含批量部分成功） | 500 | `PERSIST_FAILED` / `PROCESS_FAILED` | ERROR | 事件的 namespace |
| 数据集没匹配到实例、被存成 external dataset | — | `NAMESPACE_UNMAPPED` | WARNING | namespace（24h 桶） |

`NAMESPACE_UNMAPPED` 挂在 `Resolver.warnUnmatchedNamespace` 已有的"每 namespace 每进程一次"闸门上；resolver 通过可选的 `UnmatchedNamespaceReporter` 接口注入，`server.go` 那条给 maintenance/lineagevalidation 用的 processor **不注入**（reporter 为 nil 时行为与今天一致）。

**明确不通知**：**401**（匿名探测 + 配置错误混在一起，无可信主体，已在审计里）、**429**（producer 超预算，HTTP 响应已说清）、"事件合法但提不出血缘"的 Warn（数据质量，不是摄取失败）。展示用 `MaskedKey` 掩码，去重键用不敏感的 `key/<id>`。

## 对外 API

`NotificationService` 共 7 个方法（[notification_service.proto](proto/v1/v1/notification_service.proto)）。**不加 `(metaxisdata.v1.permission)` 注解，也不加 `allow_without_credential`，不加 `(metaxisdata.v1.audit)`**：登录必需，作用域由 handler 按当前用户保证，读收件箱不是管理动作、不该进永久审计账本（[notification_service.proto:14-23](proto/v1/v1/notification_service.proto#L14-L23)、[notification_service.go:50-59](backend/api/v1/notification_service.go#L50-L59)）。

| RPC | HTTP | 说明 |
| --- | --- | --- |
| `ListNotifications` | `GET /v1/{parent=workspaces/*}/notifications` | `unread_only` 过滤；`parseLimitAndOffset`/`paginate`；支持 `workspaces/-` |
| `GetUnreadNotificationCount` | `GET .../notifications:unreadCount` | 一次返回 `unread_count` 与 `unseen_count` 两个数（名字只说了前一个，保留是为了兼容老前端）；都走 partial index；流的兜底 |
| `MarkNotificationsSeen` | `POST .../notifications:markSeen` | 幂等：把水位推到 `NOW()`，不写 `read_at`；红点清零、未读不变 |
| `BatchMarkNotificationsRead` | `POST .../notifications:batchMarkRead` | 幂等（只置 `read_at IS NULL`）；names 非空且 ≤ `maxNotificationsPerBatch = 1000`，校验在 workspace 解析**之前** |
| `MarkAllNotificationsRead` | `POST .../notifications:markAllRead` | 全部已读 |
| `DeleteNotification` | `DELETE /v1/{name=workspaces/*/notifications/*}` | 删自己的；删不到（不存在或别人的）返回 `NotFound`，不区分 |
| `SubscribeNotifications` | —（无 `google.api.http`） | server streaming，见下节 |

不加 CEL `filter`：`unread_only` 一个布尔覆盖实际需求（全部/未读两个态）。

**没把两个数合成一个 RPC**：`GetUnreadNotificationCount` 已经是红点的兜底调用，多一个字段就够；改成一个新名字会让"前端比服务端新"的窗口里老前端直接 404。代价是方法名只描述了其中一个数。

**流无 REST 路由**：grpc-gateway 不能代理 server streaming（`ExplainSQL` 的生成网关直接返回 `Unimplemented`），浏览器直接打 Connect 端点。注册处：`connectHandlers` map + `grpcreflect` 静态列表按服务名注册（[grpc_routes.go:99](backend/server/grpc_routes.go#L99)、[:162](backend/server/grpc_routes.go#L162)、[:192](backend/server/grpc_routes.go#L192)）。

## 推送流：hub 与 handler

`backend/component/notification/hub.go` 是进程内 fan-out，按 `recipientID` 分订阅者集合：

| 机制 | 行为 | 理由 |
| --- | --- | --- |
| `publish` | `select { case ch <- event: default: 关掉这个订阅者 }` | **永不阻塞写入方**（常是 runner goroutine）；缓冲满 = 连接不健康，关流让客户端重连刷新 |
| `subscriptionBuffer` | 16 | 通知低频，这是安全阀不是工作队列 |
| `MaxSubscriptionsPerRecipient` | 8，超过时拒绝**新**连接（`ResourceExhausted`） | 一个标签页一条 + 重连短暂重叠；拒绝新的而不是踢掉旧的，否则另一个标签页静默变哑 |
| `close()` | 结束所有订阅并拒绝新订阅；此后 `subscribe` 立刻返回已关闭通道 | 停机期间新建的流立即正常结束，而非挂住 |
| 锁 | 所有对 `ch` 的 send/close 都在 hub 锁内 | 不存在"关掉之后又 send"的窗口 |

`SubscribeNotifications` handler（[notification_service.go:244-302](backend/api/v1/notification_service.go#L244-L302)）：先 `notificationCaller`（未登录 `Unauthenticated`）→ `subscriptions == nil` 时 `Unimplemented`（在任何 store 读取之前，因此可用 nil store 单测）→ 解析一次 workspace（流的每条消息共用）→ `Subscribe` → 设 `X-Accel-Buffering: no`（必须在第一次 `Send` 前，connect-go 随首帧发头）→ `heartbeat = 25s` / `lifetime = 30min` 循环。超时收尾与客户端断开都 `return nil`（**不是错误**）。

**停机顺序**：`server.Shutdown` 在 `echoServer.Shutdown(ctx)` **之前** `s.notifier.Close()`（[server.go:225-226](backend/server/server.go#L225-L226)）。常驻流是永不结束的 in-flight 请求，Echo 的 Shutdown 会等它到 `gracefulShutdownPeriod`（10s）。对 nil 的守卫是必须的（`server_lifecycle_test.go` 手工构造的 `Server{}` 没有 notifier）。

## 前端

| 文件 | 职责 |
| --- | --- |
| `frontend/src/api/notification.ts` | 7 个 RPC 的调用、`listAll` 分页与 `subscribeNotifications(signal)` 薄封装；`getNotificationCounts()` 一次拿两个数 |
| `frontend/src/store/modules/notification.ts` | Pinia：`unreadCount`、`unseenCount`、`recent`、`arrivalSeq`、`openInboxCount`、`openInbox()`/`closeInbox()`、`startStreaming()`/`stopStreaming()` |
| `frontend/src/lib/notificationText.ts` | **纯函数**：type + detail → `{titleKey, titleParams, messageKey, messageParams, href}`（有覆盖率门槛） |
| `frontend/src/components/layout/NotificationBell.vue` | 侧栏底部（`UserMenu` 之上）：红点计数（99+）、下拉最近 `RECENT_NOTIFICATION_LIMIT = 8` 条、"查看全部" |
| `frontend/src/pages/NotificationsPage.vue` | `/notifications`：全部/未读、单条已读、全部已读、删除、分页、跳转源对象 |

行为要点：

- **流的生命周期归铃铛**：`onMounted → startStreaming()`、`onBeforeUnmount → stopStreaming()`（[NotificationBell.vue:165-175](frontend/src/components/layout/NotificationBell.vue#L165-L175)）。侧栏只在登录后的壳里存在，它的生命周期就是会话的生命周期。
- **红点由 `unseenCount` 驱动，页面的两处由 `unreadCount` 驱动**：`store.hasUnseen` 画铃铛的红点与数字，`store.hasUnread` 决定"全部已读"按钮可不可点。把其中一个换到另一个上，要么红点清不掉，要么按钮在还有未读时是灰的。
- **`openInbox()` / `closeInbox()` 是"通知正开着"的登记**：`openInboxCount` 是计数而不是布尔量（铃铛下拉与页面可能同时挂着），为 0 时新消息才点亮红点。打开时乐观清零 `unseenCount` 再调 `MarkNotificationsSeen`（失败只吞掉：下次刷新红点会回来，比开一次通知抛错好）。下拉/页面卸载时必须 `closeInbox()`，否则残留的计数会让红点永远不再亮。
- **打开期间到达的消息要真的出现在列表里**：`applyArrival` 往 `recent` 前插的条件是"列表已有行 **或** 通知正开着"（[notification.ts:216-258](frontend/src/store/modules/notification.ts#L216-L258)）。水位要盖过这条消息，前提是用户看得到它；空下拉也是"开着"，此时不插入就等于把一个从没展示过的消息永久静音（红点不会再亮，因为水位已经过去了）。
- **页面打开也算打开通知**：`NotificationsPage.vue` 的 `onMounted` 调 `openInbox()`，`onBeforeUnmount` 调 `closeInbox()`；深链或刷新直接进页面时红点同样是清的。
- **`generation` 把守连接循环**（[notification.ts:70](frontend/src/store/modules/notification.ts#L70)）：`startStreaming`/`stopStreaming` 都推进 `generation`，每条循环只在自己那一代里重连。卸载与随后挂载可能落在同一 tick（布局切换），只用布尔量会让被停止的循环把新的开始误认成自己的，于是每次切换多留一条常驻流，直到 8 条上限把新连接全拒掉。
- **每次（重）连都 `refresh()` 一次**：这是"丢事件/缓冲溢出被丢订阅/进程重启/标签页被冻结"的统一兜底。任何一帧（含 `KeepAlive`）都重置退避到 `RECONNECT_MIN_MS = 1s`，上限 `RECONNECT_MAX_MS = 30s`。`refreshCounts()` 落地时若 `openInboxCount > 0`，把 `unseenCount` 压回 0，**并且服务端还认为有"新的"时补一次 `MarkNotificationsSeen`**：流漏掉的那条消息正是靠这次刷新发现的（重连/重新可见会同时刷列表与计数），只压数不推水位会让用户关掉下拉后红点又亮——为一条他刚在屏幕上读过的消息（[notification.ts:116-138](frontend/src/store/modules/notification.ts#L116-L138)）。
- **两张票防止旧响应覆盖新值**（`countTicket` / `recentTicket`）：推送到达会推进票号，已过期的 fetch 让位；两个计数共用一张 `countTicket`，因为它们来自同一次服务端计算。
- **`arrivalSeq`**：每收到一条推送 +1；页面 `watch` 它，只在 `!hasPrevious`（停在第 1 页）时重载首页，不把第 2 页的读者弹回去。
- **`Unimplemented` 是唯一不重试的错误**（前端比服务端新）：安静停掉；其余交给退避重连，重连的握手会再走一遍 `sessionInterceptor` 的会话刷新。
- **可见性监听保留**，动作从"拉一次计数"升级为 `refresh()`（计数 + 已加载过的列表）。
- **深链**（[notificationText.ts](frontend/src/lib/notificationText.ts)）：schema 同步 → `/instances/{instanceId}`；OpenLineage 拒收/失败 → `/openlineage/events`；`NAMESPACE_UNMAPPED` → `/settings/openlineage`（namespace mapping 页）。
- **文案渲染**：组件用返回的 `titleKey`/`messageKey` 调 `t()`；未知 type / 未知 detail 走 fallback 文案。`notifications.*` 同时进 `en-US.json` 与 `zh-CN.json`（各 46 个键）。`eslint.config.mjs` 的 `no-unused-keys` 对这一族加了显式 ignores（规则追不到 `notificationText.ts` 返回值里的键）；若改用别的属性名，`scripts/check-vue-i18n.mjs` 的 `KEY_PROP_RE` 也需要相应处理。

## Invariants

| 规则 | 破坏后果 |
| --- | --- |
| 每个读/写查询都必须带 `recipient_id` 作用域 | B 能看到/标已读/删除 A 的消息；`backend/store/notification_test.go:35` 的作用域守卫测试会变红 |
| 读态必须是 `read_at` 列 + `idx_notification_recipient_unread` partial index | 未读计数无法走索引，角标热路径退化为全表 `payload->>` 扫描 |
| 打开通知只能推 `principal.notification_seen_at`，**绝不能**顺手写 `read_at` | 用户没点过的消息被标已读，未读筛选与"全部已读"从此说不清；这是本特性唯一的行为红线 |
| 水位必须由服务端 `NOW()` 打戳，客户端只发"我打开了" | 客户端可控时间就能把某条消息永久排除在"新消息"之外 |
| 两个计数必须由**同一条语句**返回 | 分两次读会拿到两个快照，红点能出现比未读数还大（"5 条新的"而收件箱 0 条未读） |
| 红点只能由 `unseen_count`（`unreadCount` 与它分开）驱动 | 用 `unread_count` 画红点 = 回到"必须点开每条才能清"；用 `unseen_count` 画页面按钮 = 还有未读时按钮变灰 |
| `openInbox()` 与 `closeInbox()` 必须成对，卸载也要 `closeInbox()` | 残留的 `openInboxCount` 让红点永远不再亮，且只有刷新页面才恢复 |
| 去重必须靠 `idx_notification_dedupe` 唯一索引 + `ON CONFLICT DO NOTHING` | 只有进程内闸门时，多副本/重启后重复通知会穿过去；单副本下两层等价，调窗口必须一起调 |
| 写通知必须保持 best-effort，失败只记日志 | 同步/摄取路径会因一条消息写不进而失败 |
| `publish` 必须非阻塞、缓冲满即丢订阅 | 慢客户端会拖住 runner goroutine（写入方），同步被客户端拖死 |
| 订阅必须按当前鉴权用户注册，且方法不得加 permission 注解 | 加 permission 会让自定义角色看不到自己的消息；按别的作用域订阅会串收件箱 |
| `SubscribeNotifications` 不得加 `(metaxisdata.v1.audit)` | 每条常驻连接往永久账本写行，是纯噪声 |
| `X-Accel-Buffering: no` 必须在第一次 `Send` 前设置 | 反代会把整个流攒进 buffer，铃铛永不更新（集成用例已钉住） |
| `Shutdown` 必须先 `notifier.Close()` 再关 Echo | 每次停机白等满 `gracefulShutdownPeriod`（10s）并打一条 error |
| 前端必须用 `generation`（不能只用布尔量）把守连接循环 | 布局切换时残留多条常驻流，直到 8 条上限拒绝所有新连接 |
| 每次（重）连必须 `refresh()` 一次 | 丢事件/丢订阅/重启/冻结都没有补偿路径，收件箱与角标长期不一致 |
| 一库一次操作的结果 = 第一次结果 | 退避重试改写消息会产生两套噪声；这是已接受的取舍，不是 bug |
| `notification` 行永久保留，不接 maintenance TTL | 与 `audit_log` 同取向；清理会丢掉"系统告诉过用户什么"的账本 |

## Failure modes

| 场景 | 行为 |
| --- | --- |
| 计数查询失败 | 记专属日志行；事件仍投递，`Event.CountsKnown = false`，前端自己补问一次 |
| `CreateNotification` 被唯一索引挡下 | 返回 `(nil, nil)`，`create` 不发布——收件箱里那条已在，不必再提醒 |
| 写库失败 | `release` 抑制键并返回错误；调用方（同步/摄取）忽略它继续 |
| 进程内闸门"认领后写入失败"的窄窗口 | 窗口内那次真实失败没留消息；保留行为（否则一次抖动静默整个窗口），下一窗口自愈 |
| 订阅者缓冲满 / 客户端不再读 | hub 关掉该订阅，客户端退避重连 + 刷新；消息仍在库里 |
| 每收件人超过 8 条订阅 | 新连接收到 `ResourceExhausted`，旧连接不受影响 |
| 服务端到 30 分钟上界 | 流正常结束（非错误），客户端立刻重连并重新鉴权 |
| 服务端停机 | `Close()` 结束所有流，客户端看到干净结束，在 2s 内返回（回归用例钉住） |
| 服务端返回 `Unimplemented`（前端更新） | store 自行 `stopStreaming()`，不重试 |
| 会话在流上已过期/被吊销 | 握手期在第一次 `Receive` 被拒，`Unauthenticated`；一元调用同样被拒；前端走会话刷新或登出 |
| **server-streaming 请求遇到 401 重放** | `sessionInterceptor` 的重放对 server streaming 无效：connect-web 把请求体构造成一次性 async iterable，重放抛 `missing request message`（`Code.Unknown`）。**`frontend/src/api/session.ts` 至今没有为 streaming 缓存输入消息**（[session.ts:73-101](frontend/src/api/session.ts#L73-L101) 无条件 `await next(request)`）。续期已发生，流由连接循环的下一次重连接上；调用方看到一次 `Unknown` 错误。**`ExplainSQL`（同样 server streaming）受影响相同**，是既有缺陷、非本特性引入 |
| 多副本 | 用户在副本 A、消息由副本 B 写出 → 推不到，靠重连/重新可见那次刷新兜；`LISTEN/NOTIFY` 不做 |
| 另一个标签页改已读 | 本标签页角标要等刷新/重连/重新可见才对齐 |
| 另一个标签页打开通知 | 同样要等那次刷新：水位在服务端已经推进，红点下一次 `refreshCounts()` 就灭 |
| `MarkNotificationsSeen` 写库失败 | 前端已经本地清零（乐观），下次刷新红点会回来；不弹错、不阻塞打开，再打开一次就修好 |
| 前端比服务端新（旧服务端没有 `markSeen` / `unseen_count`） | `markSeen` 报错被吞掉，`unseen_count` 读作 0 → 红点不亮。两者同镜像发布，升级服务端即恢复 |
| **应用主机与数据库主机时钟不一致** | 水位由 DB `NOW()` 打戳，`created_at` 由应用进程打戳（[notification.go:64](backend/store/notification.go#L64)），比较跨了两个时钟域：应用时钟偏慢时，打开通知后极短时间内写入的消息会被判为"已看过"而不再点亮红点。同机/同集群 + NTP 下不可见，已作为假设写进代码注释 |

## Open items

1. **修 `frontend/src/api/session.ts` 的 streaming 重放**：为 streaming 请求把唯一那条输入消息缓存下来，重放时交给 transport 一个新的 iterable。它动的是共享会话路径、影响所有流式方法（含 `ExplainSQL`），值得单独一次评审——本特性只记录，未修。
2. 跨副本投递（PostgreSQL `LISTEN/NOTIFY`）：一旦真出现多副本再说；需要一条常驻连接、断线重连与去重语义。
3. 推送已读状态变化，让多标签页角标严格一致：需要先定"谁改的、改了哪些行、多标签页回声"。**水位的推进（`markSeen`）同理**，今天只靠刷新/重新可见对齐。
4. 桌面通知 / toast：有了这条流，`notify` 是 `applyArrival` 里多一个分支。
5. 心跳期复查 token 撤销，把 30 分钟上界收紧到心跳间隔（要把 `stateCfg` 的撤销缓存接进 API 层）。
6. `processor.go` 里"事件合法但提不出血缘"的 Warn 是否也要通知（数据质量问题，今天只记日志）。
7. 管理员给指定用户手动发消息：需要新增自由文本 detail 分支 + 一条发送 RPC，并重新考虑它与审计的关系。
8. 多副本/频繁重启下的操作聚合器加固：op 开始时落一条 `pending = TRUE` 隐藏行，完成时 UPDATE，启动时收尾陈旧行。代价是一个必须被"列表/计数/删除"处处尊重的隐藏态和一个清扫任务。
9. 后台抑制窗口整体调大：进程内闸门与数据库桶必须一起调。
10. **把水位的打戳与应用时钟统一**：今天水位吃 DB `NOW()`、`created_at` 吃应用时钟（见 Failure modes 末行）。要么 `MarkNotificationsSeen` 改成接收应用侧时间戳，要么 `CreateNotification` 改成库端 `DEFAULT NOW()`，二者取一，多副本部署前值得定下来。
11. **登出没有重置通知 store**（既有缺陷）：`frontend/src/store/modules/auth.ts` 的就地登出不 reload，`unreadCount` / `unseenCount` / `recent` 会留到下一个账号登录、被铃铛重新挂载时刷掉为止——另一个用户能短暂看到上一个人的消息列表。与本特性无关（`recent` 一直如此），但红点现在也在这条路径上。

## Where things live

代码：

- 迁移：[backend/migrator/migration/0.1/0017##notification.sql](backend/migrator/migration/0.1/0017%23%23notification.sql)、[0018##notification_seen.sql](backend/migrator/migration/0.1/0018%23%23notification_seen.sql)（`principal.notification_seen_at`）、[LATEST.sql:678-705](backend/migrator/migration/LATEST.sql#L678-L705)。
- 组件（写入口 + 推送 hub）：[backend/component/notification/service.go](backend/component/notification/service.go)、[hub.go](backend/component/notification/hub.go)。
- store：[backend/store/notification.go](backend/store/notification.go)。
- API handler 与转换：[backend/api/v1/notification_service.go](backend/api/v1/notification_service.go)。
- 同步聚合与后台失败通知：[backend/runner/schemasync/operation.go](backend/runner/schemasync/operation.go)、[syncer.go](backend/runner/schemasync/syncer.go)。
- 摄取失败通知：[backend/api/v1/openlineage_handler.go:93-143](backend/api/v1/openlineage_handler.go#L93-L143)、[backend/plugin/openlineage/resolver.go](backend/plugin/openlineage/resolver.go)。
- 接线与停机：[backend/server/grpc_routes.go:74-192](backend/server/grpc_routes.go#L74-L192)、[backend/server/server.go:128](backend/server/server.go#L128)、[:225-226](backend/server/server.go#L225-L226)。
- proto：[proto/v1/v1/notification_service.proto](proto/v1/v1/notification_service.proto)、[proto/store/store/notification.proto](proto/store/store/notification.proto)；生成物 `backend/generated-go/`、`frontend/src/types/proto-es/`、`proto/gen/grpc-doc/` 一起提交。
- 前端：[api/notification.ts](frontend/src/api/notification.ts)、[store/modules/notification.ts](frontend/src/store/modules/notification.ts)、[lib/notificationText.ts](frontend/src/lib/notificationText.ts)、[components/layout/NotificationBell.vue](frontend/src/components/layout/NotificationBell.vue)、[pages/NotificationsPage.vue](frontend/src/pages/NotificationsPage.vue)、[router/index.ts:270](frontend/src/router/index.ts#L270)、[api/session.ts](frontend/src/api/session.ts)（遗留缺陷）。
- 启用点：`NotificationBell` 挂在 `AppSidebar` 底部（[AppSidebar.vue:174](frontend/src/components/layout/AppSidebar.vue#L174)）；`/notifications` 不在 `menuItems`，经铃铛下拉"查看全部"进入。服务端无开关——通知随服务启动。

测试与门禁：

- store 作用域守卫 + 扫描：[backend/store/notification_test.go:35](backend/store/notification_test.go#L35) → `go test ./backend/store/...`。
- 组件（hub 扇出/上限/溢出/Close、create 路径、两个计数一次读、读不到就只发消息）：[backend/component/notification/hub_test.go](backend/component/notification/hub_test.go)、[service_test.go](backend/component/notification/service_test.go) → `go test ./backend/component/notification/...`。
- API（转换、鉴权、nil 通道、批次校验、打开通知必须有凭据且不碰 store）：[backend/api/v1/notification_service_test.go](backend/api/v1/notification_service_test.go) → `go test ./backend/api/v1/...`。
- ACL 守卫（无注解方法必须登记）：[backend/api/v1/acl_interceptor_test.go:145](backend/api/v1/acl_interceptor_test.go#L145)。
- 生命周期回归：[backend/server/server_lifecycle_test.go](backend/server/server_lifecycle_test.go)。
- 集成（真 server + PG + MySQL）：[backend/test/integration/runner/notification_service_test.go](backend/test/integration/runner/notification_service_test.go)（7 条，含收件人隔离、流推送、过期/吊销会话、摄取拒收去重、"打开通知只清红点不改读态"）→ `make test-integration-smoke && make test-integration`。迁移链一致性由 `TestMigrateSchemaLATESTMatchesTheIncrementChain` 钉住（`-tags integration ./backend/migrator/`）。
- 前端 store/api/lib：[store/modules/notification.test.ts](frontend/src/store/modules/notification.test.ts)、[api/notification.test.ts](frontend/src/api/notification.test.ts)、[lib/notificationText.test.ts](frontend/src/lib/notificationText.test.ts) → `pnpm --dir frontend test run`（`src/lib/`、`src/utils/` 有 95% 行 / 85% 分支门槛；红点相关行为：打开即清、开着时刷新补一次水位、空下拉里到达的消息必须进列表）。
- 前端组件交互：[components/layout/NotificationBell.test.ts](frontend/src/components/layout/NotificationBell.test.ts)（"点开铃铛 = 清红点且不标已读"、"点某条消息才标已读"、"开着下拉时卸载要归还 `openInboxCount`"）、[pages/NotificationsPage.test.ts](frontend/src/pages/NotificationsPage.test.ts)（"进页面清红点、离开后新消息又能点亮"）。
- proto 门禁：`buf format -w proto && buf lint proto && (cd proto && buf generate)`。
