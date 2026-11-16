package quality

// The built-in quality agent's own tools (product-only scope): the target summary,
// recent executions and memories it reads, the investigations/facts/decisions it
// records, and the triage capabilities (table context, check drafts). They are not
// part of the MCP read surface; they run through the same tool contract and
// pipeline as every other tool, and the deterministic wake paths reuse their
// CreateInvestigation/RecordAssetFactStatement helpers.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// maxDecisionStatement matches the API's memory statement bound.
const maxDecisionStatement = 4000

// maxTriageDraftColumns bounds the columns one draft_checks call may scope.
const maxTriageDraftColumns = 8

// MaxTriageDraftColumns is the exported bound the transport's contract tests read.
const MaxTriageDraftColumns = maxTriageDraftColumns

// sessionEvidenceKind is the evidence kind that ties a memory to its session.
const sessionEvidenceKind = "SESSION"

// AgentSession is the session identity the agent tools read and write.
type AgentSession struct {
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
	CreateQualityInvestigation(ctx context.Context, input *store.CreateQualityInvestigationInput) (*store.QualityInvestigationMessage, bool, error)
	CreateQualityMemory(ctx context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error)
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

// GetTargetSummary reads the target's plan state, criticality, rule count and gaps.
func (t *AgentTools) GetTargetSummary(ctx context.Context, session AgentSession) (string, error) {
	evidence, err := t.gatherEvidence(ctx, session)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{
		"target":      evidence.TargetGUID,
		"type":        evidence.TargetType,
		"planState":   evidence.PlanState,
		"criticality": evidence.Criticality,
		"ruleCount":   evidence.RuleCount,
		"gaps":        emptyIfNil(evidence.Gaps),
	})
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

// RecordInvestigation records an investigation with its evidence reference.
func (t *AgentTools) RecordInvestigation(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Summary   string `json:"summary"`
		Verdict   string `json:"verdict"`
		Reference string `json:"reference"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid record_investigation arguments")
	}
	if strings.TrimSpace(input.Summary) == "" {
		return "", errors.New("record_investigation needs a summary")
	}
	investigation := &store.CreateQualityInvestigationInput{
		TargetGUID:         session.TargetGUID,
		TargetType:         session.TargetType,
		Trigger:            storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_MANUAL,
		TriggerReference:   session.ResourceID,
		Status:             storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		Verdict:            verdictFromString(input.Verdict),
		Summary:            input.Summary,
		Evidence:           &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{{Kind: sessionEvidenceKind, Reference: session.ResourceID}}},
		RequestFingerprint: investigationFingerprint(session.TargetGUID, input.Summary, input.Reference),
		CreatedBy:          ptrInt32(session.AgentPrincipalID),
	}
	if input.Reference != "" {
		investigation.Trigger = storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION
		investigation.TriggerReference = input.Reference
		investigation.Evidence.Evidence = append(investigation.Evidence.Evidence,
			&storepb.QualityInvestigationEvidence{Kind: "EXECUTION", Reference: input.Reference})
	}
	message, err := t.CreateInvestigation(ctx, investigation)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"recorded": true, "investigation": message.ResourceID})
}

// CreateInvestigation writes one investigation; the store dedupes by fingerprint.
// It is the shared write path behind the model tool and the deterministic survey.
func (t *AgentTools) CreateInvestigation(ctx context.Context, input *store.CreateQualityInvestigationInput) (*store.QualityInvestigationMessage, error) {
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the investigation id")
	}
	input.ResourceID = resourceID
	message, _, err := t.deps.Store.CreateQualityInvestigation(ctx, input)
	if err != nil {
		return nil, errors.Wrap(err, "failed to record the investigation")
	}
	return message, nil
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
		Kind:       storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT,
		Subject:    session.TargetGUID,
		Statement:  statement,
		Source:     storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		Status:     storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED,
		Evidence: &storepb.QualityMemoryEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
			{Kind: "MODEL", Reference: session.ResourceID},
		}},
		CreatedBy: ptrInt32(session.AgentPrincipalID),
	})
	if err != nil {
		return errors.Wrap(err, "failed to record the asset fact")
	}
	return nil
}

// RecordDecision writes the model's conclusion as a PROPOSED decision memory. It
// never confirms itself: a person turns it into knowledge.
func (t *AgentTools) RecordDecision(ctx context.Context, session AgentSession, arguments string) (string, error) {
	var input struct {
		Statement string `json:"statement"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid record_decision arguments")
	}
	statement := strings.TrimSpace(input.Statement)
	if statement == "" {
		return "", errors.New("record_decision needs a statement")
	}
	statement = truncateRunes(statement, maxDecisionStatement)
	kind := storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_DECISION
	existing, err := t.deps.Store.ListQualityMemories(ctx, &store.FindQualityMemoryMessage{Subject: &session.TargetGUID, Kind: &kind})
	if err != nil {
		return "", errors.Wrap(err, "failed to read the target's memories")
	}
	for _, memory := range existing {
		if memory.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_REJECTED {
			continue
		}
		if memory.ExpiresAt != nil && !memory.ExpiresAt.After(t.now()) {
			continue
		}
		if memory.Statement == statement {
			return marshalJSON(map[string]any{"recorded": true})
		}
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate the memory id")
	}
	_, err = t.deps.Store.CreateQualityMemory(ctx, &store.CreateQualityMemoryInput{
		ResourceID: resourceID,
		Kind:       storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_DECISION,
		Subject:    session.TargetGUID,
		Statement:  statement,
		Source:     storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		Status:     storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_PROPOSED,
		Evidence: &storepb.QualityMemoryEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
			{Kind: sessionEvidenceKind, Reference: session.ResourceID},
		}},
		CreatedBy: ptrInt32(session.AgentPrincipalID),
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to record the decision memory")
	}
	return marshalJSON(map[string]any{"recorded": true})
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

// investigationFingerprint identifies one investigation deterministically, so a
// retried wake cannot record it twice.
func investigationFingerprint(targetGUID, summary, reference string) string {
	sum := sha256.Sum256([]byte("agent-investigation|" + targetGUID + "|" + reference + "|" + summary))
	return "agent-inv:" + hex.EncodeToString(sum[:])
}

// executionIDName renders an execution resource name for a tool result.
func executionIDName(id int64) string {
	return fmt.Sprintf("qualityExecutions/%d", id)
}

// verdictFromString maps the model's verdict word to the stored enum.
func verdictFromString(value string) storepb.QualityInvestigationVerdict {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "root_cause":
		return storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE
	case "contributing_factor":
		return storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_CONTRIBUTING_FACTOR
	default:
		return storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_INCONCLUSIVE
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

// truncateRunes bounds a string at rune granularity so a multi-byte character is
// never split; protojson rejects invalid UTF-8.
func truncateRunes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
