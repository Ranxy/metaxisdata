# 04 · api/v1 服务实现

**范围**：`backend/api/v1/` 下除身份/审计（见 `02`）以外的全部 ConnectRPC handler：
- 数据面：`instance_service.go`、`database_service.go`、`database_history.go`、`database_convert.go`
- 血缘/接入/LLM：`openlineage_service.go`、`openlineage_handler.go`、`openlineage_dataset.go`、`lineage_service.go`、`llm_service.go`、`explain_sql_service.go`
- 公共：`common.go`（过滤器/分页/转换）

**结论**：这一层代码量最大（约 7000 行），问题也最密集。两个全局性问题贯穿所有文件：**没有任何授权检查**，以及 **CEL 过滤器到 SQL 的拼接存在注入**（4 处 handler + 2 处 store）。此外有 SSRF、缓存串租户、无界查询、N+1、分页失效、大量 Bytebase 遗留 stub。

**阶段 0 更新**：A-C2 ✅、A-H5 ✅、B-C1 ✅；A-C1 ◐、A-H1 ◐、B-C2 ◐、B-H5 ◐（写操作已限管理员，读路径与 URL 校验未做）。`validate_only` 的**内网地址限制经确认后主动放弃**（见 A-H1）。M1（类型断言 panic）、B-H1/B-H4 等性能与正确性条目**未处理**。

**阶段 1 更新**：A-H4 ✅、M1 ✅、M6 ✅（`20e284b`/`ff914ac`），另**删除**了遗留的 `table` 过滤器（`bb93ee0`，见 `03` S-H3）。B-H1/B-H4 等性能与正确性条目仍未处理（阶段 2）。

**阶段 2 更新**：B-H1 ✅、B-H2 ✅、B-H3 ✅、B-H4 ✅、M5 ✅、M11 ✅（见下，均 `8c34542`）；`common.go` 新增 `connectErrorForWrite`（store `common.Conflict` → `CodeAlreadyExists`，`ff9b22a`）。B-H5 的 `base_url` 校验、B-H6/B-H7/B-H8、M2/M3/M4/M6-M10、M12-M18 仍未处理。
**阶段 3 更新**：M2 ✅（`ListMetadata` 在 `meta_type` 为空时改用带 offset 的子层级查询，第 2 页不再重复第 1 页，`52213af`）、M3 ✅（`ListMetadataHistory` 的 `> limitPlusOne` off-by-one 与"全量后再切片"改为统一 `paginate`）、M9 ✅（`ListLLMProviderProfiles` 真正读取 `page_token`/返回 `next_page_token`，默认仍 50 条）、M15 ✅（store 的 not-found 现经 `ErrorMappingInterceptor` 映射为 `NotFound` 而非 500）；另补 P-H5 的 `SearchMetadata` 分页。四个 filter 翻译器（user/instance/database/audit）与 `parseToEngineSQL` 合并为 `api/v1/filter.go` 的单一实现（`7bfdfb6`），`getSubConditionFromExpr` 删除，`engine in [...]` 由内联字面量改为参数绑定。`ListInstanceDatabase` 空 stub 与 `DatabaseService.GetDatabase`（恒 `Unimplemented`）两个 RPC 删除（`73901a1`）；`instance_service.go`/`database_service.go`/`database_history.go` 按职责拆分（`4de82e3` `6be2262`）。**未处理**：M4（`GetSchemaString` 的 `GUIDPrefix` 前缀错误）、M6（血缘列表分页）、M7、M8、M10、M12、M16、M17（`parseStructuredResponse` 的 `"## ##"`）、M18 的字段名（密钥本身已改 AES-GCM）、B-H5 的 `base_url` 校验、B-H6/B-H7/B-H8。

**阶段 3 收尾更新**：`convertToEngine`/`convertEngine` 从各 27 个 case 降到 5 个（`ceb6a3d`）；`instance_convert.go` 删掉 8 个已无对应字段的转换函数（`convertDataSourceExternalSecret`/`convertV1DataSourceExternalSecret`/`convertV1DataSourceSaslConfig`/`convertDataSourceSaslConfig`/`convertDataSourceAddresses`/`convertAdditionalAddresses`/`convertV1AuthenticationType`/`convertV1RedisType`/`convertRedisType`），`mergeDataSource` 因 `additional_addresses` 删除退化为纯 `proto.Merge`，`UpdateDataSource` 的 `update_mask` 删掉 20 个已删字段分支（含整段 IAM 凭据合并逻辑）；`instance_service.go` 的 `DeleteInstance` 注释修正因 BSR 限流未落地（见 `10` 第五节）。`database_convert.go` 删掉 `convertPackageMetadata`/`convertStreamMetadata`/`convertTaskMetadata`/`convertLinkedDatabaseMetadata` 与对应 oneof case（`ddff264`）；`convertToUser` 不再填 `Profile.source`（`e0eab33`）。前端 `MetadataBrowserPage.vue` 的 `getMetadataName` 同步删掉三个已不存在的一分支。**新增未处理项**：`MARIADB`/`OCEANBASE` 现在是一等引擎，但 `isMySQLEngine`（`explain_sql_service.go:279`）只把 MYSQL/TIDB/MARIADB 视为 MySQL（原有不一致，本轮刻意未改行为）。

**阶段 3 续更新**：① v1 `Database.project` 字段、`ListDatabases` 的 `projects/{project}` parent、数据库与实例的 `project` filter、数据库 `exclude_unassigned` filter、`DeleteInstanceRequest.force` 与其移到 default project 的逻辑全部删除（`451cb78`），`DeleteInstance` 不再需要先查库列表。② `BatchSyncInstances` 不再 fail-fast（`733b3e0`）：响应改为 `repeated BatchSyncInstanceResult`（name/databases/error），逐项报告失败并继续，只有 `requests` 为空才整请求失败；`BatchUpdateInstances` 仍是 fail-fast（响应 `repeated Instance`）。③ `UpdateNamespaceMappingRequest` 的 id 移入资源（`mapping.id`，路径 `{mapping.id}`）并新增 `update_mask`（`733b3e0`），handler 校验 mask 只允许 namespace/instance_resource_id/database_name。④ `LineageRelation.transformation`（string，内含 JSON）改为 `repeated Transformation transformations`（`997ede9`），`convertColumnLineage` 不再返回 error，前端不再 `JSON.parse`。⑤ 删除无引用的 `DatabaseSchemaMetadata.service_name` 与 `IndexMetadata.granularity`（`ee3c39b`）。

