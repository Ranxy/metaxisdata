# 06 · 后台 Runner 与 Migrator

**范围**：`backend/runner/schemasync/`、`backend/runner/lineageanalyzer/`、`backend/migrator/`（含 `migration/LATEST.sql`）。

**结论**：迁移器的事务原子性本身是好的（DDL + ledger 在同一事务内），但 `LATEST.sql` 与 Go 查询不一致（`db_schema` 缺失导致 `table` 过滤必然失败），且**完全没有增量迁移目录**——schema 变更只改 `LATEST.sql` 会让已有部署静默落后。schemasync 有两个高影响生命周期 bug（检查协程一次瞬时错误即永久退出；停用实例仍全量同步）。血缘分析器会丢失失败任务、对不支持引擎无限重试、每小时 N+1 全表扫描。

**阶段 0 更新**：本层**未涉及任何改动**（阶段 0 只动 auth/授权/filter/审计/panic/setting）。本文件全部条目仍待阶段 1/2 处理。

---

## 严重（Critical）

### R-C1. `LATEST.sql` 缺少 `db_schema`，而 `ListDatabase` 的 `table` 过滤 join 它
- **位置**：`backend/migrator/migration/LATEST.sql`（仅此一个文件）、`backend/store/database.go:368`
- **证据**：`joinQuery = "INNER JOIN db_schema ds ON db.instance = ds.instance AND db.name = ds.db_name"`，当 filter 含 `ds.metadata->'schemas'` 时触发；而 API 的 `table = "x"`/`table.matches("x")` 恰好生成该片段（`database_service.go:832-841,875-880`）。仓库中不存在 `db_schema` 表。
- **影响**：任何带 `table` 过滤的 `ListDatabases` 报 `42P01 relation "db_schema" does not exist`。公开 API 功能 100% 损坏。
- **修复**：改为从 `db.metadata->'schemas'` 读取；或真正补上 `db_schema` 表**并**提供填充它的增量迁移。

---

## 高（High）

### R-H1. DB 同步检查协程遇到一次瞬时错误即永久退出
- **位置**：`backend/runner/schemasync/syncer.go:91-97`
- **证据**：
  ```go
  if err != nil {
      if err != nil {
          slog.Error("Failed to list instance", log.WithError(err))
          return
      }
  }
  ```
- **影响**：一次瞬时 store 错误（连接池耗尽、主备切换）即永久停止所有数据库级 schema 同步，直到进程重启；实例级 ticker 仍在跑，看起来"活着"。
- **修复**：改为 `continue`（只记一次日志），删除重复的 `if`。

### R-H2. 停用的实例仍然全量同步其数据库
- **位置**：`backend/runner/schemasync/syncer.go:210-217`（实例循环的 guard 在 `:167-170`）
- **证据**：数据库循环只做 `nextSyncTime := lastSyncTime.Add(interval); if now.Before(nextSyncTime) { continue }`。`interval == 0` 时 `nextSyncTime == lastSyncTime`，任何过去的同步时间都会通过。
- **影响**：`activation=false`（文档在 `:36-37` 写明"never sync"）的实例仍会打开外部 admin 连接并重写注册表，每个 tick 一次。
- **修复**：在计算 `nextSyncTime` 前加 `if interval == defaultSyncInterval { continue }`。

### R-H3. driver 快照为空/不完整时会执行破坏性删除
- **位置**：`backend/runner/schemasync/syncer.go:637-639`（以及 `:344-356`）
- **证据**：`for _, item := range existMap { deletes = append(deletes, item) }`，无任何合理性检查；`SyncDatabaseSchema` 直接存储 driver 返回的内容并删除所有不在其中的受管资源。
- **影响**：部分/权限受限/瞬时为空的同步结果会删除该库所有表、列、视图、schema 及其 `column_lineage`（删除路径已提交）。实例级循环对不完整 `instanceMeta.Databases` 做软删除，同样会停止后续同步。
- **修复**：当新快照为空或数量异常收缩时拒绝执行删除；引入显式 full-sync 标志或先比较对象数量。

### R-H4. `LastSyncTime` 在元数据事务之外提交
- **位置**：`backend/runner/schemasync/syncer.go:520-532`
- **证据**：`s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{...MetadataUpdates...})` 在自己的连接/事务上执行（`store/database.go:268-292`），在 `:529` 的 `tx.Commit()` 之前返回。
- **影响**：若注册表事务回滚或提交失败，`db` 行已记录"刚同步"：该库会被跳过长达一个同步间隔，`:536-540` 排队的视图分析也永远不会入队。
- **修复**：提供接受 `tx` 的 `UpdateDatabase`，或在提交成功后再更新 `LastSyncTime`。

