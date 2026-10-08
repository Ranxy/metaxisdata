# Plan: 站内信 / 系统通知

> **Status: 已实现**（Phase 1–4；Phase 5 的可选增强未做）。收件人、粒度、事件范围、渠道、保留策略已按评审定稿（见"已确认的决定"），实现与设计的偏差记在"实现状态"。

## TL;DR

新增**按用户投递**的站内信：`notification` 表（`recipient_id` + JSONB payload，形状跟随 `audit_log`，但多一维"收件人"）+ `NotificationService`（登录即可调用，作用域由 handler 按当前用户保证，不新增权限）+ 前端侧边栏铃铛与 `/notifications` 页面。

写入口径是**"一次同步操作一条消息"**：

- 人工触发的同步（实例全量、实例增量、批量中的每个实例、单库、建实例后的首次同步）→ 操作结束后给**发起人**一条；
- 后台周期同步的**失败**（重试耗尽、实例级失败）→ 给**工作区管理员**一条，按小时桶去重；
- OpenLineage 摄取异常 → 给管理员一条，按 `主体（namespace 或 ingestion key）+ 错误类` 分桶去重：服务端失败（5xx）与**被拒收的请求**（400 解析/校验、413 超限、403 超 scope）都通知；未匹配到实例、落到 external dataset 的 namespace 也通知。

消息正文**不存文本**，存"类型枚举 + 结构化 detail"，由前端 i18n 渲染（`en-US` / `zh-CN` 双语文案是硬要求，服务端存死文本无法双语）。

## 实现状态

| 阶段 | 提交 | 关键文件 |
| --- | --- | --- |
| Phase 1 数据与协议 | `feat(notification): add the notification store, schema and API surface` | 两个新 proto、`backend/migrator/migration/0.1/0017##notification.sql`、`backend/store/notification.go` |
| Phase 2 组件与 API | `feat(notification): serve the caller's inbox over NotificationService`、`feat(notification): show the inbox in the sidebar bell and on a page` | `backend/component/notification/`、`backend/api/v1/notification_service.go`、`backend/server/{server,grpc_routes}.go`、`frontend/src/{api,store/modules,lib,components/layout,pages}/notification*` |
| Phase 3 同步聚合 | `feat(notification): report sync operations and ingestion failures` | `backend/runner/schemasync/operation.go`、`syncer.go`、`api/v1/{instance,database}_service.go` |
| Phase 4 OpenLineage | 同上 | `backend/api/v1/openlineage_handler.go`、`backend/plugin/openlineage/{resolver,processor}.go` |

实现时与设计不同的地方：

1. **`SchemaSyncDetail.failures` 最终叫 `databases`**，语义是"这条消息点名的库"，每条都带状态。单库同步如果只回一句"成功 1 个"，用户分不清是哪一次——而"单独触发一次库同步"正是需求里点名要的场景。实例级操作仍然只列失败/未完成的库，成功的只计数，前端按状态分别渲染（成功 / 失败原因 / 无结果）。
2. **数据库资源名是 `instances/{instance}/databases/{database}`**（本设计文档原先写成 `databases/{instance}/{database}`，不对）。
3. **两侧协作方都用窄接口**：runner 侧是 `schemasync.Notifier`，HTTP 摄取侧是 `v1.OpenLineageNotifier`（同时覆盖 `ReportUnmatchedNamespace`）；`*notification.Service` 是唯一的生产实现。好处是两侧都能在没有数据库的情况下测试，传 nil 时静默。
4. **单库同步不走操作聚合器**：`Syncer.SyncDatabaseForUser` 在请求内同步执行、直接投递；只有实例同步、批量同步、建实例这三条有异步尾巴的路径登记 `SyncOperation`。
5. **载荷边界放在 `component/notification` 的构造函数里**（`MaxFailureEntries = 100`、`MaxErrorBytes = 2KiB`，按 rune 截断），调用方传什么都不会撑大那条永久保留的行。
6. **eslint 的 `no-unused-keys` 需要显式 ignores**：`src/lib/notificationText.ts` 返回的 key 那条规则追不到（`scripts/check-vue-i18n.mjs` 的 `KEY_PROP_RE` 追得到），所以按 `relationType` 的先例把这一族加进了 `eslint.config.mjs`。
7. **铃铛承担轮询的生命周期**：它只在登录后的侧边栏里存在，挂载即开始、卸载即停止，正是会话本身的生命周期。

---

## 已确认的决定