**阶段 3 补遗更新**：① 血缘两个列表补分页（`dd6df51`）：`GetLineage`/`GetLineageForContext` 新增 `page_size`/`page_token` 与 `next_page_token`（默认 500、上限 5000），`GetLineage` 用同一个 offset 分别页化 source（`TargetGUID` 过滤）与 target（`SourceGUID` 过滤）两个列表。② ExplainSQL 的 `search_objects` 不再因为空 scope 而恒返回空，空关键字给出明确工具错误，搜索失败不再被丢弃；`fetchObjectsByGUIDs` 的 GUID/metadata 位置配对改为成对携带（`11943ae`）。③ `processBatchEvents` 不再「只要一条成功就 200」（`d562a50`）。④ LLM 写路径校验 `base_url` 并返回 `InvalidArgument`、profile 写入后失效 registry 缓存、`FetchLLMModels` 响应加 8MiB 上限（`10631e0`）。⑤ `isMySQLEngine` 纳入 OCEANBASE（`729db71`）。

**阶段 3 收尾二更新**：两个契约遗留落地。① **M4**（`c2a67e0`）：`NamespaceMappingResource`/`OpenLineageRunResource`/`OpenLineageTaskResource`/`APIKeyResource` 改名去掉 `-Resource` 后缀、声明 `metaxisdata/<Kind>` 与 `openlineage/...` pattern、以资源 `name` 取代 `int64 id`；`GetOpenLineageRun`/`GetOpenLineageTask` 绑 `{name=openlineage/runs/*}` / `{name=openlineage/tasks/*}`（GUID 是 `name` 最后一段，`guid` 字段保留），`UpdateNamespaceMapping` 绑 `{mapping.name=...}`，`DeleteNamespaceMapping`/`RevokeAPIKey` 收资源名。② **M9**（`513940f`）：`DataSource` 变成 Instance 的 AIP 子资源（`instances/{instance}/dataSources/{data_source}`），三个自定义方法改为 `CreateDataSource`（AIP-133：`parent`/`data_source`/`data_source_id`/`validate_only`）、`UpdateDataSource`（AIP-134：`{data_source.name=...}` + `update_mask`）与 `DeleteDataSource`（AIP-135，返回 Empty）；`UpdateInstance` 不再接受 `data_sources` mask（返回 `InvalidArgument` 并指向子资源方法），原来的 `mergeDataSources`/`mergeDataSource` 被纯函数 `patchDataSource` 取代——**只写掩码内字段**，所以由读取结果构造的更新不会清空密码或降级 TLS 校验（`instance_data_source_test.go` 断言）。创建实例仍可携带数据源，客户端知道实例 ID 时用 `instances/{id}/dataSources/{ds}` 表达自定义 ID，否则服务端生成。真实 server 的生命周期集成测试覆盖创建/重复 ID/非法 ID/建 ADMIN/掩码更新/删 ADMIN/删除/not-found。③ 测试补口（`c162bc0`）：审计 helper 与 `debug_interceptor`（>10240 字符 connect error 的 `[TRUNCATED]` 截断、短错误与普通错误透传）。


**阶段 3 续更正**：上一段收尾更新里 `DeleteInstance` 注释修正因 BSR 限流未落地的事项不再成立——`DeleteInstanceRequest.force` 与其注释本轮随 project 一起删除（`451cb78`）。

**阶段 5 更新**：`setting_service.go` 的 `UpdateWorkspaceProfileSetting` 掩码新增 `openlineage_retention_days`（拒绝负数），`external_url` 保存时去掉尾部 `/`（`7870016`）。`common.Obfuscate` 回滚为 XOR 后，**M18 重新成立**（LLM/实例凭据仍是同库密钥 XOR；上文"密钥本身已改 AES-GCM"的括注作废），M18 的待确认"`profile.Secret`（XOR seed）如何生成/轮换"回到"与 JWT 共用数据库 `AUTH_SECRET`"这一答案。

---

# A. 数据面：Instance / Database / History

## 严重（Critical）

### A-C1. 所有数据面 handler 都没有授权/归属校验
> **◐ 部分修复（阶段 0）** · `ec49607`：`InstanceService` 的 10 个写方法（`CreateInstance`/`UpdateInstance`/`DeleteInstance`/`UndeleteInstance`/`SyncInstance`/`BatchSyncInstances`/`BatchUpdateInstances`/`AddDataSource`/`UpdateDataSource`/`RemoveDataSource`）与 `DatabaseService.SyncDatabase` 已在 proto 声明 `permission`（`metaxisdata.instances.write`/`metaxisdata.databases.sync`），由 `ACLInterceptor` 强制 workspaceAdmin；`create`/`add_data_source` 等带的 `validate_only` 分支因此同样受限。**剩余**：所有读路径（`GetInstance`/`ListInstances`/`ListDatabases`/`ListMetadata`/血缘等）未声明 permission，仍是"任意已认证用户"；也没有实例级的归属范围校验。

- **位置**：`instance_service.go:50-57` 及全部 handler；`database_service.go` 同理
- **证据**：`GetInstance` → `getInstanceMessage(ctx, s.store, req.Msg.Name)`，没有任何 user/permission 检查；`Create/Update/Delete/SyncInstance`、`Add/Update/RemoveDataSource`、以及 `DatabaseService` 全部方法都一样。`common.UserContextKey` 被拦截器写入但这里从不读取。
- **影响**：任意已认证用户（含只读用户、刚自助注册的攻击者）可读取任意实例（含数据源配置）、创建/删除实例、触发同步（导致服务端主动外连）、轮换数据源凭证。
- **修复**：恢复/实现 ACL 拦截器，或在每个 handler 内显式校验权限与实例范围；补充"无权限用户应得 `CodePermissionDenied`"的测试。

### A-C2. CEL 过滤器 SQL 注入
> **✅ 已修复（阶段 0）** · `3321801`
> - `instance_service.go` 的 title/resource_id/host/port 与 `database_service.go` 的 name（以及当时还在的 table）全部改为 `LIKE $n` 参数绑定，值经 `likePattern()` 转义 `%`/`_`（`api/v1/common.go` 的 `likePatternEscaper`，与 `store/meta_resource.go` 既有实现同源）；`label` 的 key 也改成 `db.metadata->'labels'->>$n = ANY($m)` 并把 key 一并绑定。（table 过滤器已在阶段 1 删除，`bb93ee0`。）
> - 顺带统一了大小写行为（`table.matches` 只小写 pattern 不小写列名的老问题已随该过滤器删除，见"低"节）。
> - 守卫测试：`backend/api/v1/filter_injection_test.go` + `TestLikePatternEscapesWildcards`。

