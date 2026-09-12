# 02 · 认证、授权、身份与审计

**范围**：`backend/api/auth/`（`auth.go`、`header.go`、`config.go`）、`backend/api/v1/`（`user_service.go`、`auth_service.go`、`audit_log_service.go`、`audit.go`、`debug_interceptor.go`、`acl_interceptor.go`（阶段 0 新增）、`setting_service.go`（阶段 0 新增）、`common.go` 中与身份相关的部分）、`backend/component/state/state.go`、`backend/store/principal.go`、`backend/store/policy.go`、`backend/store/role.go`、`backend/store/group.go`、`backend/utils/member.go`。

**结论**：这是整个后端风险最集中的模块。认证层能"跑通"，但密钥是公开常量；授权层实际上不存在（拦截器被注释、`permission` 字段从不校验）；公开注册接口可未认证创建账号，且首个用户自动成为管理员；用户列表过滤器存在 SQL 注入；审计日志会把一次性明文 API key 落库。**这些不是"优化项"，而是必须在任何对外部署前修复的安全漏洞。**

**阶段 0 更新**：C1 ✅、C2 ✅、C3 ✅、C4 ◐、H3 ✅，另修复 M5（SSO 首用户管理员）、M7（`DisallowSignup` 真正生效）、M12（`allow_missing` 需管理员）与低优先级的 JWT 解析校验项。H1（token 吊销）、H4（最后管理员组绕过）、M1/M3/M4/M6 等**仍未处理**；`userCountGuard` 仍是空实现（首管理员选举已下沉到 store，不再依赖它）。

---

## 严重（Critical）

### C1. JWT 签名密钥为硬编码常量，`Mode` 恒为 dev
> **✅ 已修复（阶段 0）** · `adfec91` `84b16db`：硬编码常量删除，改为 `JWT_SECRET` 环境变量（缺失时回退 DB `AUTH_SECRET`），`< 32` 字符启动失败；解析侧加 `WithValidMethods(HS256)`/`WithIssuer`/`WithExpirationRequired()`，历史 token 全部失效。新增的 `profile_release.go`（`-tags release`）提供 `Mode=prod`，import 修正后 release 构建与 `go vet -tags release ./...` 均通过。**注意**：`Mode` 仍由 build tag 决定，默认构建为 dev（audience 与 CORS 影响见 `01` H2）。

- **位置**：`backend/bin/server/cmd/profile_dev.go:10-11`、`backend/server/server.go:106`、`backend/server/grpc_routes.go:83`、`backend/api/auth/auth.go:144-166`
- **证据**：`p.Secret = "00000000-0000-0000-0000-000000000000"`，`activeProfile` 是唯一实现且无 build tag；`auth.go` 用该 secret 做 HS256 校验；`init.go:29-38` 生成的随机 `AUTH_SECRET` 只用于字段混淆。
- **影响（修复前）**：攻击者可自行签发 `kid=v1`、`sub=<任意用户 ID>`、`aud=mt.user.access.dev` 的 token 通过校验。由于 `Mode` 恒为 `dev`，所有部署 audience 相同，一个实例的 token 可在其他实例上使用，`sub=1` 即首个管理员。
- **修复**：从环境变量/`AUTH_SECRET` 读取签名密钥并在缺失时启动失败；删除 dev 常量；补充 prod profile；已运行的部署需轮换并作废全部历史 token。

### C2. 未认证即可创建用户，且首个用户自动成为 workspaceAdmin
> **✅ 已修复（阶段 0）** · `5b19778` `c4e22fc`
> - `CreateUser` 新增 `authorizeCreateUser(ctx, userType)`：调用者是 workspaceAdmin 时可创建任意用户；否则只能注册 `UserType_USER`，且 `disallow_signup` 为 true 时返回 `CodePermissionDenied`；首个活跃 END_USER 始终放行以完成引导。SERVICE_ACCOUNT 的创建变为管理员专属。
> - 首管理员选举下沉到 `store.CreateUser`：同一事务内先 `pg_advisory_xact_lock(createEndUserAdvisoryLockKey)`，再统计活跃 END_USER，为 0 时调用 `patchWorkspaceIamPolicyImpl` 在**同一事务**内授予 `roles/workspaceAdmin`（并发注册不再可能同时成为管理员，SSO 首用户路径也一并修复，见 M5）。
> - `store.CountUsers` 增加 `principal.deleted = FALSE`，软删除用户不再影响"首个用户"判定。
> - 运营侧入口：新增 `SettingService.UpdateWorkspaceProfileSetting` 与前端 `/settings/general` 页面开关。（注意：`disallow_signup` 默认仍为 `false`，需要管理员显式打开。）

