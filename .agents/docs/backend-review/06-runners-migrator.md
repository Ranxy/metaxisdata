# 06 · 后台 Runner 与 Migrator

**范围**：`backend/runner/schemasync/`、`backend/runner/lineageanalyzer/`、`backend/migrator/`（含 `migration/LATEST.sql`）。

**结论**：迁移器的事务原子性本身是好的（DDL + ledger 在同一事务内），但 `LATEST.sql` 与 Go 查询不一致（`db_schema` 缺失导致 `table` 过滤必然失败），且**完全没有增量迁移目录**——schema 变更只改 `LATEST.sql` 会让已有部署静默落后。schemasync 有两个高影响生命周期 bug（检查协程一次瞬时错误即永久退出；停用实例仍全量同步）。血缘分析器会丢失失败任务、对不支持引擎无限重试、每小时 N+1 全表扫描。

**阶段 0 更新**：本层**未涉及任何改动**（阶段 0 只动 auth/授权/filter/审计/panic/setting）。

**阶段 1 更新**：R-C1（`db_schema`）✅、R-H1/R-H2/R-H4 ✅、R-H3 ◐（按产品决策只加日志，不改删除行为）、M8 ✅。R-H5（增量目录）经确认**不做**——当前没有待发布的 schema 变更，空增量目录只会是噪音；遗留的是"改 `LATEST.sql` 时容易忘记补增量"这一流程约束。R-H6、M1-M7、M9-M13 仍待阶段 2/3。

**阶段 2 更新**：R-H5 ✅（`migration/0.1/` 已建立：`0001##scope_explain_sql_cache.sql`、`0002##add_missing_indexes.sql`，`LATEST.sql` 同步，`8c34542` `ff9b22a`）、M4 部分 ✅（`object_type` 索引已加，`queueAll` 本身的 N+1 仍在）、M10 ✅（runner 等待加 10s 上限，`fb8ca14`）、低节"缓存在提交前更新"✅（事务内不再写缓存，提交后 `InvalidateMetaRegistryCache`，`f22f61e`）。R-H6、M1-M3、M5-M9、M11-M13 仍未处理。
**阶段 3 更新**：本层基本未动（阶段 3 的范围是死代码/重构/契约/CI）。**唯一相关**：`migration/` 双写流程在阶段 3 **没有新的 schema 变更**，因此 `LATEST.sql` 与 `migration/0.1/` 未变；`store` 侧删除 `stats.go` 的统计方法顺带移除了对不存在 `issue`/`project` 表的查询（`a39bc41`，见 `03`）。**仍未处理**：R-H6（advisory lock 用会被取消的 ctx 解锁、无 `lock_timeout`）、M1（`tableExists` 忽略 `table_schema`）、M2/M3（分析失败要等一小时、不支持引擎每小时重试）、M4 的 `queueAll` 批量化、M5-M9、M11（`SyncInstance` 返回未过滤列表）、M13（旧二进制对着更新的 ledger 静默运行）、`migrator` 集成测试仍未并入 CI。

**阶段 3 收尾更新**：`LATEST.sql` **仍无 schema 变更**，但同步了四条列/表注释——`setting.name` 的取值清单、`principal.mfa_config`（明确为无读写、无 proto 消息的遗留列）、`idp.type`（只有 OAUTH2 被实现，CHECK 保留以免既有行写失败）、`role`/`project`/`policy` 表（对应 store 消息已删除、无 Go 调用者，但 `db.project` 有外键约束）。`runner/schemasync/syncer.go` 删掉 `schema.Packages`/`schema.Streams` 两个永不会被 MySQL/PG 填满的循环，以及 `isSchemaSyncManagedMetaType`/`convertMetadataToGUID` 的 STREAM/PACKAGE 分支（`ddff264`）。