### R-H5. 没有增量迁移目录：新装与升级会静默分叉
- **位置**：`backend/migrator/migration/`（只有 `LATEST.sql`）、`backend/migrator/migrator.go:20-27,233-267`
- **证据**：`ls backend/migrator/migration` 只有 `LATEST.sql`，`getSortedVersionedFiles` 恒返回空。AGENTS.md 要求每次变更**同时**改 `LATEST.sql` 与 `migration/{MAJOR.MINOR}/`。
- **影响**：只改 `LATEST.sql` 的 schema 变更永远不会到达已有部署（停在 0.1.0，无可应用增量），而全新安装却带上它——store 查询只在升级过的实例上失败。没有测试/CI 校验 `LATEST.sql` 与增量链一致。
- **修复**：创建 `migration/0.1/` 基线增量；增加 guard 测试/CI 检查（`LATEST.sql` 变更必须伴随新增量）。

### R-H6. advisory lock 可能泄漏，永久阻塞其他副本
- **位置**：`backend/migrator/migrator.go:112-119`
- **证据**：`defer func() { conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", ...) }()` 使用会被取消的 `ctx`；获取时也没有 `lock_timeout`。
- **影响**：启动期间收到 SIGTERM 或 unlock 失败时，连接带着会话级锁回到连接池；下一个副本的 `pg_advisory_lock` 会无限阻塞。同一会话重复获取还会累加锁计数。
- **修复**：用不可取消的新 context 解锁；获取时设置 `lock_timeout`；解锁失败则销毁该连接。

---

## 中（Medium）

- **M1. `tableExists` 忽略 `table_schema`**：`migrator.go:376-378`，任意 schema 下存在同名表就让迁移器认为"已存在部署"，空 public schema 会被跳过 `LATEST.sql`，随后记录基线并在缺表状态下运行。应加 `table_schema = current_schema()` 或改用 `to_regclass`。
- **M2. 分析失败要等到下一个小时级扫描才重试**：`analyzer.go:129-138`，`drainAndAnalyze` 先删除 `analyzeMap` 全部 key 再分析，失败只记日志；血缘可陈旧长达 `lineageAnalysisInterval`（1h）。
- **M3. 不支持的引擎从不标记为已分析 → 每小时无限重试**：`analyzer.go:204-206`，`ErrorEngineNotSupported` 分支直接 `return nil`，没有 `markAnalyzed`；`queueAll` 每小时重新入队并重试所有视图/MV/manual SQL。
- **M4. `queueAll` 是 N+1 全表扫描，且 `meta_registry_resource.object_type` 无索引**：`analyzer.go:105-124` + `LATEST.sql:157-165`；每小时 3 次未索引扫描 + 每对象一次查询。
- **M5. 为比较 hash 而全量加载并反序列化元数据**：`syncer.go:392` + `store/meta_resource.go:340-354`，`ListMetaRegistry` 带 `withMetadata=true` 解析每个 JSONB 行，而 `diff()` 只需要 `GUIDKey` + `MetaHash`。建议增加轻量列表（guid, object_type, meta_hash）。
- **M6. 每实例连接限流被 `SyncInstance` 与 API 触发的同步绕过**：`syncer.go:120-128`、`api/v1/database_service.go:63`、`api/v1/instance_service.go:516`；限流只在 10s 的 DB 检查器里生效，而每个 driver 会开 `SetMaxOpenConns(50)`（`plugin/db/mysql/mysql.go:89`）。
- **M7. 表/列被删除后血缘行从不清理**：`syncer.go:504-511` 只处理 VIEW/MV，drop 表后依赖视图的 `column_lineage` 仍指向不存在的 GUID。
- **M8. 同步失败只用 Debug 级别记录**：`syncer.go:131-135,181-185`，生产 info 级别下永久失败的实例/库完全不可见，无指标无告警。
- **M9. worker pool 中的 panic 会打挂进程**：`syncer.go:125-139`、`analyzer.go:95,143-155`；`conc/pool` 会把任务 panic 传播出 `Wait()`，只有 `trySyncAll` 有 recover。
- **M10. Shutdown 的 WaitGroup 等待无超时**：`backend/server/server.go:168`（见 `01` M1）。
- **M11. `SyncInstance` 返回未过滤的数据库列表**：`syncer.go:322-342,358`，构建了遵守 `sync_databases` 的 `filteredDatabaseMetadatas`，却返回 `instanceMeta.Databases`；`SyncInstance` RPC（`instance_service.go:528-530`）会报告未同步的库。
- **M12. `principal.email` 无 UNIQUE/NOT NULL 保护**：`LATEST.sql:18-31`（见 `03` S-H6）。
- **M13. 旧二进制对着更新的 ledger 静默运行**：`migrator.go:160-194` 只检查 `f.version.LE(*recorded)`；当 `recorded` 大于内嵌最新版本时，不迁移也不报错，只打印内嵌版本号。应显式报错拒绝启动。

