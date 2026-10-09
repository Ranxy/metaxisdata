# Agent CLI（`mxd`）与 Device Login — Reference

> Status: **implemented**。Maintenance reference for `cli/` 与服务端 device login 一侧。
> Related: 使用者文档 [cli/README.md](cli/README.md);刻意决策（明文 token、scope 只在环境、device 状态进程内）[docs/security-posture.md](docs/security-posture.md);目录规则 `cli/AGENTS.md`、根 `AGENTS.md`。

## What it is

仓库第三个可交付物：一个面向 agent（也可供人用）的 CLI，通过 ConnectRPC + Bearer JWT 直连现有后端。三类能力：

1. **元数据结构查询** —— 复用现有 `InstanceService`/`DatabaseService` RPC，后端零改动。
2. **任意 SQL 的血缘** —— `LineageService.AnalyzeSQL`（无状态，不落库）；`lineage sql --depth N` 再把解析出的真实 target 接到 `GetLineageGraph`。
3. **多层上下游血缘** —— `LineageService.GetLineageGraph` 服务端有界 BFS，一次调用返回整图。

认证是 **Device Login**（RFC 8628 思路，签发本平台自己的 JWT）：CLI `CreateDeviceLogin` → 人在网页 `/device` 确认 → CLI `ExchangeDeviceLogin` 轮询拿到与确认者身份绑定的 token。

`cli/` 与后端**同一个 Go module**，但物理隔离，且 `.golangci.yaml` 的 `depguard` 只放行 stdlib、cobra、connect、protobuf、`backend/generated-go`（`cli/AGENTS.md`）。stdout 的契约是**恰好一个 JSON 文档**；进度与错误走 stderr。

## Decisions

| 决策 | 选择 | 为什么 |
| --- | --- | --- |
| 位置与产物 | 仓库根 `cli/`，`make build-cli` → `build/mxd` | 顶层按可交付物组织；单二进制便于 agent 安装 |
| 同 module | 不独立 module | 独立 module 需要 `replace` 才能引用 `backend/generated-go`，而 `replace` 对 `go install <pkg>@version` 不生效 |
| 依赖边界 | `depguard` 把「CLI 只是 API 客户端」变成可执行约束 | 物理分目录不等于依赖隔离 |
| 认证 | 复用现有 JWT（签给确认者本人） | 权限模型不变：CLI 即用户本人；不引入 OAuth client 注册等新概念。`--service-account` 只是复用 `AuthService.Login`（1 小时 API token） |
| Device 会话存储 | 进程内存（`state.State` 新增 store，Lazy TTL） | 与 SSOStateCache 一致；生命周期只有 10 分钟，不值一次 migration。代价是多副本需单实例或粘性路由 |
| 确认链接 | 默认打印**带 code** 的链接，`--no-prefill-url` 回到裸地址；server 未配 `external_url` 时 CLI 回退到 `<server>/device` | 打印裸地址会逼用户手抄 code；真正防线是确认页展示 client/来源 IP/时间 + 显式点击 |
| 码形式 | `user_code` 40bit、`device_code` 256bit；资源名 `deviceLogins/{user_code}` | 页面/URL 只出现 user_code，device_code 永不进 URL、永不进审计 |
| 多层血缘 | 服务端 BFS 新 RPC `GetLineageGraph` | 避免 CLI 侧 N+1 |
| 图遍历上限 | 每节点走完整分页 + 节点 500 / 边 10000 / 墙钟预算三重上限 | `column_lineage` 没有单对象边数不变量 |
| SQL 血缘 | 无状态新 RPC `AnalyzeSQL` | agent 要即时答案；创建 ManualSQL 是写操作且异步，语义不同 |
| scope 来源 | **只读进程环境变量 `METAXISDATA_SCOPES`**；`--scope` 按次覆盖；CLI 不保存、不管理、不推断 | 环境变量天然满足「同机多 agent/多项目互不干扰」；落盘会被互相覆盖 |
| 多 scope 语义 | 对同一段 SQL **独立多次解析**，按 scope 分组返回，不合并上下文 | scope 可能不同引擎；合并会伪造不存在的跨环境边 |
| GUID 可获取性 | `Database.guid`、`StoredMetadata.guid` 新增 OUTPUT_ONLY | scope 必须是精确 GUID，只能由服务端权威给出；用户直接复制，CLI 全程当不透明字符串 |
| 分页 | 所有 list 命令默认自动翻页，`--max-items` 默认 1000，触顶输出 `truncated` + `nextPageToken` | 服务端默认 10/50，静默截断比报错更毒 |
| JSON 风格 | `protojson` + `EmitUnpopulated: true`；信封里的嵌套消息也必须走 protojson | 与审计/JSONB/REST 一致；shape 不随数据变化 |
| temp 关系 | `lineage sql` 默认隐藏 `is_temp`，`--include-temp` 打开；scope 因此变空时报 `tempRelationsHidden` + 提示 | 裸 SELECT 的 temp 就是答案，空结果不能静默 |
| server 地址 | `auth login --server <url>` 填写并持久化；解析顺序 `--server` > `METAXISDATA_SERVER` > 凭据文件 > **报错** | 自托管 server 基本不变；不猜默认值、不探测 localhost |
| token 存储 | CLI 永不打印 token，只写本机凭据文件（0600） | agent 日志可能被转发/粘贴 |

