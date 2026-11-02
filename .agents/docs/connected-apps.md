# 已连接应用(MCP OAuth 授权 / CLI 登录)管理 — Reference

> Status: **implemented**(`ConnectedAppService`、migration `0021` 与 `0040`、管理员页与自助页均已落地)。Maintenance reference for `backend/api/v1/connected_app_service.go`、`backend/store/oauth_grant.go`、`backend/api/oauth/token.go` 与 `cli.go` 的轮换路径、`backend/api/auth/authenticator.go` 的授权存活检查,以及 `backend/mcp/` 的工具活动路径。
> 用户文档 [docs/mcp.md](../../docs/mcp.md);已接受的取舍 [docs/security-posture.md](../../docs/security-posture.md);同族参考 [mcp-server.md](mcp-server.md)(端点/令牌)、[refresh-tokens.md](refresh-tokens.md)(轮换存储)、[iam-and-permissions.md](iam-and-permissions.md)(目录与白名单)。

## What it is

一次授权就是一条**已连接应用**记录:一个用户 × 一个客户端 × 一次同意,落在 `oauth_refresh_token` 行(历史表名;Go 类型 `store.OAuthGrant`)。有两种签发仪式,共用同一行结构与同一条撤销路径,由 `kind` 区分:

- **MCP 客户端**:浏览器 OAuth 授权(authorization code + PKCE),`kind='mcp'`,access token 的 audience 是 MCP resource。
- **`mxd` CLI**:device login,`kind='cli'`,access token 的 audience 是用户 API。

`ConnectedAppService` 把这份**已经存在的数据**列出来、派生状态并逐条撤销;它不管理浏览器登录会话(`web_refresh_token` 是另一套),也不管理 MCP 传输会话——`/mcp` 是无状态的,能持久、能撤销的只有授权。CLI 会话之所以也在这里,是因为用户需要能看到「我的账号在哪台机器上登录过 CLI」并能单独吊销它。

```
用户 ──浏览器同意──▶ OAuth client(DCR) ─┐
                                          ├─签发──▶ grant 行(id 稳定, kind=mcp|cli)
用户 ──device 批准──▶ 内置 client `mxd` ──┘             │  gid claim
                                                        ▼
                            MCP access token ──▶ /mcp 每请求校验授权存活
                            CLI access token ──▶ 用户 API 每请求校验授权存活
grant 行:created_at(授权时间)/expires_at(滑动 30 天)/
          last_tool_call_at(MCP 工具调用尝试)/
          client_version+request_ip(CLI 设备,仅展示)/
          revoked_at+revoked_by(软撤销)
```

## Decisions

| # | 决策 | 一行理由 |
| --- | --- | --- |
| D1 | 范围是 MCP OAuth 授权 + CLI device login 会话 | 两者本来就是同一种东西(一次授权、一条轮换 grant);web session 仍是另一套模型(`web_refresh_token`),不混入本管理面 |
| D2 | 两个受众:管理员看全工作区,普通用户只看/撤销自己的 | 单租户已有姿态;不做资源级 IAM,用 handler 收窄 + 两条 `connectedApps.*` 权限表达放宽 |
| D3 | 撤销对**已签发** access token 生效(本副本立即,其他副本 ≤30s) | access token 带 `gid`,共享校验链查授权存活;升级时拒绝无 `gid` 的旧令牌,一次 refresh 即可恢复 |
| D4 | `last_tool_call_at` 只记录**工具调用尝试**,签发/刷新不写 | 「应用还在用吗」与「刚换了令牌」是两件事;条件 UPDATE 每授权每 5 分钟最多写一次 |
| D5 | 实体名 Connected app / 已连接应用 | proto `metaxisdata/ConnectedApp`、`connectedApps/{id}`、权限 `connectedApps.list`/`.revoke` |
| D6 | 不暴露成 MCP 工具,也不加 CLI 命令 | 管理动作是人做的;MCP 能力面保持只读(工具清单见 [mcp-server.md](mcp-server.md)) |
| D7 | 行保留到 `expires_at` 之后 7 天再清理 | 状态与「谁撤销的」可见;永久历史在审计账本 |
| D8 | 一条记录 = 一次授权,逐条撤销,不按用户/客户端批量 | 同一 (user, client) 可以有多条授权;「按连接」而非「按应用」 |
| D9 | 复用授权行 + 原地轮换,按**一次性切换**发布 | 不需要回填映射;代价是不支持滚动升级(旧副本会 DELETE+INSERT 并复活已撤销行) |
| D10 | CLI 登录复用同一张 grant 表、同一套轮换,`kind` 区分 | 用户能看见并吊销 CLI 会话;不新增第二套表/轮换逻辑。CLI 的 grant `resource`/`scope` 为空(面向用户 API,不绑 MCP endpoint),刷新走 `AuthService.ExchangeRefreshToken` 而不是 `/oauth/token`(后者被 `mcp_enabled`/`external_url` 门控,CLI 必须在任何工作区可用) |
| D11 | CLI 的 `Logout` 只撤销**自己这条** grant;MCP 的 `Logout` 撤销该 client 的全部 | 一条 CLI 记录 = 一台机器,登出一台不该把其他机器一起登出;一条 MCP 记录 = 一个客户端实例,撤销任一 token 都要断掉整条连接 |
| D12 | `client_version`/`request_ip` 只作展示,不入信任链 | 「在哪台机器登录的」需要可辨认;它们是客户端/服务端自报的元数据,不参与授权判断 |

