package v1

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// qualityRuleSelectionLimit caps how many rules one manual run may select, so
	// a single request cannot queue an unbounded batch.
	qualityRuleSelectionLimit = 200
	// qualityPreviewProbeTimeout bounds the LIMIT 0 dry-run probe. It proves
	// syntax and permission; it must not scan a table.
	qualityPreviewProbeTimeout = 10 * time.Second
	// qualityListMaximum is the page size ceiling every list here shares.
	qualityListMaximum = 1000
)

// QualityRunScheduler queues a freshly created manual run for immediate
// execution. The quality runner implements it; the API depends only on this
// narrow interface. A nil scheduler means the run is created and left PENDING,
// which is the state before the runner is wired in.
type QualityRunScheduler interface {
	// EnqueueQualityRun queues a run together with the rules it must execute. It
	// reports false when its bounded queue is full, in which case the run is
	// abandoned as a quota failure and the caller gets ResourceExhausted.
	EnqueueQualityRun(runID int64, ruleIDs []int64) bool
}

// DataQualityService serves the data quality rules, the runs that execute them
// and the per-rule results. The quality tables carry no workspace column
// (single-tenant deployment), so every request still proves its workspace from
// the resource name or parent.
type DataQualityService struct {
	store        *store.Store
	dbFactory    *dbfactory.DBFactory
	runScheduler QualityRunScheduler
}

// NewDataQualityService creates the service. runScheduler may be nil.
func NewDataQualityService(s *store.Store, dbFactory *dbfactory.DBFactory, runScheduler QualityRunScheduler) *DataQualityService {
	return &DataQualityService{
		store:        s,
		dbFactory:    dbFactory,
		runScheduler: runScheduler,
	}
}

// connectErrorInvalid builds the InvalidArgument error shared by the handlers
// and the pure mask/name parsers.
func connectErrorInvalid(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.Errorf(format, args...))
}

// workspaceID resolves the workspace a request names. "workspaces/-" is the
// current workspace shorthand the SPA sends; anything else is a stale or
// invented name and is refused instead of silently answered with the current
// workspace's data under a name that is not its own.
func (s *DataQualityService) workspaceID(ctx context.Context, parent string) (string, error) {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return "", connectErrorInvalid("parent is required")
	}

	workspaceID, err := s.store.GetWorkspaceID(ctx)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the workspace"))
	}
	current := common.FormatWorkspace(workspaceID)
	if parent != "workspaces/-" && parent != current {
		return "", connect.NewError(connect.CodeNotFound, errors.Errorf("workspace %q not found", parent))
	}
	return workspaceID, nil
}

// validateResourceWorkspace checks the workspace segment of a resource name. A
// resource name always names the deployment's one workspace; only that segment
// resolves.
func (s *DataQualityService) validateResourceWorkspace(ctx context.Context, workspace string) error {
	workspaceID, err := s.store.GetWorkspaceID(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the workspace"))
	}
	if !isCurrentWorkspace(workspace, workspaceID) {
		return connect.NewError(connect.CodeNotFound, errors.Errorf("workspace %q not found", common.FormatWorkspace(workspace)))
	}
	return nil
}

// ruleResourceID parses a rule name and proves its workspace.
func (s *DataQualityService) ruleResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualityRuleName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

// runResourceID parses a run name and proves its workspace.
func (s *DataQualityService) runResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualityRunName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

func (s *DataQualityService) fetchQualityRule(ctx context.Context, resourceID string) (*store.QualityRuleMessage, error) {
	// Archiving is a soft delete, and an archived rule stays readable so its
	// history can be inspected; the store's write paths still refuse to update a
	// deleted row.
	rule, err := s.store.GetQualityRule(ctx, &store.FindQualityRuleMessage{
		ResourceID:      &resourceID,
		IncludeDeleted:  true,
		IncludeArchived: true,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality rule")
	}
	if rule == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality rule %q not found", resourceID))
	}
	return rule, nil
}

func (s *DataQualityService) fetchQualityRun(ctx context.Context, resourceID string) (*store.QualityRunMessage, error) {
	run, err := s.store.GetQualityRun(ctx, &store.FindQualityRunMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality run")
	}
	if run == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality run %q not found", resourceID))
	}
	return run, nil
}