- **位置**：`instance_service.go:152,154,156`、`database_service.go:815,874,880`
- **证据**：
  ```go
  return "LOWER(instance.metadata->>'title') LIKE '%" + strings.ToLower(strValue) + "%'", nil
  return "ds ->> '" + variable + "' LIKE '%" + strValue + "%'", nil
  return "LOWER(db.name) LIKE '%" + strValue + "%'", nil
  WHERE t->>'name' LIKE '%` + strValue + `%')`
  fmt.Sprintf("db.metadata->'labels'->>'%s' = ANY($%d)", labelKey, ...)
  ```
  CEL 双引号字符串允许包含 `'`。
- **影响**：`name.matches("x' OR '1'='1' -- ")` 可构造合法注入 SQL，绕过归属谓词；只读事务下仍可通过布尔/子查询做数据抽取。`%`/`_` 也未转义。
- **修复**：模式参数化（`LIKE $n`，值为转义后的 `"%" + v + "%"`），label key 也用 `->>$n`；复用 `store/meta_resource.go:83` 已有的 `likePatternEscaper`。

## 高（High）

### A-H1. `validate_only` 造成 SSRF / 内网探测
> **◐ 部分修复（阶段 0）** · `ec49607`：`CreateInstance`/`AddDataSource`/`UpdateDataSource` 等 `validate_only` 路径已要求 workspaceAdmin，因此不再对任意已认证用户开放。
> **内网地址限制已按产品决策主动放弃**：自托管场景下用户连接的数据库本来就在内网，加私网 deny 会破坏核心功能；因此本轮不加 allow/deny 列表，仅保留管理员权限约束。**剩余**：driver 原始错误（含 `dial tcp <内网 IP>:<port>`）仍会透传给管理员调用方，未脱敏。
> **✅ 已修复（阶段 6）** · `f112e5c`：`validate_only`/数据源错误里的原始驱动错误（含内网 `dial tcp ip:port`）改为只回通用 `InvalidArgument`，细节写日志。

- **位置**：`instance_service.go:283-308,605-627,805-825`
- **证据**：`s.dbFactory.GetDataSourceDriver(ctx, instanceMessage, ds, ...)` 后 `connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid datasource %s", ...))`，把 driver 原始错误返回给调用方。
- **影响**：任意已认证用户可让服务端连接任意 `host/port`（含 SSH host），并从错误信息读出 `dial tcp 10.0.0.5:6379: connect: connection refused` 之类结果 → 内网端口扫描 + 服务指纹。
- **修复**：要求实例管理权限；对私网/链路本地/回环地址做 allow/deny；返回脱敏后的通用错误。

### A-H2. `DiffMetadata` 默认 `source_time = now`，文档承诺的"最早版本"无法实现
> **✅ 已修复（阶段 6）** · `f58c387`：`source_time` 未设置时改取最早可用版本（与 proto 文档一致），不再用 `now`。
- **位置**：`database_service.go:970-976`
- **证据**：`if asOf != nil { asOfTime = asOf.AsTime() } else { asOfTime = time.Now() }`，而 proto 注释写的是 "If not set, uses the earliest available version"（`database_service.proto:493-495`）。
- **影响**：不传 `source_time` 时源与目标都是 now，diff 恒为空，返回 "No changes detected."。
- **修复**：`source_time` 为空时查询最早历史行（`OrderDesc` + `Limit=1`，或新增 earliest 查询）；只有 `target_time` 默认 now。

### A-H3. `DiffMetadata` 只重建 4 类对象，多数变更不可见
> **✅ 已修复（阶段 6）** · `f58c387`：`rebuildDatabaseObjects` 覆盖 differ 已支持的其余对象类型（物化视图/序列/枚举/扩展等），并把相应 Changes 计入 summary。
- **位置**：`database_service.go:1025-1091,1095-1102,1106-1176`
- **证据**：`rebuildDatabaseObjects` 只取 `TABLE/VIEW/FUNCTION/PROCEDURE`；`buildDiffSummary` 也只统计 table/view/function/schema。
- **影响**：物化视图、序列、枚举类型、package、external table、stream、task、event、extension、event trigger、以及 schema 的 owner/comment/skip_dump 变更全部不可见；仅序列或 MV 变化会返回"无变更"和空 DDL，而底层 differ 是支持这些类型的。
- **修复**：重建全部已存储对象类型（或 `ObjectType IN (...)` 批量查询），并把相应 Changes 计入 summary。

### A-H4. `UpdateInstance(data_sources)` 会清空已存密钥并降级 TLS 校验
> **✅ 已修复（阶段 1）** · `20e284b`：`data_sources` 分支改为按 ID 合并——`mergeDataSources` 以请求列表为准决定成员（缺席的 ID 仍会被删除，符合 repeated 字段的 update_mask 语义），但每个同名 ID 的条目通过 `mergeDataSource` 叠加到 store 里的现有条目上。实现用 `proto.Merge`：它只复制"已设置"的 proto3 标量，因此请求没带（或为空）的字段——包括读取路径从来不返回的密码/SSL/SSH 私钥/IAM 凭据——保留库中值；唯二的例外是 repeated 字段 `additional_addresses`（`proto.Merge` 是追加语义，请求带值时先清空）与 store-only 字段（`verify_tls_certificate`、`cluster`、`role_arn`、Vault TLS 等，因为从未被覆盖而天然保留）。**语义边界**：proto3 无法区分"未发送"与"发送了空串"，所以本接口**无法把某个非密钥字段清空**，需要清空时应删除后通过 `AddDataSource` 重建。守卫测试 `TestMergeDataSourcePreservesUnreturnedFields`/`TestMergeDataSourceOverlaysProvidedValues`/`TestMergeDataSourcesKeysByID`。

- **位置**：`instance_service.go:405-413,1307-1353`（修复前行号）
- **证据**：`convertV1DataSource` 不填充 `verify_tls_certificate`、`cluster`、`role_arn`/`external_id`、Vault TLS 等 store-only 字段；而读取路径故意不返回 password/SSL（`instance_service.go:1066`）。
- **影响**：Get → 编辑 → Update 的常规往返会持久化空密码/空 SSL key，并把 `verify_tls_certificate` 重置为 false（TLS 校验降级），静默破坏或削弱既有连接。
- **修复**：按 data source ID 合并到现有 store metadata，保留未回传字段与密钥；不要整体替换列表。

