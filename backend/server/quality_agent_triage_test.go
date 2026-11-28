package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestTriageEnumValuesBoundsTheList(t *testing.T) {
	t.Parallel()

	values := make([]string, 0, 40)
	for index := 0; index < 40; index++ {
		values = append(values, "label_"+string(rune('a'+index%26)))
	}
	trimmed, truncated := triageEnumValues(values)
	require.Len(t, trimmed, maxTriageContextEnumValues)
	require.True(t, truncated)

	longLabel := make([]byte, maxTriageContextEnumLabelBytes+1)
	for index := range longLabel {
		longLabel[index] = 'x'
	}
	trimmed, truncated = triageEnumValues([]string{"active", string(longLabel)})
	require.Equal(t, []string{"active"}, trimmed)
	require.True(t, truncated)

	trimmed, truncated = triageEnumValues(nil)
	require.Empty(t, trimmed, "a column without labels contributes none")
	require.False(t, truncated)
}

// The table context's allowedRuleTypes is what the drafting tool can produce, and it
// is computed server-side rather than by the tool: a view must not be told custom SQL
// is available, and a table must still get the column-level types back.
func TestTriageAllowedRuleTypesMatchesTheDraftVocabulary(t *testing.T) {
	t.Parallel()

	for _, view := range []storepb.MetaType{storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW} {
		allowed := triageAllowedRuleTypes(view, storepb.Engine_POSTGRES)
		require.Contains(t, allowed, "ROW_COUNT", "a %s can still be counted", view)
		require.Contains(t, allowed, "FRESHNESS")
		for _, refused := range []string{"CUSTOM_SQL", "SCHEMA_DRIFT", "RANGE", "NULL_PERCENT", "ACCEPTED_VALUES"} {
			require.NotContainsf(t, allowed, refused, "a %s must not be offered %s for drafting", view, refused)
		}
	}

	table := triageAllowedRuleTypes(storepb.MetaType_TABLE, storepb.Engine_POSTGRES)
	require.Contains(t, table, "RANGE")
	require.Contains(t, table, "UNIQUE")
	require.NotContains(t, table, "CUSTOM_SQL", "custom SQL runs on a table but is never drafted")
}

// The drafting result is how the model learns which proposals it just created; losing
// the id, the rule type or the confidence would force a second read of the queue and
// make same-wake self-correction impossible. A config this build cannot map must keep
// the draft listed instead of failing the result.
func TestDraftedChecksNamesWhatTheModelMustActOn(t *testing.T) {
	t.Parallel()

	minRows := int64(1)
	drafts := draftedChecks([]*store.QualitySuggestionMessage{
		{
			ResourceID: "draft-1",
			Confidence: 0.8,
			Payload: &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Rule{
				Rule: &storepb.QualityRuleDraftConfig{
					Title: "orders row count",
					Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
						RowCount: &storepb.RowCountConfig{MinRows: &minRows},
					}},
				},
			}},
		},
		{
			ResourceID: "draft-2",
			Payload: &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Rule{
				Rule: &storepb.QualityRuleDraftConfig{Title: "unknown config"},
			}},
		},
	})
	require.Len(t, drafts, 2)
	require.Equal(t, "qualitySuggestions/draft-1", drafts[0].ID)
	require.Equal(t, "orders row count", drafts[0].Title)
	require.Equal(t, "ROW_COUNT", drafts[0].RuleType)
	require.Equal(t, 0.8, drafts[0].Confidence)
	require.Equal(t, "qualitySuggestions/draft-2", drafts[1].ID)
	require.Empty(t, drafts[1].RuleType, "an unmapped config stays nameless rather than failing the result")

	require.Empty(t, draftedChecks(nil), "an empty drafting pass names no drafts")
}
