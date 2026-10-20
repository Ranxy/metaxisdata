package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type qualityRunScannerStub struct {
	id                 int64
	resourceID         string
	instanceResourceID string
	trigger            storepb.QualityRunTrigger
	status             storepb.QualityRunStatus
	outcome            sql.NullInt32
	counts             []byte
	initiatorID        sql.NullInt32
	startedAt          time.Time
	finishedAt         sql.NullTime
}

func (s qualityRunScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*string)) = s.instanceResourceID
	*(dest[3].(*storepb.QualityRunTrigger)) = s.trigger
	*(dest[4].(*storepb.QualityRunStatus)) = s.status
	*(dest[5].(*sql.NullInt32)) = s.outcome
	*(dest[6].(*[]byte)) = s.counts
	*(dest[7].(*sql.NullInt32)) = s.initiatorID
	*(dest[8].(*time.Time)) = s.startedAt
	*(dest[9].(*sql.NullTime)) = s.finishedAt
	return nil
}

func TestScanQualityRunPendingHasNoOutcomeYet(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	run, err := scanQualityRun(qualityRunScannerStub{
		id:                 11,
		resourceID:         "runs/abc",
		instanceResourceID: "instances/inst1",
		trigger:            storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL,
		status:             storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
		outcome:            sql.NullInt32{},
		counts:             []byte(`{"total":4,"pass":2,"fail":1,"error":1}`),
		initiatorID:        sql.NullInt32{},
		startedAt:          startedAt,
		finishedAt:         sql.NullTime{},
	})
	require.NoError(t, err)

	require.Equal(t, int64(11), run.ID)
	require.Equal(t, "runs/abc", run.ResourceID)
	require.Equal(t, "instances/inst1", run.InstanceResourceID)
	require.Equal(t, storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL, run.Trigger)
	require.Equal(t, storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING, run.Status)
	// A NULL outcome column must stay UNSPECIFIED in memory, not PASS (the zero
	// value of the quality status enum is UNSPECIFIED, and PASS is 1).
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, run.Outcome)
	require.Equal(t, int32(4), run.Counts.GetTotal())
	require.Equal(t, int32(2), run.Counts.GetPass())
	require.Equal(t, int32(1), run.Counts.GetFail())
	require.Equal(t, int32(1), run.Counts.GetError())
	require.Nil(t, run.InitiatorID)
	require.Equal(t, startedAt, run.StartedAt)
	require.Nil(t, run.FinishedAt)
}

func TestScanQualityRunFinishedCarriesOutcomeAndFinisher(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(2 * time.Minute)
	run, err := scanQualityRun(qualityRunScannerStub{
		id:         12,
		resourceID: "runs/def",
		trigger:    storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_SCHEDULED,
		status:     storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED,
		outcome:    sql.NullInt32{Int32: int32(storepb.QualityStatus_QUALITY_STATUS_FAIL), Valid: true},
		counts:     []byte(`{}`),
		initiatorID: sql.NullInt32{
			Int32: 7,
			Valid: true,
		},
		startedAt:  startedAt,
		finishedAt: sql.NullTime{Time: finishedAt, Valid: true},
	})
	require.NoError(t, err)

	require.Equal(t, storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED, run.Status)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, run.Outcome)
	require.NotNil(t, run.InitiatorID)
	require.Equal(t, int32(7), *run.InitiatorID)
	require.NotNil(t, run.FinishedAt)
	require.Equal(t, finishedAt, *run.FinishedAt)
	// The empty JSONB default still produces a usable, non-nil message.
	require.NotNil(t, run.Counts)
}

func TestScanQualityRunSurfacesBrokenCountsJSON(t *testing.T) {
	t.Parallel()

	_, err := scanQualityRun(qualityRunScannerStub{counts: []byte(`{"total":"many"}`)})
	require.Error(t, err)
}

func TestBuildQualityRunWhereEmptyFilterListsEverything(t *testing.T) {
	t.Parallel()

	where, args := buildQualityRunWhere(nil)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)
}

func TestBuildQualityRunWhereAllFilters(t *testing.T) {
	t.Parallel()

	startAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	endAt := startAt.Add(24 * time.Hour)
	where, args := buildQualityRunWhere(&FindQualityRunMessage{
		ResourceID: ptr("runs/abc"),
		Statuses: []storepb.QualityRunStatus{
			storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
			storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING,
		},
		// UNSPECIFIED asks for runs whose outcome column is still NULL.
		Outcome:            ptr(storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED),
		Trigger:            ptr(storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_SCHEDULED),
		InstanceResourceID: ptr("instances/inst1"),
		InitiatorID:        ptr(int32(7)),
		StartAt:            &startAt,
		EndAt:              &endAt,
	})

	require.Equal(t, "resource_id = $1 AND status = ANY($2::int[]) AND outcome IS NULL AND "+
		"trigger = $3::int AND instance_resource_id = $4 AND initiator_id = $5 AND "+
		"started_at >= $6 AND started_at < $7", where)
	require.Equal(t, []any{
		"runs/abc",
		pq.Array([]int32{
			int32(storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING),
			int32(storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING),
		}),
		int32(storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_SCHEDULED),
		"instances/inst1",
		int32(7),
		startAt,
		endAt,
	}, args)
}

func TestBuildQualityRunWhereOutcomeFilter(t *testing.T) {
	t.Parallel()

	where, args := buildQualityRunWhere(&FindQualityRunMessage{
		Outcome: ptr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
	})
	require.Equal(t, "outcome = $1::int", where)
	require.Equal(t, []any{int32(storepb.QualityStatus_QUALITY_STATUS_FAIL)}, args)
}

func TestCreateQualityRunValidatesInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := &Store{}

	_, err := s.CreateQualityRun(ctx, nil)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.CreateQualityRun(ctx, &QualityRunMessage{Trigger: storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL})
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.CreateQualityRun(ctx, &QualityRunMessage{ResourceID: "runs/abc"})
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.CreateQualityRun(ctx, &QualityRunMessage{
		ResourceID: "runs/abc",
		Trigger:    storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL,
		Status:     storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED,
	})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestQualityRunMutatorsValidateInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := &Store{}
	at := time.Now().UTC()

	err := s.FinishQualityRun(ctx, 1, storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING, storepb.QualityStatus_QUALITY_STATUS_PASS, nil, at)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = s.FinishQualityRun(ctx, 1, storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, nil, at)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = s.MarkQualityRunRunning(ctx, 0, at)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.AbandonStaleQualityRuns(ctx, time.Time{})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// qualityRunByIDsStatementShape pins the reverse lookup the API uses to turn a
// result row's numeric run_id back into the run's resource id.
func TestQualityRunByIDsStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityRunByIDsStatement, "FROM quality_run")
	require.Contains(t, qualityRunByIDsStatement, "id = ANY($1::bigint[])")
	require.Contains(t, qualityRunByIDsStatement, "ORDER BY id")
}

func TestListQualityRunsByIDsEmptyInputIsEmptySlice(t *testing.T) {
	t.Parallel()

	// A zero Store has no pool: reaching the database here would panic, so this
	// also pins that an empty input is a no-op.
	s := &Store{}
	runs, err := s.ListQualityRunsByIDs(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, runs)
	require.Empty(t, runs)

	runs, err = s.ListQualityRunsByIDs(context.Background(), []int64{})
	require.NoError(t, err)
	require.NotNil(t, runs)
	require.Empty(t, runs)
}

// TestCountQualityRunsSinceRejectsTheZeroTime pins the one input the tally cannot
// mean: without a lower bound it would count every run the deployment ever made.
func TestCountQualityRunsSinceRejectsTheZeroTime(t *testing.T) {
	t.Parallel()

	s := &Store{}
	count, err := s.CountQualityRunsSince(context.Background(), time.Time{})
	require.Equal(t, 0, count)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

type qualityRunTargetScannerStub struct {
	runID    int64
	metaGUID string
	metaType storepb.MetaType
	pathJSON []byte
}

func (s qualityRunTargetScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.runID
	*(dest[1].(*string)) = s.metaGUID
	*(dest[2].(*storepb.MetaType)) = s.metaType
	*(dest[3].(*[]byte)) = s.pathJSON
	return nil
}

func TestScanQualityRunTargetReadsTheRevisionPathSnapshot(t *testing.T) {
	t.Parallel()

	target, err := scanQualityRunTarget(qualityRunTargetScannerStub{
		runID:    11,
		metaGUID: "instance_1;shop;public;orders",
		metaType: storepb.MetaType_TABLE,
		pathJSON: []byte(`{"instance":"prod","database":"shop","schema":"public","object":"orders","column":"id"}`),
	})
	require.NoError(t, err)

	require.Equal(t, int64(11), target.RunID)
	require.Equal(t, "instance_1;shop;public;orders", target.MetaGUID)
	require.Equal(t, storepb.MetaType_TABLE, target.MetaType)
	require.Equal(t, "prod", target.Path.GetInstance())
	require.Equal(t, "shop", target.Path.GetDatabase())
	require.Equal(t, "public", target.Path.GetSchema())
	require.Equal(t, "orders", target.Path.GetObject())
	require.Equal(t, "id", target.Path.GetColumn())
}

// TestScanQualityRunTargetToleratesTheEmptyDefault pins the JSONB default: a row
// whose path was never written must still produce a usable, non-nil path.
func TestScanQualityRunTargetToleratesTheEmptyDefault(t *testing.T) {
	t.Parallel()

	target, err := scanQualityRunTarget(qualityRunTargetScannerStub{pathJSON: []byte(`{}`)})
	require.NoError(t, err)
	require.NotNil(t, target.Path)
	require.Empty(t, target.Path.GetObject())
}

func TestScanQualityRunTargetSurfacesBrokenPathJSON(t *testing.T) {
	t.Parallel()

	_, err := scanQualityRunTarget(qualityRunTargetScannerStub{pathJSON: []byte(`{"object":7}`)})
	require.Error(t, err)
}

// qualityRunTargetsStatementShape pins how a run's targets are found: the result
// names a revision, and only the revision row carries what the rule watched.
func TestQualityRunTargetsStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityRunTargetsStatement, "FROM quality_rule_result r")
	require.Contains(t, qualityRunTargetsStatement, "JOIN quality_rule_revision rev ON rev.rule_id = r.rule_id AND rev.revision = r.rule_revision")
	require.Contains(t, qualityRunTargetsStatement, "DISTINCT ON (r.run_id, r.target_guid, r.target_type)")
	require.Contains(t, qualityRunTargetsStatement, "r.run_id = ANY($1::bigint[])")
}

func TestListQualityRunTargetsEmptyInputIsEmptyMap(t *testing.T) {
	t.Parallel()

	// A zero Store has no pool: reaching the database here would panic, so this
	// also pins that an empty input is a no-op.
	s := &Store{}
	targets, err := s.ListQualityRunTargets(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, targets)
	require.Empty(t, targets)

	targets, err = s.ListQualityRunTargets(context.Background(), []int64{})
	require.NoError(t, err)
	require.NotNil(t, targets)
	require.Empty(t, targets)
}
