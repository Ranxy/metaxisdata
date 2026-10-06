# Metaxisdata 安全性审查报告(2026-10)

- **审查日期**:2026-10-03
- **审查对象**:当前工作区(backend / frontend / cli / proto / docs / 构建与 CI)
- **审查方式**:8 个领域(认证会话、IAM/授权、OAuth 2.1+MCP、数据层注入、凭据密钥、网络边界/出站、前端/CLI、配置运维)并行子代理审查(模型 `deepseek-v4.1-flash`),主代理对全部高危与关键中危逐条引用代码复核(累计抽查约 45 处,含 3 项可执行复现验证),并复核了两个子代理主动排除的误报。
- **基线**:审查前通读 `docs/security-posture.md`,"有意接受的决策"不计为漏洞,单列于第 8 节。

---

## 0. 执行摘要

**总体评价:认证与协议层的实现质量高于同类自托管项目**——JWT 固定 HS256+kid/iss/exp/aud 全校验且受众比较在验证器内部、OAuth 2.1 强制 S256 PKCE 且授权码"先消费再校验"防重放、MCP token 按 RFC 8707 绑定资源标识、设备登录 256 位/40 位双码设计完整、权限字符串精确匹配且多处 fail-closed、OpenLineage API key 用 bcrypt 存储、全部随机数走 crypto/rand、前端无 `v-html` 且 token 只存 HttpOnly cookie、store 层数据访问基本全参数化。

**主要风险不在密码学,而集中在四类系统性问题:**

1. **默认开放姿态**(高危):匿名 `CreateUser` + 首用户即 `workspaceAdmin` ⇒ 未初始化实例可被扫描者远程接管;自助注册默认开启 ⇒ 任何端口可达者可成为"可读全部元数据"的成员;SSO 仅按邮箱匹配身份 ⇒ 预占邮箱可劫持管理员账号。
2. **对被管理目标库的二次注入**(高危):同步链路把从目标实例 `information_schema` 读到的对象名/库名直接拼进 `SHOW CREATE …` 与 DSN,叠加 `multiStatements=true`,使被监控库内能建表的低权限用户可借平台的高权限同步账号在目标实例执行任意 SQL。
3. **边界与出口层的系统性缺失**(中危):XFF 取最左值使审计 IP 与全部按源限流可被伪造;REST 网关自连接使审计 IP 恒为 127.0.0.1;审计账本(永久保留)没有写入上限与背压,匿名请求可放大为每请求最大 100MB 的永久磁盘占用;SSH 隧道无主机密钥校验且无超时;全仓无 CSP;OpenLineage 派生链接与 CLI 打开浏览器均无 URL scheme 校验。
4. **"恰好安全"的机制耦合**(中危):`allow_without_credential` 注解会架空 `permission` 注解且守护测试无法发现;受限 token(强制改密)只在 ConnectRPC 拦截器层生效,HTTP 端点靠"批准恰好走 Connect"才安全;登出吊销缓存是用户可自助刷爆的有界 LRU。

**统计(合并去重后)**:高危 7、中危 24、低危 21、提示 7;另列设计问题与安全技术债务 17 项。所有发现的证据(`文件:行号`)均已人工复核属实。

**建议的立即行动(P0)**:
1. 参数化 `SHOW CREATE`/`getViewDependencies` 并从 DSN 移除 `multiStatements=true`(H4/H5);
2. 未初始化实例不接受匿名注册,默认 `disallow_signup=true`(H1/H2);
3. SSH 隧道启用主机密钥校验并补齐握手超时(H7);
4. `FirstForwardedFor` 改取最右未信任段(M1);
5. 为审计载荷(MCP 8KiB 截断)统一加上限,补齐匿名端点限流(H6/M17/M18)。

---

## 1. 高危发现(H)

### H1. 未初始化实例可被匿名远程接管:首用户即 workspaceAdmin
- **证据**:`proto/v1/v1/user_service.proto:53`(`CreateUser` 带 `allow_without_credential = true`,且无 permission 注解);`backend/api/v1/acl_interceptor.go:74`(`if authCtx.AllowWithoutCredential || authCtx.Permission == "" { return nil }`,ACL 放行);`backend/api/v1/user_service.go:255-257`(`activeEndUserCount == 0` 时跳过含 `disallow_signup` 在内的全部检查);`backend/store/principal.go:400-406`(首个 END_USER 直接 `Roles: []string{common.FormatRole(common.WorkspaceAdmin)}`)。
- **攻击场景**:全新部署在管理员完成初始化前被扫描到,匿名 `POST /v1/users` 自选邮箱密码即成为唯一 `workspaceAdmin`(并发由 `pg_advisory_xact_lock` 串行化,只产生一个)。变体:提交不含密码的请求,服务端生成随机密码但不返回 END_USER 分支(`user_service.go:198-226`),该 admin 账号无人能登录,运营方需改库恢复(见 L18)。
- **修复**:未初始化态不接受匿名注册(一次性 setup token / 本机 CLI 引导);首个管理员不由匿名请求授予;在 `security-posture.md` 显式声明或关闭该窗口。

### H2. 自助注册默认开启:任何端口可达者可自助成为"可读全部元数据"的成员
- **证据**:`backend/server/init.go:76`(初始化写入空 `WorkspaceProfileSetting` ⇒ `disallow_signup=false`);`backend/api/v1/user_service.go:259-265`(默认不触发拒绝);`backend/store/predefined_roles.go:39-53`(成员基线含 InstancesGet/List、DatabasesList/Read、LineageGet、OpenLineageRead、ManualSQLs*、ExplainSQLExplain)。
- **攻击场景**:默认配置下匿名注册即 `workspaceMember`:全量读取实例清单、库表列元数据、手工 SQL 文本、血缘,并可改删他人手工 SQL。凭据混淆虽不回显(`DataSource` 凭据字段 INPUT_ONLY),但 host/port/username/`extra_connection_parameters` 全部可见。无限流、无审批、无告警。
- **修复**:注册默认 invite-only;开启时加审批/限流/告警;`disallow_signup` 作为显式安装选择而非空默认值。

### H3. SSO 身份仅按邮箱匹配 + 自助改邮箱无需当前密码:预占邮箱劫持账号
- **状态**:**已修复**(2026-10-03,commit `bb4cb7e`;修复内容与验证见 §10,末条为随后按 provider 可选的"以邮箱作为 SSO 身份"及一人多绑定,commit `5766be2`)。修复方式与原建议有一处差异:改邮箱由"要求当前密码"改为"仅 `users.update` 持有者(管理员)可改",本人亦同——邮箱是账号在工作区外被识别的身份,属管理字段。
- **证据**:`backend/api/v1/auth_service.go:386-405`(`GetUserByEmail(email)` 命中即登录,用户行不保存任何 IdP subject 绑定,且未校验 IdP 的 `email_verified`);`backend/api/v1/user_service.go:352-364`(自更新邮箱路径不要求 `current_password`,与 `password` 路径 373-380 的对照);`backend/api/v1/auth_service.go:449`(IdP 组按 `Email == group || Title == group` 匹配,可变标题参与)。
- **攻击场景**:A(自助注册,依赖 H2 默认开启)把邮箱改为 `ceo@corp.com`(`enforce_identity_domain` 白名单内亦可);B 首次 SSO 登录被匹配进 A 的账号行,A 始终握有该行密码。管理员按邮箱给"该员工"授权后角色一并被 A 继承。组名/标题碰撞可造成角色误授。
- **修复**:登录校验 IdP 主体(存储 `idp_id+subject` 并比对),邮箱仅作展示;自助改邮箱要求当前密码;IdP 组匹配只用稳定标识,标题不参与。

### H4. MySQL 同步把目标库对象名裸拼进 `SHOW CREATE …`,叠加 `multiStatements=true` ⇒ 目标实例任意 SQL 执行
- **状态**:**已修复**(2026-10-03,commit `8b3ded8`;修复内容与验证见 §10)。
- **证据**:`backend/plugin/db/mysql/sync.go:1208`(`fmt.Sprintf("SHOW CREATE TABLE \`%s\`.\`%s\`", databaseName, tableKey.Table)`,反引号未转义;同型还有 :1056 视图、:928 存储过程、:864 函数、:685 事件);`backend/plugin/db/mysql/mysql.go:108`(`params := []string{"multiStatements=true", "maxAllowedPacket=0"}` 强制开启)。
- **攻击场景**:databaseName/对象名全部来自被监控实例的 `information_schema`。MySQL 标识符可含反引号(`CREATE TABLE \`x\`\` ; DROP TABLE t; -- \``),插值后逃逸出标识符,`multiStatements=true` 使整串多语句执行。在目标库建一个恶意命名的表/视图/例程/事件,等定时同步跑一次,即可以平台登记的高权限同步账号在目标实例执行任意 SQL,无需与平台有任何交互。
- **修复**:标识符转义(反引号→双反引号、拒 NUL)或参数化替代;从同步连接的 DSN 默认移除 `multiStatements`。

### H5. PostgreSQL `getViewDependencies` 用 `'%s'` 拼 schema/view 名 ⇒ 二次 SQL 注入读取目标库数据
- **状态**:**已修复**(2026-10-03,commit `8b3ded8`;修复内容与验证见 §10)。
- **证据**:`backend/plugin/db/pg/sync.go:1098-1101`(`WHERE dependency_ns.nspname = '%s' AND dependency_view.relname = '%s'` + `Sprintf(schemaName, viewName)` + 无参数 `txn.Query`)。
- **攻击场景**:schemaName/viewName 来自目标库 `pg_views/pg_matviews`。PG 允许 `CREATE VIEW "x' UNION SELECT … --"`,注入后 `--` 注释掉剩余 WHERE,结果写入 `ViewMetadata.DependencyColumns`,经元数据接口回显给任何 `workspaceMember` ⇒ 持低权限目标库账号者借高权限同步账号读任意表数据(pgx 走扩展协议,多语句不可用,故为读取/盲注级)。
- **修复**:改 `$1/$2` 参数化。

### H6. 匿名 `CreateUser` 被全量审计且无请求体上限:未认证者可向永久账本写入最大 100MB/请求
- **证据**:`backend/api/v1/audit.go:48-51`(成功/失败都落审计);`backend/component/audit/audit.go:38-57`(请求整体序列化进 `audit_log`);`proto/v1/v1/user_service.proto:53-54`(匿名 + audit);`backend/server/grpc_routes.go:124-125`(仅 AuthService 收窄 64KiB,UserService 无 `WithReadMaxBytes`);`backend/server/echo_routes.go:27`(全局 BodyLimit 100M)。
- **攻击场景**:匿名 POST `/v1/users` 带 100MB 的 `user.title`(email 校验失败也照样留审计行),每请求 ≈100MB 永久磁盘 + WAL + 索引;`audit_log` 按既定决策永不清理,无凭证、无限速,可持续打满磁盘使整个平台不可用。MCP 侧有 8KiB/串截断(`backend/mcp/server.go:386`),Connect 侧缺同一道闸——是遗漏而非策略。
- **修复**:审计载荷统一字节上限(与 MCP 一致或整行封顶);各 service 补 `WithReadMaxBytes`;匿名方法请求字段做长度校验(另见 M17/M18 同族放大)。