## Core mechanics

### 授权与轮换

- **首发**:`exchangeAuthorizationCode` 先 `CreateOAuthGrant`(`INSERT ... RETURNING id`),再用该 id 签 access token(`writeGrantResponse`);行在插入时 `last_tool_call_at`/`revoked_at` 均为 NULL,`created_at` = 首次签发时间(≈授权完成,不是 consent 批准时刻;两者最多差授权码 TTL)。
- **刷新**:全部可从行判断的检查先跑(客户端绑定、行存在、过期、`revoked_at`、resource、scope),然后单条原子语句放行(`store/oauth_grant.go` 的 `rotateOAuthGrantQuery`):

  ```sql
  UPDATE oauth_refresh_token
     SET token_hash = $3, expires_at = $4
   WHERE token_hash = $1 AND client_id = $2
     AND revoked_at IS NULL AND expires_at > $5
  RETURNING id
  ```

  0 行 = 并发输了/已撤销/已过期/换错客户端 → `invalid_grant`。id 不变,所以 `gid` 与资源名跨轮换稳定。过期**不再删行**(留给 maintenance,页面可显示 EXPIRED);轮换后 principal 复核失败(用户不存在、已停用、早于上次改密)由 `revokeUnusableGrant`(`backend/api/oauth/grant.go`)显式置撤销,不再靠删除行隐式结束。
- **并发正确性**:本仓库使用 Postgres 默认 READ COMMITTED,后到的 UPDATE 等锁后重新评估 WHERE,先到者已改 `token_hash` 或写入 `revoked_at`,故恰好一个赢家;撤销与轮换在同一行上串行化。集成用例覆盖并发刷新与「撤销后 `/mcp` 401」;没有单独的「撤销 vs 轮换」竞态用例。

### CLI 会话

- **首发**:`ExchangeDeviceLogin` 的批准分支先 `oauth.EnsureCLIClient`(幂等插入内置 client 行 `mxd`,只为列表能显示名字),再 `oauth.CreateCLIGrant` 建 `kind='cli'` 行(空 `resource`/`scope`,`client_version`/`request_ip` 来自 device login),然后用该 id 签 **用户 API audience** 的 access token;响应同时返回 access token 和 refresh token(`refresh_expires_in`)。行与 access token 的 id 就是列表里的 `connectedApps/{id}`。
- **刷新**:`AuthService.ExchangeRefreshToken`(body 里的 refresh token,匿名,复用 Login 预算)调用 `oauth.RotateCLIGrant`:行必须存在、`kind='cli'`、未过期、未撤销,然后走与 MCP 完全相同的原子轮换与 principal 复核(`rotateGrantToken` / `recheckGrantPrincipal` / `revokeUnusableGrant`,`backend/api/oauth/grant.go`);过期时间从「现在」重算,于是会话滑动。
- 两个 audience 把表面隔开:CLI token 带用户 API audience,`/mcp` 拒绝;MCP token 带 MCP audience,用户 API 拒绝。二者都带 `cid`/`gid`,所以撤销连接对**已签发**令牌同样生效。

