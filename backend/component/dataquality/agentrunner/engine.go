// Package agentrunner is the wake engine of the built-in quality agent.
//
// A session is one target's long-lived conversation. One wake appends a user message,
// rebuilds the model input from the stored transcript, gives the model one bounded
// turn (or runs the deterministic survey when no model is configured) and records every
// assistant message, tool call and tool result as it happens. The next wake continues
// the same conversation; the runner owns when that next wake is.
package agentrunner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentpolicy"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/component/tools/quality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// modelMaxTurns bounds one wake's LLM calls; the conversation it builds is
	// bounded by the wake deadline as well.
	modelMaxTurns = 8
	// blockedThreshold is how many consecutive wakes must report the same blocker
	// before the session parks for a person.
	blockedThreshold = 3
	// maxConversationMessages bounds one wake's transcript read; compaction keeps the
	// real context well below this.
	maxConversationMessages = 2000
	// maxReasoningRunes bounds one assistant row's display-only reasoning. It is
	// never sent back, so this only protects the transcript table and the read API
	// from a provider that returns an unbounded reasoning block.
	maxReasoningRunes = 64 * 1024
)

// Store is the slice of *store.Store the engine needs.
type Store interface {
	ListQualityRules(ctx context.Context, find *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error)
	GetQualityContract(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*store.QualityContractMessage, error)
	ListQualityExecutions(ctx context.Context, find *store.FindQualityExecutionMessage) ([]*store.QualityExecutionMessage, error)
	GetQualityExecution(ctx context.Context, id int64) (*store.QualityExecutionMessage, error)
	ListQualityMemories(ctx context.Context, find *store.FindQualityMemoryMessage) ([]*store.QualityMemoryMessage, error)
	CreateQualityMemory(ctx context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error)
	AppendQualityAgentTranscriptMessage(ctx context.Context, input *store.QualityAgentTranscriptMessage) (*store.QualityAgentTranscriptMessage, error)
	ListQualityAgentTranscriptMessages(ctx context.Context, find *store.FindQualityAgentTranscriptMessage) ([]*store.QualityAgentTranscriptMessage, error)
	UpdateQualityAgentSessionCompaction(ctx context.Context, resourceID, summary string, throughSeq int64) (bool, error)
	UpdateQualityAgentSessionContextUsage(ctx context.Context, resourceID string, usage *storepb.QualityAgentSessionContextUsage) (bool, error)
	UpdateQualityAgentSystemMessage(ctx context.Context, sessionID int64, content string) (bool, error)
	GetUserByID(ctx context.Context, id int) (*store.UserMessage, error)
	GetQualityAgentByID(ctx context.Context, id int64) (*store.QualityAgentMessage, error)
	QualityAgentEnrollmentSummary(ctx context.Context, workspaceID string, agentID int64) ([]*store.QualityAgentDatabaseEnrollmentSummary, error)
	ListQualityAgentEnrollments(ctx context.Context, find *store.FindQualityAgentEnrollmentMessage) ([]*store.QualityAgentEnrollmentMessage, error)
	ListQualitySuggestions(ctx context.Context, find *store.FindQualitySuggestionMessage) ([]*store.QualitySuggestionMessage, error)
	GetQualityAgentAdoptionStatus(ctx context.Context, agentID int64, now time.Time) (*store.QualityAgentAdoptionStatusMessage, error)
	ListOpenQualityAgentCasesByTarget(ctx context.Context, agentID int64, targetGUID string, limit int) ([]*store.QualityAgentCaseMessage, error)
	GetQualityAgentParticipation(ctx context.Context, agentID int64, databaseRef, targetGUID string) (*store.QualityAgentParticipationLevels, error)
}

// Chat runs the LLM tool loop. Production passes agent.RunLoop; a test drives the
// engine with a scripted event stream.
type Chat func(ctx context.Context, cfg agent.Config) <-chan agent.Event

// ResolveModel reports the LLM configuration of one agent, or ok=false when that
// agent has no usable model (no selection, a stale one, or disabled analysis).
type ResolveModel func(ctx context.Context, agent *store.QualityAgentMessage) (llm.ResolvedConfig, bool, error)

// Engine runs one wake of one session.
type Engine struct {
	store        Store
	now          func() time.Time
	chat         Chat
	resolveModel ResolveModel
	metrics      *metrics.Metrics
	// tableContext and draftChecks are the optional triage capabilities. A process that
	// does not wire them still runs; a triage wake reports the missing capability
	// instead of pretending to have analysed the table.
	tableContext TableContextReader
	draftChecks  DraftChecksFunc
	// evidence is the application-layer read the evidence tools use. It is wired
	// after the service exists, like adoption, because the engine is constructed
	// first.
	evidence quality.AgentEvidence
	// adoption applies the agent's auto-adoption guardrails after a target wake that queued drafts.
	adoption AdoptionFunc
	// ownerAdoption executes an owner-requested adoption from the model tool. A
	// process that does not wire it keeps the tool out of reach.
	ownerAdoption OwnerAdoptionFunc
	// checker enforces each tool's IAM permission against the session's agent
	// principal. A process that does not wire it skips the check; production wires
	// it, because an in-process tool call does not pass the RPC ACL interceptor.
	checker tool.PermissionChecker
	// maxParallelReads bounds concurrent parallel-safe tool calls in one batch.
	maxParallelReads int
	// auditSink records side-effecting tool calls in the deployment ledger.
	auditSink tool.AuditSink
}