### H7. SSH 隧道无主机密钥校验且无超时:中间人窃取库凭据 + 单实例持久 DoS
- **状态**:**已修复**(2026-10-06,主修复 commit `b06249d`,独立复核后的加固 commit `73c7015`;修复内容与验证见 §10)。
- **证据**:`backend/plugin/db/util/ssh.go:21`(`HostKeyCallback: ssh.InsecureIgnoreHostKey()`);`util/ssh.go:49`(`ssh.Dial` 无 `ClientConfig.Timeout`,TCP 可接受但永不完成握机的恶意 ssh_host 将无限挂住);`backend/plugin/db/mysql/mysql.go:123-125`、`starrocks.go:89-91`(DialContext 显式丢弃 ctx);`util/ssh.go:64-68` + `pg.go:75-81`(`NoDeadlineConn` 把 SetDeadline 全部 no-op,pgx 的 ctx 取消与 15 分钟同步 deadline 全部失效)。
- **攻击场景**:(a) 同网段中间人接受 SSH 隧道即可截获后续 MySQL/PG 明文通道里的库凭据与元数据;(b) 任意成员(需实例写权限)把 `ssh_host` 指向恶意主机,握手永挂 + ctx 不可取消 ⇒ goroutine/fd 泄漏,且该实例每实例连接额度(默认 10)被永久占满,同步持续 DoS;连接测试路径(`instance_service.go:598-631`)不受每实例限额约束,可无限叠加。
- **修复**:支持 known_hosts/指纹配置,默认拒绝未知主机密钥;`ClientConfig.Timeout` + 全链路传递 ctx(去掉 NoDeadlineConn 或正确实现 deadline 透传);连接测试纳入限额。

---

## 2. 中危发现(M)

### M1. `FirstForwardedFor` 取 XFF 最左段:审计 IP 伪造 + 全部按源限流绕过
- **证据**:`backend/component/audit/audit.go:247-256`(`strings.Split(value, ",")[0]`)。
- **影响**:主流反代默认按追加语义写 XFF(`<客户端自带值>, <真实对端>`),只要把反代列入 `--trusted-proxies`(文档要求的姿势),取到的仍是客户端自选值。消费方覆盖:审计 IP(`api/v1/audit.go:132`、`openlineage_handler.go:350`、`mcp/server.go:336`、`oauth/audit.go:114`)、设备登录创建限流(`auth_service_device_login.go:49-53`)、OAuth 匿名端点限流(`oauth_endpoints.go:39-48`)。攻击者每请求换一个 XFF 首段即重置限流桶,并把伪造来源写进永不清理的 `audit_log`。
- **修复**:取最右一段,或从右往左跳过信任 CIDR 直到第一个未信任地址。已接受决策"代理须归一化/剥离 XFF"未覆盖"最左/最右"这一实现选择,常规追加配置下无法靠代理修复。

### M2. REST 网关自连接:`/v1/*` 审计 IP 恒为 127.0.0.1,诱导信任 loopback 后客户端可自选审计 IP
- **证据**:`backend/server/grpc_routes.go:177-187`(网关以 `fmt.Sprintf(":%d", profile.Port)` 自连接,内层 Peer 恒为 127.0.0.1;grpc-gateway 把外层 XFF 原样前置再追加对端)。
- **攻击场景**:全部 REST 形式请求审计行 IP 失真;运维"修复"自然做法是把 127.0.0.1 加入 `--trusted-proxies`,此后 `FirstForwardedFor` 取的正是客户端自带段(M1),审计 IP 完全可控。
- **修复**:网关路径用外层 RemoteAddr 构造 metadata 或取最右未信任段;文档明确禁止把 127.0.0.1 列入 trusted-proxies。

### M3. 登录失败限流键 = (email, 裸 TCP 对端地址):定向锁死任意邮箱 + 换邮箱绕过
- **证据**:`backend/api/v1/auth_service.go:50-56`、`:92-95`(`loginThrottleKey(request.Email, req.Peer().Addr)`;connect 的 `Peer().Addr` 即 `RemoteAddr`)。
- **攻击场景**:(a) 反代部署下所有客户端对端塌缩为代理 IP,对 `victim@example.com` 连发 10 次错密即锁 5 分钟,循环补发即可无限期阻止其登录(被拒时连密码都不校验);(b) 键按邮箱分桶,每次换不存在邮箱即新桶,对 CPU 消耗(见 M5)无约束;(c) 无源级总上限,密码喷洒不受限。同根因:未配可信代理时设备登录创建限流(10/min/source)退化为全站 10/min。
- **修复**:源维度改用 trusted-proxy 感知的真实 IP(与 audit 同一逻辑),账号维度与源维度拆为两个独立计数;Login 增加源级粗上限。

### M4. 登出吊销缓存是用户可自助刷爆的有界 LRU:已吊销 token 可复活
- **证据**:`backend/component/state/state.go:12-14`(`tokenRevocationCapacity = 4096` LRU);`backend/api/v1/auth_service.go:267-283`(`Logout` 仅验签后 `TokenExpireCache.Add`);登录限流只计失败(`login_limiter.go:53-64`),成功登录无限流;每次登录因随机 jti 产生不同 token(`api/auth/auth.go:294-302`)。
- **攻击场景**:攻击者用普通账号登录 4096+ 次并逐个 Logout,把受害者已吊销的 token 淘汰出 LRU——泄露后主动登出/共享机器登出的 7 天 token 重新可用。
- **修复**:吊销判定改为按用户 + `revoked_at`/改密水位线的持久化状态;至少把吊销集合改为带 TTL 且不可被无关主体挤占的结构。

### M5. 匿名 bcrypt 端点无有效限流:CPU 耗尽 + 邮箱枚举
- **证据**:`backend/api/v1/auth_service.go:296-302`(未知邮箱执行 dummy bcrypt 防时序侧信道,但键随邮箱变化即新桶);`user_service.go:206`(CreateUser 每请求一次 `bcrypt.Generate`);`user_service.go:182-184`(`AlreadyExists` 构成已注册邮箱枚举预言机);Connect 入口无任何通用限流。
- **攻击场景**:约 100 req/s 即可打满 CPU(约 50–100ms/请求),登录与注册均不可用;同时枚举成员邮箱名单供钓鱼/撞库。
- **修复**:Connect 入口加"按源 + 全局"双桶限流(设备登录 create 已有正确模板);CreateUser 存在性检查移到限流之后并模糊响应。

### M6. 会话 cookie 可被同域子站投毒(会话固定/登录 CSRF)
- **证据**:`backend/api/auth/auth.go:237-247`(`GetTokenFromHeaders` 取第一个同名 `access-token` cookie);cookie 无 `__Host-` 前缀、无重复同名拒绝逻辑;登出只清 host-only cookie。
- **攻击场景**:攻击者控制父域任一兄弟子域(或子域被接管)时,可种 `Domain=example.com; Path=/v1` 的同名 cookie;浏览器按路径长度排序,`Request.Cookie` 取第一个 ⇒ 受害者所有 `/v1` 请求以攻击者身份执行(录入实例/库口令全落攻击者账号),界面正常、无法自救。条件:部署在共享父域且存在可控兄弟子域。
- **修复**:HTTPS 部署改 `__Host-access-token`(隐含 Path=/、无 Domain、Secure);服务端遇多个同名 cookie 直接拒绝请求。

### M7. SSO 登录自动复活被停用账号;停用无"令牌水位线"
- **证据**:`backend/api/v1/auth_service.go:391-396`(SSO 登录命中 `MemberDeleted` 即自动 undelete);`backend/api/auth/authenticator.go:175-186`(`Resolve` 只比密码变更时间,停用不记录时刻)。
- **攻击场景**:管理员 DeleteUser(离职/失陷应急)后,该用户只要 IdP 侧仍可登录即自动复活并获全新 7 天 token,停用形同虚设;undelete 后停用前签发的旧 token 一并恢复可用。
- **修复**:SSO 登录不得自动 undelete(管理员显式恢复 + 审计);principal 记录 `revoked_at`/`credential_invalid_before` 并在 `Resolve` 比较。

### M8. `allow_without_credential` 架空 `permission` 注解(机制缺陷)+ `GetWorkspaceProfileSetting` 匿名泄露设置
- **证据**:`backend/api/v1/acl_interceptor.go:74`;`proto/v1/v1/setting_service.proto:21-22`(acw 与 `permission = "metaxisdata.settings.get"` 并存,后者是死注解);`backend/api/v1/setting_service.go:109-117`(匿名可读 `domains`、`enforce_identity_domain`、`allowed_llm_provider_profiles`、`external_url`、`mcp_enabled`);守护测试 `acl_interceptor_test.go:154-166` 测不出"注解被 acw 架空"。
- **攻击场景**:任何方法日后被加上 acw 即静默失去权限闸门且测试不红;域白名单与 LLM 允许清单对未认证者可见(external_url/mcp_enabled 的匿名可见属已接受决策,其余是增量披露)。
- **修复**:测试断言 `acw == true ⇒ permission == ""`(反向互斥);或 ACL 对 acw 方法仍执行 permission 检查,另设明确的匿名方法白名单。

### M9. `allUsers` 可绑定 `workspaceAdmin`:一次误操作把未来所有注册者变成管理员
- **证据**:`backend/api/v1/iam_service.go:142`(`case member == common.AllUsers: return nil`);`backend/api/v1/iam_helpers.go:33-42`;前端提供任意组合入口(`IamPage.vue:147`)。
- **攻击场景**:与 H2 叠加——管理员为"方便"给 allUsers 绑 workspaceAdmin("至少一个活跃管理员"校验通过),此后任何匿名注册者即管理员。单向门,难以察觉。
- **修复**:拒绝 `allUsers` 绑定任何含管理权限的角色;写校验 fail-closed。

### M10. OpenLineage 派生链接无 scheme 校验:`javascript:` 存储型 XSS
- **证据**:`backend/plugin/openlineage/airflow_links.go:48-56`(`runLogURL := strings.TrimSpace(facet.TaskInstance.LogURL)`,仅 TrimSpace);`frontend/src/pages/openlineage/OpenLineageRunDetailPage.vue:9` 与 `OpenLineageTaskDetailPage.vue:9` 直接 `:href` 绑定;数据源是持 ingestion key 即可写的原始事件 facet。
- **攻击场景**:被入侵/恶意的 Airflow 提交 `log_url = "javascript:…"`,受害成员在运行详情页点"打开运行日志"即在 SPA 源执行脚本——同源调用 ConnectRPC 全部通过 CSRF 检查,HttpOnly cookie 无济于事,以受害者身份读全部元数据/执行管理员操作。下限也是服务端数据决定的任意链接注入(钓鱼)。
- **修复**:后端派生链接只保留 http/https 且 host 非空;前端 `:href` 统一经 URL 白名单再绑定(与 M14 是同一债务的两个面)。

