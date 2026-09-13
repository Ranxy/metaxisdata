# 07 · common / utils / config / metric 共享基础包

**范围**：`backend/common/`（`error.go`、`context.go`、`const.go`、`guid.go`、`config.go`、`utils.go`、`resource_name.go`、`cel.go`、`cel_attributes.go`、`log/`、`stacktrace/`）、`backend/utils/`、`backend/config/`、`backend/metric/`。

**结论**：这一层代码量小但"死代码占比"最高（约 60%）。两个真正有影响的横切问题：① `common.Code` 约定完全没有执行链路（没有拦截器把 `common.Code` 映射成 Connect 状态码），所以 AGENTS.md 的错误码规范目前是空文；② `Obfuscate/Unobfuscate` 的"加密"是同库密钥 XOR。其余主要是 Bytebase 时代的枚举/常量/helper 残留，以及日志系统未接线（见 `01` H4）。

**阶段 0 更新**：本层仅有的改动是 `common/resource_name.go` 新增并导出了被 store/API 复用的 `IsValidResourceID`（配合 SQL 注入修复，`3321801`）。U-H1（错误码映射）与 U-H2（XOR 混淆）**未处理**；`Profile.Secret` 接线、JWT 解析校验等修在其他层（见 `05`、`02`）。

**阶段 1 更新**：M3 ✅（`ff914ac`，CEL 类型断言不再 panic，统一 `InvalidArgument`）、低节"日志系统未接线" ✅（`7fdcead`）。U-H1/U-H2、M1（CEL condition fail-open）等仍未处理。

**阶段 2 更新**：M13 相关的 `Store.Secret` 竞态 ✅（改为私有字段 + 互斥锁，`f22f61e`）；`enableCache` 待确认项已关闭（启用缓存，见下）。U-H1（`common.Code`→Connect 映射）与 U-H2（XOR 混淆）仍未处理。
**阶段 3 更新**：U-H1 ✅（新增 `api/v1/ErrorMappingInterceptor`，`common.Code` 首次真正映射为 Connect 状态码；`common.Error` 补 `Unwrap()`；`connectErrorForWrite` 退役，`89baa3a`）、U-H2 ✅（AES-256-GCM + 环境密钥 + 随机 nonce + 版本前缀，`a1faf65`）、M6 ✅（`Error()` 不再对 nil cause panic、可 `Unwrap`、`errors.Join` 链可穿透）、死代码清单 ✅（`cel.go` 的 8 个 helper/6 个变量、`cel_attributes.go` 的 15 个常量、`error.go` 的 `Wrap`/`Wrapf`/`Code.Int`/`Code.Int32` 与 migration/task 错误码族、`resource_name.go` 的 26 个符号、`const.go` 的死常量、`utils/collection.go`、`utils/member.go` 的注释块与 `MemberContainsUser`、整个 `metric` 栈均删除；`e42b9ac`）。**M1（CEL 条件 fail-open）仍未处理**：`EvalBindingCondition` 对引用 `resource.*` 的表达式仍返回 true；当前 binding 构造不带 condition，属潜伏，阶段 3 未动其语义。**M2（每次求值新建 CEL env）仍未处理**。

**阶段 3 收尾更新**：本轮**未改 `common` 的行为**；只修掉 `backend/common/utils_test.go` 里 `TestObfuscateRoundTrip` 的 flaky 断言——`NotContains(ciphertext, plaintext)` 对短明文（如 `"a"`）会随机命中 base64 密文，`-race` 全量跑时确实失败过一次，改为比较整体是否相等（`4afe1ba`）。

**阶段 3 续更新**：`common.GetProjectID`、`FormatProject`、`ProjectNamePrefix`、`DefaultProjectID` 随 project 移除而删除（`451cb78`）；`IsValidResourceID` 与 `AllUsers` 仍有调用者，保留。

**阶段 3 补遗更新**：M1（CEL 条件 fail-open）✅（`6f2b63d`）：求值为 residual（引用未绑定的 `resource.*`）时不再返回 true，而是返回错误、由 `validateIAMBinding` 丢弃该 binding；M2（每次求值新建 CEL env）✅（改为 `sync.OnceValues` 构建一次）。`common.GUIDPrefix` ✅（`6f2b63d`）：原来按点号切分，而所有 GUID 用 `;` 拼接，因此对真实 GUID 恒返回空串——`GetSchemaString` 用它做 sequence 子树前缀，导致 PostgreSQL 表的 `ALTER SEQUENCE ... OWNED BY`/identity DDL 一直缺失；现在按 `MetaGUIDSplit` 去掉最后一段，并补了 `guid_test.go`。

