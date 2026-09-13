# 01 · 进程入口与服务装配

**范围**：`backend/bin/server/`（`main.go`、`cmd/root.go`、`cmd/profile.go`、`cmd/profile_dev.go`）、`backend/config/profile.go`、`backend/server/`（`server.go`、`init.go`、`grpc_routes.go`、`echo_routes.go`、`pprof.go`、`ultimate.go`、`server_frontend_not_embed.go`）、`backend/store/db_connection.go`。

**结论**：这一层代码量不大，但集中了 3 个全局性的严重问题（硬编码 JWT 密钥、权限拦截器被注释、panic 堆栈回传客户端），以及若干"看起来在配置、实际没接线"的死配置（日志级别、JSON 日志、多个 CLI flag）。启动装配本身可以工作（`go build`/`go vet`/`go test ./...` 均通过），但可观测性和关停路径有明确缺陷。

**阶段 0 更新**：C1 ✅、C2 ✅、H1 ◐、H5 ◐ 已处理；H2（CORS/CSRF）与 H4（日志接线）**未处理**，仍待阶段 1。阶段 0 后 `golangci-lint` 已可运行（0 issues），`-tags release` 也恢复可编译。

**阶段 1 更新**：H4 ✅（日志系统接线，`7fdcead`）、H5 ◐→✅（`--external-url` 已注册并接线，`7fdcead`）、部署侧新增 `make build-release` 让 prod profile 有了真实构建目标（`7fdcead`）。H2（CORS/CSRF）仍未处理。

**阶段 2 更新**：M1 ✅（关停不再 `Fatal`，runner 等待加 10s 上限，`fb8ca14`）、M4 ✅（连接池钳制 + idle/lifetime/idleTime + `sync.Once` 初始化，`fb8ca14`）。M2（派生后丢弃 context）、M3（启动打印全部路由）、M5、M6 仍未处理。
**阶段 3 更新**：M2 ✅（删除派生后丢弃的 context 与 `Server.cancel`——它取消的 context 无人监听；真正的 runner 取消是 `runnerCtx`/`runnerCancel`）、M3 ✅（`echo.Debug` 与路由列表打印改为仅在 `RuntimeDebug`/`--debug` 时输出）、低节"未注册 flag"与 `dataDir` ✅（`fedcc12`：删除 `ha`/`saas`/`demo`/`memoryProfileThreshold` 与 `dataDir`，`activeProfile`/`getBaseProfile` 不再收参数）、`Profile.LastActiveTS` ✅（只在授权请求里写、从不读）。另删除两个无实现且会直接编译失败的构建约束：`ultimate.go` 的 `!minidemo` 与 `server_frontend_not_embed.go` 的 `!embed_frontend`（`3cc4926`，实测 `go build -tags embed_frontend ./backend/server/` 曾报 `undefined: embedFrontend`）。**H2（CORS/CSRF）仍未处理**：cookie 的 `SameSite` 仍由客户端可控的 `Origin` 决定、无 CSRF token；不过阶段 3 为路由装配补了测试，覆盖"dev 全开 CORS / prod 不发 CORS 头"（`0dae0b7`）。**M5、M6 仍未处理**。

**阶段 3 收尾更新**：`backend/server/init.go` 不再向 `WORKSPACE_PROFILE` 写入 `EnableMetricCollection: true`（该字段与整个 metric 栈已删除，`e0eab33`）；`LATEST.sql` 中 `setting.name` 取值、`principal.mfa_config`、`idp.type` 的注释改为如实描述（表结构未动，`e0eab33`）。

