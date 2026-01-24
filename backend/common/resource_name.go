//nolint:revive
package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// nolint:revive
const (
	WorkspacePrefix            = "workspaces/"
	ProjectNamePrefix          = "projects/"
	EnvironmentNamePrefix      = "environments/"
	InstanceNamePrefix         = "instances/"
	PolicyNamePrefix           = "policies/"
	DatabaseIDPrefix           = "databases/"
	InstanceRolePrefix         = "roles/"
	UserNamePrefix             = "users/"
	IdentityProviderNamePrefix = "idps/"
	SettingNamePrefix          = "settings/"
	RolePrefix                 = "roles/"
	WebhookIDPrefix            = "webhooks/"
	DatabaseGroupNamePrefix    = "databaseGroups/"
	SchemaNamePrefix           = "schemas/"
	TableNamePrefix            = "tables/"
	LogNamePrefix              = "logs/"
	DeploymentConfigPrefix     = "deploymentConfigs/"
	AuditLogPrefix             = "auditLogs/"
	GroupPrefix                = "groups/"

	SchemaSuffix   = "/schema"
	MetadataSuffix = "/metadata"
	CatalogSuffix  = "/catalog"

	UserBindingPrefix  = "user:"
	GroupBindingPrefix = "group:"
)

// GetProjectID returns the project ID from a resource name.
func GetProjectID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, ProjectNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetProjectIDDatabaseGroupID returns the project ID and database group ID from a resource name.
func GetProjectIDDatabaseGroupID(name string) (string, string, error) {
	tokens, err := GetNameParentTokens(name, ProjectNamePrefix, DatabaseGroupNamePrefix)
	if err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

// GetSchemaTableName returns the schema and table names from a resource name.
func GetSchemaTableName(name string) (string, string, error) {
	tokens, err := GetNameParentTokens(name, SchemaNamePrefix, TableNamePrefix)
	if err != nil {
		return "", "", err
	}
	if tokens[0] == "-" {
		tokens[0] = ""
	}
	return tokens[0], tokens[1], nil
}

// GetProjectIDWebhookID returns the project ID and webhook ID from a resource name.
func GetProjectIDWebhookID(name string) (string, string, error) {
	tokens, err := GetNameParentTokens(name, ProjectNamePrefix, WebhookIDPrefix)
	if err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
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

// TrimSuffixAndGetInstanceDatabaseID trims the suffix from the name and returns the instance ID and database ID.
func TrimSuffixAndGetInstanceDatabaseID(name string, suffix string) (string, string, error) {
	trimmed, err := TrimSuffix(name, suffix)
	if err != nil {
		return "", "", err
	}
	return GetInstanceDatabaseID(trimmed)
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

// GetSettingName returns the setting name from a resource name.
func GetSettingName(name string) (string, error) {
	token, err := GetNameParentTokens(name, SettingNamePrefix)
	if err != nil {
		return "", err
	}
	return token[0], nil
}

// GetIdentityProviderID returns the identity provider ID from a resource name.
func GetIdentityProviderID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, IdentityProviderNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetRoleID returns the role ID from a resource name.
func GetRoleID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, RolePrefix)
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

// TrimSuffix trims the suffix from the name and returns the trimmed name.
func TrimSuffix(name, suffix string) (string, error) {
	if !strings.HasSuffix(name, suffix) {
		return "", errors.Errorf("invalid request %q with suffix %q", name, suffix)
	}
	return strings.TrimSuffix(name, suffix), nil
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

func FormatProject(id string) string {
	return fmt.Sprintf("%s%s", ProjectNamePrefix, id)
}

func FormatUserEmail(email string) string {
	return fmt.Sprintf("%s%s", UserNamePrefix, email)
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
