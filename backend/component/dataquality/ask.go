package dataquality

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// MaxAskQuestions bounds one ask. The first version asks one decision, so the
	// limit is a protocol guard rather than a UI page size.
	MaxAskQuestions = 5
	// MaxAskOptions bounds one question's choices.
	MaxAskOptions = 10
	// MaxAskQuestionBytes bounds one question's prose.
	MaxAskQuestionBytes = 4 << 10
	// MaxAskOptionLabelBytes bounds one option label. The label is the answer value.
	MaxAskOptionLabelBytes = 200
	// MaxAskOptionDescriptionBytes bounds an option's supporting text.
	MaxAskOptionDescriptionBytes = 500
	// MaxAskAnswerBytes bounds a free-form answer.
	MaxAskAnswerBytes = 8 << 10
)

// AnswerOption is one selectable answer as the model and the browser see it.
type AnswerOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AnswerQuestion is one decision in the answer envelope.
type AnswerQuestion struct {
	ID          string         `json:"id"`
	Header      string         `json:"header,omitempty"`
	Question    string         `json:"question"`
	Detail      string         `json:"detail,omitempty"`
	Options     []AnswerOption `json:"options,omitempty"`
	MultiSelect bool           `json:"multiSelect,omitempty"`
	// Requests are the request resource names this question decides.
	Requests []string `json:"requests,omitempty"`
}

// AnswerItem is the owner's answer to one question.
type AnswerItem struct {
	ID       string   `json:"id"`
	Selected []string `json:"selected"`
	Custom   string   `json:"custom,omitempty"`
}

// AnswerEnvelope is the model-facing content of an owner's answer in the lead
// conversation. It carries the questions too, so a compacted transcript still
// explains what was answered.
type AnswerEnvelope struct {
	Kind      string           `json:"kind"`
	Ask       string           `json:"ask"`
	Questions []AnswerQuestion `json:"questions"`
	Answers   []AnswerItem     `json:"answers"`
	Requests  []string         `json:"requests,omitempty"`
}

// RequestAnswerEnvelope is the model-facing content delivered to the subagent
// whose request the owner answered.
type RequestAnswerEnvelope struct {
	Kind      string           `json:"kind"`
	Ask       string           `json:"ask"`
	Request   string           `json:"request"`
	Questions []AnswerQuestion `json:"questions"`
	Answers   []AnswerItem     `json:"answers"`
	Text      string           `json:"text"`
}

// QuestionsOf returns the ask's structured questions, synthesizing one free-text
// question for a legacy ask that predates the structured form. Every reader uses
// this so a legacy ask and a new ask answer through the same protocol.
func QuestionsOf(ask *store.QualityAgentAskMessage) []*storepb.QualityAgentAskQuestion {
	if ask == nil {
		return nil
	}
	if len(ask.Questions) > 0 {
		return ask.Questions
	}
	return []*storepb.QualityAgentAskQuestion{{Id: "q1", Question: ask.Question}}
}

// AskName renders an ask's API resource name.
func AskName(ask *store.QualityAgentAskMessage) string {
	if ask == nil {
		return ""
	}
	return common.FormatWorkspace(ask.WorkspaceID) + "/qualityAgentAsks/" + ask.ResourceID
}

// RequestName renders a request's API resource name.
func RequestName(request *store.QualityAgentRequestMessage) string {
	if request == nil {
		return ""
	}
	return common.FormatWorkspace(request.WorkspaceID) + "/qualityAgentRequests/" + request.ResourceID
}

