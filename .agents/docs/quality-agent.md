# Quality Agent — Reference

The built-in quality agent is the product's own operator: one agent belongs to an owner, watches the databases and
targets delegated to it, drafts checks for what is uncovered, and works through the checks that cannot settle
themselves. It is not an API subject — it holds an internal principal with no token and no external write API — and
what it may do on a target comes from the delegation recorded for that target, never from having written the rule.

This page is the runtime: identity and configuration, the runners, the model, the composed prompt and tool set, the
writes it may make, the work items it keeps, the delegated authority behind every write, adoption, escalations and the
owner conversation. The checks it creates and the executions it reads are ordinary quality rules and executions;
[data-quality.md](data-quality.md) describes their semantics, and the operator-facing behavior is in
[docs/data-quality.md](../../docs/data-quality.md).

## Identity and configuration

A quality agent is an internal identity, not an API subject: one `quality_agent` row holds identity and runtime configuration (state, owner, language, provider profile and model, database scope, capabilities `suggest`/`adopt`/`patrol`, report schedule, adoption guardrails, budgets) linked to a `SERVICE_ACCOUNT` principal used only as the IAM and audit actor. The principal never appears in the user directory or owner pickers, no token is issued, and there is no external agent write API. The only external surface is the read-only MCP tools. The workspace has a single switch, `agent_runner_enabled`; per-agent state and budgets govern individual agents.

## The runners

Three runners carry the runtime. `qualityenrollmentrunner` (60s) advances the enrollment queue and scans each in-scope database every 15 minutes against the registry digest; enrollment rows are `PENDING`, `ACTIVE`, `DORMANT`, `OUT_OF_SCOPE`, `GONE`, each agent+target pair one row; scope exit blocks the session as out-of-scope, agent disablement blocks it as agent-disabled, both self-heal when the cause disappears, and an object that disappears from a fully scanned database is marked gone and its session archived. Enrollment creates the only target sessions; there is no product RPC to create, pause, archive or wake one. `qualitysessionrunner` (3s tick) claims due sessions under a lease (15m lease, 10m wake), appends a user message, rebuilds the model input from the transcript after the compacted summary point, and either continues the conversation with the model or runs a deterministic patrol/event review. The compaction pressure threshold is derived from the pinned model's context window (`floor(min(W×0.8, W−O−B))`, headroom `min(65536, W/8)`), so a model change also moves when its conversation compacts: old tool results are trimmed first, then the oldest tool-pair-balanced region is summarized, the system prompt is never compacted, and transcripts are kept. `qualityagentrunner` (60s) generates each agent's scheduled DAILY/WEEKLY report — scope health, targets and coverage, enabled checks, period verdict counts, new/open issues, pending proposals and a markdown body — aggregated over that agent's enrollment target set rather than a database prefix, idempotent by `(agent_id, period_key)`, and appends a "report ready" message to the conversation.

## Model

An agent's model is one stored pair: `provider_profile` (the resource name) and `provider_model`. Both are required at write, and a changed pair is validated against the profiles that currently enable that model, so the choice cannot be a profile whose model the server would have to infer — one profile can enable several models at different prices and quality, and the model a wake used must be the model an administrator named. Validation runs only when the pair changes: an edit that leaves a stale pair alone (a rename or another agent field) is accepted, because the runtime already tolerates a stale pair and refusing the edit would make the agent unmanageable. Resolution is per wake, and the resolved configuration — credentials included — is never snapshotted into the session: `Suggester.ResolveAgentConfig` reads the registry, applies the workspace `enable_llm_analysis` gate, and returns the exact pair. The workspace allowlist (`allowed_llm_provider_models`, exact profile+model pairs) restricts Explain SQL only; the analyses and agents choose their pair in their own settings. There is no fallback to the workspace analysis pair and none to another model: an empty pair or a model the profile no longer enables leaves the agent without a model. That shows up as the deterministic conversation, an *unavailable* marker in the settings dialog, and `needs-human` for a triage session that was already admitted; a target that needs triage for the first time is not admitted at all and its enrollment row records `no-model`, which no product read exposes and which raises no owner notification. Because the registry is a process-local cache with a short TTL, a profile changed on another replica is visible to write-time validation and to wakes only after that window. The same resolution serves the wake and its compaction summary, `draft_checks` (which finds the agent by the principal the write is attributed to) and enrollment's triage admission; no agent path may pick a provider of its own.

## Composed prompt and tool set

