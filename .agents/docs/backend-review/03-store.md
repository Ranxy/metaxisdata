# 03 · Store 持久层

**范围**：`backend/store/` 全部文件（`store.go`、`db_connection.go`、`common.go`、`meta_resource.go`、`instance.go`、`database.go`、`manual_sql.go`、`column_lineage.go`、`project.go`、`policy.go`、`principal.go`、`role.go`、`group.go`、`setting.go`、`idp.go`、`llm.go`、`stats.go`、`environment.go`、`explain_sql.go`、`external_dataset.go`、`namespace_mapping.go`、`openlineage_run.go`、`openlineage_task.go`、`openlineage_api_key.go`、`audit_log.go`）。

**结论**：Store 层是全后端第二高风险区。核心问题：① 有两处 SQL 注入（project ID 拼接）；② `enableCache=false` 使"缓存未命中即全表加载"成为热路径，每个认证请求都全表扫描 `principal`；③ 若干部件引用了已不存在的表（~~`db_schema`~~、`issue`、`query_history` 等），对应功能必然运行时报错；④ 枚举以整数参数传入 text 列导致过滤静默失效；⑤ 事务卫生不一致（缺 `defer Rollback`、跨事务读改写）。

**阶段 0 更新**：S-C1 ✅（project ID 校验）、M12 ✅（`PatchWorkspaceIamPolicy` 改事务化，不再改缓存指针）；`CountUsers` 增加 `deleted = FALSE`、`CreateUser` 加 advisory lock 并在同事务授予首个管理员（见 `02` C2）。S-H1/S-H3/S-H6、M1-M11 等**未处理**。

**阶段 1 更新**：S-H3 ✅（`db_schema` join 与整个 `table` 过滤器一起删除，`bb93ee0`）、S-H4 ✅（`UpdateDatabase` 判空，`20e284b`）、低节"`UpdateInstanceV2` 不做 data source 校验" ✅（API 层 `checkInstanceDataSources` 现在要求恰好一个 ADMIN，`20e284b`）。S-H1/S-H5/S-H6、M1-M11 等仍未处理（阶段 2/3）。

**阶段 2 更新**：S-H1 ✅（缓存启用并删除 `enableCache`，缓存 miss 改定向查询，`f22f61e`）、S-H6 ✅（唯一邮箱索引 + 冲突映射，`ff9b22a`）、M10 部分 ✅（`object_type` 索引已加，`metadata` GIN 经确认不加）、M11 ✅（GUID 缓存 key 纳入 `object_type`）、M13 ✅（`GetSecret` 互斥锁 + 字段不导出）、M19 ✅（`explain_sql_cache` 加 7 天 TTL）。M3/M5/M22/M25 等仍未处理。
**阶段 3 更新**：① 死代码按 `10` 第二节清单删除——`role.go`/`project.go` 整文件与 `rolesCache`/`projectCache`、`stats.go` 的 5 个统计方法（含查询不存在 `issue`/`project` 表的 `CountIssues`/`CountProjects`）、`policy.go` 的 4 个 V2 CRUD、`group.go` 的 `CreateGroup`/`DeleteGroup`、`DeleteColumnLineageByMeta`、`QueryColumnLineageSources`/`Targets`、`CheckDatabaseUseEnvironment`、`MarshalOpenLineageRunPayload`、`common.go` 的 `RowStatus`/`SortOrder`/`OrderByKey`（`a39bc41`）；**IAM 牵连部分逐个确认后保留**（工作区 IAM 路径、group 读路径，两者被 `utils/member.go`/SSO 使用）。② 凭证加密换成 AES-256-GCM，密钥优先 `METADATA_SECRET_KEY`（新增 `store.WithEncryptionKey`），否则回退数据库 `AUTH_SECRET` 并打 Warn，空的/过短的 key 直接报错（`a1faf65`）——`GetSecret` 不再存在空 seed 导致静默乱码或除零的路径。③ `meta_resource.go`(1058 行) 拆为 `meta_resource.go`/`meta_resource_query.go`/`meta_resource_history.go`；`FindSubLevelMetaRegistryResourceMessage` 新增 `OffsetPreObjectType` 并在两个子层级查询里 `OFFSET`，子层级分页此前第 2 页会重复第 1 页（`52213af` `0590607`）。④ 新增 `setting_test.go`（密钥优先级与短 key 拒绝）。**未处理**：M3/M5/M22/M25、`project`/`role` 死表删除。

**阶段 3 收尾更新**：① `store/role.proto`/`store/project.proto`/`store/explain_sql.proto` 三个文件与 `TagPolicy`/`EnvironmentTierPolicy`/`RolePermissions`/`Project`/`Label` 消息删除；**`store.Policy` 保留**——`store/store.go:125`、`store/group.go:114`、`store/policy.go` 在用它写 `policy.resource_type`/`type` 列（原报告的"零 Go 使用"是 grep 漏了 enum 常量，`722d3cb`）。② `store/instance.go` 的凭据混淆改为表驱动（`secretFields`），删掉 Azure/AWS/GCP 凭据、`authentication_private_key`、`master_password` 的混淆分支；`Instance.roles`/`labels` 字段删除（`ceb6a3d`）。③ `store/database.proto` 删 `backup_available`、`InstanceRoleMetadata`、`LinkedDatabaseMetadata`、`Package`/`Stream`/`Task` 与六种 spatial index 配置（`ddff264`）；`store/idp.go` 只留读取路径（`e0eab33`）。④ `LATEST.sql` 的 `role`/`project`/`policy` 列注释更新为"对应消息已删/无调用者"。**剩余**：M3/M5/M22/M25 与删表删列。

