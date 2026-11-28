package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestScanQualityAgentRequest(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	origin := int64(7)
	ask := int64(9)
	values := []any{
		int64(1), "req", "ws", int64(3), sql.NullInt64{Int64: origin, Valid: true},
		"1;shop;;orders", int16(storepb.MetaType_TABLE), int16(QualityAgentRequestKindDecision),
		int16(QualityAgentRequestStatusForwarded), "title", "question", "case-1", nil,
		"target:1;shop;;orders:case:case-1:decision", sql.NullInt64{Int64: ask, Valid: true},
		"", sql.NullInt32{}, sql.NullTime{Time: now, Valid: true}, sql.NullTime{}, now, now,
	}
	message, err := scanQualityAgentRequest(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, QualityAgentRequestKindDecision, message.Kind)
	require.Equal(t, QualityAgentRequestStatusForwarded, message.Status)
	require.Equal(t, origin, *message.OriginSessionID)
	require.Equal(t, ask, *message.AskID)
	require.NotNil(t, message.ForwardedAt)
}

func TestScanQualityAgentAsk(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	conversation := int64(2)
	values := []any{
		int64(1), "ask", "ws", int64(3), sql.NullInt64{Int64: conversation, Valid: true},
		int16(QualityAgentAskKindQuestion), int64(12), "title", "question", "proposal:t",
		int16(QualityAgentAskStatusOpen), sql.NullInt32{}, sql.NullTime{}, int64(0),
		sql.NullTime{}, now, now,
	}
	message, err := scanQualityAgentAsk(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, QualityAgentAskKindQuestion, message.Kind)
	require.Equal(t, QualityAgentAskStatusOpen, message.Status)
	require.Equal(t, conversation, *message.ConversationSessionID)
	require.Equal(t, int64(12), message.MessageSeq)
	require.Equal(t, "proposal:t", message.DedupeKey)
}
