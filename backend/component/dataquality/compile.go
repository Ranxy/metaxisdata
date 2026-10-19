package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CompileInput is everything one rule's compiler may read. The target is already
// resolved against the registry, so a compiler never touches metadata.
type CompileInput struct {
	Dialect Dialect
	Target  ResolvedTarget
	Type    storepb.QualityRuleType
	Config  *storepb.QualityRuleConfig
	Filter  Filter
}

// compilerFunc turns a validated rule into the read-only statement that observes
// it. Every compiler returns exactly one statement with bound arguments.
type compilerFunc func(input CompileInput) (CompiledQuery, error)

// compilers maps each supported rule type to its compiler.
var compilers = map[storepb.QualityRuleType]compilerFunc{
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:             compileRowCount,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:      compileRowCountChange,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:          compileNullPercent,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:                compileUnique,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:    compileUniqueCombination,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:       compileAcceptedValues,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:                 compileRange,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:                 compileRegex,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:         compileStringLength,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY: compileReferentialIntegrity,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:          compileCrossColumn,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:             compileFreshness,
	storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:            compileCustomSQL,
}

// Compile produces the statement that observes the rule. It rejects a rule type
// this build cannot compile for and a config whose arm does not match the type.
func Compile(input CompileInput) (CompiledQuery, error) {
	if input.Dialect == nil {
		return CompiledQuery{}, invalidConfig("a dialect is required")
	}
	compiler, ok := compilers[input.Type]
	if !ok {
		return CompiledQuery{}, invalidConfig("rule type %s has no compiler", input.Type)
	}
	if input.Config == nil {
		return CompiledQuery{}, invalidConfig("a rule config is required")
	}
	arm, err := configArm(input.Config)
	if err != nil {
		return CompiledQuery{}, err
	}
	if arm != input.Type {
		return CompiledQuery{}, invalidConfig("the config is for %s, not for the rule's %s", arm, input.Type)
	}
	return compiler(input)
}

// whereClause renders the filter's predicate, including the WHERE keyword, or ""
// when the rule has no filter.
func whereClause(input CompileInput) (string, []any, error) {
	clause, args, err := CompileFilter(input.Dialect, input.Target, input.Filter)
	if err != nil {
		return "", nil, err
	}
	if clause == "" {
		return "", nil, nil
	}
	return " WHERE " + clause, args, nil
}

// requireColumn resolves a column the config names, rejecting a column the target
// does not have. The error is a validation error: a rule that names a missing
// column must not be saved.
func requireColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	if name == "" {
		return ColumnInfo{}, invalidConfig("a column is required")
	}
	column, ok := target.Column(name)
	if !ok {
		return ColumnInfo{}, invalidConfig("column %q does not exist on %s", name, target.GUID)
	}
	return column, nil
}

// qualify renders one column of the target, quoted for the dialect.
func qualify(dialect Dialect, column string) string {
	return dialect.QuoteIdentifier(column)
}
