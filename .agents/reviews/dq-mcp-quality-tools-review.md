# P1-8（MCP 质量只读工具）— 对抗性 review

Reviewer：一次独立对抗性 review，在全新上下文中只读执行。对象为分支 `feat/dq-mcp-quality-tools`
（base `main@35d0d13`，HEAD `cc8ed52`；feature commits `008bf67` 实现、`3708529` Lead 文档、
`cc8ed52` guide 澄清）。

**快照与工作树**：review 开始与结束时 HEAD 均为 `cc8ed52`，`git status --short` 为空（干净），
期间无并发提交，因此所有结论固定到 **`cc8ed52`**。本 review 未修改任何产品代码、测试、文档或
生成物，未提交；唯一写入是本文件。

**读了什么**：`git diff 35d0d13..HEAD`（12 文件、+1433/−32）全文与周边文件；
`proto/v1/v1/data_quality_service.proto` 的五条 RPC、权限注解、`QualityTarget`/`QualityTargetKey`/
`QualityTargetSummary`/`QualityIssue`/`QualityExecution`/五个 Request/Response 以及全部 DQ 枚举名；
`backend/mcp/{tool,server,ref,errors,guide}.go`、`tool_{guard,schema}_test.go`、`tool_test.go`、
`sdk_contract_test.go`；为判定工具行为而读的未改动实现：`backend/api/v1/data_quality_{service,target,
convert}.go`、`backend/store/quality_{target,execution}.go`、`backend/runner/qualityrunner/
execution_result.go`、`backend/component/dataquality/model.go`、`backend/server/grpc_routes.go` 的
MCP 接线、`backend/common/permission` 与 `backend/store/predefined_roles.go`；`AGENTS.md`、
`backend/AGENTS.md`、`backend/mcp/AGENTS.md`；六份必须一致的文档（`backend/mcp/guide.md`、
`docs/mcp.md`、`.agents/docs/mcp-server.md`、`.agents/docs/data-quality.md` 的 D-91/§3.11/§7 #39/
Where things live、`docs/data-quality.md`、`.agents/docs/connected-apps.md`）以及 gap analysis 的
P1-8 行；结构对照 `.agents/reviews/dq-schedule-tiers-review.md`。

**范围核对**：`git diff --stat 35d0d13..HEAD` 只含 `backend/mcp/`（实现 + 3 个测试）与文档、一个
集成测试文件；**无 proto、迁移、前端、依赖、`go.mod` 改动**；既有十个工具除 `server.go` 的
`QualityReader` 接口与 instructions 字符串外未被改动。五个工具、无 LLM explain 与 D-91 一致；
proto 的 `ExplainQualityExecution` 带 `audit=true` 且写 `data_quality_llm_cache`，不放进只读面是
正确取舍。

## 实际验证过的内容

| 命令 / 核对 | 结果 |
| --- | --- |
| `gofmt -l backend/` | 空（exit 0） |
| `golangci-lint run --allow-parallel-runners` | **exit 1**：只显示 3 条 `revive/multiline-if-init`（默认 `max-same-issues: 3` 截断），全部在 `backend/plugin/db/pg/sync.go:379/1188/1525`，不在 diff 中；`git blame` 指向 `c1f8cc8`（2025-12-25，早于 base）→ pre-existing |
| `golangci-lint run --allow-parallel-runners --max-same-issues=0 --max-issues-per-linter=0` | **173 条，全部同一条 `revive/multiline-if-init`**；与 `git diff --name-only 35d0d13..HEAD` 求交集为空 → **本 diff 新引入 0 条**。本地 lint v2.14.0，CI pin v2.13.1（`.github/workflows/ci.yml:76-78`） |
| `go test -count=1 ./backend/mcp/...` | **PASS**（0.07s），含 5 个新工具的全部用例 |
| `go test -count=1 ./...` | **全部通过**（exit 0） |
| `make build` | **exit 0** |
| `go test -count=1 -tags=integration -run '^TestMCPAuthorizationAndToolsRealServerIntegration$' ./backend/test/integration/runner` | **PASS**（20.3s；真实 Docker：testcontainers v0.41.0 起 `postgres:16-alpine`，非 skip）。断言 `require.Len(t, tools.Tools, 15)` 与 `list_quality_issues` 空数据集冒烟均通过 |
| 静态契约核对 | `wantTools` 15 条、`readPermissions` 8 条且每条都有工具使用、`tool_schema_test` 15 个 case 与 15 个工具一一对应；五个工具的 `RPC`/`Permission` 与 proto 注解逐条相等（`rules.list`×2、`issues.list`、`executions.list`、`executions.get`），都只读、都走 `dispatch`；无 `AddTool`、无 GUID 拼装 |

## 结论摘要

- **Blocking：0。** 五个工具与 RPC/权限/审计/无状态性契约一致；过滤器透传、投影、enrichment
  及其失败容忍、未监控形状、枚举错误都有测试；范围纪律干净；文档总体与代码一致。
- **Should-fix：2（S1、S2）**，都属于「零 vs 缺席 / 暂停不等于健康」的真值性问题：模型可能据投影
  断言记录并不支持的事实。
- **Nit：7（N1–N7）**：文档过度声明/措辞矛盾、行号指针、一处对象解析边界、以及若干测试缺口。
- 刻意接受/已核实无问题项见文末。

## 发现

| # | 严重度 | 一句话 | 状态 |
| --- | --- | --- | --- |
| S1 | should-fix | `list_quality_executions` 把「没有观测值」输出成 `observedValue: 0`，与真实观测 0 不可区分 | open |
| S2 | should-fix | `get_quality_target.monitored` 与 `list_quality_targets.worstStatus` 丢了 `get_quality_signal` 已有的 all-paused 防护 | open |
| N1 | nit | guide/docs 说 issue 行一定有 `target.path`，实际「无存活检查」或「补全失败」时省略；且未说是快照 | open |
| N2 | nit | §7 #39 末句「不要用字段是否存在判断语义」与投影设计自相矛盾 | open |
| N3 | nit | D-91「后者由执行列表与 get_metadata 回答」指代混乱 | open |
| N4 | nit | 两个列表工具的 `object` 解析到不可监控类型时静默返回空列表 | open |
| N5 | nit | 5 处测试缺口（见下） | open |
| N6 | nit | `.agents/docs/mcp-server.md` 的行号指针因本 diff 插入而过时 | open |
| N7 | nit | enrichment 的 `rules.list` 权限含义未记录（行为本身按既有惯例接受） | open |

