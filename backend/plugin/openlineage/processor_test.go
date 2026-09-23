package openlineage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ingestedRelationType is what a stored edge now carries: the processor derives
// the relation type from the transformations by the same rule a SQL analyzer uses,
// so an ingested aggregation is a grouping and an ingested join is a join.
func ingestedRelationType(transforms []OLTransform) model.RelationType {
	return model.RelationTypeOf(mapTransformations(transforms))
}

func TestIngestedRelationType(t *testing.T) {
	tests := []struct {
		name       string
		transforms []OLTransform
		want       model.RelationType
	}{
		{
			name:       "empty transforms -> direct",
			transforms: nil,
			want:       model.RelationTypeDirect,
		},
		{
			name: "DIRECT/IDENTITY -> direct",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "IDENTITY"},
			},
			want: model.RelationTypeDirect,
		},
		{
			name: "DIRECT/TRANSFORMATION -> indirect",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "TRANSFORMATION"},
			},
			want: model.RelationTypeIndirect,
		},
		{
			name: "DIRECT/AGGREGATION -> group",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "AGGREGATION"},
			},
			want: model.RelationTypeGroup,
		},
		{
			name: "INDIRECT/JOIN -> join",
			transforms: []OLTransform{
				{Type: "INDIRECT", Subtype: "JOIN"},
			},
			want: model.RelationTypeJoin,
		},
		{
			name: "INDIRECT/FILTER -> indirect",
			transforms: []OLTransform{
				{Type: "INDIRECT", Subtype: "FILTER"},
			},
			want: model.RelationTypeIndirect,
		},
		{
			name: "INDIRECT/SORT -> indirect",
			transforms: []OLTransform{
				{Type: "INDIRECT", Subtype: "SORT"},
			},
			want: model.RelationTypeIndirect,
		},
		{
			name: "unknown type -> indirect",
			transforms: []OLTransform{
				{Type: "UNKNOWN", Subtype: ""},
			},
			want: model.RelationTypeIndirect,
		},
		{
			name: "DIRECT with unknown subtype -> indirect",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "CUSTOM_SUBTYPE"},
			},
			want: model.RelationTypeIndirect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ingestedRelationType(tt.transforms)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMapTransformations(t *testing.T) {
	tests := []struct {
		name       string
		transforms []OLTransform
		wantLen    int
		wantFirst  *model.Transformation
	}{
		{
			name:       "nil transforms",
			transforms: nil,
			wantLen:    0,
		},
		{
			name:       "empty transforms",
			transforms: []OLTransform{},
			wantLen:    0,
		},
		{
			// An identity facet is a direct edge, and a direct edge carries no
			// transformation: keeping a PROJECT here would store a relation type
			// and a transformation list that disagree.
			name: "identity transform",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "IDENTITY"},
			},
			wantLen: 0,
		},
		{
			name: "transformation with description",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "TRANSFORMATION", Description: "UPPER(name)"},
			},
			wantLen: 1,
			wantFirst: &model.Transformation{
				Operation:  model.OperationFunction,
				Expression: "UPPER(name)",
			},
		},
		{
			name: "aggregation",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "AGGREGATION", Description: "SUM(amount)"},
			},
			wantLen: 1,
			wantFirst: &model.Transformation{
				Operation:  model.OperationAggregate,
				Expression: "SUM(amount)",
			},
		},
		{
			name: "indirect transform",
			transforms: []OLTransform{
				{Type: "INDIRECT", Subtype: "SORT", Description: "ORDER BY id"},
			},
			wantLen: 1,
			wantFirst: &model.Transformation{
				Operation:  model.OperationSort,
				Expression: "ORDER BY id",
			},
		},
		{
			name: "multiple transforms",
			transforms: []OLTransform{
				{Type: "DIRECT", Subtype: "IDENTITY"},
				{Type: "DIRECT", Subtype: "TRANSFORMATION", Description: "CAST(x AS INT)"},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapTransformations(tt.transforms)
			if tt.wantLen == 0 {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, tt.wantLen)
			if tt.wantFirst != nil {
				assert.Equal(t, tt.wantFirst.Operation, got[0].Operation)
				assert.Equal(t, tt.wantFirst.Expression, got[0].Expression)
			}
		})
	}
}

