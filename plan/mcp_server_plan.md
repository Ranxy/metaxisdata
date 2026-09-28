# Plan: 服务端内置 MCP(Model Context Protocol)+ OAuth 2.1 授权

> **Status: design / 未实现。** 已按评审意见定稿:确认项见"已确认的决定";**MCP 的能力面按 MCP 自身重新设计,不继承 CLI 的历史取舍**(见"MCP 原生设计"一节)。

## TL;DR

在**服务端**(`backend/`)内置 MCP:metaxisdata 同时扮演 OAuth 2.1 授权服务器(AS)与资源服务器(RS),在 `/mcp` 暴露 **Streamable HTTP + 无状态会话**,对外提供一组只读工具(实例/库/元数据/血缘/身份)。

三条主线:

- **授权**:标准 MCP OAuth 2.1 —— PRM(RFC 9728)+ AS Metadata(RFC 8414)+ Authorization Code + PKCE(S256)+ RFC 8707 `resource` + DCR(RFC 7591)。令牌复用现有 JWT 签发内核,只换 audience 并加 `scope`;TTL 沿用 7 天、不发 refresh token。整个 MCP 面由一个工作区开关 `mcp_enabled` 控制。
- **能力**:复用现有 9 个只读 RPC 的 service 实现,proto 只为授权确认页新增 `OAuthService` 的 2 个方法。**数据面零新增 RPC**。
- **接口**:按 MCP 原生设计 —— 用**名字寻址**代替让模型复制 GUID;用**投影 + 省略空字段 + 紧凑 JSON** 代替"稳定但臃肿的信封";临时关系**一次给全**而不是藏在 flag 后面;列表只回轻量行,详情另调。逐条对照见"MCP 原生设计"。

不做的:stdio(服务端没有这个语义)、写操作工具(只读;写要确认语义)、refresh token、把 `mxd` 的 device login 令牌当 MCP 令牌用、把 CLI 的 `SKILL.md` 当作 MCP 的说明文档(两者是不同的接口)。

---

## 已确认的决定

| # | 决定 | 影响 |
| --- | --- | --- |
| 1 | **MCP 能力面要按 MCP 重新设计**,不必兼容 CLI 的历史语义 | 新增"MCP 原生设计"一节;寻址、输出、flag、错误、文档全部重做 |
| 2 | consent RPC 放**新的 `OAuthService`** | 新建 `proto/v1/v1/oauth_service.proto`;2 个方法、无 permission 注解,需登记 ACL 白名单 |
| 3 | `oauth_client` **落库** | 一次 migration 三处同改;解锁后续"已连接应用"管理页 |
| 4 | access token **7 天、不发 refresh token** | 沿用 `GetTokenDuration`;到期由客户端重新走授权 |
| 5 | **默认无状态会话**(MCP v2 的方向) | `StreamableHTTPOptions.Stateless`;没有 session 存储、没有会话劫持面、天然多副本;放弃 server→client 请求 |
| 6 | 增加一个 **OAuth/MCP 开关** | `WorkspaceProfileSetting.mcp_enabled`;关闭时整个 MCP/OAuth 面不发布 |
| 7 | **工具调用进审计** | 需要把 audit 原语从 package `v1` 抽到共享包;`audit_log` 量会上升(永久保留) |
| 8 | **`mcp_enabled` 默认 `false`** | 新攻击面由管理员显式开启;未开启时整个 MCP/OAuth 面 404 |
| 9 | **输出用 `outputSchema` + `structuredContent`** | MCP 原生、可校验;实现方式已由 Phase 0 改定为 raw handler + 只写 `structuredContent`(避免 SDK 的文本副本翻倍),老协议版本(<`2025-06-18`)按需补紧凑文本副本 |
| 10 | **SDK 钉 v1.8.0** | 2026-09-04 发布;规范支持与 v1.7.0+ 相同(最新 2026-07-28)。注意 v1.8.0 把跨域保护的责任交给调用方(标准库中间件包裹),见"挂载与中间件" |

---

## Phase 0 结论(已实测)

**SDK**:`github.com/modelcontextprotocol/go-sdk v1.8.0` 已加入 `go.mod`(direct);顺带引入 `google/jsonschema-go`、`segmentio/{encoding,asm}`、`yosida95/uritemplate/v3`,并按 MVS **强制升级** `golang.org/x/sync v0.19.0→v0.20.0`、`golang.org/x/time v0.11.0→v0.15.0`(SDK 的 go.mod 要求这两个下界)。

证据是 `backend/mcp/sdk_contract_test.go`(8 个测试,`-race` 通过,lint 干净)。它只碰 SDK、不碰 Backend 代码,但保留为**契约测试**:下面每一条都是设计赖以成立、且 SDK 升级可能悄悄改变的假设。

| # | 问题 | 实测结果 | 对设计的影响 |
| --- | --- | --- | --- |
| 1 | SDK 是否支持 `2026-07-28` | `mcp.SupportedProtocolVersions()` = `[2026-07-28 2025-11-25 2025-06-18 2025-03-26 2024-11-05]` | 无状态设计成立;测试里断言该版本存在 |
| 2 | 工具 handler 能否拿到已验证身份 | 能。无状态 + `RequireBearerToken` 下,handler 通过 `req.Extra.TokenInfo` 看到 `userId`、`scopes`,并通过 `req.Extra.Header` 看到原始请求头 | 工具层的身份注入、IAM 检查、审计全部可行;不需要自己解析 header |
| 3 | 工具里能否读到协商后的协议版本 | 能。`req.Session.InitializeParams().ProtocolVersion` 在无状态下**按请求**填充(实测 `2026-07-28` 与 `2025-06-18` 都正确) | 让"只为老客户端补文本副本"成为可实现的分支条件(见下) |
| 4 | 缺凭据/缺 scope 的失败形态 | 401:`WWW-Authenticate: Bearer resource_metadata="…", scope="metaxisdata.mcp.read"`;403 同形(仅状态码不同)。**两者都不带 `error="insufficient_scope"`** | 401 的发现链路成立。403 与 RFC 6750 / MCP 的 SHOULD 有偏差:客户端仍能从 `scope=` 得到升级线索;若将来有客户端依赖 `error=` 参数,在 RS 外面包一层补上 |
| 5 | 一个无状态 handler 是否能同时服务新旧协议 | 能。SDK 客户端分别以 `2026-07-28` 与 `2025-06-18` 调用同一端点都成功;裸 `initialize`(2025-06-18)返回**不含 `Mcp-Session-Id`**,随后不带 session id 的 `tools/list` 也成功 | 旧客户端无需降级部署,也不需要为它们维护有状态路径 |
| 6 | 结构化输出在线上到底传了什么 | **generic `AddTool`(typed)会给 `structuredContent` 再补一份 TextContent JSON 副本**:25 行样例 response 3681B(content 1914B + structuredContent 1585B)。**raw handler 只设 `StructuredContent` 时 content 是空数组**:1769B | **工具一律用 `Server.AddTool` + 显式 schema,只写 `StructuredContent`**,避免每个结果都付 2 倍;见下 |
| 7 | 客户端不关掉 standalone SSE 会怎样 | SDK 客户端保持默认(启用 SSE)对无状态端点仍能正常 `tools/call`(GET 405 被容忍) | 不要求客户端设置 `DisableStandaloneSSE`;我们自己的集成测试仍会设它以免噪声 |
| 8 | audience 比较的规范化边界 | `oauthex.MatchesResource` **只容忍尾部斜杠**;大小写、默认端口都不归一;空 claims 永不匹配 | `external_url` 必须**先规范化一次**(小写 scheme/host、去尾斜杠、去默认端口),PRM 的 `resource` 与 JWT 的 `aud` 都用同一份规范化结果 |

### 由 Phase 0 改定的设计