The prompt and tool set are composed, not hand-written per product: a product spec (`quality-assistant` conversation, `quality-monitor` target session, `quality-triage` coverage) supplies the base prompt and tools, and each selected capability adds a product-layer profile with its prompt fragment and, optionally, tools. `suggest` adds `get_table_context`/`draft_checks` to both target products — a monitoring wake whose check cannot express the intent drafts the one that can — and `list_pending_proposals` to the conversation, `adopt` adds `adopt_pending_drafts`, and `patrol` adds prompt guidance only because reports stay runner-generated. The composition is a pure function of product, capabilities, adoption configuration and language, so the SYSTEM row and tool schema stay byte-stable between config changes and provider prefix caching survives; at startup every product tool's declared permission must be one the agent principal can actually hold (the member baseline or the quality agent role), because a tool gated by a permission no role grants would fail for every agent at call time; a configuration change rewrites the SYSTEM row in place and appends a one-time `[configuration change]` note to the next wake instruction, with no forced compaction and append-only history. Every tool goes through the in-process pipeline, which enforces IAM and audits writes.

## What the agent may write

Four tools are publish-class. `adopt_pending_drafts` creates a check: its schema is present whenever the `adopt` capability and adoption configuration are ready, but it is executable only on a wake that answers an owner question, under the readiness predicate below, with scope, the daily cap, rule validation and audit still applying. `run_check` executes the check a diagnosed case is about, now, through the ordinary manual-run path, so a failure can be reproduced or a repair verified without waiting for the next scheduled slot; the case waits for the verdict like any other verification. `control_check` pauses a check whose case says it cannot run: the server re-reads the case, the participation level, the pins and the diagnosis, refuses a check that can produce a verdict, writes the state with an optimistic revision and escalates the case, so the check stops failing while a person decides whether to resume, archive or replace it. `repair_check` applies the one deterministic repair a diagnosis names: it re-reads the case, re-derives the diagnosis, and writes the value the registry owns — today the column snapshot of a `schema_drift` check that reports the target's structure changed. The model supplies only the case, because a value it supplied would let it pin a snapshot that never existed; the patch is validated and compiled first, written as one compare-and-set against the revision the decision was based on, and the check's own fingerprint moves with it so the next run does not report the drift it just settled. The verb plans the run that verifies the repair, because a repair is only a repair once a run proves it, and refuses a paused check outright: a paused check never runs, so the case would wait for evidence that cannot arrive. A criterion is a separate case. At `repair` level the verb refuses it, because changing what a check accepts is what the delegation withholds, and it goes through `propose_revision`; at `tune` level the verb accepts it only with a replay: the recent runs are judged again under the new definition, and a change that would turn a failing run — or a run that reached no verdict — into a pass is refused and has to be proposed instead. A replay needs evidence to replay, so a check with no judged run leaves a criterion change unverifiable, and unverifiable is a proposal rather than a write. `propose_revision` is the verb for every change the delegation withholds — a criterion, a field someone pinned, a value on a check a person manages — and it writes no check: the server re-derives the diagnosis, refuses a field the diagnosis did not name, validates the proposed value with the same pipeline a save runs (without a probe, because a proposal asks for a decision rather than starting a run), records the field, the value, the effect and the reason on the escalation, and moves the case to `ESCALATED`. The change travels as a payload rather than as prose, so the owner reviews a field and a value instead of a summary of them. The participation level, the pins and the replay gate deliberately do not refuse a proposal: they withhold the write, and handing the decision to a person is the alternative they name.

### The replay

The replay is the only thing between a `tune` delegation and "turn the red green". It re-judges the check's recent completed
executions (at most twenty, newest first) with the same pure evaluator the runner used, rebuilding each observation from the row
itself: the value it measured, the row counts it came from, the baseline a comparison used, and — for freshness — the fact that a
timestamp existed, since the lag it compared is the recorded value. A verdict that is less alarming than the recorded one is a
loosening: a `FAIL` that becomes a `PASS` is the obvious case, and so is a `NOT_EVALUATED` that becomes one, because "no verdict" is
not a pass. A run that recorded no observation (`ERROR`, `SKIPPED`, or one that never finished) has nothing to re-judge and is not
counted, so an unjudged check cannot be "verified" by an empty replay: no replayable run means unavailable, and unavailable means the
change stays a proposal. The dossier and the verb ask the same question — the dossier reads whether any run can be re-judged, the
verb replays the actual change — so the action list a model reads cannot offer what the write path refuses.

