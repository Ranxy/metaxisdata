# 10 · 遗留债务清单与重构路线图

本文件把散落在各模块报告中的"遗留/死代码"集中成一份可执行清单，并给出分阶段整改路线。所有条目都可在对应模块报告里找到证据与行号。

---

## 一、遗留功能债务（按"产品已无此功能但代码还在"归类）

### 1. Bytebase 时代的组织/权限模型
- **store**：`store/role.go` 整文件无调用者；`store/project.go` 整个 store API 无调用者，且 `DeleteProject` 引用 15 张不存在的表（`query_history`/`worksheet`/`issue*`/`plan*`/`pipeline`/`task*`/`sheet`/`release`/`changelist`/`db_group`/`project_webhook`）。
- **proto**：`store.Policy`/`TagPolicy` 零使用；`TagPolicy.tags` 引用不存在的 `reviewConfigs`；`policy` 表支持 `WORKSPACE/ENVIRONMENT/PROJECT` 但无 API；`store.RolePermissions` 无 RoleService；`GroupPayload`/`GroupMember` 无 GroupService。
- **API**：~~`common.AuthContext.Permission`~~（**阶段 0 已修复**：`ACLInterceptor` 消费它，proto 写方法已声明 `permission`）/`AuthMethod`/`Resources`、`HasWorkspaceResource`、`GetProjectResources` 仍无消费者；`utils/member.go` 的 IAM 组合逻辑只通过彼此可达。
- **设置**：`WORKSPACE_APPROVAL`、`WORKSPACE_EXTERNAL_APPROVAL`、`APP_IM`、`WATERMARK`、`AI`、`SCHEMA_TEMPLATE`、`DATA_CLASSIFICATION`、`SEMANTIC_TYPES`、`SCIM` 全部未实现。
- **错误码**：`common.Code` 的 301-410（task/sql type）与 201-206（migration）大多未用。

### 2. Issue / Task / Approval / Plan
- `store/stats.go:126` `CountIssues` 查询不存在的 `issue` 表；`CountProjects`/`CountActiveUsers`/`CountInstance`/`CountInstanceGroupByEngineAndEnvironmentID` 无调用者；`id > 101` 魔法偏移。
- `store/project.proto` 的 `issue_labels`、注释提到 issue 的 `postgres_database_tenant_mode`；`DeleteInstanceRequest.force` 文档提到 "open issues"。
- `backend/common/cel.go` 的 `ConvertUnparsedRisk`/`ConvertUnparsedApproval`、`RiskFactors`/`ApprovalFactors`、`cel_attributes.go` 全部 20 个常量（含显式标注 deprecated 的 approval scope）。
- `common/const.go` 的 `ServiceAccountAccessKeyPrefix`、`SystemBotID`、`PrincipalIDForFirstUser`。

### 3. 多引擎 / 多云表面（产品只支持 MySQL/TiDB/PG）
- `proto` 的 `Engine` 28 个值；`DataSource` 带 MongoDB/Oracle/Redis Sentinel/Databricks/CockroachDB/Spanner/Hive、SSH 隧道、四类 IAM 凭据、`SASLConfig`/`KerberosConfig`、`DataSourceExternalSecret`。
- `api/v1/common.go` 的 `convertToEngine`/`convertEngine` 各 27 个 case；`instance_service.go` 的 `convertRedisType` 把 `REDIS_TYPE_UNSPECIFIED` 映射成 `STANDALONE`。
- `store/database.proto` 的 `backup_available`（备份功能）；`store/database.proto` 的 `InstanceRoleMetadata`。

### 4. SCIM / 2FA / Entra ID / 服务账号
- `User.service_key`、`User.recovery_codes`、`User.phone`、`UserProfile.source`（"Entra ID SCIM sync"）、`WorkspaceProfileSetting.require_2fa`/`maximum_role_expiration`。
- `store/idp.proto` 的 OIDC/LDAP/SCIM source、`FieldMapping` 注释指向不存在的 `principal.idp_user_info` 列；`principal.mfa_config` 注释指向不存在的 `MFAConfig` message。
- `auth_service.go` 的 SSO 分组同步、`service account` 登录分支。

