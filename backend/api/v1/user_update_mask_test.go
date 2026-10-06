package v1

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// H6: CreateUser is reachable without a credential, so a field it accepts must
// not be able to carry a request body into the users table (or the audit row
// that records the request).
func TestValidateUserFieldsAreBounded(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateEmail("alice@example.com"))
	require.Error(t, validateEmail(strings.Repeat("a", 300)+"@example.com"), "an address longer than 254 bytes is refused")

	require.NoError(t, validateUserTitle(strings.Repeat("名", maxUserTitleBytes/3)))
	require.Error(t, validateUserTitle(strings.Repeat("t", maxUserTitleBytes+1)))

	// An identity provider's display name cannot be refused, so it is clamped on
	// a rune boundary instead: a half rune would be rejected by the store.
	require.Equal(t, strings.Repeat("t", maxUserTitleBytes), clampUserTitle(strings.Repeat("t", maxUserTitleBytes)))
	clamped := clampUserTitle(strings.Repeat("名", 3000))
	require.True(t, utf8.ValidString(clamped))
	require.LessOrEqual(t, len(clamped), maxUserTitleBytes)
}

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
