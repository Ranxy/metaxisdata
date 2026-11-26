// Package agentattention tells a quality agent's owner that a work item needs a decision.
//
// It is one package rather than a method on the API service because two writers escalate a case: a
// verb the model calls and the runner that settles a batch. The parts that must not drift are the
// dedupe key (one open item per case, however often the case is escalated), which session the item
// hangs off, who is told when the agent has no owner, and the owner's language — so they live here,
// and both callers pass the same notice.
package agentattention

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Store is the slice of the store an escalation writes.
type Store interface {
	GetUserByID(ctx context.Context, id int) (*store.UserMessage, error)
	ListQualityAgentSessionsByTarget(ctx context.Context, workspaceID, targetGUID string) ([]*store.QualityAgentSessionMessage, error)
	CreateQualityAgentAttention(ctx context.Context, message *store.QualityAgentAttentionMessage) (*store.QualityAgentAttentionMessage, bool, error)
}

// Notifier delivers the doorbell behind the durable item. A nil notifier leaves the item in the
// inbox without an outbound message, which is what a deployment without channels wants.
type Notifier interface {
	SendQualityAgentAttention(ctx context.Context, userID int32, dedupeKey string, detail *storepb.QualityAgentAttentionDetail) error
	SendQualityAgentAttentionToAdmins(ctx context.Context, dedupeKey string, detail *storepb.QualityAgentAttentionDetail) error
}

// Notice is what one writer tells the owner about its case. It is a value rather than a set of
// parameters because a notice is one thing: the kind picks the headline, the patch says what to
// change, and the question says why.
type Notice struct {
	Kind store.QualityAgentAttentionKind
	// Title overrides the kind's default headline. A proposal that asks for one concrete value is a
	// different thing from a queue of drafts, though both are pending decisions.
	Title string
	// Question is the body, in the owner's language.
	Question string
	// Patch is the change the owner is asked to make, when there is exactly one, so the owner
	// reviews a field and a value instead of a summary of them.
	Patch *storepb.QualityAgentFieldPatch
}

// Raiser writes one escalation. It is stateless apart from its clock, so both callers can build one
// per request.
type Raiser struct {
	Store    Store
	Notifier Notifier
	Now      func() time.Time
}

// Raise records the notice and rings the doorbell. It is best effort: a failed notice never undoes
// the write it reports, and the case row stays the durable record.
func (r *Raiser) Raise(ctx context.Context, openCase *store.QualityAgentCaseMessage, agent *store.QualityAgentMessage, notice Notice) {
	if r == nil || r.Store == nil || openCase == nil || agent == nil {
		return
	}
	language, owner := OwnerLanguage(ctx, r.Store, agent)
	title := notice.Title
	if title == "" {
		title = agentlang.AttentionTitle(language, notice.Kind)
	}
	// Link the target session when there is one, so the card carries the target; an unlinked item is
	// still an agent-level notice rather than a lost one.
	var sessionID *int64
	if sessions, err := r.Store.ListQualityAgentSessionsByTarget(ctx, openCase.WorkspaceID, openCase.TargetGUID); err == nil {
		for _, session := range sessions {
			if session.AgentID == agent.ID {
				sessionID = &session.ID
				break
			}
		}
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	attention, created, err := r.Store.CreateQualityAgentAttention(ctx, &store.QualityAgentAttentionMessage{
		WorkspaceID:    openCase.WorkspaceID,
		AgentID:        agent.ID,
		Kind:           notice.Kind,
		SessionID:      sessionID,
		Title:          title,
		Question:       common.TruncateUTF8Bytes(notice.Question, 2000),
		DedupeKey:      DedupeKey(openCase.ResourceID),
		CaseResourceID: openCase.ResourceID,
		SuggestedPatch: notice.Patch,
		CreatedAt:      now,
	})
	if err != nil || !created {
		return
	}
	if r.Notifier == nil {
		return
	}
	detail := &storepb.QualityAgentAttentionDetail{
		Agent:     common.FormatWorkspace(openCase.WorkspaceID) + "/qualityAgents/" + agent.ResourceID,
		Attention: common.FormatWorkspace(openCase.WorkspaceID) + "/qualityAgentAttentions/" + attention.ResourceID,
		Kind:      int32(notice.Kind),
		Title:     attention.Title,
		Question:  attention.Question,
	}
	dedupeKey := "dqagent:" + attention.ResourceID
	if owner != nil {
		if sendErr := r.Notifier.SendQualityAgentAttention(ctx, int32(owner.ID), dedupeKey, detail); sendErr != nil {
			slog.Warn("failed to send a case attention notification", log.WithError(sendErr))
		}
		return
	}
	if sendErr := r.Notifier.SendQualityAgentAttentionToAdmins(ctx, dedupeKey, detail); sendErr != nil {
		slog.Warn("failed to send a case attention notification to admins", log.WithError(sendErr))
	}
}

// DedupeKey is the one key an escalated case uses, whichever writer escalated it: a case that is
// escalated twice must not page twice, and the open item already tells the owner what to decide.
func DedupeKey(caseResourceID string) string {
	return "case:" + caseResourceID
}

// OwnerLanguage resolves the language the agent's prose is written in for its owner. A caller
// that renders a question before raising it reads the same decision this package uses for the
// headline, so a notice never mixes the two languages.
func OwnerLanguage(ctx context.Context, messages Store, agent *store.QualityAgentMessage) (string, *store.UserMessage) {
	var owner *store.UserMessage
	if messages != nil && agent != nil && agent.OwnerPrincipalID != nil {
		owner, _ = messages.GetUserByID(ctx, int(*agent.OwnerPrincipalID))
	}
	return agentlang.ForAgent(agent, owner), owner
}
