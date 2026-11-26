package dataquality

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// FailureClass is the closed set of reasons an execution did not produce a verdict.
// It is computed from the execution, its rule and the registry, never from a model:
// the model chooses among the actions a class permits, it does not invent the class.
type FailureClass string

const (
	// ClassConfigValueInvalid means the statement ran and the engine refused one bound
	// value (BAD_VALUE). It is the class that names a config field.
	ClassConfigValueInvalid FailureClass = "CONFIG_VALUE_INVALID"
	// ClassConfigUnsupported means the rule asks for something this column or engine
	// cannot do: an unsupported type, a missing column, a capability the type lacks.
	ClassConfigUnsupported FailureClass = "CONFIG_UNSUPPORTED"
	// ClassStructureDrift means the target's structure moved since the revision was
	// saved, so the rule may be reading a column that changed.
	ClassStructureDrift FailureClass = "STRUCTURE_DRIFT"
	// ClassNoVerdictEmpty means there was nothing to judge: the target holds no rows.
	ClassNoVerdictEmpty FailureClass = "NO_VERDICT_EMPTY"
	// ClassNoVerdictNoBaseline means a comparison check had no baseline to compare
	// against: row_count_change without a recent run, segment_check without history.
	ClassNoVerdictNoBaseline FailureClass = "NO_VERDICT_NO_BASELINE"
	// ClassNoVerdictNoValue means the check ran over rows but the observation was NULL:
	// an aggregate with no rows, MAX(timestamp) on an empty set, or a filter that matched
	// nothing.
	ClassNoVerdictNoValue FailureClass = "NO_VERDICT_NO_VALUE"
	// ClassStuckInflight means the execution was skipped because another execution of
	// the same rule was in flight. A repeated skip is a stall, not a verdict.
	ClassStuckInflight FailureClass = "STUCK_INFLIGHT"
	// ClassSourceFault means the target's data source failed: connection, credentials,
	// permission, timeout, no READ_ONLY source. Infrastructure, never a rule defect.
	ClassSourceFault FailureClass = "SOURCE_FAULT"
	// ClassDataFailure means the check worked and the data failed. It is a conclusion,
	// never something a repair may change.
	ClassDataFailure FailureClass = "DATA_FAILURE"
	// ClassExecutionFault means the statement failed for a reason nothing else explains.
	ClassExecutionFault FailureClass = "EXECUTION_FAULT"
)

// RepairAction is one verb a diagnosis permits. The policy then filters these by the
// agent's participation, the check's pins and the target's scope; a class alone never
// authorizes a write.
type RepairAction string

const (
	// ActionReviseField supplies a valid value for the one config field a diagnosis
	// names. It is the only field-level write a `repair`-level delegation may make.
	ActionReviseField RepairAction = "REVISE_FIELD"
	// ActionRunNow asks for one immediate execution: the diagnostic action.
	ActionRunNow RepairAction = "RUN_NOW"
	// ActionControl pauses, resumes or archives the check. Pausing a check that cannot
	// produce a verdict only stops the noise; archiving is a `tune`-level verb.
	ActionControl RepairAction = "CONTROL"
	// ActionProposeRevision records a suggested patch for a person, without writing the
	// rule. It is the only action for a check the agent does not own or may not change.
	ActionProposeRevision RepairAction = "PROPOSE_REVISION"
	// ActionRecord records a verified fact.
	ActionRecord RepairAction = "RECORD"
	// ActionEscalate asks a person to decide.
	ActionEscalate RepairAction = "ESCALATE"
	// ActionResolveEmpty closes the case as "no verdict possible": the target holds no
	// rows, which is a fact about the target, not a defect in the check.
	ActionResolveEmpty RepairAction = "RESOLVE_EMPTY"
	// ActionAckStructure aligns the saved structure snapshot with the current one.
	ActionAckStructure RepairAction = "ACK_STRUCTURE"
)

