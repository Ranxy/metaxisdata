package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// "Running" is derived from the lease, not from the durable state: a live lease wins
// over an enabled state and a stale one does not claim to be running.
func TestConvertQualityAgentSessionDerivesRunningFromTheLease(t *testing.T) {
	t.Parallel()

	now := time.Unix(10_000, 0).UTC()
	lease := now.Add(time.Minute)
	message := convertQualityAgentSessionToV1("ws", &store.QualityAgentSessionMessage{
		ResourceID:       "s1",
		TargetGUID:       "target",
		State:            storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
		AgentPrincipalID: 7,
		LeaseUntil:       &lease,
		NextWakeAt:       &lease,
		CreatedAt:        now.Add(-time.Hour),
		UpdatedAt:        now,
	}, now)
	require.True(t, message.GetRunning())
	require.Equal(t, "workspaces/ws/qualityAgentSessions/s1", message.GetName())
	require.Equal(t, "users/7", message.GetAgent())
	require.Equal(t, int64(60), message.GetLeaseExpireTime().AsTime().Unix()-now.Unix())

	stale := now.Add(-time.Minute)
	message = convertQualityAgentSessionToV1("ws", &store.QualityAgentSessionMessage{
		ResourceID: "s1",
		State:      storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_PAUSED,
		LeaseUntil: &stale,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, now)
	require.False(t, message.GetRunning())
}

// A session renders the target's display path, not the escaped GUID: the session
// table stores the target identity only, so the path is rebuilt from the GUID whose
// segments are the target's instance, database, schema, object and column.
func TestConvertQualityAgentSessionRebuildsTheTargetPath(t *testing.T) {
	t.Parallel()

	now := time.Unix(10_000, 0).UTC()
	table := convertQualityAgentSessionToV1("ws", &store.QualityAgentSessionMessage{
		ResourceID: "s1",
		TargetGUID: common.BuildMetaGUID("inst", "sales", "public", "orders"),
		TargetType: storepb.MetaType_TABLE,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, now)
	require.Equal(t, "inst", table.GetTarget().GetPath().GetInstance())
	require.Equal(t, "sales", table.GetTarget().GetPath().GetDatabase())
	require.Equal(t, "public", table.GetTarget().GetPath().GetSchema())
	require.Equal(t, "orders", table.GetTarget().GetPath().GetObject())

	// A name containing the GUID separator comes back whole.
	escaped := convertQualityAgentSessionToV1("ws", &store.QualityAgentSessionMessage{
		ResourceID: "s1",
		TargetGUID: common.BuildMetaGUID("inst", "sales", "public", "we;ird"),
		TargetType: storepb.MetaType_TABLE,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, now)
	require.Equal(t, "we;ird", escaped.GetTarget().GetPath().GetObject())

	column := convertQualityAgentSessionToV1("ws", &store.QualityAgentSessionMessage{
		ResourceID: "s2",
		TargetGUID: common.BuildMetaGUID("inst", "sales", "public", "orders", "email"),
		TargetType: storepb.MetaType_COLUMN,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, now)
	require.Equal(t, "email", column.GetTarget().GetPath().GetColumn())

	// A database-level target has no object segment; the path stops at the database.
	database := convertQualityAgentSessionToV1("ws", &store.QualityAgentSessionMessage{
		ResourceID: "s3",
		TargetGUID: common.BuildMetaGUID("inst", "sales"),
		TargetType: storepb.MetaType_DATABASE,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, now)
	require.Equal(t, "sales", database.GetTarget().GetPath().GetDatabase())
	require.Empty(t, database.GetTarget().GetPath().GetObject())
}

// A transcript entry renders its tool calls and keeps the tool call identity the model
// will see again on the next wake.
func TestConvertQualityAgentMessageRendersTheToolCalls(t *testing.T) {
	t.Parallel()

	created := time.Unix(10_000, 0).UTC()
	message := convertQualityAgentMessageToV1("ws", "s1", &store.QualityAgentTranscriptMessage{
		Seq:       7,
		Wake:      2,
		Turn:      1,
		Role:      storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		Origin:    storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL,
		Content:   "looking",
		Reference: "qualityExecutions/123",
		ToolCalls: &storepb.QualityToolCallList{ToolCalls: []*storepb.QualityToolCall{
			{Id: "call_1", Name: "get_target_summary", Arguments: "{}"},
		}},
		CreatedAt: created,
	})
	require.Equal(t, "workspaces/ws/qualityAgentSessions/s1/messages/7", message.GetName())
	require.Len(t, message.GetToolCalls(), 1)
	require.Equal(t, "get_target_summary", message.GetToolCalls()[0].GetName())
	require.Equal(t, "qualityExecutions/123", message.GetReference())
	require.Equal(t, int64(7), message.GetSeq())
}

func TestSlicesReverseKeepsChronologicalOrder(t *testing.T) {
	t.Parallel()

	first := &store.QualityAgentTranscriptMessage{Seq: 1}
	second := &store.QualityAgentTranscriptMessage{Seq: 2}
	third := &store.QualityAgentTranscriptMessage{Seq: 3}
	messages := []*store.QualityAgentTranscriptMessage{third, second, first}
	slicesReverse(messages)
	require.Equal(t, []int64{1, 2, 3}, []int64{messages[0].Seq, messages[1].Seq, messages[2].Seq})
}

func TestDatabaseRefOfTargetReadsTheGUIDPrefix(t *testing.T) {
	t.Parallel()

	reference, err := databaseRefOfTarget("i1;d1;s1;t1")
	require.NoError(t, err)
	require.Equal(t, "instances/i1/databases/d1", reference)

	_, err = databaseRefOfTarget("t1")
	require.Error(t, err)
}
