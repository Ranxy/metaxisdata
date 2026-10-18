# backend/runner 整体评审：架构、实现与技术债

> **状态**：一次性评审快照（不是维护参考，不描述已发布行为）。基线 commit `387e1d1`，评审日期 2026-10-18，在 worktree 分支 `review-backend-runner` 中完成；**未修改任何产品代码**。
>
> **范围**：`backend/runner/` 四个子包（`schemasync`、`lineageanalyzer`、`lineagevalidation`、`maintenance`），以及它们与 `backend/server`、`backend/store`、`backend/api/v1`、`backend/plugin/{lineage,openlineage,db}` 的接缝。
>
> **方法**：三名评审并行（按包分工）→ 独立审稿人逐条反向核实 → Lead 读完全部 runner 源码、调用方、相关 store 方法与文档后合并去重。结论中的行号均对应当前 worktree 的真实行号；每条发现附「来源」与「核实」状态。
>
> **已并入的决策（2026-10-18）**：单副本部署；`SyncInstance` 保持同步执行；统计字段异步刷新但不入历史；无历史兼容负担、可全量重算。四项决策对建议的影响见 §11.3。
>
> **已接受的决策不在本文重复上报**（见 §9）：`docs/security-posture.md` 与 `.agents/docs/backend-open-items.md` 中明确接受的行为，本文只说明它们与 runner 的关系。

---

## 0. 结论摘要

### 0.1 总体判断

四个 runner 的**设计意图清晰、代码质量高于仓库平均水平**：schema 同步把网络抓取严格移出写事务、按库 singleflight、失败退避与通知去重、DDL 抓取失败不擦除已有 DDL；血缘分析把「完整 / 部分分析 / 硬失败」三档语义、metahash 变更检测、对象级 panic 隔离落到了实现和测试里；维护任务逐项隔离、删除语句幂等、OpenLineage 聚合的 grace 与锁序处理是对的。

真正的问题不在「某段代码写得烂」，而在三条**结构性缺口**：

1. **派生数据的写入没有并发/版本契约**：血缘分析「读定义 → 解析（秒级、含 N+1 查询）→ 覆盖写边 + version」没有 CAS；而 schema 同步会在另一个事务里删除同一对象。窗口内会留下孤儿血缘或复活已删对象；升级/回滚分析器后也不会重算（没有 analyzer 版本维度）。
2. **runner 同时是 domain service，生命周期没有统一抽象**：`schemasync` 被 API 同步调用、又被后台调度，但只有「单库」有 gate，实例级没有；四个 `Run` 各自实现 ticker/panic/超时，`Add(4)` 硬编码；进程内队列/operation 假设单副本（已确认），但该假设没有集中声明。
3. **实例元数据是整份 JSONB 覆写**：`SyncInstance` 持有一份可能已过期的克隆，在长时间网络调用后整份写回，会静默覆盖并发的 API 修改（数据源增删、标题、激活、同步间隔）。这是本次评审发现的**最高危数据丢失问题**，且与 runner 的调度方式直接相关。

### 0.2 最该先处理的事（P0/P1）

> **严重度与优先级是两件事。** 本文的「优先级」按「影响 × 触发条件 × 修复成本」排；「影响」列才是单次触发时的后果级别。独立审稿人把分报告 24 条 High/Medium 中的 21 条降了级（机制大多成立，但触发条件苛刻、或属已登记债务、或没有现存的运行期缺陷）；本文接受大部分校准，只对 **R-01 / R-02** 保留「先修」优先级——它们是**静默数据丢失 / 永久功能退化**，且修复局部、成本低。

| 优先级 | ID | 问题 | 一句话影响 |
| --- | --- | --- | --- |
| **P0** | **R-01** | `SyncInstance` 克隆 `instance.Metadata` 后整份 `UpdateInstance`（`store.UpdateInstance` 写整个 JSONB），与 API 的数据源/属性更新构成 lost update | 同步窗口内用户刚添加的数据源/凭据、刚改的激活或同步间隔，可能被一次后台同步静默回滚 |
| **P0** | **R-02** | `buildSQL` 的引擎引用表漏了 MariaDB/StarRocks/MSSQL；`isCompleteStatement` 只认严格 `"CREATE "` 前缀 | 对象名是保留字/含空格，或完整 DDL 带前导注释时，解析失败 → **清空血缘并写入当前 hash** → 小时扫描不再重排 = 永久丢血缘（已用 overlay 测试在 MariaDB/StarRocks 上实测复现） |
| **P1** | R-03 + R-04 | 分析写入没有 CAS；失败路径「清边 / 写 version」跨两个事务，`storeError` 还把 hash 抹成 NULL | sync 删除与在途分析交错时留下永久孤儿血缘；持久性错误让「放弃重试」失效，形成每小时错误循环 |
| **P1** | R-17 | 变更检测只看 metahash，没有 analyzer/规则版本 | 分析器升级/回滚后既有对象不重算；回归被写成「已分析」后回滚也无法自愈 |
| **P1** | R-05 + R-06 + R-08 | `SyncInstance` 无实例级 gate；`LastSyncTime` 早于枚举提交；全量同步吞错并报 INFO 成功 | 并发实例同步的 deleted/LastSyncTime 抖动；半途失败要等一个 interval；用户收到虚假成功 |
| **P1** | R-31 | `BatchSyncInstances` 在请求 goroutine 内串行执行最多 1000 次 `SyncInstance`（各最长 15 分钟），Connect 无 per-RPC deadline | 一个批请求可在服务端挂数小时并占满连接槽（`schemasync.md` 已知问题 1 只覆盖单实例）；已确认保持同步执行，因此加 RPC 超时/批次预算，不转异步 |
| **P1** | R-14 + R-15 | 应用上报路径无变更检测；分析器逐 GUID N+1 查类型 | 高频上报 + 大对象时 DB 写放大/读放大 |
| **P1** | R-19 | 测试缺口；`lineagevalidation` 的 settle 窗口 trigger 未合并（注释与实现不符） | 关键写路径/调度无 hermetic 测试；该测试依赖调度时机，review 中观测到一次失败（未稳定复现） |

### 0.3 风险清单（按优先级排序）

| ID | 优先级 | 影响 | 触发条件 | 位置 | 核实 |
| --- | --- | --- | --- | --- | --- |
| R-01 | **P0** | 高（静默配置/凭据回滚） | 同步窗口内有并发实例编辑 | `runner/schemasync/syncer.go:471-493`；`store/instance.go:148-188` | Confirmed（机制）；三份分报告均漏，Lead 补 |
| R-02 | **P0** | 高（永久丢血缘） | MariaDB/StarRocks 保留字名；完整 DDL 带前导注释 | `runner/lineageanalyzer/analyzer.go:443-501` | Confirmed；overlay 实测解析失败 |
| R-03 | P1 | 中（孤儿血缘） | 删除恰逢分析（秒级窗口） | `runner/lineageanalyzer/analyzer.go:261-440` | Confirmed（机制） |
| R-04 | P1 | 中（状态不自洽/重试循环） | 第二次写失败；持久性 store 错误 | `runner/lineageanalyzer/analyzer.go:503-550` | Confirmed |
| R-05 | P1 | 中（deleted 抖动/写竞争） | API 与后台同时同步同一实例 | `runner/schemasync/syncer.go:64-142,471` | Confirmed |
| R-06 | P1 | 中（新库延迟发现） | 枚举/建库/软删除失败 | `runner/schemasync/syncer.go:483-553` | Confirmed |
| R-07 | P2 | 中（通知风暴） | 坏实例 + 多库 | `runner/schemasync/syncer.go:355-396` | Confirmed |
| R-08 | P1 | 中（虚假成功） | `ListDatabases` 失败 | `runner/schemasync/syncer.go:403-415`；`operation.go:281-284` | Confirmed |
| R-09 | P2 | 中低（UI 统计永久陈旧） | 必然（任何统计变化） | `runner/schemasync/syncer.go:917-936`；`store/meta_hash.go:62-80` | Confirmed；仅前端渲染（无后端读者） |
| R-10 | P2 | 中低（事务放大） | 权限收缩清空快照 + 大库 | `runner/schemasync/syncer.go:757-764,1076-1092` | Confirmed；与 `schemasync.md` 已知问题 3 合并 |
| R-11 | P2 | 中（防御性：panic 崩进程） | 未来引入可 panic 路径 | `server/server.go:151-155`；四个 `Run` | Confirmed；当前无可触发路径 |
| R-12 | P3 | 文档债（单副本已确认） | — | 四个 runner + `server.go` | 决策：单副本，不做 lease（2026-10-18） |
| R-13 | P2 | 低中（shutdown 延迟/噪音） | 维护 pass 持锁 + 关闭 | `lineagevalidation/validation.go:99-113`；`plugin/openlineage/lineage_revalidation.go:78-80` | Confirmed（锁不响应 ctx） |
| R-14 | P1 | 中（写放大） | 应用高频重复上报 | `api/v1/application_sql_handler.go:331-333` | Confirmed（频率取决于上报节奏） |
| R-15 | P2 | 中低（读放大） | 单对象引用数百表 | `runner/lineageanalyzer/analyzer.go:408-421` | Confirmed |
| R-16 | P2 | 中（backfill 阻断事件） | 冷启动/大 backlog | `runner/lineageanalyzer/analyzer.go:189-238` | Confirmed |
| R-17 | P1 | 中高（升级不生效/永久跳过） | 分析器/规则升级或回滚 | `runner/lineageanalyzer/analyzer.go:178-184`；`LATEST.sql:324-331` | Confirmed（设计缺口） |
| R-18 | P2 | 中低（清理停摆不可见） | 慢 pass / setting 瞬时错误 | `runner/maintenance/maintenance.go:52-158` | Confirmed；零测试与 E12 已登记重合 |
| R-19 | P2 | 测试/维护性 | 调度时机；未来回归 | `lineagevalidation/validation_test.go:92-110` | 设计缺陷 Confirmed；flake 观测 1 次、未稳定复现 |
| R-20 | P2 | 中（目标库压力） | 大 StarRocks/Doris 库 | `runner/schemasync/syncer.go:729-735`；`object_definition.go:21-33,100-122` | Confirmed；降级模式为文档已知问题 6 |
| R-21 | P3 | 低（一次性误导消息） | handler 提前退出/panic | `runner/schemasync/operation.go:104-250` | Confirmed |
| R-22 | P3 | 低（无效同步） | 队列中库被软删除 | `runner/schemasync/syncer.go:167-174,544-553` | Confirmed |
| R-23 | P3 | 低（语义失真） | 目标库消失 | `runner/schemasync/syncer.go:610-620` | Confirmed |
| R-24 | P3 | 低（排障信息丢失） | 软删除失败 | `runner/schemasync/syncer.go:544-553` | Confirmed |
| R-25 | P3 | 低（极端命名下整库同步失败） | 名字含字面量 `%3B`/`;` | `common/guid.go:16-38` | Confirmed（待真实数据触发） |
| R-26 | P3 | 低（过期血缘仍展示） | 定义变空 | `runner/lineageanalyzer/analyzer.go:276-279` | Confirmed |
| R-27 | P3 | 低（回滚时日志失实） | 事务回滚 | `runner/schemasync/syncer.go:755,766` | Confirmed |
| R-28 | P3 | 低（误报通知） | 实例连接限流饱和 | `runner/schemasync/syncer.go:302-309,355-369` | Confirmed |
| R-29 | P2 | 中（重复边，仅同进程竞态） | OPENLINEAGE run 重投撞同进程 revalidation | `LATEST.sql:304-321`；`store/column_lineage.go:64-84` | 机制确认；多副本路径已按决策删除 |
| R-30 | Info | — | — | 见 §6 | — |
| R-31 | P1 | 中高（请求挂起/资源占用） | 批量同步 + 目标 hang | `api/v1/instance_service.go:29,375-425`；`server/grpc_routes.go` | Confirmed（审稿人补） |
| R-32 | P2 | 中低（重启后延迟恢复） | 重启时存在未决 ingested 边 | `lineagevalidation/validation.go:72-94` | Confirmed（审稿人补） |
| R-33 | P3 | 低（到期判定偏移） | 实例同步耗时接近 interval | `runner/schemasync/syncer.go:354,372,391` | Confirmed（审稿人补） |

### 0.4 治理路线图

