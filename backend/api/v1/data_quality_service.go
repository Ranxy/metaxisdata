package v1

import (
	"bytes"
	"context"
	"fmt"
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
	llmcomp "github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"
	"github.com/Ranxy/metaxisdata/backend/utils"
)

const (
	// qualityRuleSelectionLimit caps how many rules one manual execution request
	// may select, so a single request cannot queue an unbounded batch.
	qualityRuleSelectionLimit = 200
	// Quality rule counts are bounded so one workspace or one table cannot make the
	// scheduler unmanageable.
	qualityRuleWorkspaceLimit = 1000
	qualityRuleTableLimit     = 100
	// qualityPreviewProbeTimeout bounds the LIMIT 0 dry-run probe. It proves
	// syntax and permission; it must not scan a table.
	qualityPreviewProbeTimeout = 10 * time.Second
	// qualityListMaximum is the page size ceiling every list here shares.
	qualityListMaximum = 1000
	// qualityProfileSampleRows bounds the sample a profile request reads; the
	// aggregate query scans the table, the sample only feeds Top-K and patterns.
	qualityProfileSampleRows = 1000
	// qualityProfileQueryTimeout bounds one profile query.
	qualityProfileQueryTimeout = 120 * time.Second
	// qualityPreviewConnectionWait bounds how long a dry-run probe waits for one of
	// the instance's connection slots. The probe itself is a LIMIT 0 statement, so
	// waiting longer than the probe could take buys nothing.
	qualityPreviewConnectionWait = 3 * time.Second
	// qualityProfileConnectionWait bounds how long a profile waits for one of the
	// instance's connection slots.
	qualityProfileConnectionWait = 10 * time.Second
	// qualityProfileMaxBytes bounds the rows a profile query keeps in memory.
	qualityProfileMaxBytes = 4 << 20
	// qualityLLMMaxOutputTokens bounds what one analysis may generate. The
	// prompts ask for a small JSON document; this keeps a runaway model cheap.
	qualityLLMMaxOutputTokens = 4096
)

// QualityExecutionScheduler queues a freshly planned manual batch for immediate
// execution. The quality runner implements it; the API depends only on this
// narrow interface. A nil scheduler means the executions are planned and left
// PENDING, which is the state before the runner is wired in.
type QualityExecutionScheduler interface {
	// EnqueueQualityExecutions queues one internal batch together with the
	// instance the API grouped its rules on and the execution rows it planned. It
	// reports false when its bounded queue is full, in which case the batch is
	// abandoned as a quota failure and the caller gets ResourceExhausted. The
	// runner admits the batch against the instance's connection budget; a batch
	// that has to wait stays PENDING and is not reported as a failure.
	EnqueueQualityExecutions(batchID int64, instanceResourceID string, executionIDs []int64) bool
}

// DataQualityService serves the data quality rules, the executions that check
// them and the durable issues those checks open. The quality tables carry no
// workspace column (single-tenant deployment), so every request still proves its
// workspace from the resource name or parent.
type DataQualityService struct {
	store              *store.Store
	dbFactory          *dbfactory.DBFactory
	executionScheduler QualityExecutionScheduler
	// stateCfg is the instance connection governor. The preview probe and the
	// profile open a target driver on a user's behalf, so they lease a slot from
	// the same budget the runners use.
	stateCfg *state.State
	// llmRegistry lists the enabled provider profiles the LLM analyses can use.
	// It is nil in tests that never reach an LLM path.
	llmRegistry *llmcomp.Registry
	// llmQuota is the process-wide daily quota and concurrency limit of the LLM
	// analyses. It is created with the service and shared by every request.
	llmQuota *qualityLLMQuota
	// metrics records execution, query and LLM outcomes; a nil value disables
	// instrumentation.
	metrics *metrics.Metrics
}

