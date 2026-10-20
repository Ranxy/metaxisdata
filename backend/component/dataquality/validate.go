package dataquality

import (
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ValidatedRule is a rule that survived the whole validation pipeline: its target
// resolves against the registry, its config is legal for that target, and its
// statement compiled. A caller may therefore run it without re-checking anything.
type ValidatedRule struct {
	Resolution *Resolution
	Target     ResolvedTarget
	Filter     Filter
	Compiled   CompiledQuery
}

// ValidateRule is the save/update/dry-run pipeline of design §6.2. Every step is a
// refusal rather than a degradation: a rule that cannot be resolved, supported,
// parsed, validated or compiled never reaches the queue.
//
// filterProto may override the filter stored in the config (the API passes the
// filter separately when it is edited). The config is cloned for validation and
// compilation when it does, so the caller's message is not mutated.
func ValidateRule(ctx context.Context, s *store.Store, ruleType storepb.QualityRuleType, guid string, metaType storepb.MetaType, config *storepb.QualityRuleConfig, filterProto *storepb.QualityFilter) (*ValidatedRule, error) {
	if config == nil {
		return nil, invalidConfig("a rule config is required")
	}
	resolution, err := ResolveTarget(ctx, s, guid, metaType)
	if err != nil {
		return nil, err
	}
	if err := RequireTarget(resolution, ruleType); err != nil {
		return nil, err
	}

	effective := config
	if filterProto != nil {
		cloned, ok := proto.Clone(config).(*storepb.QualityRuleConfig)
		if !ok {
			return nil, common.Errorf(common.Internal, "failed to clone the rule config")
		}
		cloned.Filter = filterProto
		effective = cloned
	}

	filter, err := ParseFilter(effective.GetFilter())
	if err != nil {
		return nil, err
	}
	if err := ValidateConfig(resolution.Target, ruleType, effective); err != nil {
		return nil, err
	}
	// A referential rule's parent is another registry object, so its existence and
	// column type can only be checked here, with the store. Failing now keeps a
	// rule that would always ERROR out of the queue.
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY {
		if err := validateReferentialParent(ctx, s, resolution.Target, effective); err != nil {
			return nil, err
		}
	}
	// A cross-table reconciliation's right column is not part of the resolved
	// target, so its type can only be checked against the registry here.
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE {
		if err := validateCrossTableRight(ctx, s, resolution.Target, effective); err != nil {
			return nil, err
		}
	}

	dialect, err := DialectFor(resolution.Target.Engine)
	if err != nil {
		return nil, invalidConfig("%v", err)
	}
	compiled, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  resolution.Target,
		Type:    ruleType,
		Config:  effective,
		Filter:  filter,
	})
	if err != nil {
		if common.ErrorCode(err) == common.Invalid {
			return nil, err
		}
		// The compiler's remaining failures (an unrenderable table, a dialect that
		// cannot express the rule) are the caller's rule being unusable on this
		// target, which is an invalid rule rather than a server fault.
		return nil, invalidConfig("failed to compile the rule: %v", err)
	}

	return &ValidatedRule{
		Resolution: resolution,
		Target:     resolution.Target,
		Filter:     filter,
		Compiled:   compiled,
	}, nil
}

// ValidateConfig checks one rule's config against the resolved target and the
// rule type. It is the pure part of the pipeline: no database read happens here,
// so it is the entry point tests use with a literal target.
//
// The checks mirror the compilers so a config the compilers refuse is refused
// here with a message about the parameter instead of about SQL; the compiler then
// re-checks what only it can see.
func ValidateConfig(target ResolvedTarget, ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) error {
	if config == nil {
		return invalidConfig("a rule config is required")
	}
	arm, err := configArm(config)
	if err != nil {
		return err
	}
	if arm != ruleType {
		return invalidConfig("the config is for %s, not for the rule's %s", arm, ruleType)
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
	if err := validateFilter(target, config.GetFilter()); err != nil {
		return err
	}

	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:
		return validateRowCountConfig(config.GetRowCount())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		return validateRowCountChangeConfig(config.GetRowCountChange())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		return validateNullPercentConfig(target, config.GetNullPercent())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:
		return validateUniqueConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		return validateUniqueCombinationConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		return validateAcceptedValuesConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		return validateRangeConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		return validateRegexConfig(target, config.GetRegex())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:
		return validateStringLengthConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY:
		return validateReferentialIntegrityConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		return validateCrossColumnConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return validateFreshnessConfig(target, config.GetFreshness())
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		return validateCustomSQLConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		return validateSchemaDriftConfig(config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		return validateGroupCheckConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE:
		return validateAggregateConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		return validateCrossTableConfig(target, config)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		return validateCELExpressionConfig(target, config)
	default:
		return invalidConfig("unknown rule type %s", ruleType)
	}
}

// validateOnEmpty accepts the three real behaviors and UNSPECIFIED (which the
// evaluator treats as NOT_EVALUATED). An unknown number would make the empty-data
// verdict unpredictable.
func validateOnEmpty(onEmpty storepb.QualityEmptyBehavior) error {
	switch onEmpty {
	case storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_UNSPECIFIED,
		storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED,
		storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS,
		storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_FAIL:
		return nil
	default:
		return invalidConfig("unknown on_empty behavior %d", onEmpty)
	}
}

// validateThresholdMetric accepts the two real units; UNSPECIFIED means COUNT in
// thresholdMetric. An unknown number would silently become COUNT and change what a
// threshold means.
func validateThresholdMetric(metric storepb.QualityThresholdMetric) error {
	switch metric {
	case storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_UNSPECIFIED,
		storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_COUNT,
		storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT:
		return nil
	default:
		return invalidConfig("unknown threshold metric %d", metric)
	}
}

// validatePercentSupport rejects PERCENT where the rule has no denominator. A
// count-type rule measures failed_rows / total_rows; row_count, row_count_change,
// null_percent (which names its own percentage form), freshness and custom SQL do
// not return a total to divide by — custom SQL's statement returns failed rows
// only — so a percentage threshold there would silently have no meaning.
func validatePercentSupport(ruleType storepb.QualityRuleType, metric storepb.QualityThresholdMetric) error {
	if metric != storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT {
		return nil
	}
	if ruleTypeAllowsPercent(ruleType) {
		return nil
	}
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT {
		// The drift snapshot is a set of expected columns, not a sample of rows; a
		// percentage of a tiny fixed list is not a meaningful threshold.
		return invalidConfig("schema_drift measures a count of drift items; threshold_metric PERCENT is not supported")
	}
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE ||
		ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE {
		// These thresholds bound an absolute value (an interval edge or a tolerated
		// difference), not a fraction of rows.
		return invalidConfig("%s thresholds are absolute values; threshold_metric PERCENT is not supported", ruleType)
	}
	return invalidConfig("threshold_metric PERCENT is not supported by %s: it has no total-rows denominator", ruleType)
}

// ruleTypeAllowsPercent lists the count-type rules whose observation carries a
// denominator, so a percentage threshold has a real meaning. For the SQL count
// rules that is total_rows; a CEL rule's denominator is the number of sampled rows
// it evaluated. Everything else (custom_sql, freshness, row_count,
// row_count_change, null_percent) has no total to divide by and rejects PERCENT.
func ruleTypeAllowsPercent(ruleType storepb.QualityRuleType) bool {
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		return true
	default:
		return false
	}
}

