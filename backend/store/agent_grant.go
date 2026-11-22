package store

import (
	"context"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// AgentGrantRelation is one role that the quality-agent ownership model implies
// over one database scope. The relation is derived from the agent and its
// database scope, never stored: changing the owner or the scope changes the
// authorization without a grant row.
type AgentGrantRelation struct {
	// Role is the resource name, e.g. roles/qualityOwner.
	Role string
	// DatabaseRef is the canonical database resource name:
	// instances/{instance}/databases/{database}.
	DatabaseRef string
}

// ListAgentGrantRelations returns the derived role relations for one principal:
// qualityOwner on the databases of every non-disabled agent it owns, and
// qualityAgent on the databases of every active agent it is the identity of.
// A draft agent's owner already manages its scope; a non-active agent's identity
// does not act until the agent starts.
func (s *Store) ListAgentGrantRelations(ctx context.Context, principalID int) ([]*AgentGrantRelation, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT $3::text, d.database_ref
		FROM quality_agent a
		JOIN quality_agent_database d ON d.agent_id = a.id
		WHERE a.owner_principal_id = $1 AND a.state <> $4
		UNION ALL
		SELECT $5::text, d.database_ref
		FROM quality_agent a
		JOIN quality_agent_database d ON d.agent_id = a.id
		WHERE a.principal_id = $1 AND a.state = $2
		ORDER BY 1, 2`,
		principalID,
		QualityAgentStateActive,
		common.FormatRole(QualityOwnerRole),
		QualityAgentStateDisabled,
		common.FormatRole(QualityAgentRole),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the agent grant relations")
	}
	defer rows.Close()
	var relations []*AgentGrantRelation
	for rows.Next() {
		relation := &AgentGrantRelation{}
		if err := rows.Scan(&relation.Role, &relation.DatabaseRef); err != nil {
			return nil, errors.Wrap(err, "failed to scan the agent grant relation")
		}
		relations = append(relations, relation)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the agent grant relations")
	}
	return relations, nil
}
