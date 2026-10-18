package schemasync

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestMetaObjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		objectType storepb.MetaType
		metadata   *storepb.StoredMetadata
		want       string
		wantErr    bool
	}{
		{
			name:       "database",
			objectType: storepb.MetaType_DATABASE,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: &storepb.DatabaseSchemaMetadata{Name: "db1"}}},
			want:       "db1",
		},
		{
			name:       "schema",
			objectType: storepb.MetaType_SCHEMA,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_SchemaMetadata{SchemaMetadata: &storepb.SchemaMetadata{Name: "s1"}}},
			want:       "s1",
		},
		{
			name:       "table",
			objectType: storepb.MetaType_TABLE,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{Name: "t1"}}},
			want:       "t1",
		},
		{
			name:       "view",
			objectType: storepb.MetaType_VIEW,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Name: "v1"}}},
			want:       "v1",
		},
		{
			name:       "materialized view",
			objectType: storepb.MetaType_MATERIALIZED_VIEW,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{Name: "mv1"}}},
			want:       "mv1",
		},
		{
			name:       "function",
			objectType: storepb.MetaType_FUNCTION,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_FunctionMetadata{FunctionMetadata: &storepb.FunctionMetadata{Name: "f1"}}},
			want:       "f1",
		},
		{
			name:       "procedure",
			objectType: storepb.MetaType_PROCEDURE,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ProcedureMetadata{ProcedureMetadata: &storepb.ProcedureMetadata{Name: "p1"}}},
			want:       "p1",
		},
		{
			name:       "sequence",
			objectType: storepb.MetaType_SEQUENCE,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_SequenceMetadata{SequenceMetadata: &storepb.SequenceMetadata{Name: "seq1"}}},
			want:       "seq1",
		},
		{
			name:       "external table",
			objectType: storepb.MetaType_EXTERNAL_TABLE,
			metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ExternalTableMetadata{ExternalTableMetadata: &storepb.ExternalTableMetadata{Name: "e1"}}},
			want:       "e1",
		},
		{
			name:       "unsupported type",
			objectType: storepb.MetaType_COLUMN,
			metadata:   &storepb.StoredMetadata{},
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := metaObjectName(tc.objectType, tc.metadata)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSupportsObjectDefinition(t *testing.T) {
	t.Parallel()

	for _, objectType := range []storepb.MetaType{
		storepb.MetaType_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW,
		storepb.MetaType_FUNCTION,
		storepb.MetaType_PROCEDURE,
	} {
		require.True(t, supportsObjectDefinition(objectType), objectType.String())
	}
	// A column has no definition of its own, and the database/schema rows carry
	// attributes rather than DDL.
	for _, objectType := range []storepb.MetaType{
		storepb.MetaType_UNSPECIFIED,
		storepb.MetaType_DATABASE,
		storepb.MetaType_SCHEMA,
		storepb.MetaType_COLUMN,
		storepb.MetaType_SEQUENCE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_MANUAL_SQL,
	} {
		require.False(t, supportsObjectDefinition(objectType), objectType.String())
	}
}

// A full fetch covers every schema-bearing object, and objects with no
// definition of their own are never candidates.
func TestPlanDefinitionFetchFiltersNonDefinitionTypes(t *testing.T) {
	t.Parallel()

	bmc := &batchMetaCreate{
		guidList: []*store.CreateMetaRegistryResourceMessage{
			tableResource("t1"),
			viewResource("v1"),
			materializedViewResource("mv1"),
			columnResource("c1"),
			databaseResource(),
			schemaResource(),
		},
	}

	plan := bmc.planDefinitionFetch(true, 0)
	require.True(t, plan.full)
	require.False(t, plan.rotating)
	require.Len(t, plan.items, 3)
	types := make(map[storepb.MetaType]int)
	for _, item := range plan.items {
		types[item.ObjectType]++
	}
	require.Equal(t, map[storepb.MetaType]int{
		storepb.MetaType_TABLE:             1,
		storepb.MetaType_VIEW:              1,
		storepb.MetaType_MATERIALIZED_VIEW: 1,
	}, types)
}

// Between full refreshes only the objects whose metadata changed are re-read, so
// the per-sync SHOW CREATE cost follows the change rather than the database size.
func TestPlanDefinitionFetchChangeOnlyBetweenRefreshes(t *testing.T) {
	t.Parallel()

	unchanged := tableResource("unchanged")
	changed := tableResource("changed")
	bmc := &batchMetaCreate{
		guidList: []*store.CreateMetaRegistryResourceMessage{unchanged, changed, viewResource("v1")},
		updates: []*store.CreateMetaRegistryResourceMessage{
			changed,
			// A changed object without its own definition is not a candidate.
			columnResource("c1"),
		},
	}

	plan := bmc.planDefinitionFetch(false, 0)
	require.False(t, plan.full)
	require.False(t, plan.rotating)
	require.Len(t, plan.items, 1)
	require.Equal(t, "changed", plan.items[0].Metadata.GetTableMetadata().GetName())
}

// A database above the limit refreshes one rotating window per full refresh
// instead of every object, and a changed object outside the window is still read
// so a real DDL change never waits for the rotation to reach it.
func TestPlanDefinitionFetchRotatesAboveLimit(t *testing.T) {
	t.Parallel()

	guidList := make([]*store.CreateMetaRegistryResourceMessage, 0, fullDefinitionObjectLimit+1)
	for i := 0; i < fullDefinitionObjectLimit; i++ {
		guidList = append(guidList, tableResource(fmt.Sprintf("t%04d", i)))
	}
	bmc := &batchMetaCreate{guidList: guidList}
	// At the limit the refresh is still full.
	atLimit := bmc.planDefinitionFetch(true, 0)
	require.False(t, atLimit.rotating)
	require.Len(t, atLimit.items, fullDefinitionObjectLimit)

	// One more object pushes the database over the limit.
	changed := tableResource("zzz_changed")
	bmc.guidList = append(bmc.guidList, changed)
	bmc.updates = []*store.CreateMetaRegistryResourceMessage{changed}
	first := bmc.planDefinitionFetch(true, 0)
	require.True(t, first.rotating)
	// The window is the fetch limit, plus the changed object it does not cover.
	require.Len(t, first.items, fullDefinitionObjectLimit+1)
	require.Equal(t, fullDefinitionObjectLimit, first.nextCursor)

	// The next full refresh continues where the first stopped; the sorted GUIDs
	// put zzz_changed in the first window, so the second window starts after it.
	second := bmc.planDefinitionFetch(true, first.nextCursor)
	require.True(t, second.rotating)
	require.Len(t, second.items, fullDefinitionObjectLimit)
	require.Contains(t, second.items, changed)
	require.NotEqual(t, first.nextCursor, second.nextCursor)
}

// Objects whose fetch failed or whose definition is not valid UTF-8 are excluded
// from the result and counted instead: one bad row would abort the metadata
// transaction, and a transient failure must not be mistaken for "the definition
// is gone".
func TestFetchObjectDefinitionsSkipsFailures(t *testing.T) {
	t.Parallel()

	reader := &fakeDefinitionReader{
		definitions: map[string]string{
			"t1":      "CREATE TABLE `t1` (id int)",
			"empty":   "",
			"bad\xff": "\xff\xfe",
		},
		missing:  map[string]bool{"gone": true},
		failures: map[string]error{"broken": errors.New("permission denied")},
	}

	fetched, failed := fetchObjectDefinitions(context.Background(), reader, []*store.CreateMetaRegistryResourceMessage{
		tableResource("t1"),
		tableResource("gone"),
		tableResource("empty"),
		tableResource("broken"),
		tableResource("bad\xff"),
	})

	require.Equal(t, 2, failed, "broken and non-UTF-8 must be counted, not returned")

	results := make(map[string]fetchedDefinition, len(fetched))
	for _, f := range fetched {
		results[f.item.Metadata.GetTableMetadata().GetName()] = f
	}
	require.Len(t, results, 3)
	require.True(t, results["t1"].ok)
	require.Equal(t, "CREATE TABLE `t1` (id int)", results["t1"].definition)
	require.False(t, results["gone"].ok)
	require.Empty(t, results["empty"].definition)
}

// The write diff: only changed definitions are returned for writing, unchanged
// ones are dropped, and a definition that disappeared drops its row only when
// one exists.
func TestDiffDefinitions(t *testing.T) {
	t.Parallel()

	sameDDL := "CREATE TABLE `same` (id int)"
	changedStored := "CREATE TABLE `changed` (id int)"
	changedFresh := "CREATE TABLE `changed` (id int, name varchar(10))"
	newDDL := "CREATE TABLE `new` (id int)"

	existing := map[store.MetaGUIDKey][]byte{
		tableResource("same").GUIDKey():    hashOf(sameDDL),
		tableResource("changed").GUIDKey(): hashOf(changedStored),
		tableResource("gone").GUIDKey():    hashOf("CREATE TABLE `gone` (id int)"),
	}

	fetched := []fetchedDefinition{
		{item: tableResource("same"), definition: sameDDL, ok: true},
		{item: tableResource("changed"), definition: changedFresh, ok: true},
		{item: tableResource("new"), definition: newDDL, ok: true},
		{item: tableResource("gone"), ok: false},
		// No stored row, so there is nothing to delete.
		{item: tableResource("never-existed"), ok: false},
	}

	upserts, deletes, unchanged := diffDefinitions(fetched, existing)

	require.Equal(t, 1, unchanged)
	upserted := make(map[string]string, len(upserts))
	for _, upsert := range upserts {
		upserted[upsert.GUID] = upsert.Schema
	}
	require.Len(t, upserted, 2)
	require.Equal(t, changedFresh, upserted[tableResource("changed").GUID])
	require.Equal(t, newDDL, upserted[tableResource("new").GUID])

	require.Len(t, deletes, 1)
	require.Equal(t, tableResource("gone").GUID, deletes[0].GUID)
}

func hashOf(definition string) []byte {
	hash := sha256.Sum256([]byte(definition))
	return hash[:]
}

func tableResource(name string) *store.CreateMetaRegistryResourceMessage {
	return registryResource(name, storepb.MetaType_TABLE, &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{Name: name}},
	})
}

