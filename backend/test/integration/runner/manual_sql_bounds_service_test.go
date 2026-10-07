//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// TestManualSQLCollectionsAreCountBoundedRealServerIntegration pins the cap on a
// real server. Each tag and each attribute becomes its own row and the write
// permission is in the member baseline, so before the cap a single 4 MiB request
// was the only limit on how many side rows one member could write. The parent
// names an instance that does not have to exist: the count is checked before
// anything is resolved or stored.
func TestManualSQLCollectionsAreCountBoundedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	client := v1connect.NewDatabaseServiceClient(&http.Client{Timeout: 10 * time.Second}, env.BaseURL)
	ctx := context.Background()

	// The limits are the api package's constants, repeated here so a silent change
	// to them shows up as a failing test rather than passing either way.
	const maxTags = 100
	const maxAttributes = 100

	parent := fmt.Sprintf("instances/it-collection-bounds-%d/databases/db", time.Now().UnixNano())
	tags := make([]string, maxTags+1)
	for i := range tags {
		tags[i] = fmt.Sprintf("tag-%d", i)
	}
	attributes := make(map[string]string, maxAttributes+1)
	for i := range maxAttributes + 1 {
		attributes[fmt.Sprintf("key-%d", i)] = "value"
	}

	tests := []struct {
		name   string
		manual *v1pb.ManualSQL
		limit  string
	}{
		{
			name:   "tags",
			manual: &v1pb.ManualSQL{SqlText: "SELECT 1", Tags: tags},
			limit:  fmt.Sprintf("at most %d tags", maxTags),
		},
		{
			name:   "attributes",
			manual: &v1pb.ManualSQL{SqlText: "SELECT 1", Attributes: attributes},
			limit:  fmt.Sprintf("at most %d attributes", maxAttributes),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := client.CreateManualSQL(ctx, withToken(env.AdminToken(), &v1pb.CreateManualSQLRequest{
				Parent:      parent,
				ManualSqlId: "bounded",
				ManualSql:   tc.manual,
			}))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "unexpected error: %v", err)
			require.Contains(t, err.Error(), tc.limit)
		})
	}
}
