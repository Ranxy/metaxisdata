# 08 · Proto 契约（proto/v1 公开 API + proto/store 持久化形状）

**范围**：`proto/v1/v1/*.proto`（10 个服务 + `common.proto`/`annotation.proto`；阶段 0 新增 `setting_service.proto`，共 11 个服务）与 `proto/store/store/*.proto`。

**结论**：公开 API 混合了 AIP 标准方法与大量 Bytebase 时代表面（IAM/policy/role/group/project store 消息、28 种数据库引擎、注释里的 issue/task/approval、SCIM/2FA/Entra ID 字段）。store 与 v1 的一致性双向破损：`principal.mfa_config` 文档指向不存在的 `MFAConfig`；store `StoredMetadata` 有 v1 无法表达的 OpenLineage 分支；`Engine`/`MetaType`/`DataSourceType` 在两个包中重复定义。分页风格不一致（标准 page_token 与 OpenLineage 的裸 offset 并存）。另有一条真实的密钥泄露路径：`ssl_key` 与 GCP `content` 不会被审计脱敏。

**阶段 0 更新**：P-C1 ✅（脱敏名单补齐，proto 字段名未改）；P-H2/P-H3/P-H4/P-H5/P-H6 等契约问题**未处理**。阶段 0 的 proto 改动集中在：新增 `SettingService`、为写方法补 `permission` 注解、`UpdateUserRequest` 增加 `current_password`、`buf.gen.yaml` 固定插件版本（`099fbc4`，避免重新生成时产生无关 churn）。

**阶段 1 更新**：`ListDatabasesRequest.filter` 的文档删除了 `table` 过滤器（连同实现，`bb93ee0`）——这是一处**公开契约的收窄**，属于删除从未工作的功能，不是 breaking change 的规避。其余契约问题未处理。

> 注：`buf lint` 只启用了 `BASIC`（`proto/buf.yaml`），AIP 合规没有工具强制。
**阶段 3 更新**：P-H1..P-H6 全部处理、M 系列处理了 M3/M5/M6/M7/M8/M12/M14/M16/M17/M19/M20/M21，`buf format`/`buf lint`/`buf generate` 通过，生成产物（`backend/generated-go/`、`frontend/src/types/proto-es/`、`proto/gen/grpc-doc/`）与前端调用点同步更新（`73901a1`）。要点：`metaxisdata/DatabaseMetadata` 的 8 处 `resource_reference` 删除并写明"GUID 是不透明 `;` 连接标识"；`ListUsers` 的 `method_signature="parent"` 删除；ExplainSQL `meta_type` 改为公共 `MetaType` 枚举（并注明服务端以注册表为准、忽略该字段）；三个 OpenLineage 列表的 `int32 offset` 换成 `page_token` + `next_page_token`；`SearchMetadata` 补 `page_size`/`page_token`；v1 `MetaType` 增 `OPENLINEAGE = 100`，`StoredMetadata` 文档化"OpenLineage 行被显式过滤"（**选择过滤+文档而不是补 oneof 分支**，因为在 v1 复制 store 消息会加重 M2 的重复定义）；`ListDatabase`/`ListManualSQL`/`ListNamespaceMapping`/`ListAPIKey` 改为复数；`GetDatabase` 与 `ListInstanceDatabase` 两个 stub RPC 删除；store `ExplainSQLCache` 删除；`auth_method` 扩展与 `AuthMethod` 枚举删除；`GetCurrentUser` 去掉 `allow_without_credential`；ID 字符类文档修正；`GetLineage`/`GetLineageForContext` 去掉虚构的 `lineages/{guid}` 路径。**经确认推迟到下一轮**：Engine 28→3、`DataSource` 的多引擎/IAM/SSH/Vault 字段、SCIM/2FA/服务账号字段、未实现 setting 枚举、policy/role/project store 消息、死 v1 消息（`RiskLevel`/`Position`/`Range`/`InstanceRoleMetadata`/`DependencyTable` 等）。**未做**：M1/M9/M10/M11/M15/M18/M22/M23（需产品决策或资源化重设计）。

**阶段 3 收尾更新**（proto 表面收敛，4 个 commit）：把上一轮推迟的过宽表面全部收敛完，`buf format`/`buf lint`/`buf generate` 通过并重新生成了三处产物。**A 表面**（`ceb6a3d`）：`Engine` 从 28 个值收到 5 个（`MYSQL`/`POSTGRES`/`TIDB`/`MARIADB`/`OCEANBASE`——后两者由 MySQL 驱动承载），其余编号与名字全部 `reserved`，`convertToEngine`/`convertEngine` 各删 22 个 case；`DataSource` 删掉 MongoDB/Oracle/Redis sentinel/Databricks/CockroachDB 字段、四类 IAM 凭据与 `AuthenticationType`、`SASLConfig`+`KerberosConfig`、`DataSourceExternalSecret`（含 `SAECRET_TYPE_UNSPECIFIED` 拼写错误）、`authentication_private_key`；**SSH 隧道、SSL 材料、`use_ssl`、`extra_connection_parameters` 保留**（MySQL/PG 驱动确实在读）。store `Instance` 顺带删掉无生产者的 `roles` 与无 API 的 `labels`。**B 收敛**（`e0eab33`）：`SettingName` 只留 6 个被读写的值，`WorkspaceProfileSetting` 删 `require_2fa`/`token_duration`（`GetTokenDuration` 从不读）/`maximum_role_expiration`/`enable_metric_collection`；IDP 只留 OAuth2（OIDC/LDAP 配置与枚举值 reserved，`store/idp.go` 的写路径本就无调用者，一并删除）；`User.recovery_codes`、`UserProfile.source`、`GroupPayload.source` 删除。**C 收敛**（`722d3cb`）：`TagPolicy`/`EnvironmentTierPolicy`/`RolePermissions`/`Project`/`Label` 与其 proto 文件删除，**`Policy` 的两个 enum 保留**（`store.go`/`group.go`/`policy.go` 仍在往 `policy` 表的 text 列写 `WORKSPACE`/`PROJECT`/`IAM`——原报告"零 Go 使用"的说法对这两个 enum 不成立）。**D 收敛**（`ddff264`）：v1+store 的 `PackageMetadata`/`StreamMetadata`/`TaskMetadata`/`LinkedDatabaseMetadata`/`InstanceRoleMetadata` 与六种 spatial index 配置删除，`MetaType` 的 `PACKAGE`/`STREAM`/`TASK`(13–15) reserved，`DatabaseMetadata.backup_available` 与 `IndexMetadata.spatial_config` 删除；v1 `common.proto` 的 `Position`/`Range`/`RiskLevel` 删除。**保留但已知为遗留**：`DatabaseSchemaMetadata.service_name`（Oracle 概念）、`IndexMetadata.granularity`（注释写 ClickHouse）、`principal.mfa_config` 列、`idp.type` 的宽 CHECK、`project`/`role`/`policy` 表本身——删表属 schema 变更，需要单独一轮（`db.project` 有指向 `project` 的外键）。**仍未做**：M1/M9/M10/M11/M15/M18/M22/M23，以及 `DependencyTable`（store 侧 PG 同步在用，v1 侧经 marshal 复制，双方都保留）。**未完成**：`DeleteInstanceRequest.force` 的 issue/sheet 时代注释本来也一并改了，但该注释改动需要重新生成 buf 产物，而 BSR 远程插件在本轮已触发限流（`resource_exhausted: too many requests`），为避免提交与 proto 不一致的生成产物，已回滚该改动（见 `10` 第五节的踩坑记录）。