## Escalations

An escalated case raises one attention for the owner — deduped per case, carrying the target's session when there is one, the case it unblocks, and, for a proposal, the change itself — and notifies the owner or the administrators when the agent has none — because a decision nobody is told about is not a decision. The same escalation is raised by the runner when a case **freezes**: the agent spent its repair budget and the check still fails the same way, which is the agent saying it cannot fix this, and nothing in the case table tells a person that on its own. Both writers go through one escalation package, so the dedupe key, the session the item hangs off, who is notified when the agent has no owner, and the owner's language cannot drift between them. There is no publish, edit, archive, resume, settings or reject tool, and MCP exposes reads only.

Attention items (`BLOCKED`, `PROPOSAL_PENDING`, `REPORT_ANOMALY`) are the owner's to-do list: they send an inbox notification to the owner (administrators when the agent has no owner), a reply restores the target session and resolves the item, and dismiss only closes it. A reply can also ask the agent to adopt pending drafts. A target-level attention carries the target and its session, so the home list leads with the target, expands inline to up to five pending draft titles, and replies through the same target session; a report anomaly is agent-level and has no target. A pass or a person's decision that empties a target queue resolves its proposal attention.

## Work items (cases)

A case is the agent's durable work item, separate from the issue ledger: a FAIL is a conclusion about the data and becomes an issue, while a check that **cannot** produce a verdict (a refused bound, an unsupported column, an empty target, a missing baseline) becomes a case. The server opens it — never the model — in `DIAGNOSED` with the class the deterministic diagnosis computed, and one open case claims a subject (agent plus target, rule and kind) through a partial unique index, so a retried batch cannot open two. States are `OPEN`/`DIAGNOSED`/`ACTED`/`AWAITING_VERIFICATION`/`RESOLVED`/`FROZEN`/`ESCALATED`; the last three are terminal and release the subject, because a frozen case that kept its subject would make the check permanently invisible to the agent. A later execution that produces a verdict closes the open case, so the table holds current work rather than history. A case with a repair pending is verified by the next execution of its check: a verdict closes it, the same class spends one attempt and freezes at the budget (two by default), a different class re-diagnoses without spending one, and a run that has not finished spends nothing. The case's own evidence execution is never taken as its verification run, so a retried batch cannot spend an attempt, and only a case that actually has a repair pending is verified — a still-failing run on a merely diagnosed case must not burn the budget before the agent has changed anything.

The board is the read-only view of that table: `ListQualityAgentCases` lists one agent's work items, open ones first and soonest
action first, with the class, the state, the repair attempts spent and the sentence naming the check. The agent's owner and
administrators may read it. It is a projection, not a second state machine — nothing writes through it, and a person who wants to act
does so through the check itself or by answering the escalation.

## Delegated authority: participation and pins

How far an agent may go on a target is `participation`: `OBSERVE`, `REPAIR` (the delegation default) or `TUNE`. It cascades — the agent row, the database row the operator chose when delegating, then an enrollment-level exception — and an unset slot inherits the level above it rather than resetting, so narrowing one table does not re-declare the rest. The level is per target, so it cannot live in the byte-stable system prompt: the wake instruction states the resolved level and what it permits. A **pin** is the only way to take a check out of the agent's reach, and it is always a person's explicit act: `quality_rule.manual_lock` pins the whole check, `pinned_fields` pins named config paths (a pin on `range` covers `range.min`), and editing a check never creates one. Both are head-only metadata like the frequency tier — they write no revision.

A pin is written by a person through the ordinary rule update (`manual_lock` and `pinned_fields` are two mask paths), on the
check's own row in the checks list: the lock is the button there, and per-field pins are the same metadata through the API. It is
audited like any other rule write, changes no definition, writes no revision, and is revocable in one click. That is deliberate —
the delegation's safety rests on the escape hatch being easier to reach than the thing it guards against.

## Wake rendering and scheduling

The target session's wake instruction renders up to three of its open cases with their class, state and attempt count, and the next wake is the soonest of the engine's answer and the agent's earliest open case action, clamped to a minute so a past-due case cannot make the three-second tick spin. The merge happens twice on purpose: the engine's read covers the cases it rendered, and the lease release re-reads so a case another pass opened mid-wake is not left sleeping until the idle cadence.

## Perception: events and check health

