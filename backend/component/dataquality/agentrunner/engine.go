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
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// modelMaxTurns bounds one wake's LLM calls; the conversation it builds is
	// bounded by the wake deadline as well.
	modelMaxTurns = 8
	// toolResultLimit bounds one tool result fed back to the model.
	toolResultLimit = 8 << 10
	// maxDecisionStatement matches the API's memory statement bound.
	maxDecisionStatement = 4000
	// blockedThreshold is how many consecutive wakes must report the same blocker
	// before the session parks for a person.
	blockedThreshold = 3
	// maxConversationMessages bounds one wake's transcript read; compaction keeps the
	// real context well below this.
	maxConversationMessages = 2000
)

// Store is the slice of *store.Store the engine needs.
type Store interface {
	ListQualityRules(ctx context.Context, find *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error)
	GetQualityContract(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*store.QualityContractMessage, error)
	ListQualityExecutions(ctx context.Context, find *store.FindQualityExecutionMessage) ([]*store.QualityExecutionMessage, error)
	GetQualityExecution(ctx context.Context, id int64) (*store.QualityExecutionMessage, error)
	ListQualityMemories(ctx context.Context, find *store.FindQualityMemoryMessage) ([]*store.QualityMemoryMessage, error)
	CreateQualityInvestigation(ctx context.Context, input *store.CreateQualityInvestigationInput) (*store.QualityInvestigationMessage, bool, error)
	CreateQualityMemory(ctx context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error)
	AppendQualityAgentTranscriptMessage(ctx context.Context, input *store.QualityAgentTranscriptMessage) (*store.QualityAgentTranscriptMessage, error)
	ListQualityAgentTranscriptMessages(ctx context.Context, find *store.FindQualityAgentTranscriptMessage) ([]*store.QualityAgentTranscriptMessage, error)
	MaxQualityAgentTranscriptSeq(ctx context.Context, sessionID int64) (int64, error)
	UpdateQualityAgentSessionCompaction(ctx context.Context, resourceID, summary string, throughSeq int64) (bool, error)
}

// Chat runs the LLM tool loop. Production passes llm.RunAgentLoop; a test drives the
// engine with a scripted event stream.
type Chat func(ctx context.Context, cfg llm.AgentConfig) <-chan llm.AgentEvent

// ResolveModel reports the LLM configuration, or ok=false when analysis is disabled.
type ResolveModel func(ctx context.Context) (llm.ResolvedConfig, bool, error)

// Engine runs one wake of one session.
type Engine struct {
	store        Store
	now          func() time.Time
	chat         Chat
	resolveModel ResolveModel
	metrics      *metrics.Metrics
}

// NewEngine creates the deterministic engine.
func NewEngine(store Store) *Engine {
	return &Engine{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// NewModelEngine creates an engine that reasons with a model, falling back to the
// deterministic survey when the model is disabled or unavailable.
func NewModelEngine(store Store, chat Chat, resolve ResolveModel, m *metrics.Metrics) *Engine {
	engine := NewEngine(store)
	engine.chat = chat
	engine.resolveModel = resolve
	engine.metrics = m
	return engine
}

// Options carries the per-wake budgets the runner resolves from the workspace settings.
type Options struct {
	// CompactionTriggerTokens is the context size that triggers compaction; zero uses
	// the server default.
	CompactionTriggerTokens int
}

// Outcome is what the runner persists after one wake.
type Outcome struct {
	Blocked       bool
	BlockedCode   string
	BlockedReason string
	// NextWakeAt is the wake the engine asks for; nil lets the runner use the idle
	// cadence.
	NextWakeAt    *time.Time
	TokensUsed    int64
	ContextTokens int32
	// ProcessedSeq is the largest message seq this wake consumed. A newer message
	// (an event that arrived mid-wake) triggers an immediate follow-up wake.
	ProcessedSeq int64
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
	system, err := e.ensureSystemMessage(ctx, session)
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
	_, err = e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           wake,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE,
		Content:        wakePrompt(session, wake, pending),
		IdempotencyKey: fmt.Sprintf("wake:%d", wake),
	})
	if err != nil {
		return Outcome{}, err
	}
	outcome, err := e.runWake(ctx, session, wake, system, pending, options)
	if err != nil {
		return Outcome{}, err
	}
	if outcome.EngineState == nil {
		outcome.EngineState = cloneEngineState(session.EngineState)
	}
	e.recordEngine(ctx, session, wake, 0, "wake finished: "+outcome.Summary)
	return outcome, nil
}

