package catalog

import (
	"context"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type analysisContextKey struct{}

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

type TableMeta struct {
	ID      model.ObjectIdentifier
	Columns []ColumnMeta
}
type ColumnMeta struct {
	Name     string
	Type     string
	Nullable bool
}

func NewCatalogProvide(store *store.Store) Provide {
	return &provideImpl{
		store: store,
	}
}

type provideImpl struct {
	store *store.Store
}

func (p *provideImpl) GetTable(ctx context.Context, id model.ObjectIdentifier) (*TableMeta, error) {
	// Fill missing parts from AnalysisContext so unqualified names resolve correctly.
	if ac, ok := GetAnalysisContext(ctx); ok {
		id = ac.Complete(id)
	}

	guid := id.GUID()
	res, err := p.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &guid})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	columns, known := outputColumns(res.Metadata)
	if !known {
		return nil, nil
	}
	return &TableMeta{ID: id, Columns: columns}, nil
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
