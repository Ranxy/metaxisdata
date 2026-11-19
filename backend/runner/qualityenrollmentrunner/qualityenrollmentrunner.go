// Package qualityenrollmentrunner turns the runner's database scope into a queue.
//
// The scope is the desired state: every table, external table, view and materialized
// view in a selected database is discovered into quality_agent_enrollment. Admission
// then creates (or resumes) one agent session per target by priority and budget, and
// the session runner takes over: a covered target is monitored, an uncovered one is
// triaged once and parked until its review. The queue row is the desired state; the
// session row stays the execution state.
package qualityenrollmentrunner

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// tickInterval is how often the queue is advanced: due reviews, recovered
	// sessions and admission. Discovery scans each database on its own cadence.
	tickInterval = 60 * time.Second
	// databaseScanInterval is how often one database's registry objects are re-read.
	// It matches the schema sync scan, because enrollment can never run ahead of the
	// registry anyway.
	databaseScanInterval = 15 * time.Minute
	// passTimeout bounds one tick. A tick that runs out of time is retried by the
	// next one; the queue is durable, so nothing is lost.
	passTimeout = 2 * time.Minute
	// scanPageSize is how many registry rows one discovery page reads.
	scanPageSize = 2000
	// dueDormantBatch is how many due reviews are requeued per tick.
	dueDormantBatch = 100
	// coveredDormantBatch is how many newly covered targets are promoted per tick.
	coveredDormantBatch = 100
	// recoverableBatch is how many reversibly blocked sessions are recovered per tick.
	recoverableBatch = 100
	// admitBatch bounds how many sessions one tick creates.
	admitBatch = 20
	// admissionStagger spreads new sessions' first wake so a batch does not become
	// due in the same second.
	admissionStagger = 30 * time.Second
	// priorityLookback bounds the failure and schema-change signals.
	priorityLookback = 7 * 24 * time.Hour
	// maxPriorityRowsPerDatabase bounds one database's priority recomputation.
	maxPriorityRowsPerDatabase = 5000
)