// runWake selects the model wake or the deterministic survey.
func (e *Engine) runWake(ctx context.Context, session *store.QualityAgentSessionMessage, wake int32, system *store.QualityAgentTranscriptMessage, pending []*store.QualityAgentTranscriptMessage, options Options) (Outcome, error) {
	if e.resolveModel != nil && e.chat != nil {
		config, ok, err := e.resolveModel(ctx)
		if err == nil && ok {
			return e.modelWake(ctx, session, wake, system, pending, config, options)
		}
	}
	return e.deterministicWake(ctx, session, wake, pending)
}

// modelWake continues the session with the model.
func (e *Engine) modelWake(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
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
	var compactionTokens int64
	var (
		messages []*store.QualityAgentTranscriptMessage
		err      error
	)
	buildContext := func() ([]llm.AgentMessage, error) {
		messages, err = e.loadTranscript(ctx, session)
		if err != nil {
			return nil, err
		}
		compactTokens, compacted, compactErr := e.maybeCompact(ctx, session, wake, system, config, messages, options.CompactionTriggerTokens, false)
		if compactErr != nil {
			slog.Warn("failed to compact the session context",
				slog.String("session", session.ResourceID), slog.Any("error", compactErr))
		} else {
			compactionTokens += compactTokens
			if compacted {
				messages, err = e.loadTranscript(ctx, session)
				if err != nil {
					return nil, err
				}
			}
		}
		// Old tool results keep their head and tail only: the middle of an oversized
		// result from days ago is not worth resending on every turn.
		return pruneToolResults(BuildConversation(session, system, messages), keepRecentToolResults), nil
	}

	state := cloneEngineState(session.EngineState)
	var (
		turn           int32
		blocked        bool
		blockedCode    string
		blockedWhy     string
		nextWake       *time.Time
		scheduleReason string
		totalTokens    int64
		maxPrompt      int32
		truncatedTools = map[string]bool{}
		turnUsage      = map[int32]*llm.Usage{}
	)
	totalTokens = compactionTokens
	executor := e.toolExecutor(ctx, session, truncatedTools, &blocked, &blockedCode, &blockedWhy, &nextWake, &scheduleReason)
	runLoop := func(conversation []llm.AgentMessage) *llm.AgentEvent {
		chatConfig := llm.AgentConfig{
			Provider:             config,
			Feature:              "dq_agent",
			History:              conversation,
			Tools:                modelToolDefs(),
			Executor:             executor,
			MaxTurns:             modelMaxTurns,
			MaxConversationBytes: llm.DefaultMaxConversationBytes,
			IncludeUsage:         true,
			Metrics:              e.metrics,
		}
		for event := range e.chat(ctx, chatConfig) {
			switch event.Type {
			case llm.AgentEventTurnStart:
				turn = int32(event.Turn)
				e.recordEngineKeyed(ctx, session, wake, turn,
					fmt.Sprintf("model turn %d started", turn),
					fmt.Sprintf("wake:%d:turn:%d:start", wake, turn))
			case llm.AgentEventUsage:
				if event.Usage != nil {
					turnUsage[int32(event.Turn)] = event.Usage
					totalTokens += event.Usage.TotalTokens
					if event.Usage.TotalTokens == 0 {
						totalTokens += event.Usage.PromptTokens + event.Usage.CompletionTokens
					}
					if event.Usage.PromptTokens > int64(maxPrompt) {
						maxPrompt = int32(event.Usage.PromptTokens)
					}
				}
			case llm.AgentEventAssistant:
				if event.Assistant == nil {
					continue
				}
				usage := turnUsage[int32(event.Turn)]
				e.recordMessage(ctx, session, &store.QualityAgentTranscriptMessage{
					Wake:             wake,
					Turn:             int32(event.Turn),
					Role:             storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
					Origin:           storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL,
					Content:          event.Assistant.Content,
					ToolCalls:        toolCallsToStore(event.Assistant.ToolCalls),
					Tokens:           usageTokens(usage),
					PromptTokens:     usagePromptTokens(usage),
					CompletionTokens: usageCompletionTokens(usage),
					IdempotencyKey:   assistantIdempotencyKey(wake, int32(event.Turn), event.Assistant),
				})
			case llm.AgentEventToolEnd:
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
			case llm.AgentEventError:
				err := event.Error
				return &llm.AgentEvent{Type: llm.AgentEventError, Error: err}
			default:
			}
		}
		return nil
	}

	conversation, err := buildContext()
	if err != nil {
		return Outcome{}, err
	}
	failure := runLoop(conversation)
	if failure != nil && llm.IsContextOverflow(failure.Error) {
		// The provider rejected the context as too large. Compact as much as possible,
		// rebuild, and retry once inside this wake instead of wasting a whole retry cycle.
		tokens, _, compactErr := e.maybeCompact(ctx, session, wake, system, config, messages,
			options.CompactionTriggerTokens, true)
		compactionTokens += tokens
		totalTokens += tokens
		if compactErr == nil {
			if conversation, err = buildContext(); err != nil {
				return Outcome{}, err
			}
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
			NextWakeAt:    ptrTime(e.now().Add(retryWake)),
			TokensUsed:    totalTokens,
			ContextTokens: maxPrompt,
			ProcessedSeq:  consumed,
			EngineState:   state,
			Summary:       summary,
		}, nil
	}

	summary := "wake finished"
	if blocked {
		if state.BlockedCode == blockedCode {
			state.BlockedRounds++
		} else {
			state.BlockedCode = blockedCode
			state.BlockedRounds = 1
		}
		if state.BlockedRounds >= blockedThreshold {
			return Outcome{
				Blocked:       true,
				BlockedCode:   blockedCode,
				BlockedReason: blockedWhy,
				TokensUsed:    totalTokens,
				ContextTokens: maxPrompt,
				ProcessedSeq:  consumed,
				EngineState:   state,
				Summary:       "blocked: " + blockedWhy,
			}, nil
		}
		return Outcome{
			NextWakeAt:    ptrTime(e.now().Add(retryWake)),
			TokensUsed:    totalTokens,
			ContextTokens: maxPrompt,
			ProcessedSeq:  consumed,
			EngineState:   state,
			Summary:       fmt.Sprintf("reported the blocker %q once; waiting for it to persist", blockedCode),
		}, nil
	}
	state.BlockedCode = ""
	state.BlockedRounds = 0
	if scheduleReason != "" {
		summary = "wake finished; scheduled an earlier wake: " + scheduleReason
	}
	return Outcome{
		NextWakeAt:    nextWake,
		TokensUsed:    totalTokens,
		ContextTokens: maxPrompt,
		ProcessedSeq:  consumed,
		EngineState:   state,
		Summary:       summary,
	}, nil
}