- **位置**：`proto/v1/v1/user_service.proto:47-56`、`backend/api/v1/user_service.go:286-333,389-398,788-790`
- **证据**：`CreateUser` 带 `option (metaxisdata.v1.allow_without_credential) = true`，认证拦截器在 token 缺失时直接放行（`auth.go:89-91`）；`userCountGuard` 是 `return nil` 空实现；`DisallowSignup` 校验整段被注释；`count == 0` 时授予 `roles/workspaceAdmin`。
- **影响**：
  1. 全新实例上，未认证攻击者抢先注册即成为管理员；
  2. 任意实例上可无限创建带攻击者已知密码的账号（含 SERVICE_ACCOUNT）；
  3. `count == 0` 判定非原子，并发注册可同时成为管理员；
  4. `store.CountUsers` 统计包含软删除用户（`store/stats.go:33-38` 无 `deleted` 条件），删除用户后"首个用户"判定失真。
- **修复**：`CreateUser` 强制校验 `DisallowSignup`/管理员权限；首个管理员授予改为原子操作（`INSERT ... WHERE NOT EXISTS` 或 advisory lock）；`CountUsers` 只统计 `deleted = FALSE`。

### C3. `ListUsers` 过滤器 SQL 注入（两处）
> **✅ 已修复（阶段 0）** · `3321801`
> - `user_service.go` 的 `matches` 改为 `LOWER(principal.%s) LIKE $n` 并把 `likePattern(strings.ToLower(strValue))` 追加到 `positionalArgs`（`%`/`_` 已转义）；`project` 过滤值先经 `isValidResourceID`（现委托 `common.IsValidResourceID`）校验。
> - `store/principal.go`、`store/group.go` 的 project ID 用 `common.IsValidResourceID` 校验后再拼接 CTE；`store/principal.go` 的 `ListResourceFilter.Where` 仍由调用方构造，但所有进入它的用户输入现已参数化。
> - 守卫测试：`backend/api/v1/filter_injection_test.go`（user/instance/database-name/database-table/database-label 各注入载荷 + `TestLikePatternEscapesWildcards`）。

- **位置**：`backend/api/v1/user_service.go:256`、`backend/store/principal.go:206,233,272`
- **证据**：
  ```go
  // user_service.go:256 —— strValue 为用户可控 CEL 字面量，直接拼接
  return "LOWER(principal." + variable + ") LIKE '%" + strings.ToLower(strValue) + "%'", nil
  // store/principal.go:233 —— projectID 直接拼接进 CTE
  WHERE (... resource = 'projects/` + *v + `') ...
  ```
  `store/principal.go:272` 将 `filter.Where` 原样拼进 `WHERE`。
- **影响**：`filter=name.matches("x') OR TRUE --")` 可返回全部用户；`project == "projects/x' OR '1'='1"` 可破坏成员 CTE。可进一步做 union 注入读取整库（含 `password_hash`）。任何已认证用户可达，而未认证用户可先注册。
- **修复**：`matches` 改为参数绑定（`LIKE $n`，值为 `"%" + v + "%"`）；project ID 用 `isValidResourceID` 校验；**禁止**把用户输入拼进 `ListResourceFilter.Where`。
- **同类问题**：`instance_service.go:152,154,156`、`database_service.go:874,880`、`store/group.go:111`，详见 `04-api-v1.md`。

