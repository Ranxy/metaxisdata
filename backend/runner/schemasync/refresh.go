package schemasync

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// definitionRefreshDue reports whether a database's periodic full DDL refresh is
// due, and where its rotation cursor stands. A database this process has not
// refreshed yet is due, so the first sync after a restart re-reads every DDL.
func (s *Syncer) definitionRefreshDue(key string) (bool, int) {
	s.definitionMu.Lock()
	defer s.definitionMu.Unlock()
	state := s.definitionState[key]
	if state == nil {
		return true, 0
	}
	return !time.Now().Before(state.lastFullRefresh.Add(objectDefinitionFullRefreshInterval)), state.cursor
}

// recordDefinitionRefresh records a committed DDL plan. A change-only plan leaves
// the rotation and the clock alone: only a full refresh advances them.
func (s *Syncer) recordDefinitionRefresh(key string, plan definitionFetchPlan) {
	if !plan.full {
		return
	}
	s.definitionMu.Lock()
	defer s.definitionMu.Unlock()
	if s.definitionState == nil {
		s.definitionState = map[string]*objectDefinitionState{}
	}
	state := s.definitionState[key]
	if state == nil {
		state = &objectDefinitionState{}
		s.definitionState[key] = state
	}
	state.lastFullRefresh = time.Now()
	state.cursor = plan.nextCursor
}

// statisticsRefreshDue reports whether a database's table statistics are due for
// a refresh. A database this process has not refreshed yet is due, so the first
// sync after a restart brings the estimates it displays up to date.
func (s *Syncer) statisticsRefreshDue(key string) bool {
	s.statisticsMu.Lock()
	defer s.statisticsMu.Unlock()
	last, ok := s.statisticsState[key]
	if !ok {
		return true
	}
	return !time.Now().Before(last.Add(statisticsRefreshInterval))
}

// recordStatisticsRefresh records a committed statistics refresh.
func (s *Syncer) recordStatisticsRefresh(key string) {
	s.statisticsMu.Lock()
	defer s.statisticsMu.Unlock()
	if s.statisticsState == nil {
		s.statisticsState = map[string]time.Time{}
	}
	s.statisticsState[key] = time.Now()
}

// shouldSyncNow reports whether a resource whose last successful sync was
// lastSyncTime, under an instance whose sync interval is interval, is due at
// now. An interval of defaultSyncInterval means "never sync": it must be
// rejected explicitly, because lastSyncTime.Add(0) is never after now and would
// otherwise schedule every deactivated instance on every tick.
func shouldSyncNow(interval time.Duration, lastSyncTime, now time.Time) bool {
	if interval == defaultSyncInterval {
		return false
	}
	return !now.Before(lastSyncTime.Add(interval))
}
func getOrDefaultSyncInterval(instance *store.InstanceMessage) time.Duration {
	if !instance.Metadata.GetActivation() {
		return defaultSyncInterval
	}
	if !instance.Metadata.GetSyncInterval().IsValid() {
		return defaultSyncInterval
	}
	if instance.Metadata.GetSyncInterval().GetSeconds() == 0 && instance.Metadata.GetSyncInterval().GetNanos() == 0 {
		return defaultSyncInterval
	}
	return instance.Metadata.GetSyncInterval().AsDuration()
}
func getOrDefaultLastSyncTime(t *timestamppb.Timestamp) time.Time {
	if t.IsValid() {
		return t.AsTime()
	}
	return time.Unix(0, 0)
}
