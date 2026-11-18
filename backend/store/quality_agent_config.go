package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// QualityAgentState is the runtime lifecycle of one built-in agent. It mirrors
// the v1 QualityAgentState enum without importing the public API package.
type QualityAgentState int16

const (
	QualityAgentStateDraft    QualityAgentState = 1
	QualityAgentStateActive   QualityAgentState = 2
	QualityAgentStatePaused   QualityAgentState = 3
	QualityAgentStateDisabled QualityAgentState = 4
)

// QualityAgentReportFrequency is the patrol report cadence.
type QualityAgentReportFrequency int16

const (
	QualityAgentReportFrequencyUnspecified QualityAgentReportFrequency = 0
	QualityAgentReportFrequencyOff         QualityAgentReportFrequency = 1
	QualityAgentReportFrequencyDaily       QualityAgentReportFrequency = 2
	QualityAgentReportFrequencyWeekly      QualityAgentReportFrequency = 3
)

// QualityAgentAdoptionMode says what the auto-adoption pass does with a
// low-risk rule draft.
type QualityAgentAdoptionMode int16

const (
	QualityAgentAdoptionModeUnspecified QualityAgentAdoptionMode = 0
	QualityAgentAdoptionModeOff         QualityAgentAdoptionMode = 1
	QualityAgentAdoptionModePaused      QualityAgentAdoptionMode = 2
	QualityAgentAdoptionModeActive      QualityAgentAdoptionMode = 3
)

