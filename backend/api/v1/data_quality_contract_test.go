package v1

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

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

	back, err := convertQualityContractSettingsFromV1(public)
	require.NoError(t, err)
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

// An intent crosses the API boundary with its criticality, statements, dependents,
// exemptions and timestamps intact.
func TestConvertQualityContractIntentRoundTrip(t *testing.T) {
	t.Parallel()

	public := &v1pb.QualityContractIntent{
		Criticality: v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_CRITICAL,
		Statements: []*v1pb.QualityContractIntentStatement{
			{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_FRESHNESS, Tolerance: "6h"},
			{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_NULL_RATE, Column: "email"},
		},
		Dependents:    []string{"inst-1;shop;public;orders_upstream"},
		EffectiveFrom: timestamppb.New(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
		Source:        v1pb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_IMPORTED,
		DomainRef:     "domain/sales",
		Exemptions: []*v1pb.QualityContractIntentExemption{
			{
				Property:  v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_SCHEMA_STABILITY,
				Reason:    "pinned upstream",
				ExpiresAt: timestamppb.New(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
			},
		},
	}
	stored, err := convertQualityContractIntentFromV1(public)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL, stored.GetCriticality())
	require.Len(t, stored.GetStatements(), 2)
	require.Zero(t, stored.GetExemptions()[0].GetApprovedBy(), "approved_by is output only")
	require.NotZero(t, stored.GetExemptions()[0].GetExpiresAtUnixMs())
	require.Equal(t, int64(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixMilli()), stored.GetEffectiveFromUnixMs())

	back := convertQualityContractIntentToV1(stored)
	require.Equal(t, public.GetCriticality(), back.GetCriticality())
	require.Equal(t, public.GetStatements()[0].GetTolerance(), back.GetStatements()[0].GetTolerance())
	require.Equal(t, public.GetStatements()[1].GetColumn(), back.GetStatements()[1].GetColumn())
	require.Equal(t, public.GetDependents(), back.GetDependents())
	require.Equal(t, public.GetSource(), back.GetSource())
	require.Equal(t, public.GetDomainRef(), back.GetDomainRef())
	require.Equal(t, public.GetEffectiveFrom().AsTime(), back.GetEffectiveFrom().AsTime())
	require.Equal(t, public.GetExemptions()[0].GetExpiresAt().AsTime(), back.GetExemptions()[0].GetExpiresAt().AsTime())
}

// The shape validation rejects what the store must not hold.
func TestValidateQualityContractIntentShape(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateQualityContractIntentShape(&v1pb.QualityContractIntent{}))

	for name, intent := range map[string]*v1pb.QualityContractIntent{
		"unknown criticality": {Criticality: v1pb.QualityAssetCriticality(99)},
		"unknown property":    {Statements: []*v1pb.QualityContractIntentStatement{{Property: v1pb.QualityCoverageProperty(99)}}},
		"too many statements": {Statements: make([]*v1pb.QualityContractIntentStatement, maxContractIntentStatements+1)},
		"empty dependent":     {Dependents: []string{" "}},
		"to many dependents":  {Dependents: make([]string, maxContractIntentDependents+1)},
		"reasonless exemption": {
			Exemptions: []*v1pb.QualityContractIntentExemption{
				{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME},
			},
		},
		"exemption without property": {
			Exemptions: []*v1pb.QualityContractIntentExemption{{Reason: "because"}},
		},
		"invalid timestamp": {
			EffectiveFrom: &timestamppb.Timestamp{Seconds: 1e18},
		},
	} {
		require.Error(t, validateQualityContractIntentShape(intent), name)
	}
}

