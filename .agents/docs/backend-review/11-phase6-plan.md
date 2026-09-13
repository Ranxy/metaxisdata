# 阶段 6 实施计划（backend-review 收尾）

## 进度（随实施更新）

| 步骤 | 状态 | 提交 |
| --- | --- | --- |
| A1 Token 吊销加固（`02 H1`） | ✅ | `976ebc5` |
| A2 CORS / CSRF 收口（`01/02 H2`） | ✅ | `976ebc5` |
| A3 登录时间与限流（`02 M2`） | ✅ | `8ae989f` |
| A4 OAuth2 state + 配置校验 + 脱敏日志（`02 M4`） | ✅ | `25a001a` |
| A5 审计链路加固（`02 M8/M9/M10`、`04 B-C1` 残留） | ✅ | `2208521` |
| A6 ingestion key digest + 作用域（`04 B-H6/B-H8`、`03 M25`） | ✅ | `415e16e` |
| A7 其它安全缺口（`01 M6`、`A-H1` 残留、`07 U-H2`、`S-H5` 之外的杂项） | ✅ | `f112e5c` |
| A8 token header 白名单与 web token 回传（`02` 低节） | ✅ | `c30d73f` |
| D1 修复既有失败测试 | ✅ | `7912fc2` |
| B1 engine 过滤按枚举名比较（`03 S-H5`） | ✅ | `824232a` |
| B2 SyncInstance 返回过滤后的库列表（`06 M11`） | ✅ | `68f3149` |
| B3 悬空血缘清理 + manual SQL 旧 GUID（`06 M7`、`03 M15`） | ✅ | `9c69196` |
| B4 事务回滚与单语句去事务（`03 M2`） | ✅ | `a71716a` |
| B5 `RETURNING` 按键回填（`03 M3`） | ✅ | `7f0e3a0` |
| B6 历史谓词配对（`03 M7`） | ✅ | `7f0e3a0` |
| B7 `UpdateDatabase` 单事务加锁（`03 M5`） | ✅ | `eca3686` |
| B8 LLM 空 mask 部分更新（`04 B-M10`） | ✅ | `14b8f01` |
| B9 `parseStructuredResponse` 标题残留（`04 B-M17`） | ✅ | `4d936c3` |
| B10 血缘失败退避重试（`06 M2`） | ✅ | `480b957` |
| B11 runner panic 隔离（`06 M9`） | ✅ | `480b957` |
| B13 API 输入校验与错误映射（`04 A-M5/A-M9/A-M10`） | ✅ | `48dbecb` |
| B14 store 失败不再降级（`04 B-M7/B-M16`、`03 M21`） | ✅ | `0151bcb` |
| B15 nil 防护与解析修正（`05 C-H4/L2`、`04 B-M8`） | ✅ | `e7239db` |
| B17 `disallow_password_signin` 覆盖服务账号（`02 M7`） | ✅ | `83b1229` |
| B12 `DiffMetadata` 与历史比较（`04 A-H2/A-H3/A-M11/A-M12`） | ✅ | `f58c387` |
| B16 `RequireResetPassword` / `allow_missing`（`02 M6/M12/M13`） | ✅ | `bedadf7` |
| C1 ingestion 批次上限与单事务（`04 B-H7`） | ✅ | `e7d15eb` |
| C2 task 聚合增量计数（`03 M22`） | ✅ | `e7d15eb` |
| C3 OpenLineage 列表默认 LIMIT（`03 M26`） | ✅ | `311e790` |
| C4 历史批量关闭与下推分页（`03 M6/M9`） | ✅ | `3e6fbda` |
| C5 `ListDatabases` 批量取实例（`04 A-M8`） | ✅ | `a7ea214` |
| C6 external dataset 去写放大（`03 M20`） | ✅ | `99d41ea` |
| C7 LLM 会话预算与轮数（`04 B-M12`） | ✅ | `03c17b7` |
| C8 实例密钥每页只取一次（`03` 低节） | ✅ | `390a66c` |
| D2/D3 全量验证与文档同步 | ⏳ | — |

A 批完成时已验证：`gofmt -l` 空、`go build ./...`、`go vet`（默认/release/integration）、`golangci-lint`（0 issues）、
`go test ./...`、`go test -race -count=1 ./...`、`make build-release` 全绿；A1/A8 的集成用例在真实 server 上通过；
A6 的增量迁移在本地 PostgreSQL 16 上验证了全新安装、增量重复执行幂等与列/索引形状。

---

依据：`.agents/docs/backend-review/README.md` 与 `01`–`10` 各模块报告的「仍未处理 / 剩余 / 待确认」清单，逐条核对当前代码后整理。
本计划只覆盖**经确认要实施**的范围；未选入范围的条目在文末「本轮不做」中列明。