// ListQualityRules lists rules scoped to the caller's workspace.
func (s *DataQualityService) ListQualityRules(ctx context.Context, req *connect.Request[v1pb.ListQualityRulesRequest]) (*connect.Response[v1pb.ListQualityRulesResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
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

	find := &store.FindQualityRuleMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	if err := applyQualityRuleListFilters(find, req.Msg); err != nil {
		return nil, err
	}
	rules, err := s.store.ListQualityRules(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality rules")
	}
	rules, nextPageToken, err := paginate(rules, offset)
	if err != nil {
		return nil, err
	}

	latest, err := s.latestQualityRuleResults(ctx, rules)
	if err != nil {
		return nil, err
	}
	runNames, err := s.qualityRunNames(ctx, workspaceID, qualityResultRunIDs(latest))
	if err != nil {
		return nil, err
	}
	schemaChanged, err := s.targetSchemaChanges(ctx, rules)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListQualityRulesResponse{NextPageToken: nextPageToken}
	for _, rule := range rules {
		converted := convertQualityRule(workspaceID, rule)
		converted.TargetSchemaChanged = schemaChanged[rule.ID]
		if result := latest[rule.ID]; result != nil {
			converted.LastResult = convertQualityResultSummary(result, runNames[result.RunID])
		}
		response.QualityRules = append(response.QualityRules, converted)
	}
	return connect.NewResponse(response), nil
}

// applyQualityRuleListFilters maps the request filters onto the store filter.
func applyQualityRuleListFilters(find *store.FindQualityRuleMessage, request *v1pb.ListQualityRulesRequest) error {
	// The four path filters are GUID prefixes: the instance is the first GUID
	// segment, so an "instances/{instance}" resource name is accepted too.
	if instance := strings.TrimSpace(request.GetInstance()); instance != "" {
		find.Instance = qualityInstanceSegment(instance)
	}
	find.Database = strings.TrimSpace(request.GetDatabase())
	find.Schema = strings.TrimSpace(request.GetSchema())
	find.TablePrefix = strings.TrimSpace(request.GetTable())

	if guid := strings.TrimSpace(request.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	find.Dimension = storepb.QualityDimension(request.GetDimension())
	find.RuleType = storepb.QualityRuleType(request.GetRuleType())
	find.Severity = storepb.QualitySeverity(request.GetSeverity())

	find.State = storepb.QualityRuleState(request.GetState())
	if find.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED {
		// Archiving is a soft delete: an archived rule is readable only when the
		// caller asks for that state explicitly.
		find.IncludeArchived = true
		find.IncludeDeleted = true
	}
	if owner := strings.TrimSpace(request.GetOwner()); owner != "" {
		id, err := common.GetUserID(owner)
		if err != nil {
			return connectErrorInvalid("invalid owner %q", owner)
		}
		principalID := int32(id)
		find.OwnerID = &principalID
	}
	if search := strings.TrimSpace(request.GetSearch()); search != "" {
		find.Search = &search
	}
	return nil
}

// qualityInstanceSegment accepts either a raw instance id or an
// "instances/{instance}" resource name for a GUID-prefix filter.
func qualityInstanceSegment(instance string) string {
	if strings.HasPrefix(instance, common.InstanceNamePrefix) {
		if id, err := common.GetInstanceID(instance); err == nil {
			return id
		}
	}
	return instance
}

// GetQualityRule reads one rule with its newest result summary.
func (s *DataQualityService) GetQualityRule(ctx context.Context, req *connect.Request[v1pb.GetQualityRuleRequest]) (*connect.Response[v1pb.QualityRule], error) {
	workspaceID, resourceID, err := s.ruleResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	rule, err := s.fetchQualityRule(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	converted, err := s.enrichQualityRule(ctx, workspaceID, rule)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(converted), nil
}

// enrichQualityRule fills the read-only projections of one rule: whether its
// target's schema changed and a summary of its newest result.
func (s *DataQualityService) enrichQualityRule(ctx context.Context, workspaceID string, rule *store.QualityRuleMessage) (*v1pb.QualityRule, error) {
	converted := convertQualityRule(workspaceID, rule)
	latest, err := s.latestQualityRuleResults(ctx, []*store.QualityRuleMessage{rule})
	if err != nil {
		return nil, err
	}
	runNames, err := s.qualityRunNames(ctx, workspaceID, qualityResultRunIDs(latest))
	if err != nil {
		return nil, err
	}
	if result := latest[rule.ID]; result != nil {
		converted.LastResult = convertQualityResultSummary(result, runNames[result.RunID])
	}
	schemaChanged, err := s.targetSchemaChanges(ctx, []*store.QualityRuleMessage{rule})
	if err != nil {
		return nil, err
	}
	converted.TargetSchemaChanged = schemaChanged[rule.ID]
	return converted, nil
}

// CreateQualityRule validates, compiles and stores a rule.
func (s *DataQualityService) CreateQualityRule(ctx context.Context, req *connect.Request[v1pb.CreateQualityRuleRequest]) (*connect.Response[v1pb.QualityRule], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	rule := req.Msg.GetRule()
	if rule == nil {
		return nil, connectErrorInvalid("the rule is required")
	}
	if strings.TrimSpace(rule.GetTitle()) == "" {
		return nil, connectErrorInvalid("title is required")
	}
	interval := rule.GetScheduleInterval().AsDuration()
	if err := validateQualityScheduleInterval(interval); err != nil {
		return nil, err
	}
	config := convertQualityRuleConfigFromV1(rule.GetConfig())
	target := rule.GetTarget()
	if target == nil {
		return nil, connectErrorInvalid("target is required")
	}
	validated, err := dataquality.ValidateRule(ctx, s.store, storepb.QualityRuleType(rule.GetRuleType()),
		strings.TrimSpace(target.GetMetaGuid()), storepb.MetaType(target.GetMetaType()), config, nil)
	if err != nil {
		return nil, err
	}
	schemaHash, err := s.targetSchemaHash(ctx, validated.Target)
	if err != nil {
		return nil, err
	}
	ownerID, err := qualityOwnerID(rule.GetOwner())
	if err != nil {
		return nil, connectErrorInvalid("%v", err)
	}

	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the quality rule id")
	}
	now := time.Now()
	nextRunAt := now.Add(interval)
	callerID := principalIDPointer(currentUserID(ctx))
	created, err := s.store.CreateQualityRule(ctx, &store.QualityRuleMessage{
		ResourceID:       resourceID,
		Title:            rule.GetTitle(),
		Description:      rule.GetDescription(),
		Dimension:        storepb.QualityDimension(rule.GetDimension()),
		RuleType:         storepb.QualityRuleType(rule.GetRuleType()),
		TargetGUID:       validated.Target.GUID,
		TargetType:       validated.Target.MetaType,
		TargetPath:       validated.Target.Path,
		TargetMetaHash:   schemaHash,
		Config:           config,
		Severity:         storepb.QualitySeverity(rule.GetSeverity()),
		State:            storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		OwnerID:          ownerID,
		Tags:             append([]string(nil), rule.GetTags()...),
		ScheduleInterval: interval,
		NextRunAt:        &nextRunAt,
		MuteUntil:        protoTimeOrNil(rule.GetMuteUntil()),
		CreatedBy:        callerID,
		UpdatedBy:        callerID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality rule")
	}
	return connect.NewResponse(convertQualityRule(workspaceID, created)), nil
}

// UpdateQualityRule applies the update mask to one rule, revalidating the
// definition when the mask changes it.
func (s *DataQualityService) UpdateQualityRule(ctx context.Context, req *connect.Request[v1pb.UpdateQualityRuleRequest]) (*connect.Response[v1pb.QualityRule], error) {
	workspaceID, resourceID, err := s.ruleResourceID(ctx, req.Msg.GetRule().GetName())
	if err != nil {
		return nil, err
	}
	current, err := s.fetchQualityRule(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	update, err := parseQualityRuleUpdate(req.Msg.GetRule(), req.Msg.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, err
	}
	if req.Msg.GetRule().GetRevision() != 0 {
		expected := req.Msg.GetRule().GetRevision()
		update.Patch.ExpectedRevision = &expected
	}
	update.Patch.UpdatedBy = principalIDPointer(currentUserID(ctx))

	now := time.Now()
	effectiveState := current.State
	if update.Patch.State != nil {
		effectiveState = *update.Patch.State
	}
	if err := s.revalidateQualityRuleDefinition(ctx, update, current); err != nil {
		return nil, err
	}
	s.scheduleUpdatedQualityRule(ctx, update, current, effectiveState, now)

	updated, err := s.store.UpdateQualityRule(ctx, resourceID, update.Patch)
	if err != nil {
		// A lost optimistic-concurrency race is Aborted for the client, not the
		// AlreadyExists the shared code mapping would produce for a Conflict.
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAborted, err)
		}
		return nil, errors.Wrap(err, "failed to update the quality rule")
	}
	converted, err := s.enrichQualityRule(ctx, workspaceID, updated)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(converted), nil
}

// revalidateQualityRuleDefinition runs the save pipeline on the merged
// definition when the mask changes it, and refreshes the target snapshot the
// store keeps beside it.
func (s *DataQualityService) revalidateQualityRuleDefinition(ctx context.Context, update *qualityRuleUpdate, current *store.QualityRuleMessage) error {
	if !update.DefinitionChanged {
		return nil
	}
	ruleType, guid, metaType, config := mergedQualityRuleDefinition(current, update.Patch)
	validated, err := dataquality.ValidateRule(ctx, s.store, ruleType, guid, metaType, config, nil)
	if err != nil {
		return err
	}
	schemaHash, err := s.targetSchemaHash(ctx, validated.Target)
	if err != nil {
		return err
	}
	targetPath := validated.Target.Path
	update.Patch.TargetGUID = &validated.Target.GUID
	update.Patch.TargetType = &validated.Target.MetaType
	update.Patch.TargetPath = targetPath
	update.Patch.TargetMetaHash = &schemaHash
	return nil
}

// scheduleUpdatedQualityRule recomputes next_run_at when the schedule or the
// lifecycle state changed. Pausing leaves the stored next_run_at alone; enabling
// a paused rule schedules its next slot instead of firing immediately.
func (*DataQualityService) scheduleUpdatedQualityRule(_ context.Context, update *qualityRuleUpdate, current *store.QualityRuleMessage, effectiveState storepb.QualityRuleState, now time.Time) {
	if effectiveState != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
		return
	}
	interval := current.ScheduleInterval
	if update.Patch.ScheduleInterval != nil {
		interval = *update.Patch.ScheduleInterval
	}
	enabling := update.StateChanged && current.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	if !update.ScheduleChanged && !enabling {
		return
	}
	nextRunAt := now.Add(interval)
	update.Patch.NextRunAt = &nextRunAt
}

// DeleteQualityRule archives a rule; history behind it stays readable.
func (s *DataQualityService) DeleteQualityRule(ctx context.Context, req *connect.Request[v1pb.DeleteQualityRuleRequest]) (*connect.Response[emptypb.Empty], error) {
	_, resourceID, err := s.ruleResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	if _, err := s.store.ArchiveQualityRule(ctx, resourceID, int32(currentUserID(ctx))); err != nil {
		return nil, errors.Wrap(err, "failed to archive the quality rule")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// PreviewQualityRule validates, compiles and probes a rule without saving it.
func (s *DataQualityService) PreviewQualityRule(ctx context.Context, req *connect.Request[v1pb.PreviewQualityRuleRequest]) (*connect.Response[v1pb.PreviewQualityRuleResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	rule := req.Msg.GetRule()
	if rule == nil {
		return nil, connectErrorInvalid("the rule is required")
	}
	target := rule.GetTarget()
	if target == nil {
		return nil, connectErrorInvalid("target is required")
	}
	config := convertQualityRuleConfigFromV1(rule.GetConfig())
	validated, err := dataquality.ValidateRule(ctx, s.store, storepb.QualityRuleType(rule.GetRuleType()),
		strings.TrimSpace(target.GetMetaGuid()), storepb.MetaType(target.GetMetaType()), config, nil)
	if err != nil {
		return nil, err
	}
	if err := s.probeQualityRule(ctx, validated); err != nil {
		return nil, err
	}
	return connect.NewResponse(previewQualityRuleResponse(config, validated)), nil
}

// previewQualityRuleResponse renders what the editor shows after a successful
// dry run: the SQL, the bound filter values and the target columns it reads.
func previewQualityRuleResponse(config *storepb.QualityRuleConfig, validated *dataquality.ValidatedRule) *v1pb.PreviewQualityRuleResponse {
	parameters := make([]string, 0, len(validated.Compiled.Args))
	for _, argument := range validated.Compiled.Args {
		parameters = append(parameters, formatQueryParameter(argument))
	}
	return &v1pb.PreviewQualityRuleResponse{
		CompiledSql:       validated.Compiled.SQL,
		ReferencedColumns: qualityRuleReferencedColumns(config),
		Parameters:        parameters,
	}
}

// qualityRuleProbeSQL wraps the compiled statement in the engine's LIMIT 0 probe.
func qualityRuleProbeSQL(validated *dataquality.ValidatedRule) string {
	return dataquality.ProbeStatement(validated.Target.Engine, validated.Compiled)
}

// probeQualityRule runs the compiled statement wrapped in a LIMIT 0 probe against
// the target's READ_ONLY data source. It proves syntax and permission without
// scanning the table.
func (s *DataQualityService) probeQualityRule(ctx context.Context, validated *dataquality.ValidatedRule) error {
	if s.dbFactory == nil {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("a database factory is required to probe a rule"))
	}
	driver, err := s.dbFactory.GetReadOnlyDatabaseDriver(ctx, validated.Resolution.Instance, validated.Resolution.Database)
	if err != nil {
		if errors.Is(err, dbfactory.ErrNoReadOnlyDataSource) {
			return connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err,
				"add a READ_ONLY data source to the instance before previewing a rule"))
		}
		return connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to open the target's read-only data source"))
	}
	defer func() {
		if closeErr := driver.Close(ctx); closeErr != nil {
			slog.Warn("failed to close the read-only data source", clog.WithError(closeErr))
		}
	}()

	queryer, ok := driver.(db.ReadOnlyQueryer)
	if !ok {
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("engine %s does not support read-only queries", validated.Target.Engine))
	}
	_, probeErr := queryer.QueryReadOnly(ctx, qualityRuleProbeSQL(validated), db.QueryOptions{
		MaxRows: 1,
		Timeout: qualityPreviewProbeTimeout,
	})
	if probeErr != nil {
		return qualityProbeError(probeErr)
	}
	return nil
}