### M11. 全仓缺失 CSP 与点击劫持防护:`/device` 审批页可被 iframe 劫持
- **证据**:全仓(排除 node_modules)对 `Content-Security-Policy`/`X-Frame-Options` 0 命中;SPA 由裸 `http.FileServer` 提供(`server_frontend_embed.go:35-45`),echo 中间件无 `middleware.Secure()`。
- **攻击场景**:攻击者自建 device login 拿 user_code,把 `/device?user_code=…` 框进诱导页;external_url 为 https 时 cookie 为 `SameSite=None; Secure`(header.go:84-89),frame 内 cookie 照发、ConnectRPC 同源请求 CSRF 检查通过,诱点"批准"即取得受害者身份的 7 天 CLI token。CSP 同时是 M10 的第二道防线。
- **修复**:统一安全响应头:`default-src 'self'; script-src 'self'; frame-ancestors 'none'` + `X-Frame-Options: DENY` + `X-Content-Type-Options: nosniff`。

### M12. CLI 跨主机重定向时重新附加 Bearer token:7 天凭证外泄(已复现)
- **证据**:`cli/client/client.go:282-289`(bearerTransport 见 Authorization 为空即补上——恰是标准库跨主机重定向剥头之后);`client.go:198`(http.Client 无 CheckRedirect)。子代理以独立探针复现:307 跨主机后收集端收到 `Bearer SECRET-TOKEN`,基线(无该 transport)证明标准库本身会剥除。
- **攻击场景**:入口/代理误配跳转、`http://` 部署被改写 Location 或 `--server` 指向不可信服务器时,CLI 自动带 7 天会话 token 跟随到第三方主机。
- **修复**:`CheckRedirect` 非同 host 一律 `http.ErrUseLastResponse`;或 transport 仅在目标 host 与登记 host 一致时附加。

### M13. Windows 下 `cmd /c start <服务器返回的 URL>` 参数注入:本地命令执行
- **证据**:`cli/cmd/auth.go:227-236`(`case "windows": command, args = "cmd", []string{"/c", "start"}`);URL 完全由服务器响应决定且不询问用户自动打开(`cli/authflow/device.go:104-105,153-159`)。
- **攻击场景**:对攻击者/被入侵服务器执行一次 `mxd auth login`,响应的 `verification_uri_complete` 含 `&` 即逃逸为命令分隔符(`cmd /c start https://evil/x?a&calc.exe`)。登录前无 token,纯受害者场景。
- **修复**:打开前强制 http/https(与 --server 同源更佳);Windows 改 `rundll32 url.dll,FileProtocolHandler`;要求人工确认。

### M14. OpenLineage 摄取限流在无 key 时回退 `c.RealIP()`:绕过限流 + 内存堆积
- **证据**:`backend/server/openlineage_ingestion.go:38-44`(回退 `c.RealIP()`;echo 未设 IPExtractor,RealIP 无条件信任 XFF 最左段);echo RateLimiterMemoryStore 无容量上限(3 分钟清理一次)。同项目 `oauth_endpoints.go:36-48` 已有正确写法并注释说明 RealIP 的问题——属遗漏。
- **攻击场景**:不带 key 的请求每次换 XFF 即新桶,50rps 上限失效,且每次 401 触发一次 `ValidateOpenLineageAPIKey` DB 查询;唯一 key 可在窗口内堆出百万级桶。
- **修复**:回退改用 `c.Request().RemoteAddr` 或设 `e.IPExtractor`;限流 store 加容量上限。

### M15. `CreateSSOState` 匿名无限且 SSO/IdP 出站无防护:SSO 登录 DoS + 免费企业 IdP 探测器
- **证据**:`backend/api/v1/auth_service.go:244-251`(匿名写入 1024 项 LRU 的 state,`state.go:16-20`);Login 的限流只在口令分支(`auth_service.go:92-101`),IdP 分支无限流;`backend/plugin/idp/oauth2/oauth2.go:49-55`(`http.Client` 无 Timeout,UserInfo 不带 ctx)。
- **攻击场景**:匿名刷空 LRU 即可让真实用户的 SSO 回调全部失效("invalid or expired state" 登录 DoS);未认证者可让服务器带着 client_secret 持续出站打企业 IdP。
- **修复**:CreateSSOState 按 IP 限流;IdP client 加 Timeout 并透传 ctx。

### M16. OAuth 注册/授权/MCP 面的写入与内存放大:注册审计无截断、pending 10GB、`/mcp` 无限流
- **证据**:`backend/api/oauth/register.go:18`、`:125-129`(注册体上限 64KiB,但 `redirect_uris` 无单条限长且审计明细原样落库);`backend/component/state/oauth_authorization_request.go:22`(pending 容量 10000,`state` 参数无长度上限、`RedirectURI` 可达 64KiB,TTL 10 分钟);`backend/server/grpc_routes.go:299-300`(`/mcp` 只有跨源保护,无限流);`backend/mcp/server.go:386-425`(审计只按单字符串 8KiB 截断,2MiB 请求体几乎原样入账本)。
- **攻击场景**:匿名注册以限流速率持续写 ≈0.6MiB/s 的永久审计行 + 客户端行(叠加 XFF 伪造为 4 倍);已登录用户循环 authorize 填满 10000 条(约 10 分钟)占 ≈10GB RSS 并逐出他人 pending;持 MCP token 的成员每次调用写 ≈2MiB 永久审计行。
- **修复**:redirect_uri 限长(2-4KiB)、OAuth 审计明细与 MCP 同标准截断(整行封顶);authorize 的 `state` 设硬上限(如 512B);`/mcp` 加按用户限流与超时;审计只记 schema 声明过的字段。

### M17. User-Agent 未截断进入永久审计与设备登录内存
- **证据**:`backend/component/audit/audit.go:227-232`(原样取 UA);`backend/api/v1/auth_service_device_login.go:49-67`(pending 记录持有完整 UA 10 分钟,store 容量 10000)。
- **攻击场景**:匿名 Login/CreateUser(audit=true、无限流)以约 1MB 的 UA 无上限写 `audit_log`;设备登录峰值可达数百 MB-1GB 常驻内存。同文件已给 client_name/version 设 100 字节上限,唯独漏了 UA。
- **修复**:`BuildRequestMetadata` 对 UA 截断(如 256B),审计写入侧限制 metadata 字段长度。

### M18. openlineage `raw_payload` 无单事件上限 + 列表无条件全量读取:单请求数十 GB 内存放大
- **证据**:`backend/api/v1/openlineage_handler.go:26`(`maxOpenLineageBodySize = 8MiB`);`backend/store/openlineage_run.go:436-461`(列表 SQL 无条件 SELECT raw_payload);`openlineage_service.go:100-111`/`openlineage_dataset.go:22,46-47`(一次读 5000 行)。
- **攻击场景**:持一把摄取 key(50rps)几分钟灌入约 5000 个 8MiB 事件,任意一次 `ListOpenLineageDatasets`(5000×8MiB≈40GB)或 `ListOpenLineageRuns`(一页 1001 行)即 OOM。
- **修复**:单事件限长(如 1MiB)并对 namespace/job_name/run_id 限长;列表不取 raw_payload(Airflow 链接在摄取时物化);dataset 聚合下推 SQL。

### M19. MySQL DSN 由目标库库名拼接:驱动参数覆盖、TLS 降级
- **证据**:`backend/plugin/db/mysql/mysql.go:141`(`fmt.Sprintf("…/%s?%s", …, ConnectionContext.DatabaseName, …)`);子代理在仓库锁定的 go-sql-driver v1.9.3 上实测:库名 `x?tls=false&` → `dbname=x, tls=false`;`x?allowAllFiles=true&` 同理绕过 `ValidateExtraConnectionParameters` 的键黑名单。
- **攻击场景**:在目标实例建一个带 `?`/`&` 的库名即可改写平台的出站连接参数(TLS 降级 → 中间人可污染同步流量与血缘;`allowAllFiles` 打开本地文件读取面;同步静默指向另一库)。前提:能在目标实例建库。
- **修复**:用 `mysql.Config` 结构化构造(库名进 `DBName` 字段),或对库名做严格白名单(禁 `? & = /`);`ExtraConnectionParameters` 键值白名单。

### M20. PG/MSSQL `extra_connection_parameters` 零校验:连接目标可被改写
- **证据**:`backend/plugin/db/pg/pg.go:123-131`(字符串拼接,无任何校验);实测 pgx.ParseConfig:重复键 `host=evil.example` 覆盖成功、值中空格可注入新键、`sslmode=disable` 生效;`mssql.go:59-63` 无校验。MySQL/StarRocks 至少有键黑名单。
- **攻击场景**:登记 host 与实际连接 host 可不一致(审计台账失真 + PG 协议 SSRF 通道);`host` 覆盖为内网任意 PG 端口探测。注意:该字段本身需管理员权限登记,但来自被同步实例的快照更新链路时同样未过滤。
- **修复**:白名单化连接参数键值;`host`/`port` 不可被参数覆盖。

### M21. 连接测试(ValidateOnly)是无鉴权回显的内网端口扫描器
- **证据**:`backend/api/v1/instance_service.go:598-631`(CreateInstance/CreateDataSource/UpdateDataSource 三个入口);`:594-597`(刻意不设超时、不受每实例连接限额);驱动错误原样回显给调用者。全仓库无 host 白/黑名单。代码注释声明刻意,但 `security-posture.md` 未收录。
- **攻击场景**:持实例写权限者(可由自定义角色下放)对任意 `host:port` 出站探测,错误文本差异(连接拒绝/超时/协议不匹配)构成端口与服务指纹侧信道。
- **修复**:错误信息统一化(拒绝/超时/协议错误不区分);超时纳入;部署文档提供 host 策略选项(allowlist)。

### M22. `use_ssl` 未勾选校验时静默 `InsecureSkipVerify`,SSH agent 密钥被外提
- **证据**:`backend/plugin/db/util/ssl.go:27-33`(`if !ds.GetVerifyTlsCertificate() { cfg.InsecureSkipVerify = true }`,"default for backward compatibility");`util/ssh.go:29-37`(`ssh_private_key` 为空时把服务器自身 `SSH_AUTH_SOCK` 的 agent 全部密钥提供给用户指定的 SSH 主机);`idp/oauth2/oauth2.go:52` 同型开关。
- **攻击场景**:目标库 TLS 实为未认证加密,同网段可中间人;服务进程若带 agent socket 运行,其密钥会被提供给任意用户指定的 SSH 主机(公钥枚举,匹配则以运维者身份完成认证)。
- **修复**:TLS 校验默认开启并在 UI 显式提示降级;SSH agent 回退删除或显式配置;`SkipTlsVerify` 设置页加警告。