// The server records the editor as the exemptions' approver and defaults the
// source, and an intent that declares nothing is dropped.
func TestNormalizeQualityContractIntent(t *testing.T) {
	t.Parallel()

	require.Nil(t, normalizeQualityContractIntent(nil, 7, nil))
	require.Nil(t, normalizeQualityContractIntent(&storepb.QualityContractIntent{
		Source: storepb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_HUMAN,
	}, 7, nil), "a source alone is not a declaration")

	previous := &storepb.QualityContractIntent{
		Exemptions: []*storepb.QualityContractIntentExemption{
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Reason: "no volume drift expected", ApprovedBy: 3},
		},
	}
	intent := normalizeQualityContractIntent(&storepb.QualityContractIntent{
		Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_IMPORTANT,
		Exemptions: []*storepb.QualityContractIntentExemption{
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Reason: "no volume drift expected"},
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_FRESHNESS, Reason: "refreshed hourly"},
		},
	}, 7, previous)
	require.NotNil(t, intent)
	require.Equal(t, storepb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_HUMAN, intent.GetSource())
	require.Equal(t, int32(3), intent.GetExemptions()[0].GetApprovedBy(), "an unchanged exemption keeps its approver")
	require.Equal(t, int32(7), intent.GetExemptions()[1].GetApprovedBy(), "a new exemption records the editor")
}

// A published version exposes the intent it recorded; a version published before
// intent existed reads as absent rather than empty.
func TestConvertQualityContractRevisionIntent(t *testing.T) {
	t.Parallel()

	revision := &store.QualityContractRevisionMessage{
		Version: 3,
		Snapshot: &storepb.QualityContractSnapshot{
			Settings: &storepb.QualityContractSettings{
				Intent: &storepb.QualityContractIntent{
					Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL,
				},
			},
		},
	}
	require.Equal(t, v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_CRITICAL,
		convertQualityContractRevisionToV1(revision).GetIntent().GetCriticality())

	legacy := convertQualityContractRevisionToV1(&store.QualityContractRevisionMessage{Version: 1})
	require.Nil(t, legacy.GetIntent())
}

// The intent participates in the v2 fingerprint: an intent-only edit must produce a
// new version, and the same intent must fingerprint identically.
func TestQualityContractFingerprintV2CoversIntent(t *testing.T) {
	t.Parallel()

	checks := []*storepb.QualityContractCheck{{RuleResourceId: "r1", RuleRevision: 1}}
	plain := &storepb.QualityContractSettings{}
	withIntent := &storepb.QualityContractSettings{
		Intent: &storepb.QualityContractIntent{
			Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL,
		},
	}
	require.NotEqual(t, qualityContractFingerprintV2(plain, checks), qualityContractFingerprintV2(withIntent, checks))
	require.Equal(t, qualityContractFingerprintV2(withIntent, checks), qualityContractFingerprintV2(withIntent, checks))
}

// Editing an exemption makes it a new declaration attributed to this editor; an
// unchanged one keeps its approver.
func TestNormalizeQualityContractIntentReattributesChangedExemption(t *testing.T) {
	t.Parallel()

	previous := &storepb.QualityContractIntent{
		Exemptions: []*storepb.QualityContractIntentExemption{
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Reason: "first", ApprovedBy: 3},
		},
	}
	intent := normalizeQualityContractIntent(&storepb.QualityContractIntent{
		Exemptions: []*storepb.QualityContractIntentExemption{
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Reason: "changed"},
		},
	}, 7, previous)
	require.NotNil(t, intent)
	require.Equal(t, int32(7), intent.GetExemptions()[0].GetApprovedBy())
}

// The store, plan and API enums are converted by cast, so their numbering is
// load-bearing: a renumber in one proto must fail this test, not silently pick the
// wrong property.
func TestCoverageEnumNumberingMatches(t *testing.T) {
	t.Parallel()

	for property := storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME; property <= storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_RANGE; property++ {
		require.Equal(t, int32(property), int32(v1pb.QualityCoverageProperty(property)))
	}
	for criticality := storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_EXPERIMENTAL; criticality <= storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL; criticality++ {
		require.Equal(t, int32(criticality), int32(v1pb.QualityAssetCriticality(criticality)))
	}
	for source := storepb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_HUMAN; source <= storepb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_IMPORTED; source++ {
		require.Equal(t, int32(source), int32(v1pb.QualityContractIntentSource(source)))
	}
}

// An unknown source must be rejected rather than stored as a meaningless number.
func TestValidateQualityContractIntentShapeRejectsUnknownSource(t *testing.T) {
	t.Parallel()

	require.Error(t, validateQualityContractIntentShape(&v1pb.QualityContractIntent{
		Source: v1pb.QualityContractIntentSource(99),
	}))
}

