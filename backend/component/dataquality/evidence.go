package dataquality

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxEvidenceErrorMessageBytes bounds the engine message one execution evidence
// carries. The stored message is already bounded at 1 KiB; a reader of the evidence
// needs the sentence that names the fault, not the whole driver dump, and the tool
// result as a whole has a hard budget.
const maxEvidenceErrorMessageBytes = 240

// ExecutionEvidence is one execution as a reader needs it to decide whether the
// check or the data is wrong. It is the model-facing evidence shape: identifiers,
// the engine's own failure code and message, counts and timings — never sample
// values.
type ExecutionEvidence struct {
	// Execution is the resource name "qualityExecutions/<id>".
	Execution string `json:"execution"`
	// Rule and Title name the check that ran. A reader that cannot say which check
	// failed can only guess at the cause.
	Rule  string `json:"rule"`
	Title string `json:"title"`
	// Status is the verdict without the proto enum prefix: PASS, FAIL, ERROR, ...
	Status string `json:"status"`
	// ErrorCode is the stable class (BAD_VALUE, TIMEOUT, ...) and ErrorMessage the
	// engine's own text for the classes that keep it.
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	FailedRows   int64  `json:"failedRows"`
	TotalRows    int64  `json:"totalRows"`
	Revision     int32  `json:"revision"`
	// Trigger is SCHEDULED or MANUAL; Attempt distinguishes a retry of an abandoned
	// attempt from the first one.
	Trigger    string    `json:"trigger"`
	Attempt    int32     `json:"attempt"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	DurationMS int32     `json:"durationMs"`
	Sampled    bool      `json:"sampled,omitempty"`
	Truncated  bool      `json:"truncated,omitempty"`
	// ObservedValue is omitted when the check produced no value; zero is a real one.
	ObservedValue *float64 `json:"observedValue,omitempty"`
	ObservedKind  string   `json:"observedKind,omitempty"`
}

// BuildExecutionEvidence projects executions onto the model-facing shape. rules
// resolves a rule id to its resource name and title; an execution whose rule is not
// in the map still renders, with an empty name, because the execution is the fact
// and its rule may have been archived since.
func BuildExecutionEvidence(executions []*store.QualityExecutionMessage, rules []*store.QualityRuleMessage) []ExecutionEvidence {
	if len(executions) == 0 {
		return nil
	}
	byID := make(map[int64]*store.QualityRuleMessage, len(rules))
	for _, rule := range rules {
		if rule != nil {
			byID[rule.ID] = rule
		}
	}
	evidence := make([]ExecutionEvidence, 0, len(executions))
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		view := ExecutionEvidence{
			Execution:     fmt.Sprintf("qualityExecutions/%d", execution.ID),
			Status:        statusLabel(execution.Status),
			ErrorCode:     execution.ErrorCode,
			ErrorMessage:  common.TruncateUTF8Bytes(execution.ErrorMessage, maxEvidenceErrorMessageBytes),
			FailedRows:    execution.FailedRows,
			TotalRows:     execution.TotalRows,
			Revision:      execution.RuleRevision,
			Trigger:       triggerLabel(execution.Trigger),
			Attempt:       execution.Attempt,
			StartedAt:     execution.StartedAt.UTC(),
			DurationMS:    execution.DurationMS,
			Sampled:       execution.Sampled,
			Truncated:     execution.Truncated,
			ObservedValue: execution.ObservedValue,
			ObservedKind:  observedKindLabel(execution.ObservedKind),
		}
		if !execution.FinishedAt.IsZero() {
			view.FinishedAt = execution.FinishedAt.UTC()
		}
		if rule := byID[execution.RuleID]; rule != nil {
			view.Rule = "qualityRules/" + rule.ResourceID
			view.Title = rule.Title
		}
		evidence = append(evidence, view)
	}
	return evidence
}

// statusLabel strips the proto enum prefix so a reader sees "ERROR", not
// "QUALITY_STATUS_ERROR".
func statusLabel(status storepb.QualityStatus) string {
	return strings.TrimPrefix(status.String(), "QUALITY_STATUS_")
}

// triggerLabel strips the proto enum prefix from the execution trigger.
func triggerLabel(trigger storepb.QualityExecutionTrigger) string {
	return strings.TrimPrefix(trigger.String(), "QUALITY_EXECUTION_TRIGGER_")
}

// observedKindLabel strips the proto enum prefix from the observed value's unit.
func observedKindLabel(kind storepb.QualityObservedKind) string {
	if kind == storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_UNSPECIFIED {
		return ""
	}
	return strings.TrimPrefix(kind.String(), "QUALITY_OBSERVED_KIND_")
}

// maxInspectConfigBytes bounds the definition a check inspection renders. A config
// larger than this is a custom SQL statement, which the agent cannot rewrite anyway,
// so the read reports the omission instead of a string a result budget would cut in
// half.
const maxInspectConfigBytes = 4 << 10

// CheckDefinition is one check's definition as an inspecting reader needs it: the
// head revision, the fields that decide how strict it is, and the config itself.
type CheckDefinition struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	State       string `json:"state"`
	Severity    string `json:"severity"`
	Revision    int32  `json:"revision"`
	Description string `json:"description,omitempty"`
	// Config is the protojson rendering of the config; it is omitted, with
	// ConfigOmitted explaining why, when the rendering exceeds the inspection budget.
	Config        string `json:"config,omitempty"`
	ConfigOmitted string `json:"configOmitted,omitempty"`
}

// CheckEvidence is one check with its own executions and, when the newest run did not
// produce a verdict, the deterministic diagnosis of it: the unit an inspection reads.
type CheckEvidence struct {
	Target    string          `json:"target"`
	Rule      CheckDefinition `json:"rule"`
	Diagnosis *Diagnosis      `json:"diagnosis,omitempty"`
	// AllowedActions is the diagnosis's class-based list filtered by the agent's delegation, the
	// check's pins and the case's state. It is computed by the same policy the verbs enforce, so
	// the model never reads a permission it does not have.
	AllowedActions []RepairAction      `json:"allowedActions,omitempty"`
	Executions     []ExecutionEvidence `json:"executions"`
}

// BuildCheckEvidence renders one check's definition and executions. rules resolves
// the execution rows' rule names, so a caller passes the target's whole rule set.
func BuildCheckEvidence(targetGUID string, rule *store.QualityRuleMessage, executions []*store.QualityExecutionMessage, rules []*store.QualityRuleMessage) *CheckEvidence {
	if rule == nil {
		return nil
	}
	evidence := &CheckEvidence{
		Target:     targetGUID,
		Rule:       BuildCheckDefinition(rule),
		Executions: BuildExecutionEvidence(executions, rules),
	}
	if evidence.Executions == nil {
		evidence.Executions = []ExecutionEvidence{}
	}
	return evidence
}

// BuildCheckDefinition renders one check's definition.
func BuildCheckDefinition(rule *store.QualityRuleMessage) CheckDefinition {
	definition := CheckDefinition{
		Name:        "qualityRules/" + rule.ResourceID,
		Title:       rule.Title,
		Type:        strings.TrimPrefix(rule.RuleType.String(), "QUALITY_RULE_TYPE_"),
		State:       strings.TrimPrefix(rule.State.String(), "QUALITY_RULE_STATE_"),
		Severity:    strings.TrimPrefix(rule.Severity.String(), "QUALITY_SEVERITY_"),
		Revision:    rule.Revision,
		Description: rule.Description,
	}
	if rule.Config == nil {
		return definition
	}
	encoded, err := protojson.Marshal(rule.Config)
	if err != nil {
		definition.ConfigOmitted = "the definition could not be rendered"
		return definition
	}
	if len(encoded) > maxInspectConfigBytes {
		definition.ConfigOmitted = "the definition is larger than the inspection budget"
		return definition
	}
	definition.Config = string(encoded)
	return definition
}

// SelectInspectedRule resolves the check an inspection is about: the named one, or
// the check behind the newest failed execution when the caller named none. A caller
// that names a check the target does not have gets nil.
func SelectInspectedRule(rules []*store.QualityRuleMessage, executions []*store.QualityExecutionMessage, name string) *store.QualityRuleMessage {
	wanted := strings.TrimPrefix(strings.TrimSpace(name), "qualityRules/")
	byID := make(map[int64]*store.QualityRuleMessage, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		byID[rule.ID] = rule
		if wanted != "" && rule.ResourceID == wanted {
			return rule
		}
	}
	if wanted != "" {
		return nil
	}
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		rule := byID[execution.RuleID]
		if rule == nil {
			continue
		}
		if execution.Status == storepb.QualityStatus_QUALITY_STATUS_ERROR || execution.Status == storepb.QualityStatus_QUALITY_STATUS_FAIL {
			return rule
		}
	}
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		if rule := byID[execution.RuleID]; rule != nil {
			return rule
		}
	}
	return nil
}
