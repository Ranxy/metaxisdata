package llm

import (
	"strings"

	"github.com/pkg/errors"
)

// Model capacities are per-model properties: the context window is a capability
// of the model, and the output budget is the ceiling one call requests from it.
// The bounds keep an accidental value from handing an unbounded generation or an
// unusable window to a background job, while the resolution rules keep a stored
// out-of-band value harmless.
const (
	// DefaultMaxOutputTokens is the budget a model that sets none runs with.
	DefaultMaxOutputTokens = 32768
	// MinMaxOutputTokens is the smallest explicit budget a model may set. A
	// value below it would truncate almost every structured answer.
	MinMaxOutputTokens = 256
	// MaxMaxOutputTokens is the largest explicit budget a model may set.
	MaxMaxOutputTokens = 131072

	// DefaultContextWindow is the window a model that sets none is assumed to
	// accept. It matches the historical compaction constant so existing
	// deployments do not change behavior on upgrade.
	DefaultContextWindow = 262144
	// MinContextWindow is the positive-integer floor; there is no larger product
	// minimum (DSH validates only positivity).
	MinContextWindow = 1
	// MaxContextWindow is a sanity ceiling for the int32 config, not a model
	// claim.
	MaxContextWindow = 10_000_000

	// CompactionThresholdRatio is the window fraction at which the quality agent
	// compacts, mirroring DSH's compaction-basic default.
	CompactionThresholdRatio = 0.8
	// CompactionHeadroomCap bounds the extra pressure headroom. DSH uses a fixed
	// 65,536 tuned for 1M windows; we cap it at a fraction of the window instead
	// because a 128K model has no per-route override in this product.
	CompactionHeadroomCap = 65536
)

// ModelLimits is one model entry's resolved capacities.
type ModelLimits struct {
	ContextWindow   int
	MaxOutputTokens int
}

// NormalizeModelLimits resolves one model entry's capacities: zero means the
// server default and out-of-range values clamp. The output budget is kept below
// the window when stored data is out of band, but never below the minimum.
func NormalizeModelLimits(contextWindow, maxOutputTokens int32) ModelLimits {
	ctx := int(contextWindow)
	switch {
	case ctx <= 0:
		ctx = DefaultContextWindow
	case ctx > MaxContextWindow:
		ctx = MaxContextWindow
	default:
	}

	out := int(maxOutputTokens)
	switch {
	case out <= 0:
		out = DefaultMaxOutputTokens
	case out < MinMaxOutputTokens:
		out = MinMaxOutputTokens
	case out > MaxMaxOutputTokens:
		out = MaxMaxOutputTokens
	default:
	}

	if out >= ctx {
		out = ctx - 1
		if out < MinMaxOutputTokens {
			out = MinMaxOutputTokens
		}
	}
	return ModelLimits{ContextWindow: ctx, MaxOutputTokens: out}
}

// ValidateModelLimits checks a model create/update value. Zero means the server
// default for either field. The resolved pair must leave a positive message
// budget (DSH's W - O > 0); there is no fixed output reserve.
func ValidateModelLimits(contextWindow, maxOutputTokens int32) error {
	if contextWindow != 0 && (contextWindow < MinContextWindow || contextWindow > MaxContextWindow) {
		return errors.Errorf("context_window must be 0 (server default %d) or between %d and %d",
			DefaultContextWindow, MinContextWindow, MaxContextWindow)
	}
	if maxOutputTokens != 0 && (maxOutputTokens < MinMaxOutputTokens || maxOutputTokens > MaxMaxOutputTokens) {
		return errors.Errorf("max_output_tokens must be 0 (server default %d) or between %d and %d",
			DefaultMaxOutputTokens, MinMaxOutputTokens, MaxMaxOutputTokens)
	}

	ctx := int(contextWindow)
	if ctx == 0 {
		ctx = DefaultContextWindow
	}
	out := int(maxOutputTokens)
	if out == 0 {
		out = DefaultMaxOutputTokens
	}
	if out >= ctx {
		return errors.Errorf("max_output_tokens (%d) must be smaller than context_window (%d); set a smaller output budget or a larger window",
			out, ctx)
	}
	return nil
}

// CompactionTrigger is the quality agent's pressure threshold for one model,
// mirroring DSH's compaction-basic policy: floor(min(W*0.8, W-O-B)) with the
// headroom capped by the window. Zero means the pair cannot compact safely; a
// quality agent must not be pinned to it.
func CompactionTrigger(contextWindow, maxOutputTokens int) int {
	limits := NormalizeModelLimits(int32(contextWindow), int32(maxOutputTokens))
	ctx := limits.ContextWindow
	headroom := CompactionHeadroomCap
	if eighth := ctx / 8; eighth < headroom {
		headroom = eighth
	}
	pressure := ctx - limits.MaxOutputTokens - headroom
	if pressure <= 0 {
		return 0
	}
	threshold := int(float64(ctx) * CompactionThresholdRatio)
	if pressure < threshold {
		threshold = pressure
	}
	return threshold
}

// ValidateModelNames checks a profile's model list: every name is non-empty and
// unique after trimming. A duplicate would surface twice in the agent picker and
// make FindModelConfig ambiguous.
func ValidateModelNames(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for index, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			return errors.Errorf("models[%d]: a model name is required", index)
		}
		if _, ok := seen[name]; ok {
			return errors.Errorf("models[%d]: duplicate model name %q", index, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}