### S1 — `list_quality_executions` 把未观测的执行报成 `observedValue: 0`

**证据**：投影字段没有 omitempty，赋值直接取 proto 的零值：
`backend/mcp/tool.go:1244`（`ObservedValue float64 \`json:"observedValue"\``）与
`backend/mcp/tool.go:1342`（`ObservedValue: execution.GetObservedValue()`）。

这个 0 并非记录里的观测值：store 的 `ObservedValue` 是 `*float64`，`nil` 表示「ERROR 或
NOT_EVALUATED」（`backend/store/quality_execution.go:58-60`）；runner 的 `observedValue()` 对
`ERROR`/`SKIPPED`/`NOT_EVALUATED`/`UNSPECIFIED` 一律返回 nil（`backend/runner/qualityrunner/
execution_result.go:205-220`）；API 转换把它压成 0（`backend/api/v1/data_quality_convert.go:526-533`，
注释明说调用方要区分就看 status）。更糟的是 `ObservedKind` 不一定缺席：NULL 观测（`ObservedNull`，
`backend/component/dataquality/model.go:60-63`）仍会带 kind，于是行会是
`observedValue: 0, observedKind: QUALITY_OBSERVED_KIND_COUNT, outcome: QUALITY_STATUS_NOT_EVALUATED`
——与「真实观测 0」完全无法区分，也与 D-91「输出省略未设字段」的承诺相反。
现有测试只覆盖被判定（PASS/WARN/FAIL）的行：`backend/mcp/tool_test.go:1143`（outcome=FAIL、
observedValue=7），没有 ERROR/SKIPPED/NOT_EVALUATED 行。

**影响**：列表是模型找「最近观测值」并转述给用户的入口；模型会把 0 当事实说出口。

**修复（最小）**：把字段改为 `ObservedValue *float64 \`json:"observedValue,omitempty"\``，仅当
`outcome ∈ {PASS, WARN, FAIL}`（即 API 转换确实写入观测值的状态）时赋值，并补一个 ERROR/
NOT_EVALUATED 行断言 JSON 里没有 `observedValue` 的用例。`get_quality_execution` 返回整条 proto，
保留 0 是 API 契约（`observed_value` 非 optional），建议在 `guide.md` 该工具段落或 `docs/mcp.md`
加一句「`observedValue` 为 0 且 outcome 不是 PASS/WARN/FAIL 时表示没有观测值」。顺带把 D-91 的
「输出省略未设字段」精确成「枚举、时间戳、可选 double 未设即省略；计数与布尔始终出现（0/false
是合法值）」——否则与 observedValue 的实现（以及有意的计数/布尔投影）对不上。

### S2 — `get_quality_target.monitored` / `list_quality_targets.worstStatus` 丢掉 all-paused 防护

**证据**：`backend/mcp/tool.go:1164` 用 `Monitored: summary.GetRuleCount() > 0` 判定，而
`rule_count` 统计所有非归档规则、**含 PAUSED**（`backend/api/v1/data_quality_target.go:559-565` 的
`summary.RuleCount++` 循环；proto 定义 `QualityTargetSummary.rule_count`「Live rules only; archived
rules are excluded」）。target 只有暂停检查时 → `monitored: true`，没有 `allPaused`，也没有
「暂停时省略 verdict」的处理。

同一只读面的对照：`get_quality_signal` 正是为此加了 `allPaused`，并在暂停时省略 `worstStatus`
（`backend/mcp/tool.go:648-652`、`:724-742`），guide 也专门写了这条（`backend/mcp/guide.md:95-99`）。
`list_quality_targets` 的行同样只有 `enabledRuleCount`/`pausedRuleCount`（`backend/mcp/tool.go:1407-1431`），
但 `worstStatus` 的聚合 SQL 明确包含 PAUSED（`backend/store/quality_target.go:409` 的
`r.deleted = FALSE AND r.state <> ARCHIVED`），而 `qualityScore` 只算 ENABLED
（`backend/api/v1/data_quality_target.go:703-708`）。于是「全部暂停」的行会带一个历史 FAIL 的
`worstStatus`、没有分数、没有 all-paused 标记；D-77 与前端靠 `enabled_rule_count = 0` 渲染「已暂停、
不是健康结论」（`.agents/docs/data-quality.md:111`），MCP 消费方没有这条文案，只能自己推。
`backend/mcp/guide.md:133-135`「A watchable table with no live check answers with `monitored: false`」
在全暂停时容易被读成 `monitored: false`，而实现返回 `monitored: true`（暂停规则仍计入 `ruleCount`），会放大误读。

**影响**：模型可能把「全部暂停」的表说成「被监控且 FAIL」，或以 `monitored: true` 当作「有东西在
验证」。这正是 D-77 与 `get_quality_signal` 刻意避免的断言。

**修复（推荐 a）**：
(a) 给 `getQualityTargetResult` 加 `allPaused bool`（`enabledRuleCount == 0 && ruleCount > 0`），
`qualityTargetRow` 加同一标记；暂停时的语义与 `get_quality_signal` 对齐，并补 all-paused 用例。
(b) 若不加标记，则在 `guide.md`、`docs/mcp.md`、D-91/§7 #39 写明：`monitored`/`ruleCount` 只表示
「存在非归档检查」，「有没有在被验证」看 `enabledRuleCount == 0`，`worstStatus` 对暂停规则是历史；
同时修正 `guide.md:133` 的措辞。

### N1 — issue 行 `target.path` 的文档过度声明（并缺快照语义）

