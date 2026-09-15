# Plan: Agent CLI(`mxd`)与 Device Login

## TL;DR

新增一个面向 agent(也可供人使用)的 CLI `mxd`,通过 ConnectRPC + Bearer JWT 直连现有后端,提供四类能力:

1. **元数据结构查询** — 复用现有 `DatabaseService`/`InstanceService` RPC,后端零改动;
2. **任意 SQL 的血缘分析** — 新增 `LineageService.AnalyzeSQL`,把已有的 `lineage.GetAnalyzeRelation` 插件能力(目前只被 runner 使用)以无状态 RPC 暴露出来;CLI 的 `--depth N` 再把解析出的 target GUID 接到 `GetLineageGraph`,满足"上下游多层";
3. **元数据的多层上下游血缘** — 新增 `LineageService.GetLineageGraph`,服务端做有界 BFS,一次调用返回整张多层图(节点+边),替代现有前端逐层展开的 N+1 模式;
4. **执行作用域(scope)** — SQL 里的未限定名脱离 database 上下文无法解析,所以 `AnalyzeSQL` 必须有 scope。scope **只从环境变量读取,只在本次命令执行期间生效,CLI 不保存、不管理、不推断**;每个项目用哪些 scope 由用户自己维护(例如写在自己的 `AGENTS.md` 里),由用户/上层把值导进 agent 进程的环境变量。

认证采用 **Device Login**(参考 RFC 8628,签发本平台自己的 JWT):CLI 发起 `CreateDeviceLogin`,用户在网页 `/device` 页面确认后,CLI 轮询 `ExchangeDeviceLogin` 拿到与确认者身份绑定的 token。待确认请求存放在进程内存(与 `SSOStateCache` 同一模式)。

CLI 放在**仓库根目录的 `cli/`**(与 `backend/`、`frontend/` 同级),仍是**同一个 Go module**(cobra 已是依赖),直接复用 `backend/generated-go` 生成的 Connect 客户端,`make build-cli` 产出单二进制 `build/mxd`。默认输出 JSON(机器友好),`--format table` 供人使用。

---

## 需求分析

| # | 需求 | 本质 | 现状结论 |
| --- | --- | --- | --- |
| 1 | 查询元数据的结构信息 | 元数据浏览:instance → database → schema → table/column,DDL | **已有 RPC 完全覆盖,后端不需要改** |
| 2 | 查询某一条 SQL 的血缘分析(上下游多层) | 拆成两件事:(a) 对任意 SQL 文本做列级血缘解析;(b) 由解析出的 target 出发做多层展开 | (a) 解析器存在(`plugin/lineage`),但**只被 runner 内部调用,没有对外 RPC**,需新增;(b) 不再写第二个图遍历实现,由 CLI 用 `--depth` 复用 `GetLineageGraph` 组合 |
| 3 | 查询某个元数据的血缘(多层上下游) | 从一个 GUID 出发按方向做多层遍历 | `GetLineage` 只返回一跳;前端 `api/lineage.ts` 已在 TS 侧翻页,但图页仍逐节点展开(N+1)。CLI 需要一次拿全图,需新增图遍历 RPC |
| 4 | auth 触发 device login,用户页面确认 | 不可交互环境下的人机授权(OAuth Device Grant 思路) | 平台**完全没有**此流程,需新增 4 个 RPC + 前端确认页 |
| 5 | 给 SQL 分析提供执行作用域 | 未限定名必须有一个 database/schema 上下文才能解析 | 解析器靠 `catalog.AnalysisContext` 提供上下文,**没有任何对外入口**;CLI 侧也没有读取用户 scope 约定的通道 |

**关键产品约束(agent 友好)**:

- agent 无法输入密码、无法点击浏览器。授权必须是"CLI 打印一个 URL/码 → 人去网页点确认 → CLI 自动完成"。
- agent 消费的是结构化输出:默认 JSON、错误进 stderr 且机器可判、退出码稳定(能据此决定下一步,如"需要重新 auth")。
- **stdout 的契约是"恰好一个 JSON 文档"**:进度、提示、人类可读文字一律走 stderr。`auth login` 是长流程,但 stdout 只在结束时输出一个对象。
- agent 需要"发现 → 定位 → 分析"的链路:`meta search` / `meta list` → `meta get`/`ddl` → `lineage`。
- **作用域由用户提供,CLI 不做任何推断**:没有 scope 就**显式失败**,绝不静默产出一堆查不到的 GUID,也绝不替用户挑一个。
- **server 地址在 `auth login` 时确定并持久化**:用户的 server 基本不变,所以登录时用 `--server <url>` 填一次就写进凭据文件,之后所有命令都不用再带;同时**没有地址就直接报错**,不猜默认值、不探测 localhost。
- **CLI 是无状态客户端**:除"本机登录凭据(server + token)"外不保存任何东西;scope 一律按进程环境变量在每次调用时求值,保证同一台机器上多个 agent 互不干扰。

---

## 现状盘点:可复用与缺口

### 可直接复用

| 能力 | 位置 | CLI 中的用途 |
| --- | --- | --- |
| `ListInstances` / `ListDatabases` | `InstanceService` / `DatabaseService` | 导航入口;也是用户获取 scope GUID 的来源 |
| `ListMetadata`(按 parent guid)/ `GetMetadata` / `GetSchemaString` / `SearchMetadata` | `DatabaseService` | 需求 1 全部命令 |
| `GetCurrentUser`(未注解、任何已认证调用者可用) | `UserService` | `auth status` |
| `GetLineage`(单跳、source/target 方向、分页) | `LineageService` | 图遍历 RPC 的底层查询原语 |
| SQL 列级血缘解析器 | `backend/plugin/lineage`(MySQL/TiDB/MariaDB/PostgreSQL/StarRocks 已注册;MSSQL/Doris 未注册) | `AnalyzeSQL` 的实现核心;支持 SELECT / INSERT…SELECT / UPDATE / DELETE / CREATE TABLE AS / CREATE VIEW / LOAD |
| 分析器注册方式 | `backend/server/ultimate.go` 的 blank import + 各插件 `init()` | 服务端已注册;**api/v1 单测需自行 blank import** |
| `catalog.AnalysisContext` + `catalog.Provide` | `backend/plugin/lineage/catalog` | 用 scope 提供的 instance/database/schema 解析 SQL 中的未限定名 |
| JWT 签发/校验(`Authorization: Bearer` 已支持) | `backend/api/auth` | CLI 用 Bearer header,不需要 cookie(也顺带豁免 CSRF 中间件) |
| 进程内短期状态 + LRU | `backend/component/state`(SSOStateCache、TokenExpireCache、LoginLimiter) | device login 会话存储与限流,同一模式 |
| `common.RandomString`(crypto/rand,62 字符表) | `backend/common` | device_code 生成(`RandomString(43)` ≈ 256bit) |
| cobra、connect-go 客户端代码 | `go.mod`、`backend/generated-go/v1/v1connect` | CLI 框架与 RPC 客户端,无需新增代码生成配置 |
| 审计拦截器(支持匿名方法,如 `Login`)+ 脱敏函数 | `backend/api/v1/audit.go` | device login 事件审计;**脱敏名单需补 `deviceCode`** |
| `external_url` 工作区设置 | `store.GetWorkspaceGeneralSetting` | 拼接确认页 URL |
| 守卫测试与既有单测模式 | `acl_interceptor_test.go` 的 `unannotatedMethods`、`sso_state_test.go`、`auth_throttle_test.go` | 新 RPC 必须过 ACL 守卫;device store 单测照抄既有模式 |

### 缺口(本方案要补的)

