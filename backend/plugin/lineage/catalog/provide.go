package catalog

import (
	"context"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

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
