package qualityrunner

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The personal-data label read is cached per target: a batch runs every check of a
// target together, so the labels must cost one read, not one per execution. The
// same GUID can name a table and a view, so the cache keys on both; a failed read is
// not cached, so a later check of the same target retries and the caller can still
// fail safe on this execution.
func TestBatchPersonalColumnsReadsEachTargetOnce(t *testing.T) {
	t.Parallel()

	type readCall struct {
		guid     string
		metaType storepb.MetaType
	}
	var reads []readCall
	brokenCalls := 0
	cache := newBatchPersonalColumns(func(_ context.Context, targetGUID string, metaType storepb.MetaType) ([]string, error) {
		reads = append(reads, readCall{guid: targetGUID, metaType: metaType})
		switch targetGUID {
		case "orders":
			return []string{"id_card"}, nil
		case "broken":
			brokenCalls++
			if brokenCalls == 1 {
				return nil, errors.New("store unavailable")
			}
			return []string{"id_card"}, nil
		default:
			return nil, nil
		}
	})

	first, err := cache.forTarget(context.Background(), "orders", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, []string{"id_card"}, first)
	second, err := cache.forTarget(context.Background(), "orders", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Len(t, reads, 1, "a second check on the same target reuses the cached labels")

	empty, err := cache.forTarget(context.Background(), "customers", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Empty(t, empty)
	_, err = cache.forTarget(context.Background(), "customers", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Len(t, reads, 2, "a target without labels is read once")

	_, err = cache.forTarget(context.Background(), "shared", storepb.MetaType_TABLE)
	require.NoError(t, err)
	_, err = cache.forTarget(context.Background(), "shared", storepb.MetaType_VIEW)
	require.NoError(t, err)
	require.Len(t, reads, 4, "the GUID alone is not the identity")
	require.Equal(t, readCall{guid: "shared", metaType: storepb.MetaType_VIEW}, reads[3],
		"the object type is passed to the read")

	_, err = cache.forTarget(context.Background(), "broken", storepb.MetaType_TABLE)
	require.Error(t, err)
	columns, err := cache.forTarget(context.Background(), "broken", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, []string{"id_card"}, columns)
	require.Len(t, reads, 6, "a failed read is retried instead of cached")
}
