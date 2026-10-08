# 服务端内置 MCP（Model Context Protocol）+ OAuth 2.1 授权 — Reference

> Status: **implemented**。Maintenance reference for `backend/mcp/`、`backend/api/oauth/`、`backend/api/v1/oauth_service.go` 与 `/mcp` 的挂载点。
> Related: 使用者接入指南 [docs/mcp.md](docs/mcp.md)（不要重复它）;刻意决策 [docs/security-posture.md](docs/security-posture.md);目录内规则 `backend/mcp/AGENTS.md`。
> SDK: `github.com/modelcontextprotocol/go-sdk v1.8.0`（`go.mod`）。

## What it is

同一进程既是 MCP 资源服务器（RS）又是 OAuth 2.1 授权服务器（AS）。RS 是 `/mcp` 上的 **Streamable HTTP、无状态**端点，暴露 9 个只读工具；**数据面零新增 RPC** —— 工具在进程内直接调用现有 ConnectRPC handler 方法。AS 提供 PRM（RFC 9728）、AS metadata（RFC 8414）、Authorization Code + PKCE(S256)、RFC 8707 `resource`、DCR（RFC 7591）。整个面由工作区开关 `mcp_enabled`（默认 `false`）控制。

调用链：无令牌 `/mcp` → 401 + `WWW-Authenticate: Bearer resource_metadata=…` → PRM → AS metadata → (DCR) → 带 PKCE+`resource` 打开浏览器 → 已登录用户在 `/oauth/consent` 确认 → `/oauth/authorize/complete` 生成授权码并 302 回客户端 → `code_verifier`+`resource` 换令牌 → 带令牌请求 `/mcp`，每请求独立验令牌 → 工具（寻址 → IAM → 调 handler → 渲染 → 审计）。

## Decisions

| 决策 | 选择 | 为什么 |
| --- | --- | --- |
| 交付形态 | 服务端内置，挂 `/mcp` | 工具直接复用 store/service，无客户端装配 |
| 传输 | 仅 Streamable HTTP | 服务端无 stdio 语义 |
| 会话 | **无状态**（`Stateless: true`, `JSONResponse: true`） | 协议 2026-07-28 只在无状态下被接受；无会话存储、无可劫持会话、天然多副本。代价：无 server→client 请求；工具级确认改用 MRTR |
| AS 归属 | metaxisdata 自身同时是 AS 与 RS | 自托管零外部依赖；SSO 是可选功能，外部 IdP 普遍不支持 RFC 8707 |
| 客户端注册 | DCR（RFC 7591）public client（`none`） | 主流客户端仍走 DCR；CIMD 需 AS 抓 URL（SSRF 面），留后续 |
| 令牌 | 复用 JWT + MCP audience（资源 URI）+ `scope`；**另发轮换式 refresh token** | 原决策「7 天、不发 refresh token」已被推翻；见下 |
| OAuth scope | 单一粗粒度 `metaxisdata.mcp.read` | 真正的授权仍是平台 IAM；把 IAM 权限串当 scope 会造出第二套 ACL |
| 开关 | `WorkspaceProfileSetting.mcp_enabled`，默认 `false` | 新攻击面由管理员显式开启；JSONB 加字段无需 migration |
| Audience 纪律 | `/mcp` 只认 `<external_url 规范化>/mcp`；Connect 侧沿用 `mt.user.access.<mode>` | 规范 MUST audience 绑定、MUST NOT passthrough；双向隔离由 `backend/api/auth/authenticator_test.go` 钉住 |
| 工具实现 | 进程内直接调用现有 handler 方法（`connect.NewRequest` 传参），不抽 service 函数 | 这 9 个只读 handler 不依赖拦截器 context，因此同一份代码、零漂移；代价是 MCP 层自己放用户进 ctx、自己做权限与审计 |
| 权限一致性 | 守卫测试：工具权限串 == 对应 RPC 注解值，且只引用只读权限 | `backend/mcp/tool_guard_test.go` |
| 输出契约 | 与 CLI 共享语义（字段名、enum 名、错误码词表），**不共享渲染选项** | 省略空字段 + 紧凑 JSON + 投影 |
| 审计 | 每次工具调用一条审计行（**含被拒的调用**），OAuth 事件同样 | `audit_log` 永久保留，量会上升 |
| 能力面 | 只读 9 个工具 | 写操作需要确认语义，走 MRTR，另立方案 |

