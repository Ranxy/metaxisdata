package server

import (
	"testing"

	"github.com/stretchr/testify/require"
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