// QualityAgentConfigMessage is one agent's runtime configuration. A row's
// existence is what makes an agent built-in; identity lives on quality_agent.
type QualityAgentConfigMessage struct {
	AgentID int64
	State   QualityAgentState
	// OwnerPrincipalID is the only caller allowed to talk to the agent. Nil
	// means the owner was deleted; notifications fall back to workspace admins.
	OwnerPrincipalID *int32
	// Capabilities is the validated set of capability ids.
	Capabilities []string
	// ProviderProfile is an optional LLM provider profile resource name; empty
	// means the workspace default.
	ProviderProfile string

	ReportFrequency QualityAgentReportFrequency
	ReportTime      string
	ReportTimezone  string
	// ReportWeekday is 1 Monday .. 7 Sunday for WEEKLY; 0 when unused.
	ReportWeekday int16

	AdoptionMode                QualityAgentAdoptionMode
	AdoptionRuleTypes           []int16
	AdoptionConfidenceThreshold float64
	AdoptionAllowCritical       bool
	AdoptionMaxPerDay           int32

	ConversationMessagesPerHour int32
	TokensPerHour               int64
	WakesPerHour                int32
	MaxConcurrentSessions       int32
	MaxNewSessionsPerHour       int32
	MaxActiveSessions           int32

	LastActiveAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const qualityAgentConfigColumns = `agent_id, state, owner_principal_id, capabilities,
	provider_profile, report_frequency, report_time, report_timezone, report_weekday,
	adoption_mode, adoption_rule_types, adoption_confidence_threshold,
	adoption_allow_critical, adoption_max_per_day, conversation_messages_per_hour,
	tokens_per_hour, wakes_per_hour, max_concurrent_sessions, max_new_sessions_per_hour,
	max_active_sessions, last_active_at, created_at, updated_at`

// CreateQualityAgentConfig inserts one agent's runtime configuration. The agent
// row (and its principal) must already exist.
func (s *Store) CreateQualityAgentConfig(ctx context.Context, message *QualityAgentConfigMessage) (*QualityAgentConfigMessage, error) {
	if message == nil || message.AgentID <= 0 {
		return nil, common.Errorf(common.Invalid, "an agent config needs an agent id")
	}
	capabilities, err := marshalStrings(message.Capabilities)
	if err != nil {
		return nil, err
	}
	ruleTypes, err := marshalInt16s(message.AdoptionRuleTypes)
	if err != nil {
		return nil, err
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_config
			(agent_id, state, owner_principal_id, capabilities, provider_profile,
			 report_frequency, report_time, report_timezone, report_weekday,
			 adoption_mode, adoption_rule_types, adoption_confidence_threshold,
			 adoption_allow_critical, adoption_max_per_day, conversation_messages_per_hour,
			 tokens_per_hour, wakes_per_hour, max_concurrent_sessions,
			 max_new_sessions_per_hour, max_active_sessions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		RETURNING `+qualityAgentConfigColumns,
		message.AgentID, int16(message.State), nullableInt32(message.OwnerPrincipalID),
		capabilities, message.ProviderProfile, int16(message.ReportFrequency),
		message.ReportTime, message.ReportTimezone, message.ReportWeekday,
		int16(message.AdoptionMode), ruleTypes, message.AdoptionConfidenceThreshold,
		message.AdoptionAllowCritical, message.AdoptionMaxPerDay,
		message.ConversationMessagesPerHour, message.TokensPerHour,
		message.WakesPerHour, message.MaxConcurrentSessions,
		message.MaxNewSessionsPerHour, message.MaxActiveSessions)
	created, err := scanQualityAgentConfig(row)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality agent config")
	}
	return created, nil
}

// GetQualityAgentConfig reads one agent's configuration; nil means the agent is
// not a built-in agent.
func (s *Store) GetQualityAgentConfig(ctx context.Context, agentID int64) (*QualityAgentConfigMessage, error) {
	message, err := scanQualityAgentConfig(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentConfigColumns+` FROM quality_agent_config WHERE agent_id = $1`, agentID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent config")
	}
	return message, nil
}

// ListQualityAgentConfigs lists every built-in agent's configuration.
func (s *Store) ListQualityAgentConfigs(ctx context.Context) ([]*QualityAgentConfigMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentConfigColumns+` FROM quality_agent_config ORDER BY agent_id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent configs")
	}
	defer rows.Close()
	var configs []*QualityAgentConfigMessage
	for rows.Next() {
		config, err := scanQualityAgentConfig(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent config")
		}
		configs = append(configs, config)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent configs")
	}
	return configs, nil
}

// UpdateQualityAgentConfig replaces every editable configuration field except
// the state (UpdateQualityAgentState owns lifecycle transitions).
func (s *Store) UpdateQualityAgentConfig(ctx context.Context, message *QualityAgentConfigMessage) (*QualityAgentConfigMessage, error) {
	if message == nil || message.AgentID <= 0 {
		return nil, common.Errorf(common.Invalid, "an agent config needs an agent id")
	}
	capabilities, err := marshalStrings(message.Capabilities)
	if err != nil {
		return nil, err
	}
	ruleTypes, err := marshalInt16s(message.AdoptionRuleTypes)
	if err != nil {
		return nil, err
	}
	row := s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_config SET
			owner_principal_id = $2,
			capabilities = $3,
			provider_profile = $4,
			report_frequency = $5,
			report_time = $6,
			report_timezone = $7,
			report_weekday = $8,
			adoption_mode = $9,
			adoption_rule_types = $10,
			adoption_confidence_threshold = $11,
			adoption_allow_critical = $12,
			adoption_max_per_day = $13,
			conversation_messages_per_hour = $14,
			tokens_per_hour = $15,
			wakes_per_hour = $16,
			max_concurrent_sessions = $17,
			max_new_sessions_per_hour = $18,
			max_active_sessions = $19,
			updated_at = NOW()
		WHERE agent_id = $1
		RETURNING `+qualityAgentConfigColumns,
		message.AgentID, nullableInt32(message.OwnerPrincipalID), capabilities,
		message.ProviderProfile, int16(message.ReportFrequency), message.ReportTime,
		message.ReportTimezone, message.ReportWeekday, int16(message.AdoptionMode),
		ruleTypes, message.AdoptionConfidenceThreshold, message.AdoptionAllowCritical,
		message.AdoptionMaxPerDay, message.ConversationMessagesPerHour,
		message.TokensPerHour, message.WakesPerHour, message.MaxConcurrentSessions,
		message.MaxNewSessionsPerHour, message.MaxActiveSessions)
	updated, err := scanQualityAgentConfig(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality agent config %d not found", message.AgentID)
		}
		return nil, errors.Wrap(err, "failed to update the quality agent config")
	}
	return updated, nil
}

// UpdateQualityAgentState moves one agent between DRAFT/ACTIVE/PAUSED/DISABLED.
func (s *Store) UpdateQualityAgentState(ctx context.Context, agentID int64, state QualityAgentState) (*QualityAgentConfigMessage, error) {
	updated, err := scanQualityAgentConfig(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_config SET state = $2, updated_at = NOW()
		WHERE agent_id = $1
		RETURNING `+qualityAgentConfigColumns, agentID, int16(state)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality agent config %d not found", agentID)
		}
		return nil, errors.Wrap(err, "failed to update the quality agent state")
	}
	return updated, nil
}