**refresh token（推翻决策 #4 的原因与形状）**：`backend/api/oauth/token.go:18` 起 token 端点支持 `refresh_token` grant；access token 仍用 `auth.GetTokenDuration`（7 天），refresh token 是**不透明、单次使用、每次轮换**的凭据，只存 SHA-256 摘要，落库 `oauth_refresh_token`（`backend/migrator/migration/0.1/0014##refresh_tokens.sql`，`backend/store/oauth_refresh_token.go`）。grant 钉在同意时的 resource 与 scope 上，每次轮换复查 principal（存在、未停用、未早于上次改密）。`Logout` 按令牌的 `cid` claim 删除该 client 的 grant；maintenance runner 清理过期行。

## 已知偏差（原计划 → 实际，勿再当作遗漏）

| 原计划 | 实际 | 理由 |
| --- | --- | --- |
| 决策 #9：输出用 `outputSchema` + `structuredContent` | 只声明 `InputSchema`，输出只走 `structuredContent`（`backend/mcp/server.go:276-279`） | 结果里嵌的是 proto 消息的 protojson，手写 output schema 会成为第二个会漂移的真源；要做应从 proto 生成 |
| 不变量 8：注册后按 client 计数 | 只按来源地址计数（`backend/server/oauth_endpoints.go`） | 匿名注册下 `client_id` 是调用方自报的，按它分桶等于给攻击者一把新钥匙 |
| 分页返回 `hasMore` | 只回 `nextPageToken`（`backend/mcp/tool.go:217`） | token 非空即「还有」，多一个字段没有信息量 |
| consent 页展示客户端名**与版本** | 只展示客户端名（`proto/v1/v1/oauth_service.proto` 的 `client_name`） | RFC 7591 的客户端元数据没有版本字段 |
| 审计记录 response 摘要 | 只记 status（code + message，`backend/mcp/server.go:402-431`） | 行里已写明结果；把模型看到的载荷再抄进永久账本等于双写 |
| 工具调用审计只在成功路径 | 含**被拒的调用**（`dispatch` 在 `invoke` 之后无条件写行，`backend/mcp/server.go:302-311`） | 拒绝恰恰是最该留痕的 |
| MCP 令牌可吊销 | 按签发来源（签名/kid/issuer/expiry，不带 audience）校验 | 原 `Logout` 只认 user audience，MCP 令牌就没有自助补救 |
| 授权响应「含错误响应」带 `iss` | 错误路径也带（`RedirectWithError`，`backend/api/oauth/authorize.go:175`） | 拒绝路径可能是客户端唯一看到的那次响应；RFC 9207 的 mix-up 防护必须在那里成立 |

其余 review 修复已并入代码，不再是待办：`authorization_code` 脱敏补齐（含 snake_case）、工具参数按 8 KiB 单串 / 256 KiB 整体截断（参数树 >64 KiB 只记 marker）、被禁用部署的 404 不写账本、`/oauth/authorize` 与 `/oauth/authorize/complete` 纳入按地址限流、consent 拒绝后导航到 complete（客户端才收得到 `access_denied`）、`external_url` 的重复 `/` 与 `:0443` 归一化、redirect URI 拒绝 userinfo、注册时重复 `redirect_uri` 去重、`last_used_at` 在兑换成功后更新、`resolveDatabase` 拒绝跨实例 GUID、`analyze_sql` 图展开上限 20（`backend/mcp/tool.go:688`）、列表型字段空值输出 `[]` 而非 `null`、MCP 实现不广播空版本、守卫测试钉住工具集合。

## SDK 契约（实测）

`backend/mcp/sdk_contract_test.go` 是这批假设的第一道防线 —— SDK 升级时它会失败。8 个测试只碰 SDK、不碰 Backend 代码。

