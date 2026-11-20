# Built-in agents: runtime, tool catalog, product presets — Reference

> Cross-cutting map for the built-in AI agent stack: `backend/component/agent/`, `component/agent/tool/`, `component/agent/transcript/`, `component/tools/{metadata,lineage,quality}/`, `backend/mcp/`, `component/dataquality/agentrunner/` and `api/v1/explain_sql_*.go`. Product behavior of the quality agents (identity, sessions, enrollment, work-session reads, invariants) belongs to [data-quality.md](data-quality.md); the MCP wire contract belongs to [mcp-server.md](mcp-server.md). Also related: [iam-and-permissions.md](iam-and-permissions.md), [rate-limiting.md](rate-limiting.md), [metrics.md](metrics.md).

## What it is

The built-in AI agent is split into three layers, so a tool does not belong to one agent:

| Layer | Location | Responsibility |
| --- | --- | --- |
| Agent runtime | `component/agent/` | Turn and streaming loop, events, tool batching (read-only calls in parallel), conversation budget, prompt sections (`agent.Prompt`), metrics |
| Tool catalog | `component/agent/tool/` (contract) and `component/tools/{metadata,lineage,quality}/` (capabilities) | Tool definition, execution and policy metadata, one execution pipeline |
| Product preset | `component/dataquality/agentrunner/` and `api/v1/explain_sql_*.go` | Product name, base prompt and base tool set (capability profiles compose on top), turn budget, wake/session orchestration, deterministic fallback |

Dependency direction: MCP → catalog; the built-in agent products → runtime + catalog; ExplainSQL → runtime + catalog; runtime → `component/llm` (transport only). MCP does not import the runtime. `api/v1` implements the service interfaces the catalog declares and is injected by `server.go`; the catalog does not import `api/v1`.

## Tool contract and execution pipeline

`tool.Tool` is `Name + Description + Parameters (JSON Schema) + Meta + Execute(ctx, Call, *Invocation) (Result, error)`.

`Meta` declares, and registration validates (a violation is a startup error):

