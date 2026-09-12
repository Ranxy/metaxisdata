package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Every list method shares one page-token format. These cases pin the size
// resolution: the token carries the size it was issued with, so a follow-up
// request that omits page_size must not silently re-default it (which used to
// overlap or skip rows).
func TestParseLimitAndOffset(t *testing.T) {
	t.Parallel()

	tokenWithLimit := func(limit, offset int32) string {
		encoded, err := marshalPageToken(&storepb.PageToken{Limit: limit, Offset: offset})
		require.NoError(t, err)
		return encoded
	}

	t.Run("no token uses the requested size", func(t *testing.T) {
		t.Parallel()
		offset, err := parseLimitAndOffset(&pageSize{limit: 25, maximum: 1000})
		require.NoError(t, err)
		require.Equal(t, 25, offset.limit)
		require.Equal(t, 0, offset.offset)
	})

	t.Run("empty request falls back to the default", func(t *testing.T) {
		t.Parallel()
		offset, err := parseLimitAndOffset(&pageSize{maximum: 1000})
		require.NoError(t, err)
		require.Equal(t, 10, offset.limit)
	})

	t.Run("size above the maximum is clamped", func(t *testing.T) {
		t.Parallel()
		offset, err := parseLimitAndOffset(&pageSize{limit: 5000, maximum: 1000})
		require.NoError(t, err)
		require.Equal(t, 1000, offset.limit)
	})

	t.Run("token keeps the original size when page_size is omitted", func(t *testing.T) {
		t.Parallel()
		offset, err := parseLimitAndOffset(&pageSize{token: tokenWithLimit(25, 50), maximum: 1000})
		require.NoError(t, err)
		require.Equal(t, 25, offset.limit)
		require.Equal(t, 50, offset.offset)
	})

	t.Run("explicit page_size wins over the token", func(t *testing.T) {
		t.Parallel()
		offset, err := parseLimitAndOffset(&pageSize{token: tokenWithLimit(25, 50), limit: 100, maximum: 1000})
		require.NoError(t, err)
		require.Equal(t, 100, offset.limit)
		require.Equal(t, 50, offset.offset)
	})

	t.Run("negative token limit is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := parseLimitAndOffset(&pageSize{token: tokenWithLimit(-1, 0), maximum: 1000})
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("negative token offset is clamped", func(t *testing.T) {
		t.Parallel()
		offset, err := parseLimitAndOffset(&pageSize{token: tokenWithLimit(25, -5), maximum: 1000})
		require.NoError(t, err)
		require.Equal(t, 0, offset.offset)
	})

	t.Run("garbage token is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := parseLimitAndOffset(&pageSize{token: "not-base64!!", maximum: 1000})
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})
}

func TestPaginate(t *testing.T) {
	t.Parallel()

	t.Run("short page has no next token", func(t *testing.T) {
		t.Parallel()
		page, token, err := paginate([]int{1, 2, 3}, &pageOffset{limit: 10})
		require.NoError(t, err)
		require.Equal(t, []int{1, 2, 3}, page)
		require.Empty(t, token)
	})

	t.Run("overflowing probe is trimmed and yields a token", func(t *testing.T) {
		t.Parallel()
		page, token, err := paginate([]int{1, 2, 3, 4}, &pageOffset{limit: 3})
		require.NoError(t, err)
		require.Equal(t, []int{1, 2, 3}, page)
		require.NotEmpty(t, token)

		next := &storepb.PageToken{}
		require.NoError(t, unmarshalPageToken(token, next))
		require.Equal(t, int32(3), next.Limit)
		require.Equal(t, int32(3), next.Offset)
	})

	t.Run("exact page without overflow has no next token", func(t *testing.T) {
		t.Parallel()
		// The caller probes limit+1 rows, so exactly `limit` rows means the end.
		page, token, err := paginate([]int{1, 2, 3}, &pageOffset{limit: 3})
		require.NoError(t, err)
		require.Equal(t, []int{1, 2, 3}, page)
		require.Empty(t, token)
	})

	t.Run("token chains offsets", func(t *testing.T) {
		t.Parallel()
		first, token, err := paginate([]int{1, 2, 3}, &pageOffset{limit: 2})
		require.NoError(t, err)
		require.Equal(t, []int{1, 2}, first)

		decoded := &storepb.PageToken{}
		require.NoError(t, unmarshalPageToken(token, decoded))

		second, token, err := paginate([]int{3, 4, 5}, &pageOffset{limit: int(decoded.Limit), offset: int(decoded.Offset)})
		require.NoError(t, err)
		require.Equal(t, []int{3, 4}, second)
		require.NotEmpty(t, token)
	})
}

// The lineage endpoints default to a page large enough that the graph UI gets
// the whole list in one request, unlike the shared 10-row default.
func TestLineagePageOffset(t *testing.T) {
	t.Parallel()

	t.Run("empty request uses the lineage default", func(t *testing.T) {
		t.Parallel()
		offset, err := lineagePageOffset(0, "")
		require.NoError(t, err)
		require.Equal(t, defaultLineagePageSize, offset.limit)
		require.Equal(t, 0, offset.offset)
	})

	t.Run("requested size is honoured and clamped to the maximum", func(t *testing.T) {
		t.Parallel()
		offset, err := lineagePageOffset(50, "")
		require.NoError(t, err)
		require.Equal(t, 50, offset.limit)

		offset, err = lineagePageOffset(maxLineagePageSize+1, "")
		require.NoError(t, err)
		require.Equal(t, maxLineagePageSize, offset.limit)
	})

	t.Run("follow-up request without page_size keeps the issued size", func(t *testing.T) {
		t.Parallel()
		token, err := marshalPageToken(&storepb.PageToken{Limit: 50, Offset: 100})
		require.NoError(t, err)

		offset, err := lineagePageOffset(0, token)
		require.NoError(t, err)
		require.Equal(t, 50, offset.limit)
		require.Equal(t, 100, offset.offset)
	})

	t.Run("invalid token is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := lineagePageOffset(0, "not-a-token")
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})
}