// Store is the slice of *store.Store the enrollment runner needs.
type Store interface {
	GetWorkspaceID(ctx context.Context) (string, error)

	UpsertQualityAgentEnrollments(ctx context.Context, workspaceID string, updates []*store.QualityAgentEnrollmentUpsert) (int64, error)
	MarkQualityAgentEnrollmentsGone(ctx context.Context, workspaceID string, agentID int64, databaseRef string, scanStartedAt time.Time) (int64, error)
	ReconcileQualityAgentEnrollmentScope(ctx context.Context, workspaceID string, agentID int64, keepDatabases []string) (int64, int64, error)
	ListQualityAgentEnrollments(ctx context.Context, find *store.FindQualityAgentEnrollmentMessage) ([]*store.QualityAgentEnrollmentMessage, error)
	ListAdmissibleQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*store.QualityAgentEnrollmentMessage, error)
	ListDueDormantQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, now time.Time, limit int) ([]*store.QualityAgentEnrollmentMessage, error)
	ListCoveredDormantQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*store.QualityAgentEnrollmentMessage, error)
	ListRecoverableQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*store.QualityAgentEnrollmentMessage, error)
	RequeueQualityAgentEnrollments(ctx context.Context, resourceIDs []string, reason string) (int64, error)
	UpdateQualityAgentEnrollmentAfterTriage(ctx context.Context, resourceID string, nextReviewAt time.Time) (*store.QualityAgentEnrollmentMessage, bool, error)
	PromoteQualityAgentEnrollment(ctx context.Context, resourceID string, mode storepb.QualityAgentSessionMode) (*store.QualityAgentEnrollmentMessage, bool, error)
	LinkQualityAgentEnrollmentSession(ctx context.Context, resourceID string, sessionID int64, mode storepb.QualityAgentSessionMode) (*store.QualityAgentEnrollmentMessage, bool, error)
	SetQualityAgentEnrollmentBlockedReason(ctx context.Context, resourceID, reason string) error
	UpdateQualityAgentEnrollmentPriority(ctx context.Context, updates []*store.QualityAgentEnrollmentPriorityUpdate) error

	GetQualityAgentSessionByTarget(ctx context.Context, workspaceID string, agentID int64, targetGUID string) (*store.QualityAgentSessionMessage, error)
	CreateQualityAgentSession(ctx context.Context, input *store.CreateQualityAgentSessionInput) (*store.QualityAgentSessionMessage, error)
	UpdateQualityAgentSessionState(ctx context.Context, resourceID string, expectedRevision int32, state storepb.QualityAgentSessionState, blockedCode, blockedReason string, nextWakeAt *time.Time) (*store.QualityAgentSessionMessage, bool, error)
	UpdateQualityAgentSessionMode(ctx context.Context, resourceID string, mode storepb.QualityAgentSessionMode) (bool, error)
	AdvanceQualityAgentSessionWake(ctx context.Context, resourceID string, now time.Time) (bool, error)
	CountActiveQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64) (int, error)
	CountQualityAgentSessionsCreatedSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int, error)
	CountQualityAgentSessionWakesSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int64, error)
	CurrentDatabaseTime(ctx context.Context) (time.Time, error)
	ListQualityAgents(ctx context.Context) ([]*store.QualityAgentMessage, error)
	ListQualityAgentDatabasesByAgent(ctx context.Context) (map[int64][]string, error)

	ListQualityRules(ctx context.Context, find *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error)
	ListQualityContracts(ctx context.Context) ([]*store.QualityContractMessage, error)
	ListQualityTargetFailureEvidence(ctx context.Context, keys []store.MetaGUIDKey, from, to time.Time, recentLimit int) (map[store.MetaGUIDKey]*store.QualityFailureEvidence, error)
	CountColumnLineageByGUID(ctx context.Context, guids []string) (map[string]*store.ColumnLineageCount, error)
	ListMetaRegistryTargetColumns(ctx context.Context, keys []store.MetaGUIDKey) (map[store.MetaGUIDKey]map[string]struct{}, error)
	ListMetaRegistrySchemaChangeTimes(ctx context.Context, keys []store.MetaGUIDKey, from, to time.Time) (map[store.MetaGUIDKey]time.Time, error)
	ListMetaRegistryResourceDigest(ctx context.Context, find *store.FindMetaRegistryResourceMessage) ([]*store.MetaRegistryResource, error)
}

// Runner discovers the database scope into an enrollment queue and admits targets.
type Runner struct {
	store         Store
	loadSettings  func(ctx context.Context) (dataquality.Settings, error)
	llmConfigured func(ctx context.Context, agent *store.QualityAgentMessage) bool
	now           func() time.Time
	tick          time.Duration
	scanInterval  time.Duration
	// lastScan is process-local; a restart re-scans every database once, which is the
	// same convergence rule the other runners follow.
	lastScan map[string]time.Time
	// scopeFingerprints remembers each agent's scope so a save invalidates only that
	// agent's scan clocks.
	scopeFingerprints map[int64]string
	scopeInitialized  bool
}

// activeAgent is one ACTIVE built-in agent and its scope.
type activeAgent struct {
	agent     *store.QualityAgentMessage
	databases []string
}

// NewRunner creates the enrollment runner. llmConfigured reports whether the agent
// has a usable model for triage; a nil function means triage admission is refused.
func NewRunner(stores *store.Store, llmConfigured func(ctx context.Context, agent *store.QualityAgentMessage) bool) *Runner {
	return &Runner{
		store: stores,
		loadSettings: func(ctx context.Context) (dataquality.Settings, error) {
			return dataquality.LoadSettings(ctx, stores)
		},
		llmConfigured:     llmConfigured,
		now:               func() time.Time { return time.Now().UTC() },
		tick:              tickInterval,
		scanInterval:      databaseScanInterval,
		lastScan:          map[string]time.Time{},
		scopeFingerprints: map[int64]string{},
	}
}

// Name identifies the runner in logs.
func (*Runner) Name() string {
	return "qualityenrollment"
}