| 阶段 | 内容 | 预估 |
| --- | --- | --- |
| **第 1 步（止血）** | R-01 实例元数据改为「带行锁的定向 patch」（`MetadataUpdates` 模式，与 `store.UpdateDatabase` 对齐），`SyncInstance` 只写 `last_sync_time` / `version`；R-02 补全方言引用表 + 注释感知的完整 DDL 判定，并加「每个已注册 engine 的 wrap 输出可被自己的分析器解析」回归测试 | 1–2 人日 |
| **第 2 步（一致性）** | R-03/R-04/R-17：把「替换边 + 写 version」做成带 `expectedMetaHash` 前置条件的单事务；`markAnalysisFailed` 原子化；`storeError` 不再抹 hash；`column_lineage_version` 增加 `analyzer_version` 并让 `queueAll` 比较它（**已确认无历史兼容负担**：一次增量迁移 + 部署后小时扫描自动全量重算，无需回填） | 3–5 人日（含迁移） |
| **第 3 步（生命周期）** | R-05/R-06/R-08/R-31/R-11：实例级 gate、LastSyncTime 后移、错误上报、批量 API 的 per-RPC deadline/预算（**保持 SyncInstance 同步**，不转异步）、统一 Runner 骨架（errgroup + recover + jitter + per-run timeout）；R-12 只做文档（单副本已确认，不做 lease） | 3–5 人日 |
| **第 4 步（性能/可观测）** | R-09/R-10/R-13/R-14/R-15/R-16/R-18/R-20/R-29/R-32/R-33：统计异步刷新（只更新当前 metadata，**不写 history、不计入血缘变更**）、批量清理、可取消锁、事件路径变更检测、批量类型解析、批次上限与 per-object 超时、maintenance 注入与摘要日志、DDL 刷新节奏、血缘唯一约束、validation 启动首轮 | 5–8 人日 |
| **第 5 步（顺手）** | R-21～R-29、R-30 与测试补齐 | 随迭代 |

> 不建议「大重写」。当前事务纪律、singleflight、operation 聚合与测试选点都值得保留；按上面的顺序做**局部替换 + 契约加固**收益最高。

---

## 1. 范围、基线、核实方式

| 项 | 内容 |
| --- | --- |
| 基线 | `387e1d1`（`fix(openlineage): derive dataset column-lineage support from stored edges (#56)`） |
| Worktree | `.agents/worktrees/review-backend-runner`，分支 `review-backend-runner`；产品代码零改动（`git status` 干净） |
| 源码 | `backend/runner/{schemasync,lineageanalyzer,lineagevalidation,maintenance}` 共 4,322 行（含测试），其中非测试 2,891 行 |
| 顺读的接缝 | `backend/server/server.go`、`backend/api/v1/{instance_service,database_service,application_sql_handler,database_manual_sql,lineage_service_analyze,openlineage_handler}.go`、`backend/store/{meta_resource,meta_resource_schema,meta_resource_history,column_lineage,database,instance,maintenance,application_sql,meta_hash}.go`、`backend/plugin/openlineage/`、`backend/plugin/lineage/{lineage,engines,catalog,model}`、`backend/plugin/db/starrocks/definition.go`、`backend/component/{state,notification,dbfactory,llm}` |
| 文档 | `AGENTS.md`、`backend/AGENTS.md`、`.agents/docs/{schemasync,lineage-analyzer,application-sql-ingestion,backend-open-items}.md`、`docs/security-posture.md` |
| 实际执行 | `go build ./backend/...`、`go test ./backend/runner/...`、`go test -race ./backend/runner/...`、`go vet ./backend/runner/... ./backend/server/...`（均通过）；`go test -race -count=10 ./backend/runner/...` 观测到 1 次 `TestTriggerCoalescesOneSyncRound` 失败；独立审稿人另用 `go test -overlay` 注入临时测试验证 MariaDB/StarRocks 解析失败、并重复 610+ 次 `-race` 运行（该用例 0 失败） |
| 核实 | 三份分报告（`/tmp/runner-review/findings-{arch,schemasync,lineage}.md`，共 54 条原始发现）经独立审稿人逐条核对（`verification.md`：Confirmed 44 / PartlyWrong 8 / Unverifiable 2 / FalsePositive 0），合并、去重、修正 4 处机制/消费者描述后得到本文 33 条发现 |

### 代码规模与覆盖

| 包 | 非测试行 | 测试行 | `go test -cover` | 评价 |
| --- | --- | --- | --- | --- |
| `schemasync` | 1,751 | 1,132 | 42.3% | 纯函数/状态机覆盖好；写路径（事务、DDL、血缘删除）全靠集成测试 |
| `lineageanalyzer` | 619 | 345 | 24.4% | 只有 `buildSQL`/退避/分类有测试；`Run`/`queueAll`/`analyzeObject` 无测试 |
| `lineagevalidation` | 113 | 204 | 95.5% | 覆盖好，但有一个 fragile-by-design 用例（R-19） |
| `maintenance` | 158 | 0 | 0% | 零测试，且结构上不可注入 |

---

## 2. 架构评估

### 2.1 现状结构

```text
                        ┌────────────────────────── server.NewServer ──────────────────────────┐
                        │ runnerCtx (cancel on shutdown)                                         │
                        │                                                                        │
  周期/事件触发          │   lineageanalyzer.Analyzer ──┐                                          │
  ───────────────►      │   schemasync.Syncer ─────────┼── 共享 *store.Store                     │
  API 同步调用 ────────►│   lineagevalidation.Runner ──┤                                          │
  API 手动同步 ────────►│   maintenance.Runner ────────┘                                          │
                        └────────────────────────────────────────────────────────────────────────┘
                                  │                         │                      │
                        *lineage.Analyzer           *openlineage.Processor    *notification.Service
                        （编译期装配的方言值）        （revalidation 共享锁）     （inbox）
```

- **四个 runner 都是「ticker/事件 + ctx + WaitGroup」**，没有共同接口；启动点是一段手写四行（`server.go:151-155`），`Add(4)` 与实际 goroutine 数量手工同步。
- **两条执行路径并存**：后台周期任务（15m/1h/6h）与事件驱动（sync 提交后入队分析、应用上报入队、sync 后 revalidation trigger）。事件路径让「尽快生效」成立，但也带来了「事件路径没有变更检测」（R-14）与「事件路径覆盖失败退避」（R-04）两类偏差。
- **`schemasync` 同时是 domain service**：API 的 `CreateInstance`/`SyncInstance`/`BatchSyncInstances` 直接在请求 goroutine 里调用 `SyncInstance`（最长 15 分钟，见 `.agents/docs/schemasync.md` 已知问题 1），并持有 `SyncOperation` 聚合模型。包名 `runner/` 名不副实，也让「谁是同步语义的唯一所有者」变得含糊。
- **依赖方向**：`schemasync → lineageanalyzer`（具体类型）；`api/v1 → schemasync`（同步服务 + 分析队列门面）；`maintenance → store`（具体类型）；`lineagevalidation → openlineage.Processor`（具体类型）。同文件里对 revalidator/notifier 已经用了本地窄接口，说明团队知道怎么做依赖倒置，只是没有统一执行。

### 2.2 架构层面的六个判断

1. **职责划分基本合理，但 runner 层的横切关注点缺失**。每个 runner 自己实现首跑策略、ticker、panic 处理、超时、日志措辞；结果是行为不一致（sync 有 15m/DB 级超时，validation 有 5m pass 超时，maintenance 没有；analyzer 的 panic 在对象级被隔离，长循环体却没有）。应有一个薄 `runner` 骨架（见 §7.1），而不是重写业务。
2. **「派生数据 + 覆盖写」是本包的主数据流，但缺少并发/版本契约**。血缘边、DDL 行、version 行、operation 列表都是「先读后整体替换」。其中 DDL 与元数据在同一个事务里（正确），血缘边与 version 却跨事务、无 CAS、无版本（R-03/R-04/R-17）。
3. **进程内状态是单副本部署的直接结果，已确认接受（2026-10-18）**。`databaseSyncGate`、`databaseSyncMap`、`operation`、`analyzeMap`/`retryMap`、`revalidateMu`、settle channel 全部是进程内的；产品已确认单副本，因此不需要分布式 lease。缺口只剩两点：① `docs/security-posture.md` 没有把 runner 的进程内状态列入「one replica」清单（R-12，文档债）；② 同进程内 ingestion 的第二个 `openlineage.Processor` 不持 `revalidateMu`，仍可能与 revalidation 并发替换同一 run 的边（R-29），用唯一约束 + `ON CONFLICT DO NOTHING` 即可收敛。
4. **可观测性停留在「出错打日志」**。D8 已接受「同步失败无计数器/告警」，但没有被接受的空白是：maintenance 成功空跑无任何日志/耗时；analyzer 没有队列深度、backfill 进度、失败对象数；runner 没有「最近一次成功运行」的健康状态。运维只能从零散日志推断 6 小时一次、跨大表的破坏性清理是否发生。
5. **可测试性被具体类型锁死**。`schemasync.Syncer`/`maintenance.Runner` 直接持有 `*store.Store`，`lineageanalyzer.Analyzer` 直接持有 `*store.Store` + `*lineage.Analyzer`；`interval`/`now` 是包级常量或 `time.Now()`。纯函数与状态机因此测得好，涉及写路径/调度的部分只能靠昂贵、环境相关的集成测试。
6. **扩展点已经暴露出「第二真相源」**：对象标识符的引用规则在 `buildSQL` 里维护了一份引擎表（R-02），而 lineage 包已有 `EngineRegistration`（含 `FoldIdentifierNames` 等引擎事实）；「GUID → MetaType 补全」在 runner 与 `AnalyzeSQL` 各有一份（R-15）；「临时表判定」在 runner 与 handler 各有一份（§6 Info）。每新增一个引擎/对象类型，都要记得改多处。

### 2.3 生命周期与时序（当前）

```text
NewServer:
  store → migrator → lineage.Analyzer → Analyzer → State → openlineage.Processor
  → lineagevalidation.Runner / maintenance.Runner → notifier → schemasync.Syncer
  → gRPC routes → [go Run x4] (Add(4))  ← 启动即各跑一次首轮

Shutdown:
  runnerCancel() → notifier.Close() → echo.Shutdown(10s) → wait runnerWG(10s) → store.Close()
```

问题点：`runnerWG.Add(4)` 与四个 `go` 手工配对（R-11）；cancel 后只给 10s，卡在不可取消锁上的 validation pass 会让 `store.Close()` 与在途 goroutine 竞争（R-13）；四个 runner 启动即全量跑、无 jitter（R-12 的启动风暴）。

---

## 3. 问题清单（按主题）

> 编号 R-xx 为本文最终编号；「来源」列是分报告原始编号，便于追溯。

### 3.1 数据一致性与正确性

| ID | 级别 | 问题 | 来源 |
| --- | --- | --- | --- |
| R-01 | **P0** | 实例元数据整份覆写导致 lost update | Lead |
| R-02 | **P0** | 血缘标识符/DDL 形态判定错误 → 永久丢血缘 | LN-01+LN-08 |
| R-03 | P1 | 分析写入无 CAS → 孤儿血缘/version 复活 | LN-02 |
| R-04 | P1 | 失败状态写入不自洽；`storeError` 抹 hash | LN-07+LN-13 |
| R-09 | P2 | 表统计/sequence `last_value` 永不刷新（仅前端渲染） | SS-06 |
| R-17 | P1 | 无 analyzer 版本 → 升级/回滚不重算 | LN-10 |
| R-21 | P3 | operation 无 abort；expire 零计数 INFO | SS-10 |
| R-23 | P3 | 目标库消失被上报为成功 | SS-07 |
| R-25 | P3 | GUID 转义非单射 | SS-13 |
| R-26 | P3 | 空定义保留旧血缘且无说明 | LN-14 |
| R-29 | P2 | 同进程竞态下血缘边重复（无唯一约束） | LN-20+AR-01 |

### 3.2 并发、生命周期与可靠性

| ID | 级别 | 问题 | 来源 |
| --- | --- | --- | --- |
| R-05 | P1 | `SyncInstance` 无实例级串行化 | SS-02+AR-16 |
| R-06 | P1 | 实例 `LastSyncTime` 早于枚举提交 | SS-03 |
| R-07 | P2 | 实例失败仍入队其所有库 → 通知风暴 | SS-04 |
| R-11 | P2 | 长循环无 panic 隔离 + 无统一 Runner 抽象 | AR-03+SS-09+LN-16 |
| R-12 | P3 | 进程内状态假设未登记（单副本已确认，文档债） | AR-02+SS-08 |
| R-13 | P2 | validation 超时不能中断 `revalidateMu` | LN-09 |
| R-16 | P2 | 单批无上限、无 per-object 超时 | LN-06 |
| R-22 | P3 | 已软删除库仍留在队列 | SS-11 |
| R-28 | P3 | 连接限流饱和被当失败并发通知 | Lead |
| R-31 | P1 | 批量同步 API 串行执行且无 per-RPC deadline | 审稿人补 |
| R-32 | P2 | `lineagevalidation` 无启动首轮 pass | 审稿人补 |
| R-33 | P3 | `trySyncAll` 用过期 `now` 判断库到期 | 审稿人补 |

