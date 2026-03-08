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
		if id.InstanceID == "" {
			id.InstanceID = ac.InstanceID
		}
		if id.Database == "" {
			id.Database = ac.Database
		}
		if id.Schema == "" {
			id.Schema = ac.Schema
		}
	}

	guid := id.GUID()
	res, err := p.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &guid})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	tableMeta := &TableMeta{
		ID:      id,
		Columns: []ColumnMeta{},
	}
	switch res.ObjectType {
	case storepb.MetaType_TABLE:

		for _, col := range res.Metadata.GetTableMetadata().Columns {
			tableMeta.Columns = append(tableMeta.Columns, ColumnMeta{
				Name:     col.Name,
				Type:     col.Type,
				Nullable: col.Nullable,
			})
		}
		return tableMeta, nil
	case storepb.MetaType_VIEW:
		for _, col := range res.Metadata.GetViewMetadata().Columns {
			tableMeta.Columns = append(tableMeta.Columns, ColumnMeta{
				Name:     col.Name,
				Type:     col.Type,
				Nullable: col.Nullable,
			})
		}
		return tableMeta, nil

	case storepb.MetaType_MATERIALIZED_VIEW:
		// todo: fill real type and nullable info
		for _, col := range res.Metadata.GetMaterializedViewMetadata().DependencyColumns {
			tableMeta.Columns = append(tableMeta.Columns, ColumnMeta{
				Name:     col.Column,
				Type:     "",
				Nullable: false,
			})
		}
		return tableMeta, nil
	default:
		return nil, nil
	}
}
