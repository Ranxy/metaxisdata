# 10 · 遗留债务清单与重构路线图

本文件把散落在各模块报告中的"遗留/死代码"集中成一份可执行清单，并给出分阶段整改路线。所有条目都可在对应模块报告里找到证据与行号。

---

## 一、遗留功能债务（按"产品已无此功能但代码还在"归类）

### 1. Bytebase 时代的组织/权限模型
> **阶段 3 部分删除**（`a39bc41`）：`store/role.go`、`store/project.go`（含引用 15 张不存在表的 `DeleteProject`）与其 LRU 缓存已删除；`store/policy.go` 只保留工作区 IAM 路径。`project`/`role` 两张表现在没有任何 Go 调用者，但删表未做。
> **阶段 3 收尾**（`722d3cb`）：`TagPolicy`/`EnvironmentTierPolicy`/`RolePermissions`/`Project`/`Label` 及其 proto 文件已删除；**`store.Policy` 保留**——它的 `Type`/`Resource` enum 仍被 `store/store.go`/`store/group.go`/`store/policy.go` 用来写 `policy.resource_type`/`type` 列（原报告"零 Go 使用"漏掉了 enum 常量）。`project`/`role`/`policy` 表与 `db.project` 列保留（删表/删列属 schema 变更）。
> **阶段 3 续**（`904fb09` `451cb78`）：`role` 表、`project` 表与 `db.project` 列已 DROP（增量 `0.1.0003`/`0.1.0004`）；`policy` 表保留（WORKSPACE/IAM 行是活路径），其 PROJECT 行已无生产者或消费者。
- **store**：~~`store/role.go` 整文件无调用者；`store/project.go` 整个 store API 无调用者，且 `DeleteProject` 引用 15 张不存在的表（`query_history`/`worksheet`/`issue*`/`plan*`/`pipeline`/`task*`/`sheet`/`release`/`changelist`/`db_group`/`project_webhook`）。~~
- **proto**：~~`store.Policy`/`TagPolicy` 零使用；`TagPolicy.tags` 引用不存在的 `reviewConfigs`；`policy` 表支持 `WORKSPACE/ENVIRONMENT/PROJECT` 但无 API；`store.RolePermissions` 无 RoleService；~~`GroupPayload`/`GroupMember` 无 GroupService。**阶段 3 收尾**：`TagPolicy`/`RolePermissions` 删除；`Policy` 的两个 enum 保留（见上）；`GroupPayload`/`GroupMember` 保留（SSO 分组同步在用，仍无 GroupService）。
- **API**：~~`common.AuthContext.Permission`~~（**阶段 0 已修复**：`ACLInterceptor` 消费它，proto 写方法已声明 `permission`）/`AuthMethod`/`Resources`、`HasWorkspaceResource`、`GetProjectResources` 仍无消费者；`utils/member.go` 的 IAM 组合逻辑只通过彼此可达。
- **设置**：`WORKSPACE_APPROVAL`、`WORKSPACE_EXTERNAL_APPROVAL`、`APP_IM`、`WATERMARK`、`AI`、`SCHEMA_TEMPLATE`、`DATA_CLASSIFICATION`、`SEMANTIC_TYPES`、`SCIM` 全部未实现。
  - **阶段 3 收尾**：这 9 个 `SettingName` 值已 `reserved`（`e0eab33`）。
- **错误码**：`common.Code` 的 301-410（task/sql type）与 201-206（migration）大多未用。
  - **阶段 3**：这些错误码族已随死代码删除（`e42b9ac`），只剩 `Ok/Internal/NotAuthorized/Invalid/NotFound/Conflict/NotImplemented/SizeExceeded/DBExecutionError`。

### 2. Issue / Task / Approval / Plan
- `store/stats.go:126` `CountIssues` 查询不存在的 `issue` 表；`CountProjects`/`CountActiveUsers`/`CountInstance`/`CountInstanceGroupByEngineAndEnvironmentID` 无调用者；`id > 101` 魔法偏移。
  - **阶段 3**：五个统计方法（含 issue/project 查询）已随 `stats.go` 清理删除（`a39bc41`）。
- `store/project.proto` 的 `issue_labels`、注释提到 issue 的 `postgres_database_tenant_mode`；`DeleteInstanceRequest.force` 文档提到 "open issues"。
  - **阶段 3 收尾**：`store/project.proto` 整文件与 `issue_labels` 删除（`722d3cb`）；`DeleteInstanceRequest.force` 注释的修正**已写好但回滚**（需要重新生成 buf 产物，遇到 BSR 远程插件限流，见第五节），应在下一轮随任意 proto 变更一起提交。
- `backend/common/cel.go` 的 `ConvertUnparsedRisk`/`ConvertUnparsedApproval`、`RiskFactors`/`ApprovalFactors`、`cel_attributes.go` 全部 20 个常量（含显式标注 deprecated 的 approval scope）。
- `common/const.go` 的 `ServiceAccountAccessKeyPrefix`、`SystemBotID`、`PrincipalIDForFirstUser`。
  - **阶段 3 更正**：`ServiceAccountAccessKeyPrefix` 与 `SystemBotID` 仍是活引用（服务账号 key 前缀 / 系统 bot principal），**不能删**；`PrincipalIDForFirstUser` 已删。

### 3. 多引擎 / 多云表面（产品只支持 MySQL/TiDB/PG）
> **阶段 3 收尾已收敛**（`ceb6a3d`）：`Engine` 28→5（`MYSQL`/`POSTGRES`/`TIDB`/`MARIADB`/`OCEANBASE`，后两者由 MySQL 驱动承载），其余编号与名字 `reserved`；`DataSource` 的 MongoDB/Oracle/Redis sentinel/Databricks/CockroachDB 字段、四类 IAM 凭据、`SASLConfig`/`KerberosConfig`、`DataSourceExternalSecret` 全部删除；`convertToEngine`/`convertEngine` 各删 22 个 case，`convertRedisType` 等 8 个转换函数删除。**SSH 隧道保留**（MySQL/PG 驱动在用）。`api/v1/common.go` 的两个 27-case 转换函数已是过去式。
- `proto` 的 `Engine` 28 个值；`DataSource` 带 MongoDB/Oracle/Redis Sentinel/Databricks/CockroachDB/Spanner/Hive、SSH 隧道、四类 IAM 凭据、`SASLConfig`/`KerberosConfig`、`DataSourceExternalSecret`。
- `api/v1/common.go` 的 `convertToEngine`/`convertEngine` 各 27 个 case；`instance_service.go` 的 `convertRedisType` 把 `REDIS_TYPE_UNSPECIFIED` 映射成 `STANDALONE`。
- `store/database.proto` 的 `backup_available`（备份功能）；`store/database.proto` 的 `InstanceRoleMetadata`。
  - **阶段 3 收尾**：`backup_available` 与 `InstanceRoleMetadata` 已删除（`ddff264`）；`store/database.proto` 的 `DatabaseSchemaMetadata.service_name`（Oracle）与 `IndexMetadata.granularity`（ClickHouse 注释）仍在，属未清字段。
  - **阶段 3 续**：这两个字段已删除（`ee3c39b`），v1 与 store 两侧编号及名字 `reserved`。
  - **阶段 3 补遗**：按引擎注册的四个表（`plugin/db/mysql`、`plugin/schema/mysql` 的 DDL/迁移、`plugin/lineage/mysql`、OpenLineage resolver 的 `isMySQLLike`）已统一覆盖 MYSQL/TIDB/MARIADB/OCEANBASE（`729db71`）；此前 driver 无 `TIDB` 条目，MariaDB/TiDB 的 schema DDL 与 MariaDB/OceanBase 的 lineage 都缺失。

### 4. SCIM / 2FA / Entra ID / 服务账号
> **阶段 3 收尾部分收敛**（`e0eab33`）：`User.recovery_codes`、`UserProfile.source`、`GroupPayload.source`、`WorkspaceProfileSetting.require_2fa`/`maximum_role_expiration` 已删除；IDP 只留 OAuth2（OIDC/LDAP 配置与枚举值 reserved，无调用者的 store IDP 写路径删除）。**保留（活路径）**：`User.service_key`（`CreateUser` 的服务账号分支返回生成的 access key）、`User.phone`（有 E.164 校验）、`PrincipalType.SERVICE_ACCOUNT`（登录分支在用）。**仍未清**：`store/idp.proto` 的 `FieldMapping.phone`/`groups` 随 OAuth2 保留（oauth2 插件在用）。
- **阶段 3 续**：`principal.mfa_config` 列已 DROP，`idp.type` CHECK 收窄为 `('OAUTH2')`（`904fb09`，增量 `0.1.0003`）；v1 `UserType.USER` 改名 `END_USER` 与 store/DB 对齐（`4036e1e`）。
- `User.service_key`、`User.recovery_codes`、`User.phone`、`UserProfile.source`（"Entra ID SCIM sync"）、`WorkspaceProfileSetting.require_2fa`/`maximum_role_expiration`。
- `store/idp.proto` 的 OIDC/LDAP/SCIM source、`FieldMapping` 注释指向不存在的 `principal.idp_user_info` 列；`principal.mfa_config` 注释指向不存在的 `MFAConfig` message。
- `auth_service.go` 的 SSO 分组同步、`service account` 登录分支。
  - **阶段 3 收尾更正**：`service account` 登录分支**不是**遗留——`getOrCreateUserWithIDP` 之外的 `case storepb.PrincipalType_SERVICE_ACCOUNT` 会给服务账号签发 API token，且 `CreateUser` 允许创建 SERVICE_ACCOUNT，属活路径。

### 5. 无意义的 `V2` 命名
> **阶段 3 部分删除**（`a39bc41`）：`GetPolicyV2`/`CreatePolicyV2`/`UpdatePolicyV2`/`DeletePolicyV2`/`ListPoliciesV2` 随死代码一起删除（只留 `GetPolicyV2` 供 IAM 使用）。
> **阶段 3 续已完成**（`8b328ae`）：剩下的 12 个 `*V2` 方法（`GetSettingV2`/`UpsertSettingV2`/`CreateSettingIfNotExistV2`/`ListSettingV2`/`DeleteSettingV2`/`GetInstanceV2`/`ListInstancesV2`/`CreateInstanceV2`/`UpdateInstanceV2`/`GetDatabaseV2`/`GetPolicyV2`）及其 impl helper 全部去掉后缀——它们都没有对应的 V1 版本，后缀已无信息量。
- `GetSettingV2`/`UpsertSettingV2`/`CreateSettingIfNotExistV2`/`ListSettingV2`、`GetInstanceV2`/`ListInstancesV2`/`UpdateInstanceV2`/`CreateInstanceV2`、`GetDatabaseV2`/`ListDatabasesV2`、`GetPolicyV2`/`CreatePolicyV2`/`UpdatePolicyV2`/`DeletePolicyV2`/`ListPoliciesV2`、`StoreMetaResourceV2` —— 均不存在对应的 V1 版本，后缀已无信息量。