**证据**：`backend/mcp/guide.md:138-139` 与 `docs/mcp.md:124-125` 都写「issue 行由 target 的存储路径给它的表命名」。实现是一次 best-effort 的精确选择（`backend/mcp/tool.go:1016-1044`），失败静默
（`:973-976`）；目标已无存活检查时也缺席。D-91（`.agents/docs/data-quality.md:127`）与 §7 #39
（`:768`）都写了「补全失败/无存活检查时省略」，但面向模型的 guide 与 `docs/mcp.md` 没写。另外 proto 说
path 是「Where the target lived when the rule was saved… display metadata, not identity」
（`proto/v1/v1/data_quality_service.proto:890-892`），MCP 文档没说是快照。

**修复**：两处各加一句：「`target.path` 是规则保存时的路径快照，仅用于展示；目标已无存活检查或
批量补全读取失败时省略，省略不代表目标不存在——用 `guid`/`metaType` 定位」。

### N2 — §7 #39 末句与投影设计自相矛盾

**证据**：`.agents/docs/data-quality.md:768` 末句「输出省略未设字段、列表回投影，消费方不要用
"字段是否存在"判断语义」。后半句与本特性的设计相反：该 surface 正是用字段缺席表达「未设」——
`worstStatus` 缺席=还没有判定、`qualityScore`/`recentPassRate` 缺席=没有可判定的检查、
`baselineValue` 缺席=无基线、`outcome` 缺席=PENDING/RUNNING/ABANDONED。

**修复**：改为「字段缺席表示该字段没有值（不要读成 false/0/通过）；计数与布尔字段总会出现，
0/false 是合法值」；与 S1 修正后的 D-91 措辞保持一致。

### N3 — D-91「后者由执行列表与 get_metadata 回答」指代混乱

**证据**：`.agents/docs/data-quality.md:127`。`rule_history` 由 `list_quality_executions` 的
`rule` 过滤回答；`target_columns` 由 `get_metadata` 回答（表、视图、物化视图、外部表的列都在各自
`StoredMetadata` 里：`ViewMetadata.columns` 见 `proto/v1/v1/database_service.proto:1174`、
`ExternalTableMetadata.columns` 见 `:998`，TableMetadata 同理）。因此这两个被丢弃的字段确实可达，
不存在「agent 需要但不可达」的缺口；问题只在句子的指代。

**修复**：分别写明两个字段各自由谁回答，并记录「target_columns 对 view/external table 也由
`get_metadata` 返回」这一核对结果。

### N4 — 两个列表工具对不可监控的 `object` 静默返回空列表

**证据**：`backend/mcp/tool.go:961-967`（issues）与 `:1296-1302`（executions）都用
`resolveObject(ctx, args.Object, v1pb.MetaType_UNSPECIFIED)` 解析任意对象，随后只把 GUID 交给
`target_guid` 精确过滤；而规则只挂在四类可监控对象上（`qualitySignalMetaTypes`，
`backend/mcp/tool.go:748-755`）。因此传列/模式等引用会得到 `[]`，与「这个对象没有 issue/执行」
不可区分；同 surface 的 `get_quality_target` 对不可监控类型有明确答案
（`backend/mcp/tool.go:1129-1133`，`monitored: false`）。

**修复**：解析后若 `!qualitySignalMetaTypes[object.MetaType]`，返回 `invalid_argument` + hint
（「问题/执行属于检查目标（表/视图）；用它的父对象或 `guid` 查询」）；或退一步在 schema/guide 中写明
`object` 必须是检查目标。

### N5 — 测试缺口

**证据/缺口**（都不影响当前门禁，但新增分支应有用例钉住）：
1. `has_open_issue: false` 的「不过滤」语义无用例（`backend/mcp/tool_test.go:1261` 只测了 true）；
2. 两个列表工具的 `object` 走 name-path（非 GUID）解析的分支无用例（`:908`、`:1143` 都用 bare guid）；
3. `get_quality_target` 的 orphaned（有规则、`ResolveTarget` 失败 → 200 + `orphanedReason`）与
   「不可解析且无规则 → handler NotFound」两条路径无用例（`:1015`/`:1121` 只覆盖正常与空 summary）；
4. 枚举错误只覆盖 `status`/`state`/`worst_status`（`:1336`），`severity`/`outcome`/`trigger` 未覆盖；
5. S1、S2 的修复需要各自的回归用例。

**修复**：用现有 `fakeReaders` 每个补一个表驱动用例即可（reader 方法已齐全）。

### N6 — `.agents/docs/mcp-server.md` 的行号指针过时

**证据**：`:89` 写「表在 `toolDefinitions`，`backend/mcp/tool.go:36`」，实际在 `tool.go:38`
（base 时是 37，文档本来就差 1，本 diff 的 import 又 +1）；`:109` 写 `tool.go:40`（实际
`Description` 起于 `tool.go:42`）与 `tool_guard_test.go:49`（实际 53）。`:76` 的 `tool.go:767`
在 base 时已指向 971（与本 diff 无关，属既有漂移）。

**修复**：更新本 diff 触及的 89/109 两处（38/42/53）；对 `:76` 这类既有的明显过时指针，建议改成
函数名（`filterGraphByColumn`）而不是行号，避免下次再漂。

### N7 — enrichment 的权限含义未记录（行为接受）

**证据**：`list_quality_issues` 声明 `issues.list`（`backend/mcp/tool.go:154`），但补全读的是
`rules.list` 注解的 `ListQualityTargets`（`backend/mcp/tool.go:1016-1044`）。`dispatch` 只校验声明的
权限（`backend/mcp/server.go:356-377`），handler 是进程内直调、不再走 ACL 拦截器，所以一个只有
`issues.list` 的自定义角色也能拿到 rules.list 读模型里的 target path。这与既有
`get_quality_signal` 以及所有 object_addressed 工具的 `resolveObject`（读 instances.list/
databases.list/databases.read）是同一模式；`workspaceMember` 基线包含全部读权限
（`backend/store/predefined_roles.go:42-80`），单租户下影响很小——**行为本身按既有惯例接受**。