## 零、本次经确认的决策

| 议题 | 决策 |
| --- | --- |
| 整改范围 | **安全残留 + 正确性缺陷 + 性能/资源** 三批 |
| 凭证混淆 | **保持 `AUTH_SECRET` 种子 XOR**（尊重阶段 5 回滚），只补空 seed 防护并在文档写明已知取舍 |
| 部署拓扑 | **单租户**：读路径维持现有 `permission` 注解，不新增 per-instance 读授权 |
| 破坏性 schema 同步 | **保持仅日志**（现状，不再改行为） |
| CI/验证 | **只在本地跑全量验证**（含 Docker 集成套件），并把结果写进文档；不改 CI workflow |
| gRPC 反射 | **保留匿名反射**（现状，仅把该策略写进文档） |

**基线（本轮开始时实测）**：`gofmt -l` 空；`go build ./...`、`go vet ./...` 通过；`go test ./...` 仅 `backend/store` 的
`TestMarshalRolePermissionsIsDeterministic` 失败（protojson 在数组逗号后**故意随机**插入空格，断言写死 `","`）；
本机 Docker 可用，集成套件可真实运行。

---

## 一、A 批 · 安全残留

### A1. Token 吊销加固（`02 H1`）
- 目标：伪造的 `Logout` 不再能淘汰真实吊销记录；改密/改邮箱/删号后旧 token 失效。
- 改动：
  1. `Logout` 先校验 token 签名/issuer/audience/有效期，只吊销**当前有效**的 token；无效 token 直接 `Unauthenticated`（不再往缓存写任意字符串）。
  2. 吊销缓存容量从 128 提升到可配置上限（默认 4096），并在满时**拒绝吊销**而非静默淘汰（避免攻击者"冲掉"记录）。
  3. 改密后旧 token 失效：校验时把 token 的 `iat` 与用户 `Profile.LastChangePasswordTime` 比较，早于改密时间的 token 直接拒绝。这是 DB 支撑的判定，天然跨副本，无需新增状态。已核实 `UserService.UpdateUser` 的 `password` 路径会让 store 写入 `LastChangePasswordTime`（`store/principal.go`）。
  4. 删号已由 `authenticateConnect` 的 `MemberDeleted` 检查覆盖；改邮箱不改权限，不额外吊销。
- 文件：`backend/api/v1/auth_service.go`、`backend/api/auth/auth.go`、`backend/component/state/state.go`。
- 验收：新增 hermetic 测试（伪造 Logout 不生效、改密时间晚于 token 时被拒、缓存容量生效）；集成反向测试保留通过。
- 说明：token 吊销缓存仍是进程内，跨副本的"登出即失效"需要 DB/Redis；本阶段不做，文档记为已知限制（单租户部署）。改密吊销因走 DB 判定所以不受此限制。

### A2. CORS / CSRF 收口（`01 H2`、`02 H2`）
- 目标：默认构建不再对任意来源开放带凭证跨域；cookie 的 `SameSite`/`Secure` 依据服务端配置，而非客户端 `Origin`。
- 改动：
  1. CORS 改为**显式 allowlist**：新增 `Profile.CORSAllowOrigins`（`--cors-allow-origins`，逗号分隔；dev 默认 `http://localhost:3000` 等本地来源；release 默认空=不发 CORS 头）。不再用 `AllowOriginFunc` 恒真。
  2. `GetTokenCookie` 的 `SameSite` 默认 `Lax`、`Secure` 由服务端 TLS/`--external-url` 推导，删除 `origin` 入参对 `sameSite`/`Secure` 的影响（仅保留写 cookie）。
  3. CSRF 深度防御：对**带 cookie 鉴权的写请求**新增同源校验中间件——`Sec-Fetch-Site: cross-site`、或 `Origin` 与服务端 `external_url`（未配置则比对 Host）不一致时，对非 GET/HEAD 请求返回 `403`；API token / Bearer 请求不受影响。
- 文件：`backend/config/profile.go`、`backend/bin/server/cmd/root.go`、`backend/server/echo_routes.go`、`backend/api/auth/header.go`、`backend/api/v1/auth_service.go`、前端 dev 代理无关。
- 验收：`backend/server` 测试覆盖"dev allowlist 只放行配置来源 / 非 allowlist 来源无 CORS 头 / 跨站 Origin 的 cookie 写请求被 403"；`auth` 测试覆盖 SameSite/Secure 不再随 `Origin` 变化。