// validateFilter parses the filter and compiles it against the target, which is
// where column existence and value conversion are checked. The dialect is needed
// only when there is a clause to render, so a rule without a filter does not
// depend on the engine.
func validateFilter(target ResolvedTarget, filter *storepb.QualityFilter) error {
	parsed, err := ParseFilter(filter)
	if err != nil {
		return err
	}
	if len(parsed.Clauses) == 0 {
		return nil
	}
	dialect, err := DialectFor(target.Engine)
	if err != nil {
		return invalidConfig("%v", err)
	}
	if _, _, err := CompileFilter(dialect, target, parsed); err != nil {
		return err
	}
	return nil
}

// validateRowCountConfig checks the row-count bounds: at least one bound, exact is
// exclusive with min/max (the evaluator gives exact precedence, so combining them
// would silently ignore a configured bound), and the warn bounds sit inside the
// fail bounds, where a warning can actually fire before the failure.
func validateRowCountConfig(config *storepb.RowCountConfig) error {
	if config == nil {
		return invalidConfig("the row-count config is missing")
	}
	if config.MinRows == nil && config.MaxRows == nil && config.ExactRows == nil {
		return invalidConfig("a row-count rule needs at least one of min_rows, max_rows or exact_rows")
	}
	if config.ExactRows != nil {
		if *config.ExactRows < 0 {
			return invalidConfig("exact_rows must not be negative")
		}
		if config.MinRows != nil || config.MaxRows != nil {
			return invalidConfig("exact_rows cannot be combined with min_rows or max_rows")
		}
	}
	if config.MinRows != nil && *config.MinRows < 0 {
		return invalidConfig("min_rows must not be negative")
	}
	if config.MaxRows != nil && *config.MaxRows < 0 {
		return invalidConfig("max_rows must not be negative")
	}
	if config.MinRows != nil && config.MaxRows != nil && *config.MinRows > *config.MaxRows {
		return invalidConfig("min_rows %d exceeds max_rows %d", *config.MinRows, *config.MaxRows)
	}
	if config.WarnMinRows != nil {
		if *config.WarnMinRows < 0 {
			return invalidConfig("warn_min_rows must not be negative")
		}
		if config.MinRows != nil && *config.WarnMinRows < *config.MinRows {
			return invalidConfig("warn_min_rows %d is below min_rows %d", *config.WarnMinRows, *config.MinRows)
		}
	}
	if config.WarnMaxRows != nil {
		if *config.WarnMaxRows < 0 {
			return invalidConfig("warn_max_rows must not be negative")
		}
		if config.MaxRows != nil && *config.WarnMaxRows > *config.MaxRows {
			return invalidConfig("warn_max_rows %d is above max_rows %d", *config.WarnMaxRows, *config.MaxRows)
		}
	}
	if config.WarnMinRows != nil && config.WarnMaxRows != nil && *config.WarnMinRows > *config.WarnMaxRows {
		return invalidConfig("warn_min_rows %d exceeds warn_max_rows %d", *config.WarnMinRows, *config.WarnMaxRows)
	}
	return nil
}