**阶段 3 续更新**：新增两个增量并在 `LATEST.sql` 同步（`904fb09` `451cb78`）：`0.1.0003##drop_dead_auth_schema.sql`（删 `role` 表与 `principal.mfa_config` 列，`idp_type_check` 收窄为 `('OAUTH2')`）与 `0.1.0004##drop_project.sql`（删 `db.project` 列与 `project` 表）。两者都在本地 PostgreSQL 16 实测：全新安装、模拟低版本→升级、重复执行为 no-op；`0003` 另验证了历史 OIDC 行会以 SQLSTATE 23514 显式失败。`runner/schemasync/syncer.go` 不再传 `ProjectID` 给 `CreateDatabaseDefault`（`451cb78`）；`BatchSyncInstances` 的 fail-fast 行为变化见 `04`（`733b3e0`）。

**阶段 3 补遗更新**：① `migrator/tableExists` 只按表名查 `information_schema`，而该视图覆盖库内所有 schema，别的 schema 里的同名表会被误认为元数据 schema 已存在、从而跳过全新安装路径；改为限定 `current_schema()` 且 `BASE TABLE`（`2131420`）。② advisory lock 的解锁与 `lock_timeout` 复位改用不可取消的 context（ctx 被取消会让连接带着会话级锁回到连接池、之后所有迁移死锁），并给加锁本身加 `lock_timeout`，让卡住的同伴副本显式失败（`2131420`）。③ ledger 比二进制已知的最新版本新时拒绝启动，不再静默跑在不认识的 schema 上（`2131420`）。④ `analyzer.analyzeObject` 对没有 lineage analyzer 的引擎不再静默 `return nil`（那会让 `queueAll` 每小时无限重排），改为把这次跳过连同 meta hash 与原因写进 `column_lineage_version`（`9019140`）；`queueAll` 同时从「N 次全量 metadata 拉取 + N 次版本查询」改为「digest 列表 + 每类型一次版本查询」（`f50fbbc`）。⑤ 新增 `runner/maintenance`（启动时与每 6 小时）：清理过期 ExplainSQL 缓存行与 7 天前的 `llm_debug_log`；`--openlineage-retention-days`（默认 0 = 永久保留）开启时按保留期删除 run、重算 task 聚合、清理空 task 与镜像 registry 行（`cfa74df`）。⑥ 新增增量 `0.1.0005##metadata_search_index.sql`（`pg_trgm`、`meta_registry_search_text` 函数、`search_text` 生成列、三个索引），`LATEST.sql` 同步（`f50fbbc`）。

**阶段 3 收尾二更新**：① migrator 的集成测试过去每次运行都往容器里漏一个 `migrator_test_*` 库——`newTestDatabase` 用 `defer admin.Close()` 关掉管理连接池，而 `t.Cleanup` 的 `DROP DATABASE ... WITH (FORCE)` 在测试结束后才执行，错误又被 `_, _ =` 丢弃；现在管理池活到 DROP 之后，DROP 失败会让测试失败（`48d65be`）。② 同一文件不再自己用字符串匹配判断"没有 Docker"，改为复用 `backend/test/integration/dockerutil` 的探测与 sentinel，并新增共享的集成 target：`make test-integration`/`test-integration-smoke` 现在包含 `./backend/migrator/...`（CI 调用的就是 `make test-integration`），三条迁移路径（fresh install / upgrade / legacy adoption）第一次进入门禁；另外补上了文档里提到但 Makefile 中不存在的 `make test-integration-mysql`（`8823ac2`）。


**阶段 3 续更正**：上一段收尾更新的四条结论已被本轮取代——`LATEST.sql` 已有 `0003`/`0004` 两个新增量，不再是无 schema 变更；`principal.mfa_config` 列已删除；`idp.type` 的 CHECK 已收窄，不再是为兼容既有行而保留；`role`/`project` 表已 DROP，`db.project` 的外键随列一起删除，不再是删表的阻碍。

**阶段 5 更新**：`runner/maintenance` 的 OpenLineage 保留期不再来自 `--openlineage-retention-days` 这个 CLI flag（已删除），改为每轮执行时读取 `WORKSPACE_PROFILE.openlineage_retention_days`（`7870016`）——管理员在 `/settings/general` 修改后无需重启即可生效；`NewRunner` 因此不再需要 `*config.Profile`。本轮无 schema 变更（保留期是 JSONB 里的新字段，不需要迁移）。

---

## 严重（Critical）