| # | 决定 | 影响 |
| --- | --- | --- |
| 1 | 收件人＝**操作发起人**；只有后台/周期同步失败才发给**工作区管理员** | 需要"操作"实体与"角色→用户"解析 |
| 2 | **只做消息中心**，不建同步任务表、不展示"进行中/进度" | 聚合状态放在 syncer 进程内，不是新表 |
| 3 | 粒度＝**一次同步操作一条**（单库同步一条、实例同步一条） | 引入 operation 聚合器 |
| 4 | 首批事件：schema 同步 + OpenLineage 摄取异常 | 两类 detail |
| 5 | 仅站内信（铃铛 + 列表），不做邮件 / webhook | 无出站投递器 |
| 6 | **永久保留**（跟随 `audit_log`），不自动清理 | 无 TTL，表持续增长 |
| 7 | 后台通知的抑制窗口默认 **1 小时桶**（`NAMESPACE_UNMAPPED` 为 24 小时） | 常量，可调 |
| 8 | 建实例后的首次全量同步**要发**消息（发起人＝创建者） | `CreateInstance` 也要登记操作，否则用户建完实例要等到下次手动同步才有消息 |
| 9 | OpenLineage 的 **4xx 拒收**（解析/超限/超 scope）与 **namespace 未映射**都要通知管理员 | 通知点从 2 个 5xx 分支扩到全部拒收分支 + resolver |
| 10 | 管理员手动发消息**不做** | 正文仍全部走 i18n 渲染，不引入自由文本 |

另有一点在评审时未被问到、由本方案先定，可推翻：

- **一个库在"这次操作"里的结果＝它的第一次结果。** 失败后的退避重试（1m/5m/15m）属于 checker 的后台恢复机制，不改写已写出的消息，也不再补发——避免同一故障产生两套噪声。代价：偶发瞬时失败也会出现在消息里，见"边界与取舍"。

---

## 一、现状与缺口（代码事实）

