package dataquality

import (
	"slices"
	"strconv"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// ruleTypeSpec is the single description of one quality rule type. Every per-type
// list in this package derives from ruleTypeSpecs: the config arm, the suggestion
// identity, the object and engine gates, the PERCENT gate, the per-type
// compile/validate/judge functions, the failure-sample and aggregate-observation
// readers, the label vocabulary and the deterministic label drafts.
//
// Adding a rule type means adding its enum value and one row here. The compiler,
// validator and judge functions stay where they are; a row references them. The
// completeness test fails until the row carries everything a live type needs.
type ruleTypeSpec struct {
	// Type is the enum value this row describes; it is the row's key.
	Type storepb.QualityRuleType
	// Title is the label-suggestion title; empty means the type is not suggested.
	Title string

	// Arm reports whether the config's oneof carries this type's arm. It is a type
	// assertion rather than a nil getter check, so a malformed config that sets the
	// arm to a nil message still maps to its type the way it always has.
	Arm func(*storepb.QualityRuleConfig) bool
	// CheckedColumn is the single column the rule checks, or "" for table-level
	// and multi-column rules (ConfigColumn).
	CheckedColumn func(*storepb.QualityRuleConfig) string
	// IdentityColumns and Discriminators are the suggestion identity ingredients
	// (RuleSignature).
	IdentityColumns func(*storepb.QualityRuleConfig) []string
	Discriminators  func(*storepb.QualityRuleConfig) []string

	// Views reports whether the rule may watch a VIEW or MATERIALIZED_VIEW. Every
	// type may watch a table.
	Views bool
	// Metadata marks a rule that observes the registry instead of the target
	// database, so it needs no read-only source and compiles to no SQL.
	Metadata bool
	// EngineRefusals lists engines that cannot run this type, with the save-time
	// reason (Supported).
	EngineRefusals map[storepb.Engine]string
	// Percent reports whether threshold_metric PERCENT has a denominator for this
	// type (ruleTypeAllowsPercent).
	Percent bool

	// Compiler produces the observation statement (Compile).
	Compiler compilerFunc
	// Validator is the pure per-type config check (ValidateConfig).
	Validator func(ResolvedTarget, *storepb.QualityRuleConfig) error

	// Thresholds reads a count-shaped rule's fail/warn bounds; Judge is set by the
	// other rules instead. CountsTotal is true when the observation carries a
	// filtered total a PERCENT threshold can divide by.
	Thresholds  func(*storepb.QualityRuleConfig) (fail, warn *float64)
	CountsTotal bool
	Judge       func(*storepb.QualityRuleConfig, Observation, storepb.QualityEmptyBehavior) ResultState

	// Sample builds the failed-row sample; nil means the rule has no failing row
	// to show (CompileFailureRows).
	Sample func(CompileInput, int) (CompiledQuery, error)
	// Observe reads the aggregate row; nil means the type never has an aggregate
	// observation (observationFromAggregate).
	Observe func(*db.QueryResult, map[string]int) (Observation, error)

	// LabelDraft builds the deterministic draft config for a semantic label; nil
	// means no default draft exists for this type.
	LabelDraft func(storepb.QualitySemanticType, string, *storepb.ColumnProfile) *storepb.QualityRuleConfig
}

// ruleTypeSpecs is the closed catalog of rule types this build can describe, in
// proto enum order.
var ruleTypeSpecs = []ruleTypeSpec{
	{
		Type:     storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Title:    "row count",
		Arm:      armRowCount,
		Views:    true,
		Compiler: compileRowCount,
		Validator: func(_ ResolvedTarget, config *storepb.QualityRuleConfig) error {
			return validateRowCountConfig(config.GetRowCount())
		},
		Judge:   evaluateRowCount,
		Observe: observeRowCount,
	},
	{
		Type:     storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		Title:    "row count change",
		Arm:      armRowCountChange,
		Views:    true,
		Compiler: compileRowCountChange,
		Validator: func(_ ResolvedTarget, config *storepb.QualityRuleConfig) error {
			return validateRowCountChangeConfig(config.GetRowCountChange())
		},
		Judge:   evaluateRowCountChange,
		Observe: observeRowCount,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Title: "null percent",
		Arm:   armNullPercent,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetNullPercent().GetColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetNullPercent().GetColumn())
		},
		Compiler: compileNullPercent,
		Validator: func(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
			return validateNullPercentConfig(target, config.GetNullPercent())
		},
		Judge:      evaluateNullPercent,
		Sample:     sampleNullPercent,
		Observe:    countObservation,
		LabelDraft: labelDraftNullPercent,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Title: "unique",
		Arm:   armUnique,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetUnique().GetColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetUnique().GetColumn())
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			unique := config.GetUnique()
			if unique == nil || !unique.GetCountNulls() {
				return nil
			}
			// Counting NULL as a value is a different rule, not a threshold variant.
			return []string{"count_nulls=true"}
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			unique := config.GetUnique()
			if unique == nil {
				return nil, nil
			}
			return unique.FailThreshold, unique.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileUnique,
		Validator:   validateUniqueConfig,
		Sample:      sampleUnique,
		Observe:     countObservation,
		LabelDraft:  labelDraftUnique,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		Title: "unique combination",
		Arm:   armUniqueCombination,
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return config.GetUniqueCombination().GetColumns()
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			combination := config.GetUniqueCombination()
			if combination == nil || !combination.GetAllColumns() {
				return nil
			}
			return []string{"all_columns=true"}
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			combination := config.GetUniqueCombination()
			if combination == nil {
				return nil, nil
			}
			return combination.FailThreshold, combination.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileUniqueCombination,
		Validator:   validateUniqueCombinationConfig,
		Sample:      sampleUnique,
		Observe:     countObservation,
		LabelDraft:  labelDraftUniqueCombination,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		Title: "accepted values",
		Arm:   armAcceptedValues,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetAcceptedValues().GetColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetAcceptedValues().GetColumn())
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			accepted := config.GetAcceptedValues()
			if accepted == nil {
				return nil
			}
			values := append([]string(nil), accepted.GetValues()...)
			slices.Sort(values)
			return []string{
				"values=" + suggestionSignatureHash(strings.Join(slices.Compact(values), "\x00")),
				"allow_null=" + strconv.FormatBool(accepted.GetAllowNull()),
			}
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			accepted := config.GetAcceptedValues()
			if accepted == nil {
				return nil, nil
			}
			return accepted.FailThreshold, accepted.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileAcceptedValues,
		Validator:   validateAcceptedValuesConfig,
		Sample:      sampleAcceptedValues,
		Observe:     countObservation,
		LabelDraft:  labelDraftAcceptedValues,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		Title: "range",
		Arm:   armRange,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetRange().GetColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetRange().GetColumn())
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			rangeConfig := config.GetRange()
			if rangeConfig == nil {
				return nil, nil
			}
			return rangeConfig.FailThreshold, rangeConfig.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileRange,
		Validator:   validateRangeConfig,
		Sample:      sampleRange,
		Observe:     countObservation,
		LabelDraft:  labelDraftRange,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		Title: "format",
		Arm:   armRegex,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetRegex().GetColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetRegex().GetColumn())
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			regex := config.GetRegex()
			if regex == nil {
				return nil
			}
			return []string{"pattern=" + suggestionSignatureHash(regex.GetPattern())}
		},
		// T-SQL has no regular-expression operator; the pattern cannot be pushed
		// down, and evaluating it in process would silently scan the whole table.
		EngineRefusals: map[storepb.Engine]string{
			storepb.Engine_MSSQL: "MSSQL has no regular-expression operator; use custom SQL",
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			regex := config.GetRegex()
			if regex == nil {
				return nil, nil
			}
			return regex.FailThreshold, regex.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileRegex,
		Validator: func(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
			return validateRegexConfig(target, config.GetRegex())
		},
		Sample:     sampleRegex,
		Observe:    countObservation,
		LabelDraft: labelDraftRegex,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		Title: "string length",
		Arm:   armStringLength,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetStringLength().GetColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetStringLength().GetColumn())
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			stringLength := config.GetStringLength()
			if stringLength == nil {
				return nil, nil
			}
			return stringLength.FailThreshold, stringLength.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileStringLength,
		Validator:   validateStringLengthConfig,
		Sample:      sampleStringLength,
		Observe:     countObservation,
		LabelDraft:  labelDraftStringLength,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		Title: "referential integrity",
		Arm:   armReferentialIntegrity,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetReferentialIntegrity().GetChildColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetReferentialIntegrity().GetChildColumn())
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			referential := config.GetReferentialIntegrity()
			if referential == nil {
				return nil
			}
			return []string{
				"parent=" + suggestionSignatureEscape(referential.GetParentTable()) + "." +
					suggestionSignatureEscape(referential.GetParentColumn()),
			}
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			referential := config.GetReferentialIntegrity()
			if referential == nil {
				return nil, nil
			}
			return referential.FailThreshold, referential.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileReferentialIntegrity,
		Validator:   validateReferentialIntegrityConfig,
		Sample:      sampleReferentialIntegrity,
		Observe:     countObservation,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		Title: "cross column",
		Arm:   armCrossColumn,
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			cross := config.GetCrossColumn()
			return twoColumns(cross.GetLeftColumn(), cross.GetRightColumn())
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			cross := config.GetCrossColumn()
			if cross == nil {
				return nil
			}
			// A cross-column comparison is directional: "a > b" and "b > a" are
			// different rules, so the left/right roles survive the sorted column set.
			parts := []string{
				"left=" + suggestionSignatureEscape(cross.GetLeftColumn()),
				"right=" + suggestionSignatureEscape(cross.GetRightColumn()),
			}
			if cross.GetOp() != storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_UNSPECIFIED {
				parts = append(parts, "op="+cross.GetOp().String())
			}
			return parts
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			cross := config.GetCrossColumn()
			if cross == nil {
				return nil, nil
			}
			return cross.FailThreshold, cross.WarnThreshold
		},
		Percent:     true,
		CountsTotal: true,
		Compiler:    compileCrossColumn,
		Validator:   validateCrossColumnConfig,
		Sample:      sampleCrossColumn,
		Observe:     countObservation,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		Title: "freshness",
		Arm:   armFreshness,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetFreshness().GetTimestampColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetFreshness().GetTimestampColumn())
		},
		Views:    true,
		Compiler: compileFreshness,
		Validator: func(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
			return validateFreshnessConfig(target, config.GetFreshness())
		},
		Judge:      evaluateFreshness,
		Observe:    freshnessObservation,
		LabelDraft: labelDraftFreshness,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		Title: "custom SQL",
		Arm:   armCustomSQL,
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			custom := config.GetCustomSql()
			if custom == nil {
				return nil
			}
			return []string{"sql=" + suggestionSignatureHash(custom.GetSql())}
		},
		Views: true,
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			custom := config.GetCustomSql()
			if custom == nil {
				return nil, nil
			}
			return custom.FailThreshold, custom.WarnThreshold
		},
		Compiler:  compileCustomSQL,
		Validator: validateCustomSQLConfig,
		Sample:    sampleCustomSQL,
		Observe:   observeCustomSQL,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
		Title: "expression",
		Arm:   armCELExpression,
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return config.GetCelExpression().GetColumns()
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			cel := config.GetCelExpression()
			if cel == nil {
				return nil
			}
			return []string{"expr=" + suggestionSignatureHash(cel.GetExpression())}
		},
		Percent: true,
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			cel := config.GetCelExpression()
			if cel == nil {
				return nil, nil
			}
			return cel.FailThreshold, cel.WarnThreshold
		},
		CountsTotal: true,
		Compiler:    compileCelExpression,
		Validator:   validateCELExpressionConfig,
	},
	{
		Type:  storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		Arm:   armSchemaDrift,
		Views: true,
		// The drift snapshot is the registry itself: no read-only source and no SQL.
		Metadata: true,
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			drift := config.GetSchemaDrift()
			if drift == nil {
				return nil
			}
			return []string{"expected=" + suggestionSignatureHash(schemaDriftIdentitySnapshot(drift))}
		},
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			drift := config.GetSchemaDrift()
			if drift == nil {
				return nil, nil
			}
			return drift.FailThreshold, drift.WarnThreshold
		},
		CountsTotal: true,
		Compiler:    compileSchemaDrift,
		Validator: func(_ ResolvedTarget, config *storepb.QualityRuleConfig) error {
			return validateSchemaDriftConfig(config)
		},
	},
	{
		Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		Arm:  armGroupCheck,
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return config.GetGroupCheck().GetColumns()
		},
		Percent: true,
		Thresholds: func(config *storepb.QualityRuleConfig) (fail, warn *float64) {
			groupCheck := config.GetGroupCheck()
			if groupCheck == nil {
				return nil, nil
			}
			return groupCheck.FailThreshold, groupCheck.WarnThreshold
		},
		CountsTotal: true,
		Compiler:    compileGroupCheck,
		Validator:   validateGroupCheckConfig,
		Sample:      sampleGroupCheck,
		Observe:     observeGroupCheck,
	},
	{
		Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		Arm:  armAggregate,
		CheckedColumn: func(config *storepb.QualityRuleConfig) string {
			return strings.TrimSpace(config.GetAggregate().GetColumn())
		},
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			return singleColumn(config.GetAggregate().GetColumn())
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			aggregate := config.GetAggregate()
			if aggregate == nil {
				return nil
			}
			parts := []string{"fn=" + aggregate.GetFunction().String()}
			if aggregate.GetFunction() == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE {
				parts[0] += ":" + strconv.FormatFloat(aggregate.GetPercentile(), 'g', -1, 64)
			}
			return parts
		},
		Compiler:  compileAggregate,
		Validator: validateAggregateConfig,
		Judge:     evaluateAggregate,
		Observe:   observeAggregate,
	},
	{
		Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		Arm:  armCrossTable,
		IdentityColumns: func(config *storepb.QualityRuleConfig) []string {
			cross := config.GetCrossTable()
			return twoColumns(cross.GetLeftColumn(), cross.GetRightColumn())
		},
		Discriminators: func(config *storepb.QualityRuleConfig) []string {
			cross := config.GetCrossTable()
			if cross == nil {
				return nil
			}
			return []string{
				"right=" + suggestionSignatureEscape(cross.GetRightTable()),
				"left_fn=" + cross.GetLeftFunction().String(),
				"right_fn=" + cross.GetRightFunction().String(),
				// The two sides of a reconciliation are not interchangeable.
				"left_col=" + suggestionSignatureEscape(cross.GetLeftColumn()),
				"right_col=" + suggestionSignatureEscape(cross.GetRightColumn()),
			}
		},
		Compiler:  compileCrossTable,
		Validator: validateCrossTableConfig,
		Judge:     evaluateCrossTable,
		Observe:   observeCrossTable,
	},
}

func armRowCount(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_RowCount)
	return ok
}

func armRowCountChange(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_RowCountChange)
	return ok
}

func armNullPercent(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_NullPercent)
	return ok
}

func armUnique(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_Unique)
	return ok
}

func armUniqueCombination(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_UniqueCombination)
	return ok
}

func armAcceptedValues(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_AcceptedValues)
	return ok
}

func armRange(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_Range)
	return ok
}

func armRegex(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_Regex)
	return ok
}

func armStringLength(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_StringLength)
	return ok
}

func armReferentialIntegrity(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_ReferentialIntegrity)
	return ok
}

func armCrossColumn(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_CrossColumn)
	return ok
}

func armFreshness(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_Freshness)
	return ok
}

func armCustomSQL(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_CustomSql)
	return ok
}

func armCELExpression(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_CelExpression)
	return ok
}

func armSchemaDrift(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_SchemaDrift)
	return ok
}

func armGroupCheck(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_GroupCheck)
	return ok
}

func armAggregate(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_Aggregate)
	return ok
}

func armCrossTable(config *storepb.QualityRuleConfig) bool {
	_, ok := config.GetConfig().(*storepb.QualityRuleConfig_CrossTable)
	return ok
}