func TestMapOperationType(t *testing.T) {
	tests := []struct {
		name      string
		olType    string
		olSubtype string
		want      model.OperationType
	}{
		{"DIRECT/IDENTITY", "DIRECT", "IDENTITY", model.OperationProject},
		{"DIRECT/AGGREGATION", "DIRECT", "AGGREGATION", model.OperationAggregate},
		{"DIRECT/TRANSFORMATION", "DIRECT", "TRANSFORMATION", model.OperationFunction},
		{"DIRECT/other", "DIRECT", "CUSTOM", model.OperationFunction},
		{"INDIRECT/JOIN", "INDIRECT", "JOIN", model.OperationJoin},
		{"INDIRECT/GROUP_BY", "INDIRECT", "GROUP_BY", model.OperationGroupBy},
		{"INDIRECT/FILTER", "INDIRECT", "FILTER", model.OperationFilter},
		{"INDIRECT/SORT", "INDIRECT", "SORT", model.OperationSort},
		{"INDIRECT/other", "INDIRECT", "CUSTOM", model.OperationProject},
		{"unknown type", "UNKNOWN", "X", model.OperationProject},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapOperationType(tt.olType, tt.olSubtype)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseAndMapEndToEnd(t *testing.T) {
	data := loadTestdata(t, "complete_with_column_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	output := event.Outputs[0]
	require.NotNil(t, output.Facets.ColumnLineage)

	// Verify transformation mapping for the order_delivery_time column
	deliveryTime := output.Facets.ColumnLineage.Fields["order_delivery_time"]
	require.Len(t, deliveryTime.InputFields, 2)

	// First input: order_placed_on with TRANSFORMATION
	transforms1 := mapTransformations(deliveryTime.InputFields[0].Transformations)
	require.Len(t, transforms1, 1)
	assert.Equal(t, model.OperationFunction, transforms1[0].Operation)
	assert.Equal(t, "DATEDIFF(minute, order_placed_on, order_delivered_on)", transforms1[0].Expression)

	relType1 := ingestedRelationType(deliveryTime.InputFields[0].Transformations)
	assert.Equal(t, model.RelationTypeIndirect, relType1)

	// Verify identity mapping for order_id
	orderID := output.Facets.ColumnLineage.Fields["order_id"]
	require.Len(t, orderID.InputFields, 1)

	relType2 := ingestedRelationType(orderID.InputFields[0].Transformations)
	assert.Equal(t, model.RelationTypeDirect, relType2)

	transforms2 := mapTransformations(orderID.InputFields[0].Transformations)
	require.Empty(t, transforms2, "an identity facet carries no transformation")
}

func TestParseAndMapCrossInstance(t *testing.T) {
	data := loadTestdata(t, "cross_instance_event.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	output := event.Outputs[0]
	require.NotNil(t, output.Facets.ColumnLineage)

	// total_amount: AGGREGATION from quantity and price
	totalAmount := output.Facets.ColumnLineage.Fields["total_amount"]
	require.Len(t, totalAmount.InputFields, 2)

	for _, input := range totalAmount.InputFields {
		relType := ingestedRelationType(input.Transformations)
		assert.Equal(t, model.RelationTypeGroup, relType, "AGGREGATION should map to Group")

		transforms := mapTransformations(input.Transformations)
		require.Len(t, transforms, 1)
		assert.Equal(t, model.OperationAggregate, transforms[0].Operation)
		assert.Equal(t, "quantity * price", transforms[0].Expression)
	}

	// item_id: IDENTITY
	itemID := output.Facets.ColumnLineage.Fields["item_id"]
	require.Len(t, itemID.InputFields, 1)
	relType := ingestedRelationType(itemID.InputFields[0].Transformations)
	assert.Equal(t, model.RelationTypeDirect, relType)

	// event_id: TRANSFORMATION
	eventID := output.Facets.ColumnLineage.Fields["event_id"]
	require.Len(t, eventID.InputFields, 1)
	relType = ingestedRelationType(eventID.InputFields[0].Transformations)
	assert.Equal(t, model.RelationTypeIndirect, relType)
}

func TestParseAndMapAirflowPostgres(t *testing.T) {
	data := loadTestdata(t, "airflow_postgres_column_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	output := event.Outputs[0]
	require.NotNil(t, output.Facets.ColumnLineage)

	// customer_name: IDENTITY from customers
	customerName := output.Facets.ColumnLineage.Fields["customer_name"]
	require.Len(t, customerName.InputFields, 1)
	relType := ingestedRelationType(customerName.InputFields[0].Transformations)
	assert.Equal(t, model.RelationTypeDirect, relType)

	// total_amount: AGGREGATION
	totalAmount := output.Facets.ColumnLineage.Fields["total_amount"]
	require.Len(t, totalAmount.InputFields, 1)
	transforms := mapTransformations(totalAmount.InputFields[0].Transformations)
	require.Len(t, transforms, 1)
	assert.Equal(t, model.OperationAggregate, transforms[0].Operation)
	assert.Equal(t, "SUM(amount)", transforms[0].Expression)

	// item_count: AGGREGATION
	itemCount := output.Facets.ColumnLineage.Fields["item_count"]
	require.Len(t, itemCount.InputFields, 1)
	transforms = mapTransformations(itemCount.InputFields[0].Transformations)
	require.Len(t, transforms, 1)
	assert.Equal(t, model.OperationAggregate, transforms[0].Operation)
}

func TestParseAndMapAirflowBigQuery(t *testing.T) {
	data := loadTestdata(t, "airflow_bigquery.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	output := event.Outputs[0]
	require.NotNil(t, output.Facets.ColumnLineage)

	// sale_date: TRANSFORMATION from sale_time
	saleDate := output.Facets.ColumnLineage.Fields["sale_date"]
	require.Len(t, saleDate.InputFields, 1)
	relType := ingestedRelationType(saleDate.InputFields[0].Transformations)
	assert.Equal(t, model.RelationTypeIndirect, relType)

	transforms := mapTransformations(saleDate.InputFields[0].Transformations)
	require.Len(t, transforms, 1)
	assert.Equal(t, model.OperationFunction, transforms[0].Operation)
	assert.Equal(t, "DATE(sale_time)", transforms[0].Expression)
}

func TestTableLevelLineageDetection(t *testing.T) {
	data := loadTestdata(t, "airflow_python_table_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETE", event.EventType)
	require.Len(t, event.Inputs, 2)
	require.Len(t, event.Outputs, 1)

	// Output has no column lineage.
	assert.Nil(t, event.Outputs[0].Facets.ColumnLineage)

	// This event should trigger table-level lineage processing
	// (inputs present, output has no column lineage).
	hasColumnLineage := event.Outputs[0].Facets.ColumnLineage != nil &&
		len(event.Outputs[0].Facets.ColumnLineage.Fields) > 0
	assert.False(t, hasColumnLineage, "should not have column lineage")
	assert.True(t, len(event.Inputs) > 0, "should have inputs for table-level lineage")
}

func TestDatasetReferenceLineageKeepsFieldAndIndirectKind(t *testing.T) {
	meta := lineageMeta{GUID: "openlineage:task:default:etl_dag.transform_orders", Type: storepb.MetaType_OPENLINEAGE}
	source := &ResolvedDataset{GUID: "prod;warehouse;staging;orders", MetaType: storepb.MetaType_TABLE}
	target := &ResolvedDataset{GUID: "prod;warehouse;marts;summary", MetaType: storepb.MetaType_VIEW}

	// A dataset-level reference names the field that influences the whole output.
	// Dropping the field, as the processor used to, lost the filter column and
	// recorded an indirect relation as direct.
	lineage := datasetReferenceLineage(meta, source, target, ColumnLineageDatasetReference{
		Namespace: "prod", Name: "staging.orders", Field: "status",
		Transformations: []OLTransform{{Type: "INDIRECT", Subtype: "FILTER"}},
	})

	require.Equal(t, "status", lineage.SourceColumn)
	require.Empty(t, lineage.TargetColumn, "an influence on the rows reaches no single column")
	require.Equal(t, model.RelationTypeIndirect, lineage.RelationType)
	require.Len(t, lineage.Transformation, 1)
	require.Equal(t, model.OperationFilter, lineage.Transformation[0].Operation)

	// A join key is a join, the same relation type a SQL analyzer records for an
	// ON or USING column.
	join := datasetReferenceLineage(meta, source, target, ColumnLineageDatasetReference{
		Namespace: "prod", Name: "staging.orders", Field: "customer_id",
		Transformations: []OLTransform{{Type: "INDIRECT", Subtype: "JOIN"}},
	})
	require.Equal(t, model.RelationTypeJoin, join.RelationType)
	require.Equal(t, model.OperationJoin, join.Transformation[0].Operation)
}

func TestDatasetLevelLineageDetection(t *testing.T) {
	data := loadTestdata(t, "airflow_dataset_level_lineage.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	require.Len(t, event.Outputs, 1)
	output := event.Outputs[0]
	require.NotNil(t, output.Facets.ColumnLineage)

	// Fields should be empty, dataset references present.
	assert.Empty(t, output.Facets.ColumnLineage.Fields)
	require.Len(t, output.Facets.ColumnLineage.Dataset, 2)

	// This should trigger dataset-level lineage processing
	// (column lineage facet exists with dataset refs but no field-level refs).
	hasDatasetLineage := len(output.Facets.ColumnLineage.Dataset) > 0
	assert.True(t, hasDatasetLineage, "should have dataset-level lineage")
}

func TestSchemaInferredLineageDetection(t *testing.T) {
	data := loadTestdata(t, "complete_schema_only.json")
	event, err := ParseRunEvent(data)
	require.NoError(t, err)

	require.Len(t, event.Inputs, 1)
	require.Len(t, event.Outputs, 1)
	assert.Nil(t, event.Outputs[0].Facets.ColumnLineage)
	require.NotNil(t, event.Inputs[0].Facets.Schema)
	require.NotNil(t, event.Outputs[0].Facets.Schema)

	columnPairs, input, inferred := inferSchemaColumnPairs(event.Inputs, &event.Outputs[0])
	assert.True(t, inferred)
	require.Len(t, columnPairs, 4)
	assert.Equal(t, event.Inputs[0].Name, input.Name)
	assert.Equal(t, []string{"id", "tax_dt", "tax_item_id", "amount"}, columnPairs)
}

func TestSchemaInferredLineageRequiresResolverAndSingleInput(t *testing.T) {
	lineages, _, inferred := inferSchemaColumnPairs([]Dataset{}, &Dataset{})
	assert.False(t, inferred)
	assert.Empty(t, lineages)
}

func TestBuildLineageMetaUsesPersistedOpenLineageRun(t *testing.T) {
	persistedRun := &store.OpenLineageRunMessage{
		GUID:  "openlineage:run:default:etl_dag.transform_orders:a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		RunID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	}

	meta := buildLineageMeta(persistedRun)

	assert.Equal(t, storepb.MetaType_OPENLINEAGE, meta.Type)
	assert.Equal(t, persistedRun.GUID, meta.GUID)
}

func TestBuildColumnLineageKeepsFlowOnSourceAndTarget(t *testing.T) {
	meta := lineageMeta{
		GUID: "openlineage:task:default:etl_dag.transform_orders",
		Type: storepb.MetaType_OPENLINEAGE,
	}
	source := &ResolvedDataset{
		GUID:     "prod;warehouse;staging;orders",
		MetaType: storepb.MetaType_TABLE,
	}
	target := &ResolvedDataset{
		GUID:     "prod;warehouse;analytics;order_summary",
		MetaType: storepb.MetaType_TABLE,
	}

	lineage := buildColumnLineage(
		meta,
		source,
		target,
		"order_id",
		"order_id",
		[]model.Transformation{},
	)

	assert.Equal(t, meta.GUID, lineage.MetaGUID)
	assert.Equal(t, meta.Type, lineage.MetaType)
	assert.Equal(t, source.GUID, lineage.SourceGUID)
	assert.Equal(t, source.MetaType, lineage.SourceType)
	assert.Equal(t, target.GUID, lineage.TargetGUID)
	assert.Equal(t, target.MetaType, lineage.TargetType)
}
