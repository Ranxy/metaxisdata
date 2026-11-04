package v1

import (
	"context"
	"strings"
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
		{
			ID: 2, ResourceID: "b", Revision: 1,
			Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: "email"},
			}},
		},
		{
			ID: 1, ResourceID: "a", Revision: 2,
			Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
				RowCount: &storepb.RowCountConfig{},
			}},
		},
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
	settings := &storepb.QualityContractSettings{
		DefaultSeverity: storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
	}
	base := qualityContractFingerprint(settings, checks)
	require.Len(t, base, 64)
	require.Equal(t, base, qualityContractFingerprint(settings, checks))

	changedCheck := []*storepb.QualityContractCheck{
		{RuleResourceId: "a", RuleRevision: 2, Title: "rows", RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
		{RuleResourceId: "b", RuleRevision: 3, Title: "email", Column: "email"},
	}
	require.NotEqual(t, base, qualityContractFingerprint(settings, changedCheck))

	changedSettings := &storepb.QualityContractSettings{
		DefaultSeverity: storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL,
	}
	require.NotEqual(t, base, qualityContractFingerprint(changedSettings, checks))
}

// The change summary names added, removed and modified checks, and ignores a
// check whose display fields and revision are unchanged. The comparison itself is
// the store's, so a publish transaction and this read view agree on the counts.
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
	added, modified, removed := store.QualityContractCheckDiff(published, live)
	summary := qualityContractChangeSummary(added, modified, removed)
	require.Equal(t, int32(1), summary.GetAdded())
	require.Equal(t, int32(1), summary.GetModified())
	require.Equal(t, int32(1), summary.GetRemoved())

	added, modified, removed = store.QualityContractCheckDiff(published, published)
	empty := qualityContractChangeSummary(added, modified, removed)
	require.Equal(t, int32(0), empty.GetAdded())
	require.Equal(t, int32(0), empty.GetModified())
	require.Equal(t, int32(0), empty.GetRemoved())

	// A column change alone counts as modified: the published check records the
	// checked column.
	renamed := []*storepb.QualityContractCheck{
		{RuleResourceId: "same", RuleRevision: 1, Column: "email"},
		{RuleResourceId: "changed", RuleRevision: 1},
		{RuleResourceId: "removed", RuleRevision: 1},
	}
	_, modified, _ = store.QualityContractCheckDiff(published, renamed)
	require.Equal(t, int32(1), modified)
}

// Settings validation accepts the severity catalog and rejects anything else
// before the store is touched.
func TestValidateQualityContractSettings(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateQualityContractSettings(&storepb.QualityContractSettings{}))
	require.NoError(t, validateQualityContractSettings(&storepb.QualityContractSettings{
		DefaultSeverity: storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
	}))
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
		DefaultSeverity: storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
	}
	public := convertQualityContractSettingsToV1(stored)
	require.Len(t, public.GetFilter().GetClauses(), 1)
	require.Equal(t, "id", public.GetFilter().GetClauses()[0].GetColumn())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, public.GetDefaultSeverity())

	back := convertQualityContractSettingsFromV1(public)
	require.Equal(t, stored.GetFilter().GetClauses()[0].GetColumn(), back.GetFilter().GetClauses()[0].GetColumn())
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

// The publish note has a bounded size; the handler rejects rather than truncating.
func TestQualityContractNote(t *testing.T) {
	t.Parallel()

	note, err := qualityContractNote("  first  ")
	require.NoError(t, err)
	require.Equal(t, "first", note)

	_, err = qualityContractNote(strings.Repeat("x", maxQualityContractNoteBytes+1))
	require.Error(t, err)
}

