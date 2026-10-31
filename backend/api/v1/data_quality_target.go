package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityTargetOpenIssueLimit bounds the unresolved issues GetQualityTarget
// returns. A table holds at most 100 rules, and a rule has at most one
// unresolved issue, so the bound is above the reachable count.
const qualityTargetOpenIssueLimit = 200

// qualityTargetRuleHistoryLimit is how many completed executions per rule the
// workspace's history strip shows.
const qualityTargetRuleHistoryLimit = 20

// ListQualityTargets lists the targets that carry quality rules, one row per
// table or view. The aggregation happens in the store, so the directory costs one
// query rather than paging through every rule.
func (s *DataQualityService) ListQualityTargets(ctx context.Context, req *connect.Request[v1pb.ListQualityTargetsRequest]) (*connect.Response[v1pb.ListQualityTargetsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	worstStatus, err := qualityTargetWorstStatusFromV1(req.Msg.GetWorstStatus())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      qualityListMaximum,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindQualityTargetMessage{
		Limit:           &limitPlusOne,
		Offset:          &offset.offset,
		WorstStatus:     worstStatus,
		HasOpenIssue:    req.Msg.GetHasOpenIssue(),
		HasPendingDraft: req.Msg.GetHasPendingDraft(),
		Order:           qualityTargetOrderFromV1(req.Msg.GetOrderBy()),
	}
	applyQualityTargetListFilters(find, req.Msg)
	targets, err := s.store.ListQualityTargets(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality targets")
	}
	targets, nextPageToken, err := paginate(targets, offset)
	if err != nil {
		return nil, err
	}

	// The aggregate query cannot extract the checked column from every config arm
	// without duplicating that mapping in SQL, so the page's rules are read once and
	// the column count is filled in Go from the authoritative config. The same
	// batch answers "does this target still resolve", which the aggregate query
	// cannot know because it only reads DQ tables.
	if len(targets) > 0 {
		keys := make([]store.MetaGUIDKey, 0, len(targets))
		for _, target := range targets {
			keys = append(keys, store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type})
		}
		pageRules, err := s.store.ListQualityRulesByTargets(ctx, keys)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read the targets' rules")
		}
		columns := make(map[store.MetaGUIDKey]map[string]struct{}, len(targets))
		for _, rule := range pageRules {
			column := dataquality.ConfigColumn(rule.Config)
			if column == "" {
				continue
			}
			key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
			if columns[key] == nil {
				columns[key] = map[string]struct{}{}
			}
			columns[key][column] = struct{}{}
		}
		orphanReasons, err := s.targetOrphanReasons(ctx, pageRules)
		if err != nil {
			return nil, err
		}
		orphaned := make(map[store.MetaGUIDKey]bool, len(targets))
		for _, rule := range pageRules {
			if orphanReasons[rule.ID] != "" {
				orphaned[store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}] = true
			}
		}
		runSummaries, err := s.store.ListQualityTargetRunSummaries(ctx, keys)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read the targets' latest runs")
		}
		for _, target := range targets {
			key := store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type}
			target.ColumnCount = int32(len(columns[key]))
			target.Orphaned = orphaned[key]
			if summary := runSummaries[key]; summary != nil {
				target.LatestRun = summary
				target.RunCount = summary.RunCount
			}
		}
	}

	response := &v1pb.ListQualityTargetsResponse{NextPageToken: nextPageToken}
	for _, target := range targets {
		response.QualityTargets = append(response.QualityTargets, convertQualityTargetSummary(workspaceID, target))
	}
	return connect.NewResponse(response), nil
}

// UpdateQualityTargetSchedule replaces one target's schedule: every enabled
// check on the target runs together on this cadence. A change reschedules the
// target from now instead of running it immediately; a target with no enabled
// check keeps the interval but no next run.
func (s *DataQualityService) UpdateQualityTargetSchedule(ctx context.Context, req *connect.Request[v1pb.UpdateQualityTargetScheduleRequest]) (*connect.Response[v1pb.QualityTargetSchedule], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	interval := req.Msg.GetScheduleInterval().AsDuration()
	if err := validateQualityScheduleInterval(interval); err != nil {
		return nil, err
	}
	metaType, err := s.resolveQualityTargetMetaType(ctx, targetGUID, storepb.MetaType(req.Msg.GetMetaType()))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	// A target with rules always has a schedule row; ensuring first keeps the
	// update working for a target whose row predates this model.
	if _, err := s.store.EnsureQualityTargetSchedule(ctx, targetGUID, metaType, interval, now); err != nil {
		return nil, err
	}
	schedule, err := s.store.UpdateQualityTargetSchedule(ctx, targetGUID, metaType, interval, now)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityTargetSchedule(schedule)), nil
}