**阶段 3 续更新**（proto 残留 + schema 清理 + M 系列，10 个 commit）：`ee3c39b` 删掉两侧无生产者的 `DatabaseSchemaMetadata.service_name`（Oracle）与 `IndexMetadata.granularity`（ClickHouse），编号与名字 `reserved`。`451cb78` 把 project 表面连根拔掉：store `project` 表与 `db.project` 列（增量 `0.1.0004`）、v1 `Database.project`、`ListDatabases` 的 `projects/{project}` parent、数据库/实例的 `project` filter、数据库 `exclude_unassigned` filter、`DeleteInstanceRequest.force` 与它的"移到 default project"步骤；`store.Policy` 保留（WORKSPACE/IAM 行仍是活路径），但注释更正为只有 WORKSPACE 有生产者。`904fb09` 删掉 `role` 表、`principal.mfa_config` 列，并把 `idp.type` CHECK 收窄为 `('OAUTH2')`（增量 `0.1.0003`）。**M 系列**：**M1**（`792ca71`）store 审计消息改名对齐 v1（`AuditLogSeverity`/`AuditLogStatus`/`AuditRequestMetadata`），`AuditLog.id` 加注释说明 v1 用资源名 `{parent}/auditLogs/{id}` 表达同一个值；**M10**（`997ede9`）`LineageRelation.transformation` 从"内嵌 JSON 的 string"改为 `repeated Transformation`，handler 去掉二次编码与恒 nil 的 error 分支，前端不再 `JSON.parse`；**M11**（`997ede9` `b2e80ae`）删掉 store 侧零引用的 `OpenLineageRun`/`OpenLineageTask`（`bytes raw_payload` 就在这里）以及同样零引用的 `ExternalDataset`/`NamespaceMapping`（活的是手写 `*Message` 与 `SchemaField`），v1 `raw_payload` 文档化为"落库的 JSON 文本"；**M15**（`4036e1e`）v1 `UserType.USER`→`END_USER`，与 store `PrincipalType.END_USER`、`principal.type` CHECK 三处对齐；**M22**（`733b3e0`）`BatchSyncInstancesResponse` 改为逐项 `BatchSyncInstanceResult`，handler 不再 fail-fast；**M23**（`733b3e0`）`UpdateNamespaceMappingRequest` 的 id 移入 `mapping.id`（HTTP 路径 `{mapping.id}`）并新增 `update_mask`，store 按 mask 写列。**未做（经确认跳过）**：M9（`UpdateDataSource` 资源化）——它仍是父实例上的 PATCH 自定义方法，`DataSource` 没有资源名。**不需改契约（经确认）**：M18
——`validate_only` 只出现在真的会验证外部连接的地方（`CreateInstance`/`AddDataSource`/`UpdateDataSource`），`allow_missing` 只有已实现的 `UpdateUserRequest`，规则见下；M22 原文说"`repeated UpdateInstanceRequest` 应为 repeated 资源"是误报（AIP-231 就是这么规定的），真正的缺陷是 `BatchSyncInstances` 前半成功、后半失败时部分成功不可见，已修。

**阶段 3 补遗更新**：v1 `GetLineageRequest`/`GetLineageForContextRequest` 新增 `page_size`/`page_token`，两个响应新增 `next_page_token`（`dd6df51`）——这是**纯增量**改动；`GetLineage` 的 `page_size` 对 `relations_source` 与 `relations_target` 两个列表分别生效，沿用 `ListMetadata` 的表述。除这两个分页字段外，本轮没有别的 proto 改动。M9（`DataSource` 资源化）与 M4（OpenLineage/API key 资源化）经确认**继续推迟**，仍是唯一的契约类遗留。

**阶段 3 收尾二更新**（最后两个契约遗留，2 个 commit）：**M4**（`c2a67e0`）把 `NamespaceMappingResource`/`OpenLineageRunResource`/`OpenLineageTaskResource`/`APIKeyResource` 改造成真正的资源——改名去掉 `-Resource` 后缀、声明 `metaxisdata/<Kind>` 与 `openlineage/...` pattern、`name` 取代 `int64 id`，Get 绑 `{name=openlineage/runs/*}` / `{name=openlineage/tasks/*}`，Update 绑 `{mapping.name=...}`，Delete/Revoke 收资源名；只读聚合消息保持原样。**M9**（`513940f`）把 `DataSource` 变成 Instance 的 AIP 子资源：声明 `metaxisdata/DataSource`（`instances/{instance}/dataSources/{data_source}`）、以 `name` 为标识，`AddDataSource`/`RemoveDataSource`/`UpdateDataSource` 改为 `CreateDataSource`（AIP-133）/`UpdateDataSource`（AIP-134）/`DeleteDataSource`（AIP-135），`UpdateInstance` 不再接受 `data_sources` mask。两次改动都同步了 `common` 的资源名 helper、Go handler、前端调用点与生成产物，并各有一个 guard（资源名往返/非法名拒绝）或集成测试（真实 server 上的数据源生命周期、未认证反向用例）。至此 **M 系列全部处理完毕**，`08` 不再有未做的契约条目；仍待确认的是部署拓扑、`METADATA_SECRET_KEY` 轮换与 `RETURNING` 行序。

