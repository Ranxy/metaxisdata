# AI Agent 平台化重构 — Agent Core × Tool Catalog 设计稿

> Status: **proposed**（设计稿，尚未实现）。实现落地后，结论应并入 [data-quality.md](data-quality.md)（§3.9h/§3.9i 与 §8）与 [mcp-server.md](mcp-server.md)，本文收缩为一行维护参考；实现细节、阶段记录留在 PR 里，不进此目录。
> 关联：[data-quality.md](data-quality.md)（DQ agent 会话/纳管、动作目录、不变量 I-81…I-84）、[mcp-server.md](mcp-server.md)（17 个只读工具与线上契约）、[iam-and-permissions.md](iam-and-permissions.md)（权限注解）、[rate-limiting.md](rate-limiting.md)（principal 预算）、[metrics.md](metrics.md)（指标与标签基数）、[runner.md](runner.md)（后台执行器单副本假设）。
> 参考实现：DeepSeek Harness（本机 checkout `/home/ran/project/deepseek-harness`，下称 DSH）的 `packages/core/{agent,agent-loop,tools,system-prompt,session}`、`packages/mcp/mcp-client`、`packages/preset/*`、`packages/compaction/*`。

## 0. 结论：这个想法可行吗？

**可行，而且方向基本正确；但需要把「两分」修正为「三分 + 一个运行时」。**

- 你的直觉是对的：agent 的**机制**（循环、流式、上下文、事件、压缩、会话续写）与项目**能力**（元数据、血缘、质量）应该分开，能力以工具形式注册给 agent。这是 DSH 与主流 agent 框架的共同结论，也能直接解决我们现在的耦合。
- 但本项目与 DSH 有一个关键差异：**工具面已经存在且成熟**。`backend/mcp/` 已经有 17 个只读工具，带着对象寻址、结果投影、分页、权限守卫、错误词表、逐调用审计和一套线上契约（`backend/mcp/tool.go` 2113 行，`tool_guard_test.go` / `tool_schema_test.go` 把它钉死）。所以这次重构不是"从零抽象工具"，而是**把一个已证明可用的只读工具面升级为全能力工具运行时**。这大幅降低风险，也决定了落地方式：先复用，再泛化，最后才拆包。
- 正确的分层不是「agent core + 工具」，而是三层：
  1. **Agent runtime（机制）**：`RunAgentLoop`、事件、消息、prompt 组装、压缩、转录端口、工具**执行管线**。不含任何元数据/血缘/质量知识。
  2. **Tool catalog（能力）**：metadata / lineage / quality 三组工具，每个工具 = 定义 + 执行 + 元数据（只读/副作用/权限/动作/超时/结果预算/证据类型）。它**不属于** agent，而是被 agent 与 MCP **两个前端**消费。
  3. **Agent product / preset（策略与人格）**：DQ 的 monitor / triage 两套 spec、ExplainSQL spec、确定性回退、wake 控制、会话策略。一个产品 = 一段 prompt + 一份工具白名单 + 模型配置 + 预算。
- 核心动作只有 5 个：**工具契约与注册表**、**统一执行管线**、**agent core 去领域化**、**DQ agent 降为产品 preset**、**工具实现统一走 service handler 接口**。
- 明确**不建议**做的：照搬 DSH 的 Cordis 插件/DI/HMR 与 ~60 个包；引入运行期动态工具注册；为工具建独立 Go module；对 MCP 打开写工具。
- 最大的真实风险不是技术，而是**权限与副作用边界被"统一注册表"稀释**：一个 registry 同时服务只读 MCP、只读 agent 和可提议写 agent，一旦元数据没写清、门禁没统一，agent 就会拿到它不该有的能力。设计里用「工具元数据 + 两道门（IAM 权限、动作策略）+ 守卫测试」来兜底，并坚持分阶段、每阶段行为不变。

一句话概括目标：**让"新增一个 agent 能力"从改 4 个文件变成注册 1 个工具；让"新增一个 agent 产品"从复制一套引擎变成声明一个 preset；让 MCP 与内置 agent 共享同一份工具实现，而不是各写一遍。**

### 0.1 速览：可行么 / 优化点 / 问题

**可行么？** 可行，但有三个前提：① 以已经验证的 MCP 工具面为底座，而不是另起炉灶发明第二套工具抽象；② 单 module + 启动期静态装配，不引入运行期插件运行时；③ 分阶段迁移、每阶段行为不变（否则会与 DQ 的 100+ 不变量和 MCP 的线上契约正面冲突）。

**比你最初的两分更好的优化点（详见 §3–§5）：**

1. **三分层**：工具是独立能力层，同时服务 MCP 与内置 agent；产品层只声明 prompt + 白名单 + 预算。两分会把工具重新塞进 agent，重演 `llm/tools.go` 的领域泄漏。
2. **复用 MCP 的寻址/投影/错误词表/审计**：名字寻址（`mcp/ref.go`）、紧凑投影、结构化错误、逐调用审计已经是成熟资产，直接升级而不是重写。
3. **工具元数据驱动授权**：`ReadOnly/SideEffect/Permission/Action` 写进工具本身，由一条管线统一执行；权限判断不再散落在工具实现里。
4. **白名单 preset 而不是全量暴露**：工具 schema 进 prompt 的成本真实存在；`quality-monitor`/`quality-triage` 各自只带 8/10 个工具。
5. **结果预算 + 证据引用**：`MaxResultBytes` 统一截断；工具返回 `EvidenceRef`，`record_investigation` 从构造上带证据，而不是让模型自由编造引用。
6. **transcript 端口而不是先迁表**：先复用现有 `quality_agent_message`，等第二个产品真正需要持久长会话时再泛化表。
7. **控制类工具返回指令而不是回写指针**：block/schedule 变成可测试、可审计、可并发的普通返回值。
8. **工具级指标与审计**：当前只能回答"哪个 feature 慢"，补到"哪个工具慢/被拒/超预算"。

**有哪些问题（不是否决项，是要在设计里解掉的）：**

1. **过度抽象**：为两个消费者造出多层接口是真实风险 → 固定四类包、接口只在 seam 处、P0 用最小接口（R1）。
2. **权限被稀释**：统一 registry 可能让 agent 拿到未授权工具 → 两道门 + 启动校验 + MCP `ReadOnly()` 过滤 + 守卫测试（R2/D7）。
3. **行为漂移**：转录行、Outcome、MCP 输出被测试和前端锁定 → golden + 旧/新双跑（R3）。
4. **循环依赖**：catalog 与 `api/v1`、core 与 `storepb` 容易互相拉扯 → 工具包只声明接口、core 定义自己的 `Message` 端口、server 唯一装配（R4/D4/D8）。
5. **MCP wire 契约冻结**：工具名/schema/错误码/单副本输出不能为了统一而改 → 只换内部实现，parity 测试兜底（R6/D5）。
6. **token 成本**：全量工具会膨胀 prompt → 白名单 + 描述精简 + 指标（R5）。
7. **相邻技术债**：转录有 3 个写入方、准入逻辑在 3 处重复（G6b/G11）——本次不解决，但端口设计要能容纳未来收拢。
8. **调度与部署语义**：单副本、lease/claim、runner 生命周期不在本次范围内（§2.2 非目标）；顺手改它就是 R12 的范围蔓延。

---

## 1. 现状

### 1.1 一次 DQ agent wake 的真实调用链

```
qualitysessionrunner.Run
  └─ claim 到期 session（lease 15m / wake 10m）
     └─ agentrunner.Engine.Wake
        ├─ ensureSystemMessage（systemPrompt() 硬编码）
        ├─ repairInterruptedToolCalls（悬空 tool call 补合成结果）
        ├─ pendingMessages（ProcessedSeq 之后的事件/提问）
        ├─ append USER(wake)（wakePrompt() 硬编码工具名）
        ├─ modelWake
        │   ├─ loadTranscript → maybeCompact → BuildConversation → pruneToolResults
        │   ├─ llm.RunAgentLoop(AgentConfig{History, Tools: modelToolDefs(mode), Executor})
        │   │   ├─ streamOneTurn（OpenAI 兼容 SSE）
        │   │   ├─ toolExecutor → executeTool（10 个 case 的 switch）
        │   │   │   ├─ 直接 e.store.ListQualityRules / ListQualityExecutions / …
        │   │   │   ├─ recordInvestigation / recordAssetFact / recordDecision（写 store）
        │   │   │   └─ 通过 5 个裸指针回写 blocked / nextWake / scheduleReason
        │   │   └─ 事件落库（assistant / tool / engine 消息）
        │   └─ 失败 → retryWake / 确定性回退
        └─ Outcome{Blocked, NextWakeAt, TokensUsed, …} 交给 runner 落库与排期
```

### 1.2 代码地图（本次重构涉及面）

| 位置 | 行数（约） | 现在的职责 | 主要问题 |
| --- | --- | --- | --- |
| `backend/component/llm/` | ~2 000（`agent.go` 854、`tools.go` 174、`client.go` 181、`registry.go` 132、`debug.go` 165…） | ① provider registry；② OpenAI 兼容流式客户端；③ **agent loop**（turn/tool 循环、事件、预算、指标）；④ JSON 解析、失败分类、debug 日志；⑤ **ExplainSQL 领域类型与工具** | 传输层、agent 运行时、某个功能领域混在一个包 |
| `backend/component/dataquality/agentrunner/` | ~2 200（`engine.go` 832、`tools.go` 389、`compaction.go` 302、`helpers.go` 213、`context.go` 164、`prompt.go` 160、`triage.go` 132） | ① DQ wake 引擎；② 转录重建/修复；③ 压缩；④ 工具定义+执行+策略；⑤ 系统/wake prompt；⑥ triage；⑦ 确定性回退 | 通用机制（②③）与产品策略（⑤⑦）、领域工具（④）三样都在这 |
| `backend/runner/qualitysessionrunner/` | 350 | lease/claim/调度/预算/事件注入 | 基本干净；依赖 `agentrunner.Outcome/Options` |
| `backend/runner/qualityenrollmentrunner/` | 782 | 纳管队列、优先级、准入 | 可保留；triage 准入用 model resolver 探针 |
| `backend/component/dataquality/suggester.go` | 859 | L1/L2/L3 三个无工具 LLM 能力的 prompt 编排、诊断、缓存、配额 | 直接调 `llm.RunAgentLoop(MaxTurns=1)`，可复用 core |
| `backend/api/v1/explain_sql_service.go` | 854 | ExplainSQL 流式 RPC + 2 个工具的实现 + SchemaContext 构建 | 工具实现在 handler 里，与 MCP metadata 能力重复 |
| `backend/api/v1/data_quality_agent*.go` | 1 375 | agent principal / session / enrollment RPC | 与 runner 共享 store；边界尚可 |
| `backend/mcp/` | `tool.go` 2 113、`server.go` 526、`ref.go` 406 | 17 个只读工具 + 寻址/权限/审计/错误/分页/引导 | 成熟但封闭在 MCP transport，内置 agent 完全用不到 |
| `backend/store/quality_agent*.go` | 2 189 | DQ agent 持久化 | 命名 DQ 专用；泛化需要端口而不是直接改表 |

