//go:build integration

package qualityagent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

var fixtureSequence atomic.Int64

// draftAgent is a real store fixture: an agent that is not started, its lead and one
// target subagent. The agents stay DRAFT so the service environment's runners do not
// claim the sessions while a store or engine test owns them.
type draftAgent struct {
	env        *testEnvironment
	ctx        context.Context
	agent      *store.QualityAgentMessage
	lead       *store.QualityAgentSessionMessage
	session    *store.QualityAgentSessionMessage
	targetGUID string
}

type testEnvironment struct {
	store       *store.Store
	workspaceID string
}

func newDraftAgent(t *testing.T) *draftAgent {
	t.Helper()

	require.NotNil(t, testEnv, "the integration environment is not running")
	ctx := context.Background()
	workspaceID, err := testEnv.Store.GetWorkspaceID(ctx)
	require.NoError(t, err)
	resourceID := fmt.Sprintf("itqa%d", fixtureSequence.Add(1))
	agent, err := testEnv.Store.CreateQualityAgent(ctx, resourceID, "integration quality agent "+resourceID, "", nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		// The suite owns a disposable database, but deleting the agent keeps repeated
		// local runs small. Sessions, requests, asks and cases cascade from it.
		background := context.Background()
		if _, deleteErr := testEnv.Store.GetDB().ExecContext(background,
			`DELETE FROM quality_agent WHERE id = $1`, agent.ID); deleteErr != nil {
			t.Logf("cleanup: failed to delete quality agent %d: %v", agent.ID, deleteErr)
		}
		if _, updateErr := testEnv.Store.GetDB().ExecContext(background,
			`UPDATE principal SET deleted = TRUE WHERE id = $1`, agent.PrincipalID); updateErr != nil {
			t.Logf("cleanup: failed to deactivate principal %d: %v", agent.PrincipalID, updateErr)
		}
	})
	lead, err := testEnv.Store.EnsureQualityAgentConversation(ctx, workspaceID, agent.ID)
	require.NoError(t, err)
	targetGUID := fmt.Sprintf("it-qualityagent;%s;;orders", resourceID)
	session, err := testEnv.Store.CreateQualityAgentSession(ctx, &store.CreateQualityAgentSessionInput{
		WorkspaceID:     workspaceID,
		AgentID:         agent.ID,
		TargetGUID:      targetGUID,
		TargetType:      storepb.MetaType_TABLE,
		Mode:            storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR,
		Role:            storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_SUBAGENT,
		ParentSessionID: &lead.ID,
		SubagentName:    "orders",
	})
	require.NoError(t, err)
	return &draftAgent{
		env:        &testEnvironment{store: testEnv.Store, workspaceID: workspaceID},
		ctx:        ctx,
		agent:      agent,
		lead:       lead,
		session:    session,
		targetGUID: targetGUID,
	}
}

type raiseOptions struct {
	dedupeKey   string
	questionKey string
	caseID      string
	title       string
	question    string
	kind        store.QualityAgentRequestKind
	conflict    bool
	status      store.QualityAgentRequestStatus
}

func (f *draftAgent) raise(t *testing.T, options raiseOptions) (*store.QualityAgentRequestMessage, bool, error) {
	t.Helper()

	return f.raiseRaw(options)
}