// The v3 fingerprint covers the plan payload: a cadence-only change must produce a
// new version, while the same payload fingerprints identically.
func TestQualityContractFingerprintV3CoversPlan(t *testing.T) {
	t.Parallel()

	settings := &storepb.QualityContractSettings{}
	checks := []*storepb.QualityContractCheck{{RuleResourceId: "a"}}
	plan := &storepb.QualityContractPlan{
		Coverage: []*storepb.QualityContractPlanCoverage{{MinChecks: 1}},
		Cadence:  &storepb.QualityContractPlanCadence{IntervalSeconds: 3600},
	}
	require.Equal(t, qualityContractFingerprintV3(settings, plan, checks), qualityContractFingerprintV3(settings, plan, checks))

	otherCadence := &storepb.QualityContractPlan{Coverage: plan.GetCoverage(), Cadence: &storepb.QualityContractPlanCadence{IntervalSeconds: 7200}}
	require.NotEqual(t, qualityContractFingerprintV3(settings, plan, checks), qualityContractFingerprintV3(settings, otherCadence, checks))
	// A v3 fingerprint never equals the v2 fingerprint of the same settings and
	// checks, so a no-op comparison cannot cross schemas.
	require.NotEqual(t, qualityContractFingerprintV2(settings, checks), qualityContractFingerprintV3(settings, plan, checks))
}

// The snapshot a publish writes records the v3 schema and a v3 fingerprint.
func TestQualityContractSnapshotV3(t *testing.T) {
	t.Parallel()

	snapshot := qualityContractSnapshotV3(
		&storepb.QualityContractSettings{},
		&storepb.QualityContractPlan{Coverage: []*storepb.QualityContractPlanCoverage{{MinChecks: 1}}},
		nil,
	)
	require.Equal(t, int32(3), snapshot.GetSnapshotVersion())
	require.True(t, strings.HasPrefix(snapshot.GetFingerprint(), contractSnapshotVersionPrefixV3))
	require.NotNil(t, snapshot.GetPlan())
}

// The published plan payload is the same declaration the plan card renders, plus the
// target's cadence projection.
func TestQualityContractPlanSnapshot(t *testing.T) {
	t.Parallel()

	rules := []*store.QualityRuleMessage{{
		TargetGUID: "inst-1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		State:      storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
	}}
	intent := &storepb.QualityContractIntent{
		Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL,
		Statements: []*storepb.QualityContractIntentStatement{{
			Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_NULL_RATE,
			Column:   "email",
		}},
	}
	schedule := &store.QualityTargetScheduleMessage{TargetGUID: rules[0].TargetGUID, ScheduleInterval: 6 * time.Hour}

	plan := qualityContractPlanSnapshot(intent, rules, schedule)
	require.Len(t, plan.GetCoverage(), 5, "four critical template requirements plus the statement")
	require.Equal(t, int64(6*60*60), plan.GetCadence().GetIntervalSeconds())
	sourceByProperty := map[storepb.QualityCoverageProperty]storepb.QualityCoverageSource{}
	for _, declaration := range plan.GetCoverage() {
		sourceByProperty[declaration.GetProperty()] = declaration.GetSource()
	}
	require.Equal(t, storepb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_TEMPLATE,
		sourceByProperty[storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME])
	require.Equal(t, storepb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_INTENT,
		sourceByProperty[storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_NULL_RATE])
}

// A revision exposes its change kind and the plan it recorded.
func TestConvertQualityContractRevisionChangeKindAndPlan(t *testing.T) {
	t.Parallel()

	revision := &store.QualityContractRevisionMessage{
		Version:    2,
		ChangeKind: storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_PLAN,
		Snapshot: &storepb.QualityContractSnapshot{
			Plan: &storepb.QualityContractPlan{
				Coverage: []*storepb.QualityContractPlanCoverage{{
					Property:  storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME,
					MinChecks: 1,
					Source:    storepb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_TEMPLATE,
				}},
			},
		},
	}
	converted := convertQualityContractRevisionToV1(revision)
	require.Equal(t, v1pb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_PLAN, converted.GetChangeKind())
	require.Len(t, converted.GetPlan().GetCoverage(), 1)
}

