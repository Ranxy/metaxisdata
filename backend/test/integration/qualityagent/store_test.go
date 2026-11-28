//go:build integration

package qualityagent

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestQualityAgentRequestDedupeAndConflict(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)

	first, created := f.create(t, raiseOptions{dedupeKey: "it-same", question: "Raise the timeout?"})
	require.True(t, created)
	second, created := f.create(t, raiseOptions{dedupeKey: "it-same", question: "Raise the timeout?"})
	require.False(t, created, "the same key and text is a duplicate")
	require.Equal(t, first.ResourceID, second.ResourceID)
	require.Len(t, f.activeRequests(t), 1)
	require.Len(t, f.notices(t), 1, "a duplicate must not append a second lead notice")

	whitespace, created := f.create(t, raiseOptions{dedupeKey: "it-whitespace", question: "Should we pause?"})
	require.True(t, created)
	retried, created := f.create(t, raiseOptions{dedupeKey: "it-whitespace", question: " Should\nwe pause? "})
	require.False(t, created, "a whitespace-only retry is the same question")
	require.Equal(t, whitespace.ResourceID, retried.ResourceID)
	refreshed := f.getRequest(t, whitespace.ResourceID)
	require.Equal(t, " Should\nwe pause? ", refreshed.Question, "the retry refreshes the wording")
	require.Len(t, f.notices(t), 2)

	_, _, err := f.raise(t, raiseOptions{dedupeKey: "it-conflict", question: "Pause the check?", conflict: true})
	require.NoError(t, err)
	_, _, err = f.raise(t, raiseOptions{dedupeKey: "it-conflict", question: "Resume the check?", conflict: true})
	require.Error(t, err, "the same key for a different decision is a conflict")
	conflictRow, err := f.env.store.ListQualityAgentRequests(f.ctx, f.agent.ID,
		[]store.QualityAgentRequestStatus{store.QualityAgentRequestStatusOpen}, f.targetGUID, 100)
	require.NoError(t, err)
	var paused *store.QualityAgentRequestMessage
	for _, request := range conflictRow {
		if request.DedupeKey == "it-conflict" {
			paused = request
		}
	}
	require.NotNil(t, paused)
	require.Equal(t, "Pause the check?", paused.Question, "a conflict never overwrites the existing question")

	drifted, created := f.create(t, raiseOptions{dedupeKey: "it-drifted", question: "Is the target healthy?"})
	require.True(t, created)
	sameText, created := f.create(t, raiseOptions{dedupeKey: "it-drifted-other-key", question: "Is the target healthy?"})
	require.False(t, created, "the fingerprint finds the same question under a drifted key")
	require.Equal(t, drifted.ResourceID, sameText.ResourceID)
}

func TestQualityAgentRequestFingerprintKeepsKindDistinct(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	decision, created := f.create(t, raiseOptions{
		dedupeKey: "it-kind-decision", question: "The check needs a decision", kind: store.QualityAgentRequestKindDecision,
	})
	require.True(t, created)
	question, created := f.create(t, raiseOptions{
		dedupeKey: "it-kind-question", question: "The check needs a decision", kind: store.QualityAgentRequestKindQuestion,
		conflict: true,
	})
	require.True(t, created, "the same text in different kinds is a different identity")
	require.NotEqual(t, decision.ResourceID, question.ResourceID)
	require.Len(t, f.activeRequests(t), 2)
}

func TestQualityAgentRequestClosingTheActiveRowFreesItsKey(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	request, created := f.create(t, raiseOptions{dedupeKey: "it-close", question: "A question?"})
	require.True(t, created)
	_, applied, err := f.env.store.UpdateQualityAgentRequestStatus(f.ctx, request.ResourceID,
		store.QualityAgentRequestStatusOpen, store.QualityAgentRequestStatusResolved, nil, "settled", nil, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, applied)

	reopened, created := f.create(t, raiseOptions{dedupeKey: "it-close", question: "A question?"})
	require.True(t, created, "a closed request no longer owns the key")
	require.NotEqual(t, request.ResourceID, reopened.ResourceID)
	require.Len(t, f.activeRequests(t), 1)
}

