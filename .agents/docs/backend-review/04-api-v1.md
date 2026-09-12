# 04 · api/v1 服务实现

**范围**：`backend/api/v1/` 下除身份/审计（见 `02`）以外的全部 ConnectRPC handler：
- 数据面：`instance_service.go`、`database_service.go`、`database_history.go`、`database_convert.go`
- 血缘/接入/LLM：`openlineage_service.go`、`openlineage_handler.go`、`openlineage_dataset.go`、`lineage_service.go`、`llm_service.go`、`explain_sql_service.go`
- 公共：`common.go`（过滤器/分页/转换）

**结论**：这一层代码量最大（约 7000 行），问题也最密集。两个全局性问题贯穿所有文件：**没有任何授权检查**，以及 **CEL 过滤器到 SQL 的拼接存在注入**（4 处 handler + 2 处 store）。此外有 SSRF、缓存串租户、无界查询、N+1、分页失效、大量 Bytebase 遗留 stub。

---

# A. 数据面：Instance / Database / History

## 严重（Critical）

### A-C1. 所有数据面 handler 都没有授权/归属校验
- **位置**：`instance_service.go:50-57` 及全部 handler；`database_service.go` 同理
- **证据**：`GetInstance` → `getInstanceMessage(ctx, s.store, req.Msg.Name)`，没有任何 user/permission 检查；`Create/Update/Delete/SyncInstance`、`Add/Update/RemoveDataSource`、以及 `DatabaseService` 全部方法都一样。`common.UserContextKey` 被拦截器写入但这里从不读取。
- **影响**：任意已认证用户（含只读用户、刚自助注册的攻击者）可读取任意实例（含数据源配置）、创建/删除实例、触发同步（导致服务端主动外连）、轮换数据源凭证。
- **修复**：恢复/实现 ACL 拦截器，或在每个 handler 内显式校验权限与实例范围；补充"无权限用户应得 `CodePermissionDenied`"的测试。

### A-C2. CEL 过滤器 SQL 注入
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
- **位置**：`instance_service.go:283-308,605-627,805-825`
- **证据**：`s.dbFactory.GetDataSourceDriver(ctx, instanceMessage, ds, ...)` 后 `connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid datasource %s", ...))`，把 driver 原始错误返回给调用方。
- **影响**：任意已认证用户可让服务端连接任意 `host/port`（含 SSH host），并从错误信息读出 `dial tcp 10.0.0.5:6379: connect: connection refused` 之类结果 → 内网端口扫描 + 服务指纹。
- **修复**：要求实例管理权限；对私网/链路本地/回环地址做 allow/deny；返回脱敏后的通用错误。

### A-H2. `DiffMetadata` 默认 `source_time = now`，文档承诺的"最早版本"无法实现
- **位置**：`database_service.go:970-976`
- **证据**：`if asOf != nil { asOfTime = asOf.AsTime() } else { asOfTime = time.Now() }`，而 proto 注释写的是 "If not set, uses the earliest available version"（`database_service.proto:493-495`）。
- **影响**：不传 `source_time` 时源与目标都是 now，diff 恒为空，返回 "No changes detected."。
- **修复**：`source_time` 为空时查询最早历史行（`OrderDesc` + `Limit=1`，或新增 earliest 查询）；只有 `target_time` 默认 now。

### A-H3. `DiffMetadata` 只重建 4 类对象，多数变更不可见
- **位置**：`database_service.go:1025-1091,1095-1102,1106-1176`
- **证据**：`rebuildDatabaseObjects` 只取 `TABLE/VIEW/FUNCTION/PROCEDURE`；`buildDiffSummary` 也只统计 table/view/function/schema。
- **影响**：物化视图、序列、枚举类型、package、external table、stream、task、event、extension、event trigger、以及 schema 的 owner/comment/skip_dump 变更全部不可见；仅序列或 MV 变化会返回"无变更"和空 DDL，而底层 differ 是支持这些类型的。
- **修复**：重建全部已存储对象类型（或 `ObjectType IN (...)` 批量查询），并把相应 Changes 计入 summary。