### C4. 授权层实际不存在：任意成员可接管/删除任意账号
> **◐ 部分修复（阶段 0）** · `ec49607` `0f2165e`
> - 已修：重建 `ACLInterceptor` 并在 `grpc_routes.go` 接线，消费 proto 的 `permission` 注解（非空 ⇒ workspaceAdmin）；`DeleteUser`/`UndeleteUser` 已声明 `metaxisdata.users.delete`/`.undelete`。`UpdateUser` 因含自助场景在 handler 内鉴权：`allow_missing` 分支要求管理员；改他人（含改密、改邮箱）要求 workspaceAdmin；**本人改密必须提供并匹配 `current_password`**，否则 `CodeInvalidArgument`/`CodePermissionDenied`。proto 新增 `UpdateUserRequest.current_password`（INPUT_ONLY），前端 `UserManagementPage.vue` 编辑自己时展示该输入框。
> - 剩余：读路径（`ListUsers`/`GetUser`/`BatchGetUsers`）仍是"任意已认证用户"；尚无 permission→role 的细粒度映射（当前所有非空 permission 都等同于管理员）；H1 的 token 吊销问题未解决，改密后旧 token 仍可用。

- **位置**：`backend/server/grpc_routes.go:80-88`（第 85 行被注释）、`backend/api/auth/auth.go:317-321,350-355`、`backend/api/v1/user_service.go:445-471,500-507,555-595,643-674`
- **证据**：`// apiv1.NewACLInterceptor(stores, secret, iamManager, profile),` —— `NewACLInterceptor` 与 `iamManager` 在整个仓库都不存在（即使取消注释也无法编译）；`AuthContext.Permission` 被写入后没有任何消费者；没有任何 proto 方法设置 `permission`；handler 只做 `_, ok := GetUserFromContext(ctx)` 然后 `// todo check permission`。
- **影响**：
  - `UpdateUser` 的 `password` 分支既不校验权限也不校验原密码 → **任意已认证用户可把任意用户（含管理员）的密码改掉后登录**；
  - 可修改任意用户 email；
  - 可 `DeleteUser`/`UndeleteUser` 任意账号；
  - 可枚举全部用户。
  整个身份层唯一真正生效的检查是 `ListAuditLogs`（`audit_log_service.go:35-41`）。
- **修复**：重建授权拦截器（或 handler 内显式检查），基于 `AuthContext.Permission` 与 `store.RoleMessage.Permissions`；自助改密要求原密码/二次认证；他人改密/删号要求显式权限。

---

## 高（High）

### H1. Token 吊销不完整且可被攻击者"冲掉"
- **位置**：`backend/component/state/state.go:16-24`、`backend/api/v1/auth_service.go:197-213`、`backend/api/auth/auth.go:140-142`
- **证据**：`lru.New[string, bool](128)`；`Logout` 对**任意**调用方传入的字符串执行 `TokenExpireCache.Add(accessTokenStr, true)`；校验时只查该 LRU。
- **影响**：
  1. 超过 128 次登出后，最早的吊销记录被淘汰，旧 token 在 7 天有效期内重新可用；
  2. 未认证攻击者可发 128 次伪造 Logout 冲掉真实吊销记录，然后重放窃取的 token；
  3. 缓存是进程内的，多副本部署时其他副本仍接受已吊销 token；
  4. 改密码/改邮箱不会吊销任何 token。
- **修复**：吊销前先校验 token；按 `user + iat/jti` 记录并带 TTL（或持久化）；跨副本共享；改密/删号时主动吊销。

### H2. CORS 永久全开 + `SameSite=None` cookie → CSRF
- **位置**：`backend/server/echo_routes.go:25-35`、`backend/api/auth/header.go:46-50`
- **证据（默认构建仍是 dev）**：`Mode` 恒为 `dev`，因此 `AllowOriginFunc` 返回 `true` 且 `AllowCredentials: true`；cookie 的 `SameSite`/`Secure` 依据客户端可控的 `Origin` 头决定，HTTPS 时为 `SameSite=None`。
- **影响**：HTTPS 部署下任意站点可发起携带 cookie 的跨域写请求（预检通过），无 CSRF token。
- **修复**：CORS 改为配置驱动的显式 allowlist，与 `Mode` 解耦；`Secure` 依据服务端 TLS 配置；增加 CSRF 防护。