// Run drives the queue until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()
	for {
		r.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runOnce advances the queue of every ACTIVE built-in agent. Each step is best
// effort: a failing agent is logged and the rest of the tick still runs.
func (r *Runner) runOnce(ctx context.Context) {
	now := r.now()
	ctx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()

	settings, err := r.loadSettings(ctx)
	if err != nil {
		slog.Error("failed to load data quality settings for the enrollment runner", log.WithError(err))
		return
	}
	if !settings.AgentRunnerEnabled {
		return
	}
	workspaceID, err := r.store.GetWorkspaceID(ctx)
	if err != nil {
		slog.Error("failed to resolve the workspace for the enrollment runner", log.WithError(err))
		return
	}
	agents, err := r.activeAgents(ctx)
	if err != nil {
		slog.Error("failed to read the active quality agents", log.WithError(err))
		return
	}
	// Scope and queue maintenance is per agent: an agent's range change must not
	// touch another agent's rows.
	for _, current := range agents {
		r.reconcileScope(ctx, workspaceID, current, now)
		r.requeueDueReviews(ctx, workspaceID, current.agent.ID, now)
		r.promoteCovered(ctx, workspaceID, current.agent.ID, now)
		r.recoverBlocked(ctx, workspaceID, current)
	}
	// Discovery is per (agent,database); overlapping scopes scan the same registry
	// twice for now, which is correct and can be shared later.
	for _, current := range agents {
		for _, databaseRef := range current.databases {
			r.scanDueDatabase(ctx, workspaceID, current.agent.ID, databaseRef, now)
		}
	}
	for _, current := range agents {
		r.admit(ctx, workspaceID, current, now)
	}
}

// activeAgents loads the agents the runner may serve: an ACTIVE agent with at least
// one database in scope.
func (r *Runner) activeAgents(ctx context.Context) ([]*activeAgent, error) {
	agents, err := r.store.ListQualityAgents(ctx)
	if err != nil {
		return nil, err
	}
	scopes, err := r.store.ListQualityAgentDatabasesByAgent(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*activeAgent, 0, len(agents))
	for _, agent := range agents {
		if agent.State != store.QualityAgentStateActive {
			continue
		}
		databases := scopes[agent.ID]
		if len(databases) == 0 {
			continue
		}
		out = append(out, &activeAgent{agent: agent, databases: databases})
	}
	return out, nil
}

// reconcileScope aligns one agent's queue with its database scope. It runs on every
// tick so a settings save is observed without a notification hook.
func (r *Runner) reconcileScope(ctx context.Context, workspaceID string, current *activeAgent, now time.Time) {
	if r.scopeFingerprints == nil {
		r.scopeFingerprints = map[int64]string{}
	}
	if r.lastScan == nil {
		r.lastScan = map[string]time.Time{}
	}
	fingerprint := strings.Join(current.databases, "\x00")
	if r.scopeInitialized {
		if previous, ok := r.scopeFingerprints[current.agent.ID]; ok && previous == fingerprint {
			return
		}
	}
	outOfScope, restored, err := r.store.ReconcileQualityAgentEnrollmentScope(ctx, workspaceID, current.agent.ID, current.databases)
	if err != nil {
		slog.Error("failed to reconcile the enrollment scope",
			slog.Int64("agent", current.agent.ID), log.WithError(err))
		return
	}
	inScope := make(map[string]bool, len(current.databases))
	for _, database := range current.databases {
		inScope[database] = true
	}
	prefix := fmt.Sprintf("%d|", current.agent.ID)
	for key := range r.lastScan {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		database := strings.TrimPrefix(key, prefix)
		if !inScope[database] {
			delete(r.lastScan, key)
		}
	}
	r.scopeInitialized = true
	r.scopeFingerprints[current.agent.ID] = fingerprint
	if outOfScope > 0 || restored > 0 {
		slog.Info("reconciled the quality agent enrollment scope",
			slog.Int64("agent", current.agent.ID), slog.Int64("out_of_scope", outOfScope),
			slog.Int64("restored", restored), slog.Time("now", now))
	}
}

// requeueDueReviews puts dormant rows whose review is due back in the pending set.
func (r *Runner) requeueDueReviews(ctx context.Context, workspaceID string, agentID int64, now time.Time) {
	due, err := r.store.ListDueDormantQualityAgentEnrollments(ctx, workspaceID, agentID, now, dueDormantBatch)
	if err != nil {
		slog.Error("failed to read the due enrollment reviews", log.WithError(err))
		return
	}
	if len(due) == 0 {
		return
	}
	resourceIDs := make([]string, 0, len(due))
	for _, enrollment := range due {
		resourceIDs = append(resourceIDs, enrollment.ResourceID)
	}
	if _, err := r.store.RequeueQualityAgentEnrollments(ctx, resourceIDs, "review-due"); err != nil {
		slog.Error("failed to requeue the due enrollment reviews", log.WithError(err))
	}
}

// promoteCovered moves a dormant target back to monitoring once it has coverage.
func (r *Runner) promoteCovered(ctx context.Context, workspaceID string, agentID int64, now time.Time) {
	covered, err := r.store.ListCoveredDormantQualityAgentEnrollments(ctx, workspaceID, agentID, coveredDormantBatch)
	if err != nil {
		slog.Error("failed to read the newly covered enrollments", log.WithError(err))
		return
	}
	for _, enrollment := range covered {
		if err := r.promote(ctx, workspaceID, enrollment, now); err != nil {
			slog.Warn("failed to promote a covered enrollment",
				slog.String("enrollment", enrollment.ResourceID), log.WithError(err))
		}
	}
}

// promote wakes a covered target's session in monitor mode.
func (r *Runner) promote(ctx context.Context, workspaceID string, enrollment *store.QualityAgentEnrollmentMessage, now time.Time) error {
	session, err := r.store.GetQualityAgentSessionByTarget(ctx, workspaceID, enrollment.AgentID, enrollment.TargetGUID)
	if err != nil {
		return err
	}
	if session == nil || session.ArchivedAt != nil {
		// No live session: let admission create one in MONITOR mode.
		_, err := r.store.RequeueQualityAgentEnrollments(ctx, []string{enrollment.ResourceID}, "coverage-appeared")
		return err
	}
	_, modeErr := r.store.UpdateQualityAgentSessionMode(ctx, session.ResourceID,
		storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR)
	if modeErr != nil {
		return modeErr
	}
	if session.State == storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED {
		_, applied, enableErr := r.store.UpdateQualityAgentSessionState(ctx, session.ResourceID, session.Revision,
			storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED, "", "", &now)
		if enableErr != nil {
			return enableErr
		}
		if !applied {
			return nil
		}
	} else if _, err := r.store.AdvanceQualityAgentSessionWake(ctx, session.ResourceID, now); err != nil {
		return err
	}
	_, _, err = r.store.PromoteQualityAgentEnrollment(ctx, enrollment.ResourceID,
		storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR)
	return err
}

// recoverBlocked re-enables sessions that a reversible scope or agent change blocked.
func (r *Runner) recoverBlocked(ctx context.Context, workspaceID string, current *activeAgent) {
	recoverable, err := r.store.ListRecoverableQualityAgentEnrollments(ctx, workspaceID, current.agent.ID, recoverableBatch)
	if err != nil {
		slog.Error("failed to read the recoverable enrollments", log.WithError(err))
		return
	}
	if len(recoverable) == 0 {
		return
	}
	now := r.now()
	inScope := make(map[string]bool, len(current.databases))
	for _, database := range current.databases {
		inScope[database] = true
	}
	for _, enrollment := range recoverable {
		session, err := r.store.GetQualityAgentSessionByTarget(ctx, workspaceID, enrollment.AgentID, enrollment.TargetGUID)
		if err != nil || session == nil || session.ArchivedAt != nil {
			continue
		}
		switch session.BlockedCode {
		case "out-of-scope":
			if !inScope[session.DatabaseRef] {
				continue
			}
		case "agent-disabled":
			// This runner only serves active agents, so the block is stale.
		default:
			continue
		}
		_, applied, enableErr := r.store.UpdateQualityAgentSessionState(ctx, session.ResourceID, session.Revision,
			storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED, "", "", &now)
		if enableErr != nil {
			slog.Warn("failed to recover a blocked session", slog.String("session", session.ResourceID), log.WithError(enableErr))
		} else if applied {
			_ = r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "")
		}
	}
}