### 6. 未接线/半成品
> **阶段 3 已修**：`ListInstanceDatabase` 空 stub 与 `DatabaseService.GetDatabase`（恒 `Unimplemented`）两个 RPC 已删除（`73901a1`）；`ServiceDataKey`/`getServiceData`、`common.const.go` 的三个死常量、`dataDir`/`ha`/`saas`/`demo`/`memoryProfileThreshold` flag、`Profile.LastActiveTS`、`ultimate.go` 的 `!minidemo` 与 `server_frontend_not_embed.go` 的 `!embed_frontend` 约束（`-tags embed_frontend` 曾直接编译失败）均已清理（`a39bc41`–`3cc4926`）。**阶段 7 收口**：其中 4 项本轮已完成——前端内嵌（`f3a0394`）、`migrator` 的 `goMigrations` 空注册表（`4ea38d2`）、`ExplainSQL` 的未用字段（`bf70f70`）、`AgentConfig.Hooks`（`70e7c20`）。
- 前端未内嵌（`server_frontend_not_embed.go` + `embed_frontend` tag 无实现文件）。 —— **✅ 已实现（阶段 7）** · `f3a0394`：`make build-embed` 用 `-tags "release embed_frontend"` 内嵌 `frontend/dist`，SPA 路由回落 `index.html`；默认构建仍是"前端单独托管"。
- `ListInstanceDatabase` 是空 stub；`DatabaseService.GetDatabase` 返回 `Unimplemented`。
- `migrator` 的 `goMigrations` 空注册表；~~`migration/0.1/` 增量目录缺失~~（**阶段 2 已建立**，`8c34542` `ff9b22a`）。
- `metric` 包与 `plugin/metric` 无 reporter 实现。
- CLI flag `dataDir`/`ha`/`saas`/`demo`/`memoryProfileThreshold` 未注册（~~`externalURL`~~ **阶段 1 已注册并接线**，`7fdcead`）；~~`--enable-json-logging` 空实现；`--debug` 对日志无效~~（**阶段 1 已修**，`7fdcead`：`setupLogging` + `slog.SetDefault`，Text/JSON 可选、`LogLevel` 动态、`Replace` 裁剪 source）。
- ~~`config.Profile.Secret` 从未赋值~~（**阶段 0 已接线**：`getBaseProfile` 用 `os.Getenv("JWT_SECRET")` 赋值；**阶段 5 改为启动时从数据库 `AUTH_SECRET` 解析，`JWT_SECRET` 已删除**，`7870016`）；`LastActiveTS` 只写不读。
- `common.ServiceDataKey` 从不写入，`getServiceData` 恒返回 nil。
- `explain_sql` 的 `ExplainSQLMetadata.expired`/`ExplainSQLResponse.error`/`sections_json`/`ExplainSQLRequest.meta_type` 未使用或未设置。
- `component/llm` 的 `AgentConfig.Hooks`/`MaxTurns`/`AgentEvent.Done` 从未设置/读取。

---

## 二、死代码清单（可安全删除，需先跑测试）

> **阶段 3 已按本表删除**（`a39bc41` `e42b9ac` `40ec3a4` `b9a48a3` `fedcc12` `3cc4926`）：除下列例外，本表条目全部删除。例外（逐个确认存活调用者后保留）：`store/policy.go` 的工作区 IAM 路径与 `store/group.go` 的读路径（`utils/member.go`/SSO 在使用）、`utils/member.go` 的 `GetUserFormattedRolesMap` 链路、`common/error.go` 的 `ErrorCode`（阶段 3 的错误映射拦截器在使用）、`common/resource_name.go` 与 `common/const.go` 中被活跃代码引用的符号（如 `SystemBotID`、`ServiceAccountAccessKeyPrefix`、`InstanceNamePrefix`）、`manual_sql.go` 的 `withMetadata=false` 分支（`deleteManualSQLMetaRegistryTx` 在用）。`store/db_connection.go` 的各项在阶段 2 已删除。
> **阶段 3 续又删掉一批**：`role`/`project` 表结构与 `db.project` 列、仓库层全部 project 字段与 `BatchUpdateDatabases`、`common.GetProjectID`/`FormatProject`/`ProjectNamePrefix`/`DefaultProjectID`、store 的 12 个 `*V2` 方法名、store 侧零引用的 openlineage proto 消息（`b2e80ae`）。

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
3. **收口凭证加密**：`common.Obfuscate` 换成 AES-GCM + 环境密钥；`store.GetSecret` 用 `sync.Once`；`Profile.Secret` 接线或删除。**阶段 3 曾照此实现（`a1faf65`），阶段 5 按确认回滚**为 `AUTH_SECRET` 种子 XOR，`Profile.Secret` 改为启动时从 DB 解析（`7870016`）。
4. **统一分页**：抽出一个 `parsePageSize/nextPageToken` 实现，修复 token limit 被忽略、OpenLineage offset、LLM profile 无 token、sublevel 无 offset、`SearchMetadata` 无请求 token。
   - **阶段 3 补遗**：血缘两个列表此前完全没有 LIMIT（既无分页也无上限），现已补上 `page_size`/`page_token`/`next_page_token`（默认 500、上限 5000，`dd6df51`）；`GetLineage` 用同一个 offset 页化 source/target 两个列表，前端合并取全。
5. **拆分超长文件**：`instance_service.go`(1399)、`database_service.go`(1206)、`database_history.go`(1197) 按资源/职责拆分；`meta_resource.go`(1056)、`manual_sql.go`(775) 同样。
6. **store 事务规范化**：所有 `BeginTx` 后紧跟 `defer tx.Rollback()`；单语句查询不再开事务；提供 `tx` 参数版本供 runner 复用同一事务。
7. **删除死代码**（第二节），并在 CI 加 `go test ./...` + `-race` + `golangci-lint`。
8. **收敛 proto 表面**：删除未实现的 setting/enum/message，修正 `metaxisdata/DatabaseMetadata`、`ListUsers` method_signature、`meta_type` 裸 int32、v1/store 重复枚举。
   - **阶段 3 + 续已完成**（`73901a1` `ceb6a3d` `e0eab33` `722d3cb` `ddff264` `ee3c39b` `451cb78` `b2e80ae` `792ca71` `997ede9` `4036e1e` `733b3e0`，M9/M4 见下面"阶段 3 收尾二"）：P-H1..P-H6 全部处理；M 系列**全部处理**（含 **M9** `DataSource` 资源化与 **M4** OpenLineage/API key 资源化）；Engine 28→5、DataSource 多引擎/IAM/Vault 表面、project/role/policy 死消息与 `project`/`role` 死表、`service_name`/`granularity` 均已清理。

---

## 四、分阶段整改路线图

### 阶段 0：安全止血（必须最先做）——**已完成**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 1 | JWT 签名密钥改为环境注入并 fail-closed；作废历史 token | ✅ | `adfec91` `84b16db` |
| 2 | 恢复授权层：至少给用户/实例/数据源/OpenLineage key 的写操作加管理员校验 | ✅ | `ec49607` `0f2165e` + IAM 子系统 |
| 3 | 关闭未认证注册或强制 `DisallowSignup`；首个管理员授予改原子 | ✅ | `5b19778` `c4e22fc` |
| 4 | 修 SQL 注入（user/instance/database filter + principal/group project ID） | ✅ | `3321801` |
| 5 | 审计脱敏补 `key`/`content`/`sslKey`/`keytab`，并为 `CreateAPIKeyResponse` 加测试 | ✅ | `89ef84a` |
| 6 | panic 不再回传堆栈；`validate_only` 加权限 + 内网地址限制 | ◐ | `5446a10` `ec49607` |

- **1 的收尾说明（非剩余缺陷）**：`JWT_SECRET` 环境变量优先、缺失时回退 DB `AUTH_SECRET`、`< 32` 字符启动失败、解析侧三项校验（历史 token 失效）均已完成；`profile_release.go` 的 import 修正后（`84b16db`）`-tags release` 可编译并通过 `go vet -tags release ./...`。**阶段 5**：`JWT_SECRET` 已删除，签名密钥只从数据库 `AUTH_SECRET` 读取，环境变化不再作废 token（`7870016`）。但 `Makefile`/CI/Docker 未使用该 tag，默认产物仍是 dev 模式——部署 prod 必须显式 `-tags release`，后续应把它固化到构建目标或改为运行时配置。
- **2 的收尾说明（阶段 4 完成）**：写操作早已要求 workspaceAdmin；本轮补上读路径并落地真正的 permission→role 映射。`backend/common/permission/` 是单一来源的目录（`permission.json` → `permission_gen.go`），`backend/store/predefined_roles.go` 定义 `workspaceAdmin`（全目录）/`workspaceMember`（读基线），自定义角色存 `role` 表（增量 `0.1.0006` + `LATEST.sql`），`backend/component/iam` 解析调用者权限，`ACLInterceptor` 逐方法注解鉴权，`IamService`/`RoleService`/`GroupService` 提供管理面，`GetCurrentUser` 返回 `User.permissions` 供前端 gating，前端新增 Roles / Groups / Members & Permissions 页面。守卫与端到端验证：`TestEveryMethodIsPermissionGated`（注解覆盖 + 目录漂移）、`iam.Manager` 表驱动测试（基线/管理员/自定义角色/组展开/allUsers/条件 fail-closed）、`test/integration/runner/iam_service_test.go`（真实 server：基线拒绝 → 自定义角色与组授权 → `GetCurrentUser` 可见 → etag `Aborted` → 零管理员 `InvalidArgument` → 引用中角色/组不可删）。**仍未做**：仅 WORKSPACE 策略，无 per-resource（实例/数据库）策略；H1/H2/M2/M8/M9/M10 不受影响。
- **6 的剩余项**：**内网地址限制经确认后主动放弃**——自托管产品的核心用法就是让用户连接内网数据库，加私网 deny 会破坏功能，因此只保留管理员权限约束；CEL 类型断言 panic 本身仍未修（只是不再泄露堆栈）。
- 配置侧顺带完成：`config.Profile.Secret` 现在由 `JWT_SECRET` 赋值（不再是"从未赋值"）——**阶段 5 改为由数据库 `AUTH_SECRET` 解析，`JWT_SECRET` 已删除**（`7870016`）；`disallow_signup` 默认仍为 `false`，需管理员在新增的 `/settings/general` 页面显式打开。

### 阶段 1：正确性与可运维性——**本轮已完成（5 个任务 5 个 commit）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 7 | 补 `migration/0.1/` 增量 + guard 测试；修 `db_schema` 过滤 | ◐ | `bb93ee0` |
| 8 | 修 schemasync 两个生命周期 bug 与破坏性 diff；`LastSyncTime` 进事务 | ◐ | `fcb6a98` |
| 9 | 修 CEL 类型断言 panic；统一 `InvalidArgument` | ✅ | `ff914ac` |
| 10 | 接线日志系统（`slog.SetDefault` + LogLevel/Replace）；注册 `--external-url`（**阶段 5 已移除该 flag**，改由 `WORKSPACE_PROFILE.external_url` 管理）；把 `-tags release` 固化到构建目标 | ✅ | `7fdcead` |
| 11 | `UpdateInstance(data_sources)` 改为按 ID 合并；`UpdateDatabase` 判空 | ✅ | `20e284b` |

- **7 的收尾说明**：`db_schema` 经确认是不需要的遗留代码（`db.metadata` 里的 `DatabaseMetadata` 根本没有 `schemas` 字段，schema 树在 `meta_registry_resource`），因此**删除**而非改写：API 的 `table` 过滤器分支、store 的 join 推断、proto 里的过滤文档一并移除，`table` 现在返回 `InvalidArgument`。**`migration/0.1/` 增量目录在阶段 1 经确认不创建**——当时没有待发布的 schema 变更，空目录只是噪音；**阶段 2 因实际 schema 变更已建立该目录**（`0001` scope 列、`0002` 两个索引，见阶段 2）。
- **8 的收尾说明**：checker 不再因一次瞬时错误退出 ✅；停用实例不再被排队（`shouldSyncNow` 显式拒绝 `interval == 0`）✅；`LastSyncTime` 改为提交成功后再写、血缘排队提前 ✅；**破坏性删除按产品决策只加日志**（`logSchemaSyncDeletion` + 实例级软删 Warn），不拦截、不设阈值，因此"权限被收窄导致快照变空/变少"仍可能清空注册表，但一定留痕（见 `06` R-H3）。
- **10 的收尾说明**：日志输出改为 `os.Stdout`（原 `slog.Default` 写 stderr），`AddSource` 打开后由 `log.Replace` 裁剪为 `dir/file.go`；`make build-release` 带 `-tags release`，`make build` 与 AGENTS.md 的开发构建**刻意保持 dev**，CI/Docker 仍不存在，部署方需显式用 `build-release`。
- **11 的收尾说明**：合并语义是"请求列表决定成员、每个同名 ID 的条目按字段叠加"；proto3 无法区分"未发送"与"空串"，所以**无法通过本接口清空某个非密钥字段**，需要清空时应删除后用 `AddDataSource` 重建。顺带在 API 层补上"恰好一个 ADMIN"校验（原 `03` 低节条目）。