// validateRowCountChangeConfig checks the change bounds: at least one bound, every
// bound within 0–1000 percent, and each warn bound no wider than its fail bound.
func validateRowCountChangeConfig(config *storepb.RowCountChangeConfig) error {
	if config == nil {
		return invalidConfig("the row-count-change config is missing")
	}
	if config.MaxIncreasePct == nil && config.MaxDecreasePct == nil &&
		config.WarnIncreasePct == nil && config.WarnDecreasePct == nil {
		return invalidConfig("a row-count-change rule needs at least one change bound")
	}
	for _, bound := range []struct {
		name  string
		value *float64
	}{
		{name: "max_increase_pct", value: config.MaxIncreasePct},
		{name: "max_decrease_pct", value: config.MaxDecreasePct},
		{name: "warn_increase_pct", value: config.WarnIncreasePct},
		{name: "warn_decrease_pct", value: config.WarnDecreasePct},
	} {
		if bound.value == nil {
			continue
		}
		if math.IsNaN(*bound.value) || math.IsInf(*bound.value, 0) {
			return invalidConfig("%s must be a finite number", bound.name)
		}
		if *bound.value < 0 || *bound.value > 1000 {
			return invalidConfig("%s %v is outside the accepted 0–1000 percent range", bound.name, *bound.value)
		}
	}
	if config.MaxIncreasePct != nil && config.WarnIncreasePct != nil && *config.WarnIncreasePct > *config.MaxIncreasePct {
		return invalidConfig("warn_increase_pct %v exceeds max_increase_pct %v", *config.WarnIncreasePct, *config.MaxIncreasePct)
	}
	if config.MaxDecreasePct != nil && config.WarnDecreasePct != nil && *config.WarnDecreasePct > *config.MaxDecreasePct {
		return invalidConfig("warn_decrease_pct %v exceeds max_decrease_pct %v", *config.WarnDecreasePct, *config.MaxDecreasePct)
	}
	return nil
}

// validateNullPercentConfig checks the null-percent rule: the column exists, exactly
// one of the count and percentage families is configured (they measure different
// things and the evaluator picks one), the family carries its fail level, and the
// warn level is inside the fail level.
func validateNullPercentConfig(target ResolvedTarget, config *storepb.NullPercentConfig) error {
	if config == nil {
		return invalidConfig("the null-percent config is missing")
	}
	column, err := requireColumn(target, config.GetColumn())
	if err != nil {
		return err
	}
	// Custom missing values are a null_percent extension: they compare the column
	// against bound values, so the type must support equality, and a value the
	// column cannot hold is a save-time error rather than a query that silently
	// matches nothing.
	if len(config.GetNullValues()) > 0 {
		if _, err := requireComparisonColumn(target, column.Name); err != nil {
			return err
		}
	}
	if _, err := nullPercentValues(column, config.GetNullValues()); err != nil {
		return err
	}
	countForm := config.MaxNullCount != nil || config.WarnNullCount != nil
	percentForm := config.MaxNullPct != nil || config.WarnNullPct != nil
	switch {
	case countForm && percentForm:
		return invalidConfig("null_percent uses max_null_count or max_null_pct, not both")
	case !countForm && !percentForm:
		return invalidConfig("null_percent needs max_null_count or max_null_pct")
	}
	if countForm {
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
	}
	if *config.MaxNullPct < 0 || *config.MaxNullPct > 100 || math.IsNaN(*config.MaxNullPct) || math.IsInf(*config.MaxNullPct, 0) {
		return invalidConfig("max_null_pct %v is outside the accepted 0–100 range", *config.MaxNullPct)
	}
	if config.WarnNullPct != nil {
		if *config.WarnNullPct < 0 || *config.WarnNullPct > 100 || math.IsNaN(*config.WarnNullPct) || math.IsInf(*config.WarnNullPct, 0) {
			return invalidConfig("warn_null_pct %v is outside the accepted 0–100 range", *config.WarnNullPct)
		}
		if *config.WarnNullPct > *config.MaxNullPct {
			return invalidConfig("warn_null_pct %v exceeds max_null_pct %v", *config.WarnNullPct, *config.MaxNullPct)
		}
	}
	return nil
}

// validateUniqueConfig checks a single-column uniqueness rule.
func validateUniqueConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	unique := config.GetUnique()
	if unique == nil {
		return invalidConfig("the unique config is missing")
	}
	if _, err := requireGroupingColumn(target, unique.GetColumn()); err != nil {
		return err
	}
	return validateCountThresholds(config, unique.FailThreshold, unique.WarnThreshold)
}

// validateUniqueCombinationConfig checks a composite-key uniqueness rule. The
// column list and all_columns are alternatives: accepting both (and ignoring one
// of them) would make the saved rule mean something other than what the user saw.
func validateUniqueCombinationConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	combination := config.GetUniqueCombination()
	if combination == nil {
		return invalidConfig("the unique-combination config is missing")
	}
	names := combination.GetColumns()
	if combination.GetAllColumns() {
		if len(names) > 0 {
			return invalidConfig("all_columns cannot be combined with an explicit columns list")
		}
		names = target.ColumnNames()
	}
	if len(names) == 0 {
		return invalidConfig("a unique-combination rule needs at least one column")
	}
	if len(names) > compileMaxUniqueColumns {
		return invalidConfig("a unique-combination rule allows at most %d columns, got %d", compileMaxUniqueColumns, len(names))
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		column, err := requireGroupingColumn(target, name)
		if err != nil {
			return err
		}
		if _, duplicate := seen[column.Name]; duplicate {
			return invalidConfig("column %q is listed more than once", column.Name)
		}
		seen[column.Name] = struct{}{}
	}
	return validateCountThresholds(config, combination.FailThreshold, combination.WarnThreshold)
}