### H3. 审计日志落库明文 OpenLineage API key
> **✅ 已修复（阶段 0）** · `89ef84a`：`isSensitiveAuditField` 增加对裸字段名的精确匹配（`key`/`content`/`sslkey`/`sslcert`/`keytab`/`passwd`/`pwd`/`bearer`/`jwt`/`session`），子串匹配列表不变。`audit_test.go` 新增 `TestMarshalAuditMessageRedactsSecrets`（`CreateAPIKeyResponse.key`、`DataSource.sslCert`/`sslKey`/`gcpCredential`）与 `TestIsSensitiveAuditField` 表驱动测试。**剩余**：脱敏仍是"名字启发式"而非按 proto descriptor 结构化，新增敏感字段名仍可能漏网。

- **位置**：`backend/api/v1/audit.go:114-134,179-208`、`proto/v1/v1/openlineage_service.proto:66-75,300-304`、`backend/api/v1/audit_log_service.go:185-202`
- **证据**：`CreateAPIKey` 带 `audit = true`；审计拦截器把**响应**也序列化进 `audit_log.response`；`isSensitiveAuditField` 的标记列表包含 `apikey/api_key` 等，但 `CreateAPIKeyResponse` 的字段名是裸的 `key`，不在列表中；`ListAuditLogs` 会把 `Response` 原样返回。
- **影响**：一次性明文 ingestion key 被持久化并可通过审计 API 读取，`proto` 注释中"only returned once"的承诺失效，轮换也失去意义。
- **修复**：把 `key`（以及 `passwd/pwd/bearer/jwt/session`）加入脱敏列表；更稳妥的是按 proto field behavior 结构化脱敏；为 `CreateAPIKeyResponse` 增加回归测试。

### H4. 最后一个管理员的保护可被"组绑定"绕过
> **⏳ 未处理（阶段 0 范围外）**：本轮只把首个管理员的授予原子化，`hasExtraWorkspaceAdmin` 的组展开逻辑未改，删除最后一名管理员仍可能被"仅含该用户的组"绕过。

- **位置**：`backend/api/v1/user_service.go:612-640`
- **证据**：非 `allUsers` 成员走 `utils.GetUsersByMember`，该方法返回组内**全部**成员（包含正在被删除的用户），随后 `if !user.MemberDeleted && user.Type == END_USER { return true }`。
- **影响**：当 `roles/workspaceAdmin` 绑定在一个仅含该用户的组上时，`hasExtraWorkspaceAdmin` 返回 true，删除后工作区零管理员，且无法通过 API 恢复。
- **修复**：组展开时排除 `user.ID == targetUserID`，或"模拟移除后再评估策略"。

---

## 中（Medium）

### M1. 每个已认证请求都会全表扫描 principal
- **位置**：`backend/store/principal.go:89-114`、`backend/server/server.go:70`
- **证据**：`GetUserByID/GetUserByEmail` 命中缓存的前提是 `s.enableCache`，而 `store.New(ctx, profile.PgURL, false)` 恒传 `false`；未命中即执行 `listAndCacheAllUsers`（`SELECT ... FROM principal` + `user_group` join，无 `WHERE id`）。
- **影响**：认证拦截器每个 RPC 都调用 `GetUserByID`（`auth.go:172`），即每个请求全表扫描并重建永远不会被读取的缓存；`BatchGetUsers` 放大 N 倍。
- **修复**：改为 `WHERE id/email = $1` 定向查询（并缓存空结果）；要么启用缓存、要么删除缓存代码——不要"读被 flag 关闭、写却无条件执行"。

### M2. 登录存在用户枚举时间差，且无暴力破解防护
- **位置**：`backend/api/v1/auth_service.go:215-229`
- **证据**：`if user == nil { return invalidUserOrPasswordError }` 在 bcrypt 之前返回；仓库中不存在任何限流中间件。
- **影响**：错误文案相同但耗时可测，可枚举已注册邮箱；可无限次在线猜密码，成功后拿到 7 天 token。
- **修复**：用户不存在时也执行一次 dummy bcrypt 比较；增加按 IP/账号的限流与锁定。

### M3. CEL 过滤器未检查类型断言 → panic/500
> **⏳ 未处理（阶段 0 只治标）**：类型断言仍未改，`filter=email == 1` 依旧 panic → 500；`5446a10` 只是不再把堆栈回传给客户端。

