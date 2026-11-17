package agentrunner

import (
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
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
		Scope:          tool.ScopeCatalog,
		Authz:          tool.AuthzPermission,
		ReadOnly:       true,
		Permission:     string(permission.DataQualityRulesList),
		MaxResultBytes: maxToolResultBytes,
		// Registry reads are independent of each other and of session state.
		ParallelSafe: true,
	}
}

// proposeMeta is the metadata of a tool that records a proposal or evidence
// through an RPC handler.
func proposeMeta(action dataquality.ActionID) tool.Meta {
	return tool.Meta{
		Group:          "quality",
		Scope:          tool.ScopeCatalog,
		Authz:          tool.AuthzPermission,
		SideEffect:     tool.SideEffectPropose,
		Permission:     string(permission.DataQualityPlansUpdate),
		Action:         string(action),
		MaxResultBytes: maxToolResultBytes,
	}
}

// internalMeta is the metadata of a wake-control tool. It belongs to this product
// only and is scoped by session ownership, not by a workspace permission.
func internalMeta() tool.Meta {
	return tool.Meta{
		Group:  "agent",
		Scope:  tool.ScopeProductOnly,
		Authz:  tool.AuthzInternal,
		Action: "",
	}
}

// modelToolSpecs is the closed tool set of a model wake. It is read- and
// propose-only; publishing, approving, editing checks and changing settings are
// not exposed.
func modelToolSpecs(mode storepb.QualityAgentSessionMode) []toolSpec {
	empty := map[string]any{"type": "object", "properties": map[string]any{}}
	specs := []toolSpec{
		{
			name:        "get_target_summary",
			description: "Read the target's plan state, criticality, rule count and uncovered declared properties.",
			parameters:  empty,
			meta:        readMeta(),
		},
		{
			name:        "list_recent_executions",
			description: "List the target's recent executions with their outcomes and failed row counts.",
			parameters:  empty,
			meta:        readMeta(),
		},
		{
			name:        "get_memory",
			description: "List the confirmed memories about the target.",
			parameters:  empty,
			meta:        readMeta(),
		},
		{
			name:        "record_investigation",
			description: "Record an investigation with the evidence behind it.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"summary":   map[string]any{"type": "string"},
				"verdict":   map[string]any{"type": "string", "enum": []string{"root_cause", "contributing_factor", "inconclusive"}},
				"reference": map[string]any{"type": "string", "description": "an execution resource name when the investigation is about one"},
			}, "required": []string{"summary"}},
			meta: proposeMeta(dataquality.ActionWriteInvestigation),
		},
		{
			name:        "record_asset_fact",
			description: "Record a deterministic observation about the target as an asset fact.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"statement": map[string]any{"type": "string"},
			}, "required": []string{"statement"}},
			meta: proposeMeta(dataquality.ActionWriteAssetFact),
		},
		{
			name:        "record_decision",
			description: "Record a proposed decision about the target; a person confirms it before it counts as knowledge.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"statement": map[string]any{"type": "string"},
			}, "required": []string{"statement"}},
			meta: proposeMeta(dataquality.ActionRecordDecisionWhy),
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
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE {
		specs = append(specs,
			toolSpec{
				name:        "get_table_context",
				description: "Read the target's schema, column profiles, semantic labels and lineage consumers; no sample rows.",
				parameters:  empty,
				meta:        readMeta(),
			},
			toolSpec{
				name:        "draft_checks",
				description: "Draft concrete checks for the target into the review queue; a person approves them before they run.",
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
					Scope:          tool.ScopeCatalog,
					Authz:          tool.AuthzPermission,
					SideEffect:     tool.SideEffectPropose,
					Permission:     string(permission.DataQualitySuggest),
					MaxResultBytes: maxToolResultBytes,
				},
			},
		)
	}
	return specs
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
	// Tools is the explicit whitelist; the registry validates it at wake time.
	Tools []toolSpec
	// MaxTurns bounds one wake's model calls.
	MaxTurns int
}

// The specs are process-level declarations: one prompt, one tool whitelist and one
// turn budget per product, resolved once.
var (
	// monitorProduct is the long-lived monitoring product: one target, woken by
	// events, questions and a daily cadence.
	monitorProduct = &Spec{
		Name:     "quality-monitor",
		Feature:  "dq_agent",
		Prompt:   qualityPrompt(),
		Tools:    modelToolSpecs(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR),
		MaxTurns: modelMaxTurns,
	}
	// triageProduct is the coverage product: it inspects an uncovered target,
	// drafts checks and records what it concluded.
	triageProduct = &Spec{
		Name:     "quality-triage",
		Feature:  "dq_agent",
		Prompt:   qualityPrompt(),
		Tools:    modelToolSpecs(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE),
		MaxTurns: triageMaxTurns,
	}
)

// monitorSpec returns the monitoring product.
func monitorSpec() *Spec { return monitorProduct }

// triageSpec returns the coverage product.
func triageSpec() *Spec { return triageProduct }

// specFor selects the product a session runs.
func specFor(mode storepb.QualityAgentSessionMode) *Spec {
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE {
		return triageSpec()
	}
	return monitorSpec()
}
