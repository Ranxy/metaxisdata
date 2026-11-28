package quality

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeAgentStore struct {
	suggestions []*store.QualitySuggestionMessage
	status      *store.QualityAgentAdoptionStatusMessage
}

type fakeAgentEvidence struct {
	executions []dataquality.ExecutionEvidence
	check      *dataquality.CheckEvidence
	err        error
	principal  int32
	target     string
	limit      int
	rule       string
	agent      int64
}

func (f *fakeAgentEvidence) ReadTargetExecutions(_ context.Context, principalID int32, targetGUID string, limit int) ([]dataquality.ExecutionEvidence, error) {
	f.principal, f.target, f.limit = principalID, targetGUID, limit
	return f.executions, f.err
}

func (f *fakeAgentEvidence) ReadCheckEvidence(_ context.Context, principalID int32, agentID int64, targetGUID, ruleResourceID string, limit int) (*dataquality.CheckEvidence, error) {
	f.principal, f.agent, f.target, f.rule, f.limit = principalID, agentID, targetGUID, ruleResourceID, limit
	return f.check, f.err
}

func (*fakeAgentStore) ListQualityRules(context.Context, *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) GetQualityContract(context.Context, string, storepb.MetaType) (*store.QualityContractMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) ListQualityExecutions(context.Context, *store.FindQualityExecutionMessage) ([]*store.QualityExecutionMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) ListQualityMemories(context.Context, *store.FindQualityMemoryMessage) ([]*store.QualityMemoryMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) CreateQualityMemory(context.Context, *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error) {
	return nil, nil
}

func (f *fakeAgentStore) ListQualitySuggestions(context.Context, *store.FindQualitySuggestionMessage) ([]*store.QualitySuggestionMessage, error) {
	return f.suggestions, nil
}

func (f *fakeAgentStore) GetQualityAgentAdoptionStatus(context.Context, int64, time.Time) (*store.QualityAgentAdoptionStatusMessage, error) {
	return f.status, nil
}

// The target summary is the model's evidence for attribution: it must carry the
// agent's adoption outcome and the pending drafts with the ids the adoption tool
// takes, not only the rule count.
func TestGetTargetSummaryCarriesAdoptionAndPendingDrafts(t *testing.T) {
	t.Parallel()

	store := &fakeAgentStore{
		status: &store.QualityAgentAdoptionStatusMessage{
			AdoptedLast24h:    5,
			FailedLast24h:     0,
			PendingDraftCount: 2,
		},
		suggestions: []*store.QualitySuggestionMessage{{
			ResourceID: "s1",
			TargetGUID: "target-1",
			TargetType: storepb.MetaType_TABLE,
			Confidence: 0.9,
			Payload: &storepb.QualitySuggestionPayload{
				Payload: &storepb.QualitySuggestionPayload_Rule{Rule: &storepb.QualityRuleDraftConfig{
					Title:      "id must be unique",
					Confidence: 0.9,
					Config: &storepb.QualityRuleConfig{
						Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "id"}},
					},
				}},
			},
		}},
	}
	tools := NewAgentTools(AgentDeps{Store: store})
	result, err := tools.GetTargetSummary(context.Background(), AgentSession{
		WorkspaceID: "ws",
		AgentID:     1,
		TargetGUID:  "target-1",
		TargetType:  storepb.MetaType_TABLE,
	})
	require.NoError(t, err)
	require.Contains(t, result, `"adoptedLast24h":5`)
	require.Contains(t, result, `"pendingDraftCount":1`)
	require.Contains(t, result, `"id":"workspaces/ws/qualitySuggestions/s1"`)
	require.Contains(t, result, `"ruleType":"UNIQUE"`)
	require.Contains(t, result, `"title":"id must be unique"`)
}

