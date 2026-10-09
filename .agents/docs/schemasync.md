# Schema Sync（schemasync）— Reference

> Status：**implemented**，代码评审结论。本文件只记录**今天仍然成立**的已知问题与不变量；评审当时列出的 1–6 号问题均已修复，修复过程不在此保留。
> 范围：`backend/runner/schemasync`。
> Related：`docs/security-posture.md`（破坏性 sync 仅记日志；数据库 `deleted` 标记单向权威）。

## 仍然成立的不变量

| 不变量 | 位置 | 破坏后果 |
| --- | --- | --- |
| 元数据事务内只做纯写：digest 读取、`bmc.prepare()` 的 diff、DDL 抓取与 DDL diff 全部在 `BeginTx` 之前 | `backend/runner/schemasync/syncer.go:719-741` | 否则 `meta_registry_resource` 的行锁会横跨对目标实例的全部网络往返（最多 2000 个对象），大库上一次同步可让事务开启数分钟，放大 vacuum 膨胀与锁等待。 |
| 缓存失效严格在 `Commit` 之后 | `backend/runner/schemasync/syncer.go:771-778` | 回滚的同步会留下未提交的缓存条目。 |
| DDL 行严格是 meta 行的子集，且写在同一次事务里 | `backend/runner/schemasync/syncer.go:737-753` | 回滚后会留下一条没有对应元数据行的 DDL。 |
| 元数据指纹用自有 canonical 编码，不走 protojson | `backend/store/meta_hash.go:52-63` | protojson 按镜像随机化 map 字段顺序，会导致全表重写。 |
| 破坏性删除只记日志，没有收缩阈值或确认开关；`logSchemaSyncDeletion` 是唯一痕迹 | `backend/runner/schemasync/syncer.go:755`、`backend/runner/schemasync/syncer.go:1020` | 有意为之：MySQL 的 `information_schema` 只列连接用户可见的对象，权限变更可以清空快照。详见 `docs/security-posture.md`。 |
| `deleted = true` 可由任何“目标说这个库没了”的观测写入，`deleted = false` 只能由实例枚举写入；单库同步的成功路径不写 `deleted` | `backend/runner/schemasync/syncer.go:611-618`、`backend/runner/schemasync/syncer.go:800-810`、`backend/runner/schemasync/syncer.go:830` | 一次“读到了某个库的内容”不是一次实例枚举；写成双向就会 flap。详见 `docs/security-posture.md`。 |
| 每个 driver 打开点都经过实例连接限流：sync 走 `acquireInstanceConnection`，API 侧的连通性探测也调用 `stateCfg.AcquireInstanceConnection` | `backend/runner/schemasync/syncer.go:432`；`backend/api/v1/instance_service.go:632` | 绕过限流会让 `maximum_connections` 形同虚设，调用方可以对着一个只握手不响应的目标堆叠无界尝试（理由见 `backend/api/v1/instance_service.go:619-623`）。 |

## 已知问题

