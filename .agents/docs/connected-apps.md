# 已连接应用(MCP OAuth 连接)管理 — 设计方案

> 状态:**设计稿,未实现**。本文件描述待建功能,不是已交付行为。落地后应改写成维护参考,并在 [README.md](README.md) 的索引表里加一行——现在不加。
>
> 关联:[mcp-server.md](mcp-server.md)(MCP 资源服务器、OAuth 2.1 AS、无状态传输与既有不变量)、[refresh-tokens.md](refresh-tokens.md)(轮换式 refresh token 的存储与校验)、[iam-and-permissions.md](iam-and-permissions.md)(权限目录、预定义角色、RPC 注解与 ACL 拦截器)、[frontend-ui.md](frontend-ui.md)、[i18n-glossary.md](i18n-glossary.md);刻意决策见 [security-posture.md](../../docs/security-posture.md);使用者文档最终改 [docs/mcp.md](../../docs/mcp.md)。

## 0. 结论

需求成立。今天 MCP 授权已经把「谁把本部署的 MCP 端点授权给了哪个客户端」写进了 `oauth_refresh_token`——每个用户对一个客户端的一次授权就是一行,`client_id` / `user_id` / `resource` / `scope` / `expires_at` / `created_at` 全都在。**数据已经在库里,缺的是三个动作:看见它、判断它的状态、让一条连接立刻作废。**

- **看见它**:没有查询接口,也没有页面。`ListOAuthClients` / `DeleteOAuthClient` 至今零调用方,`TouchOAuthClient` 只被 token 端点调用(`.agents/docs/mcp-server.md` Open items:「无已连接应用管理页」)。
- **判断状态**:行里没有 `revoked_at`,也没有任何「最近使用」时间;`oauth_client.last_used_at` 是**客户端级**的,同一客户端被十个用户授权也只有一个时间戳。access token 是无状态 JWT,签出去之后服务端没有记录。
- **让连接作废**:今天唯一的吊销路径是 `AuthService.Logout`,而且**必须持有那个用户的 access token**(`backend/api/v1/auth_service.go:293`:`GetTokenFromHeaders` → `VerifyAccessTokenProvenance`)。管理员拿不到别人的 agent 令牌,所以**管理员根本吊销不了别人的连接**;用户自己也只有在 agent 里主动 logout 才行。

方案的形状:**把刷新凭据行升级成一条有稳定身份、有状态、可吊销的「授权记录」**(表沿用历史名 `oauth_refresh_token`,对外叫已连接应用),在 MCP access token 里写入这条记录的 id,共享校验链用 30 秒缓存确认该记录未被吊销,再补一对 list/revoke 的 ConnectRPC、一个管理员页和一个用户自助入口。

三个先摆出来的取舍:

1. **「session」在这里是授权,不是传输会话。** `/mcp` 是 `Stateless: true` 的无状态端点([mcp-server.md](mcp-server.md) Decisions;`backend/mcp/AGENTS.md`),协议层没有可列举、可吊销的 session id。真正会被建立、维持、撤销的东西是 **用户 × 客户端的一次 OAuth 授权**。界面/API 用「已连接应用」,不用「会话」——词汇表已把 session 固定为浏览器登录会话(`frontend/glossary.json` 的 `session`),复用会混淆。
2. **复用现有授权行,而不是新建一张 session 表。** 现有行已经是「一条授权」的完整状态,复用它意味着**升级后存量连接立刻可见**(无需回填映射),也不会出现「刷新凭据」和「会话」两份到期时间互相漂移。代价是轮换要从 `DELETE + INSERT` 改成原地 `UPDATE`(这恰好也是让「吊销」与「轮换」线性化、不丢吊销的前提),并且要求**一次性切换、旧副本不能与新 schema 混跑**(§4.4、§8)。
3. **立即吊销靠一个 claim 加一次带缓存的存在性检查**,而不是「删掉刷新凭据、等 access token 自己过期」。后者最长会留 7 天窗口(`DefaultTokenDuration`),与「吊销」的语义不符。升级时**拒绝没有该 claim 的旧 MCP access token**,让客户端走一次 refresh 换到带 claim 的新令牌(§5.3、§5.7)。

## 1. 已对齐的决策

本轮与用户确认过的口径,以及由它推出的边界。每条都标代价。

| # | 决策 | 推论与代价 |
| --- | --- | --- |
| **D1** | **范围只做 MCP OAuth 连接**:用户在一个 MCP 客户端(agent)上完成的授权。浏览器登录会话与 CLI device-login 令牌本轮不做 | 数据模型只覆盖 `oauth_refresh_token` 这一种凭据。将来若要管 web session,`web_refresh_token` 是另一套模型,不共用本表 |
| **D2** | **两个受众**:管理员在 Settings 看全工作区并吊销任意一条;普通用户在头像菜单看/吊销自己的 | 两个页面共用一个表格组件;list 用一个 RPC,`user` 用于自助收窄,`filter` 用于管理员按用户/状态/客户端筛选(§6.4);不新增「资源级 IAM」 |
| **D3** | **吊销立即生效**:已经签发的 MCP access token 也要在 ≤30s 内被拒绝 | access token 携带授权 id + 共享校验链判断 + 30s 缓存;**升级后拒绝无 claim 的旧 MCP 令牌**(一次性 refresh 换新,§5.3),否则「立即」在最长 7 天内不成立 |
| **D4** | **记录最近一次工具调用时间**(不是「最近一次换发令牌」) | 新列 `last_tool_call_at`,只在工具调用时更新(条件 UPDATE,每授权每 5 分钟最多写一次);签发/刷新**不**更新它,避免把凭据使用与工具活动混为一谈 |
| **D5** | 实体名 **Connected app**,中文 **已连接应用** | proto 资源 `metaxisdata/ConnectedApp`,service `ConnectedAppService`;权限 `metaxisdata.connectedApps.list` / `.revoke`;词汇表加词条 |
| **D6** | **不把已连接应用暴露成 MCP 工具或 CLI 命令** | 管理动作是人做的;MCP 面保持只读 9 工具不变(`tool_guard_test.go` 的集合断言不动) |
| **D7** | 列表**保留**已吊销/已过期记录一小段时间(7 天),之后由 maintenance 清理 | 「状态」才有东西可看;代价是表里会有短期墓碑行。审计账本永久保留,长期历史去那里查 |
| **D8** | **一条记录 = 一次授权**;吊销逐条进行,本轮不做批量、不按用户/客户端一键全断 | 同一 (user, client) 可以有多条(见事实 1),列表按行展示、不聚合;吊销一条不会结束同客户端的另一条授权(这正是「按连接」而不是「按应用」的语义) |
| **D9** | **授权行原地轮换**(复用 `oauth_refresh_token` 行,Go 侧改用 grant 语义,§4.1),按**一次性切换**发布 | 若部署必须滚动升级/多副本不停机,改选独立 `connected_app` 表方案(§4.4);部署形态已确认为一次性切换(§12) |

## 2. 读码得出的事实

### 事实 1:授权状态已经落库,但行身份在轮换时会变

`backend/store/oauth_refresh_token.go` 的轮换是**整行替换**:

