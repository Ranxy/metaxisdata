package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func diagnosisRule(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) *store.QualityRuleMessage {
	return &store.QualityRuleMessage{ID: 460, ResourceID: "r460", Title: "a check", RuleType: ruleType, Config: config}
}

func diagnosisExecution(status storepb.QualityStatus) *store.QualityExecutionMessage {
	return &store.QualityExecutionMessage{ID: 4339, RuleID: 460, State: storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, Status: status}
}

// The sample: a range bound of "now - 1 day" that the database refused with SQLSTATE
// 22007. The diagnosis must name the class and the exact field, because that pair is
// what a repair acts on and what a person needs to see when it cannot.
func TestDiagnoseExecutionNamesTheRefusedConfigField(t *testing.T) {
	t.Parallel()

	lower := "now - 1 day"
	execution := diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_ERROR)
	execution.ErrorCode = ErrCodeBadValue
	execution.ErrorMessage = `ERROR: invalid input syntax for type timestamp with time zone: "now - 1 day" (SQLSTATE 22007)`
	rule := diagnosisRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{
			Column: "performed_at", Min: &lower,
		}}})

	diagnosis := DiagnoseExecution(DiagnosisInput{Execution: execution, Rule: rule, Now: time.Now()})
	require.NotNil(t, diagnosis)
	assert.Equal(t, ClassConfigValueInvalid, diagnosis.Class)
	assert.Equal(t, "range.min", diagnosis.Field)
	assert.Equal(t, "now - 1 day", diagnosis.FieldValue)
	assert.True(t, diagnosis.Repairable)
	assert.Contains(t, diagnosis.Actions, ActionReviseField)
	assert.Equal(t, "qualityExecutions/4339", diagnosis.Evidence)
	assert.Contains(t, diagnosis.Summary, "now - 1 day")
}

// A pass is a working check; only FAIL and the non-verdict states are work for the
// agent. WARN is a conclusion about the data too.
func TestDiagnoseExecutionSkipsAPassAndClassifiesAFail(t *testing.T) {
	t.Parallel()

	assert.Nil(t, DiagnoseExecution(DiagnosisInput{Execution: diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_PASS)}))

	failed := diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_FAIL)
	failed.FailedRows, failed.TotalRows = 3, 10
	diagnosis := DiagnoseExecution(DiagnosisInput{Execution: failed, Rule: diagnosisRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, nil)})
	require.NotNil(t, diagnosis)
	assert.Equal(t, ClassDataFailure, diagnosis.Class)
	assert.Contains(t, diagnosis.Summary, "3 of 10")
	// A data failure is a conclusion: no field repair is ever on the table.
	assert.NotContains(t, diagnosis.Actions, ActionReviseField)
}

// An empty target, a missing baseline and a NULL observation are three different
// answers. Collapsing them is what made the agent read "the evaluation pipeline is
// broken" into an audit table that simply had no rows.
func TestDiagnoseExecutionSeparatesTheWaysToHaveNoVerdict(t *testing.T) {
	t.Parallel()

	empty := diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED)
	empty.TotalRows = 0
	diagnosis := DiagnoseExecution(DiagnosisInput{Execution: empty})
	require.NotNil(t, diagnosis)
	assert.Equal(t, ClassNoVerdictEmpty, diagnosis.Class)
	assert.Contains(t, diagnosis.Actions, ActionResolveEmpty)

	baseline := diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED)
	baseline.TotalRows = 500
	diagnosis = DiagnoseExecution(DiagnosisInput{
		Execution: baseline,
		Rule:      diagnosisRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, nil),
	})
	require.NotNil(t, diagnosis)
	assert.Equal(t, ClassNoVerdictNoBaseline, diagnosis.Class)

	noValue := diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED)
	noValue.TotalRows = 500
	diagnosis = DiagnoseExecution(DiagnosisInput{
		Execution: noValue,
		Rule:      diagnosisRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, nil),
	})
	require.NotNil(t, diagnosis)
	assert.Equal(t, ClassNoVerdictNoValue, diagnosis.Class)
}

