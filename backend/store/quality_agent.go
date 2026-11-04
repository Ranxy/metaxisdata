package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentMessage is one agent principal: an independent identity for
// autonomous quality work. Its token is an ordinary access token for the linked
// principal, so disabling the principal ends every token.
type QualityAgentMessage struct {
	ID          int64
	ResourceID  string
	PrincipalID int
	DisplayName string
	Description string
	Enabled     bool
	CreatedBy   *int32
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const qualityAgentColumns = `id, resource_id, principal_id, display_name, description, enabled, created_by, created_at, updated_at`

// CreateQualityAgent creates the agent principal, its row and its workspace role
// binding. The principal is a SERVICE_ACCOUNT with an address only agents use; the
// binding grants exactly the quality agent role.
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
		INSERT INTO quality_agent (resource_id, principal_id, display_name, description, enabled, created_by)
		VALUES ($1, $2, $3, $4, TRUE, $5)
		RETURNING `+qualityAgentColumns,
		resourceID, principal.ID, displayName, strings.TrimSpace(description), createdBy)
	message, err := scanQualityAgent(row)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality agent")
	}
	if err := s.bindQualityAgentRole(ctx, principal.ID); err != nil {
		return nil, err
	}
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

// ListQualityAgents lists the workspace's agent principals.
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

// GetQualityAgentByPrincipalID reads the agent an authenticated principal is, or nil
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

// DisableQualityAgent disables the agent row and soft-deletes its principal, which
// is what makes every token the agent holds stop authenticating.
func (s *Store) DisableQualityAgent(ctx context.Context, resourceID string) (*QualityAgentMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var principalID int
	if err := tx.QueryRowContext(ctx, `
		UPDATE quality_agent SET enabled = FALSE, updated_at = NOW()
		WHERE resource_id = $1 RETURNING principal_id`, resourceID).Scan(&principalID); err != nil {
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
	// The auth interceptor resolves a token through the cached principal row, so the
	// cache must forget the deactivated principal or its tokens keep authenticating.
	s.invalidatePrincipalCache(principalID)
	return message, nil
}

// invalidatePrincipalCache drops the cached principal and its email entry, the two
// caches the token resolver reads.
func (s *Store) invalidatePrincipalCache(principalID int) {
	if cached, ok := s.userIDCache.Get(principalID); ok {
		s.userEmailCache.Remove(cached.Email)
	}
	s.userIDCache.Remove(principalID)
}

// InvalidateQualityAgentTokens marks every token minted for the principal before
// now invalid, which is how a rotation makes the old bearer token useless. It goes
// through UpdateUser so the principal cache is refreshed: the auth interceptor reads
// the cached row, and a raw SQL write would leave it authenticating the old token.
func (s *Store) InvalidateQualityAgentTokens(ctx context.Context, principalID int) error {
	user, err := s.GetUserByID(ctx, principalID)
	if err != nil {
		return errors.Wrap(err, "failed to read the agent principal")
	}
	if user == nil {
		return common.Errorf(common.NotFound, "agent principal %d not found", principalID)
	}
	profile := proto.CloneOf(user.Profile)
	if profile == nil {
		profile = &storepb.UserProfile{}
	}
	profile.LastChangePasswordTime = timestamppb.Now()
	if _, err := s.UpdateUser(ctx, user, &UpdateUserMessage{Profile: profile}); err != nil {
		return errors.Wrap(err, "failed to invalidate the agent tokens")
	}
	return nil
}

// scanQualityAgent reads one quality_agent row.
func scanQualityAgent(scanner interface{ Scan(dest ...any) error }) (*QualityAgentMessage, error) {
	var (
		message   QualityAgentMessage
		createdBy sql.NullInt32
	)
	if err := scanner.Scan(&message.ID, &message.ResourceID, &message.PrincipalID,
		&message.DisplayName, &message.Description, &message.Enabled, &createdBy,
		&message.CreatedAt, &message.UpdatedAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		id := createdBy.Int32
		message.CreatedBy = &id
	}
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
