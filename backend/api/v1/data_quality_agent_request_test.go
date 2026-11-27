package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The unspecified status is the "needs me" list: open and forwarded requests, open and
// answered asks. A closed request or a resolved ask stays out unless the caller asks
// for it explicitly.
func TestCoordinationStatusDefaultsAreTheActiveSets(t *testing.T) {
	t.Parallel()

	requests, err := qualityAgentRequestStatuses(v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_UNSPECIFIED)
	require.NoError(t, err)
	require.ElementsMatch(t, []store.QualityAgentRequestStatus{
		store.QualityAgentRequestStatusOpen, store.QualityAgentRequestStatusForwarded,
	}, requests)

	asks, err := qualityAgentAskStatuses(v1pb.QualityAgentAskStatus_QUALITY_AGENT_ASK_STATUS_UNSPECIFIED)
	require.NoError(t, err)
	require.ElementsMatch(t, []store.QualityAgentAskStatus{
		store.QualityAgentAskStatusOpen, store.QualityAgentAskStatusAnswered,
	}, asks)
}

// An ask read carries its lead conversation and owner answer as resource names, and a
// request read carries its origin subagent and ask link.
func TestConvertCoordinationRowsNamesTheirRelations(t *testing.T) {
	t.Parallel()

	now := time.Unix(10_000, 0).UTC()
	conversation := int64(4)
	origin := int64(5)
	answeredBy := int32(7)
	askID := int64(9)
	ask := convertQualityAgentAskToV1("ws", "agent1", &store.QualityAgentAskMessage{
		ResourceID:            "a1",
		AgentID:               3,
		Kind:                  store.QualityAgentAskKindApproval,
		Status:                store.QualityAgentAskStatusAnswered,
		Title:                 "Drafts need a decision",
		Question:              "Approve?",
		ConversationSessionID: &conversation,
		MessageSeq:            11,
		AnswerMessageSeq:      12,
		AnsweredBy:            &answeredBy,
		AnsweredAt:            &now,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, map[int64]string{conversation: "lead4"}, []*store.QualityAgentRequestMessage{
		{TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE},
		{TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE},
		{TargetGUID: "inst;db;;users", TargetType: storepb.MetaType_TABLE},
	})
	require.Equal(t, "workspaces/ws/qualityAgentAsks/a1", ask.GetName())
	require.Equal(t, "workspaces/ws/qualityAgents/agent1", ask.GetAgent())
	require.Equal(t, "workspaces/ws/qualityAgentSessions/lead4", ask.GetConversation())
	require.Equal(t, "users/7", ask.GetAnsweredBy())
	require.Equal(t, now.Unix(), ask.GetAnswerTime().AsTime().Unix())
	require.Equal(t, int64(11), ask.GetMessageSeq())
	require.Equal(t, int64(12), ask.GetAnswerMessageSeq())
	require.EqualValues(t, 3, ask.GetRequestCount())
	require.Len(t, ask.GetTargets(), 2, "targets are deduplicated, requests are not")
	require.Equal(t, "orders", ask.GetTargets()[0].GetPath().GetObject())

	request := convertQualityAgentRequestToV1("ws", "agent1", &store.QualityAgentRequestMessage{
		ResourceID:      "r1",
		AgentID:         3,
		OriginSessionID: &origin,
		TargetGUID:      "inst;db;;orders",
		TargetType:      storepb.MetaType_TABLE,
		Kind:            store.QualityAgentRequestKindProposal,
		Status:          store.QualityAgentRequestStatusForwarded,
		Title:           "A change needs approval",
		Question:        "range.min = 1?",
		CaseResourceID:  "c1",
		AskID:           &askID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, map[int64]string{origin: "sub5"}, map[int64]string{askID: "a1"})
	require.Equal(t, "workspaces/ws/qualityAgentRequests/r1", request.GetName())
	require.Equal(t, "workspaces/ws/qualityAgentSessions/sub5", request.GetOriginSession())
	require.Equal(t, "workspaces/ws/qualityAgentAsks/a1", request.GetAsk())
	require.Equal(t, "workspaces/ws/qualityAgentCases/c1", request.GetCase())
	require.Equal(t, "orders", request.GetTarget().GetPath().GetObject())
	require.Equal(t, v1pb.QualityAgentRequestKind_QUALITY_AGENT_REQUEST_KIND_PROPOSAL, request.GetKind())
	require.Equal(t, v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_FORWARDED, request.GetStatus())
	require.Equal(t, timestamppb.New(now).AsTime(), request.GetCreateTime().AsTime())
}
