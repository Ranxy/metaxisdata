# Metaxisdata 后端代码 Review（第一版）

**审查范围**：`/home/ran/gocode/metaxisdata/backend/` 下除 `backend/plugin/`（按第一版要求排除）与 `backend/generated-go/`（buf 生成，禁止手改）以外的全部 Go 代码，外加 `proto/v1`、`proto/store` 契约、`backend/migrator/migration/LATEST.sql`、`Makefile` 与 CI 配置。约 2.6 万行。

**审查方式**：按模块通读源码（含 `store`、`api/v1`、`api/auth`、`server`、`runner`、`migrator`、`component`、`common/utils`、`proto`、测试基础设施），并对每个发现给出 `文件:行号` 与代码证据。审查期间未修改任何代码。

**验证基线**（审查当时实测）：
- `go build ./...` ✅ exit 0
- `go vet ./...` ✅ exit 0
- `go test ./...`（hermetic）✅ exit 0，无需 PostgreSQL/MySQL/Docker
- `golangci-lint run` ⚠️ 审查当时本环境无法运行（`context loading failed: no go files to analyze`，`--no-config` 下报 cache 只读/条目缺失）——属环境限制，**当时 lint 清洁度未验证**。

**阶段 0 修复后复测**（详见下文"阶段 0 修复状态"）：
- `gofmt -l backend/` 空输出；`go build ./...`、`go vet ./...`、`go test ./...` 全部 exit 0
- `golangci-lint run --allow-parallel-runners` ✅ **0 issues**（环境限制已消失，lint 清洁度现已验证）
- `go vet -tags integration ./...` ✅ exit 0（仅编译，未实际运行集成用例）
- dev 构建 `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go` ✅ exit 0
- prod profile：`go build -tags release ./backend/bin/server/`、`go vet -tags release ./...`、`go build -ldflags "-w -s" -p=16 -tags release ./backend/bin/server/main.go` 全部 ✅ exit 0（`84b16db` 修正了 `profile_release.go` 的 import）
- 前端 `biome check`、`eslint`、`vue-tsc --build` 均通过（仓库内 `vitest` 无测试文件）

**阶段 1 修复后复测**（详见下文"阶段 1 修复状态"）：
- `gofmt -l backend/` 空输出；`go build ./...`、`go vet ./...`、`go test ./...` 全部 exit 0；`golangci-lint run --allow-parallel-runners` ✅ 0 issues
- `make build-release`（即 `go build -ldflags "-w -s" -p=16 -tags release ...`）✅ exit 0，`go vet -tags release ./...` ✅ exit 0
- 新增 8 个 guard 测试（`09-tests.md` 有清单），全部随 `go test ./...` 通过
- 手工验证：`--enable-json-logging` 输出 JSON 行、`source` 已裁剪为 `dir/file.go`、`--external-url` 出现在 `--help`
- 前端未改动，未重跑前端检查；集成测试需 Docker，仍未运行

**阶段 2 修复后复测**（详见下文"阶段 2 修复状态"）：
- `gofmt -l backend/` 空输出；`go build ./...`、`go vet ./...`、`go test ./...` 全部 exit 0；`golangci-lint run --allow-parallel-runners` ✅ 0 issues
- `make build-release` ✅ exit 0，`go vet -tags release ./...` ✅ exit 0
- 新增 15 个 guard 测试（`store` 的缓存 key/唯一冲突判定/连接池上限、`api/v1` 的冲突映射、`plugin/openlineage` 的请求内缓存、`component/llm` 的 SSE 解析/截断/取消/超时），全部随 `go test ./...` 通过
- `backend/migrator/migration/0.1/` 新增两个增量（`0001##scope_explain_sql_cache.sql`、`0002##add_missing_indexes.sql`），`LATEST.sql` 同步更新
- 迁移在本地 PostgreSQL 16 上实测：① 当前 `LATEST.sql` 全新安装 ✅（含 `scope` 列与两个索引）；② 用阶段 2 之前的 `LATEST.sql` 造出 0.1.0 部署后启动服务端，真实 migrator 依次应用 `0.1.1`、`0.1.2` 并记录版本 ✅；③ 两个增量重复执行幂等 ✅；④ 唯一邮箱索引拒绝大小写变体重复、软删后可复用 ✅
- 前端未改动，未重跑前端检查；集成测试需 Docker，仍未运行

**阶段 3 修复后复测**（详见下文"阶段 3 修复状态"）：
- `gofmt -l backend/` 空输出；`go build ./...`、`go vet ./...`、`go vet -tags release ./...`、`go vet -tags integration ./...`、`go test ./...` 全部 exit 0
- `go test -race -count=1 ./...` exit 0（这正是新增 CI 的 unit 命令，本地实测通过）
- `golangci-lint run --allow-parallel-runners` ✅ 0 issues（配置新增 `run.build-tags: [integration]`，集成 harness 现被 lint 覆盖）
- `make build-release` ✅ exit 0
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` ✅；未改动 proto 时 `buf generate` 可复现（无 diff）
- 前端（因 proto 契约变更同步修改）：`vue-tsc --build` 0 错误、改动文件 `biome check`/`eslint` 通过、`vite build` 成功
- 集成测试仍需 Docker，仍未运行；新增的 CI workflow 只做了本地 YAML 解析与等价命令验证，未在 GitHub 上实跑

**阶段 3 收尾（proto 表面收敛）后复测**（详见下文"阶段 3 收尾修复状态"）：
- `gofmt -l backend/` 空；`go build ./...`、`go vet ./...`、`go vet -tags release ./...`、`go vet -tags integration ./...`、`go test ./...`、`go test -race -count=1 ./...`、`make build-release` 全部 exit 0；`golangci-lint run --allow-parallel-runners` ✅ 0 issues
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` ✅，改动确认后重跑 `buf generate` 无 diff
- 前端：`vue-tsc --build` 0 错误、`biome check src`（177 文件）、`eslint src --max-warnings=0`、`vite build` 全部通过
- 集成测试仍需 Docker，仍未运行
- 踩坑记录：`buf.gen.yaml` 的 `clean: true` + BSR 远程插件限流会在生成失败前清空输出目录，本轮遇到一次并已回滚恢复；提交前应确认 `git status` 没有大批生成文件被删除

**阶段 3 续（proto 残留 + schema 清理 + M 系列）后复测**（详见下文"阶段 3 续修复状态"）：
- `gofmt -l backend/` 空；`go build ./...`、`go vet ./...`、`go vet -tags release ./...`、`go vet -tags integration ./...`、`go test ./...`、`go test -race -count=1 ./...`、`make build-release` 全部 exit 0；`golangci-lint run --allow-parallel-runners` ✅ 0 issues
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 每次 proto 改动后都通过；未改 proto 时 `buf generate` 无 diff（本轮 BSR 登录态正常，四次生成均成功）
- 前端：`vue-tsc --build` 0 错误、`biome check src`（177 文件）、`eslint src --max-warnings=0`、`vite build` 全部通过
- `LATEST.sql` 与 `0.1.0003`/`0.1.0004` 在本地 PostgreSQL 16 实测：全新安装、模拟 0.1.2→0.1.3 与 0.1.3→0.1.4 升级（先把旧表/列恢复回去）、重复执行为 no-op；`0003` 另验证历史 OIDC 行会以 SQLSTATE 23514 显式失败
- **本轮第一次真正运行集成套件**（本机 Docker 可用）：`go test -count=1 -tags=integration ./backend/test/integration/...` 除一条**既有**失败外全部通过。`TestPostgresLineageDeletedWhenViewDroppedRealServerIntegration` 在 `27d6261`（阶段 2 末尾）、`f7cfb0d`、`904fb09` 与当前工作树上**都失败**，与本轮及阶段 3 均无关；根因待单独一轮定位（见下文与 `10` 第五节）

**阶段 3 补遗（正确性遗留 + 安全加固 + 性能与保留 + 引擎覆盖）后复测**（详见下文“阶段 3 补遗修复状态”）：
- `gofmt -l backend/` 空；`go build ./...`、`go vet ./...`、`go vet -tags release ./...`、`go vet -tags integration ./...`、`go test ./...`、`go test -race -count=1 ./...`、`make build-release` 全部 exit 0；`golangci-lint run --allow-parallel-runners` ✅ 0 issues
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` ✅（`lineage_service.proto` 新增分页字段，生成产物只有 3 个文件变化，重跑无 diff）
- 前端：`vue-tsc --build` 0 错误、`biome check src`（177 文件）、`eslint src --max-warnings=0`、`vite build` 全部通过
- **集成套件全部通过**（含此前稳定失败的那条）：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...` → `runner` 48.56s、`migrator` 12.83s，exit 0；`TestPostgresLineageDeletedWhenViewDroppedRealServerIntegration` 单测复跑 11.47s PASS
- 迁移在本地 PostgreSQL 16 实测：全新安装记录 `0.1.5`；升级路径（删掉 `search_text`/函数/三个索引并把 ledger 回退到 `0.1.4`）重新迁移可恢复；重复执行为 no-op；ledger 被改成 `9.9.9` 时启动被拒绝；`search_text` 与旧 `jsonb_each` 谓词逐关键字对拍一致，`EXPLAIN` 确认走 trgm 索引；保留清理的 run/task/registry 行数变化逐项断言

**阶段 3 收尾二（测试与 CI 收口 + M9/M4 资源化）后复测**（详见下文"阶段 3 收尾二修复状态"）：
- `gofmt -l backend/` 空；`go build ./...`、`go vet ./...`（默认/`release`/`integration`）、`go test ./...`、`go test -race -count=1 -cover ./...`、`make build-release` 全部 exit 0；`golangci-lint run --allow-parallel-runners` ✅ 0 issues
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` ✅（M4 与 M9 各一次，产物只有预期文件变化）
- 前端：`vue-tsc --build` 0 错误、`biome check`（180 文件）、`eslint --max-warnings=0`、`vitest run`（12 个用例，首批前端测试）、`vite build` 全部通过
- **集成套件全部通过**（含 migrator 与两条新用例）：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...` → `runner` 49.48s、`migrator` 11.29s，exit 0；`TestDataSourceResourceLifecycleRealServerIntegration` 18.11s PASS、`TestUnauthenticatedRequestsAreRejectedRealServerIntegration` 19.58s PASS
- 集成 harness 新增"无 Docker 时 skip（exit 0）"、部分 env fail-fast、服务器启动换端口重试与可覆盖凭据；`make test-integration-mysql` 补上，`test-integration`/`test-integration-smoke` 纳入 `./backend/migrator/...`

