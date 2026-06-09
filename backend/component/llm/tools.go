package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// SchemaContext holds pre-built schema information about database objects
// that the LLM can query via tools.
type SchemaContext struct {
	Objects []SchemaObject
}

// SchemaObject describes a database object (table, view, procedure, etc.).
type SchemaObject struct {
	GUID       string
	Name       string
	SchemaName string
	DBName     string
	MetaType   storepb.MetaType
	SQLText    string   // DDL or definition
	Columns    []ColumnInfo
	Indexes    []string
}

// ColumnInfo describes a column.
type ColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	Comment  string
}

// BuildContextFromMetadata builds schema context from the list of metadata objects.
func BuildContextFromMetadata(metas []*storepb.StoredMetadata, guids []string) *SchemaContext {
	ctx := &SchemaContext{}
	for i, meta := range metas {
		guid := ""
		if i < len(guids) {
			guid = guids[i]
		}
		obj := convertToSchemaObject(guid, meta)
		if obj != nil {
			ctx.Objects = append(ctx.Objects, *obj)
		}
	}
	return ctx
}

func convertToSchemaObject(guid string, meta *storepb.StoredMetadata) *SchemaObject {
	obj := &SchemaObject{GUID: guid}

	switch {
	case meta.GetTableMetadata() != nil:
		t := meta.GetTableMetadata()
		obj.Name = t.Name
		obj.MetaType = storepb.MetaType_TABLE
		for _, c := range t.Columns {
			obj.Columns = append(obj.Columns, ColumnInfo{
				Name:     c.Name,
				Type:     c.Type,
				Nullable: c.Nullable,
				Comment:  c.Comment,
			})
		}
		for _, idx := range t.Indexes {
			obj.Indexes = append(obj.Indexes, idx.Name)
		}

	case meta.GetViewMetadata() != nil:
		v := meta.GetViewMetadata()
		obj.Name = v.Name
		obj.MetaType = storepb.MetaType_VIEW
		obj.SQLText = v.Definition
		for _, c := range v.Columns {
			obj.Columns = append(obj.Columns, ColumnInfo{
				Name:     c.Name,
				Type:     c.Type,
				Nullable: c.Nullable,
				Comment:  c.Comment,
			})
		}

	case meta.GetMaterializedViewMetadata() != nil:
		mv := meta.GetMaterializedViewMetadata()
		obj.Name = mv.Name
		obj.MetaType = storepb.MetaType_MATERIALIZED_VIEW
		obj.SQLText = mv.Definition

	case meta.GetFunctionMetadata() != nil:
		f := meta.GetFunctionMetadata()
		obj.Name = f.Name
		obj.MetaType = storepb.MetaType_FUNCTION
		obj.SQLText = f.Definition

	case meta.GetProcedureMetadata() != nil:
		p := meta.GetProcedureMetadata()
		obj.Name = p.Name
		obj.MetaType = storepb.MetaType_PROCEDURE
		obj.SQLText = p.Definition

	case meta.GetExternalTableMetadata() != nil:
		et := meta.GetExternalTableMetadata()
		obj.Name = et.Name
		obj.MetaType = storepb.MetaType_EXTERNAL_TABLE
		for _, c := range et.Columns {
			obj.Columns = append(obj.Columns, ColumnInfo{
				Name: c.Name,
				Type: c.Type,
			})
		}

	default:
		return nil
	}

	// Extract schema and database name from GUID: instance;db;schema;name
	parts := strings.Split(guid, ";")
	if len(parts) >= 2 {
		obj.DBName = parts[1]
	}
	if len(parts) >= 3 {
		obj.SchemaName = parts[2]
	}

	return obj
}

// ---- Tool Definitions ----

// ExplainSQLTools returns the tool definitions available for SQL explanation.
func ExplainSQLTools() []ToolDef {
	return []ToolDef{
		{
			Type: "function",
			Function: struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Parameters  any    `json:"parameters"`
			}{
				Name:        "get_object_schema",
				Description: "Get detailed schema information for a database object (table, view, function, procedure, etc.). Use this when the SQL references an object and you need to understand its structure (columns, types, indexes).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "The object name. Can be unqualified (e.g. 'users') or qualified (e.g. 'public.users'). Case-insensitive fuzzy match is supported.",
						},
					},
					"required": []string{"name"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Parameters  any    `json:"parameters"`
			}{
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
			},
		},
	}
}