1. **输出只写一份**:`backend/mcp` 的工具用 `Server.AddTool(&mcp.Tool{...显式 InputSchema/OutputSchema...}, rawHandler)`,handler 返回 `&mcp.CallToolResult{StructuredContent: <compact JSON>}`,**不设 `Content`**。理由:#6 的 2.08 倍开销;而且我们要的"紧凑 JSON"不能交给 SDK 的自动副本(它不是我们控制的编码)。
2. **老客户端的文本副本按协议版本兜底**:`structuredContent` 是 SEP-2106(`2025-06-18`)引入的,更早的客户端只读 `content`。因为 #3 证明版本可读,规则是:`req.Session.InitializeParams().ProtocolVersion < "2025-06-18"` 时额外附一个紧凑文本副本,否则不附。这样当代客户端付一份,老客户端仍可用。
3. **资源标识符规范化**:新增一个"从 `external_url` 得到规范化 issuer/resource"的函数,PRM、AS metadata、`aud`、授权请求的 `resource` 校验全部走它(#8)。
4. **403 的 `error` 参数**:v1 用 SDK 默认;在 DoD 里记录这条已知偏差,并在 Phase 4 留一个"必要时包一层"的钩子。

---

## 需求与约束

### 需求

让 agent 通过 MCP 调用 metaxisdata 的元数据与血缘能力:结构查询、DDL、按名搜索、SQL 血缘分析、多层上下游血缘;并且**远程可用、有授权、可被多个 agent 并发使用**。

### 约束

| # | 约束 | 影响 |
| --- | --- | --- |
| 1 | 服务端内置,挂 `/mcp` | 工具实现直接在 `backend/` 里调 store/service;无客户端装配 |
| 2 | 仅 Streamable HTTP | 服务端没有 stdio 语义;规范本身也要求 stdio 场景改用环境凭据 |
| 3 | MCP 规范的 OAuth 鉴权 | 需要自研 AS(authorize / complete / token / register)+ RS 元数据 + 浏览器确认页 |
| 4 | 只读工具 | 不暴露 `CreateManualSQL`、`SyncInstance` 等写 RPC |
| 5 | 自托管、单二进制、SSO 可选 | 不能假设存在支持 RFC 8707 的外部 IdP,AS 必须自带 |
| 6 | 多副本可部署 | 无状态会话 + 无 session/进程内凭据状态(授权请求与授权码仍是进程内短期状态,见风险) |
| 7 | 平台既有姿态不变 | 单租户 workspace-scoped 授权;审计永久保留;凭据只混淆不加密 |

---

## 现状盘点

### 可直接复用

| 能力 | 位置 | 用途 |
| --- | --- | --- |
| JWT 签发与校验 | `backend/api/auth/auth.go`:签发内核 `generateToken(userName, userID, **aud**, expirationTime, secret, restriction)` :352、`GenerateAccessToken` :346、claims :321、校验 `VerifyAccessToken` :185(**已含 audience 校验**,`AccessTokenAudienceFmt = "mt.user.access.%s"` :35)、`GetTokenDuration` :91 | MCP 令牌 = 现有 JWT 换一个 audience + 加 `scope`;**audience 机制已存在**,不需要新造隔离机制 |
| 令牌身份解析链(吊销 LRU、用户查找、停用、改密截断) | `auth.go` `authenticateConnect` :222(revocation `TokenExpireCache` :226、`GetUserByID` :237、`MemberDeleted` :244、改密截断 :251) | RS 中间件必须走同一条链,不能只验签名 |
| IAM 引擎 | `backend/api/v1/acl_interceptor.go:18` 的 `PermissionChecker.CheckPermission(ctx, perm, user)`;生产实现 `(*iam.Manager).CheckPermission`(`backend/component/iam/manager.go:50`);权限目录 `backend/common/permission` | 工具层注入同一个 checker;权限串必须等于 RPC 注解值 |
| ACL 守卫测试骨架 | `backend/api/v1/acl_interceptor_test.go:133` `TestEveryMethodIsPermissionGated` + `methodPermission` :208 + `unannotatedMethods` :115 | 新增 consent RPC 必须登记;照此新增"工具↔RPC 权限一致 + 只读子集"守卫 |
| 审计写入与脱敏 | `backend/store/audit_log.go:25` `CreateAuditLog`;`backend/api/v1/audit.go`(`auditContext` :46、`marshalAuditMessage` :172、`sanitizeAuditValue` :194、`isSensitiveAuditField` :228、`mapSeverity` :317、`buildAuditStatus` :333、`buildRequestMetadata` :348) | 工具调用审计;脱敏原语要抽到共享包 |
| 匿名端点限流的现成形状 | `backend/server/openlineage_ingestion.go:28` `openLineageIngestionMiddleware`(`RateLimiterMemoryStore` + 429 DenyHandler;挂载 `grpc_routes.go:238`) | `/oauth/token`、`/oauth/register` 照抄 |
| 进程内短期状态 + 限流 | `backend/component/state`(device login store 的 TTL / 容量 / 一次性消费;`DeviceLoginLimiter.Allow`)、`state.go:22-59` | pending 授权请求与授权码;构造函数未导出,需在 `state.New()` 里注册 |
| 浏览器确认页模式 | `frontend/src/pages/DeviceLoginPage.vue`、路由 `/device`(`router/index.ts:261`)、`frontend/src/api/device-login.ts` | consent 页照抄结构 |
| 工作区设置读写 | `backend/store/setting.go:51` `GetWorkspaceGeneralSetting`;`backend/api/v1/setting_service.go` 的 `UpdateWorkspaceProfileSetting` mask 与 `convertToWorkspaceProfileSetting`;`proto/store/store/setting.proto:26` | 新增 `mcp_enabled` 开关;**JSONB protojson,只需加字段,无需 migration** |
| `external_url` → 页面地址 | `auth_service_device_login.go:173` `deviceLoginVerificationURI`(常量 `devicePagePath` :29) | consent / complete 地址拼法 |
| 纯 HTTP 路由挂载 | `backend/server/grpc_routes.go:232-239`(OpenLineage 的 `e.Group` + 自带中间件) | `/oauth/*`、`/.well-known/*`、`/mcp` 同处注册 |
| 数据面 9 个只读 RPC | `ListInstances`(`metaxisdata.instances.list`)、`ListDatabases`(`metaxisdata.databases.list`)、`ListMetadata`/`GetMetadata`/`SearchMetadata`/`GetSchemaString`(`metaxisdata.databases.read`)、`AnalyzeSQL`/`GetLineageGraph`(`metaxisdata.lineage.get`)、`GetCurrentUser`(无注解,已白名单) | 工具**进程内直接调用这些 handler 方法**,不再需要抽 service 层 |
| MCP 官方 Go SDK | `github.com/modelcontextprotocol/go-sdk` **v1.8.0**(2026-09-04 发布;本地 module cache 只到 v1.7.0,需联网 `go get`;规范支持与 v1.7.0+ 一致,最新 2026-07-28) | `mcp.NewServer`/`AddTool`、`NewStreamableHTTPHandler`、`mcp.SupportedProtocolVersions()`(测试断言用)、`auth.RequireBearerToken`、`auth.ProtectedResourceMetadataHandler`、`oauthex.MatchesResource` |

### 缺口

1. **没有 OAuth 授权服务器**:authorize / complete / token / register、PRM 与 AS metadata、PKCE 校验、client 注册表、pending 授权请求与授权码存储全要新写。SDK 只提供**客户端**侧与 **RS** 侧,AS 侧自研。
2. **令牌只有一种 audience**(`mt.user.access.<mode>`,且已被强制校验):新增 MCP audience 常量即可;ConnectRPC 侧不需要新代码就会拒绝 MCP 令牌,只需一条测试钉住。
3. **没有确认页**:`/oauth/consent` 页面 + `OAuthService` 的 2 个方法。
4. **没有 MCP 资源服务器**:工具表、寻址解析、渲染、权限适配、审计、错误信封、引导文档都不存在。
5. **工具实现逻辑嵌在 RPC handler 里**:原本以为要把逻辑抽成 service 函数才能复用,核实后确认这 9 个只读 handler 不依赖拦截器 context,因此 MCP 工具可以进程内直接调用 handler 方法,不需要抽函数(也就没有漂移风险)。
6. **`/oauth/*` 与 `/mcp` 不在 Connect 拦截器覆盖内**:认证、限流、审计、日志都要显式接线。
7. **audit 原语是 package `v1` 未导出的**:`backend/mcp` 要写审计行,必须先抽到共享包(例如 `backend/component/audit`)。
8. **没有开关**:`mcp_enabled` 要从 store proto 一路加到前端设置页。

---

## 方案总览

```
                        ┌─────────────────────────── metaxisdata server (Go, 单二进制) ──────────────────────────┐
  MCP client            │  ┌── OAuth 2.1 AS ────────────────────────────┐   ┌── Resource Server ─────────────┐  │
  (Claude Code /        │  │ GET  /.well-known/oauth-authorization-server│   │ POST /mcp (Streamable HTTP,    │  │
   Cursor / VS Code /   │  │ GET  /.well-known/oauth-protected-resource  │   │            stateless)          │  │
   Codex / 自研 agent)   │  │ GET  /oauth/authorize   (web session)       │   │  ├ RequireBearerToken(JWT+aud) │  │
        │               │  │ GET  /oauth/authorize/complete (302 code)   │   │  ├ backend/mcp 工具表           │  │
        │ ① 401 +       │  │ POST /oauth/token       (PKCE / resource)   │   │  │   ├ 名字寻址解析            │  │
        │ WWW-Authenticate│  │ POST /oauth/register   (DCR, 限流)         │   │  │   ├ IAM 权限检查           │  │
        │ resource_metadata│ └────────────────────────────────────────────┘   │  │   ├ 审计(逐次)            │  │
        │               │                                                   │  │   └ 投影/紧凑 JSON         │  │
        │ ② 浏览器授权   │  ┌── ConnectRPC(现有 + 新) ─────────────────┐      │  └───────────────────────────────┘  │
        │ ③ 带令牌调 /mcp └─►│ AuthService / OAuthService(consent 2 方法) │      │  ┌── 前端 SPA ─────────────────┐  │
        └──────────────►   │ Instance/Database/Lineage/User(9 个只读)   │◄─────┼──┤ /oauth/consent(新)          │  │
                           │ SettingService(开关)                      │cookie│  │ /settings/general(开关)     │  │
                           └───────────────────────────────────────────┘      │  └─────────────────────────────┘  │
                        └──────────────────────────────────────────────────────────────────────────────────────┘
```

**调用链**:无令牌请求 `/mcp` → 401 + `WWW-Authenticate: Bearer resource_metadata=…` → 读 PRM 得到 AS → 读 AS metadata → (DCR 注册) → 带 PKCE + `resource` 打开浏览器 → 已登录用户在 `/oauth/consent` 确认 → 服务端在 `/oauth/authorize/complete` 生成授权码并 302 回客户端 → 客户端用 `code_verifier` + `resource` 换令牌 → 带令牌请求 `/mcp`,每个请求独立验令牌 → 工具执行(寻址解析 → IAM → service → 渲染 → 审计)。

### 决策表

| 决策 | 选择 | 理由 / 放弃的备选 |
| --- | --- | --- |
| 交付形态 | 服务端内置,挂 `/mcp` | 工具层直接复用 store/service |
| 传输 | 仅 Streamable HTTP | 服务端无 stdio 语义 |
| 会话 | **无状态**(`Stateless: true`) | **协议 2026-07-28 的强制前提**:SDK 明确该版本"只在 `Stateless=true` 时被接受";同时无 session 存储、无可劫持的会话、天然多副本、每请求独立验令牌。代价:没有 server→client 请求(sampling/roots/progress);工具级确认改用 2026-07-28 的 MRTR(见"会话模式")。旧协议(≤2025-11-25)由 SDK 按请求协商继续支持 |
| AS 归属 | metaxisdata 自身同时是 AS 与 RS | 自托管零外部依赖;SSO 是可选功能,外部 IdP 普遍不支持 RFC 8707。备选(后续):PRM 的 `authorization_servers` 指向外部 IdP |
| 客户端注册 | v1 实现 **DCR(RFC 7591)** public client;预留 CIMD | 主流客户端仍走 DCR;spec 2026-07-28 把 DCR 标为 deprecated 但保留兼容;CIMD 需要 AS 主动抓 URL(SSRF 面),后续评估 |
| 令牌 | 复用现有 JWT + 新 audience + `scope`;TTL 7 天;不发 refresh | 已确认;规范只 SHOULD 短时令牌;客户端遇 401 会自动重走授权。备选:短时 + 轮换 refresh(需落库与检测复用) |
| OAuth scope | 单一粗粒度 `metaxisdata.mcp.read` | 真正的授权仍是平台 IAM;把 IAM 权限串当 scope 会造出第二套 ACL |
| 开关 | `WorkspaceProfileSetting.mcp_enabled`,默认 **false**;关闭时整个 MCP/OAuth 面不发布 | 新攻击面应由管理员显式开启;JSONB 加字段无需 migration |
| Audience 纪律 | MCP 令牌的 `aud` 是**资源 URI**(`<external_url 规范化>/mcp`),`/mcp` 只认它;Connect 侧沿用现有 `mt.user.access.<mode>` 校验 | 规范 MUST audience 绑定、MUST NOT passthrough;机制已在 `VerifyAccessToken` 里,新增的只是第二个 audience 值;双向隔离由测试钉住(`backend/api/auth/authenticator_test.go`) |
| 工具实现 | MCP 工具**进程内直接调用现有 handler 方法**(用 `connect.NewRequest` 传参),不再抽 service 函数;工具显式做 IAM 检查与审计 | 已核实这 9 个只读 handler 都**不依赖拦截器提供的 context**(不用 `requirePermission`、不读 `req.Header()`/`req.Peer()`,只有 `GetCurrentUser` 用 `GetUserFromContext`)。因此这比"抽函数"是更强的单一实现保证:同一份代码、零漂移风险,改动面也最小。代价:MCP 层要自己把用户放进 ctx(`common.UserContextKey`),并自己做权限与审计。备选:进程内 HTTP 回环 dispatch —— 隐藏跳转 + 重复审计,放弃 |
| 权限一致性 | 守卫测试:工具权限串必须等于对应 RPC 注解值,且只引用只读 RPC | 复刻 `TestEveryMethodIsPermissionGated` 的思路 |
| 输出契约 | 与 CLI 共享**语义**(字段名、enum 名、错误码词表),**不共享渲染选项** | 见"MCP 原生设计";省略空字段 + 紧凑 JSON + 投影 |
| 审计 | **每次工具调用一条审计行**(OAuth 事件同样) | 已确认;需要先把 audit 原语抽到共享包 |
| 能力面 | 只读 9 个工具 | 已确认;写操作需要 elicitation/确认语义,另立方案 |

---

## MCP 原生设计(刻意偏离 CLI 的地方)

CLI 的很多取舍是为"单机、无状态、被脚本解析的进程"服务的,搬到 MCP 会变成模型的负担。下表是逐条重做的结果。

| 议题 | CLI 的做法 | MCP 的选择 | 为什么不同 |
| --- | --- | --- | --- |
| **对象寻址** | 只接受不透明 GUID;skill 明确要求"复制、不许手拼" | 接受**名字路径**:`{instance, database?, schema?, name?}`,同时接受已从别的工具结果里拿到的 `guid` 直接透传 | 服务端自己知道各引擎的 GUID 布局,没有理由把这个负担交给模型;模型擅长名字、不擅长 `1;shop;;orders`。这条消除了 CLI 里整条"GUID 只能复制"的规则与它带来的多轮往返 |
| **SQL 分析的作用域** | `METAXISDATA_SCOPES` 环境变量(进程级)+ `--scope` 覆盖;scope 是 `name=guid` | 入参 `scopes: [{instance, database, schema?}]`,名字或 `instances/<id>`;**缺失时返回结构化候选**而不是猜 | 环境变量的设计目标是"同机多项目互不干扰",服务端没有这个概念;给模型一个"必须先在别处导出变量"的前置条件是 CLI 的偶然约束 |
| **缺失 scope 的失败** | `code=scope_required` + 一段文字 hint | 同上,但 `details` 里带**可选的库清单**(实例名 + 库名 + guid),模型一轮即可选对 | 工具错误应当是模型能直接消费的结构,而不是让它去读帮助文本 |
| **输出渲染** | `protojson` + `EmitUnpopulated: true` + 2 空格缩进 | **省略未设字段 + 紧凑 JSON**;共享的只有字段名风格与 enum 名 | `EmitUnpopulated` 是为"脚本不必区分缺失与空"服务的,在模型上下文里纯粹是噪声;缩进的开销与结果大小成正比 |
| **列表类结果** | `meta list` 返回完整 `StoredMetadata`(含列、索引、分区…) | 只回**投影行**(`guid`/`name`/`metaType`/`parentGuid`),完整结构走 `get_metadata` | 列 100 张表时,全量对象是上下文的主要杀手;详情本来就该是另一次调用 |
| **临时关系** | 默认隐藏,靠 `--include-temp` 打开并附 warning | 一次返回,分两个字段:`relations` 与 `temporaryRelations` | 藏起来会逼出第二次调用;而"这些输出列从哪来"恰恰是裸 SELECT 的唯一答案 |
| **图与列过滤** | 客户端 `--column` 过滤、客户端拼 N+1 次图展开 | 服务端过滤 + 服务端组合(调用方只给 `depth`) | 让模型做数据搬运和拼接是浪费 token 且易错 |
| **分页** | 默认收 1000 条,触顶给 `truncated` | `page_size`(默认 25、上限 200)+ `page_token`,响应给 `nextPageToken`/`hasMore` | 1000 条默认值在 CLI 里是"别偷偷丢数据",在 MCP 里是"直接炸上下文";小而明确的游标更合适 |
| **错误** | `code` + 文字 `hint`,外加退出码 | 保留 `code` 词表(去掉退出码),加结构化 `details` | 退出码是进程概念,MCP 没有;`details` 让模型能自纠 |
| **两层授权** | 退出码 1/2/4 区分 | 传输层(401/403 + `WWW-Authenticate`)vs 工具层(`isError` + `permission_denied`) | 只有 HTTP 层的错误才能触发客户端的重新授权 |
| **引导文档** | 二进制内嵌 `SKILL.md`,含 flag、退出码、环境变量、`mxd` 命令 | `ServerOptions.Instructions`(简短、工具选择导向)+ 一个 MCP **prompt**(`metaxisdata_guide`) | CLI 文档讲的是"怎么跑命令",对 MCP 客户端毫无意义;两者是不同的接口,不应共用一份文档(交叉引用即可) |
| **工具命名** | 命令树 `meta search` / `lineage sql` | MCP 习惯的 `search_metadata` / `analyze_sql`(`list_*`/`get_*`) | 名字对模型的影响远小于描述;描述里写"什么时候用、什么时候不要用、一个例子" |
| **作用域命名标签** | `dev=1;shop` 里的 `dev` | 不存在 | 名字标签是为了在输出里归因用户自定义的 scope;这里 scope 就是库本身 |
| **scope 数量上限与分批** | 服务端 10 个 + 客户端分批 | 保留服务端上限,超限直接报错 | 名字寻址下多 scope 不再是常态,不必为它写批处理 |

**仍然保留的 CLI 遗产**(因为它们与接口无关,只与平台有关):错误码词表、proto 字段命名、enum 名字、workspace-scoped 授权、审计姿态、`external_url` 是唯一地址来源。

---

## 授权设计(OAuth 2.1 AS + RS)

### 端点

| 端点 | 方法 | 认证 | 说明 |
| --- | --- | --- | --- |
| `/.well-known/oauth-protected-resource` 与 `.../mcp` | GET | 匿名 | RFC 9728 PRM:`resource`=`<external_url>/mcp`、`authorization_servers`=[`<external_url>`]、`scopes_supported`=[`metaxisdata.mcp.read`]、`bearer_methods_supported`=["header"]、`resource_name`。用 SDK 的 `auth.ProtectedResourceMetadataHandler`(自带 CORS `*`) |
| `/.well-known/oauth-authorization-server` | GET | 匿名 | RFC 8414 AS metadata:`issuer`=external_url、`authorization_endpoint`、`token_endpoint`、`registration_endpoint`、`response_types_supported=["code"]`、`grant_types_supported=["authorization_code"]`、`code_challenge_methods_supported=["S256"]`、`token_endpoint_auth_methods_supported=["none"]`、`scopes_supported`、`authorization_response_iss_parameter_supported=true`。**不发 `jwks_uri`**(见风险) |
| `/oauth/authorize` | GET | 浏览器既有登录态 | 校验 `client_id` / `redirect_uri`(精确匹配,loopback 允许任意端口)/ `response_type=code` / `code_challenge`+`S256` / `resource`;未登录先跳登录页并保留参数;通过后 302 到 SPA `/oauth/consent?request_id=…` |
| `/oauth/authorize/complete` | GET | 浏览器会话 | 按 `request_id` 生成授权码并 302 回客户端 `redirect_uri`(带 `code`+`state`+`iss`);**只有批准者本人的会话能完成**。授权码因此不经过 RPC 响应与前端 JS |
| `/oauth/token` | POST(form) | 匿名 + PKCE | `authorization_code`:`code`+`redirect_uri`+`code_verifier`+`resource` → 签发带 MCP audience 与 `scope` 的 JWT;错误按 RFC 6749 返回 JSON |
| `/oauth/register` | POST(JSON) | 匿名 + 限流 | RFC 7591 DCR,只接受 public client(`none`);`redirect_uris` 必须 HTTPS 或 loopback;返回 `client_id`;不做 registration access token |
| `/oauth/revoke` | POST(form) | 匿名 | RFC 7009;可选,与现有 `Logout` 语义对齐 |
| `OAuthService.GetOAuthAuthorizationRequest` | ConnectRPC | 需登录,无 permission 注解 | consent 页取详情(客户端名与版本、重定向主机、resource、scope、发起时间、来源 IP);**响应不含 code** |
| `OAuthService.ApproveOAuthAuthorizationRequest` | ConnectRPC | 需登录,无 permission 注解,`audit=true` | 入参 `request_id` + `approve`;响应只回 `approved`;审计只记"谁批准/拒绝了哪个 client 与 scope" |

> 两个 consent RPC 与 device login 的 `GetDeviceLogin`/`ApproveDeviceLogin` 同型(已认证、无 permission 注解),必须登记进 `backend/api/v1/acl_interceptor_test.go` 的 `unannotatedMethods`,否则 `TestEveryMethodIsPermissionGated` 失败。

### 令牌与 claims

沿用 `generateToken`(`:352`,**本来就接收 `aud`**)的签发内核,新增公开入口(如 `GenerateMCPAccessToken`):

| claim | 值 | 强制点 |
| --- | --- | --- |
| `aud` | **MCP 资源的规范化 URI**(RFC 8707):PRM 的 `resource` 与 JWT 的 `aud` 由同一次 `external_url` 规范化产出 | `/mcp` 用同一套 audience 校验逻辑校验该值;`VerifyAccessToken` 只认用户 audience,所以 **MCP 令牌天然无法用于 ConnectRPC**(反之亦然)。**实现时修正**:不再用 `mt.mcp.access.<mode>` 这类常量(方案初稿的写法) —— 规范要求 audience 就是资源标识,而且这样 Phase 0 发现的"`MatchesResource` 只容忍尾部斜杠"才有意义 |
| `scope` | `metaxisdata.mcp.read`(签发时由调用方传入) | RS 中间件 `RequireBearerTokenOptions.Scopes` 校验**令牌自带的 scope**(`TokenInfo.Scopes` 来自 `AccessTokenIdentity.Scopes`);不足 → 403 `insufficient_scope` |
| `iss` | 保持 JWT 现有常量 `"metaxisdata"` | 注意与 AS metadata / RFC 9207 授权响应的 `iss`(= external_url)不是一回事 |
| `sub` | 现有身份(`Subject = strconv.Itoa(userID)`) | `TokenInfo.UserID` |
| `exp` | `GetTokenDuration`(7 天) | 中间件校验 + 小 `ClockSkew` |
| `rst` | 不用 | 保留给改密/重置类受限令牌,不与 MCP 混用 |

**MCP 令牌校验必须复用现有身份解析链**:吊销 LRU(`TokenExpireCache`)、`GetUserByID`、`MemberDeleted`、改密截断(`auth.go:222` `authenticateConnect`)。建议把这条链抽成 RPC 与 MCP 共用的函数(输入 token + 期望 audience,输出 `*store.UserMessage`),否则会出现"网页上已停用/已改密的用户,MCP 令牌仍可用"的漏洞。

### Pending 授权请求与授权码

进程内 store(`backend/component/state`,照 device login 的写法:互斥锁 + TTL + 容量 + 惰性过期;在 `state.New()` 里构造并挂到 `State`)。一条记录同时承载"待批准请求"与"已签发授权码",状态机 `PENDING → APPROVED → COMPLETED` / `DENIED`:

- `request_id`(不透明;SPA 与审计里只出现它;`complete` 校验会话用户 == `approved_by_user_id`)
- `client_id`、`redirect_uri`(与注册值精确一致,loopback 允许端口不同)
- `code_challenge` + `method=S256`(强制)
- `resource`(必须等于 MCP resource)
- `approved_by_user_id` + 批准时间、`scope`
- `code`(在 `complete` 时生成,一次性,TTL 60 秒,**只经 302 回给客户端**)

换令牌时逐项复核并**立刻删除**;重复使用按 RFC 6749 视为攻击,直接拒绝(可选加固:同时作废该 code 已签发的令牌)。

### 客户端注册表

落库(已确认):新表 `oauth_client`(`client_id` 主键、`client_name`、`redirect_uris` JSONB、`token_endpoint_auth_method`、`created_time`、`last_used_time`)。理由:客户端注册是"哪个应用连过我的工作区"这类治理对象,重启即丢不利于排障与后续的"已连接应用"管理页。按仓库规则,一次 schema 变更 = `LATEST.sql` + 当前版本目录增量 + `backend/store` 查询三处同改。

### 确认页与批准者资格

- **批准者资格复用 device login 的判定**(`auth_service_device_login.go:235` `validateDeviceLoginApprover`):END_USER、未停用、通过工作区域名校验(`validateEmailWithDomains`,`user_service.go:577`)。与既有决定一致,**不在 consent 阶段重评密码策略**(重评会把 SSO-only 用户挡在门外);`disallow_signin` 由登录本身保证。
- **授权码不经过前端,也不进 RPC 响应**:批准 RPC 只接受一个决定,授权码由服务端在 `/oauth/authorize/complete` 生成并直接 302。好处:(a) 前端与模型可见通道里从不出现授权码;(b) 批准 RPC 可以放心开 `audit=true`,审计行只含"谁、批准了哪个 client、什么 scope"。代价是多一个端点与一次状态迁移,且 `complete` 必须校验会话用户。
- consent 页展示客户端名与版本、重定向目标(主机 + 路径)、resource、scope 可读说明、发起时间、来源 IP(`buildRequestMetadata` 的 trusted-proxy 语义);必须显式点击;**永不展示 token / code / code_verifier**。
- **`external_url` 不兜底**:device login 在缺失时会退回"正在连的服务器 + `/device`"并只打 warning(`:184-188`);OAuth **不能**这样 —— issuer/resource 必须稳定,靠请求 `Host` 兜底会让 audience 漂移并成立 mix-up 攻击。缺失时:PRM/AS metadata 不发布,`/mcp` 与 `/oauth/*` 明确失败(503 或按开关 404)。

### 安全不变量

1. **Audience 绑定**:`/mcp` 只接受 MCP audience;CLI/web 令牌在 `/mcp` 必须 401,MCP 令牌在 `/v1/*` 必须 401 —— 两条都有测试。
2. **禁止 token passthrough**:入站令牌绝不转发给上游;工具只在本进程内查 store。
3. **PKCE 强制**:只接受 `S256`,`plain` 一律拒绝。
4. **重定向 URI 精确匹配**:唯一例外是 loopback(`localhost`/`127.0.0.1`/`[::1]`)允许端口不同(RFC 8252 §7.3);禁止通配符域。
5. **`external_url` 是 issuer/resource 的唯一来源**:缺失即明确失败,**绝不**用请求 `Host`;仅允许 loopback 的 http,非 loopback 必须 https(规范要求 AS 端点 HTTPS)。
6. **`iss` 参数**:授权响应(含错误响应)都带 `iss`,metadata 声明支持(RFC 9207)。
7. **两层授权分开报错**:传输层 → HTTP 401/403 + `WWW-Authenticate`(让客户端能自动重新授权);平台层(IAM 拒绝某工具)→ `isError` + `permission_denied`(模型应当看到并转述)。
8. **匿名端点限流**:`/oauth/register` 与 `/oauth/token` 按 IP(注册后按 client)计数;`/oauth/authorize` 已要求登录态。
9. **确认必须显式**:consent 页展示客户端、重定向目标、resource、scope、时间、来源 IP;沿用现有 cookie 写入的 CSRF 保护;批准者资格同 device login;页面永不展示令牌与授权码。
10. **审计**:OAuth 事件(client 注册、授权批准/拒绝、令牌签发/撤销、权限拒绝)与**每次工具调用**都进审计;脱敏名单补 `code_verifier`、`client_secret`、`authorizationcode`。因为授权码只经 302 传递,审计行天然不含凭据 —— 这是设计选择而非运气。
11. **不跨域暴露 `/mcp`**:生产不放开 MCP 端点 CORS;仅元数据文档可以 `Access-Control-Allow-Origin: *`(SDK 默认)。v1.8.0 起必须**显式**用标准库 `http.NewCrossOriginProtection()` 包裹 `/mcp`(SDK 的对应选项已 deprecated,`nil` 等于不校验);DNS rebinding 保护由 `DisableLocalhostProtection=false`(默认)提供,保持默认。
12. **令牌撤销**:沿用现有 `Logout`/`TokenExpireCache`,MCP 令牌同样可吊销。
13. **开关关闭时不发布**:`mcp_enabled=false` 时 `/mcp`、PRM、AS metadata、`/oauth/*` 一律 404,不泄漏任何端点存在性。

---

## MCP 资源服务器与工具层

### 挂载与中间件

```
e.Any("/.well-known/oauth-protected-resource*", echo.WrapHandler(prmHandler))      // 匿名;开关关→404
e.GET("/.well-known/oauth-authorization-server",  asMetadataHandler)               // 匿名
e.GET("/oauth/authorize",         authorizeHandler)                                // 浏览器会话
e.GET("/oauth/authorize/complete", completeHandler)                                // 浏览器会话
e.POST("/oauth/token",    oauthLimiter(tokenHandler))                              // 匿名 + 限流
e.POST("/oauth/register", oauthLimiter(registerHandler))                           // 匿名 + 限流
e.Any("/mcp*", echo.WrapHandler(auth.RequireBearerToken(verifyMCPToken, opts)(mcpHandler)))
```

要点(已对过现有代码):

- **纯 HTTP 路由不经过 Connect 拦截器链**:`APIAuthInterceptor`/审计/ACL 只包在 Connect handler 上,所以这些路径"默认匿名",必须自己做认证、限流、审计、日志。现成先例是 OpenLineage:`grpc_routes.go:232-239` 的 `e.Group` + `openLineageIngestionMiddleware()`,handler 内部自校验 API key。
- 挂载位置:`configureGrpcRouters`(与 OpenLineage 同处)或 `configureEchoRouters` 里 `embedFrontend(e)`(echo_routes.go:59)之前;依赖注入照 `apiv1.NewOpenLineageHandler(stores, profile.TrustedProxies, lineageAnalyzer)` 的样子构造(store、`*iam.Manager`、`*state.State`、`profile`)。
- **全局 Echo 中间件仍生效**:`recoverMiddleware`、`BodyLimit("100M")`、可选 CORS、`csrfProtectionMiddleware`(echo_routes.go:44)。CSRF 豁免"带 `Authorization` 头或无 cookie"(`csrf.go:14-34`),所以换令牌请求天然豁免;consent 批准走 ConnectRPC(带 cookie)正常受保护。
- `RequireBearerToken`:`ResourceMetadataURL` 指向 PRM,`Scopes=[metaxisdata.mcp.read]`,设小 `ClockSkew`。
- **无状态**:`NewStreamableHTTPHandler(getServer, &StreamableHTTPOptions{Stateless: true, MaxRequestBodyBytes: …})`。无状态模式下 GET/DELETE 返回 405、`GetSessionID` 不参与,每个请求独立验令牌 —— 不需要会话存储,也不需要粘性路由(这正是 `2026-07-28` 强制要求的模式)。
- **跨域保护要自己包**:v1.8.0 起 `StreamableHTTPOptions.CrossOriginProtection` 已 deprecated,且 `nil` = **不做任何跨域校验**;官方建议用标准库中间件包裹:`protection := http.NewCrossOriginProtection(); e.Any("/mcp*", echo.WrapHandler(protection.Handler(mcpHandler)))`。DNS rebinding 是另一回事,由 `DisableLocalhostProtection`(默认 `false` = 保护开启)负责,保持默认。
- 前端开发态:`frontend/vite.config.ts` 的 dev proxy 目前只转发 `/v1` 与 `/metaxisdata.v1`,需补 `/oauth`、`/.well-known`。
- 限流照 `openLineageIngestionMiddleware()`(`openlineage_ingestion.go:28`);用 `state` 里的 limiter 时注意构造函数未导出,要在 `state.New()` 注册。

### 工具表

| 工具 | 对应能力 | 权限(必须等于 RPC 注解) | 入参与要点 |
| --- | --- | --- | --- |
| `list_instances` | `mxd instance list` | `metaxisdata.instances.list` | `page_size`(默认 25、上限 200)、`page_token`;投影行 |
| `list_databases` | `mxd database list` | `metaxisdata.databases.list` | `instance`(名字或 `instances/<id>`)、分页;投影行 |
| `search_metadata` | `mxd meta search` | `metaxisdata.databases.read` | `keyword`、`meta_type?`、`instance?`/`database?`/`schema?` 限定子树、分页;投影行(名字→句柄的入口) |
| `list_metadata` | `mxd meta list` | `metaxisdata.databases.read` | `object_ref` 作为父、`meta_type?`、分页;投影行 |
| `get_metadata` | `mxd meta get` | `metaxisdata.databases.read` | `object_ref`;完整结构(列/索引/键/分区) |
| `get_ddl` | `mxd meta ddl` | `metaxisdata.databases.read` | `object_ref`;返回 DDL 文本 |
| `analyze_sql` | `mxd lineage sql` | `metaxisdata.lineage.get` | `sql`(≤1 MiB)、`scopes`(1..10,`{instance,database,schema?}`)、`depth`(0..10,默认 0);返回按 scope 分组的 `relations` + `temporaryRelations` + `diagnostics` + 可选 `graph` |
| `get_lineage_graph` | `mxd lineage graph` | `metaxisdata.lineage.get` | `object_ref` 作为 root、`depth`(1..10,默认 3)、`direction`(up/down/both)、`column?`(服务端过滤) |
| `whoami` | `mxd auth status` | 无(`GetCurrentUser` 无注解,已白名单) | 回显身份与 `permissions`(让模型知道"我能查什么") |

**名字寻址(`object_ref`)** 是这些工具共用的输入形状:

```json
{"instance": "prod-mysql", "database": "shop", "schema": "public", "name": "orders"}
```

- 每个字段都接受**名字或资源 id**(`instances/1`);`schema` 可省(MySQL 家族的空 schema 层由服务端补);`name` 对表/视图/列/manual SQL 均可。
- 也可以直接给 `{"guid": "1;shop;;orders"}`(从别的工具结果里透传),**但文档不要求模型构造它**。
- `guid` 优先;否则拼路径;拼不出/歧义/不存在 → 结构化错误:`not_found` 或 `ambiguous`,并在 `details.candidates` 里列出候选项(实例/库/schema/表名 + guid)。
- 解析器只有一个,所有工具共用;解析结果同时驱动 scope 解析。

**工具描述的要求**(比名字重要):每条描述写清(a)它回答什么问题,(b)什么时候**不要**用它(引导到另一个工具),(c)一个简短示例。例如 `analyze_sql` 要写明"分析一段 SQL 文本的去向用这个;查某个已存在对象的上下游用 `get_lineage_graph`"。

**引导**:`ServerOptions.Instructions` 放简短的工具选择与寻址规则;完整说明作为 MCP prompt `metaxisdata_guide`(内容内嵌在 `backend/mcp/guide.md`,照 `cli/skill/SKILL.md` 的 `go:embed` 做法)。**不与 CLI 的 `SKILL.md` 共用文件**(两者接口不同),互相引用即可。

### 工具实现与横切关注点

1. 身份注入:把校验通过的 JWT 用户放进 `context`(`common.UserContextKey`),与 `auth.go:94` 一致 —— 否则权限检查不可能通过。
2. 流程:寻址解析 → 入参校验 → IAM 权限检查 → 调用抽出的 service 函数 → 投影/渲染 → 审计 → 返回 `CallToolResult`。

- **权限检查复用 IAM 引擎**:`PermissionChecker.CheckPermission(ctx, perm, user)`(`acl_interceptor.go:18`),生产实现 `(*iam.Manager).CheckPermission`(`manager.go:50`);用户从 `apiv1.GetUserFromContext`(`common.go:251`)取。package v1 的 `requirePermission`(`acl_interceptor.go:96`)未导出,工具层要么直接持有 `*iam.Manager`,要么落在能调用它的包里。
- **权限一致性守卫**:照 `TestEveryMethodIsPermissionGated`(`acl_interceptor_test.go:133`)+ `methodPermission`(:208)的骨架,遍历工具表 → 定位 RPC 全名 → 读 `E_Permission` → 断言相等且 `permission.Exist`;并断言只引用只读 RPC(`whoami`→`GetCurrentUser` 是唯一允许的无注解例外)。守卫只证明注解存在,**真正的判定必须在 handler 里调用 `CheckPermission`**。
- **审计(逐次,已确认)**:先做一次小重构 —— 把 `auditContext` :46、`marshalAuditMessage` :172、`sanitizeAuditValue` :194、`isSensitiveAuditField` :228、`mapSeverity` :317、`buildAuditStatus` :333、`buildRequestMetadata` :348 从 package `v1` 抽到共享包(例如 `backend/component/audit`),package v1 与新代码都从那里引用(不要复制)。工具审计行:`method="mcp/tools/call"` + 工具名、user、request=入参(脱敏)、response=状态/结果摘要、severity、latency、request metadata。IP 一律走 `buildRequestMetadata(header, peerAddr, trustedProxies)`,只在 `isTrustedProxy` 通过时才信 `X-Forwarded-For`。
- **渲染**:`protojson.MarshalOptions{EmitUnpopulated: false}`(**注意与 CLI 相反**)+ 紧凑输出;列表类工具用投影结构。`outputSchema` + `structuredContent` 用 MCP 原生方式(schema 说明字段,缺省即空)。注意 SDK 在结构化输出时若未显式设 `Content` 会自动补一份 JSON 文本;Phase 0 量一下上下文成本,必要时改回纯文本。
- **错误映射**:沿用 `cli/output.CodeOf` 的词表(`unauthenticated`/`not_found`/`invalid_argument`/`permission_denied`/`unavailable`/`resource_exhausted`/`timeout`)+ `details`;**不用退出码**。
- **入参校验**:JSON Schema 由 SDK 从 Go struct 推断;类型/必填类由 SDK 拒绝,业务类(scope 缺失、寻址歧义)走 `isError` 信封。

### 会话模式(无状态)

- **`2026-07-28` 只走无状态**:SDK 文档写明"流式 HTTP 传输仅在 `Stateless = true` 时接受 `2026-07-28` 的请求;该版本的请求打到非无状态 handler 会被拒绝"。所以无状态不是偏好,而是使用最新协议的前提。
- **旧版本照常工作**:SDK 按协商到的协议版本透明切换生命周期 —— `2025-11-25` 及更早走 `initialize` 握手,`2026-07-28` 走无握手、每个请求在 `_meta` 里携带协议版本与客户端能力。**同一个无状态 handler 同时服务新旧客户端**,不需要为老客户端单独部署或降级。
- **失去的**:server→client 请求(sampling、roots、进度通知)没有回程通道;`GET`/`DELETE` 返回 405;`2026-07-28` 下断流重连必须由客户端重新发起请求(不再有 resumability)。
- **没有失去确认能力**:工具级确认走 `2026-07-28` 的 **MRTR(multi round-trip requests)** —— 工具返回 `input-required` 结果(带 `InputRequests` 与不透明的 `RequestState`),客户端补齐后带 `InputResponses` 重发同一请求;SDK 的 MRTR 中间件让按 MRTR 风格写的 handler 对新旧客户端都成立。**因此"写操作必须切回有状态"是错的**:将来加写工具(ManualSQL 等)时用 MRTR,无状态不受影响。
- 结论:**不提供 `--mcp-stateless` 开关,也不提供有状态模式**(有状态会直接拒绝 `2026-07-28` 的客户端)。Phase 0 仍需用真实客户端各验一遍,重点是有客户端是否强依赖 `Mcp-Session-Id`。

---

## 设置与开关

新增 `WorkspaceProfileSetting.mcp_enabled`(store proto 字段 16,v1 proto 同名字段),涉及四处:

1. `proto/store/store/setting.proto` 加字段(JSONB + protojson,**无需 migration**);
2. `proto/v1/v1/setting_service.proto` 的工作区设置消息加字段;
3. `backend/api/v1/setting_service.go`:`convertToWorkspaceProfileSetting` 补映射,`UpdateWorkspaceProfileSetting` 的 mask 加 `case "mcp_enabled"`;
4. `frontend/src/pages/settings/GeneralSettingsPage.vue` 加开关 + 两个 locale 的 key。

服务端 gate:每次请求读 `GetWorkspaceGeneralSetting`(已有缓存),`mcp_enabled=false` 时 `/mcp`、PRM、AS metadata、`/oauth/*` 一律 404。**默认 `false`**(已确认:新攻击面由管理员显式开启)。开启但 `external_url` 未配置时,按安全不变量 5 明确失败。因为默认关闭,设置页的开关需要一句说明:开启前必须先配置 `external_url`。

---

## 前端

- 新增路由 `/oauth/consent`(`requiresAuth: true`, `layout: "default"`),结构照 `DeviceLoginPage.vue`;未登录先走 guard 并回跳(参数保留)。这是 SPA 里第一个 OAuth 界面。
- 页面:客户端名与版本、重定向目标(主机 + 路径,便于识别钓鱼)、scope 的可读说明、resource、发起时间、来源 IP、批准/拒绝;结果态(已批准/已拒绝/已过期/不存在)各有文案。批准后**不自己拼回调地址**,导航到 `/oauth/authorize/complete?request_id=…`。
- `frontend/src/api/oauth.ts`:两个 RPC 的封装,复用 `api/client.ts` 的共享 transport(`credentials: "include"`),注册方式照 `api/device-login.ts`。
- i18n `en-US`/`zh-CN` 同步加 key 并 `pnpm --dir frontend i18n:sort`;`GeneralSettingsPage.vue` 加开关与说明(需要 `external_url`)。
- 可选(后续):Settings → 已连接应用(列出 `oauth_client`,支持删除/吊销)。

---

## 实施步骤

> 每阶段独立可合入;proto 改动需与 `buf generate` 产物同 commit。

### Phase 0 — 前置与 spike(已完成)

产出:`go.mod`/`go.sum` 加入 SDK v1.8.0;`backend/mcp/doc.go` + `backend/mcp/sdk_contract_test.go`(8 个契约测试)。结论见上面"Phase 0 结论"一节。

1. ~~引入 SDK~~ 已完成:`github.com/modelcontextprotocol/go-sdk v1.8.0`(direct),`go mod tidy` 后 `go build ./...` 通过;仓库级 lint 需保持干净。
2. ~~spike 四件事~~ 已完成,并额外确认了 standalone SSE 与 audience 规范化边界。
3. ~~audience 取值验证~~ 已完成:`MatchesResource` 只归一尾部斜杠。

后续若升级 SDK,`backend/mcp/sdk_contract_test.go` 是这个设计的第一道防线:它会在协议版本表、`req.Extra.TokenInfo` 传递、403/401 挑战形态、结构化输出的线上形状变化时失败。

### Phase 1 — Proto 与设置开关

1. `proto/v1/v1/oauth_service.proto`(新):`OAuthService` 的 2 个方法;消息写清"展示字段"与"不可信字段"。
2. 工作区设置加 `mcp_enabled`(store proto + v1 proto)。
3. `buf format -w proto && buf lint proto && cd proto && buf generate`,提交三处产物。
4. 登记 `unannotatedMethods`;跑 `TestEveryMethodIsPermissionGated`。

### Phase 2 — 共享重构(进行中;行为不变)

1. ~~抽出 service 层函数~~ **取消**(详见决策表"工具实现"):核实后确认 9 个只读 handler 都不依赖拦截器 context,所以 MCP 工具改为进程内直接调用 handler 方法,不需要抽函数,也就没有漂移风险。
2. 把 audit 原语从 package `v1` 抽到共享包(例如 `backend/component/audit`),package v1 引用之;补 `code_verifier`/`client_secret`/`authorizationcode` 脱敏与测试。
3. **已完成** —— 抽公共身份解析链:新增 `backend/api/auth/authenticator.go`:`TokenAuthenticator.Resolve(ctx, token, audience)` 按同一次序执行签名/算法/issuer/audience/过期 → 吊销 LRU → 用户存在 → 未停用 → 改密截断,并返回**普通错误**(不再是 Connect 错误),让 ConnectRPC 与 MCP 各自映射;`VerifyAccessTokenFor` 支持显式 audience;`APIAuthInterceptor.authenticateConnect` 缩成 4 行包装,错误码与文案不变。新增 `authenticator_test.go` 钉住三条:audience 隔离、过期映射为"已吊销"、缺令牌。

### Phase 3 — OAuth AS(进行中)

1. **已完成** 令牌:audience 改为资源 URI 并携带 scope(`GenerateMCPAccessToken`);`AccessTokenIdentity` 暴露 `Scopes`/`ExpiresAt`;`TokenAuthenticator` 改用 `UserStore` 接口以便无库测试;`backend/mcp/auth.go` 的 `NewTokenVerifier` 按当前资源 audience 解析令牌,并把 principal 放进 `TokenInfo.Extra` 供工具层注入 ctx。
2. **已完成** 端点标识与元数据:`backend/api/oauth/resource.go`(规范化)、`metadata.go`(PRM + AS metadata 文档)、`authorize.go`(精确重定向匹配 + loopback 端口放宽、S256-only PKCE、scope 校验)。
3. **已完成** pending 授权请求/授权码 store(`backend/component/state/oauth_authorization_request.go`,12 个测试)。
4. **待做** `oauth_client` 表 + store(子代理在跑)、`/oauth/authorize`+`/oauth/authorize/complete`+`/oauth/token`+`/oauth/register` 的 HTTP 端点、`OAuthService` 的两个 consent 方法、限流与审计接线。

### Phase 4 — MCP 资源服务器

1. `backend/mcp`:工具表(进程内调用 `backend/api/v1` 的 handler 方法)、`object_ref` 解析器(含候选错误)、投影渲染器、`ServerOptions.Instructions`、`guide.md` prompt。
2. **工具用 raw handler + 只写 `StructuredContent`**(不设 `Content`);`ProtocolVersion < 2025-06-18` 时额外附一个紧凑文本副本(Phase 0 结论 #3/#6)。
3. 权限适配 + 逐次审计 + 错误信封;守卫测试(工具↔RPC 权限一致、只读子集、投影字段稳定、**线上只有一份载荷**)。
4. 挂载 `/mcp`(无状态 + `RequireBearerToken` + 标准库跨域保护包裹 + 开关 gate)。
5. 工具单测:SDK `NewInMemoryTransports` 起真 server + 真 client,配 `httptest` 假后端,覆盖正常路径、分页、寻址歧义、`scope_required`(带候选)、`permission_denied`、401/403。
6. 视客户端表现决定是否包一层补 `error="insufficient_scope"`(Phase 0 结论 #4)。

### Phase 5 — 前端

1. `/oauth/consent` + `api/oauth.ts` + 路由 + i18n;`GeneralSettingsPage.vue` 的 `mcp_enabled` 开关。
2. `biome:check` / `lint` / `i18n` / `type-check` / `test run` 全过。

### Phase 6 — 集成测试与文档

1. `backend/test/integration/runner/mcp_service_test.go`:真实 server 上跑完整 OAuth 授权码流程(SDK 的 `StreamableClientTransport` + `auth.AuthorizationCodeHandler`,consent 用管理员会话的 RPC 模拟点击),然后调工具断言;覆盖:无令牌 401、错误 audience 401、scope 不足 403、开关关闭 404、`analyze_sql` 名字寻址与多 scope 落位、分页续查。
2. `docs/security-posture.md` 增补:MCP 令牌 audience 绑定、OAuth AS 姿态(DCR 开放注册 + 人工确认、无 refresh、无 `jwks_uri`、pending/授权码进程内)、无状态会话、逐次审计的量级与开关默认值。
3. `AGENTS.md`:登记 `backend/mcp/AGENTS.md`(若有)与 Product surface 的 MCP 一节。
4. `cli/skill/SKILL.md` 与 `cli/README.md`:加一句"若环境提供 metaxisdata 的 MCP 工具,优先用工具;否则用 `mxd`",并说明两者 scope 语义不同(MCP 用名字寻址)。
5. `gofmt` + `golangci-lint run --allow-parallel-runners` 清零;`go build`;`make test-integration-smoke`。

---

## 测试与验收(DoD)

> 可照抄的模板:令牌 `backend/api/auth/auth_test.go`;store/限流 `backend/component/state/device_login_test.go` + `device_login_limiter_test.go`;RPC `backend/api/v1/auth_service_device_login_test.go`;端到端 `backend/test/integration/runner/agent_cli_service_test.go`(`TestDeviceLoginRealServerIntegration` :63 就是 authorize→approve→exchange 的骨架)。

1. **发现链路**:无令牌 `/mcp` → 401 + `WWW-Authenticate`;PRM 与 AS metadata 字段完整,`resource`/`issuer` 等于 `external_url` 派生值。
2. **授权链路**:DCR → authorize(登录态)→ consent 批准 → `/oauth/authorize/complete` 302 回 code → token(PKCE)→ 带令牌 `tools/call` 成功;拒绝 → `access_denied`;code 复用被拒;他人会话不能 `complete`。
3. **Audience 纪律**:CLI/web 令牌调 `/mcp` → 401;MCP 令牌调 `/v1/*` → 401。
4. **只读子集**:工具表 ↔ RPC 权限一致守卫通过;不含写 RPC。
5. **名字寻址**:`analyze_sql` 用 `{instance, database}` 名字即可分析;歧义/不存在时 `details.candidates` 可用;不要求模型构造 GUID。
6. **scope 语义**:缺 scope → `isError` + `code=scope_required` + 候选清单;两个 scope 各自落位、结果按 scope 分组、不跨 scope 合并。
7. **临时关系**:裸 SELECT 的 `temporaryRelations` 一次返回,**不需要第二次调用**;有真实目标时不重复。
8. **上下文预算**:列表工具默认 25 条 + `nextPageToken`;列表结果是投影(不含列/索引);未设字段不被输出(与 CLI 渲染相反,需在测试里显式钉住)。
9. **跨切面**:每次工具调用一条审计行且入参已脱敏;OAuth 事件有审计;匿名端点限流生效;权限拒绝有留痕。
10. **开关与前置**:默认 `mcp_enabled=false`,此时全部 404;开启但 `external_url` 缺失/非 HTTPS(非 loopback)时明确失败且不发布元数据。
11. **无状态与协议版本**:无 session 存储;多副本(两个实例轮询)下同一组用例通过;GET `/mcp` 返回 405;`mcp.SupportedProtocolVersions()` 含 `2026-07-28` 且该版本的请求在无状态 handler 下成功,旧版本客户端同样可用。
12. **线上形状**:工具响应只发一份 `structuredContent`(不设 `Content`);协商版本 < `2025-06-18` 时补一个紧凑文本副本。由 `backend/mcp/sdk_contract_test.go`(SDK 侧)与工具单测(业务侧)共同钉住。**已知偏差**:SDK 的 403 只有 `scope=`,没有 `error="insufficient_scope"`(Phase 0 结论 #4)。

---

## 风险、取舍与 Further Considerations

- **AS 是本方案最重的部分**,也是长期安全责任:PKCE、重定向、code 生命周期、注册策略都要有测试钉住。若部署已有支持 RFC 8707 与 audience 的外部 IdP,后续应支持"PRM 的 `authorization_servers` 指向外部 IdP",让用户二选一。
- **无状态的兼容性风险**:SDK 明确 `2026-07-28` 只在无状态模式下被接受,而旧版本走传统握手 —— 同一个无状态 handler 覆盖新旧客户端,但**没有"退回有状态"这条退路**(退回等于放弃 `2026-07-28`)。Phase 0 用新旧客户端各测一次;若某个必须支持的客户端在无状态下不工作,先用 `MCPGODEBUG=allowsessionsinstateless=1` 定位它是否依赖会话。
- **结果缓存(`Cacheable`/`ttlMs`)v1 不启用**:SDK 允许给工具响应带缓存提示,但 `cacheScope` 默认是 `public`;我们的每个读工具都受**调用者**的 IAM 约束,一旦被客户端或中间层按 public 缓存,就可能把一个用户可见的数据喂给另一个用户。要用只能是 `private` + 短 TTL,且只对与权限无关的响应开放。
- **DCR 开放注册**:任何人可注册 client,但授权必须由已登录用户显式确认,不构成越权;靠限流 + 审计 + 后续"已连接应用"管理页兜住滥用。CIMD 是 2026-07-28 的首选,但需要 AS 主动抓 `client_id` URL(SSRF 面,必须禁私网、限重定向),留后续。
- **7 天无 refresh**:好处是不引入 refresh 轮换与持久化;代价是到期后客户端重新弹浏览器(用户已登录时只需一次点击)。若要短时令牌,正确做法是加**轮换式 refresh token**(落库、一次性、检测复用),而不是缩短 access token。
- **`jwks_uri` 的规范偏差**:RFC 8414 把它列为 REQUIRED,但我们用对称密钥(HS256),且令牌消费者就是同一进程,没有第三方需要公钥。v1 在 metadata 里省略并写进 security posture;严格合规检查器可改为返回 `{"keys":[]}`。
- **`external_url` 成为硬前置**:没配就无法用 MCP。这是正确的(issuer 必须稳定),但要在部署文档与设置页说清楚,并给出可执行报错。
- **审计量级**:逐次审计只读调用会显著放大永久保留的 `audit_log`。若要控量,应做工具级摘要或采样,而不是缩短保留期(与既有姿态冲突)。
- **结构化输出的重复传输**:SDK 在结构化输出时会补一份 JSON 文本,`content` 与 `structuredContent` 可能同时到客户端。Phase 0 量成本,必要时改为纯文本或纯结构化。
- **device login 与 MCP 的打通**:headless agent 没有浏览器。仓库已有 device login,后续可暴露成 `grant_type=urn:ietf:params:oauth:grant-type:device_code`,让 `mxd auth login` 也能签发 MCP audience 的令牌。
- **老客户端与 `structuredContent`**:`structuredContent` 是 SEP-2106(`2025-06-18`)引入的,更早的客户端只读 `content`。设计上用协商版本分流(Phase 0 已证明版本可在工具里读到),代价是这条路需要在集成测试里覆盖两个分支;若实测发现没有 `2025-06-18` 之前的客户端,可以删掉兜底分支。
- **能力面继续演进**:`get_metadata` 对宽表、`get_lineage_graph` 对大图仍可能很大;后续可加"摘要视图"工具,或给工具加 `max_bytes` 截断语义。写操作(ManualSQL 等)需要工具级确认,走 `2026-07-28` 的 MRTR(无状态兼容),另立方案。

---

## Relevant Files(预计新增/改动)

| 类型 | 文件 |
| --- | --- |
| 依赖 | `go.mod` / `go.sum`(MCP Go SDK **v1.8.0**) |
| proto | `proto/v1/v1/oauth_service.proto`(新)、`proto/v1/v1/setting_service.proto`(开关)、`proto/store/store/setting.proto`(`mcp_enabled`)+ 三处生成产物 |
| OAuth AS | `backend/api/oauth/`(**已存在**:`resource.go` 规范化 issuer/resource、`metadata.go` 两份元数据文档、`authorize.go` 的 PKCE/重定向/scope 安全核心)、`backend/api/v1/oauth_service.go`(consent 的 2 个 RPC,待写)、`backend/component/state/oauth_authorization_request.go`(**已存在**)、`oauth_limiter.go`(待写)、`backend/component/state/state.go`(已注册) |
| 客户端注册表 | `backend/migrator/migration/0.1/0011##oauth_client.sql` + `LATEST.sql`(**已存在**)、`backend/store/oauth_client.go` + 测试(**已存在**) |
| 令牌 | `backend/api/auth/auth.go`(MCP audience 常量与签发入口、抽公共身份解析链)、`backend/api/auth/auth_test.go` |
| 迁移与 store | `backend/migrator/migration/LATEST.sql`、当前版本目录增量、`backend/store`(`oauth_client` 查询) |
| MCP RS | `backend/mcp/`(新:server、工具表、`object_ref` 解析、投影渲染、审计、`guide.md`)、**已存在**:`backend/mcp/doc.go`、`backend/mcp/sdk_contract_test.go`(Phase 0 契约测试)、可选 `backend/mcp/AGENTS.md` |
| 共享重构 | `backend/component/audit/`(新,audit 原语迁出 package v1)、`backend/api/v1/audit.go` + `audit_test.go`;**已完成**:`backend/api/auth/authenticator.go` + `authenticator_test.go`(身份解析链共享,`auth.go` 相应收缩) |
| 设置 | `backend/api/v1/setting_service.go`(mask + convert)、`frontend/src/pages/settings/GeneralSettingsPage.vue` |
| 路由 | `backend/server/grpc_routes.go`(照 `:232-239` 挂 `/oauth/*`、`/.well-known/*`、`/mcp`)、`frontend/vite.config.ts`(dev proxy) |
| 前端 | `frontend/src/router/index.ts`、`frontend/src/pages/OAuthConsentPage.vue`(新)、`frontend/src/api/oauth.ts`(新)、`frontend/src/api/client.ts`、`frontend/src/locales/{en-US,zh-CN}.json` |
| 测试 | `backend/api/oauth/*_test.go`、`backend/mcp/*_test.go`(权限/只读/渲染守卫)、`backend/test/integration/runner/mcp_service_test.go` |
| 文档 | `docs/security-posture.md`、`AGENTS.md`、`cli/skill/SKILL.md`、`cli/README.md` |

**不需要改**:现有数据面 RPC 的语义与权限注解、现有表结构(除新增 `oauth_client`)、`mxd` CLI 的代码(仅 skill 文档措辞)、前端既有页面(除 General 设置页加一个开关)。

---

## 下一步

**Phase 0 已完成**(结论见上;产出 `go.mod`/`go.sum` 的 SDK 依赖 + `backend/mcp/` 的契约测试,仓库级 lint 与 `go build ./...` 均通过)。剩下两条可以先并行推进:

1. **Phase 2 共享重构**(建议先做):抽 service 函数、audit 原语迁出 package `v1`、抽公共身份解析链。行为不变、风险最低,是 Phase 3/4 的前置。
2. **Phase 1 proto 与开关**:`oauth_service.proto` 的 2 个方法 + `mcp_enabled`(store proto / v1 proto / mask / 前端开关)。
3. 之后 Phase 3(OAuth AS)→ Phase 4(MCP RS)→ Phase 5(前端)→ Phase 6(集成与文档)。
