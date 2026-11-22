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
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentState is the runtime lifecycle of one built-in agent. It mirrors
// the v1 QualityAgentState enum without importing the public API package. State
// is the only lifecycle truth: there is no separate enabled flag.
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

// QualityAgentMessage is one built-in agent: its internal principal (an IAM and
// audit actor, never an API caller) plus the runtime configuration merged from
// the former quality_agent_config columns. State is the only lifecycle truth.
type QualityAgentMessage struct {
	ID          int64
	ResourceID  string
	PrincipalID int
	DisplayName string
	Description string
	// Language is the BCP-47 tag for owner-visible prose. Empty follows the owner.
	Language string
	// ProviderProfile is the LLM provider profile resource name the agent runs on,
	// and ProviderModel is the model that profile must enable. Both are set
	// together; empty means not configured.
	ProviderProfile string
	ProviderModel   string

	State QualityAgentState
	// OwnerPrincipalID is the only human allowed to talk to the agent. Nil means
	// the owner was deleted; notifications fall back to workspace admins.
	OwnerPrincipalID *int32
	// Capabilities is the validated set of capability ids.
	Capabilities []string

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

	CreatedBy *int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

const qualityAgentColumns = `id, resource_id, principal_id, display_name, description, language,
	provider_profile, provider_model, state, owner_principal_id, capabilities,
	report_frequency, report_time, report_timezone, report_weekday, adoption_mode,
	adoption_rule_types, adoption_confidence_threshold, adoption_allow_critical,
	adoption_max_per_day, conversation_messages_per_hour, tokens_per_hour,
	wakes_per_hour, max_concurrent_sessions, max_new_sessions_per_hour,
	max_active_sessions, created_by, created_at, updated_at`

// CreateQualityAgent creates the agent principal, its row with the default
// runtime configuration and its workspace role binding. The principal is a
// SERVICE_ACCOUNT that exists only as the agent's IAM and audit actor.
func (s *Store) CreateQualityAgent(ctx context.Context, resourceID, displayName, description string, createdBy *int32) (*QualityAgentMessage, error) {
	displayName = strings.TrimSpace(displayName)
	if resourceID == "" || displayName == "" {
		return nil, common.Errorf(common.Invalid, "an agent needs a resource id and a display name")
	}
	email := "agent-" + strings.ToLower(resourceID) + "@agents.metaxisdata.local"
	principal, err := s.CreateUser(ctx, &UserMessage{
		Email:        email,
		Name:         displayName,
		Type:         storepb.PrincipalType_SERVICE_ACCOUNT,
		PasswordHash: "",
		Profile:      &storepb.UserProfile{},
	}, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the agent principal")
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent (resource_id, principal_id, display_name, description, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+qualityAgentColumns,
		resourceID, principal.ID, displayName, strings.TrimSpace(description), createdBy)
	message, err := scanQualityAgent(row)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality agent")
	}
	if err := s.bindQualityAgentRole(ctx, principal.ID); err != nil {
		return nil, err
	}
	s.bumpAuthorizationGeneration()
	return message, nil
}

// bindQualityAgentRole adds the agent principal to the qualityAgent role binding,
// retrying the optimistic policy write a few times so a concurrent IAM change does
// not make agent creation fail spuriously.
func (s *Store) bindQualityAgentRole(ctx context.Context, principalID int) error {
	member := common.FormatUserUID(principalID)
	role := common.RolePrefix + QualityAgentRole
	for attempt := 0; attempt < 5; attempt++ {
		current, err := s.GetWorkspaceIamPolicy(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to read the workspace policy")
		}
		policy := current.Policy
		if policy == nil {
			return common.Errorf(common.Internal, "the workspace has no IAM policy")
		}
		found := false
		for _, binding := range policy.GetBindings() {
			if binding.GetRole() != role {
				continue
			}
			found = true
			if !slicesContains(binding.GetMembers(), member) {
				binding.Members = append(binding.Members, member)
			}
		}
		if !found {
			policy.Bindings = append(policy.Bindings, &storepb.Binding{
				Role:    role,
				Members: []string{member},
			})
		}
		if _, err := s.SetWorkspaceIamPolicy(ctx, policy, current.Etag); err != nil {
			if errors.Is(err, ErrPolicyEtagMismatch) {
				continue
			}
			return errors.Wrap(err, "failed to bind the quality agent role")
		}
		return nil
	}
	return common.Errorf(common.Internal, "failed to bind the quality agent role; the policy kept changing")
}

// ListQualityAgents lists the workspace's agents, oldest first.
func (s *Store) ListQualityAgents(ctx context.Context) ([]*QualityAgentMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentColumns+` FROM quality_agent ORDER BY id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agents")
	}
	defer rows.Close()
	var agents []*QualityAgentMessage
	for rows.Next() {
		agent, err := scanQualityAgent(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent")
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agents")
	}
	return agents, nil
}

// GetQualityAgentByResourceID reads one agent; nil means it does not exist.
func (s *Store) GetQualityAgentByResourceID(ctx context.Context, resourceID string) (*QualityAgentMessage, error) {
	agent, err := scanQualityAgent(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentColumns+` FROM quality_agent WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent")
	}
	return agent, nil
}

