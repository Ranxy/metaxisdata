package testutil

import (
	"context"
	"sync"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// MemoryCatalogProvide is an in-memory catalog.Provide used by the lineage test
// harness. It lives here rather than in the catalog package because a normal
// source file beside the real provider is compiled into the server binary: only
// test files import testutil, so this one is not.
type MemoryCatalogProvide struct {
	lock   sync.Mutex
	calls  int
	tables map[model.ObjectIdentifier]*catalog.TableMeta
}

// NewMemoryCatalogProvide creates an empty in-memory catalog.
func NewMemoryCatalogProvide() *MemoryCatalogProvide {
	return &MemoryCatalogProvide{tables: make(map[model.ObjectIdentifier]*catalog.TableMeta)}
}

func (p *MemoryCatalogProvide) GetTable(_ context.Context, id model.ObjectIdentifier) (*catalog.TableMeta, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.calls++
	return p.tables[id], nil
}

// AddTable registers a table's metadata.
func (p *MemoryCatalogProvide) AddTable(table *catalog.TableMeta) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.tables[table.ID] = table
}

// Calls returns how many lookups reached the catalog, which lets a test pin that
// one analysis reads a relation once however many times it is named.
func (p *MemoryCatalogProvide) Calls() int {
	p.lock.Lock()
	defer p.lock.Unlock()
	return p.calls
}
