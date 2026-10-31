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
// instead of reading a nil arm. The arms live in ruleTypeSpecs, so a rule type and
// its arm cannot drift apart.
func configArm(config *storepb.QualityRuleConfig) (storepb.QualityRuleType, error) {
	if config == nil {
		return 0, invalidConfig("the rule config is empty")
	}
	for _, spec := range ruleTypeSpecs {
		if spec.Arm(config) {
			return spec.Type, nil
		}
	}
	arm := config.GetConfig()
	if arm == nil {
		return 0, invalidConfig("the rule config is empty")
	}
	return 0, invalidConfig("unknown rule config arm %T", arm)
}

// RuleTypeForConfig returns the rule type the config's oneof arm names. It reports
// false for an empty config or an arm this build does not know. It is the exported
// form of configArm for callers that only need the type.
func RuleTypeForConfig(config *storepb.QualityRuleConfig) (storepb.QualityRuleType, bool) {
	ruleType, err := configArm(config)
	if err != nil {
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	}
	return ruleType, true
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