### A-H4. `UpdateInstance(data_sources)` 会清空已存密钥并降级 TLS 校验
- **位置**：`instance_service.go:405-413,1307-1353`
- **证据**：`convertV1DataSource` 不填充 `verify_tls_certificate`、`cluster`、`role_arn`/`external_id`、Vault TLS 等 store-only 字段；而读取路径故意不返回 password/SSL（`instance_service.go:1066`）。
- **影响**：Get → 编辑 → Update 的常规往返会持久化空密码/空 SSL key，并把 `verify_tls_certificate` 重置为 false（TLS 校验降级），静默破坏或削弱既有连接。
- **修复**：按 data source ID 合并到现有 store metadata，保留未回传字段与密钥；不要整体替换列表。

### A-H5. 审计脱敏遗漏 GCP `content`、`sslKey`、Kerberos `keytab`
- **位置**：`backend/api/v1/audit.go:197-208`（由这些 handler 的 `audit: true` 触发）
- **影响**：GCP 服务账号 JSON、TLS 私钥、Kerberos keytab 明文落入 `audit_log.request`。
- **修复**：补充 `content`/`sslkey`/`keytab`/`ssl_cert` 标记，或按 proto 注解结构化脱敏；加回归测试。
- **关联**：`02` 的 H3（OpenLineage API key）。

## 中（Medium）

- **M1. 未检查类型断言 / `AsLiteral()` panic**：`instance_service.go:78,81,84,91,98,106,109,112,141`；`database_service.go:741,808,833,865`。`name == 123`、`engine in [1]`、`name.matches(ident)` 都会 panic → 500。
- **M2. `ListMetadata` 在 `meta_type` 为空时翻页失效**：`database_service.go:200-231`；`FindSubLevelMetaRegistryResourceMessage` 只有 `LimitPreObjectType`，没有 offset（`store/meta_resource.go:51-55,775`），但仍会返回 `next_page_token`，第 2 页与第 1 页相同。
- **M3. `ListMetadataHistory` 全量加载 + 分页 off-by-one**：`database_history.go:48-51,59-72`，查询无 limit/offset，之后 `if len(events) > limitPlusOne` 应为 `>=`，否则返回 `page_size+1` 条且无 token。
- **M4. `GetSchemaString` 序列查询前缀错误**：`database_service.go:345` 用 `common.GUIDPrefix`（按 `"."` 切分，`common/guid.go:33-38`），而所有 GUID 用 `";"` 拼接 → 前缀恒为空，PG 的 `ALTER SEQUENCE ... OWNED BY`/identity DDL 丢失。
- **M5. `CreateInstance` 不校验 environment/engine**：`instance_service.go:277,947-971`，与 `UpdateInstance`（`395-401`）不一致，可创建引用不存在环境的实例。
- **M6. `UpdateInstance(data_sources)` 可删掉 admin 数据源**：`instance_service.go:405-413,338-351`；store 的 `validateDataSources`（要求恰好一个 ADMIN）在更新路径未被调用。
- **M7. 批量 RPC 部分成功无逐项结果**：`instance_service.go:536-555,561-570`；第 N 项失败时前 N-1 项已提交，客户端只拿到一个错误；也未限制 1000 条上限。
- **M8. `ListDatabase` N+1 且可能 nil deref**：`database_service.go:1178-1195`；每行一次 `GetInstanceV2`（含完整 metadata），`convertInstanceMessageToInstanceResource` 无 nil 检查，而 `GetInstanceV2` 未命中返回 `(nil, nil)`。
- **M9. `SyncDatabase` 泄漏非 Connect 错误**：`database_service.go:55-58` + `common.go:394-422`，缺失数据库/大小写冲突返回 `CodeUnknown` 而非 `NotFound`/`AlreadyExists`。
- **M10. `CreateManualSQL` 不校验 `manual_sql_id`**：`database_service.go:423-425,682-698`，含 `/` 的 ID 会生成无法被 `parseManualSQLName` 解析的资源名，对象从此不可寻址。
- **M11. 历史变更检测遗漏大量字段**：`database_history.go:244-281,579-631,754-763,1010-1017`，如列 `generation`/`identity_*`、索引 `key_length`/`descending`/`opclass_*`、外键 `match_type`、表 `triggers`/`rules` 等，真实变更被报成"无变化"。
- **M12. `rebuildSchemaContents` 吞掉重建错误并回退到当前元数据**：`database_service.go:1095-1102`，会给出"看似合理但错误"的历史 diff。

