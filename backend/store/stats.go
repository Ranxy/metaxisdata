package store

import (
	"context"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CountUsers counts the active (non-deleted) principal of the given type.
func (s *Store) CountUsers(ctx context.Context, userType storepb.PrincipalType) (int, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	count := 0

	if err := tx.QueryRowContext(ctx, `
	SELECT COUNT(*)
	FROM principal
	WHERE principal.type = $1 AND principal.deleted = FALSE`,
		userType.String()).Scan(&count); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return count, nil
}
