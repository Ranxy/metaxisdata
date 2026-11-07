package store

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The wake claim is the one predicate that decides "running": it must only take an
// archived-free session whose wake is due and whose lease is free, and it must not
// claim two workers for the same row.
func TestClaimDueQualityAgentSessionsQueryPinsTheWakePredicate(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"archived_at IS NULL",
		"next_wake_at IS NOT NULL",
		"next_wake_at <= $1",
		"lease_until IS NULL OR lease_until <= $1",
		"blocked_code = ANY($6)",
		"FOR UPDATE SKIP LOCKED",
	} {
		require.Contains(t, claimDueQualityAgentSessionsQuery, fragment)
	}
	requirePlaceholdersMatchArgs(t, claimDueQualityAgentSessionsQuery, []any{
		time.Now(), time.Now(), int16(1), 2, int16(3), []string{"token-budget"},
	})
}

// Only transient budgets may wake a blocked session on their own; a person-action
// blocker must stay parked until someone resumes it.
func TestRetryableSessionBlockedCodes(t *testing.T) {
	t.Parallel()

	codes := retryableSessionBlockedCodes()
	require.ElementsMatch(t, []string{"token-budget", "autonomy-off", "llm-quota", "no-work", "internal-error"}, codes)
	require.NotContains(t, codes, "needs-human")
	require.NotContains(t, codes, "restart-limit")
	require.NotContains(t, codes, "round-limit")
}

func TestCreateQualityAgentSessionRejectsIncompleteInput(t *testing.T) {
	t.Parallel()

	stores := &Store{}
	for _, input := range []*CreateQualityAgentSessionInput{
		nil,
		{WorkspaceID: "ws"},
		{TargetGUID: "target"},
	} {
		_, err := stores.CreateQualityAgentSession(context.Background(), input)
		require.Error(t, err)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
	}
}

func TestAppendQualityAgentTranscriptMessageRejectsMissingSession(t *testing.T) {
	t.Parallel()

	stores := &Store{}
	_, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), nil)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = stores.AppendQualityAgentTranscriptMessage(context.Background(), &QualityAgentTranscriptMessage{WorkspaceID: "ws"})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestUpdateQualityAgentSessionProgressRejectsNilResult(t *testing.T) {
	t.Parallel()

	stores := &Store{}
	_, err := stores.UpdateQualityAgentSessionProgress(context.Background(), "session", nil)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// fakeScanner fills the scan destinations from a positional value slice so the
// JSONB and enum decoding can be exercised without PostgreSQL.
type fakeScanner struct {
	values []any
}

func (f *fakeScanner) Scan(dest ...any) error {
	if len(dest) != len(f.values) {
		return fmt.Errorf("fakeScanner: %d destinations but %d values", len(dest), len(f.values))
	}
	for i, target := range dest {
		destination := reflect.ValueOf(target)
		if destination.Kind() != reflect.Pointer {
			return fmt.Errorf("fakeScanner: destination %T is not a pointer", target)
		}
		value := reflect.ValueOf(f.values[i])
		if !value.IsValid() {
			continue
		}
		if !value.Type().AssignableTo(destination.Elem().Type()) {
			return fmt.Errorf("fakeScanner: value %d has type %s, destination wants %s",
				i, value.Type(), destination.Elem().Type())
		}
		destination.Elem().Set(value)
	}
	return nil
}

func sessionScanValues(engineState []byte) []any {
	return []any{
		int64(7), "res", "ws", "target-guid", int16(1), "instances/i/databases/d", int64(3),
		int32(42), "objective", int16(1), "", "", int32(4), int32(9), int64(1234),
		int32(5000), "summary text", int64(11), int64(12), engineState,
		(*time.Time)(nil), (*time.Time)(nil), int32(0), (*int32)(nil),
		time.Unix(1000, 0).UTC(), time.Unix(2000, 0).UTC(), (*time.Time)(nil),
	}
}

func TestScanQualityAgentSessionDecodesStateAndEngineState(t *testing.T) {
	t.Parallel()

	message, err := scanQualityAgentSession(&fakeScanner{values: sessionScanValues([]byte(`{"done":["gap-a"],"blockedRounds":2,"blockedCode":"needs-data"}`))})
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED, message.State)
	require.Equal(t, storepb.MetaType(1), message.TargetType)
	require.Equal(t, int32(42), message.AgentPrincipalID)
	require.ElementsMatch(t, []string{"gap-a"}, message.EngineState.GetDone())
	require.Equal(t, int32(2), message.EngineState.GetBlockedRounds())
	require.Equal(t, "needs-data", message.EngineState.GetBlockedCode())
	require.Nil(t, message.NextWakeAt)
}

func TestScanQualityAgentSessionReadIncludesLastActivity(t *testing.T) {
	t.Parallel()

	lastActivity := time.Unix(3000, 0).UTC()
	values := append(sessionScanValues([]byte("{}")), &lastActivity)
	message, err := scanQualityAgentSessionRead(&fakeScanner{values: values})
	require.NoError(t, err)
	require.NotNil(t, message.LastActivityAt)
	require.Equal(t, lastActivity, *message.LastActivityAt)
	require.Empty(t, message.EngineState.GetDone())
}

func messageScanValues(toolCalls []byte) []any {
	return []any{
		int64(9), int64(7), "ws", int64(15), int32(2), int32(1),
		int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT),
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL),
		"the answer", toolCalls, "call-1", "get_target_summary", int32(120), int32(100),
		int32(20), "", false, "wake:2:turn:1", time.Unix(4000, 0).UTC(),
	}
}

func TestScanQualityAgentTranscriptDecodesToolCalls(t *testing.T) {
	t.Parallel()

	message, err := scanQualityAgentTranscript(&fakeScanner{values: messageScanValues([]byte(`{"toolCalls":[{"id":"call-1","name":"get_target_summary","arguments":"{}"}]}`))})
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT, message.Role)
	require.Equal(t, storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL, message.Origin)
	require.Len(t, message.ToolCalls.GetToolCalls(), 1)
	require.Equal(t, "get_target_summary", message.ToolCalls.GetToolCalls()[0].GetName())
	require.Equal(t, int32(120), message.Tokens)
}

func TestScanQualityAgentTranscriptAcceptsEmptyJSONB(t *testing.T) {
	t.Parallel()

	message, err := scanQualityAgentTranscript(&fakeScanner{values: messageScanValues([]byte(`{}`))})
	require.NoError(t, err)
	require.Empty(t, message.ToolCalls.GetToolCalls())
}