### R-C1. `LATEST.sql` 缺少 `db_schema`，而 `ListDatabase` 的 `table` 过滤 join 它
> **✅ 已修复（阶段 1）** · `bb93ee0`：经确认 `db_schema` 是不需要的遗留代码——`db.metadata` 存的是 `DatabaseMetadata`（**没有 `schemas` 字段**），schema 树实际存在 `meta_registry_resource`，所以这个 `table` 过滤器从来就不可能工作。处理方式是**删除**而不是重写：`getListDatabaseFilter` 的 `table` 分支、store 里靠字符串嗅探注入 join 的 `joinQuery`、以及 proto 里 `ListDatabasesRequest.filter` 的 `table` 文档全部删除；`table == "x"`/`table.matches("x")` 现在返回 `InvalidArgument` 而非 `42P01`（守卫测试 `TestListDatabaseFilterRejectsTableFilter`）。`R-H5`（增量目录）另经确认不做，见文件开头阶段 1 更新。

- **位置**：`backend/migrator/migration/LATEST.sql`（仅此一个文件）、`backend/store/database.go:368`
- **证据**：`joinQuery = "INNER JOIN db_schema ds ON db.instance = ds.instance AND db.name = ds.db_name"`，当 filter 含 `ds.metadata->'schemas'` 时触发；而 API 的 `table = "x"`/`table.matches("x")` 恰好生成该片段（`database_service.go:832-841,875-880`）。仓库中不存在 `db_schema` 表。
- **影响**：任何带 `table` 过滤的 `ListDatabases` 报 `42P01 relation "db_schema" does not exist`。公开 API 功能 100% 损坏。
- **修复**：改为从 `db.metadata->'schemas'` 读取；或真正补上 `db_schema` 表**并**提供填充它的增量迁移。

---

## 高（High）

### R-H1. DB 同步检查协程遇到一次瞬时错误即永久退出
> **✅ 已修复（阶段 1）** · `fcb6a98`：嵌套的 `if err != nil` 与 `return` 改为单次 `slog.Error` + `continue`，一次瞬时 store 错误只跳过当前 tick。

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
> **✅ 已修复（阶段 1）** · `fcb6a98`：调度判断统一收敛到纯函数 `shouldSyncNow(interval, lastSyncTime, now)`，它显式拒绝 `defaultSyncInterval`（"never sync"），实例循环与数据库循环共用；守卫测试 `TestShouldSyncNowRejectsNeverSyncInterval`/`TestShouldSyncNowRespectsInterval`。注意仅修 `trySyncAll` 的排队路径——显式 API 触发的同步不受影响。

- **位置**：`backend/runner/schemasync/syncer.go:210-217`（实例循环的 guard 在 `:167-170`）
- **证据**：数据库循环只做 `nextSyncTime := lastSyncTime.Add(interval); if now.Before(nextSyncTime) { continue }`。`interval == 0` 时 `nextSyncTime == lastSyncTime`，任何过去的同步时间都会通过。
- **影响**：`activation=false`（文档在 `:36-37` 写明"never sync"）的实例仍会打开外部 admin 连接并重写注册表，每个 tick 一次。
- **修复**：在计算 `nextSyncTime` 前加 `if interval == defaultSyncInterval { continue }`。

### R-H3. driver 快照为空/不完整时会执行破坏性删除
> **◐ 部分修复（阶段 1，经确认后降级为可观测性）** · `fcb6a98`：**删除行为按产品决策保持不变**（"正常来说用户不会变更连接用户的权限，这个功能由管理员来控制"），只补日志：`SyncDatabaseSchema` 在删除前用 `logSchemaSyncDeletion` 记录条数、按对象类型的计数与前 20 个 GUID；`SyncInstance` 对从实例快照中消失的数据库记一条 Warn 并列出库名。因此"权限被收窄 → 快照变空/变少 → 静默清空注册表"仍是**可能发生**的，但现在一定会在日志里留下痕迹。**未做**：空快照时拒绝删除、按比例阈值拦截、显式 full-sync 标志。

