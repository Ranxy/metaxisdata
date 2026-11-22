package permission

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCatalogMatchesSource guards the single-source invariant: the generated
// catalog (constants + AllPermissions) must exactly match permission.json, so a
// hand edit of the generated file cannot silently add or drop a permission.
func TestCatalogMatchesSource(t *testing.T) {
	data, err := os.ReadFile("permission.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Permissions []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Permissions) != len(AllPermissions()) {
		t.Fatalf("catalog size mismatch: json=%d generated=%d", len(c.Permissions), len(AllPermissions()))
	}
	seen := map[string]bool{}
	for _, p := range c.Permissions {
		seen[p.ID] = true
		if !Exist(p.ID) {
			t.Errorf("permission %q from permission.json is missing from the generated catalog", p.ID)
		}
	}
	for _, p := range AllPermissions() {
		if !seen[p] {
			t.Errorf("permission %q exists in the generated catalog but not in permission.json", p)
		}
	}
	if Exist("metaxisdata.does.not.exist") {
		t.Error("unknown permission must not exist")
	}
}

// permissionShape is metaxisdata.<resource>.<verb>, optionally with a
// sub-resource segment (metaxisdata.llm.profiles.list). Segments are camelCase
// alphanumeric, matching the proto annotation strings verbatim.
var permissionShape = regexp.MustCompile(`^metaxisdata(\.[a-zA-Z][a-zA-Z0-9]*){2,3}$`)

// TestPermissionShape guards the naming contract every catalog entry and every
// proto annotation relies on.
func TestPermissionShape(t *testing.T) {
	for _, p := range AllPermissions() {
		if !permissionShape.MatchString(p) {
			t.Errorf("permission %q must have the form metaxisdata.<resource>[.<subResource>].<verb>", p)
		}
	}
}

// The scopeable set is the shipped scoped surface: every entry is a catalog
// permission, and workspace-checked administration is not in it.
func TestScopeablePermissions(t *testing.T) {
	t.Parallel()

	for _, perm := range ScopeablePermissions() {
		require.Truef(t, Exist(perm), "scopeable permission %q is not in the catalog", perm)
	}
	require.True(t, Scopeable(DataQualityRulesCreate))
	require.True(t, Scopeable(DataQualityIssuesUpdate))
	require.False(t, Scopeable(SettingsUpdate))
	require.False(t, Scopeable(UsersUpdate))
	require.False(t, Scopeable(IAMSetPolicy))
	require.False(t, Scopeable(DataQualityAgentsManage))
	require.False(t, Scopeable("not.a.permission"))

	blocked := NonScopeable(map[Permission]bool{
		DataQualityRulesCreate: true,
		SettingsUpdate:         true,
		UsersUpdate:            true,
	})
	require.Equal(t, []Permission{SettingsUpdate, UsersUpdate}, blocked)
	require.Empty(t, NonScopeable(map[Permission]bool{DataQualityRulesCreate: true}))
}