**修复（仅文档）**：在 D-91 的决策列加一句：「补全调用复用 rules.list 读模型的 target path，属
单租户只读展示数据，与既有 object 解析一致；若将来收紧自定义角色，在 Run 内加一次
`CheckPermission` 或不补全」。

## 刻意接受与已核实无问题

- **`get_quality_execution` 的嵌套 proto 命名**（`target.metaGuid`、`executedSql`、
  `samples[].columns[].value`、`ruleRevision` 而非投影的 `target.guid`）已在
  `backend/mcp/guide.md:144-149` 明确记录，接受。
- **列表不含失败样本与 executed SQL**：`list_quality_executions` 仍能靠 `name` + `rule` + `state`/
  `outcome` + `startTime` + `run`/`attempt` 找到证据执行，接受；`errorMessage` 可能夹带 SQL 片段，
  但两个工具同属 member 基线，且这是 API 既有字段，非本 diff 引入。
- **`rule_history` 与 `target_columns` 被刻意丢弃**：分别由 `list_quality_executions` 的 `rule`
  过滤和 `get_metadata` 回答，没有 agent 需要但不可达的内容（见 N3 的核对）。
- **enrichment 失败静默**：补全是展示性装饰，D-91 已明确「补全失败则省略、不让列读失败」，
  接受；只要求补 N1 的文档说明。
- **≤200 key 界确实成立**：工具把 `page_size` 夹到 `maxPageSize = 200`（`backend/mcp/tool.go:26-29`、
  `normalizePageSize` `:1843-1854`），API 的 `paginate` 截断到 `offset.limit`（`backend/api/v1/common.go:172-181`），页内 issue 数 ≤200，去重后的 target key 数 ≤ issue 数；精确选择上限正好 200
  （`backend/api/v1/data_quality_target.go:35`、`:121-122`）。非 watchable 类型已在
  `qualityIssueTargetKeys` 里剔除（`backend/mcp/tool.go:1053`），不会触发选择校验失败。该耦合
  已在代码注释里写明，接受。
- **`has_open_issue=false` = 不过滤**：schema 描述已写明（`backend/mcp/tool.go:133`），§7 #39 也写了，
  接受（guide 未重复，schema 本身就是模型看到的契约）。
- **bare GUID 不带 `meta_type` 的 list 过滤是自洽的**：`ListQualityIssues`/`ListQualityExecutions`
  的 `target_guid` 本来就是精确 GUID 过滤，工具没有额外造语义；`get_quality_target` 对 bare GUID
  强制 `meta_type`，与 `get_ddl`/`get_metadata`/`get_quality_signal` 的既有先例一致，且所有工具输出
  都带 `metaType`，接受。
- **`get_quality_target` 对不可监控类型返回与 `get_quality_signal` 同形的未监控答案**（无
  `rules`/`summary` 字段），与既有先例一致，接受。
- **`get_quality_target` 的 target 身份重复**（顶层 `target{guid,metaType,name}` + `summary.target`
  的 proto 形状）：`summary` 是原始 protoJSON 投影，顶层是便捷身份；测试已钉住两者，接受（若要减少
  困惑，可在 guide 加一句，不构成发现）。
- **空/空白参数**：空 `execution` → `invalid_argument`（`backend/mcp/tool.go:1382-1384`）；
  `page_size<0` → `invalid_argument`；空白 `rule` → 不过滤；空白 `meta_type` → 视为未指定；
  `object.name` 只有空白会落到 ref.go 既有的「instance/database required」错误（pre-existing，
  非本 diff 引入）。接受。
- **错误映射**：新工具都经 `toolErrorFromRPC`；handler 的 NotFound（不可解析且无规则）、
  InvalidArgument（坏的执行名/规则名/页 token）不会被误标成 internal。接受。
- **`/mcp` 60s 端点上限与每 principal 调用预算**：`server.go` 只改了 `QualityReader` 接口与
  instructions，限流/超时路径未动；enrichment 是同一请求内的第二次进程内读，受同一 60s。接受。
- **集成冒烟**：`list_quality_issues` 在空质量数据集上安全（无 issue → enrichment 零 key 不发第二个
  请求），断言 `IsError=false` 足以覆盖权限/接线，接受。
- **五个工具/无 LLM explain**：与 D-91、`docs/mcp.md`、`guide.md`、`mcp-server.md`、`docs/data-quality.md`
  一致；gap analysis 的 P1-8 行仍是原始提案（4 个工具含 `explain_quality_execution`），D-91 是
  覆盖它的决策记录，建议 fix round 视需要在该行加一句「已按 D-91 收敛为 5 个只读工具、不含
  LLM explain」。
- **工具数与文档漂移**：`mcp-server.md` 的 6 处（5 处「10 个/10 个只读」、1 处「只读 9 工具」）与
  `connected-apps.md` D6 的「MCP 能力面保持只读 9 工具」均已更新；guide/`docs/mcp.md` 的工具表也已
  补齐；全仓 `grep` 未再发现「10 个工具/9 工具」的计数。

## 残留与已知边界

- 未跑 `make test-integration`（全包 + `-race`）与前端门禁（本 diff 不动前端）；未涉及 MSSQL/
  StarRocks/Doris。
- `get_quality_target` 的 `summary.openIssueCount` 在 API 侧被 `qualityTargetOpenIssueLimit = 200`
  截断（`.agents/docs/data-quality.md` 之外的既有 API 行为），不是本 diff 引入；MCP 原样透传，
  未进一步核对。
- 既有的 lint 告警 173 条（全部 `revive/multiline-if-init`）与 `mcp-server.md` 的旧行号指针属
  pre-existing，本次只要求修正 N6 中本 diff 触及的两处。

## 复验（post-fix verification）

**复验者与语境**：一位独立的 post-fix verifier，在全新上下文中只读执行；未参与 P1-8 的实现、
文档撰写或修复，不复用修复报告的任何结论。逐条把 review 的发现对回树上的代码/文档，并跑命令
证明行为确实改变。**唯一写入是本文件本节**（第一轮追加、本轮就地更新）；未改产品代码、
测试、文档或生成物，未提交。

