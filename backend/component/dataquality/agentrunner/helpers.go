package agentrunner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// maintainWake is how long a fully surveyed target sleeps before the next look.
	maintainWake = 24 * time.Hour
	// retryWake re-runs a session whose wake failed transiently.
	retryWake = 10 * time.Minute
	// maxDeterministicEvents bounds how many pending events one no-model wake handles;
	// the rest stay above the watermark and are picked up by the next wake.
	maxDeterministicEvents = 10
)

// isLeadSession reports whether a session is the owner-facing coordinator. The
// Kind fallback keeps older tests and rows readable while Role is rolled out.
func isLeadSession(session *store.QualityAgentSessionMessage) bool {
	return session != nil && (session.Role == storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_LEAD || session.Kind == 2)
}

// isSubagentSession reports whether a session owns exactly one target.
func isSubagentSession(session *store.QualityAgentSessionMessage) bool {
	return session != nil && !isLeadSession(session) && session.TargetGUID != ""
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

func ptrTime(value time.Time) *time.Time { return &value }

// assistantIdempotencyKey identifies one assistant message of a wake. The content and
// the tool calls are part of the key: a crash-retried turn that the model answered
// differently must append a new row instead of colliding with the half-written one.
func assistantIdempotencyKey(wake, turn int32, assistant *agent.Message) string {
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
