package agent

import (
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

// ToolDefs projects a resolved tool set's schemas onto the provider's
// function-definition shape. It is the one conversion every product uses, so what
// the model sees cannot drift from what the set can run.
func ToolDefs(schemas []tool.Schema) []llm.ToolDef {
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