// deterministicWake runs the no-model survey or event investigation. It consumes every
// pending event it can process (bounded per wake); an ask parks the session because only
// a model can answer it, and anything it leaves unprocessed keeps a seq above the
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
			outcome, err := e.investigateEvent(ctx, session, wake, event.Reference)
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
			Summary:      fmt.Sprintf("investigated %d event(s)", investigated),
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
// daily look; uncovered declared properties become investigations, and one asset fact
// records the survey.
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
	plan := dataquality.DerivePlan(contract != nil, dataquality.ActiveIntent(contractSettings.GetIntent(), e.now()), ruleShapes(rules))
	state := cloneEngineState(session.EngineState)
	state.Phase = "survey"
	done := doneSet(state)

	gapLabels := make([]string, 0, len(plan.Requirements))
	for _, requirement := range plan.Requirements {
		if requirement.Satisfied || requirement.Exempted {
			continue
		}
		gapLabels = append(gapLabels, gapLabel(requirement))
	}
	if len(gapLabels) == 0 {
		e.recordEngine(ctx, session, wake, 0, "deterministic survey: no coverage gap")
		return Outcome{
			NextWakeAt:  ptrTime(e.now().Add(maintainWake)),
			EngineState: state,
			Summary:     "surveyed: no coverage gap",
		}, nil
	}

	written, remaining := 0, 0
	for _, label := range gapLabels {
		if done["gap:"+label] {
			continue
		}
		if written >= maxGapInvestigations {
			remaining++
			continue
		}
		investigation := &store.CreateQualityInvestigationInput{
			TargetGUID:       session.TargetGUID,
			TargetType:       storepb.MetaType(session.TargetType),
			Trigger:          storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_COVERAGE_GAP,
			TriggerReference: session.TargetGUID + "#" + label,
			Status:           storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
			Verdict:          storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_CONTRIBUTING_FACTOR,
			Summary:          fmt.Sprintf("the target declares %s but no enabled check covers it", label),
			Evidence: &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
				{Kind: "PLAN", Reference: session.TargetGUID},
				{Kind: "COVERAGE", Reference: label},
			}},
			RequestFingerprint: "coverage-gap:" + session.TargetGUID + ":" + label,
			CreatedBy:          ptrInt32(session.AgentPrincipalID),
		}
		if _, err := e.recordInvestigation(ctx, investigation); err != nil {
			return Outcome{}, err
		}
		state.Done = append(state.Done, "gap:"+label)
		done["gap:"+label] = true
		written++
	}
	if !done["asset_fact"] {
		statement := fmt.Sprintf("the plan declares %d uncovered propert(ies): %s", len(gapLabels), joinLabels(gapLabels))
		if err := e.recordAssetFact(ctx, session, statement); err != nil {
			return Outcome{}, err
		}
		state.Done = append(state.Done, "asset_fact")
	}
	if written > 0 {
		e.recordEngine(ctx, session, wake, 0,
			fmt.Sprintf("deterministic survey recorded %d investigation(s)", written))
	}
	if remaining > 0 {
		return Outcome{
			NextWakeAt:  ptrTime(e.now().Add(gapContinueWake)),
			EngineState: state,
			Summary:     fmt.Sprintf("surveyed %d uncovered propert(ies); %d remain for the next wake", len(gapLabels), remaining),
		}, nil
	}
	return Outcome{
		NextWakeAt:  ptrTime(e.now().Add(maintainWake)),
		EngineState: state,
		Summary:     fmt.Sprintf("surveyed %d uncovered propert(ies)", len(gapLabels)),
	}, nil
}