- **位置**：`backend/api/v1/user_service.go:165,168,171,182,189,215`（同类：`instance_service.go:78,81,84,91,98,106,109,112`、`database_service.go` 同段）
- **证据**：`value.(string)`、`rawType.(string)` 等未用 comma-ok；`getVariableAndValueFromExpr` 可能返回 `int64/bool/[]any`。
- **影响**：`filter=email == 1` 触发 panic，被 `connect.WithRecover` 转成 `CodeInternal` 并把堆栈回传（见 `01` 的 C2）。
- **修复**：comma-ok + 类型 switch，返回 `InvalidArgument`。

### M4. OAuth2 登录缺少 `state`，且可能 nil config panic
- **位置**：`backend/api/v1/auth_service.go:253-273`、`proto/v1/v1/auth_service.proto:48-66`
- **证据**：`OAuth2IdentityProviderContext` 只有 `code`，全仓库无 state/nonce 校验；`oauth2.NewIdentityProvider(idp.Config.GetOauth2Config())` 会解引用 `ClientId`，配置为空时 panic。
- **影响**：登录 CSRF / 授权码注入（受害者账号被绑定到攻击者身份）；空配置导致 500。
- **修复**：服务端生成并校验 `state`（并支持 PKCE）；对 `GetOauth2Config()` 做 nil 检查。

### M5. 通过 SSO 创建的首个用户永远不会成为管理员
> **✅ 已修复（阶段 0，随 C2 一并修）** · `5b19778`：首管理员选举从 handler 下沉到 `store.CreateUser` 的事务内（advisory lock + 活跃 END_USER 计数 + 同事务授予），SSO 路径直接调用 `CreateUser`，因此首用户必然获得管理员，不再是"永久管理锁死"。

- **位置**：`backend/api/v1/auth_service.go:330-350` 对比 `backend/api/v1/user_service.go:389-398`
- **证据**：SSO 分支直接 `CreateUser` 后返回，没有 `firstEndUser` → `PatchWorkspaceIamPolicy` 逻辑。
- **影响**：若首个 END_USER 来自 SSO，工作区无管理员；后续 `CreateUser` 看到 `count > 0` 也不会补授 → 永久管理锁死。
- **修复**：把"创建用户 + 首个管理员授予"收敛到同一个 helper，两条路径共用。

### M6. 每次登录都会清空 `UserProfile.Source`
- **位置**：`backend/api/v1/auth_service.go:136-143`、`backend/store/principal.go:415-421`
- **证据**：新构造的 `UserProfile` 只填 `LastLoginTime`/`LastChangePasswordTime`，而 store 是整列 JSONB 覆盖。
- **影响**：登录后 provenance 字段被重置；未来 `UserProfile` 新增字段也会被静默丢弃。
- **修复**：`proto.Clone(loginUser.Profile)` 后只改 `LastLoginTime`。

### M7. `DisallowSignup` 从未生效；`DisallowPasswordSignin` 可被服务账号绕过
> **◐ 部分修复（阶段 0）** · `5b19778` `c4e22fc`：`CreateUser` 现在真正读取 `DisallowSignup` 并在非管理员自注册时拒绝；管理员可通过 `SettingService`/`/settings/general` 修改该设置。**剩余**：`DisallowPasswordSignin` 对服务账号的例外仍在（登录检查只覆盖 `END_USER`），本轮未改。

- **位置**：`backend/api/v1/user_service.go:291-310`、`backend/api/v1/auth_service.go:91-100`
- **证据**：`DisallowSignup` 整段被注释（且引用了不存在的 `s.profile.SaaS`，无法编译）；登录检查的条件只覆盖 `END_USER`。
- **影响**：`disallow_signup` 是死设置，公开注册始终可用；即使运营方禁用密码登录强制 SSO，服务账号仍可用 key 登录，而 `CreateUser` 未认证 → 攻击者可自建服务账号登录。
- **修复**：在 `CreateUser` 中执行 `disallow_signup`；对服务账号同样应用 `disallow_password_signin`，或在 proto 中显式说明例外。