// scanDueDatabase discovers one agent's registry objects for one database.
func (r *Runner) scanDueDatabase(ctx context.Context, workspaceID string, agentID int64, databaseRef string, now time.Time) {
	key := fmt.Sprintf("%d|%s", agentID, databaseRef)
	if last, ok := r.lastScan[key]; ok && now.Sub(last) < r.scanInterval {
		return
	}
	if err := r.scanDatabase(ctx, workspaceID, agentID, databaseRef, now); err != nil {
		slog.Warn("failed to scan a database for enrollment",
			slog.Int64("agent", agentID), slog.String("database", databaseRef), log.WithError(err))
		return
	}
	r.lastScan[key] = now
}

// scanDatabase reads one database's monitorable objects, upserts them and recomputes
// their priorities. Marking objects gone happens only after a complete scan.
func (r *Runner) scanDatabase(ctx context.Context, workspaceID string, agentID int64, databaseRef string, now time.Time) error {
	instance, database, err := common.GetInstanceDatabaseID(databaseRef)
	if err != nil {
		return err
	}
	guidPrefix := common.BuildMetaGUID(instance, database)
	objectTypes := []storepb.MetaType{
		storepb.MetaType_TABLE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW,
	}
	// The GONE watermark must come from the same clock the upsert's NOW() uses; a Go
	// clock that runs ahead would mark the rows it just wrote gone on every scan.
	scanStarted, err := r.store.CurrentDatabaseTime(ctx)
	if err != nil {
		return err
	}
	var upserts []*store.QualityAgentEnrollmentUpsert
	for offset := 0; ; {
		page, err := r.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{
			GUIDPrefix:  &guidPrefix,
			ObjectTypes: &objectTypes,
			Limit:       intPointer(scanPageSize),
			Offset:      intPointer(offset),
		})
		if err != nil {
			return err
		}
		for _, resource := range page {
			upserts = append(upserts, &store.QualityAgentEnrollmentUpsert{
				AgentID:     agentID,
				TargetGUID:  resource.GUID,
				TargetType:  resource.ObjectType,
				DatabaseRef: databaseRef,
			})
		}
		if len(page) < scanPageSize {
			break
		}
		offset += len(page)
	}
	if _, err := r.store.UpsertQualityAgentEnrollments(ctx, workspaceID, upserts); err != nil {
		return err
	}
	if _, err := r.store.MarkQualityAgentEnrollmentsGone(ctx, workspaceID, agentID, databaseRef, scanStarted); err != nil {
		return err
	}
	r.recomputePriorities(ctx, workspaceID, agentID, databaseRef, instance, database, now)
	return nil
}

