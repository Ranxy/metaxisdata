package v1

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	openlineageplugin "github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestAggregateOpenLineageDatasets(t *testing.T) {
	firstSeen := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	secondSeen := firstSeen.Add(2 * time.Hour)
	runs := []*store.OpenLineageRunMessage{
		{
			TaskGUID:     "openlineage:task:TASK:prod:jobA",
			JobNamespace: "prod",
			JobName:      "jobA",
			JobType:      "TASK",
			Integration:  "airflow",
			Source:       "scheduler",
			EventTime:    &firstSeen,
			RawPayload: []byte(`{
				"eventType":"COMPLETE",
				"run":{"runId":"run-1"},
				"job":{"namespace":"prod","name":"jobA"},
				"inputs":[
					{"namespace":"postgres://warehouse:5432/analytics","name":"public.orders"}
				],
				"outputs":[
					{
						"namespace":"postgres://warehouse:5432/analytics",
						"name":"public.daily_orders",
						"facets":{"columnLineage":{"fields":{"order_id":{"inputFields":[{"namespace":"postgres://warehouse:5432/analytics","name":"public.orders","field":"order_id"}]}}}}
					}
				]
			}`),
		},
		{
			TaskGUID:     "openlineage:task:TASK:prod:jobB",
			JobNamespace: "prod",
			JobName:      "jobB",
			JobType:      "TASK",
			Integration:  "dbt",
			Source:       "transform",
			EventTime:    &secondSeen,
			RawPayload: []byte(`{
				"eventType":"COMPLETE",
				"run":{"runId":"run-2"},
				"job":{"namespace":"prod","name":"jobB"},
				"inputs":[
					{"namespace":"postgres://warehouse:5432/analytics","name":"public.daily_orders"},
					{"namespace":"s3://analytics-bucket","name":"exports/orders_snapshot"}
				],
				"outputs":[
					{"namespace":"s3://analytics-bucket","name":"exports/orders_snapshot"}
				]
			}`),
		},
	}

	aggregates := aggregateOpenLineageDatasets(context.Background(), runs, func(_ context.Context, namespace, name string) (*openlineageplugin.ResolvedDataset, error) {
		switch {
		case namespace == "postgres://warehouse:5432/analytics" && name == "public.orders":
			return &openlineageplugin.ResolvedDataset{GUID: "inst;analytics;public;orders", MetaType: storepb.MetaType_TABLE, Internal: true}, nil
		case namespace == "postgres://warehouse:5432/analytics" && name == "public.daily_orders":
			return &openlineageplugin.ResolvedDataset{GUID: "inst;analytics;public;daily_orders", MetaType: storepb.MetaType_TABLE, Internal: true}, nil
		default:
			return &openlineageplugin.ResolvedDataset{GUID: openlineageplugin.FormatExternalGUID(namespace, name), MetaType: storepb.MetaType_EXTERNAL_DATASET, Internal: false}, nil
		}
	})

	require.Len(t, aggregates, 3)
	assert.Equal(t, "exports/orders_snapshot", aggregates[0].Name)
	assert.Equal(t, int32(1), aggregates[0].SourceJobCount)
	assert.Equal(t, int32(1), aggregates[0].TargetJobCount)
	assert.False(t, aggregates[0].Internal)

	dailyOrders := findDatasetAggregateByName(t, aggregates, "public.daily_orders")
	assert.True(t, dailyOrders.Internal)
	assert.True(t, dailyOrders.SupportsColumnLineage)
	assert.Equal(t, "analytics.public.daily_orders", dailyOrders.ResolvedTarget)
	assert.Equal(t, []string{"airflow", "dbt"}, dailyOrders.Integrations)
	assert.Equal(t, []string{"scheduler", "transform"}, dailyOrders.Sources)
	assert.Equal(t, int32(1), dailyOrders.SourceJobCount)
	assert.Equal(t, int32(1), dailyOrders.TargetJobCount)
	assert.NotNil(t, dailyOrders.LastSeen)
	assert.True(t, dailyOrders.LastSeen.Equal(secondSeen))
	assert.Equal(t, v1pb.MetaType_TABLE, dailyOrders.ResolvedMetaType)
}

func TestFilterOpenLineageDatasets(t *testing.T) {
	datasets := []*openLineageDatasetAggregate{
		{
			Name:                  "public.orders",
			Namespace:             "postgres://warehouse:5432/analytics",
			DatasetType:           "database",
			ResolvedTarget:        "analytics.public.orders",
			Internal:              true,
			SupportsColumnLineage: true,
			Integrations:          []string{"airflow"},
			Sources:               []string{"scheduler"},
		},
		{
			Name:                  "exports/orders_snapshot",
			Namespace:             "s3://analytics-bucket",
			DatasetType:           "s3",
			Internal:              false,
			SupportsColumnLineage: false,
			Integrations:          []string{"dbt"},
			Sources:               []string{"transform"},
		},
	}

	filtered := filterOpenLineageDatasets(datasets, &v1pb.ListOpenLineageDatasetsRequest{
		Search:            "orders",
		Integration:       "airflow",
		DatasetScope:      v1pb.OpenLineageDatasetScope_OPENLINEAGE_DATASET_SCOPE_INTERNAL,
		ColumnLineageOnly: true,
	})

	require.Len(t, filtered, 1)
	assert.Equal(t, "public.orders", filtered[0].Name)

	filtered = filterOpenLineageDatasets(datasets, &v1pb.ListOpenLineageDatasetsRequest{
		Source:       "transform",
		DatasetScope: v1pb.OpenLineageDatasetScope_OPENLINEAGE_DATASET_SCOPE_EXTERNAL,
	})
	require.Len(t, filtered, 1)
	assert.Equal(t, "exports/orders_snapshot", filtered[0].Name)
}