// qualityProbeError maps a failed dry run. A connection, credential or
// permission failure is a misconfigured data source rather than an invalid rule,
// so it becomes FailedPrecondition; anything the query itself caused stays
// InvalidArgument. Either message is the sanitized class text, never the driver's
// own words.
func qualityProbeError(err error) error {
	code, message := dataquality.ClassifyError(err)
	switch code {
	case dataquality.ErrCodeAuth, dataquality.ErrCodePermission, dataquality.ErrCodeConnection,
		dataquality.ErrCodeTimeout, dataquality.ErrCodeLimitExceeded, dataquality.ErrCodeNoReadOnly:
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("the dry run could not reach the target data source: %s", message))
	default:
		return connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("the rule's compiled query failed the dry run: %s", message))
	}
}

// RunQualityRules creates one manual run per selected instance and queues it.
func (s *DataQualityService) RunQualityRules(ctx context.Context, req *connect.Request[v1pb.RunQualityRulesRequest]) (*connect.Response[v1pb.RunQualityRulesResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	rules, err := s.selectQualityRulesForRun(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, connectErrorInvalid("no enabled quality rules match the selection")
	}

	groups, instanceOrder := groupQualityRulesByInstance(rules)
	if len(instanceOrder) == 0 {
		return nil, connectErrorInvalid("the selected rules have no resolvable instance")
	}
	callerID := principalIDPointer(currentUserID(ctx))
	now := time.Now()

	response := &v1pb.RunQualityRulesResponse{}
	for _, instance := range instanceOrder {
		group := groups[instance]
		ruleIDs := make([]int64, 0, len(group))
		for _, rule := range group {
			ruleIDs = append(ruleIDs, rule.ID)
		}
		resourceID, err := common.RandomString(12)
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate the quality run id")
		}
		run, err := s.store.CreateQualityRun(ctx, &store.QualityRunMessage{
			ResourceID:         resourceID,
			InstanceResourceID: instance,
			Trigger:            storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL,
			Status:             storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
			InitiatorID:        callerID,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to create the quality run")
		}
		if s.runScheduler != nil && !s.runScheduler.EnqueueQualityRun(run.ID, ruleIDs) {
			s.abandonUnqueueableQualityRun(ctx, run, len(ruleIDs), now)
			return nil, connect.NewError(connect.CodeResourceExhausted,
				errors.New("the quality runner queue is full; retry the run later"))
		}
		response.Runs = append(response.Runs, convertQualityRun(workspaceID, run))
	}
	return connect.NewResponse(response), nil
}

// abandonUnqueueableQualityRun closes a run whose dispatch lost the queue. It is
// best effort: the caller is already receiving ResourceExhausted, and the
// stale-run sweep is the backstop if this write fails.
func (s *DataQualityService) abandonUnqueueableQualityRun(ctx context.Context, run *store.QualityRunMessage, selected int, at time.Time) {
	counts := &storepb.QualityRunCounts{Total: int32(selected)}
	abandonErr := s.store.FinishQualityRun(ctx, run.ID,
		storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED,
		storepb.QualityStatus_QUALITY_STATUS_ERROR, counts, at)
	if abandonErr != nil {
		slog.Warn("failed to abandon an unqueueable quality run",
			slog.Int64("run_id", run.ID), clog.WithError(abandonErr))
	}
}

// selectQualityRulesForRun resolves the request's selection to enabled rules.
func (s *DataQualityService) selectQualityRulesForRun(ctx context.Context, request *v1pb.RunQualityRulesRequest) ([]*store.QualityRuleMessage, error) {
	named := request.GetRules()
	instance := strings.TrimSpace(request.GetInstance())
	targetGUID := strings.TrimSpace(request.GetTargetGuid())
	if len(named) > 0 && (instance != "" || targetGUID != "") {
		return nil, connectErrorInvalid("rules cannot be combined with instance or target_guid")
	}
	if len(named) > qualityRuleSelectionLimit {
		return nil, connect.NewError(connect.CodeResourceExhausted,
			errors.Errorf("select at most %d rules per run, got %d", qualityRuleSelectionLimit, len(named)))
	}

	if len(named) > 0 {
		resourceIDs := make([]string, 0, len(named))
		for _, name := range named {
			_, resourceID, err := s.ruleResourceID(ctx, name)
			if err != nil {
				return nil, err
			}
			resourceIDs = append(resourceIDs, resourceID)
		}
		rules, err := s.store.ListQualityRulesByResourceIDs(ctx, resourceIDs)
		if err != nil {
			return nil, errors.Wrap(err, "failed to resolve the selected quality rules")
		}
		selected := make([]*store.QualityRuleMessage, 0, len(rules))
		for _, rule := range rules {
			if rule.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
				selected = append(selected, rule)
			}
		}
		return selected, nil
	}

	if instance == "" && targetGUID == "" {
		return nil, connectErrorInvalid("at least one of rules, instance or target_guid is required")
	}
	find := &store.FindQualityRuleMessage{State: storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED}
	if instance != "" {
		instanceID, err := common.GetInstanceID(instance)
		if err != nil {
			return nil, connectErrorInvalid("invalid instance %q", instance)
		}
		find.Instance = instanceID
	}
	if targetGUID != "" {
		find.TargetGUID = &targetGUID
	}
	count, err := s.store.CountQualityRules(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count the selected quality rules")
	}
	if count == 0 {
		return nil, nil
	}
	if count > qualityRuleSelectionLimit {
		return nil, connect.NewError(connect.CodeResourceExhausted,
			errors.Errorf("select at most %d rules per run, the selection matches %d", qualityRuleSelectionLimit, count))
	}
	limit := qualityRuleSelectionLimit
	find.Limit = &limit
	return s.store.ListQualityRules(ctx, find)
}