### M23. LLM 出站无内网限制、无重定向策略:内网 SSRF 发起器与 key 泄露面
- **证据**:`backend/component/llm/fetcher.go:34-50`(`ValidateBaseURL` 仅查 scheme∈{http,https}+host 非空);`agent.go:72-83`(共享 client 用 `ProxyFromEnvironment`、无 CheckRedirect);`agent.go:279/305`(无条件带 Bearer key)。缓解:`llm_service.go:156-168` 改 base_url 必须同时提交新 key(旧 key 不可被转发)。
- **攻击场景**:能配置 LLM profile 的主体(可下放给自定义角色)可把平台变成带 API key 的内网 HTTP 探测器;明文 http 的 base_url 下 key 明文过网。
- **修复**:base_url 可选内网黑名单(至少告警);http(非 loopback)base_url 拒绝或显式警告;LLM 调用计入配额(见 M24)。

### M24. 全站无通用限流:LLM/ExplainSQL 无配额,审计写入无背压
- **证据**:`backend/server/grpc_routes.go:103-114`(拦截器链仅 debug/auth/audit/acl/error);`/mcp`、CreateSSOState、CreateUser、Login(见前述各条)均无覆盖;审计注解 RPC + MCP 每调用各写一行永久审计。
- **攻击场景**:任一成员可无限并发触发 LLM 调用(费用与上游耗尽);带审计注解的写操作可无界占用磁盘(与"审计永久保留"叠加)。
- **修复**:加"按(用户, 方法)"限流拦截器 + 全局并发上限;审计写入加每用户速率上限(永久账本更需要背压);LLM 加配额与费用上限告警。

### M25. pprof/metrics 无认证,运行时可由 settings 权限开启,heap 转储含 AUTH_SECRET
- **证据**:`backend/server/pprof.go:11-21`、`echo_routes.go:64-92`(仅 `runtimeDebug` 门控,无认证);`backend/api/v1/setting_service.go:173-181`(运行时开关,`settings.update` 可下放自定义角色);进程内驻留明文凭据(instanceCache 32768 条、LLM registry 30s)与 `profile.Secret`。
- **攻击场景**:管理员为"看日志"打开 Debug 后,任何人匿名取 heap/goroutine/cmdline/profile(DoS + 内部状态收集);heap 中含 JWT 签名密钥与明文凭据。与日志级别、panic 栈回显共用一把闸刀。
- **修复**:`/debug/*`、`/metrics` 独立开关 + 管理员认证(或只绑回环);运行期开启时显著告警。

---

## 3. 低危发现(L)

| # | 发现 | 证据 | 说明/修复 |
| --- | --- | --- | --- |
| L1 | CSRF 把 `Sec-Fetch-Site: same-site` 当可信 | `server/csrf.go:59-62` | 同站兄弟子域/同域异端口可过判定;https 部署 cookie 为 SameSite=None 时这是第二道防线的洞。只信任 same-origin/none,跨源前端走 `--cors-allow-origins` |
| L2 | CSRF 三来源头全缺时放行 + Origin/Host 比较忽略 scheme | `server/csrf.go:63-70,93-99` | fail-open:任何剥离三头的中间层让保护静默失效;`http://app` 视为与 `https://app` 同源。带 Cookie 的写请求无来源信息应默认拒绝;scheme 一并比较 |
| L3 | `external_url` 非 https 时会话 cookie 无 Secure | `api/auth/header.go:73-89` | TLS 终结反代 + external_url 留空/http 的部署,7 天 cookie 可被 SSL-strip 截获。Secure 由可信请求协议判定(X-Forwarded-Proto 仅信 trusted-proxies) |
| L4 | `patchDataSource` mask 内空值清空已存凭据(与注释矛盾) | `api/v1/instance_convert.go:189-221` | 注释写"空密码不得静默覆盖",实现是无条件赋值;唯一防线在前端。LLM 侧已是"空值不改"。mask 内空串视为"不变",清除单列语义 |
| L5 | 审计脱敏黑名单漏 `sslCa`(实测原文入库) | `component/audit/audit.go:94-118` | `ssl_cert/ssl_key` 已脱敏而 `ssl_ca` 原样;误把带私钥 PEM 粘进 ssl_ca 即永久入账。补 `sslca`;长期改为 proto sensitive 注解 + 值模式扫描 |
| L6 | 设备登录 user_code 经 `name`/`resource` 入永久审计 | `audit.go:132-146`、`auth_service.proto:163,203` | 能读审计者可获他人未完成的码:静默拒绝他人 CLI 登录(DoS)或把批准绑成自己(受害者 CLI 持攻击者会话)。`deviceLogins/*` 资源名脱敏或存内部 ID |
| L7 | LLM masked key 回显首尾各 3 字符 | `api/v1/llm_service.go:306-320` | 短 key 披露比例高,可跨系统匹配;与 OpenLineage mask 策略不一致。统一只露末 4 位 |
| L8 | 全局 BodyLimit 100M + h2c/http.Server 无超时 | `echo_routes.go:27`、`server.go:180-195`(`StartH2CServer`+`&http2.Server{}` 无 IdleTimeout/MaxConcurrentStreams) | 慢速连接可占满连接,大请求可耗内存。按路由收紧;设 IdleTimeout/MaxConcurrentStreams 与整体超时 |
| L9 | 默认构建即 dev profile,localhost:3000 CORS 带凭据且被 CSRF 信任 | `bin/server/cmd/profile_dev.go:1`、`root.go:81,131-134` | `make build` 产物上本地 3000 端口页面可凭据化跨源读写。release 作为默认构建;dev 启动时打印告警;修 Makefile/AGENTS.md 里"wide-open CORS"过时注释 |
| L10 | CORS 通配符匹配与 CSRF 精确匹配不一致 | echo cors.go 通配 vs `csrf.go:74-79` 精确 | 配 `https://*.example.com` 时 sibling 子域可带 Cookie 跨源 GET 读全量(写被 CSRF 拦),与 csrf.go:20 注释矛盾。拒绝通配符或统一匹配函数 |
| L11 | CLI table/进度输出未过滤控制字符 | `cli/output/output.go:166-174`、`lineage.go:216-219,343-348` | 被同步库里含 OSC 52 序列的表名可覆写终端标题/剪贴板。出口剥离 C0/C1 |
| L12 | CLI 接受 http:// 无告警;`--token` 走命令行 | `cli/client/client.go:216-240`、`root.go:117` | token 明文过网/进 shell history 与 /proc。非 loopback http 告警;提示用环境变量/凭据文件 |
| L13 | 审计 CSV 导出无公式注入防护 | `frontend/src/utils/csv.ts:19-27`、`auditLogsCsv.ts:44-66` | `=`/`+`/`-`/`@` 开头单元格在 Excel 触发公式。前置 `'` |
| L14 | OpenLineage 批量入库把 DB 错误原文回客户端 | `api/v1/openlineage_handler.go:172,207` | 单事件路径已脱敏,批量路径不一致。统一只回通用错误 |
| L15 | OpenLineage 字符串字段无长度上限 | `store/openlineage_run.go:317`、唯一 btree 索引上限约 2704B | 超长 job_name 使事件永久失败。摄取时限长返回 400 |
| L16 | 外部数据集 GUID 未转义 `:` | `store/external_dataset.go:34` | 跨 namespace 别名碰撞(身份/血缘串扰)。复用统一 GUID 转义 |
| L17 | `UpdateUser` 静默忽略未知 update_mask 路径 | `user_service.go:402-403`(default 空分支) | 掩蔽客户端 bug、审计对象失真。返回 InvalidArgument(role/group 服务已是) |
| L18 | CreateUser 无密码时 END_USER 永远无法登录 | `user_service.go:198-226` | 产生僵尸账号;与 H1 组合可锁死首个管理员名额。注册必须提供密码或返回初始密码 |
| L19 | MCP 内部错误原文透传给客户端 | `mcp/errors.go:112-119` | store/驱动错误文本(含 host:port、SQL 片段)给到模型并写入可见审计。固定文案 + 细节进服务端日志 |
| L20 | MySQL/mssql 连接参数"值"未转义 | `mysql.go:112-114,141`、`mssql.go:59-63` | 值 `bar&tls=false` 注入参数(实测);mssql 无任何校验。结构化配置 + 白名单 |
| L21 | dial 协议名 `uuid[:8]`(32bit)且从不注销 | `mysql.go:121-125`、`starrocks.go:89-91` | RegisterDialContext 静默覆盖、无 Deregister;生日碰撞会把两个实例的隧道串线(A 的凭据经 B 的隧道出网)+ 长期泄漏。全量 UUID + 用后注销 |

---

## 4. 提示(I)

| # | 发现 | 证据 | 说明 |
| --- | --- | --- | --- |
| I1 | OAuth token 端点换发时不复核审批人状态 | `api/oauth/token.go:87-91` | 只查存在性;设备登录同位置有 `validateApprover`。窗口小(码 TTL 60s)+ `/mcp` 每请求复核 `MemberDeleted`,影响=签出一条立即不可用的凭证 + 假成功审计行。对齐 validateApprover |
| I2 | 四条匿名 OAuth 路由是四个独立限流器,与文档"共享上限"矛盾 | `grpc_routes.go:277-280`、`oauth_endpoints.go:28-33` | `security-posture.md:19` 与 `docs/mcp.md` 写 share one ceiling,实现是 4 份配额(可 4 倍速率)。middleware 只创建一次并复用 |
| I3 | `/oauth/authorize` 与 `/complete` 审计行无 actor | `authorize.go:250-314,320-351` | 发起者/完成者无法归因(批准 RPC 有审计,首尾缺失)。sessionUser 成功后 recordAuditActor |
| I4 | RFC 9728 通配路由 + 503 文案披露待修复设置项 | `grpc_routes.go:272`、`metadata.go:180-182` | 信息价值低(该设置本就匿名可读),记录备查 |
| I5 | 供应链:testcontainers 为直接依赖;CI actions 按 tag 未按 SHA 固定 | `go.mod:29`;`.github/workflows/*` | release 二进制实测不含 docker 包(0 依赖),但 `go build ./...` 门禁会编译 Docker 客户端链路;一次误 import 即进服务端。harness 移独立 module/补 build tag;actions 按 SHA 固定 |
| I6 | LLM Markdown 渲染依赖 beta 库默认 sanitize 策略 | `ExplainSQLPage.vue:265-274`、`markstream-vue@1.0.1-beta.5` | 当前实测安全(标签白名单、URL 消毒),但安全性由传递依赖默认值保证且无回归测试。显式传 safe 策略 + 恶意载荷组件测试 |
| I7 | 杂项:`KILL QUERY` 拼接(当前死代码)、filter options 无分页全表聚合、page token 可伪造 MaxInt32 offset | `mysql.go:185-188`、`openlineage_filter_options.go:32-78`、`api/v1/common.go:130-141` | 保持不接线/入口校验/收紧 offset 上界 |

---

## 5. 设计问题与改进建议