- **位置**：`backend/runner/schemasync/syncer.go:637-639`（以及 `:344-356`）
- **证据**：`for _, item := range existMap { deletes = append(deletes, item) }`，无任何合理性检查；`SyncDatabaseSchema` 直接存储 driver 返回的内容并删除所有不在其中的受管资源。
- **影响**：部分/权限受限/瞬时为空的同步结果会删除该库所有表、列、视图、schema 及其 `column_lineage`（删除路径已提交）。实例级循环对不完整 `instanceMeta.Databases` 做软删除，同样会停止后续同步。
- **修复**：当新快照为空或数量异常收缩时拒绝执行删除；引入显式 full-sync 标志或先比较对象数量。

### R-H4. `LastSyncTime` 在元数据事务之外提交
> **✅ 已修复（阶段 1）** · `fcb6a98`：改为提交成功后才写 `LastSyncTime`；并把"排队被改动 VIEW/MV 的血缘分析"移到写 `db` 行之前，这样即使 `LastSyncTime` 更新失败，分析也已入队（否则下次同步哈希未变、永远不会再排队）。选择的是报告里的第二种修法（提交后写），没有引入接受外部事务的 `UpdateDatabase`。

- **位置**：`backend/runner/schemasync/syncer.go:520-532`（修复前的行号）
- **证据**：`s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{...MetadataUpdates...})` 在自己的连接/事务上执行（`store/database.go:268-292`），在 `:529` 的 `tx.Commit()` 之前返回。
- **影响**：若注册表事务回滚或提交失败，`db` 行已记录"刚同步"：该库会被跳过长达一个同步间隔，`:536-540` 排队的视图分析也永远不会入队。
- **修复**：提供接受 `tx` 的 `UpdateDatabase`，或在提交成功后再更新 `LastSyncTime`。

### R-H5. 没有增量迁移目录：新装与升级会静默分叉
> **✅ 已修复（阶段 2）** · `8c34542` `ff9b22a`：`migration/0.1/` 已建立并首次走完双写流程——`0001##scope_explain_sql_cache.sql`（`explain_sql_cache.scope`）与 `0002##add_missing_indexes.sql`（`meta_registry_resource(object_type)`、`principal(LOWER(email)) WHERE deleted=FALSE`），`LATEST.sql` 同步更新。**剩余**：仍没有自动校验"`LATEST.sql` 与增量链一致"的 guard 测试/CI（见 M13 与 `09`）。

- **位置**：`backend/migrator/migration/`（只有 `LATEST.sql`）、`backend/migrator/migrator.go:20-27,233-267`
- **证据**：`ls backend/migrator/migration` 只有 `LATEST.sql`，`getSortedVersionedFiles` 恒返回空。AGENTS.md 要求每次变更**同时**改 `LATEST.sql` 与 `migration/{MAJOR.MINOR}/`。
- **影响**：只改 `LATEST.sql` 的 schema 变更永远不会到达已有部署（停在 0.1.0，无可应用增量），而全新安装却带上它——store 查询只在升级过的实例上失败。没有测试/CI 校验 `LATEST.sql` 与增量链一致。
- **修复**：创建 `migration/0.1/` 基线增量；增加 guard 测试/CI 检查（`LATEST.sql` 变更必须伴随新增量）。

### R-H6. advisory lock 可能泄漏，永久阻塞其他副本
- **✅ 已修复（阶段 3 补遗，`2131420`）**：解锁与 `lock_timeout` 复位改用 `context.WithoutCancel`，加锁本身加 `lock_timeout`，卡住的同伴副本会显式失败而不是永久阻塞。
- **位置**：`backend/migrator/migrator.go:112-119`
- **证据**：`defer func() { conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", ...) }()` 使用会被取消的 `ctx`；获取时也没有 `lock_timeout`。
- **影响**：启动期间收到 SIGTERM 或 unlock 失败时，连接带着会话级锁回到连接池；下一个副本的 `pg_advisory_lock` 会无限阻塞。同一会话重复获取还会累加锁计数。
- **修复**：用不可取消的新 context 解锁；获取时设置 `lock_timeout`；解锁失败则销毁该连接。

---

## 中（Medium）