**复验对象**：分支 `feat/dq-mcp-quality-tools`，base `main@35d0d13`。历史：
`008bf67` 实现 → `3708529` Lead 文档 → `cc8ed52` guide 澄清 → `ecb1e40` review 快照 →
`5446b41` 代码/测试/guide 修复 → `6632789` Lead 文档修复。第一轮复验固定在 **`6632789`**
（`66327890eaa386d7e4f45c6583dbca6ebff80d65`）；N8 修复后第二轮固定在 **`8aa0cb8`**
（`8aa0cb8b3c1acac460eb29ff34c60b2b450211c9`，新增 `61a57da` guide、`dbc6576`
docs/mcp.md/D-91/§7 #39/mcp-server.md、`8aa0cb8` gap analysis）；最后一轮（原残留 5 的回归测试）
固定在 **最终 HEAD `6cebc08`**（`6cebc0861823bde11e7591cecbb8607382b244c2`，仅新增
`backend/mcp/tool_test.go` +38 行的 `TestGetQualityExecutionPinsZeroValueOmission`）。
`git diff --name-only 8aa0cb8..HEAD` 只有 `backend/mcp/tool_test.go`，产品代码与文档未再变。
各轮开始时 `git status --short` 都只有本文件的未提交复验节（或为空），未提交任何东西。

**验证方法**：除逐行读代码/文档外，为证明修复改变的是行为而不只是文字或不会失败的测试，
把**当前测试文件**放进 `git archive 5446b41^`（pre-fix 快照）的 `/tmp` 干净副本里跑新回归
用例——四个 S1/S2/N4 用例全部 **FAIL**（输出见下表）；N8 则用 `git archive HEAD` 的副本加一个
一次性 scratch 测试做端到端探测（不进入分支）。N8 修复后，又在 `8aa0cb8` 的
`git archive HEAD` 副本上重跑同一探测（`ObservedValue=0` 的 PASS/FAIL、ERROR/SKIPPED/
NOT_EVALUATED、PENDING 各一条，加列表投影与 all-paused summary 对照），确认文档的新读法
正是调用方实际见到的形状（结果见命令表）。最后对 `6cebc08` 新增的回归测试做变异验证：在
`/tmp` 副本把 `protoJSON` 改成 `protojson.MarshalOptions{EmitUnpopulated: true}` 后复跑该测试，
以证明它在 marshal 行为变化时会真的失败，而不是空转。

### 发现逐条判定