func (f *draftAgent) raiseRaw(options raiseOptions) (*store.QualityAgentRequestMessage, bool, error) {
	if options.kind == 0 {
		options.kind = store.QualityAgentRequestKindQuestion
	}
	if options.title == "" {
		options.title = "Integration question"
	}
	if options.status == 0 {
		options.status = store.QualityAgentRequestStatusOpen
	}
	origin := f.session.ID
	message := &store.QualityAgentRequestMessage{
		WorkspaceID:                 f.env.workspaceID,
		AgentID:                     f.agent.ID,
		OriginSessionID:             &origin,
		TargetGUID:                  f.targetGUID,
		TargetType:                  storepb.MetaType_TABLE,
		Kind:                        options.kind,
		Status:                      options.status,
		Title:                       options.title,
		Question:                    options.question,
		CaseResourceID:              options.caseID,
		DedupeKey:                   options.dedupeKey,
		QuestionKey:                 options.questionKey,
		ConflictOnDifferentQuestion: options.conflict,
	}
	notice := &store.QualityAgentTranscriptMessage{
		Wake:            f.lead.WakeCount + 1,
		Role:            storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:          storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:         fmt.Sprintf("Agent %s sent a message:\n[question] %s", f.session.SubagentName, options.question),
		SenderSessionID: &origin,
		SenderName:      f.session.SubagentName,
		Intent:          storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION,
		Audience:        storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		IdempotencyKey:  fmt.Sprintf("request-notice:%s:%d", options.dedupeKey, f.session.ID),
	}
	request, created, err := f.env.store.RaiseQualityAgentRequest(f.ctx, message, f.lead, notice)
	return request, created, err
}

func (f *draftAgent) create(t *testing.T, options raiseOptions) (*store.QualityAgentRequestMessage, bool) {
	t.Helper()

	request, created, err := f.raise(t, options)
	require.NoError(t, err)
	return request, created
}

func (f *draftAgent) activeRequests(t *testing.T) []*store.QualityAgentRequestMessage {
	t.Helper()

	requests, err := f.env.store.ListQualityAgentRequests(f.ctx, f.agent.ID,
		[]store.QualityAgentRequestStatus{store.QualityAgentRequestStatusOpen, store.QualityAgentRequestStatusForwarded},
		f.targetGUID, 100)
	require.NoError(t, err)
	return requests
}

func (f *draftAgent) notices(t *testing.T) []*store.QualityAgentTranscriptMessage {
	t.Helper()

	messages, err := f.env.store.ListQualityAgentTranscriptMessages(f.ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID: f.lead.ID,
		Limit:     500,
	})
	require.NoError(t, err)
	notices := make([]*store.QualityAgentTranscriptMessage, 0, len(messages))
	for _, message := range messages {
		if message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT &&
			message.Audience == storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL {
			notices = append(notices, message)
		}
	}
	return notices
}

func (f *draftAgent) getRequest(t *testing.T, resourceID string) *store.QualityAgentRequestMessage {
	t.Helper()

	request, err := f.env.store.GetQualityAgentRequestByResourceID(f.ctx, resourceID)
	require.NoError(t, err)
	require.NotNil(t, request)
	return request
}

func (f *draftAgent) reloadLead(t *testing.T) *store.QualityAgentSessionMessage {
	t.Helper()

	lead, err := f.env.store.GetQualityAgentSession(f.ctx, f.lead.ResourceID)
	require.NoError(t, err)
	require.NotNil(t, lead)
	return lead
}

// installFailTrigger makes one insert fail when the condition on NEW is true. It is
// how the suite exercises the transaction boundaries without a test hook in production.
func installFailTrigger(t *testing.T, table, condition string) {
	t.Helper()

	suffix := fixtureSequence.Add(1)
	function := fmt.Sprintf("it_fail_%d", suffix)
	trigger := fmt.Sprintf("it_fail_trigger_%d", suffix)
	db := testEnv.Store.GetDB()
	_, err := db.ExecContext(context.Background(), fmt.Sprintf(`
CREATE OR REPLACE FUNCTION %s() RETURNS trigger AS $$
BEGIN
	IF %s THEN
		RAISE EXCEPTION 'integration failure injection';
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql`, function, condition))
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), fmt.Sprintf(
		`CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, trigger, table, function))
	require.NoError(t, err)
	t.Cleanup(func() {
		background := context.Background()
		_, _ = db.ExecContext(background, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON %s`, trigger, table))
		_, _ = db.ExecContext(background, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, function))
	})
}

func nowUTC() time.Time { return time.Now().UTC() }