// SetAdoption wires the auto-adoption pass after the API service exists. The
// engine is constructed before the service that owns the rule-creation path, so
// this is a setter rather than an option.
func (e *Engine) SetAdoption(adoption AdoptionFunc) {
	e.adoption = adoption
}

// SetEvidence wires the application-layer evidence read after the API service
// exists, for the same reason SetAdoption is a setter. Without it the evidence tools
// report that the read is not configured instead of falling back to a store query.
func (e *Engine) SetEvidence(evidence quality.AgentEvidence) {
	e.evidence = evidence
}

// SetOwnerAdoption wires the owner-requested adoption path after the API service
// exists, for the same reason SetAdoption is a setter.
func (e *Engine) SetOwnerAdoption(ownerAdoption OwnerAdoptionFunc) {
	e.ownerAdoption = ownerAdoption
}

// NewEngine creates the deterministic engine.
func NewEngine(store Store) *Engine {
	return &Engine{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// NewModelEngine creates an engine that reasons with a model, falling back to the
// deterministic survey when the model is disabled or unavailable.
func NewModelEngine(store Store, chat Chat, resolve ResolveModel, m *metrics.Metrics, options ...Option) *Engine {
	engine := NewEngine(store)
	engine.chat = chat
	engine.resolveModel = resolve
	engine.metrics = m
	for _, option := range options {
		option(engine)
	}
	return engine
}

// Options carries the per-wake budgets the runner resolves. The compaction
// trigger is derived from the pinned model's capacities; an explicit value is a
// test/override hook only.
type Options struct {
	// CompactionTriggerTokens overrides the derived context size that triggers
	// compaction; zero derives it from the model capacities.
	CompactionTriggerTokens int
}

// Outcome is what the runner persists after one wake.
type Outcome struct {
	Blocked       bool
	BlockedCode   string
	BlockedReason string
	// NextWakeAt is the wake the engine asks for; nil lets the runner use the idle
	// cadence.
	NextWakeAt *time.Time
	// TriageComplete tells the runner this wake finished a TRIAGE analysis of an
	// uncovered target, so the enrollment row can park until its next review.
	TriageComplete bool
	// ProcessedSeq is the largest message seq this wake consumed. A newer message
	// (an event that arrived mid-wake) triggers an immediate follow-up wake.
	ProcessedSeq int64
	// adoptionUsed reports that the model called the owner-requested adoption tool,
	// so the wake must not also run the automatic adoption pass.
	adoptionUsed bool
	EngineState  *storepb.QualityAgentSessionEngineState
	Summary      string
}

// Wake runs one wake of a session. It repairs a tool call a crash left unanswered,
// appends the wake's user message, continues the stored conversation (model wake) or
// surveys the target (deterministic wake), and records the outcome on the transcript.
func (e *Engine) Wake(ctx context.Context, session *store.QualityAgentSessionMessage, options Options) (Outcome, error) {
	if session == nil || session.ID == 0 {
		return Outcome{}, common.Errorf(common.Invalid, "a session is required")
	}
	agent, err := e.store.GetQualityAgentByID(ctx, session.AgentID)
	if err != nil {
		return Outcome{}, errors.Wrap(err, "failed to read the session agent")
	}
	language := e.sessionLanguage(ctx, agent)
	system, systemChanged, err := e.ensureSystemMessage(ctx, session, agent, language)
	if err != nil {
		return Outcome{}, err
	}
	// A crash can leave an assistant tool call without its result. Close it now, before
	// the new turn, so the stored transcript is legal for every reader and for the
	// compaction cut.
	if err := e.repairInterruptedToolCalls(ctx, session); err != nil {
		return Outcome{}, err
	}
	pending, err := e.pendingMessages(ctx, session)
	if err != nil {
		return Outcome{}, err
	}
	wake := session.WakeCount + 1
	wakeStartedAt := e.now()
	cases := e.openCases(ctx, session)
	instruction := wakePrompt(session, wake, pending, agent, cases, e.participation(ctx, session))
	if systemChanged {
		// The stored system prompt was rewritten on this wake. The notice is part of
		// the wake message, so the transcript stays append-only and the model is told
		// once that earlier statements about its abilities may be stale.
		instruction = configurationChangeNotice(agent) + "\n\n" + instruction
	}
	_, err = e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           wake,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE,
		Content:        instruction + "\n\n" + languageReminder(language),
		IdempotencyKey: fmt.Sprintf("wake:%d", wake),
	})
	if err != nil {
		return Outcome{}, err
	}
	outcome, err := e.runWake(ctx, session, agent, wake, system, pending, wakeStartedAt, options)
	if err != nil {
		return Outcome{}, err
	}
	// A case is due work, not background: without this the session would sleep until its
	// idle cadence and the case would wait a day. The runner merges it again at release,
	// because a case can be opened by another pass while this wake is running.
	if earliest := earliestCaseAction(cases, e.now()); earliest != nil {
		if outcome.NextWakeAt == nil || earliest.Before(*outcome.NextWakeAt) {
			outcome.NextWakeAt = earliest
		}
	}
	if outcome.EngineState == nil {
		outcome.EngineState = cloneEngineState(session.EngineState)
	}
	e.recordEngine(ctx, session, wake, 0, "wake finished: "+outcome.Summary)
	return outcome, nil
}