// ExecuteTool executes a tool call against the given schema context.
func ExecuteTool(tc ToolCall, ctx *SchemaContext) ([]ToolResult, error) {
	switch tc.Function.Name {
	case "get_object_schema":
		return executeGetObjectSchema(tc, ctx)
	case "search_objects":
		return executeSearchObjects(tc, ctx)
	default:
		return nil, fmt.Errorf("unknown tool: %s", tc.Function.Name)
	}
}

func executeGetObjectSchema(tc ToolCall, ctx *SchemaContext) ([]ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments for get_object_schema: %w", err)
	}

	query := strings.ToLower(args.Name)

	var matches []SchemaObject
	for _, obj := range ctx.Objects {
		if matchObject(obj, query) {
			matches = append(matches, obj)
		}
	}

	if len(matches) == 0 {
		return []ToolResult{{ToolCallID: tc.ID, Content: fmt.Sprintf(`{"error": "no object found matching '%s'"}`, args.Name)}}, nil
	}

	// Format results as JSON.
	type resultObj struct {
		Name       string       `json:"name"`
		Type       string       `json:"type"`
		Schema     string       `json:"schema,omitempty"`
		Database   string       `json:"database,omitempty"`
		Columns    []ColumnInfo `json:"columns,omitempty"`
		Indexes    []string     `json:"indexes,omitempty"`
		SQLPreview string       `json:"sql_preview,omitempty"`
	}

	results := make([]resultObj, 0, len(matches))
	for _, obj := range matches {
		sqlPreview := obj.SQLText
		if len(sqlPreview) > 500 {
			sqlPreview = sqlPreview[:500] + "..."
		}
		results = append(results, resultObj{
			Name:       obj.Name,
			Type:       obj.MetaType.String(),
			Schema:     obj.SchemaName,
			Database:   obj.DBName,
			Columns:    obj.Columns,
			Indexes:    obj.Indexes,
			SQLPreview: sqlPreview,
		})
	}

	resultJSON, err := json.Marshal(results)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool result: %w", err)
	}

	return []ToolResult{{ToolCallID: tc.ID, Content: string(resultJSON)}}, nil
}

func executeSearchObjects(tc ToolCall, ctx *SchemaContext) ([]ToolResult, error) {
	var args struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments for search_objects: %w", err)
	}

	kw := strings.ToLower(args.Keyword)

	type objSummary struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Schema   string `json:"schema,omitempty"`
		Database string `json:"database,omitempty"`
	}

	var matches []objSummary
	for _, obj := range ctx.Objects {
		if matchObject(obj, kw) {
			matches = append(matches, objSummary{
				Name:     obj.Name,
				Type:     obj.MetaType.String(),
				Schema:   obj.SchemaName,
				Database: obj.DBName,
			})
		}
	}

	if len(matches) == 0 {
		return []ToolResult{{ToolCallID: tc.ID, Content: `{"objects": []}`}}, nil
	}

	resultJSON, _ := json.Marshal(map[string]any{"objects": matches})
	return []ToolResult{{ToolCallID: tc.ID, Content: string(resultJSON)}}, nil
}

func matchObject(obj SchemaObject, query string) bool {
	qParts := strings.Split(query, ".")
	lastPart := qParts[len(qParts)-1]

	if strings.Contains(strings.ToLower(obj.Name), lastPart) {
		return true
	}
	if obj.SchemaName != "" && strings.Contains(strings.ToLower(obj.SchemaName), lastPart) {
		return true
	}
	// Fuzzy: if query has schema.name format, match both.
	if len(qParts) == 2 {
		schemaPart := strings.ToLower(qParts[0])
		namePart := strings.ToLower(qParts[1])
		if (obj.SchemaName == "" || strings.Contains(strings.ToLower(obj.SchemaName), schemaPart)) &&
			strings.Contains(strings.ToLower(obj.Name), namePart) {
			return true
		}
	}
	return false
}
