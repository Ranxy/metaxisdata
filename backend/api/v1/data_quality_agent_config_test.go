package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestParseQualityAgentConfigDefaults(t *testing.T) {
	t.Parallel()

	config, databases, err := parseQualityAgentConfig(&v1pb.QualityAgent{
		Owner:        "users/7",
		Databases:    []string{"instances/i/databases/d"},
		Capabilities: []string{"suggest", "adopt"},
		Adoption:     &v1pb.QualityAgentAdoptionSetting{Mode: v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_ACTIVE},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"instances/i/databases/d"}, databases)
	require.NotNil(t, config.OwnerPrincipalID)
	require.Equal(t, int32(7), *config.OwnerPrincipalID)
	require.Equal(t, store.QualityAgentAdoptionModeActive, config.AdoptionMode)
	require.InDelta(t, 0.7, config.AdoptionConfidenceThreshold, 1e-9)
	require.Equal(t, int32(10), config.AdoptionMaxPerDay)
	require.Equal(t, int32(60), config.ConversationMessagesPerHour)
	require.Empty(t, config.Language, "an omitted language follows the owner")
}

func TestParseQualityAgentConfigValidation(t *testing.T) {
	t.Parallel()

	base := func() *v1pb.QualityAgent {
		return &v1pb.QualityAgent{
			Owner:        "users/7",
			Databases:    []string{"instances/i/databases/d"},
			Capabilities: []string{"suggest"},
		}
	}

	t.Run("unsupported language", func(t *testing.T) {
		t.Parallel()
		agent := base()
		agent.Language = "fr-FR"
		_, _, err := parseQualityAgentConfig(agent)
		require.Error(t, err)
	})
	t.Run("supported language", func(t *testing.T) {
		t.Parallel()
		agent := base()
		agent.Language = "zh-CN"
		config, _, err := parseQualityAgentConfig(agent)
		require.NoError(t, err)
		require.Equal(t, "zh-CN", config.Language)
	})
	t.Run("unknown capability", func(t *testing.T) {
		t.Parallel()
		agent := base()
		agent.Capabilities = []string{"teleport"}
		_, _, err := parseQualityAgentConfig(agent)
		require.Error(t, err)
	})
	t.Run("adopt requires suggest", func(t *testing.T) {
		t.Parallel()
		agent := base()
		agent.Capabilities = []string{"adopt"}
		_, _, err := parseQualityAgentConfig(agent)
		require.Error(t, err)
	})
	t.Run("needs a database", func(t *testing.T) {
		t.Parallel()
		agent := base()
		agent.Databases = nil
		_, _, err := parseQualityAgentConfig(agent)
		require.Error(t, err)
	})
	t.Run("database must be a resource name", func(t *testing.T) {
		t.Parallel()
		agent := base()
		agent.Databases = []string{"not-a-database"}
		_, _, err := parseQualityAgentConfig(agent)
		require.Error(t, err)
	})
	t.Run("weekly report needs a weekday", func(t *testing.T) {
		t.Parallel()
		agent := base()
		agent.Capabilities = []string{"patrol"}
		agent.ReportSchedule = &v1pb.QualityAgentReportSchedule{
			Frequency: v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_WEEKLY,
			Time:      "09:00",
			Timezone:  "UTC",
		}
		_, _, err := parseQualityAgentConfig(agent)
		require.Error(t, err)
	})
}

func TestApplyQualityAgentUpdateKeepsUnmaskedFields(t *testing.T) {
	t.Parallel()

	current := &store.QualityAgentMessage{
		ID:                          5,
		OwnerPrincipalID:            int32Pointer(7),
		State:                       store.QualityAgentStatePaused,
		Capabilities:                []string{"suggest"},
		Language:                    "zh-CN",
		AdoptionConfidenceThreshold: 0.7,
		AdoptionMaxPerDay:           10,
		ConversationMessagesPerHour: 60,
	}
	next, databases, err := applyQualityAgentUpdate(&v1pb.QualityAgent{
		Capabilities: []string{"suggest", "patrol"},
		Language:     "en-US",
	}, current, []string{"instances/i/databases/d"}, []string{"capabilities"})
	require.NoError(t, err)
	require.Equal(t, []string{"instances/i/databases/d"}, databases)
	require.Equal(t, "zh-CN", next.Language, "an unrelated update keeps the stored language")

	languageNext, _, err := applyQualityAgentUpdate(&v1pb.QualityAgent{Language: "en-US"},
		current, []string{"instances/i/databases/d"}, []string{"language"})
	require.NoError(t, err)
	require.Equal(t, "en-US", languageNext.Language)
	require.Equal(t, int64(5), next.ID, "the update keeps the row it writes")
	require.NotNil(t, next.OwnerPrincipalID)
	require.Equal(t, []string{"suggest", "patrol"}, next.Capabilities)
}

func TestConvertQualityAgentToV1(t *testing.T) {
	t.Parallel()

	owner := int32(9)
	agent := &store.QualityAgentMessage{
		ResourceID:                  "abc",
		PrincipalID:                 10,
		DisplayName:                 "Agent A",
		Language:                    "zh-CN",
		State:                       store.QualityAgentStateActive,
		OwnerPrincipalID:            &owner,
		Capabilities:                []string{"suggest"},
		AdoptionConfidenceThreshold: 0.7,
		AdoptionMaxPerDay:           10,
		ConversationMessagesPerHour: 60,
	}
	view := convertQualityAgentToV1("ws", agent, []string{"instances/i/databases/d"})
	require.Equal(t, "workspaces/ws/qualityAgents/abc", view.GetName())
	require.Equal(t, v1pb.QualityAgentState_QUALITY_AGENT_STATE_ACTIVE, view.GetState())
	require.Equal(t, "users/9", view.GetOwner())
	require.Equal(t, "zh-CN", view.GetLanguage())
	require.Equal(t, []string{"instances/i/databases/d"}, view.GetDatabases())
	require.Equal(t, []string{"suggest"}, view.GetCapabilities())
}

func int32Pointer(value int32) *int32 {
	return &value
}

// The merged row's state is the only lifecycle truth the API renders.
func TestQualityAgentStateToV1(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		state store.QualityAgentState
		want  v1pb.QualityAgentState
	}{
		{store.QualityAgentStateDraft, v1pb.QualityAgentState_QUALITY_AGENT_STATE_DRAFT},
		{store.QualityAgentStateActive, v1pb.QualityAgentState_QUALITY_AGENT_STATE_ACTIVE},
		{store.QualityAgentStatePaused, v1pb.QualityAgentState_QUALITY_AGENT_STATE_PAUSED},
		{store.QualityAgentStateDisabled, v1pb.QualityAgentState_QUALITY_AGENT_STATE_DISABLED},
		{store.QualityAgentState(0), v1pb.QualityAgentState_QUALITY_AGENT_STATE_UNSPECIFIED},
	} {
		require.Equal(t, test.want, qualityAgentStateToV1(test.state))
	}
}