- `consumeOAuthRefreshTokenQuery` = `DELETE ... WHERE token_hash = $1 AND client_id = $2 RETURNING token_hash`;
- `createOAuthRefreshTokenQuery` = `INSERT ...`,其中 `created_at` 绑定的是 grant 的 `IssuedAt`。

因此:

- `created_at` 是**首次签发 access token 的时间**(授权码兑换完成时,`backend/api/oauth/token.go:140` 的 `s.now()`),不是 consent 批准的时刻;两者最多差 60 秒(authorization code TTL)。轮换把它原样带下去,所以可以把它展示为「授权时间」。
- `id` 每次刷新都会变。今天可以把行列出来,却**没有稳定句柄来吊销「那一条」**:按旧 id 的吊销会落在已被删掉的行上——这是一次会丢掉的吊销。原地 `UPDATE` 让 id 稳定,吊销与轮换在同一行上串行化(§5.2)。
- **一个 (user, client) 可以有多行**:每次授权码兑换都会 INSERT 一行,旧行只在 logout/maintenance 时消失。所以「一条记录 = 一次授权」,不是「一个客户端一条」;列表按行展示(同一客户端的多次授权就是多行),不按客户端聚合。
- 守卫测试 `backend/store/refresh_token_test.go:13-32` 用子串钉住了 `DELETE ... RETURNING token_hash` 与 `WHERE token_hash/client_id`;改成 `UPDATE ... RETURNING id` 必须同步改它,不能默默漂移。

### 事实 2:access token 不可被第三方吊销

`backend/api/auth/auth.go` 的 `AccessTokenIdentity` 只有 `UserID/TokenID/.../ClientID`;令牌是本地验签的 JWT,服务端不保存已签发令牌。`Logout` 能把一个 `jti` 写进 `revoked_token`,但它的入参就是**待吊销的那个令牌本身**(`auth_service.go:294-320`)。所以管理员看到某用户挂着 10 个 agent 连接,无法让其中任何一个的 access token 立刻失效;用户换机器/丢令牌后也无法从网页端收回旧连接。

### 事实 3:每请求已经有一次用户读取,再加一次带缓存的判断是同一量级

`backend/mcp/auth.go` 的 verifier 复用 `auth.TokenAuthenticator.Resolve`,后者每请求做验签 + `revoked_token` 查询(带 30s 缓存)+ 用户查询 + 停用/改密判断(`backend/api/auth/authenticator.go:157-223`)。MCP 端点本身也按 principal 限流并逐调用审计。多一次「授权是否仍有效」的判断,只要走同样的 30s 缓存,不会改变 `/mcp` 的成本量级。

### 事实 4:自助 + 管理混合鉴权在本仓库有现成模式

- `NotificationService` 全部方法**不带 permission 注解**(进 `unannotatedMethods` 白名单),handler 用调用者 id 收窄查询(`proto/v1/v1/notification_service.proto` 的 service 注释);
- `UserService.UpdateUser` 也不带注解,handler 内部对管理员字段调 `requirePermission`(`backend/api/v1/acl_interceptor.go:96` 的注释明说这个 helper 就是给「无法用单条注解表达」的路径用的)。

所以「一个 list 方法按权限决定看全部还是只看自己」不需要发明新机制;但它必须配一份**专门的 handler 级鉴权矩阵测试**(§9),因为 `TestEveryMethodIsPermissionGated` 只检查白名单成员,看不出「handler 忘了收窄」。

### 事实 5:审计注解和「无注解」不冲突

`option (metaxisdata.v1.audit) = true` 是独立于 permission 的注解;`ApproveOAuthAuthorizationRequest` 就是「无 permission 注解 + audit=true」的先例(`proto/v1/v1/oauth_service.proto:35-41`)。审计拦截器会把**请求与响应 proto** 写进账本(`backend/api/v1/audit.go:117-159`),所以吊销返回一份快照能让永久账本自解释。带 `audit` 的方法还会自动进入 principal 预算(`principal_throttle_interceptor.go:108-124`)。

### 事实 6:维护任务已经在清理过期凭据,没有墓碑

`backend/runner/maintenance/maintenance.go:96` 调 `DeleteExpiredOAuthRefreshTokens`;行一旦过期就消失。因此「已过期」状态若要有东西可看,需要给本表单独的保留窗口(D7)。

### 事实 7:刷新凭据寿命 > access token 寿命,这是「删行安全」的隐藏前提

`DefaultRefreshTokenDuration` = 30 天(`backend/api/auth/auth.go:41`),`DefaultTokenDuration` = 7 天(`:36`),且两者目前都不可配置(`header.go:91-96`、`refresh_token.go:35-37`)。因此任何一条带 `gid` 的 access token 都会在自己的授权行被清理之前过期;「行不存在 ⇒ 令牌拒绝」不会误杀一个本应有效的令牌。这个大小关系要写进注释并用单元断言钉住(§9),否则将来把时长做成可配置时会出现「清理后令牌提前失效」。

## 3. 方案总览

```
                        ┌────────────────────────────────────────────────┐
 管理员 SPA ──────────▶ │ ConnectedAppService.List/Revoke (ConnectRPC)   │
                        │  handler 侧: 全量?只看自己? 可否吊销?          │
 用户 SPA   ──────────▶ └──────────────────┬─────────────────────────────┘
                                           │ store
                                           ▼
                                  ┌──────────────────────┐
                                  │ oauth_refresh_token  │ ← 一条 = 一次授权
                                  │ (行即授权记录)        │
                                  └───┬──────────┬───────┘
        /oauth/token 首发/原地轮换 ────┘          │ 校验链读取
        (token_hash 原地替换, id 稳定)            ▼
                                  TokenAuthenticator.Resolve
                                  → gid claim → 30s 缓存 → 授权未吊销?
                                           │
                                  MCP dispatch: 工具调用后 touch last_tool_call_at
```

三部分改动:

1. **数据**:`oauth_refresh_token` 增加 `revoked_at` / `revoked_by` / `last_tool_call_at`;轮换改为原地 `UPDATE`,行 id 从此稳定;Go 侧类型/方法改用 grant 语义(§4)。
2. **执法**:MCP access token 增加 `gid` claim;共享校验链确认授权仍有效(§5.3);刷新路径拒绝已吊销授权;`Logout` 从「删刷新凭据」改为「吊销该用户该客户端的全部授权」(§5.5)。
3. **管理面**:`ConnectedAppService`(list/revoke)+ Settings 管理员页 + 头像菜单自助页(§6、§7)。

## 4. 数据模型

### 4.1 一条授权 = 一行 `oauth_refresh_token`

**表名沿用历史名,不重命名**;Go 侧改用 grant 语义,三者对应关系固定如下,维护文档要写明:

| 层 | 名字 |
| --- | --- |
| API / UI | `ConnectedApp`(已连接应用) |
| Go store | `OAuthGrant` / `...OAuthGrant` 方法(文件 `backend/store/oauth_grant.go`,由 `oauth_refresh_token.go` 改名) |
| DB | 表 `oauth_refresh_token`(历史名;行语义已是「一次授权」,SQL 注释要说清) |