// validateAcceptedValuesConfig checks the accepted-values rule: the column exists,
// the set is non-empty and bounded, and every value can be converted to the
// column's type before it becomes a parameter.
func validateAcceptedValuesConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	accepted := config.GetAcceptedValues()
	if accepted == nil {
		return invalidConfig("the accepted-values config is missing")
	}
	column, err := requireComparisonColumn(target, accepted.GetColumn())
	if err != nil {
		return err
	}
	values := accepted.GetValues()
	if len(values) == 0 {
		return invalidConfig("an accepted-values rule needs at least one value")
	}
	if len(values) > compileMaxAcceptedValues {
		return invalidConfig("an accepted-values rule allows at most %d values, got %d", compileMaxAcceptedValues, len(values))
	}
	for _, value := range values {
		if _, err := convertFilterValue(column, value); err != nil {
			return err
		}
	}
	return validateCountThresholds(config, accepted.FailThreshold, accepted.WarnThreshold)
}

// validateRangeConfig checks the range rule: a numeric or temporal column, at
// least one bound, bounds the column's type can represent, and an ordered numeric
// pair. A temporal pair stays a string and its order is the engine's cast, so only
// non-emptiness is checked here.
func validateRangeConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	rangeConfig := config.GetRange()
	if rangeConfig == nil {
		return invalidConfig("the range config is missing")
	}
	column, err := validateComparableColumn(target, rangeConfig.GetColumn())
	if err != nil {
		return err
	}
	if rangeConfig.Min == nil && rangeConfig.Max == nil {
		return invalidConfig("a range rule needs min or max")
	}
	var lower, upper *float64
	if rangeConfig.Min != nil {
		if err := validateRangeBound(column, "min", *rangeConfig.Min); err != nil {
			return err
		}
		if column.Kind == ColumnKindNumeric {
			parsed, _ := strconv.ParseFloat(strings.TrimSpace(*rangeConfig.Min), 64)
			lower = &parsed
		}
	}
	if rangeConfig.Max != nil {
		if err := validateRangeBound(column, "max", *rangeConfig.Max); err != nil {
			return err
		}
		if column.Kind == ColumnKindNumeric {
			parsed, _ := strconv.ParseFloat(strings.TrimSpace(*rangeConfig.Max), 64)
			upper = &parsed
		}
	}
	if lower != nil && upper != nil && *lower > *upper {
		return invalidConfig("min %q exceeds max %q", *rangeConfig.Min, *rangeConfig.Max)
	}
	return validateCountThresholds(config, rangeConfig.FailThreshold, rangeConfig.WarnThreshold)
}

// validateRangeBound checks one range bound. A numeric bound must parse; a temporal
// bound stays as written (the engine casts it) but must not be empty.
func validateRangeBound(column ColumnInfo, name, value string) error {
	if strings.TrimSpace(value) == "" {
		return invalidConfig("range %s must not be empty", name)
	}
	if column.Kind == ColumnKindNumeric {
		if _, err := convertFilterValue(column, value); err != nil {
			return err
		}
	}
	return nil
}

// validateRegexConfig checks the regex rule: a string column and a pattern Go's
// regexp accepts. The pattern is trimmed the same way the compiler trims it, so a
// whitespace-only pattern is rejected rather than bound as a match-nothing regex.
func validateRegexConfig(target ResolvedTarget, config *storepb.RegexConfig) error {
	if config == nil {
		return invalidConfig("the regex config is missing")
	}
	if err := validateRegexColumn(target, config.GetColumn()); err != nil {
		return err
	}
	pattern := strings.TrimSpace(config.GetPattern())
	if pattern == "" {
		return invalidConfig("a regex rule needs a pattern")
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return invalidConfig("the regex pattern does not compile: %v", err)
	}
	return nil
}

// validateStringLengthConfig checks the string-length rule: a string column, at
// least one bound, and 0 ≤ min_len ≤ max_len.
func validateStringLengthConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	length := config.GetStringLength()
	if length == nil {
		return invalidConfig("the string-length config is missing")
	}
	if err := validateLengthColumn(target, length.GetColumn()); err != nil {
		return err
	}
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
	return validateCountThresholds(config, length.FailThreshold, length.WarnThreshold)
}

// validateReferentialIntegrityConfig checks the referential rule's shape: the
// child column exists, the parent table reference fits the engine's naming
// convention, and the parent column is named. The parent's existence and column
// types need the registry, so they are checked in ValidateRule, which has the
// store; ValidateConfig must stay free of database reads.
func validateReferentialIntegrityConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	referential := config.GetReferentialIntegrity()
	if referential == nil {
		return invalidConfig("the referential-integrity config is missing")
	}
	if _, err := requireColumn(target, referential.GetChildColumn()); err != nil {
		return err
	}
	if _, _, err := parentTableName(target, referential.GetParentTable()); err != nil {
		return err
	}
	if strings.TrimSpace(referential.GetParentColumn()) == "" {
		return invalidConfig("a referential-integrity rule needs a parent column")
	}
	return validateCountThresholds(config, referential.FailThreshold, referential.WarnThreshold)
}

