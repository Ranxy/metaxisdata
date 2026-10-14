package catalog

import (
	"context"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type analysisContextKey struct{}

type identifierFoldingKey struct{}

// WithIdentifierFolding records that the analysis's engine binds an identifier by its
// content rather than by its bytes, so a name the registry registered as `Users` is the
// object a statement named `users`. A dialect sets it for the engines whose identifiers
// are case-insensitive — T-SQL under its default collation is the one this build has —
// and every layer below obeys the same fact: the scope resolver binds references with it,
// and GetTable may fall back to a comparison the engine would make.
//
// It is never set by a caller of the lineage API: the engine is the fact, and the dialect
// is the one place that knows it. An engine whose identifiers are case-sensitive —
// MySQL on a case-sensitive filesystem, PostgreSQL's quoted aliases — must not set it.
func WithIdentifierFolding(ctx context.Context) context.Context {
	return context.WithValue(ctx, identifierFoldingKey{}, true)
}

// IdentifierFolding reports whether the analysis's engine binds identifiers
// case-insensitively. The zero value, false, is the strict byte comparison.
func IdentifierFolding(ctx context.Context) bool {
	fold, _ := ctx.Value(identifierFoldingKey{}).(bool)
	return fold
}

// AnalysisContext carries the default instance/database/schema for unqualified
// object identifiers produced during SQL lineage analysis.
type AnalysisContext struct {
	InstanceID string
	Database   string
	Schema     string
}

// WithAnalysisContext attaches ac to ctx so that GetTable can resolve
// unqualified table names against the correct instance/database/schema.
func WithAnalysisContext(ctx context.Context, ac AnalysisContext) context.Context {
	return context.WithValue(ctx, analysisContextKey{}, ac)
}

// GetAnalysisContext retrieves the AnalysisContext from ctx.
// The second return value is false when no context has been attached.
func GetAnalysisContext(ctx context.Context) (AnalysisContext, bool) {
	v, ok := ctx.Value(analysisContextKey{}).(AnalysisContext)
	return v, ok
}

// Complete fills the missing parts of an identifier from the context. A SQL
// statement only ever names an object down to database/schema level, so the
// instance — and, for unqualified names, the database and schema — can only
// come from here. Both the lineage runner and the stateless AnalyzeSQL path use
// this, which is what keeps their results identical.
func (ac AnalysisContext) Complete(id model.ObjectIdentifier) model.ObjectIdentifier {
	if id.InstanceID == "" {
		id.InstanceID = ac.InstanceID
	}
	if id.Database == "" {
		id.Database = ac.Database
	}
	if id.Schema == "" {
		id.Schema = ac.Schema
	}
	return id
}

type Provide interface {
	GetTable(ctx context.Context, id model.ObjectIdentifier) (*TableMeta, error)
}

// TableMeta is what the registry knows about a relation. ID is the identity the
// registry registered, which is the spelling it holds the object under: a lookup that
// had to fold the name answers with the registered one, so a caller records the object
// the registry has rather than the name the statement wrote.
type TableMeta struct {
	ID      model.ObjectIdentifier
	Columns []ColumnMeta
}
type ColumnMeta struct {
	Name     string
	Type     string
	Nullable bool
}

// metaRegistryReader is the part of the store this package reads. It is an interface so
// that the lookup decisions — an exact miss followed by a folded one, and nothing else —
// are testable without a database.
type metaRegistryReader interface {
	GetMetaRegistry(ctx context.Context, find *store.FindMetaRegistryResourceMessage) (*store.MetaRegistryResource, error)
}

func NewCatalogProvide(store *store.Store) Provide {
	return &provideImpl{
		store: store,
	}
}

type provideImpl struct {
	store metaRegistryReader
}

// GetTable returns the metadata for id, consulting the wrapped provider at most once per
// identifier. A nil result with a nil error means the registry does not know the object,
// which is a normal answer.
//
// An engine that binds identifiers case-insensitively gets a second chance: the name the
// statement wrote may differ from the registered spelling in case alone, and the exact
// probe cannot see through that. The fallback runs only after an exact miss, so an object
// the registry really holds under two spellings settles on the exact one, and a registry
// that answers with neither (impossible for a statement that ran, but a stored definition
// outlives the collation it was written under) reports ambiguity instead of a guess.
func (p *provideImpl) GetTable(ctx context.Context, id model.ObjectIdentifier) (*TableMeta, error) {
	// Fill missing parts from AnalysisContext so unqualified names resolve correctly.
	if ac, ok := GetAnalysisContext(ctx); ok {
		id = ac.Complete(id)
	}

	res, err := p.lookup(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if res == nil && IdentifierFolding(ctx) {
		res, err = p.lookup(ctx, id, true)
		if err != nil {
			return nil, err
		}
	}
	if res == nil {
		return nil, nil
	}
	columns, known := outputColumns(res.Metadata)
	if !known {
		return nil, nil
	}
	// The registered identity, not the requested one. They are the same identifier when
	// the exact probe answered, so only a folded lookup can tell the two apart — and that
	// is exactly the case where the difference is the point.
	identity, ok := model.GUIDToObjectIdentifier(res.GUID)
	if !ok {
		identity = id
	}
	return &TableMeta{ID: identity, Columns: columns}, nil
}

// lookup asks the registry for one identifier, by its bytes or by a comparison that
// ignores case. A folded lookup folds both sides in the database, so the collation that
// decides which characters fold is the database's, not this process's.
func (p *provideImpl) lookup(ctx context.Context, id model.ObjectIdentifier, folded bool) (*store.MetaRegistryResource, error) {
	guid := id.GUID()
	find := &store.FindMetaRegistryResourceMessage{GUID: &guid}
	if folded {
		find = &store.FindMetaRegistryResourceMessage{GUIDFolded: &guid}
	}
	return p.store.GetMetaRegistry(ctx, find)
}

// outputColumns returns the columns a registered relation exposes to a
// wildcard. Tables, foreign tables, views and materialized views all keep their
// column list inside their own metadata and have no COLUMN registry rows. The
// second return value reports whether the object type has a column list at all;
// a type without one must stay unknown so that callers fall back to a bulk
// wildcard edge instead of expanding to nothing.
func outputColumns(meta *storepb.StoredMetadata) ([]ColumnMeta, bool) {
	var stored []*storepb.ColumnMetadata
	switch {
	case meta.GetTableMetadata() != nil:
		stored = meta.GetTableMetadata().GetColumns()
	case meta.GetExternalTableMetadata() != nil:
		stored = meta.GetExternalTableMetadata().GetColumns()
	case meta.GetViewMetadata() != nil:
		stored = meta.GetViewMetadata().GetColumns()
	case meta.GetMaterializedViewMetadata() != nil:
		// A materialized view synced before its own column list was synced has
		// no columns stored. Treat that as unknown, not as empty, so an
		// instance that has not re-synced yet keeps the wildcard fallback.
		if len(meta.GetMaterializedViewMetadata().GetColumns()) == 0 {
			return nil, false
		}
		stored = meta.GetMaterializedViewMetadata().GetColumns()
	default:
		return nil, false
	}
	columns := make([]ColumnMeta, 0, len(stored))
	for _, column := range stored {
		columns = append(columns, ColumnMeta{
			Name:     column.GetName(),
			Type:     column.GetType(),
			Nullable: column.GetNullable(),
		})
	}
	return columns, true
}