**阶段 3 收尾二更新**：`resource_name.go` 新增两组资源名 helper：OpenLineage 侧 `FormatNamespaceMapping`/`GetNamespaceMappingID`、`FormatOpenLineageRun`/`GetOpenLineageRunGUID`、`FormatOpenLineageTask`/`GetOpenLineageTaskGUID`、`FormatAPIKey`/`GetAPIKeyID`（`c2a67e0`），DataSource 侧 `FormatDataSource`/`GetInstanceDataSourceID`（`513940f`）。四个 pattern 与 `FormatInstance` 共用同一命名风格，解析侧共用 `GetOpenLineageToken`/`GetOpenLineageIntID`：**畸形、缺段、带多余斜杠或不属于该实例的名字都在任何 store 调用之前返回错误**（handler 转成 `InvalidArgument`）。`utils_test.go` 里补了往返 + 非法名拒绝的表驱动测试；同一文件加上兄弟测试文件已有的 `//nolint:revive` 包名标记（包名 `common` 本身触发 revive 的 package-naming）。

**阶段 5 更新**：U-H2 被回滚（`7870016`）——`Obfuscate`/`Unobfuscate` 回到 `AUTH_SECRET` 种子 XOR（删除 AES-GCM、`v1:` 前缀与 `newAEAD`），`store` 侧的"同库密钥"问题重新成立，U-H2 状态从 ✅ 退回待办。`config/profile.go` 删除 `ExternalURL`/`EncryptionKey`/`OpenLineageRetentionDays`，`Secret` 改为启动时从数据库 `AUTH_SECRET` 解析的运行时值；`JWT_SECRET` 环境变量已不存在。本文件开头提到的"`AUTH_SECRET` 为空时 `Obfuscate` 除零"现在由 `GetSecret` 的空值检查兜住（`Obfuscate` 自身仍无保护，但除 `store` 外无调用者）。


---

## 高（High）

### U-H1. `common.Code` 从不映射为 Connect 状态码，`ErrorCode` 是死代码
- **位置**：`backend/common/error.go:87-94`、`backend/server/grpc_routes.go:80-88`、`backend/api/v1/audit.go:281,297`
- **证据**：`ErrorCode` 全仓库零调用；拦截器链只有 Debug/Auth/Audit；`mapSeverity`/`buildAuditStatus` 只看 `*connect.Error`。
- **影响**：store 返回的 `&common.Error{Code: common.NotFound}`（如 `store/idp.go:214`、`setting.go:194`、`group.go:67`）到客户端变成 `CodeUnknown`/HTTP 500；`Wrap`/`Wrapf` 零调用；`Error` 没有 `Unwrap()`，`errors.Is/As` 无法穿透。
- **修复**：增加把 `common.ErrorCode(err)` 映射为 connect code 的拦截器，或删除 `common.Error`/`ErrorCode` 统一在 API 边界用 `connect.NewError`。补 `func (e *Error) Unwrap() error`。

### U-H2. 凭据混淆的密钥与密文同库，且无完整性校验
> **◐ 部分修复（阶段 6）** · `f112e5c`：`common.Obfuscate`/`Unobfuscate` 补齐 `seed == ""` 防护——空 seed 且输入非空时返回明确错误（不再整数除零），空输入原样往返为空串。**剩余**：按本轮决策保持 `AUTH_SECRET` 种子 XOR，同库密钥与无完整性校验的取舍写入文档，本阶段不改。
- **位置**：`backend/common/utils.go:64-84`、`backend/store/setting.go:155-168`、`backend/server/init.go:29-38`
- **证据**：
  ```go
  obfuscated[i] = b ^ seedBytes[i%len(seedBytes)]   // 重复密钥 XOR + base64
  ```
  seed = 同一 PG `setting` 表的 `AUTH_SECRET`。