不重命名的理由:表重命名要多改主键索引、唯一索引、序列三组名字,并让 `TestMigrateSchemaLATESTMatchesTheIncrementChain` 的快照对名字敏感;它唯一的收益是命名整洁。**两种做法都要求一次性切换**(原地轮换本身旧副本不支持),而原地轮换在混合版本下若允许旧副本继续 DELETE+INSERT,会**静默丢掉 `revoked_at` 把已吊销授权复活**——所以混合版本必须被禁止;不重命名至少让旧副本还能读表(但它仍会按旧语义轮换,因此同样不支持)。既然混合版本本来就禁止,重命名省下的只是迁移面,不是可运行性,故选不重命名,用注释和文档承载语义。

**升级后存量行立刻成为「已连接应用」**,不需要任何回填映射。

### 4.2 列

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id BIGSERIAL PRIMARY KEY` | 既有 | 对外是 `connectedApps/{id}`;进入 access token 的 `gid` |
| `token_hash TEXT NOT NULL` | 既有 | 刷新凭据摘要;轮换时原地替换。唯一索引不变 |
| `client_id TEXT NOT NULL` | 既有 | DCR 注册的客户端 |
| `user_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE` | 既有 | 授权人。账号是软删,所以级联实际上不触发;停用/删除由校验链拒绝(§5.3) |
| `resource TEXT NOT NULL` | 既有 | 授权时的 `<external_url>/mcp`;外部地址变更后旧授权不可刷新(现状),状态推导会据此给出 `REAUTH_REQUIRED` |
| `scope TEXT NOT NULL DEFAULT ''` | 既有 | 同意时授予的 scope,刷新不扩权(现状) |
| `expires_at TIMESTAMPTZ NOT NULL` | 既有 | 刷新凭据到期;每次轮换从「现在」重新计算(滑动) |
| `created_at TIMESTAMPTZ NOT NULL` | 既有 | **首次签发令牌(≈授权)时间**;轮换不碰它 |
| `last_tool_call_at TIMESTAMPTZ` | **新增** | 最近一次工具调用尝试;NULL = 还没有调用过(存量行为 NULL,不回填) |
| `revoked_at TIMESTAMPTZ` | **新增** | 非 NULL 即已吊销;**任何路径都不得把它改回 NULL**(重新授权是新行、新 id) |
| `revoked_by INTEGER REFERENCES principal(id) ON DELETE SET NULL` | **新增** | 谁吊销的;NULL = 用户本人或系统。只在库里留痕,API 不返回 email(§6.6) |

**为什么单列 `last_tool_call_at` 而不是复用一个「最近活动」**:用户明确要的是「最近一次工具调用」。把签发/刷新也写进去会让「这个应用是不是还在用」与「它是不是刚刷新了令牌」混成一个数字,吊销取证时无法区分。代价是列表里「刚授权、还没调用」的显示为空,这是诚实的。

索引:

| 索引 | 用途 |
| --- | --- |
| `UNIQUE(token_hash)`(既有) | 轮换/查找 |
| `(user_id, client_id)`(既有) | `Logout` 按客户端吊销该用户全部授权 |
| `(expires_at)`(既有) | maintenance 清理 |
| `(user_id, created_at DESC)`(**新增**) | 自助列表;数据量小,也可不加 |

**不加** `client_name` 冗余列:列表 `LEFT JOIN oauth_client` 取名字。客户端注册行被删(目前 `DeleteOAuthClient` 无调用方)时名字为空,前端显示 client_id——这是诚实的降级。**不返回** `redirect_uris`:已注册列表不是用户批准的那个,而且可能是几十 KiB 的 JSON。

### 4.3 状态与保留

状态**服务端派生**(不让 SPA 猜),按优先级:

| 状态 | 判定 | 含义 |
| --- | --- | --- |
| `REVOKED` | `revoked_at IS NOT NULL` | 已吊销;access token 在下一次校验被拒,刷新返回 `invalid_grant` |
| `EXPIRED` | `revoked_at IS NULL AND expires_at <= now()` | 刷新窗口已过;access token(7 天)不会晚于授权到期(30 天滑动),可视为不可用 |
| `INACTIVE` | 账号已停用(`principal.deleted`) | 停用期间 access/refresh 都被现有校验链拒绝;恢复账号后连接恢复(与 web refresh 会话一致)。**不自动吊销**;不可逆清退用「吊销」或删号 |
| `REAUTH_REQUIRED` | `resource != 当前 MCP resource`(仅当 `mcp_enabled` 且 `external_url` 可解析),或 `created_at < 用户最后改密时间` | 刷新已被现有逻辑拒绝,必须重新授权;access token 也已在过期/改密截断中失效 |
| `ACTIVE` | 其余 | 可用 |

`mcp_enabled=false` 或 `external_url` 缺失时,无法判定 resource/改密,状态退化为持久化事实(`REVOKED`/`EXPIRED`/`ACTIVE`)——页面顶部的说明要写清「表面已关闭」,不要把「无法判定」误报成异常。

保留:maintenance 清理 `expires_at < now() - 7 天` 的行(D7),revoked 行同样按 `expires_at` 清理;**不要**在吊销时立刻删行,否则页面上只剩审计账本。

### 4.4 方案 A(原地轮换)与方案 B(独立表)

| | A:复用行 + 原地轮换(推荐) | B:新表 `connected_app` + `oauth_refresh_token.connected_app_id` |
| --- | --- | --- |
| schema | 加 3 列 + 1 索引 | 新表 + 3 列 + FK + 1:1 回填 |
| 轮换路径 | 改(唯一碰安全核心的点) | 不动 |
| 存量可见性 | 自动(行即授权) | 回填后可见 |
| 到期时间 | 单一来源 | 两处,需同事务同步,否则「列表有效、刷新失败」 |
| 吊销 vs 轮换 | 同一行串行化,不丢吊销 | 吊销写授权行,轮换写刷新行,互不阻塞;但两行状态可能短暂不一致 |
| 滚动升级 | **不可**(见 D9/§5.7) | 旧副本可运行,但旧副本轮换插入的行 `connected_app_id` 为 NULL,需要「NULL = 未关联,下一次新代码刷新时补建/关联」的兼容分支 |
| 代码量 | 小 | 多一张表、一个 FK、一段回填与 NULL 兼容 |

**推荐 A**,前提是发布可以**一次性切换**(单副本或维护窗口)。这个前提与仓库既有姿态一致:MCP 的 pending authorization 是进程内状态,部署文档要求「单副本或粘性路由」(`docs/security-posture.md`、`docs/mcp.md`)。用户已确认采用一次性切换,所以选方案 A;若将来部署形态变成多副本滚动升级,再按上表切到 B。

第三个变体(保留 DELETE+INSERT,只增加一个不可变 UUID 列)在滚动窗口/原地轮换混合下都会被旧副本的 INSERT 丢掉新列,可能**复活已吊销授权**,不采用。

## 5. 轮换、吊销与校验

### 5.1 首发(授权码兑换)

`exchangeAuthorizationCode` → `issueGrant` 的顺序改为**先落授权行、再签发 access token**(否则拿不到 `gid`):`CreateOAuthGrant`(`INSERT ... RETURNING id`),再 `GenerateMCPAccessToken(..., grantID)`。授权行在插入时 `last_tool_call_at` 为 NULL、`revoked_at` 为 NULL。`TouchOAuthClient` 不变。

### 5.2 刷新:原地轮换

`exchangeRefreshToken` 的顺序调整为:

1. 校验 `client_id` / `refresh_token` 非空;
2. 按 `(client_id, token_hash)` 读取授权;不存在 → `invalid_grant`;
3. 到期检查(不删行,交给 maintenance;返回 `invalid_grant`);`revoked_at != NULL` → `invalid_grant`;`resource` 与当前端点不一致 → `invalid_grant`;请求的 scope 与授予不一致 → `invalid_scope`;
4. **先生成新的 refresh token 明文与摘要**(轮换语句的 SET 需要它);
5. 原子轮换(单次使用闸门):

   ```sql
   UPDATE oauth_refresh_token
      SET token_hash = $3, expires_at = $4
    WHERE token_hash = $1 AND client_id = $2
      AND revoked_at IS NULL AND expires_at > $5
   RETURNING id
   ```

   0 行 → `invalid_grant`(并发刷新只有一个赢家;吊销与轮换并发时吊销优先);
6. 复核 principal(存在、未停用、未早于上次改密)。**失败时必须显式吊销该行**(`revoked_at = now()`,`revoked_by = NULL` = 系统):今天由闸门的 DELETE 隐式结束授权,原地轮换不会,否则会留下「列表有效、实际永远刷不出来」的行;
7. 用第 5 步返回的 `id` 作为 `gid` 签发 access token;`TouchOAuthClient` 不变。

关键点:

- 到期检查与 UPDATE 的 `expires_at > $5` 必须用**同一个 `now()`**;
- `created_at` 不参与 SET;`last_tool_call_at` 也不参与(见 D4);
- 今天的「过期即删行」分支(`token.go:169-178`)不再删除;行留给 maintenance,这样 EXPIRED 状态可见;
- 读取路径要么在 SQL 里带 `revoked_at IS NULL`,要么在 handler 里先判断——两者只选一处,不能都漏。

并发正确性:PostgreSQL 默认 READ COMMITTED,两个 `UPDATE` 抢同一行时后到者等锁,拿到锁后**重新评估 WHERE**;先到者已改掉 `token_hash` 或已写入 `revoked_at`,后到者匹配 0 行。这正是今天 `DELETE ... RETURNING` 提供的单次使用语义,行为等价;但仓库目前**没有**并发刷新测试(只有集成里的重放测试,`backend/test/integration/runner/mcp_service_test.go:211-243`),这次必须补:集成里用 N 个 goroutine 刷新同一个 token,断言恰好一个 200、其余 400;再加「吊销 vs 轮换」并发。守卫测试改成钉住 `UPDATE ... WHERE token_hash = $1 AND client_id = $2 AND revoked_at IS NULL ... RETURNING id`。

### 5.3 校验链:gid + 30 秒缓存

- `GenerateMCPAccessToken` 增加 grant id 参数;`claimsMessage` 增加 `gid int64`(`omitempty`);`AccessTokenIdentity` 增加 `GrantID int64`;
- 判定放在 **`TokenAuthenticator.Resolve`**(共享校验链,`authenticator.go:157-196`),紧跟 `revoked_token` 检查之后,且仅当 `identity.GrantID != 0` 时执行。`/mcp` 是唯一以 MCP audience 调 `Resolve` 的入口(`backend/mcp/auth.go:46`),把判定放在这里延续了「所有入口共用一条链」的不变量;
- `TokenAuthenticator` 增加一个 `GrantStore` 依赖(`*store.Store` 实现;测试传 fake/nil)。查询绑定令牌里的三元组,避免拿别人的 id 套用:

  ```sql
  SELECT EXISTS (
    SELECT 1 FROM oauth_refresh_token
     WHERE id = $1 AND user_id = $2 AND client_id = $3
       AND revoked_at IS NULL AND expires_at > NOW()
  )
  ```

  行不存在 = 不可用(失败关闭)。§2 事实 7 保证被清理的行不可能对应一个仍有效的令牌;
- 判定复用 `component/state` 的 `RevocationCache`(30s TTL、容量 8192、`Remember` 不降级),但**另开一个实例** `state.State.GrantRevocationCache`:token jti 那条缓存每次认证都会写,共用会被令牌流量挤掉,还会多抢一次锁;key = `strconv.FormatInt(grantID)`。本进程吊销后**提交完成再** `Revoke`,其他副本最迟一个 TTL 后读到;DB 读失败按失败关闭处理,与 `tokenRevoked` 一致;
- **`/mcp` verifier 拒绝没有 `gid` 的 MCP access token**(`sdkauth.ErrInvalidToken`)。这样「≤30s 立即吊销」在升级后对**所有**令牌成立;旧客户端收到 401 后会走 refresh,旧 refresh 行本来就能换出带 `gid` 的新令牌(不需要重新走浏览器);若 refresh 也过期了,才重新授权。这是一次性升级成本(§5.7);
- verifier 把 grant id 放进 `TokenInfo.Extra`(key `"metaxisdata.grant"`,与 user/peer 同处),并提供 `GrantIDFromRequest` 给 dispatch 使用。

### 5.4 吊销

`RevokeOAuthGrant(id, caller)`:

```sql
UPDATE oauth_refresh_token
   SET revoked_at = NOW(), revoked_by = $2
 WHERE id = $1 AND revoked_at IS NULL
