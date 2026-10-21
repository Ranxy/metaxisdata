package metrics

import (
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// engineLabel renders a source engine as a lowercase label value. Every engine
// the server supports has an explicit case; an unknown one collapses to
// "unknown" so the label set stays bounded.
func engineLabel(engine storepb.Engine) string {
	switch engine {
	case storepb.Engine_ENGINE_UNSPECIFIED:
		return "unknown"
	case storepb.Engine_MSSQL:
		return "mssql"
	case storepb.Engine_MYSQL:
		return "mysql"
	case storepb.Engine_POSTGRES:
		return "postgres"
	case storepb.Engine_TIDB:
		return "tidb"
	case storepb.Engine_MARIADB:
		return "mariadb"
	case storepb.Engine_OCEANBASE:
		return "oceanbase"
	case storepb.Engine_STARROCKS:
		return "starrocks"
	case storepb.Engine_DORIS:
		return "doris"
	default:
		return "unknown"
	}
}

// analysisMetaTypeLabel renders the object type of a lineage analysis. Only the
// types the analyzer queues have their own value; everything else is "other".
func analysisMetaTypeLabel(metaType storepb.MetaType) string {
	switch metaType {
	case storepb.MetaType_VIEW:
		return "view"
	case storepb.MetaType_MATERIALIZED_VIEW:
		return "materialized_view"
	case storepb.MetaType_MANUAL_SQL:
		return "manual_sql"
	case storepb.MetaType_APPLICATION_SQL:
		return "application_sql"
	default:
		return "other"
	}
}

// objectTypeLabel renders a schema-sync-managed metadata type as a label value.
func objectTypeLabel(metaType storepb.MetaType) string {
	switch metaType {
	case storepb.MetaType_DATABASE:
		return "database"
	case storepb.MetaType_SCHEMA:
		return "schema"
	case storepb.MetaType_TABLE:
		return "table"
	case storepb.MetaType_COLUMN:
		return "column"
	case storepb.MetaType_VIEW:
		return "view"
	case storepb.MetaType_MATERIALIZED_VIEW:
		return "materialized_view"
	case storepb.MetaType_EXTERNAL_TABLE:
		return "external_table"
	case storepb.MetaType_FUNCTION:
		return "function"
	case storepb.MetaType_PROCEDURE:
		return "procedure"
	case storepb.MetaType_SEQUENCE:
		return "sequence"
	default:
		return "other"
	}
}

// qualityRuleTypeLabel renders a data-quality rule type as a lowercase label
// value.
func qualityRuleTypeLabel(ruleType storepb.QualityRuleType) string {
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:
		return "row_count"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		return "row_count_change"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		return "null_percent"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:
		return "unique"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		return "unique_combination"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		return "accepted_values"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		return "range"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		return "regex"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:
		return "string_length"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY:
		return "referential_integrity"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		return "cross_column"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return "freshness"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		return "custom_sql"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		return "cel_expression"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		return "schema_drift"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		return "group_check"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE:
		return "aggregate"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		return "cross_table"
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return "unspecified"
	default:
		return "unspecified"
	}
}

// qualityStatusLabel renders a rule-execution verdict as a lowercase label
// value.
func qualityStatusLabel(status storepb.QualityStatus) string {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS:
		return "pass"
	case storepb.QualityStatus_QUALITY_STATUS_WARN:
		return "warn"
	case storepb.QualityStatus_QUALITY_STATUS_FAIL:
		return "fail"
	case storepb.QualityStatus_QUALITY_STATUS_ERROR:
		return "error"
	case storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		return "skipped"
	case storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return "not_evaluated"
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return "unspecified"
	default:
		return "unspecified"
	}
}

// qualityErrorCodeLabel renders a stable rule error code. The codes are
// constants in backend/component/dataquality; a stored legacy row could carry a
// value this process no longer writes, so the label is lowercased rather than
// rejected, and an empty code becomes "none".
func qualityErrorCodeLabel(code string) string {
	if code == "" {
		return "none"
	}
	return strings.ToLower(code)
}

// outcomeLabel renders a success/failure boolean as a label value.
func outcomeLabel(ok bool) string {
	if ok {
		return "success"
	}
	return "failure"
}
