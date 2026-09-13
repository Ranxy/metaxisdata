# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [v1/annotation.proto](#v1_annotation-proto)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
  
- [v1/audit_log_service.proto](#v1_audit_log_service-proto)
    - [AuditLog](#metaxisdata-v1-AuditLog)
    - [AuditLogStatus](#metaxisdata-v1-AuditLogStatus)
    - [AuditRequestMetadata](#metaxisdata-v1-AuditRequestMetadata)
    - [ListAuditLogsRequest](#metaxisdata-v1-ListAuditLogsRequest)
    - [ListAuditLogsResponse](#metaxisdata-v1-ListAuditLogsResponse)
  
    - [AuditLogSeverity](#metaxisdata-v1-AuditLogSeverity)
  
    - [AuditLogService](#metaxisdata-v1-AuditLogService)
  
- [v1/common.proto](#v1_common-proto)
    - [Engine](#metaxisdata-v1-Engine)
    - [State](#metaxisdata-v1-State)
  
- [v1/user_service.proto](#v1_user_service-proto)
    - [BatchGetUsersRequest](#metaxisdata-v1-BatchGetUsersRequest)
    - [BatchGetUsersResponse](#metaxisdata-v1-BatchGetUsersResponse)
    - [CreateUserRequest](#metaxisdata-v1-CreateUserRequest)
    - [DeleteUserRequest](#metaxisdata-v1-DeleteUserRequest)
    - [GetUserRequest](#metaxisdata-v1-GetUserRequest)
    - [ListUsersRequest](#metaxisdata-v1-ListUsersRequest)
    - [ListUsersResponse](#metaxisdata-v1-ListUsersResponse)
    - [UndeleteUserRequest](#metaxisdata-v1-UndeleteUserRequest)
    - [UpdateUserRequest](#metaxisdata-v1-UpdateUserRequest)
    - [User](#metaxisdata-v1-User)
    - [UserProfile](#metaxisdata-v1-UserProfile)
  
    - [UserType](#metaxisdata-v1-UserType)
  
    - [UserService](#metaxisdata-v1-UserService)
  
- [v1/auth_service.proto](#v1_auth_service-proto)
    - [CreateSSOStateResponse](#metaxisdata-v1-CreateSSOStateResponse)
    - [IdentityProviderContext](#metaxisdata-v1-IdentityProviderContext)
    - [LoginRequest](#metaxisdata-v1-LoginRequest)
    - [LoginResponse](#metaxisdata-v1-LoginResponse)
    - [LogoutRequest](#metaxisdata-v1-LogoutRequest)
    - [OAuth2IdentityProviderContext](#metaxisdata-v1-OAuth2IdentityProviderContext)
  
    - [AuthService](#metaxisdata-v1-AuthService)
  
- [v1/instance_service.proto](#v1_instance_service-proto)
    - [BatchSyncInstanceResult](#metaxisdata-v1-BatchSyncInstanceResult)
    - [BatchSyncInstancesRequest](#metaxisdata-v1-BatchSyncInstancesRequest)
    - [BatchSyncInstancesResponse](#metaxisdata-v1-BatchSyncInstancesResponse)
    - [BatchUpdateInstancesRequest](#metaxisdata-v1-BatchUpdateInstancesRequest)
    - [BatchUpdateInstancesResponse](#metaxisdata-v1-BatchUpdateInstancesResponse)
    - [CreateDataSourceRequest](#metaxisdata-v1-CreateDataSourceRequest)
    - [CreateInstanceRequest](#metaxisdata-v1-CreateInstanceRequest)
    - [DataSource](#metaxisdata-v1-DataSource)
    - [DataSource.ExtraConnectionParametersEntry](#metaxisdata-v1-DataSource-ExtraConnectionParametersEntry)
    - [DeleteDataSourceRequest](#metaxisdata-v1-DeleteDataSourceRequest)
    - [DeleteInstanceRequest](#metaxisdata-v1-DeleteInstanceRequest)
    - [GetInstanceRequest](#metaxisdata-v1-GetInstanceRequest)
    - [Instance](#metaxisdata-v1-Instance)
    - [InstanceResource](#metaxisdata-v1-InstanceResource)
    - [ListInstancesRequest](#metaxisdata-v1-ListInstancesRequest)
    - [ListInstancesResponse](#metaxisdata-v1-ListInstancesResponse)
    - [SyncInstanceRequest](#metaxisdata-v1-SyncInstanceRequest)
    - [SyncInstanceResponse](#metaxisdata-v1-SyncInstanceResponse)
    - [UndeleteInstanceRequest](#metaxisdata-v1-UndeleteInstanceRequest)
    - [UpdateDataSourceRequest](#metaxisdata-v1-UpdateDataSourceRequest)
    - [UpdateInstanceRequest](#metaxisdata-v1-UpdateInstanceRequest)
  
    - [DataSourceType](#metaxisdata-v1-DataSourceType)
  
    - [InstanceService](#metaxisdata-v1-InstanceService)
  
- [v1/database_service.proto](#v1_database_service-proto)
    - [CheckConstraintMetadata](#metaxisdata-v1-CheckConstraintMetadata)
    - [ColumnMetadata](#metaxisdata-v1-ColumnMetadata)
    - [CreateManualSQLRequest](#metaxisdata-v1-CreateManualSQLRequest)
    - [Database](#metaxisdata-v1-Database)
    - [Database.LabelsEntry](#metaxisdata-v1-Database-LabelsEntry)
    - [DatabaseSchemaMetadata](#metaxisdata-v1-DatabaseSchemaMetadata)
    - [DeleteManualSQLRequest](#metaxisdata-v1-DeleteManualSQLRequest)
    - [DependencyColumn](#metaxisdata-v1-DependencyColumn)
    - [DependencyTable](#metaxisdata-v1-DependencyTable)
    - [DiffMetadataRequest](#metaxisdata-v1-DiffMetadataRequest)
    - [DiffMetadataResponse](#metaxisdata-v1-DiffMetadataResponse)
    - [EnumTypeMetadata](#metaxisdata-v1-EnumTypeMetadata)
    - [EventMetadata](#metaxisdata-v1-EventMetadata)
    - [EventTriggerMetadata](#metaxisdata-v1-EventTriggerMetadata)
    - [ExcludeConstraintMetadata](#metaxisdata-v1-ExcludeConstraintMetadata)
    - [ExtensionMetadata](#metaxisdata-v1-ExtensionMetadata)
    - [ExternalTableMetadata](#metaxisdata-v1-ExternalTableMetadata)
    - [ForeignKeyMetadata](#metaxisdata-v1-ForeignKeyMetadata)
    - [FunctionMetadata](#metaxisdata-v1-FunctionMetadata)
    - [GenerationMetadata](#metaxisdata-v1-GenerationMetadata)
    - [GetManualSQLRequest](#metaxisdata-v1-GetManualSQLRequest)
    - [GetMetadataHistoryEventRequest](#metaxisdata-v1-GetMetadataHistoryEventRequest)
    - [GetMetadataRequest](#metaxisdata-v1-GetMetadataRequest)
    - [GetMetadataResponse](#metaxisdata-v1-GetMetadataResponse)
    - [GetSchemaStringRequest](#metaxisdata-v1-GetSchemaStringRequest)
    - [IndexMetadata](#metaxisdata-v1-IndexMetadata)
    - [ListDatabasesRequest](#metaxisdata-v1-ListDatabasesRequest)
    - [ListDatabasesResponse](#metaxisdata-v1-ListDatabasesResponse)
    - [ListManualSQLsRequest](#metaxisdata-v1-ListManualSQLsRequest)
    - [ListManualSQLsResponse](#metaxisdata-v1-ListManualSQLsResponse)
    - [ListMetadataHistoryRequest](#metaxisdata-v1-ListMetadataHistoryRequest)
    - [ListMetadataHistoryResponse](#metaxisdata-v1-ListMetadataHistoryResponse)
    - [ListMetadataRequest](#metaxisdata-v1-ListMetadataRequest)
    - [ManualSQL](#metaxisdata-v1-ManualSQL)
    - [ManualSQL.AttributesEntry](#metaxisdata-v1-ManualSQL-AttributesEntry)
    - [ManualSQLMetadata](#metaxisdata-v1-ManualSQLMetadata)
    - [ManualSQLMetadata.AttributesEntry](#metaxisdata-v1-ManualSQLMetadata-AttributesEntry)
    - [MaterializedViewMetadata](#metaxisdata-v1-MaterializedViewMetadata)
    - [MetadataFieldChange](#metaxisdata-v1-MetadataFieldChange)
    - [MetadataHistoryChangeGroup](#metaxisdata-v1-MetadataHistoryChangeGroup)
    - [MetadataHistoryChangeItem](#metaxisdata-v1-MetadataHistoryChangeItem)
    - [MetadataHistoryChildSnapshot](#metaxisdata-v1-MetadataHistoryChildSnapshot)
    - [MetadataHistoryEvent](#metaxisdata-v1-MetadataHistoryEvent)
    - [MetadataHistorySectionChangeCount](#metaxisdata-v1-MetadataHistorySectionChangeCount)
    - [MetadataHistoryTimelineEntry](#metaxisdata-v1-MetadataHistoryTimelineEntry)
    - [MetadataResponse](#metaxisdata-v1-MetadataResponse)
    - [MetadataResponse.Metadata](#metaxisdata-v1-MetadataResponse-Metadata)
    - [MetadataSchemaString](#metaxisdata-v1-MetadataSchemaString)
    - [ProcedureMetadata](#metaxisdata-v1-ProcedureMetadata)
    - [RuleMetadata](#metaxisdata-v1-RuleMetadata)
    - [SchemaMetadata](#metaxisdata-v1-SchemaMetadata)
    - [SearchManualSQLRequest](#metaxisdata-v1-SearchManualSQLRequest)
    - [SearchManualSQLResponse](#metaxisdata-v1-SearchManualSQLResponse)
    - [SearchMetadataRequest](#metaxisdata-v1-SearchMetadataRequest)
    - [SearchMetadataResponse](#metaxisdata-v1-SearchMetadataResponse)
    - [SearchMetadataResult](#metaxisdata-v1-SearchMetadataResult)
    - [SequenceMetadata](#metaxisdata-v1-SequenceMetadata)
    - [StoredMetadata](#metaxisdata-v1-StoredMetadata)
    - [SyncDatabaseRequest](#metaxisdata-v1-SyncDatabaseRequest)
    - [SyncDatabaseResponse](#metaxisdata-v1-SyncDatabaseResponse)
    - [TableMetadata](#metaxisdata-v1-TableMetadata)
    - [TablePartitionMetadata](#metaxisdata-v1-TablePartitionMetadata)
    - [TriggerMetadata](#metaxisdata-v1-TriggerMetadata)
    - [UpdateManualSQLRequest](#metaxisdata-v1-UpdateManualSQLRequest)
    - [ViewMetadata](#metaxisdata-v1-ViewMetadata)
  
    - [ColumnMetadata.IdentityGeneration](#metaxisdata-v1-ColumnMetadata-IdentityGeneration)
    - [GenerationMetadata.Type](#metaxisdata-v1-GenerationMetadata-Type)
    - [MetaType](#metaxisdata-v1-MetaType)
    - [MetadataHistoryOperation](#metaxisdata-v1-MetadataHistoryOperation)
    - [MetadataHistorySection](#metaxisdata-v1-MetadataHistorySection)
    - [TablePartitionMetadata.Type](#metaxisdata-v1-TablePartitionMetadata-Type)
  
    - [DatabaseService](#metaxisdata-v1-DatabaseService)
  
- [v1/explain_sql_service.proto](#v1_explain_sql_service-proto)
    - [ExplainSQLMetadata](#metaxisdata-v1-ExplainSQLMetadata)
    - [ExplainSQLProgress](#metaxisdata-v1-ExplainSQLProgress)
    - [ExplainSQLRequest](#metaxisdata-v1-ExplainSQLRequest)
    - [ExplainSQLResponse](#metaxisdata-v1-ExplainSQLResponse)
  
    - [ExplainSQLService](#metaxisdata-v1-ExplainSQLService)
  
- [v1/group_service.proto](#v1_group_service-proto)
    - [CreateGroupRequest](#metaxisdata-v1-CreateGroupRequest)
    - [DeleteGroupRequest](#metaxisdata-v1-DeleteGroupRequest)
    - [GetGroupRequest](#metaxisdata-v1-GetGroupRequest)
    - [Group](#metaxisdata-v1-Group)
    - [GroupMember](#metaxisdata-v1-GroupMember)
    - [ListGroupsRequest](#metaxisdata-v1-ListGroupsRequest)
    - [ListGroupsResponse](#metaxisdata-v1-ListGroupsResponse)
    - [UpdateGroupRequest](#metaxisdata-v1-UpdateGroupRequest)
  
    - [GroupMember.Role](#metaxisdata-v1-GroupMember-Role)
  
    - [GroupService](#metaxisdata-v1-GroupService)
  
- [v1/iam_service.proto](#v1_iam_service-proto)
    - [Binding](#metaxisdata-v1-Binding)
    - [GetWorkspaceIamPolicyRequest](#metaxisdata-v1-GetWorkspaceIamPolicyRequest)
    - [IamPolicy](#metaxisdata-v1-IamPolicy)
    - [IamPolicyView](#metaxisdata-v1-IamPolicyView)
    - [SetWorkspaceIamPolicyRequest](#metaxisdata-v1-SetWorkspaceIamPolicyRequest)
  
    - [IamService](#metaxisdata-v1-IamService)
  
- [v1/lineage_service.proto](#v1_lineage_service-proto)
    - [ExternalDatasetInfo](#metaxisdata-v1-ExternalDatasetInfo)
    - [GetLineageForContextRequest](#metaxisdata-v1-GetLineageForContextRequest)
    - [GetLineageForContextResponse](#metaxisdata-v1-GetLineageForContextResponse)
    - [GetLineageRequest](#metaxisdata-v1-GetLineageRequest)
    - [GetLineageResponse](#metaxisdata-v1-GetLineageResponse)
    - [LineageRelation](#metaxisdata-v1-LineageRelation)
    - [Transformation](#metaxisdata-v1-Transformation)
  
    - [LineageType](#metaxisdata-v1-LineageType)
    - [RelationType](#metaxisdata-v1-RelationType)
  
    - [LineageService](#metaxisdata-v1-LineageService)
  
- [v1/llm_service.proto](#v1_llm_service-proto)
    - [CreateLLMProviderProfileRequest](#metaxisdata-v1-CreateLLMProviderProfileRequest)
    - [DeleteLLMProviderProfileRequest](#metaxisdata-v1-DeleteLLMProviderProfileRequest)
    - [FetchLLMModelsRequest](#metaxisdata-v1-FetchLLMModelsRequest)
    - [FetchLLMModelsResponse](#metaxisdata-v1-FetchLLMModelsResponse)
    - [ListLLMProviderProfilesRequest](#metaxisdata-v1-ListLLMProviderProfilesRequest)
    - [ListLLMProviderProfilesResponse](#metaxisdata-v1-ListLLMProviderProfilesResponse)
    - [LlmProviderDefinition](#metaxisdata-v1-LlmProviderDefinition)
    - [LlmProviderModel](#metaxisdata-v1-LlmProviderModel)
    - [LlmProviderProfile](#metaxisdata-v1-LlmProviderProfile)
    - [UpdateLLMProviderProfileRequest](#metaxisdata-v1-UpdateLLMProviderProfileRequest)
  
    - [LLMProviderType](#metaxisdata-v1-LLMProviderType)
  
    - [LLMService](#metaxisdata-v1-LLMService)
  
- [v1/openlineage_service.proto](#v1_openlineage_service-proto)
    - [APIKey](#metaxisdata-v1-APIKey)
    - [CreateAPIKeyRequest](#metaxisdata-v1-CreateAPIKeyRequest)
    - [CreateAPIKeyResponse](#metaxisdata-v1-CreateAPIKeyResponse)
    - [CreateNamespaceMappingRequest](#metaxisdata-v1-CreateNamespaceMappingRequest)
    - [DeleteNamespaceMappingRequest](#metaxisdata-v1-DeleteNamespaceMappingRequest)
    - [GetOpenLineageDatasetRequest](#metaxisdata-v1-GetOpenLineageDatasetRequest)
    - [GetOpenLineageRunRequest](#metaxisdata-v1-GetOpenLineageRunRequest)
    - [GetOpenLineageTaskRequest](#metaxisdata-v1-GetOpenLineageTaskRequest)
    - [ListAPIKeysRequest](#metaxisdata-v1-ListAPIKeysRequest)
    - [ListAPIKeysResponse](#metaxisdata-v1-ListAPIKeysResponse)
    - [ListNamespaceMappingsRequest](#metaxisdata-v1-ListNamespaceMappingsRequest)
    - [ListNamespaceMappingsResponse](#metaxisdata-v1-ListNamespaceMappingsResponse)
    - [ListOpenLineageDatasetsRequest](#metaxisdata-v1-ListOpenLineageDatasetsRequest)
    - [ListOpenLineageDatasetsResponse](#metaxisdata-v1-ListOpenLineageDatasetsResponse)
    - [ListOpenLineageRunsRequest](#metaxisdata-v1-ListOpenLineageRunsRequest)
    - [ListOpenLineageRunsResponse](#metaxisdata-v1-ListOpenLineageRunsResponse)
    - [ListOpenLineageTasksRequest](#metaxisdata-v1-ListOpenLineageTasksRequest)
    - [ListOpenLineageTasksResponse](#metaxisdata-v1-ListOpenLineageTasksResponse)
    - [NamespaceMapping](#metaxisdata-v1-NamespaceMapping)
    - [OpenLineageDatasetDetailResource](#metaxisdata-v1-OpenLineageDatasetDetailResource)
    - [OpenLineageDatasetField](#metaxisdata-v1-OpenLineageDatasetField)
    - [OpenLineageDatasetJobResource](#metaxisdata-v1-OpenLineageDatasetJobResource)
    - [OpenLineageDatasetResource](#metaxisdata-v1-OpenLineageDatasetResource)
    - [OpenLineageDatasetRunResource](#metaxisdata-v1-OpenLineageDatasetRunResource)
    - [OpenLineageRun](#metaxisdata-v1-OpenLineageRun)
    - [OpenLineageTask](#metaxisdata-v1-OpenLineageTask)
    - [RevokeAPIKeyRequest](#metaxisdata-v1-RevokeAPIKeyRequest)
    - [UpdateNamespaceMappingRequest](#metaxisdata-v1-UpdateNamespaceMappingRequest)
  
    - [OpenLineageDatasetScope](#metaxisdata-v1-OpenLineageDatasetScope)
  
    - [OpenLineageService](#metaxisdata-v1-OpenLineageService)
  
- [v1/role_service.proto](#v1_role_service-proto)
    - [CreateRoleRequest](#metaxisdata-v1-CreateRoleRequest)
    - [DeleteRoleRequest](#metaxisdata-v1-DeleteRoleRequest)
    - [GetRoleRequest](#metaxisdata-v1-GetRoleRequest)
    - [ListRolesRequest](#metaxisdata-v1-ListRolesRequest)
    - [ListRolesResponse](#metaxisdata-v1-ListRolesResponse)
    - [Role](#metaxisdata-v1-Role)
    - [UpdateRoleRequest](#metaxisdata-v1-UpdateRoleRequest)
  
    - [RoleService](#metaxisdata-v1-RoleService)
  
- [v1/setting_service.proto](#v1_setting_service-proto)
    - [GetDebugConfigRequest](#metaxisdata-v1-GetDebugConfigRequest)
    - [GetDebugConfigResponse](#metaxisdata-v1-GetDebugConfigResponse)
    - [GetWorkspaceProfileSettingRequest](#metaxisdata-v1-GetWorkspaceProfileSettingRequest)
    - [UpdateDebugConfigRequest](#metaxisdata-v1-UpdateDebugConfigRequest)
    - [UpdateDebugConfigResponse](#metaxisdata-v1-UpdateDebugConfigResponse)
    - [UpdateWorkspaceProfileSettingRequest](#metaxisdata-v1-UpdateWorkspaceProfileSettingRequest)
    - [WorkspaceProfileSetting](#metaxisdata-v1-WorkspaceProfileSetting)
  
    - [SettingService](#metaxisdata-v1-SettingService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="v1_annotation-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/annotation.proto


 

 


<a name="v1_annotation-proto-extensions"></a>

### File-level Extensions
| Extension | Type | Base | Number | Description |
| --------- | ---- | ---- | ------ | ----------- |
| allow_without_credential | bool | .google.protobuf.MethodOptions | 100000 |  |
| audit | bool | .google.protobuf.MethodOptions | 100003 |  |
| permission | string | .google.protobuf.MethodOptions | 100001 |  |

 

 



<a name="v1_audit_log_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/audit_log_service.proto



<a name="metaxisdata-v1-AuditLog"></a>

### AuditLog



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| parent | [string](#string) |  |  |
| method | [string](#string) |  |  |
| resource | [string](#string) |  |  |
| user | [string](#string) |  |  |
| severity | [AuditLogSeverity](#metaxisdata-v1-AuditLogSeverity) |  |  |
| request | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| response | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| status | [AuditLogStatus](#metaxisdata-v1-AuditLogStatus) |  |  |
| latency_ms | [int64](#int64) |  |  |
| service_data | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| request_metadata | [AuditRequestMetadata](#metaxisdata-v1-AuditRequestMetadata) |  |  |






<a name="metaxisdata-v1-AuditLogStatus"></a>

### AuditLogStatus



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [int32](#int32) |  |  |
| message | [string](#string) |  |  |






<a name="metaxisdata-v1-AuditRequestMetadata"></a>

### AuditRequestMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| ip | [string](#string) |  |  |
| user_agent | [string](#string) |  |  |






<a name="metaxisdata-v1-ListAuditLogsRequest"></a>

### ListAuditLogsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| filter | [string](#string) |  |  |






<a name="metaxisdata-v1-ListAuditLogsResponse"></a>

### ListAuditLogsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| audit_logs | [AuditLog](#metaxisdata-v1-AuditLog) | repeated |  |
| next_page_token | [string](#string) |  |  |





 


<a name="metaxisdata-v1-AuditLogSeverity"></a>

### AuditLogSeverity


| Name | Number | Description |
| ---- | ------ | ----------- |
| AUDIT_LOG_SEVERITY_UNSPECIFIED | 0 |  |
| INFO | 1 |  |
| WARNING | 2 |  |
| ERROR | 3 |  |


 

 


<a name="metaxisdata-v1-AuditLogService"></a>

### AuditLogService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListAuditLogs | [ListAuditLogsRequest](#metaxisdata-v1-ListAuditLogsRequest) | [ListAuditLogsResponse](#metaxisdata-v1-ListAuditLogsResponse) |  |

 



<a name="v1_common-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/common.proto


 


<a name="metaxisdata-v1-Engine"></a>

### Engine
Engine is the database engine of an instance. Only the engines the product
actually supports are kept: MySQL-compatible ones (MySQL, TiDB, MariaDB,
OceanBase) and PostgreSQL. The removed Bytebase-era values keep their numbers
reserved so they can never be silently reused for something else.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ENGINE_UNSPECIFIED | 0 |  |
| MYSQL | 2 |  |
| POSTGRES | 3 |  |
| TIDB | 6 |  |
| MARIADB | 13 |  |
| OCEANBASE | 14 |  |



<a name="metaxisdata-v1-State"></a>

### State


| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNSPECIFIED | 0 |  |
| ACTIVE | 1 |  |
| DELETED | 2 |  |


 

 

 



<a name="v1_user_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/user_service.proto



<a name="metaxisdata-v1-BatchGetUsersRequest"></a>

### BatchGetUsersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| names | [string](#string) | repeated | The user names to retrieve. Format: users/{user uid or user email} |






<a name="metaxisdata-v1-BatchGetUsersResponse"></a>

### BatchGetUsersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| users | [User](#metaxisdata-v1-User) | repeated | The users from the specified request. |






<a name="metaxisdata-v1-CreateUserRequest"></a>

### CreateUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [User](#metaxisdata-v1-User) |  | The user to create. |






<a name="metaxisdata-v1-DeleteUserRequest"></a>

### DeleteUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the user to delete. Format: users/{user} |






<a name="metaxisdata-v1-GetUserRequest"></a>

### GetUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the user to retrieve. Format: users/{user uid or user email} |






<a name="metaxisdata-v1-ListUsersRequest"></a>

### ListUsersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of users to return. The service may return fewer than this value. If unspecified, at most 10 users will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListUsers` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListUsers` must match the call that provided the page token. |
| show_deleted | [bool](#bool) |  | Show deleted users if specified. |
| filter | [string](#string) |  | Filter is used to filter users returned in the list. The syntax and semantics of CEL are documented at https://github.com/google/cel-spec

Supported filter: - name: the user name, support &#34;==&#34; and &#34;.matches()&#34; operator. - email: the user email, support &#34;==&#34; and &#34;.matches()&#34; operator. - user_type: the type, check UserType enum for values, support &#34;==&#34;, &#34;in [xx]&#34;, &#34;!(in [xx])&#34; operator. - state: check State enum for values, support &#34;==&#34; operator.

For example: name == &#34;ed&#34; name.matches(&#34;ed&#34;) email == &#34;ed@example.com&#34; email.matches(&#34;ed&#34;) user_type == &#34;USER&#34; user_type in [&#34;USER&#34;] !(user_type in [&#34;USER&#34;]) state == &#34;DELETED&#34; You can combine filter conditions like: name.matches(&#34;ed&#34;) &amp;&amp; state == &#34;ACTIVE&#34; (name == &#34;ed&#34; || email == &#34;ed@example.com&#34;) &amp;&amp; user_type == &#34;USER&#34; |






<a name="metaxisdata-v1-ListUsersResponse"></a>

### ListUsersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| users | [User](#metaxisdata-v1-User) | repeated | The users from the specified request. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-UndeleteUserRequest"></a>

### UndeleteUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the deleted user. Format: users/{user} |






<a name="metaxisdata-v1-UpdateUserRequest"></a>

### UpdateUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [User](#metaxisdata-v1-User) |  | The user to update.

The user&#39;s `name` field is used to identify the user to update. Format: users/{user} |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. |
| current_password | [string](#string) |  | The user&#39;s current password. Required when a user changes their own password; an admin changing another user&#39;s password does not need it. |
| allow_missing | [bool](#bool) |  | If set to true, and the user is not found, a new user will be created from the fields named by `update_mask`; fields absent from the mask are ignored. `user_type` is a maskable path because it selects the kind of principal to create. |






<a name="metaxisdata-v1-User"></a>

### User



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the user. Format: users/{user}. {user} is a system-generated unique ID. |
| state | [State](#metaxisdata-v1-State) |  |  |
| email | [string](#string) |  |  |
| title | [string](#string) |  |  |
| user_type | [UserType](#metaxisdata-v1-UserType) |  |  |
| password | [string](#string) |  |  |
| service_key | [string](#string) |  |  |
| phone | [string](#string) |  | Should be a valid E.164 compliant phone number. Could be empty. |
| profile | [UserProfile](#metaxisdata-v1-UserProfile) |  |  |
| groups | [string](#string) | repeated | The groups for the user. Format: groups/{email} |
| permissions | [string](#string) | repeated | The effective workspace permissions of the caller, as a list of `metaxisdata.&lt;resource&gt;.&lt;verb&gt;` strings. Populated by GetCurrentUser only, so the frontend can gate navigation and actions without probing each RPC. |






<a name="metaxisdata-v1-UserProfile"></a>

### UserProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| last_login_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_change_password_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 


<a name="metaxisdata-v1-UserType"></a>

### UserType


| Name | Number | Description |
| ---- | ------ | ----------- |
| USER_TYPE_UNSPECIFIED | 0 |  |
| END_USER | 1 | The human user. Matches store PrincipalType.END_USER and the principal table&#39;s type check. |
| SERVICE_ACCOUNT | 2 |  |
| SYSTEM_BOT | 3 |  |


 

 


<a name="metaxisdata-v1-UserService"></a>

### UserService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetUser | [GetUserRequest](#metaxisdata-v1-GetUserRequest) | [User](#metaxisdata-v1-User) | Get the user. Any authenticated user can get the user. |
| BatchGetUsers | [BatchGetUsersRequest](#metaxisdata-v1-BatchGetUsersRequest) | [BatchGetUsersResponse](#metaxisdata-v1-BatchGetUsersResponse) | Get the users in batch. Any authenticated user can batch get users. |
| GetCurrentUser | [.google.protobuf.Empty](#google-protobuf-Empty) | [User](#metaxisdata-v1-User) | Get the current authenticated user. Permissions required: None |
| ListUsers | [ListUsersRequest](#metaxisdata-v1-ListUsersRequest) | [ListUsersResponse](#metaxisdata-v1-ListUsersResponse) | List all users. Any authenticated user can list users. |
| CreateUser | [CreateUserRequest](#metaxisdata-v1-CreateUserRequest) | [User](#metaxisdata-v1-User) | Create a user. |
| UpdateUser | [UpdateUserRequest](#metaxisdata-v1-UpdateUserRequest) | [User](#metaxisdata-v1-User) | Only the user itself and the user with permission on the workspace can update the user. |
| DeleteUser | [DeleteUserRequest](#metaxisdata-v1-DeleteUserRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Only the user with permission on the workspace can delete the user. The last remaining workspace admin cannot be deleted. |
| UndeleteUser | [UndeleteUserRequest](#metaxisdata-v1-UndeleteUserRequest) | [User](#metaxisdata-v1-User) | Only the user with permission on the workspace can undelete the user. |

 



<a name="v1_auth_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/auth_service.proto



<a name="metaxisdata-v1-CreateSSOStateResponse"></a>

### CreateSSOStateResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| state | [string](#string) |  |  |






<a name="metaxisdata-v1-IdentityProviderContext"></a>

### IdentityProviderContext



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| oauth2_context | [OAuth2IdentityProviderContext](#metaxisdata-v1-OAuth2IdentityProviderContext) |  |  |






<a name="metaxisdata-v1-LoginRequest"></a>

### LoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |
| password | [string](#string) |  |  |
| web | [bool](#bool) |  | If web is set, we will set access token, refresh token, and user to the cookie. |
| idp_name | [string](#string) |  | The name of the identity provider. Format: idps/{idp} |
| idp_context | [IdentityProviderContext](#metaxisdata-v1-IdentityProviderContext) |  | The idp_context is using to get the user information from identity provider. |






<a name="metaxisdata-v1-LoginResponse"></a>

### LoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  | The access token. Empty for a web login, which carries the token in an HttpOnly cookie instead. |
| require_reset_password | [bool](#bool) |  | Whether the workspace password policy requires the user to rotate their password before doing anything else. When true, the issued token is restricted to changing the user&#39;s own password (and logging out). |
| user | [User](#metaxisdata-v1-User) |  | The user of successful login. |






<a name="metaxisdata-v1-LogoutRequest"></a>

### LogoutRequest







<a name="metaxisdata-v1-OAuth2IdentityProviderContext"></a>

### OAuth2IdentityProviderContext



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  |  |
| state | [string](#string) |  | The state issued by CreateSSOState. Required: the server rejects a login whose state is missing, unknown or already used. |
| code_verifier | [string](#string) |  | code_verifier is the PKCE verifier matching the code_challenge sent to the identity provider. Optional; providers that do not use PKCE ignore it. |





 

 

 


<a name="metaxisdata-v1-AuthService"></a>

### AuthService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Login | [LoginRequest](#metaxisdata-v1-LoginRequest) | [LoginResponse](#metaxisdata-v1-LoginResponse) | Permissions required: None |
| Logout | [LogoutRequest](#metaxisdata-v1-LogoutRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Permissions required: None |
| CreateSSOState | [.google.protobuf.Empty](#google-protobuf-Empty) | [CreateSSOStateResponse](#metaxisdata-v1-CreateSSOStateResponse) | CreateSSOState issues a one-time OAuth2 state value. A client must fetch it before redirecting to the identity provider, pass it back to the provider and then send it with the login request; the server consumes it there. Without it an attacker can complete an authorization-code flow in a victim&#39;s browser and bind the victim&#39;s session to the attacker&#39;s identity. Permissions required: None |

 



<a name="v1_instance_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/instance_service.proto



<a name="metaxisdata-v1-BatchSyncInstanceResult"></a>

### BatchSyncInstanceResult



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance. Format: instances/{instance} |
| databases | [string](#string) | repeated | The names of the databases discovered by this sync. |
| error | [string](#string) |  | Empty when the sync succeeded; otherwise why this instance could not be synced. |






<a name="metaxisdata-v1-BatchSyncInstancesRequest"></a>

### BatchSyncInstancesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| requests | [SyncInstanceRequest](#metaxisdata-v1-SyncInstanceRequest) | repeated | The request message specifying the instances to sync. A maximum of 1000 instances can be synced in a batch. |






<a name="metaxisdata-v1-BatchSyncInstancesResponse"></a>

### BatchSyncInstancesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| results | [BatchSyncInstanceResult](#metaxisdata-v1-BatchSyncInstanceResult) | repeated | One result per requested instance, in request order. Per-instance failures are reported here and do not abort the remaining instances. |






<a name="metaxisdata-v1-BatchUpdateInstancesRequest"></a>

### BatchUpdateInstancesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| requests | [UpdateInstanceRequest](#metaxisdata-v1-UpdateInstanceRequest) | repeated | The request message specifying the resources to update. |






<a name="metaxisdata-v1-BatchUpdateInstancesResponse"></a>

### BatchUpdateInstancesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instances | [Instance](#metaxisdata-v1-Instance) | repeated |  |






<a name="metaxisdata-v1-CreateDataSourceRequest"></a>

### CreateDataSourceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | The name of the instance to add a data source to. Format: instances/{instance} |
| data_source | [DataSource](#metaxisdata-v1-DataSource) |  | The data source to create. Its `name` must be empty; the server derives it from `data_source_id`. Only READ_ONLY data sources can be created here; the ADMIN data source is part of the instance itself. |
| data_source_id | [string](#string) |  | The ID to use for the data source, which will become the final component of the data source&#39;s resource name.

This value should be 4-63 characters, and valid characters are /[a-z0-9-]/. When empty, the server generates an ID. |
| validate_only | [bool](#bool) |  | Validate only also tests the data source connection. |






<a name="metaxisdata-v1-CreateInstanceRequest"></a>

### CreateInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [Instance](#metaxisdata-v1-Instance) |  | The instance to create. |
| instance_id | [string](#string) |  | The ID to use for the instance, which will become the final component of the instance&#39;s resource name.

This value should be 4-63 characters, and valid characters are /[a-z0-9-]/. |
| validate_only | [bool](#bool) |  | Validate only also tests the data source connection. |






<a name="metaxisdata-v1-DataSource"></a>

### DataSource



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the data source. Format: instances/{instance}/dataSources/{data_source} |
| type | [DataSourceType](#metaxisdata-v1-DataSourceType) |  |  |
| username | [string](#string) |  |  |
| password | [string](#string) |  |  |
| use_ssl | [bool](#bool) |  | Use SSL to connect to the data source. By default, we use system default SSL configuration. |
| ssl_ca | [string](#string) |  |  |
| ssl_cert | [string](#string) |  |  |
| ssl_key | [string](#string) |  |  |
| host | [string](#string) |  |  |
| port | [string](#string) |  |  |
| database | [string](#string) |  |  |
| ssh_host | [string](#string) |  | Connection over SSH. The hostname of the SSH server agent. Required. |
| ssh_port | [string](#string) |  | The port of the SSH server agent. It&#39;s 22 typically. Required. |
| ssh_user | [string](#string) |  | The user to login the server. Required. |
| ssh_password | [string](#string) |  | The password to login the server. If it&#39;s empty string, no password is required. |
| ssh_private_key | [string](#string) |  | The private key to login the server. If it&#39;s empty string, we will use the system default private key from os.Getenv(&#34;SSH_AUTH_SOCK&#34;). |
| extra_connection_parameters | [DataSource.ExtraConnectionParametersEntry](#metaxisdata-v1-DataSource-ExtraConnectionParametersEntry) | repeated | Extra connection parameters for the database connection. For PostgreSQL HA, this can be used to set target_session_attrs=read-write |






<a name="metaxisdata-v1-DataSource-ExtraConnectionParametersEntry"></a>

### DataSource.ExtraConnectionParametersEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-DeleteDataSourceRequest"></a>

### DeleteDataSourceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the data source to delete. Format: instances/{instance}/dataSources/{data_source} |






<a name="metaxisdata-v1-DeleteInstanceRequest"></a>

### DeleteInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance to delete. Format: instances/{instance} |






<a name="metaxisdata-v1-GetInstanceRequest"></a>

### GetInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance to retrieve. Format: instances/{instance} |






<a name="metaxisdata-v1-Instance"></a>

### Instance



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance. Format: instances/{instance} |
| state | [State](#metaxisdata-v1-State) |  |  |
| title | [string](#string) |  |  |
| engine | [Engine](#metaxisdata-v1-Engine) |  |  |
| engine_version | [string](#string) |  |  |
| external_link | [string](#string) |  |  |
| data_sources | [DataSource](#metaxisdata-v1-DataSource) | repeated | The data sources of the instance.

`CreateInstance` creates the initial admin connection and any read-only sources carried in the request; afterwards the set is managed through `CreateDataSource`/`UpdateDataSource`/`DeleteDataSource`. `UpdateInstance` does not accept a `data_sources` update mask. |
| environment | [string](#string) |  | The environment resource. Format: environments/prod where prod is the environment resource ID. |
| activation | [bool](#bool) |  |  |
| sync_interval | [google.protobuf.Duration](#google-protobuf-Duration) |  | How often the instance is synced. |
| maximum_connections | [int32](#int32) |  | The maximum number of connections. The default is 10 if the value is unset or zero. |
| sync_databases | [string](#string) | repeated | Enable sync for following databases. Default empty, means sync all schemas &amp; databases. |
| last_sync_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | The last time the instance was synced. |






<a name="metaxisdata-v1-InstanceResource"></a>

### InstanceResource



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  |  |
| engine | [Engine](#metaxisdata-v1-Engine) |  |  |
| engine_version | [string](#string) |  |  |
| data_sources | [DataSource](#metaxisdata-v1-DataSource) | repeated |  |
| activation | [bool](#bool) |  |  |
| name | [string](#string) |  | The name of the instance. Format: instances/{instance} |
| environment | [string](#string) |  | The environment resource. Format: environments/prod where prod is the environment resource ID. |






<a name="metaxisdata-v1-ListInstancesRequest"></a>

### ListInstancesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of instances to return. The service may return fewer than this value. If unspecified, at most 10 instances will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListInstances` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListInstances` must match the call that provided the page token. |
| show_deleted | [bool](#bool) |  | Show deleted instances if specified. |
| filter | [string](#string) |  | Filter the instance. The syntax and semantics of CEL are documented at https://github.com/google/cel-spec

Supported filters: - name: the instance name, support &#34;==&#34; and &#34;.matches()&#34; operator. - resource_id: the instance id, support &#34;==&#34; and &#34;.matches()&#34; operator. - environment: the environment full name in &#34;environments/{id}&#34; format, support &#34;==&#34; operator. - state: the instance state, check State enum for values, support &#34;==&#34; operator. - engine: the instance engine, check Engine enum for values. Support &#34;==&#34;, &#34;in [xx]&#34;, &#34;!(in [xx])&#34; operator. - host: the instance host, support &#34;==&#34; and &#34;.matches()&#34; operator. - port: the instance port, support &#34;==&#34; and &#34;.matches()&#34; operator.

For example: name == &#34;sample instance&#34; name.matches(&#34;sample&#34;) resource_id = &#34;sample-instance&#34; resource_id.matches(&#34;sample&#34;) state == &#34;DELETED&#34; environment == &#34;environments/test&#34; engine == &#34;MYSQL&#34; engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;] !(engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;]) host == &#34;127.0.0.1&#34; host.matches(&#34;127.0&#34;) port == &#34;54321&#34; port.matches(&#34;543&#34;) You can combine filter conditions like: name.matches(&#34;sample&#34;) &amp;&amp; environment == &#34;environments/test&#34; host == &#34;127.0.0.1&#34; &amp;&amp; port == &#34;54321&#34; |






<a name="metaxisdata-v1-ListInstancesResponse"></a>

### ListInstancesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instances | [Instance](#metaxisdata-v1-Instance) | repeated | The instances from the specified request. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-SyncInstanceRequest"></a>

### SyncInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of instance. Format: instances/{instance} |
| enable_full_sync | [bool](#bool) |  | When full sync is enabled, all databases in the instance will be synchronized. Otherwise, only the instance metadata (such as the database list) and any newly discovered instances will be synced. |






<a name="metaxisdata-v1-SyncInstanceResponse"></a>

### SyncInstanceResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| databases | [string](#string) | repeated | All database name list in the instance. |






<a name="metaxisdata-v1-UndeleteInstanceRequest"></a>

### UndeleteInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the deleted instance. Format: instances/{instance} |






<a name="metaxisdata-v1-UpdateDataSourceRequest"></a>

### UpdateDataSourceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data_source | [DataSource](#metaxisdata-v1-DataSource) |  | The data source to update.

The data source&#39;s `name` field is used to identify it. Format: instances/{instance}/dataSources/{data_source} |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. Fields not listed keep their stored value, which is how reads that omit credentials stay non-destructive. |
| validate_only | [bool](#bool) |  | Validate only also tests the data source connection. |






<a name="metaxisdata-v1-UpdateInstanceRequest"></a>

### UpdateInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [Instance](#metaxisdata-v1-Instance) |  | The instance to update.

The instance&#39;s `name` field is used to identify the instance to update. Format: instances/{instance} |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. |





 


<a name="metaxisdata-v1-DataSourceType"></a>

### DataSourceType


| Name | Number | Description |
| ---- | ------ | ----------- |
| DATA_SOURCE_UNSPECIFIED | 0 |  |
| ADMIN | 1 |  |
| READ_ONLY | 2 |  |


 

 


<a name="metaxisdata-v1-InstanceService"></a>

### InstanceService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetInstance | [GetInstanceRequest](#metaxisdata-v1-GetInstanceRequest) | [Instance](#metaxisdata-v1-Instance) |  |
| ListInstances | [ListInstancesRequest](#metaxisdata-v1-ListInstancesRequest) | [ListInstancesResponse](#metaxisdata-v1-ListInstancesResponse) |  |
| CreateInstance | [CreateInstanceRequest](#metaxisdata-v1-CreateInstanceRequest) | [Instance](#metaxisdata-v1-Instance) |  |
| UpdateInstance | [UpdateInstanceRequest](#metaxisdata-v1-UpdateInstanceRequest) | [Instance](#metaxisdata-v1-Instance) |  |
| DeleteInstance | [DeleteInstanceRequest](#metaxisdata-v1-DeleteInstanceRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| UndeleteInstance | [UndeleteInstanceRequest](#metaxisdata-v1-UndeleteInstanceRequest) | [Instance](#metaxisdata-v1-Instance) |  |
| SyncInstance | [SyncInstanceRequest](#metaxisdata-v1-SyncInstanceRequest) | [SyncInstanceResponse](#metaxisdata-v1-SyncInstanceResponse) |  |
| BatchSyncInstances | [BatchSyncInstancesRequest](#metaxisdata-v1-BatchSyncInstancesRequest) | [BatchSyncInstancesResponse](#metaxisdata-v1-BatchSyncInstancesResponse) |  |
| BatchUpdateInstances | [BatchUpdateInstancesRequest](#metaxisdata-v1-BatchUpdateInstancesRequest) | [BatchUpdateInstancesResponse](#metaxisdata-v1-BatchUpdateInstancesResponse) |  |
| CreateDataSource | [CreateDataSourceRequest](#metaxisdata-v1-CreateDataSourceRequest) | [DataSource](#metaxisdata-v1-DataSource) |  |
| UpdateDataSource | [UpdateDataSourceRequest](#metaxisdata-v1-UpdateDataSourceRequest) | [DataSource](#metaxisdata-v1-DataSource) |  |
| DeleteDataSource | [DeleteDataSourceRequest](#metaxisdata-v1-DeleteDataSourceRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |

 



<a name="v1_database_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/database_service.proto



<a name="metaxisdata-v1-CheckConstraintMetadata"></a>

### CheckConstraintMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the check constraint. |
| expression | [string](#string) |  | The expression is the expression of a check constraint. |






<a name="metaxisdata-v1-ColumnMetadata"></a>

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
| generation | [GenerationMetadata](#metaxisdata-v1-GenerationMetadata) |  | The generation is for generated columns. |
| is_identity | [bool](#bool) |  |  |
| identity_generation | [ColumnMetadata.IdentityGeneration](#metaxisdata-v1-ColumnMetadata-IdentityGeneration) |  | The identity_generation is for identity columns, PG only. |
| identity_seed | [int64](#int64) |  | The identity_seed is for identity columns, MSSQL only. |
| identity_increment | [int64](#int64) |  | The identity_increment is for identity columns, MSSQL only. |
| default_constraint_name | [string](#string) |  | The default_constraint_name is the name of the default constraint, MSSQL only. In MSSQL, default values are implemented as named constraints. When modifying or dropping a column&#39;s default value, you must reference the constraint by name. This field stores the actual constraint name from the database.

Example: A column definition like: CREATE TABLE employees ( status NVARCHAR(20) DEFAULT &#39;active&#39; )

Will create a constraint with an auto-generated name like &#39;DF__employees__statu__3B75D760&#39; or a user-defined name if specified: ALTER TABLE employees ADD CONSTRAINT DF_employees_status DEFAULT &#39;active&#39; FOR status

To modify the default, you must first drop the existing constraint by name: ALTER TABLE employees DROP CONSTRAINT DF__employees__statu__3B75D760 ALTER TABLE employees ADD CONSTRAINT DF_employees_status DEFAULT &#39;inactive&#39; FOR status

This field is populated when syncing from the database. When empty (e.g., when parsing from SQL files), the system cannot automatically drop the constraint. |






<a name="metaxisdata-v1-CreateManualSQLRequest"></a>

### CreateManualSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: instances/{instance}/databases/{database} |
| manual_sql | [ManualSQL](#metaxisdata-v1-ManualSQL) |  |  |
| manual_sql_id | [string](#string) |  | The stable ID used as the last component of the resource name. |






<a name="metaxisdata-v1-Database"></a>

### Database



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the database. Format: instances/{instance}/databases/{database} {database} is the database name in the instance. |
| state | [State](#metaxisdata-v1-State) |  | The existence of a database. |
| successful_sync_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | The latest synchronization time. |
| schema_version | [string](#string) |  | The version of database schema. |
| environment | [string](#string) |  | The environment resource. Format: environments/prod where prod is the environment resource ID. |
| effective_environment | [string](#string) |  | The effective environment based on environment tag above and environment tag on the instance. Inheritance follows https://cloud.google.com/resource-manager/docs/tags/tags-overview. |
| labels | [Database.LabelsEntry](#metaxisdata-v1-Database-LabelsEntry) | repeated | Labels will be used for deployment and policy control. |
| instance_resource | [InstanceResource](#metaxisdata-v1-InstanceResource) |  | The instance resource. |
| drifted | [bool](#bool) |  | The schema is drifted from the source of truth. |






<a name="metaxisdata-v1-Database-LabelsEntry"></a>

### Database.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-DatabaseSchemaMetadata"></a>

### DatabaseSchemaMetadata
DatabaseSchemaMetadata is the schema metadata for databases.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| schemas | [SchemaMetadata](#metaxisdata-v1-SchemaMetadata) | repeated | The list of schemas in a database. |
| character_set | [string](#string) |  | The character set of the database. |
| collation | [string](#string) |  | The collation of the database. |
| extensions | [ExtensionMetadata](#metaxisdata-v1-ExtensionMetadata) | repeated | The list of extensions in a database. |
| datashare | [bool](#bool) |  | The database belongs to a datashare. |
| owner | [string](#string) |  |  |
| search_path | [string](#string) |  | The search_path is the search path of a PostgreSQL database. |
| event_triggers | [EventTriggerMetadata](#metaxisdata-v1-EventTriggerMetadata) | repeated | The list of event triggers in a database (PostgreSQL specific). Event triggers are database-level objects, not schema-scoped. |






<a name="metaxisdata-v1-DeleteManualSQLRequest"></a>

### DeleteManualSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: instances/{instance}/databases/{database}/manualSqls/{manual_sql} |






<a name="metaxisdata-v1-DependencyColumn"></a>

### DependencyColumn
DependencyColumn is the metadata for dependency columns.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema | [string](#string) |  | The schema is the schema of a reference column. |
| table | [string](#string) |  | The table is the table of a reference column. |
| column | [string](#string) |  | The column is the name of a reference column. |






<a name="metaxisdata-v1-DependencyTable"></a>

### DependencyTable



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema | [string](#string) |  | The schema is the schema of a reference table. |
| table | [string](#string) |  | The table is the name of a reference table. |






<a name="metaxisdata-v1-DiffMetadataRequest"></a>

### DiffMetadataRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The GUID of the schema or database whose history versions to compare. For database-level diff: &#34;instance_1;db1&#34; For schema-level diff: &#34;instance_1;db1;schema1&#34; |
| source_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | The timestamp of the source (older) version. If not set, uses the earliest available version. |
| target_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | The timestamp of the target (newer) version. If not set, uses the current/latest version. |






<a name="metaxisdata-v1-DiffMetadataResponse"></a>

### DiffMetadataResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| diff_summary | [string](#string) |  | A human-readable summary of the schema changes. |
| ddl | [string](#string) |  | The full DDL migration SQL from source to target. |






<a name="metaxisdata-v1-EnumTypeMetadata"></a>

### EnumTypeMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the enum type. |
| values | [string](#string) | repeated | The enum values of the type. |
| comment | [string](#string) |  |  |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-v1-EventMetadata"></a>

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






<a name="metaxisdata-v1-EventTriggerMetadata"></a>

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






<a name="metaxisdata-v1-ExcludeConstraintMetadata"></a>

### ExcludeConstraintMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the EXCLUDE constraint. |
| expression | [string](#string) |  | The expression is the full EXCLUDE constraint definition including &#34;EXCLUDE&#34; keyword. Example: &#34;EXCLUDE USING gist (room_id WITH =, during WITH &amp;&amp;)&#34; |






<a name="metaxisdata-v1-ExtensionMetadata"></a>

### ExtensionMetadata
ExtensionMetadata is the metadata for extensions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the extension. |
| schema | [string](#string) |  | The schema where the extension is installed. However, the extension usage is not limited to the schema. |
| version | [string](#string) |  | The version is the version of an extension. |
| description | [string](#string) |  | The description is the description of an extension. |






<a name="metaxisdata-v1-ExternalTableMetadata"></a>

### ExternalTableMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the external table. |
| external_server_name | [string](#string) |  | The external_server_name is the name of the external server. |
| external_database_name | [string](#string) |  | The external_database_name is the name of the external database. |
| columns | [ColumnMetadata](#metaxisdata-v1-ColumnMetadata) | repeated | The columns is the ordered list of columns in a foreign table. |






<a name="metaxisdata-v1-ForeignKeyMetadata"></a>

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






<a name="metaxisdata-v1-FunctionMetadata"></a>

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
| dependency_tables | [DependencyTable](#metaxisdata-v1-DependencyTable) | repeated | The dependency_tables is the list of dependency tables of a function. For PostgreSQL, it&#39;s the list of tables that the function depends on the return type definition. |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-v1-GenerationMetadata"></a>

### GenerationMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [GenerationMetadata.Type](#metaxisdata-v1-GenerationMetadata-Type) |  |  |
| expression | [string](#string) |  |  |






<a name="metaxisdata-v1-GetManualSQLRequest"></a>

### GetManualSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: instances/{instance}/databases/{database}/manualSqls/{manual_sql} |






<a name="metaxisdata-v1-GetMetadataHistoryEventRequest"></a>

### GetMetadataHistoryEventRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| event_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| operation | [MetadataHistoryOperation](#metaxisdata-v1-MetadataHistoryOperation) |  |  |






<a name="metaxisdata-v1-GetMetadataRequest"></a>

### GetMetadataRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The global unique id for metadata database: &#34;instance_100;database3&#34; table: &#34;instance_1;db2;schema3;table4&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |






<a name="metaxisdata-v1-GetMetadataResponse"></a>

### GetMetadataResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| metadata | [StoredMetadata](#metaxisdata-v1-StoredMetadata) |  |  |






<a name="metaxisdata-v1-GetSchemaStringRequest"></a>

### GetSchemaStringRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The global unique id for metadata table: &#34;instance_1;db2;schema3;table4&#34; view: &#34;instance_1;db2;schema3;view2&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |






<a name="metaxisdata-v1-IndexMetadata"></a>

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






<a name="metaxisdata-v1-ListDatabasesRequest"></a>

### ListDatabasesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | - workspaces/-: list databases in the workspace. - instances/{instances}: list databases in a instance. |
| page_size | [int32](#int32) |  | The maximum number of databases to return. The service may return fewer than this value. If unspecified, at most 10 databases will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListDatabases` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListDatabases` must match the call that provided the page token. |
| filter | [string](#string) |  | Filter is used to filter databases returned in the list. The syntax and semantics of CEL are documented at https://github.com/google/cel-spec

Supported filter: - environment: the environment full name in &#34;environments/{id}&#34; format, support &#34;==&#34; operator. - name: the database name, support &#34;.matches()&#34; operator. - instance: the instance full name in &#34;instances/{id}&#34; format, support &#34;==&#34; operator. - engine: the database engine, check Engine enum for values. Support &#34;==&#34;, &#34;in [xx]&#34;, &#34;!(in [xx])&#34; operator. - drifted: should be &#34;true&#34; or &#34;false&#34;, show drifted databases if it&#39;s true, support &#34;==&#34; operator. - labels.{key}: the database label, support &#34;==&#34; and &#34;in&#34; operators.

For example: environment == &#34;environments/{environment resource id}&#34; environment == &#34;&#34; (find databases which environment is not set) instance == &#34;instances/{instance resource id}&#34; name.matches(&#34;database name&#34;) engine == &#34;MYSQL&#34; engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;] !(engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;]) drifted == true labels.environment == &#34;production&#34; labels.region == &#34;asia&#34; labels.region in [&#34;asia&#34;, &#34;europe&#34;]

You can combine filter conditions like: environment == &#34;environments/prod&#34; &amp;&amp; name.matches(&#34;employee&#34;) |
| show_deleted | [bool](#bool) |  | Show deleted database if specified. |






<a name="metaxisdata-v1-ListDatabasesResponse"></a>

### ListDatabasesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| databases | [Database](#metaxisdata-v1-Database) | repeated | All database name list. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-ListManualSQLsRequest"></a>

### ListManualSQLsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: instances/{instance}/databases/{database} |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| schema_name | [string](#string) |  | Optional schema context filter. Empty means all schemas. |
| tags | [string](#string) | repeated | Exact-match tags for filtering. |
| show_deleted | [bool](#bool) |  |  |






<a name="metaxisdata-v1-ListManualSQLsResponse"></a>

### ListManualSQLsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| manual_sqls | [ManualSQL](#metaxisdata-v1-ManualSQL) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListMetadataHistoryRequest"></a>

### ListMetadataHistoryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListMetadataHistoryResponse"></a>

### ListMetadataHistoryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entries | [MetadataHistoryTimelineEntry](#metaxisdata-v1-MetadataHistoryTimelineEntry) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListMetadataRequest"></a>

### ListMetadataRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent_guid | [string](#string) |  | The global unique id for all metadata database: &#34;instance_100;database3&#34; table: &#34;instance_1;db2;schema3;table4&#34;

A GUID is an opaque, &#39;;&#39;-joined identifier rather than an AIP resource name, so it carries no resource_reference (the same applies to every other guid field in this file). |
| page_size | [int32](#int32) |  | The maximum number of databases to return. The service may return fewer than this value. If unspecified, at most 10 databases will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListMetadata` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListMetadata` must match the call that provided the page token.

When meta_type is unset the response groups the rows by type and page_size applies to each group separately. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) | optional | the type of metadata |






<a name="metaxisdata-v1-ManualSQL"></a>

### ManualSQL



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name. Format: instances/{instance}/databases/{database}/manualSqls/{manual_sql} |
| guid | [string](#string) |  | The globally unique metadata GUID. |
| title | [string](#string) |  |  |
| schema_name | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| sql_text | [string](#string) |  |  |
| tags | [string](#string) | repeated |  |
| attributes | [ManualSQL.AttributesEntry](#metaxisdata-v1-ManualSQL-AttributesEntry) | repeated |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-ManualSQL-AttributesEntry"></a>

### ManualSQL.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-ManualSQLMetadata"></a>

### ManualSQLMetadata
ManualSQLMetadata is the metadata for a user-maintained SQL definition.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| manual_sql_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| title | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| sql_text | [string](#string) |  |  |
| tags | [string](#string) | repeated |  |
| attributes | [ManualSQLMetadata.AttributesEntry](#metaxisdata-v1-ManualSQLMetadata-AttributesEntry) | repeated |  |
| schema_name | [string](#string) |  |  |
| instance_resource | [string](#string) |  | Format: instances/{instance} |
| database_name | [string](#string) |  |  |






<a name="metaxisdata-v1-ManualSQLMetadata-AttributesEntry"></a>

### ManualSQLMetadata.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-MaterializedViewMetadata"></a>

### MaterializedViewMetadata
MaterializedViewMetadata is the metadata for materialized views.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the materialized view. |
| definition | [string](#string) |  | The definition is the definition of a view. |
| comment | [string](#string) |  | The comment is the comment of a view. |
| dependency_columns | [DependencyColumn](#metaxisdata-v1-DependencyColumn) | repeated | The list of dependency columns of the view. |
| triggers | [TriggerMetadata](#metaxisdata-v1-TriggerMetadata) | repeated | The ordered list of columns in the materialized view. |
| indexes | [IndexMetadata](#metaxisdata-v1-IndexMetadata) | repeated | The list of indexes in the materialized view. |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-v1-MetadataFieldChange"></a>

### MetadataFieldChange



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| field | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| before | [string](#string) |  |  |
| after | [string](#string) |  |  |






<a name="metaxisdata-v1-MetadataHistoryChangeGroup"></a>

### MetadataHistoryChangeGroup



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| section | [MetadataHistorySection](#metaxisdata-v1-MetadataHistorySection) |  |  |
| changes | [MetadataHistoryChangeItem](#metaxisdata-v1-MetadataHistoryChangeItem) | repeated |  |






<a name="metaxisdata-v1-MetadataHistoryChangeItem"></a>

### MetadataHistoryChangeItem



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| section | [MetadataHistorySection](#metaxisdata-v1-MetadataHistorySection) |  |  |
| operation | [MetadataHistoryOperation](#metaxisdata-v1-MetadataHistoryOperation) |  |  |
| key | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| summary | [string](#string) |  |  |
| field_changes | [MetadataFieldChange](#metaxisdata-v1-MetadataFieldChange) | repeated |  |
| before | [MetadataHistoryChildSnapshot](#metaxisdata-v1-MetadataHistoryChildSnapshot) |  |  |
| after | [MetadataHistoryChildSnapshot](#metaxisdata-v1-MetadataHistoryChildSnapshot) |  |  |






<a name="metaxisdata-v1-MetadataHistoryChildSnapshot"></a>

### MetadataHistoryChildSnapshot



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column_metadata | [ColumnMetadata](#metaxisdata-v1-ColumnMetadata) |  |  |
| index_metadata | [IndexMetadata](#metaxisdata-v1-IndexMetadata) |  |  |
| foreign_key_metadata | [ForeignKeyMetadata](#metaxisdata-v1-ForeignKeyMetadata) |  |  |
| check_constraint_metadata | [CheckConstraintMetadata](#metaxisdata-v1-CheckConstraintMetadata) |  |  |
| partition_metadata | [TablePartitionMetadata](#metaxisdata-v1-TablePartitionMetadata) |  |  |
| trigger_metadata | [TriggerMetadata](#metaxisdata-v1-TriggerMetadata) |  |  |
| rule_metadata | [RuleMetadata](#metaxisdata-v1-RuleMetadata) |  |  |






<a name="metaxisdata-v1-MetadataHistoryEvent"></a>

### MetadataHistoryEvent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entry | [MetadataHistoryTimelineEntry](#metaxisdata-v1-MetadataHistoryTimelineEntry) |  |  |
| before_metadata | [StoredMetadata](#metaxisdata-v1-StoredMetadata) |  |  |
| after_metadata | [StoredMetadata](#metaxisdata-v1-StoredMetadata) |  |  |
| change_groups | [MetadataHistoryChangeGroup](#metaxisdata-v1-MetadataHistoryChangeGroup) | repeated |  |






<a name="metaxisdata-v1-MetadataHistorySectionChangeCount"></a>

### MetadataHistorySectionChangeCount



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| section | [MetadataHistorySection](#metaxisdata-v1-MetadataHistorySection) |  |  |
| added | [int32](#int32) |  |  |
| updated | [int32](#int32) |  |  |
| removed | [int32](#int32) |  |  |






<a name="metaxisdata-v1-MetadataHistoryTimelineEntry"></a>

### MetadataHistoryTimelineEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| event_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| valid_from | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| valid_to | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| operation | [MetadataHistoryOperation](#metaxisdata-v1-MetadataHistoryOperation) |  |  |
| summary | [string](#string) |  |  |
| section_changes | [MetadataHistorySectionChangeCount](#metaxisdata-v1-MetadataHistorySectionChangeCount) | repeated |  |






<a name="metaxisdata-v1-MetadataResponse"></a>

### MetadataResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| types_stored_metadata | [MetadataResponse.Metadata](#metaxisdata-v1-MetadataResponse-Metadata) | repeated | The list of stored metadata. |






<a name="metaxisdata-v1-MetadataResponse-Metadata"></a>

### MetadataResponse.Metadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| list | [StoredMetadata](#metaxisdata-v1-StoredMetadata) | repeated |  |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-MetadataSchemaString"></a>

### MetadataSchemaString
MetadataSchemaString is the schema define for metadata.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema | [string](#string) |  | The schema dump from metadata. |






<a name="metaxisdata-v1-ProcedureMetadata"></a>

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






<a name="metaxisdata-v1-RuleMetadata"></a>

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






<a name="metaxisdata-v1-SchemaMetadata"></a>

### SchemaMetadata
SchemaMetadata is the metadata for schemas.
This is the concept of schema in Postgres, but it&#39;s a no-op for MySQL.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The schema name. It is an empty string for databases without such concept such as MySQL. |
| tables | [TableMetadata](#metaxisdata-v1-TableMetadata) | repeated | The list of tables in a schema. |
| external_tables | [ExternalTableMetadata](#metaxisdata-v1-ExternalTableMetadata) | repeated | The list of external tables in a schema. |
| views | [ViewMetadata](#metaxisdata-v1-ViewMetadata) | repeated | The list of views in a schema. |
| functions | [FunctionMetadata](#metaxisdata-v1-FunctionMetadata) | repeated | The list of functions in a schema. |
| procedures | [ProcedureMetadata](#metaxisdata-v1-ProcedureMetadata) | repeated | The list of procedures in a schema. |
| materialized_views | [MaterializedViewMetadata](#metaxisdata-v1-MaterializedViewMetadata) | repeated | The list of materialized views in a schema. |
| sequences | [SequenceMetadata](#metaxisdata-v1-SequenceMetadata) | repeated | The list of sequences in a schema. |
| owner | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| events | [EventMetadata](#metaxisdata-v1-EventMetadata) | repeated |  |
| enum_types | [EnumTypeMetadata](#metaxisdata-v1-EnumTypeMetadata) | repeated |  |
| skip_dump | [bool](#bool) |  |  |






<a name="metaxisdata-v1-SearchManualSQLRequest"></a>

### SearchManualSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: instances/{instance}/databases/{database} |
| query | [string](#string) |  | Keyword search against the indexed search document. First phase supports token-based full-text matching only. |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| tags | [string](#string) | repeated | Exact-match tags for filtering. |
| schema_name | [string](#string) |  | Optional schema context filter. Empty means all schemas. |






<a name="metaxisdata-v1-SearchManualSQLResponse"></a>

### SearchManualSQLResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| manual_sqls | [ManualSQL](#metaxisdata-v1-ManualSQL) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-SearchMetadataRequest"></a>

### SearchMetadataRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent_guid_prefix | [string](#string) | optional | Optional metadata prefixes to search for. database: &#34;instance_100;database3&#34; table: &#34;instance_1;db2;schema3;table4&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) | optional | the type of metadata |
| search_str | [string](#string) |  | the search string |
| page_size | [int32](#int32) |  | The maximum number of results to return. If unspecified, at most 50 results will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `SearchMetadata` call. Provide this to retrieve the subsequent page. |






<a name="metaxisdata-v1-SearchMetadataResponse"></a>

### SearchMetadataResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| results | [SearchMetadataResult](#metaxisdata-v1-SearchMetadataResult) | repeated | The search results. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-SearchMetadataResult"></a>

### SearchMetadataResult



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The GUID of found metadata, e.g. &#34;instance_1;db2;schema3;table4&#34;. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  | The type of the metadata. |
| metadata | [StoredMetadata](#metaxisdata-v1-StoredMetadata) |  | The metadata content. |






<a name="metaxisdata-v1-SequenceMetadata"></a>

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






<a name="metaxisdata-v1-StoredMetadata"></a>

### StoredMetadata
StoredMetadata is the v1 view of one meta_registry_resource row.

OpenLineage registry rows (store.MetaType_OPENLINEAGE) have no v1
representation: they are internal summaries of ingested events, and the
supported way to read them is the OpenLineageService resources. They are
filtered out of ListMetadata/GetMetadata/SearchMetadata rather than
serialized as an empty StoredMetadata.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| database_schema_metadata | [DatabaseSchemaMetadata](#metaxisdata-v1-DatabaseSchemaMetadata) |  |  |
| schema_metadata | [SchemaMetadata](#metaxisdata-v1-SchemaMetadata) |  |  |
| table_metadata | [TableMetadata](#metaxisdata-v1-TableMetadata) |  |  |
| external_table_metadata | [ExternalTableMetadata](#metaxisdata-v1-ExternalTableMetadata) |  |  |
| view_metadata | [ViewMetadata](#metaxisdata-v1-ViewMetadata) |  |  |
| materialized_view_metadata | [MaterializedViewMetadata](#metaxisdata-v1-MaterializedViewMetadata) |  |  |
| function_metadata | [FunctionMetadata](#metaxisdata-v1-FunctionMetadata) |  |  |
| procedure_metadata | [ProcedureMetadata](#metaxisdata-v1-ProcedureMetadata) |  |  |
| sequence_metadata | [SequenceMetadata](#metaxisdata-v1-SequenceMetadata) |  |  |
| manual_sql_metadata | [ManualSQLMetadata](#metaxisdata-v1-ManualSQLMetadata) |  |  |
| column_metadata | [ColumnMetadata](#metaxisdata-v1-ColumnMetadata) |  |  |






<a name="metaxisdata-v1-SyncDatabaseRequest"></a>

### SyncDatabaseRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the database to sync. Format: instances/{instance}/databases/{database} |






<a name="metaxisdata-v1-SyncDatabaseResponse"></a>

### SyncDatabaseResponse







<a name="metaxisdata-v1-TableMetadata"></a>

### TableMetadata
TableMetadata is the metadata for tables.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the table. |
| columns | [ColumnMetadata](#metaxisdata-v1-ColumnMetadata) | repeated | The columns is the ordered list of columns in a table. |
| indexes | [IndexMetadata](#metaxisdata-v1-IndexMetadata) | repeated | The indexes is the list of indexes in a table. |
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
| foreign_keys | [ForeignKeyMetadata](#metaxisdata-v1-ForeignKeyMetadata) | repeated | The foreign_keys is the list of foreign keys in a table. |
| partitions | [TablePartitionMetadata](#metaxisdata-v1-TablePartitionMetadata) | repeated | The partitions is the list of partitions in a table. |
| check_constraints | [CheckConstraintMetadata](#metaxisdata-v1-CheckConstraintMetadata) | repeated | The check_constraints is the list of check constraints in a table. |
| owner | [string](#string) |  |  |
| sorting_keys | [string](#string) | repeated | The sorting_keys is a tuple of column names or arbitrary expressions. ClickHouse specific field. Reference: https://clickhouse.com/docs/en/engines/table-engines/mergetree-family/mergetree#order_by |
| triggers | [TriggerMetadata](#metaxisdata-v1-TriggerMetadata) | repeated |  |
| skip_dump | [bool](#bool) |  |  |
| rules | [RuleMetadata](#metaxisdata-v1-RuleMetadata) | repeated | The rules is the list of rules in a table (PostgreSQL specific). |
| sharding_info | [string](#string) |  | https://docs.pingcap.com/tidb/stable/information-schema-tables/ |
| primary_key_type | [string](#string) |  | https://docs.pingcap.com/tidb/stable/clustered-indexes/#clustered-indexes CLUSTERED or NONCLUSTERED. |
| exclude_constraints | [ExcludeConstraintMetadata](#metaxisdata-v1-ExcludeConstraintMetadata) | repeated | The exclude_constraints is the list of EXCLUDE constraints in a table (PostgreSQL specific). |






<a name="metaxisdata-v1-TablePartitionMetadata"></a>

### TablePartitionMetadata
TablePartitionMetadata is the metadata for table partitions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the table partition. |
| type | [TablePartitionMetadata.Type](#metaxisdata-v1-TablePartitionMetadata-Type) |  | The type of a table partition. |
| expression | [string](#string) |  | The expression is the expression of a table partition. For PostgreSQL, the expression is the text of {FOR VALUES partition_bound_spec}, see https://www.postgresql.org/docs/current/sql-createtable.html. For MySQL, the expression is the `expr` or `column_list` of the following syntax. PARTITION BY { [LINEAR] HASH(expr) | [LINEAR] KEY [ALGORITHM={1 | 2}] (column_list) | RANGE{(expr) | COLUMNS(column_list)} | LIST{(expr) | COLUMNS(column_list)} }. |
| value | [string](#string) |  | The value is the value of a table partition. For MySQL, the value is for RANGE and LIST partition types, - For a RANGE partition, it contains the value set in the partition&#39;s VALUES LESS THAN clause, which can be either an integer or MAXVALUE. - For a LIST partition, this column contains the values defined in the partition&#39;s VALUES IN clause, which is a list of comma-separated integer values. - For others, it&#39;s an empty string. |
| use_default | [string](#string) |  | The use_default is whether the users use the default partition, it stores the different value for different database engines. For MySQL, it&#39;s [INT] type, 0 means not use default partition, otherwise, it&#39;s equals to number in syntax [SUB]PARTITION {number}. |
| subpartitions | [TablePartitionMetadata](#metaxisdata-v1-TablePartitionMetadata) | repeated | The subpartitions is the list of subpartitions in a table partition. |
| indexes | [IndexMetadata](#metaxisdata-v1-IndexMetadata) | repeated |  |
| check_constraints | [CheckConstraintMetadata](#metaxisdata-v1-CheckConstraintMetadata) | repeated |  |
| exclude_constraints | [ExcludeConstraintMetadata](#metaxisdata-v1-ExcludeConstraintMetadata) | repeated |  |






<a name="metaxisdata-v1-TriggerMetadata"></a>

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






<a name="metaxisdata-v1-UpdateManualSQLRequest"></a>

### UpdateManualSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| manual_sql | [ManualSQL](#metaxisdata-v1-ManualSQL) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |






<a name="metaxisdata-v1-ViewMetadata"></a>

### ViewMetadata
ViewMetadata is the metadata for views.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the view. |
| definition | [string](#string) |  | The definition is the definition of a view. |
| comment | [string](#string) |  | The comment is the comment of a view. |
| dependency_columns | [DependencyColumn](#metaxisdata-v1-DependencyColumn) | repeated | The list of dependency columns of a view. |
| columns | [ColumnMetadata](#metaxisdata-v1-ColumnMetadata) | repeated | The ordered list of columns in the view. |
| triggers | [TriggerMetadata](#metaxisdata-v1-TriggerMetadata) | repeated | The list of triggers in the view. |
| skip_dump | [bool](#bool) |  |  |
| rules | [RuleMetadata](#metaxisdata-v1-RuleMetadata) | repeated | The rules is the list of rules in a view (PostgreSQL specific). |





 


<a name="metaxisdata-v1-ColumnMetadata-IdentityGeneration"></a>

### ColumnMetadata.IdentityGeneration


| Name | Number | Description |
| ---- | ------ | ----------- |
| IDENTITY_GENERATION_UNSPECIFIED | 0 |  |
| ALWAYS | 1 |  |
| BY_DEFAULT | 2 |  |



<a name="metaxisdata-v1-GenerationMetadata-Type"></a>

### GenerationMetadata.Type


| Name | Number | Description |
| ---- | ------ | ----------- |
| TYPE_UNSPECIFIED | 0 |  |
| TYPE_VIRTUAL | 1 |  |
| TYPE_STORED | 2 |  |



<a name="metaxisdata-v1-MetaType"></a>

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
| OPENLINEAGE | 100 | OPENLINEAGE marks meta registry rows that back the OpenLineage service. See StoredMetadata for why these rows are not returned by the metadata methods. |



<a name="metaxisdata-v1-MetadataHistoryOperation"></a>

### MetadataHistoryOperation


| Name | Number | Description |
| ---- | ------ | ----------- |
| METADATA_HISTORY_OPERATION_UNSPECIFIED | 0 |  |
| METADATA_HISTORY_OPERATION_CREATED | 1 |  |
| METADATA_HISTORY_OPERATION_UPDATED | 2 |  |
| METADATA_HISTORY_OPERATION_DELETED | 3 |  |



<a name="metaxisdata-v1-MetadataHistorySection"></a>

### MetadataHistorySection


| Name | Number | Description |
| ---- | ------ | ----------- |
| METADATA_HISTORY_SECTION_UNSPECIFIED | 0 |  |
| METADATA_HISTORY_SECTION_SELF | 1 |  |
| METADATA_HISTORY_SECTION_COLUMN | 2 |  |
| METADATA_HISTORY_SECTION_INDEX | 3 |  |
| METADATA_HISTORY_SECTION_FOREIGN_KEY | 4 |  |
| METADATA_HISTORY_SECTION_CHECK_CONSTRAINT | 5 |  |
| METADATA_HISTORY_SECTION_PARTITION | 6 |  |
| METADATA_HISTORY_SECTION_TRIGGER | 7 |  |
| METADATA_HISTORY_SECTION_RULE | 8 |  |
| METADATA_HISTORY_SECTION_TAG | 9 |  |
| METADATA_HISTORY_SECTION_ATTRIBUTE | 10 |  |



<a name="metaxisdata-v1-TablePartitionMetadata-Type"></a>

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


 

 


<a name="metaxisdata-v1-DatabaseService"></a>

### DatabaseService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| SyncDatabase | [SyncDatabaseRequest](#metaxisdata-v1-SyncDatabaseRequest) | [SyncDatabaseResponse](#metaxisdata-v1-SyncDatabaseResponse) |  |
| ListDatabases | [ListDatabasesRequest](#metaxisdata-v1-ListDatabasesRequest) | [ListDatabasesResponse](#metaxisdata-v1-ListDatabasesResponse) |  |
| ListMetadata | [ListMetadataRequest](#metaxisdata-v1-ListMetadataRequest) | [MetadataResponse](#metaxisdata-v1-MetadataResponse) |  |
| GetMetadata | [GetMetadataRequest](#metaxisdata-v1-GetMetadataRequest) | [GetMetadataResponse](#metaxisdata-v1-GetMetadataResponse) |  |
| ListMetadataHistory | [ListMetadataHistoryRequest](#metaxisdata-v1-ListMetadataHistoryRequest) | [ListMetadataHistoryResponse](#metaxisdata-v1-ListMetadataHistoryResponse) |  |
| GetMetadataHistoryEvent | [GetMetadataHistoryEventRequest](#metaxisdata-v1-GetMetadataHistoryEventRequest) | [MetadataHistoryEvent](#metaxisdata-v1-MetadataHistoryEvent) |  |
| SearchMetadata | [SearchMetadataRequest](#metaxisdata-v1-SearchMetadataRequest) | [SearchMetadataResponse](#metaxisdata-v1-SearchMetadataResponse) |  |
| GetSchemaString | [GetSchemaStringRequest](#metaxisdata-v1-GetSchemaStringRequest) | [MetadataSchemaString](#metaxisdata-v1-MetadataSchemaString) | Generates schema DDL for a database object. |
| DiffMetadata | [DiffMetadataRequest](#metaxisdata-v1-DiffMetadataRequest) | [DiffMetadataResponse](#metaxisdata-v1-DiffMetadataResponse) | Computes the schema diff and migration DDL between two metadata versions. |
| CreateManualSQL | [CreateManualSQLRequest](#metaxisdata-v1-CreateManualSQLRequest) | [ManualSQL](#metaxisdata-v1-ManualSQL) |  |
| GetManualSQL | [GetManualSQLRequest](#metaxisdata-v1-GetManualSQLRequest) | [ManualSQL](#metaxisdata-v1-ManualSQL) |  |
| ListManualSQLs | [ListManualSQLsRequest](#metaxisdata-v1-ListManualSQLsRequest) | [ListManualSQLsResponse](#metaxisdata-v1-ListManualSQLsResponse) |  |
| SearchManualSQL | [SearchManualSQLRequest](#metaxisdata-v1-SearchManualSQLRequest) | [SearchManualSQLResponse](#metaxisdata-v1-SearchManualSQLResponse) |  |
| UpdateManualSQL | [UpdateManualSQLRequest](#metaxisdata-v1-UpdateManualSQLRequest) | [ManualSQL](#metaxisdata-v1-ManualSQL) |  |
| DeleteManualSQL | [DeleteManualSQLRequest](#metaxisdata-v1-DeleteManualSQLRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |

 



<a name="v1_explain_sql_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/explain_sql_service.proto



<a name="metaxisdata-v1-ExplainSQLMetadata"></a>

### ExplainSQLMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| summary | [string](#string) |  |  |
| sections_json | [string](#string) |  |  |
| provider | [string](#string) |  |  |
| model | [string](#string) |  |  |
| cache_key | [string](#string) |  |  |
| expired | [bool](#bool) |  |  |
| from_cache | [bool](#bool) |  |  |
| cache_created_at | [string](#string) |  |  |






<a name="metaxisdata-v1-ExplainSQLProgress"></a>

### ExplainSQLProgress



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  |  |
| turn | [int32](#int32) |  |  |
| tool_name | [string](#string) |  |  |
| tool_input | [string](#string) |  |  |
| tool_output | [string](#string) |  |  |
| tool_error | [string](#string) |  |  |






<a name="metaxisdata-v1-ExplainSQLRequest"></a>

### ExplainSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta_guid | [string](#string) |  |  |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  | Advisory type of the object named by meta_guid. The server resolves the authoritative type from the registry entry and ignores this field. |
| sql_text | [string](#string) |  |  |
| force_regenerate | [bool](#bool) |  |  |
| provider_name | [string](#string) |  |  |
| scope_prefix | [string](#string) |  |  |






<a name="metaxisdata-v1-ExplainSQLResponse"></a>

### ExplainSQLResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [string](#string) |  |  |
| metadata | [ExplainSQLMetadata](#metaxisdata-v1-ExplainSQLMetadata) |  |  |
| error | [string](#string) |  |  |
| progress | [ExplainSQLProgress](#metaxisdata-v1-ExplainSQLProgress) |  |  |





 

 

 


<a name="metaxisdata-v1-ExplainSQLService"></a>

### ExplainSQLService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ExplainSQL | [ExplainSQLRequest](#metaxisdata-v1-ExplainSQLRequest) | [ExplainSQLResponse](#metaxisdata-v1-ExplainSQLResponse) stream |  |

 



<a name="v1_group_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/group_service.proto



<a name="metaxisdata-v1-CreateGroupRequest"></a>

### CreateGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| group | [Group](#metaxisdata-v1-Group) |  |  |






<a name="metaxisdata-v1-DeleteGroupRequest"></a>

### DeleteGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the group, in the form `groups/{email}`. |






<a name="metaxisdata-v1-GetGroupRequest"></a>

### GetGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the group, in the form `groups/{email}`. |






<a name="metaxisdata-v1-Group"></a>

### Group
Group is a set of users that can be bound to roles in the IAM policy as a
single principal (`groups/{email}`).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the group, in the form `groups/{email}`. |
| title | [string](#string) |  | Human-readable title. |
| description | [string](#string) |  |  |
| members | [GroupMember](#metaxisdata-v1-GroupMember) | repeated | The group&#39;s members. |






<a name="metaxisdata-v1-GroupMember"></a>

### GroupMember
GroupMember is a user&#39;s membership in a group.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| member | [string](#string) |  | The member, in the form `users/{userUID}`. |
| role | [GroupMember.Role](#metaxisdata-v1-GroupMember-Role) |  |  |






<a name="metaxisdata-v1-ListGroupsRequest"></a>

### ListGroupsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of groups to return. |
| page_token | [string](#string) |  | A page token, received from a previous `ListGroups` call. |






<a name="metaxisdata-v1-ListGroupsResponse"></a>

### ListGroupsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| groups | [Group](#metaxisdata-v1-Group) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-UpdateGroupRequest"></a>

### UpdateGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| group | [Group](#metaxisdata-v1-Group) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |





 


<a name="metaxisdata-v1-GroupMember-Role"></a>

### GroupMember.Role


| Name | Number | Description |
| ---- | ------ | ----------- |
| ROLE_UNSPECIFIED | 0 |  |
| OWNER | 1 |  |
| MEMBER | 2 |  |


 

 


<a name="metaxisdata-v1-GroupService"></a>

### GroupService
GroupService manages groups. A group is an IAM principal: the workspace IAM
policy may bind roles to `groups/{email}`, which grants every member the
role. Each RPC is gated by the IAM interceptor with the metaxisdata.groups.*
permissions.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetGroup | [GetGroupRequest](#metaxisdata-v1-GetGroupRequest) | [Group](#metaxisdata-v1-Group) | Get a group. |
| ListGroups | [ListGroupsRequest](#metaxisdata-v1-ListGroupsRequest) | [ListGroupsResponse](#metaxisdata-v1-ListGroupsResponse) | List all groups. |
| CreateGroup | [CreateGroupRequest](#metaxisdata-v1-CreateGroupRequest) | [Group](#metaxisdata-v1-Group) | Create a group. |
| UpdateGroup | [UpdateGroupRequest](#metaxisdata-v1-UpdateGroupRequest) | [Group](#metaxisdata-v1-Group) | Update a group. |
| DeleteGroup | [DeleteGroupRequest](#metaxisdata-v1-DeleteGroupRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete a group. The group must not be referenced by any IAM binding. |

 



<a name="v1_iam_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/iam_service.proto



<a name="metaxisdata-v1-Binding"></a>

### Binding
Binding binds one role to a set of principals. It is the v1 view of the
stored IAM binding; the workspace IAM policy is written whole through
IamService.SetWorkspaceIamPolicy.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [string](#string) |  | The role that is assigned to the members. Format: roles/{role} |
| members | [string](#string) | repeated | The principals requesting access. For users: users/{userUID}; for groups: groups/{email}; the pseudo-member allUsers matches every authenticated principal. |
| condition | [google.type.Expr](#google-type-Expr) |  | The condition that is associated with this binding. When present the binding applies only while the condition evaluates to true. |






<a name="metaxisdata-v1-GetWorkspaceIamPolicyRequest"></a>

### GetWorkspaceIamPolicyRequest







<a name="metaxisdata-v1-IamPolicy"></a>

### IamPolicy



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| bindings | [Binding](#metaxisdata-v1-Binding) | repeated | A binding binds one role to one or more members. |






<a name="metaxisdata-v1-IamPolicyView"></a>

### IamPolicyView
IamPolicyView is an IAM policy together with its etag. The etag is returned
by Get and must be supplied on Set for optimistic concurrency: a Set whose
etag does not match the stored policy&#39;s etag is rejected with
connect.CodeAborted so the caller can re-fetch and retry. An empty etag
skips the check (first write).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [IamPolicy](#metaxisdata-v1-IamPolicy) |  |  |
| etag | [string](#string) |  |  |






<a name="metaxisdata-v1-SetWorkspaceIamPolicyRequest"></a>

### SetWorkspaceIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [IamPolicy](#metaxisdata-v1-IamPolicy) |  |  |
| etag | [string](#string) |  | The etag from the last GetWorkspaceIamPolicy. Empty means &#34;do not check&#34;. |





 

 

 


<a name="metaxisdata-v1-IamService"></a>

### IamService
IamService exposes the workspace IAM policy for management. Get reads the
whole policy; Set replaces it whole, guarded by the etag. Both RPCs are gated
by metaxisdata.iam.getPolicy / metaxisdata.iam.setPolicy.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetWorkspaceIamPolicy | [GetWorkspaceIamPolicyRequest](#metaxisdata-v1-GetWorkspaceIamPolicyRequest) | [IamPolicyView](#metaxisdata-v1-IamPolicyView) | Get the workspace IAM policy. |
| SetWorkspaceIamPolicy | [SetWorkspaceIamPolicyRequest](#metaxisdata-v1-SetWorkspaceIamPolicyRequest) | [IamPolicyView](#metaxisdata-v1-IamPolicyView) | Set the workspace IAM policy (full replace, etag-guarded). |

 



<a name="v1_lineage_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/lineage_service.proto



<a name="metaxisdata-v1-ExternalDatasetInfo"></a>

### ExternalDatasetInfo
ExternalDatasetInfo provides metadata for a dataset outside of managed instances.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| namespace | [string](#string) |  | The OpenLineage namespace, e.g. &#34;postgres://host:5432&#34;. |
| name | [string](#string) |  | The dataset name, e.g. &#34;db.schema.table&#34; or &#34;s3://bucket/path&#34;. |
| dataset_type | [string](#string) |  | The dataset type, e.g. &#34;s3&#34;, &#34;kafka&#34;, &#34;database&#34;. |






<a name="metaxisdata-v1-GetLineageForContextRequest"></a>

### GetLineageForContextRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The global unique id for metadata view: &#34;instance_1;db2;schema3;view1&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| page_size | [int32](#int32) |  | The maximum number of relations to return. The service may return fewer than this value. If unspecified, at most 500 relations are returned. The maximum value is 5000; values above 5000 will be coerced to 5000. |
| page_token | [string](#string) |  | A page token, received from a previous `GetLineageForContext` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `GetLineageForContext` must match the call that provided the page token. |






<a name="metaxisdata-v1-GetLineageForContextResponse"></a>

### GetLineageForContextResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| relations | [LineageRelation](#metaxisdata-v1-LineageRelation) | repeated | The list of lineage relations for the given metadata. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-GetLineageRequest"></a>

### GetLineageRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The global unique id for metadata table: &#34;instance_1;db2;schema3;table4&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| lineage_type | [LineageType](#metaxisdata-v1-LineageType) |  | The lineage type to query, source or target. If not specified, both source and target lineage will be returned. |
| page_size | [int32](#int32) |  | The maximum number of relations to return. The service may return fewer than this value. page_size applies to relations_source and relations_target separately. If unspecified, at most 500 relations are returned per list. The maximum value is 5000; values above 5000 will be coerced to 5000. |
| page_token | [string](#string) |  | A page token, received from a previous `GetLineage` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `GetLineage` must match the call that provided the page token. |






<a name="metaxisdata-v1-GetLineageResponse"></a>

### GetLineageResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| relations_source | [LineageRelation](#metaxisdata-v1-LineageRelation) | repeated | The list of lineage relations for the given metadata. |
| relations_target | [LineageRelation](#metaxisdata-v1-LineageRelation) | repeated |  |
| external_datasets | [ExternalDatasetInfo](#metaxisdata-v1-ExternalDatasetInfo) | repeated | Metadata for external datasets referenced in the lineage relations. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-LineageRelation"></a>

### LineageRelation



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [int64](#int64) |  |  |
| meta_guid | [string](#string) |  |  |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| source_guid | [string](#string) |  |  |
| source_column | [string](#string) |  |  |
| source_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| target_guid | [string](#string) |  |  |
| target_column | [string](#string) |  |  |
| target_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| relation_type | [RelationType](#metaxisdata-v1-RelationType) |  |  |
| transformations | [Transformation](#metaxisdata-v1-Transformation) | repeated |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-Transformation"></a>

### Transformation
Transformation describes one step of how a source column becomes a target
column, derived from the view&#39;s SQL.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| operation | [string](#string) |  | The transformation kind: DELETE, UNION, PROJECT, FUNCTION, AGGREGATE, WINDOW, OPERATOR or CASE. |
| expression | [string](#string) |  | The text representation of the expression (most kinds). |
| function_name | [string](#string) |  | The function name (FUNCTION, AGGREGATE, WINDOW). |
| arguments | [string](#string) | repeated | The function arguments (FUNCTION). |
| group_keys | [string](#string) | repeated | The GROUP BY keys (AGGREGATE). |
| partition_by | [string](#string) | repeated | The PARTITION BY columns (WINDOW). |
| order_by | [string](#string) | repeated | The ORDER BY columns (WINDOW). |
| op_type | [string](#string) |  | The operator type, e.g. &#34;&#43;&#34;, &#34;=&#34; (OPERATOR). |
| condition | [string](#string) |  | The WHERE condition (DELETE). |





 


<a name="metaxisdata-v1-LineageType"></a>

### LineageType


| Name | Number | Description |
| ---- | ------ | ----------- |
| LINEAGE_TYPE_UNSPECIFIED | 0 |  |
| SOURCE | 1 |  |
| TARGET | 2 |  |



<a name="metaxisdata-v1-RelationType"></a>

### RelationType


| Name | Number | Description |
| ---- | ------ | ----------- |
| RELATION_TYPE_UNSPECIFIED | 0 |  |
| DIRECT | 1 | DIRECT means the source column is directly used in the target column without transformation. For example: select source_column as target_column from table. |
| INDIRECT | 2 | INDIRECT means the source column is used in the target column with transformation. For example: select concat(source_column, &#39;abc&#39;) as target_column |


 

 


<a name="metaxisdata-v1-LineageService"></a>

### LineageService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetLineage | [GetLineageRequest](#metaxisdata-v1-GetLineageRequest) | [GetLineageResponse](#metaxisdata-v1-GetLineageResponse) | GetLineage returns the lineage relations for the given metadata. The lineage relations can be either source lineage or target lineage, depending on the lineage_type specified in the request. If lineage_type is not specified, both source and target lineage will be returned. |
| GetLineageForContext | [GetLineageForContextRequest](#metaxisdata-v1-GetLineageForContextRequest) | [GetLineageForContextResponse](#metaxisdata-v1-GetLineageForContextResponse) | GetLineageForContext retrieves the field-level lineage graph derived from a specific SQL context (e.g., view, stored procedure). |

 



<a name="v1_llm_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/llm_service.proto



<a name="metaxisdata-v1-CreateLLMProviderProfileRequest"></a>

### CreateLLMProviderProfileRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| profile | [LlmProviderProfile](#metaxisdata-v1-LlmProviderProfile) |  |  |






<a name="metaxisdata-v1-DeleteLLMProviderProfileRequest"></a>

### DeleteLLMProviderProfileRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="metaxisdata-v1-FetchLLMModelsRequest"></a>

### FetchLLMModelsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| api_key | [string](#string) |  |  |
| provider_type | [LLMProviderType](#metaxisdata-v1-LLMProviderType) |  |  |






<a name="metaxisdata-v1-FetchLLMModelsResponse"></a>

### FetchLLMModelsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| model_ids | [string](#string) | repeated |  |






<a name="metaxisdata-v1-ListLLMProviderProfilesRequest"></a>

### ListLLMProviderProfilesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListLLMProviderProfilesResponse"></a>

### ListLLMProviderProfilesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| profiles | [LlmProviderProfile](#metaxisdata-v1-LlmProviderProfile) | repeated |  |
| definitions | [LlmProviderDefinition](#metaxisdata-v1-LlmProviderDefinition) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-LlmProviderDefinition"></a>

### LlmProviderDefinition



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| label | [string](#string) |  |  |
| description | [string](#string) |  |  |
| default_base_url | [string](#string) |  |  |






<a name="metaxisdata-v1-LlmProviderModel"></a>

### LlmProviderModel



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| enabled | [bool](#bool) |  |  |






<a name="metaxisdata-v1-LlmProviderProfile"></a>

### LlmProviderProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| title | [string](#string) |  |  |
| type | [LLMProviderType](#metaxisdata-v1-LLMProviderType) |  |  |
| base_url | [string](#string) |  |  |
| api_key | [string](#string) |  |  |
| models | [LlmProviderModel](#metaxisdata-v1-LlmProviderModel) | repeated |  |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| update_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| masked_api_key | [string](#string) |  |  |






<a name="metaxisdata-v1-UpdateLLMProviderProfileRequest"></a>

### UpdateLLMProviderProfileRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| profile | [LlmProviderProfile](#metaxisdata-v1-LlmProviderProfile) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |





 


<a name="metaxisdata-v1-LLMProviderType"></a>

### LLMProviderType


| Name | Number | Description |
| ---- | ------ | ----------- |
| LLM_PROVIDER_TYPE_UNSPECIFIED | 0 |  |
| LLM_PROVIDER_TYPE_OPENAI | 1 |  |
| LLM_PROVIDER_TYPE_DEEPSEEK | 2 |  |
| LLM_PROVIDER_TYPE_OPENROUTER | 3 |  |
| LLM_PROVIDER_TYPE_CUSTOM | 4 |  |


 

 


<a name="metaxisdata-v1-LLMService"></a>

### LLMService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListLLMProviderProfiles | [ListLLMProviderProfilesRequest](#metaxisdata-v1-ListLLMProviderProfilesRequest) | [ListLLMProviderProfilesResponse](#metaxisdata-v1-ListLLMProviderProfilesResponse) |  |
| CreateLLMProviderProfile | [CreateLLMProviderProfileRequest](#metaxisdata-v1-CreateLLMProviderProfileRequest) | [LlmProviderProfile](#metaxisdata-v1-LlmProviderProfile) |  |
| UpdateLLMProviderProfile | [UpdateLLMProviderProfileRequest](#metaxisdata-v1-UpdateLLMProviderProfileRequest) | [LlmProviderProfile](#metaxisdata-v1-LlmProviderProfile) |  |
| DeleteLLMProviderProfile | [DeleteLLMProviderProfileRequest](#metaxisdata-v1-DeleteLLMProviderProfileRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| FetchLLMModels | [FetchLLMModelsRequest](#metaxisdata-v1-FetchLLMModelsRequest) | [FetchLLMModelsResponse](#metaxisdata-v1-FetchLLMModelsResponse) |  |

 



<a name="v1_openlineage_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/openlineage_service.proto



<a name="metaxisdata-v1-APIKey"></a>

### APIKey



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the API key. Format: openlineage/apiKeys/{api_key} |
| masked_key | [string](#string) |  |  |
| description | [string](#string) |  |  |
| created_by | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_used_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| revoked_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Non-empty only when the key is revoked. |
| scope_namespace | [string](#string) |  | The OpenLineage namespace this key may write to. Empty means the key is unscoped and may submit events for any namespace. Ingestion rejects an event whose job or dataset namespace differs from the scope. |






<a name="metaxisdata-v1-CreateAPIKeyRequest"></a>

### CreateAPIKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| description | [string](#string) |  |  |
| scope_namespace | [string](#string) |  | Optional OpenLineage namespace to restrict the key to. Empty creates an unscoped key, which is what the previous behavior always did. |






<a name="metaxisdata-v1-CreateAPIKeyResponse"></a>

### CreateAPIKeyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  | The plain-text API key. Only returned once at creation time. |
| api_key | [APIKey](#metaxisdata-v1-APIKey) |  |  |






<a name="metaxisdata-v1-CreateNamespaceMappingRequest"></a>

### CreateNamespaceMappingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mapping | [NamespaceMapping](#metaxisdata-v1-NamespaceMapping) |  | The mapping to create. The server assigns its resource ID. |






<a name="metaxisdata-v1-DeleteNamespaceMappingRequest"></a>

### DeleteNamespaceMappingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the mapping to delete. Format: openlineage/namespaceMappings/{namespace_mapping} |






<a name="metaxisdata-v1-GetOpenLineageDatasetRequest"></a>

### GetOpenLineageDatasetRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |






<a name="metaxisdata-v1-GetOpenLineageRunRequest"></a>

### GetOpenLineageRunRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the run to retrieve. Format: openlineage/runs/{run} |






<a name="metaxisdata-v1-GetOpenLineageTaskRequest"></a>

### GetOpenLineageTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the task to retrieve. Format: openlineage/tasks/{task} |






<a name="metaxisdata-v1-ListAPIKeysRequest"></a>

### ListAPIKeysRequest







<a name="metaxisdata-v1-ListAPIKeysResponse"></a>

### ListAPIKeysResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_keys | [APIKey](#metaxisdata-v1-APIKey) | repeated |  |






<a name="metaxisdata-v1-ListNamespaceMappingsRequest"></a>

### ListNamespaceMappingsRequest







<a name="metaxisdata-v1-ListNamespaceMappingsResponse"></a>

### ListNamespaceMappingsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mappings | [NamespaceMapping](#metaxisdata-v1-NamespaceMapping) | repeated |  |






<a name="metaxisdata-v1-ListOpenLineageDatasetsRequest"></a>

### ListOpenLineageDatasetsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| search | [string](#string) |  |  |
| namespace | [string](#string) |  |  |
| integration | [string](#string) |  |  |
| source | [string](#string) |  |  |
| dataset_scope | [OpenLineageDatasetScope](#metaxisdata-v1-OpenLineageDatasetScope) |  |  |
| column_lineage_only | [bool](#bool) |  |  |






<a name="metaxisdata-v1-ListOpenLineageDatasetsResponse"></a>

### ListOpenLineageDatasetsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| datasets | [OpenLineageDatasetResource](#metaxisdata-v1-OpenLineageDatasetResource) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListOpenLineageRunsRequest"></a>

### ListOpenLineageRunsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| job_namespace | [string](#string) |  |  |
| job_name | [string](#string) |  |  |
| task_guid | [string](#string) |  |  |
| job_type | [string](#string) |  |  |
| event_type | [string](#string) |  |  |
| has_lineage | [bool](#bool) |  |  |






<a name="metaxisdata-v1-ListOpenLineageRunsResponse"></a>

### ListOpenLineageRunsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| runs | [OpenLineageRun](#metaxisdata-v1-OpenLineageRun) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListOpenLineageTasksRequest"></a>

### ListOpenLineageTasksRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| job_namespace | [string](#string) |  |  |
| job_name | [string](#string) |  |  |
| job_type | [string](#string) |  |  |
| lineage_only | [bool](#bool) |  |  |






<a name="metaxisdata-v1-ListOpenLineageTasksResponse"></a>

### ListOpenLineageTasksResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tasks | [OpenLineageTask](#metaxisdata-v1-OpenLineageTask) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-NamespaceMapping"></a>

### NamespaceMapping



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the namespace mapping. Format: openlineage/namespaceMappings/{namespace_mapping} |
| namespace | [string](#string) |  |  |
| instance_resource_id | [string](#string) |  |  |
| database_name | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-OpenLineageDatasetDetailResource"></a>

### OpenLineageDatasetDetailResource



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| dataset | [OpenLineageDatasetResource](#metaxisdata-v1-OpenLineageDatasetResource) |  |  |
| schema_fields | [OpenLineageDatasetField](#metaxisdata-v1-OpenLineageDatasetField) | repeated |  |
| related_jobs | [OpenLineageDatasetJobResource](#metaxisdata-v1-OpenLineageDatasetJobResource) | repeated |  |
| recent_runs | [OpenLineageDatasetRunResource](#metaxisdata-v1-OpenLineageDatasetRunResource) | repeated |  |






<a name="metaxisdata-v1-OpenLineageDatasetField"></a>

### OpenLineageDatasetField



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| type | [string](#string) |  |  |
| description | [string](#string) |  |  |
| column_lineage_ready | [bool](#bool) |  |  |






<a name="metaxisdata-v1-OpenLineageDatasetJobResource"></a>

### OpenLineageDatasetJobResource



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| task_guid | [string](#string) |  |  |
| job_namespace | [string](#string) |  |  |
| job_name | [string](#string) |  |  |
| job_type | [string](#string) |  |  |
| integration | [string](#string) |  |  |
| last_seen | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| run_count | [int32](#int32) |  |  |
| reads_dataset | [bool](#bool) |  |  |
| writes_dataset | [bool](#bool) |  |  |






<a name="metaxisdata-v1-OpenLineageDatasetResource"></a>

### OpenLineageDatasetResource



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| namespace | [string](#string) |  |  |
| name | [string](#string) |  |  |
| dataset_type | [string](#string) |  |  |
| resolved_target | [string](#string) |  |  |
| resolved_meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| internal | [bool](#bool) |  |  |
| supports_column_lineage | [bool](#bool) |  |  |
| last_seen | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| source_job_count | [int32](#int32) |  |  |
| target_job_count | [int32](#int32) |  |  |
| integrations | [string](#string) | repeated |  |
| sources | [string](#string) | repeated |  |






<a name="metaxisdata-v1-OpenLineageDatasetRunResource"></a>

### OpenLineageDatasetRunResource



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
| has_lineage | [bool](#bool) |  |  |
| reads_dataset | [bool](#bool) |  |  |
| writes_dataset | [bool](#bool) |  |  |






<a name="metaxisdata-v1-OpenLineageRun"></a>

### OpenLineageRun



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the run. Format: openlineage/runs/{guid} |
| guid | [string](#string) |  | The run identifier, equal to the last segment of `name`. |
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
| raw_payload | [string](#string) |  | The OpenLineage event as JSON text, exactly as it was received and stored in the `raw_payload` JSONB column. |
| airflow_dag_url | [string](#string) |  |  |
| airflow_run_log_url | [string](#string) |  |  |






<a name="metaxisdata-v1-OpenLineageTask"></a>

### OpenLineageTask



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the task. Format: openlineage/tasks/{guid} |
| guid | [string](#string) |  | The task identifier, equal to the last segment of `name`. |
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
| airflow_dag_url | [string](#string) |  |  |
| latest_event_type | [string](#string) |  |  |






<a name="metaxisdata-v1-RevokeAPIKeyRequest"></a>

### RevokeAPIKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the API key to revoke. Format: openlineage/apiKeys/{api_key} |






<a name="metaxisdata-v1-UpdateNamespaceMappingRequest"></a>

### UpdateNamespaceMappingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mapping | [NamespaceMapping](#metaxisdata-v1-NamespaceMapping) |  | The mapping to update. Its `name` field identifies the row. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. When omitted, the fields the request carries are updated; `database_name` is always written so it can be cleared. |





 


<a name="metaxisdata-v1-OpenLineageDatasetScope"></a>

### OpenLineageDatasetScope


| Name | Number | Description |
| ---- | ------ | ----------- |
| OPENLINEAGE_DATASET_SCOPE_UNSPECIFIED | 0 |  |
| OPENLINEAGE_DATASET_SCOPE_ALL | 1 |  |
| OPENLINEAGE_DATASET_SCOPE_INTERNAL | 2 |  |
| OPENLINEAGE_DATASET_SCOPE_EXTERNAL | 3 |  |


 

 


<a name="metaxisdata-v1-OpenLineageService"></a>

### OpenLineageService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListOpenLineageTasks | [ListOpenLineageTasksRequest](#metaxisdata-v1-ListOpenLineageTasksRequest) | [ListOpenLineageTasksResponse](#metaxisdata-v1-ListOpenLineageTasksResponse) |  |
| ListOpenLineageDatasets | [ListOpenLineageDatasetsRequest](#metaxisdata-v1-ListOpenLineageDatasetsRequest) | [ListOpenLineageDatasetsResponse](#metaxisdata-v1-ListOpenLineageDatasetsResponse) |  |
| GetOpenLineageDataset | [GetOpenLineageDatasetRequest](#metaxisdata-v1-GetOpenLineageDatasetRequest) | [OpenLineageDatasetDetailResource](#metaxisdata-v1-OpenLineageDatasetDetailResource) |  |
| GetOpenLineageTask | [GetOpenLineageTaskRequest](#metaxisdata-v1-GetOpenLineageTaskRequest) | [OpenLineageTask](#metaxisdata-v1-OpenLineageTask) |  |
| ListOpenLineageRuns | [ListOpenLineageRunsRequest](#metaxisdata-v1-ListOpenLineageRunsRequest) | [ListOpenLineageRunsResponse](#metaxisdata-v1-ListOpenLineageRunsResponse) |  |
| GetOpenLineageRun | [GetOpenLineageRunRequest](#metaxisdata-v1-GetOpenLineageRunRequest) | [OpenLineageRun](#metaxisdata-v1-OpenLineageRun) |  |
| CreateNamespaceMapping | [CreateNamespaceMappingRequest](#metaxisdata-v1-CreateNamespaceMappingRequest) | [NamespaceMapping](#metaxisdata-v1-NamespaceMapping) |  |
| ListNamespaceMappings | [ListNamespaceMappingsRequest](#metaxisdata-v1-ListNamespaceMappingsRequest) | [ListNamespaceMappingsResponse](#metaxisdata-v1-ListNamespaceMappingsResponse) |  |
| UpdateNamespaceMapping | [UpdateNamespaceMappingRequest](#metaxisdata-v1-UpdateNamespaceMappingRequest) | [NamespaceMapping](#metaxisdata-v1-NamespaceMapping) |  |
| DeleteNamespaceMapping | [DeleteNamespaceMappingRequest](#metaxisdata-v1-DeleteNamespaceMappingRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| CreateAPIKey | [CreateAPIKeyRequest](#metaxisdata-v1-CreateAPIKeyRequest) | [CreateAPIKeyResponse](#metaxisdata-v1-CreateAPIKeyResponse) |  |
| ListAPIKeys | [ListAPIKeysRequest](#metaxisdata-v1-ListAPIKeysRequest) | [ListAPIKeysResponse](#metaxisdata-v1-ListAPIKeysResponse) |  |
| RevokeAPIKey | [RevokeAPIKeyRequest](#metaxisdata-v1-RevokeAPIKeyRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |

 



<a name="v1_role_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/role_service.proto



<a name="metaxisdata-v1-CreateRoleRequest"></a>

### CreateRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [Role](#metaxisdata-v1-Role) |  |  |






<a name="metaxisdata-v1-DeleteRoleRequest"></a>

### DeleteRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the role, in the form `roles/{role}`. |






<a name="metaxisdata-v1-GetRoleRequest"></a>

### GetRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the role, in the form `roles/{role}`. |






<a name="metaxisdata-v1-ListRolesRequest"></a>

### ListRolesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of roles to return. |
| page_token | [string](#string) |  | A page token, received from a previous `ListRoles` call. |






<a name="metaxisdata-v1-ListRolesResponse"></a>

### ListRolesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| roles | [Role](#metaxisdata-v1-Role) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-Role"></a>

### Role
Role is a named bundle of permissions. Predefined roles (workspaceAdmin,
workspaceMember) are defined in Go and never stored in the DB; custom roles
live in the role table. Both resolve identically in the IAM engine.
Predefined roles are read-only over this API.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the role, in the form `roles/{role}`. |
| title | [string](#string) |  | Human-readable title. |
| description | [string](#string) |  | Longer description of what the role grants. |
| permissions | [string](#string) | repeated | Permissions bundled into the role, each a `metaxisdata.&lt;resource&gt;.&lt;verb&gt;` string from the permission catalog. |
| predefined | [bool](#bool) |  | Output only. Whether the role is predefined (defined in Go, read-only). |






<a name="metaxisdata-v1-UpdateRoleRequest"></a>

### UpdateRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [Role](#metaxisdata-v1-Role) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |





 

 

 


<a name="metaxisdata-v1-RoleService"></a>

### RoleService
RoleService manages custom roles. Predefined roles are read-only over this
API: create/update/delete refuse a resource ID that collides with a
predefined role. Each RPC is gated by the IAM interceptor with the
metaxisdata.roles.* permissions.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetRole | [GetRoleRequest](#metaxisdata-v1-GetRoleRequest) | [Role](#metaxisdata-v1-Role) | Get a role. |
| ListRoles | [ListRolesRequest](#metaxisdata-v1-ListRolesRequest) | [ListRolesResponse](#metaxisdata-v1-ListRolesResponse) | List all roles (predefined and custom). |
| CreateRole | [CreateRoleRequest](#metaxisdata-v1-CreateRoleRequest) | [Role](#metaxisdata-v1-Role) | Create a custom role. |
| UpdateRole | [UpdateRoleRequest](#metaxisdata-v1-UpdateRoleRequest) | [Role](#metaxisdata-v1-Role) | Update a custom role. |
| DeleteRole | [DeleteRoleRequest](#metaxisdata-v1-DeleteRoleRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete a custom role. |

 



<a name="v1_setting_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/setting_service.proto



<a name="metaxisdata-v1-GetDebugConfigRequest"></a>

### GetDebugConfigRequest







<a name="metaxisdata-v1-GetDebugConfigResponse"></a>

### GetDebugConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enabled | [bool](#bool) |  | Whether runtime debug mode is currently enabled. |






<a name="metaxisdata-v1-GetWorkspaceProfileSettingRequest"></a>

### GetWorkspaceProfileSettingRequest







<a name="metaxisdata-v1-UpdateDebugConfigRequest"></a>

### UpdateDebugConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enabled | [bool](#bool) |  | Whether to enable runtime debug mode. |






<a name="metaxisdata-v1-UpdateDebugConfigResponse"></a>

### UpdateDebugConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enabled | [bool](#bool) |  | The resulting runtime debug mode. |






<a name="metaxisdata-v1-UpdateWorkspaceProfileSettingRequest"></a>

### UpdateWorkspaceProfileSettingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| setting | [WorkspaceProfileSetting](#metaxisdata-v1-WorkspaceProfileSetting) |  | The setting to update. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. |






<a name="metaxisdata-v1-WorkspaceProfileSetting"></a>

### WorkspaceProfileSetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| external_url | [string](#string) |  | The external URL used for the SSO authentication callback. |
| disallow_signup | [bool](#bool) |  | Disallow self-service signup. When enabled, only a workspace admin can create users. |
| disallow_password_signin | [bool](#bool) |  | Disallow password signin. Workspace admins are exempt. |
| openlineage_retention_days | [int32](#int32) |  | The number of days persisted OpenLineage runs are kept. Zero (the default) keeps them forever. |





 

 

 


<a name="metaxisdata-v1-SettingService"></a>

### SettingService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetWorkspaceProfileSetting | [GetWorkspaceProfileSettingRequest](#metaxisdata-v1-GetWorkspaceProfileSettingRequest) | [WorkspaceProfileSetting](#metaxisdata-v1-WorkspaceProfileSetting) | Get the workspace profile setting. Permissions required: None

This method is readable without credentials because the login page needs to know whether self-service signup is enabled. |
| UpdateWorkspaceProfileSetting | [UpdateWorkspaceProfileSettingRequest](#metaxisdata-v1-UpdateWorkspaceProfileSettingRequest) | [WorkspaceProfileSetting](#metaxisdata-v1-WorkspaceProfileSetting) | Update the workspace profile setting. |
| GetDebugConfig | [GetDebugConfigRequest](#metaxisdata-v1-GetDebugConfigRequest) | [GetDebugConfigResponse](#metaxisdata-v1-GetDebugConfigResponse) | Get the workspace runtime debug config. |
| UpdateDebugConfig | [UpdateDebugConfigRequest](#metaxisdata-v1-UpdateDebugConfigRequest) | [UpdateDebugConfigResponse](#metaxisdata-v1-UpdateDebugConfigResponse) | Update the workspace runtime debug config.

Enabling it switches the process-wide log level to debug, gates the verbose request logging emitted by the debug interceptor, exposes /debug/pprof, and allows panic handlers to return stack traces to the caller. Disabling it restores info-level logging and generic panic errors. |

 



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

