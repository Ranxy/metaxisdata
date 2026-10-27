package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
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

// ListQualityTargets lists the targets that carry quality rules, one row per
// table or view. The aggregation happens in the store, so the directory costs one
// query rather than paging through every rule.
func (s *DataQualityService) ListQualityTargets(ctx context.Context, req *connect.Request[v1pb.ListQualityTargetsRequest]) (*connect.Response[v1pb.ListQualityTargetsResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   int(req.Msg.GetPageSize()),
		maximum: qualityListMaximum,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindQualityTargetMessage{
		Limit:           &limitPlusOne,
		Offset:          &offset.offset,
		WorstStatus:     storepb.QualityStatus(req.Msg.GetWorstStatus()),
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

	response := &v1pb.ListQualityTargetsResponse{NextPageToken: nextPageToken}
	for _, target := range targets {
		response.QualityTargets = append(response.QualityTargets, convertQualityTargetSummary(target))
	}
	return connect.NewResponse(response), nil
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
	if resolution, resolveErr := dataquality.ResolveTarget(ctx, s.store, targetGUID, metaType); resolveErr != nil {
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
		path = resolution.Target.Path
	}

	latest, err := s.latestQualityExecutions(ctx, rules)
	if err != nil {
		return nil, err
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

	summary := &v1pb.QualityTargetSummary{
		Target: &v1pb.QualityTarget{
			MetaGuid: targetGUID,
			MetaType: v1pb.MetaType(metaType),
			Path:     convertQualityTargetPathToV1(path),
		},
		OpenIssueCount:    int32(len(openIssues)),
		PendingDraftCount: drafts,
		PendingLabelCount: labels,
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
		if rule.TargetPath != nil && rule.TargetPath.Column != "" {
			columns[rule.TargetPath.Column] = struct{}{}
		}
		if schemaChanged[rule.ID] {
			summary.SchemaChanged = true
		}
	}
	summary.ColumnCount = int32(len(columns))

	response := &v1pb.GetQualityTargetResponse{
		Summary:        summary,
		OrphanedReason: orphanedReason,
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

// convertQualityTargetSummary turns one aggregate row into the API message.
func convertQualityTargetSummary(target *store.QualityTargetMessage) *v1pb.QualityTargetSummary {
	return &v1pb.QualityTargetSummary{
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
	}
}

// worstQualityStatus returns the worse of the two statuses, using the same rank
// as the store aggregate: ERROR outranks FAIL, and a status that observed
// nothing ranks below PASS.
func worstQualityStatus(current, candidate v1pb.QualityStatus) v1pb.QualityStatus {
	if qualityStatusRank(candidate) > qualityStatusRank(current) {
		return candidate
	}
	return current
}

// qualityStatusRank maps a status onto its severity for picking a target's worst
// latest result.
func qualityStatusRank(status v1pb.QualityStatus) int {
	switch status {
	case v1pb.QualityStatus_QUALITY_STATUS_ERROR:
		return 5
	case v1pb.QualityStatus_QUALITY_STATUS_FAIL:
		return 4
	case v1pb.QualityStatus_QUALITY_STATUS_WARN:
		return 3
	case v1pb.QualityStatus_QUALITY_STATUS_PASS:
		return 2
	case v1pb.QualityStatus_QUALITY_STATUS_SKIPPED,
		v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return 1
	case v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// timeToProtoTimestampPtr renders a nullable time as a protobuf timestamp.
func timeToProtoTimestampPtr(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(value.UTC())
}