## 低（Low）／代码质量

- `SearchMetadata` 永远不返回 `next_page_token`，硬编码 50 条（`database_service.go:273-304`），而响应里有该字段。
- `meta_type` 未设置时被当成 0 传给 store，导致 `NotFound` 而非解析或 `InvalidArgument`（`database_service.go:252,327`）。
- `table.matches()` 把 pattern 转小写却不 `LOWER()` 列名（`database_service.go:870,880`），`table.matches("Foo")` 永远匹配不到。
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
- `parseListInstanceFilter` 与 `getListDatabaseFilter` 是约 120 行的近重复 CEL→SQL 翻译器；store 通过 `strings.Contains(filter.Where, "ds.metadata->'schemas'")`/`hasHostPortFilter` 反推 join，十分脆弱。
- `common.go:49-185` 的 `ParseFilter`/`normalizeFilter`/`Expression` 是旧过滤器解析器，无引用。
- `convertStoredMetadataMessage` 静默丢弃 store-only 的 `openlineage_run_summary`/`openlineage_task_summary`（`database_convert.go:73-74`）。
- `convertRedisType` 把 `REDIS_TYPE_UNSPECIFIED` 映射为 `STANDALONE`（`instance_service.go:1293-1305`），语义错误。

---

# B. Lineage / OpenLineage / LLM / ExplainSQL

## 严重（Critical）

### B-C1. 明文 ingestion API key 落审计日志并可通过审计 API 读取
- **位置**：`proto/v1/v1/openlineage_service.proto:66-75,300-304`、`backend/api/v1/audit.go:114-134,179-208`、`audit_log_service.go:185-202`
- **证据**：`CreateAPIKey` 带 `audit = true`；审计拦截器把**响应**也写入；`CreateAPIKeyResponse.key` 的 protojson 字段名就是 `"key"`，不在脱敏标记列表中；`ListAuditLogs` 返回 `Response`。
- **影响**：一次性明文 key 持久化且可被读取，"只返回一次"的承诺失效，轮换无意义。
- **修复**：把裸 `key` 加入脱敏；或对"签发密钥"类方法不记录响应体。
- **关联**：`02` 的 H3。

### B-C2. 全层无授权：任意已认证用户可读任意实例血缘、读取全部 run/raw_payload、创建/吊销全局 ingestion key
- **位置**：`backend/server/grpc_routes.go:85`、`lineage_service.go:41-90`、`openlineage_service.go:202-242`
- **证据**：`GetLineage` 只接收 GUID 并直接 `FindColumnLineageMessage{TargetGUID: &req.Msg.Guid}`；`CreateAPIKey`/`ListAPIKey`/`RevokeAPIKey` 无任何权限检查；`apiv1.NewACLInterceptor` 符号已不存在。
- **影响**：血缘、OpenLineage 数据、原始 payload 全部跨实例可读；可铸造或吊销他人的 key。
- **修复**：恢复授权拦截器并声明 `permission`；key 的创建/吊销限定到 owner 或管理员。

## 高（High）

### B-H1. OpenLineage 数据集接口无界扫描全表并解析全部 payload
- **位置**：`openlineage_dataset.go:40,119`、`store/openlineage_run.go:299,306-311`
- **证据**：`ListOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{})`；store 仅在 `Limit != nil` 时追加 LIMIT，且始终 select `raw_payload`；随后每个 run 都 JSON 解析。
- **影响**：内存/CPU 随事件总量线性增长，无上限；前端传的 `pageSize: 500` 只作用于内存聚合，不进入查询。
- **修复**：服务端聚合（SQL），或至少把 limit/offset 与时间窗下推到查询，并增加保留策略。