### 1.3 现在其实有四个「工具面」

| 工具面 | 工具数 | 定义位置 | 执行位置 | 授权 | 结果形状 | 被谁消费 |
| --- | --- | --- | --- | --- | --- | --- |
| MCP | 17（只读） | `backend/mcp/tool.go` `toolDefinitions()` | 同名 `runXxx`，调 Connect handler | `Permission` + `Checker`（IAM） | `structuredContent`（紧凑 JSON 投影） | 外部 MCP 客户端 |
| ExplainSQL | 2 | `llm/tools.go` `ExplainSQLTools()` | `explain_sql_service.go` `executeExplainTool` | RPC 拦截器（用户） | 字符串 | ExplainSQL RPC |
| DQ agent | 10 | `agentrunner/prompt.go` `modelToolDefs()` | `agentrunner/tools.go` `executeTool` | agent principal + 动作策略 | `marshalJSON` 字符串 | 内置 wake 引擎 |
| 无工具（prompt-only） | 0 | `dataquality/suggester.go`、`data_quality_llm_analysis.go`、`data_quality_label_analysis.go` | 直接 `RunAgentLoop(MaxTurns=1)` | 配额 | JSON/markdown | L1/L2/L3 |

MCP 的 17 个工具包装了 16 个 RPC（`get_quality_signal` 与 `list_quality_targets` 共用 `ListQualityTargets`；`whoami` 包装无权限注解的 `GetCurrentUser`，在守卫测试里单独 allowlist）。名字 / RPC / 权限：

| 工具 | RPC | 权限 |
| --- | --- | --- |
| `list_instances` | `InstanceService.ListInstances` | `instances.list` |
| `list_databases` | `DatabaseService.ListDatabases` | `databases.list` |
| `search_metadata` | `DatabaseService.SearchMetadata` | `databases.read` |
| `list_metadata` | `DatabaseService.ListMetadata` | `databases.read` |
| `get_metadata` | `DatabaseService.GetMetadata` | `databases.read` |
| `get_ddl` | `DatabaseService.GetSchemaString` | `databases.read` |
| `get_quality_signal` | `DataQualityService.ListQualityTargets` | `dataQuality.rules.list` |
| `list_quality_targets` | `DataQualityService.ListQualityTargets` | `dataQuality.rules.list` |
| `get_quality_target` | `DataQualityService.GetQualityTarget` | `dataQuality.rules.list` |
| `list_quality_issues` | `DataQualityService.ListQualityIssues` | `dataQuality.issues.list` |
| `list_quality_executions` | `DataQualityService.ListQualityExecutions` | `dataQuality.executions.list` |
| `get_quality_execution` | `DataQualityService.GetQualityExecution` | `dataQuality.executions.get` |
| `get_quality_impact` | `DataQualityService.GetQualityTargetImpact` | `dataQuality.rules.list` |
| `get_quality_consumption_coverage` | `DataQualityService.GetQualityConsumptionCoverage` | `dataQuality.rules.list` |
| `analyze_sql` | `LineageService.AnalyzeSQL` | `lineage.get` |
| `get_lineage_graph` | `LineageService.GetLineageGraph` | `lineage.get` |
| `whoami` | `UserService.GetCurrentUser` | —（无权限注解） |

DQ agent 的 10 个工具（定义/执行/作用）：

| 工具 | 定义 | 执行 | 类别 | 依赖会话状态 |
| --- | --- | --- | --- | --- |
| `get_target_summary` | `prompt.go` | `tools.go` → `gatherEvidence` | 读 | target |
| `list_recent_executions` | `prompt.go` | 同上 | 读 | target |
| `get_memory` | `prompt.go` | 同上 | 读 | target |
| `record_investigation` | `prompt.go` | `tools.go` → `recordInvestigation` | 提议写 | target + principal |
| `record_asset_fact` | `prompt.go` | `recordAssetFact` | 提议写 | target + principal |
| `record_decision` | `prompt.go` | `recordDecision` | 提议写（受策略） | target + principal |
| `block_session` | `prompt.go` | 回写指针 | 控制 | 是 |
| `schedule_wake` | `prompt.go` | 回写指针 | 控制 | 是 |
| `get_table_context` | `prompt.go`（仅 TRIAGE） | `triage.go` | 读 | target + 画像/标注/血缘 |
| `draft_checks` | `prompt.go`（仅 TRIAGE） | `triage.go` → Suggester | 提议写 | target + principal |

### 1.4 问题清单（每条都有代码证据）

**G1 — 工具定义、执行、策略、prompt 文案分散在四处，新增一个工具要改 4 个地方。**
`modelToolDefs()`（`agentrunner/prompt.go:90`）定义 schema，`executeTool` 的 `switch name`（`agentrunner/tools.go:119`）执行，wake prompt 文本里再次硬编码工具名与语义（`prompt.go:26-32` 的自然语言规则、`:69` 的 triage wake 文案），而"哪些工具有效"由 `session.Mode` 判断（`prompt.go:136`、`:156`）。三者之间没有任何编译期连接：删掉一个 case 不会报错，改一个参数名不会同步 schema。

**G2 — 工具执行契约本身不完整。**
`llm.ToolExecutor` 是 `func(toolCall ToolCall) ([]ToolResult, error)`（`llm/client.go:46`）：**没有 context**（无法取消、无法设超时）、没有调用身份、没有执行元数据；循环只取 `results[0].Content`（`agent.go` 工具执行段），`[]ToolResult` 的复数形式是死抽象；工具失败被压成 `fmt.Sprintf("error: %s", err)`，没有结构化错误码，UI 与模型只能读字符串。

**G3 — 工具通过裸指针回写 wake 结果。**
`toolExecutor` 接收 `blocked *bool, blockedCode/blockedWhy *string, nextWake **time.Time, scheduleReason *string`（`agentrunner/tools.go:94`），由 `executeTool` 里的 `*blocked = true; *nextWake = ptrTime(...)` 直接改写（`tools.go:220-222`、`:243-244`）。工具不再是纯函数式的"输入→结果"，而是隐式共享可变状态：测试难写、并发不可能开、加一个控制类工具就要再加一个出参。

**G4 — 同一个领域能力有多份实现，已经出现漂移。**
MCP `search_metadata` + `get_metadata` + `get_ddl`（`backend/mcp/tool.go`）vs ExplainSQL `search_objects` + `get_object_schema`（`llm/tools.go` + `explain_sql_service.go:458`）；MCP `get_quality_target` vs DQ `get_target_summary`（`agentrunner/tools.go:130`）；MCP `list_quality_executions` vs DQ `list_recent_executions`；MCP `get_quality_signal` vs triage `get_table_context`（部分重叠）。命名、寻址方式（MCP 名字路径 vs 内部 GUID）、字段投影、错误形状全都不同；同一个问题"这张表健康吗"在两个前端答案形状不同。更具体地说：目标健康有 3 套读模型（`api/v1/data_quality_target.go:421` 的 `qualityTargetWorkspace`、`mcp/tool.go` 的 `get_quality_signal`/`get_quality_target`/`list_quality_targets`、`agentrunner/tools.go:33` 的 `gatherEvidence`），`StoredMetadata → 模型上下文` 有 3 套构建器（`llm/tools.go:35`、`mcp/tool.go:439`、`server/quality_agent_triage.go:24` → `agentrunner/triage.go`），`StoredMetadata` 的读取与投影还各自带一套。

**G5 — `llm` 包混入了 ExplainSQL 的领域知识。**
`llm/tools.go` 定义 `SchemaContext`/`SchemaObject`/`ColumnInfo` 并做 `StoredMetadata → SchemaObject` 转换，还 import `common.SplitMetaGUID` 与 `storepb`。`llm` 本应只懂"消息、流、工具 schema、错误分类"，现在懂 registry 对象布局。这直接阻碍"抽出干净 agent core"。

**G6 — 会话机制与 DQ 领域强耦合，无法复用。**
转录重建、悬空 tool call 修复、token 估算、压缩切点、checkpoint 水位都在 `agentrunner/{context,compaction}.go`，且直接操作 `storepb.QualityAgentTranscriptMessage` 与 `store.QualityAgentSessionMessage`；`Engine.Store` 接口有 11 个方法（`engine.go:43`）。任何第二个需要"长会话续写"的产品（ExplainSQL 多轮、血缘分析 agent、自然语言查询）都只能复制这套代码。

**G6b — 转录有 3 个写入方、3 套幂等键。**
`agentrunner/engine.go:740`（wake 引擎）、`runner/qualityrunner/agent_events.go:65`（失败事件注入）、`api/v1/data_quality_agent_session.go:301`（人工提问/唤醒）各自拼 `IdempotencyKey`。端口化时要把"写入转录"收成一个入口，否则抽取 core 只会让第四套约定更容易出现（本轮不合并三处调用点，但端口必须能表达幂等键）。

**G7 — prompt 与工具清单手工同步。**
`wakePrompt()` 的文案写着 `get_table_context`、`draft_checks`、`record_investigation`…（`prompt.go:60-77`），与 `modelToolDefs` 的 add 列表靠人肉对齐；`maxTurnsFor` 又与 mode 耦合（`prompt.go` 末尾）。没有一致性测试。

**G8 — 授权/审计有两条互不相干的实现。**
MCP 走 `toolDefinition.Permission` + `Checker.CheckPermission` + 每次调用一行审计（`mcp/server.go:282-289`、`:372-430`），有守卫测试钉住"权限 == RPC 注解"；内置 agent 走 agent principal + `dataquality.DecideAction` 动作策略 + 工作区 kill switch（`api/v1/data_quality_agent.go:159`、`agentrunner/tools.go` 的 `recordDecision`），但**没有工具级权限检查、没有工具级审计行**（agent 的写入靠 RPC 侧或 store 直接落库）。两套模型各自演进，统一注册表必须先把这件事收拢，否则就是给 agent 开新洞。

**G9 — 没有工具级超时、结果预算与并发。**
工具执行没有 deadline（循环只对整个 wake 设 10 分钟）；结果截断只在 `agentrunner/tools.go:100` 一处做（8 KiB），ExplainSQL 完全没有；工具串行执行（`agent.go` 注释 "Execute tools sequentially"），只读工具明明可以并行；单个慢查询就能吃光一次 wake 的预算。

**G10 — 可观测性只到"一次 LLM 调用"。**
`metrics.ObserveLLMCall(feature, provider, outcome, duration, tokens)` 是唯一粒度（`llm/agent.go` `run()` 的 defer）；没有 `tool` 维度的调用数/耗时/结果大小/被拒原因，无法回答"哪个工具最慢、最常失败、最常被策略拒绝"。

