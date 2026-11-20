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
	"fmt"
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
// titles. It reuses the same Suggester the suggestion center uses, so the drafts
// land in the review queue; the model cannot create enabled checks.
type DraftChecksFunc func(ctx context.Context, targetGUID string, targetType storepb.MetaType, columns []string, intent string, createdBy int32) ([]string, error)

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
	Executions  []string
	Memories    []string
	RuleCount   int
}

// gatherEvidence assembles the target's plan, recent executions and confirmed memories.
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
	limit := 5
	executions, err := t.deps.Store.ListQualityExecutions(ctx, &store.FindQualityExecutionMessage{TargetGUID: &session.TargetGUID, Limit: &limit})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's executions")
	}
	for _, execution := range executions {
		result.Executions = append(result.Executions, fmt.Sprintf("%s %s (%d failed rows)",
			executionIDName(execution.ID), execution.Status, execution.FailedRows))
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

// ListRecentExecutions lists the target's recent executions.
func (t *AgentTools) ListRecentExecutions(ctx context.Context, session AgentSession) (string, error) {
	evidence, err := t.gatherEvidence(ctx, session)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"executions": emptyIfNil(evidence.Executions)})
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
	titles, err := t.deps.DraftChecks(ctx, session.TargetGUID, session.TargetType, columns, intent, session.AgentPrincipalID)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"queued": len(titles), "drafts": titles})
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

// executionIDName renders an execution resource name for a tool result.
func executionIDName(id int64) string {
	return fmt.Sprintf("qualityExecutions/%d", id)
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
