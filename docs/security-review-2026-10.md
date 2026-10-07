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
4. `FirstForwardedFor` 改取最右未信任段(M1)——已完成(commit `e9bc802`,含 M2);
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
- **状态**:**已修复**(2026-10-06,commit `4d334d4`;修复内容与验证见 §10)。
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
- **状态**:**已修复**(2026-10-06,commit `e9bc802`;`FirstForwardedFor` 由 `audit.ClientAddress` 取代,从右往左取第一个未信任地址;修复内容与验证见 §10)。
- **证据**:`backend/component/audit/audit.go:247-256`(`strings.Split(value, ",")[0]`)。
- **影响**:主流反代默认按追加语义写 XFF(`<客户端自带值>, <真实对端>`),只要把反代列入 `--trusted-proxies`(文档要求的姿势),取到的仍是客户端自选值。消费方覆盖:审计 IP(`api/v1/audit.go:132`、`openlineage_handler.go:350`、`mcp/server.go:336`、`oauth/audit.go:114`)、设备登录创建限流(`auth_service_device_login.go:49-53`)、OAuth 匿名端点限流(`oauth_endpoints.go:39-48`)。攻击者每请求换一个 XFF 首段即重置限流桶,并把伪造来源写进永不清理的 `audit_log`。
- **修复**:取最右一段,或从右往左跳过信任 CIDR 直到第一个未信任地址。已接受决策"代理须归一化/剥离 XFF"未覆盖"最左/最右"这一实现选择,常规追加配置下无法靠代理修复。

### M2. REST 网关自连接:`/v1/*` 审计 IP 恒为 127.0.0.1,诱导信任 loopback 后客户端可自选审计 IP
- **状态**:**已修复**(2026-10-06,commit `e9bc802`;`/v1/*` 中间件把外层 `RemoteAddr` 记进受证明的 metadata,loopback 不再需要列入 trusted-proxies;修复内容与验证见 §10)。
- **证据**:`backend/server/grpc_routes.go:177-187`(网关以 `fmt.Sprintf(":%d", profile.Port)` 自连接,内层 Peer 恒为 127.0.0.1;grpc-gateway 把外层 XFF 原样前置再追加对端)。
- **攻击场景**:全部 REST 形式请求审计行 IP 失真;运维"修复"自然做法是把 127.0.0.1 加入 `--trusted-proxies`,此后 `FirstForwardedFor` 取的正是客户端自带段(M1),审计 IP 完全可控。
- **修复**:网关路径用外层 RemoteAddr 构造 metadata 或取最右未信任段;文档明确禁止把 127.0.0.1 列入 trusted-proxies。

### M3. 登录失败限流键 = (email, 裸 TCP 对端地址):定向锁死任意邮箱 + 换邮箱绕过
- **状态**:**已修复**(2026-10-06,commit `e9bc802`;账号与源拆成两个独立计数,源取 trusted-proxy 感知的真实地址,并新增 Connect 入口源级粗上限;修复内容与验证见 §10)。
- **证据**:`backend/api/v1/auth_service.go:50-56`、`:92-95`(`loginThrottleKey(request.Email, req.Peer().Addr)`;connect 的 `Peer().Addr` 即 `RemoteAddr`)。
- **攻击场景**:(a) 反代部署下所有客户端对端塌缩为代理 IP,对 `victim@example.com` 连发 10 次错密即锁 5 分钟,循环补发即可无限期阻止其登录(被拒时连密码都不校验);(b) 键按邮箱分桶,每次换不存在邮箱即新桶,对 CPU 消耗(见 M5)无约束;(c) 无源级总上限,密码喷洒不受限。同根因:未配可信代理时设备登录创建限流(10/min/source)退化为全站 10/min。
- **修复**:源维度改用 trusted-proxy 感知的真实 IP(与 audit 同一逻辑),账号维度与源维度拆为两个独立计数;Login 增加源级粗上限。

### M4. 登出吊销缓存是用户可自助刷爆的有界 LRU:已吊销 token 可复活
- **状态**:**已修复**(2026-10-06,commit `e9bc802`;改为按 jti 的持久化 `revoked_token` 表,带过期清理,进程内仅作决策缓存;修复内容与验证见 §10)。
- **证据**:`backend/component/state/state.go:12-14`(`tokenRevocationCapacity = 4096` LRU);`backend/api/v1/auth_service.go:267-283`(`Logout` 仅验签后 `TokenExpireCache.Add`);登录限流只计失败(`login_limiter.go:53-64`),成功登录无限流;每次登录因随机 jti 产生不同 token(`api/auth/auth.go:294-302`)。
- **攻击场景**:攻击者用普通账号登录 4096+ 次并逐个 Logout,把受害者已吊销的 token 淘汰出 LRU——泄露后主动登出/共享机器登出的 7 天 token 重新可用。
- **修复**:吊销判定改为按用户 + `revoked_at`/改密水位线的持久化状态;至少把吊销集合改为带 TTL 且不可被无关主体挤占的结构。

### M5. 匿名 bcrypt 端点无有效限流:CPU 耗尽 + 邮箱枚举
- **状态**:**已修复**(2026-10-06,commit `e9bc802`;Connect 入口给 Login/CreateUser 各加按源 + 全局双桶限额,存在性检查天然排在其后;按选择保留 `AlreadyExists` 文案,枚举速率由限额封顶;修复内容与验证见 §10)。
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
- **状态**:**已修复**(2026-10-06,commit `cb40791`;复核加固 commit `c93fd12`,内容与验证见 §10)。实现比原建议更严:`allUsers` 不是"禁止绑定管理角色",而是彻底不可编辑——只允许留在服务端托管的 `roles/workspaceMember` 基线上,自定义角色只能授给明确的用户与用户组。复核还顺带修掉了相邻的"守卫条件盲"与"不变式只在 API 层"两处(见 §10)。
- **证据**(修复前行号,已被修复改动漂移;`iam_service.go` 的 `allUsers` 分支现位于 `validateIamMember`,`IamPage.vue` 的 allUsers 选项已随修复删除):`backend/api/v1/iam_service.go:142`(`case member == common.AllUsers: return nil`);`backend/api/v1/iam_helpers.go:33-42`;前端提供任意组合入口(`IamPage.vue:147`)。
- **攻击场景**:与 H2 叠加——管理员为"方便"给 allUsers 绑 workspaceAdmin("至少一个活跃管理员"校验通过),此后任何匿名注册者即管理员。单向门,难以察觉。
- **修复**:拒绝 `allUsers` 绑定任何含管理权限的角色;写校验 fail-closed。

### M10. OpenLineage 派生链接无 scheme 校验:`javascript:` 存储型 XSS
- **状态**:**已修复**(2026-10-06,commit `25f2f0b`;服务端派生链接与前端 `:href` 各自做 scheme/host 白名单,修复内容与验证见 §10)。
- **证据**:`backend/plugin/openlineage/airflow_links.go:48-56`(`runLogURL := strings.TrimSpace(facet.TaskInstance.LogURL)`,仅 TrimSpace);`frontend/src/pages/openlineage/OpenLineageRunDetailPage.vue:9` 与 `OpenLineageTaskDetailPage.vue:9` 直接 `:href` 绑定;数据源是持 ingestion key 即可写的原始事件 facet。
- **攻击场景**:被入侵/恶意的 Airflow 提交 `log_url = "javascript:…"`,受害成员在运行详情页点"打开运行日志"即在 SPA 源执行脚本——同源调用 ConnectRPC 全部通过 CSRF 检查,HttpOnly cookie 无济于事,以受害者身份读全部元数据/执行管理员操作。下限也是服务端数据决定的任意链接注入(钓鱼)。
- **修复**:后端派生链接只保留 http/https 且 host 非空;前端 `:href` 统一经 URL 白名单再绑定(与 M14 是同一债务的两个面)。

### M11. 全仓缺失 CSP 与点击劫持防护:`/device` 审批页可被 iframe 劫持
- **状态**:**已修复**(2026-10-06,commit `25f2f0b`;全站中间件统一下发安全响应头,`script-src 'self'` 未放松,修复内容与验证见 §10)。
- **证据**:全仓(排除 node_modules)对 `Content-Security-Policy`/`X-Frame-Options` 0 命中;SPA 由裸 `http.FileServer` 提供(`server_frontend_embed.go:35-45`),echo 中间件无 `middleware.Secure()`。
- **攻击场景**:攻击者自建 device login 拿 user_code,把 `/device?user_code=…` 框进诱导页;external_url 为 https 时 cookie 为 `SameSite=None; Secure`(header.go:84-89),frame 内 cookie 照发、ConnectRPC 同源请求 CSRF 检查通过,诱点"批准"即取得受害者身份的 7 天 CLI token。CSP 同时是 M10 的第二道防线。
- **修复**:统一安全响应头:`default-src 'self'; script-src 'self'; frame-ancestors 'none'` + `X-Frame-Options: DENY` + `X-Content-Type-Options: nosniff`。

### M12. CLI 跨主机重定向时重新附加 Bearer token:7 天凭证外泄(已复现)
- **状态**:**已修复**(2026-10-06,主修复 commit `f3674ec`,两轮独立复核后的加固 commit `2f289cd`、`9f87299`;`CheckRedirect` 拒绝一切离开登记 host 的重定向并保留标准库的 10 跳上限,token 层独立地只对登记 host 附加凭证;修复内容与验证见 §10)。
- **证据**:`cli/client/client.go:282-289`(bearerTransport 见 Authorization 为空即补上——恰是标准库跨主机重定向剥头之后);`client.go:198`(http.Client 无 CheckRedirect)。子代理以独立探针复现:307 跨主机后收集端收到 `Bearer SECRET-TOKEN`,基线(无该 transport)证明标准库本身会剥除。
- **攻击场景**:入口/代理误配跳转、`http://` 部署被改写 Location 或 `--server` 指向不可信服务器时,CLI 自动带 7 天会话 token 跟随到第三方主机。
- **修复**:`CheckRedirect` 非同 host 一律 `http.ErrUseLastResponse`;或 transport 仅在目标 host 与登记 host 一致时附加。(落地为两者都做,并额外拒绝同 host 的 https→http 降级;见 §10)

### M13. Windows 下 `cmd /c start <服务器返回的 URL>` 参数注入:本地命令执行
- **状态**:**已修复**(2026-10-06,主修复 commit `f3674ec`,两轮独立复核后的加固 commit `2f289cd`、`9f87299`;打开前强制 http/https 且必须有主机名,Windows 改用 `rundll32 url.dll,FileProtocolHandler`;修复内容与验证见 §10)。
- **证据**:`cli/cmd/auth.go:227-236`(`case "windows": command, args = "cmd", []string{"/c", "start"}`);URL 完全由服务器响应决定且不询问用户自动打开(`cli/authflow/device.go:104-105,153-159`)。
- **攻击场景**:对攻击者/被入侵服务器执行一次 `mxd auth login`,响应的 `verification_uri_complete` 含 `&` 即逃逸为命令分隔符(`cmd /c start https://evil/x?a&calc.exe`)。登录前无 token,纯受害者场景。
- **修复**:打开前强制 http/https(与 --server 同源更佳);Windows 改 `rundll32 url.dll,FileProtocolHandler`;要求人工确认。(落地了 scheme 白名单与打开器替换;同源限制与人工确认未做,理由见 §10)

### M14. OpenLineage 摄取限流在无 key 时回退 `c.RealIP()`:绕过限流 + 内存堆积
- **状态**:**已修复**(2026-10-06,commit `f9f67fb`;无 key 回退改为 trusted-proxy 解析后的真实地址,OL 与 OAuth 的匿名限流器都改用容量受限的 store——OL 一份、OAuth 每路由一份;对抗复核后的加固 commit `414aa60` 把淘汰改为 O(1),而"四路由合并为一份预算"的改动经集成套件证明会打断既有用例后回退(commit `f440dc8`,I2 未采纳)。修复内容与验证见 §10)。
- **证据**:`backend/server/openlineage_ingestion.go:38-44`(回退 `c.RealIP()`;echo 未设 IPExtractor,RealIP 无条件信任 XFF 最左段);echo RateLimiterMemoryStore 无容量上限(3 分钟清理一次)。同项目 `oauth_endpoints.go:36-48` 已有正确写法并注释说明 RealIP 的问题——属遗漏。
- **攻击场景**:不带 key 的请求每次换 XFF 即新桶,50rps 上限失效,且每次 401 触发一次 `ValidateOpenLineageAPIKey` DB 查询;唯一 key 可在窗口内堆出百万级桶。
- **修复**:回退改用 `c.Request().RemoteAddr` 或设 `e.IPExtractor`;限流 store 加容量上限。

### M15. `CreateSSOState` 匿名无限且 SSO/IdP 出站无防护:SSO 登录 DoS + 免费企业 IdP 探测器
- **状态**:**已修复**(2026-10-06,主修复 commit `f55f62a`,独立复核后的加固 commit `10e803f`;CreateSSOState 的按源/全局预算与 state 缓存容量、IdP 客户端的超时与 ctx 透传见 §10。证据中"Login 的 IdP 分支无限流"一条在本轮之前已不成立:`e9bc802`(M5)的入口预算按 procedure 计额,与走哪个分支无关,故未另加一套专门的 IdP 预算)。
- **证据**:`backend/api/v1/auth_service.go:244-251`(匿名写入 1024 项 LRU 的 state,`state.go:16-20`);Login 的限流只在口令分支(`auth_service.go:92-101`),IdP 分支无限流;`backend/plugin/idp/oauth2/oauth2.go:49-55`(`http.Client` 无 Timeout,UserInfo 不带 ctx)。
- **攻击场景**:匿名刷空 LRU 即可让真实用户的 SSO 回调全部失效("invalid or expired state" 登录 DoS);未认证者可让服务器带着 client_secret 持续出站打企业 IdP。
- **修复**:CreateSSOState 按 IP 限流;IdP client 加 Timeout 并透传 ctx。

### M16. OAuth 注册/授权/MCP 面的写入与内存放大:注册审计无截断、pending 10GB、`/mcp` 无限流
- **状态**:**大部分已修复**(2026-10-06,commit `4e48ea6`;注册/授权/MCP 的写入与内存放大、以及 `/mcp` 的限流与超时见 §10;"审计只记 schema 声明过的字段"未做)。
- **证据**:`backend/api/oauth/register.go:18`、`:125-129`(注册体上限 64KiB,但 `redirect_uris` 无单条限长且审计明细原样落库);`backend/component/state/oauth_authorization_request.go:22`(pending 容量 10000,`state` 参数无长度上限、`RedirectURI` 可达 64KiB,TTL 10 分钟);`backend/server/grpc_routes.go:299-300`(`/mcp` 只有跨源保护,无限流);`backend/mcp/server.go:386-425`(审计只按单字符串 8KiB 截断,2MiB 请求体几乎原样入账本)。
- **攻击场景**:匿名注册以限流速率持续写 ≈0.6MiB/s 的永久审计行 + 客户端行(叠加 XFF 伪造为 4 倍);已登录用户循环 authorize 填满 10000 条(约 10 分钟)占 ≈10GB RSS 并逐出他人 pending;持 MCP token 的成员每次调用写 ≈2MiB 永久审计行。
- **修复**:redirect_uri 限长(2-4KiB)、OAuth 审计明细与 MCP 同标准截断(整行封顶);authorize 的 `state` 设硬上限(如 512B);`/mcp` 加按用户限流与超时;审计只记 schema 声明过的字段。

### M17. User-Agent 未截断进入永久审计与设备登录内存
- **状态**:**已修复**(2026-10-06;UA 上限随 H6 的 commit `4d334d4` 落地,本轮的设备登录端到端验证与地址收紧见 commit `4e48ea6`,细节见 §10)。
- **证据**:`backend/component/audit/audit.go:227-232`(原样取 UA);`backend/api/v1/auth_service_device_login.go:49-67`(pending 记录持有完整 UA 10 分钟,store 容量 10000)。
- **攻击场景**:匿名 Login/CreateUser(audit=true、无限流)以约 1MB 的 UA 无上限写 `audit_log`;设备登录峰值可达数百 MB-1GB 常驻内存。同文件已给 client_name/version 设 100 字节上限,唯独漏了 UA。
- **修复**:`BuildRequestMetadata` 对 UA 截断(如 256B),审计写入侧限制 metadata 字段长度。

### M18. openlineage `raw_payload` 无单事件上限 + 列表无条件全量读取:单请求数十 GB 内存放大
- **状态**:**已修复**(2026-10-07,主修复 commit `a691632`,独立对抗式复核后的加固 commit `db09dac`;修复内容与验证见 §10)。单事件限长与身份字段限长按原建议落地(并一并覆盖 L15 的字段限长),Airflow 链接与数据集引用改为摄取时物化,dataset 列表/详情/筛选项聚合下推 SQL;复核指出的"列表仍读未封顶派生字符串"与"详情展开全部 facet"两处随后一并封顶。加固后列出的三项残余(列表每次请求仍扫全部引用行、过滤列表与详情窗口不一致、详情 summary 仍在全部引用上聚合)已随摄入时维护的逐数据集聚合表修复,见 §10 的 2026-10-07 条目。
- **证据**:`backend/api/v1/openlineage_handler.go:26`(`maxOpenLineageBodySize = 8MiB`);`backend/store/openlineage_run.go:436-461`(列表 SQL 无条件 SELECT raw_payload);`openlineage_service.go:100-111`/`openlineage_dataset.go:22,46-47`(一次读 5000 行)。
- **攻击场景**:持一把摄取 key(50rps)几分钟灌入约 5000 个 8MiB 事件,任意一次 `ListOpenLineageDatasets`(5000×8MiB≈40GB)或 `ListOpenLineageRuns`(一页 1001 行)即 OOM。
- **修复**:单事件限长(如 1MiB)并对 namespace/job_name/run_id 限长;列表不取 raw_payload(Airflow 链接在摄取时物化);dataset 聚合下推 SQL。

### M19. MySQL DSN 由目标库库名拼接:驱动参数覆盖、TLS 降级
- **状态**:**已修复**(2026-10-07,commit `8d574c3`;修复内容与验证见 §10)。
- **证据**:`backend/plugin/db/mysql/mysql.go:141`(`fmt.Sprintf("…/%s?%s", …, ConnectionContext.DatabaseName, …)`);子代理在仓库锁定的 go-sql-driver v1.9.3 上实测:库名 `x?tls=false&` → `dbname=x, tls=false`;`x?allowAllFiles=true&` 同理绕过 `ValidateExtraConnectionParameters` 的键黑名单。
- **攻击场景**:在目标实例建一个带 `?`/`&` 的库名即可改写平台的出站连接参数(TLS 降级 → 中间人可污染同步流量与血缘;`allowAllFiles` 打开本地文件读取面;同步静默指向另一库)。前提:能在目标实例建库。
- **修复**:用 `mysql.Config` 结构化构造(库名进 `DBName` 字段),或对库名做严格白名单(禁 `? & = /`);`ExtraConnectionParameters` 键值白名单。

### M20. PG/MSSQL `extra_connection_parameters` 零校验:连接目标可被改写
- **状态**:**保留**(经确认的决策:该字段的写权限等同于连接配置本身,不构成授权边界;复核结论见 §10)。
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
| I2 | 四条匿名 OAuth 路由是四个独立限流器,与文档"共享上限"矛盾 | `grpc_routes.go:277-280`、`oauth_endpoints.go:28-33` | `security-posture.md:19` 与 `docs/mcp.md` 写 share one ceiling,实现是 4 份配额(可 4 倍速率)。建议是 middleware 只创建一次并复用;**本次未采纳**:合并会把匿名 OAuth 面收紧 4 倍,集成套件(同一进程、同一来源地址打满四条路由)随即以 429 覆盖 404/400/201 断言,故配额维持现状、文档改为按实现描述(commit `f440dc8`,理由与证据见 §10 的 M14 加固第 3 条)。是否共享一份上限属配额决策,留待单独处理 |
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
2. **"`audit_log` 永不清理"**——该决策的前提是审计行有界;H6/M16/M17 表明匿名/半匿名请求可把它当磁盘炸弹。永久保留与写入背压并不冲突,建议同时声明后者。(写入上限已随 H6 修复并在 `security-posture.md` 声明,见 §10;限流与背压仍待办。)
3. **"反向代理契约(须归一化/剥离 XFF)"**——`FirstForwardedFor` 取最左值意味着按常规追加语义配置的代理**无法**通过"归一化"修复(M1),且 REST 网关自连接使正确配置也会产生 127.0.0.1(M2)。**已修复**:改为从右往左取第一个未信任地址(常规追加配置可直接工作),网关自连接用外层 `RemoteAddr` 且只在能与 grpc-gateway 自己追加的末段对上时才采信;`security-posture.md` 该节已随之更新为"追加即可,同机代理仍按普通代理列出(含 127.0.0.1),网关自身那一跳无需列入"(见 §10,commit `e9bc802`)。
4. **"审批重用 approver 会话"**——设备登录正确(签发前后各校验);OAuth 换发端缺同一复核(I1),与设备登录路径不一致。

