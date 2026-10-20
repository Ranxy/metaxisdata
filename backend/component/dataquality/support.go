package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Supported reports whether this build can compile and run a rule type against an
// engine, and why not when it cannot. Validation rejects an unsupported
// combination when the rule is saved, so it never becomes a runtime ERROR.
func Supported(engine storepb.Engine, ruleType storepb.QualityRuleType) (bool, string) {
	if _, err := DialectFor(engine); err != nil {
		return false, err.Error()
	}
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return false, "the rule type is required"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		return false, "expression rules arrive in v1.1"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		// T-SQL has no regular-expression operator; the pattern cannot be pushed
		// down, and evaluating it in process would silently scan the whole table.
		if engine == storepb.Engine_MSSQL {
			return false, "MSSQL has no regular-expression operator; use custom SQL"
		}
		return true, ""
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		return true, ""
	default:
		return false, "unknown rule type"
	}
}

// RuleTypeForTarget reports whether a rule type may watch the given registry
// object type. Views and materialized views have no separate COLUMN registry
// rows, so only table-level checks run against them.
func RuleTypeForTarget(metaType storepb.MetaType, ruleType storepb.QualityRuleType) bool {
	switch metaType {
	case storepb.MetaType_TABLE, storepb.MetaType_EXTERNAL_TABLE:
		return true
	case storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW:
		switch ruleType {
		case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			// A schema-drift rule reads the registry's own column list, so it works
			// for any relation type that has columns; a group check runs SQL and
			// stays table-only.
			storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// IsMetadataRule reports whether a rule type observes the metadata registry
// rather than querying the target database. Such a rule needs no read-only data
// source and compiles to no SQL.
func IsMetadataRule(ruleType storepb.QualityRuleType) bool {
	return ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT
}
