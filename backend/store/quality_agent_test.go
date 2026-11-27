package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScanQualityAgentDecodesConfig(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	owner := int32(42)
	createdBy := int32(9)
	values := []any{
		int64(7), "agent-res", 11, "Patrol", "Reviews the warehouse", "zh-CN",
		"llm-provider-profiles/main", "reasoner",
		int16(2), sql.NullInt32{Int32: owner, Valid: true},
		[]byte(`["suggest","adopt","patrol"]`), int16(2), "09:00", "Asia/Shanghai",
		int16(1), int16(2), []byte(`[3,4,12]`), float64(0.85), true,
		int32(7), int32(30), int64(1000), int32(5), int32(1), int32(2), int32(50), int32(3), int32(4),
		int16(QualityAgentParticipationTune), int32(4), int32(9),
		sql.NullInt32{Int32: createdBy, Valid: true}, now, now,
	}
	agent, err := scanQualityAgent(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, int64(7), agent.ID)
	require.Equal(t, "agent-res", agent.ResourceID)
	require.Equal(t, "zh-CN", agent.Language)
	require.Equal(t, "llm-provider-profiles/main", agent.ProviderProfile)
	require.Equal(t, "reasoner", agent.ProviderModel)
	require.Equal(t, 11, agent.PrincipalID)
	require.Equal(t, QualityAgentStateActive, agent.State)
	require.NotNil(t, agent.OwnerPrincipalID)
	require.Equal(t, owner, *agent.OwnerPrincipalID)
	require.Equal(t, []string{"suggest", "adopt", "patrol"}, agent.Capabilities)
	require.Equal(t, QualityAgentReportFrequencyDaily, agent.ReportFrequency)
	require.Equal(t, "09:00", agent.ReportTime)
	require.Equal(t, "Asia/Shanghai", agent.ReportTimezone)
	require.Equal(t, int16(1), agent.ReportWeekday)
	require.Equal(t, QualityAgentAdoptionModePaused, agent.AdoptionMode)
	require.Equal(t, []int16{3, 4, 12}, agent.AdoptionRuleTypes)
	require.InDelta(t, 0.85, agent.AdoptionConfidenceThreshold, 1e-9)
	require.True(t, agent.AdoptionAllowCritical)
	require.Equal(t, int32(7), agent.AdoptionMaxPerDay)
	require.Equal(t, int32(30), agent.ConversationMessagesPerHour)
	require.Equal(t, int64(1000), agent.TokensPerHour)
	require.Equal(t, int32(5), agent.WakesPerHour)
	require.Equal(t, int32(1), agent.MaxConcurrentSessions)
	require.Equal(t, int32(2), agent.MaxNewSessionsPerHour)
	require.Equal(t, int32(50), agent.MaxActiveSessions)
	require.Equal(t, int32(3), agent.MaxTriageSessions)
	require.Equal(t, int32(4), agent.MaxNewTriageSessionsPerHour)
	require.Equal(t, QualityAgentParticipationTune, agent.Participation)
	require.Equal(t, int32(4), agent.RepairMaxPerDay)
	require.Equal(t, int32(9), agent.RunMaxPerRulePerDay)
	require.NotNil(t, agent.CreatedBy)
	require.Equal(t, createdBy, *agent.CreatedBy)
	require.Equal(t, now, agent.CreatedAt)
}

func TestScanQualityAgentWithoutOwnerOrCreator(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	values := []any{
		int64(8), "agent-res", 12, "Draft", "", "", "", "", int16(1), sql.NullInt32{},
		[]byte(`[]`), int16(0), "", "", int16(0), int16(2), []byte(`[]`),
		float64(0.7), false, int32(10), int32(60), int64(0), int32(0), int32(0),
		int32(0), int32(0), int32(0), int32(0), int16(QualityAgentParticipationRepair), int32(0), int32(0),
		sql.NullInt32{}, now, now,
	}
	agent, err := scanQualityAgent(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Nil(t, agent.OwnerPrincipalID)
	require.Nil(t, agent.CreatedBy)
	require.Empty(t, agent.ProviderProfile)
	require.Empty(t, agent.ProviderModel)
	require.Empty(t, agent.Capabilities)
	require.Empty(t, agent.AdoptionRuleTypes)
	require.Equal(t, QualityAgentStateDraft, agent.State)
}

// A nil list must encode as a JSON array, not as JSON null: capabilities and
// adoption_rule_types are NOT NULL JSONB columns defaulting to '[]', so a row
// written without a list still has to read back as an empty list.
func TestMarshalQualityAgentListsEncodeNilAsEmptyArray(t *testing.T) {
	t.Parallel()

	capabilities, err := marshalStrings(nil)
	require.NoError(t, err)
	require.Equal(t, []byte("[]"), capabilities)

	ruleTypes, err := marshalInt16s(nil)
	require.NoError(t, err)
	require.Equal(t, []byte("[]"), ruleTypes)
}
