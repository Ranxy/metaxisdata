package testutil

import (
	"context"
	"strings"
	"sync"

	"github.com/pkg/errors"

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

func (p *MemoryCatalogProvide) GetTable(ctx context.Context, id model.ObjectIdentifier) (*catalog.TableMeta, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.calls++
	if table, ok := p.tables[id]; ok {
		return table, nil
	}
	// An engine that binds identifiers case-insensitively gets the same second chance the
	// real provider gives it, and is answered with the entry it stored, so a test observes
	// the registered spelling rather than the one it asked with. A double that only ever
	// matched bytes would let a test pin an answer production never gives.
	if !catalog.IdentifierFolding(ctx) {
		return nil, nil
	}
	var (
		found *catalog.TableMeta
		count int
	)
	for _, table := range p.tables {
		if !sameIdentifierIgnoringCase(table.ID, id) {
			continue
		}
		count++
		found = table
	}
	if count > 1 {
		// The real lookup refuses to choose between two objects one fold matches.
		return nil, errors.New("found multiple meta registry with the same criteria")
	}
	return found, nil
}

// sameIdentifierIgnoringCase reports whether two identifiers name the same object under an
// engine that binds an identifier by its content rather than by its bytes. The registry
// folds the whole GUID, which folds every segment of it, so every part is compared here.
func sameIdentifierIgnoringCase(a, b model.ObjectIdentifier) bool {
	return strings.EqualFold(a.InstanceID, b.InstanceID) &&
		strings.EqualFold(a.Database, b.Database) &&
		strings.EqualFold(a.Schema, b.Schema) &&
		strings.EqualFold(a.Name, b.Name)
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
