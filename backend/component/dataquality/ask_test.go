package dataquality

import (
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
	content, text, err := BuildRequestAnswerEnvelope(ask, request, []*storepb.QualityAgentAskAnswer{
		{Id: "q1", Selected: []string{"Keep it"}},
	})
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
