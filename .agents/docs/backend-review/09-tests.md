# 09 · 测试与测试基础设施

**范围**：`backend/test/integration/`（`env/service_env.go`、`env/testenv.go`、`runner/*`）、全部 `*_test.go`（hermetic 与 integration）、`Makefile`、`.github/workflows/`。

**结论**：AGENTS.md 关于"`go test ./...` 是 hermetic 的"这一点成立（本环境实测 exit 0、无需 Docker/DB）。但 CI **从不运行** hermetic 测试；集成测试在缺少 Docker 时不是 skip 而是 `os.Exit(1)`；存在约 500 行死测试基础设施；AGENTS.md 要求的"查询形状 guard 测试"在两处内联 GUID 谓词和数据库范围谓词上缺失；`backend/api/auth` 零测试文件。

**阶段 0 更新**：新增 2 个测试文件（`filter_injection_test.go`、扩展 `audit_test.go`），T-H5 ◐（脱敏谓词已有表驱动测试）；T-C1/T-C2/T-H1/T-H3/T-H4 与 CI 相关条目**未处理**。另：审查时无法运行的 `golangci-lint` 在阶段 0 复测中已可运行（`0 issues`），"lint 清洁度未验证"这一限制已解除。

---

## 严重（Critical）

### T-C1. CI 从不运行 hermetic 单测
- **位置**：`.github/workflows/mysql-integration.yml:75-76`、`Makefile:6,9`
- **证据**：唯一 workflow 的最后一步是 `run: make test-integration`，而 `Makefile:9` 是 `go test -v -count=1 -tags=integration -run 'RealServerIntegration' ./backend/test/integration/runner`。仓库中没有任何地方运行 `go test ./...`。
- **影响**：store guard 测试、migrator 测试、api/v1 helper 测试、runner 测试都不构成 PR 门禁；非 integration 包里的编译错误也可能绿灯合入。
- **修复**：新增 `unit` job，在每个 PR 上跑 `go test -race -count=1 ./...`（paths: `backend/**`、`go.mod`、`go.sum`），integration 单独保留。

### T-C2. 缺 Docker 时集成测试硬失败而非 skip
- **位置**：`backend/test/integration/env/service_env.go:765-767,844-846`、`runner/main_test.go:73-88`
- **证据**：`startPostgresForEnv`/`startMySQLForEnv` 出错直接 `return err`，`TestMain` 随后打印并 `os.Exit(1)`。AGENTS.md 写的是"requires a working Docker daemon and skips when Docker is unavailable"。
- **影响**：`make test-integration`/`make test-integration-smoke` 在没有 Docker 的开发机和 CI runner 上直接失败。唯一的 Docker-skip 逻辑在死代码 `testenv.go:451 skipIfDockerUnavailable` 里。
- **修复**：在 `*ForEnv` 路径集中做 Docker 可用性检查，或返回 `ErrDockerUnavailable` 让 `TestMain` 映射为 exit 0/skip；字符串匹配要覆盖 socket 缺失、权限拒绝、podman。

---

## 高（High）

### T-H1. migrator 集成测试被孤立，从不执行
- **位置**：`backend/migrator/migrator_integration_test.go`（`//go:build integration`）、`Makefile:6,9`
- **证据**：`test-integration-smoke` 跑 `./backend/test/integration/...`，`test-integration`/CI 跑 `./backend/test/integration/runner`，都不含 `./backend/migrator`。
- **影响**：全新安装、升级、legacy adoption 三条迁移路径在 CI 中完全无验证，迁移回归只能到生产才暴露。
- **修复**：把 `./backend/migrator/...` 加入 smoke target 与 CI，或把文件移到 `backend/test/integration/` 下。

### T-H2. 约 500 行死测试基础设施（其中包含唯一的 Docker-skip 逻辑）
- **位置**：`testenv.go:67 SetupMySQLEnv`、`service_env.go:160 SetupMySQLServiceEnv`、`:179 SetupPostgresServiceEnv`、`runner/main_test.go:108 sharedMySQLServiceEnv`、`:134 sharedPostgresServiceEnv`、mysql test `:274 hasDetailedEdge`
- **证据**：grep 只有定义、无调用者；`resetMySQLSchema`/`resetPostgresSchema` 只能通过这些死包装到达。
- **影响**：两套竞争性 harness 增加维护成本；死掉的 `SetupMySQLEnv` 路径恰恰是唯一会在缺 Docker 时 skip 的实现，说明 live 路径在替换 harness 时丢了这个行为。`hasDetailedEdge` 等 integration-tagged helper 也逃过 golangci-lint（配置没有 build-tags）。
- **修复**：删除 `SetupMySQLEnv`/`TestEnv` 接线与未用的 `Setup*ServiceEnv`/`shared*ServiceEnv`/`hasDetailedEdge`；在 `.golangci.yaml` 加 `build-tags: [integration]`。

