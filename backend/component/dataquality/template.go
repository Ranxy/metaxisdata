package dataquality

import (
	"math"
	"regexp"
	"strings"

	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ValidateRuleTemplateConfig validates a custom rule template's parameters. A
// template is a check type and the settings that are not bound to a target, so
// only the target-independent half of ValidateConfig applies: a template names
// no table, column or filter, and a type whose meaning needs one (schema_drift's
// snapshot) cannot be templated at all.
//
// The save path clears the target scope before it calls this, so in practice the
// column checks never fire there; they are the guard for a caller that skips the
// clear, and they refuse rather than silently store less than it was asked to.
func ValidateRuleTemplateConfig(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) error {
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
		return invalidConfig("a rule type is required")
	}
	spec, ok := ruleTypeSpecFor(ruleType)
	if !ok {
		return invalidConfig("unknown rule type %s", ruleType)
	}
	// schema_drift compares a table's registry columns with a snapshot taken from
	// that same table. The snapshot is the definition, so a template would either
	// pin one table's history or store an empty snapshot no check can use.
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT {
		return invalidConfig("a schema drift template is not supported: its expected-column snapshot is bound to one table, so create the check on that table instead")
	}
	// A custom SQL statement names its own tables and columns, so it is target-bound
	// just like schema_drift's snapshot: a template would keep reading the original
	// table while the editor shows a new target, silently judging the wrong relation.
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL {
		return invalidConfig("a custom SQL template is not supported: the statement names tables and columns, so create the check on its target instead")
	}
	if config == nil {
		return invalidConfig("a rule config is required")
	}
	arm, err := configArm(config)
	if err != nil {
		return err
	}
	if arm != ruleType {
		return invalidConfig("the config is for %s, not for the template's %s", arm, ruleType)
	}
	if templateConfigArmMissing(config) {
		return invalidConfig("the %s config is required", ruleType)
	}
	if templateFilterConfigured(config.GetFilter()) {
		return invalidConfig("templates do not carry a filter: a filter compares a target's columns, so add it to the check instead")
	}
	if templateNamesTargetScope(config) {
		return invalidConfig("templates do not name columns: remove the checked column, its related table or column, or the target-bound flag")
	}
	// A standard-code template may carry a workspace code table reference; the API
	// resolves it. The standard enum itself must be one this build knows, or the
	// template would only fail when a reviewer tries to save a check from it.
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE {
		standard := config.GetStandardCode()
		if standard == nil || standard.GetStandard() == storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNSPECIFIED {
			return invalidConfig("a standard-code template needs a standard")
		}
		if !knownStandard(standard.GetStandard()) {
			return invalidConfig("standard %s is not supported by this build", standard.GetStandard())
		}
	}
	if err := validateOnEmpty(config.GetOnEmpty()); err != nil {
		return err
	}
	if err := validateThresholdMetric(config.GetThresholdMetric()); err != nil {
		return err
	}
	if err := validatePercentSupport(ruleType, config.GetThresholdMetric()); err != nil {
		return err
	}
	if err := validateCheckTimeout(config.GetTimeoutSeconds()); err != nil {
		return err
	}
	if err := validateTemplateSampling(ruleType, config.GetSampling()); err != nil {
		return err
	}
	if err := validateTemplateParameters(ruleType, config); err != nil {
		return err
	}
	return validateTemplateThresholds(spec, config)
}