**阶段 3 续更新**：① store 侧 12 个 `*V2` 方法与 impl helper 去掉后缀（`8b328ae`），`listSettingV2Impl`/`listPolicyImplV2`/`listInstanceImplV2` 统一为 `*Impl`。② `db.project` 相关全部移除（`451cb78`）：`DatabaseMessage`/`FindDatabaseMessage`/`UpdateDatabaseMessage.ProjectID`、`BatchUpdateDatabases`（唯一调用者是 `DeleteInstance.force`，一并删除）、所有 INSERT/UPDATE/SELECT/ORDER BY 与 `listInstanceImpl` 的 `db.project` join；`principal.go`/`group.go` 里的 project WITH 子句删除。③ store 审计消息改名对齐 v1（`792ca71`）：`AuditSeverity`→`AuditLogSeverity`、`AuditStatus`→`AuditLogStatus`、`RequestMetadata`→`AuditRequestMetadata`；protojson 存的是枚举**值名**，既有 JSONB 行解码不变。④ `UpdateNamespaceMapping` 新增 `updateMask []string` 参数（`733b3e0`）：空 mask 保持旧行为（namespace/instance_resource_id 为空则跳过、database_name 总是写以便清空）。⑤ `setting.value` 是 text 而非 JSONB 属有意（`d8ce592`）：结构化 setting 存 protojson、标量 setting（`AUTH_SECRET`/`BRANDING_LOGO`/`WORKSPACE_ID`）存裸字符串，后者本身不是合法 JSON。⑥ `store/openlineage.proto` 的 `ExternalDataset`/`NamespaceMapping` 消息删除（`b2e80ae`）——两张表由手写 `*Message` 结构读写，该文件只剩 JSONB 里的 `SchemaField` 与两个 `*Summary` 消息在用。**上一段收尾更新里剩余项中的删表删列已完成**（`904fb09` `451cb78`，见 `06`）；M3/M5/M25 仍未处理。

**阶段 3 补遗更新**：① 搜索改读生成的 `search_text` 列（内容恰为 name/title/comment/userComment 的拼接，由 IMMUTABLE SQL 函数维护）并加 `pg_trgm` GIN 索引（增量 `0.1.0005`，`f50fbbc`），谓词从 `m.inner_meta->>'…' ILIKE $n` 变为 `r.search_text ILIKE $n`——匹配行不变（已在真实 PostgreSQL 16 上对 `%`/`_`/双引号/反斜杠等关键字与旧 `jsonb_each` 谓词逐一对拍），并去掉了 LATERAL 扫描；空 `SearchStr` 返回 `common.Invalid`，空 `GUIDPrefix` 表示不限定实例（此前生成 `guid = '' OR guid LIKE ';%'`，恒空）。② 新增 `ListMetaRegistryResourceDigest`（不取 metadata、不碰缓存）与 `ListColumnLineageVersions`（一次取某种类型的全部版本），analyzer 的每小时 N+1 次查询降为每类型 2 次（`f50fbbc` `9019140`）。③ 新增 `WithCacheDisabled()`：观察别的进程写入的 store（集成 harness 的 `inspectStore`）不再读到本进程的陈旧缓存（`f50fbbc`）。④ 新增 `DeleteExpiredExplainSQLCache`/`DeleteExpiredLLMDebugLog`/`DeleteOpenLineageRunsBefore`（后者在同一事务内重算受影响 task 的聚合、删除已无 run 的 task 与两者的 meta registry 行）与两张表的 `created_at` 索引（`f50fbbc`）。⑤ `upsertOpenLineageTask` 的入参从 `*OpenLineageRunMessage` 改为 `taskGUID`，供保留清理复用（`f50fbbc`）。

**阶段 3 收尾二更新**：本节"查询形状即不变量"的欠账补齐（`df97e0a` `f65baaa` `711c0aa`）。① T-H3：两个子层级 list impl 里复制粘贴的 GUID 子树谓词、以及 `listDatabaseImpl` 的环境/实例/大小写/`ShowDeleted` 范围谓词，分别抽成纯构造函数 `buildSublevelMetaRegistryResourceQuery`（内部复用 `appendGUIDSubtreeCondition`）与 `buildListDatabaseQuery`；新增 `meta_resource_query_test.go`/`database_test.go` 断言谓词形状、LIKE 元字符转义、`LIMIT/OFFSET`、filter 占位符编号，并用一个公共断言保证**最大占位符编号恰好等于参数切片长度**（不匹配就是运行期 SQL 错误）。② M8：工作区 IAM 的成员/角色合并从 `patchWorkspaceIamPolicyImpl` 抽成纯函数 `patchIamPolicyBindings`，顺带修掉一个隐患——缺失角色过去按 map 迭代顺序追加，现在按请求顺序，存储 payload 确定；`generateEtag` 与 manual SQL 的 `buildManualSQLGUID`/`normalizeManualSQLTags`/`normalizeManualSQLAttributes`/`buildManualSQLStoredMetadata` 也补了测试（`policy_test.go`/`manual_sql_pure_test.go`）。