### 3.3 错误处理与可维护性

| ID | 级别 | 问题 | 来源 |
| --- | --- | --- | --- |
| R-08 | P1 | 全量同步吞错并报 INFO 成功 | SS-05 |
| R-19 | P2 | 测试缺口 + trigger 合并缺陷（测试脆弱） | Lead+LN-15 |
| R-24 | P3 | 软删除错误丢失原因 + 逐库事务 | SS-12 |
| R-27 | P3 | 删除日志先于 commit | SS-16(6) |
| R-30 | Info | 包边界/死代码/日志漂移/文档治理 | AR-08/AR-15/SS-15/SS-16/LN-17/LN-18/LN-21/LN-22 |

### 3.4 性能与扩展性

| ID | 级别 | 问题 | 来源 |
| --- | --- | --- | --- |
| R-10 | P2 | 删除清理 2×N 条 no-op DELETE 在写事务内（并入文档已知问题 3） | SS-01 |
| R-14 | P1 | 应用上报每次触发全量重分析 | LN-03 |
| R-15 | P2 | 逐 GUID N+1 查类型（绕过 LRU） | LN-05 |
| R-18 | P2 | maintenance 无超时/无可观测性 | AR-04/AR-05/AR-06/AR-09 |
| R-20 | P2 | 每轮全量抓 DDL；降级模式陈旧 | SS + 文档已知问题 6 |

---

## 4. 优先修复项详解（R-01～R-03）

### R-01 实例元数据整份覆写：`SyncInstance` 与 API 更新的 lost update

- **位置**：`backend/runner/schemasync/syncer.go:471-493`；`backend/store/instance.go:148-188`；API 写入点 `backend/api/v1/instance_service.go:241-282,296-307,500-508,536-562,583-606`
- **事实**：

```go
// syncer.go:476-490 —— 克隆一份可能在网络调用前读到的元数据，然后整份写回
metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
...
metadata.LastSyncTime = instanceMeta.Metadata.LastSyncTime
metadata.MysqlLowerCaseTableNames = instanceMeta.Metadata.MysqlLowerCaseTableNames
if instanceMeta.Version != instance.Metadata.GetVersion() {
    metadata.Version = instanceMeta.Version
}
updatedInstance, err := s.store.UpdateInstance(ctx, updateInstance)
```

```go
// store/instance.go:156-165 —— Metadata 非 nil 时整份覆盖 JSONB，没有 row lock、没有字段级 patch
if v := patch.Metadata; v != nil {
    redacted, err := s.encryptInstance(v)
    ...
    metadata, err := protojson.Marshal(redacted)
    ...
    set, args = append(set, fmt.Sprintf("metadata = $%d", len(args)+1)), append(args, metadata)
}
```

- **机制**：`trySyncAll` 在 tick 开始时读一遍所有实例（`syncer.go:349`），随后每个实例的 `SyncInstance` 先做一次到目标库的网络调用 `GetInstanceMeta`（最长 15 分钟，`syncer.go:454`），再做数据库枚举，最后才把**tick 开始时那份克隆**写回。任何在这段窗口内通过 API 完成的实例元数据修改（`UpdateInstance` 的 title/activation/sync_interval/maximum_connections、数据源 CRUD、SSH 配置）都会被静默回滚。API 侧自己也是「读→克隆→改→整份写」，所以这不是 runner 特有的并发模型缺陷，而是 runner 把一个本来就脆弱的写模式放到了**长窗口 + 自动重复**的场景里。
- **影响**：
  - 用户刚添加的只读数据源（及其凭据）可能消失，且因为写回的是旧 JSONB，凭据密文也一起回退；
  - 用户刚关闭的 `activation`（或把 `sync_interval` 设为 0）可能被重新打开，导致本应停用的实例继续同步；
  - 实例 `title`、`maximum_connections` 等展示/限流配置被回滚；
  - 每次同步都会整份重写 metadata JSONB（重新加密全部数据源），本身也是无谓的写放大。
- **建议**：
  1. 给 `store` 增加与 `UpdateDatabase` 相同的定向更新模式：`UpdateInstanceMessage.MetadataUpdates []func(*storepb.Instance)`，在同一事务里 `SELECT metadata ... FOR UPDATE`、反序列化、应用函数、写回（`store/database.go:236-261` 已是这个模式，直接照搬）；
  2. `SyncInstance` 只 patch `last_sync_time` / `version`（`mysql_lower_case_table_names` 也走同一个 patch），不再传整份 `Metadata`；
  3. API 的 `UpdateInstance`/数据源 CRUD 也迁移到 `MetadataUpdates`，顺带消除 API 之间同类的 lost update；
  4. 新增 hermetic 测试：`SyncInstance` 写回后断言并发修改的 title/activation/data_sources 仍在（可在 store 层用 fake tx 或集成用例）。
- **决策（2026-10-18）**：`SyncInstance` 保持同步执行，不做异步化；因此上述定向 patch 是唯一必要的改动，不需要引入 operation 等待模型。
- **核实**：机制与调用点已逐行确认；触发窗口真实存在（网络调用长度决定）。

### R-02 血缘因标识符引用 / 完整 DDL 判定错误而永久丢失

- **位置**：`backend/runner/lineageanalyzer/analyzer.go:443-501`
- **事实**：

```go
// analyzer.go:483-491 —— 只给 MYSQL/TIDB/DORIS 加反引号、POSTGRES 加双引号；其余 default 原样拼
switch engine {
case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_DORIS:
    identifier = "`" + strings.ReplaceAll(name, "`", "``") + "`"
case storepb.Engine_POSTGRES:
    identifier = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