**G11 — 相邻的重复（本轮不解决，但端口设计要给未来留位置）。**
会话准入条件（runner 开关、数据库 allowlist、enabled agent、目标可解析）在 3 处各写一遍（`api/v1/data_quality_agent_session.go:43`、`qualityenrollmentrunner.go:654`、`qualitysessionrunner.go:277`）；失败事件注入不在 `agentrunner` 而在 `runner/qualityrunner/agent_events.go`。这些不是本次重构的核心，但说明"agent 运行时"当前没有统一入口；阶段 P2/P3 把会话端口与工具端口立起来后，这些重复才有地方收拢。

### 1.5 不能被破坏的东西（重构的硬约束）

- **DQ 领域不变量**：[data-quality.md](data-quality.md) §5 的 I-81…I-84（会话持久单位、一次 wake 续写同一 conversation、压缩语义、runner 与事件的幂等/预算/block 语义）、I-71…I-75（agent principal、动作目录、investigation、memory 的形状与幂等）、I-82 的"工具白名单不含发布/决策/规则/设置"。
- **MCP 线上契约**：17 个工具的名字、输入 schema、输出字段与错误码词表；每次调用（含被拒）一行审计；只读权限白名单；`/mcp` 60s 端到端超时；`structuredContent` 单副本。`backend/mcp/tool_guard_test.go`、`tool_schema_test.go`、`sdk_contract_test.go` 是这些的看门人。
- **会话转录是事实来源**：assistant/tool/engine 行的顺序、幂等键、`truncated` 标记、`ProcessedSeq` 水位、compaction checkpoint 的 `summary_through_seq` 语义都被前端（`AgentConversation.vue`/`AgentToolCard.vue`）与 `ListQualityAgentSessionMessages` 依赖。
- **前端可见字段**：工具卡片、会话消息、执行进度（ExplainSQL 的 `tool_start/tool_end/tool_output`）都按现有字段渲染，工具结果形状的变化会直接破 UI。
- **单副本假设与 runner 生命周期**：runner 通过 `runner.Go` 启动、`Name()`/`LastTick()` 被状态页读取（[runner.md](runner.md)）；不要在这次重构里顺手动调度模型。
- **不新增表、不新增迁移**（P0–P3）：泛化会话表是独立决策（见开放问题 Q1）。

---

## 2. 目标与非目标

### 2.1 目标（可验证）

1. **工具可插拔**：新增一个领域工具 = 在 `component/tools/<domain>` 注册一个 `Tool`（定义+执行+元数据），并在对应 product 的 spec 白名单里加名字；**不再改** prompt 文案、schema 与 switch 三处。
2. **Agent core 无领域知识**：`component/agent`（暂定名）不 import `store`、`storepb`、`dataquality`、`mcp`；只 import 标准库 + `llm` 传输层 + 通用日志/指标接口。
3. **一套执行管线**：参数校验、身份、限流、IAM 权限、超时、panic 恢复、结果预算、审计、指标在一个地方按固定顺序执行；动作策略按 `Authz` 档位由拥有事实的层判定（RPC 写由 handler、组件提议由管线），工具实现里不再散落权限判断。
4. **一个工具目录、两个前端**：MCP 与内置 agent 共享工具定义与执行；MCP 只暴露 `ReadOnly` 子集且线上契约冻结。
5. **产品即 preset**：`quality-monitor`、`quality-triage` 两个 spec，各自 = prompt sections + 工具白名单 + 预算；boot 时校验（未知工具/重复/写工具缺权限 → 启动失败）。
6. **行为不变**：P0–P3 每个阶段，DQ wake 写出的转录行、Outcome、调度与 MCP 输出与重构前逐字节等价（除新增的审计/指标副作用）。用现有测试与新增的 golden 测试证明。

### 2.2 非目标（本轮明确不做）

- 不做 DSH 式插件系统：无运行期注册/卸载、无 Cordis/DI 容器、无 HMR、无 YAML 组合层。
- 不把 `backend/component/*` 拆成独立 Go module；保持单 module 多包。
- 不打开 MCP 写工具，不改变 MCP 的鉴权（OAuth）与只读定位（D-91）。
- 不改质量判定引擎、规则类型、调度语义、通知/渠道。
- 不新增数据库表/迁移；不重写 qualitysessionrunner 的 lease/claim。
- 不做多 agent 协作/子 agent（subagent）能力；DSH 的 `subagent`/`workflow` 留作未来扩展点，不在本轮抽象里预留复杂度。
- `qualitysuggestionrunner` 与 `Suggester` 的三条 prompt-only 路径保持现状（它们不走工具循环）；只在需要时改用 core 的单 turn 模式，不纳入工具目录。
- 不改前端；仅在结果形状必须变化时（本轮不应发生）才动前端。

### 2.3 设计原则

1. **机制与策略分离**：core 提供能力与顺序，product 决定 prompt、白名单、预算；领域工具决定业务语义。
2. **默认拒绝、显式授予**：工具的写副作用、权限、动作必须显式声明；缺声明是启动错误，不是运行期惊喜。
3. **契约先行、行为不变**：先立契约与守卫测试，再搬代码；每一步可独立回滚。
4. **一个事实来源**：工具 schema 只在注册表里定义一次；工具名只在 spec 白名单出现一次；prompt 不重复工具清单。
5. **静态装配**：所有注册与白名单在 `server.go` 启动时完成；运行期不变（对 KV cache、可测试性、审计都更友好）。
6. **失败可解释**：工具失败返回结构化错误（code/hint/candidates），模型能重试正确的东西，UI 能显示原因，审计能分类。

---

## 3. 目标架构

### 3.1 分层

```
┌──────────────────────────────────────────────────────────────────────┐
│ 前端 A：MCP transport（backend/mcp）                                  │
│  OAuth 身份 · 只读子集 · structuredContent · 逐调用审计 · 60s 超时    │
└───────────────▲──────────────────────────────────────────────────────┘
                │ 复用同一批 Tool 定义与执行体
┌───────────────┴──────────────────────────────────────────────────────┐
│ Tool catalog（backend/component/tools/{metadata,lineage,quality}）    │
│  Tool{Name, Description, Schema, Output, Meta, Execute}               │
│  Meta{ReadOnly, SideEffect, Permission, Action, Timeout, MaxResult…}  │
│  实现 = 调用 service handler 接口（Connect 方法签名），不直接碰 store │
└───────────────▲──────────────────────────────────────────────────────┘
                │ 注册进 Registry；按 AgentSpec 解析成 Set
┌───────────────┴──────────────────────────────────────────────────────┐
│ Agent runtime（backend/component/agent）                             │
│  Loop（turn/stream/tool batch/取消/预算）· Prompt sections ·          │
│  Transcript 端口（重建/修复/压缩）· 执行管线（校验→鉴权→策略→执行→    │
│  结果→审计→指标）· 事件 · Feature 级指标                              │
└───────────────▲──────────────────────────────────────────────────────┘
                │ 一个 product = 一个 AgentSpec + 一个 Waker
┌───────────────┴──────────────────────────────────────────────────────┐
│ Agent products（backend/component/dataquality/agentrunner 等）        │
│  quality-monitor / quality-triage spec · 控制类工具（block/schedule） │
│  确定性回退 · wake 编排（Outcome/水位/预算）· 会话存储适配器          │
└──────────────────────────────────────────────────────────────────────┘
```

依赖方向只有**自上而下**：MCP → catalog；product → agent runtime + catalog；agent runtime → llm（传输层）；catalog → 领域组件（`dataquality`、`common`、proto）；**没有任何反向 import**。`api/v1` 实现 catalog 声明的 service 接口，由 `server.go` 注入，因此 `catalog` 不 import `api/v1`（与现在 MCP 的 `InstanceReader`/`QualityReader` 模式一致）。

### 3.2 包布局（目标态）

| 包 | 新/旧 | 职责 | 允许 import |
| --- | --- | --- | --- |
| `backend/component/llm/` | 收窄 | OpenAI 兼容流式传输、消息/usage、失败分类、JSON 解析、provider registry、debug 日志（保留现状，删 `tools.go` 的领域类型） | 标准库、`store`（仅 registry/debug）、`metrics` |
| `backend/component/llm/debuglog/` | 可选新增 | 把 `debug.go` 的 `llm_debug_log` 写入移出 `llm`，让传输层不依赖 store（G5 的收尾） | `store` |
| `backend/component/agent/` | 新 | `Loop`（现 `RunAgentLoop`）、`AgentEvent`、`AgentMessage`、预算、取消、`Prompt` 分节、工具批调度、`Metrics`/`Feature` | 标准库、`llm`、`agent/tool`（接口） |
| `backend/component/agent/tool/` | 新 | `Tool`/`Meta`（`Scope`/`Authz`/`Permission`/`Action`）/`Result`/`Call`/`Invocation`、`Registry`、`Set`、执行管线、`Error` 词表、结果预算、`EvidenceRef` | 标准库、`common`、`permission`、`store`（仅 Principal）、`metrics` |
| `backend/component/agent/transcript/` | 新 | `Message`、`Port` 接口、`BuildConversation`、`RepairToolCalls`、`PruneToolResults`、`EstimateTokens`、`Compactor`（指令由调用方给） | 标准库、`llm` |
| `backend/component/tools/metadata/` | 新 | `list_instances`/`list_databases`/`search_metadata`/`list_metadata`/`get_metadata`/`get_ddl`/`whoami` + 名字寻址（现 `mcp/ref.go`）+ `SchemaContext` 构建（现 `llm/tools.go`） | `agent/tool`、proto、service 接口 |
| `backend/component/tools/lineage/` | 新 | `analyze_sql`、`get_lineage_graph` | 同上 |
| `backend/component/tools/quality/` | 新 | 读：`get_quality_signal`、`list_quality_targets`、`get_quality_target`、`list_quality_issues`、`list_quality_executions`、`get_quality_execution`、`get_quality_impact`、`get_quality_consumption_coverage`、`get_table_context`（`AuthzPermission`）；提议写：`record_investigation`、`record_asset_fact`、`record_decision`（`AuthzPermission` + `Action`）、`draft_checks`（`AuthzAction`，见 §3.5.1/Q7） | 同上 + `dataquality`（ActionID/plan） |
| `backend/component/dataquality/agentrunner/` | 收窄为产品层 | `spec.go`（monitor/triage）、`prompt.go`（sections）、`control_tools.go`、`fallback.go`（确定性巡检/调查）、`wake.go`（Outcome/水位/预算）、`session_port.go`（`*store.Store` → `transcript.Port` 适配） | `agent`、`agent/tool`、`agent/transcript`、`tools/quality`、`dataquality`、`store` |
| `backend/mcp/` | 收窄为 transport | OAuth/鉴权/审计/错误/分页/`structuredContent`；工具定义改为引用 catalog 的只读子集 + MCP 专用投影 | `agent/tool`、`tools/*`、service 接口 |
| `backend/runner/qualitysessionrunner/` | 不变 | lease/claim/预算/事件续跑 | `agentrunner`（Outcome/Waker） |
| `backend/component/dataquality/suggester.go` 等 | 不变（后续可选） | prompt-only LLM 能力；P4 起可改用 `agent.Loop` 直接调用（不经过工具） | `llm`/`agent` |

