package algorithm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

func columnEdge(target string, transform ...model.Transformation) model.ColumnRelation {
	return model.ColumnRelation{
		Source:         model.Column{Table: model.ObjectIdentifier{Name: "t"}, Name: "c"},
		Target:         model.Column{Table: model.ObjectIdentifier{Name: target}, Name: "d"},
		Transformation: transform,
	}
}

func TestEdgeSetKeepsEdgesThatDifferInAnythingButTheirEndpoints(t *testing.T) {
	t.Parallel()

	add := model.NewOperatorTransformation("ADDITION", "a + 1")
	multiply := model.NewOperatorTransformation("MULTIPLICATION", "a * 2")
	qualified := columnEdge("u")
	qualified.Source.Table.Schema = "s1"
	otherQualifier := columnEdge("u")
	otherQualifier.Source.Table.Database = "s1"

	tests := []struct {
		name  string
		edges []model.ColumnRelation
		want  int
	}{
		{
			name:  "identical edges collapse",
			edges: []model.ColumnRelation{columnEdge("u"), columnEdge("u")},
			want:  1,
		},
		{
			name:  "two expressions reaching one target stay apart",
			edges: []model.ColumnRelation{columnEdge("u", add), columnEdge("u", multiply)},
			want:  2,
		},
		{
			name:  "a transformation is part of the identity",
			edges: []model.ColumnRelation{columnEdge("u"), columnEdge("u", add)},
			want:  2,
		},
		{
			// The qualifier is a value in the key, so a name that contains the
			// separator the rendering uses cannot make two edges look equal.
			name: "a qualifier is not part of the name",
			edges: []model.ColumnRelation{
				qualified,
				otherQualifier,
			},
			want: 2,
		},
		{
			name: "identifiers carrying the separator do not collide",
			edges: []model.ColumnRelation{
				{Source: model.Column{Table: model.ObjectIdentifier{Name: "a"}, Name: "b\x1fc"}, Target: model.Column{Table: model.ObjectIdentifier{Name: "u"}, Name: "d"}},
				{Source: model.Column{Table: model.ObjectIdentifier{Name: "a\x1fb"}, Name: "c"}, Target: model.Column{Table: model.ObjectIdentifier{Name: "u"}, Name: "d"}},
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set := NewEdgeSet()
			for _, edge := range tt.edges {
				set.Add(edge)
			}
			require.Equal(t, tt.want, set.Len())
			require.Len(t, set.Edges(), tt.want)
		})
	}
}

func TestTransformationEqualitySeparatesEveryField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  model.Transformation
		right model.Transformation
	}{
		{
			name:  "an argument list is not its rendering",
			left:  model.Transformation{Operation: model.OperationFunction, Arguments: []string{"a\x1eb"}},
			right: model.Transformation{Operation: model.OperationFunction, Arguments: []string{"a", "b"}},
		},
		{
			name:  "the function name and the expression are not interchangeable",
			left:  model.Transformation{Operation: model.OperationFunction, FunctionName: "x"},
			right: model.Transformation{Operation: model.OperationFunction, Expression: "x"},
		},
		{
			name:  "a condition is not an expression",
			left:  model.Transformation{Operation: model.OperationFilter, Condition: "x"},
			right: model.Transformation{Operation: model.OperationFilter, Expression: "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, model.SameTransformations([]model.Transformation{tt.left}, []model.Transformation{tt.right}))
		})
	}
}

// tableInScope registers a described base table so a reference to it resolves.
func tableInScope(sp *scope.Scope, name string, columns ...string) {
	ref := &scope.TableRef{Table: name}
	ref.SetColumnLookup(func() []string { return columns })
	sp.AddTable(ref)
}

func TestInfluencesBelongToTheScopeWhoseRowsTheyDecide(t *testing.T) {
	t.Parallel()

	rows := scope.NewScope(nil)
	tableInScope(rows, "t", "x")
	other := scope.NewScope(nil)
	tableInScope(other, "u", "y")

	influences := NewInfluences()
	influences.Resolve(rows, scope.ColumnRef{Table: "t", Column: "x"}, model.NewFilterTransformation("x = 1"), false)

	// The other scope's rows are what the statement emits, so the influence
	// collected over rows must not reach it.
	emitted := 0
	influences.Emit(other, "", "__result__", true, Emitter{
		Trace:   func(*scope.TableRef, string, string, string, string, []model.Transformation) { emitted++ },
		AddEdge: func(model.ColumnRelation) { emitted++ },
		NewEdge: scope.NewLineageEdge,
	})
	require.Zero(t, emitted, "an influence over rows nobody consumed reached the result")
}