**阶段 5 更新**：阶段 3 ②的凭证加密被回滚（`7870016`）——`common.Obfuscate`/`Unobfuscate` 回到 `AUTH_SECRET` 种子 XOR，删除 `store.WithEncryptionKey` 与 `Store.encryptionKey`，`GetSecret` 只读 `AUTH_SECRET`（保留"缺失/为空即报错"，避免空 seed 除零）。`setting_test.go` 相应重写为「已解析 secret 走缓存、不查库」的 hermetic guard。另：`GetWorkspaceGeneralSetting` 里的 `openlineage_retention_days` 由 `runner/maintenance` 每轮读取（见 `06`）。


---

## 严重（Critical）

### S-C1. project ID 拼接导致 SQL 注入（`ListUsers` 可达）
> **✅ 已修复（阶段 0）** · `3321801`：选择"校验字符"而非改成占位符——`store/principal.go` 与 `store/group.go` 现在先用 `common.IsValidResourceID`（新增于 `backend/common/resource_name.go`，正则 `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`）校验 `*v`，非法即返回 `common.InvalidArgument`，合法值才拼接进 CTE。API 侧的 `isValidResourceID` 也改为委托同一个实现，避免两处分叉。守卫测试见 `backend/api/v1/filter_injection_test.go`（`project` 过滤注入载荷）。

- **位置**：`backend/store/principal.go:233`（同型：`backend/store/group.go:111`）
- **证据**：
  ```go
  WHERE ((resource_type = '` + storepb.Policy_PROJECT.String() + `' AND resource = 'projects/` + *v + `') OR ...)
  ```
  `*v` 来自 `common.GetProjectID(value.(string))`（`api/v1/user_service.go:189-193`），而 `GetNameParentTokens` 只按 `/` 切分，不校验字符。
- **影响**：`project == "projects/x' OR '1'='1"` 可改写 CTE 谓词，绕过 `ListUsers` 的项目范围限制；该处 API 代码旁边就写着 `// TODO check permission`。`group.go:111` 目前无调用者设置 `ProjectID`，属潜伏。
- **修复**：把 project ID 作为参数绑定（`resource = $n`，值为 `"projects/"+*v`）；在 `common.GetProjectID` 校验字符；把两处共享的"项目/工作区成员 CTE"抽成一个函数，避免修复分叉。

---

## 高（High）

### S-H1. `enableCache=false` 导致每次用户查询全表扫描
> **✅ 已修复（阶段 2）** · `f22f61e`：经确认走"启用缓存"而非删除缓存。`store.New` 的 `enableCache` 参数已删除，所有缓存读取无条件生效；缓存 miss 不再加载全部用户，`GetUserByID`/`GetUserByEmail` 改为 `WHERE id/email` 定向查询后回填（`getUser`），`listAndCacheAllUsers` 删除。同时修掉被"关着读"掩盖的缺陷：`GetSettingV2` miss 时补写 `settingCache`；`GetSecret` 的导出可变字段 `Store.Secret` 改为 `secretMu` 保护的私有字段。

- **位置**：`backend/store/principal.go:89-114,180-201`、`backend/server/server.go:70`
- **证据**：
  ```go
  if v, ok := s.userIDCache.Get(id); ok && s.enableCache { return v, nil }
  if err := s.listAndCacheAllUsers(ctx); err != nil { ... }
  ```
  `listAndCacheAllUsers` → `listUserImpl(ctx, tx, &FindUserMessage{ShowDeleted: true})`，无 LIMIT，并带 `user_group` 的 JSONB 展开子查询。而 `store.New(ctx, profile.PgURL, false)` 恒传 `false`。
- **影响**：缓存读永远跳过、缓存写仍然发生。认证拦截器每个 RPC 调用一次 `GetUserByID`，即每个请求做一次全表扫描 + 每用户 JSONB 展开；登录、`UpdateUser`（两次）、`DeleteUser`、`UndeleteUser` 都触发。用户数上千后是 O(N·M)/请求。
- **修复**：`!s.enableCache` 时直接 `WHERE id = $1` / `WHERE email = $1`；只有启用缓存时才 `listAndCacheAllUsers`。**不要读被 flag 关闭却无条件写。**

### S-H2. `DeleteProject` 引用 15 张不存在的表
- **位置**：`backend/store/project.go:364-541`
- **证据**：`DELETE FROM query_history ...`；`LATEST.sql` 中不存在 `query_history`、`worksheet`、`issue*`、`plan*`、`pipeline`、`task*`、`sheet`、`release`、`changelist`、`db_group`、`project_webhook`。第一条语句即 `relation "query_history" does not exist`。
- **影响**：函数永远无法成功，是 Bytebase 清理流程的残留。
- **修复**：删除该函数及整个不可达的 project store API，或按实际 schema（只有 `db`/`project`）重写并接入调用方。

### S-H3. `table` 过滤器 join 不存在的 `db_schema` 表
> **✅ 已修复（阶段 1）** · `bb93ee0`：经确认这是不需要的遗留代码，处理方式是**删除**而非改写。`store/database.go` 里 `strings.Contains(filter.Where, "ds.metadata->'schemas'")` 的 join 注入、`listDatabaseImplV2` 的 `joinQuery` 变量，以及 `api/v1` 侧生成该片段的 `table` 分支一并移除；顺带发现"改为读 `db.metadata->'schemas'`"这条建议路径**走不通**——`db.metadata` 存的是 `DatabaseMetadata`，没有 `schemas` 字段。守卫测试 `TestListDatabaseFilterRejectsTableFilter` 锁住"table 过滤器返回 InvalidArgument"。