// caseWakeFloor is the soonest a case may wake a session that is otherwise idle: the
// session tick is three seconds, and a case whose action time is in the past must not make
// the runner spin.
const caseWakeFloor = time.Minute

// openCases reads the target's unfinished work for the wake prompt and the next-wake
// merge. A read failure degrades to "no work items" instead of failing the wake: the
// transcript, not this read, is what the wake cannot lose.
func (e *Engine) openCases(ctx context.Context, session *store.QualityAgentSessionMessage) []*store.QualityAgentCaseMessage {
	// Kind 2 is the owner conversation, which has no target; any other kind is a target
	// session (Kind 1 in the database, unset in a focused test).
	if session == nil || session.Kind == 2 || session.AgentID == 0 || session.TargetGUID == "" {
		return nil
	}
	cases, err := e.store.ListOpenQualityAgentCasesByTarget(ctx, session.AgentID, session.TargetGUID, 3)
	if err != nil {
		slog.Warn("failed to read the target's open cases",
			slog.String("session", session.ResourceID), log.WithError(err))
		return nil
	}
	return cases
}

// participation resolves how far this agent may go on this target. A read failure degrades to
// the delegation default rather than failing the wake: the prompt is a view of the policy, and
// the verbs re-read it when one is requested.
func (e *Engine) participation(ctx context.Context, session *store.QualityAgentSessionMessage) agentpolicy.Participation {
	if session == nil || session.Kind == 2 || session.AgentID == 0 {
		return agentpolicy.DefaultParticipation
	}
	levels, err := e.store.GetQualityAgentParticipation(ctx, session.AgentID, session.DatabaseRef, session.TargetGUID)
	if err != nil || levels == nil {
		if err != nil {
			slog.Warn("failed to read the agent's participation",
				slog.String("session", session.ResourceID), log.WithError(err))
		}
		return agentpolicy.DefaultParticipation
	}
	return agentpolicy.Effective(
		agentpolicy.Participation(levels.Agent),
		participationOf(levels.Database),
		participationOf(levels.Target),
	)
}

// participationOf maps the store's optional level onto the policy's, keeping nil as nil so the
// cascade can tell "inherit" from "the same value written down".
func participationOf(level *store.QualityAgentParticipation) *agentpolicy.Participation {
	if level == nil {
		return nil
	}
	converted := agentpolicy.Participation(*level)
	return &converted
}

// earliestCaseAction turns the soonest due case into a wake time, clamped so a past-due
// case does not make the runner spin.
func earliestCaseAction(cases []*store.QualityAgentCaseMessage, now time.Time) *time.Time {
	var earliest *time.Time
	for _, openCase := range cases {
		if openCase == nil || openCase.NextActionAt == nil {
			continue
		}
		if earliest == nil || openCase.NextActionAt.Before(*earliest) {
			at := openCase.NextActionAt.UTC()
			earliest = &at
		}
	}
	if earliest == nil {
		return nil
	}
	floor := now.Add(caseWakeFloor)
	if earliest.Before(floor) {
		return &floor
	}
	return earliest
}

// runWake selects the model wake or the deterministic survey.
func (e *Engine) runWake(ctx context.Context, session *store.QualityAgentSessionMessage, agent *store.QualityAgentMessage, wake int32, system *store.QualityAgentTranscriptMessage, pending []*store.QualityAgentTranscriptMessage, wakeStartedAt time.Time, options Options) (Outcome, error) {
	if e.resolveModel != nil && e.chat != nil {
		config, ok, err := e.resolveModel(ctx, agent)
		if err == nil && ok {
			outcome, wakeErr := e.modelWake(ctx, session, agent, wake, system, pending, config, options)
			// Automatic adoption follows any target wake that queued a draft, not just a triage
			// wake: a monitoring wake drafts only when its check cannot express the intent, and
			// the same guardrails (mode, confidence, allowlist, critical switch, scope, daily
			// ceiling) decide whether the replacement is created. The pass filters by the wake's
			// start, so a wake that queued nothing adopts nothing.
			if wakeErr == nil && !outcome.adoptionUsed && session.Kind != 2 && e.adoption != nil {
				e.adoption(ctx, session, wakeStartedAt)
			}
			return outcome, wakeErr
		}
	}
	// The front office must stay usable without a model: answer with a deterministic
	// scope summary instead of parking the conversation.
	if session.Kind == 2 {
		return e.conversationWake(ctx, session, wake, pending)
	}
	// Triage is an analysis, not a bookkeeping survey: without a model there is nothing
	// honest to record. Park the session for a person instead of retrying every ten
	// minutes and appending an ENGINE row each time; a person configures the provider and
	// resumes it.
	if session.Mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE {
		e.recordEngine(ctx, session, wake, 0, "triage skipped: no model is configured")
		return Outcome{
			Blocked:       true,
			BlockedCode:   "needs-human",
			BlockedReason: "triage needs a model; configure an LLM provider and resume the session",
			ProcessedSeq:  session.ProcessedSeq,
			EngineState:   cloneEngineState(session.EngineState),
			Summary:       "blocked: triage needs a model",
		}, nil
	}
	return e.deterministicWake(ctx, session, wake, pending)
}