### A-H5. 审计脱敏遗漏 GCP `content`、`sslKey`、Kerberos `keytab`
> **✅ 已修复（阶段 0）** · `89ef84a`：`isSensitiveAuditField` 补齐裸字段名 `content`/`sslkey`/`sslcert`/`keytab`（另加 `key`/`passwd`/`pwd`/`bearer`/`jwt`/`session`）；`audit_test.go` 增加 `DataSource` 凭据与 `CreateAPIKeyResponse` 的回归测试。**剩余**：仍是名字启发式，未改为按 proto `INPUT_ONLY`/descriptor 结构化脱敏。

- **位置**：`backend/api/v1/audit.go:197-208`（由这些 handler 的 `audit: true` 触发）
- **影响**：GCP 服务账号 JSON、TLS 私钥、Kerberos keytab 明文落入 `audit_log.request`。
- **修复**：补充 `content`/`sslkey`/`keytab`/`ssl_cert` 标记，或按 proto 注解结构化脱敏；加回归测试。
- **关联**：`02` 的 H3（OpenLineage API key）。

## 中（Medium）

- **M1. 未检查类型断言 / `AsLiteral()` panic**：`instance_service.go:78,81,84,91,98,106,109,112,141`；`database_service.go:741,808,833,865`。`name == 123`、`engine in [1]`、`name.matches(ident)` 都会 panic → 500。（**✅ 已修复（阶段 1）** · `ff914ac`：所有取值改走带检查的 helper（`filterString`/`filterBool`/`filterStringList`/`matchArgs`），`getVariableAndValueFromExpr` 现在返回 error 并在缺少变量或字面量时报错，四个过滤器（user/instance/database/audit）统一返回 `InvalidArgument`；`exclude_unassigned` 也不再静默忽略非布尔值。守卫测试 `TestFilterParsersRejectMistypedOperands`（12 个用例）。）
- **M2. `ListMetadata` 在 `meta_type` 为空时翻页失效**：`database_service.go:200-231`；`FindSubLevelMetaRegistryResourceMessage` 只有 `LimitPreObjectType`，没有 offset（`store/meta_resource.go:51-55,775`），但仍会返回 `next_page_token`，第 2 页与第 1 页相同。
- **M3. `ListMetadataHistory` 全量加载 + 分页 off-by-one**：`database_history.go:48-51,59-72`，查询无 limit/offset，之后 `if len(events) > limitPlusOne` 应为 `>=`，否则返回 `page_size+1` 条且无 token。
- **M4. `GetSchemaString` 序列查询前缀错误**：`database_service.go:345` 用 `common.GUIDPrefix`（按 `"."` 切分，`common/guid.go:33-38`），而所有 GUID 用 `";"` 拼接 → 前缀恒为空，PG 的 `ALTER SEQUENCE ... OWNED BY`/identity DDL 丢失。 —— **✅ 已修复** · `6f2b63d`：`common.GUIDPrefix` 改为按 `MetaGUIDSplit`（`";"`）取最后一段之前的前缀，`GetSchemaString`（现位于 `database_metadata.go`）拿到的序列前缀不再恒空。
- **M5. `CreateInstance` 不校验 environment/engine**：`instance_service.go:277,947-971`，与 `UpdateInstance`（`395-401`）不一致，可创建引用不存在环境的实例。 —— **✅ 已修复（阶段 6）** · `48dbecb`：`CreateInstance` 补上 environment 存在性与 engine 合法性校验（与 `UpdateInstance` 对齐）。
- **M6. `UpdateInstance(data_sources)` 可删掉 admin 数据源**：`instance_service.go:405-413,338-351`；store 的 `validateDataSources`（要求恰好一个 ADMIN）在更新路径未被调用。**✅ 已修复（阶段 1）** · `20e284b`：`checkInstanceDataSources` 现在统计 ADMIN 数量，`!= 1` 即返回 `InvalidArgument`（同时覆盖 create 与 update 两条路径）。
- **M7. 批量 RPC 部分成功无逐项结果**：`instance_service.go:536-555,561-570`；第 N 项失败时前 N-1 项已提交，客户端只拿到一个错误；也未限制 1000 条上限。
- **M8. `ListDatabase` N+1 且可能 nil deref**：`database_service.go:1178-1195`；每行一次 `GetInstanceV2`（含完整 metadata），`convertInstanceMessageToInstanceResource` 无 nil 检查，而 `GetInstanceV2` 未命中返回 `(nil, nil)`。 —— **✅ 已修复（阶段 6）** · `a7ea214`：`ListDatabases` 先取 `distinctInstanceIDs` 再 `ListInstances(ResourceIDs)` 一次取齐；`convertToDatabase` 改为纯函数，实例缺失返回 `Internal` 而不是解引用 nil。
- **M9. `SyncDatabase` 泄漏非 Connect 错误**：`database_service.go:55-58` + `common.go:394-422`，缺失数据库/大小写冲突返回 `CodeUnknown` 而非 `NotFound`/`AlreadyExists`。 —— **✅ 已修复（阶段 6）** · `48dbecb`：`SyncDatabase` 错误经 `common.Code` 映射（缺失→`NotFound`、冲突→`AlreadyExists`）。
- **M10. `CreateManualSQL` 不校验 `manual_sql_id`**：`database_service.go:423-425,682-698`，含 `/` 的 ID 会生成无法被 `parseManualSQLName` 解析的资源名，对象从此不可寻址。 —— **✅ 已修复（阶段 6）** · `48dbecb`：`CreateManualSQL` 校验 `manual_sql_id` 合法（`common.IsValidResourceID`），非法返回 `InvalidArgument`。
- **M11. 历史变更检测遗漏大量字段**：`database_history.go:244-281,579-631,754-763,1010-1017`，如列 `generation`/`identity_*`、索引 `key_length`/`descending`/`opclass_*`、外键 `match_type`、表 `triggers`/`rules` 等，真实变更被报成"无变化"。 —— **✅ 已修复（阶段 6）** · `f58c387`：历史变更检测补齐列 `generation`/identity、索引 `key_length`/`descending`/`opclass`、外键 `match_type` 等字段（按 differ 结构逐项对齐）。
- **M12. `rebuildSchemaContents` 吞掉重建错误并回退到当前元数据**：`database_service.go:1095-1102`，会给出"看似合理但错误"的历史 diff。 —— **✅ 已修复（阶段 6）** · `f58c387`：`rebuildSchemaContents` 重建失败不再静默回退当前元数据，改为返回错误。

## 低（Low）／代码质量