// The summary's failure block is the last 24 hours, so a failure from before that
// window must not reach the model: a stale cause rendered beside failedLast24h:0
// reads as the current one, and the model would blame the present configuration for
// a write that failed last week.
func TestGetTargetSummaryHidesAFailureOutsideTheWindow(t *testing.T) {
	t.Parallel()

	failedAt := time.Date(2026, 11, 19, 6, 4, 0, 0, time.UTC)
	tests := []struct {
		name    string
		status  *store.QualityAgentAdoptionStatusMessage
		present bool
	}{
		{
			name: "a stale failure keeps its reason out of the window",
			status: &store.QualityAgentAdoptionStatusMessage{
				FailedLast24h:     0,
				LastFailureReason: "rule creation failed: the dimension is required",
				LastFailureTime:   &failedAt,
			},
			present: false,
		},
		{
			name: "a failure inside the window carries its reason and time",
			status: &store.QualityAgentAdoptionStatusMessage{
				FailedLast24h:     1,
				LastFailureReason: "rule creation failed (invalid_argument): the dimension is required",
				LastFailureTime:   &failedAt,
			},
			present: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tools := NewAgentTools(AgentDeps{Store: &fakeAgentStore{status: test.status}})
			result, err := tools.GetTargetSummary(context.Background(), AgentSession{
				WorkspaceID: "ws",
				AgentID:     1,
				TargetGUID:  "target-1",
				TargetType:  storepb.MetaType_TABLE,
			})
			require.NoError(t, err)
			require.Contains(t, result, `"failedLast24h":`)
			if test.present {
				require.Contains(t, result, `"lastFailureReason":"rule creation failed (invalid_argument): the dimension is required"`)
				require.Contains(t, result, `"lastFailureTime":"2026-11-19T06:04:00Z"`)
				return
			}
			require.NotContains(t, result, "lastFailureReason")
			require.NotContains(t, result, "lastFailureTime")
		})
	}
}

// The evidence tools are adapters over the application-layer read: they must pass the
// agent principal and the session target through, and marshal what the read returns.
// The projection itself is the application layer's job (and is tested there).
func TestEvidenceToolsReadThroughTheApplicationLayer(t *testing.T) {
	t.Parallel()

	evidence := &fakeAgentEvidence{
		executions: []dataquality.ExecutionEvidence{{
			Execution: "qualityExecutions/4339",
			Rule:      "qualityRules/r460",
			Status:    "ERROR",
			ErrorCode: "BAD_VALUE",
		}},
		check: &dataquality.CheckEvidence{
			Target: "target-1",
			Rule: dataquality.CheckDefinition{
				Name:     "qualityRules/r460",
				Type:     "RANGE",
				Revision: 2,
				Config:   `{"range":{"column":"performed_at","min":"now - 1 day"}}`,
			},
			Diagnosis: &dataquality.Diagnosis{
				Class:      dataquality.ClassConfigValueInvalid,
				Field:      "range.min",
				Repairable: true,
				Actions:    []dataquality.RepairAction{dataquality.ActionReviseField, dataquality.ActionProposeRevision, dataquality.ActionEscalate},
			},
			AllowedActions: []dataquality.RepairAction{dataquality.ActionProposeRevision, dataquality.ActionEscalate},
		},
	}
	tools := NewAgentTools(AgentDeps{Store: &fakeAgentStore{}, Evidence: evidence})
	session := AgentSession{
		WorkspaceID:      "ws",
		AgentID:          1,
		AgentPrincipalID: 110,
		TargetGUID:       "target-1",
		TargetType:       storepb.MetaType_TABLE,
	}

	executions, err := tools.ListRecentExecutions(context.Background(), session)
	require.NoError(t, err)
	require.Contains(t, executions, `"execution":"qualityExecutions/4339"`)
	require.Contains(t, executions, `"errorCode":"BAD_VALUE"`)
	require.Equal(t, int32(110), evidence.principal)
	require.Equal(t, "target-1", evidence.target)

	inspected, err := tools.InspectCheck(context.Background(), session, `{"rule":"qualityRules/r460","executions":3}`)
	require.NoError(t, err)
	require.Contains(t, inspected, `"name":"qualityRules/r460"`)
	require.Contains(t, inspected, `"revision":2`)
	require.Contains(t, inspected, `now - 1 day`)
	require.Contains(t, inspected, `"class":"CONFIG_VALUE_INVALID"`)
	require.Contains(t, inspected, `"field":"range.min"`)
	require.Contains(t, inspected, `"allowedActions":["PROPOSE_REVISION","ESCALATE"]`, "the dossier carries the policy-filtered list")
	require.Equal(t, int64(1), evidence.agent, "the dossier needs the agent to resolve its delegation")
	require.Equal(t, "qualityRules/r460", evidence.rule)
	require.Equal(t, 3, evidence.limit)
}