### M8. 审计写入被静默吞掉，且覆盖面不全
- **位置**：`backend/api/v1/audit.go:52-57,87-89,95-143`
- **证据**：`if auditErr := ...; auditErr != nil { slog.Error(...) }`，RPC 仍返回成功；`createAuditLog` 使用请求 `ctx`，客户端断开即可取消写库；`ListAuditLogs` 未标注 `audit = true`。
- **影响**：攻击者可通过取消请求或制造写失败让已审计操作"不留痕"；读取审计日志本身不被审计。
- **修复**：使用脱离请求的带超时 context 或队列落库；为 `ListAuditLogs` 与其余变更方法补齐 `audit`。

### M9. 审计 IP 来自可伪造头
- **位置**：`backend/api/v1/audit.go:304-316`
- **证据**：优先取 `X-Forwarded-For` 的第一段。
- **影响**：客户端可伪造审计记录中的来源 IP，取证价值下降。
- **修复**：仅在直连 peer 属于受信代理时才采信转发头。

### M10. `workspaces/-` 会取消审计日志的 parent 过滤
- **位置**：`backend/api/v1/audit_log_service.go:43-49`、`backend/store/audit_log.go:65-68`
- **影响**：多租户共库时，一个 workspace 的管理员可读到其他 workspace 的审计记录（当前单 workspace 部署影响低）。
- **修复**：始终按调用者 workspace 约束查询，拒绝空/`-` parent。

### M11. 分页实现有缺陷（token limit 被忽略、负 offset 未校验、int32 溢出）
- **位置**：`backend/api/v1/common.go:301-360`
- **证据**：`token.Limit` 只用于 `< 0` 检查，实际 limit 取自请求；`offset.offset` 无范围校验；`getNextPageToken` 做 `int32(p.offset + p.limit)`。
- **影响**：伪造 page token（未签名）可让 SQL 收到 `OFFSET -1` 报错；token 中的 page size 被忽略可能导致翻页循环/漏行；大 offset 溢出 int32。
- **修复**：校验 `offset >= 0`；limit 只取请求值；用 int64 与边界检查。

### M12. `UpdateUser(allow_missing)` 直接调用未认证语义的 `CreateUser`
> **◐ 部分修复（阶段 0）** · `0f2165e`：`allow_missing` 分支现在先 `requireWorkspaceAdmin`，再复用 `CreateUser`（后者又按 `disallow_signup` 判定），因此不再是一条匿名创建路径。**剩余**：别名本身仍在，`update_mask` 依旧被忽略。

- **位置**：`backend/api/v1/user_service.go:465-471`
- **影响**：忽略 update_mask，继承 `CreateUser` 的全部弱点，形成第二条需要单独加固的创建路径。
- **修复**：删除该别名，或统一走一个带权限检查的创建 helper。

### M13. `RequireResetPassword` 只是提示，服务端不强制
- **位置**：`backend/api/v1/auth_service.go:76,158-194`
- **影响**：密码轮换/首登改密策略未被执行，过期密码仍可换取完整权限 token；前端也没有读取该字段。
- **修复**：服务端限制 token 权限范围或直接拒绝登录，直到完成改密。

### M14. store `UpdateUser` 原地修改缓存中的 profile 指针
- **位置**：`backend/store/principal.go:405-411`
- **影响**：与 `userIDCache` 中的对象共享指针，并发读会观察到提交前状态；若启用缓存即为真实 data race。
- **修复**：`proto.Clone` 后再改，写入后替换缓存项。

---

## 低（Low）／代码质量

- `backend/api/v1/user_service.go:69`：按 email 查询失败时错误信息里打印 `userID`（此时为 0）。
- `backend/api/v1/user_service.go:116-122`：认证失败却返回 `CodeInternal`，且丢弃了取回的 user。
- `backend/api/v1/user_service.go:579-586`：`DeleteUser` 直接返回 store 原始错误 → Connect 映射为 `CodeUnknown`。
- `backend/api/v1/auth_service.go:142`：日志记录用户 email（PII）。
- `backend/api/v1/debug_interceptor.go:33-37`：截断错误时用 `errors.New` 重建，丢失 `connectErr.Details()` 与原始错误链，同时把完整消息以 Info 级别写入日志。
- `backend/api/auth/auth.go:144-159`：~~JWT 解析未使用 `WithValidMethods/WithIssuer/WithExpirationRequired`，`issuer` 常量只写不校验~~ —— **✅ 已修复（阶段 0，`adfec91`）**：三项校验全部补上，`issuer` 现在是硬校验。
- `backend/api/auth/auth.go:74-79`：`GetTokenFromHeaders` 返回错误（Authorization 头格式错误）时直接 401，**不会**走 `IsAuthenticationAllowed` 豁免，因此给 Login 带上格式错误的 Authorization 头会导致登录失败。
- `backend/api/v1/auth_service.go:110,121-134`：`web=true` 时 token 同时出现在响应体与 cookie 中，削弱 HttpOnly 的意义。