| # | 判定 | 证据（file:line / 命令） |
| --- | --- | --- |
| **S1** | **CLOSED** | `observedValue` 字段改为 `*float64 \`json:"observedValue,omitempty"\``（`backend/mcp/tool.go:1301-1304`），只在 `qualityStatusJudged(outcome)` 为真时赋值（`backend/mcp/tool.go:1218-1231`、`:1421-1424`）；该判定与 runner 真正留空的规则逐值一致：`backend/runner/qualityrunner/execution_result.go:172`、`:205-220`（PASS/WARN/FAIL → `&observed`；ERROR/SKIPPED/NOT_EVALUATED/UNSPECIFIED → nil），并有既有单测 `backend/runner/qualityrunner/qualityrunner_test.go:598-603`；API 把 nil 压成非 optional 的 0（`backend/api/v1/data_quality_convert.go:413`、`:526-533`）。回归用例 `backend/mcp/tool_test.go:1380-1421`（注释起于 `:1377`）：ERROR（且带 `observedKind=COUNT`，即「NULL 观测仍保留 kind」）与 NOT_EVALUATED 行断言无 `observedValue`，PASS+0 行断言存在且为 0。pre-fix 副本跑同一测试 **FAIL**，输出 `map[... "observedValue":0 ... "outcome":"QUALITY_STATUS_ERROR"]`——修复前确实报 0。 |
| **S2** | **CLOSED** | `qualityTargetAllPaused = ruleCount>0 && enabledRuleCount==0`（`backend/mcp/tool.go:1211-1213`），用于 `get_quality_target`（`:1163`、`:1195`）与目录行（`:1558`、`:1566`）；暂停时在 `proto.Merge` 出来的**副本**上清 `worstStatus` 再序列化（`:1167-1175`），目录行暂停时不赋 `WorstStatus`（`:1582-1586`）。`monitored` 仍是 `ruleCount>0`（`:1193`），即「至少有一条非归档检查」；有启用检查时仍输出 verdict（`tool_test.go:1034-1110` 断言 `allPaused:false` + `summary.worstStatus=FAIL`；目录行 `:1332-1350` 同理）。all-paused 用例 `tool_test.go:1425-1462`、`:1466-1499`；pre-fix 跑这两个用例 **FAIL**（`allPaused` 为 nil）。副本安全性单独核对：`proto.Merge` 到空消息是深拷贝，改 clone 的 `worstStatus`/嵌套 `target` 不改变源消息（指针不同、源值不变）。 |
| **N1** | **CLOSED** | `backend/mcp/guide.md:143-149` 与 `docs/mcp.md:139-142` 均写明 `target.path` 是规则保存时的路径**快照**、仅作展示，目标已无存活检查或补全读失败时省略，用 `target.guid`+`target.metaType` 定位；与 proto 注释一致（`proto/v1/v1/data_quality_service.proto:890-892`）。代码：`qualityTargetRef.Path` 为 `omitempty`（`backend/mcp/tool.go:824-828`），补全失败直接返回空 map（`:1032-1060`），path 为空即不出现。 |
| **N2** | **CLOSED** | `.agents/docs/data-quality.md:768`（§7 #39）已删去「不要用字段是否存在判断语义」，并在 `dbc6576` 后把规则写到与代码一致：「字段缺席表示该字段没有值（不要读成 false/0/通过）；投影行里的计数与布尔总会出现（0/false 是合法值），整条 proto 的 protoJSON 则按 proto3 规则省略零值（`get_quality_execution.observedValue` 为 0 时缺席、需与 outcome 一起读，`get_quality_target.summary` 的零计数同样缺席）」；D-91（`:127`）同义。投影行与 raw protoJSON 两种形状分别验证过（后者见 N8 探测）。 |
| **N3** | **CLOSED** | `.agents/docs/data-quality.md:127`（D-91）逐个点名：`rule_history` 由 `list_quality_executions` 的 `rule` 过滤回答；`target_columns` 由 `get_metadata` 回答，且视图/外部表的列也在各自 `StoredMetadata` 里。核对：`rule` 过滤在 proto（`proto/v1/v1/data_quality_service.proto:1912-1914`）与工具透传（`backend/mcp/tool.go:1352-1356` 一带）存在；`get_metadata` 返回整个 `StoredMetadata`（`backend/mcp/tool.go:556-579`），View/ExternalTable 的 columns 分别在 `proto/v1/v1/database_service.proto:1174`、`:999` 附近。 |
| **N4** | **CLOSED** | 两个列表工具在解析后调用 `requireQualityListTarget`：`backend/mcp/tool.go:899-910`（拒绝非四类可监控类型，`invalid_argument`+可执行 hint），调用点 `:979-981`（issues）、`:1361-1363`（executions）。裸 `guid` 不触发解析、`resolveObject` 对 guid 引用原样返回（`backend/mcp/ref.go:50-53`），故 `MetaType==UNSPECIFIED` 放行，与文档一致（`backend/mcp/guide.md:171-174`、`docs/mcp.md:142-145`）。用例 `tool_test.go:1504-1550`（注释起于 `:1501`）：schema/column（本用 column）按名字解析被拒、两个工具都覆盖、裸 guid 仍透传；pre-fix 副本 **FAIL**（失败信息 `ListQualityIssues/ListQualityExecutions was called`，即修复前确实返回空页）。 |
| **N5** | **CLOSED** | 五处缺口逐一有可失败的用例：① `has_open_issue=false` 不过滤 → `tool_test.go:1554-1570`；② 两个列表工具的 name-path 解析 → `:1574-1619`；③ orphaned 200+`orphanedReason` → `:1623-1650`，NotFound 映射 → `:1654-1668`；④ `severity`/`outcome`/`trigger` 枚举错误 → `:1351-1375` 表驱动（断言错误消息列出全部合法值）；⑤ S1/S2 回归 → `:1380`、`:1425`、`:1466`。定向运行全 **PASS**（见命令表），且 S1/S2/N4 用例在 pre-fix 树上确认 **FAIL**（非空转）。 |
| **N6** | **CLOSED（按 N6 所列范围）** | `.agents/docs/mcp-server.md:76` 的 `filterGraphByColumn`、`:89` 的 `toolDefinitions`、`:109` 的 `Description` 字段与 `TestEveryToolMatchesItsRPC` 均为符号名；符号确实存在：`backend/mcp/tool.go:38`（`toolDefinitions`）、`:1831`（`filterGraphByColumn`）、`backend/mcp/tool_guard_test.go:42`（`TestEveryToolMatchesItsRPC`）。N6 范围外的同文档 `:42`、`:49`、`:201` 行号指针随后已由 `dbc6576` 换成行为描述/符号名（`maxExpandedTargets`，存在于 `backend/mcp/tool.go:1735`），本轮 grep 已无 `tool.go:NNN`；该残留关闭。 |
| **N7** | **CLOSED** | `.agents/docs/data-quality.md:127`（D-91）已记录：补全读的是 `rules.list` 的只读目标聚合，与既有 `get_quality_signal`/对象解析同一模式，并写明「若将来收紧自定义角色可在 Run 内补一次 `CheckPermission` 或不补全」。行为不变：`dispatch` 只校验工具声明的权限（`backend/mcp/server.go:366-377`），enrichment 仍是同请求内的进程内读（`backend/mcp/tool.go:1032-1060`）。 |
| **N8（复验新报）** | **CLOSED（原 OPEN，已修复）** | 修复前写进文档的 `get_quality_execution` 说法与代码不符：`guide.md:159-161`、`docs/mcp.md:132-134`、D-91（`data-quality.md:127`）都称「整条 proto 的非 optional `observedValue` 在无观测时为 0，需与 outcome 一起读」。实际该工具整个返回 `protoJSON(response.Msg)`（`backend/mcp/tool.go:1462`），而 `protoJSON` 用默认 `protojson.MarshalOptions{}`（`:254-259`，未开 `EmitUnpopulated`），proto3 非 optional 的 0 会被**整个省略**：`observed_value` 在 `proto/v1/v1/data_quality_service.proto:1305` 是 `double`（非 optional）。端到端探测（HEAD 副本 + scratch 测试）：PASS 且 `ObservedValue=0` 的 `get_quality_execution` 返回 `map[attempt:1 name:... outcome:QUALITY_STATUS_PASS]`——既没有「无观测时的 0」，真实观测 0 也看不见。同一段「计数与布尔字段总会出现」对两个 raw protoJSON 输出也不成立：`get_quality_target.summary` 在 `enabledRuleCount=0` 时字段缺席（修复轮自己的测试 `tool_test.go:1460-1461` 就这么断言），`get_quality_execution` 的 `failedRows:0`/`sampled:false`/`truncated:false` 同样缺席（同一次探测）。`guide.md` 是 `go:embed` 的模型可见 prompt（`backend/mcp/guide.go:17-19`），这属于模型会照着执行的错误规则；代码行为是 pre-existing，但错误文档是本轮新引入。当时建议二选一（修复采纳前者）：改正措辞（「0 值字段被省略，judged 0 同样看不到，依据 outcome 判断是否观测」）并把「计数/布尔总会出现」限定为投影行；或给 `get_quality_execution` 加显式观测值/判定字段（会改变「整条 proto」形状，需另行评审）。**修复与复验（最终 HEAD `8aa0cb8`）**：`61a57da` 改 guide、`dbc6576` 改 docs/mcp.md/D-91/§7 #39/mcp-server.md；现文（`backend/mcp/guide.md:158-169`、 `docs/mcp.md:126-138`、`data-quality.md:127`、`:768`）明确「整条 proto 的 protoJSON 省略零值： 缺席 `observedValue` 在 PASS/WARN/FAIL 时按 0 读、在 ERROR/SKIPPED/NOT_EVALUATED 时表示无观测， PENDING/RUNNING 还没有 outcome；`failedRows`/`sampled` 等零值同样省略」，并把「计数/布尔总会出现」 限定到投影行。grep `non-optional|无观测时为 0|计数与布尔字段总会出现` 在本轮四份文档中已无命中。 HEAD 端到端探测（副本 + scratch 测试，固定 `ObservedValue=0`）：PASS/FAIL、ERROR/SKIPPED/ NOT_EVALUATED 的 `get_quality_execution` 输出都无 `observedValue`，`failedRows`/`sampled`/ `truncated` 也缺席；PENDING 行连 `outcome` 也没有；列表投影对照 PASS+0 行有 `observedValue:0`、 ERROR 行没有；all-paused summary 无 `enabledRuleCount`/`worstStatus`。与文档读法逐条一致。 |