// Without the application-layer read the tool refuses instead of falling back to an
// unbounded store query.
func TestEvidenceToolsRequireTheApplicationLayer(t *testing.T) {
	t.Parallel()

	tools := NewAgentTools(AgentDeps{Store: &fakeAgentStore{}})
	_, err := tools.ListRecentExecutions(context.Background(), AgentSession{TargetGUID: "target-1"})
	require.ErrorContains(t, err, "not configured")
	_, err = tools.InspectCheck(context.Background(), AgentSession{TargetGUID: "target-1"}, "")
	require.ErrorContains(t, err, "not configured")
}

// control_check is a request, not the authority: it passes the agent principal, the case the
// model named and the action through, and reports what the application layer actually did. A
// process without the verb refuses instead of pretending.
// repair_check applies the server-computed repair: the model names the case and nothing else, and
// the result says what was written and which run will verify it.
func TestRepairCheckPassesTheCaseAndReportsTheWrite(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotCase, gotField, gotValue string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Revise: func(_ context.Context, principalID int32, caseResourceID, field, value string) (RevisionOutcome, error) {
			gotPrincipal, gotCase, gotField, gotValue = principalID, caseResourceID, field, value
			return RevisionOutcome{
				Check: "qualityRules/r460", Field: "", To: "the target's current columns",
				Revision: 4, Execution: "qualityExecutions/4451", State: "ACTED",
			}, nil
		},
	})
	result, err := tools.RepairCheck(context.Background(), AgentSession{AgentPrincipalID: 110},
		`{"case":"workspaces/ws/qualityAgentCases/c9"}`)
	require.NoError(t, err)
	require.Equal(t, int32(110), gotPrincipal)
	require.Equal(t, "workspaces/ws/qualityAgentCases/c9", gotCase)
	require.Empty(t, gotField, "the model never supplies the value of a deterministic repair")
	require.Empty(t, gotValue)
	require.Contains(t, result, `"case":"c9"`)
	require.Contains(t, result, `"revision":4`)
	require.Contains(t, result, `"execution":"qualityExecutions/4451"`)
}

// A criterion change travels through the same verb, with the field and value the model named; the
// application layer decides whether the delegation allows it and whether the replay agrees.
func TestRepairCheckCarriesACriterionChange(t *testing.T) {
	t.Parallel()

	var gotField, gotValue string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Revise: func(_ context.Context, _ int32, _, field, value string) (RevisionOutcome, error) {
			gotField, gotValue = field, value
			return RevisionOutcome{
				Check: "qualityRules/r460", Field: field, From: "now - 1 day", To: value, Revision: 5, State: "ACTED",
			}, nil
		},
	})
	result, err := tools.RepairCheck(context.Background(), AgentSession{AgentPrincipalID: 110},
		`{"case":"c9","field":"range.min","value":"2024-01-01T00:00:00Z"}`)
	require.NoError(t, err)
	require.Equal(t, "range.min", gotField)
	require.Equal(t, "2024-01-01T00:00:00Z", gotValue)
	require.Contains(t, result, `"from":"now - 1 day"`)

	_, err = tools.RepairCheck(context.Background(), AgentSession{}, `{"case":"c9","field":"range.min"}`)
	require.ErrorContains(t, err, "needs a value")
}

func TestRepairCheckRequiresTheVerbAndACase(t *testing.T) {
	t.Parallel()

	_, err := NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).RepairCheck(context.Background(), AgentSession{}, `{"case":"c9"}`)
	require.ErrorContains(t, err, "not configured")

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).RepairCheck(context.Background(), AgentSession{}, `{}`)
	require.ErrorContains(t, err, "needs a case")
}

// propose_revision is a request for a person's decision: it passes the agent principal, the case
// and the change through, and reports the state the application layer actually reached. A process
// without the verb refuses instead of reporting a change nobody recorded.
func TestProposeRevisionPassesTheRequestThrough(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotCase, gotField, gotValue, gotRationale string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Propose: func(_ context.Context, principalID int32, caseResourceID, field, value, rationale string) (ProposalOutcome, error) {
			gotPrincipal, gotCase, gotField, gotValue, gotRationale = principalID, caseResourceID, field, value, rationale
			return ProposalOutcome{
				Check: "qualityRules/r460", Field: field, Value: value,
				Effect: "CRITERION", State: "ESCALATED",
			}, nil
		},
	})
	result, err := tools.ProposeRevision(context.Background(), AgentSession{AgentPrincipalID: 110},
		`{"case":"workspaces/ws/qualityAgentCases/c9","field":"range.min","value":"2024-01-01T00:00:00Z","rationale":"the bound never binds"}`)
	require.NoError(t, err)
	require.Equal(t, int32(110), gotPrincipal)
	require.Equal(t, "workspaces/ws/qualityAgentCases/c9", gotCase)
	require.Equal(t, "range.min", gotField)
	require.Equal(t, "2024-01-01T00:00:00Z", gotValue)
	require.Equal(t, "the bound never binds", gotRationale)
	require.Contains(t, result, `"case":"c9"`)
	require.Contains(t, result, `"effect":"CRITERION"`)
	require.Contains(t, result, `"state":"ESCALATED"`)
	require.Contains(t, result, "nothing has been written")
}