---

## 死代码与遗留债务

- **被禁用的授权**：~~`grpc_routes.go:85` 引用的 `NewACLInterceptor`/`iamManager` 已不存在~~ —— **◐ 已重建（阶段 0，`ec49607`）**：`acl_interceptor.go` 的 `NewACLInterceptor(store)` 已接线并消费 `common.AuthContext.Permission`；`HasWorkspaceResource`/`GetProjectResources`/`AuthMethod`/`Resources` 仍无消费者，`store.RoleMessage.Permissions` 仍只写不校验。
- **空实现**：`user_service.go:815` 与 `auth_service.go:353` 两个同名 `userCountGuard` 仍是 `return nil`（**阶段 0 未删**），但首管理员选举已下沉到 `store.CreateUser` 事务内，授权判定改由 `authorizeCreateUser` 负责，它们已不再承担安全语义；`getActiveUserCount` 与 `store.CountActiveUsers` 的重复仍在。
- **未使用的 helper**：`common.go` 中 `ParseFilter`/`parseExpression`/`normalizeFilter`/`getComparatorType`/`Expression`/`OperatorType` 全仓库无引用；`auth.go:198 GetTokenFromMetadata` 与 `GatewayMetadataAccessTokenKey`/`GatewayMetadataRequestOriginKey` 未使用（`metadata` import 仅为它存在）。
- **永不写入的 context key**：`common.ServiceDataKey` 从未被写入，`getServiceData` 恒返回 nil。
- **注释掉的代码块**：~~注册/许可校验~~（阶段 0 已删除 `user_service.go` 中被注释的注册/许可校验与 `firstEndUser` 块）、metric 上报（`user_service.go:371-380` 仍留）、调用者身份校验（`user_service.go:588,679` 的 `// todo check permission` 仍在，实际鉴权已由 ACL 拦截器按 proto 注解完成）、export format 转换（`common.go:362-390`）、`GetUsersByRoleInIAMPolicy`（`utils/member.go:26-68`）。
- **Bytebase 时代遗留**：`SERVICE_ACCOUNT`/`service_key`/`sa_` 前缀、`SYSTEM_BOT`、`recovery_codes`（2FA 从未实现）、`require_2fa`/`maximum_role_expiration`、`ListUsers` 的 project 过滤器（产品已无 project 概念）、`UserProfile.source`（SCIM/Entra，从未写入）、`store/stats.go` 引用不存在的 `issue` 表。
- **未实现的设置**：`WorkspaceProfileSetting.token_duration` 被忽略，`GetTokenDuration` 硬编码 7 天且忽略入参。
- **`V2` 命名**：`GetSettingV2`/`GetInstanceV2`/`GetDatabaseV2`/`GetPolicyV2` 等与同名无 V2 版本并存。

---

## 待确认

1. 是否存在构建包装用 ldflags/生成文件注入真实 `profile.Secret`？仓库内无证据，但 dev 默认值仍会随二进制发布。
2. 生产是否有反向代理统一剥离/规范化 `Origin` 与 `X-Forwarded-For`？这决定 CSRF 与审计 IP 的实际可利用性。
3. 创建 IDP 时是否强制 `OAUTH2` 必须有 `oauth2_config`（决定 nil-config panic 是否可达）？
4. `store.enableCache` 是否有意在某处开启？目前全部调用点传 `false`，应明确"启用缓存"或"删除缓存"。
5. `GetUser`/`BatchGetUsers`/`ListUsers` 的 proto 注释写的是"any authenticated user"——在自助注册开放的前提下，这个策略是否仍成立？
6. 插件层 `backend/plugin/idp/oauth2/oauth2.go` 在 error/debug 级别打印授权码、access token、userinfo 与 claims（超出本次 plugin 排除范围，建议单独排查）。
