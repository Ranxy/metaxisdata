package agentrunner

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// capabilityProfile is one capability's contribution to a product's prompt and
// tool set. The profile is product policy: it owns the model-facing wording and
// the declarations of the tools the capability grants. The domain catalog
// (agentcapability) deliberately stays data-only.
type capabilityProfile struct {
	// ID is the catalog capability id this profile implements.
	ID string
	// Prompt returns the sections this capability contributes to one product. A
	// capability that does not apply to a product returns nil.
	Prompt func(spec *Spec) []agent.PromptSection
	// Tools returns the tool declarations this capability grants to one product.
	// It may be nil: patrol, for example, has no exclusive tool today.
	Tools func(spec *Spec) []toolSpec
}

// capabilityProfiles is the canonical order capabilities contribute in. Keeping
// this order fixed keeps a rendered system prompt and tool list byte-stable while
// the selected capability set is unchanged, so provider prefix caching survives
// between wakes.
func capabilityProfiles() []capabilityProfile {
	return []capabilityProfile{
		suggestProfile(),
		adoptProfile(),
		patrolProfile(),
	}
}

// profileFor returns the profile of one capability id.
func profileFor(id string) (capabilityProfile, bool) {
	for _, profile := range capabilityProfiles() {
		if profile.ID == id {
			return profile, true
		}
	}
	return capabilityProfile{}, false
}

// selectedCapabilities lists the profiles an agent selected, in canonical order.
func selectedCapabilities(agent *store.QualityAgentMessage) []capabilityProfile {
	if agent == nil {
		return nil
	}
	selected := make([]capabilityProfile, 0, len(agent.Capabilities))
	for _, profile := range capabilityProfiles() {
		if slices.Contains(agent.Capabilities, profile.ID) {
			selected = append(selected, profile)
		}
	}
	return selected
}

// toolsFor composes a product's base tools with the tools its selected
// capabilities grant.
func toolsFor(spec *Spec, sessionAgent *store.QualityAgentMessage) []toolSpec {
	if spec == nil {
		return nil
	}
	tools := append([]toolSpec(nil), spec.Tools...)
	for _, profile := range selectedCapabilities(sessionAgent) {
		tools = append(tools, profile.Tools(spec)...)
	}
	return tools
}

// promptFor composes a product's base prompt with the sections its selected
// capabilities contribute and the current adoption policy.
func promptFor(spec *Spec, sessionAgent *store.QualityAgentMessage) *agent.Prompt {
	if spec == nil {
		return agent.NewPrompt()
	}
	sections := append([]agent.PromptSection(nil), spec.Prompt.Sections()...)
	for _, profile := range selectedCapabilities(sessionAgent) {
		// The adopt fragment claims an authority the runtime does not grant while
		// adoption is off or the agent is inactive; the policy section below states
		// that case instead.
		if profile.ID == agentcapability.Adopt && !dataquality.AgentAdoptionReady(sessionAgent) {
			continue
		}
		sections = append(sections, profile.Prompt(spec)...)
	}
	sections = append(sections, agent.PromptSection{Name: "adoption-policy", Text: adoptionPolicySection(sessionAgent, spec)})
	return agent.NewPrompt(sections...)
}

// validateCapabilityProfiles checks that every catalog capability has a profile
// and every profile names a catalog capability. A capability without a profile
// would silently grant no prompt and no tools, so it is a boot error.
func validateCapabilityProfiles() error {
	catalog := agentcapability.All()
	for _, capability := range catalog {
		if _, ok := profileFor(capability.ID); !ok {
			return fmt.Errorf("capability %q has no product profile", capability.ID)
		}
	}
	for _, profile := range capabilityProfiles() {
		if _, ok := agentcapability.Lookup(profile.ID); !ok {
			return fmt.Errorf("capability profile %q is not in the catalog", profile.ID)
		}
	}
	return nil
}