- `SearchMetadata` 永远不返回 `next_page_token`，硬编码 50 条（`database_service.go:273-304`），而响应里有该字段。
- `meta_type` 未设置时被当成 0 传给 store，导致 `NotFound` 而非解析或 `InvalidArgument`（`database_service.go:252,327`）。
- ~~`table.matches()` 把 pattern 转小写却不 `LOWER()` 列名（`database_service.go:870,880`），`table.matches("Foo")` 永远匹配不到。~~ **✅ 阶段 1 已随 `table` 过滤器整体删除**（`bb93ee0`，见 `03` S-H3）。
- `label` 过滤器文档支持 `in`，实际只支持 `==`；含 `:` 的 label 值无法使用（`database_service.go:807-815,884-891`）。
- `AddDataSource` 重复的类型检查（`instance_service.go:579-581` vs `629-631`）；重复数据源返回 `CodeNotFound` 而非 `CodeAlreadyExists`。
- `CreateInstance` 在请求路径里做完整 schema sync，且打开了一个多余的 driver 并丢弃其错误（`instance_service.go:314-335`）。
- `database_convert.go:159-346` 大量 `data, _ := proto.Marshal(meta); _ = proto.Unmarshal(data, result)` —— 依赖 storepb 与 v1pb 字段号完全一致，任何分叉都会静默丢字段。**建议改为显式字段映射或至少返回错误 + golden 测试。**
- `pluralize` 生成 "indexs"（`database_history.go:558-566`）。
- `common.go:338-360` page token 里的 `Limit` 被写入但读取时被忽略，客户端不重复传 `page_size` 会得到重叠/跳页。

## 死代码与遗留债务（数据面）

- `ListInstanceDatabase` 是空 stub，整段实现被注释，仍有 `TODO(implement when database is ready)`（`instance_service.go:233-266`）；proto 里 `instance` 同时标 `optional` 与 `REQUIRED`。
- `DatabaseService.GetDatabase` 直接返回 `CodeUnimplemented`（`database_service.go:50-52`）。
- 两处被注释的 `licenseService.IsFeatureEnabledForInstance`（`instance_service.go:632-634,681-685`）。
- `database_history.go:151-155` 死分支：在外层已要求相等的前提下再判断不等。
- `buildInstanceName`/`buildEnvironmentName`（`instance_service.go:909-924`）与 `common.FormatInstance`/`FormatEnvironment` 重复。
- `InstanceService.stateCfg`、`DatabaseService.stateCfg`/`dbFactory` 赋值后从不读取。
- `parseListInstanceFilter` 与 `getListDatabaseFilter` 是约 120 行的近重复 CEL→SQL 翻译器（阶段 0 已统一参数化写法、阶段 1 已统一类型检查 helper，但**未合并**，重复仍在）；store 通过 `strings.Contains(filter.Where, ...)`/`hasHostPortFilter` 反推 join 的脆弱做法，其中 `ds.metadata->'schemas'` 一路已随 `table` 过滤器删除（`bb93ee0`），`hasHostPortFilter` 仍在。
- `common.go:49-185` 的 `ParseFilter`/`normalizeFilter`/`Expression` 是旧过滤器解析器，无引用。
- `convertStoredMetadataMessage` 静默丢弃 store-only 的 `openlineage_run_summary`/`openlineage_task_summary`（`database_convert.go:73-74`）。
- `convertRedisType` 把 `REDIS_TYPE_UNSPECIFIED` 映射为 `STANDALONE`（`instance_service.go:1293-1305`），语义错误。

---

# B. Lineage / OpenLineage / LLM / ExplainSQL

## 严重（Critical）

### B-C1. 明文 ingestion API key 落审计日志并可通过审计 API 读取
> **✅ 已修复（阶段 0）** · `89ef84a`：裸字段名 `key` 现被精确匹配脱敏（同一改动也覆盖 `sslKey`/`content`/`keytab` 等），并为 `CreateAPIKeyResponse` 加了回归测试。**剩余**：`ListAuditLogs` 仍原样返回历史 `response`，此前已写入的明文 key 需按数据保留策略清理。
> **✅ 已修复（阶段 6）** · `2208521`：`ListAuditLogs` 返回前对历史 `response`/`request` 再跑一次脱敏，清掉阶段 0 之前落库的明文 ingestion key。

- **位置**：`proto/v1/v1/openlineage_service.proto:66-75,300-304`、`backend/api/v1/audit.go:114-134,179-208`、`audit_log_service.go:185-202`
- **证据**：`CreateAPIKey` 带 `audit = true`；审计拦截器把**响应**也写入；`CreateAPIKeyResponse.key` 的 protojson 字段名就是 `"key"`，不在脱敏标记列表中；`ListAuditLogs` 返回 `Response`。
- **影响**：一次性明文 key 持久化且可被读取，"只返回一次"的承诺失效，轮换无意义。
- **修复**：把裸 `key` 加入脱敏；或对"签发密钥"类方法不记录响应体。
- **关联**：`02` 的 H3。

### B-C2. 全层无授权：任意已认证用户可读任意实例血缘、读取全部 run/raw_payload、创建/吊销全局 ingestion key
> **◐ 部分修复（阶段 0）** · `ec49607`：`CreateAPIKey`/`RevokeAPIKey` 已声明 `metaxisdata.openlineage.apiKeys.write`、namespace mapping 的三个写方法声明 `...namespaceMappings.write`，由 `ACLInterceptor` 强制 workspaceAdmin。**剩余**：`ListAPIKey`、`GetLineage`、OpenLineage 全部读接口（run/dataset/event/raw_payload）仍未声明 permission，仍是"任意已认证用户可读"；key 也未绑定 owner/namespace（见 B-H8）。

- **位置**：`backend/server/grpc_routes.go:85`、`lineage_service.go:41-90`、`openlineage_service.go:202-242`
- **证据（修复前）**：`GetLineage` 只接收 GUID 并直接 `FindColumnLineageMessage{TargetGUID: &req.Msg.Guid}`；`CreateAPIKey`/`ListAPIKey`/`RevokeAPIKey` 无任何权限检查；`apiv1.NewACLInterceptor` 符号已不存在。
- **影响**：血缘、OpenLineage 数据、原始 payload 全部跨实例可读；可铸造或吊销他人的 key。
- **修复**：恢复授权拦截器并声明 `permission`；key 的创建/吊销限定到 owner 或管理员。

## 高（High）