- **位置**：`backend/store/database.go:367-369`（修复前行号，同 `04` A-C2 的 API 侧）
- **证据**：`if strings.Contains(filter.Where, "ds.metadata->'schemas'") { joinQuery = "INNER JOIN db_schema ds ..." }`，而 `db_schema` 在仓库中不存在。API 的 `table = "x"` 与 `table.matches("x")` 都会生成 `ds.metadata->'schemas'`。
- **影响**：`ListDatabases` 的按表搜索功能 100% 运行时报错。
- **修复**：去掉该 join，改为从 `db.metadata->'schemas'` 读取；或真正引入 `db_schema` 表。按 `meta_resource_test.go` 的模式补 guard 测试。

### S-H4. `UpdateDatabase` 可能 nil deref
> **✅ 已修复（阶段 1）** · `20e284b`：`MetadataUpdates` 分支在 `proto.CloneOf(database.Metadata)` 之前判空，返回 `common.Errorf(common.NotFound, "database %q not found")`。注意 M5（元数据更新仍是跨事务读-改-写）仍未修。

- **位置**：`backend/store/database.go:248-256`
- **证据**：`md := proto.CloneOf(database.Metadata)`，而 `GetDatabaseV2` 未命中返回 `(nil, nil)`（`database.go:90-92`）。调用方是 `runner/schemasync/syncer.go:520-524`（每次数据库同步）。
- **影响**：缺失行时 panic 整个进程。
- **修复**：先判空并返回 `common.Errorf(common.NotFound, ...)`。

### S-H5. 枚举被当作 text 参数 → 过滤静默失效
> **✅ 已修复（阶段 6）** · `824232a`：`engineFilterValue` 改为输出 `storepb.Engine_name[...]` 枚举名，`store/database.go`/`store/instance.go` 的谓词按存储的枚举名做字符串比较，并补了表驱动 guard 测试。
- **位置**：`backend/store/database.go:391-393`；API 侧 `api/v1/instance_service.go:103-104`、`api/v1/database_service.go:802-803`；`store/policy.go:249,266-272,303-307`
- **证据**：`args = append(args, *v)`，`*v` 是 `storepb.Engine`（int32），lib/pq 会发送文本 `"3"`，而 `metadata->>'engine'` 存的是 protojson 枚举名（如 `"MYSQL"`，见 `stats.go:157,172-176`）→ 谓词变成 `'MYSQL' = '3'`，恒 false（不报错）。
- **影响**：instance/database 的 engine 过滤静默返回空；`UpdatePolicyV2` 返回 `(nil,nil)`、`DeletePolicyV2` 报成功但什么都没删（当前无调用者，属陷阱）。
- **修复**：统一传 `.String()`（对照正确的 `parseToEngineSQL`，`database_service.go:745-749`）；`policy.go` 同样改用 `.String()`，或删除未用方法。

### S-H6. 用户创建无唯一性约束/检查
> **✅ 已修复（阶段 2）** · `ff9b22a`：增量 `0.1.0002` 加 `CREATE UNIQUE INDEX idx_principal_unique_email ON principal (LOWER(email)) WHERE deleted = FALSE`，`LATEST.sql` 同步。`CreateUser`/`UpdateUser` 把 23505（同时识别 `*pgconn.PgError` 与 `*pq.Error`）映射为 `common.Conflict`，handler 经 `connectErrorForWrite` 转成 `CodeAlreadyExists`。**未做去重迁移**：项目尚未上线、无历史重复数据，受影响部署会直接迁移失败而非静默停用账号。

- **位置**：`backend/store/principal.go:329-334`、`LATEST.sql:18-31`、`api/v1/user_service.go:286-384`
- **证据**：`CreateUser` 只校验小写；`email text NOT NULL` 无唯一索引；API 也未预检查。
- **影响**：可创建重复邮箱账号；`GetUserByEmail` 从"最后一次缓存的重复行"中任取其一，登录可能绑定到错误账号，改密可能改错行。
- **修复**：在 `LATEST.sql` + 增量中加唯一索引（建议 `LOWER(email) WHERE deleted = FALSE`），冲突映射为 `common.AlreadyExists`。（注：本仓库实际错误码是 `common.Conflict`，没有 `AlreadyExists`；API 侧的 Connect 码才是 `CodeAlreadyExists`。）

---

## 中（Medium）

