package agentattention

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeStore is the slice of the store one escalation writes, with every call recorded so a test can
// assert what the owner would see rather than how it was produced.
type fakeStore struct {
	user     *store.UserMessage
	sessions []*store.QualityAgentSessionMessage
	cases    []*store.QualityAgentCaseMessage

	created     []*store.QualityAgentAttentionMessage
	createdFlag bool
	refreshed   []refreshCall
	resolved    []string
}

type refreshCall struct {
	dedupeKey string
	question  string
	caseID    string
}

func (f *fakeStore) GetUserByID(context.Context, int) (*store.UserMessage, error) {
	return f.user, nil
}

func (f *fakeStore) ListQualityAgentSessionsByTarget(context.Context, string, string) ([]*store.QualityAgentSessionMessage, error) {
	return f.sessions, nil
}

func (f *fakeStore) ListQualityAgentCases(_ context.Context, find *store.FindQualityAgentCaseMessage) ([]*store.QualityAgentCaseMessage, error) {
	listed := make([]*store.QualityAgentCaseMessage, 0, len(f.cases))
	for _, openCase := range f.cases {
		if find.TargetGUID != nil && openCase.TargetGUID != *find.TargetGUID {
			continue
		}
		wanted := false
		for _, state := range find.States {
			if openCase.State == state {
				wanted = true
			}
		}
		if wanted {
			listed = append(listed, openCase)
		}
	}
	return listed, nil
}

func (f *fakeStore) CreateQualityAgentAttention(_ context.Context, message *store.QualityAgentAttentionMessage) (*store.QualityAgentAttentionMessage, bool, error) {
	f.created = append(f.created, message)
	return message, f.createdFlag, nil
}

func (f *fakeStore) RefreshOpenQualityAgentAttention(_ context.Context, _ int64, dedupeKey, question, caseResourceID string) error {
	f.refreshed = append(f.refreshed, refreshCall{dedupeKey: dedupeKey, question: question, caseID: caseResourceID})
	return nil
}

func (f *fakeStore) ResolveOpenQualityAgentAttentionByDedupe(_ context.Context, _ int64, dedupeKey string, _ time.Time) (bool, error) {
	f.resolved = append(f.resolved, dedupeKey)
	return true, nil
}

// fakeNotifier records the doorbell, which must ring once per card and never on a refresh.
type fakeNotifier struct {
	owner int
}

func (f *fakeNotifier) SendQualityAgentAttention(context.Context, int32, string, *storepb.QualityAgentAttentionDetail) error {
	f.owner++
	return nil
}

func (*fakeNotifier) SendQualityAgentAttentionToAdmins(context.Context, string, *storepb.QualityAgentAttentionDetail) error {
	return nil
}

func attentionTestCase(resourceID, targetGUID, summary string, state store.QualityAgentCaseState) *store.QualityAgentCaseMessage {
	return &store.QualityAgentCaseMessage{
		ResourceID:   resourceID,
		WorkspaceID:  "ws",
		AgentID:      101,
		TargetGUID:   targetGUID,
		State:        state,
		FailureClass: "CONFIG_UNSUPPORTED",
		Summary:      summary,
	}
}

func attentionTestAgent() *store.QualityAgentMessage {
	owner := int32(7)
	return &store.QualityAgentMessage{
		ID: 101, ResourceID: "kMeQcxhsSl8p", Language: "zh-CN", OwnerPrincipalID: &owner,
	}
}