// recomputePriorities ranks one database's queue. The reads are batched: one rules
// query per database, one contracts read, and the rest are key-set batch reads, so the
// pass is proportional to the number of targets and not to their execution history.
func (r *Runner) recomputePriorities(ctx context.Context, workspaceID string, agentID int64, databaseRef, instance, database string, now time.Time) {
	enrollments, err := r.store.ListQualityAgentEnrollments(ctx, &store.FindQualityAgentEnrollmentMessage{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		DatabaseRef: &databaseRef,
		Limit:       intPointer(maxPriorityRowsPerDatabase),
	})
	if err != nil {
		slog.Warn("failed to read a database's enrollments for prioritization",
			slog.String("database", databaseRef), log.WithError(err))
		return
	}
	if len(enrollments) == 0 {
		return
	}
	keys := make([]store.MetaGUIDKey, 0, len(enrollments))
	guids := make([]string, 0, len(enrollments))
	for _, enrollment := range enrollments {
		keys = append(keys, store.MetaGUIDKey{GUID: enrollment.TargetGUID, ObjectType: enrollment.TargetType})
		guids = append(guids, enrollment.TargetGUID)
	}
	rules, err := r.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{
		Instance: instance,
		Database: database,
	})
	if err != nil {
		slog.Warn("failed to read a database's rules for prioritization", log.WithError(err))
		return
	}
	monitored := map[string]bool{}
	for _, rule := range rules {
		if rule.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			monitored[rule.TargetGUID] = true
		}
	}
	contracts, err := r.store.ListQualityContracts(ctx)
	if err != nil {
		slog.Warn("failed to read the contracts for prioritization", log.WithError(err))
		return
	}
	criticality := map[store.MetaGUIDKey]dataquality.AssetCriticality{}
	for _, contract := range contracts {
		plan := dataquality.DerivePlan(true,
			dataquality.ActiveIntent(contract.Settings.GetIntent(), now), nil)
		criticality[store.MetaGUIDKey{GUID: contract.TargetGUID, ObjectType: contract.TargetType}] = plan.Criticality
	}
	failures, err := r.store.ListQualityTargetFailureEvidence(ctx, keys, now.Add(-priorityLookback), now, 1)
	if err != nil {
		slog.Warn("failed to read the failure evidence for prioritization", log.WithError(err))
		failures = nil
	}
	consumers, err := r.store.CountColumnLineageByGUID(ctx, guids)
	if err != nil {
		slog.Warn("failed to read the lineage counts for prioritization", log.WithError(err))
		consumers = nil
	}
	schemaChanges, err := r.store.ListMetaRegistrySchemaChangeTimes(ctx, keys, now.Add(-priorityLookback), now)
	if err != nil {
		slog.Warn("failed to read the schema changes for prioritization", log.WithError(err))
		schemaChanges = nil
	}
	columns, err := r.store.ListMetaRegistryTargetColumns(ctx, keys)
	if err != nil {
		slog.Warn("failed to read the target columns for prioritization", log.WithError(err))
		columns = nil
	}
	updates := make([]*store.QualityAgentEnrollmentPriorityUpdate, 0, len(enrollments))
	for _, enrollment := range enrollments {
		key := store.MetaGUIDKey{GUID: enrollment.TargetGUID, ObjectType: enrollment.TargetType}
		signals := prioritySignals{
			monitored:          monitored[enrollment.TargetGUID],
			failing:            failures[key] != nil,
			schemaChanged:      !schemaChanges[key].IsZero(),
			recentlyDiscovered: now.Sub(enrollment.DiscoveredAt) < 24*time.Hour,
			columnCount:        len(columns[key]),
			criticality:        criticality[key],
		}
		if count := consumers[enrollment.TargetGUID]; count != nil {
			signals.consumers = count.Downstream
		}
		priority, reason := computePriority(signals)
		mode := storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE
		if signals.monitored {
			mode = storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR
		}
		updates = append(updates, &store.QualityAgentEnrollmentPriorityUpdate{
			ResourceID:     enrollment.ResourceID,
			Priority:       priority,
			PriorityReason: reason,
			Mode:           mode,
		})
	}
	if err := r.store.UpdateQualityAgentEnrollmentPriority(ctx, updates); err != nil {
		slog.Warn("failed to update the enrollment priorities", log.WithError(err))
	}
}