RETURNING id
```

- 0 行且行存在(已吊销)→ **幂等成功**:保留第一次的 `revoked_by`(不要覆盖),返回当前快照,审计照记;
- 行不存在或不属于调用者且无吊销他人权限 → 一律 `NotFound`(见 §6.2,避免成为「这条连接是否存在」的探测口);
- 本进程缓存**提交后**标记 revoked(跨副本 ≤30s);`token_hash` 不需要删——轮换/读取/刷新都以 `revoked_at` 为准,吊销后它换不出任何东西。

### 5.5 `Logout` 的语义更新

MCP 令牌的 `Logout`(`backend/api/v1/auth_service.go:325-331`)今天删 `(user_id, client_id)` 下的刷新行。改为**吊销这些授权**(条件 UPDATE ... RETURNING id),语义不变:一个 MCP 令牌 logout,该客户端在这个用户下的全部连接结束;随后逐个清本进程缓存;`jti` 的 `RevokeToken` 路径不动;web 会话路径不动。保留「按 (user, client) 全断」而不是只断 `identity.GrantID`,与今天的语义一致。**注意**:这里的 UPDATE 与轮换的 INSERT/UPDATE 也必须串行在同一条行上(条件 UPDATE 天然满足);今天的 DELETE 会与并发轮换的 INSERT 擦肩而过,改成 UPDATE 后消失。

### 5.6 缓存一致性

| 事件 | 本进程 | 其他副本 |
| --- | --- | --- |
| 管理端/自助吊销 | 提交后立即拒绝 | ≤30s(缓存 TTL) |
| 刷新时的 principal 拒绝(置吊销) | 提交后立即拒绝 | ≤30s |
| `Logout`(MCP 令牌) | 提交后立即拒绝 | ≤30s |
| 行被 maintenance 清理 | 对应的 access token 早已过期,无实际影响 | 同左 |

**措辞纪律**:对外说「立即」时必须是「执行吊销的那个副本立即,其他副本 ≤30s」——与 `docs/security-posture.md:23` 对 `revoked_token` 的表述一致。不需要粘性路由(区别于进程内的 pending authorization)。

### 5.7 兼容窗口与升级方式

- **旧 MCP access token(无 `gid`)在升级后一律被拒**:客户端走一次 refresh 换取带 `gid` 的新 access token;refresh 凭据仍有效时用户无感,refresh 也过期时才需要重新走浏览器授权。这是让「立即吊销」不在升级后留 7 天缺口的前提(§5.3)。
- **存量刷新行**:行即授权,升级后立刻出现在列表里,`created_at` 就是当初签发的时间;`last_tool_call_at` 为 NULL(显示「暂无调用」),第一次真实调用后补齐。
- **一次性切换**:迁移跑在新版本启动时,旧副本会按旧语义 DELETE+INSERT,可能**复活已吊销授权**并丢掉 `gid`。因此发布必须是一次性切换(单副本/维护窗口),不允许滚动升级;迁移只前向,二进制回滚也要恢复数据库备份。

## 6. API 设计

### 6.1 proto

新增 `proto/v1/v1/connected_app_service.proto`(生成代码随 `buf generate` 提交):

```proto
service ConnectedAppService {
  // 管理员看全部;没有 connectedApps.list 的调用者只能看到自己的。
  // 传了 user 时,有权限的调用者按该用户过滤,无权限者只能传自己。
  rpc ListConnectedApps(ListConnectedAppsRequest) returns (ListConnectedAppsResponse) {
    option (google.api.http) = {get: "/v1/connectedApps"};
  }
  // 本人可吊销自己的;吊销他人的需要 connectedApps.revoke。返回吊销后的快照,
  // 让永久审计账本自带目标身份(行日后会被清理,账本不会)。
  rpc RevokeConnectedApp(RevokeConnectedAppRequest) returns (ConnectedApp) {
    option (google.api.http) = {
      post: "/v1/{name=connectedApps/*}:revoke"
      body: "*"
    };
    option (metaxisdata.v1.audit) = true;
  }
}

