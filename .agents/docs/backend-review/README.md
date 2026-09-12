# Metaxisdata 后端代码 Review（第一版）

**审查范围**：`/home/ran/gocode/metaxisdata/backend/` 下除 `backend/plugin/`（按第一版要求排除）与 `backend/generated-go/`（buf 生成，禁止手改）以外的全部 Go 代码，外加 `proto/v1`、`proto/store` 契约、`backend/migrator/migration/LATEST.sql`、`Makefile` 与 CI 配置。约 2.6 万行。

**审查方式**：按模块通读源码（含 `store`、`api/v1`、`api/auth`、`server`、`runner`、`migrator`、`component`、`common/utils`、`proto`、测试基础设施），并对每个发现给出 `文件:行号` 与代码证据。审查期间未修改任何代码。

**验证基线**（本次环境实测）：
- `go build ./...` ✅ exit 0
- `go vet ./...` ✅ exit 0
- `go test ./...`（hermetic）✅ exit 0，无需 PostgreSQL/MySQL/Docker
- `golangci-lint run` ⚠️ **本环境无法运行**（`context loading failed: no go files to analyze`，`--no-config` 下报 cache 只读/条目缺失；在最小临时模块上同样复现）——属环境限制，**lint 清洁度未验证**。

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
`backend/server/grpc_routes.go:85` 的 ACL 拦截器被注释，且其引用的 `NewACLInterceptor`/`iamManager` 在仓库中已不存在。`AuthContext.Permission` 被解析出来后**没有任何消费者**，没有任何 proto 方法设置 `permission`。后果：
- 任意已认证用户可修改**任意用户（含管理员）的密码**并登录（`user_service.go:500-507`，无权限校验、无原密码校验）；
- 可删除/恢复任意账号、修改任意邮箱；
- 可 CRUD 任意实例、轮换数据源凭据、触发任意实例同步；
- 可读取任意实例血缘、全部 OpenLineage run 与 `raw_payload`；
- 可创建/吊销全局 ingestion key。
整个身份层唯一真正生效的检查是 `ListAuditLogs`。

### 2. JWT 签名密钥是公开常量
`backend/bin/server/cmd/profile_dev.go:11` 把 `Secret` 硬编码为 `"00000000-0000-0000-0000-000000000000"`，且 `activeProfile` 是唯一实现（无 build tag、无 prod 版本），`Mode` 恒为 `dev`。攻击者可自行签发 `sub=1`（首个用户即 workspaceAdmin）、`aud=mt.user.access.dev` 的 token，且**跨实例通用**。随机生成的 `AUTH_SECRET` 只用于字段混淆，从不参与 JWT。

### 3. SQL 注入（6 处）
CEL 过滤器翻译把用户可控字符串直接拼进 SQL：
- `user_service.go:256`（`ListUsers` 的 `.matches()`）
- `instance_service.go:152,154,156`（`ListInstances`）
- `database_service.go:874,880`（`ListDatabases`）
- `store/principal.go:233`、`store/group.go:111`（project ID 拼进 CTE）
任意已认证用户可达；由于注册接口未认证开放（`CreateUser` 带 `allow_without_credential`，且 `DisallowSignup` 校验被注释），未认证攻击者可先注册再注入。

### 4. 敏感信息落库与回传
- **明文 OpenLineage ingestion key**：`CreateAPIKey` 标了 `audit = true`，审计拦截器把**响应**写入 `audit_log.response`，而脱敏列表漏了裸字段名 `key` → key 可经 `ListAuditLogs` 读回（`02` H3、`04` B-C1）。
- **TLS 私钥 / GCP 服务账号 JSON / Kerberos keytab**：`sslKey`/`content`/`keytab` 同样不在脱敏列表 → 明文进 `audit_log`（`08` P-C1、`04` A-H5）。
- **panic 时把完整 Go 堆栈返回给客户端**（`grpc_routes.go:73-78`）。
- **凭据"加密"是重复密钥 XOR，密钥与密文同库**（`common/utils.go:65-84`）→ 有 DB 读权限即可还原全部实例密码/SSH 私钥/LLM API key。

