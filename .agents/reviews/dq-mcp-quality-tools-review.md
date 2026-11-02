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