// NewDataQualityService creates the service. executionScheduler may be nil.
func NewDataQualityService(s *store.Store, dbFactory *dbfactory.DBFactory, executionScheduler QualityExecutionScheduler, llmRegistry *llmcomp.Registry, stateCfg *state.State, m *metrics.Metrics) *DataQualityService {
	quota := newQualityLLMQuota()
	quota.metrics = m
	return &DataQualityService{
		store:              s,
		dbFactory:          dbFactory,
		executionScheduler: executionScheduler,
		llmRegistry:        llmRegistry,
		stateCfg:           stateCfg,
		llmQuota:           quota,
		metrics:            m,
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

// executionResourceID parses an execution name, proving its workspace, and
// returns the numeric row id.
func (s *DataQualityService) executionResourceID(ctx context.Context, name string) (string, int64, error) {
	workspace, executionID, err := parseQualityExecutionID(name)
	if err != nil {
		return "", 0, err
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", 0, err
	}
	return workspace, executionID, nil
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

func (s *DataQualityService) fetchQualityExecution(ctx context.Context, executionID int64) (*store.QualityExecutionMessage, error) {
	execution, err := s.store.GetQualityExecution(ctx, executionID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality execution")
	}
	if execution == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality execution %d not found", executionID))
	}
	return execution, nil
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

	latest, err := s.latestQualityExecutions(ctx, rules)
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
		if execution := latest[rule.ID]; execution != nil {
			converted.LastResult = convertQualityExecutionSummary(workspaceID, execution)
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
	owner := strings.TrimSpace(request.GetOwner())
	if owner != "" && request.GetOwnerUnassigned() {
		return connectErrorInvalid("owner and owner_unassigned cannot be combined")
	}
	if owner != "" {
		id, err := common.GetUserID(owner)
		if err != nil {
			return connectErrorInvalid("invalid owner %q", owner)
		}
		principalID := int32(id)
		find.OwnerID = &principalID
	}
	find.OwnerUnassigned = request.GetOwnerUnassigned()
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
// target's schema changed and a summary of its newest execution.
func (s *DataQualityService) enrichQualityRule(ctx context.Context, workspaceID string, rule *store.QualityRuleMessage) (*v1pb.QualityRule, error) {
	converted := convertQualityRule(workspaceID, rule)
	latest, err := s.latestQualityExecutions(ctx, []*store.QualityRuleMessage{rule})
	if err != nil {
		return nil, err
	}
	if execution := latest[rule.ID]; execution != nil {
		converted.LastResult = convertQualityExecutionSummary(workspaceID, execution)
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
	settings, settingsErr := dataquality.LoadSettings(ctx, s.store)
	if settingsErr != nil {
		return nil, errLoadQualitySettings(settingsErr)
	}
	interval := resolveCreateScheduleInterval(rule.GetScheduleInterval().AsDuration(), settings)
	if err := validateQualityScheduleInterval(interval); err != nil {
		return nil, err
	}
	config := convertQualityRuleConfigFromV1(rule.GetConfig())
	target := rule.GetTarget()
	if target == nil {
		return nil, connectErrorInvalid("target is required")
	}
	validated, _, err := s.validateRuleInput(ctx, storepb.QualityRuleType(rule.GetRuleType()),
		strings.TrimSpace(target.GetMetaGuid()), storepb.MetaType(target.GetMetaType()), config)
	if err != nil {
		return nil, err
	}
	if err := requireReadOnlySourceForNewRule(storepb.QualityRuleType(rule.GetRuleType()), validated.Resolution); err != nil {
		return nil, err
	}
	if err := s.checkQualityRuleLimits(ctx, validated.Target); err != nil {
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

// validateRuleInput is the save/preview pipeline plus the one API-side step it
// needs: a schema_drift rule created without an expected-column snapshot gets the
// target's current columns. Resolution therefore happens before validation, and
// the filled snapshot is included in what the store receives. It reports whether
// it filled the snapshot, so an update can persist a config it did not receive.
func (s *DataQualityService) validateRuleInput(ctx context.Context, ruleType storepb.QualityRuleType, guid string, metaType storepb.MetaType, config *storepb.QualityRuleConfig) (*dataquality.ValidatedRule, bool, error) {
	filled := false
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT && config != nil {
		resolution, err := dataquality.ResolveTarget(ctx, s.store, guid, metaType)
		if err != nil {
			return nil, false, err
		}
		filled = fillSchemaDriftSnapshot(config, resolution.Target.Columns)
	}
	validated, err := dataquality.ValidateRule(ctx, s.store, ruleType, guid, metaType, config, nil)
	if err != nil {
		return nil, filled, err
	}
	return validated, filled, nil
}

// checkQualityRuleLimits refuses a create that would exceed the workspace's or
// the target table's rule budget, and refuses a query rule whose instance has no
// read-only data source: such a rule could only ever record ERROR at runtime.
func (s *DataQualityService) checkQualityRuleLimits(ctx context.Context, target dataquality.ResolvedTarget) error {
	workspaceRules, err := s.store.CountQualityRules(ctx, &store.FindQualityRuleMessage{})
	if err != nil {
		return err
	}
	if workspaceRules >= qualityRuleWorkspaceLimit {
		return connect.NewError(connect.CodeResourceExhausted,
			errors.Errorf("a workspace allows at most %d quality rules", qualityRuleWorkspaceLimit))
	}
	targetGUID := target.GUID
	tableRules, err := s.store.CountQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return err
	}
	if tableRules >= qualityRuleTableLimit {
		return connect.NewError(connect.CodeResourceExhausted,
			errors.Errorf("a table allows at most %d quality rules", qualityRuleTableLimit))
	}
	return nil
}

// requireReadOnlySourceForNewRule rejects a query rule whose instance has no
// READ_ONLY data source. The rule is validated here rather than left to fail at
// runtime, because the failure is a configuration mistake, not a data problem.
func requireReadOnlySourceForNewRule(ruleType storepb.QualityRuleType, resolution *dataquality.Resolution) error {
	if dataquality.IsMetadataRule(ruleType) {
		return nil
	}
	if resolution == nil || resolution.Instance == nil {
		return connectErrorInvalid("the rule target has no instance")
	}
	if utils.DataSourceFromInstanceWithType(resolution.Instance, storepb.DataSourceType_READ_ONLY) == nil {
		return connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
			"instance %q has no READ_ONLY data source; add one before creating a rule that queries it", resolution.Instance.ResourceID))
	}
	return nil
}

// revalidateQualityRuleDefinition runs the save pipeline on the merged
// definition when the mask changes it, and refreshes the target snapshot the
// store keeps beside it.
func (s *DataQualityService) revalidateQualityRuleDefinition(ctx context.Context, update *qualityRuleUpdate, current *store.QualityRuleMessage) error {
	if !update.DefinitionChanged {
		return nil
	}
	ruleType, guid, metaType, config := mergedQualityRuleDefinition(current, update.Patch)
	validated, filledSnapshot, err := s.validateRuleInput(ctx, ruleType, guid, metaType, config)
	if err != nil {
		return err
	}
	if filledSnapshot {
		// The snapshot was filled here rather than sent by the caller, so it has
		// to be part of the patch or the store would keep the empty config.
		update.Patch.Config = config
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
	ruleType := storepb.QualityRuleType(rule.GetRuleType())
	validated, _, err := s.validateRuleInput(ctx, ruleType,
		strings.TrimSpace(target.GetMetaGuid()), storepb.MetaType(target.GetMetaType()), config)
	if err != nil {
		return nil, err
	}
	// A metadata rule compiles to no SQL and observes the registry, so a dry run
	// must not open the target's data source at all: it only resolves the target
	// and validates the config.
	if dryRunNeedsProbe(ruleType) {
		if err := s.probeQualityRule(ctx, validated); err != nil {
			return nil, err
		}
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

// dryRunNeedsProbe reports whether PreviewQualityRule must probe the target's
// data source. Metadata rules compile to no SQL, so they skip the probe.
func dryRunNeedsProbe(ruleType storepb.QualityRuleType) bool {
	return !dataquality.IsMetadataRule(ruleType)
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
	connection, err := s.acquireQualityConnection(ctx, validated.Resolution.Instance, "the dry run", qualityPreviewConnectionWait)
	if err != nil {
		return err
	}
	defer connection()
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
	_, probeErr := queryer.QueryReadOnly(ctx, qualityRuleProbeSQL(validated), validated.Compiled.Args, db.QueryOptions{
		MaxRows: 1,
		Timeout: qualityPreviewProbeTimeout,
		Purpose: metrics.QueryPurposeDryRunProbe,
		Metrics: s.metrics,
	})
	if probeErr != nil {
		return qualityProbeError(probeErr, validated)
	}
	return nil
}

// qualityProbeError maps a failed dry run. A connection, credential or
// permission failure is a misconfigured data source rather than an invalid rule,
// so it becomes FailedPrecondition; anything the query itself caused stays
// InvalidArgument.
//
// The message carries the compiled statement and its bound values: a dry-run
// failure is usually a parameter problem, and the sanitized class alone ("the
// query failed") leaves the author with nothing to fix. The statement is the
// user's own rule, so echoing it leaks nothing they did not write; the values are
// truncated.
func qualityProbeError(err error, validated *dataquality.ValidatedRule) error {
	code, message := dataquality.ClassifyError(err)
	detail := qualityProbeErrorDetail(validated)
	switch code {
	case dataquality.ErrCodeAuth, dataquality.ErrCodePermission, dataquality.ErrCodeConnection,
		dataquality.ErrCodeTimeout, dataquality.ErrCodeLimitExceeded, dataquality.ErrCodeNoReadOnly:
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("the dry run could not reach the target data source: %s%s", message, detail))
	default:
		return connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("the rule's compiled query failed the dry run: %s%s", message, detail))
	}
}

// qualityProbeErrorDetail renders the compiled statement and its parameters for a
// dry-run error, bounded so a long custom statement cannot make the message
// unusable.
func qualityProbeErrorDetail(validated *dataquality.ValidatedRule) string {
	if validated == nil {
		return ""
	}
	statement := truncateForError(strings.TrimSpace(validated.Compiled.SQL), 2048)
	parameters := make([]string, 0, len(validated.Compiled.Args))
	for _, argument := range validated.Compiled.Args {
		parameters = append(parameters, truncateForError(formatQueryParameter(argument), 128))
	}
	if statement == "" {
		return ""
	}
	return fmt.Sprintf(" (compiled SQL: %s; parameters: [%s])", statement, strings.Join(parameters, ", "))
}

// truncateForError bounds a fragment carried by an error message.
func truncateForError(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

// RunQualityRules plans one execution per selected rule, queues the executions
// grouped by instance and returns them as PENDING. Clients poll
// GetQualityExecution for each execution's state and outcome.
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
		batch, planned, err := s.planManualQualityExecutions(ctx, instance, group, callerID, now)
		if err != nil {
			return nil, err
		}
		if s.executionScheduler != nil && !s.executionScheduler.EnqueueQualityExecutions(batch.ID, instance, qualityExecutionIDs(planned)) {
			for range planned {
				s.metrics.CountQualityExecutionAbandoned(metrics.QualityAbandonedQueueFull)
			}
			s.abandonQualityBatch(ctx, batch.ID, len(planned), now)
			return nil, connect.NewError(connect.CodeResourceExhausted,
				errors.New("the quality runner queue is full; retry the run later"))
		}
		ruleNames := make(map[int64]string, len(group))
		for _, rule := range group {
			ruleNames[rule.ID] = formatQualityRuleName(workspaceID, rule.ResourceID)
		}
		for _, execution := range planned {
			response.Executions = append(response.Executions, convertQualityExecution(workspaceID, execution, ruleNames[execution.RuleID]))
		}
	}
	return connect.NewResponse(response), nil
}

// planManualQualityExecutions creates the internal dispatch batch for one
// instance and plans one PENDING execution per selected rule. The batch admits
// the instance's connection lease and is never exposed; the executions are what
// the caller receives.
func (s *DataQualityService) planManualQualityExecutions(ctx context.Context, instance string, rules []*store.QualityRuleMessage, initiator *int32, at time.Time) (*store.QualityRunMessage, []*store.QualityExecutionMessage, error) {
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to generate the quality batch id")
	}
	batch, err := s.store.CreateQualityRun(ctx, &store.QualityRunMessage{
		ResourceID:         resourceID,
		InstanceResourceID: instance,
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		Status:             storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		InitiatorID:        initiator,
	})
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create the quality batch")
	}

	planned := make([]*store.QualityExecutionMessage, 0, len(rules))
	for _, rule := range rules {
		planned = append(planned, &store.QualityExecutionMessage{
			RunID:              batch.ID,
			RuleID:             rule.ID,
			RuleRevision:       rule.Revision,
			RuleEngineVersion:  dataquality.EngineVersion,
			TargetGUID:         rule.TargetGUID,
			TargetType:         rule.TargetType,
			TargetPath:         rule.TargetPath,
			InstanceResourceID: instance,
			Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
			InitiatorID:        initiator,
			StartedAt:          at,
		})
	}
	if err := s.store.PlanQualityExecutions(ctx, planned); err != nil {
		for range planned {
			s.metrics.CountQualityExecutionAbandoned(metrics.QualityAbandonedPlanFailed)
		}
		s.abandonQualityBatch(ctx, batch.ID, 0, at)
		return nil, nil, errors.Wrap(err, "failed to plan the quality executions")
	}
	for range planned {
		s.metrics.CountQualityExecutionCreated(metrics.QualityTriggerManual)
	}
	return batch, planned, nil
}

// qualityExecutionIDs collects the row ids of a planned batch, for the enqueue
// call.
func qualityExecutionIDs(executions []*store.QualityExecutionMessage) []int64 {
	ids := make([]int64, 0, len(executions))
	for _, execution := range executions {
		ids = append(ids, execution.ID)
	}
	return ids
}

// abandonQualityBatch closes a batch whose executions were never planned or
// queued. It is best effort: the caller is already returning an error, and the
// stale sweep is the backstop if either write fails.
func (s *DataQualityService) abandonQualityBatch(ctx context.Context, batchID int64, planned int, at time.Time) {
	if _, err := s.store.AbandonQualityExecutions(ctx, batchID, at); err != nil {
		slog.Warn("failed to abandon the quality executions of a failed batch",
			slog.Int64("batch_id", batchID), clog.WithError(err))
	}
	counts := &storepb.QualityStatusCounts{Total: int32(planned)}
	// The batch executed nothing, so it has no rollup outcome: ABANDONED with an
	// absent outcome is the honest state (the runner uses the same for a batch
	// whose admission never succeeded).
	finishErr := s.store.FinishQualityRun(ctx, batchID,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED,
		storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, counts, at)
	if finishErr != nil {
		slog.Warn("failed to abandon the quality batch",
			slog.Int64("batch_id", batchID), clog.WithError(finishErr))
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
	// A deterministic response order makes the planned executions reproducible.
	slices.Sort(order)
	return groups, order
}

// ListQualityExecutions lists executions scoped to the caller's workspace,
// newest first.
func (s *DataQualityService) ListQualityExecutions(ctx context.Context, req *connect.Request[v1pb.ListQualityExecutionsRequest]) (*connect.Response[v1pb.ListQualityExecutionsResponse], error) {
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

	find := &store.FindQualityExecutionMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	if err := s.applyQualityExecutionListFilters(ctx, find, req.Msg); err != nil {
		return nil, err
	}
	executions, err := s.store.ListQualityExecutions(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality executions")
	}
	executions, nextPageToken, err := paginate(executions, offset)
	if err != nil {
		return nil, err
	}
	ruleNames, err := s.qualityRuleNames(ctx, workspaceID, qualityExecutionRuleIDs(executions))
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListQualityExecutionsResponse{NextPageToken: nextPageToken}
	for _, execution := range executions {
		response.QualityExecutions = append(response.QualityExecutions,
			convertQualityExecution(workspaceID, execution, ruleNames[execution.RuleID]))
	}
	return connect.NewResponse(response), nil
}

// applyQualityExecutionListFilters maps the request filters onto the store
// filter. The rule filter names another resource, so it is resolved here; every
// other filter is a direct mapping.
func (s *DataQualityService) applyQualityExecutionListFilters(ctx context.Context, find *store.FindQualityExecutionMessage, request *v1pb.ListQualityExecutionsRequest) error {
	switch state := storepb.QualityExecutionState(request.GetState()); state {
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED:
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED:
		find.States = []storepb.QualityExecutionState{state}
	default:
		return connectErrorInvalid("unknown quality execution state %d", state)
	}

	switch outcome := storepb.QualityStatus(request.GetOutcome()); outcome {
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		find.Statuses = []storepb.QualityStatus{outcome}
	default:
		return connectErrorInvalid("unknown quality status %d", outcome)
	}

	if trigger := storepb.QualityExecutionTrigger(request.GetTrigger()); trigger != storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_UNSPECIFIED {
		switch trigger {
		case storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
			storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED:
			find.Trigger = &trigger
		default:
			return connectErrorInvalid("unknown quality execution trigger %d", trigger)
		}
	}
	if ruleName := strings.TrimSpace(request.GetRule()); ruleName != "" {
		_, ruleResourceID, err := s.ruleResourceID(ctx, ruleName)
		if err != nil {
			return err
		}
		rule, err := s.fetchQualityRule(ctx, ruleResourceID)
		if err != nil {
			return err
		}
		find.RuleID = &rule.ID
	}
	if guid := strings.TrimSpace(request.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	if instance := strings.TrimSpace(request.GetInstance()); instance != "" {
		instanceID, err := common.GetInstanceID(instance)
		if err != nil {
			return connectErrorInvalid("invalid instance %q", instance)
		}
		find.InstanceResourceID = &instanceID
	}
	if initiator := strings.TrimSpace(request.GetInitiator()); initiator != "" {
		id, err := common.GetUserID(initiator)
		if err != nil {
			return connectErrorInvalid("invalid initiator %q", initiator)
		}
		principalID := int32(id)
		find.InitiatorID = &principalID
	}
	if request.GetStartTime() != nil {
		startAt := request.GetStartTime().AsTime()
		find.StartAt = &startAt
	}
	if request.GetEndTime() != nil {
		endAt := request.GetEndTime().AsTime()
		find.EndAt = &endAt
	}
	return nil
}

// GetQualityExecution reads one execution.
func (s *DataQualityService) GetQualityExecution(ctx context.Context, req *connect.Request[v1pb.GetQualityExecutionRequest]) (*connect.Response[v1pb.QualityExecution], error) {
	workspaceID, executionID, err := s.executionResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	execution, err := s.fetchQualityExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	ruleNames, err := s.qualityRuleNames(ctx, workspaceID, []int64{execution.RuleID})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityExecution(workspaceID, execution, ruleNames[execution.RuleID])), nil
}

// qualityExecutionRuleIDs collects the distinct rule ids of the given executions.
func qualityExecutionRuleIDs(executions []*store.QualityExecutionMessage) []int64 {
	ruleIDs := make([]int64, 0, len(executions))
	for _, execution := range executions {
		ruleIDs = append(ruleIDs, execution.RuleID)
	}
	return dedupeInt64(ruleIDs)
}

// qualityRuleNames resolves numeric rule ids to resource names in one query.
func (s *DataQualityService) qualityRuleNames(ctx context.Context, workspaceID string, ruleIDs []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(ruleIDs))
	if len(ruleIDs) == 0 {
		return names, nil
	}
	rules, err := s.store.ListQualityRulesByIDs(ctx, dedupeInt64(ruleIDs))
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve the quality rules of the executions")
	}
	for _, rule := range rules {
		names[rule.ID] = formatQualityRuleName(workspaceID, rule.ResourceID)
	}
	return names, nil
}

// latestQualityExecutions returns each rule's newest terminal execution keyed by
// rule id.
func (s *DataQualityService) latestQualityExecutions(ctx context.Context, rules []*store.QualityRuleMessage) (map[int64]*store.QualityExecutionMessage, error) {
	ruleIDs := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	latest, err := s.store.ListLatestQualityExecutions(ctx, ruleIDs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the rules' latest executions")
	}
	return latest, nil
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

// issueResourceID parses an issue name and proves its workspace.
func (s *DataQualityService) issueResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualityIssueName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

// fetchQualityIssue reads one issue by its public resource id.
func (s *DataQualityService) fetchQualityIssue(ctx context.Context, resourceID string) (*store.QualityIssueMessage, error) {
	issue, err := s.store.GetQualityIssue(ctx, &store.FindQualityIssueMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality issue")
	}
	if issue == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality issue %q not found", resourceID))
	}
	return issue, nil
}

// ListQualityIssues lists the durable problems behind rule failures, newest first.
func (s *DataQualityService) ListQualityIssues(ctx context.Context, req *connect.Request[v1pb.ListQualityIssuesRequest]) (*connect.Response[v1pb.ListQualityIssuesResponse], error) {
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

	find := &store.FindQualityIssueMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	if err := applyQualityIssueListFilters(find, req.Msg); err != nil {
		return nil, err
	}
	if ruleName := strings.TrimSpace(req.Msg.GetRule()); ruleName != "" {
		_, ruleResourceID, err := s.ruleResourceID(ctx, ruleName)
		if err != nil {
			return nil, err
		}
		rule, err := s.fetchQualityRule(ctx, ruleResourceID)
		if err != nil {
			return nil, err
		}
		find.RuleID = &rule.ID
	}
	if owner := strings.TrimSpace(req.Msg.GetOwner()); owner != "" {
		ruleIDs, err := s.qualityRuleIDsOfOwner(ctx, owner)
		if err != nil {
			return nil, err
		}
		if len(ruleIDs) == 0 {
			// The owner has no rules, so the owner filter cannot match an issue.
			// Setting an empty RuleIDs would drop the predicate and widen the list.
			return connect.NewResponse(&v1pb.ListQualityIssuesResponse{}), nil
		}
		find.RuleIDs = ruleIDs
	}

	issues, err := s.store.ListQualityIssues(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality issues")
	}
	issues, nextPageToken, err := paginate(issues, offset)
	if err != nil {
		return nil, err
	}
	ruleNames, err := s.qualityRuleNames(ctx, workspaceID, qualityIssueRuleIDs(issues))
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListQualityIssuesResponse{NextPageToken: nextPageToken}
	for _, issue := range issues {
		response.QualityIssues = append(response.QualityIssues,
			convertQualityIssue(workspaceID, issue, ruleNames[issue.RuleID]))
	}
	return connect.NewResponse(response), nil
}

// applyQualityIssueListFilters maps the filters that need no store read. The rule
// and owner filters are resolved by the caller because they name other resources.
func applyQualityIssueListFilters(find *store.FindQualityIssueMessage, request *v1pb.ListQualityIssuesRequest) error {
	status := storepb.QualityIssueStatus(request.GetStatus())
	switch status {
	case storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_UNSPECIFIED:
	case storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED,
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED:
		find.Statuses = []storepb.QualityIssueStatus{status}
	default:
		return connectErrorInvalid("unknown quality issue status %d", status)
	}

	severity := storepb.QualitySeverity(request.GetSeverity())
	switch severity {
	case storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED:
	case storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL:
		find.Severities = []storepb.QualitySeverity{severity}
	default:
		return connectErrorInvalid("unknown quality severity %d", severity)
	}

	if guid := strings.TrimSpace(request.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	if request.GetStartTime() != nil {
		startAt := request.GetStartTime().AsTime()
		find.StartAt = &startAt
	}
	if request.GetEndTime() != nil {
		endAt := request.GetEndTime().AsTime()
		find.EndAt = &endAt
	}
	return nil
}

// qualityRuleIDsOfOwner resolves the rules owned by a principal; ownership is not
// stored on the issue, so the owner filter is an issue filter by rule ids.
func (s *DataQualityService) qualityRuleIDsOfOwner(ctx context.Context, owner string) ([]int64, error) {
	id, err := common.GetUserID(owner)
	if err != nil {
		return nil, connectErrorInvalid("invalid owner %q", owner)
	}
	principalID := int32(id)
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{OwnerID: &principalID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the owner's quality rules")
	}
	ruleIDs := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	return ruleIDs, nil
}

// qualityIssueRuleIDs collects the distinct rule ids of the given issues.
func qualityIssueRuleIDs(issues []*store.QualityIssueMessage) []int64 {
	ruleIDs := make([]int64, 0, len(issues))
	for _, issue := range issues {
		ruleIDs = append(ruleIDs, issue.RuleID)
	}
	return dedupeInt64(ruleIDs)
}

// GetQualityIssue reads one issue.
func (s *DataQualityService) GetQualityIssue(ctx context.Context, req *connect.Request[v1pb.GetQualityIssueRequest]) (*connect.Response[v1pb.QualityIssue], error) {
	workspaceID, resourceID, err := s.issueResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	issue, err := s.fetchQualityIssue(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	converted, err := s.convertIssueWithNames(ctx, workspaceID, issue)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(converted), nil
}

// convertIssueWithNames resolves one issue's rule name and converts it.
func (s *DataQualityService) convertIssueWithNames(ctx context.Context, workspaceID string, issue *store.QualityIssueMessage) (*v1pb.QualityIssue, error) {
	ruleNames, err := s.qualityRuleNames(ctx, workspaceID, qualityIssueRuleIDs([]*store.QualityIssueMessage{issue}))
	if err != nil {
		return nil, err
	}
	return convertQualityIssue(workspaceID, issue, ruleNames[issue.RuleID]), nil
}

// qualityIssueUpdate is one parsed UpdateQualityIssue mask: what the request asks
// the issue to become. A nil mutedUntil leaves the mute column untouched; a zero
// time clears it.
type qualityIssueUpdate struct {
	resolve     bool
	reason      storepb.QualityResolutionReason
	note        string
	acknowledge bool
	mutedUntil  *time.Time
}

// parseQualityIssueUpdate turns the update mask into the actions to apply. It is
// pure so the mask vocabulary and the illegal combinations are testable without a
// store.
func parseQualityIssueUpdate(issue *v1pb.QualityIssue, acknowledged bool, paths []string) (*qualityIssueUpdate, error) {
	if issue == nil {
		return nil, connectErrorInvalid("the issue is required")
	}
	update := &qualityIssueUpdate{}
	noteGiven := false
	for _, path := range paths {
		switch path {
		case "status":
			if storepb.QualityIssueStatus(issue.GetStatus()) != storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED {
				return nil, connectErrorInvalid("status can only be set to RESOLVED; use acknowledged to acknowledge an issue")
			}
			update.resolve = true
		case "resolution_reason":
			reason := storepb.QualityResolutionReason(issue.GetResolutionReason())
			switch reason {
			case storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_UNSPECIFIED,
				storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED,
				storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_EXPECTED,
				storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_NO_ACTION_NEEDED,
				storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_DUPLICATE,
				storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_AUTO_RECOVERED:
			default:
				return nil, connectErrorInvalid("unknown quality resolution reason %d", reason)
			}
			update.reason = reason
			update.resolve = true
		case "resolution_note":
			update.note = issue.GetResolutionNote()
			noteGiven = true
		case "acknowledged":
			if !acknowledged {
				return nil, connectErrorInvalid("acknowledged=false is not supported; an acknowledgement cannot be cleared")
			}
			update.acknowledge = true
		case "muted_until":
			mutedUntil := time.Time{}
			if issue.GetMutedUntil() != nil {
				mutedUntil = issue.GetMutedUntil().AsTime()
			}
			update.mutedUntil = &mutedUntil
		default:
			return nil, connectErrorInvalid("unsupported update path %q", path)
		}
	}
	if noteGiven && !update.resolve {
		return nil, connectErrorInvalid("resolution_note requires resolving the issue")
	}
	if update.resolve && update.acknowledge {
		return nil, connectErrorInvalid("resolving and acknowledging cannot be combined")
	}
	return update, nil
}

// UpdateQualityIssue acknowledges, resolves or mutes one issue.
func (s *DataQualityService) UpdateQualityIssue(ctx context.Context, req *connect.Request[v1pb.UpdateQualityIssueRequest]) (*connect.Response[v1pb.QualityIssue], error) {
	workspaceID, resourceID, err := s.issueResourceID(ctx, req.Msg.GetIssue().GetName())
	if err != nil {
		return nil, err
	}
	issue, err := s.fetchQualityIssue(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	update, err := parseQualityIssueUpdate(req.Msg.GetIssue(), req.Msg.GetAcknowledged(), req.Msg.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if update.mutedUntil != nil {
		if _, err := s.store.UpdateQualityIssue(ctx, resourceID, &store.UpdateQualityIssueMessage{MutedUntil: update.mutedUntil}); err != nil {
			return nil, qualityIssueWriteError(err, "failed to update the quality issue")
		}
	}
	if update.resolve {
		if err := s.store.ResolveQualityIssue(ctx, issue.ID, update.reason, update.note, now); err != nil {
			return nil, qualityIssueWriteError(err, "failed to resolve the quality issue")
		}
	}
	if update.acknowledge {
		if err := checkQualityIssueAcknowledgeable(issue.Status); err != nil {
			return nil, err
		}
		if err := s.store.AcknowledgeQualityIssue(ctx, issue.ID, int32(currentUserID(ctx)), now); err != nil {
			return nil, qualityIssueWriteError(err, "failed to acknowledge the quality issue")
		}
	}
	updated, err := s.fetchQualityIssue(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	converted, err := s.convertIssueWithNames(ctx, workspaceID, updated)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(converted), nil
}

// checkQualityIssueAcknowledgeable enforces the state machine's terminal state at
// the API boundary: RESOLVED is terminal, and acknowledging a resolved issue must
// not bring it back to ACKNOWLEDGED.
func checkQualityIssueAcknowledgeable(current storepb.QualityIssueStatus) error {
	if current == storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED {
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("a resolved quality issue cannot be acknowledged"))
	}
	return nil
}

// qualityIssueWriteError maps a failed issue transition. A terminal-state conflict
// is Aborted for the client rather than the AlreadyExists the shared code mapping
// would produce.
func qualityIssueWriteError(err error, message string) error {
	if common.ErrorCode(err) == common.Conflict {
		return connect.NewError(connect.CodeAborted, err)
	}
	return errors.Wrap(err, message)
}

// qualitySuggestionResourceID parses a suggestion name and proves its workspace.
func (s *DataQualityService) qualitySuggestionResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualitySuggestionName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

// ListQualitySuggestions lists the review queue, newest first.
func (s *DataQualityService) ListQualitySuggestions(ctx context.Context, req *connect.Request[v1pb.ListQualitySuggestionsRequest]) (*connect.Response[v1pb.ListQualitySuggestionsResponse], error) {
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

	find := &store.FindQualitySuggestionMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	if err := applyQualitySuggestionListFilters(find, req.Msg); err != nil {
		return nil, err
	}
	suggestions, err := s.store.ListQualitySuggestions(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality suggestions")
	}
	suggestions, nextPageToken, err := paginate(suggestions, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListQualitySuggestionsResponse{NextPageToken: nextPageToken}
	for _, suggestion := range suggestions {
		response.QualitySuggestions = append(response.QualitySuggestions, convertQualitySuggestion(workspaceID, suggestion))
	}
	return connect.NewResponse(response), nil
}

// applyQualitySuggestionListFilters maps the review-queue filters onto the store
// filter.
func applyQualitySuggestionListFilters(find *store.FindQualitySuggestionMessage, request *v1pb.ListQualitySuggestionsRequest) error {
	switch status := storepb.QualitySuggestionStatus(request.GetStatus()); status {
	case storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED:
	case storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED:
		find.Statuses = []storepb.QualitySuggestionStatus{status}
	default:
		return connectErrorInvalid("unknown quality suggestion status %d", status)
	}

	switch kind := storepb.QualitySuggestionKind(request.GetKind()); kind {
	case storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_UNSPECIFIED:
	case storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL:
		find.Kinds = []storepb.QualitySuggestionKind{kind}
	default:
		return connectErrorInvalid("unknown quality suggestion kind %d", kind)
	}

	switch source := storepb.QualitySuggestionSource(request.GetSource()); source {
	case storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_UNSPECIFIED:
	case storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE,
		storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE:
		find.Sources = []storepb.QualitySuggestionSource{source}
	default:
		return connectErrorInvalid("unknown quality suggestion source %d", source)
	}

	if guid := strings.TrimSpace(request.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	return nil
}

// UpdateQualitySuggestion accepts or rejects one queued suggestion.
func (s *DataQualityService) UpdateQualitySuggestion(ctx context.Context, req *connect.Request[v1pb.UpdateQualitySuggestionRequest]) (*connect.Response[v1pb.QualitySuggestion], error) {
	workspaceID, resourceID, err := s.qualitySuggestionResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	decision, err := qualitySuggestionDecision(req.Msg.GetDecision())
	if err != nil {
		return nil, err
	}
	updated, err := s.store.DecideQualitySuggestion(ctx, resourceID, decision, int32(currentUserID(ctx)),
		req.Msg.GetDecisionNote(), time.Now())
	if err != nil {
		// A suggestion another reviewer already decided is Aborted for the
		// client, not the AlreadyExists the shared code mapping would produce.
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAborted, err)
		}
		return nil, errors.Wrap(err, "failed to decide the quality suggestion")
	}
	// Accepting a label proposal also asserts it: column_semantic_label is the row
	// the rest of the product reads. The decision is already durable at this point,
	// so a failed label write cannot be rolled back; it is surfaced as Internal with
	// a retry path (UpdateColumnLabel) instead of silently dropping the label.
	if decision == storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED &&
		updated.Kind == storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL {
		now := time.Now().UTC()
		label, err := labelSuggestionToLabelMessage(updated, principalIDPointer(currentUserID(ctx)), now)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal,
				errors.Wrap(err, "the suggestion was accepted but its label payload is not usable"))
		}
		if _, err := s.store.UpsertColumnSemanticLabel(ctx, label); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err,
				"the suggestion was accepted but its label could not be stored; retry the column label with UpdateColumnLabel"))
		}
	}
	// Accepting a rule draft only records the decision: the reviewer opens the
	// editor with the draft and saves the rule through the normal rule path.
	return connect.NewResponse(convertQualitySuggestion(workspaceID, updated)), nil
}