### A3. 登录接口加固（`02 M2`）
- 目标：消除用户枚举时间差，并对密码爆破加节流。
- 改动：
  1. `getAndVerifyUser` 在用户不存在时也执行一次固定 dummy bcrypt 比较，使两条路径耗时一致。
  2. 新增内存登录限流器（`component/state`）：按 `email` 与来源 IP 双维度滑动窗口（如 10 次/5 分钟）+ 指数退避，超限返回 `ResourceExhausted`；成功登录清零。IP 取值复用 A5 的可信来源逻辑。
- 文件：`backend/api/v1/auth_service.go`、`backend/component/state/state.go`。
- 验收：hermetic 测试覆盖"未知用户与已知用户耗时接近（同路径都调用 bcrypt）"（以调用计数/行为断言，不做脆弱的时间断言）、限流触发与清零。

### A4. OAuth2 登录补 `state` 与配置校验（`02 M4`）
- 目标：防止登录 CSRF/授权码注入；IDP 配置非法时不再 panic。
- 改动：
  1. 登录发起时生成随机 `state`（写入短期缓存，与登录会话绑定），回调时校验并一次性消费；proto 的 `OAuth2IdentityProviderContext` 增加 `state` 字段（破坏性，仓库未上线）。
  2. `oauth2.NewIdentityProvider` 对 `oauth2_config` 为 nil/缺 `client_id`/`client_secret`/`field_mapping.identifier` 返回 `InvalidArgument`，不再解引用 nil。
  3. 顺带移除 `plugin/idp/oauth2` 在 debug/error 日志中打印授权码/access token/userinfo 的行为（`02` 待确认 #6）。
- 文件：`proto/v1/v1/auth_service.proto`、`backend/api/v1/auth_service.go`、`backend/plugin/idp/oauth2/oauth2.go`；需 `buf format/lint/generate` 并同步前端类型。
- 验收：oauth2 插件 hermetic 测试（nil 配置返回错误）；handler 测试覆盖 state 缺失/不匹配被拒。

### A5. 审计链路加固（`02 M8/M9/M10`、`04 B-C1` 残留）
- 目标：审计不因客户端断开而丢；来源 IP 不可伪造；`workspaces/-` 不再绕过范围；历史明文 key 不再回传。
- 改动：
  1. `createAuditLog` 用 `context.WithoutCancel(ctx)` + 超时落库，写失败记 Error（不再完全静默）；`ListAuditLogs` 补 `audit = true` 注解。
  2. `X-Forwarded-For` 仅在新增的 `--trusted-proxies` 配置命中时采信，否则用 `RemoteAddr`。
  3. `audit_log_service.go` 删除 `parent == "workspaces/-" → ""` 的绕过（`workspaces/-` 表示全部工作区时按显式语义处理，不再清空范围）。
  4. `ListAuditLogs` 返回前对历史 `response`/`request` 再跑一次脱敏，清掉阶段 0 之前落库的明文 ingestion key。
- 文件：`backend/api/v1/audit.go`、`backend/api/v1/audit_log_service.go`、`backend/api/v1/audit_helpers*.go`、`backend/config/profile.go`、`proto`（如需注解）。
- 验收：审计 helper 测试扩展（脱敏历史 payload、可信代理开关、`workspaces/-` 语义）；现有反例测试同步更新。

### A6. OpenLineage ingestion key 加固（`04 B-H6/B-H8`、`03 M25`）
- 目标：校验从 O(N) bcrypt 扫描降为 O(1)；key 有作用域，泄露一把 key 不能伪造任意实例血缘。
- 改动：
  1. 新增 `openlineage_api_key.key_digest` 列（SHA-256 hex，唯一索引），校验时按 digest 定向查询后再 bcrypt 比对；`last_used_at` 更新改为异步/最短路径（不再在扫描循环里同步写）。迁移增量 `0.1/0007`，`LATEST.sql` 同步；旧 key 无 digest（项目未上线，记录在迁移注释）。
  2. key 作用域：`openlineage_api_key` 增加 `scope_instance_resource_id text`（NULL=全部实例）与 `scope_namespace text`（NULL=全部）；`CreateAPIKey` 增加可选 scope 字段（proto v1 + store），ingestion handler 校验事件 namespace/实例是否在作用域内，否则 `PermissionDenied`。
  3. 前端 API key 创建/列表补齐 scope 展示与录入（`frontend/src/locales` 同步）。
- 文件：`proto/store/store/openlineage.proto`、`proto/v1/v1/openlineage_service.proto`、`backend/store/openlineage_api_key.go`、`backend/api/v1/openlineage_service.go`、`backend/api/v1/openlineage_handler.go`、`backend/migrator/migration/0.1/0007##openlineage_api_key_scope.sql`、`backend/migrator/migration/LATEST.sql`、前端。
- 验收：store 定向查询 guard 测试；handler 作用域校验测试；迁移在本地 PostgreSQL 上实测（全新安装 + 从 0.1.6 升级 + 重复执行幂等）；集成套件 OpenLineage 用例通过。

