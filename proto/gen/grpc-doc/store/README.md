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
  
- [store/data_quality.proto](#store_data_quality-proto)
    - [AcceptedValuesConfig](#metaxisdata-store-AcceptedValuesConfig)
    - [AggregateConfig](#metaxisdata-store-AggregateConfig)
    - [CelExpressionConfig](#metaxisdata-store-CelExpressionConfig)
    - [ColumnProfile](#metaxisdata-store-ColumnProfile)
    - [ColumnValue](#metaxisdata-store-ColumnValue)
    - [CrossColumnConfig](#metaxisdata-store-CrossColumnConfig)
    - [CrossTableConfig](#metaxisdata-store-CrossTableConfig)
    - [CustomSQLConfig](#metaxisdata-store-CustomSQLConfig)
    - [DataQualityLLMResult](#metaxisdata-store-DataQualityLLMResult)
    - [ExplanationHypothesis](#metaxisdata-store-ExplanationHypothesis)
    - [ExplanationResult](#metaxisdata-store-ExplanationResult)
    - [FailureSample](#metaxisdata-store-FailureSample)
    - [FreshnessConfig](#metaxisdata-store-FreshnessConfig)
    - [GroupCheckConfig](#metaxisdata-store-GroupCheckConfig)
    - [NullPercentConfig](#metaxisdata-store-NullPercentConfig)
    - [ProfileValue](#metaxisdata-store-ProfileValue)
    - [QualityContractCheck](#metaxisdata-store-QualityContractCheck)
    - [QualityContractSettings](#metaxisdata-store-QualityContractSettings)
    - [QualityContractSnapshot](#metaxisdata-store-QualityContractSnapshot)
    - [QualityFailureSamples](#metaxisdata-store-QualityFailureSamples)
    - [QualityFilter](#metaxisdata-store-QualityFilter)
    - [QualityFilterClause](#metaxisdata-store-QualityFilterClause)
    - [QualityLLMDiagnostic](#metaxisdata-store-QualityLLMDiagnostic)
    - [QualityRuleConfig](#metaxisdata-store-QualityRuleConfig)
    - [QualityRuleDraftConfig](#metaxisdata-store-QualityRuleDraftConfig)
    - [QualityStatusCounts](#metaxisdata-store-QualityStatusCounts)
    - [QualitySuggestionJobTarget](#metaxisdata-store-QualitySuggestionJobTarget)
    - [QualitySuggestionJobTargets](#metaxisdata-store-QualitySuggestionJobTargets)
    - [QualitySuggestionPayload](#metaxisdata-store-QualitySuggestionPayload)
    - [QualitySuggestionQueueSummary](#metaxisdata-store-QualitySuggestionQueueSummary)
    - [QualityTargetPath](#metaxisdata-store-QualityTargetPath)
    - [QualityThresholds](#metaxisdata-store-QualityThresholds)
    - [RangeConfig](#metaxisdata-store-RangeConfig)
    - [ReferentialIntegrityConfig](#metaxisdata-store-ReferentialIntegrityConfig)
    - [RegexConfig](#metaxisdata-store-RegexConfig)
    - [RowCountChangeConfig](#metaxisdata-store-RowCountChangeConfig)
    - [RowCountConfig](#metaxisdata-store-RowCountConfig)
    - [RuleDraftResult](#metaxisdata-store-RuleDraftResult)
    - [SchemaDriftColumn](#metaxisdata-store-SchemaDriftColumn)
    - [SchemaDriftConfig](#metaxisdata-store-SchemaDriftConfig)
    - [SemanticLabelEvidence](#metaxisdata-store-SemanticLabelEvidence)
    - [SemanticLabelSuggestion](#metaxisdata-store-SemanticLabelSuggestion)
    - [StringLengthConfig](#metaxisdata-store-StringLengthConfig)
    - [UniqueCombinationConfig](#metaxisdata-store-UniqueCombinationConfig)
    - [UniqueConfig](#metaxisdata-store-UniqueConfig)
  
    - [QualityAggregateFunction](#metaxisdata-store-QualityAggregateFunction)
    - [QualityComparison](#metaxisdata-store-QualityComparison)
    - [QualityComparisonOperator](#metaxisdata-store-QualityComparisonOperator)
    - [QualityDimension](#metaxisdata-store-QualityDimension)
    - [QualityEmptyBehavior](#metaxisdata-store-QualityEmptyBehavior)
    - [QualityExecutionState](#metaxisdata-store-QualityExecutionState)
    - [QualityExecutionTrigger](#metaxisdata-store-QualityExecutionTrigger)
    - [QualityFilterOperator](#metaxisdata-store-QualityFilterOperator)
    - [QualityIssueStatus](#metaxisdata-store-QualityIssueStatus)
    - [QualityLabelSource](#metaxisdata-store-QualityLabelSource)
    - [QualityLabelStatus](#metaxisdata-store-QualityLabelStatus)
    - [QualityObservedKind](#metaxisdata-store-QualityObservedKind)
    - [QualityResolutionReason](#metaxisdata-store-QualityResolutionReason)
    - [QualityRuleState](#metaxisdata-store-QualityRuleState)
    - [QualityRuleType](#metaxisdata-store-QualityRuleType)
    - [QualitySemanticType](#metaxisdata-store-QualitySemanticType)
    - [QualitySeverity](#metaxisdata-store-QualitySeverity)
    - [QualityStatus](#metaxisdata-store-QualityStatus)
    - [QualitySuggestionJobStatus](#metaxisdata-store-QualitySuggestionJobStatus)
    - [QualitySuggestionJobTargetState](#metaxisdata-store-QualitySuggestionJobTargetState)
    - [QualitySuggestionKind](#metaxisdata-store-QualitySuggestionKind)
    - [QualitySuggestionSource](#metaxisdata-store-QualitySuggestionSource)
    - [QualitySuggestionStatus](#metaxisdata-store-QualitySuggestionStatus)
    - [QualityThresholdMetric](#metaxisdata-store-QualityThresholdMetric)
  
- [store/openlineage.proto](#store_openlineage-proto)
    - [OpenLineageRunSummary](#metaxisdata-store-OpenLineageRunSummary)
    - [OpenLineageTaskSummary](#metaxisdata-store-OpenLineageTaskSummary)
  
- [store/database.proto](#store_database-proto)
    - [ApplicationSQLMetadata](#metaxisdata-store-ApplicationSQLMetadata)
    - [BoundingBox](#metaxisdata-store-BoundingBox)
    - [CheckConstraintMetadata](#metaxisdata-store-CheckConstraintMetadata)
    - [ColumnMetadata](#metaxisdata-store-ColumnMetadata)
    - [DatabaseMetadata](#metaxisdata-store-DatabaseMetadata)
    - [DatabaseMetadata.LabelsEntry](#metaxisdata-store-DatabaseMetadata-LabelsEntry)
    - [DatabaseSchemaMetadata](#metaxisdata-store-DatabaseSchemaMetadata)
    - [DependencyColumn](#metaxisdata-store-DependencyColumn)
    - [DependencyTable](#metaxisdata-store-DependencyTable)
    - [DimensionalConfig](#metaxisdata-store-DimensionalConfig)
    - [EnumTypeMetadata](#metaxisdata-store-EnumTypeMetadata)
    - [EventMetadata](#metaxisdata-store-EventMetadata)
    - [EventTriggerMetadata](#metaxisdata-store-EventTriggerMetadata)
    - [ExcludeConstraintMetadata](#metaxisdata-store-ExcludeConstraintMetadata)
    - [ExtensionMetadata](#metaxisdata-store-ExtensionMetadata)
    - [ExternalTableMetadata](#metaxisdata-store-ExternalTableMetadata)
    - [ForeignKeyMetadata](#metaxisdata-store-ForeignKeyMetadata)
    - [FunctionMetadata](#metaxisdata-store-FunctionMetadata)
    - [GenerationMetadata](#metaxisdata-store-GenerationMetadata)
    - [GridLevel](#metaxisdata-store-GridLevel)
    - [IndexMetadata](#metaxisdata-store-IndexMetadata)
    - [ManualSQLMetadata](#metaxisdata-store-ManualSQLMetadata)
    - [ManualSQLMetadata.AttributesEntry](#metaxisdata-store-ManualSQLMetadata-AttributesEntry)
    - [MaterializedViewMetadata](#metaxisdata-store-MaterializedViewMetadata)
    - [ProcedureMetadata](#metaxisdata-store-ProcedureMetadata)
    - [RuleMetadata](#metaxisdata-store-RuleMetadata)
    - [SchemaMetadata](#metaxisdata-store-SchemaMetadata)
    - [SequenceMetadata](#metaxisdata-store-SequenceMetadata)
    - [SpatialIndexConfig](#metaxisdata-store-SpatialIndexConfig)
    - [SpatialIndexConfig.EngineSpecificEntry](#metaxisdata-store-SpatialIndexConfig-EngineSpecificEntry)
    - [StorageConfig](#metaxisdata-store-StorageConfig)
    - [StoredMetadata](#metaxisdata-store-StoredMetadata)
    - [TableMetadata](#metaxisdata-store-TableMetadata)
    - [TablePartitionMetadata](#metaxisdata-store-TablePartitionMetadata)
    - [TessellationConfig](#metaxisdata-store-TessellationConfig)
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
  
- [store/notification.proto](#store_notification-proto)
    - [DataQualityDetail](#metaxisdata-store-DataQualityDetail)
    - [Notification](#metaxisdata-store-Notification)
    - [OpenLineageDetail](#metaxisdata-store-OpenLineageDetail)
    - [SchemaSyncDetail](#metaxisdata-store-SchemaSyncDetail)
    - [SyncDatabaseResult](#metaxisdata-store-SyncDatabaseResult)
  
    - [DataQualityNotificationKind](#metaxisdata-store-DataQualityNotificationKind)
    - [NotificationSeverity](#metaxisdata-store-NotificationSeverity)
    - [NotificationType](#metaxisdata-store-NotificationType)
    - [OpenLineageFailureKind](#metaxisdata-store-OpenLineageFailureKind)
    - [SyncDatabaseState](#metaxisdata-store-SyncDatabaseState)
    - [SyncTrigger](#metaxisdata-store-SyncTrigger)
  
- [store/policy.proto](#store_policy-proto)
    - [Binding](#metaxisdata-store-Binding)
    - [IamPolicy](#metaxisdata-store-IamPolicy)
    - [Policy](#metaxisdata-store-Policy)
  
    - [Policy.Resource](#metaxisdata-store-Policy-Resource)
    - [Policy.Type](#metaxisdata-store-Policy-Type)
  
- [store/role.proto](#store_role-proto)
    - [RolePermissions](#metaxisdata-store-RolePermissions)
  
- [store/setting.proto](#store_setting-proto)
    - [DataQualitySetting](#metaxisdata-store-DataQualitySetting)
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
Used internally for obfuscating the page token. The fields are int64 so an
offset cannot overflow int32 after a forged or very large token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| limit | [int64](#int64) |  |  |
| offset | [int64](#int64) |  |  |





 


<a name="metaxisdata-store-Engine"></a>

### Engine
Engine is the database engine of an instance. It must stay value-compatible
with metaxisdata.v1.Engine.

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


 

 

 



<a name="store_data_quality-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/data_quality.proto



<a name="metaxisdata-store-AcceptedValuesConfig"></a>

### AcceptedValuesConfig
AcceptedValuesConfig counts rows whose column value is outside a fixed set.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| values | [string](#string) | repeated | At most 200 values, encoded as strings and converted by the column&#39;s type. |
| allow_null | [bool](#bool) |  | When false (the default), a NULL value is a violation. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-AggregateConfig"></a>

### AggregateConfig
AggregateConfig bounds the value one aggregate over one column produces.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| function | [QualityAggregateFunction](#metaxisdata-store-QualityAggregateFunction) |  |  |
| percentile | [double](#double) | optional | The percentile, 0-100, for QUALITY_AGGREGATE_FUNCTION_PERCENTILE. |
| min | [double](#double) | optional | At least one fail bound is required; warn bounds are optional and must sit inside the fail range. |
| max | [double](#double) | optional |  |
| warn_min | [double](#double) | optional |  |
| warn_max | [double](#double) | optional |  |
| inclusive | [bool](#bool) |  | Whether the bounds are inclusive. Default false, i.e. open on both ends. |






<a name="metaxisdata-store-CelExpressionConfig"></a>

### CelExpressionConfig
CelExpressionConfig evaluates an expression over a sample of rows in Go. It is
v1.1; the message exists so the config shape does not change then.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| expression | [string](#string) |  |  |
| columns | [string](#string) | repeated | The columns the expression reads. They must all exist on the target. |
| sample_rows | [int32](#int32) |  |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-ColumnProfile"></a>

### ColumnProfile
ColumnProfile is the deterministic statistics of one column. It is computed
without a model and is the evidence a semantic label cites.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| total_rows | [int64](#int64) |  |  |
| null_count | [int64](#int64) |  |  |
| distinct_count | [int64](#int64) |  |  |
| distinct_estimated | [bool](#bool) |  | True when an exact distinct count was unavailable — the column&#39;s type has no SQL equality for one, or the database refused it — and the value came from a sample. |
| min_value | [string](#string) |  | Display forms of the observed extremes, truncated; empty when the column has no comparable values. |
| max_value | [string](#string) |  |  |
| min_length | [int64](#int64) | optional | Unset for non-string columns. |
| max_length | [int64](#int64) | optional |  |
| top_values | [ProfileValue](#metaxisdata-store-ProfileValue) | repeated | The most frequent values, at most ten, each truncated. |
| patterns | [string](#string) | repeated | Shape summaries such as &#34;numeric&#34;, &#34;alpha&#34;, &#34;date-like&#34;, &#34;mixed&#34;. |






<a name="metaxisdata-store-ColumnValue"></a>

### ColumnValue
ColumnValue is one column of a sampled failed row. Values are truncated before
they are stored, so a wide text column cannot bloat the result row.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| value | [string](#string) |  | The value rendered as display text. Empty for a SQL NULL and for an empty string; is_null tells the two apart. |
| is_null | [bool](#bool) | optional | Whether the value was SQL NULL. A sample recorded before this field existed has no marker, and its NULLs are indistinguishable from empty strings. |






<a name="metaxisdata-store-CrossColumnConfig"></a>

### CrossColumnConfig
CrossColumnConfig counts rows where two columns of the same row do not satisfy
the comparison.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| left_column | [string](#string) |  |  |
| right_column | [string](#string) |  |  |
| op | [QualityComparisonOperator](#metaxisdata-store-QualityComparisonOperator) |  |  |
| tolerance | [double](#double) | optional | Tolerance subtracted/added to the comparison to absorb floating point noise. Only meaningful for numeric columns. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-CrossTableConfig"></a>

### CrossTableConfig
CrossTableConfig reconciles one aggregate of the rule&#39;s table with one
aggregate of another table in the same instance and database.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| right_table | [string](#string) |  | The other table as &#34;schema.table&#34; (PostgreSQL/MSSQL) or &#34;database.table&#34; / &#34;table&#34; (MySQL family); it must live in the rule target&#39;s database. |
| left_function | [QualityAggregateFunction](#metaxisdata-store-QualityAggregateFunction) |  | The left (target) aggregate; COUNT ignores left_column. |
| left_column | [string](#string) |  |  |
| right_function | [QualityAggregateFunction](#metaxisdata-store-QualityAggregateFunction) |  | The right (reference) aggregate; COUNT ignores right_column. |
| right_column | [string](#string) |  |  |
| tolerance | [double](#double) | optional | The absolute difference tolerated before the thresholds apply: the observed value is max(0, |left - right| - tolerance). |
| fail_threshold | [double](#double) | optional | Difference beyond the tolerance that fails, and the warning level inside it. The fail threshold defaults to zero. |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-CustomSQLConfig"></a>

### CustomSQLConfig
CustomSQLConfig counts the rows a read-only statement returns as failures. The
statement is single SELECT/WITH, at most 8 KiB, and is never rewritten.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sql | [string](#string) |  |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-DataQualityLLMResult"></a>

### DataQualityLLMResult
DataQualityLLMResult is the validated output of one analysis, cached so the
same question is not paid for twice. It is separate from the SQL explanation
cache because its invalidation axis is the rule revision and prompt version,
not the SQL text.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rule_draft | [RuleDraftResult](#metaxisdata-store-RuleDraftResult) |  |  |
| explanation | [ExplanationResult](#metaxisdata-store-ExplanationResult) |  |  |
| prompt_version | [string](#string) |  |  |
| provider | [string](#string) |  |  |
| model | [string](#string) |  |  |
| prompt_tokens | [int64](#int64) |  |  |
| completion_tokens | [int64](#int64) |  |  |
| total_tokens | [int64](#int64) |  |  |






<a name="metaxisdata-store-ExplanationHypothesis"></a>

### ExplanationHypothesis
ExplanationHypothesis is one possible cause with the evidence behind it and the
next step a responder should take.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cause | [string](#string) |  |  |
| evidence | [string](#string) | repeated |  |
| next_step | [string](#string) |  |  |






<a name="metaxisdata-store-ExplanationResult"></a>

### ExplanationResult
ExplanationResult is the parsed output of one failure explanation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| hypotheses | [ExplanationHypothesis](#metaxisdata-store-ExplanationHypothesis) | repeated |  |
| confidence | [double](#double) |  |  |
| summary | [string](#string) |  |  |






<a name="metaxisdata-store-FailureSample"></a>

### FailureSample
FailureSample is one failed row, already truncated.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| columns | [ColumnValue](#metaxisdata-store-ColumnValue) | repeated |  |






<a name="metaxisdata-store-FreshnessConfig"></a>

### FreshnessConfig
FreshnessConfig measures how long ago the newest timestamp is. A NULL newest
timestamp follows on_empty.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| timestamp_column | [string](#string) |  |  |
| max_lag_seconds | [int32](#int32) |  |  |
| warn_lag_seconds | [int32](#int32) | optional |  |






<a name="metaxisdata-store-GroupCheckConfig"></a>

### GroupCheckConfig
GroupCheckConfig bounds the row count of each group a set of columns forms.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| columns | [string](#string) | repeated | The grouping columns, 1-5 of them. |
| min_rows_per_group | [int64](#int64) | optional | At least one bound is required. |
| max_rows_per_group | [int64](#int64) | optional |  |
| fail_threshold | [double](#double) | optional | Groups outside the bounds tolerated, measured per threshold_metric. |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-NullPercentConfig"></a>

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






<a name="metaxisdata-store-ProfileValue"></a>

### ProfileValue
ProfileValue is one frequent value of a low-cardinality column.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [string](#string) |  |  |
| count | [int64](#int64) |  |  |






<a name="metaxisdata-store-QualityContractCheck"></a>

### QualityContractCheck
QualityContractCheck is one rule as a published contract records it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rule_resource_id | [string](#string) |  | The rule&#39;s immutable resource id (the API-facing name segment). |
| rule_revision | [int32](#int32) |  | The revision whose definition was published. |
| title | [string](#string) |  |  |
| rule_type | [QualityRuleType](#metaxisdata-store-QualityRuleType) |  |  |
| severity | [QualitySeverity](#metaxisdata-store-QualitySeverity) |  |  |
| column | [string](#string) |  | The column the rule watches; empty for a table-level rule. |






<a name="metaxisdata-store-QualityContractSettings"></a>

### QualityContractSettings
QualityContractSettings is the target-level layer above the rules: defaults the
workspace UI uses when authoring, and the filter every rule on the target is
checked with. Rule-level fields always win where they are set.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| filter | [QualityFilter](#metaxisdata-store-QualityFilter) |  | Applied to every rule on the target, ANDed with the rule&#39;s own filter. It is evaluated at compile time, so a contract change affects the next execution without rewriting the rules. |
| default_severity | [QualitySeverity](#metaxisdata-store-QualitySeverity) |  | The severity a newly authored rule starts with; UNSPECIFIED means no default. |






<a name="metaxisdata-store-QualityContractSnapshot"></a>

### QualityContractSnapshot
QualityContractSnapshot is what a published contract version records: the
target-level settings plus the ordered checks (one per live rule) at publish
time. The rules remain the mutable source of truth; a revision is the record of
what a person published.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| settings | [QualityContractSettings](#metaxisdata-store-QualityContractSettings) |  |  |
| checks | [QualityContractCheck](#metaxisdata-store-QualityContractCheck) | repeated |  |
| fingerprint | [string](#string) |  | SHA-256 over the settings and the ordered checks. Two publishes with the same fingerprint are the same contract, so the second is a no-op. |






<a name="metaxisdata-store-QualityFailureSamples"></a>

### QualityFailureSamples
QualityFailureSamples holds the rows a failed check sampled as evidence.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| samples | [FailureSample](#metaxisdata-store-FailureSample) | repeated |  |






<a name="metaxisdata-store-QualityFilter"></a>

### QualityFilter
QualityFilter is the AND filter a rule applies before observing. Values are
encoded as strings and converted by the compiler using the column&#39;s type; no
user text reaches SQL unparameterized.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| clauses | [QualityFilterClause](#metaxisdata-store-QualityFilterClause) | repeated |  |






<a name="metaxisdata-store-QualityFilterClause"></a>

### QualityFilterClause
QualityFilterClause is one comparison in a QualityFilter.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| op | [QualityFilterOperator](#metaxisdata-store-QualityFilterOperator) |  |  |
| values | [string](#string) | repeated | Unused by IS_NULL / IS_NOT_NULL, at most 100 values for IN / NOT_IN. |






<a name="metaxisdata-store-QualityLLMDiagnostic"></a>

### QualityLLMDiagnostic
QualitySuggestionJobTarget is one table a background drafting job visits. The
path and columns are a snapshot taken at submission, so the job still renders
after the registry changes; resolution happens again when the model is asked.
QualityLLMDiagnostic is the structured context of one failed LLM analysis: the
classifier code, provider facts, the provider&#39;s stop reason, token usage when
the provider reported it, and the correlation id that ties the failure to the
server log and (when runtime debug was on) the llm_debug_log row. It never
contains prompt or response bodies.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| error_code | [string](#string) |  | A stable machine-readable classifier, e.g. TRUNCATED, STREAM_INCOMPLETE, PROVIDER_STATUS, PROVIDER_REQUEST, RESPONSE_TOO_LARGE, TIMEOUT, PARSE, UNKNOWN. |
| provider | [string](#string) |  |  |
| model | [string](#string) |  |  |
| prompt_version | [string](#string) |  |  |
| max_output_tokens | [int32](#int32) |  |  |
| finish_reason | [string](#string) |  |  |
| prompt_tokens | [int64](#int64) |  |  |
| completion_tokens | [int64](#int64) |  |  |
| total_tokens | [int64](#int64) |  |  |
| prompt_chars | [int64](#int64) |  |  |
| correlation_id | [string](#string) |  |  |
| provider_status | [int32](#int32) |  |  |
| debug_captured | [bool](#bool) |  | Whether the full request/response was persisted to llm_debug_log for this failure (runtime debug mode was on and the write was attempted). |






<a name="metaxisdata-store-QualityRuleConfig"></a>

### QualityRuleConfig
QualityRuleConfig is the typed parameters of one rule. Exactly one arm of the
oneof is set, and it must correspond to the rule&#39;s type; the filter, the empty
behavior and the count/percent metric are shared by every type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| row_count | [RowCountConfig](#metaxisdata-store-RowCountConfig) |  |  |
| row_count_change | [RowCountChangeConfig](#metaxisdata-store-RowCountChangeConfig) |  |  |
| null_percent | [NullPercentConfig](#metaxisdata-store-NullPercentConfig) |  |  |
| unique | [UniqueConfig](#metaxisdata-store-UniqueConfig) |  |  |
| unique_combination | [UniqueCombinationConfig](#metaxisdata-store-UniqueCombinationConfig) |  |  |
| accepted_values | [AcceptedValuesConfig](#metaxisdata-store-AcceptedValuesConfig) |  |  |
| range | [RangeConfig](#metaxisdata-store-RangeConfig) |  |  |
| regex | [RegexConfig](#metaxisdata-store-RegexConfig) |  |  |
| string_length | [StringLengthConfig](#metaxisdata-store-StringLengthConfig) |  |  |
| referential_integrity | [ReferentialIntegrityConfig](#metaxisdata-store-ReferentialIntegrityConfig) |  |  |
| cross_column | [CrossColumnConfig](#metaxisdata-store-CrossColumnConfig) |  |  |
| freshness | [FreshnessConfig](#metaxisdata-store-FreshnessConfig) |  |  |
| custom_sql | [CustomSQLConfig](#metaxisdata-store-CustomSQLConfig) |  |  |
| cel_expression | [CelExpressionConfig](#metaxisdata-store-CelExpressionConfig) |  | v1.1. |
| schema_drift | [SchemaDriftConfig](#metaxisdata-store-SchemaDriftConfig) |  | v1.1 first batch: metadata drift and grouped row-count bounds. |
| group_check | [GroupCheckConfig](#metaxisdata-store-GroupCheckConfig) |  |  |
| aggregate | [AggregateConfig](#metaxisdata-store-AggregateConfig) |  | v1.1 second batch: aggregates and cross-table reconciliation. |
| cross_table | [CrossTableConfig](#metaxisdata-store-CrossTableConfig) |  |  |
| threshold_metric | [QualityThresholdMetric](#metaxisdata-store-QualityThresholdMetric) |  |  |
| filter | [QualityFilter](#metaxisdata-store-QualityFilter) |  |  |
| on_empty | [QualityEmptyBehavior](#metaxisdata-store-QualityEmptyBehavior) |  |  |






<a name="metaxisdata-store-QualityRuleDraftConfig"></a>

### QualityRuleDraftConfig
QualityRuleDraftConfig is one model-proposed rule, already validated into the
typed config. It is never executed without a human saving it as a rule.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [QualityRuleConfig](#metaxisdata-store-QualityRuleConfig) |  |  |
| severity | [QualitySeverity](#metaxisdata-store-QualitySeverity) |  |  |
| title | [string](#string) |  |  |
| rationale | [string](#string) |  |  |
| confidence | [double](#double) |  |  |






<a name="metaxisdata-store-QualityStatusCounts"></a>

### QualityStatusCounts
QualityStatusCounts is the six-state tally of a set of executions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| total | [int32](#int32) |  |  |
| pass | [int32](#int32) |  |  |
| warn | [int32](#int32) |  |  |
| fail | [int32](#int32) |  |  |
| error | [int32](#int32) |  |  |
| skipped | [int32](#int32) |  |  |
| not_evaluated | [int32](#int32) |  |  |






<a name="metaxisdata-store-QualitySuggestionJobTarget"></a>

### QualitySuggestionJobTarget



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| target_guid | [string](#string) |  |  |
| target_type | [int32](#int32) |  | The metaxisdata.v1.MetaType number; the store module does not import v1. |
| path | [QualityTargetPath](#metaxisdata-store-QualityTargetPath) |  |  |
| columns | [string](#string) | repeated |  |
| state | [QualitySuggestionJobTargetState](#metaxisdata-store-QualitySuggestionJobTargetState) |  |  |
| suggestion_resource_ids | [string](#string) | repeated | Resource ids (not names) of the suggestions this target queued. |
| error | [string](#string) |  |  |
| started_at_unix_ms | [int64](#int64) |  |  |
| finished_at_unix_ms | [int64](#int64) |  |  |
| diagnostic | [QualityLLMDiagnostic](#metaxisdata-store-QualityLLMDiagnostic) |  | Set when the target failed in the model call; the classifier and context. |
| summary | [QualitySuggestionQueueSummary](#metaxisdata-store-QualitySuggestionQueueSummary) |  | What this target&#39;s generation pass did to the review queue. Set on success. |






<a name="metaxisdata-store-QualitySuggestionJobTargets"></a>

### QualitySuggestionJobTargets
QualitySuggestionJobTargets is the JSONB payload of
quality_suggestion_job.targets.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| targets | [QualitySuggestionJobTarget](#metaxisdata-store-QualitySuggestionJobTarget) | repeated |  |






<a name="metaxisdata-store-QualitySuggestionPayload"></a>

### QualitySuggestionPayload
QualitySuggestionPayload is the review-queue payload: a rule draft the reviewer
can load into the editor, or a semantic label awaiting confirmation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rule | [QualityRuleDraftConfig](#metaxisdata-store-QualityRuleDraftConfig) |  |  |
| label | [SemanticLabelSuggestion](#metaxisdata-store-SemanticLabelSuggestion) |  |  |






<a name="metaxisdata-store-QualitySuggestionQueueSummary"></a>

### QualitySuggestionQueueSummary
QualitySuggestionQueueSummary is what one generation pass did to the review
queue. Generation reconciles proposals instead of appending them, so a pass can
create, refresh, close or skip proposals. The counts let the caller tell the
user what changed without listing every row.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| created | [int32](#int32) |  | Proposals that did not exist before this pass. |
| refreshed | [int32](#int32) |  | Pending proposals whose payload this pass updated. |
| superseded | [int32](#int32) |  | Pending proposals closed because their fact now exists or was replaced. |
| skipped_existing_fact | [int32](#int32) |  | Skipped because the fact already exists: the column has a label, or a rule with the same identity exists. |
| skipped_pending | [int32](#int32) |  | Skipped because an equivalent proposal is already pending; the default generation mode leaves it untouched instead of spending a model call. |
| skipped_decided | [int32](#int32) |  | Skipped because the same proposal was already accepted or rejected and the caller did not force regeneration. |
| skipped_columns | [string](#string) | repeated | The columns a label pass left out, so a caller can name them. |






<a name="metaxisdata-store-QualityTargetPath"></a>

### QualityTargetPath
QualityTargetPath is the display snapshot of where a rule&#39;s target lived when
the rule was saved. It is not the identity: the GUID plus its meta type is.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [string](#string) |  |  |
| database | [string](#string) |  |  |
| schema | [string](#string) |  |  |
| object | [string](#string) |  |  |
| column | [string](#string) |  | Display snapshot only. The checked column is the one named in the rule&#39;s typed config; this field may be empty for rules written before it was populated, and is empty for a table-level rule. |






<a name="metaxisdata-store-QualityThresholds"></a>

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
| comparison | [QualityComparison](#metaxisdata-store-QualityComparison) |  |  |
| on_empty | [QualityEmptyBehavior](#metaxisdata-store-QualityEmptyBehavior) |  |  |






<a name="metaxisdata-store-RangeConfig"></a>

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






<a name="metaxisdata-store-ReferentialIntegrityConfig"></a>

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






<a name="metaxisdata-store-RegexConfig"></a>

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






<a name="metaxisdata-store-RowCountChangeConfig"></a>

### RowCountChangeConfig
RowCountChangeConfig bounds the change against the rule&#39;s last successful
observation. Without a recent baseline the result is NOT_EVALUATED.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| max_increase_pct | [double](#double) | optional |  |
| max_decrease_pct | [double](#double) | optional |  |
| warn_increase_pct | [double](#double) | optional |  |
| warn_decrease_pct | [double](#double) | optional |  |






<a name="metaxisdata-store-RowCountConfig"></a>

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






<a name="metaxisdata-store-RuleDraftResult"></a>

### RuleDraftResult
RuleDraftResult is the parsed output of one rule-draft analysis.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| suggestions | [QualityRuleDraftConfig](#metaxisdata-store-QualityRuleDraftConfig) | repeated |  |






<a name="metaxisdata-store-SchemaDriftColumn"></a>

### SchemaDriftColumn
SchemaDriftColumn is one column of the snapshot a schema-drift rule expects.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| type | [string](#string) |  |  |
| nullable | [bool](#bool) | optional | Whether the column is nullable. Unset means the rule does not judge nullability for this column: a snapshot written before nullability was compared stays usable and does not report a false drift. The editor and the API&#39;s fill-from-target path always set it. |






<a name="metaxisdata-store-SchemaDriftConfig"></a>

### SchemaDriftConfig
SchemaDriftConfig compares the live column list with the snapshot taken when
the rule was saved. The rule reads metadata only, so it costs the target
database nothing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| expected_columns | [SchemaDriftColumn](#metaxisdata-store-SchemaDriftColumn) | repeated | The snapshot. The API fills it from the current columns when a rule is created without one, and the editor lets a user adjust it. |
| check_types | [bool](#bool) | optional | Whether a changed column type or nullability is drift. Unset means true: either change is drift unless the rule author turns the check off. |
| check_order | [bool](#bool) |  | Whether a changed column order is drift. Default false. |
| allow_extra_columns | [bool](#bool) |  | Whether a column the snapshot does not name is drift. Default false. |
| fail_threshold | [double](#double) | optional | Drift items tolerated. The metric is always COUNT here. |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-SemanticLabelEvidence"></a>

### SemanticLabelEvidence
SemanticLabelEvidence is why a label was assigned. It cites the inputs the
labeler saw so a reviewer can check the reasoning without re-running it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| columns | [string](#string) | repeated | Columns whose values or context the label derives from. |
| metrics | [string](#string) | repeated | Profile facts such as &#34;null_rate=0.02&#34; or &#34;distinct=12&#34;. |
| lineage | [string](#string) | repeated | Lineage relations that motivated the label, e.g. &#34;join key of orders&#34;. |
| rationale | [string](#string) |  |  |






<a name="metaxisdata-store-SemanticLabelSuggestion"></a>

### SemanticLabelSuggestion
SemanticLabelSuggestion is a model-proposed semantic label for one column.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| label | [QualitySemanticType](#metaxisdata-store-QualitySemanticType) |  |  |
| confidence | [double](#double) |  |  |
| evidence | [SemanticLabelEvidence](#metaxisdata-store-SemanticLabelEvidence) |  |  |
| suggested_rule_types | [QualityRuleType](#metaxisdata-store-QualityRuleType) | repeated | The rule types the deterministic mapping would suggest for this label, in display order. The model may rank types but may not invent one. |






<a name="metaxisdata-store-StringLengthConfig"></a>

### StringLengthConfig
StringLengthConfig counts rows whose string length is outside the bounds.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| min_len | [int64](#int64) | optional |  |
| max_len | [int64](#int64) | optional |  |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |






<a name="metaxisdata-store-UniqueCombinationConfig"></a>

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






<a name="metaxisdata-store-UniqueConfig"></a>

### UniqueConfig
UniqueConfig counts duplicated non-NULL values in one column. The threshold is
measured per threshold_metric; the default fail threshold is zero.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| column | [string](#string) |  |  |
| count_nulls | [bool](#bool) |  | When true, NULL values are treated as a value: more than one NULL is a duplicate. The default is false, matching SQL uniqueness. |
| fail_threshold | [double](#double) | optional |  |
| warn_threshold | [double](#double) | optional |  |





 


<a name="metaxisdata-store-QualityAggregateFunction"></a>

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



<a name="metaxisdata-store-QualityComparison"></a>

### QualityComparison
QualityComparison is how the observed value is compared with a threshold.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_COMPARISON_UNSPECIFIED | 0 |  |
| QUALITY_COMPARISON_GREATER_THAN | 1 | The rule fails when the observed value is greater than the threshold: counts of invalid rows, ratios, and freshness lag. |
| QUALITY_COMPARISON_LESS_THAN | 2 | The rule fails when the observed value is less than the threshold. |
| QUALITY_COMPARISON_OUTSIDE_RANGE | 3 | The rule fails when the observed value falls outside [lower, upper]: row count bounds. |
| QUALITY_COMPARISON_INSIDE_RANGE | 4 | The rule fails when the observed value falls inside [lower, upper]: an accepted range expressed as a pass band. |



<a name="metaxisdata-store-QualityComparisonOperator"></a>

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



<a name="metaxisdata-store-QualityDimension"></a>

### QualityDimension
QualityDimension is the data quality dimension a rule measures. It is display
metadata and grouping for the overview.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_DIMENSION_UNSPECIFIED | 0 |  |
| QUALITY_DIMENSION_COMPLETENESS | 1 |  |
| QUALITY_DIMENSION_VALIDITY | 2 |  |
| QUALITY_DIMENSION_UNIQUENESS | 3 |  |
| QUALITY_DIMENSION_CONSISTENCY | 4 |  |
| QUALITY_DIMENSION_TIMELINESS | 5 |  |
| QUALITY_DIMENSION_ACCURACY | 6 |  |



<a name="metaxisdata-store-QualityEmptyBehavior"></a>

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



<a name="metaxisdata-store-QualityExecutionState"></a>

### QualityExecutionState
QualityExecutionState is the lifecycle of one execution, separate from the
quality outcome (Status) that is only known once it finishes. The internal
dispatch batch uses the same lifecycle.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_EXECUTION_STATE_UNSPECIFIED | 0 |  |
| QUALITY_EXECUTION_STATE_PENDING | 1 | Planned and waiting for the runner. |
| QUALITY_EXECUTION_STATE_RUNNING | 2 | The runner is executing the rule. |
| QUALITY_EXECUTION_STATE_COMPLETED | 3 | The check reached a verdict; Status holds it. |
| QUALITY_EXECUTION_STATE_ABANDONED | 4 | A restart discarded it before it finished. It is not retried. |



<a name="metaxisdata-store-QualityExecutionTrigger"></a>

### QualityExecutionTrigger
QualityExecutionTrigger is what started one execution. The internal dispatch
batch carries the trigger of its executions.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_EXECUTION_TRIGGER_UNSPECIFIED | 0 |  |
| QUALITY_EXECUTION_TRIGGER_MANUAL | 1 |  |
| QUALITY_EXECUTION_TRIGGER_SCHEDULED | 2 |  |



<a name="metaxisdata-store-QualityFilterOperator"></a>

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



<a name="metaxisdata-store-QualityIssueStatus"></a>

### QualityIssueStatus
QualityIssueStatus is the lifecycle state of an issue.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_ISSUE_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_ISSUE_STATUS_OPEN | 1 |  |
| QUALITY_ISSUE_STATUS_ACKNOWLEDGED | 2 |  |
| QUALITY_ISSUE_STATUS_RESOLVED | 3 |  |



<a name="metaxisdata-store-QualityLabelSource"></a>

### QualityLabelSource
QualityLabelSource is who asserted a column&#39;s semantic label.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_LABEL_SOURCE_UNSPECIFIED | 0 |  |
| QUALITY_LABEL_SOURCE_MANUAL | 1 |  |
| QUALITY_LABEL_SOURCE_LLM | 2 |  |



<a name="metaxisdata-store-QualityLabelStatus"></a>

### QualityLabelStatus
QualityLabelStatus is whether a label is in effect.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_LABEL_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_LABEL_STATUS_SUGGESTED | 1 |  |
| QUALITY_LABEL_STATUS_ACCEPTED | 2 |  |
| QUALITY_LABEL_STATUS_REJECTED | 3 |  |



<a name="metaxisdata-store-QualityObservedKind"></a>

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



<a name="metaxisdata-store-QualityResolutionReason"></a>

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



<a name="metaxisdata-store-QualityRuleState"></a>

### QualityRuleState
QualityRuleState is whether a rule is scheduled. ARCHIVED is the soft-delete
state: the rule, its revisions, results and issues stay readable.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_RULE_STATE_UNSPECIFIED | 0 |  |
| QUALITY_RULE_STATE_ENABLED | 1 |  |
| QUALITY_RULE_STATE_PAUSED | 2 |  |
| QUALITY_RULE_STATE_ARCHIVED | 3 |  |



<a name="metaxisdata-store-QualityRuleType"></a>

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
| QUALITY_RULE_TYPE_CEL_EXPRESSION | 14 | v1.1. The value exists here only so that the two enum definitions stay value-compatible; v1 never writes it. |
| QUALITY_RULE_TYPE_SCHEMA_DRIFT | 15 | v1.1: compares the live column list with a snapshot taken when the rule was saved. It reads metadata only and never queries the target database. |
| QUALITY_RULE_TYPE_GROUP_CHECK | 16 | v1.1: groups rows and bounds each group&#39;s row count. |
| QUALITY_RULE_TYPE_AGGREGATE | 17 | v1.1: aggregates one column and bounds the value. |
| QUALITY_RULE_TYPE_CROSS_TABLE | 18 | v1.1: reconciles one aggregate of this table with one of another table in the same instance and database. |



<a name="metaxisdata-store-QualitySemanticType"></a>

### QualitySemanticType
QualitySemanticType is the closed vocabulary a column may be labeled with. It
is the input to the deterministic semantic-type-to-rule mapping; the model may
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



<a name="metaxisdata-store-QualitySeverity"></a>

### QualitySeverity
QualitySeverity is how much an issue on this rule matters. It is rule metadata
and is copied onto the issue it opens.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SEVERITY_UNSPECIFIED | 0 |  |
| QUALITY_SEVERITY_WARNING | 1 |  |
| QUALITY_SEVERITY_ERROR | 2 |  |
| QUALITY_SEVERITY_CRITICAL | 3 |  |



<a name="metaxisdata-store-QualityStatus"></a>

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



<a name="metaxisdata-store-QualitySuggestionJobStatus"></a>

### QualitySuggestionJobStatus
QualitySuggestionJobStatus is the lifecycle of one background AI drafting job.
The status is a scalar column; the JSONB targets carry a per-target state.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_JOB_STATUS_PENDING | 1 |  |
| QUALITY_SUGGESTION_JOB_STATUS_RUNNING | 2 |  |
| QUALITY_SUGGESTION_JOB_STATUS_COMPLETED | 3 |  |
| QUALITY_SUGGESTION_JOB_STATUS_FAILED | 4 |  |
| QUALITY_SUGGESTION_JOB_STATUS_CANCELED | 5 |  |



<a name="metaxisdata-store-QualitySuggestionJobTargetState"></a>

### QualitySuggestionJobTargetState
QualitySuggestionJobTargetState is where one target of a background drafting
job stands. SKIPPED means the job stopped before visiting it (cancel or quota),
which is different from FAILED: the model was never asked.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING | 1 |  |
| QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING | 2 |  |
| QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE | 3 |  |
| QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED | 4 |  |
| QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED | 5 |  |



<a name="metaxisdata-store-QualitySuggestionKind"></a>

### QualitySuggestionKind
QualitySuggestionKind is what a suggestion proposes.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_KIND_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_KIND_RULE_DRAFT | 1 |  |
| QUALITY_SUGGESTION_KIND_LABEL | 2 |  |



<a name="metaxisdata-store-QualitySuggestionSource"></a>

### QualitySuggestionSource
QualitySuggestionSource is what produced a suggestion.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_SOURCE_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE | 1 |  |
| QUALITY_SUGGESTION_SOURCE_PROFILE | 2 |  |



<a name="metaxisdata-store-QualitySuggestionStatus"></a>

### QualitySuggestionStatus
QualitySuggestionStatus is where a suggestion is in the review queue.

PENDING is the one live state. ACCEPTED and REJECTED are human decisions and
are final. SUPERSEDED means the proposal stopped being relevant before anybody
decided it (the fact it proposed already exists, or the target changed); it is
closed by the server, never by a reviewer, and its identity may be proposed
again later.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_SUGGESTION_STATUS_UNSPECIFIED | 0 |  |
| QUALITY_SUGGESTION_STATUS_PENDING | 1 |  |
| QUALITY_SUGGESTION_STATUS_ACCEPTED | 2 |  |
| QUALITY_SUGGESTION_STATUS_REJECTED | 3 |  |
| QUALITY_SUGGESTION_STATUS_SUPERSEDED | 4 |  |



<a name="metaxisdata-store-QualityThresholdMetric"></a>

### QualityThresholdMetric
QualityThresholdMetric selects what a count-type rule&#39;s threshold is compared
against: the number of failed rows, or failed rows divided by total rows.

| Name | Number | Description |
| ---- | ------ | ----------- |
| QUALITY_THRESHOLD_METRIC_UNSPECIFIED | 0 |  |
| QUALITY_THRESHOLD_METRIC_COUNT | 1 |  |
| QUALITY_THRESHOLD_METRIC_PERCENT | 2 |  |


 

 

 



<a name="store_openlineage-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/openlineage.proto



<a name="metaxisdata-store-OpenLineageRunSummary"></a>

### OpenLineageRunSummary
OpenLineageRunSummary stores what one persisted OpenLineage run is: its
identity, the task it belongs to, and the state it was last seen in.

What its payload yields about datasets (input_count, output_count,
has_lineage) and when the row was written (created_at, updated_at) are
deliberately absent. They are the run table&#39;s to hold. Keeping them here gave
one run&#39;s state more than one fingerprint, since updated_at moves on every
delivery.


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






<a name="metaxisdata-store-OpenLineageTaskSummary"></a>

### OpenLineageTaskSummary
OpenLineageTaskSummary stores what an aggregated task/job is: its identity and
the job-level facets its runs report.

Everything the task table derives from its runs - the latest run it saw and
the counters over them - is deliberately absent. Mirroring them made the
snapshot a second copy of the event stream, with a fingerprint that moved on
every ingested event instead of when the job changed. The read path joins the
run for those.


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





 

 

 

 



<a name="store_database-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/database.proto



<a name="metaxisdata-store-ApplicationSQLMetadata"></a>

### ApplicationSQLMetadata
ApplicationSQLMetadata is one statement an application reported, with every literal
already replaced by `?`. The row is per distinct statement, not per application: which
applications ran it is recorded in application_sql_observation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| application_sql_id | [string](#string) |  | The stable identifier of the statement, generated when it is first received. It survives a change to the fingerprint rules, which is what keeps the object&#39;s lineage and history attached. |
| name | [string](#string) |  | A display label taken from the statement&#39;s first line. The full text is in sql_text. |
| sql_text | [string](#string) |  | The parameterized statement. This is the only form ever stored: the text an application sends is rewritten before it reaches this table. |
| instance_resource | [string](#string) |  | The instance that reported it. Format: instances/{instance} |
| database_name | [string](#string) |  | The database the statement ran against. |
| schema_name | [string](#string) |  | The schema, empty for the MySQL family. |






<a name="metaxisdata-store-BoundingBox"></a>

### BoundingBox
BoundingBox is the bounding box of a spatial index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| xmin | [double](#double) |  |  |
| ymin | [double](#double) |  |  |
| xmax | [double](#double) |  |  |
| ymax | [double](#double) |  |  |






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






<a name="metaxisdata-store-DimensionalConfig"></a>

### DimensionalConfig
DimensionalConfig holds dimensional parameters of a spatial index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| dimensions | [int32](#int32) |  | The number of dimensions, 2 to 4, default 2. |
| data_type | [string](#string) |  | The spatial data type, such as GEOMETRY or GEOGRAPHY. |
| operator_class | [string](#string) |  | The operator class. (PostgreSQL specific). |
| layer_gtype | [string](#string) |  | The geometry type constraint. (Oracle specific). |
| parallel_build | [bool](#bool) |  | Whether the index is built in parallel. |






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






<a name="metaxisdata-store-GridLevel"></a>

### GridLevel
GridLevel is one grid level of a spatial tessellation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [int32](#int32) |  | The level, 1 to 4 for SQL Server. |
| density | [string](#string) |  | The density, such as LOW, MEDIUM or HIGH. |






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
| spatial_config | [SpatialIndexConfig](#metaxisdata-store-SpatialIndexConfig) |  | The spatial index configuration. (SQL Server and other engines). |






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
| columns | [ColumnMetadata](#metaxisdata-store-ColumnMetadata) | repeated | The ordered list of columns in the materialized view. |
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






<a name="metaxisdata-store-SpatialIndexConfig"></a>

### SpatialIndexConfig
SpatialIndexConfig is the configuration of a spatial index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| method | [string](#string) |  | The index method, such as &#34;SPATIAL&#34; or &#34;GIST&#34;. |
| tessellation | [TessellationConfig](#metaxisdata-store-TessellationConfig) |  | The tessellation configuration. (SQL Server specific). |
| storage | [StorageConfig](#metaxisdata-store-StorageConfig) |  | The storage and performance parameters. |
| dimensional | [DimensionalConfig](#metaxisdata-store-DimensionalConfig) |  | The dimensional parameters. |
| engine_specific | [SpatialIndexConfig.EngineSpecificEntry](#metaxisdata-store-SpatialIndexConfig-EngineSpecificEntry) | repeated | Engine specific parameters. |






<a name="metaxisdata-store-SpatialIndexConfig-EngineSpecificEntry"></a>

### SpatialIndexConfig.EngineSpecificEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="metaxisdata-store-StorageConfig"></a>

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
| application_sql_metadata | [ApplicationSQLMetadata](#metaxisdata-store-ApplicationSQLMetadata) |  |  |






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






<a name="metaxisdata-store-TessellationConfig"></a>

### TessellationConfig
TessellationConfig is the tessellation configuration of a spatial index.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scheme | [string](#string) |  | The tessellation scheme, such as GEOMETRY_GRID or GEOGRAPHY_GRID. |
| bounding_box | [BoundingBox](#metaxisdata-store-BoundingBox) |  | The bounding box. (SQL Server specific). |
| grid_levels | [GridLevel](#metaxisdata-store-GridLevel) | repeated | The grid levels. (SQL Server specific). |
| cells_per_object | [int32](#int32) |  | The number of cells per object. (SQL Server specific). |






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
| subject | [string](#string) |  | Subject is the field name of the provider-assigned subject in 3rd-party idp user info, such as the OIDC &#34;sub&#34; claim. An account is bound to this value and a repeat login resolves against that binding, never against the identifier (email) claim, which is mutable.

Required unless the workspace has turned on the email-identity switch (allow_sso_email_identity), which accepts a provider that maps no subject and identifies its users by the address claim. |
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
| identifier | [string](#string) |  | Identifier is the value of the unique identifier in 3rd-party idp user info, normally the email address. It names the account but does not identify it. |
| subject | [string](#string) |  | Subject is the value of the provider-assigned subject in 3rd-party idp user info. It is the identity a login is bound to. |
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
OAuth2 is implemented: the login path rejects anything else, and
IdentityProviderService creates nothing else.

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
| password_ciphertext | [string](#string) |  | A credential is persisted only as its ciphertext: the *_ciphertext field of a pair holds a v1 AES-256-GCM value (backend/common/crypto) and the plaintext field beside it exists for the duration of a read or write, never in the stored row. |
| use_ssl | [bool](#bool) |  | Use SSL to connect to the data source. By default, we use the system&#39;s SSL configuration. |
| ssl_ca | [string](#string) |  |  |
| ssl_ca_ciphertext | [string](#string) |  |  |
| ssl_cert | [string](#string) |  |  |
| ssl_cert_ciphertext | [string](#string) |  |  |
| ssl_key | [string](#string) |  |  |
| ssl_key_ciphertext | [string](#string) |  |  |
| verify_tls_certificate | [bool](#bool) |  | verify_tls_certificate enables TLS certificate verification for SSL connections. Default is false (no verification) for backward compatibility. Set to true for secure connections (recommended for production). Only set to false for development or when certificates cannot be properly validated (e.g., self-signed certs, VPN environments). |
| host | [string](#string) |  |  |
| port | [string](#string) |  |  |
| database | [string](#string) |  |  |
| ssh_host | [string](#string) |  | SSH related The hostname of the SSH server agent. |
| ssh_port | [string](#string) |  | The port of the SSH server agent. It&#39;s 22 typically. |
| ssh_user | [string](#string) |  | The user to login the server. |
| ssh_password | [string](#string) |  | The password to login the server. If it&#39;s empty string, no password is required. |
| ssh_password_ciphertext | [string](#string) |  |  |
| ssh_private_key | [string](#string) |  | The private key to login the server. If it&#39;s empty string, we will use the system default private key from os.Getenv(&#34;SSH_AUTH_SOCK&#34;). |
| ssh_private_key_ciphertext | [string](#string) |  |  |
| ssh_host_key | [string](#string) |  | The trusted SSH host key(s) for ssh_host, one entry per line. An entry is either a fingerprint (SHA256:... or MD5:...) or a known_hosts / authorized_keys public key line, as printed by ssh-keyscan. Empty means the SSH host is not trusted: the connection is refused instead of accepting whatever host key answers. |
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
| api_key_ciphertext | [string](#string) |  | api_key_ciphertext holds the provider key as a v1 AES-256-GCM value (backend/common/crypto); the store decrypts it for its callers and never exposes the ciphertext through the API. |
| models | [LlmProviderModel](#metaxisdata-store-LlmProviderModel) | repeated |  |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| update_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| max_output_tokens | [int32](#int32) |  | The answer budget requested from this profile; 0 means the server default. |





 


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
| APPLICATION_SQL | 19 |  |
| VIEW | 5 |  |
| MATERIALIZED_VIEW | 6 |  |
| COLUMN | 7 |  |
| INDEX | 8 |  |
| FOREIGN_KEY | 9 |  |
| PROCEDURE | 10 |  |
| FUNCTION | 11 |  |
| SEQUENCE | 12 |  |
| OPENLINEAGE | 100 | for Non-database internal structure |


 

 

 



<a name="store_notification-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/notification.proto



<a name="metaxisdata-store-DataQualityDetail"></a>

### DataQualityDetail
DataQualityDetail reports one data quality event. The message carries facts
only; the client renders the localized sentence from the kind and these fields.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [DataQualityNotificationKind](#metaxisdata-store-DataQualityNotificationKind) |  |  |
| rule | [string](#string) |  | Format: workspaces/{workspace}/qualityRules/{quality_rule} |
| rule_title | [string](#string) |  |  |
| target_guid | [string](#string) |  | The registry GUID of the rule&#39;s target. |
| target_path | [string](#string) |  | The display path: instance/database/schema/table[.column]. |
| from_status | [QualityStatus](#metaxisdata-store-QualityStatus) |  |  |
| to_status | [QualityStatus](#metaxisdata-store-QualityStatus) |  |  |
| severity | [QualitySeverity](#metaxisdata-store-QualitySeverity) |  |  |
| observed_value | [double](#double) |  | The observed value and its unit (&#34;rows&#34;, &#34;%&#34;, &#34;s&#34;, &#34;timestamp&#34;). |
| observed_unit | [string](#string) |  |  |
| issue | [string](#string) |  | Format: workspaces/{workspace}/qualityIssues/{quality_issue} |
| execution | [string](#string) |  | Format: workspaces/{workspace}/qualityExecutions/{quality_execution} |
| failed_rows | [int64](#int64) |  |  |
| total_rows | [int64](#int64) |  |  |
| error_code | [string](#string) |  | The machine-readable failure class for EXECUTION_FAILED. |
| failed_rule_count | [int32](#int32) |  | The tallies a STORM_SUMMARY merged. |
| total_rule_count | [int32](#int32) |  |  |
| instance | [string](#string) |  | The instance whose dispatch batch the summary describes. Format: instances/{instance}. Empty for the per-execution kinds. |






<a name="metaxisdata-store-Notification"></a>

### Notification
Notification is one in-app message for one recipient: the outcome of a sync
operation the recipient asked for, or an ingestion failure the workspace
administrators need to see. It is personal data, so every read is scoped by
recipient_id rather than by the workspace.

id, create_time, recipient_id, read_time and dedupe_key are columns of the
notification table and are not written into the payload; the store stamps
them on insert and fills them from the row on read, so the payload and the
columns cannot disagree. The payload holds only the message proper.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [int64](#int64) |  | The notification row&#39;s primary key. |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| parent | [string](#string) |  | Format: workspaces/{workspace} |
| recipient_id | [int32](#int32) |  | The principal id this message is for. |
| type | [NotificationType](#metaxisdata-store-NotificationType) |  |  |
| severity | [NotificationSeverity](#metaxisdata-store-NotificationSeverity) |  |  |
| read_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the recipient read it. Unset means unread. Backed by notification.read_at, not by the payload, so the unread count can be answered from a partial index. |
| dedupe_key | [string](#string) |  | Notification deduplication key, empty when the message is not deduplicated. Format: &lt;event&gt;:&lt;target&gt;:&lt;bucket&gt;, where the bucket is a timestamp aligned to the event&#39;s suppression window. |
| schema_sync | [SchemaSyncDetail](#metaxisdata-store-SchemaSyncDetail) |  |  |
| openlineage | [OpenLineageDetail](#metaxisdata-store-OpenLineageDetail) |  |  |
| data_quality | [DataQualityDetail](#metaxisdata-store-DataQualityDetail) |  |  |






<a name="metaxisdata-store-OpenLineageDetail"></a>

### OpenLineageDetail
OpenLineageDetail reports an OpenLineage ingestion failure, or a dataset that
fell through to an external dataset.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [OpenLineageFailureKind](#metaxisdata-store-OpenLineageFailureKind) |  |  |
| namespace | [string](#string) |  | The namespace named by the event. Empty when the event did not parse. |
| job | [string](#string) |  |  |
| run_id | [string](#string) |  |  |
| dataset | [string](#string) |  | The dataset that did not resolve. Only set for NAMESPACE_UNMAPPED. |
| api_key | [string](#string) |  | The masked identifier of the ingestion key that submitted the request. |
| received_count | [int32](#int32) |  | How many events this request carried and how many of them failed. |
| failed_count | [int32](#int32) |  |  |
| error | [string](#string) |  | Truncated before it is stored. |






<a name="metaxisdata-store-SchemaSyncDetail"></a>

### SchemaSyncDetail
SchemaSyncDetail reports one sync operation: the instance metadata step plus
every database that was queued by it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| instance | [string](#string) |  | Format: instances/{instance} |
| instance_title | [string](#string) |  | The instance&#39;s display name, so a client can render the message without a second read. |
| trigger | [SyncTrigger](#metaxisdata-store-SyncTrigger) |  |  |
| instance_error | [string](#string) |  | Why the instance metadata step failed. Empty when it succeeded. |
| databases | [SyncDatabaseResult](#metaxisdata-store-SyncDatabaseResult) | repeated | The databases this message names, each with its outcome: the ones that failed or never reported for an instance-wide sync, and the database itself for a sync of one database. Successful databases are counted rather than listed, and the list is capped; the counts below carry the whole picture. |
| succeeded_count | [int32](#int32) |  |  |
| failed_count | [int32](#int32) |  |  |
| unfinished_count | [int32](#int32) |  |  |






<a name="metaxisdata-store-SyncDatabaseResult"></a>

### SyncDatabaseResult
SyncDatabaseResult is one database&#39;s outcome in a schema sync operation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| database | [string](#string) |  | Format: instances/{instance}/databases/{database} |
| state | [SyncDatabaseState](#metaxisdata-store-SyncDatabaseState) |  |  |
| error | [string](#string) |  | Truncated before it is stored, so one runaway error cannot bloat the row. |





 


<a name="metaxisdata-store-DataQualityNotificationKind"></a>

### DataQualityNotificationKind
DataQualityNotificationKind is the event a data quality notification reports.

| Name | Number | Description |
| ---- | ------ | ----------- |
| DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED | 0 |  |
| DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED | 1 | A rule started failing: an issue was opened. |
| DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED | 2 | A previously failing rule passes again: its issue resolved itself. |
| DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED | 3 | A rule could not be executed at all. This is not a data failure. |
| DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY | 4 | One dispatch batch failed so many rules that the individual messages were merged. |



<a name="metaxisdata-store-NotificationSeverity"></a>

### NotificationSeverity
NotificationSeverity ranks a notification for the recipient.

| Name | Number | Description |
| ---- | ------ | ----------- |
| NOTIFICATION_SEVERITY_UNSPECIFIED | 0 |  |
| NOTIFICATION_SEVERITY_INFO | 1 |  |
| NOTIFICATION_SEVERITY_WARNING | 2 |  |
| NOTIFICATION_SEVERITY_ERROR | 3 |  |



<a name="metaxisdata-store-NotificationType"></a>

### NotificationType
NotificationType is what a notification is about. It selects which detail
message the notification carries.

| Name | Number | Description |
| ---- | ------ | ----------- |
| NOTIFICATION_TYPE_UNSPECIFIED | 0 |  |
| NOTIFICATION_TYPE_SCHEMA_SYNC | 1 | The outcome of one schema sync operation. |
| NOTIFICATION_TYPE_OPENLINEAGE | 2 | An OpenLineage ingestion failure, or a dataset that could not be matched to a registered instance. |
| NOTIFICATION_TYPE_DATA_QUALITY | 3 | A data quality rule opened, recovered, failed to execute, or failed in a storm that was merged into one summary. |



<a name="metaxisdata-store-OpenLineageFailureKind"></a>

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
| OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED | 6 | The dataset matched no registered instance and was stored as an external dataset, so any lineage naming it points at something the registry does not describe. |



<a name="metaxisdata-store-SyncDatabaseState"></a>

### SyncDatabaseState
SyncDatabaseState is one database&#39;s outcome within a sync operation.

| Name | Number | Description |
| ---- | ------ | ----------- |
| SYNC_DATABASE_STATE_UNSPECIFIED | 0 |  |
| SYNC_DATABASE_STATE_SUCCEEDED | 1 |  |
| SYNC_DATABASE_STATE_FAILED | 2 |  |
| SYNC_DATABASE_STATE_UNFINISHED | 3 | The operation ended before this database produced a result, because its deadline passed or its instance disappeared. |



<a name="metaxisdata-store-SyncTrigger"></a>

### SyncTrigger
SyncTrigger is what started the sync operation a notification reports.

| Name | Number | Description |
| ---- | ------ | ----------- |
| SYNC_TRIGGER_UNSPECIFIED | 0 |  |
| SYNC_TRIGGER_MANUAL | 1 | A user asked for it, through an API call or in the UI. The initiator is the recipient. |
| SYNC_TRIGGER_BACKGROUND | 2 | The periodic scan asked for it, so there is no initiator; the workspace administrators are the recipients. |


 

 

 



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



<a name="metaxisdata-store-DataQualitySetting"></a>

### DataQualitySetting
DataQualitySetting is the stored form of the data quality settings; it mirrors
metaxisdata.v1.DataQualitySetting.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enable_llm_analysis | [bool](#bool) |  |  |
| llm_provider_profile | [string](#string) |  |  |
| store_failure_samples | [bool](#bool) |  |  |
| failure_sample_limit | [int32](#int32) |  |  |
| default_schedule_interval | [google.protobuf.Duration](#google-protobuf-Duration) |  |  |
| daily_execution_limit | [int32](#int32) |  |  |
| daily_column_analysis_limit | [int32](#int32) |  |  |
| max_columns_per_analysis | [int32](#int32) |  |  |
| send_sample_values_to_llm | [bool](#bool) |  |  |
| daily_llm_analysis_limit | [int32](#int32) |  |  |
| llm_concurrency_limit | [int32](#int32) |  |  |






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
| allowed_llm_provider_profiles | [string](#string) | repeated | The LLM provider profiles ExplainSQL may use, as profile resource names. Empty means every enabled profile is allowed. |
| mcp_enabled | [bool](#bool) |  | Whether the MCP endpoint and its OAuth 2.1 authorization server are served. Off by default: the surface is new, and enabling it requires a configured external_url, which is where its issuer and resource identifiers come from. |
| sso_email_identity_idps | [string](#string) | repeated | The identity providers whose email claim may stand in for a subject, by the resource id that idps/{idp} names. The settings page picks them from IdentityProviderService.ListIdentityProviders.

For those providers — and only those, because one workspace can configure several, trusted to different degrees — an SSO login may be identified by the address claim instead of a stable subject, and may make an existing account with that address reachable through the provider. An account that had no provider binding is adopted by doing so: its password is replaced with a random one.

Empty (the default) turns the feature off everywhere. The address claim is mutable, so listing a provider asserts that it verifies addresses — an unverified claim lets whoever can set it sign in as the account it names — and that its configuration, which only an administrator can write, is trusted. For a listed provider, field_mapping.subject becomes optional and falls back to the identifier claim. |
| data_quality | [DataQualitySetting](#metaxisdata-store-DataQualitySetting) |  | Data quality settings. Every zero value means the server&#39;s default, so the reader resolves defaults rather than trusting the stored value. |





 


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
| ENCRYPTION_KEY | 16 | ENCRYPTION_KEY is the deployment&#39;s credential data key, in the bare base64 of 32 random bytes when no key-encryption key is configured, or a v1 ciphertext under METAXISDATA_ENCRYPTION_KEY when one is. It is generated on first startup and never leaves the process otherwise. |


 

 

 



<a name="store_user-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/user.proto



<a name="metaxisdata-store-UserProfile"></a>

### UserProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| last_login_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_change_password_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| language | [string](#string) |  | The BCP-47 tag of the language the user reads the UI in, so server-side prose — the Explain SQL answer — comes back in the same language. Empty means never chosen, which reads as the default language. |





 


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