message ConnectedApp {
  option (google.api.resource) = {
    type: "metaxisdata/ConnectedApp"
    pattern: "connectedApps/{connected_app}"
  };
  string name = 1;              // connectedApps/{id}
  string client_id = 2;
  string client_name = 3;       // 注册时的自报名字,未经验证,仅作纯文本展示
  string user = 4 [(google.api.resource_reference) = {type: "metaxisdata/User"}];
  string user_email = 5;        // 仅当调用者可读用户时填充(§6.6),否则为空
  string resource = 6;
  repeated string scopes = 7;
  google.protobuf.Timestamp create_time = 8;      // 首次签发/授权时间
  google.protobuf.Timestamp last_tool_call_time = 9; // 可能为空
  google.protobuf.Timestamp expire_time = 10;
  State state = 11;
  google.protobuf.Timestamp revoke_time = 12;     // 仅 REVOKED

  enum State {
    STATE_UNSPECIFIED = 0;
    ACTIVE = 1;
    REVOKED = 2;
    EXPIRED = 3;
    REAUTH_REQUIRED = 4;
    INACTIVE = 5;
  }
}

message ListConnectedAppsRequest {
  int32 page_size = 1;
  string page_token = 2;
  // users/{id}。省略时:有 connectedApps.list 的调用者看全工作区,否则看自己。
  // 指定他人:只有 connectedApps.list 的调用者允许。
  string user = 3 [(google.api.resource_reference) = {type: "metaxisdata/User"}];
  // 筛选,语法见 §6.4。只收窄结果,不能扩大 user 已经限定的范围。
  string filter = 4;
}
message ListConnectedAppsResponse {
  repeated ConnectedApp connected_apps = 1;
  string next_page_token = 2;
}
message RevokeConnectedAppRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "metaxisdata/ConnectedApp"}
  ];
}
```

要点:

- **不做 Get**:列表已给全部展示字段;详情没有额外信息。
- **筛选见 §6.4**:`user` 只决定「看谁的行」(自助收窄 / 管理员指定),`filter` 在此基础上收窄;两者同时出现时取交集,`filter` 永远不能扩大 `user` 的范围。
- 分页沿用 `parseLimitAndOffset` / `paginate`(`backend/api/v1/common.go:125-200`),默认 `page_size` 50、上限 1000,按 `created_at DESC, id DESC` 排序(稳定且新连接在前);`page_token` 是不透明偏移,沿用现有实现。
- 新 service 在 `backend/server/grpc_routes.go` 注册 handler(与 `OAuthService` 同一段);前端 `api/client.ts` 加一个 client;`buf generate` 的生成物一并提交。

### 6.2 鉴权矩阵

两个方法都**不带 permission 注解**,进入 `backend/api/v1/acl_interceptor_test.go` 的 `unannotatedMethods` 白名单(与 NotificationService、UserService.UpdateUser 同一档),handler 自己判断:

| 方法 | 条件 | 结果 |
| --- | --- | --- |
| `ListConnectedApps` | 有 `metaxisdata.connectedApps.list`、未传 `user` | 全工作区 |
| | 有权限、传了 `user` | 按该用户过滤 |
| | 无权限 | 强制 `user_id = caller.ID`;传了他人 → `PermissionDenied` |
| `RevokeConnectedApp` | 目标行 `user_id == caller.ID` | 允许(本人撤销自己的授权) |
| | 目标行属于别人 | 有 `connectedApps.revoke` → 允许;否则 **`NotFound`**(不暴露行是否存在) |
| | 行不存在 | `NotFound` |
| | 未登录 | `Unauthenticated`(拦截器已保证,handler 再显式处理) |

为什么不用「两个 RPC(My/Admin)」:同一套表格在两种视角下只差「是否显示用户列」;一个方法 + handler 侧判断少一套 proto、少一套前端调用,并且 `requirePermission` 的既有注释已经为这种形状背书(事实 4)。代价是 `TestEveryMethodIsPermissionGated` 看不出 handler 忘了收窄,因此**必须**补一份 hermetic 鉴权矩阵测试(fake PermissionChecker:成员只看自己、管理员看全部、成员吊销他人 → NotFound、checker 报错时收窄而不是放宽),并在白名单注释里写清理由。若评审更偏好结构性约束,替代方案是把 list/revoke 标注为**基线权限**、另加 `connectedApps.listAll`/`revokeAll` 两个非基线权限做 handler 侧扩展(4 个权限、2 个方法);本方案选前者是因为 `UpdateUser` 已有同样的混合鉴权先例。

### 6.3 错误

| 场景 | 行为 |
| --- | --- |
| `name` 前缀不对 / id 非数字 | `InvalidArgument` |
| `user` 非 `users/{id}` | `InvalidArgument` |
| 行不存在 / 无权吊销他人 | `NotFound`(统一,不泄露存在性) |
| 已吊销后再吊销 | 幂等成功,返回快照,审计照记 |
| `page_token` 非法 | 沿用 `parseLimitAndOffset` 的 `InvalidArgument` |
| DB 失败 | `Internal` |

### 6.4 筛选与排序(管理员列表)

`filter` 复用仓库现有的 CEL 子集(`backend/api/v1/filter.go` 的 `translateFilter`:`&&` / `||` / `==` / `>=` / `<=` / `matches()` / `in [...]`),由服务端翻译成**参数化** SQL;前端只拼过滤表达式,不碰 SQL。支持字段:

| 字段 | 语义 | 实现 |
| --- | --- | --- |
| `user` | `users/{id}`,精确 | `resourceField("g.user_id", common.GetUserID)` |
| `state` | `ACTIVE` / `REVOKED` / `EXPIRED` / `REAUTH_REQUIRED` / `INACTIVE` | 自定义 `filterField`:把 §4.3 的派生状态写成 `CASE` 表达式,`now` 与当前 resource 用 `args.add` 绑定(不拼字面量);支持 `==` 与 `in [...]` |
| `client` | 客户端名,`==` 精确、`matches()` 子串 | `likeField("c.client_name", true)` |
| `client_id` | DCR client_id,精确/子串 | `likeField("g.client_id", false)` |
| `user_email` | 邮箱,精确/子串 | `likeField("p.email", true)`;结果仍受 self 收窄,不会暴露别人的行 |
| `create_time` | 授权时间范围 | 与 audit 的 `create_time` 同形(`>=` / `<=`) |

查询形如:

```sql
SELECT g.*, p.email, p.deleted, p.profile, c.client_name
  FROM oauth_refresh_token g
  LEFT JOIN oauth_client c ON c.client_id = g.client_id
  JOIN principal p ON p.id = g.user_id
 WHERE <user 收窄> [AND <filter>]
 ORDER BY g.created_at DESC, g.id DESC
 LIMIT $n OFFSET $m