// modelWake continues the session with the model.
func (e *Engine) modelWake(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
	sessionAgent *store.QualityAgentMessage,
	wake int32,
	system *store.QualityAgentTranscriptMessage,
	pending []*store.QualityAgentTranscriptMessage,
	config llm.ResolvedConfig,
	options Options,
) (Outcome, error) {
	// Every pending event/question is in the rebuilt context, so the wake consumes all
	// of them. Rows the wake itself writes (assistant/tool/engine) are not inputs and
	// must never move this watermark.
	consumed := session.ProcessedSeq
	for _, message := range pending {
		if message.Seq > consumed {
			consumed = message.Seq
		}
	}
	var (
		messages []*store.QualityAgentTranscriptMessage
		err      error
	)
	buildContext := func() ([]agent.Message, error) {
		messages, err = e.loadTranscript(ctx, session)
		if err != nil {
			return nil, err
		}
		_, compacted, compactErr := e.maybeCompact(ctx, session, wake, system, config, messages, options.CompactionTriggerTokens, false)
		if compactErr != nil {
			slog.Warn("failed to compact the session context",
				slog.String("session", session.ResourceID), slog.Any("error", compactErr))
		} else if compacted {
			messages, err = e.loadTranscript(ctx, session)
			if err != nil {
				return nil, err
			}
		}
		// Old tool results keep their head and tail only: the middle of an oversized
		// result from days ago is not worth resending on every turn.
		return pruneToolResults(BuildConversation(session, system, messages), keepRecentToolResults), nil
	}

	state := cloneEngineState(session.EngineState)
	var (
		turn           int32
		truncatedTools = map[string]bool{}
		turnUsage      = map[int32]*llm.Usage{}
	)
	// The control tools write this state; the wake applies it once, after the loop.
	control := &wakeControl{wake: wake, ownerAsked: ownerAsked(pending)}
	set, err := e.toolSet(session, sessionAgent, control)
	if err != nil {
		return Outcome{}, err
	}
	spec := specForSession(session)
	invocation, err := e.toolInvocation(ctx, session, spec.Name)
	if err != nil {
		return Outcome{}, err
	}
	executor := toolExecutor(ctx, set, truncatedTools, invocation)
	runLoop := func(conversation []agent.Message) *agent.Event {
		chatConfig := agent.Config{
			Provider:             config,
			Feature:              spec.Feature,
			History:              conversation,
			Tools:                agent.ToolDefs(set.Schemas()),
			Executor:             executor,
			MaxTurns:             spec.MaxTurns,
			MaxConversationBytes: agent.DefaultMaxConversationBytes,
			IncludeUsage:         true,
			Metrics:              e.metrics,
			ParallelSafe:         set.IsParallelSafe,
			MaxParallelToolCalls: e.maxParallelReads,
		}
		for event := range e.chat(ctx, chatConfig) {
			switch event.Type {
			case agent.EventTurnStart:
				turn = int32(event.Turn)
				e.recordEngineKeyed(ctx, session, wake, turn,
					fmt.Sprintf("model turn %d started", turn),
					fmt.Sprintf("wake:%d:turn:%d:start", wake, turn))
			case agent.EventUsage:
				if event.Usage != nil {
					turnUsage[int32(event.Turn)] = event.Usage
					if event.Usage.PromptTokens > 0 {
						// The provider counted the whole request it saw, which is the real
						// context occupancy for this turn.
						e.recordContextUsage(ctx, session, config, wake, int32(event.Turn),
							int(event.Usage.PromptTokens), false)
					}
				}
			case agent.EventAssistant:
				if event.Assistant == nil {
					continue
				}
				usage := turnUsage[int32(event.Turn)]
				e.recordMessage(ctx, session, &store.QualityAgentTranscriptMessage{
					Wake:           wake,
					Turn:           int32(event.Turn),
					Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
					Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL,
					Content:        event.Assistant.Content,
					Reasoning:      truncateRunes(event.Assistant.Reasoning, maxReasoningRunes),
					ToolCalls:      toolCallsToStore(event.Assistant.ToolCalls),
					Tokens:         usageTokens(usage),
					IdempotencyKey: assistantIdempotencyKey(wake, int32(event.Turn), event.Assistant),
				})
			case agent.EventToolEnd:
				if event.ToolCall == nil {
					continue
				}
				e.recordMessage(ctx, session, &store.QualityAgentTranscriptMessage{
					Wake:           wake,
					Turn:           int32(event.Turn),
					Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL,
					Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL,
					Content:        event.ToolResult,
					ToolCallID:     event.ToolCall.ID,
					ToolName:       event.ToolCall.GetName(),
					Error:          event.ToolError,
					Truncated:      truncatedTools[event.ToolCall.ID],
					IdempotencyKey: fmt.Sprintf("wake:%d:tool:%s", wake, event.ToolCall.ID),
				})
			case agent.EventError:
				err := event.Error
				return &agent.Event{Type: agent.EventError, Error: err}
			default:
			}
		}
		return nil
	}

	conversation, err := buildContext()
	if err != nil {
		return Outcome{}, err
	}
	// Record the engine's text estimate of the full model input before each call. It
	// covers providers that report no usage; the compaction decision itself uses the
	// transcript pressure alone, so the meter's warning line is an approximation.
	// Replacing the previous provider reading here is intentional: a failed call is then
	// honestly labeled as an estimate for this wake.
	recordEstimate := func() {
		e.recordContextUsage(ctx, session, config, wake, 0, EstimateTokens(conversation), true)
	}
	recordEstimate()
	failure := runLoop(conversation)
	if failure != nil && llm.IsContextOverflow(failure.Error) {
		// The provider rejected the context as too large. Compact as much as possible,
		// rebuild, and retry once inside this wake instead of wasting a whole retry cycle.
		_, _, compactErr := e.maybeCompact(ctx, session, wake, system, config, messages,
			options.CompactionTriggerTokens, true)
		if compactErr == nil {
			if conversation, err = buildContext(); err != nil {
				return Outcome{}, err
			}
			recordEstimate()
			failure = runLoop(conversation)
		}
	}
	if failure != nil {
		summary := "the model call failed; retrying later"
		if failure.Error != nil {
			summary = "the model call failed: " + truncateRunes(failure.Error.Error(), 200)
		}
		e.recordEngine(ctx, session, wake, turn, summary)
		return Outcome{
			NextWakeAt:   ptrTime(e.now().Add(retryWake)),
			ProcessedSeq: consumed,
			EngineState:  state,
			Summary:      summary,
		}, nil
	}

	summary := "wake finished"
	if control.blocked {
		if state.BlockedCode == control.blockedCode {
			state.BlockedRounds++
		} else {
			state.BlockedCode = control.blockedCode
			state.BlockedRounds = 1
		}
		if state.BlockedRounds >= blockedThreshold {
			return Outcome{
				Blocked:       true,
				BlockedCode:   control.blockedCode,
				BlockedReason: control.blockedWhy,
				ProcessedSeq:  consumed,
				EngineState:   state,
				adoptionUsed:  control.adoptionUsed,
				Summary:       "blocked: " + control.blockedWhy,
			}, nil
		}
		return Outcome{
			NextWakeAt:   ptrTime(e.now().Add(retryWake)),
			ProcessedSeq: consumed,
			EngineState:  state,
			adoptionUsed: control.adoptionUsed,
			Summary:      fmt.Sprintf("reported the blocker %q once; waiting for it to persist", control.blockedCode),
		}, nil
	}
	state.BlockedCode = ""
	state.BlockedRounds = 0
	if control.scheduleReason != "" {
		summary = "wake finished; scheduled an earlier wake: " + control.scheduleReason
	}
	return Outcome{
		NextWakeAt:     control.nextWake,
		TriageComplete: session.Mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE,
		ProcessedSeq:   consumed,
		EngineState:    state,
		adoptionUsed:   control.adoptionUsed,
		Summary:        summary,
	}, nil
}