- **M1. `tableExists` 忽略 `table_schema`**：`migrator.go:376-378`，任意 schema 下存在同名表就让迁移器认为"已存在部署"，空 public schema 会被跳过 `LATEST.sql`，随后记录基线并在缺表状态下运行。应加 `table_schema = current_schema()` 或改用 `to_regclass`。 —— **✅ 已修复（阶段 3 补遗，`2131420`）**：查询限定 `table_schema = current_schema()` 且 `table_type = 'BASE TABLE'`。
- **M2. 分析失败要等到下一个小时级扫描才重试**：`analyzer.go:129-138`，`drainAndAnalyze` 先删除 `analyzeMap` 全部 key 再分析，失败只记日志；血缘可陈旧长达 `lineageAnalysisInterval`（1h）。 —— **✅ 已修复（阶段 6）** · `480b957`：失败任务重新入队（带退避与重试计数），不再等下一次小时级扫描；连续失败达上限记 Error 并保留失败版本记录。
- **M3. 不支持的引擎从不标记为已分析 → 每小时无限重试**：`analyzer.go:204-206`，`ErrorEngineNotSupported` 分支直接 `return nil`，没有 `markAnalyzed`；`queueAll` 每小时重新入队并重试所有视图/MV/manual SQL。 —— **✅ 已修复（阶段 3 补遗，`9019140`）**：把这次跳过连同当前 meta hash 与原因写进 `column_lineage_version`，在 metadata 变化前不再重排；同时补齐了四个引擎的 lineage/schema/driver 注册（`729db71`）。
- **M4. `queueAll` 是 N+1 全表扫描，且 `meta_registry_resource.object_type` 无索引**：`analyzer.go:105-124` + `LATEST.sql:157-165`；每小时 3 次未索引扫描 + 每对象一次查询。**部分修复（阶段 2，`ff9b22a`）**：增量 `0.1.0002` 已加 `object_type` 索引，扫描不再全表；`queueAll` 的逐对象查询 N+1 仍未批量化。 —— **✅ 已修复（阶段 3 补遗，`f50fbbc`）**：另加 digest 列表（不取 metadata、不碰缓存）与 `ListColumnLineageVersions`（一次取某类型全部版本），每类型 2 次查询，逐对象的那次查询与全量 metadata 解析都去掉了。
- **M5. 为比较 hash 而全量加载并反序列化元数据**：`syncer.go:392` + `store/meta_resource.go:340-354`，`ListMetaRegistry` 带 `withMetadata=true` 解析每个 JSONB 行，而 `diff()` 只需要 `GUIDKey` + `MetaHash`。建议增加轻量列表（guid, object_type, meta_hash）。 —— **✅ 已修复（阶段 3 补遗，`f50fbbc` `2259abd`）**：analyzer 与 syncer 都改走 `ListMetaRegistryResourceDigest`，只取 guid/object_type/meta_hash，不再解析每个 JSONB 行。
- **M6. 每实例连接限流被 `SyncInstance` 与 API 触发的同步绕过**：`syncer.go:120-128`、`api/v1/database_service.go:63`、`api/v1/instance_service.go:516`；限流只在 10s 的 DB 检查器里生效，而每个 driver 会开 `SetMaxOpenConns(50)`（`plugin/db/mysql/mysql.go:89`）。
- **M7. 表/列被删除后血缘行从不清理**：`syncer.go:504-511` 只处理 VIEW/MV，drop 表后依赖视图的 `column_lineage` 仍指向不存在的 GUID。 —— **✅ 已修复（阶段 6）** · `9c69196`：`syncer.go` 的删除清理改为对任何被删除对象都按 `meta_guid`/`source`/`target` 三个方向清理 `column_lineage`，不再只覆盖 VIEW/MV。
- **M8. 同步失败只用 Debug 级别记录**：`syncer.go:131-135,181-185`，生产 info 级别下永久失败的实例/库完全不可见，无指标无告警。**✅ 已修复（阶段 1）** · `fcb6a98`：两处改为 `slog.Warn` 并用 `log.WithError(err)` 输出完整错误（原先实例级只记 `err.Error()` 字符串）。仍无指标/告警。
- **M9. worker pool 中的 panic 会打挂进程**：`syncer.go:125-139`、`analyzer.go:95,143-155`；`conc/pool` 会把任务 panic 传播出 `Wait()`，只有 `trySyncAll` 有 recover。 —— **✅ 已修复（阶段 6）** · `480b957`：`schemasync` 检查协程与 `lineageanalyzer` pool 的任务包装 recover，记录日志后继续（对齐 `trySyncAll`）。
- **M10. Shutdown 的 WaitGroup 等待无超时**：`backend/server/server.go:168`（见 `01` M1）。 —— **✅ 已修复（阶段 2，`fb8ca14`）**：`runnerWG.Wait()` 由 10s 超时兜底，超时记 Warn 后继续退出。
- **M11. `SyncInstance` 返回未过滤的数据库列表**：`syncer.go:322-342,358`，构建了遵守 `sync_databases` 的 `filteredDatabaseMetadatas`，却返回 `instanceMeta.Databases`；`SyncInstance` RPC（`instance_service.go:528-530`）会报告未同步的库。 —— **✅ 已修复（阶段 6）** · `68f3149`：末尾改为返回遵守 `sync_databases` 的 `filteredDatabaseMetadatas`，`SyncInstance` RPC 不再报告未同步的库。
- **M12. `principal.email` 无 UNIQUE/NOT NULL 保护**：`LATEST.sql:18-31`（见 `03` S-H6）。 —— **✅ 已修复（阶段 2）** · `ff9b22a`：新增唯一邮箱索引（大小写归一后比较），store 的唯一冲突映射为 `common.Conflict`。
- **M13. 旧二进制对着更新的 ledger 静默运行**：`migrator.go:160-194` 只检查 `f.version.LE(*recorded)`；当 `recorded` 大于内嵌最新版本时，不迁移也不报错，只打印内嵌版本号。应显式报错拒绝启动。 —— **✅ 已修复（阶段 3 补遗，`2131420`）**：`recorded > latestVersion` 时拒绝启动。