// GetQualityTargetSchedule reads one target's schedule. It exists so the workspace
// can refresh the schedule after a rule lifecycle change without re-reading the
// target's rules, executions and issues.
func (s *DataQualityService) GetQualityTargetSchedule(ctx context.Context, req *connect.Request[v1pb.GetQualityTargetScheduleRequest]) (*connect.Response[v1pb.QualityTargetSchedule], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	metaType, err := s.resolveQualityTargetMetaType(ctx, targetGUID, storepb.MetaType(req.Msg.GetMetaType()))
	if err != nil {
		return nil, err
	}
	schedule, err := s.store.GetQualityTargetSchedule(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	if schedule == nil {
		// A target whose rules predate the target-level schedule has no row until
		// it is edited; the caller keeps its last known schedule.
		return connect.NewResponse(&v1pb.QualityTargetSchedule{}), nil
	}
	return connect.NewResponse(convertQualityTargetSchedule(schedule)), nil
}

// resolveQualityTargetMetaType fills in the object type a schedule request left
// out. The target's rules carry it; a target with no rules needs it from the
// caller because the GUID alone is not unique across object types.
func (s *DataQualityService) resolveQualityTargetMetaType(ctx context.Context, targetGUID string, metaType storepb.MetaType) (storepb.MetaType, error) {
	if metaType != storepb.MetaType_UNSPECIFIED {
		return metaType, nil
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return storepb.MetaType_UNSPECIFIED, errors.Wrap(err, "failed to read the target's quality rules")
	}
	if len(rules) == 0 {
		return storepb.MetaType_UNSPECIFIED, connectErrorInvalid("meta_type is required when the target has no rules")
	}
	return rules[0].TargetType, nil
}

// qualityTargetWorstStatusFromV1 maps the worst-status filter, rejecting a value
// the enum does not define. UNSPECIFIED means "no filter"; every other known
// status is an exact rank match, so SKIPPED and NOT_EVALUATED are distinct.
func qualityTargetWorstStatusFromV1(status v1pb.QualityStatus) (storepb.QualityStatus, error) {
	switch status {
	case v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, nil
	case v1pb.QualityStatus_QUALITY_STATUS_PASS,
		v1pb.QualityStatus_QUALITY_STATUS_WARN,
		v1pb.QualityStatus_QUALITY_STATUS_FAIL,
		v1pb.QualityStatus_QUALITY_STATUS_ERROR,
		v1pb.QualityStatus_QUALITY_STATUS_SKIPPED,
		v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return storepb.QualityStatus(status), nil
	default:
		return storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
			connectErrorInvalid("unknown quality status %d", status)
	}
}

// GetQualityTarget reads one target's workspace: its rules with their latest
// results, the unresolved issues, and the health summary, in one call. AI drafts
// are not part of the response because reading them needs the suggest permission,
// which this RPC does not require; the client asks for them separately when it
// holds that permission.
func (s *DataQualityService) GetQualityTarget(ctx context.Context, req *connect.Request[v1pb.GetQualityTargetRequest]) (*connect.Response[v1pb.GetQualityTargetResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())

	// A target is not a resource of its own: the rules are the durable record of
	// which targets exist, and the registry says where it lives.
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's quality rules")
	}
	if metaType == storepb.MetaType_UNSPECIFIED && len(rules) > 0 {
		metaType = rules[0].TargetType
	}

	orphanedReason := ""
	var path *storepb.QualityTargetPath
	if len(rules) > 0 {
		path = rules[0].TargetPath
	}
	var resolution *dataquality.Resolution
	if resolved, resolveErr := dataquality.ResolveTarget(ctx, s.store, targetGUID, metaType); resolveErr != nil {
		if !errors.Is(resolveErr, dataquality.ErrOrphaned) {
			return nil, resolveErr
		}
		// The rules stay readable when the target is gone: their target path is a
		// snapshot, and a rule that no longer resolves is a state the reader must
		// be able to fix. With no rule and no registry row there is nothing to show.
		if len(rules) == 0 {
			return nil, resolveErr
		}
		orphanedReason = resolveErr.Error()
	} else {
		resolution = resolved
		path = resolved.Target.Path
	}

	latest, err := s.latestQualityExecutions(ctx, rules)
	if err != nil {
		return nil, err
	}
	ruleIDs := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	history, err := s.store.ListRecentQualityExecutionsByRuleIDs(ctx, ruleIDs, qualityTargetRuleHistoryLimit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the rules' recent executions")
	}
	schemaChanged, err := s.targetSchemaChanges(ctx, rules)
	if err != nil {
		return nil, err
	}
	orphanReasons, err := s.targetOrphanReasons(ctx, rules)
	if err != nil {
		return nil, err
	}
	drafts, labels, err := s.store.CountPendingQualitySuggestions(ctx, targetGUID)
	if err != nil {
		return nil, err
	}
	issueLimit := qualityTargetOpenIssueLimit
	openIssues, err := s.store.ListQualityIssues(ctx, &store.FindQualityIssueMessage{
		TargetGUID: &targetGUID,
		OpenOnly:   true,
		Limit:      &issueLimit,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's open issues")
	}
	// The target's rules exclude archived and deleted ones, so their issues must
	// too: an archived rule keeps its open issue for the audit trail, but it is not
	// part of what this workspace currently promises. Filtering here also keeps
	// ruleNames complete, so an issue row never renders without a rule title.
	liveRuleIDs := make(map[int64]struct{}, len(rules))
	for _, rule := range rules {
		liveRuleIDs[rule.ID] = struct{}{}
	}
	liveIssues := make([]*store.QualityIssueMessage, 0, len(openIssues))
	for _, issue := range openIssues {
		if _, ok := liveRuleIDs[issue.RuleID]; ok {
			liveIssues = append(liveIssues, issue)
		}
	}
	openIssues = liveIssues

	summary := &v1pb.QualityTargetSummary{
		Target: &v1pb.QualityTarget{
			MetaGuid: targetGUID,
			MetaType: v1pb.MetaType(metaType),
			Path:     convertQualityTargetPathToV1(path),
		},
		OpenIssueCount:    int32(len(openIssues)),
		PendingDraftCount: drafts,
		PendingLabelCount: labels,
		Orphaned:          orphanedReason != "",
	}
	columns := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		summary.RuleCount++
		switch rule.State {
		case storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED:
			summary.EnabledRuleCount++
		case storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED:
			summary.PausedRuleCount++
		case storepb.QualityRuleState_QUALITY_RULE_STATE_UNSPECIFIED,
			storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED:
		default:
		}
		if column := dataquality.ConfigColumn(rule.Config); column != "" {
			columns[column] = struct{}{}
		}
		if schemaChanged[rule.ID] {
			summary.SchemaChanged = true
		}
	}
	summary.ColumnCount = int32(len(columns))
	schedule, err := s.store.GetQualityTargetSchedule(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	summary.Schedule = convertQualityTargetSchedule(schedule)
	runKey := store.MetaGUIDKey{GUID: targetGUID, ObjectType: metaType}
	runSummaries, err := s.store.ListQualityTargetRunSummaries(ctx, []store.MetaGUIDKey{runKey})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's latest run")
	}
	if entry := runSummaries[runKey]; entry != nil {
		summary.LatestRun = convertQualityRun(workspaceID, entry.Run)
		summary.RunCount = entry.RunCount
	}

	response := &v1pb.GetQualityTargetResponse{
		Summary:        summary,
		OrphanedReason: orphanedReason,
	}
	if resolution != nil {
		// The resolved metadata is the only place that has the columns of a view or
		// external table (those have no COLUMN registry rows), so the workspace gets
		// them from here rather than from ListMetadata.
		for _, column := range resolution.Target.Columns {
			response.TargetColumns = append(response.TargetColumns, &v1pb.QualityTargetColumn{
				Name:     column.Name,
				Type:     column.Type,
				Nullable: column.Nullable,
			})
		}
	}
	ruleNames := make(map[int64]string, len(rules))
	for _, rule := range rules {
		ruleNames[rule.ID] = formatQualityRuleName(workspaceID, rule.ResourceID)
		converted := convertQualityRule(workspaceID, rule)
		converted.TargetSchemaChanged = schemaChanged[rule.ID]
		converted.OrphanedReason = orphanReasons[rule.ID]
		if execution := latest[rule.ID]; execution != nil {
			converted.LastResult = convertQualityExecutionSummary(workspaceID, execution)
			summary.WorstStatus = worstQualityStatus(summary.WorstStatus, v1pb.QualityStatus(execution.Status))
			if !execution.FinishedAt.IsZero() {
				if summary.LastRunTime == nil || execution.FinishedAt.After(summary.LastRunTime.AsTime()) {
					summary.LastRunTime = timestamppb.New(execution.FinishedAt.UTC())
				}
			}
		}
		response.QualityRules = append(response.QualityRules, converted)
		if executions := history[rule.ID]; len(executions) > 0 {
			entry := &v1pb.QualityRuleHistory{Rule: formatQualityRuleName(workspaceID, rule.ResourceID)}
			for _, execution := range executions {
				entry.Executions = append(entry.Executions, convertQualityExecutionSummary(workspaceID, execution))
			}
			response.RuleHistory = append(response.RuleHistory, entry)
		}
	}
	for _, issue := range openIssues {
		response.OpenIssues = append(response.OpenIssues, convertQualityIssue(workspaceID, issue, ruleNames[issue.RuleID]))
	}
	return connect.NewResponse(response), nil
}