// ValidateAskQuestions checks the shape of a question batch before it is stored.
// It rejects what a UI cannot render or an answer cannot address: duplicate ids,
// empty labels, duplicate labels, and oversized prose.
func ValidateAskQuestions(questions []*storepb.QualityAgentAskQuestion) error {
	if len(questions) == 0 {
		return common.Errorf(common.Invalid, "an ask needs at least one question")
	}
	if len(questions) > MaxAskQuestions {
		return common.Errorf(common.Invalid, "an ask holds at most %d questions", MaxAskQuestions)
	}
	ids := make(map[string]bool, len(questions))
	for _, question := range questions {
		if question == nil {
			return common.Errorf(common.Invalid, "an ask question is empty")
		}
		id := strings.TrimSpace(question.Id)
		if id == "" {
			return common.Errorf(common.Invalid, "an ask question needs an id")
		}
		if ids[id] {
			return common.Errorf(common.Invalid, "question id %q is repeated", id)
		}
		ids[id] = true
		if strings.TrimSpace(question.Question) == "" {
			return common.Errorf(common.Invalid, "question %q needs text", id)
		}
		if len(question.Question) > MaxAskQuestionBytes {
			return common.Errorf(common.Invalid, "question %q is too long (max %d bytes)", id, MaxAskQuestionBytes)
		}
		if len(question.Options) > MaxAskOptions {
			return common.Errorf(common.Invalid, "question %q holds at most %d options", id, MaxAskOptions)
		}
		labels := make(map[string]bool, len(question.Options))
		for _, option := range question.Options {
			if option == nil {
				return common.Errorf(common.Invalid, "question %q has an empty option", id)
			}
			label := strings.TrimSpace(option.Label)
			if label == "" {
				return common.Errorf(common.Invalid, "question %q has an option without a label", id)
			}
			if len(option.Label) > MaxAskOptionLabelBytes {
				return common.Errorf(common.Invalid, "option %q is too long (max %d bytes)", label, MaxAskOptionLabelBytes)
			}
			if len(option.Description) > MaxAskOptionDescriptionBytes {
				return common.Errorf(common.Invalid, "option %q description is too long (max %d bytes)", label, MaxAskOptionDescriptionBytes)
			}
			if labels[label] {
				return common.Errorf(common.Invalid, "question %q repeats option %q", id, label)
			}
			labels[label] = true
		}
		seenRequests := make(map[string]bool, len(question.Requests))
		for _, request := range question.Requests {
			name := strings.TrimSpace(request)
			if name == "" {
				return common.Errorf(common.Invalid, "question %q names an empty request", id)
			}
			if seenRequests[name] {
				return common.Errorf(common.Invalid, "question %q repeats request %q", id, name)
			}
			seenRequests[name] = true
		}
		for _, value := range append(append([]string{question.Header, question.Detail, question.Question}, labelsOf(question)...), question.Requests...) {
			if containsNUL(value) {
				return common.Errorf(common.Invalid, "question %q contains a NUL character", id)
			}
		}
	}
	return nil
}

// ValidateAskQuestionCoverage checks the per-question request subsets of a batch:
// every ask request is decided by exactly one question, and a question never ranges
// outside the ask's request set. A single question with no requests means "all",
// which the caller resolves before calling; a legacy ask is not checked here.
func ValidateAskQuestionCoverage(questions []*storepb.QualityAgentAskQuestion, requestNames []string) error {
	ask := make(map[string]bool, len(requestNames))
	for _, name := range requestNames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			ask[trimmed] = true
		}
	}
	if len(ask) == 0 {
		for _, question := range questions {
			if len(question.GetRequests()) > 0 {
				return common.Errorf(common.Invalid, "question %q names a request, but the ask covers none", question.GetId())
			}
		}
		return nil
	}
	owners := make(map[string]string, len(ask))
	for _, question := range questions {
		if len(question.GetRequests()) == 0 {
			return common.Errorf(common.Invalid, "question %q must name the requests it decides", question.GetId())
		}
		for _, name := range question.GetRequests() {
			trimmed := strings.TrimSpace(name)
			if !ask[trimmed] {
				return common.Errorf(common.Invalid, "question %q names request %q, which the ask does not cover", question.GetId(), trimmed)
			}
			if owner, ok := owners[trimmed]; ok {
				return common.Errorf(common.Invalid, "request %q is decided by both question %q and question %q", trimmed, owner, question.GetId())
			}
			owners[trimmed] = question.GetId()
		}
	}
	for name := range ask {
		if _, ok := owners[name]; !ok {
			return common.Errorf(common.Invalid, "request %q is not decided by any question", name)
		}
	}
	return nil
}

