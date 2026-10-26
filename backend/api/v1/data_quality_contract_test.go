package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The checks are ordered by column then id, so two publishes of the same rule set
// fingerprint identically regardless of the store's row order.
func TestBuildQualityContractChecksIsDeterministic(t *testing.T) {
	t.Parallel()

	rules := []*store.QualityRuleMessage{
		{ID: 2, ResourceID: "b", Revision: 1, TargetPath: &storepb.QualityTargetPath{Column: "email"}},
		{ID: 1, ResourceID: "a", Revision: 2},
	}
	checks := buildQualityContractChecks(rules)
	require.Len(t, checks, 2)
	require.Equal(t, "a", checks[0].GetRuleResourceId(), "table-level checks come before column checks")
	require.Equal(t, "b", checks[1].GetRuleResourceId())

	// The same set in the opposite input order fingerprints the same.
	reversed := buildQualityContractChecks([]*store.QualityRuleMessage{rules[1], rules[0]})
	require.Equal(t, checks[0].GetRuleResourceId(), reversed[0].GetRuleResourceId())
	require.Equal(t, checks[1].GetRuleResourceId(), reversed[1].GetRuleResourceId())
}

// Republishing the same checks and settings must produce the same fingerprint;
// changing the settings or one check must change it.
func TestQualityContractFingerprint(t *testing.T) {
	t.Parallel()

	checks := []*storepb.QualityContractCheck{
		{RuleResourceId: "a", RuleRevision: 1, Title: "rows", RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
		{RuleResourceId: "b", RuleRevision: 3, Title: "email", Column: "email"},
	}
	settings := &storepb.QualityContractSettings{DefaultScheduleIntervalSeconds: 3600}
	base := qualityContractFingerprint(settings, checks)
	require.Len(t, base, 64)
	require.Equal(t, base, qualityContractFingerprint(settings, checks))

	changedCheck := []*storepb.QualityContractCheck{
		{RuleResourceId: "a", RuleRevision: 2, Title: "rows", RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
		{RuleResourceId: "b", RuleRevision: 3, Title: "email", Column: "email"},
	}
	require.NotEqual(t, base, qualityContractFingerprint(settings, changedCheck))

	changedSettings := &storepb.QualityContractSettings{DefaultScheduleIntervalSeconds: 7200}
	require.NotEqual(t, base, qualityContractFingerprint(changedSettings, checks))
}

// The change summary names added, removed and modified checks, and ignores a
// check whose display fields and revision are unchanged.
func TestQualityContractChangeSummary(t *testing.T) {
	t.Parallel()

	published := []*storepb.QualityContractCheck{
		{RuleResourceId: "same", RuleRevision: 1},
		{RuleResourceId: "changed", RuleRevision: 1},
		{RuleResourceId: "removed", RuleRevision: 1},
	}
	live := []*storepb.QualityContractCheck{
		{RuleResourceId: "same", RuleRevision: 1},
		{RuleResourceId: "changed", RuleRevision: 2},
		{RuleResourceId: "added", RuleRevision: 1},
	}
	summary := qualityContractChangeSummary(published, live)
	require.Equal(t, int32(1), summary.Added)
	require.Equal(t, int32(1), summary.Modified)
	require.Equal(t, int32(1), summary.Removed)

	empty := qualityContractChangeSummary(published, published)
	require.Equal(t, int32(0), empty.Added)
	require.Equal(t, int32(0), empty.Modified)
	require.Equal(t, int32(0), empty.Removed)
}

// Settings validation accepts the documented schedule window and severity catalog
// and rejects anything else before the store is touched.
func TestValidateQualityContractSettings(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateQualityContractSettings(&storepb.QualityContractSettings{}))
	require.NoError(t, validateQualityContractSettings(&storepb.QualityContractSettings{
		DefaultScheduleIntervalSeconds: 900,
		DefaultSeverity:                storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
	}))
	require.Error(t, validateQualityContractSettings(&storepb.QualityContractSettings{
		DefaultScheduleIntervalSeconds: 60,
	}), "less than the minimum")
	require.Error(t, validateQualityContractSettings(&storepb.QualityContractSettings{
		DefaultScheduleIntervalSeconds: int64(8 * 24 * 3600),
	}), "more than the maximum")
	require.Error(t, validateQualityContractSettings(&storepb.QualityContractSettings{
		DefaultSeverity: storepb.QualitySeverity(99),
	}))
}

// The contract settings cross the API boundary without losing the filter or the
// creation defaults.
func TestConvertQualityContractSettings(t *testing.T) {
	t.Parallel()

	stored := &storepb.QualityContractSettings{
		Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
			{Column: "id", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT, Values: []string{"0"}},
		}},
		DefaultScheduleIntervalSeconds: 3600,
		DefaultSeverity:                storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
	}
	public := convertQualityContractSettingsToV1(stored)
	require.Len(t, public.GetFilter().GetClauses(), 1)
	require.Equal(t, "id", public.GetFilter().GetClauses()[0].GetColumn())
	require.Equal(t, int64(3600), public.GetDefaultScheduleIntervalSeconds())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, public.GetDefaultSeverity())

	back := convertQualityContractSettingsFromV1(public)
	require.Equal(t, stored.GetFilter().GetClauses()[0].GetColumn(), back.GetFilter().GetClauses()[0].GetColumn())
	require.Equal(t, stored.GetDefaultScheduleIntervalSeconds(), back.GetDefaultScheduleIntervalSeconds())
	require.Equal(t, stored.GetDefaultSeverity(), back.GetDefaultSeverity())
}

// The publisher id renders as a user resource name; an absent publisher stays
// empty rather than becoming users/0.
func TestFormatQualityContractPublisher(t *testing.T) {
	t.Parallel()

	require.Empty(t, formatQualityContractPublisher(nil))
	id := int32(7)
	require.Equal(t, "users/7", formatQualityContractPublisher(&id))
}