---

## 低（Low）

- `syncer.go:306-308` 冗余的版本判断（`if instanceMeta.Version != instance.Metadata.GetVersion() { metadata.Version = instanceMeta.Version }`）。
- `syncer.go:363` 陈旧 TODO（下方代码已实现该功能）。
- 死字段/参数：`syncer.go:54,58,362`（未使用的嵌入 `sync.Mutex`、`profile`、未赋值的 `retErr`）、`analyzer.go:41,387`（未使用的 `profile`、`markAnalyzed` 的未用参数）。 —— **✅ 已清理（阶段 7）** · `e2f381f`：`Syncer.profile`+构造参数、嵌入 `sync.Mutex`、`Analyzer.profile`+构造参数、`SyncDatabaseSchema` 的命名返回 `retErr`、开头陈旧 TODO 全部删除；`markAnalyzed` 的参数实际都在用（文档过期），保留。
- `migrator.go:82-92` 空的 `goMigrations`/`GoMigrationFunc` 脚手架，无测试。 —— **✅ 已删除（阶段 7）** · `4ea38d2`。
- `getVersionFromPath` 接受畸形版本（`migrator.go:283-288`，`00001`、负数、任意 `MAJOR.MINOR` 目录）。
- 重复迁移版本未提前检测（`migrator.go:258-266`）。
- `adoptLegacySchema` 不校验基线形状（`migrator.go:202-223`）。
- store 缓存在调用方事务提交前就被更新：`store/meta_resource.go:970-973`（由 `syncer.go:591` 调用）；当前被 `enableCache=false` 掩盖。 —— **✅ 已修复（阶段 2，`f22f61e`）**：事务内不再写缓存，`syncer` 提交后调用 `InvalidateMetaRegistryCache` 失效受影响的 `(guid, object_type)`。
- COLUMN 元资源只为 TABLE 创建：`syncer.go:718-725` vs `store/meta_resource.go:1051-1052`，导致 `ListSublevelMetaRegistryResource` 在视图下返回不了列（需确认 UI 是否直接读 `viewMetadata.columns`）。
- `SyncInstance` 中的 O(n²) 名称查找：`syncer.go:326,345` 使用 `slices.IndexFunc`。
- 未知实例的 `databaseSyncMap` 无界增长：`syncer.go:109-115`，找不到实例时 `return true` 保留条目，每 10s 重试并重复记日志。
- GUID 分隔符碰撞：`syncer.go:727-729` 用 `";"` 连接，`analyzer.go:161` 用 `SplitN(..., 4)`；库/schema/表名含 `;` 会产生歧义 GUID。

