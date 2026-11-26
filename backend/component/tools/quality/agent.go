package quality

// The built-in quality agent's own tools (product-only scope): the target summary,
// recent executions and memories it reads, the asset facts it records, and the
// triage capabilities (table context, check drafts). They are not part of the MCP
// read surface; they run through the same tool contract and pipeline as every
// other tool, and the deterministic wake paths reuse the
// RecordAssetFactStatement helper.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxTriageDraftColumns bounds the columns one draft_checks call may scope.
const maxTriageDraftColumns = 8

// MaxTriageDraftColumns is the exported bound the transport's contract tests read.
const MaxTriageDraftColumns = maxTriageDraftColumns

// AgentSession is the session identity the agent tools read and write.
// suggestionSkipCounts renders the queue summary's non-zero skip counts for the
// drafting tool's result, so an empty queue is explainable to the model.
func suggestionSkipCounts(summary *storepb.QualitySuggestionQueueSummary) map[string]int32 {
	if summary == nil {
		return nil
	}
	counts := map[string]int32{}
	if value := summary.GetSkippedExistingFact(); value > 0 {
		counts["existingFact"] = value
	}
	if value := summary.GetSkippedPending(); value > 0 {
		counts["pending"] = value
	}
	if value := summary.GetSkippedDecided(); value > 0 {
		counts["decided"] = value
	}
	if value := summary.GetSuperseded(); value > 0 {
		counts["superseded"] = value
	}
	return counts
}

type AgentSession struct {
	// WorkspaceID names the workspace in the draft resource names the model passes
	// back to the adoption tool.
	WorkspaceID string
	// AgentID is the proposing agent; adoption status and the pending queue are
	// agent-scoped even when the read is target-scoped.
	AgentID int64
	// TargetGUID is the target every read and proposal is scoped to.
	TargetGUID string
	// TargetType is the target's registry type.
	TargetType storepb.MetaType
	// ResourceID names the session in evidence references.
	ResourceID string
	// AgentPrincipalID is the actor the write is attributed to.
	AgentPrincipalID int32
}

// AgentStore is the slice of the store the agent tools use.
type AgentStore interface {
	ListQualityRules(ctx context.Context, find *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error)
	GetQualityContract(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*store.QualityContractMessage, error)
	ListQualityExecutions(ctx context.Context, find *store.FindQualityExecutionMessage) ([]*store.QualityExecutionMessage, error)
	ListQualityMemories(ctx context.Context, find *store.FindQualityMemoryMessage) ([]*store.QualityMemoryMessage, error)
	CreateQualityMemory(ctx context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error)
	ListQualitySuggestions(ctx context.Context, find *store.FindQualitySuggestionMessage) ([]*store.QualitySuggestionMessage, error)
	GetQualityAgentAdoptionStatus(ctx context.Context, agentID int64, now time.Time) (*store.QualityAgentAdoptionStatusMessage, error)
}

// TableContextColumn is one column of the target as the registry records it.
type TableContextColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Comment  string `json:"comment,omitempty"`
	Key      bool   `json:"key,omitempty"`
	// EnumValues are the labels of an enum-typed column, when the registry names
	// them. It lets the model write accepted values from the declared set instead
	// of guessing one.
	EnumValues []string `json:"enumValues,omitempty"`
	// EnumValuesTruncated reports that the labels above are a bounded subset, so
	// the model knows the set it sees is not the whole enum.
	EnumValuesTruncated bool `json:"enumValuesTruncated,omitempty"`
}

// TableContextProfile is one column's stored profile summary. Only statistics are
// exposed: no sample rows, and the profile service has already redacted personal data.
type TableContextProfile struct {
	Column        string  `json:"column"`
	NullFraction  float32 `json:"nullFraction,omitempty"`
	DistinctCount int64   `json:"distinctCount,omitempty"`
	Min           string  `json:"min,omitempty"`
	Max           string  `json:"max,omitempty"`
	TopValues     string  `json:"topValues,omitempty"`
	ProfiledAt    string  `json:"profiledAt,omitempty"`
}

// TableContext is the bounded, read-only view of a target a triage wake reads. It
// never contains sample rows.
type TableContext struct {
	TargetGUID  string                `json:"target"`
	TargetType  string                `json:"type"`
	Engine      string                `json:"engine"`
	Instance    string                `json:"instance"`
	Database    string                `json:"database"`
	Schema      string                `json:"schema,omitempty"`
	Object      string                `json:"object"`
	Columns     []TableContextColumn  `json:"columns"`
	Profiles    []TableContextProfile `json:"profiles,omitempty"`
	Labels      map[string]string     `json:"labels,omitempty"`
	Downstream  int32                 `json:"downstreamConsumers"`
	Upstream    int32                 `json:"upstreamSources"`
	SchemaStale bool                  `json:"schemaChangedRecently,omitempty"`
}