### B-H2. 数据集解析 N+1（每个 dataset 一次 namespace 映射查询 + 全实例表扫描）
- **位置**：`openlineage_dataset.go:407`、`plugin/openlineage/resolver.go:80,110`
- **证据**：`ResolveDatasetPreview` 每次都执行 `ListInstancesV2(ctx, &store.FindInstanceMessage{})`（读事务 + 全表扫描），无 memoization。
- **影响**：每次请求 `O(runs × datasets)` 次 DB 往返。
- **修复**：按 `(namespace,name)` 在请求内做一次解析缓存；把 `ListInstancesV2` 提到循环外。

### B-H3. `GetOpenLineageDataset` 对同一批 run 解析两遍
- **位置**：`openlineage_dataset.go:226,238`
- **影响**：在已经无界的接口上再翻倍 JSON 解析与解析工作量。
- **修复**：一次遍历同时产出聚合与详情。

### B-H4. ExplainSQL 缓存 key 不含实例/GUID/provider → 跨实例串用
- **位置**：`explain_sql_service.go:505,524`、`store/store.go:129-137`、`LATEST.sql:417-427`
- **证据**：`cacheKey = "sql:<sha256(sqlText)>"`；`"meta:<metaHash>"`，而 `MetaHash` 只是 `StoredMetadata` 的 sha256，不含 GUID/instance；`explain_sql_cache` 无 scope 列；`GetExplainSQLCache` 只按 `cache_key` 查。
- **影响**：两个实例中定义相同的表（或不同实例下相同的 SQL 文本）共享缓存；返回的解释里嵌入了另一个实例的列名/DDL/对象名，跨实例信息泄露；切换 provider 后仍返回旧 provider 的结果。
- **修复**：key 纳入 instance/GUID/scopePrefix 与 provider/model；增加 scope 列。

### B-H5. LLM provider key 可被外带（无归属校验 + base_url 未校验 → SSRF）
- **位置**：`llm_service.go:80-170,200-204`、`component/llm/fetcher.go:26-34`、`component/llm/agent.go:161,178`
- **证据**：`FetchLLMModels` 在请求未带 api_key 时回退到 `prof.Metadata.ApiKeyEncrypted`，然后 `GET baseURL + "/v1/models"` 并携带 `Authorization: Bearer <key>`；`Create/UpdateLLMProviderProfile` 对 `BaseUrl` 无任何校验、无管理员/归属检查。
- **影响**：任意已认证用户可把某个 profile 的 `base_url` 指向自己的服务器，再触发 `FetchLLMModels`/`ExplainSQL`，让服务端把该 profile 的存储密钥发给自己；同时构成任意出站请求（如 `http://169.254.169.254/…`）。
- **修复**：profile 管理限定管理员权限；校验 base_url（https、禁止私网/链路本地）；URL 被修改时不要回退使用已存密钥。

### B-H6. API key 校验是 O(N) bcrypt 扫描 + 每请求一次写
- **位置**：`store/openlineage_api_key.go:67-103`、`openlineage_handler.go:47`
- **证据**：`SELECT ... WHERE revoked_at IS NULL` 后逐行 `bcrypt.CompareHashAndPassword`，成功后再 `UPDATE ... last_used_at = NOW()`；该端点未认证。
- **影响**：伪造 Bearer token 即触发对全部 key 的 bcrypt（每个约 100ms），少量并发即可打满 CPU；且在校验循环中占用第二个连接做 UPDATE。
- **修复**：增加确定性的 key 前缀/ID 索引列，单行查询后只做一次 bcrypt（或改用 HMAC-SHA256 + 常量时间比较）；`last_used_at` 异步/批量更新。

### B-H7. 批量摄取无上限、逐事件事务
- **位置**：`openlineage_handler.go:52,83-109`、`store/openlineage_run.go:61-86`
- **影响**：一个 10MB 的最小事件数组可触发上万次事务与数十万次查询，长时间占用连接池；无速率限制、无请求超时。
- **修复**：限制每批事件数、批内单事务/批量插入、增加限流与服务端超时。

