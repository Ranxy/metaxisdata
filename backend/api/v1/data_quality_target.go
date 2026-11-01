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

// qualityTargetRecentExecutionLimit is how many of the target's newest completed
// executions the recent pass rate covers.
const qualityTargetRecentExecutionLimit = 20

// qualityTargetSelectionLimit caps the exact-target selection one request may
// name. The metadata browser asks for one page at a time, so the bound is well
// above a page and far below the registry.
const qualityTargetSelectionLimit = 200

// ListQualityTargets lists the targets that carry quality rules, one row per
// table or view. The aggregation happens in the store, so the directory costs one
// query rather than paging through every rule.
//
// An exact selection (req.targets) is the metadata browser's batch read: it
// answers the trust signals of the objects on screen in one call, in request order
// and without paging.
func (s *DataQualityService) ListQualityTargets(ctx context.Context, req *connect.Request[v1pb.ListQualityTargetsRequest]) (*connect.Response[v1pb.ListQualityTargetsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	worstStatus, err := qualityTargetWorstStatusFromV1(req.Msg.GetWorstStatus())
	if err != nil {
		return nil, err
	}
	selection, err := qualityTargetSelectionFromV1(req.Msg.GetTargets())
	if err != nil {
		return nil, err
	}

	find := &store.FindQualityTargetMessage{
		WorstStatus:     worstStatus,
		HasOpenIssue:    req.Msg.GetHasOpenIssue(),
		HasPendingDraft: req.Msg.GetHasPendingDraft(),
		Order:           qualityTargetOrderFromV1(req.Msg.GetOrderBy()),
	}
	applyQualityTargetListFilters(find, req.Msg)

	var (
		targets       []*store.QualityTargetMessage
		nextPageToken string
	)
	if len(selection) > 0 {
		// A selection is not a page: the caller named the targets it needs, so
		// paging and ordering are ignored and the answer keeps the request order. A
		// selected key with no live rule is simply absent — that target is not
		// monitored.
		find.TargetKeys = selection
		targets, err = s.store.ListQualityTargets(ctx, find)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list the selected quality targets")
		}
		if err := s.assembleQualityTargetPage(ctx, targets); err != nil {
			return nil, err
		}
		targets = orderQualityTargetSelection(targets, selection)
	} else {
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
		find.Limit = &limitPlusOne
		find.Offset = &offset.offset
		targets, err = s.store.ListQualityTargets(ctx, find)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list the quality targets")
		}
		targets, nextPageToken, err = paginate(targets, offset)
		if err != nil {
			return nil, err
		}
		if err := s.assembleQualityTargetPage(ctx, targets); err != nil {
			return nil, err
		}
	}

	response := &v1pb.ListQualityTargetsResponse{NextPageToken: nextPageToken}
	for _, target := range targets {
		response.QualityTargets = append(response.QualityTargets, convertQualityTargetSummary(workspaceID, target))
	}
	return connect.NewResponse(response), nil
}

// qualityTargetSelectionFromV1 validates and normalises the exact-target
// selection: at most qualityTargetSelectionLimit keys, each naming a GUID and one
// of the four object types a rule can watch, deduplicated in request order.
func qualityTargetSelectionFromV1(requested []*v1pb.QualityTargetKey) ([]store.MetaGUIDKey, error) {
	if len(requested) > qualityTargetSelectionLimit {
		return nil, connectErrorInvalid("at most %d targets may be selected", qualityTargetSelectionLimit)
	}
	selection := make([]store.MetaGUIDKey, 0, len(requested))
	seen := make(map[store.MetaGUIDKey]struct{}, len(requested))
	for _, key := range requested {
		guid := strings.TrimSpace(key.GetTargetGuid())
		if guid == "" {
			return nil, connectErrorInvalid("targets[].target_guid is required")
		}
		switch key.GetMetaType() {
		case v1pb.MetaType_TABLE,
			v1pb.MetaType_EXTERNAL_TABLE,
			v1pb.MetaType_VIEW,
			v1pb.MetaType_MATERIALIZED_VIEW:
		default:
			// Only these four types can carry a rule, so only they have a signal.
			return nil, connectErrorInvalid("targets[].meta_type must be a table, external table, view or materialized view")
		}
		storeKey := store.MetaGUIDKey{GUID: guid, ObjectType: storepb.MetaType(key.GetMetaType())}
		if _, ok := seen[storeKey]; ok {
			continue
		}
		seen[storeKey] = struct{}{}
		selection = append(selection, storeKey)
	}
	return selection, nil
}