// applyQualityTargetListFilters maps the path prefixes and search onto the store
// filter. Unlike the rule list there is no owner or rule-type filter: those are
// properties of one rule, not of a target.
func applyQualityTargetListFilters(find *store.FindQualityTargetMessage, request *v1pb.ListQualityTargetsRequest) {
	if instance := strings.TrimSpace(request.GetInstance()); instance != "" {
		find.Instance = qualityInstanceSegment(instance)
	}
	find.Database = strings.TrimSpace(request.GetDatabase())
	find.Schema = strings.TrimSpace(request.GetSchema())
	find.TablePrefix = strings.TrimSpace(request.GetTable())
	if search := strings.TrimSpace(request.GetSearch()); search != "" {
		find.Search = &search
	}
}

// qualityTargetOrderFromV1 maps the API order onto the store enum. An unknown
// value falls back to attention, the directory's default.
func qualityTargetOrderFromV1(order v1pb.QualityTargetOrder) store.QualityTargetOrder {
	switch order {
	case v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_NAME:
		return store.QualityTargetOrderName
	case v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_RULE_COUNT:
		return store.QualityTargetOrderRuleCount
	case v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_LAST_RUN:
		return store.QualityTargetOrderLastRun
	case v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_UNSPECIFIED,
		v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_ATTENTION:
		return store.QualityTargetOrderAttention
	default:
		return store.QualityTargetOrderAttention
	}
}