// suggestProfile is the coverage capability: it drafts checks wherever the agent needs a new
// or a replacement check, and explains the pending queue in the owner conversation. The tools
// follow the capability, not the session mode: a monitor wake that discovers its check cannot
// express the intent must be able to draft the one that can, and the same target may be
// triaged and monitored in the same session.
func suggestProfile() capabilityProfile {
	return capabilityProfile{
		ID: agentcapability.Suggest,
		Prompt: func(spec *Spec) []agent.PromptSection {
			switch spec.Name {
			case triageProduct.Name:
				return []agent.PromptSection{{Name: "capability-suggest", Text: strings.Join([]string{
					"Coverage work:",
					"- Read the target's schema, profiles, labels and lineage consumers with get_table_context before drafting.",
					"- Draft concrete checks with draft_checks. A draft is a proposal, never a running check.",
					"- Do not repeat work an earlier triage already recorded.",
					"- The Adoption section decides whether a draft is created automatically; the owner can also ask you to adopt pending drafts.",
				}, "\n")}}
			case monitorProduct.Name:
				return []agent.PromptSection{{Name: "capability-suggest", Text: strings.Join([]string{
					"Maintaining coverage:",
					"- When a check cannot express the intent — a value the database refuses, the wrong check type for a rolling window — draft the check that can with draft_checks instead of forcing the broken definition to work.",
					"- Read the target's schema, profiles, labels and consumers with get_table_context before drafting.",
					"- A draft is a proposal, never a running check. The Adoption section decides whether it is created automatically; otherwise a person reviews it.",
					"- A check whose own definition cannot run — an unsupported column, a refused bound, a moved structure — may be paused with control_check once its case is diagnosed; the case is then escalated so a person decides whether to resume, archive or replace it. An empty target or a data failure is not that case, so it cannot be paused this way. You cannot archive or resume a check.",
					"- A value only a person may set — a criterion, a field someone pinned — goes to the owner with propose_revision: it validates the value and records it on the case, and the case becomes escalated. Proposing is not writing.",
				}, "\n")}}
			case conversationProduct.Name:
				return []agent.PromptSection{{Name: "capability-suggest", Text: strings.Join([]string{
					"Pending proposals:",
					"- Use list_pending_proposals to read this agent's pending check drafts with their ids, check types and confidence.",
					"- Never invent counts or titles; answer from the tool result.",
					"- A draft is created only when a person or the Adoption policy accepts it.",
					"- Withdraw a draft a subagent queued and you judge wrong with withdraw_draft and a reason. A person never decided it, so this is not a rejection.",
				}, "\n")}}
			default:
				return nil
			}
		},
		Tools: func(spec *Spec) []toolSpec {
			switch spec.Name {
			case triageProduct.Name, monitorProduct.Name:
				// Both target products draft. Which one is running is expressible from the
				// session's mode in the prompt, not by withholding the tool: a monitor wake that
				// cannot draft can only escalate a check it knows how to replace.
				return []toolSpec{getTableContextSpec(), draftChecksSpec()}
			case conversationProduct.Name:
				// The lead may retract a draft its subagents queued; a person never decided
				// it, and the queue's approval ask closes with it.
				return []toolSpec{listPendingProposalsSpec(), withdrawDraftSpec()}
			default:
				return nil
			}
		},
	}
}

// adoptProfile is the adoption capability: it hands the model the owner-requested
// adoption tool. The runtime still gates the tool on an owner question and the
// adoption configuration, so the capability alone cannot write.
func adoptProfile() capabilityProfile {
	return capabilityProfile{
		ID: agentcapability.Adopt,
		Prompt: func(spec *Spec) []agent.PromptSection {
			if spec.Name != conversationProduct.Name {
				return []agent.PromptSection{{Name: "capability-adopt", Text: strings.Join([]string{
					"Adoption authority:",
					"- You hold the adopt capability. The Adoption section below is the current policy and the only place that authorizes automatic adoption.",
					"- You do not hold a tool for owner-requested adoption; the lead handles that. Report a draft that needs a person instead.",
					"- Never claim a person approved or published a check that you created through automatic adoption.",
				}, "\n")}}
			}
			return []agent.PromptSection{{Name: "capability-adopt", Text: strings.Join([]string{
				"Adoption authority:",
				"- You hold the adopt capability. The Adoption section below is the current policy and the only place that authorizes adoption.",
				"- The tool adopt_pending_drafts exists only for an owner's explicit request in the current wake. Do not call it on a scheduled or event wake.",
				"- Never claim a person approved or published a check you created through adoption.",
			}, "\n")}}
		},
		Tools: func(spec *Spec) []toolSpec {
			if spec.Name != conversationProduct.Name {
				return nil
			}
			return []toolSpec{adoptPendingDraftsSpec()}
		},
	}
}