### A7. 其它安全缺口（`01 M6`、`A-H1` 残留、`07 U-H2` 防护）
- `/metrics` 收口：仅在 `RuntimeDebug` 时注册，或加独立监听/Bearer 保护（默认关闭）。
- gateway 客户端补**发送**消息大小上限，与 `MaxCallRecvMsgSize(100MB)` 对齐并下调到合理值。
- `validate_only`/数据源错误的原始驱动错误（含内网 `dial tcp ip:port`）改为只回通用 `InvalidArgument`，细节写日志。
- `common.Obfuscate`/`Unobfuscate` 补 `seed == ""` 防护：空 seed 且非空输入时返回错误/空值，不再整数除零。
- `debug_interceptor` 不再用 `errors.New` 重建 `connect.Error`（保留 `Details()` 与错误链），原文只写日志。
- 文件：`backend/server/echo_routes.go`、`backend/server/grpc_routes.go`、`backend/api/v1/instance_service.go`（错误包装）、`backend/common/utils.go`、`backend/api/v1/debug_interceptor.go`。
- 验收：`common` 空 seed guard 测试；server 测试覆盖 `/metrics` 门控；debug interceptor 测试保留 Details。

### A8. `GetTokenFromHeaders` 白名单与 `web=true` token 回传（`02` 低节）
- 修复：`GetTokenFromHeaders` 的错误头不应让 Login 等免认证方法失败（在 `IsAuthenticationAllowed` 之后再要求 token）。
- `web=true` 时不再把 token 同时放进响应体（仅 HttpOnly cookie），前端相应改为依赖 cookie；若前端有依赖再评估。
- 文件：`backend/api/auth/auth.go`、`backend/api/v1/auth_service.go`、前端调用点。
- 验收：`api/auth` 测试扩展。

---

## 二、B 批 · 正确性缺陷

### B1. engine 过滤静默失效（`03 S-H5`）
- 现状：`instance.metadata->>'engine'` 存 protojson **枚举名**（`"MYSQL"`），而过滤器参数是 `storepb.Engine` 的 **int**，谓词恒 false。
- 修复：`engineFilterValue` 输出枚举名（`storepb.Engine_name[...]`）；`store/database.go`、`store/instance.go` 相关谓词保持字符串比较；补表驱动 guard 测试覆盖 `engine = "MYSQL"` 命中。
- 文件：`backend/api/v1/filter.go`、`backend/store/database.go`、`backend/store/instance.go`。
- 验收：filter 测试 + store 谓词测试；确认 `ListDatabases`/`ListInstances` 的 engine 过滤真实生效。

### B2. `SyncInstance` 返回未过滤的数据库列表（`06 M11`）
- 修复：末尾 `return ... instanceMeta.Databases ...` 改为返回 `filteredDatabaseMetadatas`（遵守 `sync_databases`），并同步 RPC 语义。
- 文件：`backend/runner/schemasync/syncer.go`、`backend/api/v1/instance_service.go`。
- 验收：`syncer_test.go` 新增 `sync_databases` 过滤用例。

### B3. 悬空血缘与镜像行（`06 M7`、`03 M15`）
- `syncer.go` 删除路径：TABLE/COLUMN 消失时同样清理 `column_lineage`（沿用 VIEW 的清理逻辑）。
- `store/manual_sql.go` `CreateManualSQL`：upsert 因 `schema_name` 变化而换 GUID 时，清理旧 GUID 的 `meta_registry_resource`、history、`column_lineage`（与 `UpdateManualSQL` 对齐）。
- 文件：`backend/runner/schemasync/syncer.go`、`backend/store/manual_sql.go`。
- 验收：store 纯函数/history guard 测试；集成 schemasync lineage 用例覆盖 TABLE drop。

### B4. 事务规范化与泄漏（`03 M2`、路线图 #6）
- `store/group.go` 的 `UpdateGroup` 补 `defer tx.Rollback()`（当前 3 处 `BeginTx` 只有 2 处 Rollback）。
- 全 `backend/store` 扫描：所有 `BeginTx` 后必须紧跟 `defer tx.Rollback()`；单语句查询不再开事务（`idp.go`、`manual_sql.go` 等）。
- 文件：`backend/store/group.go` 为主，其余按扫描结果。
- 验收：`golangci-lint` + 新增/扩展 store guard 测试；review 确认无遗漏。

### B5. `RETURNING id` 行序假设（`03 M3`、待确认 #4）
- 修复：`meta_resource.go` 批量 upsert 改为 `INSERT ... ON CONFLICT ... RETURNING id, guid, object_type`，按 `(guid, object_type)` 回填 `creates[i].ID`，并校验行数一致。
- 文件：`backend/store/meta_resource.go`。
- 验收：store guard 测试断言行数不匹配时报错、回填按键匹配。