**阶段 5 更新**：H5 的 `--external-url` 与 C1 的 `JWT_SECRET` 均被移除（`7870016`）。`ExternalURL` 不再进 `config.Profile`，SSO 回调 base 只从 `WORKSPACE_PROFILE.external_url` 读取、由管理员在 `/settings/general` 配置；该设置行现在只在全新安装时创建，启动不再覆盖管理员的值。JWT 签名密钥始终读数据库 `AUTH_SECRET`（`resolveJWTSecret`），环境变量变化不再让 token 失效。`config.Profile` 因此只剩 `Mode`/`Port`/`PgURL`/`RuntimeDebug`/`Secret`，其中 `Secret` 是启动时从 DB 解析出的运行时值，不再是构造 profile 时的输入。

---

## 严重（Critical）

### C1. JWT 签名密钥硬编码，且 `Mode` 永远是 dev
> **✅ 已修复（阶段 0）** · `adfec91` `84b16db`
> - 硬编码常量被删除；`getBaseProfile` 改为 `Secret: os.Getenv("JWT_SECRET")`；`server.resolveJWTSecret` 在环境变量为空时回退到 DB 的 `AUTH_SECRET`，两者都缺失或长度 `< 32`（`minJWTSecretLength`）则启动失败（fail-closed）。`auth.go` 解析侧加 `WithValidMethods(HS256)`/`WithIssuer(issuer)`/`WithExpirationRequired()`，历史 token 因签名密钥变化与声明校验全部失效。
> - **prod profile 已可用**：`activeProfile` 不再是唯一实现，新增 `//go:build release` 的 `profile_release.go`（`Mode = common.ReleaseModeProd`）；`84b16db` 把它的两个 import 从 `github.com/Ranxy/laelia/...` 修正为 `github.com/Ranxy/metaxisdata/backend/common` 与 `.../backend/config`。`go build -tags release ./backend/bin/server/` 与 `go vet -tags release ./...` 均通过。
> - **注意（非缺陷，但部署相关）**：`Mode` 完全由 build tag 决定。阶段 1 新增了 `make build-release`（带 `-tags release` ⇒ `ReleaseModeProd`，`7fdcead`），并在 AGENTS.md 里把 release 构建写成"部署用"的第一步；`make build` 与 AGENTS.md 的开发构建**刻意保持 dev**（本地开发需要宽松 CORS）。CI 与 Docker 仍不存在于仓库中，因此没有任何自动化流程产出 prod 产物——部署方必须显式使用 `make build-release`。

- **位置**：`backend/bin/server/cmd/profile_dev.go:10-11`、`backend/server/server.go:106`、`backend/server/grpc_routes.go:83`
- **证据**：
  ```go
  func activeProfile(dataDir string) *config.Profile {
      p := getBaseProfile(dataDir)
      p.Mode = common.ReleaseModeDev
      p.Secret = "00000000-0000-0000-0000-000000000000"
      return p
  }
  ```
  `activeProfile` 是唯一的实现（无 build tag、无 prod 版本），`server.go:106` 把 `s.profile.Secret` 传给 `configureGrpcRouters`，最终用于 HS256 签发/校验。`backend/server/init.go:29-38` 随机生成的 `AUTH_SECRET` 只被 `store.GetSecret()` 用于字段混淆，**从不参与 JWT**。
- **影响**：签名密钥是公开常量，任何人都能伪造 `kid=v1`、`sub=1`（首个用户即 workspaceAdmin）、`aud=mt.user.access.dev` 的 token，冒充任意用户；且所有部署 audience 相同，跨实例通用。
- **修复**：签名密钥改为启动时从环境变量/`AUTH_SECRET` 读取并 fail-closed；删除 dev 常量；补上真正的 prod profile；已运行过该代码的部署应视为所有 token 已泄露。
- **详见**：`02-auth-authorization.md`。

### C2. panic 时把完整 Go 调用栈返回给客户端
> **✅ 已修复（阶段 0）** · `5446a10`：`onPanic` 现在只返回 `connect.NewError(connect.CodeInternal, errors.New("internal server error"))`，堆栈仍写入 `slog.Error`（含 `connect.Spec`），不再进入响应。注意：CEL 过滤器的类型断言 panic（`04` M1 / `02` M3）本身仍未修，只是不再泄露堆栈。