// Diagnosis is what one execution means and what may be done about it.
type Diagnosis struct {
	// Class is the closed-set reason.
	Class FailureClass `json:"class"`
	// Summary is one sentence for the model, the transcript and the report.
	Summary string `json:"summary"`
	// Field is the config field the class is about, in dotted form ("range.min"), empty
	// when the class names no field.
	Field string `json:"field,omitempty"`
	// FieldValue is the current value of that field, so a repair can show what it is
	// changing.
	FieldValue string `json:"fieldValue,omitempty"`
	// Actions are the verbs the class permits. The policy filters them further.
	Actions []RepairAction `json:"actions"`
	// Repairable reports whether a field repair is on the table at all.
	Repairable bool `json:"repairable"`
	// Evidence names the execution the diagnosis is about.
	Evidence string `json:"evidence,omitempty"`
}

// IsFactNotWork reports whether a class describes the target rather than a defect in the check, so a
// run that lands in it opens no work item. An empty target is the one case: there is nothing to
// judge, a person cannot make the check work by approving an edit, and every run of it would produce
// the same honest "no verdict". Leaving it as work parks a permanent item that no verb can close.
func IsFactNotWork(class FailureClass) bool {
	for _, fact := range FactNotWorkClasses() {
		if class == fact {
			return true
		}
	}
	return false
}

// FactNotWorkClasses lists the classes that describe the target rather than a defect in the check.
// It is the one list the intake, the sweep and the escalation guard read, so a new fact class
// cannot be settled by one path and escalated by another.
func FactNotWorkClasses() []FailureClass {
	return []FailureClass{ClassNoVerdictEmpty}
}

// FactResolution is the sentence stored on a case a fact settles. It is written by the server, not
// the model: the case history should say why it closed without a person having said anything.
func FactResolution(class FailureClass) string {
	switch class {
	case ClassNoVerdictEmpty:
		return "the target held no rows, so no verdict was possible; closed as a fact about the data, not a defect in the check"
	default:
		return "closed as a fact about the target"
	}
}

// DiagnosisInput is everything one diagnosis is computed from. Everything that is not
// on the execution row is supplied by the caller, so the function stays pure.
type DiagnosisInput struct {
	// Execution is the completed execution to explain.
	Execution *store.QualityExecutionMessage
	// Rule is the head row of the check, used for its type.
	Rule *store.QualityRuleMessage
	// SchemaChanged reports that the target's structure fingerprint moved since the
	// rule's revision was saved. The caller computes it because it is a registry read.
	SchemaChanged bool
	// Now is the reference instant; a zero value disables nothing and is only used by
	// the summary's age wording.
	Now time.Time
}

// DiagnoseExecution explains one completed execution. It returns nil for a verdict that
// needs no case: a PASS is a working check, and only FAIL and the non-verdict states
// are work.
func DiagnoseExecution(input DiagnosisInput) *Diagnosis {
	execution := input.Execution
	if execution == nil {
		return nil
	}
	diagnosis := &Diagnosis{Evidence: fmt.Sprintf("qualityExecutions/%d", execution.ID)}
	switch execution.Status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return nil
	case storepb.QualityStatus_QUALITY_STATUS_FAIL, storepb.QualityStatus_QUALITY_STATUS_WARN:
		diagnosis.Class = ClassDataFailure
		diagnosis.Summary = fmt.Sprintf("the check worked and the data did not meet it: %d of %d rows failed",
			execution.FailedRows, execution.TotalRows)
		diagnosis.Actions = []RepairAction{ActionRunNow, ActionProposeRevision, ActionEscalate}
	case storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		diagnosis.Class = ClassStuckInflight
		diagnosis.Summary = "the execution was skipped, so no verdict was reached; a repeated skip means the rule is stuck"
		diagnosis.Actions = []RepairAction{ActionEscalate}
	case storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		diagnoseNoVerdict(diagnosis, execution, input.Rule)
	case storepb.QualityStatus_QUALITY_STATUS_ERROR:
		diagnoseError(diagnosis, execution, input.Rule, input.SchemaChanged)
	default:
		return nil
	}
	return diagnosis
}