// validateTemplateParameters checks the target-free parts of a type's parameter
// shape: the pieces that the full validator derives from the target's column type
// cannot be checked here, but a template that is incomplete on its own terms can.
// Without these a reviewer could store an empty regex or a bound-less range and only
// discover it when a colleague tries to save a check from the template.
func validateTemplateParameters(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) error {
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		pattern := strings.TrimSpace(config.GetRegex().GetPattern())
		if pattern == "" {
			return invalidConfig("a regex rule needs a pattern")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return invalidConfig("the regex pattern does not compile: %v", err)
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		values := config.GetAcceptedValues().GetValues()
		if len(values) == 0 {
			return invalidConfig("an accepted-values rule needs at least one value")
		}
		if len(values) > compileMaxAcceptedValues {
			return invalidConfig("an accepted-values rule allows at most %d values, got %d", compileMaxAcceptedValues, len(values))
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		rangeConfig := config.GetRange()
		if rangeConfig.Min == nil && rangeConfig.Max == nil {
			return invalidConfig("a range rule needs min or max")
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:
		length := config.GetStringLength()
		if length.MinLen == nil && length.MaxLen == nil {
			return invalidConfig("a string-length rule needs min_len or max_len")
		}
		if length.MinLen != nil && *length.MinLen < 0 {
			return invalidConfig("min_len must not be negative")
		}
		if length.MaxLen != nil && *length.MaxLen < 0 {
			return invalidConfig("max_len must not be negative")
		}
		if length.MinLen != nil && length.MaxLen != nil && *length.MinLen > *length.MaxLen {
			return invalidConfig("min_len %d exceeds max_len %d", *length.MinLen, *length.MaxLen)
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		if _, _, _, _, err := groupCheckBounds(config.GetGroupCheck()); err != nil {
			return err
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		expression := strings.TrimSpace(config.GetCelExpression().GetExpression())
		if expression == "" {
			return invalidConfig("a CEL expression rule needs an expression")
		}
		if len(expression) > maxCELExpressionLength {
			return invalidConfig("the CEL expression is %d bytes, above the maximum of %d", len(expression), maxCELExpressionLength)
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE:
		// The threshold validator covers these; referential and cross-column need the
		// target's columns, which a template does not have.
	default:
		return invalidConfig("%s has no template parameter validation", ruleType)
	}
	return nil
}

// ClearRuleConfigTargetScope zeroes every target-bound field of a config, so a
// stored template names no table, column or filter. The parameters that give the
// check its meaning but do not name a target stay: values, patterns, null values,
// bounds, thresholds, functions, sample sizes, allow_null, timeout, sampling,
// on_empty and threshold_metric.
//
// A rule type's target-bound fields are its checked column(s), the related
// table/column of a two-relation rule, the CEL column list, unique_combination's
// all_columns, schema_drift's snapshot and the filter. The contract test walks
// ruleTypeSpecs, so a new rule type has to be handled here (or its target-bound
// fields would survive into a template).
func ClearRuleConfigTargetScope(config *storepb.QualityRuleConfig) {
	if config == nil {
		return
	}
	config.Filter = nil
	ruleType, err := configArm(config)
	if err != nil {
		return
	}
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		// No parameter of these types names a target: the bounds and the statement
		// stand on their own.
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		if nullPercent := config.GetNullPercent(); nullPercent != nil {
			nullPercent.Column = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:
		if unique := config.GetUnique(); unique != nil {
			unique.Column = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		if combination := config.GetUniqueCombination(); combination != nil {
			combination.Columns = nil
			// all_columns means "every column of the target", which is not a
			// parameter a template may prefill.
			combination.AllColumns = false
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		if accepted := config.GetAcceptedValues(); accepted != nil {
			accepted.Column = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		if rangeConfig := config.GetRange(); rangeConfig != nil {
			rangeConfig.Column = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		if regex := config.GetRegex(); regex != nil {
			regex.Column = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:
		if stringLength := config.GetStringLength(); stringLength != nil {
			stringLength.Column = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY:
		if referential := config.GetReferentialIntegrity(); referential != nil {
			referential.ChildColumn = ""
			referential.ParentTable = ""
			referential.ParentColumn = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		if cross := config.GetCrossColumn(); cross != nil {
			cross.LeftColumn = ""
			cross.RightColumn = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		if freshness := config.GetFreshness(); freshness != nil {
			freshness.TimestampColumn = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		if cel := config.GetCelExpression(); cel != nil {
			cel.Columns = nil
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		if drift := config.GetSchemaDrift(); drift != nil {
			drift.ExpectedColumns = nil
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		if group := config.GetGroupCheck(); group != nil {
			group.Columns = nil
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE:
		if aggregate := config.GetAggregate(); aggregate != nil {
			aggregate.Column = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		if cross := config.GetCrossTable(); cross != nil {
			// The reference relation and its filter name another table's columns,
			// so a template cannot carry either; the editor picks them per target.
			cross.Reference = nil
			cross.RightFilter = nil
			cross.LeftColumn = ""
			cross.RightColumn = ""
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE:
		if standard := config.GetStandardCode(); standard != nil {
			standard.Column = ""
		}
	default:
		// An arm this build does not know has no target-bound field to clear.
	}
}

// templateFilterConfigured reports whether a filter carries a clause. A filter
// object with no clause names nothing and is equivalent to no filter.
func templateFilterConfigured(filter *storepb.QualityFilter) bool {
	return len(filter.GetClauses()) > 0
}

// templateNamesTargetScope reports whether a config still carries a
// target-bound field. It clears a clone and compares, so the check cannot drift
// from ClearRuleConfigTargetScope, and the filter is compared separately (an
// empty filter object is not target scope).
func templateNamesTargetScope(config *storepb.QualityRuleConfig) bool {
	candidate := cloneRuleConfig(config)
	candidate.Filter = nil
	cleared := cloneRuleConfig(candidate)
	ClearRuleConfigTargetScope(cleared)
	return !proto.Equal(candidate, cleared)
}

// cloneRuleConfig deep-copies a config. Validation compares a cleared clone with
// the caller's message, so it never mutates what the API is about to store.
func cloneRuleConfig(config *storepb.QualityRuleConfig) *storepb.QualityRuleConfig {
	if config == nil {
		return &storepb.QualityRuleConfig{}
	}
	cloned, ok := proto.Clone(config).(*storepb.QualityRuleConfig)
	if !ok {
		return &storepb.QualityRuleConfig{}
	}
	return cloned
}

// templateConfigArmMissing reports whether the config's oneof arm is set to a nil
// message. The arm would store as JSON null, and the check later created from the
// template would fail with "the config is missing".
func templateConfigArmMissing(config *storepb.QualityRuleConfig) bool {
	switch arm := config.GetConfig().(type) {
	case *storepb.QualityRuleConfig_RowCount:
		return arm.RowCount == nil
	case *storepb.QualityRuleConfig_RowCountChange:
		return arm.RowCountChange == nil
	case *storepb.QualityRuleConfig_NullPercent:
		return arm.NullPercent == nil
	case *storepb.QualityRuleConfig_Unique:
		return arm.Unique == nil
	case *storepb.QualityRuleConfig_UniqueCombination:
		return arm.UniqueCombination == nil
	case *storepb.QualityRuleConfig_AcceptedValues:
		return arm.AcceptedValues == nil
	case *storepb.QualityRuleConfig_Range:
		return arm.Range == nil
	case *storepb.QualityRuleConfig_Regex:
		return arm.Regex == nil
	case *storepb.QualityRuleConfig_StringLength:
		return arm.StringLength == nil
	case *storepb.QualityRuleConfig_ReferentialIntegrity:
		return arm.ReferentialIntegrity == nil
	case *storepb.QualityRuleConfig_CrossColumn:
		return arm.CrossColumn == nil
	case *storepb.QualityRuleConfig_Freshness:
		return arm.Freshness == nil
	case *storepb.QualityRuleConfig_CustomSql:
		return arm.CustomSql == nil
	case *storepb.QualityRuleConfig_CelExpression:
		return arm.CelExpression == nil
	case *storepb.QualityRuleConfig_SchemaDrift:
		return arm.SchemaDrift == nil
	case *storepb.QualityRuleConfig_GroupCheck:
		return arm.GroupCheck == nil
	case *storepb.QualityRuleConfig_Aggregate:
		return arm.Aggregate == nil
	case *storepb.QualityRuleConfig_CrossTable:
		return arm.CrossTable == nil
	case *storepb.QualityRuleConfig_StandardCode:
		return arm.StandardCode == nil
	default:
		return true
	}
}

// validateTemplateSampling checks a template's sampling method and bounds. The
// target-dependent half of validateSampling (object type, engine capability,
// whether a percentage sample can be expressed) belongs to the check, which is
// where the target is known; the parameter bounds do not.
func validateTemplateSampling(ruleType storepb.QualityRuleType, sampling *storepb.QualityRuleSampling) error {
	if !samplingConfigured(sampling) {
		return nil
	}
	spec, ok := ruleTypeSpecFor(ruleType)
	if !ok {
		return invalidConfig("unknown rule type %s", ruleType)
	}
	if !spec.Sampling {
		return samplingUnsupported(ruleType)
	}
	switch method := sampling.GetMethod().(type) {
	case *storepb.QualityRuleSampling_MaxRows:
		if method.MaxRows < 1 || method.MaxRows > maxSampledRows {
			return invalidConfig("sampling max_rows must be between 1 and %d, got %d", maxSampledRows, method.MaxRows)
		}
		return nil
	case *storepb.QualityRuleSampling_Percent:
		if math.IsNaN(method.Percent) || math.IsInf(method.Percent, 0) ||
			method.Percent < minSamplePercent || method.Percent > maxSamplePercent {
			return invalidConfig("sampling percent must be between %d and %d, got %v", minSamplePercent, maxSamplePercent, method.Percent)
		}
		return nil
	case nil:
		return invalidConfig("sampling needs max_rows or percent")
	default:
		return invalidConfig("unknown sampling method %T", method)
	}
}

// validateTemplateThresholds checks the type's own threshold-shaped settings,
// which do not depend on a target. Count-shaped types carry fail/warn thresholds;
// the rest keep their bounds in type-specific fields, so only the target-free
// checks of their validators are mirrored here (the column and its type belong to
// the check that applies the template).
func validateTemplateThresholds(spec ruleTypeSpec, config *storepb.QualityRuleConfig) error {
	// The two in-process rule types size their own sample and validate their own
	// reference; neither depends on the target's rows or columns.
	switch spec.Type {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		if _, err := celSampleRows(config.GetCelExpression().GetSampleRows()); err != nil {
			return err
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE:
		if err := validateTemplateStandardCode(config.GetStandardCode()); err != nil {
			return err
		}
	default:
		// The remaining types have no in-process sample of their own.
	}
	if spec.Thresholds != nil {
		fail, warn := spec.Thresholds(config)
		return validateCountThresholds(config, fail, warn)
	}
	switch spec.Type {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:
		return validateRowCountConfig(config.GetRowCount())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		return validateRowCountChangeConfig(config.GetRowCountChange())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		return validateTemplateNullPercent(config.GetNullPercent())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return validateTemplateFreshness(config.GetFreshness())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE:
		return validateTemplateAggregate(config.GetAggregate())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		return validateTemplateCrossTable(config.GetCrossTable())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return invalidConfig("a rule type is required")
	default:
		// A type with neither fail/warn thresholds nor a branch here would skip its
		// bound checks silently. Fail closed so adding such a type forces a branch.
		return invalidConfig("%s has no template threshold validation", spec.Type)
	}
}

// validateTemplateStandardCode mirrors the target-free half of
// validateStandardCodeConfig: a known standard, a code-table reference for the
// custom standard, and a usable sample size. The checked column and the
// referenced table's existence belong to the check.
func validateTemplateStandardCode(config *storepb.StandardCodeConfig) error {
	if config == nil {
		return invalidConfig("the standard-code config is missing")
	}
	switch config.GetStandard() {
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNSPECIFIED:
		return invalidConfig("a standard-code rule needs a standard")
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE:
		if strings.TrimSpace(config.GetCodeTable()) == "" {
			return invalidConfig("a custom code table standard requires a code_table reference")
		}
	default:
		if !knownStandard(config.GetStandard()) {
			return invalidConfig("standard %s is not supported by this build", config.GetStandard())
		}
	}
	_, err := standardCodeSampleRows(config.GetSampleRows())
	return err
}

// validateTemplateNullPercent mirrors validateNullPercentConfig without a column:
// exactly one threshold family, its fail level, and a nested warn level.
func validateTemplateNullPercent(config *storepb.NullPercentConfig) error {
	if config == nil {
		return invalidConfig("the null-percent config is missing")
	}
	countForm := config.MaxNullCount != nil || config.WarnNullCount != nil
	percentForm := config.MaxNullPct != nil || config.WarnNullPct != nil
	switch {
	case countForm && percentForm:
		return invalidConfig("null_percent uses max_null_count or max_null_pct, not both")
	case !countForm && !percentForm:
		return invalidConfig("null_percent needs max_null_count or max_null_pct")
	case countForm:
		if config.MaxNullCount == nil {
			return invalidConfig("warn_null_count needs max_null_count")
		}
		if *config.MaxNullCount < 0 {
			return invalidConfig("max_null_count must not be negative")
		}
		if config.WarnNullCount != nil {
			if *config.WarnNullCount < 0 {
				return invalidConfig("warn_null_count must not be negative")
			}
			if *config.WarnNullCount > *config.MaxNullCount {
				return invalidConfig("warn_null_count %d exceeds max_null_count %d", *config.WarnNullCount, *config.MaxNullCount)
			}
		}
		return nil
	default:
		if config.MaxNullPct == nil {
			return invalidConfig("warn_null_pct needs max_null_pct")
		}
		if !templatePercentInRange(*config.MaxNullPct) {
			return invalidConfig("max_null_pct %v is outside the accepted 0–100 range", *config.MaxNullPct)
		}
		if config.WarnNullPct != nil {
			if !templatePercentInRange(*config.WarnNullPct) {
				return invalidConfig("warn_null_pct %v is outside the accepted 0–100 range", *config.WarnNullPct)
			}
			if *config.WarnNullPct > *config.MaxNullPct {
				return invalidConfig("warn_null_pct %v exceeds max_null_pct %v", *config.WarnNullPct, *config.MaxNullPct)
			}
		}
		return nil
	}
}

// templatePercentInRange reports whether a percentage is a finite 0–100 value.
func templatePercentInRange(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

// validateTemplateFreshness mirrors the bound half of validateFreshnessConfig: a
// positive fail lag and a non-negative warn lag strictly below it.
func validateTemplateFreshness(config *storepb.FreshnessConfig) error {
	if config == nil {
		return invalidConfig("the freshness config is missing")
	}
	if config.GetMaxLagSeconds() <= 0 {
		return invalidConfig("max_lag_seconds must be greater than zero")
	}
	if config.WarnLagSeconds != nil {
		if *config.WarnLagSeconds < 0 {
			return invalidConfig("warn_lag_seconds must not be negative")
		}
		if *config.WarnLagSeconds >= config.GetMaxLagSeconds() {
			return invalidConfig("warn_lag_seconds %d must be smaller than max_lag_seconds %d", *config.WarnLagSeconds, config.GetMaxLagSeconds())
		}
	}
	return nil
}

// validateTemplateAggregate mirrors the target-free half of validateAggregateConfig:
// a supported function (COUNT duplicates row_count), a percentile only for
// PERCENTILE, and a properly nested fail/warn interval.
func validateTemplateAggregate(config *storepb.AggregateConfig) error {
	if config == nil {
		return invalidConfig("the aggregate config is missing")
	}
	switch function := config.GetFunction(); function {
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT:
		return invalidConfig("%s", aggregateCountUnsupported)
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MAX,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE:
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED:
		return invalidConfig("an aggregate function is required")
	default:
		return invalidConfig("unknown aggregate function %d", function)
	}
	if config.GetFunction() == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE {
		if config.Percentile == nil {
			return invalidConfig("an aggregate PERCENTILE needs a percentile")
		}
		if !templatePercentInRange(*config.Percentile) {
			return invalidConfig("percentile %v is outside the accepted 0–100 range", *config.Percentile)
		}
	} else if config.Percentile != nil {
		return invalidConfig("percentile is only used by the PERCENTILE function")
	}
	return validateAggregateBounds(config)
}

// validateTemplateCrossTable mirrors the target-free half of
// validateCrossTableConfig: both functions from the allowed set and a
// non-negative tolerance. The right table and the columns are filled by the check
// that applies the template, so they are empty here.
func validateTemplateCrossTable(config *storepb.CrossTableConfig) error {
	if config == nil {
		return invalidConfig("the cross-table config is missing")
	}
	if ok, reason := crossTableFunctionSupported(config.GetLeftFunction()); !ok {
		return invalidConfig("left function: %s", reason)
	}
	if ok, reason := crossTableFunctionSupported(config.GetRightFunction()); !ok {
		return invalidConfig("right function: %s", reason)
	}
	if config.Tolerance != nil {
		tolerance := *config.Tolerance
		if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance < 0 {
			return invalidConfig("tolerance %v must be a non-negative finite number", tolerance)
		}
	}
	return nil
}
