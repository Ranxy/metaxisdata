package catalog

import (
	"context"
	"sync"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// MemoryCatalogProvide is an in-memory implementation of the CatalogProvide interface used for testing.
type MemoryCatalogProvide struct {
	lock   sync.Mutex
	tables map[model.ObjectIdentifier]*TableMeta
}

func NewMemoryCatalogProvide() *MemoryCatalogProvide {
	return &MemoryCatalogProvide{
		lock:   sync.Mutex{},
		tables: make(map[model.ObjectIdentifier]*TableMeta),
	}
}

func (p *MemoryCatalogProvide) GetTable(_ context.Context, id model.ObjectIdentifier) (*TableMeta, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	table, ok := p.tables[id]
	if !ok {
		return nil, nil
	}
	return table, nil
}

func (p *MemoryCatalogProvide) AddTable(table *TableMeta) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.tables[table.ID] = table
}