// prioritySignals are the inputs of the admission order.
type prioritySignals struct {
	monitored          bool
	failing            bool
	consumers          int32
	criticality        dataquality.AssetCriticality
	schemaChanged      bool
	recentlyDiscovered bool
	columnCount        int
}

// computePriority ranks one target. A covered target comes first because a failure
// event can only reach an existing session; then the targets whose health other
// assets depend on, then the rest. The score is capped so no single signal can starve
// a database for ever.
func computePriority(signals prioritySignals) (int32, string) {
	score := int32(0)
	reasons := make([]string, 0, 6)
	if signals.monitored {
		score += 400
		reasons = append(reasons, "rules")
	}
	if signals.failing {
		score += 200
		reasons = append(reasons, "failing")
	}
	if signals.consumers > 0 {
		contributors := signals.consumers
		if contributors > 5 {
			contributors = 5
		}
		score += contributors * 40
		reasons = append(reasons, fmt.Sprintf("consumers:%d", signals.consumers))
	}
	switch signals.criticality {
	case dataquality.CriticalityCritical:
		score += 150
		reasons = append(reasons, "critical")
	case dataquality.CriticalityImportant:
		score += 100
		reasons = append(reasons, "important")
	default:
	}
	if signals.schemaChanged {
		score += 80
		reasons = append(reasons, "schema-changed")
	}
	if signals.recentlyDiscovered {
		score += 40
		reasons = append(reasons, "new")
	}
	if signals.columnCount > 0 {
		score += int32(signals.columnCount / 10)
	}
	if score > 1000 {
		score = 1000
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "unmonitored")
	}
	return score, strings.Join(reasons, ",")
}