1. **AuthService 缺 device login 全套**:`CreateDeviceLogin` / `GetDeviceLogin` / `ApproveDeviceLogin` / `ExchangeDeviceLogin`。
2. **LineageService 缺**:`AnalyzeSQL`(无状态 SQL 分析)、`GetLineageGraph`(多层图)。
3. **前端缺确认页**:路由 `/device`。
4. **没有 CLI 代码**:仓库根目前只有 `backend/`(server)与 `frontend/`(SPA)两个可交付物。
5. **ACL 守卫测试必须同步**:`backend/api/v1/acl_interceptor_test.go` 的 `unannotatedMethods` 是"无 permission 注解 RPC"的**唯一白名单**,新 RPC 缺注解就必须登记,否则 `TestEveryMethodIsPermissionGated` 失败。4 个 device RPC(`CreateDeviceLogin`/`ExchangeDeviceLogin` 匿名、`GetDeviceLogin`/`ApproveDeviceLogin` 已认证但无 permission)都要登记。
6. **审计脱敏名单缺 `deviceCode`**:`isSensitiveAuditField` 按 `password`/`token`/`secret`/`credential`/… 子串匹配,而审计拦截器同时落 request 与 **response**。`CreateDeviceLoginResponse.deviceCode` 是轮询密钥,当前不会被脱敏 → 明文进永久审计日志。
7. **裸 SQL 的 target 是合成对象**:分析器的 `resultTableName = "__result__"`(`IsTemp=true`)。runner 能拿到真实 target 是因为它先把定义包装成 `CREATE VIEW <name> AS <definition>`;`AnalyzeSQL` 直接喂原始 SQL,因此**裸 SELECT 会产出 `__result__` 这个不存在的表**。
8. **`column_lineage` 没有单对象边数上限**:`store.ListColumnLineage` 只做 `LIMIT $n`;5000 只是 API 层对 `page_size` 的钳制,不是数据不变量。BFS 每个节点必须自己翻页/封顶。
9. **GUID 无法从 API 直接拿到,而 scope 必须是精确 GUID**:`Instance`/`Database`/`StoredMetadata` 都**没有 `guid` 字段**(只有 `ManualSQL.guid` 是先例),`SearchMetadata` 是唯一能拿到子对象 GUID 的读接口。用户要往自己的 `AGENTS.md` 里写 scope 就必须能**从 CLI 输出里复制到精确的 GUID**;而 GUID 的形态依引擎而变(MySQL 家族 database 是 `<instanceID>;<database>` / schema 段为空,PostgreSQL 是 `<instanceID>;<database>;<schema>`),靠人手拼或让 agent 推断都会出错。
10. **`ListMetadata` 返回的 `StoredMetadata` 没有 GUID**:`meta list` 列出 table/view/schema 时拿不到子对象的 GUID,agent 无法把 `meta list` 的结果喂给 `meta get`/`ddl`/`lineage`。这是需求 1 链路里的一个硬洞。

---

## 方案总览

```
┌──────────────┐  Bearer JWT   ┌─────────────────────────────┐
│  mxd CLI     │ ────────────► │  metaxisdata server (Go)    │
│ (cobra +     │  ConnectRPC   │  ├ AuthService: device login │
│  connect-go) │               │  ├ DatabaseService(复用)      │
└──────┬───────┘               │  ├ InstanceService(复用)     │
       │                       │  └ LineageService:           │
       │ CreateDeviceLogin       │      AnalyzeSQL / GetLineageGraph
       ▼                       └──────────────┬──────────────┘
  打印 URL + user_code                         │ 轮询 Exchange
                                               ▼
                                    ┌────────────────────┐
                                    │ 浏览器 /device 页面  │
                                    │ 已登录用户点击确认    │
                                    └────────────────────┘

mxd lineage sql --depth N  =  AnalyzeSQL(一跳,按 scope 各跑一次)  ──target GUIDs──►  GetLineageGraph(N-1 跳)

scope:  只读进程环境变量 METAXISDATA_SCOPES(不落盘、不缓存),--scope 可按次覆盖
```

### 决策表

| 决策 | 选择 | 理由 / 放弃的备选 |
| --- | --- | --- |
| CLI 实现 | Go,仓库根目录 `cli/`,二进制名 `mxd` | 顶层已按**可交付物**组织(`backend/`=server,`frontend/`=SPA),CLI 是第三个独立可交付物,同级放置最一致。同一 Go module,直接复用 `generated-go` Connect 客户端与 proto;cobra 已有;单二进制便于 agent 安装。备选:留在 `backend/bin/cli/` — 顶层语义会退化成"backend 里也装客户端",放弃;独立 npm/TS 包 — 需要另一套代码生成与依赖管理,放弃 |
| CLI 是否独立 module | **不独立**,继续用根 module | 独立 module 需要 `replace github.com/Ranxy/metaxisdata => ../` 才能引用 `backend/generated-go`,而 `replace` 对 `go install <pkg>@version` 不生效 → Further Considerations 里承诺的 `go install` 分发路径会直接作废。同 module 下根的 `go build ./...`、`go vet ./...`、CI 的 golangci-lint 自动覆盖 `cli/`,无需改 CI |
| CLI 依赖边界 | `.golangci.yaml` 加 `depguard` 规则:`cli/**` 只允许标准库 + cobra + connect + protobuf + `backend/generated-go/...` | 物理分目录**不等于**依赖隔离:同 module 下 `cli/` 仍可 import `backend/store`,不设闸门必然被拖进服务端内脏(pgx、driver、runner);这条规则把"CLI 只是 API 客户端"变成可强制的约束 |
| CLI 认证凭据 | 复用现有 JWT(签给确认者本人) | 权限模型不变:CLI 即"用户本人"。不引入 service account / OAuth client 注册等新概念 |
| Device 会话存储 | 进程内存(`state.State` 新增 store,Lazy TTL) | 与 SSOStateCache 一致;单二进制部署已接受进程内状态。**代价:多副本必须单实例或粘性路由**,见 Further Considerations。备选:数据库表 — 生命周期只有 10 分钟,不值一次 migration,放弃 |
| 确认链接 | 同时返回 `verification_uri`(裸)与 `verification_uri_complete`;页面默认要求**手动输入** user_code | RFC 8628 的码比对只在"用户在自己终端发起"时成立;预填链接会把 CLI 变成钓鱼工具。`--no-browser` 之外,默认不自动打开也可以接受 |
| 码的形式 | `user_code` 40bit、`device_code` 256bit;`DeviceLogin` 的资源名就是 `deviceLogins/{user_code}` | 页面/URL 只出现 user_code,device_code 永不进 URL、永不进审计 |
| 多层血缘 | 服务端 BFS 新 RPC `GetLineageGraph` | 一次调用返回全图 + 节点元数据,避免 CLI 侧 N+1;将来前端图页也可换用它。备选:CLI 循环调 `GetLineage` — 逻辑复杂、往返多,放弃 |
| 图遍历实现 | 每节点**走完整分页**(与前端 `api/lineage.ts` 同语义),节点 500 / 边 10000 / 墙钟预算三重上限 | `column_lineage` 无单对象边数不变量,"Limit 大值即可"会拉回无界行 |
| SQL 血缘 | 无状态新 RPC `AnalyzeSQL` | agent 要的是即时答案,不落库。备选:创建 ManualSQL 再触发 runner — 是一次写操作且异步,语义不同,放弃 |
| **scope 的来源** | **只读进程环境变量 `METAXISDATA_SCOPES`**;`--scope` 可按次覆盖;**CLI 不保存、不提供任何 scope 管理命令** | 环境变量是进程级的,天然满足"同机多 agent/多项目互不干扰";落盘会让两个 agent 互相覆盖。用户在自己的项目文档(如 `AGENTS.md`)里维护 scope 清单并导出到 agent 进程,这是**用户的工作**,不是 CLI 的职责 |
| **scope 与平台 Environment** | **不使用**。平台 Environment 是 workspace 级的实例分类(一个环境对应多个系统的多个实例),与"某段 SQL 在哪个库解析"没有对应关系 | 把二者绑定会引入一层需要维护的间接映射;scope 应由用户按项目显式给出 |
| 多 scope 的语义 | **并行多次独立解析,按 scope 分组返回**;不做上下文合并 | scope 可能是不同 instance 甚至不同引擎,解析结果本就不同;合并会伪造出并不存在的跨环境边 |
| GUID 的可获取性 | 给 `Database` 和 `StoredMetadata` 补 `guid`(OUTPUT_ONLY) | scope 必须是精确 GUID,而它只能由服务端权威给出(引擎差异 + `;` 转义);用户从 `mxd database list` / `mxd meta list` 直接复制,GUID 全程对 CLI 透明 |
| CLI 分页 | 所有 list 命令**默认自动翻页**,`--page-size` / `--max-items`(默认上限 1000,触顶时输出 `"truncated": true`) | 服务端默认 page_size 为 10/10/10/50,静默截断比报错更毒 |
| CLI JSON 风格 | 直接 `protojson.Marshal` 的 lowerCamelCase | 与审计日志/JSONB 列/网关 REST 一致,不需要自建编码器;错误信封也用同一风格 |
| Token 有效期 | 沿用 `GetTokenDuration`(当前 7 天) | 不新增设置;过期后 CLI 提示重新 `auth login` |
| **server 地址** | `auth login --server <url>` 填写并**持久化**到凭据文件;解析顺序 `--server` > `METAXISDATA_SERVER` > 凭据文件 > **报错** | 用户自托管的 server 基本不变,登录时填一次即可;不做"默认 localhost"或地址探测,避免把请求发到错误的地方。token 由某个 server 签发,因此**地址与 token 必须作为一对原子写入**,否则会出现"token 属于 B、地址记的是 A"的静默失败 |
| token 存储 | CLI **永不打印 token**,只写本机凭据文件(0600);明文落盘是显式接受的取舍 | agent 日志可能被转发/粘贴,打印 token 即泄漏 |

---

## 连接地址与登录凭据

server 地址是**一次性配置、长期复用**的:用户在 `mxd auth login` 时用 `--server <url>` 填一次,CLI 把它持久化进凭据文件,之后所有命令直接使用,不再需要 flag 或环境变量。