// groupQualityRulesByInstance groups the selection by the instance its target
// lives in. A rule whose GUID has no instance segment is dropped: a rule that was
// validated at save time always has one.
func groupQualityRulesByInstance(rules []*store.QualityRuleMessage) (map[string][]*store.QualityRuleMessage, []string) {
	groups := make(map[string][]*store.QualityRuleMessage)
	order := make([]string, 0, len(rules))
	for _, rule := range rules {
		instance, ok := common.GetInstanceFromGUID(rule.TargetGUID)
		if !ok {
			continue
		}
		if _, exists := groups[instance]; !exists {
			order = append(order, instance)
		}
		groups[instance] = append(groups[instance], rule)
	}
	// A deterministic response order makes the created runs reproducible.
	slices.Sort(order)
	return groups, order
}

// ListQualityRuns lists runs scoped to the caller's workspace.
func (s *DataQualityService) ListQualityRuns(ctx context.Context, req *connect.Request[v1pb.ListQualityRunsRequest]) (*connect.Response[v1pb.ListQualityRunsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
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

	find, err := qualityRunListFilter(req.Msg)
	if err != nil {
		return nil, err
	}
	find.Limit = &limitPlusOne
	find.Offset = &offset.offset

	runs, err := s.store.ListQualityRuns(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality runs")
	}
	runs, nextPageToken, err := paginate(runs, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListQualityRunsResponse{NextPageToken: nextPageToken}
	for _, run := range runs {
		response.QualityRuns = append(response.QualityRuns, convertQualityRun(workspaceID, run))
	}
	return connect.NewResponse(response), nil
}

// qualityRunListFilter maps the request filters onto the store filter.
func qualityRunListFilter(request *v1pb.ListQualityRunsRequest) (*store.FindQualityRunMessage, error) {
	find := &store.FindQualityRunMessage{}
	if status := storepb.QualityRunStatus(request.GetStatus()); status != storepb.QualityRunStatus_QUALITY_RUN_STATUS_UNSPECIFIED {
		find.Statuses = []storepb.QualityRunStatus{status}
	}
	if trigger := storepb.QualityRunTrigger(request.GetTrigger()); trigger != storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_UNSPECIFIED {
		find.Trigger = &trigger
	}
	if instance := strings.TrimSpace(request.GetInstance()); instance != "" {
		instanceID, err := common.GetInstanceID(instance)
		if err != nil {
			return nil, connectErrorInvalid("invalid instance %q", instance)
		}
		find.InstanceResourceID = &instanceID
	}
	if request.GetStartTime() != nil {
		startAt := request.GetStartTime().AsTime()
		find.StartAt = &startAt
	}
	if request.GetEndTime() != nil {
		endAt := request.GetEndTime().AsTime()
		find.EndAt = &endAt
	}
	if initiator := strings.TrimSpace(request.GetInitiator()); initiator != "" {
		id, err := common.GetUserID(initiator)
		if err != nil {
			return nil, connectErrorInvalid("invalid initiator %q", initiator)
		}
		principalID := int32(id)
		find.InitiatorID = &principalID
	}
	if outcome := storepb.QualityStatus(request.GetOutcome()); outcome != storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		find.Outcome = &outcome
	}
	return find, nil
}

// GetQualityRun reads one run.
func (s *DataQualityService) GetQualityRun(ctx context.Context, req *connect.Request[v1pb.GetQualityRunRequest]) (*connect.Response[v1pb.QualityRun], error) {
	workspaceID, resourceID, err := s.runResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	run, err := s.fetchQualityRun(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityRun(workspaceID, run)), nil
}

// ListQualityRuleResults lists the per-rule results, typically for one run.
func (s *DataQualityService) ListQualityRuleResults(ctx context.Context, req *connect.Request[v1pb.ListQualityRuleResultsRequest]) (*connect.Response[v1pb.ListQualityRuleResultsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
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

	find := &store.FindQualityRuleResultMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	runNames := make(map[int64]string)
	ruleNames := make(map[int64]string)
	if requestRun := strings.TrimSpace(req.Msg.GetRun()); requestRun != "" {
		_, runResourceID, err := s.runResourceID(ctx, requestRun)
		if err != nil {
			return nil, err
		}
		run, err := s.fetchQualityRun(ctx, runResourceID)
		if err != nil {
			return nil, err
		}
		find.RunID = &run.ID
		runNames[run.ID] = formatQualityRunName(workspaceID, run.ResourceID)
	}
	if requestRule := strings.TrimSpace(req.Msg.GetRule()); requestRule != "" {
		_, ruleResourceID, err := s.ruleResourceID(ctx, requestRule)
		if err != nil {
			return nil, err
		}
		rule, err := s.fetchQualityRule(ctx, ruleResourceID)
		if err != nil {
			return nil, err
		}
		find.RuleID = &rule.ID
		ruleNames[rule.ID] = formatQualityRuleName(workspaceID, rule.ResourceID)
	}
	if status := storepb.QualityStatus(req.Msg.GetStatus()); status != storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		find.Statuses = []storepb.QualityStatus{status}
	}
	if guid := strings.TrimSpace(req.Msg.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	if req.Msg.GetStartTime() != nil {
		startAt := req.Msg.GetStartTime().AsTime()
		find.StartAt = &startAt
	}
	if req.Msg.GetEndTime() != nil {
		endAt := req.Msg.GetEndTime().AsTime()
		find.EndAt = &endAt
	}

	results, err := s.store.ListQualityRuleResults(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality rule results")
	}
	results, nextPageToken, err := paginate(results, offset)
	if err != nil {
		return nil, err
	}
	if err := s.resolveResultResourceNames(ctx, workspaceID, results, runNames, ruleNames); err != nil {
		return nil, err
	}

	response := &v1pb.ListQualityRuleResultsResponse{NextPageToken: nextPageToken}
	for _, result := range results {
		response.QualityRuleResults = append(response.QualityRuleResults,
			convertQualityRuleResult(workspaceID, result, runNames[result.RunID], ruleNames[result.RuleID]))
	}
	return connect.NewResponse(response), nil
}

// resolveResultResourceNames fills the run and rule resource names a result's
// numeric foreign keys do not carry, batching both lookups for the page.
func (s *DataQualityService) resolveResultResourceNames(ctx context.Context, workspaceID string, results []*store.QualityRuleResultMessage, runNames, ruleNames map[int64]string) error {
	var missingRunIDs, missingRuleIDs []int64
	for _, result := range results {
		if _, ok := runNames[result.RunID]; !ok {
			missingRunIDs = append(missingRunIDs, result.RunID)
		}
		if _, ok := ruleNames[result.RuleID]; !ok {
			missingRuleIDs = append(missingRuleIDs, result.RuleID)
		}
	}
	if len(missingRunIDs) > 0 {
		resolved, err := s.qualityRunNames(ctx, workspaceID, missingRunIDs)
		if err != nil {
			return err
		}
		for runID, name := range resolved {
			runNames[runID] = name
		}
	}
	if len(missingRuleIDs) > 0 {
		resolved, err := s.qualityRuleNames(ctx, workspaceID, missingRuleIDs)
		if err != nil {
			return err
		}
		for ruleID, name := range resolved {
			ruleNames[ruleID] = name
		}
	}
	return nil
}

// qualityRunNames resolves numeric run ids to resource names in one query.
func (s *DataQualityService) qualityRunNames(ctx context.Context, workspaceID string, runIDs []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(runIDs))
	if len(runIDs) == 0 {
		return names, nil
	}
	runs, err := s.store.ListQualityRunsByIDs(ctx, dedupeInt64(runIDs))
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve the quality runs of the results")
	}
	for _, run := range runs {
		names[run.ID] = formatQualityRunName(workspaceID, run.ResourceID)
	}
	return names, nil
}

