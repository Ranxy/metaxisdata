package qualityrunner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestFailureExecutionRecordsWhatItRead pins the audit trail of a failed check. The
// planned row carries the revision and target the due scan saw, which may be minutes
// old by the time the execution runs; once the runner has loaded the rule's snapshot,
// a failure must record what it actually read — the L2 explanation and the contract
// diff both read RuleRevision back — while a failure before the snapshot read can
// only name the planned values.
func TestFailureExecutionRecordsWhatItRead(t *testing.T) {
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
	executed := &dataquality.RuleSnapshot{
		Revision: 5,
		Target: dataquality.Target{
			GUID:     "instances/i;db;public;executed",
			MetaType: storepb.MetaType_VIEW,
			Path:     &storepb.QualityTargetPath{Instance: "i", Database: "db", Schema: "public", Object: "executed"},
		},
	}

	tests := []struct {
		name         string
		snapshot     *dataquality.RuleSnapshot
		status       storepb.QualityStatus
		wantRevision int32
		wantGUID     string
		wantType     storepb.MetaType
		wantObject   string
	}{
		{
			name:         "after the snapshot read it records what it read",
			snapshot:     executed,
			status:       storepb.QualityStatus_QUALITY_STATUS_ERROR,
			wantRevision: 5,
			wantGUID:     "instances/i;db;public;executed",
			wantType:     storepb.MetaType_VIEW,
			wantObject:   "executed",
		},
		{
			name:         "before the snapshot read it keeps the planned row",
			status:       storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
			wantRevision: 3,
			wantGUID:     "instances/i;db;public;planned",
			wantType:     storepb.MetaType_TABLE,
			wantObject:   "planned",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := failureExecutionMessage(planned, test.snapshot, time.Now().UTC(), test.status,
				dataquality.ErrCodeExecution, "boom")
			require.Equal(t, test.wantRevision, got.RuleRevision)
			require.Equal(t, test.wantGUID, got.TargetGUID)
			require.Equal(t, test.wantType, got.TargetType)
			require.Equal(t, test.wantObject, got.TargetPath.GetObject())
			require.Equal(t, test.status, got.Status)
		})
	}
}