| 断言 | 结果 | 对实现的影响 |
| --- | --- | --- |
| `mcp.SupportedProtocolVersions()` | 含 `2026-07-28 2025-11-25 2025-06-18 2025-03-26 2024-11-05` | 无状态设计成立 |
| handler 拿到已验证身份 | 无状态 + `RequireBearerToken` 下经 `req.Extra.TokenInfo` 看到 `UserId`/`Scopes`，经 `req.Extra.Header` 看到原始头 | 身份注入、IAM、审计可行，不必自己解析 header |
| 协商后的协议版本可读 | `req.Session.InitializeParams().ProtocolVersion` 按请求填充 | 「老客户端补文本副本」是可实现分支（当前未做） |
| 缺凭据/缺 scope 的失败形态 | 401 带 `WWW-Authenticate: Bearer resource_metadata=…, scope=…`；403 同形。**SDK 不写 RFC 6750 的 `error=`** | 由 `challengeWriter` 补全（见下） |
| 一个无状态 handler 服务新旧协议 | 新版与 `2025-06-18` 都成功；裸 `initialize` 不返回 `Mcp-Session-Id` | 无需为老客户端降级部署 |
| 结构化输出的线上形状 | SDK 的 generic typed `AddTool` 会**再补一份 TextContent JSON 副本** | 工具一律 raw handler、只写 `StructuredContent`，不设 `Content` |
| standalone SSE | 客户端保持默认也能 `tools/call`（GET 405 被容忍） | 不要求客户端 `DisableStandaloneSSE` |
| `oauthex.MatchesResource` 的归一化边界 | 只容忍尾部斜杠；大小写、默认端口都不归一 | `external_url` 必须先规范化一次，PRM/JWT `aud` 用同一份结果 |

## MCP 原生设计（刻意偏离 CLI 的地方）

| 议题 | CLI | MCP |
| --- | --- | --- |
| 对象寻址 | 只接受不透明 GUID，要求「复制、不许手拼」 | **名字路径** `{instance, database?, schema?, name?}`，也接受已拿到的 `guid` |
| SQL 分析作用域 | 进程级 `METAXISDATA_SCOPES` + `--scope` | 入参 `scopes: [{instance, database, schema?}]`；缺失时返回结构化候选 |
| 缺失 scope 的失败 | `code=scope_required` + 文字 hint | 同 code，`details` 里带可选库清单（实例名+库名+guid） |
| 输出渲染 | `protojson` + `EmitUnpopulated` + 2 空格缩进 | **省略未设字段 + 紧凑 JSON**；只共享字段名风格与 enum 名 |
| 列表类结果 | 完整 `StoredMetadata` | 只回**投影行**（`guid`/`name`/`metaType`/`parentGuid`）；详情走 `get_metadata` |
| 临时关系 | 默认隐藏，`--include-temp` 打开 | **一次返回**，分成 `relations` 与 `temporaryRelations` |
| 图与列过滤 | 客户端 N+1 展开/过滤 | 服务端组合（调用方只给 `depth`）；`column` 在工具层过滤（`backend/mcp/tool.go:767` 的 `filterGraphByColumn`） |
| 分页 | 默认 1000 条，触顶给 `truncated` | `page_size`（默认 25、上限 200）+ `page_token`，响应给 `nextPageToken` |
| 错误 | `code` + 文字 hint + 退出码 | 保留 `code` 词表（去掉退出码），加结构化 `details` |
| 两层授权 | 退出码 1/2/4 | 传输层（401/403 + `WWW-Authenticate`）vs 工具层（`isError` + `permission_denied`） |
| 引导文档 | 二进制内嵌 `SKILL.md` | `ServerOptions.Instructions`（短）+ MCP prompt `metaxisdata_guide`（`backend/mcp/guide.md`）。**不与 CLI 的 SKILL.md 共用** |
| 工具命名 | 命令树 `meta search` | MCP 习惯 `search_metadata` / `analyze_sql`（`list_*`/`get_*`） |
| 作用域命名标签 | `dev=1;shop` 的 `dev` | 不存在（scope 就是库本身） |
| scope 数量上限 | 服务端 10 + 客户端分批 | 保留服务端上限，超限直接报错 |

**保留的 CLI 遗产**（只与平台有关）：错误码词表、proto 字段命名、enum 名、workspace-scoped 授权、审计姿态、`external_url` 是唯一地址来源。

## 工具表

9 个只读工具，实现在 `backend/mcp/tool.go`（表在 `toolDefinitions`，`backend/mcp/tool.go:36`）；权限必须等于 RPC 注解。