// TableContextReader loads the table context one triage wake reads. Production
// wires it to the registry, profiles, labels and lineage; a test can return a
// fixed value.
type TableContextReader interface {
	ReadTableContext(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*TableContext, error)
}

// DraftChecksFunc drafts checks for one target and returns the queued draft
// titles plus the drafts the pipeline refused. It reuses the same Suggester the
// suggestion center uses, so the drafts land in the review queue; the model
// cannot create enabled checks. The refusals let an agent correct a draft (an
// accepted value that is not one of an enum column's labels, say) instead of
// silently retrying the same intent.
type DraftChecksFunc func(ctx context.Context, targetGUID string, targetType storepb.MetaType, columns []string, intent string, createdBy int32) ([]string, []dataquality.DraftRejection, *storepb.QualitySuggestionQueueSummary, error)

// AgentEvidence is the slice of the quality application layer the evidence tools read
// through. It is the same read the RPC and MCP surfaces use, which is what keeps one
// projection from drifting into three. A nil reader disables the evidence tools
// instead of letting them fall back to an unbounded store read.
type AgentEvidence interface {
	ReadTargetExecutions(ctx context.Context, agentPrincipalID int32, targetGUID string, limit int) ([]dataquality.ExecutionEvidence, error)
	ReadCheckEvidence(ctx context.Context, agentPrincipalID int32, agentID int64, targetGUID string, ruleResourceID string, limit int) (*dataquality.CheckEvidence, error)
}

// AgentDeps is everything the agent tools need from their product.
type AgentDeps struct {
	// Store is the domain store the tools read and write.
	Store AgentStore
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	// TableContext is the triage read capability; nil reports it as not configured.
	TableContext TableContextReader
	// DraftChecks is the triage drafting capability; nil reports it as not configured.
	DraftChecks DraftChecksFunc
	// Evidence is the application-layer evidence read; nil reports it as not configured.
	Evidence AgentEvidence
	// Control applies a state change to one case; nil reports it as not configured.
	Control ControlFunc
	// Run asks the runner to execute one check now; nil reports it as not configured.
	Run RunFunc
	// Propose records one proposed change for the owner; nil reports it as not configured.
	Propose ProposeFunc
	// Revise applies a case's deterministic repair; nil reports it as not configured.
	Revise ReviseFunc
}

// RunOutcome is what one diagnostic run did.
type RunOutcome struct {
	Check     string
	Execution string
	State     string
}

// RunFunc asks the runner to execute one check now.
type RunFunc func(ctx context.Context, agentPrincipalID int32, caseResourceID string) (RunOutcome, error)

// RunCheck re-runs the check a case is about, so a diagnosis can be reproduced or a repair
// verified without waiting for the next scheduled slot.
func (t *AgentTools) RunCheck(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Case string `json:"case"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid run_check arguments")
	}
	if strings.TrimSpace(input.Case) == "" {
		return "", errors.New("run_check needs a case")
	}
	if t.deps.Run == nil {
		return "", errors.New("the run verb is not configured in this process")
	}
	outcome, err := t.deps.Run(ctx, session.AgentPrincipalID, input.Case)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{
		"case":      sessionCaseName(input.Case),
		"check":     outcome.Check,
		"execution": outcome.Execution,
		"state":     outcome.State,
		"note":      "the runner picks the execution up within its tick; the verdict closes or advances the case",
	})
}

// RevisionOutcome is what one repair wrote.
type RevisionOutcome struct {
	Check     string
	Field     string
	From      string
	To        string
	Revision  int32
	Execution string
	State     string
}

// ReviseFunc applies one case's deterministic repair through the application layer. The field and
// value are empty for a repair whose value the server computes.
type ReviseFunc func(ctx context.Context, agentPrincipalID int32, caseResourceID, field, value string) (RevisionOutcome, error)

// RepairCheck applies the repair a case's diagnosis names. Without a field the value is the
// server's — aligning a structure snapshot means writing the columns the registry holds now — and
// the verb asks for the run that verifies it. With a field it is a criterion change, which the
// application layer allows only at a tune-level delegation and only when re-judging the recent runs
// leaves every red run red; at every other level it is a proposal.
func (t *AgentTools) RepairCheck(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Case  string `json:"case"`
		Field string `json:"field"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid repair_check arguments")
	}
	if strings.TrimSpace(input.Case) == "" {
		return "", errors.New("repair_check needs a case")
	}
	if strings.TrimSpace(input.Field) != "" && input.Value == "" {
		return "", errors.New("repair_check needs a value for the field it names")
	}
	if t.deps.Revise == nil {
		return "", errors.New("the repair verb is not configured in this process")
	}
	outcome, err := t.deps.Revise(ctx, session.AgentPrincipalID, input.Case, input.Field, input.Value)
	if err != nil {
		return "", err
	}
	result := map[string]any{
		"case":     sessionCaseName(input.Case),
		"check":    outcome.Check,
		"field":    outcome.Field,
		"to":       outcome.To,
		"revision": outcome.Revision,
		"state":    outcome.State,
		"note":     "the verdict of the next run closes or advances the case",
	}
	if outcome.From != "" {
		result["from"] = outcome.From
	}
	if outcome.Execution != "" {
		result["execution"] = outcome.Execution
	}
	return marshalJSON(result)
}