**阶段 5 更新**：`WorkspaceProfileSetting` 新增 `openlineage_retention_days`（store 字段 14 / v1 字段 4，`7870016`），与既有的 `external_url` 一起由管理员在 `/settings/general` 配置；store 与 v1 同步，生成产物与 API 文档一起提交（`buf format`/`buf lint`/`buf generate` 通过）。上文"仍待确认的是……`METADATA_SECRET_KEY` 轮换"随之关闭——该密钥已删除。

---

## 严重（Critical）

### P-C1. 审计日志会持久化未脱敏的私钥（proto 字段命名导致）
> **✅ 已修复（阶段 0）** · `89ef84a`：选择修脱敏侧而非改字段名——`isSensitiveAuditField` 现在精确匹配裸字段名 `sslkey`/`content`/`keytab`（归一化后比较，大小写与空白不敏感），另补 `sslcert`/`key`/`passwd`/`pwd`/`bearer`/`jwt`/`session`；`audit_test.go` 覆盖 `DataSource.sslCert`/`sslKey`/`gcpCredential`。**剩余**：`ssl_key`/`content` 的字段名未按建议重命名，脱敏仍是名字启发式而非按 `INPUT_ONLY` descriptor 结构化——新增敏感字段名仍可能漏网。

- **位置**：`proto/v1/v1/instance_service.proto:437,491`、`backend/api/v1/audit.go:197-208`
- **证据**：`string ssl_key = 7 [(google.api.field_behavior) = INPUT_ONLY];`、`message GCPCredential { string content = 1 [INPUT_ONLY]; }`；审计脱敏只按 key 名包含 `password|token|secret|credential|servicekey|apikey|api_key|accesskey|privatekey|private_key` 判断，`sslKey`/`content` 都不匹配。`CreateInstance`/`AddDataSource`/`UpdateDataSource` 都标了 `audit = true`，拦截器把 `protojson.Marshal(requestMessage)` 写入 `audit_log.payload` JSONB。`INPUT_ONLY` 只是注解，protojson 不会执行。
- **影响**：PEM 客户端私钥与 GCP 服务账号 JSON key 明文进入审计表；Kerberos `keytab`（`instance_service.proto:577`）同理。
- **修复**：按 protobuf descriptor（`INPUT_ONLY`/sensitive 注解）结构化脱敏，而非名字子串匹配；同时把 `ssl_key` 改名 `ssl_private_key`、`content` 改名 `private_key_json`，让现有启发式也能覆盖。
- **关联**：`02` H3（OpenLineage API key 因裸 `key` 未脱敏）、`04` A-H5。

---

## 高（High）

### P-H1. `metaxisdata/DatabaseMetadata` 资源类型被引用但从未声明
- **位置**：`proto/v1/v1/database_service.proto:248,288,300,319,440,490`、`lineage_service.proto:62,93`
- **证据**：8 处 `(google.api.resource_reference) = {type: "metaxisdata/DatabaseMetadata"}`，但没有任何 message 声明该 `(google.api.resource)`（只存在 `metaxisdata/User`、`Instance`、`Database`、`ManualSQL`）。
- **影响**：AIP-123/126 资源引用无法解析；`GetLineageRequest.guid` 文档写的是 `"instance_1;db2;schema3;table4"`，根本不是资源名。
- **修复**：要么声明资源（如挂在 `StoredMetadata` 上并给出 GUID pattern），要么去掉这些字段的 `resource_reference` 并把 GUID 记为不透明字符串。

### P-H2. `ListUsers` 声明了不存在的字段的方法签名
- **位置**：`proto/v1/v1/user_service.proto:43`
- **证据**：`option (google.api.method_signature) = "parent";`，而 `ListUsersRequest`（109-150）只有 `page_size`/`page_token`/`show_deleted`/`filter`。
- **影响**：生成的 method signature 与 REST query 参数映射错误，按签名调用会失败。
- **修复**：删除该 option，或真正补上 `parent`。

### P-H3. 公开 ExplainSQL API 用裸 `int32` 表示 `meta_type`
- **位置**：`proto/v1/v1/explain_sql_service.proto:20`、`backend/api/v1/explain_sql_service.go:503-515`
- **证据**：`int32 meta_type = 2; // store.MetaType`；v1 已有 `MetaType` 枚举（`database_service.proto:1593`），而 store 的 `MetaType` 多一个 `OPENLINEAGE = 100`（`store/meta.proto:29`），v1 没有。
- **影响**：公开契约依赖内部包枚举编号，无校验、无枚举名；store 的合法值在 v1 中不可表达。
- **修复**：改用 v1 `MetaType` 枚举并补 `OPENLINEAGE`（或显式 reserve）。

### P-H4. OpenLineage 列表用 offset 分页且无 next token
- **位置**：`proto/v1/v1/openlineage_service.proto:210-216,219-227,248-256`
- **证据**：三个 List 请求用 `int32 offset`，响应只有 repeated 字段、无 `next_page_token`；前端已直接传 offset（`frontend/src/api/openlineage.ts:26-42`）。
- **影响**：违反 AIP-158；客户端无法判断列表是否结束；并发插入下 offset 分页不稳定；与 User/Instance/Database/LLM 的 `page_size`/`page_token` 风格不一致。
- **修复**：改为 `page_token` + `next_page_token`。

