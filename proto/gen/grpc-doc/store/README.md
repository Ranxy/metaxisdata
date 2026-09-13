# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [store/audit_log.proto](#store_audit_log-proto)
    - [AuditLog](#metaxisdata-store-AuditLog)
    - [AuditLogStatus](#metaxisdata-store-AuditLogStatus)
    - [AuditRequestMetadata](#metaxisdata-store-AuditRequestMetadata)
  
    - [AuditLogSeverity](#metaxisdata-store-AuditLogSeverity)
  
- [store/common.proto](#store_common-proto)
    - [PageToken](#metaxisdata-store-PageToken)
  
    - [Engine](#metaxisdata-store-Engine)
  
- [store/openlineage.proto](#store_openlineage-proto)
    - [OpenLineageRunSummary](#metaxisdata-store-OpenLineageRunSummary)
    - [OpenLineageTaskSummary](#metaxisdata-store-OpenLineageTaskSummary)
  
- [store/database.proto](#store_database-proto)
    - [CheckConstraintMetadata](#metaxisdata-store-CheckConstraintMetadata)
    - [ColumnMetadata](#metaxisdata-store-ColumnMetadata)
    - [DatabaseMetadata](#metaxisdata-store-DatabaseMetadata)
    - [DatabaseMetadata.LabelsEntry](#metaxisdata-store-DatabaseMetadata-LabelsEntry)
    - [DatabaseSchemaMetadata](#metaxisdata-store-DatabaseSchemaMetadata)
    - [DependencyColumn](#metaxisdata-store-DependencyColumn)
    - [DependencyTable](#metaxisdata-store-DependencyTable)
    - [EnumTypeMetadata](#metaxisdata-store-EnumTypeMetadata)
    - [EventMetadata](#metaxisdata-store-EventMetadata)
    - [EventTriggerMetadata](#metaxisdata-store-EventTriggerMetadata)
    - [ExcludeConstraintMetadata](#metaxisdata-store-ExcludeConstraintMetadata)
    - [ExtensionMetadata](#metaxisdata-store-ExtensionMetadata)
    - [ExternalTableMetadata](#metaxisdata-store-ExternalTableMetadata)
    - [ForeignKeyMetadata](#metaxisdata-store-ForeignKeyMetadata)
    - [FunctionMetadata](#metaxisdata-store-FunctionMetadata)
    - [GenerationMetadata](#metaxisdata-store-GenerationMetadata)
    - [IndexMetadata](#metaxisdata-store-IndexMetadata)
    - [ManualSQLMetadata](#metaxisdata-store-ManualSQLMetadata)
    - [ManualSQLMetadata.AttributesEntry](#metaxisdata-store-ManualSQLMetadata-AttributesEntry)
    - [MaterializedViewMetadata](#metaxisdata-store-MaterializedViewMetadata)
    - [ProcedureMetadata](#metaxisdata-store-ProcedureMetadata)
    - [RuleMetadata](#metaxisdata-store-RuleMetadata)
    - [SchemaMetadata](#metaxisdata-store-SchemaMetadata)
    - [SequenceMetadata](#metaxisdata-store-SequenceMetadata)
    - [StoredMetadata](#metaxisdata-store-StoredMetadata)
    - [TableMetadata](#metaxisdata-store-TableMetadata)
    - [TablePartitionMetadata](#metaxisdata-store-TablePartitionMetadata)
    - [TriggerMetadata](#metaxisdata-store-TriggerMetadata)
    - [ViewMetadata](#metaxisdata-store-ViewMetadata)
  
    - [ColumnMetadata.IdentityGeneration](#metaxisdata-store-ColumnMetadata-IdentityGeneration)
    - [GenerationMetadata.Type](#metaxisdata-store-GenerationMetadata-Type)
    - [TablePartitionMetadata.Type](#metaxisdata-store-TablePartitionMetadata-Type)
  
- [store/group.proto](#store_group-proto)
    - [GroupMember](#metaxisdata-store-GroupMember)
    - [GroupPayload](#metaxisdata-store-GroupPayload)
  
    - [GroupMember.Role](#metaxisdata-store-GroupMember-Role)
  
- [store/idp.proto](#store_idp-proto)
    - [FieldMapping](#metaxisdata-store-FieldMapping)
    - [IdentityProviderConfig](#metaxisdata-store-IdentityProviderConfig)
    - [IdentityProviderUserInfo](#metaxisdata-store-IdentityProviderUserInfo)
    - [OAuth2IdentityProviderConfig](#metaxisdata-store-OAuth2IdentityProviderConfig)
  
    - [IdentityProviderType](#metaxisdata-store-IdentityProviderType)
    - [OAuth2AuthStyle](#metaxisdata-store-OAuth2AuthStyle)
  
- [store/instance.proto](#store_instance-proto)
    - [DataSource](#metaxisdata-store-DataSource)
    - [DataSource.ExtraConnectionParametersEntry](#metaxisdata-store-DataSource-ExtraConnectionParametersEntry)
    - [Instance](#metaxisdata-store-Instance)
  
    - [DataSourceType](#metaxisdata-store-DataSourceType)
  
- [store/llm.proto](#store_llm-proto)
    - [LlmProviderModel](#metaxisdata-store-LlmProviderModel)
    - [LlmProviderProfile](#metaxisdata-store-LlmProviderProfile)
  
    - [LLMProviderType](#metaxisdata-store-LLMProviderType)
  
- [store/meta.proto](#store_meta-proto)
    - [MetaType](#metaxisdata-store-MetaType)
  
- [store/policy.proto](#store_policy-proto)
    - [Binding](#metaxisdata-store-Binding)
    - [IamPolicy](#metaxisdata-store-IamPolicy)
    - [Policy](#metaxisdata-store-Policy)
  
    - [Policy.Resource](#metaxisdata-store-Policy-Resource)
    - [Policy.Type](#metaxisdata-store-Policy-Type)
  
- [store/role.proto](#store_role-proto)
    - [RolePermissions](#metaxisdata-store-RolePermissions)
  
- [store/setting.proto](#store_setting-proto)
    - [EnvironmentSetting](#metaxisdata-store-EnvironmentSetting)
    - [EnvironmentSetting.Environment](#metaxisdata-store-EnvironmentSetting-Environment)
    - [EnvironmentSetting.Environment.TagsEntry](#metaxisdata-store-EnvironmentSetting-Environment-TagsEntry)
    - [PasswordRestrictionSetting](#metaxisdata-store-PasswordRestrictionSetting)
    - [WorkspaceProfileSetting](#metaxisdata-store-WorkspaceProfileSetting)
  
    - [SettingName](#metaxisdata-store-SettingName)
  
- [store/user.proto](#store_user-proto)
    - [UserProfile](#metaxisdata-store-UserProfile)
  
    - [PrincipalType](#metaxisdata-store-PrincipalType)
  
- [Scalar Value Types](#scalar-value-types)



<a name="store_audit_log-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/audit_log.proto



<a name="metaxisdata-store-AuditLog"></a>

### AuditLog



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [int64](#int64) |  | The audit_log row&#39;s primary key. The public v1 AuditLog exposes it as the resource name &#34;{parent}/auditLogs/{id}&#34; instead, because audit rows are identified by an opaque server-generated id. |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| parent | [string](#string) |  |  |
| method | [string](#string) |  |  |
| resource | [string](#string) |  |  |
| user | [string](#string) |  |  |
| severity | [AuditLogSeverity](#metaxisdata-store-AuditLogSeverity) |  |  |
| request | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| response | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| status | [AuditLogStatus](#metaxisdata-store-AuditLogStatus) |  |  |
| latency_ms | [int64](#int64) |  |  |
| service_data | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| request_metadata | [AuditRequestMetadata](#metaxisdata-store-AuditRequestMetadata) |  |  |






<a name="metaxisdata-store-AuditLogStatus"></a>

### AuditLogStatus



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [int32](#int32) |  |  |
| message | [string](#string) |  |  |






<a name="metaxisdata-store-AuditRequestMetadata"></a>

### AuditRequestMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| ip | [string](#string) |  |  |
| user_agent | [string](#string) |  |  |





 


<a name="metaxisdata-store-AuditLogSeverity"></a>

### AuditLogSeverity


| Name | Number | Description |
| ---- | ------ | ----------- |
| AUDIT_SEVERITY_UNSPECIFIED | 0 |  |
| INFO | 1 |  |
| WARNING | 2 |  |
| ERROR | 3 |  |


 

 

 



<a name="store_common-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/common.proto



<a name="metaxisdata-store-PageToken"></a>

### PageToken
Used internally for obfuscating the page token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| limit | [int32](#int32) |  |  |
| offset | [int32](#int32) |  |  |





 


<a name="metaxisdata-store-Engine"></a>

### Engine
Engine is the database engine of an instance. It must stay value-compatible
with metaxisdata.v1.Engine.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ENGINE_UNSPECIFIED | 0 |  |
| MYSQL | 2 |  |
| POSTGRES | 3 |  |
| TIDB | 6 |  |
| MARIADB | 13 |  |
| OCEANBASE | 14 |  |


 

 

 



<a name="store_openlineage-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/openlineage.proto



<a name="metaxisdata-store-OpenLineageRunSummary"></a>

### OpenLineageRunSummary
OpenLineageRunSummary stores the normalized metadata for a persisted COMPLETE run.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| task_guid | [string](#string) |  |  |
| run_id | [string](#string) |  |  |
| job_namespace | [string](#string) |  |  |
| job_name | [string](#string) |  |  |
| job_type | [string](#string) |  |  |
| event_type | [string](#string) |  |  |
| event_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| producer | [string](#string) |  |  |
| source | [string](#string) |  |  |
| integration | [string](#string) |  |  |
| processing_type | [string](#string) |  |  |
| parent_job_namespace | [string](#string) |  |  |
| parent_job_name | [string](#string) |  |  |
| parent_run_id | [string](#string) |  |  |
| root_job_namespace | [string](#string) |  |  |
| root_job_name | [string](#string) |  |  |
| root_run_id | [string](#string) |  |  |
| input_count | [int32](#int32) |  |  |
| output_count | [int32](#int32) |  |  |
| has_lineage | [bool](#bool) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-store-OpenLineageTaskSummary"></a>

### OpenLineageTaskSummary
OpenLineageTaskSummary stores the aggregated task/job-level view derived from persisted runs.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| job_namespace | [string](#string) |  |  |
| job_name | [string](#string) |  |  |
| job_type | [string](#string) |  |  |
| integration | [string](#string) |  |  |
| processing_type | [string](#string) |  |  |
| parent_job_namespace | [string](#string) |  |  |
| parent_job_name | [string](#string) |  |  |
| root_job_namespace | [string](#string) |  |  |
| root_job_name | [string](#string) |  |  |
| latest_run_guid | [string](#string) |  |  |
| latest_run_id | [string](#string) |  |  |
| latest_event_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| latest_producer | [string](#string) |  |  |
| latest_source | [string](#string) |  |  |
| run_count | [int32](#int32) |  |  |
| lineage_run_count | [int32](#int32) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 

 

 

 



<a name="store_database-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/database.proto



<a name="metaxisdata-store-CheckConstraintMetadata"></a>

### CheckConstraintMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the check constraint. |
| expression | [string](#string) |  | The expression is the expression of a check constraint. |






<a name="metaxisdata-store-ColumnMetadata"></a>

### ColumnMetadata
ColumnMetadata is the metadata for columns.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the column. |
| position | [int32](#int32) |  | The position is the position in columns. |
| default | [string](#string) |  | The default value of the column. |
| default_on_null | [bool](#bool) |  | Oracle specific metadata. The default_on_null is the default on null of a column. |
| on_update | [string](#string) |  | The on_update is the on update action of a column. For MySQL like databases, it&#39;s only supported for TIMESTAMP columns with CURRENT_TIMESTAMP as on update value. |
| nullable | [bool](#bool) |  | The nullable is the nullable of a column. |
| type | [string](#string) |  | The type is the type of a column. |
| character_set | [string](#string) |  | The character_set is the character_set of a column. |
| collation | [string](#string) |  | The collation is the collation of a column. |
| comment | [string](#string) |  | The comment is the comment of a column. classification and user_comment is parsed from the comment. |
| user_comment | [string](#string) |  | The user_comment is the user comment of a table parsed from the comment. |
| generation | [GenerationMetadata](#metaxisdata-store-GenerationMetadata) |  | The generation is for generated columns. |
| is_identity | [bool](#bool) |  |  |
| identity_generation | [ColumnMetadata.IdentityGeneration](#metaxisdata-store-ColumnMetadata-IdentityGeneration) |  | The identity_generation is for identity columns, PG only. |
| identity_seed | [int64](#int64) |  | The identity_seed is for identity columns, MSSQL only. |
| identity_increment | [int64](#int64) |  | The identity_increment is for identity columns, MSSQL only. |
| default_constraint_name | [string](#string) |  | The default_constraint_name is the name of the default constraint, MSSQL only. In MSSQL, default values are implemented as named constraints. When modifying or dropping a column&#39;s default value, you must reference the constraint by name. This field stores the actual constraint name from the database.

Example: A column definition like: CREATE TABLE employees ( status NVARCHAR(20) DEFAULT &#39;active&#39; )

Will create a constraint with an auto-generated name like &#39;DF__employees__statu__3B75D760&#39; or a user-defined name if specified: ALTER TABLE employees ADD CONSTRAINT DF_employees_status DEFAULT &#39;active&#39; FOR status

To modify the default, you must first drop the existing constraint by name: ALTER TABLE employees DROP CONSTRAINT DF__employees__statu__3B75D760 ALTER TABLE employees ADD CONSTRAINT DF_employees_status DEFAULT &#39;inactive&#39; FOR status

This field is populated when syncing from the database. When empty (e.g., when parsing from SQL files), the system cannot automatically drop the constraint. |






<a name="metaxisdata-store-DatabaseMetadata"></a>

### DatabaseMetadata
DatabaseMetadata is the metadata for databases.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| labels | [DatabaseMetadata.LabelsEntry](#metaxisdata-store-DatabaseMetadata-LabelsEntry) | repeated |  |
| last_sync_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| datashare | [bool](#bool) |  |  |
| drifted | [bool](#bool) |  | The schema has drifted from the source of truth. |
| version | [string](#string) |  | The version of database schema. |






<a name="metaxisdata-store-DatabaseMetadata-LabelsEntry"></a>

### DatabaseMetadata.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-store-DatabaseSchemaMetadata"></a>

### DatabaseSchemaMetadata
DatabaseSchemaMetadata is the schema metadata for databases.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| schemas | [SchemaMetadata](#metaxisdata-store-SchemaMetadata) | repeated | The list of schemas in a database. |
| character_set | [string](#string) |  | The character set of the database. |
| collation | [string](#string) |  | The collation of the database. |
| extensions | [ExtensionMetadata](#metaxisdata-store-ExtensionMetadata) | repeated | The list of extensions in a database. |
| datashare | [bool](#bool) |  | The database belongs to a datashare. |
| owner | [string](#string) |  |  |
| search_path | [string](#string) |  | The search_path is the search path of a PostgreSQL database. |
| event_triggers | [EventTriggerMetadata](#metaxisdata-store-EventTriggerMetadata) | repeated | The list of event triggers in a database (PostgreSQL specific). Event triggers are database-level objects, not schema-scoped. |






<a name="metaxisdata-store-DependencyColumn"></a>

### DependencyColumn
DependencyColumn is the metadata for dependency columns.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema | [string](#string) |  | The schema is the schema of a reference column. |
| table | [string](#string) |  | The table is the table of a reference column. |
| column | [string](#string) |  | The column is the name of a reference column. |






<a name="metaxisdata-store-DependencyTable"></a>

### DependencyTable



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema | [string](#string) |  | The schema is the schema of a reference table. |
| table | [string](#string) |  | The table is the name of a reference table. |






<a name="metaxisdata-store-EnumTypeMetadata"></a>

### EnumTypeMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the enum type. |
| values | [string](#string) | repeated | The enum values of the type. |
| comment | [string](#string) |  |  |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-store-EventMetadata"></a>

### EventMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the event. |
| definition | [string](#string) |  | The schedule of the event. |
| time_zone | [string](#string) |  | The time zone of the event. |
| sql_mode | [string](#string) |  |  |
| character_set_client | [string](#string) |  |  |
| collation_connection | [string](#string) |  |  |
| comment | [string](#string) |  |  |






<a name="metaxisdata-store-EventTriggerMetadata"></a>

### EventTriggerMetadata
EventTriggerMetadata is the metadata for PostgreSQL event triggers.
Event triggers are database-level objects that fire on DDL events.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the event trigger. |
| event | [string](#string) |  | The event type: DDL_COMMAND_START, DDL_COMMAND_END, SQL_DROP, TABLE_REWRITE. |
| tags | [string](#string) | repeated | The tags filter (e.g., [&#39;CREATE TABLE&#39;, &#39;DROP TABLE&#39;]). |
| function_schema | [string](#string) |  | The schema of the function to execute. |
| function_name | [string](#string) |  | The name of the function to execute. |
| enabled | [bool](#bool) |  | Whether the trigger is enabled. |
| definition | [string](#string) |  | The full CREATE EVENT TRIGGER definition from pg_get_event_trigger_def(). SDL output should prefer using this field. |
| comment | [string](#string) |  | The comment on the event trigger. |
| skip_dump | [bool](#bool) |  | Skip dump flag (for extension-owned triggers). |






<a name="metaxisdata-store-ExcludeConstraintMetadata"></a>

### ExcludeConstraintMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the EXCLUDE constraint. |
| expression | [string](#string) |  | The expression is the full EXCLUDE constraint definition including &#34;EXCLUDE&#34; keyword. Example: &#34;EXCLUDE USING gist (room_id WITH =, during WITH &amp;&amp;)&#34; |






<a name="metaxisdata-store-ExtensionMetadata"></a>

### ExtensionMetadata
ExtensionMetadata is the metadata for extensions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the extension. |
| schema | [string](#string) |  | The schema where the extension is installed. However, the extension usage is not limited to the schema. |
| version | [string](#string) |  | The version is the version of an extension. |
| description | [string](#string) |  | The description is the description of an extension. |






<a name="metaxisdata-store-ExternalTableMetadata"></a>

### ExternalTableMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the external table. |
| external_server_name | [string](#string) |  | The external_server_name is the name of the external server. |
| external_database_name | [string](#string) |  | The external_database_name is the name of the external database. |
| columns | [ColumnMetadata](#metaxisdata-store-ColumnMetadata) | repeated | The columns is the ordered list of columns in a foreign table. |






<a name="metaxisdata-store-ForeignKeyMetadata"></a>

### ForeignKeyMetadata
ForeignKeyMetadata is the metadata for foreign keys.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the foreign key. |
| columns | [string](#string) | repeated | The columns are the ordered referencing columns of a foreign key. |
| referenced_schema | [string](#string) |  | The referenced_schema is the referenced schema name of a foreign key. It is an empty string for databases without such concept such as MySQL. |
| referenced_table | [string](#string) |  | The referenced_table is the referenced table name of a foreign key. |
| referenced_columns | [string](#string) | repeated | The referenced_columns are the ordered referenced columns of a foreign key. |
| on_delete | [string](#string) |  | The on_delete is the on delete action of a foreign key. |
| on_update | [string](#string) |  | The on_update is the on update action of a foreign key. |
| match_type | [string](#string) |  | The match_type is the match type of a foreign key. The match_type is the PostgreSQL specific field. It&#39;s empty string for other databases. |






<a name="metaxisdata-store-FunctionMetadata"></a>

### FunctionMetadata
FunctionMetadata is the metadata for functions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the function. |
| definition | [string](#string) |  | The definition is the definition of a function. |
| signature | [string](#string) |  | The signature is the name with the number and type of input arguments the function takes. |
| character_set_client | [string](#string) |  | MySQL specific metadata. |
| collation_connection | [string](#string) |  |  |
| database_collation | [string](#string) |  |  |
| sql_mode | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| dependency_tables | [DependencyTable](#metaxisdata-store-DependencyTable) | repeated | The dependency_tables is the list of dependency tables of a function. For PostgreSQL, it&#39;s the list of tables that the function depends on the return type definition. |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-store-GenerationMetadata"></a>

### GenerationMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [GenerationMetadata.Type](#metaxisdata-store-GenerationMetadata-Type) |  |  |
| expression | [string](#string) |  |  |






<a name="metaxisdata-store-IndexMetadata"></a>

### IndexMetadata
IndexMetadata is the metadata for indexes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the index. |
| expressions | [string](#string) | repeated | The expressions are the ordered columns or expressions of an index. This could refer to a column or an expression. |
| key_length | [int64](#int64) | repeated | The ordered list of key lengths for the index. If the key length is not specified, it is -1. |
| descending | [bool](#bool) | repeated | The ordered list of descending flags for the index columns. |
| type | [string](#string) |  | The type is the type of an index. |
| unique | [bool](#bool) |  | The unique is whether the index is unique. |
| primary | [bool](#bool) |  | The primary is whether the index is a primary key index. |
| visible | [bool](#bool) |  | The visible is whether the index is visible. |
| comment | [string](#string) |  | The comment is the comment of an index. |
| definition | [string](#string) |  | The definition of an index. |
| parent_index_schema | [string](#string) |  | The schema name of the parent index. |
| parent_index_name | [string](#string) |  | The index name of the parent index. |
| is_constraint | [bool](#bool) |  | It&#39;s a PostgreSQL specific field. The unique constraint and unique index are not the same thing in PostgreSQL. |
| opclass_names | [string](#string) | repeated | https://www.postgresql.org/docs/current/catalog-pg-opclass.html Name of the operator class for each column. (PostgreSQL specific). |
| opclass_defaults | [bool](#bool) | repeated | True if the operator class is the default. (PostgreSQL specific). |






<a name="metaxisdata-store-ManualSQLMetadata"></a>

### ManualSQLMetadata
ManualSQLMetadata is the metadata for a user-maintained SQL definition.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| manual_sql_id | [string](#string) |  | The stable identifier of the manual SQL entry. |
| name | [string](#string) |  | The display name of the manual SQL entry. |
| title | [string](#string) |  | The title is optional display text for UI surfaces. |
| comment | [string](#string) |  | The comment is a free-form description. |
| sql_text | [string](#string) |  | The SQL text provided by the user. |
| tags | [string](#string) | repeated | Tags support exact filtering in the dedicated manual SQL query path. |
| attributes | [ManualSQLMetadata.AttributesEntry](#metaxisdata-store-ManualSQLMetadata-AttributesEntry) | repeated | Attributes provide custom key/value annotations. |
| schema_name | [string](#string) |  | The optional schema context used during analysis. |
| instance_resource | [string](#string) |  | The instance resource. Format: instances/{instance} |
| database_name | [string](#string) |  | The database name in the instance. |






<a name="metaxisdata-store-ManualSQLMetadata-AttributesEntry"></a>

### ManualSQLMetadata.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-store-MaterializedViewMetadata"></a>

### MaterializedViewMetadata
MaterializedViewMetadata is the metadata for materialized views.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the materialized view. |
| definition | [string](#string) |  | The definition is the definition of a view. |
| comment | [string](#string) |  | The comment is the comment of a view. |
| dependency_columns | [DependencyColumn](#metaxisdata-store-DependencyColumn) | repeated | The list of dependency columns of the view. |
| triggers | [TriggerMetadata](#metaxisdata-store-TriggerMetadata) | repeated | The ordered list of columns in the materialized view. |
| indexes | [IndexMetadata](#metaxisdata-store-IndexMetadata) | repeated | The list of indexes in the materialized view. |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-store-ProcedureMetadata"></a>

### ProcedureMetadata
ProcedureMetadata is the metadata for procedures.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the procedure. |
| definition | [string](#string) |  | The definition is the definition of a procedure. |
| signature | [string](#string) |  | The signature is the name with the number and type of input arguments the function takes. |
| character_set_client | [string](#string) |  | MySQL specific metadata. |
| collation_connection | [string](#string) |  |  |
| database_collation | [string](#string) |  |  |
| sql_mode | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-store-RuleMetadata"></a>

### RuleMetadata
RuleMetadata is the metadata for PostgreSQL rules.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the rule. |
| event | [string](#string) |  | The event type of the rule: SELECT, INSERT, UPDATE, or DELETE. |
| condition | [string](#string) |  | The WHERE condition of the rule (optional). |
| action | [string](#string) |  | The command(s) to execute when the rule fires. |
| is_instead | [bool](#bool) |  | The is_instead indicates whether this is an INSTEAD rule. |
| is_enabled | [bool](#bool) |  | The is_enabled indicates whether the rule is enabled. |
| definition | [string](#string) |  | The full CREATE RULE statement. |






<a name="metaxisdata-store-SchemaMetadata"></a>

### SchemaMetadata
SchemaMetadata is the metadata for schemas.
This is the concept of schema in Postgres, but it&#39;s a no-op for MySQL.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The schema name. It is an empty string for databases without such concept such as MySQL. |
| tables | [TableMetadata](#metaxisdata-store-TableMetadata) | repeated | The list of tables in a schema. |
| external_tables | [ExternalTableMetadata](#metaxisdata-store-ExternalTableMetadata) | repeated | The list of external tables in a schema. |
| views | [ViewMetadata](#metaxisdata-store-ViewMetadata) | repeated | The list of views in a schema. |
| functions | [FunctionMetadata](#metaxisdata-store-FunctionMetadata) | repeated | The list of functions in a schema. |
| procedures | [ProcedureMetadata](#metaxisdata-store-ProcedureMetadata) | repeated | The list of procedures in a schema. |
| materialized_views | [MaterializedViewMetadata](#metaxisdata-store-MaterializedViewMetadata) | repeated | The list of materialized views in a schema. |
| sequences | [SequenceMetadata](#metaxisdata-store-SequenceMetadata) | repeated | The list of sequences in a schema. |
| owner | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| events | [EventMetadata](#metaxisdata-store-EventMetadata) | repeated |  |
| enum_types | [EnumTypeMetadata](#metaxisdata-store-EnumTypeMetadata) | repeated |  |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-store-SequenceMetadata"></a>

### SequenceMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of a sequence. |
| data_type | [string](#string) |  | The data type of a sequence. |
| start | [string](#string) |  | The start value of a sequence. |
| min_value | [string](#string) |  | The minimum value of a sequence. |
| max_value | [string](#string) |  | The maximum value of a sequence. |
| increment | [string](#string) |  | The increment value of a sequence. |
| cycle | [bool](#bool) |  | Whether the sequence cycles. |
| cache_size | [string](#string) |  | Cache size of a sequence. |
| last_value | [string](#string) |  | The last value of a sequence. |
| owner_table | [string](#string) |  | The table that owns the sequence. |
| owner_column | [string](#string) |  | The column that owns the sequence. |
| comment | [string](#string) |  |  |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-store-StoredMetadata"></a>

### StoredMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| database_schema_metadata | [DatabaseSchemaMetadata](#metaxisdata-store-DatabaseSchemaMetadata) |  |  |
| schema_metadata | [SchemaMetadata](#metaxisdata-store-SchemaMetadata) |  |  |
| table_metadata | [TableMetadata](#metaxisdata-store-TableMetadata) |  |  |
| external_table_metadata | [ExternalTableMetadata](#metaxisdata-store-ExternalTableMetadata) |  |  |
| view_metadata | [ViewMetadata](#metaxisdata-store-ViewMetadata) |  |  |
| materialized_view_metadata | [MaterializedViewMetadata](#metaxisdata-store-MaterializedViewMetadata) |  |  |
| function_metadata | [FunctionMetadata](#metaxisdata-store-FunctionMetadata) |  |  |
| procedure_metadata | [ProcedureMetadata](#metaxisdata-store-ProcedureMetadata) |  |  |
| sequence_metadata | [SequenceMetadata](#metaxisdata-store-SequenceMetadata) |  |  |
| openlineage_run_summary | [OpenLineageRunSummary](#metaxisdata-store-OpenLineageRunSummary) |  |  |
| openlineage_task_summary | [OpenLineageTaskSummary](#metaxisdata-store-OpenLineageTaskSummary) |  |  |
| manual_sql_metadata | [ManualSQLMetadata](#metaxisdata-store-ManualSQLMetadata) |  |  |
| column_metadata | [ColumnMetadata](#metaxisdata-store-ColumnMetadata) |  |  |






<a name="metaxisdata-store-TableMetadata"></a>

### TableMetadata
TableMetadata is the metadata for tables.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the table. |
| columns | [ColumnMetadata](#metaxisdata-store-ColumnMetadata) | repeated | The columns is the ordered list of columns in a table. |
| indexes | [IndexMetadata](#metaxisdata-store-IndexMetadata) | repeated | The indexes is the list of indexes in a table. |
| engine | [string](#string) |  | The engine is the engine of a table. |
| collation | [string](#string) |  | The collation is the collation of a table. |
| charset | [string](#string) |  | The character set of the table. |
| row_count | [int64](#int64) |  | The row_count is the estimated number of rows of a table. |
| data_size | [int64](#int64) |  | The data_size is the estimated data size of a table. |
| index_size | [int64](#int64) |  | The index_size is the estimated index size of a table. |
| data_free | [int64](#int64) |  | The data_free is the estimated free data size of a table. |
| create_options | [string](#string) |  | The create_options is the create option of a table. |
| comment | [string](#string) |  | The comment is the comment of a table. classification and user_comment is parsed from the comment. |
| user_comment | [string](#string) |  | The user_comment is the user comment of a table parsed from the comment. |
| foreign_keys | [ForeignKeyMetadata](#metaxisdata-store-ForeignKeyMetadata) | repeated | The foreign_keys is the list of foreign keys in a table. |
| partitions | [TablePartitionMetadata](#metaxisdata-store-TablePartitionMetadata) | repeated | The partitions is the list of partitions in a table. |
| check_constraints | [CheckConstraintMetadata](#metaxisdata-store-CheckConstraintMetadata) | repeated | The check_constraints is the list of check constraints in a table. |
| owner | [string](#string) |  |  |
| sorting_keys | [string](#string) | repeated | The sorting_keys is a tuple of column names or arbitrary expressions. ClickHouse specific field. Reference: https://clickhouse.com/docs/en/engines/table-engines/mergetree-family/mergetree#order_by |
| triggers | [TriggerMetadata](#metaxisdata-store-TriggerMetadata) | repeated |  |
| skip_dump | [bool](#bool) |  |  |
| rules | [RuleMetadata](#metaxisdata-store-RuleMetadata) | repeated | The rules is the list of rules in a table (PostgreSQL specific). |
| sharding_info | [string](#string) |  | https://docs.pingcap.com/tidb/stable/information-schema-tables/ |
| primary_key_type | [string](#string) |  | https://docs.pingcap.com/tidb/stable/clustered-indexes/#clustered-indexes CLUSTERED or NONCLUSTERED. |
| exclude_constraints | [ExcludeConstraintMetadata](#metaxisdata-store-ExcludeConstraintMetadata) | repeated | The exclude_constraints is the list of EXCLUDE constraints in a table (PostgreSQL specific). |






<a name="metaxisdata-store-TablePartitionMetadata"></a>

### TablePartitionMetadata
TablePartitionMetadata is the metadata for table partitions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the table partition. |
| type | [TablePartitionMetadata.Type](#metaxisdata-store-TablePartitionMetadata-Type) |  | The type of a table partition. |
| expression | [string](#string) |  | The expression is the expression of a table partition. For PostgreSQL, the expression is the text of {FOR VALUES partition_bound_spec}, see https://www.postgresql.org/docs/current/sql-createtable.html. For MySQL, the expression is the `expr` or `column_list` of the following syntax. PARTITION BY { [LINEAR] HASH(expr) | [LINEAR] KEY [ALGORITHM={1 | 2}] (column_list) | RANGE{(expr) | COLUMNS(column_list)} | LIST{(expr) | COLUMNS(column_list)} }. |
| value | [string](#string) |  | The value is the value of a table partition. For MySQL, the value is for RANGE and LIST partition types, - For a RANGE partition, it contains the value set in the partition&#39;s VALUES LESS THAN clause, which can be either an integer or MAXVALUE. - For a LIST partition, this column contains the values defined in the partition&#39;s VALUES IN clause, which is a list of comma-separated integer values. - For others, it&#39;s an empty string. |
| use_default | [string](#string) |  | The use_default is whether the users use the default partition, it stores the different value for different database engines. For MySQL, it&#39;s [INT] type, 0 means not use default partition, otherwise, it&#39;s equals to number in syntax [SUB]PARTITION {number}. |
| subpartitions | [TablePartitionMetadata](#metaxisdata-store-TablePartitionMetadata) | repeated | The subpartitions is the list of subpartitions in a table partition. |
| indexes | [IndexMetadata](#metaxisdata-store-IndexMetadata) | repeated |  |
| check_constraints | [CheckConstraintMetadata](#metaxisdata-store-CheckConstraintMetadata) | repeated |  |
| exclude_constraints | [ExcludeConstraintMetadata](#metaxisdata-store-ExcludeConstraintMetadata) | repeated |  |






<a name="metaxisdata-store-TriggerMetadata"></a>

### TriggerMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the trigger. |
| event | [string](#string) |  | The event that triggers this action, such as INSERT, UPDATE, DELETE, or TRUNCATE. |
| timing | [string](#string) |  | The timing of when the trigger fires, such as BEFORE or AFTER. |
| body | [string](#string) |  | The body of the trigger. |
| sql_mode | [string](#string) |  |  |
| character_set_client | [string](#string) |  |  |
| collation_connection | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-store-ViewMetadata"></a>

### ViewMetadata
ViewMetadata is the metadata for views.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the view. |
| definition | [string](#string) |  | The definition is the definition of a view. |
| comment | [string](#string) |  | The comment is the comment of a view. |
| dependency_columns | [DependencyColumn](#metaxisdata-store-DependencyColumn) | repeated | The list of dependency columns of a view. |
| columns | [ColumnMetadata](#metaxisdata-store-ColumnMetadata) | repeated | The ordered list of columns in the view. |
| triggers | [TriggerMetadata](#metaxisdata-store-TriggerMetadata) | repeated | The list of triggers in the view. |
| skip_dump | [bool](#bool) |  |  |
| rules | [RuleMetadata](#metaxisdata-store-RuleMetadata) | repeated | The rules is the list of rules in a view (PostgreSQL specific). |





 


<a name="metaxisdata-store-ColumnMetadata-IdentityGeneration"></a>

### ColumnMetadata.IdentityGeneration


| Name | Number | Description |
| ---- | ------ | ----------- |
| IDENTITY_GENERATION_UNSPECIFIED | 0 |  |
| ALWAYS | 1 |  |
| BY_DEFAULT | 2 |  |



<a name="metaxisdata-store-GenerationMetadata-Type"></a>

### GenerationMetadata.Type


| Name | Number | Description |
| ---- | ------ | ----------- |
| TYPE_UNSPECIFIED | 0 |  |
| TYPE_VIRTUAL | 1 |  |
| TYPE_STORED | 2 |  |



<a name="metaxisdata-store-TablePartitionMetadata-Type"></a>

### TablePartitionMetadata.Type
The type is the type of a table partition. Some database engines may not
support all types. Only available for the following database engines now:
MySQL: RANGE, RANGE COLUMNS, LIST, LIST COLUMNS, HASH, LINEAR HASH, KEY,
LINEAR_KEY
(https://dev.mysql.com/doc/refman/8.0/en/partitioning-types.html) TiDB:
RANGE, RANGE COLUMNS, LIST, LIST COLUMNS, HASH, KEY PostgreSQL: RANGE,
LIST, HASH (https://www.postgresql.org/docs/current/ddl-partitioning.html)

| Name | Number | Description |
| ---- | ------ | ----------- |
| TYPE_UNSPECIFIED | 0 |  |
| RANGE | 1 |  |
| RANGE_COLUMNS | 2 |  |
| LIST | 3 |  |
| LIST_COLUMNS | 4 |  |
| HASH | 5 |  |
| LINEAR_HASH | 6 |  |
| KEY | 7 |  |
| LINEAR_KEY | 8 |  |


 

 

 



<a name="store_group-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/group.proto



<a name="metaxisdata-store-GroupMember"></a>

### GroupMember



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| member | [string](#string) |  | Member is the principal who belong to this group.

Format: users/{userUID}. |
| role | [GroupMember.Role](#metaxisdata-store-GroupMember-Role) |  |  |






<a name="metaxisdata-store-GroupPayload"></a>

### GroupPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| members | [GroupMember](#metaxisdata-store-GroupMember) | repeated |  |





 


<a name="metaxisdata-store-GroupMember-Role"></a>

### GroupMember.Role


| Name | Number | Description |
| ---- | ------ | ----------- |
| ROLE_UNSPECIFIED | 0 |  |
| OWNER | 1 |  |
| MEMBER | 2 |  |


 

 

 



<a name="store_idp-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/idp.proto



<a name="metaxisdata-store-FieldMapping"></a>

### FieldMapping
FieldMapping maps the field names of the identity provider user info response
onto the user info consumed at login time. Nothing is persisted: the raw
response and the mapped values only live for the duration of a login.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identifier | [string](#string) |  | Identifier is the field name of the unique identifier in 3rd-party idp user info. Required. |
| display_name | [string](#string) |  | DisplayName is the field name of display name in 3rd-party idp user info. Optional. |
| phone | [string](#string) |  | Phone is the field name of primary phone in 3rd-party idp user info. Optional. |
| groups | [string](#string) |  | Groups is the field name of groups in 3rd-party idp user info. Optional. Mainly used for OIDC: https://developer.okta.com/docs/guides/customize-tokens-groups-claim/main/ |






<a name="metaxisdata-store-IdentityProviderConfig"></a>

### IdentityProviderConfig



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| oauth2_config | [OAuth2IdentityProviderConfig](#metaxisdata-store-OAuth2IdentityProviderConfig) |  |  |






<a name="metaxisdata-store-IdentityProviderUserInfo"></a>

### IdentityProviderUserInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identifier | [string](#string) |  | Identifier is the value of the unique identifier in 3rd-party idp user info. |
| display_name | [string](#string) |  | DisplayName is the value of display name in 3rd-party idp user info. |
| phone | [string](#string) |  | Phone is the value of primary phone in 3rd-party idp user info. |
| groups | [string](#string) | repeated | Groups is the value of groups in 3rd-party idp user info. Mainly used for OIDC: https://developer.okta.com/docs/guides/customize-tokens-groups-claim/main/ |
| has_groups | [bool](#bool) |  |  |






<a name="metaxisdata-store-OAuth2IdentityProviderConfig"></a>

### OAuth2IdentityProviderConfig
OAuth2IdentityProviderConfig is the structure for OAuth2 identity provider config.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth_url | [string](#string) |  |  |
| token_url | [string](#string) |  |  |
| user_info_url | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| client_secret | [string](#string) |  |  |
| scopes | [string](#string) | repeated |  |
| field_mapping | [FieldMapping](#metaxisdata-store-FieldMapping) |  |  |
| skip_tls_verify | [bool](#bool) |  |  |
| auth_style | [OAuth2AuthStyle](#metaxisdata-store-OAuth2AuthStyle) |  |  |





 


<a name="metaxisdata-store-IdentityProviderType"></a>

### IdentityProviderType
IdentityProviderType is the type of a configured identity provider. Only
OAuth2 is implemented: the login path rejects anything else, and there is no
admin API that could create an OIDC/LDAP row.

| Name | Number | Description |
| ---- | ------ | ----------- |
| IDENTITY_PROVIDER_TYPE_UNSPECIFIED | 0 |  |
| OAUTH2 | 1 |  |



<a name="metaxisdata-store-OAuth2AuthStyle"></a>

### OAuth2AuthStyle


| Name | Number | Description |
| ---- | ------ | ----------- |
| OAUTH2_AUTH_STYLE_UNSPECIFIED | 0 |  |
| IN_PARAMS | 1 | IN_PARAMS sends the &#34;client_id&#34; and &#34;client_secret&#34; in the POST body as application/x-www-form-urlencoded parameters. |
| IN_HEADER | 2 | IN_HEADER sends the client_id and client_password using HTTP Basic Authorization. This is an optional style described in the OAuth2 RFC 6749 section 2.3.1. |


 

 

 



<a name="store_instance-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/instance.proto



<a name="metaxisdata-store-DataSource"></a>

### DataSource



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| type | [DataSourceType](#metaxisdata-store-DataSourceType) |  |  |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| obfuscated_password | [string](#string) |  |  |
| use_ssl | [bool](#bool) |  | Use SSL to connect to the data source. By default, we use the system&#39;s SSL configuration. |
| ssl_ca | [string](#string) |  |  |
| obfuscated_ssl_ca | [string](#string) |  |  |
| ssl_cert | [string](#string) |  |  |
| obfuscated_ssl_cert | [string](#string) |  |  |
| ssl_key | [string](#string) |  |  |
| obfuscated_ssl_key | [string](#string) |  |  |
| verify_tls_certificate | [bool](#bool) |  | verify_tls_certificate enables TLS certificate verification for SSL connections. Default is false (no verification) for backward compatibility. Set to true for secure connections (recommended for production). Only set to false for development or when certificates cannot be properly validated (e.g., self-signed certs, VPN environments). |
| host | [string](#string) |  |  |
| port | [string](#string) |  |  |
| database | [string](#string) |  |  |
| ssh_host | [string](#string) |  | SSH related The hostname of the SSH server agent. |
| ssh_port | [string](#string) |  | The port of the SSH server agent. It&#39;s 22 typically. |
| ssh_user | [string](#string) |  | The user to login the server. |
| ssh_password | [string](#string) |  | The password to login the server. If it&#39;s empty string, no password is required. |
| obfuscated_ssh_password | [string](#string) |  |  |
| ssh_private_key | [string](#string) |  | The private key to login the server. If it&#39;s empty string, we will use the system default private key from os.Getenv(&#34;SSH_AUTH_SOCK&#34;). |
| obfuscated_ssh_private_key | [string](#string) |  |  |
| extra_connection_parameters | [DataSource.ExtraConnectionParametersEntry](#metaxisdata-store-DataSource-ExtraConnectionParametersEntry) | repeated | Extra connection parameters for the database connection. For PostgreSQL HA, this can be used to set target_session_attrs=read-write |






<a name="metaxisdata-store-DataSource-ExtraConnectionParametersEntry"></a>

### DataSource.ExtraConnectionParametersEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-store-Instance"></a>

### Instance
Instance is the proto for instances.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  |  |
| engine | [Engine](#metaxisdata-store-Engine) |  |  |
| activation | [bool](#bool) |  |  |
| version | [string](#string) |  |  |
| external_link | [string](#string) |  |  |
| data_sources | [DataSource](#metaxisdata-store-DataSource) | repeated |  |
| sync_interval | [google.protobuf.Duration](#google-protobuf-Duration) |  | The interval between automatic instance synchronizations. |
| maximum_connections | [int32](#int32) |  | The maximum number of connections. The default is 10 if the value is unset or zero. |
| sync_databases | [string](#string) | repeated | Enable sync for the following databases. Default empty, means sync all schemas &amp; databases. |
| mysql_lower_case_table_names | [int32](#int32) |  | The lower_case_table_names config for MySQL instances. It is used to determine whether the table names and database names are case sensitive. |
| last_sync_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 


<a name="metaxisdata-store-DataSourceType"></a>

### DataSourceType


| Name | Number | Description |
| ---- | ------ | ----------- |
| DATA_SOURCE_UNSPECIFIED | 0 |  |
| ADMIN | 1 |  |
| READ_ONLY | 2 |  |


 

 

 



<a name="store_llm-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/llm.proto



<a name="metaxisdata-store-LlmProviderModel"></a>

### LlmProviderModel



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| enabled | [bool](#bool) |  |  |






<a name="metaxisdata-store-LlmProviderProfile"></a>

### LlmProviderProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| title | [string](#string) |  |  |
| type | [LLMProviderType](#metaxisdata-store-LLMProviderType) |  |  |
| base_url | [string](#string) |  |  |
| api_key_encrypted | [string](#string) |  |  |
| models | [LlmProviderModel](#metaxisdata-store-LlmProviderModel) | repeated |  |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| update_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 


<a name="metaxisdata-store-LLMProviderType"></a>

### LLMProviderType


| Name | Number | Description |
| ---- | ------ | ----------- |
| LLM_PROVIDER_TYPE_UNSPECIFIED | 0 |  |
| LLM_PROVIDER_TYPE_OPENAI | 1 |  |
| LLM_PROVIDER_TYPE_DEEPSEEK | 2 |  |
| LLM_PROVIDER_TYPE_OPENROUTER | 3 |  |
| LLM_PROVIDER_TYPE_CUSTOM | 4 |  |


 

 

 



<a name="store_meta-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/meta.proto


 


<a name="metaxisdata-store-MetaType"></a>

### MetaType


| Name | Number | Description |
| ---- | ------ | ----------- |
| UNSPECIFIED | 0 |  |
| INSTANCE | 1 |  |
| DATABASE | 2 |  |
| SCHEMA | 3 |  |
| TABLE | 4 |  |
| EXTERNAL_TABLE | 16 |  |
| EXTERNAL_DATASET | 17 |  |
| MANUAL_SQL | 18 |  |
| VIEW | 5 |  |
| MATERIALIZED_VIEW | 6 |  |
| COLUMN | 7 |  |
| INDEX | 8 |  |
| FOREIGN_KEY | 9 |  |
| PROCEDURE | 10 |  |
| FUNCTION | 11 |  |
| SEQUENCE | 12 |  |
| OPENLINEAGE | 100 | for Non-database internal structure |


 

 

 



<a name="store_policy-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/policy.proto



<a name="metaxisdata-store-Binding"></a>

### Binding
The policy table stores the workspace IAM policy as IamPolicy. TagPolicy and
EnvironmentTierPolicy were deleted: no code ever produced or consumed them.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [string](#string) |  | The role that is assigned to the members. Format: roles/{role} |
| members | [string](#string) | repeated | Specifies the principals requesting access for a resource. For users, the member should be: users/{userUID} For groups, the member should be: groups/{email} |
| condition | [google.type.Expr](#google-type-Expr) |  | The condition that is associated with this binding. If the condition evaluates to true, then this binding applies to the current request. If the condition evaluates to false, then this binding does not apply to the current request. However, a different role binding might grant the same role to one or more of the principals in this binding. |






<a name="metaxisdata-store-IamPolicy"></a>

### IamPolicy



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| bindings | [Binding](#metaxisdata-store-Binding) | repeated | Collection of binding. A binding binds one or more members or groups to a single role. |






<a name="metaxisdata-store-Policy"></a>

### Policy
Policy enumerates the policy rows the store can query. Only the IAM type and
the WORKSPACE resource are produced; the enum values are kept because they are
written to the policy table&#39;s text columns and legacy rows may carry the
others.





 


<a name="metaxisdata-store-Policy-Resource"></a>

### Policy.Resource


| Name | Number | Description |
| ---- | ------ | ----------- |
| RESOURCE_UNSPECIFIED | 0 |  |
| WORKSPACE | 1 |  |
| ENVIRONMENT | 2 |  |
| PROJECT | 3 |  |



<a name="metaxisdata-store-Policy-Type"></a>

### Policy.Type


| Name | Number | Description |
| ---- | ------ | ----------- |
| TYPE_UNSPECIFIED | 0 |  |
| IAM | 1 |  |
| TAG | 2 |  |


 

 

 



<a name="store_role-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/role.proto



<a name="metaxisdata-store-RolePermissions"></a>

### RolePermissions
RolePermissions is the payload stored in the role table&#39;s permissions JSONB
column: the permission bundle of a custom role. Predefined roles are defined
in Go (backend/store/predefined_roles.go) and never stored, so only custom
roles have a row here.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| permissions | [string](#string) | repeated |  |





 

 

 

 



<a name="store_setting-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/setting.proto



<a name="metaxisdata-store-EnvironmentSetting"></a>

### EnvironmentSetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| environments | [EnvironmentSetting.Environment](#metaxisdata-store-EnvironmentSetting-Environment) | repeated |  |






<a name="metaxisdata-store-EnvironmentSetting-Environment"></a>

### EnvironmentSetting.Environment



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | The resource id of the environment. This value should be 4-63 characters, and valid characters are /[a-z0-9-]/. |
| title | [string](#string) |  | The display name of the environment. |
| tags | [EnvironmentSetting.Environment.TagsEntry](#metaxisdata-store-EnvironmentSetting-Environment-TagsEntry) | repeated |  |
| color | [string](#string) |  |  |






<a name="metaxisdata-store-EnvironmentSetting-Environment-TagsEntry"></a>

### EnvironmentSetting.Environment.TagsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-store-PasswordRestrictionSetting"></a>

### PasswordRestrictionSetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| min_length | [int32](#int32) |  | min_length is the minimum length for password, should no less than 8. |
| require_number | [bool](#bool) |  | require_number requires the password must contains at least one number. |
| require_letter | [bool](#bool) |  | require_letter requires the password must contains at least one letter, regardless of upper case or lower case |
| require_uppercase_letter | [bool](#bool) |  | require_uppercase_letter requires the password must contains at least one upper case letter. |
| require_special_character | [bool](#bool) |  | require_uppercase_letter requires the password must contains at least one special character. |
| require_reset_password_for_first_login | [bool](#bool) |  | require_reset_password_for_first_login requires users to reset their password after the 1st login. |
| password_rotation | [google.protobuf.Duration](#google-protobuf-Duration) |  | password_rotation requires users to reset their password after the duration. |






<a name="metaxisdata-store-WorkspaceProfileSetting"></a>

### WorkspaceProfileSetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| external_url | [string](#string) |  | The external URL is used for sso authentication callback. |
| disallow_signup | [bool](#bool) |  | Disallow self-service signup, users can only be invited by the owner. |
| domains | [string](#string) | repeated | The workspace domain, e.g. example.com. |
| enforce_identity_domain | [bool](#bool) |  | Only user and group from the domains can be created and login. |
| disallow_password_signin | [bool](#bool) |  | Whether to disallow password signin. (Except workspace admins) |
| openlineage_retention_days | [int32](#int32) |  | openlineage_retention_days bounds how long persisted OpenLineage runs are kept. Zero (the default) keeps them forever: they are audit data. |





 


<a name="metaxisdata-store-SettingName"></a>

### SettingName
SettingName is the name of a workspace setting. Only the settings the server
actually reads or writes are kept; the approval/IM/watermark/AI/CEL/SCIM
leftovers keep their numbers reserved.

| Name | Number | Description |
| ---- | ------ | ----------- |
| SETTING_NAME_UNSPECIFIED | 0 |  |
| AUTH_SECRET | 1 |  |
| BRANDING_LOGO | 2 |  |
| WORKSPACE_ID | 3 |  |
| WORKSPACE_PROFILE | 4 |  |
| PASSWORD_RESTRICTION | 14 |  |
| ENVIRONMENT | 15 |  |


 

 

 



<a name="store_user-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/user.proto



<a name="metaxisdata-store-UserProfile"></a>

### UserProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| last_login_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_change_password_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 


<a name="metaxisdata-store-PrincipalType"></a>

### PrincipalType
PrincipalType is the type of a principal.

| Name | Number | Description |
| ---- | ------ | ----------- |
| PRINCIPAL_TYPE_UNSPECIFIED | 0 |  |
| END_USER | 1 | END_USER represents the human being. |
| SERVICE_ACCOUNT | 2 | SERVICE_ACCOUNT represents the external service calling OpenAPI. |
| SYSTEM_BOT | 3 | SYSTEM_BOT represents the internal system bot performing operations. |


 

 

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

