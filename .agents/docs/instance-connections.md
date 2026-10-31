# 实例连接调度（Instance Connections）— Reference

> Status: **implemented**。维护参考，覆盖 `backend/component/state/connection.go`，以及所有打开目标库 driver 的调用方：`backend/runner/schemasync`、`backend/runner/qualityrunner`、`backend/api/v1` 的连通性测试、规则干跑探测与字段画像。
> 关联：[data-quality.md](data-quality.md)（内部批次级准入的消费方）、[schemasync.md](schemasync.md)、[runner.md](runner.md)（单副本与进程内状态）、[rate-limiting.md](rate-limiting.md)（请求预算，与本文件是两套独立机制）。规则：[backend/AGENTS.md](../../backend/AGENTS.md)。

## 0. 它是什么

`maximum_connections`（实例元数据字段，默认 `common.DefaultInstanceMaximumConnections = 10`）是**每个实例的并发连接预算**。任何打开目标库 driver 的路径都先向 governor 领取一个 lease，并在 driver 关闭后释放。

一个 lease 的语义是**一段占用该实例连接预算的窗口**：holder 在窗口内打开 driver、顺序执行语句、关闭 driver。普通调用方一个 lease 对应一次会话；DQ 内部批次的 lease 覆盖整个查询阶段（批次的执行串行，任意时刻至多一个会话），在问题迁移与通知之前释放。因此预算约束的是同时打开的目标库会话数，而不是 driver 连接池内部的物理连接数——前提是 holder 不并发使用自己的 driver（§2 不变量 I-2）。

governor 不是计数器，而是**公平、带优先级、可阻塞的队列**：拿不到 slot 的请求排队等待，release 时把 slot 直接交给队首，而不是让调用方立即拿到一个错误。

## 1. 为什么（这不是换一个错误码的问题）

旧实现 `AcquireInstanceConnection` 是 try-lock：拿不到就立刻返回 `ErrInstanceConnectionLimit`。同一个正常的后台竞争被三个调用方解释成了三种东西：

| 调用方 | 旧行为 | 问题 |
| --- | --- | --- |
| schemasync | 把库放回队列，下个 tick 重试 | 语义正确，但每个 tick 最多 100 个 goroutine 抢 10 个 slot，绝大多数直接失败重排 |
| 连通性测试 | `ResourceExhausted` | 可接受，但没有等待 |
| qualityrunner | **每条规则写一条 `ERROR(QUOTA)` 结果，并给管理员发执行失败通知** | 把竞争记录成规则故障；且目标的 `next_run_at` 已推进，这次检查直接丢失 |

竞争是结构性的：database checker 每 10s 为每个待同步库起一个 goroutine（全局上限 100），每个都在实例的 10 个 slot 上 try-acquire；定时/手动 DQ 批次与 UI 的测试连接又在同一预算上竞争。没有等待队列时，失败者只能报错。

结论：**容量竞争必须表现为等待或延后，而不是结果。** 这是 governor 存在的唯一理由。

## 2. 契约

### 2.1 API

| API | 行为 |
| --- | --- |
| `AcquireInstanceConnection(ctx, InstanceConnectionRequest)` | 立即准入，或按优先级排队；ctx 结束时返回 `ErrInstanceConnectionLimit`（同时 wrap ctx 的 cause）。返回的 release 幂等 |
| `TryAcquireInstanceConnection(request)` | 仅在能立即准入**且该实例没有排队者**时成功；给“可以稍后重试”的调用方用，不允许插队 |

`InstanceConnectionRequest` 的 `MaximumConnections <= 0` 表示实例没有设置上限：请求立即准入，但仍然计数（诊断用）。调用方传入的 `MaximumConnections` 一律按实例行上的值或部署默认值解析（见各调用方）。

### 2.2 优先级与公平性

| 优先级 | 用在 |
| --- | --- |
| `ConnectionPriorityInteractive` | 用户同步等待结果的 API 动作：测试连接、规则干跑探测、字段画像 |
| `ConnectionPriorityManual` | 用户触发但不同步等待的手动 DQ 批次 |
| `ConnectionPriorityBackground` | 周期任务（schemasync 的库同步、定时 DQ 批次） |

- release 后 slot 交给优先级最高者，同级按到达顺序（FIFO）。
- 实例有排队者时 `TryAcquire` 一定失败：后台批量重试不能饿死已经在等待的请求。
- 空闲 slot 直接移交给队首，不做“唤醒所有等待者再抢”的惊群。