- **影响**：见 `05` C-H3。任何 DB 读取即可还原全部实例密码/SSH 私钥/SSL key/LLM key；轮换 seed 会静默解出乱码；相同明文等值泄漏；`seed == ""` 且输入非空时除零 panic（`utils.go:69,82`）。
- **修复**：AES-256-GCM/`secretbox` + 环境/KMS 密钥 + 随机 nonce + 版本前缀；`seed == ""` 返回错误。

---

## 中（Medium）

- **M1. CEL 条件 fail-open**：`common/cel.go:257-299`，`if !celtypes.IsBool(out) { return true, nil }`；env 声明 `resource.database/schema_name/table_name`（`:46-49`）但 `EvalBindingCondition` 只绑定 `request.time`（`:250-255`）。`utils/member.go:18` 在活跃路径上对每个 binding 调用它；引用 `resource.*` 的 condition 会被当作满足，角色被全局授予。当前 binding 构造不带 condition（`store/policy.go:68`），属潜伏。**修复**：绑定真实资源属性或返回"无法求值"并拒绝；要求编译结果为 bool。 —— **✅ 已修复（阶段 3 补遗，`6f2b63d`）**：residual 不再当作满足，改为返回错误由调用方丢弃 binding；守卫测试见 `backend/common/cel_test.go`。
- **M2. 每次 binding 求值都新建 CEL 环境**：`common/cel.go:262`，`cel.NewEnv` 在 `validateIAMBinding`（`utils/member.go:17-24`）里逐 binding 调用；`GetUserFormattedRolesMap` 每请求遍历所有 policy 的所有 binding。建议包级 `sync.Once` 构建一次；并在单次 `GetUserIAMPolicyBindings` 内 memoize group 查询（`member.go:148`）。 —— **✅ 已修复（阶段 3 补遗，`6f2b63d`）**：环境用 `sync.OnceValues` 构建一次，program 仍是每次求值单独构造。
- **M3. 未检查类型断言导致 panic**：`common.go:441-467` 的 `getVariableAndValueFromExpr` 返回 `any`，调用方（`user_service.go:165,168,171,182,189,215`、`instance_service.go:78,81,84,91,98,106,109,112`、`database_service.go:741,808,833,865`）直接 `value.(string)`。`filter=email == 1` 会 panic → `connect.WithRecover` 转成 500 并回传堆栈。（**✅ 已修复（阶段 1）** · `ff914ac`）
  - **更正一个此前的猜测**：`expr.AsCall()` 在 cel-go v0.26.1 中是 Kind 守卫的，返回 `nilCall` 哨兵，**不会 panic**（见 `common/ast/expr.go:336-341`）。但 `expr.AsLiteral()` 对非字面量返回 **nil**（`expr.go:357-362`），因此 `args[0].AsLiteral().Value()`（`user_service.go:248`、`instance_service.go:141`、`database_service.go:865`）会 panic；`expr.AsCall().Target().AsIdent()`（`instance_service.go:136`、`database_service.go:860`）在 `Target()` 为 nil 时也会 panic。修复：comma-ok + `Kind() == LiteralKind` 判断 + 返回 `InvalidArgument`。
  - **修复落地**：`getVariableAndValueFromExpr` 改为返回 `(variable, value, error)`，缺变量或缺字面量即 `InvalidArgument`；新增 `filterString`/`filterBool`/`filterStringList`/`matchArgs` 四个带检查的取值 helper（`api/v1/common.go`），所有调用方改用它，`.matches()` 的目标标识符与参数一律经 `matchArgs` 校验（`Target() == nil || Kind() != IdentKind` 与 `Kind() != LiteralKind` 都返回错误）。守卫测试 `backend/api/v1/filter_type_safety_test.go` 覆盖 `email == 123`、`engine in [1]`、`name.matches(ident)`、裸 `matches("x")`、`exclude_unassigned == "true"` 等 12 个用例。
- **M4. `GetNameParentTokens` 允许空段且逐次构造格式化字符串**：`common/resource_name.go:191-205`，`fmt.Sprintf("%s/", parts[2*i]) != tokenPrefix`；`projects//databases/x` 通过校验并返回空 token，`GetProjectID` 等会返回 `""` 而非报错。
  - **阶段 3 续更正（`451cb78`）**：`GetProjectID` 已删除，M4 里以它为例的返回空串路径不再存在；`GetNameParentTokens` 允许空段这一缺陷本身未变。