- **M1. `GetResourcesUsedByRole` 把 text 列 scan 进枚举**：`store/role.go:56-59`，`policy.resource_type` 是 `text`（如 `"PROJECT"`），scan 进 `storepb.Policy_Resource`（int32）必然报 `converting driver.Value type string to a int32`。整个 `role.go` 无调用者。
- **M2. `UpdateGroup` 泄漏事务**：`store/group.go:210-215` 没有 `defer tx.Rollback()`（包内其它事务都有）。`protojson.Marshal` 或 Scan 失败即返回存活事务占用连接。该路径由 `auth_service.go:394` 可达。 —— **✅ 已修复（阶段 6）** · `a71716a`：`UpdateGroup` 补上 `defer tx.Rollback()`，并对 `backend/store` 全量扫描，保证所有 `BeginTx` 后都跟随回滚、单语句查询不再开事务。
- **M3. 批量 upsert 的 `RETURNING id` 位置假设**：`store/meta_resource.go:934-956` 按行序写入 `creates[i]`；PostgreSQL 不保证 `INSERT ... ON CONFLICT DO UPDATE ... RETURNING` 的顺序，且未检查返回行数是否等于输入数。错误 ID 会进入按 ID 索引的缓存。 —— **✅ 已修复（阶段 6）** · `7f0e3a0`：批量 upsert 改为 `RETURNING id, guid, object_type`，按 `(guid, object_type)` 回填 `creates[i].ID` 并校验返回行数与输入一致。
- **M4. `UpdateUser` 原地修改缓存 profile，且可能漏记改密时间**：`store/principal.go:405-421`，`patch.Profile = currentUser.Profile` 直接写穿缓存对象（并发 data race）；若调用方自己传了 `patch.Profile`，则 `LastChangePasswordTime` 完全不设置。
- **M5. `UpdateDatabase` 元数据更新是非原子的读-改-写**：`store/database.go:248-288`，`GetDatabaseV2` 提交自己的只读事务后，另一个事务执行 UPDATE，无行锁；`syncer.go:498-527` 还在持有外层事务时调用它。 —— **✅ 已修复（阶段 6）** · `eca3686`：元数据更新改为在同一事务内对行加锁（`SELECT ... FOR UPDATE`），不再跨事务读-改-写。
- **M6. `closeOpenMetaRegistryHistory` 逐行 UPDATE**：`store/meta_resource.go:634-645`，N 次往返；而对应的 history 插入已用 `UNNEST` 批量化。 —— **✅ 已修复（阶段 6）** · `3e6fbda`：`closeOpenMetaRegistryHistory` 改为单条 `UPDATE ... FROM unnest($1,$2)` 按键配对批量关闭。
- **M7. open-history 查询是 `ANY/ANY` 笛卡尔谓词**：`store/meta_resource.go:603-609`，`guid = ANY($1) AND object_type = ANY($2)` 会匹配未请求的 `(guid,type)` 组合。 —— **✅ 已修复（阶段 6）** · `7f0e3a0`：`guid = ANY($1) AND object_type = ANY($2)` 改为按键配对的 row-value 谓词，不再匹配未请求的 `(guid, type)` 组合。
- **M8. 子层级元数据列表无法翻页**：`store/meta_resource.go:51-55,698-709`，`FindSubLevelMetaRegistryResourceMessage` 没有 Offset，API 算了 offset 却只是切片，第 2 页返回第 1 页（见 `04` A-M2）。
- **M9. 元数据历史查询无界**：`store/meta_resource.go:486-528` 支持 Limit/Offset，但唯一调用方（`database_history.go:48-51,93-96`）不传，导致全量历史（含完整 JSONB）加载后在内存分页。 —— **✅ 已修复（阶段 6）** · `3e6fbda`：`ListMetadataHistory` 把 `Limit/Offset` 下推到 store，按「`offset+limit+2` 行、`ORDER BY valid_from DESC`」有界探测，调用方不再全量加载后在内存分页。
- **M10. 元数据搜索是全 JSONB 扫描且无索引**：`store/meta_resource.go:262-279`，`meta_registry_resource` 只有 `(guid,object_type)` 唯一索引，没有 `metadata` 的 GIN/表达式索引；而 `manual_sql` 有 `search_vector` GIN。**◐ 阶段 2（`ff9b22a`）**：加了 `object_type` 索引（服务 `queueAll` 的扫描），`metadata` GIN **经确认不加**——搜索谓词是 `inner_meta->>'name' ILIKE '%x%'`，GIN 索引无法服务子串匹配，且全仓库没有任何 jsonb `@>` 包含查询可以让它生效；要真正走索引需要改成全文检索或 `pg_trgm`，属行为变更，留待后续。 —— **✅ 已修复（阶段 3 补遗，`f50fbbc`）**：新增 `search_text` 存储生成列（恰为 name/title/comment/userComment 拼接）+ `pg_trgm` GIN 索引（增量 `0.1.0005`），谓词改为 `search_text ILIKE $n`，匹配行与旧 `jsonb_each` 谓词逐关键字对拍一致；空 `SearchStr` 改为 `common.Invalid`。
- **M11. meta 缓存 key 只用 GUID，忽略 ObjectType**：~~`store/meta_resource.go:103-107,121-124`（缓存定义 `store.go:29`），唯一约束是 `(guid,object_type)`；`MANUAL_SQL` 与 `TABLE` 可能共享四段 GUID 形状。当前被 `enableCache=false` 掩盖，一旦开启缓存即成错误结果 bug。~~ —— **✅ 已修复（阶段 2，`f22f61e`）**：GUID 缓存改为 `lru.Cache[MetaGUIDKey, ...]`，读路径经 `metaRegistryGUIDCacheKey` 只在调用方同时指定 `object_type` 时命中（GUID-only 查询直接打库）；写路径统一用 `GUIDKey()`。另：`BatchCreateMetaRegistryResourceAt`/`BatchDeleteMetaRegistryAt` 不再在调用方事务内改缓存，改为提交后由 `InvalidateMetaRegistryCache` 失效（`syncer.go` 调用）。
- **M12. `PatchWorkspaceIamPolicy` 原地修改缓存策略**：~~`store/policy.go:41-74`，`GetWorkspaceIamPolicy` 返回 `policyCache` 中的指针，循环在 upsert 前编辑其 bindings；并发读者可见半更新状态，失败时缓存永久不一致。~~ —— **✅ 已修复（阶段 0，`5b19778`）**：`PatchWorkspaceIamPolicy` 现在开启事务并调用新的 `(s *Store) patchWorkspaceIamPolicyImpl(ctx, txn, patch)`；实现通过 `listPolicyImplV2` 在事务内**重新读取并反序列化**到独立对象（不再触碰缓存指针），提交后再 `policyCache.Remove(...)` 并重新读取。`store.CreateUser` 也在同一事务里调用该 impl，因此首个管理员授予与用户插入原子。注意 `GetPolicyV2` 命中缓存时仍返回共享指针（若开启缓存需另行处理）。
- **M13. `Store.Secret` 懒初始化 data race**：`store/setting.go:155-168` 无锁读写导出字段 `s.Secret`，而 `Store` 被所有请求 goroutine 共享；`obfuscateInstance`/`unObfuscateInstance` 每行都调用。**建议 `store.New` 时用 `sync.Once` 初始化，并停止导出可变字段。** —— **✅ 已修复（阶段 2，`f22f61e`）**：字段改为不导出的 `secret`，由 `secretMu` 保护；未命中时读 `AUTH_SECRET` 并缓存。没有用 `sync.Once`，因为互斥锁版本在瞬时 DB 失败后可以重试，而 `Once` 会把错误永久缓存。
- **M14. namespace mapping 部分更新会清空 `database_name`**：`store/namespace_mapping.go:115` 无条件设置，而 `namespace`/`instance_resource_id` 只在非空时设置；只更新 namespace 会清掉 database，破坏 OpenLineage 解析。
- **M15. `CreateManualSQL` upsert 改变 GUID 却不清理旧 GUID 的镜像/血缘**：`store/manual_sql.go:449-450`，冲突键 `(instance, database, name)` 不含 `schema_name`，而 GUID 含 schema；`CreateManualSQL`（221-251）不像 `UpdateManualSQL`（348-355）那样删除被取代 GUID 的 `meta_registry_resource` 与 `column_lineage`，留下孤儿行。 —— **✅ 已修复（阶段 6）** · `9c69196`：`CreateManualSQL` 因 `schema_name` 变化而更换 GUID 时，清理旧 GUID 的 `meta_registry_resource`、history 与 `column_lineage`（与 `UpdateManualSQL` 对齐）。
- **M16. store 的 not-found 错误不带 `common.Code` → API 返回 500**：`store/manual_sql.go:301,414,523`、`namespace_mapping.go:137,159`、`openlineage_api_key.go:147` 等用裸 `errors.Errorf`，调用方统一包成 `CodeInternal`。应为 `common.Errorf(common.NotFound, ...)`。
- **M17. `updateIdentityProviderImpl` 在无字段可改时生成非法 SQL**：`store/idp.go:175-197`，`UPDATE idp SET  WHERE ...`；另外 `err == sql.ErrNoRows` 未用 `errors.Is`（213）。
- **M18. IDP 密钥明文存储**：`store/idp.go:25-37,72-89`，`protojson.Marshal` 后原样写入 `idp.config`（含 OAuth2 client secret / LDAP bind password）。
- **M19. `explain_sql_cache` 永不过期**：`store/explain_sql.go:75-91` 无时间谓词，`UpsertExplainSQLCache` 接受调用方传入的 `created_at`；表无 TTL 列。 —— **✅ 已修复（阶段 2，`8c34542`）**：`GetExplainSQLCache` 加 `created_at > now()-7d` 谓词（`ExplainSQLCacheTTL`），过期即视为 miss 并由下一次生成覆盖；同时增量 `0.1.0001` 增加 `scope` 列（缓存 key 现在也含 scope/provider/model）。`expired` 标记服务端仍不设置——过期条目根本不会被返回。
- **M20. `GetOrCreateExternalDataset` 每次解析都写库，且已有行不更新 `dataset_type`**：`store/external_dataset.go:53-57`。 —— **✅ 已修复（阶段 6）** · `99d41ea`：`GetOrCreateExternalDataset` 在 `dataset_type` 未变时不再写库，变化时只更新该列。
- **M21. `external_dataset.schema_fields` 从无写入者**：唯一 INSERT（53-57）不含该列，`FindExternalDatasetByGUIDs`（`openlineage_api_key.go:181`）仍在读取；`lineage_service.go:121` 永远拿到空 `SchemaFields`。 —— **✅ 已修复（阶段 6）** · `0151bcb`：按 B14 的"择一"取删除方案——无写入者的 `schema_fields` 列与读取/扫描路径一并删除（增量 `0.1.0008`），不再有"永远为空"的假数据。
- **M22. OpenLineage task 聚合在每个事件上全量重算**：`store/openlineage_task.go:109-166`，`COUNT(*) OVER () ... FROM openlineage_run WHERE task_guid = $1`，每个事件 O(runs)；外加每事件一次 registry upsert + history 行。 —— **✅ 已修复（阶段 6）** · `e7d15eb`：`openlineage_task` 计数改为增量——先取 task 行锁串行化同一 task 的写入，再按唯一键点查旧 `has_lineage` 得到本次增量，latest 字段就地比较，批量内按 task GUID 稳定排序避免死锁。
- **M23. `SearchAuditLogs` 接受调用方提供的 WHERE 片段**：`store/audit_log.go:64-67`。当前安全（唯一构造器白名单化变量并参数化），但 store API 接受任意 SQL 文本是安全路径上的隐患。
- **M24. 审计时间可由调用方设置**：`store/audit_log.go:35-39`，`cloned.CreateTime` 可回填；当前拦截器不设置，但 store 允许伪造；且无完整性保护（无哈希链）。
- **M25. `ValidateOpenLineageAPIKey` 是 O(N) bcrypt 扫描 + 每请求写**：`store/openlineage_api_key.go:67-103`（详见 `04` B-H6）。 —— **✅ 已修复（阶段 6）** · `415e16e`：新增 `key_digest`（SHA-256 hex，唯一索引）定向查询后再 bcrypt 比对，校验从 O(N) 全表扫描降为一次点查 + 一次比对；每个请求仍会同步写一次 `last_used_at`（有意保留"最后使用时间"语义，代价降为单行 UPDATE）。
- **M26. OpenLineage run/task 列表无默认 LIMIT**：`store/openlineage_run.go:306-311`、`store/openlineage_task.go:251-256`（详见 `04` B-H1）。**部分修复（阶段 2，`8c34542`）**：数据集页/详情两个端点已传 `Limit: 5000`；store 层仍只在 `Limit != nil` 时加 LIMIT，run 列表端点仍依赖请求参数。 —— **✅ 已修复（阶段 6）** · `311e790`：`openLineagePageClause` 在 `Limit == nil` 时也施加 5000 默认上限，run/task 两个列表共用。

