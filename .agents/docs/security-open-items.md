# 仍未开放的安全问题 — Reference

> Status: **活跃清单**(核对基线 `89a51cc`,2026-10-08)。只列今天仍成立、尚未修复、也尚未被接受为新决策的条目;每条都已对当前 HEAD 的代码复核过。
> 已接受的设计决策见 [docs/security-posture.md](docs/security-posture.md)(唯一权威,本文不重复其正文)。已修复的历史不在此逐条保留,需要时查 git 历史与提交信息。

## 仍开放

| # | 位置 | 问题 | 影响 | 建议 |
| --- | --- | --- | --- | --- |
| M22/D7 | [backend/plugin/db/util/ssl.go:28](backend/plugin/db/util/ssl.go#L28) | `verify_tls_certificate=false`(默认)时直接 `cfg.InsecureSkipVerify = true`(:31),无任何提示 | 目标库 TLS 只是未认证加密,同网段可中间人 | 校验默认开启,UI 显式提示降级 |
| M22/D7 | [backend/plugin/idp/oauth2/oauth2.go:65](backend/plugin/idp/oauth2/oauth2.go#L65) | IdP `skip_tls_verify` 直接进 `InsecureSkipVerify`,无告警 | 能 MITM 者可伪造 userinfo 主体,接管已按 subject 绑定的账号 | 默认关闭 + 设置页显著警告 |
| M22/D7 | [backend/plugin/db/util/ssh.go:68](backend/plugin/db/util/ssh.go#L68) | `ssh_private_key` 为空时回退进程自身的 `SSH_AUTH_SOCK`,把运维者 agent 的全部密钥交给用户指定的 ssh_host(:59-71) | 公钥枚举;匹配则以运维者身份完成认证。主机密钥校验已随 H7 修复,此回退未动 | 删除 agent 回退,或改为显式配置项 |
| M23 | [backend/component/llm/fetcher.go:34](backend/component/llm/fetcher.go#L34) | `ValidateBaseURL` 只校验绝对 http/https + host 非空:不拒私网、不强制 https | 能配置 LLM profile 者可把平台当带 key 的内网探测器;明文 http 时 key 明文过网 | 内网黑名单/告警,非 loopback 拒绝明文 http(「改 base_url 须同请求给 key」已封住旧 key 外带,见 [llm_service.go:157](backend/api/v1/llm_service.go#L157)) |
| M23 | [backend/component/llm/agent.go:72](backend/component/llm/agent.go#L72) | 共享 `llmHTTPClient` 无 `CheckRedirect` | 上游可 302 到任意主机并带走 Bearer key | 加 `CheckRedirect`,限制同 host |
| M25/D8 | [backend/server/pprof.go:15](backend/server/pprof.go#L15) | `/debug/pprof/*` 只由 `RuntimeDebug` 开关控制,**无认证**;`/metrics` 同一把闸([echo_routes.go:81](backend/server/echo_routes.go#L81)) | 任一持 `settings.update` 者开启后,匿名可取 heap/goroutine/cmdline/profile;heap 含常驻 JWT 签名密钥([server.go:186](backend/server/server.go#L186))与解密后的凭据 | pprof/metrics 独立开关 + 管理员认证或只绑回环;运行期开启时告警 |
| (新)IdP secret | [proto/store/store/idp.proto:37](proto/store/store/idp.proto#L37) | `idp.config.client_secret` 明文入库:[store/idp.go:118](backend/store/idp.go#L118) 直接 protojson 解析,无加密/混淆,`security-posture.md` 未收录 | 任何数据库读权限者都能拿到并以本应用身份访问企业 IdP | 提供配置入口,并用现有凭据加密存 client_secret(先给生成密文的工具) |
| M6 | [backend/api/auth/auth.go:44](backend/api/auth/auth.go#L44) | 会话 cookie 名仍是 `access-token`:无 `__Host-` 前缀、无重复同名拒绝,:260 取第一个同名 cookie | 共享父域下可控兄弟子域可种 `Domain=` 同名 cookie 抢占 `/v1`,受害者以攻击者身份执行 | HTTPS 下改 `__Host-access-token`;遇多个同名 cookie 直接拒绝 |
| D3 | [backend/server/csrf.go:53](backend/server/csrf.go#L53) | CSRF 中间件复制 `access-token` 字面量,与 [auth.go:44](backend/api/auth/auth.go#L44) 无编译期关联;server 包无 csrf 测试 | 任一侧改名会让 CSRF 静默失效 | 共享常量;补 csrf 测试 |
| M8 | [backend/api/v1/acl_interceptor.go:74](backend/api/v1/acl_interceptor.go#L74) | `AllowWithoutCredential \|\| Permission == ""` 直接放行,acw 架空 `permission` 注解;守护测试 [acl_interceptor_test.go:52](backend/api/v1/acl_interceptor_test.go#L52) 反把二者并存当合法 | 日后任何方法同时带 acw+permission 即静默失去权限闸门,测试不红 | 断言 `acw ⇒ permission==""`,或对 acw 方法仍执行权限检查 + 显式匿名名单 |
| M21 残余 | [backend/api/v1/instance_service.go:624](backend/api/v1/instance_service.go#L624) | 连接测试错误经 [dataSourceConnectionError:664](backend/api/v1/instance_service.go#L664) 原样回显驱动细节,无 host 白/黑名单(超时与每实例连接额度已在 :632 补齐) | 持实例写权限者可对任意 host:port 探测,错误差异构成端口/服务指纹 | 统一错误文案;提供 host 策略选项 |
| D11 | [backend/store/oauth_client.go:85](backend/store/oauth_client.go#L85) | `ListOAuthClients` 与 `DeleteOAuthClient`(:142)零调用方;`last_used_at` 只写不读(:123);表无总量上限 | 注册洪泛后只能手写 SQL 清理;没有运维出口 | 实现 connected-apps 管理面,或加容量/保留策略 |
| D9 | [backend/component/audit/audit.go:278](backend/component/audit/audit.go#L278) | 匿名请求的 audit actor 回退到请求自带字段(`user.name`/`email`) | 攻击者自选 actor 文本写进永久账本,归因失真 | 匿名请求强制记 IP、actor 留空 |
| L5 | [backend/component/audit/audit.go:224](backend/component/audit/audit.go#L224) | 脱敏名单含 `sslkey`/`sslcert`,漏 `ssl_ca` | 误把带私钥的 PEM 粘进 `ssl_ca` 即永久入账 | 补 `sslca`;长期改 proto sensitive 注解 + 值模式扫描 |
| 5.1 残余 | [backend/common/crypto/crypto.go:135](backend/common/crypto/crypto.go#L135) | `Seal`/`Open` 的 additionalData 为 nil,密文未绑定字段/行 | 有 DB 写权限者可把 A 实例的密文搬到 B 行,A 的凭据交给 B 的连接 | 加 AAD(实例/字段标识) |
| L21 残余 | [backend/plugin/db/mysql/mysql.go:131](backend/plugin/db/mysql/mysql.go#L131) | dial 协议名 `uuid.NewString()[:8]`(32bit)且静默覆盖注册;[starrocks.go:91](backend/plugin/db/starrocks/starrocks.go#L91) 同 | 并发隧道协议名碰撞会把两实例拨号串线(现已 Deregister,:177);碰撞概率低但非零 | 用完整 UUID 作协议名 |
| I2 | [backend/server/grpc_routes.go:318](backend/server/grpc_routes.go#L318) | 四条匿名 OAuth 路由各自调用 `oauthEndpointMiddleware`,每次新建独立 store([oauth_endpoints.go:26](backend/server/oauth_endpoints.go#L26)),实际是 4 份配额;早先文档写作「共享一份上限」 | 匿名调用方可按 4 倍速率打这四条路由(每路由 10/s、burst 20) | 待决策:合并为只创建一次的 middleware(会收紧 4 倍,曾因集成用例回退)或保留现状并只按实现描述文档 |
| D16 | [backend/api/v1/iam_helpers.go:28](backend/api/v1/iam_helpers.go#L28) | 「最后管理员」不变式只认 `roles/workspaceAdmin` 精确字符串(:36);自定义角色即使持全部权限也不计入 | guard 与实际授权能力脱节:无法以自定义角色表达「唯一管理员」,workspace 永远脱离不了预定义角色(偏保守,不会误判为仍有管理员) | 按权限判定管理员,或把该限制写进 `security-posture.md` |
| D17 | [backend/api/v1/role_service.go:110](backend/api/v1/role_service.go#L110) | `UpdateRole` 收缩已绑定角色权限无任何影响提示;`DeleteRole` 反而拒绝删除仍被绑定的角色(:155) | 管理员可无意清空已绑定角色权限,让用户静默失去访问 | 返回受影响绑定/用户数并要求确认 |

## 已失去意义的旧结论(勿再引用)

- 「凭据为同库 XOR 混淆」→ 已是 `v1:` AES-256-GCM([backend/common/crypto](backend/common/crypto/crypto.go))。
- 「SSH 隧道 `InsecureIgnoreHostKey`、无主机密钥校验」→ 已改为按数据源 `ssh_host_key` 校验(commit `b06249d`)。
- 「XFF 取最左段 / 登出吊销是内存 LRU / Connect 入口与明文路由无通用限流」→ 已分别由 M1、M4、§5.2 修复(commit `e9bc802`、`7dbcbd7`)。
- 「OpenLineage namespace/job_name/run_id 无长度上限、JSON 解析无深度限制(D14/L15)」→ 长度已在 [limits.go:19](backend/plugin/openlineage/limits.go#L19) 封顶;深度由 Go 标准库 `maxNestingDepth=10000` 限制。仅字符集仍未校验,无明确安全影响。
- 「`extra_connection_parameters` 需要结构化白名单(D13/M20/L20)」→ 已作为已接受决策写入 `docs/security-posture.md`(仅非调用方选择的库名注入由 `BuildDSN` 修复)。
- 「M7 SSO 登录自动复活被停用账号」→ 已是「停用即最终,IdP 登录不 undelete」(posture)。
- 「M24 全站无通用限流」→ §5.2 已实施按 `(principal, method)` 预算 + 审计写入背压;仅 LLM 的费用/配额告警仍未做(调用速率已有预算)。

## 复核方式

逐条 `grep -n` 关键标识符(`InsecureSkipVerify`、`ListOAuthClients`、`hasActiveWorkspaceAdmin`、`AllowWithoutCredential`、`client_secret`、`Seal(`…),再 `read` 确认上下文与全部调用方;表内 `文件:行号` 为 `89a51cc` 上的位置。修复后请把该条从本表移入 `docs/security-posture.md`(若属接受的取舍)或直接删除,不要在此保留历史。
