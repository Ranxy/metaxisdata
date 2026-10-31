package qualityrunner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestFailedExecutionRecordsTheExecutedRevision pins the audit trail of a failed
// check. The planned row carries the revision and target the due scan saw, which
// may be minutes old by the time the execution runs; once the runner has loaded
// the rule's snapshot, a failure must record what it actually read — the L2
// explanation and the contract diff both read RuleRevision back.
func TestFailedExecutionRecordsTheExecutedRevision(t *testing.T) {
	t.Parallel()

	planned := &store.QualityExecutionMessage{
		ID:           7,
		RunID:        8,
		RuleID:       9,
		RuleRevision: 3,
		TargetGUID:   "instances/i;db;public;planned",
		TargetType:   storepb.MetaType_TABLE,
		TargetPath:   &storepb.QualityTargetPath{Instance: "i", Database: "db", Schema: "public", Object: "planned"},
	}
	snapshot := &dataquality.RuleSnapshot{
		Revision: 5,
		Target: dataquality.Target{
			GUID:     "instances/i;db;public;executed",
			MetaType: storepb.MetaType_VIEW,
			Path:     &storepb.QualityTargetPath{Instance: "i", Database: "db", Schema: "public", Object: "executed"},
		},
	}

	got := failureExecutionMessage(planned, snapshot, time.Now().UTC(),
		storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeExecution, "boom")

	require.Equal(t, int32(5), got.RuleRevision)
	require.Equal(t, "instances/i;db;public;executed", got.TargetGUID)
	require.Equal(t, storepb.MetaType_VIEW, got.TargetType)
	require.Equal(t, "executed", got.TargetPath.GetObject())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, got.Status)
}

// TestFailedExecutionWithoutSnapshotKeepsThePlan covers the paths that fail before
// a snapshot is read: the planned revision is all the row can name.
func TestFailedExecutionWithoutSnapshotKeepsThePlan(t *testing.T) {
	t.Parallel()

	planned := &store.QualityExecutionMessage{ID: 1, RuleRevision: 3, TargetGUID: "instances/i;db;public;planned"}

	got := failureExecutionMessage(planned, nil, time.Now().UTC(),
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED, "", "retired")

	require.Equal(t, int32(3), got.RuleRevision)
	require.Equal(t, "instances/i;db;public;planned", got.TargetGUID)
}