---

## 低（Low）／代码质量

- **单语句查询也开只读事务**：`idp.go:107,134`、`manual_sql.go:270`、`stats.go:23,58,76,103,127,150`；`GetManualSQL` → `ListManualSQL` 为单行做 5 次往返，且每次 Create/Update 都重新读。
- **`find` 结构体被 getter 副作用修改**：`project.go:58`、`instance.go:54`、`policy.go:165`、`meta_resource.go:705-707`（如 `find.ShowDeleted = true`），复用同一指针的调用方语义被静默改变。
- **`ListResourceFilter`/`ExtraArgs` 是无防护的裸 SQL 通道**：`store/common.go:32-40`、`meta_resource.go:332-335`；边界只靠约定。
- **`BatchUpdateDatabases` 用 `environment = ''` 而非 NULL**：`database.go:304-306`，读路径 `COALESCE('', instance.environment)` 得到 `''` 而非继承实例环境，与缓存分支（341-352）不一致。
- **`BatchUpdateDatabases` 无界的 OR 列表**：`database.go:316-327`，每库 2 个参数，逼近 PG 65535 上限。
  - **阶段 3 续更正（`451cb78`）**：`BatchUpdateDatabases` 已随 project 一起删除（唯一调用者是 `DeleteInstance.force`），上面两条 `environment = ''` 与无界 OR 列表的发现不再适用于当前代码。