```

`state` 的 `CASE` 需要的 `now`/当前 resource 由 handler 一次算好传入;`mcp_enabled=false` 或 `external_url` 不可解析时省略 resource 子句(与 §4.3 的降级一致),此时 `state == REAUTH_REQUIRED` 仍可能命中改密导致的行。**筛选变化必须重置 `page_token`**(偏移分页下否则会落在错误的页);排序固定 `created_at DESC, id DESC`,保证翻页稳定。

前端管理页顶部用现成 `AdvancedSearchBar`(与 AuditLogsPage 同一套交互):`user` 选项懒加载 `listAllUsers()`(需要 `users.list`;没有该权限的自定义角色只是失去用户选择器,仍可用邮箱/客户端搜索),`state` 是 5 个状态的 options,`client` 是输入框;搜索框(type `name`)映射为 `client.matches(q) || client_id.matches(q) || user_email.matches(q)`。自助页数据量小,不提供筛选。

### 6.5 审计

`RevokeConnectedApp` 带 `audit=true`,审计行由拦截器写:method=`metaxisdata.v1.ConnectedAppService/RevokeConnectedApp`,身份=操作者,request=资源名,**response=吊销后的 `ConnectedApp` 快照**(含 user/client id 与名字),因此行在授权行被 maintenance 清理后仍然自解释。幂等重复吊销同样留行,正好记录「谁又点了一次」。**列表不审计**(读基线本来就不写账本);因此列表不受 principal 预算保护,但分页有上限,与其它读接口一致。

### 6.6 权限与信息可见性

`backend/common/permission/permission.json` 增加(再 `go generate ./backend/common/permission`):

```json
{"name": "ConnectedAppsList",   "id": "metaxisdata.connectedApps.list"},
{"name": "ConnectedAppsRevoke", "id": "metaxisdata.connectedApps.revoke"}
```

- **不进** `workspaceMember` 基线(`backend/store/predefined_roles.go`):成员仍能通过自助路径管自己的连接,但看全工作区/吊销他人默认只有 `workspaceAdmin`;
- `workspaceAdmin` 持有整个目录,自动获得;
- 前端 `frontend/src/lib/permissions.ts` 同步加组(`permissions.test.ts` 的漂移测试会钉住);`frontend/src/components/layout/AppSidebar.vue` 的 `SETTINGS_PERMISSIONS` 也要加 `connectedApps.list`,否则只被授予该权限的自定义角色看不到 Settings 入口(前端纠正 C);
- **`user_email` 只在调用者自身就是该用户、或持有 `metaxisdata.users.get` 时填充**;否则留空,前端回退显示 `users/{id}`。列表返回 `user` 资源名本身就是权限边界:有 connectedApps 权限不等于有读用户资料的权限。
- **不返回** `revoked_by` 的 email(库里有,审计里有;列表不扩散);不返回 `redirect_uris`;`client_name` 来自匿名 DCR、仅作文本展示,前端不得渲染成可点击链接。

## 7. 前端设计

### 7.1 信息架构

| 入口 | 路由 | name | 权限 | 页面 |
| --- | --- | --- | --- | --- |
| Settings → 已连接应用(仅管理员可见) | `/settings/connected-apps` | `ConnectedAppsManagement` | `metaxisdata.connectedApps.list` | 全工作区表格,含用户列 |
| 头像菜单 → 已连接应用(所有登录用户) | `/connected-apps` | `ConnectedApps` | 无(登录即可) | 自己的表格,无用户列 |

- 管理页放 Settings(`router/index.ts` 的 `/settings` children,插在 `identity-providers` 之后、`roles` 之前):它与「用户/身份提供商/角色」同属「谁能在这里做什么」的访问治理,是行政差事;`settingsContentWidth: "full"`(宽表格,理由同 `audit-logs`/`applications`)。
- 自助页不进 Settings:它属于「我的账户」,入口是侧边栏底部的 `UserMenu`(头像菜单里已有语言/主题/登出,同属 set-once 个人控制);与 `/notifications` 由 chrome 控件进入是同一类先例。
- 两个页面是**两个薄页面 + 一个共享的 section 组件**:路由 meta(权限 gate)、标题/描述、空态文案不同,但状态徽章、吊销弹窗、取数逻辑只写一份。
- SettingsNav 条目 label 用 `t("connectedApps.title")`(与页面 H1 同一个 key,沿用 `applications` 条目的注释);UserMenu 条目用 `t("menu.connectedApps")`。
- **不要**复用 `/settings/applications`:那是上报 SQL 的 application(不同概念)。

### 7.2 组件与列

文件:

- `frontend/src/api/connected-app.ts`:`connectedAppName(id)`、`listConnectedApps({ pageSize, pageToken, user, filter, signal })`、`revokeConnectedApp(name)`(返回吊销后的 `ConnectedApp`),风格对齐 `api/oauth.ts`;
- `frontend/src/api/client.ts`:`connectedAppClient = createClient(ConnectedAppService, transport)`;
- `frontend/src/components/connected-app/ConnectedAppTable.vue`:纯展示表格,props `{ apps, showUser }`,emit `revoke`;
- `frontend/src/components/connected-app/ConnectedAppsSection.vue`:取数(`usePagedFetch`)+ `Card` + `PageState` + `EmptyState`/表格 + `TablePager` + `ConfirmDeleteDialog` + toast,props `{ scope: "admin" | "self" }`,`defineExpose({ refresh })`;
- `frontend/src/pages/settings/ConnectedAppsSettingsPage.vue` / `frontend/src/pages/ConnectedAppsPage.vue`:标题/描述 + Refresh 按钮 + section。自助页调用时总是传 `user = 当前用户名`,这样即便管理员打开自助页也只看到自己的。

列(管理员 7 列 / 自助 6 列):应用(`clientName || clientId`,有名字时把 clientId 作为次行)、用户(仅管理员,`userEmail || user`)、授权时间、最近调用(`formatRelativeTime`,缺失显示「暂无调用」)、到期时间、状态、操作。时间格式复用 `@/utils/datetime`。

管理员页顶部筛选区复用 `AdvancedSearchBar`(§6.4):用户选择器(`listAllUsers()`,无 `users.list` 权限时降级隐藏/为空)、状态 options、客户端名输入框,搜索框覆盖用户邮箱 + 客户端名 + client_id;`handleFiltersUpdate` 把 pills 拼成 CEL `filter`,**任何筛选变化都重置 page token 并重新拉第一页**。自助页不显示筛选区。

状态徽章(颜色只表达语气,红色留给失败与确认按钮):`ACTIVE` → `success`;`EXPIRED` / `REAUTH_REQUIRED` → `warning`;`REVOKED` → `secondary`;`INACTIVE` → `outline`;`STATE_UNSPECIFIED` → `outline`。

吊销:行内按钮 → `ConfirmDeleteDialog`(`:title`、`:message`、`:item-name`、`:loading`、新增可选 `:confirm-label="t('connectedApps.revoke')"`)。`ConfirmDeleteDialog` 目前把确认按钮写死为 `common.delete`,需要加一个向后兼容的 `confirmLabel` prop 并补测试。成功后 toast + 刷新当前页;若吊销的是本页最后一行且存在上一页,回退一页,避免停在空页。`REVOKED`/`EXPIRED`/`INACTIVE` 行不显示吊销按钮(已无可撤销的访问),用带 tooltip 的 Ban 图标占位;`REAUTH_REQUIRED` 仍可吊销。失败时 toast,弹窗保持打开。

分页:`usePagedFetch`,`pageSize` 默认 50(TablePager 提供 20/50/100),无自动轮询,Refresh 重新拉当前页。空态文案区分管理员/自助。

### 7.3 i18n 与词汇表

- 新增 `connectedApps.*`(title、adminDescription、selfDescription、refresh、app、user、authorizedAt、lastToolCallAt、noToolCall、expiresAt、status、statusActive、statusRevoked、statusExpired、statusReauthRequired、statusInactive、statusUnknown、revoke、revokeTitle、revokeConfirm、revoked、revokeError、fetchError、emptyTitle、emptyAdminDescription、emptySelfDescription,以及筛选用的 filterUser、filterState、filterClient、searchPlaceholder)与 `menu.connectedApps`;中英同步,跑 `i18n:sort`。复用 `common.actions/cancel/close`。状态文案用显式 `switch` 返回字面 `t("connectedApps.status…")`,保证 `check-vue-i18n.mjs` 的静态追踪,不需要 `DYNAMIC_PREFIXES`。
- 术语:**`connected-app`**(category Integration;en "connected app",zh "已连接应用";scope `connectedApps.`/`menu.`)与 **`revoke`**(category Access;en "revoke",zh "撤销";scope 覆盖 `connectedApps.`/`applicationManagement.`/`openlineageSettings.`;`when` 限定英文含 revok,避免把「此操作不可撤销」误判)。写进 `frontend/glossary.json` 后跑 `i18n:glossary --write-doc`,**不要手改**生成表。
- **不要**把本实体叫 session/会话(词汇表已把 session 固定为浏览器会话)。

### 7.4 测试

- `ConnectedAppTable.test.ts`:client_name 为空时回退 client_id;用户列只在 showUser 时出现;六种状态的 label/variant;可吊销行 emit、不可吊销行隐藏;last_tool_call 为空显示「暂无调用」。
- `ConnectedAppsSection.test.ts`:首次加载调用 `listConnectedApps`;空态分管理员/自助;取数失败走 inline error;吊销确认 → `revokeConnectedApp("connectedApps/7")` → 重新拉列表 + 成功 toast;吊销失败 → 错误 toast 且弹窗不关;翻页/换页大小重置;筛选变化重置 page token 并把 CEL 表达式传给 `listConnectedApps`。
- 薄页面测试:两种受众的标题/描述、管理员有用户列、自助传 `user` 过滤。
- 修改:`SettingsNav.test.ts`(权限显隐、位置)、`UserMenu.test.ts`(条目导航)、`router/authGuard.test.ts`(管理路由无权限跳 Home,自助路由允许)、`ConfirmDeleteDialog.test.ts`(confirmLabel)。
- 门禁:`pnpm --dir frontend biome:check` → `lint` → `i18n:sort` → `i18n:glossary --write-doc` → `i18n` → `type-check` → `test run`;`proto && buf generate` 先行。

## 8. 迁移与上线

1. 迁移文件 `backend/migrator/migration/0.1/0021##connected_apps.sql`(当前最新 0020),同时改 `LATEST.sql`(刷新凭据那段,当前 `LATEST.sql:640-663`)。增量:

   ```sql
   ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS last_tool_call_at TIMESTAMPTZ;
   ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ;
   ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS revoked_by INTEGER
     REFERENCES principal(id) ON DELETE SET NULL;
   CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_user_created
     ON oauth_refresh_token(user_id, created_at DESC);
   ```

   `LATEST.sql` 的 `CREATE TABLE` 写成**最终状态**(新列 + 新索引),因为新装只跑 `LATEST.sql`、不跑增量;存量库只跑 0021。行语义(「一行 = 一次授权/已连接应用」)写进 `LATEST.sql` 与新迁移的注释——**不回改已应用的 `0014##refresh_tokens.sql`**(迁移是历史记录,语义变化由新注释与维护文档承载)。`migrator/AGENTS.md` 的规则照旧:同一个 change 里改 store、增量、LATEST。`TestMigrateSchemaLATESTMatchesTheIncrementChain` 会比较列与索引快照,新列/新索引必须两边一致。