### 授权存活检查(gid + 30s 缓存)

- access token 新增 `gid` claim(int64,`omitempty`;`backend/api/auth/auth.go`),`AccessTokenIdentity.GrantID` 携带它;MCP 与 CLI 令牌都带,web/API 令牌为 0。
- `TokenAuthenticator.Resolve` 在 jti 吊销检查之后、用户读取之前,仅当 `GrantID != 0` 时检查(`authenticator.go` 的 `grantRevoked` 之前):先查 `state.GrantRevocationCache`(30s TTL,**独立实例**,避免 jti 流量把它挤掉),未命中再查 `IsOAuthGrantLive`(`oauth_grant.go`)——查询绑定令牌自带的 `id + user_id + client_id` 且 `revoked_at IS NULL AND expires_at > NOW()`;**行不存在 = 不存活**,DB 读失败按失败关闭处理。
- **`/mcp` verifier 拒绝没有 `gid` 的 MCP access token**(`mcp/auth.go:60`)——升级前签发的令牌无法对应授权,拒绝后客户端 refresh 一次即得带 `gid` 的新令牌;refresh 也过期才重新走浏览器。
- verifier 把 grant id 放进 `TokenInfo.Extra` 的 `"metaxisdata.grant"`,工具路径据此记活动。

### 撤销与 Logout

- `RevokeOAuthGrant`:`UPDATE ... SET revoked_at=$2, revoked_by=$3 WHERE id=$1 AND revoked_at IS NULL RETURNING id`。0 行且行存在 = 幂等成功,保留第一次的 `revoked_by`;行不存在 = `NotFound`;无权限撤销他人 = `NotFound`(不暴露存在性)。
- `revoked_by` 记录操作者(自助撤销记本人,Logout 记令牌所属用户);**NULL 只出现在系统路径**(refresh 时 principal 复核失败)。
- 本副本在事务提交后调用 `GrantRevocationCache.Revoke("id")`,其他副本最迟一个 TTL 读到;`token_hash` 不清理,撤销后它换不出任何东西。
- `Logout` 按令牌自带的 claim 决定范围:**MCP 令牌**(`cid` 是注册)撤销该用户该 client 的**全部**授权;CLI 令牌(`cid` 是内置 `mxd`、`gid` 指向一次登录)只撤销**自己这条** grant,并标记缓存(`auth_service.go` 的 Logout 分支)。web 会话仍删除自己的 refresh 行。撤销只作用于已签发授权,不清理进程内 pending 授权请求——用户重新同意会产生新行、新 id,这是预期语义。

### 工具活动时间

- `mcp/server.go` 的 `dispatch` 在调用工具**之前**调用 `touchConnectedApp`(被权限/预算拒绝的调用也算一次尝试),`TouchOAuthGrant` 是单条条件 UPDATE:`last_tool_call_at = now WHERE id=$1 AND revoked_at IS NULL AND (last_tool_call_at IS NULL OR last_tool_call_at < now-5m)`;节流窗口内 0 行是正常 no-op,绝不无条件写。签发/刷新不更新它。

### 列表、筛选与状态

- `ListConnectedApps`(`connected_app_service.go:46`):默认 `page_size=50`(上限 1000,`page_token` 沿用发放时的页大小),按 `created_at DESC, id DESC`;`user` 决定范围,`filter` 只在其内收窄。
- 鉴权矩阵(handler 侧,`connected_app_test.go` 钉住):

  | 调用 | 结果 |
  | --- | --- |
  | 有 `connectedApps.list`、未传 `user` | 全工作区 |
  | 有权限、传 `user` | 按该用户过滤 |
  | 无权限 | 强制只查自己;传他人 `user` → `PermissionDenied` |
  | 撤销自己的行 | 允许(无需权限) |
  | 撤销他人行、有 `connectedApps.revoke` | 允许 |
  | 撤销他人行、无权限,或行不存在 | `NotFound`(统一,不做存在性探测) |