// ProposalOutcome is what one proposal asked for. It carries no service type, for the same
// reason RunOutcome does not.
type ProposalOutcome struct {
	Check  string
	Field  string
	Value  string
	Effect string
	State  string
}

// ProposeFunc records one proposed change on one case through the application layer.
type ProposeFunc func(ctx context.Context, agentPrincipalID int32, caseResourceID, field, value, rationale string) (ProposalOutcome, error)

// ProposeRevision asks the owner to apply one change the agent may not make itself. The field
// must be the one the failure's diagnosis named; the application layer re-derives the diagnosis
// and refuses any other field, so the model cannot use this verb to redirect a check.
func (t *AgentTools) ProposeRevision(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Case      string `json:"case"`
		Field     string `json:"field"`
		Value     string `json:"value"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid propose_revision arguments")
	}
	if strings.TrimSpace(input.Case) == "" || strings.TrimSpace(input.Field) == "" || input.Value == "" {
		return "", errors.New("propose_revision needs a case, a field and a value")
	}
	if t.deps.Propose == nil {
		return "", errors.New("the propose verb is not configured in this process")
	}
	outcome, err := t.deps.Propose(ctx, session.AgentPrincipalID, input.Case, input.Field, input.Value, input.Rationale)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{
		"case":   sessionCaseName(input.Case),
		"check":  outcome.Check,
		"field":  outcome.Field,
		"value":  outcome.Value,
		"effect": outcome.Effect,
		"state":  outcome.State,
		"note":   "the owner decides whether to apply the change; nothing has been written yet",
	})
}

// AgentTools runs the built-in quality agent's tools.
type AgentTools struct {
	deps AgentDeps
}

// NewAgentTools builds the agent tool set over its product dependencies.
func NewAgentTools(deps AgentDeps) *AgentTools {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &AgentTools{deps: deps}
}

// now returns the configured clock.
func (t *AgentTools) now() time.Time { return t.deps.Now() }

// evidence is the bounded, reference-only context a model wake reads. It never
// contains sample rows: the tools return identifiers, outcomes and counts.
type evidence struct {
	TargetGUID  string
	TargetType  string
	PlanState   string
	Criticality string
	Gaps        []string
	Memories    []string
	RuleCount   int
}

// gatherEvidence assembles the target's plan, rule count and confirmed memories. The
// execution evidence is read through the application layer instead, so it has one
// projection rather than a third one built here.
func (t *AgentTools) gatherEvidence(ctx context.Context, session AgentSession) (*evidence, error) {
	result := &evidence{
		TargetGUID: session.TargetGUID,
		TargetType: session.TargetType.String(),
	}
	rules, err := t.deps.Store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &session.TargetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's rules")
	}
	result.RuleCount = len(rules)
	contract, err := t.deps.Store.GetQualityContract(ctx, session.TargetGUID, session.TargetType)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's contract")
	}
	var contractSettings *storepb.QualityContractSettings
	if contract != nil {
		contractSettings = contract.Settings
	}
	plan := dataquality.DerivePlan(contract != nil, dataquality.ActiveIntent(contractSettings.GetIntent(), t.now()), RuleShapes(rules))
	result.PlanState = planStateLabel(plan.State)
	result.Criticality = criticalityLabel(plan.Criticality)
	for _, requirement := range plan.Requirements {
		if requirement.Satisfied || requirement.Exempted {
			continue
		}
		result.Gaps = append(result.Gaps, GapLabel(requirement))
	}
	memoryLimit := 20
	memories, err := t.deps.Store.ListQualityMemories(ctx, &store.FindQualityMemoryMessage{Subject: &session.TargetGUID, Limit: &memoryLimit})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's memories")
	}
	for _, memory := range memories {
		if memory.Status != storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
			continue
		}
		result.Memories = append(result.Memories, memory.Statement)
	}
	return result, nil
}

// GetTargetSummary reads the target's plan state, criticality, rule count, gaps,
// the agent's adoption status and the target's pending drafts. The adoption
// fields are what let the model attribute a rule-count change to its own adoption
// pass instead of guessing that a person approved it.
func (t *AgentTools) GetTargetSummary(ctx context.Context, session AgentSession) (string, error) {
	evidence, err := t.gatherEvidence(ctx, session)
	if err != nil {
		return "", err
	}
	summary := map[string]any{
		"target":      evidence.TargetGUID,
		"type":        evidence.TargetType,
		"planState":   evidence.PlanState,
		"criticality": evidence.Criticality,
		"ruleCount":   evidence.RuleCount,
		"gaps":        emptyIfNil(evidence.Gaps),
	}
	if adoption := t.adoptionView(ctx, session); adoption != nil {
		summary["adoption"] = adoption
	}
	if drafts, count, ok := t.targetPendingDrafts(ctx, session); ok {
		summary["pendingDrafts"] = drafts
		summary["pendingDraftCount"] = count
	}
	return marshalJSON(summary)
}

// adoptionView reads the agent's last-24h adoption outcome. A read failure is
// omitted rather than failing the whole summary: the tool is evidence, not a gate.
func (t *AgentTools) adoptionView(ctx context.Context, session AgentSession) map[string]any {
	if session.AgentID == 0 {
		return nil
	}
	status, err := t.deps.Store.GetQualityAgentAdoptionStatus(ctx, session.AgentID, t.now())
	if err != nil || status == nil {
		return nil
	}
	view := map[string]any{
		"adoptedLast24h":    status.AdoptedLast24h,
		"failedLast24h":     status.FailedLast24h,
		"lastFailureReason": status.LastFailureReason,
	}
	if status.LastFailureTime != nil {
		view["lastFailureTime"] = status.LastFailureTime.UTC().Format(time.RFC3339)
	}
	return view
}

// targetPendingDrafts lists the target's pending rule drafts with the id, check
// type and confidence the model needs to name them in an adoption request. The
// queue itself is bounded, and the model-visible list is capped.
func (t *AgentTools) targetPendingDrafts(ctx context.Context, session AgentSession) ([]map[string]any, int, bool) {
	if session.AgentID == 0 || session.TargetGUID == "" {
		return nil, 0, false
	}
	agentID := session.AgentID
	targetGUID := session.TargetGUID
	limit := 1000
	rows, err := t.deps.Store.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
		AgentID:    &agentID,
		TargetGUID: &targetGUID,
		Statuses:   []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:      []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		Limit:      &limit,
	})
	if err != nil {
		return nil, 0, false
	}
	views := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if len(views) >= 20 {
			break
		}
		view := map[string]any{
			"id":         common.FormatWorkspace(session.WorkspaceID) + "/qualitySuggestions/" + row.ResourceID,
			"title":      row.Payload.GetRule().GetTitle(),
			"confidence": row.Confidence,
		}
		if ruleType, ok := dataquality.RuleTypeForDraftConfig(row.Payload.GetRule().GetConfig()); ok {
			view["ruleType"] = strings.TrimPrefix(ruleType.String(), "QUALITY_RULE_TYPE_")
		}
		views = append(views, view)
	}
	return views, len(rows), true
}

// ListRecentExecutions lists the target's recent executions through the application
// layer. The execution is the fact, so the projection carries the check it belongs to
// and the engine's own failure code and message: a list of statuses and counts alone
// once led the agent to blame the evaluation pipeline for a bound the database refused.
func (t *AgentTools) ListRecentExecutions(ctx context.Context, session AgentSession) (string, error) {
	if t.deps.Evidence == nil {
		return "", errors.New("the evidence read is not configured in this process")
	}
	executions, err := t.deps.Evidence.ReadTargetExecutions(ctx, session.AgentPrincipalID, session.TargetGUID, 0)
	if err != nil {
		return "", err
	}
	if executions == nil {
		executions = []dataquality.ExecutionEvidence{}
	}
	return marshalJSON(map[string]any{"executions": executions})
}

// InspectCheck renders one check's definition and its own executions. It is the tool
// that turns "an execution failed" into "this check, at this revision, with this
// config, failed for this reason": the read that lets a model decide whether the
// check or the data is wrong.
func (t *AgentTools) InspectCheck(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Rule       string `json:"rule"`
		Executions int    `json:"executions"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid inspect_check arguments")
		}
	}
	if t.deps.Evidence == nil {
		return "", errors.New("the evidence read is not configured in this process")
	}
	evidence, err := t.deps.Evidence.ReadCheckEvidence(ctx, session.AgentPrincipalID, session.AgentID, session.TargetGUID, input.Rule, input.Executions)
	if err != nil {
		return "", err
	}
	if evidence == nil {
		return "", errors.Errorf("the target has no check %q", input.Rule)
	}
	return marshalJSON(evidence)
}

