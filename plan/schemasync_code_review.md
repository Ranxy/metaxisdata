# Schema Sync（schemasync）代码评审

> 评审范围：`backend/runner/schemasync` 全量代码及其依赖（store、db 驱动接口、API 调用方、state 限流器）。
> 评审基线：代码可构建，`go vet` 与现有测试均通过。

## 总评

这个包的质量并不差——从 git log 能看到它经过多轮认真的修复（连接限流、digest 化 diff、规范化哈希指纹、panic 恢复、软删除日志化等），注释对不变量的解释也比较到位。但正因如此，残留的问题更偏向**结构性/语义性**的，而非低级错误。

## 一、真正的问题（按严重程度排序）

### 1.【中-高】元数据写事务横跨网络抓取，行锁被长时间持有

> **已修复**：`SyncDatabaseSchema` 现在把 digest 读取、`bmc.prepare()` 纯计算 diff、DDL 抓取与 DDL diff 全部放在 `BeginTx` 之前，事务内只剩纯写（元数据 → DDL 子集 → 血缘删除）。`batchMetaCreate` 拆为 `prepare()`/`write()`，`syncObjectDefinitions` 拆为 `prepareObjectDefinitions()`/`objectDefinitionBatch.write()`。

`SyncDatabaseSchema`（syncer.go:415）的执行顺序是：

```
BeginTx (L442) → 读 digest (L453，独立只读 tx) → bmc.Run (L543，批量 upsert/delete，已拿行锁)
→ syncObjectDefinitions (L553，向目标库逐对象发起 SHOW CREATE，8 并发)
→ 逐对象血缘删除 (L564) → Commit (L570)
```

对 `meta_registry_resource` 的写入锁**拿在手里穿越了对目标实例的全部网络往返**（最多 2000 个对象，共享 15 分钟 deadline）。大库上一次同步可能让 PG 事务开启数秒到数分钟，放大 vacuum 膨胀与锁等待风险；且 fetch 失败会回滚掉前面已完成的批量写入，下次再全部重做。

而这是不必要的——digest 读取本来就开独立事务（L453），`bmc.diff()` 是纯计算，`fetchObjectDefinitions` 也不依赖事务。

**建议**：把"读 digest → diff → 抓取 DDL → diff DDL"全部挪到 `BeginTx` 之前，事务内只做纯写。语义完全不变。

### 2.【中】`sync_databases` 白名单与软删除耦合，语义有坑且路径间不一致

> **已修复（决策：删除该字段）**：git 考古确认 `sync_databases` 是初始导入（root commit `e390a28`）就带来的继承字段，产品面（前端 / CLI / spec）从无读写入口，本地部署 4 个实例全部为空。已整体删除：后端移除 `filterSyncedDatabases` 与 `SyncInstance` 的过滤（软删改用完整快照）、`UpdateInstance` 的 `sync_databases` mask 分支、`instance_convert` 的两处透传；proto 两处字段（store 9 / v1 15）直接删除并重新生成。项目未上线，无需 reserved，也无需处理历史 JSONB 数据（`ProtojsonUnmarshaler` 是 `DiscardUnknown: true`）。删除后语义为"目标快照是 source of truth，不在快照里才软删"，三条入口的差异随之消失。

软删除集合来自**过滤后**的快照（syncer.go:381-390）：

```go
filteredDatabaseMetadatas := filterSyncedDatabases(...)   // 白名单过滤
snapshotNames := ... filteredDatabaseMetadatas ...         // 以此判定"missing"
```

后果：实例配置了白名单 `[app]` 后，**所有不在名单内的已存库会在下一次实例同步时被软删**（`deleted=true`，从 UI 消失）。proto 注释只说 "Enable sync for the following databases"（proto/store/store/instance.proto:28-30），把"不同步"实现成"对用户隐藏"是个语义陷阱——尤其当用户先同步了 50 个库、事后才加白名单时，其余 49 个会悄然消失。

路径间的不一致：

- `trySyncAll` 的库循环（syncer.go:199-214）和 `SyncAllDatabases`（syncer.go:217-236）**都不检查白名单**——白名单外的库在被软删前仍会被排队同步；API 的 `EnableFullSync` 路径直接全量同步，白名单形同虚设；
- 而 `SyncInstance` 路径却用白名单做软删除。三条入口三种行为。

### 3.【中】同一数据库可被并发同步

checker 是"出队即删"（syncer.go:113），而 API 的 `SyncDatabase`（api/v1/database_service.go:43）直接同步调用 `SyncDatabaseSchema`——**没有 per-database 的 in-flight 去重**。同一数据库可能一个被 checker 跑、一个被 API 跑：

- digest 读取与写入跨事务非原子，两边都基于旧视图做 diff；
- 大多数情况结果收敛（upsert 幂等），但"对方刚创建的行被我当 deletes 清掉"的窗口是存在的。

建议：加 per-database 的 sync.Map 去重（类似限流器）。

### 4.【低-中】`SyncDatabaseSchema` 不检查 `Deleted`，成功路径无条件复活

syncer.go:598-609 在成功提交后无条件写 `Deleted: proto.Bool(false)`。排队中的条目若刚被 `SyncInstance` 标记软删（快照抖动、权限变更），这次同步会把它**复活**，下一轮实例同步又标记删除——产生 flap。

