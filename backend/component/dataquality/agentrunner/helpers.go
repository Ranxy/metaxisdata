package agentrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// maintainWake is how long a fully surveyed target sleeps before the next look.
	maintainWake = 24 * time.Hour
	// gapContinueWake continues a survey that recorded its per-wake limit of gaps.
	gapContinueWake = time.Minute
	// retryWake re-runs a session whose wake failed transiently.
	retryWake = 10 * time.Minute
	// maxGapInvestigations bounds the investigations one deterministic wake records.
	maxGapInvestigations = 5
	// maxDeterministicEvents bounds how many pending events one no-model wake handles;
	// the rest stay above the watermark and are picked up by the next wake.
	maxDeterministicEvents = 10
)

// ruleShapes is the plan derivation's view of the live rules: enabled rules only, with
// the column each one reads.
func ruleShapes(rules []*store.QualityRuleMessage) []dataquality.RuleShape {
	shapes := make([]dataquality.RuleShape, 0, len(rules))
	for _, rule := range rules {
		if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			continue
		}
		shapes = append(shapes, dataquality.RuleShape{
			Type: rule.RuleType, Column: dataquality.ConfigColumn(rule.Config), Enabled: true,
		})
	}
	return shapes
}

// gapLabel renders one uncovered requirement.
func gapLabel(requirement dataquality.PlanRequirement) string {
	label := propertyLabel(requirement.Property)
	if requirement.Column != "" {
		label += "(" + requirement.Column + ")"
	}
	return label
}

// propertyLabel renders one coverage property for a person and the model.
func propertyLabel(property dataquality.CoverageProperty) string {
	switch property {
	case dataquality.CoverageVolume:
		return "volume"
	case dataquality.CoverageFreshness:
		return "freshness"
	case dataquality.CoverageUniqueness:
		return "uniqueness"
	case dataquality.CoverageSchemaStability:
		return "schema_stability"
	case dataquality.CoverageNullRate:
		return "null_rate"
	case dataquality.CoverageAcceptedValues:
		return "accepted_values"
	case dataquality.CoverageFormat:
		return "format"
	case dataquality.CoverageRange:
		return "range"
	default:
		return "unknown"
	}
}

// doneSet reads the deterministic engine's completed work labels.
func doneSet(state *storepb.QualityAgentSessionEngineState) map[string]bool {
	done := make(map[string]bool, len(state.GetDone()))
	for _, label := range state.GetDone() {
		done[label] = true
	}
	return done
}

// cloneEngineState returns a copy so a wake can mutate it without touching the
// snapshot the runner passed in.
func cloneEngineState(state *storepb.QualityAgentSessionEngineState) *storepb.QualityAgentSessionEngineState {
	cloned := &storepb.QualityAgentSessionEngineState{
		Phase:         state.GetPhase(),
		BlockedRounds: state.GetBlockedRounds(),
		BlockedCode:   state.GetBlockedCode(),
	}
	cloned.Done = append(cloned.Done, state.GetDone()...)
	return cloned
}

// executionIDFromReference parses a quality execution reference, either its bare
// resource id ("qualityExecutions/7") or its full name
// ("workspaces/{workspace}/qualityExecutions/7"). A non-empty reason means the
// reference is not an execution.
func executionIDFromReference(reference string) (int64, string) {
	const segment = "qualityExecutions/"
	index := strings.LastIndex(reference, segment)
	if index == -1 {
		return 0, fmt.Sprintf("the reference %q is not a quality execution", reference)
	}
	id, err := strconv.ParseInt(reference[index+len(segment):], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Sprintf("the reference %q is not a quality execution", reference)
	}
	return id, ""
}

// investigationFingerprint identifies one investigation deterministically, so a
// retried wake cannot record it twice.
func investigationFingerprint(targetGUID, summary, reference string) string {
	sum := sha256.Sum256([]byte("agent-investigation|" + targetGUID + "|" + reference + "|" + summary))
	return "agent-inv:" + hex.EncodeToString(sum[:])
}

// marshalJSON renders a tool result.
func marshalJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// truncateRunes bounds a string at rune granularity so a multi-byte character is never
// split; protojson rejects invalid UTF-8.
func truncateRunes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// truncateBytes bounds a string at a byte budget without splitting a rune.
func truncateBytes(value string, limit int) string {
	return common.TruncateUTF8Bytes(value, limit)
}

func ptrTime(value time.Time) *time.Time { return &value }

func ptrInt32(value int32) *int32 { return &value }

// executionIDName renders an execution resource name for a tool result. The id is the
// stable part; a full name would repeat the workspace on every row.
func executionIDName(id int64) string {
	return fmt.Sprintf("qualityExecutions/%d", id)
}

// planStateLabel renders a plan state for the model.
func planStateLabel(state dataquality.PlanState) string {
	if state == dataquality.PlanPlanned {
		return "PLANNED"
	}
	return "UNPLANNED"
}

// criticalityLabel renders the target's plan criticality for the model.
func criticalityLabel(criticality dataquality.AssetCriticality) string {
	switch criticality {
	case dataquality.CriticalityCritical:
		return "CRITICAL"
	case dataquality.CriticalityImportant:
		return "IMPORTANT"
	default:
		return "EXPERIMENTAL"
	}
}

// assistantIdempotencyKey identifies one assistant message of a wake. The content and
// the tool calls are part of the key: a crash-retried turn that the model answered
// differently must append a new row instead of colliding with the half-written one.
func assistantIdempotencyKey(wake, turn int32, assistant *llm.AgentMessage) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(assistant.Content))
	for _, call := range assistant.ToolCalls {
		_, _ = hash.Write([]byte("|" + call.ID + "|" + call.GetName() + "|" + call.Function.Arguments))
	}
	return fmt.Sprintf("wake:%d:turn:%d:assistant:%x", wake, turn, hash.Sum(nil)[:8])
}

// interruptedToolResult is the synthetic result a crash left behind. It is written once
// per dangling call so the stored transcript is legal for the provider and for the
// compaction cut.
const interruptedToolResult = "the server restarted before this tool ran; its result is unknown"

// contentHash identifies one engine note for its idempotency key.
func contentHash(content string) []byte {
	sum := sha256.Sum256([]byte(content))
	return sum[:8]
}

// joinLabels renders uncovered properties for one sentence.
func joinLabels(labels []string) string {
	return strings.Join(labels, ", ")
}

// sessionEvidenceKind is the evidence kind that ties a memory to this session. It
// replaces the goal-era "GOAL" kind now that sessions are the durable unit.
const sessionEvidenceKind = "SESSION"