// A wording refresh must move the fingerprint with the text. The old text then must
// not resolve to the rewritten row, and the new text must.
func TestQualityAgentRequestRefreshMovesTheFingerprint(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	first := f.createDirect(t, raiseOptions{
		dedupeKey: "it-refresh", question: "version one", kind: store.QualityAgentRequestKindDecision,
	})
	require.True(t, first.created)
	before := f.getRequest(t, first.request.ResourceID).QuestionFingerprint
	require.NotEmpty(t, before)

	second := f.createDirect(t, raiseOptions{
		dedupeKey: "it-refresh", question: "version two", kind: store.QualityAgentRequestKindDecision,
	})
	require.False(t, second.created)
	require.Equal(t, first.request.ResourceID, second.request.ResourceID)
	refreshed := f.getRequest(t, first.request.ResourceID)
	require.Equal(t, "version two", refreshed.Question)
	require.NotEqual(t, before, refreshed.QuestionFingerprint, "the refresh rewrites the fingerprint")

	oldText := f.createDirect(t, raiseOptions{
		dedupeKey: "it-refresh-old", question: "version one", kind: store.QualityAgentRequestKindDecision,
	})
	require.True(t, oldText.created, "the old text is not the rewritten row's identity")
	newText := f.createDirect(t, raiseOptions{
		dedupeKey: "it-refresh-new", question: "version two", kind: store.QualityAgentRequestKindDecision,
	})
	require.False(t, newText.created, "the new text is the rewritten row's identity")
	require.Equal(t, first.request.ResourceID, newText.request.ResourceID)
}

func TestQualityAgentRequestConcurrentDedupe(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Go(func() {
			_, _, err := f.raiseRaw(raiseOptions{dedupeKey: "it-concurrent-same", question: "The same question?"})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, f.activeRequests(t), 1, "concurrent duplicates converge on one row")
	require.Len(t, f.notices(t), 1, "only the winner notifies the lead")

	var different sync.WaitGroup
	conflicts := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		different.Go(func() {
			_, _, err := f.raiseRaw(raiseOptions{
				dedupeKey: "it-concurrent-different",
				question:  fmt.Sprintf("Different question %d?", worker),
				conflict:  true,
			})
			conflicts <- err
		})
	}
	different.Wait()
	close(conflicts)
	successes, failures := 0, 0
	for err := range conflicts {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, workers-1, failures)
	require.Len(t, f.activeRequests(t), 2, "the different-key batch adds its own single row")
}

// A failed notice insert must roll the request back; a failed request insert must not
// leave a lead notice behind.
func TestQualityAgentRequestTransactionRollsBack(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	installFailTrigger(t, "quality_agent_message",
		"NEW.idempotency_key = 'request-notice:it-fail-notice:"+fmt.Sprint(f.session.ID)+"'")
	before := f.reloadLead(t).UpdatedAt
	_, _, err := f.raise(t, raiseOptions{dedupeKey: "it-fail-notice", question: "A question that must roll back?"})
	require.Error(t, err)
	require.Empty(t, f.activeRequests(t), "the request insert rolls back with the notice")
	require.Empty(t, f.notices(t))
	require.Equal(t, before, f.reloadLead(t).UpdatedAt, "the failed delivery does not advance the lead wake")

	installFailTrigger(t, "quality_agent_request", "NEW.dedupe_key = 'it-fail-request'")
	_, _, err = f.raise(t, raiseOptions{dedupeKey: "it-fail-request", question: "Another question?"})
	require.Error(t, err)
	require.Empty(t, f.notices(t), "a failed request insert never appends a notice")
}