// qualitySuggestionDecision validates the decision enum: a suggestion is only
// ever accepted or rejected, never left pending or set to an unknown number.
func qualitySuggestionDecision(decision v1pb.QualitySuggestionStatus) (storepb.QualitySuggestionStatus, error) {
	switch decision {
	case v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED:
		return storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, nil
	case v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED:
		return storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED, nil
	case v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED,
		v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING:
		return 0, connectErrorInvalid("decision must be ACCEPTED or REJECTED")
	default:
		return 0, connectErrorInvalid("unknown quality suggestion decision %d", decision)
	}
}

// ListColumnProfiles lists the cached deterministic profiles of columns.
func (s *DataQualityService) ListColumnProfiles(ctx context.Context, req *connect.Request[v1pb.ListColumnProfilesRequest]) (*connect.Response[v1pb.ListColumnProfilesResponse], error) {
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

	find := &store.FindColumnProfileMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	applyColumnProfileListFilters(find, req.Msg)
	profiles, err := s.store.ListColumnProfiles(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the column profiles")
	}
	profiles, nextPageToken, err := paginate(profiles, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListColumnProfilesResponse{NextPageToken: nextPageToken}
	for _, profile := range profiles {
		response.ColumnProfiles = append(response.ColumnProfiles, convertColumnProfileToV1(profile))
	}
	return connect.NewResponse(response), nil
}

// applyColumnProfileListFilters maps the request filters onto the store filter.
func applyColumnProfileListFilters(find *store.FindColumnProfileMessage, request *v1pb.ListColumnProfilesRequest) {
	if guid := strings.TrimSpace(request.GetTableGuid()); guid != "" {
		find.TableGUID = &guid
	}
	if metaType := storepb.MetaType(request.GetMetaType()); metaType != storepb.MetaType_UNSPECIFIED {
		find.TableType = &metaType
	}
	find.ColumnNames = trimmedColumnNames(request.GetColumns())
}

// ListColumnLabels lists the semantic labels asserted on columns.
func (s *DataQualityService) ListColumnLabels(ctx context.Context, req *connect.Request[v1pb.ListColumnLabelsRequest]) (*connect.Response[v1pb.ListColumnLabelsResponse], error) {
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

	find := &store.FindColumnSemanticLabelMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	applyColumnLabelListFilters(find, req.Msg)
	labels, err := s.store.ListColumnSemanticLabels(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the column labels")
	}
	labels, nextPageToken, err := paginate(labels, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListColumnLabelsResponse{NextPageToken: nextPageToken}
	for _, label := range labels {
		response.ColumnLabels = append(response.ColumnLabels, convertColumnSemanticLabelToV1(label))
	}
	return connect.NewResponse(response), nil
}

// applyColumnLabelListFilters maps the request filters onto the store filter.
func applyColumnLabelListFilters(find *store.FindColumnSemanticLabelMessage, request *v1pb.ListColumnLabelsRequest) {
	if guid := strings.TrimSpace(request.GetTableGuid()); guid != "" {
		find.TableGUID = &guid
	}
	if metaType := storepb.MetaType(request.GetMetaType()); metaType != storepb.MetaType_UNSPECIFIED {
		find.TableType = &metaType
	}
	find.ColumnNames = trimmedColumnNames(request.GetColumns())
}

// trimmedColumnNames drops blank entries from a column filter list.
func trimmedColumnNames(columns []string) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		if column = strings.TrimSpace(column); column != "" {
			names = append(names, column)
		}
	}
	return names
}

