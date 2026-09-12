# 10 · 遗留债务清单与重构路线图

本文件把散落在各模块报告中的"遗留/死代码"集中成一份可执行清单，并给出分阶段整改路线。所有条目都可在对应模块报告里找到证据与行号。

---

## 一、遗留功能债务（按"产品已无此功能但代码还在"归类）

### 1. Bytebase 时代的组织/权限模型
- **store**：`store/role.go` 整文件无调用者；`store/project.go` 整个 store API 无调用者，且 `DeleteProject` 引用 15 张不存在的表（`query_history`/`worksheet`/`issue*`/`plan*`/`pipeline`/`task*`/`sheet`/`release`/`changelist`/`db_group`/`project_webhook`）。
- **proto**：`store.Policy`/`TagPolicy` 零使用；`TagPolicy.tags` 引用不存在的 `reviewConfigs`；`policy` 表支持 `WORKSPACE/ENVIRONMENT/PROJECT` 但无 API；`store.RolePermissions` 无 RoleService；`GroupPayload`/`GroupMember` 无 GroupService。
- **API**：`common.AuthContext.Permission`/`AuthMethod`/`Resources`、`HasWorkspaceResource`、`GetProjectResources` 无消费者；`utils/member.go` 的 IAM 组合逻辑只通过彼此可达。
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
- CLI flag `externalURL`/`dataDir`/`ha`/`saas`/`demo`/`memoryProfileThreshold` 未注册；`--enable-json-logging` 空实现；`--debug` 对日志无效。
- `config.Profile.Secret` 从未赋值；`LastActiveTS` 只写不读。
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
| `backend/api/v1/common.go:429-467` | `getSubConditionFromExpr`/`getVariableAndValueFromExpr`（注意其类型断言会 panic） |
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

### 阶段 0：安全止血（必须最先做）
1. JWT 签名密钥改为环境注入并 fail-closed；作废历史 token。
2. 恢复授权层：至少给用户/实例/数据源/OpenLineage key 的写操作加管理员校验。
3. 关闭未认证注册或强制 `DisallowSignup`；首个管理员授予改原子。
4. 修 SQL 注入（user/instance/database filter + principal/group project ID）。
5. 审计脱敏补 `key`/`content`/`sslKey`/`keytab`，并为 `CreateAPIKeyResponse` 加测试。
6. panic 不再回传堆栈；`validate_only` 加权限 + 内网地址限制。

### 阶段 1：正确性与可运维性
7. 补 `migration/0.1/` 增量 + guard 测试；修 `db_schema` 过滤。
8. 修 schemasync 两个生命周期 bug 与破坏性 diff；`LastSyncTime` 进事务。
9. 修 CEL 类型断言 panic；统一 `InvalidArgument`。
10. 接线日志系统（`slog.SetDefault` + LogLevel/Replace）；注册 `--external-url`。
11. `UpdateInstance(data_sources)` 改为按 ID 合并；`UpdateDatabase` 判空。

### 阶段 2：性能与资源
12. `GetUserByID/Email` 改定向查询；决定 `enableCache` 的去留。
13. OpenLineage 读路径加 LIMIT/聚合/请求内解析缓存；ExplainSQL 缓存 key 加 scope + TTL。
14. 补 `meta_registry_resource(object_type)`、`metadata` GIN、`principal(email)` 唯一索引。
15. LLM agent：ctx-aware 发送、真流式、错误传播、禁止缓存截断结果。
16. 连接池 lifetime/idle 配置；runner 关停超时。

### 阶段 3：清理与重构
17. 删除第二节的死代码与遗留 proto/枚举。
18. 合并 CEL 翻译器、拆分超长文件、统一分页与错误映射。
19. CI：`go test -race ./...` + lint + 前端测试 + migrator 集成测试入列。
20. 修正 proto 契约问题（P-H1..P-H6、M 系列）并按 breaking-change 流程发布。

---

## 五、验证方式

- 本报告结论来自源码通读 + `go build ./...`、`go vet ./...`、`go test ./...`（均 exit 0）。
- `golangci-lint` 在本次环境无法运行（cache/加载问题，非仓库缺陷），lint 清洁度未验证。
- 多数安全结论（SQL 注入、JWT 伪造、SSRF、缓存串租户、panic 路径）为代码级论证，**建议在修复前先补最小复现测试**（尤其是 SQL 注入与 ExplainSQL 缓存碰撞）。
- 部分结论标注了"待确认"，需要与作者或通过集成测试确认（见各模块报告末尾）。
