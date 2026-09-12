//nolint:revive
package common

const (
	WorkspaceAdmin  = "workspaceAdmin"
	WorkspaceMember = "workspaceMember"
)

const (
	// SystemBotID is the ID of the system robot.
	SystemBotID = 1

	// AllUsers is the email of the pseudo allUsers account.
	AllUsers = "allUsers"

	// ServiceAccountAccessKeyPrefix is the prefix for service account access key.
	ServiceAccountAccessKeyPrefix = "sa_"
)

// DefaultInstanceMaximumConnections is the maximum number of connections outstanding per instance by default.
const DefaultInstanceMaximumConnections = 10

const (
	DefaultMetaSubLevelLimit = 21

	// MetaGUIDSplit is the separator for GUID in meta queries.
	MetaGUIDSplit = ";"
)