- `filter` 支持(`filter.go` 的 `parseConnectedAppFilter`):`user ==`, `state == / in`, `client`(展示名,精确/`matches`), `client_id`, `user_email`, `create_time >= / <=`;`now` 与当前 resource 以参数绑定,`state` 翻译成 `CASE`。
- 状态由服务端派生(优先级向下):

  | 状态 | 判定 |
  | --- | --- |
  | `REVOKED` | `revoked_at IS NOT NULL` |
  | `EXPIRED` | 未撤销且 `expires_at <= now` |
  | `INACTIVE` | 账号已停用(`principal.deleted`);恢复账号后连接恢复,不自动撤销 |
  | `REAUTH_REQUIRED` | (MCP)有可解析的当前 MCP resource 且行的 `resource` 不同(CLI 不受此条约束);或 `created_at` 早于用户最后改密(两类都适用) |
  | `ACTIVE` | 其余 |

  `mcp_enabled=false` 或 `external_url` 不可解析时省略 resource 子句,状态退化为持久事实,不把「无法判定」误报成异常。
- 列表行带 `kind`(`MCP`/`CLI`);CLI 行还带 `client_version`、`request_ip`,前端在应用名下标出「CLI · vX · 地址」,同名的内置 client 因此能按机器区分;CLI 行的 `last_tool_call_time` 为空(CLI 不使用工具调用),状态派生跳过 resource 子句。
- `user_email` 只在「调用者就是该用户」或持有 `users.get` 时填充;`client_name` 来自匿名 DCR 或内置 client 的固定名字,仅作纯文本展示;不返回 `revoked_by` 的 email,不返回 `redirect_uris`。

## Invariants

1. **轮换与撤销必须落在同一行并串行化**——轮换的 WHERE 必须含 `revoked_at IS NULL AND expires_at > now`,撤销必须带 `revoked_at IS NULL`;否则并发下会复活已撤销授权。
2. **`revoked_at` 是终态**——任何路径不得清空;重新授权 = 新行、新 id。`expires_at` 是滑动过期,轮换从「现在」重算;`created_at` 永不改写成轮换时刻。
3. **行 id 是稳定身份**——同时是 `connectedApps/{id}`、`gid` claim 与工具活动的外键;不得复用、重编号或在轮换时重建行。
4. **无 `gid` 的 MCP access token 一律拒绝**;授权检查只对 `GrantID != 0` 的令牌执行,查询必须绑定 `id + user_id + client_id`,行不存在视为不存活,读失败失败关闭;缓存是独立实例(与 jti 缓存共用会被令牌流量挤出)。
5. **refresh 寿命 > access token 寿命**(30 天 vs 7 天)——这是「行被清理 ⇒ 令牌本就过期」的前提;若将来把时长做成可配置,必须保留该大小关系,否则清理会提前杀死有效令牌。
6. **一条记录 = 一次授权**——同一 (user, client) 可有多行;列表不聚合,撤销一条不影响同客户端的另一条;MCP `Logout` 是唯一的「按 (user, client) 全断」路径,CLI `Logout` 只断自己那条。
7. **混合受众 RPC 先收窄再放宽**——list/revoke 无 permission 注解、在 `unannotatedMethods` 白名单;handler 必须先限定调用者可看的行,再按权限放宽;checker 报错必须失败关闭。`TestEveryMethodIsPermissionGated` 只看白名单成员,矩阵测试 `connected_app_test.go` 才是守卫。
8. **审计**:撤销带 `audit=true`(响应是撤销后的快照,行被清理后账本仍自解释),并因此自动进入 principal 预算;列表不审计、不受预算。工具调用账本不含 client/grant 归因(见下)。
9. **迁移与发布**:新装只跑 `LATEST.sql`(最终状态),存量依次跑 `0021` 与 `0040`;`0021` 的一次性切换约束仍在(旧副本 DELETE+INSERT 会丢 `revoked_at`、复活授权);`0040` 是纯增列(`kind` 默认 `mcp`,`client_version`/`request_ip` 默认空),旧副本忽略新列、新副本把旧行读成 `mcp`,因此这一步本身滚动安全。
10. **管理面不受 `mcp_enabled` 控制**——关闭表面后它仍是清理存量授权的入口。

## Failure modes