### B6. `ANY/ANY` 笛卡尔谓词（`03 M7`）
- 修复：`listOpenMetaRegistryHistoryByKey` 的 `guid = ANY($1) AND object_type = ANY($2)` 改为 `(guid, object_type) IN (SELECT * FROM unnest($1::text[], $2::int[]))` 或逐对 `OR`，确保不匹配未请求组合。
- 文件：`backend/store/meta_resource_history.go`（及调用点）。
- 验收：query-shape guard 测试（复用阶段 3 的谓词形状断言模式）。

### B7. `UpdateDatabase` 非原子读-改-写（`03 M5`）
- 修复：元数据更新在读同一事务内完成并对行加锁（`SELECT ... FOR UPDATE`），或由调用方传入 tx；避免 `GetDatabase` 自开只读事务后再 UPDATE。
- 文件：`backend/store/database.go`、`backend/runner/schemasync/syncer.go`。
- 验收：并发用例（两个 goroutine 同时更新同一 database，最终值不丢字段）。

### B8. LLM 空 `update_mask` 清空 models（`04 B-M10`）
- 修复：`llm_service.go` 空 mask 分支改为"只写请求中非零字段"，不再整体替换 `models`；或强制要求 mask。选前者以保持 PATCH 语义。
- 文件：`backend/api/v1/llm_service.go`、`backend/store/llm.go`。
- 验收：handler 测试覆盖"只改 title 不改动 models"。

### B9. `parseStructuredResponse` 残留 `"## "`（`04 B-M17`）
- 修复：首个 section 标题归一化，去掉已带的 `"## "`；`idx == -1` 分支不再把全文当 summary 并输出空 section。
- 文件：`backend/api/v1/explain_sql_service.go`。
- 验收：新增 hermetic 用例（含模型首行即标题的场景）。

### B10. 血缘分析失败重试（`06 M2`）
- 修复：`lineageanalyzer` 失败任务重新入队（带退避/重试计数），不再等下一次小时级扫描；连续失败达上限记 Error 并保留失败版本记录。
- 文件：`backend/runner/lineageanalyzer/analyzer.go`。
- 验收：analyzer 测试覆盖失败重试与退避状态机。

### B11. worker pool panic 防护（`06 M9`）
- 修复：`schemasync` 检查协程与 `lineageanalyzer` pool 的任务包装 recover，记录日志后继续（对齐 `trySyncAll`）。
- 文件：`backend/runner/schemasync/syncer.go`、`backend/runner/lineageanalyzer/analyzer.go`。
- 验收：构造会 panic 的假 driver，断言进程不退出且后续任务继续。

### B12. `DiffMetadata` 与历史比较（`04 A-H2/A-H3/A-M11/A-M12`）
- `A-H2`：`source_time` 未设置时取**最早可用版本**（与 proto 文档一致），不再用 `now`。
- `A-H3`：`rebuildDatabaseObjects` 覆盖 differ 已支持的其余对象类型（物化视图/序列/枚举/扩展等），或在文档与 proto 中收窄语义；优先补齐高价值类型。
- `A-M11`：历史变更检测补齐列 `generation`/identity、索引 `key_length`/`descending`/`opclass`、FK `match_type` 等字段（按 differ 结构逐项对齐）。
- `A-M12`：`rebuildSchemaContents` 重建失败不再静默回退当前元数据，返回错误。
- 文件：`backend/api/v1/database_service.go`、`backend/api/v1/database_history.go`、`backend/api/v1/database_metadata.go`。
- 验收：golden/表驱动测试覆盖各对象类型与"最早版本"；错误传播用例。

### B13. 数据面 API 正确性小项（`04 A-M5/A-M9/A-M10`、低节）
- `CreateInstance` 补 environment 存在性与 engine 合法性校验（与 `UpdateInstance` 对齐）。
- `SyncDatabase` 错误经 `common.Code` 映射（缺失→`NotFound`、冲突→`AlreadyExists`）。
- `CreateManualSQL` 校验 `manual_sql_id` 合法（`common.IsValidResourceID`），非法返回 `InvalidArgument`。
- `meta_type` 为空时不再当 0 传给 store（返回 `InvalidArgument` 或按文档解析）。
- `label` 过滤器：实现文档承诺的 `in`，并允许含 `:` 的值（否则收窄文档）。
- 文件：`backend/api/v1/instance_service.go`、`backend/api/v1/database_service.go`、`backend/api/v1/filter.go`。
- 验收：handler 测试（构造 store 依赖或纯函数级）。

