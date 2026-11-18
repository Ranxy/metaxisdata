package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentReportTrigger says what produced a report.
type QualityAgentReportTrigger int16

const (
	QualityAgentReportTriggerScheduled QualityAgentReportTrigger = 1
	QualityAgentReportTriggerOnDemand  QualityAgentReportTrigger = 2
)

// QualityAgentReportPeriod is the report's period shape.
type QualityAgentReportPeriod int16

const (
	QualityAgentReportPeriodDaily    QualityAgentReportPeriod = 1
	QualityAgentReportPeriodWeekly   QualityAgentReportPeriod = 2
	QualityAgentReportPeriodOnDemand QualityAgentReportPeriod = 3
)

// QualityAgentReportState is the asynchronous generation state.
type QualityAgentReportState int16

const (
	QualityAgentReportStatePending QualityAgentReportState = 1
	QualityAgentReportStateReady   QualityAgentReportState = 2
	QualityAgentReportStateFailed  QualityAgentReportState = 3
)

// QualityAgentReportMessage is one per-agent patrol report.
type QualityAgentReportMessage struct {
	ID                   int64
	ResourceID           string
	WorkspaceID          string
	AgentID              int64
	Trigger              QualityAgentReportTrigger
	PeriodKind           QualityAgentReportPeriod
	PeriodKey            string
	PeriodStart          time.Time
	PeriodEnd            time.Time
	State                QualityAgentReportState
	Scope                []string
	TargetCount          int32
	CoveredTargetCount   int32
	RuleCount            int32
	ExecutionCount       int32
	PassedCount          int32
	WarnedCount          int32
	FailedCount          int32
	ErroredCount         int32
	NewIssueCount        int32
	OpenIssueCount       int32
	PendingProposalCount int32
	TokensUsed           int64
	Severity             int16
	BodyMarkdown         string
	FailureReason        string
	CreatedAt            time.Time
	GeneratedAt          *time.Time
}

const qualityAgentReportColumns = `id, resource_id, workspace_id, agent_id, trigger,
	period_kind, period_key, period_start, period_end, state, scope, target_count,
	covered_target_count, rule_count, execution_count, passed_count, warned_count,
	failed_count, errored_count, new_issue_count, open_issue_count,
	pending_proposal_count, tokens_used, severity, body_markdown, failure_reason,
	created_at, generated_at`

// CreateQualityAgentReport inserts one report row. The unique period index makes a
// retried tick converge on one report; an existing row is returned with created=false.
func (s *Store) CreateQualityAgentReport(ctx context.Context, message *QualityAgentReportMessage) (*QualityAgentReportMessage, bool, error) {
	if message == nil || message.AgentID <= 0 || message.WorkspaceID == "" {
		return nil, false, common.Errorf(common.Invalid, "a report needs an agent and a workspace")
	}
	resourceID := message.ResourceID
	var err error
	if resourceID == "" {
		resourceID, err = common.RandomString(12)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to generate the report id")
		}
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_report
			(resource_id, workspace_id, agent_id, trigger, period_kind, period_key,
			 period_start, period_end, state, scope, target_count, covered_target_count,
			 rule_count, execution_count, passed_count, warned_count, failed_count,
			 errored_count, new_issue_count, open_issue_count, pending_proposal_count,
			 tokens_used, severity, body_markdown, failure_reason, generated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
		ON CONFLICT (agent_id, trigger, period_key) DO NOTHING
		RETURNING `+qualityAgentReportColumns,
		resourceID, message.WorkspaceID, message.AgentID, int16(message.Trigger),
		int16(message.PeriodKind), message.PeriodKey, message.PeriodStart, message.PeriodEnd,
		int16(message.State), pq.Array(message.Scope), message.TargetCount, message.CoveredTargetCount,
		message.RuleCount, message.ExecutionCount, message.PassedCount, message.WarnedCount,
		message.FailedCount, message.ErroredCount, message.NewIssueCount,
		message.OpenIssueCount, message.PendingProposalCount, message.TokensUsed,
		message.Severity, message.BodyMarkdown, message.FailureReason,
		message.GeneratedAt)
	created, err := scanQualityAgentReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		existing, readErr := s.GetQualityAgentReportByPeriod(ctx, message.AgentID, message.Trigger, message.PeriodKey)
		if readErr != nil {
			return nil, false, readErr
		}
		if existing == nil {
			return nil, false, errors.New("the report period conflicted but no row was found")
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create the quality agent report")
	}
	return created, true, nil
}

// GetQualityAgentReportByPeriod reads one period's report; nil means absent.
func (s *Store) GetQualityAgentReportByPeriod(ctx context.Context, agentID int64, trigger QualityAgentReportTrigger, periodKey string) (*QualityAgentReportMessage, error) {
	message, err := scanQualityAgentReport(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentReportColumns+` FROM quality_agent_report
		 WHERE agent_id = $1 AND trigger = $2 AND period_key = $3`,
		agentID, int16(trigger), periodKey))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent report")
	}
	return message, nil
}

// GetQualityAgentReportByResourceID reads one report; nil means absent.
func (s *Store) GetQualityAgentReportByResourceID(ctx context.Context, resourceID string) (*QualityAgentReportMessage, error) {
	message, err := scanQualityAgentReport(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentReportColumns+` FROM quality_agent_report WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent report")
	}
	return message, nil
}

// ListQualityAgentReports lists an agent's reports, newest first.
func (s *Store) ListQualityAgentReports(ctx context.Context, agentID int64, limit int) ([]*QualityAgentReportMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentReportColumns+` FROM quality_agent_report
		 WHERE agent_id = $1 ORDER BY period_end DESC, id DESC LIMIT $2`, agentID, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent reports")
	}
	defer rows.Close()
	var reports []*QualityAgentReportMessage
	for rows.Next() {
		report, err := scanQualityAgentReport(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent report")
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent reports")
	}
	return reports, nil
}

