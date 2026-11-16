package transcript

// The compaction plan keeps a share of the trigger budget verbatim as the recent
// tail and summarizes the older region.
const (
	retainRatio        = 0.16
	forcedRetainRatio  = 0.08
	forcedRetainTokens = 4_096
	minRegionMessages  = 4
)

// Plan is one legal compaction: the region to summarize and the sequence number the
// checkpoint covers.
type Plan struct {
	// Region is the contiguous span of entries to summarize.
	Region []Entry
	// ThroughSeq is the sequence of the last summarized entry: a product stores it
	// as the checkpoint's watermark.
	ThroughSeq int64
}

// PlanCompaction chooses what to summarize when the conversation is over budget.
// It reports false when there is nothing worth compacting: the pruned conversation
// fits, the region is too small, or no legal cut exists.
func PlanCompaction(entries []Entry, trigger int, forced bool) (Plan, bool) {
	retain := int(float64(trigger) * retainRatio)
	if forced {
		// An overflow is an emergency: keep only a small recent tail, not a share of
		// the trigger (which may be far above the conversation that just overflowed).
		retain = int(float64(trigger) * forcedRetainRatio)
		if retain > forcedRetainTokens {
			retain = forcedRetainTokens
		}
	}
	cut := BalancedCut(entries, RetainedCut(entries, retain))
	start := 0
	for start < cut && entries[start].Role == RoleSystem {
		start++
	}
	if cut-start < minRegionMessages {
		return Plan{}, false
	}
	region := entries[start:cut]
	return Plan{Region: region, ThroughSeq: region[len(region)-1].Seq}, true
}

// RetainedCut returns the index of the first entry that stays verbatim: the tail
// whose estimated tokens reach the retention budget.
func RetainedCut(entries []Entry, retain int) int {
	total := 0
	for index := len(entries) - 1; index >= 0; index-- {
		total += MessageTokens(entries[index])
		if total >= retain {
			return index
		}
	}
	return 0
}

// BalancedCut moves a cut earlier until it does not split an assistant tool call
// from its results. A user or system entry is a turn boundary and flushes a pending
// call, so one crash-dangling call cannot make every later cut look unbalanced.
func BalancedCut(entries []Entry, cut int) int {
	if cut <= 0 || cut > len(entries) {
		return 0
	}
	pending := make([]int, len(entries)+1)
	for index, entry := range entries {
		pending[index+1] = pending[index]
		switch entry.Role {
		case RoleAssistant:
			pending[index+1] += len(entry.ToolCalls)
		case RoleTool:
			if pending[index+1] > 0 {
				pending[index+1]--
			}
		case RoleUser, RoleSystem, RoleCompaction:
			pending[index+1] = 0
		case RoleEngine:
		default:
		}
	}
	for index := cut; index > 0; index-- {
		if pending[index] == 0 {
			return index
		}
	}
	return 0
}
