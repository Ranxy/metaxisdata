package qualityrunner

import (
	"context"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// batchCodeTables caches the workspace code tables a batch's rules reference. A
// target can carry several standard-code checks that share one table, so the table
// is read at most once per rule reference per batch.
type batchCodeTables struct {
	load   func(ctx context.Context, resourceName string) ([]string, error)
	byName map[string][]string
}

// newBatchCodeTables starts an empty cache for one batch.
func newBatchCodeTables(load func(ctx context.Context, resourceName string) ([]string, error)) *batchCodeTables {
	return &batchCodeTables{load: load, byName: make(map[string][]string)}
}

// forRule returns the values of the code table a standard-code rule references. A
// rule that names no code table gets nil without a store read.
func (c *batchCodeTables) forRule(ctx context.Context, reference string) ([]string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, nil
	}
	if cached, ok := c.byName[reference]; ok {
		return cached, nil
	}
	values, err := c.load(ctx, reference)
	if err != nil {
		return nil, err
	}
	c.byName[reference] = values
	return values, nil
}

// loadBatchCodeTables reads one code table for the batch cache. A nil store (the
// accounting tests) reads as an unavailable table, which the caller turns into an
// ERROR rather than judging values against no table at all.
func (r *Runner) loadBatchCodeTables() func(ctx context.Context, resourceName string) ([]string, error) {
	return func(ctx context.Context, resourceName string) ([]string, error) {
		if r.stores == nil {
			return nil, errCodeTableUnavailable(resourceName)
		}
		resourceID := codeTableResourceID(resourceName)
		if resourceID == "" {
			return nil, errCodeTableUnavailable(resourceName)
		}
		table, err := r.stores.GetQualityCodeTable(ctx, &store.FindQualityCodeTableMessage{ResourceID: &resourceID})
		if err != nil {
			return nil, err
		}
		if table == nil {
			return nil, errCodeTableUnavailable(resourceName)
		}
		return table.Values, nil
	}
}

// codeTableResourceID takes the identity segment of a code-table resource name:
// "workspaces/{workspace}/qualityCodeTables/{id}" becomes "{id}". The empty string
// means the reference is not a code-table name.
func codeTableResourceID(resourceName string) string {
	resourceName = strings.TrimSpace(resourceName)
	const marker = "/qualityCodeTables/"
	index := strings.LastIndex(resourceName, marker)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(resourceName[index+len(marker):])
}

// codeTableReference returns the code table a rule references, or "" when the rule
// names none (every non-standard-code type, and a standard-code rule without a
// table).
func codeTableReference(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) string {
	if ruleType != storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE {
		return ""
	}
	return strings.TrimSpace(config.GetStandardCode().GetCodeTable())
}

// errCodeTableUnavailable explains a reference that cannot be resolved. The rule
// must not judge values against no table at all: for a custom code table that would
// fail every value, and for a narrowing table it would silently stop narrowing.
func errCodeTableUnavailable(resourceName string) error {
	return common.Errorf(common.Invalid,
		"the code table %q referenced by this check is unavailable; add the table or clear the reference", resourceName)
}