| 工具 | RPC | 权限 | 入参要点 |
| --- | --- | --- | --- |
| `list_instances` | `InstanceService.ListInstances` | `metaxisdata.instances.list` | `page_size`(25/200)、`page_token`；投影行 |
| `list_databases` | `DatabaseService.ListDatabases` | `metaxisdata.databases.list` | `instance` + 分页；投影行 |
| `search_metadata` | `DatabaseService.SearchMetadata` | `metaxisdata.databases.read` | `keyword`、`meta_type?`、`instance?`/`database?`/`schema?` 限定子树、分页 |
| `list_metadata` | `DatabaseService.ListMetadata` | `metaxisdata.databases.read` | `parent`(object_ref)、`meta_type?`、分页 |
| `get_metadata` | `DatabaseService.GetMetadata` | `metaxisdata.databases.read` | `object`(object_ref)、`meta_type?`；完整结构 |
| `get_ddl` | `DatabaseService.GetSchemaString` | `metaxisdata.databases.read` | `object`、`meta_type?` |
| `analyze_sql` | `LineageService.AnalyzeSQL` | `metaxisdata.lineage.get` | `sql`(≤1 MiB)、`scopes`(1..10)、`depth`(0..10，默认 0) |
| `get_lineage_graph` | `LineageService.GetLineageGraph` | `metaxisdata.lineage.get` | `object`、`depth`(1..10，默认 3)、`direction`(up/down/both)、`column?` |
| `whoami` | `UserService.GetCurrentUser` | 无（该 RPC 无注解，已白名单） | 回显身份与 `permissions` |

每条 `Description` 说明它回答什么问题、并指明什么时候该换另一个工具（`backend/mcp/tool.go:40` 起）；守卫测试要求非空（`backend/mcp/tool_guard_test.go:49`）。

**名字寻址（`object_ref`）** 是共用输入形状：`{"instance": "prod-mysql", "database": "shop", "schema": "public", "name": "orders"}`，每个字段接受名字或资源 id（`instances/1`）；`schema` 可省；也可直接给 `{"guid": "1;shop;;orders"}`（文档不要求模型构造）。`guid` 优先，否则拼路径；拼不出/歧义/不存在 → `not_found`/`ambiguous` 并在 `details.candidates` 列候选。解析器只有一个（`backend/mcp/ref.go`），解析结果同时驱动 scope 解析。

## 授权端点

| 端点 | 方法 | 认证 | 要点 |
| --- | --- | --- | --- |
| `/.well-known/oauth-protected-resource`（含路径插入形式） | GET | 匿名 | RFC 9728 PRM；`resource`=规范化 external_url + `/mcp`，`authorization_servers`=[issuer]，`scopes_supported`=[`metaxisdata.mcp.read`]，`bearer_methods_supported`=["header"] |
| `/.well-known/oauth-authorization-server` | GET | 匿名 | RFC 8414；`grant_types_supported`=["authorization_code","refresh_token"]，只接受 S256 与 `none`，`authorization_response_iss_parameter_supported=true`，**不发 `jwks_uri`** |
| `/oauth/authorize` | GET | 浏览器登录态 | 先验 `client_id`+`redirect_uri`（精确匹配，loopback 允许端口不同），再验 `response_type=code`/`code_challenge`+S256/`resource`；记 pending 请求并 302 到 SPA `/oauth/consent?request_id=…` |
| `/oauth/authorize/complete` | GET | 浏览器会话 | 按 `request_id` 生成授权码并 302 回 `redirect_uri`（`code`+`state`+`iss`）；拒绝路径回 `access_denied`；**只有批准者本人能完成** |
| `/oauth/token` | POST(form) | 匿名 + PKCE | `authorization_code` 与 `refresh_token` 两个 grant；`resource` 必填且必须等于本部署；错误按 RFC 6749 JSON |
| `/oauth/register` | POST(JSON) | 匿名 + 限流 | RFC 7591 DCR，只接受 public client；`redirect_uris` 必须 https 或 loopback；返回 `client_id`；无 registration access token |
| `OAuthService.GetOAuthAuthorizationRequest` | ConnectRPC | 需登录，无 permission 注解 | consent 页取详情；**响应不含 code/PKCE challenge/client state** |
| `OAuthService.ApproveOAuthAuthorizationRequest` | ConnectRPC | 需登录，无 permission 注解，`audit=true` | 只接受一个决定；批准者资格走与 device login 同一个 `validateApprover` |