// One target is one question. Nine checks silenced by one empty table reached the owner as nine
// cards because the dedupe key was the case; the card now lists them and dedupes on the target.
func TestRaiseAggregatesATargetIntoOneCard(t *testing.T) {
	t.Parallel()

	other := "inst;app;public;orders"
	newest := attentionTestCase("c1", "inst;app;public;stock_alert", "check \"a\" (qualityRules/r1): cannot run", store.QualityAgentCaseStateEscalated)
	stores := &fakeStore{user: &store.UserMessage{ID: 7}, createdFlag: true, cases: []*store.QualityAgentCaseMessage{
		newest,
		attentionTestCase("c2", "inst;app;public;stock_alert", "check \"b\" (qualityRules/r2): cannot run", store.QualityAgentCaseStateEscalated),
		attentionTestCase("c3", "inst;app;public;stock_alert", "check \"c\" (qualityRules/r3): gave up", store.QualityAgentCaseStateFrozen),
		attentionTestCase("c4", other, "another target's work", store.QualityAgentCaseStateEscalated),
	}}
	notifier := &fakeNotifier{}
	raiser := &Raiser{Store: stores, Notifier: notifier}

	raiser.Raise(context.Background(), newest, attentionTestAgent(), Notice{
		Kind:     store.QualityAgentAttentionKindBlocked,
		Question: "检查「a」：引擎拒绝了这个定义。",
	})

	require.Len(t, stores.created, 1)
	card := stores.created[0]
	require.Equal(t, "target:inst;app;public;stock_alert", card.DedupeKey,
		"one card per target, not per work item")
	require.Contains(t, card.Question, "检查「a」")
	require.Contains(t, card.Question, "同一目标上还在等你决定",
		"the card lists the target's other waiting items")
	require.Contains(t, card.Question, "qualityRules/r2")
	require.Contains(t, card.Question, "qualityRules/r3")
	require.NotContains(t, card.Question, "another target's work",
		"a card answers for one target only")
	require.Equal(t, "c1", card.CaseResourceID, "the card points at the work item it was raised for")
	require.Equal(t, 1, notifier.owner, "a new card rings the doorbell once")
}

// A second escalation on the same target refreshes what the card says instead of paging again: the
// owner already has the item, and two pages for one target is the noise this aggregation removes.
func TestRaiseRefreshesAnOpenCardWithoutPagingAgain(t *testing.T) {
	t.Parallel()

	newest := attentionTestCase("c9", "inst;app;public;stock_alert", "check \"z\" (qualityRules/r9): cannot run", store.QualityAgentCaseStateEscalated)
	stores := &fakeStore{
		user:        &store.UserMessage{ID: 7},
		cases:       []*store.QualityAgentCaseMessage{newest},
		createdFlag: false,
	}
	notifier := &fakeNotifier{}
	raiser := &Raiser{Store: stores, Notifier: notifier}

	raiser.Raise(context.Background(), newest, attentionTestAgent(), Notice{
		Kind:     store.QualityAgentAttentionKindBlocked,
		Question: "检查「z」：引擎拒绝了这个定义。",
	})

	require.Empty(t, notifier.owner, "an existing card must not ring the doorbell twice")
	require.Len(t, stores.refreshed, 1)
	require.Equal(t, "target:inst;app;public;stock_alert", stores.refreshed[0].dedupeKey)
	require.Contains(t, stores.refreshed[0].question, "检查「z」")
	require.Equal(t, "c9", stores.refreshed[0].caseID)
}

// A card outlives nothing: once the last work item on a target settles, the card closes, and while
// work remains it is rewritten so it cannot list an item that is already closed.
func TestRefreshClosesOrRewritesTheCardFromLiveWork(t *testing.T) {
	t.Parallel()

	settled := &fakeStore{}
	raiser := &Raiser{Store: settled}
	raiser.Refresh(context.Background(), attentionTestAgent(), "inst;app;public;stock_alert")
	require.Equal(t, []string{"target:inst;app;public;stock_alert"}, settled.resolved,
		"no waiting work means the card closes")

	waiting := &fakeStore{cases: []*store.QualityAgentCaseMessage{
		attentionTestCase("c1", "inst;app;public;stock_alert", "check \"a\": cannot run", store.QualityAgentCaseStateEscalated),
		attentionTestCase("c2", "inst;app;public;stock_alert", "check \"b\": gave up", store.QualityAgentCaseStateFrozen),
	}}
	raiser = &Raiser{Store: waiting}
	raiser.Refresh(context.Background(), attentionTestAgent(), "inst;app;public;stock_alert")
	require.Empty(t, waiting.resolved, "work still waits, so the card stays open")
	require.Len(t, waiting.refreshed, 1)
	require.Contains(t, waiting.refreshed[0].question, "check \"a\"")
	require.Contains(t, waiting.refreshed[0].question, "check \"b\"")
}