// A contract whose newest version is v2 still compares with the v2 schema after the
// plan payload lands: an unchanged contract must not look unpublished, and a real
// drift must.
func TestQualityContractViewV2HasUnpublishedChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	settings := &storepb.QualityContractSettings{}
	rules := []*store.QualityRuleMessage{{
		ResourceID: "a",
		Revision:   1,
		Title:      "rows",
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
	}}
	checks := buildQualityContractChecks(rules)
	revision := &store.QualityContractRevisionMessage{Snapshot: &storepb.QualityContractSnapshot{
		Settings:        settings,
		Checks:          checks,
		Fingerprint:     qualityContractFingerprintV2(settings, checks),
		SnapshotVersion: 2,
	}}
	contract := &store.QualityContractMessage{Settings: settings}
	service := &DataQualityService{}

	view, err := service.qualityContractViewWith(ctx, "inst;shop;public;orders", storepb.MetaType_TABLE, contract, rules, revision)
	require.NoError(t, err)
	require.False(t, view.GetHasUnpublishedChanges(), "an untouched v2 contract must not look unpublished after the v3 upgrade")

	changed := append([]*store.QualityRuleMessage{}, rules...)
	changed = append(changed, &store.QualityRuleMessage{ResourceID: "b", Revision: 1, Title: "fresh", RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS})
	view, err = service.qualityContractViewWith(ctx, "inst;shop;public;orders", storepb.MetaType_TABLE, contract, changed, revision)
	require.NoError(t, err)
	require.True(t, view.GetHasUnpublishedChanges())
}

// A staged declaration is visible as a draft, cannot hide behind a pre-v3 version,
// and says plainly when the published version has no plan payload to diff against.
func TestQualityContractViewStagedPlan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	settings := &storepb.QualityContractSettings{}
	contract := &store.QualityContractMessage{
		Settings:          settings,
		Staged:            &storepb.QualityContractPlan{Coverage: []*storepb.QualityContractPlanCoverage{{MinChecks: 1}}},
		StagedBaseVersion: 2,
	}
	revision := &store.QualityContractRevisionMessage{Snapshot: &storepb.QualityContractSnapshot{
		Settings:        settings,
		Fingerprint:     qualityContractFingerprintV2(settings, nil),
		SnapshotVersion: 2,
	}}
	service := &DataQualityService{}
	view, err := service.qualityContractViewWith(ctx, "inst;shop;public;orders", storepb.MetaType_TABLE, contract, nil, revision)
	require.NoError(t, err)
	require.NotNil(t, view.GetStagedPlan())
	require.Equal(t, int32(2), view.GetStagedBaseVersion())
	require.True(t, view.GetHasUnpublishedChanges(), "a declaration cannot be represented in a v2 version")
	require.True(t, view.GetDiff().GetPlanNotRecorded())
}

// A declaration on a target that has never been published is still an unpublished
// change: the declaration alone is content to publish.
func TestQualityContractUnpublishedWithoutVersion(t *testing.T) {
	t.Parallel()

	settings := &storepb.QualityContractSettings{}
	require.False(t, qualityContractUnpublishedWithoutVersion(settings, nil, nil))
	declared := &store.QualityContractMessage{
		Staged: &storepb.QualityContractPlan{Coverage: []*storepb.QualityContractPlanCoverage{{MinChecks: 1}}},
	}
	require.True(t, qualityContractUnpublishedWithoutVersion(settings, declared, nil),
		"a declaration with no published version is unpublished")
}

// The working plan is the declaration when one exists and the derived projection
// otherwise.
func TestQualityContractWorkingPlan(t *testing.T) {
	t.Parallel()

	declared := &store.QualityContractMessage{
		Staged: &storepb.QualityContractPlan{Coverage: []*storepb.QualityContractPlanCoverage{{
			Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_NULL_RATE,
			Column:   "email",
		}}},
	}
	settings := &storepb.QualityContractSettings{}
	plan := qualityContractWorkingPlan(settings, nil, declared, nil)
	require.Len(t, plan.GetCoverage(), 1)
	require.Equal(t, "email", plan.GetCoverage()[0].GetColumn())

	derived := qualityContractWorkingPlan(settings, nil, &store.QualityContractMessage{Settings: settings}, nil)
	require.Len(t, derived.GetCoverage(), 2, "no declaration falls back to the IMPORTANT template")
	require.Equal(t, storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, derived.GetCoverage()[0].GetProperty())
}
