package v1

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Resource-name prefixes of the data quality resources. They are local because
// the quality tables carry no workspace column: the workspace segment is the
// API's scope proof, not a stored key.
const (
	qualityRulePrefix      = "qualityRules/"
	qualityExecutionPrefix = "qualityExecutions/"
	qualityIssuePrefix     = "qualityIssues/"

	// A rule that is scheduled must say how often. The bounds are the design's
	// accepted range; zero is rejected because pausing is how scheduling stops.
	minQualityScheduleInterval = 15 * time.Minute
	maxQualityScheduleInterval = 7 * 24 * time.Hour
)

// parseQualityRuleName splits workspaces/{workspace}/qualityRules/{quality_rule}.
func parseQualityRuleName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityRulePrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality rule name")
	}
	return tokens[0], tokens[1], nil
}

// parseQualityExecutionName splits
// workspaces/{workspace}/qualityExecutions/{quality_execution}.
func parseQualityExecutionName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityExecutionPrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality execution name")
	}
	return tokens[0], tokens[1], nil
}

// parseQualityExecutionID resolves an execution resource name to its workspace
// and the numeric row id the last segment carries.
func parseQualityExecutionID(name string) (string, int64, error) {
	workspace, executionIDText, err := parseQualityExecutionName(name)
	if err != nil {
		return "", 0, connectErrorInvalid("%v", err)
	}
	executionID, err := strconv.ParseInt(strings.TrimSpace(executionIDText), 10, 64)
	if err != nil || executionID <= 0 {
		return "", 0, connectErrorInvalid("the quality execution name does not carry an execution id")
	}
	return workspace, executionID, nil
}

// parseQualityIssueName splits workspaces/{workspace}/qualityIssues/{quality_issue}.
func parseQualityIssueName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityIssuePrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality issue name")
	}
	return tokens[0], tokens[1], nil
}

// formatQualityRuleName renders a rule resource name.
func formatQualityRuleName(workspace, qualityRule string) string {
	return common.FormatWorkspace(workspace) + "/" + qualityRulePrefix + qualityRule
}

// formatQualityExecutionName renders an execution resource name. An execution has
// no public resource id column: the last segment is the row's immutable id.
func formatQualityExecutionName(workspace string, executionID int64) string {
	return common.FormatWorkspace(workspace) + "/" + qualityExecutionPrefix + strconv.FormatInt(executionID, 10)
}

// formatQualityIssueName renders an issue resource name.
func formatQualityIssueName(workspace, qualityIssue string) string {
	return common.FormatWorkspace(workspace) + "/" + qualityIssuePrefix + qualityIssue
}

// isCurrentWorkspace reports whether a resource name's workspace segment is the
// deployment's workspace. A rule name is scoped to the one workspace, so any
// other segment is a stale or invented name and must not resolve.
func isCurrentWorkspace(workspace, currentWorkspaceID string) bool {
	return workspace == currentWorkspaceID
}

// qualityOwnerID parses users/{user}. It returns nil for an unassigned owner.
func qualityOwnerID(owner string) (*int32, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, nil
	}
	id, err := common.GetUserID(owner)
	if err != nil {
		return nil, errors.Wrap(err, "invalid owner")
	}
	if id <= 0 {
		return nil, errors.Errorf("invalid owner %q", owner)
	}
	principalID := int32(id)
	return &principalID, nil
}

// formatQualityOwner renders an owner id as users/{user}; empty when unassigned.
func formatQualityOwner(ownerID *int32) string {
	if ownerID == nil || *ownerID <= 0 {
		return ""
	}
	return common.FormatUserUID(int(*ownerID))
}

// principalIDPointer converts the authenticated caller id into the store's
// optional principal id. Zero (no authenticated caller) becomes nil.
func principalIDPointer(id int) *int32 {
	if id <= 0 {
		return nil
	}
	principalID := int32(id)
	return &principalID
}

// copyPtr duplicates an optional scalar so the converted message never aliases
// the store message's field.
func copyPtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// durationFromMilliseconds converts the stored millisecond duration into the
// protobuf duration the API exposes.
func durationFromMilliseconds(milliseconds int32) *durationpb.Duration {
	return durationpb.New(time.Duration(milliseconds) * time.Millisecond)
}

// resolveCreateScheduleInterval applies the workspace default to a new rule whose
// request left schedule_interval unset. A rule always runs on a schedule; pausing
// is how it stops, so there is no "no interval" state to store.
func resolveCreateScheduleInterval(requested time.Duration, settings dataquality.Settings) time.Duration {
	if requested > 0 {
		return requested
	}
	return settings.DefaultScheduleInterval
}

// errLoadQualitySettings maps a settings read failure onto the RPC's error space.
func errLoadQualitySettings(err error) error {
	if common.ErrorCode(err) != common.Internal {
		return err
	}
	return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to load the data quality settings"))
}

// validateQualityScheduleInterval enforces the accepted scheduling range.
func validateQualityScheduleInterval(interval time.Duration) error {
	if interval == 0 {
		return connectErrorInvalid("schedule_interval is required: a rule runs on a schedule between 15m and 7d")
	}
	if interval < minQualityScheduleInterval || interval > maxQualityScheduleInterval {
		return connectErrorInvalid("schedule_interval %s is outside the accepted range 15m–7d", interval)
	}
	return nil
}

// convertQualityFilterToV1 converts the store filter into its public shape.
func convertQualityFilterToV1(filter *storepb.QualityFilter) *v1pb.QualityFilter {
	if filter == nil {
		return nil
	}
	converted := &v1pb.QualityFilter{}
	for _, clause := range filter.GetClauses() {
		if clause == nil {
			continue
		}
		converted.Clauses = append(converted.Clauses, &v1pb.QualityFilterClause{
			Column: clause.GetColumn(),
			Op:     v1pb.QualityFilterOperator(clause.GetOp()),
			Values: append([]string(nil), clause.GetValues()...),
		})
	}
	return converted
}

// convertQualityFilterFromV1 converts the public filter into the stored shape.
func convertQualityFilterFromV1(filter *v1pb.QualityFilter) *storepb.QualityFilter {
	if filter == nil {
		return nil
	}
	converted := &storepb.QualityFilter{}
	for _, clause := range filter.GetClauses() {
		if clause == nil {
			continue
		}
		converted.Clauses = append(converted.Clauses, &storepb.QualityFilterClause{
			Column: clause.GetColumn(),
			Op:     storepb.QualityFilterOperator(clause.GetOp()),
			Values: append([]string(nil), clause.GetValues()...),
		})
	}
	return converted
}

// convertQualityThresholdsToV1 converts the threshold snapshot a result records.
func convertQualityThresholdsToV1(thresholds *storepb.QualityThresholds) *v1pb.QualityThresholds {
	if thresholds == nil {
		return nil
	}
	return &v1pb.QualityThresholds{
		WarnValue:      copyPtr(thresholds.WarnValue),
		FailValue:      copyPtr(thresholds.FailValue),
		WarnValueUpper: copyPtr(thresholds.WarnValueUpper),
		FailValueUpper: copyPtr(thresholds.FailValueUpper),
		Comparison:     v1pb.QualityComparison(thresholds.GetComparison()),
		OnEmpty:        v1pb.QualityEmptyBehavior(thresholds.GetOnEmpty()),
	}
}

// convertQualityStatusCountsToV1 converts the six-state tally.
func convertQualityStatusCountsToV1(counts *storepb.QualityStatusCounts) *v1pb.QualityStatusCounts {
	if counts == nil {
		return nil
	}
	return &v1pb.QualityStatusCounts{
		Total:        counts.GetTotal(),
		Pass:         counts.GetPass(),
		Warn:         counts.GetWarn(),
		Fail:         counts.GetFail(),
		Error:        counts.GetError(),
		Skipped:      counts.GetSkipped(),
		NotEvaluated: counts.GetNotEvaluated(),
	}
}