// qualityRuleNames resolves numeric rule ids to resource names in one query.
func (s *DataQualityService) qualityRuleNames(ctx context.Context, workspaceID string, ruleIDs []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(ruleIDs))
	if len(ruleIDs) == 0 {
		return names, nil
	}
	rules, err := s.store.ListQualityRulesByIDs(ctx, dedupeInt64(ruleIDs))
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve the quality rules of the results")
	}
	for _, rule := range rules {
		names[rule.ID] = formatQualityRuleName(workspaceID, rule.ResourceID)
	}
	return names, nil
}

// latestQualityRuleResults returns each rule's newest result keyed by rule id.
func (s *DataQualityService) latestQualityRuleResults(ctx context.Context, rules []*store.QualityRuleMessage) (map[int64]*store.QualityRuleResultMessage, error) {
	ruleIDs := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	latest, err := s.store.ListLatestQualityRuleResults(ctx, ruleIDs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the rules' latest results")
	}
	return latest, nil
}

// qualityResultRunIDs collects the distinct run ids of the given latest results.
func qualityResultRunIDs(latest map[int64]*store.QualityRuleResultMessage) []int64 {
	runIDs := make([]int64, 0, len(latest))
	for _, result := range latest {
		runIDs = append(runIDs, result.RunID)
	}
	return runIDs
}