- **位置**：`backend/server/grpc_routes.go:73-78`
- **证据**：
  ```go
  onPanic := func(_ context.Context, s connect.Spec, _ http.Header, p any) error {
      stack := stacktrace.TakeStacktrace(20, 5)
      slog.Error("v1 server panic error", ...)
      return connect.NewError(connect.CodeInternal, errors.Errorf("error: %v\n%s", p, stack))
  }
  ```
- **影响**：任何触发 panic 的请求（例如 CEL 过滤器里未检查的类型断言，见 `04-api-v1.md`）都会把文件路径、函数名、行号、部分内存值以错误消息形式回给客户端，属于信息泄露，也方便攻击者定位内部结构。
- **修复**：客户端只返回通用 internal 错误（可带 trace id），堆栈仅写日志。

---

## 高（High）

### H1. 全局授权拦截器被注释，权限字段从不校验
> **◐ 部分修复（阶段 0）** · `ec49607`
> - 已修：新增 `backend/api/v1/acl_interceptor.go` 的 `ACLInterceptor`/`NewACLInterceptor(store)` 并在 `grpc_routes.go` 接线（顺序：debug → auth → audit → acl）；拦截器消费 `AuthContext.Permission`——只要 proto 方法声明了非空 `permission` 就要求 workspaceAdmin。已在 proto 中声明 permission 的写方法见 `08-proto-contract.md` M17。`AuthContext.Permission` 现在有了真实消费者，不再是死字段。
> - 剩余：语义是"非空 permission ⇒ 管理员"，尚未实现 permission→role 的细粒度映射；读方法（Get/List/血缘/OpenLineage 读）仍未声明 permission，故仍是"任意已认证用户"。
> **✅ 已修复（阶段 4 收口）** · IAM 子系统：剩余两项都已关闭——**全部 v1 方法（含读路径）都声明了 `permission`**，`iam.Manager` 按 `workspaceMember` 读基线 / `workspaceAdmin` 全目录 / 自定义角色权限集解析，不再是"非空 ⇒ 管理员"；守卫测试 `TestEveryMethodIsPermissionGated` 要求除显式 allowlist（Login/Logout/GetCurrentUser/CreateUser/UpdateUser）外每个方法都带目录内注解。**剩余（有意）**：仍是单工作区、仅 WORKSPACE 策略，没有 per-resource 策略（阶段 6 确认按单租户部署，不做 per-instance 读授权）。

- **位置**：`backend/server/grpc_routes.go:80-88`（`// apiv1.NewACLInterceptor(...)` 在第 85 行）、`backend/api/auth/auth.go:350-355`
- **证据（修复前）**：`authContext.Permission` 被解析出来后没有任何消费者；`NewACLInterceptor` 在整个仓库已不存在（注释掉的代码无法编译）。所有 proto 方法都没有设置 `permission`。
- **影响**：任意已认证用户可执行所有 RPC。结合 C1 与公开注册（`02` 中 C2/C3），未认证攻击者可先注册再提权。
- **修复**：重建授权拦截器并强制 `AuthContext.Permission`；为每个 RPC 声明权限；或在 handler 内显式鉴权。
- **详见**：`02-auth-authorization.md`。

