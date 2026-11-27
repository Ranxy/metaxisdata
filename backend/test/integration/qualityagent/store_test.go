//go:build integration

package qualityagent

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := f.raiseRaw(raiseOptions{dedupeKey: "it-concurrent-same", question: "The same question?"})
			errs <- err
		}()
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
		different.Add(1)
		go func(worker int) {
			defer different.Done()
			_, _, err := f.raiseRaw(raiseOptions{
				dedupeKey: "it-concurrent-different",
				question:  fmt.Sprintf("Different question %d?", worker),
				conflict:  true,
			})
			conflicts <- err
		}(worker)
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

func TestQualityAgentAskDismissesItsRequests(t *testing.T) {
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
	applied, err := f.env.store.DismissQualityAgentAsk(f.ctx, created.ID, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, applied)
	dismissedAsk, err := f.env.store.GetQualityAgentAskByResourceID(f.ctx, created.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusDismissed, dismissedAsk.Status)
	require.Equal(t, store.QualityAgentRequestStatusDismissed, f.getRequest(t, request.ResourceID).Status)
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
