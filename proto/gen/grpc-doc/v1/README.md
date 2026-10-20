# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [v1/annotation.proto](#v1_annotation-proto)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
  
- [v1/common.proto](#v1_common-proto)
    - [Engine](#metaxisdata-v1-Engine)
    - [State](#metaxisdata-v1-State)
  
- [v1/application_service.proto](#v1_application_service-proto)
    - [Application](#metaxisdata-v1-Application)
    - [ApplicationAPIKey](#metaxisdata-v1-ApplicationAPIKey)
    - [ApplicationSQL](#metaxisdata-v1-ApplicationSQL)
    - [ApplicationSQLObservation](#metaxisdata-v1-ApplicationSQLObservation)
    - [CreateApplicationAPIKeyRequest](#metaxisdata-v1-CreateApplicationAPIKeyRequest)
    - [CreateApplicationAPIKeyResponse](#metaxisdata-v1-CreateApplicationAPIKeyResponse)
    - [CreateApplicationRequest](#metaxisdata-v1-CreateApplicationRequest)
    - [CreateApplicationResponse](#metaxisdata-v1-CreateApplicationResponse)
    - [DeleteApplicationRequest](#metaxisdata-v1-DeleteApplicationRequest)
    - [DeleteApplicationSQLRequest](#metaxisdata-v1-DeleteApplicationSQLRequest)
    - [GetApplicationSQLRequest](#metaxisdata-v1-GetApplicationSQLRequest)
    - [ListApplicationAPIKeysRequest](#metaxisdata-v1-ListApplicationAPIKeysRequest)
    - [ListApplicationAPIKeysResponse](#metaxisdata-v1-ListApplicationAPIKeysResponse)
    - [ListApplicationSQLsRequest](#metaxisdata-v1-ListApplicationSQLsRequest)
    - [ListApplicationSQLsResponse](#metaxisdata-v1-ListApplicationSQLsResponse)
    - [ListApplicationsRequest](#metaxisdata-v1-ListApplicationsRequest)
    - [ListApplicationsResponse](#metaxisdata-v1-ListApplicationsResponse)
    - [RevokeApplicationAPIKeyRequest](#metaxisdata-v1-RevokeApplicationAPIKeyRequest)
    - [UpdateApplicationRequest](#metaxisdata-v1-UpdateApplicationRequest)
  
    - [ApplicationSQLKind](#metaxisdata-v1-ApplicationSQLKind)
  
    - [ApplicationService](#metaxisdata-v1-ApplicationService)
  
- [v1/audit_log_service.proto](#v1_audit_log_service-proto)
    - [AuditLog](#metaxisdata-v1-AuditLog)
    - [AuditLogStatus](#metaxisdata-v1-AuditLogStatus)
    - [AuditRequestMetadata](#metaxisdata-v1-AuditRequestMetadata)
    - [ListAuditLogsRequest](#metaxisdata-v1-ListAuditLogsRequest)
    - [ListAuditLogsResponse](#metaxisdata-v1-ListAuditLogsResponse)
  
    - [AuditLogSeverity](#metaxisdata-v1-AuditLogSeverity)
  
    - [AuditLogService](#metaxisdata-v1-AuditLogService)
  
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
    - [ApproveDeviceLoginRequest](#metaxisdata-v1-ApproveDeviceLoginRequest)
    - [CreateDeviceLoginRequest](#metaxisdata-v1-CreateDeviceLoginRequest)
    - [CreateDeviceLoginResponse](#metaxisdata-v1-CreateDeviceLoginResponse)
    - [CreateSSOStateResponse](#metaxisdata-v1-CreateSSOStateResponse)
    - [DeviceLogin](#metaxisdata-v1-DeviceLogin)
    - [ExchangeDeviceLoginRequest](#metaxisdata-v1-ExchangeDeviceLoginRequest)
    - [ExchangeDeviceLoginResponse](#metaxisdata-v1-ExchangeDeviceLoginResponse)
    - [GetDeviceLoginRequest](#metaxisdata-v1-GetDeviceLoginRequest)
    - [IdentityProviderContext](#metaxisdata-v1-IdentityProviderContext)
    - [LoginRequest](#metaxisdata-v1-LoginRequest)
    - [LoginResponse](#metaxisdata-v1-LoginResponse)
    - [LogoutRequest](#metaxisdata-v1-LogoutRequest)
    - [OAuth2IdentityProviderContext](#metaxisdata-v1-OAuth2IdentityProviderContext)
    - [RefreshRequest](#metaxisdata-v1-RefreshRequest)
    - [RefreshResponse](#metaxisdata-v1-RefreshResponse)
  
    - [DeviceLoginState](#metaxisdata-v1-DeviceLoginState)
  
    - [AuthService](#metaxisdata-v1-AuthService)
  
- [v1/connected_app_service.proto](#v1_connected_app_service-proto)
    - [ConnectedApp](#metaxisdata-v1-ConnectedApp)
    - [ListConnectedAppsRequest](#metaxisdata-v1-ListConnectedAppsRequest)
    - [ListConnectedAppsResponse](#metaxisdata-v1-ListConnectedAppsResponse)
    - [RevokeConnectedAppRequest](#metaxisdata-v1-RevokeConnectedAppRequest)
  
    - [ConnectedApp.State](#metaxisdata-v1-ConnectedApp-State)
  
    - [ConnectedAppService](#metaxisdata-v1-ConnectedAppService)
  
- [v1/instance_service.proto](#v1_instance_service-proto)
    - [BatchSyncInstanceResult](#metaxisdata-v1-BatchSyncInstanceResult)
    - [BatchSyncInstancesRequest](#metaxisdata-v1-BatchSyncInstancesRequest)
    - [BatchSyncInstancesResponse](#metaxisdata-v1-BatchSyncInstancesResponse)
    - [BatchUpdateInstanceResult](#metaxisdata-v1-BatchUpdateInstanceResult)
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
    - [ApplicationSQLMetadata](#metaxisdata-v1-ApplicationSQLMetadata)
    - [BoundingBox](#metaxisdata-v1-BoundingBox)
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
    - [GetManualSQLRequest](#metaxisdata-v1-GetManualSQLRequest)
    - [GetMetadataHistoryEventRequest](#metaxisdata-v1-GetMetadataHistoryEventRequest)
    - [GetMetadataRequest](#metaxisdata-v1-GetMetadataRequest)
    - [GetMetadataResponse](#metaxisdata-v1-GetMetadataResponse)
    - [GetSchemaStringRequest](#metaxisdata-v1-GetSchemaStringRequest)
    - [GridLevel](#metaxisdata-v1-GridLevel)
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
    - [SpatialIndexConfig](#metaxisdata-v1-SpatialIndexConfig)
    - [SpatialIndexConfig.EngineSpecificEntry](#metaxisdata-v1-SpatialIndexConfig-EngineSpecificEntry)
    - [StorageConfig](#metaxisdata-v1-StorageConfig)
    - [StoredMetadata](#metaxisdata-v1-StoredMetadata)
    - [SyncDatabaseRequest](#metaxisdata-v1-SyncDatabaseRequest)
    - [SyncDatabaseResponse](#metaxisdata-v1-SyncDatabaseResponse)
    - [TableMetadata](#metaxisdata-v1-TableMetadata)
    - [TablePartitionMetadata](#metaxisdata-v1-TablePartitionMetadata)
    - [TessellationConfig](#metaxisdata-v1-TessellationConfig)
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
  
- [v1/data_quality_service.proto](#v1_data_quality_service-proto)
    - [AcceptedValuesConfig](#metaxisdata-v1-AcceptedValuesConfig)
    - [AggregateConfig](#metaxisdata-v1-AggregateConfig)
    - [CelExpressionConfig](#metaxisdata-v1-CelExpressionConfig)
    - [ColumnProfile](#metaxisdata-v1-ColumnProfile)
    - [ColumnProfileStats](#metaxisdata-v1-ColumnProfileStats)
    - [ColumnSemanticLabel](#metaxisdata-v1-ColumnSemanticLabel)
    - [ColumnValue](#metaxisdata-v1-ColumnValue)
    - [CreateQualityRuleRequest](#metaxisdata-v1-CreateQualityRuleRequest)
    - [CrossColumnConfig](#metaxisdata-v1-CrossColumnConfig)
    - [CrossTableConfig](#metaxisdata-v1-CrossTableConfig)
    - [CustomSQLConfig](#metaxisdata-v1-CustomSQLConfig)
    - [DeleteQualityRuleRequest](#metaxisdata-v1-DeleteQualityRuleRequest)
    - [ExplainQualityResultRequest](#metaxisdata-v1-ExplainQualityResultRequest)
    - [ExplainQualityResultResponse](#metaxisdata-v1-ExplainQualityResultResponse)
    - [FailureSample](#metaxisdata-v1-FailureSample)
    - [FreshnessConfig](#metaxisdata-v1-FreshnessConfig)
    - [GetQualityIssueRequest](#metaxisdata-v1-GetQualityIssueRequest)
    - [GetQualityRuleRequest](#metaxisdata-v1-GetQualityRuleRequest)
    - [GetQualityRunRequest](#metaxisdata-v1-GetQualityRunRequest)
    - [GroupCheckConfig](#metaxisdata-v1-GroupCheckConfig)
    - [ListColumnLabelsRequest](#metaxisdata-v1-ListColumnLabelsRequest)
    - [ListColumnLabelsResponse](#metaxisdata-v1-ListColumnLabelsResponse)
    - [ListColumnProfilesRequest](#metaxisdata-v1-ListColumnProfilesRequest)
    - [ListColumnProfilesResponse](#metaxisdata-v1-ListColumnProfilesResponse)
    - [ListQualityIssuesRequest](#metaxisdata-v1-ListQualityIssuesRequest)
    - [ListQualityIssuesResponse](#metaxisdata-v1-ListQualityIssuesResponse)
    - [ListQualityRuleResultsRequest](#metaxisdata-v1-ListQualityRuleResultsRequest)
    - [ListQualityRuleResultsResponse](#metaxisdata-v1-ListQualityRuleResultsResponse)
    - [ListQualityRulesRequest](#metaxisdata-v1-ListQualityRulesRequest)
    - [ListQualityRulesResponse](#metaxisdata-v1-ListQualityRulesResponse)
    - [ListQualityRunsRequest](#metaxisdata-v1-ListQualityRunsRequest)
    - [ListQualityRunsResponse](#metaxisdata-v1-ListQualityRunsResponse)
    - [ListQualitySuggestionsRequest](#metaxisdata-v1-ListQualitySuggestionsRequest)
    - [ListQualitySuggestionsResponse](#metaxisdata-v1-ListQualitySuggestionsResponse)
    - [NullPercentConfig](#metaxisdata-v1-NullPercentConfig)
    - [PreviewQualityRuleRequest](#metaxisdata-v1-PreviewQualityRuleRequest)
    - [PreviewQualityRuleResponse](#metaxisdata-v1-PreviewQualityRuleResponse)
    - [ProfileColumnsRequest](#metaxisdata-v1-ProfileColumnsRequest)
    - [ProfileColumnsResponse](#metaxisdata-v1-ProfileColumnsResponse)
    - [ProfileValue](#metaxisdata-v1-ProfileValue)
    - [QualityFilter](#metaxisdata-v1-QualityFilter)
    - [QualityFilterClause](#metaxisdata-v1-QualityFilterClause)
    - [QualityIssue](#metaxisdata-v1-QualityIssue)
    - [QualityResultSummary](#metaxisdata-v1-QualityResultSummary)
    - [QualityRule](#metaxisdata-v1-QualityRule)
    - [QualityRuleConfig](#metaxisdata-v1-QualityRuleConfig)
    - [QualityRuleDraftConfig](#metaxisdata-v1-QualityRuleDraftConfig)
    - [QualityRuleResult](#metaxisdata-v1-QualityRuleResult)
    - [QualityRun](#metaxisdata-v1-QualityRun)
    - [QualityRunCounts](#metaxisdata-v1-QualityRunCounts)
    - [QualitySuggestion](#metaxisdata-v1-QualitySuggestion)
    - [QualitySuggestionPayload](#metaxisdata-v1-QualitySuggestionPayload)
    - [QualityTarget](#metaxisdata-v1-QualityTarget)
    - [QualityTargetPath](#metaxisdata-v1-QualityTargetPath)
    - [QualityThresholds](#metaxisdata-v1-QualityThresholds)
    - [RangeConfig](#metaxisdata-v1-RangeConfig)
    - [ReferentialIntegrityConfig](#metaxisdata-v1-ReferentialIntegrityConfig)
    - [RegexConfig](#metaxisdata-v1-RegexConfig)
    - [RowCountChangeConfig](#metaxisdata-v1-RowCountChangeConfig)
    - [RowCountConfig](#metaxisdata-v1-RowCountConfig)
    - [RunQualityRulesRequest](#metaxisdata-v1-RunQualityRulesRequest)
    - [RunQualityRulesResponse](#metaxisdata-v1-RunQualityRulesResponse)
    - [SchemaDriftColumn](#metaxisdata-v1-SchemaDriftColumn)
    - [SchemaDriftConfig](#metaxisdata-v1-SchemaDriftConfig)
    - [SemanticLabelEvidence](#metaxisdata-v1-SemanticLabelEvidence)
    - [SemanticLabelSuggestion](#metaxisdata-v1-SemanticLabelSuggestion)
    - [StringLengthConfig](#metaxisdata-v1-StringLengthConfig)
    - [SuggestColumnLabelsRequest](#metaxisdata-v1-SuggestColumnLabelsRequest)
    - [SuggestColumnLabelsResponse](#metaxisdata-v1-SuggestColumnLabelsResponse)
    - [SuggestQualityRuleRequest](#metaxisdata-v1-SuggestQualityRuleRequest)
    - [SuggestQualityRuleResponse](#metaxisdata-v1-SuggestQualityRuleResponse)
    - [UniqueCombinationConfig](#metaxisdata-v1-UniqueCombinationConfig)
    - [UniqueConfig](#metaxisdata-v1-UniqueConfig)
    - [UpdateColumnLabelRequest](#metaxisdata-v1-UpdateColumnLabelRequest)
    - [UpdateColumnLabelResponse](#metaxisdata-v1-UpdateColumnLabelResponse)
    - [UpdateQualityIssueRequest](#metaxisdata-v1-UpdateQualityIssueRequest)
    - [UpdateQualityRuleRequest](#metaxisdata-v1-UpdateQualityRuleRequest)
    - [UpdateQualitySuggestionRequest](#metaxisdata-v1-UpdateQualitySuggestionRequest)
  
    - [ColumnLabelAction](#metaxisdata-v1-ColumnLabelAction)
    - [QualityAggregateFunction](#metaxisdata-v1-QualityAggregateFunction)
    - [QualityComparison](#metaxisdata-v1-QualityComparison)
    - [QualityComparisonOperator](#metaxisdata-v1-QualityComparisonOperator)
    - [QualityDimension](#metaxisdata-v1-QualityDimension)
    - [QualityEmptyBehavior](#metaxisdata-v1-QualityEmptyBehavior)
    - [QualityFilterOperator](#metaxisdata-v1-QualityFilterOperator)
    - [QualityIssueStatus](#metaxisdata-v1-QualityIssueStatus)
    - [QualityLabelSource](#metaxisdata-v1-QualityLabelSource)
    - [QualityLabelStatus](#metaxisdata-v1-QualityLabelStatus)
    - [QualityObservedKind](#metaxisdata-v1-QualityObservedKind)
    - [QualityResolutionReason](#metaxisdata-v1-QualityResolutionReason)
    - [QualityRuleState](#metaxisdata-v1-QualityRuleState)
    - [QualityRuleType](#metaxisdata-v1-QualityRuleType)
    - [QualityRunStatus](#metaxisdata-v1-QualityRunStatus)
    - [QualityRunTrigger](#metaxisdata-v1-QualityRunTrigger)
    - [QualitySemanticType](#metaxisdata-v1-QualitySemanticType)
    - [QualitySeverity](#metaxisdata-v1-QualitySeverity)
    - [QualityStatus](#metaxisdata-v1-QualityStatus)
    - [QualitySuggestionKind](#metaxisdata-v1-QualitySuggestionKind)
    - [QualitySuggestionSource](#metaxisdata-v1-QualitySuggestionSource)
    - [QualitySuggestionStatus](#metaxisdata-v1-QualitySuggestionStatus)
    - [QualityThresholdMetric](#metaxisdata-v1-QualityThresholdMetric)
  
    - [DataQualityService](#metaxisdata-v1-DataQualityService)
  
- [v1/environment_service.proto](#v1_environment_service-proto)
    - [CreateEnvironmentRequest](#metaxisdata-v1-CreateEnvironmentRequest)
    - [DeleteEnvironmentRequest](#metaxisdata-v1-DeleteEnvironmentRequest)
    - [Environment](#metaxisdata-v1-Environment)
    - [Environment.TagsEntry](#metaxisdata-v1-Environment-TagsEntry)
    - [ListEnvironmentsRequest](#metaxisdata-v1-ListEnvironmentsRequest)
    - [ListEnvironmentsResponse](#metaxisdata-v1-ListEnvironmentsResponse)
    - [UpdateEnvironmentRequest](#metaxisdata-v1-UpdateEnvironmentRequest)
  
    - [EnvironmentService](#metaxisdata-v1-EnvironmentService)
  
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
  
- [v1/identity_provider_service.proto](#v1_identity_provider_service-proto)
    - [CreateIdentityProviderRequest](#metaxisdata-v1-CreateIdentityProviderRequest)
    - [DeleteIdentityProviderBindingRequest](#metaxisdata-v1-DeleteIdentityProviderBindingRequest)
    - [DeleteIdentityProviderRequest](#metaxisdata-v1-DeleteIdentityProviderRequest)
    - [FieldMapping](#metaxisdata-v1-FieldMapping)
    - [GetIdentityProviderRequest](#metaxisdata-v1-GetIdentityProviderRequest)
    - [IdentityProvider](#metaxisdata-v1-IdentityProvider)
    - [IdentityProviderBinding](#metaxisdata-v1-IdentityProviderBinding)
    - [IdentityProviderConfig](#metaxisdata-v1-IdentityProviderConfig)
    - [IdentityProviderUserInfo](#metaxisdata-v1-IdentityProviderUserInfo)
    - [ListIdentityProviderBindingsRequest](#metaxisdata-v1-ListIdentityProviderBindingsRequest)
    - [ListIdentityProviderBindingsResponse](#metaxisdata-v1-ListIdentityProviderBindingsResponse)
    - [ListIdentityProvidersRequest](#metaxisdata-v1-ListIdentityProvidersRequest)
    - [ListIdentityProvidersResponse](#metaxisdata-v1-ListIdentityProvidersResponse)
    - [OAuth2IdentityProviderConfig](#metaxisdata-v1-OAuth2IdentityProviderConfig)
    - [OAuth2IdentityProviderTestRequestContext](#metaxisdata-v1-OAuth2IdentityProviderTestRequestContext)
    - [TestIdentityProviderRequest](#metaxisdata-v1-TestIdentityProviderRequest)
    - [TestIdentityProviderResponse](#metaxisdata-v1-TestIdentityProviderResponse)
    - [UpdateIdentityProviderRequest](#metaxisdata-v1-UpdateIdentityProviderRequest)
  
    - [IdentityProviderType](#metaxisdata-v1-IdentityProviderType)
    - [OAuth2AuthStyle](#metaxisdata-v1-OAuth2AuthStyle)
  
    - [IdentityProviderService](#metaxisdata-v1-IdentityProviderService)
  
- [v1/lineage_service.proto](#v1_lineage_service-proto)
    - [AnalysisScope](#metaxisdata-v1-AnalysisScope)
    - [AnalyzeSQLDiagnostic](#metaxisdata-v1-AnalyzeSQLDiagnostic)
    - [AnalyzeSQLRelation](#metaxisdata-v1-AnalyzeSQLRelation)
    - [AnalyzeSQLRequest](#metaxisdata-v1-AnalyzeSQLRequest)
    - [AnalyzeSQLResponse](#metaxisdata-v1-AnalyzeSQLResponse)
    - [AnalyzeSQLResult](#metaxisdata-v1-AnalyzeSQLResult)
    - [ExternalDatasetInfo](#metaxisdata-v1-ExternalDatasetInfo)
    - [GetLineageCountsRequest](#metaxisdata-v1-GetLineageCountsRequest)
    - [GetLineageCountsResponse](#metaxisdata-v1-GetLineageCountsResponse)
    - [GetLineageForContextRequest](#metaxisdata-v1-GetLineageForContextRequest)
    - [GetLineageForContextResponse](#metaxisdata-v1-GetLineageForContextResponse)
    - [GetLineageGraphRequest](#metaxisdata-v1-GetLineageGraphRequest)
    - [GetLineageGraphResponse](#metaxisdata-v1-GetLineageGraphResponse)
    - [GetLineageRequest](#metaxisdata-v1-GetLineageRequest)
    - [GetLineageResponse](#metaxisdata-v1-GetLineageResponse)
    - [LineageCount](#metaxisdata-v1-LineageCount)
    - [LineageNode](#metaxisdata-v1-LineageNode)
    - [LineageRelation](#metaxisdata-v1-LineageRelation)
    - [Transformation](#metaxisdata-v1-Transformation)
  
    - [DiagnosticCategory](#metaxisdata-v1-DiagnosticCategory)
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
  
- [v1/notification_service.proto](#v1_notification_service-proto)
    - [BatchMarkNotificationsReadRequest](#metaxisdata-v1-BatchMarkNotificationsReadRequest)
    - [DataQualityDetail](#metaxisdata-v1-DataQualityDetail)
    - [DeleteNotificationRequest](#metaxisdata-v1-DeleteNotificationRequest)
    - [GetUnreadNotificationCountRequest](#metaxisdata-v1-GetUnreadNotificationCountRequest)
    - [GetUnreadNotificationCountResponse](#metaxisdata-v1-GetUnreadNotificationCountResponse)
    - [KeepAlive](#metaxisdata-v1-KeepAlive)
    - [ListNotificationsRequest](#metaxisdata-v1-ListNotificationsRequest)
    - [ListNotificationsResponse](#metaxisdata-v1-ListNotificationsResponse)
    - [MarkAllNotificationsReadRequest](#metaxisdata-v1-MarkAllNotificationsReadRequest)
    - [MarkNotificationsSeenRequest](#metaxisdata-v1-MarkNotificationsSeenRequest)
    - [Notification](#metaxisdata-v1-Notification)
    - [NotificationEvent](#metaxisdata-v1-NotificationEvent)
    - [OpenLineageDetail](#metaxisdata-v1-OpenLineageDetail)
    - [SchemaSyncDetail](#metaxisdata-v1-SchemaSyncDetail)
    - [SubscribeNotificationsRequest](#metaxisdata-v1-SubscribeNotificationsRequest)
    - [SubscribeNotificationsResponse](#metaxisdata-v1-SubscribeNotificationsResponse)
    - [SyncDatabaseResult](#metaxisdata-v1-SyncDatabaseResult)
  
    - [DataQualityNotificationKind](#metaxisdata-v1-DataQualityNotificationKind)
    - [NotificationSeverity](#metaxisdata-v1-NotificationSeverity)
    - [NotificationType](#metaxisdata-v1-NotificationType)
    - [OpenLineageFailureKind](#metaxisdata-v1-OpenLineageFailureKind)
    - [SyncDatabaseState](#metaxisdata-v1-SyncDatabaseState)
    - [SyncTrigger](#metaxisdata-v1-SyncTrigger)
  
    - [NotificationService](#metaxisdata-v1-NotificationService)
  
- [v1/oauth_service.proto](#v1_oauth_service-proto)
    - [ApproveOAuthAuthorizationRequestRequest](#metaxisdata-v1-ApproveOAuthAuthorizationRequestRequest)
    - [GetOAuthAuthorizationRequestRequest](#metaxisdata-v1-GetOAuthAuthorizationRequestRequest)
    - [OAuthAuthorizationRequest](#metaxisdata-v1-OAuthAuthorizationRequest)
  
    - [OAuthService](#metaxisdata-v1-OAuthService)
  
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
    - [ListOpenLineageFilterOptionsRequest](#metaxisdata-v1-ListOpenLineageFilterOptionsRequest)
    - [ListOpenLineageFilterOptionsResponse](#metaxisdata-v1-ListOpenLineageFilterOptionsResponse)
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
    - [OpenLineageFilterOption](#metaxisdata-v1-OpenLineageFilterOption)
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
    - [DataQualitySetting](#metaxisdata-v1-DataQualitySetting)
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

 

 



<a name="v1_common-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/common.proto


 


<a name="metaxisdata-v1-Engine"></a>

### Engine
Engine is the database engine of an instance. Only the engines the product
actually supports are kept: MySQL-compatible ones (MySQL, TiDB, MariaDB,
OceanBase, StarRocks, Doris), PostgreSQL and SQL Server. The other removed
values keep their numbers reserved so they can never be silently reused for
something else.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ENGINE_UNSPECIFIED | 0 |  |
| MSSQL | 1 |  |
| MYSQL | 2 |  |
| POSTGRES | 3 |  |
| TIDB | 6 |  |
| MARIADB | 13 |  |
| OCEANBASE | 14 |  |
| STARROCKS | 15 |  |
| DORIS | 16 |  |



<a name="metaxisdata-v1-State"></a>

### State


| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNSPECIFIED | 0 |  |
| ACTIVE | 1 |  |
| DELETED | 2 |  |


 

 

 



<a name="v1_application_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/application_service.proto



<a name="metaxisdata-v1-Application"></a>

### Application
Application is one application whose statements are reported to the workspace.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name. Format: applications/{application_id} |
| application_id | [string](#string) |  | The stable identifier, equal to the last segment of `name`. |
| display_name | [string](#string) |  | The display name. It is unique: it is how a person names the application. |
| description | [string](#string) |  |  |
| disabled | [bool](#bool) |  | A stopped application keeps every row it ever reported, and none of its keys report anything any more. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-ApplicationAPIKey"></a>

### ApplicationAPIKey
ApplicationAPIKey is one reporting credential. Only its masked form is ever returned
after creation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name. Format: applications/{application_id}/apiKeys/{api_key} |
| api_key_id | [string](#string) |  |  |
| masked_key | [string](#string) |  | The masked key, safe to display. |
| description | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_used_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | The last request this key was used for, and how many it has made. |
| request_count | [int64](#int64) |  | Per-key counters. They stand in for one audit row per request: the audit ledger is kept forever, so auditing machine traffic would grow it with request volume rather than with distinct statements. |
| statement_accepted_count | [int64](#int64) |  | Statements rather than reports: a report whose entry carried a script counts once per statement in it. |
| statement_rejected_count | [int64](#int64) |  |  |






<a name="metaxisdata-v1-ApplicationSQL"></a>

### ApplicationSQL
ApplicationSQL is one distinct statement an application reported, with every literal
already replaced by `?`.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The globally unique metadata GUID. |
| application_sql_id | [string](#string) |  |  |
| instance_resource_id | [string](#string) |  |  |
| database_name | [string](#string) |  |  |
| schema_name | [string](#string) |  |  |
| engine | [Engine](#metaxisdata-v1-Engine) |  |  |
| sql_text | [string](#string) |  | The parameterized statement. This is the only form ever stored. |
| kind | [ApplicationSQLKind](#metaxisdata-v1-ApplicationSQLKind) |  |  |
| fingerprint | [string](#string) |  | The fingerprint that identifies the statement within its scope, and the version of the rules that produced it. |
| fingerprint_version | [int32](#int32) |  |  |
| execution_count | [int64](#int64) |  | How many times every application ran it, and when it was first and last seen. |
| first_seen_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_seen_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| observations | [ApplicationSQLObservation](#metaxisdata-v1-ApplicationSQLObservation) | repeated | The applications that reported it, with each one&#39;s own history. |






<a name="metaxisdata-v1-ApplicationSQLObservation"></a>

### ApplicationSQLObservation
ApplicationSQLObservation is one application&#39;s own history with one statement.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| application | [string](#string) |  |  |
| application_display_name | [string](#string) |  |  |
| execution_count | [int64](#int64) |  |  |
| first_seen_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_seen_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-CreateApplicationAPIKeyRequest"></a>

### CreateApplicationAPIKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: applications/{application_id} |
| description | [string](#string) |  |  |






<a name="metaxisdata-v1-CreateApplicationAPIKeyResponse"></a>

### CreateApplicationAPIKeyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_key | [string](#string) |  |  |
| key | [ApplicationAPIKey](#metaxisdata-v1-ApplicationAPIKey) |  |  |






<a name="metaxisdata-v1-CreateApplicationRequest"></a>

### CreateApplicationRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| application | [Application](#metaxisdata-v1-Application) |  |  |
| api_key_description | [string](#string) |  | The first API key&#39;s description, so the credential that a fresh application reports with can be told apart from the ones issued later. |






<a name="metaxisdata-v1-CreateApplicationResponse"></a>

### CreateApplicationResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| application | [Application](#metaxisdata-v1-Application) |  |  |
| api_key | [string](#string) |  | The plaintext API key. It is returned once and never stored in a recoverable form. |
| key | [ApplicationAPIKey](#metaxisdata-v1-ApplicationAPIKey) |  |  |






<a name="metaxisdata-v1-DeleteApplicationRequest"></a>

### DeleteApplicationRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: applications/{application_id} |






<a name="metaxisdata-v1-DeleteApplicationSQLRequest"></a>

### DeleteApplicationSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The globally unique metadata GUID of the statement. |






<a name="metaxisdata-v1-GetApplicationSQLRequest"></a>

### GetApplicationSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The globally unique metadata GUID of the statement. |






<a name="metaxisdata-v1-ListApplicationAPIKeysRequest"></a>

### ListApplicationAPIKeysRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: applications/{application_id} |






<a name="metaxisdata-v1-ListApplicationAPIKeysResponse"></a>

### ListApplicationAPIKeysResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| keys | [ApplicationAPIKey](#metaxisdata-v1-ApplicationAPIKey) | repeated |  |






<a name="metaxisdata-v1-ListApplicationSQLsRequest"></a>

### ListApplicationSQLsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: instances/{instance}/databases/{database} |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| application | [string](#string) |  | Only the statements this application has reported. |
| schema_name | [string](#string) |  |  |
| kind | [ApplicationSQLKind](#metaxisdata-v1-ApplicationSQLKind) |  |  |
| query | [string](#string) |  | Full-text search over the parameterized statements. |






<a name="metaxisdata-v1-ListApplicationSQLsResponse"></a>

### ListApplicationSQLsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| application_sqls | [ApplicationSQL](#metaxisdata-v1-ApplicationSQL) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListApplicationsRequest"></a>

### ListApplicationsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| show_disabled | [bool](#bool) |  | Include stopped applications. The default lists only the ones a report may still come from. |






<a name="metaxisdata-v1-ListApplicationsResponse"></a>

### ListApplicationsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| applications | [Application](#metaxisdata-v1-Application) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-RevokeApplicationAPIKeyRequest"></a>

### RevokeApplicationAPIKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: applications/{application_id}/apiKeys/{api_key_id} |






<a name="metaxisdata-v1-UpdateApplicationRequest"></a>

### UpdateApplicationRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| application | [Application](#metaxisdata-v1-Application) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |





 


<a name="metaxisdata-v1-ApplicationSQLKind"></a>

### ApplicationSQLKind
ApplicationSQLEngineKind is the read/write classification of a statement.

| Name | Number | Description |
| ---- | ------ | ----------- |
| APPLICATION_SQL_KIND_UNSPECIFIED | 0 |  |
| APPLICATION_SQL_KIND_UNKNOWN | 1 | Not analyzed yet: the runner writes the classification after it analyzes the statement. |
| APPLICATION_SQL_KIND_READ | 2 |  |
| APPLICATION_SQL_KIND_WRITE | 3 |  |


 

 


<a name="metaxisdata-v1-ApplicationService"></a>

### ApplicationService
ApplicationService manages the applications that report the SQL they run, and the
statements they have reported.

An application is deliberately not a metadata resource: it has no schema, no columns and
no lineage of its own. Creating one issues its first API key, which is the credential the
reporting endpoint authenticates; the key names the application, so a report cannot claim
to be another one.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| CreateApplication | [CreateApplicationRequest](#metaxisdata-v1-CreateApplicationRequest) | [CreateApplicationResponse](#metaxisdata-v1-CreateApplicationResponse) |  |
| ListApplications | [ListApplicationsRequest](#metaxisdata-v1-ListApplicationsRequest) | [ListApplicationsResponse](#metaxisdata-v1-ListApplicationsResponse) |  |
| UpdateApplication | [UpdateApplicationRequest](#metaxisdata-v1-UpdateApplicationRequest) | [Application](#metaxisdata-v1-Application) |  |
| DeleteApplication | [DeleteApplicationRequest](#metaxisdata-v1-DeleteApplicationRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| CreateApplicationAPIKey | [CreateApplicationAPIKeyRequest](#metaxisdata-v1-CreateApplicationAPIKeyRequest) | [CreateApplicationAPIKeyResponse](#metaxisdata-v1-CreateApplicationAPIKeyResponse) |  |
| ListApplicationAPIKeys | [ListApplicationAPIKeysRequest](#metaxisdata-v1-ListApplicationAPIKeysRequest) | [ListApplicationAPIKeysResponse](#metaxisdata-v1-ListApplicationAPIKeysResponse) |  |
| RevokeApplicationAPIKey | [RevokeApplicationAPIKeyRequest](#metaxisdata-v1-RevokeApplicationAPIKeyRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| ListApplicationSQLs | [ListApplicationSQLsRequest](#metaxisdata-v1-ListApplicationSQLsRequest) | [ListApplicationSQLsResponse](#metaxisdata-v1-ListApplicationSQLsResponse) | Lists reported statements. The parent scopes the result to one instance and database. |
| GetApplicationSQL | [GetApplicationSQLRequest](#metaxisdata-v1-GetApplicationSQLRequest) | [ApplicationSQL](#metaxisdata-v1-ApplicationSQL) | Reads one reported statement. It is addressed by GUID rather than by resource name because every entry point to a statement already holds a GUID: the lineage graph, the history page and GetMetadata. |
| DeleteApplicationSQL | [DeleteApplicationSQLRequest](#metaxisdata-v1-DeleteApplicationSQLRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |

 



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

For example: name == &#34;ed&#34; name.matches(&#34;ed&#34;) email == &#34;ed@example.com&#34; email.matches(&#34;ed&#34;) user_type == &#34;END_USER&#34; user_type in [&#34;END_USER&#34;] !(user_type in [&#34;END_USER&#34;]) state == &#34;DELETED&#34; You can combine filter conditions like: name.matches(&#34;ed&#34;) &amp;&amp; state == &#34;ACTIVE&#34; (name == &#34;ed&#34; || email == &#34;ed@example.com&#34;) &amp;&amp; user_type == &#34;END_USER&#34; |






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
| email | [string](#string) |  | The user&#39;s email address. It identifies the member to the outside world, so only a caller with metaxisdata.users.update may set it, on their own account or on someone else&#39;s. |
| title | [string](#string) |  |  |
| user_type | [UserType](#metaxisdata-v1-UserType) |  |  |
| password | [string](#string) |  |  |
| service_key | [string](#string) |  |  |
| phone | [string](#string) |  | Should be a valid E.164 compliant phone number. Could be empty. |
| profile | [UserProfile](#metaxisdata-v1-UserProfile) |  |  |
| groups | [string](#string) | repeated | The groups for the user. Format: groups/{email} |
| permissions | [string](#string) | repeated | The effective workspace permissions of the caller, as a list of `metaxisdata.&lt;resource&gt;.&lt;verb&gt;` strings. Populated by GetCurrentUser only, so the frontend can gate navigation and actions without probing each RPC. |
| language | [string](#string) |  | The BCP-47 tag of the language the user reads the UI in, so server-side prose — the Explain SQL answer — is written in the language the user selected. The SPA records it when the user switches language; empty means never chosen, and server-side prose then falls back to en-US. |






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
| GetUser | [GetUserRequest](#metaxisdata-v1-GetUserRequest) | [User](#metaxisdata-v1-User) | Get the user. Requires metaxisdata.users.get. |
| BatchGetUsers | [BatchGetUsersRequest](#metaxisdata-v1-BatchGetUsersRequest) | [BatchGetUsersResponse](#metaxisdata-v1-BatchGetUsersResponse) | Get the users in batch. Requires metaxisdata.users.get. |
| GetCurrentUser | [.google.protobuf.Empty](#google-protobuf-Empty) | [User](#metaxisdata-v1-User) | Get the current authenticated user. Permissions required: None |
| ListUsers | [ListUsersRequest](#metaxisdata-v1-ListUsersRequest) | [ListUsersResponse](#metaxisdata-v1-ListUsersResponse) | List all users. Requires metaxisdata.users.list. |
| CreateUser | [CreateUserRequest](#metaxisdata-v1-CreateUserRequest) | [User](#metaxisdata-v1-User) | Create a user. |
| UpdateUser | [UpdateUserRequest](#metaxisdata-v1-UpdateUserRequest) | [User](#metaxisdata-v1-User) | Only the user itself and the user with permission on the workspace can update the user. Changing an email address requires metaxisdata.users.update even for the caller&#39;s own account: the address is the identity a member is recognized by outside the workspace (and by single sign-on), so it is an administrative field, not a profile setting. |
| DeleteUser | [DeleteUserRequest](#metaxisdata-v1-DeleteUserRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Only the user with permission on the workspace can delete the user. The last remaining workspace admin cannot be deleted. |
| UndeleteUser | [UndeleteUserRequest](#metaxisdata-v1-UndeleteUserRequest) | [User](#metaxisdata-v1-User) | Only the user with permission on the workspace can undelete the user. |

 



<a name="v1_auth_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/auth_service.proto



<a name="metaxisdata-v1-ApproveDeviceLoginRequest"></a>

### ApproveDeviceLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: deviceLogins/{user_code} |
| approve | [bool](#bool) |  |  |






<a name="metaxisdata-v1-CreateDeviceLoginRequest"></a>

### CreateDeviceLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client_name | [string](#string) |  | A human-readable client name for the confirmation page, e.g. &#34;mxd&#34;. It is display only and never trusted. |
| client_version | [string](#string) |  |  |






<a name="metaxisdata-v1-CreateDeviceLoginResponse"></a>

### CreateDeviceLoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| device_code | [string](#string) |  | The polling secret. Only the client that created the request holds it, and it never appears in a URL or in an audit record. |
| user_code | [string](#string) |  | The human-readable code the user types on the confirmation page, in Crockford base32 with an &#34;XXXX-XXXX&#34; layout. |
| verification_uri | [string](#string) |  | The bare confirmation address, `{external_url}/device`. Empty when the workspace has no external URL configured. |
| verification_uri_complete | [string](#string) |  | The confirmation address with the code prefilled. Clients should only use it when the user explicitly asked to skip typing the code. |
| expires_in | [int32](#int32) |  | Lifetime of this request in seconds. |
| interval | [int32](#int32) |  | The minimum polling interval in seconds. |






<a name="metaxisdata-v1-CreateSSOStateResponse"></a>

### CreateSSOStateResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| state | [string](#string) |  |  |






<a name="metaxisdata-v1-DeviceLogin"></a>

### DeviceLogin
DeviceLogin is a pending or resolved device authorization request. Its
resource name is derived from the human-readable code so the confirmation
page needs nothing else.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: deviceLogins/{user_code} |
| state | [DeviceLoginState](#metaxisdata-v1-DeviceLoginState) |  |  |
| user_code | [string](#string) |  |  |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| expire_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| client_name | [string](#string) |  |  |
| client_version | [string](#string) |  |  |
| request_ip | [string](#string) |  | The source address the request came from, as the server sees it (it follows the trusted-proxy rules). |
| request_user_agent | [string](#string) |  |  |
| approved_by | [User](#metaxisdata-v1-User) |  | The user who approved the request, once it is approved. |






<a name="metaxisdata-v1-ExchangeDeviceLoginRequest"></a>

### ExchangeDeviceLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| device_code | [string](#string) |  |  |






<a name="metaxisdata-v1-ExchangeDeviceLoginResponse"></a>

### ExchangeDeviceLoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| state | [DeviceLoginState](#metaxisdata-v1-DeviceLoginState) |  |  |
| token | [string](#string) |  | The access token, returned only when the request was approved. The device login is consumed at the same time, so a second exchange finds nothing. |
| expires_in | [int64](#int64) |  | The remaining lifetime of the token in seconds. |
| user | [User](#metaxisdata-v1-User) |  | The user who approved the request. |






<a name="metaxisdata-v1-GetDeviceLoginRequest"></a>

### GetDeviceLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the device login to read. Format: deviceLogins/{user_code} |






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
| web | [bool](#bool) |  | If web is set, the access token and the refresh token are set as HttpOnly cookies instead of being returned in the response body. |
| idp_name | [string](#string) |  | The name of the identity provider. Format: idps/{idp} |
| idp_context | [IdentityProviderContext](#metaxisdata-v1-IdentityProviderContext) |  | The idp_context is using to get the user information from identity provider. |






<a name="metaxisdata-v1-LoginResponse"></a>

### LoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  | The access token. Empty for a web login, which carries the token in an HttpOnly cookie instead. |
| require_reset_password | [bool](#bool) |  | Whether the workspace password policy requires the user to rotate their password before doing anything else. When true, the issued token is restricted to changing the user&#39;s own password (and logging out). |
| user | [User](#metaxisdata-v1-User) |  | The user of successful login. |
| account_adopted | [bool](#bool) |  | Set when this SSO login adopted an existing account that already carried the identity provider&#39;s address: the account&#39;s password was replaced with a random one and every session minted before that was retired, so the client has to tell the user to sign in through the identity provider from now on. Always false for a password login. The name avoids the audit redaction&#39;s &#34;password&#34; marker so the adoption is visible in the ledger. |






<a name="metaxisdata-v1-LogoutRequest"></a>

### LogoutRequest







<a name="metaxisdata-v1-OAuth2IdentityProviderContext"></a>

### OAuth2IdentityProviderContext



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  |  |
| state | [string](#string) |  | The state issued by CreateSSOState. Required: the server rejects a login whose state is missing, unknown or already used. |
| code_verifier | [string](#string) |  | code_verifier is the PKCE verifier matching the code_challenge sent to the identity provider. Optional; providers that do not use PKCE ignore it. |






<a name="metaxisdata-v1-RefreshRequest"></a>

### RefreshRequest







<a name="metaxisdata-v1-RefreshResponse"></a>

### RefreshResponse
RefreshResponse is empty: both credentials the refresh produces travel in
HttpOnly cookies, so no token ever reaches page JavaScript.





 


<a name="metaxisdata-v1-DeviceLoginState"></a>

### DeviceLoginState


| Name | Number | Description |
| ---- | ------ | ----------- |
| DEVICE_LOGIN_STATE_UNSPECIFIED | 0 |  |
| PENDING | 1 |  |
| APPROVED | 2 |  |
| DENIED | 3 |  |
| EXPIRED | 4 |  |


 

 


<a name="metaxisdata-v1-AuthService"></a>

### AuthService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Login | [LoginRequest](#metaxisdata-v1-LoginRequest) | [LoginResponse](#metaxisdata-v1-LoginResponse) | Permissions required: None |
| Logout | [LogoutRequest](#metaxisdata-v1-LogoutRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Permissions required: None |
| Refresh | [RefreshRequest](#metaxisdata-v1-RefreshRequest) | [RefreshResponse](#metaxisdata-v1-RefreshResponse) | Refresh rotates the web session: it consumes the refresh-token cookie and sets a new access token and a new refresh token cookie. It needs no credential because it is called exactly when the access token has expired, and it rotates the pair so a stolen refresh token is worthless once the real holder refreshes. Permissions required: None |
| CreateSSOState | [.google.protobuf.Empty](#google-protobuf-Empty) | [CreateSSOStateResponse](#metaxisdata-v1-CreateSSOStateResponse) | CreateSSOState issues a one-time OAuth2 state value. A client must fetch it before redirecting to the identity provider, pass it back to the provider and then send it with the login request; the server consumes it there. Without it an attacker can complete an authorization-code flow in a victim&#39;s browser and bind the victim&#39;s session to the attacker&#39;s identity. Permissions required: None |
| CreateDeviceLogin | [CreateDeviceLoginRequest](#metaxisdata-v1-CreateDeviceLoginRequest) | [CreateDeviceLoginResponse](#metaxisdata-v1-CreateDeviceLoginResponse) | CreateDeviceLogin starts a device login (RFC 8628 style). The caller gets a polling secret plus a short human-readable code; a signed-in user approves the request from the web confirmation page and the CLI then exchanges the secret for an access token bound to that user. Permissions required: None |
| GetDeviceLogin | [GetDeviceLoginRequest](#metaxisdata-v1-GetDeviceLoginRequest) | [DeviceLogin](#metaxisdata-v1-DeviceLogin) | GetDeviceLogin returns one pending device login so the confirmation page can show what is being approved. Permissions required: None |
| ApproveDeviceLogin | [ApproveDeviceLoginRequest](#metaxisdata-v1-ApproveDeviceLoginRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | ApproveDeviceLogin approves or denies a pending device login. The approver becomes the identity the access token is issued for. Permissions required: None |
| ExchangeDeviceLogin | [ExchangeDeviceLoginRequest](#metaxisdata-v1-ExchangeDeviceLoginRequest) | [ExchangeDeviceLoginResponse](#metaxisdata-v1-ExchangeDeviceLoginResponse) | ExchangeDeviceLogin polls a device login. An approved request is consumed and answered with the token; every other state is reported as-is. Permissions required: None |

 



<a name="v1_connected_app_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/connected_app_service.proto



<a name="metaxisdata-v1-ConnectedApp"></a>

### ConnectedApp
ConnectedApp is one user&#39;s authorization of one MCP client. It is derived from
the grant record the OAuth token endpoint rotates in place, not from the
client registration: the same client may be authorized several times, and each
authorization is its own row.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the connected app, in the form `connectedApps/{id}`. |
| client_id | [string](#string) |  | The registered client identifier (RFC 7591). Opaque. |
| client_name | [string](#string) |  | The client&#39;s self-reported name at registration. It is not verified, so it is display text only. |
| user | [string](#string) |  | The user whose identity the client acts as, in the form `users/{id}`. |
| user_email | [string](#string) |  | The user&#39;s email. It is filled only for the caller&#39;s own rows or when the caller may read users; a connected-apps reader is not thereby a user reader. |
| resource | [string](#string) |  | The MCP resource identifier the authorization was granted for. |
| scopes | [string](#string) | repeated | The OAuth scopes the user consented to. |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the authorization completed (the first token pair was issued). |
| last_tool_call_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | The most recent tool call made with this connection. Empty when the client has been authorized but has not called a tool yet. |
| expire_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the refresh credential expires. A connection past this instant cannot mint another access token. |
| state | [ConnectedApp.State](#metaxisdata-v1-ConnectedApp-State) |  | The connection&#39;s current state. |
| revoke_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the connection was revoked. Set only for REVOKED. |






<a name="metaxisdata-v1-ListConnectedAppsRequest"></a>

### ListConnectedAppsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of connected apps to return. The service may return fewer than this value. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListConnectedApps` call. Provide this to retrieve the subsequent page. The filter and user parameters must not change between pages. |
| user | [string](#string) |  | The user whose connected apps to list, in the form `users/{id}`. Omitted, a caller holding metaxisdata.connectedApps.list sees every connection in the workspace, and any other caller sees only their own. Naming another user is refused unless the caller holds that permission. |
| filter | [string](#string) |  | A CEL filter over the list. Supported variables:

 user == &#34;users/{id}&#34; state == &#34;ACTIVE&#34; | &#34;REVOKED&#34; | &#34;EXPIRED&#34; | &#34;REAUTH_REQUIRED&#34; | &#34;INACTIVE&#34; client == &#34;name&#34; (exact), client.matches(&#34;substring&#34;) client_id == &#34;id&#34; (exact), client_id.matches(&#34;substring&#34;) user_email == &#34;email&#34; (exact), user_email.matches(&#34;substring&#34;) create_time &gt;= / &lt;= RFC 3339

The filter only narrows the rows the caller is already allowed to see. |






<a name="metaxisdata-v1-ListConnectedAppsResponse"></a>

### ListConnectedAppsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| connected_apps | [ConnectedApp](#metaxisdata-v1-ConnectedApp) | repeated | The connected apps on this page. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="metaxisdata-v1-RevokeConnectedAppRequest"></a>

### RevokeConnectedAppRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the connected app to revoke, in the form `connectedApps/{id}`. |





 


<a name="metaxisdata-v1-ConnectedApp-State"></a>

### ConnectedApp.State
State is a derived value: it is computed from the stored grant, the MCP
surface&#39;s current configuration, and the user&#39;s account, so it is not stored.

| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNSPECIFIED | 0 |  |
| ACTIVE | 1 | Usable: not revoked or expired, and a refresh would be accepted. |
| REVOKED | 2 | Explicitly revoked. Outstanding access tokens are refused within the revocation cache&#39;s window. |
| EXPIRED | 3 | The refresh credential expired; no new access token can be minted. |
| REAUTH_REQUIRED | 4 | Re-authorization is required before the connection can work: the MCP resource identifier changed, or the grant predates the user&#39;s last password change. |
| INACTIVE | 5 | The user&#39;s account is deactivated. The connection resumes if the account is restored; it is not revoked automatically. |


 

 


<a name="metaxisdata-v1-ConnectedAppService"></a>

### ConnectedAppService
ConnectedAppService manages the MCP OAuth connections a user established by
authorizing a client against the deployment&#39;s MCP endpoint. One connected app
is one authorization (one user, one client), not one transport session: the
MCP endpoint itself is stateless.

No method here carries a (metaxisdata.v1.permission) annotation, because the
same call serves two audiences: a workspace administrator listing every
connection and an ordinary user managing their own. Each handler therefore
scopes its query and authorizes the widen itself, the way NotificationService
and UserService&#39;s self-service paths do. The permission catalog still defines
connectedApps.list and connectedApps.revoke, which are what the handler checks
before returning another user&#39;s rows or revoking another user&#39;s connection.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListConnectedApps | [ListConnectedAppsRequest](#metaxisdata-v1-ListConnectedAppsRequest) | [ListConnectedAppsResponse](#metaxisdata-v1-ListConnectedAppsResponse) | List the caller&#39;s connected apps. A caller holding metaxisdata.connectedApps.list sees the whole workspace; without it the result is forced to the caller&#39;s own rows. `user` narrows the result further when the caller may see other users&#39; connections. |
| RevokeConnectedApp | [RevokeConnectedAppRequest](#metaxisdata-v1-RevokeConnectedAppRequest) | [ConnectedApp](#metaxisdata-v1-ConnectedApp) | Revoke one connected app: its outstanding access tokens stop being accepted (immediately on this replica, within 30 seconds elsewhere, the same rule as Logout) and its refresh token can no longer mint a new pair. A user may revoke their own; revoking another user&#39;s needs metaxisdata.connectedApps.revoke. The response is the revoked record so the permanent audit ledger names what was revoked even after the row is pruned. Permissions required: None |

 



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






<a name="metaxisdata-v1-BatchUpdateInstanceResult"></a>

### BatchUpdateInstanceResult



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the instance. Format: instances/{instance} |
| instance | [Instance](#metaxisdata-v1-Instance) |  | The updated instance; unset when the update failed. |
| error | [string](#string) |  | Empty when the update succeeded; otherwise why this instance could not be updated. |






<a name="metaxisdata-v1-BatchUpdateInstancesRequest"></a>

### BatchUpdateInstancesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| requests | [UpdateInstanceRequest](#metaxisdata-v1-UpdateInstanceRequest) | repeated | The request message specifying the resources to update. A maximum of 1000 instances can be updated in a batch. |






<a name="metaxisdata-v1-BatchUpdateInstancesResponse"></a>

### BatchUpdateInstancesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| results | [BatchUpdateInstanceResult](#metaxisdata-v1-BatchUpdateInstanceResult) | repeated | One result per requested instance, in request order. Per-instance failures are reported here and do not abort the remaining instances. |






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
| ssh_host_key | [string](#string) |  | The trusted SSH host key(s) for ssh_host, one entry per line. An entry is either a fingerprint (SHA256:... or MD5:...) or a known_hosts / authorized_keys public key line, as printed by ssh-keyscan. Required when ssh_host is set: an empty value refuses the connection rather than accepting whatever host key answers, because the tunnel carries the database credentials in clear text. |
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
| enable_full_sync | [bool](#bool) | optional | Whether the sync also refreshes every existing database of the instance.

Unset means true: a user-triggered sync refreshes the metadata and every database, which is what the background scan and instance creation already do. Set it to false only for a discovery-only sync that refreshes the instance metadata and queues the databases that were just discovered, leaving the existing ones to the background scan. |






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



<a name="metaxisdata-v1-ApplicationSQLMetadata"></a>

### ApplicationSQLMetadata
ApplicationSQLMetadata is one statement an application reported, with every literal
already replaced by `?`. It is the metadata browser&#39;s view of the object; how often it
ran and which applications reported it are on the Application SQL page, because the
registry row is written once and those numbers change with every report.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| application_sql_id | [string](#string) |  |  |
| name | [string](#string) |  | A display label for a one-line context: the whole statement with every run of whitespace collapsed to a single space, and bounded. The full text is in sql_text. |
| sql_text | [string](#string) |  | The parameterized statement, exactly as it is stored. |
| instance_resource | [string](#string) |  | Format: instances/{instance} |
| database_name | [string](#string) |  |  |
| schema_name | [string](#string) |  |  |






<a name="metaxisdata-v1-BoundingBox"></a>

### BoundingBox
BoundingBox is the bounding box of a spatial index.


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
| guid | [string](#string) |  | The globally unique metadata GUID of the database, e.g. &#34;instance_1;db2&#34;. It can be used directly as an analysis scope for AnalyzeSQL. |






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






<a name="metaxisdata-v1-DimensionalConfig"></a>

### DimensionalConfig
DimensionalConfig holds dimensional parameters of a spatial index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| dimensions | [int32](#int32) |  | The number of dimensions, 2 to 4, default 2. |
| data_type | [string](#string) |  | The spatial data type, such as GEOMETRY or GEOGRAPHY. |
| operator_class | [string](#string) |  | The operator class. (PostgreSQL specific). |
| layer_gtype | [string](#string) |  | The geometry type constraint. (Oracle specific). |
| parallel_build | [bool](#bool) |  | Whether the index is built in parallel. |






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






<a name="metaxisdata-v1-GridLevel"></a>

### GridLevel
GridLevel is one grid level of a spatial tessellation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [int32](#int32) |  | The level, 1 to 4 for SQL Server. |
| density | [string](#string) |  | The density, such as LOW, MEDIUM or HIGH. |






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
| spatial_config | [SpatialIndexConfig](#metaxisdata-v1-SpatialIndexConfig) |  | The spatial index configuration. (SQL Server and other engines). |






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
| columns | [ColumnMetadata](#metaxisdata-v1-ColumnMetadata) | repeated | The ordered list of columns in the materialized view. |
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






<a name="metaxisdata-v1-SpatialIndexConfig"></a>

### SpatialIndexConfig
SpatialIndexConfig is the configuration of a spatial index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| method | [string](#string) |  | The index method, such as &#34;SPATIAL&#34; or &#34;GIST&#34;. |
| tessellation | [TessellationConfig](#metaxisdata-v1-TessellationConfig) |  | The tessellation configuration. (SQL Server specific). |
| storage | [StorageConfig](#metaxisdata-v1-StorageConfig) |  | The storage and performance parameters. |
| dimensional | [DimensionalConfig](#metaxisdata-v1-DimensionalConfig) |  | The dimensional parameters. |
| engine_specific | [SpatialIndexConfig.EngineSpecificEntry](#metaxisdata-v1-SpatialIndexConfig-EngineSpecificEntry) | repeated | Engine specific parameters. |






<a name="metaxisdata-v1-SpatialIndexConfig-EngineSpecificEntry"></a>

### SpatialIndexConfig.EngineSpecificEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-StorageConfig"></a>

### StorageConfig
StorageConfig holds storage and performance parameters of an index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| fillfactor | [int32](#int32) |  | PostgreSQL parameters. |
| buffering | [string](#string) |  |  |
| tablespace | [string](#string) |  | Oracle parameters. |
| work_tablespace | [string](#string) |  |  |
| sdo_level | [int32](#int32) |  |  |
| commit_interval | [int32](#int32) |  |  |
| pad_index | [bool](#bool) |  | SQL Server parameters. |
| sort_in_tempdb | [string](#string) |  |  |
| drop_existing | [bool](#bool) |  |  |
| online | [bool](#bool) |  |  |
| allow_row_locks | [bool](#bool) |  |  |
| allow_page_locks | [bool](#bool) |  |  |
| maxdop | [int32](#int32) |  |  |
| data_compression | [string](#string) |  |  |






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
| application_sql_metadata | [ApplicationSQLMetadata](#metaxisdata-v1-ApplicationSQLMetadata) |  |  |
| guid | [string](#string) |  | The globally unique metadata GUID of this object, e.g. &#34;instance_1;db2;schema3;table4&#34;. It is what ListMetadata results are addressed by in GetMetadata, GetSchemaString and the lineage methods. |






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






<a name="metaxisdata-v1-TessellationConfig"></a>

### TessellationConfig
TessellationConfig is the tessellation configuration of a spatial index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scheme | [string](#string) |  | The tessellation scheme, such as GEOMETRY_GRID or GEOGRAPHY_GRID. |
| bounding_box | [BoundingBox](#metaxisdata-v1-BoundingBox) |  | The bounding box. (SQL Server specific). |
| grid_levels | [GridLevel](#metaxisdata-v1-GridLevel) | repeated | The grid levels. (SQL Server specific). |
| cells_per_object | [int32](#int32) |  | The number of cells per object. (SQL Server specific). |






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
| APPLICATION_SQL | 19 |  |
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
| provider | [string](#string) |  |  |
| model | [string](#string) |  |  |
| cache_key | [string](#string) |  |  |
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
| progress | [ExplainSQLProgress](#metaxisdata-v1-ExplainSQLProgress) |  |  |





 

 

 


<a name="metaxisdata-v1-ExplainSQLService"></a>

### ExplainSQLService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ExplainSQL | [ExplainSQLRequest](#metaxisdata-v1-ExplainSQLRequest) | [ExplainSQLResponse](#metaxisdata-v1-ExplainSQLResponse) stream |  |

 



<a name="v1_data_quality_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/data_quality_service.proto



<a name="metaxisdata-v1-AcceptedValuesConfig"></a>

### AcceptedValuesConfig
AcceptedValuesConfig counts rows whose column value is outside a fixed set.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| values | [string](#string) | repeated | At most 200 values, encoded as strings and converted by the column&#39;s type. |
| allow_null | [bool](#bool) |  | When false (the default), a NULL value is a violation. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-AggregateConfig"></a>

### AggregateConfig
AggregateConfig bounds the value one aggregate over one column produces.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| function | [QualityAggregateFunction](#metaxisdata-v1-QualityAggregateFunction) |  |  |
| percentile | [double](#double) | optional | The percentile, 0-100, for QUALITY_AGGREGATE_FUNCTION_PERCENTILE. |
| min | [double](#double) | optional | At least one fail bound is required; warn bounds are optional and must sit inside the fail range. |
| max | [double](#double) | optional |  |
| warn_min | [double](#double) | optional |  |
| warn_max | [double](#double) | optional |  |
| inclusive | [bool](#bool) |  | Whether the bounds are inclusive. Default false, i.e. open on both ends. |






<a name="metaxisdata-v1-CelExpressionConfig"></a>

### CelExpressionConfig
CelExpressionConfig evaluates an expression over a sample of rows in Go. It is
v1.1; the message exists so that the config shape does not change then.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| expression | [string](#string) |  |  |
| columns | [string](#string) | repeated | The columns the expression reads. They must all exist on the target. |
| sample_rows | [int32](#int32) |  |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-ColumnProfile"></a>

### ColumnProfile
ColumnProfile is the cached deterministic statistics of one column plus how
they were measured.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the profiled table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| column | [string](#string) |  |  |
| profile | [ColumnProfileStats](#metaxisdata-v1-ColumnProfileStats) |  |  |
| sample_method | [string](#string) |  | How the values were sampled, e.g. &#34;full&#34; or &#34;sample&#34;. |
| sample_rows | [int64](#int64) |  |  |
| truncated | [bool](#bool) |  |  |
| engine_version | [string](#string) |  | The engine version that measured it, so a changed measurement is traceable. |
| profiled_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-ColumnProfileStats"></a>

### ColumnProfileStats
ColumnProfileStats is the deterministic statistics of one column, measured
without a model. It is the evidence a semantic label cites.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| total_rows | [int64](#int64) |  |  |
| null_count | [int64](#int64) |  |  |
| distinct_count | [int64](#int64) |  |  |
| distinct_estimated | [bool](#bool) |  | True when the database refused an exact distinct count and the value came from a sample. |
| min_value | [string](#string) |  | Display forms of the observed extremes, truncated; empty when the column has no comparable values. |
| max_value | [string](#string) |  |  |
| min_length | [int64](#int64) | optional | Unset for non-string columns. |
| max_length | [int64](#int64) | optional |  |
| top_values | [ProfileValue](#metaxisdata-v1-ProfileValue) | repeated | The most frequent values, at most ten, each truncated. |
| patterns | [string](#string) | repeated | Shape summaries such as &#34;numeric&#34;, &#34;alpha&#34;, &#34;date-like&#34;, &#34;mixed&#34;. |






<a name="metaxisdata-v1-ColumnSemanticLabel"></a>

### ColumnSemanticLabel
ColumnSemanticLabel is the semantic type a column is asserted to hold, who
asserted it and the evidence behind it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| column | [string](#string) |  |  |
| label | [QualitySemanticType](#metaxisdata-v1-QualitySemanticType) |  |  |
| source | [QualityLabelSource](#metaxisdata-v1-QualityLabelSource) |  |  |
| status | [QualityLabelStatus](#metaxisdata-v1-QualityLabelStatus) |  |  |
| confidence | [double](#double) |  |  |
| evidence | [SemanticLabelEvidence](#metaxisdata-v1-SemanticLabelEvidence) |  |  |
| accepted_by | [string](#string) |  | Who accepted it. Format: users/{user}. Empty for a suggestion. |
| accepted_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| update_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-ColumnValue"></a>

### ColumnValue
ColumnValue is one column of a sampled failed row. Values are truncated before
they are returned, so a wide text column cannot bloat the response.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-CreateQualityRuleRequest"></a>

### CreateQualityRuleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| rule | [QualityRule](#metaxisdata-v1-QualityRule) |  | The rule to create. Its name is assigned by the server. |






<a name="metaxisdata-v1-CrossColumnConfig"></a>

### CrossColumnConfig
CrossColumnConfig counts rows where two columns of the same row do not satisfy
the comparison.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| left_column | [string](#string) |  |  |
| right_column | [string](#string) |  |  |
| op | [QualityComparisonOperator](#metaxisdata-v1-QualityComparisonOperator) |  |  |
| tolerance | [double](#double) | optional | Tolerance subtracted/added to the comparison to absorb floating point noise. Only meaningful for numeric columns. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-CrossTableConfig"></a>

### CrossTableConfig
CrossTableConfig reconciles one aggregate of the rule&#39;s table with one
aggregate of another table in the same instance and database.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| right_table | [string](#string) |  | The other table as &#34;schema.table&#34; (PostgreSQL/MSSQL) or &#34;database.table&#34; / &#34;table&#34; (MySQL family); it must live in the rule target&#39;s database. |
| left_function | [QualityAggregateFunction](#metaxisdata-v1-QualityAggregateFunction) |  | The left (target) aggregate; COUNT ignores left_column. |
| left_column | [string](#string) |  |  |
| right_function | [QualityAggregateFunction](#metaxisdata-v1-QualityAggregateFunction) |  | The right (reference) aggregate; COUNT ignores right_column. |
| right_column | [string](#string) |  |  |
| tolerance | [double](#double) | optional | The absolute difference tolerated before the thresholds apply: the observed value is max(0, |left - right| - tolerance). |
| fail_threshold | [double](#double) | optional | Difference beyond the tolerance that fails, and the warning level inside it. The fail threshold defaults to zero. |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-CustomSQLConfig"></a>

### CustomSQLConfig
CustomSQLConfig counts the rows a read-only statement returns as failures. The
statement is single SELECT/WITH, at most 8 KiB, and is never rewritten.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sql | [string](#string) |  |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-DeleteQualityRuleRequest"></a>

### DeleteQualityRuleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |






<a name="metaxisdata-v1-ExplainQualityResultRequest"></a>

### ExplainQualityResultRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityRuleResults/{quality_rule_result} |
| language | [string](#string) |  | The language the explanation is written in, e.g. &#34;en&#34; or &#34;zh-CN&#34;. |
| force_regenerate | [bool](#bool) |  | Regenerate even when a cached explanation exists. |






<a name="metaxisdata-v1-ExplainQualityResultResponse"></a>

### ExplainQualityResultResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [string](#string) |  |  |
| progress | [ExplainSQLProgress](#metaxisdata-v1-ExplainSQLProgress) |  |  |
| metadata | [ExplainSQLMetadata](#metaxisdata-v1-ExplainSQLMetadata) |  |  |






<a name="metaxisdata-v1-FailureSample"></a>

### FailureSample
FailureSample is one failed row, already truncated.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| columns | [ColumnValue](#metaxisdata-v1-ColumnValue) | repeated |  |






<a name="metaxisdata-v1-FreshnessConfig"></a>

### FreshnessConfig
FreshnessConfig measures how long ago the newest timestamp is. A NULL newest
timestamp follows on_empty.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| timestamp_column | [string](#string) |  |  |
| max_lag_seconds | [int32](#int32) |  |  |
| warn_lag_seconds | [int32](#int32) | optional |  |






<a name="metaxisdata-v1-GetQualityIssueRequest"></a>

### GetQualityIssueRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityIssues/{quality_issue} |






<a name="metaxisdata-v1-GetQualityRuleRequest"></a>

### GetQualityRuleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |






<a name="metaxisdata-v1-GetQualityRunRequest"></a>

### GetQualityRunRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityRuns/{quality_run} |






<a name="metaxisdata-v1-GroupCheckConfig"></a>

### GroupCheckConfig
GroupCheckConfig bounds the row count of each group a set of columns forms.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| columns | [string](#string) | repeated | The grouping columns, 1-5 of them. |
| min_rows_per_group | [int64](#int64) | optional | At least one bound is required. |
| max_rows_per_group | [int64](#int64) | optional |  |
| fail_threshold | [double](#double) | optional | Groups outside the bounds tolerated, measured per threshold_metric. |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-ListColumnLabelsRequest"></a>

### ListColumnLabelsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of labels to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListColumnLabels call. Provide this to retrieve the subsequent page. |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| columns | [string](#string) | repeated | The columns to read; empty means every labeled column. |






<a name="metaxisdata-v1-ListColumnLabelsResponse"></a>

### ListColumnLabelsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column_labels | [ColumnSemanticLabel](#metaxisdata-v1-ColumnSemanticLabel) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListColumnProfilesRequest"></a>

### ListColumnProfilesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of profiles to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListColumnProfiles call. Provide this to retrieve the subsequent page. |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| columns | [string](#string) | repeated | The columns to read; empty means every profiled column. |






<a name="metaxisdata-v1-ListColumnProfilesResponse"></a>

### ListColumnProfilesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column_profiles | [ColumnProfile](#metaxisdata-v1-ColumnProfile) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListQualityIssuesRequest"></a>

### ListQualityIssuesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of issues to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListQualityIssues call. Provide this to retrieve the subsequent page. |
| status | [QualityIssueStatus](#metaxisdata-v1-QualityIssueStatus) |  |  |
| severity | [QualitySeverity](#metaxisdata-v1-QualitySeverity) |  |  |
| rule | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |
| target_guid | [string](#string) |  | The exact target GUID. |
| owner | [string](#string) |  | Only issues of rules owned by this principal. Format: users/{user} |
| start_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Only issues first seen at or after this time. |
| end_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Only issues first seen at or before this time. |






<a name="metaxisdata-v1-ListQualityIssuesResponse"></a>

### ListQualityIssuesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| quality_issues | [QualityIssue](#metaxisdata-v1-QualityIssue) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListQualityRuleResultsRequest"></a>

### ListQualityRuleResultsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of results to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListQualityRuleResults call. Provide this to retrieve the subsequent page. |
| run | [string](#string) |  | Format: workspaces/{workspace}/qualityRuns/{quality_run} |
| rule | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |
| status | [QualityStatus](#metaxisdata-v1-QualityStatus) |  |  |
| target_guid | [string](#string) |  | The exact target GUID. |
| start_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Only the results that started at or after this time. |
| end_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Only the results that started before this time. |






<a name="metaxisdata-v1-ListQualityRuleResultsResponse"></a>

### ListQualityRuleResultsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| quality_rule_results | [QualityRuleResult](#metaxisdata-v1-QualityRuleResult) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListQualityRulesRequest"></a>

### ListQualityRulesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of rules to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListQualityRules call. Provide this to retrieve the subsequent page. |
| instance | [string](#string) |  | The following four filters are GUID path prefixes, not name matches: a rule matches when its target GUID starts with the given prefix. &#34;instance_1&#34; therefore selects every rule in that instance, whatever database or table it watches. |
| database | [string](#string) |  |  |
| schema | [string](#string) |  |  |
| table | [string](#string) |  |  |
| target_guid | [string](#string) |  | The exact target GUID. |
| dimension | [QualityDimension](#metaxisdata-v1-QualityDimension) |  |  |
| rule_type | [QualityRuleType](#metaxisdata-v1-QualityRuleType) |  |  |
| severity | [QualitySeverity](#metaxisdata-v1-QualitySeverity) |  |  |
| state | [QualityRuleState](#metaxisdata-v1-QualityRuleState) |  |  |
| owner | [string](#string) |  | Format: users/{user}. |
| search | [string](#string) |  | A substring match over the title and description. |






<a name="metaxisdata-v1-ListQualityRulesResponse"></a>

### ListQualityRulesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| quality_rules | [QualityRule](#metaxisdata-v1-QualityRule) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListQualityRunsRequest"></a>

### ListQualityRunsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of runs to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListQualityRuns call. Provide this to retrieve the subsequent page. |
| status | [QualityRunStatus](#metaxisdata-v1-QualityRunStatus) |  | The run lifecycle to filter by, e.g. only runs still PENDING. |
| trigger | [QualityRunTrigger](#metaxisdata-v1-QualityRunTrigger) |  |  |
| instance | [string](#string) |  | Only the runs that executed rules of this instance. Format: instances/{instance} |
| start_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Only the runs started at or after this time. |
| end_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Only the runs started before this time. |
| initiator | [string](#string) |  | Format: users/{user}. |
| outcome | [QualityStatus](#metaxisdata-v1-QualityStatus) |  | The quality rollup to filter by, e.g. only failed runs. Matches completed runs only. |






<a name="metaxisdata-v1-ListQualityRunsResponse"></a>

### ListQualityRunsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| quality_runs | [QualityRun](#metaxisdata-v1-QualityRun) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListQualitySuggestionsRequest"></a>

### ListQualitySuggestionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of suggestions to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListQualitySuggestions call. Provide this to retrieve the subsequent page. |
| status | [QualitySuggestionStatus](#metaxisdata-v1-QualitySuggestionStatus) |  |  |
| kind | [QualitySuggestionKind](#metaxisdata-v1-QualitySuggestionKind) |  |  |
| source | [QualitySuggestionSource](#metaxisdata-v1-QualitySuggestionSource) |  |  |
| target_guid | [string](#string) |  | The exact target GUID. |






<a name="metaxisdata-v1-ListQualitySuggestionsResponse"></a>

### ListQualitySuggestionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| quality_suggestions | [QualitySuggestion](#metaxisdata-v1-QualitySuggestion) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-NullPercentConfig"></a>

### NullPercentConfig
NullPercentConfig bounds a column&#39;s nulls. Exactly one of the count pair and
the percentage pair carries the fail threshold; each pair&#39;s warn field is
optional and must not exceed the fail threshold.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| max_null_count | [int64](#int64) | optional |  |
| warn_null_count | [int64](#int64) | optional |  |
| max_null_pct | [double](#double) | optional |  |
| warn_null_pct | [double](#double) | optional |  |
| null_values | [string](#string) | repeated | Values that also count as missing, besides SQL NULL. Encoded as strings and converted with the column&#39;s type; at most 20. |






<a name="metaxisdata-v1-PreviewQualityRuleRequest"></a>

### PreviewQualityRuleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| rule | [QualityRule](#metaxisdata-v1-QualityRule) |  | The rule to validate, typically one that has not been saved yet. |






<a name="metaxisdata-v1-PreviewQualityRuleResponse"></a>

### PreviewQualityRuleResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| compiled_sql | [string](#string) |  | The SQL the rule compiles to, for the editor&#39;s query preview. |
| referenced_columns | [string](#string) | repeated | The target columns the compiled SQL reads. |
| parameters | [string](#string) | repeated | The filter values the compiler bound, encoded for display, in the order the placeholders appear in compiled_sql. |






<a name="metaxisdata-v1-ProfileColumnsRequest"></a>

### ProfileColumnsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| columns | [string](#string) | repeated | The columns to profile; empty means every column of the table. At most 50. |






<a name="metaxisdata-v1-ProfileColumnsResponse"></a>

### ProfileColumnsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column_profiles | [ColumnProfile](#metaxisdata-v1-ColumnProfile) | repeated |  |






<a name="metaxisdata-v1-ProfileValue"></a>

### ProfileValue
ProfileValue is one frequent value of a low-cardinality column.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [string](#string) |  |  |
| count | [int64](#int64) |  |  |






<a name="metaxisdata-v1-QualityFilter"></a>

### QualityFilter
QualityFilter is the AND filter a rule applies before observing. Values are
encoded as strings and converted by the compiler using the column&#39;s type; no
user text reaches SQL unparameterized.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| clauses | [QualityFilterClause](#metaxisdata-v1-QualityFilterClause) | repeated |  |






<a name="metaxisdata-v1-QualityFilterClause"></a>

### QualityFilterClause
QualityFilterClause is one comparison in a QualityFilter.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| op | [QualityFilterOperator](#metaxisdata-v1-QualityFilterOperator) |  |  |
| values | [string](#string) | repeated | Unused by IS_NULL / IS_NOT_NULL, at most 100 values for IN / NOT_IN. |






<a name="metaxisdata-v1-QualityIssue"></a>

### QualityIssue
QualityIssue is the durable problem behind a rule&#39;s failures. A rule has at
most one unresolved issue; a later failing run touches it instead of opening
another, and a resolved issue is never reopened.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityIssues/{quality_issue} |
| rule | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |
| target | [QualityTarget](#metaxisdata-v1-QualityTarget) |  | The target as it was when the issue was opened. |
| status | [QualityIssueStatus](#metaxisdata-v1-QualityIssueStatus) |  |  |
| severity | [QualitySeverity](#metaxisdata-v1-QualitySeverity) |  |  |
| first_seen_run | [string](#string) |  | The run that opened the issue. Unset when it was opened without a run. |
| last_seen_run | [string](#string) |  | The newest run that observed the problem again. |
| first_seen_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_seen_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| resolved_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the issue was resolved. Unset while it is open or acknowledged. |
| resolution_reason | [QualityResolutionReason](#metaxisdata-v1-QualityResolutionReason) |  |  |
| resolution_note | [string](#string) |  | The note recorded with the resolution. |
| acknowledged_by | [string](#string) |  | Who acknowledged it. Format: users/{user}. Empty when never acknowledged. |
| acknowledged_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| muted_until | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Issue-level mute: notifications are suppressed until it passes, while the rule keeps running and results keep being recorded. |
| occurrence_count | [int32](#int32) |  | How many runs observed the problem while the issue was unresolved. |






<a name="metaxisdata-v1-QualityResultSummary"></a>

### QualityResultSummary
QualityResultSummary is the compact projection of the latest result that a
rule list shows. It is not a stored entity.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| status | [QualityStatus](#metaxisdata-v1-QualityStatus) |  |  |
| observed_value | [double](#double) |  |  |
| observed_kind | [QualityObservedKind](#metaxisdata-v1-QualityObservedKind) |  |  |
| failed_rows | [int64](#int64) |  |  |
| total_rows | [int64](#int64) |  |  |
| run | [string](#string) |  | Format: workspaces/{workspace}/qualityRuns/{quality_run} |
| finish_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-QualityRule"></a>

### QualityRule
QualityRule is one data quality check.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |
| title | [string](#string) |  | The human-readable title, shown in lists and notifications. |
| description | [string](#string) |  |  |
| dimension | [QualityDimension](#metaxisdata-v1-QualityDimension) |  |  |
| rule_type | [QualityRuleType](#metaxisdata-v1-QualityRuleType) |  |  |
| target | [QualityTarget](#metaxisdata-v1-QualityTarget) |  |  |
| config | [QualityRuleConfig](#metaxisdata-v1-QualityRuleConfig) |  |  |
| severity | [QualitySeverity](#metaxisdata-v1-QualitySeverity) |  |  |
| state | [QualityRuleState](#metaxisdata-v1-QualityRuleState) |  |  |
| owner | [string](#string) |  | Format: users/{user}. Empty when the rule has no owner. |
| schedule_interval | [google.protobuf.Duration](#google-protobuf-Duration) |  | How often the rule runs, between 15m and 7d. Zero is rejected: pausing is how scheduling stops, and a newly created rule first runs on its next scheduled slot, not immediately. |
| revision | [int32](#int32) |  | The current revision. Every semantic edit increments it and appends a revision row. |
| target_schema_changed | [bool](#bool) |  | True when the target&#39;s current metadata hash differs from the hash recorded when the rule was saved, i.e. the target&#39;s schema changed underneath it. |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| update_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_result | [QualityResultSummary](#metaxisdata-v1-QualityResultSummary) |  | The most recent result, for the list view. It is not part of the rule and is only populated by reads, never persisted on write. |
| orphaned_reason | [string](#string) |  | Why the target can no longer be resolved, e.g. because the table was dropped. Empty while the target is healthy. |
| mute_until | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Rule-level mute. It only silences issue notifications; executions still run and results are still recorded. |
| tags | [string](#string) | repeated |  |






<a name="metaxisdata-v1-QualityRuleConfig"></a>

### QualityRuleConfig
QualityRuleConfig is the typed parameters of one rule. Exactly one arm of the
oneof is set, and it must correspond to the rule&#39;s type; the filter, the empty
behavior and the count/percent metric are shared by every type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| row_count | [RowCountConfig](#metaxisdata-v1-RowCountConfig) |  |  |
| row_count_change | [RowCountChangeConfig](#metaxisdata-v1-RowCountChangeConfig) |  |  |
| null_percent | [NullPercentConfig](#metaxisdata-v1-NullPercentConfig) |  |  |
| unique | [UniqueConfig](#metaxisdata-v1-UniqueConfig) |  |  |
| unique_combination | [UniqueCombinationConfig](#metaxisdata-v1-UniqueCombinationConfig) |  |  |
| accepted_values | [AcceptedValuesConfig](#metaxisdata-v1-AcceptedValuesConfig) |  |  |
| range | [RangeConfig](#metaxisdata-v1-RangeConfig) |  |  |
| regex | [RegexConfig](#metaxisdata-v1-RegexConfig) |  |  |
| string_length | [StringLengthConfig](#metaxisdata-v1-StringLengthConfig) |  |  |
| referential_integrity | [ReferentialIntegrityConfig](#metaxisdata-v1-ReferentialIntegrityConfig) |  |  |
| cross_column | [CrossColumnConfig](#metaxisdata-v1-CrossColumnConfig) |  |  |
| freshness | [FreshnessConfig](#metaxisdata-v1-FreshnessConfig) |  |  |
| custom_sql | [CustomSQLConfig](#metaxisdata-v1-CustomSQLConfig) |  |  |
| cel_expression | [CelExpressionConfig](#metaxisdata-v1-CelExpressionConfig) |  | v1.1. |
| schema_drift | [SchemaDriftConfig](#metaxisdata-v1-SchemaDriftConfig) |  | v1.1 first batch: metadata drift and grouped row-count bounds. |
| group_check | [GroupCheckConfig](#metaxisdata-v1-GroupCheckConfig) |  |  |
| aggregate | [AggregateConfig](#metaxisdata-v1-AggregateConfig) |  | v1.1 second batch: aggregates and cross-table reconciliation. |
| cross_table | [CrossTableConfig](#metaxisdata-v1-CrossTableConfig) |  |  |
| threshold_metric | [QualityThresholdMetric](#metaxisdata-v1-QualityThresholdMetric) |  |  |
| filter | [QualityFilter](#metaxisdata-v1-QualityFilter) |  |  |
| on_empty | [QualityEmptyBehavior](#metaxisdata-v1-QualityEmptyBehavior) |  |  |






<a name="metaxisdata-v1-QualityRuleDraftConfig"></a>

### QualityRuleDraftConfig
QualityRuleDraftConfig is one model-proposed rule, already validated into the
typed config. It is never executed without a human saving it as a rule.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [QualityRuleConfig](#metaxisdata-v1-QualityRuleConfig) |  |  |
| severity | [QualitySeverity](#metaxisdata-v1-QualitySeverity) |  |  |
| title | [string](#string) |  |  |
| rationale | [string](#string) |  |  |
| confidence | [double](#double) |  |  |






<a name="metaxisdata-v1-QualityRuleResult"></a>

### QualityRuleResult
QualityRuleResult is one rule&#39;s outcome in one run.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityRuleResults/{quality_rule_result} |
| run | [string](#string) |  | Format: workspaces/{workspace}/qualityRuns/{quality_run} |
| rule | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |
| rule_revision | [int32](#int32) |  | The rule revision this result was judged with. |
| rule_engine_version | [string](#string) |  | The engine version that produced it, so a changed judgment is traceable to a changed implementation. |
| target | [QualityTarget](#metaxisdata-v1-QualityTarget) |  | The target as it was when the result was produced. |
| status | [QualityStatus](#metaxisdata-v1-QualityStatus) |  |  |
| observed_value | [double](#double) |  | The observed value; its unit is observed_kind. |
| observed_kind | [QualityObservedKind](#metaxisdata-v1-QualityObservedKind) |  |  |
| thresholds | [QualityThresholds](#metaxisdata-v1-QualityThresholds) |  | The thresholds that were applied, snapshotted. |
| failed_rows | [int64](#int64) |  |  |
| total_rows | [int64](#int64) |  |  |
| sampled | [bool](#bool) |  | True when the observation was made over a sample rather than every row. |
| sample_rows | [int64](#int64) |  |  |
| truncated | [bool](#bool) |  | True when the samples were cut off by the sample limit. |
| samples | [FailureSample](#metaxisdata-v1-FailureSample) | repeated | Failed rows kept as evidence, already truncated per value. |
| executed_sql | [string](#string) |  | The SQL that was executed. Kept so a result can be re-checked by hand. |
| error_code | [string](#string) |  | A machine-readable failure class: CONNECTION, AUTH, TIMEOUT, PERMISSION, UNSUPPORTED, SQL_SYNTAX, BAD_DATA, LIMIT_EXCEEDED or ORPHANED. Empty unless status is ERROR. |
| error_message | [string](#string) |  | A human-readable failure message. Empty unless status is ERROR. |
| duration | [google.protobuf.Duration](#google-protobuf-Duration) |  |  |
| start_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| finish_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="metaxisdata-v1-QualityRun"></a>

### QualityRun
QualityRun is one execution batch: manually selected rules, or the rules of one
instance that became due in one scheduler tick.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualityRuns/{quality_run} |
| trigger | [QualityRunTrigger](#metaxisdata-v1-QualityRunTrigger) |  |  |
| status | [QualityRunStatus](#metaxisdata-v1-QualityRunStatus) |  | The run&#39;s lifecycle. A queued or running run reports PENDING / RUNNING here; its outcome is not known yet. |
| counts | [QualityRunCounts](#metaxisdata-v1-QualityRunCounts) |  |  |
| start_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| finish_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Unset while the run is queued or still executing. |
| initiator | [string](#string) |  | The principal that started the run. Format: users/{user}. Empty for a scheduled run. |
| instance | [string](#string) |  | The instance this batch belongs to. Format: instances/{instance} |
| outcome | [QualityStatus](#metaxisdata-v1-QualityStatus) |  | The rollup of the run&#39;s results: the worst status any result reported. Unset until the run completes; ERROR and FAIL stay distinct. |






<a name="metaxisdata-v1-QualityRunCounts"></a>

### QualityRunCounts
QualityRunCounts is the six-state tally of one run.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| total | [int32](#int32) |  |  |
| pass | [int32](#int32) |  |  |
| warn | [int32](#int32) |  |  |
| fail | [int32](#int32) |  |  |
| error | [int32](#int32) |  |  |
| skipped | [int32](#int32) |  |  |
| not_evaluated | [int32](#int32) |  |  |






<a name="metaxisdata-v1-QualitySuggestion"></a>

### QualitySuggestion
QualitySuggestion is one model-proposed rule draft or semantic label waiting
for a reviewer. Nothing here is a rule: accepting a suggestion goes through the
normal rule or label path.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualitySuggestions/{quality_suggestion} |
| kind | [QualitySuggestionKind](#metaxisdata-v1-QualitySuggestionKind) |  |  |
| source | [QualitySuggestionSource](#metaxisdata-v1-QualitySuggestionSource) |  |  |
| target | [QualityTarget](#metaxisdata-v1-QualityTarget) |  | The target the suggestion is about. |
| payload | [QualitySuggestionPayload](#metaxisdata-v1-QualitySuggestionPayload) |  |  |
| status | [QualitySuggestionStatus](#metaxisdata-v1-QualitySuggestionStatus) |  |  |
| provider | [string](#string) |  | The model that produced it. |
| model | [string](#string) |  |  |
| prompt_version | [string](#string) |  |  |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| decided_by | [string](#string) |  | Who decided it. Format: users/{user}. Empty while pending. |
| decide_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| decision_note | [string](#string) |  |  |






<a name="metaxisdata-v1-QualitySuggestionPayload"></a>

### QualitySuggestionPayload
QualitySuggestionPayload is the review-queue payload: a rule draft the reviewer
can load into the editor, or a semantic label awaiting confirmation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rule | [QualityRuleDraftConfig](#metaxisdata-v1-QualityRuleDraftConfig) |  |  |
| label | [SemanticLabelSuggestion](#metaxisdata-v1-SemanticLabelSuggestion) |  |  |






<a name="metaxisdata-v1-QualityTarget"></a>

### QualityTarget
QualityTarget is what a rule observes.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| meta_guid | [string](#string) |  | The globally unique metadata GUID of the target table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| path | [QualityTargetPath](#metaxisdata-v1-QualityTargetPath) |  | Where the target lived when the rule was saved. It is display metadata, not identity: the GUID and its meta type are the identity. |
| columns | [string](#string) | repeated | The columns the rule reads, as a snapshot for display. The column named in the config is authoritative; validation requires them to agree. |






<a name="metaxisdata-v1-QualityTargetPath"></a>

### QualityTargetPath
QualityTargetPath is the display snapshot of where a target lived when the
rule was saved.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [string](#string) |  |  |
| database | [string](#string) |  |  |
| schema | [string](#string) |  |  |
| object | [string](#string) |  |  |
| column | [string](#string) |  | Empty for a table-level rule. |






<a name="metaxisdata-v1-QualityThresholds"></a>

### QualityThresholds
QualityThresholds is the threshold snapshot a result records, so a historical
result can be explained after the rule&#39;s thresholds changed. For
GREATER_THAN / LESS_THAN only value fields are set; for the range comparisons
the lower and upper bounds are.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| warn_value | [double](#double) | optional | Unset means the rule has no warning level. |
| fail_value | [double](#double) | optional |  |
| warn_value_upper | [double](#double) | optional |  |
| fail_value_upper | [double](#double) | optional |  |
| comparison | [QualityComparison](#metaxisdata-v1-QualityComparison) |  |  |
| on_empty | [QualityEmptyBehavior](#metaxisdata-v1-QualityEmptyBehavior) |  |  |






<a name="metaxisdata-v1-RangeConfig"></a>

### RangeConfig
RangeConfig counts rows outside a bound pair. At least one bound is required;
the values are strings parsed with the column&#39;s type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| min | [string](#string) | optional |  |
| max | [string](#string) | optional |  |
| inclusive | [bool](#bool) |  | Whether both bounds are inclusive. Default false, i.e. the interval is open on both ends. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-ReferentialIntegrityConfig"></a>

### ReferentialIntegrityConfig
ReferentialIntegrityConfig counts child rows with no matching parent row. The
parent must live in the same instance, database and schema.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| child_column | [string](#string) |  |  |
| parent_table | [string](#string) |  | The parent table as &#34;schema.table&#34;. |
| parent_column | [string](#string) |  |  |
| allow_null | [bool](#bool) |  | When true, a NULL child value is not an orphan. Default false. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-RegexConfig"></a>

### RegexConfig
RegexConfig counts rows whose column value does not match a regular
expression. The pattern is Go RE2 syntax and is also translated for the
engine&#39;s own regex operator.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| pattern | [string](#string) |  |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-RowCountChangeConfig"></a>

### RowCountChangeConfig
RowCountChangeConfig bounds the change against the rule&#39;s last successful
observation. Without a recent baseline the result is NOT_EVALUATED.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| max_increase_pct | [double](#double) | optional |  |
| max_decrease_pct | [double](#double) | optional |  |
| warn_increase_pct | [double](#double) | optional |  |
| warn_decrease_pct | [double](#double) | optional |  |






<a name="metaxisdata-v1-RowCountConfig"></a>

### RowCountConfig
RowCountConfig bounds the number of rows. At least one bound is required; a
warn bound makes the rule report WARN before it reports FAIL.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| min_rows | [int64](#int64) | optional |  |
| max_rows | [int64](#int64) | optional |  |
| exact_rows | [int64](#int64) | optional | When set, the row count must equal it exactly. |
| warn_min_rows | [int64](#int64) | optional |  |
| warn_max_rows | [int64](#int64) | optional |  |






<a name="metaxisdata-v1-RunQualityRulesRequest"></a>

### RunQualityRulesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| rules | [string](#string) | repeated | The rules to run, at most 200. At least one of rules, instance and target_guid must be set. |
| instance | [string](#string) |  | Restrict the selection to the enabled rules of one instance. Format: instances/{instance} |
| target_guid | [string](#string) |  | Restrict the selection to the enabled rules that watch this table. |






<a name="metaxisdata-v1-RunQualityRulesResponse"></a>

### RunQualityRulesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| runs | [QualityRun](#metaxisdata-v1-QualityRun) | repeated | One run per instance in the selection. All of them are queued and pending when the response is returned; poll GetQualityRun for their outcome. |






<a name="metaxisdata-v1-SchemaDriftColumn"></a>

### SchemaDriftColumn
SchemaDriftColumn is one column of the snapshot a schema-drift rule expects.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| type | [string](#string) |  |  |
| nullable | [bool](#bool) |  |  |






<a name="metaxisdata-v1-SchemaDriftConfig"></a>

### SchemaDriftConfig
SchemaDriftConfig compares the live column list with the snapshot taken when
the rule was saved. The rule reads metadata only, so it costs the target
database nothing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| expected_columns | [SchemaDriftColumn](#metaxisdata-v1-SchemaDriftColumn) | repeated | The snapshot. The API fills it from the current columns when a rule is created without one, and the editor lets a user adjust it. |
| check_types | [bool](#bool) | optional | Whether a changed column type is drift. Unset means true: a type change is drift unless the rule author turns the check off. |
| check_order | [bool](#bool) |  | Whether a changed column order is drift. Default false. |
| allow_extra_columns | [bool](#bool) |  | Whether a column the snapshot does not name is drift. Default false. |
| fail_threshold | [double](#double) | optional | Drift items tolerated. The metric is always COUNT here. |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-SemanticLabelEvidence"></a>

### SemanticLabelEvidence
SemanticLabelEvidence is why a label was assigned. It cites the inputs the
labeler saw so a reviewer can check the reasoning without re-running it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| columns | [string](#string) | repeated | Columns whose values or context the label derives from. |
| metrics | [string](#string) | repeated | Profile facts such as &#34;null_rate=0.02&#34; or &#34;distinct=12&#34;. |
| lineage | [string](#string) | repeated | Lineage relations that motivated the label, e.g. &#34;join key of orders&#34;. |
| rationale | [string](#string) |  |  |






<a name="metaxisdata-v1-SemanticLabelSuggestion"></a>

### SemanticLabelSuggestion
SemanticLabelSuggestion is a model-proposed semantic label for one column.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| label | [QualitySemanticType](#metaxisdata-v1-QualitySemanticType) |  |  |
| confidence | [double](#double) |  |  |
| evidence | [SemanticLabelEvidence](#metaxisdata-v1-SemanticLabelEvidence) |  |  |
| suggested_rule_types | [QualityRuleType](#metaxisdata-v1-QualityRuleType) | repeated | The rule types the deterministic mapping would suggest for this label, in display order. The model may rank types but may not invent one. |






<a name="metaxisdata-v1-StringLengthConfig"></a>

### StringLengthConfig
StringLengthConfig counts rows whose string length is outside the bounds.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| min_len | [int64](#int64) | optional |  |
| max_len | [int64](#int64) | optional |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-SuggestColumnLabelsRequest"></a>

### SuggestColumnLabelsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| columns | [string](#string) | repeated | The columns to label; empty means every column of the table. At most 50. |
| language | [string](#string) |  | The language the evidence and rationale are written in, e.g. &#34;en&#34; or &#34;zh-CN&#34;. |






<a name="metaxisdata-v1-SuggestColumnLabelsResponse"></a>

### SuggestColumnLabelsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| quality_suggestions | [QualitySuggestion](#metaxisdata-v1-QualitySuggestion) | repeated | The queued label proposals, pending review. |






<a name="metaxisdata-v1-SuggestQualityRuleRequest"></a>

### SuggestQualityRuleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| columns | [string](#string) | repeated | The columns the intent is about; empty means the rule may use any column. |
| intent | [string](#string) |  | The natural-language intent, e.g. &#34;email must not be empty&#34;. |
| language | [string](#string) |  | The language the intent and the draft are written in, e.g. &#34;en&#34; or &#34;zh-CN&#34;. |






<a name="metaxisdata-v1-SuggestQualityRuleResponse"></a>

### SuggestQualityRuleResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| quality_suggestions | [QualitySuggestion](#metaxisdata-v1-QualitySuggestion) | repeated | The queued drafts, pending review. |






<a name="metaxisdata-v1-UniqueCombinationConfig"></a>

### UniqueCombinationConfig
UniqueCombinationConfig counts duplicated tuples across several columns, or
across every column of the table when all_columns is set.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| columns | [string](#string) | repeated |  |
| all_columns | [bool](#bool) |  | Use every column of the target table instead of the list above. That makes the rule a whole-row duplicate check. |
| count_nulls | [bool](#bool) |  |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-UniqueConfig"></a>

### UniqueConfig
UniqueConfig counts duplicated non-NULL values in one column. The threshold is
measured per threshold_metric; the default fail threshold is zero.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| count_nulls | [bool](#bool) |  | When true, NULL values are treated as a value: more than one NULL is a duplicate. The default is false, matching SQL uniqueness. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-v1-UpdateColumnLabelRequest"></a>

### UpdateColumnLabelRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| table_guid | [string](#string) |  | The globally unique metadata GUID of the table. |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| column | [string](#string) |  |  |
| label | [QualitySemanticType](#metaxisdata-v1-QualitySemanticType) |  | The label to accept. Required by ACCEPT and ignored by CLEAR. |
| action | [ColumnLabelAction](#metaxisdata-v1-ColumnLabelAction) |  |  |






<a name="metaxisdata-v1-UpdateColumnLabelResponse"></a>

### UpdateColumnLabelResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column_label | [ColumnSemanticLabel](#metaxisdata-v1-ColumnSemanticLabel) |  | The stored label, unset after CLEAR. |






<a name="metaxisdata-v1-UpdateQualityIssueRequest"></a>

### UpdateQualityIssueRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| issue | [QualityIssue](#metaxisdata-v1-QualityIssue) |  | The issue to update. Its name identifies it. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The fields to change: status (RESOLVED only), resolution_reason, resolution_note, acknowledged or muted_until. |
| acknowledged | [bool](#bool) |  | Whether to acknowledge the issue. Acknowledgement is an action rather than a stored field of QualityIssue, so the update mask path &#34;acknowledged&#34; reads this value; false is rejected because clearing an acknowledgement is not a supported transition. |






<a name="metaxisdata-v1-UpdateQualityRuleRequest"></a>

### UpdateQualityRuleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rule | [QualityRule](#metaxisdata-v1-QualityRule) |  | The rule to update. Its name identifies it; only the fields named by update_mask are written. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |






<a name="metaxisdata-v1-UpdateQualitySuggestionRequest"></a>

### UpdateQualitySuggestionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/qualitySuggestions/{quality_suggestion} |
| decision | [QualitySuggestionStatus](#metaxisdata-v1-QualitySuggestionStatus) |  | The decision: ACCEPTED or REJECTED. A suggestion starts PENDING and a decision is final. |
| decision_note | [string](#string) |  | Why it was decided, shown in the review history. |





 


<a name="metaxisdata-v1-ColumnLabelAction"></a>

### ColumnLabelAction
ColumnLabelAction is what UpdateColumnLabel does with one column&#39;s label.

| Name | Number | Description |
| ---- | ------ | ----------- |
| COLUMN_LABEL_ACTION_UNSPECIFIED | 0 |  |
| COLUMN_LABEL_ACTION_ACCEPT | 1 | Store the given semantic label as a manually accepted one. |
| COLUMN_LABEL_ACTION_CLEAR | 2 | Remove the column&#39;s label. |



<a name="metaxisdata-v1-QualityAggregateFunction"></a>

### QualityAggregateFunction
QualityAggregateFunction is the aggregate an aggregate or reconciliation rule
computes. COUNT is only meaningful for a cross-table reconciliation, and
PERCENTILE only for an aggregate rule.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED | 0 |  |
| QUALITY_AGGREGATE_FUNCTION_COUNT | 1 |  |
| QUALITY_AGGREGATE_FUNCTION_SUM | 2 |  |
| QUALITY_AGGREGATE_FUNCTION_AVG | 3 |  |
| QUALITY_AGGREGATE_FUNCTION_MIN | 4 |  |
| QUALITY_AGGREGATE_FUNCTION_MAX | 5 |  |
| QUALITY_AGGREGATE_FUNCTION_STDDEV | 6 |  |
| QUALITY_AGGREGATE_FUNCTION_PERCENTILE | 7 |  |



<a name="metaxisdata-v1-QualityComparison"></a>

### QualityComparison
QualityComparison is how the observed value is compared with a threshold.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_COMPARISON_UNSPECIFIED | 0 |  |
| QUALITY_COMPARISON_GREATER_THAN | 1 | The rule fails when the observed value is greater than the threshold: counts of invalid rows, ratios, and freshness lag. |
| QUALITY_COMPARISON_LESS_THAN | 2 | The rule fails when the observed value is less than the threshold. |
| QUALITY_COMPARISON_OUTSIDE_RANGE | 3 | The rule fails when the observed value falls outside [lower, upper]: row count bounds. |
| QUALITY_COMPARISON_INSIDE_RANGE | 4 | The rule fails when the observed value falls inside [lower, upper]: an accepted range expressed as a pass band. |



<a name="metaxisdata-v1-QualityComparisonOperator"></a>

### QualityComparisonOperator
QualityComparisonOperator is the SQL comparison a cross-column rule applies
between two columns of the same row.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_COMPARISON_OPERATOR_UNSPECIFIED | 0 |  |
| QUALITY_COMPARISON_OPERATOR_EQ | 1 |  |
| QUALITY_COMPARISON_OPERATOR_NE | 2 |  |
| QUALITY_COMPARISON_OPERATOR_LT | 3 |  |
| QUALITY_COMPARISON_OPERATOR_LTE | 4 |  |
| QUALITY_COMPARISON_OPERATOR_GT | 5 |  |
| QUALITY_COMPARISON_OPERATOR_GTE | 6 |  |



<a name="metaxisdata-v1-QualityDimension"></a>

### QualityDimension
QualityDimension is the data quality dimension a rule measures. It is display
metadata and is used for grouping.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_DIMENSION_UNSPECIFIED | 0 |  |
| QUALITY_DIMENSION_COMPLETENESS | 1 |  |
| QUALITY_DIMENSION_VALIDITY | 2 |  |
| QUALITY_DIMENSION_UNIQUENESS | 3 |  |
| QUALITY_DIMENSION_CONSISTENCY | 4 |  |
| QUALITY_DIMENSION_TIMELINESS | 5 |  |
| QUALITY_DIMENSION_ACCURACY | 6 |  |



<a name="metaxisdata-v1-QualityEmptyBehavior"></a>

### QualityEmptyBehavior
QualityEmptyBehavior is what a rule does when the query succeeded but produced
nothing to judge: no rows at all, a NULL timestamp, no baseline to compare
with. The default is NOT_EVALUATED so an empty table never looks like a pass.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_EMPTY_BEHAVIOR_UNSPECIFIED | 0 |  |
| QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED | 1 |  |
| QUALITY_EMPTY_BEHAVIOR_PASS | 2 |  |
| QUALITY_EMPTY_BEHAVIOR_FAIL | 3 |  |



<a name="metaxisdata-v1-QualityFilterOperator"></a>

### QualityFilterOperator
QualityFilterOperator is one comparison in a rule&#39;s AND filter.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_FILTER_OPERATOR_UNSPECIFIED | 0 |  |
| QUALITY_FILTER_OPERATOR_EQ | 1 |  |
| QUALITY_FILTER_OPERATOR_NE | 2 |  |
| QUALITY_FILTER_OPERATOR_LT | 3 |  |
| QUALITY_FILTER_OPERATOR_LTE | 4 |  |
| QUALITY_FILTER_OPERATOR_GT | 5 |  |
| QUALITY_FILTER_OPERATOR_GTE | 6 |  |
| QUALITY_FILTER_OPERATOR_IN | 7 |  |
| QUALITY_FILTER_OPERATOR_NOT_IN | 8 |  |
| QUALITY_FILTER_OPERATOR_IS_NULL | 9 |  |
| QUALITY_FILTER_OPERATOR_IS_NOT_NULL | 10 |  |



<a name="metaxisdata-v1-QualityIssueStatus"></a>

### QualityIssueStatus
QualityIssueStatus is the lifecycle of a persistent problem behind a rule&#39;s
failures. RESOLVED is terminal: the next failing run opens a new issue rather
than reopening one.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_ISSUE_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_ISSUE_STATUS_OPEN | 1 |  |
| QUALITY_ISSUE_STATUS_ACKNOWLEDGED | 2 |  |
| QUALITY_ISSUE_STATUS_RESOLVED | 3 |  |



<a name="metaxisdata-v1-QualityLabelSource"></a>

### QualityLabelSource
QualityLabelSource is who asserted a column&#39;s semantic label.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_LABEL_SOURCE_UNSPECIFIED | 0 |  |
| QUALITY_LABEL_SOURCE_MANUAL | 1 |  |
| QUALITY_LABEL_SOURCE_LLM | 2 |  |



<a name="metaxisdata-v1-QualityLabelStatus"></a>

### QualityLabelStatus
QualityLabelStatus is whether a semantic label is in effect.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_LABEL_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_LABEL_STATUS_SUGGESTED | 1 |  |
| QUALITY_LABEL_STATUS_ACCEPTED | 2 |  |
| QUALITY_LABEL_STATUS_REJECTED | 3 |  |



<a name="metaxisdata-v1-QualityObservedKind"></a>

### QualityObservedKind
QualityObservedKind is the unit of the observed value a result records.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_OBSERVED_KIND_UNSPECIFIED | 0 |  |
| QUALITY_OBSERVED_KIND_COUNT | 1 |  |
| QUALITY_OBSERVED_KIND_RATIO | 2 |  |
| QUALITY_OBSERVED_KIND_SECONDS | 3 |  |
| QUALITY_OBSERVED_KIND_TIMESTAMP | 4 |  |
| QUALITY_OBSERVED_KIND_NUMBER | 5 |  |



<a name="metaxisdata-v1-QualityResolutionReason"></a>

### QualityResolutionReason
QualityResolutionReason is why an issue was closed.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_RESOLUTION_REASON_UNSPECIFIED | 0 |  |
| QUALITY_RESOLUTION_REASON_FIXED | 1 |  |
| QUALITY_RESOLUTION_REASON_EXPECTED | 2 |  |
| QUALITY_RESOLUTION_REASON_NO_ACTION_NEEDED | 3 |  |
| QUALITY_RESOLUTION_REASON_DUPLICATE | 4 |  |
| QUALITY_RESOLUTION_REASON_AUTO_RECOVERED | 5 |  |



<a name="metaxisdata-v1-QualityRuleState"></a>

### QualityRuleState
QualityRuleState is whether a rule is scheduled. ARCHIVED is the soft-delete
state: the rule, its revisions and results stay readable.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_RULE_STATE_UNSPECIFIED | 0 |  |
| QUALITY_RULE_STATE_ENABLED | 1 |  |
| QUALITY_RULE_STATE_PAUSED | 2 |  |
| QUALITY_RULE_STATE_ARCHIVED | 3 |  |



<a name="metaxisdata-v1-QualityRuleType"></a>

### QualityRuleType
QualityRuleType is the observation a rule makes. The value is immutable for a
rule: changing the type is a different rule and a new revision.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_RULE_TYPE_UNSPECIFIED | 0 |  |
| QUALITY_RULE_TYPE_ROW_COUNT | 1 |  |
| QUALITY_RULE_TYPE_ROW_COUNT_CHANGE | 2 |  |
| QUALITY_RULE_TYPE_NULL_PERCENT | 3 |  |
| QUALITY_RULE_TYPE_UNIQUE | 4 |  |
| QUALITY_RULE_TYPE_UNIQUE_COMBINATION | 5 |  |
| QUALITY_RULE_TYPE_ACCEPTED_VALUES | 6 |  |
| QUALITY_RULE_TYPE_RANGE | 7 |  |
| QUALITY_RULE_TYPE_REGEX | 8 |  |
| QUALITY_RULE_TYPE_STRING_LENGTH | 9 |  |
| QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY | 10 |  |
| QUALITY_RULE_TYPE_CROSS_COLUMN | 11 |  |
| QUALITY_RULE_TYPE_FRESHNESS | 12 |  |
| QUALITY_RULE_TYPE_CUSTOM_SQL | 13 |  |
| QUALITY_RULE_TYPE_CEL_EXPRESSION | 14 | v1.1. The value is reserved so that the v1 and store enum definitions stay value-compatible; this API never accepts it yet. |
| QUALITY_RULE_TYPE_SCHEMA_DRIFT | 15 | v1.1: compares the live column list with a snapshot taken when the rule was saved. It reads metadata only and never queries the target database. |
| QUALITY_RULE_TYPE_GROUP_CHECK | 16 | v1.1: groups rows and bounds each group&#39;s row count. |
| QUALITY_RULE_TYPE_AGGREGATE | 17 | v1.1: aggregates one column and bounds the value. |
| QUALITY_RULE_TYPE_CROSS_TABLE | 18 | v1.1: reconciles one aggregate of this table with one of another table in the same instance and database. |



<a name="metaxisdata-v1-QualityRunStatus"></a>

### QualityRunStatus
QualityRunStatus is the lifecycle of a run, which is separate from the quality
status its results roll up to. A queued run has no outcome yet.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_RUN_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_RUN_STATUS_PENDING | 1 | Created and waiting for the runner; clients poll while they see this. |
| QUALITY_RUN_STATUS_RUNNING | 2 | The runner is executing this run&#39;s rules. |
| QUALITY_RUN_STATUS_COMPLETED | 3 | Every rule was attempted; outcome holds the rollup. |
| QUALITY_RUN_STATUS_ABANDONED | 4 | A restart discarded the run before it finished. It is not retried. |



<a name="metaxisdata-v1-QualityRunTrigger"></a>

### QualityRunTrigger
QualityRunTrigger is what started a run.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_RUN_TRIGGER_UNSPECIFIED | 0 |  |
| QUALITY_RUN_TRIGGER_MANUAL | 1 |  |
| QUALITY_RUN_TRIGGER_SCHEDULED | 2 |  |



<a name="metaxisdata-v1-QualitySemanticType"></a>

### QualitySemanticType
QualitySemanticType is the closed vocabulary a column may be labeled with. It
is the input to the deterministic semantic-type-to-rule mapping, so a model may
only choose from these values.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SEMANTIC_TYPE_UNSPECIFIED | 0 |  |
| QUALITY_SEMANTIC_TYPE_EMAIL | 1 |  |
| QUALITY_SEMANTIC_TYPE_PHONE | 2 |  |
| QUALITY_SEMANTIC_TYPE_URL | 3 |  |
| QUALITY_SEMANTIC_TYPE_UUID | 4 |  |
| QUALITY_SEMANTIC_TYPE_IP | 5 |  |
| QUALITY_SEMANTIC_TYPE_COUNTRY_CODE | 6 |  |
| QUALITY_SEMANTIC_TYPE_CURRENCY | 7 |  |
| QUALITY_SEMANTIC_TYPE_AMOUNT | 8 |  |
| QUALITY_SEMANTIC_TYPE_QUANTITY | 9 |  |
| QUALITY_SEMANTIC_TYPE_PERCENTAGE | 10 |  |
| QUALITY_SEMANTIC_TYPE_ID | 11 |  |
| QUALITY_SEMANTIC_TYPE_BUSINESS_KEY | 12 |  |
| QUALITY_SEMANTIC_TYPE_TIMESTAMP | 13 |  |
| QUALITY_SEMANTIC_TYPE_DATE | 14 |  |
| QUALITY_SEMANTIC_TYPE_ENUM | 15 |  |
| QUALITY_SEMANTIC_TYPE_FREE_TEXT | 16 |  |
| QUALITY_SEMANTIC_TYPE_UNKNOWN | 17 |  |



<a name="metaxisdata-v1-QualitySeverity"></a>

### QualitySeverity
QualitySeverity is how much an issue on this rule matters.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SEVERITY_UNSPECIFIED | 0 |  |
| QUALITY_SEVERITY_WARNING | 1 |  |
| QUALITY_SEVERITY_ERROR | 2 |  |
| QUALITY_SEVERITY_CRITICAL | 3 |  |



<a name="metaxisdata-v1-QualityStatus"></a>

### QualityStatus
QualityStatus is the outcome of one rule execution. ERROR is not FAIL: ERROR
means the check could not be evaluated at all, FAIL means the data failed it.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_STATUS_PASS | 1 |  |
| QUALITY_STATUS_WARN | 2 |  |
| QUALITY_STATUS_FAIL | 3 |  |
| QUALITY_STATUS_ERROR | 4 |  |
| QUALITY_STATUS_SKIPPED | 5 |  |
| QUALITY_STATUS_NOT_EVALUATED | 6 |  |



<a name="metaxisdata-v1-QualitySuggestionKind"></a>

### QualitySuggestionKind
QualitySuggestionKind is what a suggestion proposes.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_KIND_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_KIND_RULE_DRAFT | 1 |  |
| QUALITY_SUGGESTION_KIND_LABEL | 2 |  |



<a name="metaxisdata-v1-QualitySuggestionSource"></a>

### QualitySuggestionSource
QualitySuggestionSource is what produced a suggestion.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_SOURCE_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE | 1 |  |
| QUALITY_SUGGESTION_SOURCE_PROFILE | 2 |  |



<a name="metaxisdata-v1-QualitySuggestionStatus"></a>

### QualitySuggestionStatus
QualitySuggestionStatus is where a suggestion is in the review queue.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_STATUS_PENDING | 1 |  |
| QUALITY_SUGGESTION_STATUS_ACCEPTED | 2 |  |
| QUALITY_SUGGESTION_STATUS_REJECTED | 3 |  |



<a name="metaxisdata-v1-QualityThresholdMetric"></a>

### QualityThresholdMetric
QualityThresholdMetric selects what a count-type rule&#39;s threshold is compared
against: the number of failed rows, or failed rows divided by total rows.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_THRESHOLD_METRIC_UNSPECIFIED | 0 |  |
| QUALITY_THRESHOLD_METRIC_COUNT | 1 |  |
| QUALITY_THRESHOLD_METRIC_PERCENT | 2 |  |


 

 


<a name="metaxisdata-v1-DataQualityService"></a>

### DataQualityService
DataQualityService manages the checks that watch the data itself: rules,
their executions, and the per-rule results of those executions.

A rule observes one target — a table, or one column of it — with a typed
check (row count, null percent, uniqueness, accepted values, ...) on a
schedule or on demand. A run groups the results of one instance; it is
created asynchronously and polled until it finishes.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListQualityRules | [ListQualityRulesRequest](#metaxisdata-v1-ListQualityRulesRequest) | [ListQualityRulesResponse](#metaxisdata-v1-ListQualityRulesResponse) | List rules, newest first. |
| GetQualityRule | [GetQualityRuleRequest](#metaxisdata-v1-GetQualityRuleRequest) | [QualityRule](#metaxisdata-v1-QualityRule) | Read one rule, including its current revision and a summary of its most recent result. |
| CreateQualityRule | [CreateQualityRuleRequest](#metaxisdata-v1-CreateQualityRuleRequest) | [QualityRule](#metaxisdata-v1-QualityRule) | Create a rule. The rule is validated and compiled before it is stored; a rejected rule is never persisted. |
| UpdateQualityRule | [UpdateQualityRuleRequest](#metaxisdata-v1-UpdateQualityRuleRequest) | [QualityRule](#metaxisdata-v1-QualityRule) | Update a rule. Every semantic edit appends a revision, so a historical result always stays explainable by the definition that produced it. |
| DeleteQualityRule | [DeleteQualityRuleRequest](#metaxisdata-v1-DeleteQualityRuleRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete a rule. The rule, its revisions and results are only archived, never physically removed, so history stays readable. |
| PreviewQualityRule | [PreviewQualityRuleRequest](#metaxisdata-v1-PreviewQualityRuleRequest) | [PreviewQualityRuleResponse](#metaxisdata-v1-PreviewQualityRuleResponse) | Validate and compile a rule without saving it: config validation, filter validation and a `LIMIT 0` probe against the target&#39;s read-only data source. It writes nothing and creates no run. |
| RunQualityRules | [RunQualityRulesRequest](#metaxisdata-v1-RunQualityRulesRequest) | [RunQualityRulesResponse](#metaxisdata-v1-RunQualityRulesResponse) | Execute rules now. The selected rules are grouped by instance, one run is created per instance, and the runs are queued; the response returns them while they are still pending. Clients poll GetQualityRun. |
| ListQualityRuns | [ListQualityRunsRequest](#metaxisdata-v1-ListQualityRunsRequest) | [ListQualityRunsResponse](#metaxisdata-v1-ListQualityRunsResponse) | List runs, newest first. |
| GetQualityRun | [GetQualityRunRequest](#metaxisdata-v1-GetQualityRunRequest) | [QualityRun](#metaxisdata-v1-QualityRun) | Read one run with its six-state tallies. |
| ListQualityRuleResults | [ListQualityRuleResultsRequest](#metaxisdata-v1-ListQualityRuleResultsRequest) | [ListQualityRuleResultsResponse](#metaxisdata-v1-ListQualityRuleResultsResponse) | List the per-rule results of runs, typically filtered by run. |
| ListQualityIssues | [ListQualityIssuesRequest](#metaxisdata-v1-ListQualityIssuesRequest) | [ListQualityIssuesResponse](#metaxisdata-v1-ListQualityIssuesResponse) | List quality issues, newest first. |
| GetQualityIssue | [GetQualityIssueRequest](#metaxisdata-v1-GetQualityIssueRequest) | [QualityIssue](#metaxisdata-v1-QualityIssue) | Read one quality issue. |
| UpdateQualityIssue | [UpdateQualityIssueRequest](#metaxisdata-v1-UpdateQualityIssueRequest) | [QualityIssue](#metaxisdata-v1-QualityIssue) | Acknowledge, resolve or mute one quality issue. |
| ListQualitySuggestions | [ListQualitySuggestionsRequest](#metaxisdata-v1-ListQualitySuggestionsRequest) | [ListQualitySuggestionsResponse](#metaxisdata-v1-ListQualitySuggestionsResponse) | List the review queue of model-proposed rule drafts and semantic labels, newest first. |
| UpdateQualitySuggestion | [UpdateQualitySuggestionRequest](#metaxisdata-v1-UpdateQualitySuggestionRequest) | [QualitySuggestion](#metaxisdata-v1-QualitySuggestion) | Accept or reject one queued suggestion. A decision is final: the store refuses a second one. |
| ListColumnProfiles | [ListColumnProfilesRequest](#metaxisdata-v1-ListColumnProfilesRequest) | [ListColumnProfilesResponse](#metaxisdata-v1-ListColumnProfilesResponse) | List the cached deterministic profiles of columns. Profiles are read-only measurements; there is no get or update. |
| ListColumnLabels | [ListColumnLabelsRequest](#metaxisdata-v1-ListColumnLabelsRequest) | [ListColumnLabelsResponse](#metaxisdata-v1-ListColumnLabelsResponse) | List the semantic labels asserted on columns. |
| UpdateColumnLabel | [UpdateColumnLabelRequest](#metaxisdata-v1-UpdateColumnLabelRequest) | [UpdateColumnLabelResponse](#metaxisdata-v1-UpdateColumnLabelResponse) | Accept a proposed semantic label or clear the label of one column. |
| ProfileColumns | [ProfileColumnsRequest](#metaxisdata-v1-ProfileColumnsRequest) | [ProfileColumnsResponse](#metaxisdata-v1-ProfileColumnsResponse) | Measure the deterministic statistics of up to 50 columns of one table. |
| SuggestQualityRule | [SuggestQualityRuleRequest](#metaxisdata-v1-SuggestQualityRuleRequest) | [SuggestQualityRuleResponse](#metaxisdata-v1-SuggestQualityRuleResponse) | Turn a natural-language intent into queued rule drafts. |
| SuggestColumnLabels | [SuggestColumnLabelsRequest](#metaxisdata-v1-SuggestColumnLabelsRequest) | [SuggestColumnLabelsResponse](#metaxisdata-v1-SuggestColumnLabelsResponse) | Propose semantic labels for up to 50 columns of one table. |
| ExplainQualityResult | [ExplainQualityResultRequest](#metaxisdata-v1-ExplainQualityResultRequest) | [ExplainQualityResultResponse](#metaxisdata-v1-ExplainQualityResultResponse) stream | Explain one failed result as a stream of hypotheses.

No (google.api.http) annotation: this is a server-streaming method, and the REST gateway cannot carry one. The browser reaches the Connect endpoint directly, the way it does for ExplainSQL. |

 



<a name="v1_environment_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/environment_service.proto



<a name="metaxisdata-v1-CreateEnvironmentRequest"></a>

### CreateEnvironmentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| environment | [Environment](#metaxisdata-v1-Environment) |  | The environment to create. The title is required; name and color are ignored on create (the server derives them). |






<a name="metaxisdata-v1-DeleteEnvironmentRequest"></a>

### DeleteEnvironmentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The environment resource name. Format: environments/{id}. |






<a name="metaxisdata-v1-Environment"></a>

### Environment



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name. Format: environments/{id}. Output only: the id is derived from the title on create and cannot be changed afterwards. |
| title | [string](#string) |  | The display name, e.g. &#34;Production&#34;. |
| color | [string](#string) |  | A preset palette key controlling the badge color. One of: slate, blue, green, amber, orange, red, violet, pink. Empty on create lets the server pick a stable default. |
| tags | [Environment.TagsEntry](#metaxisdata-v1-Environment-TagsEntry) | repeated | Free-form labels attached to the environment. |
| instance_count | [int32](#int32) |  | The number of live instances assigned to this environment. Output only, and populated by ListEnvironments only. DeleteEnvironment is refused while it is non-zero. |






<a name="metaxisdata-v1-Environment-TagsEntry"></a>

### Environment.TagsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-v1-ListEnvironmentsRequest"></a>

### ListEnvironmentsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListEnvironmentsResponse"></a>

### ListEnvironmentsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| environments | [Environment](#metaxisdata-v1-Environment) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-UpdateEnvironmentRequest"></a>

### UpdateEnvironmentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| environment | [Environment](#metaxisdata-v1-Environment) |  | The environment to update. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The fields to update: title, color, tags. |





 

 

 


<a name="metaxisdata-v1-EnvironmentService"></a>

### EnvironmentService
EnvironmentService manages the workspace&#39;s environments. An environment is a
small, workspace-scoped resource (&#34;environments/{id}&#34;) that instances are
tagged with; databases inherit their instance&#39;s environment.

Environments are stored in the ENVIRONMENT workspace setting, not in a table
of their own. The id is derived from the title on create and is immutable:
instances store the raw id, so renaming only changes the display title.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListEnvironments | [ListEnvironmentsRequest](#metaxisdata-v1-ListEnvironmentsRequest) | [ListEnvironmentsResponse](#metaxisdata-v1-ListEnvironmentsResponse) | List the workspace environments.

Readable by every workspace member: the instance forms and the metadata filters need the list, and those pages are open to every member. |
| CreateEnvironment | [CreateEnvironmentRequest](#metaxisdata-v1-CreateEnvironmentRequest) | [Environment](#metaxisdata-v1-Environment) | Create an environment. |
| UpdateEnvironment | [UpdateEnvironmentRequest](#metaxisdata-v1-UpdateEnvironmentRequest) | [Environment](#metaxisdata-v1-Environment) | Update an environment. Only title, color and tags are mutable; the resource name (the id) is not. |
| DeleteEnvironment | [DeleteEnvironmentRequest](#metaxisdata-v1-DeleteEnvironmentRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete an environment. An environment still assigned to a live instance cannot be deleted. |

 



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
| members | [string](#string) | repeated | The principals requesting access. For users: users/{userUID}; for groups: groups/{email}; the pseudo-member allUsers matches every authenticated principal, including the ones who register later, so it is accepted only on the roles/workspaceMember baseline binding. That binding is server-managed: it cannot be edited or dropped, and a Set whose policy omits it is rejected. |
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

 



<a name="v1_identity_provider_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/identity_provider_service.proto



<a name="metaxisdata-v1-CreateIdentityProviderRequest"></a>

### CreateIdentityProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identity_provider | [IdentityProvider](#metaxisdata-v1-IdentityProvider) |  | The identity provider to create. |
| identity_provider_id | [string](#string) |  | The id to use, which becomes the final segment of the resource name. Format: a lower-case letter followed by up to 62 lower-case letters, digits or hyphens. |






<a name="metaxisdata-v1-DeleteIdentityProviderBindingRequest"></a>

### DeleteIdentityProviderBindingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [string](#string) |  | The account to unbind. Format: users/{user} |
| idp | [string](#string) |  | The identity provider to unbind from. Format: idps/{idp} |
| subject | [string](#string) |  | The subject to unbind. The binding row is keyed by (idp, subject), so this pins exactly one row: an account may in principle carry several subjects from the same provider. It is a plain field rather than a segment of the resource name because a subject is an opaque provider string that can contain `/`, `@` and `:`. |






<a name="metaxisdata-v1-DeleteIdentityProviderRequest"></a>

### DeleteIdentityProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the identity provider to delete. Format: idps/{idp} |






<a name="metaxisdata-v1-FieldMapping"></a>

### FieldMapping
FieldMapping names the claims that carry each field of the identity.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identifier | [string](#string) |  | The claim holding the identifier, normally the address. Required. |
| display_name | [string](#string) |  | The claim holding the display name. Optional; defaults to the identifier. |
| phone | [string](#string) |  | The claim holding the phone number. Optional. |
| subject | [string](#string) |  | The claim holding the provider-assigned, immutable subject. Optional here but required in effect: a login is bound to it, and the server refuses a provider that maps no subject unless the workspace lists the provider as one whose address claim may stand in for it (`sso_email_identity_idps`). |
| groups | [string](#string) |  | The claim holding group memberships. Optional. When set, each login syncs the account&#39;s group membership to the matching workspace groups, which is what lets an IAM policy grant roles by group. |






<a name="metaxisdata-v1-GetIdentityProviderRequest"></a>

### GetIdentityProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the identity provider to get. Format: idps/{idp} |






<a name="metaxisdata-v1-IdentityProvider"></a>

### IdentityProvider
IdentityProvider is a configured SSO provider.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name. Format: idps/{idp}. Output only: the id is set on create and cannot change, because accounts are bound to (id, subject) pairs. |
| title | [string](#string) |  | The display title, e.g. &#34;Acme SSO&#34;. The login page shows it on the button. |
| domain | [string](#string) |  | The email domain whose users this provider serves, e.g. &#34;example.com&#34;. Used as a fallback when a provider reports an identifier that is not an address: the login then tries &#34;{identifier}@{domain}&#34;. Empty disables the fallback. |
| type | [IdentityProviderType](#metaxisdata-v1-IdentityProviderType) |  | The protocol. Only OAUTH2 is implemented; the login path rejects anything else and no other value can be created. |
| config | [IdentityProviderConfig](#metaxisdata-v1-IdentityProviderConfig) |  | The protocol configuration. |






<a name="metaxisdata-v1-IdentityProviderBinding"></a>

### IdentityProviderBinding
IdentityProviderBinding is one account&#39;s way in through one identity provider.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [string](#string) |  | The account. Format: users/{user} |
| user_email | [string](#string) |  | The account&#39;s address, for display. Read-only. |
| user_title | [string](#string) |  | The account&#39;s display name, for display. Read-only. |
| idp | [string](#string) |  | The identity provider. Format: idps/{idp} |
| subject | [string](#string) |  | The provider-assigned subject the account is bound to. This is the value a login resolves against; it is not necessarily the address. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the binding was written. |






<a name="metaxisdata-v1-IdentityProviderConfig"></a>

### IdentityProviderConfig



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| oauth2_config | [OAuth2IdentityProviderConfig](#metaxisdata-v1-OAuth2IdentityProviderConfig) |  |  |






<a name="metaxisdata-v1-IdentityProviderUserInfo"></a>

### IdentityProviderUserInfo
IdentityProviderUserInfo is the identity a login resolved from the claims.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identifier | [string](#string) |  | The identifier claim, normally the address. |
| subject | [string](#string) |  | The subject claim: the value the account is bound to. |
| display_name | [string](#string) |  | The display name. |
| phone | [string](#string) |  | The phone number. |
| groups | [string](#string) | repeated | The group claims. |
| has_groups | [bool](#bool) |  | Whether the provider returned a group claim at all. Distinguishes &#34;no groups&#34; from &#34;groups not configured&#34;, so an empty list does not clear a membership. |






<a name="metaxisdata-v1-ListIdentityProviderBindingsRequest"></a>

### ListIdentityProviderBindingsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| idp | [string](#string) |  | Optional. Restrict the answer to one identity provider. Format: idps/{idp} |
| user | [string](#string) |  | Optional. Restrict the answer to one account. Format: users/{user} |






<a name="metaxisdata-v1-ListIdentityProviderBindingsResponse"></a>

### ListIdentityProviderBindingsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identity_provider_bindings | [IdentityProviderBinding](#metaxisdata-v1-IdentityProviderBinding) | repeated |  |






<a name="metaxisdata-v1-ListIdentityProvidersRequest"></a>

### ListIdentityProvidersRequest







<a name="metaxisdata-v1-ListIdentityProvidersResponse"></a>

### ListIdentityProvidersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identity_providers | [IdentityProvider](#metaxisdata-v1-IdentityProvider) | repeated |  |






<a name="metaxisdata-v1-OAuth2IdentityProviderConfig"></a>

### OAuth2IdentityProviderConfig
OAuth2IdentityProviderConfig is an OAuth2 provider&#39;s configuration.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth_url | [string](#string) |  | The authorization endpoint the browser is sent to. Not used by the server: the client builds the authorize URL from it. |
| token_url | [string](#string) |  | The token endpoint. Required. |
| user_info_url | [string](#string) |  | The userinfo endpoint. Required: the login reads the claims from here. |
| client_id | [string](#string) |  | The client id. Required. Not a secret; it travels in the authorize URL. |
| client_secret | [string](#string) |  | The client secret. Required on create. Never returned by a read, and an empty value on update keeps the stored one.

It is stored encrypted with the deployment&#39;s data key (see `backend/common/crypto`), so a database reader without the key cannot authenticate as this application to the provider. |
| scopes | [string](#string) | repeated | The scopes to request. |
| field_mapping | [FieldMapping](#metaxisdata-v1-FieldMapping) |  | The claim mapping. |
| skip_tls_verify | [bool](#bool) |  | Whether to skip TLS certificate verification on the provider&#39;s endpoints.

Off by default and off is the only safe setting: with it on, anyone able to intercept the connection can forge the userinfo response and take over every account bound to this provider. The settings page warns when it is enabled. |
| auth_style | [OAuth2AuthStyle](#metaxisdata-v1-OAuth2AuthStyle) |  | How the client id and secret are sent to the token endpoint. |






<a name="metaxisdata-v1-OAuth2IdentityProviderTestRequestContext"></a>

### OAuth2IdentityProviderTestRequestContext



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  | The authorization code the provider returned to the callback. |
| code_verifier | [string](#string) |  | The code_verifier matching the code_challenge sent to the provider. |






<a name="metaxisdata-v1-TestIdentityProviderRequest"></a>

### TestIdentityProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the stored identity provider to test. Format: idps/{idp} |
| oauth2_context | [OAuth2IdentityProviderTestRequestContext](#metaxisdata-v1-OAuth2IdentityProviderTestRequestContext) |  |  |






<a name="metaxisdata-v1-TestIdentityProviderResponse"></a>

### TestIdentityProviderResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user_info | [IdentityProviderUserInfo](#metaxisdata-v1-IdentityProviderUserInfo) |  | The identity the provider resolved. Field mapping is applied, so a claim that did not resolve is visible as an empty field here. |






<a name="metaxisdata-v1-UpdateIdentityProviderRequest"></a>

### UpdateIdentityProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identity_provider | [IdentityProvider](#metaxisdata-v1-IdentityProvider) |  | The identity provider to update. Its `name` field identifies the row. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The fields to update. Only `title`, `domain` and `config` are mutable. |





 


<a name="metaxisdata-v1-IdentityProviderType"></a>

### IdentityProviderType


| Name | Number | Description |
| ---- | ------ | ----------- |
| IDENTITY_PROVIDER_TYPE_UNSPECIFIED | 0 |  |
| OAUTH2 | 1 | OAuth 2.0 authorization-code flow. |



<a name="metaxisdata-v1-OAuth2AuthStyle"></a>

### OAuth2AuthStyle


| Name | Number | Description |
| ---- | ------ | ----------- |
| OAUTH2_AUTH_STYLE_UNSPECIFIED | 0 |  |
| IN_PARAMS | 1 | IN_PARAMS sends the &#34;client_id&#34; and &#34;client_secret&#34; in the POST body as application/x-www-form-urlencoded parameters. |
| IN_HEADER | 2 | IN_HEADER sends the client_id and client_secret using HTTP Basic Authorization, as described in the OAuth2 RFC 6749 section 2.3.1. |


 

 


<a name="metaxisdata-v1-IdentityProviderService"></a>

### IdentityProviderService
IdentityProviderService is the admin surface for SSO.

It is the write path for the `idp` table: before it existed the only way to
configure a provider was to insert SQL by hand, so the login page had nothing
to point at and `client_secret` could not be encrypted on write.

ListIdentityProviders is deliberately readable without credentials: the login
page renders one button per configured provider before the visitor has a
session. It never returns a secret, so a provider&#39;s non-secret half (auth URL,
client id, scopes, mapping) is what an anonymous caller can see.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetIdentityProvider | [GetIdentityProviderRequest](#metaxisdata-v1-GetIdentityProviderRequest) | [IdentityProvider](#metaxisdata-v1-IdentityProvider) | Get an identity provider, including the non-secret half of its config. |
| ListIdentityProviders | [ListIdentityProvidersRequest](#metaxisdata-v1-ListIdentityProvidersRequest) | [ListIdentityProvidersResponse](#metaxisdata-v1-ListIdentityProvidersResponse) | List every configured identity provider. Public so the login page can render the enabled SSO targets without credentials.

The `permission` annotation is kept beside `allow_without_credential`, as on `SettingService.GetWorkspaceProfileSetting`: the latter wins in the interceptor, and the annotation is what records the permission this read belongs to in the catalog. |
| CreateIdentityProvider | [CreateIdentityProviderRequest](#metaxisdata-v1-CreateIdentityProviderRequest) | [IdentityProvider](#metaxisdata-v1-IdentityProvider) | Create an identity provider. The id becomes the final segment of the resource name and is immutable, so it is derived from the title by the client and checked here. |
| UpdateIdentityProvider | [UpdateIdentityProviderRequest](#metaxisdata-v1-UpdateIdentityProviderRequest) | [IdentityProvider](#metaxisdata-v1-IdentityProvider) | Update an identity provider. Only title, domain and config are mutable; the resource name is not. An empty secret in the config means &#34;keep the stored one&#34;, because the read path never hands the stored secret back. |
| DeleteIdentityProvider | [DeleteIdentityProviderRequest](#metaxisdata-v1-DeleteIdentityProviderRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete an identity provider.

Accounts bound to it keep their binding rows but can no longer sign in through it: the binding records the subject the provider assigned, which no longer resolves to a configured provider. Re-creating the provider with the same id makes those accounts reachable again, which is why the id matters. |
| TestIdentityProvider | [TestIdentityProviderRequest](#metaxisdata-v1-TestIdentityProviderRequest) | [TestIdentityProviderResponse](#metaxisdata-v1-TestIdentityProviderResponse) | TestIdentityProvider completes a real authorization-code exchange against a stored provider and reports the identity it resolves.

The provider has to be saved first: the exchange needs the stored client secret, and the browser is never given it. The caller obtains an authorization code by sending an administrator&#39;s browser to the provider&#39;s authorization endpoint with `{external_url}/oauth/callback` as the redirect URI — the same URI a login uses — so no extra callback has to be registered with the provider. The client marks that state record as a test so the callback page tests instead of signing the administrator in.

This verifies the client id and secret end to end, which endpoint reachability alone cannot. |
| ListIdentityProviderBindings | [ListIdentityProviderBindingsRequest](#metaxisdata-v1-ListIdentityProviderBindingsRequest) | [ListIdentityProviderBindingsResponse](#metaxisdata-v1-ListIdentityProviderBindingsResponse) | List the accounts bound to an identity provider subject.

A binding is what a login resolves against, so this is the read an administrator needs before moving or deleting an account: without it the only way to see why a login lands on an account is SQL. |
| DeleteIdentityProviderBinding | [DeleteIdentityProviderBindingRequest](#metaxisdata-v1-DeleteIdentityProviderBindingRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | DeleteIdentityProviderBinding unbinds one account from one provider subject.

The account loses that way in but keeps every other one; nothing else about it changes. Deleting the last binding of an account whose password was replaced on adoption leaves it reachable only by an administrator resetting the password, so the caller is expected to confirm before doing it.

It is gated by `update`, not `delete`, and the settings page hides the button behind the same permission: this edits a provider&#39;s configuration, and `delete` is reserved for removing a provider outright. |

 



<a name="v1_lineage_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/lineage_service.proto



<a name="metaxisdata-v1-AnalysisScope"></a>

### AnalysisScope
AnalysisScope is one resolution context for a SQL statement: a named database
(MySQL family) or schema (PostgreSQL-like). `name` is an opaque
caller-supplied label that is echoed back so results can be attributed; the
server attaches no meaning to it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| guid | [string](#string) |  | The metadata GUID of the database or schema the statement is resolved against, e.g. &#34;instance_1;db2&#34; or &#34;instance_1;db2;public&#34;. It only supplies the default instance/database/schema for unqualified names; the existence of the database or schema is not validated. |






<a name="metaxisdata-v1-AnalyzeSQLDiagnostic"></a>

### AnalyzeSQLDiagnostic
AnalyzeSQLDiagnostic is one thing an analysis could not represent.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| category | [DiagnosticCategory](#metaxisdata-v1-DiagnosticCategory) |  |  |
| subject | [string](#string) |  | What the diagnostic is about, the way the analyzer names it: the statement or clause shape it does not model (&#34;MERGE&#34;, &#34;WITH before INSERT&#34;), or the clause a reference was written in (&#34;a CTE body&#34;). |
| reference | [string](#string) |  | The identifier a reference diagnostic could not resolve, written the way the SQL writes it (&#34;t.c&#34;, &#34;t&#34;, &#34;c&#34;). Empty when the diagnostic is about a shape. |
| detail | [string](#string) |  | The cause, for a diagnostic that needs one: the parser defect behind a skipped clause, or the catalog error behind a degraded lookup. |






<a name="metaxisdata-v1-AnalyzeSQLRelation"></a>

### AnalyzeSQLRelation



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| source_guid | [string](#string) |  | The table-level GUID of the source column, with instance/database/schema filled in from the scope. It is reported even when the object is not in the metadata registry. |
| source_column | [string](#string) |  |  |
| source_type | [MetaType](#metaxisdata-v1-MetaType) |  | TABLE/VIEW/... when the object is found in the metadata registry. |
| target_guid | [string](#string) |  | Empty when the target is a synthetic object that only exists inside the statement (the result of a bare SELECT, a CTE, a subquery). In that case target_column is the output alias of the query and is_temp is true. |
| target_column | [string](#string) |  |  |
| target_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| relation_type | [RelationType](#metaxisdata-v1-RelationType) |  |  |
| transformations | [Transformation](#metaxisdata-v1-Transformation) | repeated |  |
| is_temp | [bool](#bool) |  |  |






<a name="metaxisdata-v1-AnalyzeSQLRequest"></a>

### AnalyzeSQLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scopes | [AnalysisScope](#metaxisdata-v1-AnalysisScope) | repeated | The scopes to resolve the statement against, at most 10. The statement is analyzed once per scope, independently: the scopes may live on different instances and even different engines, and their results are not merged. Selecting several scopes does not discover cross-scope relations that are absent from the metadata. |
| sql_text | [string](#string) |  | The SQL statement to analyze. |






<a name="metaxisdata-v1-AnalyzeSQLResponse"></a>

### AnalyzeSQLResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| results | [AnalyzeSQLResult](#metaxisdata-v1-AnalyzeSQLResult) | repeated | One result per requested scope, in the same order. |
| warnings | [string](#string) | repeated | Warnings that apply to the request as a whole. |






<a name="metaxisdata-v1-AnalyzeSQLResult"></a>

### AnalyzeSQLResult



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scope_name | [string](#string) |  | The scope name from the request, echoed back. |
| scope_guid | [string](#string) |  |  |
| relations | [AnalyzeSQLRelation](#metaxisdata-v1-AnalyzeSQLRelation) | repeated |  |
| warnings | [string](#string) | repeated | Warnings that only affect this scope, such as an engine without a lineage analyzer. They never fail the whole request. |
| diagnostics | [AnalyzeSQLDiagnostic](#metaxisdata-v1-AnalyzeSQLDiagnostic) | repeated | What the analysis could not represent. relations is everything it did resolve; a diagnostic says why an edge may be missing, which the relations alone cannot say. An analyzer that cannot parse the statement is not reported here: that fails the scope instead. |
| omitted_diagnostic_count | [int32](#int32) |  | How many diagnostics the server&#39;s per-analysis bound left out, so a caller can tell a complete list from a truncated one. |






<a name="metaxisdata-v1-ExternalDatasetInfo"></a>

### ExternalDatasetInfo
ExternalDatasetInfo provides metadata for a dataset outside of managed instances.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| namespace | [string](#string) |  | The OpenLineage namespace, e.g. &#34;postgres://host:5432&#34;. |
| name | [string](#string) |  | The dataset name, e.g. &#34;db.schema.table&#34; or &#34;s3://bucket/path&#34;. |
| dataset_type | [string](#string) |  | The dataset type, e.g. &#34;s3&#34;, &#34;kafka&#34;, &#34;database&#34;. |






<a name="metaxisdata-v1-GetLineageCountsRequest"></a>

### GetLineageCountsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guids | [string](#string) | repeated | The metadata GUIDs to count for, at most 1000. A GUID the registry has no relations for is reported with zero counts rather than failing the batch. |






<a name="metaxisdata-v1-GetLineageCountsResponse"></a>

### GetLineageCountsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| counts | [LineageCount](#metaxisdata-v1-LineageCount) | repeated | One entry per distinct requested GUID, in the order first requested. |






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






<a name="metaxisdata-v1-GetLineageGraphRequest"></a>

### GetLineageGraphRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  | The global unique id for metadata table: &#34;instance_1;db2;schema3;table4&#34; |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| lineage_type | [LineageType](#metaxisdata-v1-LineageType) |  | Which direction to walk: SOURCE looks upstream only, TARGET downstream only, and leaving it unset walks both. |
| depth | [int32](#int32) |  | How many hops to expand, 1 to 10. Defaults to 3. |






<a name="metaxisdata-v1-GetLineageGraphResponse"></a>

### GetLineageGraphResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| root_guid | [string](#string) |  |  |
| nodes | [LineageNode](#metaxisdata-v1-LineageNode) | repeated | The table-, view- and dataset-level objects in the graph. |
| edges | [LineageRelation](#metaxisdata-v1-LineageRelation) | repeated | The column-level edges between those objects. |
| external_datasets | [ExternalDatasetInfo](#metaxisdata-v1-ExternalDatasetInfo) | repeated | Metadata for external datasets referenced in the edges. |
| depth_reached | [int32](#int32) |  | The number of hops actually expanded. |
| truncated | [bool](#bool) |  | Whether a node, edge or time budget stopped the expansion early. |






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






<a name="metaxisdata-v1-LineageCount"></a>

### LineageCount
LineageCount is one object&#39;s degree in the stored graph, at table level: how
many distinct objects its column-level relations connect it to. The count is
of objects, not of relations, so two tables joined on five columns are one
upstream object rather than five.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| upstream_count | [int32](#int32) |  |  |
| downstream_count | [int32](#int32) |  |  |






<a name="metaxisdata-v1-LineageNode"></a>

### LineageNode
LineageNode is one object in the multi-level graph. Only column-level edges
are stored, so the table-level relations are the collapse of those edges: an
object whose definition yields no column-level relation at all (for example a
bare `SELECT count(*)`, or an object that was never synced) does not appear.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| guid | [string](#string) |  |  |
| meta_type | [MetaType](#metaxisdata-v1-MetaType) |  |  |
| name | [string](#string) |  | The object name; the dataset name for an external dataset. |
| database | [string](#string) |  |  |
| schema | [string](#string) |  |  |
| instance_id | [string](#string) |  |  |
| distance | [int32](#int32) |  | Hops from the root: negative upstream, positive downstream. |






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
| all | [bool](#bool) |  | Whether a set operation keeps duplicate rows: true for UNION ALL, INTERSECT ALL and EXCEPT ALL, false for their plain forms (UNION, INTERSECT, EXCEPT). |





 


<a name="metaxisdata-v1-DiagnosticCategory"></a>

### DiagnosticCategory
DiagnosticCategory classifies what an analysis could not represent. The
categories call for different action: a shape the analyzer does not model is a
coverage gap in the analyzer, while a reference that does not resolve is
usually the SQL naming something the analyzer cannot see.

| Name | Number | Description |
| ---- | ------ | ----------- |
| DIAGNOSTIC_CATEGORY_UNSPECIFIED | 0 |  |
| DIAGNOSTIC_CATEGORY_NOT_MODELLED | 1 | A statement or clause shape the analyzer does not model yet. |
| DIAGNOSTIC_CATEGORY_UNRESOLVED_REFERENCE | 2 | A reference that resolved to nothing. |
| DIAGNOSTIC_CATEGORY_AMBIGUOUS_REFERENCE | 3 | A reference a single-valued position could not choose between, which is an unqualified name several relations in scope own. |
| DIAGNOSTIC_CATEGORY_CATALOG_UNAVAILABLE | 4 | A metadata lookup that failed. The analysis carried on with the relation&#39;s columns unknown, so its edges are degraded rather than complete. |



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
| JOIN | 3 | JOIN means the source column is a join key: it decides which rows the target receives without its value reaching the target column. The target column is empty, because the influence is on the rows rather than on one column. |
| GROUP | 4 | GROUP means the source column was aggregated into the target column. For example: select count(source_column) as target_column from table |
| UNION | 5 | UNION, INTERSECT and EXCEPT mean the source column is one arm of the set operation that produced the target column. |
| INTERSECT | 6 |  |
| EXCEPT | 7 |  |
| UNKNOWN | 8 | UNKNOWN is carried so a stored relation of an unrecognized kind round-trips. No analyzer produces it. |


 

 


<a name="metaxisdata-v1-LineageService"></a>

### LineageService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetLineage | [GetLineageRequest](#metaxisdata-v1-GetLineageRequest) | [GetLineageResponse](#metaxisdata-v1-GetLineageResponse) | GetLineage returns the lineage relations for the given metadata. The lineage relations can be either source lineage or target lineage, depending on the lineage_type specified in the request. If lineage_type is not specified, both source and target lineage will be returned. |
| GetLineageCounts | [GetLineageCountsRequest](#metaxisdata-v1-GetLineageCountsRequest) | [GetLineageCountsResponse](#metaxisdata-v1-GetLineageCountsResponse) | GetLineageCounts returns how many distinct objects each requested object has upstream and downstream. A node&#39;s degree cannot be read off its neighbours&#39; relations, so a graph that labels every node it draws would otherwise cost one GetLineage per node; this answers a whole set from two aggregates. Only the counts are returned, never the relations themselves. |
| GetLineageForContext | [GetLineageForContextRequest](#metaxisdata-v1-GetLineageForContextRequest) | [GetLineageForContextResponse](#metaxisdata-v1-GetLineageForContextResponse) | GetLineageForContext retrieves the field-level lineage graph derived from a specific SQL context (e.g., view, stored procedure). |
| AnalyzeSQL | [AnalyzeSQLRequest](#metaxisdata-v1-AnalyzeSQLRequest) | [AnalyzeSQLResponse](#metaxisdata-v1-AnalyzeSQLResponse) | AnalyzeSQL parses an arbitrary SQL statement and returns its column-level relations, resolved against one or more analysis scopes. The analysis is stateless: nothing is persisted and no lineage runner is triggered, so the result describes this statement only and is not part of the stored graph. |
| GetLineageGraph | [GetLineageGraphRequest](#metaxisdata-v1-GetLineageGraphRequest) | [GetLineageGraphResponse](#metaxisdata-v1-GetLineageGraphResponse) | GetLineageGraph returns the multi-level lineage graph around one metadata object in a single call, instead of expanding it one hop at a time with GetLineage. |

 



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

 



<a name="v1_notification_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/notification_service.proto



<a name="metaxisdata-v1-BatchMarkNotificationsReadRequest"></a>

### BatchMarkNotificationsReadRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| names | [string](#string) | repeated | Format: workspaces/{workspace}/notifications/{id} |






<a name="metaxisdata-v1-DataQualityDetail"></a>

### DataQualityDetail
DataQualityDetail reports one data quality event. The message carries facts
only; the client renders the localized sentence from the kind and these fields.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [DataQualityNotificationKind](#metaxisdata-v1-DataQualityNotificationKind) |  |  |
| rule | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |
| rule_title | [string](#string) |  |  |
| target_guid | [string](#string) |  | The registry GUID of the rule&#39;s target. |
| target_path | [string](#string) |  | The display path: instance/database/schema/table[.column]. |
| from_status | [QualityStatus](#metaxisdata-v1-QualityStatus) |  |  |
| to_status | [QualityStatus](#metaxisdata-v1-QualityStatus) |  |  |
| severity | [QualitySeverity](#metaxisdata-v1-QualitySeverity) |  |  |
| observed_value | [double](#double) |  | The observed value and its unit (&#34;rows&#34;, &#34;%&#34;, &#34;s&#34;, &#34;timestamp&#34;). |
| observed_unit | [string](#string) |  |  |
| issue | [string](#string) |  | Format: workspaces/{workspace}/qualityIssues/{quality_issue} |
| run | [string](#string) |  | Format: workspaces/{workspace}/qualityRuns/{quality_run} |
| failed_rows | [int64](#int64) |  |  |
| total_rows | [int64](#int64) |  |  |
| error_code | [string](#string) |  | The machine-readable failure class for EXECUTION_FAILED. |
| failed_rule_count | [int32](#int32) |  | The tallies a STORM_SUMMARY merged. |
| total_rule_count | [int32](#int32) |  |  |






<a name="metaxisdata-v1-DeleteNotificationRequest"></a>

### DeleteNotificationRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/notifications/{id} |






<a name="metaxisdata-v1-GetUnreadNotificationCountRequest"></a>

### GetUnreadNotificationCountRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |






<a name="metaxisdata-v1-GetUnreadNotificationCountResponse"></a>

### GetUnreadNotificationCountResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| unread_count | [int32](#int32) |  |  |
| unseen_count | [int32](#int32) |  | The subset of unread_count written after the caller last opened the inbox. This is the number the badge shows: opening the inbox clears it without marking anything read, so an unread message stays unread until it is clicked. |






<a name="metaxisdata-v1-KeepAlive"></a>

### KeepAlive
KeepAlive is the notification stream&#39;s heartbeat. It carries no data.






<a name="metaxisdata-v1-ListNotificationsRequest"></a>

### ListNotificationsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |
| page_size | [int32](#int32) |  | The maximum number of notifications to return. If unspecified, at most 50 are returned. The maximum value is 1000; values above 1000 are coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous ListNotifications call. Provide this to retrieve the subsequent page. |
| unread_only | [bool](#bool) |  | Return only unread notifications. |






<a name="metaxisdata-v1-ListNotificationsResponse"></a>

### ListNotificationsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| notifications | [Notification](#metaxisdata-v1-Notification) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-MarkAllNotificationsReadRequest"></a>

### MarkAllNotificationsReadRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |






<a name="metaxisdata-v1-MarkNotificationsSeenRequest"></a>

### MarkNotificationsSeenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |






<a name="metaxisdata-v1-Notification"></a>

### Notification
Notification is one in-app message for one recipient.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: workspaces/{workspace}/notifications/{id} |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| type | [NotificationType](#metaxisdata-v1-NotificationType) |  |  |
| severity | [NotificationSeverity](#metaxisdata-v1-NotificationSeverity) |  |  |
| read_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the recipient read it. Unset means unread. |
| schema_sync | [SchemaSyncDetail](#metaxisdata-v1-SchemaSyncDetail) |  |  |
| openlineage | [OpenLineageDetail](#metaxisdata-v1-OpenLineageDetail) |  |  |
| data_quality | [DataQualityDetail](#metaxisdata-v1-DataQualityDetail) |  |  |






<a name="metaxisdata-v1-NotificationEvent"></a>

### NotificationEvent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| notification | [Notification](#metaxisdata-v1-Notification) |  |  |
| unread_count | [int32](#int32) | optional | The recipient&#39;s unread count after this notification was written. Absent when it could not be read, in which case the client asks for the count itself rather than showing a wrong number. |
| unseen_count | [int32](#int32) | optional | The recipient&#39;s unseen count after this notification was written — see GetUnreadNotificationCountResponse.unseen_count. Absent under the same condition as unread_count. |






<a name="metaxisdata-v1-OpenLineageDetail"></a>

### OpenLineageDetail
OpenLineageDetail reports an OpenLineage ingestion failure, or a dataset that
fell through to an external dataset.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [OpenLineageFailureKind](#metaxisdata-v1-OpenLineageFailureKind) |  |  |
| namespace | [string](#string) |  | The namespace named by the event. Empty when the event did not parse. |
| job | [string](#string) |  |  |
| run_id | [string](#string) |  |  |
| dataset | [string](#string) |  | The dataset that did not resolve. Only set for NAMESPACE_UNMAPPED. |
| api_key | [string](#string) |  | The masked identifier of the ingestion key that submitted the request, never the key itself. |
| received_count | [int32](#int32) |  | How many events the request carried and how many of them failed. |
| failed_count | [int32](#int32) |  |  |
| error | [string](#string) |  |  |






<a name="metaxisdata-v1-SchemaSyncDetail"></a>

### SchemaSyncDetail
SchemaSyncDetail reports one sync operation: the instance metadata step plus
every database that operation queued.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [string](#string) |  | Format: instances/{instance} |
| instance_title | [string](#string) |  | The instance&#39;s display name, so the client can render the message without a second read. |
| trigger | [SyncTrigger](#metaxisdata-v1-SyncTrigger) |  |  |
| instance_error | [string](#string) |  | Why the instance metadata step failed. Empty when it succeeded. |
| databases | [SyncDatabaseResult](#metaxisdata-v1-SyncDatabaseResult) | repeated | The databases this message names, each with its outcome: the ones that failed or never reported for an instance-wide sync, and the database itself for a sync of one database. Successful databases are counted rather than listed. |
| succeeded_count | [int32](#int32) |  |  |
| failed_count | [int32](#int32) |  |  |
| unfinished_count | [int32](#int32) |  |  |






<a name="metaxisdata-v1-SubscribeNotificationsRequest"></a>

### SubscribeNotificationsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| parent | [string](#string) |  | Format: workspaces/{workspace}. &#34;workspaces/-&#34; is the current workspace. |






<a name="metaxisdata-v1-SubscribeNotificationsResponse"></a>

### SubscribeNotificationsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| notification | [NotificationEvent](#metaxisdata-v1-NotificationEvent) |  | A notification written while the caller was connected. |
| keep_alive | [KeepAlive](#metaxisdata-v1-KeepAlive) |  | An empty message sent while nothing happens, so a proxy does not end an idle stream — nginx would after proxy_read_timeout, which is 60 seconds by default. The client ignores it. |






<a name="metaxisdata-v1-SyncDatabaseResult"></a>

### SyncDatabaseResult
SyncDatabaseResult is one database&#39;s outcome in a schema sync operation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| database | [string](#string) |  | Format: instances/{instance}/databases/{database} |
| state | [SyncDatabaseState](#metaxisdata-v1-SyncDatabaseState) |  |  |
| error | [string](#string) |  | The sync error. Empty for UNFINISHED. |





 


<a name="metaxisdata-v1-DataQualityNotificationKind"></a>

### DataQualityNotificationKind
DataQualityNotificationKind is the event a data quality notification reports.

| Name | Number | Description |
| ---- | ------ | ----------- |
| DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED | 0 |  |
| DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED | 1 | A rule started failing: an issue was opened. |
| DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED | 2 | A previously failing rule passes again: its issue resolved itself. |
| DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED | 3 | A rule could not be executed at all. This is not a data failure. |
| DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY | 4 | One run failed so many rules that the individual messages were merged. |



<a name="metaxisdata-v1-NotificationSeverity"></a>

### NotificationSeverity
NotificationSeverity ranks a notification for its recipient.

| Name | Number | Description |
| ---- | ------ | ----------- |
| NOTIFICATION_SEVERITY_UNSPECIFIED | 0 |  |
| NOTIFICATION_SEVERITY_INFO | 1 |  |
| NOTIFICATION_SEVERITY_WARNING | 2 |  |
| NOTIFICATION_SEVERITY_ERROR | 3 |  |



<a name="metaxisdata-v1-NotificationType"></a>

### NotificationType
NotificationType is what a notification is about. It selects which detail
message the notification carries.

| Name | Number | Description |
| ---- | ------ | ----------- |
| NOTIFICATION_TYPE_UNSPECIFIED | 0 |  |
| NOTIFICATION_TYPE_SCHEMA_SYNC | 1 | The outcome of one schema sync operation. |
| NOTIFICATION_TYPE_OPENLINEAGE | 2 | An OpenLineage ingestion failure, or a dataset that could not be matched to a registered instance. |
| NOTIFICATION_TYPE_DATA_QUALITY | 3 | A data quality rule opened, recovered, failed to execute, or failed in a storm that was merged into one summary. |



<a name="metaxisdata-v1-OpenLineageFailureKind"></a>

### OpenLineageFailureKind
OpenLineageFailureKind is why an OpenLineage request produced no lineage.

| Name | Number | Description |
| ---- | ------ | ----------- |
| OPENLINEAGE_FAILURE_KIND_UNSPECIFIED | 0 |  |
| OPENLINEAGE_FAILURE_KIND_INVALID_EVENT | 1 | The event could not be parsed, or crossed a per-event limit. |
| OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED | 2 | The request body or the batch crossed a transport limit. |
| OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH | 3 | The event&#39;s namespace is not the one the ingestion key is scoped to. |
| OPENLINEAGE_FAILURE_KIND_PERSIST_FAILED | 4 | The event could not be persisted. |
| OPENLINEAGE_FAILURE_KIND_PROCESS_FAILED | 5 | The event was persisted but its lineage could not be derived. |
| OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED | 6 | The dataset matched no registered instance and was stored as an external dataset, so any lineage naming it points at something the registry does not describe. Check the instance&#39;s data source host and port, or add a namespace mapping. |



<a name="metaxisdata-v1-SyncDatabaseState"></a>

### SyncDatabaseState
SyncDatabaseState is one database&#39;s outcome within a sync operation.

| Name | Number | Description |
| ---- | ------ | ----------- |
| SYNC_DATABASE_STATE_UNSPECIFIED | 0 |  |
| SYNC_DATABASE_STATE_SUCCEEDED | 1 |  |
| SYNC_DATABASE_STATE_FAILED | 2 |  |
| SYNC_DATABASE_STATE_UNFINISHED | 3 | The operation ended before this database produced a result, because its deadline passed or its instance disappeared. |



<a name="metaxisdata-v1-SyncTrigger"></a>

### SyncTrigger
SyncTrigger is what started the sync operation a notification reports.

| Name | Number | Description |
| ---- | ------ | ----------- |
| SYNC_TRIGGER_UNSPECIFIED | 0 |  |
| SYNC_TRIGGER_MANUAL | 1 | A user asked for it. The initiator is the recipient. |
| SYNC_TRIGGER_BACKGROUND | 2 | The periodic scan asked for it, so there is no initiator and the workspace administrators are the recipients. |


 

 


<a name="metaxisdata-v1-NotificationService"></a>

### NotificationService
NotificationService serves the caller&#39;s own in-app messages: the outcome of a
sync operation they asked for, and the ingestion failures a workspace
administrator needs to see.

No method here carries a (metaxisdata.v1.permission) annotation, and none is
reachable without credentials. An inbox belongs to one principal, which the
workspace-scoped IAM catalog cannot express: every handler therefore scopes
its query by the authenticated caller&#39;s id, the same way UserService&#39;s
self-service paths do. Gating these methods on a permission instead would
lock a custom role out of its own messages.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListNotifications | [ListNotificationsRequest](#metaxisdata-v1-ListNotificationsRequest) | [ListNotificationsResponse](#metaxisdata-v1-ListNotificationsResponse) | List the caller&#39;s notifications, newest first. |
| GetUnreadNotificationCount | [GetUnreadNotificationCountRequest](#metaxisdata-v1-GetUnreadNotificationCountRequest) | [GetUnreadNotificationCountResponse](#metaxisdata-v1-GetUnreadNotificationCountResponse) | Count the caller&#39;s unread notifications, and the subset of them written since the caller last opened the inbox. |
| MarkNotificationsSeen | [MarkNotificationsSeenRequest](#metaxisdata-v1-MarkNotificationsSeenRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Record that the caller has opened the inbox, which clears the badge. No notification is marked read: a message stays unread until it is clicked or the caller marks everything read. |
| BatchMarkNotificationsRead | [BatchMarkNotificationsReadRequest](#metaxisdata-v1-BatchMarkNotificationsReadRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Mark the named notifications read. Already-read notifications are left alone, so the call is idempotent. |
| MarkAllNotificationsRead | [MarkAllNotificationsReadRequest](#metaxisdata-v1-MarkAllNotificationsReadRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Mark every unread notification of the caller read. |
| DeleteNotification | [DeleteNotificationRequest](#metaxisdata-v1-DeleteNotificationRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete one of the caller&#39;s notifications. |
| SubscribeNotifications | [SubscribeNotificationsRequest](#metaxisdata-v1-SubscribeNotificationsRequest) | [SubscribeNotificationsResponse](#metaxisdata-v1-SubscribeNotificationsResponse) stream | Stream the caller&#39;s notifications as they are written, until the connection ends or the server ends it.

No (google.api.http) annotation: this is a server-streaming method, and the REST gateway cannot carry one — it answers &#34;streaming calls are not yet supported in the in-process transport&#34;, as the generated ExplainSQL gateway route does. The browser reaches the Connect endpoint directly, the way it does for ExplainSQL. |

 



<a name="v1_oauth_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/oauth_service.proto



<a name="metaxisdata-v1-ApproveOAuthAuthorizationRequestRequest"></a>

### ApproveOAuthAuthorizationRequestRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: oauthAuthorizationRequests/{request_id} |
| approve | [bool](#bool) |  | approve records the decision; false denies the request. |






<a name="metaxisdata-v1-GetOAuthAuthorizationRequestRequest"></a>

### GetOAuthAuthorizationRequestRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the pending request. Format: oauthAuthorizationRequests/{request_id} |






<a name="metaxisdata-v1-OAuthAuthorizationRequest"></a>

### OAuthAuthorizationRequest
OAuthAuthorizationRequest is one pending OAuth 2.1 authorization request. It
is display-only: the server trusts nothing the consent page sends back except
the approve decision, and never exposes the authorization code, the PKCE
challenge or the client&#39;s polling state here.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | Format: oauthAuthorizationRequests/{request_id} |
| client_name | [string](#string) |  | The client&#39;s self-reported name, as registered. It is not verified: it is shown so the user can tell clients apart, not relied upon. |
| redirect_uri | [string](#string) |  | Where the authorization code will be delivered. Shown in full so the user can recognise a redirect they did not expect. |
| resource | [string](#string) |  | The resource the client asked for, which must be this deployment&#39;s MCP endpoint. |
| scopes | [string](#string) | repeated | The scopes the client asked for. |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| expire_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_ip | [string](#string) |  | The source address the request came from, as the server sees it (it follows the trusted-proxy rules). |





 

 

 


<a name="metaxisdata-v1-OAuthService"></a>

### OAuthService
OAuthService serves the browser-facing half of the OAuth 2.1 authorization
server. The protocol endpoints themselves -- /oauth/authorize,
/oauth/authorize/complete, /oauth/token and /oauth/register -- are plain HTTP,
because OAuth clients do not speak ConnectRPC; this service carries only what
the SPA consent page needs.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetOAuthAuthorizationRequest | [GetOAuthAuthorizationRequestRequest](#metaxisdata-v1-GetOAuthAuthorizationRequestRequest) | [OAuthAuthorizationRequest](#metaxisdata-v1-OAuthAuthorizationRequest) | GetOAuthAuthorizationRequest returns one pending authorization request so the consent page can show what is being approved. The caller must be signed in; the request carries no permission annotation, like the device login confirmation it mirrors. Permissions required: None |
| ApproveOAuthAuthorizationRequest | [ApproveOAuthAuthorizationRequestRequest](#metaxisdata-v1-ApproveOAuthAuthorizationRequestRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | ApproveOAuthAuthorizationRequest records the signed-in user&#39;s decision. The approver becomes the identity the access token will be issued for. The browser then goes to /oauth/authorize/complete, which mints the authorization code and redirects it to the client, so the code never passes through an RPC response or through the page&#39;s own JavaScript. Permissions required: None |

 



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
| column_lineage_only | [bool](#bool) |  | When true, return only datasets for which supports_column_lineage is true. |






<a name="metaxisdata-v1-ListOpenLineageDatasetsResponse"></a>

### ListOpenLineageDatasetsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| datasets | [OpenLineageDatasetResource](#metaxisdata-v1-OpenLineageDatasetResource) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="metaxisdata-v1-ListOpenLineageFilterOptionsRequest"></a>

### ListOpenLineageFilterOptionsRequest







<a name="metaxisdata-v1-ListOpenLineageFilterOptionsResponse"></a>

### ListOpenLineageFilterOptionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| job_namespaces | [OpenLineageFilterOption](#metaxisdata-v1-OpenLineageFilterOption) | repeated | Job namespaces, from the runs the Jobs and Events pages list. |
| job_types | [OpenLineageFilterOption](#metaxisdata-v1-OpenLineageFilterOption) | repeated | Job types, from the aggregated jobs the Jobs page lists. |
| event_types | [OpenLineageFilterOption](#metaxisdata-v1-OpenLineageFilterOption) | repeated | Event types, from the runs the Events page lists. |
| dataset_namespaces | [OpenLineageFilterOption](#metaxisdata-v1-OpenLineageFilterOption) | repeated | Dataset namespaces, integrations and sources, from the runs that carry the inputs and outputs the Datasets page aggregates. |
| integrations | [OpenLineageFilterOption](#metaxisdata-v1-OpenLineageFilterOption) | repeated |  |
| sources | [OpenLineageFilterOption](#metaxisdata-v1-OpenLineageFilterOption) | repeated |  |
| total_runs | [int64](#int64) |  | Totals across the whole registry, for the index pages&#39; summary cards. They are counted rather than summed over the capped lists above, so a workspace with more namespaces than the cap still reports the truth. |
| total_jobs | [int64](#int64) |  |  |
| total_job_namespaces | [int64](#int64) |  |  |






<a name="metaxisdata-v1-ListOpenLineageRunsRequest"></a>

### ListOpenLineageRunsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| search | [string](#string) |  | Case-insensitive substring matched against the run&#39;s job name, namespace, run id, event type, producer and source. The equality filters below each narrow one dimension; this is the free-text box. |
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
| search | [string](#string) |  | Case-insensitive substring matched against the job&#39;s name, namespace, type, integration, processing type and latest run id. The equality filters below each narrow one dimension; this is the free-text box. |
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
| supports_column_lineage | [bool](#bool) |  | Whether the dataset has stored column-level lineage: the object its namespace and name resolve to is an endpoint of at least one lineage edge that names a column on at least one side. Derived from the lineage store, not from a producer-declared facet. |
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






<a name="metaxisdata-v1-OpenLineageFilterOption"></a>

### OpenLineageFilterOption
One filterable value and how many records carry it. Each list is ordered by
that count and capped, so the values a reader is most likely to want come
first and the answer stays bounded.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [string](#string) |  |  |
| count | [int64](#int64) |  |  |






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
| ListOpenLineageFilterOptions | [ListOpenLineageFilterOptionsRequest](#metaxisdata-v1-ListOpenLineageFilterOptionsRequest) | [ListOpenLineageFilterOptionsResponse](#metaxisdata-v1-ListOpenLineageFilterOptionsResponse) | Lists the distinct values the OpenLineage index pages offer in their filter menus. Those pages page their tables from the server, so a menu built from the rows on screen would only ever offer the values of the current page. |
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



<a name="metaxisdata-v1-DataQualitySetting"></a>

### DataQualitySetting
DataQualitySetting configures the data quality subsystem for the workspace.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enable_llm_analysis | [bool](#bool) |  | Whether the LLM-assisted entries (rule draft, failure explanation, column labels) are enabled. Off by default: the rule engine is fully offline. |
| llm_provider_profile | [string](#string) |  | The LLM provider profile the quality analyses use, as a profile resource name. Empty means the first allowed profile. |
| store_failure_samples | [bool](#bool) |  | Whether failed checks keep the sampled rows as evidence. Off means a result records its counts but no values. |
| failure_sample_limit | [int32](#int32) |  | How many failed rows a result keeps. Zero means the server default (20); capped at 100. |
| default_schedule_interval | [google.protobuf.Duration](#google-protobuf-Duration) |  | The schedule interval new rules start with. Zero means the server default (one hour). |
| daily_execution_limit | [int32](#int32) |  | The workspace&#39;s daily execution ceiling; 0 means the server default (10000). |
| daily_column_analysis_limit | [int32](#int32) |  | The workspace&#39;s daily column-analysis ceiling; 0 means the server default. |
| max_columns_per_analysis | [int32](#int32) |  | The maximum number of columns one analysis may cover; 0 means the server default (50). |
| send_sample_values_to_llm | [bool](#bool) |  | Whether raw sample values may be sent to the LLM. Off by default: only aggregate profiles leave the deployment. |






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
| domains | [string](#string) | repeated | The email domains (for example &#34;example.com&#34;) whose users may sign up and sign in. Only enforced when enforce_identity_domain is on; an empty list disables the restriction. |
| enforce_identity_domain | [bool](#bool) |  | Whether to restrict signup and signin to the domains above. |
| allowed_llm_provider_profiles | [string](#string) | repeated | The LLM provider profiles ExplainSQL may use, as profile resource names (&#34;llm-provider-profiles/{id}&#34;). Empty means every enabled profile is allowed. |
| mcp_enabled | [bool](#bool) |  | Whether the MCP endpoint and its OAuth 2.1 authorization server are served. Off by default, and only usable once external_url is configured. |
| sso_email_identity_idps | [string](#string) | repeated | The identity providers, as idps/{idp} resource names, whose email claim may stand in for a stable subject. For a listed provider, field_mapping.subject becomes optional and a login whose address claim matches an existing account makes that account reachable through the provider — adopting it (its password is replaced with a random one) when it had no provider binding at all.

Empty (the default) turns the feature off for every provider. The address claim is mutable, so listing a provider asserts that it verifies addresses and that its configuration, which only an administrator can write, is trusted: whoever can set an address there can sign in as the account it names. |
| data_quality | [DataQualitySetting](#metaxisdata-v1-DataQualitySetting) |  | Data quality settings. They are read by the quality subsystem and written on the data quality settings page; every zero value means the server&#39;s default, which is why the reader resolves defaults instead of trusting the wire value. |





 

 

 


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