// diagnoseNoVerdict separates the three ways a check can run and still say nothing:
// an empty target, a comparison without a baseline, and an observation that came back
// NULL.
func diagnoseNoVerdict(diagnosis *Diagnosis, execution *store.QualityExecutionMessage, rule *store.QualityRuleMessage) {
	switch {
	case execution.TotalRows == 0:
		diagnosis.Class = ClassNoVerdictEmpty
		diagnosis.Summary = "the target holds no rows, so no verdict is possible; this is a fact about the data, not a defect in the check"
		diagnosis.Actions = []RepairAction{ActionResolveEmpty, ActionRunNow, ActionEscalate}
	case rule != nil && baselineRuleType(rule.RuleType) && execution.BaselineValue == nil:
		diagnosis.Class = ClassNoVerdictNoBaseline
		diagnosis.Summary = "the comparison had no baseline to compare against, so no verdict was reached"
		diagnosis.Actions = []RepairAction{ActionRunNow, ActionProposeRevision, ActionEscalate}
	default:
		diagnosis.Class = ClassNoVerdictNoValue
		diagnosis.Summary = "the check ran over rows but produced no value to judge; a filter or an empty aggregate leaves nothing to compare"
		diagnosis.Actions = []RepairAction{ActionRunNow, ActionProposeRevision, ActionEscalate}
	}
}

// diagnoseError maps the stable error code onto the class. The code is the only
// reliable classification: matching on message text guesses.
func diagnoseError(diagnosis *Diagnosis, execution *store.QualityExecutionMessage, rule *store.QualityRuleMessage, schemaChanged bool) {
	switch execution.ErrorCode {
	case ErrCodeBadValue:
		diagnosis.Class = ClassConfigValueInvalid
		diagnosis.Summary = "the statement ran and the target database refused one bound value: " + strings.TrimSpace(execution.ErrorMessage)
		diagnosis.Repairable = true
		if rule != nil {
			diagnosis.Field, diagnosis.FieldValue = locateConfigField(rule.Config, execution.ErrorMessage)
		}
		diagnosis.Actions = []RepairAction{ActionReviseField, ActionRunNow, ActionProposeRevision, ActionEscalate}
	case ErrCodeUnsupported:
		if schemaChanged {
			diagnosis.Class = ClassStructureDrift
			diagnosis.Summary = "the target's structure changed since this check was saved, so the check may be reading a column that moved"
			diagnosis.Actions = []RepairAction{ActionReviseField, ActionAckStructure, ActionRunNow, ActionEscalate}
			return
		}
		diagnosis.Class = ClassConfigUnsupported
		diagnosis.Summary = "the check asks for something the target cannot do: " + strings.TrimSpace(execution.ErrorMessage)
		diagnosis.Actions = []RepairAction{ActionControl, ActionProposeRevision, ActionEscalate}
	case ErrCodeConnection, ErrCodeAuth, ErrCodeTimeout, ErrCodePermission, ErrCodeNoReadOnly:
		diagnosis.Class = ClassSourceFault
		diagnosis.Summary = "the target's data source failed (" + execution.ErrorCode + "); this is infrastructure, not a defect in the check"
		diagnosis.Actions = []RepairAction{ActionRunNow, ActionEscalate}
	case ErrCodeOrphaned:
		diagnosis.Class = ClassStructureDrift
		diagnosis.Summary = "the check's target is gone, so the check cannot run"
		diagnosis.Actions = []RepairAction{ActionControl, ActionEscalate}
	default:
		if schemaChanged {
			diagnosis.Class = ClassStructureDrift
			diagnosis.Summary = "the target's structure changed since this check was saved, and the execution failed"
			diagnosis.Actions = []RepairAction{ActionReviseField, ActionAckStructure, ActionRunNow, ActionEscalate}
			return
		}
		diagnosis.Class = ClassExecutionFault
		diagnosis.Summary = "the statement failed for a reason this build cannot name: " + strings.TrimSpace(execution.ErrorMessage)
		diagnosis.Actions = []RepairAction{ActionRunNow, ActionProposeRevision, ActionEscalate}
	}
}

