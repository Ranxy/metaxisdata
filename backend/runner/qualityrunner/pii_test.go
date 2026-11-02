package qualityrunner

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The personal-data label read is cached per target: a batch runs every check of a
// target together, so the labels must cost one read, not one per execution.
func TestBatchPersonalColumnsReadsOncePerTarget(t *testing.T) {
	t.Parallel()

	calls := 0
	cache := newBatchPersonalColumns(func(_ context.Context, targetGUID string, metaType storepb.MetaType) ([]string, error) {
		calls++
		require.Equal(t, storepb.MetaType_TABLE, metaType)
		if targetGUID == "orders" {
			return []string{"id_card"}, nil
		}
		return nil, nil
	})

	first, err := cache.forTarget(context.Background(), "orders", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, []string{"id_card"}, first)
	second, err := cache.forTarget(context.Background(), "orders", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, calls, "a second check on the same target must reuse the cached labels")

	empty, err := cache.forTarget(context.Background(), "customers", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Equal(t, 2, calls, "a different target is one more read")
}

// The same GUID can name a table and a view; the label cache keys on both.
func TestBatchPersonalColumnsKeysOnMetaType(t *testing.T) {
	t.Parallel()

	calls := 0
	cache := newBatchPersonalColumns(func(_ context.Context, _ string, _ storepb.MetaType) ([]string, error) {
		calls++
		return nil, nil
	})

	_, err := cache.forTarget(context.Background(), "shared", storepb.MetaType_TABLE)
	require.NoError(t, err)
	_, err = cache.forTarget(context.Background(), "shared", storepb.MetaType_VIEW)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

// A read failure is not cached, so a later check of the same target retries and the
// caller can fail safe on this execution.
func TestBatchPersonalColumnsDoesNotCacheErrors(t *testing.T) {
	t.Parallel()

	calls := 0
	cache := newBatchPersonalColumns(func(_ context.Context, _ string, _ storepb.MetaType) ([]string, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("store unavailable")
		}
		return []string{"id_card"}, nil
	})

	_, err := cache.forTarget(context.Background(), "orders", storepb.MetaType_TABLE)
	require.Error(t, err)
	columns, err := cache.forTarget(context.Background(), "orders", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, []string{"id_card"}, columns)
	require.Equal(t, 2, calls)
}