### P-H5. `SearchMetadata` 返回客户端永远无法使用的 page token
- **位置**：`proto/v1/v1/database_service.proto:451-470`
- **证据**：`SearchMetadataResponse.next_page_token = 2`，但请求只有 `parent_guid_prefix`/`meta_type`/`search_str`，没有 `page_size`/`page_token`。
- **影响**：搜索结果静默截断且无法翻页，响应字段是死契约。
- **修复**：补请求分页字段（AIP-158）或删除 `next_page_token`。

### P-H6. v1 `StoredMetadata` 无法表达 store 持久化的 OpenLineage 行
- **位置**：`proto/v1/v1/database_service.proto:653-670`、`proto/store/store/database.proto:990-991`
- **证据**：v1 oneof 从 `task_metadata = 12` 跳到 `manual_sql_metadata = 15`，而 store 有 `openlineage_run_summary = 13`、`openlineage_task_summary = 14`；`store.MetaType_OPENLINEAGE` 会被实际写入 `meta_registry_resource.object_type`（`store/openlineage_run.go:217`、`openlineage_task.go:178`）。
- **影响**：任何能暴露 registry 行的 v1 方法（`ListMetadata`/`GetMetadata`/`SearchMetadata`）都无法表达 OpenLineage 元数据；v1 客户端不认识 `meta_type=100`。
- **修复**：补齐两个 oneof 分支并给 v1 `MetaType` 加 `OPENLINEAGE`，或在 v1 响应中显式过滤并文档化。

---

## 中（Medium）

- **M1. 审计行形状 store/v1 不一致**：store `AuditLog{int64 id}` vs v1 `AuditLog{string name}`（`store/audit_log.proto:28` vs `audit_log_service.proto:37`）；severity/status/metadata 也是两套命名。v1 没有稳定标识符。
  - **✅ 阶段 3 续（`792ca71`）**：store 的 `AuditSeverity`/`AuditStatus`/`RequestMetadata` 改名为 `AuditLogSeverity`/`AuditLogStatus`/`AuditRequestMetadata`，与 v1 同名；protojson 存的是枚举值名而非类型名，既有 JSONB 行解码不受影响。`AuditLog.id` 保留为 `audit_log` 表主键，并加注释说明 v1 把它表达为资源名 `{parent}/auditLogs/{id}`——append-only 审计行没有别的资源 ID，v1 的 `name` 就是稳定标识符，两者的差异是有意的。
- **M2. Engine/MetaType/DataSourceType 双份定义**：`v1/common.proto:13-40` 与 `store/common.proto:13-40` 完全同值；`MetaType` 在 `database_service.proto:1593` 与 `store/meta.proto:7` 重复（store 多 `OPENLINEAGE`）；`DataSourceType` 在 `instance_service.proto:541` 与 `store/instance.proto:213` 重复。`Engine` 有 28 个值，而产品只支持 MySQL/TiDB/PG。
  - **◐ 阶段 3 收尾（`ceb6a3d`）**：`Engine` 28→5 且两侧**值完全一致**（注释写明"must stay value-compatible"），`MetaType` 两侧都加了 `OPENLINEAGE`，`convertToEngine`/`convertEngine` 从 27 个 case 降到 5 个。**双份定义按设计保留**：v1 是公开契约、store 是 JSONB 行形状，两者必须能各自演进而数字对齐——真正的风险是"改了单侧导致 marshal 复制错位"，这也是阶段 3 收尾每次删除都同时保留两侧 field number 的原因（`database_convert.go` 的 `proto.Marshal`→`proto.Unmarshal` 依赖这一点）。`DataSourceType` 仍是两份同值定义。
- **M3. `GetOpenLineageDataset` 的 `namespace`/`name` 是死字段**：`openlineage_service.proto:230-232`，handler 在 guid 为空时直接拒绝（`openlineage_dataset.go:115-117`）。
- **M4. OpenLineage/API key 消息不是 AIP 资源**：`NamespaceMappingResource`（82-89）、`OpenLineageRunResource`（91-119）、`OpenLineageTaskResource`（121-144）、`APIKeyResource`（285-294）无 `(google.api.resource)`、无 `name`、用 `int64 id`，路径绑定 `{id}`；`-Resource` 后缀也不标准。
  - **✅ 阶段 3 收尾二（`c2a67e0`）**：四个消息改名为 `NamespaceMapping`/`OpenLineageRun`/`OpenLineageTask`/`APIKey`，各自声明 `metaxisdata/<Kind>` 与 `openlineage/namespaceMappings/{namespace_mapping}`、`openlineage/runs/{run}`、`openlineage/tasks/{task}`、`openlineage/apiKeys/{api_key}` pattern，首字段从 `int64 id` 改为资源 `name`（编号不回收：仍是字段 1）。`GetOpenLineageRun`/`GetOpenLineageTask` 改为 `{name=openlineage/runs/*}` / `{name=openlineage/tasks/*}`（GUID 是 `name` 的最后一段，`guid` 字段保留），`UpdateNamespaceMapping` 绑 `{mapping.name=...}`，`DeleteNamespaceMapping`/`RevokeAPIKey` 收资源 `name`。只读聚合消息（`OpenLineageDataset*Resource`）保持原样。
