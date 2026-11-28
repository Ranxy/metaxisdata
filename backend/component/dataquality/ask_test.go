package dataquality

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func askQuestionForTest() []*storepb.QualityAgentAskQuestion {
	return []*storepb.QualityAgentAskQuestion{{
		Id:       "q1",
		Question: "Keep the check?",
		Options: []*storepb.QualityAgentAskQuestionOption{
			{Label: "Keep it"},
			{Label: "Pause it"},
		},
	}}
}

func TestValidateAnswersAcceptsAnOfferedOption(t *testing.T) {
	t.Parallel()

	answers, err := ValidateAnswers(askQuestionForTest(), []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Keep it"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Keep it"}, answers[0].Selected)
}

func TestValidateAnswersRejectsALabelThatWasNotOffered(t *testing.T) {
	t.Parallel()

	_, err := ValidateAnswers(askQuestionForTest(), []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Delete it"}},
	})
	require.ErrorContains(t, err, "was not offered")
}

func TestValidateAnswersSingleSelectTakesOneChoice(t *testing.T) {
	t.Parallel()

	_, err := ValidateAnswers(askQuestionForTest(), []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Keep it", "Pause it"}},
	})
	require.ErrorContains(t, err, "accepts one choice")
}

func TestValidateAnswersCustomReplacesASingleSelectChoice(t *testing.T) {
	t.Parallel()

	answers, err := ValidateAnswers(askQuestionForTest(), []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Keep it"}, Custom: "Keep it for a week"},
	})
	require.NoError(t, err)
	require.Empty(t, answers[0].Selected)
	require.Equal(t, "Keep it for a week", answers[0].Custom)
}

func TestValidateAnswersMultiSelectKeepsCustomBesideTheSelection(t *testing.T) {
	t.Parallel()

	questions := []*storepb.QualityAgentAskQuestion{{
		Id:          "q1",
		Question:    "Which checks?",
		MultiSelect: true,
		Options:     []*storepb.QualityAgentAskQuestionOption{{Label: "Nulls"}, {Label: "Ranges"}},
	}}
	answers, err := ValidateAnswers(questions, []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Nulls"}, Custom: "and freshness"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Nulls"}, answers[0].Selected)
	require.Equal(t, "and freshness", answers[0].Custom)
}

func TestValidateAnswersRequiresEachQuestionExactlyOnce(t *testing.T) {
	t.Parallel()

	questions := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?"},
		{Id: "q2", Question: "Second?"},
	}
	_, err := ValidateAnswers(questions, []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Custom: "one"},
		{Id: "q1", Custom: "again"},
	})
	require.ErrorContains(t, err, "repeats question")

	_, err = ValidateAnswers(questions, []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Custom: "one"},
	})
	require.ErrorContains(t, err, "one item per question")
}

func TestValidateAnswersRejectsAnEmptyAnswerToTheOnlyQuestion(t *testing.T) {
	t.Parallel()

	_, err := ValidateAnswers(askQuestionForTest(), []*storepb.QualityAgentAskAnswer{
		{Id: "q1"},
	})
	require.ErrorContains(t, err, "dismiss the ask")
}

func TestBuildAnswerEnvelopeCarriesTheQuestionIDs(t *testing.T) {
	t.Parallel()

	ask := &store.QualityAgentAskMessage{
		ResourceID:  "a1",
		WorkspaceID: "ws",
		Question:    "Keep the check?",
		Questions:   askQuestionForTest(),
	}
	content, err := BuildAnswerEnvelope(ask, []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Keep it"}},
	}, []string{"workspaces/ws/qualityAgentRequests/r1"})
	require.NoError(t, err)

	envelope, ok := ParseAnswerEnvelope(content)
	require.True(t, ok)
	require.Equal(t, "workspaces/ws/qualityAgentAsks/a1", envelope.Ask)
	require.Len(t, envelope.Questions, 1)
	require.Equal(t, "q1", envelope.Questions[0].ID)
	require.Equal(t, []string{"Keep it"}, envelope.Answers[0].Selected)
	require.Equal(t, []string{"workspaces/ws/qualityAgentRequests/r1"}, envelope.Requests)
}