Failure and error executions fan out to every session covering the target, always on, idempotent per execution and capped at 20 events per session per hour. A check that finishes `NOT_EVALUATED` is not a failure and produces no such event, so the batch also asks `dataquality.DiagnoseCheckHealth` about every check it ran: a check that ran without concluding becomes a health event (`NEVER_JUDGED`, `NO_VERDICT_EMPTY`, `STALLED`), one per check per UTC day and on the same hourly budget. The classes are deterministic and read the newest execution first, because an empty target is not a broken check and telling those apart is what the agent needs to stop guessing. A wake without a model consumes a health event as a note instead of parking the session: there is no verdict to review, and parking would silence the target exactly when it reports that it never concludes.

A check that has stopped running is invisible to both of those paths, because it produces no execution at all. The scheduler therefore carries a stall sweep, throttled to a minute on its ten-second tick: it starts from the target's schedule instead of from an execution, compares each enabled check's newest completed run with two of the target's cadences (`DiagnoseStaleCheck`, the same stall rule the batch reader applies) and tells the target's sessions about the checks that have stopped. The event is keyed per check per day and shares the failure path's hourly budget, and it names the check rather than an execution, because there is no execution to name. A check younger than its window is pending rather than stalled, so a rule that joined the schedule a minute ago does not page anyone.

## Owner conversation and transcript

Each agent also has one owner conversation (`quality-assistant`): the owner sends messages (owner-only, hourly cap), the agent replies, and when no model is available it returns a deterministic scope summary. The owner conversation stays product content only; the per-target work sessions are a separate product read (`GetQualityAgentSession`, `ListQualityAgentSessions`, `ListQualityAgentSessionMessages`) available to the agent's owner and administrators. A work-session transcript shows the wake, the assistant answer, model reasoning when the provider returned it, every tool call with arguments and result, runner notes and compaction checkpoints; the system prompt and runner notes stay folded. These reads follow the un-audited read baseline; only `ResumeQualityAgentSession` remains an administrator-only, audited recovery action. The transcript read starts at the newest page, polls with `after_seq`, and walks older history with `before_seq` (the oldest loaded seq) rather than an offset, so a live append cannot shift the page the reader is walking. The transcript window is a client-side reading surface (`useChatScroll`): the target identity and the run state live in its side rail so the messages keep the height, and the window opens on the newest message, follows polled appends while the reader is at the bottom and stops following once they scroll back (a "new messages" control returns them), and scrolling to the top loads the previous page while keeping the visible message in place. Following is anchored to the newest message, so expanding a tool call or a runner note does not drag the reader back. A blocked reason is clamped in the run-state column, which scrolls instead of growing the page, and a short viewport falls back to the shell scroller instead of clipping the transcript.

## Adoption

Adoption has three entry points. The automatic pass is a guardrailed write: an eligible draft from the wake that queued it (adopt and suggest capability, adoption not off, allow-listed type, confidence over the threshold, critical allowed, cap unspent) is created as the agent's principal and owned by its owner, with `PAUSED` or `ACTIVE` deciding the initial state. Eligibility is what that wake created or refreshed, judged against the wake start and `COALESCE(refreshed_at, created_at)`. Skipped or failed items stay pending and raise a proposal-pending attention separating policy skips from write failures; the ledger keeps the error class and a bounded message, and the read model exposes the last 24 hours' adopted, approved and failed counts and last failure. The wake path does not retry drafts above the ceiling, from a failed wake, or with a cleared refresh marker; they stay pending for the next wake, the tool or a person's approval. The owner-requested tool is the model-mediated entry point: `adopt_pending_drafts` exists when the capability and adoption are ready but runs only on an owner-question wake; without draft ids it applies the same guardrails to the target in scope, while named ids are the owner's decision that bypasses the allow-list, confidence and critical switch but never the active agent, owner attribution, scope, daily cap or rule validation; a target session may adopt only its own target, and the conversation must name a target or the drafts. The direct entry point is the owner or administrator approving pending drafts on the agent page: the approval runs through the internal create path, which does not consult the caller's rule-write permission, so the check starts `ENABLED` and the decision is attributed to the person, while the check is owned by the agent's owner (or by the approver when the agent has none). The allow-list, confidence, critical switch and scope are not consulted, and the daily ceiling is not charged. It is ledgered under its own origin (a failed write with its reason), audited in the person's name, bounded per request, and narrowed by an optional target GUID. The draft is claimed and the rule validated through the same create path; a failed create reopens it and an already-decided draft is skipped. The ceiling therefore counts only `AUTO` and `TOOL` adoptions, never an approval. `AgentAdoptionReady` gates the automatic pass and the tool, not the approval. The advisory lock, ledger and audit row are shared; the tool creates no proposal-pending attention because the owner is already in the conversation, and a wake in which it ran skips the automatic post-triage pass. `get_target_summary` returns the last 24 hours' adopted/failed counts, the last failure and the target's pending drafts with id, check type and confidence; `list_pending_proposals` returns those ids too, so the model can name drafts and attribute its own adopted checks.