---

## 8. 修复优先级路线图

**P0(立即,远程可利用/可致接管或数据面失守)**
1. H4/H5:同步 SQL 参数化 + 移除 `multiStatements=true`(目标库任意 SQL 执行/数据外读);
2. H1/H2:未初始化不接受匿名注册;`disallow_signup` 默认 true;setup token 引导;
3. H7:SSH `InsecureIgnoreHostKey` → known_hosts/指纹 + 握手超时 + ctx 透传;
4. H3:SSO 按 IdP subject 绑定;改邮箱要求当前密码;
5. M1/M2:XFF 改取最右未信任段;网关审计 metadata 修正——**已完成**(commit `e9bc802`,见 §10);
6. H6/M16/M17:审计载荷统一上限 + 匿名端点限流(CreateUser/Login/OAuth/OL/`/mcp`)——H6 与 M17 已完成,M16 的注册/授权/MCP 写入与内存放大、`/mcp` 按 principal 限流与超时也已完成(见 §10);其余匿名端点(Login/CreateUser/OL)的按源限流仍待办;

**P1(近期,利用条件明确)**
7. M3/M4/M5:限流键改真实 IP + 账号/源双计数;吊销改持久化水位线;源级 CPU 上限——**已完成**(commit `e9bc802`;吊销实现为按 jti 的持久化表而非账号水位线,M7 的"停用/恢复水位线"仍待办,见 §10);
8. M7:SSO 不自动 undelete + `revoked_at` 水位线;
9. M10/M11:airflow 链接 scheme 白名单 + 统一安全响应头(CSP/frame-ancestors)——**已完成**(commit `25f2f0b`,见 §10);
10. M12/M13:CLI `CheckRedirect` + Windows 打开器替换 + scheme 校验——**已完成**(commit `f3674ec`,加固 `2f289cd`、`9f87299`,见 §10);
11. M8/M9:acw↔permission 互斥测试 + allUsers 禁绑管理角色——M9 已完成(实现为 allUsers 完全不可编辑,见 §10);M8 待办。

**P2(中期,加固与一致性)**
12. M6/M18/M19/M20/M21/M22/M23/M24/M25 与 L 系列——M14 已完成(commit `f9f67fb`,加固 `414aa60`,见 §10)、M15 已完成(commit `f55f62a`,加固 `10e803f`,见 §10)、M18 已完成(commit `a691632`,加固 `db09dac`;三项残余随 ingest 聚合表一并修复,commit `1253a9d`,见 §10)、M19 已完成(commit `8d574c3`,见 §10)、M20 经确认按决策保留(见 §10);
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
### 2026-10-06 —— H6 已修复(commit `4d334d4`)

**审计行的字节上限**(`backend/component/audit/audit.go`)

- 新增 `MaxAuditFieldBytes`(8KiB,与原 MCP `maxAuditArgumentBytes` 同值)、`MaxAuditPayloadBytes`(256KiB)、`MaxUserAgentBytes`(256)。`MarshalAuditMessage` 在脱敏之后先把整个 `raw` map 逐字符串截断,再构造 `structpb.Struct`,最后 `capAuditPayload` 对单条 request/response 封顶:超过 256KiB 时换成 `{"truncated": "payload exceeded 262144 bytes and was dropped"}` 标记。
- **截断必须落在 rune 边界上**(独立复核发现的 Critical,同轮修掉):按字节切片会把多字节字符切成两半,产生非法 UTF-8,而 `structpb.NewStruct` 与 `CreateAuditLog` 的 `protojson.Marshal` 都会直接拒绝它——整行落不了库。于是匿名请求只要在 `user.title` 里放 9000 字节 CJK(或在 `User-Agent` 里放 300 字节 CJK),就能让自己的审计行消失,恰好把 H6 的"必留痕"反过来用。现在 `truncateAuditString` 先用 `strings.ToValidUTF8` 修掉本来就不是 UTF-8 的字节,再用新增的 `common.TruncateUTF8Bytes` 回退到 rune 边界;`MarshalAuditMessage`/`BoundAuditStruct`/`capAuditPayload` 的所有失败分支一律"写标记行",不再返回未截断的原始载荷、也不再让调用方丢掉整行(`MarshalAuditMessage` 因此不再返回 error,`Request`/`Response` 永远是有界且可编码的)。
- 截断刻意放在派生列之前:`resource`/`user`/`parent` 都从同一个 map 取值,因此这三列同样被限在 8KiB。这一点是必需的——此前匿名请求可以用 `user.name` 把 100MB 塞进 `resource` 列,payload 封顶对它无效。`status.message`(handler 用请求字段拼出的错误文本)、`User-Agent`、以及**转发地址**同样有界;转发地址只是让行有界,不改 M1 的"取最左"语义。OAuth 审计行的 `user`(actor)也补上同一处理。
- `BoundAuditStruct` 是跨包入口:MCP 的 tool arguments(`backend/mcp/server.go`)与 OAuth 的审计明细(`backend/api/oauth/audit.go`,此前只做脱敏、不做截断)都改走它,各自重复的实现(`truncateAuditValues`/`maxAuditArgumentBytes`)删除。五处 `CreateAuditLog` 调用中,两处(审计拦截器的流式分支、OpenLineage 摄取)本就不记 request 载荷;记载荷的三处现在都经过同一道闸。
- 超限只丢明细:method/actor/resource/status/latency/requestMetadata 都还在,账本仍能回答"谁在何时调了什么"。代价是响应体超过上限的行(如 `ListAuditLogs` 的整页结果,以及千级 `BatchSyncInstances`)不再逐条留存——那类响应本身就是"把账本再抄一份进账本"。

**ConnectRPC 请求体上限**(`backend/server/grpc_routes.go`)

- `handlerOpts` 增加默认 `WithReadMaxBytes(4MiB)`(echo 的 `BodyLimit("100M")` 降为传输层兜底);UserService 收窄到 64KiB,AuthService 维持 64KiB,gRPC reflection 两个 handler 也显式带上同一上限(此前它们继承 connect 的"无上限")。后给的 option 覆盖先给的(`handlerOptionsOption.applyToHandler` 按序 apply),既有的 AuthService 收窄不受影响。
- 关键在于读取时机:`connect` 在**拦截器链之前**读取请求(`NewUnaryHandler` 先 `receiveUnaryRequest` 再跑拦截器),超限直接回 `ResourceExhausted`,请求既不到 handler、也不产生审计行——这同时覆盖 Connect 与 `/v1/*` REST 网关(网关把大 body 转发过来时同样被拒)。
- UserService 的 64KiB 是被 `BatchGetUsers` 抬上去的:审计页的 CSV 导出会遍历整个账本、用一条 `batchGetUsers` 解析去重后的全部 `users/{id}`,16KiB 会在约 1400 个不同用户时把它打回原形(前端吞掉错误、CSV 静默退化为 UUID)。64KiB 约合 5000+ 个名字,同时仍把匿名 CreateUser 的请求体压到旧上限的 1/1600。`BatchGetUsers` 自身逐名字查询、无条数上限(N+1)仍是遗留问题。
- `/v1/*` 另外套了 `http.MaxBytesHandler(mux, 4MiB)`:REST 形式下网关会先把整个 body 读进来解析再转发,逐 handler 的上限拦不住这段缓冲——H6 的攻击场景正是 REST 的 `POST /v1/users`。

**匿名方法字段长度校验**(`backend/api/v1/user_service.go`)

- `validateEmail` 增加 254 字节上限(RFC 5321 的 forward-path 上限;`mail.ParseAddress` 本身接受任意长地址);新增 `validateUserTitle`(256 字节),`CreateUser` 与 `UpdateUser` 的 title 路径共用。CreateUser 无需凭证,否则一个请求就能把请求体大小的值写进 `principal.name`。
- SSO 按 provider 建号那条路径(`auth_service.go`)是 `principal.name` 的第三个写入方,那里的显示名不能拒绝(否则等于因为名字太长锁号),改用 `clampUserTitle` 在 rune 边界上截断——否则 provider 送来的长名字会让"前端编辑用户时总把 `title` 放进 update_mask"直接 400,账号在 UI 里改不动。

**回归测试**

- 单元(`backend/component/audit/audit_test.go`):字段截断(嵌套对象与数组)、整行封顶标记(64 个满长字段)、`BoundAuditStruct`(MCP 入口)、User-Agent 两个 header 名各截断一次、转发地址截断、`status.message` 截断;**多字节用例**——9000 字节 CJK 的字段/参数/UA/status 都必须保持合法 UTF-8 且行本身能被 `protojson.Marshal` 写出;CJK 字段触发 `MarshalAuditMessage` 的失败分支时返回标记而不是丢行。`common.TruncateUTF8Bytes` 单独钉住(8192 落在 rune 中间时回退,小于一个 rune 时返回空);`backend/api/v1/user_update_mask_test.go` 钉住 email/title 上限与 `clampUserTitle`。
- 集成(`backend/test/integration/runner/audit_payload_service_test.go`,真实服务器 + PostgreSQL 元数据库):匿名 `CreateUser` 带 512KiB title → `ResourceExhausted`,且 `audit_log` 里该请求**一行都没有**;带 12KiB title 的非法请求 → 审计行照写,但 `request.user.title` 恰为 8KiB+`...(truncated)`、整行不超过上限;带 9000 字节 CJK title → 审计行**必须存在**且 title 合法 UTF-8、被截到上限内。
- **反向验证**:单独撤掉 UserService 的 `WithReadMaxBytes` 后第 1 条子用例失败(错误码不是 `ResourceExhausted`、且出现审计行);单独撤掉 `MarshalAuditMessage` 的字段/整行截断后第 2 条子用例失败(title 原样 12KiB);把 `truncateAuditString` 还原成 `value[:maxBytes]` 后第 3 条子用例失败(多字节请求查不到审计行),单元的多字节用例同时报 `utf8.ValidString` 为假。三道闸各自独立生效,验证后均已恢复。

**残余(本轮未处理)**

1. **限流**:审计写入速率、匿名端点按源限流仍属 M5/M16/M24(§8 P0 第 6 项的其余部分)。本轮只让每行有界,匿名请求仍可高频写 16KiB 级的小行;MCP 侧 2MiB 的请求体上限(M16)未收窄。
2. **echo 全局 `BodyLimit("100M")` 未改**:收 body 的每条路由现在都另有上限(`/v1/*` 4MiB、`/mcp` 2MiB、OL 摄取 8MiB、`/oauth/register` 64KiB),但全局值与 L8 提到的 h2c/`http.Server` 超时仍是原样。
3. **`oauth/register` 的 `redirect_uris` 单条限长**(M16)未做,只是整条审计明细现在有界。
4. **254 字节的邮箱上限同时作用于登录**(`auth_service.go` 的 `validateEmailWithDomains`):改造前创建的、超过 254 字节的地址将无法再登录。这类地址本身违反 RFC 5321,只能由当时的客户端造出;若某部署确有此种账号,需要人工改地址。
5. **`BatchGetUsers` 无条数上限**(逐名字一次查询,N+1):本轮只按字节封顶(64KiB),没有收紧条数。
6. H1/H2/H7 与 M/L/I/D 系列按 §8 待办。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`(含改动包的 `-race`)、`make test-integration`(真实 PostgreSQL + MySQL + migrator 全绿)、release 构建(`-tags release`)。本轮未改前端,未跑前端门禁。

### 2026-10-06 —— M16、M17 已修复(commit `4e48ea6`)

**M17:User-Agent 有界,以及 metadata 的其余字段**(`backend/component/audit/audit.go`)

- 这条随 H6 一并解决:`BuildRequestMetadata` 把 UA 截到 256 字节之后,设备登录的 pending 记录(`RequestUserAgent` 直接取自它)与每条审计行的 `requestMetadata.userAgent` 都不可能再是 1MB——匿名 Login/CreateUser 的 UA 炸弹与设备登录侧的常驻内存是同一个值。
- 本轮补两点:转发地址由 8KiB 收到 64 字节(新常量 `MaxIPBytes`——真实地址最长 45 字节,而它是 M1 未修期间由客户端选定的文本,同时进入审计行、按源限流键与 OAuth pending);并加端到端用例:匿名 `CreateDeviceLogin` 带 512KiB UA → `GetDeviceLogin` 读回的值恰为 256 字节 + 截断标记。
- **反向验证**:撤掉 UA 截断后该集成用例失败,读回 524288 字节(`"524288" is not less than or equal to "270"`)。

**M16:OAuth 注册/授权与 MCP 的写入、内存放大**

- **注册**(`backend/api/oauth/authorize.go`):`ValidateRedirectURI` 增加单条 2KiB 上限。此前 64KiB 的注册体可以塞进 10 条近乎任意的 URI,存进客户端行,并被此后每个 pending 请求复制。`MatchesRedirectURI` 在请求侧执行同一上限,否则改造前注册的超长 URI 仍会以请求里那个 1MB 串进入 pending。
- **授权请求**(`parseAuthorizationRequest`):`state` ≤512B,超长直接 `invalid_request` 而不是截断——半个不透明值不是那个值;`code_challenge` 必须恰好 43 字符(S256 的 challenge 就是 verifier 的 SHA-256 base64url,原来的判空检查换成精确长度);**`scope` 先按 256 字节封顶、再折叠成一条**——`RequestedScopesValid` 只要求每项都等于唯一支持的 scope,所以"重复同一个 scope 若干次"原本能构造任意长度的字符串切片(≈1.8MB/条),这正是 M16 的 10GB 场景。
- **pending 内存**:每条 pending 的字段现在各自有界(state 512B、challenge 43B、scope 1 条、redirect URI ≤2KiB、client name ≤200B、地址 ≤64B),容量 10000 这个上限才有意义——此前单个请求可带约 1MB,≈10GB RSS 由此而来。两处容量注释补上了这个前提。
- **`/mcp`**(`backend/server/grpc_routes.go`、`backend/mcp/server.go`):按 principal 限流(600 次/分,全局 6000 次/分,进程内滑动窗口)+ 60 秒超时。限流判定放在身份解析之后、权限检查之前,被拒的调用照旧写审计行(与其它拒绝路径一致);未认证请求没有 principal,不计入预算(它在身份解析处就被拒了,到不了这一步)。
- **MCP 审计明细**:先按 64KiB 封顶参数树(`maxAuditedArgumentBytes`),再走 H6 的 `BoundAuditStruct` 同款整条封顶——端点接受 2MiB,而被预算拒掉的调用同样会带参数,所以超限时只记"带了参数",不记内容。OAuth 注册明细仍走 `BoundAuditStruct`,整条 payload 封顶 256KiB。
- 限流器把原 `DeviceLoginLimiter` 泛化为 `WindowLimiter`(`state/window_limiter.go`;设备登录的创建、查询两个预算与 MCP 预算共用同一实现,没有第二份滑动窗口),`state.MCPCallLimiter` 由 server 注入 MCP 的 `Config.CallLimiter`。计划文档里对旧类型名的引用一并更新。

**回归测试**

- 单元:`backend/api/oauth/handlers_test.go`(state 超限与上限本身、challenge 长度、超长 scope、重复 scope 折叠成一条、超长 state 不回显)、`register_test.go`(redirect URI 超限与恰好等于上限)、`backend/component/state/window_limiter_test.go`(`newMCPCallLimiter` 按 principal 计额、窗口可滚动)、`backend/mcp/tool_test.go`(预算耗尽返回 `resource_exhausted`、被拒调用仍写审计行、未认证调用不消耗额度)、`backend/mcp/audit_test.go`(超出 64KiB 的参数树只记标记)。
- 集成(`backend/test/integration/runner/device_login_bounds_service_test.go`,真实服务器 + PostgreSQL):匿名 `CreateDeviceLogin` 带 512KiB UA,读回有界值。
- **反向验证**:分别单独撤掉 UA 截断、redirect URI 上限、scope 上限/折叠、MCP 预算检查,对应用例各自失败(scope 两项子用例都报 `invalid_scope`/条数为多),随后恢复。
- **未覆盖(已知)**:`echoedClientState` 的调用点只有辅助函数级单测,`AuthorizeHandler` 端到端(Location 头里没有 state)没测(它需要已注册 client + 会话,属 MCP 集成用例的范畴);`/mcp` 的 60 秒超时没有用例(等待 60 秒不现实);生产装配处 `CallLimiter: stateCfg.MCPCallLimiter` 这一行本身没有用例守护(Config 里为 nil 表示不限流,单测用 fake 或 nil)。

**残余(本轮未处理)**

1. **"审计只记 schema 声明过的字段"**未做:审计载荷仍是"字段名脱敏 + 整体封顶",不是按 proto 注解生成的白名单。`/oauth/register` 仍会把全部 `redirect_uris`(最多 10 条 × 2KiB)记进永久账本,单行有界但单源持续注册仍能稳定写行——这是限流问题(见下)。
2. **`/mcp` 只按 principal 限流**:未认证的探测没有 principal,仍可高频打到 token 验证;按源限流属 §5.2 的统一限流重构,并依赖 M1 先把源地址取对。预算只封顶调用速率,不封顶"被拒也写一行"这件事(被拒行现在 ≤1KiB,因为参数不再随超限内容增长)。
3. **两个刻意的 RFC 偏差**(已写进 `security-posture.md`):超长 `state` 被拒后不回显(RFC 6749 §4.1.2.1 要求原样回显,但回显 1MB 会写进 Location 头);超过 64KiB 的参数树在账本里只留标记。
4. 其余同 H6 一节(全局限流、`BatchGetUsers` 条数上限等)。另外:改造前注册的、超过 2KiB 的 redirect URI 现在会被 `MatchesRedirectURI` 拒绝,该 client 需要重新注册。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`(含改动包 `-race`)、`make test-integration`(真实 PostgreSQL + MySQL + migrator 全绿)、release 构建(`-tags release`)。本轮未改前端,未跑前端门禁。

### 2026-10-06 —— M1、M2、M3、M4、M5 已修复(commit `e9bc802`,随后的加固 commit `ab3009f`、`2193f91`)

本轮把"客户端的真实地址"和"被吊销的令牌"两件事都从进程内、可被调用方影响的实现,移到了可验证的实现上;限流键与审计 IP 从此出自同一个解析函数,不会再各说各话。

**M1/M2:转发地址从右往左解析,网关自连接不再丢失外层对端**(`backend/component/audit/audit.go`、`backend/server/grpc_routes.go`)

- 删除 `FirstForwardedFor`(取最左段),新增 `audit.ClientAddress`:把每一行 `X-Forwarded-For` 拆开归一化后,从右往左走,跳过 `--trusted-proxies` 命中的地址,返回第一个未信任地址——每一跳追加的是它看到的地址,所以最右段是最近的可信代理观察到的对端,攻击者前置的段永远走不到。整条链全是代理时退回最左段(最上游)作为最佳归属。非 IP 条目(含把多字节文本塞进头部的尝试)直接跳过而不是记成客户端自选文本;`ip:port`、`[v6]:port`、裸 v6 都归一化后再比较。
- 删掉 `grpcgateway-x-forwarded-for` 死分支(D15):grpc-gateway 以无前缀的 `x-forwarded-for` 转发,该分支从未生效,顺带证明这条链路此前没有端到端验证。
- 网关自连接(M2):`/v1/*` 中间件先 `Del` 再 `Set` `Grpc-Metadata-Metaxisdata-Client-Peer`(外层 `RemoteAddr`),grpc-gateway 把它作为 metadata 转给 Connect 处理器。该戳记单独不可信——直接打到 Connect 处理器的调用方(例如经同机反代、反代原样转发未知头)也能自己写一个,M1 修复后仍会按它取地址。因此中间件同时写 `Grpc-Metadata-Metaxisdata-Gateway-Proof`:对地址做 HMAC,密钥是进程内 `crypto/rand` 生成的随机串,从不外发;`ClientAddress` 只接受能通过校验的戳记,并且只对 loopback 对端做替换。调用方既无法在网关路径上保留自己的值(两个头都被覆写),也无法在直连路径上伪造出 MAC(`2193f91`)。loopback 因此不必列入 `--trusted-proxies`;同机反代仍按普通代理列出自己的地址(含 127.0.0.1),此时网关那一跳解析成反代地址后继续沿 XFF 往左走。戳记与 XFF 条目各自归一化(`ab3009f`),非规范写法的 IPv6 外层地址同样能命中。
- 消费方全部自动受益:审计 IP、设备登录创建限流、OAuth 匿名端点限流、OpenLineage 摄取限流与 `/mcp` 审计。