- **M5. List 方法名单数**：`ListDatabase`、`ListManualSQL`、`ListNamespaceMapping`、`ListAPIKey`（AIP-132 要求 `List<复数>`），与 `ListUsers`/`ListInstances` 不一致。
- **M6. `MetadataList` 命名违反"不用 xxxList"**：`database_service.proto:270-279`。
- **M7. `ListMetadata` 分页语义非标准且文档自相矛盾**：`database_service.proto:265` 注释"未指定 meta_type 时忽略 page_size，每类返回前 20 条"，而代码用 `page_size+1`；`page_token` 注释还错误地引用 `ListDatabases`。
- **M8. `ListInstanceDatabaseRequest.instance` 同时 optional 与 REQUIRED**：`instance_service.proto:256`；且该只读列表是 POST 自定义方法，`SyncInstanceResponse.databases` 与 `ListInstanceDatabaseResponse.databases` 重复。
- **M9. `UpdateDataSource` 是对父实例的 PATCH 自定义方法**：`instance_service.proto:108`，`DataSource`（428）没有资源名只有 `id`；路径变量标识父、body 标识子，语义模糊。
  - **◐ 阶段 3 续：经确认跳过**。完整资源化要新增 `DataSource.name = instances/{i}/dataSources/{id}` 并把 `Add/Remove/UpdateDataSource` 改成 AIP-133/135 的 `CreateDataSource`/`UpdateDataSource`/`DeleteDataSource`（连前端与集成测试），本轮范围不含，保留为已知遗留。
  - **✅ 阶段 3 收尾二（`513940f`）**：`DataSource` 声明 `metaxisdata/DataSource`（`instances/{instance}/dataSources/{data_source}`）并以 `name` 为标识（字段 1，取代 `id`）。三个自定义方法改为 `CreateDataSource`（AIP-133：`parent`、`data_source`、`data_source_id`、`validate_only`，返回 `DataSource`）、`UpdateDataSource`（AIP-134：`{data_source.name=instances/*/dataSources/*}` + `update_mask` + `validate_only`）与 `DeleteDataSource`（AIP-135：`name`，返回 `Empty`）。`UpdateInstance` **不再接受 `data_sources` mask**（返回 `InvalidArgument` 并指向子资源方法）；`mergeDataSources`/`mergeDataSource` 由纯函数 `patchDataSource` 取代——掩码外的字段保持存储值，因此由读取结果构造的更新不会清空密码或降级 TLS 校验。创建实例仍可携带数据源：客户端知道实例 ID 时用 `instances/{id}/dataSources/{ds}` 表达自定义 ID，否则由服务端生成。前端编辑表单改为 diff（新增 → Create、改动 → Update 只发改动字段、缺失 → Delete），实例详情页的自定义 ID 输入保留。
- **M10. `transformation` 在 v1 是 string，在 DB 是 JSONB 数组**：`lineage_service.proto:53` vs `LATEST.sql:249`；Go 侧 `json.Marshal([]model.Transformation)` 后再二次编码为字符串。
  - **✅ 阶段 3 续（`997ede9`）**：v1 改为 `repeated Transformation transformations = 11`（字段号沿用），新消息字段与存储模型一一对应；`convertColumnLineage` 直接构造、不再返回恒 nil 的 error，前端改吃结构化字段。JSONB 列与 `model.Transformation` 不变。
- **M11. `raw_payload` 三处类型不同**：store `bytes`、v1 `string`、DB `JSONB`（`openlineage_service.proto:116` vs `LATEST.sql:311`）。
  - **✅ 阶段 3 续（`997ede9` `b2e80ae`）**：`bytes` 那一份在零引用的 store `OpenLineageRun` 消息里，该消息与其兄弟 `OpenLineageTask` 一并删除；v1 的 `raw_payload` 保持 `string` 并文档化为"落库的 JSON 文本"。三处类型不再互相矛盾：DB 是 JSONB、Go 是 `[]byte`/`string` 承载同一段 JSON 文本、公开契约明确它是 JSON。
- **M12. store `ExplainSQLCache` 未使用且与列不匹配**：`store/explain_sql.proto:9` 的 `cache_type` 是魔法 int、`explanation_json` 是 string，而列是 JSONB；仓库用 `ExplainSQLCacheRow` 手写结构（`store/explain_sql.go:15-25`）。
- **M13. 多数 JSONB 列缺 "Stored as <message>" 注释**（**⏳ 仍未处理，阶段 6 有意排除**：全量补注释是独立的重写工作，`11-phase6-plan.md` 第六节列为后续项；A6 的增量 `0007` 只在迁移文件里写了列用途，未改 `LATEST.sql` 的表注释。本轮已确认的 JSONB→message 绑定以各 store 读取路径为准）：`instance.metadata`（LATEST.sql:130）、`db.metadata:147`、`meta_registry_resource.metadata:161`、`history:171`、`audit_log.payload:393`、`llm_provider_profile.metadata:408` 等；AGENTS.md 把这些注释当作 JSONB 与 store message 的绑定契约。
- **M14. `setting` 注释与枚举不一致**：`setting.proto:20` 有 `SCHEMA_TEMPLATE = 10`，SQL 注释（LATEST.sql:36-39）没有；且 `setting.value` 是 `text` 而非 JSONB。
- **M15. `UserType` 与 `PrincipalType` 对同一个值用不同名字**：v1 `USER=1` vs store `END_USER=1`（`user_service.proto:235` vs `store/user.proto:10-18`），DB CHECK 用 `END_USER`。
  - **✅ 阶段 3 续（`4036e1e`）**：v1 改名 `END_USER=1`，三处（v1 枚举、store `PrincipalType`、`principal.type` CHECK）一致；`convertToPrincipalType` 仍显式列出每个 case，filter 解析改为接受 `END_USER`。
- **M16. `GetCurrentUser` 标注免凭证却必然要求认证**：`user_service.proto:35-36` 有 `allow_without_credential = true`，但 handler 在无 user 时返回 `Unauthenticated`（`user_service.go:88-93`）。
- **M17. `annotation.proto` 的 `permission` 扩展从未使用，`AuthMethod.IAM` 是遗留**：~~`annotation.proto:11,16-22`；拦截器读取 `permission`（`auth.go:323`）但无人设置~~ —— **✅ 部分修复（阶段 0，`ec49607`）**：`permission` 现已被真实使用，写在 `instance_service.proto`（10 处 `metaxisdata.instances.write`）、`user_service.proto`（`metaxisdata.users.delete`/`.undelete`）、`database_service.proto`（`metaxisdata.databases.sync`）、`openlineage_service.proto`（`...namespaceMappings.write`/`...apiKeys.write`）、`llm_service.proto`（`metaxisdata.llm.profiles.write`）与新增的 `setting_service.proto`（`metaxisdata.settings.write`）；`acl_interceptor.go` 消费该注解。**剩余**：`AuthMethod.IAM` 仍是遗留枚举值；读方法未声明 permission；`method_signature = "parent"`（P-H2）等契约问题未动。
- **M18. Create/Update 在 `allow_missing`/`validate_only`/`<resource>_id` 上不一致**：`CreateInstanceRequest` 有 `instance_id`+`validate_only`，`CreateUserRequest` 都没有；`UpdateUserRequest` 有 `allow_missing`，其它 Update 没有。（阶段 0 为自助改密在 `UpdateUserRequest` 新增了 `string current_password = 3 [(google.api.field_behavior) = INPUT_ONLY]`，并重新生成了三处 buf 产物。）
  - **✅ 阶段 3 续：经确认不改契约，改为明确规则**。`validate_only` 只有在能验证外部连接时才有意义，因此只出现在 `CreateInstanceRequest`/`AddDataSourceRequest`/`UpdateDataSourceRequest`；`allow_missing` 只有 `UpdateUserRequest` 真正实现（`user_service.go:321`）；`<resource>_id` 只有 `CreateInstanceRequest` 需要（`name` 由服务端生成）。规则：**公开契约只声明已被实现的字段，不补空字段**。
