# Plan: 通知的服务端推送（订阅流）

> **Status: 已实现并过了对抗性 review**（Phase 1–5，分支 `feat/notification-push`，rebase 到 `main` 的 `feat(lineage): highlight a field's whole flow` 之上）。门禁全绿：`buf format/lint/generate`（幂等）、`gofmt`、`golangci-lint run --allow-parallel-runners`（0 issues）、`go test ./...`、`go build`、`make test-integration-smoke`，以及通知这一族在真 server + PostgreSQL + MySQL 上的集成用例（5 条，含 `-race`）；前端 `biome:check` / `lint` / `i18n` / `type-check` / `test run` 全绿。
>
> review 的每条发现都做过负向验证（去掉修复后对应用例必须变红）：停机顺序、计数投递顺序、计数/列表的旧响应、退避、旧代失败、页面挂载加载，逐条见下面的"对抗性 review"。
>
> 实现与设计不一致或设计未覆盖的地方记在下面的"实现状态"。

## 实现状态

| 阶段 | 关键文件 |
| --- | --- |
| Phase 1 proto | `proto/v1/v1/notification_service.proto`（+ 生成物：`backend/generated-go/v1/`、`frontend/src/types/proto-es/`、`proto/gen/grpc-doc/`） |
| Phase 2 hub 与发布点 | `backend/component/notification/hub.go`、`hub_test.go`、`service.go`（`AdminStore` 加 `CountUnreadNotifications`；`create` 交出行并发布；`Subscribe` / `Close`） |
| Phase 3 流式 RPC 与停机 | `backend/api/v1/notification_service.go`、`backend/server/{grpc_routes,server}.go`、`backend/server/server_lifecycle_test.go` |
| Phase 4 前端 | `frontend/src/{api/notification.ts,store/modules/notification.ts,components/layout/NotificationBell.vue,pages/NotificationsPage.vue}` |
| Phase 5 文档 | 本文、`plan/system_notification_plan.md`、`docs/security-posture.md` |

实现时与设计不同的地方：

1. **ACL 守卫测试要求显式登记**：`backend/api/v1/acl_interceptor_test.go` 的 `TestEveryMethodIsPermissionGated` 会让任何没有 permission 注解的方法变红，所以 `SubscribeNotifications` 必须进 `unannotatedMethods` 白名单（设计里没写到这一步，"与其余 5 个方法一致"是靠这条守卫落地的）。
2. **集成用例不能"取流上第一条消息"**：fixture（`setupMySQLServiceDatabase` → `EnsureDatabaseVisible` → `SyncInstance`）自己那次实例级同步有异步尾巴，它的消息可能先到，而且它只列失败的库。用例改成按 `databases[0].database == <本用例的库>` 认领自己的那条，否则读到的是 fixture 的消息（实现时正是这样红过一次）。
3. **反代开关用集成断言钉住**：`X-Accel-Buffering: no` 在客户端侧靠 `stream.ResponseHeader()`（connect-go 该访问器在首次 `Receive` 返回后才可用，所以断言放在收到事件之后）验证，避免以后有人重构 handler 时把它默默删掉。
4. **停机顺序有专门的回归测试**：设计只写了"`Shutdown` 前先 `Close()`"，实现补了 `TestShutdownEndsAnOpenNotificationStream`——它挂一条真的常驻流，断言 `Shutdown` 在 2s 内返回、客户端看到的是干净结束而不是被掐断。做了负向验证：注释掉 `s.notifier.Close()` 后它确实以 10.0009s（整个 `gracefulShutdownPeriod`）失败。
5. **连接循环放在 store 模块层**：`running` / `generation` / `controller` 与循环都在 `defineStore` 之外；`startStreaming()` 幂等（`running` 为 true 时直接返回），`stopStreaming()` 无条件清理（中止流、打断退避等待、摘掉可见性监听），因为服务端返回 `Unimplemented` 时循环会自行收尾（`stopStreaming()` 把 `running` 置回 false）。
6. **连接循环用 generation 而不是布尔量把守**：`startStreaming()` / `stopStreaming()` 都在改 `generation`，每条循环只在"自己那一代"里重连。原因是卸载与随后挂载可能落在同一个 tick（布局切换就是如此），只用一个"是否停止"的布尔量时，被停止的那条循环会把新的开始误认成自己的，于是每次切换多留一条常驻流，直到服务端每账号 8 条的上限把新连接全部拒掉。store 测试里专门有一条：停掉再立刻启动后，5 分钟内必须只有 2 条流（负向验证：去掉 generation 判断后它变成 3 条）。
7. **测试卫生**：`AppSidebar.test.ts` 会挂载铃铛，所以它加了 `afterEach(() => useNotificationStore().stopStreaming())`——否则每条用例都会留下一个后台重连循环（这一点在改动前同样存在，只是那时是 30s 的轮询）。

---

## 对抗性 review：发现与处理

一次独立上下文、只读的对抗性 review（跑测试、跑探针，不改仓库）跑完后逐条核实并处理如下。**没有 blocking 级问题。**