**M3/M5:登录限流拆成账号/源双计数,Connect 入口为匿名 bcrypt 方法加双桶限额**(`backend/component/state/{login_limiter,window_limiter,state}.go`、`backend/api/v1/throttle_interceptor.go`、`backend/api/v1/auth_service.go`、`backend/server/grpc_routes.go`)

- `LoginLimiter` 由 (email, 裸对端) 单键改为两个独立计数:账号 10 次失败/5 分钟(任意来源累计)、源 20 次失败/5 分钟(任意账号累计)。源取 `audit.ClientAddress` 解析后的地址,与审计行同一个值。**成功登录只清账号计数、不清源计数**:否则一个已知口令就能在共享出口上把自己喷洒失败的额度重置。两个计数共同把"单一来源能锁死多少账号"限制为每窗口至多 2 个(20/10)。
- 新增 `ThrottleInterceptor`,在拦截器链上位于 auth 之后、audit 之前:`AuthService/Login` 120 次/分/源 + 300 次/分全局,`UserService/CreateUser` 20 次/分/源 + 100 次/分全局。源额度刻意放宽(一个 NAT 出口后的整个办公室登录是正常用法),真正限制部署级 bcrypt 开销的是全局额度;集成套件共用 127.0.0.1 作为来源,首轮 30/分/源的取值会让正常登录被拒,这也是把它放宽的直接证据。放在 auth 之后是为了识别已登录调用方;**只有 `CreateUser` 对其豁免**(管理员可能要批量建号),`Login` 即使带着凭证也照常计额——它本就不是正常登录路径,每次请求仍要为请求里的地址花一次 bcrypt,豁免会让"登录 + 登出"循环无限增长 `revoked_token`(`2193f91`)。放在 audit 之前是为了让被预算拒绝的请求不落永久账本行——与 H6 的请求体上限同一种处理。
- `CreateUser` 的邮箱存在性检查天然排在限额之后(拦截器先于 handler),枚举速率被限额封顶;按用户选择保留 `AlreadyExists` 文案,未做模糊响应。
- `WindowLimiter` 构造函数去掉了各调用点都传 `time.Minute`/`4096` 的三对参数(Lint 的 `unparam` 也提示了这一点),统一为包级常量。

**M4:吊销改为按 jti 的持久化表,进程内只作决策缓存**(`backend/migrator/...`、`backend/store/revoked_token.go`、`backend/component/state/revocation_cache.go`、`backend/api/auth/{auth,authenticator}.go`、`backend/api/v1/auth_service.go`、`backend/runner/maintenance/maintenance.go`)

- 新增 `revoked_token(jti PRIMARY KEY, expires_at, revoked_at)` 与 `idx_revoked_token_expires_at`;`migration/0.1/0013##revoked_token.sql` 与 `LATEST.sql` 同步(两者由 migrator 一致性用例钉住)。
- `AccessTokenIdentity` 增加 `TokenID`(jti),`UserStore` 增加 `IsTokenRevoked`;`Logout` 验签通过后按 jti 与 token 自身 `exp` 写表,并在本进程缓存里立即标记为已吊销。写表失败返回 `Internal` 而不是谎报成功。
- `TokenAuthenticator.Resolve` 在验签之后查吊销:`TokenRevocationCache` 命中即用,未命中读表并记入缓存。缓存 TTL 30 秒、容量 8192,淘汰或过期只多一次表读,绝不放行已吊销 token——表是权威。读表失败 fail-closed。`RevokedToken` 的查询是常量并保持 `$1` 参数化,由 `store` 包守卫测试钉住。
- 维护任务按 `expires_at` 清理过期记录:一条记录最多活到它要拒绝的那个 token 过期为止,因此记录量天然有界、也不受任何主体挤占。
- 语义:登出只吊销当前这一个 token(保留单会话登出语义,不是按账号水位线);跨副本通过表可见,进程内缓存使另一副本最多多接受 30 秒,与设备登录/OAuth pending 的多副本约定一致。

**回归测试**

- 单元 `backend/component/audit/audit_test.go`:右起解析(伪造首段被忽略、多跳信任链、全信任链回退、IPv6 归一化、非地址条目跳过、多行头)、网关戳记(带正确 MAC 才采信、伪造/篡改 MAC 或地址被忽略、无戳记保持 loopback)、`StampGatewayPeer` 覆写调用方自带的两个头。
- 单元 `backend/component/state/{login_limiter,window_limiter,revocation_cache}_test.go`:账号/源独立计数、源计数不被成功登录清除、窗口与容量;限额窗口滚动;吊销决策缓存的新鲜度、TTL 与覆写。
- 单元 `backend/api/auth/authenticator_test.go`:持久化吊销被拒;决策缓存只读表一次(计数假实现);本进程吊销立即生效且不再读表。
- 单元 `backend/api/v1/throttle_interceptor_test.go`:Login/CreateUser 各自限额、`CreateUser` 豁免已登录而 `Login` 不豁免、非目标方法放行、可信代理后的真实地址为键、未信任对端的伪造头不能换桶;另有一个 `httptest` + 真实 Connect 处理器用例钉住拦截器确实被 Connect 应用(`WrapUnary`/`Spec().Procedure`/`Peer().Addr`),而不只是 `check` 单独可用。
- 集成(`backend/test/integration/runner/auth_reverse_service_test.go`,真实服务器 + PostgreSQL):`Logout` 后 `revoked_token` 确有该 jti 且 token 被拒;未过期不清理、过期后清理;账号 10 次失败后连正确口令也返回 `ResourceExhausted`,同源另一个账号不受影响;客户端绑定 127.0.0.2 经 REST `/v1/*` 建/读设备登录,`requestIp` 必须是外层对端而不是网关的 loopback(`TestRestGatewayKeepsTheOuterPeerRealServerIntegration`)。
- migrator 集成:`TestMigrateSchemaFreshInstall`、`TestMigrateSchemaUpgrade`、`TestMigrateSchemaLATESTMatchesTheIncrementChain` 全绿。

**残余(本轮未处理)**

1. **账号锁定仍可被单一来源利用**:一个窗口内 10 次失败即锁该账号(被拒时连密码都不校验,与原先一致)。源计数把"每个来源每窗口最多锁几个账号"限为 2,但定向锁死窗口本身是账号计数的固有代价;要消除需要退避/验证码之类的机制,而不是计数。修复前该攻击因对端塌缩成代理地址而被放大(任何来源都能锁),这一放大已消失。
2. **跨副本吊销生效窗口 ≤30 秒**:进程内决策缓存 TTL 决定;单副本部署即时(本进程 `Logout` 直接写入缓存)。与 D2 的多副本清单一致。
3. **`AlreadyExists` 枚举预言机保留**:按选择不改文案,只把速率封顶;严格消除需要一个不暴露存在性的注册响应。
4. **M7 的账号水位线未做**:本次吊销按 jti,不做"停用/恢复水位线";路线图第 8 项仍待办。
5. **限流计数仍是进程内**:`LoginLimiter`、`ThrottleInterceptor` 的预算都是进程本地(D2 家族),多副本下每个副本各有一份额度。
6. **`revoked_token` 的增长由限流约束,而不只由 prune 约束**:每条记录的寿命是它所拒绝的 token 的寿命(默认 7 天);`Login` 计入限额后(`2193f91` 起已登录调用方不再豁免),单个来源最多每分钟多写 120 行、全局 300 行,稳态仍由维护任务清理。
7. **没有 jti 的旧 token 无法登出**:jti 是 `976ebc5` 引入的(审计基线 `1eefc8f` 的祖先)。与当前构建共享 `AUTH_SECRET`、由更早构建签发的仍有效 token 没有 jti:`Resolve` 把它当作永不吊销,而 `Logout` 现在显式返回 `Internal`——即"可用但撤不掉",补救是轮换 `AUTH_SECRET` 或等其过期。当前代码没有不写 jti 的签发路径(`generateToken` 是唯一签发点且必写 `ID`)。

**独立复核后的加固(commit `2193f91`)**

主修复与首轮加固完成后,由独立子代理对 `1eefc8f..HEAD` 做了对抗式只读复核(读 connect-go v1.18.1 与 grpc-gateway v2.28.0 源码,以 `go test -overlay` 探针实测,未改动仓库文件),确认 M1 的右起解析、M3 的双计数、M4 的表持久化与 fail-closed、M5 的拦截器次序与"被拒不落账本"等核心目标无法绕过,同时指出以下问题,均已修复:

1. **网关戳记在直连 Connect 路径上可伪造(中危,已实测)**:原实现把"戳记 == XFF 最后一段"当证明,但该证明只在 `/v1/*` 中间件存在的路径上成立;经同机反代直打 `/metaxisdata.v1.*` 的调用方可以让两者都等于自选值,从而自选审计 IP 与按源限流键。现改为 HMAC 证明(见上),密钥进程内生成、不外发,直连路径无法伪造。
2. **吊销决策缓存可被并发陈旧读覆盖(低危,已实测)**:`Resolve` 先读表拿到"未吊销",期间 `Logout` 写入记录并 `Revoke` 缓存,随后 `Resolve` 的 `Remember(false)` 会覆盖它,使该 token 在下次读表前(≤30 秒)仍被接受。首轮修复给 `Remember` 加了"不降级已缓存 `revoked=true`"的判断,但第二轮的只读复核用并发探针证明该判断本身不是原子的(`hashicorp/golang-lru` 的 `Peek` 与 `Add` 各持一次内部锁,`Revoke` 可以落在两次调用之间),仍会丢吊销。现在 `RevocationCache` 用自己的互斥锁把"读取现有决定 + 写入新决定"做成一步,并加并发用例(2 万次 `Remember(false)` 与 `Revoke` 竞争后必须仍是已吊销)钉住。
3. **空 jti 的 `Logout` 会谎报成功(低危,潜在)**:`RevokeToken` 对空 jti 直接返回 nil,而 `Resolve` 把空 jti 当作永不吊销。所有签发路径都会写 jti,所以只是约定而非强制;`Logout` 现在对空 jti 显式返回 `Internal`,不再出现"报成功但没有可生效的记录"。
4. **已登录调用方可无限"登录 + 登出"增长吊销表(低危)**:拦截器原先豁免所有已登录调用方,持账号者可以绕过 `Login` 预算,以 bcrypt 速度持续写入寿命 7 天的记录。改为只有 `CreateUser` 豁免已登录调用方,`Login` 始终计额。
5. **全信任链回退取最左段(低危,设计如此)**:复核确认 `forwardedClient` 在"每一跳都在 `--trusted-proxies` 内"时返回最左(最上游)地址;这与 M1 取法一致,只有在客户端地址本身落在信任 CIDR 内时才可能有影响,已在 `security-posture.md` 写明。
6. **文档表述过强(提示)**:`security-posture.md` 原写"调用方无法让两个值按其意愿相等",在直连路径上不成立;随 HMAC 改造改为"只有本进程的中间件能产生该证明"。
7. **在飞的 M2 端到端用例本身是坏的(复核发现)**:`GET /v1/deviceLogins/{code}` 不是匿名方法,原用例在 GET 上拿到 401,断言根本走不到 `requestIp`。已改为带管理员 token 读取;该用例现在把客户端绑定到 127.0.0.2,使外层对端与网关自身的 loopback 连接不同,能真正区分"取到外层对端"与"取到 127.0.0.1"。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`(改动包 `-race`)、`go vet`、release 构建(`-tags release`)、`make test-integration`(真实 PostgreSQL + MySQL,含 migrator 的 LATEST 新装/增量升级/一致性与 53 个顶层 `RealServerIntegration` 用例全绿)。本轮未改前端,未跑前端门禁。

### 2026-10-06 —— M9 已修复(commit `cb40791`)

**allUsers 不再是可编辑的成员**(`backend/store/policy.go`、`backend/api/v1/iam_service.go`、前端 `IamPage.vue`)

- 新增 `store.CheckAllUsersBinding`,对整份策略断言两件事:
  1. `allUsers` 只允许出现在 `roles/workspaceMember` 这条绑定上。它匹配每一个已认证主体——包括此后注册的所有人,而自助注册默认开启(H2),所以把它绑到别的角色(预定义的 `workspaceAdmin`,或管理员刚建的自定义角色)等于一次看似普通的授权就把该角色发给未来所有注册者;"至少一个活跃管理员"守卫还会把 allUsers 当成管理员,让这次写入同时通过最后管理员校验。
  2. 策略必须保留 `roles/workspaceMember` → `allUsers` 这条绑定(首次初始化时由服务端写入的隐式基线)。丢掉它直接拒绝,而不是静默存下一份缺少它的策略。
- 判定放在 **store 包**,v1 写路径(`validateIamPolicy`)与 store 的两个写入器(`SetWorkspaceIamPolicy`、事务内的 `patchWorkspaceIamPolicyImpl`)都调用它:前者的报错走 `InvalidArgument`,后者是"任何调用方都绕不过"的兜底。增量 patch 只强制第 1 条——它可能跑在"基线绑定早于改造就丢了"的库上,拿第 2 条去卡会把首个管理员引导弄失败(见复核加固第 2 条)。
- 为什么不是维护一份"管理权限清单":任何清单都会在新增权限的那天过期;在这个模型里"超出 workspaceMember 基线"与"含管理权限"是同一件事,且不依赖人手同步。自定义角色照常可用,只是只能授给明确的用户与用户组。
- `hasActiveWorkspaceAdmin` 的 `allUsers` 分支**保留**:改造前写入的策略可能仍带 `allUsers` → `workspaceAdmin`,权限引擎仍按它放权;该分支让守卫的判定与引擎一致,去掉它只会让 `DeleteUser` 对这类部署过度拒绝(工作区不会因此锁死——Set 路径上带该绑定的提交本来就先被第 1 条拒掉)。新写路径会拒绝再次写入该绑定,所以它是一次性待清理的历史遗留(下一次保存策略必须先删掉它)。
- 前端:成员类型选择器去掉 allUsers;`allUsers` 基线行显示为只读(没有"移除成员""移除绑定",该成员自身也没有 ×),并在可编辑草稿里自动补上缺失的基线绑定(`frontend/src/utils/iamPolicy.ts`),使改造前丢过该绑定的库能被一次保存修好,而不是卡在"服务端拒绝、界面又补不回来"。
- `proto/v1/v1/iam_service.proto` 的 `Binding.members` 注释写明该约定(生成物随 `buf generate` 一并提交),`docs/security-posture.md` 增补一条"allUsers 由服务端托管、只承载成员基线"。

**回归测试**

- 单元 `backend/store/policy_all_users_test.go`(`TestCheckAllUsersBinding`,9 例):基线与其它绑定共存、基线行带额外成员、重复基线行均通过;allUsers 绑 `roles/workspaceAdmin`、绑自定义角色、策略缺少基线绑定、空策略均被拒且错误文案指明原因。另一例钉住增量 patch 用的 `checkAllUsersRole` 允许缺基线、但仍拒绝 allUsers 越界。检查函数是纯函数,不需要数据库。
- 集成 `TestWorkspaceIamPolicyRealServerIntegration`(真实服务器 + PostgreSQL):首次初始化确实写入了 allUsers 基线绑定;`allUsers` 绑 `workspaceAdmin` 与绑新建自定义角色都在写路径上被拒;缺少基线绑定的策略被拒;带恒假 condition 的额外管理员绑定不改变"仍有真实管理员"的判定(写入被接受),但没有无 condition 的管理员时写入被拒;"没有管理员"用例改为携带基线绑定,因此确实落在最后管理员守卫上(断言错误文案)。被拒写入之后用整份策略的 `proto.Equal` 比对,证明没有部分写入。
- 前端 `frontend/src/utils/iamPolicy.test.ts`:基线行识别与草稿补全(追加整行 / 给已有的 workspaceMember 行补成员 / 已存在时原样返回 / 已有 allUsers 时不补第二条只读行)。
- **反向验证**:临时删掉 `validateIamPolicy` 里的校验调用后重跑集成用例,`allUsers onto roles/workspaceAdmin must be refused` 变红(`expected: 0x3`,即 `InvalidArgument`;实得 `0`);恢复后全绿。

**残余**

1. **改造前已存的 `allUsers` → `workspaceAdmin`/自定义角色绑定不会被自动清理**:它仍在放权,直到管理员保存一次策略(保存前必须先删掉它)。要做一次性启动修复/迁移是另一个决定,本轮没做。
2. **"全员多一项权限"不能再走 allUsers**:要么改基线代码(`backend/store/predefined_roles.go`,等价于"所有未来注册者的默认权限"),要么把角色绑给一个由 IdP 持续补员的组——后者是显式的管理员决定,不是本条的漏洞。
3. 除 `SetWorkspaceIamPolicy` 外,store 的增量写入器 `patchWorkspaceIamPolicyImpl`(首次初始化,以及 `backend/store/principal.go` 的首个用户授管理员)也只强制第 1 条,不强制"策略必须保留基线绑定";两者都不会用 allUsers 作成员,所以今天不存在绕过第 1 条的路径。
4. D16 仍在:自定义角色携带与 `workspaceAdmin` 同等权限时不参与"最后管理员"保护。

**独立复核后的加固(commit `c93fd12`)**

M9 落地后由独立子代理对 `cb40791` 做了对抗式只读复核(读 connect-go/grpc-gateway 调用链,`go test -overlay` 探针全部挂在 `/tmp`,未改动仓库文件)。结论:两条 allUsers 规则没有被任何路径绕过——角色串大小写/尾随空格/空 role、成员拼写 `allusers`、组里塞 allUsers、自定义角色 id 撞 `workspaceMember`、重复绑定、并发/etag、以及"常量比较 vs 解析快照"都被逐一实测拒绝或证明不可利用。但复核实测出以下问题,本轮已一并修掉:

1. **"最后管理员"守卫是条件盲的(中危偏高,已实测复现)**:`hasActiveWorkspaceAdmin` 只按 `role + member` 判定,从不求值绑定的 `condition`;而 `validateIamPolicy` 只要求 condition **可求值**、不要求为真。给唯一的 `workspaceAdmin` 绑定加 `request.time > timestamp("2100-01-01")` 后,写入被接受(守卫仍把它算作管理员),读路径(`utils.GetUserIAMPolicyBindings` 求值 condition)却把它过滤掉,调用者随即失去 `iam.setPolicy`,连 `GetWorkspaceIamPolicy` 都返回 `permission_denied`——**一次合法 Set 就把工作区永久锁死**,只能改库恢复。修复后守卫与权限检查同一语义:condition 求值为假或求值失败的绑定不计入管理员,且必须有一条**无 condition** 的管理员绑定,否则拒绝,错误为 `workspace must keep at least one active admin whose binding has no condition`(第二条同时挡掉"只留定时管理员、到期后再锁死"的延迟路径)。`DeleteUser` 复用同一守卫,因此"恒假的绑定被当成幸存管理员、放行删除最后一个真实管理员"这条同根因路径也一并关闭。
2. **不变式只在 v1 API 层(低-中,潜伏)**:store 的共享写入器 `patchIamPolicyBindings`/`patchWorkspaceIamPolicyImpl` 完全不校验,而它被首次初始化(`server/init.go`)与"首个 END_USER 即管理员"(`store/principal.go`)调用;以 `allUsers` 调用它会原样落库(探针输出 `roles/workspaceAdmin:[users/1 allUsers] roles/workspaceMember:[]`)。今日没有 RPC/CLI 这样调用,故不可利用,但"服务端托管"在 store 层并不成立。现 `CheckAllUsersBinding` 移到 store 包,由 v1 写路径与两个写入器共同调用;增量 patch 只强制角色那一条,以免"历史库缺基线绑定"时卡死首个管理员引导。
3. **前端重复 role 时的只读死锁(低,`cb40791` 自身引入)**:服务端接受重复 role 绑定,而草稿补全取**第一条** `roles/workspaceMember`;若存量策略是"第一条无 allUsers、第二条有",补全会把两条都变成只读基线行,且表格 `:key="binding.role"` 重复。现在补全前先判断"已有 allUsers 绑定",行 key 改为 `role#index`。
4. **文档更正**:`security-posture.md` 那句 "(it cannot be produced from the UI at all)" 与同句描述的草稿补全自相矛盾(已改);"删掉 `allUsers` 分支会让部署锁死"不成立——Set 路径上带该绑定的提交本就先被第 1 条拒掉,去掉分支只会让 `DeleteUser` 过度拒绝(已改);残余第 3 条漏掉了 `store/principal.go` 这个绕过校验的写入者(已补全)。
5. **测试更正(复核指出,已改)**:被拒写入的"未落库"断言原先比较 etag,而 etag 是毫秒精度(`store/policy_test.go` 明确钉住该性质),同一毫秒内可漏判;现改为整份策略 `proto.Equal`。复核同时确认新增断言的 `wantErr` 子串足够具体,回退实现会红。
6. **顺带记录(未改,与本条无关)**:`GetPolicy` 的缓存命中要求 `Resource != nil`,而 `GetWorkspaceIamPolicy` 传的 `Resource` 为 nil,于是工作区策略缓存只写不读,`SetWorkspaceIamPolicy` 里 "a cached etag could be stale" 的注释名不副实(它本来就是强读);改前既有。

