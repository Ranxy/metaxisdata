package openlineage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRunEvent_AirflowPostgresColumnLineage(t *testing.T) {
	data := loadTestdata(t, "airflow_postgres_column_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	assert.Equal(t, "a1b2c3d4-e5f6-7890-abcd-ef1234567890", event.Run.RunID)

	// Verify Airflow-specific run facets are preserved.
	require.NotNil(t, event.Run.Facets)
	_, hasParentRun := event.Run.Facets["parentRun"]
	assert.True(t, hasParentRun, "should have parentRun facet")
	_, hasProcessingEngine := event.Run.Facets["processing_engine"]
	assert.True(t, hasProcessingEngine, "should have processing_engine facet")
	_, hasAirflow := event.Run.Facets["airflow"]
	assert.True(t, hasAirflow, "should have airflow facet")
	_, hasAirflowState := event.Run.Facets["airflowState"]
	assert.True(t, hasAirflowState, "should have airflowState facet")

	// Verify Airflow job naming convention: dag_id.task_id.
	assert.Equal(t, "default", event.Job.Namespace)
	assert.Equal(t, "etl_dag.transform_orders", event.Job.Name)

	// Verify Airflow job facets.
	require.NotNil(t, event.Job.Facets)
	_, hasJobType := event.Job.Facets["jobType"]
	assert.True(t, hasJobType, "should have jobType facet")
	_, hasSQL := event.Job.Facets["sql"]
	assert.True(t, hasSQL, "should have sql facet")

	// Verify inputs.
	require.Len(t, event.Inputs, 2)
	assert.Equal(t, "postgres://analytics-db:5432/warehouse", event.Inputs[0].Namespace)
	assert.Equal(t, "staging.orders", event.Inputs[0].Name)
	require.NotNil(t, event.Inputs[0].Facets.Schema)
	assert.Len(t, event.Inputs[0].Facets.Schema.Fields, 4)

	assert.Equal(t, "staging.customers", event.Inputs[1].Name)
	assert.Len(t, event.Inputs[1].Facets.Schema.Fields, 3)

	// Verify outputs with column lineage.
	require.Len(t, event.Outputs, 1)
	output := event.Outputs[0]
	assert.Equal(t, "analytics.order_summary", output.Name)
	require.NotNil(t, output.Facets.ColumnLineage)
	require.Len(t, output.Facets.ColumnLineage.Fields, 4)

	// Verify column lineage mappings.
	orderID, ok := output.Facets.ColumnLineage.Fields["order_id"]
	require.True(t, ok)
	require.Len(t, orderID.InputFields, 1)
	assert.Equal(t, "staging.orders", orderID.InputFields[0].Name)
	assert.Equal(t, "order_id", orderID.InputFields[0].Field)

	totalAmount, ok := output.Facets.ColumnLineage.Fields["total_amount"]
	require.True(t, ok)
	require.Len(t, totalAmount.InputFields, 1)
	assert.Equal(t, "staging.orders", totalAmount.InputFields[0].Name)
	assert.Equal(t, "amount", totalAmount.InputFields[0].Field)
	require.Len(t, totalAmount.InputFields[0].Transformations, 1)
	assert.Equal(t, "AGGREGATION", totalAmount.InputFields[0].Transformations[0].Subtype)
	assert.Equal(t, "SUM(amount)", totalAmount.InputFields[0].Transformations[0].Description)

	itemCount, ok := output.Facets.ColumnLineage.Fields["item_count"]
	require.True(t, ok)
	require.Len(t, itemCount.InputFields, 1)
	assert.Equal(t, "AGGREGATION", itemCount.InputFields[0].Transformations[0].Subtype)
	assert.Equal(t, "COUNT(*)", itemCount.InputFields[0].Transformations[0].Description)
}

func TestParseRunEvent_AirflowPythonTableLineage(t *testing.T) {
	data := loadTestdata(t, "airflow_python_table_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	assert.Equal(t, "b2c3d4e5-f6a7-8901-bcde-f23456789012", event.Run.RunID)
	assert.Equal(t, "data_pipeline.process_data", event.Job.Name)

	// Airflow-specific facets.
	_, hasParentRun := event.Run.Facets["parentRun"]
	assert.True(t, hasParentRun)
	_, hasAirflow := event.Run.Facets["airflow"]
	assert.True(t, hasAirflow)

	// Verify inputs without column lineage (PythonOperator scenario).
	require.Len(t, event.Inputs, 2)
	assert.Equal(t, "postgres://prod-db:5432/app", event.Inputs[0].Namespace)
	assert.Equal(t, "public.raw_events", event.Inputs[0].Name)
	assert.Equal(t, "public.event_metadata", event.Inputs[1].Name)

	// Output has NO column lineage (table-level only).
	require.Len(t, event.Outputs, 1)
	output := event.Outputs[0]
	assert.Equal(t, "public.processed_events", output.Name)
	assert.Nil(t, output.Facets.ColumnLineage, "PythonOperator should not emit column lineage")

	// Schema should still be present.
	require.NotNil(t, output.Facets.Schema)
	assert.Len(t, output.Facets.Schema.Fields, 5)
}

func TestParseRunEvent_AirflowBigQuery(t *testing.T) {
	data := loadTestdata(t, "airflow_bigquery.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	assert.Equal(t, "c3d4e5f6-a7b8-9012-cdef-345678901234", event.Run.RunID)
	assert.Equal(t, "bq_etl_dag.aggregate_sales", event.Job.Name)

	// BigQuery uses bare "bigquery" namespace.
	require.Len(t, event.Inputs, 1)
	assert.Equal(t, "bigquery", event.Inputs[0].Namespace)
	assert.Equal(t, "myproject.raw.sales", event.Inputs[0].Name)

	require.Len(t, event.Outputs, 1)
	output := event.Outputs[0]
	assert.Equal(t, "bigquery", output.Namespace)
	assert.Equal(t, "myproject.analytics.daily_sales", output.Name)

	// Column lineage present.
	require.NotNil(t, output.Facets.ColumnLineage)
	assert.Len(t, output.Facets.ColumnLineage.Fields, 4)

	// Verify revenue has two input fields (price, quantity).
	revenue, ok := output.Facets.ColumnLineage.Fields["revenue"]
	require.True(t, ok)
	assert.Len(t, revenue.InputFields, 2)

	// Verify sale_date transformation.
	saleDate, ok := output.Facets.ColumnLineage.Fields["sale_date"]
	require.True(t, ok)
	require.Len(t, saleDate.InputFields, 1)
	assert.Equal(t, "sale_time", saleDate.InputFields[0].Field)
	assert.Equal(t, "TRANSFORMATION", saleDate.InputFields[0].Transformations[0].Subtype)
}

func TestParseRunEvent_AirflowDatasetLevelLineage(t *testing.T) {
	data := loadTestdata(t, "airflow_dataset_level_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	assert.Equal(t, "d4e5f6a7-b8c9-0123-defa-456789012345", event.Run.RunID)

	// Inputs.
	require.Len(t, event.Inputs, 2)
	assert.Equal(t, "public.users", event.Inputs[0].Name)
	assert.Equal(t, "public.orders", event.Inputs[1].Name)

	// Output has column lineage facet with empty fields but dataset-level references.
	require.Len(t, event.Outputs, 1)
	output := event.Outputs[0]
	require.NotNil(t, output.Facets.ColumnLineage)
	assert.Empty(t, output.Facets.ColumnLineage.Fields, "fields should be empty for dataset-level lineage")

	// Dataset-level references.
	require.Len(t, output.Facets.ColumnLineage.Dataset, 2)
	assert.Equal(t, "public.users", output.Facets.ColumnLineage.Dataset[0].Name)
	assert.Equal(t, "public.orders", output.Facets.ColumnLineage.Dataset[1].Name)
}