### 2.3 不变量

| # | 不变量 | 守卫 |
| --- | --- | --- |
| I-1 | 每个目标库 driver 打开点都经 governor：schemasync 的实例/库同步、连通性测试、DQ 内部批次、规则干跑探测、字段画像；DQ 规则在准入后目标被改到别的实例时，由 `ruleConnectionLease` 为实际实例补领 lease（只尝试一次，拿不到就 `SKIPPED`，绝不在持有批次 lease 时阻塞另一个实例），绝不在批次 lease 之外打开 driver | 各调用方的 `acquire*` 是唯一入口；`TestRuleConnectionLeaseCoversARetargetedRule` |
| I-2 | 一个 lease 的 holder 不并发使用自己的 driver（一条 DQ 执行一次查询；一个库一次同步会话），所以预算≈并发物理连接上限 | 代码结构；新调用方必须满足 |
| I-3 | release 幂等；计数不越界；取消的等待者从队列移除且不占 slot；等待中被授予 slot 后又取消的，slot 归还队列 | `backend/component/state/connection_test.go` |
| I-4 | 等待者优先于 try-acquirer；`TryAcquire` 永不越过排队者 | `connection_test.go` |
| I-5 | governor 状态在进程内，单副本假设（见 [runner.md](runner.md)） | 代码结构 |

## 3. 调用方与等待预算

| 调用方 | API | 优先级 | 等待预算 | 拿不到时 |
| --- | --- | --- | --- | --- |
| schemasync database checker 的库同步 | `TryAcquire` | Background | 0 | 库保持排队，下个 tick 重试；不算失败、不推进退避 |
| 定时 DQ 批次 | `Acquire` | Background | 2m（`scheduledAdmissionWait`） | 批次与执行行都不创建、不写结果、不通知；目标保持 due；同实例本轮剩余的 due 目标也留到下个 tick |
| 手动 DQ 批次 | `Acquire` | Manual | 5m（`manualAdmissionWait`） | 已规划的 PENDING 执行行（连同内部批次）标 `ABANDONED` 并记日志 |
| 连通性测试 | `Acquire` | Interactive | 5s（`dataSourceConnectionWait`） | `ResourceExhausted`：实例连接忙，稍后重试 |
| 规则干跑探测（`PreviewQualityRule` 的 `LIMIT 0`） | `Acquire` | Interactive | 3s（`qualityPreviewConnectionWait`） | `ResourceExhausted`：稍后重试干跑 |
| 字段画像（`ProfileColumns`） | `Acquire` | Interactive | 10s（`qualityProfileConnectionWait`） | `ResourceExhausted`：稍后重试画像 |

### 3.1 为什么 DQ 按批次领 lease

一个批次的执行对同一实例串行执行，任意时刻至多一个会话，所以一个批次只需要一个 lease；在创建批次、规划执行之前一次拿到，批次中途就不会再出现准入失败，`QUOTA` 也就不可能成为执行结果。由此：

- 同一实例的批次天然串行；`maxConcurrentInstanceBatches` 只约束不同实例。
- 拿不到 lease 的定时批次不会留下任何行：不建批次、不规划执行，目标的 `next_run_at` 本来就未推进；同一实例本轮剩余的 due 目标也不再尝试，统一留到下个 tick，避免每个目标各等一次 2 分钟。
- lease 覆盖查询与每条执行的终态写入；执行循环一结束就释放，问题迁移与通知都不再持有 slot。
- 规则在准入后改到别的实例时，`ruleConnectionLease` 只对目标实例 try-acquire 一次：拿不到就记 `SKIPPED`（下个排期再跑），而不是在持有当前批次 lease 的情况下阻塞等待，避免两个并发批次互相等待对方的实例。
- 手动批次的准入等待发生在 runner 主循环里（批次本就是串行执行），因此等待期间到期扫描会暂停；这是“用户请求优先 + 主循环简单”的取舍。
- 元数据型规则（`schema_drift`）不查目标库，但同批次的其余执行可能要查，所以批次仍占一个 slot；这是有意的简化，最多占该实例预算的 1/N，持续一个批次的时间。预算持续被占满时，只含元数据规则的手动批次也可能在 5 分钟后被标 `ABANDONED`（定时批次只是延后）。

### 3.2 已知缺口：API 触发的实例/单库同步