两个 consent RPC 必须留在 `backend/api/v1/acl_interceptor_test.go` 的 `unannotatedMethods`，否则 `TestEveryMethodIsPermissionGated` 失败。

## 令牌与 claims

用 `generateToken` 内核，新增 `GenerateMCPAccessToken`。

| claim | 值 | 强制点 |
| --- | --- | --- |
| `aud` | MCP 资源的规范化 URI（`<external_url>/mcp`） | `/mcp` 用同一套 audience 校验；`VerifyAccessToken` 只认用户 audience，所以 MCP 令牌天然无法用于 ConnectRPC（反之亦然） |
| `scope` | 签发时传入（现为 `metaxisdata.mcp.read`） | `RequireBearerTokenOptions.Scopes` 校验**令牌自带** scope；不足 → 403 |
| `cid` | client_id | `Logout` 据此删除该 client 的 refresh token |
| `iss` | 常量 `"metaxisdata"` | 与 AS metadata / RFC 9207 授权响应的 `iss`（=external_url）不是一回事 |
| `sub` | 现有身份（`strconv.Itoa(userID)`） | `TokenInfo.UserID` |
| `exp` | `GetTokenDuration`（7 天） | 中间件校验 + 30s `ClockSkew` |
| `rst` | 不用 | 留给改密/重置类受限令牌 |

**MCP 令牌校验复用现有身份解析链**：签名/算法/issuer/audience/过期 → 吊销 → 用户存在 → 未停用 → 改密截断，实现在 `backend/api/auth/authenticator.go` 的 `TokenAuthenticator.Resolve`（RPC 与 MCP 共用）。少了这条链就会出现「网页上已停用/已改密的用户，MCP 令牌仍可用」。

## Pending 授权请求与授权码 / 客户端注册表

- **进程内**（`backend/component/state/oauth_authorization_request.go`）：一条记录同时承载待批准请求与已签发授权码，状态机 `PENDING → APPROVED → COMPLETED` / `DENIED`；`request_id` 不透明、`code` 在 complete 时生成、一次性、TTL 60 秒、**只经 302 回给客户端**；`complete` 校验会话用户 == 批准者；换令牌时逐项复核（client/redirect_uri/PKCE/resource）并**立刻删除**，重复使用视为攻击拒绝。进程内 = 多副本必须粘性路由。
- **落库**：`oauth_client`（`backend/migrator/migration/0.1/0011##oauth_client.sql`、`backend/store/oauth_client.go`）保存注册；`oauth_refresh_token`（`0014##refresh_tokens.sql`）保存 grant 轮换状态。

## 挂载与横切关注点

- 纯 HTTP 路由**不经过 Connect 拦截器链**（`APIAuthInterceptor`/审计/ACL 只包 Connect handler），所以这些路径自己做认证、限流、审计、日志。挂载点 `backend/server/grpc_routes.go:308-346`。
- 每请求读工作区设置（`oauth.WorkspaceEndpoints`），因此开关与地址变更无需重启。关闭 → 404；开启但 `external_url` 缺失/非法 → 503（`writeUnavailable`）。
- 匿名端点各自独立按来源地址限流 + 超时（`backend/server/oauth_endpoints.go`：10 rps / burst 20 / 30s）。
- `/mcp` 用标准库 `http.NewCrossOriginProtection()` 包裹，再套 `http.TimeoutHandler`（60s）。
- **挑战头由我们补全**：SDK 只在构造时取一次 `resource_metadata` URL 且不写 `error=`，所以 `challengeWriter`（`backend/mcp/server.go:229`）在 401/403 上按当前设置写 `Bearer resource_metadata="…", scope="…", error="invalid_token|insufficient_scope"`。
- **审计**：工具调用一条行（`method="mcp/tools/call:<tool>"`），身份、状态/severity、latency、request metadata（IP 只在 `isTrustedProxy` 通过时才信 `X-Forwarded-For`）；入参脱敏，参数树 >64 KiB 只记 marker。OAuth 每个协议端点一条行，code/token 永不入账本。

## Invariants

