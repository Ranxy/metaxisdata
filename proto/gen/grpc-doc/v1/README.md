# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [v1/annotation.proto](#v1_annotation-proto)
    - [AuthMethod](#metaxisdata-v1-AuthMethod)
  
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
  
- [v1/common.proto](#v1_common-proto)
    - [Position](#metaxisdata-v1-Position)
    - [Range](#metaxisdata-v1-Range)
  
    - [Engine](#metaxisdata-v1-Engine)
    - [RiskLevel](#metaxisdata-v1-RiskLevel)
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
    - [IdentityProviderContext](#metaxisdata-v1-IdentityProviderContext)
    - [LoginRequest](#metaxisdata-v1-LoginRequest)
    - [LoginResponse](#metaxisdata-v1-LoginResponse)
    - [LogoutRequest](#metaxisdata-v1-LogoutRequest)
    - [OAuth2IdentityProviderContext](#metaxisdata-v1-OAuth2IdentityProviderContext)
  
    - [AuthService](#metaxisdata-v1-AuthService)
  
- [v1/instance_service.proto](#v1_instance_service-proto)
    - [AddDataSourceRequest](#metaxisdata-v1-AddDataSourceRequest)
    - [BatchSyncInstancesRequest](#metaxisdata-v1-BatchSyncInstancesRequest)
    - [BatchSyncInstancesResponse](#metaxisdata-v1-BatchSyncInstancesResponse)
    - [BatchUpdateInstancesRequest](#metaxisdata-v1-BatchUpdateInstancesRequest)
    - [BatchUpdateInstancesResponse](#metaxisdata-v1-BatchUpdateInstancesResponse)
    - [CreateInstanceRequest](#metaxisdata-v1-CreateInstanceRequest)
    - [DataSource](#metaxisdata-v1-DataSource)
    - [DataSource.AWSCredential](#metaxisdata-v1-DataSource-AWSCredential)
    - [DataSource.Address](#metaxisdata-v1-DataSource-Address)
    - [DataSource.AzureCredential](#metaxisdata-v1-DataSource-AzureCredential)
    - [DataSource.ExtraConnectionParametersEntry](#metaxisdata-v1-DataSource-ExtraConnectionParametersEntry)
    - [DataSource.GCPCredential](#metaxisdata-v1-DataSource-GCPCredential)
    - [DataSourceExternalSecret](#metaxisdata-v1-DataSourceExternalSecret)
    - [DataSourceExternalSecret.AppRoleAuthOption](#metaxisdata-v1-DataSourceExternalSecret-AppRoleAuthOption)
    - [DeleteInstanceRequest](#metaxisdata-v1-DeleteInstanceRequest)
    - [GetInstanceRequest](#metaxisdata-v1-GetInstanceRequest)
    - [Instance](#metaxisdata-v1-Instance)
    - [InstanceResource](#metaxisdata-v1-InstanceResource)
    - [KerberosConfig](#metaxisdata-v1-KerberosConfig)
    - [ListInstanceDatabaseRequest](#metaxisdata-v1-ListInstanceDatabaseRequest)
    - [ListInstanceDatabaseResponse](#metaxisdata-v1-ListInstanceDatabaseResponse)
    - [ListInstancesRequest](#metaxisdata-v1-ListInstancesRequest)
    - [ListInstancesResponse](#metaxisdata-v1-ListInstancesResponse)
    - [RemoveDataSourceRequest](#metaxisdata-v1-RemoveDataSourceRequest)
    - [SASLConfig](#metaxisdata-v1-SASLConfig)
    - [SyncInstanceRequest](#metaxisdata-v1-SyncInstanceRequest)
    - [SyncInstanceResponse](#metaxisdata-v1-SyncInstanceResponse)
    - [UndeleteInstanceRequest](#metaxisdata-v1-UndeleteInstanceRequest)
    - [UpdateDataSourceRequest](#metaxisdata-v1-UpdateDataSourceRequest)
    - [UpdateInstanceRequest](#metaxisdata-v1-UpdateInstanceRequest)
  
    - [DataSource.AuthenticationType](#metaxisdata-v1-DataSource-AuthenticationType)
    - [DataSource.RedisType](#metaxisdata-v1-DataSource-RedisType)
    - [DataSourceExternalSecret.AppRoleAuthOption.SecretType](#metaxisdata-v1-DataSourceExternalSecret-AppRoleAuthOption-SecretType)
    - [DataSourceExternalSecret.AuthType](#metaxisdata-v1-DataSourceExternalSecret-AuthType)
    - [DataSourceExternalSecret.SecretType](#metaxisdata-v1-DataSourceExternalSecret-SecretType)
    - [DataSourceType](#metaxisdata-v1-DataSourceType)
  
    - [InstanceService](#metaxisdata-v1-InstanceService)
  
- [v1/database_service.proto](#v1_database_service-proto)
    - [BoundingBox](#metaxisdata-v1-BoundingBox)
    - [CheckConstraintMetadata](#metaxisdata-v1-CheckConstraintMetadata)
    - [ColumnMetadata](#metaxisdata-v1-ColumnMetadata)
    - [Database](#metaxisdata-v1-Database)
    - [Database.LabelsEntry](#metaxisdata-v1-Database-LabelsEntry)
    - [DatabaseSchemaMetadata](#metaxisdata-v1-DatabaseSchemaMetadata)
    - [DependencyColumn](#metaxisdata-v1-DependencyColumn)
    - [DependencyTable](#metaxisdata-v1-DependencyTable)
    - [DimensionalConfig](#metaxisdata-v1-DimensionalConfig)
    - [EnumTypeMetadata](#metaxisdata-v1-EnumTypeMetadata)
    - [EventMetadata](#metaxisdata-v1-EventMetadata)
    - [EventTriggerMetadata](#metaxisdata-v1-EventTriggerMetadata)
    - [ExcludeConstraintMetadata](#metaxisdata-v1-ExcludeConstraintMetadata)
    - [ExtensionMetadata](#metaxisdata-v1-ExtensionMetadata)
    - [ExternalTableMetadata](#metaxisdata-v1-ExternalTableMetadata)
    - [ForeignKeyMetadata](#metaxisdata-v1-ForeignKeyMetadata)
    - [FunctionMetadata](#metaxisdata-v1-FunctionMetadata)
    - [GenerationMetadata](#metaxisdata-v1-GenerationMetadata)
    - [GetDatabaseRequest](#metaxisdata-v1-GetDatabaseRequest)
    - [GetMetadataRequest](#metaxisdata-v1-GetMetadataRequest)
    - [GetMetadataResponse](#metaxisdata-v1-GetMetadataResponse)
    - [GetSchemaStringRequest](#metaxisdata-v1-GetSchemaStringRequest)
    - [GridLevel](#metaxisdata-v1-GridLevel)
    - [IndexMetadata](#metaxisdata-v1-IndexMetadata)
    - [InstanceRoleMetadata](#metaxisdata-v1-InstanceRoleMetadata)
    - [LinkedDatabaseMetadata](#metaxisdata-v1-LinkedDatabaseMetadata)
    - [ListDatabaseRequest](#metaxisdata-v1-ListDatabaseRequest)
    - [ListDatabasesResponse](#metaxisdata-v1-ListDatabasesResponse)
    - [ListMetadataRequest](#metaxisdata-v1-ListMetadataRequest)
    - [MaterializedViewMetadata](#metaxisdata-v1-MaterializedViewMetadata)
    - [MetadataResponse](#metaxisdata-v1-MetadataResponse)
    - [MetadataResponse.MetadataList](#metaxisdata-v1-MetadataResponse-MetadataList)
    - [MetadataSchemaString](#metaxisdata-v1-MetadataSchemaString)
    - [PackageMetadata](#metaxisdata-v1-PackageMetadata)
    - [ProcedureMetadata](#metaxisdata-v1-ProcedureMetadata)
    - [RuleMetadata](#metaxisdata-v1-RuleMetadata)
    - [SchemaMetadata](#metaxisdata-v1-SchemaMetadata)
    - [SequenceMetadata](#metaxisdata-v1-SequenceMetadata)
    - [SpatialIndexConfig](#metaxisdata-v1-SpatialIndexConfig)
    - [SpatialIndexConfig.EngineSpecificEntry](#metaxisdata-v1-SpatialIndexConfig-EngineSpecificEntry)
    - [StorageConfig](#metaxisdata-v1-StorageConfig)
    - [StoredMetadata](#metaxisdata-v1-StoredMetadata)
    - [StreamMetadata](#metaxisdata-v1-StreamMetadata)
    - [TableMetadata](#metaxisdata-v1-TableMetadata)
    - [TablePartitionMetadata](#metaxisdata-v1-TablePartitionMetadata)
    - [TaskMetadata](#metaxisdata-v1-TaskMetadata)
    - [TessellationConfig](#metaxisdata-v1-TessellationConfig)
    - [TriggerMetadata](#metaxisdata-v1-TriggerMetadata)
    - [ViewMetadata](#metaxisdata-v1-ViewMetadata)
  
    - [ColumnMetadata.IdentityGeneration](#metaxisdata-v1-ColumnMetadata-IdentityGeneration)
    - [GenerationMetadata.Type](#metaxisdata-v1-GenerationMetadata-Type)
    - [MetaType](#metaxisdata-v1-MetaType)
    - [StreamMetadata.Mode](#metaxisdata-v1-StreamMetadata-Mode)
    - [StreamMetadata.Type](#metaxisdata-v1-StreamMetadata-Type)
    - [TablePartitionMetadata.Type](#metaxisdata-v1-TablePartitionMetadata-Type)
    - [TaskMetadata.State](#metaxisdata-v1-TaskMetadata-State)
  
    - [DatabaseService](#metaxisdata-v1-DatabaseService)
  
- [v1/lineage_service.proto](#v1_lineage_service-proto)
    - [GetLineageForContextRequest](#metaxisdata-v1-GetLineageForContextRequest)
    - [GetLineageForContextResponse](#metaxisdata-v1-GetLineageForContextResponse)
    - [GetLineageRequest](#metaxisdata-v1-GetLineageRequest)
    - [GetLineageResponse](#metaxisdata-v1-GetLineageResponse)
    - [LineageRelation](#metaxisdata-v1-LineageRelation)
  
    - [LineageType](#metaxisdata-v1-LineageType)
    - [RelationType](#metaxisdata-v1-RelationType)
  
    - [LineageService](#metaxisdata-v1-LineageService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="v1_annotation-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/annotation.proto


 


<a name="metaxisdata-v1-AuthMethod"></a>

### AuthMethod


| Name | Number | Description |
| ---- | ------ | ----------- |
| AUTH_METHOD_UNSPECIFIED | 0 |  |
| IAM | 1 | IAM uses the standard IAM authorization check on the organizational resources. |
| CUSTOM | 2 | Custom authorization method. |


 


<a name="v1_annotation-proto-extensions"></a>

### File-level Extensions
| Extension | Type | Base | Number | Description |
| --------- | ---- | ---- | ------ | ----------- |
| allow_without_credential | bool | .google.protobuf.MethodOptions | 100000 |  |
| audit | bool | .google.protobuf.MethodOptions | 100003 |  |
| auth_method | AuthMethod | .google.protobuf.MethodOptions | 100002 |  |
| permission | string | .google.protobuf.MethodOptions | 100001 |  |

 

 



<a name="v1_common-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/common.proto



<a name="metaxisdata-v1-Position"></a>

### Position
Position in a text expressed as zero-based line and zero-based column byte
offset.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| line | [int32](#int32) |  | Line position in a text (zero-based). |
| column | [int32](#int32) |  | Column position in a text (zero-based), equivalent to byte offset. |






<a name="metaxisdata-v1-Range"></a>

### Range



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| start | [int32](#int32) |  |  |
| end | [int32](#int32) |  |  |





 


<a name="metaxisdata-v1-Engine"></a>

### Engine


| Name | Number | Description |
| ---- | ------ | ----------- |
| ENGINE_UNSPECIFIED | 0 |  |
| CLICKHOUSE | 1 |  |
| MYSQL | 2 |  |
| POSTGRES | 3 |  |
| SNOWFLAKE | 4 |  |
| SQLITE | 5 |  |
| TIDB | 6 |  |
| MONGODB | 7 |  |
| REDIS | 8 |  |
| ORACLE | 9 |  |
| SPANNER | 10 |  |
| MSSQL | 11 |  |
| REDSHIFT | 12 |  |
| MARIADB | 13 |  |
| OCEANBASE | 14 |  |
| STARROCKS | 18 |  |
| DORIS | 19 |  |
| HIVE | 20 |  |
| ELASTICSEARCH | 21 |  |
| BIGQUERY | 22 |  |
| DYNAMODB | 23 |  |
| DATABRICKS | 24 |  |
| COCKROACHDB | 25 |  |
| COSMOSDB | 26 |  |
| TRINO | 27 |  |
| CASSANDRA | 28 |  |



<a name="metaxisdata-v1-RiskLevel"></a>

### RiskLevel
RiskLevel is the risk level.

| Name | Number | Description |
| ---- | ------ | ----------- |
| RISK_LEVEL_UNSPECIFIED | 0 |  |
| LOW | 1 |  |
| MODERATE | 2 |  |
| HIGH | 3 |  |



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

Supported filter: - name: the user name, support &#34;==&#34; and &#34;.matches()&#34; operator. - email: the user email, support &#34;==&#34; and &#34;.matches()&#34; operator. - user_type: the type, check UserType enum for values, support &#34;==&#34;, &#34;in [xx]&#34;, &#34;!(in [xx])&#34; operator. - state: check State enum for values, support &#34;==&#34; operator. - project: the project full name in &#34;projects/{id}&#34; format, support &#34;==&#34; operator.

For example: name == &#34;ed&#34; name.matches(&#34;ed&#34;) email == &#34;ed@example.com&#34; email.matches(&#34;ed&#34;) user_type == &#34;SERVICE_ACCOUNT&#34; user_type in [&#34;SERVICE_ACCOUNT&#34;, &#34;USER&#34;] !(user_type in [&#34;SERVICE_ACCOUNT&#34;, &#34;USER&#34;]) state == &#34;DELETED&#34; project == &#34;projects/sample-project&#34; You can combine filter conditions like: name.matches(&#34;ed&#34;) &amp;&amp; project == &#34;projects/sample-project&#34; (name == &#34;ed&#34; || email == &#34;ed@example.com&#34;) &amp;&amp; project == &#34;projects/sample-project&#34; |






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
| allow_missing | [bool](#bool) |  | If set to true, and the user is not found, a new user will be created. In this situation, `update_mask` is ignored. |






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
| recovery_codes | [string](#string) | repeated | The recovery_codes is the temporary recovery codes using in two phase verification. |
| phone | [string](#string) |  | Should be a valid E.164 compliant phone number. Could be empty. |
| profile | [UserProfile](#metaxisdata-v1-UserProfile) |  |  |
| groups | [string](#string) | repeated | The groups for the user. Format: groups/{email} |






<a name="metaxisdata-v1-UserProfile"></a>

### UserProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| last_login_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_change_password_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| source | [string](#string) |  | source means where the user comes from. For now we support Entra ID SCIM sync, so the source could be Entra ID. |





 


<a name="metaxisdata-v1-UserType"></a>

### UserType


| Name | Number | Description |
| ---- | ------ | ----------- |
| USER_TYPE_UNSPECIFIED | 0 |  |
| USER | 1 |  |
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
| token | [string](#string) |  |  |
| require_reset_password | [bool](#bool) |  |  |
| user | [User](#metaxisdata-v1-User) |  | The user of successful login. |






<a name="metaxisdata-v1-LogoutRequest"></a>

### LogoutRequest







<a name="metaxisdata-v1-OAuth2IdentityProviderContext"></a>

### OAuth2IdentityProviderContext



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  |  |





 

 

 


<a name="metaxisdata-v1-AuthService"></a>

### AuthService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Login | [LoginRequest](#metaxisdata-v1-LoginRequest) | [LoginResponse](#metaxisdata-v1-LoginResponse) | Permissions required: None |
| Logout | [LogoutRequest](#metaxisdata-v1-LogoutRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Permissions required: None |

 



<a name="v1_instance_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/instance_service.proto



<a name="metaxisdata-v1-AddDataSourceRequest"></a>

### AddDataSourceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance to add a data source to. Format: instances/{instance} |
| data_source | [DataSource](#metaxisdata-v1-DataSource) |  | Identified by data source ID. Only READ_ONLY data source can be added. |
| validate_only | [bool](#bool) |  | Validate only also tests the data source connection. |






<a name="metaxisdata-v1-BatchSyncInstancesRequest"></a>

### BatchSyncInstancesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| requests | [SyncInstanceRequest](#metaxisdata-v1-SyncInstanceRequest) | repeated | The request message specifying the instances to sync. A maximum of 1000 instances can be synced in a batch. |






<a name="metaxisdata-v1-BatchSyncInstancesResponse"></a>

### BatchSyncInstancesResponse







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






<a name="metaxisdata-v1-CreateInstanceRequest"></a>

### CreateInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [Instance](#metaxisdata-v1-Instance) |  | The instance to create. |
| instance_id | [string](#string) |  | The ID to use for the instance, which will become the final component of the instance&#39;s resource name.

This value should be 4-63 characters, and valid characters are /[a-z][0-9]-/. |
| validate_only | [bool](#bool) |  | Validate only also tests the data source connection. |






<a name="metaxisdata-v1-DataSource"></a>

### DataSource



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
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
| srv | [bool](#bool) |  | srv, authentication_database and replica_set are used for MongoDB. srv is a boolean flag that indicates whether the host is a DNS SRV record. |
| authentication_database | [string](#string) |  | authentication_database is the database name to authenticate against, which stores the user credentials. |
| replica_set | [string](#string) |  | replica_set is used for MongoDB replica set. |
| sid | [string](#string) |  | sid and service_name are used for Oracle. |
| service_name | [string](#string) |  |  |
| ssh_host | [string](#string) |  | Connection over SSH. The hostname of the SSH server agent. Required. |
| ssh_port | [string](#string) |  | The port of the SSH server agent. It&#39;s 22 typically. Required. |
| ssh_user | [string](#string) |  | The user to login the server. Required. |
| ssh_password | [string](#string) |  | The password to login the server. If it&#39;s empty string, no password is required. |
| ssh_private_key | [string](#string) |  | The private key to login the server. If it&#39;s empty string, we will use the system default private key from os.Getenv(&#34;SSH_AUTH_SOCK&#34;). |
| authentication_private_key | [string](#string) |  | PKCS#8 private key in PEM format. If it&#39;s empty string, no private key is required. Used for authentication when connecting to the data source. |
| external_secret | [DataSourceExternalSecret](#metaxisdata-v1-DataSourceExternalSecret) |  |  |
| authentication_type | [DataSource.AuthenticationType](#metaxisdata-v1-DataSource-AuthenticationType) |  |  |
| azure_credential | [DataSource.AzureCredential](#metaxisdata-v1-DataSource-AzureCredential) |  |  |
| aws_credential | [DataSource.AWSCredential](#metaxisdata-v1-DataSource-AWSCredential) |  |  |
| gcp_credential | [DataSource.GCPCredential](#metaxisdata-v1-DataSource-GCPCredential) |  |  |
| sasl_config | [SASLConfig](#metaxisdata-v1-SASLConfig) |  |  |
| additional_addresses | [DataSource.Address](#metaxisdata-v1-DataSource-Address) | repeated | additional_addresses is used for MongoDB replica set. |
| direct_connection | [bool](#bool) |  | direct_connection is used for MongoDB to dispatch all the operations to the node specified in the connection string. |
| region | [string](#string) |  | region is the location of where the DB is, works for AWS RDS. For example, us-east-1. |
| warehouse_id | [string](#string) |  | warehouse_id is used by Databricks. |
| master_name | [string](#string) |  | master_name is the master name used by connecting redis-master via redis sentinel. |
| master_username | [string](#string) |  | master_username and master_password are master credentials used by redis sentinel mode. |
| master_password | [string](#string) |  |  |
| redis_type | [DataSource.RedisType](#metaxisdata-v1-DataSource-RedisType) |  |  |
| cluster | [string](#string) |  | Cluster is the cluster name for the data source. Used by CockroachDB. |
| extra_connection_parameters | [DataSource.ExtraConnectionParametersEntry](#metaxisdata-v1-DataSource-ExtraConnectionParametersEntry) | repeated | Extra connection parameters for the database connection. For PostgreSQL HA, this can be used to set target_session_attrs=read-write |






<a name="metaxisdata-v1-DataSource-AWSCredential"></a>

### DataSource.AWSCredential



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| access_key_id | [string](#string) |  |  |
| secret_access_key | [string](#string) |  |  |
| session_token | [string](#string) |  |  |






<a name="metaxisdata-v1-DataSource-Address"></a>

### DataSource.Address



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| host | [string](#string) |  |  |
| port | [string](#string) |  |  |






<a name="metaxisdata-v1-DataSource-AzureCredential"></a>

### DataSource.AzureCredential



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| client_secret | [string](#string) |  |  |






<a name="metaxisdata-v1-DataSource-ExtraConnectionParametersEntry"></a>

### DataSource.ExtraConnectionParametersEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-DataSource-GCPCredential"></a>

### DataSource.GCPCredential



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [string](#string) |  |  |






<a name="metaxisdata-v1-DataSourceExternalSecret"></a>

### DataSourceExternalSecret



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| secret_type | [DataSourceExternalSecret.SecretType](#metaxisdata-v1-DataSourceExternalSecret-SecretType) |  |  |
| url | [string](#string) |  |  |
| auth_type | [DataSourceExternalSecret.AuthType](#metaxisdata-v1-DataSourceExternalSecret-AuthType) |  |  |
| app_role | [DataSourceExternalSecret.AppRoleAuthOption](#metaxisdata-v1-DataSourceExternalSecret-AppRoleAuthOption) |  |  |
| token | [string](#string) |  |  |
| engine_name | [string](#string) |  | engine name is the name for secret engine. |
| secret_name | [string](#string) |  | the secret name in the engine to store the password. |
| password_key_name | [string](#string) |  | the key name for the password. |






<a name="metaxisdata-v1-DataSourceExternalSecret-AppRoleAuthOption"></a>

### DataSourceExternalSecret.AppRoleAuthOption



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role_id | [string](#string) |  |  |
| secret_id | [string](#string) |  | the secret id for the role without ttl. |
| type | [DataSourceExternalSecret.AppRoleAuthOption.SecretType](#metaxisdata-v1-DataSourceExternalSecret-AppRoleAuthOption-SecretType) |  |  |
| mount_path | [string](#string) |  | The path where the approle auth method is mounted. |






<a name="metaxisdata-v1-DeleteInstanceRequest"></a>

### DeleteInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance to delete. Format: instances/{instance} |
| force | [bool](#bool) |  | If set to true, any databases and sheets from this project will also be moved to default project, and all open issues will be closed. |






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
| data_sources | [DataSource](#metaxisdata-v1-DataSource) | repeated |  |
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






<a name="metaxisdata-v1-KerberosConfig"></a>

### KerberosConfig



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| primary | [string](#string) |  |  |
| instance | [string](#string) |  |  |
| realm | [string](#string) |  |  |
| keytab | [bytes](#bytes) |  |  |
| kdc_host | [string](#string) |  |  |
| kdc_port | [string](#string) |  |  |
| kdc_transport_protocol | [string](#string) |  |  |






<a name="metaxisdata-v1-ListInstanceDatabaseRequest"></a>

### ListInstanceDatabaseRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance. Format: instances/{instance} |
| instance | [Instance](#metaxisdata-v1-Instance) | optional | The target instance. We need to set this field if the target instance is not created yet. |






<a name="metaxisdata-v1-ListInstanceDatabaseResponse"></a>

### ListInstanceDatabaseResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| databases | [string](#string) | repeated | All database name list in the instance. |






<a name="metaxisdata-v1-ListInstancesRequest"></a>

### ListInstancesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of instances to return. The service may return fewer than this value. If unspecified, at most 10 instances will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListInstances` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListInstances` must match the call that provided the page token. |
| show_deleted | [bool](#bool) |  | Show deleted instances if specified. |
| filter | [string](#string) |  | Filter the instance. The syntax and semantics of CEL are documented at https://github.com/google/cel-spec

Supported filters: - name: the instance name, support &#34;==&#34; and &#34;.matches()&#34; operator. - resource_id: the instance id, support &#34;==&#34; and &#34;.matches()&#34; operator. - environment: the environment full name in &#34;environments/{id}&#34; format, support &#34;==&#34; operator. - state: the instance state, check State enum for values, support &#34;==&#34; operator. - engine: the instance engine, check Engine enum for values. Support &#34;==&#34;, &#34;in [xx]&#34;, &#34;!(in [xx])&#34; operator. - host: the instance host, support &#34;==&#34; and &#34;.matches()&#34; operator. - port: the instance port, support &#34;==&#34; and &#34;.matches()&#34; operator. - project: the project full name in &#34;projects/{id}&#34; format, support &#34;==&#34; operator.

For example: name == &#34;sample instance&#34; name.matches(&#34;sample&#34;) resource_id = &#34;sample-instance&#34; resource_id.matches(&#34;sample&#34;) state == &#34;DELETED&#34; environment == &#34;environments/test&#34; engine == &#34;MYSQL&#34; engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;] !(engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;]) host == &#34;127.0.0.1&#34; host.matches(&#34;127.0&#34;) port == &#34;54321&#34; port.matches(&#34;543&#34;) project == &#34;projects/sample-project&#34; You can combine filter conditions like: name.matches(&#34;sample&#34;) &amp;&amp; environment == &#34;environments/test&#34; host == &#34;127.0.0.1&#34; &amp;&amp; port == &#34;54321&#34; |






<a name="metaxisdata-v1-ListInstancesResponse"></a>

### ListInstancesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instances | [Instance](#metaxisdata-v1-Instance) | repeated | The instances from the specified request. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-RemoveDataSourceRequest"></a>

### RemoveDataSourceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance to remove a data source from. Format: instances/{instance} |
| data_source | [DataSource](#metaxisdata-v1-DataSource) |  | Identified by data source ID. Only READ_ONLY data source can be removed. |






<a name="metaxisdata-v1-SASLConfig"></a>

### SASLConfig



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| krb_config | [KerberosConfig](#metaxisdata-v1-KerberosConfig) |  |  |






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
| name | [string](#string) |  | The name of the instance to update a data source. Format: instances/{instance} |
| data_source | [DataSource](#metaxisdata-v1-DataSource) |  | Identified by data source ID. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. |
| validate_only | [bool](#bool) |  | Validate only also tests the data source connection. |






<a name="metaxisdata-v1-UpdateInstanceRequest"></a>

### UpdateInstanceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [Instance](#metaxisdata-v1-Instance) |  | The instance to update.

The instance&#39;s `name` field is used to identify the instance to update. Format: instances/{instance} |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. |





 


<a name="metaxisdata-v1-DataSource-AuthenticationType"></a>

### DataSource.AuthenticationType


| Name | Number | Description |
| ---- | ------ | ----------- |
| AUTHENTICATION_UNSPECIFIED | 0 |  |
| PASSWORD | 1 |  |
| GOOGLE_CLOUD_SQL_IAM | 2 |  |
| AWS_RDS_IAM | 3 |  |
| AZURE_IAM | 4 |  |



<a name="metaxisdata-v1-DataSource-RedisType"></a>

### DataSource.RedisType


| Name | Number | Description |
| ---- | ------ | ----------- |
| REDIS_TYPE_UNSPECIFIED | 0 |  |
| STANDALONE | 1 |  |
| SENTINEL | 2 |  |
| CLUSTER | 3 |  |



<a name="metaxisdata-v1-DataSourceExternalSecret-AppRoleAuthOption-SecretType"></a>

### DataSourceExternalSecret.AppRoleAuthOption.SecretType


| Name | Number | Description |
| ---- | ------ | ----------- |
| SECRET_TYPE_UNSPECIFIED | 0 |  |
| PLAIN | 1 |  |
| ENVIRONMENT | 2 |  |



<a name="metaxisdata-v1-DataSourceExternalSecret-AuthType"></a>

### DataSourceExternalSecret.AuthType


| Name | Number | Description |
| ---- | ------ | ----------- |
| AUTH_TYPE_UNSPECIFIED | 0 |  |
| TOKEN | 1 | ref: https://developer.hashicorp.com/vault/docs/auth/token |
| VAULT_APP_ROLE | 2 | ref: https://developer.hashicorp.com/vault/docs/auth/approle |



<a name="metaxisdata-v1-DataSourceExternalSecret-SecretType"></a>

### DataSourceExternalSecret.SecretType


| Name | Number | Description |
| ---- | ------ | ----------- |
| SAECRET_TYPE_UNSPECIFIED | 0 |  |
| VAULT_KV_V2 | 1 | ref: https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2 |
| AWS_SECRETS_MANAGER | 2 | ref: https://docs.aws.amazon.com/secretsmanager/latest/userguide/intro.html |
| GCP_SECRET_MANAGER | 3 | ref: https://cloud.google.com/secret-manager/docs |



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
| ListInstanceDatabase | [ListInstanceDatabaseRequest](#metaxisdata-v1-ListInstanceDatabaseRequest) | [ListInstanceDatabaseResponse](#metaxisdata-v1-ListInstanceDatabaseResponse) |  |
| BatchSyncInstances | [BatchSyncInstancesRequest](#metaxisdata-v1-BatchSyncInstancesRequest) | [BatchSyncInstancesResponse](#metaxisdata-v1-BatchSyncInstancesResponse) |  |
| BatchUpdateInstances | [BatchUpdateInstancesRequest](#metaxisdata-v1-BatchUpdateInstancesRequest) | [BatchUpdateInstancesResponse](#metaxisdata-v1-BatchUpdateInstancesResponse) |  |
| AddDataSource | [AddDataSourceRequest](#metaxisdata-v1-AddDataSourceRequest) | [Instance](#metaxisdata-v1-Instance) |  |
| RemoveDataSource | [RemoveDataSourceRequest](#metaxisdata-v1-RemoveDataSourceRequest) | [Instance](#metaxisdata-v1-Instance) |  |
| UpdateDataSource | [UpdateDataSourceRequest](#metaxisdata-v1-UpdateDataSourceRequest) | [Instance](#metaxisdata-v1-Instance) |  |

 



<a name="v1_database_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/database_service.proto



<a name="metaxisdata-v1-BoundingBox"></a>

### BoundingBox
BoundingBox defines the bounding box for spatial indexes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| xmin | [double](#double) |  |  |
| ymin | [double](#double) |  |  |
| xmax | [double](#double) |  |  |
| ymax | [double](#double) |  |  |






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






<a name="metaxisdata-v1-Database"></a>

### Database



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the database. Format: instances/{instance}/databases/{database} {database} is the database name in the instance. |
| state | [State](#metaxisdata-v1-State) |  | The existence of a database. |
| successful_sync_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | The latest synchronization time. |
| project | [string](#string) |  | The project for a database. Format: projects/{project} |
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
| service_name | [string](#string) |  | The service name of the database. It&#39;s an Oracle-specific concept. |
| linked_databases | [LinkedDatabaseMetadata](#metaxisdata-v1-LinkedDatabaseMetadata) | repeated |  |
| owner | [string](#string) |  |  |
| search_path | [string](#string) |  | The search_path is the search path of a PostgreSQL database. |
| event_triggers | [EventTriggerMetadata](#metaxisdata-v1-EventTriggerMetadata) | repeated | The list of event triggers in a database (PostgreSQL specific). Event triggers are database-level objects, not schema-scoped. |






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






<a name="metaxisdata-v1-DimensionalConfig"></a>

### DimensionalConfig
DimensionalConfig defines dimensional and constraint parameters for spatial indexes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| dimensions | [int32](#int32) |  | Number of dimensions (2-4, default 2) |
| data_type | [string](#string) |  | Spatial data type Examples: GEOMETRY, GEOGRAPHY, POINT, POLYGON, etc. |
| operator_class | [string](#string) |  | PostgreSQL operator class Examples: gist_geometry_ops_2d, gist_geometry_ops_nd, etc. |
| layer_gtype | [string](#string) |  | Oracle geometry type constraint Examples: POINT, LINE, POLYGON, COLLECTION |
| parallel_build | [bool](#bool) |  | Parallel index creation |






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






<a name="metaxisdata-v1-GetDatabaseRequest"></a>

### GetDatabaseRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the database to retrieve. Format: instances/{instance}/databases/{database} |






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






<a name="metaxisdata-v1-GridLevel"></a>

### GridLevel
GridLevel defines a grid level for spatial tessellation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [int32](#int32) |  | 1-4 for SQL Server |
| density | [string](#string) |  | LOW, MEDIUM, HIGH |






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
| granularity | [int64](#int64) |  | The number of granules in the block. It&#39;s a ClickHouse specific field. |
| is_constraint | [bool](#bool) |  | It&#39;s a PostgreSQL specific field. The unique constraint and unique index are not the same thing in PostgreSQL. |
| spatial_config | [SpatialIndexConfig](#metaxisdata-v1-SpatialIndexConfig) |  | Spatial index specific configuration |
| opclass_names | [string](#string) | repeated | https://www.postgresql.org/docs/current/catalog-pg-opclass.html Name of the operator class for each column. (PostgreSQL specific). |
| opclass_defaults | [bool](#bool) | repeated | True if the operator class is the default. (PostgreSQL specific). |






<a name="metaxisdata-v1-InstanceRoleMetadata"></a>

### InstanceRoleMetadata
InstanceRoleMetadata is the message for instance role.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The role name. It&#39;s unique within the instance. |
| grant | [string](#string) |  | The grant display string on the instance. It&#39;s generated by database engine. |






<a name="metaxisdata-v1-LinkedDatabaseMetadata"></a>

### LinkedDatabaseMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| username | [string](#string) |  |  |
| host | [string](#string) |  |  |






<a name="metaxisdata-v1-ListDatabaseRequest"></a>

### ListDatabaseRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | - projects/{project}: list databases in a project. - workspaces/-: list databases in the workspace. - instances/{instances}: list databases in a instance. |
| page_size | [int32](#int32) |  | The maximum number of databases to return. The service may return fewer than this value. If unspecified, at most 10 databases will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListDatabases` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListDatabases` must match the call that provided the page token. |
| filter | [string](#string) |  | Filter is used to filter databases returned in the list. The syntax and semantics of CEL are documented at https://github.com/google/cel-spec

Supported filter: - environment: the environment full name in &#34;environments/{id}&#34; format, support &#34;==&#34; operator. - name: the database name, support &#34;.matches()&#34; operator. - project: the project full name in &#34;projects/{id}&#34; format, support &#34;==&#34; operator. - instance: the instance full name in &#34;instances/{id}&#34; format, support &#34;==&#34; operator. - engine: the database engine, check Engine enum for values. Support &#34;==&#34;, &#34;in [xx]&#34;, &#34;!(in [xx])&#34; operator. - exclude_unassigned: should be &#34;true&#34; or &#34;false&#34;, will not show unassigned databases if it&#39;s true, support &#34;==&#34; operator. - drifted: should be &#34;true&#34; or &#34;false&#34;, show drifted databases if it&#39;s true, support &#34;==&#34; operator. - table: filter by the database table, support &#34;==&#34; and &#34;.matches()&#34; operator. - labels.{key}: the database label, support &#34;==&#34; and &#34;in&#34; operators.

For example: environment == &#34;environments/{environment resource id}&#34; environment == &#34;&#34; (find databases which environment is not set) project == &#34;projects/{project resource id}&#34; instance == &#34;instances/{instance resource id}&#34; name.matches(&#34;database name&#34;) engine == &#34;MYSQL&#34; engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;] !(engine in [&#34;MYSQL&#34;, &#34;POSTGRES&#34;]) exclude_unassigned == true drifted == true table == &#34;sample&#34; table.matches(&#34;sam&#34;) labels.environment == &#34;production&#34; labels.region == &#34;asia&#34; labels.region in [&#34;asia&#34;, &#34;europe&#34;]

You can combine filter conditions like: environment == &#34;environments/prod&#34; &amp;&amp; name.matches(&#34;employee&#34;) |
| show_deleted | [bool](#bool) |  | Show deleted database if specified. |






<a name="metaxisdata-v1-ListDatabasesResponse"></a>

### ListDatabasesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| databases | [Database](#metaxisdata-v1-Database) | repeated | All database name list. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-ListMetadataRequest"></a>

### ListMetadataRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent_guid | [string](#string) |  | The global unique id for all metadata database: &#34;instance_100;database3&#34; table: &#34;instance_1;db2;schema3;table4&#34; |
| page_size | [int32](#int32) |  | The maximum number of databases to return. The service may return fewer than this value. If unspecified, at most 10 databases will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListDatabases` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListDatabases` must match the call that provided the page token. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) | optional | the type of metadata If meta_type is not specified, the query will ignore page_size and return the first 20 records of each meta_type. |






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






<a name="metaxisdata-v1-MetadataResponse"></a>

### MetadataResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| types_stored_metadata | [MetadataResponse.MetadataList](#metaxisdata-v1-MetadataResponse-MetadataList) | repeated | The list of stored metadata. |






<a name="metaxisdata-v1-MetadataResponse-MetadataList"></a>

### MetadataResponse.MetadataList



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






<a name="metaxisdata-v1-PackageMetadata"></a>

### PackageMetadata
PackageMetadata is the metadata for packages.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the package. |
| definition | [string](#string) |  | The definition is the definition of a package. |






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
| streams | [StreamMetadata](#metaxisdata-v1-StreamMetadata) | repeated | The list of streams in a schema, currently only used for Snowflake. |
| tasks | [TaskMetadata](#metaxisdata-v1-TaskMetadata) | repeated | The list of tasks in a schema, currently only used for Snowflake. |
| materialized_views | [MaterializedViewMetadata](#metaxisdata-v1-MaterializedViewMetadata) | repeated | The list of materialized views in a schema. |
| sequences | [SequenceMetadata](#metaxisdata-v1-SequenceMetadata) | repeated | The list of sequences in a schema. |
| packages | [PackageMetadata](#metaxisdata-v1-PackageMetadata) | repeated | The list of packages in a schema. |
| owner | [string](#string) |  |  |
| comment | [string](#string) |  |  |
| events | [EventMetadata](#metaxisdata-v1-EventMetadata) | repeated |  |
| enum_types | [EnumTypeMetadata](#metaxisdata-v1-EnumTypeMetadata) | repeated |  |
| skip_dump | [bool](#bool) |  |  |






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






<a name="metaxisdata-v1-SpatialIndexConfig"></a>

### SpatialIndexConfig
SpatialIndexConfig is the configuration for spatial indexes across different database engines.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| method | [string](#string) |  | Index method/type (database-specific) Examples: &#34;SPATIAL&#34; (MySQL/SQL Server), &#34;GIST&#34;/&#34;SPGIST&#34; (PostgreSQL), &#34;MDSYS.SPATIAL_INDEX_V2&#34; (Oracle) |
| tessellation | [TessellationConfig](#metaxisdata-v1-TessellationConfig) |  | Tessellation configuration (primarily SQL Server) |
| storage | [StorageConfig](#metaxisdata-v1-StorageConfig) |  | Storage and performance parameters |
| dimensional | [DimensionalConfig](#metaxisdata-v1-DimensionalConfig) |  | Dimensional and constraint parameters |
| engine_specific | [SpatialIndexConfig.EngineSpecificEntry](#metaxisdata-v1-SpatialIndexConfig-EngineSpecificEntry) | repeated | Database-specific parameters (stored as key-value pairs for extensibility) |






<a name="metaxisdata-v1-SpatialIndexConfig-EngineSpecificEntry"></a>

### SpatialIndexConfig.EngineSpecificEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-StorageConfig"></a>

### StorageConfig
StorageConfig defines storage and performance parameters for spatial indexes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| fillfactor | [int32](#int32) |  | PostgreSQL parameters

10-100 |
| buffering | [string](#string) |  | auto, on, off |
| tablespace | [string](#string) |  | Oracle parameters |
| work_tablespace | [string](#string) |  |  |
| sdo_level | [int32](#int32) |  |  |
| commit_interval | [int32](#int32) |  |  |
| pad_index | [bool](#bool) |  | SQL Server parameters |
| sort_in_tempdb | [string](#string) |  | ON, OFF |
| drop_existing | [bool](#bool) |  |  |
| online | [bool](#bool) |  |  |
| allow_row_locks | [bool](#bool) |  |  |
| allow_page_locks | [bool](#bool) |  |  |
| maxdop | [int32](#int32) |  |  |
| data_compression | [string](#string) |  | NONE, ROW, PAGE |






<a name="metaxisdata-v1-StoredMetadata"></a>

### StoredMetadata



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
| package_metadata | [PackageMetadata](#metaxisdata-v1-PackageMetadata) |  |  |
| sequence_metadata | [SequenceMetadata](#metaxisdata-v1-SequenceMetadata) |  |  |
| stream_metadata | [StreamMetadata](#metaxisdata-v1-StreamMetadata) |  |  |
| task_metadata | [TaskMetadata](#metaxisdata-v1-TaskMetadata) |  |  |






<a name="metaxisdata-v1-StreamMetadata"></a>

### StreamMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the stream. |
| table_name | [string](#string) |  | The table_name is the name of the table/view that the stream is created on. |
| owner | [string](#string) |  | The owner of the stream. |
| comment | [string](#string) |  | The comment of the stream. |
| type | [StreamMetadata.Type](#metaxisdata-v1-StreamMetadata-Type) |  | The type of the stream. |
| stale | [bool](#bool) |  | Indicates whether the stream was last read before the `stale_after` time. |
| mode | [StreamMetadata.Mode](#metaxisdata-v1-StreamMetadata-Mode) |  | The mode of the stream. |
| definition | [string](#string) |  | The definition of the stream. |






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






<a name="metaxisdata-v1-TaskMetadata"></a>

### TaskMetadata



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the task. |
| id | [string](#string) |  | The Snowflake-generated ID of the task. Example: 01ad32a0-1bb6-5e93-0000-000000000001. |
| owner | [string](#string) |  | The owner of the task. |
| comment | [string](#string) |  | The comment of the task. |
| warehouse | [string](#string) |  | The warehouse of the task. |
| schedule | [string](#string) |  | The schedule interval of the task. |
| predecessors | [string](#string) | repeated | The predecessor tasks of the task. |
| state | [TaskMetadata.State](#metaxisdata-v1-TaskMetadata-State) |  | The state of the task. |
| condition | [string](#string) |  | The condition of the task. |
| definition | [string](#string) |  | The definition of the task. |






<a name="metaxisdata-v1-TessellationConfig"></a>

### TessellationConfig
TessellationConfig defines tessellation parameters for spatial indexes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scheme | [string](#string) |  | Tessellation scheme Examples: GEOMETRY_GRID, GEOGRAPHY_GRID, GEOMETRY_AUTO_GRID, GEOGRAPHY_AUTO_GRID |
| bounding_box | [BoundingBox](#metaxisdata-v1-BoundingBox) |  | Bounding box for GEOMETRY indexes (SQL Server) |
| grid_levels | [GridLevel](#metaxisdata-v1-GridLevel) | repeated | Grid level configuration (SQL Server) |
| cells_per_object | [int32](#int32) |  | Cells per object (SQL Server) |






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
| VIEW | 5 |  |
| MATERIALIZED_VIEW | 6 |  |
| COLUMN | 7 |  |
| INDEX | 8 |  |
| FOREIGN_KEY | 9 |  |
| PROCEDURE | 10 |  |
| FUNCTION | 11 |  |
| SEQUENCE | 12 |  |
| PACKAGE | 13 |  |
| STREAM | 14 |  |
| TASK | 15 |  |



<a name="metaxisdata-v1-StreamMetadata-Mode"></a>

### StreamMetadata.Mode


| Name | Number | Description |
| ---- | ------ | ----------- |
| MODE_UNSPECIFIED | 0 |  |
| MODE_DEFAULT | 1 |  |
| MODE_APPEND_ONLY | 2 |  |
| MODE_INSERT_ONLY | 3 |  |



<a name="metaxisdata-v1-StreamMetadata-Type"></a>

### StreamMetadata.Type


| Name | Number | Description |
| ---- | ------ | ----------- |
| TYPE_UNSPECIFIED | 0 |  |
| TYPE_DELTA | 1 |  |



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



<a name="metaxisdata-v1-TaskMetadata-State"></a>

### TaskMetadata.State


| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNSPECIFIED | 0 |  |
| STATE_STARTED | 1 |  |
| STATE_SUSPENDED | 2 |  |


 

 


<a name="metaxisdata-v1-DatabaseService"></a>

### DatabaseService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetDatabase | [GetDatabaseRequest](#metaxisdata-v1-GetDatabaseRequest) | [Database](#metaxisdata-v1-Database) |  |
| ListDatabase | [ListDatabaseRequest](#metaxisdata-v1-ListDatabaseRequest) | [ListDatabasesResponse](#metaxisdata-v1-ListDatabasesResponse) |  |
| ListMetadata | [ListMetadataRequest](#metaxisdata-v1-ListMetadataRequest) | [MetadataResponse](#metaxisdata-v1-MetadataResponse) |  |
| GetMetadata | [GetMetadataRequest](#metaxisdata-v1-GetMetadataRequest) | [GetMetadataResponse](#metaxisdata-v1-GetMetadataResponse) |  |
| GetSchemaString | [GetSchemaStringRequest](#metaxisdata-v1-GetSchemaStringRequest) | [MetadataSchemaString](#metaxisdata-v1-MetadataSchemaString) | Generates schema DDL for a database object. |

 



<a name="v1_lineage_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/lineage_service.proto



<a name="metaxisdata-v1-GetLineageForContextRequest"></a>

### GetLineageForContextRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The global unique id for metadata view: &#34;instance_1;db2;schema3;view1&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |






<a name="metaxisdata-v1-GetLineageForContextResponse"></a>

### GetLineageForContextResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| relations | [LineageRelation](#metaxisdata-v1-LineageRelation) | repeated | The list of lineage relations for the given metadata. |






<a name="metaxisdata-v1-GetLineageRequest"></a>

### GetLineageRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The global unique id for metadata table: &#34;instance_1;db2;schema3;table4&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| lineage_type | [LineageType](#metaxisdata-v1-LineageType) |  | The lineage type to query, source or target. If not specified, both source and target lineage will be returned. |






<a name="metaxisdata-v1-GetLineageResponse"></a>

### GetLineageResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| relations | [LineageRelation](#metaxisdata-v1-LineageRelation) | repeated | The list of lineage relations for the given metadata. |






<a name="metaxisdata-v1-LineageRelation"></a>

### LineageRelation



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [int64](#int64) |  |  |
| meta_guid | [string](#string) |  |  |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| source_guid | [string](#string) |  |  |
| source_column | [string](#string) |  |  |
| target_guid | [string](#string) |  |  |
| target_column | [string](#string) |  |  |
| relation_type | [RelationType](#metaxisdata-v1-RelationType) |  |  |
| transformation | [string](#string) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 


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