### B-H1. OpenLineage 数据集接口无界扫描全表并解析全部 payload
> **✅ 已修复（阶段 2）** · `8c34542`：两个数据集端点（`ListOpenLineageDatasets`/`GetOpenLineageDataset`）改为只读最近 5000 个 run（`defaultOpenLineageRunLimit`，按 `event_time DESC`），解析器换成 `NewRequestScopedResolver`（按请求 memoize dataset preview），聚合与详情合并为一次遍历。**未做**：SQL 侧聚合与保留清理任务——经确认数据量假设与内存上界（5000 条 payload）已足够，且 `openlineage_run` 属可审计数据，不自动删除。注意数据集统计因此只覆盖最近 5000 个事件。

- **位置**：`openlineage_dataset.go:40,119`、`store/openlineage_run.go:299,306-311`
- **证据**：`ListOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{})`；store 仅在 `Limit != nil` 时追加 LIMIT，且始终 select `raw_payload`；随后每个 run 都 JSON 解析。
- **影响**：内存/CPU 随事件总量线性增长，无上限；前端传的 `pageSize: 500` 只作用于内存聚合，不进入查询。
- **修复**：服务端聚合（SQL），或至少把 limit/offset 与时间窗下推到查询，并增加保留策略。

### B-H2. 数据集解析 N+1（每个 dataset 一次 namespace 映射查询 + 全实例表扫描）
> **✅ 已修复（阶段 2）** · `8c34542`：读端点改用 `openlineageplugin.NewRequestScopedResolver`，preview 按 `(namespace, name)` memoize，`ListInstancesV2` 在同一个 resolver 内只查一次（`listInstances` lazy + 缓存）。采集路径仍用 `NewResolver`（不缓存），避免长生命周期 resolver 返回陈旧结果。

- **位置**：`openlineage_dataset.go:407`、`plugin/openlineage/resolver.go:80,110`
- **证据**：`ResolveDatasetPreview` 每次都执行 `ListInstancesV2(ctx, &store.FindInstanceMessage{})`（读事务 + 全表扫描），无 memoization。
- **影响**：每次请求 `O(runs × datasets)` 次 DB 往返。
- **修复**：按 `(namespace,name)` 在请求内做一次解析缓存；把 `ListInstancesV2` 提到循环外。

### B-H3. `GetOpenLineageDataset` 对同一批 run 解析两遍
> **✅ 已修复（阶段 2）** · `8c34542`：`buildOpenLineageDatasetDetail` 重写为单次遍历，同时产出目标数据集的聚合与详情；`aggregateOpenLineageDatasets`/`findOpenLineageDatasetAggregate` 不再在详情路径上重复执行（列表端点仍用前者）。顺带删掉 `matchDatasetInRun` 里把 task GUID 与 dataset GUID 比较的死分支。

- **位置**：`openlineage_dataset.go:226,238`
- **影响**：在已经无界的接口上再翻倍 JSON 解析与解析工作量。
- **修复**：一次遍历同时产出聚合与详情。

### B-H4. ExplainSQL 缓存 key 不含实例/GUID/provider → 跨实例串用
> **✅ 已修复（阶段 2）** · `8c34542`：缓存 key 改为 `explainSQLCacheKey(identity, scopePrefix, provider, model)`——identity 是 `sql:<sha256>` 或 `meta:<metaHash>`，并叠加 scope（实例/对象前缀）与 provider/model；增量 `0.1.0001` 增加 `scope` 列并落库。TTL 7 天，`expired` 不再需要显式设置（过期行直接不返回）。**行为变化**：缓存命中现在要求至少一个启用的 LLM profile，因为 provider/model 参与 key；禁用全部 provider 后旧缓存不再返回。

- **位置**：`explain_sql_service.go:505,524`、`store/store.go:129-137`、`LATEST.sql:417-427`
- **证据**：`cacheKey = "sql:<sha256(sqlText)>"`；`"meta:<metaHash>"`，而 `MetaHash` 只是 `StoredMetadata` 的 sha256，不含 GUID/instance；`explain_sql_cache` 无 scope 列；`GetExplainSQLCache` 只按 `cache_key` 查。
- **影响**：两个实例中定义相同的表（或不同实例下相同的 SQL 文本）共享缓存；返回的解释里嵌入了另一个实例的列名/DDL/对象名，跨实例信息泄露；切换 provider 后仍返回旧 provider 的结果。
- **修复**：key 纳入 instance/GUID/scopePrefix 与 provider/model；增加 scope 列。

### B-H5. LLM provider key 可被外带（无归属校验 + base_url 未校验 → SSRF）
> **◐ 部分修复（阶段 0）** · `ec49607`：`CreateLLMProviderProfile`/`UpdateLLMProviderProfile`/`DeleteLLMProviderProfile` 与 `FetchLLMModels` 已声明 `metaxisdata.llm.profiles.write`，需 workspaceAdmin，任意已认证用户不再能改 `base_url` 或触发外带。**剩余**：`base_url` 本身仍未校验（https/私网），管理员误配或恶意管理员仍可指向内网；URL 变更后"回退使用已存密钥"的行为未改。

- **位置**：`llm_service.go:80-170,200-204`、`component/llm/fetcher.go:26-34`、`component/llm/agent.go:161,178`
- **证据**：`FetchLLMModels` 在请求未带 api_key 时回退到 `prof.Metadata.ApiKeyEncrypted`，然后 `GET baseURL + "/v1/models"` 并携带 `Authorization: Bearer <key>`；`Create/UpdateLLMProviderProfile` 对 `BaseUrl` 无任何校验、无管理员/归属检查。
- **影响**：任意已认证用户可把某个 profile 的 `base_url` 指向自己的服务器，再触发 `FetchLLMModels`/`ExplainSQL`，让服务端把该 profile 的存储密钥发给自己；同时构成任意出站请求（如 `http://169.254.169.254/…`）。
- **修复**：profile 管理限定管理员权限；校验 base_url（https、禁止私网/链路本地）；URL 被修改时不要回退使用已存密钥。

### B-H6. API key 校验是 O(N) bcrypt 扫描 + 每请求一次写
> **✅ 已修复（阶段 6）** · `415e16e`：新增 `key_digest`（SHA-256 hex，唯一索引）列，按 digest 定向查询后再 bcrypt 比对，校验从 O(N) 全表 bcrypt 扫描降为一次点查 + 一次比对；`last_used_at` 仍是每请求一次同步单行 UPDATE（有意保留该语义，代价已从扫描中分离）。
- **位置**：`store/openlineage_api_key.go:67-103`、`openlineage_handler.go:47`
- **证据**：`SELECT ... WHERE revoked_at IS NULL` 后逐行 `bcrypt.CompareHashAndPassword`，成功后再 `UPDATE ... last_used_at = NOW()`；该端点未认证。
- **影响**：伪造 Bearer token 即触发对全部 key 的 bcrypt（每个约 100ms），少量并发即可打满 CPU；且在校验循环中占用第二个连接做 UPDATE。
- **修复**：增加确定性的 key 前缀/ID 索引列，单行查询后只做一次 bcrypt（或改用 HMAC-SHA256 + 常量时间比较）；`last_used_at` 异步/批量更新。

