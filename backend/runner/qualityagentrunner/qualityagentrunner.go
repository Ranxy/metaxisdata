// Package qualityagentrunner generates the per-agent patrol reports and appends a
// short notification to the agent conversation. It does no model work: the report
// body is a deterministic aggregate over the agent's database scope.
package qualityagentrunner

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentreport"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	tickInterval = 60 * time.Second
	passTimeout  = 2 * time.Minute
)

// Store is the slice of *store.Store the report runner needs.
type Store interface {
	GetWorkspaceID(ctx context.Context) (string, error)
	ListQualityAgents(ctx context.Context) ([]*store.QualityAgentMessage, error)
	ListQualityAgentDatabasesByAgent(ctx context.Context) (map[int64][]string, error)
	GetQualityAgentReportByPeriod(ctx context.Context, agentID int64, periodKey string) (*store.QualityAgentReportMessage, error)
	CreateQualityAgentReport(ctx context.Context, message *store.QualityAgentReportMessage) (*store.QualityAgentReportMessage, bool, error)
	BuildQualityAgentReportSnapshot(ctx context.Context, agentID int64, databases []string, since, until time.Time) (*store.QualityAgentReportSnapshot, error)
	EnsureQualityAgentConversation(ctx context.Context, workspaceID string, agentID int64) (*store.QualityAgentSessionMessage, error)
	AppendQualityAgentTranscriptMessage(ctx context.Context, input *store.QualityAgentTranscriptMessage) (*store.QualityAgentTranscriptMessage, error)
	AdvanceQualityAgentSessionWake(ctx context.Context, resourceID string, now time.Time) (bool, error)
	GetUserByID(ctx context.Context, id int) (*store.UserMessage, error)
	CreateQualityAgentAttention(ctx context.Context, message *store.QualityAgentAttentionMessage) (*store.QualityAgentAttentionMessage, bool, error)
}

// Runner generates the agents' scheduled reports.
type Runner struct {
	store        Store
	loadSettings func(ctx context.Context) (dataquality.Settings, error)
	notifier     *notification.Service
	now          func() time.Time
	tick         time.Duration
}

// NewRunner creates the report runner.
func NewRunner(stores *store.Store, notifier *notification.Service) *Runner {
	return &Runner{
		store: stores,
		loadSettings: func(ctx context.Context) (dataquality.Settings, error) {
			return dataquality.LoadSettings(ctx, stores)
		},
		notifier: notifier,
		now:      func() time.Time { return time.Now().UTC() },
		tick:     tickInterval,
	}
}

// Name identifies the runner in logs.
func (*Runner) Name() string { return "qualityagent" }

// Run drives the loop until ctx is cancelled.
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

func (r *Runner) runOnce(ctx context.Context) {
	now := r.now()
	ctx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()
	settings, err := r.loadSettings(ctx)
	if err != nil {
		slog.Error("failed to load data quality settings for the agent report runner", log.WithError(err))
		return
	}
	if !settings.AgentRunnerEnabled {
		return
	}
	workspaceID, err := r.store.GetWorkspaceID(ctx)
	if err != nil {
		slog.Error("failed to resolve the workspace for the agent report runner", log.WithError(err))
		return
	}
	agents, err := r.store.ListQualityAgents(ctx)
	if err != nil {
		slog.Error("failed to read the agents for reports", log.WithError(err))
		return
	}
	scopes, err := r.store.ListQualityAgentDatabasesByAgent(ctx)
	if err != nil {
		slog.Error("failed to read the agent scopes for reports", log.WithError(err))
		return
	}
	for _, agent := range agents {
		if agent.State != store.QualityAgentStateActive || !slices.Contains(agent.Capabilities, "patrol") {
			continue
		}
		databases := scopes[agent.ID]
		if len(databases) == 0 {
			continue
		}
		if err := r.reportAgent(ctx, workspaceID, agent, databases, now); err != nil {
			slog.Warn("failed to generate an agent report",
				slog.Int64("agent", agent.ID), log.WithError(err))
		}
	}
}