## Language

The agent's language is one setting (`en-US`/`zh-CN`, empty follows the owner's profile, then English). It is rendered into every system prompt as a language contract, with a short reminder on each wake because the wake instructions are English; the stored SYSTEM row is updated in place on the session's next wake when the language changes, so a long-lived session does not keep the old contract indefinitely. Server-generated prose (report body, report-ready message, attention title/question framing, fallback replies) uses the same `agentlang.ForAgent` decision; a blocked reason reported by the engine/model is shown as written, not translated. Model-written prose is shown as written. Raw transcript internals (system prompt, wake instructions, runner notes and tool names) remain English.

## Memory

Memory holds only confirmed asset facts: agents write them, a fact for the same subject supersedes the older one, and `get_memory` is the only reader.

## Invariants — what must not break

| Rule | Why / guard |
| --- | --- |
| An agent is an internal identity (one row plus an internal principal, no token, no external write API; external consumers get only the eight read-only MCP quality tools), and its sessions are durable: one active session per target, `ENABLED`/`BLOCKED`/`ARCHIVED`, created only by enrollment, readable by the owner and administrators, and resumable only by an administrator | agent/session tests + MCP tool guards |
| An agent's model is one stored `provider_profile` + `provider_model` pair, required at write and validated against the enabled profiles only when it changes; resolution is per wake with no fallback, and a vanished profile or model leaves the agent without a model instead of silently switching | provider config/handler tests + `llm` pair tests + engine tests |
| Every model path of an agent — wake, compaction, triage drafts — and its enrollment admission use that agent's pair through one resolver; the workspace analysis pair never applies to an agent | engine/enrollment/draft tests + wiring review |
| Model reasoning is display-only: it is persisted on the assistant transcript row when the provider returns it, never sent back to the provider, and never counted in the model context | loop/engine tests + conversion test |
| The agent language is one config value used by the system prompt, the wake reminder and server prose; a language change updates the stored SYSTEM row in place on the next wake, and the prompt instructs the model not to translate identifiers | prompt/language tests + agentlang tests + engine test |
| A wake continues the same conversation; the tool whitelist is read, propose (including `propose_revision`, which validates a value, records it on the case's escalation and escalates the case, and never writes a check), wake-control and exactly four publish-class writes: `repair_check` (applies a diagnosis's deterministic repair with a value the registry owns — or, at `tune` level only, a criterion that the replay proves leaves every failing run failing — validated, compiled, probed when the statement changed, written as one compare-and-set with the target fingerprint, case `ACTED` behind a planned verification run), `adopt_pending_drafts` (owner-question wake only, active agent with adoption ready, scoped, capped, audited, refused without the question), `run_check` (executes one check now as a diagnostic, authorized with `rules.run` on the target) and `control_check` (pauses a check that cannot produce a verdict, re-checking the case, participation, pins and diagnosis server-side, writing with an optimistic revision and escalating the case). There is no publish, edit, archive, resume, settings or reject tool; failure events are idempotent and capped | engine/context/owner-adoption/control tests |
| Compaction trims old tool results first, then summarizes balanced tool pairs, never compacts the system prompt and never deletes the transcript; its threshold is derived from the pinned model's context window and output budget, and a model too small to compact cannot be pinned to an agent at selection time; memory holds only confirmed asset facts with automatic supersede and `get_memory` is read-only | compaction/engine tests + integration |

## Failure modes

| Scenario | Behavior |
| --- | --- |
| An agent's provider or model is missing or no longer enabled | the wake has no model: the conversation answers with the deterministic scope summary, triage parks `needs-human`, the agent is shown as unavailable; never a silent switch. A changed pair the registry cannot run is refused before the agent row exists |

## The patrol report