---

## 低（Low）

- `syncer.go:306-308` 冗余的版本判断（`if instanceMeta.Version != instance.Metadata.GetVersion() { metadata.Version = instanceMeta.Version }`）。
- `syncer.go:363` 陈旧 TODO（下方代码已实现该功能）。
- 死字段/参数：`syncer.go:54,58,362`（未使用的嵌入 `sync.Mutex`、`profile`、未赋值的 `retErr`）、`analyzer.go:41,387`（未使用的 `profile`、`markAnalyzed` 的未用参数）。
- `migrator.go:82-92` 空的 `goMigrations`/`GoMigrationFunc` 脚手架，无测试。
- `getVersionFromPath` 接受畸形版本（`migrator.go:283-288`，`00001`、负数、任意 `MAJOR.MINOR` 目录）。
- 重复迁移版本未提前检测（`migrator.go:258-266`）。
- `adoptLegacySchema` 不校验基线形状（`migrator.go:202-223`）。
- store 缓存在调用方事务提交前就被更新：`store/meta_resource.go:970-973`（由 `syncer.go:591` 调用）；当前被 `enableCache=false` 掩盖。
- COLUMN 元资源只为 TABLE 创建：`syncer.go:718-725` vs `store/meta_resource.go:1051-1052`，导致 `ListSublevelMetaRegistryResource` 在视图下返回不了列（需确认 UI 是否直接读 `viewMetadata.columns`）。
- `SyncInstance` 中的 O(n²) 名称查找：`syncer.go:326,345` 使用 `slices.IndexFunc`。
- 未知实例的 `databaseSyncMap` 无界增长：`syncer.go:109-115`，找不到实例时 `return true` 保留条目，每 10s 重试并重复记日志。
- GUID 分隔符碰撞：`syncer.go:727-729` 用 `";"` 连接，`analyzer.go:161` 用 `SplitN(..., 4)`；库/schema/表名含 `;` 会产生歧义 GUID。

---

## 死代码与遗留债务

- `syncer.go:363` 陈旧 TODO；未使用的 `Syncer.profile`、嵌入 `sync.Mutex`、`Analyzer.profile`；未使用的 `retErr`；`markAnalyzed` 的未用参数。
- 空的 `goMigrations`/`GoMigrationFunc` 注册表（`migrator.go:82-92`）。
- 重复的血缘删除 SQL：runner 的 4 参 `deleteColumnLineageByMetaTx`（`syncer.go:776`）与 store 的 3 参版本（`store/manual_sql.go:765`）；store 版本有 GUID-subtree 感知 helper，而 runner 的硬编码 SQL 绕过了它。
- 泛滥的无意义 `V2` 后缀（`ListInstancesV2`、`UpdateInstanceV2`、`GetInstanceV2`、`StoreMetaResourceV2`），且不存在对应的 V1。
- `syncer.go:92-97` 重复嵌套 `if err != nil`。
- 硬编码的间隔/上限（`instanceSyncInterval`、`databaseSyncCheckerInterval`、`syncTimeout`、`MaximumOutstanding`、`MaxGoroutines`、`lineageAnalysisInterval`、`analyzeCheckerInterval`），而注入的 `profile` 却未被读取。
- **缺失 `migration/0.1/` 目录本身是最大的遗留债务**（见 R-H5）。
- 引用不存在表的遗留 store 路径：`db_schema`（`store/database.go:368`，可达）、`DeleteProject`（`store/project.go:364`）、`CountIssues`（`store/stats.go:126`）。

---

## 待确认

1. `driver.SyncDBSchema` 是否可能在不报错的情况下返回空/部分 schema（权限受限）？这决定 R-H3 的现实概率。
2. `table` CEL 过滤前端是否在用？无论如何 proto 已声明支持。
3. UI 是走 `ListSublevelMetaRegistryResource`（视图列缺失）还是直接读 `ViewMetadata.columns`？
4. 迁移原子性本身可靠：`executeMigration`（`migrator.go:314-350`）把 DDL + ledger 插入放在一个事务里，`LATEST.sql` 也是单事务；失败的增量会保留更早的增量，符合 forward-only 设计。
5. `migrator_integration_test.go` 没有并发/advisory-lock 测试、没有重复版本测试、也没有"`LATEST.sql` 等于增量累积结果"的校验——正是能捕获 R-H5 的 guard。
6. `BatchCreateMetaRegistryResourceAt` 把 `RETURNING id` 按位置写入 `creates[i]`（`store/meta_resource.go:934-953`），PG 不保证顺序；当前影响低（删除不使用这些 ID），需验证。
