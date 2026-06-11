package llm

import (
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
	SQLText    string // DDL or definition
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

	parts := strings.Split(guid, ";")
	if len(parts) >= 2 {
		obj.DBName = parts[1]
	}
	if len(parts) >= 4 {
		obj.SchemaName = parts[2]
	}

	return obj
}

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
							"description": "The object name. Can be unqualified (e.g. 'users') or qualified (e.g. 'public.users').",
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