| 场景 | 行为 |
| --- | --- |
| `name` 前缀/ID 非法、`user` 非 `users/{id}`、`page_token` 非法 | `InvalidArgument` |
| 无权限却点名他人 `user` | `PermissionDenied` |
| 行不存在 / 无权限撤销他人 | `NotFound`(统一) |
| 已撤销后再撤销 | 幂等成功,返回当前快照,审计照记,保留首个 `revoked_by` |
| `list`/`revoke` 的 DB 或 checker 失败 | `Internal`(checker 失败不放宽范围) |
| 已撤销授权的 access token | `/mcp` 401 + `error="invalid_token"`(执行撤销的副本立即,其他 ≤30s) |
| 已撤销/过期/重放/跨 client/换 resource 的 MCP refresh | `invalid_grant`;过期行保留为 `EXPIRED` 等 maintenance 清理 |
| 已撤销/过期/重放/kind 不符的 CLI refresh | `Unauthenticated`;CLI 退出码 2,提示重新 `mxd auth login` |
| 从 Connected apps 撤销一条 CLI 会话 | 该机器的 access token 立即(本副本)或 ≤30s(其他副本)被拒;refresh 链换不出新令牌;其他机器的 CLI 会话不受影响 |
| 升级前签发、无 `gid` 的 MCP token | 401;客户端 refresh 一次恢复(refresh 也过期才重新授权) |
| 账号停用 | 状态 `INACTIVE`;access/refresh 由既有校验链拒绝,恢复账号后连接恢复,不自动撤销 |
| `mcp_enabled=false` / `external_url` 缺失 | 管理面照常可读可撤销;状态退化为持久事实,不判定 `REAUTH_REQUIRED`(改密导致的仍可判定) |
| 撤销成功但缓存标记失败 | 本副本最迟一个 TTL 后生效;DB 是权威,不误放行 |
| `last_tool_call_at` 写入失败或被 5 分钟窗口节流 | 工具调用照常执行(写入是 best-effort);列表该列可能滞后至多一个窗口,不构成一致性事实 |

## 已知残余(接受 / 跟进)

| 残余 | 状态与去向 |
| --- | --- |
| `oauth_refresh_token.client_id` 无外键;若将来接上 `DeleteOAuthClient` 会留下孤儿授权行 | 开放工程债,记在 [backend-open-items.md](backend-open-items.md) |
| 撤销不清理进程内 pending 授权请求 | 已接受:用户重新同意即产生新连接;pending 本就是进程内、需本人批准 |
| `Logout` 的审计行没有目标 client;MCP 工具调用账本只有用户、没有 client/grant | 开放归因债,记在 [backend-open-items.md](backend-open-items.md) |
| `oauth_client.last_used_at` 仍只写不读;`ListOAuthClients`/`DeleteOAuthClient` 零调用方;注册无总量上限 | 仍开放,见 [security-open-items.md](security-open-items.md) D11(Connected apps 管的是授权,不是注册) |
| 没有「撤销 vs 轮换」并发竞态用例 | 集成已分别覆盖并发刷新(恰好一个赢家)与撤销(撤销后 401);竞态用例属可选补强 |
| 授权行保留到 `expires_at` 后 7 天 | 已接受:撤销行的 `expires_at` 停在最后一次轮换 +30 天,所以撤销后可存在约 37 天;永久历史在审计账本 |
| 重新 `mxd auth login` 不撤销上一次 CLI 会话 | 已接受:旧行仍是 ACTIVE 直到过期,但它是可见、可撤销的;登录动作不替用户决定要不要断掉另一台机器。用户可在本页撤销 |
| 混合版本窗口(旧副本未升级) | 旧副本的 `Logout` 没有 CLI 分支,会用 `RevokeOAuthGrantsByUserAndClient(user, "mxd")` 撤销该用户**全部** CLI 会话;`ExchangeRefreshToken` 返回 Unimplemented;列表把 CLI 行渲染成 `REAUTH_REQUIRED`/「无调用」。部署按单副本(见根 `AGENTS.md`)或先停旧副本再升级 |

## Where things live