// orderQualityTargetSelection returns the rows in the selection's order, so the
// caller can put each signal back on the row it asked about. Keys with no live
// rule are absent from both sides: they are not monitored.
func orderQualityTargetSelection(targets []*store.QualityTargetMessage, selection []store.MetaGUIDKey) []*store.QualityTargetMessage {
	if len(targets) == 0 {
		return targets
	}
	byKey := make(map[store.MetaGUIDKey]*store.QualityTargetMessage, len(targets))
	for _, target := range targets {
		byKey[store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type}] = target
	}
	ordered := make([]*store.QualityTargetMessage, 0, len(targets))
	for _, key := range selection {
		target, ok := byKey[key]
		if !ok {
			continue
		}
		ordered = append(ordered, target)
		delete(byKey, key)
	}
	return ordered
}

// assembleQualityTargetPage fills the fields the aggregate query cannot compute
// for a page of targets: the rule-derived column count, the orphan state, the
// latest run, the quality score, the recent pass rate and column coverage. The
// directory, an exact-target selection and the workspace header all read these
// numbers through this one function, so a target never reports two different
// values on two surfaces.
func (s *DataQualityService) assembleQualityTargetPage(ctx context.Context, targets []*store.QualityTargetMessage) error {
	if len(targets) == 0 {
		return nil
	}
	keys := make([]store.MetaGUIDKey, 0, len(targets))
	for _, target := range targets {
		keys = append(keys, store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type})
	}
	// The aggregate query cannot extract the checked column from every config arm
	// without duplicating that mapping in SQL, so the page's rules are read once and
	// filled in Go from the authoritative config. The same batch answers "does this
	// target still resolve", which the aggregate query cannot know because it only
	// reads DQ tables.
	pageRules, err := s.store.ListQualityRulesByTargets(ctx, keys)
	if err != nil {
		return errors.Wrap(err, "failed to read the targets' rules")
	}
	columns := make(map[store.MetaGUIDKey]map[string]struct{}, len(targets))
	// checkedColumns is the coverage numerator's set: the columns of ENABLED rules.
	// A paused rule no longer watches data, so it covers nothing.
	checkedColumns := make(map[store.MetaGUIDKey]map[string]struct{}, len(targets))
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
		if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			continue
		}
		if checkedColumns[key] == nil {
			checkedColumns[key] = map[string]struct{}{}
		}
		checkedColumns[key][column] = struct{}{}
	}
	orphanReasons, err := s.targetOrphanReasons(ctx, pageRules)
	if err != nil {
		return err
	}
	orphaned := make(map[store.MetaGUIDKey]bool, len(targets))
	for _, rule := range pageRules {
		if orphanReasons[rule.ID] != "" {
			orphaned[store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}] = true
		}
	}
	runSummaries, err := s.store.ListQualityTargetRunSummaries(ctx, keys)
	if err != nil {
		return errors.Wrap(err, "failed to read the targets' latest runs")
	}
	// Only enabled rules contribute to the score; a paused rule is not part of what
	// the target currently promises.
	enabledRules := make([]*store.QualityRuleMessage, 0, len(pageRules))
	for _, rule := range pageRules {
		if rule.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			enabledRules = append(enabledRules, rule)
		}
	}
	latest, err := s.latestQualityExecutions(ctx, enabledRules)
	if err != nil {
		return err
	}
	scores := qualityTargetScores(pageRules, latest)
	tallies, err := s.store.ListQualityTargetExecutionTallies(ctx, keys, qualityTargetRecentExecutionLimit)
	if err != nil {
		return errors.Wrap(err, "failed to read the targets' recent execution tallies")
	}
	// The column denominator and the numerator's guard come from one registry read,
	// so covered can never exceed total even while schema sync is rewriting a
	// target's column list.
	registryColumns, err := s.store.ListMetaRegistryTargetColumns(ctx, keys)
	if err != nil {
		return errors.Wrap(err, "failed to read the targets' registry columns")
	}
	for _, target := range targets {
		key := store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type}
		target.ColumnCount = int32(len(columns[key]))
		target.Orphaned = orphaned[key]
		if summary := runSummaries[key]; summary != nil {
			target.LatestRun = summary
			target.RunCount = summary.RunCount
		}
		score := scores[key]
		target.QualityScore = score.score
		target.ScoredRuleCount = score.scored
		target.RecentPassRate, target.RecentExecutionCount = qualityRecentPassRate(tallies[key])
		target.CoveredColumnCount, target.TotalColumnCount = qualityTargetColumnCoverage(registryColumns[key], checkedColumns[key])
	}
	return nil
}