**计数：S1、S2、N1–N8 共 10 项全部 CLOSED**（N8 为复验新报，已由 `61a57da`/`dbc6576`
修复、在 `8aa0cb8` 复验；最终 HEAD `6cebc08` 只是补上其回归测试，行为与文档均未再变）。

### 已验证的命令与结果

| 命令 / 核对 | 结果 |
| --- | --- |
| `gofmt -l backend/` | 空，exit 0（`6632789`、`8aa0cb8`、`6cebc08` 均如此） |
| `golangci-lint run --allow-parallel-runners` | **exit 1**：默认 `max-same-issues: 3` 截断后只显示 3 条 `revive/multiline-if-init`（`backend/migrator/migrator.go:228/277/399`），均不在 diff 中 |
| `golangci-lint run --allow-parallel-runners --max-same-issues=0 --max-issues-per-linter=0` | **exit 1，173 条**，全部同一条 `revive/multiline-if-init`；与 `git diff --name-only 35d0d13..HEAD` 求交集为**空** → 本 diff 新增 0 条。本地 v2.14.0，CI pin v2.13.1 |
| `go test -count=1 ./backend/mcp/...` | **PASS**：首次 `6632789` 为 0.090s；`8aa0cb8` 复跑为 `ok ... 0.099s`（guide 是 `go:embed`，复跑覆盖新文本）；最终 `6cebc08` 复跑为 `ok ... 0.078s`（含新增回归测试与全部新用例、守卫/契约测试） |
| 定向：`go test -count=1 -run 'TestListQualityExecutionsOmitsAbsentObservations\|TestGetQualityTargetReportsAnAllPausedTarget\|TestListQualityTargetsMarksAnAllPausedRow\|TestQualityListingRefusesAnUnwatchableObject\|TestQualityFiltersListAcceptedEnumValues\|TestListQualityTargetsTreatsFalseHasOpenIssueAsNoFilter\|TestQualityListingPassesAResolvedNamePathTargetGUID\|TestGetQualityTargetCarriesAnOrphanedReason\|TestGetQualityTargetMapsANotFound\|TestEveryToolMatchesItsRPC\|TestToolSchemasMatchTheirArgumentStructs' ./backend/mcp/` | 全部 **PASS** |
| `go test -count=1 ./...` | **PASS**，exit 0（67 个包 `ok`，无 `FAIL`）；`8aa0cb8` 复跑同结果 |
| `make build` | exit 0（`./build/metaxisdata`，`version.GitCommit=8aa0cb8`；产物被 gitignore） |
| `go test -count=1 -tags=integration -run '^TestMCPAuthorizationAndToolsRealServerIntegration$' ./backend/test/integration/runner` | **PASS**：首次 `6632789` 为 22.917s；本次 `8aa0cb8` 复跑为 `ok ... 24.332s`；真实 Docker（PostgreSQL testcontainer），用例内无 `t.Skip`，断言 15 个工具（`mcp_service_test.go:199`） |
| **原残留 5 回归测试（HEAD `6cebc08`）**：`go test -count=1 -run '^TestGetQualityExecutionPinsZeroValueOmission$' ./backend/mcp/`；再在 `/tmp` 副本把 `protoJSON` 改为 `EmitUnpopulated: true` 复跑 | HEAD 上 **PASS**（0.027s）；变异后 **FAIL**，payload 出现 `observedValue:0` 等零值字段 → 该测试确实钉住默认 marshal 的零值省略；测试内还断言 `name` 与 `attempt:1` 仍存在，证明省略断言不是空转 |
| **pre-fix 回归探测**：`git archive 5446b41^\|tar -x -C /tmp/prefix-5446`，放入当前 `tool_test.go`，跑 S1/S2/N4 四个新用例 | 四个全 **FAIL**：S1 输出带 `"observedValue":0`（outcome=ERROR）；S2 `allPaused` 为 nil；N4 修复前 `ListQualityIssues/ListQualityExecutions was called`（返回空页） |
| **N8 探测（修复前，`6632789`）**：`git archive` 到 `/tmp/head-copy`，加一次性 scratch 测试调用 `get_quality_execution`（PASS + `ObservedValue=0`） | 返回 `map[attempt:1 name:... outcome:QUALITY_STATUS_PASS]`，**无** `observedValue`；`failedRows`/`sampled`/`truncated` 的 0 值字段同样缺席 |
| **N8 复验探测（HEAD `8aa0cb8`）**：`git archive HEAD` 到 `/tmp/head-copy-n8`，同一 scratch 测试覆盖 `ObservedValue=0` 的 PASS、FAIL、ERROR、SKIPPED、NOT_EVALUATED、PENDING，加列表投影与 all-paused summary | **与文档新读法一致**：PASS/FAIL、ERROR/SKIPPED/NOT_EVALUATED 的 `get_quality_execution` 都无 `observedValue`（靠 `outcome` 区分“测到 0”与“无观测”），PENDING 无 `outcome`；`failedRows`/`sampled`/`truncated` 缺席；列表投影 PASS+0 有 `observedValue:0`、ERROR 无；all-paused summary 无 `enabledRuleCount`/`worstStatus` |
| `git diff --stat 35d0d13..HEAD` | 14 文件、+2169/−37（HEAD `6cebc08` 快照，不含本节未提交编辑）；`git diff --name-only 8aa0cb8..HEAD` 只有 `backend/mcp/tool_test.go`（测试-only，产品代码/文档未变），`6632789..8aa0cb8` 为 5 个文档/记录文件 → 上一轮 lint 结论继续成立；`git diff --name-only 35d0d13..HEAD` 中**无** `proto/`、`backend/migrator/`、`frontend/`、`go.mod`/`go.sum` |
| `git status --short` | 本轮开始时仅 ` M .agents/reviews/dq-mcp-quality-tools-review.md`（上一轮未提交的复验节）；只编辑该节，最终仍仅此一项；未提交（HEAD `6cebc08`） |
| `git rev-parse HEAD` / 分支 | **`6cebc0861823bde11e7591cecbb8607382b244c2`** / `feat/dq-mcp-quality-tools`（前两轮为 `6632789`、`8aa0cb8`） |