### H2. dev 模式恒为真 → CORS 永久全开且允许携带凭证
> **⏳ 未处理（阶段 0 范围外）** · 现状已部分缓解但仍不完整：
> - CORS 中间件本身是**条件安装**的（`if profile.Mode == common.ReleaseModeDev`），因此用 `-tags release` 构建时不注册任何 CORS 中间件 → 同源限制生效，H2 的主要风险消失。**但默认构建（`go build`/`make run`/现有 CI）不带该 tag，`Mode` 仍是 dev，CORS 依旧全开**。
> - cookie 侧未改：`GetTokenCookie` 仍按**客户端可控的** `Origin` 头决定 `SameSite`（https ⇒ `SameSite=None`），且没有 CSRF token。即使 CORS 关闭，这也只是深度防御缺口，建议一并修（例如依据服务端 TLS 配置、默认 `SameSite=Lax`）。
> - 修 CORS/CSRF 时的依赖项已解除：`-tags release` 现在可以编译（C1）。
> **✅ 已修复（阶段 6）** · `976ebc5`：CORS 改为显式 allowlist（`--cors-allow-origins`，dev 默认本地来源、release 默认空），不再用恒真的 `AllowOriginFunc`；`GetTokenCookie` 的 `SameSite` 默认 `Lax`、`Secure` 由服务端 TLS/`--external-url` 推导，不再受客户端 `Origin` 影响；新增同源校验中间件，对带 cookie 鉴权的非 GET/HEAD 跨站写请求返回 `403`。

- **位置**：`backend/server/echo_routes.go:25-35`、`backend/api/auth/header.go:46-50`
- **证据（默认构建仍是 dev）**：`if profile.Mode == common.ReleaseModeDev { ... AllowOriginFunc: func(string) (bool, error) { return true, nil } ... AllowCredentials: true }`，而默认构建的 `Mode` 为 `dev`；HTTPS 下 cookie 设为 `SameSite=None; Secure`，`origin` 又来自客户端可控的 `Origin`/`grpcgateway-origin` 头。
- **影响**：HTTPS 部署时任意站点可发起带凭证的跨域写请求（无 CSRF token），可创建/删除用户、创建实例等。
- **修复**：CORS 改为显式 allowlist 且与 `Mode` 解耦；`Secure` 依据服务端 TLS 配置而非请求头；增加 CSRF 防护。

### H3. gRPC 反射路径的认证豁免前缀不匹配（需验证实际行为）
- **位置**：`backend/server/grpc_routes.go:111-126`、`backend/api/auth/config.go:10-16`
- **证据**：`IsAuthenticationAllowed` 只豁免 `strings.HasPrefix(fullMethodName, "/grpc.reflection")`；但 connect 的 `grpcreflect` 生成的 proto package 是 `connectext.grpc.reflection.v1`（见 module cache 中 `internal/gen/.../reflection.pb.go`），procedure 不以 `/grpc.reflection` 开头。同时 `getAuthContext` 需要 `protoregistry.GlobalFiles` 中存在对应 descriptor。
- **影响**：两种可能之一 —— (a) 前缀不匹配使反射实际需要认证（安全，但 `IsAuthenticationAllowed` 的豁免分支是死代码）；(b) 若 descriptor 查找失败，反射直接报错，功能不可用。需要实际跑一次 `grpcurl` 确认。无论如何都应统一命名并显式决定是否暴露反射。
- **修复**：明确反射的访问策略；若需免认证，按真实 procedure 前缀匹配；否则删除豁免分支并保持需要 token。

### H4. `--debug` 与 `--enable-json-logging` 实际上无效（日志系统未接线）
> **✅ 已修复（阶段 1）** · `7fdcead`：`start()` 现在调用 `setupLogging(flags.enableJSONLogging)`，用 `slog.HandlerOptions{AddSource: true, Level: log.LogLevel, ReplaceAttr: log.Replace}` 构造 `NewTextHandler`/`NewJSONHandler` 并 `slog.SetDefault`。`LogLevel` 是 `*slog.LevelVar`，`--debug` 先 `Set(LevelDebug)` 再装 handler，因此调级生效；`log.Replace` 的 source 裁剪也生效（实测日志形如 `source=server/server.go:64`）。**剩余**：`log.Stack` 无论级别都 eager 采集 20 帧栈（`07` 低节）；日志输出改为 `os.Stdout`（此前 `slog.Default` 写 stderr）；`RuntimeDebug` 与日志级别仍是两套开关，没有运行时调级 API。

