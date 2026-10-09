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
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// TestUpdateUserLanguageRealServerIntegration pins the preference the Explain SQL
// prompt is built from. It is written to the profile's JSONB column and read back
// on the next request, so the round trip through the store is what has to hold: a
// tag that is dropped on the way in would silently answer every question in the
// default language. A tag the server ships no prompt text for is refused instead.
func TestUpdateUserLanguageRealServerIntegration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	adminToken := env.AdminToken()
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	const password = "Integration-Pass-1!"
	email := fmt.Sprintf("language-rule-%s@example.com", suffix)

	created, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Language member",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)
	userName := created.Msg.GetName()
	require.Empty(t, created.Msg.GetLanguage(), "a fresh account has chosen no language")

	memberToken, err := env.LoginAs(ctx, email, password)
	require.NoError(t, err)

	// The switch the SPA makes when the user picks a language from the menu: a
	// member may set their own preference, with no metaxisdata.users.update.
	updated, err := userClient.UpdateUser(ctx, withToken(memberToken, &v1pb.UpdateUserRequest{
		User:       &v1pb.User{Name: userName, Language: "zh-CN"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"language"}},
	}))
	require.NoError(t, err)
	require.Equal(t, "zh-CN", updated.Msg.GetLanguage())

	// Read it back the way the SPA does at sign-in and Explain SQL does per
	// request. The write replaces the whole profile column, so the fields it does
	// not own have to come back untouched.
	current, err := userClient.GetCurrentUser(ctx, withToken(memberToken, &emptypb.Empty{}))
	require.NoError(t, err)
	require.Equal(t, "zh-CN", current.Msg.GetLanguage())
	require.Equal(t, "Language member", current.Msg.GetTitle())

	_, err = userClient.UpdateUser(ctx, withToken(memberToken, &v1pb.UpdateUserRequest{
		User:       &v1pb.User{Name: userName, Language: "fr-FR"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"language"}},
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "a language the server cannot prompt in is refused")

	current, err = userClient.GetCurrentUser(ctx, withToken(memberToken, &emptypb.Empty{}))
	require.NoError(t, err)
	require.Equal(t, "zh-CN", current.Msg.GetLanguage(), "the refused write changed nothing")
}
