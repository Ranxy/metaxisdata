# 阶段 7 实施计划（死代码与低优先清理）

## 进度（随实施更新）

| 步骤 | 状态 | 提交 |
| --- | --- | --- |
| E1 `component/llm` 死钩子与死事件（`05` 死代码节） | ⏳ | |
| E2 runners 死字段与陈旧 TODO（`06` 死代码节） | ⏳ | |
| E3 `common`/`plugin` 死导出符号与 `GetInstaceFromGUID` 拼写（`07` 死代码节） | ⏳ | |
| E4 store `find` 死字段与 ID 缓存（`03` 死代码节） | ⏳ | |
| E5 api/v1 空 `userCountGuard`（`02` 死代码节） | ⏳ | |
| E6 migrator 空 `goMigrations` 脚手架（`06` 死代码节） | ⏳ | |
| E7 lineage testutil 仅测试用死 helper（`09`） | ⏳ | |
| E8 ExplainSQL 未用 proto 字段删除（`04` 低节） | ⏳ | |
| E9 真正的前端内嵌（`go:embed` + Makefile）（`01`/`10` 第六节） | ⏳ | |
| E10 全量验证与文档同步 | ⏳ | |

---

## 零、本次经确认的决策

| 议题 | 决策 |
| --- | --- |
| 整改范围 | **B**：阶段 6 计划列出的死代码项 + 本轮全程序 `deadcode` 分析新发现的跨包死导出符号 |
| ExplainSQL proto | **全部删除**未用字段（`meta_type`/`sections_json`/`expired`/`error`），编号 `reserved`，重新 `buf generate` |
| `goMigrations` 空脚手架 | **删除**（未来需要 Go 数据迁移时再加回） |
| 前端内嵌 | **真正实现**：`//go:build embed_frontend` + `//go:embed frontend_dist` + SPA fallback；新增 Makefile 目标，不改 CI workflow |
| lineage testutil 死 helper | **删除** |
| `Store.DeleteCache` | **保留**（无调用者，但不在本轮范围内） |
| `log.Stack` eager | **保留**（仅 panic 冷路径，收益过低） |
| `08 M13` JSONB 列注释 | **不做**（属 schema 变更，非本轮死代码） |
| 06/03 的 Low 语义项（O(n²) 查找、无界 map、GUID 分隔符碰撞、LIMIT/OFFSET 插值等） | **不做**（属 C 档，未选入） |
| 验证 | 只在本地跑全量 hermetic + `-race` + Docker 集成 + buf + 前端；不改 CI |

## 一、依据与核对方式

依据 `.agents/docs/backend-review/` 各模块报告的「死代码与遗留债务 / 低」节。**这些文档大量过期**：阶段 3/6 已完成的项目仍留在清单里，
且部分"死代码"现已活跃。因此本轮逐条核对当前代码，核对手段：

1. 全仓库 grep（含测试与集成 tag），确认符号/字段的调用者；
2. 整程序可达性分析 `golang.org/x/tools/cmd/deadcode ./backend/bin/server`（本机运行，输出见实施记录）；
3. 对 proto 字段同时核对 Go handler、store 与前端 `frontend/src` 的使用。

核对结论（**已过期、不再处理**）：`resource_name.go` 零引用常量、`common/cel.go` 的 M1/M2、`cel_attributes`、`const.go` 死常量、
`metric` 栈、`utils/collection.go`、`store/common.go` 的死枚举、`role`/`project` 死表、`instance.labels`、
`08` 的 `RiskLevel`/`Position`/`Range`/`InstanceRoleMetadata`、`ExplainSQLRequest.provider_name`（仍在用）、
`buildDeleteManualSQLStatement`（仍在用）、`pluralize`、`InstanceService.stateCfg`、`config.Profile.LastActiveTS` 等均已在此前阶段删除或本就活跃。

## 二、步骤明细

### E1 · `component/llm` 死钩子与死事件
- 删 `event.go` 的 `AgentHooks`、`AgentConfig.Hooks` 字段；删 `agent.go` 中 `BeforeToolCall`/`AfterToolCall` 两个分支。
- 删 `AgentEvent.Done` 字段（`agent.go` 里读的是另一个 `chunk.Done`，不受影响）。
- 删 `AgentEventTurnEnd` 常量与其发送点（无消费者）。
- `MaxTurns`/`MaxConversationBytes` 保留——C7 后已由 `explain_sql_service.go` 显式设置，是活字段。
- 验证：`go build ./...`、`go test ./backend/component/llm/...`。

### E2 · runners 死字段与陈旧 TODO
- `syncer.go`：删 `Syncer.profile` 字段与 `NewSyncer` 的 `profile` 参数；删嵌入的 `sync.Mutex`；`SyncDatabaseSchema` 的命名返回 `retErr` 改回普通返回；删陈旧 TODO（`SyncDatabaseSchema` 开头）。
- `analyzer.go`：删 `Analyzer.profile` 字段与 `NewAnalyzer` 的 `profile` 参数。
- 同步更新 `server.go` 里的两个构造函数调用。
- 注意：`Syncer.stateCfg` 是活字段（连接限流），保留。
- 验证：`go build ./...`、`go test ./backend/runner/...`。

