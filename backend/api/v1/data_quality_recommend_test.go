package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// recommendNullRateCandidate builds one null-rate candidate for a column, which is
// enough to exercise the queue planner's identity and coverage handling.
func recommendNullRateCandidate(column string) dataquality.Recommendation {
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: column, MaxNullPct: proto.Float64(1)},
	}}
	return dataquality.Recommendation{
		RuleType:  storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:    config,
		Signature: dataquality.RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config),
	}
}

// TestPlanRecommendationQueue pins the reconciliation rules: a saved rule wins, a
// decided identity stays decided, a pending proposal from another pass is left alone,
// and the deterministic pass may refresh its own proposal.
func TestPlanRecommendationQueue(t *testing.T) {
	t.Parallel()

	queued := recommendNullRateCandidate("queued")
	covered := recommendNullRateCandidate("covered")
	coveredClosed := recommendNullRateCandidate("covered-closed")
	labelDraft := recommendNullRateCandidate("label-draft")
	intentDraft := recommendNullRateCandidate("intent-draft")
	ownDraft := recommendNullRateCandidate("own-draft")
	rejected := recommendNullRateCandidate("rejected")
	candidates := []dataquality.Recommendation{queued, covered, coveredClosed, labelDraft, intentDraft, ownDraft, rejected}
	ruleSignatures := map[string]struct{}{
		covered.Signature:       {},
		coveredClosed.Signature: {},
	}
	states := map[string]store.QualitySuggestionSignatureState{
		coveredClosed.Signature: {
			Pending: true, PendingSource: storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION,
		},
		labelDraft.Signature: {
			Pending: true, PendingSource: storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		},
		intentDraft.Signature: {
			Pending: true, PendingSource: storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE,
		},
		ownDraft.Signature: {
			Pending: true, PendingSource: storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION,
		},
		rejected.Signature: {
			LastDecision: storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED,
		},
	}

	plan := planRecommendationQueue(candidates, ruleSignatures, states, false)
	require.Equal(t, []string{coveredClosed.Signature}, plan.Supersede)
	require.Equal(t, 1, int(plan.Summary.GetSkippedExistingFact()))
	require.Equal(t, 1, int(plan.Summary.GetSkippedPending()), "only the explicit intent draft is protected")
	require.Equal(t, 1, int(plan.Summary.GetSkippedDecided()))

	queuedSignatures := make(map[string]struct{}, len(plan.Queue))
	for _, candidate := range plan.Queue {
		queuedSignatures[candidate.Signature] = struct{}{}
	}
	require.Contains(t, queuedSignatures, queued.Signature)
	require.Contains(t, queuedSignatures, labelDraft.Signature, "a default-parameter label draft is replaced by observed thresholds")
	require.Contains(t, queuedSignatures, ownDraft.Signature, "a pass refreshes its own proposal")
	require.NotContains(t, queuedSignatures, intentDraft.Signature)
	require.NotContains(t, queuedSignatures, rejected.Signature)

	forced := planRecommendationQueue(candidates, ruleSignatures, states, true)
	require.Contains(t, forced.Queue, recommendCandidateBySignature(forced.Queue, rejected.Signature), "force regeneration re-proposes a decided identity")
}

// recommendCandidateBySignature returns the candidate with the given identity, so a
// require.Contains failure prints the candidate rather than an empty value.
func recommendCandidateBySignature(candidates []dataquality.Recommendation, signature string) dataquality.Recommendation {
	for _, candidate := range candidates {
		if candidate.Signature == signature {
			return candidate
		}
	}
	return dataquality.Recommendation{}
}