// toolInvocation is the caller identity every tool in this wake runs under: the
// session's agent principal, never the person who woke it. A product without a
// checker (tests, a deployment that has not wired IAM) returns nil, which the
// pipeline treats as "the transport already authorized this call".
func (e *Engine) toolInvocation(ctx context.Context, session *store.QualityAgentSessionMessage, product string) (*tool.Invocation, error) {
	if e.checker == nil && e.metrics == nil && e.auditSink == nil {
		return nil, nil
	}
	invocation := &tool.Invocation{Workspace: session.WorkspaceID, Now: e.now, Logger: slog.Default()}
	if e.metrics != nil {
		invocation.Observer = agentToolObserver{metrics: e.metrics, product: product}
	}
	if e.auditSink != nil {
		invocation.Audit = e.auditSink
	}
	if e.checker == nil && e.auditSink == nil {
		return invocation, nil
	}
	if session.AgentPrincipalID == 0 {
		return nil, errors.New("the session has no agent principal")
	}
	principal, err := e.store.GetUserByID(ctx, int(session.AgentPrincipalID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the agent principal")
	}
	if principal == nil {
		return nil, errors.New("the session's agent principal no longer exists")
	}
	invocation.Principal = principal
	if e.checker != nil {
		invocation.Checker = sessionPermissionChecker{checker: e.checker, targetGUID: session.TargetGUID}
	}
	return invocation, nil
}

// sessionPermissionChecker binds a target session's tool checks to the session
// target: the agent's derived database scope is checked against the target, so a
// tool cannot reach a sibling target outside the agent's databases. A
// conversation session has no target and keeps the capability check (the agent
// may read across its scope).
type sessionPermissionChecker struct {
	checker    tool.PermissionChecker
	targetGUID string
}

// resourcePermissionChecker is the resource-scoped half of the scoped checker.
type resourcePermissionChecker interface {
	CheckResource(context.Context, permission.Permission, *store.UserMessage, resource.Ref) (bool, error)
}

func (c sessionPermissionChecker) CheckPermission(ctx context.Context, perm permission.Permission, principal *store.UserMessage) (bool, error) {
	if c.targetGUID != "" {
		targetChecker, ok := c.checker.(resourcePermissionChecker)
		if !ok {
			return false, errors.New("the tool checker cannot check a target resource")
		}
		return targetChecker.CheckResource(ctx, perm, principal, resource.NewRef(resource.TypeTable, c.targetGUID))
	}
	return c.checker.CheckPermission(ctx, perm, principal)
}

// agentToolObserver labels the shared tool metrics with this product's name.
type agentToolObserver struct {
	metrics *metrics.Metrics
	product string
}

// ObserveToolCall records one tool execution for this product.
func (o agentToolObserver) ObserveToolCall(toolName, outcome string, duration time.Duration, resultBytes int) {
	o.metrics.ObserveAgentToolCall(o.product, toolName, outcome, duration, resultBytes)
}

// deterministicWake runs the no-model survey or event review. It consumes every pending
// event it can process (bounded per wake); an ask parks the session because only a
// model can answer it, and anything it leaves unprocessed keeps a seq above the
// watermark so the runner wakes it again immediately.
func (e *Engine) deterministicWake(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
	wake int32,
	pending []*store.QualityAgentTranscriptMessage,
) (Outcome, error) {
	asks := false
	events := make([]*store.QualityAgentTranscriptMessage, 0, len(pending))
	for _, message := range pending {
		switch message.Origin {
		case storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK:
			asks = true
		case storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT:
			if message.Reference != "" {
				events = append(events, message)
			}
		default:
		}
	}
	if asks {
		return Outcome{
			Blocked:       true,
			BlockedCode:   "needs-human",
			BlockedReason: "a person's question needs the model; configure an LLM provider",
			ProcessedSeq:  session.ProcessedSeq,
			EngineState:   cloneEngineState(session.EngineState),
			Summary:       "blocked: answering a person needs a model",
		}, nil
	}
	if len(events) > 0 {
		consumed := session.ProcessedSeq
		investigated := 0
		for _, event := range events {
			if investigated >= maxDeterministicEvents {
				break
			}
			// A health event names a check, not an execution: without a model there is no
			// verdict to review, so the wake records it and moves on. Parking the session
			// on an event that carries no failure would silence the target exactly when it
			// is telling us it never concludes.
			if !strings.HasPrefix(event.Reference, "qualityExecutions/") {
				e.recordEngine(ctx, session, wake, 0, "deterministic health review: the check produced no verdict")
				consumed = event.Seq
				investigated++
				continue
			}
			outcome, err := e.reviewFailedExecution(ctx, session, wake, event.Reference)
			if err != nil {
				return Outcome{}, err
			}
			if outcome.Blocked {
				outcome.ProcessedSeq = consumed
				return outcome, nil
			}
			consumed = event.Seq
			investigated++
		}
		return Outcome{
			NextWakeAt:   ptrTime(e.now().Add(maintainWake)),
			ProcessedSeq: consumed,
			EngineState:  cloneEngineState(session.EngineState),
			Summary:      fmt.Sprintf("reviewed %d event(s)", investigated),
		}, nil
	}
	outcome, err := e.maintain(ctx, session, wake)
	if err != nil {
		return Outcome{}, err
	}
	outcome.ProcessedSeq = session.ProcessedSeq
	return outcome, nil
}

// maintain surveys the target's plan. A fully surveyed target sleeps until the next
// daily look; uncovered declared properties are recorded as one asset fact.
func (e *Engine) maintain(ctx context.Context, session *store.QualityAgentSessionMessage, wake int32) (Outcome, error) {
	rules, err := e.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &session.TargetGUID})
	if err != nil {
		return Outcome{}, errors.Wrap(err, "failed to read the target's rules")
	}
	contract, err := e.store.GetQualityContract(ctx, session.TargetGUID, storepb.MetaType(session.TargetType))
	if err != nil {
		return Outcome{}, errors.Wrap(err, "failed to read the target's contract")
	}
	var contractSettings *storepb.QualityContractSettings
	if contract != nil {
		contractSettings = contract.Settings
	}
	plan := dataquality.DerivePlan(contract != nil, dataquality.ActiveIntent(contractSettings.GetIntent(), e.now()), quality.RuleShapes(rules))
	state := cloneEngineState(session.EngineState)
	done := doneSet(state)

	gapLabels := make([]string, 0, len(plan.Requirements))
	for _, requirement := range plan.Requirements {
		if requirement.Satisfied || requirement.Exempted {
			continue
		}
		gapLabels = append(gapLabels, quality.GapLabel(requirement))
	}
	if len(gapLabels) == 0 {
		e.recordEngine(ctx, session, wake, 0, "deterministic survey: no coverage gap")
		return Outcome{
			NextWakeAt:  ptrTime(e.now().Add(maintainWake)),
			EngineState: state,
			Summary:     "surveyed: no coverage gap",
		}, nil
	}

	statement := fmt.Sprintf("the plan declares %d uncovered propert(ies): %s", len(gapLabels), joinLabels(gapLabels))
	factKey := fmt.Sprintf("gapfact:%x", contentHash(statement))
	if !done[factKey] {
		if err := e.agentTools().RecordAssetFactStatement(ctx, agentSessionOf(session), statement); err != nil {
			return Outcome{}, err
		}
		state.Done = append(state.Done, factKey)
		e.recordEngine(ctx, session, wake, 0, "deterministic survey: "+statement)
	}
	return Outcome{
		NextWakeAt:  ptrTime(e.now().Add(maintainWake)),
		EngineState: state,
		Summary:     fmt.Sprintf("surveyed %d uncovered propert(ies)", len(gapLabels)),
	}, nil
}