// qualityTargetColumnCoverage counts a target's column coverage from the registry
// column names of one read: the denominator is every column the registry records
// for it, the numerator only the enabled checks' columns that still exist there. A
// check that names no column, a paused check and a column the registry no longer
// holds cover nothing, which is what keeps covered <= total.
func qualityTargetColumnCoverage(registryColumns, checkedColumns map[string]struct{}) (int32, int32) {
	covered := int32(0)
	for column := range checkedColumns {
		if _, ok := registryColumns[column]; ok {
			covered++
		}
	}
	return covered, int32(len(registryColumns))
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

// qualityTargetWorkspace is one target's assembled read model: the rules with
// their latest results and history, the unresolved issues, the resolved target,
// the schedule and the orphan state. GetQualityTarget maps it to the API and
// ExportQualityReport renders it, so both read the target exactly once and
// exactly the same way.
type qualityTargetWorkspace struct {
	targetGUID string
	metaType   storepb.MetaType
	rules      []*store.QualityRuleMessage
	latest     map[int64]*store.QualityExecutionMessage
	// history is keyed by rule id and empty when the caller asked for no history.
	history       map[int64][]*store.QualityExecutionMessage
	schemaChanged map[int64]bool
	orphanReasons map[int64]string
	openIssues    []*store.QualityIssueMessage
	// path is the target display snapshot. It comes from the rules' snapshot and
	// is replaced by the resolved path when the target still resolves.
	path       *storepb.QualityTargetPath
	resolution *dataquality.Resolution
	// orphanedReason is empty while the target resolves; when set, the target is
	// gone and the workspace is readable only from the stored snapshots.
	orphanedReason string
	schedule       *store.QualityTargetScheduleMessage
	runSummaries   map[store.MetaGUIDKey]*store.QualityTargetRunSummaryMessage
}

// loadQualityTargetWorkspace assembles one target's read model. historyLimit 0
// skips the per-rule execution history; issueLimit bounds the unresolved issues.
// The caller has already resolved and validated the workspace.
func (s *DataQualityService) loadQualityTargetWorkspace(ctx context.Context, targetGUID string, metaType storepb.MetaType, historyLimit, issueLimit int) (*qualityTargetWorkspace, error) {
	workspace := &qualityTargetWorkspace{
		targetGUID: targetGUID,
		metaType:   metaType,
		history:    map[int64][]*store.QualityExecutionMessage{},
	}

	// A target is not a resource of its own: the rules are the durable record of
	// which targets exist, and the registry says where it lives.
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's quality rules")
	}
	workspace.rules = rules
	if workspace.metaType == storepb.MetaType_UNSPECIFIED && len(rules) > 0 {
		workspace.metaType = rules[0].TargetType
	}
	if len(rules) > 0 {
		workspace.path = rules[0].TargetPath
	}
	if resolved, resolveErr := dataquality.ResolveTarget(ctx, s.store, targetGUID, workspace.metaType); resolveErr != nil {
		if !errors.Is(resolveErr, dataquality.ErrOrphaned) {
			return nil, resolveErr
		}
		// The rules stay readable when the target is gone: their target path is a
		// snapshot, and a rule that no longer resolves is a state the reader must
		// be able to fix. With no rule and no registry row there is nothing to show.
		if len(rules) == 0 {
			return nil, resolveErr
		}
		workspace.orphanedReason = resolveErr.Error()
	} else {
		workspace.resolution = resolved
		workspace.path = resolved.Target.Path
	}

	latest, err := s.latestQualityExecutions(ctx, rules)
	if err != nil {
		return nil, err
	}
	workspace.latest = latest
	if historyLimit > 0 {
		ruleIDs := make([]int64, 0, len(rules))
		for _, rule := range rules {
			ruleIDs = append(ruleIDs, rule.ID)
		}
		history, err := s.store.ListRecentQualityExecutionsByRuleIDs(ctx, ruleIDs, historyLimit)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read the rules' recent executions")
		}
		workspace.history = history
	}
	schemaChanged, err := s.targetSchemaChanges(ctx, rules)
	if err != nil {
		return nil, err
	}
	workspace.schemaChanged = schemaChanged
	orphanReasons, err := s.targetOrphanReasons(ctx, rules)
	if err != nil {
		return nil, err
	}
	workspace.orphanReasons = orphanReasons
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
	// rule names complete, so an issue row never renders without a rule title.
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
	workspace.openIssues = liveIssues

	schedule, err := s.store.GetQualityTargetSchedule(ctx, targetGUID, workspace.metaType)
	if err != nil {
		return nil, err
	}
	workspace.schedule = schedule
	runKey := store.MetaGUIDKey{GUID: targetGUID, ObjectType: workspace.metaType}
	runSummaries, err := s.store.ListQualityTargetRunSummaries(ctx, []store.MetaGUIDKey{runKey})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's latest run")
	}
	workspace.runSummaries = runSummaries
	return workspace, nil
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
	workspace, err := s.loadQualityTargetWorkspace(ctx, targetGUID,
		storepb.MetaType(req.Msg.GetMetaType()), qualityTargetRuleHistoryLimit, qualityTargetOpenIssueLimit)
	if err != nil {
		return nil, err
	}
	drafts, labels, err := s.store.CountPendingQualitySuggestions(ctx, targetGUID)
	if err != nil {
		return nil, err
	}

	summary := &v1pb.QualityTargetSummary{
		Target: &v1pb.QualityTarget{
			MetaGuid: targetGUID,
			MetaType: v1pb.MetaType(workspace.metaType),
			Path:     convertQualityTargetPathToV1(workspace.path),
		},
		OpenIssueCount:    int32(len(workspace.openIssues)),
		PendingDraftCount: drafts,
		PendingLabelCount: labels,
		Orphaned:          workspace.orphanedReason != "",
	}
	columns := make(map[string]struct{}, len(workspace.rules))
	// checkedColumns is the coverage numerator: only an enabled rule watches data.
	checkedColumns := make(map[string]struct{}, len(workspace.rules))
	for _, rule := range workspace.rules {
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
			if rule.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
				checkedColumns[column] = struct{}{}
			}
		}
		if workspace.schemaChanged[rule.ID] {
			summary.SchemaChanged = true
		}
	}
	summary.ColumnCount = int32(len(columns))
	summary.Schedule = convertQualityTargetSchedule(workspace.schedule)
	runKey := store.MetaGUIDKey{GUID: targetGUID, ObjectType: workspace.metaType}
	// Column coverage comes from the same registry read the directory and the
	// metadata browser use, so the workspace header cannot disagree with either.
	registryColumns, err := s.store.ListMetaRegistryTargetColumns(ctx, []store.MetaGUIDKey{runKey})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's registry columns")
	}
	summary.CoveredColumnCount, summary.TotalColumnCount = qualityTargetColumnCoverage(registryColumns[runKey], checkedColumns)
	if entry := workspace.runSummaries[runKey]; entry != nil {
		summary.LatestRun = convertQualityRun(workspaceID, entry.Run)
		summary.RunCount = entry.RunCount
	}
	scores := qualityTargetScores(workspace.rules, workspace.latest)
	summary.QualityScore = scores[runKey].score
	summary.ScoredRuleCount = scores[runKey].scored
	tallies, err := s.store.ListQualityTargetExecutionTallies(ctx, []store.MetaGUIDKey{runKey}, qualityTargetRecentExecutionLimit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's recent execution tallies")
	}
	summary.RecentPassRate, summary.RecentExecutionCount = qualityRecentPassRate(tallies[runKey])

	response := &v1pb.GetQualityTargetResponse{
		Summary:        summary,
		OrphanedReason: workspace.orphanedReason,
	}
	if workspace.resolution != nil {
		// The resolved metadata is the only place that has the columns of a view or
		// external table (those have no COLUMN registry rows), so the workspace gets
		// them from here rather than from ListMetadata.
		for _, column := range workspace.resolution.Target.Columns {
			response.TargetColumns = append(response.TargetColumns, &v1pb.QualityTargetColumn{
				Name:     column.Name,
				Type:     column.Type,
				Nullable: column.Nullable,
			})
		}
	}
	ruleNames := make(map[int64]string, len(workspace.rules))
	for _, rule := range workspace.rules {
		ruleNames[rule.ID] = formatQualityRuleName(workspaceID, rule.ResourceID)
		converted := convertQualityRule(workspaceID, rule)
		converted.TargetSchemaChanged = workspace.schemaChanged[rule.ID]
		converted.OrphanedReason = workspace.orphanReasons[rule.ID]
		if execution := workspace.latest[rule.ID]; execution != nil {
			converted.LastResult = convertQualityExecutionSummary(workspaceID, execution)
			summary.WorstStatus = worstQualityStatus(summary.WorstStatus, v1pb.QualityStatus(execution.Status))
			if !execution.FinishedAt.IsZero() {
				if summary.LastRunTime == nil || execution.FinishedAt.After(summary.LastRunTime.AsTime()) {
					summary.LastRunTime = timestamppb.New(execution.FinishedAt.UTC())
				}
			}
		}
		response.QualityRules = append(response.QualityRules, converted)
		if executions := workspace.history[rule.ID]; len(executions) > 0 {
			entry := &v1pb.QualityRuleHistory{Rule: formatQualityRuleName(workspaceID, rule.ResourceID)}
			for _, execution := range executions {
				entry.Executions = append(entry.Executions, convertQualityExecutionSummary(workspaceID, execution))
			}
			response.RuleHistory = append(response.RuleHistory, entry)
		}
	}
	for _, issue := range workspace.openIssues {
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

// ruleLatestResult returns the rule's newest completed execution when it judged
// the rule's current target. A moved rule keeps executions against its previous
// target; those are not a score for what the rule watches now, so the rule is
// unscored until it has run against its new target.
func ruleLatestResult(rule *store.QualityRuleMessage, latest map[int64]*store.QualityExecutionMessage) *store.QualityExecutionMessage {
	execution := latest[rule.ID]
	if execution == nil || execution.TargetGUID != rule.TargetGUID || execution.TargetType != rule.TargetType {
		return nil
	}
	return execution
}

// qualityTargetScore is one target's optional score and its scored-rule count.
type qualityTargetScore struct {
	score  *float64
	scored int32
}

// qualityTargetScores folds the enabled rules' latest judged results per target.
// A rule whose latest execution is ERROR, SKIPPED or NOT_EVALUATED is unscored
// rather than a zero, and a target with nothing judged has no score.
func qualityTargetScores(rules []*store.QualityRuleMessage, latest map[int64]*store.QualityExecutionMessage) map[store.MetaGUIDKey]qualityTargetScore {
	aggregates := make(map[store.MetaGUIDKey]*dataquality.ScoreAggregate)
	for _, rule := range rules {
		if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			continue
		}
		execution := ruleLatestResult(rule, latest)
		if execution == nil {
			continue
		}
		score, judged := dataquality.CheckScore(rule.RuleType, execution.Status, execution.FailedRows, execution.TotalRows)
		if !judged {
			continue
		}
		key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
		aggregate := aggregates[key]
		if aggregate == nil {
			aggregate = &dataquality.ScoreAggregate{}
			aggregates[key] = aggregate
		}
		aggregate.Add(score, rule.Severity, execution.Status == storepb.QualityStatus_QUALITY_STATUS_FAIL)
	}
	scores := make(map[store.MetaGUIDKey]qualityTargetScore, len(aggregates))
	for key, aggregate := range aggregates {
		score, judged := aggregate.Score()
		scores[key] = qualityTargetScore{score: scorePointer(score, judged), scored: aggregate.Scored()}
	}
	return scores
}