- **`unObfuscateInstance` 每行重新取 secret 并重复解码**：`instance.go:260`。 —— **✅ 已修复（阶段 6）** · `390a66c`：`unObfuscateInstanceWithSecret` 让整页实例只解析一次 secret，不再逐行 `GetSecret` 取锁。
- **store 错误普遍绕过 `common.Code`**：`meta_resource.go:117,188,942,950`、`instance.go:58,64`、`database.go:94`、`group.go:67`、`project.go`、`role.go:199` 等。
- **`UpdateInstanceV2` 不做 data source 校验**：`instance.go:97`（create 有，update 没有），可持久化 0 个或多个 ADMIN 数据源。**✅ 已修复（阶段 1）** · `20e284b`：在 API 层补齐——`checkInstanceDataSources`（create/update 两条路径共用）现在要求恰好一个 ADMIN，并保留 ID 唯一性校验，返回 `CodeInvalidArgument`（守卫测试 `TestCheckInstanceDataSourcesRequiresOneAdmin`）。store 的 `UpdateInstanceV2` 本身仍不校验，绕过 API 的调用方不受保护。
- **`systemBotUser` 回退对象与种子行不一致**：`principal.go:22-27` 用 `SYSTEM_BOT@example.com`（大写），而 `LATEST.sql:114` 种子是 `support@example.com`。 —— **✅ 已修复（阶段 6）** · `e7239db`：`systemBotUser` 回退对象改为与 `LATEST.sql` 种子行（`support@example.com`）对齐。
- **`CountIssues` 查询不存在的 `issue` 表**：`stats.go:126-145`；`CountActiveUsers` 有不可达的 `sql.ErrNoRows` 分支（88-92）；`id > 101` 魔法偏移是 Bytebase 播种遗留。（`CountUsers` 已在阶段 0 补上 `principal.deleted = FALSE`，见 `02` C2。）
- **LIMIT/OFFSET 用 `Sprintf` 插值**：`manual_sql.go:577-582`、`column_lineage.go:162-167`、`openlineage_run.go:306-311`、`openlineage_task.go:251-256`。不可注入（Go int），但与其它地方不一致，且负值会得到原始 PG 错误。
- **`ManualSQLID` 是幻影字段**：`manual_sql.go:481,527`，无 `manual_sql_id` 列，filter 映射到 `name`。
- **`ListOpenLineageAPIKey` 仍 select `key_hash`**：`openlineage_api_key.go:105-108`，注释却说 "without hashes exposed"；当前 `convertAPIKey` 不返回，但未来通用序列化会泄漏。
- **task registry 快照无法表达 `LatestEventType`**：`openlineage_task.go:245,291,308-331` 与 `OpenLineageTaskSummary`（`openlineage.proto:76-96`）不一致。
- **`transformation` JSONB 用 `encoding/json` 而非 protojson**：`column_lineage.go:102,196`，与 JSONB 约定不符，转 proto 后会静默失配。
- **多个 list 函数未判 `find` 是否 nil**：`column_lineage.go:131`、`openlineage_run.go:246`、`openlineage_task.go:206`、`external_dataset.go:87`、`namespace_mapping.go:70`、`llm.go:174`。
- **LLM debug log 无保留策略**：`explain_sql.go:66-72` + `component/llm/debug.go:14-18`，fire-and-forget goroutine + `context.Background()` 写完整 prompt/响应。
- **Get 类函数不拒绝多行匹配**：`openlineage_run.go:232-241`、`openlineage_task.go:191-201`、`external_dataset.go:73-82` 静默取第一行，与 `GetManualSQL`（262-264）和 `GetIdentityProvider`（`idp.go:123-125`）不一致。
- **OpenLineage API key 无 scope/过期**：`openlineage_api_key.go:29-63`。

