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

**修复状态标记**（用于下文全部模块报告）：

| 标记 | 含义 |
| --- | --- |
| ✅ **已修复（阶段 0）** | 阶段 0 已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 0）** | 阶段 0 消除了主要风险，但有明确剩余项（报告中已列出） |
| ✅ **已修复（阶段 1）** | 阶段 1 已修完并验证，附对应 commit |
| ◐ **部分修复（阶段 1）** | 阶段 1 处理了经确认的部分，剩余项已在报告中写明 |
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

**严重级别定义**：
- **严重（Critical）**：可被外部利用的安全漏洞，或必然导致数据泄露/功能完全不可用。
- **高（High）**：明确的正确性/安全/稳定性缺陷，会在生产环境中造成实质影响。
- **中（Medium）**：缺陷或性能问题，需要修复但不紧急。
- **低（Low）**：代码质量、可维护性、局部健壮性问题。

---

## 执行摘要

第一版审查以独立条目形式记录 **101 条发现**（严重 15 条、高 51 条、中 20 条，其余为低/债务），另有大量中低问题以清单形式列在各模块末尾。跨模块存在重复计数（例如"授权缺失"在 02/04 各出现一次、"JWT 密钥"在 01/02 各出现一次），去重后独立问题约 80 条。最需要立即处理的是 5 类问题：

### 1. 授权层实际上不存在（贯穿全部 API）
> **◐ 部分修复（阶段 0）** · `ec49607` `0f2165e`：ACL 拦截器已重建并接线，写操作（用户增删改、实例/数据源、`SyncDatabase`、OpenLineage namespace/key、LLM profile、settings）均要求 workspaceAdmin。**剩余**：读路径（列表/血缘/run/raw_payload）仍是"任意已认证用户"；细粒度 role→permission 映射推迟。

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
> **◐ 部分修复（阶段 1）** · `bb93ee0`：`table` 过滤器与它 join 的幽灵表 `db_schema` 一并删除（该过滤器从来不可能工作）。**剩余**：`migration/` 仍没有增量目录（经确认当前无待发布 schema 变更，暂不创建，见 `06` R-H5）；`issue`/`query_history` 等引用不存在表的遗留代码仍在。

- ~~`LATEST.sql` **没有 `db_schema` 表**，而 `ListDatabases` 的 `table` 过滤器硬编码 join 它 → 该公开功能 100% 报 `relation does not exist`。~~ 已删除该过滤器（`bb93ee0`）。
- `migration/` **没有增量目录**，只有 `LATEST.sql` → 只改 `LATEST.sql` 的 schema 变更永远到不了已有部署，新装与升级静默分叉。
- 若干部件引用已删除的表（`issue`、`query_history` 等）。

---

## 阶段 0「安全止血」修复状态

阶段 0 的 6 项要求均有对应提交（多数一项一个 commit，第 2/6 项共享 `ec49607`），另有 `099fbc4` 固定 buf 插件版本以隔离代码生成 churn。详见 `10-legacy-debt-and-roadmap.md` 第四节。

| # | 阶段 0 要求 | 状态 | 提交 | 落地说明 |
| --- | --- | --- | --- | --- |
| 1 | JWT 签名密钥环境注入 + fail-closed；作废历史 token | ✅ | `adfec91` `84b16db` | 删除硬编码常量；`JWT_SECRET` 环境变量优先，缺失时回退 DB 中每部署随机的 `AUTH_SECRET`；`< 32` 字符启动失败；解析侧加 `WithValidMethods(HS256)`/`WithIssuer`/`WithExpirationRequired`，历史 token 全部失效。prod profile（`-tags release` ⇒ `Mode=prod`）已可编译并通过 `go vet -tags release ./...`。**注意**：无任何构建目标使用该 tag，默认构建仍为 dev（见 `01` H2 的 CORS 影响）。 |
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
| 10 | 接线日志系统 + 注册 `--external-url` + `-tags release` 进构建目标 | ✅ | `7fdcead` | `setupLogging` 用 `HandlerOptions{AddSource, Level: LogLevel, ReplaceAttr: log.Replace}` 构造 Text/JSON handler 并 `slog.SetDefault`；`--external-url` 注册并写入 `profile.ExternalURL`（SSO 回调 base 不再为空）；新增 `make build-release`（`-tags release` ⇒ prod profile），`make build` 与 AGENTS.md 开发构建保持 dev，文档同步更新。 |
| 11 | `UpdateInstance(data_sources)` 按 ID 合并；`UpdateDatabase` 判空 | ✅ | `20e284b` | `mergeDataSources`/`mergeDataSource` 用 `proto.Merge` 按 ID 叠加：请求未带的凭据、SSL/SSH 私钥、`verify_tls_certificate` 等 store-only 字段全部保留，成员仍由请求列表决定（缺席即删除）；顺带在 API 层补"恰好一个 ADMIN"校验。`UpdateDatabase` 对 `GetDatabaseV2` 的 `(nil, nil)` 返回 `NotFound`，不再 panic。 |

**已知语义边界**：`UpdateInstance(data_sources)` 用的是"空值即未变更"，因为 proto3 无法区分"未发送"与"发送了空串"——要清空某个非密钥字段需删除后用 `AddDataSource` 重建。

---

## 横切主题

