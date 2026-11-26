package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A ledger row is the record behind the write budget and the answer to "what did it change", so the
// scan has to keep the case, the check, the revision move and the planned run — the four things a
// reader joins on.
func TestScanQualityAgentCaseActionKeepsTheJoinKeys(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	values := []any{
		int64(31), sql.NullInt64{Int64: 9, Valid: true}, int64(4),
		"inst1;app;public;orders", int16(storepb.MetaType_TABLE),
		sql.NullInt64{Int64: 460, Valid: true}, "inst1;app",
		int16(QualityAgentCaseActionRevise), "range.min", int32(3), int32(4),
		sql.NullInt64{Int64: 4451, Valid: true}, int16(QualityAgentCaseActionApplied),
		"CONFIG_VALUE_INVALID", sql.NullInt32{}, createdAt,
	}
	action, err := scanQualityAgentCaseAction(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, int64(31), action.ID)
	require.NotNil(t, action.CaseID)
	require.Equal(t, int64(9), *action.CaseID)
	require.Equal(t, int64(4), action.AgentID)
	require.Equal(t, "inst1;app;public;orders", action.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, action.TargetType)
	require.NotNil(t, action.RuleID)
	require.Equal(t, int64(460), *action.RuleID)
	require.Equal(t, QualityAgentCaseActionRevise, action.Action)
	require.Equal(t, "range.min", action.Field)
	require.Equal(t, int32(3), action.FromRevision)
	require.Equal(t, int32(4), action.ToRevision)
	require.NotNil(t, action.ExecutionID)
	require.Equal(t, int64(4451), *action.ExecutionID)
	require.Equal(t, QualityAgentCaseActionApplied, action.Outcome)
	require.Equal(t, "CONFIG_VALUE_INVALID", action.Reason)
	require.Nil(t, action.RequestedBy)
	require.Equal(t, createdAt, action.CreatedAt)
}

// A run requested with no work item behind it, on a check that has since been hard-deleted, still
// has to read back: the ledger outlives both.
func TestScanQualityAgentCaseActionToleratesAMissingCaseAndCheck(t *testing.T) {
	t.Parallel()

	values := []any{
		int64(32), sql.NullInt64{}, int64(4), "", int16(0), sql.NullInt64{}, "",
		int16(QualityAgentCaseActionRun), "", int32(0), int32(0),
		sql.NullInt64{}, int16(QualityAgentCaseActionApplied), "", sql.NullInt32{}, time.Time{},
	}
	action, err := scanQualityAgentCaseAction(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Nil(t, action.CaseID)
	require.Nil(t, action.RuleID)
	require.Nil(t, action.ExecutionID)
	require.Equal(t, QualityAgentCaseActionRun, action.Action)
}