### B-H8. ingestion key 全局无范围，任何 key 可伪造任意实例血缘
- **位置**：`store/openlineage_api_key.go:16-25`、`openlineage_handler.go:47`
- **影响**：key 没有 namespace/instance/owner 维度，且 handler 丢弃返回的记录；一个泄露的 key 可向任意实例注入伪造血缘（进而污染 ExplainSQL 上下文与 UI）。
- **修复**：key 绑定 namespace/instance（或 owner），拒绝超出范围的事件。

## 中（Medium）

- **M1. 批量摄取静默丢事件却返回成功**：`openlineage_handler.go:89-115`，全部事件解析失败时 `lastErr` 仍为 nil，返回 `200 {"status":"ok","processed":0}`，生产者不会重试 → 血缘静默丢失。
- **M2. `fetchObjectsByGUIDs` 的 metas 与 guids 错位**：`explain_sql_service.go:346-361`，跳过失败的 GUID 后用 `guids[:len(metas)]` 配对，导致后续对象被归到错误的 GUID/库/schema，产出"自信但错误"的解释。
- **M3. store 错误被吞成"对象不存在"**：`explain_sql_service.go:407,446`（以及 `351`），瞬时 DB 故障被当作 miss 告诉模型。
- **M4. 空 `scope_prefix` 使 `search_objects` 恒返回空**：`explain_sql_service.go:106,446` + `store/meta_resource.go:85-95`，空前缀生成 `guid LIKE ';%'`，而系统提示仍在告诉模型"有工具可查 schema"；前端未选实例时会发空 scope。
- **M5. 缓存写入错误被吞且使用请求 ctx**：`explain_sql_service.go:212-214`，客户端断开导致昂贵结果被丢弃且无日志。
- **M6. 血缘关系列表无界**：`lineage_service.go:57,72,148`，store 支持 Limit/Offset 但 handler 从不设置。
- **M7. `collectExternalDatasets` 静默降级**：`lineage_service.go:121-124`，DB 错误时返回空列表，UI 无法区分"无元数据"与"查询失败"。
- **M8. `formatResolvedTarget` 对 MySQL 空 schema 泄露 instance id**：`openlineage_dataset.go:556-574`，`"inst;db;;table"` 去掉空段后恰好 3 段不再裁剪。
- **M9. LLM profile 分页不可用**：`llm_service.go:54-78`，`page_token` 从不读取、`next_page_token` 从不设置、`page_size` 无上限，只能看到最近 50 条。
- **M10. 空 update_mask 全量替换会清空 models**：`llm_service.go:133,260-269` + `store/llm.go:124-126`，只改标题的 PATCH 会禁用全部模型，profile 从 `Registry.ListEnabled` 消失。
- **M11. 自定义 SQL 解释无失效/TTL**：`explain_sql_service.go:505` + `store/explain_sql.go:75-91`，schema 变更后旧解释永久返回；`expired` 标记服务端从不设置。
- **M12. LLM 输出全量驻留内存且无配额**：`explain_sql_service.go:127,180`，每轮上限 1MB × 最多 6 轮，且每轮重发整个会话；无限流/配额。
- **M13. 客户端断开导致 goroutine 泄漏**：`explain_sql_service.go:181-185` + `component/llm/agent.go:19-28,117-145`，handler 返回后不再消费 channel，生产者在 32 槽缓冲满后永久阻塞，且阻塞在 send 上无法感知 ctx 取消。
- **M14. "流式"实为整包缓冲，且超过 1MB 静默截断**：`component/llm/agent.go:193-205`，`io.ReadAll(io.LimitReader(resp.Body, 1MB))` 后才解析，客户端在整轮结束前收不到任何内容；超限的 SSE 尾部被丢弃，截断结果还会被缓存。
- **M15. not-found 映射为 `CodeInternal`(500)**：`openlineage_service.go:184,237` + store 返回裸 `errors.Errorf`（`store/namespace_mapping.go:137,159`、`store/openlineage_api_key.go:147`）。
- **M16. `resolveSource` 把 DB 故障报成 `NotFound`**：`explain_sql_service.go:511`。
- **M17. `parseStructuredResponse` 首个 section 标题残留 `"## "`**：`explain_sql_service.go:639,649`，缓存命中时 `buildMarkdownFromSection` 再拼 `"## "` → 渲染成 `## ## 执行逻辑`；若模型首行就是标题，`idx == -1` 会把全文当 summary 并产生一个空 section（已用独立程序复现该逻辑）。
- **M18. LLM provider key 仅做可逆 XOR 混淆**：`store/llm.go:41-64` + `common/utils.go:65-71`，字段名却叫 `api_key_encrypted`；有 DB 读权限 + 同一库中的 `AUTH_SECRET` 即可还原。