// columnLabelUpdate is one validated UpdateColumnLabel request.
type columnLabelUpdate struct {
	tableGUID string
	metaType  storepb.MetaType
	column    string
	label     storepb.QualitySemanticType
	action    v1pb.ColumnLabelAction
}

// parseUpdateColumnLabel validates the identity and the action of an
// UpdateColumnLabel request.
func parseUpdateColumnLabel(request *v1pb.UpdateColumnLabelRequest) (*columnLabelUpdate, error) {
	update := &columnLabelUpdate{
		tableGUID: strings.TrimSpace(request.GetTableGuid()),
		metaType:  storepb.MetaType(request.GetMetaType()),
		column:    strings.TrimSpace(request.GetColumn()),
		label:     storepb.QualitySemanticType(request.GetLabel()),
		action:    request.GetAction(),
	}
	if update.tableGUID == "" {
		return nil, connectErrorInvalid("table_guid is required")
	}
	if update.metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required")
	}
	if update.column == "" {
		return nil, connectErrorInvalid("column is required")
	}
	switch update.action {
	case v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT:
		if update.label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED {
			return nil, connectErrorInvalid("label is required to accept a semantic label")
		}
	case v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR:
		// The label is ignored when clearing.
	case v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_UNSPECIFIED:
		return nil, connectErrorInvalid("action must be ACCEPT or CLEAR")
	default:
		return nil, connectErrorInvalid("unknown column label action %d", update.action)
	}
	return update, nil
}