**刻意不做（已评审，勿再当作待办）**：`AnalyzeSQL` 逐 scope **串行**执行 —— 十个 scope 只是几次小查询加一次 CPU 解析，并发只会换来共享故障模式。`DeviceLoginStore.pruneLocked` 扫 map 而不是维护插入顺序 —— 上限一万条，且只在满容量 create 时扫。`cli/cmd` 把解析结果存在包级状态里，所以它的测试只测纯函数而不真的跑命令。`GetLineageGraph` 除节点/边天花板外还有墙钟预算，预算耗尽同样报 `truncated`；边天花板在**单个对象的读取内部**执行（fetcher 拿到剩余预算并多探一条），否则先读完一个 hub 的全部边就会把它拉进内存。

## 命令树

```
mxd auth login [--server <url>] [--no-browser] [--no-prefill-url] [--login-timeout 10m]
mxd auth login --service-account <email>          # 用 METAXISDATA_SERVICE_KEY 换 1 小时 API token（CI）
mxd auth status | mxd auth logout
mxd config show | mxd config path
mxd instance list
mxd database list [--instance id]
mxd meta list <parent-guid> [--type TABLE] | meta get <guid> [--type]
mxd meta search <keyword> [--type] [--parent-guid-prefix] | meta ddl <guid> [--type]
mxd lineage sql [--scope <name|guid|all>]... [--file a.sql|-] [--depth N] [--include-temp]
mxd lineage graph <guid> [--depth N] [--direction up|down|both] [--column c]
mxd skill install [--dir <path>] | mxd skill show
mxd version
```

**没有 `mxd scope ...` 命令组**；`config show` 只做回显。`lineage sql` 没有位置参数的 scope：`--scope` 覆盖环境变量，其余一律用 `METAXISDATA_SCOPES`。scope 数超过服务端上限 10 时 CLI 自动分批调用再合并（`cli/cmd/lineage.go:98`）。全局 flag：`--server --token --scope --config --format --timeout --debug --ca-cert --insecure --page-size --max-items`（`cli/cmd/root.go:115-126`）。

## 输出契约与退出码

stdout 恰好一个 JSON 对象（protojson lowerCamelCase，2 空格缩进）；`--format table` 供人用（列宽由 tabwriter 推断）；错误统一到 stderr 的 `{"error":{"code","message","hint"}}`。`--format mermaid` 未实现。

| 退出码 | 含义 | 触发（`cli/client/client.go:24-80`） |
| --- | --- | --- |
| `0` | 成功 | — |
| `1` | 参数/输入错误 | `UsageError`（含 `server_required`、`scope_required`、`config_invalid`）、`CodeInvalidArgument`/`CodeFailedPrecondition` |
| `2` | 未认证 | `CodeUnauthenticated`，以及 device 会话失效（`device_session_expired`）→ 应执行 `mxd auth login` |
| `3` | 资源未找到 | `CodeNotFound` |
| `4` | 权限不足 | `CodePermissionDenied` |
| `5` | 服务端错误 | 其余 Connect 码 |
| `6` | 超时/取消 | `CodeDeadlineExceeded`/`CodeCanceled` 与本地 `context.DeadlineExceeded`/`Canceled` |

**错误码词表**（`cli/output/output.go:201` 的 `CodeOf`）：`unauthenticated`/`permission_denied`/`not_found`/`invalid_argument`(InvalidArgument+FailedPrecondition)/`timeout`/`resource_exhausted`/`already_exists`/`unavailable`/`unimplemented`/`internal`，外加 `server_required`、`scope_required`、`config_invalid`、`device_session_expired`。任何 `CodeUnauthenticated` 响应会顺手清掉本地失效凭据。

