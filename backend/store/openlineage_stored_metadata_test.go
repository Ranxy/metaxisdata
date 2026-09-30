package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A registry snapshot holds what the object is, not what the object's own table
// derives from other rows. That is what makes the fingerprint mean "this object
// changed": the counters a payload yields and the moment the row was written
// must not be able to rewrite it.
func TestOpenLineageRunSnapshotHoldsTheRunItself(t *testing.T) {
	t.Parallel()

	eventTime := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	run := &OpenLineageRunMessage{
		GUID:               "openlineage:run:TASK:default:job:run-1",
		TaskGUID:           "openlineage:task:TASK:default:job",
		RunID:              "run-1",
		JobNamespace:       "default",
		JobName:            "job",
		JobType:            "TASK",
		EventType:          "COMPLETE",
		EventTime:          &eventTime,
		Producer:           "https://github.com/apache/airflow",
		Source:             "openlineage",
		Integration:        "AIRFLOW",
		ProcessingType:     "BATCH",
		ParentJobNamespace: "default",
		ParentJobName:      "dag",
		ParentRunID:        "parent-run",
		RootJobNamespace:   "default",
		RootJobName:        "dag",
		RootRunID:          "root-run",
		// Derived from the payload, so the snapshot must not carry them.
		InputCount:  2,
		OutputCount: 3,
		HasLineage:  true,
		CreatedAt:   eventTime,
		UpdatedAt:   eventTime.Add(time.Hour),
	}

	metadataBytes, metaHash, err := CalcStoreMetaHash(buildOpenLineageRunStoredMetadata(run))
	require.NoError(t, err)

	var persisted storepb.StoredMetadata
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(metadataBytes, &persisted))
	summary := persisted.GetOpenlineageRunSummary()
	require.NotNil(t, summary)
	require.Equal(t, "run-1", summary.GetRunId())
	require.Equal(t, "default", summary.GetJobNamespace())
	require.Equal(t, "COMPLETE", summary.GetEventType())
	require.Equal(t, "dag", summary.GetRootJobName())
	require.Equal(t, eventTime, summary.GetEventTime().AsTime())

	// protojson omits a zero value, so an absent field is only provable by name.
	for _, derived := range []string{"inputCount", "outputCount", "hasLineage", "createdAt", "updatedAt"} {
		require.NotContains(t, string(metadataBytes), derived)
	}

	// The same run delivered again is the same snapshot, however its counters and
	// the row's timestamps moved.
	redelivered := *run
	redelivered.InputCount, redelivered.OutputCount, redelivered.HasLineage = 9, 9, false
	redelivered.UpdatedAt = eventTime.Add(24 * time.Hour)
	_, redeliveredHash, err := CalcStoreMetaHash(buildOpenLineageRunStoredMetadata(&redelivered))
	require.NoError(t, err)
	require.Equal(t, metaHash, redeliveredHash)

	// A run that reaches another state is a new snapshot, which is the change the
	// registry history exists to record.
	failed := *run
	failed.EventType = "FAIL"
	_, failedHash, err := CalcStoreMetaHash(buildOpenLineageRunStoredMetadata(&failed))
	require.NoError(t, err)
	require.NotEqual(t, metaHash, failedHash)
}

// A job's snapshot is the job, so folding a new run into it must not rewrite it.
// The run the job last saw and the counters over its runs stay in the task table,
// which is where the read path joins them from.
func TestOpenLineageTaskSnapshotHoldsTheJobNotItsRuns(t *testing.T) {
	t.Parallel()

	eventTime := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	task := &OpenLineageTaskMessage{
		GUID:               "openlineage:task:TASK:default:job",
		JobNamespace:       "default",
		JobName:            "job",
		JobType:            "TASK",
		Integration:        "AIRFLOW",
		ProcessingType:     "BATCH",
		ParentJobNamespace: "default",
		ParentJobName:      "dag",
		RootJobNamespace:   "default",
		RootJobName:        "dag",
		// Derived from the runs, so the snapshot must not carry them.
		LatestRunGUID:   "openlineage:run:TASK:default:job:run-2",
		LatestRunID:     "run-2",
		LatestEventTime: &eventTime,
		LatestProducer:  "producer",
		LatestSource:    "openlineage",
		LatestEventType: "FAIL",
		RunCount:        12,
		LineageRunCount: 7,
		CreatedAt:       eventTime,
		UpdatedAt:       eventTime.Add(time.Hour),
	}

	metadataBytes, metaHash, err := CalcStoreMetaHash(buildOpenLineageTaskStoredMetadata(task))
	require.NoError(t, err)

	var persisted storepb.StoredMetadata
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(metadataBytes, &persisted))
	summary := persisted.GetOpenlineageTaskSummary()
	require.NotNil(t, summary)
	require.Equal(t, "job", summary.GetJobName())
	require.Equal(t, "AIRFLOW", summary.GetIntegration())
	require.Equal(t, "dag", summary.GetRootJobName())

	for _, derived := range []string{
		"latestRunGuid", "latestRunId", "latestEventTime", "latestEventType",
		"latestProducer", "latestSource", "runCount", "lineageRunCount",
		"createdAt", "updatedAt",
	} {
		require.NotContains(t, string(metadataBytes), derived)
	}

	moved := *task
	moved.LatestRunGUID = "openlineage:run:TASK:default:job:run-3"
	moved.LatestRunID = "run-3"
	moved.LatestEventType = "COMPLETE"
	moved.RunCount = 13
	moved.LineageRunCount = 8
	moved.UpdatedAt = eventTime.Add(2 * time.Hour)
	_, movedHash, err := CalcStoreMetaHash(buildOpenLineageTaskStoredMetadata(&moved))
	require.NoError(t, err)
	require.Equal(t, metaHash, movedHash)
}