### 5. 无意义的 `V2` 命名
- `GetSettingV2`/`UpsertSettingV2`/`CreateSettingIfNotExistV2`/`ListSettingV2`、`GetInstanceV2`/`ListInstancesV2`/`UpdateInstanceV2`/`CreateInstanceV2`、`GetDatabaseV2`/`ListDatabasesV2`、`GetPolicyV2`/`CreatePolicyV2`/`UpdatePolicyV2`/`DeletePolicyV2`/`ListPoliciesV2`、`StoreMetaResourceV2` —— 均不存在对应的 V1 版本，后缀已无信息量。

### 6. 未接线/半成品
- 前端未内嵌（`server_frontend_not_embed.go` + `embed_frontend` tag 无实现文件）。
- `ListInstanceDatabase` 是空 stub；`DatabaseService.GetDatabase` 返回 `Unimplemented`。
- `migrator` 的 `goMigrations` 空注册表；`migration/0.1/` 增量目录缺失。
- `metric` 包与 `plugin/metric` 无 reporter 实现。
- CLI flag `dataDir`/`ha`/`saas`/`demo`/`memoryProfileThreshold` 未注册（~~`externalURL`~~ **阶段 1 已注册并接线**，`7fdcead`）；~~`--enable-json-logging` 空实现；`--debug` 对日志无效~~（**阶段 1 已修**，`7fdcead`：`setupLogging` + `slog.SetDefault`，Text/JSON 可选、`LogLevel` 动态、`Replace` 裁剪 source）。
- ~~`config.Profile.Secret` 从未赋值~~（**阶段 0 已接线**：`getBaseProfile` 用 `os.Getenv("JWT_SECRET")` 赋值）；`LastActiveTS` 只写不读。
- `common.ServiceDataKey` 从不写入，`getServiceData` 恒返回 nil。
- `explain_sql` 的 `ExplainSQLMetadata.expired`/`ExplainSQLResponse.error`/`sections_json`/`ExplainSQLRequest.meta_type` 未使用或未设置。
- `component/llm` 的 `AgentConfig.Hooks`/`MaxTurns`/`AgentEvent.Done` 从未设置/读取。

---

## 二、死代码清单（可安全删除，需先跑测试）