**复核后仍未做**:store 的 patch 路径没有独立的 DB 级用例证明它会拒绝 `allUsers`(该路径没有 RPC 入口);判定函数有单测,接线由全量集成套件覆盖(初始化与首用户授权若被误拒,环境起不来)。另外本轮观察到一次全量并发运行里 `TestAnalyzeSQLFromListedGUIDRealServerIntegration` 在 `ListDatabases` 里拿不到自己刚同步的库(下一次全量 53/53 绿),与本分支改动无关,但说明共享 env 的并行隔离仍有脆弱点。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./...`(改动包 `-race`)、`go vet -tags=integration`、release 与 dev 构建、`make test-integration` 的两条命令(真实 PostgreSQL + MySQL 容器 + migrator;`RealServerIntegration` 53 用例全绿)与本节新增/改写的 IAM 用例;前端 `biome:check`、`lint`、`i18n`、`type-check`、`test run`(47 文件 / 310 用例)、`test:coverage`(新增 `iamPolicy.ts` 全 100%)。

### 2026-10-06 —— M12、M13 已修复(commit `f3674ec`)

**M12:CLI 只把 Bearer token 发给登记的服务器**(`cli/client/client.go`)

- `normalizeServer` 改为返回 `*url.URL`(校验逻辑原样搬过来:scheme 白名单、host 非空、无 userinfo/query/fragment、去尾随斜杠;裸 `?` 的判定是后来加固时补的,见下),client 由此拿到登记的 host/scheme,并加装两处判定:
  - `http.Client.CheckRedirect`:目标 host 与登记 host 不一致即返回错误、**不跟随**;登记为 https 时同 host 的 https→http 降级同样拒绝。错误文案给出重定向前后的两个 host,直接可诊断。
  - `bearerTransport.carriesCredentials`:只有目标与登记服务器同 host、且 scheme 相同(或登记为 http、目标升级为 https)时才补 `Authorization`。
- 两道闸都以**精确 host** 为准,而不是标准库的"同域"判定:`net/http` 只在重定向离开服务器**域**时剥 `Authorization`,并且把子域当作同域(`isDomainOrSubdomain`);而本项目的 transport 每一跳都把 token 补回去(它同时是 CLI 免除 cookie CSRF 的手段),所以标准库那套判断在这里既不充分,也不能作为"跟随"的依据。
- 为什么选"直接报错"而不是"跟随但剥头":CLI 只服务一个服务器,离开它的重定向必然是入口误配或攻击;立即失败连匿名请求都不发出(不泄露路径/方法/请求体),错误也直接指向配置;而"跟随到第三方再拿 401"既泄露了请求本身,又会被读成凭证过期。
- 顺带把 token 层与重定向策略统一到同一个"登记的服务器"概念:同 host 的 http→https 升级保留 token(反代终止 TLS 的正常姿势),https→http 不保留。

**M13:打开浏览器前校验 scheme,Windows 不再经 cmd.exe**(`cli/cmd/auth.go`)

- 新增 `validateBrowserURL`:只接受 scheme ∈ {http, https} 且 host 非空的地址;`javascript:`、`file:`、`cmd:` 与不可解析的地址(`https://`、`https://mx.example.com/%zz`、空串)一律拒绝。校验发生在 `exec` 之前,`authflow.Run` 既有的 "Could not open a browser automatically: %v" 分支把它打印出来;URL 本来就已经打印给用户,人工确认流程不受影响。
- Windows 打开器由 `cmd /c start <url>` 改为 `rundll32 url.dll,FileProtocolHandler <url>`:前者把地址交给 Windows 命令行解析,服务器返回的 `https://evil/x?a&calc.exe` 会以 `&` 起第二个程序;后者按 URL 协议注册表打开,地址自始至终是单个 argv 元素。
- OS 分支抽成 `browserCommand(goos string)`,使 Windows 分支在任何平台上都能被用例钉住(此前该分支只在 Windows 上可达,无法测试)。

**回归测试**(`cli/client/client_test.go`、`cli/cmd/auth_test.go`)

- M12:表驱动断言 token 只附加给登记服务器(登记服务器带;子域、异 host、同 host 降级、同 host 异端口都不带;同 host 的 http→https 升级带);`httptest` 起"登记服务器 307 → 第三方",用真实 Connect 客户端调用,断言调用失败**且第三方一个请求都没收到**;同 host 重定向(307 到 `/redirected`)必须仍被跟随并带上 `Bearer SECRET-TOKEN`;同 host 的 https→http 重定向必须报 `downgrade`。
- M13:`browserCommand` 三分支(darwin/windows/linux)的命令与参数;`https://evil.example.com/x?a&calc.exe` 的 argv 断言(rundll32 + `url.dll,FileProtocolHandler` + 地址单元素、不含 `/c`);`validateBrowserURL` 的接受/拒绝表;`openBrowser` 对 `javascript:`/`file:` 在启动任何进程前返回错误。
- **反向验证**(逐条单独回退后对应用例变红,随后恢复):去掉 `CheckRedirect` 的跨 host 判定 ⇒ 第三方收到请求、用例失败;去掉 `carriesCredentials` 的 host 判定 ⇒ 表驱动用例失败;Windows 分支还原为 `cmd /c start` ⇒ 打开器用例失败;关闭 scheme 校验 ⇒ 校验表用例失败。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues,两轮)、`go vet ./cli/...`、`go test ./...`(47 个包 ok)、`go build -ldflags "-w -s" -p=16 -o ./build/mxd ./cli`(与 `make build-cli` 同参数)。本轮未改 backend 与前端,未跑这两侧门禁。加固提交 `2f289cd` 后重跑同一组:`gofmt`、`go vet ./cli/...`、`golangci-lint`(0 issues)、`go test ./...`(47 包 ok)、`go test -race -count=3 ./cli/...`、`make build-cli` 全绿。

**独立复核后的加固(commit `2f289cd`)**

主修复落地后由独立子代理对 `f3674ec` 做对抗式只读复核(overlay 探针挂在 `/tmp`,未改动仓库文件)。复核确认 M12 的两道闸与 M13 的打开器都无可复现的绕过:`CheckRedirect`、`carriesCredentials` 与 `http.Transport` 读的是同一个 `req.URL`(connect 初始即设 `Host: url.Host` 且从不写 Host 头,不存在解析差分);host 变体(大小写、尾点、IPv6 展开、IDN、显式默认端口、空 host、相对地址)全部 fail-closed;`cli/` 内除 `client.New` 外没有第二处 HTTP 或 `Authorization`,也没有第二处 `exec.Command`(`--token` 与 `--service-account` 共用同一个 `*http.Client`);Windows 下 `os/exec` 走 `CreateProcess` + `makeCmdLine`,不经 shell,URL 是单个 argv 元素。但也实测出下列问题,均已修:

1. **自定义 `CheckRedirect` 顶掉了标准库的 10 跳上限(中高,已复现)**:`net/http` 的 `defaultCheckRedirect` 在 `len(via) >= 10` 时报错,而一旦自行设置 `CheckRedirect`,该保护随之消失。同 host 自环重定向因此被无限跟随——实测 3179 跳/秒、3 秒堆 +32MB;默认 `--timeout 30s` 把它压在约 9 万跳/数百 MB,`--timeout 0` 时无界。这是 `f3674ec` 引入的回归。现在策略保留同一上限(`maxRedirects = 10`),并新增"自环服务器必须报 `stopped after 10 redirects` 且服务器实收恰好 10 个请求"的用例(用例本身在第二轮复核后收紧,见下)。
2. **裸 `?` 通过 query 校验(低,已复现)**:`url.Parse("https://h/x?")` 的 `RawQuery` 为空、`ForceQuery` 为真,校验只看 `RawQuery`;改用 `.String()` 后会把这个 `?` 还原进 base URL,Connect 的过程名随之落进 query 串(实测线上 path=`/x`、query=`/metaxisdata.v1.AuthService/Login`),而旧的字符串拼接恰好丢掉了它——同一改动引入的小回归。现在 `ForceQuery` 一并拒绝,并加入拒绝用例。
3. **`validateBrowserURL` 的 host 判据比文档措辞宽松(提示,已收紧)**:`https://:8080/x` 的 `Host` 是 `:8080`(没有主机名)却被接受,现改判 `Hostname() != ""`。不构成注入(无 shell、地址始终是单个 argv),属与文档"with a host"的一致性。
4. **"第三方零请求"断言在单守卫回退时不可达(测试质量,已改)**:原用例先断言错误文案、后检查泄露,而 `require` 失败即 `FailNow`,回退 host 守卫时失败点落在文案上;且第三方是 http 服务器,https→http 降级规则会先一步拦住请求,泄露断言根本走不到。现在先做泄露检查,并把第三方改成与入口同样的 TLS,使 host 规则成为唯一起作用的判定。逐条回退验证:去掉 host 守卫 ⇒ 报"第三方收到请求(Authorization 为空)";同时去掉 token 层 host 绑定 ⇒ 报"第三方收到 `Bearer SECRET-TOKEN`"——两道闸各自独立可见。
5. **复核确认但未改(记录备查)**:`https://mx.example.com@evil.example.com/` 打开的是 `evil.example.com`,属已记录的"服务器可命名任意 http(s) 页面"残余;`HTTP_PROXY` 下 http 目标的代理能看到 token(与 URL host 绑定无关,https 走 CONNECT);LLM 出站路径(`backend/component/llm/agent.go` 无 `CheckRedirect`)是 M23 的同一根因,不在本轮范围;`xdg-open` 里 `$(printf …)` 的求值经实测**不构成**命令注入(控制操作符不在展开结果中重解析),不作漏洞处理。

**第二轮对抗式复核后的加固(commit `9f87299`)**

由另一个独立子代理对**累计 diff**(`4027027..e0192c3`)再做一轮对抗式只读复核(同样只有 overlay 探针挂在 `/tmp`)。它推不翻核心结论:跳数上限与标准库 `defaultCheckRedirect` 完全等价(`via` 含初始请求、`>=`、服务器实收 10 个请求、文案一字不差);上限放在 host 判定之前不漏包(边界实测 `offHostHits=0`);`%3F`/`%3f` 既不会绕过 query 校验也不会被还原成 query(线上 `RequestURI` 里仍是 `%3F`,过程名仍在 path);HTTP/2、`--timeout 0`、`--token`、`--service-account`、`CACert` 与重定向策略无交互;connect-go 对 401/503/429/300/305 一律不跟随 `Location`;Windows 下 URL 是单个 argv,不经 shell。但它指出以下**测试保证**与收尾问题,均已修:

1. **上限用例只证明了"存在某个上限"(中,已复现)**:去掉上限后它确实变红,但红在 `Timeout: 5s` 的超时(耗时 5.01s、失败信息与重定向无关),且把 `maxRedirects` 改成 3 或 1000 仍然通过——文档声称的"10 跳"没有被钉住。现在用例统计服务器收到的请求数并要求 `stopped after 10 redirects`:10 个请求到达、第 11 个不发(实测改成 3、1000、或去掉上限都会立刻变红)。
2. **降级用例断言的是文案而不是安全属性(低,已复现)**:回退降级判定后,失败原因是入口 TLS 监听对明文请求回了 `400 Bad Request`,用例从未断言"没有第二次连接发出"。现在用计数 listener 断言入口只接受过 1 条 TCP 连接(若跟随降级,同 host:port 会多出一条明文连接),且该断言排在文案断言之前——回退后失败点落在连接计数上。
3. **新错误文案未转义调用方可控文本(低,已修)**:`net/url` 拒绝 C0 控制字符,但接受 C1(如 U+009B)与双向控制字符,而错误消息用 `%s` 打印 `req.URL.Host`。改为 `%q`,新增用例钉住(回退成 `%s` 即红)。记录:`authflow` 本来就把服务器给的 URL 用 `%s` 打进进度流(`cli/authflow/device.go` 本次未改),那是既有出口,属 L11(CLI 输出未过滤控制字符)家族,不在本轮范围。
4. **边界处文案指向错误原因(低,已修)**:上限判定原在 host 判定之前,"先同 host 跳 9 次、第 10 跳指向外站"会报 `stopped after 10 redirects` 而不是 host 违规。现改为策略判定在前、上限在后,语义不变而文案正确。
5. **复核确认但未改**:`Client.Server` 仍是死字段(改前既有,全仓无读者);残余第 3 条的 `:8443` 一例钉的是 token 层表驱动用例,不是 `CheckRedirect`;`maxRedirects` 与标准库常量重复会漂移——现在由"恰好 10 个请求 + 精确文案"钉住;`normalizeServer` 的 `.String()` 与 connect 的 `url.ParseRequestURI` 往返在 15 种 host 写法上无损。

**残余(本轮未处理)**

1. **未采纳"与 `--server` 同源"这一更严选项**:`external_url` 与 API 地址本来就可以不同主机(SPA 单独部署是常见姿势),同源限制会把正常部署的自动打开变成失败。实际风险(本地命令执行)由 scheme 白名单 + 不解析命令行的打开器消除;剩下的是"服务器让用户打开任意 http(s) 页面"这一钓鱼面,它与设备登录流程本身必须让用户访问服务器给出的确认页是同一件事,同源校验也消除不了。
2. **未加"打开前人工确认"**:会让无人值守/脚本化的 `mxd auth login` 多一步交互;URL 与 user code 本来就打印给用户并提示"不是你发起的请求就不要批准",确认的语义已经由页面上的显式批准承担。
3. **跨主机重定向由"静默带 token 跟随"变为"直接失败"**:若某部署确实依赖入口把 API 域名 302 到另一个域名,需要把 `--server` 改指最终地址;错误信息里给出两个 host,便于定位。host 比较是精确字符串:异端口(用例钉住 `:8443` 一例)以及大写、尾点、显式默认端口、IPv6 展开这类等价写法都按异 host 拒绝,方向是 fail-closed,但会给"代理给出带显式默认端口的绝对 Location"的部署带来一次失败。
4. **只改了 CLI 自己的 HTTP 客户端**:`cli/` 内除 `client.New` 外没有第二处 `http.Client`(`grep` 确认),新增出站请求必须复用该构造函数才能继承本策略。

### 2026-10-06 —— M10、M11 已修复(commit `25f2f0b`,复核加固 commit `8eb58d0`)

**M10:派生链接只保留 web 地址**(`backend/plugin/openlineage/airflow_links.go`、`frontend/src/utils/safeUrl.ts`)

- 服务端 `DeriveAirflowLinks` 不再只做 `TrimSpace`:新增 `safeExternalURL`,只接受 scheme ∈ {http, https}(`url.Parse` 已把 scheme 小写化,故大小写不敏感)且 host 非空(`Hostname() != ""`,`http://:8080` 一类只写了端口的写法不通过)的 URL,其余一律返回空,并返回 `url.Parse().String()` 的规范化结果(内部空格与非 ASCII 路径会被 percent-encode,合法 `%XX` 原样保留)。被拒的包括 path/authority 里的非法转义(`…/runs/%zz`),但 **query 不校验**——`url.Parse` 不看 `RawQuery`,所以 `?q=%zz` 会原样返回(它只是查询串,进不了 scheme/host,无安全影响)。链接是读取时从 `raw_payload` 现算的,没有落库副本,所以既不需要迁移,也不存在"旧数据绕过修复"的问题;`AirflowRunLogUrl` 为空时 `AirflowDagUrl` 也不再从它派生,前端连按钮都不渲染。
- 前端新增 `utils/safeUrl.ts` 的 `safeExternalUrl`:用 WHATWG `URL` 解析后只放行 `http:`/`https:`(返回 `parsed.href`)。两个 OL **字段**的 `:href` 改为 `v-if` + `computed(() => safeExternalUrl(...))`,不再直接绑定 proto 字段——这是服务端哪天漏一处时的第二道闸,也是 `§5.4` 提到的那类统一白名单在 SPA 侧的落点(第三处 `:href` 是 LLM provider 的本地常量链接,`location.assign` 属登录重定向路径,都不在 M10 范围)。这一层的判据只有协议:WHATWG 会把 `http:/x`、`http:evil.com`、`http:///x` 重解析成 `http://x/`,但这些写法在服务端就被拒了、到不了这里;而特殊 scheme 只要解析成功就必然有 host。**前提是输入已经过服务端校验**——前端从不单独信任未经校验的值,两层不是同一套判据的重复。

**M11:统一安全响应头**(`backend/server/echo_routes.go`、`frontend/vite.config.ts`、`frontend/vitest.config.ts`)

- `configureEchoRouters` 在 `recoverMiddleware` 之后接入 `middleware.SecureWithConfig`,因此**每个**响应(SPA 文档、客户端路由回落、REST/Connect、错误响应)都带同一份策略:`default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; worker-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`,外加 `X-Frame-Options: DENY`、`X-Content-Type-Options: nosniff`。M11 的 iframe 劫持路径(`/device?user_code=…` 是客户端路由,回落成同一份 index.html)由 `frame-ancestors 'none'` + `DENY` 关闭。`base-uri 'none'` 与 `form-action 'self'` 是独立复核后补的(commit `8eb58d0`,见下):应用没有 `<base>`、所有表单都由脚本提交到同源,所以零成本,却能挡住注入的 `<base>` 改写相对 URL 或注入的 `<form>` 把会话 POST 到别处。
- 两处刻意选择写进了注释并有测试钉住:`X-XSS-Protection` 显式留空(该头已废弃,只在仍识别它的旧浏览器里有反作用);不设 HSTS——它需要一个可信的 https 信号,而这个中间件会把调用方自带的 `X-Forwarded-Proto` 当作一个(属 M1 家族的可伪造输入),https 部署应由外层反代自己下发。
- 三处放宽是 SPA 的最低需要:Vue 的 `:style` 绑定与 monaco-editor 运行期注入样式表需要 `style-src 'unsafe-inline'`;monaco 的图标是 `data:` 图片(`img-src`);monaco 的 editor worker 是同源 module worker(`worker-src`,也避免日后收紧 `default-src` 时把它悄悄打断)。`script-src` 里既没有 `'unsafe-inline'` 也没有 `'unsafe-eval'`。
- **CSP 的前置修复(否则整站文案会被打掉)**:vue-i18n 的 esm-bundler 构建在未开 JIT 时由 `new Function` 编译消息,`script-src 'self'` 会以 `EvalError` 拒绝,`t()` 全线抛错、界面只剩空壳。按官方 CSP 方案在 `vite.config.ts` 与 `vitest.config.ts` 加 `define: { __INTLIFY_JIT_COMPILATION__: true }`:消息编译成 AST 后解释执行,不用 eval;两个配置都设是为了让测试与产物走同一条代码路径(否则单测走 eval 路径、产物走 JIT 路径,差异测不出来)。
- Vite dev server 自己提供 SPA 页面,不发这些头;开发态不受影响,这一取舍写进了 `security-posture.md`。

**回归测试**