1. **异步同步的结果无处可查。** 实例级同步在 RPC 内完成（[instance_service.go:342](../backend/api/v1/instance_service.go#L342)），库级同步是**入内存队列后异步执行**（`SyncAllDatabases` / `SyncDatabasesAsync` → `databaseSyncMap`，[syncer.go:359-391](../backend/runner/schemasync/syncer.go#L359-L391)），由 10s 一次 checker 消费（[syncer.go:207-293](../backend/runner/schemasync/syncer.go#L207-L293)）。失败只 `slog.Warn` + `scheduleRetry`（1m/5m/15m，3 次后放弃，[syncer.go:150-170](../backend/runner/schemasync/syncer.go#L150-L170)）；成功只更新 `db.metadata.last_sync_time`。**没有任何同步结果表**，RPC 返回后用户无从得知结局。
2. **触发入口共 5 处**：`CreateInstance`（[:162-170](../backend/api/v1/instance_service.go#L162-L170)）、`SyncInstance`、`BatchSyncInstances`（[:333-409](../backend/api/v1/instance_service.go#L333-L409)）、`SyncDatabase`（[database_service.go:34-48](../backend/api/v1/database_service.go#L34-L48)）、周期扫描 `trySyncAll`（[syncer.go:297-357](../backend/runner/schemasync/syncer.go#L297-L357)）。其中只有 `SyncDatabase` 是请求内同步完成的，其余都有异步尾巴。
3. **周期同步没有发起人**：`trySyncAll` 每 15 分钟按 `sync_interval` 扫描，且 `SyncInstance` 失败时 `last_sync_time` 不前进，会每轮重试——这类失败必须限流，否则 15 分钟一条。
4. **可复用的既有模式**：`audit_log`（`BIGSERIAL + created_at + JSONB payload`，JSONB 绑定 `proto/store` 消息，[LATEST.sql:532-544](../backend/migrator/migration/LATEST.sql#L532-L544)）+ `AuditLogService`（分页、`workspaces/-` 解析、资源名 `{parent}/auditLogs/{id}`、store/v1 两份消息 + converter）。站内信照抄骨架，只多一个 `recipient_id`。
5. **权限模型不表达"个人数据"**：单租户、workspace 级能力（[predefined_roles.go:33-48](../backend/store/predefined_roles.go#L33-L48)）。但 ACL 拦截器对**无 permission 注解**的方法不设门（[acl_interceptor.go:74](../backend/api/v1/acl_interceptor.go#L74)），而认证默认必需（[auth.go:89-95](../backend/api/auth/auth.go#L89-L95)）——这正是"登录即可、handler 自己作用域"的现成通道，不需要往 `permission.json` 加东西（给收件箱加权限反而会出现"自定义角色看不到自己消息"的怪相）。
6. **管理员解析可行**：`GetWorkspaceIamPolicy` + `utils.GetUsersByMember`（已支持 `users/{id}` 与 `groups/{email}` 展开，[member.go:36-66](../backend/utils/member.go#L36-L66)）；`allUsers` 按不变式不可能绑到 `roles/workspaceAdmin`（[policy.go:40-60](../backend/store/policy.go#L40-L60)），管理员集合一定是有名有姓的用户。
7. **前端没有推送通道**（SSE 只出现在 LLM 客户端与 MCP），实时性只能轮询；账号区在侧边栏底部 `UserMenu`（[AppSidebar.vue:169-173](../frontend/src/components/layout/AppSidebar.vue#L169-L173)）。
8. **`src/lib/`、`src/utils/`、`src/composables/` 有覆盖率门槛**（95% lines / 85% branches），新增文件必须带测试。
9. **OpenLineage 摄取是同步 HTTP 路径，异常只进日志**：拒收（400/403/413）与服务端失败（500）都只 `slog.Warn/Error` 后直接返回（[openlineage_handler.go:106-137](../backend/api/v1/openlineage_handler.go#L106-L137)、[:171-249](../backend/api/v1/openlineage_handler.go#L171-L249)）；"数据集没匹配到任何实例、被存成 external dataset"只在 `Resolver` 里每进程每 namespace Warn 一次（[resolver.go:135-152](../backend/plugin/openlineage/resolver.go#L135-L152)）。摄取用 API key 认证，**没有发起用户**，所以这些异常只可能投给管理员。
10. **系统身份现成**：`common.SystemBotID = 1`（[const.go:10-11](../backend/common/const.go#L10-L11)）。

---

## 二、数据模型

```sql
-- notification 是发给某一个用户的一条站内信：一次已完成同步操作的结果，
-- 或一条管理员需要知道的后台异常。它是个人数据，所以读路径按 principal id
-- 作用域，而不是像其余表那样按 workspace 作用域。
CREATE TABLE IF NOT EXISTS notification (
    id BIGSERIAL PRIMARY KEY,
    recipient_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at TIMESTAMPTZ,                 -- NULL = 未读
    -- 后台通知的抑制键 <event>:<target>:<时间桶>；空串表示不去重（发给发起人的个人消息）。
    dedupe_key TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'
);

-- 列表：一个用户的最新消息优先。
CREATE INDEX IF NOT EXISTS idx_notification_recipient_created_at
    ON notification(recipient_id, created_at DESC, id DESC);

-- 未读计数只扫未读行。
CREATE INDEX IF NOT EXISTS idx_notification_recipient_unread
    ON notification(recipient_id) WHERE read_at IS NULL;

-- 冷却窗口内同一目标的重复通知被这条唯一索引挡下（跨副本亦然）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_dedupe
    ON notification(recipient_id, dedupe_key) WHERE dedupe_key <> '';

COMMENT ON COLUMN notification.payload IS 'Stored as Notification (proto/store/store/notification.proto)';
```

**为什么读已读状态是列而不是 JSONB 字段**：未读计数是轮询热路径，`payload->>'readTime' IS NULL` 走不了 partial index；`read_at` 列 + 部分索引让计数成为一次索引探针。

**迁移按仓库约定两处同改**：追加到 `backend/migrator/migration/LATEST.sql`，并新增 `backend/migrator/migration/0.1/0017##notification.sql`（幂等 DDL），同时保持 `backend/store` 查询与两者一致。

**永久保留**：不接 maintenance runner 的 TTL 清理（[maintenance.go:69-105](../backend/runner/maintenance/maintenance.go#L69-L105) 是既有清理模式，本次刻意不加入）。表会持续增长，量级话题见"后续"。

---

## 三、Proto

### 3.1 `proto/store/store/notification.proto`（行形状）

枚举值必须带前缀：同包已有 `AuditLogSeverity.INFO/WARNING/ERROR`，且 `UNSPECIFIED` 这个裸名字已被 `MetaType` 占用——proto 的枚举值名按包作用域唯一，重名是编译错误。

（`SchemaSyncDetail`、`OpenLineageDetail` 以及 `SyncTrigger` / `SyncDatabaseState` / `OpenLineageFailureKind` 在 `proto/store` 与 `proto/v1` **两个包里各定义一份**，字段一致——沿用 `AuditLog` / `AuditLogSeverity` 的"store 与 v1 两份消息"模式；store 侧多一个 `dedupe_key`。）

```proto
enum NotificationType {
  NOTIFICATION_TYPE_UNSPECIFIED = 0;
  NOTIFICATION_TYPE_SCHEMA_SYNC = 1;
  NOTIFICATION_TYPE_OPENLINEAGE_INGESTION = 2;
}

enum NotificationSeverity {
  NOTIFICATION_SEVERITY_UNSPECIFIED = 0;
  NOTIFICATION_SEVERITY_INFO = 1;
  NOTIFICATION_SEVERITY_WARNING = 2;
  NOTIFICATION_SEVERITY_ERROR = 3;
}

message Notification {
  // notification 行的主键。对外以资源名 workspaces/{workspace}/notifications/{id} 暴露。
  int64 id = 1;
  google.protobuf.Timestamp create_time = 2;
  string parent = 3;      // workspaces/{id}
  int32 recipient_id = 4; // principal.id
  NotificationType type = 5;
  NotificationSeverity severity = 6;
  google.protobuf.Timestamp read_time = 7;
  // 写入路径使用的抑制键；对外不暴露。
  string dedupe_key = 8;
  oneof detail {
    SchemaSyncDetail schema_sync = 9;
    OpenLineageDetail openlineage = 10;
  }
}
```

### 3.2 `proto/v1/v1/notification_service.proto`（公开面）

照 `AuditLog` 的"store/v1 两份消息 + converter"模式，v1 侧只读，且去掉 `recipient_id` / `dedupe_key`。

```proto
message Notification {
  string name = 1;                       // workspaces/{workspace}/notifications/{id}
  google.protobuf.Timestamp create_time = 2;
  NotificationType type = 3;
  NotificationSeverity severity = 4;
  google.protobuf.Timestamp read_time = 5;   // 未读为空
  oneof detail {
    SchemaSyncDetail schema_sync = 6;
    OpenLineageDetail openlineage = 7;
  }
}

enum SyncTrigger {
  SYNC_TRIGGER_UNSPECIFIED = 0;
  SYNC_TRIGGER_MANUAL = 1;      // 用户在页面/RPC 触发，收件人是发起人
  SYNC_TRIGGER_BACKGROUND = 2;  // 周期扫描触发，收件人是管理员
}

enum SyncDatabaseState {
  SYNC_DATABASE_STATE_UNSPECIFIED = 0;
  SYNC_DATABASE_STATE_SUCCEEDED = 1;
  SYNC_DATABASE_STATE_FAILED = 2;
  // 操作超时或目标消失前没有等到这个库的结果。
  SYNC_DATABASE_STATE_UNFINISHED = 3;
}

message SyncDatabaseResult {
  string database = 1;      // databases/{instance}/{database}
  SyncDatabaseState state = 2;
  string error = 3;         // 写入前截断到 2KB，避免一条巨型错误撑大 JSONB
}

message SchemaSyncDetail {
  string instance = 1;          // instances/{id}
  string instance_title = 2;    // 展示名，省掉前端二次查询
  SyncTrigger trigger = 3;
  // 实例元数据这一步失败时的错误；成功为空。
  string instance_error = 4;
  // 这条消息点名的库，每条带结果：实例级操作列出失败/未完成的，单库同步列出那个库。
  // 成功的只计数；列表上限 100 条，超出时靠计数表达。
  repeated SyncDatabaseResult databases = 5;
  int32 succeeded_count = 6;
  int32 failed_count = 7;
  int32 unfinished_count = 8;
}

message OpenLineageDetail {
  OpenLineageFailureKind kind = 1;
  // 事件或批次里出现的 namespace；拒收分支里事件没解析出来时为空。
  string namespace = 2;
  string job = 3;
  string run_id = 4;
  // 落到 external dataset 的那个 dataset（仅 NAMESPACE_UNMAPPED 有值）。
  string dataset = 5;
  // 提交方的 ingestion key 掩码标识（例如 "mxd_ol_…ab12"），绝不是明文。
  string api_key = 6;
  // 本次请求/批次：收到的与失败的事件数。
  int32 received_count = 7;
  int32 failed_count = 8;
  string error = 9;   // 写入前截断到 2KB
}

enum OpenLineageFailureKind {
  OPENLINEAGE_FAILURE_KIND_UNSPECIFIED = 0;
  // 事件本身解析不出来、或超出单事件限制（HTTP 400/413）。
  OPENLINEAGE_FAILURE_KIND_INVALID_EVENT = 1;
  // 请求体、批次条数等传输层超限（HTTP 413）。
  OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED = 2;
  // 事件的 namespace 与该 key 的 scope 不符（HTTP 403）。
  OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH = 3;
  // 服务端持久化失败（HTTP 500）。
  OPENLINEAGE_FAILURE_KIND_PERSIST_FAILED = 4;
  // 服务端处理/血缘派生失败（HTTP 500）。
  OPENLINEAGE_FAILURE_KIND_PROCESS_FAILED = 5;
  // 数据集没匹配到任何实例，被存成了 external dataset。
  OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED = 6;
}
```

**面向管理员的提示文本不落库**：`kind` 已经决定"该怎么办"（例如 `SCOPE_MISMATCH` → 检查这个 key 的 namespace scope；`NAMESPACE_UNMAPPED` → 核对实例地址或加一条 namespace mapping），所以提示语是前端 i18n 的一部分，不是服务端文本。

**体积边界**：`databases` 上限 100 条、每条错误 2KB，所以一条消息的 payload 有硬上界；成功库只计数不列举。`SchemaSyncDetail` 是"指针 + 摘要"，不是数据载体。

---

## 四、后端写入路径

### 4.1 组件 `backend/component/notification/`

单一写入口（**不 import `runner/schemasync`**，避免环；`schemasync`、`api/v1` 依赖它）：

```go
// Send 写入一条通知。
func (s *Service) Send(ctx context.Context, n *storepb.Notification) error

// SendToWorkspaceAdmins 解析 roles/workspaceAdmin 的成员（含组展开）后逐条克隆投递。
func (s *Service) SendToWorkspaceAdmins(ctx context.Context, n *storepb.Notification) error

// DedupeKey 生成后台通知的抑制键：<event>:<target>:<bucket>，
// bucket 是按 window 对齐的时间戳（1 小时桶、24 小时桶见下表）。
func DedupeKey(event, target string, now time.Time, window time.Duration) string
```

- 管理员集合来自 workspace IAM policy 中 `role == roles/workspaceAdmin` 的绑定，用 `utils.GetUsersByMember` 展开；跳过 `allUsers` 与 system bot，用户 ID 去重；无管理员时只记日志。
- **两层抑制**：进程内闸门（`map[dedupeKey]time.Time`，窗口见下表）先挡掉绝大多数重复，命中才去解析收件人并写库——否则一个每秒发垃圾事件的生产者会让我们每个请求都去读一遍 IAM policy 并展开组。数据库侧的 `idx_notification_dedupe` + `ON CONFLICT DO NOTHING` 是第二层，负责**跨副本、跨重启**的最终去重。
- `dedupe_key` 为空（发给发起人的个人消息）时两层都不生效，直接写。
- **best-effort 契约**：写通知失败只记日志，绝不影响同步/摄取路径（与 audit 拦截器同一约定）。调用方显式忽略返回错误。

抑制窗口（按事件类，常量表；`<bucket>` 是该窗口起点的时间戳）：

| 事件类 | 键 | 窗口 |
| --- | --- | --- |
| 后台同步失败（实例/库） | `schema-sync.instance:<instance>:<h>` / `schema-sync.database:<db>:<h>` | 1 小时 |
| OpenLineage 拒收 / 服务端失败 | `openlineage:<主体>:<kind>:<h>`，主体＝事件的 namespace，解析不出来时用 `key/<api key id>` | 1 小时 |
| OpenLineage namespace 未映射 | `openlineage.namespace-unmapped:<namespace>:<d>` | 24 小时 |

### 4.2 同步操作聚合（核心）

新增 `backend/runner/schemasync/operation.go`：进程内聚合器，与既有 `databaseSyncMap` 同生命周期。

```go
// Operation 是一次用户可见的同步操作：实例元数据 + 它入队的所有库。
type Operation struct {
    initiatorID int
    trigger     SyncTrigger
    instanceID  string
    instanceTitle string
    instanceErr string
    pending     map[string]*store.DatabaseMessage // 尚未出结果的库
    results     []DatabaseResult
    deadline    time.Time
}

func (s *Syncer) StartOperation(trigger SyncTrigger, initiatorID int, instance *store.InstanceMessage) *Operation
func (s *Syncer) RecordInstanceResult(op *Operation, err error)
func (s *Syncer) EnqueueForOperation(op *Operation, databases []*store.DatabaseMessage)
func (s *Syncer) FinishOperation(op *Operation) // 无待办库时立即收尾并投递
```

要点：

- `enqueueDatabase(database)` 改为 `enqueueDatabase(database, op)`：op 非空时在 `op.pending` 登记（替代裸的 `SyncAllDatabases` / `SyncDatabasesAsync`）。
- checker 里每个库同步返回后调用 `completeDatabase(database, err)`：把结果**扇出**给所有包含该库的 op——两个并发操作共享一个库是可能的（`databaseSyncGate` 会让后来者复用结果，[syncer.go:539-553](../backend/runner/schemasync/syncer.go#L539-L553)）。
- checker 丢弃"实例已不存在"的队列项时（[syncer.go:232-241](../backend/runner/schemasync/syncer.go#L232-L241)）也要回报，否则 op 永远等不到结果。
- **超时兜底**：op 带 `deadline`（常量，建议 2h）；checker 的 10s tick 顺带 finalize 超时 op，未出结果的库记 `UNFINISHED`。这是"永远不写消息 / 内存条目永不释放"的唯一保险。
- 调用方改造：`CreateInstance`、`SyncInstance`、`BatchSyncInstances`（每个实例一个 op）登记 op 并在同步阶段结束后 `RecordInstanceResult`，然后 `EnqueueForOperation` + `FinishOperation`；`SyncDatabase`（请求内同步完成）直接组装结果投递，不需要聚合器。
- 无库入队（实例没有新库、或实例阶段就失败）时 `FinishOperation` 立即写消息。

### 4.3 后台失败 → 管理员

- **库级**：在 `scheduleRetry` 的**放弃**分支（`attempts > maxDatabaseSyncRetries`，[syncer.go:161-166](../backend/runner/schemasync/syncer.go#L161-L166)）通知管理员。这条路径天然低频（每轮 1m/5m/15m 重试后才触发一次），语义也最清楚："重试 3 次仍失败"。dedupe 键 `schema-sync.database:<db>:<h>`。
- **实例级**：`trySyncAll` 里 `SyncInstance` 报错 → 管理员。dedupe 键 `schema-sync.instance:<instance>:<h>`——因为 `last_sync_time` 不前进，不加桶会每 15 分钟一条。
- 后台**成功不通知**：没有人在等这一条，只会变成 15 分钟一次的噪声。

### 4.4 OpenLineage 摄取异常 → 管理员

摄取是 API key 认证的机器流量，**没有发起用户**，所以异常一律给管理员。来源有三处。

**（1）被拒收的请求（4xx）** —— 在 [openlineage_handler.go](../backend/api/v1/openlineage_handler.go) 的每个拒绝分支上投递：

| 分支 | HTTP | kind | 主体（去重键里的 target） |
| --- | --- | --- | --- |
| `ParseRunEvent` 失败、批次整体解析不出 | 400 | `INVALID_EVENT` | `key/<id>`（事件没解析出来，拿不到 namespace） |
| `ValidateEventLimits` 超限 | 400 / 413 | `LIMIT_EXCEEDED` | `key/<id>` |
| 请求体 > 8MiB、批量 > 1000 条、单事件超限 | 413 | `LIMIT_EXCEEDED` | `key/<id>` |
| `eventWithinScope` 不符 | 403 | `SCOPE_MISMATCH` | 事件的 namespace |

为了让"每一次摄取失败都进收件箱"是代码结构的一部分、而不是靠评审逐个检查，把这些分散的 `return c.JSON(...)` 收成一个辅助函数（4xx 与 5xx 共用，severity 由 status 决定）：

```go
// reportIngestionFailure 记录一次摄取失败：通知管理员，再写 HTTP 响应。
func (h *OpenLineageHandler) reportIngestionFailure(c echo.Context, key *store.OpenLineageAPIKeyMessage,
    kind storepb.OpenLineageFailureKind, status int, body any, cause error) error
```

`keyMessage` 已经传进 `handleIngestion`，只需把它（而不是 `keyMessage.ScopeNamespace`）继续传给 `processBatchEvents`。展示用 `MaskedKey`，去重键用不敏感的 `key/<id>`。

**（2）服务端失败（5xx）** —— `UpsertOpenLineageRun(s)` / `ProcessRunEvent` 报错（[:125-134](../backend/api/v1/openlineage_handler.go#L125-L134)、[:205-229](../backend/api/v1/openlineage_handler.go#L205-L229)），同样走 `reportIngestionFailure`，kind 为 `PERSIST_FAILED` / `PROCESS_FAILED`、severity=ERROR。批量的"部分成功"走的也是 500（[:244-249](../backend/api/v1/openlineage_handler.go#L244-L249)），已被这条覆盖。

**（3）namespace 未映射** —— `Resolver.warnUnmatchedNamespace` 已经有一个"每个 namespace 每个进程只报一次"的闸门（[resolver.go:135-152](../backend/plugin/openlineage/resolver.go#L135-L152)，注释里写明 ingestion 用的是长生命周期 resolver）。把通知挂在**同一个闸门**上即可：闸门首次触发时除了 `slog.Warn` 再调一次注入进来的 reporter。注入方式：

- `openlineage.NewResolver` / `NewProcessor` 接受一个可选的 `UnmatchedNamespaceReporter` 接口（`ReportUnmatchedNamespace(ctx, namespace, dataset string)`）；
- 由 [grpc_routes.go:269](../backend/server/grpc_routes.go#L269) 的 `NewOpenLineageHandler` 把 notification service 接进去；
- [server.go:116](../backend/server/server.go#L116) 那个给 maintenance / lineagevalidation 用的 processor **不注入**，reporter 为 nil 时行为与今天完全一致。

**severity**：4xx 与 `NAMESPACE_UNMAPPED` 记 WARNING（生产者侧/配置侧问题），5xx 记 ERROR。

**明确不通知**：

- **401**（缺 key / key 无效）：匿名探测流量与配置错误混在一起，且没有可信主体（IP 可伪造）；通知管理员只是把攻击流量搬进收件箱。它已经在审计记录里。
- **429**（rate limit）：producer 超预算，HTTP 响应已经说清楚了，重复通知只会淹没其它消息。
- 事件解析成功但一条血缘都没提取到（[processor.go](../backend/plugin/openlineage/processor.go) 里的若干 Warn）：那是数据质量问题，不是摄取失败，留作后续。

---

## 五、对外 API

新增 `NotificationService`，**不加 `(metaxisdata.v1.permission)` 注解，也不加 `allow_without_credential`**：登录必需，作用域由 handler 保证（每一条 SQL 都带 `recipient_id = 当前用户`）。

| RPC | HTTP | 说明 |
| --- | --- | --- |
| `ListNotifications` | `GET /v1/{parent=workspaces/*}/notifications` | `unread_only` 过滤；分页沿用 `parseLimitAndOffset` / `paginate`（[common.go:156-183](../backend/api/v1/common.go#L156-L183)）；支持 `workspaces/-` 表示当前工作区 |
| `GetUnreadNotificationCount` | `GET /v1/{parent=workspaces/*}/notifications:unreadCount` | 铃铛轮询专用，走 partial index |
| `BatchMarkNotificationsRead` | `POST /v1/{parent=workspaces/*}/notifications:batchMarkRead` | 幂等，只置 `read_at IS NULL` 的行 |
| `MarkAllNotificationsRead` | `POST /v1/{parent=workspaces/*}/notifications:markAllRead` | 全部已读 |
| `DeleteNotification` | `DELETE /v1/{name=workspaces/*/notifications/*}` | 用户删自己的消息 |

决定与理由：

- **不设 `(metaxisdata.v1.audit)`**：看自己的收件箱不该写进永久审计账本；消息本身也不是用户可写资源。
- **store 层每个查询都以 `recipient_id` 作用域**——按仓库规则（[backend/AGENTS.md](../backend/AGENTS.md)："query 形状本身就是不变式时，在同包加守卫测试"）在 `backend/store/notification_test.go` 加守卫测试，断言 SQL 谓词里始终有 `recipient_id`。
- 不做 CEL `filter`：`unread_only` 一个布尔覆盖了实际需求（全部/未读两个态），引入 CEL→SQL 只会多一份测试面。
- 注册处：`backend/server/grpc_routes.go` 的 `connectHandlers` map + `grpcreflect` 列表（[:143-201](../backend/server/grpc_routes.go#L143-L201)）；REST 网关路线由 `google.api.http` 注解自动生成。

---

## 六、前端

| 文件 | 职责 | 层 |
| --- | --- | --- |
| `src/api/notification.ts` | 5 个 RPC 的调用与 `listAll` 分页 | api |
| `src/store/modules/notification.ts` | Pinia：`unreadCount`、最近消息、`startPolling()` / `stopPolling()` | store |
| `src/lib/notificationText.ts` | **纯函数**：type + detail → `{titleKey, titleParams, messageKey, messageParams, href}` | lib（有覆盖率门槛） |
| `src/components/layout/NotificationBell.vue` | 侧边栏底部（`UserMenu` 之上；折叠态为图标按钮）：红点计数（99+）、下拉最近 10 条、"查看全部" | components |
| `src/pages/NotificationsPage.vue` | `/notifications`：全部/未读切换、单条已读、全部已读、删除、分页、跳转源对象 | pages |

- **文案渲染**：正文由 `notificationText.ts` 决定 i18n key + 参数，组件再 `t()`，服务端不存文案。未知 `type` / 未知 detail 走 fallback 文案——老前端遇到新类型不会炸。
- **i18n**：`notifications.*` 同时进 `en-US.json` 与 `zh-CN.json`，跑 `pnpm --dir frontend i18n:sort`。注意 `scripts/check-vue-i18n.mjs` 的 `KEY_PROP_RE` 能静态追踪 `titleKey` / `messageKey` 这类**字面量属性**（[check-vue-i18n.mjs:60-61](../frontend/scripts/check-vue-i18n.mjs#L60-L61)），所以返回值用这两个属性名即可被追踪；若改用别的属性名，需要把 `"notifications."` 加进 `DYNAMIC_PREFIXES`。
- **轮询**：登录后启动，`document.visibilityState === "visible"` 时每 30s 拉一次未读数；失败静默（轮询失败不应弹 toast）。在 `DefaultLayout.vue`（或 `App.vue`）挂载时 start、卸载/登出时 stop。
- **深链**：schema 同步 → `/instances/{instanceId}`；OpenLineage 的拒收/失败 → `/openlineage/events`（`NAMESPACE_UNMAPPED` → `/settings/openlineage`，直接落到 namespace mapping 页面）。
- **入口**：铃铛放在侧边栏底部而不是顶部栏——桌面端没有顶部栏，账号区就在侧边栏底部；移动端抽屉里有同一个铃铛。`/notifications` 不需要在 `menuItems` 再占一行，通过铃铛下拉的"查看全部"进入。
- 顺手清理：`frontend/src/types/index.ts` 整个文件没有任何 import，其中那个同名的 `Notification` 接口在新类型（`@/types/proto-es/v1/notification_service_pb`）出现后只会误导，已随本特性删除。

---

## 七、边界与已接受的取舍

- **多副本 / 进程重启**：操作聚合器与 `databaseSyncMap`、device login state 一样是进程内状态。进程重启会丢掉未完成操作的消息——但同步本身也随之中断，下一次周期扫描会重跑（只是不再补消息）。多副本下每个副本只聚合自己入队的库，语义自洽。
  **备选加固**（本次不做）：操作开始时先落一条 `pending = TRUE` 的隐藏行，完成时 UPDATE；启动时把陈旧的 pending 行收尾为 `UNFINISHED`。它换来重启安全，代价是一个必须被"列表/计数/删除"处处尊重的隐藏态和一个清扫任务。等真出现多副本或频繁重启再做。
- **第一结果即终态**：瞬时失败会以"失败"出现在消息里，而系统的退避重试可能随后成功。换来的是消息及时到达（不用等 1m+5m+15m 的重试序列）。若更在意准确度，可改成"等退避耗尽再收尾"，代价是消息平均晚约 20 分钟。
- **永久保留**：表无界增长。`audit_log` 已被接受永久保留（见 [docs/security-posture.md](../docs/security-posture.md)），站内信沿用同一取向；量级变大后的话题是分区 / 归档，不是 TTL。
- **消息文案演进**：老前端对新类型/新字段只做优雅降级（fallback 文案 + 忽略未知字段），与 proto3 前向兼容一致。
- **拒收也通知，靠分桶收敛噪声**：一个持续发垃圾事件的生产者最多每小时给管理员一条，且消息里带着 key 的掩码标识，管理员能直接找到是哪个接入方。`NAMESPACE_UNMAPPED` 用 24 小时桶，因为它更像"配置没配好"而不是"服务出故障"。`processor.go` 里"事件合法但提不出血缘"的 Warn 刻意不在此列——那是数据质量，不是摄取失败。
- **轮询成本**：每用户 30s 一次计数查询，走 partial index；总量由既有 throttle interceptor 的请求预算兜底。
- **审计边界**：读收件箱、标已读、删除都**不审计**；后台失败通知本身也不写审计（它是通知，不是管理动作）。
- **两层抑制的一致性**：进程内闸门与数据库桶用同一个窗口，所以单副本下两者等价；数据库那层才是多副本/重启后的正确性来源。若把窗口调大，两处必须一起调。

---

## 八、分阶段落地

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| **Phase 1 数据与协议 ✅** | 两个新 proto（store + v1）；`LATEST.sql` + `0.1/0017##notification.sql`；`buf format/lint/generate` 提交生成物；`backend/store/notification.go` + 单测 | 空库与旧库都能升级；`go test ./backend/store/...` 绿；`buf lint` 干净 |
| **Phase 2 组件与 API ✅** | `backend/component/notification/`；`backend/api/v1/notification_service.go` + `grpc_routes.go` 注册；前端 `api/` + `store` + `NotificationBell` + `NotificationsPage` + i18n | 手工造一条消息 → 铃铛计数、单条已读、全部已读、删除都正确；`type-check` / `test run` / `i18n` 通过 |
| **Phase 3 同步聚合 ✅** | `schemasync/operation.go` + `syncer.go` 改造（enqueue 带 op、结果回报、超时收尾、放弃重试时通知管理员）+ 三处 API 调用点 | 一个实例全量同步 → **恰好一条**消息，含成功/失败计数；单库同步 → 一条；后台失败 → 管理员一条且每小时最多一条 |
| **Phase 4 OpenLineage ✅** | handler 的拒收分支收成 `reportIngestionFailure`（400/403/413 全覆盖）+ 两个 5xx 分支投递；`openlineage.Resolver` 注入 `UnmatchedNamespaceReporter` 并在闸门触发时通知；`grpc_routes.go` 接线 | 400 / 403 / 413 / 500 各制造一次 → 管理员各收到一条且 kind 正确；同一 key 连续发垃圾 10 次只落一条；未映射 namespace 只通知一次（24 小时内） |
| **Phase 5 可选** | 轮询发现新消息 → `notify` toast；浏览器原生 Notification；邮件 / webhook | — |

---

## 九、测试

- **store**（hermetic，scanner stub 模式见 [audit_log_test.go](../backend/store/audit_log_test.go)）：`scanNotification`、列表/计数/标已读/删除的 SQL 形状，**作用域守卫测试**断言每个查询都带 `recipient_id`。
- **schemasync**（hermetic）：operation 聚合——多库出一个结果、两库共享的扇出、超时 → `UNFINISHED`、`FinishOperation` 的幂等、`DedupeKey` 的时间桶。
- **api/v1**：`NotificationService` 的 self-scoping（用户 A 看不到 B 的消息）、分页、未知类型降级、`workspaces/-` 解析；handler 每个拒收分支的 `kind` / 主体选择（事件可解析时用 namespace，否则用 key id）；注入 reporter 后 `Resolver` 只在闸门首次触发时通知一次（reporter 为 nil 时行为不变）。
- **前端**：`notificationText.ts`（lib 层覆盖率门槛，覆盖全部 `OpenLineageFailureKind`）、notification store 的轮询/未读状态。
- **集成**（`backend/test/integration/runner/notification_service_test.go`，真 server + PostgreSQL + MySQL，已通过）：`TestSyncWritesTheCallerAMessageRealServerIntegration` 触发一次库同步 → 等到消息出现 → 断言类型/trigger/计数/点名的库/未读态，并走通未读数、标已读、删除、重复删除 NotFound；`TestNotificationInboxIsPersonalRealServerIntegration` 新建一个成员账号登录 → 其收件箱为空且未读数为 0，而管理员自己的收件箱里有那条消息。

---

## 十、剩余可调项（都有默认值，不阻塞实现）

1. 后台通知的抑制窗口：默认 1 小时桶（`NAMESPACE_UNMAPPED` 为 24 小时），可按运维口味整体调大——进程内闸门与数据库桶要一起调。
2. `processor.go` 里"事件合法但提不出血缘"的 Warn 是否也要通知。
3. 是否需要"管理员给指定用户手动发消息"（本次明确不做；由于正文走 i18n 渲染，将来要做需要新增一个自由文本 detail 分支和一条发送 RPC，并重新考虑它与审计的关系）。