2. 代码:store 层 `oauth_refresh_token.go` 改名 `oauth_grant.go`,类型/方法改用 grant 语义(`CreateOAuthGrant` / `GetOAuthGrant` / `RotateOAuthGrant` / `RevokeOAuthGrant` / `RevokeOAuthGrantsByUserAndClient` / `TouchOAuthGrant` / `ListOAuthGrants` / `IsOAuthGrantLive` / `DeleteExpiredOAuthGrants`),SQL 里表名仍是 `oauth_refresh_token`;oauth token 端点、Logout、maintenance、校验链依次接上。
3. 发布:一次性切换。迁移在启动时自动跑;**旧副本不能与新版本混跑**(§5.7)。发布说明写清两点:旧 MCP access token 会被拒、客户端会自动 refresh 一次;回滚需要恢复数据库备份。
4. `mcp_enabled=false` 时管理页仍可用(读库即可):它是清理存量授权的唯一入口,不要跟着开关隐藏。
5. 文档必须同步更新:`docs/mcp.md`(吊销入口、原地轮换)、`.agents/docs/mcp-server.md`(claims 表加 `gid`、invariant 12、failure modes、删掉「无已连接应用管理页」open item)、`refresh-tokens.md`(轮换与吊销语义)、`rate-limiting.md`(吊销受 principal 预算、last_tool_call 每 5 分钟一次的条件 UPDATE)、`iam-and-permissions.md`(目录 + 白名单 + 混合鉴权说明)、`docs/security-posture.md`(吊销措辞、外部地址变更后的旧授权状态)、`AGENTS.md` 的 refresh-token 条目;实现落地时把本文改写为维护参考并加 README 索引。