// convertQualityTargetSummary turns one aggregate row into the API message. The
// run fields are filled from the same enrichment the directory performs, so a
// target row and its workspace always agree about the latest batch.
func convertQualityTargetSummary(workspaceID string, target *store.QualityTargetMessage) *v1pb.QualityTargetSummary {
	summary := &v1pb.QualityTargetSummary{
		Target: &v1pb.QualityTarget{
			MetaGuid: target.GUID,
			MetaType: v1pb.MetaType(target.Type),
			Path:     convertQualityTargetPathToV1(target.Path),
		},
		RuleCount:         target.RuleCount,
		EnabledRuleCount:  target.EnabledCount,
		PausedRuleCount:   target.PausedCount,
		ColumnCount:       target.ColumnCount,
		OpenIssueCount:    target.OpenIssueCount,
		PendingDraftCount: target.PendingDraftCount,
		PendingLabelCount: target.PendingLabelCount,
		WorstStatus:       v1pb.QualityStatus(target.WorstStatus),
		LastRunTime:       timeToProtoTimestampPtr(target.LastRunTime),
		SchemaChanged:     target.SchemaChanged,
		Orphaned:          target.Orphaned,
		RunCount:          target.RunCount,
	}
	if target.ScheduleInterval > 0 {
		summary.Schedule = &v1pb.QualityTargetSchedule{
			ScheduleInterval: durationpb.New(target.ScheduleInterval),
			NextRunTime:      timeToProtoTimestampPtr(target.NextRunAt),
		}
	}
	if target.LatestRun != nil {
		summary.LatestRun = convertQualityRun(workspaceID, target.LatestRun.Run)
	}
	return summary
}

// worstQualityStatus returns the worse of the two statuses. The store owns the
// one rank table (the aggregate SQL CASE is generated from it), so the API
// compares through it instead of keeping a second mapping that can drift.
func worstQualityStatus(current, candidate v1pb.QualityStatus) v1pb.QualityStatus {
	if store.QualityTargetStatusRank(storepb.QualityStatus(candidate)) >
		store.QualityTargetStatusRank(storepb.QualityStatus(current)) {
		return candidate
	}
	return current
}

// timeToProtoTimestampPtr renders a nullable time as a protobuf timestamp.
func timeToProtoTimestampPtr(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(value.UTC())
}
