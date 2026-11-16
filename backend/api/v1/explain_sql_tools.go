package v1

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

// explainSQLToolSet builds the read-only tool set of one ExplainSQL request. The
// declarations are the request's capability surface; the execution bodies call the
// service's own lookups, so the tool answers from the same registry the statement
// was resolved against.
func (s *ExplainSQLService) explainSQLToolSet(scopePrefix, instanceID string) (*tool.Set, error) {
	registry := tool.NewRegistry()
	read := func() tool.Meta {
		return tool.Meta{
			Group:      "metadata",
			Scope:      tool.ScopeCatalog,
			Authz:      tool.AuthzPermission,
			ReadOnly:   true,
			Permission: string(permission.DatabasesRead),
		}
	}
	if err := registry.Register(tool.Tool{
		Name:        "get_object_schema",
		Description: "Get detailed schema information for a database object (table, view, function, procedure, etc.). Use this when the SQL references an object and you need to understand its structure (columns, types, indexes).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The object name. Can be unqualified (e.g. 'users') or qualified (e.g. 'public.users').",
				},
			},
			"required": []string{"name"},
		},
		Meta: read(),
		Execute: func(ctx context.Context, call tool.Call, _ *tool.Invocation) (tool.Result, error) {
			results, err := s.toolGetObjectSchema(ctx, explainToolCall(call), scopePrefix, instanceID)
			return explainToolResult(results, err)
		},
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(tool.Tool{
		Name:        "search_objects",
		Description: "Search for database objects by keyword. Returns a list of matching object names and types. Use this to discover what objects exist before calling get_object_schema.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword": map[string]any{
					"type":        "string",
					"description": "Keyword to search for. Matches object names, column names, and SQL definitions.",
				},
			},
			"required": []string{"keyword"},
		},
		Meta: read(),
		Execute: func(ctx context.Context, call tool.Call, _ *tool.Invocation) (tool.Result, error) {
			results, err := s.toolSearchObjects(ctx, explainToolCall(call), scopePrefix)
			return explainToolResult(results, err)
		},
	}); err != nil {
		return nil, err
	}
	return registry.Resolve([]string{"get_object_schema", "search_objects"})
}

// explainToolCall restores the provider call shape the service's tool bodies
// still take.
func explainToolCall(call tool.Call) llm.ToolCall {
	var converted llm.ToolCall
	converted.ID = call.ID
	converted.Type = "function"
	converted.Function.Name = call.Name
	converted.Function.Arguments = string(call.Arguments)
	return converted
}

// explainToolResult flattens the service's single-result shape into the tool
// contract.
func explainToolResult(results []llm.ToolResult, err error) (tool.Result, error) {
	if err != nil {
		return tool.Result{}, err
	}
	if len(results) == 0 {
		return tool.Result{}, nil
	}
	return tool.Result{Content: results[0].Content}, nil
}

// toolDefsFromSchemas projects tool schemas onto the provider's function
// definitions. The agent runtime owns this projection once it is extracted; until
// then each adapter carries the same conversion.
func toolDefsFromSchemas(schemas []tool.Schema) []llm.ToolDef {
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