// relatedTableName turns a related table reference — "table" or
// "schema.table"/"database.table" — into the schema and table segments of its
// registry GUID. A related table always lives in the target's own container: on
// the MySQL family that container is the database, so the schema segment is empty
// and "database.table" is accepted only when it names the target's database; on
// PostgreSQL/MSSQL the container is the schema, a bare "table" takes the target's
// schema, and an explicit schema must be the target's.
func relatedTableName(target ResolvedTarget, spec, role, ruleName string) (schema, table string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", invalidConfig("a %s rule needs a %s", ruleName, role)
	}
	parts := strings.Split(spec, ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", "", invalidConfig("%s %q is not a valid table name", role, spec)
		}
	}
	if isMySQLWireEngine(target.Engine) {
		switch len(parts) {
		case 1:
			return "", parts[0], nil
		case 2:
			if parts[0] != target.Database {
				return "", "", invalidConfig("%s %q must be in the rule's database %q", role, spec, target.Database)
			}
			return "", parts[1], nil
		default:
			return "", "", invalidConfig("%s %q must be \"table\" or \"database.table\"", role, spec)
		}
	}
	switch len(parts) {
	case 1:
		return target.Schema, parts[0], nil
	case 2:
		if target.Schema != "" && parts[0] != target.Schema {
			return "", "", invalidConfig("%s %q must be in the rule's schema %q", role, spec, target.Schema)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", invalidConfig("%s %q must be \"table\" or \"schema.table\"", role, spec)
	}
}

// parentTableName turns the config's parent_table into its registry segments.
func parentTableName(target ResolvedTarget, spec string) (schema, table string, err error) {
	return relatedTableName(target, spec, "parent table", "referential-integrity")
}

// relatedTargetGUID builds the registry GUID of a related relation from the
// target's resolved container and the parsed relation name. The GUID is the same
// ';'-joined path the schema syncer registers, so the lookup is the registry's own
// identity rather than a name match.
func relatedTargetGUID(target ResolvedTarget, schema, table string) string {
	return common.BuildMetaGUID(target.InstanceResourceID, target.Database, schema, table)
}

// resolveRelatedRelation resolves a related relation against the registry. A
// related table may be a table, a view or an external table; the first type the
// registry knows wins, so a rule keeps working when the relation it names is one
// of those rather than a base table.
func resolveRelatedRelation(ctx context.Context, s *store.Store, guid string) (*ResolvedTarget, error) {
	metaTypes := []storepb.MetaType{
		storepb.MetaType_TABLE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW,
	}
	var lastErr error
	for _, metaType := range metaTypes {
		resolution, err := ResolveTarget(ctx, s, guid, metaType)
		if err == nil {
			return &resolution.Target, nil
		}
		if common.ErrorCode(err) != common.NotFound {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// validateReferentialParent resolves the parent table against the registry and
// checks that the join is expressible: the parent column exists, both sides have
// equality, and their types compare. It is the part of referential-integrity
// validation the config-only layer cannot do: CompileInput carries only the
// child's resolved target, so without this the first sign of a wrong parent would
// be a runtime ERROR after the rule was saved.
//
// Kind equality alone is not enough. The "string" kind holds text, jsonb, uuid,
// bytea and user-defined types alike, and a join between a jsonb child and a text
// parent has no operator even though both are "string".
func validateReferentialParent(ctx context.Context, s *store.Store, target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	referential := config.GetReferentialIntegrity()
	if referential == nil {
		return invalidConfig("the referential-integrity config is missing")
	}
	childName := strings.TrimSpace(referential.GetChildColumn())
	child, err := requireComparisonColumn(target, childName)
	if err != nil {
		return err
	}
	parentSchema, parentTable, err := parentTableName(target, referential.GetParentTable())
	if err != nil {
		return err
	}
	parentGUID := relatedTargetGUID(target, parentSchema, parentTable)
	parent, err := resolveRelatedRelation(ctx, s, parentGUID)
	if err != nil {
		if common.ErrorCode(err) == common.NotFound {
			// The parent is a registry object of the same instance; a missing row
			// means the rule names a relation that does not exist (or was dropped),
			// not that the server failed.
			return invalidConfig("parent table %q does not exist or was dropped", referential.GetParentTable())
		}
		return err
	}
	parentColumnName := strings.TrimSpace(referential.GetParentColumn())
	parentColumn, ok := parent.Column(parentColumnName)
	if !ok {
		return invalidConfig("parent column %q does not exist on %q", parentColumnName, referential.GetParentTable())
	}
	if parentColumn.Kind == ColumnKindUnknown {
		return invalidConfig("cannot compare columns %q and %q: this build cannot classify the parent column's type", child.Name, parentColumn.Name)
	}
	if !supportsComparison(parentColumn, target.Engine) {
		return invalidConfig("parent column %q is %s; the engine cannot compare values of this type", parentColumn.Name, parentColumn.Type)
	}
	if !comparableColumnTypes(child, parentColumn, target.Engine) {
		return invalidConfig("columns %q (%s) and %q (%s) are not comparable; a referential rule needs both sides to be text or the same type", child.Name, child.Type, parentColumn.Name, parentColumn.Type)
	}
	return nil
}

// validateCrossTableRight resolves a reconciliation's right relation against the
// registry and checks the right aggregate's column the way the left side is
// checked. The compiler quotes the right column without resolving it, so without
// this step a MIN on a jsonb right column would be saved and fail on every run.
func validateCrossTableRight(ctx context.Context, s *store.Store, target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	cross := config.GetCrossTable()
	if cross == nil {
		// The pure config validator reports a missing config.
		return nil
	}
	rightSchema, rightTable, err := relatedTableName(target, cross.GetRightTable(), "right table", "cross-table")
	if err != nil {
		return err
	}
	rightGUID := relatedTargetGUID(target, rightSchema, rightTable)
	right, err := resolveRelatedRelation(ctx, s, rightGUID)
	if err != nil {
		if common.ErrorCode(err) == common.NotFound {
			return invalidConfig("right table %q does not exist or was dropped", cross.GetRightTable())
		}
		return err
	}
	rightFunction := cross.GetRightFunction()
	if rightFunction == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		return nil
	}
	rightColumnName := strings.TrimSpace(cross.GetRightColumn())
	rightColumn, ok := right.Column(rightColumnName)
	if !ok {
		return invalidConfig("right column %q does not exist on %q", rightColumnName, cross.GetRightTable())
	}
	if err := validateAggregateColumnKind(rightColumn, rightFunction); err != nil {
		return err
	}
	// The evaluator subtracts the two observations as numbers, so every non-COUNT
	// side must be numeric: MIN/MAX on a date would be a timestamp, not a difference.
	if rightFunction != storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		if rightColumn.Kind != ColumnKindNumeric {
			return invalidConfig("a cross-table %s needs a numeric column; %q is %s", aggregateFunctionName(rightFunction), rightColumn.Name, rightColumn.Type)
		}
	}
	leftFunction := cross.GetLeftFunction()
	if leftFunction == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		return nil
	}
	leftColumn, ok := target.Column(strings.TrimSpace(cross.GetLeftColumn()))
	if !ok {
		// The config-only validator reports the missing column.
		return nil
	}
	if leftColumn.Kind != ColumnKindNumeric {
		return invalidConfig("a cross-table %s needs a numeric column; %q is %s", aggregateFunctionName(leftFunction), leftColumn.Name, leftColumn.Type)
	}
	return nil
}

