package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualitySuggestionJobScannerStub stands in for one quality_suggestion_job row.
type qualitySuggestionJobScannerStub struct {
	id              int64
	resourceID      string
	status          int16
	intent          string
	language        string
	provider        string
	model           string
	promptVersion   string
	targets         []byte
	cancelRequested bool
	createdBy       sql.NullInt32
	createdAt       time.Time
	startedAt       sql.NullTime
	finishedAt      sql.NullTime
	errText         string
}

func (s qualitySuggestionJobScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*int16)) = s.status
	*(dest[3].(*string)) = s.intent
	*(dest[4].(*string)) = s.language
	*(dest[5].(*string)) = s.provider
	*(dest[6].(*string)) = s.model
	*(dest[7].(*string)) = s.promptVersion
	*(dest[8].(*[]byte)) = s.targets
	*(dest[9].(*bool)) = s.cancelRequested
	*(dest[10].(*sql.NullInt32)) = s.createdBy
	*(dest[11].(*time.Time)) = s.createdAt
	*(dest[12].(*sql.NullTime)) = s.startedAt
	*(dest[13].(*sql.NullTime)) = s.finishedAt
	*(dest[14].(*string)) = s.errText
	return nil
}

// jobTestTargets builds a one-target snapshot in the state a job is created with.
func jobTestTargets(t *testing.T) *storepb.QualitySuggestionJobTargets {
	t.Helper()
	return &storepb.QualitySuggestionJobTargets{Targets: []*storepb.QualitySuggestionJobTarget{{
		TargetGuid: "instances/inst-1;shop;public;orders",
		TargetType: int32(storepb.MetaType_TABLE),
		Path: &storepb.QualityTargetPath{
			Instance: "inst-1", Database: "shop", Schema: "public", Object: "orders",
		},
		Columns: []string{"email"},
		State:   storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
	}}}
}