### “刻意接受”项在修复后是否仍成立

- **`get_quality_execution` 的整条 proto caveat**：`61a57da`/`dbc6576` 已把「整条 proto 的 protoJSON 省略零值、缺席 `observedValue` 必须与 `outcome` 合读」写进 guide/docs/D-91/#39；最终 HEAD 探测与实现一致 → N8 CLOSED。
- **工具数 15**：`toolDefinitions` 15 条、`wantTools` 15 条（`tool_guard_test.go:90-106`）、`tool_schema_test` 15 个 case、集成断言 15；`mcp-server.md:9/30/89/207` 均为 15。全仓未再发现 MCP 文档里「10 个/9 个/14 个」的旧计数。
- **`wantTools`/`readPermissions`/schema 对账**：`readPermissions` 8 条（`tool_guard_test.go:19-28`）且每条都有工具使用；`TestEveryToolMatchesItsRPC`、`TestToolSchemasMatchTheirArgumentStructs` 均 PASS。修复轮未增删工具，契约未漂移。
- **enrichment 静默失败、≤200 key 上限、`has_open_issue=false`=不过滤、bare guid 列表语义、不可监控类型的未监控形状、错误映射**：相应代码路径本轮未改，复核仍成立；N4 新增的 `invalid_argument` 在调用 RPC 前返回，既有 `toolErrorFromRPC` 路径未被改写。
- **五个工具、无 LLM explain**：未变，D-91/docs/guide/mcp-server 一致。
- **新增字段没有「永远出现但无意义」**：`allPaused` 两种输出里都有（投影行/工作台顶层），且 false 有真实含义（并非全部暂停）。

### 残留风险与未决项

1. **原残留 5（`get_quality_execution` 零值省略的回归测试）已关闭**：`6cebc08` 新增
   `TestGetQualityExecutionPinsZeroValueOmission`（`backend/mcp/tool_test.go:1279-1311`）。它构造
   COMPLETED/PASS 且 `ObservedValue=0`、`FailedRows=0`、`TotalRows=0`、`Sampled=false`、
   `SampleRows=0`、`Truncated=false` 的整条执行，断言输出不含 `observedValue`/`failedRows`/
   `sampled`/`truncated`，同时 `name` 与 `attempt: 1` 仍在（防止空转）。变异验证：把 `protoJSON` 换成
   `EmitUnpopulated: true` 后该测试 **FAIL**（payload 出现 `observedValue:0` 等），证明它确实钉住
   默认 marshal 的文档契约；在 HEAD 上 PASS。
2. **原残留 2（`mcp-server.md` 行号指针）已关闭**：`:42`、`:49`、`:201` 的 `tool.go:217`/`tool.go:688` 已由 `dbc6576` 换成「原样透传 next-page token」与符号 `maxExpandedTargets`（`backend/mcp/tool.go:1735`），本轮 grep 该文档已无 `tool.go:[0-9]`/`tool_guard_test.go:[0-9]`。文档中仍有指 `server.go`/OAuth 文件的既有行号（如 `server.go:276-279`），不属 N8 范围，仍有漂移风险。
3. **原残留 3（S2 × `worst_status` 交互）已关闭**：`backend/mcp/guide.md:129-133`、`docs/mcp.md:130-132`、D-91（`data-quality.md:127`）、§7 #39（`:768`）都写明过滤按含暂停历史的聚合、可能返回 `allPaused=true` 且无 `worstStatus` 的行。
4. **原残留 4（gap analysis）已关闭**：`8aa0cb8` 在 `.agents/reviews/data-quality-competitive-gap-analysis.md:325` 加了日期化的 P1-8 实施记录，指向 D-91 与本 review，写明 5 个只读工具、无 `explain_quality_execution`；原表行按原始提案保留。
5. **未跑**：全量 `make test-integration`（全包 + `-race`）、前端门禁（本 diff 不动前端）、MSSQL/StarRocks/Doris 场景与浏览器/客户端级验证；`make build` 只验证编译。
6. pre-fix/N8 探测都在 `/tmp` 的 `git archive` 副本中运行（`/tmp/prefix-5446`、`/tmp/head-copy`、`/tmp/head-copy-n8`），未在分支工作树留下改动；本轮唯一的树内写入仍是本节。

**交接结论（最终 HEAD `6cebc08`）**：S1、S2、N1–N8 共 10 项**全部 CLOSED**；N8 的文档修正在 `8aa0cb8` 复验，原残留 2/3/4 关闭，原残留 5 由 `6cebc08` 的回归测试关闭并通过变异验证。门禁（gofmt/单测/全量测试/build/Docker 集成）在 `8aa0cb8` 复跑全绿，`6cebc08` 仅新增测试、`go test ./backend/mcp/...` 复跑仍 PASS；`8aa0cb8..HEAD` 只有 `backend/mcp/tool_test.go`，产品代码/文档未变，lint 结论（173 条 pre-existing、本 diff 新增 0）继续成立；分支 diff 无 proto/迁移/前端/依赖。**无未决发现，可交接。**