func TestQuestionsOfSynthesizesOneForALegacyAsk(t *testing.T) {
	t.Parallel()

	questions := QuestionsOf(&store.QualityAgentAskMessage{Question: "Legacy question?"})
	require.Len(t, questions, 1)
	require.Equal(t, "q1", questions[0].Id)
	require.Equal(t, "Legacy question?", questions[0].Question)
}

func TestAskOptionLabelsDedupesAndBounds(t *testing.T) {
	t.Parallel()

	questions := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?", Options: []*storepb.QualityAgentAskQuestionOption{
			{Label: "Keep it"},
			{Label: "Pause it"},
		}},
		{Id: "q2", Question: "Second?", Options: []*storepb.QualityAgentAskQuestionOption{
			{Label: "Keep it"},
			{Label: "Delete it"},
		}},
	}
	ask := &store.QualityAgentAskMessage{Questions: questions}
	require.Equal(t, []string{"Keep it", "Pause it", "Delete it"}, AskOptionLabels(ask))

	many := make([]*storepb.QualityAgentAskQuestionOption, 0, MaxAskNotificationOptions+1)
	for index := 0; index < MaxAskNotificationOptions+1; index++ {
		many = append(many, &storepb.QualityAgentAskQuestionOption{Label: fmt.Sprintf("option-%d", index)})
	}
	require.Len(t, AskOptionLabels(&store.QualityAgentAskMessage{Questions: []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "Which?", Options: many},
	}}), MaxAskNotificationOptions)
}

func TestBuildRequestDismissedEnvelopeNamesTheRequest(t *testing.T) {
	t.Parallel()

	ask := &store.QualityAgentAskMessage{ResourceID: "a1", WorkspaceID: "ws", Question: "Keep the check?"}
	request := &store.QualityAgentRequestMessage{ResourceID: "r1", WorkspaceID: "ws", Question: "Keep the check?"}
	content, err := BuildRequestDismissedEnvelope(ask, request)
	require.NoError(t, err)

	envelope, ok := ParseRequestDismissedEnvelope(content)
	require.True(t, ok)
	require.Equal(t, "workspaces/ws/qualityAgentAsks/a1", envelope.Ask)
	require.Equal(t, "workspaces/ws/qualityAgentRequests/r1", envelope.Request)
	require.Contains(t, envelope.Text, "dismissed")
}

func TestBuildRequestAnswerEnvelopeNamesTheRequest(t *testing.T) {
	t.Parallel()

	ask := &store.QualityAgentAskMessage{
		ResourceID:  "a1",
		WorkspaceID: "ws",
		Question:    "Keep the check?",
		Questions:   askQuestionForTest(),
	}
	request := &store.QualityAgentRequestMessage{
		ResourceID:  "r1",
		WorkspaceID: "ws",
		Question:    "Keep the check?",
	}
	content, text, err := BuildRequestAnswerEnvelope(ask, request, ask.Questions[0],
		&storepb.QualityAgentAskAnswer{Id: "q1", Selected: []string{"Keep it"}})
	require.NoError(t, err)
	require.Equal(t, "Keep it", text)

	envelope, ok := ParseRequestAnswerEnvelope(content)
	require.True(t, ok)
	require.Equal(t, "workspaces/ws/qualityAgentRequests/r1", envelope.Request)
	require.Equal(t, "Keep it", envelope.Text)
}

