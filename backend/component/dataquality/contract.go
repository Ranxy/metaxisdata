package dataquality

import (
	"context"

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
	if _, _, err := CompileFilter(dialect, resolution.Target, parsed); err != nil {
		return err
	}
	return nil
}
