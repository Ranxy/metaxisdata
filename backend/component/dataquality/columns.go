package dataquality

import (
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ConfigColumn returns the one column a rule's typed config observes, or "" when
// the rule is table-level or relates several columns.
//
// The rule's config is the authoritative source for the checked column (see
// QualityTarget.columns); QualityTargetPath.column is only a display snapshot and
// may be empty for rules written before it was populated. Filter columns and
// schema-drift snapshots are deliberately excluded: they are read, not checked.
func ConfigColumn(config *storepb.QualityRuleConfig) string {
	if config == nil {
		return ""
	}
	switch arm := config.GetConfig().(type) {
	case *storepb.QualityRuleConfig_NullPercent:
		return strings.TrimSpace(arm.NullPercent.GetColumn())
	case *storepb.QualityRuleConfig_Unique:
		return strings.TrimSpace(arm.Unique.GetColumn())
	case *storepb.QualityRuleConfig_AcceptedValues:
		return strings.TrimSpace(arm.AcceptedValues.GetColumn())
	case *storepb.QualityRuleConfig_Range:
		return strings.TrimSpace(arm.Range.GetColumn())
	case *storepb.QualityRuleConfig_Regex:
		return strings.TrimSpace(arm.Regex.GetColumn())
	case *storepb.QualityRuleConfig_StringLength:
		return strings.TrimSpace(arm.StringLength.GetColumn())
	case *storepb.QualityRuleConfig_ReferentialIntegrity:
		return strings.TrimSpace(arm.ReferentialIntegrity.GetChildColumn())
	case *storepb.QualityRuleConfig_Freshness:
		return strings.TrimSpace(arm.Freshness.GetTimestampColumn())
	case *storepb.QualityRuleConfig_Aggregate:
		return strings.TrimSpace(arm.Aggregate.GetColumn())
	case *storepb.QualityRuleConfig_RowCount,
		*storepb.QualityRuleConfig_RowCountChange,
		*storepb.QualityRuleConfig_UniqueCombination,
		*storepb.QualityRuleConfig_CrossColumn,
		*storepb.QualityRuleConfig_CustomSql,
		*storepb.QualityRuleConfig_CelExpression,
		*storepb.QualityRuleConfig_SchemaDrift,
		*storepb.QualityRuleConfig_GroupCheck,
		*storepb.QualityRuleConfig_CrossTable:
		return ""
	default:
		return ""
	}
}