// QualityAgentReportSnapshot is the deterministic aggregate over one agent's scope.
type QualityAgentReportSnapshot struct {
	Scope                []string
	TargetCount          int32
	CoveredTargetCount   int32
	RuleCount            int32
	ExecutionCount       int32
	PassedCount          int32
	WarnedCount          int32
	FailedCount          int32
	ErroredCount         int32
	NewIssueCount        int32
	OpenIssueCount       int32
	PendingProposalCount int32
}

// BuildQualityAgentReportSnapshot aggregates one period over the agent's database
// scope. Executions, rules and issues are filtered by the target GUID prefix of each
// database, so the report covers exactly the databases the agent owns.
func (s *Store) BuildQualityAgentReportSnapshot(ctx context.Context, agentID int64, databases []string, since, until time.Time) (*QualityAgentReportSnapshot, error) {
	prefixes, err := databaseGUIDPrefixes(databases)
	if err != nil {
		return nil, err
	}
	snapshot := &QualityAgentReportSnapshot{Scope: append([]string(nil), databases...)}
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE state <> $3),
			COUNT(*) FILTER (WHERE state = $4)
		FROM quality_agent_enrollment
		WHERE agent_id = $1 AND database_ref = ANY($2)`,
		agentID, pq.Array(databases),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
	).Scan(&snapshot.TargetCount, &snapshot.CoveredTargetCount); err != nil {
		return nil, errors.Wrap(err, "failed to count the report targets")
	}
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_rule
		WHERE state = $2 AND EXISTS (
			SELECT 1 FROM unnest($1::text[]) p WHERE left(target_guid, length(p)) = p
		)`, pq.Array(prefixes), int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED),
	).Scan(&snapshot.RuleCount); err != nil {
		return nil, errors.Wrap(err, "failed to count the report rules")
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT status, COUNT(*) FROM quality_execution
		WHERE state = $4 AND finished_at >= $2 AND finished_at < $3 AND EXISTS (
			SELECT 1 FROM unnest($1::text[]) p WHERE left(target_guid, length(p)) = p
		) GROUP BY status`,
		pq.Array(prefixes), since, until,
		int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED))
	if err != nil {
		return nil, errors.Wrap(err, "failed to count the report executions")
	}
	defer rows.Close()
	for rows.Next() {
		var status int16
		var count int32
		if err := rows.Scan(&status, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan the report executions")
		}
		switch storepb.QualityStatus(status) {
		case storepb.QualityStatus_QUALITY_STATUS_PASS:
			snapshot.PassedCount += count
			snapshot.ExecutionCount += count
		case storepb.QualityStatus_QUALITY_STATUS_WARN:
			snapshot.WarnedCount += count
			snapshot.ExecutionCount += count
		case storepb.QualityStatus_QUALITY_STATUS_FAIL:
			snapshot.FailedCount += count
			snapshot.ExecutionCount += count
		case storepb.QualityStatus_QUALITY_STATUS_ERROR:
			snapshot.ErroredCount += count
			snapshot.ExecutionCount += count
		default:
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the report executions")
	}
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE first_seen_at >= $2 AND first_seen_at < $3),
			COUNT(*) FILTER (WHERE status <> 3)
		FROM quality_issue
		WHERE EXISTS (
			SELECT 1 FROM unnest($1::text[]) p WHERE left(target_guid, length(p)) = p
		)`, pq.Array(prefixes), since, until,
	).Scan(&snapshot.NewIssueCount, &snapshot.OpenIssueCount); err != nil {
		return nil, errors.Wrap(err, "failed to count the report issues")
	}
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_suggestion
		WHERE agent_id = $1 AND status = $2`,
		agentID, int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING),
	).Scan(&snapshot.PendingProposalCount); err != nil {
		return nil, errors.Wrap(err, "failed to count the report pending proposals")
	}
	return snapshot, nil
}

func databaseGUIDPrefixes(databases []string) ([]string, error) {
	prefixes := make([]string, 0, len(databases))
	for _, database := range databases {
		instance, name, err := common.GetInstanceDatabaseID(database)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid database scope %q", database)
		}
		prefixes = append(prefixes, common.BuildMetaGUID(instance, name)+common.MetaGUIDSplit)
	}
	return prefixes, nil
}

func scanQualityAgentReport(scanner interface{ Scan(dest ...any) error }) (*QualityAgentReportMessage, error) {
	var (
		message     QualityAgentReportMessage
		trigger     int16
		periodKind  int16
		state       int16
		generatedAt sql.NullTime
	)
	if err := scanner.Scan(
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.AgentID,
		&trigger, &periodKind, &message.PeriodKey, &message.PeriodStart, &message.PeriodEnd,
		&state, pq.Array(&message.Scope), &message.TargetCount, &message.CoveredTargetCount,
		&message.RuleCount, &message.ExecutionCount, &message.PassedCount,
		&message.WarnedCount, &message.FailedCount, &message.ErroredCount,
		&message.NewIssueCount, &message.OpenIssueCount, &message.PendingProposalCount,
		&message.TokensUsed, &message.Severity, &message.BodyMarkdown,
		&message.FailureReason, &message.CreatedAt, &generatedAt,
	); err != nil {
		return nil, err
	}
	message.Trigger = QualityAgentReportTrigger(trigger)
	message.PeriodKind = QualityAgentReportPeriod(periodKind)
	message.State = QualityAgentReportState(state)
	if generatedAt.Valid {
		value := generatedAt.Time
		message.GeneratedAt = &value
	}
	return &message, nil
}
