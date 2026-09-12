//nolint:revive
package common

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

var resourceIDMatcher = regexp.MustCompile("^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$")

// IsValidResourceID reports whether id is a syntactically valid resource ID.
func IsValidResourceID(id string) bool {
	return resourceIDMatcher.MatchString(id)
}

// nolint:revive
const (
	WorkspacePrefix            = "workspaces/"
	EnvironmentNamePrefix      = "environments/"
	InstanceNamePrefix         = "instances/"
	DatabaseIDPrefix           = "databases/"
	UserNamePrefix             = "users/"
	IdentityProviderNamePrefix = "idps/"
	RolePrefix                 = "roles/"
	GroupPrefix                = "groups/"
	NamespaceMappingPrefix     = "openlineage/namespaceMappings/"
	OpenLineageRunPrefix       = "openlineage/runs/"
	OpenLineageTaskPrefix      = "openlineage/tasks/"
	APIKeyPrefix               = "openlineage/apiKeys/"
)

// GetOpenLineageToken returns the last segment of an OpenLineage resource name.
func GetOpenLineageToken(name, prefix string) (string, error) {
	if !strings.HasPrefix(name, prefix) {
		return "", errors.Errorf("invalid prefix %q in request %q", prefix, name)
	}
	token := strings.TrimPrefix(name, prefix)
	if token == "" || strings.Contains(token, "/") {
		return "", errors.Errorf("invalid request %q", name)
	}
	return token, nil
}

// GetOpenLineageIntID returns the numeric resource ID of a top-level OpenLineage
// resource name.
func GetOpenLineageIntID(name, prefix string) (int64, error) {
	token, err := GetOpenLineageToken(name, prefix)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return 0, errors.Errorf("invalid ID %q", token)
	}
	return id, nil
}

// GetNamespaceMappingID returns the namespace mapping ID from its resource name.
func GetNamespaceMappingID(name string) (int64, error) {
	return GetOpenLineageIntID(name, NamespaceMappingPrefix)
}

// GetOpenLineageRunGUID returns the run GUID from its resource name.
func GetOpenLineageRunGUID(name string) (string, error) {
	return GetOpenLineageToken(name, OpenLineageRunPrefix)
}

// GetOpenLineageTaskGUID returns the task GUID from its resource name.
func GetOpenLineageTaskGUID(name string) (string, error) {
	return GetOpenLineageToken(name, OpenLineageTaskPrefix)
}

// GetAPIKeyID returns the API key ID from its resource name.
func GetAPIKeyID(name string) (int64, error) {
	return GetOpenLineageIntID(name, APIKeyPrefix)
}

// FormatNamespaceMapping formats a namespace mapping resource name.
func FormatNamespaceMapping(id int64) string {
	return fmt.Sprintf("%s%d", NamespaceMappingPrefix, id)
}

// FormatOpenLineageRun formats an OpenLineage run resource name.
func FormatOpenLineageRun(guid string) string {
	return fmt.Sprintf("%s%s", OpenLineageRunPrefix, guid)
}

// FormatOpenLineageTask formats an OpenLineage task resource name.
func FormatOpenLineageTask(guid string) string {
	return fmt.Sprintf("%s%s", OpenLineageTaskPrefix, guid)
}

// FormatAPIKey formats an API key resource name.
func FormatAPIKey(id int64) string {
	return fmt.Sprintf("%s%d", APIKeyPrefix, id)
}

// GetUIDFromName returns the UID from a resource name.
func GetUIDFromName(name, prefix string) (int, error) {
	tokens, err := GetNameParentTokens(name, prefix)
	if err != nil {
		return 0, err
	}
	uid, err := strconv.Atoi(tokens[0])
	if err != nil {
		return 0, errors.Errorf("invalid ID %q", tokens[0])
	}
	return uid, nil
}

// GetEnvironmentID returns the environment ID from a resource name.
func GetEnvironmentID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, EnvironmentNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetInstanceID returns the instance ID from a resource name.
func GetInstanceID(name string) (string, error) {
	// the instance request should be instances/{instance-id}
	tokens, err := GetNameParentTokens(name, InstanceNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetInstanceDatabaseID returns the instance ID and database ID from a resource name.
func GetInstanceDatabaseID(name string) (string, string, error) {
	// the instance request should be instances/{instance-id}/databases/{database-id}
	tokens, err := GetNameParentTokens(name, InstanceNamePrefix, DatabaseIDPrefix)
	if err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

// GetUserID returns the user ID from a resource name.
func GetUserID(name string) (int, error) {
	return GetUIDFromName(name, UserNamePrefix)
}

// GetUserEmail returns the user email from a resource name.
func GetUserEmail(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, UserNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetIdentityProviderID returns the identity provider ID from a resource name.
func GetIdentityProviderID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, IdentityProviderNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetGroupEmail returns the group email.
func GetGroupEmail(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, GroupPrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetNameParentTokens returns the tokens from a resource name.
func GetNameParentTokens(name string, tokenPrefixes ...string) ([]string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2*len(tokenPrefixes) {
		return nil, errors.Errorf("invalid request %q", name)
	}

	var tokens []string
	for i, tokenPrefix := range tokenPrefixes {
		if fmt.Sprintf("%s/", parts[2*i]) != tokenPrefix {
			return nil, errors.Errorf("invalid prefix %q in request %q", tokenPrefix, name)
		}
		tokens = append(tokens, parts[2*i+1])
	}
	return tokens, nil
}

func FormatWorkspace(id string) string {
	return fmt.Sprintf("%s%s", WorkspacePrefix, id)
}

func FormatUserUID(uid int) string {
	return fmt.Sprintf("%s%d", UserNamePrefix, uid)
}

func FormatGroupEmail(email string) string {
	return fmt.Sprintf("%s%s", GroupPrefix, email)
}

func FormatEnvironment(resourceID string) string {
	return fmt.Sprintf("%s%s", EnvironmentNamePrefix, resourceID)
}

func FormatInstance(resourceID string) string {
	return fmt.Sprintf("%s%s", InstanceNamePrefix, resourceID)
}

func FormatDatabase(instance string, database string) string {
	return fmt.Sprintf("%s/%s%s", FormatInstance(instance), DatabaseIDPrefix, database)
}

func FormatRole(role string) string {
	return fmt.Sprintf("%s%s", RolePrefix, role)
}