- Schema: `backend/migrator/migration/0.1/0014##refresh_tokens.sql`(建表)、`0023##connected_apps.sql`(三列 + `(user_id, created_at DESC)` 索引)、`0040##cli_sessions.sql`(`kind`/`client_version`/`request_ip`)、`LATEST.sql`(最终状态);表名仍是 `oauth_refresh_token`。
- Store:`backend/store/oauth_grant.go`(`Create/Get/Rotate/Revoke/RevokeByUserAndClient/IsLive/Touch/GetConnectedApp/ListConnectedApps/DeleteExpired`);形状守卫 `backend/store/oauth_grant_test.go`。
- 服务:`backend/api/v1/connected_app_service.go`;筛选 `backend/api/v1/filter.go` 的 `parseConnectedAppFilter`;proto `proto/v1/v1/connected_app_service.proto`(生成物随 `buf generate` 提交)。
- 令牌与校验:`backend/api/auth/auth.go`(`gid`/`GrantID`)、`backend/api/auth/mcp_token.go`、`backend/api/auth/authenticator.go`(`GrantStore`、`grantRevoked`、`ErrGrantRevoked`)、`backend/component/state/state.go`(`GrantRevocationCache`)、`backend/mcp/auth.go`(拒绝无 `gid`、`TokenInfo.Extra` 携带 grant)、`backend/api/oauth/token.go`(创建/轮换/失败撤销;`ServerConfig.TokenStores` 是 hermetic 测试接缝),`backend/api/oauth/token_test.go`(过期/已撤销/换资源/scope 漂移都不烧凭据、抢占失败、principal 拒绝置撤销并清缓存、authorization_code 先建行再签令牌)。
- CLI 会话:`backend/api/oauth/cli.go`(`CLIClientID`/`EnsureCLIClient`/`CreateCLIGrant`/`RotateCLIGrant`)、`backend/api/oauth/grant.go`(与 MCP 共用的轮换/复核/撤销)、`backend/api/v1/auth_service_device_login.go`(签发)、`backend/api/v1/auth_service_exchange_refresh_token.go`(刷新)、`backend/api/auth/grant_token.go`(`GenerateCLIAccessToken`)。
- Logout 与清理:`backend/api/v1/auth_service.go`(MCP 分支撤销全部授权,CLI 分支只撤销 `gid`;均清缓存)、`backend/runner/maintenance/maintenance.go`(`connectedAppRetention = 7d`,`DeleteExpiredOAuthGrants`)。
- 工具活动:`backend/mcp/server.go` 的 `dispatch`/`touchConnectedApp`/`lastToolCallThrottle`。
- 权限:目录 `backend/common/permission/permission.json`(73 条,`connectedApps.*` 不在 workspaceMember 的 17 条基线里,`workspaceAdmin` 自动拥有);白名单 `backend/api/v1/acl_interceptor_test.go`;矩阵测试 `backend/api/v1/connected_app_test.go`。
- 前端:路由 `frontend/src/router/index.ts`(管理员 `/settings/connected-apps`,自助 `/connected-apps`);页面 `frontend/src/pages/settings/ConnectedAppsSettingsPage.vue`、`frontend/src/pages/ConnectedAppsPage.vue`;共享组件 `frontend/src/components/connected-app/`;API `frontend/src/api/connected-app.ts`;入口 SettingsNav / UserMenu;权限镜像 `frontend/src/lib/permissions.ts`;i18n/词汇表见 `frontend/glossary.json`。
- 集成:`backend/test/integration/runner/mcp_service_test.go` 覆盖「授权 → 列表可见 → 撤销 → `/mcp` 立即 401 → refresh `invalid_grant` → 重复撤销幂等」与「6 个并发 refresh 恰好一个赢家」;`agent_cli_service_test.go` 覆盖「device login → 列表可见(CLI/设备信息)→ 撤销 → 用户 API 401 + refresh 拒绝」与「轮换/重放拒绝/会话滑动/按机器登出」;迁移链由 `backend/migrator/migrator_integration_test.go` 的快照测试守住。
- 门禁:`gofmt`、`golangci-lint run --allow-parallel-runners`、`go test ./...`、build;`make test-integration-smoke`(迁移改动跑 `make test-integration`);前端 `biome:check`、`lint`、`i18n`、`type-check`、`test run`;proto 改动用 `buf format` / `buf lint` / `buf generate`。
- 非目标:不做 `GetConnectedApp`(列表已含全部展示字段);不做批量撤销;不管理 web 会话;不把管理动作放进 MCP 工具或 CLI(CLI 的登出走 `Logout`,只撤销自己那条)。
