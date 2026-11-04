package dataquality

import (
	"context"
	"errors"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// mergeFilters ANDs the target contract's clauses after the rule's own clauses.
// Each filter is parsed on its own and therefore bounded by MaxFilterClauses; the
// merge happens on already parsed clauses, so the combined predicate may carry up
// to twice that budget without re-parsing (which would reject it).
//
// Clause order is the contract: the rule's clauses come first and the contract's
// follow, so bound argument order is rule values then contract values.
func mergeFilters(ruleFilter, contractFilter Filter) Filter {
	switch {
	case len(contractFilter.Clauses) == 0:
		return ruleFilter
	case len(ruleFilter.Clauses) == 0:
		return contractFilter
	default:
		merged := make([]FilterClause, 0, len(ruleFilter.Clauses)+len(contractFilter.Clauses))
		merged = append(merged, ruleFilter.Clauses...)
		merged = append(merged, contractFilter.Clauses...)
		return Filter{Clauses: merged}
	}
}

// ValidateTargetFilter checks a target-level contract filter before it is stored:
// the target still resolves, every column it names exists on the target, the
// column's type can be filtered on the target's engine, and every value converts
// to the column's type. The filter is compiled, never executed, so this costs the
// metadata registry a read and no target-database query.
//
// An empty filter is always valid: it means the contract adds no predicate, which
// is also what a target with no contract row reads as.
func ValidateTargetFilter(ctx context.Context, s *store.Store, guid string, metaType storepb.MetaType, filter *storepb.QualityFilter) error {
	if filter == nil || len(filter.GetClauses()) == 0 {
		return nil
	}
	resolution, err := ResolveTarget(ctx, s, guid, metaType)
	if err != nil {
		return err
	}
	dialect, err := DialectFor(resolution.Target.Engine)
	if err != nil {
		return invalidConfig("%v", err)
	}
	parsed, err := ParseFilter(filter)
	if err != nil {
		return err
	}
	// A contract filter may scope a time variable like a rule filter does; the
	// reference instant is enough to check the column types.
	resolved, err := ResolveFilterVariables(parsed, referenceTimeVariables())
	if err != nil {
		return err
	}
	if _, _, err := CompileFilter(dialect, resolution.Target, resolved); err != nil {
		return err
	}
	return nil
}

// ValidateContractIntent checks an intent before it is stored: every scope column
// exists on the resolved target and every exemption carries a reason. The registry
// read only happens when a scope column is named, so a statement-free intent update
// stays cheap.
//
// It does not validate the enum values or the counts; those are request-shape
// checks the API performs before converting.
func ValidateContractIntent(ctx context.Context, s *store.Store, guid string, metaType storepb.MetaType, intent *storepb.QualityContractIntent) error {
	if intent == nil {
		return nil
	}
	for _, exemption := range intent.GetExemptions() {
		if strings.TrimSpace(exemption.GetReason()) == "" {
			return invalidConfig("an intent exemption needs a reason")
		}
	}
	scopes := intentScopeColumns(intent)
	if len(scopes) == 0 {
		return nil
	}
	resolution, err := ResolveTarget(ctx, s, guid, metaType)
	if err != nil {
		// A target that lost its registry row keeps its rules and its contract; the
		// read paths still serve them, so refuse only a declaration the registry
		// could verify. Rejecting the update would make the contract uneditable.
		if errors.Is(err, ErrOrphaned) {
			return nil
		}
		return err
	}
	known := make(map[string]struct{}, len(resolution.Target.Columns))
	for _, column := range resolution.Target.Columns {
		known[column.Name] = struct{}{}
	}
	for _, column := range scopes {
		if _, ok := known[column]; !ok {
			return invalidConfig("intent scope column %q does not exist on the target", column)
		}
	}
	return nil
}

// intentScopeColumns lists the non-empty column scopes an intent declares.
func intentScopeColumns(intent *storepb.QualityContractIntent) []string {
	var columns []string
	for _, statement := range intent.GetStatements() {
		if column := strings.TrimSpace(statement.GetColumn()); column != "" {
			columns = append(columns, column)
		}
	}
	for _, exemption := range intent.GetExemptions() {
		if column := strings.TrimSpace(exemption.GetColumn()); column != "" {
			columns = append(columns, column)
		}
	}
	return columns
}

// ValidatePlanCoverage checks the scope columns a plan draft declares: each named
// column must exist on the resolved target. The registry read only happens when a
// scope column is named, and an orphaned target stays editable for the same reason
// an intent does (its rules and contract are still served).
func ValidatePlanCoverage(ctx context.Context, s *store.Store, guid string, metaType storepb.MetaType, coverage []*storepb.QualityContractPlanCoverage) error {
	var scopes []string
	for _, declaration := range coverage {
		if column := strings.TrimSpace(declaration.GetColumn()); column != "" {
			scopes = append(scopes, column)
		}
	}
	if len(scopes) == 0 {
		return nil
	}
	resolution, err := ResolveTarget(ctx, s, guid, metaType)
	if err != nil {
		if errors.Is(err, ErrOrphaned) {
			return nil
		}
		return err
	}
	known := make(map[string]struct{}, len(resolution.Target.Columns))
	for _, column := range resolution.Target.Columns {
		known[column.Name] = struct{}{}
	}
	for _, column := range scopes {
		if _, ok := known[column]; !ok {
			return invalidConfig("plan coverage column %q does not exist on the target", column)
		}
	}
	return nil
}
