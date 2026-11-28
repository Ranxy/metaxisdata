package agentrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// A check or an execution is not a work item. The refusal has to name the issue ledger
// and must happen before the request row is raised, or the transcript shows a request
// for a case that never existed.
func TestCaseVerbsRejectACheckReferenceBeforeRaisingARequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
	}{
		{"control_check", `{"case":"qualityRules/r1","action":"PAUSE"}`},
		{"propose_revision", `{"case":"qualityRules/r1","field":"range.min","value":"1","rationale":"x"}`},
		{"escalate_case", `{"case":"qualityExecutions/9","reason":"x"}`},
		{"run_check", `{"case":"qualityIssues/i1"}`},
		{"repair_check", `{"case":"qualityRules/r1"}`},
		{"control_check", `{"case":"qualityRules","action":"PAUSE"}`},
		{"control_check", `{"case":"workspaces/ws/qualityRules/r1","action":"PAUSE"}`},
		{"escalate_case", `{"case":"workspaces/ws/qualityExecutions/9","reason":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stores, engine, session := newOrchestrationHarness(t)
			_, err := engine.executeTool(context.Background(), session, stores.agent, tc.name, tc.args, &wakeControl{})
			require.ErrorContains(t, err, "is not a work item")
			require.Empty(t, stores.requests)
		})
	}
}

func TestSendLeadQuestionRejectsAnIssueReference(t *testing.T) {
	t.Parallel()

	stores, engine, session := newOrchestrationHarness(t)
	_, err := engine.sendLeadMessage(context.Background(), session,
		`{"intent":"QUESTION","message":"What should I do?","case":"qualityIssues/i1"}`)
	require.ErrorContains(t, err, "is not a work item")
	require.Empty(t, stores.requests)
}

// An unknown bare id used to read as "does not belong to this target"; that told the
// model to retry instead of explaining the id space.
func TestUnknownCaseExplainsTheIdSpace(t *testing.T) {
	t.Parallel()

	stores, engine, session := newOrchestrationHarness(t)
	_, err := engine.executeTool(context.Background(), session, stores.agent, "escalate_case",
		`{"case":"missing","reason":"x"}`, &wakeControl{})
	require.ErrorContains(t, err, "not found: these verbs act on a work item")
	require.Empty(t, stores.requests)
}
