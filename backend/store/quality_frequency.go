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
// target's own tick stays the floor. Because a run's started_at is a few seconds
// after its cycle's due instant, the gate cannot compare against "the current due
// instant minus the tier period" directly: a daily check on a daily target would
// then skip every other day. Instead the tier is quantized to the target's own
// cadence: it covers ceil(period / cycle) cycles, and the boundary is one cycle
// short of that. So a tier equal to the target's cadence (daily on daily) never
// trips the gate, hourly on a 15-minute target skips three of four cycles, and a
// 35-minute target with an hourly tier runs every second cycle rather than every
// cycle (which would be denser than the tier). A tier denser than the target
// covers one cycle, so it never blocks.
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
	window := fmt.Sprintf(`MAKE_INTERVAL(secs => GREATEST(0,
			(GREATEST(1, CEIL((%s)::numeric / NULLIF(s.schedule_interval_seconds, 0)))::int - 1)
				* s.schedule_interval_seconds))`, tierSeconds)
	return fmt.Sprintf(`NOT EXISTS (
			SELECT 1 FROM quality_execution gate
			WHERE gate.rule_id = r.id
				AND gate.target_guid = r.target_guid
				AND gate.target_type = r.target_type
				AND gate.trigger = $%d
				AND gate.state = $%d
				AND gate.started_at > s.next_run_at - %s
		)`,
		scheduledArg, completedArg, window)
}