// investigateEvent turns one failed execution into an investigation. The conclusion is
// deliberately inconclusive: without a model it records the facts, not a root cause.
func (e *Engine) investigateEvent(ctx context.Context, session *store.QualityAgentSessionMessage, wake int32, reference string) (Outcome, error) {
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
	investigation := &store.CreateQualityInvestigationInput{
		TargetGUID:         session.TargetGUID,
		TargetType:         storepb.MetaType(session.TargetType),
		Trigger:            storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION,
		TriggerReference:   reference,
		Status:             storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		Verdict:            storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_INCONCLUSIVE,
		Summary:            summary,
		Evidence:           &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{{Kind: "EXECUTION", Reference: reference}}},
		RequestFingerprint: "event-investigation:" + reference,
		CreatedBy:          ptrInt32(session.AgentPrincipalID),
	}
	if _, err := e.recordInvestigation(ctx, investigation); err != nil {
		return Outcome{}, err
	}
	e.recordEngine(ctx, session, wake, 0, "deterministic investigation recorded for "+reference)
	return Outcome{
		NextWakeAt:  ptrTime(e.now().Add(maintainWake)),
		EngineState: cloneEngineState(session.EngineState),
		Summary:     "investigated the event",
	}, nil
}

// ensureSystemMessage returns the session's system message, creating it on the first
// wake. The system message is never compacted away: the context builder always
// prepends it.
func (e *Engine) ensureSystemMessage(ctx context.Context, session *store.QualityAgentSessionMessage) (*store.QualityAgentTranscriptMessage, error) {
	systemRole := storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM
	messages, err := e.store.ListQualityAgentTranscriptMessages(ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID: session.ID,
		Role:      &systemRole,
		Limit:     1,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the session's system message")
	}
	if len(messages) > 0 {
		return messages[0], nil
	}
	return e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           0,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
		Content:        systemPrompt(),
		IdempotencyKey: "system:v1",
	})
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

func usagePromptTokens(usage *llm.Usage) int32 {
	if usage == nil {
		return 0
	}
	return clampInt32(usage.PromptTokens)
}

func usageCompletionTokens(usage *llm.Usage) int32 {
	if usage == nil {
		return 0
	}
	return clampInt32(usage.CompletionTokens)
}

func clampInt32(value int64) int32 {
	if value > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(value)
}