func TestValidateAskQuestionsRejectsDuplicateIDsAndLabels(t *testing.T) {
	t.Parallel()

	duplicateIDs := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "One?"},
		{Id: "q1", Question: "Two?"},
	}
	require.ErrorContains(t, ValidateAskQuestions(duplicateIDs), "repeated")

	duplicateLabels := []*storepb.QualityAgentAskQuestion{{
		Id:       "q1",
		Question: "Keep?",
		Options: []*storepb.QualityAgentAskQuestionOption{
			{Label: "Keep"},
			{Label: "Keep"},
		},
	}}
	require.ErrorContains(t, ValidateAskQuestions(duplicateLabels), "repeats option")
}

func TestValidateAskQuestionCoverage(t *testing.T) {
	t.Parallel()

	requests := []string{"r1", "r2"}
	matching := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?", Requests: []string{"r1"}},
		{Id: "q2", Question: "Second?", Requests: []string{"r2"}},
	}
	require.NoError(t, ValidateAskQuestionCoverage(matching, requests))

	missing := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?", Requests: []string{"r1"}},
	}
	require.ErrorContains(t, ValidateAskQuestionCoverage(missing, requests), "not decided")

	overlap := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?", Requests: []string{"r1"}},
		{Id: "q2", Question: "Second?", Requests: []string{"r1", "r2"}},
	}
	require.ErrorContains(t, ValidateAskQuestionCoverage(overlap, requests), "decided by both")

	outside := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?", Requests: []string{"r1"}},
		{Id: "q2", Question: "Second?", Requests: []string{"r3"}},
	}
	require.ErrorContains(t, ValidateAskQuestionCoverage(outside, requests), "does not cover")

	general := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?"},
		{Id: "q2", Question: "Second?"},
	}
	require.NoError(t, ValidateAskQuestionCoverage(general, nil))
	require.ErrorContains(t, ValidateAskQuestionCoverage([]*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?", Requests: []string{"r1"}},
	}, nil), "covers none")
}

func TestBuildAnswerEnvelopeCarriesQuestionRequests(t *testing.T) {
	t.Parallel()

	ask := &store.QualityAgentAskMessage{
		ResourceID:  "a1",
		WorkspaceID: "ws",
		Question:    "First?",
		Questions: []*storepb.QualityAgentAskQuestion{
			{Id: "q1", Question: "First?", Requests: []string{"workspaces/ws/qualityAgentRequests/r1"}},
			{Id: "q2", Question: "Second?", Requests: []string{"workspaces/ws/qualityAgentRequests/r2"}},
		},
	}
	content, err := BuildAnswerEnvelope(ask, []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Custom: "one"},
		{Id: "q2", Custom: "two"},
	}, []string{"workspaces/ws/qualityAgentRequests/r1", "workspaces/ws/qualityAgentRequests/r2"})
	require.NoError(t, err)
	envelope, ok := ParseAnswerEnvelope(content)
	require.True(t, ok)
	require.Len(t, envelope.Questions, 2)
	require.Equal(t, []string{"workspaces/ws/qualityAgentRequests/r1"}, envelope.Questions[0].Requests)
}

func TestBuildRequestAnswerEnvelopeOnlyTheMatchingQuestion(t *testing.T) {
	t.Parallel()

	questions := []*storepb.QualityAgentAskQuestion{
		{Id: "q1", Question: "First?"},
		{Id: "q2", Question: "Second?"},
	}
	ask := &store.QualityAgentAskMessage{ResourceID: "a1", WorkspaceID: "ws", Questions: questions}
	request := &store.QualityAgentRequestMessage{ResourceID: "r1", WorkspaceID: "ws", Question: "First?"}
	content, text, err := BuildRequestAnswerEnvelope(ask, request, questions[0],
		&storepb.QualityAgentAskAnswer{Id: "q1", Custom: "one"})
	require.NoError(t, err)
	require.Equal(t, "one", text)
	envelope, ok := ParseRequestAnswerEnvelope(content)
	require.True(t, ok)
	require.Len(t, envelope.Questions, 1)
	require.Equal(t, "q1", envelope.Questions[0].ID)
	require.Len(t, envelope.Answers, 1)
	require.Equal(t, "q1", envelope.Answers[0].ID)
}
