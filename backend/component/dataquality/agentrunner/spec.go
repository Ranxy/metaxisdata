package agentrunner

import (
	"context"
	"errors"
	"fmt"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// toolSpec is one static declaration of a model-wake tool. The declaration is
// the single source of the schema the model sees; the wake binds an execution
// body to it.
type toolSpec struct {
	name        string
	description string
	parameters  any
	meta        tool.Meta
}

// readMeta is the metadata of a tool that reads the registry.
func readMeta() tool.Meta {
	return tool.Meta{
		Group:          "quality",
		Scope:          tool.ScopeProductOnly,
		Authz:          tool.AuthzPermission,
		ReadOnly:       true,
		Permission:     string(permission.DataQualityRulesList),
		MaxResultBytes: maxToolResultBytes,
		// Registry reads are independent of each other and of session state.
		ParallelSafe: true,
	}
}

// evidenceMeta is the metadata of a tool that reads a target's evidence through the
// application layer. The permission is rules.evidence, not rules.list: the latter is
// in the member baseline, which grants it to every principal on every target, so it
// cannot express "this agent, these databases".
func evidenceMeta() tool.Meta {
	meta := readMeta()
	meta.Permission = string(permission.DataQualityRulesEvidence)
	return meta
}

// repairMeta is the metadata of a tool that changes one check through the application layer. It
// is a publish-class write: the pipeline audits it, and the permission is rules.repair rather
// than rules.update, so the catalog reads as "this actor may not rewrite an arbitrary
// definition".
func repairMeta() tool.Meta {
	return tool.Meta{
		Group:          "quality",
		Scope:          tool.ScopeProductOnly,
		Authz:          tool.AuthzPermission,
		SideEffect:     tool.SideEffectPublish,
		Permission:     string(permission.DataQualityRulesRepair),
		MaxResultBytes: maxToolResultBytes,
	}
}

// proposeMeta is the metadata of a tool that records a proposal or evidence
// through an RPC handler.
func proposeMeta() tool.Meta {
	return tool.Meta{
		Group:          "quality",
		Scope:          tool.ScopeProductOnly,
		Authz:          tool.AuthzPermission,
		SideEffect:     tool.SideEffectPropose,
		Permission:     string(permission.DataQualityPlansUpdate),
		MaxResultBytes: maxToolResultBytes,
	}
}

// internalMeta is the metadata of a wake-control tool. It belongs to this product
// only and is scoped by session ownership, not by a workspace permission.
func internalMeta() tool.Meta {
	return tool.Meta{
		Group: "agent",
		Scope: tool.ScopeProductOnly,
		Authz: tool.AuthzInternal,
	}
}

// targetToolSpecs is the base tool set of a target work session. Capability
// profiles add the tools a selected capability grants; the base set itself is
// read-, propose- and wake-control only.
func targetToolSpecs() []toolSpec {
	specs := []toolSpec{
		{
			name:        "get_target_summary",
			description: "Read the target's plan state, criticality, rule count, adoption status, pending drafts and uncovered declared properties.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name:        "list_recent_executions",
			description: "List the target's recent executions with the check they belong to, their outcome, failure code and message, and row counts.",
			parameters:  emptyParameters(),
			meta:        evidenceMeta(),
		},
		{
			name: "inspect_check",
			description: "Read one check's definition — type, revision, severity and the rest of its config — together with that check's own recent executions and failure evidence. " +
				"Naming no check inspects the newest failing one.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"rule": map[string]any{
					"type":        "string",
					"description": "optional check resource name; omit to inspect the newest failing check",
				},
				"executions": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     20,
					"description": "how many of the check's executions to include, at most 20",
				},
			}},
			meta: evidenceMeta(),
		},
		{
			name: "control_check",
			description: "Pause a check that cannot run, so its failures stop while a person decides what to do with it. " +
				"The check's case becomes escalated. You cannot archive or resume a check.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"case": map[string]any{
					"type":        "string",
					"description": "the qualityAgentCases resource name or id the work item is about",
				},
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"PAUSE"},
					"description": "the state change to apply",
				},
			}, "required": []string{"case", "action"}},
			meta: repairMeta(),
		},
		{
			name:        "get_memory",
			description: "List the target's confirmed asset facts.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name:        "record_asset_fact",
			description: "Record a deterministic observation about the target as an asset fact.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"statement": map[string]any{"type": "string"},
			}, "required": []string{"statement"}},
			meta: proposeMeta(),
		},
		{
			name:        "block_session",
			description: "Report a blocker that needs a person. The same code must persist for three consecutive wakes before the session parks.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"code":   map[string]any{"type": "string"},
				"reason": map[string]any{"type": "string"},
			}, "required": []string{"code", "reason"}},
			meta: internalMeta(),
		},
		{
			name:        "schedule_wake",
			description: "Ask for an earlier wake when there is concrete unfinished work.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"after_minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 10080},
				"reason":        map[string]any{"type": "string"},
			}, "required": []string{"after_minutes"}},
			meta: internalMeta(),
		},
	}
	return specs
}

