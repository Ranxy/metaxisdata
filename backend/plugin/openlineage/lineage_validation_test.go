package openlineage

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func edge(sourceGUID, sourceColumn, targetGUID, targetColumn string) *store.ColumnLineage {
	return &store.ColumnLineage{
		SourceGUID:   sourceGUID,
		SourceColumn: sourceColumn,
		SourceType:   storepb.MetaType_TABLE,
		TargetGUID:   targetGUID,
		TargetColumn: targetColumn,
		TargetType:   storepb.MetaType_TABLE,
		RelationType: model.RelationTypeDirect,
	}
}

func guidSet(guids ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(guids))
	for _, guid := range guids {
		set[guid] = struct{}{}
	}
	return set
}

// tables marks every guid as a TABLE, which is the only object type whose
// columns exist as their own registry rows.
func tables(guids ...string) map[string]storepb.MetaType {
	set := make(map[string]storepb.MetaType, len(guids))
	for _, guid := range guids {
		set[guid] = storepb.MetaType_TABLE
	}
	return set
}

// The Airflow SQL extractor attaches every parsed input column to every parsed
// output, so a fact table ends up with edges naming columns of unrelated views.
// A column the relation does not have is the one thing the registry can prove
// wrong, and blanking it is what keeps "where does this column come from"
// trustworthy without losing the dependency.
func TestFilterIngestedLineageBlanksColumnsTheRelationDoesNotHave(t *testing.T) {
	relation := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	view := "test-pg-1;e2e;e2e_dwd;v_order_base"
	lineages := []*store.ColumnLineage{
		edge(view, "order_id", relation, "order_id"),
		edge(view, "city", relation, "city"),
		edge(view, "net_amount", relation, "_3"),
	}

	kept, report := filterIngestedLineage(
		lineages,
		tables(relation),
		guidSet(
			columnGUID(relation, "order_id"),
			columnGUID(view, "order_id"),
			columnGUID(view, "city"),
			columnGUID(view, "net_amount"),
		),
	)

	// The verifiable claim stays a column edge; the two unverifiable ones collapse
	// into the single table-level edge the run implies.
	require.Len(t, kept, 2, "the dependency survives even when the column claim does not")
	assert.Equal(t, 2, report.degradedEdges)
	assert.Equal(t, 2, report.keptEdges)
	assert.Len(t, report.invalidColumns, 2)
	assert.Contains(t, report.invalidColumns, columnGUID(relation, "city"))
	assert.Contains(t, report.invalidColumns, columnGUID(relation, "_3"))

	columnEdge, tableEdge := kept[0], kept[1]
	if columnEdge.SourceColumn == "" {
		columnEdge, tableEdge = tableEdge, columnEdge
	}
	assert.Equal(t, "order_id", columnEdge.SourceColumn, "a verifiable claim is kept")
	assert.Equal(t, "order_id", columnEdge.TargetColumn)
	assert.Empty(t, tableEdge.SourceColumn, "an unverifiable mapping degrades to the relation pair")
	assert.Empty(t, tableEdge.TargetColumn)
	assert.Equal(t, view, tableEdge.SourceGUID)
	assert.Equal(t, relation, tableEdge.TargetGUID)
}

// The real case this comes from: the extractor reports the dependency of
// dwd_order_fact.paid_amount on v_customer_payments only through a positional
// target column, so blanking that column must still leave the table-level edge
// that says the load read that view.
func TestFilterIngestedLineageKeepsTheDependencyOfADegradedEdge(t *testing.T) {
	view := "test-pg-1;e2e;e2e_dwd;v_customer_payments"
	table := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	lineages := []*store.ColumnLineage{
		edge(view, "paid_amount", table, "_3"),
		edge(view, "fee_amount", table, "_4"),
	}

	kept, report := filterIngestedLineage(lineages, tables(table), guidSet())

	// Both claims degrade to the same table-level edge, which is stored once.
	require.Len(t, kept, 1)
	assert.Equal(t, view, kept[0].SourceGUID)
	assert.Equal(t, table, kept[0].TargetGUID)
	assert.Empty(t, kept[0].TargetColumn)
	assert.Empty(t, kept[0].Transformation)
	assert.Equal(t, model.RelationTypeDirect, kept[0].RelationType)
	assert.Equal(t, 2, report.degradedEdges)
}

