package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
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