// baselineRuleType reports whether a rule type needs a prior run to conclude.
func baselineRuleType(ruleType storepb.QualityRuleType) bool {
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK:
		return true
	default:
		return false
	}
}

// quotedLiteral finds one double-quoted literal inside a driver message. PostgreSQL
// and MySQL both quote the offending value, which is what makes the field findable.
var quotedLiteral = regexp.MustCompile(`"([^"]+)"`)

// The dotted config paths a bound-value failure can be about. They are constants rather than
// literals because two callers must agree on them: locating the field a driver refused, and
// patching it. A field that can be named but not patched would make a proposal impossible, so
// the list lives here and both sides read it.
const (
	configFieldRangeMin       = "range.min"
	configFieldRangeMax       = "range.max"
	configFieldRegex          = "regex.pattern"
	configFieldExpression     = "cel_expression.expression"
	configFieldNullValues     = "null_percent.null_values"
	configFieldAcceptedValues = "accepted_values.values"
	configFieldFilterValues   = "filter.clauses"
)

// configFieldIndex names one element of a list-valued field, e.g. accepted_values.values[2].
func configFieldIndex(path string, index int) string {
	return fmt.Sprintf("%s[%d]", path, index)
}

// configFieldValue is one value-bearing config field with the dotted path a repair
// would name.
type configFieldValue struct {
	path  string
	value string
}

// configFieldValues lists the config values a bound-value failure can be about. The
// order is the order a person would check them, and a duplicate value resolves to the
// first field, which is the one the rule type is actually using.
func configFieldValues(config *storepb.QualityRuleConfig) []configFieldValue {
	if config == nil {
		return nil
	}
	fields := make([]configFieldValue, 0, 8)
	appendValue := func(path, value string) {
		if strings.TrimSpace(value) != "" {
			fields = append(fields, configFieldValue{path: path, value: value})
		}
	}
	appendValue(configFieldRangeMin, config.GetRange().GetMin())
	appendValue(configFieldRangeMax, config.GetRange().GetMax())
	appendValue(configFieldRegex, config.GetRegex().GetPattern())
	appendValue(configFieldExpression, config.GetCelExpression().GetExpression())
	for index, value := range config.GetNullPercent().GetNullValues() {
		appendValue(configFieldIndex(configFieldNullValues, index), value)
	}
	for index, value := range config.GetAcceptedValues().GetValues() {
		appendValue(configFieldIndex(configFieldAcceptedValues, index), value)
	}
	for clauseIndex, clause := range config.GetFilter().GetClauses() {
		for valueIndex, value := range clause.GetValues() {
			appendValue(configFieldIndex(configFieldIndex(configFieldFilterValues, clauseIndex)+".values", valueIndex), value)
		}
	}
	return fields
}

// locateConfigField names the config field whose value the engine refused. It matches
// the literal the driver quoted against the rule's own values, so the diagnosis points
// at the field a person would edit instead of making them read the config by hand.
func locateConfigField(config *storepb.QualityRuleConfig, message string) (field, value string) {
	if config == nil || message == "" {
		return "", ""
	}
	fields := configFieldValues(config)
	if len(fields) == 0 {
		return "", ""
	}
	for _, match := range quotedLiteral.FindAllStringSubmatch(message, -1) {
		literal := match[1]
		for _, candidate := range fields {
			if candidate.value == literal {
				return candidate.path, candidate.value
			}
		}
	}
	// A driver that does not quote the value leaves nothing to match on; the whole
	// message is the only clue, so a single value-bearing field is still worth naming.
	if len(fields) == 1 {
		return fields[0].path, fields[0].value
	}
	return "", ""
}