### 阶段 2：性能与资源——**本轮已完成（5 个任务 5 个 commit）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 12 | `GetUserByID/Email` 改定向查询；决定 `enableCache` 的去留 | ✅ | `f22f61e` |
| 13 | OpenLineage 读路径加 LIMIT/聚合/请求内解析缓存；ExplainSQL 缓存 key 加 scope + TTL | ✅ | `8c34542` |
| 14 | 补 `meta_registry_resource(object_type)`、`metadata` GIN、`principal(email)` 唯一索引 | ◐ | `ff9b22a` |
| 15 | LLM agent：ctx-aware 发送、真流式、错误传播、禁止缓存截断结果 | ✅ | `8acbfe6` |
| 16 | 连接池 lifetime/idle 配置；runner 关停超时 | ✅ | `fb8ca14` |

- **12 的收尾说明**：经确认选择"启用缓存"而非删除。`store.New` 的 `enableCache` 参数与字段删除，所有缓存读取无条件生效；缓存 miss 不再加载全部用户（`getUser` 走 `WHERE id/email` 定向查询后回填），`listAndCacheAllUsers` 删除。为配合启用，同时修掉了被"关着读"掩盖的缺陷：meta registry GUID 缓存按 `(guid, object_type)` 取 key 且 GUID-only 查询绕过缓存；`GetSettingV2` miss 时补写缓存；`GetSecret` 改为互斥锁保护的私有字段；`UpdateUser` 克隆 profile 而非原地改缓存；事务内不再预写缓存，改为提交后 `InvalidateMetaRegistryCache`。**已知边界**：仅带 GUID（不带 object type）的 `GetMetaRegistry` 不再命中缓存，改为打库（该查询本身有 `(guid, object_type)` 索引）。
- **13 的收尾说明**：经确认的范围是"LIMIT + 请求内缓存 + 单次遍历"，不做 SQL 侧聚合、不做保留清理任务（`openlineage_run` 属可审计数据）。数据集页/详情只读最近 5000 个 run（`event_time DESC`），因此**数据集统计只覆盖最近 5000 个事件**；解析器按请求 memoize preview 与 instance 列表；聚合与详情合并为一次遍历。ExplainSQL 缓存 key 纳入 scope/provider/model、新增 `scope` 列（增量 `0.1.0001`）、7 天 TTL，写入改用脱离请求的 context 并记日志。**行为变化**：缓存命中现在要求至少一个启用的 LLM profile。
- **14 的收尾说明**：增量 `0.1.0002` 加了 `object_type` 索引与 `principal (LOWER(email)) WHERE deleted = FALSE` 唯一索引；23505 在 store 映射为 `common.Conflict`、handler 转 `CodeAlreadyExists`。**`metadata` GIN 经确认不加**——搜索谓词是 `->>'name' ILIKE '%x%'`，GIN 无法服务子串匹配，且仓库没有 `@>` 包含查询；要真正走索引需改全文检索或 `pg_trgm`（行为变更）。**未做去重迁移**：项目未上线、无历史重复数据，重复邮箱会让迁移直接失败而不是静默停用账号。
- **15 的收尾说明**：SSE 改为边读边解析（行长上限 8MiB、总量上限 32MiB 且超限报错），无总超时（30s 响应头 + 60s 空闲读）；读错误/畸形 chunk/`length`/未知 finish_reason/无终止标记的 EOF/空回答都是错误，故不进缓存；tool call 按 index 排序收集；所有发送 ctx-aware 且 handler 取消子 context。`data: [DONE]` 仍视为正常结束以兼容不设 `finish_reason` 的 provider。**剩余**：`MaxTurns` 仍未由调用方显式设置（默认 6），`AgentConfig.Hooks` 仍是死代码。
- **16 的收尾说明**：池上限钳制到 `[1, 50]`（原 0 = 无上限）、`MaxIdleConns=10`、`ConnMaxLifetime=30m`、`ConnMaxIdleTime=5m`、`Initialize` 用 `sync.Once`；关停不再 `Fatal`（原会跳过 store 关闭），runner 等待上限 10s。`GetDB()` 初始化前仍返回 nil（由 `sync.Once` 保证只初始化一次）。

### 阶段 3：清理与重构——**本轮已完成（16 个 commit；proto 表面收敛见下一节收尾）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 17 | 删除第二节的死代码与遗留表面 | ✅ | `a39bc41` `e42b9ac` `40ec3a4` `b9a48a3` `fedcc12` `3cc4926` |
| 18 | 合并 CEL 翻译器、统一分页、统一错误映射、拆分超长文件 | ✅ | `7bfdfb6` `52213af` `89baa3a` `0590607` `4de82e3` `6be2262` |
| 19 | CI：`go test -race ./...` + lint | ◐ | `d3d96c1` |
| 20 | 修正 proto 契约问题（P-H1..P-H6、M 系列） | ◐ | `73901a1` |
| — | 遗留安全项：`Obfuscate` 改 AES-GCM | ◐ | `a1faf65`，**阶段 5 回滚为 XOR**（`7870016`） |
| — | 遗留测试项：`api/auth` 与 `backend/server` 测试 | ✅ | `0dae0b7` |

- **17 的收尾说明**：Go 侧死代码按第二节清单删除——`store/role.go`/`store/project.go` 整文件与两个 LRU 缓存、`stats.go` 的 5 个统计方法、`policy.go` 的 4 个 V2 CRUD、`group.go` 的写路径、`common/cel.go` 的 8 个 helper 与 6 个变量、`cel_attributes.go` 的 15 个常量、`resource_name.go` 的 26 个符号、整个 `metric` 遥测栈、`utils` 的两个死文件、测试用的双套 harness、未注册的 CLI flag、`Server.cancel` 与 `GatewayResponseModifier.Store` 等。**与 IAM 有牵连的部分逐个确认后保留**：`store/policy.go` 的工作区 IAM 路径（`GetWorkspaceIamPolicy`/`PatchWorkspaceIamPolicy`/`GetPolicyV2`）、`store/group.go` 的读路径（`GetGroup`/`ListGroups`/`UpdateGroup`，被 `utils/member.go` 与 SSO 同步使用）、`utils/member.go` 的 `GetUserFormattedRolesMap` 链路。顺带修掉两处文档与实际不符：`SystemBotID`/`ServiceAccountAccessKeyPrefix` 仍有调用者（保留），`withMetadata=false` 分支在 `manual_sql.go` 仍被使用（只删了 history 侧的死参数）。**注意**：`project`/`role` 两张表已无任何 Go 调用者，但删表属 schema 变更，未在本轮处理。
- **18 的收尾说明**：① CEL 翻译器合并为 `api/v1/filter.go` 的单一实现，字段处理器只能通过 `filterArgs` 申请占位符；唯一行为变化是 `engine in [...]` 由内联字面量改为参数绑定。② 分页统一到 `paginate[T]`，修复 token limit 被忽略、`ListMetadata` 子层级无 offset、`ListMetadataHistory` off-by-one、`ListLLMProviderProfiles` 与 `SearchMetadata` 完全没有分页四个缺陷。③ 新增 `ErrorMappingInterceptor`，`common.Code` 首次真正映射为 Connect 状态码，`connectErrorForWrite` 退役，`common.Error` 补 `Unwrap()` 且 `Error()` 不再对 nil cause panic。④ 四个超长文件按职责拆分（`meta_resource.go` → 3 个、`instance_service.go`/`database_service.go`/`database_history.go` → 各自 2–3 个），共 3 个纯移动 commit，每个都校验了"函数集合完全一致"。
- **19 的收尾说明**：新增 `.github/workflows/ci.yml`，两个 job：`go test -race -count=1 ./...` 与 `golangci-lint` v2.13.1（配置已带 `run.build-tags: [integration]`，因此集成 harness 也进入 lint）。按确认**未做**：前端 job、把 `./backend/migrator/...` 并入集成 target、缺 Docker 时的 skip 行为（T-C2）。**未验证项**：workflow 只做了本地 YAML 解析校验与本地等价命令（`go test -race`、`golangci-lint`）验证，没有在 GitHub 上真正跑过。
- **20 的收尾说明**：P-H1..P-H6 全部处理（`DatabaseMetadata` 伪引用删除并记明 GUID 是不透明标识、`method_signature="parent"` 删除、ExplainSQL `meta_type` 改枚举、OpenLineage 三处改 `page_token`/`next_page_token`、`SearchMetadata` 补分页字段、v1 `MetaType` 增 `OPENLINEAGE` 并文档化 OpenLineage 行被过滤）；M 系列做了 M3/M5/M6/M7/M8/M12/M14/M16/M17/M19/M20/M21，其中 `GetDatabase` 与 `ListInstanceDatabase` 两个 stub RPC 直接删除。**P-H6 选择的是"显式过滤 + 文档化"而不是补 oneof 分支**（补分支要在 v1 复制 store 的消息，与 M2 的重复定义问题相冲突）。**按确认推迟到下一轮**：Engine 28→3、`DataSource` 78 个字段里的 MongoDB/Oracle/Redis/IAM/SSH/Vault、SCIM/2FA/服务账号字段、未实现的 setting 枚举、policy/role/project store 消息、死 v1 消息。**未做**：M1/M9/M10/M11/M15/M18/M22/M23（需要产品决策或资源化重设计），以及仓库内并不存在的"发布/breaking-change 流程"——本轮只在 commit message 标注 `!` 并在文档说明。
- **遗留安全项收尾**：`Obfuscate`/`Unobfuscate` 改为 AES-256-GCM + SHA-256 派生密钥 + 随机 nonce + `v1:` 前缀，密钥优先取 `METADATA_SECRET_KEY`（新增 `config.Profile.EncryptionKey` + `store.WithEncryptionKey`），未配置时回退数据库 `AUTH_SECRET` 并打 Warn，key 为空或过短即报错。**破坏性影响**：XOR 时代的密文不再可读，已有部署必须重新录入实例/LLM 凭证（项目未上线，可接受）。**阶段 5 整体回滚**（`7870016`）：回到 `AUTH_SECRET` 种子 XOR，`METADATA_SECRET_KEY`/`Profile.EncryptionKey`/`store.WithEncryptionKey` 删除；代价是已写入 AES `v1:` 密文的部署需重新录入凭证。
- **遗留测试项收尾**：`api/auth` 从零测试到覆盖 token 提取/签发/校验、方法注解读取、cookie 与 gateway modifier；`backend/server` 覆盖 `/healthz`、CORS 随 profile、pprof 门控、前端占位页与 recover 中间件。**新测试顺带发现并修复一个真实缺陷**：`getAuthContext` 对未知方法名 `sd.Methods().ByName(...)` 返回 nil 后直接 `.Options()` 会 panic，现在返回 error。

### 阶段 3 收尾：proto 表面收敛——**本轮已完成（4 个 commit + 1 个测试修复；1 项注释修正因 BSR 限流回滚）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 21 | Engine + DataSource 表面收敛（v1+store+Go+前端） | ✅ | `ceb6a3d` |
| 22 | Setting / IDP / SCIM-2FA-服务账号收敛 | ✅ | `e0eab33` |
| 23 | 删除 policy/role/project/explain_sql 死 store 消息 | ✅ | `722d3cb` |
| 24 | 删除不可达的 metadata 消息与字段 | ✅ | `ddff264` |
| — | 修正 `DeleteInstanceRequest.force` 的 issue/sheet 时代注释 | ◐ | （无）改动已回滚：需要重新生成 buf 产物，遇 BSR 限流 |
| — | 修复 flaky 的 `Obfuscate` 往返测试 | ✅ | `4afe1ba` |