func TestBuildOpenLineageDatasetDetail(t *testing.T) {
	firstSeen := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	secondSeen := firstSeen.Add(2 * time.Hour)
	runs := []*store.OpenLineageRunMessage{
		{
			GUID:         "openlineage:run:TASK:prod:jobA:run-1",
			TaskGUID:     "openlineage:task:TASK:prod:jobA",
			JobNamespace: "prod",
			JobName:      "jobA",
			JobType:      "TASK",
			Integration:  "airflow",
			EventType:    "COMPLETE",
			Source:       "scheduler",
			HasLineage:   true,
			EventTime:    &firstSeen,
			RawPayload: []byte(`{
				"eventType":"COMPLETE",
				"run":{"runId":"run-1"},
				"job":{"namespace":"prod","name":"jobA"},
				"inputs":[
					{"namespace":"postgres://warehouse:5432/analytics","name":"public.orders","facets":{"schema":{"fields":[{"name":"order_id","type":"INT"},{"name":"amount","type":"NUMERIC"}]}}}
				],
				"outputs":[
					{
						"namespace":"postgres://warehouse:5432/analytics",
						"name":"public.daily_orders",
						"facets":{
							"schema":{"fields":[{"name":"order_id","type":"INT"},{"name":"total_amount","type":"NUMERIC"}]},
							"columnLineage":{"fields":{"order_id":{"inputFields":[{"namespace":"postgres://warehouse:5432/analytics","name":"public.orders","field":"order_id"}]}}}
						}
					}
				]
			}`),
		},
		{
			GUID:         "openlineage:run:TASK:prod:jobB:run-2",
			TaskGUID:     "openlineage:task:TASK:prod:jobB",
			JobNamespace: "prod",
			JobName:      "jobB",
			JobType:      "TASK",
			Integration:  "dbt",
			EventType:    "COMPLETE",
			Source:       "transform",
			HasLineage:   true,
			EventTime:    &secondSeen,
			RawPayload: []byte(`{
				"eventType":"COMPLETE",
				"run":{"runId":"run-2"},
				"job":{"namespace":"prod","name":"jobB"},
				"inputs":[
					{"namespace":"postgres://warehouse:5432/analytics","name":"public.daily_orders"}
				],
				"outputs":[
					{"namespace":"s3://analytics-bucket","name":"exports/orders_snapshot"}
				]
			}`),
		},
	}

	detail, found := buildOpenLineageDatasetDetail(
		context.Background(),
		runs,
		"inst;analytics;public;daily_orders",
		"postgres://warehouse:5432/analytics",
		"public.daily_orders",
		func(_ context.Context, namespace, name string) (*openlineageplugin.ResolvedDataset, error) {
			switch {
			case namespace == "postgres://warehouse:5432/analytics" && name == "public.orders":
				return &openlineageplugin.ResolvedDataset{GUID: "inst;analytics;public;orders", MetaType: storepb.MetaType_TABLE, Internal: true}, nil
			case namespace == "postgres://warehouse:5432/analytics" && name == "public.daily_orders":
				return &openlineageplugin.ResolvedDataset{GUID: "inst;analytics;public;daily_orders", MetaType: storepb.MetaType_TABLE, Internal: true}, nil
			default:
				return &openlineageplugin.ResolvedDataset{GUID: openlineageplugin.FormatExternalGUID(namespace, name), MetaType: storepb.MetaType_EXTERNAL_DATASET, Internal: false}, nil
			}
		},
	)

	require.True(t, found)
	require.NotNil(t, detail)
	assert.Equal(t, "public.daily_orders", detail.Dataset.Name)
	require.Len(t, detail.SchemaFields, 2)
	assert.Equal(t, "order_id", detail.SchemaFields[0].Name)
	assert.True(t, detail.SchemaFields[0].ColumnLineageReady)
	require.Len(t, detail.RelatedJobs, 2)
	assert.Equal(t, "jobB", detail.RelatedJobs[0].JobName)
	assert.True(t, detail.RelatedJobs[0].ReadsDataset)
	assert.False(t, detail.RelatedJobs[0].WritesDataset)
	assert.Equal(t, "jobA", detail.RelatedJobs[1].JobName)
	assert.False(t, detail.RelatedJobs[1].ReadsDataset)
	assert.True(t, detail.RelatedJobs[1].WritesDataset)
	require.Len(t, detail.RecentRuns, 2)
	assert.Equal(t, "run-2", detail.RecentRuns[0].RunId)
	assert.True(t, detail.RecentRuns[0].ReadsDataset)
	assert.Equal(t, "run-1", detail.RecentRuns[1].RunId)
	assert.True(t, detail.RecentRuns[1].WritesDataset)
}

func findDatasetAggregateByName(t *testing.T, datasets []*openLineageDatasetAggregate, name string) *openLineageDatasetAggregate {
	t.Helper()
	for _, dataset := range datasets {
		if dataset.Name == name {
			return dataset
		}
	}
	t.Fatalf("dataset %q not found", name)
	return nil
}