- 单元 `backend/plugin/openlineage/airflow_links_test.go`:表驱动覆盖 12 种非 web 形态(`javascript:` 两种大小写、`data:`、`file:`、协议相对、相对路径、空 host、仅端口、无 host 的 scheme、纯空白与仅换行、URL 内控制字符)与 3 种合法 web 形态(大写 scheme、首尾空白、带 userinfo),后者同时断言派生出的 DagURL。
- 单元 `backend/server/security_headers_test.go`:对 dev/prod 两个 router 的 SPA 文档、客户端路由、`/healthz`、ConnectRPC 路径与 REST 网关路径,把**实际下发的** CSP 解析成指令表与期望 map 整体比较,并断言 `nosniff`、`DENY`、无 `X-XSS-Protection`、无 HSTS。断言取自响应而非源码常量,所以"策略不再下发"也会红——独立复核实测原先那条只比较常量的用例在 `SecureWithConfig` 被删后依然绿,现已合并为一条。
- 集成(真实服务器 + PostgreSQL)`TestSecurityHeadersRealServerIntegration`:对运行中的二进制逐个请求 `/`、`/device?user_code=…`、ConnectRPC、`/v1/*` 网关、`/oauth/authorize`、`/mcp`,断言同一组头。单元测试只装配 `configureEchoRouters`,而 Connect/网关/OAuth/MCP 由另外几个函数注册,这条补上"策略是否覆盖了全部路由家族"(状态码刻意不断言:MCP 关闭时 404、匿名调用 400/401)。
- 单元 `frontend/src/utils/safeUrl.test.ts`:14 条,合法 http/https(大写 scheme、首尾空白、userinfo)与 12 种拒绝形态;`pnpm test:coverage` 下该文件 100% 行/分支。
- 单元 `frontend/src/pages/openlineage/{OpenLineageRunDetailPage,OpenLineageTaskDetailPage}.test.ts`:挂载页面,断言 `javascript:` 字段**不渲染任何锚点**、真实 http 字段渲染出正确 `href`。此前只有 helper 级用例,把页面绑定回退成直接绑 proto 字段后套件依然全绿(独立复核实测),现在这两条会红。
- 集成(真实服务器 + PostgreSQL)`TestOpenLineageAirflowLinksRejectNonWebSchemesRealServerIntegration`:用 ingestion key 真投递 `log_url = "javascript:…"` 的事件,再以管理员读 `ListOpenLineageRuns`,要求 `airflowRunLogUrl`/`airflowDagUrl` 均为空;同一用例投递真实 Airflow 地址,要求链接原样返回、DagURL 派生正确——覆盖"摄取 → 存储 → API 转换"整链,而不只是插件函数。
- **反向验证**:把 `safeExternalURL` 还原成 `TrimSpace` ⇒ 上述集成用例失败(`Should be empty, but was javascript:alert(document.cookie)`);撤掉 `SecureWithConfig` 那段 ⇒ 头用例全红(`expected: "nosniff", actual: ""`,且不再是"部分绿");把两个页面的 `:href` 回退成直接绑定 ⇒ 两条页面用例失败(`expected true to be false`,而原有的 4 条页面用例照旧全绿,正是复核指出的缺口);用改动前的产物(仍含 `new Function`)套同一份 CSP 用无头 Chrome 加载 ⇒ 控制台 `EvalError: Evaluating a string as JavaScript violates the following Content Sec…`,DOM 只剩 2.8KB、无任何文案,而修复后的产物在同样 CSP 下渲染出完整登录页且控制台零违规、零 `EvalError`。
- 端到端(真实 `-tags "release embed_frontend"` 二进制 + 本地 PostgreSQL):`curl` 确认 `/`、`/device?user_code=…`(客户端路由回落)与 400 错误响应都带完整头;无头 Chrome 在 CSP 下渲染出完整登录页(标题、按钮、图标、字体均正常)。

**残余(本轮未处理)**

1. `style-src 'unsafe-inline'` 仍是放宽项:Vue 的 `:style` 与 monaco 都需要它,要收紧得先把动态样式改成 class 或拆 `style-src-attr`,属前端构建层改动,不在本轮安全修复范围。
2. 头部对所有响应统一下发(含 JSON),没有按路由区分;HSTS 未设置,https 终结部署仍需外层反代自行下发(M11 未涉及)。
3. 白名单只在"服务端派生的链接"这一处落地(`airflow_links.go`);`§5.4` 设想的统一出站/URL 校验层里,CLI 打开浏览器与 LLM `base_url` 两处仍按路线图分属 M12/M13/M23。
4. dev profile 的页面由 Vite 提供、不带 CSP;要覆盖开发态需在 vite dev server 或开发环境反代上加头,尚未做。
5. **只修掉了"执行",没修掉"任意 web 链接"**:任何 http(s) 地址(钓鱼页)照旧能出现在受害者的 Airflow 按钮上——这正是 M10 原文列出的下限。要消除需要 host 白名单/内网黑名单(与 M21/M23 同一族),属部署策略而不是这个函数的职责。
6. **Go `net/url` 与浏览器 WHATWG 的接受/拒绝差异**(独立复核的 95 例里 15 例不同):越界端口(`:99999`)、IPv6 zone ID(`[fe80::1%25eth0]`)服务端放行而浏览器 `new URL` 抛错 ⇒ API 返回了链接、按钮却不出现(方向仍是 fail-closed);反向是 `http:/x`、authority 内反斜杠等被服务端拒、浏览器会归一化。**没有任何一例能产生非 http(s) scheme。** 未做 WHATWG 对齐:那需要换解析器或补端口/zone 校验,收益仅是消除"按钮消失"这种可见性差异。
7. **跨源 API 基址**:生产构建若把 `VITE_API_BASE_URL`(`frontend/src/api/client.ts`)配成跨源,`default-src 'self'` 会拦掉全部 API 调用。单二进制同源部署不受影响;把 SPA 托管在别处的部署需要在提供文档的一侧自行放宽 `connect-src`。
8. monaco 当前只用同源 module worker(`worker-src 'self'` 够用);它内部还有一条 `URL.createObjectURL` 的 blob worker 工厂,本应用没有走到——若日后切换,`worker-src` 需要补 `blob:`。

**验证门禁**(含加固 commit):`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`、`make test-integration-smoke`(真实 PostgreSQL + MySQL + migrator 全绿,含新增的 Airflow 链接与安全响应头两条集成用例)、`release` 与 `-tags "release embed_frontend"` 构建;前端 `biome:check`、`lint`、`i18n`、`type-check`、`test run`(49 文件 / 332 用例)、`test:coverage`、`build` 全绿。

**独立复核后的加固(commit `8eb58d0`)**

M10/M11 落地后由独立子代理对 `25f2f0b` 做了对抗式只读复核(95 例 Go↔WHATWG 解析对比、`go test -overlay` 反向探针、真实 `-tags "release embed_frontend"` 二进制逐路由核头、无头 Chrome 在同一份策略下加载修复前后产物、iframe 劫持与内联脚本/`javascript:` 导航的浏览器实测;探针全部在 `/tmp`,未改动仓库文件)。结论:

- **执行面与劫持面关闭**:95 例里没有一例能通过服务端闸门、再被浏览器解析成非 http(s) scheme;`frame-ancestors 'none'` + `DENY` 出现在 `/`、客户端路由、Connect、REST 网关、OAuth、`/mcp`、pprof/metrics、400 错误、h2c/HEAD/OPTIONS 上,Chrome 拒绝 iframe;同一策略下内联脚本、内联事件处理器与 `javascript:` 导航均被拦、同源外部脚本可加载;仓内无 `v-html`/`innerHTML` 汇聚点,也没有把调用方输入反射成 HTML/JS 的端点;产物中已无 `new Function`/`eval(`。
- 复核指出四处缺口,均已修掉:头用例里"只比较源码常量"的那条在中间件被删后依然绿(改为断言**实际下发**的指令表);头用例只覆盖 echo 自带路由,而 Connect/网关/OAuth/MCP 由别的函数注册(新增真实服务器集成用例);页面级 `:href` 绑定没有任何测试、回退成直接绑 proto 字段后套件全绿(两个详情页各加"危险字段不渲染锚点 / 真实地址渲染正确 `href`"两条用例);CSP 缺 `base-uri`/`form-action`(已补,见上)。
- 文档更正三处:query 里的 `%zz` 会原样通过(`url.Parse` 不校验 `RawQuery`),原文"非法转义一律被拒"说得过宽;前端那层"协议检查就是全部判据"的前提是输入已经过服务端校验,WHATWG 会把 `http:/x` 一类写法重解析成 `http://x/`,只是它们到不了前端;"前端 `:href` 统一经白名单"不成立——第三处 `:href` 是本地常量。
- 复核未能在 M10/M11 范围内构造出可利用问题,但记录了两条相邻观察:`LoginPage.vue` 的 `window.location.assign(redirect)` 分支因 router 的 `/:pathMatch(.*)*` catch-all 永远不可达(可能让 MCP OAuth 的登录回跳落到 NotFound 页),以及 monaco 的 blob worker 与跨源 `VITE_API_BASE_URL` 两个 CSP 前提——前者是独立问题、本轮不改,后者写进残余 7/8。

### 2026-10-06 —— M15 已修复(主修复 commit `f55f62a`,独立复核后的加固 commit `10e803f`)

**CreateSSOState 的预算,以及它保护的缓存**(`backend/component/state/{state,window_limiter}.go`、`backend/api/v1/throttle_interceptor.go`)

- `CreateSSOState` 是匿名方法,每次调用往容量有界的 `SSOStateCache` 写一个 nonce;调用方无限刷即把真实用户正在进行的 SSO 流程挤出去("invalid or expired state")。现由 `ThrottleInterceptor` 按解析后的客户端地址 + 全局双桶计额(120/min 与 300/min,与 Login 同值:两者是同一次登录流程的两半,state 预算更紧就会把流程压在它所供养的登录预算之下)。它与 Login 一样不对已登录调用方豁免。
- **只加限流不足以关闭攻击**,这是本轮唯一超出审查建议原文的改动:全局预算 300/min、TTL 5 分钟,而限流窗口是固定窗口而非滑动窗口 —— 攻击者的第一份满额可以起于用户取号之前的一个窗口并仍落在该 nonce 的有效期内,此后每个窗口边界再给一份,所以上界是 (ceil(5) + 1)×300 = 1800("TTL ÷ 窗口"的 1500 少算了一份),而容量是 1024。多来源(或轮换来源)的调用方因此仍能把缓存填满,此时约束是容量而不是速率。容量提到 4096(`ssoStateCapacity`),并把"容量 > 满额 ×(ceil(TTL/窗口) + 1)"写成被测试钉住的不变式。截断除法会在 TTL 不是窗口整数倍时少算一整份(复核实测:TTL=779s 时真值 4200 次,截断只有 3900),该写法已在复核后改为向上取整;任何让关系不再成立的常量改动(容量调低,或全局预算/TTL 调高)都会让它变红。
- 该预算同时是 IdP 出站的闸门:`Login` 的 IdP 分支在 `consumeSSOState` 之后才出站,没有本服务器签发的 state 根本走不到那里。state 与 Login 两个预算因此从两端夹住"未认证者借服务器打企业 IdP"的速率;证据里"IdP 分支无限流"一条在 `e9bc802`(M5)之后已不成立(入口预算按 procedure 计额),本轮不再另加一套语义重叠的 IdP 预算。
- 顺带更正措辞:`backend/component/state/{window_limiter,login_limiter}.go` 的类型注释与预算说明、以及 `security-posture.md`,原来把这套计数写成 "sliding window";实现其实是固定窗口 —— 窗口以每个键的首个允许请求(或首次失败)为起点,只在该窗口结束后重置,被拒的请求不延长窗口。上一条的上界推导依赖的正是这个语义,这四处措辞一并改为固定窗口并写明锚点。
- state 仍是进程内状态(与设备登录、OAuth pending 同一约定),多副本下每个副本各有一份额度与一份缓存。

**IdP 出站超时与 ctx 透传**(`backend/plugin/idp/oauth2/oauth2.go`、`backend/api/v1/auth_service.go`)

- provider 的 `http.Client` 增加 `Timeout: idpRequestTimeout`(30 秒),覆盖连接、token 交换与 userinfo 读取的整段交换。此前该客户端没有任何超时,而登录 handler 用的是不带 deadline 的请求 ctx:一个"接受连接后不再说话"的 provider 会把请求、goroutine 与 socket 无限期挂住,而每次匿名调用都带着管理员配置的 client_secret。
- `UserInfo(token)` 改为 `UserInfo(ctx, token)`,以 `http.NewRequestWithContext` 发出,登录 handler 把自己的 ctx 传进去:调用方放弃时请求立即结束,不必等客户端自己的 30 秒。`ExchangeToken` 本来就走 `conf.Exchange(ctx, …)`,与 `UserInfo` 共用同一个客户端,因此也一并被这个超时兜住。

**回归测试**

- 单元 `backend/component/state/window_limiter_test.go`:`TestSSOStateLimiterBoundsOneSource`(按源额度用尽后拒绝、另一来源有自己的桶)与 `TestSSOStateCacheOutgrowsTheStateBudget`(容量 > 满额 ×(ceil(TTL/窗口) + 1),即 4096 > 1800)。
- 单元 `backend/component/state/window_limiter_test.go`:`TestSSOStateFloodCannotEvictAValidNonce` 把同一条性质在真实限流器 + 真实 LRU 上量一遍。攻击者按最坏相位灌水(用户取号前一个窗口先花掉一份满额,此后每个窗口边界再花一份,来源各不相同,直到 nonce 过期),要求受害者的 nonce 仍留在缓存里;容量还原为 1024 时它与上一条一起变红,同时证明洪水确实强到足以逐出(不是空跑)。
- 单元 `backend/api/v1/throttle_interceptor_test.go`:`TestThrottleInterceptorSSOStateBudget`(第 121 个请求被拒、另一来源不受影响);`testSSOStateSourceBudget` 像另两个预算一样重复 state 包的常量,使静默改动变红;同一用例另外断言 `limiterFor` 给这个方法的是 `SSOStateRequestLimiter` 且与 Login 的不是同一个 —— 两个预算数字相同,只有指针身份能把"换用 Login 预算"这种退化测出来。
- 单元 `backend/plugin/idp/oauth2/oauth2_test.go`:`TestProviderRequestsAreTimeBounded`。桩 provider 接受连接后只等 `r.Context().Done()`,因此只有服务端自己施加的 deadline 能结束请求:ctx 200ms 到期必须让调用返回(证明 `NewRequestWithContext` 生效),客户端 `Timeout` 必须等于包内常量、且缩短为 200ms 后确实结束一次没有 deadline 的调用。
- 集成(`backend/test/integration/runner/sso_login_service_test.go`,真实服务器 + PostgreSQL):`TestCreateSSOStateIsBudgetedRealServerIntegration` 把客户端绑到 127.0.0.2,连发 120 次都拿到 state,第 121 次 `ResourceExhausted`。绑第二个 loopback 地址是为了让洪水花自己的桶,而不是其它用例共用的 127.0.0.1 桶(与 M2 用例同一手法;127.0.0.2 不可绑时跳过)。同一用例随后再用 REST 形式(`POST /v1/auth/ssoState`)打一次并要求同样 429:网关自连接必须花外层对端那个桶,否则换个入口就能绕过预算 —— 这一步同时是 M2 戳记在这个端点上的回归用例。该断言之前先用默认地址(127.0.0.1)的客户端取一个 state 并要求成功,把"loopback 桶此刻仍有余量"变成显式前置条件,否则戳记失效会被"loopback 桶早已被别的用例耗尽"掩盖。
- 集成(`sso_login_service_test.go`):`TestSSOLoginStopsWhenTheCallerGivesUpRealServerIntegration` 用一个只回 `/token`、`/userinfo` 永久挂起的桩 provider 起一次 SSO 登录,等桩确认收到 userinfo 调用后取消调用方 ctx,要求桩的 userinfo 处理函数在 5 秒内结束。Connect 客户端在"出站被取消"和"出站挂到超时"两种情况下都会立刻返回,所以只有 provider 能观察到出站请求是否真的停了;把 handler 里的调用点改回 `UserInfo(context.Background(), token)` 时这条会红(5.04 秒后报 outlived)。
- **反向验证**(逐条单独回退,验证后均已恢复):把 `limiterFor` 的 `CreateSSOState` 分支换成不匹配的 procedure ⇒ `TestThrottleInterceptorSSOStateBudget` 与那条集成用例都变红;把该分支改指向 `LoginRequestLimiter` ⇒ 新增的指针身份断言变红;`ssoStateCapacity` 还原为 1024 ⇒ 不变式用例与洪水用例都变红(`"1024" is not greater than "1800"`,以及"a nonce a user just minted must survive the anonymous flood for its whole TTL");把全局预算调高到 800 而容量不动 ⇒ 不变式用例变红(`"4096" is not greater than "4800"`);`SSOStateTTL` 改为 779s ⇒ 改向上取整后的公式如期变红(`"4096" is not greater than "4200"`);去掉 `Timeout: idpRequestTimeout` ⇒ 该字段断言变红;`UserInfo` 还原为 `http.NewRequest` ⇒ ctx 子用例直到 30.03 秒的客户端超时才返回,`elapsed < 5s` 失败;handler 的调用点改为 `context.Background()` ⇒ 上面那条新的集成用例变红(5.04 秒后报 outlived);让网关中间件的 `audit.StampGatewayPeer` 戳一个丢弃的 header ⇒ REST 那一步从 429 变成 200。

**残余(本轮未处理)**