## 连接地址与登录凭据

| 项 | 来源（高 → 低） | 是否持久化 |
| --- | --- | --- |
| server | `--server` > `METAXISDATA_SERVER` > 凭据文件 > **`server_required`(退出码 1)** | `auth login` 写入（与 token 原子写入） |
| token | `--token` > `METAXISDATA_TOKEN` > 凭据文件 | `auth login` 写入 |
| **scope** | `--scope` > `METAXISDATA_SCOPES` | **从不写入任何文件** |
| `--ca-cert` / `--insecure` | flag / env | 不持久化（描述当前网络路径） |

解析实现在 `cli/cmd/root.go:185-205`。地址在 `client.New` 里规范化：必须带 `http`/`https` scheme、去尾 `/`、拒绝 userinfo/query/fragment（`cli/client/client.go:252-280`）；非法 → `invalid_argument`(退出码 1)。没有「默认 localhost」，也不探测端口。

**凭据文件**：默认 `$XDG_CONFIG_HOME/metaxisdata/config.json`（`cli/config/config.go:40-49`），`--config`/`METAXISDATA_CONFIG` 指向另一份；创建即 `chmod 0600`，先写临时文件再 rename，临时文件也带最终权限（`cli/config/config.go:85-107`）。内容只有 `{"server","token","user","tokenExpiresAt"}` —— **没有 scope 字段**（`cli/config/config.go:23-28`）。

**`auth login` 的写入语义**（`cli/cmd/auth.go:137-160`）：

- 解析出**生效地址**后，把 server 与 token 作为**一个原子对**落盘；只更新 token 而地址记的是另一个 server 会让后续全部请求莫名 401。
- 生效地址与文件旧值不同时，在 stderr 打印 `server changed: <old> -> <new>`，不弹交互确认。
- 把实际写入的凭据文件路径打印到 stderr（便于同机多 agent 排查），且**不读写 scope**。
- 成功时 stdout 输出一个对象 `{"status":"approved","server","user","tokenExpiresAt"}`。
- `--service-account` 路径改走 `AuthService.Login`，token 有效期固定 1 小时，key 来自 `METAXISDATA_SERVICE_KEY`（`cli/cmd/auth.go:110-132`）。
- **`auth logout` 只清 token/user/expiry，保留 server**（`cli/config/config.go:113-117`，`cli/cmd/auth.go:209-218`）。

**`config show`**（只读）回显 `server{value,source}`（source = `flag`/`env:METAXISDATA_SERVER`/`file:<path>`）、`token{value:"[REDACTED]",source}`、`configFile`、`scopes[{name?,guid}]`、`scopeSource`（`env:METAXISDATA_SCOPES` 或 `none`）。

## 执行作用域（scope）

`catalog.AnalysisContext{InstanceID, Database, Schema}` 是解析未限定名的唯一来源，SQL 文本里最多写到 `database.schema.table` —— **instance 永远来自上下文**。所以 scope 是必填的，没有「默认猜一个」的退路。

- 唯一来源：进程环境变量 `METAXISDATA_SCOPES`，逗号分隔，每项 `guid` 或 `name=guid`（`name` 仅用于输出归因）。例：`dev=1;shop,prod=9;shop;public`。
- **按每次命令执行求值**，不写文件、不写缓存；进程退出即消失。
- `--scope <name|guid|all>` 按次覆盖；值是 GUID（含 `;`）时按 GUID 处理，否则必须命中已配置的 name；`all` 选全部（`cli/env/env.go:83-125`）。
- 未设置或为空 = 没有 scope。`name` 可省略，省略时不回显名字。

**缺失时的失败形状**（`cli/cmd/lineage.go:73-77`）：退出码 1，`code=scope_required`，hint 直接给出格式与获取 GUID 的命令，agent 可原样转达给用户。

**多 scope 语义**：多个 scope = 对同一段 SQL 做多次独立解析，不是合并上下文；输出**永远按 scope 分组**（形状保持 `results[]`，即使只有一个 scope）。跨环境的真实数据流不会因为多选 scope 而被发现 —— scope 只决定未限定名如何落位，不创造边。`GetLineageGraph` 不需要 scope（root 是绝对 GUID）。

**GUID 从哪里复制**：`mxd database list` 每条带 `guid`（MySQL 家族）；`mxd meta list <db-guid> --type SCHEMA` 每条带 `guid`（PostgreSQL）。GUID 全程当不透明字符串，CLI 不拼、不拆、不推断。