## 9. 测试计划

| 层 | 新增/修改 | 断言 |
| --- | --- | --- |
| store | `backend/store/refresh_token_test.go` 改名/扩展 | 轮换是 `UPDATE ... WHERE token_hash = $1 AND client_id = $2 ... revoked_at IS NULL ... RETURNING id`;吊销按 id 且带 `revoked_at IS NULL`;列表查询的 JOIN、user 收窄与 filter 参数化形状;过期清理条件;hash 仍是参数 |
| filter | `backend/api/v1/filter_test.go`(或同目录) | `user`/`state`/`client`/`client_id`/`user_email`/`create_time` 的翻译;派生 `state` 的 `CASE` 与 `in [...]`;非法字段/操作符 → `InvalidArgument`;`now`/resource 走参数而不是字面量 |
| oauth | `backend/api/oauth/*_test.go` | 刷新仍单次使用(重放 `invalid_grant`);吊销后刷新 `invalid_grant`;过期不删行但拒绝;principal 拒绝后行被置吊销;首发先落行再签令牌 |
| auth/mcp | `backend/api/auth/*_test.go`、`backend/mcp/*_test.go` | `gid` claim 往返;无 `gid` 的 MCP 令牌被拒;已吊销/行不存在 → 401;三元组绑定(id+user+client);缓存命中/30s 后重读/DB 错误失败关闭;刷新后新 access token 的 `gid` 不变 |
| 缓存 | `backend/component/state/revocation_cache_test.go` 扩展 | 独立实例不被 jti 流量挤出;missing⇒revoked; revoked 不降级 |
| api/v1 | 新 service 测试(含 fake checker 矩阵) | 管理员 list 全量;无权限 list 只回自己;传他人 `user` 被拒;filter 只在 user 范围内收窄;本人可吊销自己;他人需权限(否则 NotFound);幂等且保留首个 `revoked_by`;状态推导(含 REAUTH_REQUIRED/INACTIVE);`user_email` 可见性;`page_token` 边界 |
| 权限 | `permission_test.go`、`acl_interceptor_test.go`、`principal_throttle_interceptor_test.go` | 目录与生成代码一致;两个新方法进白名单;吊销因 audit 自动进预算;前端漂移测试 |
| mcp 工具面 | `tool_guard_test.go` | 9 工具集合不变(本方案不加工具) |
| 集成 | `backend/test/integration/runner/mcp_service_test.go` | 授权 → 列表可见 → 管理员吊销 → `/mcp` 立即 401 → 刷新 `invalid_grant` → 重新授权产生新行;自助只看到自己的;N goroutine 并发刷新恰好一个成功;吊销 vs 轮换并发后状态为已吊销;旧式(无 gid)令牌被拒后 refresh 成功 |
| 迁移 | `migrator_integration_test.go` | LATEST 与增量链快照一致(新列/索引) |
| 前端 | §7.4 | |

Go 门禁:`gofmt` → `golangci-lint run --allow-parallel-runners`(重复到干净)→ `go test ./...` → build;集成 `make test-integration-smoke`,迁移相关 `make test-integration`。

## 10. 工作分解

| 阶段 | 内容 | 门禁 |
| --- | --- | --- |
| P0 数据与轮换 | 迁移 + store 改名/原地轮换 + oauth token 路径 + maintenance 清理 | store/oauth 测试;`go test ./backend/store/... ./backend/api/oauth/...` |
| P1 执法 | `gid` claim + identity + 校验链 + 拒绝无 gid 令牌 + Logout 语义 + 缓存 | auth/mcp 测试 |
| P2 API | proto + `ConnectedAppService` + 列表 filter 翻译 + 权限目录 + 审计 + 矩阵测试 | `buf generate`;`go test ./backend/api/v1/...`;`TestEveryMethodIsPermissionGated` |
| P3 前端 | API client + 两个页面 + 共享组件 + 管理员筛选 + 导航 + i18n/glossary + 测试 | frontend 七连 |
| P4 集成与文档 | 集成用例(含并发/升级窗口)+ `docs/mcp.md` + 维护参考改写 + README 索引 | `make test-integration-smoke` / `make test-integration` |

## 11. 风险与已接受的取舍

| 项 | 取舍 |
| --- | --- |
| 升级瞬间拒绝旧 MCP access token | 客户端自动 refresh 一次;refresh 也过期才需重新授权。换取「立即吊销」无 7 天缺口。接受 |
| 30s 跨副本窗口 | 与现有 `Logout`/`revoked_token` 一致;对外措辞必须写清「本副本立即、其他副本 ≤30s」。接受 |
| 一次性切换(方案 A) | 已确认采用一次性切换;旧副本不能与新版本混跑。若将来必须滚动升级,再切方案 B(§4.4) |
| 每次工具调用一次条件 UPDATE | 5 分钟节流后绝大多数调用零写入;每调用本来已有权限检查 + 审计 INSERT,增量可接受;实现必须是单条条件 UPDATE,不能读后写或无条件写 |
| 派生状态的筛选 | `state` 过滤要 JOIN principal、用 `CASE` 比较 `now`/resource/改密时间,用不上索引;连接数量级小,全表扫描可接受。若将来连接数上万,再考虑物化状态列 |
| 同一 (user, client) 多行 | 每次授权是新连接,列表按行展示;吊销一条不会结束同客户端的另一条(按连接而非按应用的语义,页面描述要写清) |
| 已过期/已吊销行保留 7 天 | 状态可见;表增长 = 连接数 × 7 天,可忽略 |
| 状态推导的边界 | `mcp_enabled=false`/`external_url` 缺失时不判定 `REAUTH_REQUIRED`;改密/停用以外的异常(如账号软删后恢复)按持久化事实呈现。接受,后续可加更多状态 |
| 管理员可见全工作区的授权元数据 | 单租户「成员读一切」的既有姿态;`user_email` 仍受 `users.get` 约束。接受 |
| 列表不审计、不进 principal 预算 | 与其它读接口一致;分页有上限。接受 |
| 旧副本/回滚 | 原地轮换 + 一次切换是前提;回滚需恢复数据库备份。接受 |
| 未做:client 外键、待批准请求随吊销清理、Logout 账本目标、MCP 工具调用归属到应用 | 分别记录为后续项:client 注册目前无删除调用方;pending 请求需要用户再次同意,风险低;Logout 响应为空,账本看不到目标;工具调用账本只有用户没有 client/grant。都不阻塞本方案 |

## 12. 对齐记录

以下取舍已与用户确认,本文按此定稿:

| # | 问题 | 结论 |
| --- | --- | --- |
| 1 | 部署形态:能否一次性切换 | **能** → 采用方案 A(复用授权行 + 原地轮换),§4.4 |
| 2 | 已过期/已吊销记录保留 7 天 | **可以**,§4.3 |
| 3 | 自助入口放头像菜单 | **可以**,§7.1 |
| 4 | 撤销文案用「撤销」还是「吊销」 | **撤销**(与现有 API key 页面一致),§7.3 |
| 5 | 是否做用户/状态筛选 | **做**:管理员列表支持用户/状态/客户端筛选,§6.4、§7.2 |