1. **超时是包级常量,没有按 provider 覆盖的入口**:慢速企业 IdP 需要改这里重新编译。当前没有可配置 IdP 的 API/UI(`idp.config` 只能改库),加一个配置字段要动 proto 与生成物,不属本条范围。
2. **`UserInfo` 的响应体仍是无上限的 `io.ReadAll`**:恶意或被接管的 IdP 可以返回任意大的 body。这是管理员配置的出站目标,与 M23(LLM 出站无策略层)、M20/M21(数据源出站)同属 §5.4 的"出站策略层"缺位;本轮的 30 秒超时限制的是时间,不是内存。
3. **`skip_tls_verify` 未动**(M22/D7):provider 侧跳过 TLS 校验与"无条件信任 userinfo 返回的主体"叠加,能中间人的对手仍可伪造任意 subject。
4. **多副本语义**:state 预算与 state 缓存都是进程内的(与 D2 的多副本清单一致)。预算同时意味着一个部署的 SSO state 上限;它不阻止"多个来源各刷一部分",只是让缓存大到刷不满。复核对这一点的说法做了收紧:逐出型 DoS 在复制下**仍然关闭** —— 每个副本自己的全局桶把该副本缓存的准入压在同一上界之下(4096 次逐出需要 300/min 跑 13.6 分钟 > 5 分钟 TTL),复制只会带来既有的"nonce 与登录必须落在同一副本"约定,不会重新打开逐出路径。
5. **全局兜底预算可被少数来源饿死**(复核实测,已记录):3 个来源按 120+120+60 花掉 300/min 后,第四、第五个来源都拿到 `ResourceExhausted` —— 攻击面从"无限取号"变成了"限速型拒绝"。这是全局兜底的固有代价,Login 的 300/min 全局桶同样可被少数来源占满(既有设计),不是本条的回归;它只挡住新开的 SSO 流程,已有会话不受影响。收敛方向是 §5.2 的统一限流(按源配额与全局配额分层,而不是让少数来源吃满全局),不在本条范围。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go build ./...` 与 release 构建、`go test ./backend/...`、`make test-integration`(真实 PostgreSQL + MySQL + migrator)。本轮未改前端与 CLI,未跑这两侧门禁。加固提交 `10e803f` 后重跑同一组门禁。

**独立复核后的加固(commit `10e803f`)**

M15 落地后由独立子代理对 `f55f62a`(及当时工作区的未提交增量)做了对抗式只读复核:探针全部挂在 `/tmp` 并以 `go test -overlay` 注入,未改动仓库文件,也未跑集成套件。复核确认两条核心结论成立,并用真实限流器 + 真实 LRU 实测了要害性质(受害 nonce 存活:容量 1024 时 `false`、4096 时 `true`,因此容量必须提高;最坏相位下攻击者实际准入 1799 次,与公式 1800 一致;只有 handler 一处写缓存、`state.New()` 是唯一构造点,没有 nil limiter;x/oauth2 确实消费 `oauth2.HTTPClient` 的值,300ms 的 client 超时对 `UserInfo`/`ExchangeToken` 的建连、交换与 body 读取都生效)。它同时指出下列问题,均已修掉:

1. **不变式测试用了截断除法**(中,已实测):`int(SSOStateTTL/throttleWindow)+1` 在 TTL 不是窗口整数倍时少算一整份 —— TTL=779s 时公式给 3900 < 4096(绿),而真实上界 4200 > 4096(关系已破)。现改为向上取整,并补上 TTL=779s 的反向验证(如期变红)。复核还指出该用例只是常量算术、测不到行为,故新增真实限流器 + 真实 LRU 的洪水用例。
2. **"改用 Login 预算"没有任何测试能发现**(中):两个预算数字相同,行为断言区分不了。现在断言 `limiterFor` 返回的正是 `SSOStateRequestLimiter`,且与 Login 的不是同一个;互换后变红。
3. **handler → `UserInfo` 的 ctx 接线无人守护**(中):把调用点改成 `context.Background()` 时全部单测与既有集成用例依旧全绿 —— 唯一的 IdP 集成用例用的是即时应答的假 provider,观察不到取消。新增以"只挂 `/userinfo`"的桩观察出站请求是否真的随调用方结束的集成用例;回退调用点即变红。
4. **REST 那半条断言可能因为别的原因通过**(低-中):若共用的 127.0.0.1 桶在断言之前就被别的用例耗尽,戳记失效也会返回 429。现在断言前先用 127.0.0.1 取一个 state 并要求成功,把"loopback 桶此刻仍有余量"变成显式前置条件 —— 不满足就报错,而不是静默通过。
5. **两处文档表述与代码不符**(已改):`security-posture.md` 原写 "holds 4096 nonces over a five-minute TTL",而 `SSOStateCache` 并不按时间淘汰(用 `lru.New` 而不是 `NewWithExpire`),nonce 只在被消费或逐出时离开,TTL 只在 `consumeSSOState` 里检查 —— 稳态下缓存里可以有任意年龄的 nonce,保护有效 nonce 的是容量余量而不是 TTL;同段"单独改任何一个数字都会让它变红"对 `SSOStateTTL` 不成立(见第 1 条),已改为"任何让这条关系不再成立的常量改动"。

复核**未能推翻**的核心结论:单副本下,只控制自己请求的攻击者无法逐出他人仍有效的 nonce;网关戳记(M2/M3)在此端点上照常生效。未测到的部分:集成套件本身(复核被要求不跑),以及与本条无关的子系统。

### 2026-10-06 —— M14 已修复(commit `f9f67fb`,对抗复核后的加固 `414aa60`)

**M14:限流键取解析后的地址,限流器容量有上限**(`backend/server/rate_limiter.go`、`openlineage_ingestion.go`、`oauth_endpoints.go`)

- 新增 `rateLimitSourceKey`:非认证调用方的预算键取 `audit.ClientAddress`(按 `--trusted-proxies` 从右往左解析出的真实地址),与审计行、登录/设备登录/OAuth 匿名端点限流同源。Echo 的 `RealIP` 无条件相信 `X-Forwarded-For`,按它取键等于让调用方每请求换一个桶。
- `openLineageIngestionMiddleware` 的无 key 回退由 `c.RealIP()` 改为 `rateLimitSourceKey(c, trustedProxies)`,由 `configureGrpcRouters` 传入 `profile.TrustedProxies`;带 key 的请求照旧按 key 的 SHA-256 摘要分桶,原始 key 不进任何结构。OAuth 匿名端点改用同一个 `rateLimitSourceKey`,少一次 proto 分配;对真实 TCP 对端(`RemoteAddr` 恒为 `ip:port`)它与原来的 `audit.BuildRequestMetadata(...).GetIp()` 取值相同——原先"取值等价,因为 `ClientAddress` 只返回归一化地址或空、不会超过 `MaxIPBytes`"的表述不成立(`ClientAddress` 并不归一化,`HostFromAddr` 对无端口输入原样返回;复核以 `::ffff:203.0.113.5` 与 68 字节对端实测),已更正。这不是安全问题:对端地址由 net/http 填写,调用方不可控。
- 新增 `boundedRateLimiterStore`:每个标识一个令牌桶(语义与 echo 的 `RateLimiterMemoryStore` 相同),标识数量封顶 `rateLimiterCapacity`(4096),空闲超过 `rateLimiterExpiresIn`(3 分钟,与 echo 默认一致)的桶在 Allow 里被清扫,达到上限时淘汰一个桶(加固后为 O(1),见下)。淘汰只会给回一份新额度、不会拒绝,所以上限约束的是"调用方能让我方占多少内存",不会自己变成拒绝服务。OL 摄取(50/100)一份 store;OAuth 四个匿名路由共享一份 store(10/20,见加固节);都是进程内状态(D2 家族)。
- `golang.org/x/time` 因新实现成为直接依赖(仅 `go.mod` 的 require 分类变化,`go.sum` 未动)。

**回归测试**(`backend/server/`)

- `openlineage_ingestion_test.go`:无 key 且每请求换一个 `X-Forwarded-For` 仍必须在 burst 内被拒(加固后断言"整轮只有一个桶",由冻结时钟保证确定性);对端列入 `--trusted-proxies` 时 XFF 才决定桶,一个地址的额度不占另一个地址的;按 key 分桶的既有断言保留;新增 `TestOpenLineageIngestionMiddlewareTrustAllProxiesLetsTheCallerChooseTheBucket` 钉住"全信任链回退取最左段"这一已披露的边界。
- `oauth_endpoints_test.go`:同一性质在 OAuth 路由上钉住;`TestOAuthEndpointsCarrySeparateBudgets` 用两次构造挂两个路由,断言一条路由耗尽后另一条仍可用(配额维持每路由一份,见加固第 3 条)。
- `rate_limiter_test.go`:到容量后桶数不超上限(任意淘汰一个)、旋转标识不越过上限、空闲桶被清扫、单标识的 rate/burst 语义不变;`TestBoundedRateLimiterStoreEvictionStaysConstantTime` 用"同一插入工作量、有/无淘汰"的比值钉住 O(1)(见反向验证);`BenchmarkBoundedRateLimiterStoreNewIdentifierAtCapacity` 供人工观察。时间经注入的 `timeNow` 前进,不依赖真实时钟。

**反向验证**(逐条单独回退后对应用例变红,随后恢复)

- 把 OL 的 `IdentifierExtractor` 还原为 `c.RealIP()`:`TestOpenLineageIngestionMiddlewareIgnoresAnUntrustedForwardedHeader` 失败(`rotating X-Forwarded-For must not open a new budget`)。
- 让容量判断恒假(`if len(s.visitors) >= s.capacity` → `if false`):`TestBoundedRateLimiterStoreEvictsAtCapacity` 失败(`map[a b c] should have 2 item(s), but has 3`)。
- 把淘汰还原成"扫描最久未见"的 O(capacity) 实现:常数时间用例失败(实测 `withoutEviction=12.4ms withEviction=1.45s`,比值 117 倍,阈值 50 倍),其余用例仍绿——容量与语义断言看不出代价,这正是该用例存在的理由。

**对抗式复核后的加固(commit `414aa60`)**

由独立子代理对 `f9f67fb` 做对抗式只读复核(scratch worktree + `go test -overlay` 探针,未改动本仓库)。它确认了核心结论——9 种调用方可控的 key-less 头变体(XFF、`X-Real-IP`、`Forwarded`、多行 XFF、空 `Bearer `、`Basic`、无凭证等)都恰好停在 burst=100,摘要与地址键不碰撞,并发下不越容量,淘汰只放宽不拒绝,两条反向验证真实有效——同时实测出下列问题:第 1、2 条已修,第 3 条按集成套件的证据改为回退并更正文档,第 4、5 条已披露/更正:

1. **容量分支是 O(capacity) 扫描,构成匿名方可强制的 CPU 成本(中高,已实测)**:每到容量就为每个新标识在**全局互斥锁内**扫描整张 map 找最久未见者;而"每请求换一个垃圾 key"是免费的(带 key 路径按设计不限速),所以这是一条调用方可以随意要求的开销(复核实测 `known id 191ns/req` vs `new id 196µs/req`,`-race` 下 874ns vs 468µs)。淘汰一个**任意**桶即可——淘汰只会给回满额,选谁都不改变安全语义——现在为 O(1);文档原先"不会自己变成拒绝服务"的说法由此不再成立,已随之改写。
2. **边界用例与令牌回填赛跑(低,已复现)**:`BelievesAListedProxy` 断言第 101 个请求恰好 429,要求前 100 个在 20ms 内跑完(`-race` 下实测 12.3ms,余量 1.6 倍);复核在 64 个 CPU 占用 + `GOMAXPROCS=1` + `-race` 下复现 23/50 次失败。现在中间件经不可导出的 `...WithStore` 变体接收 store,测试用冻结时钟断言精确边界;生产构造器仍有用例覆盖,且只要求"上限出现"。
3. **四条匿名 OAuth 路由各有一份配额(与文档矛盾,即 I2)——合并后回退(commit `f440dc8`)**:复核指出 `oauthEndpointMiddleware` 每路由调用一次,一个地址因此拿到文档所述上限的 4 倍,而 `security-posture.md` 与 `docs/mcp.md` 写的是 share one ceiling。按建议把四条路由指向同一份 middleware 后,`make test-integration` 在本机与 CI 都失败:集成套件在同一进程、同一来源地址(127.0.0.1)上依次注册、授权、完成、换发,合并后的 20 次 burst 被迅速耗尽,`TestMCPAuthorizationAndToolsRealServerIntegration` 与 `TestMCPRegistrationValidatesRealServerIntegration` 开始拿到 429 而不是断言中的 404/400/201(main 上同一 job 为绿)。因此配额维持"每路由一份"(与合并前一致),文档改为按实现描述并在 I2 记录该备选方案的代价;是否共享一份上限属配额决策,需要单独的改动与集成侧配合。回归用例改为 `TestOAuthEndpointsCarrySeparateBudgets`(一条路由耗尽后另一条仍可用)。
4. **键在"信任范围覆盖客户端"的配置下仍可由调用方选(中,配置相关,非本轮引入)**:`audit.ClientAddress` 在每一跳都是受信代理时回退到**最左段**(调用方写的那一段),所以 `--trusted-proxies` 里出现 `0.0.0.0/0` 或覆盖客户端的 CIDR 时,key-less 请求又能每请求换桶(复核实测 `0.0.0.0/0` 下 500/500 全过)。这与审计 IP、以及改造前的 OAuth 限流是同一性质,属 M1 既有设计(§10 的 M1 加固第 5 条已记为"设计如此");本轮据此**披露并钉住**,而不是改掉:M14 的保证以"代理清单只列代理、不列客户端网段"为前提。
5. **文档与提交信息修正**:M14 原文攻击场景把"每次 401 触发一次 `ValidateOpenLineageAPIKey` DB 查询"当作 key-less 路径的一部分;实际上 key-less 请求在 handler 里于查 key 之前就 401(`openlineage_handler.go:61-63`),该查询属于非法 key 路径(修复提交信息沿用了这句,在此更正)。"与 `component/state.WindowLimiter` 的容量语义一致"也不准确:`WindowLimiter` 淘汰的是窗口起点最早的一条、且只在插入时清理;本 store 加固后淘汰任意一条。

**残余(本轮未处理)**

1. **标识仍部分由调用方提供**:带 key 的请求按 key 摘要分桶,所以"每请求换一个非法 key"仍会得到新桶——容量与常数时间淘汰只约束这样做的内存与 CPU 代价,不约束其请求速率;要真正封顶需要给带 key 的请求再加"按源"维度(§5.2 的统一限流,与 M24 同族)。M14 原文的"无 key 回退 `RealIP`"这条路径已关闭,复核确认 9 种 key-less 头变体都被同一个地址桶接住。
2. **键在受信范围覆盖客户端时不可信**(见加固第 4 条):`--trusted-proxies` 列出 `0.0.0.0/0` 或客户端网段即重新打开"调用方自选桶",这也是审计 IP 的既有语义。可选的加固是启动时拒绝 `/0` 条目,或给键再加"直接对端"维度;都不在 M14 范围。
3. **淘汰不区分敌我**:被淘汰者(可能是安静的生产者,也可能是某个来源地址)只是拿回一份新额度,方向是放宽而非拒绝;容量 4096 对正常部署远大于同时活跃的生产者数。
4. **限流计数仍是进程内、每副本一份额度**(D2 家族);两个匿名限流器都没有"全局回退桶"(§5.2)。
5. **OAuth 四路由的配额语义未定**:当前每路由一份(与合并前一致),I2 的"合并为一份上限"因集成套件被 429 打断而回退(加固第 3 条)。真正决定共享与否需要同时想清楚匿名 OAuth 面的目标速率与集成套件如何避开单一来源地址,属单独的配额决策。
6. **`make test-integration` 已在本轮后段跑过**:回退合并预算后本机全绿(`backend/test/integration/runner` 40.7s、`backend/migrator` 18.3s);但本轮没有新增针对该中间件的集成用例——既有的 OL 摄取集成用例走的是 handler,`configureGrpcRouters` 传入 `profile.TrustedProxies` 这一行由编译与既有套件路径覆盖。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test -count=1 ./backend/...`、`go test -race ./backend/server/`、`go vet ./backend/server/...`、release 构建(`-tags release`)与默认构建;回退合并预算后补跑 `make test-integration`(真实 PostgreSQL + MySQL + migrator,全绿)。本轮未改前端与 proto,未跑前端门禁;CI 侧 `Unit Tests`/`Lint`/`Frontend`/`Release build` 与 `MySQL Real Server Integration` 见 PR。

### 2026-10-07 —— M18 已修复(commit `a691632`)

**单事件上限与身份字段限长**(`backend/plugin/openlineage/limits.go`)

- 8MiB 的请求体上限只管一个请求,不管其中一个事件:一个 8MiB 的单个事件此前会整段入库,此后每次读到它都要付出这份体积。新增 `ValidateEventLimits`(单事件路径与批处理共用):单事件 1MiB,超限返回哨兵 `ErrEventTooLarge`(单事件答 413,批处理中任一事件超限则整批 413,与请求体上限一致);身份字段按唯一 btree 索引能容纳的长度封顶——`job.namespace`/`job.name`/`run.runId` 255 字节、job type 64 字节、dataset namespace 255 字节、dataset name 512 字节、单事件 dataset 总数 1000,并对转义后的 run GUID 另加 2000 字节上限(逐字段封顶后,若每个字符都需百分号转义仍可能逼近 2704 字节的索引项上限)。
- 超长身份此前"能入库、再插索引永远失败",对生产者而言不可重试;现在按无效事件答 400(批处理中与不可解析事件同样跳过并计入 `failed`),即 L15 建议的"摄取时限长返回 400"。

**摄取时物化,列表不再读 `raw_payload`**

- `openlineage_run` 新增 `airflow_run_log_url`(摄取时经同一 `safeExternalURL` 白名单算好并落库;`airflow_links.go` 新增 `AirflowLinksFromRunLogURL`,DAG URL 仍在读取时派生,与 `DeriveAirflowLinks` 结果一致并有单测钉住)。`openlineage_task` 的列表查询改为 join 该列,不再取 `latest_run.raw_payload`;`OpenLineageTaskMessage.LatestRawPayload` 随之改为 `LatestAirflowRunLogURL`。
- 新增 `openlineage_run_dataset`(迁移 `0015` 与 `LATEST.sql` 同步):每行一个"某 run 读/写了某数据集",含 namespace、name、direction、`has_column_lineage`、schema facet JSON、column-lineage 字段名、event_time、integration/source,外键 `run_pk → openlineage_run(id) ON DELETE CASCADE`(保留策略删 run 时引用随之消失,不需要在 maintenance 里多写一步)。摄取在同一事务里按 run 先删后插替换整组引用;终态 run 拒绝的重复投递不碰引用。
- `store.ListOpenLineageRun` 的默认投影不再选 `raw_payload`(改选 `NULL::jsonb`),只有 `FindOpenLineageRunMessage.IncludePayload` 为真才取;`GetOpenLineageRun` 显式置真(单个 run 详情仍回原文 payload),upsert 的"拒绝后回读"不需要。**这才是列表读取 payload 的真正入口**:即便旧列表接口的 `includePayload=false`,`convertOpenLineageRun` 此前仍对每一行调用 `DeriveAirflowLinks(run.RawPayload)`。
- 数据来源侧:`OpenLineageRunMessage` 携带 `Datasets`,`runMessageForEvent` 从已解析的事件里提取(同一 (direction, namespace, name) 去重;输入的 columnLineage facet 不置"该 run 写出的列血缘"标志;字段名排序以便重投递落库一致)。

**dataset 列表/详情/筛选项下推 SQL**(`backend/store/openlineage_dataset.go`)

- 列表:`GROUP BY namespace, name` 在 SQL 完成,`COUNT(DISTINCT task_guid) FILTER (direction = …)` 出源/目标 job 数、`ARRAY_AGG(DISTINCT integration/source)`、`BOOL_OR(has_column_lineage)`、`MAX(event_time)`;namespace 进 WHERE,integration/source/column-lineage-only 进 HAVING(放进 WHERE 会把该组其余 integration 滤掉,而列表要显示它们);`ORDER BY last_seen DESC, name, namespace` 后 `LIMIT 5000` 封顶。自由文本检索(含解析目标)与 internal/external 作用域需要解析,仍在 Go 侧完成,但作用于**已聚合的小行**:一次请求最多解析 5000 个数据集分组,而不是 5000 条 payload。
- 详情:`GetOpenLineageDatasetDetail` 用同一组 (namespace, name) 拼写做索引查询;summary 在拼写并集上去重计数(同一 job 用两种拼写报告同一数据集仍只算一个),相关 job 限 8、最近 run 限 10、最佳 schema 以 `jsonb_array_length DESC, event_time DESC` 取最宽(与旧 Go 逻辑等价)、column-lineage 就绪字段取输出引用的并集。GUID→拼写仍由 Go 解析(新增的 `resolveOpenLineageDatasetAggregates` 只对聚合行调用解析器,解析失败回退外部身份,与旧路径一致),因此"新增 namespace mapping 后数据集立刻变内部"的既有行为不变。
- 筛选项:dataset 的 namespace/integration/source 三个维度改为读 `openlineage_run_dataset`(integration/source 按 `COUNT(DISTINCT run_pk)`),与 run 侧维度复用同一套排序/上限助手;旧的"从 5000 条 payload 现算"路径删除。

**回归测试**