// GetMemory lists the target's confirmed memories.
func (t *AgentTools) GetMemory(ctx context.Context, session AgentSession) (string, error) {
	evidence, err := t.gatherEvidence(ctx, session)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"memories": emptyIfNil(evidence.Memories)})
}

// RecordAssetFact records a model-observed fact unless a confirmed one already says
// the same thing.
func (t *AgentTools) RecordAssetFact(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Statement string `json:"statement"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid record_asset_fact arguments")
	}
	statement := strings.TrimSpace(input.Statement)
	if statement == "" {
		return "", errors.New("record_asset_fact needs a statement")
	}
	if err := t.RecordAssetFactStatement(ctx, session, statement); err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"recorded": true})
}

// RecordAssetFactStatement writes one observation as an asset fact unless a
// confirmed memory already says the same thing. It is the shared write path
// behind the model tool and the deterministic survey.
func (t *AgentTools) RecordAssetFactStatement(ctx context.Context, session AgentSession, statement string) error {
	existing, err := t.deps.Store.ListQualityMemories(ctx, &store.FindQualityMemoryMessage{Subject: &session.TargetGUID})
	if err != nil {
		return errors.Wrap(err, "failed to read the target's memories")
	}
	for _, memory := range existing {
		if memory.Statement == statement &&
			memory.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
			return nil
		}
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return errors.Wrap(err, "failed to generate the memory id")
	}
	_, err = t.deps.Store.CreateQualityMemory(ctx, &store.CreateQualityMemoryInput{
		ResourceID: resourceID,
		Subject:    session.TargetGUID,
		Statement:  statement,
		Evidence: &storepb.QualityMemoryEvidenceList{Evidence: []*storepb.QualityMemoryEvidence{
			{Kind: "MODEL", Reference: session.ResourceID},
		}},
		CreatedBy: ptrInt32(session.AgentPrincipalID),
	})
	if err != nil {
		return errors.Wrap(err, "failed to record the asset fact")
	}
	return nil
}

// GetTableContext reads the bounded table context a triage wake uses.
func (t *AgentTools) GetTableContext(ctx context.Context, session AgentSession) (string, error) {
	if t.deps.TableContext == nil {
		return "", errors.New("the table context is not configured in this process")
	}
	target, err := t.deps.TableContext.ReadTableContext(ctx, session.TargetGUID, session.TargetType)
	if err != nil {
		return "", err
	}
	return marshalJSON(target)
}

// DraftChecks drafts checks for the target from the model's intent and columns.
func (t *AgentTools) DraftChecks(ctx context.Context, session AgentSession, arguments string) (string, error) {
	if t.deps.DraftChecks == nil {
		return "", errors.New("check drafting is not configured in this process")
	}
	var input struct {
		Intent  string   `json:"intent"`
		Columns []string `json:"columns"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid draft_checks arguments")
	}
	intent := strings.TrimSpace(input.Intent)
	if intent == "" {
		return "", errors.New("draft_checks needs an intent")
	}
	if len(intent) > 2000 {
		intent = intent[:2000]
	}
	columns := make([]string, 0, len(input.Columns))
	for _, column := range input.Columns {
		column = strings.TrimSpace(column)
		if column == "" {
			continue
		}
		columns = append(columns, column)
		if len(columns) >= maxTriageDraftColumns {
			break
		}
	}
	titles, rejections, summary, err := t.deps.DraftChecks(ctx, session.TargetGUID, session.TargetType, columns, intent, session.AgentPrincipalID)
	if err != nil {
		return "", err
	}
	result := map[string]any{"queued": len(titles), "drafts": titles}
	if skipped := suggestionSkipCounts(summary); len(skipped) > 0 {
		// A draft that was valid but not queued (the fact exists, an equivalent
		// proposal is pending, or it was already decided) is an honest zero: the
		// model must see why instead of repeating a correct intent.
		result["skipped"] = skipped
	}
	if len(rejections) > 0 {
		rejected := make([]map[string]string, 0, len(rejections))
		for _, rejection := range rejections {
			rejected = append(rejected, map[string]string{"title": rejection.Title, "reason": rejection.Reason})
		}
		result["rejected"] = rejected
	}
	return marshalJSON(result)
}

