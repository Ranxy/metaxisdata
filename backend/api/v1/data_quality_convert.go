package v1

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Resource-name prefixes of the three data quality resources. They are local
// because the quality tables carry no workspace column: the workspace segment is
// the API's scope proof, not a stored key.
const (
	qualityRulePrefix       = "qualityRules/"
	qualityRunPrefix        = "qualityRuns/"
	qualityRuleResultPrefix = "qualityRuleResults/"
	qualityIssuePrefix      = "qualityIssues/"

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

// parseQualityRunName splits workspaces/{workspace}/qualityRuns/{quality_run}.
func parseQualityRunName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityRunPrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality run name")
	}
	return tokens[0], tokens[1], nil
}

// parseQualityRuleResultName splits
// workspaces/{workspace}/qualityRuleResults/{quality_rule_result}.
func parseQualityRuleResultName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityRuleResultPrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality rule result name")
	}
	return tokens[0], tokens[1], nil
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

// formatQualityRunName renders a run resource name.
func formatQualityRunName(workspace, qualityRun string) string {
	return common.FormatWorkspace(workspace) + "/" + qualityRunPrefix + qualityRun
}

// formatQualityIssueName renders an issue resource name.
func formatQualityIssueName(workspace, qualityIssue string) string {
	return common.FormatWorkspace(workspace) + "/" + qualityIssuePrefix + qualityIssue
}

// formatQualityRuleResultName renders a result resource name. Results have no
// public resource id column, so the name carries the row's numeric id: it is
// stable for the row's lifetime and the table is insert-only.
func formatQualityRuleResultName(workspace, qualityRuleResult string) string {
	return common.FormatWorkspace(workspace) + "/" + qualityRuleResultPrefix + qualityRuleResult
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

// convertQualityRunCountsToV1 converts the six-state tally.
func convertQualityRunCountsToV1(counts *storepb.QualityRunCounts) *v1pb.QualityRunCounts {
	if counts == nil {
		return nil
	}
	return &v1pb.QualityRunCounts{
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

// convertQualityRuleResult converts one stored result. runName and ruleName are
// resolved by the caller because a result row only stores numeric foreign keys.
func convertQualityRuleResult(workspaceID string, result *store.QualityRuleResultMessage, runName, ruleName string) *v1pb.QualityRuleResult {
	if result == nil {
		return nil
	}
	converted := &v1pb.QualityRuleResult{
		Name:              formatQualityRuleResultName(workspaceID, strconv.FormatInt(result.ID, 10)),
		Run:               runName,
		Rule:              ruleName,
		RuleRevision:      result.RuleRevision,
		RuleEngineVersion: result.RuleEngineVersion,
		Target: &v1pb.QualityTarget{
			MetaGuid: result.TargetGUID,
			MetaType: v1pb.MetaType(result.TargetType),
		},
		Status:        v1pb.QualityStatus(result.Status),
		ObservedValue: observedValue(result.ObservedValue),
		ObservedKind:  v1pb.QualityObservedKind(result.ObservedKind),
		Thresholds:    convertQualityThresholdsToV1(result.Thresholds),
		FailedRows:    result.FailedRows,
		TotalRows:     result.TotalRows,
		Sampled:       result.Sampled,
		SampleRows:    result.SampleRows,
		Truncated:     result.Truncated,
		Samples:       convertQualityFailureSamplesToV1(result.Sample),
		ExecutedSql:   result.ExecutedSQL,
		ErrorCode:     result.ErrorCode,
		ErrorMessage:  result.ErrorMessage,
		Duration:      durationFromMilliseconds(result.DurationMS),
		StartTime:     timestamppb.New(result.StartedAt),
		FinishTime:    timestamppb.New(result.FinishedAt),
	}
	return converted
}

// convertQualityResultSummary projects the newest result onto the compact shape a
// rule list shows.
func convertQualityResultSummary(result *store.QualityRuleResultMessage, runName string) *v1pb.QualityResultSummary {
	if result == nil {
		return nil
	}
	return &v1pb.QualityResultSummary{
		Status:        v1pb.QualityStatus(result.Status),
		ObservedValue: observedValue(result.ObservedValue),
		ObservedKind:  v1pb.QualityObservedKind(result.ObservedKind),
		FailedRows:    result.FailedRows,
		TotalRows:     result.TotalRows,
		Run:           runName,
		FinishTime:    timestamppb.New(result.FinishedAt),
	}
}

// convertQualityIssue converts one issue row. The rule and run names are
// resolved by the caller because an issue stores numeric foreign keys only.
func convertQualityIssue(workspaceID string, issue *store.QualityIssueMessage, ruleName string, runNames map[int64]string) *v1pb.QualityIssue {
	if issue == nil {
		return nil
	}
	return &v1pb.QualityIssue{
		Name:             formatQualityIssueName(workspaceID, issue.ResourceID),
		Rule:             ruleName,
		Target:           &v1pb.QualityTarget{MetaGuid: issue.TargetGUID, MetaType: v1pb.MetaType(issue.TargetType)},
		Status:           v1pb.QualityIssueStatus(issue.Status),
		Severity:         v1pb.QualitySeverity(issue.Severity),
		FirstSeenRun:     qualityRunNameOf(runNames, issue.FirstSeenRunID),
		LastSeenRun:      qualityRunNameOf(runNames, issue.LastSeenRunID),
		FirstSeenTime:    timestamppb.New(issue.FirstSeenAt),
		LastSeenTime:     timestamppb.New(issue.LastSeenAt),
		ResolvedTime:     timestampOrNil(issue.ResolvedAt),
		ResolutionReason: v1pb.QualityResolutionReason(issue.ResolutionReason),
		ResolutionNote:   issue.ResolutionNote,
		AcknowledgedBy:   formatQualityOwner(issue.AcknowledgedBy),
		AcknowledgedTime: timestampOrNil(issue.AcknowledgedAt),
		MutedUntil:       timestampOrNil(issue.MutedUntil),
		OccurrenceCount:  issue.OccurrenceCount,
	}
}

// qualityRunNameOf resolves a nullable numeric run id through the batch map.
func qualityRunNameOf(runNames map[int64]string, runID *int64) string {
	if runID == nil {
		return ""
	}
	return runNames[*runID]
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

// convertQualityRun converts one run row into its public shape.
func convertQualityRun(workspaceID string, run *store.QualityRunMessage) *v1pb.QualityRun {
	if run == nil {
		return nil
	}
	instance := ""
	if run.InstanceResourceID != "" {
		instance = common.FormatInstance(run.InstanceResourceID)
	}
	return &v1pb.QualityRun{
		Name:       formatQualityRunName(workspaceID, run.ResourceID),
		Trigger:    v1pb.QualityRunTrigger(run.Trigger),
		Status:     v1pb.QualityRunStatus(run.Status),
		Counts:     convertQualityRunCountsToV1(run.Counts),
		StartTime:  timestamppb.New(run.StartedAt),
		FinishTime: timestampOrNil(run.FinishedAt),
		Initiator:  formatQualityOwner(run.InitiatorID),
		Instance:   instance,
		Outcome:    v1pb.QualityStatus(run.Outcome),
	}
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