func labelsOf(question *storepb.QualityAgentAskQuestion) []string {
	labels := make([]string, 0, len(question.Options))
	for _, option := range question.Options {
		labels = append(labels, option.Label)
	}
	return labels
}

// NormalizeAnswers trims the answer's text and applies the single-select rule:
// a custom answer replaces the selected choice. A multi-select answer keeps both.
func NormalizeAnswers(questions []*storepb.QualityAgentAskQuestion, answers []*storepb.QualityAgentAskAnswer) []*storepb.QualityAgentAskAnswer {
	byID := make(map[string]*storepb.QualityAgentAskQuestion, len(questions))
	for _, question := range questions {
		byID[strings.TrimSpace(question.Id)] = question
	}
	normalized := make([]*storepb.QualityAgentAskAnswer, 0, len(answers))
	for _, answer := range answers {
		if answer == nil {
			continue
		}
		next := &storepb.QualityAgentAskAnswer{
			Id:       strings.TrimSpace(answer.Id),
			Selected: append([]string(nil), answer.Selected...),
			Custom:   strings.TrimSpace(answer.Custom),
		}
		if question := byID[next.Id]; question != nil && !question.MultiSelect && next.Custom != "" {
			next.Selected = nil
		}
		normalized = append(normalized, next)
	}
	return normalized
}

// ValidateAnswers checks one answer per question, that every selected label was
// offered, and that a single-select question names at most one choice. It returns
// the normalized batch so the caller stores exactly what it validated.
func ValidateAnswers(questions []*storepb.QualityAgentAskQuestion, answers []*storepb.QualityAgentAskAnswer) ([]*storepb.QualityAgentAskAnswer, error) {
	normalized := NormalizeAnswers(questions, answers)
	if len(normalized) != len(questions) {
		return nil, common.Errorf(common.Invalid, "the answer needs one item per question (%d wanted, %d given)", len(questions), len(normalized))
	}
	byID := make(map[string]*storepb.QualityAgentAskQuestion, len(questions))
	for _, question := range questions {
		byID[strings.TrimSpace(question.Id)] = question
	}
	seen := make(map[string]bool, len(normalized))
	for _, answer := range normalized {
		question := byID[answer.Id]
		if question == nil {
			return nil, common.Errorf(common.Invalid, "the answer names unknown question %q", answer.Id)
		}
		if seen[answer.Id] {
			return nil, common.Errorf(common.Invalid, "the answer repeats question %q", answer.Id)
		}
		seen[answer.Id] = true
		if len(answer.Custom) > MaxAskAnswerBytes {
			return nil, common.Errorf(common.Invalid, "the answer to %q is too long (max %d bytes)", answer.Id, MaxAskAnswerBytes)
		}
		if containsNUL(answer.Custom) {
			return nil, common.Errorf(common.Invalid, "the answer to %q contains a NUL character", answer.Id)
		}
		if !question.MultiSelect && len(answer.Selected) > 1 {
			return nil, common.Errorf(common.Invalid, "question %q accepts one choice", answer.Id)
		}
		offered := make(map[string]bool, len(question.Options))
		for _, option := range question.Options {
			offered[option.Label] = true
		}
		selected := make(map[string]bool, len(answer.Selected))
		for _, label := range answer.Selected {
			if !offered[label] {
				return nil, common.Errorf(common.Invalid, "option %q was not offered by question %q", label, answer.Id)
			}
			if selected[label] {
				return nil, common.Errorf(common.Invalid, "option %q is selected twice", label)
			}
			selected[label] = true
		}
		if len(question.Options) == 0 && len(answer.Selected) > 0 {
			return nil, common.Errorf(common.Invalid, "question %q takes free text, not an option", answer.Id)
		}
	}
	if len(questions) == 1 && normalized[0].Selected == nil && normalized[0].Custom == "" {
		return nil, common.Errorf(common.Invalid, "an answer is required; dismiss the ask instead")
	}
	return normalized, nil
}