// dedupeInt64 removes duplicate ids while preserving their first-seen order.
func dedupeInt64(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	deduped := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	return deduped
}

// targetSchemaChanges reports, per rule id, whether the target's stored DDL hash
// differs from the hash snapshotted when the rule was saved. Rules without a
// snapshot, or whose target has no stored schema, are absent from the map.
func (s *DataQualityService) targetSchemaChanges(ctx context.Context, rules []*store.QualityRuleMessage) (map[int64]bool, error) {
	changed := make(map[int64]bool, len(rules))
	snapshots := make(map[int64][]byte, len(rules))
	keys := make([]store.MetaGUIDKey, 0, len(rules))
	index := make(map[store.MetaGUIDKey][]int64, len(rules))
	for _, rule := range rules {
		if len(rule.TargetMetaHash) == 0 {
			continue
		}
		key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
		if _, ok := index[key]; !ok {
			keys = append(keys, key)
		}
		index[key] = append(index[key], rule.ID)
		snapshots[rule.ID] = rule.TargetMetaHash
	}
	if len(keys) == 0 {
		return changed, nil
	}
	hashes, err := s.store.ListMetaRegistrySchemaHashes(ctx, keys)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the targets' current schema hashes")
	}
	for key, ruleIDs := range index {
		current, ok := hashes[key]
		if !ok {
			continue
		}
		for _, ruleID := range ruleIDs {
			changed[ruleID] = !bytes.Equal(current, snapshots[ruleID])
		}
	}
	return changed, nil
}

// targetSchemaHash reads the target's current DDL hash for the rule snapshot.
func (s *DataQualityService) targetSchemaHash(ctx context.Context, target dataquality.ResolvedTarget) ([]byte, error) {
	key := store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.MetaType}
	hashes, err := s.store.ListMetaRegistrySchemaHashes(ctx, []store.MetaGUIDKey{key})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's schema hash")
	}
	return hashes[key], nil
}

// protoTimeOrNil converts an optional protobuf timestamp to the store's optional
// time. An unset timestamp stays nil instead of becoming the epoch.
func protoTimeOrNil(timestamp *timestamppb.Timestamp) *time.Time {
	if timestamp == nil {
		return nil
	}
	value := timestamp.AsTime()
	return &value
}