- **位置**：`backend/common/log/log.go:12-13,36-38`、`backend/bin/server/cmd/root.go:72,77-79`
- **证据**：`var LogLevel = new(slog.LevelVar)` 从未被安装到任何 handler；`slog.Default()` 仍使用默认 TextHandler + LevelInfo。`log.Replace`（裁剪 source 路径）同样从未安装。`--enable-json-logging` 注册了 flag 但没有任何读取处。
- **影响**：`--debug` 无法打开 debug 日志，`DebugInterceptor` 里所有 `slog.LevelDebug` 日志被静默丢弃；日志无法输出 JSON 供采集；source 路径裁剪不生效。
- **修复**：在 `start()` 中构造 handler（`slog.NewTextHandler`/`NewJSONHandler` + `HandlerOptions{Level: LogLevel, ReplaceAttr: log.Replace}`）并 `slog.SetDefault`，或删除这些 flag。

### H5. 大量 CLI flag 声明但从未注册，`externalURL` 缺失导致 SSO 回调地址为空
> **✅ 已修复（阶段 1，SSO 部分）** · `c4e22fc`（阶段 0）先让管理员可通过 `UpdateWorkspaceProfileSetting` 写 `external_url`；`7fdcead` 注册了 `--external-url` 并把它接进 `getBaseProfile`（`ExternalURL: flags.externalURL`），于是 `initializeSetting` 在启动时能把 `profile.ExternalURL` 写入 `WORKSPACE_PROFILE`（仅当非空），`auth_service.go` 拼 `"/oauth/callback"` 不再得到空 base。**剩余**：前端设置页仍未暴露 `external_url`（只能走 API 或 CLI flag）；`dataDir`/`ha`/`saas`/`demo`/`memoryProfileThreshold` 仍是"声明未注册"的 Bytebase 遗留，未清理。

- **位置**：`backend/bin/server/cmd/root.go:40-54,70-74`
- **证据**：`externalURL`、`dataDir`、`ha`、`saas`、`demo`、`memoryProfileThreshold` 都只在 struct 里声明，`init()` 只注册了 `port`/`enable-json-logging`/`debug`。而 `backend/api/v1/auth_service.go:262` 用 `setting.ExternalUrl` 拼 OAuth 回调；`initializeSetting` 写入的 `ExternalUrl` 来自 `profile.ExternalURL`（恒为空）。
- **影响**：SSO 登录回调 `"/oauth/callback"` 不完整，OAuth2 登录开箱不可用（除非管理员另行通过设置接口写入 external URL）。其余 flag 是 Bytebase 遗留。
- **修复**：注册并接线 `--external-url`（或从 `WORKSPACE_PROFILE` 设置读取），删除无用 flag。

---

## 中（Medium）

### M1. 关停路径可能直接 `os.Exit(1)`，并可能无限等待 runner
> **✅ 已修复（阶段 2）** · `fb8ca14`：`echoServer.Shutdown` 的错误改为 `slog.Error` 后继续（不再 `Logger.Fatal` ⇒ `os.Exit(1)`，因此 store 关闭与 stopper 都会执行）；`runnerWG.Wait()` 改为 `select` + `runnerShutdownTimeout`（10s，与 `gracefulShutdownPeriod` 一致），超时记 Warn 后继续退出。

- **位置**：`backend/server/server.go:146-181`
- **证据**：
  ```go
  if err := s.echoServer.Shutdown(ctx); err != nil {
      s.echoServer.Logger.Fatal(err)   // Fatal => os.Exit(1)
  }
  ...
  s.runnerWG.Wait()                    // 无超时
  ```
- **影响**：Shutdown 出错时进程立即退出，跳过 store 关闭与 `stopper`；runner 若不响应 `ctx`（例如卡在外部 DB I/O），`Wait()` 会一直阻塞，超过 graceful period 也无法退出。
- **修复**：记录错误而非 Fatal；给 `runnerWG.Wait()` 加超时（`select` + `time.After`）并在超时后强制返回。