### E3 · `common`/`plugin` 死导出符号与拼写
- `common/permission/permission.go`：删 `Exists`。
- `common/guid.go`：`GetInstaceFromGUID` → `GetInstanceFromGUID`（2 处调用 + `guid_test.go`）；删 `GetSchemaFromGUID`（仅测试引用）及其测试。
- `plugin/db/driver.go`：删 `ErrorWithPosition`（含 `Error`/`Unwrap`）；`plugin/db/pg/pg.go` 删 `LockTimeoutError`（含 `Error`）；`plugin/db/pg/pg.go` 删 `IsNonTransactionStatement`；`plugin/db/pg/system_objects.go` 删 `IsSystemUser`。
- `plugin/schema`：删 `GetSequenceDefinition` 分发器、`RegisterGetSequenceDefinition`、`getSequenceDefinitions` 注册表与 `getSequenceDefinition` 类型；删 `plugin/schema/pg/get_database_definition.go` 的 `GetSequenceDefinition` 及其 init 注册（`writeCreateSequence` 等仍被 `writeTable` 使用，保留）。其余 `Get*Definition` 分发器均有调用者，保留。
- 验证：`go build ./...`、`go test ./backend/common/... ./backend/plugin/...`。

### E4 · store `find` 死字段与 ID 缓存
- `FindMetaRegistryResourceMessage`：删 `ID`/`IDList`/`ExcludeObjectType` 及 `buildMetaRegistryWhereClause` 的对应分支；无任何调用者设置它们。
- 随之删 ID-keyed `metaRegistryCache`：`GetMetaRegistry` 的按 ID 命中分支、各处 `Add(metaRegistry.ID, …)`、`Remove`、`store.go` 的字段与 `lru.New`。GUID-keyed 缓存保留。
- `FindMetaRegistryHistoryMessage`：删 `ValidFrom` 及分支（`Limit`/`Offset`/`OrderDesc`/`TransitionTime` 均活跃，保留）。
- 验证：`go build ./...`、`go test ./backend/store/...`。

### E5 · api/v1 空守卫
- 删 `AuthService.userCountGuard`/`UserService.userCountGuard`（均 `return nil`）及其 4 个调用点与包裹的 `if err != nil`。
- 验证：`go build ./...`、`go test ./backend/api/...`。

### E6 · migrator 空脚手架
- 删 `goMigrations`、`GoMigrationFunc` 与 `migrateSchemaFS` 中的 Go-migration 分支（含注释）。
- 验证：`go test ./backend/migrator/...`。

### E7 · lineage testutil 死 helper
- 删 `plugin/lineage/mysql`/`postgresql` 的 `RunLineageTests`/`RunLineageTest`（无调用者），随后删 testutil 中仅被它们使用的实现。
- 删 testutil 的 `RunLineageTestsFromYAML`、`AssertEdgeCount`、`AssertEdgeExists`、`AssertNoEdgeFromTable`、`AssertAllEdgesToTable`、`CreateCatalogWithSchema`（零引用）。
- 只被上述函数使用的类型/辅助函数一并清理；`RunLineageTestSuitesFromYAMLDir`、`LoadLineageTestSuiteFromYAML`、`ValidateExpectedEdges`、`EdgeMatches`、`FormatRelations` 保持。
- 验证：`go test ./backend/plugin/lineage/...`。

### E8 · ExplainSQL 未用 proto 字段
- `explain_sql_service.proto`：`ExplainSQLRequest.meta_type`(2)、`ExplainSQLMetadata.sections_json`(2)/`expired`(6)、`ExplainSQLResponse.error`(3) → `reserved`。
- Go handler：去掉两处 `SectionsJson` 赋值（缓存内部仍保存 sections，只不再回传字段）。
- 前端：`api/explain.ts` 去掉 `metaType`；`ExplainSQLPage.vue` 去掉 `expired` badge/regenerate 分支、`explainMeta.expired`、`chunk.payload.case === "error"` 分支、`m.expired`；删 `en-US`/`zh-CN` 的 `explainSQL.expired`。
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate`，提交生成的 Go/TS/文档/openapi 产物。
- 验证：`go build ./...`、前端 `vue-tsc`/`vitest`/`build`。

### E9 · 真正的前端内嵌
- `server_frontend_not_embed.go` 加 `//go:build !embed_frontend`。
- 新增 `server_frontend_embed.go`（`//go:build embed_frontend`）：`//go:embed all:frontend_dist`，SPA fallback（未知路径回落到 `index.html`），缺 `index.html` 时只告警。
- 提交 `backend/server/frontend_dist/.gitkeep`，`.gitignore` 忽略该目录下其余文件。
- `Makefile` 新增 `frontend-dist`（`pnpm --dir frontend build` + 拷贝）与 `build-embed`（`-tags "release embed_frontend"`）；`build`/`build-release` 行为不变。
- 更新 `AGENTS.md` 中"服务器不内嵌前端"的说明。
- 验证：默认与 `-tags embed_frontend` 均可编译；跑一次 `pnpm --dir frontend build` + `make build-embed` 并确认产物内嵌。

### E10 · 全量验证与文档同步
- `gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`/`embed_frontend`）、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（跑到 0 issues）、`make build-release`、`make build-embed`。
- `buf format`/`lint`/`generate` 可复现；前端 biome/eslint/vue-tsc/vitest/vite build。
- Docker 集成套件：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...`。
- 文档：`01`–`09` 各模块报告的死代码节标注本轮结果并修正过期描述；`10` 新增「阶段 7」小节；`README.md` 新增「阶段 7」状态节；本文件补全实施记录。

## 三、本轮不做（已明确排除）

- `Store.DeleteCache` 删除、`log.Stack` 惰性化、`08 M13` JSONB 列注释（见决策表）。
- 06/03 的 Low 语义/性能项（`SyncInstance` O(n²)、`databaseSyncMap` 无界增长、GUID 分隔符碰撞、LIMIT/OFFSET 插值、Get 类多行不拒绝、`openlineage_api_key` select `key_hash` 等）。
- `newACLInterceptorWithChecker`（`_test.go` 使用的测试接缝，保留）。
- `store.WithCacheDisabled`（集成 harness 使用，保留）。
- CI workflow 变更、文档全量重写。
