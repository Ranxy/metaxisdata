# Agent 平台（Agent Runtime × Tool Catalog × Product Preset）— Reference

> Status: **implemented**。维护参考，覆盖 `backend/component/agent/`、`backend/component/agent/tool/`、`backend/component/agent/transcript/`、`backend/component/tools/{metadata,lineage,quality}/`、`backend/mcp/`、`backend/component/dataquality/agentrunner/`、`backend/api/v1/explain_sql_*.go`。
> 关联：[data-quality.md](data-quality.md)（DQ agent 会话/纳管/动作目录/不变量 I-71…I-84）、[mcp-server.md](mcp-server.md)（17 个只读工具的线上契约）、[iam-and-permissions.md](iam-and-permissions.md)、[rate-limiting.md](rate-limiting.md)、[metrics.md](metrics.md)。
> 设计背景与取舍记录在 git 历史与本文件的 §5；实现分期为 P0–P4（见 §6）。

## 1. 它是什么

内置 AI agent 拆成三层，工具不再属于某一个 agent：

| 层 | 位置 | 职责 |
| --- | --- | --- |
| Agent runtime | `component/agent/` | turn/流式循环、事件、工具批调度（只读并行）、会话预算、prompt 分节（`agent.Prompt`）、指标 |
| Tool catalog | `component/agent/tool/`（契约）+ `component/tools/{metadata,lineage,quality}/`（能力） | 工具定义 + 执行 + 策略元数据；一条执行管线；被内置 agent 与 MCP 两个前端消费 |
| Product preset | `component/dataquality/agentrunner/`、`api/v1/explain_sql_*.go` | 产品名、prompt、工具白名单、turn 预算、wake/会话编排、确定性回退 |

依赖方向：MCP/product → catalog → runtime → `component/llm`（仅 provider 传输）。`api/v1` 实现 catalog 声明的 service 接口，由 `server.go` 注入；catalog 不 import `api/v1`。

## 2. 工具契约与执行管线

`tool.Tool = Name + Description + Parameters(JSON Schema) + Meta + Execute(ctx, Call, *Invocation) (Result, error)`。

`Meta` 的关键声明（注册时校验，违规是启动错误）：

- `Scope`：`ScopeCatalog`（MCP 与内置 agent 都可消费）或 `ScopeProductOnly`（仅产品层，进不了 MCP）。
- `Authz`：`AuthzPermission`（RPC 支撑，管线用调用者自己的 principal 查 IAM）、`AuthzAction`（组件调用，动作策略把关）、`AuthzInternal`（agent 簿记/控制，仅产品层、不做 IAM）。
- `ReadOnly`、`SideEffect`（None/Propose/Publish）、`Permission`、`Action`、`Timeout`、`MaxResultBytes`、`ParallelSafe`。

执行顺序（`Set.Execute`）：查表 → 身份 → IAM（`AuthzPermission` 且 Invocation 带 Checker）→ 动作策略（`Meta.Action` 非空且带 Policy；`RequiresApproval` 也拒绝）→ 超时/panic 隔离 → 结果渲染与 `MaxResultBytes` 截断 → 审计（仅 `SideEffect != None`）→ 指标（Observer）。

- 错误是结构化的 `tool.Error{Code,Message,Hint,Candidates,Retryable}`，模型可见、wake 不会因普通工具失败而中止。
- `Invocation` 显式携带 Principal/Workspace/Checker/Policy/Observer/Audit，工具不读进程内隐式状态。
- `MaxResultBytes=0` 表示不截断；DQ 工具显式 8 KiB（与历史行为一致）。

## 3. 前端与产品

### MCP（`backend/mcp/`）

- 17 个只读工具的定义、鉴权、限流、逐调用审计与 wire 形状都在 MCP 层；工具体全部委派给 `component/tools/{metadata,lineage,quality}`。
- 线上契约冻结：工具名/输入 schema/输出投影/错误码/details 由 `tool_test.go`、`tool_guard_test.go`、`tool_schema_test.go`、`sdk_contract_test.go` 钉住；`tool.go` 现在只剩定义与投影辅助。
- 每次 dispatch 记录工具指标 `product="mcp"`；只读，无写工具（D-91 不变）。

### 内置 DQ agent（`agentrunner`）

- 两个 spec（进程级单例）：`quality-monitor`（8 工具、8 turns）与 `quality-triage`（+`get_table_context`/`draft_checks`、12 turns）；`specFor(mode)` 选择，UNSPECIFIED 落 monitor。
- `systemPrompt` 由 `agent.Prompt` 的 `identity`/`long-session-rules` 两节渲染；`spec_test.go` 用独立字面量钉住渲染结果，并检查 prompt/wake 文案里出现的工具名必须在该 spec 白名单内。
- 每次 wake 构造一个 `tool.Invocation`：principal 是 session 的 agent SERVICE_ACCOUNT（**不是**唤醒者）；wired checker = `iam.Manager`；Policy = `sessionActionPolicy`（`DecideAction` + 目标真实 criticality + 工作区 autonomy 开关）；Observer 打 `product=spec.Name`；Audit = `server.agentToolAuditSink`。
- 控制类工具（`block_session`/`schedule_wake`）是产品层 `AuthzInternal` 工具，写 `wakeControl` 值，由 wake 编排在批次后统一应用。
- 只读工具标 `ParallelSafe`，生产每次 wake 最多 4 个只读调用并发；事件与对话仍按模型顺序提交。
- 无模型时确定性巡检/事件调查保留（不经过工具层）。

