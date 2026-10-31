package dataquality

import (
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
	spec, ok := ruleTypeSpecForConfig(config)
	if !ok || spec.CheckedColumn == nil {
		return ""
	}
	return spec.CheckedColumn(config)
}
