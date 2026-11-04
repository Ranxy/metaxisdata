package v1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The idempotency fingerprint must be stable across calls, sensitive to item order
// and to the base version: a retry returns the same request, a different batch is a
// different request.
func TestQualityChangeRequestFingerprint(t *testing.T) {
	t.Parallel()

	items := &storepb.QualityChangeRequestItems{Items: []*storepb.QualityChangeRequestItem{
		{Kind: storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE,
			Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, MinChecks: 1},
		{Kind: storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_SUGGESTION_REF,
			SuggestionResourceId: "abc"},
	}}
	fingerprint := qualityChangeRequestFingerprint(items, 3)
	require.Equal(t, fingerprint, qualityChangeRequestFingerprint(items, 3))
	require.NotEqual(t, fingerprint, qualityChangeRequestFingerprint(items, 4), "a different base is a different request")

	reordered := &storepb.QualityChangeRequestItems{Items: []*storepb.QualityChangeRequestItem{items.Items[1], items.Items[0]}}
	require.NotEqual(t, fingerprint, qualityChangeRequestFingerprint(reordered, 3), "item order names the batch")
}

// The rejection taxonomy is a closed set: a structured reason code is what makes a
// refusal usable later.
func TestQualityChangeRequestRejectionReasons(t *testing.T) {
	t.Parallel()

	require.Empty(t, func() []storepb.QualityChangeRejectionReason {
		reasons, err := qualityChangeRequestRejectionReasons(nil)
		require.NoError(t, err)
		return reasons
	}())
	reasons, err := qualityChangeRequestRejectionReasons([]v1pb.QualityChangeRejectionReason{
		v1pb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_NOISE,
		v1pb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_TOO_TIGHT,
	})
	require.NoError(t, err)
	require.Equal(t, []storepb.QualityChangeRejectionReason{
		storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_NOISE,
		storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_TOO_TIGHT,
	}, reasons)

	_, err = qualityChangeRequestRejectionReasons([]v1pb.QualityChangeRejectionReason{99})
	require.Error(t, err)
}

// A pending rule draft becomes the rule the accept path creates: title, severity and
// the typed config travel, and the target is the suggestion's.
func TestQualityRuleFromDraft(t *testing.T) {
	t.Parallel()

	minRows := int64(1)
	suggestion := &store.QualitySuggestionMessage{
		TargetGUID: "inst;db;public;orders",
		TargetType: storepb.MetaType_TABLE,
		Payload: &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Rule{
			Rule: &storepb.QualityRuleDraftConfig{
				Title:     "orders must have rows",
				Rationale: "the table is never empty",
				Severity:  storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
				Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
					RowCount: &storepb.RowCountConfig{MinRows: &minRows},
				}},
			},
		}},
	}
	rule, err := qualityRuleFromDraft(suggestion, storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS)
	require.NoError(t, err)
	require.Equal(t, "orders must have rows", rule.GetTitle())
	require.Equal(t, "the table is never empty", rule.GetDescription())
	require.Equal(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, rule.GetRuleType())
	require.Equal(t, v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, rule.GetDimension())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR, rule.GetSeverity())
	require.Equal(t, suggestion.TargetGUID, rule.GetTarget().GetMetaGuid())
	require.NotNil(t, rule.GetConfig().GetRowCount())

	_, err = qualityRuleFromDraft(&store.QualitySuggestionMessage{}, storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS)
	require.Error(t, err)
}

// Unknown item kinds are refused before any store access: the request cannot carry
// a red-line action.
func TestValidateQualityChangeRequestItemsRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}
	_, err := service.validateQualityChangeRequestItems(context.TODO(), "inst;db;public;orders",
		storepb.MetaType_TABLE, nil, []*v1pb.QualityChangeRequestItem{{Kind: v1pb.QualityChangeRequestItemKind(99)}})
	require.Error(t, err)
}

// A rendered request carries the resource name, the item shapes and the per-item
// outcomes.
func TestConvertQualityChangeRequestToV1(t *testing.T) {
	t.Parallel()

	createdBy := int32(7)
	message := &store.QualityChangeRequestMessage{
		ResourceID:  "cr1",
		TargetGUID:  "inst;db;public;orders",
		TargetType:  storepb.MetaType_TABLE,
		State:       storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPLIED,
		BaseVersion: 2,
		Source:      storepb.QualityChangeRequestSource_QUALITY_CHANGE_REQUEST_SOURCE_HUMAN,
		Items: &storepb.QualityChangeRequestItems{Items: []*storepb.QualityChangeRequestItem{{
			Kind:      storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE,
			Property:  storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_FRESHNESS,
			MinChecks: 1,
		}}},
		ItemResults: &storepb.QualityChangeRequestItemResults{Results: []*storepb.QualityChangeRequestItemResult{{
			Index: 0, Status: storepb.QualityChangeRequestItemResult_STATUS_APPLIED,
		}}},
		CreatedBy: &createdBy,
	}
	converted := convertQualityChangeRequestToV1("ws1", message)
	require.Equal(t, "workspaces/ws1/qualityChangeRequests/cr1", converted.GetName())
	require.Empty(t, converted.GetItems()[0].GetSuggestion(), "a coverage item has no suggestion name")
	require.Equal(t, v1pb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPLIED, converted.GetState())
	require.Len(t, converted.GetItems(), 1)
	require.Len(t, converted.GetItemResults(), 1)
	require.Equal(t, "users/7", converted.GetCreatedBy())
	require.Equal(t, common.FormatUserUID(7), converted.GetCreatedBy())
}

// A batch cannot declare the same property and column twice, and a minimum outside
// the declaration bounds is refused before any write.
func TestValidateQualityChangeRequestItemsBounds(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}
	duplicate := []*v1pb.QualityChangeRequestItem{
		{Kind: v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE,
			Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, MinChecks: 1},
		{Kind: v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE,
			Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, MinChecks: 2},
	}
	_, err := service.validateQualityChangeRequestItems(context.TODO(), "inst;db;public;orders",
		storepb.MetaType_TABLE, nil, duplicate)
	require.Error(t, err, "the same declaration twice is refused")

	tooManyChecks := []*v1pb.QualityChangeRequestItem{{
		Kind:      v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE,
		Property:  v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME,
		MinChecks: maxQualityPlanMinChecks + 1,
	}}
	_, err = service.validateQualityChangeRequestItems(context.TODO(), "inst;db;public;orders",
		storepb.MetaType_TABLE, nil, tooManyChecks)
	require.Error(t, err)
}

// The pre-validation rendering used for the idempotency lookup rejects an unknown
// kind and trims a suggestion name the same way the stored item does.
func TestQualityChangeRequestItemsForFingerprint(t *testing.T) {
	t.Parallel()

	items, err := qualityChangeRequestItemsForFingerprint([]*v1pb.QualityChangeRequestItem{{
		Kind:       v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_SUGGESTION_REF,
		Suggestion: "  workspaces/ws1/qualitySuggestions/abc  ",
	}})
	require.NoError(t, err)
	require.Equal(t, "workspaces/ws1/qualitySuggestions/abc", items.GetItems()[0].GetSuggestionResourceId())

	_, err = qualityChangeRequestItemsForFingerprint([]*v1pb.QualityChangeRequestItem{{Kind: v1pb.QualityChangeRequestItemKind(99)}})
	require.Error(t, err)
}