func TestQualityAgentAskDedupesAndDrains(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	first, _ := f.create(t, raiseOptions{dedupeKey: "it-ask-r1", question: "First request?"})
	second, _ := f.create(t, raiseOptions{dedupeKey: "it-ask-r2", question: "Second request?"})
	askMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:        "One owner question",
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		IdempotencyKey: "it-ask-owner-message-1",
	}
	ask := &store.QualityAgentAskMessage{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, Kind: store.QualityAgentAskKindQuestion,
		Title: "A decision", Question: "One owner question",
		TargetGUID: f.targetGUID, TargetType: storepb.MetaType_TABLE, DedupeKey: "it-ask-dedupe",
	}
	createdAsk, created, err := f.env.store.CreateQualityAgentAskDeduped(f.ctx, ask, f.lead, askMessage,
		[]*store.QualityAgentRequestMessage{first, second}, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, created)
	require.NotEmpty(t, createdAsk.ResourceID)
	require.Equal(t, store.QualityAgentAskStatusOpen, createdAsk.Status)
	for _, request := range []*store.QualityAgentRequestMessage{first, second} {
		require.Equal(t, store.QualityAgentRequestStatusForwarded, f.getRequest(t, request.ResourceID).Status)
	}

	duplicateMessage := *askMessage
	duplicateMessage.IdempotencyKey = "it-ask-owner-message-2"
	duplicateAsk, created, err := f.env.store.CreateQualityAgentAskDeduped(f.ctx, ask, f.lead, &duplicateMessage,
		[]*store.QualityAgentRequestMessage{first}, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, created, "the ask dedupe key returns the open ask")
	require.Equal(t, createdAsk.ResourceID, duplicateAsk.ResourceID)
	asks, err := f.env.store.ListQualityAgentAsks(f.ctx, f.agent.ID, []store.QualityAgentAskStatus{store.QualityAgentAskStatusOpen}, 10)
	require.NoError(t, err)
	require.Len(t, asks, 1)
	require.Len(t, f.notices(t), 2, "the duplicate ask writes no second owner message")

	_, applied, err := f.env.store.MarkQualityAgentAskAnswered(f.ctx, createdAsk.ResourceID, 1, 1, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, applied)
	answer := &store.QualityAgentTranscriptMessage{
		Wake:           f.session.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:        "Use the first answer",
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		IdempotencyKey: "it-answer-r1",
	}
	_, _, applied, err = f.env.store.AnswerQualityAgentRequestAtomic(f.ctx, first.ResourceID,
		store.QualityAgentRequestStatusForwarded, "Use the first answer", f.session, answer, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, applied)
	stillOpen, err := f.env.store.GetQualityAgentAskByResourceID(f.ctx, createdAsk.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusAnswered, stillOpen.Status, "one request is still forwarded")

	answer.IdempotencyKey = "it-answer-r2"
	_, _, applied, err = f.env.store.AnswerQualityAgentRequestAtomic(f.ctx, second.ResourceID,
		store.QualityAgentRequestStatusForwarded, "Use the second answer", f.session, answer, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, applied)
	drained, err := f.env.store.GetQualityAgentAskByResourceID(f.ctx, createdAsk.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusResolved, drained.Status)
}