### 解析顺序

| 优先级 | 来源 | 说明 |
| --- | --- | --- |
| 1 | `--server <url>` | 单次覆盖;在 `auth login` 上出现时**同时被持久化** |
| 2 | `METAXISDATA_SERVER` | 环境变量;在 `auth login` 上生效时**同样被持久化** |
| 3 | 凭据文件里的 `server` | 上次登录写入的值 |
| 4 | — | **报错 `server_required`,退出码 1** |

没有第 4 档的"默认值":不猜 `http://localhost:8080`,也不探测常见端口。发到错误的 server 上只会得到一个莫名奇妙的 401,比直接说"你还没配地址"难排查得多。

```json
{"error":{"code":"server_required","message":"no server address configured",
  "hint":"run `mxd auth login --server https://mx.example.com` once; it will be saved for later commands."}}
```

地址会被规范化:必须带 `http`/`https` scheme,尾部 `/` 去掉,URL 解析失败或 scheme 非法 → `config_invalid`(退出码 1)。`--ca-cert`/`--insecure` 这些 TLS 选项**不持久化**(它们描述的是当前机器到 server 的路径,可能因环境不同而异)。

### `auth login` 的写入语义

- `auth login` 解析出**生效地址**后,在同一次写入里把 **server 与 token 作为一个原子对**落盘。理由:token 是该 server 签发的,如果只更新 token 而地址记的是另一个 server,后续所有请求都会失败,而且失败原因看不出来。
- 如果生效地址与文件里的旧值不同,登录时在 stderr 打印一行 `server changed: <old> -> <new>`,不弹交互确认(server 变了说明用户就是想换,而他刚显式给了地址)。
- **`auth logout` 只清 token/user/expiry,保留 server**:退出登录不等于换服务器,不该逼用户重新填地址。
- `auth login` 会把实际写入的凭据文件路径打印到 stderr(便于同机多 agent 排查),并且**不读写 scope**。

### 凭据文件

- 默认路径:`$XDG_CONFIG_HOME/metaxisdata/config.json`(Linux 默认 `~/.config/metaxisdata/config.json`),创建即 `chmod 0600`;
- 内容:`{"server": "https://mx.example.com", "token": "...", "user": {"email": "...", "name": "..."}, "tokenExpiresAt": "..."}`;
- `--config <path>` / `METAXISDATA_CONFIG` 指定另一份凭据文件,用于"同机两个 agent 用不同身份/不同 server"的场景;不指定时用默认路径;
- 里面**没有 scope 字段**——scope 只从环境变量读(见下一节)。

### 可观测性:`mxd config show`

只读,回显生效的 server/token 来源与当前 scope,用来排查"这个 agent 进程到底连的是哪儿、带的是什么":

```json
{
  "server": {"value":"https://mx.example.com","source":"file:/home/u/.config/metaxisdata/config.json"},
  "token": {"value":"[REDACTED]","source":"file:/home/u/.config/metaxisdata/config.json"},
  "configFile": "/home/u/.config/metaxisdata/config.json",
  "scopes": [{"name":"dev","guid":"1;shop"},{"name":"prod","guid":"9;shop;public"}],
  "scopeSource": "env:METAXISDATA_SCOPES"
}
```

`server.source` 取值为 `flag` / `env:METAXISDATA_SERVER` / `file:<path>`;`scopeSource` 为 `none` 时 `scopes` 为空数组——一眼能看出"这个进程没带 scope"。

---

## 执行作用域(scope)

### 为什么 scope 是必填的

`catalog.AnalysisContext{InstanceID, Database, Schema}` 是解析未限定名的唯一来源,而 SQL 文本里最多只能写到 `database.schema.table`——**instance 永远来自上下文**。所以 `AnalyzeSQL` 没有 scope 就没有任何可用的答案,不存在"默认猜一个"的合理退路。这一条决定了三件事:

1. scope 在 proto 里是 REQUIRED;
2. CLI 在没有可用 scope 时 fail fast(退出码 1 + `scope_required` + 可执行的修复提示),而不是拿空上下文硬跑;
3. CLI **不替用户决定** scope:没有配置、没有推断、没有 fallback。

### 唯一来源:进程环境变量

| 变量 | 含义 |
| --- | --- |
| `METAXISDATA_SCOPES` | 逗号分隔的 scope 列表;每项为 `guid`,或 `name=guid`(`name` 仅用于输出归因)。例:`dev=1;shop,prod=9;shop;public` |

- **按每次命令执行求值**,不写文件、不写缓存;进程退出即消失。
- `--scope <name|guid|all>` 可按次覆盖,适合一次性查询;它的信任级别与环境变量相同(都是调用方显式给出),不是"CLI 的默认值"。
- `METAXISDATA_SCOPES` 未设置或为空 = 没有 scope。
- `name` 可省略;省略时输出里只回显 `scopeGuid`,不做任何名字推断。

**为什么必须按进程而不落盘**:同一台机器上两个 agent 可能服务不同项目、甚至不同身份。任何"记住上次用的 scope"的设计都会让它们互相覆盖,而且失败是静默的。环境变量把这份状态交给**启动 agent 的人**,这也正是它该在的地方。

**用户的用法**(写在自己的项目 `AGENTS.md` / 启动脚本里,由用户维护):

```bash
# 本项目只涉及这两个库
export METAXISDATA_SCOPES='dev=1;shop,prod=9;shop;public'
```

agent 只负责在调用 `mxd` 前确保这些变量已在它的进程环境里;scope 该填什么由用户决定。

### 怎么拿到精确的 GUID(给用户,不给 agent 猜)

scope 必须是精确 GUID,所以 CLI 的输出必须能直接复制。为此:

- `mxd database list` 每条记录带 `guid`(`Database.guid`,新增 OUTPUT_ONLY)→ MySQL 家族的 scope 直接复制;
- `mxd meta list <db-guid> --type SCHEMA` 每条记录带 `guid`(`StoredMetadata.guid`,新增 OUTPUT_ONLY)→ PostgreSQL 的 scope(`<instanceID>;<database>;<schema>`)直接复制。

GUID 的形态差异(MySQL 家族 schema 段为空、`;` 转义)全部由服务端负责,CLI **把 GUID 当作不透明字符串**,只做透传与展示,不拼、不拆、不推断。这也让 CLI 不需要引入任何 GUID 工具代码。

### 多 scope 的语义(必须写清楚,否则会被误解)

- 多个 scope = **对同一段 SQL 做多次独立解析**,不是把多个上下文合并成一个大上下文。同一个 `orders` 在 dev 与 prod 下会解析成两个不同 instance 的 GUID。
- 因此输出**永远按 scope 分组**,即使只激活了一个 scope,形状也保持 `results[]`(agent 的解析逻辑不必分支)。
- **跨环境的真实数据流不会因为多选了 scope 而被发现**:一条 prod 任务读 dev 表的边,只有在元数据里确实存在(OpenLineage 上报、或跨库的手工 SQL)时才会出现在图里。scope 只决定"未限定名如何落位",不创造边。这个限制写进 proto 注释与 `--help`。
- `GetLineageGraph` 不需要 scope:root 是绝对 GUID,已经隐含了环境。多 scope 只在 `lineage sql`(以及它派生的 `--depth` 展开)上有意义。

### 缺失时的失败形状

```json
{"error":{"code":"scope_required",
  "message":"no analysis scope configured",
  "hint":"set METAXISDATA_SCOPES (e.g. METAXISDATA_SCOPES='dev=1;shop'), or pass --scope <guid>. GUIDs come from `mxd database list` / `mxd meta list --type SCHEMA`; which scopes a project uses is a per-project decision, keep it in your own project docs."}}
```

退出码 1。`hint` 直接给出格式与获取 GUID 的命令,agent 可以把这段原样转达给用户,不需要自己发明一个 scope。

---

## Device Login 设计

### 流程

```
CLI                                  Server                          浏览器(已登录用户)
 │ POST CreateDeviceLogin             │                               │
 │  {clientName,clientVersion}        │                               │
 │ ─────────────────────────────────► │ 生成 device_code(256bit)      │
 │                                    │ 生成 user_code(40bit 可读)     │
 │ ◄───── device_code, user_code, ──  │ 存入 DeviceLoginStore(PENDING)│
 │         verification_uri(+complete),│                              │
 │         expires_in=600, interval=3  │                              │
 │ stderr: 打印 URL + 大字号 user_code │                               │
 │ (--open-browser 时尝试打开浏览器)    │                               │
 │                                    │                               │
 │                                    │ GET /device(用户手动输码) ────►│
 │                                    │ ◄── GetDeviceLogin(详情) ─────│
 │                                    │    ApproveDeviceLogin ───────►│ 用户比对页面上的码
 │                                    │ 绑定确认者 user_id, APPROVED   │ 与终端显示的码是否一致
 │                                    │ (audit)                       │
 │ POST ExchangeDeviceLogin           │                               │
 │  {device_code} ───────────────────► │ 首次见到 APPROVED:             │
 │ ◄── token + user + expires_in ────  │ 签发 JWT、会话消费(删除)        │
 │ 写入凭据文件,stdout 输出一个 JSON    │                               │
