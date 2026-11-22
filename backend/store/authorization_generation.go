package store

// AuthorizationGeneration returns the process-local generation of the IAM
// inputs. The scoped authorizer keys its per-principal snapshot on it, so a
// grant, role, group or agent write through this store invalidates the cache.
func (s *Store) AuthorizationGeneration() uint64 {
	return s.authorizationGeneration.Load()
}

// bumpAuthorizationGeneration is called after an IAM-relevant write commits.
func (s *Store) bumpAuthorizationGeneration() {
	s.authorizationGeneration.Add(1)
}
