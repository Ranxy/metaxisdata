package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScanQualityAgentConfig(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	owner := int32(42)
	lastActive := time.Unix(2000, 0).UTC()
	values := []any{
		int64(7), int16(2), sql.NullInt32{Int32: owner, Valid: true},
		[]byte(`["suggest","adopt","patrol"]`), "providers/p", int16(2),
		"09:00", "Asia/Shanghai", int16(0), int16(2), []byte(`[3,4,12]`),
		float64(0.85), true, int32(7), int32(30), int64(1000), int32(5),
		int32(1), int32(2), int32(50), sql.NullTime{Time: lastActive, Valid: true},
		now, now,
	}
	config, err := scanQualityAgentConfig(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, int64(7), config.AgentID)
	require.Equal(t, QualityAgentStateActive, config.State)
	require.NotNil(t, config.OwnerPrincipalID)
	require.Equal(t, owner, *config.OwnerPrincipalID)
	require.Equal(t, []string{"suggest", "adopt", "patrol"}, config.Capabilities)
	require.Equal(t, QualityAgentReportFrequencyDaily, config.ReportFrequency)
	require.Equal(t, "09:00", config.ReportTime)
	require.Equal(t, "Asia/Shanghai", config.ReportTimezone)
	require.Equal(t, QualityAgentAdoptionModePaused, config.AdoptionMode)
	require.Equal(t, []int16{3, 4, 12}, config.AdoptionRuleTypes)
	require.InDelta(t, 0.85, config.AdoptionConfidenceThreshold, 1e-9)
	require.True(t, config.AdoptionAllowCritical)
	require.Equal(t, int32(7), config.AdoptionMaxPerDay)
	require.Equal(t, int32(30), config.ConversationMessagesPerHour)
	require.Equal(t, int64(1000), config.TokensPerHour)
	require.Equal(t, int32(5), config.WakesPerHour)
	require.Equal(t, int32(1), config.MaxConcurrentSessions)
	require.Equal(t, int32(2), config.MaxNewSessionsPerHour)
	require.Equal(t, int32(50), config.MaxActiveSessions)
	require.NotNil(t, config.LastActiveAt)
	require.Equal(t, lastActive, *config.LastActiveAt)
	require.Equal(t, now, config.CreatedAt)
}

func TestScanQualityAgentConfigWithoutOwnerOrLastActive(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	values := []any{
		int64(8), int16(1), sql.NullInt32{}, []byte(`[]`), "", int16(0),
		"", "", int16(0), int16(1), []byte(`[]`), float64(0.7), false,
		int32(10), int32(60), int64(0), int32(0), int32(0), int32(0), int32(0),
		sql.NullTime{}, now, now,
	}
	config, err := scanQualityAgentConfig(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Nil(t, config.OwnerPrincipalID)
	require.Nil(t, config.LastActiveAt)
	require.Empty(t, config.Capabilities)
	require.Empty(t, config.AdoptionRuleTypes)
}

func TestQualityAgentConfigJSONRoundTrip(t *testing.T) {
	t.Parallel()

	capabilities, err := marshalStrings([]string{"suggest", "patrol"})
	require.NoError(t, err)
	decoded, err := unmarshalStrings(capabilities)
	require.NoError(t, err)
	require.Equal(t, []string{"suggest", "patrol"}, decoded)

	ruleTypes, err := marshalInt16s([]int16{3, 12})
	require.NoError(t, err)
	decodedTypes, err := unmarshalInt16s(ruleTypes)
	require.NoError(t, err)
	require.Equal(t, []int16{3, 12}, decodedTypes)

	empty, err := marshalStrings(nil)
	require.NoError(t, err)
	require.Equal(t, []byte("[]"), empty)
}