### M2. `Server.Run` 派生并丢弃 context（死代码）
- **位置**：`backend/server/server.go:126-128`
- **证据**：`_, cancel := context.WithCancel(ctx); s.cancel = cancel` —— 派生出的 context 从未使用。
- **影响**：误导读者，`s.cancel` 只用于 Shutdown，实际没有任何 goroutine 监听它。
- **修复**：要么把派生 context 传给 server/goroutine，要么删除。

### M3. 启动时无条件开启 echo Debug 并 `fmt.Printf` 打印全部路由
- **位置**：`backend/server/server.go:116-119`
- **证据**：`s.echoServer.Debug = true` 与 `for _, route := range s.echoServer.Routes() { fmt.Printf(...) }`。
- **影响**：生产日志噪声；`fmt.Printf` 绕过 slog。属调试残留。
- **修复**：仅在 `RuntimeDebug`/debug 模式打印。

### M4. 连接池配置不完整，且可能被配置成"无限制"
> **✅ 已修复（阶段 2）** · `fb8ca14`：`maxOpenConns` 经 `clampMaxOpenConns` 钳制到 `[1, 50]`（原 `maxConns - reservedConns` 可为 0，而 `database/sql` 把 0 当作无上限）；新增 `SetMaxIdleConns(10)`、`SetConnMaxLifetime(30m)`、`SetConnMaxIdleTime(5m)`；`Initialize` 用 `sync.Once` 保护（并发调用不再可能双开连接池并泄漏其一）；删除未使用的 `stopWatcher` 字段与冗余的 `_ "github.com/jackc/pgx/v5"` 导入。`GetDB()` 在初始化前仍返回 nil（由 `sync.Once` 保证只初始化一次，调用方在 `New` 失败时不会继续）。

- **位置**：`backend/store/db_connection.go:16,19-24,53-82`
- **证据**：`stopWatcher chan struct{}` 创建后从未使用；`maxOpenConns := maxConns - reservedConns; if maxOpenConns > 50 { maxOpenConns = 50 }` 没有下限，若 `SHOW` 结果异常得到 0 或负数，`database/sql` 将其解释为**无限制**；未设置 `SetMaxIdleConns`（默认 2）、`SetConnMaxLifetime`、`SetConnMaxIdleTime`；`Initialize`/`GetDB` 无同步，初始化前 `GetDB()` 返回 nil。
- **影响**：误配置下连接数失控；空闲连接默认只有 2 个，高并发时连接频繁重建；经过 NAT/PgBouncer 时缺少 lifetime 可能导致陈旧连接错误；并发初始化存在竞态。
- **修复**：`max(1, ...)` 下限；设置 idle/lifetime；删除死字段与冗余的 `_ "github.com/jackc/pgx/v5"` 导入；用 `sync.Once` 保护初始化。

### M5. `initializeSetting` 每次启动都写库，首次引导判定语义隐晦
- **位置**：`backend/server/init.go:15-134`
- **证据**：无论是否首次启动，末尾都会 `UpsertSettingV2(WORKSPACE_PROFILE)`（第 92-97 行）；`firstTimeOnboarding` 的判定来自 `CreateSettingIfNotExistV2(BRANDING_LOGO)` 的第二个返回值（第 20 行），语义上"品牌 logo 是否存在"等于"是否首次"。
- **影响**：每次启动一次无谓写；`firstTimeOnboarding` 与"首次"概念耦合脆弱，若 BRANDING_LOGO 被删除会重复授予 `allUsers` workspaceMember 角色。
- **修复**：用显式的 onboarding 标记或 workspace 创建时间判断；仅在值变化时 upsert。