文件级迁移映射（附录 A 有完整工具清单）：

| 现在 | 去向 |
| --- | --- |
| `llm/agent.go`、`llm/event.go`、`llm/message.go` 的循环/事件部分 | `component/agent/loop.go`、`event.go`、`message.go` |
| `llm/config.go`（provider 选择） | 留在 `llm`（provider 选择是 registry 的职责） |
| `llm/tools.go` `SchemaContext`/`BuildContextFromMetadata` | `component/tools/metadata/context.go` |
| `llm/tools.go` `ExplainSQLTools` | 删除；ExplainSQL 改用 catalog 的 metadata 工具 |
| `agentrunner/context.go`、`compaction.go`（机制部分） | `component/agent/transcript/` |
| `agentrunner/prompt.go` `systemPrompt`/`wakePrompt` | `agentrunner/prompt.go`（产品层，保留但改为 sections） |
| `agentrunner/prompt.go` `modelToolDefs` | 删除；由 `agentrunner/spec.go` 的白名单 + catalog 生成 |
| `agentrunner/tools.go` 的读/提议写工具 | `component/tools/quality/` |
| `agentrunner/tools.go` 的 `block_session`/`schedule_wake` | `agentrunner/control_tools.go`（产品层，不进 catalog） |
| `mcp/tool.go` 的工具定义 | `component/tools/*`；`mcp` 保留 wire 适配与 MCP 专用投影 |
| `mcp/ref.go` | `component/tools/metadata/ref.go`（MCP 与内部 agent 共用） |
| `explain_sql_service.go` 的 `executeExplainTool`/`toolGetObjectSchema` 等 | 删除；改用 catalog |
| `server/quality_agent_triage.go` | `agentrunner/session_port.go` 或 `tools/quality/context.go`（取决于归属；建议读侧进 catalog，写侧进产品） |

### 3.3 工具契约（Go 草案）

```go
// package tool

// Tool 是一个模型可调用的能力。定义、执行、元数据在同一个值里，
// 这样"新增工具"只发生在一处。
type Tool struct {
    Name        string
    Description string
    Schema      json.RawMessage // 参数 JSON Schema，进入 prompt 的唯一来源
    Output      *OutputSpec     // 可选：结果的结构化契约（文档/校验/UI 投影）
    Meta        Meta
    Execute     func(ctx context.Context, call Call, inv *Invocation) (Result, error)
}

// Meta 是策略与运行预算的声明处。缺声明 = 启动错误。
type Meta struct {
    Group      string      // "metadata" | "lineage" | "quality" | "agent"
    Scope      Scope       // ScopeCatalog（两个前端都可消费）| ScopeProductOnly（仅内置产品）
    Authz      AuthzMode   // 见下；决定这个工具靠 IAM 权限、动作策略还是仅会话归属
    ReadOnly   bool        // true 才能进 MCP 只读子集
    SideEffect SideEffect  // None | Propose | Publish
    Permission string      // IAM 权限串（AuthzPermission 必填；来自所调 RPC 的注解）
    Action     string      // 动作目录 id（AuthzAction 必填）。用字符串而不是 dataquality.ActionID，
                           // 是为了让 agent/tool 不 import 领域包；DQ 的策略适配器把字符串映射回去。
    Timeout    time.Duration // 0 = 继承调用方 deadline
    MaxResultBytes int       // 0 = 不截断；DQ 的模型工具显式 8 KiB（与今天一致）
    ParallelSafe bool        // 只读且无共享状态才允许 true
}

// AuthzMode 说清一个工具"凭什么被允许执行"。
type AuthzMode int
const (
    // AuthzPermission：工具调用 RPC handler，管线用调用者自己的 principal 复现
    // 被进程内调用跳过的 ACL interceptor，再执行。MCP 可复用。
    AuthzPermission AuthzMode = iota
    // AuthzAction：工具调用领域组件（没有对应 RPC 权限），由动作策略把关。
    // 目前只有 draft_checks；必须在 spec 里显式出现，不能进 MCP。
    AuthzAction
    // AuthzInternal：agent 簿记/控制（block/schedule/transcript），不是工作区动作；
    // 只受会话归属约束，不做 IAM 检查，只能注册在产品层。
    AuthzInternal
)

type Scope int
const (
    ScopeCatalog Scope = iota // 可被 MCP 与内置 agent 共同消费
    ScopeProductOnly          // 只属于某个 agent 产品（控制类/簿记类）
)

type SideEffect int
const (
    SideEffectNone SideEffect = iota
    SideEffectPropose // 写提案/证据，不改判定、不发布
    SideEffectPublish // 改判定/发布/管理——本轮不暴露给 agent
)

// Call 是模型发起的一次调用，参数已冻结。
type Call struct {
    ID        string
    Name      string
    Arguments json.RawMessage
}

// Invocation 是本次调用可用的全部上下文；工具不读全局状态。
type Invocation struct {
    Principal *store.UserMessage   // 用户或 agent 的 SERVICE_ACCOUNT principal（agent/tool 允许 import store；agent 循环本身不允许）
    Workspace string
    Session   SessionRef           // 产品会话标识（可空）
    Agent     *AgentRef            // 内置 agent 时非空（id/enabled/autonomy）
    Control   *ControlState        // 控制类工具专用；产品层传入
    Policy    Policy               // 动作策略门面（接口；由产品层注入 DQ 的实现）
    Budget    Budget               // 限流/配额/剩余 token
    Audit     AuditSink            // 一行一条；nil = 不审计
    Evidence  *EvidenceCollector   // 跨工具收集证据引用
    Now       func() time.Time
    Logger    *slog.Logger
}

// Result 是工具的规范化输出。
type Result struct {
    Value     any      // 结构化值（json.Marshal 后进 transcript / structuredContent）
    Content   string   // 模型可见文本；空 = 紧凑 JSON(Value)
    IsError   bool     // 业务失败（模型可重试/换参），不是"调用炸了"
    Error     *ToolError
    Truncated bool     // 结果被预算截断
    Evidence  []EvidenceRef // kind+reference，不接受自由文本
    Meta      map[string]any // UI 卡片附加信息（不进模型上下文）
}

type ToolError struct {
    Code       string   // 与 MCP 错误词表一致：invalid_argument / not_found / ambiguous / permission_denied / resource_exhausted / unavailable / internal
    Message    string   // 给模型看的一句话
    Hint       string   // 下一步怎么做
    Candidates []string // 寻址失败时的候选
    Retryable  bool
}

// Policy 是动作策略的 seam。agent/tool 只认识字符串动作 id 与判定结果；
// DQ 的产品层把 action 映射到 dataquality.ActionID 并计算 criticality。
type Policy interface {
    Decide(ctx context.Context, action string, inv *Invocation) (PolicyDecision, error)
}

type PolicyDecision struct {
    Allowed           bool
    RequiresApproval  bool
    Code, Reason      string // denied_autonomy_off / denied_human_only / denied_red_line / denied_critical / requires_approval…
}
```

注册与选择：

```go
type Registry struct{ /* name → Tool, group → names, 只读子集 */ }

func (r *Registry) Register(t Tool) error // 重名、schema 为空、Authz/Scope 声明不完整 → error
func (r *Registry) Catalog() *Registry    // 只含 ScopeCatalog；MCP 与内置 agent 共同能力的来源
func (r *Registry) ReadOnly() *Registry   // MCP 用；Catalog 中 ReadOnly=true 的子集
// 启动规则：AuthzPermission ⇒ Permission 非空；AuthzAction ⇒ Action 非空；
// AuthzInternal ⇒ ScopeProductOnly 且 Permission/Action 为空。违规 = 启动失败。

// AgentSpec 是一个 agent 产品的声明。白名单是显式名字列表，
// boot 时解析成 Set；未知名字是启动错误，不是运行期 "unknown tool"。
type Spec struct {
    Name        string          // "quality-monitor" | "quality-triage" | "explain-sql"
    Feature     string          // 指标标签（沿用 "dq_agent" 等）
    Sections    []PromptSection // 系统提示的组成（顺序固定）
    Tools       []string        // 显式白名单
    MaxTurns    int
    MaxConversationBytes int
    Temperature *float64
    MaxTokens   int
    Compaction  CompactionConfig
}

func (r *Registry) Resolve(spec Spec) (Set, error)
func (s Set) Schemas() []ToolSchema             // 进 prompt
func (s Set) Execute(ctx context.Context, call Call, inv *Invocation) Result
```

### 3.4 一次工具调用的执行管线（唯一入口）

| # | 步骤 | 职责 | 失败时模型看到 | 备注 |
| --- | --- | --- | --- | --- |
| 1 | 查表 | name 在 Set 里？ | `unknown_tool`（正常错误结果） | 不 panic、不终止 wake |
| 2 | 解析参数 | JSON → 工具声明的 Go 输入类型；校验必填/枚举/边界 | `invalid_argument` + hint | 参数在进入执行体前冻结 |
| 3 | 身份 | principal/workspace 来自 Invocation，**不从 ctx 猜** | `unauthenticated` | 内置 agent 用 session 上的 agent principal（SERVICE_ACCOUNT），**不是提问的人**；MCP 用 OAuth 用户 |
| 4 | 预算 | principal 限流（[rate-limiting.md](rate-limiting.md)）+ 唤醒剩余预算 | `resource_exhausted` | 与 MCP 的 `CallLimiter` 共用实现 |
| 5 | IAM 权限 | `AuthzPermission` 工具：`Checker.CheckPermission(meta.Permission, principal)` | `permission_denied` | 检查的是**调用者自己的** principal；MCP 已有，内置 agent 新增（补上进程内调用跳过的 interceptor） |
| 6 | 动作策略 | `AuthzAction` 工具在管线里调 `Policy.Decide`；`AuthzPermission` 写工具由 handler 的 `authorizeAgentAction` 执行（它读目标后才拿得到 criticality） | `denied_*` + 原因 / `requires_approval` | 判定只做一次、在拥有事实的层做；人不受影响 |
| 7 | 执行 | `context.WithTimeout`；`defer recover()`；响应 ctx 取消 | `unavailable`/`tool_timeout`/`internal` | panic 与超时都变成结果，不结束 wake |
| 8 | 结果整形 | `Value` → `Content`（工具 render 或紧凑 JSON）；`MaxResultBytes` 截断并置 `Truncated`；证据引用入 `Evidence` | — | transcript 行写 `truncated` 标记（现有行为） |
| 9 | 审计/指标 | 一行审计（含被拒）；`tool_calls_total{product,tool,outcome}`、`tool_duration_seconds`、`tool_result_bytes` | — | 审计失败不阻塞调用（现状同样 best-effort） |

关键取舍：**工具实现内部不再做权限判断**。今天 `recordDecision` 里直接调 `DecideAction`（`agentrunner/tools.go:317`）并且把"critical 资产要人"写死在工具里；重构后这只是 `Meta.Action = ActionRecordDecisionWhy` + 管线的一步，工具体只剩下"读目标 → 写内存"。