---

## 死代码与遗留债务

- **`store/role.go` 整个文件无调用者**（`CreateRole`/`GetRole`/`ListRoles`/`UpdateRole`/`DeleteRole`/`GetResourcesUsedByRole`）；`rolesCache`（`store.go:32,77,94`）只为它存在；`role` 表无任何 API 引用。
- **`store/project.go` 的 store API 全部无调用者**（`GetProjectV2`/`ListProjectV2`/`CreateProjectV2`/`UpdateProjectV2`/`BatchUpdateProjectsV2`/`DeleteProject`），文件是 Bytebase 残留（注释掉的 creator/policy/webhook 代码）。
- **`CreateGroup`/`DeleteGroup`**（`group.go:166-207,269-286`）无调用者。
- **`UpdatePolicyV2`/`DeletePolicyV2`/`ListPoliciesV2`**（`policy.go:192-318`）无调用者，且两个 mutator 已损坏（枚举当 text）。
- **`store/common.go` 几乎全死**：`RowStatus`/`Normal`/`Archived`/`SortOrder`/`ASC`/`DESC`/`OrderByKey` 无引用。
- **`withMetadata=false` 分支不可达**：`meta_resource.go:355-366,430-441`（两个调用方都传 `true`）。
- **未使用的请求字段**：`FindMetaRegistryResourceMessage.ID`/`IDList`/`ExcludeObjectType`；`FindMetaRegistryHistoryMessage.OrderDesc`/`TransitionTime`/`ValidFrom`/`Limit`/`Offset`。
- **`GetMetaRegistryAsOf`/`ListSublevelMetaRegistryResourceAsOf`** 仅集成测试使用。
- **`UserProfile.source`** 被读（`user_service.go:700`）但从不写，且每次登录被清空。
- **死导出函数**：`DeleteColumnLineageByMeta`（`column_lineage.go:241`）、`QueryColumnLineageSources`/`Targets`（`explain_sql.go:24,45`）、`CheckDatabaseUseEnvironment`（`environment.go:24`）、`MarshalOpenLineageRunPayload`（`openlineage_run.go:403`）。
- **`openlineage_api_key.go:167-207` 放着无关的 `FindExternalDatasetByGUIDs`**，位置错误。
- **仅测试使用的 query builder**：`manual_sql.go:384-387` 的 `buildDeleteManualSQLMetaRegistryStatement`，生产走的是另一条 history-aware 路径；该 guard 测试给出虚假信心。
- **IDP 的 Create/List/Update/Delete 无 API 调用**（只有 `GetIdentityProvider` 被 `auth_service.go:236` 使用）；容量为 4 的 `idpCache` 实际只读；`Store.DeleteCache`（`setting.go:96-101`）不清 `idpCache`/`instanceCache`/`metaRegistryCache`，且自身无调用者。
- **`db_connection.go:16,22` 的 `stopWatcher` 未使用**；`_ "github.com/jackc/pgx/v5"` 冗余。
- **`V2` 命名**：`GetSettingV2`/`UpsertSettingV2`/`CreateSettingIfNotExistV2` 等与无 V2 版本并存。
  - **阶段 3 续更正（`8b328ae`）**：12 个 `*V2` 方法与 impl helper 已去掉后缀，`setting.go` 里现在是 `GetSetting`/`UpsertSetting`/`CreateSettingIfNotExist`；`database.go`/`instance.go`/`policy.go` 同理。
- **`external_dataset.schema_fields` + `schemaFieldsScanner`** 从无写入者。

---

## 待确认

1. **`lib/pq` 数组编码**（`column_lineage.go:113-122`）：`pq.Array([]storepb.MetaType)` 不匹配 `case []int32`，会走 `GenericArray`；配合 pgx stdlib + 显式 `::int[]` 转换应可用，但需要集成测试确认。
2. **GUID 与冲突键安全性**：`guid = EXCLUDED.guid` 目前安全（GUID 由冲突键元组派生），但 `buildOpenLineageScopedGUID` 用 `:` 连接 `url.PathEscape` 后的片段，而 `PathEscape` 不转义 `:`，对抗性命名可能碰撞 → 唯一约束冲突。
3. **保留策略**：`openlineage_run`/`openlineage_task`/registry history/`audit_log`/`llm_debug_log` 都没有删除路径，需确认是否有意如此。
4. **`RETURNING` 顺序**（`meta_resource.go:934-956`）：未实测，建议无论顺序如何都改为按 `(guid, object_type)` 匹配。
5. **`listOpenMetaRegistryHistoryByKey` 笛卡尔谓词**（`meta_resource.go:603-609`）：未能构造实际故障场景（结果 map 会按真实 key 重新索引），属谓词 bug。
6. **`enableCache` 是否计划在某处开启**？~~全部调用点传 `false`，需明确"启用"或"删除缓存"。~~ —— **阶段 2 已关闭（`f22f61e`）**：经确认启用缓存，`enableCache` 参数与字段一并删除，缓存读取无条件生效。
