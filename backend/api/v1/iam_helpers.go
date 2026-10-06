package v1

import (
	"context"
	"time"

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
//
// A binding only counts while its condition holds at request time: the
// permission check evaluates conditions (utils.GetUserIAMPolicyBindings), so a
// binding whose condition is false grants nothing and cannot be what keeps the
// workspace administrable. At least one counting binding must additionally be
// unconditional, so a write cannot leave the workspace with only time-boxed
// admins that lapse later — that would be the same permanent lockout one day
// further out.
func hasActiveWorkspaceAdmin(ctx context.Context, stores *store.Store, policy *storepb.IamPolicy, excludeUserID int) (bool, error) {
	workspaceAdminRole := common.FormatRole(common.WorkspaceAdmin)
	excludedMember := ""
	if excludeUserID != 0 {
		excludedMember = common.FormatUserUID(excludeUserID)
	}

	active, unconditional := false, false
	for _, binding := range policy.GetBindings() {
		if binding.GetRole() != workspaceAdminRole {
			continue
		}
		expression := binding.GetCondition().GetExpression()
		effective, err := common.EvalBindingCondition(expression, time.Now())
		if err != nil || !effective {
			// Fail closed on a condition that is false now or cannot be
			// evaluated: the binding grants nothing at check time either way.
			continue
		}
		for _, member := range binding.GetMembers() {
			if excludeUserID != 0 && member == excludedMember {
				continue
			}
			covered := false
			if member == common.AllUsers {
				count, err := activeEndUserCount(ctx, stores)
				if err != nil {
					return false, err
				}
				covered = count > 0
				if excludeUserID != 0 {
					covered = count > 1
				}
			} else {
				for _, user := range utils.GetUsersByMember(ctx, stores, member) {
					if excludeUserID != 0 && user.ID == excludeUserID {
						continue
					}
					if !user.MemberDeleted && user.Type == storepb.PrincipalType_END_USER {
						covered = true
					}
				}
			}
			if covered {
				active = true
				unconditional = unconditional || expression == ""
			}
		}
	}
	return active && unconditional, nil
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