func TestInheritMovesInfluencesSoTheyEmitOnce(t *testing.T) {
	t.Parallel()

	from := scope.NewScope(nil)
	tableInScope(from, "t", "x")
	to := scope.NewScope(nil)
	tableInScope(to, "u", "y")

	influences := NewInfluences()
	influences.Resolve(from, scope.ColumnRef{Table: "t", Column: "x"}, model.NewFilterTransformation("x = 1"), false)
	influences.Inherit(from, to)
	influences.Inherit(from, to)

	var edges []model.ColumnRelation
	emitter := Emitter{
		Trace: func(*scope.TableRef, string, string, string, string, []model.Transformation) {
			t.Fatal("unexpected trace")
		},
		AddEdge: func(rel model.ColumnRelation) { edges = append(edges, rel) },
		NewEdge: scope.NewLineageEdge,
	}
	influences.Emit(to, "", "__result__", true, emitter)
	influences.Emit(from, "", "__result__", true, emitter)

	require.Len(t, edges, 1, "an inherited influence must not be emitted twice")
	require.Equal(t, "t", edges[0].Source.Table.Name)
	require.Equal(t, "x", edges[0].Source.Name)
	require.Empty(t, edges[0].Target.Name)
	require.Equal(t, model.OperationFilter, edges[0].Transformation[0].Operation)
	require.Equal(t, "x = 1", edges[0].Transformation[0].Condition)
}

func TestBindCTEKeepsInfluencesForEveryReference(t *testing.T) {
	t.Parallel()

	body := scope.NewScope(nil)
	tableInScope(body, "t", "x")
	cte := &scope.CTEDefinition{Name: "c"}

	influences := NewInfluences()
	influences.Resolve(body, scope.ColumnRef{Table: "t", Column: "x"}, model.NewFilterTransformation("x = 1"), false)
	influences.BindCTE(cte, body)

	emitted := 0
	emitter := Emitter{
		Trace:   func(*scope.TableRef, string, string, string, string, []model.Transformation) { emitted++ },
		AddEdge: func(model.ColumnRelation) { emitted++ },
		NewEdge: scope.NewLineageEdge,
	}
	for range 2 {
		reader := scope.NewScope(nil)
		influences.InheritCTE(reader, cte)
		influences.Emit(reader, "", "__result__", true, emitter)
	}
	require.Equal(t, 2, emitted, "a CTE referenced twice carries its influences to both readers")

	// An unreferenced CTE's influences stay bound to the definition and are
	// dropped with the statement instead of reaching a result they do not.
	unreferenced := scope.NewScope(nil)
	influences.Emit(unreferenced, "", "__result__", true, emitter)
	require.Equal(t, 2, emitted)
}

func TestEmitTracesAQueryLocalRelation(t *testing.T) {
	t.Parallel()

	sp := scope.NewScope(nil)
	cte := &scope.TableRef{Table: "c", IsCTE: true, Lineage: []model.ColumnRelation{columnEdge("c")}}
	cte.SetColumnLookup(func() []string { return []string{"x"} })
	sp.AddTable(cte)

	influences := NewInfluences()
	influences.Resolve(sp, scope.ColumnRef{Table: "c", Column: "x"}, model.NewFilterTransformation("x = 1"), false)

	var traced int
	var added []model.ColumnRelation
	influences.Emit(sp, "s", "dst", false, Emitter{
		Trace: func(relation *scope.TableRef, column, targetQualifier, targetTable, targetColumn string, _ []model.Transformation) {
			traced++
			require.Same(t, cte, relation)
			require.Equal(t, "x", column)
			require.Equal(t, "s", targetQualifier)
			require.Equal(t, "dst", targetTable)
			require.Empty(t, targetColumn)
		},
		AddEdge: func(rel model.ColumnRelation) { added = append(added, rel) },
		NewEdge: scope.NewLineageEdge,
	})
	require.Equal(t, 1, traced)
	require.Empty(t, added, "a query-local relation must be traced, never emitted as an endpoint")
}

func TestResolveAttributesASelectListAliasToItsOwnSources(t *testing.T) {
	t.Parallel()

	sp := scope.NewScope(nil)
	tableInScope(sp, "t", "x")
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:   "total",
		Sources: scope.NewColumnSources([]scope.ColumnRef{{Table: "t", Column: "x"}}, nil),
	})

	influences := NewInfluences()
	influences.Resolve(sp, scope.ColumnRef{Column: "total"}, model.NewFilterTransformation("total > 1"), true)

	var edges []model.ColumnRelation
	influences.Emit(sp, "", "__result__", true, Emitter{
		Trace: func(*scope.TableRef, string, string, string, string, []model.Transformation) {
			t.Fatal("unexpected trace")
		},
		AddEdge: func(rel model.ColumnRelation) { edges = append(edges, rel) },
		NewEdge: scope.NewLineageEdge,
	})
	require.Len(t, edges, 1)
	require.Equal(t, "x", edges[0].Source.Name, "HAVING over an alias influences the columns behind it")
}