### 5.1 凭据保护:从混淆到加密的升级路径(优先级最高的结构性改进)
现状(已接受决策)是 32 字节 `AUTH_SECRET` 同时担任 JWT HMAC 密钥、凭据 XOR 种子。本轮实测表明其强度**弱于文档表述**:重复密钥 XOR(周期=32)下,同明文→同密文(可跨实例检测口令复用),且**任意可预测前缀即可恢复密钥流**——仅泄漏 `instance.metadata`(拿不到 `setting` 表)就足以用 PEM 头之类已知明文全量还原凭据。建议:
1. AES-256-GCM,密文 `v1:base64(nonce||ct||tag)`,每条独立随机 nonce(消除确定性与已知明文恢复);
2. 密钥分层:DEK 加密数据、KEK 来自 env/KMS,停止 `AUTH_SECRET` 双职(签名与加密分离);
3. 把 `secretFields()` 的"写时混淆/读时解混淆"choke point 换成 `Encrypt/Decrypt`,字段表带类型;
4. 一次性可续跑迁移作业(逐行重加密,记录进度),旧读路径保留一个版本;
5. 密文加版本前缀 + HMAC,解密失败必须报错(当前 `Unobfuscate` 对错误种子只做 XOR 返回乱码,手工改 `setting` 会让全部凭据静默损坏);
6. 修掉 `component/llm/registry.go:59` 仍在说 "AES-GCM decryption" 的过期注释,并把 `api_key_encrypted` 改名为 `api_key_ciphertext`。

### 5.2 限流体系重构
当前限流是"各端点 ad-hoc"模式:Login 按 (email, 裸对端)、设备登录按可信 IP、OAuth 四路由各一份、OpenLineage 无 key 回退 RealIP、Connect 入口与 `/mcp` 完全没有。建议:统一为"全局拦截器 + 按源/按用户双桶"模式,键一律取 trusted-proxy 解析后的真实 IP(修复 M1 后),匿名方法额外加全局桶;账本类写入(审计、OAuth client)统一加背压。

### 5.3 审计体系:从"字段名黑名单"到"数据分类"
- 脱敏只作用于 request/response map 的键名匹配:漏 `sslCa`(L5),`resource`/`status.message`/`parent` 自由文本从不扫描(L6 的成因),`key`/整个 `apiKey` 对象被整段替换又损害可用性;
- 载荷上限两套标准:MCP 8KiB/串 vs Connect 无限(H6 的成因);
- actor 对匿名请求回退到请求自带字段(`audit.go:152-158`),攻击者可自选 actor 文本——建议匿名请求强制记 IP、留空 actor;
- 建议:proto sensitive 注解 + 值模式扫描(PEM 头、`ol_`+64hex、Crockford 码),并把"覆盖全部凭据字段"固化为黄金测试;CSV 导出纳入脱敏回归。

### 5.4 出站连接的策略层(SSH/TLS/LLM/IdP 一揽子)
本轮 4 条与出站相关的发现共享同一根因:出站面没有统一的"超时 + 校验 + 策略"层。建议抽象一个出站连接构造器:统一 Timeout 与 ctx 透传(修掉 NoDeadlineConn)、统一 TLS/SSH 主机校验默认、统一 host 策略选项(allowlist/内网黑名单,至少对 LLM base_url 与连接测试生效)、统一 scheme 校验(`isSafeExternalURL`,后端派生链接、前端 `:href`、CLI 打开浏览器三处共用,见 M10/M13)。

### 5.5 权限机制收敛
- `allow_without_credential` 与 `permission` 互斥应成为被测试钉住的不变量(M8);
- 无注解方法的默认语义是"handler 自己负责"(fail-open),守护测试只能证明"注解存在",证明不了"handler 真的检查了"——把"匿名方法名单"单列并独立断言;
- 用户管理的授权规则内联在 handler(`authorizeCreateUser`)与注解式 ACL 两套并存,建议收敛为单一决策函数;
- `GetPredefinedRole` 返回全局可变 map(进程级改权风险),应返回副本。

### 5.6 账号生命周期水位线
停用/恢复没有"令牌失效时刻"概念:`Resolve` 只比密码变更时间,undelete 后停用前签发的旧 token 恢复可用(M7)。建议 principal 增加 `revoked_at`/`credential_invalid_before` 并在 `Resolve` 比较——它同时是 M4(吊销 LRU)的更稳替代。

---

## 6. 安全技术债务清单

| # | 债务 | 位置/说明 |
| --- | --- | --- |
| D1 | 受限 token(强制改密)只在 ConnectRPC 拦截器生效,`TokenAuthenticator.Resolve` 不检查 restriction | `api/auth/auth.go:159-174,211-221`;OAuth HTTP 端点 `server.go:78-88` 依赖"批准恰好走 Connect"才安全,任何新增非 Connect 入口即成绕过 |
| D2 | 登录限流与吊销缓存均为进程内状态,但未写进 security-posture 的多副本清单 | 多副本下同一账号各得 10 次失败额度;登出吊销只在签发副本生效 |
| D3 | `csrf.go:53` 复制 `auth.AccessTokenCookieName` 字面量 | 任一侧改名使 CSRF 静默失效,无编译期保证;且 server 包无 csrf 测试 |
| D4 | 密码策略默认极弱(MinLength 8、无复杂度、无泄露口令检查),无 MFA/邮箱验证/自助改密 | `server/init.go:50-57`;`UpdateUser` 当前口令校验无限流,持被窃受限 token 者可无限在线猜 |
| D5 | `iat_ns` 自定义亚秒 claim 依赖 jwt 库 1 秒精度的非契约行为 | `api/auth/auth.go:269`;换库/第三方验证器会让"改密即失效"静默失效 |
| D6 | SSO 后端匿名可达而前端按钮禁用("SSO Login (Disabled)") | `LoginPage.vue:156` —— 未在产品路径验证过的可达接口面,接入 UI 或下线 |
| D7 | `InsecureSkipVerify` 家族:idp SkipTlsVerify、db use_ssl 默认不校验、ssh InsecureIgnoreHostKey | 均为开关但无一致告警策略(M22) |
| D8 | 单把 `RuntimeDebug` 闸刀同时控日志级别、pprof/metrics、panic 栈回显 | `setting_service.go:169-176`;缺运维分层开关(M25) |
| D9 | 审计 actor 可被匿名请求自带字段伪造 | `audit.go:152-158`(见 5.3) |
| D10 | MCP token 无 client_id/azp 声明,`mcp/tools/call` 无法精确归因到某次授权 | 事件复盘与"按客户端吊销"都做不到;JWT 加 client_id 即可 |
| D11 | OAuth client 无运维出口:`ListOAuthClients`/`DeleteOAuthClient` 在非测试代码零调用方 | "connected apps"页未实现;灌入几十万客户端后只能手写 SQL 清理;`last_used_at` 只写不读 |
| D12 | `ExtraArgs` 是公开 API 上的"裸 SQL 片段"能力 | `store/meta_resource.go:367`;当前唯一调用点是硬编码常量(安全),但把注入能力留在 API 上,建议受约束构造器 |
| D13 | MySQL 连接参数是"黑名单 + 字符串拼接"三层叠加 | `mysql.go:108-141`(M19/M20/L20 的根);建议结构化 `mysql.Config` + 白名单 |
| D14 | OpenLineage 摄取字段无输入规范 | namespace/job_name/run_id 无长度/字符集校验直进唯一索引(L15);JSON 解析无深度限制 |
| D15 | `audit.go:248` 的 `grpcgateway-x-forwarded-for` 分支是死代码 | grpc-gateway 实际以无前缀 `x-forwarded-for` 转发——证明 XFF 路径未做过端到端验证(M1/M2 的成因) |
| D16 | 唯一一条 IAM 安全不变式只认 `roles/workspaceAdmin` 精确字符串 | `iam_helpers.go:19`;自定义角色携带同等权限不参与"最后管理员"保护 |
| D17 | `UpdateRole` 收缩已绑定角色权限无影响提示 | `role_service.go:98-138`,管理员可无意清空已绑定角色的权限 |

---

## 7. 与 security-posture.md(已接受决策)的关系

以下已接受决策本轮**未发现边界外新问题**,维持原判:单租户工作区授权模型、gRPC reflection 匿名、CLI token 明文落盘(0600)、设备登录/OAuth pending 进程内状态、MCP 面默认关闭与 token 资源绑定、MCP 每调用留审计行(含被拒)、无 refresh token、破坏性 schema 同步仅记日志、`audit_log`/`meta_registry_resource_history` 永久保留。

但以下四条的**副作用或表述**建议更新:

1. **"存储凭据是混淆非加密;拥有数据库读权限可恢复一切"**——实测强度更弱:已知明文前缀(如 PEM 头)即可在**只有 `instance.metadata`**、拿不到 `setting` 表的情形下恢复密钥流全量还原凭据。文档不应再暗示"攻击者必须先拿到 setting 表"。(见 5.1)
2. **"`audit_log` 永不清理"**——该决策的前提是审计行有界;H6/M16/M17 表明匿名/半匿名请求可把它当磁盘炸弹。永久保留与写入背压并不冲突,建议同时声明后者。
3. **"反向代理契约(须归一化/剥离 XFF)"**——`FirstForwardedFor` 取最左值意味着按常规追加语义配置的代理**无法**通过"归一化"修复(M1),且 REST 网关自连接使正确配置也会产生 127.0.0.1(M2)。契约需要在实现层兑现,建议随 M1/M2 修复后更新该节。
4. **"审批重用 approver 会话"**——设备登录正确(签发前后各校验);OAuth 换发端缺同一复核(I1),与设备登录路径不一致。

---

## 8. 修复优先级路线图

**P0(立即,远程可利用/可致接管或数据面失守)**
1. H4/H5:同步 SQL 参数化 + 移除 `multiStatements=true`(目标库任意 SQL 执行/数据外读);
2. H1/H2:未初始化不接受匿名注册;`disallow_signup` 默认 true;setup token 引导;
3. H7:SSH `InsecureIgnoreHostKey` → known_hosts/指纹 + 握手超时 + ctx 透传;
4. H3:SSO 按 IdP subject 绑定;改邮箱要求当前密码;
5. M1/M2:XFF 改取最右未信任段;网关审计 metadata 修正;
6. H6/M16/M17:审计载荷统一上限 + 匿名端点限流(CreateUser/Login/OAuth/OL/`/mcp`)。

**P1(近期,利用条件明确)**
7. M3/M4/M5:限流键改真实 IP + 账号/源双计数;吊销改持久化水位线;源级 CPU 上限;
8. M7:SSO 不自动 undelete + `revoked_at` 水位线;
9. M10/M11:airflow 链接 scheme 白名单 + 统一安全响应头(CSP/frame-ancestors);
10. M12/M13:CLI `CheckRedirect` + Windows 打开器替换 + scheme 校验;
11. M8/M9:acw↔permission 互斥测试 + allUsers 禁绑管理角色。

**P2(中期,加固与一致性)**
12. M6/M14/M15/M18/M19/M20/M21/M22/M23/M24/M25 与 L 系列;
13. 5.3 审计体系(注解驱动脱敏 + 黄金测试)、5.5 权限机制收敛。

**P3(结构性投资)**
14. 5.1 凭据加密升级(含迁移作业与轮换工具);
15. 5.2 限流体系重构、5.4 出站策略层、5.6 账号生命周期水位线;
16. D 系列债务按触碰到的子系统顺带清偿(改到哪清到哪)。

---