// qualityRecentPassRate is the pass share of a target's recent completed
// executions: PASS / (PASS + WARN + FAIL), a ratio in [0, 1]. The second result
// is how many executions were judged. The tally already holds only judged
// verdicts: ERROR, SKIPPED and NOT_EVALUATED are not verdicts and are excluded by
// the store.
func qualityRecentPassRate(tally store.QualityTargetExecutionTally) (*float64, int32) {
	judged := tally.Judged()
	if judged == 0 {
		return nil, 0
	}
	rate := float64(tally.Pass) / float64(judged)
	return &rate, judged
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
		RuleCount:            target.RuleCount,
		EnabledRuleCount:     target.EnabledCount,
		PausedRuleCount:      target.PausedCount,
		ColumnCount:          target.ColumnCount,
		OpenIssueCount:       target.OpenIssueCount,
		PendingDraftCount:    target.PendingDraftCount,
		PendingLabelCount:    target.PendingLabelCount,
		WorstStatus:          v1pb.QualityStatus(target.WorstStatus),
		LastRunTime:          timeToProtoTimestampPtr(target.LastRunTime),
		SchemaChanged:        target.SchemaChanged,
		Orphaned:             target.Orphaned,
		RunCount:             target.RunCount,
		QualityScore:         target.QualityScore,
		ScoredRuleCount:      target.ScoredRuleCount,
		RecentPassRate:       target.RecentPassRate,
		RecentExecutionCount: target.RecentExecutionCount,
		CoveredColumnCount:   target.CoveredColumnCount,
		TotalColumnCount:     target.TotalColumnCount,
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

// AcknowledgeQualityTargetSchema re-baselines a target's checks to its current
// structure after a reviewer accepted the change. It clears the schema-changed
// signal without touching the checks: what moved is the shape they were reviewed
// against, not their definition, so no revision is written. The update is
// idempotent and reports how many checks moved; the audit ledger records who
// accepted the change.
func (s *DataQualityService) AcknowledgeQualityTargetSchema(ctx context.Context, req *connect.Request[v1pb.AcknowledgeQualityTargetSchemaRequest]) (*connect.Response[v1pb.AcknowledgeQualityTargetSchemaResponse], error) {
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
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required when the target has no rules")
	}

	key := store.MetaGUIDKey{GUID: targetGUID, ObjectType: metaType}
	fingerprints, err := s.store.ListMetaRegistrySchemaFingerprints(ctx, []store.MetaGUIDKey{key})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's schema fingerprint")
	}
	fingerprint := fingerprints[key]
	if len(fingerprint) == 0 {
		// Without a fingerprint there is nothing to re-baseline against. The target
		// is either gone from the registry or its database has not been synced
		// since structure fingerprints landed; both are "wait for the syncer", not
		// a client bug the reader can fix by editing a check.
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
			"target %q has no structure fingerprint yet; wait for the next schema sync", targetGUID))
	}
	updated, err := s.store.BaselineQualityRulesToSchemaHash(ctx, targetGUID, metaType, fingerprint)
	if err != nil {
		return nil, errors.Wrap(err, "failed to re-baseline the target's quality rules")
	}
	return connect.NewResponse(&v1pb.AcknowledgeQualityTargetSchemaResponse{
		BaselinedRuleCount: int32(updated),
	}), nil
}