### 5. 与 schema 不一致的功能必然失败
- `LATEST.sql` **没有 `db_schema` 表**，而 `ListDatabases` 的 `table` 过滤器硬编码 join 它 → 该公开功能 100% 报 `relation does not exist`。
- `migration/` **没有增量目录**，只有 `LATEST.sql` → 只改 `LATEST.sql` 的 schema 变更永远到不了已有部署，新装与升级静默分叉。
- 若干部件引用已删除的表（`issue`、`query_history` 等）。

---

## 横切主题

| 主题 | 说明 | 主要位置 |
| --- | --- | --- |
| **授权缺失** | 拦截器被注释、`permission` 从不校验 | `server/grpc_routes.go:85`、`api/auth/auth.go:350` |
| **SQL 拼接** | 4 个 handler + 2 个 store 把用户输入拼进 `WHERE` | `user/instance/database_service.go`、`store/principal.go`、`store/group.go` |
| **秘密处理** | 硬编码 JWT 密钥、XOR"加密"、审计脱敏遗漏 | `profile_dev.go:11`、`common/utils.go`、`api/v1/audit.go:197` |
| **未认证入口** | `CreateUser` 免凭证 + 首个用户自动管理员 | `user_service.proto:53`、`user_service.go:286-398` |
| **缓存被禁用但仍在写** | `store.New(..., false)` 使所有 LRU 读失效，写仍发生；`GetUserByID` 因此每请求全表扫描 | `server/server.go:70`、`store/principal.go:89-114` |
| **错误码不生效** | `common.Code` 无映射链路，store 的 NotFound/Conflict 到客户端变 500 | `common/error.go:87`、`server/grpc_routes.go:80` |
| **日志系统未接线** | `LogLevel`/`Replace` 从未安装，`--debug`/`--enable-json-logging` 无效 | `common/log/log.go`、`cmd/root.go:72,78` |
| **无界查询 / N+1** | OpenLineage 数据集全表 + payload 解析；血缘无分页；`queueAll` 每小时全表 | `openlineage_dataset.go:40,119`、`lineage_service.go:57`、`analyzer.go:105` |
| **分页不一致** | 标准 page_token 与 OpenLineage 裸 offset、LLM 无 token、sublevel 无 offset 并存 | `proto/v1/*`、`api/v1/common.go:338` |
| **大量 Bytebase 遗留** | IAM/role/project/issue/多引擎/SCIM/2FA、`V2` 命名 | 见 `10-legacy-debt-and-roadmap.md` |
| **测试/CI 缺口** | CI 从不跑 hermetic 测试；缺 Docker 时集成测试硬失败；auth 零测试 | `09-tests.md` |

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

---

## 建议的阅读与整改顺序

1. **先读** [`02-auth-authorization.md`](02-auth-authorization.md) 与 [`01-entrypoint-server.md`](01-entrypoint-server.md)，它们覆盖最紧急的安全边界。
2. **再读** [`04-api-v1.md`](04-api-v1.md) 与 [`03-store.md`](03-store.md)，覆盖注入、SSRF、无界查询与持久层正确性。
3. **然后** [`06-runners-migrator.md`](06-runners-migrator.md)（迁移与同步的正确性/数据安全）。
4. **最后** [`05`](05-components.md)、[`07`](07-common-utils.md)、[`08`](08-proto-contract.md)、[`09`](09-tests.md) 与 [`10`](10-legacy-debt-and-roadmap.md)（组件、基础设施、契约、测试、清理路线）。
5. 整改排期见 [`10-legacy-debt-and-roadmap.md`](10-legacy-debt-and-roadmap.md) 第四节，其中"阶段 0：安全止血"应在任何对外部署前完成。

---

## 关于本报告的确定性

- 所有条目均附 `文件:行号` 与代码摘录；标注"待确认"的条目表示需要作者确认或需要集成测试/运行时验证，主要集中在：部署拓扑（是否有反向代理、是否单租户）、`enableCache` 是否有意关闭、`RETURNING` 顺序、`db_schema` 的实际报错形态、以及部分 proto 字段是否为有意保留。
- 少数结论已通过独立执行验证（例如 `parseStructuredResponse` 的 `"## ## "` 缺陷用独立程序复现）。
- 一处此前的推测已被更正：cel-go v0.26.1 的 `expr.AsCall()` 是 Kind 守卫的、不会 panic；真正会 panic 的是未检查的 `value.(string)` 类型断言与对非字面量调用 `AsLiteral().Value()`（详见 `07` M3）。