### M6. 其它装配细节
> **◐ 部分修复（阶段 6）** · `f112e5c`：`/metrics` 改为受 `RuntimeDebug` 门控（默认关闭）；gateway 客户端补发送消息大小上限并与既有接收上限对齐。**剩余**：`recoverMiddleware` 的栈采集时点与未使用的 `GatewayResponseModifier.Store` 字段本轮未改。
- **位置/证据**：
  - `backend/server/echo_routes.go:69-83`：`recoverMiddleware` 中 `log.Stack("panic-stack")` 取到的是 recover 之后的栈，不是 panic 发生点的栈。
  - `backend/server/grpc_routes.go:130-135`：gateway 客户端 `grpc.MaxCallRecvMsgSize(100MB)` 仅限制接收；未见对应的发送/请求体大小限制。
  - `backend/server/echo_routes.go:58-62`：`echo-contrib/prometheus` 的 `/metrics` 未做鉴权（pprof 至少受 `RuntimeDebug` 控制）。
  - `backend/server/grpc_routes.go:46-61`：`GatewayResponseModifier.Store` 字段未被使用。
- **影响**：可观测性/安全边界的小缺口。
- **修复**：修正栈采集；统一消息大小限制；把 `/metrics` 放到独立监听地址或加鉴权；删除未用字段。

---

## 低（Low）／遗留债务

- `backend/server/ultimate.go` 用空导入注册 driver/schema/lineage 插件，属于编译期插件模式；与 `--minidemo` build tag 搭配，但 `minidemo` 在仓库中没有任何对应实现文件，tag 实际无意义。
- `backend/server/server_frontend_not_embed.go`：`GET /*` 返回占位页，前端未内嵌（AGENTS.md 已注明）；`embed_frontend` build tag 也没有对应实现文件，`--tags embed_frontend` 会编译失败（缺 `embedFrontend` 定义）。**需验证**。
- `backend/config/profile.go:19-20` `LastActiveTS` 只写不读（许可证/活跃度遗留）。
- `backend/server/pprof.go:11-21`：pprof 只受启动时的 `--debug` 控制，`RuntimeDebug` 注释说"can be set in runtime"，但没有任何运行时开关 API。
- `backend/bin/server/cmd/root.go:20-36` 的 banner、`flags.ha/saas/demo` 均为 Bytebase 遗留。
- `backend/server/echo_routes.go:50` TODO：前端内嵌待实现。

---

## 建议的整改顺序

1. ~~修 C1（JWT 密钥）与 H1（授权）~~ —— **阶段 0 已完成**：C1 ✅（环境注入 + fail-closed + 可用 prod profile，`adfec91` `84b16db`），H1 ◐（拦截器重建并接线，`ec49607`；读路径与细粒度映射留待后续）。
2. ~~修 C2（堆栈回传）~~ ✅（`5446a10`）；**H2（CORS/CSRF）仍未处理**——用 `-tags release` 可绕开全开 CORS，但默认构建仍是 dev，且 cookie 的 `SameSite` 逻辑与 CSRF 防护未改。
3. ~~接线日志系统（H4）与 `--external-url`（H5）~~ —— **阶段 1 已完成**：H4 ✅（`setupLogging` + `slog.SetDefault`，`7fdcead`），H5 ✅（`--external-url` 注册并接入 profile，`7fdcead`）。剩余：其余未注册的遗留 flag、前端 `external_url` 入口。
4. ~~清理关停路径（M1）与连接池（M4）~~ —— **阶段 2 已完成**：M1 ✅（不再 `Fatal`，runner 等待 10s 上限），M4 ✅（钳制 + idle/lifetime/idleTime + `sync.Once`），`fb8ca14`。剩余：M2（派生后丢弃 context）、M3（启动打印全部路由与 `echo.Debug=true`）。
5. 删除未注册 flag、调试残留与死字段；~~补一个真正使用 `-tags release` 的构建目标（Makefile/CI）~~ —— **构建目标已补**（`make build-release`，`7fdcead`）；flag/死字段清理未做，CI 与 Docker 仍不存在。