// RuleShapes is the plan derivation's view of the live rules: enabled rules only,
// with the column each one reads.
func RuleShapes(rules []*store.QualityRuleMessage) []dataquality.RuleShape {
	shapes := make([]dataquality.RuleShape, 0, len(rules))
	for _, rule := range rules {
		if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			continue
		}
		shapes = append(shapes, dataquality.RuleShape{
			Type: rule.RuleType, Column: dataquality.ConfigColumn(rule.Config), Enabled: true,
		})
	}
	return shapes
}

// GapLabel renders one uncovered requirement.
func GapLabel(requirement dataquality.PlanRequirement) string {
	label := PropertyLabel(requirement.Property)
	if requirement.Column != "" {
		label += "(" + requirement.Column + ")"
	}
	return label
}

// PropertyLabel renders one coverage property for a person and the model.
func PropertyLabel(property dataquality.CoverageProperty) string {
	switch property {
	case dataquality.CoverageVolume:
		return "volume"
	case dataquality.CoverageFreshness:
		return "freshness"
	case dataquality.CoverageUniqueness:
		return "uniqueness"
	case dataquality.CoverageSchemaStability:
		return "schema_stability"
	case dataquality.CoverageNullRate:
		return "null_rate"
	case dataquality.CoverageAcceptedValues:
		return "accepted_values"
	case dataquality.CoverageFormat:
		return "format"
	case dataquality.CoverageRange:
		return "range"
	default:
		return "unknown"
	}
}

