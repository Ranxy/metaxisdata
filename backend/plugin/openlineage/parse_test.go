package openlineage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata", name)
}

func loadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(testdataPath(name))
	require.NoError(t, err)
	return data
}

func TestParseRunEvent_CompleteWithColumnLineage(t *testing.T) {
	data := loadTestdata(t, "complete_with_column_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	assert.Equal(t, "2026-04-05T10:30:00.000Z", event.EventTime)
	assert.Equal(t, "d46e465b-d358-4d32-83d4-df660ff614dd", event.Run.RunID)
	assert.Equal(t, "scheduler", event.Job.Namespace)
	assert.Equal(t, "etl_delivery_summary", event.Job.Name)

	// Inputs
	require.Len(t, event.Inputs, 2)
	assert.Equal(t, "postgres://warehouse:5432", event.Inputs[0].Namespace)
	assert.Equal(t, "public.delivery_7_days", event.Inputs[0].Name)
	require.NotNil(t, event.Inputs[0].Facets.Schema)
	assert.Len(t, event.Inputs[0].Facets.Schema.Fields, 4)
	assert.Equal(t, "order_id", event.Inputs[0].Facets.Schema.Fields[0].Name)
	assert.Equal(t, "INTEGER", event.Inputs[0].Facets.Schema.Fields[0].Type)

	assert.Equal(t, "public.customers", event.Inputs[1].Name)
	assert.Len(t, event.Inputs[1].Facets.Schema.Fields, 3)

	// Outputs
	require.Len(t, event.Outputs, 1)
	output := event.Outputs[0]
	assert.Equal(t, "postgres://warehouse:5432", output.Namespace)
	assert.Equal(t, "public.top_delivery_times", output.Name)
	require.NotNil(t, output.Facets.Schema)
	assert.Len(t, output.Facets.Schema.Fields, 4)

	// Column lineage
	require.NotNil(t, output.Facets.ColumnLineage)
	cl := output.Facets.ColumnLineage
	require.Len(t, cl.Fields, 4)

	// order_id: direct identity from delivery_7_days.order_id
	orderIDField, ok := cl.Fields["order_id"]
	require.True(t, ok)
	require.Len(t, orderIDField.InputFields, 1)
	assert.Equal(t, "public.delivery_7_days", orderIDField.InputFields[0].Name)
	assert.Equal(t, "order_id", orderIDField.InputFields[0].Field)
	require.Len(t, orderIDField.InputFields[0].Transformations, 1)
	assert.Equal(t, "DIRECT", orderIDField.InputFields[0].Transformations[0].Type)
	assert.Equal(t, "IDENTITY", orderIDField.InputFields[0].Transformations[0].Subtype)

	// customer_name: from customers.name
	customerNameField, ok := cl.Fields["customer_name"]
	require.True(t, ok)
	require.Len(t, customerNameField.InputFields, 1)
	assert.Equal(t, "public.customers", customerNameField.InputFields[0].Name)
	assert.Equal(t, "name", customerNameField.InputFields[0].Field)

	// order_delivery_time: from two input fields with TRANSFORMATION
	deliveryTimeField, ok := cl.Fields["order_delivery_time"]
	require.True(t, ok)
	require.Len(t, deliveryTimeField.InputFields, 2)
	assert.Equal(t, "order_placed_on", deliveryTimeField.InputFields[0].Field)
	assert.Equal(t, "TRANSFORMATION", deliveryTimeField.InputFields[0].Transformations[0].Subtype)
	assert.Equal(t, "order_delivered_on", deliveryTimeField.InputFields[1].Field)

	// RawJSON should be stored
	assert.NotNil(t, event.RawJSON)
	assert.Equal(t, data, []byte(event.RawJSON))
}

func TestParseRunEvent_CompleteSchemaOnly(t *testing.T) {
	data := loadTestdata(t, "complete_schema_only.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	assert.Equal(t, "a1b2c3d4-e5f6-7890-abcd-ef1234567890", event.Run.RunID)
	require.Len(t, event.Inputs, 1)
	require.Len(t, event.Outputs, 1)

	// No column lineage
	assert.Nil(t, event.Outputs[0].Facets.ColumnLineage)

	// Schema is present
	require.NotNil(t, event.Outputs[0].Facets.Schema)
	assert.Len(t, event.Outputs[0].Facets.Schema.Fields, 6)
}

func TestParseRunEvent_StartEvent(t *testing.T) {
	data := loadTestdata(t, "start_event.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "START", event.EventType)
	assert.Equal(t, "d46e465b-d358-4d32-83d4-df660ff614dd", event.Run.RunID)
	assert.Equal(t, "workshop", event.Job.Namespace)
	assert.Equal(t, "process_taxes", event.Job.Name)

	// Run facets should be raw JSON
	require.NotNil(t, event.Run.Facets)
	_, hasNominalTime := event.Run.Facets["nominalTime"]
	assert.True(t, hasNominalTime)

	// Job facets
	require.NotNil(t, event.Job.Facets)
	_, hasSQL := event.Job.Facets["sql"]
	assert.True(t, hasSQL)

	require.Len(t, event.Inputs, 1)
	assert.Empty(t, event.Outputs)
}

func TestParseRunEvent_FailEvent(t *testing.T) {
	data := loadTestdata(t, "fail_event.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "FAIL", event.EventType)
	assert.Equal(t, "d46e465b-d358-4d32-83d4-df660ff614dd", event.Run.RunID)

	// Error facet should be in raw facets
	require.NotNil(t, event.Run.Facets)
	_, hasError := event.Run.Facets["errorMessage"]
	assert.True(t, hasError)
}

func TestParseRunEvent_CompleteMinimal(t *testing.T) {
	data := loadTestdata(t, "complete_minimal.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", event.Run.RunID)
	assert.Empty(t, event.Inputs)
	assert.Empty(t, event.Outputs)
}

func TestParseRunEvent_CrossInstanceEvent(t *testing.T) {
	data := loadTestdata(t, "cross_instance_event.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	require.Len(t, event.Inputs, 1)
	assert.Equal(t, "mysql://prod-mysql:3306", event.Inputs[0].Namespace)
	assert.Equal(t, "orders.order_items", event.Inputs[0].Name)

	require.Len(t, event.Outputs, 1)
	output := event.Outputs[0]
	assert.Equal(t, "kafka://kafka-broker:9092", output.Namespace)
	assert.Equal(t, "order_events", output.Name)

	require.NotNil(t, output.Facets.ColumnLineage)
	assert.Len(t, output.Facets.ColumnLineage.Fields, 4)

	// total_amount has 2 input fields (quantity, price)
	totalAmount, ok := output.Facets.ColumnLineage.Fields["total_amount"]
	require.True(t, ok)
	require.Len(t, totalAmount.InputFields, 2)
	assert.Equal(t, "quantity", totalAmount.InputFields[0].Field)
	assert.Equal(t, "price", totalAmount.InputFields[1].Field)
	assert.Equal(t, "AGGREGATION", totalAmount.InputFields[0].Transformations[0].Subtype)
}

func TestParseRunEvent_InvalidJSON(t *testing.T) {
	_, err := ParseRunEvent([]byte(`{invalid json}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse OpenLineage RunEvent")
}

func TestParseRunEvent_MissingRunID(t *testing.T) {
	data := []byte(`{"eventType":"COMPLETE","run":{},"job":{"namespace":"ns","name":"job"},"inputs":[],"outputs":[]}`)
	_, err := ParseRunEvent(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing run.runId")
}

func TestParseRunEvent_MissingEventType(t *testing.T) {
	data := []byte(`{"run":{"runId":"abc-123"},"job":{"namespace":"ns","name":"job"},"inputs":[],"outputs":[]}`)
	_, err := ParseRunEvent(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing eventType")
}

func TestParseRunEvent_EmptyBody(t *testing.T) {
	_, err := ParseRunEvent([]byte{})
	require.Error(t, err)
}