- 单元:`plugin/openlineage/limits_test.go`(合理事件通过;超限事件 `errors.Is(ErrEventTooLarge)`;六类超长字段各自报错且不是尺寸错误;逐字段到顶且全字符转义时 GUID 超限被拒;dataset 数超限被拒)、`api/v1/openlineage_handler_test.go`(超限事件使批处理整批 413 且未触碰 store;超长身份在批处理中计为 `failed`;`openLineageDatasetRefs` 的去重/输入不置列血缘标志/schema 与字段名落库/重投递不擦除;`runMessageForEvent` 物化 Airflow 链接)、`api/v1/openlineage_dataset_test.go`(聚合行→解析结果映射、解析失败回退、请求过滤映射、schema 字段与列血缘就绪、job/run 转换)、`store/openlineage_dataset_test.go`(聚合 SQL 形状:GROUP BY、过滤在 HAVING、封顶、不含 `raw_payload`;拼写谓词按对绑定、不跨对组合;默认投影不取 payload)。
- 集成(真实服务器 + PostgreSQL):新增 `TestOpenLineageDatasetPagesReadMaterializedReferencesRealServerIntegration`——摄取两个 run 后断言 `ListOpenLineageDatasets` 的源/目标 job 数与列血缘徽章、`GetOpenLineageDataset` 的 schema 字段与就绪标记、related jobs/recent runs、dataset 筛选项、run 列表带 Airflow 链接而 `raw_payload` 为空、`GetOpenLineageRun` 仍返回原文、task 列表带链接、单事件超限 413、超长身份 400、删除 run 后引用随外键级联消失且数据集页不再列出。
- **反向验证**(以 `go test -overlay` 探针单独回退,不改工作区;随后恢复):
  1. 把 `openLineageRunPayloadColumn(false)` 还原为 `"raw_payload"` ⇒ `TestOpenLineageRunPayloadColumn` 变红(`Should not be: "raw_payload"`);
  2. 删掉 dataset 数上限检查 ⇒ `TestValidateEventLimitsRejectsTooManyDatasets` 变红(`An error is expected but got nil`);
  3. 从 upsert 事务里删掉 `replaceOpenLineageRunDatasets` 调用 ⇒ 上述集成用例在第一条列表断言处失败(`"[]" should have 2 item(s), but has 0`)。
  
  第 3 条需注意:集成套件由测试进程再 `go build` 出服务器二进制,只给 `go test` 传 `-overlay` 不会作用于该子进程(实测此时用例仍绿);探针必须以 `GOFLAGS=-overlay=…` 传入,子构建才会一起生效。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`(41 包全绿)、`make test-integration-smoke`(真实 PostgreSQL + MySQL + migrator,含 LATEST.sql 与增量链一致性用例,runner 40.2s、migrator 19.3s)、release 构建(`-tags release`)。本轮未改前端与 proto,未跑前端门禁。

**独立对抗式复核后的加固(commit `db09dac`)**

主修复 `a691632` 之后,由独立子代理做对抗式只读复核(scratch worktree + `go test -overlay` 探针,未改动本仓库)。它确认了主修复的核心结论(默认投影不取 `raw_payload`;task 列表 join 物化列;单事件与批处理两条摄取路径都过 `ValidateEventLimits`;身份字段封顶覆盖全部唯一 btree 索引;终态 run 拒绝的重复投递不碰引用;`0015` 与 `LATEST.sql` 一致;外键级联有效;三条反向验证均可复现),但指出"一次请求仍能读到长度不受限的、由 payload 派生的字节",即内存放大只是被缩小而非消除。逐条复核后处理如下:

1. **列表仍在读未封顶的派生字符串(高,已修)**:`openlineage_run` 的 `producer`/`integration`/`processing_type`/`event_type`/`parent_*`/`root_*` 只受 1MiB 事件上限约束,而 `airflow_run_log_url` 经 `url.String()` 百分号转义后可达事件的约 3 倍(子代理实测 240144 字节的事件产出 720009 字节的链接);一页 1001 行因此仍可能读到 GB 级。现全部按字段封顶并答 400(eventType 64、producer 512、integration/processing_type 64、parent/root 名字与 run id 各 255),Airflow 链接超过 2048 字节时**丢弃链接而不是拒绝事件**(链接是可选展示字段,不该让合法事件失败;丢弃记 debug 日志)。单元测试钉住每一条,集成用例断言超长链接的那一行存储为空、payload 仍可取回,且正常链接原样入库。
2. **数据集详情展开全部引用行的 facet JSON(中,已修)**:最佳 schema 的 `ORDER BY jsonb_array_length` 与 column-lineage 字段名的 `jsonb_array_elements_text` 会 detoast 该数据集历史所有引用(子代理实测 400 条 × 320KB facet ⇒ 10.8s、140MB 外部归并、35330 个临时块)。现在 facet/job/run 三类查询改为读一个"最近 100 条引用"的有界子查询(`openLineageDatasetRecentRefsCTE`),并把 `(namespace, name)` 索引改为 `(namespace, name, event_time DESC NULLS LAST)` 让子查询能提前停止(索引改动落在同一份 `0015` 里——该迁移尚未上线);摄取侧同时把单个 schema facet 封顶 64KiB、column-lineage 字段名数组封顶 32KiB,超出的尾部丢弃而事件仍被接受。**本轮实测**:400 条引用、其中每条约 90KB(故意超过摄取侧上限的直接 SQL 探针)的库上,`GetOpenLineageDatasetDetail` 从旧实现的 10.8s 降到 67ms。语义收紧一处:最佳 schema 由"全部历史里最宽"变为"最近 100 条引用里最宽"(最近 10 个 run、最近 8 个 job 不受影响,它们必在这 100 条之内)。
3. **集成/来源数组无界且顺序不确定(低,已修)**:`ARRAY_AGG(DISTINCT …)` 原先没有 `ORDER BY`(旧 Go 实现用 `sortedKeys` 排序),且一个 producer 每事件换一个 integration 就能把单个分组的数组撑大;现按值排序并切到最多 16 项。相关 job 的 integration 改为取**最新 run** 的(与旧 Go 行为一致),不再是词序 `MAX`。
4. **columnLineage 字符串仍能把索引写爆(中,已修;属 L15 家族)**:facet 的字段名、`inputFields[].field`、`dataset[].field` 以及 facet 内引用数据集的 namespace/name 此前完全不受检查,超长值会在 `column_lineage` 的 `source_column`/`target_column` 索引上以 500 失败(子代理以 40000 字节随机十六进制名复现 `index row requires 40016 bytes, maximum size is 8191`)——而 run 行此时已提交,生产者只能永久重试。现按 512/512/255 封顶并答 400。
5. **facet 内新列 `schema_fields`/`column_lineage_fields` 的 JSONB 约定(F9,记录)**:它们没有 `COMMENT ON COLUMN` 绑定 store 消息,与 `raw_payload` 同样存原始 facet JSON;`backend/AGENTS.md` 的 "JSONB 绑定 proto/store 消息" 因此又多了一处显式例外,两列的 SQL 注释已写明。

**加固新增/扩展的测试**:单元——`plugin/openlineage` 的 `TestValidateEventLimitsRejectsOverlongColumnLineageStrings` 与扩展后的超长字段表(producer、parent/root、eventType、integration、列名/facet 内 dataset 名);`api/v1` 的 `TestRunMessageForEventDropsAnOverlongAirflowLink`、`TestOpenLineageDatasetRefsCapsTheStoredFacets`(截断且保留前导项、字段名排序);`store` 的 `TestOpenLineageDatasetReadsAreBounded` 钉住数组排序/切片与详情窗口 SQL 的形状。集成——`TestOpenLineageIngestionBoundsTheStoredValuesRealServerIntegration`:超长 Airflow 链接被丢弃而事件与其 payload 仍入库、producer/eventType/列名超长答 400、3000 字段的 schema 被截断而详情仍可用、单事件 150 个数据集全部建引用(跨 INSERT 分块)、`integrations` 按值排序且 job 报最新 run 的 integration、上限内的链接原样入库。

**加固后的验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test ./backend/...`(41 包全绿)、`make test-integration-smoke`(真实 PostgreSQL + MySQL + migrator,含 `0015` 索引改动后的 LATEST/增量一致性,runner 39.7s、migrator 18.7s)、release 构建。另有一次一次性性能探针(未入库):400 条引用、每条约 90KB facet 的库上 `GetOpenLineageDatasetDetail` 耗时 67ms。

**复核结论中不成立的一条(F4)**:复核称 SPA 任务详情页的"关联数据集"面板因本次改动永久为空,属新引入的前端回归。实测**不成立**:基线 `591c664` 上 `ListOpenLineageRuns` 走的是 `convertOpenLineageRun(run, false)`,`RawPayload` 只在 `includePayload=true` 时赋值(自功能提交 `6c565d7` 起即如此,探针 `TestProbeBaseRunListNeverCarriedThePayload` 在基线 worktree 上通过),即 run 列表从未返回过 payload——该面板自功能引入以来就是空的,与本次改动无关,是"前端按 payload 聚合、列表接口却不给 payload"的既有契约缺陷。修它需要把每个 run 的数据集引用放进列表响应(proto + 前端)或把该聚合搬到服务端,属独立改动,本轮未做。

**加固后的残余/代价(记录而非修复)**

以下三条随后均已修复(commit `1253a9d`,见 §10 的 2026-10-07 条目),原文保留以便追溯:

- 数据集列表的 `GROUP BY namespace, name` 仍要扫描全部引用行才能确定最近 5000 个分组(`LIMIT` 无法先于 `GROUP BY`);复核在 100 万引用行上实测约 4.5s。应用侧内存仍有界(≤5000 个小行),但每次列表请求都要付这份库内代价。彻底解法是像 `openlineage_task` 那样在摄取时维护一张逐数据集聚合表(或定期物化),属后续改动。
- 列表与详情的封顶窗口不一致:带 namespace/integration/source 过滤的列表可以把过滤下推到 SQL,从而显示超出"未过滤最近 5000 个数据集"的项,而详情按未过滤聚合解析 GUID,超过该窗口时可能 404;详情另有 64 个拼写上限。
- 详情 summary 的 `COUNT(DISTINCT task_guid)` 仍在数据集自己的全部引用上聚合(只有 facet/job/run 三类查询被窗口化),对单个被灌爆的数据集仍是 O(行数) 的库内代价。

**残余/注意**

1. **数据集列表与详情受 `maxOpenLineageDatasetGroups = 5000` 限制**:只覆盖最近见过的 5000 个数据集(按最近事件时间),更老的数据集列不出来、也打不开(详情按同一封顶聚合把 GUID 解析回拼写)。这是旧实现"最近 5000 个 run"窗口的同族上限,但每行更小且不解析 payload;超过该量级的部署需要把解析也搬进 SQL(见第 3 条)。
2. **不回填**:升级前已入库的 run 没有物化引用与 Airflow 链接列,这些旧事件的数据集不会出现在数据集页,旧 run 的 Airflow 链接也会消失,直到事件被重新摄取。项目尚未上线,故未做迁移回填;若要在已上线环境升级,需要一次性回填作业(可用 SQL 从 `raw_payload` 抽取,`0015` 里刻意没有写入数据)。
3. **dataset 的作用域过滤与自由文本检索仍在应用侧**(需要解析 namespace → 实例),SQL 只负责聚合与可下推的过滤,所以仍是"一次请求解析 ≤5000 个数据集分组",而不是"一次请求零工作"。要消除这最后一段,需要把解析结果物化(或在解析变更时失效),代价是 namespace mapping / 实例改动后需要重解析。
4. **物化 schema facet 会增加存储**:每个 (run, dataset) 引用各存一份 schema facet 与 column-lineage 字段名,数据集详情因此不必回读 payload;这使 `openlineage_run_dataset` 的行比纯计数表大,换取读路径完全不碰 `raw_payload`。
5. **`GetOpenLineageRun` 仍按需返回整段 payload**(契约不变),因此单 run 详情不是"不读 payload"。注意 1MiB 上限约束的是**事件字节**:JSONB 重新编码后实际存储可达约 2.0 倍,接口返回的字符串另约 1.18 倍(子代理实测),即"最多一份 1MiB"并不成立,加固一节已更正。

### 2026-10-07 —— M18 的三项残余已修复(commit `1253a9d`)

上一节列出的三条残余本轮全部关闭:数据集列表每次请求仍扫全部引用行的库内代价、过滤列表与详情窗口不一致导致的 404、以及详情 summary 在单个数据集全部引用上聚合的代价。三条同源——数据集页从引用表作答——所以一次改动一起解决。

**摄取时维护逐数据集聚合表**(迁移 `0016##openlineage_dataset_aggregate.sql`、`backend/store/openlineage_dataset_aggregate.go`)

- 新增 `openlineage_dataset`:每个数据集一行,保存 `ref_count`、`column_lineage_ref_count`、按方向的 job 数(`source_job_count`/`target_job_count`)与 `last_seen`(最新引用的事件时间)。行存在即"该数据集仍有引用",最后一个引用消失时删除该行。
- 新增 `openlineage_dataset_member`:每个 (数据集, kind, value) 一行,`ref_count` 为携带它的引用数;kind 为 `task:input`/`task:output`(value 是 task GUID)或 `integration`/`source`。job 数就是对应方向上的 task 行数,列表显示的数组从 value 行读出。这正是让聚合在重投递下**精确**而不是单调的机制:引用被替换掉时成员计数减一,减到 0 就删除该行并回退对应计数;`last_seen` 只在"被移走的那条可能正是最新"时才回读 `MAX(event_time)`(走既有 `(namespace, name, event_time)` 索引)。成员行主键里最长的是 task GUID,摄取侧已封顶,仍在 btree 索引项上限之内。
- 摄取侧(`openlineage_run.go`):写入引用前先读回该 run 原有引用,把「旧集合 → 新集合」折算成每数据集一份增量。完全相同的引用相互抵消,所以"重投递但内容没变"不写任何行;整个批次的增量在**所有 task 锁取完之后**统一施加,加锁顺序因此是全局的(先按 task 升序、再按 (namespace, name) 升序),两个批次不会因为"先持数据集锁、再等 task 锁"而成环。数据集行锁由 `INSERT ... ON CONFLICT DO UPDATE`(空更新)取得,计数在同一持锁事务内读改写,并发摄取同一数据集不丢更新。
- 保留策略(`maintenance.go`):批量 `DELETE` 无法逐条告知聚合,`DeleteOpenLineageRunsBefore` 改为先把"这次会删到的引用所属数据集"(`DISTINCT namespace, name`)暂存进事务级临时表(不占应用内存),删完 run(级联删引用)后:只剩空壳的数据集行连同成员行删除,仍被引用的数据集按剩余引用重建聚合与成员行。

**列表、筛选项与详情读写同一个窗口**(`backend/store/openlineage_dataset.go`、`backend/api/v1/openlineage_dataset.go`)

- 列表不再 `GROUP BY` 引用表:窗口 CTE `dataset_window AS MATERIALIZED` 先取 `last_seen DESC` 的最近 `maxOpenLineageDatasetGroups`(5000)个数据集,过滤器在其**之后**施加;integration/source 过滤变成成员表主键上的 `EXISTS` 等值探测,显示的数组通过主键前缀的有序 `LIMIT 16` 读取(不扫描数据集的值)。`MATERIALIZED` 是刻意写下的栅栏:过滤条件不可能被下推到窗口的 `LIMIT` 之下——实测当前规划器本来也不会下推,写出来是为了让"过滤不能越过窗口"成为查询本身的性质,而不是规划器的巧合。
- namespace / integration / source 三个菜单维度同样只读这个窗口(每个数据集最多 16 个值),所以菜单里出现的值一定能筛出结果,列表里出现的数据集一定在窗口内;数据集页最后一次全引用表聚合随之消失。这三项计数的语义由"引用数"变为"窗口内数据集的个数"。
- 详情按 GUID 解析拼写改用同一个窗口(`ListOpenLineageDatasetWindow`),所以"过滤后的列表能显示、详情却 404"不再可能:过滤只能在窗口内缩小,不能在窗口外扩张。summary 改为读聚合行(单一拼写 O(1));只有同一数据集被多种拼写上报时才退回成员行去重(同一 job 两种拼写仍只算一个 job),facet/job/run 三类查询仍走"最近 100 条引用"的有界子查询。
- 一处语义收紧:菜单能提供的值也受这个窗口限制——超出窗口的值不再出现在菜单里(与列表可达范围一致)。

**回归测试**

- 单元(`backend/store`):`openLineageDatasetDeltas` 的增量折算(无变化不写、引用被移除为负、方向迁移、空 integration/source、同一批多个 run 合并为一份、数据集顺序稳定);列表查询形状(窗口先于过滤器、`MATERIALIZED`、不再出现 `openlineage_run_dataset`/`GROUP BY`/`raw_payload`);窗口 CTE 为列表与详情共用;成员数组读取有界;拼写谓词带表别名。
- 集成(真实服务器 + PostgreSQL):`TestOpenLineageDatasetAggregateFollowsARedeliveryRealServerIntegration` —— 重投递撤回引用后 `ref_count`/`target_job_count`/`column_lineage_ref_count` 与 `last_seen` 一起回落(被撤的正是最新的那条,`last_seen` 必须退回到剩下的那条)、最后一个引用消失后数据集行与成员行一并消失、列表与详情同步;`TestOpenLineageDatasetAggregateFollowsTheRetentionPruneRealServerIntegration` —— 驱动真实保留策略 prune,断言只剩空壳的数据集被删除、仍被别的 run 引用的数据集按剩余引用重建(含成员行);`TestOpenLineageDatasetWindowKeepsTheListAndDetailAlignedRealServerIntegration` —— 该用例刻意不并行,直接插入 5000 行"填满窗口"的聚合行与 2 行被挤出窗口的聚合行,断言按 namespace 过滤的列表为空(过滤不能越过窗口)、被挤出窗口的数据集详情 404、窗口内的数据集详情可读。既有 `TestOpenLineageDatasetPagesReadMaterializedReferencesRealServerIntegration` 里"删掉 run 后数据集页不再列出"一段改为驱动保留策略 prune(原先的裸 `DELETE` 只是 prune 的替身,而聚合的维护路径正是 prune),外键级联断言保留。

**反向验证**(逐条单独回退,对应用例变红,随后恢复;探针直接改工作区后用 `git checkout` 还原)

1. 成员计数减到 0 时不回退 job 数(`return false, true, nil` → `false, false, nil`):重投递用例失败(`target_job_count` 期望 1、实际 2)。
2. 去掉 `last_seen` 回读:重投递用例失败(期望退回到 `base`、实际留在 `base+1min`)。
3. 去掉保留策略里的 `reconcileOpenLineageDatasets`:prune 用例失败(`the dataset whose only reference was pruned is gone`,计数 1≠0)。
4. 把列表改回"先过滤再封顶"(即修复前的形状):窗口对齐用例失败(`a filter must not reach past the window`,该 namespace 本应为空却返回 1 行)——正是修复前"列表给得出、详情 404"的那一类项。
5. 只去掉 `AS MATERIALIZED` 而其余不动:**不会**变红——当前规划器不会把外层过滤下推到子查询的 `LIMIT` 之下。窗口对齐用例因此钉住的是"列表结果不会超出窗口"这一行为,而不是某种查询写法。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test -count=1 ./backend/...`(全绿)、默认与 release 构建、`make test-integration-smoke`(真实 PostgreSQL + MySQL + migrator,含 `LATEST.sql` 与增量链一致性;runner 49.3s、migrator 19.8s)。本轮未改前端与 proto,未跑前端门禁。

**残余/代价(记录而非修复)**

> 以下四条中的前三条随后已处理、第四条部分处理(记忆化),见 §10 的下一条条目。

- **摄取成本增加**:每条引用除自身外还要维护最多 3 行成员(task/integration/source)与一个数据集行,换来的是列表/详情不再对全部引用行做聚合;`openlineage_dataset_member` 因此约为引用行数的 1–3 倍,存储相应增加。
- **保留策略重建与并发摄取之间是既有形状的竞态**:重建按语句快照重算,期间提交的摄取增量可能被覆盖,直到该数据集再次被摄取或再次 prune。`openlineage_task` 的重建(加固前的 M18 修复)同形,属既有设计,不是本轮引入。
- **在 store 之外直接删 run(手写 SQL)会让聚合行停在旧值**,直到保留策略对该数据集重算——与 `openlineage_task` 的"读不修行,等下一次摄取或 prune"同族;`DeleteOpenLineageRunsBefore` 是受支持的删除路径。
- **解析仍在应用侧**:一次请求仍要解析 ≤5000 个数据集分组(作用域过滤与自由文本检索需要 namespace → 实例解析),没有变成"零工作";要消除它需要物化解析结果并在 namespace mapping / 实例变更时失效。

### 2026-10-07 —— M18 残余的四项优化(commit `c05ff2c`、`794812f`、`226e8d7`、`18abe4b`)

上一条目列出的四条残余本轮逐一处理:解析仍按数据集各查一次、摄取维护逐数据集发语句、保留策略重建与并发摄取的竞态、以及 store 之外删 run 留下的空壳聚合行。

**解析按 namespace 记忆一次(commit `c05ff2c`)**

- 现状(实测):读路径解析窗口里每一行拼写时,`Resolver` 对 dataset 预览与实例列表有请求级记忆,但每次预览开头的 `GetNamespaceMapping` 没有——窗口 5000 行就是 5000 次查询,窗口对齐用例里一次详情请求约 2s,几乎全在这里,比聚合本身贵得多。
- 做法:把「一个 namespace 解析成什么」(mapping 命名的实例、该实例本身、以及 host:port 命中的实例)按 namespace 记忆在请求级;mapping 命中的 namespace 不再去列实例(与记忆化之前的路径一致,那时它在这步之前就返回了)。摄取用的 `NewResolver` 不记忆,`requestScoped=false` 时既不读也不写缓存,所以两次事件之间新登记的 mapping/实例对下一个事件仍然可见。
- 可测性:`Resolver` 改为接收 `ResolutionStore` 接口,这是让"一条请求对一个 namespace 只查一次"能被测试钉住的原因——单元用例用计数假 store 断言 200 个数据集一次查询、两个 namespace 两次、以及非请求态下不记忆(每次调用都重新查)。
- 反向验证:去掉缓存写入后 `TestResolveDatasetPreviewLooksUpOneNamespaceOnce` 变红(期望 1、实际 200)。

**摄取维护批量化(commit `794812f`)**

- 现状:每个被触及的数据集约 5 条语句(加锁取状态 1 + 成员增删最多 3 + 计数写 1),而一次事件最多可点名 1000 个数据集(`MaxEventDatasets`),于是单个事件在引用写入之外还要约 5000 次往返——`MaxEventDatasets` 留下的放大面。
- 做法:按语句类批量化——① 一条 `INSERT ... ON CONFLICT DO UPDATE ... RETURNING` 批量加锁并读回每个数据集的状态;② 批量 upsert 正增量成员(`RETURNING` 的计数等于该行增量即为新建,据此加 job 数);③ 批量 `UPDATE ... FROM (VALUES ...)` 负增量成员;④ 批量删除归零成员(据此减 job 数);⑤ 批量写回计数并在同一条语句里用 `CASE` + 相关子查询回读被撤走最新引用者的 `event_time`。锁序不变:所有数据集行在任何成员行之前、按 (namespace, name) 顺序加锁,批次仍是"先 task 升序、再 dataset 升序"。语句按行数分块(每块 1000 行),因为参数个数有上限而一个批次可以点名任意多个数据集。
- 新助手:`valuesList` 渲染带列级 cast 的 `VALUES` 列表——`FROM` 里的 `VALUES` 拿不到目标列类型(列定义列表只对返回 record 的函数合法),占位符要跨整表编号,单测钉住这两点。
- 测试:单元 `TestValuesListNumbersPlaceholdersAcrossRows`;集成扩了"一个事件 150 个数据集"用例(重投递只留 100 个,被丢掉的 50 个必须一并消失),并新增 `TestOpenLineageDatasetAggregateChunksALargeBatchRealServerIntegration`——一个事务里两个各 1000 个数据集的事件(共 2000 数据集、6000 成员行)全部落库、再重投递各留 1 个,1.4s。
- 不变量用例:`TestOpenLineageDatasetAggregateMatchesAFullRecomputeRealServerIntegration` 把重投递、数据集在两个方向间迁移、同一 run 双向引用同一数据集、无 integration facet、无 `event_time` 等形态走一遍后,把存储的聚合(**数据集行与成员行**)逐行与"从引用表重算"的结果对比——后者正是保留策略重建用的那段 SQL,于是"增量维护 = 全量重算"成为一条被钉住的不变量。反向验证:把成员增量从累加改成覆盖即变红。
- 反向验证:去掉"成员归零时回退 job 数"后重投递用例变红(期望 1、实际 2)。

**保留策略先锁后算(commit `226e8d7`)**

- 现状:重建是"按语句快照算出的绝对赋值"。当一个在途写入者已写好引用、持有聚合行锁、尚未提交时,重建语句会在该行锁上等待,而它算出的 `EXCLUDED` 值来自等待前的快照——写入者提交后这次重建把它的贡献覆盖掉。`DELETE ... WHERE NOT EXISTS (引用)` 那一步是同一窗口的镜像。
- 做法:先把受影响的数据集行建出来(`INSERT ... ON CONFLICT DO NOTHING`),再按 `(namespace, name) COLLATE "C"` 顺序 `FOR UPDATE OF ds` 加锁(与摄取路径的 Go 字节序一致),然后才做删除空壳/重算/重建成员。持锁之后的语句快照一定包含所有先到的写入者;后到的写入者在同一把锁上排队,拿到锁后基于重建值施加自己的相对增量。同时把 task 重算移到数据集重算之前并按 task GUID 排序——两边统一为"先 task 升序、再 dataset 升序",顺带修掉保留策略原先按 map 随机序取 task 锁这一既有隐患。
- 测试:`TestOpenLineageDatasetPruneLocksBeforeItRecomputesRealServerIntegration` 用一个第二连接扮演"在途写入者"(run 与引用已写、按摄取路径的方式锁住聚合行、未提交),在 prune 进行中查询 `pg_stat_activity`:断言它阻塞在聚合行准备语句上,并且**尚未触碰 `openlineage_run_dataset`**;提交后断言重建把该写入者的引用算了进去(`ref_count`/`target_job_count` 与 `last_seen` 都来自它)。
- 反向验证:回退这段后同一用例变红——prune 阻塞在 `DELETE ... NOT EXISTS (SELECT 1 FROM openlineage_run_dataset ...)` 里,即在写者持锁期间就已经在读它不该看到的引用。

**空壳聚合行清扫(commit `18abe4b`)**

- 现状:在 store 的路径之外删 run(手写 `DELETE`、运维清理)会级联删掉引用,但留下的聚合行不会有人再碰——页面继续列出没有任何引用的数据集,详情也给不出 job/run。
- 做法:`DeleteEmptyOpenLineageDatasets(olderThan)` 删除"没有任何引用且 `updated_at` 早于 `olderThan`"的聚合行;维护 pass 每次都跑(与是否设置保留窗口无关,它不是保留策略),grace 为 1 小时。grace 是必要的:尚未提交的写入者对本 pass 不可见,而它即将填的那一行不能被清掉。
- 测试:`TestOpenLineageEmptyDatasetSweepRealServerIntegration` 手工删掉一个 run,断言壳仍在、grace 内不清、超过 grace 只清掉无引用那一个、被另一个 run 引用的留下。
- 反向验证:去掉 `updated_at` 守卫后该用例变红(grace 内那条也被清掉,`deleted` 由 0 变 1)。
- 已知边界:`DeleteEmptyOpenLineageDatasets` 只清空壳,不修计数——引用的**部分**被越界删除造成的计数漂移,要到该数据集下次被摄取或再次 prune 才回到一致。

**验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test -count=1 ./backend/...`(全绿)、默认与 release 构建、`make test-integration-smoke`(真实 PostgreSQL + MySQL + migrator;runner 40.7s、migrator 20.1s)。本轮未改前端与 proto,未跑前端门禁。