// planStateLabel renders a plan state for the model.
func planStateLabel(state dataquality.PlanState) string {
	if state == dataquality.PlanPlanned {
		return "PLANNED"
	}
	return "UNPLANNED"
}

// criticalityLabel renders the target's plan criticality for the model.
func criticalityLabel(criticality dataquality.AssetCriticality) string {
	switch criticality {
	case dataquality.CriticalityCritical:
		return "CRITICAL"
	case dataquality.CriticalityImportant:
		return "IMPORTANT"
	default:
		return "EXPERIMENTAL"
	}
}

// marshalJSON renders a tool result.
func marshalJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// emptyIfNil turns a nil slice into an empty one so JSON carries [] not null.
func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// ptrInt32 returns a pointer to value.
func ptrInt32(value int32) *int32 { return &value }

// ControlOutcome is what one state change did, in the shape the tool reports. It carries no
// service type so the application layer stays free to change its own response struct.
type ControlOutcome struct {
	Check    string
	State    string
	Revision int32
	Reason   string
}

// ControlFunc applies one control action to the case the model named. A nil func reports the
// capability as not configured, in the same way the other product tools do.
type ControlFunc func(ctx context.Context, agentPrincipalID int32, caseResourceID, action string) (ControlOutcome, error)

// ControlCheck pauses a check through the application layer. The runtime gates it: the case,
// the participation level, the pins and the diagnosis are all re-read server-side, so this tool
// is a request, never the authority.
func (t *AgentTools) ControlCheck(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Case   string `json:"case"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid control_check arguments")
	}
	if strings.TrimSpace(input.Case) == "" || strings.TrimSpace(input.Action) == "" {
		return "", errors.New("control_check needs a case and an action")
	}
	if t.deps.Control == nil {
		return "", errors.New("the control verb is not configured in this process")
	}
	outcome, err := t.deps.Control(ctx, session.AgentPrincipalID, input.Case, input.Action)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{
		"case":     sessionCaseName(input.Case),
		"check":    outcome.Check,
		"state":    outcome.State,
		"revision": outcome.Revision,
		"reason":   outcome.Reason,
	})
}

// sessionCaseName keeps the model's case reference as it wrote it, so the result and its request
// name the same thing; a bare id is returned unchanged.
func sessionCaseName(name string) string {
	trimmed := strings.TrimSpace(name)
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}