// reviewFailedExecution reviews one failed execution without a model. The facts stay in
// the session transcript; without a model there is no root cause to conclude.
func (e *Engine) reviewFailedExecution(ctx context.Context, session *store.QualityAgentSessionMessage, wake int32, reference string) (Outcome, error) {
	executionID, parseReason := executionIDFromReference(reference)
	if parseReason != "" {
		return Outcome{
			Blocked:       true,
			BlockedCode:   "needs-human",
			BlockedReason: parseReason,
			Summary:       "blocked: the event reference is not an execution",
		}, nil
	}
	execution, err := e.store.GetQualityExecution(ctx, executionID)
	if err != nil {
		return Outcome{}, errors.Wrap(err, "failed to read the triggering execution")
	}
	if execution == nil {
		return Outcome{
			Blocked:       true,
			BlockedCode:   "needs-human",
			BlockedReason: "the triggering execution no longer exists",
			Summary:       "blocked: the triggering execution is gone",
		}, nil
	}
	if execution.TargetGUID != session.TargetGUID || execution.TargetType != storepb.MetaType(session.TargetType) {
		return Outcome{
			Blocked:       true,
			BlockedCode:   "needs-human",
			BlockedReason: "the triggering execution belongs to another target",
			Summary:       "blocked: the execution does not match the session target",
		}, nil
	}
	if execution.Status != storepb.QualityStatus_QUALITY_STATUS_FAIL &&
		execution.Status != storepb.QualityStatus_QUALITY_STATUS_ERROR {
		return Outcome{
			Blocked:       true,
			BlockedCode:   "needs-human",
			BlockedReason: "the triggering execution neither failed nor errored",
			Summary:       "blocked: the execution is not a failure",
		}, nil
	}
	summary := fmt.Sprintf("execution %s finished %s with %d failed rows",
		reference, execution.Status.String(), execution.FailedRows)
	if execution.ErrorCode != "" {
		summary = fmt.Sprintf("execution %s ended %s: %s", reference, execution.ErrorCode, execution.ErrorMessage)
	}
	e.recordEngine(ctx, session, wake, 0, "deterministic event review: "+summary)
	return Outcome{
		NextWakeAt:  ptrTime(e.now().Add(maintainWake)),
		EngineState: cloneEngineState(session.EngineState),
		Summary:     "reviewed the event",
	}, nil
}