| # | 级别 | 发现 | 处理 |
| --- | --- | --- | --- |
| 1 | should-fix | 连接时的 `refresh()` 是即发即弃的：一个**旧**的未读数可能在一条更新的流事件之后落地，把角标退回旧值（reviewer 用探针复现：事件带 1、随后的计数返回 0 → 最终 0） | 两个被服务端同时推送的值各加一张票（`countTicket` / `recentTicket`）：只有最新的写者能写。推送事件会推进票号，于是已过期的请求自然让位（`refreshUnreadCount` / `refreshRecent`），与 `usePagedFetch` 的既有做法一致 |
| 2 | should-fix | 退避只在**真消息**上重置：空闲收件箱只有 keepalive，于是每次服务端 30 分钟收尾后都要等满 30s；这与服务端"到点收尾不是错误"的注释自相矛盾 | 任何一帧（含 keepalive）都重置退避；25s 心跳因此把健康连接的等待钉在下限 |
| 3 | should-fix | 未读计数在 hub 锁**之外**读，两个几乎同时的写入可能把计数按相反顺序投递（reviewer 用 overlay 探针复现 `2 then 1`），客户端照单全收 | `Service.publish` 用一个进程级 `publishMu` 把"读计数 + 交给订阅者"串起来：投递顺序与快照顺序一致。代价与边界写在代码注释里（临界区只有一次走 partial index 的计数 + 非阻塞扇出） |
| 4 | should-fix | 隔离用例的反向断言是**零等待** `default:`：即使成员真收到了管理员的消息也可能通过（reviewer 把第二个流故意换成管理员 token，用例照样 PASS） | 改成 2s 有界等待；同时让 helper 在**流结束**时关闭通道，于是"安静"与"已死"可区分。这一改立刻暴露出另一个既有问题：用例给流式客户端设了 `http.Client{Timeout: 10s}`，**整个请求**的超时会在用例中途掐断流——旧断言正是因此空过的。两条流式用例改用无总超时的客户端（单发调用仍保留 10s），并用 reviewer 的实验做负向验证：把第二个流换成管理员 token 后，用例确实变红 |
| 5 | should-fix | 两个声称存在的测试其实没有：`fakeSubscriptions.recipientIDs` 只写不读；`subscriptions == nil → Unimplemented` 没有用例 | 把 nil 检查挪到 auth 之后（在任何 store 读取之前，因此可用 nil store 单测），补上该用例；删掉那个没人读的字段，并把"作用域"归口到端到端集成用例（在那里它才真正可观测）；前端补 `applyArrival` 的重复 / 截断 / 空消息用例 |
| 6 | nit | "subscribed before the sync" 只保证 client-send 顺序，connect-go 的 `CallServerStream` 在服务端注册之前就返回；窗口内写出的消息会漏 | 保留（一次同步远慢于握手），记在此处 |
| 7 | nit | `notificationEvents` 的 goroutine 在缓冲写满时会泄漏（`Close()` 解不开一个阻塞的 send） | helper 自建可取消 ctx，返回的 close 同时 cancel；send 上 select `ctx.Done()` |
| 8 | nit | `GetUnreadNotificationCount` 的注释还写着"铃铛轮询专用" | 改成"流的兜底" |
| 9 | nit | 计数查询失败走 `LogFailure`，日志说"Failed to write a notification"，而写入其实成功了 | 换成计数专属的日志行 |
| 10 | nit | `Unimplemented` 分支不看 `live()`：一个迟到于重启的失败会顺手关掉新一代刚开的流 | 加 `live()` 守卫，并补一条用例（旧代的失败与中止同时到达） |
| 11 | nit | 安全态势里写成"白名单条目放行了这个方法和另外五个"——生产里没有白名单，是 ACL 拦截器对无注解方法不放门 | 改成"ACL 拦截器不放门，白名单只是守卫测试在记录这个决定" |
| 12 | nit（既有） | `/notifications` **从来没有在挂载时加载第一页**：`usePagedFetch` 只被显式调用才拉，页面没有 `onMounted`，所以打开页面看到的是空态，直到手动刷新——这正是用户最初抱怨的一部分 | 补 `onMounted(() => void resetPages())`（与 `AuditLogsPage` 同一做法），并新增页面级用例：挂载即加载、到达新消息重载首页、停在第 2 页的读者不被打断 |
| 13 | nit | 设计文档把循环守卫写成布尔量 `stopped`，代码用的是 `running` + `generation` | 文档改成与代码一致 |

reviewer 尝试证伪但没打破的（摘要）：hub 在 `-race` + overlay 并发压力下无 panic/无死锁/无丢消息；停机守卫不是同义反复（去掉 `Close()` 后它以 10.0003s 失败）；`X-Accel-Buffering` 确实随第一帧发出；8 条上限按调用者计且 `ResourceExhausted`；生成本就是新鲜的（`git archive` 到 `/tmp` 重新 `buf generate` 后逐字节一致）；proto3 optional 的"0 与缺失"可区分；`grpcreflect` 按服务名注册，无需改动。

reviewer 明确**未能**验证、留给后续的：浏览器里 connect-web 流式端到端的真机验证（本次靠 Go 客户端 + 线格式 + 源码推理）、流上收到已过期 token 时的 401 行为、静默消失的客户端在下次心跳前占着订阅。

---

## TL;DR

消息的写入口只有一个（`notification.Service`），所以在**写入口挂一个进程内 hub**，再在 `NotificationService` 上新增一个 **server-streaming RPC `SubscribeNotifications`**：调用方一连接就订阅自己收件箱的实时事件，每条事件带**这条消息本身 + 该收件人的最新未读数**。前端去掉 30s 轮询，改为**一条常驻流 + 断线自动重连**，每次（重）连成功后刷新一次状态、标签页重新可见时刷新一次——这两次刷新就是全部兜底，不再有周期性请求。

选 server-streaming RPC 而不是字面 `text/event-stream`，是因为：仓库里已有先例（`ExplainSQL` 用 `connect-web` 的 server streaming，前端 `for await` 消费），走 ConnectRPC 自动获得 auth/throttle/error-mapping 拦截器、不需要新开一条 plain-HTTP 路由并自己补鉴权与限流，也保住了 `plan/frontend-architecture-review.md` 点名的资产（前端 100% 收敛在 ConnectRPC client，没有 ad-hoc `EventSource`/`WebSocket`）。

## 已确认的决定

| # | 决定 | 影响 |
| --- | --- | --- |
| 1 | 传输用 **ConnectRPC server-streaming RPC**，不用字面 SSE/`EventSource` | 改 proto + `buf generate`；复用 ExplainSQL 的流式先例；鉴权/限流免费 |
| 2 | 事件**带上消息本身 + 最新未读数** | 铃铛收到即可立刻更新，零额外往返；服务端每条消息多一次计数查询 |
| 3 | **单副本 / 进程内 hub** | 与"设备登录状态进程内""同步操作聚合器进程内"一致；跨副本投递不做（见第八节） |
| 4 | 覆盖**铃铛 + `/notifications` 页面首页自动刷新** | 页面只在自己停在第 1 页时刷新，不打断分页阅读 |
| 5 | **去掉 30s 轮询**，只保留"重连成功"与"标签页重新可见"时各刷新一次 | 不再有周期性请求；空窗由这两次刷新补齐 |

