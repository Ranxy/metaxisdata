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
// current target cycle: no completed scheduled execution may have its due slot
// less than one tier period before the current slot. Comparing slots rather than
// actual start times is what makes a tier equal to the target's own cadence exact:
// a run starts on the scheduler tick after its slot, so a start-time comparison
// would put the previous run just inside "one period ago" and a daily check on a
// daily target would skip every other day. Manual executions carry no slot and
// therefore never satisfy the subquery.
//
// The period is zero for UNSPECIFIED (follow the target), so the gate never blocks
// a check that runs every cycle; the current cycle's own completed attempt is
// handled by the attempts tally of the calling query.
//
// completedArg and scheduledArg are the 1-based placeholder numbers the calling
// query uses for the COMPLETED state and the SCHEDULED trigger, so the due-rules
// read and the pending-count read share one predicate instead of drifting.
func qualityFrequencyGateSQL(completedArg, scheduledArg int) string {
	// The tier period is rendered from the same helper the callers and tests read,
	// so the SQL mapping cannot drift from the Go one.
	tierSeconds := fmt.Sprintf(`CASE r.frequency_tier
				WHEN %d THEN %d
				WHEN %d THEN %d
				WHEN %d THEN %d
				ELSE 0 END`,
		storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_HOURLY,
		int(qualityFrequencyTierPeriod(storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_HOURLY)/time.Second),
		storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_DAILY,
		int(qualityFrequencyTierPeriod(storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_DAILY)/time.Second),
		storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_WEEKLY,
		int(qualityFrequencyTierPeriod(storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_WEEKLY)/time.Second),
	)
	return fmt.Sprintf(`NOT EXISTS (
			SELECT 1 FROM quality_execution gate
			WHERE gate.rule_id = r.id
				AND gate.target_guid = r.target_guid
				AND gate.target_type = r.target_type
				AND gate.trigger = $%d
				AND gate.state = $%d
				AND gate.scheduled_at > s.next_run_at - MAKE_INTERVAL(secs => %s)
		)`,
		scheduledArg, completedArg, tierSeconds)
}