func TestScanQualitySuggestionJobRoundTripsTheTargetsAndLifetime(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 20, 9, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	finishedAt := startedAt.Add(2 * time.Minute)
	targets := jobTestTargets(t)
	targets.Targets[0].SuggestionResourceIds = []string{"suggestion1"}
	targetsJSON, err := protojson.Marshal(targets)
	require.NoError(t, err)

	job, err := scanQualitySuggestionJob(qualitySuggestionJobScannerStub{
		id:              31,
		resourceID:      "job1",
		status:          int16(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED),
		intent:          "email must not be empty",
		language:        "en",
		provider:        "profiles/openai",
		model:           "gpt-4",
		promptVersion:   "dq.rule_draft.v1",
		targets:         targetsJSON,
		cancelRequested: false,
		createdBy:       sql.NullInt32{Int32: 5, Valid: true},
		createdAt:       createdAt,
		startedAt:       sql.NullTime{Time: startedAt, Valid: true},
		finishedAt:      sql.NullTime{Time: finishedAt, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, int64(31), job.ID)
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED, job.Status)
	require.Equal(t, "email must not be empty", job.Intent)
	require.Equal(t, "en", job.Language)
	require.Equal(t, "profiles/openai", job.Provider)
	require.Equal(t, "gpt-4", job.Model)
	require.Equal(t, "dq.rule_draft.v1", job.PromptVersion)
	require.NotNil(t, job.Targets)
	require.Len(t, job.Targets.GetTargets(), 1)
	require.Equal(t, "instances/inst-1;shop;public;orders", job.Targets.GetTargets()[0].GetTargetGuid())
	require.Equal(t, []string{"suggestion1"}, job.Targets.GetTargets()[0].GetSuggestionResourceIds())
	require.NotNil(t, job.CreatedBy)
	require.Equal(t, int32(5), *job.CreatedBy)
	require.Equal(t, startedAt, *job.StartedAt)
	require.Equal(t, finishedAt, *job.FinishedAt)
	require.False(t, job.CancelRequested)
}

func TestScanQualitySuggestionJobLeavesPendingLifetimeNil(t *testing.T) {
	t.Parallel()

	job, err := scanQualitySuggestionJob(qualitySuggestionJobScannerStub{
		resourceID: "job1",
		status:     int16(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING),
		targets:    []byte("{}"),
	})
	require.NoError(t, err)
	require.Nil(t, job.CreatedBy)
	require.Nil(t, job.StartedAt)
	require.Nil(t, job.FinishedAt)
	require.NotNil(t, job.Targets)
}

func TestValidateQualitySuggestionJob(t *testing.T) {
	t.Parallel()

	base := &QualitySuggestionJobMessage{
		ResourceID: "job1",
		Status:     storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
		Intent:     "email must not be empty",
		Targets:    jobTestTargets(t),
	}
	require.NoError(t, validateQualitySuggestionJob(base))
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestionJob(nil)))

	for name, mutate := range map[string]func(*QualitySuggestionJobMessage){
		"no resource": func(job *QualitySuggestionJobMessage) { job.ResourceID = " " },
		"no status": func(job *QualitySuggestionJobMessage) {
			job.Status = storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED
		},
		"unknown status": func(job *QualitySuggestionJobMessage) {
			job.Status = storepb.QualitySuggestionJobStatus(99)
		},
		"no intent":      func(job *QualitySuggestionJobMessage) { job.Intent = " " },
		"no targets":     func(job *QualitySuggestionJobMessage) { job.Targets = nil },
		"empty targets":  func(job *QualitySuggestionJobMessage) { job.Targets = &storepb.QualitySuggestionJobTargets{} },
		"target no guid": func(job *QualitySuggestionJobMessage) { job.Targets.Targets[0].TargetGuid = "" },
		"target no type": func(job *QualitySuggestionJobMessage) { job.Targets.Targets[0].TargetType = 0 },
		"target bad state": func(job *QualitySuggestionJobMessage) {
			job.Targets.Targets[0].State = storepb.QualitySuggestionJobTargetState(99)
		},
		"target unspecified": func(job *QualitySuggestionJobMessage) {
			job.Targets.Targets[0].State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Shallow copies share the targets pointer, so each case rebuilds it.
			job := *base
			job.Targets = jobTestTargets(t)
			mutate(&job)
			err := validateQualitySuggestionJob(&job)
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
		})
	}

	// The target count is bounded.
	many := make([]*storepb.QualitySuggestionJobTarget, 0, qualitySuggestionJobTargetLimit+1)
	for range qualitySuggestionJobTargetLimit + 1 {
		many = append(many, &storepb.QualitySuggestionJobTarget{
			TargetGuid: "g",
			TargetType: int32(storepb.MetaType_TABLE),
			State:      storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
		})
	}
	tooMany := *base
	tooMany.Targets = &storepb.QualitySuggestionJobTargets{Targets: many}
	err := validateQualitySuggestionJob(&tooMany)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestQualitySuggestionJobQueryBindsFilters(t *testing.T) {
	t.Parallel()

	query, args := qualitySuggestionJobQuery(nil)
	require.Contains(t, query, "FROM quality_suggestion_job")
	require.Contains(t, query, "ORDER BY created_at DESC, id DESC")
	require.Equal(t, []any{}, args)

	resourceID := "job1"
	limit, offset := 20, 40
	query, args = qualitySuggestionJobQuery(&FindQualitySuggestionJobMessage{
		ResourceID: &resourceID,
		Statuses: []storepb.QualitySuggestionJobStatus{
			storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
			storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
		},
		Limit:  &limit,
		Offset: &offset,
	})
	require.Contains(t, query, "resource_id = $1")
	require.Contains(t, query, "status = ANY($2::int[])")
	require.Contains(t, query, "LIMIT $3")
	require.Contains(t, query, "OFFSET $4")
	requirePlaceholdersMatchArgs(t, query, args)
	require.Len(t, args, 4)
	require.Equal(t, resourceID, args[0])
	require.Equal(t, pq.Array([]int32{
		int32(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING),
		int32(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING),
	}), args[1])
	require.Equal(t, 20, args[2])
	require.Equal(t, 40, args[3])
	for _, value := range []string{resourceID, "job1"} {
		require.NotContains(t, query, value)
	}
}