// The v2 fingerprint must be deterministic bytes: protojson output is seeded per
// build, so hashing it would make an unchanged contract look changed after a
// redeploy. These assertions pin the properties the versioning relies on.
func TestQualityContractFingerprintV2IsDeterministic(t *testing.T) {
	t.Parallel()

	settings := &storepb.QualityContractSettings{}
	checks := []*storepb.QualityContractCheck{{RuleResourceId: "a", RuleRevision: 1, Title: "rows"}}
	first := qualityContractFingerprintV2(settings, checks)
	require.Equal(t, first, qualityContractFingerprintV2(settings, checks))
	require.True(t, strings.HasPrefix(first, contractSnapshotVersionPrefixV2))
	require.NotEqual(t, qualityContractFingerprint(settings, checks), first,
		"a v1 and a v2 fingerprint must never compare equal")

	changed := []*storepb.QualityContractCheck{{RuleResourceId: "a", RuleRevision: 2, Title: "rows"}}
	require.NotEqual(t, first, qualityContractFingerprintV2(settings, changed))
}

func TestQualityContractSnapshotV2(t *testing.T) {
	t.Parallel()

	settings := &storepb.QualityContractSettings{}
	checks := []*storepb.QualityContractCheck{{RuleResourceId: "a", RuleRevision: 1, Title: "rows"}}
	snapshot := qualityContractSnapshotV2(settings, checks)
	require.Equal(t, int32(contractSnapshotVersionV2), snapshot.GetSnapshotVersion())
	require.Equal(t, qualityContractFingerprintV2(settings, checks), snapshot.GetFingerprint())
}

// Legacy snapshots are compared by content, not by re-hashing their v1
// fingerprint (which a different build would compute differently).
func TestQualityContractSnapshotContentEqual(t *testing.T) {
	t.Parallel()

	settings := &storepb.QualityContractSettings{}
	checks := []*storepb.QualityContractCheck{{RuleResourceId: "a", RuleRevision: 1, Title: "rows"}}
	require.True(t, store.QualityContractSnapshotContentEqual(
		&storepb.QualityContractSnapshot{Settings: settings, Checks: checks, Fingerprint: "old", SnapshotVersion: 0},
		&storepb.QualityContractSnapshot{Settings: settings, Checks: checks, Fingerprint: "v2:new", SnapshotVersion: 2},
	), "fingerprint and schema version are not content")

	changed := []*storepb.QualityContractCheck{{RuleResourceId: "a", RuleRevision: 2, Title: "rows"}}
	require.False(t, store.QualityContractSnapshotContentEqual(
		&storepb.QualityContractSnapshot{Settings: settings, Checks: checks},
		&storepb.QualityContractSnapshot{Settings: settings, Checks: changed},
	))
}

// The view wiring must use content comparison for a legacy revision; using the v2
// fingerprint there would make every legacy contract look changed forever.
func TestQualityContractViewLegacyHasUnpublishedChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	settings := &storepb.QualityContractSettings{}
	minimum := int64(1)
	rules := []*store.QualityRuleMessage{{
		ResourceID: "a",
		Revision:   1,
		Title:      "rows",
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MinRows: &minimum},
		}},
	}}
	checks := buildQualityContractChecks(rules)
	revision := &store.QualityContractRevisionMessage{Snapshot: &storepb.QualityContractSnapshot{
		Settings:    settings,
		Checks:      checks,
		Fingerprint: qualityContractFingerprint(settings, checks),
	}}
	contract := &store.QualityContractMessage{Settings: settings}
	service := &DataQualityService{}

	view, err := service.qualityContractViewWith(ctx, "inst;shop;public;orders", storepb.MetaType_TABLE, contract, rules, revision)
	require.NoError(t, err)
	require.False(t, view.GetHasUnpublishedChanges(), "unchanged legacy content must not look unpublished")

	changed := append([]*store.QualityRuleMessage{}, rules...)
	changed = append(changed, &store.QualityRuleMessage{
		ResourceID: "b",
		Revision:   1,
		Title:      "more rows",
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
	})
	view, err = service.qualityContractViewWith(ctx, "inst;shop;public;orders", storepb.MetaType_TABLE, contract, changed, revision)
	require.NoError(t, err)
	require.True(t, view.GetHasUnpublishedChanges())
}