---

## 死代码与遗留债务

- `syncer.go:363` 陈旧 TODO；未使用的 `Syncer.profile`、嵌入 `sync.Mutex`、`Analyzer.profile`；未使用的 `retErr`；`markAnalyzed` 的未用参数。 —— **✅ 已清理（阶段 7）** · `e2f381f`（见上节；`markAnalyzed` 参数保留）。
- 空的 `goMigrations`/`GoMigrationFunc` 注册表（`migrator.go:82-92`）。 —— **✅ 已删除（阶段 7）** · `4ea38d2`。
- 重复的血缘删除 SQL：runner 的 4 参 `deleteColumnLineageByMetaTx`（`syncer.go:776`）与 store 的 3 参版本（`store/manual_sql.go:765`）；store 版本有 GUID-subtree 感知 helper，而 runner 的硬编码 SQL 绕过了它。
- 泛滥的无意义 `V2` 后缀（`ListInstancesV2`、`UpdateInstanceV2`、`GetInstanceV2`、`StoreMetaResourceV2`），且不存在对应的 V1。
- `syncer.go:92-97` 重复嵌套 `if err != nil`。
- 硬编码的间隔/上限（`instanceSyncInterval`、`databaseSyncCheckerInterval`、`syncTimeout`、`MaximumOutstanding`、`MaxGoroutines`、`lineageAnalysisInterval`、`analyzeCheckerInterval`），而注入的 `profile` 却未被读取。
- ~~缺失 `migration/0.1/` 目录本身是最大的遗留债务~~（见 R-H5）。 —— **阶段 2 已修复**：目录与两个增量已建立（`8c34542` `ff9b22a`）。
- 引用不存在表的遗留 store 路径：~~`db_schema`（`store/database.go:368`，可达，**阶段 1 已删除** `bb93ee0`）~~、`DeleteProject`（`store/project.go:364`）、`CountIssues`（`store/stats.go:126`）。

---

## 待确认

1. `driver.SyncDBSchema` 是否可能在不报错的情况下返回空/部分 schema（权限受限）？这决定 R-H3 的现实概率。**阶段 1 已查明**：会。MySQL/TiDB 的表/列/索引查询都走 `information_schema`，它按连接用户的权限过滤行（`sync.go:207,228,310,328` 等），凭证被换或被收窄后同步**成功返回但表列表变少或全空，不报错**；PG 走 `pg_catalog` 不过滤表，但 `foreign_tables` 等子查询仍受权限影响。真正空库也会合法返回空结果，因此"空"无法与"权限丢失"区分——这正是 R-H3 只加日志、不加自动拦截的原因（`fcb6a98`）。
2. `table` CEL 过滤前端是否在用？**阶段 1 已确认：前端未使用**（只有 proto 文档提到），且该实现依赖的 `db_schema` 表与 `db.metadata->'schemas'` 都不存在/不存在该字段，因此整个过滤器被删除（`bb93ee0`）。
3. UI 是走 `ListSublevelMetaRegistryResource`（视图列缺失）还是直接读 `ViewMetadata.columns`？
4. 迁移原子性本身可靠：`executeMigration`（`migrator.go:314-350`）把 DDL + ledger 插入放在一个事务里，`LATEST.sql` 也是单事务；失败的增量会保留更早的增量，符合 forward-only 设计。
5. `migrator_integration_test.go` 没有并发/advisory-lock 测试、没有重复版本测试、也没有"`LATEST.sql` 等于增量累积结果"的校验——正是能捕获 R-H5 的 guard。
6. `BatchCreateMetaRegistryResourceAt` 把 `RETURNING id` 按位置写入 `creates[i]`（`store/meta_resource.go:934-953`），PG 不保证顺序；当前影响低（删除不使用这些 ID），需验证。