func TestProposeRevisionRequiresTheVerbAndItsArguments(t *testing.T) {
	t.Parallel()

	tools := NewAgentTools(AgentDeps{Store: &fakeAgentStore{}})
	_, err := tools.ProposeRevision(context.Background(), AgentSession{}, `{"case":"c9","field":"range.min","value":"x"}`)
	require.ErrorContains(t, err, "not configured")

	_, err = tools.ProposeRevision(context.Background(), AgentSession{}, `{"case":"c9","field":"range.min"}`)
	require.ErrorContains(t, err, "needs a case, a field and a value")

	_, err = tools.ProposeRevision(context.Background(), AgentSession{}, `{"field":"range.min","value":"x"}`)
	require.ErrorContains(t, err, "needs a case, a field and a value")
}

func TestControlCheckPassesTheRequestThrough(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotCase, gotAction string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Control: func(_ context.Context, principalID int32, caseResourceID, action string) (ControlOutcome, error) {
			gotPrincipal, gotCase, gotAction = principalID, caseResourceID, action
			return ControlOutcome{
				Check: "qualityRules/r460", State: "PAUSED", Revision: 3,
				Reason: "the agent paused the check because it cannot run",
			}, nil
		},
	})
	result, err := tools.ControlCheck(context.Background(), AgentSession{
		AgentPrincipalID: 110,
		TargetGUID:       "target-1",
	}, `{"case":"workspaces/ws/qualityAgentCases/c9","action":"PAUSE"}`)
	require.NoError(t, err)
	require.Equal(t, int32(110), gotPrincipal)
	require.Equal(t, "workspaces/ws/qualityAgentCases/c9", gotCase)
	require.Equal(t, "PAUSE", gotAction)
	require.Contains(t, result, `"state":"PAUSED"`)
	require.Contains(t, result, `"revision":3`)
	require.Contains(t, result, `"case":"c9"`)
}

func TestControlCheckRequiresTheVerbAndItsArguments(t *testing.T) {
	t.Parallel()

	tools := NewAgentTools(AgentDeps{Store: &fakeAgentStore{}})
	_, err := tools.ControlCheck(context.Background(), AgentSession{}, `{"case":"c9","action":"PAUSE"}`)
	require.ErrorContains(t, err, "not configured")

	_, err = tools.ControlCheck(context.Background(), AgentSession{}, `{"action":"PAUSE"}`)
	require.ErrorContains(t, err, "needs a case and an action")
}

// run_check is a request: it passes the agent principal and the case through and reports the
// execution the runner will pick up. Without the verb it refuses rather than doing nothing.
func TestRunCheckPassesTheRequestThrough(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotCase string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Run: func(_ context.Context, principalID int32, caseResourceID string) (RunOutcome, error) {
			gotPrincipal, gotCase = principalID, caseResourceID
			return RunOutcome{Check: "qualityRules/r460", Execution: "qualityExecutions/4451", State: "ACTED"}, nil
		},
	})
	result, err := tools.RunCheck(context.Background(), AgentSession{AgentPrincipalID: 110}, `{"case":"c9"}`)
	require.NoError(t, err)
	require.Equal(t, int32(110), gotPrincipal)
	require.Equal(t, "c9", gotCase)
	require.Contains(t, result, `"execution":"qualityExecutions/4451"`)
	require.Contains(t, result, `"state":"ACTED"`)

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).RunCheck(context.Background(), AgentSession{}, `{"case":"c9"}`)
	require.ErrorContains(t, err, "not configured")
}