- **M5. `common/context.go` 的 helper 不安全/不确定**：`HasWorkspaceResource`（`:43-50`）遇到 nil 元素会 panic；`GetProjectResources`（`:52-63`）返回 map 迭代顺序（不确定）。当前两者无调用者。 —— **✅ 已删除** · `e42b9ac`：两个无调用者的 helper 与同批 telemetry 表面一起删除，问题不再存在。
- **M6. `common.Error` 的 nil `Err` panic 且不可 Unwrap**：`error.go:81-83` 的 `e.Err.Error()` 在 `&common.Error{Code: ...}` 字面量上 panic；`Wrap(nil, code)` 返回非 nil（违反 nil=成功）；缺 `Unwrap`（见 U-H1）。

---

## 低（Low）

- **日志系统未接线**：`common/log/log.go:12-13,17-33,36-38`，`LogLevel` 与 `Replace` 从未安装到任何 handler；仓库中没有 `slog.SetDefault`/`slog.New`。`--debug` 无效，source 路径裁剪无效。**✅ 已修复（阶段 1）** · `7fdcead`：`cmd/root.go` 新增 `setupLogging`，用 `HandlerOptions{AddSource: true, Level: log.LogLevel, ReplaceAttr: log.Replace}` 构造 Text/JSON handler 并 `slog.SetDefault`；`--debug` 现在真正生效，实测输出形如 `time=... level=INFO source=server/server.go:64`（路径已被裁剪）与 `--enable-json-logging` 的 JSON 行。**剩余**：`log.Stack`（`:44-47`）无论级别都会 eager 采集 20 帧栈，日志默认输出改到 `os.Stdout`（原先 `slog.Default` 写 stderr）。
- **`ValidateGroupCELExpr` 返回裸错误**：`common/cel.go:130-144`，而其兄弟函数返回 `connect.CodeInvalidArgument`；`RiskFactors`（`:18-35`）还漏了 `cel.ParserExpressionSizeLimit(celLimit)`。
- **`GetQueryExportFactors`/`findField` 脆弱**：`common/cel.go:208-248`，`if issues != nil` 不是正确的失败判断（其它地方用 `issues.Err() != nil`）；只检查 `Args[0]`，`"x" == resource.database` 检测不到；`idExpr != nil` 分支提前 `return` 不递归；可能把 `""` 追加进 `Databases`。
- **`guid.go` 拼写错误**：`GetInstaceFromGUID`（导出 API，用于 `database_service.go:308,919`）；`GetDatabaseFromGUID` 无调用者；`llm/tools.go:118-124` 重复实现了 GUID 解析。
- **`const.go` 未使用常量**：`DefaultTestEnvironmentID`、`DefaultProdEnvironmentID`、`MetaInsertBatchSize`；`ServiceAccountAccessKeyPrefix` 是服务账号遗留。
- **`config.go`**：`ReleaseModeProd` 现被 `profile_release.go`（`//go:build release`，阶段 0）使用，import 修正后 `-tags release` 构建通过（`84b16db`）；但 `ReleaseModeDev` 只被 `profile.go` 的字面量 `common.ReleaseMode("dev")` 间接使用，且没有任何构建目标带 `-tags release`，因此 prod 模式需要显式构建参数才生效。
- **`utils.Map`**（`utils/collection.go`）零调用者。
- **`utils/member.go`**：整段注释掉的 `GetUsersByRoleInIAMPolicy`（L26-68）是死 Bytebase 代码；`MemberContainsUser`/`GetUserIAMPolicyBindings`/`GetUserRolesInIamPolicy` 只通过彼此可达（`GetUserFormattedRolesMap` 是唯一活跃入口）；`utils.Uniq` 只被死的 `GetUserRolesInIamPolicy` 使用。
- **`stacktrace.TakeStacktrace`** 在 recover 之后调用时拿到的不是 panic 发生点的栈（见 `01` M6）。

---

## 死代码与遗留债务（按文件）