// Ingestion runs before the next schema sync, so a table created minutes ago is
// legitimately unknown. Keeping those edges and reporting them beats losing
// lineage for a relation nobody can replay.
func TestFilterIngestedLineageKeepsUnknownRelationsAndReportsThem(t *testing.T) {
	known := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	newTable := "mysql-dev-1;e2e_ods;;brand_new_table"
	cte := "test-pg-1;e2e;public;line_agg"
	lineages := []*store.ColumnLineage{
		edge(newTable, "id", known, "order_id"),
		edge(cte, "line_count", known, "line_count"),
	}

	kept, report := filterIngestedLineage(
		lineages,
		tables(known),
		guidSet(columnGUID(known, "order_id"), columnGUID(known, "line_count")),
	)

	assert.Len(t, kept, 2)
	assert.Equal(t, 0, report.degradedEdges)
	assert.Equal(t, 2, report.unknownRelationEdges)
	assert.Len(t, report.unknownRelations, 2)
	assert.Contains(t, report.unknownRelations, newTable)
	assert.Contains(t, report.unknownRelations, cte)
}

// An external dataset has no registry row by design, so it is never evidence
// that an edge is wrong.
func TestFilterIngestedLineageLeavesExternalEndpointsAlone(t *testing.T) {
	external := FormatExternalGUID("s3://bucket", "raw/events.json")
	known := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	lineages := []*store.ColumnLineage{edge(external, "payload", known, "order_id")}

	kept, report := filterIngestedLineage(
		lineages,
		tables(known),
		guidSet(columnGUID(known, "order_id")),
	)

	assert.Len(t, kept, 1)
	assert.Equal(t, 0, report.degradedEdges)
	assert.Equal(t, 0, report.unknownRelationEdges)
}

// Table-level edges carry no column, and the registry cannot contradict them.
func TestFilterIngestedLineageKeepsRelationsWithoutColumns(t *testing.T) {
	source := "mysql-dev-1;e2e_ods;;orders"
	target := "test-pg-1;e2e;e2e_ods;orders"
	lineages := []*store.ColumnLineage{edge(source, "", target, "")}

	kept, report := filterIngestedLineage(lineages, tables(source, target), guidSet())

	assert.Len(t, kept, 1)
	assert.Equal(t, 0, report.degradedEdges)
}

func TestFilterIngestedLineageBoundsReportedNames(t *testing.T) {
	relation := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	lineages := make([]*store.ColumnLineage, 0, maxReportedNames*3)
	for i := 0; i < maxReportedNames*3; i++ {
		lineages = append(lineages, edge(relation, "order_id", relation, "_"+strconv.Itoa(i)))
	}

	kept, report := filterIngestedLineage(
		lineages,
		tables(relation),
		guidSet(columnGUID(relation, "order_id")),
	)

	// Every bogus target vanishes, and the surviving table-level edge is one row.
	assert.Len(t, kept, 1)
	assert.Equal(t, maxReportedNames*3, report.degradedEdges)
	assert.Len(t, report.invalidColumns, maxReportedNames)
}

func TestCollectRelationGUIDsDeduplicatesAndSkipsExternal(t *testing.T) {
	known := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	lineages := []*store.ColumnLineage{
		edge(known, "order_id", known, "order_id"),
		edge(FormatExternalGUID("s3://bucket", "raw/events.json"), "payload", known, "order_id"),
	}

	assert.ElementsMatch(t, []string{known}, collectRelationGUIDs(lineages))
}

