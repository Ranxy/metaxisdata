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
	// Filter is the rule's own parsed filter.
	Filter Filter
	// ContractFilter is the target contract's parsed filter. It is ANDed after
	// Filter when the WHERE clause is rendered, so every statement a rule compiles
	// to (the aggregate, the CEL sample and the failed-row sample) sees the same
	// rows. The empty filter adds nothing.
	ContractFilter Filter
}

// compilerFunc turns a validated rule into the read-only statement that observes
// it. Every compiler returns exactly one statement with bound arguments.
type compilerFunc func(input CompileInput) (CompiledQuery, error)

// Compile produces the statement that observes the rule. It rejects a rule type
// this build cannot compile for and a config whose arm does not match the type.
// The per-type compiler is a field of the rule type descriptor.
func Compile(input CompileInput) (CompiledQuery, error) {
	if input.Dialect == nil {
		return CompiledQuery{}, invalidConfig("a dialect is required")
	}
	spec, ok := ruleTypeSpecFor(input.Type)
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
	return spec.Compiler(input)
}

// whereClause renders the predicate's WHERE keyword plus the rule's filter and the
// target contract's filter, or "" when neither has a clause. The two filters are
// merged before compilation so one predicate is rendered with one placeholder
// sequence; compiling them separately and joining the SQL would restart the
// second filter's numbering at 1.
func whereClause(input CompileInput) (string, []any, error) {
	clause, args, err := CompileFilter(input.Dialect, input.Target, mergeFilters(input.Filter, input.ContractFilter))
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