- **`common/cel.go`**：`ConvertUnparsedRisk`、`ConvertUnparsedApproval`、`ValidateGroupCELExpr`、`ValidateMaskingRuleCELExpr`、`ValidateMaskingExceptionCELExpr`、`ValidateProjectMemberCELExpr`、`GetQueryExportFactors` 以及变量 `RiskFactors`、`ApprovalFactors`、`IAMPolicyConditionCELAttributes`、`MaskingRulePolicyCELAttributes`、`MaskingExceptionPolicyCELAttributes`、`DatabaseGroupCELAttributes` 全部零调用者。仅 `EvalBindingCondition` 活跃（经 `utils/member.go`）。约 300 行里 230 行不可达。
- **`common/cel_attributes.go`**：全部 20 个导出常量零调用；`approval scope (deprecated)` 块（L52-58）是明确的 Bytebase 遗留。
- **`common/error.go`**：`ErrorCode`、`Wrap`、`Wrapf`、`Code.Int`、`Code.Int32` 未使用；23 个 `Code` 值中 20 个未使用（含全部 `Migration*` 201-206 与 `Task*` 301-410）。
- **`common/resource_name.go`**：阶段 0 新增并导出 `IsValidResourceID`（被 `api/v1` 与 `store` 共同复用，替换了 API 层的重复实现）；其余未使用的前缀有 `EnvironmentNamePrefix`、`PolicyNamePrefix`、`InstanceRolePrefix`、`IdentityProviderNamePrefix`、`SettingNamePrefix`、`WebhookIDPrefix`、`DatabaseGroupNamePrefix`、`SchemaNamePrefix`、`TableNamePrefix`、`LogNamePrefix`、`DeploymentConfigPrefix`、`AuditLogPrefix`、`SchemaSuffix`、`MetadataSuffix`、`CatalogSuffix`、`UserBindingPrefix`、`GroupBindingPrefix`；未使用的函数有 `GetProjectIDDatabaseGroupID`、`GetSchemaTableName`、`GetProjectIDWebhookID`、`GetUIDFromName`、`TrimSuffixAndGetInstanceDatabaseID`、`GetSettingName`、`GetRoleID`、`FormatUserEmail`；`RolePrefix` 与 `InstanceRolePrefix` 重复。
- **`backend/metric/metric.go` + `backend/plugin/metric`**：除类型 `InstanceCountMetric` 外全部常量零引用；`PrincipalRegistrationMetricName`/`PrincipalLoginMetricName` 只出现在注释块中；`metric.Reporter`/`Collector` 无实现；`CountInstanceGroupByEngineAndEnvironmentID` 无调用者。整个遥测栈（`mt.issue.count`、`mt.project.count`、`mt.service-account.count`、`mt.issue.create`、`mt.api.request`）是 Bytebase 遗留且未实现。**修复**：删除，或在 `EnableMetricCollection` 后接一个真实 reporter。
- **`config/profile.go`**：`LastActiveTS` 只写不读；~~`Secret` 从未被赋值~~（**阶段 0 已接线**：`getBaseProfile` 用 `os.Getenv("JWT_SECRET")` 赋值，见 `05`）。

---

## 待确认

1. `AUTH_SECRET` 的设置值是否可能为空（管理员清空、或恢复的 DB 缺 `setting` 但有实例行）？若是，`Obfuscate` 的除零可达，且所有实例解密失败。**阶段 0 更新**：JWT 侧已 fail-closed（`JWT_SECRET` 与 `AUTH_SECRET` 都缺失或过短时启动失败），但**字段混淆侧的除零与空 seed 仍未修**，本条仍然成立。
2. 是否有任何 API 能写入 `Binding.Condition`？目前 binding 只在不带 condition 的情况下创建，这使 M1 的 fail-open 从"活跃"降级为"潜伏"。
3. ~~跨包但很重要：`store.New(ctx, profile.PgURL, false)`（`backend/server/server.go:70`）关闭了**所有** store LRU 缓存（`enableCache` 门控每个缓存读），但缓存写仍无条件发生 → 每个元数据/数据库/group 读取都打 PostgreSQL，同时白白占用内存。需确认 `false` 是否有意。~~ —— **阶段 2 已关闭（`f22f61e`）**：经确认启用缓存，`enableCache` 参数与字段删除，缓存读取无条件生效；缓存 miss 的定向查询与失效时机（提交后）也在同一提交修正。
4. `backend/server/grpc_routes.go:129-139` 为 REST gateway 创建的 `grpc.NewClient` 连接在 shutdown 时未关闭（反复启动/停止的测试场景可能泄漏），需确认。