// convertQualityFailureSamplesToV1 flattens the stored sample wrapper into the
// API's repeated FailureSample.
func convertQualityFailureSamplesToV1(samples *storepb.QualityFailureSamples) []*v1pb.FailureSample {
	if samples == nil {
		return nil
	}
	converted := make([]*v1pb.FailureSample, 0, len(samples.GetSamples()))
	for _, sample := range samples.GetSamples() {
		if sample == nil {
			continue
		}
		row := &v1pb.FailureSample{}
		for _, value := range sample.GetColumns() {
			if value == nil {
				continue
			}
			row.Columns = append(row.Columns, &v1pb.ColumnValue{
				Column: value.GetColumn(),
				Value:  value.GetValue(),
			})
		}
		converted = append(converted, row)
	}
	return converted
}

// convertQualityTargetPathToV1 converts a display path snapshot.
func convertQualityTargetPathToV1(path *storepb.QualityTargetPath) *v1pb.QualityTargetPath {
	if path == nil {
		return nil
	}
	return &v1pb.QualityTargetPath{
		Instance: path.GetInstance(),
		Database: path.GetDatabase(),
		Schema:   path.GetSchema(),
		Object:   path.GetObject(),
		Column:   path.GetColumn(),
	}
}

// convertQualityTargetFromRule builds the target a rule message exposes: the
// identity from the rule head and the columns its config reads.
func convertQualityTargetFromRule(rule *store.QualityRuleMessage) *v1pb.QualityTarget {
	if rule == nil {
		return nil
	}
	return &v1pb.QualityTarget{
		MetaGuid: rule.TargetGUID,
		MetaType: v1pb.MetaType(rule.TargetType),
		Path:     convertQualityTargetPathToV1(rule.TargetPath),
		Columns:  qualityRuleReferencedColumns(rule.Config),
	}
}

// convertQualityRule converts one rule head into its public shape. The dynamic
// fields (target_schema_changed, last_result, orphaned_reason) are filled by the
// read paths that can batch them.
func convertQualityRule(workspaceID string, rule *store.QualityRuleMessage) *v1pb.QualityRule {
	if rule == nil {
		return nil
	}
	return &v1pb.QualityRule{
		Name:             formatQualityRuleName(workspaceID, rule.ResourceID),
		Title:            rule.Title,
		Description:      rule.Description,
		Dimension:        v1pb.QualityDimension(rule.Dimension),
		RuleType:         v1pb.QualityRuleType(rule.RuleType),
		Target:           convertQualityTargetFromRule(rule),
		Config:           convertQualityRuleConfigToV1(rule.Config),
		Severity:         v1pb.QualitySeverity(rule.Severity),
		State:            v1pb.QualityRuleState(rule.State),
		Owner:            formatQualityOwner(rule.OwnerID),
		ScheduleInterval: durationpb.New(rule.ScheduleInterval),
		Revision:         rule.Revision,
		CreateTime:       timestamppb.New(rule.CreatedAt),
		UpdateTime:       timestamppb.New(rule.UpdatedAt),
		MuteUntil:        timestampOrNil(rule.MuteUntil),
		Tags:             append([]string(nil), rule.Tags...),
	}
}

// convertQualityExecution converts one stored execution. ruleName is resolved by
// the caller because an execution row stores the rule's numeric id only.
func convertQualityExecution(workspaceID string, execution *store.QualityExecutionMessage, ruleName string) *v1pb.QualityExecution {
	if execution == nil {
		return nil
	}
	instance := ""
	if execution.InstanceResourceID != "" {
		instance = common.FormatInstance(execution.InstanceResourceID)
	}
	converted := &v1pb.QualityExecution{
		Name:              formatQualityExecutionName(workspaceID, execution.ID),
		Rule:              ruleName,
		RuleRevision:      execution.RuleRevision,
		RuleEngineVersion: execution.RuleEngineVersion,
		Target: &v1pb.QualityTarget{
			MetaGuid: execution.TargetGUID,
			MetaType: v1pb.MetaType(execution.TargetType),
			Path:     convertQualityTargetPathToV1(execution.TargetPath),
		},
		State:         v1pb.QualityExecutionState(execution.State),
		Outcome:       v1pb.QualityStatus(execution.Status),
		Trigger:       v1pb.QualityExecutionTrigger(execution.Trigger),
		Initiator:     formatQualityOwner(execution.InitiatorID),
		Instance:      instance,
		ObservedValue: observedValue(execution.ObservedValue),
		ObservedKind:  v1pb.QualityObservedKind(execution.ObservedKind),
		Thresholds:    convertQualityThresholdsToV1(execution.Thresholds),
		FailedRows:    execution.FailedRows,
		TotalRows:     execution.TotalRows,
		Sampled:       execution.Sampled,
		SampleRows:    execution.SampleRows,
		Truncated:     execution.Truncated,
		Samples:       convertQualityFailureSamplesToV1(execution.Sample),
		ExecutedSql:   execution.ExecutedSQL,
		ErrorCode:     execution.ErrorCode,
		ErrorMessage:  execution.ErrorMessage,
		Duration:      durationFromMilliseconds(execution.DurationMS),
		StartTime:     timestamppb.New(execution.StartedAt),
	}
	// An execution that has not reached a terminal state has no finish time; the
	// proto field stays unset rather than showing the epoch.
	if !execution.FinishedAt.IsZero() {
		converted.FinishTime = timestamppb.New(execution.FinishedAt)
	}
	return converted
}

// convertQualityExecutionSummary projects the newest execution onto the compact
// shape a rule list shows.
func convertQualityExecutionSummary(workspaceID string, execution *store.QualityExecutionMessage) *v1pb.QualityExecutionSummary {
	if execution == nil {
		return nil
	}
	return &v1pb.QualityExecutionSummary{
		Status:        v1pb.QualityStatus(execution.Status),
		ObservedValue: observedValue(execution.ObservedValue),
		ObservedKind:  v1pb.QualityObservedKind(execution.ObservedKind),
		FailedRows:    execution.FailedRows,
		TotalRows:     execution.TotalRows,
		Execution:     formatQualityExecutionName(workspaceID, execution.ID),
		FinishTime:    timestamppb.New(execution.FinishedAt),
	}
}

// convertQualityIssue converts one issue row. The rule name is resolved by the
// caller because an issue stores a numeric foreign key only; the first/last seen
// execution names are built from their row ids.
func convertQualityIssue(workspaceID string, issue *store.QualityIssueMessage, ruleName string) *v1pb.QualityIssue {
	if issue == nil {
		return nil
	}
	return &v1pb.QualityIssue{
		Name:               formatQualityIssueName(workspaceID, issue.ResourceID),
		Rule:               ruleName,
		Target:             &v1pb.QualityTarget{MetaGuid: issue.TargetGUID, MetaType: v1pb.MetaType(issue.TargetType)},
		Status:             v1pb.QualityIssueStatus(issue.Status),
		Severity:           v1pb.QualitySeverity(issue.Severity),
		FirstSeenExecution: formatQualityExecutionID(workspaceID, issue.FirstSeenExecutionID),
		LastSeenExecution:  formatQualityExecutionID(workspaceID, issue.LastSeenExecutionID),
		FirstSeenTime:      timestamppb.New(issue.FirstSeenAt),
		LastSeenTime:       timestamppb.New(issue.LastSeenAt),
		ResolvedTime:       timestampOrNil(issue.ResolvedAt),
		ResolutionReason:   v1pb.QualityResolutionReason(issue.ResolutionReason),
		ResolutionNote:     issue.ResolutionNote,
		AcknowledgedBy:     formatQualityOwner(issue.AcknowledgedBy),
		AcknowledgedTime:   timestampOrNil(issue.AcknowledgedAt),
		MutedUntil:         timestampOrNil(issue.MutedUntil),
		OccurrenceCount:    issue.OccurrenceCount,
	}
}

