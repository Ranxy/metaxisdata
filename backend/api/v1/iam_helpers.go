package v1

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
	"github.com/Ranxy/metaxisdata/backend/utils"
)

// hasActiveWorkspaceAdmin reports whether policy grants roles/workspaceAdmin to
// at least one active end user, either directly, through a group, or via
// allUsers. excludeUserID skips one user's membership (the delete/self-leave
// last-admin guard); it must be 0 when no exclusion is wanted. Group expansion
// excludes that user too, so a group containing only the departing admin does
// not count as a surviving one.
func hasActiveWorkspaceAdmin(ctx context.Context, stores *store.Store, policy *storepb.IamPolicy, excludeUserID int) (bool, error) {
	workspaceAdminRole := common.FormatRole(common.WorkspaceAdmin)
	excludedMember := ""
	if excludeUserID != 0 {
		excludedMember = common.FormatUserUID(excludeUserID)
	}

	for _, binding := range policy.GetBindings() {
		if binding.GetRole() != workspaceAdminRole {
			continue
		}
		for _, member := range binding.GetMembers() {
			if excludeUserID != 0 && member == excludedMember {
				continue
			}
			if member == common.AllUsers {
				count, err := activeEndUserCount(ctx, stores)
				if err != nil {
					return false, err
				}
				if excludeUserID != 0 {
					return count > 1, nil
				}
				return count > 0, nil
			}
			for _, user := range utils.GetUsersByMember(ctx, stores, member) {
				if excludeUserID != 0 && user.ID == excludeUserID {
					continue
				}
				if !user.MemberDeleted && user.Type == storepb.PrincipalType_END_USER {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// activeEndUserCount counts the non-deleted END_USER principals.
func activeEndUserCount(ctx context.Context, stores *store.Store) (int, error) {
	userStat, err := stores.StatUsers(ctx)
	if err != nil {
		return 0, err
	}
	for _, stat := range userStat {
		if !stat.Deleted && stat.Type == storepb.PrincipalType_END_USER {
			return stat.Count, nil
		}
	}
	return 0, nil
}