### B-H7. 批量摄取无上限、逐事件事务
> **✅ 已修复（阶段 6）** · `e7d15eb`：单请求限制 1000 事件 / 8MiB 体积，超限显式返回 413；新增 `Store.UpsertOpenLineageRuns` 让整批事件共用一个事务，不再逐事件开事务。
- **位置**：`openlineage_handler.go:52,83-109`、`store/openlineage_run.go:61-86`
- **影响**：一个 10MB 的最小事件数组可触发上万次事务与数十万次查询，长时间占用连接池；无速率限制、无请求超时。
- **修复**：限制每批事件数、批内单事务/批量插入、增加限流与服务端超时。

### B-H8. ingestion key 全局无范围，任何 key 可伪造任意实例血缘
> **✅ 已修复（阶段 6）** · `415e16e`：`openlineage_api_key` 增加 `scope_namespace`（空串=不限），ingestion handler 校验事件的 job 以及每个输入/输出数据集的 namespace 是否都等于该作用域，超出作用域返回 `403`（handler 会丢弃 keyMessage 的其余维度，没有实例级作用域）。
- **位置**：`store/openlineage_api_key.go:16-25`、`openlineage_handler.go:47`
- **影响**：key 没有 namespace/instance/owner 维度，且 handler 丢弃返回的记录；一个泄露的 key 可向任意实例注入伪造血缘（进而污染 ExplainSQL 上下文与 UI）。
- **修复**：key 绑定 namespace/instance（或 owner），拒绝超出范围的事件。

## 中（Medium）

- **M1. 批量摄取静默丢事件却返回成功**：`openlineage_handler.go:89-115`，全部事件解析失败时 `lastErr` 仍为 nil，返回 `200 {"status":"ok","processed":0}`，生产者不会重试 → 血缘静默丢失。 —— **✅ 已修复（阶段 3 补遗，`d562a50`）**：handler 分别统计 `processed`/`failed` 并回显，全部无法解析返回 400（重发无用），存在服务端失败返回 500（可重试），不再「只要一条成功就 200」。
- **M2. `fetchObjectsByGUIDs` 的 metas 与 guids 错位**：`explain_sql_service.go:346-361`，跳过失败的 GUID 后用 `guids[:len(metas)]` 配对，导致后续对象被归到错误的 GUID/库/schema，产出"自信但错误"的解释。
- **M3. store 错误被吞成"对象不存在"**：`explain_sql_service.go:407,446`（以及 `351`），瞬时 DB 故障被当作 miss 告诉模型。
- **M4. 空 `scope_prefix` 使 `search_objects` 恒返回空**：`explain_sql_service.go:106,446` + `store/meta_resource.go:85-95`，空前缀生成 `guid LIKE ';%'`，而系统提示仍在告诉模型"有工具可查 schema"；前端未选实例时会发空 scope。 —— **✅ 已修复（阶段 3 补遗，`11943ae`）**：空前缀在 store 里现在表示「不限实例」（此前生成 `guid = '' OR guid LIKE ';%'`，恒空），空关键字返回明确的工具错误，搜索失败不再被 `_` 丢弃。
- **M5. 缓存写入错误被吞且使用请求 ctx**：`explain_sql_service.go:212-214`，客户端断开导致昂贵结果被丢弃且无日志。 —— **✅ 已修复（阶段 2，`8c34542`）**：写入改用 `context.WithoutCancel(ctx)` + 5s 超时，失败记 `slog.Warn`（含 cache_key）。
- **M6. 血缘关系列表无界**：`lineage_service.go:57,72,148`，store 支持 Limit/Offset 但 handler 从不设置。 —— **✅ 已修复（阶段 3 补遗，`dd6df51`）**：两个列表补 `page_size`/`page_token`/`next_page_token`（默认 500、上限 5000），handler 用同一个 offset 页化 source/target，前端 `getLineage` 循环取全。
- **M7. `collectExternalDatasets` 静默降级**：`lineage_service.go:121-124`，DB 错误时返回空列表，UI 无法区分"无元数据"与"查询失败"。 —— **✅ 已修复（阶段 6）** · `0151bcb`：`collectExternalDatasets` 查询失败不再静默返回空列表，改为记日志/返回错误。
- **M8. `formatResolvedTarget` 对 MySQL 空 schema 泄露 instance id**：`openlineage_dataset.go:556-574`，`"inst;db;;table"` 去掉空段后恰好 3 段不再裁剪。 —— **✅ 已修复（阶段 6）** · `e7239db`：`formatResolvedTarget` 在 MySQL 空 schema 时不再把 instance id 当作 table 段。
- **M9. LLM profile 分页不可用**：`llm_service.go:54-78`，`page_token` 从不读取、`next_page_token` 从不设置、`page_size` 无上限，只能看到最近 50 条。
- **M10. 空 update_mask 全量替换会清空 models**：`llm_service.go:133,260-269` + `store/llm.go:124-126`，只改标题的 PATCH 会禁用全部模型，profile 从 `Registry.ListEnabled` 消失。 —— **✅ 已修复（阶段 6）** · `14b8f01`：空 `update_mask` 分支改为只写请求中非零字段，不再整体替换 `models`（保持 PATCH 语义）。
- **M11. 自定义 SQL 解释无失效/TTL**：`explain_sql_service.go:505` + `store/explain_sql.go:75-91`，schema 变更后旧解释永久返回；`expired` 标记服务端从不设置。 —— **✅ 已修复（阶段 2，`8c34542`）**：7 天 TTL 在读取时生效（过期即 miss 并重新生成），`expired` 仍不设置（过期行不会返回）。metadata 类解释仍由 metaHash 自动失效。
- **M12. LLM 输出全量驻留内存且无配额**：`explain_sql_service.go:127,180`，每轮上限 1MB × 最多 6 轮，且每轮重发整个会话；无限流/配额。 —— **✅ 已修复（阶段 6）** · `03c17b7`：`llm.DefaultMaxTurns`（6）与 `llm.DefaultMaxConversationBytes`（4MiB）成为显式上限，`run` 用 `conversationBudget` 累计并超限报错，ExplainSQL 调用点显式传入两者。
- **M13. 客户端断开导致 goroutine 泄漏**：`explain_sql_service.go:181-185` + `component/llm/agent.go:19-28,117-145`，handler 返回后不再消费 channel，生产者在 32 槽缓冲满后永久阻塞，且阻塞在 send 上无法感知 ctx 取消。 —— **✅ 已修复（阶段 2，`8acbfe6`）**：所有发送改为 `select { case ch <- evt: case <-ctx.Done(): return }`（`sendEvent`/`sendRaw`），且 handler 用 `context.WithCancel` 派生子 context 并在返回时取消，因此即使 Connect 不取消服务端 ctx，生产者也不会永久阻塞。
- **M14. "流式"实为整包缓冲，且超过 1MB 静默截断**：`component/llm/agent.go:193-205`，`io.ReadAll(io.LimitReader(resp.Body, 1MB))` 后才解析，客户端在整轮结束前收不到任何内容；超限的 SSE 尾部被丢弃，截断结果还会被缓存。 —— **✅ 已修复（阶段 2，`8acbfe6`）**：改为 `bufio.Scanner` 边读边解析（SSE 行长上限 8MiB，响应总量上限 32MiB 且超限报错而非截断），错误/截断/空回答一律中止且不写缓存；`data: [DONE]` 作为正常结束（兼容不设 `finish_reason` 的 provider），无 finish_reason 且无 `[DONE]` 的 EOF 视为流中断。
- **M15. not-found 映射为 `CodeInternal`(500)**：`openlineage_service.go:184,237` + store 返回裸 `errors.Errorf`（`store/namespace_mapping.go:137,159`、`store/openlineage_api_key.go:147`）。
- **M16. `resolveSource` 把 DB 故障报成 `NotFound`**：`explain_sql_service.go:511`。 —— **✅ 已修复（阶段 6）** · `0151bcb`：`resolveSource` 区分 `NotFound` 与 `Internal`，DB 故障不再报成 `NotFound`。
- **M17. `parseStructuredResponse` 首个 section 标题残留 `"## "`**：`explain_sql_service.go:639,649`，缓存命中时 `buildMarkdownFromSection` 再拼 `"## "` → 渲染成 `## ## 执行逻辑`；若模型首行就是标题，`idx == -1` 会把全文当 summary 并产生一个空 section（已用独立程序复现该逻辑）。 —— **✅ 已修复（阶段 6）** · `4d936c3`：首个 section 标题归一化去掉已带的 `"## "`；`idx == -1` 分支不再把全文当 summary 并输出空 section。
- **M18. LLM provider key 仅做可逆 XOR 混淆**：`store/llm.go:41-64` + `common/utils.go:65-71`，字段名却叫 `api_key_encrypted`；有 DB 读权限 + 同一库中的 `AUTH_SECRET` 即可还原。