// isMySQLWireEngine reports whether the engine shares the MySQL naming convention
// (one container, no schema segment).
func isMySQLWireEngine(engine storepb.Engine) bool {
	switch engine {
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE, storepb.Engine_STARROCKS, storepb.Engine_DORIS:
		return true
	default:
		return false
	}
}

// validateCrossColumnConfig checks the cross-column rule: two comparable columns of
// the same kind (comparing a date to a number has no engine-independent meaning),
// a real operator, and a tolerance only where arithmetic can absorb it.
func validateCrossColumnConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	cross := config.GetCrossColumn()
	if cross == nil {
		return invalidConfig("the cross-column config is missing")
	}
	left, err := validateComparableColumn(target, cross.GetLeftColumn())
	if err != nil {
		return err
	}
	right, err := validateComparableColumn(target, cross.GetRightColumn())
	if err != nil {
		return err
	}
	if left.Kind != right.Kind {
		return invalidConfig("columns %q (%s) and %q (%s) are not comparable", left.Name, left.Kind, right.Name, right.Kind)
	}
	if err := validateComparisonOperator(cross.GetOp()); err != nil {
		return err
	}
	if cross.Tolerance != nil {
		if math.IsNaN(*cross.Tolerance) || math.IsInf(*cross.Tolerance, 0) {
			return invalidConfig("the cross-column tolerance must be a finite number")
		}
		if *cross.Tolerance < 0 {
			return invalidConfig("the cross-column tolerance must not be negative")
		}
		if left.Kind != ColumnKindNumeric {
			return invalidConfig("a cross-column tolerance needs numeric columns")
		}
	}
	return validateCountThresholds(config, cross.FailThreshold, cross.WarnThreshold)
}

// validateComparisonOperator accepts the six real comparisons and rejects
// UNSPECIFIED and unknown numbers.
func validateComparisonOperator(op storepb.QualityComparisonOperator) error {
	switch op {
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
		storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_NE,
		storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LT,
		storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE,
		storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GT,
		storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GTE:
		return nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_UNSPECIFIED:
		return invalidConfig("a cross-column rule needs a comparison operator")
	default:
		return invalidConfig("unknown comparison operator %d", op)
	}
}

// validateFreshnessConfig checks the freshness rule: a temporal column, a positive
// fail lag, and a non-negative warn lag that is strictly smaller (a warn lag equal
// to the fail lag could never be reported: the fail check runs first).
func validateFreshnessConfig(target ResolvedTarget, config *storepb.FreshnessConfig) error {
	if config == nil {
		return invalidConfig("the freshness config is missing")
	}
	if err := validateTemporalColumn(target, config.GetTimestampColumn()); err != nil {
		return err
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

// validateCustomSQLConfig checks the custom-SQL rule: the statement is a single
// read-only SELECT/WITH this build's parser accepts for the target engine, within
// the size limit, and the thresholds are legal.
func validateCustomSQLConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	custom := config.GetCustomSql()
	if custom == nil {
		return invalidConfig("the custom-SQL config is missing")
	}
	if err := ValidateCustomSQL(target.Engine, custom.GetSql()); err != nil {
		return err
	}
	return validateCountThresholds(config, custom.FailThreshold, custom.WarnThreshold)
}

// validateCountThresholds checks the fail/warn pair shared by the count-type rules.
// COUNT thresholds are failed rows, so they are non-negative; PERCENT thresholds
// are a percentage of the filtered rows, so they also stay within 0–100. An unset
// fail threshold means 0, which is what the evaluator uses; a warn level above the
// fail level could never be reported because the fail check runs first.
// validateCELExpressionConfig checks one CEL rule: the referenced columns exist,
// the sample size is legal, the thresholds are consistent in their unit, and the
// expression compiles to a bool against those columns' CEL types.
func validateCELExpressionConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	celConfig := config.GetCelExpression()
	if celConfig == nil {
		return invalidConfig("the CEL expression config is missing")
	}
	columns, err := celColumns(target, celConfig.GetColumns())
	if err != nil {
		return err
	}
	if _, err := celSampleRows(celConfig.GetSampleRows()); err != nil {
		return err
	}
	if err := validateCountThresholds(config, celConfig.FailThreshold, celConfig.WarnThreshold); err != nil {
		return err
	}
	if _, err := CompileCEL(columns, celConfig.GetExpression()); err != nil {
		return err
	}
	return nil
}