### B14. LLM/血缘错误语义（`04 B-M7/B-M16`、`03 M21`）
- `resolveSource`：DB 故障不再报 `NotFound`（区分 `NotFound` 与 `Internal`）。
- `collectExternalDatasets`：查询失败不再静默返回空列表，记日志/返回错误。
- `external_dataset.schema_fields`：要么在解析时写入，要么删除该列与读取路径（择一，避免"永远为空"的假数据）。优先补写入。
- 文件：`backend/api/v1/explain_sql_service.go`、`backend/api/v1/lineage_service.go`、`backend/store/external_dataset.go`。
- 验收：store 纯函数测试 + lineage 错误传播测试。

### B15. 其它明确 bug（`05 C-H4`、`05-L2`、`04 B-M8`、低节）
- `dbfactory`：`instance == nil` 或 `instance.Metadata == nil` 返回明确错误（不再解引用 panic）；`db.Open` 错误包 `common.Wrapf`。
- `state.resourceLimiter.Decrement` 加下限（计数不为负）。
- `formatResolvedTarget` MySQL 空 schema 不再把 instance id 当 table 段。
- `systemBotUser` 回退对象与 `LATEST.sql` 种子行（`support@example.com`）对齐。
- `hasLineageSignal` 删除不可达分支（或按列血缘信号正确判定）。
- 文件：`backend/component/dbfactory/dbfactory.go`、`backend/component/state/state.go`、`backend/api/v1/openlineage_dataset.go`、`backend/store/principal.go`、`backend/plugin/openlineage/metadata.go`。
- 验收：各包 hermetic 测试。

### B16. `UpdateUser` / `allow_missing` / `RequireResetPassword`（`02 M6/M12/M13`）
- `M6`：登录不再整列覆盖 `UserProfile`（改为按字段 patch，保留未知/新增字段）。
- `M12`：`allow_missing` 分支应用 `UpdateMask`，或在 proto 文档明确"忽略 mask"并拒绝同时传 mask。
- `M13`：`require_reset_password` 真正生效——登录时若为真，签发**受限 token**（仅允许改密/登出），或直接拒绝并要求改密；同步前端。
- 文件：`backend/api/v1/auth_service.go`、`backend/api/v1/user_service.go`、`backend/store/principal.go`、proto、前端。
- 验收：auth handler 测试。
- **实施（`bedadf7`，`02 M6/M12/M13`）**：
  - `M13` 选受限 token 方案：JWT 增 `rst` claim → `auth.TokenRestriction`；拦截器按 `restrictedTokenAllowedProcedures`
    白名单放行（当前仅 `UserService/UpdateUser` + `AuthService/Logout`），并把限制放进 context；`UpdateUser` 再要求
    `isSelf` 且 mask 恰为 `["password"]`。前端登录页内嵌改密表单，改密后用新密码重新登录再跳转。
  - `M6` 改为 `profileWithLastLogin`（`proto.CloneOf` + 只覆盖 `last_login_time`），既保留其它字段也不写穿 store 缓存。
  - `M12` 改为按 AIP-134 应用 mask：`applyUpdateMaskToUser` 只保留 mask 点名的字段（`user_type` 作为创建类型可被点名），
    proto 注释同步更正。
  - 验收：`auth_test.go` 的受限 token 往返/白名单用例，`auth_login_profile_test.go`、`user_update_mask_test.go`；
    `go test ./...`、`golangci-lint`（0 issues）、前端 `biome`/`eslint`/`vue-tsc`/`vitest` 全绿。

### B17. `DisallowPasswordSignin` 服务账号例外（`02 M7`）
- 决策二选一（实施时按最小惊讶原则）：对服务账号同样应用禁令；若确需例外，在 proto 与文档显式声明。
- 文件：`backend/api/v1/auth_service.go`、proto 注释。
- 验收：登录策略测试。

---

## 三、C 批 · 性能与资源

### C1. ingestion 批量摄取上限与事务（`04 B-H7`）
- 限制单请求事件数（如 1000）与请求体（如 8MiB），超限返回 `ResourceExhausted`/`413`；同批事件合并事务，减少逐事件事务。
- 文件：`backend/api/v1/openlineage_handler.go`、`backend/store/openlineage_run.go`。
- 验收：handler 测试（超限拒绝、部分失败仍 200 但 `failed` 可见，保持阶段 3 补遗行为）。

### C2. OpenLineage task 聚合增量更新（`03 M22`）
- 用增量计数替代每事件 `COUNT(*) OVER ()` 全量重算。
- 文件：`backend/store/openlineage_task.go`。
- 验收：store 测试断言聚合值一致且查询数下降（计数或 SQL 形状 guard）。

