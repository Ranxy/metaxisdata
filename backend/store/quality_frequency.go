package store

import (
	"fmt"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The periods a check's frequency tier asks for. UNSPECIFIED follows the target.
const (
	hourlyFrequencyTier = time.Hour
	dailyFrequencyTier  = 24 * time.Hour
	weeklyFrequencyTier = 7 * 24 * time.Hour
)

// qualityFrequencyTierPeriod is the spacing a tier asks for; zero follows the
// target and therefore runs in every cycle.
func qualityFrequencyTierPeriod(tier storepb.QualityFrequencyTier) time.Duration {
	switch tier {
	case storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_UNSPECIFIED:
		return 0
	case storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_HOURLY:
		return hourlyFrequencyTier
	case storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_DAILY:
		return dailyFrequencyTier
	case storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_WEEKLY:
		return weeklyFrequencyTier
	default:
		return 0
	}
}

// qualityFrequencyGateSQL renders the predicate a rule must satisfy to run in the
// current target cycle: no completed scheduled execution may have started after
// the gate boundary. A tiered check runs at most once per its period, and the
// target's own tick stays the floor: the boundary is the tier period minus one
// target cycle. A check that ran in the previous cycle is then exactly one cycle
// older than the current due instant, so a tier equal to the target's cadence (a
// daily check on a daily target) never trips the gate, while a coarser tier skips
// the cycles in between. A tier denser than the target yields a negative window,
// which floors at zero, so it never blocks.
//
// completedArg and scheduledArg are the 1-based placeholder numbers the calling
// query uses for the COMPLETED state and the SCHEDULED trigger, so the due-rules
// read and the pending-count read share one predicate instead of drifting.
func qualityFrequencyGateSQL(completedArg, scheduledArg int) string {
	tierSeconds := fmt.Sprintf(`CASE r.frequency_tier
				WHEN %d THEN %d
				WHEN %d THEN %d
				WHEN %d THEN %d
				ELSE 0 END`,
		storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_HOURLY, int(hourlyFrequencyTier/time.Second),
		storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_DAILY, int(dailyFrequencyTier/time.Second),
		storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_WEEKLY, int(weeklyFrequencyTier/time.Second),
	)
	return fmt.Sprintf(`NOT EXISTS (
			SELECT 1 FROM quality_execution gate
			WHERE gate.rule_id = r.id
				AND gate.target_guid = r.target_guid
				AND gate.target_type = r.target_type
				AND gate.trigger = $%d
				AND gate.state = $%d
				AND gate.started_at > s.next_run_at - MAKE_INTERVAL(secs => GREATEST(0, (%s) - s.schedule_interval_seconds))
		)`,
		scheduledArg, completedArg, tierSeconds)
}
