package store

import (
	"context"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CountUsers counts the active (non-deleted) principal of the given type.
func (s *Store) CountUsers(ctx context.Context, userType storepb.PrincipalType) (int, error) {
	count := 0
	// A single statement needs no transaction: opening one only adds a
	// BEGIN/COMMIT round trip.
	if err := s.GetDB().QueryRowContext(ctx, `
	SELECT COUNT(*)
	FROM principal
	WHERE principal.type = $1 AND principal.deleted = FALSE`,
		userType.String()).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