func TestCollectColumnGUIDsSkipsColumnsOfUnknownRelations(t *testing.T) {
	known := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	unknown := "test-pg-1;e2e;public;line_agg"
	lineages := []*store.ColumnLineage{
		edge(unknown, "line_count", known, "line_count"),
		edge(known, "", unknown, ""),
	}

	columns := collectColumnGUIDs(lineages, tables(known))

	assert.ElementsMatch(t, []string{columnGUID(known, "line_count")}, columns)
}

func TestColumnGUIDAppendsTheEscapedColumnName(t *testing.T) {
	assert.Equal(t, "inst;db;;t;amount", columnGUID("inst;db;;t", "amount"))
	// A column name carrying the separator must not shift the GUID fields.
	assert.Equal(t, "inst;db;;t;a%3Bb", columnGUID("inst;db;;t", "a;b"))
}

// A SQL facet with no datasets is how an extractor that dropped every table
// looks: the run says "no lineage" and nothing else.
func TestMissingSQLLineage(t *testing.T) {
	withSQL := &RunEvent{Job: Job{Facets: map[string]json.RawMessage{"sql": json.RawMessage(`{"query":"select 1"}`)}}}
	assert.True(t, missingSQLLineage(withSQL))

	withDatasets := *withSQL
	withDatasets.Outputs = []Dataset{{Namespace: "postgres://localhost:5432", Name: "e2e.e2e_dwd.dwd_order_fact"}}
	assert.False(t, missingSQLLineage(&withDatasets))

	withoutSQL := &RunEvent{Inputs: []Dataset{{Namespace: "postgres://localhost:5432", Name: "e2e.e2e_ods.orders"}}}
	assert.False(t, missingSQLLineage(withoutSQL))

	assert.False(t, missingSQLLineage(&RunEvent{}))
}

// Only a TABLE stores its columns as registry rows. A view keeps them inside
// its own metadata, so a claim against a view column cannot be checked and must
// not be dropped for a row that was never written.
func TestFilterIngestedLineageLeavesViewColumnsAlone(t *testing.T) {
	view := "test-pg-1;e2e;e2e_dwd;v_order_base"
	table := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	lineages := []*store.ColumnLineage{edge(view, "not_a_real_column", table, "order_id")}

	kept, report := filterIngestedLineage(
		lineages,
		map[string]storepb.MetaType{
			view:  storepb.MetaType_VIEW,
			table: storepb.MetaType_TABLE,
		},
		guidSet(columnGUID(table, "order_id")),
	)

	assert.Len(t, kept, 1)
	assert.Equal(t, 0, report.degradedEdges)
}

// The registry lookup that validates the edges also knows each relation's object
// type, so an ingested edge carries the same types an analyzed one does.
func TestApplyObjectTypes(t *testing.T) {
	view := "test-pg-1;e2e;e2e_dwd;v_order_base"
	table := "test-pg-1;e2e;e2e_dwd;dwd_order_fact"
	unknown := "test-pg-1;e2e;public;line_agg"
	lineage := edge(view, "", table, "")
	lineage.SourceType = storepb.MetaType_TABLE
	lineage.TargetType = storepb.MetaType_TABLE

	applyObjectTypes(lineage, map[string]storepb.MetaType{view: storepb.MetaType_VIEW})

	assert.Equal(t, storepb.MetaType_VIEW, lineage.SourceType)
	assert.Equal(t, storepb.MetaType_TABLE, lineage.TargetType, "an unknown relation keeps the type it had")

	external := edge(FormatExternalGUID("s3://bucket", "raw/x"), "payload", unknown, "")
	external.SourceType = storepb.MetaType_EXTERNAL_DATASET
	external.TargetType = storepb.MetaType_TABLE
	applyObjectTypes(external, map[string]storepb.MetaType{})
	assert.Equal(t, storepb.MetaType_EXTERNAL_DATASET, external.SourceType, "an external dataset is not in the lookup")
	assert.Equal(t, storepb.MetaType_TABLE, external.TargetType)
}