func TestQualityAgentAskDismissalClosesAndNotifies(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	request, _ := f.create(t, raiseOptions{dedupeKey: "it-dismiss-r", question: "Dismiss me?"})
	askMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:        "Dismiss me?",
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		IdempotencyKey: "it-dismiss-owner-message",
	}
	ask := &store.QualityAgentAskMessage{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, Kind: store.QualityAgentAskKindQuestion,
		Title: "Dismiss", Question: "Dismiss me?", TargetGUID: f.targetGUID,
		TargetType: storepb.MetaType_TABLE, DedupeKey: "it-dismiss-ask",
	}
	created, isNew, err := f.env.store.CreateQualityAgentAskDeduped(f.ctx, ask, f.lead, askMessage,
		[]*store.QualityAgentRequestMessage{request}, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, isNew)

	content, err := dataquality.BuildRequestDismissedEnvelope(created, request)
	require.NoError(t, err)
	ownerMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:        "Dismiss this request.",
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_DISMISS,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		ReplyTo:        dataquality.AskName(created),
		IdempotencyKey: "it-dismiss-lead",
	}
	dismissed, message, closed, err := f.env.store.DismissQualityAgentAskAtomic(f.ctx, created.ResourceID,
		ownerMessage, []*store.QualityAgentRequestDelivery{{
			Request:  request,
			Subagent: f.session,
			Message: &store.QualityAgentTranscriptMessage{
				Wake:           f.session.WakeCount + 1,
				Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
				Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
				Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_DISMISS,
				Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
				Content:        content,
				ReplyTo:        dataquality.RequestName(request),
				IdempotencyKey: "it-dismiss-subagent",
			},
		}}, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, dismissed)
	require.NotNil(t, message)
	require.Len(t, closed, 1)
	require.Equal(t, store.QualityAgentAskStatusDismissed, dismissed.Status)
	require.Equal(t, store.QualityAgentRequestStatusDismissed, closed[0].Status)
	require.Empty(t, closed[0].Resolution, "a dismissal never records an answer")

	storedAsk, err := f.env.store.GetQualityAgentAskByResourceID(f.ctx, created.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusDismissed, storedAsk.Status)
	require.Equal(t, store.QualityAgentRequestStatusDismissed, f.getRequest(t, request.ResourceID).Status)

	messages, err := f.env.store.ListQualityAgentTranscriptMessages(f.ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID: f.session.ID, Limit: 100,
	})
	require.NoError(t, err)
	var notified bool
	for _, entry := range messages {
		if entry.Intent == storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_DISMISS &&
			strings.Contains(entry.Content, "request_dismissed") {
			notified = true
		}
	}
	require.True(t, notified, "the origin subagent is told its request was dismissed")

	// The ask's status is the guard: a second dismissal must not append a second
	// owner message or a second notice.
	second := *ownerMessage
	second.IdempotencyKey = "it-dismiss-lead-2"
	_, _, _, err = f.env.store.DismissQualityAgentAskAtomic(f.ctx, created.ResourceID, &second, nil, time.Now().UTC())
	require.Error(t, err, "a closed ask refuses a second dismissal")
}

// A case's terminal transition closes its requests and a request-backed ask, but an
// agent-level ask with no requests is not drained by an unrelated case ending.
func TestQualityAgentCaseSettlementClosesOnlyRequestBackedAsks(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	caseRequest, _ := f.create(t, raiseOptions{dedupeKey: "it-case-request", caseID: "case-1", question: "Settle this case?"})
	askMessage := &store.QualityAgentTranscriptMessage{
		Wake: f.lead.WakeCount + 1, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin: storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK, Content: "Settle?",
		Audience: storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER, IdempotencyKey: "it-case-ask-message",
	}
	ask := &store.QualityAgentAskMessage{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, Kind: store.QualityAgentAskKindQuestion,
		Title: "Settle", Question: "Settle?", TargetGUID: f.targetGUID, TargetType: storepb.MetaType_TABLE, DedupeKey: "it-case-ask",
	}
	createdAsk, _, err := f.env.store.CreateQualityAgentAskDeduped(f.ctx, ask, f.lead, askMessage,
		[]*store.QualityAgentRequestMessage{caseRequest}, time.Now().UTC())
	require.NoError(t, err)

	anomaly := &store.QualityAgentAskMessage{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, Kind: store.QualityAgentAskKindAnomaly,
		Title: "Anomaly", Question: "No request behind me", DedupeKey: "it-anomaly-ask",
	}
	anomalyMessage := *askMessage
	anomalyMessage.IdempotencyKey = "it-anomaly-message"
	createdAnomaly, _, err := f.env.store.CreateQualityAgentAskDeduped(f.ctx, anomaly, f.lead, &anomalyMessage, nil, time.Now().UTC())
	require.NoError(t, err)

	closed, err := f.env.store.ResolveQualityAgentRequestsForCase(f.ctx, f.agent.ID, "case-1", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, int64(1), closed)
	resolved, err := f.env.store.ResolveQualityAgentAskIfDrained(f.ctx, createdAsk.ID, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, resolved)
	stillOpen, err := f.env.store.GetQualityAgentAskByResourceID(f.ctx, createdAnomaly.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusOpen, stillOpen.Status, "an ask with no requests is not drained")
}