func validateCountThresholds(config *storepb.QualityRuleConfig, fail, warn *float64) error {
	percent := thresholdMetric(config) == storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	failValue := 0.0
	if fail != nil {
		failValue = *fail
	}
	if err := validateThresholdValue("failure threshold", failValue, percent); err != nil {
		return err
	}
	if warn != nil {
		if err := validateThresholdValue("warning threshold", *warn, percent); err != nil {
			return err
		}
		if *warn > failValue {
			return invalidConfig("warning threshold %v exceeds failure threshold %v", *warn, failValue)
		}
	}
	return nil
}

// validateThresholdValue checks one threshold in its unit.
func validateThresholdValue(name string, value float64, percent bool) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return invalidConfig("%s must be a finite number", name)
	}
	if value < 0 {
		return invalidConfig("%s must not be negative", name)
	}
	if percent && value > 100 {
		return invalidConfig("%s %v is above 100 percent", name, value)
	}
	return nil
}

// validateRegexColumn resolves a column and requires regular-expression support.
func validateRegexColumn(target ResolvedTarget, name string) error {
	_, err := requireRegexColumn(target, name)
	return err
}

// validateLengthColumn resolves a column and requires a character-length function.
func validateLengthColumn(target ResolvedTarget, name string) error {
	_, err := requireLengthColumn(target, name)
	return err
}

// validateTemporalColumn resolves a column and requires it to be a date/time.
func validateTemporalColumn(target ResolvedTarget, name string) error {
	column, err := requireColumn(target, name)
	if err != nil {
		return err
	}
	if column.Kind != ColumnKindTemporal {
		return invalidConfig("column %q is %s; this rule needs a temporal column", column.Name, column.Type)
	}
	return nil
}

// validateComparableColumn resolves a column whose values support ordering. Text is
// excluded: a range over text is a collation question, not a data quality check.
func validateComparableColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind != ColumnKindNumeric && column.Kind != ColumnKindTemporal {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a numeric or temporal column", column.Name, column.Type)
	}
	return column, nil
}

// validateSchemaDriftConfig checks the snapshot a drift rule compares against: at
// least one expected column, every entry with a non-empty unique name and a type,
// and sane thresholds. PERCENT is refused earlier (the observation is a fixed
// count of drift items); check_types, check_order and allow_extra_columns are
// booleans with a fixed meaning, so they need no validation.
func validateSchemaDriftConfig(config *storepb.QualityRuleConfig) error {
	drift := config.GetSchemaDrift()
	if drift == nil {
		return invalidConfig("the schema-drift config is missing")
	}
	expected := drift.GetExpectedColumns()
	if len(expected) == 0 {
		return invalidConfig("a schema-drift rule needs at least one expected column")
	}
	seen := make(map[string]struct{}, len(expected))
	for _, column := range expected {
		if column == nil {
			return invalidConfig("a schema-drift snapshot column is required")
		}
		name := strings.TrimSpace(column.GetName())
		if name == "" {
			return invalidConfig("every expected column needs a name")
		}
		if _, ok := seen[name]; ok {
			return invalidConfig("expected column %q is named more than once", name)
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(column.GetType()) == "" {
			return invalidConfig("expected column %q needs a type", name)
		}
	}
	return validateCountThresholds(config, drift.FailThreshold, drift.WarnThreshold)
}

// validateGroupCheckConfig checks a grouped row-count rule: 1–5 existing group
// columns with no repeats, at least one row bound with min ≤ max, and sane
// thresholds in the configured unit.
func validateGroupCheckConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	group := config.GetGroupCheck()
	if group == nil {
		return invalidConfig("the group-check config is missing")
	}
	if _, err := groupCheckColumns(target, group.GetColumns()); err != nil {
		return err
	}
	if _, _, _, _, err := groupCheckBounds(group); err != nil {
		return err
	}
	return validateCountThresholds(config, group.FailThreshold, group.WarnThreshold)
}

// validateAggregateConfig checks an aggregate rule: the column exists, the
// function is one this engine can run, PERCENTILE carries a 0–100 percentile, the
// function's column type fits, and the interval bounds nest properly.
func validateAggregateConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	aggregate := config.GetAggregate()
	if aggregate == nil {
		return invalidConfig("the aggregate config is missing")
	}
	column, err := requireColumn(target, aggregate.GetColumn())
	if err != nil {
		return err
	}
	function := aggregate.GetFunction()
	if ok, reason := aggregateFunctionSupported(target.Engine, function); !ok {
		return invalidConfig("%s", reason)
	}
	if function == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE {
		if aggregate.Percentile == nil {
			return invalidConfig("an aggregate PERCENTILE needs a percentile")
		}
		percentile := *aggregate.Percentile
		if math.IsNaN(percentile) || math.IsInf(percentile, 0) || percentile < 0 || percentile > 100 {
			return invalidConfig("percentile %v is outside the accepted 0–100 range", percentile)
		}
	} else if aggregate.Percentile != nil {
		return invalidConfig("percentile is only used by the PERCENTILE function")
	}
	if err := validateAggregateColumnKind(column, function); err != nil {
		return err
	}
	return validateAggregateBounds(aggregate)
}