```

### 状态机

```
PENDING ──approve(user)──► APPROVED ──exchange(首次)──► CONSUMED(返回 token,条目删除)
   │                        │
   │ deny(user)             │ 超过 expires(签发窗口关闭)
   ▼                        ▼
  DENIED                  EXPIRED(lazy 清理)
```

- approve 时**绑定确认者 user_id**,exchange 时为该用户签发 `GenerateAccessToken`(与 web 登录同一签发路径,含 `profileWithLastLogin` 更新)。
- exchange 一次性:签发后条目立即删除;重复轮询得到 `CodeNotFound`,CLI 必须把它翻译成"device 会话已失效,需重新 `auth login`"(退出码 2),而不是泛化的"未找到"。
- TTL 内未 approve / 未 exchange → lazy 过期。

### 安全边界(必须实现,不是可选项)

1. **不预填 user_code 的默认路径**。页面默认呈现输入框,用户从终端读取并手动输入;`verification_uri_complete` 只作为"用户明确选择 `--prefill-url`"的便利路径存在,并且页面在预填时仍要求点击确认、并把 client 名称/版本/来源 IP 与发起时间显著展示。
2. **device_code 不进审计**。`CreateDeviceLogin` 保持 `audit=true`,但审计拦截器同时落 request 与 response,而 `isSensitiveAuditField` 的子串名单不命中 `deviceCode`。最小改动是把 `devicecode`/`device_code` 加进 `isSensitiveAuditField` 的 bare-name 名单(`userCode` 不是秘密,不需要脱敏),并在 `audit_test.go` 加一条用例钉住。
3. **approve 的资格闸门只覆盖密码认证的 END_USER**。`needResetPassword` 在 web 登录路径里**只在非 IDP 分支求值**;若 device 流程无条件套用,会永久挡住"仅 SSO / 开了密码轮换"工作区的用户,而这些人根本不知道自己的随机密码。因此:仅当确认者是 END_USER **且**该工作区允许密码登录路径时才评估密码策略,并与 web 一致。同时复查 `MemberDeleted`(approve → exchange 之间可能被停用)与 `validateEmailWithDomains` 域限制。
4. **限流分两层**。`CreateDeviceLogin` 按对端 IP 计数(默认 10/min),**并**保留一个更宽松的全局桶,避免反代后所有用户共享同一个 IP 桶而互相饿死。`ExchangeDeviceLogin` 记录 `lastPollAt`,间隔 < 1s 返回 `CodeResourceExhausted`(RFC 8628 `slow_down`)。`GetDeviceLogin`/`ApproveDeviceLogin` 也加一个简单的按调用者计数,避免 user_code 枚举。
5. **user_code 唯一性**。生成后必须在 store 内校验不冲突并重试(10k 并发 + 40bit 空间碰撞概率极低但非零);同时保证 `DeviceLogin` 资源名与 user_code 一一对应。
6. **exchange 时复查账号状态**,不只看 approve 时的快照。

### 新增 proto(`auth_service.proto`)

```protobuf
service AuthService {
  // 开启一次设备登录。匿名、按来源限流、审计(响应中的 deviceCode 必须脱敏)。
  // 无 permission 注解 → 必须登记到 acl_interceptor_test.go 的 unannotatedMethods。
  rpc CreateDeviceLogin(CreateDeviceLoginRequest) returns (CreateDeviceLoginResponse) {
    option (google.api.http) = { post: "/v1/auth/deviceLogins" body: "*" };
    option (metaxisdata.v1.allow_without_credential) = true;
    option (metaxisdata.v1.audit) = true;
  }
  // 确认页读取待确认请求详情。需要登录(cookie/Bearer 均可),无 permission 注解。
  rpc GetDeviceLogin(GetDeviceLoginRequest) returns (DeviceLogin) {
    option (google.api.http) = { get: "/v1/{name=deviceLogins/*}" };
  }
  // 确认/拒绝。需要登录,审计,无 permission 注解。
  rpc ApproveDeviceLogin(ApproveDeviceLoginRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { post: "/v1/{name=deviceLogins/*}:approve" body: "*" };
    option (metaxisdata.v1.audit) = true;
  }
  // CLI 轮询/换取。匿名,以 device_code 为不透明凭据。
  rpc ExchangeDeviceLogin(ExchangeDeviceLoginRequest) returns (ExchangeDeviceLoginResponse) {
    option (google.api.http) = { post: "/v1/auth/deviceLogins:exchange" body: "*" };
    option (metaxisdata.v1.allow_without_credential) = true;
  }
}