- **M19. `CreateInstanceRequest.instance_id` 的字符类文档写错**：`instance_service.proto:195`（以及 `store/setting.proto:85-86`）写成 `/[a-z][0-9]-/`，实际意图是 `[a-z0-9-]`。
- **M20. `ListUsersRequest.filter` 文档描述产品没有的 project/服务账号模型**：`user_service.proto:141`。
- **M21. `GetLineage`/`GetLineageForContext` 的 HTTP 路径虚构了 `lineages/` 前缀**：`lineage_service.proto:19,24`，而 `guid` 文档是 `"instance_1;db2;schema3;table4"`。
- **M22. `BatchUpdateInstances` 接收 repeated UpdateRequest 而非 repeated 资源**：`instance_service.proto:277`；`BatchSyncInstancesResponse` 为空，部分失败不可见。
  - **✅ 阶段 3 续（`733b3e0`）**：`repeated UpdateInstanceRequest` 部分是**审查误报**——AIP-231 的 `BatchUpdate*` 就是要求 `repeated UpdateXxxRequest`，且 `BatchUpdateInstancesResponse` 本来就返回 `repeated Instance`。真正的缺陷是 `BatchSyncInstancesResponse` 为空且 handler 遇错即返回：现在它返回逐项 `BatchSyncInstanceResult{name, databases, error}`，每个实例独立处理，只有 `requests` 为空才让整个 RPC 失败。`BatchUpdateInstances` 仍是 fail-fast（响应已能表达成功项）。
- **M23. namespace mapping 的 create/update 缺 id/mask/资源名**：`openlineage_service.proto:266-279`，body 里的 `int64 id` 与路径 `{id}` 可能冲突。
  - **✅ 部分收敛（`733b3e0`）**：删掉请求顶层的 `id`（字段 1 移除），身份改由资源携带 `mapping.id`，HTTP 路径改为 `/v1/openlineage/namespaceMappings/{mapping.id}`，并新增 `update_mask`；handler 只接受 `namespace`/`instance_resource_id`/`database_name` 三个 path，store 按 mask 生成 SET 子句（空 mask 保持旧行为：namespace/instance_resource_id 为空则跳过、`database_name` 总是写以便清空）。**未做**：资源化本身（补 `name`、去 `-Resource` 后缀、`int64 id` 改字符串 ID）属 M4，仍未处理。

---

## 遗留/过宽表面（Legacy / over-broad）

> **阶段 3 收尾已收敛**（`ceb6a3d` `e0eab33` `722d3cb` `ddff264`）：每条的处置见行内标记；未能收敛的（表/列的 schema 变更、需要产品决策的 M 系列、因 BSR 限流回滚的注释修正）见本节末尾。
> **阶段 3 续已收敛**（`ee3c39b` `451cb78` `904fb09` `b2e80ae`）：project 表/列及其全部 API 表面、`service_name`/`granularity`、store 侧零引用的 openlineage 消息均已删除；`role` 表与 `principal.mfa_config` 列删除、`idp.type` CHECK 收窄。剩下的只有 M9/M4 的资源化重设计。

- **policy/IAM store 消息基本是死的**：`store.Policy`/`store.TagPolicy` 零 Go 使用；`TagPolicy.tags` 引用不存在的 `reviewConfigs/{...}`（`store/policy.proto:26`）。`IamPolicy`/`Binding` 被使用（9/5 处）但没有 v1 服务暴露；`policy` 表支持 `WORKSPACE/ENVIRONMENT/PROJECT` 却没有对应 API。
  - **✅ 部分收敛（`722d3cb`）**：`TagPolicy`/`EnvironmentTierPolicy` 及其 proto 文件删除。**更正**：`store.Policy` 并非零使用——`store/store.go:125`、`store/group.go:114`、`store/policy.go:29,160-173` 在读写的 `resource_type`/`type` text 列上使用 `Policy_Resource`/`Policy_Type`/`Policy_WORKSPACE`/`Policy_PROJECT`/`Policy_IAM`，故 `Policy` 消息保留（原报告只 grep 了 `storepb.Policy\b`，漏掉了这些 enum 常量）。`policy` 表本身保留。
- **project store 消息带 issue 时代字段**：`Project.issue_labels`（`store/project.proto:16`）与注释提到 issue 的 `postgres_database_tenant_mode`（:19）；`Label` 零使用；`project.data_classification_config_id`（LATEST.sql:95）属于不存在的数据分类功能。
  - **✅ 收敛（`722d3cb`）**：`Project`/`Label` 与 `store/project.proto` 整文件删除，`LATEST.sql` 的 `project` 表注释改为"无 Go 调用者、`Project` 消息已删除"。**表与 `data_classification_config_id` 列保留**：`db.project` 有外键指向 `project(resource_id)`，删表要先删列，属 schema 变更。
  - **✅ 阶段 3 续（`451cb78`）**：`db.project` 列与 `project` 表（含 `data_classification_config_id`）已删除，增量 `0.1.0004`；同时删掉 v1 的 project API 表面（见"阶段 3 续更新"）。