// Infrastructure failures are never a rule defect, so they carry no repair at all.
func TestDiagnoseExecutionClassifiesSourceFaults(t *testing.T) {
	t.Parallel()

	for _, code := range []string{ErrCodeConnection, ErrCodeAuth, ErrCodeTimeout, ErrCodePermission, ErrCodeNoReadOnly} {
		execution := diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_ERROR)
		execution.ErrorCode = code
		diagnosis := DiagnoseExecution(DiagnosisInput{Execution: execution})
		require.NotNil(t, diagnosis, code)
		assert.Equal(t, ClassSourceFault, diagnosis.Class, code)
		assert.NotContains(t, diagnosis.Actions, ActionReviseField, code)
	}
}

// A structure change explains an otherwise unexplainable execution failure, and it is
// the one case where the saved snapshot is the thing to fix.
func TestDiagnoseExecutionPrefersStructureDriftWhenTheShapeMoved(t *testing.T) {
	t.Parallel()

	execution := diagnosisExecution(storepb.QualityStatus_QUALITY_STATUS_ERROR)
	execution.ErrorCode = ErrCodeExecution
	diagnosis := DiagnoseExecution(DiagnosisInput{Execution: execution, SchemaChanged: true})
	require.NotNil(t, diagnosis)
	assert.Equal(t, ClassStructureDrift, diagnosis.Class)
	assert.Contains(t, diagnosis.Actions, ActionAckStructure)
}

// A driver that does not quote the value still leaves a findable field when the rule
// has exactly one value-bearing field; a rule with several is left unnamed rather than
// guessed at.
func TestLocateConfigField(t *testing.T) {
	t.Parallel()

	one := "2026-01-01"
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{Min: &one}}}
	field, value := locateConfigField(config, `invalid input syntax for type timestamp: "2026-01-01"`)
	assert.Equal(t, "range.min", field)
	assert.Equal(t, "2026-01-01", value)

	field, value = locateConfigField(config, "some unquoted driver message")
	assert.Equal(t, "range.min", field)
	assert.Equal(t, "2026-01-01", value)

	two := "2026-12-31"
	twoBounds := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{Min: &one, Max: &two}}}
	field, _ = locateConfigField(twoBounds, "some unquoted driver message")
	assert.Empty(t, field, "two candidates and no quoted value is a guess, not a diagnosis")
}

// The fact classes are the one list the intake, the sweep and the escalation guard read. A class
// that is a fact without a stored resolution would close a case with an empty history, and a class
// missing from the list would reopen the escalation path the redesign closed.
func TestFactNotWorkClassesAreSettledWithAReason(t *testing.T) {
	t.Parallel()

	classes := FactNotWorkClasses()
	require.Contains(t, classes, ClassNoVerdictEmpty)
	require.True(t, IsFactNotWork(ClassNoVerdictEmpty))
	require.False(t, IsFactNotWork(ClassConfigUnsupported),
		"a definition that cannot run is a defect a person or a repair can act on")
	require.False(t, IsFactNotWork(ClassDataFailure), "a failed check concluded; it is not a fact")
	require.False(t, IsFactNotWork(""))
	for _, class := range classes {
		require.NotEmpty(t, FactResolution(class), "class %s needs a sentence for the case history", class)
	}
	require.NotEqual(t, FactResolution(ClassNoVerdictEmpty), FactResolution(""),
		"an unknown class gets the generic sentence")
}

// The report read spells the empty-target class itself because the store cannot import this
// package; the two spellings must stay one fact.
func TestTheStoreSpellingOfTheEmptyClassMatchesTheClass(t *testing.T) {
	t.Parallel()

	require.Equal(t, string(ClassNoVerdictEmpty), store.QualityAgentFailureClassNoVerdictEmpty)
}