// flag_proposal carries the agent's objection to its own draft through to the application layer,
// which is the only thing the adoption gate can read: the gate judges a draft's shape, not whether
// the model that drafted it believes it.
func TestFlagProposalPassesTheConcernThrough(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotDraft, gotConcern string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Flag: func(_ context.Context, principalID int32, draftResourceID, concern string) (FlagOutcome, error) {
			gotPrincipal, gotDraft, gotConcern = principalID, draftResourceID, concern
			return FlagOutcome{
				Draft: "qualitySuggestions/s123", Concern: concern, State: "PENDING",
			}, nil
		},
	})
	result, err := tools.FlagProposal(context.Background(), AgentSession{AgentPrincipalID: 110},
		`{"draft":"workspaces/ws/qualitySuggestions/s123","concern":"the audit table appends out of order"}`)
	require.NoError(t, err)
	require.Equal(t, int32(110), gotPrincipal)
	require.Equal(t, "workspaces/ws/qualitySuggestions/s123", gotDraft)
	require.Equal(t, "the audit table appends out of order", gotConcern)
	require.Contains(t, result, `"draft":"qualitySuggestions/s123"`)
	require.Contains(t, result, "never adopted automatically")

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).FlagProposal(context.Background(), AgentSession{},
		`{"draft":"s1","concern":"x"}`)
	require.ErrorContains(t, err, "not configured")

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).FlagProposal(context.Background(), AgentSession{},
		`{"draft":"s1"}`)
	require.ErrorContains(t, err, "needs a draft and a concern")
}

// withdraw_draft is the author's exit from the review queue: it takes the draft and the reason to the
// application layer and reports the queue state. It is not a rejection, so a correct retraction must
// say so instead of borrowing the decision vocabulary.
func TestWithdrawDraftPassesTheRetractionThrough(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotDraft, gotReason string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Withdraw: func(_ context.Context, principalID int32, draftResourceID, reason string) (WithdrawOutcome, error) {
			gotPrincipal, gotDraft, gotReason = principalID, draftResourceID, reason
			return WithdrawOutcome{Draft: "qualitySuggestions/s123", Reason: reason, State: "WITHDRAWN"}, nil
		},
	})
	result, err := tools.WithdrawDraft(context.Background(), AgentSession{AgentPrincipalID: 110},
		`{"draft":"qualitySuggestions/s123","reason":"the table is emptied by the ETL every night"}`)
	require.NoError(t, err)
	require.Equal(t, int32(110), gotPrincipal)
	require.Equal(t, "qualitySuggestions/s123", gotDraft)
	require.Equal(t, "the table is emptied by the ETL every night", gotReason)
	require.Contains(t, result, `"state":"WITHDRAWN"`)
	require.Contains(t, result, "a corrected draft may be proposed again")

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).WithdrawDraft(context.Background(), AgentSession{},
		`{"draft":"s1","reason":"x"}`)
	require.ErrorContains(t, err, "not configured")

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).WithdrawDraft(context.Background(), AgentSession{},
		`{"draft":"s1"}`)
	require.ErrorContains(t, err, "needs a draft and a reason")
}

// escalate_case is the model's exit for a decision it may not make: it carries the work item and the
// question to the application layer, which turns it into an escalated case and an owner attention.
func TestEscalateCasePassesTheQuestionThrough(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotCase, gotReason string
	tools := NewAgentTools(AgentDeps{
		Store: &fakeAgentStore{},
		Escalate: func(_ context.Context, principalID int32, caseResourceID, reason string) (EscalationOutcome, error) {
			gotPrincipal, gotCase, gotReason = principalID, caseResourceID, reason
			return EscalationOutcome{Check: "qualityRules/r460", State: "ESCALATED", Reason: reason}, nil
		},
	})
	result, err := tools.EscalateCase(context.Background(), AgentSession{AgentPrincipalID: 110},
		`{"case":"workspaces/ws/qualityAgentCases/c9","reason":"the bound is a business decision"}`)
	require.NoError(t, err)
	require.Equal(t, int32(110), gotPrincipal)
	require.Equal(t, "workspaces/ws/qualityAgentCases/c9", gotCase)
	require.Equal(t, "the bound is a business decision", gotReason)
	require.Contains(t, result, `"case":"c9"`)
	require.Contains(t, result, `"state":"ESCALATED"`)
	require.Contains(t, result, "owes this decision")

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).EscalateCase(context.Background(), AgentSession{},
		`{"case":"c9","reason":"x"}`)
	require.ErrorContains(t, err, "not configured")

	_, err = NewAgentTools(AgentDeps{Store: &fakeAgentStore{}}).EscalateCase(context.Background(), AgentSession{},
		`{"case":"c9"}`)
	require.ErrorContains(t, err, "needs a case and a reason")
}