message CreateDeviceLoginRequest {
  string client_name = 1;    // 如 "mxd",仅展示用,不可信
  string client_version = 2;
}
message CreateDeviceLoginResponse {
  string device_code = 1;                 // 轮询密钥,256bit,仅 CLI 持有
  string user_code = 2;                   // 人可读,XXXX-XXXX,Crockford base32
  // 不含 user_code 的裸确认地址,{external_url}/device;未配置 external_url 时为空。
  string verification_uri = 3;
  // 带 user_code 的便利地址;仅在用户明确选择 --prefill-url 时使用。
  string verification_uri_complete = 4;
  int32 expires_in = 5;                   // 秒,默认 600
  int32 interval = 6;                     // 建议轮询间隔,秒,默认 3
}
message GetDeviceLoginRequest { string name = 1; } // deviceLogins/{user_code}
message DeviceLogin {
  string name = 1;                       // deviceLogins/{user_code}
  DeviceLoginState state = 2;
  string user_code = 3;
  google.protobuf.Timestamp create_time = 4;
  google.protobuf.Timestamp expire_time = 5;
  string client_name = 6;
  string client_version = 7;
  string request_ip = 8;               // 服务端视角来源 IP(遵循 trusted-proxies 语义)
  string request_user_agent = 9;
  User approved_by = 10;               // 已确认时给出
}
enum DeviceLoginState {
  DEVICE_LOGIN_STATE_UNSPECIFIED = 0;
  PENDING = 1;
  APPROVED = 2;
  DENIED = 3;
  EXPIRED = 4;
}
message ApproveDeviceLoginRequest {
  string name = 1;
  bool approve = 2;
}
message ExchangeDeviceLoginRequest { string device_code = 1; }
message ExchangeDeviceLoginResponse {
  DeviceLoginState state = 1;
  string token = 2;            // 仅 state=APPROVED 时返回;随后条目被消费
  int64 expires_in = 3;        // token 剩余秒数
  User user = 4;               // 确认者
}
```

### 服务端实现要点

- **存储**:`backend/component/state` 新增 `DeviceLoginStore`(互斥锁 + `user_code → session`、`device_code → 同一 session` 两个 map,外加一个按插入顺序的 ring/切片以支持"最旧先逐";容量上限 ~10k,所有读取时 lazy 过期)。新增/消费/确认 O(1)。
- **生成**:device_code 用 `common.RandomString(43)`;user_code 用新增的 Crockford base32 助记符生成器(排除 `0O1I`,8 字符分两段,Crockford 解码时把 `O→0`/`I,L→1` 归一化),生成后校验唯一。
- **Approve**:仅 END_USER 可确认;只能确认 PENDING(对 APPROVED/DENIED/EXPIRED 重复操作返回 `CodeFailedPrecondition`);确认后 `state=APPROVED, approvedBy=userID`;资格闸门见"安全边界 3"。
- **Exchange**:PENDING → 原样返回状态(CLI 继续轮询);DENIED/EXPIRED → 返回状态并删除条目;APPROVED → 复查账号状态、签发 token、删除条目。
- **审计**:Create 与 Approve 都打 `audit=true` 且确认脱敏覆盖 `deviceCode`;Exchange 匿名不审计(3s 一次的轮询会刷爆审计日志)。

### 前端确认页(`/device`)

- 路由 `path: "/device"`,`meta: { requiresAuth: true, layout: "default" }`。未登录时现有 guard 会 `next({name:"Login", query:{redirect: to.fullPath}})`,`user_code` query 自动保留,登录后回到确认页。
- 页面行为:
  1. 默认呈现 user_code **输入框**(Crockford 归一化,接受带/不带连字符与大小写);仅当 URL 带 `?user_code=` 时预填并额外提示"这段链接由谁提供给你?请核对终端上显示的代码";
  2. `GetDeviceLogin` 展示:client 名称/版本、来源 IP、发起时间、**大字号 user_code**;
  3. 确认/拒绝按钮 → `ApproveDeviceLogin`;
  4. 结果态:PENDING/APPROVED/DENIED/EXPIRED/不存在各给 i18n 文案(`en-US`/`zh-CN` 同步加 key,注意 sorted);
  5. 页面**永不展示 token** —— token 只经 exchange 到达 CLI。
- 前端调用走 `frontend/src/api/client.ts` 的 Connect 客户端(新增 `api/device-login.ts`)。

---

## 新增查询 RPC 设计(`lineage_service.proto`)

### 1. `AnalyzeSQL` — 需求 2:任意 SQL 的血缘

```protobuf
rpc AnalyzeSQL(AnalyzeSQLRequest) returns (AnalyzeSQLResponse) {
  option (google.api.http) = { post: "/v1/lineages:analyzeSql" body: "*" };
  option (metaxisdata.v1.permission) = "metaxisdata.lineage.get";
}
// AnalysisScope is one resolution context for a SQL text. A scope is a named
// database (MySQL-family) or schema (PostgreSQL-like). `name` is an opaque
// caller-supplied label echoed back in the response so results can be
// attributed; the server attaches no meaning to it.
message AnalysisScope {
  string name = 1;
  // "instance_1;db2" (MySQL-family) or "instance_1;db2;public" (schema-based).
  // Used only to fill unqualified names; database/schema existence is not
  // validated.
  string guid = 2 [(google.api.field_behavior) = REQUIRED];
}
message AnalyzeSQLRequest {
  // 1..10 scopes. The SQL is analyzed once per scope, independently: scopes may
  // live on different instances and even different engines, and their results
  // are NOT merged. Selecting several scopes does not discover cross-environment
  // edges that are absent from the metadata.
  repeated AnalysisScope scopes = 1 [(google.api.field_behavior) = REQUIRED];
  // 待分析 SQL。服务端限制 1 MiB;超出返回 CodeInvalidArgument。
  string sql_text = 2 [(google.api.field_behavior) = REQUIRED];
}
message AnalyzeSQLResponse {
  repeated AnalyzeSQLResult results = 1;   // 每个 scope 一份,顺序与请求一致
  repeated string warnings = 2;            // 请求级 warning
}
message AnalyzeSQLResult {
  string scope_name = 1;   // 原样回显请求里的 name
  string scope_guid = 2;
  repeated AnalyzeSQLRelation relations = 3;
  // scope 级 warning:引擎无分析器、引用对象不在元数据注册表等。
  // 这些只影响本 scope,不会让整个请求失败。
  repeated string warnings = 4;
}
message AnalyzeSQLRelation {
  // 已用 AnalysisContext 补全后的表级 GUID(未注册对象也照常给出)。
  string source_guid = 1;
  string source_column = 2;
  MetaType source_type = 3;    // 能在 registry 命中则填 TABLE/VIEW/...,否则 UNSPECIFIED
  // target 是 SQL 内部合成对象时(裸 SELECT 的 __result__、CTE、子查询)为空,
  // 此时 target_column 是该查询的输出别名、is_temp=true。
  string target_guid = 4;
  string target_column = 5;
  MetaType target_type = 6;
  RelationType relation_type = 7;
  repeated Transformation transformations = 8;
  bool is_temp = 9;
}
```

实现(在 `backend/api/v1/lineage_service_analyze.go`):

1. 校验:`len(scopes)` 在 1..10(`CodeInvalidArgument`);`sql_text` ≤ 1 MiB;每个 `guid` 非空;
2. 对每个 scope **独立**执行 3~5 步,结果互不影响:
   `common.SplitMetaGUID(scope.guid)` → instance/database/schema;`store.GetInstance` 取 engine(instance 不存在 → 该 scope 记 scope 级 warning,不中断其他 scope);
   `catalog.WithAnalysisContext(ctx, catalog.AnalysisContext{InstanceID, Database, Schema})` 后调 `lineage.GetAnalyzeRelation(ctx, engine, sqlText)` — 与 runner 同款路径,行为一致;
3. 对每条 relation 补全缺省的 instance/database/schema(照抄 runner 的补全逻辑,抽公共函数),构造 source GUID;target 侧:
   - `rel.IsTemp == true` 或 `rel.Target.Table.Name == "__result__"` → **`target_guid` 留空**,`target_column` 保留输出别名,`is_temp=true`;不写 warning;
   - 否则构造 target GUID,`is_temp=false`;
4. 对 distinct guid 批量查 `GetMetaRegistry` 填 `source_type`/`target_type`,未命中则记入该 scope 的 `warnings`(如 `table "1;db2;t1" not found in metadata registry`);
5. `ErrorEngineNotSupported` → 该 scope 的 relations 为空 + `engine MSSQL has no lineage analyzer` warning(**不是**请求级错误:多 scope 里可能只有部分引擎不受支持);解析失败 → 该 scope 的 relations 为空 + 错误文案 warning;
6. 请求级错误(整个请求失败)只在:scope 数量/长度/`sql_text` 非法,或**所有 scope 都以同一原因失败**时返回 `CodeInvalidArgument`/`CodeFailedPrecondition`。这样 `--scope all` 不会因为其中一个引擎不支持而全盘失败;MySQL 分析器要求恰好一条语句、PG 支持多语句的差异,在 CLI 文档与 scope 级 `warnings` 中体现。

**无状态**:不写 `column_lineage`,不触发 runner;与视图分析的血缘语义差异(后者落库、metahash 变更检测)在 proto 注释中写明。

**单测注意**:分析器通过 `backend/server/ultimate.go` 的 blank import 注册到全局 map,`backend/api/v1` 包自身不 import 它们 → 该包的分析测试必须自行 blank import 目标引擎包,否则一律 `ErrorEngineNotSupported`;`lineage.CatelogProvide` 在单测里需要 `InitCatalogProvide` 或注入 `catalog.NewMemoryCatalogProvide()`(`SELECT *` 展开会调用它)。多 scope 单测要覆盖:同名表在两个 scope 下落成两个不同 GUID;一个 scope 引擎不支持而另一个成功;`results` 顺序与请求 `scopes` 顺序一致。

### 2. `GetLineageGraph` — 需求 3:元数据的多层上下游

```protobuf
rpc GetLineageGraph(GetLineageGraphRequest) returns (GetLineageGraphResponse) {
  option (google.api.http) = { get: "/v1/lineages:graph" };
  option (metaxisdata.v1.permission) = "metaxisdata.lineage.get";
}
message GetLineageGraphRequest {
  string guid = 1 [(google.api.field_behavior) = REQUIRED];
  MetaType meta_type = 2;
  LineageType lineage_type = 3;   // SOURCE=只看上游 / TARGET=只看下游 / 不指定=双向
  int32 depth = 4;                // 1..10;未指定默认 3
}
message GetLineageGraphResponse {
  string root_guid = 1;
  repeated LineageNode nodes = 2;        // 表/视图/ManualSQL/外部数据集级节点
  repeated LineageRelation edges = 3;   // 列级边,复用现有消息
  repeated ExternalDatasetInfo external_datasets = 4;
  int32 depth_reached = 5;
  bool truncated = 6;                   // 触达节点/边上限或超出墙钟预算被截断
}
message LineageNode {
  string guid = 1;
  MetaType meta_type = 2;
  string name = 3;          // 对象名;外部数据集为 dataset name
  string database = 4;
  string schema = 5;
  string instance_id = 6;
  int32 distance = 7;       // 距 root 的跳数,上游为负、下游为正
}
```

实现要点:

- 起点 guid 先走与 `GetLineage` 相同的 `getLineageMeta` 校验(外部数据集 GUID 也允许);
- **BFS**:frontier = {root};每层对 frontier 中每个 guid 按方向调 `store.ListColumnLineage`(`Limit`/`Offset` **走完整分页**,与前端 `api/lineage.ts` 同语义),收集边;新出现的对端 GUID 进入下一层 frontier(表级 GUID 去重,列名只是边属性);`depth` 递减到 0 或 frontier 空 / 达到任一上限为止;
- **上限三重**:节点 500、边 10000、单请求墙钟预算(默认 15s,`context.WithTimeout`)。任一触达即置 `truncated=true` 并停止展开;`depth_reached` 报实际到达层数;
- **节点元数据批量取**:把所有新发现 GUID 收集后按批查一次 registry(而不是每节点一次),registry 命中的填 `meta_type/name`;外部数据集填 `ExternalDatasetInfo`;其余从 GUID 分段解析(未同步对象)name 兜底;
- 环:去重天然防死循环;
- `distance` 有符号,agent 可直接按层渲染或做影响面统计;
- **语义说明写进 proto 注释**:图里只有列级边(没有独立的 `table_lineage` 表),表级关系由列级边坍缩得到;分析不出任何列级关系的对象(如 `SELECT count(*)`、未同步对象)不会出现在图里。

---

## CLI 设计

### 代码布局与构建

仓库顶层按可交付物组织:`backend/`(server)、`frontend/`(SPA)、`cli/`(mxd)。CLI 与后端同 module,但物理隔离,避免"backend 里也装客户端"的语义混淆。

```
cli/                           # Go package: github.com/Ranxy/metaxisdata/cli
  main.go                      # package main;全局 flag: --server --token --scope --config --format
                               #              --timeout --debug --ca-cert --insecure --page-size --max-items
  cmd/                         # cobra 命令:auth.go meta.go lineage.go instance.go database.go
                               #              config.go version.go
  client/client.go             # connect 客户端构造、Bearer 注入、错误→退出码映射
  env/env.go                   # METAXISDATA_* 读取与 --scope 覆盖(scope 的唯一来源,无持久化)
  config/config.go             # 本机凭据文件读写(server/token/user),0600;--config 指定路径
  output/output.go             # json/table 渲染
  authflow/device.go           # device login 流程(create→URL→poll)