### C3. OpenLineage run/task 默认 LIMIT（`03 M26`）
- store 在 `Limit == nil` 时也施加默认上限（如 5000），上层可显式传更大值；与数据集路径对齐。
- 文件：`backend/store/openlineage_run.go`、`backend/store/openlineage_task.go`。
- 验收：query 形状 guard 测试。

### C4. 元数据历史与 history 批量（`03 M6/M9`）
- `closeOpenMetaRegistryHistory` 改批量 `UPDATE ... WHERE key IN (unnest...)`。
- 历史查询把 `Limit/Offset` 下推到 store（`database_history.go` 调用点），不再全量后再内存切片。
- 文件：`backend/store/meta_resource_history.go`、`backend/store/meta_resource_query.go`、`backend/api/v1/database_history.go`。
- 验收：分页测试（大历史量下第 2 页正确）+ query guard。

### C5. `ListDatabase` N+1 与 nil deref（`04 A-M8`）
- 批量取 instance（一次查询），并对 `GetInstance` 的 `(nil, nil)` 判空，避免 panic。
- 文件：`backend/api/v1/database_service.go`。
- 验收：handler 测试；查询次数断言（可选）。

### C6. `GetOrCreateExternalDataset` 写放大（`03 M20`）
- 已存在且字段未变时不再写库；变化时更新 `dataset_type`。
- 文件：`backend/store/external_dataset.go`。
- 验收：store 测试（第二次调用无写入）。

### C7. LLM 内存/配额与 debug 日志（`04 B-M12`、低节）
- 限制单会话大小与轮数（配置化 `MaxTurns` 真正设置、消息总量上限），超限明确报错。
- debug 日志写库用脱离请求的 ctx + 既有保留清理；避免写入完整 prompt/response 之外的额外副本。
- 文件：`backend/api/v1/explain_sql_service.go`、`backend/component/llm/agent.go`、`backend/component/llm/debug.go`、`backend/runner/maintenance`。
- 验收：agent 测试（超限报错、MaxTurns 生效）。

### C8. `unObfuscateInstance` 重复解密（`03` 低节）
- 每实例只取一次 secret、只解码一次列表，避免逐行重复 `GetSecret`。
- 文件：`backend/store/instance.go`。
- 验收：函数级测试或代码审查记录。

#### C 批实施说明
- **`C1`（`e7d15eb`）**：单请求上限 1000 事件 / 8MiB 体积，超限显式 413（`io.LimitReader` 此前会静默截断成解析错误）；
  新增 `Store.UpsertOpenLineageRuns` 让整批事件共用一个事务，`UpsertOpenLineageRun` 复用它。
- **`C2`（`e7d15eb`）**：`openlineage_task` 的计数改为增量：先取 task 行锁（`INSERT ... ON CONFLICT DO UPDATE SET
  updated_at = openlineage_task.updated_at`）串行化同一 task 的写入，再按 `(job_namespace, job_name, job_type, run_id)`
  唯一键点查旧 `has_lineage` 得到本次增量；latest 字段按 `event_time DESC NULLS LAST` 语义就地比较。批量内按 task GUID
  稳定排序避免多 task 死锁。保留清理改为 `rebuildOpenLineageTask`（唯一仍做全量聚合的路径）。
- **`C3`（`311e790`）**：`openLineagePageClause` 在 `Limit == nil` 时施加 5000 上限，run/task 两个列表共用。
- **`C4`（`3e6fbda`）**：`closeOpenMetaRegistryHistory` 改为单条 `UPDATE ... FROM unnest($1,$2)`（按键配对）；
  `ListMetadataHistory` 改为按「offset+limit+2 行、`ORDER BY valid_from DESC`」下推探测（命中
  `(guid, object_type, valid_from DESC)` 索引），最旧一行仅作上下文不计入事件；`GetMetadataHistoryEvent` 用
  `TransitionTime` 只读与事件时刻相关的行。
- **`C5`（`a7ea214`）**：`ListDatabases` 先 `distinctInstanceIDs` 再 `ListInstances(ResourceIDs)` 一次取齐，
  `convertToDatabase` 改为纯函数，实例缺失返回 `Internal` 而不是解引用 nil。
- **`C6`（`99d41ea`）**：`GetOrCreateExternalDataset` 在 `dataset_type` 未变时不再写库，变化时只更新该列。
- **`C7`（`03c17b7`）**：`llm.DefaultMaxTurns`（6）与 `llm.DefaultMaxConversationBytes`（4MiB）成为显式上限，
  `run` 用 `conversationBudget` 累计并超限报错；ExplainSQL 调用点显式传入两者。debug 日志早已是脱离请求 ctx 的
  有界队列 + 保留清理，本轮未改。