func (r *Runner) reportAgent(ctx context.Context, workspaceID string, agent *store.QualityAgentMessage, databases []string, now time.Time) error {
	schedule, ok, err := dueReport(agent, now)
	if err != nil || !ok {
		return err
	}
	existing, err := r.store.GetQualityAgentReportByPeriod(ctx, agent.ID, schedule.periodKey)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	snapshot, err := r.store.BuildQualityAgentReportSnapshot(ctx, agent.ID, databases,
		schedule.periodStart, schedule.periodEnd)
	if err != nil {
		return err
	}
	language := r.ownerLanguage(ctx, agent)
	body := agentreport.Render(snapshot, schedule.periodStart, schedule.periodEnd, schedule.location, language)
	report, created, err := r.store.CreateQualityAgentReport(ctx, &store.QualityAgentReportMessage{
		WorkspaceID:          workspaceID,
		AgentID:              agent.ID,
		PeriodKind:           schedule.periodKind,
		PeriodKey:            schedule.periodKey,
		PeriodStart:          schedule.periodStart,
		PeriodEnd:            schedule.periodEnd,
		Scope:                databases,
		TargetCount:          snapshot.TargetCount,
		CoveredTargetCount:   snapshot.CoveredTargetCount,
		RuleCount:            snapshot.RuleCount,
		ExecutionCount:       snapshot.ExecutionCount,
		PassedCount:          snapshot.PassedCount,
		WarnedCount:          snapshot.WarnedCount,
		FailedCount:          snapshot.FailedCount,
		ErroredCount:         snapshot.ErroredCount,
		NewIssueCount:        snapshot.NewIssueCount,
		OpenIssueCount:       snapshot.OpenIssueCount,
		PendingProposalCount: snapshot.PendingProposalCount,
		BodyMarkdown:         body,
		GeneratedAt:          &now,
	})
	if err != nil || !created {
		return err
	}
	conversation, err := r.store.EnsureQualityAgentConversation(ctx, workspaceID, agent.ID)
	if err != nil {
		return err
	}
	content := agentlang.ReportReady(language,
		schedule.periodStart.Format("2006-01-02"), schedule.periodEnd.Format("2006-01-02"))
	_, appendErr := r.store.AppendQualityAgentTranscriptMessage(ctx, &store.QualityAgentTranscriptMessage{
		SessionID:      conversation.ID,
		WorkspaceID:    workspaceID,
		Wake:           conversation.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
		Content:        content,
		Reference:      "qualityAgentReports/" + report.ResourceID,
		IdempotencyKey: "report:" + report.ResourceID,
	})
	if appendErr != nil {
		return appendErr
	}
	if _, err := r.store.AdvanceQualityAgentSessionWake(ctx, conversation.ResourceID, now); err != nil {
		return err
	}
	r.escalateAnomaly(ctx, workspaceID, agent, report, snapshot)
	return nil
}

// ownerLanguage reads the owner's chosen server-prose language; an absent owner or
// profile falls back to English.
func (r *Runner) ownerLanguage(ctx context.Context, agent *store.QualityAgentMessage) string {
	if agent == nil || agent.OwnerPrincipalID == nil {
		return agentlang.English
	}
	owner, err := r.store.GetUserByID(ctx, int(*agent.OwnerPrincipalID))
	if err != nil {
		return agentlang.English
	}
	return agentlang.Of(owner)
}

