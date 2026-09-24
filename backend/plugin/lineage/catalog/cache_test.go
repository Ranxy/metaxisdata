package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// countingProvide records how many lookups reached it.
type countingProvide struct {
	calls  int
	tables map[model.ObjectIdentifier]*TableMeta
	err    error
}

func (p *countingProvide) GetTable(_ context.Context, id model.ObjectIdentifier) (*TableMeta, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.tables[id], nil
}

func TestCacheReadsEachRelationOnce(t *testing.T) {
	t.Parallel()

	table := &TableMeta{ID: model.ObjectIdentifier{Name: "t"}, Columns: []ColumnMeta{{Name: "a"}}}
	provide := &countingProvide{tables: map[model.ObjectIdentifier]*TableMeta{table.ID: table}}
	cache := NewCache(provide)

	first, err := cache.GetTable(context.Background(), table.ID)
	require.NoError(t, err)
	require.Equal(t, table, first)

	second, err := cache.GetTable(context.Background(), table.ID)
	require.NoError(t, err)
	require.Equal(t, table, second)
	require.Equal(t, 1, provide.calls, "the same identifier was read twice")

	_, err = cache.GetTable(context.Background(), model.ObjectIdentifier{Name: "other"})
	require.NoError(t, err)
	require.Equal(t, 2, provide.calls, "a distinct identifier was not read")
}

func TestCacheReadsAMissOnce(t *testing.T) {
	t.Parallel()

	provide := &countingProvide{}
	cache := NewCache(provide)

	for range 3 {
		meta, err := cache.GetTable(context.Background(), model.ObjectIdentifier{Name: "unknown"})
		require.NoError(t, err)
		require.Nil(t, meta)
	}
	require.Equal(t, 1, provide.calls, "a relation the registry does not know was read more than once")
}

func TestCacheRemembersAFailure(t *testing.T) {
	t.Parallel()

	provide := &countingProvide{err: errors.New("connection reset")}
	cache := NewCache(provide)

	for range 3 {
		_, err := cache.GetTable(context.Background(), model.ObjectIdentifier{Name: "t"})
		require.EqualError(t, err, "connection reset")
	}
	require.Equal(t, 1, provide.calls, "a failed lookup was repeated")
}

func TestCacheWithoutProviderIsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewCache(nil))

	// A nil cache answers "nothing is known" rather than panicking, which is what a
	// caller holding no provider expects.
	var cache *Cache
	meta, err := cache.GetTable(context.Background(), model.ObjectIdentifier{Name: "t"})
	require.NoError(t, err)
	require.Nil(t, meta)
}