### ExplainSQL（`api/v1/explain_sql_*.go`）

- 已运行在 agent runtime 上，工具经共享 `tool.Set` 管线；wired checker（`iam.Manager`）与 Observer（`product="explain_sql"`）。
- 工具本身仍是 ExplainSQL 专有的 2 个（`get_object_schema`/`search_objects`），**尚未**迁移到共享 metadata/lineage 工具——见 §5 deferred。

## 4. 会话与转录

- `component/agent/transcript/`：存储无关的 `Entry`/`Role` + `BuildConversation`（system + checkpoint + 非 ENGINE 消息）、`RepairToolCalls`、`EstimateTokens`、`PruneToolResults`、`RetainedCut`/`BalancedCut`/`PlanCompaction`。
- DQ 的 `storepb.QualityAgentTranscriptMessage` ↔ `Entry` 映射只在 `agentrunner/session_port.go`；核心不 import storepb/store。
- 压缩的摘要调用、checkpoint 提示词、落库与 `summary_through_seq` 水位仍在产品层。
- 表未泛化：P0–P4 无迁移；第二个需要长会话的产品再决策（见 §5）。

## 5. 决策与已知限制

- **单 module、静态装配**：无运行期插件/DI/HMR；工具在 `server.go` 启动时注册，spec 白名单 boot 时解析，未知/重复工具是启动错误。
- **不继承调用者权限**：内置 agent 一律以自己的 principal 执行（避免 confused deputy）；人类提问/建会话只是归因。
- **IAM 在管线、动作策略在拥有事实的层**：`AuthzPermission` 写工具的 handler 内策略保留（RPC 路径）；内置 agent 的 propose 工具不调用 handler，因此在管线执行同一 `DecideAction`（单一判定点）。
- **审计**：MCP 每次调用一行；内置 agent 只审计 `SideEffect != None` 的工具（读不审计，避免把每日巡检读放大成永久账本）。行 method 为 `agent/tools/call:<tool>`，actor 是 agent principal。
- **ExplainSQL 工具目录迁移 deferred**：会改变模型可见的工具名/schema 与前端进度展示；收益仅是消除 2 个工具实现的重复。需要先有 ExplainSQL 行为回归网（golden/集成）再迁移。
- **transcript 表泛化 deferred**：若出现第二个持久长会话产品（ExplainSQL 多轮、元数据治理），再评估 `agent_session`/`agent_message` 与迁移。
- **MCP 写工具不开放**；agent 的 `Publish` 副作用档位当前没有暴露路径。
- **单副本/调度语义不变**：lease/claim、runner 生命周期、事件接入均未迁移到 runtime。

## 6. 实施分期（已落地）

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| P0 | `agent/tool` 契约 + 注册表 + 管线；ExplainSQL 与 DQ 工具接入 | done |
| P1 | `llm` 拆传输层；`agent`（loop/events/prompt）；`agent/transcript` | done |
| P2 | metadata/lineage/quality 工具包；MCP 17 工具全部委派，wire 冻结 | done |
| P3 | monitor/triage spec、控制工具返回值、IAM 按 principal、动作策略在管线 | done |
| P4 | 工具级指标（agent + MCP）、只读工具并行；ExplainSQL catalog 迁移 | 指标/并行 done；迁移 deferred（§5） |

## 7. Where things live

| 面 | 位置 |
| --- | --- |
| 工具契约/注册表/管线 | `component/agent/tool/{tool,registry,execute}.go` |
| Loop/事件/prompt | `component/agent/{loop,event,prompt}.go`、`schema.go`（ToolDefs） |
| 转录/压缩 | `component/agent/transcript/` |
| 工具能力 | `component/tools/metadata/`（寻址、错误信封、metadata 工具）、`component/tools/lineage/`、`component/tools/quality/` |
| MCP 传输 | `backend/mcp/{server,tool_*}.go`（定义/鉴权/审计/指标） |
| DQ 产品 | `component/dataquality/agentrunner/{spec,prompt,toolset,session_port,wakecontrol,engine,tools,triage}.go` |
| ExplainSQL | `api/v1/explain_sql_{service,tools,context}.go` |
| 服务端装配 | `backend/server/server.go`（engine+IAM+policy+audit+parallel）、`backend/server/grpc_routes.go`（MCP、ExplainSQL checker） |
| 指标 | `backend/metrics/agent.go` |
| 关键测试 | `agent/tool/*_test.go`、`agent/parallel_test.go`、`agentrunner/{engine,spec,context,compaction,triage}_test.go`、`mcp/{tool,tool_guard,tool_schema,tool_metrics}_test.go`、`metrics/metrics_test.go`（golden） |