| # | 严重度 | 问题 | 位置 / 证据 |
| --- | --- | --- | --- |
| 1 | 中 | **API 触发的同步最长可挂 15 分钟。** `SyncInstance`/`SyncDatabase` 都在请求 goroutine 里同步执行，内部 deadline 是 `syncTimeout = 15 * time.Minute`；目标库 hang 时 HTTP 请求就挂满 15 分钟。入口为 `backend/api/v1/instance_service.go:349`（`SyncInstance`）与 `backend/api/v1/database_service.go:45`（`SyncDatabase` → `SyncDatabaseForUser`，后者在 `backend/runner/schemasync/operation.go:297` 直接调用 `SyncDatabaseSchema`）。建议 API 路径用更短超时，或改异步 + 轮询。 | `backend/runner/schemasync/syncer.go:32`（常量）、`:454`（`GetInstanceMeta`）、`:607`（`SyncDatabaseSchema`） |
| 2 | 低 | checker 每 10s 无条件 `ListInstances`，队列为空时纯浪费；可先 `Range` 判空再查。 | `backend/runner/schemasync/syncer.go:243` |
| 3 | 低 | 血缘清理是逐对象 2 条 DELETE（2N 次往返），可合并成基于 `= ANY($1)` 的两条批量语句。 | 循环 `backend/runner/schemasync/syncer.go:760-764`；语句 `backend/runner/schemasync/syncer.go:1076-1092` |
| 4 | 低 | 7 段近乎相同的类型循环可表驱动化；其中物化视图那一段未复用 `schemaGUIDPrefix`，与邻居不一致。 | `backend/runner/schemasync/syncer.go:639-708`，不一致处在 `:663` |
| 5 | 低 | 每列一次多余的 `proto.Clone`：`CalcMetaHash` 内部已经 Clone，之后无可变路径。 | `backend/runner/schemasync/syncer.go:993` |
| 6 | 低 | 降级模式可能让 DDL 永久过期：schema-bearing 对象超过 2000 时只刷新变更对象，而 StarRocks 的属性变更不动元数据哈希，这些对象不会被补刷。注释已承认，可考虑轮换批次补刷。 | `backend/runner/schemasync/object_definition.go:107-121`（阈值 `:31`，注释 `:26-30`） |

## 历史债务（顺手清理，非 bug）

| 项 | 位置 |
| --- | --- |
| `MaximumOutstanding` 导出但仅包内使用且无 doc，应为 `maximumOutstanding`。 | `backend/runner/schemasync/syncer.go:35` |
| `Run` 的注释仍写 “will run the schema syncer once”，实际是永久循环。 | `backend/runner/schemasync/syncer.go:215` |
| 两处死逻辑：`database := database` 是 Go 1.22 之前的写法，现已冗余（同函数的 instance 循环就没写）；`if instanceMeta.Version != …` 与无条件赋值等价（`metadata` 是 `instance.Metadata` 的克隆，`UpdateInstance` 反正都会调用）。 | `backend/runner/schemasync/syncer.go:385`；`backend/runner/schemasync/syncer.go:487-489` |
| deadline 写法与预算：`context.WithDeadline(time.Now().Add(syncTimeout))` 可简化为 `WithTimeout`；同一个 deadlineCtx 同时覆盖 `SyncDBSchema` 与 DDL 抓取，前者慢会挤压后者（失败保留旧 DDL，不致错，但值得知晓）。 | `backend/runner/schemasync/syncer.go:454,607`；`backend/runner/schemasync/syncer.go:607,609,731` |
| `SyncInstance` 返回 4 元组而调用方大量丢弃，签名演进后没收敛。 | `backend/runner/schemasync/syncer.go:471`；`backend/api/v1/instance_service.go:362` 是 `_, _, _, _` |
| `supportsObjectDefinition` 预留了 FUNCTION/PROCEDURE，但目前只有 StarRocks 一个驱动实现了 `ObjectDefinitionReader`。 | `backend/runner/schemasync/object_definition.go:124-134`；`backend/plugin/db/starrocks/definition.go:20` |
| 多处 “used to … / instead of …” 修复叙事型注释：对理解有帮助，但也说明这是事故驱动的热点区域——每次修 bug 都在旧结构上叠加而非重构。 | `backend/runner/schemasync/`，例如 `backend/runner/schemasync/syncer.go:501-502`、`backend/runner/schemasync/operation.go:20` |

## 测试与门禁

- 单测（hermetic）：`backend/runner/schemasync/syncer_test.go`（digest diff、限流、gate 串行、退避阶梯、`shouldSyncNow` 边界）、`backend/runner/schemasync/object_definition_test.go`、`backend/runner/schemasync/operation_test.go`。
- 集成：`make test-integration-smoke`、`make test-integration`、`make test-integration-mysql`（见 `backend/test/integration/README.md`）。
- 门禁：`gofmt`、`golangci-lint run --allow-parallel-runners`、`go test ./backend/runner/schemasync/...`；改动触及 schema sync 时补集成套件。