func viewResource(name string) *store.CreateMetaRegistryResourceMessage {
	return registryResource(name, storepb.MetaType_VIEW, &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Name: name}},
	})
}

func materializedViewResource(name string) *store.CreateMetaRegistryResourceMessage {
	return registryResource(name, storepb.MetaType_MATERIALIZED_VIEW, &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{Name: name}},
	})
}

func columnResource(name string) *store.CreateMetaRegistryResourceMessage {
	return registryResource(name, storepb.MetaType_COLUMN, &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ColumnMetadata{ColumnMetadata: &storepb.ColumnMetadata{Name: name}},
	})
}

func databaseResource() *store.CreateMetaRegistryResourceMessage {
	return registryResource("db1", storepb.MetaType_DATABASE, &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: &storepb.DatabaseSchemaMetadata{Name: "db1"}},
	})
}

func schemaResource() *store.CreateMetaRegistryResourceMessage {
	return registryResource("", storepb.MetaType_SCHEMA, &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_SchemaMetadata{SchemaMetadata: &storepb.SchemaMetadata{Name: ""}},
	})
}

func registryResource(name string, objectType storepb.MetaType, metadata *storepb.StoredMetadata) *store.CreateMetaRegistryResourceMessage {
	return &store.CreateMetaRegistryResourceMessage{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       "instances/i;databases/d;schemas/;objects/" + name,
			ObjectType: objectType,
			Metadata:   metadata,
		},
	}
}

type fakeDefinitionReader struct {
	definitions map[string]string
	missing     map[string]bool
	failures    map[string]error
}

func (f *fakeDefinitionReader) GetObjectDefinition(_ context.Context, _ storepb.MetaType, _ string, name string) (string, bool, error) {
	if err, ok := f.failures[name]; ok {
		return "", false, err
	}
	if f.missing[name] {
		return "", false, nil
	}
	if definition, ok := f.definitions[name]; ok {
		return definition, true, nil
	}
	return "", false, nil
}

var _ db.ObjectDefinitionReader = (*fakeDefinitionReader)(nil)