// The statements that move a job between states are the state machine: the guards
// are in the SQL, so this pins them against a future rewrite.
func TestQualitySuggestionJobStatementsGuardTheLifecycle(t *testing.T) {
	t.Parallel()

	require.Contains(t, claimQualitySuggestionJobQuery, "status = 1")
	require.Contains(t, claimQualitySuggestionJobQuery, "FOR UPDATE SKIP LOCKED")
	require.Contains(t, claimQualitySuggestionJobQuery, "LIMIT 1")
	requirePlaceholdersMatchArgs(t, claimQualitySuggestionJobQuery, nil)

	require.Contains(t, markQualitySuggestionJobRunningQuery, "status = 2")
	requirePlaceholdersMatchArgs(t, markQualitySuggestionJobRunningQuery, []any{int64(1), time.Now()})

	require.Contains(t, updateQualitySuggestionJobProgressQuery, "status = 2")
	requirePlaceholdersMatchArgs(t, updateQualitySuggestionJobProgressQuery,
		[]any{int64(1), []byte("{}"), "p", "m", "v"})

	require.Contains(t, finishQualitySuggestionJobQuery, "status = 2")
	requirePlaceholdersMatchArgs(t, finishQualitySuggestionJobQuery,
		[]any{int64(1), int32(3), "err", time.Now()})

	require.Contains(t, qualitySuggestionJobCancelLockQuery, "FOR UPDATE")
	requirePlaceholdersMatchArgs(t, qualitySuggestionJobCancelLockQuery, []any{"job1"})

	require.Contains(t, createQualitySuggestionJobQuery, "INSERT INTO quality_suggestion_job")
	require.Contains(t, createQualitySuggestionJobQuery, "RETURNING id, created_at")
	requirePlaceholdersMatchArgs(t, createQualitySuggestionJobQuery, make([]any, 11))

	// Retention only prunes terminal rows: the three status arguments are
	// COMPLETED, FAILED and CANCELED.
	require.Contains(t, deleteExpiredQualitySuggestionJobsQuery, "finished_at IS NOT NULL")
	require.Contains(t, deleteExpiredQualitySuggestionJobsQuery, "status IN ($1, $2, $3)")
	requirePlaceholdersMatchArgs(t, deleteExpiredQualitySuggestionJobsQuery,
		[]any{int16(3), int16(4), int16(5), time.Now()})
}

func TestQualitySuggestionJobStatusHelpers(t *testing.T) {
	t.Parallel()

	for _, status := range []storepb.QualitySuggestionJobStatus{
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED,
	} {
		require.True(t, qualitySuggestionJobStatusIsValid(status), status.String())
	}
	require.False(t, qualitySuggestionJobStatusIsValid(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED))
	require.False(t, qualitySuggestionJobStatusIsValid(storepb.QualitySuggestionJobStatus(99)))

	for _, status := range []storepb.QualitySuggestionJobStatus{
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED,
	} {
		require.True(t, qualitySuggestionJobTerminalStatusIsValid(status), status.String())
	}
	require.False(t, qualitySuggestionJobTerminalStatusIsValid(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING))
	require.False(t, qualitySuggestionJobTerminalStatusIsValid(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING))
	require.False(t, qualitySuggestionJobTerminalStatusIsValid(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED))

	require.True(t, qualitySuggestionJobTargetStateIsValid(storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE))
	require.False(t, qualitySuggestionJobTargetStateIsValid(storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED))
	require.False(t, qualitySuggestionJobTargetStateIsValid(storepb.QualitySuggestionJobTargetState(99)))
}

// Canceling a pending job skips only targets that were never asked; a target with
// an outcome keeps it.
func TestMarkQualitySuggestionJobTargetsSkipped(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 20, 9, 0, 0, 0, time.UTC)
	targets := &storepb.QualitySuggestionJobTargets{Targets: []*storepb.QualitySuggestionJobTarget{
		{TargetGuid: "pending", State: storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING},
		{TargetGuid: "done", State: storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE},
		{TargetGuid: "failed", State: storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED},
		{TargetGuid: "running", State: storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING},
	}}
	markQualitySuggestionJobTargetsSkipped(targets, at)

	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED, targets.Targets[0].GetState())
	require.Equal(t, at.UnixMilli(), targets.Targets[0].GetFinishedAtUnixMs())
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, targets.Targets[1].GetState())
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED, targets.Targets[2].GetState())
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING, targets.Targets[3].GetState(),
		"a running target is the runner's to close, not the canceller's")
}

func TestValidateFindQualitySuggestionJobRequiresTheResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, common.Invalid, common.ErrorCode(validateFindQualitySuggestionJob(nil)))
	empty := ""
	require.Equal(t, common.Invalid, common.ErrorCode(validateFindQualitySuggestionJob(&FindQualitySuggestionJobMessage{ResourceID: &empty})))
	resourceID := "job1"
	require.NoError(t, validateFindQualitySuggestionJob(&FindQualitySuggestionJobMessage{ResourceID: &resourceID}))
}
