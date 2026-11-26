package permission

import "slices"

// Scopeable reports whether a permission can be satisfied by a grant on a
// resource scope (an instance or a database subtree).
//
// The catalog is flat: a permission name does not say whether its RPC resolves a
// resource. A permission whose handler always checks the workspace — settings,
// user or role administration, IAM itself, catalog authoring helpers — can never
// be satisfied by a scoped grant, so the authorizer must not report it and a
// grant may not carry it. Only permissions whose handler resolves a target and
// asks the resource authorizer belong in this set.
func Scopeable(p Permission) bool {
	return scopeablePermissions[p]
}

// ScopeablePermissions returns the catalog subset that can be granted at a
// scope, sorted. It is a copy: callers cannot mutate the set.
func ScopeablePermissions() []Permission {
	out := make([]Permission, 0, len(scopeablePermissions))
	for p := range scopeablePermissions {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// NonScopeable returns the permissions in the set that cannot be granted at a
// scope, sorted. An empty result means every permission applies there.
func NonScopeable(perms map[Permission]bool) []Permission {
	var out []Permission
	for p := range perms {
		if !scopeablePermissions[p] {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// scopeablePermissions is the shipped scoped surface: the quality-rule, issue,
// execution, contract, plan, profile and label actions whose handler resolves a
// table target. A new scoped permission is added here together with its target
// check, and the scoped-method guard test keeps the two in step.
var scopeablePermissions = map[Permission]bool{
	DataQualityRulesGet:         true,
	DataQualityRulesList:        true,
	DataQualityRulesEvidence:    true,
	DataQualityRulesCreate:      true,
	DataQualityRulesUpdate:      true,
	DataQualityRulesDelete:      true,
	DataQualityRulesRun:         true,
	DataQualityExecutionsGet:    true,
	DataQualityExecutionsList:   true,
	DataQualityIssuesGet:        true,
	DataQualityIssuesList:       true,
	DataQualityIssuesUpdate:     true,
	DataQualityExplain:          true,
	DataQualitySuggest:          true,
	DataQualityContractsPublish: true,
	DataQualityPlansUpdate:      true,
	DataQualityProfileRun:       true,
	DataQualityLabelsUpdate:     true,
}