- **21 的收尾说明**：经确认保留的引擎集合是 **MYSQL / POSTGRES / TIDB / MARIADB / OCEANBASE**（不是文档原先建议的 3 个）——MariaDB/OceanBase 复用 MySQL 驱动且 schema/lineage 已有注册，删掉等于移除能力。`Engine` 其余 23 个值连同名字在 v1 与 store 两侧 `reserved`（编号不回收），`convertToEngine`/`convertEngine` 从 27 case 降到 5。`DataSource` 删掉 MongoDB/Oracle/Redis/Databricks/CockroachDB 字段、四类 IAM 凭据、`SASLConfig`+`KerberosConfig`、`DataSourceExternalSecret`、`authentication_private_key`；**SSH 隧道、SSL、`use_ssl`、`extra_connection_parameters` 保留**（`plugin/db/util/ssh.go`、`plugin/db/{mysql,pg}` 在用）。`mergeDataSource` 因 `additional_addresses` 删除而退化为纯 `proto.Merge`，"列表替换"的特例消失。store `Instance` 顺带删除 `roles`（两个驱动里的采集是注释代码）与 `labels`（无 v1 字段）。**破坏性**：含已删引擎/凭据类型的既有行无法被 protojson 解码，未上线可接受。
- **22 的收尾说明**：`SettingName` 只留 `AUTH_SECRET`/`BRANDING_LOGO`/`WORKSPACE_ID`/`WORKSPACE_PROFILE`/`PASSWORD_RESTRICTION`/`ENVIRONMENT`；`WorkspaceProfileSetting` 删 `require_2fa`、`token_duration`（`GetTokenDuration` 从不读它，token 时长是常量）、`maximum_role_expiration`、`enable_metric_collection`（metric 栈阶段 3 已删）与两个注释字段；`backend/server/init.go` 不再写 `EnableMetricCollection: true`。IDP 只留 OAuth2；`convertIdentityProviderType`/`convertIdentityProviderConfigString` 去掉 OIDC/LDAP 分支，**无调用者的 store 写路径**（`CreateIdentityProvider`/`ListIdentityProviders`/`UpdateIdentityProvider`/`DeleteIdentityProvider`/`getConfigBytes` 与 `UpdateIdentityProviderMessage`）删除，只留登录用的 `GetIdentityProvider`。`User.recovery_codes`、`UserProfile.source`（store+v1）、`GroupPayload.source` 删除。`LATEST.sql` 的 `setting` 名字注释、`principal.mfa_config` 注释（改为"无读写、无消息"）、`idp.type` CHECK 注释同步更新。
- **23 的收尾说明**：删除 `store/role.proto`、`store/project.proto`、`store/explain_sql.proto`（阶段 3 删消息后只剩头部的空文件）三个文件，以及 `policy.proto` 的 `TagPolicy`/`EnvironmentTierPolicy`。**与计划不同的一处**：`store.Policy`（含 `Type`/`Resource` enum）**不能删**——`store/store.go:125`、`store/group.go:114`、`store/policy.go` 在用它；原报告只 grep 了 `storepb.Policy\b`，漏掉了 `storepb.Policy_WORKSPACE` 这类常量。表结构未动（`db.project` 外键指向 `project`）。
- **24 的收尾说明**：v1+store 对称删除 `PackageMetadata`（Oracle）、`StreamMetadata`/`TaskMetadata`（Snowflake）、`LinkedDatabaseMetadata`（Oracle/Redshift）、`InstanceRoleMetadata`、`SpatialIndexConfig`/`TessellationConfig`/`BoundingBox`/`GridLevel`/`StorageConfig`/`DimensionalConfig`，以及 `SchemaMetadata.streams/tasks/packages`、`DatabaseSchemaMetadata.linked_databases`、`IndexMetadata.spatial_config`、`StoredMetadata` 的三个 oneof 分支、`DatabaseMetadata.backup_available`；`MetaType` 的 `PACKAGE`/`STREAM`/`TASK`(13–15) 两侧 `reserved`。schema syncer 里两个永远不会被 MySQL/PG 填满的循环（`schema.Packages`/`schema.Streams`）、`isSchemaSyncManagedMetaType`/`convertMetadataToGUID` 的对应分支、`getNextLevelObjectType` 的两项、`api/v1/database_convert.go` 的四个转换函数一并删除。**关键约束**：metadata 的 store→v1 转换是 `proto.Marshal` 后 `proto.Unmarshal` 到 v1 类型，两侧 field number 必须一致，因此每次删除都在两侧保留相同编号（`reserved`）。
- **注释修正（回滚）**：`DeleteInstanceRequest.force` 的 "all open issues will be closed" 应改为 handler 的真实行为（把实例的数据库移到默认 project），文案已写好，但该 proto 改动需要重新生成 buf 产物，而 BSR 远程插件在本轮触发限流；为避免提交与 proto 不一致的生成产物，改动已回滚，留待下一轮随任意 proto 变更一起提交。
- **测试修复**：阶段 3 新增的 `TestObfuscateRoundTrip` 用 `NotContains(ciphertext, plaintext)` 断言，短明文（如 `"a"`）会随机出现在 base64 密文里，`-race` 全量跑时命中一次；改为比较整体是否相等。这是本轮唯一一个"验证阶段才发现"的缺陷。

### 阶段 3 续：proto 残留、schema 清理与 M 系列——**本轮已完成（10 个 commit）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 25 | 删两侧无生产者的 `service_name`/`granularity`；store 的 `V2` 后缀重命名 | ✅ | `ee3c39b` `8b328ae` |
| 26 | 删 `role` 表、`principal.mfa_config` 列，收窄 `idp.type` CHECK（增量 `0.1.0003`） | ✅ | `904fb09` |
| 27 | 删 `project` 表与 `db.project` 列及其全部 API 表面（增量 `0.1.0004`） | ✅ | `451cb78` |
| 28 | M1/M10/M11/M15 契约形状与类型对齐 | ✅ | `792ca71` `997ede9` `4036e1e` |
| 29 | M22/M23 请求形状：逐项批同步结果、mapping 身份 + `update_mask` | ✅ | `733b3e0` |
| 30 | `setting.value` 契约写进列注释（关闭待确认 3） | ✅ | `d8ce592` |
| — | 删 store 侧零引用的 openlineage 消息（`ExternalDataset`/`NamespaceMapping`） | ✅ | `b2e80ae` |
| — | M9 `UpdateDataSource` 资源化 | ◐ | 经确认跳过 |
| — | M18 create/update 一致性 | ✅ | 不改契约，规则文档化 |

- **25 的说明**：`DatabaseSchemaMetadata.service_name`（Oracle 概念）只被 `database_convert.go` 从 store 复制到 v1、两侧都没人填，`IndexMetadata.granularity`（ClickHouse 概念）连复制都没有；两者在 v1 与 store 同时删除、编号与名字 `reserved`。store 侧 12 个 `*V2` 方法及其 impl helper 去掉后缀（`GetSettingV2`→`GetSetting` 等），`listSettingV2Impl`/`listPolicyImplV2`/`listInstanceImplV2` 统一为 `*Impl` 命名。
- **26 的说明**：`role` 表无 Go 调用者、无外键引用，`DROP TABLE` 连带其 owner sequence 与唯一索引；`principal.mfa_config` 无读写无消息。`idp.type` CHECK 收窄为 `('OAUTH2')`——drop/re-add 幂等，历史行若带 OIDC/LDAP 会让迁移以 SQLSTATE 23514 **显式失败**而不是静默保留一个登录路径已无法处理的类型。
- **27 的说明**：project 从来没有自己的 API、只有一行 "Default"，`db.project` 恒等于它。删掉持久化后，所有读取方一并删除：仓库层的 project 字段/过滤/排序/`BatchUpdateDatabases`、实例列表的 `db.project` join、v1 的 `Database.project`、`ListDatabases` 的 `projects/{project}` parent、数据库/实例的 `project` 过滤、数据库 `exclude_unassigned`、用户/分组的 project 成员过滤（读的是无人写入的 `policy` PROJECT 行）、`DeleteInstanceRequest.force` 与"移到 default project"步骤、前端两处 project 列与三个 i18n key。`common.GetProjectID`/`FormatProject`/`ProjectNamePrefix`/`DefaultProjectID` 随之为死代码并删除。
- **28 的说明**：**M1** store 审计消息改名对齐 v1（protojson 存的是枚举值名，既有 JSONB 行不受影响），并注明 v1 用资源名表达 `AuditLog.id`；**M10** `LineageRelation.transformation` 由"内嵌 JSON 的 string"改为 `repeated Transformation`，去掉 `json.Marshal` 二次编码与恒 nil 的 error 分支，前端不再 `JSON.parse`；**M11** `bytes raw_payload` 所在的死消息删除、v1 `raw_payload` 文档化为落库 JSON 文本；**M15** v1 `UserType.USER`→`END_USER`，与 store/DB 三处一致。
- **29 的说明**：`BatchSyncInstancesResponse` 由空消息改为逐项 `BatchSyncInstanceResult{name,databases,error}`，handler 不再遇错即返回——原来的"前半成功、后半失败"不再不可见；只有 `requests` 为空才整请求失败（`BatchUpdateInstances` 保持 fail-fast，其响应已能表达成功项）。`UpdateNamespaceMappingRequest` 删掉与路径重复的顶层 `id`，身份移入 `mapping.id`（路径 `{mapping.id}`）并新增 `update_mask`，store 按 mask 生成 SET；空 mask 保持旧行为（namespace/instance_resource_id 为空则跳过、`database_name` 总是写以便清空）。
- **M9/M18 的说明**：M9 需要把 `DataSource` 提升为带 `name` 的子资源并把三个自定义方法改成 AIP-133/135 的标准方法（连前端与集成测试），经确认本轮不做。M18 经确认**不改契约**：`validate_only` 只保留在真的会验证外部连接的 `CreateInstance`/`AddDataSource`/`UpdateDataSource`，`allow_missing` 只保留在已实现的 `UpdateUserRequest`，规则写进 `08`；原文对 M22 的"repeated UpdateInstanceRequest"指控是误报（AIP-231 的规定）。
- **30 的说明**：`setting.value` 是多态列（结构化 setting 存 protojson、`AUTH_SECRET`/`BRANDING_LOGO`/`WORKSPACE_ID` 存裸字符串），绑不到单一 store 消息，因此明确保留 `text` 并把契约写进 `LATEST.sql` 列注释，而不是改成 JSONB 再给每个标量加一层引号。同时关闭待确认 5：`transformation` 已结构化、`raw_payload` 的 `bytes` 那份已删、`explanation_json` 本来就是 JSONB。
- **验证**：`LATEST.sql` 与两个增量在本地 PostgreSQL 16 上实测了全新安装、模拟低版本升级、重复执行为 no-op，以及 `0003` 的历史 OIDC 行显式失败；集成套件首次真正运行（见第五节）。

### 阶段 3 补遗：正确性遗留、安全加固、性能与保留、引擎覆盖——**本轮已完成（11 个 commit + 1 个测试修复）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 31 | GUID 前缀用错分隔符（PG sequence ownership 丢失）+ IAM 条件 fail-open | ✅ | `6f2b63d` |
| 32 | 血缘列表分页（`04` M6） | ✅ | `dd6df51` |
| 33 | OpenLineage 批摄取静默丢事件（`04` M1） | ✅ | `d562a50` |
| 34 | 空 `scope_prefix` 使 `search_objects` 恒空（`04` M4）+ `BuildContextFromMetadata` 位置化配对（`05` M9） | ✅ | `11943ae` |
| 35 | `base_url` 校验与响应上限（`05` M8）、每请求查询全部 LLM profile（`05` M10）、有 tool call 时丢 assistant 文本（`05` M4）、`NewDBDebugLogger` 无界 goroutine（`05` M5） | ✅ | `10631e0` |
| 36 | `metadata` 搜索索引（pg_trgm）、`queueAll` 批量化、ExplainSQL 过期行清理、`openlineage_run` 保留策略、观察型 store 绕过缓存 | ✅ | `f50fbbc` |
| 37 | `tableExists` 忽略 `table_schema`（`06` M1）、advisory lock 用可取消 ctx 解锁、旧二进制静默跑在更新 ledger 上（`06` R-H6/M13） | ✅ | `2131420` |
| 38 | 不支持引擎每小时无限重试（`06` M3） | ✅ | `9019140` |
| 39 | 保留清理 runner + `--openlineage-retention-days`（**阶段 5 该 flag 已移除**，改由 `WORKSPACE_PROFILE.openlineage_retention_days` 管理） | ✅ | `cfa74df` |
| 40 | `MARIADB`/`OCEANBASE`/`TIDB` 的 driver/schema/lineage 注册缺口 | ✅ | `729db71` |
| — | syncer 用 digest 列表而非全量 metadata 做快照 diff（`06` M5） | ✅ | `2259abd` |
| — | 既有集成失败的根因定位（测试替身缓存，非产品缺陷） | ✅ | `f50fbbc` |
| — | `backend/server` 测试的 `-race` 竞态 | ✅ | `a16c8d4` |

