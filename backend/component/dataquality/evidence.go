package dataquality

import (
	"fmt"
	"strings"
	"time"

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