// escalateAnomaly opens one owner attention when a report has failures or new
// issues; the notification is the doorbell.
func (r *Runner) escalateAnomaly(ctx context.Context, workspaceID string, agent *store.QualityAgentMessage, report *store.QualityAgentReportMessage, snapshot *store.QualityAgentReportSnapshot) {
	if r.notifier == nil {
		return
	}
	if snapshot.FailedCount == 0 && snapshot.NewIssueCount == 0 {
		return
	}
	if agent == nil {
		return
	}
	language := r.ownerLanguage(ctx, agent)
	attention, created, err := r.store.CreateQualityAgentAttention(ctx, &store.QualityAgentAttentionMessage{
		WorkspaceID: workspaceID,
		AgentID:     agent.ID,
		Kind:        store.QualityAgentAttentionKindReportAnomaly,
		Title:       agentlang.AttentionTitle(language, store.QualityAgentAttentionKindReportAnomaly),
		Question:    agentlang.ReportQuestion(language, snapshot.FailedCount, snapshot.NewIssueCount),
		DedupeKey:   "report:" + report.ResourceID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil || !created {
		return
	}
	detail := &storepb.QualityAgentAttentionDetail{
		Agent:     common.FormatWorkspace(workspaceID) + "/qualityAgents/" + agent.ResourceID,
		Attention: common.FormatWorkspace(workspaceID) + "/qualityAgentAttentions/" + attention.ResourceID,
		Kind:      int32(store.QualityAgentAttentionKindReportAnomaly),
		Title:     attention.Title,
		Question:  attention.Question,
	}
	dedupeKey := "dqagent:" + attention.ResourceID
	if agent.OwnerPrincipalID != nil {
		if sendErr := r.notifier.SendQualityAgentAttention(ctx, *agent.OwnerPrincipalID, dedupeKey, detail); sendErr != nil {
			slog.Warn("failed to send a report anomaly notification", log.WithError(sendErr))
		}
	} else if sendErr := r.notifier.SendQualityAgentAttentionToAdmins(ctx, dedupeKey, detail); sendErr != nil {
		slog.Warn("failed to send a report anomaly notification to admins", log.WithError(sendErr))
	}
}

type reportSchedule struct {
	periodKind  store.QualityAgentReportPeriod
	periodKey   string
	periodStart time.Time
	periodEnd   time.Time
	location    *time.Location
}

func dueReport(agent *store.QualityAgentMessage, now time.Time) (reportSchedule, bool, error) {
	if agent.ReportFrequency != store.QualityAgentReportFrequencyDaily &&
		agent.ReportFrequency != store.QualityAgentReportFrequencyWeekly {
		return reportSchedule{}, false, nil
	}
	location, err := time.LoadLocation(agent.ReportTimezone)
	if err != nil {
		return reportSchedule{}, false, fmt.Errorf("invalid report timezone %q: %w", agent.ReportTimezone, err)
	}
	hour, minute, err := parseClock(agent.ReportTime)
	if err != nil {
		return reportSchedule{}, false, err
	}
	localNow := now.In(location)
	switch agent.ReportFrequency {
	case store.QualityAgentReportFrequencyDaily:
		scheduled := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
		if localNow.Before(scheduled) {
			scheduled = scheduled.AddDate(0, 0, -1)
		}
		return reportSchedule{
			periodKind:  store.QualityAgentReportPeriodDaily,
			periodKey:   scheduled.Format("2006-01-02"),
			periodStart: scheduled.AddDate(0, 0, -1),
			periodEnd:   scheduled,
			location:    location,
		}, true, nil
	case store.QualityAgentReportFrequencyWeekly:
		weekday := time.Weekday(int(agent.ReportWeekday) % 7)
		scheduled := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
		for scheduled.Weekday() != weekday {
			scheduled = scheduled.AddDate(0, 0, -1)
		}
		if localNow.Before(scheduled) {
			scheduled = scheduled.AddDate(0, 0, -7)
		}
		year, week := scheduled.ISOWeek()
		return reportSchedule{
			periodKind:  store.QualityAgentReportPeriodWeekly,
			periodKey:   fmt.Sprintf("%d-W%02d", year, week),
			periodStart: scheduled.AddDate(0, 0, -7),
			periodEnd:   scheduled,
			location:    location,
		}, true, nil
	default:
		return reportSchedule{}, false, nil
	}
}

func parseClock(value string) (int, int, error) {
	value += ":00"
	var hour, minute, second int
	if _, err := fmt.Sscanf(value, "%d:%d:%d", &hour, &minute, &second); err != nil {
		return 0, 0, fmt.Errorf("invalid report time %q", value)
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid report time %q", value)
	}
	return hour, minute, nil
}
