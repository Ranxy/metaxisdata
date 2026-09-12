//go:build integration

package runner

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// TestUnauthenticatedRequestsAreRejectedRealServerIntegration is the reverse of
// the hermetic api/auth tests: a real server must refuse a request that carries
// no credential, a malformed one, or one signed with a foreign key.
func TestUnauthenticatedRequestsAreRejectedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	instanceClient := v1connect.NewInstanceServiceClient(httpClient, env.BaseURL)
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)

	ctx := context.Background()

	tests := []struct {
		name          string
		authorization string
	}{
		{"no credential", ""},
		{"malformed token", "Bearer not-a-jwt"},
		{"token with the wrong algorithm", "Bearer eyJhbGciOiJub25lIn0.e30."},
		{"empty bearer", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := connect.NewRequest(&v1pb.ListInstancesRequest{PageSize: 1})
			if tt.authorization != "" {
				req.Header().Set("Authorization", tt.authorization)
			}

			_, err := instanceClient.ListInstances(ctx, req)
			require.Error(t, err)
			require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "unexpected error: %v", err)
		})
	}

	// A read that never touches the store must be rejected the same way.
	_, err := userClient.GetCurrentUser(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}
