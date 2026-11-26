package metrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// caseMetrics instruments the agent's work items: how many it opens, where they end up, and how
// often a write is refused before it happens.
//
// The rates a dashboard wants are ratios over these counters rather than gauges of their own:
// autonomy is RESOLVED over RESOLVED plus ESCALATED and FROZEN, and a reopen rate is a second
// transition of the same subject. Computing them here would freeze one definition of each; the
// counters let the question change without a deploy. Labels stay closed sets the code owns, which
// is what keeps a per-target breakdown out of the series count.
type caseMetrics struct {
	opened      *prometheus.CounterVec
	transitions *prometheus.CounterVec
	refused     *prometheus.CounterVec
}

func newCaseMetrics() caseMetrics {
	return caseMetrics{
		opened: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "agent",
			Name:      "case_opened_total",
			Help:      "Work items the server opened for an agent, by failure class.",
		}, []string{"class"}),
		transitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "agent",
			Name:      "case_transitions_total",
			Help:      "Work-item state changes by the state reached.",
		}, []string{"state"}),
		refused: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "agent",
			Name:      "write_refused_total",
			Help:      "Agent write verbs refused before writing, by verb, participation level and effect.",
		}, []string{"verb", "level", "effect"}),
	}
}

func (m caseMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.opened, m.transitions, m.refused}
}

// ObserveAgentCaseOpened records one work item the server opened. A nil Metrics records nothing.
func (m *Metrics) ObserveAgentCaseOpened(class string) {
	if m == nil {
		return
	}
	m.Agent.cases.opened.WithLabelValues(agentFailureClassLabel(class)).Inc()
}

// ObserveAgentCaseTransition records one work-item state change. The destination is the label,
// because the pair would double the series to say what the previous value of the same series
// already said.
func (m *Metrics) ObserveAgentCaseTransition(state string) {
	if m == nil {
		return
	}
	m.Agent.cases.transitions.WithLabelValues(agentCaseStateLabel(state)).Inc()
}

// ObserveAgentWriteRefused records a verb the delegation, the pins or the replay refused. It is the
// observation that says the prompt is inviting the model somewhere the policy will not follow.
func (m *Metrics) ObserveAgentWriteRefused(verb, level, effect string) {
	if m == nil {
		return
	}
	m.Agent.cases.refused.WithLabelValues(lowerLabel(verb), lowerLabel(level), lowerLabel(effect)).Inc()
}

// agentCaseStateLabel renders one work-item state for a metric label. The stored lifecycle names are
// the closed set, so an unknown value collapses to "other" and the label stays bounded.
func agentCaseStateLabel(state string) string {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "OPEN", "DIAGNOSED", "ACTED", "AWAITING_VERIFICATION", "RESOLVED", "FROZEN", "ESCALATED":
		return strings.ToLower(strings.TrimSpace(state))
	default:
		return "other"
	}
}

// agentFailureClassLabel renders one failure class for a metric label: the closed set the diagnosis
// produces, lowercased, and "unspecified" for the empty class of a case that has none yet.
func agentFailureClassLabel(class string) string {
	switch strings.ToUpper(strings.TrimSpace(class)) {
	case "CONFIG_VALUE_INVALID", "CONFIG_UNSUPPORTED", "STRUCTURE_DRIFT", "NO_VERDICT_EMPTY",
		"NO_VERDICT_NO_BASELINE", "NO_VERDICT_NO_VALUE", "STUCK_INFLIGHT", "SOURCE_FAULT",
		"DATA_FAILURE", "EXECUTION_FAULT":
		return strings.ToLower(strings.TrimSpace(class))
	default:
		return "unspecified"
	}
}

// lowerLabel normalizes a caller-supplied label value into the bounded lowercase vocabulary the
// case instruments use.
func lowerLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "none"
	}
	return value
}
