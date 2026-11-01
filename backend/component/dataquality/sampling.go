package dataquality

import (
	"math"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Sampling binds how much of a target one execution reads. It is an execution
// concern, not a judgment concern: the thresholds still judge what the statement
// measured, and the result is marked as sampled so a reader knows the verdict came
// from a sample.
//
// The two methods are user intents, not SQL mechanisms. Which clause, or which
// bounded derived relation, implements one belongs to the dialect; a rule is
// refused at save time when its engine cannot express the chosen method, so a
// check never silently degrades into a different kind of sample.

const (
	// maxSampledRows bounds max_rows. A larger bound is a full scan with extra
	// steps, and an operator who really wants it can raise the limit deliberately.
	maxSampledRows = 10_000_000
	// minSamplePercent and maxSamplePercent bound a percentage sample. They are the
	// same bounds the proto documents and the editor enforces, so a rule saved
	// through any client is editable in the dialog afterwards.
	minSamplePercent = 1
	maxSamplePercent = 99
)

// samplingConfigured reports whether the config asks for a sample at all.
func samplingConfigured(sampling *storepb.QualityRuleSampling) bool {
	return sampling.GetMethod() != nil
}

// validateSampling is the save-time gate on a rule's sampling. It checks the
// method and its bounds, whether the rule type can be judged on a sample at all,
// and whether the target can express the method — so a rule that cannot run is
// refused when it is saved rather than reporting ERROR on its first run.
func validateSampling(target ResolvedTarget, ruleType storepb.QualityRuleType, sampling *storepb.QualityRuleSampling) error {
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
		if math.IsNaN(method.Percent) || method.Percent < minSamplePercent || method.Percent > maxSamplePercent {
			return invalidConfig("sampling percent must be between %d and %d, got %v", minSamplePercent, maxSamplePercent, method.Percent)
		}
		if target.MetaType != storepb.MetaType_TABLE {
			// PostgreSQL can sample a materialized view, SQL Server and Doris cannot
			// sample a view at all; restricting the method to tables keeps the same
			// rule meaning the same thing on every engine.
			return invalidConfig("sampling by percentage applies to tables only")
		}
		dialect, err := DialectFor(target.Engine)
		if err != nil {
			return invalidConfig("%v", err)
		}
		if !dialect.SupportsTableSample() {
			return invalidConfig("this instance's engine (%s) cannot sample by percentage; use a row limit instead", target.Engine)
		}
		return nil
	case nil:
		return invalidConfig("sampling needs max_rows or percent")
	default:
		return invalidConfig("unknown sampling method %T", method)
	}
}

// samplingUnsupported explains why one rule type cannot be judged from a sample.
// The reason is part of the refusal: a user who tries to sample a row count needs
// to know the count would describe the sample, not the table.
func samplingUnsupported(ruleType storepb.QualityRuleType) error {
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		return invalidConfig("%s cannot be sampled: the row count of a sample is not the table's row count", ruleType)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return invalidConfig("freshness cannot be sampled: a sample can miss the newest row and report a false staleness")
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		return invalidConfig("%s cannot be sampled: it reads two relations", ruleType)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		return invalidConfig("custom SQL cannot be sampled: the statement is never rewritten")
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		return invalidConfig("schema drift cannot be sampled: it reads the metadata registry, not rows")
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		return invalidConfig("a CEL expression has its own sample_rows setting; the check's sampling does not apply to it")
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return invalidConfig("a rule type is required")
	default:
		return invalidConfig("%s cannot be sampled", ruleType)
	}
}

// sampleRowsFromTotal reports whether an observation's total is a row count that
// can stand in for the sample size. group_check totals groups, not rows, so a
// sampled group check records no row count rather than a misleading one.
func sampleRowsFromTotal(ruleType storepb.QualityRuleType) bool {
	return ruleType != storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK
}
