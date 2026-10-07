package v1

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	openlineageplugin "github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The store aggregates the references in SQL; this attaches each aggregate to
// its resolution, and a dataset the resolver cannot answer keeps its external
// identity instead of emptying the page.
func TestResolveOpenLineageDatasetAggregates(t *testing.T) {
	t.Parallel()

	seen := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	aggregates := []*store.OpenLineageDatasetAggregateMessage{
		{
			Namespace:             "postgres://warehouse:5432/analytics",
			Name:                  "public.orders",
			LastSeen:              &seen,
			SourceJobCount:        2,
			TargetJobCount:        1,
			Integrations:          []string{"airflow"},
			Sources:               []string{"openlineage"},
			SupportsColumnLineage: true,
		},
		{Namespace: "s3://analytics-bucket", Name: "exports/orders_snapshot", LastSeen: &seen, TargetJobCount: 3},
		{Namespace: "broken://namespace", Name: "public.orders"},
	}

	datasets := resolveOpenLineageDatasetAggregates(context.Background(), aggregates, func(_ context.Context, namespace, name string) (*openlineageplugin.ResolvedDataset, error) {
		switch namespace {
		case "postgres://warehouse:5432/analytics":
			return &openlineageplugin.ResolvedDataset{GUID: "inst;analytics;public;orders", MetaType: storepb.MetaType_TABLE, Internal: true}, nil
		case "s3://analytics-bucket":
			return &openlineageplugin.ResolvedDataset{GUID: openlineageplugin.FormatExternalGUID(namespace, name), MetaType: storepb.MetaType_EXTERNAL_DATASET, Internal: false}, nil
		default:
			return nil, errors.New("resolver failed")
		}
	})

	require.Len(t, datasets, 3)

	internal := datasets[0]
	require.True(t, internal.Internal)
	require.Equal(t, "inst;analytics;public;orders", internal.GUID)
	require.Equal(t, "analytics.public.orders", internal.ResolvedTarget)
	require.Equal(t, v1pb.MetaType_TABLE, internal.ResolvedMetaType)
	require.Equal(t, "database", internal.DatasetType)
	require.Equal(t, int32(2), internal.SourceJobCount)
	require.Equal(t, int32(1), internal.TargetJobCount)
	require.Equal(t, []string{"airflow"}, internal.Integrations)
	require.Equal(t, []string{"openlineage"}, internal.Sources)
	require.True(t, internal.SupportsColumnLineage)
	require.NotNil(t, internal.LastSeen)
	require.True(t, internal.LastSeen.AsTime().Equal(seen))

	external := datasets[1]
	require.False(t, external.Internal)
	require.Equal(t, openlineageplugin.FormatExternalGUID("s3://analytics-bucket", "exports/orders_snapshot"), external.GUID)
	require.Empty(t, external.ResolvedTarget)
	require.Equal(t, "s3", external.DatasetType)
	require.NotNil(t, external.LastSeen)
	require.True(t, external.LastSeen.AsTime().Equal(seen))

	fallback := datasets[2]
	require.False(t, fallback.Internal)
	require.Equal(t, openlineageplugin.FormatExternalGUID("broken://namespace", "public.orders"), fallback.GUID)
	require.Equal(t, v1pb.MetaType_EXTERNAL_DATASET, fallback.ResolvedMetaType)
	require.Nil(t, fallback.LastSeen, "a dataset without an event time keeps a null last-seen")
}

// The request's SQL-answerable filters reach the store; the free-text search and
// the scope are applied in Go because they need the dataset's resolution.
func TestOpenLineageDatasetFind(t *testing.T) {
	t.Parallel()

	find := openLineageDatasetFind(&v1pb.ListOpenLineageDatasetsRequest{
		Namespace:         "ns",
		Integration:       "airflow",
		Source:            "openlineage",
		ColumnLineageOnly: true,
	})
	require.Equal(t, "ns", *find.Namespace)
	require.Equal(t, "airflow", *find.Integration)
	require.Equal(t, "openlineage", *find.Source)
	require.True(t, *find.ColumnLineageOnly)

	empty := openLineageDatasetFind(&v1pb.ListOpenLineageDatasetsRequest{})
	require.Nil(t, empty.Namespace)
	require.Nil(t, empty.Integration)
	require.Nil(t, empty.Source)
	require.Nil(t, empty.ColumnLineageOnly)
}