1. **Audience 绑定**：`/mcp` 只接受 MCP audience；CLI/web 令牌在 `/mcp` 必须 401，MCP 令牌在 `/v1/*` 必须 401。两条都有测试。
2. **禁止 token passthrough**：入站令牌绝不转发给上游；工具只在本进程内查 store。
3. **PKCE 强制**：只接受 `S256`，`plain` 一律拒绝。
4. **重定向 URI 精确匹配**：唯一例外是 loopback 允许端口不同；禁止通配符域、userinfo、fragment。
5. **`external_url` 是 issuer/resource 的唯一来源**：缺失即明确失败，**绝不**用请求 `Host`；仅允许 loopback 的 http，非 loopback 必须 https。
6. **`iss` 参数**：授权响应（含错误/拒绝响应）都带 RFC 9207 `iss`，metadata 声明支持。
7. **两层授权分开报错**：传输层 → HTTP 401/403 + `WWW-Authenticate`；平台层 IAM 拒绝 → `isError` + `permission_denied`。否则客户端无法触发重新授权，模型也看不到原因。
8. **匿名端点限流**：`/oauth/register`、`/oauth/token`、`/oauth/authorize`、`/oauth/authorize/complete` 各按来源地址计数（**不是**按 client —— 见已知偏差）。
9. **确认必须显式**：consent 页展示客户端名、重定向目标、resource、scope、时间、来源 IP；批准者资格同 device login；页面**永不**展示 token 与授权码。
10. **审计**：OAuth 事件与**每次工具调用**（含被拒）都进审计；脱敏名单含 `code_verifier`、`client_secret`、`authorizationcode`、`deviceCode`。授权码只经 302 传递，审计行天然不含凭据。
11. **不跨域暴露 `/mcp`**：生产不放开 MCP CORS；仅元数据文档可 `Access-Control-Allow-Origin: *`。v1.8.0 起必须**显式**用 `http.NewCrossOriginProtection()` 包裹（`nil` 等于不校验）；DNS rebinding 保护保持默认开启。
12. **令牌撤销**：MCP 令牌可按签发来源（签名/kid/issuer/expiry，不带 audience）吊销；`Logout` 同时删除该 client 的 refresh token 行。
13. **开关关闭时不发布**：`mcp_enabled=false` 时 `/mcp`、PRM、AS metadata、`/oauth/*` 一律 404，不泄漏任何端点存在性。

## Failure modes

| 场景 | 行为 |
| --- | --- |
| 无令牌 / 令牌无效、过期、被吊销 | 401 + `WWW-Authenticate: Bearer resource_metadata=…, scope=…, error="invalid_token"` |
| 令牌缺 `metaxisdata.mcp.read` | 403 + 同形挑战头，`error="insufficient_scope"` |
| `mcp_enabled=false` | `/mcp`、两份元数据、`/oauth/*` 全部 404；**不写审计行** |
| 开启但 `external_url` 缺失或有 query/fragment/userinfo、非 loopback http | 503（PRM/AS metadata 与 MCP 端点），明确失败且不发布元数据 |
| IAM 拒绝某工具 | HTTP 200，`isError=true`，`code=permission_denied`；审计行记 WARNING 级 |
| 名字歧义 / 不存在 | `isError=true`，`code=ambiguous`/`not_found`，`details.candidates` 给候选项 |
| 没有 scope | `isError=true`，`code=scope_required`，候选库清单 |
| 引擎无分析器 / 对象未同步 | `code=unsupported`（不是 `invalid_argument`，也不是瞬时故障） |
| 单请求超 60s | `{"error":"temporarily_unavailable",...}` |
| 请求体 >2 MiB | SDK/传输层拒绝 |
| 超过按 principal 的调用预算 | 工具级 `resource_exhausted`；仍写审计行 |
| 超过按来源地址的预算 | 429 + `Retry-After: 60`，发生在 bearer 校验之前 |
| 跨站 POST `/mcp` | 标准库跨域保护拒绝 |
| refresh token 复用 / 过期 / 跨 client / resource 变更 | `invalid_grant`；过期或复用时行被原子删除 |

## Open items

