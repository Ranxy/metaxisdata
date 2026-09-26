package openlineage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

func TestSqlFacetQuery(t *testing.T) {
	tests := []struct {
		name  string
		facet string
		want  string
		ok    bool
	}{
		{name: "query", facet: `{"query":"INSERT INTO t SELECT 1"}`, want: "INSERT INTO t SELECT 1", ok: true},
		{name: "trimmed", facet: `{"query":"  SELECT 1 \n"}`, want: "SELECT 1", ok: true},
		{name: "empty query", facet: `{"query":"   "}`, ok: false},
		{name: "no query field", facet: `{"_producer":"airflow"}`, ok: false},
		{name: "not an object", facet: `"select 1"`, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &RunEvent{Job: Job{Facets: map[string]json.RawMessage{"sql": json.RawMessage(tt.facet)}}}
			got, ok := sqlFacetQuery(event)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}

	assert.False(t, hasSQLFacet(&RunEvent{}))
	assert.False(t, hasSQLFacet(&RunEvent{Job: Job{Facets: map[string]json.RawMessage{"processing_engine": json.RawMessage(`{}`)}}}))
}

func TestAnchorDataset(t *testing.T) {
	fromOutputs := &RunEvent{
		Inputs:  []Dataset{{Name: "in"}},
		Outputs: []Dataset{{Name: "out"}},
	}
	anchor, ok := anchorDataset(fromOutputs)
	require.True(t, ok)
	assert.Equal(t, "out", anchor.Name, "a write names the connection the statement ran on")

	onlyInputs := &RunEvent{Inputs: []Dataset{{Name: "in"}}}
	anchor, ok = anchorDataset(onlyInputs)
	require.True(t, ok)
	assert.Equal(t, "in", anchor.Name)

	_, ok = anchorDataset(&RunEvent{})
	assert.False(t, ok)
}

// The analyzer answers with the relation names the SQL names, so the mapping is
// what turns them into the GUIDs the run stores. A statement result is not a
// stored object and must not become one.
func TestMapAnalyzedRelations(t *testing.T) {
	meta := lineageMeta{GUID: "openlineage:run:TASK:default:dag.task:run-1", Type: storepb.MetaType_OPENLINEAGE}
	analysisContext := catalog.AnalysisContext{InstanceID: "test-pg-1", Database: "e2e", Schema: "e2e_dwd"}
	relations := []model.ColumnRelation{
		{
			Source:         model.Column{Table: model.ObjectIdentifier{Schema: "e2e_dwd", Name: "v_order_line"}, Name: "line_amount"},
			Target:         model.Column{Table: model.ObjectIdentifier{Schema: "e2e_dwd", Name: "dwd_order_fact"}, Name: "line_revenue"},
			Transformation: []model.Transformation{{Operation: model.OperationAggregate, Expression: "sum"}},
			RelationType:   model.RelationTypeGroup,
		},
		{
			// A bare SELECT records its result against a synthetic table.
			Source:       model.Column{Table: model.ObjectIdentifier{Schema: "e2e_dwd", Name: "v_order_line"}, Name: "order_id"},
			Target:       model.Column{Table: model.ObjectIdentifier{Name: model.ResultTableName}, Name: "order_id"},
			RelationType: model.RelationTypeDirect,
			IsTemp:       true,
		},
		{
			// A loading statement names the file it reads the same way.
			Source:       model.Column{Table: model.ObjectIdentifier{Name: model.FileSourceName}, Name: "*"},
			Target:       model.Column{Table: model.ObjectIdentifier{Schema: "e2e_dwd", Name: "dwd_order_fact"}, Name: "*"},
			RelationType: model.RelationTypeDirect,
		},
	}

	lineages := mapAnalyzedRelations(meta, analysisContext, relations)

	require.Len(t, lineages, 1)
	assert.Equal(t, "test-pg-1;e2e;e2e_dwd;v_order_line", lineages[0].SourceGUID)
	assert.Equal(t, "line_amount", lineages[0].SourceColumn)
	assert.Equal(t, "test-pg-1;e2e;e2e_dwd;dwd_order_fact", lineages[0].TargetGUID)
	assert.Equal(t, "line_revenue", lineages[0].TargetColumn)
	assert.Equal(t, model.RelationTypeGroup, lineages[0].RelationType)
	assert.Len(t, lineages[0].Transformation, 1)
	assert.Equal(t, meta.GUID, lineages[0].MetaGUID)
	assert.Equal(t, storepb.MetaType_OPENLINEAGE, lineages[0].MetaType)
}

func TestMapAnalyzedRelationsDefaultsTransformations(t *testing.T) {
	lineages := mapAnalyzedRelations(lineageMeta{GUID: "run", Type: storepb.MetaType_OPENLINEAGE}, catalog.AnalysisContext{InstanceID: "i", Database: "d"},
		[]model.ColumnRelation{{
			Source: model.Column{Table: model.ObjectIdentifier{Name: "a"}, Name: "x"},
			Target: model.Column{Table: model.ObjectIdentifier{Name: "b"}, Name: "x"},
		}})

	require.Len(t, lineages, 1)
	assert.NotNil(t, lineages[0].Transformation, "a stored edge carries an empty transformation list, never nil")
	assert.Empty(t, lineages[0].Transformation)
	assert.Equal(t, "i;d;;a", lineages[0].SourceGUID)
}