func TestConvertOpenLineageDatasetSchemaFields(t *testing.T) {
	t.Parallel()

	fields := convertOpenLineageDatasetSchemaFields(&store.OpenLineageDatasetDetailMessage{
		SchemaFields:        []byte(`[{"name":"order_id","type":"INT"},{"name":"total","type":"NUMERIC","description":"sum"}]`),
		ColumnLineageFields: []string{"order_id"},
	})
	require.Len(t, fields, 2)
	assert.Equal(t, "order_id", fields[0].Name)
	assert.Equal(t, "INT", fields[0].Type)
	assert.True(t, fields[0].ColumnLineageReady)
	assert.Equal(t, "total", fields[1].Name)
	assert.Equal(t, "sum", fields[1].Description)
	assert.False(t, fields[1].ColumnLineageReady)

	require.Nil(t, convertOpenLineageDatasetSchemaFields(&store.OpenLineageDatasetDetailMessage{}))
	require.Nil(t, convertOpenLineageDatasetSchemaFields(&store.OpenLineageDatasetDetailMessage{
		SchemaFields: []byte(`{`),
	}))
}

func TestConvertOpenLineageDatasetJobsAndRuns(t *testing.T) {
	t.Parallel()

	seen := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	jobs := convertOpenLineageDatasetJobs([]*store.OpenLineageDatasetJobMessage{
		{
			TaskGUID:      "openlineage:task:TASK:prod:jobA",
			JobNamespace:  "prod",
			JobName:       "jobA",
			JobType:       "TASK",
			Integration:   "airflow",
			LastSeen:      &seen,
			RunCount:      4,
			WritesDataset: true,
		},
		{TaskGUID: "openlineage:task:TASK:prod:jobB", JobName: "jobB"},
	})
	require.Len(t, jobs, 2)
	assert.Equal(t, "jobA", jobs[0].JobName)
	assert.Equal(t, int32(4), jobs[0].RunCount)
	assert.True(t, jobs[0].WritesDataset)
	assert.False(t, jobs[0].ReadsDataset)
	require.NotNil(t, jobs[0].LastSeen)
	assert.True(t, jobs[0].LastSeen.AsTime().Equal(seen))
	assert.Nil(t, jobs[1].LastSeen)

	runs := convertOpenLineageDatasetRuns([]*store.OpenLineageDatasetRunMessage{
		{
			RunGUID:       "openlineage:run:TASK:prod:jobA:run-1",
			TaskGUID:      "openlineage:task:TASK:prod:jobA",
			RunID:         "run-1",
			JobNamespace:  "prod",
			JobName:       "jobA",
			JobType:       "TASK",
			EventType:     "COMPLETE",
			EventTime:     &seen,
			HasLineage:    true,
			ReadsDataset:  true,
			WritesDataset: true,
		},
		{RunGUID: "openlineage:run:TASK:prod:jobB:run-2", RunID: "run-2"},
	})
	require.Len(t, runs, 2)
	assert.Equal(t, "run-1", runs[0].RunId)
	assert.True(t, runs[0].HasLineage)
	assert.True(t, runs[0].ReadsDataset)
	require.NotNil(t, runs[0].EventTime)
	assert.True(t, runs[0].EventTime.AsTime().Equal(seen))
	assert.Nil(t, runs[1].EventTime)
}

func TestFilterOpenLineageDatasets(t *testing.T) {
	t.Parallel()

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

	// The search box advertises integrations and sources, so a term that only
	// appears in one of them has to match.
	filtered = filterOpenLineageDatasets(datasets, &v1pb.ListOpenLineageDatasetsRequest{
		Search: "dbt",
	})
	require.Len(t, filtered, 1)
	assert.Equal(t, "exports/orders_snapshot", filtered[0].Name)

	filtered = filterOpenLineageDatasets(datasets, &v1pb.ListOpenLineageDatasetsRequest{
		Search: "SCHEDULER",
	})
	require.Len(t, filtered, 1)
	assert.Equal(t, "public.orders", filtered[0].Name)

	// The resolved target is advertised by the search box as well, and it only
	// exists after resolution.
	filtered = filterOpenLineageDatasets(datasets, &v1pb.ListOpenLineageDatasetsRequest{
		Search: "analytics.public.orders",
	})
	require.Len(t, filtered, 1)
	assert.Equal(t, "public.orders", filtered[0].Name)
}

// A MySQL guid has an empty schema segment. It must be trimmed positionally, so
// the instance id is never rendered as the database.
func TestFormatResolvedTargetKeepsTheSegmentPositions(t *testing.T) {
	t.Parallel()

	require.Equal(t, "db.schema.table", formatResolvedTarget("inst;db;schema;table", true))
	require.Equal(t, "db..table", formatResolvedTarget("inst;db;;table", true))
	require.Equal(t, "db.table", formatResolvedTarget("db;table", true))
	require.Empty(t, formatResolvedTarget("inst;db;;table", false))
}
