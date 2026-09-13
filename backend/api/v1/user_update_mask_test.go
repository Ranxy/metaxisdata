package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// A create through PATCH with allow_missing must apply the update mask: the
// request body may not smuggle in fields the caller did not declare.
func TestApplyUpdateMaskToUser(t *testing.T) {
	t.Parallel()

	body := &v1pb.User{
		Email:    "alice@example.com",
		Title:    "alice",
		Password: "s3cret-password",
		Phone:    "+8613800000000",
		UserType: v1pb.UserType_END_USER,
	}

	tests := []struct {
		name  string
		paths []string
		want  *v1pb.User
	}{
		{
			name:  "declared fields are kept",
			paths: []string{"email", "title", "password", "phone"},
			want: &v1pb.User{
				Email:    body.Email,
				Title:    body.Title,
				Password: body.Password,
				Phone:    body.Phone,
			},
		},
		{
			name:  "undeclared fields are dropped",
			paths: []string{"title"},
			want:  &v1pb.User{Title: body.Title},
		},
		{
			name:  "user_type selects the kind of principal",
			paths: []string{"email", "user_type"},
			want:  &v1pb.User{Email: body.Email, UserType: v1pb.UserType_END_USER},
		},
		{
			name:  "an unknown path is ignored",
			paths: []string{"no_such_field"},
			want:  &v1pb.User{},
		},
		{
			name:  "no mask yields nothing to create from",
			paths: nil,
			want:  &v1pb.User{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, applyUpdateMaskToUser(body, test.paths))
		})
	}

	// The request body itself must not be touched.
	require.Equal(t, body.Email, "alice@example.com")
	require.Equal(t, body.UserType, v1pb.UserType_END_USER)
}