| 位置 | 内容 |
| --- | --- |
| `backend/store/role.go` | 整个文件（6 个方法 + `rolesCache`） |
| `backend/store/project.go` | 整个 store API（`Get/List/Create/Update/BatchUpdate/DeleteProject`） |
| `backend/store/stats.go` | `CountIssues`、`CountProjects`、`CountActiveUsers`、`CountInstance`、`CountInstanceGroupByEngineAndEnvironmentID` |
| `backend/store/column_lineage.go:241` | `DeleteColumnLineageByMeta` |
| `backend/store/explain_sql.go:24,45` | `QueryColumnLineageSources`/`Targets` |
| `backend/store/environment.go:24` | `CheckDatabaseUseEnvironment` |
| `backend/store/openlineage_run.go:403` | `MarshalOpenLineageRunPayload` |
| `backend/store/common.go` | `RowStatus`/`Normal`/`Archived`/`SortOrder`/`ASC`/`DESC`/`OrderByKey` |
| `backend/store/group.go` | `CreateGroup`/`DeleteGroup` |
| `backend/store/policy.go:192-318` | `UpdatePolicyV2`/`DeletePolicyV2`/`ListPoliciesV2`（且已损坏） |
| `backend/store/meta_resource.go:355-366,430-441` | `withMetadata=false` 分支 |
| `backend/common/cel.go` | 8 个 helper + 6 个 vars（约 230/300 行） |
| `backend/common/cel_attributes.go` | 全部 20 个常量 |
| `backend/common/error.go` | `ErrorCode`、`Wrap`、`Wrapf`、`Code.Int/Int32` |
| `backend/common/resource_name.go` | 约 24 个未用符号 |
| `backend/common/guid.go` | `GetDatabaseFromGUID` |
| `backend/common/context.go` | `HasWorkspaceResource`、`GetProjectResources` |
| `backend/api/auth/auth.go:198` | `GetTokenFromMetadata` + 两个 gateway metadata 常量 |
| `backend/api/v1/common.go:49-185,362-390` | 旧 `ParseFilter` 系列 + 注释掉的 export format 转换 |
| `backend/api/v1/common.go:429-467` | ~~`getSubConditionFromExpr`/`getVariableAndValueFromExpr`（注意其类型断言会 panic）~~ —— **阶段 1 已重写**（`ff914ac`）：`getVariableAndValueFromExpr` 现在返回 error 且类型断言全部经 helper 检查，两个函数都在活跃路径上，不再是死代码候选 |
| `backend/utils/collection.go` | `Map` |
| `backend/utils/member.go:26-68` | 注释掉的 `GetUsersByRoleInIAMPolicy` |
| `backend/metric/` + `backend/plugin/metric/` | 整个遥测栈 |
| `backend/test/integration/env/testenv.go:67` | `SetupMySQLEnv` 及其 helper |
| `backend/test/integration/env/service_env.go:160,179` | `Setup*ServiceEnv` |
| `backend/test/integration/runner/main_test.go:108,134` | `shared*ServiceEnv` |
| `backend/store/db_connection.go:16,22` | `stopWatcher` + 冗余 pgx 导入 |
| `backend/store/manual_sql.go:384-387` | 仅测试使用的 query builder |

---

## 三、重构建议（按收益排序）

1. **统一错误处理边界**：实现一个 Connect 拦截器把 `common.ErrorCode(err)` 映射为状态码，并给 `common.Error` 加 `Unwrap()`；否则要么让 `common.Code` 真正生效，要么删掉它。当前 500/`Unknown` 遍地。
2. **消除 CEL→SQL 的字符串拼接**：把 4 个 handler 的 filter 翻译器合并为**一个**结构化、参数化的实现（`ListResourceFilter` 只允许占位符），并加表驱动 guard 测试。这同时修掉 SQL 注入与 4 份重复代码。
3. **收口凭证加密**：`common.Obfuscate` 换成 AES-GCM + 环境密钥；`store.GetSecret` 用 `sync.Once`；`Profile.Secret` 接线或删除。
4. **统一分页**：抽出一个 `parsePageSize/nextPageToken` 实现，修复 token limit 被忽略、OpenLineage offset、LLM profile 无 token、sublevel 无 offset、`SearchMetadata` 无请求 token。
5. **拆分超长文件**：`instance_service.go`(1399)、`database_service.go`(1206)、`database_history.go`(1197) 按资源/职责拆分；`meta_resource.go`(1056)、`manual_sql.go`(775) 同样。
6. **store 事务规范化**：所有 `BeginTx` 后紧跟 `defer tx.Rollback()`；单语句查询不再开事务；提供 `tx` 参数版本供 runner 复用同一事务。
7. **删除死代码**（第二节），并在 CI 加 `go test ./...` + `-race` + `golangci-lint`。
8. **收敛 proto 表面**：删除未实现的 setting/enum/message，修正 `metaxisdata/DatabaseMetadata`、`ListUsers` method_signature、`meta_type` 裸 int32、v1/store 重复枚举。

---

## 四、分阶段整改路线图