func (f *draftAgent) createDirect(t *testing.T, options raiseOptions) requestCreation {
	t.Helper()

	if options.kind == 0 {
		options.kind = store.QualityAgentRequestKindDecision
	}
	origin := f.session.ID
	request, created, err := f.env.store.CreateQualityAgentRequest(f.ctx, &store.QualityAgentRequestMessage{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, OriginSessionID: &origin,
		TargetGUID: f.targetGUID, TargetType: storepb.MetaType_TABLE, Kind: options.kind,
		Status: store.QualityAgentRequestStatusOpen, Title: options.title, Question: options.question,
		CaseResourceID: options.caseID, DedupeKey: options.dedupeKey, QuestionKey: options.questionKey,
		ConflictOnDifferentQuestion: options.conflict,
	})
	require.NoError(t, err)
	return requestCreation{request: request, created: created}
}

type requestCreation struct {
	request *store.QualityAgentRequestMessage
	created bool
}

// One owner answer must settle the ask and every active request behind it in one
// transaction, and a second answer must be refused instead of delivered twice.
func TestQualityAgentAskAnswerIsAtomicAndDelivered(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	request, _ := f.create(t, raiseOptions{dedupeKey: "it-answer-ask-r", question: "Keep the check?"})
	askMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:        "Keep the check?",
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		IdempotencyKey: "it-answer-ask-question",
	}
	ask := &store.QualityAgentAskMessage{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, Kind: store.QualityAgentAskKindQuestion,
		Title: "A decision", Question: "Keep the check?",
		Questions: []*storepb.QualityAgentAskQuestion{{
			Id:       "q1",
			Question: "Keep the check?",
			Options: []*storepb.QualityAgentAskQuestionOption{
				{Label: "Keep it"},
				{Label: "Pause it"},
			},
		}},
		TargetGUID: f.targetGUID, TargetType: storepb.MetaType_TABLE, DedupeKey: "it-answer-ask",
	}
	created, isNew, err := f.env.store.CreateQualityAgentAskDeduped(f.ctx, ask, f.lead, askMessage,
		[]*store.QualityAgentRequestMessage{request}, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, isNew)

	answers := []*storepb.QualityAgentAskAnswer{{Id: "q1", Selected: []string{"Keep it"}}}
	leadMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		Content:        `{"kind":"answer_to_ask","ask":"` + created.ResourceID + `"}`,
		ReplyTo:        "workspaces/" + f.env.workspaceID + "/qualityAgentAsks/" + created.ResourceID,
		IdempotencyKey: "it-answer-ask-lead",
	}
	deliveryMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.session.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		Content:        `{"kind":"answer_to_request","request":"` + request.ResourceID + `"}`,
		ReplyTo:        "workspaces/" + f.env.workspaceID + "/qualityAgentRequests/" + request.ResourceID,
		IdempotencyKey: "it-answer-ask-sub",
	}
	answeredBy := int32(f.agent.PrincipalID)
	updated, delivered, answered, err := f.env.store.AnswerQualityAgentAskAtomic(f.ctx, created.ResourceID,
		answers, answeredBy, leadMessage,
		[]*store.QualityAgentRequestDelivery{{
			Request: request, Subagent: f.session, Message: deliveryMessage, Resolution: "Keep it",
		}}, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, delivered)
	require.Len(t, answered, 1)
	require.Equal(t, store.QualityAgentRequestStatusAnswered, f.getRequest(t, request.ResourceID).Status)

	stored, err := f.env.store.GetQualityAgentAskByResourceID(f.ctx, created.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusResolved, stored.Status, "the only request is settled")
	require.Len(t, stored.Answers, 1)
	require.Equal(t, []string{"Keep it"}, stored.Answers[0].Selected)
	require.Equal(t, "Keep it", f.getRequest(t, request.ResourceID).Resolution)

	second := *leadMessage
	second.IdempotencyKey = "it-answer-ask-lead-2"
	_, _, _, err = f.env.store.AnswerQualityAgentAskAtomic(f.ctx, created.ResourceID,
		answers, answeredBy, &second, nil, time.Now().UTC())
	require.Error(t, err, "a closed ask refuses a second answer")
}