- **API 注释里泄漏 issue/task 词汇**：`DeleteInstanceRequest.force` 文档写"all open issues will be closed"（`instance_service.proto:221`）。
  - **◐ 改动已写好但回滚（本轮的 BSR 限流）**：正确的文案是"该实例的数据库会被移到默认 project 后再删除实例"（与 handler 的 `BatchUpdateDatabases` 到 `DefaultProjectID` 一致），改动已在本地完成，但因需要重新生成 buf 产物而遭遇远程插件限流，为保证生成产物与 proto 同步已回滚。**下一轮随任意 proto 变更一起提交**。
  - **✅ 阶段 3 续（`451cb78`）：该遗留以"删字段"收口**。`DeleteInstanceRequest.force` 连同它的"移到 default project"逻辑一起删除（字段号 2 与名字 `reserved`），原定的"改注释"方案作废——没有 project 就没有要迁移的数据库。
- **有 store 无服务**：`store.RolePermissions` 被读写（`store/role.go`）但没有 v1 RoleService；`GroupPayload`/`GroupMember` 被使用但没有 GroupService；`store.InstanceRole` 建模了角色管理（连接上限、密码过期、角色属性），`InstanceRoleMetadata` 零使用。
  - **✅ 收敛（`722d3cb` `ceb6a3d` `ddff264`）**：`RolePermissions`、`InstanceRole`、`InstanceRoleMetadata` 全部删除（`store/role.go` 阶段 3 已删；两个驱动里的 roles 采集本就是注释掉的代码）。**保留** `GroupPayload`/`GroupMember`（SSO 分组同步在用，仍无 GroupService）。
- **Setting 枚举大多未实现**：只有 `AUTH_SECRET`/`BRANDING_LOGO`/`WORKSPACE_ID`/`WORKSPACE_PROFILE`/`PASSWORD_RESTRICTION`/`ENVIRONMENT` 被 Go 引用；`WORKSPACE_APPROVAL`、`WORKSPACE_EXTERNAL_APPROVAL`、`APP_IM`、`WATERMARK`、`AI`、`SCHEMA_TEMPLATE`、`DATA_CLASSIFICATION`、`SEMANTIC_TYPES`、`SCIM` 都是审批/IM/水印/分类/SCIM 遗留；`WorkspaceProfileSetting` 还留着注释掉的 `announcement`/`database_change_mode` 和不存在的角色模型的 `maximum_role_expiration`。
  - **✅ 收敛（`e0eab33`）**：9 个未实现的名字与 `require_2fa`/`token_duration`/`maximum_role_expiration`/`enable_metric_collection`/两个注释字段全部 `reserved`；`backend/server/init.go` 不再写 `EnableMetricCollection: true`。`setting.value` 仍是 `text` 而非 JSONB（原样保留）。
- **IDP 表面超出产品**：`store/idp.proto` 支持 OAuth2/OIDC/LDAP、SCIM source、`FieldMapping` 注释指向不存在的 `principal.idp_user_info` 列（`store/idp.proto:95`，`LATEST.sql:18-31` 无此列）；`User.service_key`（INPUT_ONLY）、`recovery_codes`（无 field_behavior）、`UserProfile.source`（"Entra ID SCIM sync"）都是服务账号/2FA/SCIM 遗留。
  - **◐ 收敛（`e0eab33`）**：OIDC/LDAP 配置与枚举值删除，`FieldMapping` 注释改为"只在登录期映射、不落库"；`User.recovery_codes`、`UserProfile.source`、`GroupPayload.source` 删除；无调用者的 store IDP 写路径（Create/List/Update/Delete + `getConfigBytes`）一并删除。**保留**：`User.service_key`（`CreateUser` 的 SERVICE_ACCOUNT 分支返回生成的 access key，是活路径）、`User.phone`、`PrincipalType.SERVICE_ACCOUNT`、`idp.type` 的宽 CHECK、`principal.mfa_config` 列。
- **引擎/数据源表面远超产品**：`Engine` 28 个值；`DataSource` 带 MongoDB（`srv`/`replica_set`/`direct_connection`/`additional_addresses`）、Oracle（`sid`/`service_name`）、Redis sentinel、Databricks、CockroachDB、Spanner/Hive 等，以及 SSH 隧道、四类 IAM 凭据、`SASLConfig`/`KerberosConfig`、`DataSourceExternalSecret`（Vault/AWS/GCP 密钥管理）。另注意拼写错误 `SAECRET_TYPE_UNSPECIFIED`（`instance_service.proto:378`）。
  - **✅ 收敛（`ceb6a3d`）**：`Engine` 28→5（`MYSQL`/`POSTGRES`/`TIDB`/`MARIADB`/`OCEANBASE`，其余编号与名字 reserved）；上列多引擎/IAM/SASL/Vault 字段全删，`SAECRET_TYPE_UNSPECIFIED` 随 `DataSourceExternalSecret` 消失。**SSH 隧道保留**——`plugin/db/mysql/mysql.go:109`、`plugin/db/pg/pg.go:69` 真的在读 `ssh_host`/`ssh_port`/`ssh_user`/`ssh_private_key` 建隧道，`plugin/db/util/ssh.go` 是它的实现；SSL/`use_ssl`/`extra_connection_parameters` 同理保留。
- **死 v1 消息/枚举**：`RiskLevel`、`Position`、`Range`（`common.proto`）、`InstanceRoleMetadata`、`DependencyTable`（`database_service.proto`）零使用；store 的 `PageToken`、`OpenLineageTask`、`ExternalDataset`、`NamespaceMapping`、`ExplainSQLCache`、`InstanceRoleMetadata` 未使用。
  - **✅ 收敛（`ceb6a3d` `ddff264`，`ExplainSQLCache` 见阶段 3 `73901a1`）**：v1 `Position`/`Range`/`RiskLevel`/`InstanceRoleMetadata` 与 store `InstanceRoleMetadata` 删除。**更正**：`DependencyTable` 不是死消息——store 侧 PG 同步在填（`plugin/db/pg/sync.go:1776-1786`），v1 侧经 marshal 复制，两侧都保留；`PageToken` 是分页 token 载荷，保留。
  - **✅ 阶段 3 续更正（`b2e80ae`）**：上一轮把 store 的 `OpenLineageTask`/`ExternalDataset`/`NamespaceMapping` 写成"活 store"，那说的是 `store/*.go` 里的**手写** `*Message` 结构；这些 proto **消息**本身零引用，已随 `OpenLineageRun` 一起删除。`SchemaField` 是唯一被引用的（`store/external_dataset.go` 的 JSONB 列），保留。