- `Scope`: `ScopeCatalog` (consumable by MCP and any product) or `ScopeProductOnly` (product layer only, never exposed over MCP).
- `Authz`: `AuthzPermission` (an RPC/tool capability; the pipeline checks IAM with the caller's own principal) or `AuthzInternal` (agent bookkeeping and wake control, product-only, no IAM). There is no action-policy field and no second policy layer.
- `ReadOnly`, `SideEffect` (None/Propose/Publish), `Permission`, `Timeout`, `MaxResultBytes`, `ParallelSafe`.

Execution order (`Set.Execute`): registry lookup → identity → IAM when `AuthzPermission` and the invocation carries a checker → per-tool timeout and panic isolation → result rendering and `MaxResultBytes` truncation → audit (only when `SideEffect != None`) → metrics (Observer). RPC handlers serve humans, so the ACL interceptor is the boundary there; an in-process agent's write tools run with the agent principal through the same IAM.

- Errors are structured `tool.Error{Code, Message, Hint, Candidates, Retryable}`; code, hint and candidates feed audit, UI and programmatic callers. The built-in agent renders only `Message` to the model (`"error: " + Message`), while MCP puts the full envelope in an `isError` result. An ordinary tool failure does not end a wake.
- `Invocation` carries Principal/Workspace/Checker/Observer/Audit explicitly; a tool never reads ambient process state.
- `MaxResultBytes = 0` means no truncation; the quality tools set 8 KiB explicitly.

## Surfaces and presets

**MCP** (`backend/mcp/`): 17 read-only tools (8 of them quality tools). The definitions, authorization, rate limiting, per-call audit and wire shape live in the MCP layer; every tool body delegates to `component/tools/{metadata,lineage,quality}`. The wire contract is frozen and pinned by `tool_test.go`, `tool_guard_test.go`, `tool_schema_test.go` and `sdk_contract_test.go`; `tool.go` holds only the definitions and projection helpers. Each dispatch records tool metrics with `product="mcp"`. Read-only by design: no write tool is exposed.

**Built-in quality agents** (`agentrunner`): three process-level specs — `quality-assistant` (front-office conversation), `quality-monitor` (one long-lived target session) and `quality-triage` (coverage work on an uncovered target) — with turn budgets 8, 8 and 12. `specForSession` sends a front-office session to the assistant and a target session by its mode to monitor or triage (UNSPECIFIED falls to monitor). Each spec declares only its product base prompt and base tools; a wake's prompt and tool list are composed from that base, the selected capabilities and the adoption configuration, in a fixed capability order, and each capability contributes a product-layer `capabilityProfile` fragment and its tools (static code, not a runtime plugin layer). The product tools are `ScopeProductOnly`, the wake controls (`block_session`, `schedule_wake`) are `AuthzInternal` and write a `wakeControl` value that the wake orchestration applies after the batch. `renderSystemPrompt` renders the base sections, the capability sections, the `adoption-policy` section derived from the agent's configuration and the per-agent language contract; each wake carries a short language reminder because the wake instructions are English. `ValidateSpecs` is the boot gate: every catalog capability has a profile, every profile names a catalog capability, every product's base plus profile tools register and resolve, and the engine has an execution branch for each. `spec_test.go` pins the base text against independent literals and the composition matrix (which capability contributes which fragment and tool to which product); `owneradoption_test.go` pins the capability/adoption schema gate and the execution refusal that keeps `adopt_pending_drafts` from writing on a scheduled or event wake. Each wake builds a `tool.Invocation` whose principal is the session agent's SERVICE_ACCOUNT (**not** the person who woke it), with the `iam.Manager` as checker, no action policy, an Observer labelling `product=spec.Name`, and the agent audit sink. Parallel-safe reads run up to four concurrently per batch; events and conversation still commit in model order. With no model available the deterministic patrol and event investigation remain (they bypass the tool layer).

**ExplainSQL** (`api/v1/explain_sql_*.go`): runs on the agent runtime (`agent.RunLoop`), with tools going through the shared `tool.Set` pipeline, the `iam.Manager` as checker and an Observer labelling `product="explain_sql"`. Its two tools (`get_object_schema`, `search_objects`) are defined and executed in `component/tools/metadata/explain.go`, keeping the model-visible names, schema and compact JSON; `explain_sql_tools.go` keeps only per-request scope resolution and the metrics observer.

## Sessions and transcripts

`component/agent/transcript/` is storage-agnostic: `Entry`/`Role`, `BuildConversation` (system + checkpoint + non-ENGINE messages), `RepairToolCalls`, `EstimateTokens`, `PruneToolResults`, `RetainedCut`/`BalancedCut` and `PlanCompaction`. The mapping between the DQ `storepb.QualityAgentTranscriptMessage` and `Entry` lives only in `agentrunner/session_port.go`; the core imports neither `storepb` nor the store. The compaction summary call, checkpoint prompts, persistence and the `summary_through_seq` watermark stay in the product layer. Model reasoning is display-only: it is carried on the completed assistant message, persisted on the transcript row when the provider returns it, and deliberately kept out of `Entry` and the provider request. The table is not generalized; a second product needing long sessions is the trigger to decide.

## Decisions and known limits

- **Single module, static assembly**: no runtime plugins, DI or HMR. Specs and capability profiles are static declarations, but each wake composes them for the agent's selected capabilities and adoption configuration, so a configuration change takes effect on the next wake with the stored SYSTEM row rewritten in place; `agentrunner.ValidateSpecs` validates the composition at startup (registration, resolution, an execution branch per declared tool). MCP and ExplainSQL build and resolve their own tool sets per request, with no process-level registry.
- **No caller-permission inheritance**: a built-in agent always executes with its own principal (avoiding the confused-deputy problem); the human who asked or owns the conversation is attribution only. Owner-requested adoption is the one write that executes a person's decision, and it is authorized by the product's gate (the owner's question in the current wake, adoption enabled, database scope and the daily cap) instead of inheriting that person's permissions; the ledger and audit row name the requesting owner.
- **IAM lives in the pipeline**: permission-gated write tools keep their handler-level policy on the RPC path, while an agent's tools use the agent principal through the pipeline (`roles/qualityAgent` holds only quality rules get/list, suggest and plans.update); there is no second action policy. Owner-requested adoption is the one `SideEffectPublish` tool: its pipeline permission mirrors the RPC's `dataQuality.agents.get`, but the boundary is the product gate — the owner's question in the current wake, the shared adoption readiness predicate, database scope, the daily cap and rule validation — and every adoption is ledgered and audited.
- **Audit**: MCP records one row per call; the built-in agent audits only tools with `SideEffect != None` (reads are not audited, so daily patrols do not inflate the permanent ledger). The method is `agent/tools/call:<tool>` and the actor is the agent principal.
- **Transcript tables deferred**: generalize `agent_session`/`agent_message` with a migration only when a second persistent long-session product appears. **No MCP write tool**; the `Publish` side-effect level is exposed only by the built-in quality agent's owner-requested adoption tool, never over MCP.
- **Single replica and scheduling are unchanged**: lease/claim, runner lifecycle and event ingestion did not move into the runtime.
- **No external agent surface**: the 2026-11 refactor removed the agent bearer token, rotation RPCs, external write entry points and action policy. The built-in per-agent runtime is the only agent form, and MCP is the only external (read-only) integration surface.

## Where things live

- Tool contract/registry/pipeline: `component/agent/tool/` (`tool.go`, `registry.go`, `execute.go`); loop/events/prompt in `component/agent/`; transcript in `component/agent/transcript/`; tool bodies in `component/tools/{metadata,lineage,quality}/`.
- MCP transport and wire: `backend/mcp/`; quality-agent product and specs: `component/dataquality/agentrunner/{spec,capability,prompt,toolset,session_port,wakecontrol,engine,triage,conversation}.go`; ExplainSQL: `api/v1/explain_sql_{service,tools,context}.go`.
- Assembly: `backend/server/server.go` (engine + IAM + audit + parallel limit) and `server/grpc_routes.go` (MCP, ExplainSQL checker); metrics in `backend/metrics/agent.go`. The registry, not this file, is the authoritative tool list.
- Tests: `component/agent/tool/*_test.go`, `agent/parallel_test.go`, `agentrunner/{engine,spec,context,compaction,triage,conversation,owneradoption}_test.go`, `mcp/{tool,tool_guard,tool_schema,tool_metrics}_test.go`, `metrics/metrics_test.go` (golden labels).