### 3.5 一个 catalog，两个前端

| 关注点 | MCP 前端（`backend/mcp`） | 内置 agent 前端（product + loop） |
| --- | --- | --- |
| 身份 | OAuth bearer → `store.UserMessage`（**用户自己**） | session 的 agent principal（**agent 自己**，不是提问的人） |
| 工具集 | `catalog.ReadOnly()` | `spec` 白名单解析出的 Set |
| 输出线上形状 | `structuredContent`（MCP 专用投影 + 紧凑 JSON） | transcript 的 `tool` 行 `content` |
| 审计 | 每次调用一行（现状） | 每次调用一行（新增），actor=agent |
| 超时 | `/mcp` 60s 端到端 | wake 10m；工具级 `Meta.Timeout` |
| 写能力 | 无 | 仅 `Propose`，且过动作策略 |

共享的是**定义、参数校验、权限判定、动作策略、结果预算、错误词表、寻址实现**，差异只留在传输与投影层。这样 G4 的漂移从根上消失。

对 MCP 的承诺：**wire 契约不变**。做法是 MCP 继续拥有自己的 `toolDefinitions()` 视图（名字、描述、InputSchema、投影），但它的 `Run` 改为调用 catalog 的工具执行体（或 `Run` 与 catalog 共享同一个实现函数）。P2 加一条守卫测试：对同名工具，`MCP InputSchema` 必须等于 `catalog.Schema`（允许 MCP 额外附加描述），否则 CI 失败。

### 3.5.1 身份与授权：agent 是自己的主体，不是用户的代理

先澄清最容易误解的一点：**工具管线里的 IAM 检查不是"用发起会话/提问的那个人的权限"**。执行身份永远是本次调用的 principal——MCP 是 OAuth 背后的用户，内置 agent 是这个 agent 自己的 `SERVICE_ACCOUNT`。对内置 agent 而言，人只是归因，不是授权来源。

四层各自回答一个不同的问题：

| 层 | 回答的问题 | 谁定义 | 作用对象 | 失败形态 |
| --- | --- | --- | --- | --- |
| 执行身份（principal） | 这次调用是谁在做？ | token / session | 用户或 agent principal | `unauthenticated` |
| IAM ACL（权限） | 这个 principal 在本工作区**可以**调用哪些能力？ | workspace IAM policy + 角色 | 任何 principal（含 SERVICE_ACCOUNT） | `permission_denied` |
| 动作策略（autonomy） | 这个 agent **现在应不应该自主**做这件事？ | DQ 动作目录 + kill switch + criticality | 仅 agent | `denied_*` / `requires_approval` |
| 产品白名单（spec） | 这个 agent 产品**暴露哪些工具**？ | product spec | 仅内置 agent | 启动错误 / 工具不存在 |

关系：**IAM 是外边界，动作策略是自主性边界，spec 是能力选择**。spec 只能收窄、不能授权；动作策略不能绕过 IAM；IAM 不表达"要不要人批准"。

agent 的身份事实（与外部 agent 完全一致）：

- `quality_agent` 行 + `SERVICE_ACCOUNT` principal + `roles/qualityAgent` 绑定（`backend/store/predefined_roles.go`）；
- 角色只有 `dataQuality.rules.get/list` + `dataQuality.plans.update`；**读能力来自 `workspaceMember` 基线**——单租户下每个已认证主体都能读元数据、血缘、质量证据（`backend/store/predefined_roles.go` 的 `memberBaselinePermissions`）；
- token 是普通 access token（90 天、可轮换、停用 principal 即全部失效），走与用户相同的 auth/ACL 链；
- 写动作必须同时过 IAM（`plans.update`）和动作策略（A0/A1/A2 + kill switch + criticality 放大器）。

由此推出三条设计规则：

1. **内置 agent 与外部 agent 共用一份授权契约。** runner 不是"拥有环境权限的内部服务"：重构后它执行时用的就是 agent principal，写的也是 agent 有权写的东西（今天它直连 store 写质量表，这正是要收掉的旁路）。否则 `roles/qualityAgent` 只约束外部 token，内置 runner 靠环境权限行动，两者必然漂移——这正是 I-71/I-72 要防的。内置 agent 不需要 IAM 之外的特权，只需要**不经过网络**。
2. **人类提问/创建会话只是归因，不是授权。** 人问一句话不会把自己的权限借给 agent（避免 confused deputy）；`quality_agent_session.created_by`、ASK 消息、审计行记录人，但执行身份始终是 agent。若未来工作区收窄到"某个人不能读全部"，才需要额外检查"提问者能否看到这条结果"；当前单租户全读，这个问题不存在。
3. **agent 的簿记不是工作区动作。** 追加转录、更新 session/lease、排下一次 wake、block 自己，是 agent 对自己会话的状态写入，由会话归属约束，不经 IAM（它们没有对应 RPC，也不应被建模成权限）；只有"对工作区资源做动作"（读证据、写调查/记忆/计划提案）才进入权限模型。

工具怎么取得授权（D4 的补充）：

- 工具调用 RPC handler 时，**进程内调用会跳过 ACL interceptor**（MCP 已经踩过这个坑，所以它自己调 `Checker`）。统一管线第 5 步做的就是这件事：用 agent principal 复现 interceptor 的检查；随后 handler 里的 `authorizeAgentAction` 是第二道（动作策略）。实现上建议提供一个 `invokeAs(principal, perm, fn)` 小助手：设置 `common.UserContextKey`、校验 permission、执行，MCP 与内置工具共用，避免"忘了检查"。
- `Meta.Authz` 决定工具走哪条路：`AuthzPermission`（RPC 支撑，`Permission` 必填，MCP 可复用）、`AuthzAction`（组件调用，`Action` 必填，必须在 spec 显式出现，进不了 MCP）、`AuthzInternal`（仅产品层的控制/簿记，不做 IAM 检查）。启动校验：`AuthzPermission` 缺 `Permission`、`AuthzAction` 缺 `Action`、`AuthzInternal` 出现在 catalog 或 MCP → 启动失败/测试失败。

两个需要评审拍板的点：

- **Q7 `draft_checks` 的授权。** 它现在直接调 `Suggester`（无 RPC、无审计、agent 角色也没有 `dataQuality.suggest`），而这是 M7 triage 的核心能力。统一后要么 (a) 改走 `SuggestQualityRule`/`CreateQualitySuggestionJob` RPC，并给 `roles/qualityAgent` 加 `dataQuality.suggest`（显式、可审计，但改 I-71 的"角色只有两项写权限"）；要么 (b) 保留组件调用，用 `AuthzAction` + 一张新的动作卡（不扩大角色，但能力仍不在 RPC 审计面）。我倾向 (a)：把"agent 会往复核队列写草案"变成可见、可审计、可撤销的能力；但这必须作为产品决策显式记录。
- **Q8 SERVICE_ACCOUNT 是否该继承 `workspaceMember` 基线。** 当前是继承的，所以 quality agent 的 token 也能调用 manual SQL 等基线 RPC；内置 spec 不暴露这些工具，但外部 agent 拿着 token 可以直连 RPC。严格最小权限需要在 IAM 层区分"agent principal 的基线"，属于平台级语义变更，建议单独立项而不是夹在本次重构里。

### 3.6 Agent runtime（core）的边界

Core 只保留机制：

- **Loop**：一次调用的 turn 循环；流式事件；工具批调度（先串行，后续对 `ParallelSafe` 开并行）；`MaxTurns`/`MaxConversationBytes`；取消；context overflow 信号；`Feature` 级指标。**不读 store、不认识 DQ**。
- **Prompt**：按固定顺序组装 sections（身份 → 产品规则 → 运行时上下文快照 → 工具使用说明）；工具 schema 由 `Set.Schemas()` 自动注入；产品文本不重复工具清单。DSH 的"动态上下文作为可替换的快照消息"值得借用（见 §5）。
- **Transcript 端口**：
  ```go
  type Port interface {
      Load(ctx context.Context, afterSeq int64) ([]Message, error)
      Append(ctx context.Context, msg Message) (seq int64, err error)
      SaveSummary(ctx context.Context, summary string, throughSeq int64) error
  }
  ```
  `Message` 用 `llm.AgentMessage` 的扩展（role/content/toolCalls/toolCallID/toolName/truncated）；DQ 的 `storepb.QualityAgentTranscriptMessage` 映射放在 `agentrunner/session_port.go`，core 不 import storepb。**P0–P3 不加表**：适配器映射到现有 `quality_agent_message`。
- **Compactor**：纯函数（切点、修剪、token 估算）+ 一个 `Summarize(ctx, instruction, msgs) (string, usage, error)`；**摘要指令文本留在产品层**（今天 `compaction.go` 里那段 DQ 专用指令移回 `agentrunner`）。
- **事件模型**：沿用 `AgentEvent{Type, Content, ToolCall, ToolResult, Assistant, Usage, Error, Turn}`，新增 `AgentEventToolEnd` 的 `Truncated`/`Evidence` 字段（可选）。**不引入 DSH 的三层事件体系**——我们的持久化已经是"转录行 + 幂等键"，够用且被前端依赖。

### 3.7 产品层：DQ quality agent

产品层只剩"策略与编排"：

- `spec.go`：
  ```go
  func MonitorSpec() agent.Spec {
      return agent.Spec{
          Name: "quality-monitor", Feature: "dq_agent",
          Sections: []agent.PromptSection{identitySection, monitorRulesSection},
          Tools: []string{
              "get_target_summary", "list_recent_executions", "get_memory",
              "record_investigation", "record_asset_fact", "record_decision",
              "block_session", "schedule_wake",
          },
          MaxTurns: 8, MaxConversationBytes: llm.DefaultMaxConversationBytes,
      }
  }
  func TriageSpec() agent.Spec { /* 上面 + get_table_context, draft_checks, MaxTurns 12 */ }
  ```
- `control_tools.go`：`block_session`、`schedule_wake` 只在产品层注册；它们通过 `Invocation.Control` 读写一个**类型化的 wake 状态**，返回 `Result` + 一个 `ControlDirective`（`Block{Code,Reason}` / `Schedule{After,Reason}`），由 wake 编排在工具批次结束后统一应用——**彻底去掉 5 个出参指针**。
- `fallback.go`：无模型时的确定性巡检/事件调查（`engine.go` 的 `deterministicWake`/`maintain`/`investigateEvent` 原样搬入）。
- `wake.go`：`Wake`/`runWake`/`modelWake` 的编排与 `Outcome`、`ProcessedSeq`、`BlockedRounds`、token 统计、重试语义。
- `prompt.go`：系统提示改成分节（identity / long-session rules / tool discipline / no-publish rule），wake prompt 保留文案但工具相关句子由 spec 校验（新增一致性测试：prompt 文本里出现的 backticked 工具名必须都在 spec 白名单里，且 spec 每个工具都被提及或明确豁免）。
- `session_port.go`：`*store.Store` → `agent/transcript.Port` 的适配（`AppendQualityAgentTranscriptMessage`、`ListQualityAgentTranscriptMessages`、`UpdateQualityAgentSessionCompaction`），幂等键与修复逻辑保持今天的语义（`interruptedToolResult` 常量、`repairInterruptedToolCalls`）。