## 9. 正面结论(已验证的安全实现,供回归参考)

- **密码学随机**:全部 `crypto/rand`(device code 43×base62≈256bit、user code 8×32=40bit、SSO state、jti、AUTH_SECRET 32B≈190bit、OpenLineage key 32B),无 `math/rand`。
- **JWT**:固定 HS256 + kid=v1 白名单(拒 none/RS 混淆),iss/exp(必需)/aud 全校验;aud 比较在验证器内部,调用方漏不掉;密码修改使 token 失效(跨副本持久状态);MCP token 以 `<external_url>/mcp` 为 aud,RFC 8707 资源绑定完整。
- **OAuth 2.1**:仅 authorization_code;PKCE 强制 S256 且常量时间比对;授权码 256bit、"先消费再校验"防重放;redirect_uri 非 loopback 精确匹配(loopback 仅放宽端口),拒绝 fragment/userinfo/自定义 scheme;错误重定向参数全为服务端常量,`state` 透传 + RFC 9207 `iss`;consent 归属校验(`request.UserID != userID` 拒绝)+ `validateApprover` 双重校验。
- **MCP**:token 经同一 `TokenAuthenticator.Resolve`(吊销缓存、停用、改密复核);scope 由 token 声明;每次调用(含被拒)审计;`tool_guard_test` 用 protoregistry 钉住"工具 Permission == 所包 RPC 注解且属读白名单";工具入参不拼 SQL(`SearchMetadata` 走 `ILIKE $n` + 转义);外层 `http.NewCrossOriginProtection`。
- **权限**:字符串精确匹配(map 查键),无前缀/模糊;多处 fail-closed(user==nil、角色解析出错、CEL 求值失败均拒绝);写方法先 ACL 后查资源(无存在性预言机);预定义角色只读;最后管理员保护含组展开。
- **凭据**:混淆密文不回显任何 API(INPUT_ONLY);编辑空值不改(LLM 侧)、改 base_url 强制换 key(旧 key 不可被转发);OpenLineage key bcrypt + sha256 摘要定位、恒定时间比对、不可枚举;日志无明文凭据(核对了驱动错误文本);`PG_URL` 不进 cmdline。
- **前端/CLI 基本面**:`v-html` 零使用,元数据/LLM 文本全走插值;token 只在 HttpOnly cookie,localStorage 仅存偏好;登录 redirect 仅同源绝对路径;`/device` 用后即从 URL/history 移除 user_code;CLI `config show` 对 token `[REDACTED]`,凭据文件 0600 原子写,exec.Command 仅浏览器打开一处。
- **数据层**:store 层除 LIMIT/OFFSET 数值外全参数化;CEL 过滤列名常量、值全参数;LIKE/GUID 子树有转义;`Manual SQL` 为纯存储/标签/搜索,服务端不执行;分页普遍封顶(1000/5000),血缘图有深度/节点/边/时间预算。
- **误报排除**(子代理主动验证后不成立):MySQL DSN 口令特殊字符注入(ParseDSN 正确解析);`RegisterTLSConfig` 注销时序(sql.Open 已克隆配置);`logout` 可被伪造字符串打爆(先验签)。

---

## 附录:审查来源

| 领域 | 覆盖范围 |
| --- | --- |
| 认证/会话/CSRF/设备登录 | `backend/api/auth/*`、`backend/server/{csrf,echo_routes,grpc_routes,server,init,pprof}.go`、`backend/component/state/*`、`backend/plugin/idp/*`、auth/user/oauth/audit service |
| 授权/IAM | `backend/component/iam/*`、`backend/common/permission/*`、`backend/store/predefined_roles.go`、`backend/api/v1/acl_interceptor*`、全部 proto RPC 注解(全量解析) |
| OAuth 2.1 + MCP | `backend/api/oauth/*`、`backend/mcp/*`、`backend/server/oauth_endpoints.go`、`backend/component/state/oauth_authorization_request.go`、MCP go-sdk v1.8.0 源码 |
| 数据层/注入 | `backend/store/*`、`backend/migrator/*`、`backend/plugin/db/{mysql,pg}/sync.go`、`backend/plugin/{openlineage,lineage}`、go-sql-driver v1.9.3 实测 |
| 凭据/密钥 | `AUTH_SECRET`/`Obfuscate` 全调用链、instance/LLM/OpenLineage 凭据读写、审计脱敏(可执行验证)、日志面 |
| 网络边界/出站 | `backend/component/dbfactory/*`、`backend/plugin/db/*`(含 util/ssh、util/ssl)、`backend/component/llm/*`、idp oauth2、airflow;pgx/go-mssqld/ssh 库源码核对 |
| 前端/CLI | `frontend/src` 全量汇聚点扫描、`cli/*`(含两个 /tmp 复现探针)、`frontend/package.json` |
| 配置/运维 | `backend/config/*`、`backend/bin/server/cmd/*`、限流全景、`backend/runner/maintenance`、CI/Makefile、go.mod 依赖面(`go list -deps` 实测) |

**核实方法说明**:8 个领域子代理(模型 `deepseek-v4.1-flash`)产出发现后,主代理对全部高危与关键中危逐条读取其引用代码复核(约 45 处),全部属实;子代理的 3 项可执行验证(CLI 重定向外泄、MySQL DSN 参数覆盖、审计脱敏/XOR 已知明文恢复)与 2 项主动误报排除亦经代码层面确认。行号以当前工作区为准,后续提交可能使个别行号漂移,请以引用的代码片段定位。

---

## 10. 修复记录

按时间追加。§1–§6 保留审查当时的原文,修复情况以各条目开头的状态指针和本节为准。

### 2026-10-03 —— H4、H5 已修复(commit `8b3ded8`)

**H4 MySQL 同步标识符注入**(`backend/plugin/db/mysql/`)

- 新增 `identifier.go`:`QuoteIdentifier`(反引号加倍转义)、`qualifiedIdentifier`(拼接 `` `db`.`name` ``)。`sync.go` 中 5 处 `SHOW CREATE`(事件、函数、存储过程、视图、表)全部改为转义后拼接,不再把目录名直接填进反引号模板。
- DSN 不再强制 `multiStatements=true`(`mysql.go`);StarRocks/Doris 同样移除(`starrocks/starrocks.go`),并复用同一 `QuoteIdentifier`(`starrocks/definition.go`),删掉包内重复实现。
- 附带:`listPartitionTables` 在 `SHOW CREATE TABLE` 失败后原本继续对 nil `*sql.Rows` 调 `Next()`,会把同步 worker 打成 panic;现改为记录日志并跳过该表。

**H5 PostgreSQL `getViewDependencies` 二次注入**(`backend/plugin/db/pg/sync.go`)

- 查询提为包级常量 `viewDependenciesQuery`,`dependency_ns.nspname` / `dependency_view.relname` 的 `'%s'` 字面量改为 `$1`/`$2`,调用改为 `txn.Query(viewDependenciesQuery, schemaName, viewName)`。

**回归测试**

- 单元:`backend/plugin/db/mysql/identifier_test.go`(覆盖名字含反引号、库名含反引号,以及 `` x`; DROP TABLE t; -- `` 这样的注入样例)、`backend/plugin/db/pg/sync_test.go`(断言该查询使用 `$1`/`$2` 且不含 `%s`)。
- 集成:`TestMySQLSyncEscapesCatalogIdentifiersRealServerIntegration` 在真实 MySQL 上建立名字含反引号的视图与 HASH 分区表,要求同步成功,并由转义后的 `SHOW CREATE TABLE` 产出分区数(`UseDefault`)。反向验证:将两处还原为漏洞写法后该测试确实失败(同步报 MySQL 1064,查询串为 `` SHOW CREATE VIEW `it_app_…`.`v`iew` ``),修复后通过。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`、release 构建、`make test-integration`(PostgreSQL + MySQL 真实服务 + migrator)全部通过。

**仍未处理**:H4/H5 之外的发现(H1–H3、H6–H7、M/L/I/D 系列)按 §8 路线图待办。

### 2026-10-03 —— H3 已修复(commit `bb4cb7e`)

**SSO 身份绑定 IdP subject**(`backend/api/v1/auth_service.go`、`backend/store/principal.go`、`backend/plugin/idp/oauth2/oauth2.go`)

- `principal` 新增 `idp_resource_id` / `idp_subject` 两列与部分唯一索引(`WHERE idp_resource_id <> ''`),增量 `backend/migrator/migration/0.1/0012##principal_idp_binding.sql`;`FieldMapping` 与 `IdentityProviderUserInfo` 各增 `subject` 字段(`proto/store/store/idp.proto`,已 `buf format/lint/generate`),OAuth2 插件把配置的 subject claim 映射进 user info。
- 登录先按 `(idp, subject)` 解析账号:命中即登录,邮箱 claim 只作展示——管理员在平台侧改了邮箱,同一个人下次 SSO 仍解析到同一账号。被停用的绑定账号只返回给登录入口由其按既有逻辑拒绝,不再自动 undelete,也不再改写其组关系。
- 未命中绑定、而该邮箱已有**活跃**账号时拒绝登录(`FailedPrecondition`,提示管理员移走该账号的地址或删除它):既有行没有绑定,服务端无法区分"管理员预置"与"攻击者预占",因此一律不采纳。已软删除的行不参与判定(它不能登录,地址本就视为空闲,与部分唯一邮箱索引同一约定),因此"删掉占位账号"是真正可用的补救路径。
- 首次登录创建账号时写入绑定;provider 配置缺 `fieldMapping.subject`、或 user info 里没有该 claim 时登录失败——没有稳定主体就只能退回可变的邮箱 claim,这正是漏洞的成因。插件构造时进一步拒绝 `fieldMapping.subject == fieldMapping.identifier`(把主体映射回邮箱 claim 等于没绑定)。
- 顺带把同一函数内的两处已列问题一并收紧:SSO 登录不再自动 undelete 被停用账号(该分支随重写移除,与 M7 同源);IdP 组同步只按组资源标识匹配,可变标题不再参与角色授予(注:`FieldMapping.groups` 在 OAuth2 插件里当前没有映射进 user info,`HasGroups` 恒为 false,该路径今日不可达,属提前收紧)。
- **升级注意**:已存在的 `idp` 行没有 `fieldMapping.subject`,升级后其 SSO 登录会以"缺少 subject 映射"失败;需要运维在 `idp.config` 里补上该字段(没有配置 IdP 的 API/UI,本来也只能改库),建议映射 OIDC 的 `sub`。

**改邮箱改为管理员专属**(`backend/api/v1/user_service.go`、`frontend/src/pages/settings/UserManagementPage.vue`)

- `UpdateUser` 的 `email` 路径无条件要求 `metaxisdata.users.update`(本人亦然),与 `password` 路径"改自己要当前密码"并列;成员仍可自助修改自己的 title/phone/password。
- 前端编辑弹窗在无 `users.update` 时禁用邮箱输入框并给出提示,且不把 `email` 放进 update_mask(否则整个请求会被服务端拒绝)。
- 该字段语义写入 `proto/v1/v1/user_service.proto` 注释与 `docs/security-posture.md`。

