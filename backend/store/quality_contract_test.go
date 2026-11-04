package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A version's kind names which sections changed against its predecessor; a diff in
// more than one section collapses to BOTH.
func TestQualityContractChangeKindOf(t *testing.T) {
	t.Parallel()

	base := &storepb.QualityContractSnapshot{
		Settings: &storepb.QualityContractSettings{},
		Plan:     &storepb.QualityContractPlan{},
		Checks:   []*storepb.QualityContractCheck{{RuleResourceId: "a"}},
	}
	require.Equal(t, storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_UNSPECIFIED,
		QualityContractChangeKindOf(base, base, 0, 0, 0))

	intent := &storepb.QualityContractSnapshot{
		Settings: &storepb.QualityContractSettings{Intent: &storepb.QualityContractIntent{
			Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_IMPORTANT,
		}},
		Plan:   base.GetPlan(),
		Checks: base.GetChecks(),
	}
	require.Equal(t, storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_INTENT,
		QualityContractChangeKindOf(base, intent, 0, 0, 0))

	plan := &storepb.QualityContractSnapshot{
		Settings: base.GetSettings(),
		Plan:     &storepb.QualityContractPlan{Coverage: []*storepb.QualityContractPlanCoverage{{MinChecks: 1}}},
		Checks:   base.GetChecks(),
	}
	require.Equal(t, storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_PLAN,
		QualityContractChangeKindOf(base, plan, 0, 0, 0))
	// The plan is derived from the intent, so an edit that moves both is INTENT.
	bothPlanAndIntent := &storepb.QualityContractSnapshot{
		Settings: intent.GetSettings(),
		Plan:     plan.GetPlan(),
		Checks:   base.GetChecks(),
	}
	require.Equal(t, storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_INTENT,
		QualityContractChangeKindOf(base, bothPlanAndIntent, 0, 0, 0))
	require.Equal(t, storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_CHECKS,
		QualityContractChangeKindOf(base, base, 1, 0, 0))
	require.Equal(t, storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_BOTH,
		QualityContractChangeKindOf(base, intent, 0, 1, 0))

	// A v2 baseline has no intent/plan fields; publishing an empty plan must not
	// classify as either section changing.
	legacy := &storepb.QualityContractSnapshot{Checks: base.GetChecks()}
	require.Equal(t, storepb.QualityContractChangeKind_QUALITY_CONTRACT_CHANGE_KIND_UNSPECIFIED,
		QualityContractChangeKindOf(legacy, &storepb.QualityContractSnapshot{
			Checks: base.GetChecks(),
			Plan:   &storepb.QualityContractPlan{},
		}, 0, 0, 0))
}