### 3.8 工具与产品清单（目标态）

| 产品 | Spec | 工具（白名单） | 模型 | 写能力 |
| --- | --- | --- | --- | --- |
| DQ monitor | `quality-monitor` | 8 个（上表 monitor） | 必需（无模型走确定性回退） | `Propose`：investigation/asset fact/decision |
| DQ triage | `quality-triage` | 10 个（+ table context/draft checks） | 必需（无模型 park） | `Propose`：draft checks |
| ExplainSQL（后续） | `explain-sql` | metadata 读工具 + lineage 读工具（2–4 个） | 必需 | 无 |
| （未来）血缘分析 / 元数据治理 | 各自 spec | 读 + 提议写 | 必需 | `Propose` |

---

## 4. 关键决策（D1–D14）

### D1 三分层，而不是两分

**决策**：agent runtime / tool catalog / product preset。
**理由**：工具不是 agent 的私有物——MCP 已经证明同一批能力有两个消费者；把工具塞进 agent core 会重演今天 `llm/tools.go` 的领域泄漏。产品层单独存在，是为了让"人格、白名单、预算、回退"可替换而不碰机制。
**备选**：agent core 内嵌工具注册（否决：MCP 复用不了）；把工具做成 agent 的插件系统（否决：见 D2）。

### D2 单 module、静态注册、不做运行期插件

**决策**：`Registry.Register` 只在 `server.go` 启动时调用；没有 unregister/HMR/动态 enable。
**理由**：DSH 的插件/DI 是为"第三方扩展 + 多产品组合"服务；我们是单二进制、自托管、工具与产品同仓演进。静态装配让 schema、权限、审计在启动时即可验证（重复名、未知白名单、写工具缺权限直接启动失败），并让 prompt 前缀稳定（KV cache 友好）。
**备选**：运行期注册（否决：审计与 prompt 稳定性变差，且没有第三方扩展需求）；独立 Go module（否决：无版本隔离需求，增加 CI/依赖负担）。

### D3 工具 = 定义 + 执行 + 元数据 + 结构化结果

**决策**：一个 `Tool` 值同时持有 schema、描述、执行体、策略元数据、结果契约。
**理由**：今天 schema 在 `prompt.go`、执行在 `tools.go`、策略散落、文案在 prompt 字符串——四份真相。合一后编译器/启动校验能兜住"删了 case 忘了删 schema"这类错误。
**备选**：DSH 的 `defineTool` 生成式 schema（Go 无同类生态；短期手写 JSON Schema + parity 测试，长期考虑 struct tag 生成，见 Q3）。

### D4 工具实现调用 service handler 接口，不直连 store

**决策**：工具包声明最小 service 接口（Connect handler 方法签名，与 `backend/mcp/server.go:35-69` 一致），`api/v1` 的 service 实现它，`server.go` 注入。管线在调用前用**调用者自己的 principal** 复现被进程内调用跳过的 ACL interceptor（建议封装成 `invokeAs(principal, perm, fn)`，MCP 与内置工具共用），handler 内的动作策略作为第二道。
**理由**：MCP 的现有决策（"工具跑的就是 API 跑的代码"）已经证明这能保证校验、权限、审计、转换一致；DQ 工具目前直连 store，导致同一个写入有两套路径与两套语义。改成接口后，agent 的写入自动经过 handler 的权限与审计，policy 只需补"动作策略"这一层。
**边界**：只有存在 RPC 支撑的动作才走这条路；`draft_checks` 这类"组件管线 + 动作策略"的提议写走 `AuthzAction`，agent 簿记/控制走 `AuthzInternal`（均见 §3.5.1）。
**备选**：直连 store（否决：绕过 IAM/审计，漂移）；复制 handler 逻辑（否决：第三份真相）。

### D5 一个 catalog、两个前端；MCP 线上契约冻结

**决策**：catalog 是唯一定义/执行来源；MCP 是 transport 适配器，只取 `ReadOnly` 子集，输出仍走 MCP 自己的投影。
**理由**：消灭 G4 的双份实现；但 MCP 的 wire 形状（字段名、枚举、错误词表、单副本 `structuredContent`）被外部客户端与测试锁定，不能为了统一而改。
**备选**：让内部 agent 直接走 MCP 的 HTTP 端点（否决：同进程多一次网络/鉴权，且 MCP 无写工具）。

### D6 工具只由 AgentSpec 显式白名单选择

**决策**：spec 列工具名；未知名字/重复/空 spec 是启动错误；没有 wildcard、没有运行期条件注册。
**理由**：token 成本可控、审计可解释、prompt 可预测；mode 差异（monitor vs triage）用两个 spec 表达，不需要 DSH 的 `when` 条件。
**备选**：按 group/tag 选择（否决：组变大时无法控制上下文）；全量暴露（否决：17+ 工具的 schema 会显著膨胀 prompt 并降低选择准确率）。

### D7 两道门：IAM 权限 + 动作策略

**决策**：按 `Meta.Authz` 分档授权——`AuthzPermission` 工具在管线里用调用者 principal 过 IAM；动作策略则**在拥有事实的层**执行：RPC-backed 写仍由 handler 的 `authorizeAgentAction`（读目标后带 criticality）判定，组件级提议（`draft_checks`）由管线用 `Policy.Decide` 判定；`AuthzInternal` 只受会话归属约束。MCP 只消费 `ScopeCatalog + ReadOnly`，只走 IAM。工具用 `Meta.Action` 声明对应动作卡；集成测试钉住"每个 Propose 工具在 autonomy 关闭时都被拒"，证明它真的经过了动作策略；缺声明是启动错误。
**理由**：两套现有模型（MCP 的 IAM、DQ 的 autonomy 开关 + 动作目录）各自都是正确的，只是没汇合；汇合点必须在管线里，不能在各工具里。但同一判定不能带不同输入做两遍（管线没有 target criticality，handler 有），所以 IAM 在管线、动作策略在拥有事实的层，并用守卫测试把两边对齐。分档也避免逼着 `draft_checks` 去要一个它没有的 RPC 权限。
**备选**：所有工具一律两道门（否决：会迫使扩大 `roles/qualityAgent`，见 Q7）；只留 IAM（否决：动作目录表达的"critical 资产要人"是 DQ 的领域承诺，IAM 表达不了）；只留动作策略（否决：会绕过工作区角色体系，且 MCP 没有动作策略）。

### D8 转录作为端口，P0–P3 不迁移表

**决策**：core 定义 `transcript.Port` 与 `Message`；DQ 用现有表适配。
**理由**：这是"复用会话机制"的最小代价路径；泛化表（`agent_session`/`agent_message`）会牵动迁移、前端、RPC、集成测试与 400KB 的 data-quality.md，风险和收益不成比例。
**备选**：立即建通用表（否决：本轮非目标）；让 core 直接用 `storepb.QualityAgent*`（否决：领域泄漏复发）。

### D9 Compaction 留在 core，摘要指令留在 product

**决策**：切点/修剪/token 估算/摘要调用编排在 `agent/transcript`；DQ 的 checkpoint 指令文本在 `agentrunner`。
**理由**：机制跨产品复用；指令是产品语言（写的是"data quality agent"），换产品必须换。
**备选**：整块留产品（否决：第二个产品要复制 300 行）；整块进 core 带 DQ 文案（否决：领域泄漏）。

### D10 控制类工具留在产品层，用返回值而不是指针

**决策**：`block_session`/`schedule_wake` 不进 catalog；通过 `Result.Control` 返回指令，由 wake 编排统一应用。
**理由**：这类工具只在"一次 wake"的语义里成立，MCP 与未来产品都不该看到；返回值让工具可测试、可并发、可审计。
**备选**：指针回写（否决：G3）；放进 catalog 加条件（否决：无消费者）。

### D11 结果预算与证据引用

**决策**：`Meta.MaxResultBytes`（默认 8 KiB，与现行为一致）+ `Result.Truncated` + `EvidenceRef{Kind,Reference,Note}`；`record_investigation` 等工具从 `Invocation.Evidence` 合并证据，模型不再自由编造引用文本。
**理由**：上下文是稀缺资源（[data-quality.md](data-quality.md) 的 I-83 已经在做修剪）；证据"由构造保证"能强化 I-74 的"没有证据的调查不算完成"。
**备选**：全局统一截断（否决：不同工具合理上限差异大）；模型自由写 evidence（否决：不可验证，正是今天 investigation 用指纹/引用兜底的原因）。

### D12 并行工具默认关闭，只对只读+`ParallelSafe` 打开

**决策**：P0–P3 保持串行（与今天逐字节一致）；P4 起对 `ReadOnly && ParallelSafe` 的工具开并行，模型顺序提交结果，写与控制类工具是 barrier。
**理由**：DQ 工具里 `record_*` 与 `block_*` 有顺序语义；先拿到正确性，再拿吞吐。只读工具（metadata/lineage/quality 读）并行收益最大。
**备选**：一步到位并行（否决：与"每阶段行为不变"冲突）。

### D13 不继承调用者权限：agent 以自己的 principal 执行

**决策**：内置 agent 的每次工具调用都带 agent 自己的 principal；创建会话的人、提问的人只做归因，不把他们的权限借给 agent（不做 on-behalf-of / impersonation）。
**理由**：避免 confused deputy——否则任何能唤醒 agent 的用户都能借它做自己无权做的事；也让审计的 actor 稳定（永远是 agent），与外部 agent 的 token 行为一致。单租户下所有成员都可读，因此这条规则当前只影响写与未来收窄的可见性。
**备选**：按提问者身份执行（否决：权限放大 + 审计归因混乱）；按"提问者与 agent 权限交集"执行（否决：不可解释、结果随人变化，难以复现）。

### D14 内置 agent 与外部 agent 共用同一份授权契约

**决策**：runner 不做"环境特权"的内部写；它经过的权限/动作策略与外部 agent 调 RPC 时一致；两者唯一区别是内置 agent 不经过网络。
**理由**：否则 `roles/qualityAgent` 只约束外部 token，内置 runner 靠代码路径行动，权限模型形同虚设（I-71/I-72 的初衷就是"agent 永远没有发布/决策权"）。共用契约也让工具权限声明对两个前端都成立。
**备选**：内置 agent 信任边界内免检（否决：双模漂移，且无法用守卫测试证明 agent 不能越权）；只让内置 agent 走动作策略（否决：外部 agent 仍受 IAM，两套语义）。

---

## 5. 从 DSH 借鉴什么、不借鉴什么

### 5.1 值得借鉴的 6 个机制