**阶段 5（settings 收回数据库 + 回滚 METADATA_SECRET_KEY）后复测**（详见下文"阶段 5 修复状态"）：
- `gofmt -l backend/` 空；`go build ./...`、`go vet ./...`、`go test ./...`、部署构建 `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go`、`golangci-lint run --allow-parallel-runners` ✅ 0 issues
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` ✅（`WorkspaceProfileSetting` 新增 `openlineage_retention_days`，store/v1/前端/API 文档产物同步）
- 前端：`biome check`（188 文件）、`eslint`、`vue-tsc --build`、`vitest run`（17 用例）全部通过
- 集成测试未重跑（需 Docker）；本机 `go test ./...` 另有一条既有失败 `TestMarshalRolePermissionsIsDeterministic`（clean HEAD 上同样失败：本机 protobuf 的 protojson 在数组元素间输出 `", "`），与本次改动无关

**阶段 6（安全残留 + 正确性 + 性能/资源）后复测**（详见下文"阶段 6 修复状态"与 [`11-phase6-plan.md`](11-phase6-plan.md)）：
- `gofmt -l backend/` 空；`go build ./...`、`go vet ./...`（默认/`release`/`integration`）、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners` ✅ 0 issues、`make build-release` 全部通过
- `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` ✅（`auth_service`/`user_service` 注释变更）；重跑 `buf generate` 无 diff，产物可复现
- 前端：`biome check src`（187 文件）、`eslint src`、`vue-tsc --noEmit`、`vitest run`（17 用例）、`vite build` 全部通过
- **集成套件全部通过**：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...` → `runner` 48.7s、`migrator` 13.2s，exit 0；新增真实 server 用例 `TestOpenLineageIngestionAggregatesRunsRealServerIntegration` 覆盖批次事务、去重计数、latest 语义与 dataset 不重写
- D1 修掉了此前唯一失败的 hermetic 测试 `TestMarshalRolePermissionsIsDeterministic`；D2 顺带发现并修掉 B13 引入的集成失败：既有用例给 `manual_sql_id` 用了含下划线的值（`IsValidResourceID` 按 AIP-122 只允许小写字母/数字/连字符），已把测试 ID 改为连字符形式

**修复状态标记**（用于下文全部模块报告）：

| 标记 | 含义 |
| --- | --- |
| ✅ **已修复（阶段 0）** | 阶段 0 已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 0）** | 阶段 0 消除了主要风险，但有明确剩余项（报告中已列出） |
| ✅ **已修复（阶段 1）** | 阶段 1 已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 1）** | 阶段 1 处理了经确认的部分，剩余项已在报告中写明 |
| ✅ **已修复（阶段 2）** | 阶段 2 已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 2）** | 阶段 2 处理了经确认的部分，剩余项已在报告中写明 |
| ✅ **已修复（阶段 3）** | 阶段 3 已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 3）** | 阶段 3 处理了经确认的部分，剩余项已在报告中写明 |
| ✅ **已修复（阶段 3 补遗）** | 阶段 3 补遗已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 3 补遗）** | 阶段 3 补遗处理了经确认的部分，剩余项已在报告中写明 |
| ✅ **已修复（阶段 4）** | 阶段 4（IAM 权限管理）已修完并验证，附对应 commit |
| ✅ **已修复（阶段 5）** | 阶段 5（settings 收回数据库 + 回滚 METADATA_SECRET_KEY）已修完并验证，附对应 commit |
| ✅ **已修复（阶段 6）** | 阶段 6（安全残留 + 正确性 + 性能/资源）已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 6）** | 阶段 6 处理了经确认的部分，剩余项已在 `11-phase6-plan.md` 文末「本轮不做」写明 |
| ⏳ **未处理** | 尚未涉及，仍需按路线图处理 |

---

## 目录

| 文件 | 模块 |
| --- | --- |
| [`01-entrypoint-server.md`](01-entrypoint-server.md) | 进程入口与服务装配：`bin/server`、`config`、`server` |
| [`02-auth-authorization.md`](02-auth-authorization.md) | 认证、授权、身份、审计：`api/auth`、`user_service`、`auth_service`、`audit*` |
| [`03-store.md`](03-store.md) | 持久层：`backend/store` 全部文件 |
| [`04-api-v1.md`](04-api-v1.md) | ConnectRPC 服务：instance/database/history/lineage/openlineage/llm/explain |
| [`05-components.md`](05-components.md) | 组件层：`component/llm`、`dbfactory`、`state` |
| [`06-runners-migrator.md`](06-runners-migrator.md) | 后台任务与迁移：`runner/*`、`migrator`、`LATEST.sql` |
| [`07-common-utils.md`](07-common-utils.md) | 共享基础包：`common`、`utils`、`config`、`metric` |
| [`08-proto-contract.md`](08-proto-contract.md) | API 与持久化契约：`proto/v1`、`proto/store` |
| [`09-tests.md`](09-tests.md) | 测试与测试基础设施、CI |
| [`10-legacy-debt-and-roadmap.md`](10-legacy-debt-and-roadmap.md) | 遗留债务清单与分阶段整改路线图 |
| [`11-phase6-plan.md`](11-phase6-plan.md) | 阶段 6（安全残留 + 正确性 + 性能/资源）实施计划、决策与进度 |

**严重级别定义**：
- **严重（Critical）**：可被外部利用的安全漏洞，或必然导致数据泄露/功能完全不可用。
- **高（High）**：明确的正确性/安全/稳定性缺陷，会在生产环境中造成实质影响。
- **中（Medium）**：缺陷或性能问题，需要修复但不紧急。
- **低（Low）**：代码质量、可维护性、局部健壮性问题。

---

## 执行摘要

第一版审查以独立条目形式记录 **101 条发现**（严重 15 条、高 51 条、中 20 条，其余为低/债务），另有大量中低问题以清单形式列在各模块末尾。跨模块存在重复计数（例如"授权缺失"在 02/04 各出现一次、"JWT 密钥"在 01/02 各出现一次），去重后独立问题约 80 条。最需要立即处理的是 5 类问题：

### 1. 授权层实际上不存在（贯穿全部 API）
> **✅ 已修复（阶段 0 起步，阶段 4 收口）** · `ec49607` `0f2165e` + IAM 子系统：ACL 拦截器已重建并接线，**全部 v1 方法（含读路径）都声明 `permission` 注解**，由 `iam.Manager` 按 `workspaceMember` 读基线 / `workspaceAdmin` 全目录 / 自定义角色权限集解析，不再是"注解非空 ⇒ 管理员"。管理面新增 `IamService`（工作区策略 Get/Set，etag 乐观并发）、`RoleService`（自定义角色 CRUD）、`GroupService`（组 CRUD），前端新增 Roles / Groups / Members & Permissions 页面并按 `User.permissions` 隐藏入口。守卫测试 `TestEveryMethodIsPermissionGated` 要求除显式 allowlist（Login/Logout/GetCurrentUser/CreateUser/UpdateUser）外每个方法都必须带目录内注解。详见 `plan/iam_permission_plan.md` 与 `02` C4/H4。**剩余**：仍是单工作区、仅 WORKSPACE 策略，没有 per-resource（实例/数据库）策略；H1 token 吊销等条目不受影响。

`backend/server/grpc_routes.go:85` 的 ACL 拦截器当时被注释，且其引用的 `NewACLInterceptor`/`iamManager` 在仓库中已不存在。`AuthContext.Permission` 被解析出来后**没有任何消费者**，没有任何 proto 方法设置 `permission`。后果：
- 任意已认证用户可修改**任意用户（含管理员）的密码**并登录（`user_service.go:500-507`，无权限校验、无原密码校验）；
- 可删除/恢复任意账号、修改任意邮箱；
- 可 CRUD 任意实例、轮换数据源凭据、触发任意实例同步；
- 可读取任意实例血缘、全部 OpenLineage run 与 `raw_payload`；
- 可创建/吊销全局 ingestion key。
整个身份层唯一真正生效的检查是 `ListAuditLogs`。

### 2. JWT 签名密钥是公开常量
> **✅ 已修复（阶段 0）** · `adfec91` `84b16db`：硬编码常量已删除，改为 `JWT_SECRET` 环境变量（缺失时回退 DB `AUTH_SECRET`）且 `< 32` 字符启动失败；解析侧加 `WithValidMethods(HS256)`/`WithIssuer`/`WithExpirationRequired`，历史 token 全部失效。prod profile（`-tags release` ⇒ `Mode=prod`）现已可编译。**注意**：`Makefile`/CI/Docker 均未使用该 tag，默认构建仍按 dev 运行，详见 `01` C1。

`backend/bin/server/cmd/profile_dev.go:11` 当时把 `Secret` 硬编码为 `"00000000-0000-0000-0000-000000000000"`，且 `activeProfile` 是唯一实现（无 build tag、无 prod 版本），`Mode` 恒为 `dev`。攻击者可自行签发 `sub=1`（首个用户即 workspaceAdmin）、`aud=mt.user.access.dev` 的 token，且**跨实例通用**。随机生成的 `AUTH_SECRET` 只用于字段混淆，从不参与 JWT。

### 3. SQL 注入（6 处）
> **✅ 已修复（阶段 0）** · `3321801`：4 处 handler filter 全部改为 `LIKE $n` 参数绑定并转义 `%`/`_`，label key 参数化；`store/principal.go`、`store/group.go` 的 project ID 先经 `common.IsValidResourceID` 校验；新增 `backend/api/v1/filter_injection_test.go` 守卫测试（含 `TestLikePatternEscapesWildcards`）。

CEL 过滤器翻译把用户可控字符串直接拼进 SQL：
- `user_service.go:256`（`ListUsers` 的 `.matches()`）
- `instance_service.go:152,154,156`（`ListInstances`）
- `database_service.go:874,880`（`ListDatabases`）
- `store/principal.go:233`、`store/group.go:111`（project ID 拼进 CTE）
任意已认证用户可达；由于注册接口未认证开放（`CreateUser` 带 `allow_without_credential`，且 `DisallowSignup` 校验被注释），未认证攻击者可先注册再注入。

### 4. 敏感信息落库与回传
> **◐ 部分修复（阶段 0）** · `89ef84a` `5446a10`：脱敏表补齐裸字段名 `key`/`content`/`sslKey`/`sslCert`/`keytab`/`passwd`/`pwd`/`bearer`/`jwt`/`session`，并为 `CreateAPIKeyResponse`、`DataSource` 凭据加回归测试；panic 不再回传堆栈。**剩余**：`Obfuscate` 仍是重复密钥 XOR，密钥与密文同库；审计写入被静默吞掉。

- **明文 OpenLineage ingestion key**：`CreateAPIKey` 标了 `audit = true`，审计拦截器把**响应**写入 `audit_log.response`，而脱敏列表漏了裸字段名 `key` → key 可经 `ListAuditLogs` 读回（`02` H3、`04` B-C1）。
- **TLS 私钥 / GCP 服务账号 JSON / Kerberos keytab**：`sslKey`/`content`/`keytab` 同样不在脱敏列表 → 明文进 `audit_log`（`08` P-C1、`04` A-H5）。
- **panic 时把完整 Go 堆栈返回给客户端**（`grpc_routes.go:73-78`）。
- **凭据"加密"是重复密钥 XOR，密钥与密文同库**（`common/utils.go:65-84`）→ 有 DB 读权限即可还原全部实例密码/SSH 私钥/LLM API key。

### 5. 与 schema 不一致的功能必然失败
> **◐ 部分修复（阶段 1/2）** · `bb93ee0`：`table` 过滤器与它 join 的幽灵表 `db_schema` 一并删除（该过滤器从来不可能工作）。`8c34542`/`ff9b22a`：`migration/0.1/` 增量目录已建立（`0001` scope 列、`0002` 两个索引），双维护流程开始实际运行。**剩余**：`issue`/`query_history` 等引用不存在表的遗留代码仍在。

- ~~`LATEST.sql` **没有 `db_schema` 表**，而 `ListDatabases` 的 `table` 过滤器硬编码 join 它 → 该公开功能 100% 报 `relation does not exist`。~~ 已删除该过滤器（`bb93ee0`）。
- ~~`migration/` **没有增量目录**，只有 `LATEST.sql` → 只改 `LATEST.sql` 的 schema 变更永远到不了已有部署，新装与升级静默分叉。~~ 阶段 2 建立了 `migration/0.1/` 并首次走完"`LATEST.sql` + 增量"双写（`8c34542` `ff9b22a`）。
- 若干部件引用已删除的表（`issue`、`query_history` 等）。

---

## 阶段 0「安全止血」修复状态

阶段 0 的 6 项要求均有对应提交（多数一项一个 commit，第 2/6 项共享 `ec49607`），另有 `099fbc4` 固定 buf 插件版本以隔离代码生成 churn。详见 `10-legacy-debt-and-roadmap.md` 第四节。

| # | 阶段 0 要求 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 1 | JWT 签名密钥环境注入 + fail-closed；作废历史 token | ✅ | `adfec91` `84b16db` | 删除硬编码常量；`JWT_SECRET` 环境变量优先，缺失时回退 DB 中每部署随机的 `AUTH_SECRET`；`< 32` 字符启动失败；解析侧加 `WithValidMethods(HS256)`/`WithIssuer`/`WithExpirationRequired`，历史 token 全部失效。prod profile（`-tags release` ⇒ `Mode=prod`）已可编译并通过 `go vet -tags release ./...`。**注意**：无任何构建目标使用该 tag，默认构建仍为 dev（见 `01` H2 的 CORS 影响）。**阶段 5**：`JWT_SECRET` 已删除，签名密钥只从 DB `AUTH_SECRET` 读取（`7870016`）。 |
| 2 | 恢复授权层：用户/实例/数据源/OpenLineage key 写操作加管理员校验 | ◐ | `ec49607` `0f2165e` | 重建 `ACLInterceptor` 并接线；proto 逐方法声明 `permission`，非空即要求 workspaceAdmin，覆盖用户删除/恢复、实例与数据源全部写操作（含 `validate_only`）、`SyncDatabase`、OpenLineage namespace/key、LLM profile、settings 写入；`UpdateUser` 因含自助场景在 handler 内鉴权。**剩余**：读路径未收紧；permission 到 role 的细粒度映射未实现（当前"非空 ⇒ 管理员"）。 |
| 3 | 关闭未认证注册或强制 `DisallowSignup`；首个管理员授予改原子 | ✅ | `5b19778` `c4e22fc` | `CreateUser` 按 `disallow_signup` 判定（管理员可建任意用户；其他调用者只能注册 END_USER；首个 END_USER 始终放行以完成引导）；store `CreateUser` 用 `pg_advisory_xact_lock` 串行化并在同一事务内授予首个管理员（SSO 首用户路径顺带修复）；`CountUsers` 只统计未删除用户；新增 `SettingService` + `/settings/general` 供管理员开关。 |
| 4 | 修 SQL 注入（user/instance/database filter + principal/group project ID） | ✅ | `3321801` | 见执行摘要第 3 条。 |
| 5 | 审计脱敏补 `key`/`content`/`sslKey`/`keytab` + `CreateAPIKeyResponse` 测试 | ✅ | `89ef84a` | 见执行摘要第 4 条；另补 `sslCert`/`passwd`/`pwd`/`bearer`/`jwt`/`session`。 |
| 6 | panic 不回传堆栈；`validate_only` 加权限 + 内网地址限制 | ◐ | `5446a10` `ec49607` | panic 只回通用 `internal server error`，堆栈仅写日志；`validate_only` 所属的实例写方法已要求 workspaceAdmin。**内网地址限制经确认后主动放弃**（自托管场景下用户连接的目标本就是内网数据库），仅保留管理员权限约束。 |

**验证**：`gofmt` 无差异；`go build ./...`、`go vet ./...`、`go test ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`go vet -tags integration ./...`、release 二进制构建、前端 `biome`/`eslint`/`vue-tsc` 全部通过。集成测试（需 Docker）仅做了编译级校验，未实际运行。

---

## 阶段 1「正确性与可运维性」修复状态

阶段 1 的 5 个编号任务各一个 commit（`10-legacy-debt-and-roadmap.md` 第四节）。其中 7、8 各含一个经确认后**不做**的项，因此标 ◐。

| # | 阶段 1 要求 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 7 | 补 `migration/0.1/` 增量 + guard 测试；修 `db_schema` 过滤 | ◐ | `bb93ee0` | `db_schema` 确认为不需要的遗留代码（`db.metadata` 存 `DatabaseMetadata`，无 `schemas` 字段；schema 树在 `meta_registry_resource`），因此**删除**：API 的 `table` 过滤器分支、store 的 join 推断、proto 过滤文档一并移除，`table` 改为返回 `InvalidArgument` + 守卫测试。**增量目录 `migration/0.1/` 经确认不创建**（当前无待发布 schema 变更），"改 `LATEST.sql` 必须同时补增量"仍是人工流程约束。 |
| 8 | 修 schemasync 两个生命周期 bug 与破坏性 diff；`LastSyncTime` 进事务 | ◐ | `fcb6a98` | checker 瞬时错误改为记日志 + `continue`（不再永久退出）；调度收敛到 `shouldSyncNow`，显式拒绝 `interval == 0`（停用实例不再被排队）；`LastSyncTime` 改为提交成功后再写、血缘排队提到写 `db` 行之前；同步失败日志 Debug→Warn。**破坏性删除按产品决策只加日志**：`logSchemaSyncDeletion` 记录条数/类型分布/GUID 样本，实例级软删记 Warn，但不拦截、不设阈值。 |
| 9 | 修 CEL 类型断言 panic；统一 `InvalidArgument` | ✅ | `ff914ac` | `getVariableAndValueFromExpr` 改为返回 error，新增 `filterString`/`filterBool`/`filterStringList`/`matchArgs`，user/instance/database/audit 四个解析器全部走检查路径；`email == 123`、`engine in [1]`、`name.matches(ident)`、裸 `matches("x")` 不再 panic 而是 `InvalidArgument`；`exclude_unassigned` 不再静默忽略非布尔值。12 个用例的守卫测试。 |
| 10 | 接线日志系统 + 注册 `--external-url` + `-tags release` 进构建目标 | ✅ | `7fdcead` | `setupLogging` 用 `HandlerOptions{AddSource, Level: LogLevel, ReplaceAttr: log.Replace}` 构造 Text/JSON handler 并 `slog.SetDefault`；`--external-url` 注册并写入 `profile.ExternalURL`（SSO 回调 base 不再为空）；新增 `make build-release`（`-tags release` ⇒ prod profile），`make build` 与 AGENTS.md 开发构建保持 dev，文档同步更新。**阶段 5**：`--external-url` 已移除，`external_url` 只由管理员在 `/settings/general` 配置（`7870016`）。 |
| 11 | `UpdateInstance(data_sources)` 按 ID 合并；`UpdateDatabase` 判空 | ✅ | `20e284b` | `mergeDataSources`/`mergeDataSource` 用 `proto.Merge` 按 ID 叠加：请求未带的凭据、SSL/SSH 私钥、`verify_tls_certificate` 等 store-only 字段全部保留，成员仍由请求列表决定（缺席即删除）；顺带在 API 层补"恰好一个 ADMIN"校验。`UpdateDatabase` 对 `GetDatabaseV2` 的 `(nil, nil)` 返回 `NotFound`，不再 panic。 |

**已知语义边界**：`UpdateInstance(data_sources)` 用的是"空值即未变更"，因为 proto3 无法区分"未发送"与"发送了空串"——要清空某个非密钥字段需删除后用 `AddDataSource` 重建。

---

## 阶段 2「性能与资源」修复状态

阶段 2 的 5 个编号任务各一个 commit（`10-legacy-debt-and-roadmap.md` 第四节）。其中 14 有一项经确认后**不做**（`metadata` GIN 索引），因此标 ◐。

| # | 阶段 2 要求 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 12 | `GetUserByID/Email` 改定向查询；决定 `enableCache` 的去留 | ✅ | `f22f61e` | 经确认**启用缓存**并删除 `enableCache` 开关：读路径不再"关着读、开着写"。同时修掉被关闭状态掩盖的正确性缺陷——meta registry 的 GUID 缓存改为 `(guid, object_type)` 且 GUID-only 查询绕过缓存、用户查询命中失败走 `WHERE id/email` 定向查询、`GetSettingV2` 缺失时补写缓存、`GetSecret` 的导出可变字段改为互斥锁保护、`UpdateUser` 不再原地改缓存 profile、事务内不再预写缓存（改为提交后 `InvalidateMetaRegistryCache`）。 |
| 13 | OpenLineage 读路径加 LIMIT/聚合/请求内解析缓存；ExplainSQL 缓存 key 加 scope + TTL | ✅ | `8c34542` | 经确认的范围：数据集页/详情只读最近 5000 个 run、解析器按请求 memoize（preview + instance 列表）、聚合与详情合并为一次遍历。ExplainSQL 缓存 key 纳入 scope/provider/model，新增 `scope` 列与 7 天 TTL（增量 `0001`），缓存写入改用脱离请求的 context 并记录失败。 |
| 14 | 补 `meta_registry_resource(object_type)`、`metadata` GIN、`principal(email)` 唯一索引 | ◐ | `ff9b22a` | 增量 `0002` 加了 `object_type` 索引与 `principal (LOWER(email)) WHERE deleted = FALSE` 唯一索引；唯一冲突（23505，同时匹配 `*pgconn.PgError` 与 `*pq.Error`）映射为 `common.Conflict` 并在 handler 转成 `CodeAlreadyExists`。**`metadata` GIN 索引经确认不加**：搜索是 `->>'name' ILIKE '%x%'`，GIN 无法服务该谓词，仓库也没有任何 `@>` 包含查询。 |
| 15 | LLM agent：ctx-aware 发送、真流式、错误传播、禁止缓存截断结果 | ✅ | `8acbfe6` | SSE body 边读边解析（`bufio.Scanner`，工具调用参数 delta 上限 8MiB）；读错误、畸形 chunk、`finish_reason=length`、未知 finish_reason、超 32MiB、EOF 既无 finish_reason 也无 `[DONE]`、空回答全部成为错误，因此不会返回也不会入缓存；工具调用按 index 排序（不再 `0..len-1` 丢调用）；所有发送 `select` ctx；handler 用子 context 取消生产者；MaxTurns 耗尽且仍有 tool call 视为错误。超时改为 30s 响应头 + 60s 空闲读（去掉 5 分钟总超时），共享 `http.Client` 复用连接。 |
| 16 | 连接池 lifetime/idle 配置；runner 关停超时 | ✅ | `fb8ca14` | 连接池钳制到 `[1, 50]`（原 `0` 会被 `database/sql` 解释为无上限）、`MaxIdleConns=10`、`ConnMaxLifetime=30m`、`ConnMaxIdleTime=5m`，`Initialize` 用 `sync.Once`（避免并发双开泄漏），删除死字段与冗余 import。关停不再 `Fatal`（原会 `os.Exit(1)` 跳过 store 关闭），`runnerWG.Wait()` 加 10s 上限，超时记 Warn 后继续退出。 |

## 阶段 3「清理与重构」修复状态

阶段 3 的 4 个编号任务按子任务拆成 16 个 commit（`10-legacy-debt-and-roadmap.md` 第四节）。19 与 20 各有一个经确认后**不做**的范围，因此标 ◐。

| # | 阶段 3 要求 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 17 | 删除死代码与遗留表面 | ✅ | `a39bc41` `e42b9ac` `40ec3a4` `b9a48a3` `fedcc12` `3cc4926` | 按第二节清单删除：`store/role.go`+`store/project.go` 整文件（含其 LRU 缓存）、`stats.go` 的 5 个统计方法、`policy.go` 的 4 个 V2 CRUD、`group.go` 写路径、`common/cel.go` 的 8 个 helper/6 个变量、`cel_attributes.go` 的 15 个常量、`resource_name.go` 的 26 个符号、整个 `metric` 遥测栈、`utils` 两个死文件、测试双套 harness、未注册 flag、`Server.cancel`、`GatewayResponseModifier.Store`、`-tags embed_frontend`/`minidemo` 两个无实现且会编译失败的约束。**IAM 牵连部分逐个确认后保留**（工作区 IAM 路径、group 读路径、`GetUserFormattedRolesMap`）。 |
| 18 | 合并 CEL 翻译器 / 拆分超长文件 / 统一分页与错误映射 | ✅ | `7bfdfb6` `52213af` `89baa3a` `0590607` `4de82e3` `6be2262` | 4 个 filter 翻译器合并为 `api/v1/filter.go` 的单一实现（字段处理器只能经 `filterArgs` 申请占位符；`engine in [...]` 由内联字面量改为参数绑定）；分页统一到 `paginate[T]`，修掉 token limit 被忽略、sublevel 无 offset、metadata history off-by-one、LLM profile 与 SearchMetadata 无分页；新增 `ErrorMappingInterceptor` 让 `common.Code` 真正映射为 Connect 状态码（`connectErrorForWrite` 退役、`common.Error` 补 `Unwrap()` 且不再 nil panic）；4 个超长文件按职责拆成 2–3 个文件（3 个纯移动 commit，均校验函数集合一致）。 |
| 19 | CI：`go test -race ./...` + lint + 前端测试 + migrator 集成测试入列 | ◐ | `d3d96c1` | 新增 `.github/workflows/ci.yml`：`unit`（`go test -race -count=1 ./...`）+ `lint`（golangci-lint v2.13.1）。经确认**只做 Go 单测与 lint**：前端 job、`./backend/migrator/...` 并入集成 target、缺 Docker 的 skip 行为（T-C2）均推迟。workflow 未在 GitHub 实跑。 |
| 20 | 修正 proto 契约问题（P-H1..P-H6、M 系列）并按 breaking-change 流程发布 | ◐ | `73901a1` | P-H1..P-H6 全部处理；M 系列做了 M3/M5/M6/M7/M8/M12/M14/M16/M17/M19/M20/M21（含删除 `GetDatabase`、`ListInstanceDatabase` 两个 stub RPC）。P-H6 选择"显式过滤 + 文档化"而非补 v1 oneof 分支。**经确认推迟**：Engine 28→3、`DataSource` 的多引擎/IAM/SSH/Vault 字段、SCIM/2FA/服务账号、未实现 setting 枚举、policy/role/project store 消息、死 v1 消息。M1/M9/M10/M11/M15/M18/M22/M23 未做（需产品决策）。仓库内不存在"发布流程"，本轮以 commit `!` + 文档说明代替。 |
| — | 遗留安全项：`Obfuscate` 改 AES-GCM | ◐ | `a1faf65` | AES-256-GCM + SHA-256 派生密钥 + 随机 nonce + `v1:` 版本前缀；密钥优先 `METADATA_SECRET_KEY`（`config.Profile.EncryptionKey` + `store.WithEncryptionKey`），未配置时回退数据库 `AUTH_SECRET` 并 Warn，为空/过短即报错；所有调用点处理错误。**破坏性**：XOR 时代密文不可读，已有部署需重新录入凭证。**阶段 5 整体回滚为 `AUTH_SECRET` 种子 XOR**（`7870016`），同库密钥问题重新成立。 |
| — | 遗留测试项：`api/auth` 与 `backend/server` 测试 | ✅ | `0dae0b7` | `api/auth` 覆盖 token 提取/签发/校验、方法注解、cookie、gateway modifier；`backend/server` 覆盖 `/healthz`、CORS 随 profile、pprof 门控、前端占位页、recover 中间件。新测试发现并修复 `getAuthContext` 对未知方法名的 nil deref panic。 |

## 阶段 3 收尾「proto 表面收敛」修复状态

阶段 3 收尾把第 20 项经确认推迟的过宽表面一次做完，按 A/B/C/D 四个表面各一个 commit（`10-legacy-debt-and-roadmap.md` 第四节的 21–24）。全部为破坏性改动，项目未上线。

| # | 事项 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 21 | Engine + DataSource 表面收敛 | ✅ | `ceb6a3d` | `Engine` 28→5（经确认保留 `MYSQL`/`POSTGRES`/`TIDB`/`MARIADB`/`OCEANBASE`，后两者复用 MySQL 驱动），其余编号与名字在 v1+store 两侧 `reserved`；`convertToEngine`/`convertEngine` 各删 22 case。`DataSource` 删 MongoDB/Oracle/Redis sentinel/Databricks/CockroachDB 字段、四类 IAM 凭据、`SASLConfig`/`KerberosConfig`、`DataSourceExternalSecret`、`authentication_private_key`；**SSH/SSL/`use_ssl`/`extra_connection_parameters` 保留**（驱动真在用）。store `Instance` 删 `roles`/`labels`；`obfuscate`/`unObfuscate` 重写为表驱动。前端 engine 映射表同步收窄。 |
| 22 | Setting / IDP / SCIM-2FA-服务账号收敛 | ✅ | `e0eab33` | `SettingName` 只留 6 个被读写的值；`WorkspaceProfileSetting` 删 `require_2fa`/`token_duration`/`maximum_role_expiration`/`enable_metric_collection`；`init.go` 不再写 `EnableMetricCollection`。IDP 只留 OAuth2（OIDC/LDAP 配置与枚举值 reserved），无调用者的 store IDP 写路径删除。`User.recovery_codes`、`UserProfile.source`、`GroupPayload.source` 删除；`LATEST.sql` 的 setting/mfa/idp 注释同步。 |
| 23 | policy/role/project 死 store 消息 | ✅ | `722d3cb` | 删 `store/role.proto`/`store/project.proto`/`store/explain_sql.proto`（空文件）与 `TagPolicy`/`EnvironmentTierPolicy`。**`store.Policy` 的两个 enum 保留**——`store/store.go`、`store/group.go`、`store/policy.go` 在往 `policy` 表的 text 列写 `WORKSPACE`/`PROJECT`/`IAM`（原报告 grep 漏掉 enum 常量）。表结构未动。 |
| 24 | 不可达 metadata 消息 | ✅ | `ddff264` | v1+store 对称删 `PackageMetadata`/`StreamMetadata`/`TaskMetadata`/`LinkedDatabaseMetadata`/`InstanceRoleMetadata`/六种 spatial index 配置及其引用字段与 oneof 分支，`MetaType` 的 `PACKAGE`/`STREAM`/`TASK`(13–15) reserved；syncer 里两个永不会被填满的循环与转换函数删除。两侧 field number 必须保持一致（metadata 转换靠 `proto.Marshal`→`Unmarshal` 复用编号）。 |
| — | 修正 `DeleteInstanceRequest.force` 注释 | ✅ | `451cb78` | 阶段 3 续以"删除该字段"收口：`force` 连同"移到 default project"逻辑一起删除（字段号 2 与名字 `reserved`），原定的"改注释"方案作废。 |
| — | 修复 flaky 的 `Obfuscate` 往返测试 | ✅ | `4afe1ba` | 阶段 3 写的 `NotContains(ciphertext, plaintext)` 对短明文会随机失败（base64 密文可能包含 `"a"`），改为比较整体是否相等；`-race` 全量跑时命中过一次。 |

**已知取舍（阶段 2）**：
- 启用缓存后，事务内写不再预写缓存，而是提交后失效 + 下一次读回填；`GetMetaRegistry` 仅带 GUID（不带 object type）的调用不再命中缓存（该查询本身走 `(guid, object_type)` 索引）。
- 破坏性 schema 同步仍只记日志不拦截（阶段 1 的产品决策，见 `06` R-H3）。
- ExplainSQL 缓存命中现在需要至少一个启用的 LLM profile（provider/model 参与 key）；禁用全部 provider 后旧缓存不再返回。

---

## 阶段 3 续「proto 残留 + schema 清理 + M 系列」修复状态

阶段 3 收尾之后剩下的三块：proto 残留字段与 `V2` 命名（A）、`role`/`project` 死表与死列（B）、需要产品决策的 M 系列契约（C，M9 经确认跳过）。共 10 个 commit（`10-legacy-debt-and-roadmap.md` 第四节的 25–30）。全部为破坏性改动，项目未上线。

| # | 事项 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 25 | proto 残留与 `V2` 命名 | ✅ | `ee3c39b` `8b328ae` | 删两侧无生产者的 `DatabaseSchemaMetadata.service_name`（Oracle）与 `IndexMetadata.granularity`（ClickHouse），编号与名字 `reserved`。store 的 12 个 `*V2` 方法及 impl helper 去掉后缀（`GetSettingV2`→`GetSetting` 等），`listSettingV2Impl`/`listPolicyImplV2`/`listInstanceImplV2` 统一为 `*Impl`。 |
| 26 | 死认证 schema 清理 | ✅ | `904fb09` | 增量 `0.1.0003`：`DROP TABLE role`（连带 owner sequence 与索引）、`DROP COLUMN principal.mfa_config`、`idp.type` CHECK 收窄为 `('OAUTH2')`（drop/re-add 幂等；历史 OIDC/LDAP 行会让迁移以 SQLSTATE 23514 显式失败）。 |
| 27 | 删 `project` 表与 `db.project` 列及全部 API 表面 | ✅ | `451cb78` | 增量 `0.1.0004`。仓库层删 project 字段/过滤/排序/`BatchUpdateDatabases`，实例列表去掉 `db.project` join；v1 删 `Database.project`、`ListDatabases` 的 `projects/{project}` parent、数据库/实例 `project` 过滤、`exclude_unassigned`、用户/分组 project 过滤、`DeleteInstanceRequest.force`；前端删两处 project 列与三个 i18n key；`common.GetProjectID`/`FormatProject`/`ProjectNamePrefix`/`DefaultProjectID` 成为死代码并删除。**`store.Policy` 与其 PROJECT enum 保留**（WORKSPACE/IAM 行是活路径），注释更正为只有 WORKSPACE 有生产者。 |
| 28 | M1/M10/M11/M15 契约形状与类型 | ✅ | `792ca71` `997ede9` `4036e1e` `b2e80ae` | **M1** store 审计消息改名对齐 v1（protojson 存枚举值名，既有行不受影响）+ 注明 v1 用资源名表达 `AuditLog.id`；**M10** `LineageRelation.transformation` 由"内嵌 JSON 的 string"改为 `repeated Transformation`，去掉二次编码与恒 nil error，前端不再 `JSON.parse`；**M11** 删 store 侧零引用的 `OpenLineageRun`/`OpenLineageTask`（`bytes raw_payload` 在这里）与 `ExternalDataset`/`NamespaceMapping`，v1 `raw_payload` 文档化为落库 JSON 文本；**M15** v1 `UserType.USER`→`END_USER`，与 store `PrincipalType`、`principal.type` CHECK 三处一致。 |
| 29 | M22/M23 请求形状 | ✅ | `733b3e0` | **M22**：`repeated UpdateInstanceRequest` 部分是审查误报（AIP-231 的规定），真正缺陷是 `BatchSyncInstances` 的部分成功不可见——响应改为逐项 `BatchSyncInstanceResult{name,databases,error}` 并继续处理后续实例，只有 `requests` 为空才整请求失败。**M23**：删掉与路径重复的顶层 `id`，身份移入 `mapping.id`（路径 `{mapping.id}`）并新增 `update_mask`，store 按 mask 生成 SET。 |
| 30 | `setting.value` 契约 | ✅ | `d8ce592` | 确认该列是**多态**的（结构化 setting 存 protojson、`AUTH_SECRET`/`BRANDING_LOGO`/`WORKSPACE_ID` 存裸字符串），绑不到单一 store 消息，故明确保留 `text` 并把契约写进列注释；关闭待确认 3 与 5。 |
| — | M9 `UpdateDataSource` 资源化 | ◐ | （无） | 经确认跳过：需要给 `DataSource` 加 `name` 并把三个自定义方法改成 AIP-133/135 标准方法（连前端与集成测试），本轮范围不含。 |
| — | M18 create/update 一致性 | ✅ | （无） | 经确认**不改契约**：`validate_only` 只保留在真的验证外部连接的三处，`allow_missing` 只保留在已实现的 `UpdateUserRequest`；规则写进 `08`。 |

**新发现（已在阶段 3 补遗修复）**：`TestPostgresLineageDeletedWhenViewDroppedRealServerIntegration`（`backend/test/integration/runner/schemasync_lineage_postgres_service_test.go:87`）曾稳定失败——`require.Eventually` 等被 DROP 的 VIEW 的 `meta_registry_resource` 与 `column_lineage` 行都消失，20s 内未满足；用 `git worktree` 验证过它在 `27d6261`（阶段 2 末尾）、`f7cfb0d`、`904fb09` 与当时的工作树上都失败，因此不是阶段 3 引入。**根因是集成 harness 的测试替身缓存**（`backend/test/integration/env/service_env.go` 的 `inspectStore` 是第二个 in-process store，`f22f61e` 打开缓存后它也开始缓存 VIEW 行，而 server 进程的缓存失效不会跨进程传播），数据库里那两行其实早已为空。修法是新增 `store.WithCacheDisabled()` 并让两个 `inspectStore` 启用它（`f50fbbc`），生产缓存不受影响。

---

## 阶段 3 补遗「正确性遗留 + 安全加固 + 性能与保留 + 引擎覆盖」修复状态

阶段 3 收尾与阶段 3 续之后，各模块报告里剩下的「仍未处理」项本轮做了一批：正确性遗留（A）、安全加固（C）、性能与保留（B）、引擎覆盖（G），外加一条既有集成失败的根因定位（E）。测试/CI 类遗留（前端 Vitest 与 CI job、T-C2 Docker skip、T-H3 guard、`api/v1` handler 测试等）**本轮未做**。共 11 个 commit + 1 个测试修复（`10-legacy-debt-and-roadmap.md` 第四节的 31–40）。

| # | 事项 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 31 | GUID 前缀 + IAM 条件 fail-closed | ✅ | `6f2b63d` | `common.GUIDPrefix` 按 `"."` 切分而 GUID 用 `";"`，对真实 GUID 恒返回空串，`GetSchemaString` 因此永远查不到 sequence，PG 的 `ALTER SEQUENCE ... OWNED BY`/identity DDL 丢失；改为按 `MetaGUIDSplit` 去掉最后一段并补单测。CEL 求值为 residual（引用未绑定的 `resource.*`）时曾返回 true = 全局授权，现在 fail closed（返回错误、调用方记日志并丢弃 binding），env 改为构建一次。 |
| 32 | 血缘列表分页 | ✅ | `dd6df51` | `GetLineage`/`GetLineageForContext` 新增 `page_size`/`page_token`/`next_page_token`（默认 500、上限 5000）；`GetLineage` 用同一 offset 页化 source/target，`lineage_type` 只选一个时 token 仍有效。前端 `getLineage` 循环取页并合并（`external_datasets` 按 GUID 合并），血缘图可见集合不变；集成 harness 同步取全。 |
| 33 | OpenLineage 批摄取 | ✅ | `d562a50` | 过去只要一条成功就返回 200、解析失败的事件被静默跳过；现在回显 `processed`/`failed`，全不可解析返回 400，存在服务端失败返回 500。 |
| 34 | 空 scope + 对象配对 | ✅ | `11943ae` | 空 `scope_prefix` 曾生成 `guid = '' OR guid LIKE ';%'`（恒空），而未选实例时它恰好为空；空前缀现在表示跨全部实例，空关键字返回明确工具错误，搜索失败不再被丢弃。`fetchObjectsByGUIDs` 的 `guids[:len(metas)]` 位置配对会让一次失败后的 GUID 全部错位，改为成对携带。 |
| 35 | LLM 组件四项 | ✅ | `10631e0` | `ValidateBaseURL`（绝对 http/https + host）+ 8MiB 响应上限 + 连接池复用 + 15s 期限，profile 写入对非法 base_url 返回 `InvalidArgument`；`Registry.ListEnabled` 改全部分页 + 30s 缓存 + 写侧失效（原来每请求打库并解密全部 key，且第 50 个 profile 之后不可见）；`ConvertToLlm` 保留 assistant 文本；`NewDBDebugLogger` 改有界 worker 队列。 |
| 36 | 搜索索引 + 批量扫描 + 清理/保留 | ✅ | `f50fbbc` `2259abd` | 新增 `search_text` 存储生成列（恰为 name/title/comment/userComment 拼接）+ `pg_trgm` GIN 索引（增量 `0.1.0005`），谓词改为 `search_text ILIKE $n`，匹配行与旧 `jsonb_each` 谓词逐关键字对拍一致；空搜索串改为 `InvalidArgument`。`queueAll` 与 schemasync 的 `diff()` 都改 digest 列表 + 每类型一次版本查询（后者不再解析每个 JSONB 行，`2259abd`）。新增 `DeleteExpiredExplainSQLCache`/`DeleteExpiredLLMDebugLog`/`DeleteOpenLineageRunsBefore` 与 `WithCacheDisabled`。 |
| 37 | migrator 三项 | ✅ | `2131420` | `tableExists` 限定 `current_schema()` + `BASE TABLE`（原来别的 schema 的同名表会让全新安装路径被跳过）；advisory lock 的解锁与 `lock_timeout` 复位改用不可取消的 ctx，并给加锁本身加 `lock_timeout`；ledger 比二进制新时拒绝启动。 |
| 38 | 不支持引擎的重试 | ✅ | `9019140` | 引擎无 lineage analyzer 时把跳过连同 meta hash 与原因写进 `column_lineage_version`，不再每小时无限重排队。 |
| 39 | 保留清理 runner | ✅ | `cfa74df` | `runner/maintenance` 启动时与每 6 小时清理过期 ExplainSQL 缓存行与 7 天前的 `llm_debug_log`；`openlineage_run` 默认永久保留，`--openlineage-retention-days`（默认 0）显式开启后连带重算 task 聚合并清理空 task 与镜像 registry 行。**阶段 5**：该 flag 已移除，保留期改由 `WorkspaceProfileSetting.openlineage_retention_days` 配置（`7870016`）。 |
| 40 | 引擎注册覆盖 | ✅ | `729db71` | driver 补 `TIDB`（此前完全缺失，TiDB 实例连 Open 都失败）；schema DDL/迁移补 `MARIADB`/`TIDB`；lineage 补 `MARIADB`/`OCEANBASE`；OpenLineage resolver 的 `isMySQLLike` 纳入 `OCEANBASE`。 |
| — | 既有集成失败根因 | ✅ | `f50fbbc` | 见上：测试替身缓存，不是产品缺陷。 |
| — | `backend/server` 测试竞态 | ✅ | `a16c8d4` | dev/prod 两个 `sync.Once` 在并行测试下同时调用 `registerMetrics`，约 1/5 概率 `-race` 失败；改为一个 Once 顺序构建。 |
| — | M9 / M4 资源化 | ✅ | `513940f` `c2a67e0` | 阶段 3 收尾二完成，见下节。 |

**已知取舍（阶段 3 补遗）**：
- `metadata` 搜索仍是**子串匹配**（ILIKE），只是改由 trgm 索引服务；没有改成全文检索，因为 FTS 是词元匹配，会改变 `search_objects` 与 `SearchMetadata` 的语义。
- `openlineage_run` 默认仍永久保留（可审计数据）；开启保留期会连带删除由这些 run 聚合出的 task 与两者的 registry 行。
- LLM profile 列表只是 30s TTL 缓存 + 写侧失效，没有做按需加载单个 profile。
- 空 `scope_prefix` 现在表示"跨全部实例搜索"：所有已认证用户本来就能浏览全部实例，因此不是新的信息暴露面。

---

## 阶段 3 收尾二「测试与 CI 收口 + M9/M4 资源化」修复状态

阶段 3 补遗做完了正确性/安全/性能/引擎覆盖（31–40），本轮把剩下的**测试与 CI 欠账（41–48）**与**最后两个契约遗留 M9/M4（49–50）**一起收掉。共 10 个 commit（`10-legacy-debt-and-roadmap.md` 第四节的 41–50）。

| # | 事项 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 41 | 集成测试缺 Docker 时 skip + `TestMain` panic 不挂起 | ✅ | `061208c` | 两个套件共用 `dockerutil`（testcontainers provider 探测）；`TestMain` 无运行时打印跳过信息并 exit 0，setup panic 被 recover 并上报，不再让收集端等到 workflow 超时。 |
| 42 | harness 启动加固 | ✅ | `061208c` | 部分 `INTEGRATION_*` 配置直接拒绝（不再静默混用）；服务器启动失败换端口重试、进程提前退出立刻带日志失败；凭据可用 `INTEGRATION_POSTGRES_USER/PASSWORD`、`INTEGRATION_MYSQL_USER/PASSWORD` 覆盖；只 DROP 自己派生的 `*_integration` 库；删掉恒空 `serverDir`。 |
| 43 | migrator 集成测试清理不再是空操作 | ✅ | `48d65be` | `defer admin.Close()` 让 `t.Cleanup` 的 `DROP DATABASE` 打在已关闭的池上且错误被丢弃（每次都漏一个 `migrator_test_*` 库）；现在管理池活到 DROP 之后，DROP 失败即测试失败。 |
| 44 | 集成 target 收口 | ✅ | `8823ac2` | 补上文档里存在但 Makefile 里没有的 `make test-integration-mysql`；`test-integration`/`test-integration-smoke` 纳入 `./backend/migrator/...`，三条迁移路径第一次进入集成门禁与 CI。 |
| 45 | store 查询形状 guard（T-H3） | ✅ | `df97e0a` `f65baaa` | 子对象 GUID 子树谓词与数据库列表范围谓词改为纯构造函数（复用 `appendGUIDSubtreeCondition`），表驱动测试断言谓词形状、LIKE 转义与**占位符编号 = 参数长度**。 |
| 46 | store 纯函数 guard（M8） | ✅ | `711c0aa` | 工作区 IAM 合并抽成纯函数 `patchIamPolicyBindings`（缺失角色按请求顺序、不再依赖 map 顺序），manual SQL 四个 helper 与 `generateEtag` 补测试。 |
| 47 | 缺失的单测 | ✅ | `c162bc0` | 审计 helper（含"已认证用户优先于请求字段"）、`debug_interceptor` 截断、`backend/server` 真实启停路径、`component/llm` 的 fetcher/message/tools、以及真实 server 的未认证反向集成测试。 |
| 48 | CI 前端 job + 覆盖率（M6） | ✅ | `65f4eaa` | 新增 frontend job（pnpm/Node + `biome ci`、`lint:ci`、`vue-tsc`、`vitest run`、生产构建），Go 单测 job 加 `-cover`；首批 12 个 Vitest 用例覆盖 `extractErrorMessage` 与 `getLineage` 的分页遍历。 |
| 49 | M9 `DataSource` 资源化 | ✅ | `513940f` | `metaxisdata/DataSource`（`instances/{instance}/dataSources/{data_source}`）+ `CreateDataSource`/`UpdateDataSource`/`DeleteDataSource`（AIP-133/134/135）；`UpdateInstance` 不再接受 `data_sources` mask，纯函数 `patchDataSource` 只写掩码内字段（掩码外的密码/TLS 不被清空），前端编辑改成 diff 后调子方法；真实 server 生命周期集成测试。 |
| 50 | M4 OpenLineage/API key 资源化 | ✅ | `c2a67e0` | 四个消息改名并声明资源与 `openlineage/...` pattern，`name` 取代 `int64 id`，Get 绑 `{name=openlineage/runs/*}`/`{name=openlineage/tasks/*}`，Delete/Revoke 收资源名；`common` 新增 format/parse helper 并有往返 + 非法名拒绝测试。 |

**已知取舍与剩余（阶段 3 收尾二）**：
- CI workflow 仍**从未在 GitHub 上真正执行**，本地只验证了等价命令与 YAML 结构。
- 前端仍**没有覆盖率**：`test:coverage` 需要新增 `@vitest/coverage-v8` 依赖，本轮以 Go 侧 `-cover` 代替。
- `metadata` 搜索仍是有意的子串匹配（trgm 索引服务），LLM profile 仍是 30s TTL 缓存 + 写侧失效，`openlineage_run` 默认永久保留（保留期改由管理员设置，见阶段 5）。
- `09` 的低优先项（`waitForHTTPReady` 把 5xx 当 ready、`SELECT 1;` 空断言、fixture DDL 重复（M5）、部分测试缺 `t.Parallel()`）保留。

---

## 阶段 5「settings 收回数据库 + 回滚 METADATA_SECRET_KEY」修复状态

一次功能改动（`7870016`）：把运行期配置从进程 profile / 环境变量收回数据库设置，并回滚 `a1faf65` 引入的凭证加密密钥。四项改动互相牵连在 `cmd/profile.go`、`config/profile.go`、`server/` 等文件里，作为单个 commit 提交。

| # | 事项 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 51 | `OpenLineageRetentionDays` 改为管理员设置 | ✅ | `7870016` | 移入 `WorkspaceProfileSetting.openlineage_retention_days`（store 字段 14 / v1 字段 4）；`runner/maintenance` 每轮 `GetWorkspaceGeneralSetting` 读取（无需重启），`/settings/general` 可编辑；删除 `--openlineage-retention-days`。 |
| 52 | `ExternalURL` 改为管理员设置 | ✅ | `7870016` | 删除 `--external-url` 与 `initializeSetting` 的启动覆盖，`WORKSPACE_PROFILE` 只在全新安装时创建、之后完全由管理员管理；前端补齐入口；保存时去掉尾部 `/`；未配置时 SSO 返回 `FailedPrecondition`。 |
| 53 | JWT 签名密钥只从数据库读取 | ✅ | `7870016` | 删除 `JWT_SECRET` 环境变量，`resolveJWTSecret` 始终读 `AUTH_SECRET`；环境变化不再让全部 token 失效。 |
| 54 | 回滚 `METADATA_SECRET_KEY` / AES-GCM | ✅ | `7870016` | `common.Obfuscate/Unobfuscate` 回到 `AUTH_SECRET` 种子 XOR；删除 `Profile.EncryptionKey`/`store.WithEncryptionKey`；`GetSecret` 仍对缺失/空的 `AUTH_SECRET` 报错（避免空 seed 除零）；`store/setting_test.go` 换成「已解析 secret 走缓存」的 hermetic guard。**破坏性**：已写入 AES `v1:` 密文的部署需重新录入凭证。 |

**已知取舍与剩余（阶段 5）**：
- `AUTH_SECRET` 同时用于 JWT 签名与字段混淆（回到 `a1faf65` 之前的形态），这是本轮明确选择的取舍；若将来要分离，需要新设置项与迁移路径。
- 回滚意味着"同库密钥 XOR"这一问题重新成立：`07` U-H2 与 `04` M18 从"已修复"退回"待办"（见各模块报告的第 5 阶段更新）。
- 字段混淆侧的空 seed 由 `GetSecret` 的空值检查兜住，但 `common.Obfuscate` 本身对空 seed 仍会除零（当前除 `store` 外无其它调用者）。

---

## 阶段 6「安全残留 + 正确性 + 性能/资源」修复状态

范围与决策见 [`11-phase6-plan.md`](11-phase6-plan.md)：只做**安全残留、正确性缺陷、性能/资源**三批，共 25 个步骤（A1–A8、B1–B17、C1–C8）加 D1–D3；每步独立 commit。

| 批次 | 步骤 | 状态 | 提交 |
| --- | --- | --- | --- |
| A · 安全 | A1 token 吊销加固（`02 H1`） | ✅ | `976ebc5` |
| A · 安全 | A2 CORS / CSRF 收口（`01`/`02 H2`） | ✅ | `976ebc5` |
| A · 安全 | A3 登录时间与限流（`02 M2`） | ✅ | `8ae989f` |
| A · 安全 | A4 OAuth2 state + 配置校验 + 脱敏日志（`02 M4`） | ✅ | `25a001a` |
| A · 安全 | A5 审计链路加固（`02 M8/M9/M10`、`04 B-C1` 残留） | ✅ | `2208521` |
| A · 安全 | A6 ingestion key digest + 作用域（`04 B-H6/B-H8`、`03 M25`） | ✅ | `415e16e` |
| A · 安全 | A7 其它安全缺口（`01 M6`、`04 A-H1` 残留、`07 U-H2`、杂项） | ✅ | `f112e5c` |
| A · 安全 | A8 token header 白名单与 web token 回传（`02` 低节） | ✅ | `c30d73f` |
| B · 正确性 | B1 engine 过滤按枚举名比较（`03 S-H5`） | ✅ | `824232a` |
| B · 正确性 | B2 `SyncInstance` 返回过滤后的库列表（`06 M11`） | ✅ | `68f3149` |
| B · 正确性 | B3 悬空血缘清理 + manual SQL 旧 GUID（`06 M7`、`03 M15`） | ✅ | `9c69196` |
| B · 正确性 | B4 事务回滚与单语句去事务（`03 M2`） | ✅ | `a71716a` |
| B · 正确性 | B5/B6 `RETURNING` 按键回填、历史谓词配对（`03 M3/M7`） | ✅ | `7f0e3a0` |
| B · 正确性 | B7 `UpdateDatabase` 单事务加锁（`03 M5`） | ✅ | `eca3686` |
| B · 正确性 | B8 LLM 空 mask 部分更新（`04 B-M10`） | ✅ | `14b8f01` |
| B · 正确性 | B9 `parseStructuredResponse` 标题残留（`04 B-M17`） | ✅ | `4d936c3` |
| B · 正确性 | B10/B11 血缘失败退避重试、runner panic 隔离（`06 M2/M9`） | ✅ | `480b957` |
| B · 正确性 | B12 `DiffMetadata` 与历史比较（`04 A-H2/A-H3/A-M11/A-M12`） | ✅ | `f58c387` |
| B · 正确性 | B13 API 输入校验与错误映射（`04 A-M5/A-M9/A-M10`、低节） | ✅ | `48dbecb` |
| B · 正确性 | B14 store 失败不再降级（`04 B-M7/B-M16`、`03 M21`） | ✅ | `0151bcb` |
| B · 正确性 | B15 nil 防护与解析修正（`05 C-H4/L2`、`04 B-M8`） | ✅ | `e7239db` |
| B · 正确性 | B16 `RequireResetPassword` / `allow_missing`（`02 M6/M12/M13`） | ✅ | `bedadf7` |
| B · 正确性 | B17 `disallow_password_signin` 覆盖服务账号（`02 M7`） | ✅ | `83b1229` |
| C · 性能 | C1 ingestion 批次上限与单事务（`04 B-H7`） | ✅ | `e7d15eb` |
| C · 性能 | C2 task 聚合增量计数（`03 M22`） | ✅ | `e7d15eb` |
| C · 性能 | C3 OpenLineage 列表默认 LIMIT（`03 M26`） | ✅ | `311e790` |
| C · 性能 | C4 历史批量关闭与下推分页（`03 M6/M9`） | ✅ | `3e6fbda` |
| C · 性能 | C5 `ListDatabases` 批量取实例（`04 A-M8`） | ✅ | `a7ea214` |
| C · 性能 | C6 external dataset 去写放大（`03 M20`） | ✅ | `99d41ea` |
| C · 性能 | C7 LLM 会话预算与轮数（`04 B-M12`） | ✅ | `03c17b7` |
| C · 性能 | C8 实例密钥每页只取一次（`03` 低节） | ✅ | `390a66c` |
| D · 验证 | D1 修复既有失败测试 | ✅ | `7912fc2` |
| D · 验证 | D2 全量本地验证（含集成套件） | ✅ | 见下节复测 |
| D · 验证 | D3 文档同步（本节与各模块报告标记） | ✅ | 本节 |

**本轮明确的决策（不再视为待确认）**：
- 凭证混淆**保持** `AUTH_SECRET` 种子 XOR（尊重阶段 5 的回滚），只在 `store` 侧补空 seed 防护并在文档写明取舍；"同库密钥 XOR"仍是有意接受的已知风险。
- 部署按**单租户**处理：读路径不新增 per-instance 授权，维持 `permission` 注解现状。
- 破坏性 schema 同步**保持仅日志**，不加硬拦截。
- gRPC 反射**保持匿名**（现状，仅把策略写进文档）。
- **不改 CI workflow**：全量验证只在本地跑（含 Docker 集成套件），结果写进本文档。
- `RequireResetPassword` 选**受限 token** 方案（JWT 增 `rst` claim，拦截器按白名单只放行自助改密与登出），而不是拒绝登录——拒绝会让首登改密没有入口。

**已知取舍与剩余（阶段 6）**：
- token 吊销缓存（`state.TokenExpireCache`）仍是**进程内**的：多副本部署下 A 副本的登出不会让 B 副本已签发的 token 失效（密码变更失效走数据库，跨副本有效）。单租户单副本下影响可接受，已在 `02` 写明。
- `openlineage_run` 仍**默认永久保留**（保留天数由 `WORKSPACE_PROFILE.openlineage_retention_days` 决定，默认不清理），本轮未改默认值。
- 反射匿名、`metadata` 搜索用子串匹配（非全文检索）、`MARIADB`/`OCEANBASE` 的 plugin 覆盖缺口、per-resource IAM 策略等仍是已知项，见 [`11-phase6-plan.md`](11-phase6-plan.md) 第六节「本轮不做」。

---

## 横切主题

| 主题 | 说明 | 主要位置 | 修复状态 |
| --- | --- | --- | --- |
| **授权缺失** | 拦截器被注释、`permission` 从不校验 | `server/grpc_routes.go:85`、`api/auth/auth.go:350` | ✅ 阶段 0 恢复拦截器，阶段 4 完成：全部方法（含读）带注解，`iam.Manager` 按角色/权限集解析，新增 IAM/Role/Group 管理面与前端页面 |
| **SQL 拼接** | 4 个 handler + 2 个 store 把用户输入拼进 `WHERE` | `user/instance/database_service.go`、`store/principal.go`、`store/group.go` | ✅ 阶段 0：全部参数化 + project ID 校验 + guard 测试 |
| **秘密处理** | 硬编码 JWT 密钥、XOR"加密"、审计脱敏遗漏 | `profile_dev.go:11`、`common/utils.go`、`api/v1/audit.go:197` | ✅ 阶段 0/3/5/6：JWT 与审计脱敏已修；阶段 3 换成 AES-256-GCM + `METADATA_SECRET_KEY`（`a1faf65`），**阶段 5 又回滚为 `AUTH_SECRET` 种子 XOR 并删除 `METADATA_SECRET_KEY`**（`7870016`）——同库密钥问题因此重新成立，见 `07` U-H2；**阶段 6**：`common.Obfuscate`/`Unobfuscate` 对空 seed 返回错误而不是除零（`f112e5c`），ingestion key 改用 SHA-256 digest 点查 + namespace 作用域（`415e16e`）。 |
| **凭据被往返请求清空** | `UpdateInstance(data_sources)` 整体替换数据源列表，丢掉读取路径不返回的密钥/store-only 字段 | `instance_service.go:405-413,1307-1353` | ✅ 阶段 1：按 ID 合并（`20e284b`） |
| **未认证入口** | `CreateUser` 免凭证 + 首个用户自动管理员 | `user_service.proto:53`、`user_service.go:286-398` | ✅ 阶段 0：按 `disallow_signup` 判定，首管理员授予原子化 |
| **缓存被禁用但仍在写** | `store.New(..., false)` 使所有 LRU 读失效，写仍发生；`GetUserByID` 因此每请求全表扫描 | `server/server.go:70`、`store/principal.go:89-114` | ✅ 阶段 2：缓存启用、开关删除、定向查询 + key/竞态修复（`f22f61e`） |
| **错误码不生效** | `common.Code` 无映射链路，store 的 NotFound/Conflict 到客户端变 500 | `common/error.go:87`、`server/grpc_routes.go:80` | ✅ 阶段 3：新增 `ErrorMappingInterceptor` 统一映射，`common.Error` 补 `Unwrap()`（`89baa3a`） |
| **日志系统未接线** | `LogLevel`/`Replace` 从未安装，`--debug`/`--enable-json-logging` 无效 | `common/log/log.go`、`cmd/root.go:72,78` | ✅ 阶段 1：`slog.SetDefault` + Text/JSON handler（`7fdcead`） |
| **无界查询 / N+1** | OpenLineage 数据集全表 + payload 解析；血缘无分页；`queueAll` 每小时全表；task 聚合每事件全量重算；`ListDatabases` 每行查实例 | `openlineage_dataset.go:40,119`、`lineage_service.go:57`、`analyzer.go:105`、`store/openlineage_task.go`、`database_service.go` | ✅ 阶段 2/3/补遗/6：数据集读限 5000 + 请求内缓存（`8c34542`）；三个 OpenLineage 列表补分页（`52213af`）；血缘两列表补 `page_size`/`page_token`（`dd6df51`）；`queueAll` 改为 2 次查询/类型且不再解析 metadata（`f50fbbc`）；**阶段 6**：ingestion 批次上限（1000 事件 / 8MiB）+ 整批单事务、task 计数改增量、run/task 列表默认 `LIMIT 5000`、元数据历史批量关闭与分页下推、`ListDatabases` 批量取实例、未变 external dataset 不再重写、LLM 会话轮数与字节预算（`e7d15eb` `311e790` `3e6fbda` `a7ea214` `99d41ea` `03c17b7`） |
| **分页不一致** | 标准 page_token 与 OpenLineage 裸 offset、LLM 无 token、sublevel 无 offset 并存 | `proto/v1/*`、`api/v1/common.go:338` | ✅ 阶段 3：统一 `page_token`/`next_page_token` 与 `paginate[T]`（`73901a1` `52213af`） |
| **大量 Bytebase 遗留** | IAM/role/project/issue/多引擎/SCIM/2FA、`V2` 命名 | 见 `10-legacy-debt-and-roadmap.md` | ✅ 阶段 3 + 收尾 + 续：Go 侧死代码、role/project store API、metric 栈、CEL 死代码已删（`a39bc41`–`3cc4926`）；proto 表面收敛完成——Engine 28→5、DataSource 多引擎/IAM/SASL/Vault 字段、9 个未实现 setting、OIDC/LDAP、`recovery_codes`/`source`、policy/role/project 死消息、不可达 metadata 消息、`service_name`/`granularity`、store 死 openlineage 消息全部删除（`ceb6a3d` `e0eab33` `722d3cb` `ddff264` `ee3c39b` `b2e80ae`）；`role`/`project` 表与 `db.project` 列已 DROP（`904fb09` `451cb78`）；`V2` 命名重命名完成（`8b328ae`）；M 系列全部处理（M9/M4 见阶段 3 收尾二）。 |
| **测试/CI 缺口** | CI 从不跑 hermetic 测试；缺 Docker 时集成测试硬失败；auth 零测试 | `09-tests.md` | ◐ 阶段 3/补遗：CI 新增 `-race` 单测 + lint job、`api/auth` 与 `backend/server` 从零建立测试（`d3d96c1` `0dae0b7`）；集成套件现已**全部通过**（既有失败根因是 harness 的 inspectStore 缓存，`f50fbbc`）；顺带修掉 `backend/server` 测试约 1/5 概率的 `-race` 竞态（`a16c8d4`）；阶段 3 收尾二补齐了缺 Docker 的 skip 行为（T-C2）、前端 job + 首批 Vitest、`./backend/migrator/...` 并入集成 target、store 查询形状 guard（T-H3）、审计 helper/debug 拦截器/server 启停/llm 组件测试；`./backend/migrator/...` 现在随 `make test-integration` 一起跑。**剩余**：CI 从未在 GitHub 实跑、前端覆盖率、`09` 的低优先项 |

---

## 按模块的问题数量概览

| 模块 | 严重 | 高 | 中 | 低/债务 | 一句话结论 |
| --- | --- | --- | --- | --- | --- |
| 01 入口/装配 | 2 | 5 | 6 | 6 | 全局安全问题集中地；日志/flag 大量未接线 |
| 02 认证/授权/审计 | 4 | 4 | 14 | 8 | 后端风险最高模块，必须优先修 |
| 03 Store | 1 | 6 | 26 | 20+ | 注入 + 缓存禁用导致的全表扫描 + 引用不存在表 |
| 04 api/v1 | 4 | 13 | 30 | 24 | 无授权 + 注入 + SSRF + 缓存串租户 + 无界查询 |
| 05 组件 | 0 | 4 | 10 | 5 | LLM agent 泄漏/截断/"假流式"；dbfactory 是 SSRF 入口 |
| 06 Runner/Migrator | 1 | 6 | 13 | 10+ | 缺增量迁移；同步协程死亡；破坏性 diff |
| 07 common/utils | 0 | 2 | 6 | 10+ | 约 60% 死代码；错误码约定未执行 |
| 08 Proto | 1 | 6 | 23 | 大量 | store/v1 契约分叉；AIP 违规；审计脱敏根因 |
| 09 测试 | 2 | 5 | 10 | 5 | CI 不跑单测；auth 零测试；guard 测试缺失 |

> 阶段 0/1/2 修复后，上表中的问题数量尚未重新统计；已修复条目见各阶段修复状态与各模块报告中的 ✅/◐ 标记。新增测试：`backend/api/v1/filter_injection_test.go`、`backend/api/v1/filter_type_safety_test.go`、`backend/api/v1/instance_data_source_test.go`、`backend/api/v1/common_test.go`、`backend/api/v1/audit_test.go` 扩展、`backend/runner/schemasync/syncer_test.go` 扩展、`backend/store/principal_test.go`、`backend/store/db_connection_test.go`、`backend/store/meta_resource_test.go` 扩展、`backend/plugin/openlineage/resolver_test.go` 扩展、`backend/component/llm/agent_test.go`；**阶段 3 新增**：`backend/api/v1/filter_test.go`、`backend/api/v1/pagination_test.go`、`backend/api/v1/error_interceptor_test.go`（即改写后的 `common_test.go`）、`backend/common/error_test.go`、`backend/common/utils_test.go`、`backend/store/setting_test.go`、`backend/api/auth/auth_test.go`、`backend/server/echo_routes_test.go`。**阶段 3 收尾**没有新增测试，但修掉了 `backend/common/utils_test.go` 里一个会随机失败的断言（`4afe1ba`）。**阶段 3 补遗新增**：`backend/common/guid_test.go`、`backend/common/cel_test.go`（含“未绑定变量必须 fail closed”用例）、`backend/api/v1/pagination_test.go` 的血缘分页用例；并修掉 `backend/server/echo_routes_test.go` 的 `-race` 竞态（`a16c8d4`）。**阶段 3 收尾二新增**：`backend/test/integration/dockerutil/dockerutil_test.go`、`backend/test/integration/env/env_config_test.go`、`backend/store/meta_resource_query_test.go`、`backend/store/database_test.go`、`backend/store/policy_test.go`、`backend/store/manual_sql_pure_test.go`、`backend/api/v1/audit_helpers_test.go`、`backend/api/v1/debug_interceptor_test.go`、`backend/server/server_lifecycle_test.go`、`backend/component/llm/{fetcher,message,tools}_test.go`、`backend/test/integration/runner/{auth_reverse_service_test.go,instance_data_source_service_test.go}`，以及首批前端 `frontend/src/{utils/error.test.ts,api/lineage.test.ts}`（12 个用例）。**阶段 5**：`backend/common/utils_test.go` 删除 AES 的 4 个用例（保留资源名测试），`backend/store/setting_test.go` 从「密钥优先级/短 key 拒绝」重写为「已解析 secret 走缓存、不查库」的 hermetic guard。

---

## 建议的阅读与整改顺序

1. **先读** [`02-auth-authorization.md`](02-auth-authorization.md) 与 [`01-entrypoint-server.md`](01-entrypoint-server.md)，它们覆盖最紧急的安全边界。
2. **再读** [`04-api-v1.md`](04-api-v1.md) 与 [`03-store.md`](03-store.md)，覆盖注入、SSRF、无界查询与持久层正确性。
3. **然后** [`06-runners-migrator.md`](06-runners-migrator.md)（迁移与同步的正确性/数据安全）。
4. **最后** [`05`](05-components.md)、[`07`](07-common-utils.md)、[`08`](08-proto-contract.md)、[`09`](09-tests.md) 与 [`10`](10-legacy-debt-and-roadmap.md)（组件、基础设施、契约、测试、清理路线）。
5. 整改排期见 [`10-legacy-debt-and-roadmap.md`](10-legacy-debt-and-roadmap.md) 第四节："阶段 0：安全止血"（4 条完整修复、2 条部分修复）、"阶段 1：正确性与可运维性"（3 条完整修复、2 条部分修复）、"阶段 2：性能与资源"（4 条完整修复、1 条部分修复）、"阶段 3：清理与重构"（17/18 完整修复；19/20 各有一项经确认推迟，其中 20 的推迟范围已在"阶段 3 收尾：proto 表面收敛"一节做完，19 的前端 job/migrator 集成/Docker skip 仍未做）、"阶段 3 续：proto 残留、schema 清理与 M 系列"（25–30 完成，仅 M9 经确认跳过）、"阶段 3 补遗：正确性遗留、安全加固、性能与保留、引擎覆盖"（31–40 完成）、"阶段 3 收尾二：测试与 CI 收口 + M9/M4 资源化"（41–50 完成，M9/M4 已落地）、"阶段 4：IAM 权限管理"（`6fddae6`）、"阶段 5：settings 收回数据库 + 回滚 METADATA_SECRET_KEY"（51–54 完成）均已完成，剩余项已逐条标注，可在对外部署前作为基线。

---

## 关于本报告的确定性

- 所有条目均附 `文件:行号` 与代码摘录；标注"待确认"的条目表示需要作者确认或需要集成测试/运行时验证。**阶段 1 已关闭两条**：`db_schema` 的实际报错形态（该过滤器被整体删除，`bb93ee0`）与 `SyncDBSchema` 是否会静默返回空/部分快照（会：MySQL 的 `information_schema` 按权限过滤行，`fcb6a98`）。**阶段 2 又关闭一条**：`enableCache` 的去留（经确认启用，`f22f61e`）。**阶段 3 收尾关闭四条**：`principal.mfa_config` 是死列（2FA 无实现）、`store.ExplainSQLCache` 应删（已删）、`policy`/`user_group` 表是活路径、`project`/`role` 表无 Go 调用者但受 `db.project` 外键约束不能直接删（`08` 已逐条补注）。**阶段 3 续关闭三条**：`setting.value` 是**有意**的 `text`（多态列，`d8ce592`）、`transformation`/`raw_payload` 的编码问题已解决（`997ede9` `b2e80ae`）、`project`/`role` 表与 `db.project` 列已实际删除（`451cb78` `904fb09`）。**阶段 3 补遗又关闭三条**：集成套件里那条既有失败的根因（是集成 harness 的 `inspectStore` 缓存了别的进程写入前的行，不是 lineage analyzer 回写，`f50fbbc`）、`MARIADB`/`OCEANBASE` 乃至 `TIDB` 的 schema/血缘/driver 注册缺口（`729db71`）、`search_text` 是否为可行索引方案（已实测谓词等价且走 trgm 索引）。**阶段 3 收尾二又关闭一条**：M9/M4 的资源化决策——`DataSource` 与 OpenLineage/API key 均已按 AIP 资源化（`513940f` `c2a67e0`）。**阶段 5 关闭一条并重开一条**：`METADATA_SECRET_KEY` 的注入与轮换不再是待确认项——该密钥连同 `JWT_SECRET` 一起删除，运行期配置统一回到数据库设置（`7870016`）；代价是"同库密钥 XOR"重新成立，`07` U-H2 / `04` M18 重新成为待办。仍待确认的集中在：部署拓扑（是否有反向代理、是否单租户）、`RETURNING` 顺序；另有一条流程性事实：CI workflow 从未在 GitHub 实跑。
- 少数结论已通过独立执行验证（例如 `parseStructuredResponse` 的 `"## ## "` 缺陷用独立程序复现）。
- 一处此前的推测已被更正：cel-go v0.26.1 的 `expr.AsCall()` 是 Kind 守卫的、不会 panic；真正会 panic 的是未检查的 `value.(string)` 类型断言与对非字面量调用 `AsLiteral().Value()`（详见 `07` M3，阶段 1 已修，`ff914ac`）。