// UpdateColumnLabel accepts a proposed semantic label or clears one column's
// label.
func (s *DataQualityService) UpdateColumnLabel(ctx context.Context, req *connect.Request[v1pb.UpdateColumnLabelRequest]) (*connect.Response[v1pb.UpdateColumnLabelResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	update, err := parseUpdateColumnLabel(req.Msg)
	if err != nil {
		return nil, err
	}
	switch update.action {
	case v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT:
		now := time.Now()
		stored, err := s.store.UpsertColumnSemanticLabel(ctx, &store.ColumnSemanticLabelMessage{
			TableGUID:  update.tableGUID,
			TableType:  update.metaType,
			ColumnName: update.column,
			Label:      update.label,
			Source:     storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_MANUAL,
			Status:     storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED,
			Confidence: 1,
			AcceptedBy: principalIDPointer(currentUserID(ctx)),
			AcceptedAt: &now,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to store the column label")
		}
		return connect.NewResponse(&v1pb.UpdateColumnLabelResponse{
			ColumnLabel: convertColumnSemanticLabelToV1(stored),
		}), nil
	case v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR:
		if _, err := s.store.DeleteColumnSemanticLabel(ctx, update.tableGUID, update.metaType, update.column); err != nil {
			return nil, errors.Wrap(err, "failed to clear the column label")
		}
		return connect.NewResponse(&v1pb.UpdateColumnLabelResponse{}), nil
	case v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_UNSPECIFIED:
		return nil, connectErrorInvalid("action must be ACCEPT or CLEAR")
	default:
		return nil, connectErrorInvalid("unknown column label action %d", update.action)
	}
}

// ProfileColumns measures the deterministic statistics of the requested columns
// (or every column), stores them as the cached profile and returns them. It runs
// one aggregate query and one bounded sample query against the table's read-only
// data source; no model is involved.
func (s *DataQualityService) ProfileColumns(ctx context.Context, req *connect.Request[v1pb.ProfileColumnsRequest]) (*connect.Response[v1pb.ProfileColumnsResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	guid := strings.TrimSpace(req.Msg.GetTableGuid())
	if guid == "" {
		return nil, connectErrorInvalid("table_guid is required")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required")
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	resolution, err := dataquality.ResolveTarget(ctx, s.store, guid, metaType)
	if err != nil {
		return nil, err
	}
	columns := trimmedQualityColumns(req.Msg.GetColumns())
	if len(columns) == 0 {
		columns = resolution.Target.ColumnNames()
	}
	if len(columns) > settings.MaxColumnsPerAnalysis {
		return nil, connectErrorInvalid("at most %d columns may be profiled at once", settings.MaxColumnsPerAnalysis)
	}

	queryer, release, err := s.qualityReadOnlyQueryer(ctx, resolution, "profiling columns")
	if err != nil {
		return nil, err
	}
	defer release()

	profiler := dataquality.NewProfiler()
	profiler.Metrics = s.metrics
	table, err := profiler.Profile(ctx, queryer, resolution.Target, columns, dataquality.ProfileOptions{
		MaxColumns:   settings.MaxColumnsPerAnalysis,
		SampleRows:   qualityProfileSampleRows,
		QueryTimeout: qualityProfileQueryTimeout,
		MaxBytes:     qualityProfileMaxBytes,
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	response := &v1pb.ProfileColumnsResponse{ColumnProfiles: make([]*v1pb.ColumnProfile, 0, len(columns))}
	for _, column := range columns {
		stats, ok := table.Columns[column]
		if !ok {
			continue
		}
		stored, err := s.store.UpsertColumnProfile(ctx, &store.ColumnProfileMessage{
			TableGUID:     resolution.Target.GUID,
			TableType:     resolution.Target.MetaType,
			ColumnName:    column,
			Profile:       stats,
			SampleMethod:  table.SampleMethod,
			SampleRows:    table.SampleRows,
			Truncated:     table.Truncated,
			EngineVersion: dataquality.EngineVersion,
			ProfiledAt:    now,
		})
		if err != nil {
			return nil, err
		}
		response.ColumnProfiles = append(response.ColumnProfiles, convertColumnProfileToV1(stored))
	}
	return connect.NewResponse(response), nil
}

// qualityReadOnlyQueryer opens the target's read-only data source and returns it
// with a release function. The caller must call release when done; a missing
// READ_ONLY source is a configuration error, never a reason to fall back.
func (s *DataQualityService) qualityReadOnlyQueryer(ctx context.Context, resolution *dataquality.Resolution, action string) (db.ReadOnlyQueryer, func(), error) {
	if s.dbFactory == nil {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("a database factory is required to %s", action))
	}
	connection, err := s.acquireQualityConnection(ctx, resolution.Instance, action, qualityProfileConnectionWait)
	if err != nil {
		return nil, nil, err
	}
	driver, err := s.dbFactory.GetReadOnlyDatabaseDriver(ctx, resolution.Instance, resolution.Database)
	if err != nil {
		connection()
		if errors.Is(err, dbfactory.ErrNoReadOnlyDataSource) {
			return nil, nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrapf(err,
				"add a READ_ONLY data source to the instance before %s", action))
		}
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Wrapf(err, "failed to open the target's read-only data source"))
	}
	queryer, ok := driver.(db.ReadOnlyQueryer)
	if !ok {
		_ = driver.Close(ctx)
		connection()
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("engine %s does not support read-only queries", resolution.Target.Engine))
	}
	release := func() {
		if closeErr := driver.Close(ctx); closeErr != nil {
			slog.Warn("failed to close the read-only data source", clog.WithError(closeErr))
		}
		connection()
	}
	return queryer, release, nil
}

// acquireQualityConnection reserves one of the instance's connection slots for a
// read-only query an API request runs. A dry run and a profile open a driver on a
// user's behalf, so they lease from the same budget as the runners instead of
// pushing the target past its ceiling. A busy instance is reported as
// ResourceExhausted: the action is retryable and nothing was recorded.
func (s *DataQualityService) acquireQualityConnection(ctx context.Context, instance *store.InstanceMessage, action string, wait time.Duration) (func(), error) {
	if s.stateCfg == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("the instance connection governor is not configured"))
	}
	if instance == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("cannot run %s without an instance", action))
	}
	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	release, err := s.stateCfg.AcquireInstanceConnection(waitCtx, state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: maximumConnections,
		// The caller is an API request whose user is waiting, so it takes the
		// interactive class ahead of the runners' background work.
		Priority: state.ConnectionPriorityInteractive,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted,
			errors.Errorf("the instance is using all of its connections right now; retry %s shortly", action))
	}
	return release, nil
}

// trimmedQualityColumns trims and de-duplicates a column list, preserving order.
func trimmedQualityColumns(columns []string) []string {
	seen := make(map[string]struct{}, len(columns))
	trimmed := make([]string, 0, len(columns))
	for _, column := range columns {
		name := strings.TrimSpace(column)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		trimmed = append(trimmed, name)
	}
	return trimmed
}

// SuggestColumnLabels lives in data_quality_label_analysis.go: it profiles the
// columns, lets the model name their semantic types and queues the deterministic
// label and rule-draft suggestions.