```

- 不引入 `cli/internal/`:仓库目前没有任何 `internal/` 目录,依赖隔离改用下面的 depguard 规则表达,不新增一套目录约定。
- **依赖边界由 lint 强制**:`.golangci.yaml` 启用 `depguard`,对 `cli/**` 只放行 `$gostd`、`github.com/spf13/cobra`、`connectrpc.com/connect`、`google.golang.org/protobuf`、`github.com/Ranxy/metaxisdata/backend/generated-go`。这样 `cli/` 不会被 `backend/store`、`backend/plugin/*`、`backend/component/*` 拖进服务端内脏;规则本身也是"CLI 只是 API 客户端"这条架构约束的可执行文档。CLI **不解析、不拼装 GUID**(服务端给的 `guid` 一律当不透明字符串透传),所以不需要任何 GUID 工具包。
- `make build-cli` → `build/mxd`(Makefile 新目标:`go build -ldflags "-w -s" -o ./build/mxd ./cli`;不进 `build-release`,独立分发)。`build/` 已在 `.gitignore`。
- 无新依赖:cobra、connectrpc.com/go、标准库。连接用生成的 `v1connect.NewXxxClient(httpClient, baseURL, connect.WithInterceptors(...))`,客户端拦截器注入 `Authorization: Bearer`;TLS 用 `--ca-cert`(追加到系统池)或显式 `--insecure`(打印告警)。
- `chmod 0600` 只在类 Unix 生效;Windows 上退化为默认 ACL,在文档中注明。

### 命令树

```
mxd auth login [--server <url>] [--no-browser] [--prefill-url] [--timeout 10m]   # device login(主路径)
mxd auth login --service-account <email> [--server <url>]                        # 用服务账号密钥换 token(可选)
mxd auth status                                     # GetCurrentUser + 本地 token 到期时间 + 生效 server
mxd auth logout                                     # Logout RPC(吊销)+ 清本地 token(保留 server)

mxd config show                                     # 只读:server/token 来源 + 当前 scope(来自 env)
mxd config path                                     # 凭据文件路径

mxd instance list
mxd database list [--instance id]                   # 每条带 guid → 可直接作为 scope

mxd meta list <parent-guid> [--type TABLE]           # ListMetadata,每条带 guid
mxd meta get <guid> [--type]                        # GetMetadata(表→列/索引/外键/分区/触发器…)
mxd meta search <keyword> [--type] [--parent-guid-prefix]
mxd meta ddl <guid> [--type]                        # GetSchemaString

mxd lineage sql [--scope <name|guid|all>]... [--file a.sql|-] [--depth N]   # AnalyzeSQL(+可选接 GetLineageGraph)
mxd lineage graph <guid> [--depth N] [--direction up|down|both] [--column c]  # GetLineageGraph
mxd version
```

- **首次使用只有一步配置**:`mxd auth login --server <url>`,地址随 token 一起持久化;之后 `mxd database list`、`mxd lineage ...` 都不需要再带 `--server`。非 auth 命令如果解析不到地址,报 `server_required` 并提示去跑 `mxd auth login --server <url>`。
- **没有 `mxd scope ...` 命令组**:scope 不由 CLI 管理。`config show` 只做回显。
- `lineage sql` 没有位置参数的 scope:`--scope` 覆盖环境变量,其余情况一律用 `METAXISDATA_SCOPES`。
- `lineage sql --depth N`:先按选中的 scope 调 `AnalyzeSQL`;再把**每个 scope 各自**的 `is_temp=false` 且 `target_guid` 非空的 target 作为 root 调 `GetLineageGraph(depth=N-1)`,结果按 scope 归到该组的 `graphs[]`(跨 scope 不做合并)。默认 `--depth 0` 表示只解析该语句一跳,需求 2 的"多层"由显式 depth 触发。scope 数超过服务端上限 10 时,CLI 自动分批调用再合并。
- `--column` 是 CLI 侧过滤:只展示 `source_column/target_column == c` 的边(便于回答"这个列从哪来/影响谁")。
- GUID 全程当不透明字符串:CLI 只透传 `database list` / `meta list` 输出的 `guid`,不拼也不拆。MySQL 家族在 database 与 table 之间有一层空名 schema(`ListMetadata` 会把它作为一个 SCHEMA 条目返回),对 agent 是可见且一致的层级。

### 配置与令牌存储

CLI 只持久化**本机登录凭据**,不持久化任何 scope。地址与凭据的完整规则见"连接地址与登录凭据"一节,这里只列总表:

| 项 | 来源(高 → 低) | 是否持久化 |
| --- | --- | --- |
| server | `--server` > `METAXISDATA_SERVER` > 凭据文件 > **报错** | `auth login` 写入(与 token 原子写入) |
| token | `--token` > `METAXISDATA_TOKEN` > 凭据文件 | `auth login` 写入 |
| **scope** | `--scope` > `METAXISDATA_SCOPES` | **从不写入任何文件** |
| `--ca-cert` / `--insecure` | flag / env | **不持久化**(与当前环境的网络路径有关) |

- 凭据文件默认 `$XDG_CONFIG_HOME/metaxisdata/config.json`(Linux 默认 `~/.config/metaxisdata/config.json`),创建即 `chmod 0600`;
- 内容:`{"server": "...", "token": "...", "user": {"email": "...", "name": "..."}, "tokenExpiresAt": "..."}`;
- `--config <path>` / `METAXISDATA_CONFIG` 指定另一份凭据文件,用于"同机两个 agent 用不同身份/不同 server"的场景;不指定时用默认路径;
- `auth login` **写入当前生效的凭据文件**、把 server 与 token 一起落盘,并把文件路径打印到 stderr;它不读写 scope;
- `auth logout` 只清 token/user/expiry,**保留 server**;
- token 永不打印到 stdout/stderr;成功时 stdout 输出**一个** JSON 对象 `{"status":"approved","server":"...","user":{...},"tokenExpiresAt":...}`,进度与提示走 stderr;
- 明文落盘是显式取舍:CLI 侧凭据等价于 7 天会话,与"存储凭据只做混淆不做加密"的既有姿态一致,写入 `AGENTS.md` 的 Security and deployment posture 一节。

### 分页契约

所有 list 命令**默认自动翻页**,直到 `next_page_token` 为空或达到 `--max-items`(默认 1000);触顶时输出信封带 `"truncated": true` 与 `"nextPageToken": "..."`,agent 可据此决定是否显式续查。服务端默认 `page_size` 是 10(instances/databases/metadata,**metadata 未指定 type 时按类型各自 10 条**)/ 50(search),因此"只调一次"会静默丢数据。

### 输出与退出码

- 默认 `--format json`(stdout 恰好一个 JSON 对象,字段为 protojson 的 lowerCamelCase);`lineage sql` 的结果、`graph` 的 nodes/edges 均为结构化数据;
- `--format table` 供人用(列宽按 TTY 推断,非 TTY 时退化为固定宽度);`--format mermaid` 作为后续增强(见 Further Considerations);
- 错误统一到 stderr,JSON:`{"error":{"code":"unauthenticated","message":"..."}}`;
- 退出码:`0` 成功;`1` 参数/输入错误(含 `server_required`、`scope_required`、`config_invalid`);`2` 未认证(token 缺失/过期/被吊销,**或 device 会话已失效**)→ agent 应执行 `mxd auth login`;`3` 资源未找到;`4` 权限不足(`CodePermissionDenied`);`5` 服务端错误;`6` 超时/取消;
- 错误码映射:`CodeUnauthenticated`→2,`CodePermissionDenied`→4,`CodeNotFound`→3(`ExchangeDeviceLogin` 的 `CodeNotFound` 特判为 `device_session_expired`,仍归 2),`CodeInvalidArgument`/`CodeFailedPrecondition`→1,`CodeDeadlineExceeded`/context canceled→6,其余→5;
- 任何 `CodeUnauthenticated` 响应会顺手清掉本地失效凭据。

### Agent 使用示例

```
# 一次性配置:登录时填 server,之后所有命令都不用再带
$ mxd auth login --server https://mx.example.com
# 以下为 stderr(人类可读,含实际写入的凭据文件路径;server 从无到有也会在这里提示):
# 请在浏览器打开 https://mx.example.com/device 并输入代码:7Q2X-9M4K(600 秒内有效)
{"status":"approved","server":"https://mx.example.com","user":{"email":"dev@example.com"},"tokenExpiresAt":"2026-01-08T10:00:00Z"}

# scope 由用户写进项目的 AGENTS.md / 启动脚本,agent 只是带着它执行
$ export METAXISDATA_SCOPES='dev=1;shop,prod=9;shop;public'

$ mxd config show
{"server":{"value":"https://mx.example.com","source":"file:/home/u/.config/metaxisdata/config.json"},
 "token":{"value":"[REDACTED]","source":"file:/home/u/.config/metaxisdata/config.json"},
 "configFile":"/home/u/.config/metaxisdata/config.json",
 "scopes":[{"name":"dev","guid":"1;shop"},{"name":"prod","guid":"9;shop;public"}],
 "scopeSource":"env:METAXISDATA_SCOPES"}

$ mxd database list --instance 1
{"databases":[{"name":"instances/1/databases/shop","guid":"1;shop"}],
 "truncated":false}

$ mxd meta search orders --type TABLE
{"results":[{"guid":"1;shop;;orders","metaType":"TABLE"}],"truncated":false}

$ mxd lineage graph '1;shop;;orders' --depth 5 --direction down
{"rootGuid":"1;shop;;orders","nodes":[...],"edges":[...],"depthReached":4,"truncated":false}

$ mxd lineage sql --file etl.sql --depth 2
{"results":[
   {"scopeName":"dev","scopeGuid":"1;shop",
    "relations":[{"sourceGuid":"1;shop;;orders","sourceColumn":"amount","targetGuid":"1;shop;;order_daily","isTemp":false}],
    "graphs":[{"rootGuid":"1;shop;;order_daily","nodes":[...],"edges":[...]}],
    "warnings":[]},
   {"scopeName":"prod","scopeGuid":"9;shop;public",
    "relations":[{"sourceGuid":"9;shop;public;orders","sourceColumn":"amount","targetGuid":"9;shop;public;order_daily","isTemp":false}],
    "graphs":[{"rootGuid":"9;shop;public;order_daily","nodes":[...],"edges":[...]}],
    "warnings":[]}],
 "warnings":[]}

# 全新机器、还没登录:明确失败,并给出那一条命令
$ mxd database list
{"error":{"code":"server_required","message":"no server address configured",
          "hint":"run `mxd auth login --server https://mx.example.com` once; it will be saved for later commands."}}
# 退出码 1

# 进程里没有 scope 时同样明确失败,并指向"这是项目的决定"
$ mxd lineage sql --file etl.sql
{"error":{"code":"scope_required","message":"no analysis scope configured",
          "hint":"set METAXISDATA_SCOPES (e.g. METAXISDATA_SCOPES='dev=1;shop'), or pass --scope <guid>...."}}
# 退出码 1
```

---

## 实施步骤

> 每阶段独立可合入;proto 改动需与 `buf generate` 产物同 commit。

### Phase 1 — Proto(前置)

1. `proto/v1/v1/auth_service.proto`:4 个 device login RPC + 消息;
2. `proto/v1/v1/lineage_service.proto`:`AnalyzeSQL`(含 `AnalysisScope`)、`GetLineageGraph`;
3. `proto/v1/v1/database_service.proto`:给 `Database` 加 `guid`、给 `StoredMetadata` 加 `guid`,均为 `[(google.api.field_behavior) = OUTPUT_ONLY]`(与同文件的 `ManualSQL.guid` 同型)。这是"用户能复制到精确 scope GUID"和"`meta list` 结果能喂给下一跳"的前提;
4. `buf format -w proto && buf lint proto && cd proto && buf generate`;
5. 提交 `backend/generated-go/`、`frontend/src/types/proto-es/`、`proto/gen/grpc-doc/`。

### Phase 2 — 后端 Device Login

1. `backend/component/state/device_login.go`:`DeviceLoginStore`(状态机、TTL、容量与最旧先逐、poll 间隔、user_code 唯一性);配套单测(状态迁移、一次性消费、过期、限流、碰撞重试);
2. `backend/api/v1/auth_service_device_login.go`:四个方法 + user_code 生成器(Crockford base32);`grpc_routes.go` 无需改(handler 已由 `NewAuthServiceHandler` 统一注册);
3. **`backend/api/v1/acl_interceptor_test.go`**:把 4 个 device RPC 加入 `unannotatedMethods`,否则 `TestEveryMethodIsPermissionGated` 失败;
4. **`backend/api/v1/audit.go` + `audit_test.go`**:`isSensitiveAuditField` 补 `devicecode`,`audit_test.go` 加用例钉住 `deviceCode` 被脱敏;
5. 约束实现:Approve 仅 END_USER、资格闸门与 web 一致(密码策略只对密码认证路径、`MemberDeleted`、域限制)、exchange 复查账号状态、profile last login 更新;
6. 单测参考 `sso_state_test.go` / `auth_throttle_test.go` 的模式。

### Phase 3 — 后端血缘 RPC 与 GUID 输出

1. `backend/api/v1/lineage_service_analyze.go`:`AnalyzeSQL`(scope 数量/长度校验;逐 scope 独立解析与 scope 级 warning;与 runner 共享的 GUID 补全逻辑抽到公共 helper;`__result__`/`IsTemp` 的 target 规则;`sql_text` 上限);
2. `backend/api/v1/lineage_service_graph.go`:`GetLineageGraph`(BFS 抽成接受 fetcher 函数的纯函数以便单测;每节点完整分页;批量 registry 查询;节点/边/墙钟三重上限);
3. 填 `Database.guid`(`database_service.go`/`database_convert.go`)与 `StoredMetadata.guid`(`database_metadata.go` 的转换处);
4. 单测:用 `plugin/lineage/testutil` 驱动 AnalyzeSQL 的 SQL 解析矩阵(**测试文件需 blank import 目标引擎包,并注入 catalog provide**);断言裸 SELECT 无假 `target_guid`、CREATE VIEW/INSERT…SELECT 的 target 正确;断言两个 scope 下同名表落成两个不同 GUID、部分 scope 引擎不支持时请求仍成功且 `results` 顺序与请求一致;断言 `ListMetadata` 返回的每项都带 `guid`;BFS 的深度/去重/截断/环用内存 fake store 测。

### Phase 4 — 前端确认页

1. `frontend/src/api/device-login.ts` + `client.ts` 注册;
2. `frontend/src/pages/DeviceLoginPage.vue` + 路由 `/device`(默认手动输码、预填时额外提示、大字号比对);
3. `locales/en-US.json`、`zh-CN.json` 加 key(`pnpm --dir frontend i18n:sort`);
4. `biome:check` / `lint` / `type-check` / `test run` 全过。

### Phase 5 — CLI

1. `cli/` 骨架(cobra + client + env + config + output + authflow),并在 `.golangci.yaml` 落 `depguard` 规则;
2. **env/凭据子系统**:读 `METAXISDATA_SERVER` / `METAXISDATA_TOKEN` / `METAXISDATA_SCOPES` / `METAXISDATA_CONFIG`,叠加 `--server` / `--token` / `--scope` 覆盖;`METAXISDATA_SCOPES` 解析成 `{name?, guid}` 列表,**只存在于内存**;server 解析不到时 `server_required`;
3. **`auth login` 的持久化**:接受 `--server <url>`(URL 规范化 + scheme 校验),把 **server 与 token 作为一对原子写入**凭据文件,地址变化时在 stderr 提示 `server changed: <old> -> <new>`;`auth logout` 保留 server;
4. `auth`、`instance`、`database`、`meta`、`lineage`、`config` 命令组;所有 list 命令按"分页契约"自动翻页;`lineage sql --scope`(超 10 自动分批)与 `--depth` 组合;无 scope 时的 `scope_required` 失败路径;
5. `--ca-cert`/`--insecure`、`--page-size`/`--max-items`;(可选)`auth login --service-account`;
6. Makefile `build-cli` 目标(`./cli` → `build/mxd`);
7. `gofmt`、`golangci-lint run`(全量,不带文件名);
8. CLI 单测:env 与 flag 的优先级(用 `t.Setenv`);server 解析四级顺序与 `server_required`;`auth login --server` 落盘后**不带任何 flag/env 的后续命令能读到该地址**;`auth logout` 后 server 仍在;`--config` 指向另一份文件时的写入路径;`--server` 非法 scheme 的 `config_invalid`;`METAXISDATA_SCOPES` 解析(带名/不带名/空值/非法项);**断言 CLI 从不写 scope 到磁盘**;错误→退出码映射(含 `scope_required`、`device_session_expired`);分页自动续查;输出渲染(stdout 单 JSON);device flow 的轮询循环(用 httptest fake server)。

### Phase 6 — 集成测试与文档

1. `backend/test/integration` 增补:
   - device login 端到端:create →(带认证客户端)Get/Approve → exchange 拿 token → 用该 token 调 `GetMetadata` 成功;并覆盖拒绝(DENIED)、超时(EXPIRED)、重复 exchange(NotFound);
   - `AnalyzeSQL` 对种子 MySQL fixture(含视图/INSERT…SELECT/裸 SELECT)的边数与方向断言,含"裸 SELECT 不产出 `__result__` GUID";再加一个**双 scope**用例(同一 instance 的另一个 database),断言两个 scope 各自落位;
   - `GetLineageGraph` 对 fixture 的 depth/`truncated` 断言;
   - `database list` / `meta list` 的输出都带 `guid`,且该 `guid` 可直接作为 `AnalyzeSQL` 的 scope 得到非空结果(用 MySQL 与 PG fixture 各验一次形态差异);
2. `gofmt` + `golangci-lint run --allow-parallel-runners` 清零;`go build` server 与 CLI;
3. 文档:把 token 落盘取舍与"多副本需单实例/粘性路由"写入 `AGENTS.md` 的 Security and deployment posture,并在其 Project Architecture 表中登记新的顶层目录 `cli/`;同时写清两条约定——**server 在 `auth login --server` 时填一次并持久化**、**scope 只来自环境变量、不由 CLI 管理、由用户在项目文档中维护**;
4. 本文档更新为实施后状态(或按需精简)。

## Relevant Files(新增/改动一览)

| 类型 | 文件 |
| --- | --- |
| proto | `proto/v1/v1/auth_service.proto`、`proto/v1/v1/lineage_service.proto`、`proto/v1/v1/database_service.proto`(`Database.guid`、`StoredMetadata.guid`)+ 三处生成产物 |
| 后端 | `backend/component/state/device_login.go`(新)、`backend/api/v1/auth_service_device_login.go`(新)、`backend/api/v1/lineage_service_analyze.go`(新)、`backend/api/v1/lineage_service_graph.go`(新)、`backend/api/v1/auth_service.go`(共享 helper 微调)、`backend/api/v1/database_service.go` + `database_convert.go` + `database_metadata.go`(填 guid)、**`backend/api/v1/acl_interceptor_test.go`(device RPC 登记白名单)**、**`backend/api/v1/audit.go` + `audit_test.go`(脱敏 `deviceCode`)** |
| 前端 | `frontend/src/router/index.ts`、`frontend/src/pages/DeviceLoginPage.vue`(新)、`frontend/src/api/device-login.ts`(新)、`frontend/src/api/client.ts`、`frontend/src/locales/{en-US,zh-CN}.json` |
| CLI | `cli/**`(新,与 `backend/`、`frontend/` 同级)、`Makefile`、`.golangci.yaml`(`depguard` 边界规则) |
| 文档 | `AGENTS.md`(Project Architecture + Security and deployment posture + scope 约定)、`cli/README.md`(用法与 scope 约定) |

**不需要改**:数据库 schema(无 migration)、IAM 权限目录与预置角色基线(device RPC 走无注解白名单,不改 `permission.json`)、`grpc_routes.go` 的注册代码、`buf.gen.yaml`、平台 Environment 相关的一切。

## 测试与验收(DoD)

1. `make build-cli` 产出可运行的 `build/mxd`;
2. 手工验收脚本:全新环境直接 `mxd database list` → 退出码 1 + `server_required` → `mxd auth login --server https://mx.example.com` → 浏览器手动输码确认 → **不带任何 server flag/env** 直接 `mxd database list --instance 1` 成功 → 复制 `guid` → `export METAXISDATA_SCOPES='dev=<guid>'` → `mxd config show` → `mxd meta list '<guid>'` → `mxd lineage sql --file q.sql --depth 2` → `mxd lineage graph <guid> --depth 5`;
3. **server 持久化**:`auth login --server <url>` 后凭据文件里 `server` 与 `token` 同时更新;`auth logout` 后 `token` 被清除而 `server` 保留,再次 `auth login` 不必重填地址;`--server` 指向新地址登录时 stderr 出现 `server changed`;
4. **多 agent 隔离**:同一台机器两个 shell 带不同的 `METAXISDATA_SCOPES`,`mxd lineage sql` 各自只解析自己的 scope、`mxd config show` 显示各自的 `scopes`/`scopeSource`;反复执行前后,磁盘上除凭据文件外**没有任何 scope 相关写入**(测试用只读 HOME 或对比文件 mtime/内容断言);
5. **多作用域**:一次 `mxd lineage sql --scope all` 在两个 scope 下各返回一组 `results`,同名表落成两个不同 GUID;其中一个 scope 的引擎不受支持时,该组只有 warning,整体仍成功(退出码 0);
6. **无 scope 路径**:`METAXISDATA_SCOPES` 未设置且未给 `--scope` 时退出码 1、`code=scope_required`,并带可执行 `hint`;
7. **GUID 可复制**:`database list` 与 `meta list` 的每项都有 `guid`,把它用作 scope 能得到非空分析结果(MySQL 与 PG 各验一次);
8. 拒绝路径:浏览器点"拒绝"后 CLI 收到 `DENIED` 且退出码 2;10 分钟不操作 CLI 收到 `EXPIRED`;错误 user_code 页面提示不存在;exchange 消费后再次调用得到 `device_session_expired`;
9. `go test ./...`(含 `TestEveryMethodIsPermissionGated` 与 `deviceCode` 脱敏用例)与 `make test-integration-smoke` 通过(Docker 可用时);
10. agent 闭环:仅凭 `mxd --help`、stdout 的单个 JSON 与退出码,一个 LLM agent 能完成"搜索表→看结构→查上下游血缘→分析一段 SQL 并展开多层";
11. `mxd meta search` 在结果超过一页时不截断(自动翻页)或明确给出 `truncated`/`nextPageToken`。

## Further Considerations

- **多副本部署(当前约束,不是未来问题)**:`DeviceLoginStore` 是进程内的;多副本 + 负载均衡会让 approve 落在与 create 不同的副本上并返回 NotFound,表现为随机失败。部署要求写死为"CLI 授权需单实例或前置粘性路由",并在启动时若检测到多副本配置就日志告警。若多副本成为常态,把该 store 换成带 TTL 的表(一次 migration)即可,接口不变。
- **确认页强化(可选)**:高敏工作区可要求"sudo 模式"(token 签发 < 5 分钟内才允许 approve),或 approve 时二次输入密码。当前按"已登录 + 码比对 + 显式点击"实现。
- **CLI 分发**:加 goreleaser / `go install github.com/Ranxy/metaxisdata/cli@version` 可作为后续(同 module 才能成立);`--format mermaid` 渲染血缘图也留给 v2。若将来 CLI 需要独立版本节奏或极简依赖图,正确路径是先把 `backend/generated-go` 抽成独立 module,再让 `cli/` 成为独立 module —— 在那之前独立 module 只会因 `replace` 让 `go install @version` 失效。
- **scope 约定的落地形式**:本方案只规定"从 `METAXISDATA_SCOPES` 读"。用户怎么维护这份清单(写在项目 `AGENTS.md`、`.env`、orchestrator 配置)属于各项目的自由;可以做一个 `mxd config show` 之外的小工具/文档模板给用户抄,但**不要**让 CLI 去读写这些文件。
- **一台机器连多个 server(可选)**:当前设计假定"一个用户对本机默认只有一台 server",地址在登录时持久化。若确实需要并存多个 server,用 `--config <path>` 各存一份凭据即可,不需要引入命名的 profile/别名概念;真到了那一步,再加 `--manager <name>` 之类的选档参数也只是在凭据文件外面套一层 map,接口不受影响。
- **AnalyzeSQL 的落库变体**:若之后出现"把这段 SQL 登记为 ManualSQL 并参与周期分析"的需求,走已有 `CreateManualSQL`,与本 RPC 正交。
- **token 有效期设置**:CLI 场景 7 天偏短,`GetTokenDuration` 的注释已预留工作区设置位;需要时加 setting,不影响本方案接口。服务账号路径只能拿到 1 小时的 API token(`apiTokenDuration`),CI 建议用 `METAXISDATA_TOKEN` 显式注入。
- **`GetLineageGraph` 的续查**:当前 `truncated=true` 无法续查;若真实图经常触顶,再加 `next_page_token`(以 frontier 快照为游标),接口向前兼容。
- **scope 数上限**:服务端一次最多 10 个 scope(`--scope all` 由 CLI 分批)。若某个项目真的需要一次覆盖几十个库,更合适的做法是让用户在项目文档里明确"这次只关心哪几个",而不是调大上限——上限本身就是防止"顺手 all 一下"的护栏。