- **未使用的 store 字段**：`DatabaseMetadata.backup_available`（`store/database.proto:14`，备份功能遗留）；`Instance.labels`（`store/instance.proto:42`）存在但 v1 `Instance` 无 `labels`，实例标签无法通过 API 读写。
  - **✅ 收敛（`ddff264` `ceb6a3d`）**：两个字段都删除。
- **阶段 3 收尾仍未动的遗留**：~~`DatabaseSchemaMetadata.service_name`（Oracle 概念）、`IndexMetadata.granularity`（注释写 ClickHouse）~~（**阶段 3 续已删**，`ee3c39b`）；~~`principal.mfa_config` 列（无 proto 消息、无读写）~~、~~`idp.type` 的宽 CHECK~~（**阶段 3 续已删/已收窄**，`904fb09`）；~~`project`/`role` 表结构与 `db.project` 列~~（**阶段 3 续已删**，`451cb78` `904fb09`）。**仍保留**：`policy` 表（WORKSPACE/IAM 行是活路径）、`setting.value` 的 text 类型（见待确认 3，已确认有意）、`GroupPayload`/`GroupMember`（SSO 分组同步在用）。~~M9/M4 的资源化~~（**阶段 3 收尾二已完成**）。

---

## 待确认

1. `metaxisdata/DatabaseMetadata` 是有意的不声明伪类型（给 GUID 用），还是声明被删了？这决定修复是"补声明"还是"删引用"。
   - **✅ 已关闭（阶段 3，`73901a1`）**：按"GUID 是不透明 `;` 连接标识、不应声明资源"处理，8 处 `resource_reference` 删除并把约定写进注释。
2. `principal.mfa_config` 文档指向不存在的 `MFAConfig`：MFA 是否在别处实现（手写结构），还是该列已死？删列前需确认。
   - **✅ 已关闭（阶段 3 收尾，`e0eab33`）**：2FA 整体是死功能——`User.recovery_codes`（v1）与 `require_2fa`（store）都零引用，已删除；仓库里没有任何手写 MFA 结构，`mfa_config` 列确认是死的（无读写、无消息）。**列本身保留**（删列是 schema 变更），`LATEST.sql` 注释已改为如实说明。
3. `setting.value` 是 `text` 而非 JSONB，`WorkspaceProfileSetting`/`PasswordRestrictionSetting`/`EnvironmentSetting` 是用 protojson 序列化进这个 text 列吗？若是，列类型与 JSONB 约定矛盾。
   - **✅ 已关闭（阶段 3 续，`d8ce592`）**：是 protojson，但 `setting.value` 是**多态**列，不能绑定到单一 store 消息：`WORKSPACE_PROFILE`/`PASSWORD_RESTRICTION`/`ENVIRONMENT` 存 protojson 对象，而 `AUTH_SECRET`/`BRANDING_LOGO`/`WORKSPACE_ID` 存裸字符串（本身不是合法 JSON）。改成 JSONB 就得把标量包成 JSON 字符串、并在每次读密钥时再剥一层引号，收益为零，因此**明确保留 `text`** 并把契约写进 `LATEST.sql` 的列注释——AGENTS.md 的"JSONB 列对应一个 store 消息"约定在此处是例外。
4. `store.ExplainSQLCache` 未使用：手写的 `ExplainSQLCacheRow` 是要取代它，还是应把 proto 接上？
   - **✅ 已关闭（阶段 3，`73901a1`）**：`ExplainSQLCache` 与 `store/explain_sql.proto` 删除，手写的 `ExplainSQLCacheRow` 是唯一形状。
5. `transformation`/`raw_payload`/`explanation_json` 的 string/bytes/JSONB 三重编码是否有意且有文档？
   - **✅ 已关闭（阶段 3 续，`997ede9` `b2e80ae`）**：`transformation` 改为结构化消息（DB 仍是 JSONB 数组），`raw_payload` 的 `bytes` 那一份随死消息删除、v1 文档化为 JSON 文本，`explanation_json` 本来就是 JSONB 列。三处不再互相矛盾，契约里都有注释。
6. `GetCurrentUser` 的 `allow_without_credential = true` 是 gateway 路径需要，还是单纯写错？
   - **✅ 已关闭（阶段 3，`73901a1`）**：确认是写错，注解删除；handler 本来就会在无用户时返回 `Unauthenticated`。
7. `policy`/`project`/`role`/`user_group` 表是否仍被任何授权路径使用（`IamPolicy`/`Binding` 有 Go 使用）？这决定能否删除。
   - **✅ 已关闭（阶段 3 收尾）**：`policy` 表的 WORKSPACE/IAM 行是活路径（`store/policy.go` 读写，`resources_type`/`type` 用 `Policy_*` enum），表与 `Policy` 消息都保留；`user_group` 表是活路径（SSO 分组同步 + `GroupPayload`）。`project`/`role` 表已无 Go 调用者，但 `db.project` 有外键指向 `project`，删表要先做 schema 变更（未做）。
   - **✅ 已执行（阶段 3 续，`451cb78` `904fb09`）**：`db.project` 列与 `project` 表、`role` 表已用增量 `0.1.0004`/`0.1.0003` 删除。`policy` 表的 PROJECT 行不再有任何生产者或消费者（用户/分组的 project 过滤器已删），但表本身与 WORKSPACE/IAM 行保留。
8. `ListInstanceDatabase` 是 POST 读接口且与 `SyncInstanceResponse.databases` 重复，是否为旧前端的兼容垫片？
   - **✅ 已关闭（阶段 3，`73901a1`）**：确认是空 stub，RPC 与其消息一并删除。