| DSH 机制 | DSH 位置 | 在我们的落点 |
| --- | --- | --- |
| **append-only 事件日志是事实来源，模型历史由它派生** | `packages/core/session`、`docs/subsystems/session.md` | 我们已经有了（`quality_agent_message` + 幂等键 + `ProcessedSeq`）；借的是"端口化"：core 只认 `transcript.Port`，产品负责映射 |
| **Tool = schema + `execute` 返回规范化值 + render** | `packages/core/tools/src/contracts.ts`、`schema.ts` | `Tool.Result{Value,Content}`：`Value` 给程序/证据，`Content` 给模型；不再让模型解析散文 |
| **单一有序策略管线（pre-execute → guard → execute → post-execute → result）** | `packages/core/tools/src/pipeline.ts`、`events.ts` | §3.4 的 9 步管线；工具体不含权限判断 |
| **step 级 tool admission（同一份 Set 同时决定可见性与执行）** | `packages/core/tools/src/index.ts` 的 `ResolvedToolSet` | `Registry.Resolve(spec)` 一次解析、prompt 与执行共用；boot 时确定，运行期不变 |
| **协作取消 + 显式 abort 结果 + 合成 tool 结果保持历史合法** | `packages/core/agent-loop`、`tools` 取消契约 | 现有 `repairInterruptedToolCalls` 已是同一思想；补上"工具执行收 ctx、超时即取消" |
| **compaction 作为可撤销的表面替换（surface replace）而非删日志** | `packages/compaction`、`agent-loop` 的 `surfaceOp` | 现有 checkpoint summary + `summary_through_seq` 已等价；借其"切点必须工具配对平衡"的约束（`compaction_test.go` 的 `TestBalancedCutKeepsToolPairs` 已在测） |

补充借鉴（较小、成本低）：

- **工具结果预算后处理**（DSH spill policy）：我们不需要落盘 spill，但 `MaxResultBytes` + `truncated` 标记是同构的。
- **prompt 分节与集中顺序号**（DSH `SECTION_ORDERS`）：避免多段文案争夺位置，也让"运行时事实"与"产品规则"分开。
- **动态上下文作为可替换快照**：DQ 的 wake prompt 同时承载"事实（pending 事件数、target、objective）"与"指令"；可以拆成 `runtime context` 小节 + 短指令，减少 prompt 随事实变化而整段漂移。

### 5.2 明确不借鉴

| DSH 做法 | 为什么不适用 |
| --- | --- |
| Cordis 插件运行时 / 服务容器 / 作用域 shadowing / HMR / YAML profile 分层 | 单二进制、自托管、同仓演进；引入后复杂度远超收益。Go 用 `interface` + `server.go` 装配即可 |
| ~60 个包、每能力 service-definition/provider/consumer 三件套 | 包粒度服务插件生态，不服务单产品；我们目标 6–8 个包 |
| 三层事件体系（session/agent/capability）与 `surfaceOp` 重放 | 我们的持久化是转录行 + RPC 读，前端不消费 Replay；增加一层抽象没有消费者 |
| Client 呈现层（`presentCall`/toolview/React 卡片） | 前端已有 `AgentToolCard.vue` 读 transcript 行；不需要 core 参与呈现 |
| 多 provider 的 request header / system-prompt 增量更新机制 | 我们的 provider 只有 OpenAI 兼容流；一次请求带全量 prompt 更简单、更好调试 |
| subagent / workflow / PTC `run_code` / Agent Teams | 当前产品没有多 agent 需求；留作扩展点，不预先抽象 |
| 无 max-turn 预算、把策略全交给插件 | DQ runner 需要确定性的成本上界（wake deadline/token 预算）；core 保留 `MaxTurns`/`MaxConversationBytes` |
| MCP 工具以 `mcp__server__tool` 命名注入 | 我们的 MCP 是"我们自己的能力出口"，不是第三方工具接入；命名空间反而破坏既有 wire 契约 |

---

## 6. 分阶段迁移计划

> 原则：每个阶段一个（或两个）PR，行为不变；先立契约与测试，再搬代码；每阶段结束时 `go test ./...`、集成 smoke、MCP 守卫测试全绿。

### P0 — 工具契约与执行管线（行为不变）

- 新增 `component/agent/tool`：`Tool`/`Meta`/`Result`/`Invocation`/`Registry`/`Set`/管线骨架。
- 现有两处工具**原地**改成"定义 + 执行"两个函数，再由 registry 包一层：
  - ExplainSQL 的 2 个工具（`llm/tools.go` + `explain_sql_service.go`）；
  - DQ agent 的 10 个工具（`agentrunner/prompt.go` + `tools.go`），控制类工具的指针回写暂时保留（阶段 P3 再改），管线先不接策略。
- `llm.AgentConfig.Tools/Executor` 保留兼容；新增 `ExecutorV2` 或在 loop 内部适配（避免同时改 loop 与工具）。
- 验收：现有 `agentrunner/engine_test.go`、`context_test.go`、`compaction_test.go`、`llm/agent_test.go`、`explain_sql` 相关测试不改断言即通过；新增 registry/管线单测。
- 风险：低。可回滚。

### P1 — 拆出 agent runtime（最大的一步）

- `component/llm` 删 `tools.go` 的领域类型；`SchemaContext` 移到 `tools/metadata/context.go`（P1 可先放 `component/agent/context`，P2 再挪）。
- `llm/agent.go`+`event.go`+`message.go` → `component/agent`：`Loop`、`AgentEvent`、`Message`、预算、取消、Feature 指标。
- `agentrunner/{context,compaction}.go` 的机制部分 → `component/agent/transcript`；DQ 适配器放 `agentrunner/session_port.go`；摘要指令回产品层。
- Prompt 分节：`agent.Prompt` + `agentrunner` 的 sections；保持生成文本逐字不变（golden 对比）。
- 验收：DQ 一次 wake 的模型输入 golden diff 为空；压缩触发/切点/修剪测试通过；ExplainSQL 输出流行为不变；`go vet`/lint 无循环依赖。
- 范围可控性：`agentrunner` 在生产代码里只被 `server/server.go:274` 与 `qualitysessionrunner`（通过 `Waker`）引用；爆炸半径小，也让 P1 结束后旧/新引擎双跑对比可行。
- 风险：中高（改动面大）。缓解：先端口后替换、golden 测试、每步可回滚。

### P2 — 领域工具包 + MCP 复用

- `component/tools/{metadata,lineage,quality}`：把 `mcp/tool.go` 的 17 个工具实现与 `mcp/ref.go` 迁入（先做 metadata+lineage+quality 读），MCP 改为引用 catalog 定义与执行体，保留 MCP 投影/审计/鉴权。
- `quality` 包补 `get_table_context`（从 `server/quality_agent_triage.go` 的读侧迁入）与 4 个提议写工具（改为调用 DQ service handler 接口）。
- 守卫测试：MCP 17 工具名冻结、InputSchema parity、read-only 白名单、权限注解一致；内部 spec 白名单可解析。
- 验收：MCP 工具数量/名字/输出 golden 不变；集成 `mcp_service_test` 通过；DQ 工具行为不变。
- 风险：中（MCP wire 契约）。缓解：parity 测试、分工具迁移、投影层不动。

### P3 — DQ agent 产品化

- `agentrunner` 收窄：`spec.go`、`prompt.go`（sections）、`control_tools.go`、`fallback.go`、`wake.go`、`session_port.go`。
- 控制类工具改为 `Result.Control` + wake 统一应用；删除 5 个出参指针。
- 按 `Authz` 档位接线：IAM 进管线（agent principal）；`record_*` 的动作策略由 handler 判定，`draft_checks` 由管线 `Policy.Decide` 判定；每个 Propose 工具补 autonomy-off 的集成断言。
- prompt/spec 一致性测试；工具级指标与审计。
- 验收：I-81…I-84 的现有测试全部通过；新增 wake 转录行 golden 与旧实现逐字节一致；blocked/schedule/水位行为不变；前端不改。
- 风险：中。缓解：golden + 双跑对比（同一 session 分别用旧/新引擎跑脚本化模型，比较 transcript）。

### P4 — 扩展（按需）

- ExplainSQL 迁移为 `explain-sql` spec（删 854 行 handler 里的工具/上下文逻辑，预计净减 200–300 行）。
- `suggester` 的三条 prompt-only 路径改用 `agent.Loop`（不经过工具）或保持现状。
- 只读工具并行（D12）；工具级指标看板；`EvidenceCollector` 全量接入。
- 写工具统一（未来若需要 rerun/mute/ack，先补动作目录的 `Implemented/AgentReachable` 与审批语义，再暴露）。
- 第二个产品（血缘分析/元数据治理/自然语言查询）验证扩展性：新增 spec + 少量工具，不改 core。

---

## 7. 风险与缓解

| # | 风险 | 触发信号 | 缓解 |
| --- | --- | --- | --- |
| R1 | **过度抽象**：为 2 个消费者造出 5 层接口 | 新包数量 > 8；单个工具要穿过 3 个接口才执行 | 固定"core / tool / catalog / product"四类；接口只在 seam 处（transcript/chat/policy/service）；P0 用最小接口，P2 才加抽象 |
| R2 | **权限被稀释**：统一 registry 让 agent 拿到未授权工具 | 任何 `SideEffect != None` 却声明不出授权模式（`AuthzPermission` 无 Permission / `AuthzAction` 无 Action）；MCP 出现写工具 | 启动校验 + 守卫测试 + MCP `ReadOnly()` 过滤 + `Catalog()` 过滤 + 动作策略门；review 检查清单 |
| R3 | **行为漂移**：转录行/Outcome/前端字段变化 | golden diff 非空；前端测试失败 | 每阶段 golden + 双跑；P0–P3 不新增字段、不改 RPC |
| R4 | **循环依赖**：catalog ↔ api/v1、core ↔ storepb | `go list -deps` 出现环 | catalog 只声明接口、server 注入；core 定义 `Message` 端口；CI 加 import 守卫测试（类似 `mcp/tool_guard_test.go`） |
| R5 | **token 成本膨胀**：工具 schema 全量进 prompt | prompt token 上升 > 10% | spec 白名单 + 描述精简 + 测量（`agent_prompt_tokens` 指标） |
| R6 | **MCP 契约破坏** | `tool_schema_test`/`sdk_contract_test`/集成失败 | wire 层不改；parity 测试；工具名冻结测试 |
| R7 | **DQ 语义回退**：block/schedule/水位/幂等 | engine/runner 测试失败；会话卡住 | 控制指令集中在 wake 编排；保留现有幂等键；双跑对比 |
| R8 | **事务与副作用**：工具走 handler 后审计/权限语义变化 | 集成测试审计行数变化 | handler 的审计保持；IAM 门在管线、动作策略在拥有事实的层；审计 actor 明确为 agent principal |
| R9 | **调试变难**：工具执行穿过 registry，堆栈变长 | 线上排障找不到工具实现 | 工具 `Meta.Group` + 结构化日志（tool/name/call_id/session）；`tool` 维度指标 |
| R10 | **团队认知负担**：新包边界不被遵守，半年后重新耦合 | 新代码把 store 调用写进 agent core | 在 `backend/AGENTS.md` 增补 import 规则；CI import 守卫；review checklist |
| R11 | **性能回退**：每 turn 重建历史/估计 token 变贵 | wake 延迟上升 | 保持现有 watermark 增量读（`AfterSeq`）；compaction 只在超阈值触发；不引入每 turn 全量重算 |
| R12 | **范围蔓延**：顺手改调度/表/前端 | PR 越界 | 非目标清单写进 PR 模板；每阶段验收固定 |

