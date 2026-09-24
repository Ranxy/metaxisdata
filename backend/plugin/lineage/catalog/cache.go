package catalog

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// Cache memoizes one analysis's catalog lookups.
//
// It exists because the dialects disagreed about the cost of a lookup:
// PostgreSQL and StarRocks asked the catalog once per reference, so `FROM t a
// JOIN t b` read the same table twice, while the MySQL family asked once.
//
// Memoizing a failure matters as much as memoizing a hit. The catalog is the only
// I/O an analysis performs, and a failure has to be reported once rather than once
// per reference that names the relation.
type Cache struct {
	provide  Provide
	entries  map[model.ObjectIdentifier]*TableMeta
	failures map[model.ObjectIdentifier]error
}

// NewCache wraps provide in a cache for one analysis. It returns nil when there is
// no provider, so a caller can hold the result in the field it already checks for
// nil instead of carrying a provider and a cache side by side.
func NewCache(provide Provide) *Cache {
	if provide == nil {
		return nil
	}
	return &Cache{
		provide:  provide,
		entries:  make(map[model.ObjectIdentifier]*TableMeta),
		failures: make(map[model.ObjectIdentifier]error),
	}
}

// GetTable returns the metadata for id, consulting the wrapped provider at most
// once per identifier. A nil result with a nil error means the registry does not
// know the object, which is a normal answer: it is memoized like a hit so a
// relation missing from the registry does not cost a lookup per reference either.
func (c *Cache) GetTable(ctx context.Context, id model.ObjectIdentifier) (*TableMeta, error) {
	if c == nil {
		return nil, nil
	}
	if meta, ok := c.entries[id]; ok {
		return meta, nil
	}
	if err, ok := c.failures[id]; ok {
		return nil, err
	}
	meta, err := c.provide.GetTable(ctx, id)
	if err != nil {
		c.failures[id] = err
		return nil, err
	}
	c.entries[id] = meta
	return meta, nil
}