// ensureSystemMessage returns the session's system message, creating it on the
// first wake and rewriting it in place when the composed prompt changed. changed
// reports a rewrite of an existing row: the wake uses it to append one
// configuration-change notice instead of silently leaving the old wording in the
// model's history.
func (e *Engine) ensureSystemMessage(ctx context.Context, session *store.QualityAgentSessionMessage, agent *store.QualityAgentMessage, language string) (*store.QualityAgentTranscriptMessage, bool, error) {
	systemRole := storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM
	messages, err := e.store.ListQualityAgentTranscriptMessages(ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID: session.ID,
		Role:      &systemRole,
		Limit:     1,
	})
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to read the session's system message")
	}
	desired := renderSystemPrompt(specForSession(session), agent, language)
	if len(messages) > 0 {
		if messages[0].Content != desired {
			updated, err := e.store.UpdateQualityAgentSystemMessage(ctx, session.ID, desired)
			if err != nil {
				return nil, false, errors.Wrap(err, "failed to update the session system message")
			}
			if !updated {
				return nil, false, errors.New("the session system message disappeared while updating it")
			}
			messages[0].Content = desired
			return messages[0], true, nil
		}
		return messages[0], false, nil
	}
	created, err := e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           0,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
		Content:        desired,
		IdempotencyKey: "system:v1",
	})
	if err != nil {
		return nil, false, err
	}
	return created, false, nil
}

// sessionLanguage is the single language decision for a session: the agent's
// configured language, then its owner's profile language, then English. A missing
// owner falls back to the language defaults, so it cannot fail.
func (e *Engine) sessionLanguage(ctx context.Context, agent *store.QualityAgentMessage) string {
	if agent == nil {
		return agentlang.English
	}
	var owner *store.UserMessage
	if agent.OwnerPrincipalID != nil {
		owner, _ = e.store.GetUserByID(ctx, int(*agent.OwnerPrincipalID))
	}
	return agentlang.ForAgent(agent, owner)
}

// repairInterruptedToolCalls closes the trailing assistant tool call a crash left
// unanswered. Only the newest assistant is considered: an older one was followed by a
// later turn and has already been repaired by the wake that followed it.
func (e *Engine) repairInterruptedToolCalls(ctx context.Context, session *store.QualityAgentSessionMessage) error {
	messages, err := e.store.ListQualityAgentTranscriptMessages(ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID:   session.ID,
		Limit:       50,
		NewestFirst: true,
	})
	if err != nil {
		return errors.Wrap(err, "failed to read the session tail")
	}
	answers := map[string]bool{}
	for _, message := range messages {
		switch message.Role {
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL:
			answers[message.ToolCallID] = true
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT:
			for _, call := range message.ToolCalls.GetToolCalls() {
				if answers[call.GetId()] {
					continue
				}
				answers[call.GetId()] = true
				_, appendErr := e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
					Wake:           session.WakeCount,
					Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL,
					Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
					Content:        interruptedToolResult,
					ToolCallID:     call.GetId(),
					ToolName:       call.GetName(),
					Error:          "interrupted",
					IdempotencyKey: "interrupted:" + call.GetId(),
				})
				if appendErr != nil {
					return appendErr
				}
			}
			return nil
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE:
			// Runner notes may follow a completed assistant turn; keep looking for the
			// assistant they belong to.
		default:
			// A user/system message means the tail is complete, not a crash.
			return nil
		}
	}
	return nil
}

// pendingMessages reads the messages a previous wake did not consume.
func (e *Engine) pendingMessages(ctx context.Context, session *store.QualityAgentSessionMessage) ([]*store.QualityAgentTranscriptMessage, error) {
	// Only an event or a person's question is an input that needs consuming; assistant,
	// tool and engine rows the wake itself writes must never keep the session hot.
	messages, err := e.store.ListQualityAgentTranscriptMessages(ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID:  session.ID,
		AfterSeq:   session.ProcessedSeq,
		InputsOnly: true,
		Limit:      100,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the pending session inputs")
	}
	return messages, nil
}

