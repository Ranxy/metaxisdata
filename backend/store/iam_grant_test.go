package store

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// A grant query must narrow by the resource chain and the member set; a filter
// that names no scope or no member must match nothing rather than every row.
func TestBuildIamGrantWhere(t *testing.T) {
	t.Parallel()

	t.Run("a scoped member query", func(t *testing.T) {
		t.Parallel()
		where, args := buildIamGrantWhere(&FindIamGrantMessage{
			Scopes: []IamGrantScope{
				{ResourceType: "table", ResourceID: "i;db;public;t"},
				{ResourceType: "database", ResourceID: "i;db"},
			},
			Members:            []string{"users/7", "allUsers"},
			IncludeGroupGrants: true,
		})
		joined := strings.Join(where, " AND ")
		require.Contains(t, joined, "(resource_type = $1 AND resource_id = $2)")
		require.Contains(t, joined, "(resource_type = $3 AND resource_id = $4)")
		require.Contains(t, joined, "member = ANY($5)")
		require.Contains(t, joined, "member LIKE 'groups/%'")
		require.Len(t, args, 5)
	})

	t.Run("an empty scope matches nothing", func(t *testing.T) {
		t.Parallel()
		where, _ := buildIamGrantWhere(&FindIamGrantMessage{Scopes: []IamGrantScope{}})
		require.Contains(t, strings.Join(where, " AND "), "FALSE")
	})

	t.Run("an empty member set matches nothing", func(t *testing.T) {
		t.Parallel()
		where, _ := buildIamGrantWhere(&FindIamGrantMessage{Members: []string{}})
		require.Contains(t, strings.Join(where, " AND "), "FALSE")
	})

	t.Run("exact resource filters", func(t *testing.T) {
		t.Parallel()
		resourceType, resourceID := "database", "i;db"
		where, args := buildIamGrantWhere(&FindIamGrantMessage{
			ResourceType: &resourceType,
			ResourceID:   &resourceID,
		})
		joined := strings.Join(where, " AND ")
		require.Contains(t, joined, "resource_type = $1")
		require.Contains(t, joined, "resource_id = $2")
		require.Len(t, args, 2)
	})

	t.Run("no filter reads every grant", func(t *testing.T) {
		t.Parallel()
		where, args := buildIamGrantWhere(&FindIamGrantMessage{})
		require.Equal(t, []string{"TRUE"}, where)
		require.Empty(t, args)
	})
}

// The allUsers invariant is enforced in the store too: the implicit baseline
// must never become a scoped write grant, even for an internal caller.
func TestCreateIamGrantRejectsAllUsers(t *testing.T) {
	t.Parallel()

	_, err := (&Store{}).CreateIamGrant(context.Background(), &IamGrantMessage{
		ResourceType: "database",
		ResourceID:   "i;db",
		Role:         "roles/qualityOwner",
		Member:       common.AllUsers,
	})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}
