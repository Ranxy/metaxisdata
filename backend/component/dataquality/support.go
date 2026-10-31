package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Supported reports whether this build can compile and run a rule type against an
// engine, and why not when it cannot. Validation rejects an unsupported
// combination when the rule is saved, so it never becomes a runtime ERROR. The
// engine gate is the descriptor table's; the dialect gate is global.
func Supported(engine storepb.Engine, ruleType storepb.QualityRuleType) (bool, string) {
	if _, err := DialectFor(engine); err != nil {
		return false, err.Error()
	}
	spec, ok := ruleTypeSpecFor(ruleType)
	if !ok {
		if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
			return false, "the rule type is required"
		}
		return false, "unknown rule type"
	}
	if reason, refused := spec.EngineRefusals[engine]; refused {
		return false, reason
	}
	return true, ""
}

// RuleTypeForTarget reports whether a rule type may watch the given registry
// object type. Views and materialized views have no separate COLUMN registry
// rows, so only the types the descriptor marks view-capable run against them.
func RuleTypeForTarget(metaType storepb.MetaType, ruleType storepb.QualityRuleType) bool {
	switch metaType {
	case storepb.MetaType_TABLE, storepb.MetaType_EXTERNAL_TABLE:
		return true
	case storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW:
		spec, ok := ruleTypeSpecFor(ruleType)
		return ok && spec.Views
	default:
		return false
	}
}

// IsMetadataRule reports whether a rule type observes the metadata registry
// rather than querying the target database. Such a rule needs no read-only data
// source and compiles to no SQL.
func IsMetadataRule(ruleType storepb.QualityRuleType) bool {
	spec, ok := ruleTypeSpecFor(ruleType)
	return ok && spec.Metadata
}

// aggregateCountUnsupported is the save-time reason a single aggregate COUNT is
// refused: row_count already bounds the filtered row count, so a second rule type
// for the same observation would only split the catalog. cross_table still offers
// COUNT through crossTableFunctionSupported.
const aggregateCountUnsupported = "aggregate COUNT duplicates row_count; use a row_count rule"

// aggregateFunctionSupported reports whether one aggregate function can be
// compiled for an aggregate rule. SUM/AVG/MIN/MAX/STDDEV are available everywhere;
// only PostgreSQL and SQL Server have a percentile-cont aggregate, so the MySQL
// family and StarRocks refuse PERCENTILE at save time. COUNT is not an aggregate
// rule's function (row_count observes the filtered count); cross_table has its own
// table for it.
func aggregateFunctionSupported(engine storepb.Engine, function storepb.QualityAggregateFunction) (bool, string) {
	switch function {
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MAX,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV:
		return true, ""
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT:
		return false, aggregateCountUnsupported
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE:
		switch engine {
		case storepb.Engine_POSTGRES, storepb.Engine_MSSQL:
			return true, ""
		default:
			return false, "PERCENTILE is only supported on PostgreSQL and MSSQL"
		}
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED:
		return false, "an aggregate function is required"
	default:
		return false, "unknown aggregate function"
	}
}

// crossTableFunctionSupported reports whether one side of a reconciliation may use
// a function. cross_table compares magnitudes, so STDDEV and PERCENTILE are not
// offered there.
func crossTableFunctionSupported(function storepb.QualityAggregateFunction) (bool, string) {
	switch function {
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MAX:
		return true, ""
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED:
		return false, "a cross-table function is required"
	default:
		return false, "cross_table supports COUNT, SUM, AVG, MIN and MAX only"
	}
}