## Device Login

```
CLI ── CreateDeviceLogin {clientName,clientVersion} ──► server 生成 device_code(256bit)+user_code(40bit)
    ◄── device_code, user_code, verification_uri(+complete), expires_in=600, interval=3
    stderr 打印 URL + code（--no-browser 时不尝试开浏览器）
浏览器 /device：GetDeviceLogin 详情 → ApproveDeviceLogin → PENDING→APPROVED（绑定确认者 user_id，审计）
CLI ── ExchangeDeviceLogin {device_code} ──► APPROVED 首次：签发 JWT、消费条目
    ◄── token + user + expires_in；写入凭据文件，stdout 一个 JSON
```

**状态机**：`PENDING ──approve──► APPROVED ──exchange(首次)──► CONSUMED（条目删除）`；`PENDING ──deny──► DENIED`；超 TTL → `EXPIRED`（lazy 清理）。approve 时绑定确认者 `user_id`，exchange 为该用户签发 `GenerateAccessToken`（与 web 登录同一路径，含 last-login 更新）；exchange 一次性，重复轮询得到 `CodeNotFound`，CLI 翻译成 `device_session_expired`（退出码 2）而不是泛化的「未找到」。

**服务端边界**（`backend/component/state/device_login.go:32-49`）：TTL 10 分钟、最短轮询间隔 1s、建议 3s、容量 10000；`user_code` 8 位 Crockford base32（排除 `ILOU`，`O→0`/`I,L→1` 归一化）、`device_code` 43 位 base62；生成后校验唯一（碰撞重试 5 次，`ErrDeviceLoginCodeTaken`）。

## Invariants

1. **server 与 token 是一个原子对**：`auth login` 必须同时写；分开写会产生「token 属于 B、地址记的是 A」的静默失败。
2. **scope 永不落盘**：只从进程环境读。落盘会让同机两个 agent 互相覆盖，且失败是静默的。
3. **stdout 恰好一个 JSON 文档**：进度、提示、人类可读文字一律走 stderr；`--format table` 也只在 stdout 放表格。
4. **token 永不打印**：只进 0600 的凭据文件；`config show` 只回显 `[REDACTED]`。
5. **token 不出 server host**：HTTP 客户端拒绝离开 `--server` 地址的重定向（含子域与 https→http 降级），并保留标准库的 10 跳上限；bearer 传输层也只对同一 host 附加 header（`cli/client/client.go:190-232`、`338-348`）。
6. **device 状态进程内**：create/approve/exchange 必须到达同一副本，否则 approve 落到别处会 NotFound；部署需单实例或粘性路由（`docs/security-posture.md`）。
7. **exchange 一次性**：签发后立即删除条目；`CodeNotFound` → `device_session_expired`，退出码 2。
8. **批准者资格与 web 一致，但不重评密码策略**：仅 END_USER、未停用、通过域限制（`validateApprover`，`backend/api/v1/auth_service_device_login.go:237`）；device login 与 OAuth consent 共用一个实现。重评密码策略会把 SSO-only 用户挡在门外。
9. **device_code 不进审计**：审计拦截器同时落 request 与 response，脱敏名单含 `devicecode`/`device_code`（`backend/component/audit/audit.go:239`，`backend/component/audit/audit_test.go:29`）；`userCode` 不是秘密，不脱敏。
10. **交换时复查账号状态**：approve 与 exchange 之间可能被停用；签发前重新 `GetUserByID` 并重跑 `validateApprover`（`backend/api/v1/auth_service_device_login.go:205-213`）。
11. **限流分两层**：`CreateDeviceLogin` 按来源地址 + 全局桶（在拦截器链上，`backend/api/v1/throttle_interceptor.go:121`），`GetDeviceLogin`/`ApproveDeviceLogin` 另有按调用者计数（`auth_service_device_login.go:93,123`），`Exchange` 间隔 <1s 回 `CodeResourceExhausted`（slow_down）。
12. **user_code 一一对应**：`DeviceLogin` 资源名就是 `deviceLogins/{user_code}`；生成后必须校验唯一并重试。
13. **GUID 是不透明字符串**：CLI 不解析、不拼装，只透传服务端给的 `guid`。
14. **`lineage sql --depth N` 原样传给 `GetLineageGraph`**，不是 `N-1`（`cli/cmd/lineage.go:266` 的 `graphDepth := depth`）。

## Failure modes