// A batch ask delivers each question's answer only to the requests that question
// names, even when those requests share one subagent session.
func TestQualityAgentAskAnswerDeliversEachQuestionToItsRequest(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	first, _ := f.create(t, raiseOptions{dedupeKey: "it-batch-r1", question: "First?"})
	second, _ := f.create(t, raiseOptions{dedupeKey: "it-batch-r2", question: "Second?"})
	askMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:        "First?\n\nSecond?",
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		IdempotencyKey: "it-batch-question",
	}
	ask := &store.QualityAgentAskMessage{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, Kind: store.QualityAgentAskKindQuestion,
		Title: "Two decisions", Question: "First?\n\nSecond?",
		Questions: []*storepb.QualityAgentAskQuestion{
			{Id: "first", Question: "First?", Requests: []string{dataquality.RequestName(first)}},
			{Id: "second", Question: "Second?", Requests: []string{dataquality.RequestName(second)}},
		},
		TargetGUID: f.targetGUID, TargetType: storepb.MetaType_TABLE, DedupeKey: "it-batch-ask",
	}
	created, isNew, err := f.env.store.CreateQualityAgentAskDeduped(f.ctx, ask, f.lead, askMessage,
		[]*store.QualityAgentRequestMessage{first, second}, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, isNew)

	answers := []*storepb.QualityAgentAskAnswer{
		{Id: "first", Custom: "answer one"},
		{Id: "second", Custom: "answer two"},
	}
	leadMessage := &store.QualityAgentTranscriptMessage{
		Wake:           f.lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		Content:        `{"kind":"answer_to_ask"}`,
		ReplyTo:        dataquality.AskName(created),
		IdempotencyKey: "it-batch-lead",
	}
	delivery := func(request *store.QualityAgentRequestMessage, content, key string) *store.QualityAgentRequestDelivery {
		return &store.QualityAgentRequestDelivery{
			Request:  request,
			Subagent: f.session,
			Message: &store.QualityAgentTranscriptMessage{
				Wake:           f.session.WakeCount + 1,
				Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
				Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
				Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
				Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
				Content:        content,
				ReplyTo:        dataquality.RequestName(request),
				IdempotencyKey: key,
			},
			Resolution: content,
		}
	}
	updated, _, delivered, err := f.env.store.AnswerQualityAgentAskAtomic(f.ctx, created.ResourceID, answers,
		int32(f.agent.PrincipalID), leadMessage, []*store.QualityAgentRequestDelivery{
			delivery(first, "answer one", "it-batch-sub-1"),
			delivery(second, "answer two", "it-batch-sub-2"),
		}, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Len(t, delivered, 2)
	require.Equal(t, "answer one", f.getRequest(t, first.ResourceID).Resolution)
	require.Equal(t, "answer two", f.getRequest(t, second.ResourceID).Resolution)

	messages, err := f.env.store.ListQualityAgentTranscriptMessages(f.ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID: f.session.ID, Limit: 100,
	})
	require.NoError(t, err)
	var firstAnswer, secondAnswer bool
	for _, message := range messages {
		if strings.Contains(message.Content, "answer one") {
			firstAnswer = true
		}
		if strings.Contains(message.Content, "answer two") {
			secondAnswer = true
		}
	}
	require.True(t, firstAnswer, "the first request's answer reached the subagent")
	require.True(t, secondAnswer, "the second request's answer reached the subagent")
}