The report is a deterministic aggregate, rendered by `component/dataquality/agentreport` and stored with its counts as columns. It
leads with the scope and the period's executions, then answers the two questions an owner actually asks: **what the agent fixed** —
the work items it closed by itself in the period, broken down by failure class — and **what is waiting for you** — the items it
escalated or froze, the ones still in progress, the drafts still pending and the issues still open. The work-item counts are the
agent's own cases, read by agent rather than by target, so a report never counts another agent's work. Both sections state their
honest zero: "nothing was settled by the agent" is different from "the agent fixed nothing because there was nothing to fix", and a
report that hides the difference cannot show whether delegating the database is working.

## When the agent distrusts its own draft

The adoption gate judges a draft's shape: its rule type, the drafting model's confidence, the target's criticality, the daily
ceiling, the scope. It cannot judge whether the model that drafted it believes it — and a model that has read an audit table's
insertion pattern often does not. `flag_proposal` is that doubt's route: the agent names one of its own pending drafts and states
the objection, and the concern is stored beside the payload (head-only metadata, so a later refresh cannot erase it) and shown in
the review queue. A flagged draft is never adopted automatically — the concern outranks the allowlist, because the doubt is about
something the gate cannot see — but it can still be adopted deliberately by the owner, because a flag is a doubt and not a refusal.
The skip is recorded in the adoption ledger with the concern quoted, so the reason a draft stayed pending is answerable later.

## Budgets and the ledger

Every write an agent makes to a check appends one row to `quality_agent_case_action`: the case, the agent, the target and the
instance (copied rather than joined, so the row survives an archived rule), the kind (`REVISE`, `CONTROL`, `RUN`, `PROPOSE`), the
field, the definition revisions the write produced, the execution it planned, the failure class it acted on and when. The table is
append-only and records applied writes only: a refused write is a metric, and recording it as a spent budget would make a refusal
cost the agent its allowance.

That ledger is what makes a budget possible. Two ceilings exist, both per rolling day, both stored on the agent row with the same
"zero means the server default" convention as the runtime budgets: writes to checks across the agent's whole scope (`REPAIR` and
`CONTROL` share it, because pausing a check and rewriting one are the same churn from an operator's point of view) and immediate
runs per check. The ceilings answer different questions: how much definition churn the delegation tolerates is a property of the
agent, while how often one broken check may be re-run is a property of the check — running ten different checks once is normal,
running one check ten times an hour is noise.

The ceiling exists because a terminal case releases its subject. "Repair, freeze, reopen, repair" would otherwise loop on the
agent's own, with each round looking locally reasonable and no person ever paged. A refusal is `RESOURCE_EXHAUSTED`, names the
ceiling and says what still works — proposing, recording a fact, escalating, and the reset tomorrow — because a bare "no" is what
makes a model retry the same verb or park the session. The budget is checked in the pipeline before validation and the probe and
after the verb is authorized, so a write that would be refused costs nothing; it is recorded after the write succeeded, and a
ledger failure is logged rather than raised, because the check is already changed and losing the row only means one write goes
uncounted.

## Instrumentation

Three counters describe how autonomous the agent is: work items opened (by failure class), work-item transitions (by the state
reached) and writes refused (by verb, participation level and effect). The ratios a dashboard wants — autonomy is resolved over
resolved plus escalated and frozen, a reopen rate is a second transition of the same subject — are computed from those counters
rather than recorded as gauges, so the definition can change without a deploy. A refused write is the one signal that the prompt is
inviting the model somewhere the policy will not follow.

## Where to look

| Path | Owns |
| --- | --- |
| `backend/component/dataquality/agentrunner/` | The wake: spec composition, tool dispatch, the engine loop, prompt rendering |
| `backend/component/dataquality/agentpolicy/` | Participation, the effect table and the per-verb decisions |
| `backend/component/dataquality/agentlang/` | The server prose an owner reads (escalations, scope summary) |
| `backend/component/dataquality/agentcapability/` | The capability identifiers a spec and a policy agree on |
| `backend/component/dataquality/diagnosis.go`, `case.go`, `health.go` | Diagnosis, case verification and check health, all pure |
| `backend/api/v1/data_quality_agent_*.go` | The service: sessions, adoption, evidence, cases, the agent verbs, attention |
| `backend/runner/qualityrunner/` | The batch-side intake: events, health events, case opening and closing |
| `backend/component/tools/quality/agent.go` | The product-only tool bodies the model calls |
| `backend/store/quality_agent*.go` | Agents, sessions, cases, participation, attention, memory |