// validateAggregateColumnKind enforces an aggregate function's column-type
// constraint: arithmetic aggregates need numbers, MIN/MAX also accept temporal
// values, and COUNT accepts anything.
func validateAggregateColumnKind(column ColumnInfo, function storepb.QualityAggregateFunction) error {
	switch function {
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT:
		return nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE:
		if column.Kind != ColumnKindNumeric {
			return invalidConfig("%s needs a numeric column; %q is %s", aggregateFunctionName(function), column.Name, column.Type)
		}
		return nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MAX:
		if column.Kind != ColumnKindNumeric && column.Kind != ColumnKindTemporal {
			return invalidConfig("%s needs a numeric or temporal column; %q is %s", aggregateFunctionName(function), column.Name, column.Type)
		}
		return nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED:
		return invalidConfig("an aggregate function is required")
	default:
		return invalidConfig("unknown aggregate function %d", function)
	}
}

// aggregateFunctionName renders the enum name without its proto prefix.
func aggregateFunctionName(function storepb.QualityAggregateFunction) string {
	return strings.TrimPrefix(function.String(), "QUALITY_AGGREGATE_FUNCTION_")
}

// validateAggregateBounds checks the fail interval and the warn interval nested
// inside it: at least one fail bound, finite values, min ≤ max, and each warn
// bound inside the fail interval so a warning can actually fire before a failure.
func validateAggregateBounds(aggregate *storepb.AggregateConfig) error {
	if aggregate.Min == nil && aggregate.Max == nil {
		return invalidConfig("an aggregate rule needs min or max")
	}
	for _, bound := range []struct {
		name  string
		value *float64
	}{
		{name: "min", value: aggregate.Min},
		{name: "max", value: aggregate.Max},
		{name: "warn_min", value: aggregate.WarnMin},
		{name: "warn_max", value: aggregate.WarnMax},
	} {
		if bound.value == nil {
			continue
		}
		if math.IsNaN(*bound.value) || math.IsInf(*bound.value, 0) {
			return invalidConfig("%s must be a finite number", bound.name)
		}
	}
	if aggregate.Min != nil && aggregate.Max != nil && *aggregate.Min > *aggregate.Max {
		return invalidConfig("min %v exceeds max %v", *aggregate.Min, *aggregate.Max)
	}
	if aggregate.WarnMin != nil {
		if aggregate.Min != nil && *aggregate.WarnMin < *aggregate.Min {
			return invalidConfig("warn_min %v is below min %v", *aggregate.WarnMin, *aggregate.Min)
		}
		if aggregate.Max != nil && *aggregate.WarnMin > *aggregate.Max {
			return invalidConfig("warn_min %v is above max %v", *aggregate.WarnMin, *aggregate.Max)
		}
	}
	if aggregate.WarnMax != nil {
		if aggregate.Max != nil && *aggregate.WarnMax > *aggregate.Max {
			return invalidConfig("warn_max %v is above max %v", *aggregate.WarnMax, *aggregate.Max)
		}
		if aggregate.Min != nil && *aggregate.WarnMax < *aggregate.Min {
			return invalidConfig("warn_max %v is below min %v", *aggregate.WarnMax, *aggregate.Min)
		}
	}
	if aggregate.WarnMin != nil && aggregate.WarnMax != nil && *aggregate.WarnMin > *aggregate.WarnMax {
		return invalidConfig("warn_min %v exceeds warn_max %v", *aggregate.WarnMin, *aggregate.WarnMax)
	}
	return nil
}

// validateCrossTableConfig checks a reconciliation: the right table's shape and
// container, both functions from the allowed set, the column rules for COUNT and
// non-COUNT sides, the left column's existence and type, and a non-negative
// tolerance. The right table's own metadata is resolved by the API layer, which is
// the only place that can look it up in the registry.
func validateCrossTableConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	cross := config.GetCrossTable()
	if cross == nil {
		return invalidConfig("the cross-table config is missing")
	}
	dialect, err := DialectFor(target.Engine)
	if err != nil {
		return invalidConfig("%v", err)
	}
	if _, err := compileRightTable(dialect, target, cross.GetRightTable()); err != nil {
		return err
	}
	leftFunction := cross.GetLeftFunction()
	if ok, reason := crossTableFunctionSupported(leftFunction); !ok {
		return invalidConfig("left function: %s", reason)
	}
	rightFunction := cross.GetRightFunction()
	if ok, reason := crossTableFunctionSupported(rightFunction); !ok {
		return invalidConfig("right function: %s", reason)
	}

	leftColumn := strings.TrimSpace(cross.GetLeftColumn())
	if leftFunction == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		if leftColumn != "" {
			return invalidConfig("a cross-table COUNT check takes no left_column")
		}
	} else {
		column, err := requireColumn(target, leftColumn)
		if err != nil {
			return err
		}
		if err := validateAggregateColumnKind(column, leftFunction); err != nil {
			return err
		}
	}

	rightColumn := strings.TrimSpace(cross.GetRightColumn())
	if rightFunction == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		if rightColumn != "" {
			return invalidConfig("a cross-table COUNT check takes no right_column")
		}
	} else if rightColumn == "" {
		return invalidConfig("a cross-table %s check needs a right_column", aggregateFunctionName(rightFunction))
	}

	if cross.Tolerance != nil {
		tolerance := *cross.Tolerance
		if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance < 0 {
			return invalidConfig("tolerance %v must be a non-negative finite number", tolerance)
		}
	}
	return validateCountThresholds(config, cross.FailThreshold, cross.WarnThreshold)
}