由本方案先定、可推翻的几条：

- **连接时长上界 30 分钟**：流只在连接时鉴权一次，所以由服务端主动收尾，逼迫客户端重连（= 重新鉴权），让 logout/撤销在一个上界内生效。到点收尾不是错误，客户端下一轮重连即可。
- **心跳消息 25s**：Connect protocol 没有 SSE 的注释帧，所以心跳是一条显式的空 `KeepAlive` 消息；它同时是写失败（对端已死）的探测点。25s 小于 nginx 默认 `proxy_read_timeout` 60s。
- **背压策略是"丢订阅、不丢消息"**：订阅者缓冲满就关掉它的流，客户端重连时刷新，消息本身早已入库。
- **已读状态的变化不推送**：另一个标签页把消息标为已读，本标签页的角标不会立刻变（要等刷新/重连/重新可见）。这是本方案的取舍，见第八节。

## 一、现状与缺口（代码事实）

1. **前端只有轮询，没有推送通道**：`useNotificationStore.startPolling()` 每 30s 拉一次未读数，且只在 `document.visibilityState === "visible"` 时拉（[notification.ts:16](../frontend/src/store/modules/notification.ts#L16)、[:94-125](../frontend/src/store/modules/notification.ts#L94-L125)）；铃铛的"最近列表"只在**点开下拉时**才拉一次（[NotificationBell.vue:147-149](../frontend/src/components/layout/NotificationBell.vue#L147-L149)）。所以一条后台跑出来的同步结果，最多要等 30s 角标才变，而下拉里的列表在打开那一刻是上一次拉取的结果——这正是"要刷新页面才看得到"的来源。
2. **流的生命周期归铃铛**：`onMounted(() => store.startPolling())` / `onBeforeUnmount(() => store.stopPolling())`（[NotificationBell.vue:154-155](../frontend/src/components/layout/NotificationBell.vue#L154-L155)）。侧边栏只在登录后的壳里存在，它的生命周期就是会话的生命周期——这个归属不需要改，只是把"轮询"换成"订阅流"。
3. **`connect-web` 已经支持浏览器里的 server streaming，且本项目在用**：`ExplainSQL` 是 server-streaming（[explain_sql_service.proto:11](../proto/v1/v1/explain_sql_service.proto#L11)），前端 `explainSQLClient.explainSQL(request)` 返回 async iterable，`for await` 逐块消费（[explain.ts:13-22](../frontend/src/api/explain.ts#L13-L22)、[ExplainSQLPage.vue:738-739](../frontend/src/pages/ExplainSQLPage.vue#L738-L739)）。`createConnectTransport` 的类型注释也写明它"makes unary and server-streaming methods available to web browsers"，并且本项目的 transport 没有设 `defaultTimeoutMs`（[client.ts:23-27](../frontend/src/api/client.ts#L23-L27)），所以长连接不会被客户端超时掐断。
4. **写入口是单点**：`Service.Send` / `SendToWorkspaceAdmins` 是全部消息的出口，内部都落到 `create()`（[service.go:205-330](../backend/component/notification/service.go#L205-L330)）；`create` 调用 `store.CreateNotification`，后者靠 `idx_notification_dedupe` + `ON CONFLICT DO NOTHING` 判重，**被抑制时返回 `(nil, nil)`**（[notification.go:36-84](../backend/store/notification.go#L36-L84)）。在 `create` 里发布事件，意味着所有调用方（syncer 操作聚合器、OpenLineage 摄取、resolver）**一行都不用改**就获得了推送能力。
5. **`SendToWorkspaceAdmins` 目前丢掉了写库返回的行**：它循环 `s.create(ctx, clone)` 但只接错误（[service.go:264-278](../backend/component/notification/service.go#L264-L278)）。推送需要那条行的 `id` 与 `create_time`，所以 `create` 要改成把写成的行交出来。
6. **未读计数有一条便宜的查询**：`CountUnreadNotifications` 走 `idx_notification_recipient_unread`（[notification.go:143-153](../backend/store/notification.go#L143-L153)），且 `notification.Service` 依赖的是窄接口 `AdminStore`（[service.go:69-75](../backend/component/notification/service.go#L69-L75)），加一个方法即可，测试仍可无库。
7. **流的鉴权语义是"连接时一次"**：Connect 拦截器链在**握手**时跑完 auth（[grpc_routes.go:126-142](../backend/server/grpc_routes.go#L126-L142)），流开始之后不再复查 token。前端 `sessionInterceptor` 只在 `await next(request)`（即握手）失败时刷新重试——`connect-web` 的实现是先 `await fetch` 再 `validateResponse(status)` 才构造响应体（`@connectrpc/connect-web/dist/cjs/connect-transport.js:181-182`），所以**握手期的 401 会被既有会话刷新机制接住，流中期的网络故障不会**。这两点决定了：客户端每次重连都会（重新）鉴权，服务端只需要给流一个时长上界。
8. **Echo 的 http.Server 没有读写超时**：`echo.New()` 用的是 `new(http.Server)`，`ReadTimeout`/`WriteTimeout`/`IdleTimeout` 全为 0（`echo@v4.13.4/echo.go` 的 `New()`），所以长连接不会被服务器自己掐断；但 `Shutdown` 会**等所有 in-flight 请求结束**，上限 `gracefulShutdownPeriod = 10s`（[server.go:35](../backend/server/server.go#L35)、[:210-241](../backend/server/server.go#L210-L241)）。常驻流必须被主动收尾，否则每次停机白等 10s 并打一条 error。
9. **grpc-gateway 不能代理 server streaming**：`ExplainSQL` 虽然带了 `google.api.http` 注解，生成的网关 handler 直接 `status.Error(codes.Unimplemented, "streaming calls are not yet supported in the in-process transport")`（[explain_sql_service.pb.gw.go:66-70](../backend/generated-go/v1/explain_sql_service.pb.gw.go#L66-L70)）。所以新方法**不加** `google.api.http` 注解：浏览器直接打 Connect 端点，多一条永远失败的 REST 路由没有意义。
10. **反代必须不缓冲**：nginx 默认 `proxy_buffering on`，会把响应体攒进 buffer 才发——对任何流式响应都是致命的。服务端加 `X-Accel-Buffering: no` 是 nginx 认识的那一个开关（比要求每个部署改配置可靠）。

## 二、总体设计

```
写入口（唯一）：schemasync 操作聚合器 / OpenLineage 摄取 / resolver
   │  Send / SendToWorkspaceAdmins
   ▼
notification.Service.create
   ├─ store.CreateNotification ──► 返回 nil 表示被 dedupe 抑制：不发布
   ├─ store.CountUnreadNotifications        （每条消息一次，不是每个连接一次）
   └─ hub.publish(recipientID, Event)       ← 进程内，按收件人分订阅者集合
              │
              ▼
NotificationService.SubscribeNotifications （server streaming）
   │  连接时解析一次 workspace，用于 name 转换；心跳 25s；连接上界 30min
   ▼
浏览器 notification store
   ├─ 未读数 = 事件里的权威值（缺了就先问一次）
   ├─ 最近列表前插（仅当已加载过）
   └─ arrivalSeq++ ──► /notifications 停在第 1 页时重载
   ▲
   └─ 断线 → 指数退避重连 → 重连成功先 refresh() 一次
```

一条不变式贯穿前后端：**每次（重）连成功都刷新一次状态**。于是"丢事件""缓冲溢出被丢订阅""进程重启""标签页被浏览器冻结"这些情况都退化成同一个动作——重连 + 刷新，不需要逐条补偿。

## 三、Proto

只改 `proto/v1/v1/notification_service.proto`（`proto/store` 不动：没有新表、没有新列、没有 JSONB 形状变化）。

新增方法（**不加** `google.api.http`，理由见第一节第 9 条）：

```proto
  // Stream the caller's notifications as they are written, until the
  // connection ends or the server ends it.
  //
  // No (google.api.http) annotation: this is a server-streaming method, and the
  // REST gateway cannot carry one — it answers "streaming calls are not yet
  // supported in the in-process transport" (see the generated ExplainSQL
  // gateway route). The browser reaches the Connect endpoint directly, the way
  // it does for ExplainSQL.
  rpc SubscribeNotifications(SubscribeNotificationsRequest) returns (stream SubscribeNotificationsResponse) {}
```

新增消息：

```proto
message SubscribeNotificationsRequest {
  // Format: workspaces/{workspace}. "workspaces/-" is the current workspace.
  string parent = 1 [(google.api.field_behavior) = REQUIRED];
}

message SubscribeNotificationsResponse {
  oneof event {
    // A message written while the caller was connected.
    NotificationEvent notification = 1;
    // An empty message sent while nothing happens, so a proxy does not end an
    // idle stream (nginx would after proxy_read_timeout). The client ignores it.
    KeepAlive keep_alive = 2;
  }
}

message NotificationEvent {
  Notification notification = 1 [(google.api.field_behavior) = OUTPUT_ONLY];
  // The recipient's unread count after this message was written. Absent when the
  // count could not be read: the client asks for it instead of showing a wrong
  // number. (proto3 optional, so 0 — "everything read" — stays distinguishable.)
  optional int32 unread_count = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
}

// KeepAlive is the notification stream's heartbeat: it carries no data.
message KeepAlive {}
```

服务级注释补一句：这个方法同样没有 `(metaxisdata.v1.permission)` 注解、同样登录必需，作用域由 handler 保证（订阅的是**调用者自己的**收件箱）；订阅类方法**不加 `(metaxisdata.v1.audit)`**——一条常驻连接不是一次管理动作，也不该往永久账本里写行。

生成物：`buf format -w proto` → `buf lint proto` → `cd proto && buf generate`，把 `backend/generated-go/`、`frontend/src/types/proto-es/`、`proto/gen/grpc-doc/` 一起提交。`grpcreflect` 的静态列表按服务名注册，新增方法不需要改。

## 四、后端：进程内 hub 与发布点

### 4.1 新文件 `backend/component/notification/hub.go`

```go
// Event is one message handed to the live subscriptions of an inbox: the row a
// write produced, and the recipient's unread count right after it.
type Event struct {
	Notification *storepb.Notification
	UnreadCount  int32
	// UnreadCountKnown is false when the count could not be read. The message
	// itself is still worth delivering; the badge is not.
	UnreadCountKnown bool
}

const (
	// subscriptionBuffer is how many messages one connection may fall behind by.
	// Notifications are low-frequency, so this is a safety valve rather than a
	// working queue.
	subscriptionBuffer = 16
	// MaxSubscriptionsPerRecipient bounds what one signed-in account may hold open.
	// One browser tab holds one, and a reconnect overlaps briefly with the old
	// stream. Past this the newest connection is refused rather than the oldest
	// one dropped: the caller is told, instead of another tab going quiet.
	MaxSubscriptionsPerRecipient = 8
)
```

`hub` 的内部形状：

```go
type hub struct {
	mu     sync.Mutex
	closed bool
	subs   map[int]map[*subscription]struct{} // principal id → 该收件人的订阅集合
}
type subscription struct{ ch chan Event }

func (h *hub) subscribe(recipientID int) (*subscription, error) // 超过上限返回错误
func (h *hub) unsubscribe(recipientID int, s *subscription)
func (h *hub) publish(recipientID int, event Event)             // 非阻塞
func (h *hub) close()                                           // 结束所有订阅
```

语义要点：

- **publish 永不阻塞调用方**：`select { case s.ch <- event: default: 关掉这个订阅者 }`。写通知的 goroutine 常常是 runner，绝不能被一个慢客户端拖住；缓冲满说明这个连接已经不健康，关掉它的流、让客户端重连刷新，消息本身还在库里。
- **close 之后 subscribe 立刻返回一个已关闭的通道**：停机期间新建的流会立即正常结束，而不是挂在那里。
- 所有对 `ch` 的 send/close 都在 hub 的锁内，不存在"关掉之后又 send"的窗口。

### 4.2 `Service` 的改动

- `AdminStore` 接口加一个方法（窄接口风格不变，测试仍不需要数据库）：

```go
	CountUnreadNotifications(ctx context.Context, recipientID int) (int, error)
```

- 公开两个方法给 API 层：

```go
// Subscribe registers a live subscription for one recipient's inbox. The returned
// channel is closed when the subscription ends — the server is shutting down, the
// connection could not keep up, or the caller unsubscribed. The returned function
// releases the subscription and is safe to call more than once.
func (s *Service) Subscribe(recipientID int) (<-chan Event, func(), error)

// Close ends every live subscription. It is what lets the server drain: Echo's
// Shutdown waits for in-flight requests, and a notification stream is a request
// that never ends on its own.
func (s *Service) Close()
```

- `create` 从"只报错"改成"报错 + 交出行"，并在**真的写进去**之后发布：

```go
func (s *Service) create(ctx context.Context, n *storepb.Notification) error {
	if strings.TrimSpace(n.GetParent()) == "" {
		workspaceID, err := s.store.GetWorkspaceID(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to resolve the workspace")
		}
		n.Parent = common.FormatWorkspace(workspaceID)
	}
	created, err := s.store.CreateNotification(ctx, n)
	if err != nil {
		return err
	}
	if created == nil {
		// 被 (recipient_id, dedupe_key) 唯一索引挡下的重复：没有新行，也就没有
		// 新消息要推送。收件箱里那条已经在，用户不需要再被提醒一次。
		return nil
	}
	s.publish(ctx, created)
	return nil
}

// publish hands one written message to its recipient's live subscriptions. The
// unread count is read here, once per message, so every connection of the same
// inbox shares one answer rather than each paying for its own query.
func (s *Service) publish(ctx context.Context, n *storepb.Notification) {
	recipientID := int(n.GetRecipientId())
	event := Event{Notification: n}
	if count, err := s.store.CountUnreadNotifications(ctx, recipientID); err != nil {
		// 计数读不到不是丢这条消息的理由：客户端收到没有 unread_count 的事件会
		// 自己去问一次。
		LogFailure(err, slog.Int("recipient_id", recipientID))
	} else {
		event.UnreadCount, event.UnreadCountKnown = int32(count), true
	}
	s.hub.publish(recipientID, event)
}
```

- `SendToWorkspaceAdmins` 不用改结构：它逐条 `create(ctx, clone)`，每条 clone 各自发布，所以每个管理员收到的都是自己那一行（各自的 id、各自已读态）。`Send` 同理。
- **调用方零改动**：`schemasync` 的操作聚合器、`ReportUnmatchedNamespace`、OpenLineage 摄取失败路径全都只调 `Send`/`SendToWorkspaceAdmins`，推送自动生效。

## 五、后端：流式 RPC 与接线

### 5.1 `backend/api/v1/notification_service.go`

依赖用窄接口声明（与 `OpenLineageNotifier` 同一做法，[openlineage_handler.go:43-50](../backend/api/v1/openlineage_handler.go#L43-L50)）：

```go
// NotificationSubscriptions is the push side of the notification component: what
// SubscribeNotifications listens to while a caller's browser is connected.
// *notification.Service is the production implementation; the interface keeps the
// handler from depending on how messages are fanned out, and lets a test drive it
// without a database.
type NotificationSubscriptions interface {
	Subscribe(recipientID int) (<-chan notification.Event, func(), error)
}

type NotificationService struct {
	v1connect.UnimplementedNotificationServiceHandler
	store         *store.Store
	subscriptions NotificationSubscriptions
}

func NewNotificationService(store *store.Store, subscriptions NotificationSubscriptions) *NotificationService
```

常量：

```go
const (
	// notificationHeartbeat is how often an idle stream is poked. It has to stay
	// under a proxy's read timeout (nginx defaults to 60s), and it is also how a
	// peer that is gone is noticed: the write fails and the handler returns.
	notificationHeartbeat = 25 * time.Second
	// notificationStreamLifetime bounds one connection. A stream is authenticated
	// once, when it is opened, so it ends itself well before a session decision
	// could go stale: the client reconnects and therefore re-authenticates, which
	// is what makes a logout or a token revocation take effect.
	notificationStreamLifetime = 30 * time.Minute
)
```

handler：

```go
func (s *NotificationService) SubscribeNotifications(ctx context.Context, req *connect.Request[v1pb.SubscribeNotificationsRequest], stream *connect.ServerStream[v1pb.SubscribeNotificationsResponse]) error {
	user, err := notificationCaller(ctx)
	if err != nil {
		return err
	}
	// Resolved once per connection: every message this stream carries is named
	// under the same workspace, and the name is the only field the write path
	// does not have.
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return err
	}
	if s.subscriptions == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("live notifications are not configured"))
	}
	events, unsubscribe, err := s.subscriptions.Subscribe(user.ID)
	if err != nil {
		return connect.NewError(connect.CodeResourceExhausted, err)
	}
	defer unsubscribe()

	// nginx buffers a proxied response body until a buffer fills, which would hold
	// every event in the proxy. This is the per-response switch it honours. It has
	// to be set before the first Send: connect-go emits the headers with it.
	stream.ResponseHeader().Set("X-Accel-Buffering", "no")

	heartbeat := time.NewTicker(notificationHeartbeat)
	defer heartbeat.Stop()
	lifetime := time.After(notificationStreamLifetime)

	for {
		select {
		case <-ctx.Done():
			// The client went away or the connection dropped.
			return nil
		case <-lifetime:
			// See notificationStreamLifetime. Ending the stream is not an error.
			return nil
		case event, ok := <-events:
			if !ok {
				// The subscription was dropped (it could not keep up) or the server
				// is draining. Either way the client reconnects and refreshes.
				return nil
			}
			if err := stream.Send(convertToV1NotificationEvent(event, workspaceID)); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := stream.Send(&v1pb.SubscribeNotificationsResponse{
				Event: &v1pb.SubscribeNotificationsResponse_KeepAlive{KeepAlive: &v1pb.KeepAlive{}},
			}); err != nil {
				return err
			}
		}
	}
}
```

转换函数（与既有 `convertToV1Notification` 并列，纯函数、可单测）：

```go
func convertToV1NotificationEvent(event notification.Event, workspaceID string) *v1pb.SubscribeNotificationsResponse {
	converted := &v1pb.NotificationEvent{Notification: convertToV1Notification(event.Notification, workspaceID)}
	if event.UnreadCountKnown {
		converted.UnreadCount = proto.Int32(event.UnreadCount)
	}
	return &v1pb.SubscribeNotificationsResponse{
		Event: &v1pb.SubscribeNotificationsResponse_Notification{Notification: converted},
	}
}
```

未知 detail 的降级不用再写一遍：`convertToV1Notification` 已经处理（[notification_service.go:213-238](../backend/api/v1/notification_service.go#L213-L238)），老前端收到新类型也不会炸。

### 5.2 接线与停机

- [grpc_routes.go:99](../backend/server/grpc_routes.go#L99)：`notificationService := apiv1.NewNotificationService(stores, notifier)`（`notifier` 已是 `configureGrpcRouters` 的参数）。
- [server.go](../backend/server/server.go) `Shutdown`：在 `s.echoServer.Shutdown(ctx)` **之前**结束所有订阅：

```go
	// A notification stream is an in-flight request that never ends on its own;
	// Echo's Shutdown waits for those, so they are ended first or the graceful
	// period is spent waiting for clients to close their tabs.
	if s.notifier != nil {
		s.notifier.Close()
	}
```

`Shutdown` 里对 nil 的守卫是必须的：`server_lifecycle_test.go` 手工构造的 `Server{}` 没有 notifier（[server_lifecycle_test.go:30-34](../backend/server/server_lifecycle_test.go#L30-L34)、[:77-82](../backend/server/server_lifecycle_test.go#L77-L82)）。

- 拦截器链不用动：auth（必须登录）、两个 throttle（只约束调用发起，不限制连接时长）、error mapping 全都自动生效；audit 不生效（没有注解），ACL 不设门（没有 permission 注解），与其余 5 个方法一致。

## 六、前端：store 的流式改造

`frontend/src/api/notification.ts` 加一个薄封装（与 `explain.ts` 同一形状）：

```ts
/** The caller's live notification stream. Ends when the signal aborts. */
export function subscribeNotifications(signal?: AbortSignal) {
  return notificationClient.subscribeNotifications(
    create(SubscribeNotificationsRequestSchema, { parent: WORKSPACE_PARENT }),
    { signal }
  );
}
```

`frontend/src/store/modules/notification.ts` 把轮询换成订阅：

- 删掉 `POLL_INTERVAL_MS` 与 `pollTimer`，改成一条**常驻循环**（`startStreaming()` / `stopStreaming()`），并把那段"本应用没有推送通道"的注释改写成描述订阅流、重连与两次刷新兜底。
- state 增一个 `arrivalSeq: number`：每收到一条推送事件就 +1，页面据此决定要不要重载首页（页面可能正在看第 2 页，不能因为"有新消息"就把读者弹回第 1 页）。

```ts
const RECONNECT_MIN_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;

let controller: AbortController | undefined;
let running = false;
let wake: (() => void) | undefined;
// 每次 start/stop 都会推进：异步循环没有别的办法判断自己还是不是连接的主人。
let generation = 0;
// 服务端也会推的两个值各有一张票：消息到达时还在飞的请求比消息旧，不能覆盖它。
let countTicket = 0;
let recentTicket = 0;

async function run(store: NotificationStore, mine: number) {
  const live = () => running && generation === mine;
  let delay = RECONNECT_MIN_MS;
  while (live()) {
    controller = new AbortController();
    try {
      // One refresh per (re)connect. It is what makes every gap harmless: a
      // message written while the stream was down, a tab the browser froze, a
      // subscription the server dropped because it fell behind — all of them end
      // up covered by this one call.
      void store.refresh().catch(() => {});
      for await (const message of subscribeNotifications(controller.signal)) {
        // 任何一帧（含 keepalive）都说明连接是健康的，退避回到下限。
        delay = RECONNECT_MIN_MS;
        if (message.event.case === "notification") {
          store.applyArrival(message.event.value);
        }
        // A keepalive carries nothing; it exists so the connection is not idle.
      }
    } catch (error) {
      if (live() && error instanceof ConnectError && error.code === Code.Unimplemented) {
        store.stopStreaming();
        return;
      }
    }
    // 一旦有更新的一代接管了连接，这条循环不能再碰任何外部状态。
    if (!live()) return;
    controller = undefined;
    await wait(delay);
    delay = Math.min(delay * 2, RECONNECT_MAX_MS);
  }
}
```

要点：

- **`sleep` 必须可被 `stopStreaming()` 打断**（登出/卸载时不能留一个 30s 的定时器），用一个能被 `wake()` 唤醒的 promise，而不是裸 `setTimeout`。
- **每次（重）连都 `refresh()`**：这就是第二节那条不变式的前端一侧。初始挂载时它也顺带完成了以前 `tick()` 做的事（登录进来先把角标画出来）。
- **可见性监听保留**，但动作从"拉一次计数"升级为 `refresh()`（计数 + 已加载过的列表）：浏览器对后台标签页的连接可能冻结或关闭，重新可见时刷新一次最省事。
- 握手期的 `Unauthenticated` 由既有 `sessionInterceptor` 处理（刷新 + 重试一次，失败则登出，[session.ts:73-101](../frontend/src/api/session.ts#L73-L101)）；流中期的网络错误不进拦截器，交给上面的退避重连——重连的握手会再走一遍刷新逻辑。`Unimplemented`（前端比服务端新，服务端还没有这个方法）是唯一不重试的错误：重试解决不了，安静停掉即可。

`applyArrival(event)`：

```ts
applyArrival(event: NotificationEvent) {
  if (event.unreadCount !== undefined) {
    this.unreadCount = event.unreadCount;
  } else {
    // 服务端这次读不到计数（事件里没有该字段）：自己问一次，别把角标显示错。
    void this.refreshUnreadCount().catch(() => {});
  }
  if (this.recent.length > 0 && !this.recent.some((item) => item.name === event.notification?.name)) {
    this.recent = [event.notification!, ...this.recent].slice(0, RECENT_NOTIFICATION_LIMIT);
  }
  this.arrivalSeq += 1;
}
```

（`recent.length > 0` 与既有的 `refresh()` 判断一致：用户没打开过下拉就不必在后台替他维护列表。）

## 七、前端：铃铛与 `/notifications` 页面

- **铃铛**：只把 `onMounted(() => store.startPolling())` / `onBeforeUnmount(() => store.stopPolling())` 换成 `startStreaming()` / `stopStreaming()`（[NotificationBell.vue:154-155](../frontend/src/components/layout/NotificationBell.vue#L154-L155)）。归属不变——侧边栏只在登录后的壳里存在，它的生命周期就是会话的生命周期；"点开下拉时拉一次最新列表"保留（它同时也是打开下拉时的一次兜底）。
- **`/notifications` 页面**：加一个 watcher，只在自己停在第 1 页时重载：

```ts
// 新消息只属于第 1 页。停在后面的页时重载会把读者弹回最新的行，所以只在第一页刷新。
watch(
  () => store.arrivalSeq,
  () => {
    if (!hasPrevious.value) {
      void refresh();
    }
  }
);
```

`hasPrevious` 由 `usePagedFetch` 提供（[usePagedFetch.ts:23-34](../frontend/src/composables/usePagedFetch.ts#L23-L34)、[NotificationsPage.vue:271-282](../frontend/src/pages/NotificationsPage.vue#L271-L282)）。未读筛选下新消息天然属于结果集，不需要额外判断。

- **i18n**：本方案不新增任何用户可见文本，locales 不动（如果实现时决定加"已断开"之类的指示，才需要 `en-US`/`zh-CN` 双语并跑 `i18n:sort`）。

## 八、边界、取舍与风险

- **多副本**：hub 是进程内的。用户连在副本 A、消息由副本 B 写出时，推送到不了——要靠重连/重新可见那次刷新兜。这与既有的"设备登录状态进程内""同步操作聚合器进程内"是同一取向。真要多副本时，正解是 PostgreSQL `LISTEN/NOTIFY`（写库后 NOTIFY 收件人，每个副本一个 listener 连接），本方案刻意不做：它要多一条常驻连接、断线重连与去重语义，而现在的部署形态是单副本。
- **HTTP/1.1 的连接预算**：一个标签页会长期占住一条同源连接（浏览器的每源 6 条上限里的一条）。本项目是 SPA + 少量并发 RPC，这没有实际影响；TLS 终止的反代若上 HTTP/2，浏览器会多路复用，这条也就消失了。值得知道，不是阻塞。
- **已读状态不推送**：另一个标签页"全部已读"或撤销已读后，本标签页的角标会旧到下一次刷新/重连/重新可见才对齐（本标签页自己的操作当然会刷新）。要推送读状态变化，就得再定一轮"谁改的、改了哪些行、多标签页怎么避免回声"，收益远小于成本。
- **一次鉴权、随时可断**：流只在握手时鉴权。30 分钟上界让 logout/撤销在一个上界内生效，并且客户端每次重连的握手都会走会话刷新；不做心跳期复查 token（要把 `stateCfg` 的撤销缓存接进 API 层，收益是一个更短的上界）。
- **背压**：订阅缓冲 16 条，满了就关掉那条流。消息在库里，客户端重连即恢复，不存在"消息丢了"。
- **best-effort 不变**：写通知失败仍只记日志；计数读不到仍然投递消息本身（事件里没有 `unread_count`，客户端补问一次）。推送本身不会失败（非阻塞 send）。
- **每收件人 8 条订阅上限**：一个标签页一条，重连时短暂重叠。超了拒绝新连接而不是踢掉旧连接——被拒的一方收到 `ResourceExhausted`，而不是另一个标签页静默变哑。
- **反代契约**：`X-Accel-Buffering: no` + 25s 心跳 + `proxy_read_timeout` 默认 60s；部署若把读超时调到 25s 以下需要同步调小心跳（常量）。
- **`plan/frontend-architecture-review.md` 的资产保持**：前端不引入 `EventSource`/裸 `fetch`，订阅流走 `api/notification.ts` + ConnectRPC client；该文档第 67 行点名的那条纪律仍然成立。

## 九、分阶段落地

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| **Phase 1 proto** | `SubscribeNotifications` + 三个新消息；`buf format/lint/generate` 并提交生成物 | `buf lint proto` 干净；`go build ./...` 通过；前端 `type-check` 通过 |
| **Phase 2 hub 与发布点** | `component/notification/hub.go`；`AdminStore` 加计数；`create` 交出行并发布；`Subscribe`/`Close` | 组件单测绿：扇出、按收件人隔离、上限、溢出丢订阅、Close 之后 subscribe 立即结束、dedupe 抑制不发布 |
| **Phase 3 流式 RPC 与停机** | `api/v1` 的 handler + 转换函数；`grpc_routes.go` 接线；`server.go` 停机先关 hub；集成用例 | 集成：连上以后触发一次库同步 → 流上出现该消息、`unread_count` 与 `GetUnreadNotificationCount` 一致；未登录被拒；停机不再空等 |
| **Phase 4 前端** | `api/notification.ts` 封装；store 去掉轮询、加订阅循环 + 重连 + `arrivalSeq`；铃铛与页面接线；迁移既有 5 个 store 测试 | `biome:check` / `lint` / `i18n` / `type-check` / `test run` 全绿；手工：触发同步 → 铃铛角标与下拉无需刷新即更新 |
| **Phase 5 文档** | 本文标记为已实现；`system_notification_plan.md` 第 7 条"实现状态"与 Phase 5 的"轮询发现新消息"改写指向本文；`docs/security-posture.md` 若有需要，补一条"通知流只在连接时鉴权、30 分钟上界" | 文档与代码一致，没有残留的"只能轮询"表述 |

## 十、测试

**Go（hermetic，`go test ./...` 不需要数据库）**

- `backend/component/notification/`（扩展 `service_test.go` 或新 `hub_test.go`）：
  - 两个订阅者收同一收件人的事件都拿到；另一个收件人拿不到；
  - 订阅上限：超过 `MaxSubscriptionsPerRecipient` 的第 N+1 次 subscribe 返回错误；
  - 缓冲溢出：不发新事件的订阅者被关掉通道，而 `publish` 立刻返回（不阻塞写通知的 goroutine）；
  - `Close()` 结束所有订阅，且此后的 subscribe 立刻拿到已关闭的通道；
  - `create` 路径：`CreateNotification` 返回 `(nil, nil)`（被 dedupe 抑制）时**不发布**；返回行时发布，且 `UnreadCountKnown` 与假 store 的计数一致；计数报错时事件仍然发布、`UnreadCountKnown` 为 false。
- `backend/api/v1/notification_service_test.go`：`convertToV1NotificationEvent`（有/无 `unread_count`、name 带工作区、未知 detail 降级）；`SubscribeNotifications` 未登录返回 `Unauthenticated`；`subscriptions == nil` 时返回 `Unimplemented`。

**Go（integration，`backend/test/integration/runner/notification_service_test.go`，真 server + PostgreSQL + MySQL）**

- `TestSubscriptionPushesAMessageWhileConnectedRealServerIntegration`：用 admin token 打开 `SubscribeNotifications`（connect-go 客户端拿到 `*connect.ServerStreamForClient`），再触发一次库同步，断言在限时内收到 `notification` 事件：类型是 `SCHEMA_SYNC`、点名的库正确、`name` 形如 `workspaces/{ws}/notifications/{id}`、`unreadCount` 等于 `GetUnreadNotificationCount`。收流要放在 goroutine 里配 `time.After`，因为 `Receive()` 会阻塞。
- 同一文件里加一条按收件人隔离的：另一个成员账号的流收不到管理员那条消息（与既有 `TestNotificationInboxIsPersonalRealServerIntegration` 同一取向）。

**前端（Vitest）**

- `frontend/src/store/modules/notification.test.ts`：mock `subscribeNotifications` 为一个测试可手动推进的 async generator，替换现有的两条轮询用例：
  - 推送事件更新角标与列表；`KeepAlive` 事件什么都不改；
  - 事件不带 `unreadCount` 时调用 `refreshUnreadCount()`；
  - 流异常结束后按退避重连，且每次连接都触发一次 `refresh()`；
  - `stopStreaming()` 会中止在飞的流、打断退避等待、不再重连；
  - 重新可见时刷新一次。
- `NotificationBell.vue` 的挂载/卸载接线由 store 测试覆盖；`NotificationsPage.vue` 的"仅第 1 页刷新"如果实现时觉得值得，可加一个组件级交互测试（页面不在覆盖率门槛内，属可选）。

## 十一、验证清单

**门禁（按仓库约定逐条跑）**

```bash
# proto
buf format -w proto && buf lint proto && (cd proto && buf generate)
# go
gofmt -w <modified files> && golangci-lint run --allow-parallel-runners   # 反复跑到干净
go test ./...
go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go
# 集成（触到 server wiring 与真实 PG/MySQL）
make test-integration-smoke && make test-integration
# frontend
pnpm --dir frontend biome:check && pnpm --dir frontend lint && pnpm --dir frontend i18n
pnpm --dir frontend type-check && pnpm --dir frontend test run
```

**手工验收（真实浏览器，前后端各起一个）**

1. `PG_URL=... go run ./backend/bin/server/main.go --port 8083 --debug` + `pnpm --dir frontend dev`；
2. 登录，打开侧边栏铃铛并**保持页面不动**；
3. 另开一处触发一次库同步（页面点同步或 REST 调用）；
4. 断言：同步完成后**无需刷新**，角标立刻出现、下拉里的列表在打开时已包含那条消息（与 `create_time`、库名一致）；
5. 停在 `/notifications` 第 1 页重复第 3 步 → 列表自动出现新行；翻到第 2 页再触发 → 视图不被弹回；
6. 断开后端（或 `kill` 进程）→ 前端静默退避重连、不报错；后端恢复后 30s 内自动连上并刷新（角标补齐断线期间的消息）；
7. 服务端 `SIGTERM`：进程在秒级退出（不再等 `gracefulShutdownPeriod`），日志里没有 `failed to shut down the web server`；
8. 命令行旁证：`curl -N -X POST 'http://127.0.0.1:8083/metaxisdata.v1.NotificationService/SubscribeNotifications' -H 'Content-Type: application/json' -b '<登录 cookie>' -d '{"parent":"workspaces/-"}'` 能看到 25s 一次的心跳帧，触发同步后能看到事件帧。

## 十二、后续（本方案不做）

1. 跨副本投递（PostgreSQL `LISTEN/NOTIFY`），一旦真出现多副本再说。
2. 推送已读状态变化，让多标签页的角标严格一致。
3. 桌面通知 / toast（前一份文档 Phase 5 的另一半）：有了这条流，`notify` 只是 `applyArrival` 里多一个分支，可作为独立小改。
4. 心跳期复查 token 撤销，把 30 分钟上界收紧到心跳间隔。
