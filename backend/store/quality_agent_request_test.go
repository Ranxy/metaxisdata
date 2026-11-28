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
		"target:1;shop;;orders:case:case-1:decision", "timeout", "fingerprint",
		sql.NullInt64{Int64: ask, Valid: true},
		"", sql.NullInt32{}, sql.NullTime{Time: now, Valid: true}, sql.NullTime{}, now, now,
	}
	message, err := scanQualityAgentRequest(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, QualityAgentRequestKindDecision, message.Kind)
	require.Equal(t, QualityAgentRequestStatusForwarded, message.Status)
	require.Equal(t, origin, *message.OriginSessionID)
	require.Equal(t, ask, *message.AskID)
	require.Equal(t, "timeout", message.QuestionKey)
	require.Equal(t, "fingerprint", message.QuestionFingerprint)
	require.NotNil(t, message.ForwardedAt)
}

func TestScanQualityAgentAsk(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	conversation := int64(2)
	values := []any{
		int64(1), "ask", "ws", int64(3), sql.NullInt64{Int64: conversation, Valid: true},
		int16(QualityAgentAskKindQuestion), int64(12), "title", "question",
		"inst;db;;t", int16(storepb.MetaType_TABLE), "proposal:t",
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
	require.Equal(t, "inst;db;;t", message.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, message.TargetType)
}

func TestQuestionFingerprintNormalizesWhitespaceAndScope(t *testing.T) {
	t.Parallel()

	base := &QualityAgentRequestMessage{
		TargetGUID: "inst;db;;orders", CaseResourceID: "case-1", Question: "Raise  the timeout?",
	}
	first := qualityAgentQuestionFingerprint(base)
	require.NotEmpty(t, first)
	spaced := *base
	spaced.Question = " Raise the timeout? "
	require.Equal(t, first, qualityAgentQuestionFingerprint(&spaced), "whitespace is not a different question")
	otherCase := *base
	otherCase.CaseResourceID = "case-2"
	require.NotEqual(t, first, qualityAgentQuestionFingerprint(&otherCase))
	otherTarget := *base
	otherTarget.TargetGUID = "inst;db;;users"
	require.NotEqual(t, first, qualityAgentQuestionFingerprint(&otherTarget))
	otherKind := *base
	otherKind.Kind = QualityAgentRequestKindQuestion
	base.Kind = QualityAgentRequestKindDecision
	require.NotEqual(t, qualityAgentQuestionFingerprint(base), qualityAgentQuestionFingerprint(&otherKind),
		"a question must not share a fingerprint with a decision")
}

func TestQuestionConflictIgnoresWhitespaceButNotADifferentQuestion(t *testing.T) {
	t.Parallel()

	existing := &QualityAgentRequestMessage{Question: "Raise the timeout?"}
	message := &QualityAgentRequestMessage{
		Question:                    " Raise\nthe timeout? ",
		ConflictOnDifferentQuestion: true,
	}
	require.False(t, qualityAgentQuestionConflict(existing, message),
		"a whitespace-only retry is the same question")
	message.Question = "Pause the check?"
	require.True(t, qualityAgentQuestionConflict(existing, message))
	message.ConflictOnDifferentQuestion = false
	require.False(t, qualityAgentQuestionConflict(existing, message),
		"only questions opt into the conflict")
}