### 阶段 0：安全止血（必须最先做）——**已完成**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 1 | JWT 签名密钥改为环境注入并 fail-closed；作废历史 token | ✅ | `adfec91` `84b16db` |
| 2 | 恢复授权层：至少给用户/实例/数据源/OpenLineage key 的写操作加管理员校验 | ◐ | `ec49607` `0f2165e` |
| 3 | 关闭未认证注册或强制 `DisallowSignup`；首个管理员授予改原子 | ✅ | `5b19778` `c4e22fc` |
| 4 | 修 SQL 注入（user/instance/database filter + principal/group project ID） | ✅ | `3321801` |
| 5 | 审计脱敏补 `key`/`content`/`sslKey`/`keytab`，并为 `CreateAPIKeyResponse` 加测试 | ✅ | `89ef84a` |
| 6 | panic 不再回传堆栈；`validate_only` 加权限 + 内网地址限制 | ◐ | `5446a10` `ec49607` |

- **1 的收尾说明（非剩余缺陷）**：`JWT_SECRET` 环境变量优先、缺失时回退 DB `AUTH_SECRET`、`< 32` 字符启动失败、解析侧三项校验（历史 token 失效）均已完成；`profile_release.go` 的 import 修正后（`84b16db`）`-tags release` 可编译并通过 `go vet -tags release ./...`。但 `Makefile`/CI/Docker 未使用该 tag，默认产物仍是 dev 模式——部署 prod 必须显式 `-tags release`，后续应把它固化到构建目标或改为运行时配置。
- **2 的剩余项**：写操作已全部要求 workspaceAdmin；读路径（Get/List/血缘/OpenLineage run/raw_payload）仍未收紧；尚未实现 permission→role 的细粒度映射（当前语义是"注解非空 ⇒ 管理员"）。
- **6 的剩余项**：**内网地址限制经确认后主动放弃**——自托管产品的核心用法就是让用户连接内网数据库，加私网 deny 会破坏功能，因此只保留管理员权限约束；CEL 类型断言 panic 本身仍未修（只是不再泄露堆栈）。
- 配置侧顺带完成：`config.Profile.Secret` 现在由 `JWT_SECRET` 赋值（不再是"从未赋值"）；`disallow_signup` 默认仍为 `false`，需管理员在新增的 `/settings/general` 页面显式打开。

### 阶段 1：正确性与可运维性——**本轮已完成（5 个任务 5 个 commit）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 7 | 补 `migration/0.1/` 增量 + guard 测试；修 `db_schema` 过滤 | ◐ | `bb93ee0` |
| 8 | 修 schemasync 两个生命周期 bug 与破坏性 diff；`LastSyncTime` 进事务 | ◐ | `fcb6a98` |
| 9 | 修 CEL 类型断言 panic；统一 `InvalidArgument` | ✅ | `ff914ac` |
| 10 | 接线日志系统（`slog.SetDefault` + LogLevel/Replace）；注册 `--external-url`；把 `-tags release` 固化到构建目标 | ✅ | `7fdcead` |
| 11 | `UpdateInstance(data_sources)` 改为按 ID 合并；`UpdateDatabase` 判空 | ✅ | `20e284b` |

- **7 的收尾说明**：`db_schema` 经确认是不需要的遗留代码（`db.metadata` 里的 `DatabaseMetadata` 根本没有 `schemas` 字段，schema 树在 `meta_registry_resource`），因此**删除**而非改写：API 的 `table` 过滤器分支、store 的 join 推断、proto 里的过滤文档一并移除，`table` 现在返回 `InvalidArgument`。**`migration/0.1/` 增量目录经确认不创建**——当前没有待发布的 schema 变更，空目录只是噪音；残留的是"改 `LATEST.sql` 时必须同时补增量"这一流程约束（见 `06` R-H5）。
- **8 的收尾说明**：checker 不再因一次瞬时错误退出 ✅；停用实例不再被排队（`shouldSyncNow` 显式拒绝 `interval == 0`）✅；`LastSyncTime` 改为提交成功后再写、血缘排队提前 ✅；**破坏性删除按产品决策只加日志**（`logSchemaSyncDeletion` + 实例级软删 Warn），不拦截、不设阈值，因此"权限被收窄导致快照变空/变少"仍可能清空注册表，但一定留痕（见 `06` R-H3）。
- **10 的收尾说明**：日志输出改为 `os.Stdout`（原 `slog.Default` 写 stderr），`AddSource` 打开后由 `log.Replace` 裁剪为 `dir/file.go`；`make build-release` 带 `-tags release`，`make build` 与 AGENTS.md 的开发构建**刻意保持 dev**，CI/Docker 仍不存在，部署方需显式用 `build-release`。
- **11 的收尾说明**：合并语义是"请求列表决定成员、每个同名 ID 的条目按字段叠加"；proto3 无法区分"未发送"与"空串"，所以**无法通过本接口清空某个非密钥字段**，需要清空时应删除后用 `AddDataSource` 重建。顺带在 API 层补上"恰好一个 ADMIN"校验（原 `03` 低节条目）。