建议：入口加 `database.Deleted` 短路判断。

### 5.【低】失败重试延迟大、无退避

只有连接耗尽会重新入队（syncer.go:132-137）；其余错误的库要等 15 分钟 tick 且 interval 到期才被重新排队。作为周期任务可接受，但刚失败的库很可能立刻再失败，建议简单退避。

### 6.【低】CreateInstance 双开 driver 且绕过限流

api/v1/instance_service.go:148-167 先开一个 admin driver 只为 fail-fast，随后 `SyncInstance` 内部又开一个。外层那个**不受实例连接限流器计数**，绕过了 "every driver path goes through the limiter" 的设计意图。

### 7.【低】API 同步路径最长可挂起 15 分钟

`SyncInstance` / `SyncDatabase` 都在请求 goroutine 里同步执行，内部 deadline 是 `syncTimeout = 15min`。目标库 hang 时 HTTP 请求挂 15 分钟，前端体验接近卡死。建议 API 路径用更短超时，或改异步 + 轮询。

## 二、可优化点

| 项 | 位置 | 说明 |
|---|---|---|
| 血缘清理 2N 次往返 | syncer.go:564-568 | 逐对象 2 条 DELETE，可合并为基于 `= ANY($1)` 的两条批量语句（参考 store 的 unnest 配对写法） |
| checker 空转 | syncer.go:85 | 每 10s 无条件 `ListInstances`，队列为空时纯浪费；可先 `Range` 判空 |
| 7 段重复代码 | syncer.go:463-533 | 表/视图/物化视图/序列/函数/过程/外表近乎相同的 7 个块，可表驱动化；L487 未复用 `schemaGUIDPrefix`，与邻居不一致 |
| 冗余 proto.Clone | syncer.go:754 | 每列 Clone 一次，但 `CalcMetaHash` 内部已 Clone、后续无可变路径 |
| 白名单 O(n·m) | syncer.go:316 | `slices.Contains` 嵌套循环，名单大时改 map |
| 死逻辑 | syncer.go:339-341 | `if instanceMeta.Version != ...` 与无条件赋值等价，`UpdateInstance` 反正都会调用 |
| deadline 写法 | syncer.go:291, 435 | `context.WithDeadline(time.Now().Add(syncTimeout))` 可简化为 `WithTimeout` |
| 共享 deadline 预算 | syncer.go:435 | 同一 deadlineCtx 覆盖 `SyncDBSchema` 和 DDL 抓取——前者慢会挤压后者（失败保留旧 DDL，不致错，但值得知晓） |
| 降级模式 DDL 永久过期 | object_definition.go:26-33 | >2000 对象时只刷新变更对象，StarRocks 属性变更不动 metadata 哈希 → DDL 可能永久过期。注释已承认，可考虑轮换批次补刷 |

## 三、历史债务

包的演进是典型的 **patch-on-patch**，残留印记明显：

1. **过时的循环变量拷贝**：`database := database`（syncer.go:200）是 Go 1.22 前的习惯，现在冗余（同函数里 instance 循环就没写）。
2. **过时注释**：`Run` 的注释说 "will run the schema syncer **once**"（syncer.go:58），实际是永久循环。
3. **导出的包私有常量**：`MaximumOutstanding = 100`（syncer.go:36）导出且无 doc，仅包内使用，应为 `maximumOutstanding`。
4. **宽返回签名**：`SyncInstance` 返回 4 元组，调用方大量丢弃（syncer.go:180 是 `_, _, _, _`），说明签名演进后没收敛。
5. **投机性通用**：`supportsObjectDefinition`（object_definition.go:104）预留了 FUNCTION/PROCEDURE，但目前只有 StarRocks 一个驱动实现了 `ObjectDefinitionReader`。
6. **修复叙事型注释**：多处 "used to …" 注释（digest 化、二次扫描、allowlist 返回值等）。对理解很有帮助，但也说明这里是事故驱动的热点区域——每次修 bug 都在旧结构上叠加，而非重构。
7. **store 层 V2 清理**（commit 8b328ae）等大扫除说明这块经历过更大范围的返工。

## 四、建议的处理优先级

1. ~~**先做 #1（事务内挪出网络抓取）**——改动小、收益明确，是真正的生产隐患~~ 已完成
2. ~~**#2（白名单语义）**需要先决定产品语义：白名单到底是"同步范围"还是"可见范围"？然后统一三条入口~~ 已完成：字段整体删除，软删改用完整快照
3. #3、#4 花十几行就能补上防护
4. 其余属于顺手清理，可在下一次动这个包时一起做

## 附：正面观察

- 不变量有 guard test 保护（digest diff、哈希规范化、限流器、shouldSyncNow 边界）；
- AGENTS.md 明确记录"破坏性 schema sync 仅记日志"为有意的设计决定（logSchemaSyncDeletion 是唯一痕迹）；
- 缓存失效严格放在 commit 之后；DDL 行严格作为 meta 行的子集维护；
- 哈希指纹使用自有 canonical 编码，规避了 protojson 按镜像随机化导致的全表重写问题（commit 166766e）。