- **31 的说明**：`common.GUIDPrefix` 按 `"."` 切分，而所有 GUID 用 `";"` 拼接，因此对真实 GUID 恒返回 `""`；`GetSchemaString` 用它做 sequence 子树的查询前缀，于是 PostgreSQL 表的 `ALTER SEQUENCE ... OWNED BY` / identity DDL 一直缺失。改为按 `MetaGUIDSplit` 去掉最后一段并补单测。CEL 侧 `doEvalBindingCondition` 在结果不是 bool（表达式引用了未绑定的 `resource.*`，求值为 residual）时返回 true，等于把限定单库的 binding 全局授予；现在改为 fail closed（返回错误、调用方记日志并丢弃该 binding），并把"每次求值新建 CEL env"改成构建一次。当前 binding 构造不带 condition，所以这是潜伏缺陷的收口而不是活跃漏洞。
- **32 的说明**：`GetLineage`/`GetLineageForContext` 过去对对象的全部血缘行不做任何 LIMIT，被广泛引用的表可以拉出无界结果集。两者新增 `page_size`/`page_token` 与 `next_page_token`，默认每列表 500 条、上限 5000；`GetLineage` 用同一个 offset 分别页化 source/target 两个列表（`lineage_type` 只选一个时 token 仍然有效）。前端 `getLineage` 循环取页并合并，`external_datasets` 按 GUID 合并，因此血缘图的可见集合不变；集成 harness 的 `GetLineageForContext` 也改为取全所有页。
- **33 的说明**：批摄取只要有一条成功就返回 200，解析失败的事件被 `continue` 跳过且不计入响应，生产者不会重试、事件静默丢失。现在分别统计 `processed`/`failed` 并回显，全部无法解析时返回 400（重发无用），存在服务端失败时返回 500（可重试）。
- **34 的说明**：`search_objects` 传的是空字符串指针，store 生成 `guid = '' OR guid LIKE ';%'`，什么也匹配不到——而未选实例时 `scope_prefix` 恰好为空，工具却仍在系统提示里被承诺可用。空前缀现在表示"跨全部实例"，空关键字返回明确的工具错误，搜索失败也不再被丢弃。`fetchObjectsByGUIDs` 用 `guids[:len(metas)]` 按位置重建配对，一次查找失败就会让后续 GUID 全部错位、把错误的 database/schema 名写进 LLM 提示；现在 GUID 与 metadata 成对携带。
- **35 的说明**：`FetchModels` 直接拼接 profile 里的 `base_url`（空值生成相对路径、非 HTTP scheme 交给 HTTP client）且响应无上限；新增 `ValidateBaseURL`（要求绝对 http/https + host）、8MiB 上限、复用连接池并加 15s 期限，`Create/UpdateLLMProviderProfile` 对非法 `base_url` 直接返回 `InvalidArgument`。`Registry.ListEnabled` 过去每个 ExplainSQL 请求都打一次库并解密全部 profile 的 key，还受 store 默认 limit 限制（第 50 个之后不可见）；现在走全部分页 + 30s 缓存，profile 写入时显式失效。`ConvertToLlm` 在有 tool call 时不带 `Content`，模型看不到自己上一轮的推理；已改为同时携带。`NewDBDebugLogger` 每次调用起一个无界 goroutine 且丢弃错误，改为固定 worker + 有界队列 + 单次写入超时，队列满时告警丢弃。
- **36 的说明**：搜索谓词是 `metadata -> <inner> ->> name/title/comment/userComment ILIKE '%x%'`，任何 jsonb 索引都服务不了子串匹配，只能 LATERAL `jsonb_each` 全表扫。新增 `search_text` 存储生成列（内容恰为这四个字段的拼接，由 IMMUTABLE SQL 函数维护）与 `pg_trgm` GIN 索引，谓词改写为 `search_text ILIKE $n`——匹配行完全不变（已在真实 PG 上对 `%`/`_`/`"`/`\` 等关键字逐一对拍新旧谓词）。空搜索串改为 `InvalidArgument` 而不是匹配全表。`queueAll` 过去每小时把全部 view/MV/manual SQL 的 metadata 全量取回并解析只为比较 hash，再逐对象查一次版本；现在用不带 metadata 的 digest 列表 + 每种类型一次版本查询。schemasync 的 `diff()` 同样只需要 guid/object_type/meta_hash，也改走 digest 列表（`2259abd`，`06` M5）。清理与保留侧新增 `DeleteExpiredExplainSQLCache`/`DeleteExpiredLLMDebugLog`/`DeleteOpenLineageRunsBefore`（后者同时重算受影响 task 的聚合、删除已无 run 的 task 与两者的 meta registry 行），并给两张表的 `created_at` 加索引。观察型 store 可用 `WithCacheDisabled` 绕过缓存——见下面的既有集成失败说明。
- **37 的说明**：`tableExists` 只按表名查 `information_schema`，而该视图覆盖库内所有 schema，别的 schema 里的同名表会被误认为元数据 schema 已存在、从而跳过全新安装路径；改为限定 `current_schema()` 且 `BASE TABLE`。advisory lock 的解锁用的是启动 ctx，ctx 被取消时解锁失败，连接带着会话级锁回到连接池，之后所有迁移都会死锁；解锁（与 `lock_timeout` 复位）改用不可取消的 ctx，并给加锁本身加 `lock_timeout`，让卡住的同伴副本显式失败而不是永久挂起。旧二进制对着更新 ledger 会静默启动，现在把 ledger 与二进制已知的最新版本比较并拒绝。
- **38 的说明**：`analyzeObject` 在引擎没有注册 lineage analyzer 时直接 `return nil`，什么都不记录，于是 `queueAll` 每小时把这些对象重新入队、无限重试。现在把这次跳过连同当前 meta hash 与原因写进 `column_lineage_version`：在 metadata 变化前不再重试，原因仍然可见。
- **39 的说明**：新增 `runner/maintenance`，启动时与每 6 小时跑一次：清理 ExplainSQL 过期缓存行（过期后本来就查不到，但从未删除）与 7 天前的 `llm_debug_log`（该表存完整请求/响应体）。`openlineage_run` 属可审计数据，默认永久保留；新增 `--openlineage-retention-days`（默认 0 = 不删）供运维显式开启，开启后连带重算 task 聚合并清掉空 task 与镜像的 registry 行。**阶段 5**：该 flag 已删除，保留期改由管理员在 `/settings/general` 配置的 `WORKSPACE_PROFILE.openlineage_retention_days` 决定，`runner/maintenance` 每轮读取（`7870016`）。
- **40 的说明**：四个引擎都宣称支持且都说 MySQL，但按引擎注册的表彼此不一致——driver 完全没有 `TIDB`（TiDB 实例连 Open 都失败）、schema DDL 生成缺 `MARIADB`/`TIDB`（`DiffMetadata` 报 engine not supported）、lineage 分析缺 `MARIADB`/`OCEANBASE`（这也是它们每小时被排队又被跳过的原因之一）。四个注册表现在都覆盖 MYSQL/TIDB/MARIADB/OCEANBASE；OpenLineage resolver 的 `isMySQLLike` 也纳入 OCEANBASE，让它的 dataset 名按 database.table 而不是 schema.table 切分。
- **既有集成失败的说明（根因更正）**：根因不是 analyzer 用陈旧 metadata 写回，而是**测试替身的进程内缓存**。`backend/test/integration/env/service_env.go` 的 `inspectStore` 是第二个 in-process store，`f22f61e` 打开缓存后它也开始缓存 VIEW/TABLE 行；server 跑在另一个进程，其 `InvalidateMetaRegistryCache` 不会跨进程传播，因此 `inspectStore` 永远返回那行已被 DROP 的 VIEW——而 PostgreSQL 里的 `meta_registry_resource` 与 `column_lineage` 行其实早已为空（插桩可见 rawMeta=0 / rawLineage=0）。修法是新增 `store.WithCacheDisabled()` 并让两个 harness 的 `inspectStore` 带上它，恢复 `f22f61e` 之前 harness 的语义，生产缓存不受影响。该测试现已稳定通过（单测 11.47s、整包 48.56s，与 `TestPostgresSchemaSyncAndLineage…`/`TestPostgresLineageUpdatesAfterViewChange…` 一并验证）。
- **测试竞态的说明**：`backend/server` 的 `devTestServer`/`prodTestServer` 各有一个 `sync.Once`，而所有测试都 `t.Parallel()`：两个 Once body 可能同时调用 echo-contrib 的 `registerMetrics`，在进程级 Prometheus registry 上产生 data race，约 1/5 概率让 `go test -race ./backend/server/...` 失败（阶段 3 新增该测试时留下的 harness 缺陷）。改为一个 Once 顺序构建两个 server，之后连续 8 次 `-race` 全绿。
- **验证**：见第五节"阶段 3 补遗复测"。三个增量（`0003`–`0005`）在本地 PostgreSQL 16 上实测了全新安装、升级、幂等与函数/索引可用性；`search_text` 与旧谓词逐关键字对拍；保留清理的 run/task/registry 行数变化逐项断言。集成套件（含此前稳定失败的那条）**全部通过**。

**阶段 3 补遗后仍未处理**：**M9 与 M4 的资源化重设计**（`DataSource`、OpenLineage/API key 消息，经确认继续推迟）、`api/auth` 的集成反向测试、`backend/server` 的启动/关停路径测试、`api/v1` 的 `debug_interceptor.go` 与各 handler 的测试缺口、`store` 三处查询形状 guard（T-H3）、前端 0 个 Vitest 文件、CI workflow 从未在 GitHub 实跑、缺 Docker 时集成测试的 skip 行为（T-C2）与前端 CI job、`./backend/migrator/...` 并入集成 target、`metadata` 搜索现在有 trgm 索引但**没有**改成全文检索（有意：FTS 是词元匹配，会改变 `search_objects` 的语义）、LLM profile 目前只是 30s TTL 缓存 + 写侧失效（未做按需单 profile 加载）。

### 阶段 3 收尾二：测试与 CI 收口 + M9/M4 资源化——**本轮已完成（10 个 commit）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 41 | T-C2 缺 Docker 时集成测试硬失败 → skip；`TestMain` 启动 panic 会挂到 workflow 超时（`09` M7） | ✅ | `061208c` |
| 42 | 部分外部服务 env 静默混用 testcontainers（`09` M2）、`reservePort` TOCTOU（`09` M3）、DSN 硬编码凭据 + 无条件 DROP 派生库（`09` M9）、死 `serverDir` | ✅ | `061208c` |
| 43 | migrator 集成测试的清理是静默空操作（`09` M1） | ✅ | `48d65be` |
| 44 | 文档里不存在的 `make test-integration-mysql`（`09` M10）、migrator 集成测试从不执行（`09` T-H1） | ✅ | `8823ac2` |
| 45 | store 查询形状 guard：GUID 子树 + 数据库范围谓词（`09` T-H3） | ✅ | `df97e0a` `f65baaa` |
| 46 | store 纯函数不变量 guard：manual SQL + 工作区 IAM（`09` M8） | ✅ | `711c0aa` |
| 47 | `api/v1` 审计 helper（`09` T-H5 剩余）、`debug_interceptor`、`backend/server` 启停路径、`component/llm`、`api/auth` 集成反向测试 | ✅ | `c162bc0` |
| 48 | CI 前端 job + Go 覆盖率（`09` M6）、首批前端 Vitest | ✅ | `65f4eaa` |
| 49 | M9 `DataSource` 资源化（AIP-133/134/135 + 前端 + 集成测试） | ✅ | `513940f` |
| 50 | M4 OpenLineage/API key 资源化 | ✅ | `c2a67e0` |

- **41/42 的说明**：harness 有五个缺陷会一起让 `make test-integration*` 不可信。① 没有容器运行时直接 `os.Exit(1)`，而 AGENTS.md 写的是 skip；现在两个套件共用 `backend/test/integration/dockerutil`（用 testcontainers provider 探测），runner 的 `TestMain` 打印跳过信息并 exit 0。② `TestMain` 用两个只发送一次的 goroutine 启动两个 env，setup 里任何 panic 都会让进程崩溃、收集端永远等不到第二个结果（直到 workflow 20 分钟超时）；现在 panic 被 recover 并作为错误上报。③ 只设一半 `INTEGRATION_*` 会静默混用（一个外部服务 + 一个 testcontainers），现在 `ValidateIntegrationEnv` 直接拒绝。④ `reservePort` 在子进程绑定前就关闭监听，端口可能被别的进程抢走；现在 readiness 失败会用新端口重试，且进程提前退出时立刻带日志失败而不是耗完 60s。⑤ DSN 硬编码 `postgres:postgres`/`root:root`，`recreatePostgresDatabase` 又无条件 DROP 目标库；现在凭据可用 `INTEGRATION_POSTGRES_USER/PASSWORD`、`INTEGRATION_MYSQL_USER/PASSWORD` 覆盖，且只删自己派生的 `*_integration` 库。顺带删掉恒空的 `serverDir` 管道。
- **43 的说明**：`newTestDatabase` 用 `defer admin.Close()` 关掉管理连接池，而 `t.Cleanup` 的 `DROP DATABASE` 在测试结束后才跑，错误被 `_, _ =` 丢弃——每次运行都在容器里漏一个 `migrator_test_*` 库。现在管理池活到 DROP 之后，DROP 失败会让测试失败；同一文件也不再自己匹配错误字符串，而是复用共享的容器运行时探测。
- **44 的说明**：`backend/test/integration/README.md` 与 Makefile 的 `.PHONY` 都写着 `make test-integration-mysql`，但 Makefile 里没有这个 target。现已补上（只跑 MySQL 的真实服务场景），并让 `test-integration`/`test-integration-smoke` 包含 `./backend/migrator/...`，于是全新安装、增量升级与 legacy adoption 三条迁移路径第一次进入集成门禁（CI 就是 `make test-integration`）。
- **45 的说明**：子对象列表把 GUID 子树谓词复制粘贴在两处，数据库列表的范围谓词（`ShowDeleted`、大小写、环境/实例/引擎）也只有一份实现。丢掉 `ESCAPE` 或某个 `deleted = false` 会静默扩大结果集而没有任何测试失败。现在两处都改成纯构造函数（`buildSublevelMetaRegistryResourceQuery` 复用 `appendGUIDSubtreeCondition`，`buildListDatabaseQuery` 持有范围谓词），并用表驱动测试断言谓词形状、LIKE 元字符转义，以及**占位符编号始终与参数切片长度一致**。
- **46 的说明**：工作区 IAM 的成员/角色合并过去内联在 `patchWorkspaceIamPolicyImpl` 里，撤销错角色或重复成员只能到生产才发现；现在抽成纯函数 `patchIamPolicyBindings`（顺带确定化：缺失角色按请求顺序追加，而不是 map 迭代顺序），并连同 `buildManualSQLGUID`/`normalizeManualSQLTags`/`normalizeManualSQLAttributes`/`buildManualSQLStoredMetadata`/`generateEtag` 一起补测试。
- **47 的说明**：审计拦截器的 helper（`shouldSkipAudit`、`resolveParent`/`Resource`/`Actor`、`mapSeverity`、`buildAuditStatus`、`buildRequestMetadata`、`getNestedString`）决定每条审计记录写什么、算在谁头上，过去只有脱敏名单有测试；现在补表驱动测试，并断言**已认证用户优先于调用方可控的请求字段**。`debug_interceptor` 补 >10240 字符 connect error 的 `[TRUNCATED]` 截断与短错误/普通错误透传；`backend/server` 补真实监听端口的启停路径（Shutdown 释放端口并等待 runner）；`component/llm` 补 `ValidateBaseURL`、`FetchModels`（stub provider，含 8MiB 上限）、`ConvertToLlm` 与 `BuildContextFromMetadata`；集成侧新增"真实 server 对缺失/畸形/空/alg=none 凭据返回 `Unauthenticated`"的反向测试。
- **48 的说明**：CI 过去只有 Go 单测 + lint，Vue 的构建、类型检查与 lint 只能靠开发者本机，而前端本来也 0 个测试文件。新增 `frontend` job（pnpm 10.24.0 + Node 22，`biome ci`、新增的 `lint:ci`（ESLint 不带 `--fix`）、`vue-tsc`、`vitest run`、生产构建，paths 覆盖 `frontend/**`），Go 单测 job 加 `-cover`。首批 Vitest 覆盖两处真正出过回归的前端逻辑：`extractErrorMessage` 与 `getLineage` 的分页遍历（token 推进、逐页合并、external dataset 按 guid 去重、token 不前进时的兜底）。
- **49 的说明**：`AddDataSource`/`RemoveDataSource`/`UpdateDataSource` 是挂在父实例上的自定义方法，`DataSource` 只有 `id` 没有资源名。现在 `DataSource` 声明 `metaxisdata/DataSource`（`instances/{instance}/dataSources/{data_source}`）并以 `name` 为标识，三个方法改为 `CreateDataSource`（AIP-133：`parent`/`data_source`/`data_source_id`/`validate_only`）、`UpdateDataSource`（AIP-134：`{data_source.name=...}` + `update_mask`）与 `DeleteDataSource`（AIP-135，返回 Empty）。`UpdateInstance` 不再接受 `data_sources` mask，前端编辑表单改成"新增走 Create、改动走 Update（只发改动字段）、删除走 Delete"的 diff；原 `mergeDataSources`/`mergeDataSource` 由纯函数 `patchDataSource` 取代——**掩码外的字段保持存储值**，所以由读取结果构造的更新不会清空密码，也不会把 TLS 校验降级（单测断言）。创建实例仍可携带数据源：客户端知道实例 ID 时用 `instances/{id}/dataSources/{ds}` 表达自定义 ID，否则由服务端生成。真实 server 的生命周期集成测试覆盖创建、重复 ID（`AlreadyExists`）、非法 ID 与建 ADMIN（`InvalidArgument`）、掩码更新、删除 ADMIN（`InvalidArgument`）、删除与 not-found。
- **50 的说明**：`NamespaceMappingResource`/`OpenLineageRunResource`/`OpenLineageTaskResource`/`APIKeyResource` 既没有 `google.api.resource` 也没有 `name`，路径绑 `{id}` 或 `{guid}`。四个消息改名为 `NamespaceMapping`/`OpenLineageRun`/`OpenLineageTask`/`APIKey`，各自声明资源与 `openlineage/...` pattern，首字段改为资源 `name`；`GetOpenLineageRun`/`GetOpenLineageTask` 绑 `{name=openlineage/runs/*}` / `{name=openlineage/tasks/*}`（GUID 就是 `name` 的最后一段，仍保留 `guid` 字段），`UpdateNamespaceMapping` 绑 `{mapping.name=...}`，Delete/Revoke 收资源名。`common` 新增四个 pattern 的 format/parse helper，并有往返 + 非法名拒绝测试：畸形或不属于该实例的名字在任何 store 调用之前就返回 `InvalidArgument`。前端在 API 边界拼资源名，页面仍然拿 guid/对象工作。
- **验证**：见第五节"阶段 3 收尾二复测"。集成套件（含 migrator 与新增的数据源生命周期、鉴权反向用例）全部通过；`buf format`/`lint`/`generate` 两次（M4、M9）均成功且产物干净；前端新增 12 个 Vitest 用例并通过全部本地门禁。

**阶段 3 收尾二后仍未处理**：CI workflow **仍从未在 GitHub 上实跑**（本地只验证了等价命令与 YAML 结构）；前端仍**没有覆盖率**（`test:coverage` 需要新增 `@vitest/coverage-v8` 依赖，本轮用 Go 侧 `-cover` 代替）；`09` 的低优先项（`waitForHTTPReady` 把 5xx 也当 ready、`schemasync_lineage_postgres_service_test.go` 的 `SELECT 1;` 空断言、harness fixture DDL 与 runner 测试重复（M5）、测试风格不一致与缺 `t.Parallel()`）以及"`metadata` 搜索不做全文检索、LLM profile 只做 30s TTL 缓存"两个有意取舍仍然保留；`M18` 契约一致性经确认不改契约（规则已文档化）。

### 阶段 5：settings 收回数据库 + 回滚 METADATA_SECRET_KEY——**本轮已完成（1 个 commit）**

| # | 事项 | 状态 | 提交 |
| --- | --- | --- | --- |
| 51 | `OpenLineageRetentionDays` 改为管理员设置（移除 CLI flag） | ✅ | `7870016` |
| 52 | `ExternalURL` 改为管理员设置（移除 CLI flag 与启动覆盖） | ✅ | `7870016` |
| 53 | JWT 签名密钥只从数据库 `AUTH_SECRET` 读取（移除 `JWT_SECRET`） | ✅ | `7870016` |
| 54 | 回滚 `METADATA_SECRET_KEY` / AES-GCM 到 `AUTH_SECRET` 种子 XOR | ✅ | `7870016` |

- **51–54 的说明**：四项都是"把运行期配置从 profile/环境变量收回数据库设置"，互相牵连在 `cmd/profile.go`、`config/profile.go`、`server/` 等文件里，因此作为单个 commit。① `openlineage_retention_days` 进 `WorkspaceProfileSetting`（store 字段 14 / v1 字段 4），`runner/maintenance` 每轮 `GetWorkspaceGeneralSetting` 读取，`/settings/general` 可编辑，`--openlineage-retention-days` 删除。② `external_url` 本就可由 `UpdateWorkspaceProfileSetting` 写入，本轮删除 `--external-url` 与 `initializeSetting` 的启动覆盖、补齐前端入口，未配置时 SSO 返回 `FailedPrecondition`。③ 删除 `JWT_SECRET`，`resolveJWTSecret` 始终读 `AUTH_SECRET`。④ 按确认**整体回滚** `a1faf65`：`Obfuscate`/`Unobfuscate` 回到 XOR，删除 `Profile.EncryptionKey`/`store.WithEncryptionKey`；`GetSecret` 仍对缺失/空的 `AUTH_SECRET` 报错以避免空 seed 除零。
- **取舍与代价**：`AUTH_SECRET` 重新同时承担 JWT 签名与字段混淆（`a1faf65` 之前的形态）；`METADATA_SECRET_KEY` 的注入与轮换从"待确认"关闭，但"同库密钥 XOR"（`07` U-H2 / `04` M18 / `05` C-H3）重新回到待办。已用 AES `v1:` 密文写过的部署需重新录入实例/LLM 凭证（项目未上线）。
- **验证**：见第五节"阶段 5 复测"；`buf format`/`buf lint`/`buf generate` 一次（`openlineage_retention_days`），前端 `biome`/`eslint`/`vue-tsc`/`vitest` 全绿；本机 `go test ./...` 只剩一条与本轮无关的既有失败 `TestMarshalRolePermissionsIsDeterministic`（clean HEAD 上同样失败）。

---

### 阶段 6：安全残留 + 正确性缺陷 + 性能/资源——**本轮已完成（25 步 + 1 个既有测试修复；详见 `11-phase6-plan.md`）**

阶段 5 之后，`01`–`09` 各报告中仍标着"未处理/剩余"的条目按风险分三批收口：**A 批安全残留（8 步）**、**B 批正确性缺陷（17 步）**、**C 批性能与资源（8 步）**，外加 D 批验证与文档。每步一个 commit，完整映射、验收方式与本轮决策见 [`11-phase6-plan.md`](11-phase6-plan.md)。

| 批次 | 步骤 | 状态 | 提交 |
| --- | --- | --- | --- |
| A · 安全 | A1 token 吊销加固（`02 H1`；吊销缓存仍为进程内） | ✅ | `976ebc5` |
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
| D · 验证 | D1 修复既有失败测试（`backend/store` 角色权限断言） | ✅ | `7912fc2` |
| D · 验证 | D2 全量本地验证（含 Docker 集成套件） | ✅ | 见第五节 |
| D · 验证 | D3 文档同步（本表与各模块报告标记） | ✅ | 本表 |

- **本轮决策（已从"待确认"关闭）**：① 凭证混淆**保持** `AUTH_SECRET` 种子 XOR（尊重阶段 5 的回滚），只在 `store` 侧补空 seed 防护；"同库密钥 XOR"仍是有意接受的已知风险。② 部署按**单租户**处理，读路径不新增 per-instance 授权。③ 破坏性 schema 同步**保持仅日志**。④ gRPC 反射**保持匿名**（仅写进文档）。⑤ **不改 CI workflow**，全量验证只在本地跑（含 Docker 集成套件）并把结果写进文档。⑥ `RequireResetPassword` 选**受限 token**（JWT `rst` claim + 拦截器白名单只放行自助改密/登出），而不是拒绝登录。
- **主要落地**：登录 JWT 增加 `jti`/`iat_ns` 与受限 claim（登出吊销、密码变更失效、强制改密）；CORS 改 allowlist + CSRF 双提交校验 + 请求体上限；OAuth2 加一次性 state 与 PKCE verifier；审计走脱离请求的有界 ctx 并按可信代理解析客户端 IP；ingestion key 改 SHA-256 digest 查询 + namespace 作用域；`openlineage_task` 计数改增量、批次单事务并加事件/体积上限；元数据历史批量关闭 + 分页下推；`ListDatabases` 批量取实例；external dataset 不再重写未变行；LLM 会话有显式轮数与字节预算。
- **本轮仍未处理**（有意排除，见 `11-phase6-plan.md` 第六节）：测试/CI 批次（CI release job、集成套件入 CI、前端覆盖率、`09` 低优先测试项）、死代码与低优先清理（`AgentConfig.Hooks`/`AgentEvent.Done`、`log.Stack` eager 采集等）、文档全量重写、产品决策项（per-resource IAM、`disallow_signup` 默认值、字段加密方案变更、`openlineage_run` 保留默认）、破坏性 schema 同步硬拦截、反射策略变更。

---

### 阶段 7：死代码与低优先清理——**本轮已完成（9 步 + 1 项新功能；详见 `12-cleanup-plan.md`）**

阶段 6 明确排除的"死代码与低优先清理"单独成轮。`01`–`09` 的报告已大量过期（阶段 3/6 删掉的东西仍留在清单里，部分"死代码"其实活跃），
因此本轮先用「全仓库 grep（含测试与 integration tag）+ 整程序可达性分析（`golang.org/x/tools/cmd/deadcode ./backend/bin/server`）」
逐条核对当前代码，再分步删除。范围经确认为 **B 档**：阶段 6 计划列出的死代码项 + 本轮新发现的跨包死导出符号。

| 步骤 | 内容 | 状态 | 提交 |
| --- | --- | --- | --- |
| E1 | `component/llm`：`AgentHooks`/`AgentConfig.Hooks`、`AgentEvent.Done`、`AgentEventTurnEnd`（`05`） | ✅ | `70e7c20` |
| E2 | runners：`Syncer.profile`+参数、嵌入 `sync.Mutex`、`Analyzer.profile`+参数、命名返回 `retErr`、陈旧 TODO（`06`） | ✅ | `e2f381f` |
| E3 | `permission.Exists`、`GetInstaceFromGUID` 拼写、`ErrorWithPosition`/`LockTimeoutError`/`IsNonTransactionStatement`/`IsSystemUser`、未接线的序列 DDL 与多文件 SDL 脚手架（`07`） | ✅ | `cdc2c42` |
| E4 | store：`FindMetaRegistryResourceMessage` 的 `ID`/`IDList`/`ExcludeObjectType`、`FindMetaRegistryHistoryMessage.ValidFrom`、随之无读路径的 ID-keyed `metaRegistryCache`（`03`） | ✅ | `40996c6` |
| E5 | api/v1：两个空实现 `userCountGuard` 及其 4 个调用点（`02`） | ✅ | `99d7c6b` |
| E6 | migrator：空的 `goMigrations`/`GoMigrationFunc` 脚手架（`06`） | ✅ | `4ea38d2` |
| E7 | lineage：仅测试用死 helper（`RunLineageTestsFromYAML`、`Assert*`、`CreateSimpleCatalog`/`CreateCatalogWithSchema`、`Bool`/`Int`/`RelType` 及 mysql/postgresql 的死包装）（`09`） | ✅ | `33f410e` |
| E8 | ExplainSQL 未用 proto 字段 `meta_type`/`sections_json`/`expired`/`response.error` → `reserved`，前端与 i18n 同步（`04`） | ✅ | `bf70f70` |
| E9 | **真正的前端内嵌**：`//go:build embed_frontend` + `//go:embed frontend_dist` + SPA fallback，Makefile `build-embed`/`frontend-dist`（`01`/`10` 第六节） | ✅ | `f3a0394` |

- **本轮决策（已从"待确认"关闭）**：① ExplainSQL 的四个未用字段**全部删除**（`meta_type` 服务端本就忽略、`sections_json` 前端不读、`expired` 服务端从不设置、`error` 从不发送），编号 `reserved`；② 空 `goMigrations` 脚手架**删除**（未来需要 Go 数据迁移时再加回）；③ 前端内嵌**真正实现**而不是继续留 placeholder；④ `Store.DeleteCache`、`log.Stack` eager 采集、`08 M13` JSONB 列注释（属 schema 变更）、`06`/`03` 的 Low 语义/性能项（O(n²) 查找、无界 map、GUID 分隔符碰撞、LIMIT/OFFSET 插值等）**本轮不做**。
- **主要落地**：llm 事件与 runner 结构体只剩真正被读写的内容；`permission.Exists`、两个死错误类型、两个 pg 死函数与从未接线的独立序列 DDL/多文件 SDL 脚手架删除；store `find` 消息与缓存只保留有读路径的部分；api/v1 去掉两个恒 `nil` 的配额守卫；migrator 去掉空的 Go 迁移注册表；lineage 测试工具链只留 YAML 路径真正用到的部分；ExplainSQL 公共契约与前端一起瘦身；服务器新增 `-tags embed_frontend` 自包含构建（`make build-embed`）。
- **核对更正**（避免把活跃代码当死代码删掉）：`GetSchemaFromGUID` 实际被 `database_metadata.go` 使用（首次 grep 输出被截断误判，构建失败后恢复）；testutil 的 `RunLineageTests`/`RunLineageTest` 被 YAML 目录运行器调用，必须保留。`resource_name.go` 零引用常量、`common/cel.go` 的 M1/M2、`const.go` 死常量、`metric` 栈、`role`/`project` 死表、`instance.labels`、`08` 的 `RiskLevel`/`Position`/`Range`/`InstanceRoleMetadata`、`ExplainSQLRequest.provider_name`、`buildDeleteManualSQLStatement`、`pluralize`、`InstanceService.stateCfg`、`config.Profile.LastActiveTS` 等经核对**此前已删除或本就活跃**，本轮不动。

---

## 五、验证方式

- 本报告结论来自源码通读 + `go build ./...`、`go vet ./...`、`go test ./...`（审查时均 exit 0）。
- `golangci-lint` 在审查环境无法运行（cache/加载问题，非仓库缺陷），当时 lint 清洁度未验证；**阶段 0 修复后已可运行并输出 `0 issues.`**。
- 多数安全结论（SQL 注入、JWT 伪造、SSRF、缓存串租户、panic 路径）为代码级论证，**建议在修复前先补最小复现测试**（尤其是 SQL 注入与 ExplainSQL 缓存碰撞）。
- 阶段 0 已补的回归测试：`backend/api/v1/filter_injection_test.go`（注入载荷 + LIKE 转义）、`backend/api/v1/audit_test.go` 扩展（`key`/`sslKey`/`cert` 等脱敏）。
- 阶段 0 复测：`gofmt -l` 空、`go build ./...`、`go vet ./...`、`go test ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`go vet -tags integration ./...`（仅编译）与 dev/prod 两种二进制构建全部通过；prod profile 需 `go build -tags release`（`84b16db` 修正 import 后才可用）。
- 阶段 1 复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`、`go test ./...`（含新增 8 个 guard 测试）、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release`（`-tags release` 二进制构建）、`go vet -tags release ./...` 全部通过；另实测 `--enable-json-logging` 输出 JSON 行、`source` 路径已裁剪、`--external-url` 出现在 `--help` 中。集成测试仍未运行（需 Docker），前端未改动故未重跑前端检查。
- 阶段 1 关闭的"待确认"：① `driver.SyncDBSchema` 确实会在不报错的情况下返回空/部分 schema（MySQL 的 `information_schema` 按权限过滤行），这是 R-H3 只加日志的依据；② `table` 过滤器前端未使用，且 `db.metadata` 无 `schemas` 字段、`db_schema` 表从未存在，故整体删除而非改写。
- 阶段 2 复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`、`go test ./...`（含新增 13 个 guard 测试）、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release`、`go vet -tags release ./...` 全部通过。迁移在本地 PostgreSQL 16 上实测：全新安装、0.1.0→0.1.2 真实升级（migrator 日志 `Migrating 0.1.1.`/`0.1.2.` + `schema_migration_history` 落账）、增量重复执行幂等、唯一邮箱索引语义（拒绝大小写变体、软删后可复用）均通过；服务端 SIGTERM 关停路径也在该测试中顺带验证。集成测试仍未运行（需 Docker），前端未改动故未重跑前端检查。
- 阶段 2 关闭的"待确认"：`enableCache` 的去留（经确认启用，`f22f61e`）。仍待确认：部署拓扑（单租户？）、`RETURNING` 顺序、部分 proto 字段是否为有意保留。
- 阶段 3 复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`、`go vet -tags release ./...`、`go vet -tags integration ./...`、`go test ./...`、`go test -race -count=1 ./...`（CI 的 unit 命令，本地实测通过）、`golangci-lint run --allow-parallel-runners`（0 issues，配置新增 `run.build-tags: [integration]` 后仍为 0）、`make build-release` 全部通过。前端因 proto 改动重跑 `vue-tsc --build`（0 错误）、`biome check`（改动文件）、`eslint`（改动文件）与 `vite build`（成功）。`buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 均通过，且生成产物可复现（未改动 proto 时 `buf generate` 无 diff）。
- 阶段 3 关闭的"待确认"：`metaxisdata/DatabaseMetadata` 是有意的不声明伪类型还是声明被删——**按"GUID 是不透明标识、不应声明资源"处理**，删除 8 处 `resource_reference` 并写明约定（`73901a1`）；`getAuthContext` 对未知方法名会 panic——**确认为真实缺陷并修复**（`0dae0b7`）。
- 阶段 3 新增的"待确认"：① ~~`METADATA_SECRET_KEY` 的实际部署方式（KMS？secret 注入？）与轮换流程~~（**阶段 5 已关闭**：该密钥连同 `JWT_SECRET` 一起删除，运行期配置统一回到数据库设置，`7870016`）；② `project`/`role` 死表与 proto 过宽表面的收敛时机（已确认推迟到下一轮）；③ CI workflow 尚未在 GitHub 上实跑。
- 阶段 3 收尾复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`、`go vet -tags release ./...`、`go vet -tags integration ./...`、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release` 全部通过；`buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 通过且可复现（改动确认后重跑无 diff）；前端 `vue-tsc --build` 0 错误、`biome check src`（177 文件）、`eslint src --max-warnings=0`、`vite build` 全部通过。集成测试仍需 Docker，仍未运行。
- 阶段 3 收尾关闭的"待确认"：`principal.mfa_config` 是死列（2FA 无任何实现，`LATEST.sql` 注释已改）、`store.ExplainSQLCache` 应删除（已删，手写 `ExplainSQLCacheRow` 是唯一形状）、`policy`/`user_group` 表是活路径、`project`/`role` 表无调用者但受外键约束不能直接删。
- 阶段 3 收尾新增的"待确认"：① `project`/`role` 表与 `db.project` 列的删除时机（需要一次真实 schema 变更，且要同时改 `store/database.go` 的 project 写入）；② `MARIADB`/`OCEANBASE` 虽被保留为一等引擎，但 `plugin/lineage/mysql` 只注册了 MYSQL/TIDB、`plugin/schema/mysql` 只注册了 MYSQL/OCEANBASE，两个引擎的 schema/血缘覆盖仍是缺口（本轮刻意未改行为，见 `08`）；③ `DatabaseSchemaMetadata.service_name`、`IndexMetadata.granularity` 等未清字段的删除时机。
- 阶段 3 续复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`、`go vet -tags release ./...`、`go vet -tags integration ./...`、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release` 全部通过；每次 proto 改动后 `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 均通过且产物可复现（未改 proto 时无 diff）；前端 `vue-tsc --build` 0 错误、`biome check src`（177 文件）、`eslint src --max-warnings=0`、`vite build` 全部通过。`LATEST.sql` 与 `0.1.0003`/`0.1.0004` 在本地 PostgreSQL 16 实测：全新安装、模拟 0.1.2→0.1.3 与 0.1.3→0.1.4 升级（先把旧表/列恢复回去）、重复执行为 no-op，`0003` 另验证历史 OIDC 行会以 SQLSTATE 23514 显式失败。**这也是本轮第一次真正运行集成套件**（本机 Docker 可用）：除下一条外全部通过。
- 阶段 3 续的**新发现**（`TestPostgresLineageDeletedWhenViewDroppedRealServerIntegration` 稳定失败）**已在阶段 3 补遗定位并修复**：根因是集成 harness 的 `inspectStore`（第二个 in-process store）自己缓存了 VIEW 行，而 server 进程的缓存失效不会跨进程传播，于是它一直返回那行已被 DROP 的 VIEW；数据库里的 `meta_registry_resource`/`column_lineage` 其实早已为空（插桩可见 rawMeta=0 / rawLineage=0）。修法是新增 `store.WithCacheDisabled()` 并让 harness 的两个 `inspectStore` 启用它（`f50fbbc`）。此前「怀疑 lineage analyzer 用陈旧 metadata 写回」的猜测是错的，已更正；`09` 第五节同步。
- 阶段 3 续新增的"待确认"：① 既有集成失败的根因与修复时机——**已关闭**（`f50fbbc`，见上）；② M9/M4 资源化重设计的产品决策——**已确认继续推迟**；③ 部署拓扑、~~`METADATA_SECRET_KEY` 的注入与轮换~~（**阶段 5 已关闭**）、`RETURNING` 行序等前几轮的待确认仍未关闭。
- 生成产物的一个运维注意事项：`proto/buf.gen.yaml` 使用 `clean: true` + 远程插件，BSR 限流（`resource_exhausted: too many requests`）会在生成失败前先清空输出目录；本次收尾确实遇到过一次并已 `git reset` 恢复。**在提交前务必确认 `git status` 没有大批生成文件被删除**，限流时等一段时间重试。本轮在提交这个仅改注释的 proto 变更时误把 `clean: true` 清空后的空目录 `git add` 进了 commit（全仓库 93 个生成文件被删），已用 `git reset --hard HEAD~1` 回滚，并在后续重试全部失败后**放弃了该注释改动**。该遗留已在阶段 3 续以"删除 `DeleteInstanceRequest.force` 字段"收口（`451cb78`）；阶段 3 续全程 `buf registry whoami` 显示已登录（metaxisdata），四次 `buf generate` 均成功，说明那次限流是一次性事件。
- 阶段 3 补遗复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`）、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release` 全部通过；`buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 通过且产物可复现（`lineage_service.proto` 新增 6 个分页字段，只有 3 个生成文件变化）；前端 `vue-tsc --build` 0 错误、`biome check src`（177 文件）、`eslint src --max-warnings=0`、`vite build` 全部通过。**集成套件全部通过**：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...`（`runner` 48.56s + `migrator` 12.83s，exit 0），包括此前稳定失败的 `TestPostgresLineageDeletedWhenViewDroppedRealServerIntegration`（单测复跑 11.47s PASS，两个邻居用例同跑 PASS）。**三个增量（`0003`–`0005`）在本地 PostgreSQL 16 上实测**（临时程序，跑完即删）：全新安装记录 `0.1.5`、`search_text` 是 `GENERATED ALWAYS` 列、`pg_trgm` 与该 GIN 索引存在、重复 `MigrateSchema` 为 no-op、ledger 被插入 `9.9.9` 后启动被拒绝、把 `search_text`/函数/三个索引删掉并把 ledger 回退到 `0.1.4` 后重新迁移能完整恢复（`0005`）；`search_text` 谓词与旧 `jsonb_each` 谓词在 `orders`/`ORDER`/`customer`/`%`/`_`/双引号/反斜杠等关键字上逐一对拍行数一致，`SET enable_seqscan=off` 后 `EXPLAIN` 显示 `Bitmap Index Scan on idx_meta_registry_resource_search_text`；保留清理前 3 run/2 task/5 registry 行，删除两个过期 run 后剩 1/1/2，且存活 task 的 `run_count` 被重算为 1；ExplainSQL 过期行被删除、未过期行保留。
- 阶段 3 补遗"待确认"：无新增阻塞项。`openlineage_run` 默认仍是永久保留（**阶段 5**：`--openlineage-retention-days` 已改为 `WORKSPACE_PROFILE.openlineage_retention_days` 设置项，`7870016`）；`metadata` 搜索仍是子串匹配而非全文检索（有意保持 `search_objects` 语义）。
- 阶段 3 收尾二复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`）、`go test ./...`、`go test -race -count=1 -cover ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release` 全部通过；M4 与 M9 两次 proto 改动后 `buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 均通过且产物只有预期文件变化；前端 `vue-tsc --build`（0 错误）、`biome check`（180 文件）、`eslint --max-warnings=0`、`vitest run`（12 个用例）、`vite build` 全部通过。**集成套件全部通过**：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...` → `runner` 49.48s、`migrator` 11.29s，exit 0；其中新增的 `TestDataSourceResourceLifecycleRealServerIntegration` 单测复跑 18.11s PASS、`TestUnauthenticatedRequestsAreRejectedRealServerIntegration` 19.58s PASS。
- 阶段 3 收尾二"待确认"：无新增阻塞项。仍未关闭的是前几轮的部署拓扑（是否有反向代理、是否单租户）、~~`METADATA_SECRET_KEY` 的注入与轮换流程~~（**阶段 5 已关闭**）、`RETURNING` 行序；另外 CI workflow 仍从未在 GitHub 上真正执行。
- 阶段 5 复测：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`、`go test ./...`、部署构建 `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go`、`golangci-lint run --allow-parallel-runners`（0 issues）全部通过；`buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 通过且产物只有预期文件变化（`WorkspaceProfileSetting` 新增 `openlineage_retention_days`，store/v1/前端/API 文档同步）；前端 `biome check`（188 文件）、`eslint`、`vue-tsc --build`（0 错误）、`vitest run`（17 用例）全部通过。本机 `go test ./...` 另有一条**既有失败** `TestMarshalRolePermissionsIsDeterministic`（在干净 HEAD worktree 上同样失败：本机 protobuf 的 protojson 在数组元素间输出 `", "`，而断言写的是 `","`），与阶段 5 无关。集成测试未重跑（需 Docker）。
- 阶段 5"待确认"：无新增阻塞项。`AUTH_SECRET` 同时承担 JWT 签名与字段混淆是这次明确选择的取舍；若将来要分离，需要新设置项与迁移路径。`METADATA_SECRET_KEY` 相关待确认全部关闭。
- 阶段 6 复测（全部本地，含 Docker 集成套件）：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`）、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release` 全部通过；`buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 通过且可复现（重跑无 diff，只有 `auth_service`/`user_service` 注释变更的产物）；前端 `biome check src`（187 文件）、`eslint src`、`vue-tsc --noEmit`、`vitest run`（17 用例）、`vite build` 全部通过。**集成套件全部通过**：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...` → `runner` 48.7s、`migrator` 13.2s，exit 0。本轮新增的真实 server 用例 `TestOpenLineageIngestionAggregatesRunsRealServerIntegration` 覆盖批次单事务、run 去重计数、latest 语义、lineage 计数增减、未变 dataset 不重写与 scope 403。
- 阶段 6 顺带修复的既有失败：① D1 修掉 `TestMarshalRolePermissionsIsDeterministic`（改成语义断言，不再依赖 protojson 的随机空白）；② D2 发现 B13 引入的集成失败——既有用例给 `manual_sql_id` 传了含下划线的值（`common.IsValidResourceID` 按 AIP-122 只允许小写字母/数字/连字符），已把三个测试 ID 改为连字符形式（`bef6706`）。这提醒：新增服务端校验后必须跑一遍集成套件。
- 阶段 6"待确认"：无新增阻塞项。仍开放的是前几轮遗留：token 吊销缓存是进程内的（多副本下登出不通告，密码变更失效跨副本有效）、`openlineage_run` 默认永久保留、per-resource IAM、`MARIADB`/`OCEANBASE` 的 plugin 覆盖缺口、CI workflow 仍未在 GitHub 上实跑。
- 阶段 7 复测（全部本地，含 Docker 集成套件）：`gofmt -l backend/` 空、`go build ./...`、`go vet ./...`（默认/`release`/`integration`/`embed_frontend`/`release embed_frontend` 五种标签组合）、`go test ./...`、`go test -race -count=1 ./...`、`golangci-lint run --allow-parallel-runners`（0 issues）、`make build-release` 全部通过；`buf format -w proto`、`buf lint proto`、`cd proto && buf generate` 通过且可复现（重跑无 diff，`explain_sql_service` 的 proto/Go/TS/文档/openapi 产物同步）；`go mod tidy` 把 testutil 直接依赖的 `gopkg.in/yaml.v3` 从 indirect 提升为 direct；前端 `biome check src`（187 文件）、`eslint src --max-warnings=0`、`vue-tsc -b`、`vitest run`（17 用例）、`vite build` 全部通过。**集成套件全部通过**：`go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...` → `runner` 61.8s、`migrator` 22.3s，exit 0。
- 阶段 7 的前端内嵌验证：本机 `pnpm` 被 `packageManager: pnpm@10.24.0` 门控（实装 11.10.0），故按 `make build-embed` 的等价步骤执行——`frontend/node_modules/.bin/vite build` 生成 `frontend/dist` → 拷进 `backend/server/frontend_dist` → `go build -tags "release embed_frontend"`（84MB，对比非内嵌 67MB）→ 临时包内用例断言 `/` 与 `/instances/some-instance` 都返回含 `id="app"` 的 SPA shell、`/assets/<hash>.js` 返回 200（用例用完即删）。只留 `.gitkeep` 时 `go vet -tags embed_frontend` 仍可编译（运行时告警"no frontend was bundled"）。
- 阶段 7"待确认"：无新增阻塞项。`08 M13`（JSONB 列注释 `Stored as`）仍是唯一明确遗留的契约类小项；`Store.DeleteCache` 无调用者但保留；`06`/`03` 的 Low 语义/性能项与 CI 相关项继续开放。