// GetQualityAgentByID reads one agent row; nil means it does not exist.
func (s *Store) GetQualityAgentByID(ctx context.Context, id int64) (*QualityAgentMessage, error) {
	if id <= 0 {
		return nil, nil
	}
	agent, err := scanQualityAgent(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentColumns+` FROM quality_agent WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent")
	}
	return agent, nil
}

// GetQualityAgentByPrincipalID reads the agent an internal principal is, or nil
// when the principal is not an agent.
func (s *Store) GetQualityAgentByPrincipalID(ctx context.Context, principalID int) (*QualityAgentMessage, error) {
	agent, err := scanQualityAgent(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentColumns+` FROM quality_agent WHERE principal_id = $1`, principalID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent by principal")
	}
	return agent, nil
}

// UpdateQualityAgentIdentity replaces the display name and description of one
// agent. The principal is untouched.
func (s *Store) UpdateQualityAgentIdentity(ctx context.Context, resourceID, displayName, description string) (*QualityAgentMessage, error) {
	displayName = strings.TrimSpace(displayName)
	description = strings.TrimSpace(description)
	if displayName == "" {
		return nil, common.Errorf(common.Invalid, "an agent needs a display name")
	}
	message, err := scanQualityAgent(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent SET display_name = $2, description = $3, updated_at = NOW()
		WHERE resource_id = $1
		RETURNING `+qualityAgentColumns, resourceID, displayName, description))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to update the quality agent identity")
	}
	return message, nil
}

// UpdateQualityAgentConfig writes every editable runtime field except the state
// (UpdateQualityAgentState owns lifecycle transitions) and the identity.
func (s *Store) UpdateQualityAgentConfig(ctx context.Context, message *QualityAgentMessage) (*QualityAgentMessage, error) {
	if message == nil || message.ID <= 0 {
		return nil, common.Errorf(common.Invalid, "an agent config needs an agent id")
	}
	// The API requires the pair, but the store is the last line of defense for a future
	// caller: a profile without a model (or the reverse) could never resolve.
	if (message.ProviderProfile == "") != (message.ProviderModel == "") {
		return nil, common.Errorf(common.Invalid, "an agent needs both a provider profile and a model")
	}
	capabilities, err := marshalStrings(message.Capabilities)
	if err != nil {
		return nil, err
	}
	ruleTypes, err := marshalInt16s(message.AdoptionRuleTypes)
	if err != nil {
		return nil, err
	}
	updated, err := scanQualityAgent(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent SET
			owner_principal_id = $2,
			capabilities = $3,
			report_frequency = $4,
			report_time = $5,
			report_timezone = $6,
			report_weekday = $7,
			adoption_mode = $8,
			adoption_rule_types = $9,
			adoption_confidence_threshold = $10,
			adoption_allow_critical = $11,
			adoption_max_per_day = $12,
			conversation_messages_per_hour = $13,
			tokens_per_hour = $14,
			wakes_per_hour = $15,
			max_concurrent_sessions = $16,
			max_new_sessions_per_hour = $17,
			max_active_sessions = $18,
			language = $19,
			provider_profile = $20,
			provider_model = $21,
			updated_at = NOW()
		WHERE id = $1
		RETURNING `+qualityAgentColumns,
		message.ID, nullableInt32(message.OwnerPrincipalID), capabilities,
		int16(message.ReportFrequency), message.ReportTime, message.ReportTimezone,
		message.ReportWeekday, int16(message.AdoptionMode), ruleTypes,
		message.AdoptionConfidenceThreshold, message.AdoptionAllowCritical,
		message.AdoptionMaxPerDay, message.ConversationMessagesPerHour,
		message.TokensPerHour, message.WakesPerHour, message.MaxConcurrentSessions,
		message.MaxNewSessionsPerHour, message.MaxActiveSessions, message.Language,
		message.ProviderProfile, message.ProviderModel))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality agent %d not found", message.ID)
		}
		return nil, errors.Wrap(err, "failed to update the quality agent config")
	}
	s.bumpAuthorizationGeneration()
	return updated, nil
}

// UpdateQualityAgentAdoption writes only the guardrail mode. Writing the whole config
// row from the adoption RPC would let a toggle revert a concurrent provider change.
func (s *Store) UpdateQualityAgentAdoption(ctx context.Context, agentID int64, mode QualityAgentAdoptionMode) (*QualityAgentMessage, error) {
	updated, err := scanQualityAgent(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent SET adoption_mode = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING `+qualityAgentColumns, agentID, int16(mode)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality agent %d not found", agentID)
		}
		return nil, errors.Wrap(err, "failed to update the quality agent adoption")
	}
	return updated, nil
}