## 低（Low）／死代码（血缘/LLM）

- `openlineage_dataset.go:396-398` 死分支：用 run 的 *task* GUID 与 *dataset* GUID 比较，且返回与 fall-through 相同的值。
- `openlineage_dataset.go:470` 魔法数 `MetaType: 17`，应使用 `storepb.MetaType_EXTERNAL_DATASET`。 —— **✅ 已修复（阶段 2，`8c34542`）**：改用 `storepb.MetaType_EXTERNAL_DATASET`。
- 未使用的 proto 字段：`ExplainSQLRequest.meta_type`（handler 用 `meta.ObjectType`）、`ExplainSQLMetadata.expired`（UI 有"过期"徽标但服务端从不设置）、`ExplainSQLResponse.error`（错误一律走 RPC error）、`ExplainSQLMetadata.sections_json`（前端未用）、`FetchLLMModelsRequest.provider_name`（前端从不发送）。
- `GetOpenLineageDatasetRequest.namespace/name` 在 `guid` 为空时被拒绝，实际不可单独使用（`openlineage_dataset.go:115`）。
- 重复实现标准库：`stringsJoin`（`llm_service.go:341-350`）、`bytesTrimLeft`（`openlineage_handler.go:172-180`）。
- 陈旧注释/空分支：`grpc_routes.go:85` 引用不存在的函数；`explain_sql_service.go:501` 的 `// ---- resolveSource, buildSystemPrompt, etc. (unchanged) ----` 编辑残留；`explain_sql_service.go:186` 空 `case llm.AgentEventAgentEnd`。
- `plugin/openlineage/metadata.go:94-107` 的 `hasLineageSignal` 在 inputs/outputs 非空时恒为 true，使后续列血缘循环不可达。 —— **✅ 已修复（阶段 6）** · `e7239db`：删除 `hasLineageSignal` 的不可达分支。
- `openlineage_run`/`explain_sql_cache`/`llm_debug_log` 均无保留/清理任务；`llm_debug_log` 以 fire-and-forget goroutine + `context.Background()` 写入完整 prompt/响应且吞掉错误。**阶段 2 部分**：`explain_sql_cache` 现按 7 天 TTL 在读取时失效（`8c34542`），但仍不物理清理过期行；`openlineage_run` 经确认**不**加自动清理（数据可审计），改以读路径限 5000 控制内存。
- `/api/v1/lineage` 是普通 Echo 路由（`grpc_routes.go:169-172`），绕过审计与 debug 拦截器。
- 请求体超限被静默截断后报"解析失败"，而不是 413（`openlineage_handler.go:52`）。 —— **✅ 已修复（阶段 6）** · `e7d15eb`：`readBodyLimited` 多读一字节判断真实越界，超过 8MiB 或超过 1000 个事件都显式返回 413，不再静默截断。
- provider 错误体被透传给客户端：`fmt.Errorf("LLM status %d: %.4000s", resp.StatusCode, respStr)`（`agent.go:201`）。

## 待确认（血缘/LLM）

1. 部署是否为单租户？若实例边界有意义，则 B-C2/B-H1/B-H4 的等级需保持；审计 key 泄露（B-C1）与 LLM key 外带（B-H5）与租户模型无关。
2. `profile.Secret`（XOR seed）如何生成/轮换，决定 M18 的实际严重度。
3. Connect 在客户端断开时是否会取消服务端流式请求的 context（影响 M5；M13 的 goroutine 泄漏与之无关）。
4. `ListOpenLineageDatasets`/`ListOpenLineageRuns`/`ListLLMProviderProfiles` 的分页契约是否为有意设计。
5. `ExplainSQL.provider_name` 服务端支持但 UI 不发送，provider 选择是否为规划功能（若启用，缓存 key 必须包含它）。
6. OpenLineage 事件是否可能缺少时区偏移（会被存成 NULL event time 并排到最后）。
