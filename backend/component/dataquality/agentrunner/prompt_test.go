package agentrunner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The adoption gate reaches the model as configuration, not as folklore: the
// eligible types, the confidence floor, the daily ceiling and the critical switch
// are all rendered, and so is the consequence of drafting outside them, which a
// wake otherwise learns only by watching an adoption be skipped.
func TestAdoptionPolicySectionRendersTheConfiguredGate(t *testing.T) {
	t.Parallel()

	agent := capabilityTestAgent(agentcapability.Suggest, agentcapability.Adopt)
	section := adoptionPolicySection(agent, conversationProduct)
	require.Contains(t, section, "mode ACTIVE")
	require.Contains(t, section, "eligible check types: NULL_PERCENT")
	require.Contains(t, section, "minimum model confidence: 0.70")
	require.Contains(t, section, "daily ceiling: 20")
	require.Contains(t, section, "critical assets: not allowed")
	require.Contains(t, section, "waits for a person")

	// The defaults are stated as numbers, because the model reasons about them.
	agent.AdoptionRuleTypes = nil
	agent.AdoptionConfidenceThreshold = 0
	agent.AdoptionMaxPerDay = 0
	agent.AdoptionAllowCritical = true
	section = adoptionPolicySection(agent, conversationProduct)
	require.Contains(t, section, "the default low-risk check types")
	require.Contains(t, section, "minimum model confidence: 0.70")
	require.Contains(t, section, "daily ceiling: 10")
	require.Contains(t, section, "critical assets: allowed")

	// An agent that cannot adopt is told so instead of being shown a policy it
	// cannot use.
	require.Contains(t, adoptionPolicySection(capabilityTestAgent(agentcapability.Suggest), conversationProduct), "Adoption is unavailable")
}

// A monitoring wake must be told what a blocker is not. It once read "escalate a blocker instead of
// guessing", judged a persistent FAIL a blocker, and invented a case id out of the execution it had
// — while the issue ledger had already reported that failure to the owner.
func TestMonitorPromptSaysAFailingCheckIsNotAWorkItem(t *testing.T) {
	t.Parallel()

	profile, ok := profileFor(agentcapability.Patrol)
	require.True(t, ok, "the patrol capability has a profile")
	sections := profile.Prompt(monitorSpec())
	require.Len(t, sections, 1)
	require.Contains(t, sections[0].Text, "A failing check is a finding, not a work item")
	require.Contains(t, sections[0].Text, "issue ledger")
	require.NotContains(t, sections[0].Text, "Escalate a blocker that needs a person instead of guessing",
		"an unqualified escalation invitation is what produced a fabricated case id")
}

// A fact is durable knowledge, not a wake log. The dev agent stored 188 "facts" averaging 500
// characters, one per wake, because both the tool and the wake wording invited a summary of the
// reasoning; the guidance now says what a fact is, without constraining what the tool accepts.
func TestFactRecordingIsGuidedNotConstrained(t *testing.T) {
	t.Parallel()

	var recordSpec toolSpec
	for _, spec := range triageSpec().Tools {
		if spec.name == "record_asset_fact" {
			recordSpec = spec
		}
	}
	require.Equal(t, "record_asset_fact", recordSpec.name, "the fact tool is on the triage product")
	require.Contains(t, recordSpec.description, "durable fact")
	require.Contains(t, recordSpec.description, "reasoning in your message")
	require.Contains(t, recordSpec.description, "Record nothing when a wake concluded nothing new")

	profile, ok := profileFor(agentcapability.Patrol)
	require.True(t, ok)
	sections := profile.Prompt(monitorSpec())
	require.Contains(t, sections[0].Text, "one durable sentence per fact")
}

// The wake prompt must show the key the tool accepts, not the internal dedupe key, and
// must not invite the model to reuse a question_key for a proposal or decision.
func TestOpenRequestBlockNamesTheStoredQuestionKey(t *testing.T) {
	t.Parallel()

	requests := []*store.QualityAgentRequestMessage{
		{
			ResourceID: "r1", Kind: store.QualityAgentRequestKindQuestion,
			Status: store.QualityAgentRequestStatusOpen, QuestionKey: "timeout",
			CaseResourceID: "c1", Question: "Raise the timeout?",
		},
		{
			ResourceID: "r2", Kind: store.QualityAgentRequestKindQuestion,
			Status: store.QualityAgentRequestStatusForwarded, CaseResourceID: "c1",
			Question: "Is the case settled?",
		},
		{
			ResourceID: "r3", Kind: store.QualityAgentRequestKindProposal,
			Status: store.QualityAgentRequestStatusOpen, Question: "Approve the change?",
		},
	}

	block := openRequestBlock(requests)
	require.Contains(t, block, "question_key=timeout (case c1)")
	require.Contains(t, block, "no question_key (case c1; omit question_key to retry)")
	require.Contains(t, block, "[FORWARDED]")
	require.NotContains(t, block, "r3", "a proposal does not accept a question_key")
}