// ownerAsked reports whether this wake is answering a person's question. The
// adoption tool is reachable only then; a scheduled or event wake has no owner
// instruction behind it.
func ownerAsked(pending []*store.QualityAgentTranscriptMessage) bool {
	for _, message := range pending {
		if message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK {
			return true
		}
	}
	return false
}

// loadTranscript reads the messages after the compaction checkpoint.
func (e *Engine) loadTranscript(ctx context.Context, session *store.QualityAgentSessionMessage) ([]*store.QualityAgentTranscriptMessage, error) {
	messages, err := e.store.ListQualityAgentTranscriptMessages(ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID:     session.ID,
		AfterSeq:      session.SummaryThroughSeq,
		ExcludeEngine: true,
		Limit:         maxConversationMessages,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the session transcript")
	}
	return messages, nil
}

// appendMessage writes one transcript entry and returns it (the store returns the
// existing row for a retried idempotency key).
func (e *Engine) appendMessage(ctx context.Context, session *store.QualityAgentSessionMessage, message *store.QualityAgentTranscriptMessage) (*store.QualityAgentTranscriptMessage, error) {
	message.SessionID = session.ID
	message.WorkspaceID = session.WorkspaceID
	created, err := e.store.AppendQualityAgentTranscriptMessage(ctx, message)
	if err != nil {
		return nil, errors.Wrap(err, "failed to append the session message")
	}
	return created, nil
}

// recordMessage persists a message best-effort: the transcript is observability, so a
// failed write must not abort a wake that already did real work. The step is logged and
// the wake continues.
func (e *Engine) recordMessage(ctx context.Context, session *store.QualityAgentSessionMessage, message *store.QualityAgentTranscriptMessage) {
	if _, err := e.appendMessage(ctx, session, message); err != nil {
		slog.Warn("failed to record a session message",
			slog.String("session", session.ResourceID), slog.String("role", message.Role.String()),
			slog.Any("error", err))
	}
}

// recordEngine writes one runner note (UI/observability only, never model context).
func (e *Engine) recordEngine(ctx context.Context, session *store.QualityAgentSessionMessage, wake, turn int32, content string) {
	message := &store.QualityAgentTranscriptMessage{
		Wake:    wake,
		Turn:    turn,
		Role:    storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE,
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
		Content: truncateRunes(content, 2000),
	}
	// Identical notes within one wake/turn collapse onto one row.
	message.IdempotencyKey = fmt.Sprintf("wake:%d:turn:%d:engine:%x", wake, turn, contentHash(content))
	e.recordMessage(ctx, session, message)
}

// recordEngineKeyed writes one runner note under an explicit idempotency key.
func (e *Engine) recordEngineKeyed(ctx context.Context, session *store.QualityAgentSessionMessage, wake, turn int32, content, key string) {
	e.recordMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           wake,
		Turn:           turn,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
		Content:        truncateRunes(content, 2000),
		IdempotencyKey: key,
	})
}

// recordContextUsage persists the engine's latest model-input sample for the session
// detail's context meter. It is best-effort observability: a failed sample never
// aborts a wake, and the sample carries the model capacities it was measured against.
func (e *Engine) recordContextUsage(ctx context.Context, session *store.QualityAgentSessionMessage, config llm.ResolvedConfig, wake, turn int32, used int, estimated bool) {
	if session == nil || used <= 0 {
		return
	}
	// The resolved config is normalized in production; normalize again so a caller that
	// passes zero capacities cannot persist an unusable window.
	limits := llm.NormalizeModelLimits(int32(config.ContextWindow), int32(config.MaxOutputTokens))
	usage := &storepb.QualityAgentSessionContextUsage{
		UsedTokens:              clampInt32(int64(used)),
		ContextWindow:           int32(limits.ContextWindow),
		MaxOutputTokens:         int32(limits.MaxOutputTokens),
		CompactionTriggerTokens: int32(llm.CompactionTrigger(limits.ContextWindow, limits.MaxOutputTokens)),
		Model:                   config.ModelName,
		Wake:                    wake,
		Turn:                    turn,
		MeasuredAt:              timestamppb.New(e.now()),
		Estimated:               estimated,
	}
	if _, err := e.store.UpdateQualityAgentSessionContextUsage(ctx, session.ResourceID, usage); err != nil {
		slog.Warn("failed to record the session context usage",
			slog.String("session", session.ResourceID), slog.Any("error", err))
	}
}

// toolCallsToStore maps the provider's tool calls to the stored shape.
func toolCallsToStore(calls []llm.ToolCall) *storepb.QualityToolCallList {
	if len(calls) == 0 {
		return &storepb.QualityToolCallList{}
	}
	list := &storepb.QualityToolCallList{ToolCalls: make([]*storepb.QualityToolCall, 0, len(calls))}
	for _, call := range calls {
		list.ToolCalls = append(list.ToolCalls, &storepb.QualityToolCall{
			Id:        call.ID,
			Name:      call.GetName(),
			Arguments: call.Function.Arguments,
		})
	}
	return list
}

func usageTokens(usage *llm.Usage) int32 {
	if usage == nil {
		return 0
	}
	if usage.TotalTokens > 0 {
		return clampInt32(usage.TotalTokens)
	}
	return clampInt32(usage.PromptTokens + usage.CompletionTokens)
}

func clampInt32(value int64) int32 {
	if value > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(value)
}