// formatQualityExecutionID renders a nullable execution row id as a resource
// name; an unset id renders empty.
func formatQualityExecutionID(workspaceID string, executionID *int64) string {
	if executionID == nil {
		return ""
	}
	return formatQualityExecutionName(workspaceID, *executionID)
}

// observedValue keeps a legitimate zero distinct from an absent observation.
// The API field is not optional, so an absent value reads as zero; callers that
// need to tell them apart use the status (ERROR / NOT_EVALUATED).
func observedValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// qualityRuleUpdate is one parsed UpdateQualityRule mask: the store patch plus the
// facts the handler needs to revalidate and reschedule.
type qualityRuleUpdate struct {
	Patch *store.UpdateQualityRuleMessage
	// DefinitionChanged is set when the mask names dimension, rule_type, target
	// or config: the merged definition must be revalidated and its path/hash
	// refreshed.
	DefinitionChanged bool
	// ScheduleChanged and StateChanged decide whether next_run_at is recomputed.
	ScheduleChanged bool
	StateChanged    bool
}

// parseQualityRuleUpdate turns the update mask into a store patch. It is pure so
// the mask vocabulary and the "unknown path is invalid" rule are testable
// without a store.
func parseQualityRuleUpdate(rule *v1pb.QualityRule, paths []string) (*qualityRuleUpdate, error) {
	if rule == nil {
		return nil, connectErrorInvalid("the rule is required")
	}
	update := &qualityRuleUpdate{Patch: &store.UpdateQualityRuleMessage{}}
	for _, path := range paths {
		switch path {
		case "title":
			title := rule.GetTitle()
			update.Patch.Title = &title
		case "description":
			description := rule.GetDescription()
			update.Patch.Description = &description
		case "dimension":
			dimension := storepb.QualityDimension(rule.GetDimension())
			if dimension == storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED {
				return nil, connectErrorInvalid("dimension is required")
			}
			update.Patch.Dimension = &dimension
			update.DefinitionChanged = true
		case "rule_type":
			ruleType := storepb.QualityRuleType(rule.GetRuleType())
			if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
				return nil, connectErrorInvalid("rule_type is required")
			}
			update.Patch.RuleType = &ruleType
			update.DefinitionChanged = true
		case "target":
			target := rule.GetTarget()
			if target == nil || strings.TrimSpace(target.GetMetaGuid()) == "" {
				return nil, connectErrorInvalid("target.meta_guid is required")
			}
			metaType := storepb.MetaType(target.GetMetaType())
			if metaType == storepb.MetaType_UNSPECIFIED {
				return nil, connectErrorInvalid("target.meta_type is required")
			}
			guid := strings.TrimSpace(target.GetMetaGuid())
			update.Patch.TargetGUID = &guid
			update.Patch.TargetType = &metaType
			update.DefinitionChanged = true
		case "config":
			config := convertQualityRuleConfigFromV1(rule.GetConfig())
			if config == nil {
				return nil, connectErrorInvalid("config is required")
			}
			update.Patch.Config = config
			update.DefinitionChanged = true
		case "severity":
			severity := storepb.QualitySeverity(rule.GetSeverity())
			if severity == storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED {
				return nil, connectErrorInvalid("severity is required")
			}
			update.Patch.Severity = &severity
		case "state":
			state := storepb.QualityRuleState(rule.GetState())
			if state != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED &&
				state != storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED {
				return nil, connectErrorInvalid("state must be ENABLED or PAUSED; use DeleteQualityRule to archive a rule")
			}
			update.Patch.State = &state
			update.StateChanged = true
		case "owner":
			ownerID, err := qualityOwnerID(rule.GetOwner())
			if err != nil {
				return nil, connectErrorInvalid("%v", err)
			}
			if ownerID == nil {
				// An empty owner in the mask clears the assignment.
				cleared := int32(0)
				ownerID = &cleared
			}
			update.Patch.OwnerID = ownerID
		case "tags":
			tags := append([]string(nil), rule.GetTags()...)
			update.Patch.Tags = &tags
		case "schedule_interval":
			interval := rule.GetScheduleInterval().AsDuration()
			if err := validateQualityScheduleInterval(interval); err != nil {
				return nil, err
			}
			update.Patch.ScheduleInterval = &interval
			update.ScheduleChanged = true
		case "mute_until":
			mutedUntil := time.Time{}
			if rule.GetMuteUntil() != nil {
				mutedUntil = rule.GetMuteUntil().AsTime()
			}
			update.Patch.MuteUntil = &mutedUntil
		default:
			return nil, connectErrorInvalid("unsupported update path %q", path)
		}
	}
	return update, nil
}

// mergedQualityRuleDefinition applies a patch to the current definition, which is
// what validation and target resolution run against.
func mergedQualityRuleDefinition(current *store.QualityRuleMessage, patch *store.UpdateQualityRuleMessage) (
	storepb.QualityRuleType, string, storepb.MetaType, *storepb.QualityRuleConfig,
) {
	ruleType := current.RuleType
	if patch.RuleType != nil {
		ruleType = *patch.RuleType
	}
	guid := current.TargetGUID
	if patch.TargetGUID != nil {
		guid = *patch.TargetGUID
	}
	metaType := current.TargetType
	if patch.TargetType != nil {
		metaType = *patch.TargetType
	}
	config := current.Config
	if patch.Config != nil {
		config = patch.Config
	}
	return ruleType, guid, metaType, config
}

// qualityRuleReferencedColumns returns the target columns a rule's config and
// filter read, in a stable order. It backs the target snapshot and the preview's
// referenced columns; a rule that reads no named column (count, custom SQL)
// returns nil.
func qualityRuleReferencedColumns(config *storepb.QualityRuleConfig) []string {
	if config == nil {
		return nil
	}
	var columns []string
	switch arm := config.GetConfig().(type) {
	case *storepb.QualityRuleConfig_RowCount:
	case *storepb.QualityRuleConfig_RowCountChange:
	case *storepb.QualityRuleConfig_NullPercent:
		columns = appendColumn(columns, arm.NullPercent.GetColumn())
	case *storepb.QualityRuleConfig_Unique:
		columns = appendColumn(columns, arm.Unique.GetColumn())
	case *storepb.QualityRuleConfig_UniqueCombination:
		for _, column := range arm.UniqueCombination.GetColumns() {
			columns = appendColumn(columns, column)
		}
	case *storepb.QualityRuleConfig_AcceptedValues:
		columns = appendColumn(columns, arm.AcceptedValues.GetColumn())
	case *storepb.QualityRuleConfig_Range:
		columns = appendColumn(columns, arm.Range.GetColumn())
	case *storepb.QualityRuleConfig_Regex:
		columns = appendColumn(columns, arm.Regex.GetColumn())
	case *storepb.QualityRuleConfig_StringLength:
		columns = appendColumn(columns, arm.StringLength.GetColumn())
	case *storepb.QualityRuleConfig_ReferentialIntegrity:
		columns = appendColumn(columns, arm.ReferentialIntegrity.GetChildColumn())
	case *storepb.QualityRuleConfig_CrossColumn:
		columns = appendColumn(columns, arm.CrossColumn.GetLeftColumn())
		columns = appendColumn(columns, arm.CrossColumn.GetRightColumn())
	case *storepb.QualityRuleConfig_Freshness:
		columns = appendColumn(columns, arm.Freshness.GetTimestampColumn())
	case *storepb.QualityRuleConfig_SchemaDrift:
		for _, column := range arm.SchemaDrift.GetExpectedColumns() {
			columns = appendColumn(columns, column.GetName())
		}
	case *storepb.QualityRuleConfig_GroupCheck:
		for _, column := range arm.GroupCheck.GetColumns() {
			columns = appendColumn(columns, column)
		}
	case *storepb.QualityRuleConfig_Aggregate:
		columns = appendColumn(columns, arm.Aggregate.GetColumn())
	case *storepb.QualityRuleConfig_CrossTable:
		columns = appendColumn(columns, arm.CrossTable.GetLeftColumn())
	case *storepb.QualityRuleConfig_CustomSql:
	case *storepb.QualityRuleConfig_CelExpression:
		for _, column := range arm.CelExpression.GetColumns() {
			columns = appendColumn(columns, column)
		}
	default:
	}
	for _, clause := range config.GetFilter().GetClauses() {
		columns = appendColumn(columns, clause.GetColumn())
	}
	return columns
}