func TestMergeSetOpColumnsKeepsEachArmsOwnExpression(t *testing.T) {
	t.Parallel()

	base := scope.NewScope(nil)
	add := model.NewOperatorTransformation("ADDITION", "a + 1")
	addArm := scope.NewScope(nil)
	addArm.AddOutputColumn(scope.OutputColumn{
		Alias:   "x",
		Sources: scope.NewColumnSources([]scope.ColumnRef{{Table: "t1", Column: "a"}}, []model.Transformation{add}),
	})
	multiply := model.NewOperatorTransformation("MULTIPLICATION", "b * 2")
	multiplyArm := scope.NewScope(nil)
	multiplyArm.AddOutputColumn(scope.OutputColumn{
		Alias:   "x",
		Sources: scope.NewColumnSources([]scope.ColumnRef{{Table: "t2", Column: "b"}}, []model.Transformation{multiply}),
	})

	// The base scope holds the first arm's columns: the arm is analyzed there.
	union := model.NewUnionTransformation()
	for _, column := range addArm.GetOutputColumns() {
		base.AddOutputColumn(column)
	}
	MergeSetOpColumns(base,
		[][]scope.OutputColumn{addArm.GetOutputColumns(), multiplyArm.GetOutputColumns()},
		[][]model.Transformation{{union}, {union}},
	)

	merged := base.GetOutputColumns()
	require.Len(t, merged, 1)
	require.Len(t, merged[0].Sources, 2)
	require.Equal(t, "a", merged[0].Sources[0].Ref.Column)
	require.Equal(t, "a + 1", merged[0].Sources[0].Transform[1].Expression)
	require.Equal(t, model.OperationUnion, merged[0].Sources[0].Transform[0].Operation)
	require.Equal(t, "b", merged[0].Sources[1].Ref.Column)
	require.Equal(t, "b * 2", merged[0].Sources[1].Transform[1].Expression,
		"the second arm's expression was replaced by the first arm's")
}

func TestMergeSetOpColumnsIgnoresArmsThatExposeFewerColumns(t *testing.T) {
	t.Parallel()

	base := scope.NewScope(nil)
	wide := scope.NewScope(nil)
	wide.AddOutputColumn(scope.OutputColumn{Alias: "x", Sources: scope.NewColumnSources([]scope.ColumnRef{{Table: "t1", Column: "a"}}, nil)})
	wide.AddOutputColumn(scope.OutputColumn{Alias: "y", Sources: scope.NewColumnSources([]scope.ColumnRef{{Table: "t1", Column: "b"}}, nil)})
	narrow := scope.NewScope(nil)
	narrow.AddOutputColumn(scope.OutputColumn{Alias: "x", Sources: scope.NewColumnSources([]scope.ColumnRef{{Table: "t2", Column: "a"}}, nil)})

	for _, column := range wide.GetOutputColumns() {
		base.AddOutputColumn(column)
	}
	MergeSetOpColumns(base,
		[][]scope.OutputColumn{wide.GetOutputColumns(), narrow.GetOutputColumns()},
		[][]model.Transformation{{model.NewUnionTransformation()}, {model.NewUnionTransformation()}},
	)

	merged := base.GetOutputColumns()
	require.Len(t, merged, 2)
	require.Len(t, merged[0].Sources, 2)
	require.Len(t, merged[1].Sources, 1, "the narrow arm contributes nothing to the column it does not expose")
}

func TestArmChainKeepsNestedOperationsOutermostFirst(t *testing.T) {
	t.Parallel()

	union := model.NewUnionTransformation()
	intersect := model.NewIntersectTransformation()
	except := model.NewExceptTransformation()

	require.Len(t, ArmChain(nil, union), 1)
	// A left-deep tree repeats the same operation, and one union of three arms
	// is not a stack of two.
	require.Len(t, ArmChain([]model.Transformation{union}, union), 1)
	chain := ArmChain(ArmChain(nil, union), intersect)
	require.Len(t, chain, 2)
	require.Equal(t, model.OperationUnion, chain[0].Operation)
	require.Equal(t, model.OperationIntersect, chain[1].Operation)

	// The chain an arm carries describes the operation that produced the merged
	// result, which is the outermost one.
	require.Equal(t, model.RelationTypeUnion, model.RelationTypeOf(ArmChain(chain, except)))
}