// conversationToolSpecs is the front-office tool set: reads over the agent's scope
// and queue, plus the two wake controls. It deliberately cannot touch a single
// target's plan or checks; that work belongs to the target sessions.
func conversationToolSpecs() []toolSpec {
	return []toolSpec{
		{
			name:        "get_scope_summary",
			description: "Read the agent's scope health: databases, discovered/monitored/dormant targets and the queue split.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name:        "list_scope_targets",
			description: "List targets in the agent's scope with their queue state, mode and priority.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name:        "schedule_wake",
			description: "Ask for an earlier wake when there is concrete unfinished work.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"after_minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 10080},
				"reason":        map[string]any{"type": "string"},
			}, "required": []string{"after_minutes"}},
			meta: internalMeta(),
		},
		{
			name:        "block_session",
			description: "Report a blocker that needs the owner.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"code":   map[string]any{"type": "string"},
				"reason": map[string]any{"type": "string"},
			}, "required": []string{"code", "reason"}},
			meta: internalMeta(),
		},
	}
}

// Spec is one agent product: its name, prompt, tool whitelist and turn budget.
// The mode selects which spec a session runs, so monitor and triage differ only
// in declaration, never in engine code.
type Spec struct {
	// Name identifies the product in metrics and logs.
	Name string
	// Feature labels this product's model calls in metrics.
	Feature string
	// Prompt is the system prompt the runtime renders.
	Prompt *agent.Prompt
	// Tools is the base whitelist; capability profiles add feature tools and the
	// registry validates the composed set at wake time.
	Tools []toolSpec
	// MaxTurns bounds one wake's model calls.
	MaxTurns int
}

// The specs are process-level declarations: one prompt, one tool whitelist and one
// turn budget per product, resolved once.
var (
	// conversationProduct is the agent's front office: the owner's conversation,
	// woken only by a question or a scheduled follow-up.
	conversationProduct = &Spec{
		Name:     "quality-assistant",
		Feature:  "dq_agent",
		Prompt:   conversationPrompt(),
		Tools:    conversationToolSpecs(),
		MaxTurns: modelMaxTurns,
	}
	// monitorProduct is the long-lived monitoring product: one target, woken by
	// events, questions and a daily cadence.
	monitorProduct = &Spec{
		Name:     "quality-monitor",
		Feature:  "dq_agent",
		Prompt:   qualityPrompt(),
		Tools:    targetToolSpecs(),
		MaxTurns: modelMaxTurns,
	}
	// triageProduct is the coverage product: it inspects an uncovered target,
	// drafts checks and records what it concluded.
	triageProduct = &Spec{
		Name:     "quality-triage",
		Feature:  "dq_agent",
		Prompt:   qualityPrompt(),
		Tools:    targetToolSpecs(),
		MaxTurns: triageMaxTurns,
	}
)