**回归测试**

- 单元:`backend/plugin/idp/oauth2/oauth2_test.go` 断言 subject 从独立 claim 映射(`identifier=email`、`subject=sub`),缺 `fieldMapping.subject` 时构造失败,`subject == identifier` 的退化配置也被拒绝。(这两条校验后来从插件移到登录服务,只有那里知道工作区名单,见 §10 末条。)
- 集成(真实服务器 + 假 OAuth2 IdP):`TestSSOLoginBindsToTheIdentityProviderSubjectRealServerIntegration` 在测试进程内起 token/userinfo 桩服务、直接向元数据库写入 `idp` 行,覆盖五条:首次登录建立绑定并被 `principal.idp_subject` 记录;他人预占的邮箱不被接管(`FailedPrecondition`,且该行绑定仍为空);管理员删除占位账号后地址释放、SSO 可建立新绑定;重复登录按绑定解析而非邮箱 claim(管理员改邮箱后仍是同一账号与同一 `users/{id}`);被停用账号不被 SSO 复活。
- 集成:`TestUpdateUserEmailRequiresAdminRealServerIntegration` 覆盖成员改自己邮箱 403 且原邮箱仍可登录、成员仍可改自己 title、管理员可改他人邮箱(旧邮箱随即登录失败)、被授予 workspaceAdmin 后可改自己邮箱。
- **反向验证**:临时删除 `UpdateUser` 的 email 权限检查、并让 SSO 重新按邮箱采纳既有账号后,上述两条用例确实失败(`an address taken by someone else is not handed over`、`a member cannot move their own address`);恢复修复后全部通过。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`、`make test-integration`(PostgreSQL + MySQL 真实服务 + migrator 全绿)、release/dev 构建;前端 `biome:check`、`lint`、`i18n`、`type-check`、`test run`(46 files / 304 tests 通过)。

**残余(本轮未处理)**:

1. IdP 的 `email_verified` 仍未校验(H3 证据中列出):主体绑定后邮箱不再决定账号归属,因此它不再构成接管路径;但当 provider 允许未验证的自选邮箱时,"管理员按邮箱给新账号授权"仍可能授给攻击者的新账号。补齐需要一个 claim 映射字段,而当前没有可配置 IdP 的 API/UI,故未在本轮加映射,记录备查。注意:下文(commit `5766be2`)新增的按 provider 可选开关会让这条残余在列入某 provider 后成为**运维显式接受**的风险。
2. provider 侧的 `skip_tls_verify` 与"无条件信任 userinfo 返回的主体"叠加,能中间人的网络对手即可伪造任意 subject 接管已绑定账号(M22/D7 家族)。
3. 没有管理绑定的 API:未绑定账号只能改地址或删除后由 SSO 新建(角色与历史不随之迁移,下文的按 provider 开关为老账号提供了另一条路),`principal_idp_binding` 的解绑/改绑同样只能删号重建或直接改库。
4. 地址被回收再分配、或 provider 允许用户自选/复用地址时,地址身份会把人送进旧账号;列入 provider 时的 UI 警告与本节记录了这一前提。

**仍未处理**:H1/H2/H6/H7 与 M/L/I/D 系列按 §8 路线图待办。

### 2026-10-06 —— H3 补充:按 provider 开启"以邮箱作为 SSO 身份",一人可绑多个 provider

最初的修复要求 provider 给出稳定 subject、未绑定账号一律不采纳,且一个账号只能绑一个 `(provider, subject)`。两处运维现实与之冲突:(a) provider 只暴露邮箱声明,或 SSO 账号早于绑定列存在;(b) 一个工作区可以配置多个 provider,同一个人可能在不止一个里(或整批用户从一个 provider 迁到另一个)——此时第二个 provider 的登录会被 `already linked ... under another subject` 拒绝,换 provider 只能删号重建(丢角色与历史)。本条目按运维选择处理这两点。

**一人多绑定**

- 新增 `principal_idp_binding(principal_id, idp_resource_id, subject)`,主键 `(idp_resource_id, subject)`(一个 subject 即该 provider 的一个人),另加 `principal_id` 索引。表在 [LATEST.sql](backend/migrator/migration/LATEST.sql),读取与写入集中在 [idp_binding.go](backend/store/idp_binding.go)。**迁移文件已合并为一个 `0012##principal_idp_binding.sql`**:它原本是「给 principal 加两列」,后来又加了一个 0013 把绑定搬进表并删列;既然两个版本都未上线,0012 直接重写为最终形态(建表),0013 删除——任何库里都不存在写进那两列的绑定,也不再有这两列。本地已跑过旧 0012 的库属于未上线环境,重建即可。
- 登录先按 `(idp, subject)` 查绑定表,命中即登录;查不到再按邮箱找**活跃**账号(软删除的行不参与,与部分唯一邮箱索引同一约定)。
- **已有绑定的账号再加一个 provider**:只加一条绑定,`account_adopted` 为假、密码不动、先签发的会话不受影响——账号本来就不是靠密码进出的,没有需要作废的东西。**原本没有任何绑定的账号**被接管时才是 adoption:密码置为随机、刷新密码变更时间,前任会话一并失效(`TokenAuthenticator.Resolve` 按 password change time 判废)。
- `BindAccountForSSO` 在**一个事务**里完成:先看该 subject 是否已属于别的账号,再以 `SELECT ... WHERE deleted = FALSE AND type = 'END_USER' AND LOWER(email) = LOWER($3) FOR UPDATE` 复核账号,然后插入绑定并按需作废密码。账号被删除、被改地址、被并发改动,或 subject 已绑到别的账号,一律 `FailedPrecondition` 要求重试,不会按先前读到的行去接管。

**开关按 provider 收敛**

- 设置由工作区级 bool 改为 `sso_email_identity_idps`(重复字段;API 用 `idps/{idp}` 资源名,store 存裸 resource id,`FormatIdentityProviderUID` 负责转换,**已预留旧字段号 9/17**)。只对列出的 provider 生效:同一工作区里另有"信任程度较低"的 provider 时,不会连它一起放行。
- 列出后:`fieldMapping.subject` 变为可选,登录改绑定到小写化的邮箱声明;同邮箱的活跃账号因此可经该 provider 登录(无绑定的账号被接管)。未列出的 provider 保持默认:邮箱不是身份,subject 必需且不得等同于 identifier。
- 仍然拒绝:非 END_USER(避免把服务账号的 API key 交给 provider 送来的任意人);软删除的账号;地址已变或并发改动(见上)。未列出的 provider 仍走"拒绝 + 管理员移地址或删号"的旧路径。

**提示用户**

- 登录响应 `account_adopted`([auth_service.proto](proto/v1/v1/auth_service.proto))为真时,前端登录页弹出"原密码已失效,请以后使用 SSO 登录";同时写 `slog.Warn` 与审计行(Login 带 audit 注解,响应入账)。字段名刻意不含 `password`/`session` 等审计脱敏关键字,否则这次接管会在永久账本里被脱敏掉([audit_test.go](backend/api/v1/audit_test.go) 钉住)。
- **该提示今天到不了终端用户**:SPA 的 SSO 按钮仍禁用、`/oauth/callback` 没有前端路由、CLI 走设备登录(D6),没有任何随包客户端会发起 SSO 登录,`account_adopted` 目前只对手写 REST 客户端可见。真正可靠的信道是运维侧:`slog.Warn` 与审计行记下"哪个账号被哪个 provider 的哪次登录接管"。密码登录失败仍返回统一的"邮箱或密码不正确",避免新增"该邮箱是 SSO 账号"的枚举预言机(与 M5 的 dummy bcrypt 同一考虑);代价是从未走过 SSO 的被接管用户只能从通用报错中看出密码失效。

**独立复核(两轮只读,模型 `deepseek-v4.1-flash`)已修掉的问题**

1. **"失效"不能被后续登录悄悄还原**:原先每次登录把整列 `profile` 从本请求持有的行写回,而 store 用户缓存无 TTL,多副本/并发下会写回旧 profile(含旧密码变更时间),等于复活前任会话。改为 `Store.RecordLastLogin` 用 `jsonb_set` 只写 `lastLoginTime`(键名由 [principal_test.go](backend/store/principal_test.go) 对 protojson 钉住),设备登录同改。
2. **被拒登录不再烧 bcrypt**:随机密码哈希改为真正需要时才生成(接管或建号),避免未认证者用必然被拒的 SSO 请求白耗 CPU(M5 家族)。
3. **错误码**:绑定事务的冲突映射为 `FailedPrecondition`("账号已变更/已被绑定,请重试")而非 `Internal`。
4. **测试不能因错误原因通过**:原"开关关闭时拒绝"的子测试实际死在"配置缺 subject"上;现在每条子测试显式声明自己依赖的 provider 列表,并按列表语义断言。

**测试**

- `TestSSOEmailIdentityAdoptsAnAccountRealServerIntegration`:未列出的 provider 不得接管(且原密码可用);列出的 provider 接管账号(`account_adopted` 为真、地址大小写归一到小写、原密码与旧 token 均失效、绑定表落库);**第二个 provider 为同一账号加一条绑定**(同一 `users/{id}`、`account_adopted` 为假、第一个 provider 签发的 token 仍可用、绑定表两条);新建账号时 `account_adopted` 为假;服务账号不接管。默认值在 `restoreSSOEmailIdentityIDPs` 中断言为空。
- `TestSSOLoginBindsToTheIdentityProviderSubjectRealServerIntegration` 的绑定断言改为读 `principal_idp_binding`。
- 门禁:`gofmt`、`golangci-lint`(0 issues)、`go test -race ./...`、`make test-integration`(真实 PG+MySQL+migrator;migrator 侧覆盖 LATEST.sql 新装、增量链升级、以及「LATEST.sql 与增量链描述同一份 schema」的一致性用例)全绿;前端 `biome:check`/`lint`/`i18n`/`type-check`/`test run`(304 passed)、release 构建通过。

**运维注意**

1. 设置与 `idp` 配置都走进程内 LRU(无 TTL、无跨副本失效),改动后需让其它副本重启才生效(D2 家族的多副本约定)。
2. 地址身份一旦列入某 provider,该 provider 的邮箱被回收再分配给新人时会直接进入旧账号;这正是 UI 红字要求"provider 验证邮箱且同一地址只归属一个人"的原因。
3. 删除 SSO 账号后其绑定仍留在表里,同一 subject 再登录会被判为"已停用"(需管理员 undelete);要彻底改绑,把旧账号删除后由 SSO 以新 subject 新建账号(角色需重新授予)。
4. **把一个 provider 从名单里移除会锁死经它接管过的账号**:这些账号的密码是接管时写入的随机值,而移除后该 provider 的映射若没有可用 subject,登录在解析绑定之前就以"缺少 subject"失败,邮箱路径也关闭(账号有绑定)。补救是管理员给这些账号重设密码,或临时把它加回名单。错误文案已同时点出这两条出路,但"移除即撤销"这一语义要在开启前想清楚。
5. 本次改动的独立复核还指出:`UpdateWorkspaceProfileSetting` 是整份设置的读-改-写(无行锁),若两人同时保存或客户端提交了陈旧的表单,可能把刚被移除的 provider 悄悄加回名单——这是该设置对象既有性质(所有字段共用一次整体保存),不是本次新增;真要收紧需要给设置加 etag/乐观并发。