### 阶段 2：性能与资源（未开始）
12. `GetUserByID/Email` 改定向查询；决定 `enableCache` 的去留。
13. OpenLineage 读路径加 LIMIT/聚合/请求内解析缓存；ExplainSQL 缓存 key 加 scope + TTL。
14. 补 `meta_registry_resource(object_type)`、`metadata` GIN、`principal(email)` 唯一索引。
15. LLM agent：ctx-aware 发送、真流式、错误传播、禁止缓存截断结果。
16. 连接池 lifetime/idle 配置；runner 关停超时。

### 阶段 3：清理与重构（未开始）
17. 删除第二节的死代码与遗留 proto/枚举。
18. 合并 CEL 翻译器、拆分超长文件、统一分页与错误映射。
19. CI：`go test -race ./...` + lint + 前端测试 + migrator 集成测试入列。
20. 修正 proto 契约问题（P-H1..P-H6、M 系列）并按 breaking-change 流程发布。

---

## 五、验证方式

- 本报告结论来自源码通读 + `go build ./...`、`go vet ./...`、`go test ./...`（审查时均 exit 0）。
- `golangci-lint` 在审查环境无法运行（cache/加载问题，非仓库缺陷），当时 lint 清洁度未验证；**阶段 0 修复后已可运行并输出 `0 issues.`**。
- 多数安全结论（SQL 注入、JWT 伪造、SSRF、缓存串租户、panic 路径）为代码级论证，**建议在修复前先补最小复现测试**（尤其是 SQL 注入与 ExplainSQL 缓存碰撞）。
- 阶段 0 已补的回归测试：`backend/api/v1/filter_injection_test.go`（注入载荷 + LIKE 转义）、`backend/api/v1/audit_test.go` 扩展（`key`/`sslKey`/`cert` 等脱敏）。
- 阶段 0 复测：`gofmt -l` 空、`go build ./...`、`go vet ./...`、`go test ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`go vet -tags integration ./...`（仅编译）与 dev/prod 两种二进制构建全部通过；prod profile 需 `go build -tags release`（`84b16db` 修正 import 后才可用）。
- 阶段 1 复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`、`go test ./...`（含新增 8 个 guard 测试）、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release`（`-tags release` 二进制构建）、`go vet -tags release ./...` 全部通过；另实测 `--enable-json-logging` 输出 JSON 行、`source` 路径已裁剪、`--external-url` 出现在 `--help` 中。集成测试仍未运行（需 Docker），前端未改动故未重跑前端检查。
- 阶段 1 关闭的"待确认"：① `driver.SyncDBSchema` 确实会在不报错的情况下返回空/部分 schema（MySQL 的 `information_schema` 按权限过滤行），这是 R-H3 只加日志的依据；② `table` 过滤器前端未使用，且 `db.metadata` 无 `schemas` 字段、`db_schema` 表从未存在，故整体删除而非改写。仍待确认：部署拓扑（单租户？）、`enableCache` 是否有意关闭、`RETURNING` 顺序、部分 proto 字段是否为有意保留。