// BuildAnswerEnvelope renders the lead-conversation answer content. requestNames
// are the API resource names of the requests the answer was delivered to.
func BuildAnswerEnvelope(ask *store.QualityAgentAskMessage, answers []*storepb.QualityAgentAskAnswer, requestNames []string) (string, error) {
	if ask == nil {
		return "", common.Errorf(common.Invalid, "an answer needs an ask")
	}
	envelope := AnswerEnvelope{
		Kind:      "answer_to_ask",
		Ask:       AskName(ask),
		Questions: answerQuestions(QuestionsOf(ask)),
		Answers:   answerItems(answers),
		Requests:  requestNames,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", common.Errorf(common.Internal, "failed to encode the answer: %v", err)
	}
	return string(encoded), nil
}

// BuildRequestAnswerEnvelope renders the content delivered to the subagent whose
// request the owner answered. The request id is the subagent's pairing key.
func BuildRequestAnswerEnvelope(ask *store.QualityAgentAskMessage, request *store.QualityAgentRequestMessage, question *storepb.QualityAgentAskQuestion, answer *storepb.QualityAgentAskAnswer) (string, string, error) {
	if ask == nil || request == nil || question == nil || answer == nil {
		return "", "", common.Errorf(common.Invalid, "a request answer needs an ask, a request, a question and an answer")
	}
	envelope := RequestAnswerEnvelope{
		Kind:      "answer_to_request",
		Ask:       AskName(ask),
		Request:   RequestName(request),
		Questions: answerQuestions([]*storepb.QualityAgentAskQuestion{question}),
		Answers:   answerItems([]*storepb.QualityAgentAskAnswer{answer}),
		Text:      AnswerTextFor(question, answer),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", "", common.Errorf(common.Internal, "failed to encode the request answer: %v", err)
	}
	return string(encoded), envelope.Text, nil
}

// RequestDismissedEnvelope is the model-facing notice delivered to a subagent when
// the owner dismisses the ask holding its request. It is not an answer: the request
// closes with no resolution.
type RequestDismissedEnvelope struct {
	Kind    string `json:"kind"`
	Ask     string `json:"ask"`
	Request string `json:"request"`
	Text    string `json:"text"`
}

// BuildRequestDismissedEnvelope renders the notice for the subagent whose request the
// owner dismissed. The request id is still the pairing key.
func BuildRequestDismissedEnvelope(ask *store.QualityAgentAskMessage, request *store.QualityAgentRequestMessage) (string, error) {
	if ask == nil || request == nil {
		return "", common.Errorf(common.Invalid, "a request dismissal needs an ask and a request")
	}
	envelope := RequestDismissedEnvelope{
		Kind:    "request_dismissed",
		Ask:     AskName(ask),
		Request: RequestName(request),
		Text:    "the owner dismissed this question without an answer",
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", common.Errorf(common.Internal, "failed to encode the request dismissal: %v", err)
	}
	return string(encoded), nil
}

// BuildDecisionAnswerEnvelope is the envelope for a decision the lead made itself
// rather than relaying an owner answer. The request id is still the pairing key, so
// the subagent can tell which question this answers.
func BuildDecisionAnswerEnvelope(request *store.QualityAgentRequestMessage, answer string, selected []string, custom string) string {
	text := strings.TrimSpace(answer)
	if text == "" {
		values := append([]string(nil), selected...)
		if strings.TrimSpace(custom) != "" {
			values = append(values, strings.TrimSpace(custom))
		}
		text = strings.Join(values, ", ")
	}
	envelope := RequestAnswerEnvelope{
		Kind:    "answer_to_request",
		Request: RequestName(request),
		Questions: []AnswerQuestion{{
			ID:       "request",
			Question: request.Question,
		}},
		Answers: []AnswerItem{{
			ID:       "request",
			Selected: append([]string(nil), selected...),
			Custom:   strings.TrimSpace(custom),
		}},
		Text: text,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return text
	}
	return string(encoded)
}