### 2026-10-06 —— H7 已修复(主修复 commit `b06249d`,独立复核后的加固 commit `73c7015`)

**SSH 主机密钥校验**(`backend/plugin/db/util/ssh.go`)

- 删除 `ssh.InsecureIgnoreHostKey()`。数据源新增 `ssh_host_key`(`proto/store/store/instance.proto` 与 `proto/v1/v1/instance_service.proto` 字段 48;随 `instance.metadata` 持久化,非 INPUT_ONLY,便于管理员回读编辑)。每条一行,接受:
  - `SHA256:...`/`MD5:...` 指纹(容忍 `ssh-keygen -lf` 的尾随注释、大小写与 base64 填充差异;库的 `FingerprintLegacyMD5` 只返回裸摘要,比较侧补 `MD5:` 前缀,该细节写进注释);
  - `known_hosts` 或 `authorized_keys` 公钥行(`ssh-keyscan` 输出);公钥按类型 + 常量时间比较。
- 列表为空即拒绝连接(fail-closed),没有"接受未知主机"的开关;`ssh_host` 未设时仍走直连。
- 信任锚放在数据源而不是服务器文件(known_hosts 文件/`--ssh-known-hosts`):能改 `ssh_host` 的实例写权限持有者同时决定它必须出示的密钥——按既有权限模型,写实例本来就要求 `workspaceAdmin`(可下放给自定义角色),把信任锚留在实例配置上避免让运维登录主机 `ssh-keyscan`;代价是能改实例的人也能改信任锚,这一点已在 `docs/security-posture.md` 中写明。
- 已知主机行的 host 段不参与校验:列表是配置该数据源的管理员对 `ssh_host` 的断言(权限模型下写实例本就是管理员行为);从 known_hosts 文件整段粘贴会连同无关主机的行一起信任,只应粘贴目标堡垒机那一行。`@revoked`/`@cert-authority` 标记显式拒绝而不是静默忽略。

**建链、超时与取消全链路**

- 不再用 `ssh.Dial`:它把 `config.Timeout` 只交给 `net.DialTimeout`,banner 与密钥交换没有 deadline。改为 `net.Dialer.DialContext(ctx)` + `ssh.NewClientConn`,握手 deadline 取 `min(now+SSHTimeout, ctx deadline)`,握手成功后清除;`context.AfterFunc(ctx, conn.Close)` 让调用方放弃时立刻中断进行中的握手。
- 新增 `DialThroughTunnel`:`ssh.Client.DialContext` 打开隧道内连接,调用方的等待取 `min(ctx deadline, SSHTimeout)`,因此即使调用方带着 15 分钟同步 deadline,恶意对端也不能把它拖满整次同步。限的是"等待":被放弃的 channel open 会挂在 `ssh.Client` 上直到隧道关闭(x/crypto 的 `DialContext` 只用 ctx 结束等待,不取消 open),见残余 4。
- MySQL/StarRocks 的 `RegisterDialContext` 闭包不再显式丢弃 ctx;PostgreSQL 的 `DialFunc` 同样。`NoDeadlineConn`(SetDeadline 全 no-op)换成 `DeadlineConn`:deadline 到期关闭隧道 channel 以打断阻塞读写,零值 deadline 取消定时器。这正是 pgx 取消语义依赖的行为——`DeadlineContextWatcherHandler` 正是靠设置 deadline 打断阻塞读。
- 三处驱动的 `Open` 由 `_ context.Context` 改为真实 ctx(`dbfactory` 一直在透传调用方 ctx)。

**连接测试纳入每实例限额**

- `component/state` 新增导出的 `State.AcquireInstanceConnection`/`ErrInstanceConnectionLimit`,`runner/schemasync` 与 `InstanceService.pingDataSource` 共用同一额度;连接测试在额度耗尽时返回 `ResourceExhausted`,不再有"测试连接不受限额"的旁路。

**回归测试**

- 单元 `backend/plugin/db/util/ssh_test.go`(进程内 SSH 服务器,支持 direct-tcpip 转发与"只握手不回应 channel"两种形态):空 `ssh_host_key` 拒绝;正确 SHA256/MD5 指纹(带空行、注释、大小写)与 `known_hosts`/`authorized_keys` 行接受;错误指纹、畸形条目拒绝;只 accept、不说 SSH 的静默主机在注入的 200ms 超时内失败;ctx deadline 与 ctx 取消都能在 5s 内结束握手;`DialThroughTunnel` 端到端透传数据、对不回应的对端在超时内失败;`DeadlineConn` 到期打断阻塞读、清除 deadline 后隧道仍可用。
- 单元 `backend/component/state/resource_limiter_test.go`:同步与连接测试共享同一实例额度。
- 单元 `backend/api/v1/instance_data_source_test.go`:`ssh_host_key` 的 store↔API 双向转换与 update_mask 补丁。
- 集成(真实服务器进程 + 测试进程内 SSH 堡垒机 + 真实 MySQL)`TestSSHTunnelHostKeyRealServerIntegration`:配置了指纹时经隧道连上 MySQL,且指纹随 API 回读;错误指纹在握手阶段被拒(库凭据不过隧道)、返回 `InvalidArgument`;未配置指纹被拒。
- **反向验证**:把回调换回"接受一切" ⇒ `TestGetSSHClientRejectsAnUntrustedHostKey` 变红;去掉握手 deadline ⇒ 静默主机用例挂死并被 20s 超时判失败;把 `DeadlineConn` 还原为 no-op ⇒ deadline 用例挂死。恢复后全绿。

**独立复核后的加固(commit `73c7015`)**

主修复完成后由独立子代理对 `b06249d` 做了对抗式复核(读 x/crypto v0.48.0 与 pgx v5.9.1 源码,并以 `go test -overlay` 探针实测,未改动仓库文件),确认 H7 的核心目标达成(无绕过路径、握手/取消/限额语义正确),同时指出四处缺口,均已修复:

1. **失败的 `Open` 泄漏隧道(中危,实测复现)**:`mysql`/`starrocks` 在隧道已建立后因 DSN 被拒(如 `extra_connection_parameters=timeout=zzz`,现有校验只挡 `allowAllFiles`)而返回,`d.sshClient` 与 `RegisterDialContext` 注册的 dialer 都留在进程里——驱动没被返回就没有 `Close` 可调。现由 `Open` 自己在错误路径 `closeTunnel()`,`Close` 同时 `DeregisterDialContext`。新增跨驱动回归用例 `TestDriverOpenReleasesTheSSHTunnel`(进程内堡垒机统计会话数,覆盖 MySQL 与 StarRocks);反向验证:删掉错误路径的 `closeTunnel()` ⇒ 两个子用例都变红。
2. **SSH agent 回退不受握手上限约束(中危,可挂死)**:`SSH_AUTH_SOCK` 的 unix socket 没有 dial 超时,且 `Signers()` 在认证阶段读取,不受 SSH 连接 deadline 约束——agent 卡住会让握手越过 `SSHTimeout`,同步路径(不取消的 runner ctx)会一直占着实例额度。现 agent socket 用 `DialTimeout` 并以同一 deadline 设限。新增 `TestSSHHandshakeIsBoundedWhenTheAgentStalls`(服务端要求 publickey,client 必须问 agent);反向验证:还原为无超时的 `net.Dial` ⇒ 用例挂死并被 20s 超时判失败。
3. **空摘要指纹被当成合法配置(低危)**:`SHA256:`/`MD5:` 会让配置检查通过、先建立 TCP/SSH 连接再报"host key mismatch"。现空摘要按畸形条目在 dial 前拒绝,并加入畸形条目用例。
4. **连接测试占用额度但无 deadline(低-中危)**:目标接受连接后卡住数据库握手时,实例写权限者可用 10 个并发测试请求占满共享额度、饿死同步。现 `pingDataSource` 以 `dataSourcePingTimeout`(60s)设限——上限的是单次占用时长,不再是无限期。
   - 复核同时指出:`CreateInstance(validate_only)` 的额度键取自请求里的 instance ID 与 `maximum_connections`,未创建的实例没有已存限额可用,因此"新 ID 即新桶"仍成立;这比修复前的"完全不限额"已收紧,但 §10 上一节"不再有旁路"的说法只对已存在实例成立,这里更正。
   - 上一节关于 channel open 的描述也已更正为"限的是调用方的等待"。

**仍未处理**:
1. 没有 SSH 隧道的端到端 **schema 同步** 集成用例:新增集成用例走的是连接测试路径(`ValidateOnly`)。隧道内的实际查询由单元测试与驱动改动覆盖,未做真实 MySQL/PG 全同步用例。
2. 写入数据源时不做 `ssh_host_key` 语法校验,畸形条目在首次连接(或连接测试)时报错。
3. `D7` 的 ssh 项随本修复关闭;`util/ssl.go` 的默认 `InsecureSkipVerify`(M22)与 `SSH_AUTH_SOCK` 回退(M22)仍待处理。
4. **被放弃的 channel open 仍会留在 `ssh.Client` 上**(低危,复核确认):`ssh.Client.DialContext` 只用 ctx 结束等待,不取消 open;每个被放弃的 open 留一个 goroutine 与 mux 条目,直到该驱动的隧道关闭为止。x/crypto 未提供带 ctx 的 open,要么接受(等待已有界、隧道关闭即释放),要么在超时时关闭整个 `ssh.Client`(会连带杀掉该实例其它连接,不做)。
5. **已知主机行的 host 段不参与校验**(低危,复核确认):整段粘贴 known_hosts 会信任无关主机的密钥。这是"管理员可信、信任锚随实例"这一设计选择的直接后果,已在 `security-posture.md` 写明;若日后要收紧,需按 `ssh_host`+端口匹配 host 模式(含 `[host]:port`、通配与哈希行)。
6. **`DeadlineConn` 的语义不是 `net.Conn` 等价**(低危,复核确认):到期关隧道而非返回 `net.Error` 超时,pgx 的 `peekMessage` 因此走 `asyncClose`,被取消的查询会丢弃池中连接并为 `CancelRequest` 再开一条隧道。安全但更重;若要贴近原生语义需要给 `chanConn` 套一层可取消的读。
7. **`chanConn.Close()` 可能阻塞**(低危,复核仅静态确认):channel close 要经 transport 写包,若对端停止读取导致发送缓冲写满,`DeadlineConn` 的关隧道与读取都可能卡住;未能构造出该场景。属 x/crypto `chanConn` 的既有性质。
8. 其余复核未修项:`PortFIFO`/`sshPortSize` 仍是无用死代码;隧道内 MySQL/StarRocks 用裸 `chanConn`,若 DSN 带 `readTimeout`/`writeTimeout` 会因 `SetReadDeadline` 不支持而立即失败(既有行为,M20/L20 家族)。