**独立对抗式复核后的加固(commit `92a8bd8`、`940cfbb`、`4d0fd7f`)**

上一条目的四项改动随后交独立子代理做对抗式只读复核(HEAD `ac28ff7`;探针全部挂在 `/tmp/probe`,以 `go test -overlay` 与真实 PG 16 集成套件注入,未改动仓库文件)。它用"增量结果 vs 从引用表重算"的差分探针覆盖了批量、方向迁移、空 integration、1800 数据集跨分块、分块删除等形态,确认了计数与每一项成员行都与重算一致,并实测了 13 种 namespace 形态下记忆化解析与逐次解析结果完全相同。它同时指出下列问题,均已修掉:

1. **`last_seen` 不再是引用的 `MAX(event_time)`(高,已复现,基座 `1253a9d` 起就有)**:`openLineageDatasetDeltas` 会把"引用数、列血缘、成员都抵消"的增量当作"什么都不用写"丢掉,但这类增量仍然带着 `removed` 与新的 `lastSeen`——一次只改了引用事件时间的重投递(任何被读取的数据集,其输入引用从不带列血缘标志,所以普通的 START→COMPLETE 就会命中)因此不写任何行,而此后没有任何东西会重算它。端到端复现:同一 run 两次 COMPLETE、输入相同、`eventTime` 从 t0 改到 t0+1h,`last_seen` 仍停在 t0,而 `MAX(ref.event_time)` 是 t0+1h;列表窗口按 `last_seen` 排序,于是正在被写入的数据集可能因陈旧时间被挤出 5000 窗口,详情随即 404——正是基线修复要消除的那类现象。现在只有"确实无可写"的增量才会被丢弃。反向验证:去掉新增的 `!delta.removed && delta.lastSeen == nil` 条件,单元用例 `TestOpenLineageDatasetDeltas/a replacement that only moves a reference's event time still writes` 变红(`"[]" should have 1 item(s), but has 0`),把该形态加进不变量用例后集成侧同时变红。

2. **保留策略与摄入互相死锁(中高,已复现,属既有形状但 `226e8d7` 的注释称锁序已对齐)**:摄入的顺序是 task 行 → run 行 → 引用 → dataset 行,而 prune 是"先删 run 行 → 注册表 → task 重算(此时才取 task 行)"。生产者在 prune 正在删除某 run 时重投递该 run:摄入先拿到 task 行,再去写已被删除但未提交的 run 行;prune 持有该 run 行,稍后又要 task 行——成环。复现(probe `zz_probe_deadlock_test.go`):把 prune 停在批量 DELETE 之后(靠 `meta_registry_resource` 行锁),启动重投递,再放行;在 `ac28ff7` 上 prune 成功、投递得到 HTTP 500,服务端日志 `ERROR: deadlock detected (SQLSTATE 40P01)` 于 `upsertOpenLineageRunImpl`——该投递的 lineage 完全没写进去(prune 的后半段(注册表清理、task 重算、数据集重建)可以持续很久,窗口很宽)。现在 prune 在删除 run 之前先按 task GUID 顺序把将要重算的 task 行锁住(upsert + 空更新),两条路径于是都是"先 task、再 run、后 dataset"。回归用例 `TestOpenLineagePruneAndIngestShareATaskRealServerIntegration` 复刻同一交错并断言两侧都成功;反向验证:去掉这段前置加锁,该用例变红(投递 500、日志 40P01)。这一改动同时让复核提到的"prune 重建后又把在途摄入的旧引用减一遍"窗口不可达——摄入不可能不持有 task 锁就停在重建与自身状态之间。

3. **空壳清扫可能删掉仍有引用的聚合行(中,已复现,`18abe4b` 引入)**:`updated_at` 写的是 `NOW()`,即**事务开始**时间;一个早于 grace 开始、晚于 grace 提交的事务,提交后行版本仍低于截止时间。清扫语句在写者持锁时阻塞,提交后 EvalPlanQual 用旧快照重算 `NOT EXISTS`,看不到刚提交的引用,于是把行删掉(其成员行随外键级联消失),留下"引用在、聚合无"的状态:页面不再列出该数据集。psql 复现:0 行聚合、1 行引用。现在清扫先把候选行按 `(namespace, name)` 字节序锁定并 `SKIP LOCKED` 跳过任何仍被写者持有的行,"没有提交的引用"因此不会被误判,age 条件只作为"不打扰热行"的保守约束保留(它本身不足以判定,注释里已写明)。回归用例 `TestOpenLineageEmptyDatasetSweepSkipsARowAWriterHoldsRealServerIntegration` 按摄入的方式持锁+写引用,断言清扫既不等待也不删除;反向验证:去掉 `SKIP LOCKED` 后该用例变红(`the sweep waited for a writer's row lock instead of skipping it`,随后提交即删除)。

4. **复核在过程中拦下的一处回归(只存在于未提交工作区,已修,记录备查)**:前置加锁语句的第一版写成 `SELECT DISTINCT ... ORDER BY task_guid COLLATE "C"`,PostgreSQL 以 `for SELECT DISTINCT, ORDER BY expressions must appear in select list (42P10)` 拒绝——它是 prune 的第一条语句,会让保留策略静默停摆(只打一条日志),而当时新增的死锁用例因为 prune 立刻报错而表现为"等待超时"。改为 `ORDER BY 1`(选择列表里的 `task_guid` 自带 `COLLATE "C"`,按序数排序仍是字节序)后通过;复核用仓库自己的保留策略集成用例与真实列定义逐字复现了该报错。

复核**未能推翻**的部分(它列出的探针与理由):计数与全部成员行对"从引用表重算"的差分在所有形态下一致;`refCount == delta` 的"新建"判定与 `refCount <= 0` 的"耗尽"判定构造不出反例,行不会出现负计数或残留;分块不破坏"先锁全部数据集、再动成员"的次序,最坏参数数 8000/语句远低于上限,`GREATEST(timestamptz, …)` 与 NULL 语义、`int`/`bigint`、以及 `FROM` 里的 `VALUES` 列类型都与预期一致;prune 的 `SELECT ... FOR UPDATE` 经 `EXPLAIN` 确认是 `LockRows → Sort`,按 `COLLATE "C"` 排序与 Go 的字节序一致;请求级 namespace 记忆化在 13 种 namespace 形态 × 4 个数据集名下与未记忆化解析给出相同 GUID 与 Internal 标志。

**加固后的验证门禁**:`gofmt`、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test -count=1 ./backend/...`(全绿)、`make test-integration-smoke`(真实 PostgreSQL + MySQL + migrator;runner 41.5s、migrator 19.4s)。四项修复各自附了反向验证,另加"增量 = 全量重算"的不变量用例 `TestOpenLineageDatasetAggregateMatchesAFullRecomputeRealServerIntegration`(数据集行与成员行都逐行对比,反向验证:把成员增量从累加改成覆盖即变红)。

**复核未能验证的部分**(记录):并发大批次的压力测试、100 万数据集的最坏规模、多副本下两个 prune/清扫同时运行、以及前端渲染陈旧 `last_seen` 的效果——都只有结构论证或 SQL 级证据。

**残余**

1. **解析仍未物化进 SQL**:窗口内的作用域过滤与自由文本检索仍需 app 侧解析(检索命中面包含"解析后的目标"),现在只是把每次解析的成本降到纯 CPU。要彻底去掉"详情受窗口限制",需要把解析结果(guid/resolved_target/internal)落库,并配一套失效机制(mapping/实例变更时按 namespace 重解析,或给聚合行加 epoch 让读路径只对已读到的 ≤5000 行重解析)。
2. **空壳清扫是每 6 小时一次 O(数据集数) 次索引探测**,并把"越界删除"的可见后果限制在 grace + 一个 pass 之内(见上面的加固:它只删"没有任何引用且没有被写者持有"的行);要让它彻底不可能静默,可选 `BEFORE DELETE ON openlineage_run` 守卫触发器(要求 store 的删除路径 `SET LOCAL` 放行),但那会与既有"刻意越界删除以验证读路径优雅降级"的用例冲突,属策略变更而非修复。
3. **摄取成本仍是每条引用最多 3 行成员**:批量化只把往返次数与数据集数解耦,不减少行数;若要把行数也降下来,可把 integration/source 两个维度收进数据集行的一份 JSONB 计数映射(代价是热数据集每次摄取都要重写该行)。

### 2026-10-07 —— M19 已修复(commit `8d574c3`);M20 经确认按决策保留

**M19 MySQL-wire DSN 由目标库库名拼接**(`backend/plugin/db/mysql/`、`backend/plugin/db/starrocks/`)

- 新增 `dsn.go` 的 `BuildDSN`:`DBName` 经 `url.PathEscape` 转义——这正是驱动自身 `Config.FormatDSN` 对 `DBName` 的处理方式,所以转义后的库名经 `ParseDSN` 原样还原。MySQL 与 StarRocks/Doris 两个 MySQL-wire 驱动都改走该构造函数,不再各自 `fmt.Sprintf` 拼接模板(StarRocks 的模板与 MySQL 同型,同样受影响;报告只引用了 `mysql.go:141`)。
- 攻击面复核:库名由同步链路从目标实例 `information_schema` 读出(`backend/runner/schemasync/syncer.go` 的 `DatabaseName: databaseMetadata.Name` / `database.DatabaseName`),不需要任何平台侧权限。修复前,目标库里一个名为 `` x?tls=false& `` 的数据库会把 DSN 路径段在此结束、把其余部分当作驱动参数解析:平台以登记的高权限同步账号连到 `x` 且 TLS 关闭;把 `tls` 换成 `multiStatements`(`?multiStatements=true&`)、`allowAllFiles` 或另一个库名同理。修复后库名只是库名,不存在可注入的参数段。
- 未采用 `mysql.Config` + `mysql.NewConnector` 的完全结构化改造:那会连带改变 `extra_connection_parameters` 的现有语义(驱动选项与会话变量透传),而该字段的写入需要实例/数据源写权限(见下),不属于未授权路径,按决策本轮保持其行为不变;结构化构造带来的其余收益(如 `uuid[:8]` dial 协议,L21/D13)不在本轮范围。

**回归测试**

- 单元 `backend/plugin/db/mysql/dsn_test.go`:`BuildDSN` 对 `app`、`x?tls=false&`、`x?allowAllFiles=true&`、`a&b=c`、`a#b`、`a/b`、`100%`、`?` 逐一断言 `ParseDSN` 后 `DBName` 原样还原、`TLSConfig` 为空、`MultiStatements`/`AllowAllFiles` 为假、没有新增 `Params`;`TestRawDSNInterpolationRewritesTheConnection` 把修复前的裸拼接 DSN 作为可执行描述钉住威胁模型(它能解析成功,静默把 `DBName` 变成 `x` 并令 `TLSConfig=false`——正因不报错才危险);`TestGetMySQLConnectionEscapesCatalogDatabaseName` 钉住驱动自身的调用点,而不只是构造函数。
- 集成(真实 MySQL):`TestMySQLSyncConnectsToCatalogDatabaseNameWithDSNSyntaxRealServerIntegration` 在真实服务器上创建 `` it_dsn_<hash>?tls=false& `` 数据库与 `dsn_probe` 表,登记实例、使其可见并触发单库同步,要求 `dsn_probe` 的元数据注册项出现在该库下。
- **反向验证**:把 `getMySQLConnection` 还原为裸拼接后,单元用例变红(实际 `DBName` 为 `x`);集成用例也变红——同步连到被截断的 `it_dsn_<hash>`,目标报告该库不存在,`markDatabaseDeleted` 把它标记为 deleted(`database ... has been deleted`),元数据永远不出现;恢复修复后两者全绿。

**M20 PG/MSSQL `extra_connection_parameters` 零校验 —— 保留(经确认的决策)**

- 该字段只能经实例/数据源写权限写入(`workspaceAdmin`,可下放给自定义角色),而持有者本就能改同一资源的 `host`/`port`/`username`/`password`/`ssh_host`;参数级白名单只约束一个已被信任的主体改变连接目标的形式,不构成授权边界,因此本轮不改(MySQL/StarRocks 既有的 `allowAllFiles` 键黑名单保持不变,但它不是执行点——见残余第 1 条)。
- 报告"来自被同步实例的快照更新链路时同样未过滤"的说法经复核不成立:`SyncInstance` 只是把 store 中已存的 `Instance.Metadata` 克隆后回写版本与 `lastSyncTime`(`backend/runner/schemasync/syncer.go`),`extra_connection_parameters` 不会由目标库内容产生或改写;写它的唯一路径是 API 请求(`backend/api/v1/instance_convert.go`)。该回写是无版本守卫的读-改-写(见残余第 2 条),但丢的是并发管理员的编辑,不是引入了目标库内容。
- 该决策已写入 `docs/security-posture.md`(连接参数的写权限即信任边界),M20 条目的状态指针同步更新。

**独立对抗式复核(只读子代理)**

- 复核目标是把 M19 的结论证伪,未改动本仓库(全部实验在 /tmp 副本)。穷举全部 ≤3 字节库名(16,777,472 个)、300 万随机长名与定向语料(`?` `/` `&` `=` `@` `%` `#` `\x00` 非法 UTF-8 反引号 换行 64 字符)后,无一名能改变 `ParseDSN` 的 `DBName`、打开 `TLSConfig`/`MultiStatements`/`AllowAllFiles`、注入 `Params`、改变 `Addr`/`User`/`Passwd`,也无合法名解析失败;两个驱动调用点均覆盖;反向验证独立复现(单元 `DBName="x"`;集成里真实 MySQL 把被截断的库标记 deleted)。全仓搜索确认,由目标 catalog 派生并进入连接串的值只有库名一处(PG 走 `pgx.ConnConfig` 字段、MSSQL 走 `url.Values`)。**M19 结论维持:未授权路径已关闭。**
- 复核同时确认上述 M20 判断,并指出下列残余;本轮不改行为,只在此记录。

**残余(本轮未处理)**

1. **`allowAllFiles` 键黑名单只查键,值仍是注入面(低,需实例写权限)**:`ValidateExtraConnectionParameters` 只看键名,`{"sql_mode":"x&allowAllFiles=true"}` 能通过校验并经 `BuildDSN` 拼进查询串,`ParseDSN` 得到 `AllowAllFiles=true`;`{"sql_mode":"x/y"}` 会让该实例的 DSN 直接解析失败(连接 DoS)。这与 M20 同属"需要实例写权限"的决策范围(该权限本就能改 `host`/`use_ssl`),不构成未授权路径,故本轮不收紧;若日后要收,应做键值校验或改用 `mysql.Config` 结构化构造。
2. **同步回写 `Instance.Metadata` 无 CAS(低,既有)**:`SyncInstance` 读-改-写整个 `metadata` 列(`backend/runner/schemasync/syncer.go` → `backend/store/instance.go`),期间提交的一次数据源编辑会被静默覆盖;不会引入目标库数据,属独立的并发加固项。
3. **StarRocks/Doris 没有服务端真实回归用例**:该驱动的同类缺陷由共享 `BuildDSN` 与单元用例覆盖,复核另用差分探针(还原裸拼接后 `Open` 在 `x?loc=NotALocation&` 等名称上失败)验证,但没有 StarRocks 集成用例;PG/MSSQL 的库名路径同样是"结构上免疫、无测试钉住"。
4. **复核覆盖范围**:只跑了两个驱动包、本轮新增用例与定向集成用例,未重跑完整单元与集成门禁(该门禁由本条目"验证门禁"一段记录)。

**验证门禁**:`gofmt`(无输出)、`golangci-lint run --allow-parallel-runners`(0 issues)、`go test -count=1 ./...`(全绿)、release 构建(`-tags release`)、`make test-integration`(真实 PostgreSQL + MySQL + migrator:runner 39.4s、migrator 18.5s,全绿,含本轮新增用例)。本轮未改前端与 proto,未跑前端门禁。