// admit creates or resumes sessions for one agent's pending enrollments, within its
// own rate, active-set and event-reserve limits.
func (r *Runner) admit(ctx context.Context, workspaceID string, current *activeAgent, now time.Time) {
	maxNew := int(current.agent.MaxNewSessionsPerHour)
	if maxNew <= 0 {
		maxNew = dataquality.DefaultAgentRunnerMaxNewSessionsPerHour
	}
	maxActive := int(current.agent.MaxActiveSessions)
	if maxActive <= 0 {
		maxActive = dataquality.DefaultAgentRunnerMaxActiveSessions
	}
	created, err := r.store.CountQualityAgentSessionsCreatedSince(ctx, workspaceID, current.agent.ID,
		now.Add(-time.Hour))
	if err != nil {
		slog.Error("failed to count the automatic sessions", slog.Int64("agent", current.agent.ID), log.WithError(err))
		return
	}
	active, err := r.store.CountActiveQualityAgentEnrollments(ctx, workspaceID, current.agent.ID)
	if err != nil {
		slog.Error("failed to count the active enrollments", slog.Int64("agent", current.agent.ID), log.WithError(err))
		return
	}
	if created >= maxNew {
		r.reasonCandidates(ctx, workspaceID, current.agent.ID, "admission-limit")
		return
	}
	if active >= maxActive {
		r.reasonCandidates(ctx, workspaceID, current.agent.ID, "active-limit")
		return
	}
	// Automated admission may only spend the share of the wake budget that is not
	// reserved for failed checks and people.
	wakeLimit := current.agent.WakesPerHour
	if wakeLimit <= 0 {
		wakeLimit = int32(dataquality.DefaultAgentRunnerWakesPerHour)
	}
	automaticShare := int(wakeLimit) * (100 - dataquality.DefaultAgentRunnerEventBudgetPercent) / 100
	if automaticShare < 1 {
		automaticShare = 1
	}
	wakes, err := r.store.CountQualityAgentSessionWakesSince(ctx, workspaceID, current.agent.ID, now.Add(-time.Hour))
	if err != nil {
		slog.Error("failed to count the session wakes", slog.Int64("agent", current.agent.ID), log.WithError(err))
		return
	}
	if wakes >= int64(automaticShare) {
		r.reasonCandidates(ctx, workspaceID, current.agent.ID, "event-budget")
		return
	}
	remaining := maxNew - created
	if activeRemaining := maxActive - active; activeRemaining < remaining {
		remaining = activeRemaining
	}
	if remaining <= 0 {
		r.reasonCandidates(ctx, workspaceID, current.agent.ID, "active-limit")
		return
	}
	limit := admitBatch
	if remaining < limit {
		limit = remaining
	}
	candidates, err := r.store.ListAdmissibleQualityAgentEnrollments(ctx, workspaceID, current.agent.ID, limit)
	if err != nil {
		slog.Error("failed to read the admissible enrollments", slog.Int64("agent", current.agent.ID), log.WithError(err))
		return
	}
	inScope := make(map[string]bool, len(current.databases))
	for _, database := range current.databases {
		inScope[database] = true
	}
	for index, enrollment := range candidates {
		if active+index >= maxActive {
			_ = r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "active-limit")
			continue
		}
		if !inScope[enrollment.DatabaseRef] {
			_ = r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "out-of-scope")
			continue
		}
		if err := r.admitOne(ctx, workspaceID, current, enrollment, index, now); err != nil {
			slog.Warn("failed to admit an enrollment",
				slog.String("enrollment", enrollment.ResourceID), log.WithError(err))
		}
	}
}