---

## 8. 验证与门禁

### 8.1 新增守卫测试（按包）

`component/agent/tool`：

- `TestRegistryRejectsDuplicateName`
- `TestRegistryRejectsWriteToolWithoutPermissionOrAction`
- `TestSpecUnknownToolFailsAtBoot`
- `TestSpecResolveIsStable`（同样 spec 解析出的 Set 顺序稳定 ← prompt 前缀稳定）
- `TestPipelineUnknownTool` / `TestPipelineInvalidArguments`
- `TestPipelinePermissionDenied` / `TestPipelineActionPolicyDenied` / `TestPipelineRequiresApproval`
- `TestPipelineToolTimeout` / `TestPipelinePanicRecovered` / `TestPipelineContextCanceled`
- `TestPipelineResultTruncatedAtBudget` / `TestPipelineEvidenceRefsCollected`
- `TestToolAuthorizationMode`（`AuthzPermission` 缺 Permission、`AuthzAction` 缺 Action、`AuthzInternal` 进了 catalog/MCP 都失败）
- `TestProductOnlyToolsStayOutOfCatalog`（block/schedule 等控制类工具不可被 MCP 或其他 spec 解析）
- `TestBuiltInAgentUsesAgentPrincipal`（Invocation 只带 agent principal，不带提问者）

`component/agent/transcript`：

- `TestBuildConversationOrder`（system → checkpoint → 非 ENGINE 消息）
- `TestRepairToolCalls`（悬空/孤儿）
- `TestCutRegionKeepsToolPairsBalanced`
- `TestEstimateTokensMonotonic`

`component/tools/*`：

- `TestMCPToolNamesFrozen`（17 个，名字集合相等）
- `TestMCPInputSchemaParityWithCatalog`（同名工具 schema 一致）
- `TestCatalogReadOnlyExcludesPropose`（MCP 子集不含写工具）
- `TestToolPermissionMatchesRPCAnnotation`（沿用 MCP 现有守卫，扩展到内部 catalog）
- `TestAgentRoleCoversSpecToolPermissions`：DQ 两个 spec 里 `AuthzPermission` 工具的权限集合 ⊆ `workspaceMember` 基线 ∪ `roles/qualityAgent`；新增工具忘了给角色加权限时直接失败（`draft_checks` 是 `AuthzAction` 例外，单独列出）

`agentrunner`：

- `TestPromptMentionsOnlySpecTools`（双向：文本→白名单、白名单→文本或豁免）
- `TestWakeTranscriptGolden`（旧/新引擎对同一脚本化事件流产生相同转录行）
- `TestControlDirectivesAppliedOnce`（block 重复上报、schedule 覆盖）
- 现有 `engine_test.go`/`context_test.go`/`compaction_test.go`/`triage_test.go` 不改断言通过

集成：`mcp_service_test.go`、`data_quality_service_test.go`（agent principal / kill switch / session / transcript）、`data_quality_journey_service_test.go` 全绿。注意 `agent_cli_service_test.go` 测的是 `mxd` CLI 的设备登录，不是 DQ agent，不要把它当成本次重构的回归网。

### 8.2 指标与日志

- 保留 `llm_calls_total`/`llm_duration_seconds`/`llm_tokens_*`（`Feature` 标签）。
- 新增 `agent` 子系统（沿用 `metrics` 的 `namespace`+`subsystem` 约定）：`metaxisdata_agent_tool_calls_total{product,tool,outcome}`、`metaxisdata_agent_tool_call_duration_seconds{product,tool}`、`metaxisdata_agent_tool_result_bytes{product,tool}`、`metaxisdata_agent_wake_total{product,outcome}`、`metaxisdata_agent_prompt_tokens{product}`。
- 标签基数遵守 [metrics.md](metrics.md)：`tool` 是有限静态集合（catalog 名字），`product` 是 spec 名；不要用 session/target GUID 做标签。
- 结构化日志：`session`、`wake`、`tool`、`call_id`、`outcome`、`duration_ms`、`truncated`；错误只记 code + 摘要，不记 prompt/工具正文。

### 8.3 文档门禁

- 实现落地后：`data-quality.md` 的 Where things live 与 §3.9h/§3.9i 更新；`mcp-server.md` 的工具表与"工具实现"决策更新；`backend/AGENTS.md` 增加包边界与 import 规则；本文收缩为维护参考。
- 每个阶段 PR 的验收清单包含"文档是否仍准确"。

---

## 9. 开放问题

- **Q1 转录表是否泛化？** 若第二个产品需要持久长会话（ExplainSQL 多轮、元数据治理），再评估 `agent_session`/`agent_message` 表与迁移；本轮用端口隔离，不预设。
- **Q2 MCP 是否要开写？** 本轮明确不开。若未来要开，需要先设计"工具级确认/审批"语义（DSH 用 `ask` + approval；MCP 用 MRTR），并与 DQ 的 `requires_approval` 对齐。
- **Q3 工具 schema 的单一真相？** 短期：手写 JSON Schema + parity 测试（MCP 现状）；长期：Go struct tag/生成器统一生成 schema（需评估与 proto 的关系，避免第三套 DSL）。
- **Q4 工具包粒度？** 目标态 3 个域包 + agent/tool。若 metadata 膨胀（17→更多），再按子域拆文件而非包。
- **Q5 身份传递？** 内置 agent 的 principal 必须显式放进 `Invocation`，不从 ctx 猜；`context.WithValue(common.UserContextKey)` 仅用于让现有 handler 读到调用者（MCP 已有此做法）。
- **Q6 动作策略与 kill switch 的关系？** 保留两个输入（工作区 `agent_autonomy_enabled` 与 agent `enabled`），由管线读取后传给 `DecideAction`；不合并字段，避免改变设置语义。
- **Q7 `draft_checks` 怎么授权？** 走 `suggest` RPC 并给 `roles/qualityAgent` 加 `dataQuality.suggest`（显式、可审计，但改 I-71 的"角色只有两项写权限"），还是保留 `Suggester` 组件调用 + `AuthzAction` + 新动作卡（不扩大角色，但能力不在 RPC 审计面）。倾向前者，需要产品决策（§3.5.1）。
- **Q8 SERVICE_ACCOUNT 是否继承 `workspaceMember` 基线？** 当前继承，导致 agent token 也能调 manual SQL 等基线 RPC。严格最小权限需要在 IAM 层区分 agent 基线，属于平台级变更，建议独立立项（§3.5.1）。
- **Q9 人类提问的可见性边界？** 单租户下所有成员可读全部，所以"提问者能否看到工具结果"目前不构成问题；若未来引入工作区/资源级可见性，需要在工具结果返回前加一道"requester 可见性"检查，而不是把提问者权限交给 agent（与 D13 一致）。

---

## 附录 A：现有全部工具清单

### A.1 MCP（17，只读）

见 §1.3 表。补充：全部 `ReadOnlyHint=true`、`DestructiveHint=false`、`OpenWorldHint=false`；每次调用（含被拒）审计；`whoami` 无权限注解，列在 `methodsWithoutAPermission` 白名单。

### A.2 DQ agent（10）

见 §1.3 表。

### A.3 ExplainSQL（2）

| 工具 | 参数 | 现在实现 |
| --- | --- | --- |
| `get_object_schema` | `name`（可限定） | `explain_sql_service.go`：store 查 `meta_registry_resource` → `llm.BuildContextFromMetadata` |
| `search_objects` | `keyword` | 同上：`store` 搜索 → 投影 |

迁移后：由 `component/tools/metadata` 提供（复用 `mcp/ref.go` 的名字寻址与 `get_metadata`/`get_ddl` 的实现），ExplainSQL 的 spec 白名单 = `search_metadata` + `get_metadata`（+ 需要时 `get_lineage_graph`）。顺带清理 `explain_sql_service.go:592-630` 为旧测试名保留的 provider-resolution 包装（直接调 `llm.FindConfig`/`ResolveConfig`），避免抽 core 后出现第二套解析入口。

### A.4 无工具 LLM 路径（保持）

| 能力 | 入口 | 是否可改用 core |
| --- | --- | --- |
| L1 规则草稿 | `SuggestQualityRule` / `CreateQualitySuggestionJob` → `dataquality.Suggester` | 可（`agent.Loop` 单 turn 无工具） |
| L2 失败解释 | `ExplainQualityExecution` | 可 |
| L3 标注建议 | `SuggestColumnLabels` | 可 |

---

## 附录 B：包依赖规则（供 `backend/AGENTS.md` 增补）

```
llm            → 标准库、store（仅 registry/debug）、metrics
agent          → 标准库、llm、agent/tool（接口）、metrics
agent/tool     → 标准库、common、permission、store（仅 Principal 类型）、metrics
agent/transcript → 标准库、llm
tools/*        → agent/tool、proto、领域组件；只声明 service 接口，不 import api/v1
mcp            → agent/tool、tools/*、service 接口、api/auth、api/oauth、store
agentrunner    → agent、agent/tool、agent/transcript、tools/quality、dataquality、store
api/v1         → agent（产品 spec 的 RPC 入口）、tools/*（实现接口）；不 import agentrunner 的内部类型（除 Waker 接口）
server         → 以上全部（唯一装配处）
```

禁止：`agent`/`agent/tool`/`agent/transcript` import `storepb`、`dataquality`、`mcp`、`api/v1`；其中 `agent`（循环/prompt/事件）连 `store` 也不 import，只有 `agent/tool` 为 `Principal` 类型允许 import `store`；`tools/*` import `mcp` 或 `api/v1`；`mcp` import `agentrunner`。

---

## 附录 C：与"直接照搬 DSH"的差异一句话总结

| 维度 | DSH | 我们 |
| --- | --- | --- |
| 扩展模型 | 运行期插件（Cordis） | 编译期静态注册 |
| 包数量 | ~60 | 6–9 |
| 工具来源 | 插件 + MCP 外部工具 | 自有 catalog，两个前端 |
| 会话 | 通用事件日志 + projection + 多后端 | DQ 表 + 端口适配（先不泛化） |
| Prompt | 多段 section + 动态 context 快照 + 多 provider 更新机制 | 分节 + 一次全量请求 |
| 多 agent | subagent/workflow/team | 本轮不做 |
| 呈现 | Client 插件 | 现有 Vue 组件读 transcript |

**结论**：借 DSH 的六个机制，不借它的运行时框架。我们的目标不是成为 agent 平台，而是让"给 agent 加一项能力"和"加一个 agent 产品"这两件事变成廉价的、可测试的、不会破坏既有契约的操作。
