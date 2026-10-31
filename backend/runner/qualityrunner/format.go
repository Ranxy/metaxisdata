package qualityrunner

import (
	"fmt"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityObservedUnit is the display unit of an observation, matching the units
// the frontend labels: row counts, percentages, seconds and timestamps.
func qualityObservedUnit(kind storepb.QualityObservedKind) string {
	switch kind {
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT:
		return "rows"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO:
		return "%"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS:
		return "s"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_TIMESTAMP:
		return "timestamp"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER:
		return ""
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

// qualityTargetDisplayPath renders the target path snapshot as
// instance/database/schema/table[.column], skipping the containers an engine does
// not have. It is display text, not an identity.
func qualityTargetDisplayPath(path *storepb.QualityTargetPath) string {
	if path == nil {
		return ""
	}
	segments := make([]string, 0, 4)
	for _, segment := range []string{path.GetInstance(), path.GetDatabase(), path.GetSchema(), path.GetObject()} {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	display := strings.Join(segments, "/")
	if path.GetColumn() != "" {
		display += "." + path.GetColumn()
	}
	return display
}

// statusOrUnspecified turns the nullable "previous status" into the detail's
// non-nullable enum. UNSPECIFIED means "there was no earlier execution", which the
// client renders as "first result" rather than as a transition from PASS.
func statusOrUnspecified(status *storepb.QualityStatus) storepb.QualityStatus {
	if status == nil {
		return storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED
	}
	return *status
}

// formatQualityRuleName formats a rule resource name. An empty workspace is the
// degraded form used when the workspace could not be read; it still keeps the
// detail parseable.
func formatQualityRuleName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	if workspaceID == "" {
		return "qualityRules/" + resourceID
	}
	return "workspaces/" + workspaceID + "/qualityRules/" + resourceID
}

// formatQualityIssueName formats an issue resource name.
func formatQualityIssueName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	if workspaceID == "" {
		return "qualityIssues/" + resourceID
	}
	return "workspaces/" + workspaceID + "/qualityIssues/" + resourceID
}

// formatQualityExecutionName formats an execution resource name. An execution is
// addressed by its numeric row id: the table is insert-only history, so it has no
// separate public identity column.
func formatQualityExecutionName(workspaceID string, executionID int64) string {
	if executionID <= 0 {
		return ""
	}
	name := fmt.Sprintf("qualityExecutions/%d", executionID)
	if workspaceID == "" {
		return name
	}
	return "workspaces/" + workspaceID + "/" + name
}

// formatInstanceResourceName formats the instance a storm summary is scoped to.
// Instance resource names are top-level, like every parent resource the API
// accepts, so they carry no workspace prefix.
func formatInstanceResourceName(instanceResourceID string) string {
	if instanceResourceID == "" {
		return ""
	}
	return common.InstanceNamePrefix + instanceResourceID
}

// executionID returns the numeric row id the execution resource name is built
// from, or 0 when the row is not available.
func (e *ruleExecution) executionID() int64 {
	if e.result == nil {
		return 0
	}
	return e.result.ID
}

// ruleResourceID returns the rule's public resource id, or "" when the mutable
// head could not be read.
func (e *ruleExecution) ruleResourceID() string {
	if e.head == nil {
		return ""
	}
	return e.head.ResourceID
}

// ruleTitle returns the rule's display title, or "" when the head is missing.
func (e *ruleExecution) ruleTitle() string {
	if e.head == nil {
		return ""
	}
	return e.head.Title
}

// ownerID returns the rule's owner principal id, or nil.
func (e *ruleExecution) ownerID() *int32 {
	if e.head == nil {
		return nil
	}
	return e.head.OwnerID
}

// severity returns the rule's current severity. The immutable revision carries the
// severity the execution was judged with, but the notification and the issue row
// report the rule's current one; the head wins when it is available.
func (e *ruleExecution) severity() storepb.QualitySeverity {
	if e.head != nil {
		return e.head.Severity
	}
	if e.snapshot != nil {
		return e.snapshot.Severity
	}
	return storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED
}

// targetGUID returns the registry GUID the execution is about.
func (e *ruleExecution) targetGUID() string {
	if e.snapshot != nil && e.snapshot.Target.GUID != "" {
		return e.snapshot.Target.GUID
	}
	if e.result != nil {
		return e.result.TargetGUID
	}
	return ""
}

// targetType returns the registry object type the execution is about.
func (e *ruleExecution) targetType() storepb.MetaType {
	if e.snapshot != nil {
		return e.snapshot.Target.MetaType
	}
	if e.result != nil {
		return e.result.TargetType
	}
	return storepb.MetaType_UNSPECIFIED
}

// requiresReadOnlySource reports whether executing this rule type needs a
// connection to the target database. Metadata rules (schema_drift) observe the
// registry instead, so they must run without a READ_ONLY data source; every query
// rule still requires one.
func requiresReadOnlySource(ruleType storepb.QualityRuleType) bool {
	return !dataquality.IsMetadataRule(ruleType)
}