## 低（Low）／死代码（血缘/LLM）

- `openlineage_dataset.go:396-398` 死分支：用 run 的 *task* GUID 与 *dataset* GUID 比较，且返回与 fall-through 相同的值。
- `openlineage_dataset.go:470` 魔法数 `MetaType: 17`，应使用 `storepb.MetaType_EXTERNAL_DATASET`。
- 未使用的 proto 字段：`ExplainSQLRequest.meta_type`（handler 用 `meta.ObjectType`）、`ExplainSQLMetadata.expired`（UI 有"过期"徽标但服务端从不设置）、`ExplainSQLResponse.error`（错误一律走 RPC error）、`ExplainSQLMetadata.sections_json`（前端未用）、`FetchLLMModelsRequest.provider_name`（前端从不发送）。
- `GetOpenLineageDatasetRequest.namespace/name` 在 `guid` 为空时被拒绝，实际不可单独使用（`openlineage_dataset.go:115`）。
- 重复实现标准库：`stringsJoin`（`llm_service.go:341-350`）、`bytesTrimLeft`（`openlineage_handler.go:172-180`）。
- 陈旧注释/空分支：`grpc_routes.go:85` 引用不存在的函数；`explain_sql_service.go:501` 的 `// ---- resolveSource, buildSystemPrompt, etc. (unchanged) ----` 编辑残留；`explain_sql_service.go:186` 空 `case llm.AgentEventAgentEnd`。
- `plugin/openlineage/metadata.go:94-107` 的 `hasLineageSignal` 在 inputs/outputs 非空时恒为 true，使后续列血缘循环不可达。
- `openlineage_run`/`explain_sql_cache`/`llm_debug_log` 均无保留/清理任务；`llm_debug_log` 以 fire-and-forget goroutine + `context.Background()` 写入完整 prompt/响应且吞掉错误。
- `/api/v1/lineage` 是普通 Echo 路由（`grpc_routes.go:169-172`），绕过审计与 debug 拦截器。
- 请求体超限被静默截断后报"解析失败"，而不是 413（`openlineage_handler.go:52`）。
- provider 错误体被透传给客户端：`fmt.Errorf("LLM status %d: %.4000s", resp.StatusCode, respStr)`（`agent.go:201`）。

## 待确认（血缘/LLM）

1. 部署是否为单租户？若实例边界有意义，则 B-C2/B-H1/B-H4 的等级需保持；审计 key 泄露（B-C1）与 LLM key 外带（B-H5）与租户模型无关。
2. `profile.Secret`（XOR seed）如何生成/轮换，决定 M18 的实际严重度。
3. Connect 在客户端断开时是否会取消服务端流式请求的 context（影响 M5；M13 的 goroutine 泄漏与之无关）。
4. `ListOpenLineageDatasets`/`ListOpenLineageRuns`/`ListLLMProviderProfiles` 的分页契约是否为有意设计。
5. `ExplainSQL.provider_name` 服务端支持但 UI 不发送，provider 选择是否为规划功能（若启用，缓存 key 必须包含它）。
6. OpenLineage 事件是否可能缺少时区偏移（会被存成 NULL event time 并排到最后）。