// A no-case question must not be retried with a case: case is part of both the dedupe
// key and the fingerprint, so adding one would create a second request.
func TestOpenRequestBlockMarksQuestionsWithoutACase(t *testing.T) {
	t.Parallel()

	requests := []*store.QualityAgentRequestMessage{
		{
			ResourceID: "r1", Kind: store.QualityAgentRequestKindQuestion,
			Status: store.QualityAgentRequestStatusOpen, QuestionKey: "timeout",
			Question: "Raise the timeout?",
		},
		{
			ResourceID: "r2", Kind: store.QualityAgentRequestKindQuestion,
			Status:   store.QualityAgentRequestStatusOpen,
			Question: "Is the target healthy?",
		},
	}

	block := openRequestBlock(requests)
	require.Contains(t, block, "question_key=timeout (no case)")
	require.Contains(t, block, "no question_key (no case; omit question_key and the case, keep the wording)")
}

// An owner answer must reach the model with its ask and question ids. The old wake
// saw only the sentence and guessed from message order when two asks were open.
func TestPendingOwnerAnswerBlockNamesTheAskAndQuestion(t *testing.T) {
	t.Parallel()

	ask := &store.QualityAgentAskMessage{
		ResourceID:  "a1",
		WorkspaceID: "ws",
		Question:    "Keep the check?",
		Questions: []*storepb.QualityAgentAskQuestion{{
			Id:       "q1",
			Question: "Keep the check?",
			Options:  []*storepb.QualityAgentAskQuestionOption{{Label: "Keep it"}},
			Requests: []string{"workspaces/ws/qualityAgentRequests/r1"},
		}},
	}
	content, err := dataquality.BuildAnswerEnvelope(ask, []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Keep it"}},
	}, []string{"workspaces/ws/qualityAgentRequests/r1"})
	require.NoError(t, err)

	block := pendingOwnerAnswerBlock([]*store.QualityAgentTranscriptMessage{{
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Intent:  storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		ReplyTo: "workspaces/ws/qualityAgentAsks/a1",
		Content: content,
	}}, 5)
	require.Contains(t, block, "ask workspaces/ws/qualityAgentAsks/a1")
	require.Contains(t, block, "question q1")
	require.Contains(t, block, "Keep it")
	require.Contains(t, block, "requests: workspaces/ws/qualityAgentRequests/r1")
	require.Contains(t, block, "delivered to: workspaces/ws/qualityAgentRequests/r1")
}

// A subagent's answer must name the request it settles; otherwise two open questions
// are indistinguishable in its inbox.
// A dismissed request is closed without an answer; the subagent must see that as a
// dismissal, not as an unreadable instruction that leaves it waiting.
func TestPendingAgentMessagesBlockRendersTheRequestDismissal(t *testing.T) {
	t.Parallel()

	ask := &store.QualityAgentAskMessage{ResourceID: "a1", WorkspaceID: "ws", Question: "Keep the check?"}
	request := &store.QualityAgentRequestMessage{ResourceID: "r1", WorkspaceID: "ws", Question: "Keep the check?"}
	content, err := dataquality.BuildRequestDismissedEnvelope(ask, request)
	require.NoError(t, err)

	block := pendingAgentMessagesBlock([]*store.QualityAgentTranscriptMessage{{
		Origin:     storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Intent:     storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_DISMISS,
		SenderName: "lead",
		Content:    content,
	}}, 5)
	require.Contains(t, block, "[DISMISSED] workspaces/ws/qualityAgentRequests/r1")
	require.Contains(t, block, "do not wait for an answer")
}

func TestPendingAgentMessagesBlockRendersTheRequestAnswer(t *testing.T) {
	t.Parallel()

	ask := &store.QualityAgentAskMessage{
		ResourceID:  "a1",
		WorkspaceID: "ws",
		Question:    "Keep the check?",
		Questions: []*storepb.QualityAgentAskQuestion{{
			Id:       "q1",
			Question: "Keep the check?",
			Options:  []*storepb.QualityAgentAskQuestionOption{{Label: "Keep it"}},
		}},
	}
	request := &store.QualityAgentRequestMessage{
		ResourceID:  "r1",
		WorkspaceID: "ws",
		Question:    "Keep the check?",
	}
	content, _, err := dataquality.BuildRequestAnswerEnvelope(ask, request, ask.Questions[0],
		&storepb.QualityAgentAskAnswer{Id: "q1", Selected: []string{"Keep it"}})
	require.NoError(t, err)

	block := pendingAgentMessagesBlock([]*store.QualityAgentTranscriptMessage{{
		Origin:     storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Intent:     storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		SenderName: "lead",
		Content:    content,
	}}, 5)
	require.Contains(t, block, "workspaces/ws/qualityAgentRequests/r1")
	require.Contains(t, block, "Keep it")
}