// monitorSpec returns the monitoring product.
func monitorSpec() *Spec { return monitorProduct }

// triageSpec returns the coverage product.
func triageSpec() *Spec { return triageProduct }

// specForSession selects the product a session runs. A conversation session has no
// target; target sessions pick monitor or triage from their mode.
func specForSession(session *store.QualityAgentSessionMessage) *Spec {
	if session != nil && session.Kind == 2 {
		return conversationProduct
	}
	return specFor(session.Mode)
}

// specFor selects the product a target session runs.
func specFor(mode storepb.QualityAgentSessionMode) *Spec {
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE {
		return triageSpec()
	}
	return monitorSpec()
}

// ValidateSpecs checks the product declarations at startup: every spec's tools
// register, the whitelist resolves, and the engine has an execution branch for
// each name. The server calls it before constructing the runner, so a renamed or
// misspelled tool fails the boot instead of reaching the model.
func ValidateSpecs() error {
	if err := validateCapabilityProfiles(); err != nil {
		return err
	}
	for _, spec := range []*Spec{monitorProduct, triageProduct, conversationProduct} {
		declarations := append([]toolSpec(nil), spec.Tools...)
		for _, profile := range capabilityProfiles() {
			declarations = append(declarations, profile.Tools(spec)...)
		}
		registry := tool.NewRegistry()
		seen := make(map[string]bool, len(declarations))
		for _, declaration := range declarations {
			if seen[declaration.name] {
				return fmt.Errorf("spec %s declares tool %q twice", spec.Name, declaration.name)
			}
			seen[declaration.name] = true
			declarationTool := tool.Tool{
				Name:        declaration.name,
				Description: declaration.description,
				Parameters:  declaration.parameters,
				Meta:        declaration.meta,
				Execute: func(context.Context, tool.Call, *tool.Invocation) (tool.Result, error) {
					return tool.Result{}, errors.New("the declaration is not executable")
				},
			}
			if err := registry.Register(declarationTool); err != nil {
				return fmt.Errorf("spec %s: %w", spec.Name, err)
			}
			if !handlesTool(declaration.name) {
				return fmt.Errorf("spec %s declares tool %q but the engine has no execution branch", spec.Name, declaration.name)
			}
			if err := validateToolPermission(declaration); err != nil {
				return fmt.Errorf("spec %s: %w", spec.Name, err)
			}
		}
		if _, err := registry.Resolve(toolSpecNames(declarations)); err != nil {
			return fmt.Errorf("spec %s: %w", spec.Name, err)
		}
	}
	return nil
}

// validateToolPermission checks that a tool's declared permission is one the agent
// principal can actually hold. A tool gated by a permission no role grants would fail at
// call time for every agent, which is a declaration bug worth a boot error rather than a
// wake-time surprise.
//
// "Holdable" means the member baseline or the quality agent role grants it. The baseline
// counts because Manager.CheckPermission short-circuits on it for every principal, which is
// also why a baseline permission cannot scope a read to one database.
func validateToolPermission(declaration toolSpec) error {
	if declaration.meta.Authz != tool.AuthzPermission {
		return nil
	}
	perm := permission.Permission(declaration.meta.Permission)
	if perm == "" {
		return fmt.Errorf("tool %q declares no permission", declaration.name)
	}
	for _, roleID := range []string{store.WorkspaceMemberRole, store.QualityAgentRole} {
		if role := store.GetPredefinedRole(roleID); role != nil && role.Permissions[perm] {
			return nil
		}
	}
	return fmt.Errorf("tool %q declares %q, which neither the member baseline nor the quality agent role grants",
		declaration.name, perm)
}

// toolSpecNames lists a declaration slice's tool names in order.
func toolSpecNames(declarations []toolSpec) []string {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.name)
	}
	return names
}

// specNames lists a spec's tool names in declaration order.
func specNames(declarations []toolSpec) []string {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.name)
	}
	return names
}