| 场景 | 行为 |
| --- | --- |
| 全新机器、无地址 | `server_required`，退出码 1，hint 指向 `mxd auth login --server <url>` |
| 无 scope | `scope_required`，退出码 1，hint 给出格式与获取 GUID 的命令 |
| token 缺失/过期/被吊销 | `unauthenticated`，退出码 2 → `mxd auth login` |
| device 会话被消费/拒绝/过期 | `device_session_expired`，退出码 2 |
| 地址 scheme 非法 / 凭据文件损坏 | `invalid_argument` / `config_invalid`，退出码 1 |
| server 返回重定向到另一 host 或降级 | 拒绝跟随，错误同时点出两个 host |
| 列表触顶 `--max-items` | 输出 `"truncated": true` 与 `"nextPageToken"`，agent 决定是否续查 |
| `lineage sql` 某 scope 引擎不支持 | 该 scope relations 为空 + scope 级 warning，请求仍成功（除非所有 scope 同因失败） |
| 裸 SELECT | 真实 target 为空、`is_temp=true` 的临时关系默认隐藏；报 `tempRelationsHidden` 与提示 |
| 分析器无法完整表示语句 | relations 照常返回 + `diagnostics`（`not_modelled`/`unresolved_reference`/`ambiguous_reference`/`catalog_unavailable`）；解析错误则整个 scope 失败 |

## Open items

| 未做/未决 | 证据与位置 |
| --- | --- |
| `--format mermaid` | `cli/output/output.go:24-39` 只接受 `json`/`table` |
| goreleaser / `go install` 分发 | 仓库无 `.goreleaser*`；`make build-cli` 是唯一构建入口（`Makefile:17`） |
| 历史快照 GUID（`StoredMetadata.guid` 之外） | `Database.guid`（`proto/v1/v1/database_service.proto:170`）与 `StoredMetadata.guid`（:514）已加；历史事件的 GUID 由事件本身携带，未额外暴露 |
| scope 的服务端持久化 | 未做，且与「scope 只在进程环境」的刻意决策相反 |
| 多副本 device login | 未测；需第二个实例（见 Invariants 6） |
| 确认页强化（sudo 模式 / 二次输入密码） | 未做；当前是「已登录 + 码比对 + 显式点击」 |
| `GetLineageGraph` 截断后无法续查 | `truncated=true` 只报事实，没有 `next_page_token`；真需要时应以 frontier 快照为游标 |
| 多 server 并存 | 靠 `--config <path>` 各存一份；无命名 profile/别名 |

## Where things live

- CLI：`cli/main.go`；命令 `cli/cmd/{root,auth,config,instance,database,meta,lineage,pagination,skill}.go`；客户端与重定向策略 `cli/client/client.go`；凭据文件 `cli/config/config.go`；环境与 scope 解析 `cli/env/env.go`；渲染 `cli/output/output.go`；device 流程 `cli/authflow/device.go`；内嵌 agent skill `cli/skill/SKILL.md` + `cli/skill/skill.go`。
- 服务端 device login：`backend/api/v1/auth_service_device_login.go`（4 个 RPC + 生成器 + `validateApprover`）、`backend/component/state/device_login.go`（store/状态机/TTL/限流键）、`backend/api/v1/throttle_interceptor.go`（create 与 logout 的预算）；proto `proto/v1/v1/auth_service.proto`。
- 血缘 RPC：`backend/api/v1/lineage_service_analyze.go`、`backend/api/v1/lineage_service_graph.go`；proto `proto/v1/v1/lineage_service.proto`；GUID 输出 `proto/v1/v1/database_service.proto`。
- 前端确认页：`frontend/src/pages/DeviceLoginPage.vue`、路由 `frontend/src/router/index.ts:262`、`frontend/src/api/device-login.ts`；locale `frontend/src/locales/{en-US,zh-CN}.json`。
- 使用者文档：`cli/README.md`。刻意决策：`docs/security-posture.md`。
- 门禁：`gofmt` + `golangci-lint run --allow-parallel-runners`（含 `depguard`）；`go test ./cli/...`（`cli/env/env_test.go`、`cli/config/config_test.go`、`cli/client/client_test.go` 的 `TestExitCode`/`TestNormalizeServer`、`cli/authflow/device_test.go`、`cli/cmd/*_test.go`）；真实 server 端到端 `make test-integration-smoke` 跑 `backend/test/integration/runner/agent_cli_service_test.go`（`TestDeviceLoginRealServerIntegration:63` 是 authorize→approve→exchange 骨架，另有 denied、AnalyzeSQL、GUID、lineage graph 用例）。