- **`C8`（`390a66c`）**：`unObfuscateInstanceWithSecret` 让整页实例只解析一次 secret（原逐行 `GetSecret` 取锁）。
- 验收：`store/openlineage_task_test.go`（增量/最新语义、分页 clause）、`api/v1/openlineage_handler_test.go`
  （批次上限、体积上限、免写路径）、`api/v1/database_history_test.go`（有界探测分页 = 全量分页）、
  `api/v1/database_convert_test.go`、`llm/agent_test.go`（MaxTurns、会话预算，走假 provider 的真实 loop）、
  `store/instance_test.go`，以及真实 server 集成用例
  `backend/test/integration/runner/openlineage_ingestion_service_test.go`（批次事务、去重计数、latest、
  lineage 增减、dataset 不重写、scope 403）。

---

## 四、D 批 · 验证与文档

### D1. 修复既有失败测试
- `backend/store/role_test.go`：`TestMarshalRolePermissionsIsDeterministic` 不再断言 protojson 的精确字节（其空格是**故意**随机的）；改为断言 `first == second` + 反序列化后的权限集合有序且相等。这是让 `go test ./...` 全绿的必要修复。
- 验收：该测试在任意构建下稳定通过，且仍锁住"排序"这一真实不变量。

### D2. 全量验证（本地）
- `gofmt -l backend/` 空；`go build ./...`、`go vet ./...`（默认/`release`/`integration`）、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release`。
- 任何 proto 改动：`buf format -w proto`、`buf lint proto`、`cd proto && buf generate`，并确认生成产物可复现（重跑无 diff）。
- 任何前端改动：`biome check`、`eslint`、`vue-tsc --build`、`vitest run`、`vite build`。
- 集成套件：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...`（Docker 可用）。
- 迁移改动（A6）：本地 PostgreSQL 16 实测全新安装、增量升级、重复执行幂等。

### D3. 文档同步（仅限本轮改动相关）
- 更新 `README.md` 与 `10-legacy-debt-and-roadmap.md`：新增「阶段 6」小节，逐条记录本计划步骤的 ✅/◐/剩余与 commit。
- 修订被本轮改变的条目（`01 H2/M6`、`02 H1/M2/M4/M8-M10/M13`、`03 S-H5/M2/M3/M5/M6/M7/M9/M20/M21/M22/M25/M26`、`04 B-H6/B-H7/B-H8/B-M10/B-M17` 等）。
- 明确写入本轮决策：XOR 取舍、单租户、破坏性同步仅日志、匿名反射、CI 未变。
- 顺带修正已过期状态（`01 H1` 已被阶段 4 覆盖、`02 M11/M14` 已修、`06 M12` 已修、`07 M5` 已删、`04 A-M4` 已修、`08 M13` 待办）。
- 不在本阶段做：全量重写 `08` 的 JSONB `Stored as` 注释（列为后续项，除非 A6 迁移顺带补上 `openlineage_api_key` 的注释）。

---

## 五、执行顺序与理由

1. **A 批先做**：安全项相互独立、改动局部，且是"对外部署前"风险最高的一批。A2（CORS/CSRF）与 A5（审计）会触及 server/auth 装配，放在 A1/A3/A4 之后可复用同一套测试。
2. **B 批随后**：正确性项多集中在 store/runner，B4（事务规范化）作为基础设施先行，B1/B2/B3 直接修静默错误。
3. **C 批最后**：性能项依赖 B 批的 store 改动（如 B5/B6 的查询形状），放后面避免重复改同一段 SQL。
4. **D 批贯穿**：每完成一个 commit 就跑对应单测；每批结束跑一次全量 hermetic + 集成，最后一个 commit 做完整 D2/D3。
5. 每个步骤独立 commit（conventional commit），提交信息引用报告 ID（如 `fix(store): compare the engine filter against the stored enum name (S-H5)`）。

---

## 六、本轮不做（已明确排除）

- 测试/CI 批次：CI release job、集成套件入 CI、前端覆盖率 `@vitest/coverage-v8`、`09` 低优先测试项（`waitForServerReady` 5xx、`SELECT 1;` 空断言、fixture DDL 重复、风格/`t.Parallel`）、handler 测试覆盖缺口——**除 D1 的失败测试外**不动。
- 死代码与低优先清理：`resource_name.go` 零引用常量、`AgentConfig.Hooks`/`MaxTurns`/`AgentEvent.Done`、`guid.GetInstaceFromGUID` 拼写、`log.Stack` eager 采集、前端内嵌、`goMigrations` 空注册表、`ExplainSQL` 未用字段等。
- 文档全量同步（仅做 D3 规定的范围）。
- 产品决策项：per-resource IAM 策略、`disallow_signup` 默认值、字段加密方案变更、`openlineage_run` 保留默认。
- 破坏性 schema 同步硬拦截（保持仅日志）。
- gRPC 反射策略变更（保持匿名）。