default:
    identifier = name
}
return definition, fmt.Sprintf("%s %s AS %s", keyword, identifier, definition), nil
```

```go
// analyzer.go:496-501 —— 只认严格 "CREATE " 前缀（注释、换行、制表符都不认）
func isCompleteStatement(definition string) bool {
    return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(definition)), "CREATE ")
}
```

- **机制**：
  1. **MariaDB 与 StarRocks 都落进 `default`**。MariaDB 与 MySQL/TiDB 共用 `backend/plugin/db/mysql` 驱动、视图定义同样被剥成裸查询体（`backend/plugin/db/mysql/sync.go:1066,1075-1082`），需要反引号；StarRocks 是 MySQL 协议方言，也同样需要反引号。对象名是 `order`、`user`、`my view`、`order-total`、数字开头等未引用无法解析的名字时，包装出的 `CREATE VIEW <name> AS ...` 解析失败。
  2. **`isCompleteStatement` 的判定与「完整 DDL」引擎不匹配**：StarRocks MV、MSSQL VIEW 存的是完整语句；一旦 DDL 以 `--`/`/* */` 注释开头，或写成 `CREATE\nVIEW`，就会被判成裸查询体，二次包裹成 `CREATE ... AS -- comment\nCREATE VIEW ...`，同样解析失败。
  3. 解析失败走 `analyzeObject` 的 default 分支 → `markAnalysisFailed`（`analyzer.go:544-550`）**清空该对象已有血缘**并写入当前 `meta_hash`；`queueAll` 因 hash 相等不再入队（`analyzer.go:178-184`），直到对象定义本身变化。日志只有一条 `Lineage analysis failed on an unsupported statement`。
- **影响**：合法对象的血缘永久消失（对 MariaDB/StarRocks 的特殊命名、带注释的 MSSQL/StarRocks DDL 是确定性触发）；用户无法从 UI 区分「没有血缘」与「分析失败」。现有 `TestBuildSQL` 反而把「StarRocks 裸查询体不加引号」钉成了期望（`analyzer_test.go:77-84`），MariaDB/TiDB/MSSQL/含特殊字符的名字没有任何用例。
- **建议**：
  1. 引用规则不要在本包维护第二份方言表：让 `lineage.EngineRegistration` 增加 `QuoteIdentifier(name string) string`（或复用各 dialect 已有的标识符规则），`buildSQL` 直接调用；
  2. 若短期仍在本包改：`case` 补全 MySQL 家族（MYSQL/TIDB/MARIADB/DORIS/STARROCKS）反引号、MSSQL `[` + `]]` + `]`（或按 QUOTED_IDENTIFIER 用双引号），`default` 至少 panic/报错而不是静默拼裸名；
  3. `isCompleteStatement` 改为 token 级判定：先跳过前导空白与注释，再看首个 token 是否为 `CREATE`；或让各引擎在 sync 侧显式标记定义形态（body vs complete statement），不要用启发式；
  4. 回归测试：对 `engines.Registrations()` 的每个引擎，断言 `buildSQL` 输出能被该引擎分析器解析；名字覆盖保留字、空格、连字符、反引号/方括号、数字开头；DDL 覆盖前导 `--`/`/* */`/`CREATE\nVIEW`/`CREATE OR ALTER`。
- **核实**：MARIADB 注册、MariaDB 视图定义来源、`buildSQL` 分支、`markAnalysisFailed` 的 hash 写入均已逐行确认；测试断言也确认钉住了未引用行为。

### R-03 分析写入没有 CAS：sync 删除与在途分析竞态 → 孤儿血缘

- **位置**：`backend/runner/lineageanalyzer/analyzer.go:261-284,435-440`；`backend/store/column_lineage.go:64-84`；清理侧 `backend/runner/schemasync/syncer.go:760-764`
- **事实**：`analyzeObject` 先 `GetMetaRegistry`（:262）读定义，随后解析（:284）与 N+1 查询 source 类型（:408-421，见 R-15），最后 `BatchReplaceColumnLineage`（:436）+ `markAnalyzed`（:440）。`BatchReplaceColumnLineage` 是「先 DELETE 再 INSERT」，不看 registry 行是否还在；`column_lineage` 没有指向 `meta_registry_resource(guid, object_type)` 的外键（`LATEST.sql:304-321`），写孤儿行不会被数据库拒绝。
- **影响**：对象 O 入队后、分析完成前，sync 判定 O 被删并提交（同事务删 registry 行、边、version）。随后分析把旧定义的边与 version 行写回：**已删除对象留下永久孤儿血缘 + 一条 version 行**。清理路径已经执行过，`queueAll` 只遍历现存 registry 对象，没有任何路径会再清它；若同 GUID 以别的类型重建，旧边还会挂错对象。窗口是秒级（解析 + N+1 查询），触发需要「删除恰逢分析」；单副本同样可能（多副本已确认不在考虑范围）。
- **建议**：把「替换边 + 写 version」做成带前置条件的 store 操作：`BatchReplaceColumnLineageIfCurrent(ctx, metaGUID, metaType, expectedMetaHash, lineages)`，同一事务里 `SELECT meta_hash FROM meta_registry_resource WHERE guid=$1 AND object_type=$2 FOR SHARE`，行不存在或 hash 不等返回 `errStale`（调用方不写 version、不重试）；成功路径与失败路径都走这个受保护接口。可选加固：给 `column_lineage(meta_guid, meta_type)` 加复合外键 `REFERENCES meta_registry_resource(guid, object_type) ON DELETE CASCADE`（source/target 端点不能加，因为可能指向尚未同步的对象）。
- **核实**：机制确认；触发概率低，定级为中（P1）——窗口是秒级，触发需要「删除恰逢分析」；单副本部署下仍是真实窗口。

---

## 5. 其余发现详解（R-04～R-33）

### R-04 失败状态不自洽：清边/写 version 两事务；`storeError` 抹掉 hash

- **位置**：`analyzer.go:503-520,538-550`；`store/column_lineage.go:482-492`
- **事实**：

```go
// markAnalysisFailed：先清边（事务 1），再写 version（事务 2）
if err := s.BatchReplaceColumnLineage(ctx, metaGUID, metaType, nil); err != nil { ... }
return markAnalyzed(ctx, s, metaGUID, metaType, metaHash, ...)
```

```go
// storeError：不设置 MetaHash，upsert 的 DO UPDATE 会把已有 hash 覆盖成 NULL
_ = s.UpsertColumnLineageVersion(ctx, &store.ColumnLineageVersion{
    MetaGUID: metaGUID, MetaType: metaType, ErrorMessage: &full,
})
```

- **影响**：
  1. 清边成功、写 version 失败/进程退出 → 「边已清空，version 仍持旧 hash」。多数情况旧 hash ≠ 当前 hash，下一轮自愈；但若同一 hash 下发生确定性硬失败（例如升级回归，见 R-17），该对象就再也不会被重算。
  2. `storeError` 把所有 store/解析前置错误（GUID 非法、实例缺失、registry 多行命中）都写成 hash=NULL，于是小时扫描必然重排：对**持久性**错误形成「每小时 4 次尝试 + 4 条错误日志」的永久循环，使 `scheduleRetry` 的「放弃」语义形同虚设（与 `.agents/docs/lineage-analyzer.md` 不变量 7「三条路径都写当前 hash」也不一致）。
  3. registry 已删/不存在的对象也会被 `storeError` 写一条 `NULL` hash + error_message 的 version 行；而 cleanup 只在删除事务里执行一次、`queueAll` 也不遍历已不存在对象，于是该行永久留存（`column_lineage_version` 的空间/噪音，不触发重试）。
- **建议**：store 增加原子 `MarkAnalysisFailed(ctx, guid, type, metaHash, message)`（同事务 DELETE 边 + UPSERT version）；`storeError` 改为「只更新 error_message、不动 hash」（`DO UPDATE SET error_message = EXCLUDED.error_message, analyzed_at = NOW()`），把重试控制留给 retryMap；或显式提供 `InvalidateAnalysisState` 而不是靠 NULL 的副作用。补测试：注入 `UpsertColumnLineageVersion` 错误，断言不会留下「hash 相等 + 边已清空」。

### R-05 `SyncInstance` 没有实例级串行化（只有单库 gate）

- **位置**：`syncer.go:64-142,471-556`
- **事实**：`databaseSyncGate` 只包住 `SyncDatabaseSchema`（:563-586）；`SyncInstance` 被 API（`instance_service.go:165,349,403`）与后台 pool（`syncer.go:362`）并发调用，没有互斥。两个并发 `SyncInstance` 会各自打开 admin 连接、各自 `UpdateInstance`、各自枚举/创建/软删除数据库。
- **影响**：叠加 R-01（最后写者胜）、R-06（LastSyncTime）与 `CreateDatabaseDefault` 的 `ON CONFLICT ... DO UPDATE SET deleted = EXCLUDED.deleted`（`store/database.go:141-150`），一个调用可能「复活」另一个调用刚软删除的库，下一个快照再隐藏，形成 deleted 抖动；单次 `SyncInstance` 最长 15 分钟，窗口不小。单副本下同样存在（多副本不在考虑范围）。
- **建议**：增加与 `databaseSyncGate` 同构的 `instanceSyncGate`（owner/waiters、panic 释放、waiter 尊重自己的 ctx）包住 `SyncInstance`；或抽成通用 keyed singleflight 复用。测试：两个并发 `SyncInstance` 只执行一次、waiter 复用 owner 的错误。

### R-06 实例 `LastSyncTime` 早于数据库枚举/软删除提交

- **位置**：`syncer.go:483-493`（写回）对比 `:495-522`（建库）、`:528-553`（软删除）
- **事实**：`metadata.LastSyncTime` 在 `GetInstanceMeta` 成功时赋值，并在 :490 先落库；随后 `ListDatabases`、逐个 `CreateDatabaseDefault`、逐个 `UpdateDatabase(deleted=true)` 才执行，任一失败直接 return error，但实例已被标记为「已同步」。数据库级的同一约束被刻意做对（`syncer.go:800-821`：commit 之后才写），实例级没有对齐。
- **影响**：一次瞬时 store 错误或单库插入失败后，扫描在下一个 interval 之前不会重试该实例，新发现的库不会被创建/入队；`SyncInterval` 配成 24h 时窗口同步放大。后台路径只有一条 Warn。
- **建议**：把实例元数据写回移到枚举与软删除全部成功之后；或拆成「version/lowerCase 先写、LastSyncTime 最后写」。补测试（需要可注入 store，见 R-19）。

### R-07 实例同步失败后仍入队该实例全部 due 数据库

- **位置**：`syncer.go:355-372`（失败只记日志）、`:379-396`（无条件下一个循环入队）
- **事实**：`trySyncAll` 对每个 due 实例并发 `SyncInstance`，失败只 `slog.Warn` + `notifyBackgroundInstanceFailure`；`wp.Wait()` 后仍按 `shouldSyncNow(instance, db.LastSyncTime, now)` 入队所有库。坏实例（凭据错误/host 不可达）的库 `LastSyncTime` 仍旧，全部入队。
- **影响**：checker 为每个库各开一次 driver、各走 4 次重试，放弃时按「每库每小时一条」写管理员通知（`operation.go:353`、notification dedupe）。50 库的坏实例 ≈ 50 条/小时通知 + 200 次连接尝试，根因（实例级失败）早已单独通知过；排障时 N 条 per-database 失败掩盖真正原因。
- **建议**：`trySyncAll` 收集本轮失败实例集合，第二个循环跳过这些实例；或把「实例同步成功」作为数据库入队前提。测试：实例同步失败 ⇒ 该实例的库不入队。

### R-08 `SyncAllDatabases` 吞错：全量同步以 INFO「成功」收尾

- **位置**：`syncer.go:403-415`；`operation.go:281-284`
- **事实**：

```go
databases, err := s.store.ListDatabases(ctx, find)
if err != nil {
    slog.Debug("Failed to find databases to sync", slog.String("error", err.Error()))
    databases = nil
}
s.EnqueueDatabases(ctx, operation, databases)   // nil 列表 = seal，pending 为空
```

`instanceErr` 为空、`pending` 为空 → `maybeFinishOperation` 立即投递 INFO 消息，`Succeeded/Failed/Unfinished = 0`。唯一痕迹是默认级别不可见的 Debug 日志。
- **影响**：用户点「全量同步」后收到「成功」，实际 0 个库入队；对可信度是实质伤害，且后台没有 Error/Warn 可排查。
- **建议**：把错误写进 operation（新增 `finishErr` 或复用 `instanceErr`），让消息变 ERROR 并带原因；日志至少 Warn。测试：`ListDatabases` 失败 ⇒ 通知 severity=ERROR 且 `InstanceError != ""`。

### R-09 表统计与 sequence `last_value` 永不刷新

- **位置**：`syncer.go:917-936`（diff 只按 MetaHash）；`store/meta_hash.go:62-77`（指纹归一化统计字段）；UI `frontend/src/components/metadata/TableList.vue:14-15,38-41`、`TableMetadataDetail.vue:375-380`
- **事实**：`normalizeMetadataForHash` 把 `row_count/data_size/index_size/data_free` 与 sequence `last_value` 置 0 后再算 hash；`diff()` 只在 hash 变化时更新。于是这些字段只在「首次插入或最近一次 schema 变化」时落库，此后不再刷新。引入指纹的提交自述是「keeping rowCount/dataSize for the UI」——为了展示保留字段，却没有刷新路径。
- **影响**：一个从 100 行长到千万行的表仍显示 100；前端 `TableList`/`TableMetadataDetail` 直接渲染这些字段，用户看到的是首见时的快照。**后端没有任何 `row_count`/`data_size` 的读者**（`grep` 全仓确认），所以「Explain SQL/MCP 读到过时规模」不成立——影响面仅 UI 展示。`syncer_test.go:151-156` 还把「stats-only 变化不算 update」钉成了预期，掩盖问题。
- **校准**：机制与 UI 影响确认，但无后端/接口消费方；影响是中低，优先级 P2。
- **建议（按 2026-10-18 决策：异步刷新、不入历史）**：
  1. 把统计字段从「元数据变更/历史」链路彻底剥离：`normalizeMetadataForHash` 继续忽略它们（保持现有语义），另建独立的 stats-only 更新路径，只 `UPDATE meta_registry_resource.metadata`（或单独 stats 表/列），**绝不写 `meta_registry_resource_history`、不改变 `meta_hash`、不触发 lineage 重分析/缓存以外的事件**；
  2. 刷新节奏独立于 schema sync（例如每小时/每天跑一次批量 stats 采集，或由 sync 在写事务之后异步提交，失败只记日志）；
  3. UI 继续读当前值，语义变成「最近一次 stats 刷新」而非「首见快照」；需要的话在 API 加一个 `stats_updated_at` 字段；
  4. 改掉 `syncer_test.go:151-156` 的断言，补「stats-only 刷新会更新当前 metadata、但 history 行数不变」的测试。

### R-10 破坏性删除时每个被删对象 2 条 DELETE，绝大多数是 no-op，且全在写事务内

- **位置**：`syncer.go:757-764`（循环）、`:1076-1092`（语句）
- **事实**：

```go
for _, item := range bmc.deletes {
    if err := deleteColumnLineageByMetaTx(ctx, tx, item.GUID, item.ObjectType); err != nil { ... }
}
```

语句对 `(meta_guid, meta_type)`、`(source_guid, source_type)`、`(target_guid, target_type)` 三个方向各删一次，并删 `column_lineage_version`。但 `bmc.deletes` 覆盖 schema sync 管理的全部类型，其中 COLUMN（数量最大）、SCHEMA、DATABASE、SEQUENCE、FUNCTION、PROCEDURE 的 GUID **从不作为血缘端点出现**（全仓 `ColumnLineage{` 写入方只有 analyzer 的 VIEW/MV/MANUAL_SQL/APPLICATION_SQL 与 OpenLineage 的 TABLE/VIEW/MV/EXTERNAL_TABLE/EXTERNAL_DATASET）；TABLE/EXTERNAL_TABLE 的 `meta_guid` 支路也不命中（表和外表不做分析）。VIEW/MV 的 `meta_guid` 支路**会**命中（它们自身是分析对象），分报告「基本是 no-op」的说法已修正。因此权限收缩导致快照清空时，一个 1000 表 × 20 列的库会在**已经持有 `meta_registry_resource` 行锁的写事务里**执行约 4 万条无意义 DELETE（每条对 3 个索引做探针），把「事务只做短暂写」的不变量（`.agents/docs/schemasync.md` 不变量 1）打破；按 100–300µs/往返估算约 4–12 秒，WAL 放大量级有限（no-op DELETE 不产生行版本）。
- **校准**：该问题与 `.agents/docs/schemasync.md`「已知问题 3」（低，逐对象 2 条 DELETE 可批量化）是同一项；本条的**新增证据**只是「列数放大 + 全部落在写事务内」，不改变文档定级（低～低中）。建议与 D10 一起收敛，不必单独立项。
- **建议**：先在内存里按「可能成为血缘端点的类型」过滤（TABLE/VIEW/MATERIALIZED_VIEW/EXTERNAL_TABLE），再用 `= ANY($1)` 的两条批量语句完成 `column_lineage` / `column_lineage_version` 清理；注意保留 VIEW/MV 的 `meta_guid` 支路（它对「target GUID 与 metaGUID 不一致」的方言是唯一清理途径）；同时与 store 侧已有的「子树删除」SQL 收敛（`backend-open-items.md` D10），避免两套语义漂移。加 guard 测试：删除 1 张含 N 列的表应只发固定条数语句。

### R-11 长循环无 panic 隔离；无统一 Runner 抽象；`Add(4)` 硬编码

- **位置**：`server.go:151-155`；`analyzer.go:117-153`、`syncer.go:236-334`、`maintenance.go:52-67`、`validation.go:71-94`
- **事实**：四个 `Run(ctx, *sync.WaitGroup)` 各自实现；长期循环体（analyzer 的 queueAll/消费循环、syncer 的 checker、maintenance 的 runOnce、validation 的 runOnce）没有顶层 recover；而 `sourcegraph/conc` 的 `pool.Wait()` 会把子 goroutine 的 panic 重新抛出（`conc@v0.3.0/waitgroup.go` 的 `Repanic`），`Run` 由 `go` 裸启动，HTTP 的 `connect.WithRecover` 覆盖不到后台 goroutine → **后台 panic 会打崩整个进程**。目前只有 trySyncAll、单库同步、单对象分析有 recover。
- **影响**：一个 nil 解引用/库 panic 从「一个 tick 失败」升级为「整站重启」；新增/删除 runner 时漏改 `Add(4)` 会让 `Wait` 提前返回甚至负计数 panic；首跑/超时/日志策略不一致增加维护成本。**校准**：这是防御性缺口——本次评审没有在 checker/validation/maintenance 循环里找到可稳定触发的 panic 路径（现有代码有 nil 检查与锁保护），所以优先级 P2，而不是现存故障。
- **建议**：定义 `type Runner interface { Run(ctx context.Context) error }`，server 持有 `[]Runner`，用 `errgroup.WithContext` 统一启动/等待/取消；每个 goroutine 顶层 `defer recover → Error + debug.Stack()`；把 ticker 封装成带 jitter、per-run timeout、首跑策略的 helper；日志统一注入 logger/统一 `component` 字段。短期至少给四个 `Run` 各加一层 recover，并把 `Add(4)` 改成按 slice 长度。

### R-12 进程内状态假设未登记（单副本已确认，文档债）

- **位置**：四个 runner + `server.go:151-155`；`docs/security-posture.md`（进程内状态清单）
- **事实**：所有队列、gate、operation、retry、settle channel 都是进程内的；没有 `pg_try_advisory_lock` 选主，也没有 ticker jitter；四个 runner 启动即跑首轮。仓库里唯一的 advisory lock 在启动迁移（`backend/migrator/migrator.go`）。
- **影响（单副本前提下）**：
  - `docs/security-posture.md` 的 one-replica 清单没有 runner 条目，部署者无法从文档得知「runner 的队列/operation 也只在一个进程内」；
  - 无 jitter + 启动即跑：一次重启会同时触发全量扫描与最重的 retention prune（实例/对象多时是明显的启动风暴）；
  - 同进程内的写入冲突仍在（ingestion 第二个 Processor vs revalidation，R-29），与副本数无关。
  - 多副本下的 duplicate-key/重复边等后果按 2026-10-18 决策不再作为产品风险考虑（仅作为「若未来改多副本」的已知前提记录在此）。
- **建议（已决策：不做 lease/advisory lock）**：把 runner 的进程内状态补进 `docs/security-posture.md`（与 device/SSO/OAuth 并列），`docs/deploy.md` 写明部署约束；ticker 加 0–10% jitter、「本轮结束后再计时」；R-29 用唯一约束解决。本条降为 **P3 文档债**。

### R-13 `lineagevalidation` 的 `passTimeout` 约束不了 `revalidateMu`

- **位置**：`lineagevalidation/validation.go:99-113`；`plugin/openlineage/processor.go:31-35`、`lineage_revalidation.go:78-80`
- **事实**：`runOnce` 建了 5 分钟 `passCtx`，但被调用方第一行是普通 `sync.Mutex.Lock()`，不响应 ctx。maintenance 每 6 小时跑的两个 revalidation 与它共用同一把锁；单次 `Revalidate*` 受 `revalidationBatch=500` 条约束，最坏持锁时长未实测（对每个 run 串行 List + validate + 批量替换），不能断言「远超 5 分钟」。
- **影响**：确定成立的危害是 shutdown 语义——锁不可取消，`passTimeout` 只约束已拿锁后的查询；关闭时 goroutine 可能卡在 `Lock()`，`server.go:240-248` 等满 10s 后继续 `store.Close()`，随后该 goroutine 醒来对已关闭连接池发查询（错误噪音）。sync 后的 revalidation 也可能被一次长维护 pass 推迟（不是无限期，取决于积压）。注释承诺的「a stuck pass cannot wedge the runner」不成立。
- **校准**：机制确认，实际影响是 shutdown 噪音/延迟与 revalidation 推迟，不是数据问题；优先级 P2。
- **建议**：把锁换成可取消信号量（`select { case <-ctx.Done(): ...; case sem <- struct{}{}: }`），或让 `Revalidator` 接口显式接收/检查 ctx；`runOnce` 侧可先做非阻塞「锁空闲」探测。测试：持锁时 `runOnce` 在 ctx 取消后能返回。

### R-14 应用上报路径无变更检测：每条语句每次上报都全量重分析

- **位置**：`api/v1/application_sql_handler.go:331-333`；`store/application_sql.go:225-246`；`runner/lineageanalyzer/analyzer.go:86-91,226-234,400-406,436-440`
- **事实**：`ReportApplicationSQLs` 返回的 statements 包含已存在的行（upsert + rollup，不是只有新插入），handler 对**每一条**都 `QueueLineageAnalysis`；runner 的 `QueueAnalysis` 不做 hash 比较（只有 `queueAll` 做），每次都会重新解析、N+1 查 registry、整批 delete+insert 边、再写一次 `application_sql.kind`。`analyzeMap` 只做同一 10s tick 内去重。
- **影响**：频率取决于生产者的上报节奏（代码不保证固定频率）：`ReportApplicationSQLs` 对已存在的语句也会返回并触发入队，因此按分钟轮询上报的应用，每条活跃语句每分钟都会被完整重分析一次；万级语句时 `column_lineage` 每分钟 delete+insert 数万行，与「行数随 distinct statement 增长」的设计目标相悖。文档只承认「ingest 一次解析 + runner 再解析」的解析成本，未覆盖整批边替换/N+1/kind 写入。
- **建议**：让 `ReportApplicationSQLs` 返回 `inserted/updated` 标记（`ON CONFLICT ... RETURNING (xmax = 0)`），handler 只对真正新增或 `sql_text` 变化的语句排队；或 runner 在 drain 时先按 `column_lineage_version` 的 hash 与 registry hash 比较，相同则跳过（抽成 `needsAnalysis`，`queueAll` 复用）。若保留「每次上报必重算」，至少把 `BatchReplaceColumnLineage` 改成边集合不变的比较式更新。

### R-15 分析器逐 GUID N+1 查元数据类型（绕过 LRU，与 `AnalyzeSQL` 不一致）

- **位置**：`analyzer.go:408-421`；对照 `api/v1/lineage_service_analyze.go:257-284`
- **事实**：`for guid := range guidLookupSet { a.store.GetMetaRegistry(ctx, Find{GUID: &g}) }` 串行查询，每个 source GUID 一次 DB 往返；GUID-only 查询不命中 store 的 LRU（`store/meta_resource.go:113-118` 要求 GUID+ObjectType）。同仓库 `AnalyzeSQL` 用 `ListMetaRegistryResourceDigest(GUIDs: &guids)` 一次批量完成。循环内也不检查 `ctx.Err()`。
- **影响**：单对象查询数从 2 变成 2+S（S=不同 source 数）；MANUAL_SQL 脚本/APPLICATION_SQL 大 join 可到数百，10 并发下放大为 10×(2+S) 个并发读；shutdown 时会对每个 GUID 依次打 WARN（日志风暴）。
- **建议**：复用 `ListMetaRegistryResourceDigest` 的 `GUIDs` 批量形式，一次查询填 `guidTypeMap`；`analyzeObject` 入口的 `GetMetaRegistry` 带上已知 `ObjectType: &metaType`（可命中缓存并消除多行风险）；把「relation → 已补全类型」抽成 runner 与 `AnalyzeSQL` 共用的 helper。

### R-16 单批无上限、无 per-object 超时；backfill 阻断事件队列

- **位置**：`analyzer.go:189-238`
- **事实**：消费者每个 tick 把 `analyzeMap` **全部**内容抽成一个批次，`wp.Wait()` 阻塞消费者直到整批完成；`analyzeObject` 只受 runner 全局 ctx 约束，没有单对象超时。冷启动/升级后首次全量入队时，批次可以等于整个 registry。
- **影响**：长时间 backfill 期间，用户刚保存的手工 SQL、新上报的 app SQL 排在整批之后（可能几十分钟）；单个卡死的解析/DB 查询会拖住整批；runner 实际停摆到重启，`runnerShutdownTimeout` 只保证进程能退出。
- **建议**：每 tick 只取上限 N（如 `MaxGoroutines*4` 或 1000）个 key，剩余留到下一 tick（天然限流 + 事件可插队）；每次 `analyzeObject` 包 `context.WithTimeout`（2–3 分钟），超时按瞬时失败处理（注意与 R-02/R-04 的错误分类配合：不能当确定性解析失败清边）；首次全扫可单独降速/分批。

### R-17 变更检测只有 metahash，没有 analyzer/规则版本

- **位置**：`analyzer.go:178-184,299-300,440,548-549`；`LATEST.sql:324-330`
- **事实**：「是否重算」只看 registry 的 meta_hash 与 version.meta_hash。没有 analyzer 版本列；引擎不支持（OceanBase）与解析失败都写入当前 hash 表示「已分析」。
- **影响**：
  - 分析器变强（新增方言、omni 升级修复解析、规则变更）后，既有对象不会重算，直到定义变化；
  - 分析器变弱（升级回归）时，只有事件路径会触发重算；一旦重算失败，`markAnalysisFailed` 把当前 hash 写成「已分析」，回滚部署后也不会再试；
  - 引擎从不支持变为支持（新增方言注册）后，旧的「skip」记录同样不会重算。
  - 这意味着任何分析器修复上线都需要运维手工清 `column_lineage_version`（或改元数据），而仓库没有这样的运维路径/文档。
- **建议（按 2026-10-18 决策：无历史兼容负担）**：`column_lineage_version` 直接加 `analyzer_version`（schema 增量 + `LATEST.sql`），由 `lineage.Analyzer`/`engines` 暴露单调版本（可复用 `sqlsanitize.Version` 的机制）；`queueAll` 判据改为「hash 变化 OR analyzer_version != 当前」；部署后小时扫描自动全量重算，**不需要数据回填、双写或兼容旧行**。同时可顺手加一个受控的「全量重算」入口（管理命令或内部 RPC）用于紧急修复。与 `.agents/docs/lineage-analyzer.md` T5（omni 升级流程）一起更新。

### R-18 `maintenance` 无 pass 超时、无可观测性；setting 失败直接 `return`

- **位置**：`maintenance.go:52-158`；`lineagevalidation/validation.go:99-101`（对照）
- **事实**：`runOnce` 无 deadline（validation 有 5m passTimeout）；成功但零删除时没有任何日志/耗时/计数；`GetWorkspaceGeneralSetting` 失败时 `return`（当前恰好是最后一项，但新增任何后续清理项都会被静默跳过）；包内零测试，依赖具体 `*store.Store`、`now`/`interval` 不可注入。
- **影响**：6 小时一次、跨大表（`DeleteOpenLineageRunsBefore` 可一次删百万级 run 并逐 task reconcile）的破坏性清理：慢 pass 会吞掉后续 tick 且不可见；「retention 有没有跑、为什么没删、跑了多久」只能从零散日志推断；一次瞬时读失败让 retention 延迟 6 小时且只有一行 Error。
- **校准**：零测试本身是 `.agents/docs/backend-open-items.md` E12 已登记的债务（不重复计为新发现）；「无 metrics/计数器」是 D8 已接受前提。本条真正新增的是「`runOnce` 无超时 + 无摘要日志 + setting 失败即 return」这三点，属可观测性/健壮性改进，优先级 P2。
- **建议**：给每项/整轮加 `context.WithTimeout`，超时 Warn 并标注项目；每轮结束打一条摘要（duration、各项 deleted、失败项、是否因 setting 跳过 retention）；`if err != nil { Error } else { ... }`，不中断后续项；把 store 收窄为本地窄接口 + 注入 `now`/`interval`，补表驱动测试（某项报错不影响后续、setting 失败不跳过、`days<=0` 不删、retention/grace cutoff 精确、nil revalidator 不 panic）。

### R-19 测试缺口 + `lineagevalidation` 的 trigger 合并缺陷（测试脆弱）

- **位置**：`lineagevalidation/validation_test.go:92-110`；各包测试如 §8
- **事实**：`TestTriggerCoalescesOneSyncRound` 连续 `Trigger()` 5 次后断言只跑一次 pass；实现里 channel 容量 1，消费者取走第一个 token 后进入 settle，期间到达的 trigger 会重新占位，settle 结束后再跑一次 pass。评审中 `go test -race -count=10 ./backend/runner/...` 观测到 1 次 `expected 1, actual 2`；独立审稿人在单测试 `-count=300`、加压 `-count=200`、整包 `-count=100` 共 610+ 次 `-race` 重复中 **0 次复现**。因此准确结论是：**设计缺陷成立（settle 窗口的信号不会被折叠，注释承诺未实现），测试依赖调度时机、属 fragile-by-design**；「稳定 flaky」的断言证据不足。
- **影响**：settle 窗口内提交的 sync 会产生一次冗余 revalidation pass；测试在特定调度下可能红（review 中观测到一次），给 CI 带来不可预测性。
- **建议**：settle 结束后、`runOnce` 之前非阻塞 drain 一次 trigger，或用 `sync/atomic` 的 dirty 标志（Trigger 置 dirty，pass 开始清，结束时若 dirty 再跑）；修复后该测试即确定性。另见 §8 的测试缺口清单。

### R-20 每轮数据库同步都全量抓取 DDL；降级模式可能永久陈旧

- **位置**：`syncer.go:729-735`；`object_definition.go:21-33,100-122`
- **事实**：只对实现了 `ObjectDefinitionReader` 的驱动（目前仅 StarRocks）生效；`prepareObjectDefinitions` 在**每个**数据库的**每次**同步（15 分钟）都对全部 schema-bearing 对象（上限 2000）抓一次 `SHOW CREATE`，即使用户只改了统计；超过 2000 时降级为「只刷新变更对象」，而 StarRocks 属性变更不动元数据 hash，这些对象可能永久陈旧（文档已知问题 6 已登记后者，但全量抓取本身的成本未登记）。
- **影响**：大实例（100 库 × 2000 对象）每 15 分钟产生 20 万次 `SHOW CREATE`；对目标库压力大，同步时间随对象数线性增长，且发生在写事务之前（不会持锁，但占用连接/限流槽）。
- **建议**：把 DDL 刷新与 schema 同步解耦成不同节奏（例如 DDL 全量刷新每小时/每天一次，日常只刷新变更对象），或让引擎提供更廉价的 DDL 版本/checksum；降级模式用轮换批次补刷，避免永久陈旧。

### R-31 批量同步 API 在请求 goroutine 内串行执行，且没有 per-RPC deadline

- **位置**：`api/v1/instance_service.go:29,375-425`；`runner/schemasync/syncer.go:32,454`；`server/grpc_routes.go`（仅 MCP 路由有 60s `http.TimeoutHandler`）
- **事实**：`maxBatchInstances = 1000`；`BatchSyncInstances` 的 for 循环对每个实例同步调用 `SyncInstance`（:403，内部 `GetInstanceMeta` deadline 15 分钟），再 `SyncAllDatabases`；Connect handler 链路上没有任何 per-RPC 超时（`mcpEndpointTimeout` 只包 `/mcp`）。
- **影响**：1000 个目标 hang 的实例可在服务端串行挂约 250 小时，请求 goroutine 与每实例连接槽长期被占；`.agents/docs/schemasync.md` 已知问题 1 只写了「单实例 API 同步最长 15 分钟」，未覆盖批量放大。这是可用性/资源问题，不是数据错误。
- **建议（按 2026-10-18 决策：保持同步，不转异步）**：给 Connect 链路加 per-RPC deadline（至少给 batch RPC 加）；限制 `BatchSyncInstances` 的单次总预算（实例数 × 超时）并返回已完成的部分结果；在 proto/文档中写明最坏耗时与「目标 hang 会拖住整批」的事实。把批量同步整体转异步是更彻底的方向，但已确认暂不做。
- **核实**：常量、循环、timeout 位置均由审稿人逐行确认（三份分报告未覆盖，作为漏项补入）。

### R-32 `lineagevalidation` 没有启动首轮 pass

- **位置**：`lineagevalidation/validation.go:72-94`；对照 `maintenance.go:55`、`analyzer.go:125`、`syncer.go:221`
- **事实**：另外三个 runner 启动即跑首轮；`lineagevalidation.Run` 只等 trigger，而 trigger 只在 `len(bmc.updates) > 0` 的 sync 提交后发出（`syncer.go:796-798`）。
- **影响**：进程重启/升级后，ingestion 留下的 unknown-endpoint 边要等下一次「有变更的 sync」或 6 小时维护 pass 才被复查；与 R-11（每次 pass 只处理 500 条）叠加时恢复更慢。
- **建议**：`Run` 启动时先 `Trigger()` 一次（或直接 `runOnce`）；或在文档中说明「重启后最长 6 小时恢复」。
- **核实**：审稿人补入，代码事实确认。

### R-33 `trySyncAll` 在 `wp.Wait()` 之后仍用循环前捕获的 `now`

- **位置**：`runner/schemasync/syncer.go:354,372,391`
- **事实**：`now := time.Now()` 在实例同步之前，`wp.Wait()` 之后判断数据库是否到期仍用 `now`；实例同步最坏 15 分钟。
- **影响**：本轮开始后才到期的库会被判为「未到期」，要等下一轮 15 分钟扫描；极端情况持续偏移一个实例同步时长。
- **建议**：`wp.Wait()` 后重新取 `now`（或按数据库逐个取），成本可忽略。
- **核实**：审稿人补入，代码事实确认；优先级 P3。

---

## 6. 低风险项（R-21～R-29）与 Info

### P2/P3

| ID | 优先级 | 问题 | 位置 | 建议 |
| --- | --- | --- | --- | --- |
| R-21 | P3 | operation 只有「sealed + instanceRecorded + pending 空」一条完成路径，没有 abort；`EnqueueDatabases` 不检查 sealed；handler 在 StartOperation 与 Record 之间 panic/退出时 operation 挂 2 小时后被 expire 成零计数 INFO | `operation.go:104-127,183-217,239-250` | 加 `Abort(ctx, err)` 并在 handler `defer` 兜底；`EnqueueDatabases` 首行拒绝已 sealed；expire 对未 record 的 operation 记 `instanceErr = "operation abandoned"`，不要发 INFO 成功 |
| R-22 | P3 | 已软删除的库仍留在 `databaseSyncMap`/`databaseSyncRetryMap`，checker 继续同步并给已删除行写 `LastSyncTime`；`SyncDatabaseSchema` 入口不看 `Deleted` | `syncer.go:167-174,544-553,262-273,811-819` | 软删除时清队列；checker 取出后检查 `Deleted`；`SyncDatabaseSchema` 对 deleted 行直接返回 |
| R-23 | P3 | 目标报告数据库不存在时 `markDatabaseDeleted` 后 `return nil`：checker/operation/API 全部按成功计数 | `syncer.go:610-620,323-324`；`operation.go:159-161` | 用可区分返回值（`errDatabaseHidden`/`SyncOutcome` 枚举）上报；operation 单独计数，API 选择 404 或 200+提示 |
| R-24 | P3 | 软删除循环丢弃底层错误（`errors.Errorf` 不用 `%w`），且逐库一个事务，中途失败留下「部分可见」状态 | `syncer.go:544-553` | `errors.Wrapf`；store 增加批量 `UPDATE db SET deleted=true WHERE instance=$1 AND name=ANY($2)` |
| R-25 | P3 | `EscapeGUIDPart` 把 `;`→`%3B`，但不转义字面量 `%`：名字含字面量 `%3B` 与名字含 `;` 会得到同一 GUID；同一批次出现重复 `(guid, object_type)` 会让 `ON CONFLICT DO UPDATE` 报 `cannot affect row a second time`，整库同步永久失败 | `common/guid.go:16-38`；`syncer.go:908-943`；`store/meta_resource.go:459-500` | 让转义可逆且单射（先 `%`→`%25` 再 `;`→`%3B`，或长度前缀）；`diff()` 写前按 key 去重并对同键不同对象 Warn，避免整库失败 |
| R-26 | P3 | 定义从有变空时 `markAnalyzed(hash, "")` 保留旧血缘、不写 `error_message`，图上继续显示过期边 | `analyzer.go:276-279` | 明确语义：视为「无法分析」写非空 error_message，或与解析失败一样清边；登记进 `lineage-analyzer.md` failure modes |
| R-27 | P3 | `logSchemaSyncDeletion` 在 `Commit` 之前调用，回滚时这条「Deleting metadata resources」日志描述一次未发生的删除（而它是破坏性同步唯一痕迹） | `syncer.go:755,766` | 移到 commit 成功之后，或同时记录 commit 结果 |
| R-28 | P3 | 实例级连接限流饱和（`state.ErrInstanceConnectionLimit`）没有像 database 级那样特殊处理，被当作实例同步失败并发管理员通知；饱和是可恢复的竞争，不是凭据/网络故障 | `syncer.go:302-309`（DB 级对照）、`355-369` | 实例级也 `errors.Is` 识别，跳过本轮、不通知、不推进任何退避 |
| R-29 | P2 | 同进程竞态下 `BatchReplaceColumnLineage`（DELETE+INSERT、无唯一约束、无外键）会产生重复边；`GetLineage` 不去重（`api/v1/lineage_service.go:92-118`），RPC 会原样返回重复行 | `LATEST.sql:304-321`；`store/column_lineage.go:64-84` | 已确认单副本、无历史兼容：直接给 `column_lineage` 加唯一约束（`meta_guid, meta_type, source_guid, source_column, target_guid, target_column, relation_type`，transformation 是 JSONB 不能进键）+ 插入 `ON CONFLICT DO NOTHING`，迁移时清一次重复行即可；不需要 advisory lock |

### Info：架构与技术债（不单列严重度）

1. **包边界与依赖方向**（AR-08/SS-15）：`schemasync` 直接依赖兄弟包 `lineageanalyzer` 的具体类型，同时为 API 提供 `QueueLineageAnalysis` 门面（`database_manual_sql.go:72,269`）；`maintenance` 依赖具体 `*store.Store`。建议 `schemasync` 内定义 `type LineageQueue interface { QueueAnalysis(string, storepb.MetaType) }`，API 直接依赖分析队列接口；`maintenance` 为 7 个 delete + setting 读取定义本地窄接口（与 R-18 一并解决）。
2. **`ObjectDefinitionReader` 无法表达 schema 限定**（SS-15）：`GetObjectDefinition(ctx, objectType, name)` 只传名字；StarRocks/Doris 一库一 schema 够用，但接口注释已计划 MySQL/PG 多 schema，扩展时需要 breaking change。建议现在就把 schema（或完整 GUID/StoredMetadata）纳入参数。
3. **重复实现**（LN-17/LN-18/LN-22）：GUID→MetaType 补全在 runner 与 `AnalyzeSQL` 两份（R-15）；临时表判定 runner 只看 `IsTemp`，handler 还比对 `Target.Table.Name == tempResultTable`（runner 遵守 `model.ColumnRelation` 的契约，额外比对属防御性重复，不是现存缺陷）；VIEW/MV 的 `TargetGUID` 从包裹名重算而非直接用 `metaGUID`，残余风险是「target 是幽灵 GUID」——`TargetType` 已预置为 `metaType`，不会留 0。建议各收敛为一处（`model.IsTempTarget`、共享 resolver），并把 VIEW/MV 的 target 直接钉为 `metaGUID`。
4. **`SetApplicationSQLKind` 写在边之前**（LN-22）：kind 是派生缓存，先于事实写入；若随后的边写失败，窗口内 kind 与图不一致。建议移到 `BatchReplace` 成功之后或同事务。
5. **日志/注释/死代码**（SS-16/AR-10/AR-11/AR-15）：`slog.Debug(fmt.Sprintf(...))` 丢结构化字段且文案说「started」实为首轮扫描后；`enqueueDatabase` 的 `_ *SyncOperation` 死参数；`operation.go:16-20` 注释写「retries twice」而实现最多 3 次退避；`syncer.go:487-489` 的 version 条件与无条件赋值等价；7 段类型循环的 `if err != nil` 不可达；`server.stopper []func()` 是死代码且 `store.Close()` 失败会跳过它；关闭时 `StartH2CServer` 返回 `http.ErrServerClosed` 被误报 Error。逐项顺手清理。
6. **文档与治理**（AR-15）：`backend-open-items.md` 的 D9 只登记了两个包的硬编码常量，`maintenance` 的 6h/7d/1h、`revalidationBatch=500`、`operationTimeout=2h` 未登记；`.agents/docs/README.md` 没有 maintenance/runner 生命周期参考；`lineagevalidation/validation.go:7` 以注释硬编码 maintenance 的 6 小时周期；OpenLineage retention 的 proto 文案（「how long persisted runs are kept」）与实现（按 `event_time`、`event_time IS NULL` 永不裁剪、最长延迟 6h）不一致。建议新增 `.agents/docs/runner.md`（四个 runner 的启动顺序、间隔、失败/重试、多副本假设、关闭语义），并把 D9 扩全。
7. **启动/关闭语义**（AR-14）：LLM debug 写盘 worker 是包级 goroutine + `context.Background()`，不在 `runnerWG` 内；`Shutdown` 只等 runnerWG 就 `store.Close()`，worker 可能在库关闭后继续写、队列也不排空。建议纳入统一后台 worker 生命周期（相邻问题，非 runner 包内）。
8. **分析队列的状态机有两处可构造竞态**（LN-04/AR-12）：`QueueAnalysis` 与 `scheduleRetry`/放弃分支并发时，新请求可能被旧的失败退避覆盖，或在「删 retry → 删 analyzeMap」之间被删除（最长延迟 1 小时）；`drainAndAnalyze` 的 `Range → 先 append 后 Delete` 也会抹掉刚好在两步之间到达的新入队。后果是**延迟**而非数据错误。建议按 key 串行化「drain 删除 + 分析 + 回写」，或用 generation/dirty 标志；这是低成本、值得顺手修的一处。
9. **重新校验的批次/接口语义**（LN-11/AR-07）：`Revalidate*` 每次最多处理 `revalidationBatch=500` 条边，runner 传 `limit=0` 表示「用默认批次」（语义只写在实现里），且一次 pass 不检查是否还有积压、不自我重触发；maintenance 侧丢弃 `stillUnknown`。积压大时排空速度受限（维护 6h 一次）。建议接口注释写明 `0=默认批次`，返回值增加 `hasMore`，maintenance/validation 在有剩余时循环或自触发（用 passTimeout 设上限）。
10. **`EnableFullSync` 的语义不一致**（SS-14）：`SyncInstance` 在 `EnableFullSync=false`（前端默认）时只入队 `newDatabases`，已有库完全不刷新；后台扫描与 `CreateInstance` 都是全量。建议把默认值改为 true，或在 proto 注释/文档中写明 false 表示「仅发现新库」。

---

## 7. 架构级重构建议（跨条目）

### 7.1 统一的 Runner 骨架（推荐先做，收益最高）

```go
// backend/runner/runner.go（示意）
type Runner interface {
    Name() string
    Run(ctx context.Context) error   // 阻塞到 ctx 取消；返回错误只用于日志/健康状态
}
```

- server 持有 `[]Runner`，用 `errgroup.WithContext` 启动；`Shutdown` 先 cancel 再 `Wait`，超时可配；
- 每个 `Run` 外层统一 `defer recover`（记录 `debug.Stack`），把「进程崩溃」降级为「runner 退出」；
- 提供 `Loop(ctx, interval, jitter, firstRun, fn)` helper：处理 ticker、jitter、首跑、per-run timeout、跳过 tick 的记录；
- 统一日志字段（`runner=lineageanalyzer`、`run_duration`、`processed`、`failed`），不引入 metrics 也能回答「上次跑没跑、跑了多久、失败多少」；
- 四个现有 `Run` 改成实现该接口，业务函数（`trySyncAll`、`queueAll`、`runOnce`）不动。

### 7.2 把 `schemasync` 拆成 service + runner

- 把 `SyncInstance`/`SyncDatabaseSchema`/`SyncOperation`/通知等「同步语义」移到 `backend/component/schemasync`（或 `backend/server` 下），`runner/schemasync` 只保留调度（周期扫描 + 队列消费）并调用 service；
- API 直接依赖 service，不再通过 `runner` 包拿「分析队列门面」；
- 依赖方向变为 `runner → component → store/plugin`，消除 `api → runner` 的反向依赖，也让「同步语义唯一所有者」清晰。
- 这是中等规模重构，可作为第 3 步的一部分；短期至少先做 7.3 的接口倒置与 R-05 的实例 gate。

### 7.3 派生数据写入的 CAS + 版本模型

- `store` 提供：
  - `BatchReplaceColumnLineageIfCurrent(ctx, guid, type, expectedHash, edges)`：`SELECT ... FOR SHARE` 校验 registry 行与 hash，再 delete/insert；
  - `MarkAnalysisFailed(ctx, guid, type, hash, msg)`：同事务清边 + 写 version；
  - `RecordAnalysisError(ctx, guid, type, msg)`：只更新 error_message，不动 hash；
- `column_lineage_version` 增加 `analyzer_version`；`lineage.Analyzer` 暴露单调版本；
- `queueAll` / 事件路径都走 `needsAnalysis(ctx, key)`（hash + analyzer_version），事件路径不再无条件重算；
- schema 同步删除对象时，清理逻辑与上述条件写共同保证「不会复活」：条件写因 registry 行不存在而失败，调用方记录 stale 并不重试。

### 7.4 单副本假设的落档（已决策 2026-10-18）

- 已确认不考虑多副本，因此**不做** leader election / `pg_try_advisory_lock` / 跨进程锁；`docs/security-posture.md` 的「one replica」清单需要补上 runner 的进程内状态（gate/queue/operation/retry/settle），`docs/deploy.md` 也应在部署约束里写明。
- 同进程内的写入冲突仍需处理（与副本数无关）：ingestion 的第二个 `openlineage.Processor` 与 revalidation 可能并发替换同一 run 的边（R-29），用 `column_lineage` 唯一约束 + `ON CONFLICT DO NOTHING` 收敛；`SyncInstance` 的实例级 gate（R-05）同样只在进程内，但单副本下这就够了。
- 低成本健壮性改进：ticker 加 0–10% jitter，「本轮结束后再计时」，避免重启瞬间同时扫全量（不依赖多副本假设也有价值）。

### 7.5 可测试性

- 把 `*store.Store` 收窄为本地写接口（`syncStore`/`analysisStore`/`maintenanceStore`），把 `now func() time.Time`、`interval`、`passTimeout` 变成字段（`lineagevalidation.Runner.settle` 已是好例子）；
- 这样 `SyncInstance`、`SyncDatabaseSchema` 的事务顺序、`queueAll`/`drainAndAnalyze`、`maintenance.runOnce` 都能用 fake 做 hermetic 表驱动测试，不必依赖集成套件；
- 新增测试清单见 §8.2。

---

## 8. 测试与门禁

### 8.1 现状

- 纯函数/状态机测试选点好：`buildSQL`（7 例）、退避阶梯、`scheduleRetry` 的 map 行为、`classifyApplicationSQL`（含自我边/去重/大小写身份）、`batchMetaCreate.diff`、`definitionCandidates/diffDefinitions`、`databaseSyncGate` 的 owner/waiter/panic/ctx、operation 聚合/超时/通知、`lineagevalidation` 的触发合并/运行中触发/失败继续/ctx 取消。
- 缺口集中在「写路径 + 调度」：`SyncInstance`、`SyncDatabaseSchema` 的事务边界/DDL/血缘删除/缓存失效/LastSyncTime 顺序、`queueAll`/`drainAndAnalyze`、`Run` 的 ticker/ctx/panic、`maintenance.runOnce`、事件路径的变更检测，全部没有 hermetic 测试；维护包零测试；`SyncAllDatabases` 错误路径、实例失败后的入队、同进程内 checker+API 的并发路径也没有覆盖。
- CI 只跑 `go test -race -cover ./...`，无覆盖率阈值；`TestTriggerCoalescesOneSyncRound` 依赖调度时机（R-19：review 中观测到 1 次失败，审稿人 610+ 次隔离运行 0 失败），本质是「设计缺陷 + fragile test」，不是稳定 flaky。

### 8.2 建议新增（按优先级）

| # | 测试 | 目的 |
| --- | --- | --- |
| T1 | `SyncInstance` 与并发 API 更新交错（fake store 或集成） | 钉住 R-01：并发修改的 title/activation/data_sources 不被回滚 |
| T2 | `buildSQL` 全引擎 + 特殊名 + 前导注释 DDL 表驱动 | 钉住 R-02；并要求 wrap 输出可被对应分析器解析 |
| T3 | `BatchReplaceColumnLineageIfCurrent` 的 stale 路径（行已删/hash 变） | 钉住 R-03：不产生孤儿边、不写 version |
| T4 | `markAnalysisFailed` 注入第二次写失败 | 钉住 R-04：不留「hash 相等 + 边已清空」 |
| T5 | 删除 1 张含 N 列的表：断言清理语句数/结果 | 钉住 R-10 的规模回归 |
| T6 | `SyncAllDatabases` 的 `ListDatabases` 失败 | 断言 operation 变 ERROR（R-08） |
| T7 | 实例同步失败 ⇒ 该实例的库不入队 | 钉住 R-07 |
| T8 | `queueAll` 的 hash + analyzer_version 判据；事件路径 `needsAnalysis` | 钉住 R-14/R-17 |
| T9 | `Run` 的 ctx 取消/panic 隔离/`Add` 计数（用 fake Runner） | 钉住 R-11 |
| T10 | 持 `revalidateMu` 时 `runOnce` 在 ctx 取消后返回 | 钉住 R-13 |
| T11 | `maintenance.runOnce` 表驱动（错误隔离、setting 失败、cutoff 边界、nil revalidator） | 钉住 R-18 |
| T12 | `-race -count=10` 的 runner 测试门禁 | 让 R-19 这类 flake 在 CI 暴露 |

### 8.3 门禁建议

- 在 CI 增加 `go test -race -count=3 ./backend/runner/...`（或至少对 `lineagevalidation` 用 `-count=20` 跑关键用例）；
- 对 runner 包设置最低覆盖率门槛（例如 `schemasync`/`lineageanalyzer` 各 55%），防止「只测纯函数」继续退化；
- 涉及 `runner` 的改动跑一次集成 smoke；schema 同步/血缘分析改动跑对应集成套件（见 `backend/test/integration/README.md`）。

---

## 9. 已接受的决策与既有债（本文不重复上报）

| 条目 | 今天的状态 | 与本文关系 |
| --- | --- | --- |
| **D9** runner 间隔/上限是编译期常量（`schemasync` 15m/10s/15m/100/3、`lineageanalyzer` 1h/10s/10） | 有意接受，无运维覆盖 | 本文不重报；但 D9 未登记 `maintenance`、`revalidationBatch=500`、`operationTimeout=2h`（R-30 Info） |
| **D8** 同步失败只有 `slog.Warn`，无计数器/告警 | 有意接受，需先决定是否重建遥测 | R-18 中 maintenance 的可观测性缺口未被 D8 覆盖，属新空白 |
| **破坏性 schema sync 仅日志、无收缩阈值/确认** | 有意接受（MySQL information_schema 权限收缩可清空快照） | R-10 只讨论该路径的**事务成本**，不主张加阈值；R-25 讨论 GUID 冲突，不改变该决策 |
| **进程内状态（device login / SSO / OAuth / notification / token 缓存）** | `docs/security-posture.md` 逐条登记「one replica or sticky routing」；**2026-10-18 已确认 runner 也按单副本部署** | R-12 降为文档债：把 runner 的进程内状态补进该清单；不需要 lease/advisory lock |
| **D7 store 层不校验数据源** | 有意接受（历史 ADMIN 行兼容） | 与本文无关 |
| **E12** maintenance 零测试 | `.agents/docs/backend-open-items.md:37` 已登记（0 个 `_test.go` 的包） | R-18 提及「零测试 + 不可注入」时以 E12 为背景，不重复计为新债 |
| **D10 血缘删除两套 SQL** | 已知开放项 | R-10 建议与 D10 合并收敛，不重复登记 |
| **`.agents/docs/schemasync.md` 已知问题 1–6** | 已登记：API 同步最长 15m、checker 空转、逐对象清理、7 段循环、多余 Clone、降级模式 DDL 陈旧 | 本文 R-20 补充「全量抓取成本」；R-10 补充「no-op 规模」；其余不重报 |
| **`.agents/docs/lineage-analyzer.md` Open items T5（omni 升级流程缺失）** | 已知开放项 | R-17 是它的下游后果（没有 analyzer 版本 → 升级不重算），建议合并处理 |

---

## 10. 正面观察

1. **事务纪律**：schema 同步把 digest 读取、diff、DDL 抓取全部放在 `BeginTx` 之前；元数据行、DDL 行、被删对象的血缘行在同一事务；缓存失效严格在 commit 之后；数据库 `LastSyncTime` 在 commit 之后写。`.agents/docs/schemasync.md` 的不变量表与代码逐条对得上。
2. **per-database singleflight 质量高**：owner/waiters、finish 先删表后 close、waiter 尊重自己的 ctx、panic 时先发布错误再 re-panic，并有针对并发语义的 hermetic 测试。它精确覆盖了「两个同步各读旧 digest 再写」的最危险冲突。
3. **「未建模 ≠ 失败」的三档语义**：`UnsupportedStatementError` 保留边 + 缺口、`ErrorEngineNotSupported` 记 skip 不清边、只有真解析失败才清边；注释解释了每档理由，测试也钉住了。
4. **确定性失败写当前 hash**：成功/skip/解析失败三条路径都写 hash，机制上避免「每小时无限重试同一段坏 SQL」；这是 R-17 版本模型可以建立在之上的正确基础。
5. **operation 聚合模型清晰**：instance 步与数据库步用两个边沿避免提前完成；一个 operation 只投递一条消息；失败/未完成升为 ERROR；同一库的结果 fan-out 给所有等待的 operation。
6. **失败隔离与去重**：DDL 抓取失败/非 UTF-8 只计数不删已有 DDL；通知有小时桶 dedupe；退避有界（30s/2m/5m、1m/5m/15m）；限流饱和被识别为竞争而非失败（DB 级）。
7. **维护任务逐项隔离**：每个 cleanup 独立错误处理；删除语句幂等；OpenLineage 聚合的 grace + `FOR UPDATE SKIP LOCKED` + 锁序处理正确，避免了吞掉未提交 ingest 的引用与死锁。
8. **装配显式化**：lineage analyzer 是编译期列表 + 值传递，没有 `init()` 自注册/包级可变状态；server 显式注入每个消费者，测试可构造自己的引擎集合。

---

## 11. 核实情况与待验证事项

### 11.1 独立审稿人的核实结果

三份分报告共 54 条原始发现，经独立审稿人逐条**反向核实**（重新读当前代码、用 `go test -overlay` 注入临时测试验证解析行为、读 `conc` 源码确认 panic 语义、重复运行测试）：

| 统计 | 数量 | 说明 |
| --- | --- | --- |
| Confirmed | 44 | 代码事实成立 |
| PartlyWrong | 8 | AR-01、AR-02、SS-01、SS-06、LN-03、LN-09、LN-17、LN-18（结论成立但机制/消费者/严重度需修正） |
| Unverifiable | 2 | SS-08（多副本部署前提）、LN-19（同 GUID 多 object_type 共存需数据核对） |
| FalsePositive | 0 | 没有整条不成立；问题集中在**严重度系统性偏高**（24 条 High/Medium 中 21 条被降级）与跨报告重复 |

**本文处理方式**：接受大部分严重度校准（「影响」列反映单次触发后果，「优先级」按修复收益/成本重排）；只对 R-01/R-02 保留 P0——它们是静默数据丢失/永久功能退化且修复局部。已修正的 4 处机制/消费者描述：SS-01 的 VIEW/MV `meta_guid` 支路（会命中，不是 no-op）、SS-06 的「Explain SQL/MCP 读统计」（不成立，仅前端渲染）、LN-18 的「TargetType 留 0」（不成立，构造时已预置 `metaType`）、LN-17 的「第三方 analyzer 漏设 IsTemp」（契约违约假设，runner 遵守 `model/relation.go`）。重复项已合并：AR-16≡SS-02、AR-13≡LN-05、AR-02≡SS-08≡LN-20、AR-01≈LN-02。

### 11.2 逐项状态

| 项 | 状态 | 说明 |
| --- | --- | --- |
| R-01 机制与调用点 | Confirmed | `UpdateInstance` 整份写 JSONB；runner 克隆跨网络调用；API 数据源 CRUD 同模式；三份分报告均漏，Lead 补 |
| R-02 方言分支与失败链 | Confirmed | MariaDB 注册、视图裸 body、`default` 无引用、`markAnalysisFailed` 写 hash 均已核对；审稿人用 overlay 测试实测 `CREATE VIEW order AS …` 在 MariaDB/StarRocks 解析失败 |
| R-03 竞态窗口 | Confirmed（机制） | 触发需要「删除恰逢分析」，概率低；未在集成环境构造 |
| R-04 hash 覆盖 | Confirmed | `UpsertColumnLineageVersion` 的 `DO UPDATE SET meta_hash = EXCLUDED.meta_hash`；另会为已删除对象留下孤儿 version 行 |
| R-05/R-06/R-07/R-08 | Confirmed | 调用点、写序、失败路径逐行核对；R-06/R-07 的影响是延迟/噪音，不是数据损坏 |
| R-09 统计字段 | Confirmed（UI 层） | 未在真实库验证展示值；后端无消费者，仅前端两处渲染 |
| R-10 no-op 规模 | Confirmed（结论） | 分报告「VIEW/MV 的 meta_guid 支路也是 no-op」不准确，已修正；与 `schemasync.md` 已知问题 3 合并 |
| R-11 panic 隔离 | Confirmed（代码） | `conc` 的 `Wait` 会 re-panic；但未找到可稳定触发的 panic 路径 → 防御性缺口 |
| R-12 多副本 | 已决策：单副本（2026-10-18） | 不做 lease；只补 `security-posture.md` 的进程内状态清单，属文档债 |
| R-13 锁不可取消 | Confirmed | `sync.Mutex.Lock` 不响应 ctx；确定影响是 shutdown 噪音/延迟，不是无限期停摆 |
| R-14 重分析 | Confirmed（频率依赖上报节奏） | 已存在语句也入队；「每 10 秒」不是代码保证，而是轮询上报场景的推导 |
| R-17 analyzer 版本 | Confirmed（设计缺口） | 已决策无历史兼容 → 直接加列 + 部署后自动全量重算 |
| R-18 maintenance | Confirmed | 零测试与 E12 已登记重合；新增点是超时/摘要/setting 失败语义 |
| R-19 trigger 合并 | 设计缺陷 Confirmed；flake 未稳定复现 | 评审中 `-race -count=10` 全包观测 1 次失败；审稿人 610+ 次隔离运行 0 失败 → 判 fragile-by-design |
| R-20 DDL 全量抓取 | Confirmed | 降级模式陈旧是文档已知问题 6；全量成本为新补充 |
| R-25 GUID 冲突 | 待验证 | 需要构造含字面量 `%3B` 或 `;` 的对象名；机制成立 |
| R-28 限流饱和通知 | Confirmed（机制） | 饱和是否常见取决于部署规模，未实测 |
| R-29 重复边 | 条件性 | 单副本下仍有一个真实窗口：ingestion 的第二个 Processor vs revalidation；多副本路径按决策 1 删除 |
| R-31/R-32/R-33 | Confirmed（审稿人补） | 批量 API 无 per-RPC 超时、validation 无启动首轮、stale `now` |

### 11.3 已确认的决策（2026-10-18，产品/负责人确认）

| # | 决策 | 对本文的调整 |
| --- | --- | --- |
| 1 | **不考虑多副本**（单副本部署） | R-12 从「部署相关」降为 **P3 文档债**：不做 leader election / advisory lock，只把 runner 的进程内状态补进 `docs/security-posture.md` 的单副本清单。R-29 的多副本路径删除，但**同进程内**仍保留一个真实窗口（ingestion 的第二个 `openlineage.Processor` 不持 `revalidateMu`）；修法简化为「加唯一约束 + `ON CONFLICT DO NOTHING`」，不需要跨进程锁。 |
| 2 | **`SyncInstance` 保持同步执行**，暂不做异步 | R-01/R-05/R-06 的修法不变（定向 patch、实例 gate、LastSyncTime 后移）。R-31 的修法改为：**不转异步**，而是给 Connect/RPC 链路加 per-RPC deadline、限制 `BatchSyncInstances` 的单次总预算/实例数，并在文档写明最坏耗时。 |
| 3 | **统计字段可以异步刷新，但不得计入历史、不得更新历史** | R-09 的修法确定为：新增独立的 stats-only 更新路径，只 `UPDATE` 当前 `meta_registry_resource.metadata`（或独立的 stats 存储），**不写 `meta_registry_resource_history`、不改变 metahash、不触发血缘重分析**；异步节奏可独立于 schema sync（如按小时/天），UI 继续读当前值。现有「stats-only 变化不算 update」的测试断言需要改写。 |
| 4 | **不需要历史兼容**（项目尚无用户；重算可接受） | R-17 的修法简化为：`column_lineage_version` 直接加 `analyzer_version` 列（增量迁移 + `LATEST.sql`），部署后 `queueAll` 因版本不等自动全量重算，**无需数据回填/双写**；R-03/R-04 的 CAS/原子写也可以直接改 schema 与 store 接口，不必保留旧调用路径。R-25 的 GUID 转义也可以直接改成单射编码，无需兼容旧 GUID（但迁移时要重建 registry 或清空重同步）。 |

**仍然开放、需要后续产品决策的两点**：

1. **统计刷新频率与存储位置**（决策 3 已定「异步、不入历史」，但具体是 `meta_registry_resource.metadata` 旁路更新，还是单独 stats 表/列，需要一次 schema 设计）。
2. **R-14 的上报语义**：是否让 `ReportApplicationSQLs` 返回「新增/已存在」标记（需要改 store 返回契约）；若暂不改，runner 侧用 `column_lineage_version` + metahash 跳过也可，但要接受一次额外查询。


---

## 12. 附录

### 12.1 文件索引

| 关注点 | 代码 | 本文条目 |
| --- | --- | --- |
| 同步调度/队列/checker | `backend/runner/schemasync/syncer.go` | R-01,R-05,R-06,R-07,R-08,R-10,R-11,R-22,R-23,R-24,R-27,R-28 |
| 实例同步 | `syncer.go:471-556` | R-01,R-05,R-06,R-24 |
| 单库同步/事务 | `syncer.go:558-824` | R-03,R-10,R-27 |
| operation 聚合 | `backend/runner/schemasync/operation.go` | R-08,R-21,R-23 |
| DDL 抓取/写入 | `backend/runner/schemasync/object_definition.go` | R-20 |
| 血缘分析 | `backend/runner/lineageanalyzer/analyzer.go` | R-02,R-03,R-04,R-15,R-16,R-17,R-26 |
| 应用 SQL 分类 | `analyzer.go:552-619` | 正面观察；Info 3 |
| sync 后 revalidation | `backend/runner/lineagevalidation/validation.go` | R-13,R-19 |
| 维护清理 | `backend/runner/maintenance/maintenance.go` | R-18 |
| 装配/生命周期 | `backend/server/server.go` | R-11,R-12 |
| 批量同步 API | `backend/api/v1/instance_service.go` | R-31 |
| 实例写路径 | `backend/store/instance.go`、`backend/api/v1/instance_service.go` | R-01 |
| 血缘写入 | `backend/store/column_lineage.go`、`LATEST.sql` | R-03,R-04,R-29 |
| 元数据指纹 | `backend/store/meta_hash.go` | R-09 |
| GUID | `backend/common/guid.go` | R-25 |

### 12.2 分报告来源映射

| 分报告 | 文件 | 对应本文条目 |
| --- | --- | --- |
| AR-01～AR-16 | `/tmp/runner-review/findings-arch.md` | AR-01→R-29；AR-02→R-12；AR-03→R-11；AR-04/05/06/09→R-18；AR-07→Info 9；AR-08→Info 1；AR-10/11→Info 5；AR-12→Info 8；AR-13→R-15；AR-14→Info 7；AR-15→Info 6；AR-16→R-05 |
| SS-01～SS-16 | `/tmp/runner-review/findings-schemasync.md` | SS-01→R-10；SS-02→R-05；SS-03→R-06；SS-04→R-07；SS-05→R-08；SS-06→R-09；SS-07→R-23；SS-08→R-12；SS-09→R-11；SS-10→R-21；SS-11→R-22；SS-12→R-24；SS-13→R-25；SS-14→Info 10；SS-15→Info 1/2；SS-16→Info 5/R-27 |
| LN-01～LN-22 | `/tmp/runner-review/findings-lineage.md` | LN-01/LN-08→R-02；LN-02→R-03；LN-03→R-14；LN-04→Info 8；LN-05→R-15；LN-06→R-16；LN-07/LN-13→R-04；LN-09→R-13；LN-10→R-17；LN-11→Info 9；LN-12→R-04（错误分类）；LN-14→R-26；LN-15→R-19；LN-16→R-11；LN-17/LN-18/LN-22→Info 3/4；LN-19→Info 3；LN-20→R-29；LN-21→Info 6 |
| 审稿人补项 | `/tmp/runner-review/verification.md` | 批量 API 超时→R-31；validation 无启动首轮→R-32；stale `now`→R-33；storeError 孤儿 version 行→R-04 |

> 分报告包含更长的证据片段、逐条测试缺口与「待验证」说明；本文是合并、去重与严重度校准后的版本。原始分报告在评审会话的 `/tmp/runner-review/` 下，未提交（临时文件）。
