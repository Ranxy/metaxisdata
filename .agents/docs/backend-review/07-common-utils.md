# 07 · common / utils / config / metric 共享基础包

**范围**：`backend/common/`（`error.go`、`context.go`、`const.go`、`guid.go`、`config.go`、`utils.go`、`resource_name.go`、`cel.go`、`cel_attributes.go`、`log/`、`stacktrace/`）、`backend/utils/`、`backend/config/`、`backend/metric/`。

**结论**：这一层代码量小但"死代码占比"最高（约 60%）。两个真正有影响的横切问题：① `common.Code` 约定完全没有执行链路（没有拦截器把 `common.Code` 映射成 Connect 状态码），所以 AGENTS.md 的错误码规范目前是空文；② `Obfuscate/Unobfuscate` 的"加密"是同库密钥 XOR。其余主要是 Bytebase 时代的枚举/常量/helper 残留，以及日志系统未接线（见 `01` H4）。

---

## 高（High）

### U-H1. `common.Code` 从不映射为 Connect 状态码，`ErrorCode` 是死代码
- **位置**：`backend/common/error.go:87-94`、`backend/server/grpc_routes.go:80-88`、`backend/api/v1/audit.go:281,297`
- **证据**：`ErrorCode` 全仓库零调用；拦截器链只有 Debug/Auth/Audit；`mapSeverity`/`buildAuditStatus` 只看 `*connect.Error`。
- **影响**：store 返回的 `&common.Error{Code: common.NotFound}`（如 `store/idp.go:214`、`setting.go:194`、`group.go:67`）到客户端变成 `CodeUnknown`/HTTP 500；`Wrap`/`Wrapf` 零调用；`Error` 没有 `Unwrap()`，`errors.Is/As` 无法穿透。
- **修复**：增加把 `common.ErrorCode(err)` 映射为 connect code 的拦截器，或删除 `common.Error`/`ErrorCode` 统一在 API 边界用 `connect.NewError`。补 `func (e *Error) Unwrap() error`。

### U-H2. 凭据混淆的密钥与密文同库，且无完整性校验
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

- **M1. CEL 条件 fail-open**：`common/cel.go:257-299`，`if !celtypes.IsBool(out) { return true, nil }`；env 声明 `resource.database/schema_name/table_name`（`:46-49`）但 `EvalBindingCondition` 只绑定 `request.time`（`:250-255`）。`utils/member.go:18` 在活跃路径上对每个 binding 调用它；引用 `resource.*` 的 condition 会被当作满足，角色被全局授予。当前 binding 构造不带 condition（`store/policy.go:68`），属潜伏。**修复**：绑定真实资源属性或返回"无法求值"并拒绝；要求编译结果为 bool。
- **M2. 每次 binding 求值都新建 CEL 环境**：`common/cel.go:262`，`cel.NewEnv` 在 `validateIAMBinding`（`utils/member.go:17-24`）里逐 binding 调用；`GetUserFormattedRolesMap` 每请求遍历所有 policy 的所有 binding。建议包级 `sync.Once` 构建一次；并在单次 `GetUserIAMPolicyBindings` 内 memoize group 查询（`member.go:148`）。
- **M3. 未检查类型断言导致 panic**：`common.go:441-467` 的 `getVariableAndValueFromExpr` 返回 `any`，调用方（`user_service.go:165,168,171,182,189,215`、`instance_service.go:78,81,84,91,98,106,109,112`、`database_service.go:741,808,833,865`）直接 `value.(string)`。`filter=email == 1` 会 panic → `connect.WithRecover` 转成 500 并回传堆栈。
  - **更正一个此前的猜测**：`expr.AsCall()` 在 cel-go v0.26.1 中是 Kind 守卫的，返回 `nilCall` 哨兵，**不会 panic**（见 `common/ast/expr.go:336-341`）。但 `expr.AsLiteral()` 对非字面量返回 **nil**（`expr.go:357-362`），因此 `args[0].AsLiteral().Value()`（`user_service.go:248`、`instance_service.go:141`、`database_service.go:865`）会 panic；`expr.AsCall().Target().AsIdent()`（`instance_service.go:136`、`database_service.go:860`）在 `Target()` 为 nil 时也会 panic。修复：comma-ok + `Kind() == LiteralKind` 判断 + 返回 `InvalidArgument`。
- **M4. `GetNameParentTokens` 允许空段且逐次构造格式化字符串**：`common/resource_name.go:191-205`，`fmt.Sprintf("%s/", parts[2*i]) != tokenPrefix`；`projects//databases/x` 通过校验并返回空 token，`GetProjectID` 等会返回 `""` 而非报错。
- **M5. `common/context.go` 的 helper 不安全/不确定**：`HasWorkspaceResource`（`:43-50`）遇到 nil 元素会 panic；`GetProjectResources`（`:52-63`）返回 map 迭代顺序（不确定）。当前两者无调用者。
- **M6. `common.Error` 的 nil `Err` panic 且不可 Unwrap**：`error.go:81-83` 的 `e.Err.Error()` 在 `&common.Error{Code: ...}` 字面量上 panic；`Wrap(nil, code)` 返回非 nil（违反 nil=成功）；缺 `Unwrap`（见 U-H1）。

---

## 低（Low）