// patrolProfile is the monitoring capability. Reports are generated by the
// runner, so this profile contributes prompt guidance but no exclusive tool.
func patrolProfile() capabilityProfile {
	return capabilityProfile{
		ID: agentcapability.Patrol,
		Prompt: func(spec *Spec) []agent.PromptSection {
			switch spec.Name {
			case monitorProduct.Name:
				return []agent.PromptSection{{Name: "capability-patrol", Text: strings.Join([]string{
					"Monitoring:",
					"- Review recent executions; before concluding anything about a failure, read that check's definition and its own evidence with inspect_check.",
					"- Record what a person should know with record_asset_fact: one durable sentence per fact, worded so a later wake recognizes it. The reasoning behind it belongs in your message, not in the fact.",
					"- Scheduled patrol reports are generated by the runner; you do not publish them through a tool.",
				}, "\n")}}
			case conversationProduct.Name:
				return []agent.PromptSection{{Name: "capability-patrol", Text: strings.Join([]string{
					"Patrol and reports:",
					"- You monitor the covered targets in your scope; the runner generates the scheduled patrol reports and announces them in this conversation.",
					"- Answer scope questions from get_scope_summary and list_scope_targets, never from memory or guesses.",
				}, "\n")}}
			default:
				return nil
			}
		},
		Tools: func(_ *Spec) []toolSpec { return nil },
	}
}

// getTableContextSpec is the triage read tool.
func getTableContextSpec() toolSpec {
	return toolSpec{
		name:        "get_table_context",
		description: "Read the target's schema (including each enum column's declared labels), column profiles, semantic labels, lineage consumers and allowedRuleTypes (the check types this target can run); no sample rows. Read it before drafting.",
		parameters:  emptyParameters(),
		meta:        readMeta(),
	}
}

// draftChecksSpec is the triage drafting tool.
func draftChecksSpec() toolSpec {
	return toolSpec{
		name:        "draft_checks",
		description: "Draft concrete checks for the target into the review queue; a person or the adoption policy decides them before they run. Name in the intent only check types get_table_context lists as allowedRuleTypes: the draft pipeline refuses a type the target cannot run, and a refused draft is returned with its reason so a corrected intent can be retried.",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"intent": map[string]any{"type": "string", "description": "what the checks should watch, in one sentence"},
			"columns": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "optional column names; at most 8",
			},
		}, "required": []string{"intent"}},
		meta: tool.Meta{
			Group:          "quality",
			Scope:          tool.ScopeProductOnly,
			Authz:          tool.AuthzPermission,
			SideEffect:     tool.SideEffectPropose,
			Permission:     string(permission.DataQualitySuggest),
			MaxResultBytes: maxToolResultBytes,
		},
	}
}

// listPendingProposalsSpec is the owner-conversation read tool.
func listPendingProposalsSpec() toolSpec {
	return toolSpec{
		name:        "list_pending_proposals",
		description: "List this agent's pending check drafts with their ids, check types and confidence.",
		parameters:  emptyParameters(),
		meta:        readMeta(),
	}
}

// adoptPendingDraftsSpec is the owner-requested adoption tool. It is a
// publish-class write: the pipeline audits it, and the runtime exposes it only on
// an owner question when the adoption configuration is ready.
func adoptPendingDraftsSpec() toolSpec {
	return toolSpec{
		name: "adopt_pending_drafts",
		description: "Adopt pending check drafts when the owner explicitly asks. Without draft ids it applies the configured adoption guardrails; " +
			"with draft ids it accepts exactly those drafts as the owner's decision.",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"draft_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "optional draft resource names; omit to apply the configured adoption policy",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "optional target guid; required in the owner conversation when no draft ids are given",
			},
		}},
		meta: tool.Meta{
			Group:          "quality",
			Scope:          tool.ScopeProductOnly,
			Authz:          tool.AuthzPermission,
			SideEffect:     tool.SideEffectPublish,
			Permission:     string(permission.DataQualityAgentsGet),
			MaxResultBytes: maxToolResultBytes,
		},
	}
}

// emptyParameters is the JSON schema of a tool that takes no arguments.
func emptyParameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