// AnswerTextFor renders one question's answer as the human-readable sentence used
// as the request's resolution.
func AnswerTextFor(question *storepb.QualityAgentAskQuestion, answer *storepb.QualityAgentAskAnswer) string {
	if question == nil || answer == nil {
		return ""
	}
	values := append([]string(nil), answer.Selected...)
	if answer.Custom != "" {
		values = append(values, answer.Custom)
	}
	if len(values) == 0 {
		return "skipped"
	}
	return strings.Join(values, ", ")
}

// AnswerText renders the owner's choices as one human-readable sentence, used as
// the request's resolution and as the model-facing text beside the envelope.
func AnswerText(questions []*storepb.QualityAgentAskQuestion, answers []*storepb.QualityAgentAskAnswer) string {
	items := make(map[string]*storepb.QualityAgentAskAnswer, len(answers))
	for _, answer := range answers {
		items[strings.TrimSpace(answer.Id)] = answer
	}
	parts := make([]string, 0, len(questions))
	for _, question := range questions {
		answer := items[strings.TrimSpace(question.Id)]
		if answer == nil {
			continue
		}
		values := append([]string(nil), answer.Selected...)
		if answer.Custom != "" {
			values = append(values, answer.Custom)
		}
		text := strings.Join(values, ", ")
		if text == "" {
			text = "skipped"
		}
		if len(questions) == 1 {
			parts = append(parts, text)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", question.Question, text))
	}
	return strings.Join(parts, "; ")
}

func answerQuestions(questions []*storepb.QualityAgentAskQuestion) []AnswerQuestion {
	converted := make([]AnswerQuestion, 0, len(questions))
	for _, question := range questions {
		if question == nil {
			continue
		}
		options := make([]AnswerOption, 0, len(question.Options))
		for _, option := range question.Options {
			if option == nil {
				continue
			}
			options = append(options, AnswerOption{Label: option.Label, Description: option.Description})
		}
		converted = append(converted, AnswerQuestion{
			ID:          question.Id,
			Header:      question.Header,
			Question:    question.Question,
			Detail:      question.Detail,
			Options:     options,
			MultiSelect: question.MultiSelect,
			Requests:    append([]string(nil), question.Requests...),
		})
	}
	return converted
}

func answerItems(answers []*storepb.QualityAgentAskAnswer) []AnswerItem {
	converted := make([]AnswerItem, 0, len(answers))
	for _, answer := range answers {
		if answer == nil {
			continue
		}
		converted = append(converted, AnswerItem{
			ID:       answer.Id,
			Selected: append([]string(nil), answer.Selected...),
			Custom:   answer.Custom,
		})
	}
	return converted
}

// ParseAnswerEnvelope reads an answer envelope from a transcript message. It is
// the reader the wake prompt uses; an unreadable message is not an answer.
func ParseAnswerEnvelope(content string) (*AnswerEnvelope, bool) {
	var envelope AnswerEnvelope
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return nil, false
	}
	if envelope.Kind != "answer_to_ask" || envelope.Ask == "" {
		return nil, false
	}
	return &envelope, true
}

// ParseRequestAnswerEnvelope reads the envelope delivered to a subagent. The
// request id is the pairing key; an unreadable message is not an answer.
func ParseRequestAnswerEnvelope(content string) (*RequestAnswerEnvelope, bool) {
	var envelope RequestAnswerEnvelope
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return nil, false
	}
	if envelope.Kind != "answer_to_request" || envelope.Request == "" {
		return nil, false
	}
	return &envelope, true
}

// ParseRequestDismissedEnvelope reads the notice delivered to a subagent. The
// request id is the pairing key; an unreadable message is not a dismissal.
func ParseRequestDismissedEnvelope(content string) (*RequestDismissedEnvelope, bool) {
	var envelope RequestDismissedEnvelope
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return nil, false
	}
	if envelope.Kind != "request_dismissed" || envelope.Request == "" {
		return nil, false
	}
	return &envelope, true
}

func containsNUL(value string) bool {
	return strings.ContainsRune(value, 0)
}
