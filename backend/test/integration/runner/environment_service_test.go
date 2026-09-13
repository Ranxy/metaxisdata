//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// TestEnvironmentServiceRealServerIntegration drives the environment lifecycle
// against a real server and store: create derives an immutable id, the list
// reports it with its usage count, update renames without changing the id, and
// delete is refused while a live instance still references it.
func TestEnvironmentServiceRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	ctx := t.Context()
	adminToken := env.AdminToken()

	client := v1connect.NewEnvironmentServiceClient(httpClient, env.BaseURL)
	instanceClient := v1connect.NewInstanceServiceClient(httpClient, env.BaseURL)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	title := "IT Environment " + suffix

	created, err := client.CreateEnvironment(ctx, withToken(adminToken, &v1pb.CreateEnvironmentRequest{
		Environment: &v1pb.Environment{Title: title},
	}))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(created.Msg.GetName(), "environments/"), "name = %q", created.Msg.GetName())
	require.Equal(t, title, created.Msg.GetTitle())
	require.NotEmpty(t, created.Msg.GetColor(), "the server must assign a palette color")

	// A duplicate title is rejected instead of silently adding a second entry.
	_, err = client.CreateEnvironment(ctx, withToken(adminToken, &v1pb.CreateEnvironmentRequest{
		Environment: &v1pb.Environment{Title: title},
	}))
	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	// The list reports the new environment with a zero usage count.
	list, err := client.ListEnvironments(ctx, withToken(adminToken, &v1pb.ListEnvironmentsRequest{PageSize: 100}))
	require.NoError(t, err)
	var listed *v1pb.Environment
	for _, environment := range list.Msg.GetEnvironments() {
		if environment.GetName() == created.Msg.GetName() {
			listed = environment
		}
	}
	require.NotNil(t, listed, "the created environment must appear in the list")
	require.Equal(t, int32(0), listed.GetInstanceCount())

	// Renaming keeps the id so existing instance references stay valid.
	renamed := title + " renamed"
	updated, err := client.UpdateEnvironment(ctx, withToken(adminToken, &v1pb.UpdateEnvironmentRequest{
		Environment: &v1pb.Environment{
			Name:  created.Msg.GetName(),
			Title: renamed,
			Color: "green",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title", "color"}},
	}))
	require.NoError(t, err)
	require.Equal(t, renamed, updated.Msg.GetTitle())
	require.Equal(t, "green", updated.Msg.GetColor())
	require.Equal(t, created.Msg.GetName(), updated.Msg.GetName())

	// Assign it to a live instance: deleting the environment is now refused.
	instance, err := env.CreatePostgresInstance(ctx, "env-it-"+suffix)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DeleteInstance(context.Background(), instance.GetName())
		_, _ = client.DeleteEnvironment(context.Background(), withToken(adminToken, &v1pb.DeleteEnvironmentRequest{Name: created.Msg.GetName()}))
	})

	_, err = instanceClient.UpdateInstance(ctx, withToken(adminToken, &v1pb.UpdateInstanceRequest{
		Instance: &v1pb.Instance{
			Name:        instance.GetName(),
			Environment: created.Msg.GetName(),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"environment"}},
	}))
	require.NoError(t, err)

	_, err = client.DeleteEnvironment(ctx, withToken(adminToken, &v1pb.DeleteEnvironmentRequest{Name: created.Msg.GetName()}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	// Once the instance is deleted the environment is releasable again.
	_, err = env.DeleteInstance(ctx, instance.GetName())
	require.NoError(t, err)
	_, err = client.DeleteEnvironment(ctx, withToken(adminToken, &v1pb.DeleteEnvironmentRequest{Name: created.Msg.GetName()}))
	require.NoError(t, err)
}