// reasonCandidates records on a few rows why admission is not progressing, so the queue
// page can explain a stalled run instead of showing an unexplained PENDING.
func (r *Runner) reasonCandidates(ctx context.Context, workspaceID string, agentID int64, reason string) {
	candidates, err := r.store.ListAdmissibleQualityAgentEnrollments(ctx, workspaceID, agentID, 5)
	if err != nil {
		return
	}
	for _, enrollment := range candidates {
		_ = r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, reason)
	}
}

// admitOne creates the target's session or resumes an existing one, then links the
// enrollment to it.
func (r *Runner) admitOne(ctx context.Context, workspaceID string, current *activeAgent, enrollment *store.QualityAgentEnrollmentMessage, index int, now time.Time) error {
	agentID := current.agent.ID
	mode := enrollment.Mode
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_UNSPECIFIED {
		mode = storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR
	}
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE &&
		!slices.Contains(current.agent.Capabilities, "suggest") {
		return r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "capability-suggest-disabled")
	}
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR &&
		!slices.Contains(current.agent.Capabilities, "patrol") {
		return r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "capability-patrol-disabled")
	}
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE && !r.llmConfiguredFor(ctx, current.agent) {
		return r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "no-model")
	}
	session, err := r.store.GetQualityAgentSessionByTarget(ctx, workspaceID, agentID, enrollment.TargetGUID)
	if err != nil {
		return err
	}
	if session != nil && session.ArchivedAt == nil {
		if session.State == storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED {
			switch session.BlockedCode {
			case "out-of-scope", "agent-disabled":
				_, applied, enableErr := r.store.UpdateQualityAgentSessionState(ctx, session.ResourceID, session.Revision,
					storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED, "", "", &now)
				if enableErr != nil {
					return enableErr
				}
				if !applied {
					return nil
				}
			default:
				return r.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "session-blocked")
			}
		} else {
			_, wakeErr := r.store.AdvanceQualityAgentSessionWake(ctx, session.ResourceID, now)
			if wakeErr != nil {
				return wakeErr
			}
		}
		if _, err := r.store.UpdateQualityAgentSessionMode(ctx, session.ResourceID, mode); err != nil {
			return err
		}
		_, _, err = r.store.LinkQualityAgentEnrollmentSession(ctx, enrollment.ResourceID, session.ID, mode)
		return err
	}
	nextWake := now.Add(time.Duration(index) * admissionStagger)
	session, err = r.store.CreateQualityAgentSession(ctx, &store.CreateQualityAgentSessionInput{
		WorkspaceID: workspaceID,
		TargetGUID:  enrollment.TargetGUID,
		TargetType:  enrollment.TargetType,
		DatabaseRef: enrollment.DatabaseRef,
		AgentID:     agentID,
		Mode:        mode,
		NextWakeAt:  &nextWake,
	})
	if err != nil {
		if common.ErrorCode(err) != common.Conflict {
			return err
		}
		existing, findErr := r.store.GetQualityAgentSessionByTarget(ctx, workspaceID, agentID, enrollment.TargetGUID)
		if findErr != nil {
			return findErr
		}
		if existing == nil {
			return err
		}
		session = existing
	}
	_, _, err = r.store.LinkQualityAgentEnrollmentSession(ctx, enrollment.ResourceID, session.ID, mode)
	return err
}

// llmConfiguredFor reports whether triage can call a model for one agent.
func (r *Runner) llmConfiguredFor(ctx context.Context, agent *store.QualityAgentMessage) bool {
	if r.llmConfigured == nil {
		return false
	}
	return r.llmConfigured(ctx, agent)
}

func intPointer(value int) *int { return &value }