| 未做/未决 | 证据与位置 |
| --- | --- |
| 工具未声明 `OutputSchema`（只走 `structuredContent`） | `backend/mcp/server.go:276-279` 只设 `InputSchema`。理由：结果是 protojson，手写 schema 会成第二真源；真要做应从 proto 生成 |
| 注册后按 client 计数未做（只按 IP） | `backend/server/oauth_endpoints.go` 与 `backend/api/oauth/register.go:83-90` 都只按地址；`client_id` 是自报的，按它分桶等于给攻击者新钥匙 |
| 分页未回 `hasMore`（只回 `nextPageToken`） | `backend/mcp/tool.go:217` 起；token 非空即「还有」 |
| CIMD 未做 | 全仓库无实现；DCR 是 v1 路径 |
| 多副本未测 | `backend/component/state/oauth_authorization_request.go:105-109` 明确进程内，需要粘性路由；集成套件只跑单实例 |
| `Cacheable`/`ttlMs` 未启用 | 全仓库无实现；`cacheScope` 默认 `public`，与按调用者的 IAM 冲突 |
| 省略 `jwks_uri` | `backend/api/oauth/metadata.go:121-124`：对称密钥 HS256，消费者就是本进程；偏差已记入 security posture |
| 无「已连接应用」管理页 | `ListOAuthClients`/`DeleteOAuthClient` 零调用方；`TouchOAuthClient` 只被 token 端点调用（`backend/api/oauth/token.go:250`）。`last_used_at` 已在写，页面未做 |
| 老客户端（<`2025-06-18`）文本副本未做 | `backend/mcp/server.go:355-358` 只写 `StructuredContent`、不设 `Content`；版本可读（见 SDK 契约），真需要时是一行分支 |
| 写工具未做 | 只读 9 工具是确认过的能力面；写操作需 MRTR 语义 |
| device login 与 MCP 打通未做 | 未暴露 `device_code` grant；headless agent 仍需浏览器 |

## Where things live

- RS 与工具：`backend/mcp/server.go`（Config/Handler/dispatch/审计）、`backend/mcp/tool.go`（工具表 + 9 个 Run）、`backend/mcp/ref.go`（object_ref/scope 解析）、`backend/mcp/errors.go`（错误信封）、`backend/mcp/auth.go`（`NewTokenVerifier`）、`backend/mcp/guide.go` + `backend/mcp/guide.md`（prompt）。
- AS：`backend/api/oauth/resource.go`（规范化 issuer/resource）、`backend/api/oauth/metadata.go`（PRM/AS metadata + 开关解析）、`backend/api/oauth/authorize.go`（PKCE/重定向/scope 安全核心）、`backend/api/oauth/token.go`（两种 grant）、`backend/api/oauth/register.go`（DCR）、`backend/api/oauth/server.go`（会话/错误渲染）、`backend/api/oauth/audit.go`。
- consent RPC：`backend/api/v1/oauth_service.go`；proto `proto/v1/v1/oauth_service.proto`。
- 令牌：`backend/api/auth/mcp_token.go`、`backend/api/auth/refresh_token.go`、`backend/api/auth/authenticator.go`。
- 进程内状态：`backend/component/state/oauth_authorization_request.go`；注册表与 grant：`backend/store/oauth_client.go`、`backend/store/oauth_refresh_token.go`；迁移 `backend/migrator/migration/0.1/0011##oauth_client.sql`、`0014##refresh_tokens.sql` 与 `LATEST.sql`。
- 前置设置：`proto/store/store/setting.proto` 的 `mcp_enabled`/`external_url`、`backend/api/v1/setting_service.go`、`frontend/src/pages/settings/GeneralSettingsPage.vue`；consent 页 `frontend/src/pages/OAuthConsentPage.vue`（路由 `frontend/src/router/index.ts:278`）、`frontend/src/api/oauth.ts`。
- 门禁：`go test ./backend/mcp/... ./backend/api/oauth/...`（hermetic：`tool_test.go`、`tool_guard_test.go`、`sdk_contract_test.go`、`oauth/*_test.go`）；`make test-integration-smoke` 跑 `backend/test/integration/runner/mcp_service_test.go`（真实 server + Docker，两个入口测试 `TestMCPAuthorizationAndToolsRealServerIntegration`、`TestMCPRegistrationValidatesRealServerIntegration`）；`TestEveryMethodIsPermissionGated` 覆盖 `unannotatedMethods`。
- 使用者文档：`docs/mcp.md`。刻意决策：`docs/security-posture.md`。