- **日志系统未接线**：`common/log/log.go:12-13,17-33,36-38`，`LogLevel` 与 `Replace` 从未安装到任何 handler；仓库中没有 `slog.SetDefault`/`slog.New`。`--debug` 无效，source 路径裁剪无效。`log.Stack`（`:44-47`）无论级别都会 eager 采集 20 帧栈。
- **`ValidateGroupCELExpr` 返回裸错误**：`common/cel.go:130-144`，而其兄弟函数返回 `connect.CodeInvalidArgument`；`RiskFactors`（`:18-35`）还漏了 `cel.ParserExpressionSizeLimit(celLimit)`。
- **`GetQueryExportFactors`/`findField` 脆弱**：`common/cel.go:208-248`，`if issues != nil` 不是正确的失败判断（其它地方用 `issues.Err() != nil`）；只检查 `Args[0]`，`"x" == resource.database` 检测不到；`idExpr != nil` 分支提前 `return` 不递归；可能把 `""` 追加进 `Databases`。
- **`guid.go` 拼写错误**：`GetInstaceFromGUID`（导出 API，用于 `database_service.go:308,919`）；`GetDatabaseFromGUID` 无调用者；`llm/tools.go:118-124` 重复实现了 GUID 解析。
- **`const.go` 未使用常量**：`DefaultTestEnvironmentID`、`DefaultProdEnvironmentID`、`MetaInsertBatchSize`；`ServiceAccountAccessKeyPrefix` 是服务账号遗留。
- **`config.go`**：`ReleaseModeProd` 未使用；`ReleaseModeDev` 只被 `profile.go:13` 的字面量 `common.ReleaseMode("dev")` 间接使用。
- **`utils.Map`**（`utils/collection.go`）零调用者。
- **`utils/member.go`**：整段注释掉的 `GetUsersByRoleInIAMPolicy`（L26-68）是死 Bytebase 代码；`MemberContainsUser`/`GetUserIAMPolicyBindings`/`GetUserRolesInIamPolicy` 只通过彼此可达（`GetUserFormattedRolesMap` 是唯一活跃入口）；`utils.Uniq` 只被死的 `GetUserRolesInIamPolicy` 使用。
- **`stacktrace.TakeStacktrace`** 在 recover 之后调用时拿到的不是 panic 发生点的栈（见 `01` M6）。

---

## 死代码与遗留债务（按文件）

- **`common/cel.go`**：`ConvertUnparsedRisk`、`ConvertUnparsedApproval`、`ValidateGroupCELExpr`、`ValidateMaskingRuleCELExpr`、`ValidateMaskingExceptionCELExpr`、`ValidateProjectMemberCELExpr`、`GetQueryExportFactors` 以及变量 `RiskFactors`、`ApprovalFactors`、`IAMPolicyConditionCELAttributes`、`MaskingRulePolicyCELAttributes`、`MaskingExceptionPolicyCELAttributes`、`DatabaseGroupCELAttributes` 全部零调用者。仅 `EvalBindingCondition` 活跃（经 `utils/member.go`）。约 300 行里 230 行不可达。
- **`common/cel_attributes.go`**：全部 20 个导出常量零调用；`approval scope (deprecated)` 块（L52-58）是明确的 Bytebase 遗留。
- **`common/error.go`**：`ErrorCode`、`Wrap`、`Wrapf`、`Code.Int`、`Code.Int32` 未使用；23 个 `Code` 值中 20 个未使用（含全部 `Migration*` 201-206 与 `Task*` 301-410）。
- **`common/resource_name.go`**：未使用的前缀有 `EnvironmentNamePrefix`、`PolicyNamePrefix`、`InstanceRolePrefix`、`IdentityProviderNamePrefix`、`SettingNamePrefix`、`WebhookIDPrefix`、`DatabaseGroupNamePrefix`、`SchemaNamePrefix`、`TableNamePrefix`、`LogNamePrefix`、`DeploymentConfigPrefix`、`AuditLogPrefix`、`SchemaSuffix`、`MetadataSuffix`、`CatalogSuffix`、`UserBindingPrefix`、`GroupBindingPrefix`；未使用的函数有 `GetProjectIDDatabaseGroupID`、`GetSchemaTableName`、`GetProjectIDWebhookID`、`GetUIDFromName`、`TrimSuffixAndGetInstanceDatabaseID`、`GetSettingName`、`GetRoleID`、`FormatUserEmail`；`RolePrefix` 与 `InstanceRolePrefix` 重复。
- **`backend/metric/metric.go` + `backend/plugin/metric`**：除类型 `InstanceCountMetric` 外全部常量零引用；`PrincipalRegistrationMetricName`/`PrincipalLoginMetricName` 只出现在注释块中；`metric.Reporter`/`Collector` 无实现；`CountInstanceGroupByEngineAndEnvironmentID` 无调用者。整个遥测栈（`mt.issue.count`、`mt.project.count`、`mt.service-account.count`、`mt.issue.create`、`mt.api.request`）是 Bytebase 遗留且未实现。**修复**：删除，或在 `EnableMetricCollection` 后接一个真实 reporter。
- **`config/profile.go`**：`LastActiveTS` 只写不读；`Secret` 从未被赋值（见 `05`）。

---

## 待确认

1. `AUTH_SECRET` 的设置值是否可能为空（管理员清空、或恢复的 DB 缺 `setting` 但有实例行）？若是，`Obfuscate` 的除零可达，且所有实例解密失败。
2. 是否有任何 API 能写入 `Binding.Condition`？目前 binding 只在不带 condition 的情况下创建，这使 M1 的 fail-open 从"活跃"降级为"潜伏"。
3. 跨包但很重要：`store.New(ctx, profile.PgURL, false)`（`backend/server/server.go:70`）关闭了**所有** store LRU 缓存（`enableCache` 门控每个缓存读），但缓存写仍无条件发生 → 每个元数据/数据库/group 读取都打 PostgreSQL，同时白白占用内存。需确认 `false` 是否有意。
4. `backend/server/grpc_routes.go:129-139` 为 REST gateway 创建的 `grpc.NewClient` 连接在 shutdown 时未关闭（反复启动/停止的测试场景可能泄漏），需确认。