// appendColumn appends a non-empty column name unless it is already listed.
func appendColumn(columns []string, column string) []string {
	column = strings.TrimSpace(column)
	if column == "" {
		return columns
	}
	for _, existing := range columns {
		if existing == column {
			return columns
		}
	}
	return append(columns, column)
}

// formatQueryParameter renders one bound argument for the preview response.
func formatQueryParameter(value any) string {
	switch typed := value.(type) {
	case nil:
		return "NULL"
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

// convertQualityRuleConfigToV1 converts the typed rule parameters into their
// public shape. Exactly one oneof arm is set on either side.
func convertQualityRuleConfigToV1(config *storepb.QualityRuleConfig) *v1pb.QualityRuleConfig {
	if config == nil {
		return nil
	}
	converted := &v1pb.QualityRuleConfig{
		ThresholdMetric: v1pb.QualityThresholdMetric(config.GetThresholdMetric()),
		Filter:          convertQualityFilterToV1(config.GetFilter()),
		OnEmpty:         v1pb.QualityEmptyBehavior(config.GetOnEmpty()),
	}
	switch arm := config.GetConfig().(type) {
	case *storepb.QualityRuleConfig_RowCount:
		converted.Config = &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{
			MinRows:     copyPtr(arm.RowCount.MinRows),
			MaxRows:     copyPtr(arm.RowCount.MaxRows),
			ExactRows:   copyPtr(arm.RowCount.ExactRows),
			WarnMinRows: copyPtr(arm.RowCount.WarnMinRows),
			WarnMaxRows: copyPtr(arm.RowCount.WarnMaxRows),
		}}
	case *storepb.QualityRuleConfig_RowCountChange:
		converted.Config = &v1pb.QualityRuleConfig_RowCountChange{RowCountChange: &v1pb.RowCountChangeConfig{
			MaxIncreasePct:  copyPtr(arm.RowCountChange.MaxIncreasePct),
			MaxDecreasePct:  copyPtr(arm.RowCountChange.MaxDecreasePct),
			WarnIncreasePct: copyPtr(arm.RowCountChange.WarnIncreasePct),
			WarnDecreasePct: copyPtr(arm.RowCountChange.WarnDecreasePct),
		}}
	case *storepb.QualityRuleConfig_NullPercent:
		converted.Config = &v1pb.QualityRuleConfig_NullPercent{NullPercent: &v1pb.NullPercentConfig{
			Column:        arm.NullPercent.GetColumn(),
			MaxNullCount:  copyPtr(arm.NullPercent.MaxNullCount),
			WarnNullCount: copyPtr(arm.NullPercent.WarnNullCount),
			MaxNullPct:    copyPtr(arm.NullPercent.MaxNullPct),
			WarnNullPct:   copyPtr(arm.NullPercent.WarnNullPct),
			NullValues:    append([]string(nil), arm.NullPercent.GetNullValues()...),
		}}
	case *storepb.QualityRuleConfig_Unique:
		converted.Config = &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{
			Column:        arm.Unique.GetColumn(),
			CountNulls:    arm.Unique.GetCountNulls(),
			FailThreshold: copyPtr(arm.Unique.FailThreshold),
			WarnThreshold: copyPtr(arm.Unique.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_UniqueCombination:
		converted.Config = &v1pb.QualityRuleConfig_UniqueCombination{UniqueCombination: &v1pb.UniqueCombinationConfig{
			Columns:       append([]string(nil), arm.UniqueCombination.GetColumns()...),
			AllColumns:    arm.UniqueCombination.GetAllColumns(),
			CountNulls:    arm.UniqueCombination.GetCountNulls(),
			FailThreshold: copyPtr(arm.UniqueCombination.FailThreshold),
			WarnThreshold: copyPtr(arm.UniqueCombination.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_AcceptedValues:
		converted.Config = &v1pb.QualityRuleConfig_AcceptedValues{AcceptedValues: &v1pb.AcceptedValuesConfig{
			Column:        arm.AcceptedValues.GetColumn(),
			Values:        append([]string(nil), arm.AcceptedValues.GetValues()...),
			AllowNull:     arm.AcceptedValues.GetAllowNull(),
			FailThreshold: copyPtr(arm.AcceptedValues.FailThreshold),
			WarnThreshold: copyPtr(arm.AcceptedValues.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_Range:
		converted.Config = &v1pb.QualityRuleConfig_Range{Range: &v1pb.RangeConfig{
			Column:        arm.Range.GetColumn(),
			Min:           copyPtr(arm.Range.Min),
			Max:           copyPtr(arm.Range.Max),
			Inclusive:     arm.Range.GetInclusive(),
			FailThreshold: copyPtr(arm.Range.FailThreshold),
			WarnThreshold: copyPtr(arm.Range.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_Regex:
		converted.Config = &v1pb.QualityRuleConfig_Regex{Regex: &v1pb.RegexConfig{
			Column:        arm.Regex.GetColumn(),
			Pattern:       arm.Regex.GetPattern(),
			FailThreshold: copyPtr(arm.Regex.FailThreshold),
			WarnThreshold: copyPtr(arm.Regex.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_StringLength:
		converted.Config = &v1pb.QualityRuleConfig_StringLength{StringLength: &v1pb.StringLengthConfig{
			Column:        arm.StringLength.GetColumn(),
			MinLen:        copyPtr(arm.StringLength.MinLen),
			MaxLen:        copyPtr(arm.StringLength.MaxLen),
			FailThreshold: copyPtr(arm.StringLength.FailThreshold),
			WarnThreshold: copyPtr(arm.StringLength.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_ReferentialIntegrity:
		converted.Config = &v1pb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &v1pb.ReferentialIntegrityConfig{
			ChildColumn:   arm.ReferentialIntegrity.GetChildColumn(),
			ParentTable:   arm.ReferentialIntegrity.GetParentTable(),
			ParentColumn:  arm.ReferentialIntegrity.GetParentColumn(),
			AllowNull:     arm.ReferentialIntegrity.GetAllowNull(),
			FailThreshold: copyPtr(arm.ReferentialIntegrity.FailThreshold),
			WarnThreshold: copyPtr(arm.ReferentialIntegrity.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_CrossColumn:
		converted.Config = &v1pb.QualityRuleConfig_CrossColumn{CrossColumn: &v1pb.CrossColumnConfig{
			LeftColumn:    arm.CrossColumn.GetLeftColumn(),
			RightColumn:   arm.CrossColumn.GetRightColumn(),
			Op:            v1pb.QualityComparisonOperator(arm.CrossColumn.GetOp()),
			Tolerance:     copyPtr(arm.CrossColumn.Tolerance),
			FailThreshold: copyPtr(arm.CrossColumn.FailThreshold),
			WarnThreshold: copyPtr(arm.CrossColumn.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_Freshness:
		converted.Config = &v1pb.QualityRuleConfig_Freshness{Freshness: &v1pb.FreshnessConfig{
			TimestampColumn: arm.Freshness.GetTimestampColumn(),
			MaxLagSeconds:   arm.Freshness.GetMaxLagSeconds(),
			WarnLagSeconds:  copyPtr(arm.Freshness.WarnLagSeconds),
		}}
	case *storepb.QualityRuleConfig_CustomSql:
		converted.Config = &v1pb.QualityRuleConfig_CustomSql{CustomSql: &v1pb.CustomSQLConfig{
			Sql:           arm.CustomSql.GetSql(),
			FailThreshold: copyPtr(arm.CustomSql.FailThreshold),
			WarnThreshold: copyPtr(arm.CustomSql.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_CelExpression:
		converted.Config = &v1pb.QualityRuleConfig_CelExpression{CelExpression: &v1pb.CelExpressionConfig{
			Expression:    arm.CelExpression.GetExpression(),
			Columns:       append([]string(nil), arm.CelExpression.GetColumns()...),
			SampleRows:    arm.CelExpression.GetSampleRows(),
			FailThreshold: copyPtr(arm.CelExpression.FailThreshold),
			WarnThreshold: copyPtr(arm.CelExpression.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_SchemaDrift:
		converted.Config = &v1pb.QualityRuleConfig_SchemaDrift{SchemaDrift: &v1pb.SchemaDriftConfig{
			ExpectedColumns:   convertSchemaDriftColumnsToV1(arm.SchemaDrift.GetExpectedColumns()),
			CheckTypes:        copyPtr(arm.SchemaDrift.CheckTypes),
			CheckOrder:        arm.SchemaDrift.GetCheckOrder(),
			AllowExtraColumns: arm.SchemaDrift.GetAllowExtraColumns(),
			FailThreshold:     copyPtr(arm.SchemaDrift.FailThreshold),
			WarnThreshold:     copyPtr(arm.SchemaDrift.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_GroupCheck:
		converted.Config = &v1pb.QualityRuleConfig_GroupCheck{GroupCheck: &v1pb.GroupCheckConfig{
			Columns:         append([]string(nil), arm.GroupCheck.GetColumns()...),
			MinRowsPerGroup: copyPtr(arm.GroupCheck.MinRowsPerGroup),
			MaxRowsPerGroup: copyPtr(arm.GroupCheck.MaxRowsPerGroup),
			FailThreshold:   copyPtr(arm.GroupCheck.FailThreshold),
			WarnThreshold:   copyPtr(arm.GroupCheck.WarnThreshold),
		}}
	case *storepb.QualityRuleConfig_Aggregate:
		converted.Config = &v1pb.QualityRuleConfig_Aggregate{Aggregate: &v1pb.AggregateConfig{
			Column:     arm.Aggregate.GetColumn(),
			Function:   v1pb.QualityAggregateFunction(arm.Aggregate.GetFunction()),
			Percentile: copyPtr(arm.Aggregate.Percentile),
			Min:        copyPtr(arm.Aggregate.Min),
			Max:        copyPtr(arm.Aggregate.Max),
			WarnMin:    copyPtr(arm.Aggregate.WarnMin),
			WarnMax:    copyPtr(arm.Aggregate.WarnMax),
			Inclusive:  arm.Aggregate.GetInclusive(),
		}}
	case *storepb.QualityRuleConfig_CrossTable:
		converted.Config = &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
			RightTable:    arm.CrossTable.GetRightTable(),
			LeftFunction:  v1pb.QualityAggregateFunction(arm.CrossTable.GetLeftFunction()),
			LeftColumn:    arm.CrossTable.GetLeftColumn(),
			RightFunction: v1pb.QualityAggregateFunction(arm.CrossTable.GetRightFunction()),
			RightColumn:   arm.CrossTable.GetRightColumn(),
			Tolerance:     copyPtr(arm.CrossTable.Tolerance),
			FailThreshold: copyPtr(arm.CrossTable.FailThreshold),
			WarnThreshold: copyPtr(arm.CrossTable.WarnThreshold),
		}}
	case nil:
	default:
	}
	return converted
}

// convertQualityRuleConfigFromV1 converts the public rule parameters into the
// stored shape.
func convertQualityRuleConfigFromV1(config *v1pb.QualityRuleConfig) *storepb.QualityRuleConfig {
	if config == nil {
		return nil
	}
	converted := &storepb.QualityRuleConfig{
		ThresholdMetric: storepb.QualityThresholdMetric(config.GetThresholdMetric()),
		Filter:          convertQualityFilterFromV1(config.GetFilter()),
		OnEmpty:         storepb.QualityEmptyBehavior(config.GetOnEmpty()),
	}
	switch arm := config.GetConfig().(type) {
	case *v1pb.QualityRuleConfig_RowCount:
		converted.Config = &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			MinRows:     copyPtr(arm.RowCount.MinRows),
			MaxRows:     copyPtr(arm.RowCount.MaxRows),
			ExactRows:   copyPtr(arm.RowCount.ExactRows),
			WarnMinRows: copyPtr(arm.RowCount.WarnMinRows),
			WarnMaxRows: copyPtr(arm.RowCount.WarnMaxRows),
		}}
	case *v1pb.QualityRuleConfig_RowCountChange:
		converted.Config = &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{
			MaxIncreasePct:  copyPtr(arm.RowCountChange.MaxIncreasePct),
			MaxDecreasePct:  copyPtr(arm.RowCountChange.MaxDecreasePct),
			WarnIncreasePct: copyPtr(arm.RowCountChange.WarnIncreasePct),
			WarnDecreasePct: copyPtr(arm.RowCountChange.WarnDecreasePct),
		}}
	case *v1pb.QualityRuleConfig_NullPercent:
		converted.Config = &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column:        arm.NullPercent.GetColumn(),
			MaxNullCount:  copyPtr(arm.NullPercent.MaxNullCount),
			WarnNullCount: copyPtr(arm.NullPercent.WarnNullCount),
			MaxNullPct:    copyPtr(arm.NullPercent.MaxNullPct),
			WarnNullPct:   copyPtr(arm.NullPercent.WarnNullPct),
			NullValues:    append([]string(nil), arm.NullPercent.GetNullValues()...),
		}}
	case *v1pb.QualityRuleConfig_Unique:
		converted.Config = &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
			Column:        arm.Unique.GetColumn(),
			CountNulls:    arm.Unique.GetCountNulls(),
			FailThreshold: copyPtr(arm.Unique.FailThreshold),
			WarnThreshold: copyPtr(arm.Unique.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_UniqueCombination:
		converted.Config = &storepb.QualityRuleConfig_UniqueCombination{UniqueCombination: &storepb.UniqueCombinationConfig{
			Columns:       append([]string(nil), arm.UniqueCombination.GetColumns()...),
			AllColumns:    arm.UniqueCombination.GetAllColumns(),
			CountNulls:    arm.UniqueCombination.GetCountNulls(),
			FailThreshold: copyPtr(arm.UniqueCombination.FailThreshold),
			WarnThreshold: copyPtr(arm.UniqueCombination.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_AcceptedValues:
		converted.Config = &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: &storepb.AcceptedValuesConfig{
			Column:        arm.AcceptedValues.GetColumn(),
			Values:        append([]string(nil), arm.AcceptedValues.GetValues()...),
			AllowNull:     arm.AcceptedValues.GetAllowNull(),
			FailThreshold: copyPtr(arm.AcceptedValues.FailThreshold),
			WarnThreshold: copyPtr(arm.AcceptedValues.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_Range:
		converted.Config = &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{
			Column:        arm.Range.GetColumn(),
			Min:           copyPtr(arm.Range.Min),
			Max:           copyPtr(arm.Range.Max),
			Inclusive:     arm.Range.GetInclusive(),
			FailThreshold: copyPtr(arm.Range.FailThreshold),
			WarnThreshold: copyPtr(arm.Range.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_Regex:
		converted.Config = &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{
			Column:        arm.Regex.GetColumn(),
			Pattern:       arm.Regex.GetPattern(),
			FailThreshold: copyPtr(arm.Regex.FailThreshold),
			WarnThreshold: copyPtr(arm.Regex.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_StringLength:
		converted.Config = &storepb.QualityRuleConfig_StringLength{StringLength: &storepb.StringLengthConfig{
			Column:        arm.StringLength.GetColumn(),
			MinLen:        copyPtr(arm.StringLength.MinLen),
			MaxLen:        copyPtr(arm.StringLength.MaxLen),
			FailThreshold: copyPtr(arm.StringLength.FailThreshold),
			WarnThreshold: copyPtr(arm.StringLength.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_ReferentialIntegrity:
		converted.Config = &storepb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
			ChildColumn:   arm.ReferentialIntegrity.GetChildColumn(),
			ParentTable:   arm.ReferentialIntegrity.GetParentTable(),
			ParentColumn:  arm.ReferentialIntegrity.GetParentColumn(),
			AllowNull:     arm.ReferentialIntegrity.GetAllowNull(),
			FailThreshold: copyPtr(arm.ReferentialIntegrity.FailThreshold),
			WarnThreshold: copyPtr(arm.ReferentialIntegrity.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_CrossColumn:
		converted.Config = &storepb.QualityRuleConfig_CrossColumn{CrossColumn: &storepb.CrossColumnConfig{
			LeftColumn:    arm.CrossColumn.GetLeftColumn(),
			RightColumn:   arm.CrossColumn.GetRightColumn(),
			Op:            storepb.QualityComparisonOperator(arm.CrossColumn.GetOp()),
			Tolerance:     copyPtr(arm.CrossColumn.Tolerance),
			FailThreshold: copyPtr(arm.CrossColumn.FailThreshold),
			WarnThreshold: copyPtr(arm.CrossColumn.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_Freshness:
		converted.Config = &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{
			TimestampColumn: arm.Freshness.GetTimestampColumn(),
			MaxLagSeconds:   arm.Freshness.GetMaxLagSeconds(),
			WarnLagSeconds:  copyPtr(arm.Freshness.WarnLagSeconds),
		}}
	case *v1pb.QualityRuleConfig_CustomSql:
		converted.Config = &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{
			Sql:           arm.CustomSql.GetSql(),
			FailThreshold: copyPtr(arm.CustomSql.FailThreshold),
			WarnThreshold: copyPtr(arm.CustomSql.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_CelExpression:
		converted.Config = &storepb.QualityRuleConfig_CelExpression{CelExpression: &storepb.CelExpressionConfig{
			Expression:    arm.CelExpression.GetExpression(),
			Columns:       append([]string(nil), arm.CelExpression.GetColumns()...),
			SampleRows:    arm.CelExpression.GetSampleRows(),
			FailThreshold: copyPtr(arm.CelExpression.FailThreshold),
			WarnThreshold: copyPtr(arm.CelExpression.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_SchemaDrift:
		converted.Config = &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{
			ExpectedColumns:   convertSchemaDriftColumnsFromV1(arm.SchemaDrift.GetExpectedColumns()),
			CheckTypes:        copyPtr(arm.SchemaDrift.CheckTypes),
			CheckOrder:        arm.SchemaDrift.GetCheckOrder(),
			AllowExtraColumns: arm.SchemaDrift.GetAllowExtraColumns(),
			FailThreshold:     copyPtr(arm.SchemaDrift.FailThreshold),
			WarnThreshold:     copyPtr(arm.SchemaDrift.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_GroupCheck:
		converted.Config = &storepb.QualityRuleConfig_GroupCheck{GroupCheck: &storepb.GroupCheckConfig{
			Columns:         append([]string(nil), arm.GroupCheck.GetColumns()...),
			MinRowsPerGroup: copyPtr(arm.GroupCheck.MinRowsPerGroup),
			MaxRowsPerGroup: copyPtr(arm.GroupCheck.MaxRowsPerGroup),
			FailThreshold:   copyPtr(arm.GroupCheck.FailThreshold),
			WarnThreshold:   copyPtr(arm.GroupCheck.WarnThreshold),
		}}
	case *v1pb.QualityRuleConfig_Aggregate:
		converted.Config = &storepb.QualityRuleConfig_Aggregate{Aggregate: &storepb.AggregateConfig{
			Column:     arm.Aggregate.GetColumn(),
			Function:   storepb.QualityAggregateFunction(arm.Aggregate.GetFunction()),
			Percentile: copyPtr(arm.Aggregate.Percentile),
			Min:        copyPtr(arm.Aggregate.Min),
			Max:        copyPtr(arm.Aggregate.Max),
			WarnMin:    copyPtr(arm.Aggregate.WarnMin),
			WarnMax:    copyPtr(arm.Aggregate.WarnMax),
			Inclusive:  arm.Aggregate.GetInclusive(),
		}}
	case *v1pb.QualityRuleConfig_CrossTable:
		converted.Config = &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
			RightTable:    arm.CrossTable.GetRightTable(),
			LeftFunction:  storepb.QualityAggregateFunction(arm.CrossTable.GetLeftFunction()),
			LeftColumn:    arm.CrossTable.GetLeftColumn(),
			RightFunction: storepb.QualityAggregateFunction(arm.CrossTable.GetRightFunction()),
			RightColumn:   arm.CrossTable.GetRightColumn(),
			Tolerance:     copyPtr(arm.CrossTable.Tolerance),
			FailThreshold: copyPtr(arm.CrossTable.FailThreshold),
			WarnThreshold: copyPtr(arm.CrossTable.WarnThreshold),
		}}
	case nil:
	default:
	}
	return converted
}

// qualitySuggestionPrefix is the resource-name prefix of the review queue.
const qualitySuggestionPrefix = "qualitySuggestions/"

// parseQualitySuggestionName splits
// workspaces/{workspace}/qualitySuggestions/{quality_suggestion}.
func parseQualitySuggestionName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualitySuggestionPrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality suggestion name")
	}
	return tokens[0], tokens[1], nil
}

// formatQualitySuggestionName renders a suggestion resource name.
func formatQualitySuggestionName(workspace, qualitySuggestion string) string {
	return common.FormatWorkspace(workspace) + "/" + qualitySuggestionPrefix + qualitySuggestion
}

// convertQualitySuggestion converts one review-queue row into its public shape.
func convertQualitySuggestion(workspaceID string, suggestion *store.QualitySuggestionMessage) *v1pb.QualitySuggestion {
	if suggestion == nil {
		return nil
	}
	return &v1pb.QualitySuggestion{
		Name:          formatQualitySuggestionName(workspaceID, suggestion.ResourceID),
		Kind:          v1pb.QualitySuggestionKind(suggestion.Kind),
		Source:        v1pb.QualitySuggestionSource(suggestion.Source),
		Target:        &v1pb.QualityTarget{MetaGuid: suggestion.TargetGUID, MetaType: v1pb.MetaType(suggestion.TargetType)},
		Payload:       convertQualitySuggestionPayloadToV1(suggestion.Payload),
		Status:        v1pb.QualitySuggestionStatus(suggestion.Status),
		Provider:      suggestion.Provider,
		Model:         suggestion.Model,
		PromptVersion: suggestion.PromptVersion,
		CreateTime:    timestamppb.New(suggestion.CreatedAt),
		DecidedBy:     formatQualityOwner(suggestion.DecidedBy),
		DecideTime:    timestampOrNil(suggestion.DecidedAt),
		DecisionNote:  suggestion.DecisionNote,
	}
}

// qualitySuggestionJobPrefix is the resource-name prefix of the background
// drafting jobs.
const qualitySuggestionJobPrefix = "qualitySuggestionJobs/"

// parseQualitySuggestionJobName splits
// workspaces/{workspace}/qualitySuggestionJobs/{quality_suggestion_job}.
func parseQualitySuggestionJobName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualitySuggestionJobPrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality suggestion job name")
	}
	return tokens[0], tokens[1], nil
}

// formatQualitySuggestionJobName renders a job resource name.
func formatQualitySuggestionJobName(workspace, qualitySuggestionJob string) string {
	return common.FormatWorkspace(workspace) + "/" + qualitySuggestionJobPrefix + qualitySuggestionJob
}

// convertQualitySuggestionJob converts one stored job into its public shape: the
// target snapshots, their suggestion resource names and the counts the page shows.
func convertQualitySuggestionJob(workspaceID string, job *store.QualitySuggestionJobMessage) *v1pb.QualitySuggestionJob {
	if job == nil {
		return nil
	}
	converted := &v1pb.QualitySuggestionJob{
		Name:          formatQualitySuggestionJobName(workspaceID, job.ResourceID),
		Status:        convertQualitySuggestionJobStatus(job.Status),
		Intent:        job.Intent,
		Language:      job.Language,
		Provider:      job.Provider,
		Model:         job.Model,
		PromptVersion: job.PromptVersion,
		CreateTime:    timestamppb.New(job.CreatedAt),
		StartTime:     timestampOrNil(job.StartedAt),
		FinishTime:    timestampOrNil(job.FinishedAt),
		Error:         job.Error,
		CreatedBy:     formatQualityOwner(job.CreatedBy),
	}
	for _, target := range job.Targets.GetTargets() {
		if target == nil {
			continue
		}
		convertedTarget := &v1pb.QualitySuggestionJobTarget{
			Target: &v1pb.QualityTarget{
				MetaGuid: target.GetTargetGuid(),
				MetaType: v1pb.MetaType(target.GetTargetType()),
				Path:     convertQualityTargetPathToV1(target.GetPath()),
				Columns:  append([]string(nil), target.GetColumns()...),
			},
			State:      convertQualitySuggestionJobTargetState(target.GetState()),
			Error:      target.GetError(),
			StartTime:  unixMillisecondTimestamp(target.GetStartedAtUnixMs()),
			FinishTime: unixMillisecondTimestamp(target.GetFinishedAtUnixMs()),
		}
		for _, resourceID := range target.GetSuggestionResourceIds() {
			convertedTarget.Suggestions = append(convertedTarget.Suggestions,
				formatQualitySuggestionName(workspaceID, resourceID))
		}
		converted.Targets = append(converted.Targets, convertedTarget)
	}
	converted.TargetCount = int32(len(converted.GetTargets()))
	for _, target := range converted.GetTargets() {
		switch target.GetState() {
		case v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
			v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING,
			v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED:
			// Still in flight: not completed yet.
		case v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE,
			v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED,
			v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED:
			converted.CompletedTargetCount++
		default:
			// An unknown state is not counted as completed.
		}
		converted.SuggestionCount += int32(len(target.GetSuggestions()))
	}
	return converted
}

// convertQualitySuggestionJobStatus maps the stored lifecycle enum onto its public
// form. The two enums share their numbering, but the switch keeps an unknown value
// from leaking through a cast.
func convertQualitySuggestionJobStatus(status storepb.QualitySuggestionJobStatus) v1pb.QualitySuggestionJobStatus {
	switch status {
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING:
		return v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING:
		return v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED:
		return v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED:
		return v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED:
		return v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED:
		return v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED
	default:
		return v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED
	}
}

// convertQualitySuggestionJobTargetState maps one stored target state onto its
// public form.
func convertQualitySuggestionJobTargetState(state storepb.QualitySuggestionJobTargetState) v1pb.QualitySuggestionJobTargetState {
	switch state {
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING:
		return v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING:
		return v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE:
		return v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED:
		return v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED:
		return v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED:
		return v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED
	default:
		return v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED
	}
}

// unixMillisecondTimestamp renders a stored epoch-millisecond instant, or nil for
// the zero value (the target has not started/finished).
func unixMillisecondTimestamp(milliseconds int64) *timestamppb.Timestamp {
	if milliseconds <= 0 {
		return nil
	}
	return timestamppb.New(time.UnixMilli(milliseconds).UTC())
}

// convertQualitySuggestionPayloadToV1 converts the review-queue payload.
func convertQualitySuggestionPayloadToV1(payload *storepb.QualitySuggestionPayload) *v1pb.QualitySuggestionPayload {
	if payload == nil {
		return nil
	}
	converted := &v1pb.QualitySuggestionPayload{}
	switch arm := payload.GetPayload().(type) {
	case *storepb.QualitySuggestionPayload_Rule:
		converted.Payload = &v1pb.QualitySuggestionPayload_Rule{Rule: convertQualityRuleDraftConfigToV1(arm.Rule)}
	case *storepb.QualitySuggestionPayload_Label:
		converted.Payload = &v1pb.QualitySuggestionPayload_Label{Label: convertSemanticLabelSuggestionToV1(arm.Label)}
	case nil:
	default:
	}
	return converted
}

// convertQualitySuggestionPayloadFromV1 converts the review-queue payload back
// into the stored shape.
func convertQualitySuggestionPayloadFromV1(payload *v1pb.QualitySuggestionPayload) *storepb.QualitySuggestionPayload {
	if payload == nil {
		return nil
	}
	converted := &storepb.QualitySuggestionPayload{}
	switch arm := payload.GetPayload().(type) {
	case *v1pb.QualitySuggestionPayload_Rule:
		converted.Payload = &storepb.QualitySuggestionPayload_Rule{Rule: convertQualityRuleDraftConfigFromV1(arm.Rule)}
	case *v1pb.QualitySuggestionPayload_Label:
		converted.Payload = &storepb.QualitySuggestionPayload_Label{Label: convertSemanticLabelSuggestionFromV1(arm.Label)}
	case nil:
	default:
	}
	return converted
}

// convertQualityRuleDraftConfigToV1 converts one model-proposed rule draft.
func convertQualityRuleDraftConfigToV1(draft *storepb.QualityRuleDraftConfig) *v1pb.QualityRuleDraftConfig {
	if draft == nil {
		return nil
	}
	return &v1pb.QualityRuleDraftConfig{
		Config:     convertQualityRuleConfigToV1(draft.GetConfig()),
		Severity:   v1pb.QualitySeverity(draft.GetSeverity()),
		Title:      draft.GetTitle(),
		Rationale:  draft.GetRationale(),
		Confidence: draft.GetConfidence(),
	}
}

// convertQualityRuleDraftConfigFromV1 converts one model-proposed rule draft back
// into the stored shape.
func convertQualityRuleDraftConfigFromV1(draft *v1pb.QualityRuleDraftConfig) *storepb.QualityRuleDraftConfig {
	if draft == nil {
		return nil
	}
	return &storepb.QualityRuleDraftConfig{
		Config:     convertQualityRuleConfigFromV1(draft.GetConfig()),
		Severity:   storepb.QualitySeverity(draft.GetSeverity()),
		Title:      draft.GetTitle(),
		Rationale:  draft.GetRationale(),
		Confidence: draft.GetConfidence(),
	}
}

// convertSemanticLabelSuggestionToV1 converts one model-proposed label.
func convertSemanticLabelSuggestionToV1(suggestion *storepb.SemanticLabelSuggestion) *v1pb.SemanticLabelSuggestion {
	if suggestion == nil {
		return nil
	}
	converted := &v1pb.SemanticLabelSuggestion{
		Column:     suggestion.GetColumn(),
		Label:      v1pb.QualitySemanticType(suggestion.GetLabel()),
		Confidence: suggestion.GetConfidence(),
		Evidence:   convertSemanticLabelEvidenceToV1(suggestion.GetEvidence()),
	}
	for _, ruleType := range suggestion.GetSuggestedRuleTypes() {
		converted.SuggestedRuleTypes = append(converted.SuggestedRuleTypes, v1pb.QualityRuleType(ruleType))
	}
	return converted
}

// convertSemanticLabelSuggestionFromV1 converts one model-proposed label back
// into the stored shape.
func convertSemanticLabelSuggestionFromV1(suggestion *v1pb.SemanticLabelSuggestion) *storepb.SemanticLabelSuggestion {
	if suggestion == nil {
		return nil
	}
	converted := &storepb.SemanticLabelSuggestion{
		Column:     suggestion.GetColumn(),
		Label:      storepb.QualitySemanticType(suggestion.GetLabel()),
		Confidence: suggestion.GetConfidence(),
		Evidence:   convertSemanticLabelEvidenceFromV1(suggestion.GetEvidence()),
	}
	for _, ruleType := range suggestion.GetSuggestedRuleTypes() {
		converted.SuggestedRuleTypes = append(converted.SuggestedRuleTypes, storepb.QualityRuleType(ruleType))
	}
	return converted
}

// convertSemanticLabelEvidenceToV1 converts the evidence behind a label.
func convertSemanticLabelEvidenceToV1(evidence *storepb.SemanticLabelEvidence) *v1pb.SemanticLabelEvidence {
	if evidence == nil {
		return nil
	}
	return &v1pb.SemanticLabelEvidence{
		Columns:   append([]string(nil), evidence.GetColumns()...),
		Metrics:   append([]string(nil), evidence.GetMetrics()...),
		Lineage:   append([]string(nil), evidence.GetLineage()...),
		Rationale: evidence.GetRationale(),
	}
}

// convertSemanticLabelEvidenceFromV1 converts the evidence back into the stored
// shape.
func convertSemanticLabelEvidenceFromV1(evidence *v1pb.SemanticLabelEvidence) *storepb.SemanticLabelEvidence {
	if evidence == nil {
		return nil
	}
	return &storepb.SemanticLabelEvidence{
		Columns:   append([]string(nil), evidence.GetColumns()...),
		Metrics:   append([]string(nil), evidence.GetMetrics()...),
		Lineage:   append([]string(nil), evidence.GetLineage()...),
		Rationale: evidence.GetRationale(),
	}
}

// convertColumnProfileToV1 converts one cached column profile.
func convertColumnProfileToV1(profile *store.ColumnProfileMessage) *v1pb.ColumnProfile {
	if profile == nil {
		return nil
	}
	return &v1pb.ColumnProfile{
		TableGuid:     profile.TableGUID,
		MetaType:      v1pb.MetaType(profile.TableType),
		Column:        profile.ColumnName,
		Profile:       convertColumnProfileStatsToV1(profile.Profile),
		SampleMethod:  profile.SampleMethod,
		SampleRows:    profile.SampleRows,
		Truncated:     profile.Truncated,
		EngineVersion: profile.EngineVersion,
		ProfiledAt:    timestamppb.New(profile.ProfiledAt),
	}
}

// convertColumnProfileStatsToV1 converts the measured statistics.
func convertColumnProfileStatsToV1(stats *storepb.ColumnProfile) *v1pb.ColumnProfileStats {
	if stats == nil {
		return nil
	}
	converted := &v1pb.ColumnProfileStats{
		TotalRows:         stats.GetTotalRows(),
		NullCount:         stats.GetNullCount(),
		DistinctCount:     stats.GetDistinctCount(),
		DistinctEstimated: stats.GetDistinctEstimated(),
		MinValue:          stats.GetMinValue(),
		MaxValue:          stats.GetMaxValue(),
		MinLength:         copyPtr(stats.MinLength),
		MaxLength:         copyPtr(stats.MaxLength),
		Patterns:          append([]string(nil), stats.GetPatterns()...),
	}
	for _, value := range stats.GetTopValues() {
		if value == nil {
			continue
		}
		converted.TopValues = append(converted.TopValues, &v1pb.ProfileValue{
			Value: value.GetValue(),
			Count: value.GetCount(),
		})
	}
	return converted
}

// convertColumnProfileStatsFromV1 converts the measured statistics back into the
// stored shape.
func convertColumnProfileStatsFromV1(stats *v1pb.ColumnProfileStats) *storepb.ColumnProfile {
	if stats == nil {
		return nil
	}
	converted := &storepb.ColumnProfile{
		TotalRows:         stats.GetTotalRows(),
		NullCount:         stats.GetNullCount(),
		DistinctCount:     stats.GetDistinctCount(),
		DistinctEstimated: stats.GetDistinctEstimated(),
		MinValue:          stats.GetMinValue(),
		MaxValue:          stats.GetMaxValue(),
		MinLength:         copyPtr(stats.MinLength),
		MaxLength:         copyPtr(stats.MaxLength),
		Patterns:          append([]string(nil), stats.GetPatterns()...),
	}
	for _, value := range stats.GetTopValues() {
		if value == nil {
			continue
		}
		converted.TopValues = append(converted.TopValues, &storepb.ProfileValue{
			Value: value.GetValue(),
			Count: value.GetCount(),
		})
	}
	return converted
}

// convertColumnSemanticLabelToV1 converts one asserted semantic label.
func convertColumnSemanticLabelToV1(label *store.ColumnSemanticLabelMessage) *v1pb.ColumnSemanticLabel {
	if label == nil {
		return nil
	}
	return &v1pb.ColumnSemanticLabel{
		TableGuid:    label.TableGUID,
		MetaType:     v1pb.MetaType(label.TableType),
		Column:       label.ColumnName,
		Label:        v1pb.QualitySemanticType(label.Label),
		Source:       v1pb.QualityLabelSource(label.Source),
		Status:       v1pb.QualityLabelStatus(label.Status),
		Confidence:   label.Confidence,
		Evidence:     convertSemanticLabelEvidenceToV1(label.Evidence),
		AcceptedBy:   formatQualityOwner(label.AcceptedBy),
		AcceptedTime: timestampOrNil(label.AcceptedAt),
		UpdateTime:   timestamppb.New(label.UpdatedAt),
	}
}

// convertSchemaDriftColumnsToV1 converts the expected-column snapshot. Nullability
// keeps its presence: an unset field means the rule does not judge it, and
// materializing a false there would let older snapshots drift on every nullable
// column.
func convertSchemaDriftColumnsToV1(columns []*storepb.SchemaDriftColumn) []*v1pb.SchemaDriftColumn {
	if columns == nil {
		return nil
	}
	converted := make([]*v1pb.SchemaDriftColumn, 0, len(columns))
	for _, column := range columns {
		if column == nil {
			continue
		}
		converted = append(converted, &v1pb.SchemaDriftColumn{
			Name:     column.GetName(),
			Type:     column.GetType(),
			Nullable: column.Nullable,
		})
	}
	return converted
}

// convertSchemaDriftColumnsFromV1 converts the expected-column snapshot back into
// the stored shape, preserving whether the nullable field was present.
func convertSchemaDriftColumnsFromV1(columns []*v1pb.SchemaDriftColumn) []*storepb.SchemaDriftColumn {
	if columns == nil {
		return nil
	}
	converted := make([]*storepb.SchemaDriftColumn, 0, len(columns))
	for _, column := range columns {
		if column == nil {
			continue
		}
		converted = append(converted, &storepb.SchemaDriftColumn{
			Name:     column.GetName(),
			Type:     column.GetType(),
			Nullable: column.Nullable,
		})
	}
	return converted
}

// fillSchemaDriftSnapshot fills a schema_drift rule's expected-column snapshot
// from the target's current columns when the request did not carry one. It
// reports whether it wrote anything, so a caller that has to persist the snapshot
// knows whether the config changed. The snapshot keeps the registry's column
// order, which is what check_order compares against, and records nullability
// explicitly: a snapshot this API wrote is a complete column contract.
func fillSchemaDriftSnapshot(config *storepb.QualityRuleConfig, columns []dataquality.ColumnInfo) bool {
	drift := config.GetSchemaDrift()
	if drift == nil || len(drift.GetExpectedColumns()) > 0 || len(columns) == 0 {
		return false
	}
	expected := make([]*storepb.SchemaDriftColumn, 0, len(columns))
	for _, column := range columns {
		expected = append(expected, &storepb.SchemaDriftColumn{
			Name:     column.Name,
			Type:     column.Type,
			Nullable: proto.Bool(column.Nullable),
		})
	}
	drift.ExpectedColumns = expected
	return true
}
