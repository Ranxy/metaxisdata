//go:build integration

package qualityagent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

type httpAgentFixture struct {
	ctx          context.Context
	env          *integrationenv.ServiceEnv
	dataQuality  v1connect.DataQualityServiceClient
	settings     v1connect.SettingServiceClient
	llm          v1connect.LLMServiceClient
	users        v1connect.UserServiceClient
	adminToken   string
	ownerToken   string
	ownerName    string
	agentName    string
	agent        *store.QualityAgentMessage
	workspaceID  string
	targetGUID   string
	provider     *fakeAgentProvider
	providerName string
}

func newHTTPAgentFixture(t *testing.T) *httpAgentFixture {
	t.Helper()

	return newHTTPAgentFixtureWithTables(t, false)
}

func newHTTPAgentFixtureWithTables(t *testing.T, keepOrders bool) *httpAgentFixture {
	t.Helper()

	require.NotNil(t, testEnv)
	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}
	f := &httpAgentFixture{
		ctx:         ctx,
		env:         testEnv,
		dataQuality: v1connect.NewDataQualityServiceClient(client, testEnv.BaseURL),
		settings:    v1connect.NewSettingServiceClient(client, testEnv.BaseURL),
		llm:         v1connect.NewLLMServiceClient(client, testEnv.BaseURL),
		users:       v1connect.NewUserServiceClient(client, testEnv.BaseURL),
		adminToken:  testEnv.AdminToken(),
	}
	workspaceID, err := testEnv.Store.GetWorkspaceID(ctx)
	require.NoError(t, err)
	f.workspaceID = workspaceID

	// A dedicated PostgreSQL source database keeps the target GUID and sync isolated
	// from every other scenario in this package.
	sequence := fixtureSequence.Add(1)
	instanceID := fmt.Sprintf("itqa-http-%d", sequence)
	sourceDatabase := fmt.Sprintf("it_qa_http_%d", sequence)
	instance, err := testEnv.CreatePostgresInstance(ctx, instanceID)
	require.NoError(t, err)
	require.NoError(t, testEnv.ExecPostgres(ctx, "postgres", fmt.Sprintf(`CREATE DATABASE %s`, sourceDatabase)))
	require.NoError(t, testEnv.ExecPostgres(ctx, sourceDatabase, integrationenv.PostgresFixtureResetDDL))
	// The agent scope is the whole database. Unless a scenario needs a second target,
	// remove the fixture's other objects so exactly one session exists.
	require.NoError(t, testEnv.ExecPostgres(ctx, sourceDatabase,
		`DROP VIEW IF EXISTS public.user_order_view`))
	if !keepOrders {
		require.NoError(t, testEnv.ExecPostgres(ctx, sourceDatabase,
			`DROP TABLE IF EXISTS public.orders, public.manual_sql_summary`))
	}
	t.Cleanup(func() {
		background := context.Background()
		_ = testEnv.ExecPostgres(background, "postgres", fmt.Sprintf(
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`,
			sourceDatabase))
		_ = testEnv.ExecPostgres(background, "postgres", fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, sourceDatabase))
		_ = instance
	})
	_ = testEnv.EnsureDatabaseVisible(ctx, t, instance.GetName(), sourceDatabase)
	databaseName := common.FormatDatabase(instanceID, sourceDatabase)
	_ = testEnv.SyncDatabase(ctx, t, databaseName)
	_, err = testEnv.CreateDataSource(ctx, common.FormatInstance(instanceID), &v1pb.DataSource{
		Type:     v1pb.DataSourceType_READ_ONLY,
		Username: "postgres",
		Password: "postgres",
		Host:     testEnv.PostgresHost,
		Port:     testEnv.PostgresPort,
	}, "readonly", false)
	require.NoError(t, err)
	_ = testEnv.SyncDatabase(ctx, t, databaseName)
	f.targetGUID = waitForMetaGUIDByName(ctx, t, testEnv, instanceID+";"+sourceDatabase, "users")

	// One covered target makes the enrollment runner create a MONITOR session instead
	// of a TRIAGE admission.
	minRows := int64(1)
	_, err = f.dataQuality.CreateQualityRule(ctx, withToken(f.adminToken, &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "integration http agent row count",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Target:    &v1pb.QualityTarget{MetaGuid: f.targetGUID, MetaType: v1pb.MetaType_TABLE},
			Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{
				RowCount: &v1pb.RowCountConfig{MinRows: &minRows},
			}},
			Severity: v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
	}))
	require.NoError(t, err)
	_, err = f.dataQuality.PublishQualityContract(ctx, withToken(f.adminToken, &v1pb.PublishQualityContractRequest{
		Parent:     "workspaces/-",
		TargetGuid: f.targetGUID,
		MetaType:   v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)

	f.provider = newFakeAgentProvider(t)
	profile, err := f.llm.CreateLLMProviderProfile(ctx, withToken(f.adminToken, &v1pb.CreateLLMProviderProfileRequest{
		Profile: &v1pb.LlmProviderProfile{
			Title:   "integration agent provider",
			Type:    v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI,
			BaseUrl: f.provider.URL(),
			ApiKey:  agentProviderAPIKey,
			Models:  []*v1pb.LlmProviderModel{{Name: "integration-agent-model", Enabled: true}},
		},
	}))
	require.NoError(t, err)
	f.providerName = profile.Msg.GetName()
	t.Cleanup(func() {
		_, _ = f.llm.DeleteLLMProviderProfile(context.Background(),
			withToken(testEnv.AdminToken(), &v1pb.DeleteLLMProviderProfileRequest{Name: f.providerName}))
	})

	originalSettings, err := f.settings.GetWorkspaceProfileSetting(ctx,
		withToken(f.adminToken, &v1pb.GetWorkspaceProfileSettingRequest{}))
	require.NoError(t, err)
	originalDataQuality := &v1pb.DataQualitySetting{}
	if current := originalSettings.Msg.GetDataQuality(); current != nil {
		if cloned, ok := proto.Clone(current).(*v1pb.DataQualitySetting); ok {
			originalDataQuality = cloned
		}
	}
	enabled := &v1pb.DataQualitySetting{}
	if cloned, ok := proto.Clone(originalDataQuality).(*v1pb.DataQualitySetting); ok {
		enabled = cloned
	}
	enabled.EnableLlmAnalysis = true
	enabled.AgentRunnerEnabled = true
	_, err = f.settings.UpdateWorkspaceProfileSetting(ctx, withToken(f.adminToken, &v1pb.UpdateWorkspaceProfileSettingRequest{
		Setting:    &v1pb.WorkspaceProfileSetting{DataQuality: enabled},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"data_quality"}},
	}))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = f.settings.UpdateWorkspaceProfileSetting(context.Background(),
			withToken(testEnv.AdminToken(), &v1pb.UpdateWorkspaceProfileSettingRequest{
				Setting:    &v1pb.WorkspaceProfileSetting{DataQuality: originalDataQuality},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"data_quality"}},
			}))
	})

	email := fmt.Sprintf("itqa-owner-%d@example.com", sequence)
	const password = "Integration-Password-1!"
	createdOwner, err := f.users.CreateUser(ctx, withToken(f.adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{Email: email, Title: "Integration agent owner", Password: password, UserType: v1pb.UserType_END_USER},
	}))
	require.NoError(t, err)
	f.ownerName = createdOwner.Msg.GetName()
	f.ownerToken, err = testEnv.LoginAs(ctx, email, password)
	require.NoError(t, err)

	created, err := f.dataQuality.CreateQualityAgent(ctx, withToken(f.adminToken, &v1pb.CreateQualityAgentRequest{
		Parent:          "workspaces/-",
		DisplayName:     "integration http agent",
		Owner:           f.ownerName,
		ProviderProfile: f.providerName,
		ProviderModel:   "integration-agent-model",
		Databases:       []string{databaseName},
		Capabilities:    []string{"suggest", "patrol"},
	}))
	require.NoError(t, err)
	f.agentName = created.Msg.GetAgent().GetName()
	stored, err := testEnv.Store.GetQualityAgentByResourceID(ctx, resourceIDFromName(f.agentName))
	require.NoError(t, err)
	f.agent = stored
	_, err = f.dataQuality.StartQualityAgent(ctx, withToken(f.adminToken, &v1pb.StartQualityAgentRequest{Name: f.agentName}))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = f.dataQuality.PauseQualityAgent(context.Background(),
			withToken(f.adminToken, &v1pb.PauseQualityAgentRequest{Name: f.agentName}))
	})
	return f
}

func (f *httpAgentFixture) waitForInitialReview(t *testing.T, session *v1pb.QualityAgentSession) {
	t.Helper()

	stored, err := f.env.Store.GetQualityAgentSession(f.ctx, resourceIDFromName(session.GetName()))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current, err := f.env.Store.GetQualityAgentSession(f.ctx, stored.ResourceID)
		return err == nil && current != nil && current.LastReviewedAt != nil
	}, 60*time.Second, 250*time.Millisecond,
		"the covered target never finished its deterministic review: %s", f.env.ServerLogs())
	require.Empty(t, f.provider.recordedCalls(), "a healthy target's review must not call the model")
}

func (f *httpAgentFixture) waitForTargetSessions(t *testing.T, count int) []*v1pb.QualityAgentSession {
	t.Helper()

	var sessions []*v1pb.QualityAgentSession
	require.Eventually(t, func() bool {
		response, err := f.dataQuality.ListQualityAgentSessions(f.ctx, withToken(f.adminToken,
			&v1pb.ListQualityAgentSessionsRequest{Parent: "workspaces/-", Agent: f.agentName, PageSize: 100}))
		if err != nil {
			return false
		}
		sessions = response.Msg.GetQualityAgentSessions()
		return len(sessions) >= count
	}, 150*time.Second, 500*time.Millisecond,
		"the enrollment runner never materialized %d target sessions: %s", count, f.env.ServerLogs())
	return sessions
}

func (f *httpAgentFixture) waitForTargetSession(t *testing.T) *v1pb.QualityAgentSession {
	t.Helper()

	var session *v1pb.QualityAgentSession
	require.Eventually(t, func() bool {
		response, err := f.dataQuality.ListQualityAgentSessions(f.ctx, withToken(f.adminToken,
			&v1pb.ListQualityAgentSessionsRequest{Parent: "workspaces/-", Agent: f.agentName, PageSize: 100}))
		if err != nil {
			return false
		}
		for _, candidate := range response.Msg.GetQualityAgentSessions() {
			if candidate.GetTarget().GetMetaGuid() == f.targetGUID {
				session = candidate
				return true
			}
		}
		return false
	}, 150*time.Second, 500*time.Millisecond,
		"the enrollment runner never materialized the covered target: %s", f.env.ServerLogs())
	return session
}

func (f *httpAgentFixture) nudgeTarget(t *testing.T, session *v1pb.QualityAgentSession) {
	t.Helper()

	stored, err := f.env.Store.GetQualityAgentSession(f.ctx, resourceIDFromName(session.GetName()))
	require.NoError(t, err)
	_, err = f.env.Store.AppendQualityAgentTranscriptMessage(f.ctx, &store.QualityAgentTranscriptMessage{
		SessionID:      stored.ID,
		WorkspaceID:    f.workspaceID,
		Wake:           stored.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:        "Please inspect the target and ask the lead if a decision is needed.",
		SenderName:     "lead",
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ASSIGN,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		IdempotencyKey: "it-http-target-nudge",
	})
	require.NoError(t, err)
	_, err = f.env.Store.AdvanceQualityAgentSessionWake(f.ctx, stored.ResourceID, time.Now().UTC())
	require.NoError(t, err)
}

func (f *httpAgentFixture) waitForOpenQuestion(t *testing.T) *store.QualityAgentRequestMessage {
	t.Helper()

	var request *store.QualityAgentRequestMessage
	require.Eventually(t, func() bool {
		requests, err := f.env.Store.ListQualityAgentRequests(f.ctx, f.agent.ID,
			[]store.QualityAgentRequestStatus{store.QualityAgentRequestStatusOpen}, f.targetGUID, 10)
		if err != nil || len(requests) == 0 {
			return false
		}
		request = requests[0]
		return true
	}, 90*time.Second, 250*time.Millisecond,
		"the target subagent never asked the lead: %s", f.env.ServerLogs())
	return request
}

func (f *httpAgentFixture) waitForOpenAsk(t *testing.T) *store.QualityAgentAskMessage {
	t.Helper()

	var ask *store.QualityAgentAskMessage
	require.Eventually(t, func() bool {
		asks, err := f.env.Store.ListQualityAgentAsks(f.ctx, f.agent.ID,
			[]store.QualityAgentAskStatus{store.QualityAgentAskStatusOpen}, 10)
		if err != nil || len(asks) == 0 {
			return false
		}
		ask = asks[0]
		return true
	}, 90*time.Second, 250*time.Millisecond,
		"the lead never forwarded the question to the owner: %s", f.env.ServerLogs())
	return ask
}

func (f *httpAgentFixture) diagnostics() string {
	var builder strings.Builder
	for index, call := range f.provider.recordedCalls() {
		fmt.Fprintf(&builder, "provider call %d role=%s last_user=%q\n", index+1, providerRole(call), truncate(lastUserContent(call), 120))
	}
	if lead, err := f.env.Store.GetQualityAgentConversation(f.ctx, f.agent.ID); err == nil && lead != nil {
		lease := "nil"
		if lead.LeaseUntil != nil {
			lease = lead.LeaseUntil.String()
		}
		next := "nil"
		if lead.NextWakeAt != nil {
			next = lead.NextWakeAt.String()
		}
		fmt.Fprintf(&builder, "lead session state=%d next_wake=%s lease=%s wake=%d processed=%d blocked=%q\n",
			lead.State, next, lease, lead.WakeCount, lead.ProcessedSeq, lead.BlockedCode)
		if messages, err := f.env.Store.ListQualityAgentTranscriptMessages(f.ctx, &store.FindQualityAgentTranscriptMessage{
			SessionID: lead.ID, Limit: 40,
		}); err == nil {
			for _, message := range messages {
				toolCount := 0
				if message.ToolCalls != nil {
					toolCount = len(message.ToolCalls.ToolCalls)
				}
				fmt.Fprintf(&builder, "lead %s %s intent=%s content=%q tools=%d\n",
					message.Role, message.Origin, message.Intent, truncate(message.Content, 120), toolCount)
			}
		}
	}
	if requests, err := f.env.Store.ListQualityAgentRequests(f.ctx, f.agent.ID, nil, f.targetGUID, 20); err == nil {
		for _, request := range requests {
			fmt.Fprintf(&builder, "request %s key=%s status=%d question=%q\n", request.ResourceID, request.DedupeKey, request.Status, truncate(request.Question, 80))
		}
	}
	if asks, err := f.env.Store.ListQualityAgentAsks(f.ctx, f.agent.ID, nil, 20); err == nil {
		for _, ask := range asks {
			fmt.Fprintf(&builder, "ask %s status=%d question=%q\n", ask.ResourceID, ask.Status, truncate(ask.Question, 80))
		}
	}
	return builder.String()
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func (f *httpAgentFixture) getHTTPRequest(t *testing.T, resourceID string) *store.QualityAgentRequestMessage {
	t.Helper()

	request, err := f.env.Store.GetQualityAgentRequestByResourceID(f.ctx, resourceID)
	require.NoError(t, err)
	require.NotNil(t, request)
	return request
}

func (f *httpAgentFixture) waitForRequestStatus(t *testing.T, resourceID string, status store.QualityAgentRequestStatus) *store.QualityAgentRequestMessage {
	t.Helper()

	var request *store.QualityAgentRequestMessage
	require.Eventually(t, func() bool {
		candidate, err := f.env.Store.GetQualityAgentRequestByResourceID(f.ctx, resourceID)
		if err != nil || candidate == nil || candidate.Status != status {
			return false
		}
		request = candidate
		return true
	}, 90*time.Second, 250*time.Millisecond,
		"request %s never reached status %d: %s\nstate:\n%s", resourceID, status, f.env.ServerLogs(), f.diagnostics())
	return request
}

func (f *httpAgentFixture) waitForAskStatus(t *testing.T, resourceID string, status store.QualityAgentAskStatus) {
	t.Helper()

	require.Eventually(t, func() bool {
		candidate, err := f.env.Store.GetQualityAgentAskByResourceID(f.ctx, resourceID)
		return err == nil && candidate != nil && candidate.Status == status
	}, 90*time.Second, 250*time.Millisecond,
		"ask %s never reached status %d: %s\nstate:\n%s", resourceID, status, f.env.ServerLogs(), f.diagnostics())
}

// TestQualityAgentHTTPServerOwnerLoop is the HTTP wire test: a real runner wakes a
// real target and lead through an OpenAI-compatible fake provider, the owner answers
// through the public API, the ask becomes ANSWERED, and the request's delivery back to
// the subagent runs through the same atomic store method the lead's tool uses. The
// lead-model answer path itself is covered against the real store in the engine suite.
func TestQualityAgentHTTPServerOwnerLoop(t *testing.T) {
	f := newHTTPAgentFixture(t)
	session := f.waitForTargetSession(t)
	f.waitForInitialReview(t, session)
	f.nudgeTarget(t, session)

	request := f.waitForOpenQuestion(t)
	require.NotNil(t, request)
	ask := f.waitForOpenAsk(t)
	require.NotEmpty(t, ask.ResourceID)
	askName := common.FormatWorkspace(f.workspaceID) + "/qualityAgentAsks/" + ask.ResourceID

	_, err := f.dataQuality.SendQualityAgentMessage(f.ctx, withToken(f.ownerToken, &v1pb.SendQualityAgentMessageRequest{
		Name:        f.agentName,
		MessageBody: "Yes, raise it to five minutes.",
		ReplyTo:     askName,
	}))
	require.NoError(t, err)
	f.waitForAskStatus(t, ask.ResourceID, store.QualityAgentAskStatusAnswered)
	require.Equal(t, store.QualityAgentRequestStatusForwarded, f.getHTTPRequest(t, request.ResourceID).Status,
		"the owner's reply answers the ask; the lead still owns the request")

	// The lead's model answer path is covered against the real store in the engine
	// suite. Here the HTTP test ends the wire after the owner reply proves the ask
	// transition, then delivers the answer through the same atomic store method the
	// lead's tool calls, so the subagent transcript and ask drainage are still real.
	storedSession, err := f.env.Store.GetQualityAgentSession(f.ctx, resourceIDFromName(session.GetName()))
	require.NoError(t, err)
	answer := &store.QualityAgentTranscriptMessage{
		SessionID:      storedSession.ID,
		WorkspaceID:    f.workspaceID,
		Wake:           storedSession.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:        "Agent lead sent a message:\n[answer] raise it to five minutes",
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		IdempotencyKey: "it-http-answer",
	}
	_, _, applied, err := f.env.Store.AnswerQualityAgentRequestAtomic(f.ctx, request.ResourceID,
		store.QualityAgentRequestStatusForwarded, "raise it to five minutes", storedSession, answer, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, applied)
	answered := f.waitForRequestStatus(t, request.ResourceID, store.QualityAgentRequestStatusAnswered)
	require.Contains(t, answered.Resolution, "raise it to five minutes")
	f.waitForAskStatus(t, ask.ResourceID, store.QualityAgentAskStatusResolved)

	// The subagent receives the answer as an INTERNAL lead message, and the owner's
	// front office never exposes the tool-call machinery.
	require.Eventually(t, func() bool {
		messages, err := f.env.Store.ListQualityAgentTranscriptMessages(f.ctx, &store.FindQualityAgentTranscriptMessage{
			SessionID: storedSession.ID, Limit: 100,
		})
		if err != nil {
			return false
		}
		for _, message := range messages {
			if message.Intent == storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER &&
				strings.Contains(message.Content, "raise it to five minutes") {
				return true
			}
		}
		return false
	}, 60*time.Second, 250*time.Millisecond, "the subagent never received the lead's answer")

	conversation, err := f.dataQuality.ListQualityAgentConversationMessages(f.ctx, withToken(f.ownerToken,
		&v1pb.ListQualityAgentConversationMessagesRequest{Parent: f.agentName, PageSize: 100}))
	require.NoError(t, err)
	var ownerAnswer bool
	for _, message := range conversation.Msg.GetQualityAgentConversationMessages() {
		require.Empty(t, message.GetToolCalls(), "the owner conversation never exposes raw tool calls")
		if message.GetRole() == v1pb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER &&
			message.GetContent() == "Yes, raise it to five minutes." {
			ownerAnswer = true
		}
	}
	require.True(t, ownerAnswer, "the owner sees their own reply in the front office")
	require.GreaterOrEqual(t, len(f.provider.recordedCalls()), 5, "the scripted lead/target turns all ran")
	var attentionTable int
	require.NoError(t, f.env.Store.GetDB().QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'quality_agent_attention'`).Scan(&attentionTable))
	require.Zero(t, attentionTable, "the legacy attention table is gone")
}

// The A architecture materializes every in-scope target, not one session per agent
// under an old active-session cap. Two fixture tables produce two sessions.
func TestQualityAgentMaterializesEveryTarget(t *testing.T) {
	f := newHTTPAgentFixtureWithTables(t, true)
	sessions := f.waitForTargetSessions(t, 2)
	targets := map[string]bool{}
	for _, session := range sessions {
		targets[session.GetTarget().GetMetaGuid()] = true
	}
	require.True(t, targets[f.targetGUID], "the covered users table has a session")
	var orders bool
	for target := range targets {
		if strings.HasSuffix(target, ";orders") {
			orders = true
		}
	}
	require.True(t, orders, "the uncovered orders table is admitted too")
}

func withToken[T any](token string, message *T) *connect.Request[T] {
	request := connect.NewRequest(message)
	request.Header().Set("Authorization", "Bearer "+token)
	return request
}

func resourceIDFromName(name string) string {
	return name[strings.LastIndex(name, "/")+1:]
}

func waitForMetaGUIDByName(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, prefix, name string) string {
	t.Helper()

	var guid string
	require.Eventually(t, func() bool {
		metaType := storepb.MetaType_TABLE
		resources, err := env.Store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
			GUIDPrefix: &prefix,
			ObjectType: &metaType,
		})
		if err != nil {
			return false
		}
		for _, resource := range resources {
			if strings.HasSuffix(resource.GUID, ";"+name) {
				guid = resource.GUID
				return true
			}
		}
		return false
	}, 30*time.Second, 200*time.Millisecond, "the fixture table was not synced under prefix %s", prefix)
	return guid
}