// ReplaceQualityAgentDatabases replaces one agent's database scope in one
// transaction.
func (s *Store) ReplaceQualityAgentDatabases(ctx context.Context, agentID int64, databases []string) error {
	trimmed := make([]string, 0, len(databases))
	for _, database := range databases {
		if value := strings.TrimSpace(database); value != "" {
			trimmed = append(trimmed, value)
		}
	}
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM quality_agent_database WHERE agent_id = $1`, agentID); err != nil {
		return errors.Wrap(err, "failed to clear the agent database scope")
	}
	if len(trimmed) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quality_agent_database (agent_id, database_ref)
			SELECT $1, unnest($2::text[])
			ON CONFLICT DO NOTHING`, agentID, pq.Array(trimmed)); err != nil {
			return errors.Wrap(err, "failed to write the agent database scope")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit the agent database scope")
	}
	return nil
}

// ListQualityAgentDatabases lists one agent's database scope, sorted.
func (s *Store) ListQualityAgentDatabases(ctx context.Context, agentID int64) ([]string, error) {
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT database_ref FROM quality_agent_database WHERE agent_id = $1 ORDER BY database_ref`, agentID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the agent database scope")
	}
	defer rows.Close()
	var databases []string
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, errors.Wrap(err, "failed to scan the agent database scope")
		}
		databases = append(databases, database)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the agent database scope")
	}
	return databases, nil
}

// ListQualityAgentDatabasesByAgent lists the scope of every agent, keyed by
// agent id. The enrollment runner uses it to build each agent's scan plan.
func (s *Store) ListQualityAgentDatabasesByAgent(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT agent_id, database_ref FROM quality_agent_database ORDER BY agent_id, database_ref`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the agent database scopes")
	}
	defer rows.Close()
	result := make(map[int64][]string)
	for rows.Next() {
		var (
			agentID  int64
			database string
		)
		if err := rows.Scan(&agentID, &database); err != nil {
			return nil, errors.Wrap(err, "failed to scan an agent database scope")
		}
		result[agentID] = append(result[agentID], database)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the agent database scopes")
	}
	return result, nil
}

// scanQualityAgentConfig reads one quality_agent_config row.
func scanQualityAgentConfig(scanner interface{ Scan(dest ...any) error }) (*QualityAgentConfigMessage, error) {
	var (
		message          QualityAgentConfigMessage
		ownerPrincipalID sql.NullInt32
		capabilities     []byte
		ruleTypes        []byte
		state            int16
		reportFrequency  int16
		adoptionMode     int16
		lastActiveAt     sql.NullTime
	)
	if err := scanner.Scan(
		&message.AgentID, &state, &ownerPrincipalID, &capabilities,
		&message.ProviderProfile, &reportFrequency, &message.ReportTime,
		&message.ReportTimezone, &message.ReportWeekday, &adoptionMode,
		&ruleTypes, &message.AdoptionConfidenceThreshold,
		&message.AdoptionAllowCritical, &message.AdoptionMaxPerDay,
		&message.ConversationMessagesPerHour, &message.TokensPerHour,
		&message.WakesPerHour, &message.MaxConcurrentSessions,
		&message.MaxNewSessionsPerHour, &message.MaxActiveSessions,
		&lastActiveAt, &message.CreatedAt, &message.UpdatedAt,
	); err != nil {
		return nil, err
	}
	message.State = QualityAgentState(state)
	message.ReportFrequency = QualityAgentReportFrequency(reportFrequency)
	message.AdoptionMode = QualityAgentAdoptionMode(adoptionMode)
	if ownerPrincipalID.Valid {
		id := ownerPrincipalID.Int32
		message.OwnerPrincipalID = &id
	}
	if lastActiveAt.Valid {
		value := lastActiveAt.Time
		message.LastActiveAt = &value
	}
	decodedCapabilities, err := unmarshalStrings(capabilities)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode the agent capabilities")
	}
	message.Capabilities = decodedCapabilities
	decodedRuleTypes, err := unmarshalInt16s(ruleTypes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode the adoption rule types")
	}
	message.AdoptionRuleTypes = decodedRuleTypes
	return &message, nil
}

func marshalStrings(values []string) ([]byte, error) {
	if values == nil {
		values = []string{}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode a string list")
	}
	return payload, nil
}

func unmarshalStrings(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(payload, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func marshalInt16s(values []int16) ([]byte, error) {
	if values == nil {
		values = []int16{}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode an int list")
	}
	return payload, nil
}

func unmarshalInt16s(payload []byte) ([]int16, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var values []int16
	if err := json.Unmarshal(payload, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func nullableInt32(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}
