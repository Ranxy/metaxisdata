package openlineage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveRunMetadata(t *testing.T) {
	event := &RunEvent{
		Run: Run{
			RunID: "task-run-123",
			Facets: map[string]json.RawMessage{
				"parent": json.RawMessage(`{
					"job": {"namespace": "airflow", "name": "example_dag"},
					"run": {"runId": "dag-run-456"},
					"root": {
						"job": {"namespace": "airflow", "name": "root_dag"},
						"run": {"runId": "root-run-789"}
					}
				}`),
			},
		},
		Job: Job{
			Namespace: "airflow",
			Name:      "example_dag.extract_users",
			Facets: map[string]json.RawMessage{
				"jobType": json.RawMessage(`{
					"jobType": "TASK",
					"integration": "AIRFLOW",
					"processingType": "BATCH"
				}`),
			},
		},
		Inputs: []Dataset{{Namespace: "warehouse", Name: "raw.users"}},
	}

	derived := DeriveRunMetadata(event)

	require.Equal(t, "TASK", derived.JobType)
	assert.Equal(t, "AIRFLOW", derived.Integration)
	assert.Equal(t, "BATCH", derived.ProcessingType)
	assert.Equal(t, "airflow", derived.ParentJobNamespace)
	assert.Equal(t, "example_dag", derived.ParentJobName)
	assert.Equal(t, "dag-run-456", derived.ParentRunID)
	assert.Equal(t, "airflow", derived.RootJobNamespace)
	assert.Equal(t, "root_dag", derived.RootJobName)
	assert.Equal(t, "root-run-789", derived.RootRunID)
	assert.True(t, derived.HasLineage)
	assert.Equal(t, BuildOpenLineageTaskGUID("airflow", "example_dag.extract_users", "TASK"), derived.TaskGUID)
	assert.Equal(t, "openlineage:run:TASK:airflow:example_dag.extract_users:task-run-123", BuildOpenLineageRunGUID("airflow", "example_dag.extract_users", derived.JobType, "task-run-123"))
}

func TestDeriveRunMetadataDefaults(t *testing.T) {
	derived := DeriveRunMetadata(&RunEvent{
		Run: Run{RunID: "run-1"},
		Job: Job{Namespace: "ns", Name: "job"},
	})

	assert.Equal(t, "UNSPECIFIED", derived.JobType)
	assert.Equal(t, BuildOpenLineageTaskGUID("ns", "job", "UNSPECIFIED"), derived.TaskGUID)
	assert.False(t, derived.HasLineage)
	assert.Equal(t, "openlineage:task:UNSPECIFIED:ns:job", derived.TaskGUID)
}
