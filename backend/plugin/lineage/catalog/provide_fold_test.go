package catalog

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// recordingReader answers the two probes separately, so a test can tell an exact lookup
// from a folded one and assert that the second only happens when it should.
type recordingReader struct {
	exact map[string]*store.MetaRegistryResource
	// folded is what a folded probe answers, and foldedErr what it reports instead — the
	// store refuses to choose when one fold matches several objects.
	folded    map[string]*store.MetaRegistryResource
	foldedErr error
	calls     []string
}

func (r *recordingReader) GetMetaRegistry(_ context.Context, find *store.FindMetaRegistryResourceMessage) (*store.MetaRegistryResource, error) {
	switch {
	case find.GUID != nil:
		r.calls = append(r.calls, "exact:"+*find.GUID)
		return r.exact[*find.GUID], nil
	case find.GUIDFolded != nil:
		r.calls = append(r.calls, "folded:"+*find.GUIDFolded)
		if r.foldedErr != nil {
			return nil, r.foldedErr
		}
		return r.folded[*find.GUIDFolded], nil
	default:
		return nil, nil
	}
}

func tableRow(guid string, columns ...string) *store.MetaRegistryResource {
	stored := make([]*storepb.ColumnMetadata, 0, len(columns))
	for _, column := range columns {
		stored = append(stored, &storepb.ColumnMetadata{Name: column, Type: "int", Nullable: false})
	}
	return &store.MetaRegistryResource{
		GUID:       guid,
		ObjectType: storepb.MetaType_TABLE,
		Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			Name:    "t",
			Columns: stored,
		}}},
	}
}

// analysisContext is what the runner and AnalyzeSQL attach: a statement only names an
// object down to database/schema level, so the rest comes from here.
func analysisContext() context.Context {
	return WithAnalysisContext(context.Background(), AnalysisContext{InstanceID: "inst", Database: "demo", Schema: "dbo"})
}

// A statement may spell a name in another case than the sync registered it. The lookup
// then has to answer with the registered object, identity included: a caller that records
// the name it asked with would name an object the registry does not have.
func TestGetTableAnswersWithTheRegisteredIdentity(t *testing.T) {
	t.Parallel()

	reader := &recordingReader{
		exact:  map[string]*store.MetaRegistryResource{},
		folded: map[string]*store.MetaRegistryResource{"inst;demo;dbo;users": tableRow("inst;demo;dbo;Users", "ID", "Name")},
	}
	provide := &provideImpl{store: reader}

	meta, err := provide.GetTable(WithIdentifierFolding(analysisContext()), model.ObjectIdentifier{Schema: "dbo", Name: "users"})
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, model.ObjectIdentifier{InstanceID: "inst", Database: "demo", Schema: "dbo", Name: "Users"}, meta.ID)
	require.Equal(t, []ColumnMeta{{Name: "ID", Type: "int"}, {Name: "Name", Type: "int"}}, meta.Columns)
	require.Equal(t, []string{"exact:inst;demo;dbo;users", "folded:inst;demo;dbo;users"}, reader.calls)
}

// An exact answer settles the question, so the folded probe never runs: a case-sensitive
// collation may hold `users` and `Users` as two objects, and the one the statement named
// exactly is the one it meant.
func TestGetTableStopsAtTheExactMatch(t *testing.T) {
	t.Parallel()

	reader := &recordingReader{
		exact:  map[string]*store.MetaRegistryResource{"inst;demo;dbo;users": tableRow("inst;demo;dbo;users", "id")},
		folded: map[string]*store.MetaRegistryResource{"inst;demo;dbo;users": tableRow("inst;demo;dbo;Users", "ID")},
	}
	provide := &provideImpl{store: reader}

	meta, err := provide.GetTable(WithIdentifierFolding(analysisContext()), model.ObjectIdentifier{Schema: "dbo", Name: "users"})
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, model.ObjectIdentifier{InstanceID: "inst", Database: "demo", Schema: "dbo", Name: "users"}, meta.ID)
	require.Equal(t, []ColumnMeta{{Name: "id", Type: "int"}}, meta.Columns)
	require.Equal(t, []string{"exact:inst;demo;dbo;users"}, reader.calls)
}

// Folding is the engine's rule, not a default: without the dialect declaring it the
// catalog compares bytes, which is what every other engine needs.
func TestGetTableDoesNotFoldWithoutTheDialect(t *testing.T) {
	t.Parallel()

	reader := &recordingReader{
		exact:  map[string]*store.MetaRegistryResource{},
		folded: map[string]*store.MetaRegistryResource{"inst;demo;dbo;users": tableRow("inst;demo;dbo;Users")},
	}
	provide := &provideImpl{store: reader}

	meta, err := provide.GetTable(analysisContext(), model.ObjectIdentifier{Schema: "dbo", Name: "users"})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.Equal(t, []string{"exact:inst;demo;dbo;users"}, reader.calls)
}

// A folded probe that matches more than one object is an error rather than a pick: the
// caller has no way to tell which of them the statement meant, and a wrong edge is worse
// than a reported gap.
func TestGetTableReportsAnAmbiguousFoldedName(t *testing.T) {
	t.Parallel()

	reader := &recordingReader{
		exact:     map[string]*store.MetaRegistryResource{},
		folded:    map[string]*store.MetaRegistryResource{},
		foldedErr: errors.New("found multiple meta registry with the same criteria"),
	}
	provide := &provideImpl{store: reader}

	meta, err := provide.GetTable(WithIdentifierFolding(analysisContext()), model.ObjectIdentifier{Schema: "dbo", Name: "users"})
	require.Error(t, err)
	require.Nil(t, meta)
	require.Equal(t, []string{"exact:inst;demo;dbo;users", "folded:inst;demo;dbo;users"}, reader.calls)
}
