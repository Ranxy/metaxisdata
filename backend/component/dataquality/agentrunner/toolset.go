package agentrunner

import (
	"context"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxToolResultBytes bounds one tool result fed back to the model. The context is
// the scarce resource: an oversized result is cut, and the transcript records the
// truncation.
const maxToolResultBytes = 8 << 10

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

// toolControl is the wake state a control tool may change. The execution body
// writes it explicitly instead of hiding it in shared mutable state.
type toolControl struct {
	blocked        *bool
	blockedCode    *string
	blockedWhy     *string
	nextWake       **time.Time
	scheduleReason *string
}

// toolSet binds the mode's declarations to this wake's execution body. The set is
// per wake because a tool body reads the session and its wake state; the
// declarations are shared.
func (e *Engine) toolSet(session *store.QualityAgentSessionMessage, control *toolControl) (*tool.Set, error) {
	specs := modelToolSpecs(session.Mode)
	registry := tool.NewRegistry()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		spec := spec
		names = append(names, spec.name)
		err := registry.Register(tool.Tool{
			Name:        spec.name,
			Description: spec.description,
			Parameters:  spec.parameters,
			Meta:        spec.meta,
			Execute: func(ctx context.Context, call tool.Call, _ *tool.Invocation) (tool.Result, error) {
				content, err := e.executeTool(ctx, session, spec.name, string(call.Arguments),
					control.blocked, control.blockedCode, control.blockedWhy, control.nextWake, control.scheduleReason)
				if err != nil {
					return tool.Result{}, err
				}
				return tool.Result{Content: content}, nil
			},
		})
		if err != nil {
			return nil, err
		}
	}
	return registry.Resolve(names)
}

// llmToolDefs projects the tool schemas onto the provider's function-definition
// shape. The agent runtime owns this projection once it is extracted; until then
// each adapter carries the same ten-line conversion.
func llmToolDefs(schemas []tool.Schema) []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(schemas))
	for _, schema := range schemas {
		var def llm.ToolDef
		def.Type = "function"
		def.Function.Name = schema.Name
		def.Function.Description = schema.Description
		def.Function.Parameters = schema.Parameters
		defs = append(defs, def)
	}
	return defs
}
