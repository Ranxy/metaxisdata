//nolint:revive
package common

// CEL attribute names used by IAM policy conditions.
const (
	// CELAttributeResourceEnvironmentID is the environment ID of the resource.
	CELAttributeResourceEnvironmentID = "resource.environment_id"
	// CELAttributeResourceSchemaName is the schema name of the resource.
	CELAttributeResourceSchemaName = "resource.schema_name"
	// CELAttributeResourceTableName is the table name of the resource.
	CELAttributeResourceTableName = "resource.table_name"
	// CELAttributeResourceDatabase is the full database name of the resource (used in IAM policy conditions).
	CELAttributeResourceDatabase = "resource.database"
	// CELAttributeRequestTime is the timestamp of the request.
	CELAttributeRequestTime = "request.time"
)