// UpdateQualityAgentState moves one agent between DRAFT/ACTIVE/PAUSED/DISABLED.
func (s *Store) UpdateQualityAgentState(ctx context.Context, agentID int64, state QualityAgentState) (*QualityAgentMessage, error) {
	updated, err := scanQualityAgent(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent SET state = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING `+qualityAgentColumns, agentID, int16(state)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality agent %d not found", agentID)
		}
		return nil, errors.Wrap(err, "failed to update the quality agent state")
	}
	s.bumpAuthorizationGeneration()
	return updated, nil
}

// DisableQualityAgent moves the agent to DISABLED and soft-deletes its principal.
// DISABLED is the terminal state; the soft-deleted principal keeps the audit
// actor resolvable while ending any credential it could still hold.
func (s *Store) DisableQualityAgent(ctx context.Context, resourceID string) (*QualityAgentMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var principalID int
	if err := tx.QueryRowContext(ctx, `
		UPDATE quality_agent SET state = $2, updated_at = NOW()
		WHERE resource_id = $1 RETURNING principal_id`,
		resourceID, int16(QualityAgentStateDisabled)).Scan(&principalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to disable the quality agent")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE principal SET deleted = TRUE WHERE id = $1`, principalID); err != nil {
		return nil, errors.Wrap(err, "failed to deactivate the agent principal")
	}
	message, err := scanQualityAgent(tx.QueryRowContext(ctx,
		`SELECT `+qualityAgentColumns+` FROM quality_agent WHERE resource_id = $1`, resourceID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the disabled quality agent")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the agent deactivation")
	}
	s.invalidatePrincipalCache(principalID)
	return message, nil
}

// invalidatePrincipalCache drops the cached principal and its email entry, the two
// caches the auth resolver reads.
func (s *Store) invalidatePrincipalCache(principalID int) {
	if cached, ok := s.userIDCache.Get(principalID); ok {
		s.userEmailCache.Remove(cached.Email)
	}
	s.userIDCache.Remove(principalID)
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
	s.bumpAuthorizationGeneration()
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

// scanQualityAgent reads one quality_agent row.
func scanQualityAgent(scanner interface{ Scan(dest ...any) error }) (*QualityAgentMessage, error) {
	var (
		message          QualityAgentMessage
		state            int16
		ownerPrincipalID sql.NullInt32
		capabilities     []byte
		reportFrequency  int16
		adoptionMode     int16
		ruleTypes        []byte
		createdBy        sql.NullInt32
	)
	if err := scanner.Scan(
		&message.ID, &message.ResourceID, &message.PrincipalID, &message.DisplayName,
		&message.Description, &message.Language, &message.ProviderProfile,
		&message.ProviderModel, &state, &ownerPrincipalID, &capabilities,
		&reportFrequency, &message.ReportTime, &message.ReportTimezone,
		&message.ReportWeekday, &adoptionMode, &ruleTypes,
		&message.AdoptionConfidenceThreshold, &message.AdoptionAllowCritical,
		&message.AdoptionMaxPerDay, &message.ConversationMessagesPerHour,
		&message.TokensPerHour, &message.WakesPerHour, &message.MaxConcurrentSessions,
		&message.MaxNewSessionsPerHour, &message.MaxActiveSessions, &createdBy,
		&message.CreatedAt, &message.UpdatedAt,
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
	if createdBy.Valid {
		id := createdBy.Int32
		message.CreatedBy = &id
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

// slicesContains is the small membership check the policy binding needs.
func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