### T-H3. 内联 GUID-subtree 谓词与数据库范围谓词没有 guard 测试
- **位置**：`backend/store/meta_resource.go:774,846`、`backend/store/database.go:360-398`
- **证据**：`appendGUIDSubtreeCondition`（`meta_resource.go:85`）有 `meta_resource_test.go:13` 覆盖，但同一谓词被复制粘贴到 `listSublevelMetaRegistryResourceImpl`（:774）和 `listSublevelMetaRegistryResourceHistoryImpl`（:846）却没有测试；`listDatabaseImplV2`（`database.go:360-398`）的 `ShowDeleted`/大小写/项目/环境/实例范围谓词也没有 guard。
- **影响**：AGENTS.md 明确要求"当查询形状本身就是不变量时"补 guard 测试，而恰好这类回归（丢掉 `ESCAPE`/`deleted = false` 谓词会静默扩大结果）在两处 GUID 站点和数据库列表查询上没有保护。
- **修复**：把内联谓词改为调用 `appendGUIDSubtreeCondition`；为 `listDatabaseImplV2` 加表驱动 guard 测试，断言生成的 SQL/args。

### T-H4. `backend/api/auth` 零测试
- **证据**：`go test ./...` 显示 `backend/api/auth [no test files]`。未测试项：`GetTokenFromMetadata`（`auth.go:198`）、`GetTokenFromHeaders`（:221）、`audienceContains`（:247）、`GenerateAPIToken`/`GenerateAccessToken`/`generateToken`（:262-299）、`getAuthContext`（:301）、`IsAuthenticationAllowed`（`config.go:10`）以及两条拦截器路径（`WrapUnary:74`、`WrapStreamingHandler:108`）。
- **影响**：token 解析、audience/过期拒绝、免认证判定都是安全关键逻辑，完全无验证；也没有"无 token 应返回 `Unauthenticated`"的集成反向测试。
- **修复**：加表驱动 hermetic 测试（Bearer 大小写、畸形头、cookie 优先级、过期/错误 audience/错误 kid 拒绝、`IsAuthenticationAllowed`），并加一个集成反向测试。

### T-H5. 审计脱敏谓词与拦截器 helper 几乎无测试
> **◐ 部分修复（阶段 0）** · `89ef84a`：新增 `TestIsSensitiveAuditField`（表驱动，覆盖每个精确匹配标记）与 `TestMarshalAuditMessageRedactsSecrets`（`CreateAPIKeyResponse.key`、`DataSource.sslCert`/`sslKey`/`gcpCredential`）。**剩余**：`shouldSkipAudit`/`resolveParent`/`resolveResource`/`resolveActor`/`mapSeverity`/`buildAuditStatus`/`buildRequestMetadata`/`getServiceData` 仍未测试；大小写/空白归一与嵌套数组的覆盖仍偏薄。

- **位置**：`backend/api/v1/audit.go:197-208`
- **证据**：只有 `marshalAuditMessage` 与 `isNilConnectValue` 有测试（`audit_test.go`），覆盖 `password` 与 `idpContext`。`isSensitiveAuditField` 的完整标记列表以及 `shouldSkipAudit:146`、`resolveParent:210`、`resolveResource:222`、`resolveActor:238`、`mapSeverity:277`、`buildAuditStatus:293`、`buildRequestMetadata:304`、`getServiceData:326` 均未测试。
- **影响**：新增敏感字段名或改动标记列表可能把凭据写进 `audit_log` 而没有任何测试失败（这正是 `sslKey`/`content`/`key` 漏洞未被发现的原因）。
- **修复**：对 `isSensitiveAuditField` 做表驱动测试（每个标记、大小写/空白归一、嵌套数组），并覆盖 severity/status 映射与 IP/UA 回退。

---

## 阶段 0 新增的 guard 测试（已落地）

