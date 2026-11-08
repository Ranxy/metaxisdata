package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestScanQualityAgentEnrollment(t *testing.T) {
	t.Parallel()

	discovered := time.Unix(1000, 0).UTC()
	values := []any{
		int64(5), "enr-res", "ws", "i;d;public;t", int16(4), "instances/i/databases/d",
		int16(2), int16(2), int32(450), "rules,failing", "admission-limit",
		(*int64)(nil), int32(1), (*time.Time)(nil), (*time.Time)(nil),
		discovered, discovered, (*int32)(nil), (*time.Time)(nil), int32(3),
		discovered, discovered,
	}
	enrollment, err := scanQualityAgentEnrollment(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, int64(5), enrollment.ID)
	require.Equal(t, "enr-res", enrollment.ResourceID)
	require.Equal(t, "ws", enrollment.WorkspaceID)
	require.Equal(t, storepb.MetaType_TABLE, enrollment.TargetType)
	require.Equal(t, storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE, enrollment.State)
	require.Equal(t, storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE, enrollment.Mode)
	require.Equal(t, int32(450), enrollment.Priority)
	require.Equal(t, "admission-limit", enrollment.BlockedReason)
	require.Nil(t, enrollment.SessionID)
	require.Equal(t, int32(3), enrollment.Revision)
	require.Equal(t, discovered, enrollment.DiscoveredAt)
}

// A malformed discovery item is refused before any query runs, so a bug in the scanner
// cannot write an enrollment without a target.
func TestUpsertQualityAgentEnrollmentsValidatesInput(t *testing.T) {
	t.Parallel()

	stores := &Store{}
	_, err := stores.UpsertQualityAgentEnrollments(context.Background(), "ws", []*QualityAgentEnrollmentUpsert{
		{TargetGUID: "", DatabaseRef: "instances/i/databases/d"},
	})
	require.Error(t, err)

	_, err = stores.UpsertQualityAgentEnrollments(context.Background(), "ws", []*QualityAgentEnrollmentUpsert{
		{TargetGUID: "i;d;public;t", DatabaseRef: ""},
	})
	require.Error(t, err)
}

// Empty batches are no-ops: the runner calls these on every tick even when discovery
// found nothing.
func TestEmptyEnrollmentBatchesAreNoOps(t *testing.T) {
	t.Parallel()

	stores := &Store{}
	affected, err := stores.UpsertQualityAgentEnrollments(context.Background(), "ws", nil)
	require.NoError(t, err)
	require.Zero(t, affected)

	require.NoError(t, stores.UpdateQualityAgentEnrollmentPriority(context.Background(), nil))

	affected, err = stores.RequeueQualityAgentEnrollments(context.Background(), nil, "")
	require.NoError(t, err)
	require.Zero(t, affected)

	names, err := stores.ListQualityAgentSessionResourceIDs(context.Background(), "ws", nil)
	require.NoError(t, err)
	require.Empty(t, names)
}

// The admission read must only consider pending rows and must interleave databases, or
// one large database starves the rest of the queue.
func TestAdmissibleEnrollmentQueryPinsTheRoundRobinPredicate(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"state = $2",
		"PARTITION BY database_ref",
		"ORDER BY priority DESC, target_guid ASC",
		"db_rank ASC",
		"LIMIT $3",
	} {
		require.Contains(t, admissibleQualityAgentEnrollmentsQuery, fragment)
	}
	requirePlaceholdersMatchArgs(t, strings.ReplaceAll(admissibleQualityAgentEnrollmentsQuery, qualityAgentEnrollmentColumns, "id"), []any{"ws", int16(1), 20})
}

// The scope statements must leave a human's pause and a needs-human block alone, and
// their placeholders must match the arguments the reconciler passes.
func TestReconcileScopeQueryPinsSafetyPredicates(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"state <> $5",
		"(s.state <> $4 OR s.blocked_code IN ('', 'out-of-scope', 'agent-disabled'))",
	} {
		require.Contains(t, blockOutOfScopeSessionsQuery, fragment)
	}
	requirePlaceholdersMatchArgs(t, blockOutOfScopeSessionsQuery, []any{
		"ws", []string{"instances/i/databases/d"}, int16(5), int16(3), int16(2),
	})
	requirePlaceholdersMatchArgs(t, reconcileOutOfScopeEnrollmentsQuery, []any{
		"ws", []string{"instances/i/databases/d"}, int16(5), int16(4),
	})
	requirePlaceholdersMatchArgs(t, restoreInScopeEnrollmentsQuery, []any{
		"ws", []string{"instances/i/databases/d"}, int16(1), int16(5),
	})
}
