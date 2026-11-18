package store

import (
	"context"

	"github.com/pkg/errors"
)

// ErrQualityAgentBusy means another pass already holds the agent's advisory lock.
// The caller should leave the work for the next tick rather than queueing.
var ErrQualityAgentBusy = errors.New("the quality agent is busy")

// WithQualityAgentAdvisoryLock serializes an agent's admission-like critical
// section (auto-adoption, one conversation's hourly cap) across processes sharing
// the database. It is a session-level advisory lock on a dedicated connection; the
// callback may use the pool normally.
func (s *Store) WithQualityAgentAdvisoryLock(ctx context.Context, key string, fn func() error) error {
	conn, err := s.GetDB().Conn(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to acquire an advisory lock connection")
	}
	defer func() { _ = conn.Close() }()
	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, key).Scan(&acquired); err != nil {
		return errors.Wrap(err, "failed to acquire the quality agent advisory lock")
	}
	if !acquired {
		return ErrQualityAgentBusy
	}
	defer func() {
		// The caller's context may already be cancelled; unlocking is cleanup and
		// must still run.
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtext($1))`, key)
	}()
	return fn()
}
