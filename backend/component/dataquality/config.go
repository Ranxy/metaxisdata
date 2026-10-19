package dataquality

import (
	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// invalidConfig builds the InvalidArgument error every validation path returns.
func invalidConfig(format string, args ...any) error {
	return common.Errorf(common.Invalid, format, args...)
}

// configArm reports which rule type the config's oneof arm belongs to. It lets
// validation and compilation catch a config that does not match the rule's type
// instead of reading a nil arm.
func configArm(config *storepb.QualityRuleConfig) (storepb.QualityRuleType, error) {
	switch arm := config.GetConfig().(type) {
	case *storepb.QualityRuleConfig_RowCount:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, nil
	case *storepb.QualityRuleConfig_RowCountChange:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, nil
	case *storepb.QualityRuleConfig_NullPercent:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, nil
	case *storepb.QualityRuleConfig_Unique:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, nil
	case *storepb.QualityRuleConfig_UniqueCombination:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, nil
	case *storepb.QualityRuleConfig_AcceptedValues:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, nil
	case *storepb.QualityRuleConfig_Range:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE, nil
	case *storepb.QualityRuleConfig_Regex:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, nil
	case *storepb.QualityRuleConfig_StringLength:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH, nil
	case *storepb.QualityRuleConfig_ReferentialIntegrity:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, nil
	case *storepb.QualityRuleConfig_CrossColumn:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN, nil
	case *storepb.QualityRuleConfig_Freshness:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, nil
	case *storepb.QualityRuleConfig_CustomSql:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, nil
	case *storepb.QualityRuleConfig_CelExpression:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, nil
	case *storepb.QualityRuleConfig_SchemaDrift:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, nil
	case *storepb.QualityRuleConfig_GroupCheck:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, nil
	case nil:
		return 0, invalidConfig("the rule config is empty")
	default:
		return 0, invalidConfig("unknown rule config arm %T", arm)
	}
}

// emptyBehavior returns the configured behavior for a rule with nothing to judge.
// UNSPECIFIED means NOT_EVALUATED: an empty table never reads as a pass by default.
func emptyBehavior(config *storepb.QualityRuleConfig) storepb.QualityEmptyBehavior {
	switch config.GetOnEmpty() {
	case storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_UNSPECIFIED:
		return storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED
	default:
		return config.GetOnEmpty()
	}
}

// thresholdMetric returns the configured count/percent unit, defaulting to COUNT.
func thresholdMetric(config *storepb.QualityRuleConfig) storepb.QualityThresholdMetric {
	switch config.GetThresholdMetric() {
	case storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT:
		return storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	default:
		return storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_COUNT
	}
}