`SyncInstance` 与 `SyncDatabase` 在请求 goroutine 里同步执行，拿不到 slot 时仍返回错误，并写一条持久的操作失败通知（operation 的 ERROR 消息）。把这条路径改成排队需要把 API 同步异步化（[schemasync.md](schemasync.md) 的已知问题 1 已记录请求最长可挂 15 分钟），不在本次范围。本文件保证的是 database checker 的排队路径、DQ 批次，以及所有由本文件接入的 API 只读查询。

## 4. 为什么没有持久化队列

- 部署是单副本（[runner.md](runner.md)）；`quality_execution` 行本身就是手动运行的持久记录：排队期间是 `PENDING`，执行页可见。`quality_run` 是只读的派发批次（`QualityRun`），不是写入路径；手动队列在进程内，容量 512。进程重启后由启动清扫把 15 分钟前的 `PENDING/RUNNING` 执行与批次标 `ABANDONED`；scheduled 检查按目标周期重试预算补跑，手动执行不补跑。
- 定时批次的“队列”就是目标的 `quality_target_schedule.next_run_at`：拿不到 lease 时目标保持 due，下一个 10s tick 自然重试。
- 多副本需要数据库 lease 或跨进程队列，属于明确非目标。

## 5. 失败模式

| 场景 | 行为 |
| --- | --- |
| 实例预算被 10 个库同步占满，定时 DQ 批次到期 | 批次在队列里等（≤2m）；有 slot 释放就立刻开始，否则本 tick 放弃、目标保持 due，同实例其余目标也留到下一 tick |
| 同上，但用户手动触发 | 执行行先 `PENDING` 排队（≤5m）；超时执行与批次标 `ABANDONED`，用户可在执行页看到 |
| 用户点“测试连接”时预算被占满 | 排队 ≤5s；仍拿不到返回 `ResourceExhausted`，不打开未计量的连接 |
| 等待期间服务关闭 | ctx 取消，等待者出队；已规划的 PENDING 手动执行与批次由 `abandonUnadmittedBatch` 用 detached ctx 写盘收尾。若进程被强杀，残留的 PENDING 执行要等到某次启动时已超过 15 分钟才被清扫（启动清扫的 cutoff 是 15 分钟，故意避开刚启动时 API 新建的行） |
| release 被调用两次 | 第二次无操作（`sync.Once`），不会释放别人的 slot |
| 实例被删除 / metadata 为空 | 规则解析为 `SKIPPED(ORPHANED)`，不打开连接，因此 DQ 不为它领 lease |
| 等待者很多 | 按优先级 + FIFO 服务；后台 `TryAcquire` 让位，不会饿死等待者 |

## 6. 测试与门禁

- `backend/component/state/connection_test.go`：共享预算、阻塞等待、等待预算、取消出队、优先级/FIFO、`TryAcquire` 不插队、release 幂等、无上限、已取消的 ctx 不占 slot。
- `backend/runner/qualityrunner/qualityrunner_test.go`：批次级 lease 持有/等待、预算持续占用时延后、缺失实例不领 lease、非正数上限取部署默认值。
- `backend/runner/schemasync/syncer_test.go`：`TryAcquire` 语义与 `errInstanceConnectionsExhausted`。
- 门禁：`gofmt`、`golangci-lint run --allow-parallel-runners`、`go test ./backend/...`（hermetic）。

## 7. Where things live

| 面 | 位置 |
| --- | --- |
| governor / 优先级 / 队列 | `backend/component/state/connection.go` |
| 状态持有与构造 | `backend/component/state/state.go`（`State.instanceConnections`） |
| DQ 批次准入 | `backend/runner/qualityrunner/admission.go`（`admitBatch`、`instanceForAdmission`、`instanceConnectionLimit`、`abandonUnadmittedBatch`、`abandonBatch`） |
| DQ 规则级兜底 | 同上（`ruleConnectionLease`：目标在准入后被改到别的实例时补领） |
| schemasync 准入 | `backend/runner/schemasync/instance_sync.go`（`acquireInstanceConnection` → `TryAcquire`） |
| 连通性测试准入 | `backend/api/v1/instance_service.go`（`pingDataSource`） |
| 干跑/画像准入 | `backend/api/v1/data_quality_service.go`（`acquireQualityConnection`，供 `probeQualityRule` 与 `qualityReadOnlyQueryer` 使用） |
| 测试 | `backend/component/state/connection_test.go`、`backend/runner/qualityrunner/qualityrunner_test.go`、`backend/runner/schemasync/syncer_test.go` |