| 主题 | 说明 | 主要位置 | 修复状态 |
| --- | --- | --- | --- |
| **授权缺失** | 拦截器被注释、`permission` 从不校验 | `server/grpc_routes.go:85`、`api/auth/auth.go:350` | ◐ 阶段 0：拦截器已恢复，写操作限管理员；读路径未收紧 |
| **SQL 拼接** | 4 个 handler + 2 个 store 把用户输入拼进 `WHERE` | `user/instance/database_service.go`、`store/principal.go`、`store/group.go` | ✅ 阶段 0：全部参数化 + project ID 校验 + guard 测试 |
| **秘密处理** | 硬编码 JWT 密钥、XOR"加密"、审计脱敏遗漏 | `profile_dev.go:11`、`common/utils.go`、`api/v1/audit.go:197` | ◐ 阶段 0：JWT 与审计脱敏已修；XOR 混淆仍在 |
| **凭据被往返请求清空** | `UpdateInstance(data_sources)` 整体替换数据源列表，丢掉读取路径不返回的密钥/store-only 字段 | `instance_service.go:405-413,1307-1353` | ✅ 阶段 1：按 ID 合并（`20e284b`） |
| **未认证入口** | `CreateUser` 免凭证 + 首个用户自动管理员 | `user_service.proto:53`、`user_service.go:286-398` | ✅ 阶段 0：按 `disallow_signup` 判定，首管理员授予原子化 |
| **缓存被禁用但仍在写** | `store.New(..., false)` 使所有 LRU 读失效，写仍发生；`GetUserByID` 因此每请求全表扫描 | `server/server.go:70`、`store/principal.go:89-114` | ⏳ 未处理（阶段 2） |
| **错误码不生效** | `common.Code` 无映射链路，store 的 NotFound/Conflict 到客户端变 500 | `common/error.go:87`、`server/grpc_routes.go:80` | ◐ 阶段 1：filter 解析统一 `InvalidArgument`；`common.Code`→Connect 映射仍未做（阶段 3） |
| **日志系统未接线** | `LogLevel`/`Replace` 从未安装，`--debug`/`--enable-json-logging` 无效 | `common/log/log.go`、`cmd/root.go:72,78` | ✅ 阶段 1：`slog.SetDefault` + Text/JSON handler（`7fdcead`） |
| **无界查询 / N+1** | OpenLineage 数据集全表 + payload 解析；血缘无分页；`queueAll` 每小时全表 | `openlineage_dataset.go:40,119`、`lineage_service.go:57`、`analyzer.go:105` | ⏳ 未处理（阶段 2） |
| **分页不一致** | 标准 page_token 与 OpenLineage 裸 offset、LLM 无 token、sublevel 无 offset 并存 | `proto/v1/*`、`api/v1/common.go:338` | ⏳ 未处理（阶段 3） |
| **大量 Bytebase 遗留** | IAM/role/project/issue/多引擎/SCIM/2FA、`V2` 命名 | 见 `10-legacy-debt-and-roadmap.md` | ◐ 阶段 1：删掉 `db_schema`/`table` 过滤器一处；其余仍在（阶段 3） |
| **测试/CI 缺口** | CI 从不跑 hermetic 测试；缺 Docker 时集成测试硬失败；auth 零测试 | `09-tests.md` | ◐ 阶段 0/1 新增 10 个 guard 测试；CI 与 auth 测试未补 |

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

> 阶段 0/1 修复后，上表中的问题数量尚未重新统计；已修复条目见"阶段 0/1 修复状态"与各模块报告中的 ✅/◐ 标记。新增测试：`backend/api/v1/filter_injection_test.go`、`backend/api/v1/filter_type_safety_test.go`、`backend/api/v1/instance_data_source_test.go`、`backend/api/v1/audit_test.go` 扩展、`backend/runner/schemasync/syncer_test.go` 扩展。

---

## 建议的阅读与整改顺序

1. **先读** [`02-auth-authorization.md`](02-auth-authorization.md) 与 [`01-entrypoint-server.md`](01-entrypoint-server.md)，它们覆盖最紧急的安全边界。
2. **再读** [`04-api-v1.md`](04-api-v1.md) 与 [`03-store.md`](03-store.md)，覆盖注入、SSRF、无界查询与持久层正确性。
3. **然后** [`06-runners-migrator.md`](06-runners-migrator.md)（迁移与同步的正确性/数据安全）。
4. **最后** [`05`](05-components.md)、[`07`](07-common-utils.md)、[`08`](08-proto-contract.md)、[`09`](09-tests.md) 与 [`10`](10-legacy-debt-and-roadmap.md)（组件、基础设施、契约、测试、清理路线）。
5. 整改排期见 [`10-legacy-debt-and-roadmap.md`](10-legacy-debt-and-roadmap.md) 第四节："阶段 0：安全止血"（4 条完整修复、2 条部分修复）与"阶段 1：正确性与可运维性"（3 条完整修复、2 条部分修复）均已完成，剩余项已逐条标注，可在对外部署前作为基线。

---

## 关于本报告的确定性

- 所有条目均附 `文件:行号` 与代码摘录；标注"待确认"的条目表示需要作者确认或需要集成测试/运行时验证。**阶段 1 已关闭两条**：`db_schema` 的实际报错形态（该过滤器被整体删除，`bb93ee0`）与 `SyncDBSchema` 是否会静默返回空/部分快照（会：MySQL 的 `information_schema` 按权限过滤行，`fcb6a98`）。仍待确认的集中在：部署拓扑（是否有反向代理、是否单租户）、`enableCache` 是否有意关闭、`RETURNING` 顺序、以及部分 proto 字段是否为有意保留。
- 少数结论已通过独立执行验证（例如 `parseStructuredResponse` 的 `"## ## "` 缺陷用独立程序复现）。
- 一处此前的推测已被更正：cel-go v0.26.1 的 `expr.AsCall()` 是 Kind 守卫的、不会 panic；真正会 panic 的是未检查的 `value.(string)` 类型断言与对非字面量调用 `AsLiteral().Value()`（详见 `07` M3，阶段 1 已修，`ff914ac`）。