| 文件 | 测试 | 保护的不变量 |
| --- | --- | --- |
| `backend/api/v1/filter_injection_test.go`（新，`3321801`） | `TestFilterParsersDoNotSpliceLiterals` | user/instance/database-name/database-table/database-label 五条 CEL→SQL 路径收到注入载荷时，载荷不出现在生成的 `Where` 中，且值出现在 `Args` 里 |
| 同上 | `TestLikePatternEscapesWildcards` | `%`/`_`/`\` 先转义再包成 `%...%` |
| `backend/api/v1/audit_test.go`（扩展，`89ef84a`） | `TestMarshalAuditMessageRedactsSecrets` | `CreateAPIKeyResponse.key`、`DataSource` 的 `sslCert`/`sslKey`/`gcpCredential` 不得进入审计 payload |
| 同上 | `TestIsSensitiveAuditField` | 脱敏标记列表的精确匹配行为 |

> 注意：T-H3 指出的 **store 侧** guard（`listSublevelMetaRegistryResourceImpl`/`listSublevelMetaRegistryResourceHistoryImpl`/`listDatabaseImplV2`）仍缺失，新测试只覆盖 API 层 filter 翻译器。

---

## 中（Medium）

- **M1. migrator 集成测试清理是静默空操作**：`migrator_integration_test.go:151,156-158`，`defer admin.Close()` 在 `newTestDatabase` 返回时执行，而 `t.Cleanup` 的 `DROP DATABASE` 在测试结束时对已关闭的连接池执行，错误被 `_, _ =` 丢弃。
- **M2. 部分外部服务 env 会静默混用模式**：`service_env.go:729-748,819-829` 只按引擎检查各自变量；只设 MySQL 变量会让 PostgreSQL 仍走 testcontainers。README:93 与 AGENTS.md 说的是"partial env config fails fast"。
- **M3. `reservePort` 存在 TOCTOU**：`service_env.go:704-711`，读取端口后关闭 listener，服务器稍后再绑定；`TestMain` 并发启动 MySQL 与 PostgreSQL env（`main_test.go:56-57`），可能撞端口 → 60s 超时 → 整个 integration job 失败。
- **M4. 容器清理注册太晚**：`testenv.go:106-110`，在 `startPostgres`、`store.New`、`MigrateSchema`、`UpsertSettingV2`、`startMySQL` 之后才注册；中间任何 `require` 失败都会泄漏容器（Ryuk 可缓解）。
- **M5. 跨 harness 与 runner 的 fixture 重复**：`testenv.go:260-429` 与 `mysql test:196-221`/`postgres test:286-318` 重复声明同一套 `users`/`orders`/`user_order_view` DDL；启动时创建的 `it_app`/`it_drop_me` 未被 per-test DB 使用。
- **M6. CI 缺 race/覆盖率/lint/前端 job**：单 job、无 `-race`、无 `-cover`、无 `golangci-lint`、无 `pnpm --dir frontend test`；`frontend` 里 `*.test.ts(x)` 数量为 0（尽管 AGENTS.md 记录了 Vitest + jsdom）。
- **M7. `TestMain` 在启动 panic 时可能永久挂起**：`runner/main_test.go:38-71`，两个 goroutine 向容量 2 的 channel 发送，panic 则无人发送，`for range 2 { <-results }` 永不返回，只能等 workflow 20 分钟超时。
- **M8. store 纯函数不变量无测试**：`manual_sql.go:69,73,107,171`（`buildManualSQLGUID`/`normalizeManualSQLTags`/`normalizeManualSQLAttributes`/`buildManualSQLStoredMetadata`）、`policy.go:24,41`（`generateEtag`/`PatchWorkspaceIamPolicy`）；只有 delete 语句构造器有 guard。
- **M9. 外部模式硬编码凭据并派生性地 DROP 数据库**：`service_env.go:204,289,435,796-817,887`、`testenv.go:55,215,261`；DSN 硬编码 `postgres:postgres`/`root:root`，`recreatePostgresDatabase` 对派生名 `{INTEGRATION_POSTGRES_DB}_{scope}_integration` 无条件 `DROP DATABASE IF EXISTS`。无 admin 用户/密码覆盖。
- **M10. 文档中的 `make test-integration-mysql` 目标不存在**：`README:23,86` 与 `Makefile:1` 的 `.PHONY` 都提到，但 Makefile 里没有该 target。

---

## 低（Low）

- `waitForHTTPReady`（`service_env.go:681-702`）把任何 HTTP 响应（含 5xx）都当作 ready。
- `serverDir` 是死字段（`service_env.go:229,308`，恒为空）。
- `schemasync_lineage_postgres_service_test.go:218` 的 `require.NoError(t, env.ExecPostgres(..., "SELECT 1;"))` 是无意义断言。
- `waitForPostgresReady`（`testenv.go:198`）每次重试都新建完整 `store.Store`；skip 检测靠 `strings.Contains(msg, "docker") && ... "daemon"`（`testenv.go:451`），很脆弱。
- 风格不一致：`migrator_test.go`/`openlineage_api_key_test.go`/`analyzer_test.go` 用 `t.Fatalf` 而非 testify；`openlineage_dataset_test.go` 缺 `t.Parallel()`；`analyzer_test.go:64` 有冗余 `tt := tt`；`meta_resource_test.go:32` 断言 `toOpen` 切片顺序（实现细节）。

---

## 覆盖缺口（明确清单）

**完全没有测试文件的模块**（`go test ./...` 输出确认）：
- `backend/api/auth` —— JWT 生成/校验、header/cookie 提取、认证拦截器、`IsAuthenticationAllowed`。
- `backend/server` —— Echo/Connect 路由装配、拦截器、优雅关停、pprof、前端 handler。
- `backend/api/v1` 的 `debug_interceptor.go`、`auth_service.go`、`user_service.go`、`instance_service.go`、`database_service.go`、`lineage_service.go`、`llm_service.go`、`explain_sql_service.go`、`openlineage_service.go`、`openlineage_handler.go`、`setting_service.go`（阶段 0 新增，无测试）、`acl_interceptor.go`（阶段 0 新增，无测试）、`common.go`（阶段 0 后为 6 个纯 helper 测试文件：新增 `filter_injection_test.go`）。
- `backend/component/llm`（8 个文件）—— agent 循环、tools、registry、fetcher、message/event。
- `backend/component/state`、`backend/component/dbfactory`、`backend/config`、`backend/metric`、`backend/bin/server/cmd`、`backend/test/integration/env`（harness 自身无自测）。
- `backend/common`（CEL 构建、GUID/resource name、错误码）、`common/log`、`common/stacktrace`、`backend/utils`。
- `frontend` —— 0 个 Vitest 文件，尽管 AGENTS.md 记录了 Vitest。

**无直接测试的 store 文件**（只有 `audit_log.go`、`manual_sql.go` 的 delete builder、`meta_resource.go` 的 helper、`openlineage_api_key.go` 的 mask 有测试）：`policy.go`、`role.go`、`group.go`、`principal.go`、`project.go`、`database.go`、`instance.go`、`column_lineage.go`、`openlineage_run.go`、`openlineage_task.go`、`llm.go`、`setting.go`、`idp.go`、`namespace_mapping.go`、`stats.go`、`explain_sql.go`、`external_dataset.go`、`db_connection.go`、`environment.go`、`common.go`、`store.go`。

**Runner**：`schemasync` 只测纯 helper（`convertMetadataToGUID`、`normalizeMetadataForHash`、`batchMetaCreate.diff`、间隔/上次同步默认值、异步入队），同步循环/缓存更新/DB 交互未测；`lineageanalyzer` 只测 `buildSQL`。

**Migrator**：`migrator_test.go` 只覆盖版本/路径纯逻辑；覆盖 fresh install/upgrade/legacy adoption 的集成文件从不执行。

**AGENTS.md 要求但缺失的 guard**：`listSublevelMetaRegistryResourceImpl`（`meta_resource.go:774`）、`listSublevelMetaRegistryResourceHistoryImpl`（:846）、`listDatabaseImplV2`（`database.go:360-398`）。

---

## 待确认

- 缺 Docker 时的 skip 行为是有意移除，还是替换 harness 时丢失？需与作者确认。
- `make test-integration-smoke` 是否应覆盖 `./backend/migrator/...`？孤立的集成文件可能是有意为之，但没有任何文档说明。
- "partial env config fails fast" 是按引擎还是跨两个引擎？AGENTS.md 与 README:93 读起来是跨引擎，代码只实现了按引擎。
- 前端测试是有意缺失吗？AGENTS.md 记录了 Vitest/jsdom 和 `frontend/vitest.config.ts`，但零测试文件。
- **golangci-lint 在本沙箱无法运行**（**阶段 0 已解除**）：审查当时报 `context loading failed: no go files to analyze`，加 `--no-config`/显式路径后报 `loading compiled Go files from cache: ... cache entry not found` 与 `/home/ran/.cache/golangci-lint` 只读；在最小临时模块上同样复现，因此当时判定为环境限制而非仓库缺陷。阶段 0 修复后复测 `golangci-lint run --allow-parallel-runners` 输出 `0 issues.`，**lint 清洁度现已验证**。`go build ./...`、`go vet ./...`、`go test ./...` 均已通过（exit 0）。
